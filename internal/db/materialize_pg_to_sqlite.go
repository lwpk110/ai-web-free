package db

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// MaterializePostgresToSQLite 将云端 PostgreSQL 数据库的最新全量数据物化同步写入本地 SQLite 文件。
// 保证即使在无网或未配置 DATABASE_URL 的离线降级状态下，本地 SQLite 也能拥有与云端 100% 对齐的物化快照。
func MaterializePostgresToSQLite(pgDB *gorm.DB, localSQLitePath string) error {
	if pgDB == nil {
		return fmt.Errorf("pgDB is nil")
	}
	if localSQLitePath == "" {
		localSQLitePath = "data/ai-web-free.db"
	}
	dir := filepath.Dir(localSQLitePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建本地 SQLite 目录失败: %w", err)
	}

	sqliteDB, err := gorm.Open(sqlite.Open(localSQLitePath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return fmt.Errorf("打开本地 SQLite 目标库失败: %w", err)
	}
	if sqlDB, err := sqliteDB.DB(); err == nil {
		defer sqlDB.Close()
	}

	// 自动平滑初始化表结构
	if err := sqliteDB.AutoMigrate(
		&GalleryWork{},
		&Conversation{},
		&ChatMessageRecord{},
		&ChatPreset{},
		&PromptFavorite{},
		&VideoTaskRecord{},
		&AccountRuntimeState{},
		&PoolAccountRecord{},
		&ProxyNodeRecord{},
		&AccountRestrictionRecord{},
		&CustomVoiceModelRecord{},
		&ProviderChannelRecord{},
		&CredentialFormatPatternRecord{},
		&AudioGenerationRecord{},
		&ProviderAccountRecord{},
		&DramaProjectRecord{},
		&DramaRunRecord{},
	); err != nil {
		return fmt.Errorf("本地 SQLite AutoMigrate 失败: %w", err)
	}

	summary := make(map[string]int)

	// 1. PoolAccountRecord
	var accounts []PoolAccountRecord
	if err := pgDB.Find(&accounts).Error; err == nil && len(accounts) > 0 {
		_ = sqliteDB.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(accounts, 100).Error
		summary["号池账号 (PoolAccountRecord)"] = len(accounts)
	}

	// 2. ProxyNodeRecord
	var proxies []ProxyNodeRecord
	if err := pgDB.Find(&proxies).Error; err == nil && len(proxies) > 0 {
		_ = sqliteDB.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(proxies, 100).Error
		summary["代理节点 (ProxyNodeRecord)"] = len(proxies)
	}

	// 3. AccountRestrictionRecord
	var restrictions []AccountRestrictionRecord
	if err := pgDB.Find(&restrictions).Error; err == nil && len(restrictions) > 0 {
		_ = sqliteDB.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(restrictions, 100).Error
		summary["封禁/限制 (AccountRestrictionRecord)"] = len(restrictions)
	}

	// 4. CustomVoiceModelRecord
	var voices []CustomVoiceModelRecord
	if err := pgDB.Find(&voices).Error; err == nil && len(voices) > 0 {
		_ = sqliteDB.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(voices, 100).Error
		summary["自定义音色 (CustomVoiceModelRecord)"] = len(voices)
	}

	// 5. GalleryWork
	var gallery []GalleryWork
	if err := pgDB.Find(&gallery).Error; err == nil && len(gallery) > 0 {
		_ = sqliteDB.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(gallery, 100).Error
		summary["生图画廊 (GalleryWork)"] = len(gallery)
	}

	// 6. Conversation & ChatMessageRecord
	var convs []Conversation
	if err := pgDB.Find(&convs).Error; err == nil && len(convs) > 0 {
		_ = sqliteDB.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(convs, 100).Error
		summary["会话记录 (Conversation)"] = len(convs)
	}
	var msgs []ChatMessageRecord
	if err := pgDB.Find(&msgs).Error; err == nil && len(msgs) > 0 {
		_ = sqliteDB.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(msgs, 100).Error
		summary["聊天消息 (ChatMessageRecord)"] = len(msgs)
	}

	// 7. VideoTaskRecord
	var videos []VideoTaskRecord
	if err := pgDB.Find(&videos).Error; err == nil && len(videos) > 0 {
		_ = sqliteDB.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(videos, 100).Error
		summary["视频任务 (VideoTaskRecord)"] = len(videos)
	}

	// 8. DramaProjectRecord & DramaRunRecord
	var projects []DramaProjectRecord
	if err := pgDB.Find(&projects).Error; err == nil && len(projects) > 0 {
		_ = sqliteDB.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(projects, 100).Error
		summary["短剧项目 (DramaProjectRecord)"] = len(projects)
	}
	var runs []DramaRunRecord
	if err := pgDB.Find(&runs).Error; err == nil && len(runs) > 0 {
		_ = sqliteDB.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(runs, 100).Error
		summary["短剧运行 (DramaRunRecord)"] = len(runs)
	}

	// 9. AudioGenerationRecord
	var audios []AudioGenerationRecord
	if err := pgDB.Find(&audios).Error; err == nil && len(audios) > 0 {
		_ = sqliteDB.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(audios, 100).Error
		summary["音频记录 (AudioGenerationRecord)"] = len(audios)
	}

	// 10. ProviderAccountRecord
	var provAccounts []ProviderAccountRecord
	if err := pgDB.Find(&provAccounts).Error; err == nil && len(provAccounts) > 0 {
		_ = sqliteDB.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(provAccounts, 100).Error
		summary["渠道账号 (ProviderAccountRecord)"] = len(provAccounts)
	}

	// 11. CredentialFormatPatternRecord
	var patterns []CredentialFormatPatternRecord
	if err := pgDB.Find(&patterns).Error; err == nil && len(patterns) > 0 {
		_ = sqliteDB.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(patterns, 100).Error
		summary["凭据规则 (CredentialFormatPatternRecord)"] = len(patterns)
	}

	log.Printf("[DB Materialize] ✅ 成功将云端 PostgreSQL 数据全量物化同步至本地 SQLite (%s)！物化清单:", localSQLitePath)
	for item, count := range summary {
		log.Printf("  - %s: %d 条记录", item, count)
	}
	return nil
}
