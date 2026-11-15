package db

import (
	"path/filepath"
	"testing"
	"time"
)

func TestInitDBMemoryAndAutoMigrate(t *testing.T) {
	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB(:memory:) failed: %v", err)
	}

	// 1. GalleryWork CRUD
	gw := GalleryWork{
		ID:          "img-test-1",
		Prompt:      "A serene misty forest",
		Model:       "gpt-image-2.5-sunburst",
		Size:        "1024x1024",
		Quality:     "hd",
		Mode:        "generations",
		StorageType: "local",
		FilePath:    "images/img-test-1.png",
		URL:         "/storage/images/img-test-1.png",
		FileSize:    2048,
		LatencyMs:   1200,
		IsFavorite:  true,
		CreatedAt:   time.Now(),
	}
	if err := db.Create(&gw).Error; err != nil {
		t.Fatalf("Create GalleryWork failed: %v", err)
	}

	var foundGW GalleryWork
	if err := db.First(&foundGW, "id = ?", "img-test-1").Error; err != nil {
		t.Fatalf("Query GalleryWork failed: %v", err)
	}
	if foundGW.Prompt != gw.Prompt || !foundGW.IsFavorite {
		t.Errorf("GalleryWork mismatch: %+v", foundGW)
	}

	// 2. ChatMessageRecord CRUD
	chat := ChatMessageRecord{
		ID:         "msg-test-1",
		SessionKey: "default",
		Role:       "user",
		Content:    "Hello Copilot",
		OpenUIMode: true,
		CreatedAt:  time.Now(),
	}
	if err := db.Create(&chat).Error; err != nil {
		t.Fatalf("Create ChatMessageRecord failed: %v", err)
	}

	var chats []ChatMessageRecord
	if err := db.Where("session_key = ?", "default").Find(&chats).Error; err != nil || len(chats) != 1 {
		t.Fatalf("Query ChatMessageRecord failed: %v, count=%d", err, len(chats))
	}

	// 3. PromptFavorite CRUD
	fav := PromptFavorite{
		PromptID:  "civitai-12345",
		CreatedAt: time.Now(),
	}
	if err := db.Create(&fav).Error; err != nil {
		t.Fatalf("Create PromptFavorite failed: %v", err)
	}

	var favCount int64
	db.Model(&PromptFavorite{}).Count(&favCount)
	if favCount != 1 {
		t.Errorf("PromptFavorite count = %d, want 1", favCount)
	}

	// 4. AccountRuntimeState CRUD
	cooldown := time.Now().Add(5 * time.Minute)
	state := AccountRuntimeState{
		AccountID:        "user@example.com",
		Status:           "rate_limited",
		RateLimitedUntil: &cooldown,
		LastError:        "429 Too Many Requests",
		ErrorCount:       1,
		UpdatedAt:        time.Now(),
	}
	if err := db.Create(&state).Error; err != nil {
		t.Fatalf("Create AccountRuntimeState failed: %v", err)
	}
}

func TestInitDBFileFallback(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "sub", "test.db")

	db, err := InitDB(dbPath)
	if err != nil {
		t.Fatalf("InitDB file path failed: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		defer sqlDB.Close()
	}

	var count int64
	if err := db.Model(&GalleryWork{}).Count(&count).Error; err != nil {
		t.Fatalf("Count on file db failed: %v", err)
	}
	if count != 0 {
		t.Errorf("Initial count = %d, want 0", count)
	}
}
