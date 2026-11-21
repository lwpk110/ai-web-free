package db

import (
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SaveBatchTaskToDB 持久化保存或更新批量异步任务记录
func SaveBatchTaskToDB(database *gorm.DB, rec *BatchActionTaskRecord) error {
	if database == nil || rec == nil || strings.TrimSpace(rec.ID) == "" {
		return nil
	}
	if rec.CreatedAt.IsZero() {
		rec.CreatedAt = time.Now()
	}
	rec.UpdatedAt = time.Now()
	return database.Clauses(clause.OnConflict{
		UpdateAll: true,
	}).Create(rec).Error
}

// GetLatestActiveBatchTaskFromDB 从数据库获取指定供应商最近未被 Dismiss 的批量异步任务
func GetLatestActiveBatchTaskFromDB(database *gorm.DB, provider string) (*BatchActionTaskRecord, error) {
	if database == nil {
		return nil, nil
	}
	var rec BatchActionTaskRecord
	query := database.Where("dismissed = ?", false)
	if p := strings.TrimSpace(provider); p != "" {
		query = query.Where("provider = ?", strings.ToLower(p))
	}
	if err := query.Order("created_at desc").Limit(1).Take(&rec).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &rec, nil
}

// DismissBatchTaskInDB 将数据库中指定的批量任务标记为已关闭
func DismissBatchTaskInDB(database *gorm.DB, taskID string) error {
	if database == nil || strings.TrimSpace(taskID) == "" {
		return nil
	}
	return database.Model(&BatchActionTaskRecord{}).
		Where("id = ?", strings.TrimSpace(taskID)).
		Updates(map[string]interface{}{
			"dismissed":  true,
			"updated_at": time.Now(),
		}).Error
}

// ListRecentBatchTasksFromDB 获取最近的批量异步任务历史列表
func ListRecentBatchTasksFromDB(database *gorm.DB, provider string, limit int) ([]BatchActionTaskRecord, error) {
	if database == nil {
		return nil, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var list []BatchActionTaskRecord
	query := database.Model(&BatchActionTaskRecord{})
	if p := strings.TrimSpace(provider); p != "" {
		query = query.Where("provider = ?", strings.ToLower(p))
	}
	if err := query.Order("created_at desc").Limit(limit).Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}
