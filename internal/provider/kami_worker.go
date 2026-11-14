package provider

import (
	"log"
	"strings"
	"sync"
	"time"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/audit"
	"ai-web-free/internal/chatgpt"
)

// KamiLoginExecutor 执行单账号卡密登录并返回提取结果，可在单元测试中替换。
type KamiLoginExecutor func(acct *accounts.Account, defaultProxy string, headless bool) KamiLoginOutcome

// KamiWorkerStats 卡密后台串行续期队列的可观测快照。
type KamiWorkerStats struct {
	Running        bool   `json:"running"`
	RunningEmail   string `json:"running_email,omitempty"`
	QueueLength    int    `json:"queue_length"`
	TotalSucceeded int64  `json:"total_succeeded"`
	TotalFailed    int64  `json:"total_failed"`
	LastRunAt      string `json:"last_run_at,omitempty"`
	LastError      string `json:"last_error,omitempty"`
}

var kamiRenewBackoff = []time.Duration{
	5 * time.Minute,
	30 * time.Minute,
	2 * time.Hour,
}

// KamiRenewWorker 单消费者串行卡密自动续期 Worker，同一时刻最多启动一个 chromedp 浏览器实例。
type KamiRenewWorker struct {
	pool         *accounts.Pool
	defaultProxy string
	headless     bool
	persistFn    func() error
	auditLog     *audit.RingBuffer
	loginFn      KamiLoginExecutor
	backoff      []time.Duration

	mu             sync.Mutex
	queue          []*accounts.Account
	queued         map[string]bool
	runningID      string
	runningEmail   string
	totalSucceeded int64
	totalFailed    int64
	lastRunAt      time.Time
	lastError      string
	started        bool
	closed         bool

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
		pool:         pool,
		defaultProxy: defaultProxy,
		headless:     headless,
		persistFn:    persistFn,
		auditLog:     auditLog,
		loginFn:      defaultKamiLoginExecutor,
		backoff:      kamiRenewBackoff,
		queued:       make(map[string]bool),
		notifyCh:     make(chan struct{}, 1),
		stopCh:       make(chan struct{}),
		doneCh:       make(chan struct{}),
	}
}

func defaultKamiLoginExecutor(acct *accounts.Account, defaultProxy string, headless bool) KamiLoginOutcome {
	res, err := RunKamiPlaywrightLogin([]*accounts.Account{acct}, defaultProxy, headless)
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

	acct.RenewState = "queued"
	w.queue = append(w.queue, acct)
	w.queued[acct.ID] = true
	if reason != "" {
		log.Printf("[kami-worker] enqueued %s (%s)", acct.Email, reason)
	}
	select {
	case w.notifyCh <- struct{}{}:
	default:
	}
	return true
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

	st := KamiWorkerStats{
		Running:        w.runningID != "",
		RunningEmail:   w.runningEmail,
		QueueLength:    len(w.queue),
		TotalSucceeded: w.totalSucceeded,
		TotalFailed:    w.totalFailed,
		LastError:      w.lastError,
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
	w.runningID = acct.ID
	w.runningEmail = acct.Email
	return acct
}

func (w *KamiRenewWorker) processOne(acct *accounts.Account) {
	acct.RenewState = "running"

	w.mu.Lock()
	loginFn := w.loginFn
	defaultProxy := w.defaultProxy
	headless := w.headless
	backoffTable := w.backoff
	w.mu.Unlock()

	outcome := loginFn(acct, defaultProxy, headless)
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
		acct.Status = accounts.StatusActive
		acct.CooldownUntil = time.Time{}
		acct.ImageCooldownUntil = time.Time{}
		acct.RenewState = "idle"
		acct.RenewFailures = 0
		acct.RenewCooldownUntil = time.Time{}
		acct.LastRenewError = ""
		acct.LastError = ""
		acct.LastChecked = now

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
		if w.auditLog != nil {
			w.auditLog.RecordEvent("/internal/account-banned", acct.Email, acct.Proxy, "kami_renew_banned", errMsg)
		}
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
	acct.RenewState = "cooldown"
	acct.RenewCooldownUntil = now.Add(backoffTable[idx])
	acct.LastRenewError = errMsg
	acct.LastChecked = now

	if w.auditLog != nil {
		w.auditLog.RecordEvent("/internal/kami-renew", acct.Email, acct.Proxy, "renew_failed", errMsg)
	}

	w.mu.Lock()
	w.totalFailed++
	w.lastRunAt = now
	w.lastError = errMsg
	w.runningID = ""
	w.runningEmail = ""
	w.mu.Unlock()
}
