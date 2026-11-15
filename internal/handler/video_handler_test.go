package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/config"
	"ai-web-free/internal/db"
	"ai-web-free/internal/provider"
	"ai-web-free/internal/proxy"

	"github.com/gin-gonic/gin"
)

func setupVideoTestRouter(t *testing.T) *gin.Engine {
	gin.SetMode(gin.TestMode)

	// 初始化内存 SQLite DB
	_, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("failed to init db: %v", err)
	}

	reg := provider.NewRegistry()
	chatgptPlugin := provider.NewChatGPTProvider(accounts.NewPool(nil), proxy.NewPool(nil), &config.Config{}, nil)
	provider.RegisterDefaultProviders(reg, chatgptPlugin)

	vh := NewVideoHandler(reg, nil)

	r := gin.New()
	r.GET("/v1/videos/models", vh.ListModels)
	r.POST("/v1/videos/generations", vh.Generations)
	r.GET("/v1/videos/tasks/:id", vh.GetTask)
	r.GET("/v1/videos/:id", vh.GetTask)
	r.GET("/admin/api/v1/multimodal/video-tasks", vh.ListTasks)

	return r
}

func TestVideoListModels(t *testing.T) {
	r := setupVideoTestRouter(t)

	req, _ := http.NewRequest(http.MethodGet, "/v1/videos/models", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	var resp struct {
		Object string           `json:"object"`
		Data   []VideoModelInfo `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal models response: %v", err)
	}

	if len(resp.Data) < 3 {
		t.Fatalf("expected at least 3 video models, got %d", len(resp.Data))
	}

	hasCog := false
	hasMuse := false
	for _, m := range resp.Data {
		if m.ID == "THUDM/CogVideoX-5b" {
			hasCog = true
		}
		if m.ID == "muse-video" {
			hasMuse = true
		}
	}
	if !hasCog || !hasMuse {
		t.Fatalf("missing CogVideoX or Muse in models list: %+v", resp.Data)
	}
}

func TestVideoGenerationsAndQuery(t *testing.T) {
	r := setupVideoTestRouter(t)

	// 1. 提交任务
	genReq := VideoGenerationRequest{
		Model:    "THUDM/CogVideoX-5b",
		Prompt:   "A cinematic shot of cherry blossoms falling in spring, 4k",
		Duration: 5,
	}
	bodyBytes, _ := json.Marshal(genReq)

	req, _ := http.NewRequest(http.MethodPost, "/v1/videos/generations", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d: %s", w.Code, w.Body.String())
	}

	var submitResp struct {
		TaskID   string `json:"task_id"`
		Model    string `json:"model"`
		Provider string `json:"provider"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &submitResp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if submitResp.TaskID == "" || submitResp.Provider != "siliconflow" {
		t.Fatalf("unexpected submit resp: %+v", submitResp)
	}

	// 2. 轮询状态 /v1/videos/tasks/:id
	queryReq, _ := http.NewRequest(http.MethodGet, "/v1/videos/tasks/"+submitResp.TaskID, nil)
	queryW := httptest.NewRecorder()
	r.ServeHTTP(queryW, queryReq)

	if queryW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for query, got %d", queryW.Code)
	}

	// 3. 轮询状态 /v1/videos/:id
	stdReq, _ := http.NewRequest(http.MethodGet, "/v1/videos/"+submitResp.TaskID, nil)
	stdW := httptest.NewRecorder()
	r.ServeHTTP(stdW, stdReq)

	if stdW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for std query, got %d", stdW.Code)
	}

	// 4. 等待异步任务模拟完成 (4-5s)
	time.Sleep(5 * time.Second)

	finalReq, _ := http.NewRequest(http.MethodGet, "/v1/videos/"+submitResp.TaskID, nil)
	finalW := httptest.NewRecorder()
	r.ServeHTTP(finalW, finalReq)

	var finalTask VideoTask
	_ = json.Unmarshal(finalW.Body.Bytes(), &finalTask)
	if finalTask.Status != "SUCCEEDED" || finalTask.VideoURL == "" {
		t.Fatalf("expected task SUCCEEDED with video url, got: %+v", finalTask)
	}

	// 5. 管理端列表验证
	adminReq, _ := http.NewRequest(http.MethodGet, "/admin/api/v1/multimodal/video-tasks", nil)
	adminW := httptest.NewRecorder()
	r.ServeHTTP(adminW, adminReq)

	if adminW.Code != http.StatusOK {
		t.Fatalf("expected 200 OK for admin list, got %d", adminW.Code)
	}
	var adminResp struct {
		Tasks []*VideoTask `json:"tasks"`
		Total int          `json:"total"`
	}
	_ = json.Unmarshal(adminW.Body.Bytes(), &adminResp)
	if adminResp.Total < 1 {
		t.Fatalf("expected at least 1 task in admin list, got %d", adminResp.Total)
	}
}

func TestVideoMetaMuseGeneration(t *testing.T) {
	r := setupVideoTestRouter(t)

	genReq := VideoGenerationRequest{
		Model:    "muse-video",
		Prompt:   "A character speaking with cinematic facial expressions and natural audio",
		Duration: 5,
	}
	bodyBytes, _ := json.Marshal(genReq)

	req, _ := http.NewRequest(http.MethodPost, "/v1/videos/generations", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202 Accepted, got %d", w.Code)
	}

	var submitResp struct {
		TaskID   string `json:"task_id"`
		Provider string `json:"provider"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &submitResp)
	if submitResp.Provider != "meta_muse" {
		t.Fatalf("expected provider meta_muse, got %s", submitResp.Provider)
	}
}
