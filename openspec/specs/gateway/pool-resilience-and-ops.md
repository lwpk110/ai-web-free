# 赛道 1 · 生产级号池运维与高可用闭环规范

## 1. 设计目标与约束

- **零 CGO / 纯静态二进制约束**：全部存储、审计、定时调度、浏览器重登均在纯 Go + `chromedp` 体系下实现，不引入 CGO 或外部数据库服务，确保 `CGO_ENABLED=0`（`Dockerfile` 多阶段 `distroless/static-debian12:nonroot`）无缝构建。
- **粘性防风控优先**：单账号维持独立 TLS 指纹（`bogdanfinn/tls-client`）与固定代理绑定，仅当代理节点不可达或号池异常时触发容灾切换。
- **禁止代理故障时静默直连**：只有存在健康替代节点时才迁移绑定；全部节点不可用时保留原绑定并让上游请求失败，等待管理员恢复或更换代理。

---

## 2. 核心子系统设计

### 2.1 分级冷却与全链路 Failover 状态机
- **账号冷却字段**（`internal/accounts/account.go`）：
  - `CooldownUntil`：全能力 429 冷却截至时间；当状态为 `StatusRateLimited` 且到期后，后台巡检自动恢复为 `StatusActive`。
  - `ImageCooldownUntil`：仅针对生图/编辑/变体配额触发限流时的局部冷却期（不阻塞该账号继续服务纯文本对话）。
- **请求级自动切换（`internal/handler/`）与 5 类根因分类**：
  - 在 `/v1/chat/completions`、`/v1/images/generations`、`/v1/images/edits`、`/v1/images/variations`、`/v1/audio/*` **尚未向下游写出响应首包前**，及管理端测活/卡密自动续期失败处理中，按 `chatgpt.ClassifyAccountErrorReason` 执行精确状态迁移：
    1. **永久封禁/停用**（命中 `account_deactivated` / `account_suspended` / `permanently banned`，优先于 HTTP 401/403 判定）：调用 `MarkBanned` 将账号置为 `StatusBanned`、记录 `/internal/account-banned` 审计事件并**禁止入队** `KamiRenewWorker` 避免无效重登；
    2. **上游 `403` / Cloudflare / Sentinel / Turnstile WAF 拦截**：调用 `RecordProxyBlock`（仅施加短冷却，**保留令牌有效状态不置 `StatusExpired`**）并上报 `ProxyPool.ReportFailure` 计入连续失败计数，熔断后自动迁移绑定代理并重试；
    3. **上游 `401 Unauthorized`（非封禁）**：标记账号 `StatusExpired` 并入卡密自动续期队列（若具备卡密凭据），切换下一可用账号重试；
    4. **上游 `429` 或生图频次受限**：调用 `ReportRateLimit` 进入全能力或生图局部冷却，切换下一可用账号重试；
    5. **代理传输错误（非业务 HTTP 错误）**：上报代理故障并在断路器跳闸时重绑健康代理后重试。
  - 最多尝试 **3 个账号**（可避开临时外部 Token），完整 Failover 轨迹写入审计日志。

### 2.2 卡密账号后台串行自动续期 Worker
- **调度逻辑**：
  - 定期健康检查与 `401` 失效反馈共同触发：识别包含 `Email + Password + TOTPSecret` 且无 `RefreshToken`/`SessionToken` 的卡密账号，当 `StatusExpired` 或距离 `ExpiresAt < 24h`（主动提前续期）且冷却时间到期时入队。
  - 单消费者串行执行（同时最多启动 **1 个** 无头 `chromedp` 实例），避免多浏览器并发触发内存峰值与集中风控。
  - 连续失败采用指数退避（`5m → 30m → 2h`）；成功提取 Token 后同步更新 JWT 元数据、`StatusActive` 并原子回写 `access_tokens.txt`。
- **可观测接口与账号级运行时状态回现**：
  - `KamiRenewWorker.Stats()` 聚合进 `GET /admin/api/v1/overview` 与 `GET /admin/api/v1/providers/:provider/accounts` 的 `kami_worker` 字段（包含 `running_account_id`、`running_phase`、`running_phase_label`、`running_started_at`、`queued_account_ids`）。
  - `Account` / `AccountSnapshot` 维护纯内存线程安全运行时动作状态（`active_action`、`action_phase`、`action_phase_label`、`action_queue_pos`、`action_task_id`、`action_started_at`）及最近一次完成摘要（`last_action`、`last_action_ok`、`last_action_summary`、`last_action_at`），页面刷新（`F5`）与路由切换时自动恢复。

### 2.3 代理池节点健康矩阵与自动漂移
- **状态追踪**（`internal/proxy/pool.go`）：
  - 记录每个节点 `LatencyMs`、`Status`（`active` / `cooling`）、`ConsecutiveErrors`、`TotalFailures`、`LastChecked`、`LastError`、`CooldownUntil`。
  - 连续传输失败达到 `3` 次时将节点置为 `cooling`（默认 `5m`），并自动将绑定该节点的账号迁移到健康代理后重建 `Account.Client`。
- **管理端 API**：
  - `GET /admin/api/v1/proxies`：返回节点健康快照（含每节点当前绑定账号数）。
  - `POST /admin/api/v1/proxies/probe`：并发探测代理列表连通性与延迟并返回结果。
  - `POST /admin/api/v1/proxies`：更新代理池（支持持久化 `proxies.txt`）并自动校准失效绑定账号。
  - `POST /admin/api/v1/providers/:provider/accounts/bulk-proxy`：将选中账号统一绑定到健康代理并原子重建各自的 TLS Client；代理池已有节点时禁止把批量激活目标设为直连。绑定在当前运行期生效，重启后按代理池重新初始化。
  - `POST /admin/api/v1/providers/:provider/accounts/kami-login` 与单账号 `POST /admin/api/v1/providers/:provider/accounts/:id/action`（`action: "login"`, 可选 `headless`）：均以单消费者串行队列异步运行；若已有重登任务正在运行，新触发的单账号或批量账号会自动去重并追加至当前任务等待队列（更新 `queued_account_ids` 与 `action_queue_pos`）。
  - **细粒度执行阶段实时回显**：`chromedp` 登录流程实时上报阶段（`queued` → `checking_proxy` → `launch_browser` → `open_login` → `enter_email` → `enter_password` → `enter_totp` / `waiting_challenge` → `wait_session_token`，多账号间隔为可中断 `cooldown_wait`），通过 `GET /admin/api/v1/overview`、`GET /admin/api/v1/providers/:provider/accounts` 内联 `login_task` 与 `GET .../accounts/kami-login/tasks/:taskID` 返回；登录成功时同步清理残留冷却与 `LastRenewError`。
  - `POST /admin/api/v1/providers/:provider/accounts/kami-login/cancel`：支持按 `account_id` 取消单个等待账号或设置 `cancel_all: true` 取消全部剩余排队账号（正在执行中的账号不可中止，其余队列立即释放并记录取消原因）。
  - `POST /admin/api/v1/providers/:provider/pool/probe-all?async=true` 与 `POST /admin/api/v1/providers/:provider/tasks/dismiss`：支持全池测活异步执行与 `probe_task` 进度回显，以及在重登/测活完成后保留可展开战报直到管理员手动关闭（`dismissed=true`）。
  - 启动浏览器前先探测代理；仅经健康代理登录，代理池无健康节点时逐账号失败并保留原绑定，禁止退化为直连。任务串行运行，多账号之间等待可取消 `10s` 间隔；成功 Token 立即回写，失败结果进入审计日志。任务状态保存在内存中，服务重启后任务历史清空。

### 2.4 零 CGO 请求审计环形缓冲与可选 JSONL 落盘
- **组件**：`internal/audit/` 提供线程安全定长 `RingBuffer`（默认保留最近 `1000` 条记录），记录网关请求（时间、Method/Path、Model、命中账号、代理节点、耗时 `ms`、HTTP 状态、`failover_trace`、错误信息）与关键号池运维事件。
- **可选文件持久化**：当配置环境变量 `AUDIT_LOG_FILE`（可选配套 `AUDIT_LOG_MAX_MB`，默认 `10`）时按体积自动轮转写入 JSONL；未配置时不写盘，完全兼容只读 `distroless` 容器。
- **管理端 API**：`GET /admin/api/v1/logs?filter=all|failover|error&path=...&limit=100`

### 2.5 号池导出闭环与可选 `ADMIN_TOKEN` 鉴权
- **导出 API**：`GET /admin/api/v1/providers/:provider/export?format=sub2api_json|access_tokens|kami&include_secrets=false`
  - `sub2api_json` 默认脱敏 `extra.password` 与 `extra.totp_secret`，仅当 `include_secrets=true` 时导出完整明文。
- **管理端鉴权**（`ADMIN_TOKEN`）：
  - 若环境变量 `ADMIN_TOKEN` 非空，`/admin/api/v1/*`（`/admin/api/v1/auth/status` 除外）需携带 `Authorization: Bearer <ADMIN_TOKEN>` 或 `X-Admin-Token` 请求头。
  - 未配置 `ADMIN_TOKEN` 时保持本地零门槛直入体验。

---

### 2.6 全平台根因诊断与一键自愈 API
- **诊断聚合接口**：`GET /admin/api/v1/diagnostics`
  - 实时汇总账号池快照、代理健康矩阵、卡密串行续期队列及最近审计记录，输出健康评分（`0-100`）、总严重度（`healthy` / `degraded` / `critical`）、5 大根因分类（`banned` / `waf_proxy_block` / `token_expired` / `rate_limit` / `proxy_breaker`）、异常账号脱敏清单、熔断代理节点，以及可直接用于前端无大模型离线诊断展示的 `markdown_report`。
- **修复与自愈执行接口**：`POST /admin/api/v1/diagnostics/repair`
  - 支持安全自愈动作：`smart_heal`（恢复冷却 + 停用封禁 + 探测并迁移受损代理 + 刷新/重排队过期账号 + 当 `active == 0` 时应急注入 3 个匿名 UUID）、`rebind_proxies`、`renew_expired`、`probe_all`、`topup_free_uuid`、`disable_banned`。
  - 支持破坏性清理动作：`delete_banned`（从号池移除封禁账号并回写持久化文件），必须显式携带 `confirm: true` 才允许执行（否则返回 `400 Bad Request`）。

### 2.7 热池 / 待命池（按供应商与账号类型的活跃阈值）

- **目标**：免费账号数量远超实际需求时，仅让 N 个账号处于工作状态，其余保持 `standby`（凭据保留，不参与 `Acquire`、不触发续期、不预建 TLS Client）。
- **状态**：新增 `standby`。`Acquire` / `NeedsProactiveRenew` 自动跳过；`disabled` / `banned` 不会被激活。
- **阈值**（`internal/config/pool_policy.go`，优先级 `POOL_<PROVIDER>_<TYPE>_<KEY>` > `POOL_<PROVIDER>_<KEY>` > `POOL_<KEY>`）：
  `ACTIVE_TARGET`（0=关闭，保持历史行为）、`MIN_HEALTHY`（默认 target/2）、`MAX_ACTIVE`、`ROTATE_INTERVAL`、`ROTATE_RATIO`（默认 0.2）、`ACTIVATE_BATCH`（默认 5）。
  例：`POOL_CHATGPT_FREE_ACTIVE_TARGET=15`、`POOL_CHATGPT_FREE_ROTATE_INTERVAL=24h`。
- **调谐 `Pool.Rebalance()`**（启动时一次，之后每分钟）：
  1. 轮换：到期后把最久在热池的 `ROTATE_RATIO` 比例降级为待命（首次仅建立时间基线）。
  2. 收缩：工作中账号（active/pending/限流/可续期过期）超过目标时，优先降级限流者，其次最久者。
  3. 补位：`max(target-working, min_healthy-healthy)`，受 `MAX_ACTIVE`、`ACTIVATE_BATCH` 限制；优先 Token 有效、最久未用的待命账号；Token 已过期且可续期者置为 `expired` 交由健康巡检。
- **粘性约束不变**：账号与代理/指纹绑定不因进出热池改变；待命仅影响是否参与调度。
- **运维**：`POST /admin/api/v1/providers/:provider/pool/rebalance` 立即调谐；账号动作 `standby` / `activate` 手动切换；管理台支持「待命」筛选与按钮。
- **范围**：当前仅 ChatGPT 号池接入（其余供应商自行管理账号）；`PoolPolicy` 与 `Rebalance` 按账号类型工作，后续供应商可复用。

## 网关 API Key 与账号分组（文本模型网关 Phase 1）

- **身份**：每个 Agent 一把 `sk-gw-*` Key，绑定 1 个主分组，可选降级分组（主组无可用账号时回退）。非 `sk-gw-` 令牌沿用旧的全局 `Authorization` / 透传逻辑。
- **分组**：账号多对多。`account_ids` 非空则仅这些账号；否则按 `account_types`（free/puid/noauth，默认 free）筛选。含模型白名单 `model_allowlist` 与别名 `model_aliases`。
- **模型**：`/v1/models` 返回分组内账号缓存模型 ∩ 白名单 ∪ 别名；缓存为空时回退实时抓取并同样过滤。请求模型不在白名单/别名内返回 403 `model_not_allowed`。
- **限额**：每 Key `rpm` 与 `max_concurrent`（0 = 不限），超限 429；统计 total/failed/last_used。无 Token 计费。
- **存储**：GORM 表 `gateway_groups` / `gateway_keys`；Key 存 SHA-256 哈希（鉴权）+ AES-GCM 密文（可复制，密钥取 `GATEWAY_KEY_SECRET` 或 `ADMIN_TOKEN`）。
- **Admin API**：`/admin/api/v1/gateway/groups[/:id]`、`/gateway/keys[/:id]`、`GET /gateway/keys/:id/secret`。
- **范围**：Phase 1 仅 `/v1/chat/completions`、`/v1/responses`、`/v1/models`；图像/音频/视频沿用旧鉴权。
