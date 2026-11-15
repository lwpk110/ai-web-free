package handler

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ai-web-free/internal/db"
	"ai-web-free/internal/storage"

	"github.com/gin-gonic/gin"
)

func setupTestGalleryHandler(t *testing.T) (*GalleryHandler, *gin.Engine) {
	gin.SetMode(gin.TestMode)
	database, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	engine := storage.NewMemoryStorageEngine()
	handler := NewGalleryHandler(database, engine)

	router := gin.New()
	adminGroup := router.Group("/admin/api/v1")
	{
		adminGroup.GET("/gallery", handler.ListWorks)
		adminGroup.POST("/gallery", handler.SaveWork)
		adminGroup.POST("/gallery/batch-migrate", handler.BatchMigrate)
		adminGroup.PUT("/gallery/:id/metadata", handler.UpdateMetadata)
		adminGroup.PUT("/gallery/:id/favorite", handler.ToggleFavorite)
		adminGroup.DELETE("/gallery/:id", handler.DeleteWork)
		adminGroup.DELETE("/gallery", handler.ClearWorks)

		adminGroup.GET("/chat/messages", handler.ListChatMessages)
		adminGroup.POST("/chat/messages", handler.SaveChatMessages)
		adminGroup.DELETE("/chat/messages", handler.ClearChatMessages)

		adminGroup.GET("/prompts/favorites", handler.ListPromptFavorites)
		adminGroup.POST("/prompts/favorites/toggle", handler.TogglePromptFavorite)
		adminGroup.POST("/prompts/favorites/sync", handler.SyncPromptFavorites)
	}
	router.GET("/storage/images/:filename", handler.ServeStaticImage)

	return handler, router
}

func TestGalleryMetadataAndFilters(t *testing.T) {
	_, router := setupTestGalleryHandler(t)
	fakeB64 := base64.StdEncoding.EncodeToString([]byte("gallery-image"))
	items := []SaveWorkRequest{
		{ID: "gallery-character", Prompt: "portrait of Lin", Model: "model-a", Size: "1024x1792", Category: "character_portrait", Source: "drama_canvas", ProjectID: "project-1", ProjectTitle: "Moon Story", RoleName: "Lin", Tags: []string{"lead", "portrait"}, B64JSON: fakeB64},
		{ID: "gallery-general", Prompt: "mountain landscape", Model: "model-b", Size: "1024x1024", B64JSON: fakeB64},
	}
	for _, item := range items {
		body, _ := json.Marshal(item)
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/gallery", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("save gallery work failed: %d %s", w.Code, w.Body.String())
		}
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/admin/api/v1/gallery?category=character_portrait&source=drama_canvas&project_id=project-1&q=portrait", nil)
	router.ServeHTTP(w, req)
	var list struct {
		Items []db.GalleryWork `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil || w.Code != http.StatusOK || list.Total != 1 || len(list.Items) != 1 {
		t.Fatalf("filtered gallery result mismatch: status=%d body=%s err=%v", w.Code, w.Body.String(), err)
	}
	if list.Items[0].Source != "drama_canvas" || list.Items[0].ProjectTitle != "Moon Story" || list.Items[0].RoleName != "Lin" || len(list.Items[0].Tags) != 2 {
		t.Fatalf("gallery metadata did not round-trip: %+v", list.Items[0])
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/admin/api/v1/gallery?category=character_portrait&q=mountain", nil)
	router.ServeHTTP(w, req)
	list = struct {
		Items []db.GalleryWork `json:"items"`
		Total int              `json:"total"`
	}{}
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if list.Total != 0 {
		t.Fatalf("keyword search escaped category filter: %+v", list.Items)
	}

	updateBody, _ := json.Marshal(map[string]any{"category": "scene_anchor", "tags": []string{"night", "Night", "rain"}})
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/admin/api/v1/gallery/gallery-character/metadata", bytes.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	var updated db.GalleryWork
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil || w.Code != http.StatusOK {
		t.Fatalf("update gallery metadata failed: status=%d body=%s err=%v", w.Code, w.Body.String(), err)
	}
	if updated.Category != "scene_anchor" || len(updated.Tags) != 2 || updated.Tags[0] != "night" || updated.Tags[1] != "rain" {
		t.Fatalf("gallery metadata normalization mismatch: %+v", updated)
	}

	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/admin/api/v1/gallery?q=lead", nil)
	router.ServeHTTP(w, req)
	list = struct {
		Items []db.GalleryWork `json:"items"`
		Total int              `json:"total"`
	}{}
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if list.Total != 0 {
		t.Fatalf("updated tags should replace old tags; got %d matching works", list.Total)
	}
}

func TestGalleryWorksCRUD(t *testing.T) {
	_, router := setupTestGalleryHandler(t)

	// 1. Save single work with base64
	fakeB64 := base64.StdEncoding.EncodeToString([]byte("fake-image-bytes"))
	reqBody := SaveWorkRequest{
		ID:        "img-test-1",
		Prompt:    "A futuristic glass greenhouse",
		Model:     "gpt-image-2.5-sunburst",
		Size:      "1024x1024",
		Quality:   "hd",
		Mode:      "generations",
		B64JSON:   fakeB64,
		LatencyMs: 1500,
	}
	bodyBytes, _ := json.Marshal(reqBody)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/gallery", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("POST /gallery failed with %d: %s", w.Code, w.Body.String())
	}

	var saved db.GalleryWork
	_ = json.Unmarshal(w.Body.Bytes(), &saved)
	if saved.ID != "img-test-1" || saved.Prompt != reqBody.Prompt {
		t.Errorf("saved mismatch: %+v", saved)
	}

	// 2. Query list
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/admin/api/v1/gallery?page=1&page_size=10", nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /gallery failed: %d", w.Code)
	}
	var listRes struct {
		Items []db.GalleryWork `json:"items"`
		Total int              `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &listRes)
	if listRes.Total != 1 || len(listRes.Items) != 1 {
		t.Fatalf("Total = %d, want 1", listRes.Total)
	}

	// 3. Toggle favorite
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPut, "/admin/api/v1/gallery/img-test-1/favorite", nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("Toggle favorite failed: %d", w.Code)
	}
	var toggled db.GalleryWork
	_ = json.Unmarshal(w.Body.Bytes(), &toggled)
	if !toggled.IsFavorite {
		t.Errorf("IsFavorite should be true")
	}

	// 4. Serve static image
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/storage/images/img-test-1.png", nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /storage/images/img-test-1.png failed: %d", w.Code)
	}
	if w.Header().Get("Cache-Control") == "" {
		t.Errorf("Cache-Control header missing")
	}

	// 5. Delete work
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/admin/api/v1/gallery/img-test-1", nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE /gallery/img-test-1 failed: %d", w.Code)
	}

	// Verify empty list
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/admin/api/v1/gallery", nil)
	router.ServeHTTP(w, req)
	_ = json.Unmarshal(w.Body.Bytes(), &listRes)
	if listRes.Total != 0 {
		t.Errorf("Total after delete = %d, want 0", listRes.Total)
	}
}

func TestGalleryBatchMigrate(t *testing.T) {
	_, router := setupTestGalleryHandler(t)

	// Save one first
	fakeB64 := base64.StdEncoding.EncodeToString([]byte("img1"))
	batchReq := BatchMigrateRequest{
		Items: []SaveWorkRequest{
			{ID: "img-batch-1", Prompt: "Prompt 1", B64JSON: fakeB64},
			{ID: "img-batch-2", Prompt: "Prompt 2", B64JSON: fakeB64},
		},
	}
	body, _ := json.Marshal(batchReq)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/gallery/batch-migrate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Batch migrate failed: %d %s", w.Code, w.Body.String())
	}
	var res struct {
		Migrated int `json:"migrated"`
		Skipped  int `json:"skipped"`
		Total    int `json:"total"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res.Migrated != 2 || res.Skipped != 0 {
		t.Errorf("Batch migrate res: %+v", res)
	}

	// Re-run batch migrate with duplicate: should skip
	batchReq.Items = append(batchReq.Items, SaveWorkRequest{
		ID: "img-batch-3", Prompt: "Prompt 3", B64JSON: fakeB64,
	})
	body, _ = json.Marshal(batchReq)
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/admin/api/v1/gallery/batch-migrate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res.Migrated != 1 || res.Skipped != 2 {
		t.Errorf("Expected 1 migrated, 2 skipped, got %+v", res)
	}
}

func TestChatMessagesPersistence(t *testing.T) {
	_, router := setupTestGalleryHandler(t)

	// 1. Save messages
	saveReq := SaveChatMessagesRequest{
		SessionKey: "default",
		Messages: []ChatMessageItem{
			{ID: "msg-1", Role: "user", Content: "Hello", Timestamp: time.Now().UnixMilli()},
			{ID: "msg-2", Role: "assistant", Content: "World", Timestamp: time.Now().UnixMilli()},
		},
	}
	body, _ := json.Marshal(saveReq)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/chat/messages", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("POST /chat/messages failed: %d", w.Code)
	}

	// 2. Query messages
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/admin/api/v1/chat/messages?session_key=default", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /chat/messages failed: %d", w.Code)
	}
	var res struct {
		Messages []db.ChatMessageRecord `json:"messages"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if len(res.Messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(res.Messages))
	}

	// 3. Clear messages
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodDelete, "/admin/api/v1/chat/messages?session_key=default", nil)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("DELETE /chat/messages failed: %d", w.Code)
	}
}

func TestPromptFavoritesPersistence(t *testing.T) {
	_, router := setupTestGalleryHandler(t)

	// 1. Toggle favorite
	body, _ := json.Marshal(map[string]string{"id": "civitai-101"})
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/prompts/favorites/toggle", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Toggle favorite failed: %d", w.Code)
	}
	var toggleRes struct {
		IsFavorite  bool     `json:"is_favorite"`
		FavoriteIDs []string `json:"favorite_ids"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &toggleRes)
	if !toggleRes.IsFavorite || len(toggleRes.FavoriteIDs) != 1 {
		t.Errorf("Unexpected toggle result: %+v", toggleRes)
	}

	// 2. Sync favorites
	syncBody, _ := json.Marshal(map[string][]string{"ids": {"civitai-102", "lexica-201"}})
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/admin/api/v1/prompts/favorites/sync", bytes.NewReader(syncBody))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Sync favorites failed: %d", w.Code)
	}

	// 3. List favorites
	w = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/admin/api/v1/prompts/favorites", nil)
	router.ServeHTTP(w, req)
	var listRes struct {
		FavoriteIDs []string `json:"favorite_ids"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &listRes)
	if len(listRes.FavoriteIDs) != 3 {
		t.Errorf("Expected 3 favorites, got %d (%v)", len(listRes.FavoriteIDs), listRes.FavoriteIDs)
	}
}
