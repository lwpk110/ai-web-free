package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ai-web-free/internal/accounts"
)

func TestBulkAccountActionDisableEnableAndDelete(t *testing.T) {
	router, pool, _, auditLog := setupTestAdminRouter(t, "", nil)

	acct2 := accounts.CreateAccount("test-access-token-2", accounts.TypeFree, accounts.DefaultProfiles)
	acct2.ID = "acc-admin-2"
	acct2.Email = "admin-test-2@example.com"
	acct2.Status = accounts.StatusActive
	pool.AddAccount(acct2)

	// 1. 批量停用 (disable)
	req := httptest.NewRequest(http.MethodPost,
		"/admin/api/v1/providers/chatgpt/accounts/bulk-action",
		strings.NewReader(`{"account_ids":["acc-admin-1","acc-admin-2"],"action":"disable"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var res struct {
		Action string `json:"action"`
		Result struct {
			Total   int `json:"total"`
			Success int `json:"success"`
			Failed  int `json:"failed"`
		} `json:"result"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if res.Result.Total != 2 || res.Result.Success != 2 {
		t.Fatalf("expected 2 success, got %+v", res.Result)
	}

	a1 := pool.GetByID("acc-admin-1")
	a2 := pool.GetByID("acc-admin-2")
	if a1 == nil || a1.Status != accounts.StatusDisabled || a2 == nil || a2.Status != accounts.StatusDisabled {
		t.Fatalf("expected accounts to be disabled, got a1=%+v, a2=%+v", a1, a2)
	}

	// 2. 批量启用 (enable)
	req = httptest.NewRequest(http.MethodPost,
		"/admin/api/v1/providers/chatgpt/accounts/bulk-action",
		strings.NewReader(`{"account_ids":["acc-admin-1","acc-admin-2"],"action":"enable"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if a1.Status != accounts.StatusActive || a2.Status != accounts.StatusActive {
		t.Fatalf("expected accounts to be active after enable, got a1=%+v, a2=%+v", a1, a2)
	}

	// 3. 批量删除 (delete)
	req = httptest.NewRequest(http.MethodPost,
		"/admin/api/v1/providers/chatgpt/accounts/bulk-action",
		strings.NewReader(`{"account_ids":["acc-admin-1"],"action":"delete"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if pool.GetByID("acc-admin-1") != nil {
		t.Fatalf("expected acc-admin-1 to be deleted")
	}

	// 4. 校验审计日志
	entries, _ := auditLog.Query("all", "/admin/api/v1/providers/chatgpt/accounts/bulk-action", 10)
	if len(entries) < 3 {
		t.Fatalf("expected at least 3 audit entries, got %d", len(entries))
	}
}

func TestBulkAccountActionValidation(t *testing.T) {
	router, _, _, _ := setupTestAdminRouter(t, "", nil)

	// 缺少 action
	req := httptest.NewRequest(http.MethodPost,
		"/admin/api/v1/providers/chatgpt/accounts/bulk-action",
		strings.NewReader(`{"account_ids":["acc-admin-1"]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing action, got %d", w.Code)
	}

	// 空 account_ids
	req = httptest.NewRequest(http.MethodPost,
		"/admin/api/v1/providers/chatgpt/accounts/bulk-action",
		strings.NewReader(`{"account_ids":[],"action":"disable"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty account_ids, got %d", w.Code)
	}

	// 未知 provider
	req = httptest.NewRequest(http.MethodPost,
		"/admin/api/v1/providers/unknown_provider/accounts/bulk-action",
		strings.NewReader(`{"account_ids":["id-1"],"action":"disable"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown provider, got %d", w.Code)
	}
}

func TestBulkAccountActionGrok(t *testing.T) {
	router, _, _, _ := setupTestAdminRouter(t, "", nil)

	// 先导入两个 Grok 账号
	req := httptest.NewRequest(http.MethodPost,
		"/admin/api/v1/providers/grok/accounts/import",
		strings.NewReader(`{"credential_type":"cookie","raw_text":"sso=token_grok1; sso-rw=rw1\nsso=token_grok2; sso-rw=rw2"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 importing grok accounts, got %d: %s", w.Code, w.Body.String())
	}

	// 列出 grok 账号以获取真实 ID
	req = httptest.NewRequest(http.MethodGet, "/admin/api/v1/providers/grok/accounts", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var accList struct {
		Accounts []struct {
			ID string `json:"id"`
		} `json:"accounts"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &accList)
	if len(accList.Accounts) < 2 {
		t.Fatalf("expected at least 2 grok accounts, got %d", len(accList.Accounts))
	}

	id1 := accList.Accounts[0].ID
	id2 := accList.Accounts[1].ID

	// 批量停用
	body := fmt.Sprintf(`{"account_ids":["%s","%s"],"action":"disable"}`, id1, id2)
	req = httptest.NewRequest(http.MethodPost, "/admin/api/v1/providers/grok/accounts/bulk-action", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// 批量删除
	bodyDel := fmt.Sprintf(`{"account_ids":["%s","%s"],"action":"delete"}`, id1, id2)
	req = httptest.NewRequest(http.MethodPost, "/admin/api/v1/providers/grok/accounts/bulk-action", strings.NewReader(bodyDel))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

