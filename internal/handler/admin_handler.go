package handler

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/audit"
	"ai-web-free/internal/chatgpt"
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

func (h *AdminHandler) providerAsChatGPT(providerID string) *provider.ChatGPTProvider {
	if h.registry == nil {
		return nil
	}
	if p, ok := h.registry.Get(providerID); ok {
		if cg, ok := p.(*provider.ChatGPTProvider); ok {
			return cg
		}
	}
	return nil
}

func (h *AdminHandler) chatgptProvider() *provider.ChatGPTProvider {
	return h.providerAsChatGPT("chatgpt")
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
	var activeLoginTask *provider.KamiLoginTask
	var activeProbeTask *provider.ProbeTask
	if cg := h.chatgptProvider(); cg != nil {
		activeLoginTask = cg.ActiveKamiLoginTask()
		activeProbeTask = cg.ActiveProbeTask()
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
		"login_task":             activeLoginTask,
		"probe_task":             activeProbeTask,
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
	resp := gin.H{
		"provider": providerID,
		"stats":    p.Stats(),
		"accounts": list,
	}
	if cg := h.providerAsChatGPT(providerID); cg != nil {
		resp["login_task"] = cg.ActiveKamiLoginTask()
		resp["probe_task"] = cg.ActiveProbeTask()
		if kw := cg.KamiWorker(); kw != nil {
			resp["kami_worker"] = kw.Stats()
		} else if kw := h.kamiWorker(); kw != nil {
			resp["kami_worker"] = kw.Stats()
		}
	}
	c.JSON(http.StatusOK, resp)
}

// BulkUpdateAccountProxy applies one proxy binding to a selected account set.
func (h *AdminHandler) BulkUpdateAccountProxy(c *gin.Context) {
	providerID := strings.ToLower(strings.TrimSpace(c.Param("provider")))
	if providerID != "chatgpt" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "当前仅支持 ChatGPT 号池代理编辑"}})
		return
	}
	providerPlugin, ok := h.registry.Get(providerID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "未找到供应商插件: " + providerID}})
		return
	}
	var body struct {
		AccountIDs []string `json:"account_ids"`
		ProxyURL   string   `json:"proxy_url"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "请求 JSON 格式无效: " + err.Error()}})
		return
	}
	ids := make([]string, 0, len(body.AccountIDs))
	seen := make(map[string]struct{}, len(body.AccountIDs))
	for _, id := range body.AccountIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 || len(ids) > 500 {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "请选择 1 到 500 个账号"}})
		return
	}
	proxyURL := strings.TrimSpace(body.ProxyURL)
	if proxyURL == "" && h.proxyPool != nil && len(h.proxyPool.List()) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "当前代理池已有节点，请选择一个健康代理用于账号登录"}})
		return
	}
	if proxyURL != "" {
		if h.proxyPool == nil || !h.proxyPool.Contains(proxyURL) {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "所选代理不在当前代理池中，请刷新代理列表"}})
			return
		}
		if !h.proxyPool.IsHealthy(proxyURL) {
			c.JSON(http.StatusConflict, gin.H{"error": gin.H{"message": "所选代理当前不可用，请选择健康节点"}})
			return
		}
	}
	pool := h.accountPool()
	if pool == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "号池运行时未初始化"}})
		return
	}

	type accountProxyResult struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		OK    bool   `json:"ok"`
		Error string `json:"error,omitempty"`
	}
	results := make([]accountProxyResult, 0, len(ids))
	updated := 0
	for _, id := range ids {
		acct := pool.GetByID(id)
		if acct == nil {
			results = append(results, accountProxyResult{ID: id, Error: "账号不存在"})
			continue
		}
		result := accountProxyResult{ID: acct.ID, Email: acct.Email}
		if err := pool.RebindAccountProxy(acct, proxyURL); err != nil {
			result.Error = "重建账号客户端失败: " + err.Error()
			log.Printf("[account-proxy] failed account=%s error=%v", acct.Email, err)
			if auditLog := h.auditLog(); auditLog != nil {
				auditLog.RecordEvent("/admin/api/v1/providers/"+providerID+"/accounts/bulk-proxy", acct.Email, "", "account_proxy_update_failed", result.Error)
			}
		} else {
			result.OK = true
			updated++
			log.Printf("[account-proxy] updated account=%s proxy_bound=%t", acct.Email, proxyURL != "")
			if auditLog := h.auditLog(); auditLog != nil {
				note := "account_proxy_set_direct"
				if proxyURL != "" {
					note = "account_proxy_set_bound"
				}
				auditLog.RecordEvent("/admin/api/v1/providers/"+providerID+"/accounts/bulk-proxy", acct.Email, "", note, "")
			}
		}
		results = append(results, result)
	}
	c.JSON(http.StatusOK, gin.H{
		"provider": providerID,
		"result": gin.H{
			"total":   len(ids),
			"updated": updated,
			"failed":  len(ids) - updated,
			"results": results,
		},
		"stats": providerPlugin.Stats(),
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

// AccountAction 对单个账号执行操作（probe / refresh / login / enable / disable / delete）
func (h *AdminHandler) AccountAction(c *gin.Context) {
	providerID := c.Param("provider")
	accountID := c.Param("id")
	p, ok := h.registry.Get(providerID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "未找到供应商插件: " + providerID}})
		return
	}
	var body struct {
		Action   string `json:"action"`
		Headless *bool  `json:"headless"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Action == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "缺少 action 参数"}})
		return
	}
	headless := true
	if body.Headless != nil {
		headless = *body.Headless
	}
	type actionWithOpts interface {
		ExecuteAccountActionWithOpts(id, action string, headless bool) (map[string]interface{}, error)
	}
	var result map[string]interface{}
	var err error
	if runner, ok := p.(actionWithOpts); ok {
		result, err = runner.ExecuteAccountActionWithOpts(accountID, body.Action, headless)
	} else {
		result, err = p.ExecuteAccountAction(accountID, body.Action)
	}
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
	type kamiLoginTaskRunner interface {
		StartKamiLoginBatch(ids []string, headless bool) (provider.KamiLoginTask, error)
		StartKamiPendingLogin(force, headless bool) (provider.KamiLoginTask, error)
		KamiLoginTaskSnapshot(id string) (provider.KamiLoginTask, bool)
	}
	runner, ok := p.(kamiLoginTaskRunner)
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "该供应商插件不支持卡密浏览器登录"}})
		return
	}
	var body struct {
		Headless   *bool    `json:"headless"`
		Force      bool     `json:"force"`
		AccountIDs []string `json:"account_ids"`
	}
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "请求 JSON 格式无效: " + err.Error()}})
			return
		}
	}
	headless := true
	if body.Headless != nil {
		headless = *body.Headless
	}
	var task provider.KamiLoginTask
	var err error
	if len(body.AccountIDs) > 0 {
		task, err = runner.StartKamiLoginBatch(body.AccountIDs, headless)
	} else {
		task, err = runner.StartKamiPendingLogin(body.Force, headless)
	}
	if err != nil {
		log.Printf("[kami-login] batch failed provider=%s error=%v", providerID, err)
		if auditLog := h.auditLog(); auditLog != nil {
			auditLog.RecordEvent("/admin/api/v1/providers/"+providerID+"/accounts/kami-login", "", "", "kami_login_batch_failed", err.Error())
		}
		status := http.StatusBadRequest
		if strings.Contains(err.Error(), "已有批量登录任务") {
			status = http.StatusConflict
		}
		c.JSON(status, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"provider": providerID, "task": task})
}

// KamiLoginTask 返回一个异步卡密批量登录任务的增量状态。
func (h *AdminHandler) KamiLoginTask(c *gin.Context) {
	providerID := c.Param("provider")
	p, ok := h.registry.Get(providerID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "未找到供应商插件: " + providerID}})
		return
	}
	taskRunner, ok := p.(interface {
		KamiLoginTaskSnapshot(id string) (provider.KamiLoginTask, bool)
	})
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "该供应商插件不支持异步卡密登录任务"}})
		return
	}
	task, ok := taskRunner.KamiLoginTaskSnapshot(c.Param("taskID"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "登录任务不存在或已过期"}})
		return
	}
	c.JSON(http.StatusOK, gin.H{"provider": providerID, "task": task})
}

func probeAllProviderAccounts(p provider.ProviderPlugin) (probed, passed, failed int, results []map[string]interface{}) {
	if runner, ok := p.(interface {
		StartOrRunProbeAll(bool) (provider.ProbeTask, error)
	}); ok {
		if task, err := runner.StartOrRunProbeAll(false); err == nil {
			return task.Probed, task.Passed, task.Failed, task.Results
		}
	}

	snapshots := p.ListAccounts("free", "", "")
	puidSnaps := p.ListAccounts("puid", "", "")
	snapshots = append(snapshots, puidSnaps...)

	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	var mu sync.Mutex

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
	return len(results), passed, failed, results
}

// CancelKamiLogin 从正在运行的重登或自动续期等待队列中取消尚未开始执行的账号。
func (h *AdminHandler) CancelKamiLogin(c *gin.Context) {
	providerID := c.Param("provider")
	p, ok := h.registry.Get(providerID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "未找到供应商插件: " + providerID}})
		return
	}
	cg := h.providerAsChatGPT(providerID)
	if cg == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "该供应商插件不支持队列取消"}})
		return
	}
	var body struct {
		TaskID    string `json:"task_id"`
		AccountID string `json:"account_id"`
		CancelAll bool   `json:"cancel_all"`
	}
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "请求 JSON 格式无效: " + err.Error()}})
			return
		}
	}

	task, count, err := cg.CancelKamiLogin(body.TaskID, body.AccountID, body.CancelAll)
	if err != nil && !body.CancelAll && strings.TrimSpace(body.AccountID) != "" {
		if kw := cg.KamiWorker(); kw != nil && kw.CancelQueued(strings.TrimSpace(body.AccountID)) {
			c.JSON(http.StatusOK, gin.H{
				"provider":  providerID,
				"cancelled": 1,
				"task":      cg.ActiveKamiLoginTask(),
				"stats":     p.Stats(),
			})
			return
		}
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"provider":  providerID,
		"cancelled": count,
		"task":      task,
		"stats":     p.Stats(),
	})
}

// DismissTask 标记已完成的可观察战报（重登或全池测活）为已关闭。
func (h *AdminHandler) DismissTask(c *gin.Context) {
	providerID := c.Param("provider")
	if _, ok := h.registry.Get(providerID); !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "未找到供应商插件: " + providerID}})
		return
	}
	cg := h.providerAsChatGPT(providerID)
	if cg == nil {
		c.JSON(http.StatusOK, gin.H{"ok": true, "dismissed": false})
		return
	}
	var body struct {
		Kind   string `json:"kind"`
		TaskID string `json:"task_id"`
	}
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "请求 JSON 格式无效: " + err.Error()}})
			return
		}
	}
	dismissed := false
	switch strings.ToLower(strings.TrimSpace(body.Kind)) {
	case "login":
		dismissed = cg.DismissKamiLoginTask(body.TaskID)
	case "probe":
		dismissed = cg.DismissProbeTask(body.TaskID)
	default:
		if cg.DismissKamiLoginTask(body.TaskID) {
			dismissed = true
		}
		if cg.DismissProbeTask(body.TaskID) {
			dismissed = true
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "dismissed": dismissed})
}

// ProbeAllAccounts 批量并发测活当前供应商的所有非免登账号（支持 ?async=true 后台异步追踪进度）
func (h *AdminHandler) ProbeAllAccounts(c *gin.Context) {
	providerID := c.Param("provider")
	p, ok := h.registry.Get(providerID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "未找到供应商插件: " + providerID}})
		return
	}

	async := c.Query("async") == "true" || c.Query("async") == "1"
	if runner, ok := p.(interface {
		StartOrRunProbeAll(bool) (provider.ProbeTask, error)
	}); ok {
		task, err := runner.StartOrRunProbeAll(async)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}})
			return
		}
		status := http.StatusOK
		if async {
			status = http.StatusAccepted
		}
		c.JSON(status, gin.H{
			"provider": providerID,
			"task":     task,
			"probed":   task.Probed,
			"passed":   task.Passed,
			"failed":   task.Failed,
			"results":  task.Results,
			"stats":    p.Stats(),
		})
		return
	}

	probed, passed, failed, results := probeAllProviderAccounts(p)

	c.JSON(http.StatusOK, gin.H{
		"provider": providerID,
		"probed":   probed,
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
			c.Header("Content-Disposition", `attachment; filename="ai_web_free_sub2api.json"`)
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

func respondAdminModelsCatalog(
	c *gin.Context,
	upstreamSlugs []string,
	source string,
	cachedAt time.Time,
	warning string,
	includeBuiltins bool,
) {
	seen := make(map[string]bool, len(upstreamSlugs)+20)
	data := make([]gin.H, 0, len(upstreamSlugs)+20)
	upstreamCount := 0
	for _, raw := range upstreamSlugs {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		upstreamCount++
		data = append(data, gin.H{
			"id":       id,
			"object":   "model",
			"owned_by": "openai",
			"created":  0,
			"source":   "upstream",
		})
	}
	if source == "fallback" && upstreamCount == 0 {
		for _, id := range []string{"gpt-5-6", "gpt-5-3", "gpt-5-mini", "gpt-4o", "gpt-4o-mini"} {
			if !seen[id] {
				seen[id] = true
				data = append(data, gin.H{
					"id":       id,
					"object":   "model",
					"owned_by": "openai",
					"created":  0,
					"source":   "builtin",
				})
			}
		}
	}
	if includeBuiltins {
		for _, id := range BuiltinModelIDs() {
			if !seen[id] {
				seen[id] = true
				data = append(data, gin.H{
					"id":       id,
					"object":   "model",
					"owned_by": "openai",
					"created":  0,
					"source":   "builtin",
				})
			}
		}
	}

	payload := gin.H{
		"object":         "list",
		"source":         source,
		"upstream_count": upstreamCount,
		"data":           data,
	}
	if !cachedAt.IsZero() {
		payload["cached_at"] = cachedAt.Format(time.RFC3339)
	}
	if strings.TrimSpace(warning) != "" {
		payload["warning"] = warning
	}
	c.JSON(http.StatusOK, payload)
}

// ListModelsCatalog 返回动态获取并池级缓存的模型目录（支持 ?refresh=true 强制重新拉取）。
func (h *AdminHandler) ListModelsCatalog(c *gin.Context) {
	forceRefresh := c.Query("refresh") == "true" || c.Query("refresh") == "1"
	includeBuiltins := c.DefaultQuery("include_builtins", "true") != "false" && c.DefaultQuery("include_builtins", "true") != "0"
	pool := h.accountPool()

	if !forceRefresh && pool != nil {
		if cached, cachedAt := pool.CachedModels(); len(cached) > 0 {
			respondAdminModelsCatalog(c, cached, "cache", cachedAt, "", includeBuiltins)
			return
		}
	}

	var lastErr string
	if pool != nil {
		const maxProbeAccounts = 3
		attempts := 0
		excluded := make(map[string]bool)
		for _, kind := range []accounts.AccountType{accounts.TypeFree, accounts.TypePUID} {
			for attempts < maxProbeAccounts {
				acct, err := pool.AcquireForCapability(kind, "", excluded)
				if err != nil || acct == nil {
					break
				}
				excluded[acct.ID] = true
				attempts++
				if acct.Client == nil {
					_ = acct.InitClient()
				}
				models, status, listErr := chatgpt.ListModels(acct.Client, acct)
				if listErr == nil {
					slugs := make([]string, 0, len(models))
					seen := make(map[string]bool, len(models))
					for _, m := range models {
						id := strings.TrimSpace(m.Slug)
						if id != "" && !seen[id] {
							seen[id] = true
							slugs = append(slugs, id)
						}
					}
					if len(slugs) > 0 {
						pool.RecordAccountModels(acct.ID, slugs)
						respondAdminModelsCatalog(c, slugs, "live", time.Now(), "", includeBuiltins)
						return
					}
					lastErr = "上游返回空模型列表"
					continue
				}
				lastErr = fmt.Sprintf("HTTP %d: %v", status, listErr)
				if status == http.StatusUnauthorized {
					pool.ReportFailure(acct)
				}
			}
		}
		if cached, cachedAt := pool.CachedModels(); len(cached) > 0 {
			respondAdminModelsCatalog(c, cached, "cache", cachedAt, lastErr, includeBuiltins)
			return
		}
	}

	if lastErr == "" {
		lastErr = "当前无可用登录态账号，已使用内置参考目录"
	}
	respondAdminModelsCatalog(c, nil, "fallback", time.Time{}, lastErr, includeBuiltins)
}
