package bootstrap

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/config"
	"ai-web-free/internal/db"
)

func TestSyncLocalFilesToDBAndExport(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test.db")
	cfg := &config.Config{
		DatabaseURL: dbPath,
	}

	// 1. 测试在空数据库下同步
	count, err := SyncLocalFilesToDB(cfg)
	if err != nil {
		t.Fatalf("SyncLocalFilesToDB failed: %v", err)
	}
	t.Logf("synced %d accounts to db", count)

	// 2. 写入测试账号至 DB 并导出
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	testAccs := []*accounts.Account{
		{
			ID:         "acc-export-1",
			Email:      "export_test@example.com",
			Password:   "MyPass123",
			TOTPSecret: "JBSWY3DPEHPK3PXP",
			Token:      "mock-access-token-999",
			Type:       accounts.TypeFree,
			Status:     accounts.StatusActive,
		},
	}
	if err := accounts.BatchSaveAccountsToDB(database, testAccs); err != nil {
		t.Fatalf("BatchSaveAccountsToDB failed: %v", err)
	}

	origDir, _ := os.Getwd()
	if err := os.Chdir(tempDir); err != nil {
		t.Fatalf("chdir failed: %v", err)
	}
	defer func() {
		_ = os.Chdir(origDir)
	}()

	exportedCount, err := ExportDBToLocalFiles(cfg)
	if err != nil {
		t.Fatalf("ExportDBToLocalFiles failed: %v", err)
	}
	if exportedCount < 1 {
		t.Errorf("expected at least 1 exported account, got %d", exportedCount)
	}

	kamiData, err := os.ReadFile("kami_accounts.txt")
	if err != nil {
		t.Fatalf("read kami_accounts.txt failed: %v", err)
	}
	if !strings.Contains(string(kamiData), "export_test@example.com") {
		t.Errorf("kami_accounts.txt missing export_test@example.com: %s", string(kamiData))
	}
}
