package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"ai-web-free/internal/accounts"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
	"github.com/google/uuid"
)

type KamiPhaseReporter func(accountID, phase, phaseLabel string)

type KamiBatchLoginExecutor func(targets []*accounts.Account, defaultProxy string, headless bool, onPhase KamiPhaseReporter) (map[string]KamiLoginOutcome, error)

type kamiLoginQueueItem struct {
	account  *accounts.Account
	headless bool
}

type KamiLoginTaskResult struct {
	AccountID string `json:"account_id"`
	Email     string `json:"email"`
	OK        bool   `json:"ok"`
	Cancelled bool   `json:"cancelled,omitempty"`
	Error     string `json:"error,omitempty"`
}

type KamiLoginTask struct {
	ID                string                `json:"id"`
	Status            string                `json:"status"`
	Total             int                   `json:"total"`
	Completed         int                   `json:"completed"`
	Success           int                   `json:"success"`
	Failed            int                   `json:"failed"`
	Cancelled         int                   `json:"cancelled,omitempty"`
	CurrentAccountID  string                `json:"current_account_id,omitempty"`
	CurrentEmail      string                `json:"current_email,omitempty"`
	CurrentPhase      string                `json:"current_phase,omitempty"`
	CurrentPhaseLabel string                `json:"current_phase_label,omitempty"`
	CurrentStartedAt  string                `json:"current_started_at,omitempty"`
	QueuedAccountIDs  []string              `json:"queued_account_ids,omitempty"`
	Dismissed         bool                  `json:"dismissed,omitempty"`
	Results           []KamiLoginTaskResult `json:"results"`
	CreatedAt         string                `json:"created_at"`
	UpdatedAt         string                `json:"updated_at"`
}

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
	for _, bin := range []string{"google-chrome", "google-chrome-stable", "chromium", "chromium-browser", "chrome.exe", "chromium.exe"} {
		if p, err := exec.LookPath(bin); err == nil {
			return p, nil
		}
	}
	candidates := []string{}
	switch runtime.GOOS {
	case "windows":
		for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("LocalAppData")} {
			if base != "" {
				candidates = append(candidates, filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe"))
			}
		}
	case "darwin":
		candidates = append(candidates,
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
		)
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates,
				filepath.Join(home, "Applications", "Google Chrome.app", "Contents", "MacOS", "Google Chrome"),
				filepath.Join(home, "Applications", "Chromium.app", "Contents", "MacOS", "Chromium"),
			)
		}
	}
	for _, p := range candidates {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		patterns := []string{
			filepath.Join(home, ".cache", "ms-playwright", "chromium-*", "chrome-linux*", "chrome"),
			filepath.Join(home, "AppData", "Local", "ms-playwright", "chromium-*", "chrome-win", "chrome.exe"),
			filepath.Join(home, "AppData", "Local", "ms-playwright", "chromium-*", "chrome-win64", "chrome.exe"),
			filepath.Join(home, "Library", "Caches", "ms-playwright", "chromium-*", "chrome-mac", "Chromium.app", "Contents", "MacOS", "Chromium"),
			filepath.Join(home, "Library", "Caches", "ms-playwright", "chromium-*", "chrome-mac-arm64", "Chromium.app", "Contents", "MacOS", "Chromium"),
		}
		for _, pat := range patterns {
			if matches, err := filepath.Glob(pat); err == nil && len(matches) > 0 {
				return matches[len(matches)-1], nil
			}
		}
	}
	return "", errors.New("未找到 Chrome/Chromium 可执行文件，请安装 Chrome/Chromium、将其加入 PATH 或设置 CHROME_PATH")
}

// KamiPhaseLabel 将内部登录阶段 key 映射为中文用户可读阶段名。
func KamiPhaseLabel(phase string) string {
	switch phase {
	case "queued":
		return "排队等待重登"
	case "checking_proxy":
		return "代理连通预检"
	case "launch_browser":
		return "启动浏览器实例"
	case "open_login":
		return "打开登录页"
	case "enter_email":
		return "填充账号邮箱"
	case "enter_password":
		return "填充登录密码"
	case "enter_totp":
		return "填充 2FA 动态码"
	case "waiting_challenge":
		return "等待安全验证过盾"
	case "wait_session_token":
		return "提取会话 Token"
	case "cooldown_wait":
		return "等待账号间隔"
	case "probing":
		return "正在测活"
	case "refreshing":
		return "正在刷新 Token"
	default:
		return phase
	}
}

func summarizeActionError(errMsg string) string {
	s := strings.TrimSpace(errMsg)
	if idx := strings.Index(s, "\n"); idx >= 0 {
		s = strings.TrimSpace(s[:idx])
	}
	runes := []rune(s)
	if len(runes) > 72 {
		return string(runes[:72]) + "..."
	}
	return s
}

// RunKamiPlaywrightLogin 使用纯 Go chromedp (CDP) 驱动本机 Chrome/Chromium 批量登录卡密账号并提取 AccessToken / SessionToken。
func RunKamiPlaywrightLogin(targets []*accounts.Account, defaultProxy string, headless bool) (map[string]KamiLoginOutcome, error) {
	return RunKamiPlaywrightLoginWithProgress(targets, defaultProxy, headless, nil)
}

// RunKamiPlaywrightLoginWithProgress 与 RunKamiPlaywrightLogin 相同，但在关键登录步骤触发实时阶段回调。
func RunKamiPlaywrightLoginWithProgress(targets []*accounts.Account, defaultProxy string, headless bool, onPhase KamiPhaseReporter) (map[string]KamiLoginOutcome, error) {
	if len(targets) == 0 {
		return map[string]KamiLoginOutcome{}, nil
	}
	chromePath, err := resolveChromeExecutable()
	if err != nil {
		return nil, err
	}
	if !headless && runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" {
		_ = os.Setenv("DISPLAY", ":0")
	}

	out := make(map[string]KamiLoginOutcome, len(targets))
	for _, acct := range targets {
		if acct == nil || acct.Email == "" || acct.Password == "" || acct.TOTPSecret == "" {
			continue
		}
		if len(out) > 0 {
			time.Sleep(10 * time.Second)
		}
		proxyURL := strings.TrimSpace(acct.Proxy)
		if proxyURL == "" {
			proxyURL = strings.TrimSpace(defaultProxy)
		}
		startedAt := time.Now()
		log.Printf("[kami-login] starting account=%s proxy_bound=%t headless=%t", acct.Email, proxyURL != "", headless)
		outcome := loginSingleAccountCDP(chromePath, acct.ID, acct.Email, acct.Password, acct.TOTPSecret, proxyURL, headless, onPhase)
		if outcome.OK {
			log.Printf("[kami-login] succeeded account=%s elapsed=%s", acct.Email, time.Since(startedAt).Round(time.Second))
		} else {
			log.Printf("[kami-login] failed account=%s elapsed=%s error=%s", acct.Email, time.Since(startedAt).Round(time.Second), outcome.Error)
		}
		out[strings.ToLower(strings.TrimSpace(acct.Email))] = outcome
	}
	return out, nil
}

func loginSingleAccountCDP(chromePath, accountID, email, password, totpSecret, proxyURL string, headless bool, onPhase KamiPhaseReporter) KamiLoginOutcome {
	outcome := KamiLoginOutcome{Email: email}
	phase := "launch_browser"
	lastStepError := ""
	reportPhase := func(nextPhase string) {
		phase = nextPhase
		if onPhase != nil && accountID != "" {
			onPhase(accountID, nextPhase, KamiPhaseLabel(nextPhase))
		}
	}
	reportPhase("launch_browser")

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chromePath),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("incognito", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.WindowSize(1280, 800),
	)
	if runtime.GOOS == "linux" {
		opts = append(opts,
			chromedp.Flag("no-sandbox", true),
			chromedp.Flag("disable-dev-shm-usage", true),
		)
	}
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

	loginTimeout := 210 * time.Second
	if !headless {
		loginTimeout = 360 * time.Second
	}
	ctx, timeoutCancel := context.WithTimeout(ctx, loginTimeout)
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
	reportPhase("open_login")
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
	reportPhase("enter_email")
	inputTimeout := 25 * time.Second
	if !headless {
		inputTimeout = 120 * time.Second
	}
	if err := waitVisibleAndType(ctx, emailSel, email, inputTimeout, headless, email, reportPhase, "enter_email"); err == nil {
		time.Sleep(500 * time.Millisecond)
		if err := clickContinueButton(ctx); err != nil {
			lastStepError = "提交邮箱失败: " + err.Error()
		}
	} else {
		lastStepError = "邮箱输入框未出现: " + err.Error() + "; " + loginPageState(ctx)
		log.Printf("[kami-login] email step failed account=%s error=%s", email, lastStepError)
	}

	// 2. 填写密码并点击 Continue
	passSel := `input[name="password"], input[type="password"]`
	reportPhase("enter_password")
	if err := waitVisibleAndType(ctx, passSel, password, inputTimeout, headless, email, reportPhase, "enter_password"); err == nil {
		time.Sleep(500 * time.Millisecond)
		if err := clickContinueButton(ctx); err != nil {
			lastStepError = "提交密码失败: " + err.Error()
		}
	} else {
		lastStepError = "密码输入框未出现: " + err.Error() + "; " + loginPageState(ctx)
		log.Printf("[kami-login] password step failed account=%s error=%s", email, lastStepError)
	}

	// 3. 填写 2FA TOTP
	totpSel := `input[inputmode="numeric"], input[name="code"], input[autocomplete="one-time-code"], input[placeholder*="code" i]`
	reportPhase("enter_totp")
	totpTimeout := 12 * time.Second
	if !headless {
		totpTimeout = 120 * time.Second
	}
	if err := waitVisible(ctx, totpSel, totpTimeout, headless, email, reportPhase, "enter_totp"); err == nil {
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
	reportPhase("wait_session_token")
	loginStartedAt := time.Now()
	deadline := time.Now().Add(120 * time.Second)
	sessionProbeAttempted := false
	lastKnownURL := ""
	for time.Now().Before(deadline) {
		if tok, _ := getToken(); tok != "" {
			break
		}

		currentURL, _ := chromedp.Run(ctx, chromedp.Location())
		if currentURL != "" && currentURL != lastKnownURL {
			lastKnownURL = currentURL
			log.Printf("[kami-login] page account=%s url=%s", email, sanitizeLoginURL(currentURL))
		}
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
			if !sessionProbeAttempted && time.Since(loginStartedAt) > 15*time.Second {
				sessionProbeAttempted = true
				if sessionJSON, err := readSessionFromNewTab(ctx); err == nil {
					var parsed struct {
						AccessToken string `json:"accessToken"`
						User        struct {
							ID string `json:"id"`
						} `json:"user"`
					}
					if json.Unmarshal([]byte(sessionJSON), &parsed) == nil && parsed.AccessToken != "" {
						setToken(parsed.AccessToken, parsed.User.ID)
						break
					}
				}
			}
		}
		time.Sleep(2 * time.Second)
	}

	accessToken, userID := getToken()
	if accessToken == "" {
		currentURL, urlErr := chromedp.Run(ctx, chromedp.Location())
		if currentURL != "" {
			lastKnownURL = currentURL
		}
		outcome.Error = fmt.Sprintf("未能获取到 AccessToken (phase=%s, last_url=%q, url_error=%v, last_step=%s)", phase, sanitizeLoginURL(lastKnownURL), urlErr, lastStepError)
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

func sanitizeLoginURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}

func readSessionFromNewTab(parent context.Context) (string, error) {
	ctx, cancel := chromedp.NewContext(parent)
	defer cancel()
	ctx, timeoutCancel := context.WithTimeout(ctx, 12*time.Second)
	defer timeoutCancel()
	if err := chromedp.Do(ctx, chromedp.Navigate("https://chatgpt.com/api/auth/session")); err != nil {
		return "", err
	}
	return chromedp.Run(ctx, chromedp.Evaluate[string]("document.body.innerText"))
}

func waitVisible(ctx context.Context, selector string, timeout time.Duration, headless bool, email string, reportPhase func(string), resumePhase string) error {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	selectorJSON, _ := json.Marshal(selector)
	expression := `(() => {
		const title = (document.title || '').toLowerCase();
		const body = (document.body?.innerText || '').toLowerCase();
		return {
			visible: Array.from(document.querySelectorAll(` + string(selectorJSON) + `)).some(el => el.offsetParent !== null),
			challenge: /please wait|请稍候|checking your browser|verify you are human|验证您是人类/.test(title + ' ' + body) || Boolean(document.querySelector('#challenge-form, .cf-browser-verification, iframe[src*="challenges.cloudflare.com"]'))
		};
	})()`
	challengeReported := false
	for {
		state, err := chromedp.Run(cctx, chromedp.Evaluate[map[string]bool](expression))
		if err != nil {
			return err
		}
		if state["visible"] {
			if challengeReported && reportPhase != nil && resumePhase != "" {
				reportPhase(resumePhase)
			}
			return nil
		}
		if state["challenge"] {
			if headless {
				return errors.New("检测到浏览器安全验证页，请关闭无头模式并手动完成验证")
			}
			if !challengeReported {
				log.Printf("[kami-login] waiting for manual browser verification account=%s", email)
				challengeReported = true
				if reportPhase != nil {
					reportPhase("waiting_challenge")
				}
			}
		}
		select {
		case <-cctx.Done():
			return cctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func waitVisibleAndType(ctx context.Context, selector, value string, timeout time.Duration, headless bool, email string, reportPhase func(string), resumePhase string) error {
	if err := waitVisible(ctx, selector, timeout, headless, email, reportPhase, resumePhase); err != nil {
		return err
	}
	return chromedp.Do(ctx,
		chromedp.Focus(chromedp.CSS(selector)),
		chromedp.SendKeys(chromedp.CSS(selector), value),
	)
}

func clickContinueButton(ctx context.Context) error {
	clicked, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
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
	if err != nil {
		return err
	}
	if !clicked {
		return errors.New("未找到可点击的继续按钮")
	}
	return nil
}

func loginPageState(ctx context.Context) string {
	state, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(() => JSON.stringify({
		title: (document.title || '').slice(0, 80),
		inputs: Array.from(document.querySelectorAll('input'))
			.filter(el => el.offsetParent !== null)
			.slice(0, 8)
			.map(el => ({type: el.type || '', name: el.name || '', autocomplete: el.autocomplete || '', placeholder: (el.placeholder || '').slice(0, 64)})),
		buttons: Array.from(document.querySelectorAll('button'))
			.filter(el => el.offsetParent !== null)
			.slice(0, 8)
			.map(el => (el.innerText || '').trim().slice(0, 64))
	}))()`))
	if err != nil {
		return "页面摘要不可用: " + err.Error()
	}
	if len(state) > 800 {
		state = state[:800]
	}
	return "页面摘要=" + state
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
	return p.loginKamiAccountsWithProgress(targets, headless, nil)
}

func (p *ChatGPTProvider) loginKamiAccountsWithProgress(targets []*accounts.Account, headless bool, onPhase KamiPhaseReporter) (map[string]KamiLoginOutcome, error) {
	defaultProxy := ""
	if p.cfg != nil {
		defaultProxy = p.cfg.ProxyURL
		if defaultProxy == "" {
			defaultProxy = p.cfg.HTTPProxy
		}
	}
	for _, acct := range targets {
		if acct == nil || acct.Proxy != "" {
			continue
		}
		if proxyURL := p.allocateProxy(); proxyURL != "" {
			if err := p.pool.RebindAccountProxy(acct, proxyURL); err != nil {
				log.Printf("[kami-login] proxy bind failed account=%s error=%v", acct.Email, err)
				if p.auditLog != nil {
					p.auditLog.RecordEvent("/admin/api/v1/providers/chatgpt/accounts/kami-login", acct.Email, "", "kami_login_failed", "proxy bind failed: "+err.Error())
				}
				return nil, fmt.Errorf("账号 %s 绑定代理失败: %w", acct.Email, err)
			}
		}
	}
	if p.auditLog != nil {
		for _, acct := range targets {
			if acct != nil {
				p.auditLog.RecordEvent("/admin/api/v1/providers/chatgpt/accounts/kami-login", acct.Email, "", "kami_login_started", "")
			}
		}
	}
	p.kamiLoginMu.Lock()
	executor := p.kamiLoginFn
	p.kamiLoginMu.Unlock()
	if executor == nil {
		executor = RunKamiPlaywrightLoginWithProgress
	}
	outcomes, err := executor(targets, defaultProxy, headless, onPhase)
	if err != nil {
		log.Printf("[kami-login] browser batch failed accounts=%d error=%v", len(targets), err)
		if p.auditLog != nil {
			for _, acct := range targets {
				if acct != nil {
					p.auditLog.RecordEvent("/admin/api/v1/providers/chatgpt/accounts/kami-login", acct.Email, "", "kami_login_failed", err.Error())
				}
			}
		}
		return nil, err
	}

	for _, acct := range targets {
		key := strings.ToLower(strings.TrimSpace(acct.Email))
		o, ok := outcomes[key]
		if !ok {
			continue
		}
		if p.auditLog != nil {
			action := "kami_login_failed"
			if o.OK {
				action = "kami_login_ok"
			}
			p.auditLog.RecordEvent("/admin/api/v1/providers/chatgpt/accounts/kami-login", acct.Email, acct.Proxy, action, o.Error)
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
			acct.CooldownUntil = time.Time{}
			acct.ImageCooldownUntil = time.Time{}
			acct.RenewFailures = 0
			acct.RenewCooldownUntil = time.Time{}
			acct.LastRenewError = ""
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

func cloneKamiLoginTask(task *KamiLoginTask) KamiLoginTask {
	snapshot := *task
	snapshot.Results = append([]KamiLoginTaskResult(nil), task.Results...)
	snapshot.QueuedAccountIDs = append([]string(nil), task.QueuedAccountIDs...)
	return snapshot
}

func (p *ChatGPTProvider) rebuildKamiQueueSnapshotLocked(task *KamiLoginTask) {
	queuedIDs := make([]string, 0, len(p.kamiLoginQueue))
	for idx, item := range p.kamiLoginQueue {
		if item.account == nil {
			continue
		}
		queuedIDs = append(queuedIDs, item.account.ID)
		p.pool.SetAccountRenewState(item.account.ID, "queued")
		p.pool.SetAccountActionState(item.account.ID, "login", "queued", KamiPhaseLabel("queued"), task.ID, idx+1, time.Time{})
	}
	task.QueuedAccountIDs = queuedIDs
	task.UpdatedAt = time.Now().Format(time.RFC3339)
}

func (p *ChatGPTProvider) validateKamiLoginTarget(id string) (*accounts.Account, *KamiLoginTaskResult) {
	acct := p.pool.GetByID(id)
	result := KamiLoginTaskResult{AccountID: id}
	if acct == nil {
		result.Error = "账号不存在"
		return nil, &result
	}
	result.AccountID = acct.ID
	result.Email = acct.Email
	switch {
	case acct.Email == "" || acct.Password == "" || acct.TOTPSecret == "":
		result.Error = "账号未配置完整卡密凭据"
	case acct.Status == accounts.StatusDisabled:
		result.Error = "账号已停用"
	case acct.ActiveAction == "renew" && acct.ActionPhase != "queued":
		result.Error = "该账号正在后台自动续期中"
	default:
		return acct, nil
	}
	return nil, &result
}

// StartKamiLoginBatch queues one serialized background login task (or appends to the running queue) and returns its snapshot.
func (p *ChatGPTProvider) StartKamiLoginBatch(ids []string, headless bool) (KamiLoginTask, error) {
	if len(ids) == 0 || len(ids) > 10000 {
		return KamiLoginTask{}, errors.New("请选择 1 到 10000 个账号")
	}

	p.kamiLoginMu.Lock()
	var auditFailures []KamiLoginTaskResult
	if p.kamiLoginRunning != "" {
		if existing := p.kamiLoginTasks[p.kamiLoginRunning]; existing != nil && (existing.Status == "queued" || existing.Status == "running") {
			alreadyInTask := make(map[string]struct{}, len(p.kamiLoginQueue)+1)
			if existing.CurrentAccountID != "" {
				alreadyInTask[existing.CurrentAccountID] = struct{}{}
			}
			for _, item := range p.kamiLoginQueue {
				if item.account != nil {
					alreadyInTask[item.account.ID] = struct{}{}
				}
			}
			seen := make(map[string]struct{}, len(ids))
			for _, rawID := range ids {
				id := strings.TrimSpace(rawID)
				if id == "" {
					continue
				}
				if _, ok := seen[id]; ok {
					continue
				}
				seen[id] = struct{}{}
				if _, dup := alreadyInTask[id]; dup {
					continue
				}
				acct, failRes := p.validateKamiLoginTarget(id)
				if failRes != nil {
					existing.Total++
					existing.Completed++
					existing.Failed++
					existing.Results = append(existing.Results, *failRes)
					auditFailures = append(auditFailures, *failRes)
					continue
				}
				if p.kamiWorker != nil {
					p.kamiWorker.CancelQueued(acct.ID)
				}
				if acct.RenewState == "cooldown" {
					p.pool.ResetRenewBackoff(acct)
				}
				alreadyInTask[acct.ID] = struct{}{}
				existing.Total++
				p.kamiLoginQueue = append(p.kamiLoginQueue, kamiLoginQueueItem{
					account:  acct,
					headless: headless,
				})
			}
			p.rebuildKamiQueueSnapshotLocked(existing)
			snapshot := cloneKamiLoginTask(existing)
			p.kamiLoginMu.Unlock()
			if p.auditLog != nil {
				for _, res := range auditFailures {
					p.auditLog.RecordEvent("/admin/api/v1/providers/chatgpt/accounts/kami-login", res.Email, "", "kami_login_failed", res.Error)
				}
			}
			return snapshot, nil
		}
	}

	now := time.Now().Format(time.RFC3339)
	task := &KamiLoginTask{
		ID:        uuid.NewString(),
		Status:    "queued",
		CreatedAt: now,
		UpdatedAt: now,
		Results:   make([]KamiLoginTaskResult, 0, len(ids)),
	}
	seen := make(map[string]struct{}, len(ids))
	targets := make([]*accounts.Account, 0, len(ids))
	for _, rawID := range ids {
		id := strings.TrimSpace(rawID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		task.Total++
		acct, failRes := p.validateKamiLoginTarget(id)
		if failRes != nil {
			task.Completed++
			task.Failed++
			task.Results = append(task.Results, *failRes)
			auditFailures = append(auditFailures, *failRes)
			continue
		}
		targets = append(targets, acct)
	}
	if task.Total == 0 {
		p.kamiLoginMu.Unlock()
		return KamiLoginTask{}, errors.New("请选择至少一个账号")
	}

	if len(targets) == 0 {
		task.Status = "completed"
	} else {
		p.kamiLoginQueue = p.kamiLoginQueue[:0]
		for _, acct := range targets {
			if p.kamiWorker != nil {
				p.kamiWorker.CancelQueued(acct.ID)
			}
			if acct.RenewState == "cooldown" {
				p.pool.ResetRenewBackoff(acct)
			}
			p.kamiLoginQueue = append(p.kamiLoginQueue, kamiLoginQueueItem{
				account:  acct,
				headless: headless,
			})
		}
		p.rebuildKamiQueueSnapshotLocked(task)
		p.kamiLoginRunning = task.ID
	}

	p.kamiLoginTasks[task.ID] = task
	p.kamiLoginTaskOrder = append(p.kamiLoginTaskOrder, task.ID)
	for len(p.kamiLoginTaskOrder) > 20 {
		oldest := p.kamiLoginTaskOrder[0]
		if oldest == p.kamiLoginRunning {
			break
		}
		p.kamiLoginTaskOrder = p.kamiLoginTaskOrder[1:]
		delete(p.kamiLoginTasks, oldest)
	}
	snapshot := cloneKamiLoginTask(task)
	p.kamiLoginMu.Unlock()

	if p.auditLog != nil {
		for _, result := range auditFailures {
			p.auditLog.RecordEvent("/admin/api/v1/providers/chatgpt/accounts/kami-login", result.Email, "", "kami_login_failed", result.Error)
		}
	}

	if len(targets) > 0 {
		go p.runKamiLoginTask(task.ID)
	}
	return snapshot, nil
}

// StartKamiPendingLogin starts an async login task for pending/expired card accounts.
func (p *ChatGPTProvider) StartKamiPendingLogin(force, headless bool) (KamiLoginTask, error) {
	candidates := p.pool.ListKamiCandidates(!force)
	ids := make([]string, 0, len(candidates))
	for _, acct := range candidates {
		ids = append(ids, acct.ID)
	}
	if len(ids) == 0 {
		return KamiLoginTask{}, errors.New("没有需要登录的卡密账号")
	}
	return p.StartKamiLoginBatch(ids, headless)
}

// KamiLoginTaskSnapshot returns a copied task state safe for concurrent polling.
func (p *ChatGPTProvider) KamiLoginTaskSnapshot(id string) (KamiLoginTask, bool) {
	p.kamiLoginMu.Lock()
	defer p.kamiLoginMu.Unlock()
	task := p.kamiLoginTasks[id]
	if task == nil {
		return KamiLoginTask{}, false
	}
	return cloneKamiLoginTask(task), true
}

// ActiveKamiLoginTask 返回当前正在运行的登录任务，或最近一个尚未被用户关闭的已完成任务。
func (p *ChatGPTProvider) ActiveKamiLoginTask() *KamiLoginTask {
	p.kamiLoginMu.Lock()
	defer p.kamiLoginMu.Unlock()
	if p.kamiLoginRunning != "" {
		if task := p.kamiLoginTasks[p.kamiLoginRunning]; task != nil {
			cp := cloneKamiLoginTask(task)
			return &cp
		}
	}
	for i := len(p.kamiLoginTaskOrder) - 1; i >= 0; i-- {
		task := p.kamiLoginTasks[p.kamiLoginTaskOrder[i]]
		if task != nil && !task.Dismissed {
			cp := cloneKamiLoginTask(task)
			return &cp
		}
	}
	return nil
}

// CancelKamiLogin 从正在运行的批量/单号登录队列中移除尚未开始的等待账号。
func (p *ChatGPTProvider) CancelKamiLogin(taskID, accountID string, cancelAll bool) (*KamiLoginTask, int, error) {
	p.kamiLoginMu.Lock()
	targetID := strings.TrimSpace(taskID)
	if targetID == "" {
		targetID = p.kamiLoginRunning
	}
	task := p.kamiLoginTasks[targetID]
	if task == nil || (task.Status != "queued" && task.Status != "running") {
		p.kamiLoginMu.Unlock()
		return nil, 0, errors.New("当前没有运行中的重登任务")
	}
	accountID = strings.TrimSpace(accountID)
	if !cancelAll && accountID == "" {
		p.kamiLoginMu.Unlock()
		return nil, 0, errors.New("请指定要取消排队的账号或选择取消全部排队")
	}
	if !cancelAll && task.CurrentAccountID == accountID {
		p.kamiLoginMu.Unlock()
		return nil, 0, errors.New("该账号正在执行登录中，无法取消")
	}

	remaining := make([]kamiLoginQueueItem, 0, len(p.kamiLoginQueue))
	cancelled := make([]KamiLoginTaskResult, 0)
	for _, item := range p.kamiLoginQueue {
		if item.account == nil {
			continue
		}
		if cancelAll || item.account.ID == accountID {
			cancelled = append(cancelled, KamiLoginTaskResult{
				AccountID: item.account.ID,
				Email:     item.account.Email,
				OK:        false,
				Cancelled: true,
				Error:     "已取消排队",
			})
			continue
		}
		remaining = append(remaining, item)
	}
	if len(cancelled) == 0 {
		p.kamiLoginMu.Unlock()
		if cancelAll {
			return nil, 0, errors.New("当前队列中没有等待执行的账号")
		}
		return nil, 0, errors.New("该账号不在等待队列中")
	}

	p.kamiLoginQueue = remaining
	for _, res := range cancelled {
		task.Results = append(task.Results, res)
		task.Completed++
		task.Cancelled++
		p.pool.ClearAccountQueuedAction(res.AccountID, "已取消重登排队")
		p.pool.SetAccountRenewState(res.AccountID, "idle")
	}
	p.rebuildKamiQueueSnapshotLocked(task)
	snapshot := cloneKamiLoginTask(task)
	p.kamiLoginMu.Unlock()

	if p.auditLog != nil {
		for _, res := range cancelled {
			p.auditLog.RecordEvent("/admin/api/v1/providers/chatgpt/accounts/kami-login/cancel", res.Email, "", "kami_login_cancelled", res.Error)
		}
	}
	return &snapshot, len(cancelled), nil
}

// DismissKamiLoginTask 将已完成的重登任务横幅标记为已关闭（刷新页面后不再提示）。
func (p *ChatGPTProvider) DismissKamiLoginTask(taskID string) bool {
	p.kamiLoginMu.Lock()
	defer p.kamiLoginMu.Unlock()
	taskID = strings.TrimSpace(taskID)
	dismissed := false
	if taskID != "" {
		if task := p.kamiLoginTasks[taskID]; task != nil && task.Status == "completed" {
			task.Dismissed = true
			task.UpdatedAt = time.Now().Format(time.RFC3339)
			dismissed = true
		}
		return dismissed
	}
	for _, task := range p.kamiLoginTasks {
		if task != nil && task.Status == "completed" && !task.Dismissed {
			task.Dismissed = true
			task.UpdatedAt = time.Now().Format(time.RFC3339)
			dismissed = true
		}
	}
	return dismissed
}

func (p *ChatGPTProvider) kamiCooldownDuration() time.Duration {
	p.kamiLoginMu.Lock()
	d := p.kamiLoginCooldown
	p.kamiLoginMu.Unlock()
	if d > 0 {
		return d
	}
	return 10 * time.Second
}

func (p *ChatGPTProvider) runKamiLoginTask(taskID string) {
	p.updateKamiLoginTask(taskID, func(task *KamiLoginTask) {
		task.Status = "running"
	})
	probedProxiesOnce := false
	ranBrowser := false
	cooldown := p.kamiCooldownDuration()

	recordFailure := func(acct *accounts.Account, result KamiLoginTaskResult) {
		log.Printf("[kami-login] failed account=%s error=%s", result.Email, result.Error)
		if p.auditLog != nil {
			p.auditLog.RecordEvent("/admin/api/v1/providers/chatgpt/accounts/kami-login", result.Email, "", "kami_login_failed", result.Error)
		}
		p.pool.FinishAccountAction(acct.ID, "login", false, "重登失败: "+summarizeActionError(result.Error))
		p.pool.SetAccountRenewState(acct.ID, "idle")
		p.kamiLoginMu.Lock()
		if task := p.kamiLoginTasks[taskID]; task != nil {
			task.Results = append(task.Results, result)
			task.Completed++
			task.Failed++
			task.CurrentAccountID = ""
			task.CurrentEmail = ""
			task.CurrentPhase = ""
			task.CurrentPhaseLabel = ""
			task.CurrentStartedAt = ""
			task.UpdatedAt = time.Now().Format(time.RFC3339)
		}
		p.kamiLoginMu.Unlock()
	}

	for {
		p.kamiLoginMu.Lock()
		task := p.kamiLoginTasks[taskID]
		if task == nil || len(p.kamiLoginQueue) == 0 {
			if task != nil {
				task.Status = "completed"
				task.CurrentAccountID = ""
				task.CurrentEmail = ""
				task.CurrentPhase = ""
				task.CurrentPhaseLabel = ""
				task.CurrentStartedAt = ""
				task.QueuedAccountIDs = nil
				task.UpdatedAt = time.Now().Format(time.RFC3339)
			}
			if p.kamiLoginRunning == taskID {
				p.kamiLoginRunning = ""
			}
			p.kamiLoginMu.Unlock()
			return
		}

		if ranBrowser {
			task.CurrentAccountID = ""
			task.CurrentEmail = ""
			task.CurrentPhase = "cooldown_wait"
			task.CurrentPhaseLabel = KamiPhaseLabel("cooldown_wait")
			task.CurrentStartedAt = time.Now().Format(time.RFC3339)
			task.UpdatedAt = time.Now().Format(time.RFC3339)
			p.kamiLoginMu.Unlock()

			deadline := time.Now().Add(cooldown)
			for time.Now().Before(deadline) {
				step := time.Until(deadline)
				if step > 100*time.Millisecond {
					step = 100 * time.Millisecond
				}
				if step > 0 {
					time.Sleep(step)
				}
				p.kamiLoginMu.Lock()
				empty := len(p.kamiLoginQueue) == 0
				p.kamiLoginMu.Unlock()
				if empty {
					break
				}
			}

			p.kamiLoginMu.Lock()
			task = p.kamiLoginTasks[taskID]
			if task == nil || len(p.kamiLoginQueue) == 0 {
				if task != nil {
					task.Status = "completed"
					task.CurrentAccountID = ""
					task.CurrentEmail = ""
					task.CurrentPhase = ""
					task.CurrentPhaseLabel = ""
					task.CurrentStartedAt = ""
					task.QueuedAccountIDs = nil
					task.UpdatedAt = time.Now().Format(time.RFC3339)
				}
				if p.kamiLoginRunning == taskID {
					p.kamiLoginRunning = ""
				}
				p.kamiLoginMu.Unlock()
				return
			}
		}

		item := p.kamiLoginQueue[0]
		p.kamiLoginQueue = p.kamiLoginQueue[1:]
		acct := item.account
		startedAt := time.Now()
		task.CurrentAccountID = acct.ID
		task.CurrentEmail = acct.Email
		task.CurrentPhase = "checking_proxy"
		task.CurrentPhaseLabel = KamiPhaseLabel("checking_proxy")
		task.CurrentStartedAt = startedAt.Format(time.RFC3339)
		p.rebuildKamiQueueSnapshotLocked(task)
		p.pool.SetAccountRenewState(acct.ID, "running")
		p.pool.SetAccountActionState(acct.ID, "login", "checking_proxy", KamiPhaseLabel("checking_proxy"), taskID, 0, startedAt)
		p.kamiLoginMu.Unlock()

		if p.proxyPool != nil && p.proxyPool.Count() > 0 {
			if !probedProxiesOnce {
				probedProxiesOnce = true
				probeCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
				p.proxyPool.ProbeAll(probeCtx, nil, p.pool.BoundProxyCounts())
				cancel()
				p.pool.ReconcileProxies(
					func(u string) bool { return !p.proxyPool.IsHealthy(u) },
					func(exclude map[string]bool) string { return p.proxyPool.AllocateHealthy(exclude) },
				)
			}
			if acct.Proxy == "" {
				proxyURL := p.proxyPool.AllocateHealthy(nil)
				if proxyURL == "" || !p.proxyPool.IsHealthy(proxyURL) {
					recordFailure(acct, KamiLoginTaskResult{AccountID: acct.ID, Email: acct.Email, Error: "代理预检失败：没有健康代理节点，登录未启动"})
					continue
				}
				if err := p.pool.RebindAccountProxy(acct, proxyURL); err != nil {
					recordFailure(acct, KamiLoginTaskResult{AccountID: acct.ID, Email: acct.Email, Error: "绑定代理失败: " + err.Error()})
					continue
				}
			}
			if !p.proxyPool.IsHealthy(acct.Proxy) {
				recordFailure(acct, KamiLoginTaskResult{AccountID: acct.ID, Email: acct.Email, Error: "账号绑定代理未通过连通性预检，登录未启动；请更换健康代理"})
				continue
			}
		}

		onPhase := func(accountID, phase, label string) {
			if label == "" {
				label = KamiPhaseLabel(phase)
			}
			p.pool.SetAccountActionState(accountID, "login", phase, label, taskID, 0, startedAt)
			p.updateKamiLoginTask(taskID, func(t *KamiLoginTask) {
				if t.CurrentAccountID == accountID {
					t.CurrentPhase = phase
					t.CurrentPhaseLabel = label
				}
			})
		}

		onPhase(acct.ID, "launch_browser", KamiPhaseLabel("launch_browser"))
		ranBrowser = true
		outcomes, err := p.loginKamiAccountsWithProgress([]*accounts.Account{acct}, item.headless, onPhase)
		result := KamiLoginTaskResult{AccountID: acct.ID, Email: acct.Email}
		if err != nil {
			result.Error = err.Error()
		} else if outcome, ok := outcomes[strings.ToLower(strings.TrimSpace(acct.Email))]; !ok {
			result.Error = "浏览器登录未返回账号结果"
		} else {
			result.OK = outcome.OK
			result.Error = outcome.Error
		}
		if result.OK {
			if err := p.PersistAccessTokens(); err != nil {
				log.Printf("[kami-login] persist access_tokens failed: %v", err)
			}
			p.pool.FinishAccountAction(acct.ID, "login", true, "重登成功，Token 已更新")
		} else {
			errSummary := summarizeActionError(result.Error)
			if errSummary == "" {
				errSummary = "未获取到有效 Token"
			}
			p.pool.FinishAccountAction(acct.ID, "login", false, "重登失败: "+errSummary)
		}
		p.pool.SetAccountRenewState(acct.ID, "idle")

		p.kamiLoginMu.Lock()
		if task := p.kamiLoginTasks[taskID]; task != nil {
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
		p.kamiLoginMu.Unlock()
	}
}

func (p *ChatGPTProvider) updateKamiLoginTask(taskID string, update func(*KamiLoginTask)) {
	p.kamiLoginMu.Lock()
	defer p.kamiLoginMu.Unlock()
	if task := p.kamiLoginTasks[taskID]; task != nil {
		update(task)
		task.UpdatedAt = time.Now().Format(time.RFC3339)
	}
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
