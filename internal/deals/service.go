package deals

import (
	"context"
	"errors"
	"log"
	"strings"
	"sync"
	"time"

	"ai-web-free/internal/db"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	globalService   *Service
	globalServiceMu sync.RWMutex
)

// SetService 设置全局 Deals 服务
func SetService(s *Service) {
	globalServiceMu.Lock()
	defer globalServiceMu.Unlock()
	globalService = s
}

// GetService 获取全局 Deals 服务
func GetService() *Service {
	globalServiceMu.RLock()
	defer globalServiceMu.RUnlock()
	return globalService
}

type Service struct {
	mu           sync.RWMutex
	database     *gorm.DB
	crawler      *Crawler
	interval     time.Duration
	lastUpdateAt time.Time
	nextUpdateAt time.Time
	isRunning    bool
	stopChan     chan struct{}
}

// NewService 初始化羊毛与免费资讯调度服务（默认每 4 小时调度一次）
func NewService(database *gorm.DB, interval time.Duration) *Service {
	if interval <= 0 {
		interval = 4 * time.Hour
	}
	s := &Service{
		database: database,
		crawler:  NewCrawler(15 * time.Second),
		interval: interval,
		stopChan: make(chan struct{}),
	}
	return s
}

// Start 启动后台定时任务
func (s *Service) Start() {
	s.mu.Lock()
	if s.isRunning {
		s.mu.Unlock()
		return
	}
	s.isRunning = true
	s.nextUpdateAt = time.Now().Add(s.interval)
	s.mu.Unlock()

	// 启动后异步立即做一次初始抓取
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		_, _ = s.RefreshNow(ctx)
	}()

	// 4 小时定时轮询
	go s.scheduleLoop()
}

// Stop 停止调度
func (s *Service) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.isRunning {
		return
	}
	s.isRunning = false
	close(s.stopChan)
}

func (s *Service) scheduleLoop() {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.mu.Lock()
			s.nextUpdateAt = time.Now().Add(s.interval)
			s.mu.Unlock()

			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			_, err := s.RefreshNow(ctx)
			cancel()
			if err != nil {
				log.Printf("[Deals] Periodic fetch failed: %v", err)
			}
		}
	}
}

// RefreshNow 手动或定时执行一次全量抓取与数据库热度更新
func (s *Service) RefreshNow(ctx context.Context) (int, error) {
	deals := s.crawler.FetchAll(ctx)
	if len(deals) == 0 {
		return 0, errors.New("抓取结果为空")
	}

	gdb := s.database
	if gdb == nil {
		gdb = db.GetDB()
	}
	if gdb == nil {
		return 0, errors.New("数据库未就绪")
	}

	now := time.Now()
	savedCount := 0

	for _, item := range deals {
		// 根据 URL 幂等入库并更新热度指标
		err := gdb.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "url"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"id", "title", "summary", "source", "category", "author", "published_at", "likes", "replies", "views",
				"score", "is_hot", "tags_json", "crawled_at", "updated_at",
			}),
		}).Create(&item).Error

		if err == nil {
			savedCount++
		}
	}
	if err := s.recalculateStoredDeals(gdb, now); err != nil {
		return savedCount, err
	}

	s.mu.Lock()
	s.lastUpdateAt = now
	s.mu.Unlock()

	return savedCount, nil
}

// recalculateStoredDeals repairs legacy synthetic activity counts and refreshes normalized scores.
func (s *Service) recalculateStoredDeals(gdb *gorm.DB, now time.Time) error {
	var records []db.FreebieDeal
	if err := gdb.Find(&records).Error; err != nil {
		return err
	}
	for i := range records {
		record := &records[i]
		// Earlier crawler versions filled unsupported RSS/API activity with fabricated metrics.
		if !strings.HasPrefix(record.ID, "seed-") {
			switch record.Source {
			case "nodeseek":
				record.Likes, record.Replies, record.Views = 0, 0, 0
			case "v2ex":
				record.Likes, record.Views = 0, 0
			}
		} else {
			record.Likes, record.Replies, record.Views = 0, 0, 0
		}
		record.Score = CalculateHeatScore(record.Likes, record.Replies, record.Views, record.PublishedAt, now)
		record.IsHot = !strings.HasPrefix(record.ID, "seed-") && record.Score >= 12.0
		if err := gdb.Model(record).Updates(map[string]interface{}{
			"likes": record.Likes, "replies": record.Replies, "views": record.Views,
			"score": record.Score, "is_hot": record.IsHot,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

// ListDeals 查询羊毛资讯列表（支持按最热/最新排序与筛选）
func (s *Service) ListDeals(q DealsQuery) ([]Deal, int64, error) {
	gdb := s.database
	if gdb == nil {
		gdb = db.GetDB()
	}
	if gdb == nil {
		return nil, 0, errors.New("数据库未就绪")
	}

	tx := gdb.Model(&db.FreebieDeal{})

	// 来源筛选
	if q.Source != "" && q.Source != "all" {
		tx = tx.Where("source = ?", strings.ToLower(q.Source))
	}
	// 分类筛选
	if q.Category != "" && q.Category != "all" {
		tx = tx.Where("category = ?", strings.ToLower(q.Category))
	}
	if q.HotOnly {
		tx = tx.Where("is_hot = ? AND id NOT LIKE ? AND source != ? AND ((source = ? AND replies > 0) OR (source != ? AND (likes > 0 OR replies > 0 OR views > 0)))", true, "seed-%", "nodeseek", "v2ex", "v2ex")
	}
	// 关键词检索
	if q.Keyword != "" {
		kw := "%" + strings.ToLower(q.Keyword) + "%"
		tx = tx.Where("(LOWER(title) LIKE ? OR LOWER(summary) LIKE ? OR LOWER(tags_json) LIKE ?)", kw, kw, kw)
	}

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	// 排序逻辑：默认最热优先（Score 降序），支持多种排序模式
	switch q.Sort {
	case "newest":
		tx = tx.Order("published_at desc, score desc")
	case "likes":
		tx = tx.Order("likes desc, score desc, published_at desc")
	case "replies":
		tx = tx.Order("replies desc, score desc, published_at desc")
	default:
		tx = tx.Order("score desc, likes desc, published_at desc")
	}

	limit := q.Limit
	if limit <= 0 || limit > 200 {
		limit = 60
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	tx = tx.Limit(limit).Offset(offset)

	var records []db.FreebieDeal
	if err := tx.Find(&records).Error; err != nil {
		return nil, 0, err
	}

	out := make([]Deal, 0, len(records))
	for i := range records {
		out = append(out, FromDB(&records[i]))
	}
	return out, total, nil
}

// Status 获取服务调度与状态概览
func (s *Service) Status() DealsStatusResponse {
	s.mu.RLock()
	last := s.lastUpdateAt
	next := s.nextUpdateAt
	interval := s.interval
	running := s.isRunning
	s.mu.RUnlock()

	gdb := s.database
	if gdb == nil {
		gdb = db.GetDB()
	}

	var total int64
	var hotCount int64
	if gdb != nil {
		_ = gdb.Model(&db.FreebieDeal{}).Count(&total).Error
		_ = gdb.Model(&db.FreebieDeal{}).Where("is_hot = ? AND id NOT LIKE ? AND source != ? AND ((source = ? AND replies > 0) OR (source != ? AND (likes > 0 OR replies > 0 OR views > 0)))", true, "seed-%", "nodeseek", "v2ex", "v2ex").Count(&hotCount).Error
	}

	return DealsStatusResponse{
		TotalCount:   total,
		HotCount:     hotCount,
		LastUpdateAt: last,
		NextUpdateAt: next,
		Interval:     interval.String(),
		IsRunning:    running,
		Sources:      []string{"linuxdo", "nodeseek", "v2ex", "official"},
	}
}
