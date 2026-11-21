package accounts

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"log"
	"math"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var ErrNoAvailable = errors.New("no available account of the requested type")

// Pool 账号池管理，按类型分三个数组，Acquire 直接取无需遍历
// 临时账号存放在单独的 map[string]*Account 中,以 token hash 为 key
type Pool struct {
	mu      sync.Mutex
	noauth  []*Account
	free    []*Account
	puid    []*Account
	cursors [3]int // 0=noauth,1=free,2=puid

	// 临时账号 (外部传入的 accessToken 创建的)
	tempMu    sync.RWMutex
	temporary map[string]*Account // key = tokenHash

	// 上游模型目录内存缓存（按探测/拉取结果合并去重，受 p.mu 保护）
	cachedModels           []string
	modelsCachedAt         time.Time
	providerModels         map[string][]string
	providerModelsCachedAt map[string]time.Time

	// 账号永久封禁回调（用于持久化 banned 状态并剔除死 Token）
	onBanned func(acct *Account, reason string)

	// 热池/待命池策略（按账号类型）
	policies   map[AccountType]PoolPolicy
	lastRotate map[AccountType]time.Time
}

// typeIndex 返回 AccountType 在 cursors 中的索引
func typeIndex(t AccountType) int {
	switch t {
	case TypeNoAuth:
		return 0
	case TypeFree:
		return 1
	case TypePUID:
		return 2
	}
	return -1
}

func (p *Pool) sliceFor(t AccountType) *[]*Account {
	switch t {
	case TypeNoAuth:
		return &p.noauth
	case TypeFree:
		return &p.free
	case TypePUID:
		return &p.puid
	}
	return nil
}

func NewPool(initial []*Account) *Pool {
	pool := &Pool{
		temporary:              make(map[string]*Account),
		providerModels:         make(map[string][]string),
		providerModelsCachedAt: make(map[string]time.Time),
	}
	for _, acct := range initial {
		pool.AddAccount(acct)
	}
	return pool
}

// AddAccount 按类型添加到对应数组
func (p *Pool) AddAccount(acct *Account) {
	if acct == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	switch acct.Type {
	case TypeNoAuth:
		p.noauth = append(p.noauth, acct)
	case TypeFree:
		p.free = append(p.free, acct)
	case TypePUID:
		p.puid = append(p.puid, acct)
	}
}

// Acquire 从对应类型数组中轮询获取一个可用账号
func (p *Pool) Acquire(acctType AccountType) (*Account, error) {
	return p.AcquireForCapability(acctType, "", nil)
}

func isImageCapability(capName string) bool {
	return capName == CapImageGenerate.Name || capName == CapImageEdit.Name || capName == CapImageVariation.Name
}

// AcquireForCapability 按账号类型与能力轮询获取可用账号，跳过 exclude、全能力冷却及对应生图冷却中的账号。
func (p *Pool) AcquireForCapability(acctType AccountType, capName string, exclude map[string]bool) (*Account, error) {
	return p.AcquireMatching([]AccountType{acctType}, nil, capName, exclude)
}

// AcquireMatching 按类型优先级与自定义过滤器取号（分组调度使用），match 为 nil 表示不过滤。
func (p *Pool) AcquireMatching(types []AccountType, match func(*Account) bool, capName string, exclude map[string]bool) (*Account, error) {
	p.mu.Lock()
	var picked *Account
	for _, t := range types {
		if a := p.acquireLocked(t, match, capName, exclude); a != nil {
			picked = a
			break
		}
	}
	p.mu.Unlock()
	if picked != nil {
		_ = picked.EnsureClient()
		return picked, nil
	}
	return nil, ErrNoAvailable
}

// CollectModels 汇总匹配账号已缓存的模型 slug（去重、保持首次出现顺序）。
func (p *Pool) CollectModels(types []AccountType, match func(*Account) bool) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	seen := map[string]bool{}
	var out []string
	for _, t := range types {
		slice := p.sliceFor(t)
		if slice == nil {
			continue
		}
		for _, a := range *slice {
			if a.Status != StatusActive || (match != nil && !match(a)) {
				continue
			}
			for _, m := range a.Models {
				if !seen[m] {
					seen[m] = true
					out = append(out, m)
				}
			}
		}
	}
	return out
}

func (p *Pool) acquireLocked(acctType AccountType, match func(*Account) bool, capName string, exclude map[string]bool) *Account {
	slice := p.sliceFor(acctType)
	if slice == nil || len(*slice) == 0 {
		return nil
	}

	idx := typeIndex(acctType)
	if idx < 0 {
		return nil
	}

	now := time.Now()
	needImage := isImageCapability(capName)
	entries := *slice
	for i := 0; i < len(entries); i++ {
		cur := (p.cursors[idx] + i) % len(entries)
		a := entries[cur]
		if exclude != nil && exclude[a.ID] || (match != nil && !match(a)) {
			continue
		}
		// 冷却期届满时惰性恢复，避免请求侧必须等待后台巡检周期
		if a.Status == StatusRateLimited && !a.IsInCooldown(now) {
			a.Status = StatusActive
			a.CooldownUntil = time.Time{}
		}
		if a.Status != StatusActive {
			continue
		}
		if a.IsInCooldown(now) {
			continue
		}
		if needImage && a.IsImageInCooldown(now) {
			continue
		}
		p.cursors[idx] = (cur + 1) % len(entries)
		a.TotalCalls++
		a.LastUsed = now
		return a
	}

	return nil
}

// Release 保留接口但不再需要主动调用（统计在 Acquire 时已更新）。
// 保留以防将来改为独占模式需要。
func (p *Pool) Release(acct *Account, result error) {
}

// ReportFailure 标记账号为过期，Acquire 时自动跳过，后续健康检查会尝试续期
func (p *Pool) ReportFailure(acct *Account) bool {
	return p.MarkExpired(acct, acctErrOrKeep(acct))
}

func acctErrOrKeep(acct *Account) string {
	if acct == nil {
		return ""
	}
	return acct.LastError
}

// MarkExpired 将账号标记为过期并递增失败计数。
func (p *Pool) MarkExpired(acct *Account, reason string) bool {
	if acct == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	acct.Status = StatusExpired
	acct.FailedCalls++
	acct.LastChecked = time.Now()
	if reason != "" {
		acct.LastError = reason
	}
	return true
}

// SetOnBanned 注册账号永久封禁时的全局回调。
func (p *Pool) SetOnBanned(fn func(acct *Account, reason string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onBanned = fn
}

// MarkBanned 将账号标记为永久封禁/停用并终止自动续期队列跟踪。
func (p *Pool) MarkBanned(acct *Account, reason string) bool {
	if acct == nil {
		return false
	}
	p.mu.Lock()
	acct.Status = StatusBanned
	acct.FailedCalls++
	acct.LastChecked = time.Now()
	acct.CooldownUntil = time.Time{}
	acct.ImageCooldownUntil = time.Time{}
	acct.RenewState = ""
	acct.RenewCooldownUntil = time.Time{}
	if reason != "" {
		acct.LastError = reason
	}
	callback := p.onBanned
	p.mu.Unlock()

	if callback != nil {
		callback(acct, reason)
	}
	return true
}

// RecordProxyBlock 记录代理风控/WAF/Cloudflare 403 拦截：保留令牌有效状态，仅施加短冷却并记录诊断错误。
func (p *Pool) RecordProxyBlock(acct *Account, cooldown time.Duration, reason string) bool {
	if acct == nil {
		return false
	}
	if cooldown <= 0 {
		cooldown = 2 * time.Minute
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	acct.FailedCalls++
	acct.LastChecked = time.Now()
	if acct.Status != StatusBanned && acct.Status != StatusDisabled && acct.Status != StatusExpired {
		acct.Status = StatusRateLimited
		acct.CooldownUntil = time.Now().Add(cooldown)
	}
	if reason != "" {
		acct.LastError = reason
	}
	return true
}

// ResetRenewBackoff 清除账号卡密自动续期的退避冷却状态，便于管理员或自愈任务立即重试。
func (p *Pool) ResetRenewBackoff(acct *Account) bool {
	if acct == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	acct.RenewState = ""
	acct.RenewCooldownUntil = time.Time{}
	acct.RenewFailures = 0
	acct.LastRenewError = ""
	return true
}

// RecoverProxyBlockedStatus 在账号完成代理迁移或代理恢复后，解除因代理风控/传输故障触发的短时限流状态。
func (p *Pool) RecoverProxyBlockedStatus(acct *Account) bool {
	if acct == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if acct.Status == StatusRateLimited && (acct.Token != "" || acct.Type == TypeNoAuth) {
		acct.Status = StatusActive
		acct.CooldownUntil = time.Time{}
		return true
	}
	return false
}

// ReportRateLimit 根据限流作用域置入全能力冷却（StatusRateLimited）或仅生图局部冷却。
func (p *Pool) ReportRateLimit(acct *Account, imageOnly bool, cooldown time.Duration, reason string) {
	if acct == nil {
		return
	}
	if cooldown <= 0 {
		cooldown = 5 * time.Minute
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	until := now.Add(cooldown)
	acct.FailedCalls++
	if reason != "" {
		acct.LastError = reason
	}
	if imageOnly {
		acct.ImageCooldownUntil = until
		return
	}
	acct.CooldownUntil = until
	acct.Status = StatusRateLimited
}

// ExpiredAccounts 返回所有过期账号（用于健康检查）
func (p *Pool) ExpiredAccounts() []*Account {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []*Account
	for _, list := range [][]*Account{p.noauth, p.free, p.puid} {
		for _, a := range list {
			if a.Status == StatusExpired {
				out = append(out, a)
			}
		}
	}
	return out
}

// TokenRenewer 续期回调，由 bootstrap 提供
type TokenRenewer func(acct *Account) bool

// StartHealthCheck 启动健康检查 goroutine
func (p *Pool) StartHealthCheck(interval time.Duration, renew TokenRenewer) func() {
	if interval <= 0 {
		interval = 5 * time.Minute
	}
	stop := make(chan struct{})
	go func() {
		// 启动 500ms 后异步执行首次健康巡检与 Token 刷新，避免阻塞服务冷启动
		firstCheck := time.NewTimer(500 * time.Millisecond)
		defer firstCheck.Stop()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-firstCheck.C:
				p.runHealthCheck(renew)
			case <-ticker.C:
				p.runHealthCheck(renew)
			case <-stop:
				return
			}
		}
	}()
	return func() { close(stop) }
}

func (p *Pool) runHealthCheck(renew TokenRenewer) {
	if recovered := p.RecoverCooldowns(); recovered > 0 {
		log.Printf("[health] recovered %d account(s) from rate-limit cooldown", recovered)
	}
	if renew == nil {
		return
	}

	seen := make(map[string]bool)
	candidates := make([]*Account, 0)
	for _, acct := range p.ExpiredAccounts() {
		if acct != nil && !seen[acct.ID] {
			seen[acct.ID] = true
			candidates = append(candidates, acct)
		}
	}
	for _, acct := range p.NeedsProactiveRenew(24 * time.Hour) {
		if acct != nil && !seen[acct.ID] {
			seen[acct.ID] = true
			candidates = append(candidates, acct)
		}
	}

	for _, acct := range candidates {
		if renew(acct) {
			p.mu.Lock()
			acct.Status = StatusActive
			acct.CooldownUntil = time.Time{}
			acct.LastError = ""
			p.mu.Unlock()
			idShort := acct.ID
			if len(idShort) > 8 {
				idShort = idShort[:8]
			}
			log.Printf("[health] account %s renewed successfully", idShort)
		}
	}
}

// ── 临时账号管理 (外部传入的 accessToken) ─────────────────────────

// GetOrCreateTempAccount 获取或创建外部 token 对应的临时账号。
// 命中已有临时账号:刷新 LastUsed;未命中:用 fingerprint 创建新的,绑代理、随机 UA、InitClient。
// 池里池外都查(为了去重)。
func (p *Pool) GetOrCreateTempAccount(token, userAgent string, proxyURL string) *Account {
	tokenHash := tokenHashOf(token)

	// 1) 查已有 (命中的同时刷新 LastUsed)
	p.tempMu.RLock()
	if existing, ok := p.temporary[tokenHash]; ok {
		p.tempMu.RUnlock()
		existing.LastUsed = time.Now()
		return existing
	}
	p.tempMu.RUnlock()

	// 2) 没命中,创建一个
	fp := BrowserFingerprint{
		OaiDeviceID:         uuid.NewString(),
		OaiSessionID:        uuid.NewString(),
		UserAgent:           userAgent,
		ScreenWidth:         1920,
		ScreenHeight:        1080,
		HardwareConcurrency: 8,
		Platform:            "Win32",
		TLSProfileName:      "chrome_150",
	}
	// 如果请求没传 User-Agent,用默认
	if fp.UserAgent == "" {
		fp.UserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/150.0.0.0 Safari/537.36"
	}
	// 解析 JWT 拿 chatgpt_account_id
	chatGPTID := ExtractChatGPTAccountID(token)

	acct := NewAccount(uuid.NewString(), TypeFree, token)
	acct.ChatGPTAccountID = chatGPTID
	acct.Fingerprint = fp
	acct.Proxy = proxyURL
	acct.Status = StatusActive
	acct.LastUsed = time.Now()
	acct.IsTemporary = true
	// 初始化专属 TLS client
	_ = acct.InitClient()

	// 3) 放入 map (可能有并发竞争,后写覆盖先写,反正同一个 token 效果一样)
	p.tempMu.Lock()
	if existing, ok := p.temporary[tokenHash]; ok {
		p.tempMu.Unlock()
		existing.LastUsed = time.Now()
		return existing
	}
	p.temporary[tokenHash] = acct
	p.tempMu.Unlock()
	return acct
}

// TouchAccount 刷新临时账号的 LastUsed,让 GC 知道它在用。
func (p *Pool) TouchAccount(account *Account) {
	if account == nil {
		return
	}
	account.LastUsed = time.Now()
}

// StartTempAccountGC 启动后台 GC,清理 idle 超过 idleTimeout 的临时账号。
// 返回 stop 函数。
func (p *Pool) StartTempAccountGC(idleTimeout time.Duration, interval time.Duration) func() {
	if idleTimeout <= 0 {
		idleTimeout = 10 * time.Minute
	}
	if interval <= 0 {
		interval = 1 * time.Minute
	}
	stop := make(chan struct{})
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				p.evictIdleTempAccounts(idleTimeout)
			case <-stop:
				return
			}
		}
	}()
	return func() { close(stop) }
}

func (p *Pool) evictIdleTempAccounts(idleTimeout time.Duration) {
	now := time.Now()
	p.tempMu.Lock()
	defer p.tempMu.Unlock()
	for hash, acct := range p.temporary {
		if now.Sub(acct.LastUsed) > idleTimeout {
			delete(p.temporary, hash)
		}
	}
}

// TokenHashOf 暴露给外部包 (handler) 使用
func TokenHashOf(token string) string {
	return tokenHashOf(token)
}

func tokenHashOf(token string) string {
	h := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", h[:16])
}

// ── 号池管理与快照接口 (供 Provider 插件与 Admin UI 使用) ─────────────────

// PoolStats 号池聚合统计指标
type PoolStats struct {
	Total        int   `json:"total"`
	Active       int   `json:"active"`
	Pending      int   `json:"pending"`
	Expired      int   `json:"expired"`
	RateLimited  int   `json:"rate_limited"`
	ImageLimited int   `json:"image_limited"`
	Disabled     int   `json:"disabled"`
	Banned       int   `json:"banned"`
	Standby      int   `json:"standby"`
	FreeCount    int   `json:"free_count"`
	NoAuthCount  int   `json:"noauth_count"`
	PUIDCount    int   `json:"puid_count"`
	TempCount    int   `json:"temp_count"`
	TotalCalls   int64 `json:"total_calls"`
	FailedCalls  int64 `json:"failed_calls"`
}

// AccountSnapshot 账号脱敏视图快照
type AccountSnapshot struct {
	ID                 string   `json:"id"`
	Type               string   `json:"type"`
	Status             string   `json:"status"`
	Email              string   `json:"email"`
	PlanType           string   `json:"plan_type"`
	ChatGPTAccountID   string   `json:"chatgpt_account_id"`
	TeamUserID         string   `json:"team_user_id,omitempty"`
	TokenPreview       string   `json:"token_preview"`
	HasRefreshToken    bool     `json:"has_refresh_token"`
	HasSessionToken    bool     `json:"has_session_token"`
	HasKamiCredentials bool     `json:"has_kami_credentials"`
	HasTOTPSecret      bool     `json:"has_totp_secret"`
	IsTemporary        bool     `json:"is_temporary"`
	ImageLimited       bool     `json:"image_limited"`
	Proxy              string   `json:"proxy"`
	TLSProfile         string   `json:"tls_profile"`
	TotalCalls         int64    `json:"total_calls"`
	FailedCalls        int64    `json:"failed_calls"`
	ExpiresAt          string   `json:"expires_at,omitempty"`
	CooldownUntil      string   `json:"cooldown_until,omitempty"`
	ImageCooldownUntil string   `json:"image_cooldown_until,omitempty"`
	RenewState         string   `json:"renew_state,omitempty"`
	RenewCooldownUntil string   `json:"renew_cooldown_until,omitempty"`
	LastRenewError     string   `json:"last_renew_error,omitempty"`
	ActiveAction       string   `json:"active_action,omitempty"`
	ActionPhase        string   `json:"action_phase,omitempty"`
	ActionPhaseLabel   string   `json:"action_phase_label,omitempty"`
	ActionQueuePos     int      `json:"action_queue_pos,omitempty"`
	ActionTaskID       string   `json:"action_task_id,omitempty"`
	ActionStartedAt    string   `json:"action_started_at,omitempty"`
	LastAction         string   `json:"last_action,omitempty"`
	LastActionOK       bool     `json:"last_action_ok,omitempty"`
	LastActionSummary  string   `json:"last_action_summary,omitempty"`
	LastActionAt       string   `json:"last_action_at,omitempty"`
	RemainingDays      float64  `json:"remaining_days"`
	LastUsed           string   `json:"last_used,omitempty"`
	LastChecked        string   `json:"last_checked,omitempty"`
	LastError          string   `json:"last_error,omitempty"`
	CreatedAt          string   `json:"created_at"`
	Capabilities       []string `json:"capabilities"`
	Models             []string `json:"models,omitempty"`
	ModelsCount        int      `json:"models_count,omitempty"`
}

func maskToken(t string) string {
	if len(t) <= 16 {
		return t
	}
	return t[:8] + "..." + t[len(t)-6:]
}

func capabilitiesForType(t AccountType) []string {
	all := []Capability{
		CapChat, CapResponses, CapToolCalling, CapImageGenerate,
		CapImageEdit, CapImageVariation, CapTTS, CapTranscribe, CapFileUpload, CapWebSocket,
	}
	var out []string
	for _, c := range all {
		if t.Satisfies(c) {
			out = append(out, c.Name)
		}
	}
	return out
}

func toSnapshot(a *Account) AccountSnapshot {
	a.SyncJWTMetadata()
	now := time.Now()
	snap := AccountSnapshot{
		ID:                 a.ID,
		Type:               a.Type.String(),
		Status:             a.Status.String(),
		Email:              a.Email,
		PlanType:           a.PlanType,
		ChatGPTAccountID:   a.ChatGPTAccountID,
		TeamUserID:         a.TeamUserID,
		TokenPreview:       maskToken(a.Token),
		HasRefreshToken:    a.RefreshToken != "",
		HasSessionToken:    a.SessionToken != "",
		HasKamiCredentials: a.Password != "",
		HasTOTPSecret:      a.TOTPSecret != "",
		IsTemporary:        a.IsTemporary,
		ImageLimited:       a.IsImageInCooldown(now),
		Proxy:              a.Proxy,
		TLSProfile:         a.Fingerprint.TLSProfileName,
		TotalCalls:         a.TotalCalls,
		FailedCalls:        a.FailedCalls,
		RenewState:         a.RenewState,
		LastRenewError:     a.LastRenewError,
		ActiveAction:       a.ActiveAction,
		ActionPhase:        a.ActionPhase,
		ActionPhaseLabel:   a.ActionPhaseLabel,
		ActionQueuePos:     a.ActionQueuePos,
		ActionTaskID:       a.ActionTaskID,
		LastAction:         a.LastAction,
		LastActionOK:       a.LastActionOK,
		LastActionSummary:  a.LastActionSummary,
		LastError:          a.LastError,
		CreatedAt:          a.CreatedAt.Format(time.RFC3339),
		Capabilities:       capabilitiesForType(a.Type),
	}
	if len(a.Models) > 0 {
		snap.Models = append([]string(nil), a.Models...)
		snap.ModelsCount = len(a.Models)
	}
	if !a.ActionStartedAt.IsZero() {
		snap.ActionStartedAt = a.ActionStartedAt.Format(time.RFC3339)
	}
	if !a.LastActionAt.IsZero() {
		snap.LastActionAt = a.LastActionAt.Format(time.RFC3339)
	}
	if !a.ExpiresAt.IsZero() {
		snap.ExpiresAt = a.ExpiresAt.Format(time.RFC3339)
		days := time.Until(a.ExpiresAt).Hours() / 24.0
		snap.RemainingDays = math.Round(days*10) / 10
	}
	if a.IsInCooldown(now) {
		snap.CooldownUntil = a.CooldownUntil.Format(time.RFC3339)
	}
	if a.IsImageInCooldown(now) {
		snap.ImageCooldownUntil = a.ImageCooldownUntil.Format(time.RFC3339)
	}
	if !a.RenewCooldownUntil.IsZero() && now.Before(a.RenewCooldownUntil) {
		snap.RenewCooldownUntil = a.RenewCooldownUntil.Format(time.RFC3339)
	}
	if !a.LastUsed.IsZero() {
		snap.LastUsed = a.LastUsed.Format(time.RFC3339)
	}
	if !a.LastChecked.IsZero() {
		snap.LastChecked = a.LastChecked.Format(time.RFC3339)
	}
	return snap
}

// Stats 统计号池各状态与各类型数量
func (p *Pool) Stats() PoolStats {
	p.mu.Lock()
	var stats PoolStats
	stats.NoAuthCount = len(p.noauth)
	stats.FreeCount = len(p.free)
	stats.PUIDCount = len(p.puid)
	now := time.Now()

	accumulate := func(a *Account) {
		stats.Total++
		stats.TotalCalls += a.TotalCalls
		stats.FailedCalls += a.FailedCalls
		if a.IsImageInCooldown(now) {
			stats.ImageLimited++
		}
		switch a.Status {
		case StatusActive:
			stats.Active++
		case StatusPending:
			stats.Pending++
		case StatusExpired:
			stats.Expired++
		case StatusRateLimited:
			stats.RateLimited++
		case StatusDisabled:
			stats.Disabled++
		case StatusBanned:
			stats.Banned++
		case StatusStandby:
			stats.Standby++
		}
	}

	for _, list := range [][]*Account{p.noauth, p.free, p.puid} {
		for _, a := range list {
			accumulate(a)
		}
	}
	p.mu.Unlock()

	p.tempMu.RLock()
	stats.TempCount = len(p.temporary)
	p.tempMu.RUnlock()

	return stats
}

// ListSnapshots 按条件过滤返回账号快照列表
func (p *Pool) ListSnapshots(typeFilter, statusFilter, search string) []AccountSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()

	typeFilter = strings.ToLower(strings.TrimSpace(typeFilter))
	statusFilter = strings.ToLower(strings.TrimSpace(statusFilter))
	search = strings.ToLower(strings.TrimSpace(search))

	var out []AccountSnapshot
	// 优先展示 free 和 puid 登录账号，再展示 noauth
	for _, list := range [][]*Account{p.free, p.puid, p.noauth} {
		for _, a := range list {
			snap := toSnapshot(a)
			if typeFilter != "" && typeFilter != "all" && snap.Type != typeFilter {
				continue
			}
			if statusFilter == "renewing" {
				if snap.ActiveAction == "" && snap.RenewState != "queued" && snap.RenewState != "running" {
					continue
				}
			} else if statusFilter != "" && statusFilter != "all" && snap.Status != statusFilter {
				continue
			}
			if search != "" {
				haystack := strings.ToLower(snap.ID + " " + snap.Email + " " + snap.ChatGPTAccountID + " " + snap.Proxy + " " + snap.TokenPreview)
				if !strings.Contains(haystack, search) {
					continue
				}
			}
			out = append(out, snap)
		}
	}
	return out
}

func (p *Pool) findAccountLocked(id string) *Account {
	if id == "" {
		return nil
	}
	for _, list := range [][]*Account{p.free, p.puid, p.noauth} {
		for _, a := range list {
			if a.ID == id || strings.HasPrefix(a.ID, id) {
				return a
			}
		}
	}
	return nil
}

// GetByID 根据账号 ID（或前缀）查找账号
func (p *Pool) GetByID(id string) *Account {
	if p == nil || id == "" {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.findAccountLocked(id)
}

// SetStatus 修改指定账号的状态
func (p *Pool) SetStatus(id string, status AccountStatus) bool {
	if p == nil || id == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if a := p.findAccountLocked(id); a != nil {
		a.Status = status
		return true
	}
	return false
}

// SetAccountRenewState 线程安全地更新账号续期状态标记。
func (p *Pool) SetAccountRenewState(id, state string) {
	if p == nil || id == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if a := p.findAccountLocked(id); a != nil {
		a.RenewState = state
	}
}

// SetAccountActionState 更新账号实时操作阶段与排队序号，供前端列表刷新/轮询即时回显。
func (p *Pool) SetAccountActionState(id, activeAction, phase, phaseLabel, taskID string, queuePos int, startedAt time.Time) {
	if p == nil || id == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	a := p.findAccountLocked(id)
	if a == nil {
		return
	}
	a.ActiveAction = activeAction
	a.ActionPhase = phase
	a.ActionPhaseLabel = phaseLabel
	a.ActionTaskID = taskID
	a.ActionQueuePos = queuePos
	if !startedAt.IsZero() {
		a.ActionStartedAt = startedAt
	} else if a.ActionStartedAt.IsZero() {
		a.ActionStartedAt = time.Now()
	}
}

func clearAccountActiveActionLocked(a *Account) {
	a.ActiveAction = ""
	a.ActionPhase = ""
	a.ActionPhaseLabel = ""
	a.ActionQueuePos = 0
	a.ActionTaskID = ""
	a.ActionStartedAt = time.Time{}
}

// FinishAccountAction 结束账号当前操作，清空运行态并写入最近一次操作结果摘要。
func (p *Pool) FinishAccountAction(id, action string, ok bool, summary string) {
	if p == nil || id == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	a := p.findAccountLocked(id)
	if a == nil {
		return
	}
	clearAccountActiveActionLocked(a)
	a.LastAction = action
	a.LastActionOK = ok
	a.LastActionSummary = summary
	a.LastActionAt = time.Now()
}

// ClearAccountQueuedAction 取消排队态账号的运行标记，并在需要时记录取消摘要。
func (p *Pool) ClearAccountQueuedAction(id, summary string) {
	if p == nil || id == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	a := p.findAccountLocked(id)
	if a == nil {
		return
	}
	prevAction := a.ActiveAction
	clearAccountActiveActionLocked(a)
	if a.RenewState == "queued" {
		a.RenewState = ""
	}
	if summary != "" {
		if prevAction != "" {
			a.LastAction = prevAction
		}
		a.LastActionOK = false
		a.LastActionSummary = summary
		a.LastActionAt = time.Now()
	}
}

// RemoveByID 从号池移除指定账号
func (p *Pool) RemoveByID(id string) (*Account, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, slicePtr := range []*[]*Account{&p.free, &p.puid, &p.noauth} {
		list := *slicePtr
		for i, a := range list {
			if a.ID == id || strings.HasPrefix(a.ID, id) {
				*slicePtr = append(list[:i], list[i+1:]...)
				return a, true
			}
		}
	}
	return nil, false
}

// HasChatGPTAccountID 检查是否已存在相同的 ChatGPTAccountID 或 Token
func (p *Pool) HasChatGPTAccountID(chatGPTID, rawToken string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, list := range [][]*Account{p.free, p.puid, p.noauth} {
		for _, a := range list {
			if chatGPTID != "" && a.ChatGPTAccountID == chatGPTID {
				return true
			}
			if rawToken != "" && a.Token == rawToken {
				return true
			}
		}
	}
	return false
}

// ExportAccessTokens 导出当前池内所有非临时 free/puid 账号的 access_token 行
func (p *Pool) ExportAccessTokens() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var lines []string
	seen := make(map[string]bool)
	for _, list := range [][]*Account{p.free, p.puid} {
		for _, a := range list {
			if a.IsTemporary || a.Token == "" || a.RefreshToken != "" || a.SessionToken != "" {
				continue
			}
			if a.Status == StatusBanned || a.Status == StatusDisabled {
				continue
			}
			line := a.Token
			if a.TeamUserID != "" {
				line += ":" + a.TeamUserID
			}
			if !seen[line] {
				seen[line] = true
				lines = append(lines, line)
			}
		}
	}
	return lines
}

// FindByEmail 根据邮箱查找非临时账号（忽略大小写）
func (p *Pool) FindByEmail(email string) *Account {
	target := strings.ToLower(strings.TrimSpace(email))
	if target == "" {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, list := range [][]*Account{p.free, p.puid} {
		for _, a := range list {
			if strings.ToLower(strings.TrimSpace(a.Email)) == target {
				return a
			}
		}
	}
	return nil
}

// HasEmail 检查号池中是否已存在指定邮箱的账号
func (p *Pool) HasEmail(email string) bool {
	return p.FindByEmail(email) != nil
}

// ListKamiCandidates 返回所有配置了卡密凭据（密码+2FA）的账号，用于批量登录提取/续期。
// 若 onlyNeedLogin 为 true，仅返回 Pending/Expired/无 Token 的账号。
func (p *Pool) ListKamiCandidates(onlyNeedLogin bool) []*Account {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []*Account
	for _, list := range [][]*Account{p.free, p.puid} {
		for _, a := range list {
			if a.Password == "" || a.TOTPSecret == "" {
				continue
			}
			if onlyNeedLogin && a.Status == StatusActive && a.Token != "" {
				continue
			}
			out = append(out, a)
		}
	}
	return out
}

// ExportKamiAccounts 导出所有包含卡密凭据的非临时账号
func (p *Pool) ExportKamiAccounts() []KamiAccount {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []KamiAccount
	seen := make(map[string]bool)
	for _, list := range [][]*Account{p.free, p.puid} {
		for _, a := range list {
			if a.IsTemporary || a.Email == "" || a.Password == "" || a.TOTPSecret == "" {
				continue
			}
			key := strings.ToLower(strings.TrimSpace(a.Email))
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, KamiAccount{
				Email:      a.Email,
				Password:   a.Password,
				TOTPSecret: a.TOTPSecret,
			})
		}
	}
	return out
}

// RecoverCooldowns 将冷却期已届满的账号恢复为可用状态，并返回恢复计数。
func (p *Pool) RecoverCooldowns() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	recovered := 0
	for _, list := range [][]*Account{p.noauth, p.free, p.puid} {
		for _, a := range list {
			if !a.ImageCooldownUntil.IsZero() && !a.IsImageInCooldown(now) {
				a.ImageCooldownUntil = time.Time{}
			}
			if a.Status == StatusRateLimited && !a.IsInCooldown(now) {
				a.Status = StatusActive
				a.CooldownUntil = time.Time{}
				a.LastError = ""
				recovered++
			}
		}
	}
	return recovered
}

// NeedsProactiveRenew 返回需要提前/自动续期的卡密账号（过期、缺 Token 或距离到期不足 within）。
// 当账号类型启用了热池策略时，严格遵守热池容量：
// 1) 仅提前续期热池中已激活且即将过期的账号，避免掉出热池；
// 2) 对于失效或缺 Token 的卡密账号（StatusExpired / StatusPending），仅在当前健康可用数低于目标活跃数时才补位续期，且单次最多补足 deficit（受 ActivateBatch 限制）；
// 3) 待命账号（StatusStandby）由 Rebalance 控制激活，不直接参与后台续期扫描。
func (p *Pool) NeedsProactiveRenew(within time.Duration) []*Account {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	var out []*Account

	for _, t := range []AccountType{TypeFree, TypePUID} {
		list := p.sliceFor(t)
		if list == nil {
			continue
		}
		pol, hasPol := p.policies[t]
		polEnabled := hasPol && pol.Enabled()

		var activeDue []*Account
		var inactiveDue []*Account
		healthyCount := 0

		for _, a := range *list {
			if a.IsTemporary || a.Password == "" || a.TOTPSecret == "" {
				continue
			}
			// 若具备 refresh/session 凭据，则走常规刷新链路
			if a.RefreshToken != "" || a.SessionToken != "" {
				continue
			}
			if a.Status == StatusDisabled || a.Status == StatusBanned || a.Status == StatusStandby {
				continue
			}
			if a.RenewState == "queued" || a.RenewState == "running" {
				continue
			}
			if !a.RenewCooldownUntil.IsZero() && now.Before(a.RenewCooldownUntil) {
				continue
			}

			if a.isHealthy(now) {
				healthyCount++
			}

			due := a.Status == StatusExpired || a.Token == ""
			isExpiringActive := false
			if !due && within > 0 && !a.ExpiresAt.IsZero() && a.ExpiresAt.Sub(now) < within {
				due = true
				if a.Status == StatusActive {
					isExpiringActive = true
				}
			}

			if due {
				if isExpiringActive {
					activeDue = append(activeDue, a)
				} else {
					inactiveDue = append(inactiveDue, a)
				}
			}
		}

		if !polEnabled {
			out = append(out, activeDue...)
			out = append(out, inactiveDue...)
			continue
		}

		out = append(out, activeDue...)

		deficit := pol.ActiveTarget - healthyCount
		if deficit > 0 {
			batch := pol.ActivateBatch
			if batch <= 0 {
				batch = 5
			}
			if deficit > batch {
				deficit = batch
			}
			for i := 0; i < len(inactiveDue) && i < deficit; i++ {
				out = append(out, inactiveDue[i])
			}
		}
	}
	return out
}

// BoundProxyCounts 统计每个代理 URL 当前绑定的账号数量（含临时账号）。
func (p *Pool) BoundProxyCounts() map[string]int {
	counts := make(map[string]int)
	p.mu.Lock()
	for _, list := range [][]*Account{p.noauth, p.free, p.puid} {
		for _, a := range list {
			if a.Proxy != "" {
				counts[a.Proxy]++
			}
		}
	}
	p.mu.Unlock()

	p.tempMu.RLock()
	for _, a := range p.temporary {
		if a != nil && a.Proxy != "" {
			counts[a.Proxy]++
		}
	}
	p.tempMu.RUnlock()
	return counts
}

// RebindAccountProxy 为账号切换代理并重建 TLS 客户端。
func (p *Pool) RebindAccountProxy(acct *Account, newProxy string) error {
	if acct == nil {
		return nil
	}
	if strings.TrimSpace(newProxy) != "" {
		parsed, err := url.Parse(strings.TrimSpace(newProxy))
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("invalid proxy url: %q", newProxy)
		}
	}
	candidate := *acct
	candidate.Proxy = newProxy
	if err := candidate.InitClient(); err != nil {
		return err
	}
	p.mu.Lock()
	acct.Proxy = candidate.Proxy
	acct.Client = candidate.Client
	p.mu.Unlock()
	return nil
}

// ReconcileProxies 遍历号池账号，当绑定代理满足 shouldRebind 时迁移至 pickHealthy 返回的新代理。
func (p *Pool) ReconcileProxies(shouldRebind func(proxyURL string) bool, pickHealthy func(exclude map[string]bool) string) int {
	if shouldRebind == nil || pickHealthy == nil {
		return 0
	}
	p.mu.Lock()
	var targets []*Account
	for _, list := range [][]*Account{p.free, p.puid, p.noauth} {
		for _, a := range list {
			if a.Proxy != "" && shouldRebind(a.Proxy) {
				targets = append(targets, a)
			}
		}
	}
	p.mu.Unlock()

	migrated := 0
	for _, a := range targets {
		oldProxy := a.Proxy
		next := pickHealthy(map[string]bool{oldProxy: true})
		if next == "" || next == oldProxy {
			continue
		}
		if err := p.RebindAccountProxy(a, next); err == nil {
			migrated++
		}
	}
	return migrated
}

// AllPersistentAccounts 返回所有非临时 Free/PUID 账号（用于导出等批处理）。
func (p *Pool) AllPersistentAccounts() []*Account {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]*Account, 0, len(p.free)+len(p.puid))
	for _, list := range [][]*Account{p.free, p.puid} {
		for _, a := range list {
			if !a.IsTemporary {
				out = append(out, a)
			}
		}
	}
	return out
}

// RecordAccountModels 记录指定账号探测到的上游模型，并合并更新池级模型目录缓存。
func normalizeModelIDs(models []string) []string {
	seen := make(map[string]bool, len(models))
	clean := make([]string, 0, len(models))
	for _, raw := range models {
		slug := strings.TrimSpace(raw)
		if slug == "" || seen[slug] {
			continue
		}
		seen[slug] = true
		clean = append(clean, slug)
	}
	if len(clean) == 0 {
		return nil
	}
	return clean
}

func (p *Pool) RecordAccountModels(acctID string, slugs []string) {
	clean := normalizeModelIDs(slugs)
	if p == nil || len(clean) == 0 {
		return
	}
	p.mu.Lock()
	found := false
	if acct := p.findAccountLocked(acctID); acct != nil {
		acct.Models = append([]string(nil), clean...)
		found = true
	}
	p.providerModels["chatgpt"] = clean
	p.rebuildModelsLocked()
	p.mu.Unlock()

	if !found && acctID != "" {
		p.tempMu.Lock()
		for _, a := range p.temporary {
			if a != nil && (a.ID == acctID || strings.HasPrefix(a.ID, acctID)) {
				a.Models = append([]string(nil), clean...)
				break
			}
		}
		p.tempMu.Unlock()
	}
}

// RecordProviderModelsAt 记录某个供应商实时模型目录，并重建全站合并缓存。
func (p *Pool) RecordProviderModelsAt(provider string, models []string, cachedAt time.Time) {
	clean := normalizeModelIDs(models)
	if p == nil || provider == "" || len(clean) == 0 {
		return
	}
	p.mu.Lock()
	if p.providerModels == nil {
		p.providerModels = make(map[string][]string)
	}
	if p.providerModelsCachedAt == nil {
		p.providerModelsCachedAt = make(map[string]time.Time)
	}
	p.providerModels[provider] = clean
	if cachedAt.After(p.providerModelsCachedAt[provider]) {
		p.providerModelsCachedAt[provider] = cachedAt
	}
	p.rebuildModelsLocked()
	p.mu.Unlock()
}

func (p *Pool) rebuildModelsLocked() {
	seen := make(map[string]bool)
	merged := make([]string, 0)
	providers := make([]string, 0, len(p.providerModels))
	for provider := range p.providerModels {
		providers = append(providers, provider)
	}
	sort.Strings(providers)
	for _, provider := range providers {
		for _, model := range p.providerModels[provider] {
			if model == "" || seen[model] {
				continue
			}
			seen[model] = true
			merged = append(merged, model)
		}
	}
	p.cachedModels = merged
	p.modelsCachedAt = time.Now()
}

// CachedModels 返回池内已缓存的上游模型列表及其最近更新时间。
func (p *Pool) CachedModels() ([]string, time.Time) {
	if p == nil {
		return nil, time.Time{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.cachedModels) == 0 {
		return nil, p.modelsCachedAt
	}
	out := make([]string, len(p.cachedModels))
	copy(out, p.cachedModels)
	return out, p.modelsCachedAt
}

// ProviderModels 返回指定供应商的实时模型缓存。
func (p *Pool) ProviderModels(provider string) ([]string, time.Time) {
	if p == nil || provider == "" {
		return nil, time.Time{}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	models := p.providerModels[provider]
	if len(models) == 0 {
		return nil, p.providerModelsCachedAt[provider]
	}
	out := make([]string, len(models))
	copy(out, models)
	return out, p.providerModelsCachedAt[provider]
}

// SyncPersistentAccounts 在后台拉取云端账号变更后原位热刷新内存号池。
// 对已有账号保留指针地址（保护活跃 WSSActor 与运行中操作），仅在代理变更或新账号加入时创建 TLS Client；
// 对云端已删除的持久账号从池中剔除（不影响 IsTemporary 临时账号）。
func (p *Pool) SyncPersistentAccounts(incoming []*Account) (added, updated, removed int) {
	if p == nil {
		return 0, 0, 0
	}

	incomingByID := make(map[string]*Account, len(incoming))
	for _, inc := range incoming {
		if inc == nil || strings.TrimSpace(inc.ID) == "" || inc.IsTemporary {
			continue
		}
		incomingByID[inc.ID] = inc
	}

	p.mu.Lock()
	existingByID := make(map[string]*Account)
	for _, list := range [][]*Account{p.noauth, p.free, p.puid} {
		for _, a := range list {
			if a != nil && !a.IsTemporary && a.ID != "" {
				existingByID[a.ID] = a
			}
		}
	}

	nextNoAuth := make([]*Account, 0, len(p.noauth))
	nextFree := make([]*Account, 0, len(p.free))
	nextPUID := make([]*Account, 0, len(p.puid))

	appendToType := func(a *Account) {
		switch a.Type {
		case TypeNoAuth:
			nextNoAuth = append(nextNoAuth, a)
		case TypeFree:
			nextFree = append(nextFree, a)
		case TypePUID:
			nextPUID = append(nextPUID, a)
		}
	}

	// 1. 保留并原位更新已有持久账号，或剔除云端已删除账号
	for _, list := range [][]*Account{p.noauth, p.free, p.puid} {
		for _, cur := range list {
			if cur == nil {
				continue
			}
			if cur.IsTemporary {
				appendToType(cur)
				continue
			}
			inc, keep := incomingByID[cur.ID]
			if !keep {
				removed++
				continue
			}
			// 若账号正处于登录/续期/测活任务中，暂不覆写凭据与状态，避免打断进行中任务
			if cur.ActiveAction == "" {
				changed := cur.Type != inc.Type ||
					cur.Token != inc.Token ||
					cur.RefreshToken != inc.RefreshToken ||
					cur.SessionToken != inc.SessionToken ||
					cur.PUID != inc.PUID ||
					cur.TeamUserID != inc.TeamUserID ||
					cur.ChatGPTAccountID != inc.ChatGPTAccountID ||
					cur.PlanType != inc.PlanType ||
					cur.Email != inc.Email ||
					cur.Password != inc.Password ||
					cur.TOTPSecret != inc.TOTPSecret ||
					cur.Proxy != inc.Proxy ||
					cur.Status != inc.Status ||
					cur.LastError != inc.LastError ||
					!cur.ExpiresAt.Equal(inc.ExpiresAt) ||
					!cur.CooldownUntil.Equal(inc.CooldownUntil) ||
					!cur.ImageCooldownUntil.Equal(inc.ImageCooldownUntil)

				proxyChanged := cur.Proxy != inc.Proxy
				cur.Type = inc.Type
				cur.Token = inc.Token
				cur.RefreshToken = inc.RefreshToken
				cur.SessionToken = inc.SessionToken
				cur.PUID = inc.PUID
				cur.TeamUserID = inc.TeamUserID
				cur.ChatGPTAccountID = inc.ChatGPTAccountID
				cur.PlanType = inc.PlanType
				cur.Email = inc.Email
				cur.Password = inc.Password
				cur.TOTPSecret = inc.TOTPSecret
				cur.Proxy = inc.Proxy
				cur.Status = inc.Status
				cur.LastError = inc.LastError
				cur.ExpiresAt = inc.ExpiresAt
				cur.CooldownUntil = inc.CooldownUntil
				cur.ImageCooldownUntil = inc.ImageCooldownUntil
				if inc.TotalCalls > cur.TotalCalls {
					cur.TotalCalls = inc.TotalCalls
				}
				if inc.FailedCalls > cur.FailedCalls {
					cur.FailedCalls = inc.FailedCalls
				}
				if inc.LastUsed.After(cur.LastUsed) {
					cur.LastUsed = inc.LastUsed
				}
				if (proxyChanged || cur.Client == nil) && cur.Status != StatusStandby {
					_ = cur.InitClient()
				}
				if changed {
					updated++
				}
			}
			appendToType(cur)
		}
	}

	// 2. 插入新增持久账号（保持 incoming 顺序）
	for _, inc := range incoming {
		if inc == nil || strings.TrimSpace(inc.ID) == "" || inc.IsTemporary {
			continue
		}
		if _, exists := existingByID[inc.ID]; exists {
			continue
		}
		if inc.Status != StatusStandby && inc.Client == nil {
			_ = inc.InitClient()
		}
		appendToType(inc)
		added++
	}

	p.noauth = nextNoAuth
	p.free = nextFree
	p.puid = nextPUID
	p.cursors = [3]int{}
	p.mu.Unlock()

	if added > 0 || updated > 0 || removed > 0 {
		p.Rebalance()
	}
	return added, updated, removed
}
