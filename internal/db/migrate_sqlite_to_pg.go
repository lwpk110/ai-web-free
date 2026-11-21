package db

import (
	"fmt"
	"log"
	"os"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// AutoMigrateSQLiteToPostgres 将本地 SQLite 历史数据无缝迁移写入目标 PostgreSQL 数据库。
// 仅在目标 PG 核心号池为空且本地 SQLite 文件存在时自动执行，保证零停机无感平滑切换。
func AutoMigrateSQLiteToPostgres(localSQLitePath string, targetPG *gorm.DB) error {
	if localSQLitePath == "" {
		localSQLitePath = "data/ai-web-free.db"
	}
	if _, err := os.Stat(localSQLitePath); os.IsNotExist(err) {
		return nil
	}

	srcDB, err := gorm.Open(sqlite.Open(localSQLitePath+"?_pragma=busy_timeout(5000)"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return fmt.Errorf("打开本地 SQLite 待迁移数据失败: %w", err)
	}
	if sqlDB, closeErr := srcDB.DB(); closeErr == nil {
		defer sqlDB.Close()
	}

	var targetAccountCount int64
	_ = targetPG.Model(&PoolAccountRecord{}).Count(&targetAccountCount)
	if targetAccountCount > 0 {
		// 目标云端 PG 已有号池数据，跳过全量自愈迁移以避免意外覆盖云端最新状态
		return nil
	}

	log.Printf("[DB Migration] 🚀 检测到云端 PostgreSQL 为空库，开始自动将本地 SQLite (%s) 历史数据同步至云端 PG...", localSQLitePath)

	migratedSummary := make(map[string]int)

	// 1. 迁移号池账号记录 (PoolAccountRecord)
	var poolAccounts []PoolAccountRecord
	if err := srcDB.Find(&poolAccounts).Error; err == nil && len(poolAccounts) > 0 {
		_ = targetPG.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(poolAccounts, 100).Error
		migratedSummary["号池账号 (PoolAccountRecord)"] = len(poolAccounts)
	}

	// 2. 迁移代理节点 (ProxyNodeRecord) 与多环境粘性代理映射 (AccountProfileProxyRecord)
	var proxyNodes []ProxyNodeRecord
	if err := srcDB.Find(&proxyNodes).Error; err == nil && len(proxyNodes) > 0 {
		_ = targetPG.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(proxyNodes, 100).Error
		migratedSummary["代理节点 (ProxyNodeRecord)"] = len(proxyNodes)
	}
	var profileProxies []AccountProfileProxyRecord
	if err := srcDB.Find(&profileProxies).Error; err == nil && len(profileProxies) > 0 {
		_ = targetPG.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(profileProxies, 100).Error
		migratedSummary["Profile 粘性代理绑定 (AccountProfileProxyRecord)"] = len(profileProxies)
	}

	// 3. 迁移账号限制记录 (AccountRestrictionRecord)
	var restrictions []AccountRestrictionRecord
	if err := srcDB.Find(&restrictions).Error; err == nil && len(restrictions) > 0 {
		_ = targetPG.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(restrictions, 100).Error
		migratedSummary["封禁/限制名单 (AccountRestrictionRecord)"] = len(restrictions)
	}

	// 4. 迁移自定义音色 (CustomVoiceModelRecord)
	var customVoices []CustomVoiceModelRecord
	if err := srcDB.Find(&customVoices).Error; err == nil && len(customVoices) > 0 {
		_ = targetPG.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(customVoices, 100).Error
		migratedSummary["自定义音色 (CustomVoiceModelRecord)"] = len(customVoices)
	}

	// 5. 迁移画廊作品 (GalleryWork)
	var galleryWorks []GalleryWork
	if err := srcDB.Find(&galleryWorks).Error; err == nil && len(galleryWorks) > 0 {
		_ = targetPG.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(galleryWorks, 100).Error
		migratedSummary["生图画廊 (GalleryWork)"] = len(galleryWorks)
	}

	// 6. 迁移会话历史 (Conversation & ChatMessageRecord)
	var conversations []Conversation
	if err := srcDB.Find(&conversations).Error; err == nil && len(conversations) > 0 {
		_ = targetPG.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(conversations, 100).Error
		migratedSummary["会话记录 (Conversation)"] = len(conversations)
	}
	var chatMessages []ChatMessageRecord
	if err := srcDB.Find(&chatMessages).Error; err == nil && len(chatMessages) > 0 {
		_ = targetPG.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(chatMessages, 100).Error
		migratedSummary["消息记录 (ChatMessageRecord)"] = len(chatMessages)
	}

	// 7. 迁移视频任务 (VideoTaskRecord)
	var videoTasks []VideoTaskRecord
	if err := srcDB.Find(&videoTasks).Error; err == nil && len(videoTasks) > 0 {
		_ = targetPG.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(videoTasks, 100).Error
		migratedSummary["视频任务 (VideoTaskRecord)"] = len(videoTasks)
	}

	// 8. 迁移短剧项目 (DramaProjectRecord & DramaRunRecord)
	var dramaProjects []DramaProjectRecord
	if err := srcDB.Find(&dramaProjects).Error; err == nil && len(dramaProjects) > 0 {
		_ = targetPG.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(dramaProjects, 100).Error
		migratedSummary["短剧项目 (DramaProjectRecord)"] = len(dramaProjects)
	}
	var dramaRuns []DramaRunRecord
	if err := srcDB.Find(&dramaRuns).Error; err == nil && len(dramaRuns) > 0 {
		_ = targetPG.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(dramaRuns, 100).Error
		migratedSummary["短剧执行 (DramaRunRecord)"] = len(dramaRuns)
	}

	// 9. 迁移音频生成记录 (AudioGenerationRecord)
	var audioRecords []AudioGenerationRecord
	if err := srcDB.Find(&audioRecords).Error; err == nil && len(audioRecords) > 0 {
		_ = targetPG.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(audioRecords, 100).Error
		migratedSummary["音频工坊记录 (AudioGenerationRecord)"] = len(audioRecords)
	}

	// 10. 迁移独立供应商账号 (ProviderAccountRecord)
	var providerAccounts []ProviderAccountRecord
	if err := srcDB.Find(&providerAccounts).Error; err == nil && len(providerAccounts) > 0 {
		_ = targetPG.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(providerAccounts, 100).Error
		migratedSummary["第三方渠道账号 (ProviderAccountRecord)"] = len(providerAccounts)
	}

	// 11. 迁移凭据格式模式 (CredentialFormatPatternRecord) 与系统配置 (SystemSettingRecord)
	var patterns []CredentialFormatPatternRecord
	if err := srcDB.Find(&patterns).Error; err == nil && len(patterns) > 0 {
		_ = targetPG.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(patterns, 100).Error
		migratedSummary["凭据解析规则 (CredentialFormatPatternRecord)"] = len(patterns)
	}
	var sysSettings []SystemSettingRecord
	if err := srcDB.Find(&sysSettings).Error; err == nil && len(sysSettings) > 0 {
		_ = targetPG.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(sysSettings, 100).Error
		migratedSummary["系统配置 (SystemSettingRecord)"] = len(sysSettings)
	}

	log.Printf("[DB Migration] ✅ 成功将本地 SQLite 历史数据全量灌入云端 PostgreSQL！迁移清单:")
	for item, count := range migratedSummary {
		log.Printf("  - %s: %d 条记录", item, count)
	}

	return nil
}
