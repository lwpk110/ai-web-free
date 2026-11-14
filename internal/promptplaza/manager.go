package promptplaza

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Manager 管理提示词广场的内置种子库、多源定时同步、AI 策展与可选持久化落盘。
type Manager struct {
	mu           sync.RWMutex
	items        []PromptItem
	syncStatus   SyncStatus
	persistFile  string
	proxyFunc    ProxyFunc
	chatExecutor PoolChatExecutor
	endpoints    Endpoints
	httpTimeout  time.Duration

	syncMu sync.Mutex
}

// NewManager 初始化提示词广场管理器，加载内置种子库与可选持久化缓存。
func NewManager(opts Options) *Manager {
	seeds, err := LoadSeedItems()
	if err != nil {
		seeds = nil
	}

	m := &Manager{
		items:        seeds,
		persistFile:  strings.TrimSpace(opts.PersistFile),
		proxyFunc:    opts.ProxyFunc,
		chatExecutor: opts.ChatExecutor,
		endpoints:    resolveEndpoints(opts.Endpoints),
		httpTimeout:  opts.HTTPTimeout,
	}

	if m.persistFile != "" {
		m.loadFromDisk()
	}

	m.mu.Lock()
	m.syncStatus.State = "idle"
	m.syncStatus.TotalCount = len(m.items)
	m.syncStatus.SourceCounts = countSourcesLocked(m.items)
	m.mu.Unlock()

	return m
}

func (m *Manager) loadFromDisk() {
	data, err := os.ReadFile(m.persistFile)
	if err != nil || len(data) == 0 {
		return
	}
	var payload persistedPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return
	}
	if len(payload.Items) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.items = mergeItems(m.items, payload.Items)
	if payload.SyncStatus.LastSyncedAt != "" {
		m.syncStatus.LastSyncedAt = payload.SyncStatus.LastSyncedAt
	}
	if len(payload.SyncStatus.ActiveSources) > 0 {
		m.syncStatus.ActiveSources = payload.SyncStatus.ActiveSources
	}
}

func (m *Manager) saveToDisk(items []PromptItem, status SyncStatus) {
	if m.persistFile == "" {
		return
	}
	payload := persistedPayload{
		Items:      items,
		SyncStatus: status,
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return
	}
	dir := filepath.Dir(m.persistFile)
	if dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0755)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(m.persistFile)+".tmp")
	if err != nil {
		_ = os.WriteFile(m.persistFile, raw, 0644)
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(m.persistFile)
	}
	if err := os.Rename(tmpName, m.persistFile); err != nil {
		os.Remove(tmpName)
		_ = os.WriteFile(m.persistFile, raw, 0644)
	}
}

// List 按筛选条件与排序规则返回提示词列表与分类统计。
func (m *Manager) List(filter ListFilter) ListResponse {
	m.mu.RLock()
	snapshot := make([]PromptItem, len(m.items))
	copy(snapshot, m.items)
	status := m.syncStatus
	status.SourceCounts = cloneMap(m.syncStatus.SourceCounts)
	status.ActiveSources = append([]string(nil), m.syncStatus.ActiveSources...)
	m.mu.RUnlock()

	catCounts := map[string]int{"all": len(snapshot)}
	for _, cat := range AllCategories() {
		catCounts[cat] = 0
	}
	srcCounts := map[string]int{"all": len(snapshot)}
	for _, item := range snapshot {
		catCounts[item.Category]++
		srcCounts[item.Source]++
	}

	q := strings.ToLower(strings.TrimSpace(filter.Query))
	catFilter := strings.TrimSpace(filter.Category)
	modeFilter := strings.TrimSpace(filter.Mode)
	srcFilter := strings.TrimSpace(filter.Source)

	filtered := make([]PromptItem, 0, len(snapshot))
	for _, item := range snapshot {
		if catFilter != "" && catFilter != "all" && item.Category != catFilter {
			continue
		}
		if modeFilter != "" && modeFilter != "all" && item.RecommendedMode != modeFilter {
			continue
		}
		if srcFilter != "" && srcFilter != "all" && item.Source != srcFilter {
			continue
		}
		if q != "" {
			haystack := strings.ToLower(item.TitleZH + "\n" + item.PromptEN + "\n" + item.PromptZH + "\n" + item.Category + "\n" + item.Source)
			if !strings.Contains(haystack, q) {
				continue
			}
		}
		filtered = append(filtered, item)
	}

	sortBy := strings.ToLower(strings.TrimSpace(filter.Sort))
	if sortBy == "latest" {
		sort.SliceStable(filtered, func(i, j int) bool {
			if filtered[i].UpdatedAt != filtered[j].UpdatedAt {
				return filtered[i].UpdatedAt > filtered[j].UpdatedAt
			}
			return filtered[i].Stats.Score > filtered[j].Stats.Score
		})
	} else {
		sort.SliceStable(filtered, func(i, j int) bool {
			if filtered[i].Stats.Score != filtered[j].Stats.Score {
				return filtered[i].Stats.Score > filtered[j].Stats.Score
			}
			return filtered[i].Stats.Likes > filtered[j].Stats.Likes
		})
		if srcFilter == "" || srcFilter == "all" {
			filtered = interleaveBySource(filtered)
		}
	}

	return ListResponse{
		Items:        filtered,
		Categories:   catCounts,
		Sources:      srcCounts,
		LastSyncedAt: status.LastSyncedAt,
		SyncStatus:   status,
	}
}

// Status 返回当前同步状态快照。
func (m *Manager) Status() SyncStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	status := m.syncStatus
	status.SourceCounts = cloneMap(m.syncStatus.SourceCounts)
	status.ActiveSources = append([]string(nil), m.syncStatus.ActiveSources...)
	return status
}

// Expand 使用本机号池或降级策略扩写用户创意为完整生图提示词。
func (m *Manager) Expand(ctx context.Context, req ExpandRequest) (ExpandResponse, error) {
	return ExpandIdea(ctx, m.chatExecutor, req)
}

// TriggerSync 触发一次多源采集与 AI 策展同步（支持同步等待或异步后台执行）。
func (m *Manager) TriggerSync(ctx context.Context, async bool) SyncStatus {
	m.mu.Lock()
	if m.syncStatus.Running {
		status := m.syncStatus
		m.mu.Unlock()
		return status
	}
	m.syncStatus.Running = true
	m.syncStatus.State = "running"
	m.syncStatus.LastError = ""
	status := m.syncStatus
	m.mu.Unlock()

	if async {
		go m.runSync(context.Background())
		return status
	}
	m.runSync(ctx)
	return m.Status()
}

func (m *Manager) runSync(ctx context.Context) {
	m.syncMu.Lock()
	defer m.syncMu.Unlock()

	var proxyURL string
	if m.proxyFunc != nil {
		proxyURL = m.proxyFunc()
	}
	client := newCrawlerHTTPClient(proxyURL, m.httpTimeout)
	crawled := crawlAllSources(ctx, client, m.endpoints)

	// 阶段 1：先将爬虫抓回的真实图文素材做快速启发式规范化并立即合入，避免等待 AI 策展造成首屏空窗。
	if len(crawled.Items) > 0 {
		quickCurated := make([]PromptItem, 0, len(crawled.Items))
		for _, it := range crawled.Items {
			quickCurated = append(quickCurated, heuristicCurateItem(it))
		}
		phase1At := time.Now().UTC().Format(time.RFC3339)

		m.mu.Lock()
		merged := mergeItems(m.items, quickCurated)
		m.items = merged
		m.syncStatus.LastSyncedAt = phase1At
		m.syncStatus.ActiveSources = append([]string(nil), crawled.ActiveSources...)
		m.syncStatus.SourceCounts = countSourcesLocked(merged)
		m.syncStatus.TotalCount = len(merged)
		snapshotStatus := m.syncStatus
		snapshotItems := make([]PromptItem, len(merged))
		copy(snapshotItems, merged)
		m.mu.Unlock()

		m.saveToDisk(snapshotItems, snapshotStatus)
	}

	// 阶段 2：若号池执行器可用，则并发执行 AI 策展提炼与联网趋势检索，并二次合入替换或追加条目。
	var incoming []PromptItem
	var aiSearchItems []PromptItem
	var aiErr error
	if m.chatExecutor != nil {
		var curated []PromptItem
		curated, aiSearchItems, aiErr = curateAndSearchWithAI(ctx, m.chatExecutor, crawled.Items)
		incoming = make([]PromptItem, 0, len(curated)+len(aiSearchItems))
		incoming = append(incoming, aiSearchItems...)
		incoming = append(incoming, curated...)
	}

	activeSources := append([]string(nil), crawled.ActiveSources...)
	if len(aiSearchItems) > 0 {
		activeSources = append(activeSources, SourceAISearch)
	}

	var errNotes []string
	if len(crawled.Errors) > 0 {
		errNotes = append(errNotes, crawled.Errors...)
	}
	if aiErr != nil && len(aiSearchItems) == 0 && len(incoming) == 0 {
		errNotes = append(errNotes, aiErr.Error())
	}

	now := time.Now().UTC().Format(time.RFC3339)

	m.mu.Lock()
	merged := m.items
	if len(incoming) > 0 {
		merged = mergeItems(m.items, incoming)
		m.items = merged
	}
	m.syncStatus.Running = false
	m.syncStatus.State = "idle"
	m.syncStatus.LastSyncedAt = now
	m.syncStatus.ActiveSources = activeSources
	m.syncStatus.SourceCounts = countSourcesLocked(merged)
	m.syncStatus.TotalCount = len(merged)
	if len(errNotes) > 0 && len(crawled.Items) == 0 && len(incoming) == 0 {
		m.syncStatus.State = "last_error"
		m.syncStatus.LastError = strings.Join(errNotes, "; ")
	} else {
		m.syncStatus.LastError = ""
	}
	snapshotStatus := m.syncStatus
	snapshotItems := make([]PromptItem, len(merged))
	copy(snapshotItems, merged)
	m.mu.Unlock()

	m.saveToDisk(snapshotItems, snapshotStatus)
}

// StartScheduler 启动周期性后台巡检同步（interval <= 0 时返回空操作清理函数）。
func (m *Manager) StartScheduler(interval time.Duration) func() {
	if interval <= 0 {
		return func() {}
	}
	stopCh := make(chan struct{})
	var once sync.Once
	go func() {
		// 启动后延迟 1 秒执行首次后台预热同步，让多源热榜与真实预览图更快就绪。
		initial := time.NewTimer(1 * time.Second)
		ticker := time.NewTicker(interval)
		defer initial.Stop()
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-initial.C:
				m.TriggerSync(context.Background(), true)
			case <-ticker.C:
				m.TriggerSync(context.Background(), true)
			}
		}
	}()
	return func() {
		once.Do(func() { close(stopCh) })
	}
}

func mergeItems(existing, incoming []PromptItem) []PromptItem {
	byID := make(map[string]PromptItem, len(existing)+len(incoming))
	promptSeen := make(map[string]string, len(existing)+len(incoming))
	order := make([]string, 0, len(existing)+len(incoming))

	upsert := func(it PromptItem) {
		if it.ID == "" || strings.TrimSpace(it.PromptEN) == "" {
			return
		}
		fp := strings.ToLower(strings.Join(strings.Fields(it.PromptEN), " "))
		if prevID, exists := promptSeen[fp]; exists && prevID != it.ID {
			// 同文案去重：保留先入或较高热度版本
			if prev, ok := byID[prevID]; ok && it.Stats.Score <= prev.Stats.Score {
				return
			}
			delete(byID, prevID)
		}
		if _, exists := byID[it.ID]; !exists {
			order = append(order, it.ID)
		}
		byID[it.ID] = it
		promptSeen[fp] = it.ID
	}

	for _, it := range existing {
		upsert(it)
	}
	for _, it := range incoming {
		upsert(it)
	}

	out := make([]PromptItem, 0, len(byID))
	for _, id := range order {
		if it, ok := byID[id]; ok {
			out = append(out, it)
		}
	}

	// 若总数过大，保留全部内置种子 + 热度最高的新增条目（上限 120 条）
	if len(out) > 120 {
		sort.SliceStable(out, func(i, j int) bool {
			iSeed := out[i].Source == SourceSeed
			jSeed := out[j].Source == SourceSeed
			if iSeed != jSeed {
				return iSeed
			}
			return out[i].Stats.Score > out[j].Stats.Score
		})
		out = out[:120]
	}
	return out
}

func countSourcesLocked(items []PromptItem) map[string]int {
	counts := make(map[string]int)
	for _, it := range items {
		counts[it.Source]++
	}
	return counts
}

func cloneMap(src map[string]int) map[string]int {
	if len(src) == 0 {
		return map[string]int{}
	}
	dst := make(map[string]int, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func interleaveBySource(items []PromptItem) []PromptItem {
	if len(items) <= 1 {
		return items
	}
	priorityOrder := []string{
		SourceCivitai,
		SourceAISearch,
		SourceLexica,
		SourceHuggingFace,
		SourceReddit,
		SourceSeed,
	}
	knownPriority := make(map[string]bool, len(priorityOrder))
	for _, p := range priorityOrder {
		knownPriority[p] = true
	}

	buckets := make(map[string][]PromptItem, len(priorityOrder))
	var extraOrder []string
	extraSeen := make(map[string]bool)
	for _, it := range items {
		if !knownPriority[it.Source] && !extraSeen[it.Source] {
			extraSeen[it.Source] = true
			extraOrder = append(extraOrder, it.Source)
		}
		buckets[it.Source] = append(buckets[it.Source], it)
	}

	order := append(append([]string(nil), priorityOrder...), extraOrder...)
	out := make([]PromptItem, 0, len(items))
	for len(out) < len(items) {
		progressed := false
		for _, src := range order {
			b := buckets[src]
			if len(b) == 0 {
				continue
			}
			out = append(out, b[0])
			buckets[src] = b[1:]
			progressed = true
		}
		if !progressed {
			break
		}
	}
	return out
}
