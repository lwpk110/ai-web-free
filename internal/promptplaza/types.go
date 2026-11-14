package promptplaza

import (
	"context"
	"time"
)

// 标准 6 大风格分类，与前端和 OpenSpec 规范保持一致。
const (
	CategoryPhotography       = "摄影写实"
	Category3DMiniature       = "3D 与微缩"
	CategoryPosterTypography  = "海报与文字排版"
	CategoryIllustrationAnime = "插画与二次元"
	CategoryProductUI         = "产品与 UI"
	CategoryReferenceEdit     = "参考图编辑"
)

// 数据源标识常量。
const (
	SourceSeed        = "seed"
	SourceCivitai     = "civitai"
	SourceLexica      = "lexica"
	SourceReddit      = "reddit"
	SourceHuggingFace = "huggingface"
	SourceAISearch    = "ai_search"
)

// DefaultLexicaTopics 默认轮换抓取的高审美主题词。
var DefaultLexicaTopics = []string{
	"isometric miniature diorama",
	"minimalist product photography",
	"editorial typography poster",
	"studio ghibli watercolor landscape",
}

// AllCategories 返回标准有序分类列表。
func AllCategories() []string {
	return []string{
		CategoryPhotography,
		Category3DMiniature,
		CategoryPosterTypography,
		CategoryIllustrationAnime,
		CategoryProductUI,
		CategoryReferenceEdit,
	}
}

// PromptVariable 描述模板中的可交互变量槽位（形如 [主体]）。
type PromptVariable struct {
	Key          string   `json:"key"`
	Label        string   `json:"label"`
	DefaultValue string   `json:"default_value"`
	Options      []string `json:"options,omitempty"`
}

// PromptStats 表示社区互动热度与综合评分。
type PromptStats struct {
	Likes  int     `json:"likes"`
	Hearts int     `json:"hearts"`
	Score  float64 `json:"score"`
}

// PromptItem 表示提示词广场中的单条提示词条目。
type PromptItem struct {
	ID               string           `json:"id"`
	TitleZH          string           `json:"title_zh"`
	Category         string           `json:"category"`
	PromptEN         string           `json:"prompt_en"`
	PromptZH         string           `json:"prompt_zh"`
	PreviewURL       string           `json:"preview_url,omitempty"`
	RecommendedMode  string           `json:"recommended_mode"`
	RecommendedModel string           `json:"recommended_model"`
	RecommendedSize  string           `json:"recommended_size"`
	Variables        []PromptVariable `json:"variables,omitempty"`
	Source           string           `json:"source"`
	Stats            PromptStats      `json:"stats"`
	UpdatedAt        string           `json:"updated_at"`
}

// SyncStatus 描述最近一次多源同步任务的运行与统计状态。
type SyncStatus struct {
	State         string         `json:"state"`
	Running       bool           `json:"running"`
	LastSyncedAt  string         `json:"last_synced_at,omitempty"`
	LastError     string         `json:"last_error,omitempty"`
	ActiveSources []string       `json:"active_sources,omitempty"`
	SourceCounts  map[string]int `json:"source_counts,omitempty"`
	TotalCount    int            `json:"total_count"`
}

// ListFilter 定义 GET /admin/api/v1/prompts 的筛选与排序条件。
type ListFilter struct {
	Category string
	Mode     string
	Source   string
	Query    string
	Sort     string // "popular" (default) | "latest"
}

// ListResponse 定义提示词广场列表接口响应体。
type ListResponse struct {
	Items        []PromptItem   `json:"items"`
	Categories   map[string]int `json:"categories"`
	Sources      map[string]int `json:"sources"`
	LastSyncedAt string         `json:"last_synced_at,omitempty"`
	SyncStatus   SyncStatus     `json:"sync_status"`
}

// ExpandRequest 定义 AI 魔法扩写请求体。
type ExpandRequest struct {
	Idea string `json:"idea"`
	Mode string `json:"mode,omitempty"`
	Size string `json:"size,omitempty"`
}

// ExpandResponse 定义 AI 魔法扩写响应体。
type ExpandResponse struct {
	TitleZH          string           `json:"title_zh"`
	Category         string           `json:"category"`
	PromptEN         string           `json:"prompt_en"`
	PromptZH         string           `json:"prompt_zh"`
	RecommendedMode  string           `json:"recommended_mode"`
	RecommendedModel string           `json:"recommended_model"`
	RecommendedSize  string           `json:"recommended_size"`
	Variables        []PromptVariable `json:"variables,omitempty"`
	Source           string           `json:"source"`
}

// ProxyFunc 返回可供爬虫使用的代理地址（空字符串表示直连）。
type ProxyFunc func() string

// PoolChatExecutor 调用本机 ChatGPT 号池执行文本对话或联网搜索。
type PoolChatExecutor func(ctx context.Context, systemPrompt, userPrompt string, useWebSearch bool) (string, error)

// Endpoints 配置多源爬虫的外部接口地址，便于测试注入 mock server。
type Endpoints struct {
	CivitaiURL     string
	LexicaURL      string
	RedditURL      string
	HuggingFaceURL string
	LexicaTopics   []string
}

// Options 配置 PromptPlaza Manager。
type Options struct {
	PersistFile  string
	ProxyFunc    ProxyFunc
	ChatExecutor PoolChatExecutor
	Endpoints    Endpoints
	HTTPTimeout  time.Duration
}

// persistedPayload 定义可选落盘 JSON 的完整结构。
type persistedPayload struct {
	Items      []PromptItem `json:"items"`
	SyncStatus SyncStatus   `json:"sync_status"`
}
