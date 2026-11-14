package handler

import (
	"errors"
	"net/http"
	"strings"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/authresolver"
	"ai-web-free/internal/chatgpt"
	"ai-web-free/internal/config"
	officialtypes "ai-web-free/typings/official"

	"github.com/gin-gonic/gin"
)

type ModelsHandler struct {
	accountPool *accounts.Pool
	cfg         *config.Config
}

func NewModelsHandler(pool *accounts.Pool, cfg *config.Config) *ModelsHandler {
	return &ModelsHandler{accountPool: pool, cfg: cfg}
}

// BuiltinModelIDs 返回网关内置兼容的图像、TTS 与语音识别模型列表。
func BuiltinModelIDs() []string {
	return []string{
		"gpt-image-2.5",
		"gpt-image-2.5-sunburst",
		"gpt-image-2.5-flare",
		"gpt-image-2.6",
		"gpt-image-2",
		"gpt-image-1",
		"dall-e-3",
		"gpt-4o-mini-tts",
		"tts-1",
		"tts-1-hd",
		"gpt-4o-transcribe",
		"whisper-1",
	}
}

func buildOpenAIModelsResponse(slugs []string, includeBuiltins bool) (officialtypes.ModelsResponse, []string) {
	result := officialtypes.ModelsResponse{Object: "list", Data: make([]officialtypes.Model, 0, len(slugs))}
	seen := make(map[string]bool, len(slugs))
	upstream := make([]string, 0, len(slugs))
	for _, raw := range slugs {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		upstream = append(upstream, id)
		result.Data = append(result.Data, officialtypes.Model{
			ID: id, Object: "model", OwnedBy: "openai",
			// 网页接口未提供创建时间，使用 0 表示未知。
			Created: 0,
		})
	}
	if includeBuiltins {
		for _, id := range BuiltinModelIDs() {
			if !seen[id] {
				seen[id] = true
				result.Data = append(result.Data, officialtypes.Model{
					ID: id, Object: "model", OwnedBy: "openai", Created: 0,
				})
			}
		}
	}
	return result, upstream
}

// ListModels 将 ChatGPT 网页模型 slug 映射为 OpenAI 模型 ID。
func (h *ModelsHandler) ListModels(c *gin.Context) {
	account, status, err := h.resolveAccount(c)
	if err != nil {
		respondError(c, status, err)
		return
	}

	includeBuiltins := c.Query("include_builtins") == "true" || c.Query("include_builtins") == "1"
	useCache := c.Query("use_cache") == "true" || c.Query("use_cache") == "1"

	// 工作区覆盖仅作用于本次请求，不修改池内共享账号。
	requestAccount := *account
	hasTeamOverride := false
	if parts := strings.SplitN(c.GetHeader("Authorization"), ",", 2); len(parts) == 2 {
		requestAccount.TeamUserID = strings.TrimSpace(parts[1])
		hasTeamOverride = true
	}
	for _, header := range []string{"ChatGPT-Account-ID", "Team-Account-ID", "X-ChatGPT-Account-ID"} {
		if value := strings.TrimSpace(c.GetHeader(header)); value != "" {
			requestAccount.TeamUserID = value
			hasTeamOverride = true
			break
		}
	}

	if useCache && !account.IsTemporary && !hasTeamOverride && h.accountPool != nil {
		if cached, _ := h.accountPool.CachedModels(); len(cached) > 0 {
			result, _ := buildOpenAIModelsResponse(cached, includeBuiltins)
			c.JSON(http.StatusOK, result)
			return
		}
	}

	models, status, err := chatgpt.ListModels(account.Client, &requestAccount)
	if err != nil {
		if status == http.StatusUnauthorized {
			h.accountPool.ReportFailure(account)
		}
		respondError(c, status, err)
		return
	}

	rawSlugs := make([]string, 0, len(models))
	for _, model := range models {
		rawSlugs = append(rawSlugs, model.Slug)
	}
	result, upstreamSlugs := buildOpenAIModelsResponse(rawSlugs, includeBuiltins)
	if h.accountPool != nil && !hasTeamOverride && len(upstreamSlugs) > 0 {
		h.accountPool.RecordAccountModels(account.ID, upstreamSlugs)
	}
	c.JSON(http.StatusOK, result)
}

func (h *ModelsHandler) resolveAccount(c *gin.Context) (*accounts.Account, int, error) {
	resolver := authresolver.AccessTokenResolver{CustomerKey: h.cfg.Authorization}
	token, _, err := resolver.ResolveAccessToken(c)
	if err != nil {
		return nil, http.StatusUnauthorized, err
	}
	if strings.HasPrefix(token, "eyJ") {
		if !h.cfg.EnableExternalToken {
			return nil, http.StatusUnauthorized, errors.New("external access token disabled (set ENABLE_EXTERNAL_TOKEN=true)")
		}
		proxyURL := h.cfg.ProxyURL
		if proxyURL == "" {
			proxyURL = h.cfg.HTTPProxy
		}
		return h.accountPool.GetOrCreateTempAccount(token, c.GetHeader("User-Agent"), proxyURL), http.StatusOK, nil
	}
	if token != "" {
		return resolveAccount(c, h.accountPool, h.cfg, true)
	}
	// /backend-api/models 需要登录态，匿名 UUID 不参与模型查询。
	for _, kind := range []accounts.AccountType{accounts.TypeFree, accounts.TypePUID} {
		if account, err := h.accountPool.Acquire(kind); err == nil {
			return account, http.StatusOK, nil
		}
	}
	return nil, http.StatusUnauthorized, ErrNoAvailable
}
