package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDashScopeProviderVideoGeneration(t *testing.T) {
	var capturedModel string
	var capturedImgURL string
	var capturedAsyncHeader string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/services/aigc/video-generation/video-synthesis":
			capturedAsyncHeader = r.Header.Get("X-DashScope-Async")
			var payload struct {
				Model string `json:"model"`
				Input struct {
					Prompt string `json:"prompt"`
					ImgURL string `json:"img_url"`
				} `json:"input"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode submit payload: %v", err)
			}
			capturedModel = payload.Model
			capturedImgURL = payload.Input.ImgURL

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"output":{"task_id":"wan-task-999","task_status":"PENDING"}}`))
		case "/tasks/wan-task-999":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"output":{"task_id":"wan-task-999","task_status":"SUCCEEDED","video_url":"https://cdn.example.com/wan-999.mp4"}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := NewDashScopeProvider([]string{"sk-dashscope-test-key"}, srv.URL)
	p.client = srv.Client()

	// Text-to-video resolves wan-video-v1 -> wanx2.1-t2v-turbo
	taskID, err := p.SubmitVideoTask(context.Background(), "wan-video-v1", "雨夜街头霓虹倒影", "", 5)
	if err != nil {
		t.Fatalf("SubmitVideoTask t2v failed: %v", err)
	}
	if taskID != "wan-task-999" {
		t.Fatalf("unexpected taskID: %s", taskID)
	}
	if capturedAsyncHeader != "enable" {
		t.Fatalf("expected X-DashScope-Async=enable, got %q", capturedAsyncHeader)
	}
	if capturedModel != "wanx2.1-t2v-turbo" || capturedImgURL != "" {
		t.Fatalf("unexpected t2v mapping: model=%q img=%q", capturedModel, capturedImgURL)
	}

	// Image-to-video resolves wan-video-v1 -> wanx2.1-i2v-turbo
	_, err = p.SubmitVideoTask(context.Background(), "wan-video-v1", "镜头缓推", "data:image/png;base64,AAA", 5)
	if err != nil {
		t.Fatalf("SubmitVideoTask i2v failed: %v", err)
	}
	if capturedModel != "wanx2.1-i2v-turbo" || !strings.HasPrefix(capturedImgURL, "data:image/png") {
		t.Fatalf("unexpected i2v mapping: model=%q img=%q", capturedModel, capturedImgURL)
	}

	status, videoURL, progress, err := p.QueryVideoTask(context.Background(), "wan-task-999")
	if err != nil {
		t.Fatalf("QueryVideoTask failed: %v", err)
	}
	if status != "SUCCEEDED" || videoURL != "https://cdn.example.com/wan-999.mp4" || progress != 100 {
		t.Fatalf("unexpected query result: status=%s url=%s progress=%d", status, videoURL, progress)
	}
}
