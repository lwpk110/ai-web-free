package handler

import (
	"bytes"
	"context"
	"net/http"
	"strings"
	"testing"

	"ai-web-free/internal/db"
	"ai-web-free/internal/storage"
)

func TestMediaResolverLocalStorageAndDataURI(t *testing.T) {
	ctx := context.Background()
	mem := storage.NewMemoryStorageEngine()
	payload := []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0x01, 0x02}
	url, relPath, err := mem.SaveInCategory(ctx, "images", "char-anchor-1", payload, "image/png")
	if err != nil {
		t.Fatalf("SaveInCategory failed: %v", err)
	}
	if url != "/storage/images/char-anchor-1.png" {
		t.Fatalf("unexpected url: %s", url)
	}

	gotBytes, mimeType, filename, ok := LoadLocalImageAsset(url, mem, nil)
	if !ok || !bytes.Equal(gotBytes, payload) || mimeType != "image/png" || filename != "char-anchor-1.png" {
		t.Fatalf("LoadLocalImageAsset relative failed: ok=%v mime=%s file=%s len=%d", ok, mimeType, filename, len(gotBytes))
	}

	// Loopback HTTP URL pointing to /storage/images/... must resolve locally without HTTP fetch.
	loopbackBytes, _, _, ok2 := LoadLocalImageAsset("http://127.0.0.1:18080"+url+"?v=1", mem, nil)
	if !ok2 || !bytes.Equal(loopbackBytes, payload) {
		t.Fatalf("LoadLocalImageAsset loopback failed: ok=%v len=%d", ok2, len(loopbackBytes))
	}

	// Non-loopback third-party URL must not be intercepted.
	if _, _, _, ok3 := LoadLocalImageAsset("https://example.com/storage/images/char-anchor-1.png", mem, nil); ok3 {
		t.Fatalf("unexpected interception of external URL")
	}

	// DB GalleryWork fallback by work ID -> local_path
	gdb, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	work := db.GalleryWork{
		ID:       "work-abc",
		Prompt:   "hero",
		Model:    "flux",
		URL:      "/storage/images/custom-name.png",
		FilePath: relPath,
	}
	if err := gdb.Create(&work).Error; err != nil {
		t.Fatalf("Create GalleryWork failed: %v", err)
	}
	resolvedDataURI := ResolveMediaURLToDataURI("/storage/images/work-abc.png", mem, gdb)
	if !strings.HasPrefix(resolvedDataURI, "data:image/png;base64,") {
		t.Fatalf("expected data URI from DB fallback, got %s", resolvedDataURI)
	}

	// imageEditConvertURLWithRuntime must decode /storage/images/... directly into editImageInput.
	rt := &GatewayRuntime{StorageEngine: mem, DB: gdb}
	item, found, err := imageEditConvertURLWithRuntime(nil, rt, url)
	if err != nil || !found || !bytes.Equal(item.Data, payload) {
		t.Fatalf("imageEditConvertURLWithRuntime failed: found=%v err=%v len=%d", found, err, len(item.Data))
	}
	_ = http.StatusOK
}
