package db

import (
	"path/filepath"
	"testing"
)

func TestDramaProjectRecordCRUD(t *testing.T) {
	gdb, err := InitDB(filepath.Join(t.TempDir(), "drama-crud.db"))
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	if sqlDB, err := gdb.DB(); err == nil {
		t.Cleanup(func() { _ = sqlDB.Close() })
	}

	rec := DramaProjectRecord{
		ID:            "proj-1",
		Title:         "雾夜追踪",
		Genre:         "都市悬疑",
		AspectRatio:   "9:16",
		StylePreset:   "电影感写实",
		RoutingPolicy: "free_first",
		Outline:       "第一幕：暴雨夜霓虹街头",
		GraphJSON:     `{"nodes":[{"id":"script-1"}],"edges":[]}`,
		Status:        "draft",
		ShotCount:     4,
	}
	if err := gdb.Create(&rec).Error; err != nil {
		t.Fatalf("create drama project failed: %v", err)
	}

	var loaded DramaProjectRecord
	if err := gdb.First(&loaded, "id = ?", "proj-1").Error; err != nil {
		t.Fatalf("load drama project failed: %v", err)
	}
	if loaded.Title != rec.Title || loaded.ShotCount != 4 || loaded.RoutingPolicy != "free_first" {
		t.Fatalf("unexpected loaded record: %+v", loaded)
	}

	if err := gdb.Model(&loaded).Updates(map[string]interface{}{
		"title":      "雾夜追踪（精修版）",
		"shot_count": 6,
		"status":     "completed",
	}).Error; err != nil {
		t.Fatalf("update drama project failed: %v", err)
	}

	var updated DramaProjectRecord
	if err := gdb.First(&updated, "id = ?", "proj-1").Error; err != nil {
		t.Fatalf("reload updated project failed: %v", err)
	}
	if updated.Title != "雾夜追踪（精修版）" || updated.ShotCount != 6 || updated.Status != "completed" {
		t.Fatalf("unexpected updated record: %+v", updated)
	}

	if err := gdb.Delete(&DramaProjectRecord{}, "id = ?", "proj-1").Error; err != nil {
		t.Fatalf("delete drama project failed: %v", err)
	}
}
