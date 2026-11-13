package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ai-web-free/internal/accounts"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// KamiLoginOutcome 单账号浏览器登录提取结果
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

// resolveChromeExecutable 查找本机可用的 Chrome/Chromium 可执行文件。
func resolveChromeExecutable() (string, error) {
	if envPath := strings.TrimSpace(os.Getenv("CHROME_PATH")); envPath != "" {
		if st, err := os.Stat(envPath); err == nil && !st.IsDir() {
			return envPath, nil
		}
	}
	for _, bin := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser"} {
		if p, err := exec.LookPath(bin); err == nil {
			return p, nil
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		patterns := []string{
			filepath.Join(home, ".cache", "ms-playwright", "chromium-*", "chrome-linux*", "chrome"),
			filepath.Join(home, ".cache", "ms-playwright", "chromium-*", "chrome-linux", "chrome"),
		}
		for _, pat := range patterns {
			if matches, err := filepath.Glob(pat); err == nil && len(matches) > 0 {
				return matches[len(matches)-1], nil
			}
		}
	}
	return "", errors.New("未找到 Chrome/Chromium 可执行文件，请安装 google-chrome/chromium 或设置 CHROME_PATH")
}

// RunKamiPlaywrightLogin 使用纯 Go chromedp (CDP) 驱动本机 Chrome/Chromium 批量登录卡密账号并提取 AccessToken / SessionToken。
func RunKamiPlaywrightLogin(targets []*accounts.Account, defaultProxy string, headless bool) (map[string]KamiLoginOutcome, error) {
	if len(targets) == 0 {
		return map[string]KamiLoginOutcome{}, nil
	}
	chromePath, err := resolveChromeExecutable()
	if err != nil {
		return nil, err
	}
	if !headless && os.Getenv("DISPLAY") == "" {
		_ = os.Setenv("DISPLAY", ":0")
	}

	out := make(map[string]KamiLoginOutcome, len(targets))
	for _, acct := range targets {
		if acct == nil || acct.Email == "" || acct.Password == "" || acct.TOTPSecret == "" {
			continue
		}
		proxyURL := strings.TrimSpace(acct.Proxy)
		if proxyURL == "" {
			proxyURL = strings.TrimSpace(defaultProxy)
		}
		outcome := loginSingleAccountCDP(chromePath, acct.Email, acct.Password, acct.TOTPSecret, proxyURL, headless)
		out[strings.ToLower(strings.TrimSpace(acct.Email))] = outcome
	}
	return out, nil
}

func loginSingleAccountCDP(chromePath, email, password, totpSecret, proxyURL string, headless bool) KamiLoginOutcome {
	outcome := KamiLoginOutcome{Email: email}

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chromePath),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.UserAgent("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36"),
		chromedp.WindowSize(1280, 800),
	)
	if headless {
		opts = append(opts, chromedp.Flag("headless", "new"))
	} else {
		opts = append(opts, chromedp.Flag("headless", false))
	}
	if proxyURL != "" {
		opts = append(opts, chromedp.ProxyServer(proxyURL))
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer allocCancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	ctx, timeoutCancel := context.WithTimeout(ctx, 130*time.Second)
	defer timeoutCancel()

	var mu sync.Mutex
	var capturedAccessToken string
	var capturedUserID string

	setToken := func(token, userID string) {
		mu.Lock()
		defer mu.Unlock()
		if capturedAccessToken == "" && token != "" {
			capturedAccessToken = token
		}
		if capturedUserID == "" && userID != "" {
			capturedUserID = userID
		}
	}
	getToken := func() (string, string) {
		mu.Lock()
		defer mu.Unlock()
		return capturedAccessToken, capturedUserID
	}

	if err := chromedp.Listen(ctx, func(ev any) {
		switch e := ev.(type) {
		case *network.EventRequestWillBeSent:
			for k, v := range e.Request.Headers {
				if strings.EqualFold(k, "authorization") {
					if auth, ok := v.(string); ok && strings.HasPrefix(auth, "Bearer ey") {
						tok := strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
						if len(tok) > 50 {
							setToken(tok, "")
						}
					}
				}
			}
		case *network.EventResponseReceived:
			if strings.Contains(e.Response.URL, "/api/auth/session") && e.Response.Status == 200 {
				reqID := e.RequestID
				go func() {
					cctx, ccancel := context.WithTimeout(ctx, 5*time.Second)
					defer ccancel()
					res, err := chromedp.Call(cctx, network.GetResponseBody, network.GetResponseBodyParams{RequestID: reqID})
					if err != nil {
						return
					}
					var sessionResp struct {
						AccessToken string `json:"accessToken"`
						User        struct {
							ID string `json:"id"`
						} `json:"user"`
					}
					if err := json.Unmarshal([]byte(res.Body), &sessionResp); err == nil && sessionResp.AccessToken != "" {
						setToken(sessionResp.AccessToken, sessionResp.User.ID)
					}
				}()
			}
		}
	}); err != nil {
		outcome.Error = fmt.Sprintf("注册网络监听失败: %v", err)
		return outcome
	}

	if _, err := chromedp.Call(ctx, network.Enable, network.EnableParams{}); err != nil {
		outcome.Error = fmt.Sprintf("启用网络监听失败: %v", err)
		return outcome
	}
	if err := chromedp.Do(ctx, chromedp.Navigate("https://chatgpt.com/auth/login")); err != nil {
		outcome.Error = fmt.Sprintf("打开登录页失败: %v", err)
		return outcome
	}

	// 若落地页有 Log in 按钮则先点击
	time.Sleep(2 * time.Second)
	clickedLogin, _ := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
		const btns = Array.from(document.querySelectorAll('button'));
		const b = btns.find(el => el.offsetParent !== null && /\blog\s*in\b/i.test(el.innerText || ''));
		if (b) { b.click(); return true; }
		return false;
	})()`))
	if clickedLogin {
		time.Sleep(2 * time.Second)
	}

	// 1. 填写邮箱并点击 Continue
	emailSel := `input[name="username"], input[type="email"], #email-input`
	if err := waitVisibleAndType(ctx, emailSel, email, 25*time.Second); err == nil {
		time.Sleep(500 * time.Millisecond)
		_ = clickContinueButton(ctx)
	}

	// 2. 填写密码并点击 Continue
	passSel := `input[name="password"], input[type="password"]`
	if err := waitVisibleAndType(ctx, passSel, password, 25*time.Second); err == nil {
		time.Sleep(500 * time.Millisecond)
		_ = clickContinueButton(ctx)
	}

	// 3. 填写 2FA TOTP
	totpSel := `input[inputmode="numeric"], input[name="code"], input[autocomplete="one-time-code"], input[placeholder*="code" i]`
	if err := waitVisible(ctx, totpSel, 12*time.Second); err == nil {
		if code, _, totpErr := accounts.GenerateTOTP(totpSecret, time.Now()); totpErr == nil && code != "" {
			_ = chromedp.Do(ctx,
				chromedp.Focus(chromedp.CSS(totpSel)),
				chromedp.SendKeys(chromedp.CSS(totpSel), code),
			)
			time.Sleep(500 * time.Millisecond)
			_ = clickContinueButton(ctx)
		}
	}

	// 4. 轮询等待登录完成并提取 Token
	deadline := time.Now().Add(95 * time.Second)
	for time.Now().Before(deadline) {
		if tok, _ := getToken(); tok != "" {
			break
		}

		currentURL, _ := chromedp.Run(ctx, chromedp.Location())
		if strings.Contains(currentURL, "mfa-challenge") {
			_ = clickContinueButton(ctx)
		}

		if strings.Contains(currentURL, "chatgpt.com") && !strings.Contains(currentURL, "auth0") && !strings.Contains(currentURL, "auth.openai.com") {
			sessionJSON, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(async () => {
				try {
					const r = await fetch('/api/auth/session');
					if (!r.ok) return '';
					return await r.text();
				} catch (e) { return ''; }
			})()`, chromedp.EvalAwaitPromise))
			if err == nil && strings.Contains(sessionJSON, "accessToken") {
				var parsed struct {
					AccessToken string `json:"accessToken"`
					User        struct {
						ID string `json:"id"`
					} `json:"user"`
				}
				if jErr := json.Unmarshal([]byte(sessionJSON), &parsed); jErr == nil && parsed.AccessToken != "" {
					setToken(parsed.AccessToken, parsed.User.ID)
					break
				}
			}
		}
		time.Sleep(2 * time.Second)
	}

	accessToken, userID := getToken()
	if accessToken == "" {
		currentURL, _ := chromedp.Run(ctx, chromedp.Location())
		outcome.Error = fmt.Sprintf("未能获取到 AccessToken (当前 URL: %s)", currentURL)
		return outcome
	}

	// 提取 __Secure-next-auth.session-token Cookie
	var sessionCookie string
	if cookieRes, err := chromedp.Call(ctx, network.GetCookies, network.GetCookiesParams{URLs: []string{"https://chatgpt.com"}}); err == nil {
		for _, ck := range cookieRes.Cookies {
			if ck.Name == "__Secure-next-auth.session-token" && ck.Value != "" {
				sessionCookie = ck.Value
				break
			}
		}
	}

	chatGPTAccountID := accounts.ExtractChatGPTAccountID(accessToken)
	if chatGPTAccountID == "" {
		chatGPTAccountID = userID
	}
	expiresAt := int64(0)
	if exp := accounts.ExtractExpiresAt(accessToken); !exp.IsZero() {
		expiresAt = exp.Unix()
	} else {
		expiresAt = time.Now().Add(10 * 24 * time.Hour).Unix()
	}

	outcome.OK = true
	outcome.AccessToken = accessToken
	outcome.SessionToken = sessionCookie
	outcome.ChatGPTAccountID = chatGPTAccountID
	outcome.ExpiresAt = expiresAt
	return outcome
}

func waitVisible(ctx context.Context, selector string, timeout time.Duration) error {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return chromedp.Do(cctx, chromedp.WaitVisible(chromedp.CSS(selector)))
}

func waitVisibleAndType(ctx context.Context, selector, value string, timeout time.Duration) error {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return chromedp.Do(cctx,
		chromedp.WaitVisible(chromedp.CSS(selector)),
		chromedp.Focus(chromedp.CSS(selector)),
		chromedp.SendKeys(chromedp.CSS(selector), value),
	)
}

func clickContinueButton(ctx context.Context) error {
	_, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
		const selectors = ['button[type="submit"]', 'button[name="action"][value="default"]'];
		for (const s of selectors) {
			const el = document.querySelector(s);
			if (el && el.offsetParent !== null) { el.click(); return true; }
		}
		const btns = Array.from(document.querySelectorAll('button'));
		const match = btns.find(b => b.offsetParent !== null && /^(continue|verify|next|继续|验证|下一步)$/i.test((b.innerText || '').trim()));
		if (match) { match.click(); return true; }
		return false;
	})()`))
	return err
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
