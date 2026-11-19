package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ai-web-free/internal/db"
	"ai-web-free/internal/deals"
	"github.com/gin-gonic/gin"
)

func TestDealsHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test-deals-handler.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("init db failed: %v", err)
	}
	sqlDB, err := database.DB()
	if err == nil && sqlDB != nil {
		defer sqlDB.Close()
	}

	srv := deals.NewService(database, 4*time.Hour)
	deals.SetService(srv)
	h := NewDealsHandler(srv)

	r := gin.New()
	admin := r.Group("/admin/api/v1")
	{
		admin.GET("/deals", h.ListDeals)
		admin.POST("/deals/refresh", h.RefreshDeals)
		admin.GET("/deals/status", h.GetDealsStatus)
		admin.POST("/deals/chat", h.Chat)
	}

	// 1. 触发刷新
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/admin/api/v1/deals/refresh", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("refresh expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if err := database.Create(&db.FreebieDeal{
		ID: "handler-hot-test", Title: "测试热榜线报", URL: "https://example.com/handler-hot",
		Source: "linuxdo", Likes: 30, Replies: 16, Views: 900, Score: 55, IsHot: true,
		PublishedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("insert hot fixture failed: %v", err)
	}

	// 2. 查询列表
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/admin/api/v1/deals?sort=hot&hot_only=true", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("list deals expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var listResp struct {
		Items []deals.Deal     `json:"items"`
		Total int64            `json:"total"`
		Query deals.DealsQuery `json:"query"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listResp); err != nil {
		t.Fatalf("unmarshal list failed: %v", err)
	}
	if listResp.Total == 0 || len(listResp.Items) == 0 {
		t.Fatalf("expected non-empty items, got total=%d", listResp.Total)
	}
	if !listResp.Query.HotOnly {
		t.Fatal("hot_only query parameter was not passed through")
	}
	for _, item := range listResp.Items {
		if !item.IsHot {
			t.Fatalf("hot-only endpoint returned non-hot deal %q", item.ID)
		}
	}

	// 3. 检查状态
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodGet, "/admin/api/v1/deals/status", nil)
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// 4. 线报 AI 对话支持上下文延续与真实联网搜索选项
	var gotSearch bool
	var gotModel, gotPrompt string
	h.SetChatExecutor(func(_ context.Context, _, userPrompt string, useWebSearch bool, model ...string) (string, error) {
		gotSearch = useWebSearch
		gotPrompt = userPrompt
		if len(model) > 0 {
			gotModel = model[0]
		}
		return "已搜索并附上来源。", nil
	})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest(http.MethodPost, "/admin/api/v1/deals/chat", strings.NewReader(`{"web_search":true,"messages":[{"role":"user","content":"比较免费额度"},{"role":"assistant","content":"可以比较平台额度"},{"role":"user","content":"再比较有效期"}]}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("deals chat expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if !gotSearch || gotModel != "gpt-5-6" {
		t.Fatalf("web search must use ChatGPT search-capable model, got search=%v model=%q", gotSearch, gotModel)
	}
	if !strings.Contains(gotPrompt, "助手：\n可以比较平台额度") || !strings.Contains(gotPrompt, "再比较有效期") {
		t.Fatalf("conversation history missing from prompt: %q", gotPrompt)
	}
	var chatResp struct {
		Answer string `json:"answer"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &chatResp); err != nil || chatResp.Answer == "" {
		t.Fatalf("expected answer in chat response, got %q err=%v", w.Body.String(), err)
	}
}
