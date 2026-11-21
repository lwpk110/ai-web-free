package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/db"
)

func TestBulkActionTaskAsyncExecutionAndLifecycle(t *testing.T) {
	router, pool, _, _ := setupTestAdminRouter(t, "", nil)

	acct2 := accounts.CreateAccount("test-access-token-2", accounts.TypeFree, accounts.DefaultProfiles)
	acct2.ID = "acc-admin-2"
	acct2.Email = "admin-test-2@example.com"
	acct2.Status = accounts.StatusActive
	pool.AddAccount(acct2)

	// 1. 发起异步批量停用 (async: true)
	req := httptest.NewRequest(http.MethodPost,
		"/admin/api/v1/providers/chatgpt/accounts/bulk-action",
		strings.NewReader(`{"account_ids":["acc-admin-1","acc-admin-2"],"action":"disable","async":true}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var startRes struct {
		Action string          `json:"action"`
		Task   *BulkActionTask `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &startRes); err != nil {
		t.Fatalf("unmarshal start error: %v", err)
	}
	if startRes.Task == nil || startRes.Task.ID == "" {
		t.Fatalf("expected task in response, got nil")
	}
	taskID := startRes.Task.ID
	if startRes.Task.Total != 2 {
		t.Errorf("expected total 2, got %d", startRes.Task.Total)
	}

	// 2. 轮询 GET /bulk-action/task 确认任务可查询
	req = httptest.NewRequest(http.MethodGet,
		"/admin/api/v1/providers/chatgpt/accounts/bulk-action/task",
		nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var getRes struct {
		Task *BulkActionTask `json:"task"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &getRes); err != nil || getRes.Task == nil {
		t.Fatalf("expected valid task from query: %v", err)
	}
	if getRes.Task.ID != taskID {
		t.Errorf("expected task id %s, got %s", taskID, getRes.Task.ID)
	}

	// 3. 检查 Overview 接口是否附带 bulk_task
	req = httptest.NewRequest(http.MethodGet, "/admin/api/v1/overview", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	var ovRes map[string]interface{}
	_ = json.Unmarshal(w.Body.Bytes(), &ovRes)
	if ovRes["bulk_task"] == nil {
		t.Errorf("expected bulk_task in overview")
	}

	// 4. 等待后台协程执行完毕
	deadline := time.Now().Add(3 * time.Second)
	completed := false
	for time.Now().Before(deadline) {
		req = httptest.NewRequest(http.MethodGet,
			"/admin/api/v1/providers/chatgpt/accounts/bulk-action/task",
			nil)
		w = httptest.NewRecorder()
		router.ServeHTTP(w, req)
		var checkRes struct {
			Task *BulkActionTask `json:"task"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &checkRes)
		if checkRes.Task != nil && checkRes.Task.Status == "completed" {
			completed = true
			if checkRes.Task.Success != 2 {
				t.Errorf("expected 2 success, got %d", checkRes.Task.Success)
			}
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !completed {
		t.Fatalf("task did not complete in time")
	}

	// 5. 测试 DismissTask
	req = httptest.NewRequest(http.MethodPost,
		"/admin/api/v1/providers/chatgpt/tasks/dismiss",
		strings.NewReader(`{"kind":"bulk","task_id":"`+taskID+`"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("dismiss failed: %s", w.Body.String())
	}

	// 再次查询 overview，已关闭的 task 应该为 nil
	req = httptest.NewRequest(http.MethodGet, "/admin/api/v1/overview", nil)
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	_ = json.Unmarshal(w.Body.Bytes(), &ovRes)
	if ovRes["bulk_task"] != nil {
		t.Errorf("expected bulk_task to be nil after dismiss, got %+v", ovRes["bulk_task"])
	}
}

func TestBulkActionTaskPersistenceAndSelfHealing(t *testing.T) {
	testDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	router, pool, _, _ := setupTestAdminRouter(t, "", nil)

	acct := accounts.CreateAccount("test-access-token-db", accounts.TypeFree, accounts.DefaultProfiles)
	acct.ID = "acc-db-1"
	acct.Email = "db-test@example.com"
	acct.Status = accounts.StatusActive
	pool.AddAccount(acct)

	// 1. 发起同步操作并检查入库 (async: false 或缺省)
	req := httptest.NewRequest(http.MethodPost,
		"/admin/api/v1/providers/chatgpt/accounts/bulk-action",
		strings.NewReader(`{"account_ids":["acc-db-1"],"action":"disable"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// 2. 校验数据库中是否已有该任务记录
	rec, err := db.GetLatestActiveBatchTaskFromDB(testDB, "chatgpt")
	if err != nil || rec == nil {
		t.Fatalf("expected batch task in DB, err: %v", err)
	}
	if rec.Action != "disable" || rec.Status != "completed" || rec.Total != 1 {
		t.Errorf("unexpected record in DB: %+v", rec)
	}
	// 先将第一个任务 dismiss，避免干扰后续查询
	_ = db.DismissBatchTaskInDB(testDB, rec.ID)

	// 3. 模拟服务重启场景：向 DB 写入一条处于 running 状态的任务记录（模拟意外宕机中断）
	crashedTask := &db.BatchActionTaskRecord{
		ID:                "crashed-task-1",
		Provider:          "chatgpt",
		Action:            "refresh",
		ActionLabel:       "批量刷新",
		Status:            "running",
		Total:             5,
		Completed:         2,
		Success:           2,
		CurrentAccountID:  "acc-crashed",
		CurrentPhaseLabel: "刷新凭据中...",
		CreatedAt:         time.Now().Add(10 * time.Second),
		UpdatedAt:         time.Now().Add(10 * time.Second),
	}
	if err := db.SaveBatchTaskToDB(testDB, crashedTask); err != nil {
		t.Fatalf("failed to insert crashed task: %v", err)
	}

	// 实例化全新 handler（模拟重启后的服务内存为空），调用 ActiveBulkActionTask()
	freshHandler := &AdminHandler{}
	recovered := freshHandler.ActiveBulkActionTask()
	if recovered == nil {
		t.Fatalf("expected recovered task from DB")
	}
	if recovered.ID != "crashed-task-1" {
		t.Errorf("expected crashed-task-1, got %s", recovered.ID)
	}
	if recovered.Status != "cancelled" {
		t.Errorf("expected status to be auto-healed to 'cancelled', got %s", recovered.Status)
	}

	// 验证自愈后数据库中的记录也被更新为 cancelled
	var dbRecord db.BatchActionTaskRecord
	if err := testDB.First(&dbRecord, "id = ?", "crashed-task-1").Error; err != nil {
		t.Fatalf("query record failed: %v", err)
	}
	if dbRecord.Status != "cancelled" {
		t.Errorf("expected record in DB to be cancelled, got %s", dbRecord.Status)
	}

	// 4. 测试 Dismiss 接口同步更新数据库
	req = httptest.NewRequest(http.MethodPost,
		"/admin/api/v1/providers/chatgpt/tasks/dismiss",
		strings.NewReader(`{"kind":"bulk","task_id":"crashed-task-1"}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("dismiss failed: %s", w.Body.String())
	}

	// 校验 DB 中的 Dismissed 已置为 true
	if err := testDB.First(&dbRecord, "id = ?", "crashed-task-1").Error; err != nil {
		t.Fatalf("query record after dismiss failed: %v", err)
	}
	if !dbRecord.Dismissed {
		t.Errorf("expected record in DB to be dismissed")
	}
}
