package bootstrap

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"ai-web-free/httpclient"
	bogdanfinn "ai-web-free/httpclient/bogdanfinn"
	"ai-web-free/internal/accounts"
	"ai-web-free/internal/audit"
	"ai-web-free/internal/browserfp"
	"ai-web-free/internal/chatgpt"
	"ai-web-free/internal/config"
	"ai-web-free/internal/db"
	"ai-web-free/internal/handler"
	"ai-web-free/internal/health"
	"ai-web-free/internal/opslog"
	"ai-web-free/internal/promptplaza"
	"ai-web-free/internal/provider"
	"ai-web-free/internal/proxy"
	"ai-web-free/internal/storage"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
)

// App 封装应用启动所需的所有依赖
type App struct {
	Router        *gin.Engine
	Config        *config.Config
	AccountPool   *accounts.Pool
	HealthTracker *health.Tracker
	Cleanup       func() // 服务退出时调用，停止后台协程
}

// Init 完成所有初始化逻辑，返回 App 实例
func Init() (*App, error) {
	opsLog := opslog.Global()
	opsLog.InstallStdLogHook()

	gin.SetMode(gin.ReleaseMode)
	loadDotEnv()
	browserfp.Init()

	cfg := config.Load()

	proxies := loadProxyList()
	proxyPool := proxy.NewPool(proxies)

	// ─── 加载账号 ────────────────────────────────────────────────

	profiles := accounts.DefaultProfiles
	var accs []*accounts.Account

	// 加载已封禁账号持久化名单（防止重启后死号又变绿）
	bannedMap := accounts.LoadBannedFromFile("banned_accounts.txt")
	// 加载管理员手动禁用名单（防止重启后失效）
	disabledMap := accounts.LoadDisabledFromFile("disabled_accounts.txt")
	// 加载账号粘性代理绑定关系（保证重启后账号出口 IP 不漂移）
	accountProxyMap := accounts.LoadAccountProxiesFromFile("account_proxies.txt")

	assignStickyProxy := func(acct *accounts.Account) {
		key := strings.ToLower(strings.TrimSpace(acct.Email))
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(acct.ChatGPTAccountID))
		}
		if key == "" {
			key = acct.ID
		}
		if savedProxy, ok := accountProxyMap[key]; ok && savedProxy != "" {
			if proxyPool.Contains(savedProxy) && proxyPool.IsHealthy(savedProxy) {
				acct.Proxy = savedProxy
				return
			}
		}
		// 无历史绑定或原绑定节点不可用时分配新节点并建立粘性映射
		newProxy := proxyPool.Allocate()
		acct.Proxy = newProxy
		if key != "" && newProxy != "" {
			accountProxyMap[key] = newProxy
		}
	}

	// 去重 key: chatgpt_account_id (for free/puid) 或 token (for noauth/refresh)
	seen := make(map[string]bool)

	// 1. access_tokens.txt — 纯 access token，不可续期
	for _, t := range accounts.LoadTokensFromFile("access_tokens.txt") {
		// 从 access_token JWT 解析 chatgpt_account_id 用于去重
		chatGPTID := accounts.ExtractChatGPTAccountID(t.Token)
		email := accounts.ExtractEmail(t.Token)
		key := "access:" + chatGPTID
		if chatGPTID == "" && email != "" {
			key = "access:" + email
		}
		if seen[key] {
			continue
		}
		seen[key] = true

		acct := accounts.CreateAccount(t.Token, accounts.TypeFree, profiles)
		acct.TeamUserID = t.TeamID
		acct.ChatGPTAccountID = chatGPTID
		if email != "" {
			acct.Email = email
		}
		assignStickyProxy(acct)

		banReason := ""
		disableReason := ""
		if chatGPTID != "" {
			banReason = bannedMap[strings.ToLower(chatGPTID)]
			disableReason = disabledMap[strings.ToLower(chatGPTID)]
		}
		if banReason == "" && email != "" {
			banReason = bannedMap[strings.ToLower(email)]
		}
		if disableReason == "" && email != "" {
			disableReason = disabledMap[strings.ToLower(email)]
		}

		if banReason != "" {
			acct.Status = accounts.StatusBanned
			acct.LastError = banReason
			acct.Token = ""
		} else if disableReason != "" {
			acct.Status = accounts.StatusDisabled
			acct.LastError = disableReason
		} else if !acct.ExpiresAt.IsZero() && acct.ExpiresAt.Before(time.Now()) {
			acct.Status = accounts.StatusExpired
			acct.LastError = "Token 已过期"
		} else {
			acct.Status = accounts.StatusActive
		}
		accs = append(accs, acct)
	}

	// 2. refresh_tokens.txt — 带 refresh_token，可续期
	for _, t := range accounts.LoadTokensFromFile("refresh_tokens.txt") {
		// 立即交换一次获取 access_token 才能解析 chatgpt_account_id
		// 先用 refresh_token 本身做去重 key,避免重复交换
		key := "refresh:" + t.Token
		if seen[key] {
			continue
		}
		seen[key] = true

		acct := accounts.CreateAccount("", accounts.TypeFree, profiles)
		acct.RefreshToken = t.Token
		acct.TeamUserID = t.TeamID
		assignStickyProxy(acct)
		// 立即交换一次获取 access_token
		if exchangeRefreshToken(acct) {
			// exchange 成功后从 access_token 解析 chatgpt_account_id
			acct.ChatGPTAccountID = accounts.ExtractChatGPTAccountID(acct.Token)
			acct.Status = accounts.StatusActive
		} else {
			acct.Status = accounts.StatusExpired
		}
		accs = append(accs, acct)
	}

	// 3. session_tokens.txt — ChatGPT session token，用于免费账号续期
	for _, t := range accounts.LoadTokensFromFile("session_tokens.txt") {
		key := "session:" + t.Token
		if seen[key] {
			continue
		}
		seen[key] = true

		acct := accounts.CreateAccount("", accounts.TypeFree, profiles)
		acct.SessionToken = t.Token
		acct.TeamUserID = t.TeamID
		assignStickyProxy(acct)
		// 立即交换一次获取 access_token
		if exchangeSessionToken(acct) {
			// exchange 成功后从 access_token 解析 chatgpt_account_id
			acct.ChatGPTAccountID = accounts.ExtractChatGPTAccountID(acct.Token)
			acct.Status = accounts.StatusActive
		} else {
			acct.Status = accounts.StatusExpired
		}
		accs = append(accs, acct)
	}

	// 4. free_tokens.txt — 设备 UUID
	for _, t := range accounts.LoadTokensFromFile("free_tokens.txt") {
		key := "free:" + t.Token
		if seen[key] {
			continue
		}
		seen[key] = true

		acct := accounts.CreateAccount(t.Token, accounts.TypeNoAuth, profiles)
		assignStickyProxy(acct)
		acct.Status = accounts.StatusActive
		accs = append(accs, acct)
	}

	// 4.5 kami_accounts.txt — 卡密账号（邮箱----密码----2FA密钥）
	for _, k := range accounts.LoadKamiFromFile("kami_accounts.txt") {
		emailLower := strings.ToLower(strings.TrimSpace(k.Email))
		banReason := bannedMap[emailLower]
		disableReason := disabledMap[emailLower]

		var matched *accounts.Account
		for _, existing := range accs {
			if existing.Type != accounts.TypeNoAuth && strings.EqualFold(strings.TrimSpace(existing.Email), strings.TrimSpace(k.Email)) {
				matched = existing
				break
			}
		}
		if matched != nil {
			matched.Password = k.Password
			matched.TOTPSecret = k.TOTPSecret
			if banReason != "" {
				matched.Status = accounts.StatusBanned
				matched.LastError = banReason
				matched.Token = ""
			} else if disableReason != "" {
				matched.Status = accounts.StatusDisabled
				matched.LastError = disableReason
			}
			continue
		}
		acct := accounts.CreateAccount("", accounts.TypeFree, profiles)
		acct.Email = k.Email
		acct.Password = k.Password
		acct.TOTPSecret = k.TOTPSecret
		assignStickyProxy(acct)
		if banReason != "" {
			acct.Status = accounts.StatusBanned
			acct.LastError = banReason
		} else if disableReason != "" {
			acct.Status = accounts.StatusDisabled
			acct.LastError = disableReason
		} else {
			acct.Status = accounts.StatusPending
			acct.LastError = "待浏览器登录提取 Token"
		}
		accs = append(accs, acct)
	}

	// 回写初始分配或对齐后的粘性代理配置
	_ = accounts.SaveAccountProxiesToFile("account_proxies.txt", accountProxyMap)

	// 5. FREE_ACCOUNTS — 自动生成 UUID 账号
	if cfg.FreeAccounts {
		for i := 0; i < cfg.FreeAccountsNum; i++ {
			uid := uuid.NewString()
			// UUID 设备号天然唯一,不需要去重
			acct := accounts.CreateAccount(uid, accounts.TypeNoAuth, profiles)
			acct.Proxy = proxyPool.Allocate()
			acct.Status = accounts.StatusActive
			accs = append(accs, acct)
		}
	}

	accountPool := accounts.NewPool(accs)

	// 热池/待命池：按供应商 + 账号类型读取阈值（POOL_CHATGPT_FREE_ACTIVE_TARGET 等），0 表示不限制
	applyPoolPolicy(accountPool, "chatgpt", accounts.TypeFree)
	applyPoolPolicy(accountPool, "chatgpt", accounts.TypePUID)
	accountPool.Rebalance()

	// 初始化 TLS Client（待命账号延迟到激活时创建）
	for _, acct := range accs {
		if acct.Status == accounts.StatusStandby || acct.Client != nil {
			continue
		}
		_ = acct.InitClient()
	}
	stopRebalancer := accountPool.StartRebalancer(time.Minute)
	healthTracker := health.NewTracker()

	var kamiWorker *provider.KamiRenewWorker

	// 启动健康检查（每 10 分钟续期过期 token）
	renewFn := func(acct *accounts.Account) bool {
		if acct.RefreshToken != "" {
			return exchangeRefreshToken(acct)
		}
		if acct.SessionToken != "" {
			return exchangeSessionToken(acct)
		}
		if cfg.KamiAutoRenew && kamiWorker != nil && acct.Password != "" && acct.TOTPSecret != "" {
			kamiWorker.Enqueue(acct, "health_check")
		}
		return false
	}
	stopHealthCheck := accountPool.StartHealthCheck(10*time.Minute, renewFn)
	healthTracker.RegisterService(
		"account_health_check",
		"账号健康巡检",
		"scheduler",
		(10 * time.Minute).String(),
		"每 10 分钟恢复冷却账号并主动续期即将过期 Token",
		true,
		true,
	)
	stopTempGC := accountPool.StartTempAccountGC(10*time.Minute, 1*time.Minute)
	healthTracker.RegisterService(
		"temp_account_gc",
		"临时账号清理 GC",
		"scheduler",
		(1 * time.Minute).String(),
		"每 1 分钟清理空闲超过 10 分钟的外部透传临时账号",
		true,
		true,
	)

	// 初始化多供应商插件中心
	chatgptPlugin := provider.NewChatGPTProvider(accountPool, proxyPool, &cfg, renewFn)
	accountPool.SetOnBanned(func(acct *accounts.Account, reason string) {
		if acct.Email != "" {
			_ = accounts.AppendBannedToFile("banned_accounts.txt", acct.Email, reason)
		}
		if acct.ChatGPTAccountID != "" {
			_ = accounts.AppendBannedToFile("banned_accounts.txt", acct.ChatGPTAccountID, reason)
		}
		_ = chatgptPlugin.PersistAccessTokens()
	})
	if len(bannedMap) > 0 {
		_ = chatgptPlugin.PersistAccessTokens()
	}
	reg := provider.NewRegistry()
	provider.RegisterDefaultProviders(reg, chatgptPlugin)
	provider.ApplyConfig(reg, &cfg)

	// 注册路由与生产级号池运维组件
	defaultProxy := cfg.ProxyURL
	if defaultProxy == "" {
		defaultProxy = cfg.HTTPProxy
	}
	if defaultProxy == "" && len(proxies) > 0 {
		defaultProxy = proxies[0]
	}
	auditLog := audit.NewRingBuffer(1000, cfg.AuditLogFile, cfg.AuditLogMaxMB)
	kamiWorker = provider.NewKamiRenewWorker(accountPool, defaultProxy, cfg.KamiHeadless, chatgptPlugin.PersistAccessTokens, auditLog)
	chatgptPlugin.SetAuditLog(auditLog)
	chatgptPlugin.SetKamiWorker(kamiWorker)
	stopKamiScan := func() {}
	if cfg.KamiAutoRenew {
		kamiWorker.Start()
		healthTracker.RegisterService(
			"kami_renew_worker",
			"卡密自动续期 Worker",
			"worker",
			"",
			"串行无头浏览器续期消费者已就绪",
			true,
			true,
		)
		scanStop := make(chan struct{})
		stopKamiScan = func() { close(scanStop) }
		go func() {
			firstScan := time.NewTimer(15 * time.Second)
			ticker := time.NewTicker(10 * time.Minute)
			defer firstScan.Stop()
			defer ticker.Stop()
			for {
				select {
				case <-scanStop:
					return
				case <-firstScan.C:
					kamiWorker.EnqueueDue(24 * time.Hour)
					healthTracker.MarkTick("kami_due_scan", "已完成到期卡密扫描入队")
				case <-ticker.C:
					kamiWorker.EnqueueDue(24 * time.Hour)
					healthTracker.MarkTick("kami_due_scan", "已完成定时到期卡密扫描")
				}
			}
		}()
		healthTracker.RegisterService(
			"kami_due_scan",
			"卡密到期扫描调度",
			"scheduler",
			(10 * time.Minute).String(),
			"每 10 分钟扫描 24 小时内到期的卡密账号",
			true,
			true,
		)
	} else {
		healthTracker.RegisterService(
			"kami_renew_worker",
			"卡密自动续期 Worker",
			"worker",
			"",
			"已禁用（KAMI_AUTO_RENEW=false）",
			false,
			false,
		)
		healthTracker.RegisterService(
			"kami_due_scan",
			"卡密到期扫描调度",
			"scheduler",
			(10 * time.Minute).String(),
			"已禁用（KAMI_AUTO_RENEW=false）",
			false,
			false,
		)
	}
	// 初始化持久化数据库与文件存储引擎（严格零 CGO，优雅降级；支持七牛云两级分层缓存与冷启动自动恢复）
	var qiniuClient *storage.QiniuClient
	if cfg.QiniuAccessKey != "" && cfg.QiniuSecretKey != "" && cfg.QiniuBucket != "" {
		qiniuClient = storage.NewQiniuClient(storage.QiniuConfig{
			AccessKey: cfg.QiniuAccessKey,
			SecretKey: cfg.QiniuSecretKey,
			Bucket:    cfg.QiniuBucket,
			Domain:    cfg.QiniuDomain,
			Region:    cfg.QiniuRegion,
			UploadURL: cfg.QiniuUploadURL,
		})
		storage.AutoRestoreDBIfMissing(cfg.DatabaseURL, qiniuClient)
	}

	database, dbErr := db.InitDB(cfg.DatabaseURL)
	if dbErr != nil {
		log.Printf("[Bootstrap] 严重警告: 数据库初始化失败: %v", dbErr)
	}
	if database != nil {
		if grokAny, ok := reg.Get("grok"); ok {
			if grokProvider, ok := grokAny.(*provider.GrokProvider); ok {
				grokProvider.SetGrokCredentialPatternDB(database)
			}
		}
	}
	var storageEngine storage.StorageEngine
	var storageErr error
	localStorage, storageErr := storage.NewLocalStorageEngine(cfg.StorageDir)
	if storageErr != nil {
		storageEngine = storage.NewMemoryStorageEngine()
	} else if qiniuClient != nil {
		storageEngine = storage.NewTieredStorageEngine(localStorage, qiniuClient)
	} else {
		storageEngine = localStorage
	}

	voiceStore := provider.NewVoiceCatalogStore(storageEngine)
	if cachedVoices, _, err := voiceStore.LoadCatalog(context.Background()); err == nil && len(cachedVoices) > 0 {
		if edgeP, ok := reg.Get("edge_tts"); ok {
			if edgeTTS, ok := edgeP.(*provider.EdgeTTSProvider); ok {
				edgeTTS.SeedCachedVoices(cachedVoices)
			}
		}
	}

	rtForPlaza := &handler.GatewayRuntime{
		AccountPool:   accountPool,
		ProxyPool:     proxyPool,
		KamiWorker:    kamiWorker,
		AuditLog:      auditLog,
		Config:        &cfg,
		DB:            database,
		StorageEngine: storageEngine,
	}
	plazaMgr := promptplaza.NewManager(promptplaza.Options{
		PersistFile:  cfg.PromptPlazaFile,
		ProxyFunc:    proxyPool.Allocate,
		ChatExecutor: handler.NewPoolChatExecutor(rtForPlaza, accountPool, &cfg, reg),
	})
	promptSyncInterval := time.Duration(cfg.PromptSyncIntervalHours) * time.Hour
	stopPromptSync := plazaMgr.StartScheduler(promptSyncInterval)
	healthTracker.RegisterService(
		"prompt_plaza_sync",
		"提示词广场热榜同步",
		"scheduler",
		promptSyncInterval.String(),
		func() string {
			if cfg.PromptSyncIntervalHours > 0 {
				return "后台周期性拉取多源提示词与预览图"
			}
			return "已禁用（PROMPT_SYNC_INTERVAL_HOURS<=0）"
		}(),
		cfg.PromptSyncIntervalHours > 0,
		cfg.PromptSyncIntervalHours > 0,
	)

	router := handler.RegisterRouterWithDeps(handler.RouterDeps{
		AccountPool:   accountPool,
		ProxyPool:     proxyPool,
		Registry:      reg,
		Config:        &cfg,
		KamiWorker:    kamiWorker,
		AuditLog:      auditLog,
		OpsLog:        opsLog,
		PromptPlaza:   plazaMgr,
		DB:            database,
		StorageEngine: storageEngine,
		HealthTracker: healthTracker,
	})

	return &App{
		Router:        router,
		Config:        &cfg,
		AccountPool:   accountPool,
		HealthTracker: healthTracker,
		Cleanup: func() {
			stopHealthCheck()
			stopRebalancer()
			healthTracker.MarkRunning("account_health_check", false, "已停止")
			stopTempGC()
			healthTracker.MarkRunning("temp_account_gc", false, "已停止")
			stopKamiScan()
			if cfg.KamiAutoRenew {
				healthTracker.MarkRunning("kami_due_scan", false, "已停止")
				healthTracker.MarkRunning("kami_renew_worker", false, "已停止")
			}
			stopPromptSync()
			if cfg.PromptSyncIntervalHours > 0 {
				healthTracker.MarkRunning("prompt_plaza_sync", false, "已停止")
			}
			kamiWorker.Stop()
		},
	}, nil
}

// exchangeRefreshToken 用 refresh_token 换 access_token，使用账号自身的 Client（已绑定代理）
// applyPoolPolicy 读取「供应商 + 账号类型」热池阈值并注入号池。
func applyPoolPolicy(pool *accounts.Pool, providerID string, t accounts.AccountType) {
	pc := config.LoadPoolPolicy(providerID, t.String())
	pool.SetPolicy(t, accounts.PoolPolicy{
		ActiveTarget:   pc.ActiveTarget,
		MinHealthy:     pc.MinHealthy,
		MaxActive:      pc.MaxActive,
		RotateInterval: pc.RotateInterval,
		RotateRatio:    pc.RotateRatio,
		ActivateBatch:  pc.ActivateBatch,
	})
	if pc.ActiveTarget > 0 {
		log.Printf("[pool] %s/%s hot-pool policy: target=%d min_healthy=%d max_active=%d rotate=%s",
			providerID, t.String(), pc.ActiveTarget, pc.MinHealthy, pc.MaxActive, pc.RotateInterval)
	}
}

func exchangeRefreshToken(acct *accounts.Account) bool {
	if acct.RefreshToken == "" {
		return false
	}
	if acct.Client == nil {
		_ = acct.InitClient()
	}
	result, _, err := chatgpt.GETTokenForRefreshToken(acct.Client, acct.RefreshToken, "")
	if err != nil {
		acct.LastError = err.Error()
		if chatgpt.LooksLikeAccountBanned(0, err.Error()) {
			acct.Status = accounts.StatusBanned
		}
		return false
	}
	// 优先尝试 struct 形式 (*OpenAIAccessTokenWithSession 等)
	if r, ok := result.(interface{ GetAccessToken() string }); ok {
		if at := r.GetAccessToken(); at != "" {
			acct.Token = at
			acct.LastError = ""
			return true
		}
	}
	// fallback: map[string]interface{} (旧 API 直接返回 map)
	if data, ok := result.(map[string]interface{}); ok {
		if errVal, _ := data["error"].(string); errVal != "" {
			desc, _ := data["error_description"].(string)
			fullErr := errVal
			if desc != "" {
				fullErr = fmt.Sprintf("%s: %s", errVal, desc)
			}
			acct.LastError = fullErr
			if chatgpt.LooksLikeAccountBanned(0, fullErr) {
				acct.Status = accounts.StatusBanned
			}
			return false
		}
		if accessToken, ok := data["access_token"].(string); ok && accessToken != "" {
			acct.Token = accessToken
			acct.LastError = ""
			return true
		}
	}
	return false
}

// exchangeSessionToken 用 session_token 换 access_token。
// 必须全程使用普通 std client：指纹 TLS client 请求 /api/auth/session 时，
// 即使响应被 Cloudflare 拦截无法解析，OpenAI 端也已消费掉一次性轮换的会话令牌，
// 任何后续重试都会拿到匿名空会话。
// OpenAI 会话令牌为一次性轮换，成功后必须把新令牌写回文件并更新账号，
// 否则重启加载与 10 分钟健康续期都会拿已消费的旧令牌。
func exchangeSessionToken(acct *accounts.Account) bool {
	if acct.SessionToken == "" {
		return false
	}
	fetch := func(client httpclient.HttpClient) (string, string) {
		result, _, err := chatgpt.GETTokenForSessionToken(client, acct.SessionToken, acct.Proxy)
		if err != nil {
			return "", ""
		}
		if r, ok := result.(interface {
			GetAccessToken() string
			GetSessionToken() string
		}); ok {
			return r.GetAccessToken(), r.GetSessionToken()
		}
		if data, ok := result.(map[string]interface{}); ok {
			accessToken, _ := data["accessToken"].(string)
			sessionToken, _ := data["session_token"].(string)
			return accessToken, sessionToken
		}
		return "", ""
	}
	accessToken, rotated := fetch(bogdanfinn.NewStdClient())
	if accessToken == "" {
		return false
	}
	acct.Token = accessToken
	if rotated != "" && rotated != acct.SessionToken {
		oldToken := acct.SessionToken
		acct.SessionToken = rotated
		persistRotatedSessionToken(oldToken, rotated, acct.TeamUserID)
	}
	return true
}

// sessionTokenFileMu 保护 session_tokens.txt 的读-改-写，避免并发续期互相覆盖。
var sessionTokenFileMu sync.Mutex

// persistRotatedSessionToken 将轮换得到的新 session token 写回 session_tokens.txt，
// 只替换旧令牌所在行，保留其它账号的行；文件不存在时直接写入。
// 写入经临时文件 + rename，避免中途失败把整个凭据文件截断。
func persistRotatedSessionToken(oldToken, newToken, teamID string) {
	const path = "session_tokens.txt"

	value := newToken
	if teamID != "" {
		value += ":" + teamID
	}

	sessionTokenFileMu.Lock()
	defer sessionTokenFileMu.Unlock()

	data, err := os.ReadFile(path)
	if err != nil {
		writeFileAtomic(path, []byte(value+"\n"))
		return
	}
	lines := strings.Split(strings.TrimRight(string(data), "\r\n"), "\n")
	replaced := false
	for i, line := range lines {
		// 与 accounts.LoadTokensFromFile 一致：token 在 ":" 前，其后为 team_id
		if strings.TrimSpace(strings.SplitN(strings.TrimSpace(line), ":", 2)[0]) == oldToken {
			lines[i] = value
			replaced = true
			break
		}
	}
	if !replaced {
		lines = append(lines, value)
	}
	writeFileAtomic(path, []byte(strings.Join(lines, "\n")+"\n"))
}

// writeFileAtomic 先写同目录临时文件再 rename，保证凭据文件不会被写成半截。
func writeFileAtomic(path string, data []byte) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp")
	if err != nil {
		_ = os.WriteFile(path, data, 0644)
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return
	}
	// Windows 下 rename 到已存在的文件需要先移除目标
	if runtime.GOOS == "windows" {
		_ = os.Remove(path)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		_ = os.WriteFile(path, data, 0644)
	}
}

// loadProxyList 从 proxies.txt / PROXY_URL / http_proxy 加载代理列表
func loadProxyList() []string {
	var proxies []string
	proxyUrl := os.Getenv("PROXY_URL")
	if proxyUrl != "" {
		proxies = append(proxies, proxyUrl)
	}

	if _, err := os.Stat("proxies.txt"); err == nil {
		data, err := os.ReadFile("proxies.txt")
		if err == nil {
			lines := string(data)
			for _, line := range strings.Split(lines, "\n") {
				line = strings.TrimSpace(line)
				if line != "" {
					proxies = append(proxies, line)
				}
			}
		}
	}

	if len(proxies) == 0 {
		proxy := os.Getenv("http_proxy")
		if proxy != "" {
			proxies = append(proxies, proxy)
		}
	}

	return proxies
}

func loadDotEnv() {
	content, err := os.ReadFile(".env")
	if err != nil {
		return
	}
	content = bytes.TrimPrefix(content, []byte("\xef\xbb\xbf"))
	envMap, err := godotenv.Unmarshal(string(content))
	if err != nil {
		return
	}
	for k, v := range envMap {
		if os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}
}
