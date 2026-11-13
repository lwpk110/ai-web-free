package provider

import (
	"testing"

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
