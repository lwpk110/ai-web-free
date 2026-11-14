package provider

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/audit"
	"ai-web-free/internal/chatgpt"
	"ai-web-free/internal/config"
	"ai-web-free/internal/proxy"

	"github.com/google/uuid"
)

type ChatGPTProvider struct {
	pool               *accounts.Pool
	proxyPool          *proxy.Pool
	cfg                *config.Config
	renewFn            func(*accounts.Account) bool
	kamiWorker         *KamiRenewWorker
	auditLog           *audit.RingBuffer
	fileMu             sync.Mutex
	kamiLoginMu        sync.Mutex
	kamiLoginTasks     map[string]*KamiLoginTask
	kamiLoginTaskOrder []string
	kamiLoginRunning   string
	kamiLoginQueue     []kamiLoginQueueItem
	kamiLoginFn        KamiBatchLoginExecutor
	kamiLoginCooldown  time.Duration
	probeMu            sync.Mutex
	currentProbeTask   *ProbeTask
}

func NewChatGPTProvider(pool *accounts.Pool, proxyPool *proxy.Pool, cfg *config.Config, renewFn func(*accounts.Account) bool) *ChatGPTProvider {
	return &ChatGPTProvider{
		pool:           pool,
		proxyPool:      proxyPool,
		cfg:            cfg,
		renewFn:        renewFn,
		kamiLoginTasks: make(map[string]*KamiLoginTask),
		kamiLoginFn:    RunKamiPlaywrightLoginWithProgress,
	}
}

// SetKamiLoginExecutor 替换批量/单号卡密登录执行器（主要用于单元测试注入 mock）。
func (p *ChatGPTProvider) SetKamiLoginExecutor(fn KamiBatchLoginExecutor) {
	if fn == nil {
		return
	}
	p.kamiLoginMu.Lock()
	p.kamiLoginFn = fn
	p.kamiLoginMu.Unlock()
}

// SetKamiWorker 绑定卡密后台自动续期队列。
func (p *ChatGPTProvider) SetKamiWorker(w *KamiRenewWorker) {
	p.kamiWorker = w
}

// KamiWorker 返回已绑定的卡密后台自动续期队列。
func (p *ChatGPTProvider) KamiWorker() *KamiRenewWorker {
	return p.kamiWorker
}

// SetAuditLog 绑定请求与号池运维审计缓冲。
func (p *ChatGPTProvider) SetAuditLog(a *audit.RingBuffer) {
	p.auditLog = a
}

func (p *ChatGPTProvider) reportProbeProxyFailure(acct *accounts.Account, errMsg, auditPrefix string) {
	if p.proxyPool == nil || acct == nil || acct.Proxy == "" {
		return
	}
	failedProxy := acct.Proxy
	if tripped := p.proxyPool.ReportFailure(failedProxy, errMsg); tripped {
		migrated := p.pool.ReconcileProxies(
			func(u string) bool { return !p.proxyPool.IsHealthy(u) },
			func(excl map[string]bool) string { return p.proxyPool.AllocateHealthy(excl) },
		)
		if p.auditLog != nil {
			p.auditLog.RecordEvent("/internal/proxy-breaker", acct.Email, failedProxy, fmt.Sprintf("%s%d", auditPrefix, migrated), errMsg)
		}
	}
}

func (p *ChatGPTProvider) Descriptor() ProviderDescriptor {
	featured := []string{
		"gpt-5-6",
		"gpt-5-6-mini",
		"gpt-6",
		"gpt-6-mini",
		"gpt-image-2.5",
		"gpt-image-2.5-sunburst",
		"gpt-image-2.5-flare",
		"gpt-image-2.6",
		"gpt-image-2",
		"dall-e-3",
	}
	if p.pool != nil {
		if cached, _ := p.pool.CachedModels(); len(cached) > 0 {
			seen := make(map[string]bool, len(cached)+5)
			merged := make([]string, 0, len(cached)+5)
			for _, id := range cached {
				if id != "" && !seen[id] {
					seen[id] = true
					merged = append(merged, id)
				}
			}
			for _, img := range []string{"gpt-image-2.5-sunburst", "gpt-image-2.5-flare", "gpt-image-2.5", "gpt-image-2.6", "dall-e-3"} {
				if !seen[img] {
					seen[img] = true
					merged = append(merged, img)
				}
			}
			featured = merged
		}
	}
	return ProviderDescriptor{
		ID:          "chatgpt",
		Name:        "ChatGPT Web",
		Vendor:      "OpenAI",
		Status:      "enabled",
		Version:     "2.6.0",
		Description: "ChatGPT Web 免费号池与多模态逆向引擎（支持 GPT-5.6 / GPT-6 / GPT-Image-2.5 & 2.6 / Responses / TTS / STT）",
		SupportedAccountTypes: []string{
			"free",
			"noauth",
			"puid",
		},
		CredentialFormats: []string{
			"access_token",
			"session_token",
			"refresh_token",
			"free_uuid",
			"kami",
			"sub2api_json",
		},
		Capabilities: []string{
			"chat",
			"responses",
			"tool_calling",
			"image_generation",
			"image_edit",
			"image_variation",
			"tts",
			"transcribe",
			"file_upload",
			"websocket",
		},
		FeaturedModels: featured,
	}
}

func (p *ChatGPTProvider) Stats() accounts.PoolStats {
	return p.pool.Stats()
}

func (p *ChatGPTProvider) ListAccounts(typeFilter, statusFilter, search string) []accounts.AccountSnapshot {
	return p.pool.ListSnapshots(typeFilter, statusFilter, search)
}

func (p *ChatGPTProvider) allocateProxy() string {
	if p.proxyPool != nil {
		if px := p.proxyPool.Allocate(); px != "" {
			return px
		}
	}
	if p.cfg != nil {
		if p.cfg.ProxyURL != "" {
			return p.cfg.ProxyURL
		}
		if p.cfg.HTTPProxy != "" {
			return p.cfg.HTTPProxy
		}
	}
	return ""
}

func (p *ChatGPTProvider) ImportCredentials(req ImportRequest) (ImportResult, error) {
	credType := strings.ToLower(strings.TrimSpace(req.CredentialType))
	if credType == "" {
		credType = "access_token"
	}

	if credType == "kami" {
		raw := req.RawText
		if strings.TrimSpace(raw) == "" && len(req.Tokens) > 0 {
			raw = strings.Join(req.Tokens, "\n")
		}
		return p.importKamiAccounts(raw, req.Persist, req.AutoLogin, req.Headless)
	}
	if credType == "sub2api_json" {
		raw := req.RawText
		if strings.TrimSpace(raw) == "" && len(req.Tokens) > 0 {
			raw = strings.Join(req.Tokens, "\n")
		}
		return p.importSub2APIJSON(raw, req.Persist)
	}

	var lines []string
	for _, t := range req.Tokens {
		if s := strings.TrimSpace(t); s != "" {
			lines = append(lines, s)
		}
	}
	if req.RawText != "" {
		for _, line := range strings.Split(req.RawText, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			lines = append(lines, line)
		}
	}

	var res ImportResult
	profiles := accounts.DefaultProfiles

	// 特殊情况：自动生成免登 UUID 号池
	if credType == "free_uuid" && len(lines) == 0 && req.GenerateCount > 0 {
		count := req.GenerateCount
		if count > 500 {
			count = 500
		}
		for i := 0; i < count; i++ {
			uid := uuid.NewString()
			acct := accounts.CreateAccount(uid, accounts.TypeNoAuth, profiles)
			acct.Proxy = p.allocateProxy()
			acct.Status = accounts.StatusActive
			_ = acct.InitClient()
			p.pool.AddAccount(acct)
			res.Added++
		}
		res.TotalPool = p.pool.Stats().Total
		return res, nil
	}

	if len(lines) == 0 {
		return res, errors.New("未提供有效的凭据内容")
	}

	for _, rawLine := range lines {
		parts := strings.SplitN(rawLine, ":", 2)
		token := strings.TrimSpace(parts[0])
		teamID := ""
		if len(parts) > 1 {
			teamID = strings.TrimSpace(parts[1])
		}
		if token == "" {
			continue
		}

		switch credType {
		case "access_token":
			chatGPTID := accounts.ExtractChatGPTAccountID(token)
			if p.pool.HasChatGPTAccountID(chatGPTID, token) {
				res.Skipped++
				continue
			}
			acct := accounts.CreateAccount(token, accounts.TypeFree, profiles)
			acct.TeamUserID = teamID
			acct.SyncJWTMetadata()
			acct.Proxy = p.allocateProxy()
			acct.Status = accounts.StatusActive
			_ = acct.InitClient()
			p.pool.AddAccount(acct)
			res.Added++

		case "refresh_token":
			acct := accounts.CreateAccount("", accounts.TypeFree, profiles)
			acct.RefreshToken = token
			acct.TeamUserID = teamID
			acct.Proxy = p.allocateProxy()
			_ = acct.InitClient()
			if p.renewFn != nil && p.renewFn(acct) {
				acct.SyncJWTMetadata()
				if p.pool.HasChatGPTAccountID(acct.ChatGPTAccountID, acct.Token) {
					res.Skipped++
					continue
				}
				acct.Status = accounts.StatusActive
				p.pool.AddAccount(acct)
				res.Added++
			} else {
				res.Failed++
				res.Messages = append(res.Messages, "refresh_token 换取失败: "+token[:minLen(12, len(token))]+"...")
			}

		case "session_token":
			acct := accounts.CreateAccount("", accounts.TypeFree, profiles)
			acct.SessionToken = token
			acct.TeamUserID = teamID
			acct.Proxy = p.allocateProxy()
			_ = acct.InitClient()
			if p.renewFn != nil && p.renewFn(acct) {
				acct.SyncJWTMetadata()
				if p.pool.HasChatGPTAccountID(acct.ChatGPTAccountID, acct.Token) {
					res.Skipped++
					continue
				}
				acct.Status = accounts.StatusActive
				p.pool.AddAccount(acct)
				res.Added++
			} else {
				res.Failed++
				res.Messages = append(res.Messages, "session_token 换取失败")
			}

		case "free_uuid":
			if p.pool.HasChatGPTAccountID("", token) {
				res.Skipped++
				continue
			}
			acct := accounts.CreateAccount(token, accounts.TypeNoAuth, profiles)
			acct.Proxy = p.allocateProxy()
			acct.Status = accounts.StatusActive
			_ = acct.InitClient()
			p.pool.AddAccount(acct)
			res.Added++

		default:
			return res, fmt.Errorf("不支持的凭据类型: %s", credType)
		}
	}

	if req.Persist && credType == "access_token" && res.Added > 0 {
		if err := p.PersistAccessTokens(); err == nil {
			res.Persisted = true
		}
	}

	res.TotalPool = p.pool.Stats().Total
	return res, nil
}

// ProbeTask 表示全池批量测活的异步/同步执行进度快照。
type ProbeTask struct {
	ID        string                   `json:"id"`
	Status    string                   `json:"status"` // "running" | "completed"
	Total     int                      `json:"total"`
	Probed    int                      `json:"probed"`
	Passed    int                      `json:"passed"`
	Failed    int                      `json:"failed"`
	Results   []map[string]interface{} `json:"results,omitempty"`
	CreatedAt string                   `json:"created_at"`
	UpdatedAt string                   `json:"updated_at"`
	Dismissed bool                     `json:"dismissed,omitempty"`
}

func cloneProbeTask(t *ProbeTask) ProbeTask {
	cp := *t
	if t.Results != nil {
		cp.Results = append([]map[string]interface{}(nil), t.Results...)
	}
	return cp
}

// ActiveProbeTask 返回运行中或最近一次未关闭的全池测活任务快照。
func (p *ChatGPTProvider) ActiveProbeTask() *ProbeTask {
	p.probeMu.Lock()
	defer p.probeMu.Unlock()
	if p.currentProbeTask == nil || p.currentProbeTask.Dismissed {
		return nil
	}
	cp := cloneProbeTask(p.currentProbeTask)
	return &cp
}

// DismissProbeTask 标记已完成的全池测活战报为已关闭。
func (p *ChatGPTProvider) DismissProbeTask(taskID string) bool {
	p.probeMu.Lock()
	defer p.probeMu.Unlock()
	if p.currentProbeTask == nil || p.currentProbeTask.Status == "running" {
		return false
	}
	if taskID != "" && p.currentProbeTask.ID != taskID {
		return false
	}
	p.currentProbeTask.Dismissed = true
	return true
}

// StartOrRunProbeAll 启动全池并发测活；async=true 时立即返回任务快照并后台执行，async=false 时等待完成。
func (p *ChatGPTProvider) StartOrRunProbeAll(async bool) (ProbeTask, error) {
	p.probeMu.Lock()
	if async && p.currentProbeTask != nil && p.currentProbeTask.Status == "running" {
		cp := cloneProbeTask(p.currentProbeTask)
		p.probeMu.Unlock()
		return cp, nil
	}

	snapshots := p.ListAccounts("free", "", "")
	snapshots = append(snapshots, p.ListAccounts("puid", "", "")...)
	targets := make([]accounts.AccountSnapshot, 0, len(snapshots))
	for _, snap := range snapshots {
		if snap.Status == "disabled" || snap.ActiveAction == "login" || snap.ActiveAction == "renew" {
			continue
		}
		targets = append(targets, snap)
	}

	now := time.Now()
	nowStr := now.Format(time.RFC3339)
	task := &ProbeTask{
		ID:        uuid.NewString(),
		Status:    "running",
		Total:     len(targets),
		Results:   make([]map[string]interface{}, 0, len(targets)),
		CreatedAt: nowStr,
		UpdatedAt: nowStr,
	}
	if len(targets) == 0 {
		task.Status = "completed"
		p.currentProbeTask = task
		cp := cloneProbeTask(task)
		p.probeMu.Unlock()
		return cp, nil
	}
	p.currentProbeTask = task
	for _, snap := range targets {
		p.pool.SetAccountActionState(snap.ID, "probe", "queued", "等待测活", task.ID, 0, now)
	}
	initial := cloneProbeTask(task)
	p.probeMu.Unlock()

	run := func() ProbeTask {
		sem := make(chan struct{}, 3)
		var wg sync.WaitGroup
		for _, snap := range targets {
			wg.Add(1)
			go func(id string) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				acct := p.pool.GetByID(id)
				var res map[string]interface{}
				var err error
				if acct == nil {
					err = errors.New("未找到对应账号")
				} else {
					res, err = p.executeProbe(acct, task.ID)
				}

				p.probeMu.Lock()
				if err != nil {
					task.Failed++
					task.Results = append(task.Results, map[string]interface{}{"id": id, "ok": false, "error": err.Error()})
				} else {
					if okFlag, _ := res["ok"].(bool); okFlag {
						task.Passed++
					} else {
						task.Failed++
					}
					task.Results = append(task.Results, res)
				}
				task.Probed++
				task.UpdatedAt = time.Now().Format(time.RFC3339)
				p.probeMu.Unlock()
			}(snap.ID)
		}
		wg.Wait()

		p.probeMu.Lock()
		task.Status = "completed"
		task.UpdatedAt = time.Now().Format(time.RFC3339)
		cp := cloneProbeTask(task)
		p.probeMu.Unlock()
		return cp
	}

	if async {
		go func() {
			_ = run()
		}()
		return initial, nil
	}
	return run(), nil
}

func (p *ChatGPTProvider) ExecuteAccountAction(id, action string) (map[string]interface{}, error) {
	return p.ExecuteAccountActionWithOpts(id, action, true)
}

// ExecuteAccountActionWithOpts 对单个账号执行运维操作，支持透传 headless 等扩展参数。
func (p *ChatGPTProvider) ExecuteAccountActionWithOpts(id, action string, headless bool) (map[string]interface{}, error) {
	action = strings.ToLower(strings.TrimSpace(action))
	if action == "delete" {
		removed, ok := p.pool.RemoveByID(id)
		if !ok {
			return nil, errors.New("未找到对应账号")
		}
		_ = p.PersistAccessTokens()
		return map[string]interface{}{
			"id":      removed.ID,
			"action":  "delete",
			"deleted": true,
		}, nil
	}

	acct := p.pool.GetByID(id)
	if acct == nil {
		return nil, errors.New("未找到对应账号")
	}

	switch action {
	case "enable":
		acct.CooldownUntil = time.Time{}
		acct.ImageCooldownUntil = time.Time{}
		acct.LastError = ""
		p.pool.SetStatus(acct.ID, accounts.StatusActive)
		return map[string]interface{}{
			"id":     acct.ID,
			"status": accounts.StatusActive.String(),
		}, nil

	case "disable":
		p.pool.SetStatus(acct.ID, accounts.StatusDisabled)
		return map[string]interface{}{
			"id":     acct.ID,
			"status": accounts.StatusDisabled.String(),
		}, nil

	case "refresh":
		if acct.RefreshToken == "" && acct.SessionToken == "" {
			return nil, errors.New("该账号仅包含 AccessToken，无 RefreshToken/SessionToken 可供续期")
		}
		p.pool.SetAccountActionState(acct.ID, "refresh", "refreshing", "正在刷新 Token", "", 0, time.Now())
		if p.renewFn != nil && p.renewFn(acct) {
			acct.SyncJWTMetadata()
			acct.LastChecked = time.Now()
			acct.LastError = ""
			p.pool.SetStatus(acct.ID, accounts.StatusActive)
			p.pool.FinishAccountAction(acct.ID, "refresh", true, "Token 续期成功")
			return map[string]interface{}{
				"id":         acct.ID,
				"status":     acct.Status.String(),
				"expires_at": acct.ExpiresAt.Format(time.RFC3339),
			}, nil
		}
		acct.LastChecked = time.Now()
		acct.LastError = "Token 续期失败"
		p.pool.SetStatus(acct.ID, accounts.StatusExpired)
		p.pool.FinishAccountAction(acct.ID, "refresh", false, "Token 续期失败")
		return nil, errors.New("Token 续期请求失败")

	case "probe":
		return p.executeProbe(acct, "")

	case "totp":
		if acct.TOTPSecret == "" {
			return nil, errors.New("该账号未配置 2FA 密钥")
		}
		code, remaining, err := accounts.GenerateTOTP(acct.TOTPSecret, time.Now())
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{
			"id":                acct.ID,
			"email":             acct.Email,
			"totp_code":         code,
			"remaining_seconds": remaining,
			"message":           fmt.Sprintf("2FA 动态码: %s (剩余 %ds)", code, remaining),
		}, nil

	case "login":
		if acct.Status == accounts.StatusDisabled {
			return nil, errors.New("账号已停用，请先启用后再重登")
		}
		if acct.Email == "" || acct.Password == "" || acct.TOTPSecret == "" {
			return nil, errors.New("该账号未配置邮箱/密码/2FA 卡密凭据，无法自动登录")
		}
		if acct.ActiveAction == "renew" && acct.ActionPhase != "queued" {
			return nil, errors.New("该账号正在后台自动续期中，请稍候")
		}
		if p.kamiWorker != nil {
			p.kamiWorker.CancelQueued(acct.ID)
		}
		if acct.RenewState == "cooldown" {
			p.pool.ResetRenewBackoff(acct)
		}
		task, err := p.StartKamiLoginBatch([]string{acct.ID}, headless)
		if err != nil {
			return nil, err
		}
		msg := fmt.Sprintf("账号 %s 已加入后台重登任务（将实时回显执行阶段）", acct.Email)
		if task.CurrentAccountID != "" && task.CurrentAccountID != acct.ID {
			for idx, qid := range task.QueuedAccountIDs {
				if qid == acct.ID {
					msg = fmt.Sprintf("账号 %s 已加入重登队列（排队 #%d）", acct.Email, idx+1)
					break
				}
			}
		}
		return map[string]interface{}{
			"id":      acct.ID,
			"email":   acct.Email,
			"status":  acct.Status.String(),
			"queued":  true,
			"task_id": task.ID,
			"task":    task,
			"message": msg,
		}, nil

	default:
		return nil, fmt.Errorf("未知操作指令: %s", action)
	}
}

func (p *ChatGPTProvider) executeProbe(acct *accounts.Account, taskID string) (map[string]interface{}, error) {
	start := time.Now()
	acct.LastChecked = start
	p.pool.SetAccountActionState(acct.ID, "probe", "probing", "正在测活模型列表", taskID, 0, start)
	if acct.Client == nil {
		_ = acct.InitClient()
	}
	if acct.Type == accounts.TypeNoAuth {
		latency := time.Since(start).Milliseconds()
		p.pool.FinishAccountAction(acct.ID, "probe", true, fmt.Sprintf("免登就绪 · %dms", latency))
		return map[string]interface{}{
			"id":         acct.ID,
			"ok":         true,
			"status":     acct.Status.String(),
			"latency_ms": latency,
			"message":    "免登 UUID 账号就绪",
		}, nil
	}
	models, httpStatus, err := chatgpt.ListModels(acct.Client, acct)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		errMsg := fmt.Sprintf("HTTP %d: %v", httpStatus, err)
		acct.LastError = errMsg
		reason := chatgpt.ClassifyAccountErrorReason(httpStatus, errMsg)
		// 先记录本次测活结果并清除 "probe" 运行态，再按需触发后续续期入队，避免覆盖 renew 排队态
		p.pool.FinishAccountAction(acct.ID, "probe", false, fmt.Sprintf("测活异常 (HTTP %d)", httpStatus))
		switch reason {
		case chatgpt.ErrCategoryBanned:
			p.pool.MarkBanned(acct, errMsg)
			if p.auditLog != nil {
				p.auditLog.RecordEvent("/internal/account-banned", acct.Email, acct.Proxy, "probe_banned", errMsg)
			}
		case chatgpt.ErrCategoryWAFProxy:
			p.pool.RecordProxyBlock(acct, 2*time.Minute, errMsg)
			p.reportProbeProxyFailure(acct, errMsg, "probe_tripped_migrated_")
		case chatgpt.ErrCategoryTokenExpired:
			p.pool.MarkExpired(acct, errMsg)
			if p.kamiWorker != nil {
				p.kamiWorker.Enqueue(acct, "probe_unauthorized")
			}
		case chatgpt.ErrCategoryImageLimit, chatgpt.ErrCategoryRateLimit:
			isImg := reason == chatgpt.ErrCategoryImageLimit
			cd := 5 * time.Minute
			if isImg {
				cd = 15 * time.Minute
			}
			p.pool.ReportRateLimit(acct, isImg, cd, errMsg)
		case chatgpt.ErrCategoryProxyTransport:
			p.reportProbeProxyFailure(acct, errMsg, "probe_transport_migrated_")
		default:
			if httpStatus == 401 {
				p.pool.MarkExpired(acct, errMsg)
				if p.kamiWorker != nil {
					p.kamiWorker.Enqueue(acct, "probe_unauthorized")
				}
			} else if httpStatus == 429 {
				p.pool.ReportRateLimit(acct, false, 5*time.Minute, errMsg)
			}
		}
		return map[string]interface{}{
			"id":          acct.ID,
			"ok":          false,
			"status":      acct.Status.String(),
			"http_status": httpStatus,
			"latency_ms":  latency,
			"error":       acct.LastError,
		}, nil
	}
	acct.LastError = ""
	acct.CooldownUntil = time.Time{}
	acct.ImageCooldownUntil = time.Time{}
	p.pool.SetStatus(acct.ID, accounts.StatusActive)
	var slugs []string
	for _, m := range models {
		if m.Slug != "" {
			slugs = append(slugs, m.Slug)
		}
	}
	if p.pool != nil {
		p.pool.RecordAccountModels(acct.ID, slugs)
	}
	p.pool.FinishAccountAction(acct.ID, "probe", true, fmt.Sprintf("测活通过 · %dms · %d 模型", latency, len(slugs)))
	return map[string]interface{}{
		"id":           acct.ID,
		"ok":           true,
		"status":       acct.Status.String(),
		"latency_ms":   latency,
		"models_count": len(slugs),
		"models":       slugs,
	}, nil
}

// PersistAccessTokens 将池内非临时 access_token 原子写回 access_tokens.txt
func (p *ChatGPTProvider) PersistAccessTokens() error {
	p.fileMu.Lock()
	defer p.fileMu.Unlock()

	lines := p.pool.ExportAccessTokens()
	content := strings.Join(lines, "\n")
	if len(lines) > 0 {
		content += "\n"
	}
	return writeAtomic("access_tokens.txt", []byte(content))
}

func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp")
	if err != nil {
		return os.WriteFile(path, data, 0644)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(path)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return os.WriteFile(path, data, 0644)
	}
	return nil
}

func minLen(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// RegisterDefaultProviders 注册 ChatGPT 主插件以及预留的多供应商插件槽位
func RegisterDefaultProviders(reg *Registry, chatgptPlugin *ChatGPTProvider) {
	reg.Register(chatgptPlugin)

	reg.Register(NewStubProvider(ProviderDescriptor{
		ID:                    "claude",
		Name:                  "Claude Web",
		Vendor:                "Anthropic",
		Status:                "planned",
		Version:               "0.1.0-alpha",
		Description:           "Claude.ai 免费/Pro SessionKey 号池插件（支持 Claude 4.5 Sonnet / Artifacts 逆向转 OpenAI 格式）",
		SupportedAccountTypes: []string{"free", "pro"},
		CredentialFormats:     []string{"session_key", "oauth_token"},
		Capabilities:          []string{"chat", "tool_calling", "file_upload"},
		FeaturedModels:        []string{"claude-4-5-sonnet", "claude-4-5-haiku"},
	}))

	reg.Register(NewStubProvider(ProviderDescriptor{
		ID:                    "gemini",
		Name:                  "Gemini Web",
		Vendor:                "Google",
		Status:                "planned",
		Version:               "0.1.0-alpha",
		Description:           "Google Gemini Web (__Secure-1PSID) 免费号池插件（支持 Gemini 3 / Imagen 3 逆向生图与对话）",
		SupportedAccountTypes: []string{"free", "advanced"},
		CredentialFormats:     []string{"cookie_1psid"},
		Capabilities:          []string{"chat", "image_generation", "file_upload"},
		FeaturedModels:        []string{"gemini-3-pro", "gemini-3-flash", "imagen-3"},
	}))

	reg.Register(NewStubProvider(ProviderDescriptor{
		ID:                    "grok",
		Name:                  "Grok Web",
		Vendor:                "xAI",
		Status:                "planned",
		Version:               "0.1.0-alpha",
		Description:           "Grok.com / X 免登与 SSO 免费号池插件（支持 Grok-4 / Aurora Image 生图）",
		SupportedAccountTypes: []string{"free", "noauth", "supergrok"},
		CredentialFormats:     []string{"sso_cookie", "anon_token"},
		Capabilities:          []string{"chat", "image_generation", "image_edit"},
		FeaturedModels:        []string{"grok-4", "grok-4-mini", "grok-image"},
	}))

	reg.Register(NewStubProvider(ProviderDescriptor{
		ID:                    "deepseek",
		Name:                  "DeepSeek Web",
		Vendor:                "DeepSeek",
		Status:                "planned",
		Version:               "0.1.0-alpha",
		Description:           "chat.deepseek.com 免费号池插件（支持 DeepSeek-V3.2 / R2 深度思考与联网搜索）",
		SupportedAccountTypes: []string{"free"},
		CredentialFormats:     []string{"user_token"},
		Capabilities:          []string{"chat", "reasoning", "web_search"},
		FeaturedModels:        []string{"deepseek-chat", "deepseek-reasoner"},
	}))
}
