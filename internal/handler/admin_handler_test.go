package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/audit"
	"ai-web-free/internal/chatgpt"
	"ai-web-free/internal/config"
	"ai-web-free/internal/promptplaza"
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

func TestAdminTasksInlineOverviewCancelDismissAndAsyncProbe(t *testing.T) {
	gin.SetMode(gin.TestMode)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"slug":"gpt-4o","title":"GPT-4o"}]}`))
	}))
	defer upstream.Close()

	origBaseURL := chatgpt.BaseURL
	chatgpt.BaseURL = upstream.URL + "/backend-api"
	defer func() { chatgpt.BaseURL = origBaseURL }()

	pool := accounts.NewPool(nil)
	makeKami := func(id, email string) *accounts.Account {
		a := accounts.CreateAccount("", accounts.TypeFree, accounts.DefaultProfiles)
		a.ID = id
		a.Email = email
		a.Password = "secret"
		a.TOTPSecret = "JBSWY3DPEHPK3PXP"
		a.Status = accounts.StatusExpired
		pool.AddAccount(a)
		return a
	}
	a1 := makeKami("adm-1", "one@example.com")
	a2 := makeKami("adm-2", "two@example.com")

	cfg := &config.Config{KamiAutoRenew: false}
	cg := provider.NewChatGPTProvider(pool, nil, cfg, nil)
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	cg.SetKamiLoginExecutor(func(targets []*accounts.Account, _ string, _ bool, onPhase provider.KamiPhaseReporter) (map[string]provider.KamiLoginOutcome, error) {
		out := make(map[string]provider.KamiLoginOutcome, len(targets))
		for _, acct := range targets {
			if onPhase != nil {
				onPhase(acct.ID, "wait_session_token", "提取会话 Token")
			}
			select {
			case started <- struct{}{}:
			default:
			}
			<-release
			out[strings.ToLower(acct.Email)] = provider.KamiLoginOutcome{
				Email:       acct.Email,
				OK:          true,
				AccessToken: "tok-" + acct.ID,
				ExpiresAt:   time.Now().Add(5 * 24 * time.Hour).Unix(),
			}
		}
		return out, nil
	})

	reg := provider.NewRegistry()
	provider.RegisterDefaultProviders(reg, cg)
	auditLog := audit.NewRingBuffer(50, "", 0)
	worker := provider.NewKamiRenewWorker(pool, "", true, nil, auditLog)
	cg.SetKamiWorker(worker)
	cg.SetAuditLog(auditLog)

	router := RegisterRouterWithDeps(RouterDeps{
		AccountPool: pool,
		Registry:    reg,
		Config:      cfg,
		KamiWorker:  worker,
		AuditLog:    auditLog,
	})

	// 1. Trigger single-account login via POST /accounts/:id/action with headless=false
	req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/providers/chatgpt/accounts/"+a1.ID+"/action", strings.NewReader(`{"action":"login","headless":false}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on single login action, got %d body=%s", w.Code, w.Body.String())
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for mock login")
	}

	// Queue second account dynamically while first is running
	req = httptest.NewRequest(http.MethodPost, "/admin/api/v1/providers/chatgpt/accounts/"+a2.ID+"/action", strings.NewReader(`{"action":"login","headless":true}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 queueing second account, got %d body=%s", w.Code, w.Body.String())
	}

	// 2. GET /overview & GET /accounts inline current login_task & phase
	req = httptest.NewRequest(http.MethodGet, "/admin/api/v1/overview", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var ov struct {
		LoginTask *provider.KamiLoginTask `json:"login_task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &ov); err != nil || ov.LoginTask == nil || ov.LoginTask.CurrentPhase != "wait_session_token" {
		t.Fatalf("expected inline login_task in overview with wait_session_token, got body=%s", w.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/admin/api/v1/providers/chatgpt/accounts", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var accResp struct {
		Accounts  []accounts.AccountSnapshot `json:"accounts"`
		LoginTask *provider.KamiLoginTask    `json:"login_task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &accResp); err != nil || accResp.LoginTask == nil || len(accResp.Accounts) != 2 {
		t.Fatalf("expected inline login_task & 2 accounts, got body=%s", w.Body.String())
	}

	// 3. Cancel queued a2 via POST .../kami-login/cancel
	req = httptest.NewRequest(http.MethodPost, "/admin/api/v1/providers/chatgpt/accounts/kami-login/cancel", strings.NewReader(`{"account_id":"`+a2.ID+`"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on cancel, got %d body=%s", w.Code, w.Body.String())
	}

	close(release)
	deadline := time.Now().Add(2 * time.Second)
	var taskID = ov.LoginTask.ID
	for time.Now().Before(deadline) {
		if snap, ok := cg.KamiLoginTaskSnapshot(taskID); ok && snap.Status == "completed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// 4. Dismiss completed task via POST .../tasks/dismiss
	req = httptest.NewRequest(http.MethodPost, "/admin/api/v1/providers/chatgpt/tasks/dismiss", strings.NewReader(`{"kind":"login","task_id":"`+taskID+`"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on dismiss, got %d body=%s", w.Code, w.Body.String())
	}
	if cg.ActiveKamiLoginTask() != nil {
		t.Fatalf("expected nil ActiveKamiLoginTask after HTTP dismiss")
	}

	// 5. Async probe-all returns 202 and transitions to completed
	req = httptest.NewRequest(http.MethodPost, "/admin/api/v1/providers/chatgpt/pool/probe-all?async=true", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202 on async probe-all, got %d body=%s", w.Code, w.Body.String())
	}
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pt := cg.ActiveProbeTask()
		if pt != nil && pt.Status == "completed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	pt := cg.ActiveProbeTask()
	if pt == nil || pt.Status != "completed" || pt.Probed != 2 {
		t.Fatalf("expected completed probe task for 2 accounts, got %+v", pt)
	}
}

func TestAdminPromptPlazaEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)

	mockCivitai := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{
					"id":     777,
					"url":    "https://cdn.example.test/civitai-777.jpg",
					"width":  1024,
					"height": 1024,
					"nsfw":   false,
					"stats":  map[string]int{"likeCount": 250, "heartCount": 90},
					"meta":   map[string]string{"prompt": "Minimalist glass coffee table with golden morning sunlight"},
				},
			},
		})
	}))
	defer mockCivitai.Close()

	mockEmpty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer mockEmpty.Close()

	plazaMgr := promptplaza.NewManager(promptplaza.Options{
		Endpoints: promptplaza.Endpoints{
			CivitaiURL:     mockCivitai.URL,
			LexicaURL:      mockEmpty.URL,
			RedditURL:      mockEmpty.URL,
			HuggingFaceURL: mockEmpty.URL,
			LexicaTopics:   []string{"glass"},
		},
		ChatExecutor: func(ctx context.Context, sys, user string, useWebSearch bool) (string, error) {
			if useWebSearch {
				return `[{"title_zh":"AI 趋势测试卡","category":"3D 与微缩","prompt_en":"Tiny glowing crystal cube with [主体] inside","prompt_zh":"内部封装微缩[主体]的发光晶体方块","recommended_mode":"generations","recommended_model":"gpt-image-2.5-sunburst","recommended_size":"1024x1024","variables":[{"key":"主体","label":"核心主体","default_value":"floating bonsai","options":["floating bonsai"]}]}]`, nil
			}
			if strings.Contains(user, "创意想法：") {
				return `{"title_zh":"霓虹猫咪咖啡馆","category":"摄影写实","prompt_en":"Cyberpunk ginger cat sipping espresso in neon rain, [光影材质]","prompt_zh":"霓虹雨夜喝咖啡的赛博朋克橘猫，[光影材质]","recommended_mode":"generations","recommended_model":"gpt-image-2.5-sunburst","recommended_size":"1792x1024","variables":[{"key":"光影材质","label":"氛围","default_value":"wet reflections","options":["wet reflections"]}]}`, nil
			}
			return `[]`, nil
		},
	})

	pool := accounts.NewPool(nil)
	proxyPool := proxy.NewPool(nil)
	cfg := &config.Config{AdminToken: "plaza-secret"}
	chatgptPlugin := provider.NewChatGPTProvider(pool, proxyPool, cfg, nil)
	reg := provider.NewRegistry()
	provider.RegisterDefaultProviders(reg, chatgptPlugin)

	router := RegisterRouterWithDeps(RouterDeps{
		AccountPool: pool,
		ProxyPool:   proxyPool,
		Registry:    reg,
		Config:      cfg,
		PromptPlaza: plazaMgr,
	})

	// 1. Unauthorized blocked
	req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/prompts", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without admin token, got %d", w.Code)
	}

	// 2. GET /prompts with token returns seeds + category stats
	req = httptest.NewRequest(http.MethodGet, "/admin/api/v1/prompts?category="+url.QueryEscape(promptplaza.Category3DMiniature), nil)
	req.Header.Set("X-Admin-Token", "plaza-secret")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on GET /prompts, got %d body=%s", w.Code, w.Body.String())
	}
	var listResp promptplaza.ListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal listResp: %v", err)
	}
	if len(listResp.Items) == 0 || listResp.Categories["all"] < 24 {
		t.Fatalf("expected seed items and total >= 24, got items=%d categories=%+v", len(listResp.Items), listResp.Categories)
	}

	// 3. POST /prompts/sync?wait=1 merges civitai & ai_search
	req = httptest.NewRequest(http.MethodPost, "/admin/api/v1/prompts/sync?wait=1", nil)
	req.Header.Set("X-Admin-Token", "plaza-secret")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on POST /prompts/sync, got %d body=%s", w.Code, w.Body.String())
	}
	req = httptest.NewRequest(http.MethodGet, "/admin/api/v1/prompts?source=civitai", nil)
	req.Header.Set("X-Admin-Token", "plaza-secret")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var civResp promptplaza.ListResponse
	_ = json.Unmarshal(w.Body.Bytes(), &civResp)
	if len(civResp.Items) != 1 || civResp.Items[0].ID != "civitai-777" {
		t.Fatalf("expected civitai-777 after sync, got %+v", civResp.Items)
	}

	// 4. POST /prompts/expand validates empty idea -> 400
	req = httptest.NewRequest(http.MethodPost, "/admin/api/v1/prompts/expand", strings.NewReader(`{"idea":"  "}`))
	req.Header.Set("X-Admin-Token", "plaza-secret")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 on blank idea, got %d", w.Code)
	}

	// 5. POST /prompts/expand returns AI expanded prompt
	req = httptest.NewRequest(http.MethodPost, "/admin/api/v1/prompts/expand", strings.NewReader(`{"idea":"赛博朋克猫咪喝咖啡","mode":"generations","size":"1792x1024"}`))
	req.Header.Set("X-Admin-Token", "plaza-secret")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on valid expand, got %d body=%s", w.Code, w.Body.String())
	}
	var expResp promptplaza.ExpandResponse
	if err := json.Unmarshal(w.Body.Bytes(), &expResp); err != nil {
		t.Fatalf("unmarshal expand response: %v", err)
	}
	if expResp.Source != "ai_pool" || expResp.TitleZH != "霓虹猫咪咖啡馆" {
		t.Fatalf("unexpected expand response: %+v", expResp)
	}
}

func TestAdminModelsCatalogCacheAndFallback(t *testing.T) {
	// 1. No logged-in account (only noauth) -> fallback catalog with builtin models
	noauthAcct := modelTestAccount(accounts.TypeNoAuth, &modelsTestClient{status: 200, body: `{"models":[]}`})
	emptyPool := accounts.NewPool([]*accounts.Account{noauthAcct})
	emptyReg := provider.NewRegistry()
	provider.RegisterDefaultProviders(emptyReg, provider.NewChatGPTProvider(emptyPool, proxy.NewPool(nil), &config.Config{}, nil))
	emptyRouter := RegisterRouterWithDeps(RouterDeps{
		AccountPool: emptyPool,
		ProxyPool:   proxy.NewPool(nil),
		Registry:    emptyReg,
		Config:      &config.Config{},
	})
	w := httptest.NewRecorder()
	emptyRouter.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/api/v1/models?include_builtins=true", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("fallback status = %d: %s", w.Code, w.Body.String())
	}
	var fallbackResp struct {
		Source        string `json:"source"`
		UpstreamCount int    `json:"upstream_count"`
		Data          []struct {
			ID     string `json:"id"`
			Source string `json:"source"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &fallbackResp); err != nil {
		t.Fatal(err)
	}
	if fallbackResp.Source != "fallback" || fallbackResp.UpstreamCount != 0 || len(fallbackResp.Data) < 10 {
		t.Fatalf("unexpected fallback response: %+v", fallbackResp)
	}

	// 2. Pool with active free account: first call fetches live, second reuses cache, refresh=true re-fetches
	client := &modelsTestClient{
		status: 200,
		body:   `{"models":[{"slug":"gpt-5-8"},{"slug":"gpt-5-6"}]}`,
	}
	freeAcct := modelTestAccount(accounts.TypeFree, client)
	pool := accounts.NewPool([]*accounts.Account{freeAcct})
	proxyPool := proxy.NewPool(nil)
	cfg := &config.Config{}
	chatgptPlugin := provider.NewChatGPTProvider(pool, proxyPool, cfg, nil)
	reg := provider.NewRegistry()
	provider.RegisterDefaultProviders(reg, chatgptPlugin)
	router := RegisterRouterWithDeps(RouterDeps{
		AccountPool: pool,
		ProxyPool:   proxyPool,
		Registry:    reg,
		Config:      cfg,
	})

	// First fetch -> live (calls=1)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/api/v1/models?include_builtins=true", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("first fetch status = %d: %s", w.Code, w.Body.String())
	}
	var liveResp struct {
		Source        string `json:"source"`
		CachedAt      string `json:"cached_at"`
		UpstreamCount int    `json:"upstream_count"`
		Data          []struct {
			ID     string `json:"id"`
			Source string `json:"source"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &liveResp); err != nil {
		t.Fatal(err)
	}
	if liveResp.Source != "live" || liveResp.UpstreamCount != 2 || client.calls != 1 || liveResp.CachedAt == "" {
		t.Fatalf("expected live catalog calls=1, got %+v calls=%d", liveResp, client.calls)
	}

	// Second fetch without refresh -> cached (calls remains 1)
	client.body = `{"models":[{"slug":"gpt-6-preview"}]}`
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/api/v1/models?include_builtins=true", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("cached fetch status = %d: %s", w.Code, w.Body.String())
	}
	var cacheResp struct {
		Source        string `json:"source"`
		UpstreamCount int    `json:"upstream_count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &cacheResp); err != nil {
		t.Fatal(err)
	}
	if cacheResp.Source != "cache" || client.calls != 1 || cacheResp.UpstreamCount != 2 {
		t.Fatalf("expected cached response calls=1, got %+v calls=%d", cacheResp, client.calls)
	}

	// Force refresh -> live again (calls=2) & merges previous models
	w = httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/admin/api/v1/models?include_builtins=true&refresh=true", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("refresh fetch status = %d: %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &liveResp); err != nil {
		t.Fatal(err)
	}
	if liveResp.Source != "live" || client.calls != 2 {
		t.Fatalf("expected refreshed live response calls=2, got %+v calls=%d", liveResp, client.calls)
	}
	cached, _ := pool.CachedModels()
	if len(cached) != 3 {
		t.Fatalf("expected merged pool cache of 3 slugs, got %v", cached)
	}
}
