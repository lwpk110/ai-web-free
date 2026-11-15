package storage

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
)

func TestMediaStorageSaveInCategoryAndRead(t *testing.T) {
	ctx := context.Background()
	videoPayload := []byte("fake-mp4-binary-payload")

	local, err := NewLocalStorageEngine(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalStorageEngine failed: %v", err)
	}
	url, relPath, err := local.SaveInCategory(ctx, "videos", "task-123", videoPayload, "video/mp4")
	if err != nil {
		t.Fatalf("local SaveInCategory failed: %v", err)
	}
	if url != "/storage/videos/task-123.mp4" {
		t.Fatalf("unexpected local url: %s", url)
	}
	if filepath.ToSlash(relPath) != "videos/task-123.mp4" {
		t.Fatalf("unexpected local relPath: %s", relPath)
	}
	got, err := local.Read(ctx, relPath)
	if err != nil {
		t.Fatalf("local Read failed: %v", err)
	}
	if !bytes.Equal(got, videoPayload) {
		t.Fatalf("local Read mismatch: got %d bytes want %d", len(got), len(videoPayload))
	}
	if _, err := local.Read(ctx, "../secret.txt"); err == nil {
		t.Fatalf("expected traversal error for local Read")
	}

	mem := NewMemoryStorageEngine()
	memURL, memRel, err := mem.SaveInCategory(ctx, "audio", "clip-1", []byte("mp3"), "audio/mpeg")
	if err != nil {
		t.Fatalf("memory SaveInCategory failed: %v", err)
	}
	if memURL != "/storage/audio/clip-1.mp3" || memRel != "audio/clip-1.mp3" {
		t.Fatalf("unexpected memory paths: %s / %s", memURL, memRel)
	}
	memGot, err := mem.Read(ctx, memRel)
	if err != nil || string(memGot) != "mp3" {
		t.Fatalf("memory Read mismatch: got %q err %v", string(memGot), err)
	}
}
