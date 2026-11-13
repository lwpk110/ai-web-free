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
	pool       *accounts.Pool
	proxyPool  *proxy.Pool
	cfg        *config.Config
	renewFn    func(*accounts.Account) bool
	kamiWorker *KamiRenewWorker
	auditLog   *audit.RingBuffer
	fileMu     sync.Mutex
}

func NewChatGPTProvider(pool *accounts.Pool, proxyPool *proxy.Pool, cfg *config.Config, renewFn func(*accounts.Account) bool) *ChatGPTProvider {
	return &ChatGPTProvider{
		pool:      pool,
		proxyPool: proxyPool,
		cfg:       cfg,
		renewFn:   renewFn,
	}
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

func (p *ChatGPTProvider) Descriptor() ProviderDescriptor {
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
		FeaturedModels: []string{
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
		},
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

func (p *ChatGPTProvider) ExecuteAccountAction(id, action string) (map[string]interface{}, error) {
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
		if p.renewFn != nil && p.renewFn(acct) {
			acct.SyncJWTMetadata()
			acct.LastChecked = time.Now()
			acct.LastError = ""
			p.pool.SetStatus(acct.ID, accounts.StatusActive)
			return map[string]interface{}{
				"id":         acct.ID,
				"status":     acct.Status.String(),
				"expires_at": acct.ExpiresAt.Format(time.RFC3339),
			}, nil
		}
		acct.LastChecked = time.Now()
		acct.LastError = "Token 续期失败"
		p.pool.SetStatus(acct.ID, accounts.StatusExpired)
		return nil, errors.New("Token 续期请求失败")

	case "probe":
		start := time.Now()
		acct.LastChecked = start
		if acct.Client == nil {
			_ = acct.InitClient()
		}
		if acct.Type == accounts.TypeNoAuth {
			return map[string]interface{}{
				"id":         acct.ID,
				"status":     acct.Status.String(),
				"latency_ms": time.Since(start).Milliseconds(),
				"message":    "免登 UUID 账号就绪",
			}, nil
		}
		models, httpStatus, err := chatgpt.ListModels(acct.Client, acct)
		latency := time.Since(start).Milliseconds()
		if err != nil {
			acct.LastError = fmt.Sprintf("HTTP %d: %v", httpStatus, err)
			if httpStatus == 401 || httpStatus == 403 {
				p.pool.MarkExpired(acct, acct.LastError)
				if p.kamiWorker != nil {
					p.kamiWorker.Enqueue(acct, "probe_unauthorized")
				}
			} else if httpStatus == 429 {
				p.pool.ReportRateLimit(acct, false, 5*time.Minute, acct.LastError)
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
		return map[string]interface{}{
			"id":           acct.ID,
			"ok":           true,
			"status":       acct.Status.String(),
			"latency_ms":   latency,
			"models_count": len(slugs),
			"models":       slugs,
		}, nil

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
		if acct.Email == "" || acct.Password == "" || acct.TOTPSecret == "" {
			return nil, errors.New("该账号未配置邮箱/密码/2FA 卡密凭据，无法自动登录")
		}
		outcomes, err := p.loginKamiAccounts([]*accounts.Account{acct}, true)
		if err != nil {
			acct.LastError = err.Error()
			return nil, err
		}
		_ = p.PersistAccessTokens()
		outcome := outcomes[strings.ToLower(strings.TrimSpace(acct.Email))]
		if !outcome.OK {
			return nil, errors.New(outcome.Error)
		}
		return map[string]interface{}{
			"id":         acct.ID,
			"email":      acct.Email,
			"status":     acct.Status.String(),
			"expires_at": acct.ExpiresAt.Format(time.RFC3339),
			"message":    fmt.Sprintf("账号 %s 登录提取 Token 成功，已激活进入号池", acct.Email),
		}, nil

	default:
		return nil, fmt.Errorf("未知操作指令: %s", action)
	}
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
