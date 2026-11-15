package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ai-web-free/internal/provider"
	"ai-web-free/internal/storage"

	"github.com/gin-gonic/gin"
)

func TestVideoDramaNoMockFallbackAndWanVideoStream(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// 1. NoMockFallback returns 424 Failed Dependency when provider has zero active keys.
	emptyReg := provider.NewRegistry()
	emptyReg.Register(provider.NewDashScopeProvider(nil, "http://127.0.0.1:0"))
	vhNoCreds := NewVideoHandler(emptyReg, nil)

	rEmpty := gin.New()
	rEmpty.POST("/v1/videos/generations", vhNoCreds.Generations)
	rEmpty.GET("/v1/videos/models", vhNoCreds.ListModels)

	// Confirm wan-video-v1 is registered in SupportedVideoModels.
	modReq := httptest.NewRequest(http.MethodGet, "/v1/videos/models", nil)
	modResp := httptest.NewRecorder()
	rEmpty.ServeHTTP(modResp, modReq)
	if modResp.Code != http.StatusOK || !strings.Contains(modResp.Body.String(), "wan-video-v1") {
		t.Fatalf("expected wan-video-v1 in /v1/videos/models, status=%d body=%s", modResp.Code, modResp.Body.String())
	}

	noMockBody := `{"model":"wan-video-v1","prompt":"大雨霓虹夜景","no_mock_fallback":true}`
	req424 := httptest.NewRequest(http.MethodPost, "/v1/videos/generations", strings.NewReader(noMockBody))
	req424.Header.Set("Content-Type", "application/json")
	resp424 := httptest.NewRecorder()
	rEmpty.ServeHTTP(resp424, req424)
	if resp424.Code != http.StatusFailedDependency || !strings.Contains(resp424.Body.String(), "no_video_provider") {
		t.Fatalf("expected 424 with code=no_video_provider, got %d body=%s", resp424.Code, resp424.Body.String())
	}

	// 2. Wired DashScope + local /storage/images/... reference + same-origin StreamVideoFile cache.
	fakeVideoBytes := []byte("FTYPMP4-DRAMA-VIDEO-BYTES")
	var receivedImgURL string
	var mockSrvURL string
	dsSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/services/aigc/video-generation/video-synthesis":
			var body struct {
				Input struct {
					ImgURL string `json:"img_url"`
				} `json:"input"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			receivedImgURL = body.Input.ImgURL
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"output":{"task_id":"ds-task-77","task_status":"PENDING"}}`))
		case "/tasks/ds-task-77":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"output":{"task_id":"ds-task-77","task_status":"SUCCEEDED","video_url":"` + mockSrvURL + `/cdn/ds-77.mp4"}}`))
		case "/cdn/ds-77.mp4":
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write(fakeVideoBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer dsSrv.Close()
	mockSrvURL = dsSrv.URL

	mem := storage.NewMemoryStorageEngine()
	refURL, _, err := mem.SaveInCategory(context.Background(), "images", "shot-1-keyframe", []byte("PNGDATA"), "image/png")
	if err != nil {
		t.Fatalf("SaveInCategory failed: %v", err)
	}

	reg := provider.NewRegistry()
	dsProvider := provider.NewDashScopeProvider([]string{"sk-test"}, dsSrv.URL)
	reg.Register(dsProvider)

	vh := NewVideoHandler(reg, nil)
	vh.SetStorageEngine(mem)

	r := gin.New()
	r.POST("/v1/videos/generations", vh.Generations)
	r.GET("/v1/videos/files/:id", vh.StreamVideoFile)
	r.GET("/storage/videos/:id", vh.StreamVideoFile)

	genPayload, _ := json.Marshal(map[string]interface{}{
		"model":            "wan-video-v1",
		"prompt":           "镜头缓慢推近角色面部",
		"image_url":        refURL,
		"wait":             true,
		"no_mock_fallback": true,
	})
	genReq := httptest.NewRequest(http.MethodPost, "/v1/videos/generations", bytes.NewReader(genPayload))
	genReq.Header.Set("Content-Type", "application/json")
	genResp := httptest.NewRecorder()
	r.ServeHTTP(genResp, genReq)
	if genResp.Code != http.StatusOK {
		t.Fatalf("expected wait=true wan-video-v1 200 OK, got %d body=%s", genResp.Code, genResp.Body.String())
	}

	var completed VideoTask
	if err := json.Unmarshal(genResp.Body.Bytes(), &completed); err != nil {
		t.Fatalf("decode completed task: %v", err)
	}
	if completed.Status != "SUCCEEDED" || completed.LocalURL == "" {
		t.Fatalf("unexpected completed task: %+v", completed)
	}
	if !strings.HasPrefix(receivedImgURL, "data:image/png;base64,") {
		t.Fatalf("expected local /storage/images URL resolved to data URI before DashScope submit, got %q", receivedImgURL)
	}

	// StreamVideoFile fetches remote MP4 once, caches into MemoryStorageEngine, and serves same-origin CORS headers.
	streamReq := httptest.NewRequest(http.MethodGet, completed.LocalURL, nil)
	streamResp := httptest.NewRecorder()
	r.ServeHTTP(streamResp, streamReq)
	if streamResp.Code != http.StatusOK || !bytes.Equal(streamResp.Body.Bytes(), fakeVideoBytes) {
		t.Fatalf("expected streamed mp4 bytes, status=%d len=%d", streamResp.Code, streamResp.Body.Len())
	}
	if streamResp.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("expected CORS Access-Control-Allow-Origin: *, got %q", streamResp.Header().Get("Access-Control-Allow-Origin"))
	}
	cached, err := mem.Read(context.Background(), "videos/"+completed.TaskID+".mp4")
	if err != nil || !bytes.Equal(cached, fakeVideoBytes) {
		t.Fatalf("expected cached video in storage engine, err=%v len=%d", err, len(cached))
	}
}
