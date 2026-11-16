package storage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTieredStorageEngine(t *testing.T) {
	tempDir := t.TempDir()
	local, err := NewLocalStorageEngine(tempDir)
	if err != nil {
		t.Fatalf("NewLocalStorageEngine failed: %v", err)
	}

	remoteStore := make(map[string][]byte)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		if r.Method == "GET" {
			key := path[1:] // strip leading slash
			if data, ok := remoteStore[key]; ok {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(data)
				return
			}
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	qiniu := NewQiniuClient(QiniuConfig{
		AccessKey: "mock_ak",
		SecretKey: "mock_sk",
		Bucket:    "mock_bucket",
		Domain:    srv.Listener.Addr().String(),
	})
	qiniu.httpClient = srv.Client()

	engine := NewTieredStorageEngine(local, qiniu)
	engine.asyncUp = false // 同步便于测试

	ctx := context.Background()

	// 1. 本地未命中，云端存在 -> Read 触发回填
	remoteStore["storage/images/test_fill.png"] = []byte("fill_content")
	got, err := engine.Read(ctx, "images/test_fill.png")
	if err != nil {
		t.Fatalf("Read from remote failed: %v", err)
	}
	if string(got) != "fill_content" {
		t.Errorf("got %s, want fill_content", string(got))
	}

	// 验证已回填到本地
	if !local.Exists(ctx, "images/test_fill.png") {
		t.Errorf("expected local cache hit after read")
	}

	// 2. Save 验证
	url, relPath, err := engine.SaveInCategory(ctx, "images", "new_img", []byte("img_bytes"), "image/png")
	if err != nil {
		t.Fatalf("SaveInCategory failed: %v", err)
	}
	if url == "" || relPath == "" {
		t.Errorf("expected valid url and relPath")
	}

	if !engine.Exists(ctx, relPath) {
		t.Errorf("expected engine.Exists to be true")
	}
}
