package provider

import (
	"log"
	"strings"
	"sync"
	"time"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/audit"
	"ai-web-free/internal/chatgpt"
	"ai-web-free/internal/db"
)

// KamiLoginExecutor 执行单账号卡密登录并返回提取结果，可在单元测试中替换。
type KamiLoginExecutor func(acct *accounts.Account, defaultProxy string, headless bool) KamiLoginOutcome

// KamiWorkerStats 卡密后台串行续期队列的可观测快照。
type KamiWorkerStats struct {
	Running           bool     `json:"running"`
	RunningAccountID  string   `json:"running_account_id,omitempty"`
	RunningEmail      string   `json:"running_email,omitempty"`
	RunningPhase      string   `json:"running_phase,omitempty"`
	RunningPhaseLabel string   `json:"running_phase_label,omitempty"`
	RunningStartedAt  string   `json:"running_started_at,omitempty"`
	QueueLength       int      `json:"queue_length"`
	QueuedAccountIDs  []string `json:"queued_account_ids,omitempty"`
	TotalSucceeded    int64    `json:"total_succeeded"`
	TotalFailed       int64    `json:"total_failed"`
	LastRunAt         string   `json:"last_run_at,omitempty"`
	LastError         string   `json:"last_error,omitempty"`
}

var kamiRenewBackoff = []time.Duration{
	5 * time.Minute,
	30 * time.Minute,
	2 * time.Hour,
}

type kamiLoginProgressExecutor func(acct *accounts.Account, defaultProxy string, headless bool, onPhase KamiPhaseReporter) KamiLoginOutcome

// KamiRenewWorker 单消费者串行卡密自动续期 Worker，同一时刻最多启动一个 chromedp 浏览器实例。
type KamiRenewWorker struct {
	pool            *accounts.Pool
	defaultProxy    string
	headless        bool
	persistFn       func() error
	auditLog        *audit.RingBuffer
	loginFn         KamiLoginExecutor
	loginProgressFn kamiLoginProgressExecutor
	backoff         []time.Duration

	mu                sync.Mutex
	queue             []*accounts.Account
	queued            map[string]bool
	runningID         string
	runningEmail      string
	runningPhase      string
	runningPhaseLabel string
	runningStartedAt  time.Time
	totalSucceeded    int64
	totalFailed       int64
	lastRunAt         time.Time
	lastError         string
	started           bool
	closed            bool

	notifyCh chan struct{}
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// NewKamiRenewWorker 创建串行卡密自动续期 Worker。
func NewKamiRenewWorker(
	pool *accounts.Pool,
	defaultProxy string,
	headless bool,
	persistFn func() error,
	auditLog *audit.RingBuffer,
) *KamiRenewWorker {
	return &KamiRenewWorker{
		pool:            pool,
		defaultProxy:    defaultProxy,
		headless:        headless,
		persistFn:       persistFn,
		auditLog:        auditLog,
		loginFn:         defaultKamiLoginExecutor,
		loginProgressFn: defaultKamiLoginProgressExecutor,
		backoff:         kamiRenewBackoff,
		queued:          make(map[string]bool),
		notifyCh:        make(chan struct{}, 1),
		stopCh:          make(chan struct{}),
		doneCh:          make(chan struct{}),
	}
}

func defaultKamiLoginExecutor(acct *accounts.Account, defaultProxy string, headless bool) KamiLoginOutcome {
	return defaultKamiLoginProgressExecutor(acct, defaultProxy, headless, nil)
}

func defaultKamiLoginProgressExecutor(acct *accounts.Account, defaultProxy string, headless bool, onPhase KamiPhaseReporter) KamiLoginOutcome {
	res, err := RunKamiPlaywrightLoginWithProgress([]*accounts.Account{acct}, defaultProxy, headless, onPhase)
	if err != nil {
		return KamiLoginOutcome{Email: acct.Email, OK: false, Error: err.Error()}
	}
	if outcome, ok := res[strings.ToLower(strings.TrimSpace(acct.Email))]; ok {
		return outcome
	}
	return KamiLoginOutcome{Email: acct.Email, OK: false, Error: "浏览器登录未返回账号结果"}
}

// SetLoginExecutor 替换卡密登录执行器（主要用于单元测试注入 mock）。
func (w *KamiRenewWorker) SetLoginExecutor(fn KamiLoginExecutor) {
	if fn == nil {
		return
	}
	w.mu.Lock()
	w.loginFn = fn
	w.loginProgressFn = nil
	w.mu.Unlock()
}

// Start 启动单消费者串行消费 goroutine。幂等。
func (w *KamiRenewWorker) Start() {
	w.mu.Lock()
	if w.started || w.closed {
		w.mu.Unlock()
		return
	}
	w.started = true
	w.mu.Unlock()

	go w.loop()
}

// Stop 优雅停止后台消费 goroutine。
func (w *KamiRenewWorker) Stop() {
	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return
	}
	wasStarted := w.started
	w.closed = true
	close(w.stopCh)
	w.mu.Unlock()

	if wasStarted {
		<-w.doneCh
	}
}

func (w *KamiRenewWorker) setActionState(acct *accounts.Account, activeAction, phase, phaseLabel string, queuePos int, startedAt time.Time) {
	if acct == nil {
		return
	}
	if w.pool != nil {
		w.pool.SetAccountActionState(acct.ID, activeAction, phase, phaseLabel, "", queuePos, startedAt)
		return
	}
	acct.ActiveAction = activeAction
	acct.ActionPhase = phase
	acct.ActionPhaseLabel = phaseLabel
	acct.ActionQueuePos = queuePos
	acct.ActionTaskID = ""
	acct.ActionStartedAt = startedAt
}

func (w *KamiRenewWorker) setRenewState(acct *accounts.Account, state string) {
	if acct == nil {
		return
	}
	if w.pool != nil {
		w.pool.SetAccountRenewState(acct.ID, state)
		return
	}
	acct.RenewState = state
}

func (w *KamiRenewWorker) finishAction(acct *accounts.Account, ok bool, summary string) {
	if acct == nil {
		return
	}
	if w.pool != nil {
		w.pool.FinishAccountAction(acct.ID, "renew", ok, summary)
		return
	}
	acct.ActiveAction = ""
	acct.ActionPhase = ""
	acct.ActionPhaseLabel = ""
	acct.ActionQueuePos = 0
	acct.ActionTaskID = ""
	acct.ActionStartedAt = time.Time{}
	acct.LastAction = "renew"
	acct.LastActionOK = ok
	acct.LastActionSummary = summary
	acct.LastActionAt = time.Now()
}

func (w *KamiRenewWorker) refreshQueuePositionsLocked() {
	for idx, item := range w.queue {
		if item == nil {
			continue
		}
		w.setRenewState(item, "queued")
		w.setActionState(item, "renew", "queued", "排队等待自动续期", idx+1, time.Time{})
	}
}

// Enqueue 将满足条件的卡密账号加入串行续期队列（按账号 ID 去重并遵守失败退避期）。
func (w *KamiRenewWorker) Enqueue(acct *accounts.Account, reason string) bool {
	if w == nil || acct == nil {
		return false
	}
	if acct.IsTemporary || acct.Password == "" || acct.TOTPSecret == "" {
		return false
	}
	if acct.RefreshToken != "" || acct.SessionToken != "" {
		return false
	}
	if acct.Status == accounts.StatusDisabled || acct.Status == accounts.StatusBanned {
		return false
	}
	if acct.ActiveAction == "login" {
		return false
	}

	now := time.Now()
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return false
	}
	if w.queued[acct.ID] || w.runningID == acct.ID {
		return false
	}
	if !acct.RenewCooldownUntil.IsZero() && now.Before(acct.RenewCooldownUntil) {
		return false
	}

	w.queue = append(w.queue, acct)
	w.queued[acct.ID] = true
	w.refreshQueuePositionsLocked()
	if reason != "" {
		log.Printf("[kami-worker] enqueued %s (%s)", acct.Email, reason)
	}
	select {
	case w.notifyCh <- struct{}{}:
	default:
	}
	return true
}

func (w *KamiRenewWorker) clearQueuedAccountState(acct *accounts.Account) {
	if acct == nil {
		return
	}
	if w.pool != nil {
		w.pool.ClearAccountQueuedAction(acct.ID, "已取消自动续期排队")
		w.pool.SetAccountRenewState(acct.ID, "idle")
		return
	}
	acct.ActiveAction = ""
	acct.ActionPhase = ""
	acct.ActionPhaseLabel = ""
	acct.ActionQueuePos = 0
	acct.ActionTaskID = ""
	acct.ActionStartedAt = time.Time{}
	acct.RenewState = "idle"
	acct.LastAction = "renew"
	acct.LastActionOK = false
	acct.LastActionSummary = "已取消自动续期排队"
	acct.LastActionAt = time.Now()
}

// CancelQueued 从自动续期等待队列中移除指定账号（如果尚未开始运行）。
func (w *KamiRenewWorker) CancelQueued(accountID string) bool {
	if id := strings.TrimSpace(accountID); id != "" {
		return w.CancelQueuedBatch([]string{id}) > 0
	}
	return false
}

// CancelQueuedBatch 从自动续期等待队列中批量移除尚未开始运行的账号，返回取消数量。
func (w *KamiRenewWorker) CancelQueuedBatch(accountIDs []string) int {
	if w == nil || len(accountIDs) == 0 {
		return 0
	}
	targets := make(map[string]bool, len(accountIDs))
	for _, raw := range accountIDs {
		if id := strings.TrimSpace(raw); id != "" {
			targets[id] = true
		}
	}
	if len(targets) == 0 {
		return 0
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.queue) == 0 {
		return 0
	}
	next := make([]*accounts.Account, 0, len(w.queue))
	removed := make([]*accounts.Account, 0, len(targets))
	for _, item := range w.queue {
		if item != nil && targets[item.ID] {
			removed = append(removed, item)
			delete(w.queued, item.ID)
			continue
		}
		next = append(next, item)
	}
	if len(removed) == 0 {
		return 0
	}
	w.queue = next
	w.refreshQueuePositionsLocked()
	for _, item := range removed {
		w.clearQueuedAccountState(item)
	}
	return len(removed)
}

// CancelAllQueued 清空自动续期等待队列中所有尚未开始运行的账号，返回取消数量。
func (w *KamiRenewWorker) CancelAllQueued() int {
	if w == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	if len(w.queue) == 0 {
		return 0
	}
	removed := make([]*accounts.Account, 0, len(w.queue))
	for _, item := range w.queue {
		if item != nil {
			removed = append(removed, item)
		}
	}
	w.queue = nil
	w.queued = make(map[string]bool)
	for _, item := range removed {
		w.clearQueuedAccountState(item)
	}
	return len(removed)
}

// EnqueueDue 扫描号池中过期或临期（< within）的卡密账号并入队。
func (w *KamiRenewWorker) EnqueueDue(within time.Duration) int {
	if w == nil || w.pool == nil {
		return 0
	}
	candidates := w.pool.NeedsProactiveRenew(within)
	added := 0
	for _, acct := range candidates {
		if w.Enqueue(acct, "proactive_or_expired") {
			added++
		}
	}
	return added
}

// Stats 返回卡密自动续期队列当前状态。
func (w *KamiRenewWorker) Stats() KamiWorkerStats {
	if w == nil {
		return KamiWorkerStats{}
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	queuedIDs := make([]string, 0, len(w.queue))
	for _, item := range w.queue {
		if item != nil {
			queuedIDs = append(queuedIDs, item.ID)
		}
	}
	st := KamiWorkerStats{
		Running:           w.runningID != "",
		RunningAccountID:  w.runningID,
		RunningEmail:      w.runningEmail,
		RunningPhase:      w.runningPhase,
		RunningPhaseLabel: w.runningPhaseLabel,
		QueueLength:       len(w.queue),
		QueuedAccountIDs:  queuedIDs,
		TotalSucceeded:    w.totalSucceeded,
		TotalFailed:       w.totalFailed,
		LastError:         w.lastError,
	}
	if !w.runningStartedAt.IsZero() {
		st.RunningStartedAt = w.runningStartedAt.Format(time.RFC3339)
	}
	if !w.lastRunAt.IsZero() {
		st.LastRunAt = w.lastRunAt.Format(time.RFC3339)
	}
	return st
}

func (w *KamiRenewWorker) loop() {
	defer close(w.doneCh)
	for {
		acct := w.popNext()
		if acct != nil {
			w.processOne(acct)
			continue
		}
		select {
		case <-w.stopCh:
			return
		case <-w.notifyCh:
		}
	}
}

func (w *KamiRenewWorker) popNext() *accounts.Account {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.queue) == 0 {
		return nil
	}
	acct := w.queue[0]
	w.queue[0] = nil
	w.queue = w.queue[1:]
	delete(w.queued, acct.ID)
	startedAt := time.Now()
	w.runningID = acct.ID
	w.runningEmail = acct.Email
	w.runningPhase = "launch_browser"
	w.runningPhaseLabel = KamiPhaseLabel("launch_browser")
	w.runningStartedAt = startedAt
	w.refreshQueuePositionsLocked()
	w.setRenewState(acct, "running")
	w.setActionState(acct, "renew", w.runningPhase, w.runningPhaseLabel, 0, startedAt)
	return acct
}

func (w *KamiRenewWorker) processOne(acct *accounts.Account) {
	w.mu.Lock()
	loginFn := w.loginFn
	loginProgressFn := w.loginProgressFn
	defaultProxy := w.defaultProxy
	headless := w.headless
	backoffTable := w.backoff
	startedAt := w.runningStartedAt
	w.mu.Unlock()

	onPhase := func(accountID, phase, label string) {
		if label == "" {
			label = KamiPhaseLabel(phase)
		}
		w.mu.Lock()
		if w.runningID == accountID {
			w.runningPhase = phase
			w.runningPhaseLabel = label
		}
		w.mu.Unlock()
		if acct != nil && acct.ID == accountID {
			w.setActionState(acct, "renew", phase, label, 0, startedAt)
		}
	}

	var outcome KamiLoginOutcome
	if loginProgressFn != nil {
		outcome = loginProgressFn(acct, defaultProxy, headless, onPhase)
	} else {
		outcome = loginFn(acct, defaultProxy, headless)
	}
	now := time.Now()

	if outcome.OK && outcome.AccessToken != "" {
		acct.Token = outcome.AccessToken
		if outcome.SessionToken != "" {
			acct.SessionToken = outcome.SessionToken
		}
		if outcome.ChatGPTAccountID != "" {
			acct.ChatGPTAccountID = outcome.ChatGPTAccountID
		}
		acct.SyncJWTMetadata()
		if acct.ExpiresAt.IsZero() && outcome.ExpiresAt > 0 {
			acct.ExpiresAt = time.Unix(outcome.ExpiresAt, 0)
		}
		if w.pool != nil {
			w.pool.SetStatus(acct.ID, accounts.StatusActive)
		} else {
			acct.Status = accounts.StatusActive
		}
		acct.CooldownUntil = time.Time{}
		acct.ImageCooldownUntil = time.Time{}
		w.setRenewState(acct, "idle")
		acct.RenewFailures = 0
		acct.RenewCooldownUntil = time.Time{}
		acct.LastRenewError = ""
		acct.LastError = ""
		acct.LastChecked = now
		if acct.Email != "" {
			_ = db.DeleteRestrictionFromDB(db.GetDB(), acct.Email, "banned")
		}
		if acct.ChatGPTAccountID != "" {
			_ = db.DeleteRestrictionFromDB(db.GetDB(), acct.ChatGPTAccountID, "banned")
		}
		w.finishAction(acct, true, "后台自动续期成功，Token 已更新")

		if w.persistFn != nil {
			if err := w.persistFn(); err != nil {
				log.Printf("[kami-worker] persist access_tokens failed for %s: %v", acct.Email, err)
			}
		}
		if w.auditLog != nil {
			w.auditLog.RecordEvent("/internal/kami-renew", acct.Email, acct.Proxy, "renew_ok", "")
		}

		w.mu.Lock()
		w.totalSucceeded++
		w.lastRunAt = now
		w.lastError = ""
		w.runningID = ""
		w.runningEmail = ""
		w.runningPhase = ""
		w.runningPhaseLabel = ""
		w.runningStartedAt = time.Time{}
		w.mu.Unlock()
		return
	}

	errMsg := outcome.Error
	if errMsg == "" {
		errMsg = "kami auto renew failed"
	}
	if chatgpt.LooksLikeAccountBanned(0, errMsg) {
		if w.pool != nil {
			w.pool.MarkBanned(acct, errMsg)
		} else {
			acct.Status = accounts.StatusBanned
			acct.LastError = errMsg
			acct.RenewState = ""
			acct.RenewCooldownUntil = time.Time{}
		}
		w.finishAction(acct, false, "账号已被封禁: "+summarizeActionError(errMsg))
		if w.auditLog != nil {
			w.auditLog.RecordEvent("/internal/account-banned", acct.Email, acct.Proxy, "kami_renew_banned", errMsg)
		}
		w.mu.Lock()
		w.totalFailed++
		w.lastRunAt = now
		w.lastError = errMsg
		w.runningID = ""
		w.runningEmail = ""
		w.runningPhase = ""
		w.runningPhaseLabel = ""
		w.runningStartedAt = time.Time{}
		w.mu.Unlock()
		return
	}
	acct.RenewFailures++
	idx := acct.RenewFailures - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(backoffTable) {
		idx = len(backoffTable) - 1
	}
	w.setRenewState(acct, "cooldown")
	acct.RenewCooldownUntil = now.Add(backoffTable[idx])
	acct.LastRenewError = errMsg
	acct.LastChecked = now
	w.finishAction(acct, false, "自动续期失败: "+summarizeActionError(errMsg))

	if w.auditLog != nil {
		w.auditLog.RecordEvent("/internal/kami-renew", acct.Email, acct.Proxy, "renew_failed", errMsg)
	}

	w.mu.Lock()
	w.totalFailed++
	w.lastRunAt = now
	w.lastError = errMsg
	w.runningID = ""
	w.runningEmail = ""
	w.runningPhase = ""
	w.runningPhaseLabel = ""
	w.runningStartedAt = time.Time{}
	w.mu.Unlock()
}
