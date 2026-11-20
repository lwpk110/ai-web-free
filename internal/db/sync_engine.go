package db

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

type cloudOpenerFn func(ctx context.Context, dsn string, extraModels ...any) (*gorm.DB, error)

// SyncEngine 管理本地 SQLite 物化库与远程高延迟云数据库之间的异步双向同步。
type SyncEngine struct {
	localDB  *gorm.DB
	cloudDSN string
	cfg      SyncConfig

	cloudOpener cloudOpenerFn
	cloudDB     *gorm.DB

	syncSem chan struct{}
	metaMu  sync.Mutex
	stateMu sync.RWMutex

	tables     []SyncTableSpec
	tableMap   map[string]SyncTableSpec
	tableStats map[string]*TableSyncStatus

	state               string
	cloudConnected      bool
	initialHydrated     bool
	lastPushAt          time.Time
	lastPullAt          time.Time
	lastSyncLatencyMs   int64
	totalPushed         int64
	totalPulled         int64
	totalDeleted        int64
	consecutiveFailures int
	lastError           string
	nextRetryAt         time.Time

	stopOnce sync.Once
	stopCh   chan struct{}
	cancel   context.CancelFunc
	loopCtx  context.Context
	wg       sync.WaitGroup
	started  bool
}

// NewSyncEngine 创建物化数据库同步引擎（调用 PrepareLocal 与 Start 后开始后台运行）。
func NewSyncEngine(localDB *gorm.DB, cloudDSN string, cfg SyncConfig) *SyncEngine {
	if cfg.PushInterval <= 0 {
		cfg.PushInterval = 5 * time.Second
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}

	loopCtx, cancel := context.WithCancel(context.Background())
	syncSem := make(chan struct{}, 1)
	e := &SyncEngine{
		localDB:     localDB,
		cloudDSN:    strings.TrimSpace(cloudDSN),
		cfg:         cfg,
		cloudOpener: OpenCloudDB,
		syncSem:     syncSem,
		tableMap:    make(map[string]SyncTableSpec),
		tableStats:  make(map[string]*TableSyncStatus),
		state:       "idle",
		stopCh:      make(chan struct{}),
		cancel:      cancel,
		loopCtx:     loopCtx,
	}
	return e
}

func (e *SyncEngine) acquireSync(ctx context.Context) error {
	if ctx == nil {
		e.syncSem <- struct{}{}
		return nil
	}
	select {
	case e.syncSem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *SyncEngine) releaseSync() {
	<-e.syncSem
}

// SetCloudOpener 替换云端数据库连接工厂（主要用于单元测试注入模拟云库）。
func (e *SyncEngine) SetCloudOpener(fn func(ctx context.Context, dsn string, extraModels ...any) (*gorm.DB, error)) {
	e.metaMu.Lock()
	defer e.metaMu.Unlock()
	if fn != nil {
		e.cloudOpener = fn
	}
}

// RegisterTables 注册参与物化同步的业务表规格（按表名幂等去重）。
func (e *SyncEngine) RegisterTables(specs ...SyncTableSpec) {
	e.metaMu.Lock()
	defer e.metaMu.Unlock()
	e.stateMu.Lock()
	defer e.stateMu.Unlock()

	for _, spec := range specs {
		if spec.TableName == "" {
			continue
		}
		if _, exists := e.tableMap[spec.TableName]; exists {
			continue
		}
		e.tables = append(e.tables, spec)
		e.tableMap[spec.TableName] = spec
		e.tableStats[spec.TableName] = &TableSyncStatus{
			TableName:   spec.TableName,
			HasBlobData: spec.HasBlobData,
		}
	}
}

// PrepareLocal 在本地 SQLite 初始化 _sync_control/_sync_outbox/_sync_state 并安装全部触发器。
func (e *SyncEngine) PrepareLocal() error {
	e.metaMu.Lock()
	tables := append([]SyncTableSpec(nil), e.tables...)
	e.metaMu.Unlock()

	if len(tables) == 0 {
		e.RegisterTables(DefaultSyncTables()...)
		e.metaMu.Lock()
		tables = append([]SyncTableSpec(nil), e.tables...)
		e.metaMu.Unlock()
	}

	if err := initLocalSyncMetadataAndTriggers(e.localDB, tables); err != nil {
		return err
	}
	e.loadPersistedSyncState()
	return nil
}

func (e *SyncEngine) loadPersistedSyncState() {
	var rows []syncStateRow
	if err := e.localDB.Raw(`SELECT table_name, last_pull_at, initial_reconciled, updated_at FROM _sync_state`).Scan(&rows).Error; err != nil {
		return
	}
	byName := make(map[string]syncStateRow, len(rows))
	for _, r := range rows {
		byName[r.TableName] = r
	}

	e.stateMu.Lock()
	defer e.stateMu.Unlock()

	allHydrated := len(e.tables) > 0
	for _, tbl := range e.tables {
		st := e.tableStats[tbl.TableName]
		if st == nil {
			continue
		}
		if row, ok := byName[tbl.TableName]; ok {
			st.LastPullAt = row.LastPullAt
			st.InitialReconciled = row.InitialReconciled == 1
			if !st.InitialReconciled {
				allHydrated = false
			}
			if t := parseOutboxTime(row.LastPullAt); !t.IsZero() && t.After(e.lastPullAt) {
				e.lastPullAt = t
			}
		} else {
			allHydrated = false
		}
	}
	e.initialHydrated = allHydrated
}

// Start 非阻塞启动后台异步同步协程，绝不阻塞 HTTP 端口监听。
func (e *SyncEngine) Start() {
	e.stateMu.Lock()
	if e.started {
		e.stateMu.Unlock()
		return
	}
	e.started = true
	e.state = "connecting"
	e.stateMu.Unlock()

	e.wg.Add(1)
	go e.runLoop()
}

// Stop 优雅停机：停止后台调度并在 timeout 内把剩余 _sync_outbox 变更推送至云端。
func (e *SyncEngine) Stop(timeout time.Duration) {
	if e == nil {
		return
	}
	e.stopOnce.Do(func() {
		close(e.stopCh)
		e.cancel()
		e.wg.Wait()

		if timeout <= 0 {
			timeout = 5 * time.Second
		}
		if e.countOutboxPending() > 0 && e.cloudDSN != "" {
			flushCtx, flushCancel := context.WithTimeout(context.Background(), timeout)
			defer flushCancel()
			if err := e.SyncOnce(flushCtx, SyncDirectionPush); err != nil {
				log.Printf("[DB Sync] 停机刷盘未完全成功（本地 SQLite Outbox 已持久保留待下次补推）: %v", err)
			}
		}

		if err := e.acquireSync(context.Background()); err == nil {
			if e.cloudDB != nil {
				if sqlDB, err := e.cloudDB.DB(); err == nil {
					_ = sqlDB.Close()
				}
				e.cloudDB = nil
			}
			e.releaseSync()
		}

		e.stateMu.Lock()
		e.state = "stopped"
		e.cloudConnected = false
		e.stateMu.Unlock()
		e.notifyStatusChange()
	})
}

func (e *SyncEngine) runLoop() {
	defer e.wg.Done()

	// 首次后台建连与双向对齐允许更长超时（首次冷建表时云端 AutoMigrate 可能耗时较长）
	initTimeout := e.cfg.Timeout
	if initTimeout < 180*time.Second {
		initTimeout = 180 * time.Second
	}
	initCtx, cancelInit := context.WithTimeout(e.loopCtx, initTimeout)
	if err := e.SyncOnce(initCtx, SyncDirectionBoth); err != nil && e.loopCtx.Err() == nil {
		log.Printf("[DB Sync] 首次后台云端对齐暂未就绪（本地 SQLite 正常运行，稍后自动重试）: %v", err)
	}
	cancelInit()

	pushTicker := time.NewTicker(e.cfg.PushInterval)
	defer pushTicker.Stop()

	var pullTicker *time.Ticker
	var pullC <-chan time.Time
	if e.cfg.PullInterval > 0 {
		pullTicker = time.NewTicker(e.cfg.PullInterval)
		pullC = pullTicker.C
		defer pullTicker.Stop()
	}

	for {
		select {
		case <-e.stopCh:
			return
		case <-pushTicker.C:
			if e.inBackoff() {
				continue
			}
			pending := e.countOutboxPending()
			hydrated := e.isInitialHydrated()
			if pending == 0 && hydrated {
				// 空闲且已完成初始化水合时：零云库查询，利于 Neon Serverless 自动休眠省额度
				continue
			}
			dir := SyncDirectionPush
			if !hydrated {
				dir = SyncDirectionBoth
			}
			ctx, cancel := context.WithTimeout(e.loopCtx, e.cfg.Timeout)
			_ = e.SyncOnce(ctx, dir)
			cancel()
		case <-pullC:
			if e.inBackoff() {
				continue
			}
			ctx, cancel := context.WithTimeout(e.loopCtx, e.cfg.Timeout)
			_ = e.SyncOnce(ctx, SyncDirectionPull)
			cancel()
		}
	}
}

func (e *SyncEngine) inBackoff() bool {
	e.stateMu.RLock()
	defer e.stateMu.RUnlock()
	return !e.nextRetryAt.IsZero() && time.Now().UTC().Before(e.nextRetryAt)
}

func (e *SyncEngine) isInitialHydrated() bool {
	e.stateMu.RLock()
	defer e.stateMu.RUnlock()
	return e.initialHydrated
}

// TriggerSync 手动触发一次同步（重置退避冷却并执行指定方向），返回最新状态快照。
func (e *SyncEngine) TriggerSync(ctx context.Context, dir SyncDirection) (SyncStatus, error) {
	if e == nil {
		return SyncStatus{Enabled: false, Mode: "local_only", State: "disabled"}, fmt.Errorf("sync engine is not initialized")
	}
	if dir == "" {
		dir = SyncDirectionBoth
	}
	e.stateMu.Lock()
	e.nextRetryAt = time.Time{}
	e.stateMu.Unlock()

	err := e.SyncOnce(ctx, dir)
	return e.Status(), err
}

// SyncOnce 串行执行一次指定方向（both / push / pull）的云端同步。
func (e *SyncEngine) SyncOnce(ctx context.Context, dir SyncDirection) error {
	if e == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if dir == "" {
		dir = SyncDirectionBoth
	}

	if err := e.acquireSync(ctx); err != nil {
		return err
	}
	defer e.releaseSync()

	startedAt := time.Now()
	if !e.isInitialHydrated() && dir != SyncDirectionPush {
		e.setTransientState("hydrating")
	} else {
		e.setTransientState("syncing")
	}

	cloudDB, err := e.ensureCloudDBLocked(ctx)
	if err != nil {
		e.recordFailure(err, time.Since(startedAt))
		return err
	}

	var pulledChanged map[string]int
	if dir == SyncDirectionBoth || dir == SyncDirectionPull {
		changed, pullErr := e.pullAllLocked(ctx, cloudDB)
		if pullErr != nil {
			e.maybeResetCloudDBLocked(ctx, cloudDB)
			e.recordFailure(pullErr, time.Since(startedAt))
			return pullErr
		}
		pulledChanged = changed
		e.stateMu.Lock()
		e.lastPullAt = time.Now().UTC()
		e.initialHydrated = true
		e.stateMu.Unlock()
	}

	if dir == SyncDirectionBoth || dir == SyncDirectionPush {
		if pushErr := e.pushPendingLocked(ctx, cloudDB); pushErr != nil {
			e.maybeResetCloudDBLocked(ctx, cloudDB)
			e.recordFailure(pushErr, time.Since(startedAt))
			return pushErr
		}
	}

	e.recordSuccess(time.Since(startedAt))
	if len(pulledChanged) > 0 && e.cfg.OnRemoteChanged != nil {
		e.cfg.OnRemoteChanged(pulledChanged)
	}
	return nil
}

func (e *SyncEngine) extraCloudModels() []any {
	coreNames := make(map[string]struct{})
	cache := &sync.Map{}
	namer := schema.NamingStrategy{}
	for _, m := range CoreModels() {
		if sch, err := schema.Parse(m, cache, namer); err == nil && sch.Table != "" {
			coreNames[sch.Table] = struct{}{}
		}
	}
	extra := []any{&SyncTombstoneRecord{}, &SyncJournalRecord{}}
	e.metaMu.Lock()
	tables := append([]SyncTableSpec(nil), e.tables...)
	e.metaMu.Unlock()
	for _, tbl := range tables {
		if _, isCore := coreNames[tbl.TableName]; !isCore && tbl.NewModel != nil {
			extra = append(extra, tbl.NewModel())
		}
	}
	return extra
}

func (e *SyncEngine) ensureCloudDBLocked(ctx context.Context) (*gorm.DB, error) {
	if e.cloudDB != nil {
		return e.cloudDB, nil
	}
	if e.cloudDSN == "" {
		return nil, fmt.Errorf("cloud DATABASE_URL is empty")
	}

	e.metaMu.Lock()
	opener := e.cloudOpener
	e.metaMu.Unlock()

	e.setTransientState("connecting")
	cloudDB, err := opener(ctx, e.cloudDSN, e.extraCloudModels()...)
	if err != nil {
		return nil, err
	}
	e.cloudDB = cloudDB
	e.stateMu.Lock()
	e.cloudConnected = true
	e.stateMu.Unlock()
	return cloudDB, nil
}

func (e *SyncEngine) maybeResetCloudDBLocked(ctx context.Context, cloudDB *gorm.DB) {
	if cloudDB == nil {
		return
	}
	pingCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	sqlDB, err := cloudDB.DB()
	if err != nil || sqlDB.PingContext(pingCtx) != nil {
		if sqlDB != nil {
			_ = sqlDB.Close()
		}
		e.cloudDB = nil
		e.stateMu.Lock()
		e.cloudConnected = false
		e.stateMu.Unlock()
	}
}

func (e *SyncEngine) pullAllLocked(ctx context.Context, cloudDB *gorm.DB) (map[string]int, error) {
	// 预先读取各表同步状态，用于一次性聚合查询 _sync_journal 捕获离线延迟推送或时钟偏差的变更主键
	var stRows []syncStateRow
	_ = e.localDB.WithContext(ctx).Raw(`SELECT table_name, last_pull_at, initial_reconciled, updated_at FROM _sync_state`).Scan(&stRows).Error
	stateByTable := make(map[string]syncStateRow, len(stRows))
	var minSince time.Time
	hasIncremental := false
	for _, r := range stRows {
		stateByTable[r.TableName] = r
		if r.InitialReconciled == 1 {
			if t := parseOutboxTime(r.LastPullAt); !t.IsZero() {
				since := t.Add(-2 * time.Second)
				if !hasIncremental || since.Before(minSince) {
					minSince = since
					hasIncremental = true
				}
			}
		}
	}

	journalPKsByTable := make(map[string][]string)
	if hasIncremental {
		var journals []SyncJournalRecord
		if err := cloudDB.WithContext(ctx).
			Where("synced_at >= ?", minSince.UTC()).
			Find(&journals).Error; err == nil {
			for _, j := range journals {
				journalPKsByTable[j.TableNameStr] = append(journalPKsByTable[j.TableNameStr], j.PK)
			}
		}
	}

	changedTables := make(map[string]int)
	for _, tbl := range e.tables {
		if err := ctx.Err(); err != nil {
			return changedTables, err
		}
		changed, err := e.pullTableLocked(ctx, cloudDB, tbl, stateByTable[tbl.TableName], journalPKsByTable[tbl.TableName])
		if err != nil {
			return changedTables, fmt.Errorf("pull %s: %w", tbl.TableName, err)
		}
		if changed > 0 {
			changedTables[tbl.TableName] = changed
		}
	}
	return changedTables, nil
}

func (e *SyncEngine) pullTableLocked(ctx context.Context, cloudDB *gorm.DB, tbl SyncTableSpec, stRow syncStateRow, journalPKs []string) (int, error) {
	pullStart := time.Now().UTC()

	isInitial := stRow.InitialReconciled == 0

	// 读取本地该表当前尚未推送到云端的最新 Outbox 操作，避免拉取覆盖本地更晚的写入/删除
	var outboxRows []outboxItem
	if err := e.localDB.Raw(`SELECT seq, table_name, pk, op, changed_at FROM _sync_outbox WHERE table_name = ? ORDER BY seq ASC`, tbl.TableName).Scan(&outboxRows).Error; err != nil {
		return 0, err
	}
	pendingLocal := make(map[string]outboxItem, len(outboxRows))
	for _, item := range outboxRows {
		pendingLocal[item.PK] = item
	}

	pageSize := 200
	if tbl.HasBlobData {
		pageSize = 20
	}

	var cloudRows []SyncRow
	var tombstones []SyncTombstoneRecord

	if isInitial || (tbl.UpdatedAtCol == "" && tbl.CreatedAtCol == "") {
		for offset := 0; ; offset += pageSize {
			batch, err := tbl.FetchAll(ctx, cloudDB, pageSize, offset)
			if err != nil {
				return 0, err
			}
			cloudRows = append(cloudRows, batch...)
			if len(batch) < pageSize {
				break
			}
		}
		if err := cloudDB.WithContext(ctx).Where("table_name = ?", tbl.TableName).Find(&tombstones).Error; err != nil {
			return 0, err
		}
	} else {
		since := parseOutboxTime(stRow.LastPullAt)
		if !since.IsZero() {
			since = since.Add(-2 * time.Second)
		}
		for offset := 0; ; offset += pageSize {
			batch, err := tbl.FetchSince(ctx, cloudDB, since, pageSize, offset)
			if err != nil {
				return 0, err
			}
			cloudRows = append(cloudRows, batch...)
			if len(batch) < pageSize {
				break
			}
		}
		if err := cloudDB.WithContext(ctx).Where("table_name = ? AND deleted_at >= ?", tbl.TableName, since).Find(&tombstones).Error; err != nil {
			return 0, err
		}
	}

	cloudMap := make(map[string]SyncRow, len(cloudRows))
	for _, r := range cloudRows {
		cloudMap[r.PK] = r
	}
	if !isInitial && len(journalPKs) > 0 {
		var missingFromSince []string
		seenMissing := make(map[string]struct{}, len(journalPKs))
		for _, pk := range journalPKs {
			if _, ok := cloudMap[pk]; ok {
				continue
			}
			if _, dup := seenMissing[pk]; dup {
				continue
			}
			seenMissing[pk] = struct{}{}
			missingFromSince = append(missingFromSince, pk)
		}
		if len(missingFromSince) > 0 {
			extraRows, err := tbl.FetchByPKs(ctx, cloudDB, missingFromSince)
			if err != nil {
				return 0, err
			}
			for _, r := range extraRows {
				cloudMap[r.PK] = r
			}
		}
	}
	tombMap := make(map[string]time.Time, len(tombstones))
	for _, t := range tombstones {
		tombMap[t.PK] = t.DeletedAt.UTC()
	}

	candidatePKSet := make(map[string]struct{}, len(cloudMap)+len(tombMap))
	for pk := range cloudMap {
		candidatePKSet[pk] = struct{}{}
	}
	for pk := range tombMap {
		candidatePKSet[pk] = struct{}{}
	}
	candidatePKs := make([]string, 0, len(candidatePKSet))
	for pk := range candidatePKSet {
		candidatePKs = append(candidatePKs, pk)
	}

	localMap := make(map[string]SyncRow, len(candidatePKs))
	if len(candidatePKs) > 0 {
		existing, err := tbl.FetchByPKs(ctx, e.localDB, candidatePKs)
		if err != nil {
			return 0, err
		}
		for _, r := range existing {
			localMap[r.PK] = r
		}
	}

	var toDeleteLocal []string
	var toUpsertLocal []any
	var pruneOutboxSeqs []int64

	// 1. 先处理云端墓碑删除
	for pk, deletedAt := range tombMap {
		if cloudRow, alsoInCloud := cloudMap[pk]; alsoInCloud && cloudRow.Timestamp.After(deletedAt) {
			continue
		}
		localRow, existsLocal := localMap[pk]
		if pending, hasPending := pendingLocal[pk]; hasPending {
			pendingAt := parseOutboxTime(pending.ChangedAt)
			if pending.opMatches("upsert") && pendingAt.After(deletedAt) {
				continue
			}
			if !pendingAt.After(deletedAt) {
				pruneOutboxSeqs = append(pruneOutboxSeqs, pending.Seq)
			}
		}
		if existsLocal {
			if localRow.Timestamp.After(deletedAt) {
				continue
			}
			toDeleteLocal = append(toDeleteLocal, pk)
		}
	}

	// 2. 再处理云端新增/更新（LWW: cloudRow.Timestamp > localRow.Timestamp）
	for pk, cloudRow := range cloudMap {
		if deletedAt, hasTomb := tombMap[pk]; hasTomb && !cloudRow.Timestamp.After(deletedAt) {
			continue
		}
		pending, hasPending := pendingLocal[pk]
		if hasPending {
			pendingAt := parseOutboxTime(pending.ChangedAt)
			if pending.opMatches("delete") && pendingAt.After(cloudRow.Timestamp) {
				continue
			}
		}
		localRow, existsLocal := localMap[pk]
		if !existsLocal {
			toUpsertLocal = append(toUpsertLocal, cloudRow.Record)
			if hasPending && !parseOutboxTime(pending.ChangedAt).After(cloudRow.Timestamp) {
				pruneOutboxSeqs = append(pruneOutboxSeqs, pending.Seq)
			}
			continue
		}
		if cloudRow.Timestamp.After(localRow.Timestamp) {
			if hasPending && pending.opMatches("upsert") && parseOutboxTime(pending.ChangedAt).After(cloudRow.Timestamp) && localRow.Timestamp.IsZero() {
				continue
			}
			toUpsertLocal = append(toUpsertLocal, cloudRow.Record)
			if hasPending && !parseOutboxTime(pending.ChangedAt).After(cloudRow.Timestamp) {
				pruneOutboxSeqs = append(pruneOutboxSeqs, pending.Seq)
			}
		}
	}

	// 3. 若是首次冷启动双向对齐，扫描本地独有或更新的行写入 _sync_outbox 以便后续补推上云
	var seedOutboxPKs []string
	if isInitial {
		for offset := 0; ; offset += pageSize {
			localBatch, err := tbl.FetchAll(ctx, e.localDB, pageSize, offset)
			if err != nil {
				return 0, err
			}
			for _, lr := range localBatch {
				if _, alreadyPending := pendingLocal[lr.PK]; alreadyPending {
					continue
				}
				if tombAt, hasTomb := tombMap[lr.PK]; hasTomb && !lr.Timestamp.After(tombAt) {
					continue
				}
				cloudRow, existsCloud := cloudMap[lr.PK]
				if !existsCloud || lr.Timestamp.After(cloudRow.Timestamp) {
					seedOutboxPKs = append(seedOutboxPKs, lr.PK)
				}
			}
			if len(localBatch) < pageSize {
				break
			}
		}
	}

	pullAtStr := pullStart.Format(time.RFC3339Nano)
	err := withSuppressedTriggers(e.localDB, func(tx *gorm.DB) error {
		if len(toDeleteLocal) > 0 {
			if err := tbl.DeleteByPKs(ctx, tx, toDeleteLocal); err != nil {
				return err
			}
		}
		if len(toUpsertLocal) > 0 {
			if err := tbl.UpsertBatch(ctx, tx, toUpsertLocal, e.cfg.BatchSize); err != nil {
				return err
			}
		}
		if len(pruneOutboxSeqs) > 0 {
			const maxChunk = 400
			for start := 0; start < len(pruneOutboxSeqs); start += maxChunk {
				end := start + maxChunk
				if end > len(pruneOutboxSeqs) {
					end = len(pruneOutboxSeqs)
				}
				if err := tx.WithContext(ctx).Exec(`DELETE FROM _sync_outbox WHERE seq IN ?`, pruneOutboxSeqs[start:end]).Error; err != nil {
					return err
				}
			}
		}
		if len(seedOutboxPKs) > 0 {
			nowStr := time.Now().UTC().Format(time.RFC3339Nano)
			for _, pk := range seedOutboxPKs {
				if err := tx.WithContext(ctx).Exec(
					`INSERT INTO _sync_outbox (table_name, pk, op, changed_at) VALUES (?, ?, 'upsert', ?)`,
					tbl.TableName, pk, nowStr,
				).Error; err != nil {
					return err
				}
			}
		}
		return tx.WithContext(ctx).Exec(
			`INSERT INTO _sync_state (table_name, last_pull_at, initial_reconciled, updated_at)
			 VALUES (?, ?, 1, ?)
			 ON CONFLICT(table_name) DO UPDATE SET
			   last_pull_at = excluded.last_pull_at,
			   initial_reconciled = 1,
			   updated_at = excluded.updated_at`,
			tbl.TableName, pullAtStr, pullAtStr,
		).Error
	})
	if err != nil {
		return 0, err
	}

	changedCount := len(toDeleteLocal) + len(toUpsertLocal)
	e.stateMu.Lock()
	if st := e.tableStats[tbl.TableName]; st != nil {
		st.LastPullAt = pullAtStr
		st.InitialReconciled = true
		st.PulledRows += int64(len(toUpsertLocal))
		st.DeletedRows += int64(len(toDeleteLocal))
	}
	e.totalPulled += int64(len(toUpsertLocal))
	e.totalDeleted += int64(len(toDeleteLocal))
	e.stateMu.Unlock()

	return changedCount, nil
}

func (o outboxItem) opMatches(target string) bool {
	return strings.EqualFold(strings.TrimSpace(o.Op), target)
}

func (e *SyncEngine) pushPendingLocked(ctx context.Context, cloudDB *gorm.DB) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var items []outboxItem
		if err := e.localDB.WithContext(ctx).
			Raw(`SELECT seq, table_name, pk, op, changed_at FROM _sync_outbox ORDER BY seq ASC LIMIT 5000`).
			Scan(&items).Error; err != nil {
			return fmt.Errorf("read _sync_outbox: %w", err)
		}
		if len(items) == 0 {
			return nil
		}

		// 按注册顺序逐表消费，保持同一主键仅保留最大 seq 的最终语义
		byTableOps := make(map[string]map[string]string)
		maxSeqByTable := make(map[string]int64)
		for _, item := range items {
			ops := byTableOps[item.TableName]
			if ops == nil {
				ops = make(map[string]string)
				byTableOps[item.TableName] = ops
			}
			ops[item.PK] = strings.ToLower(strings.TrimSpace(item.Op))
			if item.Seq > maxSeqByTable[item.TableName] {
				maxSeqByTable[item.TableName] = item.Seq
			}
		}

		for _, tbl := range e.tables {
			ops := byTableOps[tbl.TableName]
			if len(ops) == 0 {
				continue
			}
			maxSeq := maxSeqByTable[tbl.TableName]

			var deletePKs []string
			var upsertPKs []string
			for pk, op := range ops {
				if op == "delete" {
					deletePKs = append(deletePKs, pk)
				} else {
					upsertPKs = append(upsertPKs, pk)
				}
			}

			// 先删后写，避免唯一索引（如 ProxyNodeRecord.URL / gatewaykeys.Group.Name）在删旧建新时碰撞
			if len(deletePKs) > 0 {
				if err := tbl.DeleteByPKs(ctx, cloudDB, deletePKs); err != nil {
					return fmt.Errorf("push delete %s: %w", tbl.TableName, err)
				}
				now := time.Now().UTC()
				tombstones := make([]SyncTombstoneRecord, 0, len(deletePKs))
				for _, pk := range deletePKs {
					tombstones = append(tombstones, SyncTombstoneRecord{
						TableNameStr: tbl.TableName,
						PK:           pk,
						DeletedAt:    now,
					})
				}
				if err := cloudDB.WithContext(ctx).Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "table_name"}, {Name: "pk"}},
					DoUpdates: clause.AssignmentColumns([]string{"deleted_at"}),
				}).CreateInBatches(&tombstones, e.cfg.BatchSize).Error; err != nil {
					return fmt.Errorf("push tombstones %s: %w", tbl.TableName, err)
				}
			}

			pushedUpserts := 0
			if len(upsertPKs) > 0 {
				rows, err := tbl.FetchByPKs(ctx, e.localDB, upsertPKs)
				if err != nil {
					return fmt.Errorf("fetch local %s for push: %w", tbl.TableName, err)
				}
				if len(rows) > 0 {
					records := make([]any, 0, len(rows))
					for _, r := range rows {
						records = append(records, r.Record)
					}
					if err := tbl.UpsertBatch(ctx, cloudDB, records, e.cfg.BatchSize); err != nil {
						return fmt.Errorf("push upsert %s: %w", tbl.TableName, err)
					}
					pushedUpserts = len(records)

					nowSync := time.Now().UTC()
					journals := make([]SyncJournalRecord, 0, len(rows))
					for _, r := range rows {
						journals = append(journals, SyncJournalRecord{
							TableNameStr: tbl.TableName,
							PK:           r.PK,
							SyncedAt:     nowSync,
						})
					}
					if err := cloudDB.WithContext(ctx).Clauses(clause.OnConflict{
						Columns:   []clause.Column{{Name: "table_name"}, {Name: "pk"}},
						DoUpdates: clause.AssignmentColumns([]string{"synced_at"}),
					}).CreateInBatches(&journals, e.cfg.BatchSize).Error; err != nil {
						return fmt.Errorf("push journal %s: %w", tbl.TableName, err)
					}
				}
				// 清除云端对应主键的历史墓碑
				for _, chunk := range chunkStrings(upsertPKs, 400) {
					if err := cloudDB.WithContext(ctx).
						Where("table_name = ? AND pk IN ?", tbl.TableName, chunk).
						Delete(&SyncTombstoneRecord{}).Error; err != nil {
						return fmt.Errorf("clear tombstones %s: %w", tbl.TableName, err)
					}
				}
			}

			if err := e.localDB.WithContext(ctx).Exec(
				`DELETE FROM _sync_outbox WHERE table_name = ? AND seq <= ?`,
				tbl.TableName, maxSeq,
			).Error; err != nil {
				return fmt.Errorf("ack _sync_outbox %s: %w", tbl.TableName, err)
			}

			e.stateMu.Lock()
			if st := e.tableStats[tbl.TableName]; st != nil {
				st.PushedRows += int64(pushedUpserts)
				st.DeletedRows += int64(len(deletePKs))
			}
			e.totalPushed += int64(pushedUpserts)
			e.totalDeleted += int64(len(deletePKs))
			e.lastPushAt = time.Now().UTC()
			e.stateMu.Unlock()
		}

		// 若存在未注册表的孤立 outbox 记录则清理，防止死循环
		for tblName, maxSeq := range maxSeqByTable {
			if _, ok := e.tableMap[tblName]; !ok {
				_ = e.localDB.WithContext(ctx).Exec(`DELETE FROM _sync_outbox WHERE table_name = ? AND seq <= ?`, tblName, maxSeq).Error
			}
		}

		if len(items) < 5000 {
			return nil
		}
	}
}

func (e *SyncEngine) countOutboxPending() int64 {
	if e == nil || e.localDB == nil {
		return 0
	}
	var count int64
	_ = e.localDB.Raw(`SELECT COUNT(*) FROM _sync_outbox`).Scan(&count).Error
	return count
}

func (e *SyncEngine) countOutboxPendingByTable() map[string]int64 {
	result := make(map[string]int64)
	if e == nil || e.localDB == nil {
		return result
	}
	type row struct {
		TableName string `gorm:"column:table_name"`
		Cnt       int64  `gorm:"column:cnt"`
	}
	var rows []row
	_ = e.localDB.Raw(`SELECT table_name, COUNT(*) AS cnt FROM _sync_outbox GROUP BY table_name`).Scan(&rows).Error
	for _, r := range rows {
		result[r.TableName] = r.Cnt
	}
	return result
}

func (e *SyncEngine) setTransientState(state string) {
	e.stateMu.Lock()
	e.state = state
	e.stateMu.Unlock()
}

func (e *SyncEngine) recordSuccess(elapsed time.Duration) {
	e.stateMu.Lock()
	e.state = "idle"
	e.cloudConnected = true
	e.lastSyncLatencyMs = elapsed.Milliseconds()
	e.consecutiveFailures = 0
	e.lastError = ""
	e.nextRetryAt = time.Time{}
	e.stateMu.Unlock()
	e.notifyStatusChange()
}

func (e *SyncEngine) recordFailure(err error, elapsed time.Duration) {
	e.stateMu.Lock()
	e.state = "degraded"
	e.lastSyncLatencyMs = elapsed.Milliseconds()
	e.consecutiveFailures++
	e.lastError = err.Error()
	backoff := syncBackoffDuration(e.consecutiveFailures)
	e.nextRetryAt = time.Now().UTC().Add(backoff)
	e.stateMu.Unlock()
	e.notifyStatusChange()
}

func syncBackoffDuration(failures int) time.Duration {
	switch {
	case failures <= 1:
		return 2 * time.Second
	case failures == 2:
		return 5 * time.Second
	case failures == 3:
		return 15 * time.Second
	case failures == 4:
		return 30 * time.Second
	default:
		return 60 * time.Second
	}
}

func (e *SyncEngine) notifyStatusChange() {
	if e.cfg.OnStatusChange != nil {
		e.cfg.OnStatusChange(e.Status())
	}
}

// Status 返回脱敏后的实时同步引擎状态快照。
func (e *SyncEngine) Status() SyncStatus {
	if e == nil {
		return SyncStatus{
			Enabled: false,
			Mode:    "local_only",
			State:   "disabled",
		}
	}

	pendingByTable := e.countOutboxPendingByTable()
	var totalPending int64
	for _, cnt := range pendingByTable {
		totalPending += cnt
	}

	e.stateMu.RLock()
	defer e.stateMu.RUnlock()

	tables := make([]TableSyncStatus, 0, len(e.tables))
	for _, tbl := range e.tables {
		if st := e.tableStats[tbl.TableName]; st != nil {
			copySt := *st
			copySt.PendingOutbox = pendingByTable[tbl.TableName]
			tables = append(tables, copySt)
		}
	}

	out := SyncStatus{
		Enabled:             true,
		Mode:                "materialized",
		State:               e.state,
		LocalPath:           e.cfg.LocalPath,
		CloudTarget:         MaskDSN(e.cloudDSN),
		CloudConnected:      e.cloudConnected,
		InitialHydrated:     e.initialHydrated,
		OutboxPending:       totalPending,
		LastSyncLatencyMs:   e.lastSyncLatencyMs,
		TotalPushed:         e.totalPushed,
		TotalPulled:         e.totalPulled,
		TotalDeleted:        e.totalDeleted,
		ConsecutiveFailures: e.consecutiveFailures,
		LastError:           e.lastError,
		PushInterval:        e.cfg.PushInterval.String(),
		PullInterval:        e.cfg.PullInterval.String(),
		Tables:              tables,
	}
	if !e.lastPushAt.IsZero() {
		out.LastPushAt = e.lastPushAt.Format(time.RFC3339)
	}
	if !e.lastPullAt.IsZero() {
		out.LastPullAt = e.lastPullAt.Format(time.RFC3339)
	}
	if !e.nextRetryAt.IsZero() {
		out.NextRetryAt = e.nextRetryAt.Format(time.RFC3339)
	}
	return out
}

// LocalDB 返回底层本地物化 SQLite 实例（供调用方/测试检查）。
func (e *SyncEngine) LocalDB() *gorm.DB {
	return e.localDB
}

var _ = sql.ErrNoRows
