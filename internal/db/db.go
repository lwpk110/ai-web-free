package db

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var (
	globalDB   *gorm.DB
	globalDBMu sync.RWMutex
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

// InitDB 初始化持久化数据库，支持纯 Go SQLite、PostgreSQL 与 MySQL，严格零 CGO。
func InitDB(dsn string) (*gorm.DB, error) {
	trimmed := strings.TrimSpace(dsn)
	var dialector gorm.Dialector

	gormCfg := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
		NowFunc: func() time.Time {
			return time.Now().UTC()
		},
	}

	switch {
	case strings.HasPrefix(trimmed, "postgres://") || strings.HasPrefix(trimmed, "postgresql://"):
		dialector = postgres.Open(trimmed)
	case strings.HasPrefix(trimmed, "mysql://"):
		rawDSN := strings.TrimPrefix(trimmed, "mysql://")
		dialector = mysql.Open(rawDSN)
	case strings.Contains(trimmed, "@tcp("):
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
		log.Printf("[DB] Failed to open database with %q (%v), attempting fallback to :memory:", dsn, err)
		memDb, memErr := gorm.Open(sqlite.Open(":memory:?_pragma=busy_timeout(5000)"), gormCfg)
		if memErr != nil {
			return nil, fmt.Errorf("failed to open database (%v) and memory fallback failed (%v)", err, memErr)
		}
		db = memDb
	}

	// 自动平滑迁移模型表结构
	if err := db.AutoMigrate(
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
		return nil, fmt.Errorf("failed to auto migrate database schemas: %w", err)
	}

	// 种入默认内置角色预设与免密号池渠道
	seedBuiltinPresets(db)
	seedDefaultChannels(db)

	SetDB(db)
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

	for _, p := range builtin {
		var existing ChatPreset
		if err := db.Where("id = ?", p.ID).First(&existing).Error; err != nil {
			p.CreatedAt = time.Now().UTC()
			p.UpdatedAt = time.Now().UTC()
			_ = db.Create(&p).Error
		}
	}
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

	for _, ch := range channels {
		var existing ProviderChannelRecord
		if err := db.Where("id = ?", ch.ID).First(&existing).Error; err != nil {
			ch.CreatedAt = time.Now().UTC()
			ch.UpdatedAt = time.Now().UTC()
			_ = db.Create(&ch).Error
		} else if ch.ID == "chan-jianying-tts" && existing.Status == "active" {
			_ = db.Model(&existing).Updates(map[string]interface{}{
				"status":      "disabled",
				"description": ch.Description,
				"updated_at":  time.Now().UTC(),
			}).Error
		}
	}
}
