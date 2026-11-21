package config

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ServerHost               string
	ServerPort               string
	TLSCert                  string
	TLSKey                   string
	Authorization            string
	AdminToken               string
	BaseURL                  string
	APIReverseProxy          string
	FilesReverseProxy        string
	StreamMode               bool
	MaxContinueCount         int
	EnableHistory            bool
	EnableExternalToken      bool // 是否接受外部传入的 accessToken
	ToolCallingEnabled       bool
	RefusalRetries           int
	DebugToolLog             string
	FreeAccounts             bool
	FreeAccountsNum          int
	ProxyURL                 string
	HTTPProxy                string
	DebugSentinel            bool
	AuditLogFile             string
	AuditLogMaxMB            int
	KamiAutoRenew            bool
	KamiHeadless             bool
	PromptSyncIntervalHours  int
	PromptPlazaFile          string
	DatabaseURL              string
	DBMaterializeEnabled     bool
	DBLocalPath              string
	DBSyncPushInterval       time.Duration
	DBSyncPullInterval       time.Duration
	DBSyncBatchSize          int
	DBSyncTimeout            time.Duration
	StorageDir               string
	S3Endpoint               string
	S3Bucket                 string
	S3AccessKey              string
	S3SecretKey              string
	S3Region                 string
	S3PublicURL              string
	SiliconFlowAPIKey        string
	DashScopeAPIKey          string
	GrokBaseURL              string
	GrokAccountsFile         string
	GrokSSOCookies           string
	GrokCredentialLLMEnabled bool
	GrokCredentialLLMModel   string
	LocalTTSBaseURL          string
	LocalTTSAPIKey           string
	LocalTTSModel            string
	RenderServiceURL         string
	QiniuAccessKey           string
	QiniuSecretKey           string
	QiniuBucket              string
	QiniuDomain              string
	QiniuRegion              string
	QiniuUploadURL           string
	AppProfile               string
	ClashSubURL              string
	ClashSubProfile          string
	ClashMultiportHost       string
	ClashMultiportStart      int
	ClashMultiportMax        int
	ClashExcludeRegex        string
	ClashPriorityRegions     string
	MihomoConfigPath         string
	MihomoAPIURL             string
	MihomoAPISecret          string
	ClashSyncIntervalHours   int
}

func Load() Config {
	return Config{
		ServerHost:               getEnv("SERVER_HOST", "0.0.0.0"),
		ServerPort:               getEnvWithFallback("SERVER_PORT", "PORT", "18080"),
		TLSCert:                  os.Getenv("TLS_CERT"),
		TLSKey:                   os.Getenv("TLS_KEY"),
		Authorization:            os.Getenv("Authorization"),
		AdminToken:               os.Getenv("ADMIN_TOKEN"),
		BaseURL:                  getEnv("BASE_URL", "https://chatgpt.com/backend-api"),
		APIReverseProxy:          os.Getenv("API_REVERSE_PROXY"),
		FilesReverseProxy:        os.Getenv("FILES_REVERSE_PROXY"),
		StreamMode:               getBoolEnv("STREAM_MODE", true),
		MaxContinueCount:         getIntEnv("MAX_CONTINUE_COUNT", 3),
		EnableHistory:            getBoolEnv("ENABLE_HISTORY", false),
		EnableExternalToken:      getBoolEnv("ENABLE_EXTERNAL_TOKEN", true),
		ToolCallingEnabled:       getBoolEnv("TOOL_CALLING_ENABLED", true),
		RefusalRetries:           getIntEnv("REFUSAL_RETRIES", 3),
		DebugToolLog:             os.Getenv("DEBUG_TOOL_LOG"),
		FreeAccounts:             getBoolEnv("FREE_ACCOUNTS", false),
		FreeAccountsNum:          getIntEnv("FREE_ACCOUNTS_NUM", 1024),
		ProxyURL:                 os.Getenv("PROXY_URL"),
		HTTPProxy:                os.Getenv("http_proxy"),
		DebugSentinel:            getBoolEnv("DEBUG_SENTINEL", false),
		AuditLogFile:             os.Getenv("AUDIT_LOG_FILE"),
		AuditLogMaxMB:            getIntEnv("AUDIT_LOG_MAX_MB", 10),
		KamiAutoRenew:            getBoolEnv("KAMI_AUTO_RENEW", true),
		KamiHeadless:             getKamiHeadlessDefault(),
		PromptSyncIntervalHours:  getIntEnv("PROMPT_SYNC_INTERVAL_HOURS", 6),
		PromptPlazaFile:          os.Getenv("PROMPT_PLAZA_FILE"),
		DatabaseURL:              getEnvWithFallback("DATABASE_URL", "DB_DSN", "data/ai-web-free.db"),
		DBMaterializeEnabled:     getBoolEnv("DB_MATERIALIZE_ENABLED", false),
		DBLocalPath:              getEnv("DB_LOCAL_PATH", "data/ai-web-free.db"),
		DBSyncPushInterval:       getDurationEnv("DB_SYNC_PUSH_INTERVAL", 5*time.Second),
		DBSyncPullInterval:       getDurationEnv("DB_SYNC_PULL_INTERVAL", 0),
		DBSyncBatchSize:          getIntEnv("DB_SYNC_BATCH_SIZE", 100),
		DBSyncTimeout:            getDurationEnv("DB_SYNC_TIMEOUT", 30*time.Second),
		StorageDir:               getEnv("STORAGE_DIR", "data/storage"),
		S3Endpoint:               os.Getenv("S3_ENDPOINT"),
		S3Bucket:                 os.Getenv("S3_BUCKET"),
		S3AccessKey:              os.Getenv("S3_ACCESS_KEY"),
		S3SecretKey:              os.Getenv("S3_SECRET_KEY"),
		S3Region:                 getEnv("S3_REGION", "auto"),
		S3PublicURL:              os.Getenv("S3_PUBLIC_URL"),
		SiliconFlowAPIKey:        os.Getenv("SILICONFLOW_API_KEY"),
		DashScopeAPIKey:          os.Getenv("DASHSCOPE_API_KEY"),
		GrokBaseURL:              getEnv("GROK_BASE_URL", "https://grok.com"),
		GrokAccountsFile:         getEnv("GROK_ACCOUNTS_FILE", "grok_accounts.txt"),
		GrokSSOCookies:           os.Getenv("GROK_SSO_COOKIES"),
		GrokCredentialLLMEnabled: getBoolEnv("GROK_CREDENTIAL_LLM_ENABLED", false),
		GrokCredentialLLMModel:   getEnv("GROK_CREDENTIAL_LLM_MODEL", "deepseek-ai/DeepSeek-R1-Distill-Qwen-7B"),
		LocalTTSBaseURL:          getEnvWithFallback("LOCAL_TTS_BASE_URL", "CUSTOM_TTS_BASE_URL", ""),
		LocalTTSAPIKey:           getEnvWithFallback("LOCAL_TTS_API_KEY", "CUSTOM_TTS_API_KEY", ""),
		LocalTTSModel:            getEnvWithFallback("LOCAL_TTS_MODEL", "CUSTOM_TTS_MODEL", "kokoro"),
		RenderServiceURL:         os.Getenv("RENDER_SERVICE_URL"),
		QiniuAccessKey:           os.Getenv("QINIU_ACCESS_KEY"),
		QiniuSecretKey:           os.Getenv("QINIU_SECRET_KEY"),
		QiniuBucket:              getEnv("QINIU_BUCKET", "ai-web-free-storage"),
		QiniuDomain:              os.Getenv("QINIU_DOMAIN"),
		QiniuRegion:              getEnv("QINIU_REGION", "z0"),
		QiniuUploadURL:           getEnv("QINIU_UPLOAD_URL", "https://upload.qiniup.com"),
		AppProfile:               NormalizeProfile(getEnvWithFallback("APP_PROFILE", "PROFILE", "home")),
		ClashSubURL:              strings.TrimSpace(os.Getenv("CLASH_SUB_URL")),
		ClashSubProfile:          NormalizeProfile(getEnv("CLASH_SUB_PROFILE", "vps")),
		ClashMultiportHost:       getEnv("CLASH_MULTIPORT_HOST", "127.0.0.1"),
		ClashMultiportStart:      getIntEnv("CLASH_MULTIPORT_START", 20001),
		ClashMultiportMax:        getIntEnv("CLASH_MULTIPORT_MAX", 40),
		ClashExcludeRegex:        getEnv("CLASH_EXCLUDE_REGEX", `(?i)(剩余|流量|到期|官网|重置|群|套餐|网址|失联|维护|测试|CN|中国|HK|香港|MO|澳门|RU|俄罗斯|BY|白俄罗斯|IR|伊朗|KP|朝鲜)`),
		ClashPriorityRegions:     getEnv("CLASH_PRIORITY_REGIONS", "US,JP,SG,TW,KR,DE,UK,NL,CA,AU"),
		MihomoConfigPath:         getEnv("MIHOMO_CONFIG_PATH", "/etc/mihomo/config.yaml"),
		MihomoAPIURL:             getEnv("MIHOMO_API_URL", "http://127.0.0.1:9090"),
		MihomoAPISecret:          strings.TrimSpace(os.Getenv("MIHOMO_API_SECRET")),
		ClashSyncIntervalHours:   getIntEnv("CLASH_SYNC_INTERVAL_HOURS", 12),
	}
}

// NormalizeProfile 规范化运行时 Profile 名称（空值默认回退为 "home"）。
func NormalizeProfile(raw string) string {
	p := strings.ToLower(strings.TrimSpace(raw))
	if p == "" {
		return "home"
	}
	return p
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getEnvWithFallback(key, fallbackKey, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	if v := os.Getenv(fallbackKey); v != "" {
		return v
	}
	return defaultVal
}

func getBoolEnv(key string, defaultVal bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return defaultVal
	}
	return b
}

func getIntEnv(key string, defaultVal int) int {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return defaultVal
	}
	return n
}

func getDurationEnv(key string, defaultVal time.Duration) time.Duration {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return defaultVal
	}
	if d, err := time.ParseDuration(v); err == nil {
		if d < 0 {
			return defaultVal
		}
		return d
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	return defaultVal
}

func getKamiHeadlessDefault() bool {
	if v := os.Getenv("KAMI_HEADLESS"); v != "" {
		return strings.EqualFold(v, "true") || v == "1"
	}
	// 在无图形环境（如无 DISPLAY 的 Linux 生产容器）下默认无头；桌面/调试环境下默认有头以方便人工过盾与封号呈现
	if runtime.GOOS == "linux" && os.Getenv("DISPLAY") == "" {
		return true
	}
	return false
}
