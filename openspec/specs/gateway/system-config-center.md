# 系统配置中心模块（DB-Backed System Config Center）架构规范

> **状态**：Accepted（对应 `/grill-me` 架构决策 `ADR-0019`）
> **最后更新**：2026-11-22

---

## 1. 背景与解决的核心痛点

1. **GitHub Secrets 不可回显与全量盲改痛点**：
   在 CI/CD 自动部署 VPS 场景下，若将 40+ 个业务配置项（如 `CLASH_SUB_URL`、`SILICONFLOW_API_KEY`、`DASHSCOPE_API_KEY`、`POOL_*` 热池阈值、`QINIU_*` 等）全部放入 GitHub Secret `VPS_ENV_CONTENT`，由于 GitHub Secrets 单向不可查看明文，每次微调单个参数都需要从本地全量重新粘贴覆盖并重新走一遍 CI/CD 发布重启。
2. **多运行时环境（`home` / `office` / `vps`）配置共享与差异化冲突**：
   多台设备通过云端数据库（Neon PostgreSQL / 本地 SQLite 物化同步）协同时，大部分业务配置（如云端多模态 API Key、热池策略、七牛对象存储）希望一处修改全局生效（`global`），而少部分环境相关配置（如 `PROXY_URL`、`CLASH_SUB_URL`、`MIHOMO_CONFIG_PATH`、`KAMI_HEADLESS`）又需要支持按 `profile` 独立覆盖。
3. **运行期热生效诉求与人机工程体验**：
   修改第三方 API Key、热池目标容量、Clash 订阅参数或流式/工具调用开关时，应当在管理台保存后立即热刷新内存与相关子系统，无需重启 Go 进程；同时提供非反人类的敏感字段「点击揭示明文即编辑」、差异高亮、快速常用面板与 `.env` 批量导入/导出。

---

## 2. 核心架构设计

### 2.1 分层职责：Bootstrap 极简引导层 vs 数据库业务配置层

将现有的配置项明确划分为两层：

#### A. Bootstrap 极简引导层（固定在 `.env`，管理台 `#/settings` 只读展示）
仅负责进程启动、端口监听、数据库连接与最高管理鉴权。在 `#/settings` 中归入「引导层只读 (.env)」分组并附带「一键复制极简 `VPS_ENV_CONTENT`」生成器：
- `SERVER_HOST`、`SERVER_PORT`、`TLS_CERT`、`TLS_KEY`
- `APP_PROFILE`（当前实例默认 Runtime Profile，如 `home` / `office` / `vps`）
- `DATABASE_URL`、`DB_MATERIALIZE_ENABLED`、`DB_LOCAL_PATH`、`DB_SYNC_*`
- `ADMIN_TOKEN`、`GATEWAY_KEY_SECRET`
- `CF_TUNNEL_TOKEN`、`CF_TUNNEL_PROTOCOL`（供宿主机 `setup_vps.sh` 启动 `cloudflared.service` 使用）

> **效果**：生产 VPS 上的 `.env`（及 GitHub Secret `VPS_ENV_CONTENT`）从此只需配置极简引导变量，一次配好基本不再改动！

#### B. 数据库动态业务配置层（`system_setting_records` 表，纳入物化同步）
其余所有业务配置项统一由系统配置中心托管，分为 5 大业务分类（Category）：
1. **网关与对话 (`gateway`)**：`Authorization`、`BASE_URL`、`API_REVERSE_PROXY`、`FILES_REVERSE_PROXY`、`STREAM_MODE`、`MAX_CONTINUE_COUNT`、`ENABLE_HISTORY`、`ENABLE_EXTERNAL_TOKEN`、`TOOL_CALLING_ENABLED`、`REFUSAL_RETRIES`、`DEBUG_SENTINEL`
2. **多模态云端 Key (`multimodal`)**：`SILICONFLOW_API_KEY`、`DASHSCOPE_API_KEY`、`LOCAL_TTS_BASE_URL`、`LOCAL_TTS_API_KEY`、`LOCAL_TTS_MODEL`、`RENDER_SERVICE_URL`、`GROK_BASE_URL`、`GROK_ACCOUNTS_FILE`、`GROK_SSO_COOKIES`、`GROK_CREDENTIAL_LLM_ENABLED`、`GROK_CREDENTIAL_LLM_MODEL`
3. **号池与热池策略 (`pool`)**：`FREE_ACCOUNTS`、`FREE_ACCOUNTS_NUM`、`KAMI_AUTO_RENEW`、`KAMI_HEADLESS`、`PROMPT_SYNC_INTERVAL_HOURS`、`PROMPT_PLAZA_FILE`、`POOL_CHATGPT_FREE_ACTIVE_TARGET`、`POOL_CHATGPT_FREE_MIN_HEALTHY`、`POOL_CHATGPT_FREE_MAX_ACTIVE`、`POOL_CHATGPT_FREE_ROTATE_INTERVAL`、`POOL_CHATGPT_FREE_ROTATE_RATIO`、`POOL_CHATGPT_FREE_ACTIVATE_BATCH`、`POOL_GROK_FREE_ACTIVE_TARGET`、`POOL_GROK_FREE_MIN_HEALTHY`、`POOL_GROK_FREE_MAX_ACTIVE`、`POOL_GROK_FREE_ROTATE_INTERVAL`、`POOL_GROK_FREE_ACTIVATE_BATCH`
4. **Clash 订阅与代理 (`proxy`)**：`PROXY_URL`、`CLASH_SUB_URL`、`CLASH_SUB_PROFILE`、`CLASH_MULTIPORT_HOST`、`CLASH_MULTIPORT_START`、`CLASH_MULTIPORT_MAX`、`CLASH_EXCLUDE_REGEX`、`CLASH_PRIORITY_REGIONS`、`MIHOMO_CONFIG_PATH`、`MIHOMO_API_URL`、`MIHOMO_API_SECRET`、`CLASH_SYNC_INTERVAL_HOURS`
5. **存储与审计 (`storage`)**：`STORAGE_DIR`、`QINIU_ACCESS_KEY`、`QINIU_SECRET_KEY`、`QINIU_BUCKET`、`QINIU_DOMAIN`、`QINIU_REGION`、`QINIU_UPLOAD_URL`、`S3_ENDPOINT`、`S3_BUCKET`、`S3_ACCESS_KEY`、`S3_SECRET_KEY`、`S3_REGION`、`S3_PUBLIC_URL`、`AUDIT_LOG_FILE`、`AUDIT_LOG_MAX_MB`

---

### 2.2 覆盖优先级与 `.env` 自动种子迁移（Auto-Seed）

1. **四级读取优先级（从高到低）**：
   $$\text{DB Profile Override } (\texttt{scope=active\_profile}) \succ \text{DB Global } (\texttt{scope=global}) \succ \text{Local } \texttt{.env} \text{ EnvVar} \succ \text{Code Default}$$
2. **首次启动自动种子入库（Non-Destructive Auto-Seed）**：
   - 进程启动时扫描当前 `.env` / 环境变量中的业务配置项；
   - 若某配置项在环境变量中有显式非空值，且数据库中对应的目标 `scope`（环境强相关项如 `PROXY_URL`、`CLASH_SUB_URL` 等可种入对应 Profile 或 `global`，其余通用项种入 `global`）**尚无记录**，则自动将其写入 `system_setting_records` 表（`updated_by = "env_seed"`）。
   - 一旦数据库中已存在该配置项（尤其是管理员在 `#/settings` 修改保存后），**始终以数据库为准**，重启时绝不会被旧 `.env` 文件反向覆盖。

---

### 2.3 数据库模型与 AES-GCM 敏感字段加密

在 `internal/db/models.go` 新增 `SystemSettingRecord` 并加入 `CoreModels()` 与 `DefaultSyncTables()`（支持 SQLite ↔ Neon PG 异步双向同步）：

```go
// SystemSettingRecord 系统配置中心持久化模型（支持 global 与 profile 两级作用域及 AES-GCM 敏感加密）
type SystemSettingRecord struct {
	ID          string    `gorm:"primaryKey;size:128" json:"id"`               // "<scope>:<key>"
	Scope       string    `gorm:"size:32;default:'global';index" json:"scope"` // "global" | "home" | "office" | "vps" | ...
	Key         string    `gorm:"size:96;index" json:"key"`
	Category    string    `gorm:"size:32;index" json:"category"`               // gateway | multimodal | pool | proxy | storage
	Value       string    `gorm:"type:text" json:"value,omitempty"`
	ValueCipher string    `gorm:"type:text" json:"value_cipher,omitempty"`
	IsSecret    bool      `gorm:"default:false" json:"is_secret"`
	UpdatedBy   string    `gorm:"size:32;default:'admin'" json:"updated_by"`
	CreatedAt   time.Time `gorm:"index" json:"created_at"`
	UpdatedAt   time.Time `gorm:"index" json:"updated_at"`
}

func (SystemSettingRecord) TableName() string {
	return "system_setting_records"
}
```

- **加密派生机制**：
  - 复用 `GATEWAY_KEY_SECRET`（若未设置则回退 `ADMIN_TOKEN`，均为空时使用内置应用盐派生兜底密钥）通过 `SHA-256("ai-web-free/system-settings/" + secret)` 构造 256-bit `AES-GCM` AEAD，密文格式为 `enc:v1:<base64(nonce+ciphertext)>`。
  - **列表查询默认脱敏**：`GET /admin/api/v1/settings` 对 `is_secret=true` 的字段仅返回 `masked_value`（例如 `sk-abcd...7890`）与 `has_secret_value: true`，绝不在常规列表响应中暴露明文。
  - **点击明文即编辑**：管理员在 `#/settings` 点击某敏感项的「查看/编辑明文」按钮时，调用 `GET /admin/api/v1/settings/reveal?scope=...&key=...` 解密返回真实明文并直接填充进可编辑文本框，支持微调保存与一键复制；未点开也未修改的敏感项不会在保存时被掩码字符串覆盖。

---

### 2.4 全链路动态热刷新（Hot-Reload Hooks）

当管理员通过 `PUT /admin/api/v1/settings` 保存配置、删除覆盖，或后台 `SyncEngine` 从云库拉取到 `system_setting_records` 变更时，按以下顺序执行热刷新：

1. **原位更新运行时 `*config.Config` 与进程环境变量**：
   - 将解析后的最新有效配置（`Profile 覆盖 > Global 全局 > .env > 默认值`）原位写回 `rt.Config` 各字段，并同步更新进程内环境变量视图（确保 `middlewares.Authorization` 与 `config.LoadPoolPolicy` 等依赖 `os.Getenv` 的模块立即生效）。
2. **多模态供应商热重注**：
   - 调用 `provider.ApplyConfig(reg, rt.Config)`，使 `SiliconFlowProvider`、`DashScopeProvider`、`GrokProvider`、`LocalTTSProvider` 的 API Key、BaseURL 与 Cookie 立即生效。
3. **号池热池策略（PoolPolicy）即时调谐**：
   - 重新调用 `applyPoolPolicy` 更新 `chatgpt` Free/PUID 策略并触发 `accountPool.Rebalance()`，无需重启即可动态扩缩热池活跃账号数。
4. **出站代理联动**：
   - 若新配置的 `PROXY_URL` 非空且当前 Profile 的 `proxyPool` 尚未包含该节点，自动追加到当前 Profile 代理池并同步至内存池。

---

### 2.5 CI/CD `deploy.yml` 智能合并保留模式

升级 `.github/workflows/deploy.yml` 对 `/opt/ai-web-free/.env` 的处理：
- 若服务器上已存在 `/opt/ai-web-free/.env`，将 `VPS_ENV_CONTENT` 中的键值与现有 `.env` **按键智能合并**（`VPS_ENV_CONTENT` 中声明的键更新或覆盖同名旧键，现有 `.env` 中其余未提及的键自动保留），权限保持 `0600`。
- 配合系统配置中心，`VPS_ENV_CONTENT` 只需保留极简引导项（`APP_PROFILE=vps`、`DATABASE_URL=...`、`ADMIN_TOKEN=...`、`CF_TUNNEL_TOKEN=...`），日常业务配置全部在管理台 `#/settings` 可视化维护。

---

## 3. 管理端 API 与前端 `#/settings` 人性化交互规范

### 3.1 API 契约（`/admin/api/v1/settings*`）

| 方法与路径 | 说明 |
| :--- | :--- |
| `GET /admin/api/v1/settings?scope=global\|home\|office\|vps` | 返回全部分类配置项元数据、当前有效值（敏感项脱敏）、全局值、指定 Scope 覆盖值、生效来源（`profile` / `global` / `env` / `default`）及只读引导层快照 |
| `PUT /admin/api/v1/settings` | 批量保存指定 `scope`（`global` 或具体 `profile`）下的配置项，触发全链路热刷新并返回生效摘要 |
| `DELETE /admin/api/v1/settings?scope=<profile>&key=<KEY>` | 清除某个 Profile 下的专属覆盖值（自动回退继承 `global` 全局值或默认值）或清除 `global` 自定义值 |
| `GET /admin/api/v1/settings/reveal?scope=...&key=...` | 解密并返回单个敏感配置项的明文值（受 `AdminAuthorization` 保护） |
| `GET /admin/api/v1/settings/export?scope=...&format=dotenv\|json&include_secrets=true` | 导出指定作用域或当前合并生效的配置为 `.env` 或 JSON 备份 |

### 3.2 前端 `#/settings`（「系统配置中心」）深度人机工程设计

1. **拒绝 50 个输入框平铺轰炸**：
   - 左侧采用分组导航栏（桌面端粘性侧栏、移动端横向胶囊）：
     - `⚡ 常用核心配置`（高频 7 项一屏直达 + 已配置/未配置指示灯）
     - `🎨 多模态云端 Key`
     - `✈️ Clash 订阅与代理`
     - `🛡️ 号池与热池策略`
     - `🌐 网关与对话协议`
     - `💾 对象存储与审计`
     - `🔒 引导层只读 (.env)`（附「一键生成 5 行极简 `VPS_ENV_CONTENT`」卡片）
2. **清晰的 Scope 继承模型**：
   - 顶部提供作用域分段控制器：`🌐 全局共享 (global)` vs 各环境 Profile（`home` / `office` / `vps` 等）。
   - 在 Profile 视图下，默认高亮并聚焦环境差异化项（可一键切换展示全部），清楚标示每一项是 **「继承自全局」** 还是 **「当前环境专属覆盖」**，避免误改全局配置。
3. **不反人类的敏感字段编辑 & 脏数据提交保护**：
   - 敏感项未展开时只展示脱敏胶囊与「👁 查看/编辑明文」「复制明文」按钮；点击「查看/编辑明文」自动解密并填入真实明文到输入框，支持在原值上直接修改。
   - 保存仅提交发生实际修改的字段（Dirty-Only Submit），绝不误把掩码当明文写回。
4. **防呆快捷键与 `.env` 批量导入**：
   - 页面底部采用 **Sticky 浮动未保存变更栏**，显示待保存项计数、支持 `Ctrl+S` / `Cmd+S` 保存热生效及一键放弃更改。
   - 内置 **「粘贴导入 `.env`」** 弹窗：自动解析 `KEY=VALUE`，跳过只读引导项与注释行，预览可导入项后一键入库。
