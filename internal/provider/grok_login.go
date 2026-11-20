package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"ai-web-free/internal/accounts"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/chromedp"
)

const grokSignInURL = "https://accounts.x.ai/sign-in?redirect=grok-com&email=true"

var (
	grokTOTPPattern   = regexp.MustCompile(`^[A-Z2-7]+=*$`)
	grokCookiePattern = regexp.MustCompile(`(?:^|;\s*)(?:sso|sso-rw)\s*=`)
	grokJWTPattern    = regexp.MustCompile(`^eyJ[A-Za-z0-9+/=_-]+\.[A-Za-z0-9+/=_-]+\.[A-Za-z0-9+/=_-]+$`)
)

// GrokCardAccount describes one email/password/TOTP card line.
type GrokCardAccount struct {
	Email       string
	Password    string
	TOTPSecret  string
	LegacyToken string
	OldCookie   string
	Proxy       string
	LineNo      int
}

type GrokLoginOptions struct {
	ProxyURL    string
	BrowserPath string
	Headless    bool
	Timeout     time.Duration
	OnPhase     KamiPhaseReporter
}

type GrokLoginResult struct {
	Email  string
	OK     bool
	Cookie string
	Error  string
}

func ParseGrokCardLine(line string, lineNo int) (GrokCardAccount, error) {
	var account GrokCardAccount
	parts := splitGrokCardLine(line)
	if len(parts) < 3 {
		return account, fmt.Errorf("line %d: expected email|password|totp_secret", lineNo)
	}

	account.Email = parts[0]
	account.Password = parts[1]
	account.LineNo = lineNo
	third := parts[2]
	switch {
	case grokCookiePattern.MatchString(third):
		account.OldCookie = third
	case isGrokLegacyToken(third):
		account.LegacyToken = third
		if account.OldCookie == "" {
			account.OldCookie = fmt.Sprintf("sso=%s; sso-rw=%s", third, third)
		}
	default:
		account.TOTPSecret = third
	}

	for _, extra := range parts[3:] {
		switch {
		case grokCookiePattern.MatchString(extra):
			account.OldCookie = extra
		case isGrokLegacyToken(extra):
			if account.LegacyToken != "" {
				return GrokCardAccount{}, fmt.Errorf("line %d: duplicate legacy token", lineNo)
			}
			account.LegacyToken = extra
		case regexp.MustCompile(`(?i)^(https?|socks[45])://`).MatchString(extra):
			account.Proxy = extra
		default:
			return GrokCardAccount{}, fmt.Errorf("line %d: unrecognized fourth field", lineNo)
		}
	}

	if account.Email == "" || !strings.Contains(account.Email, "@") || account.Password == "" {
		return GrokCardAccount{}, fmt.Errorf("line %d: invalid email or password", lineNo)
	}
	if account.TOTPSecret == "" && account.OldCookie == "" && account.LegacyToken == "" {
		return GrokCardAccount{}, fmt.Errorf("line %d: missing TOTP secret, legacy token, or old SSO cookie", lineNo)
	}
	if account.TOTPSecret != "" {
		normalized := strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(account.TOTPSecret))
		if len(normalized) < 16 || !grokTOTPPattern.MatchString(normalized) {
			return GrokCardAccount{}, fmt.Errorf("line %d: invalid TOTP secret", lineNo)
		}
	}
	return account, nil
}

func isGrokLegacyToken(value string) bool {
	return grokJWTPattern.MatchString(strings.TrimSpace(value))
}

func splitGrokCardLine(line string) []string {
	var parts []string
	switch {
	case strings.Contains(line, "|"):
		parts = strings.Split(line, "|")
	case strings.Contains(line, "----"):
		parts = strings.Split(line, "----")
	default:
		parts = strings.Fields(line)
	}
	for index := range parts {
		parts[index] = strings.TrimSpace(parts[index])
	}
	return parts
}

// DetectGrokCredentialLine classifies one Grok credential line as kami or cookie.
// It accepts mixed batches: pipe cards, four-dash cards, legacy email----SSO lines,
// and standalone SSO header strings can coexist in the same import.
func DetectGrokCredentialLine(line string) (string, GrokCardAccount, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", GrokCardAccount{}, errors.New("empty Grok credential line")
	}
	if card, err := ParseGrokCardLine(trimmed, 1); err == nil {
		return "kami", card, nil
	}
	if grokCookiePattern.MatchString(trimmed) || isGrokLegacyToken(trimmed) {
		return "cookie", GrokCardAccount{}, nil
	}
	if parts := strings.SplitN(trimmed, "----", 2); len(parts) == 2 {
		email := strings.TrimSpace(parts[0])
		cookie := strings.TrimSpace(parts[1])
		if email != "" && strings.Contains(email, "@") && (grokCookiePattern.MatchString(cookie) || isGrokLegacyToken(cookie)) {
			return "cookie", GrokCardAccount{}, nil
		}
	}
	return "", GrokCardAccount{}, errors.New("unrecognized Grok credential format")
}

func ParseGrokCardFile(path string) ([]GrokCardAccount, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := strings.TrimPrefix(string(data), "\ufeff")

	var accounts []GrokCardAccount
	for index, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "=") || strings.HasPrefix(line, "-") {
			continue
		}
		// Skip non-credential instruction lines that do not look like cards or cookies
		if !strings.Contains(line, "@") && !grokCookiePattern.MatchString(line) {
			continue
		}
		account, parseErr := ParseGrokCardLine(line, index+1)
		if parseErr != nil {
			return nil, parseErr
		}
		accounts = append(accounts, account)
	}
	return accounts, nil
}

func FilterGrokCardAccounts(accounts []GrokCardAccount, target string, limit int) []GrokCardAccount {
	selected := make([]GrokCardAccount, 0, len(accounts))
	target = strings.ToLower(strings.TrimSpace(target))
	for _, account := range accounts {
		if target != "" && !strings.Contains(strings.ToLower(account.Email), target) {
			continue
		}
		selected = append(selected, account)
		if limit > 0 && len(selected) == limit {
			break
		}
	}
	return selected
}

func RunGrokCardLogin(ctx context.Context, cards []GrokCardAccount, options GrokLoginOptions) ([]GrokLoginResult, error) {
	if options.Timeout < 30*time.Second {
		options.Timeout = 180 * time.Second
	}
	if options.BrowserPath == "" {
		browserPath, err := resolveChromeExecutable()
		if err != nil {
			return nil, err
		}
		options.BrowserPath = browserPath
	}

	results := make([]GrokLoginResult, 0, len(cards))
	for _, card := range cards {
		cookie, err := loginGrokCard(ctx, card, options)
		result := GrokLoginResult{Email: card.Email}
		if err != nil {
			result.Error = err.Error()
		} else {
			result.OK = true
			result.Cookie = cookie
		}
		results = append(results, result)
	}
	return results, nil
}

func loginGrokCard(ctx context.Context, card GrokCardAccount, options GrokLoginOptions) (string, error) {
	if options.Headless {
		return "", errors.New("Grok 网页端登录受 Cloudflare Turnstile 防火墙拦截，无头模式已禁用。请使用配套提取工具获取 SSO Cookie 后直接导入，或关闭无头模式手动登录")
	}

	proxyURL := card.Proxy
	if proxyURL == "" {
		proxyURL = options.ProxyURL
	}

	var defaultUA string
	switch runtime.GOOS {
	case "linux":
		defaultUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"
	case "darwin":
		defaultUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"
	default:
		defaultUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"
	}

	allocatorOptions := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(options.BrowserPath),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("incognito", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("enable-automation", false),
		chromedp.Flag("excludeSwitches", "enable-automation"),
		chromedp.Flag("user-agent", defaultUA),
		chromedp.Flag("lang", "zh-CN,zh,en-US,en"),
		chromedp.WindowSize(1280, 800),
	)
	if runtime.GOOS == "linux" {
		allocatorOptions = append(allocatorOptions,
			chromedp.Flag("no-sandbox", true),
			chromedp.Flag("disable-dev-shm-usage", true),
		)
	}
	if options.Headless {
		allocatorOptions = append(allocatorOptions, chromedp.Flag("headless", "new"))
	} else {
		allocatorOptions = append(allocatorOptions,
			chromedp.VisibleWindow,
			chromedp.Flag("new-window", true),
			chromedp.Flag("start-maximized", true),
		)
	}
	if proxyURL != "" {
		allocatorOptions = append(allocatorOptions, chromedp.ProxyServer(proxyURL))
	}

	allocatorContext, allocatorCancel := chromedp.NewExecAllocator(context.Background(), allocatorOptions...)
	defer allocatorCancel()

	browserContext, browserCancel := chromedp.NewContext(allocatorContext)
	defer browserCancel()

	loginContext, loginCancel := context.WithTimeout(browserContext, options.Timeout)
	defer loginCancel()

	if _, err := chromedp.Call(loginContext, network.Enable, network.EnableParams{}); err != nil {
		return "", fmt.Errorf("enable network events: %w", err)
	}
	_, _ = chromedp.Call(loginContext, page.AddScriptToEvaluateOnNewDocument, page.AddScriptToEvaluateOnNewDocumentParams{
		Source: `if (!navigator.languages || navigator.languages.length === 0) {` +
			`Object.defineProperty(navigator, "languages", {get: () => ["zh-CN", "zh", "en-US", "en"]});}`,
	})

	reportPhase := func(phase string) {
		if options.OnPhase != nil {
			options.OnPhase(card.Email, phase, KamiPhaseLabel(phase))
		}
	}

	// 轮询检查是否已在任意页面截获有效 SSO Cookie
	pollCookie := func() string {
		return extractGrokSSOCookie(loginContext)
	}

	reportPhase("open_login")
	if err := chromedp.Do(loginContext, chromedp.Navigate(grokSignInURL)); err != nil {
		return "", fmt.Errorf("open sign-in page: %w", err)
	}
	_, _ = chromedp.Call(loginContext, page.BringToFront, cdp.Empty{})
	time.Sleep(500 * time.Millisecond)
	dismissGrokCookieBanner(loginContext)

	if cookie := pollCookie(); cookie != "" {
		return cookie, nil
	}

	reportPhase("enter_email")
	emailSelector := `input[name="email"], input#email, input[type="email"]`
	if err := waitGrokVisible(loginContext, emailSelector, 30*time.Second); err != nil {
		if cookie := pollCookie(); cookie != "" {
			return cookie, nil
		}
		return "", fmt.Errorf("email input not found: %w", err)
	}
	if err := chromedp.Do(loginContext,
		chromedp.Focus(chromedp.CSS(emailSelector)),
		chromedp.SendKeys(chromedp.CSS(emailSelector), card.Email),
	); err != nil {
		return "", fmt.Errorf("fill email: %w", err)
	}
	time.Sleep(300 * time.Millisecond)
	if err := clickGrokSubmit(loginContext); err != nil {
		if cookie := pollCookie(); cookie != "" {
			return cookie, nil
		}
		return "", fmt.Errorf("submit email: %w", err)
	}

	if cookie := pollCookie(); cookie != "" {
		return cookie, nil
	}

	reportPhase("enter_password")
	passwordSelector := `input[name="password"], input[type="password"]`
	if err := waitGrokVisible(loginContext, passwordSelector, 30*time.Second); err != nil {
		if cookie := pollCookie(); cookie != "" {
			return cookie, nil
		}
		return "", fmt.Errorf("password input not found: %w", err)
	}
	dismissGrokCookieBanner(loginContext)
	if err := chromedp.Do(loginContext,
		chromedp.Focus(chromedp.CSS(passwordSelector)),
		chromedp.SendKeys(chromedp.CSS(passwordSelector), card.Password),
	); err != nil {
		return "", fmt.Errorf("fill password: %w", err)
	}

	// 等待 Turnstile 人机验证通过或捕获 SSO Cookie 的统一循环
	waitForTurnstileOrCookie := func(maxWait time.Duration) (bool, error) {
		deadline := time.Now().Add(maxWait)
		for time.Now().Before(deadline) {
			select {
			case <-loginContext.Done():
				return false, loginContext.Err()
			case <-time.After(500 * time.Millisecond):
			}
			if cookie := pollCookie(); cookie != "" {
				return true, nil
			}
			present, curToken := checkGrokTurnstileState(loginContext)
			if !present || curToken != "" {
				return true, nil
			}
		}
		return false, nil
	}

	// 探测 Cloudflare Turnstile 人机验证（密码输入后）
	turnstilePresent, token := checkGrokTurnstileState(loginContext)
	if turnstilePresent && token == "" {
		_, _ = chromedp.Call(loginContext, page.BringToFront, cdp.Empty{})
		reportPhase("waiting_challenge")
		waitMax := 15 * time.Second
		if !options.Headless {
			waitMax = 120 * time.Second
		}
		resolved, err := waitForTurnstileOrCookie(waitMax)
		if err != nil {
			return "", err
		}
		if cookie := pollCookie(); cookie != "" {
			return cookie, nil
		}
		if !resolved {
			if options.Headless {
				return "", errors.New("检测到 Cloudflare 人机安全验证 (Turnstile)，在无头模式下被拦截，请关闭无头模式并手动完成验证")
			}
			return "", errors.New("等待 Cloudflare 人机安全验证超时，请在浏览器中完成验证后重试")
		}
		reportPhase("enter_password")
	}

	dismissGrokCookieBanner(loginContext)
	_ = clickGrokSubmit(loginContext)

	time.Sleep(1 * time.Second)
	if cookie := pollCookie(); cookie != "" {
		return cookie, nil
	}

	// 若点击提交后再次触发 Turnstile 人机验证
	turnstilePresent, token = checkGrokTurnstileState(loginContext)
	if turnstilePresent && token == "" {
		_, _ = chromedp.Call(loginContext, page.BringToFront, cdp.Empty{})
		reportPhase("waiting_challenge")
		waitMax := 15 * time.Second
		if !options.Headless {
			waitMax = 120 * time.Second
		}
		resolved, err := waitForTurnstileOrCookie(waitMax)
		if err != nil {
			return "", err
		}
		if cookie := pollCookie(); cookie != "" {
			return cookie, nil
		}
		if resolved {
			_ = clickGrokSubmit(loginContext)
			time.Sleep(1 * time.Second)
		}
	}

	if postErr := checkGrokPostSubmitError(loginContext); postErr != nil {
		if cookie := pollCookie(); cookie != "" {
			return cookie, nil
		}
		if !options.Headless && strings.Contains(postErr.Error(), "Turnstile") {
			reportPhase("waiting_challenge")
			_, _ = waitForTurnstileOrCookie(60 * time.Second)
			if cookie := pollCookie(); cookie != "" {
				return cookie, nil
			}
		} else {
			return "", postErr
		}
	}

	otpSelector := `input[inputmode="numeric"], input[name="code"], input[name="otp"], input[autocomplete="one-time-code"], input[placeholder*="code" i], input[placeholder*="验证码" i]`
	if err := waitGrokVisible(loginContext, otpSelector, 10*time.Second); err == nil {
		if cookie := pollCookie(); cookie != "" {
			return cookie, nil
		}
		reportPhase("enter_totp")
		if card.TOTPSecret == "" {
			if !options.Headless {
				deadline := time.Now().Add(60 * time.Second)
				for time.Now().Before(deadline) {
					if cookie := pollCookie(); cookie != "" {
						return cookie, nil
					}
					time.Sleep(1 * time.Second)
				}
			}
			return "", errors.New("该账号需要 2FA 验证码，但卡密中未配置 TOTP Secret")
		}
		code, _, totpErr := accounts.GenerateTOTP(card.TOTPSecret, time.Now())
		if totpErr != nil {
			return "", fmt.Errorf("generate TOTP: %w", totpErr)
		}
		if err := chromedp.Do(loginContext,
			chromedp.Focus(chromedp.CSS(otpSelector)),
			chromedp.SendKeys(chromedp.CSS(otpSelector), code),
		); err != nil {
			return "", fmt.Errorf("fill OTP: %w", err)
		}
		if err := clickGrokSubmit(loginContext); err != nil {
			if cookie := pollCookie(); cookie != "" {
				return cookie, nil
			}
			return "", fmt.Errorf("submit OTP: %w", err)
		}
	}

	reportPhase("wait_session_token")
	deadline := time.Now().Add(options.Timeout)
	for time.Now().Before(deadline) {
		cookie := pollCookie()
		if cookie != "" {
			return cookie, nil
		}
		select {
		case <-loginContext.Done():
			return "", loginContext.Err()
		case <-time.After(time.Second):
		}
	}
	return "", fmt.Errorf("SSO cookie not found within %s", options.Timeout)
}

func waitGrokVisible(ctx context.Context, selector string, timeout time.Duration) error {
	waitContext, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	encodedSelector, _ := json.Marshal(selector)
	expression := `(() => Array.from(document.querySelectorAll(` + string(encodedSelector) + `)).some(element => element.offsetParent !== null))()`
	for {
		visible, err := chromedp.Run(waitContext, chromedp.Evaluate[bool](expression))
		if err != nil {
			return err
		}
		if visible {
			return nil
		}
		select {
		case <-waitContext.Done():
			return waitContext.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func clickGrokSubmit(ctx context.Context) error {
	clicked, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
		const submit = document.querySelector('button[type="submit"], input[type="submit"]');
		if (submit && submit.offsetParent !== null && !submit.disabled) { submit.click(); return true; }
		const buttons = Array.from(document.querySelectorAll("button, input[type=\"button\"]"));
		const match = buttons.find(button => button.offsetParent !== null && !button.disabled && /^(next|continue|verify|submit|log in|sign in|下一步|继续|验证|登录)$/i.test((button.innerText || button.value || "").trim()));
		if (match) { match.click(); return true; }
		if (submit && submit.offsetParent !== null) { submit.click(); return true; }
		return false;
	})()`))
	if err != nil {
		return err
	}
	if !clicked {
		return errors.New("submit button not found")
	}
	return nil
}

func dismissGrokCookieBanner(ctx context.Context) {
	_, _ = chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
		const selectors = [
			"#onetrust-accept-btn-handler",
			".onetrust-close-btn-handler",
			"#onetrust-policy-close",
			"#onetrust-close-btn-container button",
			'button[aria-label="Close"]',
			'button[aria-label="关闭"]'
		];
		for (const selector of selectors) {
			const element = document.querySelector(selector);
			if (element && element.offsetParent !== null) { element.click(); return true; }
		}
		return false;
	})()`))
}

func checkGrokTurnstileState(ctx context.Context) (present bool, token string) {
	res, err := chromedp.Run(ctx, chromedp.Evaluate[map[string]string](`(() => {
		const input = document.querySelector('input[name="cf-turnstile-response"]');
		const hasIframe = Boolean(document.querySelector('iframe[src*="challenges.cloudflare.com"]'));
		const token = input ? (input.value || '').trim() : '';
		return {
			"present": (input || hasIframe) ? "true" : "false",
			"token": token
		};
	})()`))
	if err != nil {
		return false, ""
	}
	return res["present"] == "true", res["token"]
}

func checkGrokPostSubmitError(ctx context.Context) error {
	errKey, _ := chromedp.Run(ctx, chromedp.Evaluate[string](`(() => {
		const text = (document.body.innerText || '');
		if (text.includes('Complete the verification challenge')) return 'turnstile_incomplete';
		if (/invalid (email or )?password|incorrect password|密码错误/i.test(text)) return 'invalid_credentials';
		if (/account( has been)? (deactivated|suspended)|permanently banned|已被(删除|停用|封禁)/i.test(text)) return 'account_banned';
		return '';
	})()`))
	switch errKey {
	case "turnstile_incomplete":
		return errors.New("Cloudflare 人机安全验证未通过，请重试")
	case "invalid_credentials":
		return errors.New("邮箱或密码错误")
	case "account_banned":
		return errors.New("account_deactivated: 该账号已被删除或封禁")
	}
	return nil
}

func extractGrokSSOCookie(ctx context.Context) string {
	values := make(map[string]string)

	// 1. 全局存储检索（不受特定 URL 路径限制）
	if storageRes, err := chromedp.Call(ctx, storage.GetCookies, storage.GetCookiesParams{}); err == nil {
		for _, cookie := range storageRes.Cookies {
			name := strings.ToLower(cookie.Name)
			if (name == "sso" || name == "sso-rw") && cookie.Value != "" && grokCookieDomainAllowed(cookie.Domain) {
				values[name] = cookie.Value
			}
		}
		if len(values) > 0 {
			return FormatGrokSSOCookie(values)
		}
	}

	// 2. 显式 URL 域检索兜底
	response, err := chromedp.Call(ctx, network.GetCookies, network.GetCookiesParams{
		URLs: []string{"https://grok.com", "https://x.ai", "https://accounts.x.ai", "https://x.com"},
	})
	if err != nil {
		return ""
	}

	for _, cookie := range response.Cookies {
		name := strings.ToLower(cookie.Name)
		if (name == "sso" || name == "sso-rw") && cookie.Value != "" && grokCookieDomainAllowed(cookie.Domain) {
			values[name] = cookie.Value
		}
	}
	return FormatGrokSSOCookie(values)
}

func grokCookieDomainAllowed(domain string) bool {
	domain = strings.ToLower(strings.TrimPrefix(domain, "."))
	return domain == "grok.com" || strings.HasSuffix(domain, ".grok.com") || domain == "x.ai" || strings.HasSuffix(domain, ".x.ai")
}

func FormatGrokSSOCookie(values map[string]string) string {
	var pieces []string
	for _, name := range []string{"sso", "sso-rw"} {
		if value := strings.TrimSpace(values[name]); value != "" {
			pieces = append(pieces, name+"="+value)
		}
	}
	return strings.Join(pieces, "; ")
}

func WriteGrokCookieOutput(path string, results []GrokLoginResult, merge bool) (int, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return 0, err
	}

	outputByEmail := make(map[string]string)
	if merge {
		data, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return 0, err
		}
		for _, rawLine := range strings.Split(string(data), "\n") {
			line := strings.TrimSpace(rawLine)
			if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "----") {
				continue
			}
			email, _, _ := strings.Cut(line, "----")
			outputByEmail[strings.ToLower(strings.TrimSpace(email))] = line
		}
	}

	var ordered []string
	seen := make(map[string]bool)
	for _, result := range results {
		if !result.OK || result.Cookie == "" {
			continue
		}
		line := result.Email + "----" + result.Cookie
		key := strings.ToLower(result.Email)
		outputByEmail[key] = line
		ordered = append(ordered, line)
		seen[key] = true
	}
	for key, line := range outputByEmail {
		if !seen[key] {
			ordered = append(ordered, line)
			seen[key] = true
		}
	}

	content := strings.Join(ordered, "\n")
	if content != "" {
		content += "\n"
	}
	temporaryPath := path + ".tmp"
	if err := os.WriteFile(temporaryPath, []byte(content), 0600); err != nil {
		return 0, err
	}
	if err := os.Chmod(temporaryPath, 0600); err != nil {
		_ = os.Remove(temporaryPath)
		return 0, err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		_ = os.Remove(temporaryPath)
		return 0, err
	}
	return len(ordered), nil
}
