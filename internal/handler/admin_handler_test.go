package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/audit"
	"ai-web-free/internal/chatgpt"
	"ai-web-free/internal/config"
	"ai-web-free/internal/provider"
	"ai-web-free/internal/proxy"

	"github.com/gin-gonic/gin"
)

func setupTestAdminRouter(t *testing.T, adminToken string, proxies []string) (*gin.Engine, *accounts.Pool, *proxy.Pool, *audit.RingBuffer) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	origBaseURL := chatgpt.BaseURL
	dplSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><script src="https://cdn.oaistatic.com/test?dpl=test-dpl"></script></html>`))
	}))
	chatgpt.BaseURL = dplSrv.URL + "/backend-api"
	t.Cleanup(func() {
		chatgpt.BaseURL = origBaseURL
		dplSrv.Close()
	})

	pool := accounts.NewPool(nil)
	acct := accounts.CreateAccount("test-access-token", accounts.TypeFree, accounts.DefaultProfiles)
	acct.ID = "acc-admin-1"
	acct.Email = "admin-test@example.com"
	acct.Password = "secret-pass"
	acct.TOTPSecret = "JBSWY3DPEHPK3PXP"
	acct.Status = accounts.StatusActive
	if len(proxies) > 0 {
		acct.Proxy = proxies[0]
	}
	pool.AddAccount(acct)

	proxyPool := proxy.NewPool(proxies)
	cfg := &config.Config{
		AdminToken:    adminToken,
		KamiAutoRenew: false,
	}
	chatgptPlugin := provider.NewChatGPTProvider(pool, proxyPool, cfg, nil)
	reg := provider.NewRegistry()
	provider.RegisterDefaultProviders(reg, chatgptPlugin)

	auditLog := audit.NewRingBuffer(100, "", 0)
	worker := provider.NewKamiRenewWorker(pool, "", true, nil, auditLog)
	chatgptPlugin.SetAuditLog(auditLog)
	chatgptPlugin.SetKamiWorker(worker)

	router := RegisterRouterWithDeps(RouterDeps{
		AccountPool: pool,
		ProxyPool:   proxyPool,
		Registry:    reg,
		Config:      cfg,
		KamiWorker:  worker,
		AuditLog:    auditLog,
	})
	return router, pool, proxyPool, auditLog
}

func TestAdminAuthStatusAndOverviewProtection(t *testing.T) {
	router, _, _, _ := setupTestAdminRouter(t, "top-secret-token", []string{"http://127.0.0.1:8888"})

	// 1. auth/status always accessible
	req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/auth/status", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on auth/status, got %d", w.Code)
	}
	var statusResp map[string]bool
	if err := json.Unmarshal(w.Body.Bytes(), &statusResp); err != nil {
		t.Fatalf("unmarshal auth/status: %v", err)
	}
	if !statusResp["auth_required"] || statusResp["unlocked"] {
		t.Fatalf("expected auth_required=true unlocked=false, got %+v", statusResp)
	}

	// 2. overview blocked without token
	req = httptest.NewRequest(http.MethodGet, "/admin/api/v1/overview", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 on protected overview without token, got %d", w.Code)
	}

	// 3. auth/status reports unlocked=true with valid X-Admin-Token
	req = httptest.NewRequest(http.MethodGet, "/admin/api/v1/auth/status", nil)
	req.Header.Set("X-Admin-Token", "top-secret-token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	statusResp = map[string]bool{}
	_ = json.Unmarshal(w.Body.Bytes(), &statusResp)
	if !statusResp["auth_required"] || !statusResp["unlocked"] {
		t.Fatalf("expected unlocked=true with valid header, got %+v", statusResp)
	}

	// 4. overview returns enriched resilience fields when authorized
	req = httptest.NewRequest(http.MethodGet, "/admin/api/v1/overview", nil)
	req.Header.Set("X-Admin-Token", "top-secret-token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on authorized overview, got %d: %s", w.Code, w.Body.String())
	}
	var ov map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &ov); err != nil {
		t.Fatalf("unmarshal overview: %v", err)
	}
	for _, key := range []string{"admin_auth_required", "proxies_summary", "kami_worker", "audit_summary"} {
		if _, ok := ov[key]; !ok {
			t.Fatalf("expected overview key %q, got keys %+v", key, ov)
		}
	}
}

func TestBulkUpdateAccountProxyRebindsSelectedAccounts(t *testing.T) {
	router, pool, _, auditLog := setupTestAdminRouter(t, "", []string{
		"http://127.0.0.1:8888",
		"http://127.0.0.1:8889",
	})

	req := httptest.NewRequest(http.MethodPost,
		"/admin/api/v1/providers/chatgpt/accounts/bulk-proxy",
		strings.NewReader(`{"account_ids":["acc-admin-1"],"proxy_url":"http://127.0.0.1:8889"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	acct := pool.GetByID("acc-admin-1")
	if acct == nil || acct.Proxy != "http://127.0.0.1:8889" {
		t.Fatalf("expected selected account to bind requested proxy, got %+v", acct)
	}
	entries, _ := auditLog.Query("all", "/admin/api/v1/providers/chatgpt/accounts/bulk-proxy", 10)
	if len(entries) != 1 || entries[0].Note != "account_proxy_set_bound" {
		t.Fatalf("expected proxy update audit event, got %+v", entries)
	}

	req = httptest.NewRequest(http.MethodPost,
		"/admin/api/v1/providers/chatgpt/accounts/bulk-proxy",
		strings.NewReader(`{"account_ids":["acc-admin-1"],"proxy_url":""}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected direct egress to be rejected while proxy nodes exist, got %d", w.Code)
	}
}

func TestKamiLoginEndpointReturnsPollableAsyncTask(t *testing.T) {
	router, _, _, _ := setupTestAdminRouter(t, "", nil)
	req := httptest.NewRequest(http.MethodPost,
		"/admin/api/v1/providers/chatgpt/accounts/kami-login",
		strings.NewReader(`{"headless":true,"account_ids":["missing-account-id"]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202 for queued task, got %d: %s", w.Code, w.Body.String())
	}
	var startResp struct {
		Task provider.KamiLoginTask `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &startResp); err != nil {
		t.Fatalf("unmarshal task start response: %v", err)
	}
	if startResp.Task.ID == "" || startResp.Task.Total != 1 {
		t.Fatalf("unexpected task start snapshot: %+v", startResp.Task)
	}

	deadline := time.Now().Add(time.Second)
	var task provider.KamiLoginTask
	for time.Now().Before(deadline) {
		req = httptest.NewRequest(http.MethodGet,
			"/admin/api/v1/providers/chatgpt/accounts/kami-login/tasks/"+startResp.Task.ID, nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 while polling task, got %d: %s", w.Code, w.Body.String())
		}
		var pollResp struct {
			Task provider.KamiLoginTask `json:"task"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &pollResp); err != nil {
			t.Fatalf("unmarshal task poll response: %v", err)
		}
		task = pollResp.Task
		if task.Status == "completed" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if task.Status != "completed" || task.Failed != 1 || len(task.Results) != 1 || task.Results[0].Error != "账号不存在" {
		t.Fatalf("unexpected async task result: %+v", task)
	}
}

func TestAdminProxiesProbeAndReconcile(t *testing.T) {
	// 启动一个本地 mock 代理端口，使 TCP 拨号立即响应、CONNECT 快速返回错误而不阻塞等待上游超时
	mockProxySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer mockProxySrv.Close()

	oldProxy := "http://127.0.0.1:1"
	router, pool, _, auditLog := setupTestAdminRouter(t, "", []string{oldProxy})

	// 1. GET /proxies
	req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/proxies", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on GET /proxies, got %d", w.Code)
	}
	var listResp struct {
		Summary struct {
			Total int `json:"total"`
		} `json:"summary"`
		Nodes []struct {
			URL           string `json:"url"`
			BoundAccounts int    `json:"bound_accounts"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal GET /proxies: %v", err)
	}
	if listResp.Summary.Total != 1 || len(listResp.Nodes) != 1 || listResp.Nodes[0].BoundAccounts != 1 {
		t.Fatalf("unexpected proxy list payload: %+v", listResp)
	}

	// 2. POST /proxies replaces oldProxy with mockProxySrv.URL and migrates bound account
	bodyBytes, _ := json.Marshal(map[string]any{
		"proxies": []string{mockProxySrv.URL},
		"persist": false,
	})
	req = httptest.NewRequest(http.MethodPost, "/admin/api/v1/proxies", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on POST /proxies, got %d: %s", w.Code, w.Body.String())
	}
	var updResp struct {
		ProxiesCount int `json:"proxies_count"`
		Migrated     int `json:"migrated"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &updResp)
	if updResp.ProxiesCount != 1 || updResp.Migrated != 1 {
		t.Fatalf("expected proxies_count=1 migrated=1, got %+v", updResp)
	}
	accts := pool.AllPersistentAccounts()
	if len(accts) != 1 || accts[0].Proxy != mockProxySrv.URL {
		t.Fatalf("expected account migrated to %s, got %v", mockProxySrv.URL, accts[0].Proxy)
	}

	// 3. POST /proxies/probe runs probe
	req = httptest.NewRequest(http.MethodPost, "/admin/api/v1/proxies/probe", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on POST /proxies/probe, got %d: %s", w.Code, w.Body.String())
	}

	// 4. GET /logs should reflect proxy update event
	if _, summary := auditLog.Query("all", "", 1); summary.TotalEvents == 0 {
		t.Fatalf("expected proxy update recorded in auditLog")
	}
}

func TestAdminLogsFilterAndAccountsExportRedaction(t *testing.T) {
	router, _, _, auditLog := setupTestAdminRouter(t, "", nil)

	auditLog.Record(audit.Entry{
		Kind:          "request",
		Method:        "POST",
		Path:          "/v1/images/generations",
		Model:         "gpt-image-1",
		Status:        200,
		AttemptCount:  2,
		FailoverTrace: "acc-1:429 -> acc-admin-1:200",
	})
	auditLog.Record(audit.Entry{
		Kind:   "request",
		Method: "POST",
		Path:   "/v1/chat/completions",
		Model:  "gpt-4o",
		Status: 502,
		Error:  "upstream failed",
	})
	auditLog.RecordEvent("/admin/api/v1/test", "acc-admin-1", "admin-test@example.com", "probe_ok", "note")

	// Filter failover
	req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/logs?filter=failover&limit=10", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on /logs, got %d", w.Code)
	}
	var logsResp struct {
		Entries []audit.Entry `json:"entries"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &logsResp); err != nil {
		t.Fatalf("unmarshal /logs: %v", err)
	}
	if len(logsResp.Entries) != 1 || logsResp.Entries[0].Path != "/v1/images/generations" {
		t.Fatalf("expected 1 failover entry, got %+v", logsResp.Entries)
	}

	// Export sub2api_json redacted by default
	req = httptest.NewRequest(http.MethodGet, "/admin/api/v1/providers/chatgpt/export?format=sub2api_json", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on export sub2api_json, got %d", w.Code)
	}
	if strings.Contains(w.Body.String(), "secret-pass") || strings.Contains(w.Body.String(), "JBSWY3DPEHPK3PXP") {
		t.Fatalf("expected secrets redacted when include_secrets=false, got %s", w.Body.String())
	}

	// Export sub2api_json with include_secrets=true
	req = httptest.NewRequest(http.MethodGet, "/admin/api/v1/providers/chatgpt/export?format=sub2api_json&include_secrets=true&download=true", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on export with secrets, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "secret-pass") || !strings.Contains(w.Header().Get("Content-Disposition"), "ai_web_free_sub2api.json") {
		t.Fatalf("expected secrets and attachment header, got body=%s disp=%s", w.Body.String(), w.Header().Get("Content-Disposition"))
	}

	// Export kami redacted vs unredacted
	req = httptest.NewRequest(http.MethodGet, "/admin/api/v1/providers/chatgpt/export?format=kami", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK || w.Header().Get("X-Secrets-Redacted") != "true" {
		t.Fatalf("expected 200 + X-Secrets-Redacted on kami export, got %d hdr=%s", w.Code, w.Header().Get("X-Secrets-Redacted"))
	}
	if strings.Contains(w.Body.String(), "secret-pass") {
		t.Fatalf("expected kami export redacted by default, got %s", w.Body.String())
	}

	// Export access_tokens
	req = httptest.NewRequest(http.MethodGet, "/admin/api/v1/providers/chatgpt/export?format=access_tokens", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "test-access-token") {
		t.Fatalf("expected access_tokens output, got %d %s", w.Code, w.Body.String())
	}
}

func TestAdminDiagnosticsAndRepair(t *testing.T) {
	router, pool, pxPool, auditLog := setupTestAdminRouter(t, "", []string{"http://127.0.0.1:18081", "http://127.0.0.1:18082"})

	// 将初始账号标为永久封禁，并添加 403 WAF 与 401 过期账号，制造 active == 0 故障场景
	for _, a := range pool.AllPersistentAccounts() {
		pool.MarkBanned(a, "HTTP 403: account_deactivated")
	}

	wafAcct := accounts.CreateAccount("eyJ.waf.tok", accounts.TypeFree, accounts.DefaultProfiles)
	wafAcct.ID = "acc-waf-2"
	wafAcct.Email = "waf-2@example.com"
	wafAcct.Proxy = "http://127.0.0.1:18081"
	wafAcct.Status = accounts.StatusActive
	pool.AddAccount(wafAcct)
	pool.RecordProxyBlock(wafAcct, 2*time.Minute, "HTTP 403: Just a moment Cloudflare cf-ray")
	pxPool.ReportFailure("http://127.0.0.1:18081", "HTTP 403 WAF")

	expAcct := accounts.CreateAccount("eyJ.exp.tok", accounts.TypeFree, accounts.DefaultProfiles)
	expAcct.ID = "acc-exp-3"
	expAcct.Email = "exp-3@example.com"
	expAcct.Status = accounts.StatusActive
	pool.AddAccount(expAcct)
	pool.MarkExpired(expAcct, "HTTP 401: token_expired")

	auditLog.RecordEvent("/v1/chat/completions", "waf-2@example.com", "http://127.0.0.1:18081", "waf_403", "Cloudflare challenge")

	// 1. 查询全平台诊断报告
	req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/diagnostics", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on diagnostics, got %d body=%s", w.Code, w.Body.String())
	}
	var diag DiagnosticsReport
	if err := json.Unmarshal(w.Body.Bytes(), &diag); err != nil {
		t.Fatalf("unmarshal diagnostics: %v", err)
	}
	if diag.OverallSeverity != "critical" || diag.HealthScore > 20 {
		t.Fatalf("expected critical severity & low health score when active==0, got sev=%s score=%d", diag.OverallSeverity, diag.HealthScore)
	}
	if !strings.Contains(diag.MarkdownReport, "平台全链路诊断报告") {
		t.Fatalf("expected non-empty chinese markdown_report, got %s", diag.MarkdownReport)
	}
	seenCats := map[string]bool{}
	for _, rc := range diag.RootCauses {
		seenCats[rc.Category] = true
	}
	for _, reqCat := range []string{"banned", "waf_proxy_block", "token_expired"} {
		if !seenCats[reqCat] {
			t.Fatalf("expected root cause category %s in report, got %+v", reqCat, diag.RootCauses)
		}
	}

	// 2. 删除封禁账号缺少 confirm=true 必须拒绝 (400)
	req = httptest.NewRequest(http.MethodPost, "/admin/api/v1/diagnostics/repair", strings.NewReader(`{"action":"delete_banned","confirm":false}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when delete_banned without confirm, got %d body=%s", w.Code, w.Body.String())
	}

	// 3. confirm=true 后允许清理封禁账号
	req = httptest.NewRequest(http.MethodPost, "/admin/api/v1/diagnostics/repair", strings.NewReader(`{"action":"delete_banned","confirm":true}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on confirmed delete_banned, got %d body=%s", w.Code, w.Body.String())
	}
	if pool.Stats().Banned != 0 {
		t.Fatalf("expected banned count 0 after deletion, got %d", pool.Stats().Banned)
	}

	// 4. 在 0 可用账号状态下执行 smart_heal 应自动补充匿名 UUID 恢复可用态
	req = httptest.NewRequest(http.MethodPost, "/admin/api/v1/diagnostics/repair", strings.NewReader(`{"action":"smart_heal"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on smart_heal, got %d body=%s", w.Code, w.Body.String())
	}
	var repairResp struct {
		OK          bool              `json:"ok"`
		Details     map[string]int    `json:"details"`
		Diagnostics DiagnosticsReport `json:"diagnostics"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &repairResp); err != nil {
		t.Fatalf("unmarshal smart_heal resp: %v", err)
	}
	if !repairResp.OK || repairResp.Diagnostics.PoolStats.Active < 3 {
		t.Fatalf("expected smart_heal to restore >=3 active accounts via emergency free_uuid, got active=%d resp=%s",
			repairResp.Diagnostics.PoolStats.Active, w.Body.String())
	}
}
