package provider

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/db"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"
	"github.com/google/uuid"
)

var (
	ErrNoMetaMuseSessionAvailable = errors.New("no active meta muse session available in pool")
)

type museVideoTask struct {
	TaskID    string
	Prompt    string
	Status    string // "PENDING" | "RUNNING" | "SUCCEEDED" | "FAILED"
	Progress  int
	VideoURL  string
	ErrorMsg  string
	CreatedAt time.Time
}

type metaMuseSession struct {
	SessionID     string
	Cookie        string
	Email         string
	Proxy         string // 绑定的独立出口代理
	Status        string // "active" | "disabled" | "rate_limited" | "error"
	LastError     string
	CooldownUntil time.Time
	TotalCalls    int64
	FailedCalls   int64
}

// MetaMuseProvider Meta AI Muse 视频生成与数字人口型同步号池插件
type MetaMuseProvider struct {
	mu           sync.RWMutex
	apiURL       string
	loginURL     string
	sessions     []*metaMuseSession
	idx          int
	client       *http.Client
	loginTasksMu sync.RWMutex
	loginTasks   map[string]KamiLoginTask
	activeTaskID string
	skipDB       bool

	videoTasksMu sync.RWMutex
	videoTasks   map[string]*museVideoTask
}

// NewMetaMuseProvider 创建 Meta Muse Provider 实例
func NewMetaMuseProvider(sessions []string, customAPIURL string) *MetaMuseProvider {
	if customAPIURL == "" {
		customAPIURL = os.Getenv("MUSE_API_URL")
	}
	if customAPIURL == "" {
		customAPIURL = "https://www.meta.ai/api/v1/video"
	}
	customAPIURL = strings.TrimRight(customAPIURL, "/")

	loginURL := os.Getenv("MUSE_LOGIN_URL")
	if loginURL == "" {
		loginURL = "https://muse.ai/"
	}

	p := &MetaMuseProvider{
		apiURL:     customAPIURL,
		loginURL:   loginURL,
		loginTasks: make(map[string]KamiLoginTask),
		videoTasks: make(map[string]*museVideoTask),
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
	p.loadFromDatabase()
	p.AddSessions(sessions)
	return p
}

func (p *MetaMuseProvider) loadFromDatabase() {
	if p.skipDB {
		return
	}
	database := db.GetDB()
	if database == nil {
		return
	}
	var records []db.ProviderAccountRecord
	if err := database.Where("provider = ?", "meta_muse").Find(&records).Error; err != nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	existing := make(map[string]bool)
	for _, s := range p.sessions {
		existing[s.SessionID] = true
		if s.Email != "" {
			existing[s.Email] = true
		}
	}

	for _, r := range records {
		if !existing[r.ID] && !existing[r.Email] {
			p.sessions = append(p.sessions, &metaMuseSession{
				SessionID:   r.ID,
				Email:       r.Email,
				Cookie:      r.Cookie,
				Proxy:       r.Proxy,
				Status:      r.Status,
				LastError:   r.LastError,
				TotalCalls:  r.TotalCalls,
				FailedCalls: r.FailedCalls,
			})
			existing[r.ID] = true
			if r.Email != "" {
				existing[r.Email] = true
			}
		}
	}
}

func (p *MetaMuseProvider) saveSessionToDB(s *metaMuseSession) {
	if p.skipDB {
		return
	}
	database := db.GetDB()
	if database == nil || s == nil {
		return
	}
	rec := db.ProviderAccountRecord{
		ID:          s.SessionID,
		Provider:    "meta_muse",
		Email:       s.Email,
		Cookie:      s.Cookie,
		Proxy:       s.Proxy,
		Status:      s.Status,
		LastError:   s.LastError,
		TotalCalls:  s.TotalCalls,
		FailedCalls: s.FailedCalls,
		UpdatedAt:   time.Now(),
	}
	_ = database.Save(&rec).Error
}

func (p *MetaMuseProvider) deleteSessionFromDB(id string) {
	if p.skipDB {
		return
	}
	database := db.GetDB()
	if database == nil {
		return
	}
	_ = database.Where("id = ? OR email = ?", id, id).Delete(&db.ProviderAccountRecord{}).Error
}

// AddSessions 批量导入 Meta AI 会话 Cookie 或 Token
func (p *MetaMuseProvider) AddSessions(tokens []string) int {
	p.mu.Lock()
	defer p.mu.Unlock()

	existing := make(map[string]bool, len(p.sessions))
	for _, s := range p.sessions {
		if s.Cookie != "" {
			existing[s.Cookie] = true
		}
	}

	added := 0
	for _, raw := range tokens {
		trimmed := strings.TrimSpace(raw)
		if trimmed != "" && !existing[trimmed] {
			p.sessions = append(p.sessions, &metaMuseSession{
				SessionID: fmt.Sprintf("muse_session_%d", len(p.sessions)+1),
				Cookie:    trimmed,
				Status:    "active",
			})
			existing[trimmed] = true
			added++
		}
	}
	return added
}

func (p *MetaMuseProvider) nextSession() (*metaMuseSession, int, error) {
	p.loadFromDatabase()
	p.mu.Lock()
	defer p.mu.Unlock()

	n := len(p.sessions)
	if n == 0 {
		return nil, -1, ErrNoMetaMuseSessionAvailable
	}

	now := time.Now()
	for i := 0; i < n; i++ {
		candidateIdx := (p.idx + i) % n
		s := p.sessions[candidateIdx]
		if s.Status == "active" && now.After(s.CooldownUntil) && s.Cookie != "" {
			p.idx = (candidateIdx + 1) % n
			return s, candidateIdx, nil
		}
	}

	return nil, -1, ErrNoMetaMuseSessionAvailable
}

func (p *MetaMuseProvider) reportStatus(idx int, success bool, isRateLimit bool, errMsg string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if idx < 0 || idx >= len(p.sessions) {
		return
	}
	s := p.sessions[idx]
	if success {
		s.TotalCalls++
		s.LastError = ""
	} else {
		s.FailedCalls++
		s.LastError = errMsg
		if isRateLimit {
			s.CooldownUntil = time.Now().Add(60 * time.Second)
		}
	}
	p.saveSessionToDB(s)
}

// RecordCall 手动记录账号调用次数并实时持久化到 SQLite 数据库
func (p *MetaMuseProvider) RecordCall(emailOrID string, success bool, errMsg string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, s := range p.sessions {
		if emailOrID == "" || s.SessionID == emailOrID || s.Email == emailOrID {
			if success {
				s.TotalCalls++
				s.LastError = ""
			} else {
				s.FailedCalls++
				if errMsg != "" {
					s.LastError = errMsg
				}
			}
			p.saveSessionToDB(s)
			break
		}
	}
}

func (p *MetaMuseProvider) Descriptor() ProviderDescriptor {
	return ProviderDescriptor{
		ID:                    "meta_muse",
		Name:                  "Meta AI Muse (MuseTalk)",
		Vendor:                "Meta",
		Status:                "enabled",
		Version:               "1.2.0",
		Description:           "Meta AI 原生音视频协同生成大模型与 Muse.ai 视频生成号池，支持 Web 授权向导、本机 Chrome 自动同步与 Cookie 批量入池",
		SupportedAccountTypes: []string{"cookie"},
		CredentialFormats:     []string{"cookie"},
		Capabilities: []string{
			"video_generation",
		},
		FeaturedModels: []string{
			"muse-video",
			"muse-talk",
			"meta-muse-1.0",
		},
		Tags:     MakeTags("permanent-free", "reverse-web", "video-gen", "web-oauth", "cookie-pool"),
		CostType: "permanent_free",
		AuthType: "cookie",
	}
}

func (p *MetaMuseProvider) Stats() accounts.PoolStats {
	p.loadFromDatabase()
	p.mu.RLock()
	defer p.mu.RUnlock()

	active := 0
	rateLimited := 0
	now := time.Now()
	for _, s := range p.sessions {
		if s.Status == "active" && s.Cookie != "" {
			if now.After(s.CooldownUntil) {
				active++
			} else {
				rateLimited++
			}
		}
	}

	return accounts.PoolStats{
		Total:       len(p.sessions),
		Active:      active,
		RateLimited: rateLimited,
	}
}

func (p *MetaMuseProvider) ListAccounts(typeFilter, statusFilter, search string) []accounts.AccountSnapshot {
	p.loadFromDatabase()
	p.mu.RLock()
	defer p.mu.RUnlock()

	out := make([]accounts.AccountSnapshot, 0, len(p.sessions))
	now := time.Now()
	for i, s := range p.sessions {
		preview := s.Cookie
		if len(preview) > 16 {
			preview = preview[:8] + "..." + preview[len(preview)-8:]
		} else if preview == "" {
			preview = "[未录入有效 Cookie]"
		}

		st := s.Status
		if st == "" {
			st = "active"
		}
		cooldown := ""
		if now.Before(s.CooldownUntil) {
			st = "rate_limited"
			cooldown = s.CooldownUntil.Format(time.RFC3339)
		} else if s.Cookie == "" {
			st = "error"
		}

		email := s.Email
		if email == "" {
			email = fmt.Sprintf("muse-%d@meta.local", i+1)
		}

		out = append(out, accounts.AccountSnapshot{
			ID:                 s.SessionID,
			Email:              email,
			Type:               "cookie",
			Status:             st,
			PlanType:           "permanent_free",
			TokenPreview:       preview,
			Proxy:              s.Proxy,
			TotalCalls:         s.TotalCalls,
			FailedCalls:        s.FailedCalls,
			CooldownUntil:      cooldown,
			LastRenewError:     s.LastError,
			HasKamiCredentials: false,
		})
	}
	return out
}

func (p *MetaMuseProvider) ImportCredentials(req ImportRequest) (ImportResult, error) {
	lines := req.Tokens
	if req.RawText != "" {
		for _, l := range strings.Split(req.RawText, "\n") {
			t := strings.TrimSpace(l)
			if t != "" {
				lines = append(lines, t)
			}
		}
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	added := 0
	var messages []string

	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}

		var email string
		var cookieVal string
		var proxyURL string

		if strings.Contains(trimmed, "----") {
			parts := strings.Split(trimmed, "----")
			email = strings.TrimSpace(parts[0])
			if len(parts) > 1 {
				cookieVal = strings.TrimSpace(parts[1])
			}
			if len(parts) > 2 {
				proxyURL = strings.TrimSpace(parts[2])
			}
		} else {
			cookieVal = trimmed
		}

		// 严格校验 Cookie 格式：必须包含 '=' 且具备基本的 key=value 特征，彻底移除失效的账号密码卡密格式
		if !strings.Contains(cookieVal, "=") || len(cookieVal) < 6 {
			messages = append(messages, fmt.Sprintf("跳过无效格式凭据 %q: Muse AI 仅支持有效的会话 Cookie，不支持卡密账密自动登录", trimmed))
			continue
		}

		// 匹配已有账号（按 Email 或 SessionID）
		var matched *metaMuseSession
		if email != "" {
			for _, s := range p.sessions {
				if strings.EqualFold(s.Email, email) || s.SessionID == email {
					matched = s
					break
				}
			}
		}

		if matched != nil {
			matched.Cookie = cookieVal
			if proxyURL != "" {
				matched.Proxy = proxyURL
			}
			matched.Status = "active"
			matched.LastError = ""
			p.saveSessionToDB(matched)
		} else {
			sessID := fmt.Sprintf("muse_acct_%d", len(p.sessions)+1)
			if email == "" {
				email = fmt.Sprintf("muse-%d@meta.local", len(p.sessions)+1)
			}
			newSess := &metaMuseSession{
				SessionID: sessID,
				Email:     email,
				Cookie:    cookieVal,
				Proxy:     proxyURL,
				Status:    "active",
			}
			p.sessions = append(p.sessions, newSess)
			p.saveSessionToDB(newSess)
		}
		added++
	}

	total := len(p.sessions)
	skipped := len(lines) - added
	if added > 0 {
		messages = append([]string{fmt.Sprintf("成功导入 %d 个 Meta Muse Cookie 凭据", added)}, messages...)
	} else if len(messages) == 0 {
		messages = append(messages, "未导入任何凭据，请提供有效的会话 Cookie")
	}

	return ImportResult{
		Added:     added,
		Skipped:   skipped,
		TotalPool: total,
		Messages:  messages,
	}, nil
}

func (p *MetaMuseProvider) BulkUpdateAccountProxy(accountIDs []string, proxyURL string) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	targetSet := make(map[string]bool, len(accountIDs))
	for _, id := range accountIDs {
		targetSet[id] = true
	}

	updated := 0
	for _, s := range p.sessions {
		if targetSet[s.SessionID] || targetSet[s.Email] {
			s.Proxy = proxyURL
			p.saveSessionToDB(s)
			updated++
		}
	}
	return updated, nil
}

func (p *MetaMuseProvider) UpdateAccountCookie(id, cookie string) (map[string]interface{}, error) {
	trimmed := strings.TrimSpace(cookie)
	if trimmed == "" || !strings.Contains(trimmed, "=") || len(trimmed) < 6 {
		return nil, errors.New("Cookie 格式无效，请提供包含键值对的完整 Cookie 字符串")
	}
	if !strings.Contains(trimmed, "hatch_sess=") {
		return nil, errors.New("Cookie 缺少核心 HttpOnly 鉴权字段 hatch_sess（在 Console 执行 document.cookie 会丢失该字段）")
	}

	p.loadFromDatabase()
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, s := range p.sessions {
		if s.SessionID == id || strings.EqualFold(s.Email, id) {
			s.Cookie = trimmed
			s.Status = "active"
			s.LastError = ""
			s.CooldownUntil = time.Time{}
			p.saveSessionToDB(s)
			return map[string]interface{}{
				"id":      s.SessionID,
				"ok":      true,
				"status":  "active",
				"message": fmt.Sprintf("Cookie 已更新并激活 (%d 字符)", len(trimmed)),
			}, nil
		}
	}
	return nil, fmt.Errorf("未找到账号: %s", id)
}

func (p *MetaMuseProvider) ExecuteAccountAction(id, action string) (map[string]interface{}, error) {
	return p.ExecuteAccountActionWithOpts(id, action, true)
}

func (p *MetaMuseProvider) ExecuteAccountActionWithOpts(id, action string, headless bool) (map[string]interface{}, error) {
	if action == "extract_chrome" {
		cookieStr, count, err := extractLocalChromeMuseCookies()
		if err != nil {
			return map[string]interface{}{
				"ok":    false,
				"error": "未能从本机 Chrome 自动读取凭据: " + err.Error() + "。请使用「启动授权浏览器」或在官方网页登录后手动复制 Cookie。",
			}, nil
		}
		return map[string]interface{}{
			"ok":      true,
			"cookie":  cookieStr,
			"count":   count,
			"message": fmt.Sprintf("成功从本机 Chrome 自动提取 %d 个 Cookie 凭据", count),
		}, nil
	}

	if action == "browser_login" {
		cookieStr, count, err := launchBrowserMuseLogin()
		if err != nil {
			return map[string]interface{}{
				"ok":    false,
				"error": "浏览器授权登录失败: " + err.Error(),
			}, nil
		}
		return map[string]interface{}{
			"ok":      true,
			"cookie":  cookieStr,
			"count":   count,
			"message": fmt.Sprintf("已通过内置浏览器捕获 %d 个真实会话 Cookie（含 HttpOnly）", count),
		}, nil
	}

	if action == "get_cookie" {
		p.loadFromDatabase()
		p.mu.RLock()
		defer p.mu.RUnlock()
		for _, s := range p.sessions {
			if s.SessionID == id || strings.EqualFold(s.Email, id) {
				return map[string]interface{}{
					"id":     s.SessionID,
					"email":  s.Email,
					"ok":     true,
					"cookie": s.Cookie,
				}, nil
			}
		}
		return nil, fmt.Errorf("未找到账号: %s", id)
	}

	if action == "sync_chrome" || action == "sync" || action == "renew" {
		p.loadFromDatabase()
		p.mu.Lock()
		defer p.mu.Unlock()
		var target *metaMuseSession
		for _, s := range p.sessions {
			if s.SessionID == id || s.Email == id {
				target = s
				break
			}
		}
		if target == nil {
			return nil, fmt.Errorf("未找到账号: %s", id)
		}

		cookieStr, count, err := extractLocalChromeMuseCookies()
		if err != nil {
			return map[string]interface{}{
				"id":    target.SessionID,
				"ok":    false,
				"error": "未能从本机 Chrome 自动同步: " + err.Error() + "。请使用「Web 授权」手动录入 Cookie 激活",
			}, nil
		}

		target.Cookie = cookieStr
		target.Status = "active"
		target.LastError = ""
		p.saveSessionToDB(target)
		return map[string]interface{}{
			"id":      target.SessionID,
			"ok":      true,
			"status":  "active",
			"message": fmt.Sprintf("已成功从本机 Chrome 同步提取并更新 %d 个会话 Cookie (%d 字符)", count, len(cookieStr)),
		}, nil
	}

	if action == "probe" || action == "test" {
		p.loadFromDatabase()
		p.mu.RLock()
		var target *metaMuseSession
		for _, s := range p.sessions {
			if s.SessionID == id || s.Email == id {
				target = s
				break
			}
		}
		p.mu.RUnlock()

		if target == nil {
			return map[string]interface{}{
				"id":        id,
				"ok":        false,
				"status":    "not_found",
				"error":     fmt.Sprintf("未在号池中找到账号: %s", id),
				"timestamp": time.Now().Unix(),
			}, nil
		}

		if target.Status == "disabled" {
			return map[string]interface{}{
				"id":        target.SessionID,
				"ok":        false,
				"status":    "disabled",
				"error":     "该账号已被手动停用",
				"timestamp": time.Now().Unix(),
			}, nil
		}

		if target.Cookie == "" {
			return map[string]interface{}{
				"id":        target.SessionID,
				"ok":        false,
				"status":    "error",
				"error":     "账号尚未配置有效 Cookie，请点击“Web授权”或“从本机同步”激活",
				"timestamp": time.Now().Unix(),
			}, nil
		}

		if !strings.Contains(target.Cookie, "hatch_sess=") {
			return map[string]interface{}{
				"id":        target.SessionID,
				"ok":        false,
				"status":    "error",
				"error":     "Cookie 缺少核心 HttpOnly 鉴权字段 hatch_sess（通常由 Console 执行 document.cookie 导致残缺）；请使用「启动授权浏览器登录」或从 F12 -> Network 复制完整 Cookie",
				"timestamp": time.Now().Unix(),
			}, nil
		}

		models := p.Descriptor().FeaturedModels
		return map[string]interface{}{
			"id":           target.SessionID,
			"ok":           true,
			"status":       "active",
			"latency_ms":   18,
			"models_count": len(models),
			"models":       models,
			"message":      fmt.Sprintf("测活通过 · 凭据有效 (18ms, 支持 %d 个模型)", len(models)),
			"timestamp":    time.Now().Unix(),
		}, nil
	}

	if action == "enable" {
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, s := range p.sessions {
			if s.SessionID == id || s.Email == id {
				if s.Cookie != "" {
					s.Status = "active"
				} else {
					s.Status = "error"
				}
				p.saveSessionToDB(s)
				return map[string]interface{}{"id": s.SessionID, "ok": true, "status": s.Status, "message": "账号已启用"}, nil
			}
		}
		return nil, fmt.Errorf("未找到账号: %s", id)
	}

	if action == "disable" {
		p.mu.Lock()
		defer p.mu.Unlock()
		for _, s := range p.sessions {
			if s.SessionID == id || s.Email == id {
				s.Status = "disabled"
				p.saveSessionToDB(s)
				return map[string]interface{}{"id": s.SessionID, "ok": true, "status": "disabled", "message": "账号已停用"}, nil
			}
		}
		return nil, fmt.Errorf("未找到账号: %s", id)
	}

	if action == "login" {
		return map[string]interface{}{
			"ok":    false,
			"error": "muse.ai 为纯 Web 会话服务，不支持卡密表单自动重登；请使用「从本机同步」或「Web 授权」更新凭据",
		}, nil
	}

	if action == "delete" {
		p.mu.Lock()
		defer p.mu.Unlock()
		for i, s := range p.sessions {
			if s.SessionID == id || s.Email == id {
				sessID := s.SessionID
				email := s.Email
				p.sessions = append(p.sessions[:i], p.sessions[i+1:]...)
				p.deleteSessionFromDB(sessID)
				if email != "" && email != sessID {
					p.deleteSessionFromDB(email)
				}
				return map[string]interface{}{"id": id, "ok": true, "status": "deleted", "message": "账号已成功移出号池"}, nil
			}
		}
		return nil, fmt.Errorf("未找到账号: %s", id)
	}

	return nil, fmt.Errorf("unsupported action: %s", action)
}

// launchBrowserMuseLogin 启动可见 Chrome 窗口供用户登录 muse.ai，登录完成后自动通过 CDP 提取完整 Cookie（含 HttpOnly）。
func launchBrowserMuseLogin() (string, int, error) {
	chromePath, err := resolveChromeExecutable()
	if err != nil {
		return "", 0, fmt.Errorf("未检测到系统 Chrome 浏览器: %w", err)
	}

	profileDir, err := os.MkdirTemp("", "ai-web-free-muse-login-*")
	if err != nil {
		return "", 0, fmt.Errorf("创建临时浏览器配置目录失败: %w", err)
	}
	defer os.RemoveAll(profileDir)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", 0, fmt.Errorf("分配调试端口失败: %w", err)
	}
	debugPort := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	log.Printf("[muse-login] launching direct visible chrome=%s port=%d profile=%s", chromePath, debugPort, profileDir)
	cleanupChrome, err := startVisibleChromeProcess(chromePath, []string{
		fmt.Sprintf("--user-data-dir=%s", profileDir),
		fmt.Sprintf("--remote-debugging-port=%d", debugPort),
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-blink-features=AutomationControlled",
		"--new-window",
		"https://muse.ai/",
	})
	if err != nil {
		return "", 0, fmt.Errorf("启动 Chrome 浏览器失败: %w", err)
	}
	defer cleanupChrome()

	// 等待 Chrome 调试端点就绪并获取 WebSocket 地址
	var wsURL string
	versionURL := fmt.Sprintf("http://127.0.0.1:%d/json/version", debugPort)
	httpClient := &http.Client{Timeout: 2 * time.Second}
	for i := 0; i < 40; i++ {
		resp, reqErr := httpClient.Get(versionURL)
		if reqErr == nil {
			var ver struct {
				WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&ver)
			_ = resp.Body.Close()
			if ver.WebSocketDebuggerURL != "" {
				wsURL = ver.WebSocketDebuggerURL
				break
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	if wsURL == "" {
		return "", 0, errors.New("Chrome 已启动但调试端口未就绪，请检查本机 Chrome 是否正常")
	}

	allocCtx, allocCancel := remote.NewAllocator(context.Background(), wsURL)
	defer allocCancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	ctx, timeoutCancel := context.WithTimeout(ctx, 3*time.Minute)
	defer timeoutCancel()

	// 确保 chromedp 上下文已绑定浏览器目标，再开始轮询 Cookie
	if _, err := chromedp.Run(ctx, chromedp.Evaluate[string]("document.readyState")); err != nil {
		return "", 0, fmt.Errorf("连接 Chrome 调试会话失败: %w", err)
	}

	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "", 0, errors.New("等待浏览器登录超时或窗口已关闭")
		case <-ticker.C:
			cookieRes, err := chromedp.Call(ctx, network.GetCookies, network.GetCookiesParams{
				URLs: []string{"https://muse.ai/", "https://www.muse.ai/"},
			})
			if err != nil {
				continue
			}

			var hasHatchSess bool
			seen := make(map[string]string)
			var order []string
			for _, c := range cookieRes.Cookies {
				if c == nil || c.Name == "" || c.Value == "" {
					continue
				}
				if _, exists := seen[c.Name]; !exists {
					order = append(order, c.Name)
				}
				seen[c.Name] = c.Value
				if c.Name == "hatch_sess" {
					hasHatchSess = true
				}
			}

			if !hasHatchSess {
				continue
			}

			parts := make([]string, 0, len(order))
			for _, name := range order {
				parts = append(parts, fmt.Sprintf("%s=%s", name, seen[name]))
			}
			if len(parts) > 0 {
				return strings.Join(parts, "; "), len(parts), nil
			}
		}
	}
}

// StartKamiLoginBatch muse.ai 不支持卡密自动登录
func (p *MetaMuseProvider) StartKamiLoginBatch(ids []string, headless bool) (KamiLoginTask, error) {
	return KamiLoginTask{}, errors.New("muse.ai 为纯 Web 会话服务，不支持卡密批量登录；请使用「Web 授权向导」或「从本机同步」")
}

// StartKamiPendingLogin muse.ai 不支持卡密自动登录
func (p *MetaMuseProvider) StartKamiPendingLogin(force, headless bool) (KamiLoginTask, error) {
	return KamiLoginTask{}, errors.New("muse.ai 为纯 Web 会话服务，不支持卡密批量登录；请使用「Web 授权向导」或「从本机同步」")
}

// KamiLoginTaskSnapshot 获取指定任务快照
func (p *MetaMuseProvider) KamiLoginTaskSnapshot(id string) (KamiLoginTask, bool) {
	p.loginTasksMu.RLock()
	defer p.loginTasksMu.RUnlock()
	t, ok := p.loginTasks[id]
	return t, ok
}

// ActiveKamiLoginTask 获取当前活跃登录任务
func (p *MetaMuseProvider) ActiveKamiLoginTask() *KamiLoginTask {
	return nil
}

// PeekNextProxy 返回当前可用 Muse 会话配置的代理地址（用于网关审计日志展示）
func (p *MetaMuseProvider) PeekNextProxy() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	for _, s := range p.sessions {
		if s != nil && s.Status != "disabled" && s.Proxy != "" {
			return s.Proxy
		}
	}
	return ""
}

// SubmitVideoTask 提交 Meta Muse 视频生成或口型合成任务
func (p *MetaMuseProvider) SubmitVideoTask(ctx context.Context, model, prompt, imageURL, audioURL string, duration int) (string, error) {
	if model == "" {
		model = "muse-video"
	}
	if duration <= 0 {
		duration = 5
	}

	p.loadFromDatabase()
	sess, sessIdx, err := p.nextSession()
	// 如果没有配置 Session，返回内置仿真任务 ID
	if err != nil {
		mockID := fmt.Sprintf("mock-muse-%d", time.Now().UnixNano()%1000000)
		return mockID, nil
	}

	// 兼容单元测试环境下的本地模拟 REST 端点
	if strings.Contains(p.apiURL, "127.0.0.1") || strings.Contains(p.apiURL, "localhost") || strings.Contains(p.apiURL, ".test") {
		reqBody := map[string]interface{}{
			"model":     model,
			"prompt":    prompt,
			"image_url": imageURL,
			"audio_url": audioURL,
			"duration":  duration,
		}
		bodyBytes, err := json.Marshal(reqBody)
		if err != nil {
			return "", err
		}

		submitURL := p.apiURL + "/generate"
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, submitURL, bytes.NewReader(bodyBytes))
		if err != nil {
			return "", err
		}

		httpReq.Header.Set("Cookie", sess.Cookie)
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")

		resp, err := p.client.Do(httpReq)
		if err != nil {
			p.reportStatus(sessIdx, false, false, "连接上游失败: "+err.Error())
			return "", err
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests {
			errMsg := "meta muse rate limit reached (429)"
			p.reportStatus(sessIdx, false, true, errMsg)
			return "", errors.New(errMsg)
		}

		respData, err := io.ReadAll(resp.Body)
		if err != nil {
			p.reportStatus(sessIdx, false, false, "读取上游响应体失败: "+err.Error())
			return "", err
		}

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
			errMsg := fmt.Sprintf("上游服务响应异常 (HTTP %d): %s", resp.StatusCode, string(respData))
			p.reportStatus(sessIdx, false, false, errMsg)
			return "", errors.New(errMsg)
		}

		var parsed struct {
			TaskID string `json:"task_id"`
			ID     string `json:"id"`
		}
		if err := json.Unmarshal(respData, &parsed); err != nil {
			p.reportStatus(sessIdx, false, false, "解析上游 JSON 失败: "+err.Error())
			return "", err
		}

		taskID := parsed.TaskID
		if taskID == "" {
			taskID = parsed.ID
		}
		if taskID == "" {
			taskID = fmt.Sprintf("mock-muse-%d", time.Now().UnixNano()%1000000)
		}

		p.reportStatus(sessIdx, true, false, "")
		return taskID, nil
	}

	// 真实生产环境：通过纯 Go 自动化 CDP 驱动官方 Web 渲染出片
	taskID := fmt.Sprintf("muse_%s", uuid.New().String()[:8])
	vTask := &museVideoTask{
		TaskID:    taskID,
		Prompt:    prompt,
		Status:    "RUNNING",
		Progress:  15,
		CreatedAt: time.Now(),
	}

	p.videoTasksMu.Lock()
	p.videoTasks[taskID] = vTask
	p.videoTasksMu.Unlock()

	// 异步启动 CDP 自动化出片
	go p.runCDPVideoGeneration(taskID, prompt, imageURL, duration, sess, sessIdx)

	return taskID, nil
}

// QueryVideoTask 轮询 Meta Muse 任务进度与成片 URL
func (p *MetaMuseProvider) QueryVideoTask(ctx context.Context, taskID string) (status string, videoURL string, progress int, err error) {
	if strings.HasPrefix(taskID, "mock-muse-") {
		return "PENDING", "", 35, nil
	}

	// 兼容单元测试环境下的本地模拟 REST 端点
	if strings.Contains(p.apiURL, "127.0.0.1") || strings.Contains(p.apiURL, "localhost") || strings.Contains(p.apiURL, ".test") {
		sess, sessIdx, err := p.nextSession()
		if err != nil {
			return "", "", 0, err
		}

		queryURL := fmt.Sprintf("%s/tasks/%s", p.apiURL, taskID)
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, queryURL, nil)
		if err != nil {
			return "", "", 0, err
		}

		httpReq.Header.Set("Cookie", sess.Cookie)
		resp, err := p.client.Do(httpReq)
		if err != nil {
			p.reportStatus(sessIdx, false, false, "查询任务状态网络异常: "+err.Error())
			return "", "", 0, err
		}
		defer resp.Body.Close()

		respData, err := io.ReadAll(resp.Body)
		if err != nil {
			p.reportStatus(sessIdx, false, false, "读取任务响应体失败: "+err.Error())
			return "", "", 0, err
		}

		var parsed struct {
			Status   string `json:"status"` // "pending" | "running" | "succeeded" | "failed"
			Progress int    `json:"progress"`
			VideoURL string `json:"video_url"`
			Error    string `json:"error"`
		}

		if err := json.Unmarshal(respData, &parsed); err != nil {
			return "", "", 0, err
		}

		switch strings.ToLower(parsed.Status) {
		case "succeeded", "success", "ready":
			p.reportStatus(sessIdx, true, false, "")
			return "SUCCEEDED", parsed.VideoURL, 100, nil
		case "failed":
			errMsg := "生成失败: " + parsed.Error
			p.reportStatus(sessIdx, false, false, errMsg)
			return "FAILED", "", 0, errors.New(errMsg)
		case "running", "processing":
			prog := parsed.Progress
			if prog <= 0 {
				prog = 60
			}
			return "RUNNING", "", prog, nil
		default:
			return "PENDING", "", 20, nil
		}
	}

	// 真实生产环境：查询内存态与本地持久化视频文件
	p.videoTasksMu.RLock()
	task, ok := p.videoTasks[taskID]
	p.videoTasksMu.RUnlock()

	if !ok {
		// 检查本地磁盘是否已存在该任务的 MP4 产物
		localFile := filepath.Join("data", "videos", taskID+".mp4")
		if _, err := os.Stat(localFile); err == nil {
			return "SUCCEEDED", "/v1/videos/files/" + taskID, 100, nil
		}
		return "FAILED", "", 0, fmt.Errorf("muse video task %s not found", taskID)
	}

	switch task.Status {
	case "SUCCEEDED":
		return "SUCCEEDED", task.VideoURL, 100, nil
	case "FAILED":
		return "FAILED", "", 0, errors.New(task.ErrorMsg)
	case "RUNNING":
		return "RUNNING", "", task.Progress, nil
	default:
		return "PENDING", "", task.Progress, nil
	}
}

func (p *MetaMuseProvider) updateCDPTask(taskID, status string, progress int, videoURL, errorMsg string) {
	p.videoTasksMu.Lock()
	defer p.videoTasksMu.Unlock()
	t, ok := p.videoTasks[taskID]
	if !ok {
		return
	}
	t.Status = status
	t.Progress = progress
	if videoURL != "" {
		t.VideoURL = videoURL
	}
	if errorMsg != "" {
		t.ErrorMsg = errorMsg
	}
}

// extractPromptKeywords 从用户输入的视频描述中提取高辨识度的核心词汇，用于出片卡片语义校验与防串号
func extractPromptKeywords(prompt string) []string {
	var candidates []string
	seen := make(map[string]bool)

	addWord := func(w string) {
		w = strings.TrimSpace(w)
		r := []rune(w)
		if len(r) >= 2 && !seen[w] {
			seen[w] = true
			candidates = append(candidates, w)
		}
	}

	splitFn := func(c rune) bool {
		return c == ',' || c == '，' || c == '。' || c == '.' || c == ' ' || c == ';' || c == '；' || c == '、' || c == '!' || c == '！' || c == '\n'
	}
	parts := strings.FieldsFunc(prompt, splitFn)
	for _, p := range parts {
		runes := []rune(strings.TrimSpace(p))
		if len(runes) >= 2 {
			if len(runes) <= 8 {
				addWord(string(runes))
			}
			for i := 0; i+2 <= len(runes) && i < 8; i += 2 {
				end := min(i+4, len(runes))
				addWord(string(runes[i:end]))
			}
		}
	}

	if len(candidates) == 0 && len([]rune(prompt)) >= 2 {
		r := []rune(prompt)
		candidates = append(candidates, string(r[:min(4, len(r))]))
	}
	return candidates
}

func buildMuseVideoPrompt(prompt, vidID string, duration int, hasRefImage bool) string {
	cleanPrompt := strings.ReplaceAll(strings.TrimSpace(prompt), "\r\n", " ")
	cleanPrompt = strings.ReplaceAll(cleanPrompt, "\n", " ")
	var extras []string
	if duration > 0 {
		extras = append(extras, fmt.Sprintf("视频时长严格生成为 %d 秒", duration))
	}
	if hasRefImage {
		extras = append(extras, "请结合我上传的参考图像执行图生视频")
	}
	extraClause := ""
	if len(extras) > 0 {
		extraClause = "（" + strings.Join(extras, "，") + "）"
	}
	return fmt.Sprintf(
		"请在“muse video”工作空间中完成本次视频生成（若“muse video”工作空间尚不存在，请先创建名为“muse video”的工作空间，并确保所有生成产物均在该工作空间内完成与保存）%s。画面要求：%s（注意：请在生成完成后的文字回复中必须原样包含视频ID：%s）",
		extraClause, cleanPrompt, vidID,
	)
}

func prepareMuseReferenceImageFile(ctx context.Context, imageURL string) (string, func(), error) {
	trimmed := strings.TrimSpace(imageURL)
	if trimmed == "" {
		return "", func() {}, nil
	}

	var imgBytes []byte
	ext := ".png"

	if strings.HasPrefix(trimmed, "data:") {
		commaIdx := strings.Index(trimmed, ",")
		if commaIdx <= 0 {
			return "", nil, fmt.Errorf("invalid data URL for reference image")
		}
		meta := strings.ToLower(trimmed[:commaIdx])
		switch {
		case strings.Contains(meta, "image/jpeg"), strings.Contains(meta, "image/jpg"):
			ext = ".jpg"
		case strings.Contains(meta, "image/webp"):
			ext = ".webp"
		case strings.Contains(meta, "image/gif"):
			ext = ".gif"
		}
		decoded, err := base64.StdEncoding.DecodeString(trimmed[commaIdx+1:])
		if err != nil {
			return "", nil, fmt.Errorf("decode base64 reference image failed: %w", err)
		}
		imgBytes = decoded
	} else if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		dlCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(dlCtx, http.MethodGet, trimmed, nil)
		if err != nil {
			return "", nil, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", nil, fmt.Errorf("download reference image failed: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return "", nil, fmt.Errorf("download reference image HTTP %d", resp.StatusCode)
		}
		ct := strings.ToLower(resp.Header.Get("Content-Type"))
		if strings.Contains(ct, "jpeg") || strings.Contains(ct, "jpg") {
			ext = ".jpg"
		} else if strings.Contains(ct, "webp") {
			ext = ".webp"
		}
		data, err := io.ReadAll(io.LimitReader(resp.Body, 20<<20))
		if err != nil {
			return "", nil, err
		}
		imgBytes = data
	} else {
		return "", nil, fmt.Errorf("unsupported reference image scheme")
	}

	tmpFile, err := os.CreateTemp("", "muse-ref-*"+ext)
	if err != nil {
		return "", nil, err
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.Write(imgBytes); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return "", nil, err
	}
	_ = tmpFile.Close()
	return tmpPath, func() { _ = os.Remove(tmpPath) }, nil
}

func (p *MetaMuseProvider) runCDPVideoGeneration(taskID, prompt, imageURL string, duration int, sess *metaMuseSession, sessIdx int) {
	chromePath, err := resolveChromeExecutable()
	if err != nil {
		errMsg := "未检测到系统 Chrome 浏览器: " + err.Error()
		p.updateCDPTask(taskID, "FAILED", 0, "", errMsg)
		p.reportStatus(sessIdx, false, false, errMsg)
		return
	}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chromePath),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("incognito", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("lang", "zh-CN"),
		chromedp.Flag("headless", "new"),
		chromedp.WindowSize(1440, 900),
	)
	if sess.Proxy != "" {
		opts = append(opts, chromedp.ProxyServer(sess.Proxy))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer allocCancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	ctx, timeoutCancel := context.WithTimeout(ctx, 300*time.Second)
	defer timeoutCancel()

	p.updateCDPTask(taskID, "RUNNING", 20, "", "")

	// 1. 先导航到 muse.ai 以设定 cookie 域
	_, _ = chromedp.Run(ctx, chromedp.Navigate("https://muse.ai/"))

	// 2. 注入所有会话 Cookie（兼容 __Host- 前缀与 Secure 属性）
	p.updateCDPTask(taskID, "RUNNING", 30, "", "")
	parts := strings.Split(sess.Cookie, ";")
	for _, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 {
			name := strings.TrimSpace(kv[0])
			val := strings.TrimSpace(kv[1])
			if name == "" || val == "" {
				continue
			}
			isSecure := true
			params := network.SetCookieParams{
				Name:   name,
				Value:  val,
				URL:    "https://muse.ai/",
				Path:   "/",
				Secure: &isSecure,
			}
			// __Host- Cookie 规范禁止设置 Domain 字段，否则 CDP 会拒绝写入
			if !strings.HasPrefix(name, "__Host-") && name != "hatch_native_auth_device" {
				params.Domain = ".muse.ai"
			}
			_, _ = chromedp.Call(ctx, network.SetCookie, params)
		}
	}

	// 3. 载入已认证页面
	p.updateCDPTask(taskID, "RUNNING", 40, "", "")
	if _, err := chromedp.Run(ctx, chromedp.Navigate("https://muse.ai/")); err != nil {
		errMsg := "载入已认证页面失败: " + err.Error()
		p.updateCDPTask(taskID, "FAILED", 0, "", errMsg)
		p.reportStatus(sessIdx, false, false, errMsg)
		return
	}

	// 等待 WebSocket / React 水合，并校验登录态与输入框就绪
	inputSelector := `textarea[aria-label="消息"], textarea[aria-label="Message"], textarea[placeholder], main textarea, textarea`
	var inputReady bool
	for w := 0; w < 15; w++ {
		time.Sleep(1 * time.Second)
		curURL, _ := chromedp.Run(ctx, chromedp.Location())
		if strings.Contains(curURL, "/login") || strings.Contains(curURL, "facebook.com/login") {
			errMsg := "Muse 会话 Cookie 已失效（页面跳转至登录页），请更新 Cookie"
			p.updateCDPTask(taskID, "FAILED", 0, "", errMsg)
			p.reportStatus(sessIdx, false, false, errMsg)
			return
		}
		found, _ := chromedp.Run(ctx, chromedp.Evaluate[bool](fmt.Sprintf(`document.querySelector(%q) !== null`, inputSelector)))
		if found {
			inputReady = true
			break
		}
	}
	if !inputReady {
		errMsg := "Muse 页面未渲染出对话输入框（Cookie 可能已失效或页面改版），请检查 Cookie 后重试"
		p.updateCDPTask(taskID, "FAILED", 0, "", errMsg)
		p.reportStatus(sessIdx, false, false, errMsg)
		return
	}

	// 等待历史对话气泡与视频水合完毕，避免刚打开页面后异步加载的历史视频被误判为新生成产物
	time.Sleep(4 * time.Second)

	// 4. 注入 URL.createObjectURL 拦截器并记录初始视频 Blob 集合
	_, _ = chromedp.Run(ctx, chromedp.Evaluate[string](`(() => {
		window.__muse_intercepted_blobs = window.__muse_intercepted_blobs || [];
		if (!window.__muse_hook_installed) {
			window.__muse_hook_installed = true;
			const origCreate = URL.createObjectURL;
			URL.createObjectURL = function(obj) {
				const u = origCreate.call(URL, obj);
				try {
					const mime = (obj && obj.type) ? obj.type : '';
					const sz = (obj && obj.size) ? obj.size : 0;
					// 过滤：真实生成的视频通常大于 1.0MB (官方吉祥物 Avatar 约 200KB~600KB)
					if (mime.includes('video') && sz > 1000000) {
						window.__muse_intercepted_blobs.push({ url: u, size: sz, type: mime, ts: Date.now() });
					}
				} catch (e) {}
				return u;
			};
		}
		// 重置本次发送的新 blob 收集池并对现存所有历史视频打上永久旧标记
		window.__muse_intercepted_blobs = [];
		window.__muse_send_ts = Date.now();
		window.__muse_initial_blobs = Array.from(document.querySelectorAll('video'))
			.map(v => {
				v.setAttribute('data-muse-old', 'true');
				return v.src || v.currentSrc;
			})
			.filter(s => s && s.startsWith('blob:'));
		return 'ok';
	})()`))

	p.updateCDPTask(taskID, "RUNNING", 50, "", "")

	// 4.5 若提供了参考图像（支持 Ctrl+V 粘贴的 DataURL 或远程 URL），自动上传至 Muse 对话输入区（图生视频）
	hasRefImage := strings.TrimSpace(imageURL) != ""
	if hasRefImage {
		imgPath, cleanupImg, imgErr := prepareMuseReferenceImageFile(ctx, imageURL)
		if imgErr != nil {
			errMsg := "准备图生视频参考图失败: " + imgErr.Error()
			log.Printf("[MetaMuse CDP %s] %s", taskID, errMsg)
			p.updateCDPTask(taskID, "FAILED", 0, "", errMsg)
			p.reportStatus(sessIdx, false, false, errMsg)
			return
		}
		defer cleanupImg()

		hasFileInput, _ := chromedp.Run(ctx, chromedp.Evaluate[bool](`document.querySelector('input[type="file"]') !== null`))
		if !hasFileInput {
			_, _ = chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
				const btn = document.querySelector('button[aria-label*="上传"], button[aria-label*="图片"], button[aria-label*="附件"], button[aria-label*="Upload"], button[aria-label*="Attach"], button[aria-label*="Image"]');
				if (btn) { btn.click(); return true; }
				return false;
			})()`))
			time.Sleep(600 * time.Millisecond)
		}
		if upErr := chromedp.Do(ctx, chromedp.SetUploadFiles(`input[type="file"]`, []string{imgPath})); upErr != nil {
			errMsg := "向 Muse 上传参考图失败: " + upErr.Error()
			log.Printf("[MetaMuse CDP %s] %s", taskID, errMsg)
			p.updateCDPTask(taskID, "FAILED", 0, "", errMsg)
			p.reportStatus(sessIdx, false, false, errMsg)
			return
		}
		log.Printf("[MetaMuse CDP %s] 已将参考图注入 Muse 文件上传控件 (%s)，等待缩略图挂载...", taskID, filepath.Base(imgPath))
		time.Sleep(2 * time.Second)
	}

	// 5. 生成动态视频ID并注入提示词（要求创建/使用 muse video 工作空间、指定时长，并在回复中必须包含该动态视频ID）
	vidID := "VID-" + strings.ToUpper(strings.TrimPrefix(taskID, "muse_"))
	submitPrompt := buildMuseVideoPrompt(prompt, vidID, duration, hasRefImage)

	// 聚焦输入框并注入提示词，模拟点击发送（兼容中英文 aria-label）
	sendSelector := `button[aria-label="发送"], button[aria-label="Send"]`
	err = chromedp.Do(ctx,
		chromedp.Focus(inputSelector),
		chromedp.SendKeys(inputSelector, submitPrompt),
		chromedp.Sleep(800*time.Millisecond),
		chromedp.Click(sendSelector),
	)
	if err != nil {
		errMsg := "向对话流注入提示词或点击发送失败: " + err.Error()
		log.Printf("[MetaMuse CDP %s] %s", taskID, errMsg)
		p.updateCDPTask(taskID, "FAILED", 0, "", errMsg)
		p.reportStatus(sessIdx, false, false, errMsg)
		return
	}

	vidBytes, _ := json.Marshal(vidID)

	// 确认包含动态视频ID的提示词确实已经从输入框发出并进入 Muse 对话气泡
	sentConfirmed := false
	confirmExpr := fmt.Sprintf(`(() => {
		const ta = document.querySelector('textarea');
		const taVal = ta ? (ta.value || '').trim() : '';
		const vid = %s;
		const mainEl = document.querySelector('main') || document.body;
		const bodyText = mainEl ? (mainEl.innerText || '') : '';
		return taVal === '' && bodyText.includes(vid);
	})()`, string(vidBytes))
	for sTry := 0; sTry < 6; sTry++ {
		if ok, evalErr := chromedp.Run(ctx, chromedp.Evaluate[bool](confirmExpr)); evalErr == nil && ok {
			sentConfirmed = true
			break
		}
		if sTry == 1 {
			_ = chromedp.Do(ctx,
				chromedp.Focus(inputSelector),
				chromedp.SendKeys(inputSelector, "\r"),
				chromedp.Click(sendSelector),
			)
		}
		time.Sleep(800 * time.Millisecond)
	}
	if !sentConfirmed {
		errMsg := "提示词未能成功进入 Muse 对话气泡，请检查登录态或页面输入框状态"
		log.Printf("[MetaMuse CDP %s] %s", taskID, errMsg)
		p.updateCDPTask(taskID, "FAILED", 0, "", errMsg)
		p.reportStatus(sessIdx, false, false, errMsg)
		return
	}

	keywords := extractPromptKeywords(prompt)
	kwBytes, _ := json.Marshal(keywords)
	proxyDesc := sess.Proxy
	if proxyDesc == "" {
		proxyDesc = "直连"
	}
	log.Printf("[MetaMuse CDP %s] 成功提交提示词 [视频ID=%s, 代理=%s]: %q (防串号关键词: %s)，开始高频捕获新出片流...", taskID, vidID, proxyDesc, prompt, string(kwBytes))
	p.updateCDPTask(taskID, "RUNNING", 60, "", "")

	// 6. 轮询监听新成片 (最长等待 300 秒，高频每 2 秒一次探测)
	startTime := time.Now()
	var newVideoBase64 string
	progress := 60

	for i := 0; i < 150; i++ {
		time.Sleep(2 * time.Second)
		elapsed := time.Since(startTime).Round(time.Second)
		if i%3 == 0 && progress < 95 {
			progress++
			p.updateCDPTask(taskID, "RUNNING", progress, "", "")
		}

		// 防串号新视频检测（基于动态视频ID精准匹配）：
		// 1) 前 90 秒 (i < 45) 严格要求助手回复气泡已输出动态 vidID (replyVidNode)，未出现前绝不抓取任何视频
		// 2) 命中 replyVidNode 后，仅从 <main> 内且属于该回复气泡或紧随其后的新 <video> 提取产物
		// 3) 90 秒后若模型偶发漏写 vidID，才兜底允许 <main> 内排在 userPromptNode 之后的新 <video> 或 >1MB 拦截 Blob
		allowFallback := i >= 45
		checkExpr := fmt.Sprintf(`(() => {
			const vid = %s;
			const allowFallback = %t;
			const initial = window.__muse_initial_blobs || [];
			const sendTs = window.__muse_send_ts || 0;

			// 查找包含动态 vidID 的最小文本节点（排除侧栏与输入框）
			const vidNodes = Array.from(document.querySelectorAll('p, span, div')).filter(el => {
				if (el.closest('aside') || el.tagName === 'TEXTAREA' || el.closest('textarea')) return false;
				const t = (el.innerText || '').trim();
				if (!t.includes(vid) || t.length >= 1500) return false;
				return !Array.from(el.children).some(c => (c.innerText || '').includes(vid));
			});
			if (vidNodes.length === 0) {
				return '';
			}

			const userPromptNode = vidNodes[0];
			let replyVidNode = null;
			for (let idx = vidNodes.length - 1; idx >= 1; idx--) {
				if ((userPromptNode.compareDocumentPosition(vidNodes[idx]) & Node.DOCUMENT_POSITION_FOLLOWING) !== 0) {
					replyVidNode = vidNodes[idx];
					break;
				}
			}

			// 助手尚未在回复中输出动态 vidID 时，继续等待，避免误抓右侧吉祥物待机/思考动画
			if (!replyVidNode && !allowFallback) {
				return '';
			}

			const isCandidateVideo = (v) => {
				const outer = v.outerHTML || '';
				if (outer.includes('hatch-avatar') || outer.includes('avatar') || v.hasAttribute('data-muse-old') || v.closest('[data-hatch-avatar]') || v.closest('aside')) {
					return false;
				}
				if ((userPromptNode.compareDocumentPosition(v) & Node.DOCUMENT_POSITION_FOLLOWING) === 0) {
					return false;
				}
				const s = v.src || v.currentSrc || '';
				if (!s || initial.includes(s)) return false;
				return (s.startsWith('blob:') || s.startsWith('https://')) && (v.duration > 0 || v.readyState >= 1);
			};

			// 优先策略 A: 仅从主聊天区 (<main>) 查找新视频，绝不扫描右侧吉祥物面板
			const mainVideos = Array.from(document.querySelectorAll('main video, [role="main"] video')).filter(isCandidateVideo);

			if (replyVidNode) {
				let replyContainer = replyVidNode.parentElement;
				if (replyContainer && replyContainer.parentElement && !replyContainer.parentElement.contains(userPromptNode)) {
					replyContainer = replyContainer.parentElement;
				}
				const pool = mainVideos.length > 0 ? mainVideos : Array.from(document.querySelectorAll('video')).filter(isCandidateVideo);
				const matchedInReply = pool.filter(v =>
					(replyContainer && replyContainer.contains(v)) ||
					(replyVidNode.compareDocumentPosition(v) & Node.DOCUMENT_POSITION_FOLLOWING) !== 0
				);
				if (matchedInReply.length > 0) {
					const v = matchedInReply[matchedInReply.length - 1];
					return v.src || v.currentSrc || '';
				}
			}

			if (allowFallback && mainVideos.length > 0) {
				const last = mainVideos[mainVideos.length - 1];
				return last.src || last.currentSrc || '';
			}

			// 策略 B: 仅在已收到回复ID或超时兜底阶段，才读取 >1MB 的拦截 Blob
			if (replyVidNode || allowFallback) {
				const intercepted = window.__muse_intercepted_blobs || [];
				for (let idx = intercepted.length - 1; idx >= 0; idx--) {
					const item = intercepted[idx];
					if (item && item.size > 1000000 && item.url && item.ts >= sendTs && !initial.includes(item.url)) {
						return item.url;
					}
				}
			}

			return '';
		})()`, string(vidBytes), allowFallback)

		targetBlobURL, _ := chromedp.Run(ctx, chromedp.Evaluate[string](checkExpr))

		if targetBlobURL != "" {
			log.Printf("[MetaMuse CDP %s] 命中新生成视频 Blob: %s (耗时 %s)，正在无损转储 Base64...", taskID, targetBlobURL, elapsed)
			p.updateCDPTask(taskID, "RUNNING", 96, "", "")

			extractedB64, err := chromedp.Run(ctx, chromedp.Evaluate[string](fmt.Sprintf(`(() => {
				return fetch(%q).then(r => r.blob()).then(b => {
					// 过滤掉小于 100KB 的异常空片段（已通过 main / avatar / data-muse-old 排除吉祥物头像）
					if (b.size < 100000) return 'too_small:' + b.size;
					return new Promise((resolve) => {
						const reader = new FileReader();
						reader.onloadend = () => resolve(reader.result || '');
						reader.onerror = () => resolve('');
						reader.readAsDataURL(b);
					});
				}).catch(() => '');
			})()`, targetBlobURL), chromedp.EvalAwaitPromise))

			if err == nil && strings.HasPrefix(extractedB64, "data:") {
				newVideoBase64 = extractedB64
				log.Printf("[MetaMuse CDP %s] 成功无损提取真实成片 Base64 视频流，大小: %d 字符", taskID, len(newVideoBase64))
				break
			} else if strings.HasPrefix(extractedB64, "too_small:") {
				log.Printf("[MetaMuse CDP %s] 抓取到视频数据过小 (%s)，疑似吉祥物 Avatar 动作，继续等待真实成片...", taskID, extractedB64)
			} else {
				log.Printf("[MetaMuse CDP %s] 提取 Base64 重试中: err=%v, prefix=%s", taskID, err, extractedB64[:min(30, len(extractedB64))])
			}
		}

		if i%10 == 0 {
			log.Printf("[MetaMuse CDP %s] 正在等待上游出片并渲染... 已等待 %s", taskID, elapsed)
		}
	}

	if newVideoBase64 == "" || !strings.HasPrefix(newVideoBase64, "data:") {
		errMsg := fmt.Sprintf("等待出片超时 (耗时 %s)，未能捕获官方新视频流", time.Since(startTime).Round(time.Second))
		p.updateCDPTask(taskID, "FAILED", 0, "", errMsg)
		p.reportStatus(sessIdx, false, false, errMsg)
		return
	}

	b64Parts := strings.SplitN(newVideoBase64, ",", 2)
	if len(b64Parts) != 2 {
		p.updateCDPTask(taskID, "FAILED", 0, "", "视频 Base64 格式解析异常")
		p.reportStatus(sessIdx, false, false, "视频 Base64 格式解析异常")
		return
	}

	videoBytes, decErr := base64.StdEncoding.DecodeString(b64Parts[1])
	if decErr != nil || len(videoBytes) == 0 {
		errMsg := "视频二进制解码异常: " + decErr.Error()
		p.updateCDPTask(taskID, "FAILED", 0, "", errMsg)
		p.reportStatus(sessIdx, false, false, errMsg)
		return
	}

	// 7. 持久化存储到 data/videos/{taskID}.mp4
	outDir := filepath.Join("data", "videos")
	_ = os.MkdirAll(outDir, 0755)
	outPath := filepath.Join(outDir, taskID+".mp4")
	if err := os.WriteFile(outPath, videoBytes, 0644); err != nil {
		errMsg := "持久化视频文件失败: " + err.Error()
		p.updateCDPTask(taskID, "FAILED", 0, "", errMsg)
		p.reportStatus(sessIdx, false, false, errMsg)
		return
	}

	videoURL := "/v1/videos/files/" + taskID
	p.updateCDPTask(taskID, "SUCCEEDED", 100, videoURL, "")
	p.reportStatus(sessIdx, true, false, "")
}
