package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/config"
	"ai-web-free/internal/provider"

	"github.com/gin-gonic/gin"
)

func setupMultimodalTestRouter() (*gin.Engine, *provider.Registry) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	reg := provider.NewRegistry()
	provider.RegisterDefaultProviders(reg, nil)

	pool := accounts.NewPool(nil)
	cfg := &config.Config{}

	audioHandler := NewAudioHandler(pool, cfg)
	audioHandler.SetRegistry(reg)

	imageHandler := NewImageHandler(pool, cfg)
	imageHandler.SetRegistry(reg)

	videoHandler := NewVideoHandler(reg, nil)

	router.GET("/v1/audio/voices", audioHandler.ListVoices)
	router.POST("/v1/audio/speech", audioHandler.TTS)
	router.POST("/v1/videos/generations", videoHandler.Generations)
	router.GET("/v1/videos/tasks/:id", videoHandler.GetTask)
	router.GET("/admin/api/v1/multimodal/video-tasks", videoHandler.ListTasks)

	return router, reg
}

func TestListVoicesEndpoint(t *testing.T) {
	r, _ := setupMultimodalTestRouter()

	req, _ := http.NewRequest(http.MethodGet, "/v1/audio/voices", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var res struct {
		Voices []provider.VoiceDescriptor `json:"voices"`
		Total  int                        `json:"total"`
	}

	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if res.Total < 10 {
		t.Fatalf("expected at least 10 voices, got %d", res.Total)
	}

	foundJianying := false
	foundCosy := false
	foundEdge := false
	for _, v := range res.Voices {
		if v.ID == "zh_female_shuangkuai" {
			foundJianying = true
		}
		if v.ID == "cosyvoice-v1:alex" {
			foundCosy = true
		}
		if v.ID == "zh-CN-XiaoxiaoNeural" {
			foundEdge = true
		}
	}

	if !foundJianying || !foundCosy || !foundEdge {
		t.Fatalf("missing expected voices in list: jianying=%v, cosy=%v, edge=%v", foundJianying, foundCosy, foundEdge)
	}
}

func TestVideoGenerationsFlow(t *testing.T) {
	r, _ := setupMultimodalTestRouter()

	// 1. 提交视频任务
	reqBody := VideoGenerationRequest{
		Model:    "jimeng-video-2.0",
		Prompt:   "金色的树叶飘落",
		Duration: 5,
		Wait:     false,
	}
	bodyBytes, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest(http.MethodPost, "/v1/videos/generations", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d: %s", w.Code, w.Body.String())
	}

	var submitRes struct {
		TaskID string `json:"task_id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &submitRes); err != nil {
		t.Fatalf("failed to parse submit response: %v", err)
	}

	if submitRes.TaskID == "" || submitRes.Status != "PENDING" {
		t.Fatalf("invalid submit response: %+v", submitRes)
	}

	// 2. 查询视频任务状态
	queryReq, _ := http.NewRequest(http.MethodGet, "/v1/videos/tasks/"+submitRes.TaskID, nil)
	queryW := httptest.NewRecorder()
	r.ServeHTTP(queryW, queryReq)

	if queryW.Code != http.StatusOK {
		t.Fatalf("expected status 200 on task query, got %d", queryW.Code)
	}

	var taskRes VideoTask
	if err := json.Unmarshal(queryW.Body.Bytes(), &taskRes); err != nil {
		t.Fatalf("failed to parse task query response: %v", err)
	}

	if taskRes.TaskID != submitRes.TaskID {
		t.Fatalf("expected task id %s, got %s", submitRes.TaskID, taskRes.TaskID)
	}

	// 3. 管理端任务列表
	listReq, _ := http.NewRequest(http.MethodGet, "/admin/api/v1/multimodal/video-tasks", nil)
	listW := httptest.NewRecorder()
	r.ServeHTTP(listW, listReq)

	if listW.Code != http.StatusOK {
		t.Fatalf("expected status 200 on list tasks, got %d", listW.Code)
	}
}
