package deals

import (
	"time"

	"ai-web-free/internal/db"
)

// Deal 表示单条羊毛与免费额度资讯
type Deal struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Summary     string    `json:"summary"`
	URL         string    `json:"url"`
	Source      string    `json:"source"`   // linuxdo, nodeseek, v2ex, openrouter, official
	Category    string    `json:"category"` // ai, api, vps, card, general
	Author      string    `json:"author,omitempty"`
	Likes       int       `json:"likes"`
	Replies     int       `json:"replies"`
	Views       int       `json:"views"`
	Score       float64   `json:"score"`
	IsHot       bool      `json:"is_hot"`
	Tags        []string  `json:"tags,omitempty"`
	PublishedAt time.Time `json:"published_at"`
	CrawledAt   time.Time `json:"crawled_at"`
}

// FromDB 将数据库记录转换为前端传输对象
func FromDB(r *db.FreebieDeal) Deal {
	var tags []string
	if r.TagsJSON != "" {
		_ = jsonUnmarshal(r.TagsJSON, &tags)
	}
	return Deal{
		ID:          r.ID,
		Title:       r.Title,
		Summary:     r.Summary,
		URL:         r.URL,
		Source:      r.Source,
		Category:    r.Category,
		Author:      r.Author,
		Likes:       r.Likes,
		Replies:     r.Replies,
		Views:       r.Views,
		Score:       r.Score,
		IsHot:       r.IsHot,
		Tags:        tags,
		PublishedAt: r.PublishedAt,
		CrawledAt:   r.CrawledAt,
	}
}

// DealsQuery 列表查询参数
type DealsQuery struct {
	Sort     string `json:"sort"`     // hot (最热), newest (最新)
	HotOnly  bool   `json:"hot_only"` // 仅查询热榜资讯
	Source   string `json:"source"`   // all, linuxdo, nodeseek, v2ex, openrouter
	Category string `json:"category"` // all, ai, api, vps, card
	Keyword  string `json:"keyword"`
	Limit    int    `json:"limit"`
	Offset   int    `json:"offset"`
}

// DealsStatusResponse 状态查询响应
type DealsStatusResponse struct {
	TotalCount   int64     `json:"total_count"`
	HotCount     int64     `json:"hot_count"`
	LastUpdateAt time.Time `json:"last_update_at"`
	NextUpdateAt time.Time `json:"next_update_at"`
	Interval     string    `json:"interval"`
	IsRunning    bool      `json:"is_running"`
	Sources      []string  `json:"sources"`
}
