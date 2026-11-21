package config

import (
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SettingSpec 描述单个系统配置项的元数据，供数据库配置中心与管理台动态表单渲染使用。
type SettingSpec struct {
	Key                string `json:"key"`
	Label              string `json:"label"`
	Description        string `json:"description"`
	GroupTitle         string `json:"group_title"`
	Placeholder        string `json:"placeholder,omitempty"`
	Category           string `json:"category"` // gateway | multimodal | pool | proxy | storage | bootstrap
	Type               string `json:"type"`     // string | int | float | bool | duration
	DefaultValue       string `json:"default_value"`
	IsSecret           bool   `json:"is_secret"`
	IsFeatured         bool   `json:"is_featured"`
	ProfileOverridable bool   `json:"profile_overridable"`
	ReadOnlyBootstrap  bool   `json:"read_only"`
}

// Catalog 返回平台全量系统配置元数据清单（按分类与展示优先级排序）。
func Catalog() []SettingSpec {
	return []SettingSpec{
		// ─── 1. 多模态云端 Key & 通道 (multimodal) ──────────────────────
		{
			Key:          "SILICONFLOW_API_KEY",
			Label:        "硅基流动 (SiliconFlow) API Key",
			Description:  "支持逗号分隔多 Key 轮询。配置后立即解锁永久免费 FLUX.1 生图、CosyVoice2 声音与 CogVideoX 视频生成。",
			GroupTitle:   "免费云端模型供应商 Key",
			Placeholder:  "sk-...",
			Category:     "multimodal",
			Type:         "string",
			IsSecret:     true,
			IsFeatured:   true,
		},
		{
			Key:          "DASHSCOPE_API_KEY",
			Label:        "阿里云百炼 (DashScope) API Key",
			Description:  "支持逗号分隔多 Key 轮询。配置后解锁 CosyVoice 声音复刻、自然语言捏声音与通义万相生图。",
			GroupTitle:   "免费云端模型供应商 Key",
			Placeholder:  "sk-...",
			Category:     "multimodal",
			Type:         "string",
			IsSecret:     true,
			IsFeatured:   true,
		},
		{
			Key:                "GROK_SSO_COOKIES",
			Label:              "xAI Grok SSO Cookie",
			Description:        "可选默认 Grok Web 凭据（如 sso=...; sso-rw=...，逗号或分号分隔），热保存立即并入 Grok 号池。",
			GroupTitle:         "Grok Web 号池与凭据识别",
			Placeholder:        "sso=xxx; sso-rw=yyy",
			Category:           "multimodal",
			Type:               "string",
			IsSecret:           true,
			ProfileOverridable: true,
		},
		{
			Key:          "GROK_CREDENTIAL_LLM_ENABLED",
			Label:        "启用 LLM 智能归一化 Grok 凭据",
			Description:  "导入未知格式 Grok 凭据时，调用 SiliconFlow 轻量模型自动识别格式并沉淀离线规则（默认关闭）。",
			GroupTitle:   "Grok Web 号池与凭据识别",
			Category:     "multimodal",
			Type:         "bool",
			DefaultValue: "false",
		},
		{
			Key:          "GROK_CREDENTIAL_LLM_MODEL",
			Label:        "凭据归一化模型",
			Description:  "Grok 未知格式凭据自动解析所使用的 SiliconFlow 模型 ID。",
			GroupTitle:   "Grok Web 号池与凭据识别",
			Category:     "multimodal",
			Type:         "string",
			DefaultValue: "deepseek-ai/DeepSeek-R1-Distill-Qwen-7B",
		},
		{
			Key:                "LOCAL_TTS_BASE_URL",
			Label:              "自定义 OpenAI 兼容 TTS 端点",
			Description:        "可选外部云端或局域网 Kokoro / OpenAI 兼容 /v1/audio/speech 服务地址。",
			GroupTitle:         "自定义语音与视频渲染服务",
			Placeholder:        "http://127.0.0.1:8880",
			Category:           "multimodal",
			Type:               "string",
			ProfileOverridable: true,
		},
		{
			Key:         "LOCAL_TTS_API_KEY",
			Label:       "自定义 TTS 端点 API Key",
			Description: "对接外部 OpenAI 兼容语音服务时的鉴权 Bearer Key（可选）。",
			GroupTitle:  "自定义语音与视频渲染服务",
			Category:    "multimodal",
			Type:        "string",
			IsSecret:    true,
		},
		{
			Key:          "LOCAL_TTS_MODEL",
			Label:        "自定义 TTS 默认模型",
			Description:  "调用自定义 TTS 端点时默认传递的模型名称。",
			GroupTitle:   "自定义语音与视频渲染服务",
			Category:     "multimodal",
			Type:         "string",
			DefaultValue: "kokoro",
		},
		{
			Key:                "RENDER_SERVICE_URL",
			Label:              "短剧 FFmpeg 渲染服务地址",
			Description:        "可选外置 FFmpeg 音画合成服务端点（例如 http://127.0.0.1:8090）。",
			GroupTitle:         "自定义语音与视频渲染服务",
			Placeholder:        "http://127.0.0.1:8090",
			Category:           "multimodal",
			Type:               "string",
			ProfileOverridable: true,
		},

		// ─── 2. Clash 订阅与出站代理 (proxy) ────────────────────────────
		{
			Key:                "CLASH_SUB_URL",
			Label:              "Clash Verge 机场订阅链接",
			Description:        "支持 Clash YAML 与 Base64 节点订阅。配置后网关可一键/定时生成多端口代理池（建议在 vps Profile 下设置或全局共享）。",
			GroupTitle:         "Clash 订阅转 Mihomo 多端口池",
			Placeholder:        "https://your-airport.example.com/api/v1/client/subscribe?token=...",
			Category:           "proxy",
			Type:               "string",
			IsSecret:           true,
			IsFeatured:         true,
			ProfileOverridable: true,
		},
		{
			Key:                "PROXY_URL",
			Label:              "默认全局出站代理",
			Description:        "可选单条出站代理（如家里填 http://127.0.0.1:10809，公司填 http://192.168.2.12:10809），支持按 Profile 差异覆盖。",
			GroupTitle:         "基础出站代理",
			Placeholder:        "http://127.0.0.1:10809",
			Category:           "proxy",
			Type:               "string",
			IsFeatured:         true,
			ProfileOverridable: true,
		},
		{
			Key:                "CLASH_SUB_PROFILE",
			Label:              "订阅默认写入 Profile",
			Description:        "后台定时任务拉取机场订阅后，生成的多端口节点写入哪个数据库 Profile。",
			GroupTitle:         "Clash 订阅转 Mihomo 多端口池",
			Category:           "proxy",
			Type:               "string",
			DefaultValue:       "vps",
			ProfileOverridable: true,
		},
		{
			Key:                "CLASH_MULTIPORT_HOST",
			Label:              "多端口监听绑定地址",
			Description:        "Mihomo listeners 绑定地址，默认 127.0.0.1（强烈建议仅监听回环地址防止公网盗刷）。",
			GroupTitle:         "Clash 订阅转 Mihomo 多端口池",
			Category:           "proxy",
			Type:               "string",
			DefaultValue:       "127.0.0.1",
			ProfileOverridable: true,
		},
		{
			Key:                "CLASH_MULTIPORT_START",
			Label:              "多端口起始端口号",
			Description:        "Mihomo 分配给第一个机场节点的本地监听端口（默认 20001）。",
			GroupTitle:         "Clash 订阅转 Mihomo 多端口池",
			Category:           "proxy",
			Type:               "int",
			DefaultValue:       "20001",
			ProfileOverridable: true,
		},
		{
			Key:                "CLASH_MULTIPORT_MAX",
			Label:              "最大映射节点端口数",
			Description:        "最多从机场订阅中挑选前 N 个优质节点分别映射独立本地端口（默认 40）。",
			GroupTitle:         "Clash 订阅转 Mihomo 多端口池",
			Category:           "proxy",
			Type:               "int",
			DefaultValue:       "40",
			ProfileOverridable: true,
		},
		{
			Key:                "CLASH_PRIORITY_REGIONS",
			Label:              "地区优先级排序",
			Description:        "逗号分隔的国家/地区代码，靠前地区优先获得端口号（对 OpenAI/Grok 更友好）。",
			GroupTitle:         "节点过滤与地区优选",
			Category:           "proxy",
			Type:               "string",
			DefaultValue:       "US,JP,SG,TW,KR,DE,UK,NL,CA,AU",
			ProfileOverridable: true,
		},
		{
			Key:                "CLASH_EXCLUDE_REGEX",
			Label:              "过滤伪节点与高风控地区正则",
			Description:        "匹配到的节点名称将被自动剔除（默认剔除流量/到期提示及 CN/HK/MO/RU 等地区）。",
			GroupTitle:         "节点过滤与地区优选",
			Category:           "proxy",
			Type:               "string",
			DefaultValue:       `(?i)(剩余|流量|到期|官网|重置|群|套餐|网址|失联|维护|测试|CN|中国|HK|香港|MO|澳门|RU|俄罗斯|BY|白俄罗斯|IR|伊朗|KP|朝鲜)`,
			ProfileOverridable: true,
		},
		{
			Key:                "CLASH_SYNC_INTERVAL_HOURS",
			Label:              "订阅自动同步周期 (小时)",
			Description:        "后台定时刷新机场订阅并热重载 Mihomo 的间隔小时数（0 表示仅手动触发）。",
			GroupTitle:         "Mihomo 内核联动",
			Category:           "proxy",
			Type:               "int",
			DefaultValue:       "12",
			ProfileOverridable: true,
		},
		{
			Key:                "MIHOMO_CONFIG_PATH",
			Label:              "Mihomo 配置文件输出路径",
			Description:        "VPS 上默认 /etc/mihomo/config.yaml；Windows 本地调试可按 home Profile 覆盖为 data/mihomo_test.yaml。",
			GroupTitle:         "Mihomo 内核联动",
			Category:           "proxy",
			Type:               "string",
			DefaultValue:       "/etc/mihomo/config.yaml",
			ProfileOverridable: true,
		},
		{
			Key:                "MIHOMO_API_URL",
			Label:              "Mihomo External Controller 地址",
			Description:        "用于向本地 Mihomo 发送 PUT /configs?force=true 零停机热重载指令。",
			GroupTitle:         "Mihomo 内核联动",
			Category:           "proxy",
			Type:               "string",
			DefaultValue:       "http://127.0.0.1:9090",
			ProfileOverridable: true,
		},
		{
			Key:                "MIHOMO_API_SECRET",
			Label:              "Mihomo External Controller 密钥",
			Description:        "若 Mihomo 控制器配置了 secret 鉴权则在此填写（可选）。",
			GroupTitle:         "Mihomo 内核联动",
			Category:           "proxy",
			Type:               "string",
			IsSecret:           true,
			ProfileOverridable: true,
		},

		// ─── 3. 网关与对话协议 (gateway) ───────────────────────────────
		{
			Key:         "Authorization",
			Label:       "全局兼容 API 鉴权密钥",
			Description: "校验 /v1/* 请求的 Bearer Token（若使用网关分组 sk-gw-* Key 可留空）。",
			GroupTitle:  "接口安全与接入控制",
			Placeholder: "sk-awf-...",
			Category:    "gateway",
			Type:        "string",
			IsSecret:    true,
			IsFeatured:  true,
		},
		{
			Key:          "ENABLE_EXTERNAL_TOKEN",
			Label:        "允许外部透传 Access Token",
			Description:  "开启后允许客户端直接在 Authorization Header 传入自有 ChatGPT Access Token。",
			GroupTitle:   "接口安全与接入控制",
			Category:     "gateway",
			Type:         "bool",
			DefaultValue: "true",
		},
		{
			Key:          "STREAM_MODE",
			Label:        "启用 SSE 流式响应",
			Description:  "默认开启流式输出；关闭后强制返回完整非流式响应。",
			GroupTitle:   "对话与工具调用引擎",
			Category:     "gateway",
			Type:         "bool",
			DefaultValue: "true",
			IsFeatured:   true,
		},
		{
			Key:          "TOOL_CALLING_ENABLED",
			Label:        "启用工具调用模拟 (<tool_call>)",
			Description:  "为不支持原生 function calling 的免费网页模型注入 XML 协议模拟与自动纠偏。",
			GroupTitle:   "对话与工具调用引擎",
			Category:     "gateway",
			Type:         "bool",
			DefaultValue: "true",
		},
		{
			Key:          "MAX_CONTINUE_COUNT",
			Label:        "长输出自动续写最大轮数",
			Description:  "模型因长度限制截断时自动发送 continue 的最大轮数（0 表示关闭）。",
			GroupTitle:   "对话与工具调用引擎",
			Category:     "gateway",
			Type:         "int",
			DefaultValue: "3",
		},
		{
			Key:          "REFUSAL_RETRIES",
			Label:        "工具调用拒答自动重试次数",
			Description:  "模型陷入 sandbox 拒绝输出工具调用时的纠偏重试上限。",
			GroupTitle:   "对话与工具调用引擎",
			Category:     "gateway",
			Type:         "int",
			DefaultValue: "3",
		},
		{
			Key:          "ENABLE_HISTORY",
			Label:        "保留 ChatGPT 服务端上下文",
			Description:  "开启后复用上游会话历史上下文（默认关闭以保持无状态 OpenAI 兼容性）。",
			GroupTitle:   "对话与工具调用引擎",
			Category:     "gateway",
			Type:         "bool",
			DefaultValue: "false",
		},
		{
			Key:          "BASE_URL",
			Label:        "ChatGPT 上游 Base URL",
			Description:  "默认 https://chatgpt.com/backend-api，通常保持默认即可。",
			GroupTitle:   "上游反代与调试",
			Category:     "gateway",
			Type:         "string",
			DefaultValue: "https://chatgpt.com/backend-api",
		},
		{
			Key:         "API_REVERSE_PROXY",
			Label:       "API 反向代理覆盖地址",
			Description: "可选自定义转发到 ChatGPT backend-api 的反代 URL。",
			GroupTitle:  "上游反代与调试",
			Category:    "gateway",
			Type:        "string",
		},
		{
			Key:         "FILES_REVERSE_PROXY",
			Label:       "文件端点反代覆盖地址",
			Description: "可选自定义 /backend-api/files 上传下载反代 URL。",
			GroupTitle:  "上游反代与调试",
			Category:    "gateway",
			Type:        "string",
		},
		{
			Key:          "DEBUG_SENTINEL",
			Label:        "输出 Sentinel 过盾调试日志",
			Description:  "排查 403 / Turnstile / PoW 校验异常时开启，生产日常建议关闭。",
			GroupTitle:   "上游反代与调试",
			Category:     "gateway",
			Type:         "bool",
			DefaultValue: "false",
		},

		// ─── 4. 号池与热池策略 (pool) ─────────────────────────────────
		{
			Key:          "POOL_CHATGPT_FREE_ACTIVE_TARGET",
			Label:        "ChatGPT Free 热池目标活跃数",
			Description:  "同时保持在热池参与调度的活跃账号数（0=全部活跃不限制，其余待命减少风控面）。",
			GroupTitle:   "ChatGPT Free 热池/待命池策略",
			Category:     "pool",
			Type:         "int",
			DefaultValue: "15",
			IsFeatured:   true,
		},
		{
			Key:          "POOL_CHATGPT_FREE_MIN_HEALTHY",
			Label:        "ChatGPT Free 最低健康水位",
			Description:  "热池健康可用账号低于此阈值时自动从待命池激活补位。",
			GroupTitle:   "ChatGPT Free 热池/待命池策略",
			Category:     "pool",
			Type:         "int",
			DefaultValue: "8",
		},
		{
			Key:          "POOL_CHATGPT_FREE_MAX_ACTIVE",
			Label:        "ChatGPT Free 热池硬上限",
			Description:  "热池允许的最大活跃账号上限（0=不设硬上限）。",
			GroupTitle:   "ChatGPT Free 热池/待命池策略",
			Category:     "pool",
			Type:         "int",
			DefaultValue: "20",
		},
		{
			Key:          "POOL_CHATGPT_FREE_ROTATE_INTERVAL",
			Label:        "ChatGPT Free 热池定时轮换周期",
			Description:  "周期性将部分热池账号放回待命并换入新账号（如 24h，0 表示不轮换）。",
			GroupTitle:   "ChatGPT Free 热池/待命池策略",
			Category:     "pool",
			Type:         "duration",
			DefaultValue: "24h",
		},
		{
			Key:          "POOL_CHATGPT_FREE_ROTATE_RATIO",
			Label:        "ChatGPT Free 单次轮换比例",
			Description:  "每次轮换替换的热池账号比例 (0 ~ 1]，默认 0.2。",
			GroupTitle:   "ChatGPT Free 热池/待命池策略",
			Category:     "pool",
			Type:         "float",
			DefaultValue: "0.2",
		},
		{
			Key:          "POOL_CHATGPT_FREE_ACTIVATE_BATCH",
			Label:        "ChatGPT Free 单轮最大补位激活数",
			Description:  "每轮后台扫描允许触发自动续期/激活的最大账号数量。",
			GroupTitle:   "ChatGPT Free 热池/待命池策略",
			Category:     "pool",
			Type:         "int",
			DefaultValue: "5",
		},
		{
			Key:          "POOL_GROK_FREE_ACTIVE_TARGET",
			Label:        "Grok Free 热池目标活跃数",
			Description:  "Grok 号池同时保持活跃的账号数（0=不限制，保存立即动态重平衡）。",
			GroupTitle:   "Grok Free 热池策略",
			Category:     "pool",
			Type:         "int",
			DefaultValue: "0",
		},
		{
			Key:          "KAMI_AUTO_RENEW",
			Label:        "卡密自动浏览器续期",
			Description:  "定期扫描即将到期或过期的卡密账号（邮箱+密码+2FA）自动排队无头重登。",
			GroupTitle:   "账号自动运维与提示词广场",
			Category:     "pool",
			Type:         "bool",
			DefaultValue: "true",
		},
		{
			Key:                "KAMI_HEADLESS",
			Label:              "卡密登录使用无头浏览器",
			Description:        "Linux VPS 生产环境建议开启（true），Windows 桌面调试可关闭以人工观察过盾。",
			GroupTitle:         "账号自动运维与提示词广场",
			Category:           "pool",
			Type:               "bool",
			DefaultValue:       "true",
			ProfileOverridable: true,
		},
		{
			Key:          "FREE_ACCOUNTS",
			Label:        "启动生成匿名免登设备账号",
			Description:  "开启后冷启动自动生成一批免登 UUID 匿名号用于免费兜底对话。",
			GroupTitle:   "账号自动运维与提示词广场",
			Category:     "pool",
			Type:         "bool",
			DefaultValue: "false",
		},
		{
			Key:          "FREE_ACCOUNTS_NUM",
			Label:        "匿名免登设备账号生成数量",
			Description:  "FREE_ACCOUNTS=true 时生成的免登设备账号数量。",
			GroupTitle:   "账号自动运维与提示词广场",
			Category:     "pool",
			Type:         "int",
			DefaultValue: "1024",
		},
		{
			Key:          "PROMPT_SYNC_INTERVAL_HOURS",
			Label:        "提示词广场同步间隔 (小时)",
			Description:  "后台自动拉取热门生图灵感提示词的时间间隔（0=禁用自动拉取）。",
			GroupTitle:   "账号自动运维与提示词广场",
			Category:     "pool",
			Type:         "int",
			DefaultValue: "6",
		},

		// ─── 5. 对象存储与审计日志 (storage) ───────────────────────────
		{
			Key:                "STORAGE_DIR",
			Label:              "本地多媒体缓存目录",
			Description:        "生图、音频与短剧素材的本地磁盘存储路径。",
			GroupTitle:         "本地存储与审计日志",
			Category:           "storage",
			Type:               "string",
			DefaultValue:       "data/storage",
			ProfileOverridable: true,
		},
		{
			Key:                "AUDIT_LOG_FILE",
			Label:              "审计日志 JSONL 落盘路径",
			Description:        "留空仅驻留最近 1000 条内存环形缓冲；设置文件路径（如 data/audit.jsonl）后同步轮转写入。",
			GroupTitle:         "本地存储与审计日志",
			Placeholder:        "data/audit.jsonl",
			Category:           "storage",
			Type:               "string",
			ProfileOverridable: true,
		},
		{
			Key:          "AUDIT_LOG_MAX_MB",
			Label:        "单个审计文件轮转上限 (MB)",
			Description:  "触发日志文件自动轮转的体积阈值（MB）。",
			GroupTitle:   "本地存储与审计日志",
			Category:     "storage",
			Type:         "int",
			DefaultValue: "10",
		},
		{
			Key:         "QINIU_ACCESS_KEY",
			Label:       "七牛云 AccessKey",
			Description: "用于图片/音频/SQLite 自动热备至七牛云 Kodo 对象存储。",
			GroupTitle:  "七牛云 Kodo 对象存储",
			Category:    "storage",
			Type:        "string",
		},
		{
			Key:         "QINIU_SECRET_KEY",
			Label:       "七牛云 SecretKey",
			Description: "七牛云对象存储密钥（AES-GCM 加密保管）。",
			GroupTitle:  "七牛云 Kodo 对象存储",
			Category:    "storage",
			Type:        "string",
			IsSecret:    true,
		},
		{
			Key:          "QINIU_BUCKET",
			Label:        "七牛云 Bucket 空间名",
			Description:  "存储媒体与热备快照的目标 Bucket 名称。",
			GroupTitle:   "七牛云 Kodo 对象存储",
			Category:     "storage",
			Type:         "string",
			DefaultValue: "ai-web-free-storage",
		},
		{
			Key:         "QINIU_DOMAIN",
			Label:       "七牛云 CDN 外链域名",
			Description: "绑定到 Bucket 的访问域名（用于拼接公开媒体 URL）。",
			GroupTitle:  "七牛云 Kodo 对象存储",
			Category:    "storage",
			Type:        "string",
		},
		{
			Key:          "QINIU_REGION",
			Label:        "七牛云存储区域代码",
			Description:  "如 z0 (华东)、z1 (华北)、z2 (华南)、na0 (北美)、as0 (新加坡)。",
			GroupTitle:   "七牛云 Kodo 对象存储",
			Category:     "storage",
			Type:         "string",
			DefaultValue: "z0",
		},
		// ─── 6. Bootstrap 引导层只读预览 (bootstrap) ─────────────────────
		{
			Key:               "APP_PROFILE",
			Label:             "当前实例引导 Profile",
			Description:       "决定本机启动时激活哪个代理与配置环境（home / office / vps），仅由本地 .env 控制。",
			GroupTitle:        "进程启动引导参数（仅在 .env 配置，只读展示）",
			Category:          "bootstrap",
			Type:              "string",
			DefaultValue:      "home",
			ReadOnlyBootstrap: true,
		},
		{
			Key:               "SERVER_HOST",
			Label:             "HTTP 监听地址",
			Description:       "服务绑定的网卡 IP（默认 0.0.0.0）。",
			GroupTitle:        "进程启动引导参数（仅在 .env 配置，只读展示）",
			Category:          "bootstrap",
			Type:              "string",
			DefaultValue:      "0.0.0.0",
			ReadOnlyBootstrap: true,
		},
		{
			Key:               "SERVER_PORT",
			Label:             "HTTP 监听端口",
			Description:       "网关监听端口（默认 18080）。",
			GroupTitle:        "进程启动引导参数（仅在 .env 配置，只读展示）",
			Category:          "bootstrap",
			Type:              "string",
			DefaultValue:      "18080",
			ReadOnlyBootstrap: true,
		},
		{
			Key:               "DATABASE_URL",
			Label:             "持久化数据库连接串 (已脱敏)",
			Description:       "支持本地 SQLite 或云端 Neon PostgreSQL（用于跨设备共享号池与本配置中心数据）。",
			GroupTitle:        "进程启动引导参数（仅在 .env 配置，只读展示）",
			Category:          "bootstrap",
			Type:              "string",
			DefaultValue:      "data/ai-web-free.db",
			IsSecret:          true,
			ReadOnlyBootstrap: true,
		},
		{
			Key:               "DB_MATERIALIZE_ENABLED",
			Label:             "数据库物化双向同步模式",
			Description:       "连接云端 PostgreSQL 时启用本地 SQLite 毫秒级读写 + 异步双向同步。",
			GroupTitle:        "进程启动引导参数（仅在 .env 配置，只读展示）",
			Category:          "bootstrap",
			Type:              "bool",
			DefaultValue:      "false",
			ReadOnlyBootstrap: true,
		},
		{
			Key:               "ADMIN_TOKEN",
			Label:             "管理后台解锁密钥",
			Description:       "保护 /admin/api/v1/* 与本配置中心，同时用于派生配置加密密钥。",
			GroupTitle:        "进程启动引导参数（仅在 .env 配置，只读展示）",
			Category:          "bootstrap",
			Type:              "string",
			IsSecret:          true,
			ReadOnlyBootstrap: true,
		},
		{
			Key:               "CF_TUNNEL_TOKEN",
			Label:             "Cloudflare Tunnel 穿透 Token",
			Description:       "VPS 部署脚本检测到此值时会自动安装并托管 cloudflared 域名穿透服务。",
			GroupTitle:        "进程启动引导参数（仅在 .env 配置，只读展示）",
			Category:          "bootstrap",
			Type:              "string",
			IsSecret:          true,
			ReadOnlyBootstrap: true,
		},
	}
}

// FindSettingSpec 按变量名查找配置元数据定义。
func FindSettingSpec(key string) (SettingSpec, bool) {
	target := strings.TrimSpace(key)
	for _, spec := range Catalog() {
		if strings.EqualFold(spec.Key, target) {
			return spec, true
		}
	}
	return SettingSpec{}, false
}

// ExtractCurrentValue 从当前运行时 Config 或环境变量提取指定键的字符串表示。
func ExtractCurrentValue(cfg *Config, key string) string {
	if cfg == nil {
		return os.Getenv(key)
	}
	switch key {
	case "SERVER_HOST":
		return cfg.ServerHost
	case "SERVER_PORT":
		return cfg.ServerPort
	case "APP_PROFILE":
		return cfg.AppProfile
	case "DATABASE_URL":
		return cfg.DatabaseURL
	case "DB_MATERIALIZE_ENABLED":
		return strconv.FormatBool(cfg.DBMaterializeEnabled)
	case "ADMIN_TOKEN":
		return cfg.AdminToken
	case "CF_TUNNEL_TOKEN":
		return os.Getenv("CF_TUNNEL_TOKEN")
	case "Authorization":
		return cfg.Authorization
	case "BASE_URL":
		return cfg.BaseURL
	case "API_REVERSE_PROXY":
		return cfg.APIReverseProxy
	case "FILES_REVERSE_PROXY":
		return cfg.FilesReverseProxy
	case "STREAM_MODE":
		return strconv.FormatBool(cfg.StreamMode)
	case "MAX_CONTINUE_COUNT":
		return strconv.Itoa(cfg.MaxContinueCount)
	case "ENABLE_HISTORY":
		return strconv.FormatBool(cfg.EnableHistory)
	case "ENABLE_EXTERNAL_TOKEN":
		return strconv.FormatBool(cfg.EnableExternalToken)
	case "TOOL_CALLING_ENABLED":
		return strconv.FormatBool(cfg.ToolCallingEnabled)
	case "REFUSAL_RETRIES":
		return strconv.Itoa(cfg.RefusalRetries)
	case "FREE_ACCOUNTS":
		return strconv.FormatBool(cfg.FreeAccounts)
	case "FREE_ACCOUNTS_NUM":
		return strconv.Itoa(cfg.FreeAccountsNum)
	case "PROXY_URL":
		return cfg.ProxyURL
	case "DEBUG_SENTINEL":
		return strconv.FormatBool(cfg.DebugSentinel)
	case "AUDIT_LOG_FILE":
		return cfg.AuditLogFile
	case "AUDIT_LOG_MAX_MB":
		return strconv.Itoa(cfg.AuditLogMaxMB)
	case "KAMI_AUTO_RENEW":
		return strconv.FormatBool(cfg.KamiAutoRenew)
	case "KAMI_HEADLESS":
		return strconv.FormatBool(cfg.KamiHeadless)
	case "PROMPT_SYNC_INTERVAL_HOURS":
		return strconv.Itoa(cfg.PromptSyncIntervalHours)
	case "STORAGE_DIR":
		return cfg.StorageDir
	case "SILICONFLOW_API_KEY":
		return cfg.SiliconFlowAPIKey
	case "DASHSCOPE_API_KEY":
		return cfg.DashScopeAPIKey
	case "GROK_SSO_COOKIES":
		return cfg.GrokSSOCookies
	case "GROK_CREDENTIAL_LLM_ENABLED":
		return strconv.FormatBool(cfg.GrokCredentialLLMEnabled)
	case "GROK_CREDENTIAL_LLM_MODEL":
		return cfg.GrokCredentialLLMModel
	case "LOCAL_TTS_BASE_URL":
		return cfg.LocalTTSBaseURL
	case "LOCAL_TTS_API_KEY":
		return cfg.LocalTTSAPIKey
	case "LOCAL_TTS_MODEL":
		return cfg.LocalTTSModel
	case "RENDER_SERVICE_URL":
		return cfg.RenderServiceURL
	case "QINIU_ACCESS_KEY":
		return cfg.QiniuAccessKey
	case "QINIU_SECRET_KEY":
		return cfg.QiniuSecretKey
	case "QINIU_BUCKET":
		return cfg.QiniuBucket
	case "QINIU_DOMAIN":
		return cfg.QiniuDomain
	case "QINIU_REGION":
		return cfg.QiniuRegion
	case "CLASH_SUB_URL":
		return cfg.ClashSubURL
	case "CLASH_SUB_PROFILE":
		return cfg.ClashSubProfile
	case "CLASH_MULTIPORT_HOST":
		return cfg.ClashMultiportHost
	case "CLASH_MULTIPORT_START":
		return strconv.Itoa(cfg.ClashMultiportStart)
	case "CLASH_MULTIPORT_MAX":
		return strconv.Itoa(cfg.ClashMultiportMax)
	case "CLASH_EXCLUDE_REGEX":
		return cfg.ClashExcludeRegex
	case "CLASH_PRIORITY_REGIONS":
		return cfg.ClashPriorityRegions
	case "MIHOMO_CONFIG_PATH":
		return cfg.MihomoConfigPath
	case "MIHOMO_API_URL":
		return cfg.MihomoAPIURL
	case "MIHOMO_API_SECRET":
		return cfg.MihomoAPISecret
	case "CLASH_SYNC_INTERVAL_HOURS":
		return strconv.Itoa(cfg.ClashSyncIntervalHours)
	default:
		return os.Getenv(key)
	}
}

// ApplyEffectiveMap 将合并后的有效业务配置映射原位写入运行时 *Config，并同步部分依赖 os.Getenv 的热池/鉴权变量。
func ApplyEffectiveMap(cfg *Config, effective map[string]string) {
	if cfg == nil || len(effective) == 0 {
		return
	}
	CaptureInitialEnvSnapshot()
	parseBool := func(val string, fallback bool) bool {
		b, err := strconv.ParseBool(strings.TrimSpace(val))
		if err != nil {
			return fallback
		}
		return b
	}
	parseInt := func(val string, fallback int) int {
		n, err := strconv.Atoi(strings.TrimSpace(val))
		if err != nil || n < 0 {
			return fallback
		}
		return n
	}

	for key, val := range effective {
		spec, ok := FindSettingSpec(key)
		if ok && spec.ReadOnlyBootstrap {
			continue
		}
		// 同步写入当前进程环境变量视图，确保 middlewares.Authorization 与 LoadPoolPolicy 等读取实时值
		_ = os.Setenv(key, val)

		switch key {
		case "Authorization":
			cfg.Authorization = val
		case "BASE_URL":
			if strings.TrimSpace(val) != "" {
				cfg.BaseURL = strings.TrimSpace(val)
			}
		case "API_REVERSE_PROXY":
			cfg.APIReverseProxy = strings.TrimSpace(val)
		case "FILES_REVERSE_PROXY":
			cfg.FilesReverseProxy = strings.TrimSpace(val)
		case "STREAM_MODE":
			cfg.StreamMode = parseBool(val, cfg.StreamMode)
		case "MAX_CONTINUE_COUNT":
			cfg.MaxContinueCount = parseInt(val, cfg.MaxContinueCount)
		case "ENABLE_HISTORY":
			cfg.EnableHistory = parseBool(val, cfg.EnableHistory)
		case "ENABLE_EXTERNAL_TOKEN":
			cfg.EnableExternalToken = parseBool(val, cfg.EnableExternalToken)
		case "TOOL_CALLING_ENABLED":
			cfg.ToolCallingEnabled = parseBool(val, cfg.ToolCallingEnabled)
		case "REFUSAL_RETRIES":
			cfg.RefusalRetries = parseInt(val, cfg.RefusalRetries)
		case "FREE_ACCOUNTS":
			cfg.FreeAccounts = parseBool(val, cfg.FreeAccounts)
		case "FREE_ACCOUNTS_NUM":
			cfg.FreeAccountsNum = parseInt(val, cfg.FreeAccountsNum)
		case "PROXY_URL":
			cfg.ProxyURL = strings.TrimSpace(val)
		case "DEBUG_SENTINEL":
			cfg.DebugSentinel = parseBool(val, cfg.DebugSentinel)
		case "AUDIT_LOG_FILE":
			cfg.AuditLogFile = strings.TrimSpace(val)
		case "AUDIT_LOG_MAX_MB":
			cfg.AuditLogMaxMB = parseInt(val, cfg.AuditLogMaxMB)
		case "KAMI_AUTO_RENEW":
			cfg.KamiAutoRenew = parseBool(val, cfg.KamiAutoRenew)
		case "KAMI_HEADLESS":
			cfg.KamiHeadless = parseBool(val, cfg.KamiHeadless)
		case "PROMPT_SYNC_INTERVAL_HOURS":
			cfg.PromptSyncIntervalHours = parseInt(val, cfg.PromptSyncIntervalHours)
		case "STORAGE_DIR":
			if strings.TrimSpace(val) != "" {
				cfg.StorageDir = strings.TrimSpace(val)
			}
		case "SILICONFLOW_API_KEY":
			cfg.SiliconFlowAPIKey = strings.TrimSpace(val)
		case "DASHSCOPE_API_KEY":
			cfg.DashScopeAPIKey = strings.TrimSpace(val)
		case "GROK_SSO_COOKIES":
			cfg.GrokSSOCookies = strings.TrimSpace(val)
		case "GROK_CREDENTIAL_LLM_ENABLED":
			cfg.GrokCredentialLLMEnabled = parseBool(val, cfg.GrokCredentialLLMEnabled)
		case "GROK_CREDENTIAL_LLM_MODEL":
			if strings.TrimSpace(val) != "" {
				cfg.GrokCredentialLLMModel = strings.TrimSpace(val)
			}
		case "LOCAL_TTS_BASE_URL":
			cfg.LocalTTSBaseURL = strings.TrimSpace(val)
		case "LOCAL_TTS_API_KEY":
			cfg.LocalTTSAPIKey = strings.TrimSpace(val)
		case "LOCAL_TTS_MODEL":
			if strings.TrimSpace(val) != "" {
				cfg.LocalTTSModel = strings.TrimSpace(val)
			}
		case "RENDER_SERVICE_URL":
			cfg.RenderServiceURL = strings.TrimSpace(val)
		case "QINIU_ACCESS_KEY":
			cfg.QiniuAccessKey = strings.TrimSpace(val)
		case "QINIU_SECRET_KEY":
			cfg.QiniuSecretKey = strings.TrimSpace(val)
		case "QINIU_BUCKET":
			cfg.QiniuBucket = strings.TrimSpace(val)
		case "QINIU_DOMAIN":
			cfg.QiniuDomain = strings.TrimSpace(val)
		case "QINIU_REGION":
			cfg.QiniuRegion = strings.TrimSpace(val)
		case "CLASH_SUB_URL":
			cfg.ClashSubURL = strings.TrimSpace(val)
		case "CLASH_SUB_PROFILE":
			if strings.TrimSpace(val) != "" {
				cfg.ClashSubProfile = NormalizeProfile(val)
			}
		case "CLASH_MULTIPORT_HOST":
			if strings.TrimSpace(val) != "" {
				cfg.ClashMultiportHost = strings.TrimSpace(val)
			}
		case "CLASH_MULTIPORT_START":
			if n := parseInt(val, 0); n > 0 {
				cfg.ClashMultiportStart = n
			}
		case "CLASH_MULTIPORT_MAX":
			if n := parseInt(val, 0); n > 0 {
				cfg.ClashMultiportMax = n
			}
		case "CLASH_EXCLUDE_REGEX":
			cfg.ClashExcludeRegex = val
		case "CLASH_PRIORITY_REGIONS":
			cfg.ClashPriorityRegions = strings.TrimSpace(val)
		case "MIHOMO_CONFIG_PATH":
			if strings.TrimSpace(val) != "" {
				cfg.MihomoConfigPath = strings.TrimSpace(val)
			}
		case "MIHOMO_API_URL":
			cfg.MihomoAPIURL = strings.TrimSpace(val)
		case "MIHOMO_API_SECRET":
			cfg.MihomoAPISecret = strings.TrimSpace(val)
		case "CLASH_SYNC_INTERVAL_HOURS":
			cfg.ClashSyncIntervalHours = parseInt(val, cfg.ClashSyncIntervalHours)
		}
	}
	_ = time.Second
}

// ResolveSettingsSecret 统一解析用于加解密系统配置敏感字段的派生口令。
func ResolveSettingsSecret(cfg *Config) string {
	if s := strings.TrimSpace(os.Getenv("GATEWAY_KEY_SECRET")); s != "" {
		return s
	}
	if cfg != nil && strings.TrimSpace(cfg.AdminToken) != "" {
		return strings.TrimSpace(cfg.AdminToken)
	}
	return ""
}

var (
	initialEnvMu       sync.Mutex
	initialEnvCaptured bool
	initialEnvValues   map[string]string
	initialEnvPresent  map[string]bool
)

// CaptureInitialEnvSnapshot 在数据库配置覆写 os.Setenv 之前，捕获一次原始环境变量（含 .env 加载结果）。
func CaptureInitialEnvSnapshot() {
	initialEnvMu.Lock()
	defer initialEnvMu.Unlock()
	if initialEnvCaptured {
		return
	}
	captureInitialEnvLocked()
}

// RefreshInitialEnvSnapshot 重置并重新捕获当前进程环境变量快照（主要用于单元测试）。
func RefreshInitialEnvSnapshot() {
	initialEnvMu.Lock()
	defer initialEnvMu.Unlock()
	captureInitialEnvLocked()
}

func captureInitialEnvLocked() {
	catalog := Catalog()
	initialEnvValues = make(map[string]string, len(catalog))
	initialEnvPresent = make(map[string]bool, len(catalog))
	for _, spec := range catalog {
		if val, ok := os.LookupEnv(spec.Key); ok && strings.TrimSpace(val) != "" {
			initialEnvValues[spec.Key] = val
			initialEnvPresent[spec.Key] = true
		}
	}
	initialEnvCaptured = true
}

// LookupInitialEnv 返回进程启动（.env 加载后、DB 覆盖前）某配置项的原始环境变量值。
func LookupInitialEnv(key string) (string, bool) {
	CaptureInitialEnvSnapshot()
	initialEnvMu.Lock()
	defer initialEnvMu.Unlock()
	val, ok := initialEnvValues[key]
	return val, ok && initialEnvPresent[key]
}

// BuildEffectiveMap 按优先级计算全部配置项的生效值与来源（profile > global > env > default）。
func BuildEffectiveMap(globalVals, profileVals map[string]string) (map[string]string, map[string]string) {
	CaptureInitialEnvSnapshot()
	catalog := Catalog()
	effective := make(map[string]string, len(catalog))
	sources := make(map[string]string, len(catalog))
	for _, spec := range catalog {
		if spec.ReadOnlyBootstrap {
			val, ok := LookupInitialEnv(spec.Key)
			if !ok {
				val = strings.TrimSpace(os.Getenv(spec.Key))
			}
			if val != "" {
				effective[spec.Key] = val
				sources[spec.Key] = "env"
			} else {
				effective[spec.Key] = spec.DefaultValue
				sources[spec.Key] = "default"
			}
			continue
		}
		if spec.ProfileOverridable {
			if pv, ok := profileVals[spec.Key]; ok {
				effective[spec.Key] = pv
				sources[spec.Key] = "profile"
				continue
			}
		}
		if gv, ok := globalVals[spec.Key]; ok {
			effective[spec.Key] = gv
			sources[spec.Key] = "global"
			continue
		}
		if ev, ok := LookupInitialEnv(spec.Key); ok {
			effective[spec.Key] = ev
			sources[spec.Key] = "env"
			continue
		}
		effective[spec.Key] = spec.DefaultValue
		sources[spec.Key] = "default"
	}
	return effective, sources
}
