package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-web-free/internal/accounts"
)

func TestGrokProviderImportAndStats(t *testing.T) {
	p := NewGrokProvider(nil, "https://grok.com", "", accounts.PoolPolicy{})

	tokens := []string{
		"sso=token_1; sso-rw=rw_1",
		"user2@example.com----sso=token_2; sso-rw=rw_2",
		"sso=token_1; sso-rw=rw_1", // 重复项
	}
	added := p.AddAccounts(tokens, false)
	if added != 2 {
		t.Fatalf("expected 2 added, got %d", added)
	}

	stats := p.Stats()
	if stats.Total != 2 || stats.Active != 2 {
		t.Fatalf("stats mismatch: total=%d active=%d", stats.Total, stats.Active)
	}

	accountsList := p.ListAccounts("", "", "")
	if len(accountsList) != 2 {
		t.Fatalf("expected 2 accounts, got %d", len(accountsList))
	}
	if accountsList[1].Email != "user2@example.com" {
		t.Fatalf("expected email user2@example.com, got %s", accountsList[1].Email)
	}
}

func TestGrokRefreshModelsFetchesLiveCatalog(t *testing.T) {
	var requests []struct {
		Locale string `json:"locale"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/rest/models" {
			t.Errorf("unexpected catalog request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var payload struct {
			Locale string `json:"locale"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		requests = append(requests, payload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
			"models": [
				{"modelId": "grok-live-4"},
				{"modelId": "grok-live-4"},
				{"modelId": ""}
			],
			"defaultFreeModel": "grok-live-default"
		}`)
	}))
	defer server.Close()

	provider := NewGrokProvider(nil, server.URL, "", accounts.PoolPolicy{})
	models, err := provider.RefreshModels(context.Background())
	if err != nil {
		t.Fatalf("refresh models: %v", err)
	}
	if len(models) != 1 || models[0] != "grok-live-4" {
		t.Fatalf("unexpected live models: %v", models)
	}
	if len(requests) != 1 || requests[0].Locale != "en-US" {
		t.Fatalf("unexpected catalog requests: %+v", requests)
	}
	provider.modelMu.RLock()
	defaultModel := provider.defaultModel
	provider.modelMu.RUnlock()
	if defaultModel != "grok-live-default" {
		t.Fatalf("unexpected default model: %q", defaultModel)
	}
	if cached := provider.CachedModels(); len(cached) != 1 || cached[0] != "grok-live-4" {
		t.Fatalf("unexpected cached models: %v", cached)
	}
	if provider.ModelsCachedAt().IsZero() {
		t.Fatal("expected model cache timestamp")
	}
	if featured := provider.Descriptor().FeaturedModels; len(featured) != 1 || featured[0] != "grok-live-4" {
		t.Fatalf("unexpected descriptor models: %v", featured)
	}
}

func TestGrokProviderRebalanceAndBackfill(t *testing.T) {
	pol := accounts.PoolPolicy{
		ActiveTarget:  3,
		MinHealthy:    2,
		MaxActive:     5,
		ActivateBatch: 3,
	}
	p := NewGrokProvider(nil, "https://grok.com", "", pol)

	var tokens []string
	for i := 1; i <= 10; i++ {
		tokens = append(tokens, fmt.Sprintf("u%d@example.com----sso=token_%d", i, i))
	}
	p.AddAccounts(tokens, false)

	// 调谐后应只有 3 个 active，其余 7 个 standby
	stats := p.Stats()
	if stats.Active != 3 {
		t.Fatalf("expected 3 active accounts, got %d", stats.Active)
	}
	if stats.Standby != 7 {
		t.Fatalf("expected 7 standby accounts, got %d", stats.Standby)
	}

	// 模拟当前活跃账号全部由于 429 冷却或封禁
	p.mu.Lock()
	for _, a := range p.accounts {
		if a.Status == accounts.StatusActive {
			a.Status = accounts.StatusBanned
		}
	}
	p.mu.Unlock()

	// 再次触发调谐，应自动从待命池补位 3 个
	results := p.Rebalance()
	if len(results) == 0 || results[0].Activated != 3 {
		t.Fatalf("expected 3 accounts activated, got %+v", results)
	}

	statsAfter := p.Stats()
	if statsAfter.Active != 3 {
		t.Fatalf("expected 3 active accounts after rebalance, got %d", statsAfter.Active)
	}
	if statsAfter.Banned != 3 {
		t.Fatalf("expected 3 banned accounts, got %d", statsAfter.Banned)
	}
	if statsAfter.Standby != 4 {
		t.Fatalf("expected 4 standby accounts left, got %d", statsAfter.Standby)
	}
}

func TestGrokProviderAccountActions(t *testing.T) {
	p := NewGrokProvider(nil, "https://grok.com", "", accounts.PoolPolicy{})
	p.AddAccounts([]string{"sso=action_token; sso-rw=action_rw"}, false)

	list := p.ListAccounts("", "", "")
	if len(list) != 1 {
		t.Fatalf("expected 1 account, got %d", len(list))
	}
	acctID := list[0].ID

	// 1. 转为待命
	res, err := p.ExecuteAccountAction(acctID, "standby")
	if err != nil || res["status"] != "standby" {
		t.Fatalf("failed to standby: %v, %v", res, err)
	}
	if p.Stats().Standby != 1 {
		t.Fatalf("expected 1 standby account")
	}

	// 2. 重新激活
	res, err = p.ExecuteAccountAction(acctID, "activate")
	if err != nil || res["status"] != "active" {
		t.Fatalf("failed to activate: %v, %v", res, err)
	}
	if p.Stats().Active != 1 {
		t.Fatalf("expected 1 active account")
	}

	// 3. 停用
	res, err = p.ExecuteAccountAction(acctID, "disable")
	if err != nil || res["status"] != "disabled" {
		t.Fatalf("failed to disable: %v, %v", res, err)
	}
	if p.Stats().Disabled != 1 {
		t.Fatalf("expected 1 disabled account")
	}

	// 4. 删除
	res, err = p.ExecuteAccountAction(acctID, "delete")
	if err != nil || res["deleted"] != true {
		t.Fatalf("failed to delete: %v, %v", res, err)
	}
	if p.Stats().Total != 0 {
		t.Fatalf("expected 0 accounts left")
	}
}

func waitForGrokLoginTask(t *testing.T, p *GrokProvider, total int) KamiLoginTask {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if task := p.ActiveKamiLoginTask(); task != nil && task.Status == "completed" && task.Completed >= total {
			return *task
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for Grok login task, got %+v", p.ActiveKamiLoginTask())
	return KamiLoginTask{}
}

func TestGrokKamiImportAutoLoginUpdatesSSO(t *testing.T) {
	p := NewGrokProvider(nil, "https://grok.com", "", accounts.PoolPolicy{})
	p.grokLoginMu.Lock()
	p.grokLoginCooldown = 0
	p.grokLoginMu.Unlock()
	p.grokLoginFn = func(_ context.Context, card GrokCardAccount, options GrokLoginOptions) (string, error) {
		if !options.Headless {
			t.Errorf("expected headless login")
		}
		if card.Email == "" || card.Password == "" || card.TOTPSecret == "" {
			t.Fatalf("incomplete card: %+v", card)
		}
		return "sso=new_token; sso-rw=new_rw", nil
	}

	result, err := p.ImportCredentials(ImportRequest{
		CredentialType: "kami",
		RawText:        "user@example.com|password123|JBSWY3DPEHPK3PXP",
		Persist:        false,
		AutoLogin:      true,
	})
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if result.Added != 1 || result.Failed != 0 {
		t.Fatalf("unexpected import result: %+v", result)
	}

	task := waitForGrokLoginTask(t, p, 1)
	if task.Status != "completed" || task.Success != 1 || task.Failed != 0 {
		t.Fatalf("unexpected task: %+v", task)
	}
	list := p.ListAccounts("kami", "", "")
	if len(list) != 1 {
		t.Fatalf("expected one kami account, got %d", len(list))
	}
	if list[0].Status != "active" || !list[0].HasKamiCredentials || list[0].TokenPreview == "未提取 SSO" {
		t.Fatalf("account was not activated: %+v", list[0])
	}
	if list[0].LastAction != "login" || !list[0].LastActionOK {
		t.Fatalf("login action state missing: %+v", list[0])
	}
}

func TestGrokLoginActionReturnsAsyncTask(t *testing.T) {
	p := NewGrokProvider(nil, "https://grok.com", "", accounts.PoolPolicy{})
	p.AddAccounts([]string{"login@example.com|password123|JBSWY3DPEHPK3PXP"}, false)
	p.grokLoginMu.Lock()
	p.grokLoginCooldown = 0
	p.grokLoginMu.Unlock()
	p.grokLoginFn = func(_ context.Context, _ GrokCardAccount, _ GrokLoginOptions) (string, error) {
		return "sso=action_cookie; sso-rw=action_rw", nil
	}

	result, err := p.ExecuteAccountActionWithOpts("grok-login", "login", true)
	if err != nil {
		t.Fatalf("login action failed: %v", err)
	}
	taskID, _ := result["task_id"].(string)
	if taskID == "" || result["task"] == nil {
		t.Fatalf("login action did not return a task: %+v", result)
	}
	task := waitForGrokLoginTask(t, p, 1)
	if task.ID != taskID || task.Success != 1 {
		t.Fatalf("unexpected login task: %+v", task)
	}
	if !p.DismissKamiLoginTask(taskID) {
		t.Fatalf("expected completed task to be dismissible")
	}
}

func TestGrokLoginFailureKeepsCardCredentials(t *testing.T) {
	p := NewGrokProvider(nil, "https://grok.com", "", accounts.PoolPolicy{})
	p.AddAccounts([]string{"failed@example.com|password123|JBSWY3DPEHPK3PXP"}, false)
	p.grokLoginMu.Lock()
	p.grokLoginCooldown = 0
	p.grokLoginFn = func(_ context.Context, _ GrokCardAccount, _ GrokLoginOptions) (string, error) {
		return "", errors.New("captcha required")
	}
	p.grokLoginMu.Unlock()

	task, err := p.StartKamiPendingLogin(false, true)
	if err != nil {
		t.Fatalf("start login failed: %v", err)
	}
	completed := waitForGrokLoginTask(t, p, 1)
	if completed.ID != task.ID || completed.Failed != 1 || completed.Success != 0 {
		t.Fatalf("unexpected failed task: %+v", completed)
	}

	p.mu.RLock()
	account := p.findGrokAccountLocked("grok-failed")
	p.mu.RUnlock()
	if account == nil || account.Password == "" || account.TOTPSecret == "" || account.Cookie != "" {
		t.Fatalf("card credentials were lost: %+v", account)
	}
	if account.Status != accounts.StatusPending || account.LastError == "" {
		t.Fatalf("failure state was not recorded: %+v", account)
	}
}

func TestGrokLoginQueueCancellation(t *testing.T) {
	p := NewGrokProvider(nil, "https://grok.com", "", accounts.PoolPolicy{})
	p.AddAccounts([]string{
		"first@example.com|password123|JBSWY3DPEHPK3PXP",
		"second@example.com|password123|JBSWY3DPEHPK3PXP",
	}, false)
	p.grokLoginMu.Lock()
	p.grokLoginCooldown = 0
	p.grokLoginMu.Unlock()
	release := make(chan struct{})
	p.grokLoginFn = func(_ context.Context, card GrokCardAccount, _ GrokLoginOptions) (string, error) {
		select {
		case <-release:
			return "sso=" + card.Email + "; sso-rw=rw", nil
		default:
			<-release
			return "sso=" + card.Email + "; sso-rw=rw", nil
		}
	}

	task, err := p.StartKamiLoginBatch([]string{"grok-first", "grok-second"}, true)
	if err != nil {
		t.Fatalf("start login failed: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if snapshot, ok := p.KamiLoginTaskSnapshot(task.ID); ok && snapshot.CurrentAccountID == "grok-first" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if snapshot, _ := p.KamiLoginTaskSnapshot(task.ID); snapshot.CurrentAccountID != "grok-first" {
		t.Fatalf("first account did not start: %+v", snapshot)
	}

	cancelledTask, count, err := p.CancelKamiLoginBatch(task.ID, []string{"grok-second"})
	if err != nil || count != 1 || cancelledTask == nil || cancelledTask.Cancelled != 1 {
		t.Fatalf("unexpected cancellation: task=%+v count=%d err=%v", cancelledTask, count, err)
	}
	close(release)

	completed := waitForGrokLoginTask(t, p, 2)
	if completed.Success != 1 || completed.Cancelled != 1 || completed.Failed != 0 {
		t.Fatalf("unexpected completed task: %+v", completed)
	}
	if !p.DismissKamiLoginTask(task.ID) {
		t.Fatalf("expected task to be dismissible")
	}
}

func TestGrokImportAutoDetectsMixedCredentialFormats(t *testing.T) {
	p := NewGrokProvider(nil, "https://grok.com", "", accounts.PoolPolicy{})
	p.grokLoginMu.Lock()
	p.grokLoginCooldown = 0
	p.grokLoginMu.Unlock()
	logins := make(chan string, 2)
	p.grokLoginFn = func(_ context.Context, card GrokCardAccount, options GrokLoginOptions) (string, error) {
		if !options.Headless {
			t.Errorf("expected headless login")
		}
		logins <- card.Email
		return "sso=" + card.Email + "; sso-rw=rw", nil
	}

	result, err := p.ImportCredentials(ImportRequest{
		CredentialType: "auto",
		RawText: strings.Join([]string{
			"pipe@example.com|password123|JBSWY3DPEHPK3PXP",
			"dash@example.com----password123----JBSWY3DPEHPK3PXP",
			"legacy@example.com----sso=legacy; sso-rw=legacy-rw",
			"sso=pure; sso-rw=pure-rw",
			"not-a-grok-credential",
		}, "\n"),
		Persist:   false,
		AutoLogin: true,
	})
	if err != nil {
		t.Fatalf("auto import failed: %v", err)
	}
	if result.Added != 4 || result.Failed != 1 || result.Skipped != 0 {
		t.Fatalf("unexpected import result: %+v", result)
	}
	if len(result.Messages) != 3 {
		t.Fatalf("expected import, detection, and login messages: %+v", result.Messages)
	}
	if result.LoginTask == nil || result.LoginTask.Total != 2 {
		t.Fatalf("expected import response to carry the exact two-card login task: %+v", result.LoginTask)
	}

	task := waitForGrokLoginTask(t, p, 2)
	if task.Total != 2 || task.Success != 2 || task.Failed != 0 {
		t.Fatalf("expected only two card accounts to login: %+v", task)
	}
	close(logins)
	loggedIn := make(map[string]bool, 2)
	for email := range logins {
		loggedIn[email] = true
	}
	if !loggedIn["pipe@example.com"] || !loggedIn["dash@example.com"] {
		t.Fatalf("unexpected card logins: %+v", loggedIn)
	}

	stats := p.Stats()
	if stats.Total != 4 || stats.Pending != 0 || stats.Active != 4 {
		t.Fatalf("unexpected mixed-format pool stats: %+v", stats)
	}
}

func TestGrokImportUsesLLMFallbackForUnrecognizedLine(t *testing.T) {
	p := NewGrokProvider(nil, "https://grok.com", "", accounts.PoolPolicy{})
	p.grokCredentialLLMFn = func(_ context.Context, lines []string) ([]grokCredentialLLMResult, error) {
		if len(lines) != 1 || !strings.Contains(lines[0], "llm@example.com") {
			t.Fatalf("unexpected fallback input: %d line(s)", len(lines))
		}
		return []grokCredentialLLMResult{{
			Index: 0,
			Kind:  "kami",
			Value: "llm@example.com|password123|JBSWY3DPEHPK3PXP",
		}}, nil
	}

	result, err := p.ImportCredentials(ImportRequest{
		CredentialType: "auto",
		RawText:        "邮箱: llm@example.com  密码: password123  2FA: JBSWY3DPEHPK3PXP",
		Persist:        false,
	})
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if result.Added != 1 || result.Failed != 0 {
		t.Fatalf("unexpected import result: %+v", result)
	}
	if len(result.Messages) != 2 || !strings.Contains(result.Messages[1], "LLM 兜底：卡密 1 条") {
		t.Fatalf("unexpected messages: %+v", result.Messages)
	}
	if p.Stats().Pending != 1 || p.Stats().Active != 0 {
		t.Fatalf("normalized card was not imported as pending: %+v", p.Stats())
	}
}

func TestGrokImportLegacyJWTAsPendingCredential(t *testing.T) {
	p := NewGrokProvider(nil, "https://grok.com", "", accounts.PoolPolicy{})
	p.grokLoginFn = func(context.Context, GrokCardAccount, GrokLoginOptions) (string, error) {
		t.Fatal("expired legacy JWT must not start an automatic login")
		return "", nil
	}

	result, err := p.ImportCredentials(ImportRequest{
		CredentialType: "auto",
		RawText: "legacy@example.com|password123|" +
			"eyJhbGciOiJIUzI1NiJ9.eyJzZXNzaW9uX2lkIjoiMTIzNDU2Nzg5MCJ9.c2lnbmF0dXJl",
		Persist:   false,
		AutoLogin: true,
	})
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if result.Added != 1 || result.Failed != 0 || result.LoginTask != nil {
		t.Fatalf("unexpected import result: %+v", result)
	}
	if stats := p.Stats(); stats.Total != 1 || stats.Pending != 1 || stats.Active != 0 {
		t.Fatalf("legacy JWT was not imported as pending: %+v", stats)
	}
	list := p.ListAccounts("", "", "")
	if len(list) != 1 || list[0].TokenPreview != "旧 Token 未启用" || list[0].HasKamiCredentials {
		t.Fatalf("unexpected legacy JWT snapshot: %+v", list)
	}
}

func TestGrokLegacyJWTPersistsAcrossReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "grok-accounts.txt")
	p := NewGrokProvider(nil, "https://grok.com", path, accounts.PoolPolicy{})
	p.AddAccounts([]string{
		"persist@example.com|password123|" +
			"eyJhbGciOiJIUzI1NiJ9.eyJzZXNzaW9uX2lkIjoiMTIzNDU2Nzg5MCJ9.c2lnbmF0dXJl",
	}, false)
	p.persistToDisk()

	reloaded := NewGrokProvider(nil, "https://grok.com", path, accounts.PoolPolicy{})
	if stats := reloaded.Stats(); stats.Total != 1 || stats.Pending != 1 {
		t.Fatalf("unexpected reload stats: %+v", stats)
	}
	reloaded.mu.RLock()
	account := reloaded.findGrokAccountLocked("grok-persist")
	reloaded.mu.RUnlock()
	if account == nil || !strings.HasPrefix(account.LegacyToken, "eyJ") || account.TOTPSecret != "" || account.Cookie != "" {
		t.Fatalf("legacy JWT was not preserved on reload: %+v", account)
	}
}

func TestGrokImportRejectsHallucinatedLLMCredential(t *testing.T) {
	p := NewGrokProvider(nil, "https://grok.com", "", accounts.PoolPolicy{})
	p.grokCredentialLLMFn = func(_ context.Context, _ []string) ([]grokCredentialLLMResult, error) {
		return []grokCredentialLLMResult{{
			Index: 0,
			Kind:  "kami",
			Value: "other@example.com|password123|JBSWY3DPEHPK3PXP",
		}}, nil
	}

	result, err := p.ImportCredentials(ImportRequest{
		CredentialType: "auto",
		RawText:        "邮箱: llm@example.com  密码: password123  2FA: JBSWY3DPEHPK3PXP",
		Persist:        false,
	})
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if result.Added != 0 || result.Failed != 1 || p.Stats().Total != 0 {
		t.Fatalf("hallucinated credential was imported: %+v stats=%+v", result, p.Stats())
	}
	for _, message := range result.Messages {
		if strings.Contains(message, "password123") || strings.Contains(message, "JBSWY3DPEHPK3PXP") {
			t.Fatalf("message leaked credential: %s", message)
		}
	}
}

func TestGrokImportLLMFallbackPreservesDeterministicLines(t *testing.T) {
	p := NewGrokProvider(nil, "https://grok.com", "", accounts.PoolPolicy{})
	p.grokCredentialLLMFn = func(_ context.Context, lines []string) ([]grokCredentialLLMResult, error) {
		if len(lines) != 1 || !strings.Contains(lines[0], "llm@example.com") {
			t.Fatalf("unexpected fallback input: %d line(s)", len(lines))
		}
		return []grokCredentialLLMResult{{
			Index: 0,
			Kind:  "cookie",
			Value: "llm@example.com----sso=from-llm; sso-rw=from-llm-rw",
		}}, nil
	}

	result, err := p.ImportCredentials(ImportRequest{
		CredentialType: "auto",
		RawText: strings.Join([]string{
			"deterministic@example.com----sso=fixed; sso-rw=fixed-rw",
			"邮箱: llm@example.com SSO: sso=from-llm; sso-rw=from-llm-rw",
		}, "\n"),
		Persist: false,
	})
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if result.Added != 2 || result.Failed != 0 {
		t.Fatalf("unexpected import result: %+v", result)
	}
	if p.Stats().Active != 2 || p.Stats().Total != 2 {
		t.Fatalf("unexpected pool stats: %+v", p.Stats())
	}
}

func TestDecodeGrokCredentialLLMJSONStripsReasoning(t *testing.T) {
	var parsed struct {
		Results []grokCredentialLLMResult `json:"results"`
	}
	output := "<think>normalize only</think>\n{\"results\":[{\"index\":0,\"kind\":\"unknown\"}]}"
	if err := decodeGrokCredentialLLMJSON(output, &parsed); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(parsed.Results) != 1 || parsed.Results[0].Kind != "unknown" {
		t.Fatalf("unexpected parsed result: %+v", parsed.Results)
	}
}
