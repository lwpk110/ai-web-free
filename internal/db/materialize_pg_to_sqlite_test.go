package db

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestMaterializePostgresToSQLite(t *testing.T) {
	tempDir := t.TempDir()
	sourcePath := filepath.Join(tempDir, "source_sim_pg.db")
	targetPath := filepath.Join(tempDir, "target_materialized.db")

	srcDB, err := gorm.Open(sqlite.Open(sourcePath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open source db: %v", err)
	}
	if err := srcDB.AutoMigrate(
		&PoolAccountRecord{},
		&ProxyNodeRecord{},
		&DramaProjectRecord{},
		&GalleryWork{},
	); err != nil {
		t.Fatalf("failed to auto migrate source: %v", err)
	}

	// 插入测试模拟数据
	acc := PoolAccountRecord{
		ID:        "acc-cloud-1",
		Provider:  "chatgpt",
		Type:      "free",
		Email:     "cloud@example.com",
		Token:     "cloud-token",
		Status:    "active",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := srcDB.Create(&acc).Error; err != nil {
		t.Fatalf("failed to seed source: %v", err)
	}

	project := DramaProjectRecord{
		ID:        "proj-cloud-1",
		Title:     "云端微短剧",
		Genre:     "都市",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := srcDB.Create(&project).Error; err != nil {
		t.Fatalf("failed to seed project: %v", err)
	}

	// 执行物化同步
	if err := MaterializePostgresToSQLite(srcDB, targetPath); err != nil {
		t.Fatalf("MaterializePostgresToSQLite failed: %v", err)
	}

	// 校验目标 SQLite 数据库
	dstDB, err := gorm.Open(sqlite.Open(targetPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("failed to open materialized target: %v", err)
	}

	var accCount int64
	dstDB.Model(&PoolAccountRecord{}).Count(&accCount)
	if accCount != 1 {
		t.Errorf("expected 1 account in materialized target, got %d", accCount)
	}

	var projCount int64
	dstDB.Model(&DramaProjectRecord{}).Count(&projCount)
	if projCount != 1 {
		t.Errorf("expected 1 drama project in materialized target, got %d", projCount)
	}
}
