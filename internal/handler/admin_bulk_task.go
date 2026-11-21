package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"ai-web-free/internal/db"
	"ai-web-free/internal/provider"

	"github.com/gin-gonic/gin"
)

// BulkActionTaskItemResult 单个账号在批量操作中的执行回显结果
type BulkActionTaskItemResult struct {
	ID      string `json:"id"`
	Email   string `json:"email,omitempty"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
}

// BulkActionTask 选定账号批量运维操作后台异步任务进度快照
type BulkActionTask struct {
	ID                string                     `json:"id"`
	Provider          string                     `json:"provider"`
	Action            string                     `json:"action"`
	ActionLabel       string                     `json:"action_label"`
	Status            string                     `json:"status"` // "running" | "completed" | "cancelled"
	Total             int                        `json:"total"`
	Completed         int                        `json:"completed"`
	Success           int                        `json:"success"`
	Failed            int                        `json:"failed"`
	Cancelled         int                        `json:"cancelled,omitempty"`
	CurrentAccountID  string                     `json:"current_account_id,omitempty"`
	CurrentEmail      string                     `json:"current_email,omitempty"`
	CurrentPhaseLabel string                     `json:"current_phase_label,omitempty"`
	CurrentStartedAt  string                     `json:"current_started_at,omitempty"`
	QueuedAccountIDs  []string                   `json:"queued_account_ids,omitempty"`
	Results           []BulkActionTaskItemResult `json:"results"`
	CreatedAt         string                     `json:"created_at"`
	UpdatedAt         string                     `json:"updated_at"`
	Dismissed         bool                       `json:"dismissed,omitempty"`
}

func cloneBulkActionTask(t *BulkActionTask) BulkActionTask {
	cp := *t
	if t.QueuedAccountIDs != nil {
		cp.QueuedAccountIDs = append([]string(nil), t.QueuedAccountIDs...)
	}
	if t.Results != nil {
		cp.Results = append([]BulkActionTaskItemResult(nil), t.Results...)
	}
	return cp
}

// BulkActionTaskToRecord 将内存任务对象序列化为可持久化入库的模型
func BulkActionTaskToRecord(task *BulkActionTask) *db.BatchActionTaskRecord {
	if task == nil {
		return nil
	}
	queuedJSON, _ := json.Marshal(task.QueuedAccountIDs)
	resultsJSON, _ := json.Marshal(task.Results)

	createTime, err := time.Parse(time.RFC3339, task.CreatedAt)
	if err != nil {
		createTime = time.Now()
	}
	updateTime, err := time.Parse(time.RFC3339, task.UpdatedAt)
	if err != nil {
		updateTime = time.Now()
	}

	return &db.BatchActionTaskRecord{
		ID:                task.ID,
		Provider:          task.Provider,
		Action:            task.Action,
		ActionLabel:       task.ActionLabel,
		Status:            task.Status,
		Total:             task.Total,
		Completed:         task.Completed,
		Success:           task.Success,
		Failed:            task.Failed,
		Cancelled:         task.Cancelled,
		CurrentAccountID:  task.CurrentAccountID,
		CurrentEmail:      task.CurrentEmail,
		CurrentPhaseLabel: task.CurrentPhaseLabel,
		CurrentStartedAt:  task.CurrentStartedAt,
		QueuedAccountIDs:  string(queuedJSON),
		Results:           string(resultsJSON),
		Dismissed:         task.Dismissed,
		CreatedAt:         createTime,
		UpdatedAt:         updateTime,
	}
}

// RecordToBulkActionTask 将数据库记录反序列化为内存任务结构
func RecordToBulkActionTask(rec *db.BatchActionTaskRecord) *BulkActionTask {
	if rec == nil {
		return nil
	}
	var queued []string
	if strings.TrimSpace(rec.QueuedAccountIDs) != "" {
		_ = json.Unmarshal([]byte(rec.QueuedAccountIDs), &queued)
	}
	var results []BulkActionTaskItemResult
	if strings.TrimSpace(rec.Results) != "" {
		_ = json.Unmarshal([]byte(rec.Results), &results)
	}

	return &BulkActionTask{
		ID:                rec.ID,
		Provider:          rec.Provider,
		Action:            rec.Action,
		ActionLabel:       rec.ActionLabel,
		Status:            rec.Status,
		Total:             rec.Total,
		Completed:         rec.Completed,
		Success:           rec.Success,
		Failed:            rec.Failed,
		Cancelled:         rec.Cancelled,
		CurrentAccountID:  rec.CurrentAccountID,
		CurrentEmail:      rec.CurrentEmail,
		CurrentPhaseLabel: rec.CurrentPhaseLabel,
		CurrentStartedAt:  rec.CurrentStartedAt,
		QueuedAccountIDs:  queued,
		Results:           results,
		CreatedAt:         rec.CreatedAt.Format(time.RFC3339),
		UpdatedAt:         rec.UpdatedAt.Format(time.RFC3339),
		Dismissed:         rec.Dismissed,
	}
}

func saveBulkTaskToDB(task *BulkActionTask) {
	if task == nil {
		return
	}
	database := db.GetDB()
	if database == nil {
		return
	}
	rec := BulkActionTaskToRecord(task)
	_ = db.SaveBatchTaskToDB(database, rec)
}

func bulkActionLabel(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "probe":
		return "批量测试"
	case "refresh":
		return "批量刷新"
	case "delete":
		return "批量移除"
	case "disable":
		return "批量停用"
	case "enable":
		return "批量启用"
	case "standby":
		return "批量转待命"
	case "activate":
		return "批量激活"
	default:
		return action
	}
}

// ActiveBulkActionTask 返回当前运行中或已完成未关闭的批量任务快照（支持从数据库自愈恢复）
func (h *AdminHandler) ActiveBulkActionTask() *BulkActionTask {
	h.bulkTaskMu.Lock()
	defer h.bulkTaskMu.Unlock()

	// 若内存中没有任务，尝试从数据库恢复最近未关闭的战报或任务
	if h.currentBulkTask == nil {
		if database := db.GetDB(); database != nil {
			if rec, err := db.GetLatestActiveBatchTaskFromDB(database, ""); err == nil && rec != nil {
				task := RecordToBulkActionTask(rec)
				// 若从库中查出的任务处于 running 状态，说明是服务重启导致中断，自动纠偏为 cancelled 并更新入库
				if task != nil && task.Status == "running" {
					task.Status = "cancelled"
					task.CurrentAccountID = ""
					task.CurrentPhaseLabel = "服务重启，任务已中止"
					task.UpdatedAt = time.Now().Format(time.RFC3339)
					saveBulkTaskToDB(task)
				}
				h.currentBulkTask = task
			}
		}
	}

	if h.currentBulkTask == nil || h.currentBulkTask.Dismissed {
		return nil
	}
	cp := cloneBulkActionTask(h.currentBulkTask)
	return &cp
}

// DismissBulkActionTask 标记已完成的批量任务卡片为已关闭（内存 + 数据库双向落库）
func (h *AdminHandler) DismissBulkActionTask(taskID string) bool {
	h.bulkTaskMu.Lock()
	defer h.bulkTaskMu.Unlock()
	matched := false
	if h.currentBulkTask != nil && (taskID == "" || h.currentBulkTask.ID == taskID) {
		if h.currentBulkTask.Status != "running" {
			h.currentBulkTask.Dismissed = true
			saveBulkTaskToDB(h.currentBulkTask)
			matched = true
		}
	}
	if taskID != "" {
		if database := db.GetDB(); database != nil {
			_ = db.DismissBatchTaskInDB(database, taskID)
			return true
		}
	}
	return matched
}

// CancelBulkActionTask 取消当前后台批量任务未开始的排队（内存 + 数据库双向落库）
func (h *AdminHandler) CancelBulkActionTask(taskID string) bool {
	h.bulkTaskMu.Lock()
	defer h.bulkTaskMu.Unlock()
	if h.currentBulkTask == nil || h.currentBulkTask.Status != "running" {
		return false
	}
	if taskID != "" && h.currentBulkTask.ID != taskID {
		return false
	}
	if h.bulkCancelCh != nil {
		select {
		case <-h.bulkCancelCh:
		default:
			close(h.bulkCancelCh)
		}
	}
	h.currentBulkTask.Status = "cancelled"
	h.currentBulkTask.UpdatedAt = time.Now().Format(time.RFC3339)
	saveBulkTaskToDB(h.currentBulkTask)
	return true
}

// GetBulkActionTaskHandler 获取当前活跃的批量任务
func (h *AdminHandler) GetBulkActionTaskHandler(c *gin.Context) {
	task := h.ActiveBulkActionTask()
	if task == nil {
		c.JSON(http.StatusOK, gin.H{"task": nil})
		return
	}
	c.JSON(http.StatusOK, gin.H{"task": task})
}

// CancelBulkActionTaskHandler 取消正在运行的批量任务
func (h *AdminHandler) CancelBulkActionTaskHandler(c *gin.Context) {
	var body struct {
		TaskID string `json:"task_id"`
	}
	_ = c.ShouldBindJSON(&body)
	ok := h.CancelBulkActionTask(body.TaskID)
	c.JSON(http.StatusOK, gin.H{"ok": ok})
}

func removeStringFromSlice(slice []string, val string) []string {
	out := make([]string, 0, len(slice))
	for _, s := range slice {
		if s != val {
			out = append(out, s)
		}
	}
	return out
}

func (h *AdminHandler) executeBulkActionAsync(
	task *BulkActionTask,
	providerID string,
	action string,
	ids []string,
	headless bool,
	p provider.ProviderPlugin,
	cancelCh chan struct{},
) {
	pool := h.accountPool()
	label := bulkActionLabel(action)
	now := time.Now()

	// 任务创建时首先持久化入库
	h.bulkTaskMu.Lock()
	saveBulkTaskToDB(task)
	h.bulkTaskMu.Unlock()

	// 初始先将所有排队账号设置状态
	if pool != nil {
		for idx, id := range ids {
			pool.SetAccountActionState(id, action, "queued", "等待"+label, task.ID, idx+1, now)
		}
	}

	type actionWithOpts interface {
		ExecuteAccountActionWithOpts(id, action string, headless bool) (map[string]interface{}, error)
	}

	isNetworkAction := action == "probe" || action == "refresh" || action == "login"
	concurrency := 1
	if isNetworkAction {
		concurrency = 3 // 测活与刷新采用受控并发提升处理速度
	}

	type workItem struct {
		id    string
		email string
	}
	workItems := make([]workItem, 0, len(ids))
	for _, id := range ids {
		email := ""
		if pool != nil {
			if a := pool.GetByID(id); a != nil {
				email = a.Email
			}
		}
		workItems = append(workItems, workItem{id: id, email: email})
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	for _, item := range workItems {
		// 检查是否取消
		select {
		case <-cancelCh:
			h.bulkTaskMu.Lock()
			task.Cancelled++
			task.Completed++
			task.QueuedAccountIDs = removeStringFromSlice(task.QueuedAccountIDs, item.id)
			task.Results = append(task.Results, BulkActionTaskItemResult{
				ID:    item.id,
				Email: item.email,
				OK:    false,
				Error: "任务已由用户手动取消",
			})
			task.UpdatedAt = time.Now().Format(time.RFC3339)
			saveBulkTaskToDB(task)
			h.bulkTaskMu.Unlock()
			if pool != nil {
				pool.SetAccountActionState(item.id, "", "", "", "", 0, time.Time{})
			}
			continue
		default:
		}

		wg.Add(1)
		sem <- struct{}{}

		go func(w workItem) {
			defer wg.Done()
			defer func() { <-sem }()

			// 检查是否在排队中被取消
			select {
			case <-cancelCh:
				h.bulkTaskMu.Lock()
				task.Cancelled++
				task.Completed++
				task.QueuedAccountIDs = removeStringFromSlice(task.QueuedAccountIDs, w.id)
				task.Results = append(task.Results, BulkActionTaskItemResult{
					ID:    w.id,
					Email: w.email,
					OK:    false,
					Error: "任务已由用户手动取消",
				})
				task.UpdatedAt = time.Now().Format(time.RFC3339)
				saveBulkTaskToDB(task)
				h.bulkTaskMu.Unlock()
				if pool != nil {
					pool.SetAccountActionState(w.id, "", "", "", "", 0, time.Time{})
				}
				return
			default:
			}

			startAt := time.Now()
			h.bulkTaskMu.Lock()
			task.CurrentAccountID = w.id
			task.CurrentEmail = w.email
			task.CurrentPhaseLabel = "正在" + label
			task.CurrentStartedAt = startAt.Format(time.RFC3339)
			task.QueuedAccountIDs = removeStringFromSlice(task.QueuedAccountIDs, w.id)
			task.UpdatedAt = time.Now().Format(time.RFC3339)
			saveBulkTaskToDB(task)
			h.bulkTaskMu.Unlock()

			if pool != nil {
				pool.SetAccountActionState(w.id, action, "running", "正在"+label, task.ID, 0, startAt)
			}

			var res map[string]interface{}
			var err error
			if runner, ok := p.(actionWithOpts); ok {
				res, err = runner.ExecuteAccountActionWithOpts(w.id, action, headless)
			} else {
				res, err = p.ExecuteAccountAction(w.id, action)
			}

			h.bulkTaskMu.Lock()
			task.Completed++
			resItem := BulkActionTaskItemResult{
				ID:    w.id,
				Email: w.email,
			}
			if err != nil {
				resItem.OK = false
				resItem.Error = err.Error()
				task.Failed++
			} else {
				resItem.OK = true
				task.Success++
				if msg, ok := res["message"].(string); ok && msg != "" {
					resItem.Message = msg
				}
			}
			task.Results = append(task.Results, resItem)
			task.UpdatedAt = time.Now().Format(time.RFC3339)
			saveBulkTaskToDB(task)
			h.bulkTaskMu.Unlock()

			if pool != nil {
				pool.SetAccountActionState(w.id, "", "", "", "", 0, time.Time{})
			}
		}(item)
	}

	wg.Wait()

	h.bulkTaskMu.Lock()
	if task.Status != "cancelled" {
		task.Status = "completed"
	}
	task.CurrentAccountID = ""
	task.CurrentEmail = ""
	task.CurrentPhaseLabel = ""
	task.CurrentStartedAt = ""
	task.QueuedAccountIDs = nil
	task.UpdatedAt = time.Now().Format(time.RFC3339)
	saveBulkTaskToDB(task)
	totalCount := task.Total
	successCount := task.Success
	failedCount := task.Failed
	h.bulkTaskMu.Unlock()

	if auditLog := h.auditLog(); auditLog != nil {
		auditLog.RecordEvent(
			"/admin/api/v1/providers/"+providerID+"/accounts/bulk-action",
			"",
			"",
			fmt.Sprintf("bulk_%s", action),
			fmt.Sprintf("total=%d success=%d failed=%d", totalCount, successCount, failedCount),
		)
	}
}
