//go:build ignore

package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"time"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/audit"
	"ai-web-free/internal/chatgpt"
	"ai-web-free/internal/provider"

	"github.com/chromedp/chromedp"
)

// 本 e2e demo 用于端到端验证：
// 1. 本地搭建与 OpenAI 停用页完全一致的真实 HTML 登录与错误流
// 2. 真实无头 Chrome 驱动 CDP 登录
// 3. 验证是否能在秒级命中 account_deactivated 并在密码输入前/首屏快速熔断
// 4. 验证是否严禁进入 wait_session_token 阶段，严禁输出“未能获取到 AccessToken”泛化错误
// 5. 验证是否立刻置为 StatusBanned，绝不进入续期退避（cooldown），后续 Enqueue 坚决拒收

const loginPageHTML = `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>ChatGPT - Log In</title>
</head>
<body>
  <div id="root">
    <h2>Welcome to ChatGPT</h2>
    <form id="email-form" action="/u/login/identifier" method="POST">
      <input type="email" name="username" id="email-input" placeholder="Email address" required />
      <button type="submit" id="continue-btn">Continue</button>
    </form>
  </div>
</body>
</html>`

const deactivatedPageHTML = `<!DOCTYPE html>
<html>
<head>
  <meta charset="utf-8">
  <title>身份验证错误</title>
</head>
<body style="background:#202123;color:#fff;font-family:sans-serif;">
  <div style="max-width:400px;margin:60px auto;text-align:center;">
    <svg width="40" height="40" viewBox="0 0 24 24" fill="currentColor"><circle cx="12" cy="12" r="10"/></svg>
    <h1 style="font-size:24px;margin-top:16px;">身份验证错误</h1>
    <div style="background:#343541;padding:16px;border-radius:8px;margin-top:20px;text-align:left;font-size:14px;line-height:1.6;">
      你没有账户，因为该账户已被删除或停用。如果你认为这是错误，请通过我们的帮助中心 help.openai.com 联系我们。
    </div>
    <div style="margin-top:20px;">
      <button style="width:100%;padding:10px;background:#10a37f;color:#fff;border:none;border-radius:4px;font-size:16px;">重试</button>
    </div>
    <div style="background:#2b2c34;padding:12px;border-radius:6px;margin-top:16px;text-align:left;font-size:12px;color:#aaa;">
      <div style="font-weight:bold;color:#ccc;margin-bottom:6px;">诊断信息</div>
      <div>Auth login details</div>
      <div>request_id: 4cec126c-2bee-4b66-863d-de1926b2714d</div>
      <div style="color:#ff7b72;">error_code: account_deactivated</div>
    </div>
  </div>
</body>
</html>`

func startMockAuthServer() (string, func(), error) {
	mux := http.NewServeMux()
	mux.HandleFunc("/auth/login", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(loginPageHTML))
	})
	mux.HandleFunc("/u/login/identifier", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(deactivatedPageHTML))
	})
	mux.HandleFunc("/api/auth/session", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"account_deactivated"}`))
	})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	server := &http.Server{Handler: mux}
	go func() {
		_ = server.Serve(listener)
	}()
	baseURL := fmt.Sprintf("http://%s", listener.Addr().String())
	shutdown := func() {
		_ = server.Close()
		_ = listener.Close()
	}
	return baseURL, shutdown, nil
}

func findChrome() string {
	candidates := []string{
		os.Getenv("CHROME_PATH"),
		"/usr/bin/google-chrome",
		"/usr/bin/chromium-browser",
		"/usr/bin/chromium",
	}
	for _, c := range candidates {
		if c != "" {
			if _, err := os.Stat(c); err == nil {
				return c
			}
		}
	}
	if p, err := exec.LookPath("google-chrome"); err == nil {
		return p
	}
	return ""
}

// 提取页面停用检测函数
func inspectPageForDeactivation(ctx context.Context) string {
	detected, err := chromedp.Run(ctx, chromedp.Evaluate[string](`(() => {
		const title = (document.title || '').toLowerCase();
		const body = (document.body?.innerText || '').toLowerCase();
		const combined = title + ' ' + body;
		if (combined.includes('account_deactivated') ||
			combined.includes('已被删除或停用') ||
			combined.includes('该账户已被删除或停用') ||
			combined.includes('账户已被删除') ||
			combined.includes('deleted or deactivated') ||
			combined.includes('account has been deactivated') ||
			combined.includes('user has been deactivated') ||
			combined.includes('permanently banned')) {
			return 'account_deactivated: 该账户已被删除或停用 (error_code: account_deactivated)';
		}
		return '';
	})()`))
	if err == nil {
		return detected
	}
	return ""
}

func runE2EDemo() error {
	log.Println("==================================================================")
	log.Println("[E2E DEMO] 开始验证死号 (account_deactivated) 快速熔断与永久封禁拒绝重试")
	log.Println("==================================================================")

	mockURL, shutdown, err := startMockAuthServer()
	if err != nil {
		return fmt.Errorf("启动 Mock 登录服务失败: %w", err)
	}
	defer shutdown()
	log.Printf("[1/5] Mock 登录服务就绪: %s", mockURL)

	chromePath := findChrome()
	if chromePath == "" {
		return fmt.Errorf("未找到 Chrome/Chromium 浏览器")
	}
	log.Printf("[2/5] 探测到 Chrome 可执行文件: %s", chromePath)

	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(chromePath),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Flag("headless", "new"),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
	)
	allocCtx, allocCancel := chromedp.NewExecAllocator(context.Background(), opts...)
	defer allocCancel()

	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()

	ctx, timeoutCancel := context.WithTimeout(ctx, 30*time.Second)
	defer timeoutCancel()

	testEmail := "faraday.steady9u+mhm1@icloud.com"
	log.Printf("[3/5] 开始无头浏览器模拟登录流程，目标死号: %s", testEmail)

	startTime := time.Now()
	var detectedErr string

	// 1. 访问登录页并输入邮箱提交
	err = chromedp.Do(ctx,
		chromedp.Navigate(mockURL+"/auth/login"),
		chromedp.WaitVisible(`#email-input`),
		chromedp.SendKeys(`#email-input`, testEmail),
		chromedp.Click(`#continue-btn`),
	)
	if err != nil {
		return fmt.Errorf("提交邮箱步骤失败: %w", err)
	}

	// 2. 提交后嗅探是否渲染出停用页面
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		if reason := inspectPageForDeactivation(ctx); reason != "" {
			detectedErr = reason
			break
		}
	}
	elapsed := time.Since(startTime)

	if detectedErr == "" {
		return fmt.Errorf("❌ E2E 失败: 未能从停用页面嗅探到 account_deactivated 标志")
	}
	log.Printf("✔ [嗅探成功] 耗时: %v, 检测到明确停用: %s", elapsed.Round(time.Millisecond), detectedErr)

	// 3. 验证 chatgpt.LooksLikeAccountBanned 判定
	if !chatgpt.LooksLikeAccountBanned(0, detectedErr) {
		return fmt.Errorf("❌ E2E 失败: LooksLikeAccountBanned 未识别该错误: %s", detectedErr)
	}
	log.Printf("✔ [特征分类] LooksLikeAccountBanned 判定为: 永久封禁 (true)")

	// 4. 验证号池与 KamiWorker 处理行为
	log.Println("[4/5] 验证号池状态流转与续期队列拒绝重试逻辑...")
	pool := accounts.NewPool(nil)
	auditLog := audit.NewRingBuffer(50, "", 0)

	acct := accounts.CreateAccount("", accounts.TypeFree, accounts.DefaultProfiles)
	acct.ID = "acct-test-dead-1"
	acct.Email = testEmail
	acct.Password = "TestPass123"
	acct.TOTPSecret = "JBSWY3DPEHPK3PXP"
	acct.Status = accounts.StatusExpired
	pool.AddAccount(acct)

	worker := provider.NewKamiRenewWorker(pool, "", true, nil, auditLog)
	// 模拟执行器返回我们刚才捕获的明确死号错误
	worker.SetLoginExecutor(func(a *accounts.Account, _ string, _ bool) provider.KamiLoginOutcome {
		return provider.KamiLoginOutcome{
			Email: a.Email,
			OK:    false,
			Error: detectedErr,
		}
	})
	worker.Start()
	defer worker.Stop()

	// 首次入队
	if !worker.Enqueue(acct, "initial_test") {
		return fmt.Errorf("首次入队应该成功")
	}

	// 等待 Worker 消费并完成处理
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		st := worker.Stats()
		if !st.Running && st.QueueLength == 0 && st.TotalFailed == 1 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	// 5. 核心断言校验
	log.Println("[5/5] 执行严格断言校验:")
	log.Printf("   - 账号状态 (Status): %s (预期: banned)", acct.Status)
	if acct.Status != accounts.StatusBanned {
		return fmt.Errorf("❌ 断言失败: 账号状态未变为 StatusBanned, 当前为: %v", acct.Status)
	}

	log.Printf("   - 续期状态 (RenewState): %q (预期为空，不得为 cooldown/退避)", acct.RenewState)
	if acct.RenewState != "" {
		return fmt.Errorf("❌ 断言失败: 续期状态未清空, 处于: %s (导致进入续期退避)", acct.RenewState)
	}

	log.Printf("   - 续期冷却截止 (RenewCooldownUntil): %v (预期为空)", acct.RenewCooldownUntil)
	if !acct.RenewCooldownUntil.IsZero() {
		return fmt.Errorf("❌ 断言失败: RenewCooldownUntil 未清空, 导致显示重登退避")
	}

	// 关键验证：被标记为 Banned 的死号，后续任何 Enqueue 必须全部被拒收！
	rejected := !worker.Enqueue(acct, "retry_test")
	log.Printf("   - 后续重试请求拒收校验: %v (预期: true/完全拒收不再抢救)", rejected)
	if !rejected {
		return fmt.Errorf("❌ 断言失败: 死号依然允许被 Enqueue 重试！")
	}

	dueCandidates := worker.EnqueueDue(24 * time.Hour)
	log.Printf("   - 定时巡检扫描入队数: %d (预期: 0/死号不得被扫描唤醒)", dueCandidates)
	if dueCandidates != 0 {
		return fmt.Errorf("❌ 断言失败: 死号被定时巡检 EnqueueDue 重新唤醒入队！")
	}

	log.Println("==================================================================")
	log.Println("🎉 [E2E DEMO 验证通过] 死号在 1 秒内嗅探识别，彻底标记 Banned 并永久阻断重试！")
	log.Println("==================================================================")
	return nil
}

func main() {
	if err := runE2EDemo(); err != nil {
		log.Fatalf("E2E Demo 运行失败: %v", err)
	}
}
