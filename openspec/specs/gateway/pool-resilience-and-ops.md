# 赛道 1 · 生产级号池运维与高可用闭环规范

## 1. 设计目标与约束

- **零 CGO / 纯静态二进制约束**：全部存储、审计、定时调度、浏览器重登均在纯 Go + `chromedp` 体系下实现，不引入 CGO 或外部数据库服务，确保 `CGO_ENABLED=0`（`Dockerfile` 多阶段 `distroless/static-debian12:nonroot`）无缝构建。
- **粘性防风控优先**：单账号维持独立 TLS 指纹（`bogdanfinn/tls-client`）与固定代理绑定，仅当代理节点不可达或号池异常时触发容灾切换。

---

## 2. 核心子系统设计

### 2.1 分级冷却与全链路 Failover 状态机
- **账号冷却字段**（`internal/accounts/account.go`）：
  - `CooldownUntil`：全能力 429 冷却截至时间；当状态为 `StatusRateLimited` 且到期后，后台巡检自动恢复为 `StatusActive`。
  - `ImageCooldownUntil`：仅针对生图/编辑/变体配额触发限流时的局部冷却期（不阻塞该账号继续服务纯文本对话）。
- **请求级自动切换（`internal/handler/`）**：
  - 在 `/v1/chat/completions`、`/v1/images/generations`、`/v1/images/edits`、`/v1/images/variations`、`/v1/audio/*` **尚未向下游写出响应首包前**：
    1. 遇到上游 `401 Unauthorized`：标记账号 `StatusExpired` 并入卡密自动续期队列（若具备卡密凭据），切换下一可用账号重试；
    2. 遇到上游 `429` 或生图频次受限：调用 `ReportRateLimit` 进入全能力或生图局部冷却，切换下一可用账号重试；
    3. 遇到代理传输错误（非业务 HTTP 错误）：上报代理故障并在断路器跳闸时重绑健康代理后重试。
  - 最多尝试 **3 个账号**（可避开临时外部 Token），完整 Failover 轨迹写入审计日志。

### 2.2 卡密账号后台串行自动续期 Worker
- **调度逻辑**：
  - 定期健康检查与 `401` 失效反馈共同触发：识别包含 `Email + Password + TOTPSecret` 且无 `RefreshToken`/`SessionToken` 的卡密账号，当 `StatusExpired` 或距离 `ExpiresAt < 24h`（主动提前续期）且冷却时间到期时入队。
  - 单消费者串行执行（同时最多启动 **1 个** 无头 `chromedp` 实例），避免多浏览器并发触发内存峰值与集中风控。
  - 连续失败采用指数退避（`5m → 30m → 2h`）；成功提取 Token 后同步更新 JWT 元数据、`StatusActive` 并原子回写 `access_tokens.txt`。
- **可观测接口**：`KamiRenewWorker.Stats()` 聚合进 `GET /admin/api/v1/overview` 的 `kami_worker` 字段。

### 2.3 代理池节点健康矩阵与自动漂移
- **状态追踪**（`internal/proxy/pool.go`）：
  - 记录每个节点 `LatencyMs`、`Status`（`active` / `cooling`）、`ConsecutiveErrors`、`TotalFailures`、`LastChecked`、`LastError`、`CooldownUntil`。
  - 连续传输失败达到 `3` 次时将节点置为 `cooling`（默认 `5m`），并自动将绑定该节点的账号迁移到健康代理后重建 `Account.Client`。
- **管理端 API**：
  - `GET /admin/api/v1/proxies`：返回节点健康快照（含每节点当前绑定账号数）。
  - `POST /admin/api/v1/proxies/probe`：并发探测代理列表连通性与延迟并返回结果。
  - `POST /admin/api/v1/proxies`：更新代理池（支持持久化 `proxies.txt`）并自动校准失效绑定账号。

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
