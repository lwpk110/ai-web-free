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
