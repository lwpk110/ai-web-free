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

func buildOpenAIModelsResponse(slugs []string) (officialtypes.ModelsResponse, []string) {
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
	return result, upstream
}

// ListModels 将 ChatGPT 网页模型 slug 映射为 OpenAI 模型 ID。
func (h *ModelsHandler) ListModels(c *gin.Context) {
	if cached, ok := gatewayModelsResponse(c, h.accountPool); ok {
		c.JSON(http.StatusOK, cached)
		return
	}
	account, status, err := h.resolveAccount(c)
	if err != nil {
		respondError(c, status, err)
		return
	}

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
			result, _ := buildOpenAIModelsResponse(cached)
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
	result, upstreamSlugs := buildOpenAIModelsResponse(rawSlugs)
	if h.accountPool != nil && !hasTeamOverride && len(upstreamSlugs) > 0 {
		h.accountPool.RecordAccountModels(account.ID, upstreamSlugs)
	}
	c.JSON(http.StatusOK, filterModelsForGateway(c, result))
}

func (h *ModelsHandler) resolveAccount(c *gin.Context) (*accounts.Account, int, error) {
	if acct, st, err, ok := acquireForGateway(c, h.accountPool, true, "", nil); ok {
		return acct, st, err
	}
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
