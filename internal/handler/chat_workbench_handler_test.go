package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"ai-web-free/internal/db"
	"ai-web-free/internal/vectorstore"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupTestChatDB(t *testing.T) (*gorm.DB, *ChatWorkbenchHandler, *gin.Engine) {
	gin.SetMode(gin.TestMode)
	database, err := gorm.Open(sqlite.Open(":memory:?_pragma=busy_timeout(5000)"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open test sqlite db: %v", err)
	}

	if err := database.AutoMigrate(
		&db.Conversation{},
		&db.ChatMessageRecord{},
		&db.ChatPreset{},
	); err != nil {
		t.Fatalf("failed to auto migrate: %v", err)
	}

	// 种入内置预设
	database.Create(&db.ChatPreset{
		ID:           "preset-copilot",
		Name:         "平台运维诊断 Copilot",
		SystemPrompt: "你是平台运维诊断专家",
		IsBuiltin:    true,
		SortOrder:    1,
	})

	vs := vectorstore.NewMemoryVectorStore()
	h := NewChatWorkbenchHandler(database, vs)

	r := gin.New()
	admin := r.Group("/admin/api/v1")
	{
		admin.GET("/chat/conversations", h.ListConversations)
		admin.POST("/chat/conversations", h.CreateConversation)
		admin.GET("/chat/conversations/:id", h.GetConversation)
		admin.PATCH("/chat/conversations/:id", h.UpdateConversation)
		admin.DELETE("/chat/conversations/:id", h.DeleteConversation)
		admin.POST("/chat/conversations/:id/messages", h.SaveConversationMessages)
		admin.DELETE("/chat/conversations/:id/messages/:msg_id", h.DeleteConversationMessage)
		admin.GET("/chat/conversations/:id/export", h.ExportConversation)
		admin.POST("/chat/conversations/import", h.ImportConversations)
		admin.GET("/chat/search", h.SearchChat)
		admin.GET("/chat/presets", h.ListPresets)
		admin.POST("/chat/presets", h.SavePreset)
		admin.DELETE("/chat/presets/:id", h.DeletePreset)
	}

	return database, h, r
}

func TestChatWorkbench_ConversationsCRUD(t *testing.T) {
	_, _, r := setupTestChatDB(t)

	// 1. 创建会话
	createBody := map[string]interface{}{
		"title":         "测试微服务架构排障",
		"model":         "gpt-4o",
		"system_prompt": "分析微服务架构",
		"is_pinned":     true,
	}
	bodyData, _ := json.Marshal(createBody)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/chat/conversations", bytes.NewReader(bodyData))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d: %s", w.Code, w.Body.String())
	}

	var created db.Conversation
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("failed to decode created conversation: %v", err)
	}
	if created.Title != "测试微服务架构排障" || !created.IsPinned {
		t.Errorf("unexpected created conversation: %+v", created)
	}

	// 2. 列表查询
	reqList := httptest.NewRequest(http.MethodGet, "/admin/api/v1/chat/conversations?q=微服务", nil)
	wList := httptest.NewRecorder()
	r.ServeHTTP(wList, reqList)
	if wList.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", wList.Code)
	}

	var listResp struct {
		Conversations []db.Conversation `json:"conversations"`
		Total         int               `json:"total"`
	}
	_ = json.Unmarshal(wList.Body.Bytes(), &listResp)
	if listResp.Total != 1 || len(listResp.Conversations) != 1 {
		t.Fatalf("expected total 1, got %d", listResp.Total)
	}

	// 3. 更新会话
	newTitle := "更新后的标题"
	updateBody, _ := json.Marshal(map[string]interface{}{"title": &newTitle})
	reqUpdate := httptest.NewRequest(http.MethodPatch, "/admin/api/v1/chat/conversations/"+created.ID, bytes.NewReader(updateBody))
	reqUpdate.Header.Set("Content-Type", "application/json")
	wUpdate := httptest.NewRecorder()
	r.ServeHTTP(wUpdate, reqUpdate)
	if wUpdate.Code != http.StatusOK {
		t.Fatalf("update failed: %d: %s", wUpdate.Code, wUpdate.Body.String())
	}

	// 4. 获取详情
	reqGet := httptest.NewRequest(http.MethodGet, "/admin/api/v1/chat/conversations/"+created.ID, nil)
	wGet := httptest.NewRecorder()
	r.ServeHTTP(wGet, reqGet)
	if wGet.Code != http.StatusOK {
		t.Fatalf("get failed: %d", wGet.Code)
	}
	var getResp struct {
		Conversation db.Conversation `json:"conversation"`
	}
	_ = json.Unmarshal(wGet.Body.Bytes(), &getResp)
	if getResp.Conversation.Title != "更新后的标题" {
		t.Errorf("expected updated title, got %s", getResp.Conversation.Title)
	}

	// 5. 删除会话
	reqDel := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/chat/conversations/"+created.ID, nil)
	wDel := httptest.NewRecorder()
	r.ServeHTTP(wDel, reqDel)
	if wDel.Code != http.StatusOK {
		t.Fatalf("delete failed: %d", wDel.Code)
	}
}

func TestChatWorkbench_MessagesAndSearch(t *testing.T) {
	_, _, r := setupTestChatDB(t)

	// 创建一个会话
	convID := "conv-search-test"
	saveMsgReq := map[string]interface{}{
		"messages": []map[string]interface{}{
			{
				"id":        "msg-1",
				"role":      "user",
				"content":   "请帮我排查 Kubernetes Pod 为什么一直 CrashLoopBackOff",
				"timestamp": 1700000000000,
			},
			{
				"id":        "msg-2",
				"role":      "assistant",
				"content":   "CrashLoopBackOff 通常是因为容器内主进程退出或 OOMKilled",
				"latencyMs": 450,
				"timestamp": 1700000001000,
			},
		},
	}
	bodyData, _ := json.Marshal(saveMsgReq)
	req := httptest.NewRequest(http.MethodPost, "/admin/api/v1/chat/conversations/"+convID+"/messages", bytes.NewReader(bodyData))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("save messages failed: %d: %s", w.Code, w.Body.String())
	}

	// 获取消息验证
	reqGet := httptest.NewRequest(http.MethodGet, "/admin/api/v1/chat/conversations/"+convID, nil)
	wGet := httptest.NewRecorder()
	r.ServeHTTP(wGet, reqGet)
	if wGet.Code != http.StatusOK {
		t.Fatalf("get conv failed: %d", wGet.Code)
	}
	var getResp struct {
		Conversation db.Conversation        `json:"conversation"`
		Messages     []db.ChatMessageRecord `json:"messages"`
	}
	_ = json.Unmarshal(wGet.Body.Bytes(), &getResp)
	if len(getResp.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(getResp.Messages))
	}
	if getResp.Conversation.MessageCount != 2 {
		t.Errorf("expected MessageCount 2, got %d", getResp.Conversation.MessageCount)
	}

	// 全文检索测试
	reqSearch := httptest.NewRequest(http.MethodGet, "/admin/api/v1/chat/search?q=CrashLoopBackOff", nil)
	wSearch := httptest.NewRecorder()
	r.ServeHTTP(wSearch, reqSearch)
	if wSearch.Code != http.StatusOK {
		t.Fatalf("search failed: %d", wSearch.Code)
	}
	var searchResp struct {
		Hits  []SearchHit `json:"hits"`
		Count int         `json:"count"`
	}
	_ = json.Unmarshal(wSearch.Body.Bytes(), &searchResp)
	if searchResp.Count < 1 {
		t.Fatalf("expected at least 1 search hit, got %d", searchResp.Count)
	}

	// 删除单条消息
	reqDelMsg := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/chat/conversations/"+convID+"/messages/msg-1", nil)
	wDelMsg := httptest.NewRecorder()
	r.ServeHTTP(wDelMsg, reqDelMsg)
	if wDelMsg.Code != http.StatusOK {
		t.Fatalf("delete msg failed: %d", wDelMsg.Code)
	}

	// 再次查询确认只剩 1 条
	reqGet2 := httptest.NewRequest(http.MethodGet, "/admin/api/v1/chat/conversations/"+convID, nil)
	wGet2 := httptest.NewRecorder()
	r.ServeHTTP(wGet2, reqGet2)
	var getResp2 struct {
		Messages []db.ChatMessageRecord `json:"messages"`
	}
	_ = json.Unmarshal(wGet2.Body.Bytes(), &getResp2)
	if len(getResp2.Messages) != 1 {
		t.Errorf("expected 1 message after delete, got %d", len(getResp2.Messages))
	}
}

func TestChatWorkbench_Presets(t *testing.T) {
	_, _, r := setupTestChatDB(t)

	// 1. 获取预设列表（应包含预置的 preset-copilot）
	reqList := httptest.NewRequest(http.MethodGet, "/admin/api/v1/chat/presets", nil)
	wList := httptest.NewRecorder()
	r.ServeHTTP(wList, reqList)
	if wList.Code != http.StatusOK {
		t.Fatalf("presets list failed: %d", wList.Code)
	}
	var listResp struct {
		Presets []db.ChatPreset `json:"presets"`
	}
	_ = json.Unmarshal(wList.Body.Bytes(), &listResp)
	if len(listResp.Presets) == 0 {
		t.Fatalf("expected at least 1 preset")
	}

	// 2. 禁止删除内置预设
	reqDelBuiltin := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/chat/presets/preset-copilot", nil)
	wDelBuiltin := httptest.NewRecorder()
	r.ServeHTTP(wDelBuiltin, reqDelBuiltin)
	if wDelBuiltin.Code != http.StatusForbidden {
		t.Errorf("expected 403 Forbidden when deleting builtin preset, got %d", wDelBuiltin.Code)
	}

	// 3. 创建自定义预设
	saveBody, _ := json.Marshal(map[string]interface{}{
		"name":          "我的自定义助手",
		"system_prompt": "你是一个助手",
	})
	reqSave := httptest.NewRequest(http.MethodPost, "/admin/api/v1/chat/presets", bytes.NewReader(saveBody))
	reqSave.Header.Set("Content-Type", "application/json")
	wSave := httptest.NewRecorder()
	r.ServeHTTP(wSave, reqSave)
	if wSave.Code != http.StatusOK {
		t.Fatalf("save preset failed: %d", wSave.Code)
	}
	var customPreset db.ChatPreset
	_ = json.Unmarshal(wSave.Body.Bytes(), &customPreset)

	// 4. 删除自定义预设
	reqDelCustom := httptest.NewRequest(http.MethodDelete, "/admin/api/v1/chat/presets/"+customPreset.ID, nil)
	wDelCustom := httptest.NewRecorder()
	r.ServeHTTP(wDelCustom, reqDelCustom)
	if wDelCustom.Code != http.StatusOK {
		t.Errorf("delete custom preset failed: %d", wDelCustom.Code)
	}
}

func TestChatWorkbench_ExportImport(t *testing.T) {
	_, _, r := setupTestChatDB(t)

	// 1. 批量导入
	importPayload := map[string]interface{}{
		"conversations": []map[string]interface{}{
			{
				"conversation": map[string]interface{}{
					"id":    "conv-import-1",
					"title": "已导入的分布式架构讨论",
					"model": "gpt-4o",
				},
				"messages": []map[string]interface{}{
					{
						"id":      "m-imp-1",
						"role":    "user",
						"content": "如何实现高可用分布式锁？",
					},
					{
						"id":      "m-imp-2",
						"role":    "assistant",
						"content": "可以使用基于 Raft/ETCD 或 Redis Redlock 算法实现",
					},
				},
			},
		},
	}
	bodyData, _ := json.Marshal(importPayload)
	reqImport := httptest.NewRequest(http.MethodPost, "/admin/api/v1/chat/conversations/import", bytes.NewReader(bodyData))
	reqImport.Header.Set("Content-Type", "application/json")
	wImport := httptest.NewRecorder()
	r.ServeHTTP(wImport, reqImport)
	if wImport.Code != http.StatusOK {
		t.Fatalf("import failed: %d: %s", wImport.Code, wImport.Body.String())
	}

	// 2. 导出为 Markdown
	reqExportMD := httptest.NewRequest(http.MethodGet, "/admin/api/v1/chat/conversations/conv-import-1/export?format=markdown", nil)
	wExportMD := httptest.NewRecorder()
	r.ServeHTTP(wExportMD, reqExportMD)
	if wExportMD.Code != http.StatusOK {
		t.Fatalf("export md failed: %d", wExportMD.Code)
	}
	mdContent := wExportMD.Body.String()
	if !bytes.Contains([]byte(mdContent), []byte("已导入的分布式架构讨论")) || !bytes.Contains([]byte(mdContent), []byte("如何实现高可用分布式锁？")) {
		t.Errorf("unexpected exported markdown: %s", mdContent)
	}

	// 3. 导出为 JSON
	reqExportJSON := httptest.NewRequest(http.MethodGet, "/admin/api/v1/chat/conversations/conv-import-1/export?format=json", nil)
	wExportJSON := httptest.NewRecorder()
	r.ServeHTTP(wExportJSON, reqExportJSON)
	if wExportJSON.Code != http.StatusOK {
		t.Fatalf("export json failed: %d", wExportJSON.Code)
	}
}
