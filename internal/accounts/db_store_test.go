package accounts

import (
	"testing"
	"time"

	"ai-web-free/internal/db"
)

func TestDBStore_RoundTripAndMerge(t *testing.T) {
	testDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	profiles := DefaultProfiles

	// 1. 创建并保存一个初始账号（仅包含 email 与 password/totp）
	acct1 := CreateAccount("", TypeFree, profiles)
	acct1.Email = "User1@Example.COM"
	acct1.Password = "SecretPass123"
	acct1.TOTPSecret = "JBSWY3DPEHPK3PXP"
	acct1.Status = StatusPending
	acct1.Proxy = "http://127.0.0.1:7890"

	if err := SaveAccountToDB(testDB, acct1); err != nil {
		t.Fatalf("SaveAccountToDB failed: %v", err)
	}

	// 2. 从数据库加载并校验
	loaded, err := LoadAccountsFromDB(testDB, profiles)
	if err != nil {
		t.Fatalf("LoadAccountsFromDB failed: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 account, got %d", len(loaded))
	}
	loadedAcct := loaded[0]
	if loadedAcct.Email != "User1@Example.COM" || loadedAcct.Password != "SecretPass123" {
		t.Errorf("loaded account credentials mismatch: %+v", loadedAcct)
	}
	if loadedAcct.Status != StatusPending || loadedAcct.Proxy != "http://127.0.0.1:7890" {
		t.Errorf("loaded account status/proxy mismatch: %+v", loadedAcct)
	}

	// 3. 模拟 Playwright 登录成功后赋予 Token，再次保存应平滑合并
	acct2 := CreateAccount("mock-jwt-token-xyz", TypeFree, profiles)
	acct2.Email = "user1@example.com"
	acct2.ChatGPTAccountID = "chatgpt-acc-888"
	acct2.Status = StatusActive
	now := time.Now().Truncate(time.Second)
	acct2.ExpiresAt = now.Add(24 * time.Hour)

	if err := SaveAccountToDB(testDB, acct2); err != nil {
		t.Fatalf("SaveAccountToDB update failed: %v", err)
	}

	loaded2, err := LoadAccountsFromDB(testDB, profiles)
	if err != nil {
		t.Fatalf("LoadAccountsFromDB failed: %v", err)
	}
	if len(loaded2) != 1 {
		t.Fatalf("expected 1 merged account, got %d", len(loaded2))
	}
	merged := loaded2[0]
	if merged.Token != "mock-jwt-token-xyz" {
		t.Errorf("expected token mock-jwt-token-xyz, got %s", merged.Token)
	}
	if merged.Password != "SecretPass123" || merged.TOTPSecret != "JBSWY3DPEHPK3PXP" {
		t.Errorf("expected password/totp to be preserved, got pass=%s totp=%s", merged.Password, merged.TOTPSecret)
	}
	if merged.ChatGPTAccountID != "chatgpt-acc-888" {
		t.Errorf("expected chatgpt account id chatgpt-acc-888, got %s", merged.ChatGPTAccountID)
	}

	// 4. 测试删除
	if err := DeleteAccountFromDB(testDB, merged.ID); err != nil {
		t.Fatalf("DeleteAccountFromDB failed: %v", err)
	}
	loaded3, _ := LoadAccountsFromDB(testDB, profiles)
	if len(loaded3) != 0 {
		t.Errorf("expected 0 accounts after delete, got %d", len(loaded3))
	}

	// 5. 批量保存测试
	accs := []*Account{
		{ID: "batch-1", Email: "b1@test.com", Type: TypeFree, Status: StatusActive},
		{ID: "batch-2", Email: "b2@test.com", Type: TypePUID, Status: StatusActive},
	}
	if err := BatchSaveAccountsToDB(testDB, accs); err != nil {
		t.Fatalf("BatchSaveAccountsToDB failed: %v", err)
	}
	loadedBatch, _ := LoadAccountsFromDB(testDB, profiles)
	if len(loadedBatch) != 2 {
		t.Fatalf("expected 2 accounts from batch, got %d", len(loadedBatch))
	}

	// 6. 空 DB 安全测试
	if err := SaveAccountToDB(nil, accs[0]); err != nil {
		t.Errorf("expected nil error on nil DB, got %v", err)
	}
	if loadedNil, err := LoadAccountsFromDB(nil, profiles); err != nil || loadedNil != nil {
		t.Errorf("expected nil on nil DB load, got %v, %v", loadedNil, err)
	}
}
