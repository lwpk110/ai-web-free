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
	BlobData      []byte    `gorm:"type:blob" json:"-"`                          // 仅在无磁盘模式下兜底
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
	ID           string    `gorm:"primaryKey;size:64" json:"id"`
	Title        string    `gorm:"size:255;index" json:"title"`
	Model        string    `gorm:"size:64" json:"model"`
	SystemPrompt string    `gorm:"type:text" json:"system_prompt,omitempty"`
	PresetID     string    `gorm:"size:64;index" json:"preset_id,omitempty"`
	IsPinned     bool      `gorm:"default:false;index" json:"is_pinned"`
	IsArchived   bool      `gorm:"default:false;index" json:"is_archived"`
	Tags         string    `gorm:"size:255" json:"tags,omitempty"`
	ClientIP     string    `gorm:"size:64" json:"client_ip,omitempty"`
	MessageCount int       `gorm:"default:0" json:"message_count"`
	CreatedAt    time.Time `gorm:"index" json:"created_at"`
	UpdatedAt    time.Time `gorm:"index" json:"updated_at"`
}

// ChatMessageRecord AI 对话消息记录模型（归属于 Conversation 或旧版 SessionKey）
type ChatMessageRecord struct {
	ID             string    `gorm:"primaryKey;size:64" json:"id"`
	ConversationID string    `gorm:"size:64;index" json:"conversation_id"` // 对应 Conversation.ID
	SessionKey     string    `gorm:"size:64;index;default:'default'" json:"session_key,omitempty"` // 兼容旧版单一会话
	Role           string    `gorm:"size:32" json:"role"` // user, assistant, system
	Content        string    `gorm:"type:text" json:"content"`
	Model          string    `gorm:"size:64" json:"model,omitempty"`
	OpenUIMode     bool      `gorm:"default:false" json:"openui_mode"`
	Skill          string    `gorm:"size:32;default:'auto'" json:"skill,omitempty"`
	TokensPrompt   int       `gorm:"default:0" json:"tokens_prompt,omitempty"`
	TokensComp     int       `gorm:"default:0" json:"tokens_comp,omitempty"`
	LatencyMs      int64     `gorm:"default:0" json:"latency_ms,omitempty"`
	Citations      string    `gorm:"type:text" json:"citations,omitempty"` // 格式化 JSON 字符串存放引用来源
	CreatedAt      time.Time `gorm:"index" json:"created_at"`
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
	BlobData  []byte    `gorm:"type:blob" json:"-"`
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
	GraphJSON     string    `gorm:"type:text" json:"graph_json"`
	Status        string    `gorm:"size:32;default:'draft'" json:"status"`
	ShotCount     int       `gorm:"default:0" json:"shot_count"`
	CreatedAt     time.Time `gorm:"index" json:"created_at"`
	UpdatedAt     time.Time `gorm:"index" json:"updated_at"`
}
