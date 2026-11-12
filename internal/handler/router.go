package handler

import (
	"io/fs"
	"net/http"
	"strings"

	"ai-web-free/httpclient/bogdanfinn"
	"ai-web-free/internal/accounts"
	"ai-web-free/internal/chatgpt"
	"ai-web-free/internal/config"
	"ai-web-free/internal/provider"
	"ai-web-free/internal/proxy"
	"ai-web-free/middlewares"
	"ai-web-free/web"

	"github.com/gin-gonic/gin"
)

func optionsHandler(c *gin.Context) {
	c.Header("Access-Control-Allow-Origin", "*")
	c.Header("Access-Control-Allow-Methods", "POST")
	c.Header("Access-Control-Allow-Headers", "*")
	c.JSON(200, gin.H{
		"message": "pong",
	})
}

func RegisterRouter(accountPool *accounts.Pool, proxyPool *proxy.Pool, registry *provider.Registry, cfg *config.Config) *gin.Engine {
	chatHandler := NewChatHandler(accountPool, cfg)
	imageHandler := NewImageHandler(accountPool, cfg)
	audioHandler := NewAudioHandler(accountPool, cfg)
	authHandler := NewAuthHandler(accountPool)
	modelsHandler := NewModelsHandler(accountPool, cfg)
	adminHandler := NewAdminHandler(registry, proxyPool, cfg)

	// 初始化基础前置参数（DPL、BasicCookies 等）
	proxyUrl := cfg.ProxyURL
	if proxyUrl == "" {
		proxyUrl = cfg.HTTPProxy
	}
	client := bogdanfinn.NewStdClient()
	chatgpt.GetDpl(client, proxyUrl)

	router := gin.Default()
	router.Use(middlewares.Cors)

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
	} else {
		router.GET("/", func(c *gin.Context) { c.JSON(200, gin.H{"message": "Hello, world!"}) })
	}

	router.GET("/ping", func(c *gin.Context) { c.JSON(200, gin.H{"message": "pong"}) })

	// ─── 管理端 Admin APIs (/admin/api/v1/*) ───────────────────────
	adminGroup := router.Group("/admin/api/v1")
	{
		adminGroup.GET("/overview", adminHandler.Overview)
		adminGroup.GET("/providers", adminHandler.ListProviders)
		adminGroup.GET("/providers/:provider/stats", adminHandler.ProviderStats)
		adminGroup.GET("/providers/:provider/accounts", adminHandler.ListAccounts)
		adminGroup.POST("/providers/:provider/accounts/import", adminHandler.ImportAccounts)
		adminGroup.POST("/providers/:provider/accounts/:id/action", adminHandler.AccountAction)
		adminGroup.POST("/providers/:provider/pool/probe-all", adminHandler.ProbeAllAccounts)
		adminGroup.POST("/proxies", adminHandler.UpdateProxies)
	}

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

	return router
}
