package db

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/logger"
)

// SchemaMetaRecord 记录数据库 Schema 指纹，避免远程 PostgreSQL 每次启动都重复执行耗时极高的全表 AutoMigrate。
type SchemaMetaRecord struct {
	Key       string    `gorm:"primaryKey;size:64"`
	Value     string    `gorm:"type:text"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"`
}

func (SchemaMetaRecord) TableName() string {
	return "schema_meta"
}

func computeSchemaFingerprint(models []any) string {
	h := sha256.New()
	for _, m := range models {
		t := reflect.TypeOf(m)
		for t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
		_, _ = fmt.Fprintf(h, "%s{", t.Name())
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			_, _ = fmt.Fprintf(h, "%s:%s:%s;", f.Name, f.Type.String(), f.Tag.Get("gorm"))
		}
		_, _ = h.Write([]byte("}\n"))
	}
	return hex.EncodeToString(h.Sum(nil))
}

var (
	globalDB         *gorm.DB
	globalDBMu       sync.RWMutex
	globalSyncEngine *SyncEngine
	globalSyncMu     sync.RWMutex
)

// SetDB 设置全局 GORM 实例
func SetDB(db *gorm.DB) {
	globalDBMu.Lock()
	defer globalDBMu.Unlock()
	globalDB = db
}

// GetDB 获取全局 GORM 实例
func GetDB() *gorm.DB {
	globalDBMu.RLock()
	defer globalDBMu.RUnlock()
	return globalDB
}

// SetSyncEngine 设置全局物化数据库同步引擎实例
func SetSyncEngine(engine *SyncEngine) {
	globalSyncMu.Lock()
	defer globalSyncMu.Unlock()
	globalSyncEngine = engine
}

// GetSyncEngine 获取全局物化数据库同步引擎实例
func GetSyncEngine() *SyncEngine {
	globalSyncMu.RLock()
	defer globalSyncMu.RUnlock()
	return globalSyncEngine
}

// IsRemoteDSN 判断给定的连接串是否指向远程云数据库（PostgreSQL / MySQL）。
func IsRemoteDSN(dsn string) bool {
	trimmed := strings.TrimSpace(dsn)
	lower := strings.ToLower(trimmed)
	return strings.HasPrefix(lower, "postgres://") ||
		strings.HasPrefix(lower, "postgresql://") ||
		strings.HasPrefix(lower, "mysql://") ||
		strings.Contains(lower, "@tcp(")
}

// MaskDSN 对数据库连接串进行严格脱敏，移除账号、密码与敏感查询参数，仅保留协议与主机/库名。
func MaskDSN(dsn string) string {
	trimmed := strings.TrimSpace(dsn)
	if trimmed == "" || trimmed == "default" {
		return "data/ai-web-free.db"
	}
	if !IsRemoteDSN(trimmed) {
		return trimmed
	}
	if strings.Contains(trimmed, "@tcp(") && !strings.Contains(trimmed, "://") {
		idx := strings.Index(trimmed, "@tcp(")
		rest := trimmed[idx+1:]
		if q := strings.Index(rest, "?"); q >= 0 {
			rest = rest[:q]
		}
		return "mysql://" + rest
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		if at := strings.LastIndex(trimmed, "@"); at >= 0 {
			rest := trimmed[at+1:]
			if q := strings.Index(rest, "?"); q >= 0 {
				rest = rest[:q]
			}
			return "***@" + rest
		}
		return "***"
	}
	u.User = nil
	u.RawQuery = ""
	u.Fragment = ""
	return u.String()
}

// CoreModels 返回 internal/db 内定义的全部核心业务表模型切片。
func CoreModels() []any {
	return []any{
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
		&FreebieDeal{},
		&BatchActionTaskRecord{},
	}
}

func appendPostgresConnectTimeout(dsn string, timeoutSec int) string {
	if timeoutSec <= 0 || strings.Contains(strings.ToLower(dsn), "connect_timeout=") {
		return dsn
	}
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return fmt.Sprintf("%s%sconnect_timeout=%d", dsn, sep, timeoutSec)
}

// InitDB 初始化持久化数据库，支持纯 Go SQLite、PostgreSQL 与 MySQL，严格零 CGO。
func InitDB(dsn string, extraModels ...any) (*gorm.DB, error) {
	db, err := openDialectorAndMigrate(context.Background(), dsn, true, true, true, extraModels...)
	if err != nil {
		return nil, err
	}
	SetDB(db)
	return db, nil
}

// OpenCloudDB 连接并迁移远程云端数据库（禁止静默回退到内存 SQLite，确保同步目标真实可用）。
func OpenCloudDB(ctx context.Context, dsn string, extraModels ...any) (*gorm.DB, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return openDialectorAndMigrate(ctx, dsn, false, false, false, extraModels...)
}

func openDialectorAndMigrate(ctx context.Context, dsn string, allowMemoryFallback, runSeeds, runLegacySQLiteMigration bool, extraModels ...any) (*gorm.DB, error) {
	trimmed := strings.TrimSpace(dsn)
	var dialector gorm.Dialector

	gormCfg := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	}

	lower := strings.ToLower(trimmed)
	isPostgres := strings.HasPrefix(lower, "postgres://") || strings.HasPrefix(lower, "postgresql://")
	isMySQL := strings.HasPrefix(lower, "mysql://") || strings.Contains(lower, "@tcp(")

	switch {
	case isPostgres:
		// PreferSimpleProtocol 避免 PgBouncer/Neon-pooler 下的预处理语句额外往返，显著降低跨洋延迟
		dialector = postgres.New(postgres.Config{
			DSN:                  appendPostgresConnectTimeout(trimmed, 15),
			PreferSimpleProtocol: true,
		})
	case strings.HasPrefix(lower, "mysql://"):
		rawDSN := strings.TrimPrefix(trimmed, "mysql://")
		dialector = mysql.Open(rawDSN)
	case strings.Contains(lower, "@tcp("):
		dialector = mysql.Open(trimmed)
	case trimmed == ":memory:":
		dialector = sqlite.Open(":memory:?_pragma=busy_timeout(5000)")
	default:
		filePath := trimmed
		if filePath == "" || filePath == "default" {
			filePath = "data/ai-web-free.db"
		}
		dir := filepath.Dir(filePath)
		if dir != "." && dir != "/" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				if !allowMemoryFallback {
					return nil, fmt.Errorf("cannot create db dir %q: %w", dir, err)
				}
				log.Printf("[DB] Warning: cannot create db dir %q (%v), falling back to :memory:", dir, err)
				dialector = sqlite.Open(":memory:?_pragma=busy_timeout(5000)")
			}
		}
		if dialector == nil {
			sqliteDSN := fmt.Sprintf("%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", filePath)
			dialector = sqlite.Open(sqliteDSN)
		}
	}

	db, err := gorm.Open(dialector, gormCfg)
	if err != nil {
		if !allowMemoryFallback {
			return nil, fmt.Errorf("failed to open remote database (%s): %w", MaskDSN(dsn), err)
		}
		log.Printf("[DB] Failed to open database with %q (%v), attempting fallback to :memory:", MaskDSN(dsn), err)
		memDb, memErr := gorm.Open(sqlite.Open(":memory:?_pragma=busy_timeout(5000)"), gormCfg)
		if memErr != nil {
			return nil, fmt.Errorf("failed to open database (%v) and memory fallback failed (%v)", err, memErr)
		}
		db = memDb
		isPostgres = false
		isMySQL = false
	}

	if !allowMemoryFallback {
		if sqlDB, sqlErr := db.DB(); sqlErr == nil {
			if pingErr := sqlDB.PingContext(ctx); pingErr != nil {
				_ = sqlDB.Close()
				return nil, fmt.Errorf("failed to ping remote database (%s): %w", MaskDSN(dsn), pingErr)
			}
		}
	}

	// 针对不同数据库引擎调优连接池参数（特别是 Neon Serverless PG 连接池与 SQLite WAL 模式）
	if sqlDB, sqlErr := db.DB(); sqlErr == nil {
		if isPostgres || isMySQL {
			sqlDB.SetMaxIdleConns(5)
			sqlDB.SetMaxOpenConns(25)
			sqlDB.SetConnMaxLifetime(30 * time.Minute)
			sqlDB.SetConnMaxIdleTime(5 * time.Minute)
		} else {
			sqlDB.SetMaxOpenConns(1)
		}
	}

	models := CoreModels()
	for _, em := range extraModels {
		if em != nil {
			models = append(models, em)
		}
	}

	// 自动平滑迁移模型表结构：对远程数据库先比对 Schema 指纹，未变更时跳过耗时极高的逐表元数据巡检
	needMigrate := true
	schemaFingerprint := ""
	useFingerprint := isPostgres || isMySQL
	if useFingerprint {
		schemaFingerprint = computeSchemaFingerprint(models)
		var meta SchemaMetaRecord
		if qErr := db.WithContext(ctx).Where("key = ?", "schema_fingerprint").Limit(1).Take(&meta).Error; qErr == nil && meta.Value == schemaFingerprint {
			needMigrate = false
		}
	}
	if needMigrate {
		migrateTargets := models
		if useFingerprint {
			migrateTargets = append(migrateTargets, &SchemaMetaRecord{})
		}
		if mErr := db.WithContext(ctx).AutoMigrate(migrateTargets...); mErr != nil {
			if !allowMemoryFallback {
				if sqlDB, sqlErr := db.DB(); sqlErr == nil {
					_ = sqlDB.Close()
				}
			}
			return nil, fmt.Errorf("failed to auto migrate database schemas: %w", mErr)
		}
		if useFingerprint && schemaFingerprint != "" {
			_ = db.WithContext(ctx).Clauses(clause.OnConflict{UpdateAll: true}).Create(&SchemaMetaRecord{
				Key:   "schema_fingerprint",
				Value: schemaFingerprint,
			}).Error
		}
	}

	// 种入默认内置角色预设与免密号池渠道
	if runSeeds {
		seedBuiltinPresets(db)
		seedDefaultChannels(db)
	}

	// 若在直连云库模式下连接到云端 PostgreSQL，自动检查并从本地 SQLite 无缝自愈迁移历史数据
	if runLegacySQLiteMigration && isPostgres {
		if migErr := AutoMigrateSQLiteToPostgres("data/ai-web-free.db", db); migErr != nil {
			log.Printf("[DB] 自动同步本地 SQLite 数据至 PostgreSQL 失败: %v", migErr)
		}
	}

	return db, nil
}

// seedBuiltinPresets 种入常用 AI 角色模板
func seedBuiltinPresets(db *gorm.DB) {
	builtin := []ChatPreset{
		{
			ID:           "preset-copilot",
			Name:         "平台运维诊断 Copilot",
			Description:  "深度排查号池熔断、429 限流、403 风控及卡密队列，提供一键自愈与链路分析",
			Icon:         "ShieldAlert",
			SystemPrompt: "你是 AI-Web-Free 平台的专业运维与架构诊断 Copilot。你精通号池健康度探测、反爬风控绕过、JWT 凭据分析与自动化自愈。\n当用户提出排障需求或使用 /diagnose、/fix 时，请结合平台架构与链路给出系统化分析，并在适当时推荐可视化治理卡片。",
			DefaultModel: "auto",
			IsBuiltin:    true,
			SortOrder:    1,
		},
		{
			ID:           "preset-coder",
			Name:         "全栈工程师与架构师",
			Description:  "精通 Go、React、TypeScript、Docker、分布式架构与高性能并发设计",
			Icon:         "Code2",
			SystemPrompt: "你是一位资深的全栈技术架构师与首席软件工程师，精通现代云原生技术栈（Go, React, TypeScript, K8s, Docker）。\n你的回答应保持生产级水准：代码需具备强壮的错误处理、零 CGO 兼容、类型安全与并发安全考量，并主动给出架构权衡建议。",
			DefaultModel: "gpt-4o",
			IsBuiltin:    true,
			SortOrder:    2,
		},
		{
			ID:           "preset-translator",
			Name:         "多语言科技翻译官",
			Description:  "提供专业中英日双向母语级翻译，精准润色技术术语与论文",
			Icon:         "Languages",
			SystemPrompt: "你是一位精通中英日多语言的技术翻译专家。在翻译计算机科学、软件工程、人工智能与商业文档时，要求术语地道、信达雅兼备、语句自然通顺，并在必要时对专业名词提供括号中英对照。",
			DefaultModel: "gpt-4o-mini",
			IsBuiltin:    true,
			SortOrder:    3,
		},
		{
			ID:           "preset-prompt",
			Name:         "生图提示词架构大师",
			Description:  "将简短灵感扩写为电影级 Midjourney / FLUX / DALL-E 结构化生图 Prompt",
			Icon:         "Sparkles",
			SystemPrompt: "你是一位顶级的 AI 图像提示词（Prompt）架构大师。你擅长将用户的模糊构想转化为结构严密、画面感极强的中英双语生图提示词，涵盖主体细节、光影构图、胶片质感、艺术家风格与渲染引擎参数。",
			DefaultModel: "gpt-4o",
			IsBuiltin:    true,
			SortOrder:    4,
		},
		{
			ID:           "preset-writer",
			Name:         "专业技术文档撰稿人",
			Description:  "擅长撰写生产级 README、架构设计决策（ADR）、OpenAPI 规范与技术博客",
			Icon:         "FileText",
			SystemPrompt: "你是一位专业的技术作家与文档架构师。你的输出结构清晰、逻辑严谨，善用 GitHub 风格 Markdown、Mermaid 流程图、参数表格与要点对比，帮助工程团队沉淀高可用技术资产。",
			DefaultModel: "gpt-4o",
			IsBuiltin:    true,
			SortOrder:    5,
		},
	}

	now := time.Now().UTC()
	for i := range builtin {
		builtin[i].CreatedAt = now
		builtin[i].UpdatedAt = now
	}
	_ = db.Clauses(clause.OnConflict{DoNothing: true}).Create(&builtin).Error
}

// seedDefaultChannels 自动录入无需登录与可用的免费多模态渠道到号池数据库
func seedDefaultChannels(db *gorm.DB) {
	channels := []ProviderChannelRecord{
		{
			ID:           "chan-jianying-tts",
			ProviderID:   "jianying",
			ChannelName:  "剪映旧免密 TTS 节点 (已退役)",
			AuthType:     "no_auth",
			Credential:   "anonymous-noauth",
			Status:       "disabled",
			IsFreeTier:   true,
			Capabilities: "tts,stream",
			Description:  "字节剪映旧免密域名（api-normal-c-lq.zijieapi.com 已返回 NXDOMAIN 下线），默认禁用；请使用 Edge-TTS、硅基流动或阿里云百炼云端语音引擎",
		},
		{
			ID:           "chan-local-tts",
			ProviderID:   "local_tts",
			ChannelName:  "自定义 OpenAI 兼容云端 TTS 节点",
			AuthType:     "no_auth",
			Credential:   "custom-endpoint",
			Status:       "standby",
			IsFreeTier:   true,
			Capabilities: "tts",
			Description:  "可选外部 OpenAI 兼容云端/中继语音端点（配置 CUSTOM_TTS_BASE_URL 即生效，无需本地部署推理）",
		},
		{
			ID:           "chan-edge-tts",
			ProviderID:   "edge_tts",
			ChannelName:  "微软 Edge-TTS 官方免费语音节点",
			AuthType:     "no_auth",
			Credential:   "anonymous-edge",
			Status:       "active",
			IsFreeTier:   true,
			Capabilities: "tts,stream",
			Description:  "微软官方大声朗读引擎，内置晓晓、云希、云扬等影视解说神级音色，纯 Go 免密直连，零额度消耗",
		},
		{
			ID:           "chan-siliconflow-free",
			ProviderID:   "siliconflow",
			ChannelName:  "硅基流动 CogVideoX & FLUX 免费池",
			AuthType:     "api_key",
			Credential:   "pool-preset-active",
			Status:       "active",
			IsFreeTier:   true,
			Capabilities: "video_generation,image_generation,tts",
			Description:  "永久免费 FLUX.1 极速文生图、CogVideoX 视频生成与 CosyVoice 拟真人声，支持导入新用户免额度 Key",
		},
		{
			ID:           "chan-meta-muse-web",
			ProviderID:   "meta_muse",
			ChannelName:  "Meta AI Muse 音视频协同逆向公开节点",
			AuthType:     "no_auth",
			Credential:   "anonymous-muse",
			Status:       "active",
			IsFreeTier:   true,
			Capabilities: "video_generation",
			Description:  "Meta 原生音画一体协同大模型（自带环境音与配乐）与 MuseTalk 角色数字人口型同步，开箱可用",
		},
		{
			ID:           "chan-jimeng-daily",
			ProviderID:   "jimeng",
			ChannelName:  "字节即梦 2.0 每日免费积分号池",
			AuthType:     "cookie",
			Credential:   "cookie-standby",
			Status:       "standby",
			IsFreeTier:   true,
			Capabilities: "video_generation,image_generation",
			Description:  "即梦每日赠送 66~88 免费积分，支持输入 Session Cookie 接入高品质复杂运镜",
		},
		{
			ID:           "chan-dashscope-trial",
			ProviderID:   "dashscope",
			ChannelName:  "阿里云百炼 CosyVoice 声音复刻与万相生图",
			AuthType:     "api_key",
			Credential:   "key-standby",
			Status:       "standby",
			IsFreeTier:   true,
			Capabilities: "tts,image_generation",
			Description:  "阿里云百炼官方通道，新用户赠送 100 万 Token，支持声音复刻与自然语言捏声音",
		},
	}

	now := time.Now().UTC()
	for i := range channels {
		channels[i].CreatedAt = now
		channels[i].UpdatedAt = now
	}
	_ = db.Clauses(clause.OnConflict{DoNothing: true}).Create(&channels).Error
	_ = db.Model(&ProviderChannelRecord{}).
		Where("id = ? AND status = ?", "chan-jianying-tts", "active").
		Updates(map[string]interface{}{
			"status":      "disabled",
			"description": channels[0].Description,
			"updated_at":  now,
		}).Error
}
