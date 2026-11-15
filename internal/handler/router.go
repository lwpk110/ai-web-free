package handler

import (
	"io/fs"
	"net/http"
	"strings"

	"ai-web-free/httpclient/bogdanfinn"
	"ai-web-free/internal/accounts"
	"ai-web-free/internal/audit"
	"ai-web-free/internal/chatgpt"
	"ai-web-free/internal/config"
	"ai-web-free/internal/opslog"
	"ai-web-free/internal/promptplaza"
	"ai-web-free/internal/provider"
	"ai-web-free/internal/proxy"
	"ai-web-free/internal/storage"
	"ai-web-free/internal/vectorstore"
	"ai-web-free/middlewares"
	"ai-web-free/web"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func optionsHandler(c *gin.Context) {
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Methods", "POST")
	c.Header("Access-Control-Allow-Headers", "*")
	c.JSON(200, gin.H{
		"message": "pong",
	})
}

// RouterDeps 聚合 RegisterRouterWithDeps 所需的运行时依赖。
type RouterDeps struct {
	AccountPool   *accounts.Pool
	ProxyPool     *proxy.Pool
	Registry      *provider.Registry
	Config        *config.Config
	KamiWorker    *provider.KamiRenewWorker
	AuditLog      *audit.RingBuffer
	OpsLog        *opslog.Buffer
	PromptPlaza   *promptplaza.Manager
	DB            *gorm.DB
	StorageEngine storage.StorageEngine
	VectorStore   vectorstore.VectorStore
}

func RegisterRouter(accountPool *accounts.Pool, proxyPool *proxy.Pool, registry *provider.Registry, cfg *config.Config) *gin.Engine {
	return RegisterRouterWithDeps(RouterDeps{
		AccountPool: accountPool,
		ProxyPool:   proxyPool,
		Registry:    registry,
		Config:      cfg,
	})
}

func RegisterRouterWithDeps(deps RouterDeps) *gin.Engine {
	accountPool := deps.AccountPool
	proxyPool := deps.ProxyPool
	registry := deps.Registry
	cfg := deps.Config

	opsLogBuf := deps.OpsLog
	if opsLogBuf == nil {
		opsLogBuf = opslog.Global()
	}

	rt := &GatewayRuntime{
		AccountPool:   accountPool,
		ProxyPool:     proxyPool,
		KamiWorker:    deps.KamiWorker,
		AuditLog:      deps.AuditLog,
		OpsLog:        opsLogBuf,
		PromptPlaza:   deps.PromptPlaza,
		Config:        cfg,
		DB:            deps.DB,
		StorageEngine: deps.StorageEngine,
		VectorStore:   deps.VectorStore,
	}
	if rt.VectorStore == nil {
		rt.VectorStore = vectorstore.NewMemoryVectorStore()
	}
	if rt.PromptPlaza == nil {
		var proxyFn promptplaza.ProxyFunc
		if proxyPool != nil {
			proxyFn = proxyPool.Allocate
		}
		var persistFile string
		if cfg != nil {
			persistFile = cfg.PromptPlazaFile
		}
		rt.PromptPlaza = promptplaza.NewManager(promptplaza.Options{
			PersistFile:  persistFile,
			ProxyFunc:    proxyFn,
			ChatExecutor: NewPoolChatExecutor(rt, accountPool, cfg),
		})
	}
	chatHandler := NewChatHandler(accountPool, cfg)
	imageHandler := NewImageHandler(accountPool, cfg)
	audioHandler := NewAudioHandler(accountPool, cfg)
	authHandler := NewAuthHandler(accountPool)
	modelsHandler := NewModelsHandler(accountPool, cfg)
	adminHandler := NewAdminHandler(registry, proxyPool, cfg)
	galleryHandler := NewGalleryHandler(rt.DB, rt.StorageEngine)
	chatWorkbenchHandler := NewChatWorkbenchHandler(rt.DB, rt.VectorStore)
	chatHandler.SetRuntime(rt)
	imageHandler.SetRuntime(rt)
	audioHandler.SetRuntime(rt)
	adminHandler.SetRuntime(rt)
	audioHandler.SetRegistry(registry)
	imageHandler.SetRegistry(registry)
	videoHandler := NewVideoHandler(registry, deps.AuditLog)
	videoHandler.SetStorageEngine(rt.StorageEngine)
	dramaHandler := NewDramaHandler(rt.DB, accountPool, registry, rt.StorageEngine)

	// 初始化基础前置参数（DPL、BasicCookies 等）
	proxyUrl := cfg.ProxyURL
	if proxyUrl == "" {
		proxyUrl = cfg.HTTPProxy
	}
	client := bogdanfinn.NewStdClient()
	chatgpt.GetDpl(client, proxyUrl)

	router := gin.Default()
	router.Use(middlewares.Cors)
	router.Use(func(c *gin.Context) {
		c.Next()
		status := c.Writer.Status()
		if status >= 500 || len(c.Errors) > 0 {
			level := "error"
			if status < 500 {
				level = "warn"
			}
			detail := c.Errors.String()
			opsLogBuf.Record(opslog.Entry{
				Source:  "backend",
				Level:   level,
				Module:  "http",
				Message: c.Request.Method + " " + c.Request.URL.Path + " -> " + http.StatusText(status),
				Detail:  detail,
			})
		}
	})

	// ─── 内嵌 Thesys OpenUI React 构建产物 (web/dist) ─────────────
	distFS, err := fs.Sub(web.StaticFS, "dist")
	if err == nil {
		fileServer := http.FileServer(http.FS(distFS))
		serveIndex := func(c *gin.Context) {
			indexData, readErr := fs.ReadFile(distFS, "index.html")
			if readErr != nil {
				c.JSON(http.StatusOK, gin.H{"message": "Hello, world!"})
				return
			}
			c.Data(http.StatusOK, "text/html; charset=utf-8", indexData)
		}
		router.GET("/", func(c *gin.Context) {
			if strings.Contains(c.GetHeader("Accept"), "application/json") {
				c.JSON(200, gin.H{"message": "Hello, world!", "service": "ai-web-free"})
				return
			}
			serveIndex(c)
		})
		router.GET("/index.html", serveIndex)
		router.GET("/assets/*filepath", func(c *gin.Context) {
			fileServer.ServeHTTP(c.Writer, c.Request)
		})
		router.GET("/samples/*filepath", func(c *gin.Context) {
			fileServer.ServeHTTP(c.Writer, c.Request)
		})
	} else {
		router.GET("/", func(c *gin.Context) { c.JSON(200, gin.H{"message": "Hello, world!"}) })
	}

	router.GET("/ping", func(c *gin.Context) { c.JSON(200, gin.H{"message": "pong"}) })

	// ─── 管理端 Admin APIs (/admin/api/v1/*) ───────────────────────
	adminToken := ""
	if cfg != nil {
		adminToken = cfg.AdminToken
	}
	adminGroup := router.Group("/admin/api/v1")
	adminGroup.Use(middlewares.AdminAuthorization(adminToken))
	{
		adminGroup.GET("/auth/status", adminHandler.AuthStatus)
		adminGroup.GET("/overview", adminHandler.Overview)
		adminGroup.GET("/models", adminHandler.ListModelsCatalog)
		adminGroup.GET("/diagnostics", adminHandler.Diagnostics)
		adminGroup.POST("/diagnostics/repair", adminHandler.ExecuteDiagnosticRepair)
		adminGroup.GET("/logs", adminHandler.ListLogs)
		adminGroup.GET("/ops-logs", adminHandler.ListOpsLogs)
		adminGroup.POST("/ops-logs/frontend", adminHandler.IngestFrontendLogs)
		adminGroup.DELETE("/ops-logs", adminHandler.ClearOpsLogs)
		adminGroup.GET("/proxies", adminHandler.ListProxies)
		adminGroup.POST("/proxies/probe", adminHandler.ProbeProxies)
		adminGroup.POST("/proxies", adminHandler.UpdateProxies)
		adminGroup.GET("/providers", adminHandler.ListProviders)
		adminGroup.GET("/providers/:provider/stats", adminHandler.ProviderStats)
		adminGroup.GET("/providers/:provider/accounts", adminHandler.ListAccounts)
		adminGroup.POST("/providers/:provider/accounts/bulk-proxy", adminHandler.BulkUpdateAccountProxy)
		adminGroup.GET("/providers/:provider/export", adminHandler.ExportAccounts)
		adminGroup.POST("/providers/:provider/accounts/import", adminHandler.ImportAccounts)
		adminGroup.POST("/providers/:provider/accounts/kami-login", adminHandler.LoginKamiAccounts)
		adminGroup.POST("/providers/:provider/accounts/kami-login/cancel", adminHandler.CancelKamiLogin)
		adminGroup.GET("/providers/:provider/accounts/kami-login/tasks/:taskID", adminHandler.KamiLoginTask)
		adminGroup.POST("/providers/:provider/tasks/dismiss", adminHandler.DismissTask)
		adminGroup.POST("/providers/:provider/accounts/:id/action", adminHandler.AccountAction)
		adminGroup.POST("/providers/:provider/pool/probe-all", adminHandler.ProbeAllAccounts)
		adminGroup.GET("/prompts", adminHandler.ListPrompts)
		adminGroup.POST("/prompts/sync", adminHandler.SyncPrompts)
		adminGroup.POST("/prompts/expand", adminHandler.ExpandPrompt)
		adminGroup.POST("/prompts/video-expand", adminHandler.ExpandVideoPrompt)

		// 图像工坊作品画廊
		adminGroup.GET("/gallery", galleryHandler.ListWorks)
		adminGroup.POST("/gallery", galleryHandler.SaveWork)
		adminGroup.POST("/gallery/batch-migrate", galleryHandler.BatchMigrate)
		adminGroup.PUT("/gallery/:id/favorite", galleryHandler.ToggleFavorite)
		adminGroup.DELETE("/gallery/:id", galleryHandler.DeleteWork)
		adminGroup.DELETE("/gallery", galleryHandler.ClearWorks)

		// AI Copilot 会话历史持久化（旧版兼容接口）
		adminGroup.GET("/chat/messages", galleryHandler.ListChatMessages)
		adminGroup.POST("/chat/messages", galleryHandler.SaveChatMessages)
		adminGroup.DELETE("/chat/messages", galleryHandler.ClearChatMessages)

		// 专业 AI 对话工作台（多会话、预设、全文检索与导入导出）
		adminGroup.GET("/chat/conversations", chatWorkbenchHandler.ListConversations)
		adminGroup.POST("/chat/conversations", chatWorkbenchHandler.CreateConversation)
		adminGroup.GET("/chat/conversations/:id", chatWorkbenchHandler.GetConversation)
		adminGroup.PATCH("/chat/conversations/:id", chatWorkbenchHandler.UpdateConversation)
		adminGroup.DELETE("/chat/conversations/:id", chatWorkbenchHandler.DeleteConversation)
		adminGroup.POST("/chat/conversations/:id/messages", chatWorkbenchHandler.SaveConversationMessages)
		adminGroup.DELETE("/chat/conversations/:id/messages/:msg_id", chatWorkbenchHandler.DeleteConversationMessage)
		adminGroup.GET("/chat/conversations/:id/export", chatWorkbenchHandler.ExportConversation)
		adminGroup.POST("/chat/conversations/import", chatWorkbenchHandler.ImportConversations)
		adminGroup.GET("/chat/search", chatWorkbenchHandler.SearchChat)
		adminGroup.GET("/chat/presets", chatWorkbenchHandler.ListPresets)
		adminGroup.POST("/chat/presets", chatWorkbenchHandler.SavePreset)
		adminGroup.DELETE("/chat/presets/:id", chatWorkbenchHandler.DeletePreset)

		// 提示词广场收藏持久化
		adminGroup.GET("/prompts/favorites", galleryHandler.ListPromptFavorites)
		adminGroup.POST("/prompts/favorites/toggle", galleryHandler.TogglePromptFavorite)
		adminGroup.POST("/prompts/favorites/sync", galleryHandler.SyncPromptFavorites)

		// 多模态音频与视频任务运维
		adminGroup.GET("/multimodal/video-tasks", videoHandler.ListTasks)
		adminGroup.DELETE("/multimodal/video-tasks/:id", videoHandler.DeleteTask)
		adminGroup.DELETE("/multimodal/video-tasks", videoHandler.ClearTasks)
		adminGroup.GET("/multimodal/audio-history", audioHandler.ListAudioHistory)
		adminGroup.GET("/providers/channels", adminHandler.ListChannels)

		// 短剧工坊无限画布：能力矩阵、工程持久化与同源媒体缓存
		adminGroup.GET("/drama/capabilities", dramaHandler.Capabilities)
		adminGroup.GET("/drama/projects", dramaHandler.ListProjects)
		adminGroup.POST("/drama/projects", dramaHandler.SaveProject)
		adminGroup.GET("/drama/projects/:id", dramaHandler.GetProject)
		adminGroup.PUT("/drama/projects/:id", dramaHandler.SaveProject)
		adminGroup.DELETE("/drama/projects/:id", dramaHandler.DeleteProject)
		adminGroup.POST("/drama/media/cache", dramaHandler.CacheMedia)
	}

	// 静态生图/音频资源强缓存分发
	router.GET("/storage/images/:filename", galleryHandler.ServeStaticImage)
	router.GET("/storage/audio/:filename", dramaHandler.ServeCachedAudio)

	router.POST("/auth/session", authHandler.Session)
	router.POST("/auth/refresh", authHandler.Refresh)

	router.OPTIONS("/v1/chat/completions", optionsHandler)
	router.OPTIONS("/v1/models", optionsHandler)
	router.OPTIONS("/v1/responses", optionsHandler)
	router.OPTIONS("/v1/images/generations", optionsHandler)
	router.OPTIONS("/v1/images/edits", optionsHandler)
	router.OPTIONS("/v1/images/variations", optionsHandler)
	router.OPTIONS("/v1/files", optionsHandler)
	router.OPTIONS("/v1/audio/speech", optionsHandler)
	router.OPTIONS("/v1/audio/transcriptions", optionsHandler)
	router.OPTIONS("/v1/audio/translations", optionsHandler)
	router.OPTIONS("/v1/audio/voices", optionsHandler)
	router.OPTIONS("/v1/audio/voices/design", optionsHandler)
	router.OPTIONS("/v1/audio/voices/clone", optionsHandler)
	router.OPTIONS("/v1/audio/history", optionsHandler)
	router.OPTIONS("/v1/audio/files/:id", optionsHandler)
	router.OPTIONS("/v1/videos/generations", optionsHandler)
	router.OPTIONS("/v1/videos/tasks/:id", optionsHandler)
	router.OPTIONS("/v1/videos/:id", optionsHandler)
	router.OPTIONS("/v1/videos/:id/content", optionsHandler)
	router.OPTIONS("/v1/videos/models", optionsHandler)
	router.OPTIONS("/v1/videos/files/:id", optionsHandler)
	router.GET("/v1/videos/files/:id", videoHandler.StreamVideoFile)
	router.HEAD("/v1/videos/files/:id", videoHandler.StreamVideoFile)
	router.GET("/v1/videos/:id/content", videoHandler.StreamVideoFile)
	router.HEAD("/v1/videos/:id/content", videoHandler.StreamVideoFile)
	router.GET("/storage/videos/:id", videoHandler.StreamVideoFile)
	router.HEAD("/storage/videos/:id", videoHandler.StreamVideoFile)

	authGroup := router.Group("").Use(middlewares.Authorization)
	authGroup.POST("/v1/chat/completions", chatHandler.Nightmare)
	authGroup.POST("/v1/responses", chatHandler.Responses)
	authGroup.POST("/v1/files", chatHandler.Files)
	authGroup.GET("/v1/models", modelsHandler.ListModels)
	authGroup.POST("/backend-api/conversation", chatHandler.ChatGPTConversation)
	authGroup.POST("/v1/images/generations", imageHandler.Generations)
	authGroup.POST("/v1/images/edits", imageHandler.Edits)
	authGroup.POST("/v1/images/variations", imageHandler.Variations)
	authGroup.POST("/v1/audio/speech", audioHandler.TTS)
	authGroup.POST("/v1/audio/transcriptions", audioHandler.Transcriptions)
	authGroup.POST("/v1/audio/translations", audioHandler.Translations)
	authGroup.GET("/v1/audio/voices", audioHandler.ListVoices)
	authGroup.POST("/v1/audio/voices/design", audioHandler.VoiceDesign)
	authGroup.POST("/v1/audio/voices/clone", audioHandler.VoiceClone)
	authGroup.GET("/v1/audio/history", audioHandler.ListAudioHistory)
	authGroup.GET("/v1/audio/files/:id", audioHandler.GetAudioFile)
	authGroup.POST("/v1/videos/generations", videoHandler.Generations)
	authGroup.GET("/v1/videos/tasks/:id", videoHandler.GetTask)
	authGroup.GET("/v1/videos/:id", videoHandler.GetTask)
	authGroup.GET("/v1/videos/models", videoHandler.ListModels)

	return router
}
