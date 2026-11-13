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
	if !strings.Contains(w.Body.String(), "secret-pass") || !strings.Contains(w.Header().Get("Content-Disposition"), "aurora_sub2api.json") {
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
