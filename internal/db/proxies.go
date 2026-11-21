package db

import (
	"crypto/sha1"
	"encoding/hex"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// NormalizeProfileName 规范化代理 Profile 名称（空字符串默认归入 "home"）。
func NormalizeProfileName(profile string) string {
	p := strings.ToLower(strings.TrimSpace(profile))
	if p == "" {
		return "home"
	}
	return p
}

func proxyID(rawURL string) string {
	h := sha1.Sum([]byte(strings.TrimSpace(rawURL)))
	return hex.EncodeToString(h[:8])
}

func proxyProfileID(profile, rawURL string) string {
	normProfile := NormalizeProfileName(profile)
	cleanURL := strings.TrimSpace(rawURL)
	if normProfile == "home" {
		// 保持与历史单 Profile 主键向后兼容
		return proxyID(cleanURL)
	}
	h := sha1.Sum([]byte(normProfile + "|" + cleanURL))
	return hex.EncodeToString(h[:10])
}

func accountProfileProxyID(profile, accountKey string) string {
	normProfile := NormalizeProfileName(profile)
	normKey := strings.ToLower(strings.TrimSpace(accountKey))
	h := sha1.Sum([]byte(normProfile + "|" + normKey))
	return hex.EncodeToString(h[:12])
}

func parseProtocolAndPort(rawURL string) (string, int) {
	proto := "http"
	port := 0
	if parsed, err := url.Parse(strings.TrimSpace(rawURL)); err == nil {
		if parsed.Scheme != "" {
			proto = strings.ToLower(parsed.Scheme)
		}
		if pStr := parsed.Port(); pStr != "" {
			if n, err := strconv.Atoi(pStr); err == nil && n > 0 {
				port = n
			}
		}
	}
	return proto, port
}

// ProfileSummary 描述数据库中单个 Runtime Profile 的代理节点统计。
type ProfileSummary struct {
	Profile    string `json:"profile"`
	Label      string `json:"label"`
	Total      int    `json:"total"`
	ClashCount int    `json:"clash_count"`
}

func profileLabel(profile string) string {
	switch NormalizeProfileName(profile) {
	case "home":
		return "家里开发环境 (home)"
	case "office":
		return "公司办公环境 (office)"
	case "vps":
		return "生产 VPS 多端口 (vps)"
	default:
		return profile
	}
}

// ListProxyProfiles 汇总数据库中所有已配置（以及内置标准 home/office/vps）的 Profile 概览。
func ListProxyProfiles(database *gorm.DB) []ProfileSummary {
	standard := []string{"home", "office", "vps"}
	counts := make(map[string]*ProfileSummary, len(standard))
	order := make([]string, 0, len(standard)+2)
	for _, p := range standard {
		counts[p] = &ProfileSummary{
			Profile: p,
			Label:   profileLabel(p),
		}
		order = append(order, p)
	}

	if database != nil {
		var records []ProxyNodeRecord
		if err := database.Where("status != ?", "disabled").Find(&records).Error; err == nil {
			for _, r := range records {
				p := NormalizeProfileName(r.Profile)
				sum, ok := counts[p]
				if !ok {
					sum = &ProfileSummary{
						Profile: p,
						Label:   profileLabel(p),
					}
					counts[p] = sum
					order = append(order, p)
				}
				sum.Total++
				if r.Source == "clash_sub" {
					sum.ClashCount++
				}
			}
		}
	}

	out := make([]ProfileSummary, 0, len(order))
	for _, p := range order {
		if sum, ok := counts[p]; ok {
			out = append(out, *sum)
		}
	}
	return out
}

// LoadProxyRecordsByProfile 读取指定 Profile 下的全部有效代理节点记录（含节点名、地区、端口元数据）。
func LoadProxyRecordsByProfile(database *gorm.DB, profile string) ([]ProxyNodeRecord, error) {
	if database == nil {
		return nil, nil
	}
	normProfile := NormalizeProfileName(profile)
	var records []ProxyNodeRecord
	q := database.Where("status != ?", "disabled")
	if normProfile == "home" {
		q = q.Where("(profile = ? OR profile = '' OR profile IS NULL)", normProfile)
	} else {
		q = q.Where("profile = ?", normProfile)
	}
	if err := q.Order("port asc, created_at asc").Find(&records).Error; err != nil {
		return nil, err
	}
	for i := range records {
		records[i].Profile = NormalizeProfileName(records[i].Profile)
		if records[i].Protocol == "" || records[i].Port == 0 {
			proto, port := parseProtocolAndPort(records[i].URL)
			if records[i].Protocol == "" {
				records[i].Protocol = proto
			}
			if records[i].Port == 0 {
				records[i].Port = port
			}
		}
	}
	return records, nil
}

// LoadProxiesByProfile 读取指定 Profile 下的全部有效代理 URL 列表。
func LoadProxiesByProfile(database *gorm.DB, profile string) ([]string, error) {
	records, err := LoadProxyRecordsByProfile(database, profile)
	if err != nil {
		return nil, err
	}
	urls := make([]string, 0, len(records))
	seen := make(map[string]bool, len(records))
	for _, r := range records {
		u := strings.TrimSpace(r.URL)
		if u != "" && !seen[u] {
			seen[u] = true
			urls = append(urls, u)
		}
	}
	return urls, nil
}

// LoadProxiesFromDB 从数据库读取默认 ("home") Profile 的全部有效代理 URL（保持旧函数签名兼容）。
func LoadProxiesFromDB(database *gorm.DB) ([]string, error) {
	return LoadProxiesByProfile(database, "home")
}

// SaveProxyRecordsByProfile 批量 Upsert 指定 Profile 的结构化代理节点记录。
func SaveProxyRecordsByProfile(database *gorm.DB, profile string, nodes []ProxyNodeRecord) error {
	if database == nil || len(nodes) == 0 {
		return nil
	}
	normProfile := NormalizeProfileName(profile)
	now := time.Now().UTC()
	records := make([]ProxyNodeRecord, 0, len(nodes))
	seenID := make(map[string]bool, len(nodes))

	for _, item := range nodes {
		clean := strings.TrimSpace(item.URL)
		if clean == "" || strings.HasPrefix(clean, "#") {
			continue
		}
		id := proxyProfileID(normProfile, clean)
		if seenID[id] {
			continue
		}
		seenID[id] = true

		proto, port := parseProtocolAndPort(clean)
		if item.Protocol != "" {
			proto = item.Protocol
		}
		if item.Port > 0 {
			port = item.Port
		}
		source := strings.TrimSpace(item.Source)
		if source == "" {
			source = "manual"
		}
		status := strings.TrimSpace(item.Status)
		if status == "" {
			status = "active"
		}

		records = append(records, ProxyNodeRecord{
			ID:        id,
			Profile:   normProfile,
			URL:       clean,
			NodeName:  strings.TrimSpace(item.NodeName),
			Region:    strings.ToUpper(strings.TrimSpace(item.Region)),
			Source:    source,
			Port:      port,
			Protocol:  proto,
			LatencyMs: item.LatencyMs,
			Status:    status,
			LastError: item.LastError,
			CreatedAt: now,
			UpdatedAt: now,
		})
	}
	if len(records) == 0 {
		return nil
	}
	return database.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"profile", "url", "node_name", "region", "source", "port", "protocol", "status", "updated_at",
		}),
	}).CreateInBatches(records, 100).Error
}

// SaveProxiesByProfile 批量增量写入指定 Profile 的代理 URL 列表。
func SaveProxiesByProfile(database *gorm.DB, profile string, urls []string) error {
	if database == nil || len(urls) == 0 {
		return nil
	}
	nodes := make([]ProxyNodeRecord, 0, len(urls))
	for _, raw := range urls {
		clean := strings.TrimSpace(raw)
		if clean == "" || strings.HasPrefix(clean, "#") {
			continue
		}
		nodes = append(nodes, ProxyNodeRecord{
			URL:    clean,
			Source: "manual",
			Status: "active",
		})
	}
	return SaveProxyRecordsByProfile(database, profile, nodes)
}

// SaveProxiesToDB 批量增量写入默认 ("home") Profile 代理列表。
func SaveProxiesToDB(database *gorm.DB, urls []string) error {
	return SaveProxiesByProfile(database, "home", urls)
}

// ReplaceProxiesByProfile 全量替换指定 Profile 下的代理列表（不影响其他 Profile 的节点数据）。
func ReplaceProxiesByProfile(database *gorm.DB, profile string, urls []string) error {
	if database == nil {
		return nil
	}
	normProfile := NormalizeProfileName(profile)
	// 先保留已有同 URL 节点的 NodeName / Region / Source / Port 元数据，避免手工保存列表时冲掉 Clash 订阅节点标签
	existingMeta := make(map[string]ProxyNodeRecord)
	if oldRecords, err := LoadProxyRecordsByProfile(database, normProfile); err == nil {
		for _, r := range oldRecords {
			existingMeta[strings.TrimSpace(r.URL)] = r
		}
	}

	if normProfile == "home" {
		_ = database.Where("profile = ? OR profile = '' OR profile IS NULL", normProfile).Delete(&ProxyNodeRecord{}).Error
	} else {
		_ = database.Where("profile = ?", normProfile).Delete(&ProxyNodeRecord{}).Error
	}

	nodes := make([]ProxyNodeRecord, 0, len(urls))
	for _, raw := range urls {
		clean := strings.TrimSpace(raw)
		if clean == "" || strings.HasPrefix(clean, "#") {
			continue
		}
		if old, ok := existingMeta[clean]; ok {
			old.Status = "active"
			nodes = append(nodes, old)
		} else {
			nodes = append(nodes, ProxyNodeRecord{
				URL:    clean,
				Source: "manual",
				Status: "active",
			})
		}
	}
	return SaveProxyRecordsByProfile(database, normProfile, nodes)
}

// ReplaceClashProxiesByProfile 刷新指定 Profile 下由 Clash 订阅同步生成的代理节点（source='clash_sub'），保留用户手工添加的节点。
func ReplaceClashProxiesByProfile(database *gorm.DB, profile string, clashNodes []ProxyNodeRecord) error {
	if database == nil {
		return nil
	}
	normProfile := NormalizeProfileName(profile)
	_ = database.Where("profile = ? AND source = ?", normProfile, "clash_sub").Delete(&ProxyNodeRecord{}).Error
	for i := range clashNodes {
		clashNodes[i].Source = "clash_sub"
	}
	return SaveProxyRecordsByProfile(database, normProfile, clashNodes)
}

// ReplaceProxiesInDB 全量替换默认 ("home") Profile 中的代理列表。
func ReplaceProxiesInDB(database *gorm.DB, urls []string) error {
	return ReplaceProxiesByProfile(database, "home", urls)
}

// DeleteProxyFromDBByProfile 从数据库删除指定 Profile 下的某个代理节点。
func DeleteProxyFromDBByProfile(database *gorm.DB, profile, rawURL string) error {
	if database == nil || strings.TrimSpace(rawURL) == "" {
		return nil
	}
	normProfile := NormalizeProfileName(profile)
	clean := strings.TrimSpace(rawURL)
	id := proxyProfileID(normProfile, clean)
	if normProfile == "home" {
		return database.Where("id = ? OR ((profile = 'home' OR profile = '' OR profile IS NULL) AND url = ?)", id, clean).
			Delete(&ProxyNodeRecord{}).Error
	}
	return database.Where("id = ? OR (profile = ? AND url = ?)", id, normProfile, clean).
		Delete(&ProxyNodeRecord{}).Error
}

// DeleteProxyFromDB 从默认 ("home") Profile 删除指定代理节点。
func DeleteProxyFromDB(database *gorm.DB, rawURL string) error {
	return DeleteProxyFromDBByProfile(database, "home", rawURL)
}

// ─── 按 Profile 隔离的账号粘性代理绑定持久化 ─────────────────────────

// LoadAccountProfileProxies 读取指定 Runtime Profile 下所有账号的粘性代理映射（key 为小写 account_key -> proxy_url）。
func LoadAccountProfileProxies(database *gorm.DB, profile string) map[string]string {
	out := make(map[string]string)
	if database == nil {
		return out
	}
	normProfile := NormalizeProfileName(profile)
	var rows []AccountProfileProxyRecord
	if err := database.Where("profile = ?", normProfile).Find(&rows).Error; err != nil {
		return out
	}
	for _, r := range rows {
		k := strings.ToLower(strings.TrimSpace(r.AccountKey))
		u := strings.TrimSpace(r.ProxyURL)
		if k != "" && u != "" {
			out[k] = u
		}
	}
	return out
}

// SaveAccountProfileProxiesBatch 批量保存指定 Runtime Profile 下的账号粘性代理映射。
func SaveAccountProfileProxiesBatch(database *gorm.DB, profile string, bindings map[string]string) error {
	if database == nil || len(bindings) == 0 {
		return nil
	}
	normProfile := NormalizeProfileName(profile)
	now := time.Now().UTC()
	records := make([]AccountProfileProxyRecord, 0, len(bindings))
	for rawKey, rawProxy := range bindings {
		k := strings.ToLower(strings.TrimSpace(rawKey))
		u := strings.TrimSpace(rawProxy)
		if k == "" || u == "" {
			continue
		}
		records = append(records, AccountProfileProxyRecord{
			ID:         accountProfileProxyID(normProfile, k),
			Profile:    normProfile,
			AccountKey: k,
			ProxyURL:   u,
			CreatedAt:  now,
			UpdatedAt:  now,
		})
	}
	if len(records) == 0 {
		return nil
	}
	return database.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"profile", "account_key", "proxy_url", "updated_at"}),
	}).CreateInBatches(records, 100).Error
}

// SaveAccountProfileProxy 保存单个账号在指定 Runtime Profile 下的粘性代理绑定。
func SaveAccountProfileProxy(database *gorm.DB, profile, accountKey, proxyURL string) error {
	k := strings.ToLower(strings.TrimSpace(accountKey))
	u := strings.TrimSpace(proxyURL)
	if database == nil || k == "" {
		return nil
	}
	normProfile := NormalizeProfileName(profile)
	id := accountProfileProxyID(normProfile, k)
	if u == "" {
		return database.Where("id = ?", id).Delete(&AccountProfileProxyRecord{}).Error
	}
	return SaveAccountProfileProxiesBatch(database, normProfile, map[string]string{k: u})
}
