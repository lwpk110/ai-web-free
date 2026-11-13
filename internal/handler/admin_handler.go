package handler

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/audit"
	"ai-web-free/internal/config"
	"ai-web-free/internal/provider"
	"ai-web-free/internal/proxy"
	"ai-web-free/middlewares"

	"github.com/gin-gonic/gin"
)

type AdminHandler struct {
	registry  *provider.Registry
	proxyPool *proxy.Pool
	cfg       *config.Config
	runtime   *GatewayRuntime
	startedAt time.Time
}

func NewAdminHandler(reg *provider.Registry, proxyPool *proxy.Pool, cfg *config.Config) *AdminHandler {
	return &AdminHandler{
		registry:  reg,
		proxyPool: proxyPool,
		cfg:       cfg,
		startedAt: time.Now(),
	}
}

// SetRuntime 注入运维句柄所需的号池、队列与审计运行态。
func (h *AdminHandler) SetRuntime(rt *GatewayRuntime) {
	h.runtime = rt
}

func (h *AdminHandler) accountPool() *accounts.Pool {
	if h.runtime != nil {
		return h.runtime.AccountPool
	}
	return nil
}

func (h *AdminHandler) auditLog() *audit.RingBuffer {
	if h.runtime != nil {
		return h.runtime.AuditLog
	}
	return nil
}

func (h *AdminHandler) kamiWorker() *provider.KamiRenewWorker {
	if h.runtime != nil && h.runtime.KamiWorker != nil {
		return h.runtime.KamiWorker
	}
	if h.registry != nil {
		if p, ok := h.registry.Get("chatgpt"); ok {
			if cg, ok := p.(*provider.ChatGPTProvider); ok {
				return cg.KamiWorker()
			}
		}
	}
	return nil
}

func (h *AdminHandler) boundProxyCounts() map[string]int {
	if pool := h.accountPool(); pool != nil {
		return pool.BoundProxyCounts()
	}
	return map[string]int{}
}

func buildProxySummary(nodes []proxy.NodeStatus) gin.H {
	active := 0
	cooling := 0
	for _, n := range nodes {
		if n.Status == "cooling" {
			cooling++
		} else {
			active++
		}
	}
	return gin.H{
		"total":   len(nodes),
		"active":  active,
		"cooling": cooling,
	}
}

// Overview 返回网关全局总览、代理池健康摘要、卡密队列状态及供应商插件统计。
func (h *AdminHandler) Overview(c *gin.Context) {
	descriptors := h.registry.ListDescriptors()
	providerSummaries := make([]gin.H, 0, len(descriptors))
	for _, desc := range descriptors {
		p, _ := h.registry.Get(desc.ID)
		var stats interface{}
		if p != nil {
			stats = p.Stats()
		}
		providerSummaries = append(providerSummaries, gin.H{
			"descriptor": desc,
			"stats":      stats,
		})
	}

	var proxies []string
	var proxyNodes []proxy.NodeStatus
	if h.proxyPool != nil {
		proxies = h.proxyPool.List()
		proxyNodes = h.proxyPool.Snapshots(h.boundProxyCounts())
	} else {
		proxies = []string{}
		proxyNodes = []proxy.NodeStatus{}
	}

	kamiStats := provider.KamiWorkerStats{}
	if kw := h.kamiWorker(); kw != nil {
		kamiStats = kw.Stats()
	}
	_, auditSummary := h.auditLog().Query("all", "", 1)

	c.JSON(http.StatusOK, gin.H{
		"service":                "ai-web-free",
		"version":                "2.6.0",
		"uptime_seconds":         int64(time.Since(h.startedAt).Seconds()),
		"started_at":             h.startedAt.Format(time.RFC3339),
		"auth_enabled":           h.cfg != nil && h.cfg.Authorization != "",
		"admin_auth_required":    h.cfg != nil && h.cfg.AdminToken != "",
		"external_token_enabled": h.cfg != nil && h.cfg.EnableExternalToken,
		"stream_mode":            h.cfg != nil && h.cfg.StreamMode,
		"proxies_count":          len(proxies),
		"proxies":                proxies,
		"proxies_summary":        buildProxySummary(proxyNodes),
		"kami_worker":            kamiStats,
		"audit_summary":          auditSummary,
		"providers":              providerSummaries,
	})
}

// ListProviders 返回所有已注册供应商插件
func (h *AdminHandler) ListProviders(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"providers": h.registry.ListDescriptors(),
	})
}

// ProviderStats 返回指定供应商号池统计
func (h *AdminHandler) ProviderStats(c *gin.Context) {
	providerID := c.Param("provider")
	p, ok := h.registry.Get(providerID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "未找到供应商插件: " + providerID}})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"provider":   providerID,
		"descriptor": p.Descriptor(),
		"stats":      p.Stats(),
	})
}

// ListAccounts 返回指定供应商的账号池快照列表
func (h *AdminHandler) ListAccounts(c *gin.Context) {
	providerID := c.Param("provider")
	p, ok := h.registry.Get(providerID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "未找到供应商插件: " + providerID}})
		return
	}
	typeFilter := c.Query("type")
	statusFilter := c.Query("status")
	search := c.Query("q")

	list := p.ListAccounts(typeFilter, statusFilter, search)
	c.JSON(http.StatusOK, gin.H{
		"provider": providerID,
		"stats":    p.Stats(),
		"accounts": list,
	})
}

// ImportAccounts 批量导入账号凭据
func (h *AdminHandler) ImportAccounts(c *gin.Context) {
	providerID := c.Param("provider")
	p, ok := h.registry.Get(providerID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "未找到供应商插件: " + providerID}})
		return
	}
	var req provider.ImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "请求 JSON 格式无效: " + err.Error()}})
		return
	}
	res, err := p.ImportCredentials(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}, "result": res})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"provider": providerID,
		"result":   res,
		"stats":    p.Stats(),
	})
}

// AccountAction 对单个账号执行操作（probe / refresh / enable / disable / delete）
func (h *AdminHandler) AccountAction(c *gin.Context) {
	providerID := c.Param("provider")
	accountID := c.Param("id")
	p, ok := h.registry.Get(providerID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "未找到供应商插件: " + providerID}})
		return
	}
	var body struct {
		Action string `json:"action"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Action == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "缺少 action 参数"}})
		return
	}
	result, err := p.ExecuteAccountAction(accountID, body.Action)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"provider": providerID,
		"result":   result,
		"stats":    p.Stats(),
	})
}

// LoginKamiAccounts 批量对池内待激活/过期的卡密账号执行浏览器登录提取 Token
func (h *AdminHandler) LoginKamiAccounts(c *gin.Context) {
	providerID := c.Param("provider")
	p, ok := h.registry.Get(providerID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "未找到供应商插件: " + providerID}})
		return
	}
	type kamiRunner interface {
		LoginPendingKami(headless, force bool) (map[string]interface{}, error)
	}
	runner, ok := p.(kamiRunner)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "该供应商插件不支持卡密浏览器登录"}})
		return
	}
	var body struct {
		Headless *bool `json:"headless"`
		Force    bool  `json:"force"`
	}
	_ = c.ShouldBindJSON(&body)
	headless := true
	if body.Headless != nil {
		headless = *body.Headless
	}
	res, err := runner.LoginPendingKami(headless, body.Force)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error()}, "stats": p.Stats()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"provider": providerID,
		"result":   res,
		"stats":    p.Stats(),
	})
}

// ProbeAllAccounts 批量并发测活当前供应商的所有非免登账号
func (h *AdminHandler) ProbeAllAccounts(c *gin.Context) {
	providerID := c.Param("provider")
	p, ok := h.registry.Get(providerID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "未找到供应商插件: " + providerID}})
		return
	}

	snapshots := p.ListAccounts("free", "", "")
	puidSnaps := p.ListAccounts("puid", "", "")
	snapshots = append(snapshots, puidSnaps...)

	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var results []map[string]interface{}
	passed := 0
	failed := 0

	for _, snap := range snapshots {
		if snap.Status == "disabled" {
			continue
		}
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			res, err := p.ExecuteAccountAction(id, "probe")
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++
				results = append(results, map[string]interface{}{"id": id, "ok": false, "error": err.Error()})
				return
			}
			if okFlag, _ := res["ok"].(bool); okFlag {
				passed++
			} else {
				failed++
			}
			results = append(results, res)
		}(snap.ID)
	}
	wg.Wait()

	c.JSON(http.StatusOK, gin.H{
		"provider": providerID,
		"probed":   len(results),
		"passed":   passed,
		"failed":   failed,
		"results":  results,
		"stats":    p.Stats(),
	})
}

// UpdateProxies 动态更新代理池并校准失效绑定账号。
func (h *AdminHandler) UpdateProxies(c *gin.Context) {
	var body struct {
		RawText string   `json:"raw_text"`
		Proxies []string `json:"proxies"`
		Persist bool     `json:"persist"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}
	var list []string
	for _, p := range body.Proxies {
		if s := strings.TrimSpace(p); s != "" {
			list = append(list, s)
		}
	}
	if body.RawText != "" {
		for _, line := range strings.Split(body.RawText, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				list = append(list, line)
			}
		}
	}
	if h.proxyPool != nil {
		h.proxyPool.Replace(list)
	}
	migrated := 0
	if pool := h.accountPool(); pool != nil && h.proxyPool != nil {
		migrated = pool.ReconcileProxies(
			func(u string) bool { return !h.proxyPool.Contains(u) || !h.proxyPool.IsHealthy(u) },
			func(excl map[string]bool) string { return h.proxyPool.AllocateHealthy(excl) },
		)
	}
	if body.Persist {
		content := strings.Join(list, "\n")
		if len(list) > 0 {
			content += "\n"
		}
		_ = os.WriteFile("proxies.txt", []byte(content), 0644)
	}
	if logBuf := h.auditLog(); logBuf != nil {
		logBuf.RecordEvent("/admin/api/v1/proxies", "", "", fmt.Sprintf("updated_%d_migrated_%d", len(list), migrated), "")
	}
	c.JSON(http.StatusOK, gin.H{
		"proxies_count": len(list),
		"proxies":       list,
		"migrated":      migrated,
	})
}

// AuthStatus 返回 ADMIN_TOKEN 鉴权状态（自身免鉴权以便前端锁屏解锁流程）。
func (h *AdminHandler) AuthStatus(c *gin.Context) {
	expected := ""
	if h.cfg != nil {
		expected = strings.TrimSpace(h.cfg.AdminToken)
	}
	authRequired := expected != ""
	unlocked := !authRequired || middlewares.MatchAdminToken(expected, middlewares.ExtractAdminToken(c))
	c.JSON(http.StatusOK, gin.H{
		"auth_required": authRequired,
		"unlocked":      unlocked,
	})
}

// ListProxies 返回代理池节点健康矩阵（含每节点绑定账号数与熔断状态）。
func (h *AdminHandler) ListProxies(c *gin.Context) {
	if h.proxyPool == nil {
		c.JSON(http.StatusOK, gin.H{
			"summary": buildProxySummary(nil),
			"nodes":   []proxy.NodeStatus{},
		})
		return
	}
	nodes := h.proxyPool.Snapshots(h.boundProxyCounts())
	c.JSON(http.StatusOK, gin.H{
		"summary": buildProxySummary(nodes),
		"nodes":   nodes,
	})
}

// ProbeProxies 并发探测代理节点连通性与延迟并返回最新健康矩阵。
func (h *AdminHandler) ProbeProxies(c *gin.Context) {
	if h.proxyPool == nil {
		c.JSON(http.StatusOK, gin.H{
			"summary": buildProxySummary(nil),
			"nodes":   []proxy.NodeStatus{},
		})
		return
	}
	nodes := h.proxyPool.ProbeAll(c.Request.Context(), proxy.DefaultProbeFunc, h.boundProxyCounts())
	if pool := h.accountPool(); pool != nil {
		migrated := pool.ReconcileProxies(
			func(u string) bool { return !h.proxyPool.IsHealthy(u) },
			func(excl map[string]bool) string { return h.proxyPool.AllocateHealthy(excl) },
		)
		if migrated > 0 {
			nodes = h.proxyPool.Snapshots(h.boundProxyCounts())
		}
	}
	if logBuf := h.auditLog(); logBuf != nil {
		logBuf.RecordEvent("/admin/api/v1/proxies/probe", "", "", fmt.Sprintf("probed_%d", len(nodes)), "")
	}
	c.JSON(http.StatusOK, gin.H{
		"summary": buildProxySummary(nodes),
		"nodes":   nodes,
	})
}

// ListLogs 查询请求与号池运维环形审计日志。
func (h *AdminHandler) ListLogs(c *gin.Context) {
	filter := c.DefaultQuery("filter", "all")
	pathFilter := c.Query("path")
	limit := 100
	if rawLimit := strings.TrimSpace(c.Query("limit")); rawLimit != "" {
		if n, err := strconv.Atoi(rawLimit); err == nil && n > 0 {
			limit = n
		}
	}
	entries, summary := h.auditLog().Query(filter, pathFilter, limit)
	c.JSON(http.StatusOK, gin.H{
		"summary": summary,
		"entries": entries,
	})
}

// ExportAccounts 导出指定供应商账号池（支持 sub2api_json / access_tokens / kami，默认脱敏密钥）。
func (h *AdminHandler) ExportAccounts(c *gin.Context) {
	providerID := strings.ToLower(strings.TrimSpace(c.Param("provider")))
	if providerID != "chatgpt" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "当前仅支持导出 chatgpt 供应商号池"}})
		return
	}
	pool := h.accountPool()
	if pool == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "号池运行时未初始化"}})
		return
	}

	format := strings.ToLower(strings.TrimSpace(c.DefaultQuery("format", "sub2api_json")))
	includeSecrets := c.Query("include_secrets") == "true" || c.Query("include_secrets") == "1"
	download := c.Query("download") == "true" || c.Query("download") == "1"

	switch format {
	case "sub2api_json":
		bundle := accounts.BuildSub2APIJSON(pool.AllPersistentAccounts(), includeSecrets)
		if download {
			c.Header("Content-Disposition", `attachment; filename="aurora_sub2api.json"`)
		}
		c.JSON(http.StatusOK, bundle)

	case "access_tokens":
		tokens := pool.ExportAccessTokens()
		body := strings.Join(tokens, "\n")
		if len(tokens) > 0 {
			body += "\n"
		}
		if download {
			c.Header("Content-Disposition", `attachment; filename="access_tokens.txt"`)
		}
		c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(body))

	case "kami":
		kamiAccts := pool.ExportKamiAccounts()
		var body string
		if includeSecrets {
			body = accounts.FormatKamiLines(kamiAccts)
		} else {
			c.Header("X-Secrets-Redacted", "true")
			var b strings.Builder
			for _, ka := range kamiAccts {
				b.WriteString(ka.Email)
				b.WriteString("----******----******\n")
			}
			body = b.String()
		}
		if download {
			c.Header("Content-Disposition", `attachment; filename="kami_accounts.txt"`)
		}
		c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(body))

	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "不支持的导出格式: " + format}})
	}
}
