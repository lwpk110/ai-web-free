package handler

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/audit"
	"ai-web-free/internal/chatgpt"
	"ai-web-free/internal/provider"
	"ai-web-free/internal/proxy"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ProxySummary 与前端 ProxySummary 对齐，概括代理池节点健康分布。
type ProxySummary struct {
	Total   int `json:"total"`
	Active  int `json:"active"`
	Cooling int `json:"cooling"`
}

func summarizeProxies(nodes []proxy.NodeStatus) ProxySummary {
	out := ProxySummary{Total: len(nodes)}
	for _, n := range nodes {
		if n.Status == "cooling" {
			out.Cooling++
		} else {
			out.Active++
		}
	}
	return out
}

// DiagnosticRootCauseItem 描述一类平台级异常根因及其关联账号、代理与建议修复动作。
type DiagnosticRootCauseItem struct {
	Category       string   `json:"category"`
	Severity       string   `json:"severity"`
	Title          string   `json:"title"`
	Count          int      `json:"count"`
	Summary        string   `json:"summary"`
	AffectedIDs    []string `json:"affected_ids,omitempty"`
	AccountEmails  []string `json:"account_emails,omitempty"`
	Proxies        []string `json:"proxies,omitempty"`
	SuggestedFixes []string `json:"suggested_fixes,omitempty"`
}

// DiagnosticRepairOption 描述可在控制台/Copilot 一键触发的诊断修复方案。
type DiagnosticRepairOption struct {
	Action      string `json:"action"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Destructive bool   `json:"destructive"`
	Recommended bool   `json:"recommended"`
	TargetCount int    `json:"target_count"`
}

// DiagnosticAccountBrief 表示存在异常或待关注账号的脱敏诊断摘要。
type DiagnosticAccountBrief struct {
	ID         string `json:"id"`
	Email      string `json:"email"`
	Type       string `json:"type"`
	Status     string `json:"status"`
	Reason     string `json:"reason"`
	Proxy      string `json:"proxy"`
	LastError  string `json:"last_error,omitempty"`
	RenewState string `json:"renew_state,omitempty"`
	HasRefresh bool   `json:"has_refresh"`
	HasSession bool   `json:"has_session"`
	HasKami    bool   `json:"has_kami"`
}

// DiagnosticsReport 汇总全平台号池、代理、卡密队列与审计日志的 5 类根因诊断报告。
type DiagnosticsReport struct {
	GeneratedAt     string                    `json:"generated_at"`
	HealthScore     int                       `json:"health_score"`
	OverallSeverity string                    `json:"overall_severity"`
	Headline        string                    `json:"headline"`
	PoolStats       accounts.PoolStats        `json:"pool_stats"`
	ProxySummary    ProxySummary              `json:"proxy_summary"`
	KamiWorker      *provider.KamiWorkerStats `json:"kami_worker,omitempty"`
	AuditSummary    audit.Summary             `json:"audit_summary"`
	RootCauses      []DiagnosticRootCauseItem `json:"root_causes"`
	FlaggedAccounts []DiagnosticAccountBrief  `json:"flagged_accounts"`
	TrippedProxies  []proxy.NodeStatus        `json:"tripped_proxies"`
	RepairOptions   []DiagnosticRepairOption  `json:"repair_options"`
	MarkdownReport  string                    `json:"markdown_report"`
}

func appendUniqueLimited(slice []string, val string, limit int) []string {
	v := strings.TrimSpace(val)
	if v == "" {
		return slice
	}
	for _, existing := range slice {
		if existing == v {
			return slice
		}
	}
	if limit > 0 && len(slice) >= limit {
		return slice
	}
	return append(slice, v)
}

func accountLabel(snap accounts.AccountSnapshot) string {
	if strings.TrimSpace(snap.Email) != "" {
		return snap.Email
	}
	if len(snap.ID) > 8 {
		return snap.ID[:8]
	}
	return snap.ID
}

func classifySnapshotCategory(snap accounts.AccountSnapshot) string {
	if snap.Status == "banned" || chatgpt.LooksLikeAccountBanned(0, snap.LastError) {
		return chatgpt.ErrCategoryBanned
	}
	reason := chatgpt.ClassifyAccountErrorReason(0, snap.LastError)
	if snap.Status == "expired" {
		if reason == chatgpt.ErrCategoryWAFProxy {
			return chatgpt.ErrCategoryWAFProxy
		}
		return chatgpt.ErrCategoryTokenExpired
	}
	if snap.RenewState == "cooldown" {
		return chatgpt.ErrCategoryTokenExpired
	}
	if reason == chatgpt.ErrCategoryWAFProxy || reason == chatgpt.ErrCategoryProxyTransport {
		return chatgpt.ErrCategoryWAFProxy
	}
	if snap.Status == "rate_limited" || snap.ImageLimited ||
		reason == chatgpt.ErrCategoryRateLimit || reason == chatgpt.ErrCategoryImageLimit {
		return chatgpt.ErrCategoryRateLimit
	}
	if reason == chatgpt.ErrCategoryTokenExpired {
		return chatgpt.ErrCategoryTokenExpired
	}
	return chatgpt.ErrCategoryOther
}

// BuildDiagnosticsReport 聚合号池状态、代理熔断、卡密续期与审计日志生成完整平台诊断报告。
func (h *AdminHandler) BuildDiagnosticsReport() DiagnosticsReport {
	var stats accounts.PoolStats
	var snapshots []accounts.AccountSnapshot
	if pool := h.accountPool(); pool != nil {
		stats = pool.Stats()
		snapshots = pool.ListSnapshots("", "", "")
	}

	var proxyNodes []proxy.NodeStatus
	if h.proxyPool != nil {
		proxyNodes = h.proxyPool.Snapshots(h.boundProxyCounts())
	}
	proxySum := summarizeProxies(proxyNodes)

	var kamiStats *provider.KamiWorkerStats
	if kw := h.kamiWorker(); kw != nil {
		st := kw.Stats()
		kamiStats = &st
	}

	var auditEntries []audit.Entry
	var auditSum audit.Summary
	if logBuf := h.auditLog(); logBuf != nil {
		auditEntries, auditSum = logBuf.Query("all", "", 60)
	}

	type causeBucket struct {
		ids     []string
		emails  []string
		proxies []string
	}
	buckets := map[string]*causeBucket{
		chatgpt.ErrCategoryBanned:       {},
		chatgpt.ErrCategoryWAFProxy:     {},
		chatgpt.ErrCategoryTokenExpired: {},
		chatgpt.ErrCategoryRateLimit:    {},
		"proxy_breaker":                 {},
	}

	var flagged []DiagnosticAccountBrief
	bareExpiredCount := 0
	refreshExpiredCount := 0
	kamiExpiredCount := 0
	imageLimitedOnly := 0
	globalRateLimited := 0

	for _, snap := range snapshots {
		isFlagged := snap.Status != "active" || snap.ImageLimited || snap.LastError != "" || snap.RenewState == "cooldown"
		if !isFlagged {
			continue
		}
		if snap.Status == "disabled" && !chatgpt.LooksLikeAccountBanned(0, snap.LastError) && snap.LastError == "" {
			continue
		}

		cat := classifySnapshotCategory(snap)
		flagged = append(flagged, DiagnosticAccountBrief{
			ID:         snap.ID,
			Email:      accountLabel(snap),
			Type:       snap.Type,
			Status:     snap.Status,
			Reason:     cat,
			Proxy:      snap.Proxy,
			LastError:  snap.LastError,
			RenewState: snap.RenewState,
			HasRefresh: snap.HasRefreshToken,
			HasSession: snap.HasSessionToken,
			HasKami:    snap.HasKamiCredentials,
		})

		if b, ok := buckets[cat]; ok {
			b.ids = append(b.ids, snap.ID)
			b.emails = appendUniqueLimited(b.emails, accountLabel(snap), 8)
			b.proxies = appendUniqueLimited(b.proxies, snap.Proxy, 6)
		}

		switch cat {
		case chatgpt.ErrCategoryTokenExpired:
			if snap.HasKamiCredentials {
				kamiExpiredCount++
			} else if snap.HasRefreshToken || snap.HasSessionToken {
				refreshExpiredCount++
			} else {
				bareExpiredCount++
			}
		case chatgpt.ErrCategoryRateLimit:
			if snap.ImageLimited && snap.Status == "active" {
				imageLimitedOnly++
			} else {
				globalRateLimited++
			}
		}
	}

	recentWAFLogs := 0
	recentBanLogs := 0
	for _, entry := range auditEntries {
		if entry.Note == "waf_403" || strings.Contains(strings.ToLower(entry.Error), "cloudflare") {
			recentWAFLogs++
			buckets[chatgpt.ErrCategoryWAFProxy].proxies = appendUniqueLimited(buckets[chatgpt.ErrCategoryWAFProxy].proxies, entry.Proxy, 6)
			buckets[chatgpt.ErrCategoryWAFProxy].emails = appendUniqueLimited(buckets[chatgpt.ErrCategoryWAFProxy].emails, entry.AccountEmail, 6)
		}
		if entry.Note == "banned" || entry.Note == "probe_banned" || entry.Note == "kami_renew_banned" {
			recentBanLogs++
		}
	}

	var trippedProxies []proxy.NodeStatus
	for _, node := range proxyNodes {
		if node.Status == "cooling" || node.ConsecutiveErrors > 0 {
			trippedProxies = append(trippedProxies, node)
			buckets["proxy_breaker"].proxies = appendUniqueLimited(buckets["proxy_breaker"].proxies, node.URL, 8)
		}
	}

	var rootCauses []DiagnosticRootCauseItem

	if b := buckets[chatgpt.ErrCategoryBanned]; len(b.ids) > 0 {
		rootCauses = append(rootCauses, DiagnosticRootCauseItem{
			Category:       "banned",
			Severity:       "critical",
			Title:          "账号永久封禁 / 停用",
			Count:          len(b.ids),
			Summary:        fmt.Sprintf("检测到 %d 个账号命中 OpenAI account_deactivated/banned（审计记录 %d 条），已与续期队列隔离，建议停用或确认清理。", len(b.ids), recentBanLogs),
			AffectedIDs:    b.ids,
			AccountEmails:  b.emails,
			Proxies:        b.proxies,
			SuggestedFixes: []string{"disable_banned", "delete_banned"},
		})
	}

	if b := buckets[chatgpt.ErrCategoryWAFProxy]; len(b.ids) > 0 || recentWAFLogs > 0 {
		count := len(b.ids)
		if count == 0 {
			count = recentWAFLogs
		}
		sev := "warning"
		if stats.Active == 0 {
			sev = "critical"
		}
		rootCauses = append(rootCauses, DiagnosticRootCauseItem{
			Category:       "waf_proxy_block",
			Severity:       sev,
			Title:          "403 WAF / Cloudflare / Sentinel 拦截",
			Count:          count,
			Summary:        fmt.Sprintf("发现 %d 个账号及近期 %d 次请求触发 403 WAF/风控挑战（属代理 IP 污点，未破坏原有 Token），建议迁移至健康代理后测活。", len(b.ids), recentWAFLogs),
			AffectedIDs:    b.ids,
			AccountEmails:  b.emails,
			Proxies:        b.proxies,
			SuggestedFixes: []string{"rebind_proxies", "probe_all"},
		})
	}

	if b := buckets[chatgpt.ErrCategoryTokenExpired]; len(b.ids) > 0 {
		sev := "warning"
		if stats.Active == 0 {
			sev = "critical"
		}
		rootCauses = append(rootCauses, DiagnosticRootCauseItem{
			Category:       "token_expired",
			Severity:       sev,
			Title:          "401 令牌过期 / 卡密续期冷却",
			Count:          len(b.ids),
			Summary:        fmt.Sprintf("共 %d 个账号处于过期或续期退避（可刷新 refresh/session: %d，卡密账号: %d，不可自动续期裸 Token: %d）。", len(b.ids), refreshExpiredCount, kamiExpiredCount, bareExpiredCount),
			AffectedIDs:    b.ids,
			AccountEmails:  b.emails,
			Proxies:        b.proxies,
			SuggestedFixes: []string{"renew_expired", "topup_free_uuid"},
		})
	}

	if b := buckets[chatgpt.ErrCategoryRateLimit]; len(b.ids) > 0 {
		rootCauses = append(rootCauses, DiagnosticRootCauseItem{
			Category:       "rate_limit",
			Severity:       "warning",
			Title:          "429 全局限流 / 生图专属配额受限",
			Count:          len(b.ids),
			Summary:        fmt.Sprintf("%d 个账号处于冷却期（全局 429 冷却: %d，仅生图配额冷却/文本可用: %d），可通过补充匿名 UUID 分担文本流量。", len(b.ids), globalRateLimited, imageLimitedOnly),
			AffectedIDs:    b.ids,
			AccountEmails:  b.emails,
			Proxies:        b.proxies,
			SuggestedFixes: []string{"topup_free_uuid", "probe_all"},
		})
	}

	if len(trippedProxies) > 0 {
		sev := "warning"
		if proxySum.Total > 0 && proxySum.Active == 0 {
			sev = "critical"
		}
		rootCauses = append(rootCauses, DiagnosticRootCauseItem{
			Category:       "proxy_breaker",
			Severity:       sev,
			Title:          "代理节点熔断 / 连通性降级",
			Count:          len(trippedProxies),
			Summary:        fmt.Sprintf("代理池 %d/%d 节点异常或触发连续 3 次失败熔断冷却，需探测代理并重新绑定受影响账号。", len(trippedProxies), proxySum.Total),
			Proxies:        buckets["proxy_breaker"].proxies,
			SuggestedFixes: []string{"rebind_proxies"},
		})
	}

	sort.SliceStable(rootCauses, func(i, j int) bool {
		if rootCauses[i].Severity != rootCauses[j].Severity {
			return rootCauses[i].Severity == "critical"
		}
		return rootCauses[i].Count > rootCauses[j].Count
	})

	score := computeHealthScore(stats, proxySum, len(buckets[chatgpt.ErrCategoryWAFProxy].ids), len(trippedProxies))
	overallSeverity := "healthy"
	if score < 50 || stats.Active == 0 {
		overallSeverity = "critical"
	} else if score < 85 || len(rootCauses) > 0 {
		overallSeverity = "degraded"
	}

	headline := buildDiagnosticHeadline(stats, proxySum, overallSeverity, rootCauses)
	repairOptions := buildRepairOptions(stats, trippedProxies)

	report := DiagnosticsReport{
		GeneratedAt:     time.Now().UTC().Format(time.RFC3339),
		HealthScore:     score,
		OverallSeverity: overallSeverity,
		Headline:        headline,
		PoolStats:       stats,
		ProxySummary:    proxySum,
		KamiWorker:      kamiStats,
		AuditSummary:    auditSum,
		RootCauses:      rootCauses,
		FlaggedAccounts: flagged,
		TrippedProxies:  trippedProxies,
		RepairOptions:   repairOptions,
	}
	report.MarkdownReport = buildDiagnosticMarkdown(&report)
	return report
}

func computeHealthScore(stats accounts.PoolStats, proxySum ProxySummary, wafCount, trippedProxyCount int) int {
	if stats.Total == 0 {
		return 40
	}
	if stats.Active == 0 {
		return 10
	}
	score := 100
	unhealthyRatio := float64(stats.Total-stats.Active) / float64(stats.Total)
	score -= int(unhealthyRatio * 55)
	if stats.Banned > 0 {
		score -= minInt(20, stats.Banned*5)
	}
	if wafCount > 0 {
		score -= minInt(15, wafCount*5)
	}
	if proxySum.Total > 0 && trippedProxyCount > 0 {
		proxyPenalty := int(float64(trippedProxyCount) / float64(proxySum.Total) * 25)
		score -= proxyPenalty
	}
	if score < 5 {
		score = 5
	}
	if score > 100 {
		score = 100
	}
	return score
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func buildDiagnosticHeadline(stats accounts.PoolStats, proxySum ProxySummary, severity string, causes []DiagnosticRootCauseItem) string {
	if stats.Total == 0 {
		return "号池当前为空（0 个账号），请导入凭据或一键补充匿名 UUID。"
	}
	if stats.Active == 0 {
		return fmt.Sprintf("全池熔断告警：0/%d 账号可用（封禁 %d · 过期 %d · 限流 %d · 异常代理 %d/%d）。",
			stats.Total, stats.Banned, stats.Expired, stats.RateLimited, proxySum.Cooling, proxySum.Total)
	}
	if len(causes) == 0 {
		return fmt.Sprintf("平台运行健康：%d/%d 账号可用，%d 个代理节点正常。", stats.Active, stats.Total, proxySum.Active)
	}
	return fmt.Sprintf("平台处于%s状态：%d/%d 账号可用，发现 %d 类异常根因（%s）。",
		map[string]string{"critical": "严重告警", "degraded": "降级", "healthy": "健康"}[severity],
		stats.Active, stats.Total, len(causes), causes[0].Title)
}

func buildRepairOptions(stats accounts.PoolStats, trippedProxies []proxy.NodeStatus) []DiagnosticRepairOption {
	hasIssues := stats.Active == 0 || stats.Expired > 0 || stats.Banned > 0 || stats.RateLimited > 0 || len(trippedProxies) > 0
	return []DiagnosticRepairOption{
		{
			Action:      "smart_heal",
			Label:       "一键智能自愈",
			Description: "恢复已到期冷却 + 隔离封禁账号 + 迁移受损代理 + 续期过期账号 + 若 0 可用自动补充匿名 UUID",
			Recommended: hasIssues,
			TargetCount: stats.Total - stats.Active,
		},
		{
			Action:      "rebind_proxies",
			Label:       "探测并迁移受损代理",
			Description: "并发探测代理池，将 403 WAF 或绑定熔断节点的账号迁往健康节点并恢复冷却状态",
			TargetCount: len(trippedProxies),
		},
		{
			Action:      "renew_expired",
			Label:       "续期过期账号",
			Description: "清除卡密续期退避并刷新具有 refresh/session/卡密凭据的过期账号",
			TargetCount: stats.Expired,
		},
		{
			Action:      "probe_all",
			Label:       "全量测活账号",
			Description: "并发对号池执行 /models 连通性检查，按 5 类根因精确更新状态机",
			TargetCount: stats.FreeCount + stats.PUIDCount,
		},
		{
			Action:      "topup_free_uuid",
			Label:       "补充匿名 UUID",
			Description: "批量生成并注入 5 个免登匿名设备 UUID，快速缓解文本流量不可用",
			Recommended: stats.Active == 0,
			TargetCount: 5,
		},
		{
			Action:      "disable_banned",
			Label:       "停用封禁账号",
			Description: "将已封禁/停用账号设为 disabled，保留记录避免再次调度与续期",
			TargetCount: stats.Banned,
		},
		{
			Action:      "delete_banned",
			Label:       "清理删除封禁账号",
			Description: "从内存池永久移除所有已封禁账号（不可逆操作，需二次确认）",
			Destructive: true,
			TargetCount: stats.Banned,
		},
	}
}

func buildDiagnosticMarkdown(r *DiagnosticsReport) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("### 平台全链路诊断报告（健康评分 %d/100）\n\n", r.HealthScore))
	b.WriteString(fmt.Sprintf("- **总体结论**: %s\n", r.Headline))
	b.WriteString(fmt.Sprintf("- **号池状态**: 总计 `%d` · 可用 `%d` · 过期 `%d` · 限流 `%d` · 生图受限 `%d` · 封禁 `%d` · 停用 `%d`\n",
		r.PoolStats.Total, r.PoolStats.Active, r.PoolStats.Expired, r.PoolStats.RateLimited, r.PoolStats.ImageLimited, r.PoolStats.Banned, r.PoolStats.Disabled))
	b.WriteString(fmt.Sprintf("- **代理矩阵**: 总计 `%d` · 可用 `%d` · 熔断冷却 `%d`\n",
		r.ProxySummary.Total, r.ProxySummary.Active, r.ProxySummary.Cooling))
	if r.KamiWorker != nil {
		b.WriteString(fmt.Sprintf("- **卡密续期队列**: 运行中 `%v` · 队列 `%d` · 成功 `%d` · 失败 `%d`\n",
			r.KamiWorker.Running, r.KamiWorker.QueueLength, r.KamiWorker.TotalSucceeded, r.KamiWorker.TotalFailed))
	}
	b.WriteString(fmt.Sprintf("- **近期审计**: 请求 `%d` · 失败 `%d` · Failover 恢复 `%d` · 运维事件 `%d`\n",
		r.AuditSummary.TotalRequests, r.AuditSummary.FailedRequests, r.AuditSummary.FailoverRecovered, r.AuditSummary.TotalEvents))

	if len(r.RootCauses) > 0 {
		b.WriteString("\n#### 异常根因分析\n")
		for idx, item := range r.RootCauses {
			b.WriteString(fmt.Sprintf("%d. **[%s] %s（%d）**：%s\n", idx+1, strings.ToUpper(item.Severity), item.Title, item.Count, item.Summary))
			if len(item.AccountEmails) > 0 {
				b.WriteString(fmt.Sprintf("   - 示例账号: `%s`\n", strings.Join(item.AccountEmails, "`, `")))
			}
			if len(item.Proxies) > 0 {
				b.WriteString(fmt.Sprintf("   - 关联代理: `%s`\n", strings.Join(item.Proxies, "`, `")))
			}
		}
	} else {
		b.WriteString("\n#### 异常根因分析\n当前未发现明显故障根因，所有探测指标处于健康区间。\n")
	}

	b.WriteString("\n#### 建议处置动作\n")
	b.WriteString("- 优先执行 **一键智能自愈**（`smart_heal`）快速恢复冷却与代理绑定。\n")
	if r.PoolStats.Active == 0 {
		b.WriteString("- 当前无激活账号，可执行 **补充匿名 UUID**（`topup_free_uuid`）立即恢复文本对话能力。\n")
	}
	if r.PoolStats.Banned > 0 {
		b.WriteString("- 已封禁账号不可续期，请点击 **停用封禁账号** 或确认后执行 **清理删除封禁账号**。\n")
	}
	return b.String()
}

// Diagnostics 返回全平台 5 类根因诊断报告及离线可读 Markdown 摘要。
func (h *AdminHandler) Diagnostics(c *gin.Context) {
	c.JSON(http.StatusOK, h.BuildDiagnosticsReport())
}

// ExecuteDiagnosticRepair 执行诊断后的安全自愈或破坏性清理修复动作。
func (h *AdminHandler) ExecuteDiagnosticRepair(c *gin.Context) {
	var body struct {
		Action   string `json:"action"`
		Provider string `json:"provider"`
		Confirm  bool   `json:"confirm"`
		Count    int    `json:"count"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "请求 JSON 格式无效: " + err.Error()}})
		return
	}
	action := strings.ToLower(strings.TrimSpace(body.Action))
	if action == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "缺少 action 参数"}})
		return
	}
	providerID := strings.ToLower(strings.TrimSpace(body.Provider))
	if providerID == "" {
		providerID = "chatgpt"
	}
	p, ok := h.registry.Get(providerID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": gin.H{"message": "未找到供应商插件: " + providerID}})
		return
	}
	pool := h.accountPool()
	if pool == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "号池运行时未初始化"}})
		return
	}

	count := body.Count
	if count <= 0 {
		count = 5
	} else if count > 50 {
		count = 50
	}

	details := map[string]interface{}{}
	var summary string

	switch action {
	case "rebind_proxies":
		migrated, recovered := h.repairRebindProxies(c, pool)
		details["migrated"] = migrated
		details["recovered"] = recovered
		summary = fmt.Sprintf("代理修复完成：迁移 %d 个账号到健康节点，解除 %d 个代理风控冷却。", migrated, recovered)

	case "renew_expired":
		refreshed, enqueued, resetCount := h.repairRenewExpired(p, pool)
		details["refreshed"] = refreshed
		details["enqueued_kami"] = enqueued
		details["reset_backoff"] = resetCount
		summary = fmt.Sprintf("过期账号修复已触发：同步刷新 %d 个账号，重置退避并入队 %d 个卡密账号。", refreshed, enqueued)

	case "probe_all":
		probed, passed, failed, _ := probeAllProviderAccounts(p)
		details["probed"] = probed
		details["passed"] = passed
		details["failed"] = failed
		summary = fmt.Sprintf("全量测活完成：共探测 %d 个账号，通过 %d 个，失败 %d 个。", probed, passed, failed)

	case "topup_free_uuid":
		added, err := h.repairTopupFreeUUID(p, count)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": err.Error()}})
			return
		}
		details["added"] = added
		summary = fmt.Sprintf("已成功补充 %d 个匿名免登 UUID 账号。", added)

	case "disable_banned":
		disabled := h.repairDisableBanned(pool)
		details["disabled"] = disabled
		summary = fmt.Sprintf("已停用 %d 个封禁账号，防止继续参与请求与续期调度。", disabled)

	case "delete_banned":
		if !body.Confirm {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"message": "删除封禁账号属于破坏性操作，请显式传入 confirm: true 确认执行"},
			})
			return
		}
		deleted := h.repairDeleteBanned(p, pool)
		details["deleted"] = deleted
		summary = fmt.Sprintf("已永久清理删除 %d 个封禁账号。", deleted)

	case "smart_heal":
		details, summary = h.repairSmartHeal(c, p, pool)

	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "不支持的修复动作: " + action}})
		return
	}

	if logBuf := h.auditLog(); logBuf != nil {
		logBuf.RecordEvent("/admin/api/v1/diagnostics/repair", "", "", "repair_"+action, summary)
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":          true,
		"action":      action,
		"summary":     summary,
		"details":     details,
		"diagnostics": h.BuildDiagnosticsReport(),
	})
}

func (h *AdminHandler) repairRebindProxies(c *gin.Context, pool *accounts.Pool) (migrated, recovered int) {
	snaps := pool.ListSnapshots("", "", "")
	wafProxies := make(map[string]bool)
	wafBlockedIDs := make(map[string]bool)
	for _, snap := range snaps {
		reason := chatgpt.ClassifyAccountErrorReason(0, snap.LastError)
		if reason == chatgpt.ErrCategoryWAFProxy || reason == chatgpt.ErrCategoryProxyTransport {
			wafBlockedIDs[snap.ID] = true
			if snap.Proxy != "" {
				wafProxies[snap.Proxy] = true
			}
		}
	}

	if h.proxyPool != nil {
		probeCtx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
		defer cancel()
		_ = h.proxyPool.ProbeAll(probeCtx, proxy.DefaultProbeFunc, h.boundProxyCounts())
		migrated = pool.ReconcileProxies(
			func(u string) bool { return !h.proxyPool.IsHealthy(u) || wafProxies[u] },
			func(excl map[string]bool) string { return h.proxyPool.AllocateHealthy(excl) },
		)
	}

	latestSnaps := pool.ListSnapshots("", "", "")
	latestByID := make(map[string]accounts.AccountSnapshot, len(latestSnaps))
	for _, s := range latestSnaps {
		latestByID[s.ID] = s
	}
	for _, acct := range pool.AllPersistentAccounts() {
		if !wafBlockedIDs[acct.ID] {
			continue
		}
		snap, ok := latestByID[acct.ID]
		if !ok {
			continue
		}
		if h.proxyPool == nil || snap.Proxy == "" || h.proxyPool.IsHealthy(snap.Proxy) {
			if pool.RecoverProxyBlockedStatus(acct) {
				recovered++
			}
		}
	}
	return migrated, recovered
}

func (h *AdminHandler) repairRenewExpired(p provider.ProviderPlugin, pool *accounts.Pool) (refreshed, enqueued, resetCount int) {
	_ = pool.RecoverCooldowns()
	kw := h.kamiWorker()
	acctByID := make(map[string]*accounts.Account)
	for _, acct := range pool.AllPersistentAccounts() {
		acctByID[acct.ID] = acct
	}
	for _, snap := range pool.ListSnapshots("", "", "") {
		if snap.Status == "banned" || snap.Status == "disabled" {
			continue
		}
		acct := acctByID[snap.ID]
		if acct == nil {
			continue
		}
		if chatgpt.LooksLikeAccountBanned(0, snap.LastError) {
			pool.MarkBanned(acct, snap.LastError)
			continue
		}
		needRenew := snap.Status == "expired" || snap.TokenPreview == "" || snap.RenewState == "cooldown"
		if !needRenew {
			continue
		}
		if snap.HasRefreshToken || snap.HasSessionToken {
			if res, err := p.ExecuteAccountAction(snap.ID, "refresh"); err == nil {
				if okFlag, _ := res["ok"].(bool); okFlag {
					refreshed++
					continue
				}
			}
		}
		if snap.HasKamiCredentials {
			if pool.ResetRenewBackoff(acct) {
				resetCount++
			}
			if kw != nil {
				if kw.Enqueue(acct, "diag_repair_renew") {
					enqueued++
				}
			}
		}
	}
	return refreshed, enqueued, resetCount
}

func (h *AdminHandler) repairTopupFreeUUID(p provider.ProviderPlugin, count int) (int, error) {
	tokens := make([]string, 0, count)
	for i := 0; i < count; i++ {
		tokens = append(tokens, uuid.NewString())
	}
	res, err := p.ImportCredentials(provider.ImportRequest{
		CredentialType: "free_uuid",
		Tokens:         tokens,
	})
	if err != nil {
		return 0, err
	}
	return res.Added, nil
}

func (h *AdminHandler) repairDisableBanned(pool *accounts.Pool) int {
	disabled := 0
	for _, snap := range pool.ListSnapshots("", "", "") {
		if snap.Status == "banned" || (snap.Status != "disabled" && chatgpt.LooksLikeAccountBanned(0, snap.LastError)) {
			if pool.SetStatus(snap.ID, accounts.StatusDisabled) {
				disabled++
			}
		}
	}
	return disabled
}

func (h *AdminHandler) repairDeleteBanned(p provider.ProviderPlugin, pool *accounts.Pool) int {
	deleted := 0
	for _, snap := range pool.ListSnapshots("", "", "") {
		if snap.Status == "banned" || chatgpt.LooksLikeAccountBanned(0, snap.LastError) {
			if _, ok := pool.RemoveByID(snap.ID); ok {
				deleted++
			}
		}
	}
	if deleted > 0 {
		if persister, ok := p.(interface{ PersistAccessTokens() error }); ok {
			_ = persister.PersistAccessTokens()
		}
	}
	return deleted
}

func (h *AdminHandler) repairSmartHeal(c *gin.Context, p provider.ProviderPlugin, pool *accounts.Pool) (map[string]interface{}, string) {
	cooldownRecovered := pool.RecoverCooldowns()
	bannedDisabled := h.repairDisableBanned(pool)
	migrated, proxyRecovered := h.repairRebindProxies(c, pool)
	refreshed, enqueued, resetCount := h.repairRenewExpired(p, pool)

	emergencyUUIDs := 0
	if pool.Stats().Active == 0 {
		if added, err := h.repairTopupFreeUUID(p, 3); err == nil {
			emergencyUUIDs = added
		}
	}

	details := map[string]interface{}{
		"cooldown_recovered": cooldownRecovered,
		"banned_disabled":    bannedDisabled,
		"proxies_migrated":   migrated,
		"proxy_recovered":    proxyRecovered,
		"tokens_refreshed":   refreshed,
		"kami_enqueued":      enqueued,
		"backoff_reset":      resetCount,
		"emergency_uuids":    emergencyUUIDs,
	}
	summary := fmt.Sprintf(
		"一键智能自愈完成：恢复冷却 %d 个，停用封禁 %d 个，迁移代理 %d 个（解除风控 %d 个），刷新令牌 %d 个，卡密续期入队 %d 个，应急补充匿名 UUID %d 个。",
		cooldownRecovered, bannedDisabled, migrated, proxyRecovered, refreshed, enqueued, emergencyUUIDs,
	)
	return details, summary
}
