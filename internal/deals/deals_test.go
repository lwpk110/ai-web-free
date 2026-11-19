package deals

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"ai-web-free/internal/db"
)

func TestCalculateHeatScore(t *testing.T) {
	now := time.Now()

	// 1小时前发布的高赞内容应该比24小时前发布的更高分
	scoreRecent := CalculateHeatScore(100, 50, 1000, now.Add(-1*time.Hour), now)
	scoreOld := CalculateHeatScore(100, 50, 1000, now.Add(-24*time.Hour), now)

	if scoreRecent <= scoreOld {
		t.Fatalf("recent deal score (%f) must be higher than old deal score (%f)", scoreRecent, scoreOld)
	}

	// 没有可用互动指标时不伪造热度分
	scoreZero := CalculateHeatScore(0, 0, 0, now, now)
	if scoreZero != 0 {
		t.Fatalf("score with no activity data should be 0, got %f", scoreZero)
	}
	if scoreRecent > 100 {
		t.Fatalf("normalized heat score should not exceed 100, got %f", scoreRecent)
	}
	if scorePopular := CalculateHeatScore(50000, 10000, 1000000, now, now); scorePopular >= 100 {
		t.Fatalf("normalized heat score should approach but not reach 100, got %f", scorePopular)
	}
}

func TestDealsServiceFlow(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test-deals.db")
	database, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("init test db failed: %v", err)
	}
	sqlDB, err := database.DB()
	if err == nil && sqlDB != nil {
		defer sqlDB.Close()
	}

	srv := NewService(database, 10*time.Millisecond)

	// 手动触发一次抓取（内网使用 seed 兜底）
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	count, err := srv.RefreshNow(ctx)
	if err != nil {
		t.Fatalf("refresh now failed: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected non-zero deals saved, got 0")
	}

	// 插入一条非热榜线报，确保热榜筛选不只是按热度排序。
	coldDeal := db.FreebieDeal{
		ID:          "test-cold-deal",
		Title:       "普通线报筛选测试",
		URL:         "https://example.com/test-cold-deal",
		Source:      "official",
		Category:    "general",
		PublishedAt: time.Now(),
		IsHot:       false,
	}
	if err := database.Create(&coldDeal).Error; err != nil {
		t.Fatalf("insert non-hot deal failed: %v", err)
	}
	hotDeal := db.FreebieDeal{
		ID:          "test-hot-deal",
		Title:       "真实互动热榜筛选测试",
		URL:         "https://example.com/test-hot-deal",
		Source:      "linuxdo",
		Category:    "general",
		Likes:       50,
		Replies:     20,
		Views:       500,
		Score:       CalculateHeatScore(50, 20, 500, time.Now(), time.Now()),
		IsHot:       true,
		PublishedAt: time.Now(),
	}
	if err := database.Create(&hotDeal).Error; err != nil {
		t.Fatalf("insert hot deal failed: %v", err)
	}

	// 列表查询：按最热排序
	items, total, err := srv.ListDeals(DealsQuery{Sort: "hot", Limit: 10})
	if err != nil {
		t.Fatalf("list deals failed: %v", err)
	}
	if total == 0 || len(items) == 0 {
		t.Fatalf("expected deals returned, got total=%d len=%d", total, len(items))
	}

	// 校验排序：第一个 item 的 Score 应该 >= 第二个 item 的 Score
	if len(items) >= 2 && items[0].Score < items[1].Score {
		t.Fatalf("deals should be sorted by score desc: %f vs %f", items[0].Score, items[1].Score)
	}

	hotItems, hotTotal, err := srv.ListDeals(DealsQuery{Sort: "hot", HotOnly: true, Limit: 200})
	if err != nil {
		t.Fatalf("hot-only list query failed: %v", err)
	}
	if hotTotal >= total {
		t.Fatalf("hot-only total should exclude non-hot records: hot=%d total=%d", hotTotal, total)
	}
	for _, item := range hotItems {
		if !item.IsHot {
			t.Fatalf("hot-only list returned non-hot deal %q", item.ID)
		}
	}

	// 校验状态概览
	st := srv.Status()
	if st.TotalCount == 0 {
		t.Fatalf("expected non-zero status total count, got %d", st.TotalCount)
	}
}
