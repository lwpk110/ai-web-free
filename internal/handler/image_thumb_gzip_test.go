package handler

import (
	"bytes"
	"compress/gzip"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ai-web-free/internal/db"
	"ai-web-free/internal/storage"

	"github.com/gin-gonic/gin"
)

func buildNoisyPNG(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			v := uint8((x*13 ^ y*37 + (x*y)%251) & 0xff)
			img.SetNRGBA(x, y, color.NRGBA{
				R: v,
				G: uint8((v + uint8(x)) & 0xff),
				B: uint8((255 - v + uint8(y)) & 0xff),
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test png: %v", err)
	}
	return buf.Bytes()
}

func TestServeStaticImageThumbAndOriginal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tmpDir := t.TempDir()
	eng, err := storage.NewLocalStorageEngine(tmpDir)
	if err != nil {
		t.Fatalf("NewLocalStorageEngine: %v", err)
	}

	rawPNG := buildNoisyPNG(t, 1200, 900)
	if len(rawPNG) < 100*1024 {
		t.Fatalf("expected large noisy PNG for realistic test, got %d bytes", len(rawPNG))
	}

	ctx := context.Background()
	_, _, err = eng.SaveInCategory(ctx, "images", "sample_hd", rawPNG, "image/png")
	if err != nil {
		t.Fatalf("SaveInCategory: %v", err)
	}

	gh := NewGalleryHandler(nil, eng)
	r := gin.New()
	r.GET("/storage/images/:filename", gh.ServeStaticImage)
	r.HEAD("/storage/images/:filename", gh.ServeStaticImage)

	// 1. Requesting ?w=480 returns a much smaller 480px JPEG thumbnail and caches it on disk
	req := httptest.NewRequest(http.MethodGet, "/storage/images/sample_hd.png?w=450", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("thumb GET status = %d: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "image/jpeg") {
		t.Fatalf("expected image/jpeg Content-Type, got %q", ct)
	}
	thumbBytes := w.Body.Bytes()
	if len(thumbBytes) >= len(rawPNG)/4 {
		t.Fatalf("expected thumb (%d bytes) to be much smaller than original (%d bytes)", len(thumbBytes), len(rawPNG))
	}
	decodedThumb, err := jpeg.Decode(bytes.NewReader(thumbBytes))
	if err != nil {
		t.Fatalf("decode returned jpeg: %v", err)
	}
	if decodedThumb.Bounds().Dx() != 480 || decodedThumb.Bounds().Dy() != 360 {
		t.Fatalf("unexpected thumb dimensions: %v", decodedThumb.Bounds())
	}

	thumbCachePath := filepath.Join(tmpDir, "thumbs", "sample_hd.png.w480.jpg")
	if info, statErr := os.Stat(thumbCachePath); statErr != nil || info.Size() == 0 {
		t.Fatalf("expected L1 thumb cache file at %s, err=%v", thumbCachePath, statErr)
	}

	// 2. Requesting without ?w still returns exact original full-res PNG
	origReq := httptest.NewRequest(http.MethodGet, "/storage/images/sample_hd.png", nil)
	origW := httptest.NewRecorder()
	r.ServeHTTP(origW, origReq)
	if origW.Code != http.StatusOK {
		t.Fatalf("original GET status = %d", origW.Code)
	}
	if !bytes.Equal(origW.Body.Bytes(), rawPNG) {
		t.Fatalf("expected original PNG bytes unchanged (got %d vs %d)", origW.Body.Len(), len(rawPNG))
	}

	// 3. HEAD request on ?w=480 returns 200 with Content-Type image/jpeg
	headReq := httptest.NewRequest(http.MethodHead, "/storage/images/sample_hd.png?w=480", nil)
	headW := httptest.NewRecorder()
	r.ServeHTTP(headW, headReq)
	if headW.Code != http.StatusOK {
		t.Fatalf("HEAD thumb status = %d", headW.Code)
	}
}

func TestGzipMiddlewareAndDramaListProjectsOmitsBlobs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gdb, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	hugeGraph := `{"nodes":[` + strings.Repeat(`{"id":"n","type":"shot"},`, 200) + `{}]}`
	if err := gdb.Create(&db.DramaProjectRecord{
		ID:          "proj-heavy",
		Title:       "测试大工程",
		GraphJSON:   hugeGraph,
		ProjectJSON: hugeGraph,
		LayoutJSON:  hugeGraph,
		ShotCount:   12,
	}).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}

	dh := NewDramaHandler(gdb, nil, nil, nil)
	r := gin.New()
	r.Use(gzipMiddleware())
	r.GET("/admin/api/v1/drama/projects", dh.ListProjects)
	r.GET("/events", func(c *gin.Context) {
		c.Header("Content-Type", "text/event-stream")
		c.String(http.StatusOK, "data: hello\n\n")
	})

	// 1. JSON response with Accept-Encoding: gzip -> gzipped & excludes heavy graph_json
	req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/drama/projects", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list projects status = %d", w.Code)
	}
	if w.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected Content-Encoding: gzip, got %q", w.Header().Get("Content-Encoding"))
	}
	gr, err := gzip.NewReader(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	plain, err := io.ReadAll(gr)
	_ = gr.Close()
	if err != nil {
		t.Fatalf("read gzip body: %v", err)
	}
	bodyStr := string(plain)
	if !strings.Contains(bodyStr, "proj-heavy") || !strings.Contains(bodyStr, "测试大工程") {
		t.Fatalf("missing project metadata in response: %s", bodyStr)
	}
	if strings.Contains(bodyStr, "graph_json") || strings.Contains(bodyStr, "project_json") || strings.Contains(bodyStr, "layout_json") {
		t.Fatalf("expected heavy JSON blobs omitted from ListProjects response: %s", bodyStr)
	}

	// 2. c.Data (used by index.html) clears uncompressed Content-Length
	r.GET("/", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(strings.Repeat("<html>hello world</html>", 20)))
	})
	htmlReq := httptest.NewRequest(http.MethodGet, "/", nil)
	htmlReq.Header.Set("Accept-Encoding", "gzip")
	htmlW := httptest.NewRecorder()
	r.ServeHTTP(htmlW, htmlReq)
	if htmlW.Header().Get("Content-Encoding") != "gzip" {
		t.Fatalf("expected Content-Encoding: gzip on /, got %q", htmlW.Header().Get("Content-Encoding"))
	}
	if cl := htmlW.Header().Get("Content-Length"); cl != "" {
		t.Fatalf("expected Content-Length cleared for gzipped c.Data, got %q", cl)
	}
	htmlGr, err := gzip.NewReader(bytes.NewReader(htmlW.Body.Bytes()))
	if err != nil {
		t.Fatalf("html gzip reader: %v", err)
	}
	htmlPlain, err := io.ReadAll(htmlGr)
	_ = htmlGr.Close()
	if err != nil || !strings.Contains(string(htmlPlain), "<html>hello world</html>") {
		t.Fatalf("unexpected decoded html: err=%v body=%q", err, string(htmlPlain))
	}

	// 3. SSE stream is bypassed by gzipMiddleware
	sseReq := httptest.NewRequest(http.MethodGet, "/events", nil)
	sseReq.Header.Set("Accept-Encoding", "gzip")
	sseW := httptest.NewRecorder()
	r.ServeHTTP(sseW, sseReq)
	if sseW.Header().Get("Content-Encoding") != "" {
		t.Fatalf("expected SSE response not gzipped, got %q", sseW.Header().Get("Content-Encoding"))
	}
	if sseW.Body.String() != "data: hello\n\n" {
		t.Fatalf("unexpected SSE body: %q", sseW.Body.String())
	}
}
