package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ai-web-free/internal/accounts"
	"ai-web-free/internal/config"
	"ai-web-free/internal/db"
	"ai-web-free/internal/provider"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const bootstrapEnvTemplate = `# 极简引导层 .env (VPS_ENV_CONTENT 只需保留以下约 8 行)
SERVER_HOST=0.0.0.0
SERVER_PORT=18080
APP_PROFILE=vps
DATABASE_URL=postgresql://user:pass@ep-xxx.neon.tech/neondb?sslmode=require
DB_MATERIALIZE_ENABLED=true
DB_LOCAL_PATH=data/ai-web-free.db
ADMIN_TOKEN=请替换为管理台令牌
GATEWAY_KEY_SECRET=请替换为32位加密主密钥
`

// SystemSettingView 为管理台返回的单个系统配置项视图（敏感字段已脱敏）。
type SystemSettingView struct {
	Key                string `json:"key"`
	Label              string `json:"label"`
	Description        string `json:"description"`
	GroupTitle         string `json:"group_title"`
	Placeholder        string `json:"placeholder,omitempty"`
	Category           string `json:"category"`
	Type               string `json:"type"`
	DefaultValue       string `json:"default_value"`
	IsSecret           bool   `json:"is_secret"`
	IsFeatured         bool   `json:"is_featured"`
	ProfileOverridable bool   `json:"profile_overridable"`
	ReadOnly           bool   `json:"read_only"`
	EffectiveValue     string `json:"effective_value"`
	ScopeValue         string `json:"scope_value"`
	GlobalValue        string `json:"global_value"`
	Source             string `json:"source"` // "profile" | "global" | "env" | "default"
	HasScopeOverride   bool   `json:"has_scope_override"`
	HasGlobalValue     bool   `json:"has_global_value"`
	IsConfigured       bool   `json:"is_configured"`
	UpdatedAt          string `json:"updated_at,omitempty"`
	UpdatedBy          string `json:"updated_by,omitempty"`
}

func (h *AdminHandler) settingsDB() *gorm.DB {
	return db.GetDB()
}

func (h *AdminHandler) settingsActiveProfile() string {
	if h.proxyPool != nil {
		if p := strings.TrimSpace(h.proxyPool.ActiveProfile()); p != "" {
			return config.NormalizeProfile(p)
		}
	}
	if h.cfg != nil && strings.TrimSpace(h.cfg.AppProfile) != "" {
		return config.NormalizeProfile(h.cfg.AppProfile)
	}
	return "home"
}

func (h *AdminHandler) settingsSecret() string {
	return config.ResolveSettingsSecret(h.cfg)
}

// ReloadSystemSettings 从数据库加载最新系统配置，按优先级覆盖内存 *config.Config 并触发各组件热重载。
func (h *AdminHandler) ReloadSystemSettings() {
	database := h.settingsDB()
	if database == nil {
		return
	}
	activeProf := h.settingsActiveProfile()
	secret := h.settingsSecret()

	globalVals := decodeScopeRecordsMap(database, "global", secret)
	var profileVals map[string]string
	if activeProf != "" && activeProf != "global" {
		profileVals = decodeScopeRecordsMap(database, activeProf, secret)
	}

	effective, _ := config.BuildEffectiveMap(globalVals, profileVals)
	config.ApplyEffectiveMap(h.cfg, effective)

	if h.registry != nil && h.cfg != nil {
		provider.ApplyConfig(h.registry, h.cfg)
	}

	if pool := h.accountPool(); pool != nil {
		for _, acctType := range []accounts.AccountType{accounts.TypeFree, accounts.TypePUID} {
			pc := config.LoadPoolPolicy("chatgpt", acctType.String())
			pool.SetPolicy(acctType, accounts.PoolPolicy{
				ActiveTarget:   pc.ActiveTarget,
				MinHealthy:     pc.MinHealthy,
				MaxActive:      pc.MaxActive,
				RotateInterval: pc.RotateInterval,
				RotateRatio:    pc.RotateRatio,
				ActivateBatch:  pc.ActivateBatch,
			})
		}
		pool.Rebalance()
	}

	if h.proxyPool != nil && h.cfg != nil && h.cfg.ProxyURL != "" && !h.proxyPool.Contains(h.cfg.ProxyURL) {
		h.proxyPool.Replace(append(h.proxyPool.List(), h.cfg.ProxyURL))
	}
}

func decodeScopeRecordsMap(database *gorm.DB, scope, secret string) map[string]string {
	out := make(map[string]string)
	if database == nil {
		return out
	}
	recs, err := db.LoadSystemSettingsByScope(database, scope)
	if err != nil {
		return out
	}
	for k, rec := range recs {
		val, decErr := db.DecodeSettingValue(rec, secret)
		if decErr != nil {
			continue
		}
		out[k] = val
	}
	return out
}

func (h *AdminHandler) buildAvailableScopes(database *gorm.DB, activeProfile string) []string {
	seen := make(map[string]bool)
	var scopes []string
	add := func(s string) {
		norm := db.NormalizeSettingScope(s)
		if norm == "" || seen[norm] {
			return
		}
		seen[norm] = true
		scopes = append(scopes, norm)
	}
	add("global")
	for _, p := range db.ListProxyProfiles(database) {
		add(p.Profile)
	}
	for _, s := range db.ListSystemSettingScopes(database) {
		add(s)
	}
	if activeProfile != "" {
		add(activeProfile)
	}
	return scopes
}

func (h *AdminHandler) buildSystemSettingsPayload(reqScope string) gin.H {
	database := h.settingsDB()
	normScope := db.NormalizeSettingScope(reqScope)
	activeProfile := h.settingsActiveProfile()
	secret := h.settingsSecret()

	evalProfile := activeProfile
	if normScope != "global" {
		evalProfile = normScope
	}

	var globalRecs, evalRecs, scopeRecs map[string]db.SystemSettingRecord
	globalVals := make(map[string]string)
	evalVals := make(map[string]string)
	if database != nil {
		globalRecs, _ = db.LoadSystemSettingsByScope(database, "global")
		for k, rec := range globalRecs {
			if plain, err := db.DecodeSettingValue(rec, secret); err == nil {
				globalVals[k] = plain
			}
		}
		if evalProfile != "" && evalProfile != "global" {
			evalRecs, _ = db.LoadSystemSettingsByScope(database, evalProfile)
			for k, rec := range evalRecs {
				if plain, err := db.DecodeSettingValue(rec, secret); err == nil {
					evalVals[k] = plain
				}
			}
		}
	}

	if normScope == "global" {
		scopeRecs = globalRecs
	} else {
		scopeRecs = evalRecs
	}

	effectiveMap, sources := config.BuildEffectiveMap(globalVals, evalVals)
	catalog := config.Catalog()
	items := make([]SystemSettingView, 0, len(catalog))

	for _, spec := range catalog {
		rawEff := effectiveMap[spec.Key]
		if spec.ReadOnlyBootstrap && rawEff == "" && h.cfg != nil {
			rawEff = config.ExtractCurrentValue(h.cfg, spec.Key)
		}
		rawGlobal, hasGlobal := globalVals[spec.Key]

		var rawScope string
		var hasScope bool
		if normScope == "global" {
			rawScope, hasScope = rawGlobal, hasGlobal
		} else {
			rawScope, hasScope = evalVals[spec.Key]
		}

		var isConfigured bool
		if spec.Type == "bool" {
			isConfigured = strings.EqualFold(strings.TrimSpace(rawEff), "true")
		} else {
			isConfigured = strings.TrimSpace(rawEff) != ""
		}

		dispEff := rawEff
		dispScope := rawScope
		dispGlobal := rawGlobal
		if spec.IsSecret {
			dispEff = db.MaskSecretValue(rawEff)
			if hasScope {
				dispScope = db.MaskSecretValue(rawScope)
			}
			if hasGlobal {
				dispGlobal = db.MaskSecretValue(rawGlobal)
			}
		}

		var updatedAt, updatedBy string
		if rec, ok := scopeRecs[spec.Key]; ok {
			if !rec.UpdatedAt.IsZero() {
				updatedAt = rec.UpdatedAt.UTC().Format(time.RFC3339)
			}
			updatedBy = rec.UpdatedBy
		} else if normScope != "global" {
			if rec, ok := globalRecs[spec.Key]; ok {
				if !rec.UpdatedAt.IsZero() {
					updatedAt = rec.UpdatedAt.UTC().Format(time.RFC3339)
				}
				updatedBy = rec.UpdatedBy
			}
		}

		items = append(items, SystemSettingView{
			Key:                spec.Key,
			Label:              spec.Label,
			Description:        spec.Description,
			GroupTitle:         spec.GroupTitle,
			Placeholder:        spec.Placeholder,
			Category:           spec.Category,
			Type:               spec.Type,
			DefaultValue:       spec.DefaultValue,
			IsSecret:           spec.IsSecret,
			IsFeatured:         spec.IsFeatured,
			ProfileOverridable: spec.ProfileOverridable,
			ReadOnly:           spec.ReadOnlyBootstrap,
			EffectiveValue:     dispEff,
			ScopeValue:         dispScope,
			GlobalValue:        dispGlobal,
			Source:             sources[spec.Key],
			HasScopeOverride:   normScope != "global" && hasScope,
			HasGlobalValue:     hasGlobal,
			IsConfigured:       isConfigured,
			UpdatedAt:          updatedAt,
			UpdatedBy:          updatedBy,
		})
	}

	return gin.H{
		"scope":                  normScope,
		"active_profile":         activeProfile,
		"available_scopes":       h.buildAvailableScopes(database, activeProfile),
		"items":                  items,
		"bootstrap_env_template": bootstrapEnvTemplate,
	}
}

// GetSystemSettings 获取系统配置中心清单及各配置来源与生效值。
func (h *AdminHandler) GetSystemSettings(c *gin.Context) {
	scope := c.Query("scope")
	c.JSON(http.StatusOK, h.buildSystemSettingsPayload(scope))
}

type updateSettingsReq struct {
	Scope  string            `json:"scope"`
	Values map[string]string `json:"values"`
	DotEnv string            `json:"dotenv"`
}

func parseDotEnvText(raw string) map[string]string {
	parsed := make(map[string]string)
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "export ") {
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "export "))
		}
		eq := strings.IndexByte(trimmed, '=')
		if eq <= 0 {
			continue
		}
		k := strings.TrimSpace(trimmed[:eq])
		v := strings.TrimSpace(trimmed[eq+1:])
		if len(v) >= 2 && ((v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'')) {
			v = v[1 : len(v)-1]
		}
		if k != "" {
			parsed[k] = v
		}
	}
	return parsed
}

func validateSettingValue(spec config.SettingSpec, val string) error {
	trimmed := strings.TrimSpace(val)
	switch spec.Type {
	case "int":
		if trimmed == "" {
			return fmt.Errorf("配置项 %s (%s) 需要非负整数", spec.Label, spec.Key)
		}
		n, err := strconv.Atoi(trimmed)
		if err != nil || n < 0 {
			return fmt.Errorf("配置项 %s (%s) 需要非负整数", spec.Label, spec.Key)
		}
	case "float":
		if trimmed == "" {
			return fmt.Errorf("配置项 %s (%s) 需要有效数值", spec.Label, spec.Key)
		}
		if _, err := strconv.ParseFloat(trimmed, 64); err != nil {
			return fmt.Errorf("配置项 %s (%s) 需要有效数值", spec.Label, spec.Key)
		}
	case "bool":
		if _, err := strconv.ParseBool(trimmed); err != nil {
			return fmt.Errorf("配置项 %s (%s) 需要布尔值 (true / false)", spec.Label, spec.Key)
		}
	case "duration":
		if trimmed == "" {
			return fmt.Errorf("配置项 %s (%s) 需要时长格式（如 24h、30m 或 0）", spec.Label, spec.Key)
		}
		if trimmed != "0" {
			if _, err := time.ParseDuration(trimmed); err != nil {
				return fmt.Errorf("配置项 %s (%s) 时长格式无效（示例：24h、30m 或 0）", spec.Label, spec.Key)
			}
		}
	}
	return nil
}

// UpdateSystemSettings 批量写入数据库系统配置并触发全量热重载。
func (h *AdminHandler) UpdateSystemSettings(c *gin.Context) {
	database := h.settingsDB()
	if database == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "当前未挂载持久化数据库，无法保存系统配置"}})
		return
	}

	var req updateSettingsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "请求 JSON 格式无效: " + err.Error()}})
		return
	}

	merged := make(map[string]string)
	if strings.TrimSpace(req.DotEnv) != "" {
		for k, v := range parseDotEnvText(req.DotEnv) {
			merged[k] = v
		}
	}
	for k, v := range req.Values {
		merged[k] = v
	}

	targetScope := db.NormalizeSettingScope(req.Scope)
	var scopeItems []db.SystemSettingUpsertItem
	var globalItems []db.SystemSettingUpsertItem
	var updatedKeys []string

	for k, rawVal := range merged {
		spec, ok := config.FindSettingSpec(k)
		if !ok || spec.ReadOnlyBootstrap {
			continue
		}
		// 防止未解锁的掩码串意外覆盖真实密文
		if spec.IsSecret && strings.Contains(rawVal, "••••") {
			continue
		}
		if err := validateSettingValue(spec, rawVal); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": err.Error()}})
			return
		}
		val := strings.TrimSpace(rawVal)
		if spec.Type == "bool" {
			if b, err := strconv.ParseBool(val); err == nil {
				val = strconv.FormatBool(b)
			}
		}
		item := db.SystemSettingUpsertItem{
			Key:      spec.Key,
			Category: spec.Category,
			Value:    val,
			IsSecret: spec.IsSecret,
		}
		if targetScope != "global" && !spec.ProfileOverridable {
			globalItems = append(globalItems, item)
		} else {
			scopeItems = append(scopeItems, item)
		}
		updatedKeys = append(updatedKeys, spec.Key)
	}

	secret := h.settingsSecret()
	if len(globalItems) > 0 {
		if err := db.UpsertSystemSettings(database, "global", globalItems, secret, "admin_ui"); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "保存全局配置失败: " + err.Error()}})
			return
		}
	}
	if len(scopeItems) > 0 {
		if err := db.UpsertSystemSettings(database, targetScope, scopeItems, secret, "admin_ui"); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "保存配置失败: " + err.Error()}})
			return
		}
	}

	h.ReloadSystemSettings()

	if logBuf := h.auditLog(); logBuf != nil && len(updatedKeys) > 0 {
		logBuf.RecordEvent("/admin/api/v1/settings", "", "", fmt.Sprintf("settings_updated_scope_%s_count_%d", targetScope, len(updatedKeys)), "")
	}

	c.JSON(http.StatusOK, h.buildSystemSettingsPayload(targetScope))
}

// DeleteSystemSettingOverride 删除指定 Scope 下的配置项覆盖（恢复上一级继承或默认值），并触发热重载。
func (h *AdminHandler) DeleteSystemSettingOverride(c *gin.Context) {
	database := h.settingsDB()
	if database == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": gin.H{"message": "当前未挂载持久化数据库"}})
		return
	}
	scope := c.Query("scope")
	key := strings.TrimSpace(c.Query("key"))
	if key == "" {
		var body struct {
			Scope string `json:"scope"`
			Key   string `json:"key"`
		}
		if err := c.ShouldBindJSON(&body); err == nil {
			if scope == "" {
				scope = body.Scope
			}
			key = strings.TrimSpace(body.Key)
		}
	}
	if key == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "缺少要重置的 key 参数"}})
		return
	}

	normScope := db.NormalizeSettingScope(scope)
	if err := db.DeleteSystemSetting(database, normScope, key); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "恢复默认配置失败: " + err.Error()}})
		return
	}

	h.ReloadSystemSettings()

	if logBuf := h.auditLog(); logBuf != nil {
		logBuf.RecordEvent("/admin/api/v1/settings", "", "", fmt.Sprintf("settings_reset_%s_%s", normScope, key), "")
	}

	c.JSON(http.StatusOK, h.buildSystemSettingsPayload(normScope))
}

// RevealSystemSetting 显式揭露单个敏感配置的真实明文（支持编辑页即看即改）。
func (h *AdminHandler) RevealSystemSetting(c *gin.Context) {
	var body struct {
		Scope string `json:"scope"`
		Key   string `json:"key"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || strings.TrimSpace(body.Key) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"message": "请提供有效的配置 key"}})
		return
	}
	key := strings.TrimSpace(body.Key)
	normScope := db.NormalizeSettingScope(body.Scope)
	secret := h.settingsSecret()
	database := h.settingsDB()

	var plain, resolvedScope string
	var found bool
	if database != nil {
		var err error
		plain, resolvedScope, found, err = db.RevealSystemSetting(database, normScope, key, secret)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": gin.H{"message": "解密配置失败: " + err.Error()}})
			return
		}
	}
	if !found {
		if envVal, ok := config.LookupInitialEnv(key); ok {
			plain = envVal
			resolvedScope = "env"
			found = true
		} else if h.cfg != nil {
			if cur := config.ExtractCurrentValue(h.cfg, key); strings.TrimSpace(cur) != "" {
				plain = cur
				resolvedScope = "runtime"
				found = true
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"key":   key,
		"scope": resolvedScope,
		"value": plain,
		"found": found,
	})
}

// ExportSystemSettings 将有效系统配置导出为分类注释清晰的 .env 或 JSON 备份。
func (h *AdminHandler) ExportSystemSettings(c *gin.Context) {
	normScope := db.NormalizeSettingScope(c.Query("scope"))
	format := strings.ToLower(strings.TrimSpace(c.DefaultQuery("format", "dotenv")))
	includeSecrets := strings.EqualFold(strings.TrimSpace(c.DefaultQuery("include_secrets", "false")), "true")

	database := h.settingsDB()
	secret := h.settingsSecret()
	globalVals := decodeScopeRecordsMap(database, "global", secret)
	var profileVals map[string]string
	if normScope != "global" {
		profileVals = decodeScopeRecordsMap(database, normScope, secret)
	}
	effective, _ := config.BuildEffectiveMap(globalVals, profileVals)

	catalog := config.Catalog()
	if format == "json" {
		exportMap := make(map[string]string)
		for _, spec := range catalog {
			if spec.ReadOnlyBootstrap {
				continue
			}
			val := effective[spec.Key]
			if strings.TrimSpace(val) == "" {
				continue
			}
			if spec.IsSecret && !includeSecrets {
				val = db.MaskSecretValue(val)
			}
			exportMap[spec.Key] = val
		}
		raw, _ := json.MarshalIndent(exportMap, "", "  ")
		c.JSON(http.StatusOK, gin.H{
			"scope":   normScope,
			"format":  "json",
			"content": string(raw),
		})
		return
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("# ai-web-free 系统配置导出 (scope=%s, exported_at=%s)\n", normScope, time.Now().Format(time.RFC3339)))
	if !includeSecrets {
		sb.WriteString("# 注：敏感密钥已默认脱敏；如需明文备份请在导出弹窗勾选“包含真实明文密钥”\n")
	}
	lastGroup := ""
	for _, spec := range catalog {
		if spec.ReadOnlyBootstrap {
			continue
		}
		val := effective[spec.Key]
		if strings.TrimSpace(val) == "" {
			continue
		}
		if spec.GroupTitle != lastGroup {
			sb.WriteString(fmt.Sprintf("\n# ─── %s ───\n", spec.GroupTitle))
			lastGroup = spec.GroupTitle
		}
		if spec.IsSecret && !includeSecrets {
			val = db.MaskSecretValue(val)
		}
		if strings.ContainsAny(val, " \t#\"'") {
			val = strconv.Quote(val)
		}
		sb.WriteString(fmt.Sprintf("%s=%s\n", spec.Key, val))
	}

	c.JSON(http.StatusOK, gin.H{
		"scope":   normScope,
		"format":  "dotenv",
		"content": sb.String(),
	})
}
