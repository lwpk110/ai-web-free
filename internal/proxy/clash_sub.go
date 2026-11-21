package proxy

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/goccy/go-yaml"
)

// DefaultClashExcludeRegex 默认过滤机场信息伪节点与对 OpenAI/Grok 风控较严或锁区的地区节点。
const DefaultClashExcludeRegex = `(?i)(剩余|流量|到期|官网|重置|群|套餐|网址|失联|维护|测试|CN|中国|HK|香港|MO|澳门|RU|俄罗斯|BY|白俄罗斯|IR|伊朗|KP|朝鲜)`

// DefaultClashPriorityRegions 默认优先分配的地区顺序。
const DefaultClashPriorityRegions = "US,JP,SG,TW,KR,DE,UK,NL,CA,AU"

// ClashSyncOptions 配置一次 Clash 订阅拉取与 Mihomo 多端口生成任务。
type ClashSyncOptions struct {
	SubURL          string            `json:"sub_url"`
	RawSubscription string            `json:"raw_subscription,omitempty"`
	TargetProfile   string            `json:"target_profile"`
	ListenHost      string            `json:"listen_host"`
	StartPort       int               `json:"start_port"`
	MaxPorts        int               `json:"max_ports"`
	ExcludeRegex    string            `json:"exclude_regex"`
	PriorityRegions string            `json:"priority_regions"`
	ConfigPath      string            `json:"config_path,omitempty"`
	MihomoAPIURL    string            `json:"mihomo_api_url,omitempty"`
	MihomoAPISecret string            `json:"mihomo_api_secret,omitempty"`
	WriteConfig     bool              `json:"write_config"`
	ReloadMihomo    bool              `json:"reload_mihomo"`
	ExistingPorts   map[string]int    `json:"-"` // node_name -> port (用于保持同节点名端口号稳定不漂移)
	HTTPClient      *http.Client      `json:"-"`
}

// ClashMappedPort 描述单个分配到本地端口的机场节点映射关系。
type ClashMappedPort struct {
	Port     int    `json:"port"`
	ProxyURL string `json:"proxy_url"`
	NodeName string `json:"node_name"`
	Region   string `json:"region"`
	Protocol string `json:"protocol"`
	Server   string `json:"server,omitempty"`
}

// ClashSyncResult 汇总一次 Clash 订阅转换与多端口生成结果。
type ClashSyncResult struct {
	TargetProfile   string            `json:"target_profile"`
	TotalParsed     int               `json:"total_parsed"`
	FilteredOut     int               `json:"filtered_out"`
	MappedCount     int               `json:"mapped_count"`
	StartPort       int               `json:"start_port"`
	EndPort         int               `json:"end_port"`
	MappedNodes     []ClashMappedPort `json:"mapped_nodes"`
	ConfigPath      string            `json:"config_path,omitempty"`
	ConfigWritten   bool              `json:"config_written"`
	MihomoReloaded  bool              `json:"mihomo_reloaded"`
	ReloadMethod    string            `json:"reload_method,omitempty"`
	ReloadWarning   string            `json:"reload_warning,omitempty"`
	GeneratedConfig string            `json:"generated_config,omitempty"`
	SyncedAt        string            `json:"synced_at"`
}

type clashYAMLDoc struct {
	Proxies []map[string]any `yaml:"proxies"`
}

type mihomoListener struct {
	Name   string `yaml:"name" json:"name"`
	Type   string `yaml:"type" json:"type"`
	Port   int    `yaml:"port" json:"port"`
	Listen string `yaml:"listen" json:"listen"`
	Proxy  string `yaml:"proxy" json:"proxy"`
}

type mihomoGeneratedConfig struct {
	MixedPort          int              `yaml:"mixed-port"`
	AllowLan           bool             `yaml:"allow-lan"`
	BindAddress        string           `yaml:"bind-address"`
	Mode               string           `yaml:"mode"`
	LogLevel           string           `yaml:"log-level"`
	IPv6               bool             `yaml:"ipv6"`
	ExternalController string           `yaml:"external-controller"`
	Secret             string           `yaml:"secret,omitempty"`
	Proxies            []map[string]any `yaml:"proxies"`
	Listeners          []mihomoListener `yaml:"listeners"`
}

// SyncClashSubscription 拉取或解析机场订阅，过滤并按地区优先级排序，生成 Mihomo 多端口 listeners 配置并可选热重载。
func SyncClashSubscription(ctx context.Context, opts ClashSyncOptions) (*ClashSyncResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.TargetProfile == "" {
		opts.TargetProfile = "vps"
	}
	opts.TargetProfile = strings.ToLower(strings.TrimSpace(opts.TargetProfile))
	if opts.ListenHost == "" {
		opts.ListenHost = "127.0.0.1"
	}
	if opts.StartPort <= 0 {
		opts.StartPort = 20001
	}
	if opts.MaxPorts <= 0 {
		opts.MaxPorts = 40
	}
	if opts.MaxPorts > 500 {
		opts.MaxPorts = 500
	}
	if strings.TrimSpace(opts.ExcludeRegex) == "" {
		opts.ExcludeRegex = DefaultClashExcludeRegex
	}
	if strings.TrimSpace(opts.PriorityRegions) == "" {
		opts.PriorityRegions = DefaultClashPriorityRegions
	}

	rawBody := strings.TrimSpace(opts.RawSubscription)
	if rawBody == "" {
		subURL := strings.TrimSpace(opts.SubURL)
		if subURL == "" {
			return nil, fmt.Errorf("未配置机场订阅链接 (CLASH_SUB_URL)")
		}
		fetched, err := fetchSubscriptionContent(ctx, subURL, opts.HTTPClient)
		if err != nil {
			return nil, err
		}
		rawBody = fetched
	}

	rawProxies, err := ParseSubscriptionProxies(rawBody)
	if err != nil {
		return nil, err
	}
	if len(rawProxies) == 0 {
		return nil, fmt.Errorf("未从订阅内容中解析出任何有效代理节点")
	}

	var excludeRe *regexp.Regexp
	if pattern := strings.TrimSpace(opts.ExcludeRegex); pattern != "" && pattern != "-" && pattern != "none" {
		compiled, regErr := regexp.Compile(pattern)
		if regErr != nil {
			return nil, fmt.Errorf("排除节点正则表达式无效 (%s): %w", pattern, regErr)
		}
		excludeRe = compiled
	}

	priorityMap := buildRegionPriorityMap(opts.PriorityRegions)

	type candidateNode struct {
		raw      map[string]any
		name     string
		region   string
		protocol string
		server   string
		rank     int
		origIdx  int
	}

	var candidates []candidateNode
	seenNames := make(map[string]bool)
	filteredOut := 0

	for idx, p := range rawProxies {
		name := strings.TrimSpace(fmt.Sprint(p["name"]))
		server := strings.TrimSpace(fmt.Sprint(p["server"]))
		pType := strings.ToLower(strings.TrimSpace(fmt.Sprint(p["type"])))
		if name == "" || name == "<nil>" || server == "" || server == "<nil>" || pType == "" || pType == "<nil>" {
			filteredOut++
			continue
		}
		if seenNames[name] {
			continue
		}
		if isInvalidInfoServer(server) {
			filteredOut++
			continue
		}
		if excludeRe != nil && excludeRe.MatchString(name) {
			filteredOut++
			continue
		}

		seenNames[name] = true
		region := DetectNodeRegion(name)
		rank, ok := priorityMap[region]
		if !ok {
			rank = 999
		}
		candidates = append(candidates, candidateNode{
			raw:      p,
			name:     name,
			region:   region,
			protocol: pType,
			server:   server,
			rank:     rank,
			origIdx:  idx,
		})
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf("订阅共解析 %d 个节点，但被过滤规则全部剔除（请检查 CLASH_EXCLUDE_REGEX）", len(rawProxies))
	}

	// 按地区优先级 -> 原始顺序稳定排序
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].rank != candidates[j].rank {
			return candidates[i].rank < candidates[j].rank
		}
		return candidates[i].origIdx < candidates[j].origIdx
	})

	if len(candidates) > opts.MaxPorts {
		candidates = candidates[:opts.MaxPorts]
	}

	// 分配端口号：优先保持 ExistingPorts 中同节点名的历史端口号（若落在 [StartPort, StartPort+MaxPorts) 区间内），防止订阅刷新导致粘性端口漂移
	minPort := opts.StartPort
	maxPort := opts.StartPort + len(candidates) - 1
	usedPorts := make(map[int]bool, len(candidates))
	assignedPorts := make([]int, len(candidates))

	if len(opts.ExistingPorts) > 0 {
		for i, c := range candidates {
			if oldPort, ok := opts.ExistingPorts[c.name]; ok && oldPort >= minPort && oldPort <= maxPort && !usedPorts[oldPort] {
				assignedPorts[i] = oldPort
				usedPorts[oldPort] = true
			}
		}
	}

	nextPort := minPort
	for i := range candidates {
		if assignedPorts[i] != 0 {
			continue
		}
		for usedPorts[nextPort] {
			nextPort++
		}
		assignedPorts[i] = nextPort
		usedPorts[nextPort] = true
	}

	// 构建最终结果与 Mihomo listeners
	type indexedAssignment struct {
		port int
		cand candidateNode
	}
	assignments := make([]indexedAssignment, len(candidates))
	for i, c := range candidates {
		assignments[i] = indexedAssignment{
			port: assignedPorts[i],
			cand: c,
		}
	}
	sort.Slice(assignments, func(i, j int) bool {
		return assignments[i].port < assignments[j].port
	})

	selectedProxies := make([]map[string]any, 0, len(assignments))
	listeners := make([]mihomoListener, 0, len(assignments))
	mappedNodes := make([]ClashMappedPort, 0, len(assignments))

	for _, item := range assignments {
		selectedProxies = append(selectedProxies, item.cand.raw)
		listeners = append(listeners, mihomoListener{
			Name:   fmt.Sprintf("pool-in-%d", item.port),
			Type:   "mixed",
			Port:   item.port,
			Listen: opts.ListenHost,
			Proxy:  item.cand.name,
		})
		proxyURL := fmt.Sprintf("http://%s:%d", opts.ListenHost, item.port)
		mappedNodes = append(mappedNodes, ClashMappedPort{
			Port:     item.port,
			ProxyURL: proxyURL,
			NodeName: item.cand.name,
			Region:   item.cand.region,
			Protocol: item.cand.protocol,
			Server:   item.cand.server,
		})
	}

	extCtrl := "127.0.0.1:9090"
	if apiURL := strings.TrimSpace(opts.MihomoAPIURL); apiURL != "" {
		if u, err := url.Parse(apiURL); err == nil && u.Host != "" {
			extCtrl = u.Host
		}
	}

	cfgDoc := mihomoGeneratedConfig{
		MixedPort:          7890,
		AllowLan:           false,
		BindAddress:        opts.ListenHost,
		Mode:               "rule",
		LogLevel:           "warning",
		IPv6:               false,
		ExternalController: extCtrl,
		Secret:             strings.TrimSpace(opts.MihomoAPISecret),
		Proxies:            selectedProxies,
		Listeners:          listeners,
	}

	yamlBytes, err := yaml.Marshal(cfgDoc)
	if err != nil {
		return nil, fmt.Errorf("序列化 Mihomo YAML 配置失败: %w", err)
	}
	header := fmt.Sprintf("# Generated by ai-web-free Clash Multi-Port Pool Generator at %s\n# Profile: %s | Mapped Ports: %d..%d (%d nodes)\n",
		time.Now().Format(time.RFC3339), opts.TargetProfile, minPort, maxPort, len(mappedNodes))
	fullYAML := header + string(yamlBytes)

	res := &ClashSyncResult{
		TargetProfile:   opts.TargetProfile,
		TotalParsed:     len(rawProxies),
		FilteredOut:     filteredOut,
		MappedCount:     len(mappedNodes),
		StartPort:       minPort,
		EndPort:         maxPort,
		MappedNodes:     mappedNodes,
		ConfigPath:      opts.ConfigPath,
		GeneratedConfig: fullYAML,
		SyncedAt:        time.Now().Format(time.RFC3339),
	}

	if opts.WriteConfig && strings.TrimSpace(opts.ConfigPath) != "" {
		if writeErr := writeConfigAtomic(opts.ConfigPath, []byte(fullYAML)); writeErr != nil {
			res.ReloadWarning = fmt.Sprintf("写入 Mihomo 配置文件 (%s) 失败: %v", opts.ConfigPath, writeErr)
		} else {
			res.ConfigWritten = true
		}
	}

	if opts.ReloadMihomo && res.ConfigWritten {
		method, reloadErr := reloadMihomoDaemon(ctx, opts.MihomoAPIURL, opts.MihomoAPISecret, opts.ConfigPath, opts.HTTPClient)
		if reloadErr != nil {
			res.ReloadWarning = reloadErr.Error()
		} else {
			res.MihomoReloaded = true
			res.ReloadMethod = method
		}
	}

	return res, nil
}

func fetchSubscriptionContent(ctx context.Context, subURL string, client *http.Client) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: 25 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, subURL, nil)
	if err != nil {
		return "", fmt.Errorf("无效的订阅 URL: %w", err)
	}
	// 使用 clash.meta / ClashVerge User-Agent，确保机场下发完整 Mihomo YAML 节点格式
	req.Header.Set("User-Agent", "clash-verge/v2.0.0 mihomo/1.18.0")
	req.Header.Set("Accept", "*/*")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("拉取机场订阅失败: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 15*1024*1024))
	if err != nil {
		return "", fmt.Errorf("读取订阅响应失败: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		preview := strings.TrimSpace(string(bodyBytes))
		if len(preview) > 200 {
			preview = preview[:200]
		}
		return "", fmt.Errorf("机场订阅返回 HTTP %d: %s", resp.StatusCode, preview)
	}
	return strings.TrimSpace(string(bodyBytes)), nil
}

// ParseSubscriptionProxies 支持从 Clash YAML 或 Base64 编码的通用 URI 订阅解析节点列表。
func ParseSubscriptionProxies(raw string) ([]map[string]any, error) {
	trimmed := strings.TrimSpace(strings.TrimPrefix(raw, "\xef\xbb\xbf"))
	if trimmed == "" {
		return nil, fmt.Errorf("订阅内容为空")
	}

	// 1. 若包含 proxies: 关键字，直接按 Clash YAML 解析
	if strings.Contains(trimmed, "proxies:") {
		var doc clashYAMLDoc
		if err := yaml.Unmarshal([]byte(trimmed), &doc); err == nil && len(doc.Proxies) > 0 {
			return doc.Proxies, nil
		}
	}

	// 2. 尝试 Base64 解码（可能解码出 Clash YAML 或多行 URI）
	if decoded, ok := tryDecodeBase64Subscription(trimmed); ok {
		if strings.Contains(decoded, "proxies:") {
			var doc clashYAMLDoc
			if err := yaml.Unmarshal([]byte(decoded), &doc); err == nil && len(doc.Proxies) > 0 {
				return doc.Proxies, nil
			}
		}
		if uriProxies := parseShareURIList(decoded); len(uriProxies) > 0 {
			return uriProxies, nil
		}
	}

	// 3. 直接按多行分享 URI (ss://, vmess://, trojan://, vless://, hysteria2://, hy2://) 解析
	if uriProxies := parseShareURIList(trimmed); len(uriProxies) > 0 {
		return uriProxies, nil
	}

	// 4. 兜底再次尝试 YAML Unmarshal
	var doc clashYAMLDoc
	if err := yaml.Unmarshal([]byte(trimmed), &doc); err != nil {
		return nil, fmt.Errorf("无法识别订阅格式（既非有效 Clash YAML 也非 Base64 节点列表）: %w", err)
	}
	return doc.Proxies, nil
}

func tryDecodeBase64Subscription(s string) (string, bool) {
	clean := strings.Map(func(r rune) rune {
		if r == '\r' || r == '\n' || r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, s)
	if len(clean) < 16 {
		return "", false
	}
	encodings := []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	}
	for _, enc := range encodings {
		if b, err := enc.DecodeString(clean); err == nil && len(b) > 0 {
			str := strings.TrimSpace(string(b))
			if strings.Contains(str, "://") || strings.Contains(str, "proxies:") {
				return str, true
			}
		}
	}
	return "", false
}

func parseShareURIList(text string) []map[string]any {
	var out []map[string]any
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if node, ok := parseSingleShareURI(line); ok {
			out = append(out, node)
		}
	}
	return out
}

func parseSingleShareURI(uriStr string) (map[string]any, bool) {
	lower := strings.ToLower(uriStr)
	switch {
	case strings.HasPrefix(lower, "vmess://"):
		return parseVmessURI(uriStr[8:])
	case strings.HasPrefix(lower, "trojan://"):
		return parseTrojanURI(uriStr)
	case strings.HasPrefix(lower, "ss://"):
		return parseShadowsocksURI(uriStr)
	case strings.HasPrefix(lower, "vless://"):
		return parseVlessURI(uriStr)
	case strings.HasPrefix(lower, "hysteria2://"), strings.HasPrefix(lower, "hy2://"):
		return parseHysteria2URI(uriStr)
	default:
		return nil, false
	}
}

func parseVmessURI(b64Part string) (map[string]any, bool) {
	decoded, ok := decodeAnyBase64(strings.TrimSpace(b64Part))
	if !ok {
		return nil, false
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(decoded), &payload); err != nil {
		return nil, false
	}
	server := strings.TrimSpace(fmt.Sprint(payload["add"]))
	port := parseAnyInt(payload["port"])
	uuid := strings.TrimSpace(fmt.Sprint(payload["id"]))
	name := strings.TrimSpace(fmt.Sprint(payload["ps"]))
	if server == "" || port <= 0 || uuid == "" {
		return nil, false
	}
	if name == "" || name == "<nil>" {
		name = fmt.Sprintf("vmess-%s:%d", server, port)
	}
	cipher := strings.TrimSpace(fmt.Sprint(payload["scy"]))
	if cipher == "" || cipher == "<nil>" {
		cipher = "auto"
	}
	node := map[string]any{
		"name":    name,
		"type":    "vmess",
		"server":  server,
		"port":    port,
		"uuid":    uuid,
		"alterId": parseAnyInt(payload["aid"]),
		"cipher":  cipher,
		"udp":     true,
	}
	if tlsVal := strings.TrimSpace(fmt.Sprint(payload["tls"])); strings.EqualFold(tlsVal, "tls") {
		node["tls"] = true
		if sni := strings.TrimSpace(fmt.Sprint(payload["sni"])); sni != "" && sni != "<nil>" {
			node["servername"] = sni
		}
	}
	if netType := strings.TrimSpace(fmt.Sprint(payload["net"])); netType == "ws" {
		node["network"] = "ws"
		wsOpts := map[string]any{}
		if path := strings.TrimSpace(fmt.Sprint(payload["path"])); path != "" && path != "<nil>" {
			wsOpts["path"] = path
		}
		if host := strings.TrimSpace(fmt.Sprint(payload["host"])); host != "" && host != "<nil>" {
			wsOpts["headers"] = map[string]any{"Host": host}
		}
		if len(wsOpts) > 0 {
			node["ws-opts"] = wsOpts
		}
	}
	return node, true
}

func parseTrojanURI(rawURI string) (map[string]any, bool) {
	u, err := url.Parse(rawURI)
	if err != nil || u.Hostname() == "" {
		return nil, false
	}
	port, _ := strconv.Atoi(u.Port())
	if port <= 0 {
		port = 443
	}
	password := u.User.Username()
	if password == "" {
		return nil, false
	}
	name, _ := url.QueryUnescape(u.Fragment)
	name = strings.TrimSpace(name)
	if name == "" {
		name = fmt.Sprintf("trojan-%s:%d", u.Hostname(), port)
	}
	node := map[string]any{
		"name":     name,
		"type":     "trojan",
		"server":   u.Hostname(),
		"port":     port,
		"password": password,
		"udp":      true,
	}
	q := u.Query()
	if sni := q.Get("sni"); sni != "" {
		node["sni"] = sni
	}
	if q.Get("allowInsecure") == "1" || strings.EqualFold(q.Get("allowInsecure"), "true") {
		node["skip-cert-verify"] = true
	}
	return node, true
}

func parseShadowsocksURI(rawURI string) (map[string]any, bool) {
	withoutScheme := strings.TrimPrefix(rawURI, "ss://")
	withoutScheme = strings.TrimPrefix(withoutScheme, "SS://")
	var name string
	if hashIdx := strings.Index(withoutScheme, "#"); hashIdx >= 0 {
		name, _ = url.QueryUnescape(withoutScheme[hashIdx+1:])
		withoutScheme = withoutScheme[:hashIdx]
	}
	if qIdx := strings.Index(withoutScheme, "?"); qIdx >= 0 {
		withoutScheme = withoutScheme[:qIdx]
	}

	var method, password, server string
	var port int

	if atIdx := strings.LastIndex(withoutScheme, "@"); atIdx >= 0 {
		userInfo := withoutScheme[:atIdx]
		hostPort := withoutScheme[atIdx+1:]
		if decoded, ok := decodeAnyBase64(userInfo); ok {
			userInfo = decoded
		}
		parts := strings.SplitN(userInfo, ":", 2)
		if len(parts) != 2 {
			return nil, false
		}
		method, password = parts[0], parts[1]
		h, pStr, err := splitHostPortSafe(hostPort)
		if err != nil {
			return nil, false
		}
		server = h
		port, _ = strconv.Atoi(pStr)
	} else {
		decoded, ok := decodeAnyBase64(withoutScheme)
		if !ok {
			return nil, false
		}
		atIdx := strings.LastIndex(decoded, "@")
		if atIdx < 0 {
			return nil, false
		}
		userInfo := decoded[:atIdx]
		hostPort := decoded[atIdx+1:]
		parts := strings.SplitN(userInfo, ":", 2)
		if len(parts) != 2 {
			return nil, false
		}
		method, password = parts[0], parts[1]
		h, pStr, err := splitHostPortSafe(hostPort)
		if err != nil {
			return nil, false
		}
		server = h
		port, _ = strconv.Atoi(pStr)
	}

	if server == "" || port <= 0 || method == "" || password == "" {
		return nil, false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = fmt.Sprintf("ss-%s:%d", server, port)
	}
	return map[string]any{
		"name":     name,
		"type":     "ss",
		"server":   server,
		"port":     port,
		"cipher":   method,
		"password": password,
		"udp":      true,
	}, true
}

func parseVlessURI(rawURI string) (map[string]any, bool) {
	u, err := url.Parse(rawURI)
	if err != nil || u.Hostname() == "" {
		return nil, false
	}
	port, _ := strconv.Atoi(u.Port())
	if port <= 0 {
		port = 443
	}
	uuid := u.User.Username()
	if uuid == "" {
		return nil, false
	}
	name, _ := url.QueryUnescape(u.Fragment)
	name = strings.TrimSpace(name)
	if name == "" {
		name = fmt.Sprintf("vless-%s:%d", u.Hostname(), port)
	}
	q := u.Query()
	node := map[string]any{
		"name":   name,
		"type":   "vless",
		"server": u.Hostname(),
		"port":   port,
		"uuid":   uuid,
		"udp":    true,
	}
	if sec := q.Get("security"); sec == "tls" || sec == "reality" {
		node["tls"] = true
		if sni := q.Get("sni"); sni != "" {
			node["servername"] = sni
		}
	}
	if flow := q.Get("flow"); flow != "" {
		node["flow"] = flow
	}
	if netType := q.Get("type"); netType != "" {
		node["network"] = netType
	}
	return node, true
}

func parseHysteria2URI(rawURI string) (map[string]any, bool) {
	normalized := rawURI
	if strings.HasPrefix(strings.ToLower(normalized), "hy2://") {
		normalized = "hysteria2://" + normalized[6:]
	}
	u, err := url.Parse(normalized)
	if err != nil || u.Hostname() == "" {
		return nil, false
	}
	port, _ := strconv.Atoi(u.Port())
	if port <= 0 {
		port = 443
	}
	password := u.User.Username()
	name, _ := url.QueryUnescape(u.Fragment)
	name = strings.TrimSpace(name)
	if name == "" {
		name = fmt.Sprintf("hy2-%s:%d", u.Hostname(), port)
	}
	node := map[string]any{
		"name":     name,
		"type":     "hysteria2",
		"server":   u.Hostname(),
		"port":     port,
		"password": password,
	}
	q := u.Query()
	if sni := q.Get("sni"); sni != "" {
		node["sni"] = sni
	}
	if q.Get("insecure") == "1" || strings.EqualFold(q.Get("insecure"), "true") {
		node["skip-cert-verify"] = true
	}
	return node, true
}

func splitHostPortSafe(hp string) (string, string, error) {
	idx := strings.LastIndex(hp, ":")
	if idx <= 0 || idx == len(hp)-1 {
		return "", "", fmt.Errorf("invalid host:port %q", hp)
	}
	host := strings.Trim(hp[:idx], "[]")
	port := hp[idx+1:]
	return host, port, nil
}

func decodeAnyBase64(s string) (string, bool) {
	clean := strings.TrimSpace(s)
	for _, enc := range []*base64.Encoding{
		base64.StdEncoding,
		base64.RawStdEncoding,
		base64.URLEncoding,
		base64.RawURLEncoding,
	} {
		if b, err := enc.DecodeString(clean); err == nil && len(b) > 0 {
			return string(b), true
		}
	}
	return "", false
}

func parseAnyInt(v any) int {
	switch tv := v.(type) {
	case int:
		return tv
	case int64:
		return int(tv)
	case float64:
		return int(tv)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(tv))
		return n
	default:
		n, _ := strconv.Atoi(strings.TrimSpace(fmt.Sprint(v)))
		return n
	}
}

func isInvalidInfoServer(server string) bool {
	s := strings.ToLower(strings.TrimSpace(server))
	return s == "" || s == "127.0.0.1" || s == "0.0.0.0" || s == "localhost" || s == "example.com"
}

// DetectNodeRegion 根据机场节点名称智能识别国家/地区代码。
func DetectNodeRegion(nodeName string) string {
	upper := strings.ToUpper(nodeName)
	rules := []struct {
		region   string
		keywords []string
	}{
		{"US", []string{"🇺🇸", "美国", "USA", "UNITED STATES", "LOS ANGELES", "SAN JOSE", "SEATTLE", "SILICON", "DALLAS", "CHICAGO", "NEW YORK", "MIAMI", "硅谷", "洛杉矶", "西雅图", "纽约", "达拉斯"}},
		{"JP", []string{"🇯🇵", "日本", "JAPAN", "TOKYO", "OSAKA", "东京", "大阪", "千叶"}},
		{"SG", []string{"🇸🇬", "新加坡", "SINGAPORE", "狮城"}},
		{"TW", []string{"🇹🇼", "台湾", "TAIWAN", "TAIPEI", "台北", "新北", "彰化"}},
		{"KR", []string{"🇰🇷", "韩国", "KOREA", "SEOUL", "首尔", "春川"}},
		{"DE", []string{"🇩🇪", "德国", "GERMANY", "FRANKFURT", "法兰克福"}},
		{"UK", []string{"🇬🇧", "英国", "UNITED KINGDOM", "LONDON", "伦敦"}},
		{"NL", []string{"🇳🇱", "荷兰", "NETHERLANDS", "AMSTERDAM", "阿姆斯特丹"}},
		{"CA", []string{"🇨🇦", "加拿大", "CANADA", "TORONTO", "VANCOUVER", "多伦多", "温哥华"}},
		{"AU", []string{"🇦🇺", "澳大利亚", "澳洲", "AUSTRALIA", "SYDNEY", "悉尼"}},
		{"FR", []string{"🇫🇷", "法国", "FRANCE", "PARIS", "巴黎"}},
		{"HK", []string{"🇭🇰", "香港", "HONG KONG", "HONGKONG"}},
		{"MO", []string{"🇲🇴", "澳门", "MACAU", "MACAO"}},
		{"CN", []string{"🇨🇳", "中国", "CHINA"}},
		{"RU", []string{"🇷🇺", "俄罗斯", "RUSSIA", "MOSCOW", "莫斯科"}},
	}

	for _, r := range rules {
		for _, kw := range r.keywords {
			if strings.Contains(upper, kw) {
				return r.region
			}
		}
	}

	// 单词边界短代码匹配 (如 "US 01", "[JP]", "SG-01")
	shortCodes := []string{"US", "JP", "SG", "TW", "KR", "DE", "UK", "GB", "NL", "CA", "AU", "FR", "HK", "MO", "CN", "RU"}
	for _, code := range shortCodes {
		re := regexp.MustCompile(`(?:^|[^A-Z0-9])` + code + `(?:[^A-Z0-9]|$)`)
		if re.MatchString(upper) {
			if code == "GB" {
				return "UK"
			}
			return code
		}
	}
	return "OTHER"
}

func buildRegionPriorityMap(priorityCSV string) map[string]int {
	out := make(map[string]int)
	parts := strings.Split(priorityCSV, ",")
	rank := 1
	for _, raw := range parts {
		code := strings.ToUpper(strings.TrimSpace(raw))
		if code == "" {
			continue
		}
		if _, exists := out[code]; !exists {
			out[code] = rank
			rank++
		}
	}
	return out
}

func writeConfigAtomic(path string, content []byte) error {
	dir := filepath.Dir(path)
	if dir != "." && dir != "/" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return os.WriteFile(path, content, 0644)
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if runtime.GOOS == "windows" {
		_ = os.Remove(path)
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
		return os.WriteFile(path, content, 0644)
	}
	return nil
}

func reloadMihomoDaemon(ctx context.Context, apiURL, secret, configPath string, client *http.Client) (string, error) {
	// 1. 优先通过 Mihomo External Controller REST API (PUT /configs?force=true) 零停机热重载
	cleanAPI := strings.TrimRight(strings.TrimSpace(apiURL), "/")
	if cleanAPI != "" {
		if client == nil {
			client = &http.Client{Timeout: 5 * time.Second}
		}
		absPath := configPath
		if abs, err := filepath.Abs(configPath); err == nil {
			absPath = abs
		}
		payload, _ := json.Marshal(map[string]string{"path": absPath})
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, cleanAPI+"/configs?force=true", bytes.NewReader(payload))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			if strings.TrimSpace(secret) != "" {
				req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(secret))
			}
			resp, doErr := client.Do(req)
			if doErr == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				if resp.StatusCode >= 200 && resp.StatusCode < 300 {
					return "mihomo_api", nil
				}
			}
		}
	}

	// 2. 在 Linux VPS 上回退执行 systemctl restart mihomo
	if runtime.GOOS == "linux" {
		cmdCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(cmdCtx, "systemctl", "restart", "mihomo")
		if out, err := cmd.CombinedOutput(); err == nil {
			return "systemctl", nil
		} else {
			return "", fmt.Errorf("Mihomo API (%s) 未就绪且 systemctl restart mihomo 失败: %v (%s)", cleanAPI, err, strings.TrimSpace(string(out)))
		}
	}

	return "", fmt.Errorf("Mihomo External Controller (%s) 未响应（配置已生成落盘，若在本地调试请确认 Mihomo 已启动）", cleanAPI)
}
