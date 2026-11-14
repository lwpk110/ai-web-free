package provider

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/config"
	"ai-web-free/internal/proxy"
)

func TestProviderRegistry(t *testing.T) {
	reg := NewRegistry()
	pool := accounts.NewPool(nil)
	proxyPool := proxy.NewPool([]string{"http://127.0.0.1:8080"})
	cfg := &config.Config{}

	chatgptPlugin := NewChatGPTProvider(pool, proxyPool, cfg, nil)
	RegisterDefaultProviders(reg, chatgptPlugin)

	descriptors := reg.ListDescriptors()
	if len(descriptors) != 5 {
		t.Fatalf("expected 5 descriptors, got %d", len(descriptors))
	}

	p, ok := reg.Get("chatgpt")
	if !ok || p.Descriptor().ID != "chatgpt" {
		t.Fatalf("failed to get chatgpt provider")
	}

	// 检查预留槽位
	claude, ok := reg.Get("claude")
	if !ok || claude.Descriptor().Status != "planned" {
		t.Fatalf("expected planned claude provider")
	}
}

func TestChatGPTProviderImportAndActions(t *testing.T) {
	pool := accounts.NewPool(nil)
	proxyPool := proxy.NewPool([]string{"http://127.0.0.1:8080"})
	cfg := &config.Config{}

	p := NewChatGPTProvider(pool, proxyPool, cfg, nil)

	// 1. 批量导入免登 UUID 号
	importRes, err := p.ImportCredentials(ImportRequest{
		CredentialType: "free_uuid",
		Tokens:         []string{"uuid-test-1", "uuid-test-2"},
		Persist:        false,
	})
	if err != nil {
		t.Fatalf("import failed: %v", err)
	}
	if importRes.Added != 2 {
		t.Fatalf("expected 2 added, got %d", importRes.Added)
	}

	stats := p.Stats()
	if stats.Total != 2 || stats.NoAuthCount != 2 {
		t.Fatalf("unexpected stats: %+v", stats)
	}

	// 2. 账号快照列表
	list := p.ListAccounts("noauth", "", "")
	if len(list) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(list))
	}

	targetID := list[0].ID

	// 3. 执行单号测活
	probeRes, err := p.ExecuteAccountAction(targetID, "probe")
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}
	if probeRes["status"] != "active" {
		t.Fatalf("unexpected probe status: %v", probeRes["status"])
	}

	// 4. 禁用账号
	_, err = p.ExecuteAccountAction(targetID, "disable")
	if err != nil {
		t.Fatalf("disable failed: %v", err)
	}
	snap := p.ListAccounts("", "disabled", "")
	if len(snap) != 1 || snap[0].ID != targetID {
		t.Fatalf("expected 1 disabled account")
	}

	// 5. 删除账号
	_, err = p.ExecuteAccountAction(targetID, "delete")
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}
	if p.Stats().Total != 1 {
		t.Fatalf("expected 1 remaining account")
	}
}

func TestKamiLoginBatchTaskUpdatesPreflightResults(t *testing.T) {
	pool := accounts.NewPool(nil)
	acct := accounts.CreateAccount("", accounts.TypeFree, accounts.DefaultProfiles)
	acct.ID = "missing-kami-credentials"
	acct.Email = "pending@example.com"
	pool.AddAccount(acct)
	plugin := NewChatGPTProvider(pool, proxy.NewPool(nil), &config.Config{}, nil)

	task, err := plugin.StartKamiLoginBatch([]string{acct.ID}, true)
	if err != nil {
		t.Fatalf("start login task: %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for task.Status != "completed" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
		task, _ = plugin.KamiLoginTaskSnapshot(task.ID)
	}
	if task.Status != "completed" || task.Total != 1 || task.Completed != 1 || task.Failed != 1 {
		t.Fatalf("unexpected completed task: %+v", task)
	}
	if len(task.Results) != 1 || task.Results[0].OK || task.Results[0].Error != "账号未配置完整卡密凭据" {
		t.Fatalf("expected an incremental preflight failure result, got %+v", task.Results)
	}
}

func TestKamiLoginBatchDoesNotStartBrowserWhenProxyPreflightFails(t *testing.T) {
	proxyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxyServer.Close()

	pool := accounts.NewPool(nil)
	acct := accounts.CreateAccount("access-token", accounts.TypeFree, accounts.DefaultProfiles)
	acct.ID = "proxy-preflight"
	acct.Email = "proxy-preflight@example.com"
	acct.Password = "password"
	acct.TOTPSecret = "JBSWY3DPEHPK3PXP"
	acct.Proxy = proxyServer.URL
	pool.AddAccount(acct)
	proxyPool := proxy.NewPool([]string{proxyServer.URL})
	plugin := NewChatGPTProvider(pool, proxyPool, &config.Config{}, nil)

	task, err := plugin.StartKamiLoginBatch([]string{acct.ID}, true)
	if err != nil {
		t.Fatalf("start login task: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for task.Status != "completed" && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		task, _ = plugin.KamiLoginTaskSnapshot(task.ID)
	}
	if task.Status != "completed" || task.Completed != 1 || task.Failed != 1 || len(task.Results) != 1 {
		t.Fatalf("expected proxy preflight failure result, got %+v", task)
	}
	if !strings.Contains(task.Results[0].Error, "代理") || acct.Proxy != proxyServer.URL {
		t.Fatalf("expected proxy failure without direct egress, result=%+v proxy=%q", task.Results[0], acct.Proxy)
	}
}

func TestChatGPTProviderImportKamiAndSub2API(t *testing.T) {
	pool := accounts.NewPool(nil)
	proxyPool := proxy.NewPool([]string{"http://127.0.0.1:8080"})
	cfg := &config.Config{}
	p := NewChatGPTProvider(pool, proxyPool, cfg, nil)

	// 1. 测试卡密格式导入（不触发浏览器登录）
	kamiRes, err := p.ImportCredentials(ImportRequest{
		CredentialType: "kami",
		RawText:        "alice@example.com----pass123----JBSWY3DPEHPK3PXP\nbob@example.com----pass456----JBSWY3DPEHPK3PXQ",
		Persist:        false,
		AutoLogin:      false,
	})
	if err != nil {
		t.Fatalf("kami import failed: %v", err)
	}
	if kamiRes.Added != 2 || p.Stats().Pending != 2 {
		t.Fatalf("unexpected kami import result: %+v stats=%+v", kamiRes, p.Stats())
	}

	// 2. 测试 TOTP 动作
	snaps := p.ListAccounts("free", "pending", "alice@example.com")
	if len(snaps) != 1 {
		t.Fatalf("expected 1 pending alice account, got %d", len(snaps))
	}
	totpRes, err := p.ExecuteAccountAction(snaps[0].ID, "totp")
	if err != nil {
		t.Fatalf("totp action failed: %v", err)
	}
	if code, ok := totpRes["totp_code"].(string); !ok || len(code) != 6 {
		t.Fatalf("unexpected totp_code: %+v", totpRes)
	}

	// 3. 测试 Sub2API JSON 导入激活已有 pending 账号
	subRes, err := p.ImportCredentials(ImportRequest{
		CredentialType: "sub2api_json",
		RawText: `{
			"type": "sub2api-data",
			"version": 1,
			"accounts": [{
				"name": "alice@example.com",
				"credentials": {
					"access_token": "tok-alice-123",
					"session_token": "sess-alice-123",
					"chatgpt_account_id": "acct-alice-123"
				},
				"extra": {
					"email": "alice@example.com",
					"password": "pass123",
					"totp_secret": "JBSWY3DPEHPK3PXP"
				}
			}]
		}`,
		Persist: false,
	})
	if err != nil {
		t.Fatalf("sub2api_json import failed: %v", err)
	}
	if subRes.Added != 1 || p.Stats().Active != 1 {
		t.Fatalf("unexpected sub2api import result: %+v stats=%+v", subRes, p.Stats())
	}
}

func TestSingleAccountLoginActionPhaseQueueAppendAndCancel(t *testing.T) {
	pool := accounts.NewPool(nil)
	p := NewChatGPTProvider(pool, nil, &config.Config{}, nil)
	p.kamiLoginCooldown = 5 * time.Millisecond

	makeAcct := func(id, email string) *accounts.Account {
		a := accounts.CreateAccount("", accounts.TypeFree, accounts.DefaultProfiles)
		a.ID = id
		a.Email = email
		a.Password = "pass"
		a.TOTPSecret = "JBSWY3DPEHPK3PXP"
		a.Status = accounts.StatusExpired
		a.LastRenewError = "old error"
		a.RenewCooldownUntil = time.Now().Add(5 * time.Minute)
		pool.AddAccount(a)
		return a
	}

	a1 := makeAcct("acc-1", "one@example.com")
	a2 := makeAcct("acc-2", "two@example.com")
	a3 := makeAcct("acc-3", "three@example.com")

	startedCh := make(chan struct{}, 1)
	releaseCh := make(chan struct{})
	p.SetKamiLoginExecutor(func(targets []*accounts.Account, _ string, _ bool, onPhase KamiPhaseReporter) (map[string]KamiLoginOutcome, error) {
		out := make(map[string]KamiLoginOutcome, len(targets))
		for _, acct := range targets {
			if onPhase != nil {
				onPhase(acct.ID, "enter_email", KamiPhaseLabel("enter_email"))
				onPhase(acct.ID, "enter_totp", KamiPhaseLabel("enter_totp"))
			}
			select {
			case startedCh <- struct{}{}:
			default:
			}
			<-releaseCh
			out[strings.ToLower(acct.Email)] = KamiLoginOutcome{
				Email:            acct.Email,
				OK:               true,
				AccessToken:      "tok-" + acct.ID,
				SessionToken:     "sess-" + acct.ID,
				ChatGPTAccountID: "cg-" + acct.ID,
				ExpiresAt:        time.Now().Add(7 * 24 * time.Hour).Unix(),
			}
		}
		return out, nil
	})

	// 1. Trigger single-account async re-login.
	res1, err := p.ExecuteAccountActionWithOpts(a1.ID, "login", false)
	if err != nil {
		t.Fatalf("single login action failed: %v", err)
	}
	task1, ok := res1["task"].(KamiLoginTask)
	if !ok || task1.ID == "" {
		t.Fatalf("expected task in action result, got %+v", res1)
	}

	select {
	case <-startedCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for mock login executor start")
	}

	// Verify live phase visibility in snapshot and ActiveKamiLoginTask.
	activeTask := p.ActiveKamiLoginTask()
	if activeTask == nil || activeTask.CurrentAccountID != a1.ID || activeTask.CurrentPhase != "enter_totp" {
		t.Fatalf("expected active phase enter_totp on acc-1, got %+v", activeTask)
	}
	snaps := p.ListAccounts("", "", "one@example.com")
	if len(snaps) != 1 || snaps[0].ActiveAction != "login" || snaps[0].ActionPhase != "enter_totp" {
		t.Fatalf("expected snapshot to expose active login phase, got %+v", snaps)
	}

	// 2. Dynamically append a2 and a3 while a1 is still running.
	if _, err := p.ExecuteAccountActionWithOpts(a2.ID, "login", true); err != nil {
		t.Fatalf("append a2 failed: %v", err)
	}
	res3, err := p.ExecuteAccountActionWithOpts(a3.ID, "login", true)
	if err != nil {
		t.Fatalf("append a3 failed: %v", err)
	}
	task3 := res3["task"].(KamiLoginTask)
	if task3.ID != task1.ID || task3.Total != 3 || len(task3.QueuedAccountIDs) != 2 {
		t.Fatalf("expected dynamic append onto task %s (total=3, queued=2), got %+v", task1.ID, task3)
	}

	// 3. Cancel a2 only -> a3 shifts from queue #2 to #1.
	cancelledTask, count, err := p.CancelKamiLogin(task1.ID, a2.ID, false)
	if err != nil || count != 1 || len(cancelledTask.QueuedAccountIDs) != 1 || cancelledTask.QueuedAccountIDs[0] != a3.ID {
		t.Fatalf("expected cancel a2 leave [acc-3], count=%d err=%v task=%+v", count, err, cancelledTask)
	}
	snap2 := pool.ListSnapshots("", "", "two@example.com")
	if len(snap2) != 1 || snap2[0].ActiveAction != "" || snap2[0].LastActionSummary != "已取消重登排队" {
		t.Fatalf("expected cleared queue state on a2, got %+v", snap2)
	}
	snap3 := pool.ListSnapshots("", "", "three@example.com")
	if len(snap3) != 1 || snap3[0].ActionQueuePos != 1 {
		t.Fatalf("expected a3 queue position shifted to 1, got %+v", snap3)
	}

	// 4. Cancel remaining (a3), release running login (a1), wait for completion.
	if _, _, err := p.CancelKamiLogin(task1.ID, "", true); err != nil {
		t.Fatalf("cancel remaining failed: %v", err)
	}
	close(releaseCh)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		st, _ := p.KamiLoginTaskSnapshot(task1.ID)
		if st.Status == "completed" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	finalTask, _ := p.KamiLoginTaskSnapshot(task1.ID)
	if finalTask.Status != "completed" || finalTask.Success != 1 || finalTask.Cancelled != 2 {
		t.Fatalf("unexpected final task state: %+v", finalTask)
	}
	if a1.Status != accounts.StatusActive || a1.LastRenewError != "" || a1.LastActionSummary != "重登成功，Token 已更新" {
		t.Fatalf("expected a1 active with cleared error and summary, got status=%v err=%q sum=%q", a1.Status, a1.LastRenewError, a1.LastActionSummary)
	}

	// 5. Dismiss task removes it from ActiveKamiLoginTask.
	if !p.DismissKamiLoginTask(task1.ID) {
		t.Fatalf("expected DismissKamiLoginTask to succeed")
	}
	if p.ActiveKamiLoginTask() != nil {
		t.Fatalf("expected nil ActiveKamiLoginTask after dismiss")
	}
}
