package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/db"
	"ai-web-free/internal/provider"
	"ai-web-free/internal/storage"

	"github.com/gin-gonic/gin"
)

func TestDramaHandlerCapabilitiesProjectsAndCache(t *testing.T) {
	gin.SetMode(gin.TestMode)

	gdb, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	mem := storage.NewMemoryStorageEngine()
	reg := provider.NewRegistry()
	reg.Register(provider.NewDashScopeProvider([]string{"sk-ds-1"}, "https://dashscope.aliyuncs.com/api/v1"))
	reg.Register(provider.NewEdgeTTSProvider())

	pool := accounts.NewPool([]*accounts.Account{
		{ID: "cgpt-1", Email: "drama@example.com", Token: "tok-1", Status: accounts.StatusActive},
	})

	dh := NewDramaHandler(gdb, pool, reg, mem)
	gh := NewGalleryHandler(gdb, mem)

	r := gin.New()
	r.GET("/admin/api/v1/drama/capabilities", dh.Capabilities)
	r.GET("/admin/api/v1/drama/projects", dh.ListProjects)
	r.POST("/admin/api/v1/drama/projects", dh.SaveProject)
	r.GET("/admin/api/v1/drama/projects/:id", dh.GetProject)
	r.PUT("/admin/api/v1/drama/projects/:id", dh.SaveProject)
	r.DELETE("/admin/api/v1/drama/projects/:id", dh.DeleteProject)
	r.POST("/admin/api/v1/drama/media/cache", dh.CacheMedia)
	r.GET("/storage/images/:filename", gh.ServeStaticImage)
	r.GET("/storage/audio/:filename", dh.ServeCachedAudio)

	// 1. Capabilities check reflects active ChatGPT + DashScope + zero-config TTS.
	capReq := httptest.NewRequest(http.MethodGet, "/admin/api/v1/drama/capabilities", nil)
	capResp := httptest.NewRecorder()
	r.ServeHTTP(capResp, capReq)
	if capResp.Code != http.StatusOK {
		t.Fatalf("capabilities failed: %d %s", capResp.Code, capResp.Body.String())
	}
	var capPayload map[string]map[string]interface{}
	if err := json.Unmarshal(capResp.Body.Bytes(), &capPayload); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	if capPayload["llm"]["available"] != true || capPayload["video"]["available"] != true || capPayload["tts"]["available"] != true {
		t.Fatalf("unexpected capabilities flags: %+v", capPayload)
	}

	// 2. Project Save / Get / List / Delete round-trip.
	saveBody := `{"id":"proj-canvas-1","title":"霓虹雨夜","genre":"悬疑","aspect_ratio":"9:16","graph_json":"{\"nodes\":[]}","shot_count":3}`
	saveReq := httptest.NewRequest(http.MethodPost, "/admin/api/v1/drama/projects", bytes.NewBufferString(saveBody))
	saveReq.Header.Set("Content-Type", "application/json")
	saveResp := httptest.NewRecorder()
	r.ServeHTTP(saveResp, saveReq)
	if saveResp.Code != http.StatusOK {
		t.Fatalf("save project failed: %d %s", saveResp.Code, saveResp.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/admin/api/v1/drama/projects/proj-canvas-1", nil)
	getResp := httptest.NewRecorder()
	r.ServeHTTP(getResp, getReq)
	if getResp.Code != http.StatusOK || !bytes.Contains(getResp.Body.Bytes(), []byte("霓虹雨夜")) {
		t.Fatalf("get project failed: %d %s", getResp.Code, getResp.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/admin/api/v1/drama/projects", nil)
	listResp := httptest.NewRecorder()
	r.ServeHTTP(listResp, listReq)
	if listResp.Code != http.StatusOK || !bytes.Contains(listResp.Body.Bytes(), []byte("proj-canvas-1")) {
		t.Fatalf("list projects failed: %d %s", listResp.Code, listResp.Body.String())
	}

	// 3. CacheMedia with base64 data URL -> serves cleanly via /storage/audio/:filename & /storage/images/:filename.
	rawAudio := []byte("ID3-TEST-MP3")
	audioDataURL := "data:audio/mpeg;base64," + base64.StdEncoding.EncodeToString(rawAudio)
	cacheAudioReq := httptest.NewRequest(http.MethodPost, "/admin/api/v1/drama/media/cache", bytes.NewBufferString(`{"data_url":"`+audioDataURL+`","category":"audio","id":"bgm-1"}`))
	cacheAudioReq.Header.Set("Content-Type", "application/json")
	cacheAudioResp := httptest.NewRecorder()
	r.ServeHTTP(cacheAudioResp, cacheAudioReq)
	if cacheAudioResp.Code != http.StatusOK {
		t.Fatalf("cache audio failed: %d %s", cacheAudioResp.Code, cacheAudioResp.Body.String())
	}
	var cachedOut struct {
		LocalURL string `json:"local_url"`
	}
	_ = json.Unmarshal(cacheAudioResp.Body.Bytes(), &cachedOut)
	if cachedOut.LocalURL != "/storage/audio/bgm-1.mp3" {
		t.Fatalf("unexpected cached audio url: %s", cachedOut.LocalURL)
	}

	audioFetchReq := httptest.NewRequest(http.MethodGet, cachedOut.LocalURL, nil)
	audioFetchResp := httptest.NewRecorder()
	r.ServeHTTP(audioFetchResp, audioFetchReq)
	if audioFetchResp.Code != http.StatusOK || !bytes.Equal(audioFetchResp.Body.Bytes(), rawAudio) {
		t.Fatalf("serve cached audio failed: %d len=%d", audioFetchResp.Code, audioFetchResp.Body.Len())
	}

	// Also test Delete project.
	delReq := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/drama/projects/proj-canvas-1", nil)
	delResp := httptest.NewRecorder()
	r.ServeHTTP(delResp, delReq)
	if delResp.Code != http.StatusOK {
		t.Fatalf("delete project failed: %d", delResp.Code)
	}
}
