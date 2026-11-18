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
