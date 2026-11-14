package handler

import (
	"context"
	"errors"
	"net/http"
	"strings"

	chatgptrequestconverter "ai-web-free/conversion/requests/chatgpt"
	"ai-web-free/httpclient/bogdanfinn"
	"ai-web-free/internal/accounts"
	"ai-web-free/internal/chatgpt"
	"ai-web-free/internal/config"
	"ai-web-free/internal/promptplaza"
	chatgpt_types "ai-web-free/typings/chatgpt"
	officialtypes "ai-web-free/typings/official"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// NewPoolChatExecutor 构建基于网关本机号池与 Failover 运行时的文本/联网搜索执行器。
func NewPoolChatExecutor(rt *GatewayRuntime, pool *accounts.Pool, cfg *config.Config) promptplaza.PoolChatExecutor {
	return func(ctx context.Context, systemPrompt, userPrompt string, useWebSearch bool) (string, error) {
		if pool == nil || rt == nil {
			return "", errors.New("account pool unavailable")
		}
		if ctx != nil && ctx.Err() != nil {
			return "", ctx.Err()
		}

		messages := make([]officialtypes.APIMessage, 0, 2)
		if strings.TrimSpace(systemPrompt) != "" {
			messages = append(messages, officialtypes.APIMessage{
				Role:    "system",
				Content: officialtypes.MessageContent{TextValue: systemPrompt},
			})
		}
		messages = append(messages, officialtypes.APIMessage{
			Role:    "user",
			Content: officialtypes.MessageContent{TextValue: userPrompt},
		})

		apiReq := officialtypes.APIRequest{
			Model:    "auto",
			Stream:   false,
			Messages: messages,
		}

		res, err := rt.RunWithFailover(nil, pool, cfg, FailoverOptions{
			Capability: accounts.CapChat.Name,
			Model:      "auto",
		}, func(acct *accounts.Account, client *bogdanfinn.TlsClient) AttemptOutcome {
			if !AccountSatisfies(acct, accounts.CapChat.Name) {
				return AttemptOutcome{StatusCode: http.StatusForbidden, Err: errAccountCapDenied}
			}
			translated := chatgptrequestconverter.ConvertAPIRequest(apiReq, acct, acct.Proxy, client)
			if useWebSearch {
				translated.SystemHints = []string{"search"}
				for idx := range translated.Messages {
					if translated.Messages[idx].Author.Role == "user" {
						if translated.Messages[idx].Metadata == nil {
							translated.Messages[idx].Metadata = map[string]interface{}{}
						}
						translated.Messages[idx].Metadata["system_hints"] = []string{"search"}
					}
				}
			}
			state := chatgpt.NewChatClientState()
			resp, wsConn, turnStile, status, callErr := conversationClientOrder(&client, acct, translated, acct.Proxy, false, state, pool)
			return AttemptOutcome{
				Response:   resp,
				WSConn:     wsConn,
				TurnStile:  turnStile,
				Payload:    chatAttemptBundle{client: client, translated: translated, clientState: state},
				StatusCode: status,
				Err:        callErr,
			}
		})
		if err != nil {
			return "", err
		}
		if res.Outcome.Response == nil {
			return "", errors.New("empty upstream response")
		}
		defer res.Outcome.Response.Body.Close()
		if res.Outcome.Response.StatusCode != http.StatusOK {
			if res.Outcome.WSConn != nil {
				_ = res.Outcome.WSConn.Close()
			}
			return "", FailoverStatusError(res, res.Outcome.Response.StatusCode)
		}

		bundle, _ := res.Outcome.Payload.(chatAttemptBundle)
		result := chatgpt.HandlerDetailedWithOptions(
			nil,
			res.Outcome.Response,
			bundle.client,
			res.Account,
			uuid.NewString(),
			bundle.translated,
			false,
			"auto",
			chatgpt.HandlerDetailedOptions{
				Websocket:      res.Outcome.WSConn,
				ClientState:    bundle.clientState,
				ProxyURL:       res.Account.Proxy,
				SuppressOutput: true,
			},
		)
		text := strings.TrimSpace(result.Text)
		if text == "" {
			return "", errors.New("empty completion text")
		}
		return text, nil
	}
}

func (h *AdminHandler) promptPlaza() *promptplaza.Manager {
	if h.runtime != nil && h.runtime.PromptPlaza != nil {
		return h.runtime.PromptPlaza
	}
	return nil
}

// ListPrompts 返回提示词广场筛选列表、分类统计及同步状态。
func (h *AdminHandler) ListPrompts(c *gin.Context) {
	mgr := h.promptPlaza()
	if mgr == nil {
		respondError(c, http.StatusServiceUnavailable, errors.New("prompt plaza unavailable"))
		return
	}
	resp := mgr.List(promptplaza.ListFilter{
		Category: c.Query("category"),
		Mode:     c.Query("mode"),
		Source:   c.Query("source"),
		Query:    c.Query("q"),
		Sort:     c.Query("sort"),
	})
	c.JSON(http.StatusOK, resp)
}

// SyncPrompts 触发一次多源社区爬虫抓取与号池 AI 提炼任务。
func (h *AdminHandler) SyncPrompts(c *gin.Context) {
	mgr := h.promptPlaza()
	if mgr == nil {
		respondError(c, http.StatusServiceUnavailable, errors.New("prompt plaza unavailable"))
		return
	}
	var body struct {
		Wait bool `json:"wait"`
	}
	_ = c.ShouldBindJSON(&body)
	wait := body.Wait || c.Query("wait") == "1" || c.Query("sync") == "1"

	status := mgr.TriggerSync(c.Request.Context(), !wait)
	if h.auditLog() != nil {
		h.auditLog().RecordEvent("/admin/api/v1/prompts/sync", "admin", "", "sync", status.State)
	}
	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"sync_status": status,
	})
}

// ExpandPrompt 调用本机号池将简短创意扩写为结构化中英双语生图提示词。
func (h *AdminHandler) ExpandPrompt(c *gin.Context) {
	mgr := h.promptPlaza()
	if mgr == nil {
		respondError(c, http.StatusServiceUnavailable, errors.New("prompt plaza unavailable"))
		return
	}
	var req promptplaza.ExpandRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(req.Idea) == "" {
		respondError(c, http.StatusBadRequest, errors.New("idea is required"))
		return
	}
	expanded, err := mgr.Expand(c.Request.Context(), req)
	if err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	c.JSON(http.StatusOK, expanded)
}

// ensure chatgpt_types stays imported for type completeness if needed
var _ = chatgpt_types.NewChatGPTRequest
