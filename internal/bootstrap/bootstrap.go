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
	"ai-web-free/internal/deals"
	"ai-web-free/internal/gatewaykeys"
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
	"gorm.io/gorm"
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

	// ─── 初始化持久化数据库与物化同步模式判断 ──────────────────────
	materializeActive := cfg.DBMaterializeEnabled && db.IsRemoteDSN(cfg.DatabaseURL)
	primaryDSN := cfg.DatabaseURL
	if materializeActive {
		primaryDSN = cfg.DBLocalPath
		if strings.TrimSpace(primaryDSN) == "" {
			primaryDSN = "data/ai-web-free.db"
		}
	}

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
		storage.AutoRestoreDBIfMissing(primaryDSN, qiniuClient)
	}

	database, dbErr := db.InitDB(primaryDSN)
	if dbErr != nil {
		log.Printf("[Bootstrap] 严重警告: 数据库初始化失败: %v", dbErr)
	}
	if materializeActive && dbErr == nil {
		log.Printf("[Bootstrap] 已启用本地 SQLite 物化层 (%s)，云数据库 (%s) 将在后台异步双向同步", primaryDSN, db.MaskDSN(cfg.DatabaseURL))
	}

	activeProfile := config.NormalizeProfile(cfg.AppProfile)
	proxies, proxyMetas := loadProxyListForProfile(database, activeProfile)
	proxyPool := proxy.NewPool(proxies)
	proxyPool.SetActiveProfile(activeProfile)
	proxyPool.SetNodeMetas(proxyMetas)
	log.Printf("[Bootstrap] 当前运行环境 Profile=%s，已加载 %d 个环境专属代理节点", activeProfile, len(proxies))

	// ─── 加载账号 ────────────────────────────────────────────────

	profiles := accounts.DefaultProfiles
	var accs []*accounts.Account

	// 加载已封禁账号持久化名单（结合 SQLite 数据库与本地文件双向对齐）
	bannedMap := accounts.LoadBannedFromFile("banned_accounts.txt")
	// 加载管理员手动禁用名单（结合 SQLite 数据库与本地文件双向对齐）
	disabledMap := accounts.LoadDisabledFromFile("disabled_accounts.txt")

	if database != nil {
		dbBanned := db.LoadRestrictionsFromDB(database, "banned")
		newBanned := make(map[string]string)
		for id, reason := range bannedMap {
			if _, exists := dbBanned[id]; !exists {
				newBanned[id] = reason
			}
		}
		for id, reason := range dbBanned {
			if _, ok := bannedMap[id]; !ok {
				bannedMap[id] = reason
			}
		}
		if len(newBanned) > 0 {
			_ = db.SaveRestrictionsBatchToDB(database, newBanned, "banned")
		}

		dbDisabled := db.LoadRestrictionsFromDB(database, "disabled")
		newDisabled := make(map[string]string)
		for id, reason := range disabledMap {
			if _, exists := dbDisabled[id]; !exists {
				newDisabled[id] = reason
			}
		}
		for id, reason := range dbDisabled {
			if _, ok := disabledMap[id]; !ok {
				disabledMap[id] = reason
			}
		}
		if len(newDisabled) > 0 {
			_ = db.SaveRestrictionsBatchToDB(database, newDisabled, "disabled")
		}
	}
	// 加载当前 Profile 专属的账号粘性代理绑定关系（与本地 account_proxies.txt 合并，保证不同 Profile 切换时出口 IP 独立且不互相污染）
	accountProxyMap := accounts.LoadAccountProxiesFromFile("account_proxies.txt")
	if database != nil {
		for k, u := range db.LoadAccountProfileProxies(database, activeProfile) {
			accountProxyMap[k] = u
		}
	}
	newProfileBindings := make(map[string]string)

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
		// 若账号自身的历史 Proxy 在当前 Profile 代理池内且健康，直接复用并记录到当前 Profile 粘性表
		if acct.Proxy != "" && proxyPool.Contains(acct.Proxy) && proxyPool.IsHealthy(acct.Proxy) {
			if key != "" {
				accountProxyMap[key] = acct.Proxy
				newProfileBindings[key] = acct.Proxy
			}
			return
		}
		// 无历史绑定或原绑定节点不属于当前 Profile 时，从当前 Profile 代理池分配新节点并建立 Profile 粘性映射
		newProxy := proxyPool.Allocate()
		acct.Proxy = newProxy
		if key != "" && newProxy != "" {
			accountProxyMap[key] = newProxy
			newProfileBindings[key] = newProxy
		}
	}

	applyRestrictions := func(acct *accounts.Account) {
		chatGPTID := strings.TrimSpace(acct.ChatGPTAccountID)
		email := strings.TrimSpace(acct.Email)
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
		}
	}

	// 1. 优先从 SQLite 数据库加载已持久化号池账号
	dbAccounts, dbLoadErr := accounts.LoadAccountsFromDB(database, profiles)
	if dbLoadErr != nil {
		log.Printf("[Bootstrap] 警告: 从数据库加载号池失败: %v", dbLoadErr)
	}
	for _, acct := range dbAccounts {
		assignStickyProxy(acct)
		applyRestrictions(acct)
		accs = append(accs, acct)
	}
	if len(dbAccounts) > 0 {
		log.Printf("[Bootstrap] 从持久化数据库成功加载 %d 个号池账号", len(dbAccounts))
	}

	findExisting := func(email, chatGPTID, token string) *accounts.Account {
		emailClean := strings.ToLower(strings.TrimSpace(email))
		idClean := strings.TrimSpace(chatGPTID)
		tokenClean := strings.TrimSpace(token)
		for _, a := range accs {
			if emailClean != "" && strings.ToLower(strings.TrimSpace(a.Email)) == emailClean {
				return a
			}
			if idClean != "" && strings.TrimSpace(a.ChatGPTAccountID) == idClean {
				return a
			}
			if tokenClean != "" && (a.Token == tokenClean || a.RefreshToken == tokenClean || a.SessionToken == tokenClean) {
				return a
			}
		}
		return nil
	}

	var accountsToPersist []*accounts.Account

	// 2. access_tokens.txt — 纯 access token，不可续期
	for _, t := range accounts.LoadTokensFromFile("access_tokens.txt") {
		chatGPTID := accounts.ExtractChatGPTAccountID(t.Token)
		email := accounts.ExtractEmail(t.Token)
		if matched := findExisting(email, chatGPTID, t.Token); matched != nil {
			changed := false
			if matched.Token == "" {
				matched.Token = t.Token
				changed = true
			}
			if matched.TeamUserID == "" && t.TeamID != "" {
				matched.TeamUserID = t.TeamID
				changed = true
			}
			if matched.ChatGPTAccountID == "" && chatGPTID != "" {
				matched.ChatGPTAccountID = chatGPTID
				changed = true
			}
			if matched.Email == "" && email != "" {
				matched.Email = email
				changed = true
			}
			applyRestrictions(matched)
			if changed {
				accountsToPersist = append(accountsToPersist, matched)
			}
			continue
		}

		acct := accounts.CreateAccount(t.Token, accounts.TypeFree, profiles)
		acct.TeamUserID = t.TeamID
		acct.ChatGPTAccountID = chatGPTID
		if email != "" {
			acct.Email = email
		}
		assignStickyProxy(acct)
		applyRestrictions(acct)
		if acct.Status == accounts.StatusPending {
			acct.Status = accounts.StatusActive
		}
		accs = append(accs, acct)
		accountsToPersist = append(accountsToPersist, acct)
	}

	// 3. refresh_tokens.txt — 带 refresh_token，可续期（冷启动不发起阻塞 HTTP 请求，由后台巡检异步换取）
	for _, t := range accounts.LoadTokensFromFile("refresh_tokens.txt") {
		if matched := findExisting("", "", t.Token); matched != nil {
			if matched.RefreshToken == "" {
				matched.RefreshToken = t.Token
				accountsToPersist = append(accountsToPersist, matched)
			}
			continue
		}

		acct := accounts.CreateAccount("", accounts.TypeFree, profiles)
		acct.RefreshToken = t.Token
		acct.TeamUserID = t.TeamID
		assignStickyProxy(acct)
		applyRestrictions(acct)
		if acct.Status == accounts.StatusPending {
			acct.Status = accounts.StatusExpired
			acct.LastError = "待首次刷新 Access Token"
		}
		accs = append(accs, acct)
		accountsToPersist = append(accountsToPersist, acct)
	}

	// 4. session_tokens.txt — ChatGPT session token，用于免费账号续期（冷启动不发起阻塞 HTTP 请求，由后台巡检异步换取）
	for _, t := range accounts.LoadTokensFromFile("session_tokens.txt") {
		if matched := findExisting("", "", t.Token); matched != nil {
			if matched.SessionToken == "" {
				matched.SessionToken = t.Token
				accountsToPersist = append(accountsToPersist, matched)
			}
			continue
		}

		acct := accounts.CreateAccount("", accounts.TypeFree, profiles)
		acct.SessionToken = t.Token
		acct.TeamUserID = t.TeamID
		assignStickyProxy(acct)
		applyRestrictions(acct)
		if acct.Status == accounts.StatusPending {
			acct.Status = accounts.StatusExpired
			acct.LastError = "待首次刷新 Access Token"
		}
		accs = append(accs, acct)
		accountsToPersist = append(accountsToPersist, acct)
	}

	// 5. free_tokens.txt — 设备 UUID
	for _, t := range accounts.LoadTokensFromFile("free_tokens.txt") {
		if matched := findExisting("", "", t.Token); matched != nil {
			continue
		}

		acct := accounts.CreateAccount(t.Token, accounts.TypeNoAuth, profiles)
		assignStickyProxy(acct)
		acct.Status = accounts.StatusActive
		accs = append(accs, acct)
		accountsToPersist = append(accountsToPersist, acct)
	}

	// 6. kami_accounts.txt — 卡密账号（邮箱----密码----2FA密钥）
	for _, k := range accounts.LoadKamiFromFile("kami_accounts.txt") {
		if matched := findExisting(k.Email, "", ""); matched != nil {
			changed := false
			if matched.Password != k.Password {
				matched.Password = k.Password
				changed = true
			}
			if matched.TOTPSecret != k.TOTPSecret {
				matched.TOTPSecret = k.TOTPSecret
				changed = true
			}
			applyRestrictions(matched)
			if changed {
				accountsToPersist = append(accountsToPersist, matched)
			}
			continue
		}

		acct := accounts.CreateAccount("", accounts.TypeFree, profiles)
		acct.Email = k.Email
		acct.Password = k.Password
		acct.TOTPSecret = k.TOTPSecret
		assignStickyProxy(acct)
		applyRestrictions(acct)
		if acct.Status == accounts.StatusPending {
			acct.LastError = "待浏览器登录提取 Token"
		}
		accs = append(accs, acct)
		accountsToPersist = append(accountsToPersist, acct)
	}

	// 7. FREE_ACCOUNTS — 自动生成 UUID 账号
	if cfg.FreeAccounts {
		for i := 0; i < cfg.FreeAccountsNum; i++ {
			uid := uuid.NewString()
			acct := accounts.CreateAccount(uid, accounts.TypeNoAuth, profiles)
			acct.Proxy = proxyPool.Allocate()
			acct.Status = accounts.StatusActive
			accs = append(accs, acct)
			accountsToPersist = append(accountsToPersist, acct)
		}
	}

	// 9. 仅将新增或更新的账号持久化增量写入 SQLite 数据库（避免启动全量覆写已有账号产生大量 SQL 往返）
	if database != nil && len(accountsToPersist) > 0 {
		if err := accounts.BatchSaveAccountsToDB(database, accountsToPersist); err != nil {
			log.Printf("[Bootstrap] 同步新增账号至持久化数据库失败: %v", err)
		} else {
			log.Printf("[Bootstrap] 已成功将 %d 个新增/更新号池账号同步持久化至 SQLite 数据库", len(accountsToPersist))
		}
	}
	if database != nil && len(newProfileBindings) > 0 {
		_ = db.SaveAccountProfileProxiesBatch(database, activeProfile, newProfileBindings)
	}

	accountPool := accounts.NewPool(accs)

	// 热池/待命池：按供应商 + 账号类型读取阈值（POOL_CHATGPT_FREE_ACTIVE_TARGET 等），0 表示不限制
	applyPoolPolicy(accountPool, "chatgpt", accounts.TypeFree)
	applyPoolPolicy(accountPool, "chatgpt", accounts.TypePUID)
	accountPool.Rebalance()

	// 启动阶段不再全量同步初始化 TLS Client，改由号池 Acquire 借出或热池轮换激活时按需懒加载（Lazy Init），消除 CPU 与内存冷启动停顿
	stopRebalancer := accountPool.StartRebalancer(time.Minute)
	healthTracker := health.NewTracker()

	var kamiWorker *provider.KamiRenewWorker

	// 启动健康检查（每 10 分钟续期过期 token）
	renewFn := func(acct *accounts.Account) bool {
		if acct.RefreshToken != "" {
			ok := exchangeRefreshToken(acct)
			if ok {
				_ = accounts.SaveAccountToDB(database, acct)
			}
			return ok
		}
		if acct.SessionToken != "" {
			ok := exchangeSessionToken(acct)
			if ok {
				_ = accounts.SaveAccountToDB(database, acct)
			}
			return ok
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
		if database != nil {
			if acct.Email != "" {
				_ = db.SaveRestrictionToDB(database, acct.Email, "banned", reason)
			}
			if acct.ChatGPTAccountID != "" {
				_ = db.SaveRestrictionToDB(database, acct.ChatGPTAccountID, "banned", reason)
			}
		}
		_ = chatgptPlugin.PersistAccessTokens()
	})
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
	if database != nil {
		if grokAny, ok := reg.Get("grok"); ok {
			if grokProvider, ok := grokAny.(*provider.GrokProvider); ok {
				grokProvider.SetGrokCredentialPatternDB(database)
			}
		}

		// 启动 AI 羊毛与福利资讯 4 小时周期性定时抓取调度服务
		dealsService := deals.NewService(database, 4*time.Hour)
		deals.SetService(dealsService)
		dealsService.Start()
		healthTracker.RegisterService(
			"deals_crawler_scheduler",
			"AI 羊毛与福利资讯调度器",
			"scheduler",
			(4 * time.Hour).String(),
			"每 4 小时自动抓取并重排最热 AI 羊毛与免费额度线报",
			true,
			true,
		)
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

	stopClashSync := func() {}
	clashSyncEnabled := strings.TrimSpace(cfg.ClashSubURL) != "" && cfg.ClashSyncIntervalHours > 0
	if clashSyncEnabled {
		clashStop := make(chan struct{})
		stopClashSync = func() { close(clashStop) }
		clashInterval := time.Duration(cfg.ClashSyncIntervalHours) * time.Hour
		runClashSyncOnce := func() {
			targetProf := db.NormalizeProfileName(cfg.ClashSubProfile)
			existingPorts := make(map[string]int)
			if database != nil {
				if existing, err := db.LoadProxyRecordsByProfile(database, targetProf); err == nil {
					for _, r := range existing {
						if r.Source == "clash_sub" && r.NodeName != "" && r.Port > 0 {
							existingPorts[r.NodeName] = r.Port
						}
					}
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			res, err := proxy.SyncClashSubscription(ctx, proxy.ClashSyncOptions{
				SubURL:          cfg.ClashSubURL,
				TargetProfile:   targetProf,
				ListenHost:      cfg.ClashMultiportHost,
				StartPort:       cfg.ClashMultiportStart,
				MaxPorts:        cfg.ClashMultiportMax,
				ExcludeRegex:    cfg.ClashExcludeRegex,
				PriorityRegions: cfg.ClashPriorityRegions,
				ConfigPath:      cfg.MihomoConfigPath,
				MihomoAPIURL:    cfg.MihomoAPIURL,
				MihomoAPISecret: cfg.MihomoAPISecret,
				WriteConfig:     true,
				ReloadMihomo:    true,
				ExistingPorts:   existingPorts,
			})
			if err != nil {
				log.Printf("[ClashSync] 自动同步机场订阅失败: %v", err)
				healthTracker.MarkTick("clash_multiport_sync", "同步失败: "+err.Error())
				return
			}
			clashRecords := make([]db.ProxyNodeRecord, 0, len(res.MappedNodes))
			for _, item := range res.MappedNodes {
				clashRecords = append(clashRecords, db.ProxyNodeRecord{
					Profile:  targetProf,
					URL:      item.ProxyURL,
					NodeName: item.NodeName,
					Region:   item.Region,
					Source:   "clash_sub",
					Port:     item.Port,
					Protocol: "http",
					Status:   "active",
				})
			}
			if database != nil {
				_ = db.ReplaceClashProxiesByProfile(database, targetProf, clashRecords)
			}
			if proxyPool.ActiveProfile() == targetProf {
				records, _ := db.LoadProxyRecordsByProfile(database, targetProf)
				urls := make([]string, 0, len(records))
				metas := make(map[string]proxy.NodeMeta, len(records))
				for _, r := range records {
					urls = append(urls, r.URL)
					metas[r.URL] = proxy.NodeMeta{
						Profile:  r.Profile,
						NodeName: r.NodeName,
						Region:   r.Region,
						Source:   r.Source,
						Port:     r.Port,
					}
				}
				proxyPool.Replace(urls)
				proxyPool.SetNodeMetas(metas)
				accountPool.ReconcileProxies(
					func(u string) bool { return !proxyPool.Contains(u) || !proxyPool.IsHealthy(u) },
					func(excl map[string]bool) string { return proxyPool.AllocateHealthy(excl) },
				)
			}
			detail := fmt.Sprintf("已同步 %d 个多端口节点 (%d..%d) 至 Profile %s", res.MappedCount, res.StartPort, res.EndPort, targetProf)
			log.Printf("[ClashSync] %s", detail)
			healthTracker.MarkTick("clash_multiport_sync", detail)
		}
		go func() {
			firstTimer := time.NewTimer(5 * time.Second)
			ticker := time.NewTicker(clashInterval)
			defer firstTimer.Stop()
			defer ticker.Stop()
			for {
				select {
				case <-clashStop:
					return
				case <-firstTimer.C:
					runClashSyncOnce()
				case <-ticker.C:
					runClashSyncOnce()
				}
			}
		}()
		healthTracker.RegisterService(
			"clash_multiport_sync",
			"Clash 订阅多端口同步器",
			"scheduler",
			clashInterval.String(),
			fmt.Sprintf("自动拉取机场订阅并映射端口 %d..%d 至 Profile %s", cfg.ClashMultiportStart, cfg.ClashMultiportStart+cfg.ClashMultiportMax-1, cfg.ClashSubProfile),
			true,
			true,
		)
	} else {
		healthTracker.RegisterService(
			"clash_multiport_sync",
			"Clash 订阅多端口同步器",
			"scheduler",
			"",
			"未启用自动定时同步（未设置 CLASH_SUB_URL 或 CLASH_SYNC_INTERVAL_HOURS<=0，支持管理台手动触发）",
			false,
			false,
		)
	}

	gwSecret := strings.TrimSpace(os.Getenv("GATEWAY_KEY_SECRET"))
	if gwSecret == "" {
		gwSecret = cfg.AdminToken
	}
	gwStore, gwErr := gatewaykeys.NewStore(database, gwSecret)
	if gwErr != nil {
		log.Printf("[Gateway] API Key store init failed: %v", gwErr)
	}

	var syncEngine *db.SyncEngine
	if materializeActive && database != nil {
		syncCfg := db.SyncConfig{
			Enabled:      true,
			LocalPath:    primaryDSN,
			PushInterval: cfg.DBSyncPushInterval,
			PullInterval: cfg.DBSyncPullInterval,
			BatchSize:    cfg.DBSyncBatchSize,
			Timeout:      cfg.DBSyncTimeout,
			OnRemoteChanged: func(changedTables map[string]int) {
				curProf := proxyPool.ActiveProfile()
				if changedTables["proxy_node_records"] > 0 {
					if nextRecords, err := db.LoadProxyRecordsByProfile(database, curProf); err == nil {
						nextProxies := make([]string, 0, len(nextRecords))
						nextMetas := make(map[string]proxy.NodeMeta, len(nextRecords))
						for _, r := range nextRecords {
							u := strings.TrimSpace(r.URL)
							if u == "" {
								continue
							}
							nextProxies = append(nextProxies, u)
							nextMetas[u] = proxy.NodeMeta{
								Profile:  r.Profile,
								NodeName: r.NodeName,
								Region:   r.Region,
								Source:   r.Source,
								Port:     r.Port,
							}
						}
						proxyPool.Replace(nextProxies)
						proxyPool.SetNodeMetas(nextMetas)
					}
				}
				if changedTables["pool_account_records"] > 0 || changedTables["account_restriction_records"] > 0 || changedTables["account_profile_proxies"] > 0 {
					if reloaded, err := accounts.LoadAccountsFromDB(database, profiles); err == nil {
						latestBanned := db.LoadRestrictionsFromDB(database, "banned")
						latestDisabled := db.LoadRestrictionsFromDB(database, "disabled")
						profProxies := db.LoadAccountProfileProxies(database, curProf)
						for _, acct := range reloaded {
							keyID := strings.ToLower(strings.TrimSpace(acct.ChatGPTAccountID))
							keyEmail := strings.ToLower(strings.TrimSpace(acct.Email))
							stickyKey := keyEmail
							if stickyKey == "" {
								stickyKey = keyID
							}
							if stickyKey == "" {
								stickyKey = acct.ID
							}
							if pURL, ok := profProxies[stickyKey]; ok && proxyPool.Contains(pURL) && proxyPool.IsHealthy(pURL) {
								acct.Proxy = pURL
							} else if acct.Proxy == "" || !proxyPool.Contains(acct.Proxy) {
								acct.Proxy = proxyPool.Allocate()
							}
							banReason := ""
							disableReason := ""
							if keyID != "" {
								banReason = latestBanned[keyID]
								disableReason = latestDisabled[keyID]
							}
							if banReason == "" && keyEmail != "" {
								banReason = latestBanned[keyEmail]
							}
							if disableReason == "" && keyEmail != "" {
								disableReason = latestDisabled[keyEmail]
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
							}
						}
						accountPool.SyncPersistentAccounts(reloaded)
					}
				}
				if changedTables["gateway_groups"] > 0 || changedTables["gateway_keys"] > 0 {
					if gwStore != nil {
						_ = gwStore.ReloadFromDB()
					}
				}
			},
			OnStatusChange: func(st db.SyncStatus) {
				detail := fmt.Sprintf("状态 %s · Outbox %d 条 · 推送 %d / 拉取 %d", st.State, st.OutboxPending, st.TotalPushed, st.TotalPulled)
				if st.LastError != "" {
					detail = fmt.Sprintf("降级重试中 (失败 %d 次): %s", st.ConsecutiveFailures, st.LastError)
				}
				healthTracker.MarkTick("db_materialized_sync", detail)
			},
		}
		syncEngine = db.NewSyncEngine(database, cfg.DatabaseURL, syncCfg)
		tables := append(db.DefaultSyncTables(),
			db.NewTypedSyncTable[gatewaykeys.Group](),
			db.NewTypedSyncTable[gatewaykeys.Key](),
		)
		syncEngine.RegisterTables(tables...)
		if err := syncEngine.PrepareLocal(); err != nil {
			log.Printf("[DB Sync] 本地 SQLite 同步触发器初始化失败: %v", err)
		} else {
			db.SetSyncEngine(syncEngine)
			syncEngine.Start()
			healthTracker.RegisterService(
				"db_materialized_sync",
				"数据库物化同步引擎",
				"worker",
				cfg.DBSyncPushInterval.String(),
				fmt.Sprintf("本地 SQLite (%s) <-> %s 异步双向同步中", primaryDSN, db.MaskDSN(cfg.DatabaseURL)),
				true,
				true,
			)
		}
	} else {
		db.SetSyncEngine(nil)
		disabledReason := "已禁用（DB_MATERIALIZE_ENABLED=false）"
		if cfg.DBMaterializeEnabled && !db.IsRemoteDSN(cfg.DatabaseURL) {
			disabledReason = "未启用（当前 DATABASE_URL 为本地 SQLite，无需额外物化）"
		}
		healthTracker.RegisterService(
			"db_materialized_sync",
			"数据库物化同步引擎",
			"worker",
			"",
			disabledReason,
			false,
			false,
		)
	}

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
		SyncEngine:    syncEngine,
		GatewayKeys:   gwStore,
		StorageEngine: storageEngine,
		HealthTracker: healthTracker,
	})

	return &App{
		Router:        router,
		Config:        &cfg,
		AccountPool:   accountPool,
		HealthTracker: healthTracker,
		Cleanup: func() {
			if syncEngine != nil {
				syncEngine.Stop(5 * time.Second)
				healthTracker.MarkRunning("db_materialized_sync", false, "已完成停机刷盘并停止")
			}
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
			stopClashSync()
			if clashSyncEnabled {
				healthTracker.MarkRunning("clash_multiport_sync", false, "已停止")
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
		if db.GetDB() != nil {
			_ = accounts.SaveAccountToDB(db.GetDB(), acct)
		} else {
			persistRotatedSessionToken(oldToken, rotated, acct.TeamUserID)
		}
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

// loadProxyListForProfile 按指定 Runtime Profile 从数据库 / proxies.txt / PROXY_URL / http_proxy 加载代理列表并双向对齐
func loadProxyListForProfile(database *gorm.DB, profile string) ([]string, map[string]proxy.NodeMeta) {
	normProfile := db.NormalizeProfileName(profile)
	var proxies []string
	seen := make(map[string]bool)
	metas := make(map[string]proxy.NodeMeta)

	addProxy := func(raw string, meta proxy.NodeMeta) {
		clean := strings.TrimSpace(raw)
		if clean == "" || strings.HasPrefix(clean, "#") || seen[clean] {
			return
		}
		seen[clean] = true
		proxies = append(proxies, clean)
		if meta.Profile == "" {
			meta.Profile = normProfile
		}
		metas[clean] = meta
	}

	// 1. 优先从数据库读取当前 Profile 的代理节点
	var dbProxyCount int
	if database != nil {
		if dbRecords, err := db.LoadProxyRecordsByProfile(database, normProfile); err == nil {
			dbProxyCount = len(dbRecords)
			for _, r := range dbRecords {
				addProxy(r.URL, proxy.NodeMeta{
					Profile:  r.Profile,
					NodeName: r.NodeName,
					Region:   r.Region,
					Source:   r.Source,
					Port:     r.Port,
				})
			}
		}
	}

	// 2. 合并 PROXY_URL 环境变量
	if p := os.Getenv("PROXY_URL"); p != "" {
		addProxy(p, proxy.NodeMeta{Profile: normProfile, Source: "env_seed"})
	}

	// 3. 合并 proxies.txt（仅在当前 Profile 数据库尚无专属节点或为默认 home Profile 时导入，避免把本地 proxies.txt 混入其他 Profile）
	if dbProxyCount == 0 || normProfile == "home" {
		if _, err := os.Stat("proxies.txt"); err == nil {
			if data, err := os.ReadFile("proxies.txt"); err == nil {
				for _, line := range strings.Split(string(data), "\n") {
					addProxy(line, proxy.NodeMeta{Profile: normProfile, Source: "manual"})
				}
			}
		}
	}

	// 4. 合并 http_proxy 环境变量
	if len(proxies) == 0 {
		if p := os.Getenv("http_proxy"); p != "" {
			addProxy(p, proxy.NodeMeta{Profile: normProfile, Source: "env_seed"})
		}
	}

	// 5. 仅在有新增代理节点时持久化至数据库当前 Profile（无数据库时才回退至 proxies.txt）
	if database != nil && len(proxies) > dbProxyCount {
		_ = db.SaveProxiesByProfile(database, normProfile, proxies)
	} else if database == nil && len(proxies) > 0 {
		content := strings.Join(proxies, "\n") + "\n"
		_ = os.WriteFile("proxies.txt", []byte(content), 0644)
	}

	return proxies, metas
}

func loadProxyList(database *gorm.DB) []string {
	list, _ := loadProxyListForProfile(database, "home")
	return list
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
