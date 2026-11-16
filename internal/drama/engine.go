package drama

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"ai-web-free/internal/db"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// NodeExecutor 抽象节点实际执行能力（便于生产注入与单元测试 Mock）。
type NodeExecutor interface {
	GeneratePortrait(ctx context.Context, proj *DramaProject, char CharacterSpec) (asset AssetSpec, modelUsed string, err error)
	GenerateSceneAnchor(ctx context.Context, proj *DramaProject, scene SceneSpec) (asset AssetSpec, modelUsed string, err error)
	GenerateKeyframe(ctx context.Context, proj *DramaProject, shot ShotSpec, refImageURLs []string) (asset AssetSpec, modelUsed string, degraded bool, statusNote string, err error)
	GenerateTTS(ctx context.Context, proj *DramaProject, shot ShotSpec, speaker *CharacterSpec) (asset AssetSpec, durationSec float64, err error)
}

// BreakdownExecutor 可选的多阶段拆剧本执行器（P2）。
type BreakdownExecutor interface {
	AnalyzeScript(ctx context.Context, proj *DramaProject) ([]BeatSpec, error)
	SelectHighlights(ctx context.Context, proj *DramaProject) ([]string, error)
	BuildCharacters(ctx context.Context, proj *DramaProject) ([]CharacterSpec, error)
	BuildScenes(ctx context.Context, proj *DramaProject) ([]SceneSpec, error)
	BuildShots(ctx context.Context, proj *DramaProject) ([]ShotSpec, error)
}

// VideoExecutor 可选的视频生成与 task_id 续轮询执行器（P2）。
type VideoExecutor interface {
	GenerateOrPollVideo(
		ctx context.Context,
		proj *DramaProject,
		shot ShotSpec,
		existingTaskID string,
		onTaskID func(taskID string),
	) (asset AssetSpec, modelUsed string, degraded bool, statusNote string, err error)
}

// RenderExecutor 可选的外部 FFmpeg MP4 渲染服务执行器（P3）。
type RenderExecutor interface {
	RenderMP4(ctx context.Context, proj *DramaProject) (asset AssetSpec, degraded bool, statusNote string, err error)
}

type RunOptions struct {
	Targets          []string `json:"targets,omitempty"`
	Force            []string `json:"force,omitempty"`
	Until            string   `json:"until,omitempty"`
	ImageConcurrency int      `json:"image_concurrency,omitempty"`
	TTSConcurrency   int      `json:"tts_concurrency,omitempty"`
}

type RunLogEntry struct {
	Timestamp string `json:"timestamp"`
	Level     string `json:"level"` // info | success | warn | error
	NodeKey   string `json:"node_key,omitempty"`
	Message   string `json:"message"`
}

type RunSummary struct {
	Total      int   `json:"total"`
	Ready      int   `json:"ready"`
	Degraded   int   `json:"degraded"`
	Skipped    int   `json:"skipped"`
	Running    int   `json:"running"`
	Failed     int   `json:"failed"`
	Pending    int   `json:"pending"`
	DurationMs int64 `json:"duration_ms"`
}

type RunEvent struct {
	Type      string       `json:"type"` // node.update | run.update | run.log
	RunID     string       `json:"run_id"`
	ProjectID string       `json:"project_id"`
	NodeKey   string       `json:"node_key,omitempty"`
	NodeState *NodeState   `json:"node_state,omitempty"`
	Status    string       `json:"status,omitempty"`
	Message   string       `json:"message,omitempty"`
	Log       *RunLogEntry `json:"log,omitempty"`
	Summary   *RunSummary  `json:"summary,omitempty"`
	Revision  int64        `json:"revision,omitempty"`
	Timestamp string       `json:"timestamp"`
}

type activeRunState struct {
	runID     string
	projectID string
	startedAt time.Time
	cancel    context.CancelFunc
	subsMu    sync.RWMutex
	subs      map[chan RunEvent]struct{}
	logsMu    sync.Mutex
	logs      []RunLogEntry
	summary   RunSummary
}

func (r *activeRunState) broadcast(ev RunEvent) {
	ev.Timestamp = time.Now().UTC().Format(time.RFC3339)
	r.subsMu.RLock()
	defer r.subsMu.RUnlock()
	for ch := range r.subs {
		select {
		case ch <- ev:
		default:
			// 慢消费者丢弃旧事件，避免阻塞调度器
		}
	}
}

type Engine struct {
	db                   *gorm.DB
	executor             NodeExecutor
	imageConcurrencyFunc func() int
	maxRetries           int

	mu         sync.Mutex
	activeRuns map[string]*activeRunState // projectID -> activeRunState
	runByID    map[string]*activeRunState // runID -> activeRunState
	projLocks  map[string]*sync.Mutex
}

func NewEngine(database *gorm.DB, executor NodeExecutor, imageConcurrencyFunc func() int) *Engine {
	return &Engine{
		db:                   database,
		executor:             executor,
		imageConcurrencyFunc: imageConcurrencyFunc,
		maxRetries:           2,
		activeRuns:           make(map[string]*activeRunState),
		runByID:              make(map[string]*activeRunState),
		projLocks:            make(map[string]*sync.Mutex),
	}
}

func (e *Engine) getProjectLock(projectID string) *sync.Mutex {
	e.mu.Lock()
	defer e.mu.Unlock()
	lk, ok := e.projLocks[projectID]
	if !ok {
		lk = &sync.Mutex{}
		e.projLocks[projectID] = lk
	}
	return lk
}

func (e *Engine) resolveImageConcurrency(override int) int {
	if override > 0 {
		return override
	}
	if e.imageConcurrencyFunc != nil {
		n := e.imageConcurrencyFunc()
		if n > 0 {
			if n > 8 {
				return 8
			}
			return n
		}
	}
	return 2
}

// LoadOrMigrateProject 读取工程 Schema；若仅有旧 graph_json 则自动无损迁移并回写数据库。
func (e *Engine) LoadOrMigrateProject(projectID string) (*DramaProject, *LayoutData, *db.DramaProjectRecord, error) {
	if e.db == nil {
		return nil, nil, nil, errors.New("database not initialized")
	}
	var rec db.DramaProjectRecord
	if err := e.db.First(&rec, "id = ?", projectID).Error; err != nil {
		return nil, nil, nil, err
	}

	var layout LayoutData
	if strings.TrimSpace(rec.LayoutJSON) != "" {
		_ = json.Unmarshal([]byte(rec.LayoutJSON), &layout)
	}
	if layout.Positions == nil {
		layout.Positions = make(map[string]NodePosition)
	}

	if strings.TrimSpace(rec.ProjectJSON) != "" {
		var proj DramaProject
		if err := json.Unmarshal([]byte(rec.ProjectJSON), &proj); err == nil {
			proj.ID = rec.ID
			proj.EnsureMaps()
			if rec.Revision > proj.Revision {
				proj.Revision = rec.Revision
			}
			return &proj, &layout, &rec, nil
		}
	}

	// 从旧 graph_json 自动迁移
	proj, migratedLayout, err := MigrateGraphJSON(
		rec.ID, rec.Title, rec.Genre, rec.AspectRatio, rec.StylePreset, rec.RoutingPolicy, rec.Outline, rec.GraphJSON,
	)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(layout.Positions) == 0 && migratedLayout != nil {
		layout = *migratedLayout
	}
	proj.Revision = maxInt64(rec.Revision, 1)
	if err := e.saveProjectLocked(&rec, proj, &layout); err != nil {
		return nil, nil, nil, err
	}
	return proj, &layout, &rec, nil
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func (e *Engine) saveProjectLocked(rec *db.DramaProjectRecord, proj *DramaProject, layout *LayoutData) error {
	proj.EnsureMaps()
	projBytes, err := json.Marshal(proj)
	if err != nil {
		return err
	}
	rec.Title = proj.Script.Title
	rec.Genre = proj.Script.Genre
	rec.AspectRatio = proj.Script.AspectRatio
	rec.StylePreset = proj.Script.Style
	rec.RoutingPolicy = proj.Script.RoutingPolicy
	rec.Outline = proj.Script.Outline
	rec.SchemaVersion = proj.SchemaVersion
	rec.Revision = proj.Revision
	rec.ProjectJSON = string(projBytes)
	rec.ShotCount = len(proj.Shots)
	if layout != nil {
		if lb, err := json.Marshal(layout); err == nil {
			rec.LayoutJSON = string(lb)
		}
	}
	return e.db.Save(rec).Error
}

// SaveProjectState 供外部（如 PATCH / PUT）在持锁下更新工程。
func (e *Engine) SaveProjectState(projectID string, proj *DramaProject, layout *LayoutData) error {
	lk := e.getProjectLock(projectID)
	lk.Lock()
	defer lk.Unlock()

	var rec db.DramaProjectRecord
	if err := e.db.First(&rec, "id = ?", projectID).Error; err != nil {
		return err
	}
	return e.saveProjectLocked(&rec, proj, layout)
}

// LockNode 锁定或解锁指定节点。
func (e *Engine) SetNodeLocked(projectID, nodeKey string, locked bool) (*DramaProject, error) {
	lk := e.getProjectLock(projectID)
	lk.Lock()
	defer lk.Unlock()

	proj, layout, rec, err := e.LoadOrMigrateProject(projectID)
	if err != nil {
		return nil, err
	}
	st, ok := proj.Nodes[nodeKey]
	if !ok || st == nil {
		st = &NodeState{Status: StatusPending}
		proj.Nodes[nodeKey] = st
	}
	st.Locked = locked
	st.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if !locked {
		SyncNodeHashesAndStates(proj, false)
	}
	proj.Revision++
	if err := e.saveProjectLocked(rec, proj, layout); err != nil {
		return nil, err
	}
	return proj, nil
}

// GetActiveRunID 返回当前工程正在运行的 Run ID（若有）。
func (e *Engine) GetActiveRunID(projectID string) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if r, ok := e.activeRuns[projectID]; ok {
		return r.runID
	}
	return ""
}

// StartRun 启动后台 DAG 执行；同一工程已有活跃 Run 时幂等返回已有记录。
func (e *Engine) StartRun(ctx context.Context, projectID string, opts RunOptions) (*db.DramaRunRecord, bool, error) {
	if e.db == nil {
		return nil, false, errors.New("database not initialized")
	}

	e.mu.Lock()
	if existing, ok := e.activeRuns[projectID]; ok {
		e.mu.Unlock()
		var rec db.DramaRunRecord
		if err := e.db.First(&rec, "id = ?", existing.runID).Error; err == nil {
			return &rec, true, nil
		}
		return &db.DramaRunRecord{ID: existing.runID, ProjectID: projectID, Status: "running"}, true, nil
	}

	// 验证工程存在
	if _, _, _, err := e.LoadOrMigrateProject(projectID); err != nil {
		e.mu.Unlock()
		return nil, false, err
	}

	runID := "run_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	targetsB, _ := json.Marshal(opts.Targets)
	forceB, _ := json.Marshal(opts.Force)
	rec := db.DramaRunRecord{
		ID:          runID,
		ProjectID:   projectID,
		Status:      "running",
		TargetsJSON: string(targetsB),
		ForceJSON:   string(forceB),
		Until:       opts.Until,
	}
	if err := e.db.Create(&rec).Error; err != nil {
		e.mu.Unlock()
		return nil, false, err
	}

	runCtx, cancel := context.WithCancel(context.Background())
	state := &activeRunState{
		runID:     runID,
		projectID: projectID,
		startedAt: time.Now(),
		cancel:    cancel,
		subs:      make(map[chan RunEvent]struct{}),
	}
	e.activeRuns[projectID] = state
	e.runByID[runID] = state
	e.mu.Unlock()

	go e.executeRun(runCtx, state, opts)
	return &rec, false, nil
}

// GetRunSnapshot 返回内存中活跃 Run 的已产生日志与汇总快照。
func (e *Engine) GetRunSnapshot(runID string) ([]RunLogEntry, RunSummary, bool) {
	e.mu.Lock()
	state, ok := e.runByID[runID]
	e.mu.Unlock()
	if !ok {
		return nil, RunSummary{}, false
	}
	state.logsMu.Lock()
	logsCopy := append([]RunLogEntry(nil), state.logs...)
	sumCopy := state.summary
	state.logsMu.Unlock()
	return logsCopy, sumCopy, true
}

func (e *Engine) appendRunLog(state *activeRunState, level, nodeKey, msg string) {
	entry := RunLogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Level:     level,
		NodeKey:   nodeKey,
		Message:   msg,
	}
	state.logsMu.Lock()
	state.logs = append(state.logs, entry)
	if len(state.logs) > 500 {
		state.logs = state.logs[len(state.logs)-500:]
	}
	logsBytes, _ := json.Marshal(state.logs)
	sumCopy := state.summary
	sumCopy.DurationMs = time.Since(state.startedAt).Milliseconds()
	sumBytes, _ := json.Marshal(sumCopy)
	state.logsMu.Unlock()

	if e.db != nil {
		_ = e.db.Model(&db.DramaRunRecord{}).
			Where("id = ?", state.runID).
			Updates(map[string]interface{}{
				"logs_json":    string(logsBytes),
				"summary_json": string(sumBytes),
				"updated_at":   time.Now().UTC(),
			}).Error
	}

	state.broadcast(RunEvent{
		Type:      "run.log",
		RunID:     state.runID,
		ProjectID: state.projectID,
		NodeKey:   nodeKey,
		Log:       &entry,
		Summary:   &sumCopy,
	})
}

// Subscribe 订阅指定 Run 的实时 SSE 事件。
func (e *Engine) Subscribe(runID string) (<-chan RunEvent, func(), bool) {
	e.mu.Lock()
	state, ok := e.runByID[runID]
	e.mu.Unlock()
	if !ok {
		return nil, func() {}, false
	}
	ch := make(chan RunEvent, 64)
	state.subsMu.Lock()
	state.subs[ch] = struct{}{}
	state.subsMu.Unlock()

	unsub := func() {
		state.subsMu.Lock()
		if _, exists := state.subs[ch]; exists {
			delete(state.subs, ch)
			close(ch)
		}
		state.subsMu.Unlock()
	}
	return ch, unsub, true
}

// ResumeInterruptedRuns 在服务重启后恢复中断的 running Run。
func (e *Engine) ResumeInterruptedRuns(ctx context.Context) {
	if e.db == nil {
		return
	}
	var runs []db.DramaRunRecord
	if err := e.db.Where("status = ?", "running").Find(&runs).Error; err != nil || len(runs) == 0 {
		return
	}
	for _, r := range runs {
		lk := e.getProjectLock(r.ProjectID)
		lk.Lock()
		proj, layout, rec, err := e.LoadOrMigrateProject(r.ProjectID)
		if err == nil {
			changed := false
			for _, st := range proj.Nodes {
				if st != nil && st.Status == StatusRunning {
					st.Status = StatusPending
					st.StatusNote = "服务重启，正在续跑"
					changed = true
				}
			}
			if changed {
				_ = e.saveProjectLocked(rec, proj, layout)
			}
		}
		lk.Unlock()

		var opts RunOptions
		_ = json.Unmarshal([]byte(r.TargetsJSON), &opts.Targets)
		_ = json.Unmarshal([]byte(r.ForceJSON), &opts.Force)
		opts.Until = r.Until

		e.mu.Lock()
		if _, exists := e.activeRuns[r.ProjectID]; !exists {
			runCtx, cancel := context.WithCancel(context.Background())
			state := &activeRunState{
				runID:     r.ID,
				projectID: r.ProjectID,
				startedAt: r.CreatedAt,
				cancel:    cancel,
				subs:      make(map[chan RunEvent]struct{}),
			}
			if strings.TrimSpace(r.LogsJSON) != "" {
				_ = json.Unmarshal([]byte(r.LogsJSON), &state.logs)
			}
			e.activeRuns[r.ProjectID] = state
			e.runByID[r.ID] = state
			e.mu.Unlock()
			log.Printf("[DramaEngine] Resuming interrupted run %s for project %s", r.ID, r.ProjectID)
			go e.executeRun(runCtx, state, opts)
		} else {
			e.mu.Unlock()
		}
	}
}

func matchesPattern(patterns []string, key string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p == "" || p == "*" {
			return true
		}
		if strings.HasSuffix(p, "*") {
			prefix := strings.TrimSuffix(p, "*")
			if strings.HasPrefix(key, prefix) {
				return true
			}
		} else if p == key {
			return true
		}
	}
	return false
}

var breakdownStageKeys = []string{
	"script.analyze",
	"script.highlights",
	"bible.characters",
	"bible.scenes",
	"storyboard.shots",
}

func includesBreakdownTargets(targets []string) bool {
	if len(targets) == 0 {
		return false
	}
	for _, k := range breakdownStageKeys {
		if matchesPattern(targets, k) {
			return true
		}
	}
	return false
}

func stageOrder(key string) int {
	switch {
	case key == "script.analyze":
		return 1
	case key == "script.highlights":
		return 2
	case key == "bible.characters", key == "bible.scenes":
		return 3
	case key == "storyboard.shots":
		return 4
	case strings.HasPrefix(key, "image.portrait:"), strings.HasPrefix(key, "image.scene:"):
		return 5
	case strings.HasPrefix(key, "image.keyframe:"), strings.HasPrefix(key, "tts.line:"):
		return 6
	case strings.HasPrefix(key, "video.clip:"):
		return 7
	case key == "timeline.assemble":
		return 8
	case key == "render.mp4":
		return 9
	default:
		return 99
	}
}

func nodeDependencies(proj *DramaProject, key string, includeBreakdown bool) []string {
	switch {
	case key == "script.analyze":
		return nil
	case key == "script.highlights":
		return []string{"script.analyze"}
	case key == "bible.characters", key == "bible.scenes":
		return []string{"script.highlights"}
	case key == "storyboard.shots":
		return []string{"bible.characters", "bible.scenes"}
	case strings.HasPrefix(key, "image.portrait:"):
		if includeBreakdown {
			return []string{"bible.characters"}
		}
		return nil
	case strings.HasPrefix(key, "image.scene:"):
		if includeBreakdown {
			return []string{"bible.scenes"}
		}
		return nil
	case strings.HasPrefix(key, "tts.line:"):
		if includeBreakdown {
			return []string{"storyboard.shots"}
		}
		return nil
	case strings.HasPrefix(key, "image.keyframe:"):
		shid := strings.TrimPrefix(key, "image.keyframe:")
		sh := findShot(proj, shid)
		if sh == nil {
			return nil
		}
		var deps []string
		if includeBreakdown {
			deps = append(deps, "storyboard.shots")
		}
		if sh.SpeakerID != "" {
			if ch := findChar(proj, sh.SpeakerID); ch != nil && strings.TrimSpace(ch.Role) != "旁白" {
				deps = append(deps, PortraitNodeKey(ch.ID))
			}
		}
		if sh.SceneID != "" {
			if sc := findScene(proj, sh.SceneID); sc != nil {
				deps = append(deps, SceneNodeKey(sc.ID))
			}
		}
		if sh.Continuous {
			if prev := findPrevShot(proj, *sh); prev != nil {
				deps = append(deps, KeyframeNodeKey(prev.ID))
			}
		}
		return deps
	case strings.HasPrefix(key, "video.clip:"):
		shid := strings.TrimPrefix(key, "video.clip:")
		sh := findShot(proj, shid)
		if sh == nil {
			return nil
		}
		deps := []string{KeyframeNodeKey(shid)}
		if strings.TrimSpace(sh.Dialogue) != "" || strings.TrimSpace(sh.Action) != "" {
			deps = append(deps, TTSNodeKey(shid))
		}
		return deps
	case key == "timeline.assemble":
		var deps []string
		for _, sh := range proj.Shots {
			deps = append(deps, VideoNodeKey(sh.ID))
			if strings.TrimSpace(sh.Dialogue) != "" || strings.TrimSpace(sh.Action) != "" {
				deps = append(deps, TTSNodeKey(sh.ID))
			}
		}
		return deps
	case key == "render.mp4":
		return []string{TimelineNodeKey()}
	default:
		return nil
	}
}

func (e *Engine) executeRun(ctx context.Context, state *activeRunState, opts RunOptions) {
	defer func() {
		e.mu.Lock()
		delete(e.activeRuns, state.projectID)
		delete(e.runByID, state.runID)
		e.mu.Unlock()
	}()

	lk := e.getProjectLock(state.projectID)
	lk.Lock()
	proj, layout, rec, err := e.LoadOrMigrateProject(state.projectID)
	if err != nil {
		lk.Unlock()
		e.finishRun(state, "failed", err.Error(), 0)
		return
	}
	SyncNodeHashesAndStates(proj, false)
	// 上次遗留的 failed 节点在本次 Run 启动时重置为 pending，以便补跑
	for _, st := range proj.Nodes {
		if st != nil && st.Status == StatusFailed && !st.Locked {
			st.Status = StatusPending
			st.Error = ""
		}
	}
	_ = e.saveProjectLocked(rec, proj, layout)
	lk.Unlock()

	hasBreakdown := includesBreakdownTargets(opts.Targets)
	maxStage := 99
	if strings.TrimSpace(opts.Until) != "" {
		maxStage = stageOrder(strings.TrimSpace(opts.Until))
	}

	computeWorkKeys := func(p *DramaProject) []string {
		var candidates []string
		if hasBreakdown {
			candidates = append(candidates, breakdownStageKeys...)
		}
		candidates = append(candidates, ExpectedNodeKeys(p)...)
		if matchesPattern(opts.Targets, RenderMP4NodeKey()) && len(opts.Targets) > 0 && opts.Targets[0] != "*" {
			candidates = append(candidates, RenderMP4NodeKey())
		}
		selected := make(map[string]bool)
		for _, k := range candidates {
			if stageOrder(k) > maxStage {
				continue
			}
			if matchesPattern(opts.Targets, k) {
				selected[k] = true
			}
		}
		// 如果只指定了 storyboard.shots 等下游拆本节点，递归补全拆本上游
		changed := true
		for changed {
			changed = false
			for k := range selected {
				for _, dep := range nodeDependencies(p, k, hasBreakdown) {
					if stageOrder(dep) <= maxStage && !selected[dep] {
						selected[dep] = true
						changed = true
					}
				}
			}
		}
		var out []string
		for _, k := range candidates {
			if selected[k] {
				out = append(out, k)
			}
		}
		return out
	}

	workKeys := computeWorkKeys(proj)

	imgCapOverride := opts.ImageConcurrency
	if imgCapOverride <= 0 && proj.Script.ImageConcurrency > 0 {
		imgCapOverride = proj.Script.ImageConcurrency
	}
	imgCap := e.resolveImageConcurrency(imgCapOverride)
	ttsCap := opts.TTSConcurrency
	if ttsCap <= 0 {
		ttsCap = 6
	}
	imageSem := make(chan struct{}, imgCap)
	ttsSem := make(chan struct{}, ttsCap)

	state.broadcast(RunEvent{
		Type:      "run.update",
		RunID:     state.runID,
		ProjectID: state.projectID,
		Status:    "running",
		Message:   fmt.Sprintf("后台 DAG 引擎已启动（生图并发上限 %d，共 %d 个节点）", imgCap, len(workKeys)),
	})
	e.appendRunLog(state, "info", "", fmt.Sprintf("启动后台 DAG 调度（生图并发上限 %d，候选节点 %d 个）", imgCap, len(workKeys)))

	// 循环调度：直到 workKeys 中所有节点都进入终态（ready / degraded / failed）
	var schedMu sync.Mutex
	inFlight := make(map[string]bool)
	loggedSkipped := make(map[string]bool)
	doneCond := sync.NewCond(&schedMu)

	isTerminal := func(st *NodeState) bool {
		if st == nil {
			return false
		}
		return st.Status == StatusReady || st.Status == StatusDegraded || st.Status == StatusFailed
	}

	shouldSkip := func(proj *DramaProject, key string, st *NodeState) bool {
		if st == nil {
			return false
		}
		if st.Locked && st.Output != "" {
			return true
		}
		if len(opts.Force) > 0 && matchesPattern(opts.Force, key) {
			return false
		}
		curHash := ComputeNodeInputHash(proj, key)
		return (st.Status == StatusReady || st.Status == StatusDegraded) && st.InputHash == curHash
	}

	for {
		if ctx.Err() != nil {
			e.finishRun(state, "cancelled", "任务已取消", proj.Revision)
			return
		}

		lk.Lock()
		curProj, curLayout, curRec, loadErr := e.LoadOrMigrateProject(state.projectID)
		if loadErr != nil {
			lk.Unlock()
			e.finishRun(state, "failed", loadErr.Error(), 0)
			return
		}

		schedMu.Lock()
		workKeys = computeWorkKeys(curProj)
		allDone := true
		var readyToLaunch []string
		failedCount := 0
		successCount := 0
		var sum RunSummary
		sum.Total = len(workKeys)
		var newSkipLogs []struct {
			key    string
			reason string
		}

		for _, key := range workKeys {
			st := curProj.Nodes[key]
			if st == nil {
				st = &NodeState{Status: StatusPending}
				curProj.Nodes[key] = st
			}
			if shouldSkip(curProj, key, st) {
				successCount++
				if st.Status == StatusDegraded {
					sum.Degraded++
				} else {
					sum.Ready++
				}
				sum.Skipped++
				if !loggedSkipped[key] {
					loggedSkipped[key] = true
					reason := "输入指纹未变，复用已有产物"
					if st.Locked {
						reason = "节点已锁定，跳过覆盖"
					}
					newSkipLogs = append(newSkipLogs, struct {
						key    string
						reason string
					}{key: key, reason: reason})
				}
				continue
			}
			if st.Status == StatusFailed {
				failedCount++
				sum.Failed++
				continue
			}
			if inFlight[key] {
				allDone = false
				sum.Running++
				continue
			}

			allDone = false
			sum.Pending++
			// 检查依赖
			deps := nodeDependencies(curProj, key, hasBreakdown)
			depsReady := true
			var failedDep string
			for _, dep := range deps {
				depSt := curProj.Nodes[dep]
				if depSt == nil || (!isTerminal(depSt) && !shouldSkip(curProj, dep, depSt)) {
					depsReady = false
					break
				}
				if depSt.Status == StatusFailed {
					failedDep = dep
					break
				}
			}
			if failedDep != "" {
				st.Status = StatusFailed
				st.Error = fmt.Sprintf("上游节点 %s 失败", failedDep)
				st.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
				curProj.Revision++
				_ = e.saveProjectLocked(curRec, curProj, curLayout)
				state.broadcast(RunEvent{
					Type:      "node.update",
					RunID:     state.runID,
					ProjectID: state.projectID,
					NodeKey:   key,
					NodeState: st,
					Revision:  curProj.Revision,
				})
				failedCount++
				continue
			}
			if depsReady {
				inFlight[key] = true
				st.Status = StatusRunning
				st.Error = ""
				st.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
				readyToLaunch = append(readyToLaunch, key)
			}
		}
		if len(readyToLaunch) > 0 {
			curProj.Revision++
			_ = e.saveProjectLocked(curRec, curProj, curLayout)
		}
		schedMu.Unlock()
		lk.Unlock()

		state.logsMu.Lock()
		state.summary = sum
		state.logsMu.Unlock()
		for _, sk := range newSkipLogs {
			e.appendRunLog(state, "info", sk.key, fmt.Sprintf("跳过 %s（%s）", sk.key, sk.reason))
		}

		for _, k := range readyToLaunch {
			nodeKey := k
			e.appendRunLog(state, "info", nodeKey, fmt.Sprintf("开始执行 %s", nodeKey))
			state.broadcast(RunEvent{
				Type:      "node.update",
				RunID:     state.runID,
				ProjectID: state.projectID,
				NodeKey:   nodeKey,
				NodeState: &NodeState{Status: StatusRunning},
			})

			go func() {
				sem := imageSem
				if strings.HasPrefix(nodeKey, "tts.line:") {
					sem = ttsSem
				}
				sem <- struct{}{}
				defer func() { <-sem }()

				e.runSingleNode(ctx, state, nodeKey)

				schedMu.Lock()
				delete(inFlight, nodeKey)
				doneCond.Broadcast()
				schedMu.Unlock()
			}()
		}

		if allDone {
			finalStatus := "completed"
			msg := fmt.Sprintf("流水线执行完成（成功 %d 节点）", successCount)
			if failedCount > 0 && successCount > 0 {
				finalStatus = "partial"
				msg = fmt.Sprintf("部分完成：成功 %d 个，失败 %d 个（再次触发将仅补跑失败节点）", successCount, failedCount)
			} else if failedCount > 0 && successCount == 0 {
				finalStatus = "failed"
				msg = fmt.Sprintf("执行失败（%d 个节点失败）", failedCount)
			}
			e.finishRun(state, finalStatus, msg, curProj.Revision)
			return
		}

		schedMu.Lock()
		if len(readyToLaunch) == 0 && len(inFlight) == 0 {
			schedMu.Unlock()
			// 防御性退出，避免死循环
			e.finishRun(state, "partial", "无可调度节点，结束运行", curProj.Revision)
			return
		}
		for len(inFlight) > 0 {
			doneCond.Wait()
			break
		}
		schedMu.Unlock()
	}
}

func (e *Engine) runSingleNode(ctx context.Context, state *activeRunState, nodeKey string) {
	nodeStart := time.Now()
	lk := e.getProjectLock(state.projectID)
	lk.Lock()
	projSnap, _, _, err := e.LoadOrMigrateProject(state.projectID)
	lk.Unlock()
	if err != nil {
		e.commitNodeFailure(state, nodeKey, 1, err.Error())
		return
	}
	execHash := ComputeNodeInputHash(projSnap, nodeKey)

	var (
		asset      AssetSpec
		modelUsed  string
		degraded   bool
		statusNote string
		durSec     float64
		lastErr    error
		attempts   int

		beatsOut     []BeatSpec
		highlights   []string
		charsOut     []CharacterSpec
		scenesOut    []SceneSpec
		shotsOut     []ShotSpec
		timelineOut  *TimelineSpec
	)

	for attempt := 0; attempt <= e.maxRetries; attempt++ {
		if ctx.Err() != nil {
			lastErr = ctx.Err()
			break
		}
		attempts = attempt + 1
		if attempt > 0 {
			backoff := time.Duration(attempt) * time.Second
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				lastErr = ctx.Err()
				break
			}
		}

		switch {
		case strings.HasPrefix(nodeKey, "image.portrait:"):
			cid := strings.TrimPrefix(nodeKey, "image.portrait:")
			ch := findChar(projSnap, cid)
			if ch == nil {
				lastErr = fmt.Errorf("character %s not found", cid)
				break
			}
			asset, modelUsed, lastErr = e.executor.GeneratePortrait(ctx, projSnap, *ch)

		case strings.HasPrefix(nodeKey, "image.scene:"):
			sid := strings.TrimPrefix(nodeKey, "image.scene:")
			sc := findScene(projSnap, sid)
			if sc == nil {
				lastErr = fmt.Errorf("scene %s not found", sid)
				break
			}
			asset, modelUsed, lastErr = e.executor.GenerateSceneAnchor(ctx, projSnap, *sc)

		case strings.HasPrefix(nodeKey, "image.keyframe:"):
			shid := strings.TrimPrefix(nodeKey, "image.keyframe:")
			sh := findShot(projSnap, shid)
			if sh == nil {
				lastErr = fmt.Errorf("shot %s not found", shid)
				break
			}
			var refs []string
			if ch := findChar(projSnap, sh.SpeakerID); ch != nil && ch.Portrait != "" {
				if a, ok := projSnap.Assets[ch.Portrait]; ok && a.URL != "" {
					refs = append(refs, a.URL)
				}
			}
			if sc := findScene(projSnap, sh.SceneID); sc != nil && sc.Anchor != "" {
				if a, ok := projSnap.Assets[sc.Anchor]; ok && a.URL != "" {
					refs = append(refs, a.URL)
				}
			}
			if sh.Continuous {
				if prev := findPrevShot(projSnap, *sh); prev != nil && prev.Keyframe != "" {
					if a, ok := projSnap.Assets[prev.Keyframe]; ok && a.URL != "" {
						refs = append(refs, a.URL)
					}
				}
			}
			asset, modelUsed, degraded, statusNote, lastErr = e.executor.GenerateKeyframe(ctx, projSnap, *sh, refs)

		case strings.HasPrefix(nodeKey, "tts.line:"):
			shid := strings.TrimPrefix(nodeKey, "tts.line:")
			sh := findShot(projSnap, shid)
			if sh == nil {
				lastErr = fmt.Errorf("shot %s not found", shid)
				break
			}
			var speaker *CharacterSpec
			if sh.SpeakerID != "" {
				speaker = findChar(projSnap, sh.SpeakerID)
			}
			asset, durSec, lastErr = e.executor.GenerateTTS(ctx, projSnap, *sh, speaker)

		case nodeKey == "script.analyze":
			be, ok := e.executor.(BreakdownExecutor)
			if !ok {
				lastErr = fmt.Errorf("breakdown executor not available")
				break
			}
			beatsOut, lastErr = be.AnalyzeScript(ctx, projSnap)

		case nodeKey == "script.highlights":
			be, ok := e.executor.(BreakdownExecutor)
			if !ok {
				lastErr = fmt.Errorf("breakdown executor not available")
				break
			}
			highlights, lastErr = be.SelectHighlights(ctx, projSnap)

		case nodeKey == "bible.characters":
			be, ok := e.executor.(BreakdownExecutor)
			if !ok {
				lastErr = fmt.Errorf("breakdown executor not available")
				break
			}
			charsOut, lastErr = be.BuildCharacters(ctx, projSnap)

		case nodeKey == "bible.scenes":
			be, ok := e.executor.(BreakdownExecutor)
			if !ok {
				lastErr = fmt.Errorf("breakdown executor not available")
				break
			}
			scenesOut, lastErr = be.BuildScenes(ctx, projSnap)

		case nodeKey == "storyboard.shots":
			be, ok := e.executor.(BreakdownExecutor)
			if !ok {
				lastErr = fmt.Errorf("breakdown executor not available")
				break
			}
			shotsOut, lastErr = be.BuildShots(ctx, projSnap)

		case strings.HasPrefix(nodeKey, "video.clip:"):
			shid := strings.TrimPrefix(nodeKey, "video.clip:")
			sh := findShot(projSnap, shid)
			if sh == nil {
				lastErr = fmt.Errorf("shot %s not found", shid)
				break
			}
			ve, ok := e.executor.(VideoExecutor)
			if !ok {
				degraded = true
				modelUsed = "ken_burns"
				statusNote = "使用关键帧 Ken Burns 运镜合成"
				lastErr = nil
				break
			}
			existingTaskID := ""
			if prevSt := projSnap.Nodes[nodeKey]; prevSt != nil {
				existingTaskID = prevSt.TaskID
			}
			asset, modelUsed, degraded, statusNote, lastErr = ve.GenerateOrPollVideo(ctx, projSnap, *sh, existingTaskID, func(tid string) {
				if tid == "" {
					return
				}
				lk.Lock()
				p, l, r, lErr := e.LoadOrMigrateProject(state.projectID)
				if lErr == nil {
					if p.Nodes[nodeKey] == nil {
						p.Nodes[nodeKey] = &NodeState{Status: StatusRunning}
					}
					p.Nodes[nodeKey].TaskID = tid
					p.Nodes[nodeKey].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
					_ = e.saveProjectLocked(r, p, l)
				}
				lk.Unlock()
			})

		case nodeKey == "timeline.assemble":
			tl := AssembleTimeline(projSnap)
			timelineOut = &tl
			statusNote = fmt.Sprintf("已装配 %d 镜时间线（总时长 %.1fs）", len(tl.Tracks), tl.DurationSec)
			lastErr = nil

		case nodeKey == "render.mp4":
			re, ok := e.executor.(RenderExecutor)
			if !ok {
				degraded = true
				modelUsed = "browser_canvas"
				statusNote = "未配置外部渲染器，请使用前端 Canvas 合成导出"
				lastErr = nil
				break
			}
			asset, degraded, statusNote, lastErr = re.RenderMP4(ctx, projSnap)
			if !degraded && lastErr == nil {
				modelUsed = "ffmpeg-render-service"
			} else if degraded {
				modelUsed = "browser_canvas"
			}

		default:
			lastErr = fmt.Errorf("unsupported node key: %s", nodeKey)
		}

		if lastErr == nil {
			break
		}
		if attempt < e.maxRetries {
			e.appendRunLog(state, "warn", nodeKey, fmt.Sprintf("节点 %s 第 %d 次尝试失败，准备重试：%v", nodeKey, attempt+1, lastErr))
		}
	}

	if lastErr != nil {
		e.commitNodeFailure(state, nodeKey, attempts, lastErr.Error())
		return
	}

	lk.Lock()
	defer lk.Unlock()
	latestProj, layout, rec, loadErr := e.LoadOrMigrateProject(state.projectID)
	if loadErr != nil {
		return
	}
	latestHash := ComputeNodeInputHash(latestProj, nodeKey)
	now := time.Now().UTC().Format(time.RFC3339)

	// 写入资产（如果有产出 URL）
	outputID := ""
	if asset.URL != "" {
		if asset.ID == "" {
			asset.ID = "asset_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
		}
		if asset.CreatedAt == "" {
			asset.CreatedAt = now
		}
		latestProj.Assets[asset.ID] = asset
		outputID = asset.ID
	}

	// 绑定到领域对象字段
	switch {
	case nodeKey == "script.analyze":
		latestProj.Analysis.Beats = beatsOut
	case nodeKey == "script.highlights":
		latestProj.Analysis.HighlightBeatIDs = highlights
	case nodeKey == "bible.characters":
		latestProj.Characters = charsOut
	case nodeKey == "bible.scenes":
		latestProj.Scenes = scenesOut
	case nodeKey == "storyboard.shots":
		latestProj.Shots = shotsOut
	case nodeKey == "timeline.assemble" && timelineOut != nil:
		latestProj.Timeline = *timelineOut
	case nodeKey == "render.mp4" && outputID != "":
		latestProj.Timeline.RenderedMP4 = asset.URL
	case strings.HasPrefix(nodeKey, "image.portrait:") && outputID != "":
		cid := strings.TrimPrefix(nodeKey, "image.portrait:")
		for i := range latestProj.Characters {
			if latestProj.Characters[i].ID == cid {
				latestProj.Characters[i].Portrait = outputID
			}
		}
	case strings.HasPrefix(nodeKey, "image.scene:") && outputID != "":
		sid := strings.TrimPrefix(nodeKey, "image.scene:")
		for i := range latestProj.Scenes {
			if latestProj.Scenes[i].ID == sid {
				latestProj.Scenes[i].Anchor = outputID
			}
		}
	case strings.HasPrefix(nodeKey, "image.keyframe:") && outputID != "":
		shid := strings.TrimPrefix(nodeKey, "image.keyframe:")
		for i := range latestProj.Shots {
			if latestProj.Shots[i].ID == shid {
				latestProj.Shots[i].Keyframe = outputID
			}
		}
	case strings.HasPrefix(nodeKey, "tts.line:") && outputID != "":
		shid := strings.TrimPrefix(nodeKey, "tts.line:")
		for i := range latestProj.Shots {
			if latestProj.Shots[i].ID == shid {
				latestProj.Shots[i].Audio = outputID
				if durSec > 0 {
					latestProj.Shots[i].ActualAudioDuration = durSec
					latestProj.Shots[i].DurationSec = durSec
				}
			}
		}
	case strings.HasPrefix(nodeKey, "video.clip:") && outputID != "":
		shid := strings.TrimPrefix(nodeKey, "video.clip:")
		for i := range latestProj.Shots {
			if latestProj.Shots[i].ID == shid {
				latestProj.Shots[i].Video = outputID
			}
		}
	}

	st := latestProj.Nodes[nodeKey]
	if st == nil {
		st = &NodeState{}
		latestProj.Nodes[nodeKey] = st
	}
	if latestHash != execHash {
		// 运行期间上游被用户/Agent 编辑，标记为 stale 以便按新输入重算
		st.Status = StatusStale
		st.StatusNote = "运行期间输入已变更，等待重算"
	} else if degraded {
		st.Status = StatusDegraded
		st.StatusNote = statusNote
	} else {
		st.Status = StatusReady
		st.StatusNote = statusNote
	}
	st.InputHash = execHash
	if outputID != "" {
		st.Output = outputID
	}
	st.Attempts = attempts
	st.Error = ""
	st.ModelUsed = modelUsed
	st.UpdatedAt = now
	latestProj.Revision++
	_ = e.saveProjectLocked(rec, latestProj, layout)

	state.broadcast(RunEvent{
		Type:      "node.update",
		RunID:     state.runID,
		ProjectID: state.projectID,
		NodeKey:   nodeKey,
		NodeState: st,
		Revision:  latestProj.Revision,
	})
	durSecNode := time.Since(nodeStart).Seconds()
	if st.Status == StatusDegraded {
		e.appendRunLog(state, "warn", nodeKey, fmt.Sprintf("降级完成 %s（%s，耗时 %.1fs）", nodeKey, st.StatusNote, durSecNode))
	} else if st.Status == StatusStale {
		e.appendRunLog(state, "warn", nodeKey, fmt.Sprintf("节点 %s 运行期间上游已修改，已标为待更新", nodeKey))
	} else {
		detail := modelUsed
		if detail == "" {
			detail = st.StatusNote
		}
		if detail != "" {
			e.appendRunLog(state, "success", nodeKey, fmt.Sprintf("完成 %s（%s，耗时 %.1fs）", nodeKey, detail, durSecNode))
		} else {
			e.appendRunLog(state, "success", nodeKey, fmt.Sprintf("完成 %s（耗时 %.1fs）", nodeKey, durSecNode))
		}
	}
}

func (e *Engine) commitNodeFailure(state *activeRunState, nodeKey string, attempts int, errMsg string) {
	lk := e.getProjectLock(state.projectID)
	lk.Lock()
	defer lk.Unlock()
	proj, layout, rec, err := e.LoadOrMigrateProject(state.projectID)
	if err != nil {
		return
	}
	st := proj.Nodes[nodeKey]
	if st == nil {
		st = &NodeState{}
		proj.Nodes[nodeKey] = st
	}
	st.Status = StatusFailed
	st.Attempts = attempts
	st.Error = errMsg
	st.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	proj.Revision++
	_ = e.saveProjectLocked(rec, proj, layout)

	state.broadcast(RunEvent{
		Type:      "node.update",
		RunID:     state.runID,
		ProjectID: state.projectID,
		NodeKey:   nodeKey,
		NodeState: st,
		Revision:  proj.Revision,
	})
	e.appendRunLog(state, "error", nodeKey, fmt.Sprintf("失败 %s（已尝试 %d 次）：%s", nodeKey, attempts, errMsg))
}

func (e *Engine) finishRun(state *activeRunState, status, msg string, revision int64) {
	durSec := time.Since(state.startedAt).Seconds()
	lvl := "success"
	if status == "partial" {
		lvl = "warn"
	} else if status == "failed" || status == "cancelled" {
		lvl = "error"
	}
	e.appendRunLog(state, lvl, "", fmt.Sprintf("%s（总耗时 %.1fs）", msg, durSec))

	now := time.Now().UTC()
	state.logsMu.Lock()
	logsBytes, _ := json.Marshal(state.logs)
	sumCopy := state.summary
	sumCopy.DurationMs = time.Since(state.startedAt).Milliseconds()
	sumBytes, _ := json.Marshal(sumCopy)
	state.logsMu.Unlock()

	if e.db != nil {
		_ = e.db.Model(&db.DramaRunRecord{}).
			Where("id = ?", state.runID).
			Updates(map[string]interface{}{
				"status":       status,
				"error":        msg,
				"logs_json":    string(logsBytes),
				"summary_json": string(sumBytes),
				"finished_at":  &now,
				"updated_at":   now,
			}).Error
	}
	state.broadcast(RunEvent{
		Type:      "run.update",
		RunID:     state.runID,
		ProjectID: state.projectID,
		Status:    status,
		Message:   msg,
		Summary:   &sumCopy,
		Revision:  revision,
	})
}
