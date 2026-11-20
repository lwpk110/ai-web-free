package db

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestAutoMigrateSQLiteToPostgres(t *testing.T) {
	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "source.db")

	srcDB, err := gorm.Open(sqlite.Open(sourcePath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open source db: %v", err)
	}
	if sqlDB, err := srcDB.DB(); err == nil {
		defer sqlDB.Close()
	}
	if err := srcDB.AutoMigrate(
		&PoolAccountRecord{},
		&ProxyNodeRecord{},
		&AccountRestrictionRecord{},
		&CustomVoiceModelRecord{},
	); err != nil {
		t.Fatalf("failed to auto migrate source: %v", err)
	}

	// 插入测试数据
	account := PoolAccountRecord{
		ID:        "acc-test-1",
		Provider:  "chatgpt",
		Type:      "free",
		Email:     "test1@example.com",
		Token:     "test-token",
		Proxy:     "http://127.0.0.1:10809",
		Status:    "active",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := srcDB.Create(&account).Error; err != nil {
		t.Fatalf("failed to seed account: %v", err)
	}

	proxy := ProxyNodeRecord{
		ID:        "proxy-test-1",
		URL:       "http://127.0.0.1:10809",
		Status:    "active",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := srcDB.Create(&proxy).Error; err != nil {
		t.Fatalf("failed to seed proxy: %v", err)
	}

	// 目标库
	targetDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open target db: %v", err)
	}
	if err := targetDB.AutoMigrate(
		&PoolAccountRecord{},
		&ProxyNodeRecord{},
		&AccountRestrictionRecord{},
		&CustomVoiceModelRecord{},
	); err != nil {
		t.Fatalf("failed to auto migrate target: %v", err)
	}

	// 执行迁移
	if err := AutoMigrateSQLiteToPostgres(sourcePath, targetDB); err != nil {
		t.Fatalf("AutoMigrateSQLiteToPostgres failed: %v", err)
	}

	// 验证目标库是否有该账号
	var count int64
	targetDB.Model(&PoolAccountRecord{}).Count(&count)
	if count != 1 {
		t.Fatalf("expected 1 pool account in target, got %d", count)
	}

	var loaded PoolAccountRecord
	if err := targetDB.First(&loaded, "id = ?", "acc-test-1").Error; err != nil {
		t.Fatalf("failed to find migrated account: %v", err)
	}
	if loaded.Email != "test1@example.com" || loaded.Proxy != "http://127.0.0.1:10809" {
		t.Fatalf("migrated account data mismatch: %+v", loaded)
	}
}
