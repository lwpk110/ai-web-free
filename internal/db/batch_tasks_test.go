package db

import (
	"testing"
	"time"
)

func TestBatchTasksDBCRUD(t *testing.T) {
	database, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}

	// 1. 测试 nil 处理
	if err := SaveBatchTaskToDB(nil, nil); err != nil {
		t.Errorf("expected nil error on nil params, got %v", err)
	}
	latest, err := GetLatestActiveBatchTaskFromDB(nil, "")
	if err != nil || latest != nil {
		t.Errorf("expected nil result on nil DB, got %v, %v", latest, err)
	}

	// 2. 插入任务 1
	task1 := &BatchActionTaskRecord{
		ID:          "task-db-001",
		Provider:    "chatgpt",
		Action:      "probe",
		ActionLabel: "批量测试",
		Status:      "running",
		Total:       10,
		Completed:   3,
		Success:     3,
		Failed:      0,
		Results:     `[{"id":"acc-1","ok":true}]`,
		Dismissed:   false,
		CreatedAt:   time.Now().Add(-1 * time.Minute),
	}
	if err := SaveBatchTaskToDB(database, task1); err != nil {
		t.Fatalf("SaveBatchTaskToDB failed: %v", err)
	}

	// 3. 查询当前活跃未 dismissed 任务
	active, err := GetLatestActiveBatchTaskFromDB(database, "chatgpt")
	if err != nil || active == nil {
		t.Fatalf("GetLatestActiveBatchTaskFromDB failed: %v", err)
	}
	if active.ID != "task-db-001" || active.Total != 10 {
		t.Errorf("unexpected task retrieved: %+v", active)
	}

	// 4. 更新任务 1 为已完成
	task1.Status = "completed"
	task1.Completed = 10
	task1.Success = 9
	task1.Failed = 1
	if err := SaveBatchTaskToDB(database, task1); err != nil {
		t.Fatalf("SaveBatchTaskToDB update failed: %v", err)
	}
	active, err = GetLatestActiveBatchTaskFromDB(database, "chatgpt")
	if err != nil || active == nil || active.Status != "completed" {
		t.Fatalf("expected completed status, got: %+v", active)
	}

	// 5. 测试 Dismiss
	if err := DismissBatchTaskInDB(database, "task-db-001"); err != nil {
		t.Fatalf("DismissBatchTaskInDB failed: %v", err)
	}
	activeAfterDismiss, err := GetLatestActiveBatchTaskFromDB(database, "chatgpt")
	if err != nil {
		t.Fatalf("query after dismiss failed: %v", err)
	}
	if activeAfterDismiss != nil {
		t.Errorf("expected nil after dismiss, got %+v", activeAfterDismiss)
	}

	// 6. 测试列表查询
	tasks, err := ListRecentBatchTasksFromDB(database, "chatgpt", 10)
	if err != nil {
		t.Fatalf("ListRecentBatchTasksFromDB failed: %v", err)
	}
	if len(tasks) != 1 || !tasks[0].Dismissed {
		t.Errorf("expected 1 dismissed task in list, got %+v", tasks)
	}
}
