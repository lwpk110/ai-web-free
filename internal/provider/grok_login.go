package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"ai-web-free/internal/accounts"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

const grokSignInURL = "https://accounts.x.ai/sign-in?redirect=grok-com&email=true"

var (
	grokTOTPPattern   = regexp.MustCompile(`^[A-Z2-7]+=*$`)
	grokCookiePattern = regexp.MustCompile(`(?:^|;\s*)(?:sso|sso-rw)\s*=`)
	grokJWTPattern    = regexp.MustCompile(`^eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$`)
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
	if grokCookiePattern.MatchString(trimmed) {
		return "cookie", GrokCardAccount{}, nil
	}
	if parts := strings.SplitN(trimmed, "----", 2); len(parts) == 2 {
		email := strings.TrimSpace(parts[0])
		cookie := strings.TrimSpace(parts[1])
		if email != "" && strings.Contains(email, "@") && grokCookiePattern.MatchString(cookie) {
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
		if line == "" || strings.HasPrefix(line, "#") {
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
	proxyURL := card.Proxy
	if proxyURL == "" {
		proxyURL = options.ProxyURL
	}

	allocatorOptions := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(options.BrowserPath),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("incognito", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"),
		chromedp.Flag("lang", "zh-CN,zh,en-US,en"),
		chromedp.WindowSize(1280, 800),
	)
	if proxyURL != "" {
		allocatorOptions = append(allocatorOptions, chromedp.ProxyServer(proxyURL))
	}
	if options.Headless {
		allocatorOptions = append(allocatorOptions, chromedp.Flag("headless", "new"))
	} else {
		allocatorOptions = append(allocatorOptions, chromedp.Flag("headless", false))
	}

	allocatorContext, allocatorCancel := chromedp.NewExecAllocator(ctx, allocatorOptions...)
	defer allocatorCancel()

	browserContext, browserCancel := chromedp.NewContext(allocatorContext)
	defer browserCancel()

	loginContext, loginCancel := context.WithTimeout(browserContext, options.Timeout)
	defer loginCancel()

	if _, err := chromedp.Call(loginContext, network.Enable, network.EnableParams{}); err != nil {
		return "", fmt.Errorf("enable network events: %w", err)
	}
	_, _ = chromedp.Call(loginContext, page.AddScriptToEvaluateOnNewDocument, page.AddScriptToEvaluateOnNewDocumentParams{
		Source: `Object.defineProperty(navigator, "webdriver", {get: () => undefined});` +
			`window.chrome = window.chrome || {runtime: {}};` +
			`if (!navigator.languages || navigator.languages.length === 0) {` +
			`Object.defineProperty(navigator, "languages", {get: () => ["zh-CN", "zh", "en-US", "en"]});}`,
	})

	if err := chromedp.Do(loginContext, chromedp.Navigate(grokSignInURL)); err != nil {
		return "", fmt.Errorf("open sign-in page: %w", err)
	}
	time.Sleep(500 * time.Millisecond)
	_, _ = chromedp.Run(loginContext, chromedp.Evaluate[bool](`(() => {
		const selectors = ["#onetrust-policy-close", "#onetrust-close-btn-container button", 'button[aria-label="Close"]', 'button[aria-label="关闭"]'];
		for (const selector of selectors) {
			const element = document.querySelector(selector);
			if (element && element.offsetParent !== null) { element.click(); return true; }
		}
		return false;
	})()`))

	emailSelector := `input[name="email"], input#email, input[type="email"]`
	if err := waitGrokVisible(loginContext, emailSelector, 30*time.Second); err != nil {
		return "", fmt.Errorf("email input not found: %w", err)
	}
	if err := chromedp.Do(loginContext,
		chromedp.Focus(chromedp.CSS(emailSelector)),
		chromedp.SendKeys(chromedp.CSS(emailSelector), card.Email),
	); err != nil {
		return "", fmt.Errorf("fill email: %w", err)
	}
	if err := clickGrokSubmit(loginContext); err != nil {
		return "", fmt.Errorf("submit email: %w", err)
	}

	passwordSelector := `input[name="password"], input[type="password"]`
	if err := waitGrokVisible(loginContext, passwordSelector, 30*time.Second); err != nil {
		return "", fmt.Errorf("password input not found: %w", err)
	}
	if err := chromedp.Do(loginContext,
		chromedp.Focus(chromedp.CSS(passwordSelector)),
		chromedp.SendKeys(chromedp.CSS(passwordSelector), card.Password),
	); err != nil {
		return "", fmt.Errorf("fill password: %w", err)
	}
	if err := clickGrokSubmit(loginContext); err != nil {
		return "", fmt.Errorf("submit password: %w", err)
	}

	otpSelector := `input[inputmode="numeric"], input[name="code"], input[name="otp"], input[autocomplete="one-time-code"], input[placeholder*="code" i], input[placeholder*="验证码" i]`
	if err := waitGrokVisible(loginContext, otpSelector, 15*time.Second); err == nil {
		if card.TOTPSecret == "" {
			return "", errors.New("OTP requested but card has no TOTP secret")
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
			return "", fmt.Errorf("submit OTP: %w", err)
		}
	}

	deadline := time.Now().Add(options.Timeout)
	for time.Now().Before(deadline) {
		cookie := extractGrokSSOCookie(loginContext)
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
		const submit = document.querySelector('button[type="submit"]');
		if (submit && submit.offsetParent !== null) { submit.click(); return true; }
		const buttons = Array.from(document.querySelectorAll("button"));
		const match = buttons.find(button => button.offsetParent !== null && /^(next|continue|verify|下一步|继续|验证)$/i.test((button.innerText || "").trim()));
		if (match) { match.click(); return true; }
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

func extractGrokSSOCookie(ctx context.Context) string {
	response, err := chromedp.Call(ctx, network.GetCookies, network.GetCookiesParams{
		URLs: []string{"https://grok.com", "https://x.ai", "https://accounts.x.ai"},
	})
	if err != nil {
		return ""
	}

	values := make(map[string]string)
	for _, cookie := range response.Cookies {
		name := strings.ToLower(cookie.Name)
		if name != "sso" && name != "sso-rw" || cookie.Value == "" || !grokCookieDomainAllowed(cookie.Domain) {
			continue
		}
		values[name] = cookie.Value
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
