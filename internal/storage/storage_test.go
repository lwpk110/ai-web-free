package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalStorageEngineSaveAndDelete(t *testing.T) {
	tempDir := t.TempDir()
	engine, err := NewLocalStorageEngine(tempDir)
	if err != nil {
		t.Fatalf("NewLocalStorageEngine failed: %v", err)
	}

	ctx := context.Background()
	testData := []byte("fake-png-binary-content")
	url, relPath, err := engine.Save(ctx, "img-test-1", testData, "image/png")
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if url != "/storage/images/img-test-1.png" {
		t.Errorf("url = %q, want /storage/images/img-test-1.png", url)
	}
	if relPath != filepath.Join("images", "img-test-1.png") {
		t.Errorf("relPath = %q", relPath)
	}

	if !engine.Exists(ctx, relPath) {
		t.Errorf("Exists should be true for %q", relPath)
	}

	fullPath := filepath.Join(tempDir, relPath)
	dataOnDisk, err := os.ReadFile(fullPath)
	if err != nil || string(dataOnDisk) != string(testData) {
		t.Fatalf("File on disk mismatch: %v", err)
	}

	// Delete
	if err := engine.Delete(ctx, relPath); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if engine.Exists(ctx, relPath) {
		t.Errorf("Exists should be false after delete")
	}

	// Test traversal protection
	err = engine.Delete(ctx, "../outside.txt")
	if err == nil {
		t.Errorf("Delete outside file should return error")
	}
}

func TestMemoryStorageEngine(t *testing.T) {
	engine := NewMemoryStorageEngine()
	ctx := context.Background()
	testData := []byte("mem-data")

	url, path, err := engine.Save(ctx, "mem-img", testData, "image/png")
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	if !engine.Exists(ctx, path) {
		t.Errorf("Exists should be true")
	}
	if url != "/storage/images/mem-img.png" {
		t.Errorf("url = %q", url)
	}
	_ = engine.Delete(ctx, path)
	if engine.Exists(ctx, path) {
		t.Errorf("Exists should be false after delete")
	}
}
