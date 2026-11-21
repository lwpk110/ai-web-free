package db

import (
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// LoadRestrictionsFromDB 从数据库读取黑名单或禁用名单 (rType: "banned" | "disabled" | "")
func LoadRestrictionsFromDB(database *gorm.DB, rType string) map[string]string {
	res := make(map[string]string)
	if database == nil {
		return res
	}
	var records []AccountRestrictionRecord
	query := database.Model(&AccountRestrictionRecord{})
	if rType != "" {
		query = query.Where("type = ?", rType)
	}
	if err := query.Find(&records).Error; err != nil {
		return res
	}
	for _, r := range records {
		id := strings.ToLower(strings.TrimSpace(r.ID))
		if id != "" {
			res[id] = r.Reason
		}
	}
	return res
}

// SaveRestrictionToDB 保存单条黑名单/禁用记录
func SaveRestrictionToDB(database *gorm.DB, identifier, rType, reason string) error {
	if database == nil {
		return nil
	}
	cleanID := strings.ToLower(strings.TrimSpace(identifier))
	if cleanID == "" {
		return nil
	}
	now := time.Now()
	rec := AccountRestrictionRecord{
		ID:        cleanID,
		Type:      strings.ToLower(strings.TrimSpace(rType)),
		Reason:    strings.TrimSpace(reason),
		CreatedAt: now,
		UpdatedAt: now,
	}
	return database.Clauses(clause.OnConflict{
		UpdateAll: true,
	}).Create(&rec).Error
}

// SaveRestrictionsBatchToDB 批量保存黑名单/禁用记录
func SaveRestrictionsBatchToDB(database *gorm.DB, restrictions map[string]string, rType string) error {
	if database == nil || len(restrictions) == 0 {
		return nil
	}
	now := time.Now()
	records := make([]AccountRestrictionRecord, 0, len(restrictions))
	cleanType := strings.ToLower(strings.TrimSpace(rType))
	for id, reason := range restrictions {
		cleanID := strings.ToLower(strings.TrimSpace(id))
		if cleanID == "" {
			continue
		}
		records = append(records, AccountRestrictionRecord{
			ID:        cleanID,
			Type:      cleanType,
			Reason:    strings.TrimSpace(reason),
			CreatedAt: now,
			UpdatedAt: now,
		})
	}
	if len(records) == 0 {
		return nil
	}
	return database.Clauses(clause.OnConflict{
		UpdateAll: true,
	}).CreateInBatches(records, 100).Error
}

// BatchSaveRestrictionsToDB 兼容别名
func BatchSaveRestrictionsToDB(database *gorm.DB, rType string, restrictions map[string]string) error {
	return SaveRestrictionsBatchToDB(database, restrictions, rType)
}

// DeleteRestrictionFromDB 从数据库删除指定黑名单/禁用记录
func DeleteRestrictionFromDB(database *gorm.DB, identifier, rType string) error {
	if database == nil {
		return nil
	}
	cleanID := strings.ToLower(strings.TrimSpace(identifier))
	if cleanID == "" {
		return nil
	}
	query := database.Where("id = ?", cleanID)
	if rType != "" {
		query = query.Where("type = ?", strings.ToLower(strings.TrimSpace(rType)))
	}
	return query.Delete(&AccountRestrictionRecord{}).Error
}
