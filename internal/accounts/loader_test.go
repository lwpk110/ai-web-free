package accounts

import (
	"os"
	"testing"
)

func TestLoadTokensFromFile(t *testing.T) {
	// 写临时文件
	content := "token1\n# comment\ntoken2:team123\n\ntoken3\n"
	tmpfile := "_test_tokens.txt"
	os.WriteFile(tmpfile, []byte(content), 0644)
	defer os.Remove(tmpfile)

	secrets := LoadTokensFromFile(tmpfile)
	if len(secrets) != 3 {
		t.Fatalf("got %d tokens, want 3", len(secrets))
	}
	if secrets[0].Token != "token1" {
		t.Errorf("secrets[0].Token = %q, want %q", secrets[0].Token, "token1")
	}
	if secrets[1].Token != "token2" {
		t.Errorf("secrets[1].Token = %q, want %q", secrets[1].Token, "token2")
	}
	if secrets[1].TeamID != "team123" {
		t.Errorf("secrets[1].TeamID = %q, want %q", secrets[1].TeamID, "team123")
	}
	if secrets[2].Token != "token3" {
		t.Errorf("secrets[2].Token = %q, want %q", secrets[2].Token, "token3")
	}
}

func TestLoadTokensFromFileMissing(t *testing.T) {
	secrets := LoadTokensFromFile("_nonexistent_file_")
	if secrets == nil || len(secrets) != 0 {
		t.Errorf("missing file should return empty list, got %v", secrets)
	}
}

func TestBannedAccountsPersistence(t *testing.T) {
	tmpFile := "_test_banned_accounts.txt"
	defer os.Remove(tmpFile)

	// 初始追加
	if err := AppendBannedToFile(tmpFile, "user1@example.com", "account_deactivated"); err != nil {
		t.Fatalf("append failed: %v", err)
	}
	if err := AppendBannedToFile(tmpFile, "USER2@example.com", "permanently banned"); err != nil {
		t.Fatalf("append failed: %v", err)
	}

	// 检查重复追加幂等性
	if err := AppendBannedToFile(tmpFile, "user1@example.com", "other reason"); err != nil {
		t.Fatalf("idempotent append failed: %v", err)
	}

	banned := LoadBannedFromFile(tmpFile)
	if len(banned) != 2 {
		t.Fatalf("expected 2 banned accounts, got %d", len(banned))
	}
	if banned["user1@example.com"] != "account_deactivated" {
		t.Errorf("expected account_deactivated, got %s", banned["user1@example.com"])
	}
	if banned["user2@example.com"] != "permanently banned" {
		t.Errorf("expected permanently banned, got %s", banned["user2@example.com"])
	}

	// 移除
	if err := RemoveBannedFromFile(tmpFile, "user1@example.com"); err != nil {
		t.Fatalf("remove failed: %v", err)
	}
	banned = LoadBannedFromFile(tmpFile)
	if len(banned) != 1 {
		t.Fatalf("expected 1 banned account after removal, got %d", len(banned))
	}
	if _, ok := banned["user1@example.com"]; ok {
		t.Errorf("expected user1@example.com to be removed")
	}
}

func TestDisabledAccountsPersistence(t *testing.T) {
	tmpFile := "_test_disabled_accounts.txt"
	defer os.Remove(tmpFile)

	if err := AppendDisabledToFile(tmpFile, "test@example.com", "管理员手动禁用"); err != nil {
		t.Fatalf("append failed: %v", err)
	}
	disabled := LoadDisabledFromFile(tmpFile)
	if disabled["test@example.com"] != "管理员手动禁用" {
		t.Errorf("expected disabled, got %s", disabled["test@example.com"])
	}
	if err := RemoveDisabledFromFile(tmpFile, "test@example.com"); err != nil {
		t.Fatalf("remove failed: %v", err)
	}
	disabled = LoadDisabledFromFile(tmpFile)
	if len(disabled) != 0 {
		t.Errorf("expected 0 disabled accounts, got %d", len(disabled))
	}
}

func TestAccountProxiesPersistence(t *testing.T) {
	tmpFile := "_test_account_proxies.txt"
	defer os.Remove(tmpFile)

	if err := SetAccountProxyInFile(tmpFile, "user1@test.com", "http://127.0.0.1:10809"); err != nil {
		t.Fatalf("set proxy failed: %v", err)
	}
	bindings := LoadAccountProxiesFromFile(tmpFile)
	if bindings["user1@test.com"] != "http://127.0.0.1:10809" {
		t.Errorf("expected http://127.0.0.1:10809, got %s", bindings["user1@test.com"])
	}

	// 清空代理绑定
	if err := SetAccountProxyInFile(tmpFile, "user1@test.com", ""); err != nil {
		t.Fatalf("clear proxy failed: %v", err)
	}
	bindings = LoadAccountProxiesFromFile(tmpFile)
	if len(bindings) != 0 {
		t.Errorf("expected 0 bindings, got %d", len(bindings))
	}
}

func TestDeterministicFingerprint(t *testing.T) {
	fp1 := deterministicProfile("user1@example.com", DefaultProfiles)
	fp2 := deterministicProfile("user1@example.com", DefaultProfiles)
	if fp1.TLSProfileName != fp2.TLSProfileName || fp1.UserAgent != fp2.UserAgent {
		t.Errorf("deterministicProfile should be identical for same seed, got %s vs %s", fp1.TLSProfileName, fp2.TLSProfileName)
	}
}
