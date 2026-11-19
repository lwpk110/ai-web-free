package db

import (
	"time"
)

// GalleryWork 图像工坊作品持久化模型
type GalleryWork struct {
	ID            string    `gorm:"primaryKey;size:64" json:"id"`
	Prompt        string    `gorm:"type:text;not null" json:"prompt"`
	RevisedPrompt string    `gorm:"type:text" json:"revised_prompt,omitempty"`
	Model         string    `gorm:"size:64;index" json:"model"`
	Size          string    `gorm:"size:32" json:"size"`
	Quality       string    `gorm:"size:32" json:"quality,omitempty"`
	Mode          string    `gorm:"size:32;default:'generations'" json:"mode"`
	Category      string    `gorm:"size:48;default:'general';index" json:"category"`
	Source        string    `gorm:"size:48;default:'image_studio';index" json:"source"`
	ProjectID     string    `gorm:"size:64;index" json:"project_id,omitempty"`
	ProjectTitle  string    `gorm:"size:255;index" json:"project_title,omitempty"`
	RoleName      string    `gorm:"size:128;index" json:"role_name,omitempty"`
	ShotIndex     int       `gorm:"default:0;index" json:"shot_index,omitempty"`
	Tags          []string  `gorm:"serializer:json;type:text" json:"tags,omitempty"`
	StorageType   string    `gorm:"size:32;default:'local'" json:"storage_type"` // local, s3, blob, remote
	FilePath      string    `gorm:"size:255" json:"file_path,omitempty"`
	URL           string    `gorm:"type:text;not null" json:"url"`
	BlobData      []byte    `json:"-"`                          // 仅在无磁盘模式下兜底
	FileSize      int64     `json:"file_size"`
	MimeType      string    `gorm:"size:64;default:'image/png'" json:"mime_type"`
	LatencyMs     int       `json:"latency_ms"`
	IsFavorite    bool      `gorm:"default:false;index" json:"is_favorite"`
	ClientIP      string    `gorm:"size:64" json:"client_ip,omitempty"`
	CreatedAt     time.Time `gorm:"index" json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Conversation AI 对话工作台会话主模型
type Conversation struct {
	ID            string    `gorm:"primaryKey;size:64" json:"id"`
	Title         string    `gorm:"size:255;index" json:"title"`
	Model         string    `gorm:"size:64" json:"model"`
	SystemPrompt  string    `gorm:"type:text" json:"system_prompt,omitempty"`
	PresetID      string    `gorm:"size:64;index" json:"preset_id,omitempty"`
	HeadMessageID string    `gorm:"size:64" json:"head_message_id,omitempty"`
	CustomJSON    string    `gorm:"type:text" json:"custom_json,omitempty"`
	IsPinned      bool      `gorm:"default:false;index" json:"is_pinned"`
	IsArchived    bool      `gorm:"default:false;index" json:"is_archived"`
	Tags          string    `gorm:"size:255" json:"tags,omitempty"`
	ClientIP      string    `gorm:"size:64" json:"client_ip,omitempty"`
	MessageCount  int       `gorm:"default:0" json:"message_count"`
	CreatedAt     time.Time `gorm:"index" json:"created_at"`
	UpdatedAt     time.Time `gorm:"index" json:"updated_at"`
}

// ChatMessageRecord AI 对话消息记录模型（支持基于 ParentID 的分支树与多模态 Parts）
type ChatMessageRecord struct {
	ID              string    `gorm:"primaryKey;size:64" json:"id"`
	ConversationID  string    `gorm:"size:64;index" json:"conversation_id"` // 对应 Conversation.ID
	ParentID        string    `gorm:"size:64;index" json:"parent_id,omitempty"`
	SessionKey      string    `gorm:"size:64;index;default:'default'" json:"session_key,omitempty"` // 兼容旧版单一会话
	Role            string    `gorm:"size:32" json:"role"`                                         // user, assistant, system
	Content         string    `gorm:"type:text" json:"content"`
	PartsJSON       string    `gorm:"type:text" json:"parts_json,omitempty"`
	AttachmentsJSON string    `gorm:"type:text" json:"attachments_json,omitempty"`
	MetadataJSON    string    `gorm:"type:text" json:"metadata_json,omitempty"`
	StatusJSON      string    `gorm:"type:text" json:"status_json,omitempty"`
	Model           string    `gorm:"size:64" json:"model,omitempty"`
	OpenUIMode      bool      `gorm:"default:false" json:"openui_mode"`
	Skill           string    `gorm:"size:32;default:'auto'" json:"skill,omitempty"`
	TokensPrompt    int       `gorm:"default:0" json:"tokens_prompt,omitempty"`
	TokensComp      int       `gorm:"default:0" json:"tokens_comp,omitempty"`
	LatencyMs       int64     `gorm:"default:0" json:"latency_ms,omitempty"`
	Citations       string    `gorm:"type:text" json:"citations,omitempty"` // 格式化 JSON 字符串存放引用来源
	CreatedAt       time.Time `gorm:"index" json:"created_at"`
}

// ChatPreset AI 角色预设与系统提示词模板
type ChatPreset struct {
	ID           string    `gorm:"primaryKey;size:64" json:"id"`
	Name         string    `gorm:"size:128;not null" json:"name"`
	Description  string    `gorm:"size:255" json:"description"`
	Icon         string    `gorm:"size:64" json:"icon"`
	SystemPrompt string    `gorm:"type:text;not null" json:"system_prompt"`
	DefaultModel string    `gorm:"size:64" json:"default_model,omitempty"`
	IsBuiltin    bool      `gorm:"default:false;index" json:"is_builtin"`
	SortOrder    int       `gorm:"default:0;index" json:"sort_order"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// PromptFavorite 提示词广场收藏持久化模型
type PromptFavorite struct {
	PromptID  string    `gorm:"primaryKey;size:128" json:"prompt_id"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}

// VideoTaskRecord 视频生成任务持久化模型
type VideoTaskRecord struct {
	ID           string     `gorm:"primaryKey;size:64" json:"id"`
	TaskID       string     `gorm:"size:64;index" json:"task_id"`
	VidID        string     `gorm:"size:64;index" json:"vid_id,omitempty"`
	AccountEmail string     `gorm:"size:128;index" json:"account_email,omitempty"`
	Provider     string     `gorm:"size:64;index" json:"provider"`
	Model       string     `gorm:"size:64;index" json:"model"`
	Prompt      string     `gorm:"type:text;not null" json:"prompt"`
	ImageURL    string     `gorm:"type:text" json:"image_url,omitempty"`
	AudioURL    string     `gorm:"type:text" json:"audio_url,omitempty"`
	Duration    int        `gorm:"default:5" json:"duration"`
	Status      string     `gorm:"size:32;index" json:"status"` // PENDING | RUNNING | SUCCEEDED | FAILED
	Progress    int        `gorm:"default:0" json:"progress"`
	VideoURL          string     `gorm:"type:text" json:"video_url,omitempty"`
	ErrorMsg          string     `gorm:"type:text" json:"error_msg,omitempty"`
	UpstreamSessionID string     `gorm:"size:128;index" json:"upstream_session_id,omitempty"`
	UpstreamMsgID     string     `gorm:"size:128;index" json:"upstream_msg_id,omitempty"`
	UpstreamSeq       int64      `gorm:"default:0" json:"upstream_seq,omitempty"`
	UpstreamAssetPath string     `gorm:"type:text" json:"upstream_asset_path,omitempty"`
	UpstreamAssetURL  string     `gorm:"type:text" json:"upstream_asset_url,omitempty"`
	ExpectedBytes     int64      `gorm:"default:0" json:"expected_bytes,omitempty"`
	UpstreamMeta      string     `gorm:"type:text" json:"upstream_meta,omitempty"`
	CreatedAt         time.Time  `gorm:"index" json:"created_at"`
	CompletedAt       *time.Time `json:"completed_at,omitempty"`
}

// AccountRuntimeState 号池节点运行态持久化模型
type AccountRuntimeState struct {
	AccountID         string     `gorm:"primaryKey;size:128" json:"account_id"`
	Status            string     `gorm:"size:32" json:"status"`
	RateLimitedUntil  *time.Time `json:"rate_limited_until,omitempty"`
	ImageLimitedUntil *time.Time `json:"image_limited_until,omitempty"`
	LastError         string     `gorm:"type:text" json:"last_error,omitempty"`
	ErrorCount        int        `gorm:"default:0" json:"error_count"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

// PoolAccountRecord ChatGPT 与通用号池账号完整持久化模型（支持凭据、Token、状态、限流与统计同步至 SQLite）
type PoolAccountRecord struct {
	ID                 string     `gorm:"primaryKey;size:128" json:"id"`
	Provider           string     `gorm:"size:64;default:'chatgpt';index" json:"provider"` // chatgpt, etc.
	Type               string     `gorm:"size:32;index" json:"type"`                     // free, puid, noauth
	Email              string     `gorm:"size:128;index" json:"email"`
	Password           string     `gorm:"type:text" json:"password,omitempty"`
	TOTPSecret         string     `gorm:"column:totp_secret;size:128" json:"totp_secret,omitempty"`
	Token              string     `gorm:"type:text" json:"token,omitempty"`
	RefreshToken       string     `gorm:"type:text" json:"refresh_token,omitempty"`
	SessionToken       string     `gorm:"type:text" json:"session_token,omitempty"`
	PUID               string     `gorm:"column:puid;size:128" json:"puid,omitempty"`
	TeamUserID         string     `gorm:"column:team_user_id;size:128" json:"team_user_id,omitempty"`
	ChatGPTAccountID   string     `gorm:"column:chatgpt_account_id;size:128;index" json:"chatgpt_account_id,omitempty"`
	PlanType           string     `gorm:"size:64" json:"plan_type,omitempty"`
	Proxy              string     `gorm:"size:255" json:"proxy,omitempty"`
	Status             string     `gorm:"size:32;index" json:"status"` // active, standby, rate_limited, expired, banned, disabled, pending
	LastError          string     `gorm:"type:text" json:"last_error,omitempty"`
	RenewState         string     `gorm:"size:32" json:"renew_state,omitempty"`
	RenewFailures      int        `gorm:"default:0" json:"renew_failures"`
	LastRenewError     string     `gorm:"type:text" json:"last_renew_error,omitempty"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	CooldownUntil      *time.Time `json:"cooldown_until,omitempty"`
	ImageCooldownUntil *time.Time `json:"image_cooldown_until,omitempty"`
	TotalCalls         int64      `gorm:"default:0" json:"total_calls"`
	FailedCalls        int64      `gorm:"default:0" json:"failed_calls"`
	LastUsed           *time.Time `json:"last_used,omitempty"`
	CreatedAt          time.Time  `gorm:"index" json:"created_at"`
	UpdatedAt          time.Time  `gorm:"index" json:"updated_at"`
}

// ProxyNodeRecord 代理节点持久化模型（支持出口代理 IP 列表持久化与健康状态持久化）
type ProxyNodeRecord struct {
	ID        string    `gorm:"primaryKey;size:64" json:"id"`
	URL       string    `gorm:"size:255;uniqueIndex" json:"url"`
	Protocol  string    `gorm:"size:32" json:"protocol"`
	LatencyMs int64     `gorm:"default:0" json:"latency_ms"`
	Status    string    `gorm:"size:32;default:'active';index" json:"status"` // active | cooling | disabled
	LastError string    `gorm:"type:text" json:"last_error,omitempty"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
	UpdatedAt time.Time `gorm:"index" json:"updated_at"`
}

// AccountRestrictionRecord 账号黑名单/手动禁用名单持久化模型
type AccountRestrictionRecord struct {
	ID        string    `gorm:"primaryKey;size:128" json:"id"` // 邮箱或 chatgpt_account_id（小写去重）
	Type      string    `gorm:"size:32;index" json:"type"`     // banned | disabled
	Reason    string    `gorm:"type:text" json:"reason"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
	UpdatedAt time.Time `gorm:"index" json:"updated_at"`
}

// CustomVoiceModelRecord 自定义/克隆音色持久化模型
type CustomVoiceModelRecord struct {
	ID            string    `gorm:"primaryKey;size:64" json:"id"`
	Name          string    `gorm:"size:128;index" json:"name"`
	Role          string    `gorm:"size:64" json:"role,omitempty"`
	Category      string    `gorm:"size:64" json:"category,omitempty"`
	Gender        string    `gorm:"size:32" json:"gender,omitempty"`
	Locale        string    `gorm:"size:32" json:"locale,omitempty"`
	Transcript    string    `gorm:"type:text" json:"transcript,omitempty"`
	TagsJSON      string    `gorm:"type:text" json:"tags_json,omitempty"`
	SourceURL     string    `gorm:"type:text" json:"source_url,omitempty"`
	SourcePage    string    `gorm:"type:text" json:"source_page,omitempty"`
	SampleExt     string    `gorm:"size:16" json:"sample_ext,omitempty"`
	SampleBytes   int       `gorm:"default:0" json:"sample_bytes,omitempty"`
	CloneEngine   string    `gorm:"size:64" json:"clone_engine,omitempty"`
	ClonedVoiceID string    `gorm:"size:128;index" json:"cloned_voice_id,omitempty"`
	CloneStatus   string    `gorm:"size:32;index" json:"clone_status"`
	Error         string    `gorm:"type:text" json:"error,omitempty"`
	CreatedAt     time.Time `gorm:"index" json:"created_at"`
	UpdatedAt     time.Time `gorm:"index" json:"updated_at"`
}

// ProviderChannelRecord 记录号池中已录入的多模态渠道与账号（持久化至 SQLite 数据库）
type ProviderChannelRecord struct {
	ID           string    `gorm:"primaryKey;size:64" json:"id"`
	ProviderID   string    `gorm:"size:64;index" json:"provider_id"` // jianying, edge_tts, siliconflow, dashscope, jimeng, meta_muse
	ChannelName  string    `gorm:"size:128" json:"channel_name"`
	AuthType     string    `gorm:"size:32" json:"auth_type"` // no_auth, api_key, cookie
	Credential   string    `gorm:"type:text" json:"credential"`
	Status       string    `gorm:"size:32;index" json:"status"` // active, rate_limited, unconfigured, standby
	IsFreeTier   bool      `gorm:"default:true" json:"is_free_tier"`
	Capabilities string    `gorm:"size:255" json:"capabilities"` // tts, video_generation, image_generation
	Description  string    `gorm:"type:text" json:"description"`
	CreatedAt    time.Time `gorm:"index" json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// CredentialFormatPatternRecord 保存 LLM 成功归一化后的脱敏格式模板，用于后续离线自我进化识别。
// InputTemplate / OutputTemplate 必须只包含固定文案与占位符，绝不保存邮箱、密码、TOTP、Cookie 或代理明文。
type CredentialFormatPatternRecord struct {
	ID             string     `gorm:"primaryKey;size:64" json:"id"`
	Provider       string     `gorm:"size:64;uniqueIndex:idx_credential_format_provider_template" json:"provider"`
	CredentialType string     `gorm:"size:32;index" json:"credential_type"`
	InputTemplate  string     `gorm:"type:text;not null;uniqueIndex:idx_credential_format_provider_template" json:"input_template"`
	OutputTemplate string     `gorm:"type:text;not null" json:"output_template"`
	UseCount       int64      `gorm:"default:0" json:"use_count"`
	SuccessCount   int64      `gorm:"default:0" json:"success_count"`
	FailureCount   int64      `gorm:"default:0" json:"failure_count"`
	Enabled        bool       `gorm:"default:true;index" json:"enabled"`
	LastUsedAt     *time.Time `json:"last_used_at,omitempty"`
	CreatedAt      time.Time  `gorm:"index" json:"created_at"`
	UpdatedAt      time.Time  `gorm:"index" json:"updated_at"`
}

// AudioGenerationRecord 语音工坊配音作品历史记录持久化模型
type AudioGenerationRecord struct {
	ID        string    `gorm:"primaryKey;size:64" json:"id"`
	VoiceID   string    `gorm:"size:64;index" json:"voice_id"`
	VoiceName string    `gorm:"size:128" json:"voice_name"`
	Provider  string    `gorm:"size:64;index" json:"provider"`
	InputText string    `gorm:"type:text;not null" json:"input_text"`
	Speed     float64   `gorm:"default:1.0" json:"speed"`
	Format    string    `gorm:"size:16;default:'mp3'" json:"format"`
	AudioURL  string    `gorm:"type:text" json:"audio_url"`
	BlobData  []byte    `json:"-"`
	Duration  float64   `json:"duration"`
	CreatedAt time.Time `gorm:"index" json:"created_at"`
}

// ProviderAccountRecord 记录各外部供应商（如 Meta Muse / 即梦等）账号、密码、代理与会话 Cookie 持久化
type ProviderAccountRecord struct {
	ID          string    `gorm:"primaryKey;size:128" json:"id"`
	Provider    string    `gorm:"size:64;index" json:"provider"` // meta_muse, jimeng, etc.
	Email       string    `gorm:"size:128;index" json:"email"`
	Password    string    `gorm:"type:text" json:"password,omitempty"`
	Cookie      string    `gorm:"type:text" json:"cookie,omitempty"`
	Proxy       string    `gorm:"size:255" json:"proxy,omitempty"`
	Status      string    `gorm:"size:32;index" json:"status"` // active, pending_login, failed, rate_limited
	LastError   string    `gorm:"type:text" json:"last_error,omitempty"`
	TotalCalls  int64     `gorm:"default:0" json:"total_calls"`
	FailedCalls int64     `gorm:"default:0" json:"failed_calls"`
	CreatedAt   time.Time `gorm:"index" json:"created_at"`
	UpdatedAt   time.Time `gorm:"index" json:"updated_at"`
}

// DramaProjectRecord 短剧工坊无限画布多工程持久化模型
type DramaProjectRecord struct {
	ID            string    `gorm:"primaryKey;size:64" json:"id"`
	Title         string    `gorm:"size:255;index" json:"title"`
	Genre         string    `gorm:"size:64" json:"genre"`
	AspectRatio   string    `gorm:"size:16;default:'9:16'" json:"aspect_ratio"`
	StylePreset   string    `gorm:"size:64" json:"style_preset"`
	RoutingPolicy string    `gorm:"size:32;default:'free_first'" json:"routing_policy"`
	Outline       string    `gorm:"type:text" json:"outline"`
	SchemaVersion int       `gorm:"default:1" json:"schema_version"`
	Revision      int64     `gorm:"default:1" json:"revision"`
	ProjectJSON   string    `gorm:"type:text" json:"project_json,omitempty"`
	LayoutJSON    string    `gorm:"type:text" json:"layout_json,omitempty"`
	GraphJSON     string    `gorm:"type:text" json:"graph_json,omitempty"`
	Status        string    `gorm:"size:32;default:'draft'" json:"status"`
	ShotCount     int       `gorm:"default:0" json:"shot_count"`
	CreatedAt     time.Time `gorm:"index" json:"created_at"`
	UpdatedAt     time.Time `gorm:"index" json:"updated_at"`
}

// DramaRunRecord 短剧后台 DAG 执行 Run 记录
type DramaRunRecord struct {
	ID          string     `gorm:"primaryKey;size:64" json:"id"`
	ProjectID   string     `gorm:"size:64;index" json:"project_id"`
	Status      string     `gorm:"size:32;index" json:"status"` // running, completed, partial, failed, cancelled
	TargetsJSON string     `gorm:"type:text" json:"targets_json,omitempty"`
	ForceJSON   string     `gorm:"type:text" json:"force_json,omitempty"`
	Until       string     `gorm:"size:64" json:"until,omitempty"`
	SummaryJSON string     `gorm:"type:text" json:"summary_json,omitempty"`
	LogsJSON    string     `gorm:"type:text" json:"logs_json,omitempty"`
	Error       string     `gorm:"type:text" json:"error,omitempty"`
	CreatedAt   time.Time  `gorm:"index" json:"created_at"`
	UpdatedAt   time.Time  `gorm:"index" json:"updated_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
}

// FreebieDeal 羊毛与免费额度资讯持久化模型
type FreebieDeal struct {
	ID          string    `gorm:"primaryKey;size:64" json:"id"`
	Title       string    `gorm:"size:512;not null;index" json:"title"`
	Summary     string    `gorm:"type:text" json:"summary"`
	URL         string    `gorm:"size:512;uniqueIndex" json:"url"`
	Source      string    `gorm:"size:64;index" json:"source"`       // linuxdo, nodeseek, v2ex, openrouter, official
	Category    string    `gorm:"size:64;default:'welfare';index" json:"category"` // ai, api, vps, card, general
	Author      string    `gorm:"size:128" json:"author,omitempty"`
	Likes       int       `gorm:"default:0" json:"likes"`
	Replies     int       `gorm:"default:0" json:"replies"`
	Views       int       `gorm:"default:0" json:"views"`
	Score       float64   `gorm:"index" json:"score"`                // 综合热度得分
	IsHot       bool      `gorm:"default:false;index" json:"is_hot"` // 是否上热榜
	TagsJSON    string    `gorm:"type:text" json:"tags_json,omitempty"`
	PublishedAt time.Time `gorm:"index" json:"published_at"`
	CrawledAt   time.Time `gorm:"index" json:"crawled_at"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}
