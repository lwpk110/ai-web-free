package handler

import (
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"ai-web-free/internal/config"
	"ai-web-free/internal/provider"
	"ai-web-free/internal/proxy"

	"github.com/gin-gonic/gin"
)

type AdminHandler struct {
	registry  *provider.Registry
	proxyPool *proxy.Pool
	cfg       *config.Config
	startedAt time.Time
}

func NewAdminHandler(reg *provider.Registry, proxyPool *proxy.Pool, cfg *config.Config) *AdminHandler {
	return &AdminHandler{
		registry:  reg,
		proxyPool: proxyPool,
		cfg:       cfg,
		startedAt: time.Now(),
	}
}

// Overview 返回网关全局总览、代理池状态及所有供应商插件统计
func (h *AdminHandler) Overview(c *gin.Context) {
	descriptors := h.registry.ListDescriptors()
	providerSummaries := make([]gin.H, 0, len(descriptors))
	for _, desc := range descriptors {
		p, _ := h.registry.Get(desc.ID)
		var stats interface{}
		if p != nil {
			stats = p.Stats()
		}
		providerSummaries = append(providerSummaries, gin.H{
			"descriptor": desc,
			"stats":      stats,
		})
	}

	var proxies []string
	if h.proxyPool != nil {
		proxies = h.proxyPool.List()
	}

	c.JSON(http.StatusOK, gin.H{
		"service":                "ai-web-free",
		"version":                "2.6.0",
		"uptime_seconds":         int64(time.Since(h.startedAt).Seconds()),
		"started_at":             h.startedAt.Format(time.RFC3339),
		"auth_enabled":           h.cfg != nil && h.cfg.Authorization != "",
		"external_token_enabled": h.cfg != nil && h.cfg.EnableExternalToken,
		"stream_mode":            h.cfg != nil && h.cfg.StreamMode,
		"proxies_count":          len(proxies),
		"proxies":                proxies,
		"providers":              providerSummaries,
	})
}

// ListProviders 返回所有已注册供应商插件
func (h *AdminHandler) ListProviders(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"providers": h.registry.ListDescriptors(),
	})
}

// ProviderStats 返回指定供应商号池统计
func (h *AdminHandler) ProviderStats(c *gin.Context) {
	providerID := c.Param("provider")
	p, ok := h.registry.Get(providerID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "未找到供应商插件: " + providerID}})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"provider":   providerID,
		"descriptor": p.Descriptor(),
		"stats":      p.Stats(),
	})
}

// ListAccounts 返回指定供应商的账号池快照列表
func (h *AdminHandler) ListAccounts(c *gin.Context) {
	providerID := c.Param("provider")
	p, ok := h.registry.Get(providerID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "未找到供应商插件: " + providerID}})
		return
	}
	typeFilter := c.Query("type")
	statusFilter := c.Query("status")
	search := c.Query("q")

	list := p.ListAccounts(typeFilter, statusFilter, search)
	c.JSON(http.StatusOK, gin.H{
		"provider": providerID,
		"stats":    p.Stats(),
		"accounts": list,
	})
}

// ImportAccounts 批量导入账号凭据
func (h *AdminHandler) ImportAccounts(c *gin.Context) {
	providerID := c.Param("provider")
	p, ok := h.registry.Get(providerID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "未找到供应商插件: " + providerID}})
		return
	}
	var req provider.ImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "请求 JSON 格式无效: " + err.Error()}})
		return
	}
	res, err := p.ImportCredentials(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}, "result": res})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"provider": providerID,
		"result":   res,
		"stats":    p.Stats(),
	})
}

// AccountAction 对单个账号执行操作（probe / refresh / enable / disable / delete）
func (h *AdminHandler) AccountAction(c *gin.Context) {
	providerID := c.Param("provider")
	accountID := c.Param("id")
	p, ok := h.registry.Get(providerID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "未找到供应商插件: " + providerID}})
		return
	}
	var body struct {
		Action string `json:"action"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Action == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "缺少 action 参数"}})
		return
	}
	result, err := p.ExecuteAccountAction(accountID, body.Action)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"provider": providerID,
		"result":   result,
		"stats":    p.Stats(),
	})
}

// ProbeAllAccounts 批量并发测活当前供应商的所有非免登账号
func (h *AdminHandler) ProbeAllAccounts(c *gin.Context) {
	providerID := c.Param("provider")
	p, ok := h.registry.Get(providerID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "未找到供应商插件: " + providerID}})
		return
	}

	snapshots := p.ListAccounts("free", "", "")
	puidSnaps := p.ListAccounts("puid", "", "")
	snapshots = append(snapshots, puidSnaps...)

	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var results []map[string]interface{}
	passed := 0
	failed := 0

	for _, snap := range snapshots {
		if snap.Status == "disabled" {
			continue
		}
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			res, err := p.ExecuteAccountAction(id, "probe")
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++
				results = append(results, map[string]interface{}{"id": id, "ok": false, "error": err.Error()})
				return
			}
			if okFlag, _ := res["ok"].(bool); okFlag {
				passed++
			} else {
				failed++
			}
			results = append(results, res)
		}(snap.ID)
	}
	wg.Wait()

	c.JSON(http.StatusOK, gin.H{
		"provider": providerID,
		"probed":   len(results),
		"passed":   passed,
		"failed":   failed,
		"results":  results,
		"stats":    p.Stats(),
	})
}

// UpdateProxies 动态更新代理池
func (h *AdminHandler) UpdateProxies(c *gin.Context) {
	var body struct {
		RawText string   `json:"raw_text"`
		Proxies []string `json:"proxies"`
		Persist bool     `json:"persist"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}})
		return
	}
	var list []string
	for _, p := range body.Proxies {
		if s := strings.TrimSpace(p); s != "" {
			list = append(list, s)
		}
	}
	if body.RawText != "" {
		for _, line := range strings.Split(body.RawText, "\n") {
			line = strings.TrimSpace(line)
			if line != "" && !strings.HasPrefix(line, "#") {
				list = append(list, line)
			}
		}
	}
	if h.proxyPool != nil {
		h.proxyPool.Replace(list)
	}
	if body.Persist {
		content := strings.Join(list, "\n")
		if len(list) > 0 {
			content += "\n"
		}
		_ = os.WriteFile("proxies.txt", []byte(content), 0644)
	}
	c.JSON(http.StatusOK, gin.H{
		"proxies_count": len(list),
		"proxies":       list,
	})
}
