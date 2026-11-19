//go:build ignore

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/provider"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

func maskString(s string, prefixLen, suffixLen int) string {
	if len(s) <= prefixLen+suffixLen {
		return "***"
	}
	return s[:prefixLen] + "***" + s[len(s)-suffixLen:]
}

func maskEmail(email string) string {
	at := strings.Index(email, "@")
	if at <= 2 {
		return "***" + email[at:]
	}
	return email[:2] + "***" + email[at:]
}

func autoDetectProxy() string {
	// 1. Env vars
	for _, env := range []string{"PROXY_URL", "HTTP_PROXY", "http_proxy", "HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy"} {
		if val := strings.TrimSpace(os.Getenv(env)); val != "" {
			return val
		}
	}
	// 2. Check common local proxy ports
	for _, addr := range []string{"127.0.0.1:10809", "127.0.0.1:7890", "127.0.0.1:10808"} {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			conn.Close()
			return "http://" + addr
		}
	}
	return ""
}

func main() {
	filePath := flag.String("file", "grok-free.txt", "卡密账号文件路径")
	targetIndex := flag.Int("index", -1, "选择测试账号的序号 (1-based)，-1 表示随机选择")
	targetEmail := flag.String("email", "", "指定匹配的账号邮箱")
	proxyArg := flag.String("proxy", "", "代理地址 (留空自动检测本机代理)")
	headless := flag.Bool("headless", false, "是否无头运行 (默认 false 有头模式，便于遇到 CF 人机盾时人工介入)")
	timeoutSec := flag.Int("timeout", 180, "全局超时时间 (秒)")
	flag.Parse()

	log.Println("==========================================================")
	log.Println("  🤖 Grok Web 逆向登录与 Cloudflare 防御 E2E 端到端验证工具")
	log.Println("==========================================================")

	// 1. 解析卡密文件
	cards, err := provider.ParseGrokCardFile(*filePath)
	if err != nil {
		log.Fatalf("❌ 解析卡密文件 %s 失败: %v", *filePath, err)
	}
	if len(cards) == 0 {
		log.Fatalf("❌ 文件 %s 中未找到有效的 Grok 账号", *filePath)
	}
	log.Printf("✅ 成功解析卡密文件: %s，共获取到 %d 个账号\n", *filePath, len(cards))

	// 2. 选择账号
	var selectedCard provider.GrokCardAccount
	if *targetEmail != "" {
		for _, c := range cards {
			if strings.EqualFold(c.Email, *targetEmail) || strings.Contains(strings.ToLower(c.Email), strings.ToLower(*targetEmail)) {
				selectedCard = c
				break
			}
		}
		if selectedCard.Email == "" {
			log.Fatalf("❌ 未找到匹配邮箱 %s 的账号", *targetEmail)
		}
	} else if *targetIndex > 0 && *targetIndex <= len(cards) {
		selectedCard = cards[*targetIndex-1]
	} else {
		// 随机选择一个
		r := rand.New(rand.NewSource(time.Now().UnixNano()))
		selectedCard = cards[r.Intn(len(cards))]
	}

	log.Printf("🎯 [选中账号] 邮箱: %s (行号: %d)", maskEmail(selectedCard.Email), selectedCard.LineNo)
	log.Printf("   - 凭据状态: 密码:已配置 | 2FA密钥:%v | 旧Token:%v | 旧Cookie:%v",
		selectedCard.TOTPSecret != "", selectedCard.LegacyToken != "", selectedCard.OldCookie != "")

	// 3. 确定代理
	activeProxy := *proxyArg
	if activeProxy == "" && selectedCard.Proxy != "" {
		activeProxy = selectedCard.Proxy
	}
	if activeProxy == "" {
		activeProxy = autoDetectProxy()
	}
	if activeProxy != "" {
		log.Printf("🌐 [代理配置] 使用代理: %s", activeProxy)
	} else {
		log.Printf("⚠️ [代理配置] 未检测到代理，将直连 (海外站点可能需要代理)")
	}

	// 4. 解析 Chrome 可执行文件路径
	chromePath, err := provider.ResolveChromeExecutable()
	if err != nil {
		log.Fatalf("❌ 未检测到 Chrome 可执行文件，请确认已安装 Google Chrome 或设置 CHROME_PATH 环境变量: %v", err)
	}
	log.Printf("🔍 [浏览器路径] %s", chromePath)

	// 5. 配置 Chrome 与 CDP (不继承 DefaultExecAllocatorOptions 的 Headless 选项)
	var opts []chromedp.ExecAllocatorOption
	opts = append(opts,
		chromedp.ExecPath(chromePath),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("incognito", true),
		chromedp.Flag("disable-blink-features", "AutomationControlled"),
		chromedp.Flag("user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/133.0.0.0 Safari/537.36"),
		chromedp.Flag("lang", "zh-CN,zh,en-US,en"),
		chromedp.WindowSize(1280, 800),
	)
	if activeProxy != "" {
		opts = append(opts, chromedp.ProxyServer(activeProxy))
	}
	if *headless {
		opts = append(opts, chromedp.Flag("headless", "new"))
		log.Printf("🖥️ [浏览器模式] 无头模式 (headless=new)")
	} else {
		opts = append(opts, chromedp.Flag("start-maximized", true))
		log.Printf("🖥️ [浏览器模式] 真实桌面可见模式 (桌面将弹出 Chrome 浏览器窗口)")
	}

	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer allocCancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	ctx, timeoutCancel := context.WithTimeout(ctx, time.Duration(*timeoutSec)*time.Second)
	defer timeoutCancel()

	_, _ = chromedp.Call(ctx, network.Enable, network.EnableParams{})
	_, _ = chromedp.Call(ctx, page.AddScriptToEvaluateOnNewDocument, page.AddScriptToEvaluateOnNewDocumentParams{
		Source: `Object.defineProperty(navigator, "webdriver", {get: () => undefined});` +
			`window.chrome = window.chrome || {runtime: {}};` +
			`if (!navigator.languages || navigator.languages.length === 0) {` +
			`Object.defineProperty(navigator, "languages", {get: () => ["zh-CN", "zh", "en-US", "en"]});}`,
	})

	log.Println("🚀 [阶段 1] 打开统一认证页 https://accounts.x.ai/sign-in?redirect=grok-com&email=true ...")
	if err := chromedp.Do(ctx, chromedp.Navigate("https://accounts.x.ai/sign-in?redirect=grok-com&email=true")); err != nil {
		log.Fatalf("❌ 打开登录页失败: %v", err)
	}

	time.Sleep(1 * time.Second)
	dismissCookieConsent(ctx)

	log.Printf("✍️ [阶段 2] 填充邮箱: %s 并点击下一步", maskEmail(selectedCard.Email))
	emailSelector := `input[name="email"], input#email, input[type="email"]`
	if err := waitVisible(ctx, emailSelector, 30*time.Second); err != nil {
		log.Fatalf("❌ 邮箱输入框未出现: %v", err)
	}
	if err := chromedp.Do(ctx,
		chromedp.Focus(chromedp.CSS(emailSelector)),
		chromedp.SendKeys(chromedp.CSS(emailSelector), selectedCard.Email),
	); err != nil {
		log.Fatalf("❌ 填充邮箱失败: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	if err := clickSubmit(ctx); err != nil {
		log.Fatalf("❌ 点击邮箱提交失败: %v", err)
	}

	log.Println("🔑 [阶段 3] 等待密码输入框并填充密码...")
	passwordSelector := `input[name="password"], input[type="password"]`
	if err := waitVisible(ctx, passwordSelector, 30*time.Second); err != nil {
		log.Fatalf("❌ 密码输入框未出现: %v", err)
	}
	dismissCookieConsent(ctx)
	if err := chromedp.Do(ctx,
		chromedp.Focus(chromedp.CSS(passwordSelector)),
		chromedp.SendKeys(chromedp.CSS(passwordSelector), selectedCard.Password),
	); err != nil {
		log.Fatalf("❌ 填充密码失败: %v", err)
	}

	log.Println("🛡️ [阶段 4] 探测 Cloudflare 防自动登录机制 (Turnstile 验盾)...")
	time.Sleep(1 * time.Second)
	turnstilePresent, token := checkTurnstile(ctx)
	if turnstilePresent {
		log.Printf("   ⚠️ 页面存在 Cloudflare Turnstile 验证组件 (当前 token: %s)", func() string {
			if token != "" {
				return "已生成(" + maskString(token, 6, 6) + ")"
			}
			return "等待通过"
		}())
		if token == "" {
			waitLimit := 10 * time.Second
			if !*headless {
				waitLimit = 90 * time.Second
				log.Println("   👉 [人工介入提示] 请在弹出的 Chrome 浏览器窗口中，点击 Cloudflare 验证框或完成人机验证...")
			}
			deadline := time.Now().Add(waitLimit)
			resolved := false
			for time.Now().Before(deadline) {
				time.Sleep(500 * time.Millisecond)
				_, curToken := checkTurnstile(ctx)
				if curToken != "" {
					resolved = true
					token = curToken
					log.Printf("   🎉 Cloudflare Turnstile 验证成功通过！(token 长度=%d)", len(token))
					break
				}
			}
			if !resolved {
				if *headless {
					log.Fatalf("❌ [拦截] 检测到 Cloudflare 防自动登录机制 (Turnstile)，无头模式被拦截。请使用有头模式 (go run scripts/grok_e2e_login.go) 手动点击验证。")
				} else {
					log.Fatalf("❌ 等待 Cloudflare Turnstile 验证超时 (90s)，未完成人机验证。")
				}
			}
		}
	} else {
		log.Println("   ℹ️ 未检测到 Turnstile 验证组件或已自动放行")
	}

	dismissCookieConsent(ctx)
	log.Println("📨 [阶段 5] 提交登录表单...")
	if err := clickSubmit(ctx); err != nil {
		log.Fatalf("❌ 提交登录表单失败: %v", err)
	}

	time.Sleep(1500 * time.Millisecond)
	if err := checkPostSubmit(ctx); err != nil {
		log.Fatalf("❌ 登录提交后拦截: %v", err)
	}

	// 6. 检查 2FA OTP
	otpSelector := `input[inputmode="numeric"], input[name="code"], input[name="otp"], input[autocomplete="one-time-code"], input[placeholder*="code" i], input[placeholder*="验证码" i]`
	if err := waitVisible(ctx, otpSelector, 8*time.Second); err == nil {
		log.Println("🔢 [阶段 6] 触发 2FA 动态验证码校验...")
		if selectedCard.TOTPSecret == "" {
			log.Fatalf("❌ 账号触发 2FA 验证，但卡密中无 TOTP Secret！")
		}
		code, _, err := accounts.GenerateTOTP(selectedCard.TOTPSecret, time.Now())
		if err != nil {
			log.Fatalf("❌ 计算 TOTP 失败: %v", err)
		}
		log.Printf("   -> 自动计算并填入 2FA 动态码: %s", code)
		_ = chromedp.Do(ctx,
			chromedp.Focus(chromedp.CSS(otpSelector)),
			chromedp.SendKeys(chromedp.CSS(otpSelector), code),
		)
		time.Sleep(300 * time.Millisecond)
		if err := clickSubmit(ctx); err != nil {
			log.Fatalf("❌ 提交 2FA 失败: %v", err)
		}
	}

	log.Println("🍪 [阶段 7] 等待并捕获 Grok SSO 会话 Cookie...")
	ssoCookie := ""
	ssoDeadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(ssoDeadline) {
		resp, err := chromedp.Call(ctx, network.GetCookies, network.GetCookiesParams{
			URLs: []string{"https://grok.com", "https://x.ai", "https://accounts.x.ai"},
		})
		if err == nil {
			cookieMap := make(map[string]string)
			for _, c := range resp.Cookies {
				name := strings.ToLower(c.Name)
				if (name == "sso" || name == "sso-rw") && c.Value != "" {
					cookieMap[name] = c.Value
				}
			}
			if len(cookieMap) > 0 {
				ssoCookie = provider.FormatGrokSSOCookie(cookieMap)
				break
			}
		}
		time.Sleep(1 * time.Second)
	}

	if ssoCookie == "" {
		log.Fatalf("❌ 在超时时间内未提取到有效的 Grok SSO Cookie")
	}

	log.Println("\n🎉🎉🎉 ========================================================== 🎉🎉🎉")
	log.Printf("  🏆 [E2E 验证成功] 账号 %s 成功完成登录！", maskEmail(selectedCard.Email))
	log.Printf("  🍪 提取到 SSO Cookie: %s", maskString(ssoCookie, 16, 12))
	log.Println("🎉🎉🎉 ========================================================== 🎉🎉🎉\n")

	// 8. 实时对 Grok API 执行测活验证
	log.Println("📡 [阶段 8] 对 Grok 官方会话 API 执行连通性与凭据有效性测活...")
	probeOK, probeErr := probeGrokAPI(ssoCookie, activeProxy)
	if probeOK {
		log.Println("✅ [API 验证通过] 该 SSO Cookie 可正常调用 Grok 会话接口！")
	} else {
		log.Printf("⚠️ [API 验证反馈] %v", probeErr)
	}

	log.Println("\n✨ E2E 完整验证流程已顺利结束。")
}

func probeGrokAPI(cookie, proxyURL string) (bool, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	if proxyURL != "" {
		if pURL, err := url.Parse(proxyURL); err == nil {
			client.Transport = &http.Transport{Proxy: http.ProxyURL(pURL)}
		}
	}
	req, _ := http.NewRequest(http.MethodGet, "https://grok.com/rest/app-chat/conversations?limit=1", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Referer", "https://grok.com/")
	resp, err := client.Do(req)
	if err != nil {
		return false, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusOK {
		return true, nil
	}
	return false, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
}

func dismissCookieConsent(ctx context.Context) {
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

func waitVisible(ctx context.Context, selector string, timeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		vis, err := chromedp.Run(waitCtx, chromedp.Evaluate[bool](fmt.Sprintf(`(() => {
			const el = document.querySelector(%q);
			return Boolean(el && el.offsetParent !== null);
		})()`, selector)))
		if err != nil {
			return err
		}
		if vis {
			return nil
		}
		select {
		case <-waitCtx.Done():
			return waitCtx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

func clickSubmit(ctx context.Context) error {
	clicked, err := chromedp.Run(ctx, chromedp.Evaluate[bool](`(() => {
		const submit = document.querySelector('button[type="submit"]');
		if (submit && submit.offsetParent !== null) { submit.click(); return true; }
		const buttons = Array.from(document.querySelectorAll("button"));
		const match = buttons.find(button => button.offsetParent !== null && /^(next|continue|verify|login|sign in|下一步|继续|验证|登录)$/i.test((button.innerText || "").trim()));
		if (match) { match.click(); return true; }
		return false;
	})()`))
	if err != nil {
		return err
	}
	if !clicked {
		return fmt.Errorf("未找到可点击的提交/下一步按钮")
	}
	return nil
}

func checkTurnstile(ctx context.Context) (bool, string) {
	res, err := chromedp.Run(ctx, chromedp.Evaluate[map[string]string](`(() => {
		const input = document.querySelector('input[name="cf-turnstile-response"]');
		const hasIframe = Boolean(document.querySelector('iframe[src*="challenges.cloudflare.com"]'));
		const hasChallenge = /verification challenge|验证|human/i.test(document.body.innerText || '');
		const token = input ? (input.value || '').trim() : '';
		return {
			"present": (input || hasIframe || hasChallenge) ? "true" : "false",
			"token": token
		};
	})()`))
	if err != nil {
		return false, ""
	}
	return res["present"] == "true", res["token"]
}

func checkPostSubmit(ctx context.Context) error {
	errKey, _ := chromedp.Run(ctx, chromedp.Evaluate[string](`(() => {
		const text = (document.body.innerText || '');
		if (text.includes('Complete the verification challenge')) return 'turnstile_incomplete';
		if (/invalid (email or )?password|incorrect password|密码错误/i.test(text)) return 'invalid_credentials';
		if (/account( has been)? (deactivated|suspended)|permanently banned|已被(删除|停用|封禁)/i.test(text)) return 'account_banned';
		return '';
	})()`))
	switch errKey {
	case "turnstile_incomplete":
		return fmt.Errorf("Cloudflare 人机安全验证未通过 (Complete the verification challenge)")
	case "invalid_credentials":
		return fmt.Errorf("邮箱或密码错误 (invalid credentials)")
	case "account_banned":
		return fmt.Errorf("account_deactivated: 该账号已被删除或封禁")
	}
	return nil
}
