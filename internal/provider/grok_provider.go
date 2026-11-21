package provider

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"ai-web-free/httpclient"
	"ai-web-free/httpclient/bogdanfinn"
	"ai-web-free/internal/accounts"
	"ai-web-free/internal/db"
	"ai-web-free/internal/proxy"
	officialtypes "ai-web-free/typings/official"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const grokBrowserUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/134.0.0.0 Safari/537.36"

var (
	ErrNoGrokAccountAvailable = errors.New("no active grok account available in pool")
)

type grokAccount struct {
	ID            string                 `json:"id"`
	Email         string                 `json:"email"`
	Password      string                 `json:"password,omitempty"`
	TOTPSecret    string                 `json:"totp_secret,omitempty"`
	LegacyToken   string                 `json:"legacy_token,omitempty"`
	Cookie        string                 `json:"cookie"`
	Proxy         string                 `json:"proxy"`
	Status        accounts.AccountStatus `json:"status"`
	CreatedAt     time.Time              `json:"created_at"`
	LastUsed      time.Time              `json:"last_used"`
	CooldownUntil time.Time              `json:"cooldown_until"`
	ActivatedAt   time.Time              `json:"activated_at"`
	TotalCalls    int64                  `json:"total_calls"`
	FailedCalls   int64                  `json:"failed_calls"`
	LastError     string                 `json:"last_error"`

	ActiveAction      string    `json:"active_action,omitempty"`
	ActionPhase       string    `json:"action_phase,omitempty"`
	ActionPhaseLabel  string    `json:"action_phase_label,omitempty"`
	ActionQueuePos    int       `json:"action_queue_pos,omitempty"`
	ActionTaskID      string    `json:"action_task_id,omitempty"`
	ActionStartedAt   time.Time `json:"action_started_at,omitempty"`
	LastAction        string    `json:"last_action,omitempty"`
	LastActionOK      bool      `json:"last_action_ok,omitempty"`
	LastActionSummary string    `json:"last_action_summary,omitempty"`
	LastActionAt      time.Time `json:"last_action_at,omitempty"`
}

func (a *grokAccount) IsInCooldown(now time.Time) bool {
	return !a.CooldownUntil.IsZero() && now.Before(a.CooldownUntil)
}

func (a *grokAccount) occupiesSlot() bool {
	return a.Status == accounts.StatusActive || a.Status == accounts.StatusPending || a.Status == accounts.StatusRateLimited
}

// GrokProvider xAI Grok.com / X Web 免费号池插件
type GrokProvider struct {
	mu           sync.RWMutex
	baseURL      string
	assetBaseURL string
	accounts     []*grokAccount
	idx          int
	policy       accounts.PoolPolicy
	lastRotate   time.Time
	proxyPool    *proxy.Pool
	persistFile  string

	grokLoginMu        sync.Mutex
	grokLoginTasks     map[string]*KamiLoginTask
	grokLoginTaskOrder []string
	grokLoginRunning   string
	grokLoginQueue     []grokLoginQueueItem
	grokLoginFn        func(context.Context, GrokCardAccount, GrokLoginOptions) (string, error)
	grokLoginCooldown  time.Duration

	grokCredentialLLMFn grokCredentialLLMFn

	grokCredentialPatternMu sync.RWMutex
	grokCredentialPatternDB *gorm.DB

	modelMu        sync.RWMutex
	cachedModels   []string
	modelModes     map[string]string
	modelsCachedAt time.Time
	defaultModel   string
}

// NewGrokProvider 创建 Grok 供应商插件
func NewGrokProvider(pPool *proxy.Pool, baseURL, persistFile string, policy accounts.PoolPolicy) *GrokProvider {
	if baseURL == "" {
		baseURL = "https://grok.com"
	}
	p := &GrokProvider{
		baseURL:           baseURL,
		assetBaseURL:      "https://assets.grok.com",
		proxyPool:         pPool,
		persistFile:       persistFile,
		policy:            policy,
		modelModes:        make(map[string]string),
		grokLoginTasks:    make(map[string]*KamiLoginTask),
		grokLoginCooldown: 10 * time.Second,
		grokLoginFn: func(ctx context.Context, card GrokCardAccount, options GrokLoginOptions) (string, error) {
			results, err := RunGrokCardLogin(ctx, []GrokCardAccount{card}, options)
			if err != nil {
				return "", err
			}
			if len(results) == 0 || !results[0].OK {
				if len(results) > 0 {
					return "", errors.New(results[0].Error)
				}
				return "", errors.New("Grok login returned no result")
			}
			return results[0].Cookie, nil
		},
	}
	p.loadFromDB()
	p.loadFromDisk()
	p.persistToDB()
	p.Rebalance()
	return p
}

// SetAssetBaseURL 设置静态资源下载基准地址（用于单测与反代映射）
func (p *GrokProvider) SetAssetBaseURL(url string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.assetBaseURL = strings.TrimRight(strings.TrimSpace(url), "/")
}

func (p *GrokProvider) loadFromDB() {
	gdb := p.grokCredentialPatternDB
	if gdb == nil {
		gdb = db.GetDB()
	}
	if gdb == nil {
		return
	}
	var records []db.PoolAccountRecord
	if err := gdb.Where("provider = ?", "grok").Order("created_at asc").Find(&records).Error; err != nil || len(records) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	existingCookie := make(map[string]bool, len(p.accounts))
	existingEmail := make(map[string]*grokAccount, len(p.accounts))
	for _, a := range p.accounts {
		if a.Cookie != "" {
			existingCookie[a.Cookie] = true
		}
		if a.Email != "" {
			existingEmail[strings.ToLower(a.Email)] = a
		}
	}

	for _, r := range records {
		recEmail := strings.ToLower(strings.TrimSpace(r.Email))
		if recEmail != "" {
			if matched, ok := existingEmail[recEmail]; ok {
				if matched.Password == "" && r.Password != "" {
					matched.Password = r.Password
				}
				if matched.TOTPSecret == "" && r.TOTPSecret != "" {
					matched.TOTPSecret = r.TOTPSecret
				}
				if matched.Cookie == "" && r.Token != "" {
					matched.Cookie = r.Token
				}
				continue
			}
		}
		if r.Token != "" && existingCookie[r.Token] {
			continue
		}
		acct := &grokAccount{
			ID:          r.ID,
			Email:       r.Email,
			Password:    r.Password,
			TOTPSecret:  r.TOTPSecret,
			LegacyToken: r.SessionToken,
			Cookie:      r.Token,
			Proxy:       r.Proxy,
			CreatedAt:   r.CreatedAt,
			TotalCalls:  r.TotalCalls,
			FailedCalls: r.FailedCalls,
			LastError:   r.LastError,
		}
		if acct.ID == "" {
			acct.ID = uuid.NewString()
		}
		if r.Status == "banned" {
			acct.Status = accounts.StatusBanned
		} else if r.Status == "disabled" {
			acct.Status = accounts.StatusDisabled
		} else if r.Status == "rate_limited" {
			acct.Status = accounts.StatusRateLimited
		} else if r.Status == "standby" {
			acct.Status = accounts.StatusStandby
		} else if r.Status == "expired" || strings.Contains(r.LastError, "401") || strings.Contains(r.LastError, "失效") || strings.Contains(r.LastError, "Invalid credentials") {
			acct.Status = accounts.StatusExpired
		} else {
			acct.Status = accounts.StatusActive
		}
		if r.CooldownUntil != nil {
			acct.CooldownUntil = *r.CooldownUntil
		}
		if r.LastUsed != nil {
			acct.LastUsed = *r.LastUsed
		}
		p.accounts = append(p.accounts, acct)
		if acct.Cookie != "" {
			existingCookie[acct.Cookie] = true
		}
		if acct.Email != "" {
			existingEmail[strings.ToLower(acct.Email)] = acct
		}
	}
}

func (p *GrokProvider) persistToDB() {
	gdb := p.grokCredentialPatternDB
	if gdb == nil {
		gdb = db.GetDB()
	}
	if gdb == nil {
		return
	}
	p.mu.RLock()
	list := make([]*grokAccount, len(p.accounts))
	copy(list, p.accounts)
	p.mu.RUnlock()

	if len(list) == 0 {
		return
	}

	records := make([]db.PoolAccountRecord, 0, len(list))
	now := time.Now()
	for _, a := range list {
		if a == nil {
			continue
		}
		rec := db.PoolAccountRecord{
			ID:           a.ID,
			Provider:     "grok",
			Type:         "free",
			Email:        a.Email,
			Password:     a.Password,
			TOTPSecret:   a.TOTPSecret,
			SessionToken: a.LegacyToken,
			Token:        a.Cookie,
			Proxy:        a.Proxy,
			Status:       a.Status.String(),
			LastError:    a.LastError,
			TotalCalls:   a.TotalCalls,
			FailedCalls:  a.FailedCalls,
			CreatedAt:    a.CreatedAt,
			UpdatedAt:    now,
		}
		if rec.ID == "" {
			rec.ID = uuid.NewString()
			a.ID = rec.ID
		}
		if rec.CreatedAt.IsZero() {
			rec.CreatedAt = now
		}
		if !a.CooldownUntil.IsZero() {
			t := a.CooldownUntil
			rec.CooldownUntil = &t
		}
		if !a.LastUsed.IsZero() {
			t := a.LastUsed
			rec.LastUsed = &t
		}
		records = append(records, rec)
	}

	if len(records) > 0 {
		_ = gdb.Clauses(clause.OnConflict{UpdateAll: true}).CreateInBatches(records, 100).Error
	}
}

func (p *GrokProvider) loadFromDisk() {
	if p.persistFile == "" {
		return
	}
	data, err := os.ReadFile(p.persistFile)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	p.AddAccounts(lines, false)
}

func (p *GrokProvider) persistToDisk() {
	p.persistToDB()
	// 当 SQLite 数据库已连接时，数据库即为唯一事实源，不再写回离线文本文件
	if gdb := p.grokCredentialPatternDB; gdb != nil || db.GetDB() != nil {
		return
	}
	if p.persistFile == "" {
		return
	}
	p.mu.RLock()
	defer p.mu.RUnlock()

	var buf bytes.Buffer
	for _, a := range p.accounts {
		if a.Status == accounts.StatusBanned {
			continue
		}
		if a.Password != "" {
			parts := []string{a.Email, a.Password}
			if a.TOTPSecret != "" {
				parts = append(parts, a.TOTPSecret)
			}
			if a.LegacyToken != "" {
				parts = append(parts, a.LegacyToken)
			}
			if a.Cookie != "" && a.Cookie != fmt.Sprintf("sso=%s; sso-rw=%s", a.LegacyToken, a.LegacyToken) {
				parts = append(parts, a.Cookie)
			}
			buf.WriteString(strings.Join(parts, "|") + "\n")
		} else if a.Email != "" {
			buf.WriteString(fmt.Sprintf("%s----%s\n", a.Email, a.Cookie))
		} else {
			buf.WriteString(fmt.Sprintf("%s\n", a.Cookie))
		}
	}
	_ = os.WriteFile(p.persistFile, buf.Bytes(), 0644)
}

// SetPolicy 动态设置热池策略
func (p *GrokProvider) SetPolicy(pol accounts.PoolPolicy) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.policy = pol
}

// AddAccounts 批量导入 Grok Cookie 或 email|password|totp 卡密凭据。
func (p *GrokProvider) AddAccounts(tokens []string, persist bool) int {
	added, _ := p.addAccounts(tokens, persist)
	return added
}

func (p *GrokProvider) addAccounts(tokens []string, persist bool) (int, []string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	existingCookie := make(map[string]bool, len(p.accounts))
	existingEmail := make(map[string]*grokAccount, len(p.accounts))
	for _, account := range p.accounts {
		existingCookie[account.Cookie] = true
		if account.Email != "" {
			existingEmail[account.Email] = account
		}
	}

	added := 0
	affectedIDs := make([]string, 0, len(tokens))
	now := time.Now()
	for _, raw := range tokens {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}

		email := ""
		cookie := ""
		password := ""
		totpSecret := ""
		legacyToken := ""
		kind, parsedCard, detectErr := DetectGrokCredentialLine(trimmed)
		if detectErr != nil {
			continue
		}
		var card GrokCardAccount
		if kind == "kami" {
			card = parsedCard
			email, password, totpSecret, legacyToken, cookie =
				card.Email, card.Password, card.TOTPSecret, card.LegacyToken, card.OldCookie
		} else {
			if strings.Contains(trimmed, "----") {
				parts := strings.SplitN(trimmed, "----", 2)
				email = strings.TrimSpace(parts[0])
				cookie = strings.TrimSpace(parts[1])
			} else {
				cookie = trimmed
			}
			if isGrokLegacyToken(cookie) {
				legacyToken = cookie
				cookie = fmt.Sprintf("sso=%s; sso-rw=%s", cookie, cookie)
			}
			if cookie == "" || existingCookie[cookie] {
				continue
			}
		}

		if target := existingEmail[email]; email != "" && target != nil {
			credentialsChanged := false
			if password != "" && password != target.Password {
				target.Password = password
				credentialsChanged = true
			}
			if totpSecret != "" && totpSecret != target.TOTPSecret {
				target.TOTPSecret = totpSecret
				credentialsChanged = true
			}
			if legacyToken != "" && legacyToken != target.LegacyToken {
				target.LegacyToken = legacyToken
				credentialsChanged = true
			}
			if cookie != "" && cookie != target.Cookie && !existingCookie[cookie] {
				target.Cookie = cookie
				target.Status = accounts.StatusActive
				target.LastError = ""
				target.ActivatedAt = now
				credentialsChanged = true
			}
			if card.Proxy != "" && card.Proxy != target.Proxy {
				target.Proxy = card.Proxy
				credentialsChanged = true
			}
			if credentialsChanged {
				affectedIDs = append(affectedIDs, target.ID)
			}
			continue
		}
		if cookie != "" && existingCookie[cookie] {
			continue
		}

		accountProxy := card.Proxy
		if accountProxy == "" && p.proxyPool != nil {
			accountProxy = p.proxyPool.Allocate()
		}
		id := fmt.Sprintf("grok-%s", uuid.NewString()[:8])
		if email != "" && strings.Contains(email, "@") {
			id = "grok-" + strings.Split(email, "@")[0]
		}

		status := accounts.StatusActive
		if cookie == "" {
			status = accounts.StatusPending
		}
		account := &grokAccount{
			ID:          id,
			Email:       email,
			Password:    password,
			TOTPSecret:  totpSecret,
			LegacyToken: legacyToken,
			Cookie:      cookie,
			Proxy:       accountProxy,
			Status:      status,
			CreatedAt:   now,
			ActivatedAt: now,
		}
		p.accounts = append(p.accounts, account)
		existingCookie[cookie] = true
		if email != "" {
			existingEmail[email] = account
		}
		added++
		affectedIDs = append(affectedIDs, account.ID)
	}

	if added > 0 {
		p.rebalanceLocked(now)
		if persist {
			go p.persistToDisk()
		}
	}
	return added, affectedIDs
}

// Rebalance 对 Grok 号池执行热池与待命池调谐
func (p *GrokProvider) Rebalance() []accounts.RebalanceResult {
	p.mu.Lock()
	defer p.mu.Unlock()
	return []accounts.RebalanceResult{p.rebalanceLocked(time.Now())}
}

func (p *GrokProvider) rebalanceLocked(now time.Time) accounts.RebalanceResult {
	res := accounts.RebalanceResult{
		Type:   "grok_free",
		Target: p.policy.ActiveTarget,
	}

	if !p.policy.Enabled() || len(p.accounts) == 0 {
		slots := p.countSlotsLocked(now)
		res.Working, res.Healthy, res.Standby = slots[0], slots[1], slots[2]
		return res
	}

	justDemoted := make(map[string]bool)
	demote := func(a *grokAccount) {
		a.Status = accounts.StatusStandby
		justDemoted[a.ID] = true
		res.Demoted++
	}

	// 1. 定时轮换
	if p.policy.RotateInterval > 0 {
		if p.lastRotate.IsZero() {
			p.lastRotate = now
		} else if now.Sub(p.lastRotate) >= p.policy.RotateInterval {
			p.lastRotate = now
			slots := p.countSlotsLocked(now)
			standbyCount := slots[2]
			if standbyCount > 0 {
				cands := p.getDemotableAccountsLocked()
				n := int(math.Ceil(float64(len(cands)) * p.policy.RotateRatio))
				if n > standbyCount {
					n = standbyCount
				}
				for i := 0; i < n && i < len(cands); i++ {
					demote(cands[i])
					res.Rotated++
				}
			}
		}
	}

	// 2. 超额收缩
	slots := p.countSlotsLocked(now)
	working := slots[0]
	limit := p.policy.ActiveTarget
	if p.policy.MaxActive > 0 && p.policy.MaxActive < limit {
		limit = p.policy.MaxActive
	}
	if working > limit {
		for _, a := range p.getDemotableAccountsLocked() {
			if working <= limit {
				break
			}
			demote(a)
			working--
		}
	}

	// 3. 补位
	slots = p.countSlotsLocked(now)
	working, healthy := slots[0], slots[1]
	want := p.policy.ActiveTarget - working
	if d := p.policy.MinHealthy - healthy; d > want {
		want = d
	}
	if p.policy.MaxActive > 0 && want > p.policy.MaxActive-working {
		want = p.policy.MaxActive - working
	}
	if want > p.policy.ActivateBatch {
		want = p.policy.ActivateBatch
	}
	if want > 0 {
		var standbys []*grokAccount
		for _, a := range p.accounts {
			if a.Status == accounts.StatusStandby && !justDemoted[a.ID] {
				standbys = append(standbys, a)
			}
		}
		sort.SliceStable(standbys, func(i, j int) bool {
			if !standbys[i].ActivatedAt.Equal(standbys[j].ActivatedAt) {
				return standbys[i].ActivatedAt.Before(standbys[j].ActivatedAt)
			}
			return standbys[i].FailedCalls < standbys[j].FailedCalls
		})
		for _, a := range standbys {
			if want <= 0 {
				break
			}
			a.Status = accounts.StatusActive
			a.ActivatedAt = now
			a.CooldownUntil = time.Time{}
			res.Activated++
			want--
		}
	}

	finalSlots := p.countSlotsLocked(now)
	res.Working, res.Healthy, res.Standby = finalSlots[0], finalSlots[1], finalSlots[2]
	return res
}

func (p *GrokProvider) countSlotsLocked(now time.Time) [3]int {
	working, healthy, standby := 0, 0, 0
	for _, a := range p.accounts {
		if a.occupiesSlot() {
			working++
		}
		if a.Status == accounts.StatusActive && !a.IsInCooldown(now) {
			healthy++
		}
		if a.Status == accounts.StatusStandby {
			standby++
		}
	}
	return [3]int{working, healthy, standby}
}

func (p *GrokProvider) getDemotableAccountsLocked() []*grokAccount {
	var cands []*grokAccount
	for _, a := range p.accounts {
		if a.Status == accounts.StatusActive || a.Status == accounts.StatusRateLimited {
			cands = append(cands, a)
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		ri, rj := cands[i].Status == accounts.StatusRateLimited, cands[j].Status == accounts.StatusRateLimited
		if ri != rj {
			return ri
		}
		if !cands[i].ActivatedAt.Equal(cands[j].ActivatedAt) {
			return cands[i].ActivatedAt.Before(cands[j].ActivatedAt)
		}
		return cands[i].FailedCalls > cands[j].FailedCalls
	})
	return cands
}

func (p *GrokProvider) nextAccount() (*grokAccount, int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	n := len(p.accounts)
	if n == 0 {
		return nil, -1, ErrNoGrokAccountAvailable
	}

	now := time.Now()
	for i := 0; i < n; i++ {
		candidateIdx := (p.idx + i) % n
		a := p.accounts[candidateIdx]
		if a.Status == accounts.StatusRateLimited && !a.IsInCooldown(now) {
			a.Status = accounts.StatusActive
			a.CooldownUntil = time.Time{}
		}
		if a.Status == accounts.StatusActive && !a.IsInCooldown(now) {
			p.idx = (candidateIdx + 1) % n
			a.TotalCalls++
			a.LastUsed = now
			return a, candidateIdx, nil
		}
	}
	return nil, -1, ErrNoGrokAccountAvailable
}

// CachedModels 返回最近一次实时拉取的 Grok 模型目录。
func (p *GrokProvider) CachedModels() []string {
	p.modelMu.RLock()
	defer p.modelMu.RUnlock()
	if len(p.cachedModels) == 0 {
		return nil
	}
	out := make([]string, len(p.cachedModels))
	copy(out, p.cachedModels)
	return out
}

// ModelsCachedAt 返回 Grok 模型目录缓存时间。
func (p *GrokProvider) ModelsCachedAt() time.Time {
	p.modelMu.RLock()
	defer p.modelMu.RUnlock()
	return p.modelsCachedAt
}

func (p *GrokProvider) defaultGrokModel() string {
	p.modelMu.RLock()
	defer p.modelMu.RUnlock()
	return p.defaultModel
}

type grokModelsResponse struct {
	Models []struct {
		ModelID   string `json:"modelId"`
		ModelMode string `json:"modelMode,omitempty"`
	} `json:"models"`
	DefaultFreeModel string `json:"defaultFreeModel"`
	DefaultAnonModel string `json:"defaultAnonModel"`
}

func (p *GrokProvider) modelCatalogTarget() (string, string) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	now := time.Now()
	for _, account := range p.accounts {
		if account.Status == accounts.StatusActive && !account.IsInCooldown(now) {
			return account.Cookie, account.Proxy
		}
	}
	if len(p.accounts) > 0 {
		return p.accounts[0].Cookie, p.accounts[0].Proxy
	}
	return "", ""
}

// RefreshModels 调用 Grok Web 实时模型目录端点并更新本地缓存。
func (p *GrokProvider) RefreshModels(ctx context.Context) ([]string, error) {
	cookie, proxyURL := p.modelCatalogTarget()
	body := []byte(`{"locale":"en-US"}`)
	resp, _, err := p.doRequestWithProxyFallback(ctx, proxyURL, func() (*http.Request, error) {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/rest/models", bytes.NewReader(body))
		if reqErr != nil {
			return nil, reqErr
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", grokBrowserUserAgent)
		req.Header.Set("Origin", p.baseURL)
		req.Header.Set("Referer", p.baseURL+"/")
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		return req, nil
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("grok models returned HTTP %d", resp.StatusCode)
	}
	var payload grokModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}

	seen := make(map[string]bool)
	models := make([]string, 0, len(payload.Models))
	for _, item := range payload.Models {
		model := strings.TrimSpace(item.ModelID)
		if model == "" || seen[model] {
			continue
		}
		seen[model] = true
		models = append(models, model)
	}
	if len(models) == 0 {
		return nil, errors.New("grok models returned an empty catalog")
	}
	defaultModel := payload.DefaultFreeModel
	if defaultModel == "" {
		defaultModel = payload.DefaultAnonModel
	}
	if defaultModel == "" {
		defaultModel = models[0]
	}

	p.modelMu.Lock()
	p.cachedModels = models
	p.defaultModel = defaultModel
	p.modelsCachedAt = time.Now()
	if p.modelModes == nil {
		p.modelModes = make(map[string]string)
	}
	for _, item := range payload.Models {
		if item.ModelID != "" && item.ModelMode != "" {
			p.modelModes[item.ModelID] = item.ModelMode
		}
	}
	p.modelMu.Unlock()
	return p.CachedModels(), nil
}

// resolveUpstreamModel 将用户传入的模型别名（如 grok-2-image / grok）映射为 Grok Web 可执行的模型与模式。
func (p *GrokProvider) resolveUpstreamModel(ctx context.Context, requested string, forImage bool) (string, string) {
	requested = strings.TrimSpace(requested)
	p.modelMu.RLock()
	cachedDef := p.defaultModel
	mode := ""
	if p.modelModes != nil && requested != "" {
		mode = p.modelModes[requested]
	}
	p.modelMu.RUnlock()

	targetModel := requested
	if targetModel == "" || strings.EqualFold(targetModel, "grok") || strings.EqualFold(targetModel, "grok-2-image") || strings.EqualFold(targetModel, "aurora") {
		if cachedDef != "" {
			targetModel = cachedDef
		} else {
			targetModel = "grok-3"
		}
	}

	if mode == "" {
		p.modelMu.RLock()
		if p.modelModes != nil {
			mode = p.modelModes[targetModel]
		}
		p.modelMu.RUnlock()
	}

	if mode == "" {
		switch {
		case strings.HasPrefix(targetModel, "grok-4-1-thinking"):
			mode = "MODEL_MODE_GROK_4_1_THINKING"
		case strings.HasPrefix(targetModel, "grok-420"):
			mode = "MODEL_MODE_GROK_420"
		case strings.HasPrefix(targetModel, "grok-3"):
			mode = "MODEL_MODE_FAST"
		default:
			mode = "MODEL_MODE_FAST"
		}
	}

	return targetModel, mode
}

type grokAccountLease struct {
	ID     string
	Email  string
	Cookie string
	Proxy  string
}

func (p *GrokProvider) leaseAccount(excludedIDs map[string]bool) (*grokAccountLease, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	n := len(p.accounts)
	if n == 0 {
		return nil, ErrNoGrokAccountAvailable
	}

	now := time.Now()

	// 候选过滤：排除已被排除、已封禁、已禁用、已失效、冷却中或无凭据的账号
	isCandidate := func(a *grokAccount) bool {
		if a == nil || (excludedIDs != nil && excludedIDs[a.ID]) {
			return false
		}
		if a.Status == accounts.StatusRateLimited && !a.IsInCooldown(now) {
			a.Status = accounts.StatusActive
			a.CooldownUntil = time.Time{}
		}
		if a.Status != accounts.StatusActive || a.IsInCooldown(now) || a.Cookie == "" {
			return false
		}
		// 若已有明确凭据失效错误记录且未更换新 Cookie，自动置为 expired 并跳过
		if strings.Contains(a.LastError, "401") || strings.Contains(a.LastError, "失效") || strings.Contains(a.LastError, "Invalid credentials") {
			a.Status = accounts.StatusExpired
			return false
		}
		return true
	}

	// 第一轮优先挑选完全无报错记录的健康活跃账号
	for i := 0; i < n; i++ {
		candidateIdx := (p.idx + i) % n
		a := p.accounts[candidateIdx]
		if isCandidate(a) && a.LastError == "" {
			p.idx = (candidateIdx + 1) % n
			a.TotalCalls++
			a.LastUsed = now
			return &grokAccountLease{
				ID:     a.ID,
				Email:  a.Email,
				Cookie: a.Cookie,
				Proxy:  a.Proxy,
			}, nil
		}
	}

	// 第二轮选择其余候选可用账号
	for i := 0; i < n; i++ {
		candidateIdx := (p.idx + i) % n
		a := p.accounts[candidateIdx]
		if isCandidate(a) {
			p.idx = (candidateIdx + 1) % n
			a.TotalCalls++
			a.LastUsed = now
			return &grokAccountLease{
				ID:     a.ID,
				Email:  a.Email,
				Cookie: a.Cookie,
				Proxy:  a.Proxy,
			}, nil
		}
	}

	return nil, ErrNoGrokAccountAvailable
}

func (p *GrokProvider) reportOutcomeByID(id string, success bool, statusCode int, errMsg string) {
	p.mu.Lock()
	var acct *grokAccount
	for _, a := range p.accounts {
		if a.ID == id {
			acct = a
			break
		}
	}
	if acct == nil {
		p.mu.Unlock()
		return
	}

	now := time.Now()
	if success {
		acct.LastError = ""
		p.mu.Unlock()
		return
	}

	acct.FailedCalls++
	acct.LastError = errMsg
	needPersist := false
	needRebalance := false

	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		acct.Status = accounts.StatusExpired
		acct.LastError = fmt.Sprintf("凭据已失效 HTTP %d: %s", statusCode, errMsg)
		needPersist = true
		needRebalance = true
	} else if statusCode == http.StatusTooManyRequests {
		acct.Status = accounts.StatusRateLimited
		acct.CooldownUntil = now.Add(10 * time.Minute)
		needPersist = true
	}
	if needRebalance {
		p.rebalanceLocked(now)
	}
	p.mu.Unlock()

	if needPersist {
		go p.persistToDB()
	}
}

func sanitizeTraceErr(errStr string) string {
	errStr = strings.ReplaceAll(errStr, ":", "_")
	errStr = strings.ReplaceAll(errStr, " ", "_")
	errStr = strings.ReplaceAll(errStr, "\"", "")
	if len(errStr) > 40 {
		return errStr[:40]
	}
	return errStr
}

func (p *GrokProvider) reportStatus(idx int, success bool, isRateLimit bool, errMsg string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if idx < 0 || idx >= len(p.accounts) {
		return
	}
	a := p.accounts[idx]
	if success {
		a.LastError = ""
	} else {
		a.FailedCalls++
		a.LastError = errMsg
		if isRateLimit {
			a.Status = accounts.StatusRateLimited
			a.CooldownUntil = time.Now().Add(10 * time.Minute)
		}
	}
}

func (p *GrokProvider) createHTTPClient(proxyURL string) *http.Client {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   4 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		MaxIdleConns:       10,
		IdleConnTimeout:    30 * time.Second,
		DisableCompression: false,
	}
	if proxyURL != "" {
		if parsed, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(parsed)
		}
	}
	return &http.Client{
		Transport: transport,
		Timeout:   120 * time.Second,
	}
}

func isGrokProxyTransportError(err error) bool {
	if err == nil {
		return false
	}
	lower := strings.ToLower(err.Error())
	for _, kw := range []string{
		"proxyconnect",
		"connection refused",
		"socks",
		"no such host",
		"i/o timeout",
		"dial tcp",
	} {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// doRequestWithProxyFallback 优先使用账号粘性代理发起请求；若代理本身不可达，上报代理池熔断并依次尝试健康备选代理与直连兜底。
func (p *GrokProvider) doRequestWithProxyFallback(ctx context.Context, primaryProxy string, buildReq func() (*http.Request, error)) (*http.Response, string, error) {
	candidates := []string{primaryProxy}
	if primaryProxy == "" {
		req, err := buildReq()
		if err != nil {
			return nil, "", err
		}
		resp, err := p.createHTTPClient("").Do(req)
		return resp, "", err
	}

	seen := map[string]bool{primaryProxy: true}
	var lastErr error
	for idx := 0; idx < len(candidates); idx++ {
		proxyURL := candidates[idx]
		req, err := buildReq()
		if err != nil {
			return nil, proxyURL, err
		}
		start := time.Now()
		resp, err := p.createHTTPClient(proxyURL).Do(req)
		if err == nil {
			if proxyURL != "" && p.proxyPool != nil {
				p.proxyPool.ReportSuccess(proxyURL, time.Since(start).Milliseconds())
			}
			return resp, proxyURL, nil
		}
		lastErr = err
		if proxyURL != "" && isGrokProxyTransportError(err) {
			if p.proxyPool != nil {
				p.proxyPool.ReportFailure(proxyURL, err.Error())
				altProxy := p.proxyPool.AllocateHealthy(seen)
				if altProxy != "" && !seen[altProxy] && p.proxyPool.IsHealthy(altProxy) {
					seen[altProxy] = true
					candidates = append(candidates, altProxy)
					continue
				}
			}
			if !seen[""] {
				seen[""] = true
				candidates = append(candidates, "")
			}
			continue
		}
		return nil, proxyURL, err
	}
	return nil, primaryProxy, lastErr
}

// Descriptor 返回供应商插件元信息
func (p *GrokProvider) Descriptor() ProviderDescriptor {
	models := p.CachedModels()
	return ProviderDescriptor{
		ID:                    "grok",
		Name:                  "xAI Grok",
		Vendor:                "xAI",
		Status:                "enabled",
		Version:               "1.0.0",
		Description:           "Grok.com / X 免登与 SSO 免费号池插件（支持实时模型目录与 Aurora Image 逆向生图）",
		SupportedAccountTypes: []string{"free", "sso_cookie", "kami"},
		CredentialFormats:     []string{"auto", "kami", "cookie"},
		Capabilities: []string{
			"chat",
			"image_generation",
		},
		FeaturedModels: models,
		ImageModels:    []string{"grok-2-image"},
		Tags:           MakeTags("daily-credits", "cookie-pool", "reverse-web", "img-flux-wanx"),
		CostType:       "daily_credits",
		AuthType:       "cookie",
	}
}

// Stats 统计号池状态
func (p *GrokProvider) Stats() accounts.PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	now := time.Now()
	var stats accounts.PoolStats
	stats.Total = len(p.accounts)
	for _, a := range p.accounts {
		stats.TotalCalls += a.TotalCalls
		stats.FailedCalls += a.FailedCalls
		switch a.Status {
		case accounts.StatusActive:
			if a.IsInCooldown(now) {
				stats.RateLimited++
			} else {
				stats.Active++
			}
		case accounts.StatusStandby:
			stats.Standby++
		case accounts.StatusPending:
			stats.Pending++
		case accounts.StatusRateLimited:
			stats.RateLimited++
		case accounts.StatusExpired:
			stats.Expired++
		case accounts.StatusDisabled:
			stats.Disabled++
		case accounts.StatusBanned:
			stats.Banned++
		}
	}
	return stats
}

// ListAccounts 返回脱敏的账号列表
func (p *GrokProvider) ListAccounts(typeFilter, statusFilter, search string) []accounts.AccountSnapshot {
	p.mu.RLock()
	defer p.mu.RUnlock()

	typeFilter = strings.ToLower(strings.TrimSpace(typeFilter))
	statusFilter = strings.ToLower(strings.TrimSpace(statusFilter))
	search = strings.ToLower(strings.TrimSpace(search))

	out := make([]accounts.AccountSnapshot, 0, len(p.accounts))
	now := time.Now()
	for _, a := range p.accounts {
		accountType := "sso_cookie"
		if a.Password != "" {
			accountType = "kami"
		}
		if typeFilter != "" && typeFilter != "all" && typeFilter != accountType && !(typeFilter == "cookie" && accountType == "sso_cookie") {
			continue
		}
		st := a.Status.String()
		if a.Status == accounts.StatusActive && a.IsInCooldown(now) {
			st = "rate_limited"
		}
		if statusFilter != "" && statusFilter != "all" && st != statusFilter {
			continue
		}
		if search != "" {
			haystack := strings.ToLower(fmt.Sprintf("%s %s %s", a.ID, a.Email, a.Proxy))
			if !strings.Contains(haystack, search) {
				continue
			}
		}

		preview := a.Cookie
		if preview == "" {
			if a.LegacyToken != "" {
				preview = "旧 Token 未启用"
			} else {
				preview = "未提取 SSO"
			}
		} else if len(preview) > 16 {
			preview = preview[:8] + "..." + preview[len(preview)-6:]
		}

		cd := ""
		if !a.CooldownUntil.IsZero() {
			cd = a.CooldownUntil.Format(time.RFC3339)
		}
		lastUsed := ""
		if !a.LastUsed.IsZero() {
			lastUsed = a.LastUsed.Format(time.RFC3339)
		}
		actionStartedAt := ""
		if !a.ActionStartedAt.IsZero() {
			actionStartedAt = a.ActionStartedAt.Format(time.RFC3339)
		}
		lastActionAt := ""
		if !a.LastActionAt.IsZero() {
			lastActionAt = a.LastActionAt.Format(time.RFC3339)
		}

		out = append(out, accounts.AccountSnapshot{
			ID:                 a.ID,
			Email:              a.Email,
			Type:               accountType,
			Status:             st,
			PlanType:           "grok_free",
			TokenPreview:       preview,
			HasKamiCredentials: a.Password != "",
			HasTOTPSecret:      a.TOTPSecret != "",
			Proxy:              a.Proxy,
			TotalCalls:         a.TotalCalls,
			FailedCalls:        a.FailedCalls,
			CooldownUntil:      cd,
			ActiveAction:       a.ActiveAction,
			ActionPhase:        a.ActionPhase,
			ActionPhaseLabel:   a.ActionPhaseLabel,
			ActionQueuePos:     a.ActionQueuePos,
			ActionTaskID:       a.ActionTaskID,
			ActionStartedAt:    actionStartedAt,
			LastAction:         a.LastAction,
			LastActionOK:       a.LastActionOK,
			LastActionSummary:  a.LastActionSummary,
			LastActionAt:       lastActionAt,
			LastUsed:           lastUsed,
			LastError:          a.LastError,
			CreatedAt:          a.CreatedAt.Format(time.RFC3339),
			Capabilities:       []string{"chat", "image_generation"},
		})
	}
	return out
}

func normalizeGrokCredentialLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if trimmed := strings.TrimSpace(line); trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			out = append(out, trimmed)
		}
	}
	return out
}

// ImportCredentials 导入 Grok Cookie 或卡密凭据；auto 模式逐行自动识别混合格式。
func (p *GrokProvider) ImportCredentials(req ImportRequest) (ImportResult, error) {
	credentialType := strings.ToLower(strings.TrimSpace(req.CredentialType))
	if credentialType == "" {
		credentialType = "auto"
	}
	if credentialType != "auto" && credentialType != "kami" && credentialType != "cookie" {
		return ImportResult{}, errors.New("Grok 支持 auto 自动识别、kami 卡密或 cookie SSO 凭据")
	}

	rawTokens := make([]string, 0, len(req.Tokens))
	rawTokens = append(rawTokens, req.Tokens...)
	if strings.TrimSpace(req.RawText) != "" {
		for _, line := range strings.Split(req.RawText, "\n") {
			if trimmed := strings.TrimSpace(line); trimmed != "" {
				rawTokens = append(rawTokens, trimmed)
			}
		}
	}
	tokens := normalizeGrokCredentialLines(rawTokens)
	importTokens := make([]string, 0, len(tokens))
	unknownLines := make([]string, 0)
	failed := 0
	detectedKami := 0
	detectedCookie := 0
	llmKami := 0
	llmCookie := 0
	for _, line := range tokens {
		kind, _, err := DetectGrokCredentialLine(line)
		if err != nil {
			if credentialType == "auto" {
				unknownLines = append(unknownLines, line)
			}
			failed++
			continue
		}
		if credentialType != "auto" && kind != credentialType {
			failed++
			continue
		}
		importTokens = append(importTokens, line)
		switch kind {
		case "kami":
			detectedKami++
		case "cookie":
			detectedCookie++
		}
	}

	learnedPatterns := p.learnedGrokCredentialPatterns()
	learnedKami := 0
	learnedCookie := 0
	llmLines := make([]string, 0, len(unknownLines))
	for _, line := range unknownLines {
		kind, value, matched := p.matchLearnedGrokCredential(line, learnedPatterns)
		if !matched {
			llmLines = append(llmLines, line)
			continue
		}
		failed--
		importTokens = append(importTokens, value)
		switch kind {
		case "kami":
			learnedKami++
		case "cookie":
			learnedCookie++
		}
	}

	llmFallbackUsed := false
	var llmError string
	if len(llmLines) > 0 && credentialType == "auto" {
		p.mu.RLock()
		llmFn := p.grokCredentialLLMFn
		p.mu.RUnlock()
		if llmFn != nil {
			llmFallbackUsed = true
			ctx, cancel := context.WithTimeout(context.Background(), grokCredentialLLMTimeout)
			items, err := llmFn(ctx, llmLines)
			cancel()
			if err != nil {
				llmError = grokCredentialLLMError(err)
			} else {
				for _, item := range items {
					original := ""
					if item.Index >= 0 && item.Index < len(llmLines) {
						original = llmLines[item.Index]
					}
					kind, validateErr := validateGrokCredentialLLMResult(original, item)
					if validateErr != nil || kind == "unknown" || original == "" {
						continue
					}
					failed--
					value := strings.TrimSpace(item.Value)
					importTokens = append(importTokens, value)
					switch kind {
					case "kami":
						llmKami++
					case "cookie":
						llmCookie++
					}
					_ = p.learnGrokCredentialPattern(original, value, kind)
				}
			}
		}
	}
	if failed < 0 {
		failed = 0
	}

	added, affectedIDs := p.addAccounts(importTokens, req.Persist)
	p.mu.RLock()
	total := len(p.accounts)
	loginIDs := make([]string, 0, len(affectedIDs))
	for _, id := range affectedIDs {
		if account := p.findGrokAccountLocked(id); account != nil && account.Password != "" && account.Cookie == "" && account.TOTPSecret != "" {
			loginIDs = append(loginIDs, id)
		}
	}
	p.mu.RUnlock()

	skipped := len(tokens) - added - failed
	if skipped < 0 {
		skipped = 0
	}
	messages := []string{fmt.Sprintf("成功导入 %d 个 Grok 凭据", added)}
	if credentialType == "auto" {
		detectionMessage := fmt.Sprintf("自动识别：卡密 %d 条，SSO Cookie %d 条", detectedKami, detectedCookie)
		if learnedKami+learnedCookie > 0 {
			detectionMessage += fmt.Sprintf("；记忆格式：卡密 %d 条，SSO Cookie %d 条", learnedKami, learnedCookie)
		}
		if llmFallbackUsed {
			detectionMessage += fmt.Sprintf("；LLM 兜底：卡密 %d 条，SSO Cookie %d 条", llmKami, llmCookie)
		}
		detectionMessage += fmt.Sprintf("，无法识别 %d 条", failed)
		messages = append(messages, detectionMessage)
		if llmError != "" {
			messages = append(messages, llmError)
		}
	}
	var loginTask *KamiLoginTask
	if req.AutoLogin && len(loginIDs) > 0 {
		if task, err := p.StartKamiLoginBatch(loginIDs, req.Headless); err == nil {
			loginTask = &task
			messages = append(messages, fmt.Sprintf("已启动后台重登任务，队列 %d 个账号", task.Total-task.Completed))
		} else {
			messages = append(messages, "自动重登未启动: "+err.Error())
		}
	}

	return ImportResult{
		Added:     added,
		Skipped:   skipped,
		Failed:    failed,
		TotalPool: total,
		Persisted: req.Persist,
		Messages:  messages,
		LoginTask: loginTask,
	}, nil
}

// ExecuteAccountAction 执行单账号运维操作
func (p *GrokProvider) ExecuteAccountAction(id, action string) (map[string]interface{}, error) {
	return p.ExecuteAccountActionWithOpts(id, action, false)
}

func (p *GrokProvider) executeGrokProbe(id string) (map[string]interface{}, error) {
	p.mu.RLock()
	target := p.findGrokAccountLocked(id)
	if target == nil {
		p.mu.RUnlock()
		return nil, errors.New("账号不存在")
	}
	email := target.Email
	proxyURL := target.Proxy
	cookie := target.Cookie
	p.mu.RUnlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	resp, _, err := p.doRequestWithProxyFallback(ctx, proxyURL, func() (*http.Request, error) {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/rest/app-chat/conversations?limit=1", nil)
		if reqErr != nil {
			return nil, reqErr
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
		req.Header.Set("Cookie", cookie)
		req.Header.Set("Referer", p.baseURL+"/")
		return req, nil
	})
	latency := time.Since(start).Milliseconds()
	if err != nil {
		errMsg := fmt.Sprintf("网络连接异常: %v", err)
		p.mu.Lock()
		if account := p.findGrokAccountLocked(id); account != nil {
			account.FailedCalls++
			account.LastError = errMsg
		}
		p.mu.Unlock()
		go p.persistToDB()
		return map[string]interface{}{
			"id":         id,
			"email":      email,
			"ok":         false,
			"status":     "network_error",
			"latency_ms": latency,
			"error":      errMsg,
		}, nil
	}
	defer resp.Body.Close()

	p.mu.Lock()
	defer p.mu.Unlock()
	target = p.findGrokAccountLocked(id)
	if target == nil {
		return nil, errors.New("账号不存在")
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		target.Status = accounts.StatusExpired
		target.LastError = fmt.Sprintf("凭据已失效 HTTP %d", resp.StatusCode)
		go p.persistToDB()
		return map[string]interface{}{
			"id":          id,
			"email":       email,
			"ok":          false,
			"status":      "expired",
			"http_status": resp.StatusCode,
			"latency_ms":  latency,
			"error":       target.LastError,
		}, nil
	}
	if resp.StatusCode != http.StatusOK {
		target.LastError = fmt.Sprintf("探测异常 HTTP %d", resp.StatusCode)
		go p.persistToDB()
		return map[string]interface{}{
			"id":          id,
			"email":       email,
			"ok":          false,
			"status":      target.Status.String(),
			"http_status": resp.StatusCode,
			"latency_ms":  latency,
			"error":       target.LastError,
		}, nil
	}

	target.LastError = ""
	target.Status = accounts.StatusActive
	go p.persistToDB()
	models := p.CachedModels()
	return map[string]interface{}{
		"id":           id,
		"email":        email,
		"ok":           true,
		"status":       "active",
		"http_status":  resp.StatusCode,
		"latency_ms":   latency,
		"models_count": len(models),
		"models":       models,
		"message":      fmt.Sprintf("测活通过 (%dms)", latency),
	}, nil
}

// ExecuteAccountActionWithOpts 执行单账号运维操作（带选项）。
func (p *GrokProvider) ExecuteAccountActionWithOpts(id, action string, headless bool) (map[string]interface{}, error) {
	action = strings.ToLower(strings.TrimSpace(action))

	if action == "login" || action == "refresh" {
		p.mu.RLock()
		target := p.findGrokAccountLocked(id)
		if target == nil {
			p.mu.RUnlock()
			return nil, errors.New("账号不存在")
		}
		email := target.Email
		status := target.Status.String()
		if target.Status == accounts.StatusDisabled {
			p.mu.RUnlock()
			return nil, errors.New("账号已停用，请先启用后再重登")
		}
		if target.Email == "" || target.Password == "" {
			p.mu.RUnlock()
			return nil, errors.New("该账号未配置邮箱与密码凭据，无法自动登录")
		}
		p.mu.RUnlock()

		task, err := p.StartKamiLoginBatch([]string{id}, headless)
		if err != nil {
			return nil, err
		}
		modeHint := "有头可见模式，遇到人机验证请在弹出的 Chrome 窗口中完成"
		if headless {
			modeHint = "无头模式"
		}
		message := fmt.Sprintf("账号 %s 已加入 Grok 重登任务（%s）", email, modeHint)
		if task.CurrentAccountID != "" && task.CurrentAccountID != id {
			for index, queuedID := range task.QueuedAccountIDs {
				if queuedID == id {
					message = fmt.Sprintf("账号 %s 已加入 Grok 重登队列（排队 #%d，%s）", email, index+1, modeHint)
					break
				}
			}
		}
		return map[string]interface{}{
			"id":      id,
			"email":   email,
			"status":  status,
			"queued":  true,
			"task_id": task.ID,
			"task":    task,
			"message": message,
		}, nil
	}

	if action == "totp" {
		p.mu.RLock()
		target := p.findGrokAccountLocked(id)
		if target == nil {
			p.mu.RUnlock()
			return nil, errors.New("账号不存在")
		}
		email := target.Email
		secret := target.TOTPSecret
		p.mu.RUnlock()
		if secret == "" {
			return nil, errors.New("该账号未配置 2FA 密钥")
		}
		code, remaining, err := accounts.GenerateTOTP(secret, time.Now())
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"id":                id,
			"email":             email,
			"totp_code":         code,
			"remaining_seconds": remaining,
			"message":           fmt.Sprintf("2FA 动态码: %s (剩余 %ds)", code, remaining),
		}, nil
	}

	if action == "get_cookie" {
		p.mu.RLock()
		target := p.findGrokAccountLocked(id)
		if target == nil {
			p.mu.RUnlock()
			return nil, errors.New("账号不存在")
		}
		cookie := target.Cookie
		email := target.Email
		p.mu.RUnlock()
		return map[string]interface{}{
			"id":     id,
			"email":  email,
			"cookie": cookie,
		}, nil
	}

	if action == "probe" || action == "test" {
		return p.executeGrokProbe(id)
	}

	p.mu.Lock()
	var target *grokAccount
	targetIdx := -1
	for index, account := range p.accounts {
		if account.ID == id {
			target = account
			targetIdx = index
			break
		}
	}
	if target == nil {
		p.mu.Unlock()
		return nil, errors.New("账号不存在")
	}

	switch action {
	case "standby":
		target.Status = accounts.StatusStandby
		p.mu.Unlock()
		go p.persistToDB()
		return map[string]interface{}{"id": target.ID, "status": "standby"}, nil
	case "activate", "enable":
		target.Status = accounts.StatusActive
		target.CooldownUntil = time.Time{}
		target.ActivatedAt = time.Now()
		p.mu.Unlock()
		go p.persistToDB()
		return map[string]interface{}{"id": target.ID, "status": "active"}, nil
	case "disable":
		target.Status = accounts.StatusDisabled
		p.mu.Unlock()
		go p.persistToDB()
		return map[string]interface{}{"id": target.ID, "status": "disabled"}, nil
	case "delete":
		deletedID := id
		p.accounts = append(p.accounts[:targetIdx], p.accounts[targetIdx+1:]...)
		p.mu.Unlock()
		go p.persistToDisk()
		if gdb := p.grokCredentialPatternDB; gdb != nil || db.GetDB() != nil {
			if gdb == nil {
				gdb = db.GetDB()
			}
			if gdb != nil {
				_ = gdb.Where("id = ? AND provider = ?", deletedID, "grok").Delete(&db.PoolAccountRecord{}).Error
			}
		}
		return map[string]interface{}{"id": id, "deleted": true}, nil
	default:
		p.mu.Unlock()
		return nil, fmt.Errorf("不支持的操作: %s", action)
	}
}

// BulkUpdateAccountProxy 批量分配代理
func (p *GrokProvider) BulkUpdateAccountProxy(accountIDs []string, proxyURL string) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	idMap := make(map[string]bool, len(accountIDs))
	for _, id := range accountIDs {
		idMap[id] = true
	}

	updated := 0
	for _, a := range p.accounts {
		if len(accountIDs) == 0 || idMap[a.ID] {
			a.Proxy = proxyURL
			updated++
		}
	}
	return updated, nil
}

// UpdateAccountCookie 更新单个账号 Cookie
func (p *GrokProvider) UpdateAccountCookie(id, cookie string) (map[string]interface{}, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, a := range p.accounts {
		if a.ID == id {
			a.Cookie = strings.TrimSpace(cookie)
			a.Status = accounts.StatusActive
			a.LastError = ""
			a.CooldownUntil = time.Time{}
			a.ActivatedAt = time.Now()
			a.LastAction = "update_cookie"
			a.LastActionOK = true
			a.LastActionSummary = "已手动更新 Cookie 凭据"
			a.LastActionAt = time.Now()
			go p.persistToDisk()
			go p.persistToDB()
			return map[string]interface{}{"id": id, "updated": true, "message": "Cookie 更新成功，账号已重新激活"}, nil
		}
	}
	return nil, errors.New("账号不存在")
}

// ChatCompletion 对话实现（支持流式 SSE 与完整文本）
func (p *GrokProvider) ChatCompletion(ctx context.Context, originalReq officialtypes.APIRequest) (<-chan string, func(), error) {
	lease, err := p.leaseAccount(nil)
	if err != nil {
		return nil, nil, err
	}

	upstreamModel, modelMode := p.resolveUpstreamModel(ctx, originalReq.Model, false)

	// 拼接提示词
	var promptBuilder strings.Builder
	for _, m := range originalReq.Messages {
		text := m.Text()
		if text != "" {
			if promptBuilder.Len() > 0 {
				promptBuilder.WriteString("\n\n")
			}
			promptBuilder.WriteString(fmt.Sprintf("%s: %s", strings.ToUpper(m.Role), text))
		}
	}
	finalPrompt := promptBuilder.String()

	payload := map[string]interface{}{
		"temporary":             true,
		"modelName":             upstreamModel,
		"modelMode":             modelMode,
		"message":               finalPrompt,
		"fileAttachments":       []interface{}{},
		"imageAttachments":      []interface{}{},
		"disableSearch":         false,
		"enableImageGeneration": false,
		"returnImageBytes":      false,
		"returnBuffers":         true,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, err
	}

	reqURL := p.baseURL + "/rest/app-chat/conversations/new"
	resp, _, err := p.doRequestWithProxyFallback(ctx, lease.Proxy, func() (*http.Request, error) {
		req, reqErr := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(bodyBytes))
		if reqErr != nil {
			return nil, reqErr
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", grokBrowserUserAgent)
		req.Header.Set("Origin", p.baseURL)
		req.Header.Set("Referer", p.baseURL+"/")
		req.Header.Set("Cookie", lease.Cookie)
		req.Header.Set("Accept", "text/event-stream")
		return req, nil
	})
	if err != nil {
		p.reportOutcomeByID(lease.ID, false, 0, err.Error())
		return nil, nil, err
	}

	if resp.StatusCode == 429 {
		resp.Body.Close()
		p.reportOutcomeByID(lease.ID, false, 429, "Rate limit 429")
		return nil, nil, errors.New("grok upstream rate limited (429)")
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		resp.Body.Close()
		p.reportOutcomeByID(lease.ID, false, resp.StatusCode, "Invalid credentials")
		return nil, nil, fmt.Errorf("grok upstream invalid credentials (%d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		p.reportOutcomeByID(lease.ID, false, resp.StatusCode, fmt.Sprintf("HTTP %d", resp.StatusCode))
		return nil, nil, fmt.Errorf("grok upstream returned HTTP %d", resp.StatusCode)
	}

	tokenChan := make(chan string, 100)
	cancel := func() {
		_ = resp.Body.Close()
	}

	go func() {
		defer close(tokenChan)
		defer resp.Body.Close()

		scanner := bufio.NewScanner(resp.Body)
		// Grok 可能输出很长的 JSON 行
		buf := make([]byte, 64*1024)
		scanner.Buffer(buf, 1024*1024)

		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}
			if strings.HasPrefix(line, "data: ") {
				line = strings.TrimPrefix(line, "data: ")
			}

			// 解析 Grok 响应行
			var parsed struct {
				Token  string `json:"token"`
				Result struct {
					Response struct {
						Token string `json:"token"`
					} `json:"response"`
				} `json:"result"`
			}
			if err := json.Unmarshal([]byte(line), &parsed); err == nil {
				delta := parsed.Token
				if delta == "" {
					delta = parsed.Result.Response.Token
				}
				if delta != "" {
					select {
					case tokenChan <- delta:
					case <-ctx.Done():
						return
					}
				}
			}
		}
		p.reportOutcomeByID(lease.ID, true, 200, "")
	}()

	return tokenChan, cancel, nil
}

// GrokImageItem 单张生成的图像元数据。
type GrokImageItem struct {
	URL           string `json:"url,omitempty"`
	B64JSON       string `json:"b64_json,omitempty"`
	RevisedPrompt string `json:"revised_prompt,omitempty"`
}

// GrokImageAttemptSpan 单次 Grok 账号生图尝试追踪跨度。
type GrokImageAttemptSpan struct {
	AccountID string `json:"account_id"`
	Email     string `json:"email"`
	Proxy     string `json:"proxy"`
	Status    int    `json:"status"`
	Error     string `json:"error,omitempty"`
}

// GrokImageResult Grok 图像生成结果及容灾追踪元数据。
type GrokImageResult struct {
	Images     []GrokImageItem        `json:"images"`
	Model      string                 `json:"model"`
	AccountID  string                 `json:"account_id"`
	Email      string                 `json:"email"`
	Proxy      string                 `json:"proxy"`
	Attempts   int                    `json:"attempts"`
	DurationMs int64                  `json:"duration_ms"`
	Trace      string                 `json:"trace"`
	Spans      []GrokImageAttemptSpan `json:"spans"`
}

func parseGrokCardAttachmentJSON(rawJSON string) string {
	rawJSON = strings.TrimSpace(rawJSON)
	if rawJSON == "" {
		return ""
	}
	var card struct {
		ImageChunk struct {
			ImageURL  string `json:"imageUrl"`
			ImageUUID string `json:"imageUuid"`
		} `json:"image_chunk"`
		ImageURL string `json:"imageUrl"`
		URL      string `json:"url"`
	}
	if err := json.Unmarshal([]byte(rawJSON), &card); err == nil {
		if card.ImageChunk.ImageURL != "" {
			return card.ImageChunk.ImageURL
		}
		if card.ImageURL != "" {
			return card.ImageURL
		}
		if card.URL != "" {
			return card.URL
		}
	}
	if idx := strings.Index(rawJSON, `"imageUrl":"`); idx != -1 {
		rest := rawJSON[idx+12:]
		if end := strings.Index(rest, `"`); end != -1 {
			return rest[:end]
		}
	}
	return ""
}

func parseGrokImageStream(r io.Reader) (string, error) {
	scanner := bufio.NewScanner(r)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 2*1024*1024)

	var foundURL string
	var lastErr string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			line = strings.TrimPrefix(line, "data: ")
		}

		var chunk struct {
			Error *struct {
				Message string `json:"message"`
				Code    any    `json:"code"`
			} `json:"error"`
			Result struct {
				Response struct {
					CardAttachment struct {
						JSONData string `json:"jsonData"`
					} `json:"cardAttachment"`
					ModelResponse struct {
						CardAttachmentsJson []string `json:"cardAttachmentsJson"`
					} `json:"modelResponse"`
					ModelOutput struct {
						ImageURLs []string `json:"imageUrls"`
					} `json:"modelOutput"`
				} `json:"response"`
			} `json:"result"`
			ImageURL string `json:"imageUrl"`
		}

		if err := json.Unmarshal([]byte(line), &chunk); err == nil {
			if chunk.Error != nil && chunk.Error.Message != "" {
				lastErr = chunk.Error.Message
			}
			// 1. Check cardAttachment.jsonData
			if chunk.Result.Response.CardAttachment.JSONData != "" {
				if u := parseGrokCardAttachmentJSON(chunk.Result.Response.CardAttachment.JSONData); u != "" {
					foundURL = u
					break
				}
			}
			// 2. Check modelResponse.cardAttachmentsJson
			for _, cardStr := range chunk.Result.Response.ModelResponse.CardAttachmentsJson {
				if u := parseGrokCardAttachmentJSON(cardStr); u != "" {
					foundURL = u
					break
				}
			}
			if foundURL != "" {
				break
			}
			// 3. Check modelOutput.imageUrls
			if len(chunk.Result.Response.ModelOutput.ImageURLs) > 0 && chunk.Result.Response.ModelOutput.ImageURLs[0] != "" {
				foundURL = chunk.Result.Response.ModelOutput.ImageURLs[0]
				break
			}
			// 4. Check top-level imageUrl
			if chunk.ImageURL != "" {
				foundURL = chunk.ImageURL
				break
			}
		}
	}

	if foundURL == "" {
		if lastErr != "" {
			return "", fmt.Errorf("grok stream error: %s", lastErr)
		}
		return "", errors.New("grok upstream did not return image url")
	}
	return foundURL, nil
}

func resolveGrokAssetURL(assetBaseURL, raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	if assetBaseURL == "" {
		assetBaseURL = "https://assets.grok.com"
	}
	assetBaseURL = strings.TrimRight(assetBaseURL, "/")
	raw = strings.TrimLeft(raw, "/")
	return assetBaseURL + "/" + raw
}

func (p *GrokProvider) downloadGrokImageAsset(ctx context.Context, rawURL, cookie, proxyURL string) ([]byte, error) {
	p.mu.RLock()
	assetBaseURL := p.assetBaseURL
	p.mu.RUnlock()
	targetURL := resolveGrokAssetURL(assetBaseURL, rawURL)

	resp, effectiveProxy, err := p.doRequestWithProxyFallback(ctx, proxyURL, func() (*http.Request, error) {
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
		if reqErr != nil {
			return nil, reqErr
		}
		req.Header.Set("User-Agent", grokBrowserUserAgent)
		req.Header.Set("Referer", p.baseURL+"/")
		req.Header.Set("Origin", p.baseURL)
		if cookie != "" {
			req.Header.Set("Cookie", cookie)
		}
		return req, nil
	})
	if err != nil || (resp != nil && resp.StatusCode == http.StatusForbidden) {
		if resp != nil {
			_ = resp.Body.Close()
		}
		// 备用：若标准 net/http 遭遇 403 挑战，使用 Bogdanfinn TLS 客户端穿透
		tlsClient := bogdanfinn.NewStdClient()
		if effectiveProxy != "" {
			_ = tlsClient.SetProxy(effectiveProxy)
		}
		headers := httpclient.Headers{
			"User-Agent": grokBrowserUserAgent,
			"Referer":    p.baseURL + "/",
			"Origin":     p.baseURL,
		}
		if cookie != "" {
			headers["Cookie"] = cookie
		}
		tlsResp, doErr := tlsClient.Request(httpclient.GET, targetURL, headers, nil, nil)
		if doErr == nil && tlsResp.StatusCode == http.StatusOK {
			defer tlsResp.Body.Close()
			return io.ReadAll(tlsResp.Body)
		}
		if tlsResp != nil {
			_ = tlsResp.Body.Close()
		}
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("download grok asset returned HTTP %d", resp.StatusCode)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download grok asset returned HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

// GenerateImageAsset 使用 Grok Web Aurora 逆向生图（带多账号容灾轮转与资源安全回传）。
func (p *GrokProvider) GenerateImageAsset(ctx context.Context, prompt, requestedModel, size string, count int) (GrokImageResult, error) {
	start := time.Now()
	res := GrokImageResult{
		Model: requestedModel,
		Spans: make([]GrokImageAttemptSpan, 0, 3),
	}

	upstreamModel, modelMode := p.resolveUpstreamModel(ctx, requestedModel, true)
	if count <= 0 {
		count = 1
	}
	if count > 4 {
		count = 4
	}

	excluded := make(map[string]bool)
	maxAttempts := 10
	p.mu.RLock()
	if totalAccs := len(p.accounts); totalAccs > 0 && totalAccs < maxAttempts {
		maxAttempts = totalAccs
	}
	p.mu.RUnlock()
	var lastErr error
	var traceParts []string

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		lease, err := p.leaseAccount(excluded)
		if err != nil {
			if lastErr == nil {
				lastErr = err
			}
			break
		}
		excluded[lease.ID] = true
		res.AccountID = lease.ID
		res.Email = lease.Email
		res.Proxy = lease.Proxy
		res.Attempts = attempt

		payload := map[string]interface{}{
			"temporary":             true,
			"modelName":             upstreamModel,
			"modelMode":             modelMode,
			"message":               "Generate an image: " + prompt,
			"fileAttachments":       []interface{}{},
			"imageAttachments":      []interface{}{},
			"disableSearch":         true,
			"enableImageGeneration": true,
			"returnImageBytes":      false,
			"enableImageStreaming":  false,
			"imageGenerationCount":  count,
			"toolOverrides":         map[string]interface{}{"imageGen": true},
			"sendFinalMetadata":     true,
		}

		bodyBytes, err := json.Marshal(payload)
		if err != nil {
			lastErr = err
			span := GrokImageAttemptSpan{
				AccountID: lease.ID,
				Email:     lease.Email,
				Proxy:     lease.Proxy,
				Status:    500,
				Error:     err.Error(),
			}
			res.Spans = append(res.Spans, span)
			traceParts = append(traceParts, fmt.Sprintf("%s(500:marshal_err)", lease.ID))
			break
		}

		reqURL := p.baseURL + "/rest/app-chat/conversations/new"
		resp, effectiveProxy, err := p.doRequestWithProxyFallback(ctx, lease.Proxy, func() (*http.Request, error) {
			req, reqErr := http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(bodyBytes))
			if reqErr != nil {
				return nil, reqErr
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "text/event-stream")
			req.Header.Set("User-Agent", grokBrowserUserAgent)
			req.Header.Set("Origin", p.baseURL)
			req.Header.Set("Referer", p.baseURL+"/")
			req.Header.Set("Cookie", lease.Cookie)
			return req, nil
		})
		if err != nil {
			p.reportOutcomeByID(lease.ID, false, 0, err.Error())
			lastErr = err
			span := GrokImageAttemptSpan{
				AccountID: lease.ID,
				Email:     lease.Email,
				Proxy:     lease.Proxy,
				Status:    0,
				Error:     err.Error(),
			}
			res.Spans = append(res.Spans, span)
			traceParts = append(traceParts, fmt.Sprintf("%s(err:%s)", lease.ID, sanitizeTraceErr(err.Error())))
			continue
		}

		if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
			resp.Body.Close()
			p.reportOutcomeByID(lease.ID, false, resp.StatusCode, "Invalid credentials")
			lastErr = fmt.Errorf("grok account %s invalid credentials (%d)", lease.ID, resp.StatusCode)
			span := GrokImageAttemptSpan{
				AccountID: lease.ID,
				Email:     lease.Email,
				Proxy:     lease.Proxy,
				Status:    resp.StatusCode,
				Error:     "Invalid credentials",
			}
			res.Spans = append(res.Spans, span)
			traceParts = append(traceParts, fmt.Sprintf("%s(%d:auth_fail)", lease.ID, resp.StatusCode))
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			p.reportOutcomeByID(lease.ID, false, resp.StatusCode, "Rate limited")
			lastErr = fmt.Errorf("grok account %s rate limited (429)", lease.ID)
			span := GrokImageAttemptSpan{
				AccountID: lease.ID,
				Email:     lease.Email,
				Proxy:     lease.Proxy,
				Status:    resp.StatusCode,
				Error:     "Rate limited",
			}
			res.Spans = append(res.Spans, span)
			traceParts = append(traceParts, fmt.Sprintf("%s(429:rate_limit)", lease.ID))
			continue
		}

		if resp.StatusCode != http.StatusOK {
			bodySnippet, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			resp.Body.Close()
			errMsg := fmt.Sprintf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(bodySnippet)))
			p.reportOutcomeByID(lease.ID, false, resp.StatusCode, errMsg)
			lastErr = fmt.Errorf("grok upstream returned %s", errMsg)
			span := GrokImageAttemptSpan{
				AccountID: lease.ID,
				Email:     lease.Email,
				Proxy:     lease.Proxy,
				Status:    resp.StatusCode,
				Error:     errMsg,
			}
			res.Spans = append(res.Spans, span)
			traceParts = append(traceParts, fmt.Sprintf("%s(%d:status_err)", lease.ID, resp.StatusCode))
			continue
		}

		// 解析图像流响应
		rawImageURL, parseErr := parseGrokImageStream(resp.Body)
		resp.Body.Close()
		if parseErr != nil {
			p.reportOutcomeByID(lease.ID, false, 200, parseErr.Error())
			lastErr = parseErr
			span := GrokImageAttemptSpan{
				AccountID: lease.ID,
				Email:     lease.Email,
				Proxy:     lease.Proxy,
				Status:    200,
				Error:     parseErr.Error(),
			}
			res.Spans = append(res.Spans, span)
			traceParts = append(traceParts, fmt.Sprintf("%s(200:parse_err)", lease.ID))
			continue
		}

		// 使用当前已登录账号的鉴权 Cookie 与实际生效出口下载图像字节
		imgBytes, dlErr := p.downloadGrokImageAsset(ctx, rawImageURL, lease.Cookie, effectiveProxy)
		if dlErr != nil {
			p.reportOutcomeByID(lease.ID, false, 200, dlErr.Error())
			lastErr = dlErr
			span := GrokImageAttemptSpan{
				AccountID: lease.ID,
				Email:     lease.Email,
				Proxy:     lease.Proxy,
				Status:    200,
				Error:     dlErr.Error(),
			}
			res.Spans = append(res.Spans, span)
			traceParts = append(traceParts, fmt.Sprintf("%s(200:dl_err)", lease.ID))
			continue
		}

		// 成功返回
		p.reportOutcomeByID(lease.ID, true, 200, "")
		span := GrokImageAttemptSpan{
			AccountID: lease.ID,
			Email:     lease.Email,
			Proxy:     lease.Proxy,
			Status:    200,
		}
		res.Spans = append(res.Spans, span)
		traceParts = append(traceParts, fmt.Sprintf("%s(200:ok)", lease.ID))

		b64 := base64.StdEncoding.EncodeToString(imgBytes)
		p.mu.RLock()
		assetBaseURL := p.assetBaseURL
		p.mu.RUnlock()
		fullAssetURL := resolveGrokAssetURL(assetBaseURL, rawImageURL)
		res.Images = []GrokImageItem{
			{
				URL:           fullAssetURL,
				B64JSON:       b64,
				RevisedPrompt: prompt,
			},
		}
		res.DurationMs = time.Since(start).Milliseconds()
		res.Trace = strings.Join(traceParts, " -> ")
		return res, nil
	}

	res.DurationMs = time.Since(start).Milliseconds()
	res.Trace = strings.Join(traceParts, " -> ")
	if lastErr == nil {
		lastErr = ErrNoGrokAccountAvailable
	}
	return res, lastErr
}

// GenerateImage 使用 Grok Aurora 逆向生图（兼容旧接口签名）
func (p *GrokProvider) GenerateImage(ctx context.Context, prompt, model, size string) (string, error) {
	res, err := p.GenerateImageAsset(ctx, prompt, model, size, 1)
	if err != nil {
		return "", err
	}
	if len(res.Images) == 0 {
		return "", errors.New("grok image generation returned no images")
	}
	if res.Images[0].URL != "" {
		return res.Images[0].URL, nil
	}
	if res.Images[0].B64JSON != "" {
		return "data:image/jpeg;base64," + res.Images[0].B64JSON, nil
	}
	return "", errors.New("empty grok image result")
}
