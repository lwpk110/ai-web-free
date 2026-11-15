package handler

import (
	"fmt"
	"net/http"
	"time"

	"ai-web-free/internal/health"
	"ai-web-free/internal/proxy"
	"ai-web-free/internal/storage"

	"github.com/gin-gonic/gin"
)

// BuildServerStatus 聚合 Go 运行时指标、后台服务启停状态与核心子系统健康检查报告。
func (h *AdminHandler) BuildServerStatus() health.ServerStatusReport {
	now := time.Now().UTC()
	rtMetrics := health.CollectRuntimeMetrics()

	var services []health.BackgroundServiceStatus
	if h.runtime != nil && h.runtime.HealthTracker != nil {
		services = h.runtime.HealthTracker.Snapshot()
	}
	if services == nil {
		services = []health.BackgroundServiceStatus{}
	}

	// 若未通过 bootstrap 注册卡密 Worker（例如测试环境直接构造 RouterDeps），则按 KamiWorker 实例补充状态。
	hasKamiSvc := false
	for _, s := range services {
		if s.Name == "kami_renew_worker" {
			hasKamiSvc = true
			break
		}
	}
	if !hasKamiSvc && h.kamiWorker() != nil {
		kwStats := h.kamiWorker().Stats()
		enabled := h.cfg != nil && h.cfg.KamiAutoRenew
		status := "disabled"
		detail := "已禁用（KAMI_AUTO_RENEW=false）"
		if enabled {
			status = "running"
			detail = fmt.Sprintf("队列待处理 %d 项", kwStats.QueueLength)
		}
		services = append(services, health.BackgroundServiceStatus{
			Name:     "kami_renew_worker",
			Label:    "卡密自动续期 Worker",
			Category: "worker",
			Enabled:  enabled,
			Running:  enabled,
			Status:   status,
			Detail:   detail,
		})
	}

	subsystems := h.collectSubsystemHealth()
	svcTotal, svcRunning, svcDisabled, svcStopped := health.SummarizeServices(services)

	subTotal := len(subsystems)
	subHealthy := 0
	subDegraded := 0
	hasUnhealthy := false
	for _, sub := range subsystems {
		switch sub.Status {
		case "healthy":
			subHealthy++
		case "degraded":
			subDegraded++
		case "unhealthy":
			hasUnhealthy = true
		}
	}

	overall := "healthy"
	if hasUnhealthy {
		overall = "unhealthy"
	} else if svcStopped > 0 || subDegraded > 0 {
		overall = "degraded"
	}

	summary := health.ServerHealthSummary{
		Status:             overall,
		ServicesTotal:      svcTotal,
		ServicesRunning:    svcRunning,
		ServicesDisabled:   svcDisabled,
		ServicesStopped:    svcStopped,
		SubsystemsTotal:    subTotal,
		SubsystemsHealthy:  subHealthy,
		SubsystemsDegraded: subDegraded,
		Goroutines:         rtMetrics.Goroutines,
		MemAllocMB:         rtMetrics.MemAllocMB,
	}

	return health.ServerStatusReport{
		Status:             overall,
		Service:            "ai-web-free",
		Version:            "2.6.0",
		UptimeSeconds:      int64(time.Since(h.startedAt).Seconds()),
		StartedAt:          h.startedAt.UTC().Format(time.RFC3339),
		CheckedAt:          now.Format(time.RFC3339),
		Summary:            summary,
		Runtime:            rtMetrics,
		BackgroundServices: services,
		Subsystems:         subsystems,
	}
}

func (h *AdminHandler) collectSubsystemHealth() []health.SubsystemHealth {
	subsystems := make([]health.SubsystemHealth, 0, 7)

	// 1. HTTP 网关
	subsystems = append(subsystems, health.SubsystemHealth{
		Name:   "http_gateway",
		Label:  "HTTP 网关服务",
		Status: "healthy",
		Detail: "Gin 路由与中间件在线响应中",
	})

	// 2. 账号池
	if pool := h.accountPool(); pool != nil {
		stats := pool.Stats()
		status := "healthy"
		detail := fmt.Sprintf("活跃 %d / 总计 %d 个账号", stats.Active, stats.Total)
		if stats.Total == 0 {
			status = "degraded"
			detail = "号池为空，等待导入账号或启用 FREE_ACCOUNTS"
		} else if stats.Active == 0 {
			status = "degraded"
			detail = fmt.Sprintf("总计 %d 个账号，当前无活跃可用账号", stats.Total)
		}
		subsystems = append(subsystems, health.SubsystemHealth{
			Name:   "account_pool",
			Label:  "多供应商号池",
			Status: status,
			Detail: detail,
			Meta: map[string]any{
				"active": stats.Active,
				"total":  stats.Total,
			},
		})
	} else {
		subsystems = append(subsystems, health.SubsystemHealth{
			Name:   "account_pool",
			Label:  "多供应商号池",
			Status: "unhealthy",
			Detail: "号池未初始化",
		})
	}

	// 3. 代理池
	if h.proxyPool != nil {
		nodes := h.proxyPool.Snapshots(h.boundProxyCounts())
		sum := summarizeProxies(nodes)
		status := "healthy"
		detail := "直连模式（未配置外部代理）"
		if sum.Total > 0 {
			if sum.Active > 0 {
				detail = fmt.Sprintf("可用 %d / 总计 %d 个代理节点", sum.Active, sum.Total)
			} else {
				status = "degraded"
				detail = fmt.Sprintf("%d 个代理节点全部处于熔断冷却中", sum.Total)
			}
		}
		subsystems = append(subsystems, health.SubsystemHealth{
			Name:   "proxy_pool",
			Label:  "代理调度池",
			Status: status,
			Detail: detail,
			Meta: map[string]any{
				"total":   sum.Total,
				"active":  sum.Active,
				"cooling": sum.Cooling,
			},
		})
	} else {
		subsystems = append(subsystems, health.SubsystemHealth{
			Name:   "proxy_pool",
			Label:  "代理调度池",
			Status: "healthy",
			Detail: "直连模式（未启用代理池）",
		})
	}

	// 4. 持久化数据库
	if h.runtime != nil && h.runtime.DB != nil {
		sqlDB, err := h.runtime.DB.DB()
		if err != nil {
			subsystems = append(subsystems, health.SubsystemHealth{
				Name:   "database",
				Label:  "持久化数据库",
				Status: "unhealthy",
				Detail: fmt.Sprintf("获取数据库连接失败: %v", err),
			})
		} else if pingErr := sqlDB.Ping(); pingErr != nil {
			subsystems = append(subsystems, health.SubsystemHealth{
				Name:   "database",
				Label:  "持久化数据库",
				Status: "unhealthy",
				Detail: fmt.Sprintf("数据库 Ping 失败: %v", pingErr),
			})
		} else {
			subsystems = append(subsystems, health.SubsystemHealth{
				Name:   "database",
				Label:  "持久化数据库",
				Status: "healthy",
				Detail: fmt.Sprintf("连接正常（%s）", h.runtime.DB.Dialector.Name()),
			})
		}
	} else {
		subsystems = append(subsystems, health.SubsystemHealth{
			Name:   "database",
			Label:  "持久化数据库",
			Status: "disabled",
			Detail: "未挂载数据库实例（使用纯内存模式）",
		})
	}

	// 5. 媒体存储引擎
	if h.runtime != nil && h.runtime.StorageEngine != nil {
		switch h.runtime.StorageEngine.(type) {
		case *storage.LocalStorageEngine:
			subsystems = append(subsystems, health.SubsystemHealth{
				Name:   "storage_engine",
				Label:  "媒体存储引擎",
				Status: "healthy",
				Detail: "本地磁盘存储引擎就绪",
			})
		case *storage.MemoryStorageEngine:
			subsystems = append(subsystems, health.SubsystemHealth{
				Name:   "storage_engine",
				Label:  "媒体存储引擎",
				Status: "degraded",
				Detail: "当前使用内存临时存储（重启后不保留）",
			})
		default:
			subsystems = append(subsystems, health.SubsystemHealth{
				Name:   "storage_engine",
				Label:  "媒体存储引擎",
				Status: "healthy",
				Detail: "存储引擎已挂载",
			})
		}
	} else {
		subsystems = append(subsystems, health.SubsystemHealth{
			Name:   "storage_engine",
			Label:  "媒体存储引擎",
			Status: "disabled",
			Detail: "未挂载媒体存储引擎",
		})
	}

	// 6. 审计日志缓冲
	if al := h.auditLog(); al != nil {
		_, auditSum := al.Query("all", "", 1)
		detail := fmt.Sprintf("环形缓冲 %d/%d 条", auditSum.BufferedCount, auditSum.Capacity)
		if auditSum.FileLogging {
			detail += " · 已开启文件落盘"
		}
		subsystems = append(subsystems, health.SubsystemHealth{
			Name:   "audit_log",
			Label:  "审计日志缓冲",
			Status: "healthy",
			Detail: detail,
		})
	}

	// 7. 向量检索
	if h.runtime != nil && h.runtime.VectorStore != nil {
		subsystems = append(subsystems, health.SubsystemHealth{
			Name:   "vector_store",
			Label:  "向量检索引擎",
			Status: "healthy",
			Detail: "对话上下文向量检索已就绪",
		})
	}

	_ = proxy.NodeStatus{}
	return subsystems
}

// PublicHealth 提供免鉴权的轻量级健康探针：GET /health 与 GET /healthz。
func (h *AdminHandler) PublicHealth(c *gin.Context) {
	report := h.BuildServerStatus()
	code := http.StatusOK
	if report.Status == "unhealthy" {
		code = http.StatusServiceUnavailable
	}
	c.JSON(code, gin.H{
		"status":             report.Status,
		"service":            report.Service,
		"version":            report.Version,
		"uptime_seconds":     report.UptimeSeconds,
		"started_at":         report.StartedAt,
		"checked_at":         report.CheckedAt,
		"goroutines":         report.Summary.Goroutines,
		"mem_alloc_mb":       report.Summary.MemAllocMB,
		"services_running":   report.Summary.ServicesRunning,
		"services_total":     report.Summary.ServicesTotal,
		"subsystems_healthy": report.Summary.SubsystemsHealthy,
		"subsystems_total":   report.Summary.SubsystemsTotal,
	})
}

// SystemStatus 返回完整服务器状态监控报告：GET /admin/api/v1/system/status。
func (h *AdminHandler) SystemStatus(c *gin.Context) {
	c.JSON(http.StatusOK, h.BuildServerStatus())
}
