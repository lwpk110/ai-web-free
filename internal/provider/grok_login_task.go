package provider

import (
	"context"
	"errors"
	"strings"
	"time"

	"ai-web-free/internal/accounts"

	"github.com/google/uuid"
)

type grokLoginQueueItem struct {
	accountID string
	headless  bool
}

func (p *GrokProvider) findGrokAccountLocked(id string) *grokAccount {
	for _, account := range p.accounts {
		if account.ID == id {
			return account
		}
	}
	return nil
}

func (p *GrokProvider) grokLoginTarget(id string) (GrokCardAccount, string, *KamiLoginTaskResult) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	account := p.findGrokAccountLocked(id)
	if account == nil {
		return GrokCardAccount{}, "", &KamiLoginTaskResult{AccountID: id, Error: "账号不存在"}
	}
	card := GrokCardAccount{
		Email:      account.Email,
		Password:   account.Password,
		TOTPSecret: account.TOTPSecret,
		OldCookie:  account.Cookie,
		Proxy:      account.Proxy,
	}
	result := KamiLoginTaskResult{AccountID: account.ID, Email: account.Email}
	switch {
	case account.Email == "" || account.Password == "" || account.TOTPSecret == "":
		result.Error = "账号未配置完整邮箱/密码/2FA 卡密凭据"
	case account.Status == accounts.StatusDisabled:
		result.Error = "账号已停用"
	case account.ActiveAction == "login" && account.ActionPhase != "queued":
		result.Error = "该账号正在后台重登中"
	default:
		return card, account.Proxy, nil
	}
	return GrokCardAccount{}, "", &result
}

func (p *GrokProvider) setGrokAccountActionState(id, phase, taskID string, queuePos int, startedAt time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	account := p.findGrokAccountLocked(id)
	if account == nil {
		return
	}
	account.ActiveAction = "login"
	account.ActionPhase = phase
	account.ActionPhaseLabel = KamiPhaseLabel(phase)
	account.ActionTaskID = taskID
	account.ActionQueuePos = queuePos
	if !startedAt.IsZero() {
		account.ActionStartedAt = startedAt
	} else if account.ActionStartedAt.IsZero() {
		account.ActionStartedAt = time.Now()
	}
}

func (p *GrokProvider) rebuildGrokLoginQueueSnapshotLocked(task *KamiLoginTask) {
	queuedIDs := make([]string, 0, len(p.grokLoginQueue))
	p.mu.Lock()
	for index, item := range p.grokLoginQueue {
		queuedIDs = append(queuedIDs, item.accountID)
		account := p.findGrokAccountLocked(item.accountID)
		if account == nil {
			continue
		}
		account.ActiveAction = "login"
		account.ActionPhase = "queued"
		account.ActionPhaseLabel = KamiPhaseLabel("queued")
		account.ActionTaskID = task.ID
		account.ActionQueuePos = index + 1
	}
	p.mu.Unlock()
	task.QueuedAccountIDs = queuedIDs
	task.UpdatedAt = time.Now().Format(time.RFC3339)
}

// StartKamiLoginBatch queues Grok card accounts into one serialized browser-login task.
func (p *GrokProvider) StartKamiLoginBatch(ids []string, headless bool) (KamiLoginTask, error) {
	if len(ids) == 0 || len(ids) > 10000 {
		return KamiLoginTask{}, errors.New("请选择 1 到 10000 个账号")
	}

	normalized := make([]string, 0, len(ids))
	seen := make(map[string]struct{}, len(ids))
	for _, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		normalized = append(normalized, id)
	}
	if len(normalized) == 0 {
		return KamiLoginTask{}, errors.New("请选择至少一个账号")
	}

	p.grokLoginMu.Lock()
	if p.grokLoginRunning != "" {
		if existing := p.grokLoginTasks[p.grokLoginRunning]; existing != nil && (existing.Status == "queued" || existing.Status == "running") {
			alreadyQueued := make(map[string]struct{}, len(p.grokLoginQueue)+1)
			if existing.CurrentAccountID != "" {
				alreadyQueued[existing.CurrentAccountID] = struct{}{}
			}
			for _, item := range p.grokLoginQueue {
				alreadyQueued[item.accountID] = struct{}{}
			}
			for _, id := range normalized {
				if _, duplicate := alreadyQueued[id]; duplicate {
					continue
				}
				_, _, failure := p.grokLoginTarget(id)
				if failure != nil {
					existing.Total++
					existing.Completed++
					existing.Failed++
					existing.Results = append(existing.Results, *failure)
					continue
				}
				alreadyQueued[id] = struct{}{}
				existing.Total++
				p.grokLoginQueue = append(p.grokLoginQueue, grokLoginQueueItem{accountID: id, headless: headless})
			}
			p.rebuildGrokLoginQueueSnapshotLocked(existing)
			snapshot := cloneKamiLoginTask(existing)
			p.grokLoginMu.Unlock()
			return snapshot, nil
		}
	}

	now := time.Now().Format(time.RFC3339)
	task := &KamiLoginTask{
		ID:        uuid.NewString(),
		Status:    "queued",
		Total:     len(normalized),
		Results:   make([]KamiLoginTaskResult, 0, len(normalized)),
		CreatedAt: now,
		UpdatedAt: now,
	}
	targets := make([]grokLoginQueueItem, 0, len(normalized))
	for _, id := range normalized {
		if _, _, failure := p.grokLoginTarget(id); failure != nil {
			task.Completed++
			task.Failed++
			task.Results = append(task.Results, *failure)
			continue
		}
		targets = append(targets, grokLoginQueueItem{accountID: id, headless: headless})
	}
	if len(targets) == 0 {
		task.Status = "completed"
	} else {
		p.grokLoginQueue = append(p.grokLoginQueue[:0], targets...)
		p.rebuildGrokLoginQueueSnapshotLocked(task)
		p.grokLoginRunning = task.ID
	}

	p.grokLoginTasks[task.ID] = task
	p.grokLoginTaskOrder = append(p.grokLoginTaskOrder, task.ID)
	for len(p.grokLoginTaskOrder) > 20 {
		oldest := p.grokLoginTaskOrder[0]
		if oldest == p.grokLoginRunning {
			break
		}
		p.grokLoginTaskOrder = p.grokLoginTaskOrder[1:]
		delete(p.grokLoginTasks, oldest)
	}
	snapshot := cloneKamiLoginTask(task)
	p.grokLoginMu.Unlock()

	if len(targets) > 0 {
		go p.runGrokLoginTask(task.ID)
	}
	return snapshot, nil
}

// StartKamiPendingLogin starts a login task for pending or expired Grok card accounts.
func (p *GrokProvider) StartKamiPendingLogin(force, headless bool) (KamiLoginTask, error) {
	p.mu.RLock()
	ids := make([]string, 0, len(p.accounts))
	for _, account := range p.accounts {
		if account.Email == "" || account.Password == "" || account.TOTPSecret == "" || account.Status == accounts.StatusDisabled {
			continue
		}
		if !force && account.Status != accounts.StatusPending && account.Status != accounts.StatusExpired {
			continue
		}
		ids = append(ids, account.ID)
	}
	p.mu.RUnlock()
	if len(ids) == 0 {
		return KamiLoginTask{}, errors.New("没有需要登录的 Grok 卡密账号")
	}
	return p.StartKamiLoginBatch(ids, headless)
}

// KamiLoginTaskSnapshot returns a copied Grok login-task state for polling.
func (p *GrokProvider) KamiLoginTaskSnapshot(id string) (KamiLoginTask, bool) {
	p.grokLoginMu.Lock()
	defer p.grokLoginMu.Unlock()
	task := p.grokLoginTasks[strings.TrimSpace(id)]
	if task == nil {
		return KamiLoginTask{}, false
	}
	return cloneKamiLoginTask(task), true
}

// ActiveKamiLoginTask returns the active Grok login task or the latest unfinished banner.
func (p *GrokProvider) ActiveKamiLoginTask() *KamiLoginTask {
	p.grokLoginMu.Lock()
	defer p.grokLoginMu.Unlock()
	if p.grokLoginRunning != "" {
		if task := p.grokLoginTasks[p.grokLoginRunning]; task != nil {
			snapshot := cloneKamiLoginTask(task)
			return &snapshot
		}
	}
	for index := len(p.grokLoginTaskOrder) - 1; index >= 0; index-- {
		task := p.grokLoginTasks[p.grokLoginTaskOrder[index]]
		if task != nil && !task.Dismissed {
			snapshot := cloneKamiLoginTask(task)
			return &snapshot
		}
	}
	return nil
}

// CancelKamiLogin removes waiting Grok accounts from a running login queue.
func (p *GrokProvider) CancelKamiLogin(taskID, accountID string, cancelAll bool) (*KamiLoginTask, int, error) {
	targets := make(map[string]bool, 1)
	if id := strings.TrimSpace(accountID); id != "" {
		targets[id] = true
	}
	return p.cancelGrokLoginTargets(taskID, targets, cancelAll)
}

// CancelKamiLoginBatch removes selected waiting Grok accounts from a running login queue.
func (p *GrokProvider) CancelKamiLoginBatch(taskID string, accountIDs []string) (*KamiLoginTask, int, error) {
	targets := make(map[string]bool, len(accountIDs))
	for _, raw := range accountIDs {
		if id := strings.TrimSpace(raw); id != "" {
			targets[id] = true
		}
	}
	return p.cancelGrokLoginTargets(taskID, targets, false)
}

func (p *GrokProvider) cancelGrokLoginTargets(taskID string, targets map[string]bool, cancelAll bool) (*KamiLoginTask, int, error) {
	p.grokLoginMu.Lock()
	selectedTaskID := strings.TrimSpace(taskID)
	if selectedTaskID == "" {
		selectedTaskID = p.grokLoginRunning
	}
	task := p.grokLoginTasks[selectedTaskID]
	if task == nil || (task.Status != "queued" && task.Status != "running") {
		p.grokLoginMu.Unlock()
		return nil, 0, errors.New("当前没有运行中的 Grok 重登任务")
	}
	if !cancelAll && len(targets) == 0 {
		p.grokLoginMu.Unlock()
		return nil, 0, errors.New("请指定要取消排队的账号或选择取消全部排队")
	}
	if !cancelAll && len(targets) == 1 && targets[task.CurrentAccountID] {
		p.grokLoginMu.Unlock()
		return nil, 0, errors.New("该账号正在登录中，无法从等待队列移除")
	}

	remaining := make([]grokLoginQueueItem, 0, len(p.grokLoginQueue))
	cancelled := make([]KamiLoginTaskResult, 0)
	for _, item := range p.grokLoginQueue {
		if cancelAll || targets[item.accountID] {
			result := KamiLoginTaskResult{AccountID: item.accountID, Cancelled: true, Error: "已取消排队"}
			p.mu.Lock()
			if account := p.findGrokAccountLocked(item.accountID); account != nil {
				result.Email = account.Email
				account.ActiveAction = ""
				account.ActionPhase = ""
				account.ActionPhaseLabel = ""
				account.ActionQueuePos = 0
				account.ActionTaskID = ""
				account.ActionStartedAt = time.Time{}
				account.LastAction = "login"
				account.LastActionOK = false
				account.LastActionSummary = "已取消 Grok 重登排队"
				account.LastActionAt = time.Now()
			}
			p.mu.Unlock()
			cancelled = append(cancelled, result)
			continue
		}
		remaining = append(remaining, item)
	}
	if len(cancelled) == 0 {
		p.grokLoginMu.Unlock()
		if cancelAll {
			return nil, 0, errors.New("当前队列中没有等待执行的账号")
		}
		return nil, 0, errors.New("所选账号不在等待队列中")
	}

	p.grokLoginQueue = remaining
	task.Results = append(task.Results, cancelled...)
	for range cancelled {
		task.Completed++
		task.Cancelled++
	}
	p.rebuildGrokLoginQueueSnapshotLocked(task)
	snapshot := cloneKamiLoginTask(task)
	p.grokLoginMu.Unlock()
	return &snapshot, len(cancelled), nil
}

// DismissKamiLoginTask dismisses a completed Grok login-task banner.
func (p *GrokProvider) DismissKamiLoginTask(taskID string) bool {
	p.grokLoginMu.Lock()
	defer p.grokLoginMu.Unlock()
	taskID = strings.TrimSpace(taskID)
	dismissed := false
	if taskID != "" {
		task := p.grokLoginTasks[taskID]
		if task != nil && task.Status == "completed" {
			task.Dismissed = true
			task.UpdatedAt = time.Now().Format(time.RFC3339)
			dismissed = true
		}
		return dismissed
	}
	for _, task := range p.grokLoginTasks {
		if task != nil && task.Status == "completed" && !task.Dismissed {
			task.Dismissed = true
			task.UpdatedAt = time.Now().Format(time.RFC3339)
			dismissed = true
		}
	}
	return dismissed
}

func (p *GrokProvider) updateGrokLoginTask(taskID string, update func(*KamiLoginTask)) {
	p.grokLoginMu.Lock()
	defer p.grokLoginMu.Unlock()
	if task := p.grokLoginTasks[taskID]; task != nil {
		update(task)
		task.UpdatedAt = time.Now().Format(time.RFC3339)
	}
}

func (p *GrokProvider) updateGrokLoginPhase(taskID, accountID, phase string) {
	p.updateGrokLoginTask(taskID, func(task *KamiLoginTask) {
		if task.CurrentAccountID == accountID {
			task.CurrentPhase = phase
			task.CurrentPhaseLabel = KamiPhaseLabel(phase)
		}
	})
	p.setGrokAccountActionState(accountID, phase, taskID, 0, time.Time{})
}

func (p *GrokProvider) applyGrokLoginResult(accountID string, result KamiLoginTaskResult, cookie string) bool {
	p.mu.Lock()
	account := p.findGrokAccountLocked(accountID)
	if account != nil {
		if result.OK {
			account.Cookie = strings.TrimSpace(cookie)
			account.Status = accounts.StatusActive
			account.CooldownUntil = time.Time{}
			account.ActivatedAt = time.Now()
			account.LastError = ""
		} else {
			account.LastError = summarizeActionError(result.Error)
			if account.LastError == "" {
				account.LastError = "未获取到有效 SSO Cookie"
			}
			if account.Cookie == "" {
				account.Status = accounts.StatusPending
			} else {
				account.Status = accounts.StatusExpired
			}
		}
		account.ActiveAction = ""
		account.ActionPhase = ""
		account.ActionPhaseLabel = ""
		account.ActionQueuePos = 0
		account.ActionTaskID = ""
		account.ActionStartedAt = time.Time{}
		account.LastAction = "login"
		account.LastActionOK = result.OK
		if result.OK {
			account.LastActionSummary = "重登成功，SSO Cookie 已更新"
		} else {
			account.LastActionSummary = "重登失败: " + account.LastError
		}
		account.LastActionAt = time.Now()
	}
	p.mu.Unlock()
	return account != nil
}

func (p *GrokProvider) recordGrokLoginResult(taskID string, result KamiLoginTaskResult, cookie string) {
	_ = p.applyGrokLoginResult(result.AccountID, result, cookie)
	if result.OK && p.persistFile != "" {
		go p.persistToDisk()
	}
	p.grokLoginMu.Lock()
	if task := p.grokLoginTasks[taskID]; task != nil {
		task.Results = append(task.Results, result)
		task.Completed++
		if result.OK {
			task.Success++
		} else {
			task.Failed++
		}
		task.CurrentAccountID = ""
		task.CurrentEmail = ""
		task.CurrentPhase = ""
		task.CurrentPhaseLabel = ""
		task.CurrentStartedAt = ""
		task.UpdatedAt = time.Now().Format(time.RFC3339)
	}
	p.grokLoginMu.Unlock()
}

func (p *GrokProvider) completeGrokLoginTaskLocked(taskID string) {
	if task := p.grokLoginTasks[taskID]; task != nil {
		task.Status = "completed"
		task.CurrentAccountID = ""
		task.CurrentEmail = ""
		task.CurrentPhase = ""
		task.CurrentPhaseLabel = ""
		task.CurrentStartedAt = ""
		task.QueuedAccountIDs = nil
		task.UpdatedAt = time.Now().Format(time.RFC3339)
	}
	if p.grokLoginRunning == taskID {
		p.grokLoginRunning = ""
	}
}

func (p *GrokProvider) grokLoginCooldownDuration() time.Duration {
	p.grokLoginMu.Lock()
	cooldown := p.grokLoginCooldown
	p.grokLoginMu.Unlock()
	if cooldown < 0 {
		return 0
	}
	return cooldown
}

func (p *GrokProvider) runGrokLoginTask(taskID string) {
	p.updateGrokLoginTask(taskID, func(task *KamiLoginTask) {
		task.Status = "running"
	})
	loginFn := p.grokLoginFn
	cooldown := p.grokLoginCooldownDuration()
	ranBrowser := false

	for {
		if ranBrowser {
			deadline := time.Now().Add(cooldown)
			for time.Now().Before(deadline) {
				step := time.Until(deadline)
				if step > 100*time.Millisecond {
					step = 100 * time.Millisecond
				}
				if step > 0 {
					time.Sleep(step)
				}
				p.grokLoginMu.Lock()
				empty := len(p.grokLoginQueue) == 0
				p.grokLoginMu.Unlock()
				if empty {
					break
				}
			}
		}

		p.grokLoginMu.Lock()
		task := p.grokLoginTasks[taskID]
		if task == nil || len(p.grokLoginQueue) == 0 {
			p.completeGrokLoginTaskLocked(taskID)
			p.grokLoginMu.Unlock()
			return
		}
		item := p.grokLoginQueue[0]
		p.grokLoginQueue = p.grokLoginQueue[1:]
		startedAt := time.Now()
		task.CurrentAccountID = item.accountID
		task.CurrentEmail = ""
		task.CurrentPhase = "launch_browser"
		task.CurrentPhaseLabel = KamiPhaseLabel("launch_browser")
		task.CurrentStartedAt = startedAt.Format(time.RFC3339)
		p.rebuildGrokLoginQueueSnapshotLocked(task)
		p.grokLoginMu.Unlock()

		card, proxyURL, failure := p.grokLoginTarget(item.accountID)
		if failure != nil {
			p.recordGrokLoginResult(taskID, *failure, "")
			continue
		}
		task.CurrentEmail = card.Email
		p.updateGrokLoginTask(taskID, func(current *KamiLoginTask) {
			if current.CurrentAccountID == item.accountID {
				current.CurrentEmail = card.Email
			}
		})
		p.setGrokAccountActionState(item.accountID, "launch_browser", taskID, 0, startedAt)
		ranBrowser = true

		loginCtx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
		p.updateGrokLoginPhase(taskID, item.accountID, "open_login")
		cookie, err := loginFn(loginCtx, card, GrokLoginOptions{
			ProxyURL: proxyURL,
			Headless: item.headless,
			Timeout:  180 * time.Second,
		})
		cancel()

		result := KamiLoginTaskResult{AccountID: item.accountID, Email: card.Email}
		if err != nil {
			result.Error = err.Error()
		} else if strings.TrimSpace(cookie) == "" {
			result.Error = "未获取到有效 SSO Cookie"
		} else {
			result.OK = true
			result.Error = ""
		}
		p.recordGrokLoginResult(taskID, result, cookie)
	}
}
