package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/config"
	"ai-web-free/internal/health"
	"ai-web-free/internal/provider"
	"ai-web-free/internal/proxy"
)

func TestPublicHealthAndAdminSystemStatus(t *testing.T) {
	acct := accounts.CreateAccount("tok-1", accounts.TypeFree, accounts.DefaultProfiles)
	acct.Status = accounts.StatusActive
	pool := accounts.NewPool([]*accounts.Account{acct})
	proxyPool := proxy.NewPool(nil)
	cfg := &config.Config{AdminToken: "secret-admin"}
	reg := provider.NewRegistry()
	provider.RegisterDefaultProviders(reg, provider.NewChatGPTProvider(pool, proxyPool, cfg, nil))

	tracker := health.NewTracker()
	tracker.RegisterService("account_health_check", "账号健康巡检", "scheduler", "10m0s", "每 10 分钟检查一次", true, true)
	tracker.RegisterService("kami_renew_worker", "卡密自动续期 Worker", "worker", "", "已禁用", false, false)

	router := RegisterRouterWithDeps(RouterDeps{
		AccountPool:   pool,
		ProxyPool:     proxyPool,
		Registry:      reg,
		Config:        cfg,
		HealthTracker: tracker,
	})

	// 1. /health 与 /healthz 无需 ADMIN_TOKEN
	for _, path := range []string{"/health", "/healthz"} {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("%s expected 200, got %d: %s", path, w.Code, w.Body.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
			t.Fatalf("%s invalid json: %v", path, err)
		}
		if payload["status"] != "healthy" {
			t.Fatalf("%s expected healthy status, got %v", path, payload["status"])
		}
		if int(payload["services_running"].(float64)) != 1 || int(payload["services_total"].(float64)) != 2 {
			t.Fatalf("%s unexpected service counts: %+v", path, payload)
		}
	}

	// 2. /admin/api/v1/system/status 受 ADMIN_TOKEN 保护
	wUnauth := httptest.NewRecorder()
	router.ServeHTTP(wUnauth, httptest.NewRequest(http.MethodGet, "/admin/api/v1/system/status", nil))
	if wUnauth.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without admin token, got %d", wUnauth.Code)
	}

	// 3. 携带有效 ADMIN_TOKEN 返回完整服务器状态报告
	reqAuth := httptest.NewRequest(http.MethodGet, "/admin/api/v1/system/status", nil)
	reqAuth.Header.Set("Authorization", "Bearer secret-admin")
	wAuth := httptest.NewRecorder()
	router.ServeHTTP(wAuth, reqAuth)
	if wAuth.Code != http.StatusOK {
		t.Fatalf("expected 200 with admin token, got %d: %s", wAuth.Code, wAuth.Body.String())
	}
	var report health.ServerStatusReport
	if err := json.Unmarshal(wAuth.Body.Bytes(), &report); err != nil {
		t.Fatalf("invalid system status json: %v", err)
	}
	if report.Status != "healthy" {
		t.Fatalf("expected healthy report, got %s", report.Status)
	}
	if report.Runtime.PID <= 0 || report.Runtime.Goroutines <= 0 {
		t.Fatalf("expected valid runtime metrics, got %+v", report.Runtime)
	}
	if len(report.BackgroundServices) != 2 {
		t.Fatalf("expected 2 background services, got %d", len(report.BackgroundServices))
	}
	if len(report.Subsystems) < 4 {
		t.Fatalf("expected subsystems list, got %d", len(report.Subsystems))
	}

	// 4. 当已启用的后台服务停止时，整体状态降级为 degraded
	tracker.MarkRunning("account_health_check", false, "巡检已停止")
	reqDegraded := httptest.NewRequest(http.MethodGet, "/admin/api/v1/system/status", nil)
	reqDegraded.Header.Set("Authorization", "Bearer secret-admin")
	wDegraded := httptest.NewRecorder()
	router.ServeHTTP(wDegraded, reqDegraded)
	if wDegraded.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", wDegraded.Code)
	}
	var degradedReport health.ServerStatusReport
	if err := json.Unmarshal(wDegraded.Body.Bytes(), &degradedReport); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	if degradedReport.Status != "degraded" || degradedReport.Summary.ServicesStopped != 1 {
		t.Fatalf("expected degraded status with 1 stopped service, got status=%s stopped=%d", degradedReport.Status, degradedReport.Summary.ServicesStopped)
	}
}
