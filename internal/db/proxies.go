package db

import (
	"crypto/sha1"
	"encoding/hex"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func proxyID(rawURL string) string {
	h := sha1.Sum([]byte(strings.TrimSpace(rawURL)))
	return hex.EncodeToString(h[:8])
}

// LoadProxiesFromDB 从 SQLite 数据库读取全部已保存的有效代理 URL
func LoadProxiesFromDB(database *gorm.DB) ([]string, error) {
	if database == nil {
		return nil, nil
	}
	var records []ProxyNodeRecord
	if err := database.Where("status != ?", "disabled").Order("created_at asc").Find(&records).Error; err != nil {
		return nil, err
	}
	var urls []string
	for _, r := range records {
		u := strings.TrimSpace(r.URL)
		if u != "" {
			urls = append(urls, u)
		}
	}
	return urls, nil
}

// SaveProxiesToDB 批量增量写入代理列表至 SQLite 数据库
func SaveProxiesToDB(database *gorm.DB, urls []string) error {
	if database == nil || len(urls) == 0 {
		return nil
	}
	now := time.Now()
	for _, raw := range urls {
		clean := strings.TrimSpace(raw)
		if clean == "" || strings.HasPrefix(clean, "#") {
			continue
		}
		proto := "http"
		if parsed, err := url.Parse(clean); err == nil && parsed.Scheme != "" {
			proto = parsed.Scheme
		}
		rec := ProxyNodeRecord{
			ID:        proxyID(clean),
			URL:       clean,
			Protocol:  proto,
			Status:    "active",
			CreatedAt: now,
			UpdatedAt: now,
		}
		_ = database.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "url"}},
			DoUpdates: clause.AssignmentColumns([]string{"status", "updated_at"}),
		}).Create(&rec).Error
	}
	return nil
}

// ReplaceProxiesInDB 全量替换数据库中的代理列表
func ReplaceProxiesInDB(database *gorm.DB, urls []string) error {
	if database == nil {
		return nil
	}
	_ = database.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&ProxyNodeRecord{}).Error
	return SaveProxiesToDB(database, urls)
}

// DeleteProxyFromDB 从数据库删除指定代理节点
func DeleteProxyFromDB(database *gorm.DB, rawURL string) error {
	if database == nil || strings.TrimSpace(rawURL) == "" {
		return nil
	}
	return database.Where("url = ? OR id = ?", strings.TrimSpace(rawURL), proxyID(rawURL)).Delete(&ProxyNodeRecord{}).Error
}
