package proxy

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"ai-web-free/httpclient/bogdanfinn"

	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
)

const (
	defaultFailThreshold = 3
	defaultCooldown      = 5 * time.Minute
)

// NodeStatus 描述单个出口代理节点的实时健康状态与延迟统计。
type NodeStatus struct {
	URL               string `json:"url"`
	Status            string `json:"status"` // "active" | "cooling"
	LatencyMs         int64  `json:"latency_ms"`
	ConsecutiveErrors int    `json:"consecutive_errors"`
	TotalFailures     int64  `json:"total_failures"`
	LastChecked       string `json:"last_checked,omitempty"`
	LastError         string `json:"last_error,omitempty"`
	CooldownUntil     string `json:"cooldown_until,omitempty"`
	BoundAccounts     int    `json:"bound_accounts"`
}

type nodeState struct {
	status            string
	latencyMs         int64
	consecutiveErrors int
	totalFailures     int64
	lastChecked       time.Time
	lastError         string
	cooldownUntil     time.Time
}

// Pool 管理代理 IP 的分配、健康状态机与容灾切换。
type Pool struct {
	mu            sync.Mutex
	cursor        int
	ipv4List      []string
	nodes         map[string]*nodeState
	failThreshold int
	cooldown      time.Duration
}

func NewPool(ipv4Proxies []string) *Pool {
	if ipv4Proxies == nil {
		ipv4Proxies = []string{}
	}
	p := &Pool{
		ipv4List:      make([]string, 0, len(ipv4Proxies)),
		nodes:         make(map[string]*nodeState),
		failThreshold: defaultFailThreshold,
		cooldown:      defaultCooldown,
	}
	for _, raw := range ipv4Proxies {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		p.ipv4List = append(p.ipv4List, s)
		if _, exists := p.nodes[s]; !exists {
			p.nodes[s] = &nodeState{status: "active"}
		}
	}
	return p
}

// Allocate 返回一个代理 IP，优先选择健康节点按 round-robin 轮询。
func (p *Pool) Allocate() string {
	return p.AllocateHealthy(nil)
}

// AllocateHealthy 跳过 exclude 集合与冷却期内的失效代理；若全部冷却则返回任一非 exclude 代理兜底。
func (p *Pool) AllocateHealthy(exclude map[string]bool) string {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.ipv4List) == 0 {
		return ""
	}

	now := time.Now()
	p.recoverExpiredLocked(now)

	// 第 1 轮：找健康且不在 exclude 中的节点
	for i := 0; i < len(p.ipv4List); i++ {
		idx := (p.cursor + i) % len(p.ipv4List)
		candidate := p.ipv4List[idx]
		if exclude != nil && exclude[candidate] {
			continue
		}
		st := p.ensureNodeLocked(candidate)
		if st.status == "active" {
			p.cursor = (idx + 1) % len(p.ipv4List)
			return candidate
		}
	}

	// 第 2 轮：兜底非 exclude 节点
	for i := 0; i < len(p.ipv4List); i++ {
		idx := (p.cursor + i) % len(p.ipv4List)
		candidate := p.ipv4List[idx]
		if exclude != nil && exclude[candidate] {
			continue
		}
		p.cursor = (idx + 1) % len(p.ipv4List)
		return candidate
	}
	return ""
}

// Release 回收一个代理 IP。
func (p *Pool) Release(ip string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if ip != "" {
		log.Printf("[proxy] released %s", ip)
	}
}

// Count 返回可用 IPv4 代理数量。
func (p *Pool) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.ipv4List)
}

// List 返回当前代理池列表副本。
func (p *Pool) List() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.ipv4List))
	copy(out, p.ipv4List)
	return out
}

// Contains 检查指定代理是否在当前池列表中。
func (p *Pool) Contains(proxyURL string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, item := range p.ipv4List {
		if item == proxyURL {
			return true
		}
	}
	return false
}

// Replace 动态替换代理池列表，保留仍存在节点的历史健康信息。
func (p *Pool) Replace(proxies []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	nextList := make([]string, 0, len(proxies))
	seen := make(map[string]bool, len(proxies))
	for _, raw := range proxies {
		s := strings.TrimSpace(raw)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		nextList = append(nextList, s)
		p.ensureNodeLocked(s)
	}
	p.ipv4List = nextList
	p.cursor = 0
}

// ReportSuccess 重置代理连续失败计数并标记为健康。
func (p *Pool) ReportSuccess(proxyURL string, latencyMs int64) {
	if proxyURL == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.ensureNodeLocked(proxyURL)
	st.status = "active"
	st.consecutiveErrors = 0
	st.lastError = ""
	st.cooldownUntil = time.Time{}
	st.lastChecked = time.Now()
	if latencyMs > 0 {
		st.latencyMs = latencyMs
	}
}

// ReportFailure 上报一次代理传输错误，连续达到阈值则熔断并进入冷却，返回本次是否触发熔断。
func (p *Pool) ReportFailure(proxyURL, errMsg string) bool {
	if proxyURL == "" {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	st := p.ensureNodeLocked(proxyURL)
	st.consecutiveErrors++
	st.totalFailures++
	st.lastError = strings.TrimSpace(errMsg)
	st.lastChecked = time.Now()
	if st.consecutiveErrors >= p.failThreshold {
		wasActive := st.status == "active"
		st.status = "cooling"
		st.cooldownUntil = time.Now().Add(p.cooldown)
		return wasActive
	}
	return false
}

// IsHealthy 判断代理当前是否处于可用状态（冷却期满自动恢复）。
func (p *Pool) IsHealthy(proxyURL string) bool {
	if proxyURL == "" {
		return true
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	p.recoverExpiredLocked(now)
	st, ok := p.nodes[proxyURL]
	if !ok {
		return false
	}
	return st.status == "active"
}

// Snapshots 返回所有节点健康快照与绑定账号数。
func (p *Pool) Snapshots(boundCounts map[string]int) []NodeStatus {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	p.recoverExpiredLocked(now)

	out := make([]NodeStatus, 0, len(p.ipv4List))
	for _, u := range p.ipv4List {
		st := p.ensureNodeLocked(u)
		item := NodeStatus{
			URL:               u,
			Status:            st.status,
			LatencyMs:         st.latencyMs,
			ConsecutiveErrors: st.consecutiveErrors,
			TotalFailures:     st.totalFailures,
			LastError:         st.lastError,
			BoundAccounts:     boundCounts[u],
		}
		if !st.lastChecked.IsZero() {
			item.LastChecked = st.lastChecked.Format(time.RFC3339)
		}
		if !st.cooldownUntil.IsZero() {
			item.CooldownUntil = st.cooldownUntil.Format(time.RFC3339)
		}
		out = append(out, item)
	}
	return out
}

// ProbeFn 定义单节点连通性探测器，返回延迟毫秒数或错误。
type ProbeFn func(ctx context.Context, proxyURL string) (int64, error)

// ProbeAll 并发探测当前所有代理节点的连通性与延迟。
func (p *Pool) ProbeAll(ctx context.Context, probeFn ProbeFn, boundCounts map[string]int) []NodeStatus {
	if probeFn == nil {
		probeFn = DefaultProbeFunc
	}
	list := p.List()
	var wg sync.WaitGroup
	sem := make(chan struct{}, 5)

	for _, proxyURL := range list {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			latency, err := probeFn(ctx, u)
			if err != nil {
				p.mu.Lock()
				st := p.ensureNodeLocked(u)
				st.consecutiveErrors = p.failThreshold
				st.totalFailures++
				st.status = "cooling"
				st.cooldownUntil = time.Now().Add(p.cooldown)
				st.lastChecked = time.Now()
				st.lastError = err.Error()
				p.mu.Unlock()
			} else {
				p.ReportSuccess(u, latency)
			}
		}(proxyURL)
	}
	wg.Wait()
	return p.Snapshots(boundCounts)
}

// DefaultProbeFunc 先验证代理 URL 与 TCP 可达性，再通过代理发起轻量 HTTPS 请求测试出站延迟。
func DefaultProbeFunc(ctx context.Context, proxyURL string) (int64, error) {
	u, err := url.Parse(strings.TrimSpace(proxyURL))
	if err != nil || u.Host == "" {
		return 0, fmt.Errorf("invalid proxy url: %q", proxyURL)
	}
	start := time.Now()
	dialer := &net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return 0, fmt.Errorf("proxy dial failed: %w", err)
	}
	_ = conn.Close()

	baseClient, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(),
		tls_client.WithTimeoutSeconds(6),
		tls_client.WithClientProfile(profiles.Chrome_150),
	)
	if err != nil {
		return time.Since(start).Milliseconds(), nil
	}
	tc := &bogdanfinn.TlsClient{Client: baseClient}
	if err := tc.SetProxy(proxyURL); err != nil {
		return 0, err
	}
	resp, err := tc.Request(http.MethodGet, "https://chatgpt.com/cdn-cgi/trace", nil, nil, nil)
	if err != nil {
		return 0, fmt.Errorf("upstream reachability failed: %w", err)
	}
	_ = resp.Body.Close()
	latency := time.Since(start).Milliseconds()
	if latency <= 0 {
		latency = 1
	}
	return latency, nil
}

func (p *Pool) ensureNodeLocked(u string) *nodeState {
	st, ok := p.nodes[u]
	if !ok {
		st = &nodeState{status: "active"}
		p.nodes[u] = st
	}
	return st
}

func (p *Pool) recoverExpiredLocked(now time.Time) {
	for _, st := range p.nodes {
		if st.status == "cooling" && !st.cooldownUntil.IsZero() && !now.Before(st.cooldownUntil) {
			st.status = "active"
			st.consecutiveErrors = 0
			st.cooldownUntil = time.Time{}
			st.lastError = ""
		}
	}
}
