package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"ai-web-free/internal/accounts"
)

// KamiLoginOutcome 单账号 Playwright 登录提取结果
type KamiLoginOutcome struct {
	Email            string `json:"email"`
	OK               bool   `json:"ok"`
	Skipped          bool   `json:"skipped,omitempty"`
	AccessToken      string `json:"access_token,omitempty"`
	SessionToken     string `json:"session_token,omitempty"`
	ChatGPTAccountID string `json:"chatgpt_account_id,omitempty"`
	ExpiresAt        int64  `json:"expires_at,omitempty"`
	Error            string `json:"error,omitempty"`
}

type kamiWorkerInput struct {
	Proxy    string            `json:"proxy,omitempty"`
	Accounts []kamiWorkerEntry `json:"accounts"`
}

type kamiWorkerEntry struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	TOTPSecret string `json:"totp_secret"`
	Proxy      string `json:"proxy,omitempty"`
}

type kamiWorkerOutput struct {
	OK      bool               `json:"ok"`
	Error   string             `json:"error,omitempty"`
	Results []KamiLoginOutcome `json:"results"`
}

func resolveKamiScriptPath() (string, error) {
	candidates := []string{
		filepath.Join("scripts", "kami_login.py"),
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "scripts", "kami_login.py"))
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	return "", errors.New("未找到 scripts/kami_login.py 登录脚本")
}

// RunKamiPlaywrightLogin 调用 Python Playwright 脚本批量登录卡密账号并提取 AccessToken / SessionToken。
func RunKamiPlaywrightLogin(targets []*accounts.Account, defaultProxy string, headless bool) (map[string]KamiLoginOutcome, error) {
	if len(targets) == 0 {
		return map[string]KamiLoginOutcome{}, nil
	}
	scriptPath, err := resolveKamiScriptPath()
	if err != nil {
		return nil, err
	}
	pythonBin := "python3"
	if _, err := exec.LookPath(pythonBin); err != nil {
		pythonBin = "python"
		if _, err2 := exec.LookPath(pythonBin); err2 != nil {
			return nil, errors.New("系统未安装 python3，无法自动执行 Playwright 登录")
		}
	}

	input := kamiWorkerInput{
		Proxy:    defaultProxy,
		Accounts: make([]kamiWorkerEntry, 0, len(targets)),
	}
	for _, a := range targets {
		if a == nil || a.Email == "" || a.Password == "" || a.TOTPSecret == "" {
			continue
		}
		input.Accounts = append(input.Accounts, kamiWorkerEntry{
			Email:      a.Email,
			Password:   a.Password,
			TOTPSecret: a.TOTPSecret,
			Proxy:      a.Proxy,
		})
	}
	if len(input.Accounts) == 0 {
		return map[string]KamiLoginOutcome{}, nil
	}

	payloadBytes, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}

	timeout := time.Duration(len(input.Accounts)) * 3 * time.Minute
	if timeout < 3*time.Minute {
		timeout = 3 * time.Minute
	}
	if timeout > 20*time.Minute {
		timeout = 20 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	args := []string{scriptPath, "--stdin-json", "--json-output"}
	if headless {
		args = append(args, "--headless")
	}
	cmd := exec.CommandContext(ctx, pythonBin, args...)
	cmd.Stdin = bytes.NewReader(payloadBytes)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errMsg := strings.TrimSpace(stderr.String())
		if errMsg == "" {
			errMsg = strings.TrimSpace(stdout.String())
		}
		if errMsg == "" {
			errMsg = err.Error()
		}
		return nil, fmt.Errorf("Playwright 登录脚本执行失败: %s", errMsg)
	}

	var parsed kamiWorkerOutput
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		return nil, fmt.Errorf("解析登录脚本输出失败: %w (stdout=%s)", err, strings.TrimSpace(stdout.String()))
	}
	if !parsed.OK && parsed.Error != "" {
		return nil, errors.New(parsed.Error)
	}

	out := make(map[string]KamiLoginOutcome, len(parsed.Results))
	for _, r := range parsed.Results {
		out[strings.ToLower(strings.TrimSpace(r.Email))] = r
	}
	return out, nil
}

func (p *ChatGPTProvider) importKamiAccounts(rawText string, persist, autoLogin, headless bool) (ImportResult, error) {
	var res ImportResult
	parsed := accounts.ParseKamiText(rawText)
	if len(parsed) == 0 {
		return res, errors.New("未解析到有效的卡密账号（格式: 邮箱----密码----2FA密钥）")
	}

	profiles := accounts.DefaultProfiles
	var loginCandidates []*accounts.Account

	for _, k := range parsed {
		if existing := p.pool.FindByEmail(k.Email); existing != nil {
			existing.Password = k.Password
			existing.TOTPSecret = k.TOTPSecret
			if existing.Status == accounts.StatusActive && existing.Token != "" {
				res.Skipped++
			} else {
				loginCandidates = append(loginCandidates, existing)
				res.Skipped++
			}
			continue
		}

		acct := accounts.CreateAccount("", accounts.TypeFree, profiles)
		acct.Email = k.Email
		acct.Password = k.Password
		acct.TOTPSecret = k.TOTPSecret
		acct.Proxy = p.allocateProxy()
		acct.Status = accounts.StatusPending
		acct.LastError = "待浏览器登录提取 Token"
		_ = acct.InitClient()
		p.pool.AddAccount(acct)
		loginCandidates = append(loginCandidates, acct)
		res.Added++
	}

	if persist {
		if err := p.PersistKamiAccounts(); err == nil {
			res.Persisted = true
		}
	}

	if autoLogin && len(loginCandidates) > 0 {
		outcomes, err := p.loginKamiAccounts(loginCandidates, headless)
		if err != nil {
			res.Messages = append(res.Messages, err.Error())
		} else {
			okCount := 0
			failCount := 0
			for _, o := range outcomes {
				if o.OK {
					okCount++
				} else {
					failCount++
					if o.Error != "" {
						res.Messages = append(res.Messages, fmt.Sprintf("%s: %s", o.Email, o.Error))
					}
				}
			}
			if okCount > 0 && persist {
				_ = p.PersistAccessTokens()
			}
			res.Messages = append(res.Messages, fmt.Sprintf("自动登录完成: 成功 %d, 失败 %d", okCount, failCount))
		}
	}

	res.TotalPool = p.pool.Stats().Total
	return res, nil
}

func (p *ChatGPTProvider) importSub2APIJSON(rawJSON string, persist bool) (ImportResult, error) {
	var res ImportResult
	data, err := accounts.ParseSub2APIJSON(rawJSON)
	if err != nil {
		return res, err
	}

	profiles := accounts.DefaultProfiles
	for _, sub := range data.Accounts {
		email := strings.TrimSpace(sub.Extra.Email)
		if email == "" {
			email = strings.TrimSpace(sub.Name)
		}
		token := strings.TrimSpace(sub.Credentials.AccessToken)
		sessionToken := strings.TrimSpace(sub.Credentials.SessionToken)
		refreshToken := strings.TrimSpace(sub.Credentials.RefreshToken)

		if token == "" && refreshToken == "" && sessionToken == "" {
			if email != "" && sub.Extra.Password != "" && sub.Extra.TOTPSecret != "" {
				if existing := p.pool.FindByEmail(email); existing != nil {
					existing.Password = sub.Extra.Password
					existing.TOTPSecret = sub.Extra.TOTPSecret
					res.Skipped++
					continue
				}
				acct := accounts.CreateAccount("", accounts.TypeFree, profiles)
				acct.Email = email
				acct.Password = sub.Extra.Password
				acct.TOTPSecret = sub.Extra.TOTPSecret
				acct.Proxy = p.allocateProxy()
				acct.Status = accounts.StatusPending
				_ = acct.InitClient()
				p.pool.AddAccount(acct)
				res.Added++
			} else {
				res.Failed++
			}
			continue
		}

		// 若池内已有同邮箱的 Pending 账号，直接激活并填充 Token
		if existing := p.pool.FindByEmail(email); existing != nil && (existing.Token == "" || existing.Status != accounts.StatusActive) {
			if token != "" {
				existing.Token = token
			}
			if sessionToken != "" {
				existing.SessionToken = sessionToken
			}
			if refreshToken != "" {
				existing.RefreshToken = refreshToken
			}
			if sub.Extra.Password != "" {
				existing.Password = sub.Extra.Password
			}
			if sub.Extra.TOTPSecret != "" {
				existing.TOTPSecret = sub.Extra.TOTPSecret
			}
			existing.SyncJWTMetadata()
			if existing.ChatGPTAccountID == "" && sub.Credentials.ChatGPTAccountID != "" {
				existing.ChatGPTAccountID = sub.Credentials.ChatGPTAccountID
			}
			if existing.Client == nil {
				_ = existing.InitClient()
			}
			existing.LastError = ""
			p.pool.SetStatus(existing.ID, accounts.StatusActive)
			res.Added++
			continue
		}

		chatGPTID := sub.Credentials.ChatGPTAccountID
		if token != "" && chatGPTID == "" {
			chatGPTID = accounts.ExtractChatGPTAccountID(token)
		}
		if token != "" && p.pool.HasChatGPTAccountID(chatGPTID, token) {
			res.Skipped++
			continue
		}

		acct := accounts.CreateAccount(token, accounts.TypeFree, profiles)
		if email != "" {
			acct.Email = email
		}
		acct.Password = sub.Extra.Password
		acct.TOTPSecret = sub.Extra.TOTPSecret
		acct.SessionToken = sessionToken
		acct.RefreshToken = refreshToken
		acct.SyncJWTMetadata()
		if acct.ChatGPTAccountID == "" {
			acct.ChatGPTAccountID = chatGPTID
		}
		acct.Proxy = p.allocateProxy()
		acct.Status = accounts.StatusActive
		_ = acct.InitClient()
		p.pool.AddAccount(acct)
		res.Added++
	}

	if persist && res.Added > 0 {
		if err := p.PersistAccessTokens(); err == nil {
			res.Persisted = true
		}
		_ = p.PersistKamiAccounts()
	}

	res.TotalPool = p.pool.Stats().Total
	return res, nil
}

func (p *ChatGPTProvider) loginKamiAccounts(targets []*accounts.Account, headless bool) (map[string]KamiLoginOutcome, error) {
	defaultProxy := ""
	if p.cfg != nil {
		defaultProxy = p.cfg.ProxyURL
		if defaultProxy == "" {
			defaultProxy = p.cfg.HTTPProxy
		}
	}
	outcomes, err := RunKamiPlaywrightLogin(targets, defaultProxy, headless)
	if err != nil {
		return nil, err
	}

	for _, acct := range targets {
		key := strings.ToLower(strings.TrimSpace(acct.Email))
		o, ok := outcomes[key]
		if !ok {
			continue
		}
		acct.LastChecked = time.Now()
		if o.OK && o.AccessToken != "" {
			acct.Token = o.AccessToken
			if o.SessionToken != "" {
				acct.SessionToken = o.SessionToken
			}
			acct.SyncJWTMetadata()
			if acct.ChatGPTAccountID == "" && o.ChatGPTAccountID != "" {
				acct.ChatGPTAccountID = o.ChatGPTAccountID
			}
			if acct.ExpiresAt.IsZero() && o.ExpiresAt > 0 {
				acct.ExpiresAt = time.Unix(o.ExpiresAt, 0)
			}
			acct.LastError = ""
			p.pool.SetStatus(acct.ID, accounts.StatusActive)
		} else if !o.OK && o.Error != "" {
			acct.LastError = o.Error
		}
	}

	return outcomes, nil
}

// LoginPendingKami 批量为池内尚未激活/已过期的卡密账号执行 Playwright 登录提取 Token。
func (p *ChatGPTProvider) LoginPendingKami(headless, force bool) (map[string]interface{}, error) {
	candidates := p.pool.ListKamiCandidates(!force)
	if len(candidates) == 0 {
		return map[string]interface{}{
			"total":   0,
			"success": 0,
			"failed":  0,
			"message": "没有需要登录提取 Token 的卡密账号",
		}, nil
	}

	outcomes, err := p.loginKamiAccounts(candidates, headless)
	if err != nil {
		return nil, err
	}
	success := 0
	failed := 0
	var details []KamiLoginOutcome
	for _, o := range outcomes {
		if o.OK {
			success++
		} else {
			failed++
		}
		details = append(details, o)
	}
	if success > 0 {
		_ = p.PersistAccessTokens()
	}
	return map[string]interface{}{
		"total":   len(candidates),
		"success": success,
		"failed":  failed,
		"results": details,
	}, nil
}

// PersistKamiAccounts 将池内带卡密凭据的非临时账号原子写回 kami_accounts.txt。
func (p *ChatGPTProvider) PersistKamiAccounts() error {
	p.fileMu.Lock()
	defer p.fileMu.Unlock()

	list := p.pool.ExportKamiAccounts()
	if len(list) == 0 {
		return nil
	}
	content := accounts.FormatKamiLines(list)
	return writeAtomic("kami_accounts.txt", []byte(content))
}
