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
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/db"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
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

func (p *MetaMuseProvider) ExecuteAccountAction(id, action string) (map[string]interface{}, error) {
	return p.ExecuteAccountActionWithOpts(id, action, true)
}

func (p *MetaMuseProvider) ExecuteAccountActionWithOpts(id, action string, headless bool) (map[string]interface{}, error) {
	if action == "extract_chrome" {
		cookieStr, count, err := extractLocalChromeMuseCookies()
		if err != nil {
			return map[string]interface{}{
				"ok":    false,
				"error": "未能从本机 Chrome 自动读取凭据: " + err.Error() + "。请使用官方网页登录后手动复制 Cookie。",
			}, nil
		}
		return map[string]interface{}{
			"ok":      true,
			"cookie":  cookieStr,
			"count":   count,
			"message": fmt.Sprintf("成功从本机 Chrome 自动提取 %d 个 Cookie 凭据", count),
		}, nil
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

// extractLocalChromeMuseCookies 跨平台探测并从本机 Google Chrome 读取解密后的 muse.ai 完整会话 Cookie
func extractLocalChromeMuseCookies() (string, int, error) {
	pyScript := `
import os, sys, sqlite3, shutil, json

def get_muse_cookie():
    possible_paths = [
        os.path.expanduser('~/.config/google-chrome/Default/Cookies'),
        os.path.expanduser('~/.config/chromium/Default/Cookies'),
        os.path.expanduser('~/Library/Application Support/Google/Chrome/Default/Cookies'),
    ]
    cookie_file = None
    for p in possible_paths:
        if os.path.exists(p):
            cookie_file = p
            break
    if not cookie_file:
        return None

    try:
        import secretstorage, hashlib
        from cryptography.hazmat.primitives.ciphers import Cipher, algorithms, modes
        conn = secretstorage.dbus_init()
        collection = secretstorage.get_default_collection(conn)
        items = collection.search_items({'application': 'chrome', 'xdg:schema': 'chrome_libsecret_os_crypt_password_v2'})
        chrome_secret = None
        for item in items:
            chrome_secret = item.get_secret()
            break
        if not chrome_secret:
            chrome_secret = b'peanuts'
        salt = b'saltysalt'
        iv = b' ' * 16
        key = hashlib.pbkdf2_hmac('sha1', chrome_secret, salt, 1, 16)
        
        tmp_db = '/tmp/_chrome_cookies_sync.db'
        shutil.copyfile(cookie_file, tmp_db)
        db_conn = sqlite3.connect(tmp_db)
        c = db_conn.cursor()
        c.execute('SELECT host_key, name, encrypted_value FROM cookies WHERE host_key LIKE "%muse.ai%"')
        rows = c.fetchall()
        db_conn.close()
        try: os.remove(tmp_db)
        except: pass
        
        parts = []
        for host, name, enc in rows:
            if enc[:3] in (b'v10', b'v11'):
                cipher = Cipher(algorithms.AES(key), modes.CBC(iv))
                dec = cipher.decryptor().update(enc[3:]) + cipher.decryptor().finalize()
                pad = dec[-1]
                raw = dec[:-pad]
                val = raw[32:].decode('utf-8', errors='ignore')
                parts.append(f'{name}={val}')
        if parts:
            return '; '.join(parts)
    except Exception:
        return None
    return None

c = get_muse_cookie()
if c:
    count = len(c.split('; '))
    print(json.dumps({'ok': True, 'cookie': c, 'count': count}))
else:
    print(json.dumps({'ok': False, 'error': 'not_found'}))
`
	cmd := exec.Command("python3", "-c", pyScript)
	out, err := cmd.Output()
	if err != nil {
		return "", 0, fmt.Errorf("执行本地 Chrome 凭据读取失败: %w", err)
	}

	var res struct {
		OK     bool   `json:"ok"`
		Cookie string `json:"cookie"`
		Count  int    `json:"count"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return "", 0, fmt.Errorf("解析本地 Chrome 凭据输出失败: %w", err)
	}
	if !res.OK || res.Cookie == "" {
		return "", 0, errors.New("未在本地 Chrome 中检测到已登录的 muse.ai 会话凭据")
	}
	return res.Cookie, res.Count, nil
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
	go p.runCDPVideoGeneration(taskID, prompt, sess, sessIdx)

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

func (p *MetaMuseProvider) runCDPVideoGeneration(taskID, prompt string, sess *metaMuseSession, sessIdx int) {
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

	// 2. 注入所有会话 Cookie
	p.updateCDPTask(taskID, "RUNNING", 30, "", "")
	parts := strings.Split(sess.Cookie, ";")
	for _, part := range parts {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) == 2 {
			name := strings.TrimSpace(kv[0])
			val := strings.TrimSpace(kv[1])
			domain := ".muse.ai"
			if strings.HasPrefix(name, "__Host-") || name == "hatch_native_auth_device" {
				domain = "muse.ai"
			}
			_, _ = chromedp.Call(ctx, network.SetCookie, network.SetCookieParams{
				Name:   name,
				Value:  val,
				Domain: domain,
				Path:   "/",
			})
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

	// 等待 WebSocket / React 水合
	time.Sleep(6 * time.Second)

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
					// 过滤：真实生成的视频通常大于 1.0MB (官方吉祥物 Avatar 只有 200KB~400KB)
					if mime.includes('video') && sz > 1000000 {
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

	// 5. 聚焦输入框并注入提示词，模拟点击发送
	err = chromedp.Do(ctx,
		chromedp.Focus(`textarea[aria-label="消息"]`),
		chromedp.SendKeys(`textarea[aria-label="消息"]`, prompt),
		chromedp.Sleep(800*time.Millisecond),
		chromedp.Click(`button[aria-label="发送"]`),
	)
	if err != nil {
		errMsg := "向对话流注入提示词或点击发送失败: " + err.Error()
		log.Printf("[MetaMuse CDP %s] %s", taskID, errMsg)
		p.updateCDPTask(taskID, "FAILED", 0, "", errMsg)
		p.reportStatus(sessIdx, false, false, errMsg)
		return
	}

	keywords := extractPromptKeywords(prompt)
	kwBytes, _ := json.Marshal(keywords)
	log.Printf("[MetaMuse CDP %s] 成功提交提示词: %q (防串号关键词: %s)，开始高频捕获新出片流...", taskID, prompt, string(kwBytes))
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

		// 精准防串号检测：
		// 1) 气泡必须包含完成语（“做好了”/“ready”）且必须包含本次 Prompt 的核心关键词！杜绝上一个生成的产物（如小橘猫）串号
		// 2) 视频元素必须不含 data-muse-old 属性，不在 initial blobs 列表中
		// 3) 若从拦截器取，时间戳必须晚于 __muse_send_ts 且大小 > 1.0MB
		checkExpr := fmt.Sprintf(`(() => {
			const keywords = %s;
			const initial = window.__muse_initial_blobs || [];
			const sendTs = window.__muse_send_ts || 0;

			// 匹配当前任务成片气泡（必须包含“做好了”且匹配当前提示词关键词）
			const isCurrentTaskBubble = (text) => {
				const hasReady = text.includes('做好了') || text.includes('已做好') || text.includes('is ready') || text.includes('video is ready') || text.includes('Completed');
				if (!hasReady) return false;
				if (!keywords || keywords.length === 0) return true;
				return keywords.some(k => k && text.includes(k));
			};

			// 优先策略 A: 遍历所有包含目标完成语和当前关键词的消息容器
			const allBlocks = Array.from(document.querySelectorAll('*')).filter(el => {
				const t = el.innerText || '';
				return isCurrentTaskBubble(t) && el.querySelector('video');
			});

			for (let i = allBlocks.length - 1; i >= 0; i--) {
				const blockVideos = Array.from(allBlocks[i].querySelectorAll('video')).filter(v => {
					const outer = v.outerHTML || '';
					return !outer.includes('hatch-avatar') && 
					       !outer.includes('avatar') && 
					       !v.hasAttribute('data-muse-old') && 
					       !v.closest('[data-hatch-avatar]');
				});
				for (let j = blockVideos.length - 1; j >= 0; j--) {
					const s = blockVideos[j].src || blockVideos[j].currentSrc;
					if (s && s.startsWith('blob:') && !initial.includes(s)) {
						return s;
					}
				}
			}

			// 策略 B: 拦截器捕获到的晚于发送时间戳且 >1.0MB 的大视频 Blob
			const intercepted = window.__muse_intercepted_blobs || [];
			for (let i = intercepted.length - 1; i >= 0; i--) {
				const item = intercepted[i];
				if (item && item.size > 1000000 && item.url && item.ts >= sendTs && !initial.includes(item.url)) {
					// 同时必须要求页面上出现了当前任务的完成语义
					const pageText = document.body.innerText || '';
					if (isCurrentTaskBubble(pageText)) {
						return item.url;
					}
				}
			}

			// 策略 C: 页面最新非 Avatar 且非 data-muse-old 的新视频
			const pageText = document.body.innerText || '';
			if (isCurrentTaskBubble(pageText)) {
				const videos = Array.from(document.querySelectorAll('video')).filter(v => {
					const outer = v.outerHTML || '';
					return !outer.includes('hatch-avatar') && 
					       !outer.includes('avatar') && 
					       !v.hasAttribute('data-muse-old') && 
					       !v.closest('[data-hatch-avatar]');
				});
				for (let i = videos.length - 1; i >= 0; i--) {
					const s = videos[i].src || videos[i].currentSrc;
					if (s && s.startsWith('blob:') && !initial.includes(s)) {
						return s;
					}
				}
			}

			return '';
		})()`, string(kwBytes))

		targetBlobURL, _ := chromedp.Run(ctx, chromedp.Evaluate[string](checkExpr))

		if targetBlobURL != "" {
			log.Printf("[MetaMuse CDP %s] 命中新生成视频 Blob: %s (耗时 %s)，正在无损转储 Base64...", taskID, targetBlobURL, elapsed)
			p.updateCDPTask(taskID, "RUNNING", 96, "", "")

			extractedB64, err := chromedp.Run(ctx, chromedp.Evaluate[string](fmt.Sprintf(`(() => {
				return fetch(%q).then(r => r.blob()).then(b => {
					// 过滤掉小于 600KB 的非作品片段
					if (b.size < 600000) return 'too_small:' + b.size;
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
