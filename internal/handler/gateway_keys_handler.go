package handler

import (
	"errors"
	"net/http"
	"strings"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/gatewaykeys"
	officialtypes "ai-web-free/typings/official"

	"github.com/gin-gonic/gin"
)

const (
	gwStoreCtxKey  = "gw_store"
	gwGroupCtxKey  = "gw_group"
	gwKeyCtxKey    = "gw_key"
	gwAuthedCtxKey = "gw_authed"
)

func gatewayError(c *gin.Context, status int, code string, err error) {
	c.AbortWithStatusJSON(status, gin.H{"error": gin.H{
		"message": err.Error(),
		"type":    "invalid_request_error",
		"param":   nil,
		"code":    code,
	}})
}

// GatewayKeyAuth 识别 sk-gw- 前缀的网关 API Key：校验、限流、绑定分组并统计。
// 非网关 Key 的请求原样放行，由旧的全局密钥 / 透传令牌逻辑处理。
func GatewayKeyAuth(store *gatewaykeys.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := bearerToken(c.GetHeader("Authorization"))
		if !gatewaykeys.IsGatewayKey(token) {
			c.Next()
			return
		}
		if store == nil {
			gatewayError(c, http.StatusUnauthorized, "invalid_api_key", errors.New("gateway API keys are not enabled"))
			return
		}
		key, group, err := store.Authenticate(token)
		if err != nil {
			gatewayError(c, http.StatusUnauthorized, "invalid_api_key", errors.New("invalid API key"))
			return
		}
		release, err := store.Admit(key)
		if err != nil {
			gatewayError(c, http.StatusTooManyRequests, "rate_limit_exceeded", err)
			return
		}
		defer release()

		c.Set(gwStoreCtxKey, store)
		c.Set(gwGroupCtxKey, group)
		c.Set(gwKeyCtxKey, key)
		c.Set(gwAuthedCtxKey, true)
		// 清除网关 Key，使后续账号解析走号池分支，不会把它误当作 ChatGPT 令牌。
		c.Request.Header.Del("Authorization")

		c.Next()
		store.Record(key.ID, c.Writer.Status() >= http.StatusBadRequest)
	}
}

// LegacyAuthorization 已通过网关 Key 鉴权的请求跳过旧的全局密钥校验。
func LegacyAuthorization(legacy gin.HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.GetBool(gwAuthedCtxKey) {
			c.Next()
			return
		}
		legacy(c)
	}
}

func bearerToken(h string) string {
	h = strings.TrimSpace(h)
	if len(h) >= 7 && strings.EqualFold(h[:7], "Bearer ") {
		h = strings.TrimSpace(h[7:])
	}
	return h
}

func gatewayGroup(c *gin.Context) (*gatewaykeys.Group, *gatewaykeys.Store) {
	if c == nil {
		return nil, nil
	}
	v, ok := c.Get(gwGroupCtxKey)
	if !ok {
		return nil, nil
	}
	g, _ := v.(*gatewaykeys.Group)
	sv, _ := c.Get(gwStoreCtxKey)
	s, _ := sv.(*gatewaykeys.Store)
	return g, s
}

// acquireForGateway 按 Key 所属分组（耗尽时降级分组）取号。ok=false 表示本请求不是网关 Key 请求。
func acquireForGateway(c *gin.Context, pool *accounts.Pool, needsPaid bool, capName string, exclude map[string]bool) (acct *accounts.Account, status int, err error, ok bool) {
	group, store := gatewayGroup(c)
	if group == nil {
		return nil, 0, nil, false
	}
	if pool == nil {
		return nil, http.StatusServiceUnavailable, ErrNoAvailable, true
	}
	candidates := []*gatewaykeys.Group{group}
	if fb := store.Fallback(group); fb != nil {
		candidates = append(candidates, fb)
	}
	for _, g := range candidates {
		a, e := pool.AcquireMatching(g.Types(), g.Match(), capName, exclude)
		if e != nil || a == nil {
			continue
		}
		if needsPaid && a.Type == accounts.TypeNoAuth {
			return nil, http.StatusForbidden, errors.New("this endpoint requires a logged-in ChatGPT account"), true
		}
		return a, http.StatusOK, nil, true
	}
	return nil, http.StatusServiceUnavailable, ErrNoAvailable, true
}

// applyGatewayModel 对网关 Key 请求执行模型白名单校验与别名映射；失败时已写出 403 响应。
func applyGatewayModel(c *gin.Context, model *string) bool {
	group, _ := gatewayGroup(c)
	if group == nil {
		return true
	}
	actual, err := group.ResolveModel(*model)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": gin.H{
			"message": err.Error(),
			"type":    "invalid_request_error",
			"param":   "model",
			"code":    "model_not_allowed",
		}})
		return false
	}
	*model = actual
	return true
}

// gatewayModelsResponse 构造分组可见模型列表（缓存为空时返回 ok=false，交由实时抓取）。
func gatewayModelsResponse(c *gin.Context, pool *accounts.Pool) (officialtypes.ModelsResponse, bool) {
	group, _ := gatewayGroup(c)
	if group == nil || pool == nil {
		return officialtypes.ModelsResponse{}, false
	}
	cached := pool.CollectModels(group.Types(), group.Match())
	if len(cached) == 0 {
		return officialtypes.ModelsResponse{}, false
	}
	return filterModelsForGateway(c, buildModelsFromIDs(cached)), true
}

func buildModelsFromIDs(ids []string) officialtypes.ModelsResponse {
	res, _ := buildOpenAIModelsResponse(ids)
	return res
}

// filterModelsForGateway 把完整模型响应收敛为分组可见模型（非网关请求原样返回）。
func filterModelsForGateway(c *gin.Context, in officialtypes.ModelsResponse) officialtypes.ModelsResponse {
	group, _ := gatewayGroup(c)
	if group == nil {
		return in
	}
	ids := make([]string, 0, len(in.Data))
	for _, m := range in.Data {
		ids = append(ids, m.ID)
	}
	res, _ := buildOpenAIModelsResponse(group.VisibleModels(ids))
	return res
}

// ─── Admin: /admin/api/v1/gateway/{groups,keys} ───────────────

// GatewayKeysHandler 网关 Key 与分组管理接口。
type GatewayKeysHandler struct {
	store *gatewaykeys.Store
}

func NewGatewayKeysHandler(store *gatewaykeys.Store) *GatewayKeysHandler {
	return &GatewayKeysHandler{store: store}
}

func (h *GatewayKeysHandler) fail(c *gin.Context, err error) {
	switch {
	case errors.Is(err, gatewaykeys.ErrNotFound):
		respondError(c, http.StatusNotFound, err)
	case errors.Is(err, gatewaykeys.ErrInvalid):
		respondError(c, http.StatusBadRequest, err)
	case errors.Is(err, gatewaykeys.ErrRevealUnsupport):
		respondError(c, http.StatusConflict, err)
	default:
		respondError(c, http.StatusInternalServerError, err)
	}
}

func (h *GatewayKeysHandler) ListGroups(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"groups": h.store.ListGroups()})
}

func (h *GatewayKeysHandler) CreateGroup(c *gin.Context) {
	var in gatewaykeys.Group
	if err := c.ShouldBindJSON(&in); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	in.Enabled = true
	g, err := h.store.CreateGroup(in)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, g)
}

func (h *GatewayKeysHandler) UpdateGroup(c *gin.Context) {
	var in gatewaykeys.Group
	if err := c.ShouldBindJSON(&in); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	g, err := h.store.UpdateGroup(c.Param("id"), in)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, g)
}

func (h *GatewayKeysHandler) DeleteGroup(c *gin.Context) {
	if err := h.store.DeleteGroup(c.Param("id")); err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

type gatewayKeyBody struct {
	Name          string `json:"name"`
	GroupID       string `json:"group_id"`
	RPM           int    `json:"rpm"`
	MaxConcurrent int    `json:"max_concurrent"`
	Enabled       *bool  `json:"enabled"`
}

func (h *GatewayKeysHandler) ListKeys(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"keys": h.store.ListKeys()})
}

// CreateKey 创建 Key；响应中的 key 为明文，同样可经 RevealKey 再次获取（需配置加密密钥）。
func (h *GatewayKeysHandler) CreateKey(c *gin.Context) {
	var in gatewayKeyBody
	if err := c.ShouldBindJSON(&in); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	k, plain, err := h.store.CreateKey(in.Name, in.GroupID, in.RPM, in.MaxConcurrent)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"key": plain, "record": k})
}

func (h *GatewayKeysHandler) UpdateKey(c *gin.Context) {
	var in gatewayKeyBody
	if err := c.ShouldBindJSON(&in); err != nil {
		respondError(c, http.StatusBadRequest, err)
		return
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	k, err := h.store.UpdateKey(c.Param("id"), in.Name, in.GroupID, in.RPM, in.MaxConcurrent, enabled)
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, k)
}

func (h *GatewayKeysHandler) DeleteKey(c *gin.Context) {
	if err := h.store.DeleteKey(c.Param("id")); err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (h *GatewayKeysHandler) RevealKey(c *gin.Context) {
	plain, err := h.store.RevealKey(c.Param("id"))
	if err != nil {
		h.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"key": plain})
}
