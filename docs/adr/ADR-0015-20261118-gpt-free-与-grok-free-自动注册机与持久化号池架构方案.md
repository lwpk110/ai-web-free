# ADR-0015: GPT Free 与 Grok Free 自动注册机与持久化号池架构方案

| 字段 | 内容 |
| :--- | :--- |
| 状态 | Accepted |
| 日期 | 2026-11-18 |
| 涉及模块 | `internal/provider/grok_provider.go`, `internal/provider/grok_login.go`, `internal/bootstrap/sync.go`, `scripts/` |
| 关联规范 | `openspec/specs/gateway/pool-resilience-and-ops.md` |

---

## 1. 背景与问题定义 (Context & Problem Statement)

在 `ai-web-free` 接入 OpenAI (ChatGPT Free) 与 xAI (Grok Free) 免费号池的工程实践中，号池的生命周期维护与凭据补给面临严峻瓶颈：

1. **卡商卡密生态的虚幻与死局**：
   - 市场上采购的 Grok/GPT 免费号卡密，通常仅包含注册时生成的一次性初始 Session（如 Grok 卡密尾部的 JWT `{"session_id":"..."}`），卡商附带的所谓“提取工具”实质仅为文本切割器，缺乏任何联网刷新能力；
   - 依赖卡商容易面临批次大面积失效、格式多变、凭据损坏等被动局面。
2. **Cloudflare Turnstile 网页端风控阻断**：
   - xAI 网页端（`accounts.x.ai/sign-in`）与 OpenAI 登录端均挂载了严格的 Cloudflare Turnstile / Proof of Work 防火墙；
   - 自动化无头浏览器（Headless Chrome / CDP）在数据中心或云服务器环境下 100% 触发人机盾挂起，导致传统的服务端静默自动重登（Auto-Login）彻底失效并引发账号风控。
3. **“旧号反复续期”的维护性价比极低**：
   - 旧号重登涉及账密校验、Turnstile 挑战、2FA/TOTP 以及异地登录邮件确认等多重门槛；
   - 相反，Grok 与 GPT 均对新用户开放注册，只要拥有临时邮箱即可低成本无限造号。“以注册代续期、滚动淘汰用完即弃”已成为逆向开源社区的最佳实践。

---

## 2. 关键架构决策 (Decision Drivers & Choices)

```
┌────────────────────────────────────────────────────────┐
│             外部自动化注册机 (Auto-Registrar)            │
│  (部署于本地家宽/住宅代理环境，支持 CLI / Sidecar 独立运行)   │
│                                                        │
│  ┌──────────────────┐          ┌───────────────────┐   │
│  │ 临时邮箱中继服务   │ ◀──────▶ │  CF Turnstile     │   │
│  │ (CF Email Worker/│          │  打码/屏外浏览器   │   │
│  │  Mail.tm API)    │          │  (CapSolver/Patch)│   │
│  └──────────────────┘          └───────────────────┘   │
│           │                              │             │
│           ▼                              ▼             │
│  ┌─────────────────────────────────────────────────┐   │
│  │ 自动化注册调度器 (Grok / ChatGPT Auto-Register)    │   │
│  └─────────────────────────────────────────────────┘   │
└───────────────────────────┬────────────────────────────┘
                            │ POST /admin/api/v1/accounts/import
                            ▼
┌────────────────────────────────────────────────────────┐
│            ai-web-free 生产网关 (纯静态 / 零 CGO)       │
│                                                        │
│   ┌──────────────────┐           ┌─────────────────┐   │
│   │ 禁用无头静默登录   │           │ 凭据摄入与自动   │   │
│   │ (避免触发 CF 封号)│           │ 覆盖更新        │   │
│   └──────────────────┘           └─────────────────┘   │
│           │                               │            │
│           ▼                               ▼            │
│   ┌────────────────────────────────────────────────┐   │
│   │   动态健康号池调度 (Round-Robin + Failover)      │   │
│   │   遇到 401/403 自动隔离 -> 触发注册机低水位补号    │   │
│   └────────────────────────────────────────────────┘   │
└────────────────────────────────────────────────────────┘
```

### 决策 1：服务端彻底禁用无头登录，仅保留交互式有头续期（Headed Mode Only）
- **决策内容**：
  1. 在 `internal/provider/grok_login.go` 的 `loginGrokCard` 入口明确校验 `options.Headless`，若为无头模式直接拒绝并报错熔断，避免在云端静默重试导致封号；
  2. 针对单个卡密保留“有头桌面模式（`headless=false`）”续期能力，允许运维人员在本地带 GUI 环境下通过真实浏览器人工完成 Turnstile 挑战后捕获会话。
- **理由**：
  保障网关在生产 Docker / Linux 环境下的绝对安全性与零误伤，彻底杜绝无头对抗 Cloudflare 导致全池瘫痪。

### 决策 2：网关核心与注册机服务“物理解耦”
- **决策内容**：
  1. `ai-web-free` 主程序坚持纯 Go、静态编译（`CGO_ENABLED=0`）、零外部 Python/Node 依赖，保持轻量高并发网关定位；
  2. 注册机独立为外置组件（提供 `scripts/` 下的独立脚本或单独容器 Sidecar），运行于家宽网络或住宅代理下，通过标准 HTTP 管理接口 `POST /admin/api/v1/accounts/import` 批量推入凭据。
- **理由**：
  注册机需要与打码平台、临时邮件轮询、浏览器反指纹插件重度交互，与生产网关解耦可确保网关的高可用与极简部署。

### 决策 3：临时邮箱基础设施选型（Mail Provider Abstraction）
- **决策内容**：
  注册机统一抽象 `MailProvider` 接口：
  ```go
  type MailProvider interface {
      CreateMailbox(ctx context.Context) (email string, cleanup func(), err error)
      FetchVerificationCode(ctx context.Context, email string, timeout time.Duration) (code string, err error)
  }
  ```
  优先实现两大通道：
  1. **Cloudflare Email Routing + Worker**：利用用户自有免费域名，收信后自动存储到 KV 或 Webhook 回调，稳定且无发信频控；
  2. **公开临时邮箱 API**（如 Mail.tm / DuckMail / CloudMail）：零配置开箱即用。

### 决策 4：Cloudflare Turnstile 穿透策略分级
- **决策内容**：
  注册机在处理人机验证时按场景分级：
  1. **全自动生产模式（纯协议 + 打码服务）**：
     使用 `bogdanfinn/tls-client` 发起 HTTP 请求，捕获页面上的 Turnstile `sitekey` 与 `pageurl`，上报给第三方打码平台（CapSolver / YesCaptcha / 2Captcha），换取 `cf-turnstile-response` token 后直接发包完成注册。单号综合打码成本 < $0.001（不到 1 分钱人民币），全程仅需 3~5 秒；
  2. **本地调试模式（真实屏外浏览器 + TurnstilePatch）**：
     使用本地 Chrome 加载反指纹扩展并将窗口放置在屏幕可视区域外（`-10000, -10000`），依靠本地家宽 IP 与真实浏览器环境自动过盾。

### 决策 5：凭据升级、会话维活与「用户名+密码+RT模式」双保险
- **决策内容**：
  1. **主流「用户名+密码+RT」模式原生适配**：
     针对卡商与社区主流的 `邮箱----密码----RefreshToken` 凭据，解析引擎（`internal/accounts/kami.go`）全面升级为泛化识别（支持标准 `rt_` 前缀及任意长字符串 OAuth Refresh Token）。
     - **日常纯协议免盾**：网关优先使用 Refresh Token 直接调用 OAuth Token 接口（`chatgpt.GETTokenForRefreshToken`）轮转 Access Token，纯 HTTP 通信，彻底绕开 Cloudflare Turnstile 网页盾；
     - **账密作为灾备兜底**：密码仅用于当 Refresh Token 彻底被 revoke 撤销时，在本地有头模式或注册机通道作为重新签发凭据的最后保险。
  2. **Grok 凭据双轨制**：注册机成功注册后，不仅提取 Web 的 `sso` / `sso-rw` Cookie 写入号池，更利用 xAI 官方设备授权流（`/oauth2/device/code`、`/verify`、`/approve`）进一步将 Session 转换为底层 OIDC / OAuth Token；
  3. **ChatGPT 凭据标准化**：注册成功后直接捕获并持久化 Refresh Token 与 Access Token，由号池已有的后台轮转协程（每 10 天自动无感刷新）实现免维护生命周期。

### 决策 6：号池低水位自动补给机制（Watermark-triggered Replenishment）
- **决策内容**：
  1. 在 `GrokProvider` 与 `ChatGPTProvider` 中维护活跃水位线 `MinActiveWatermark`；
  2. 当由于 401/403/封号导致健康活跃账号数低于水位（例如活跃数 < 5）时，网关通过 Webhook 或管理告警触发注册机批量生成新号补充；
  3. 过期账号自动标记为 `StatusExpired`，定期由后台任务安全淘汰，不再保留无意义的历史凭据包袱。

---

## 3. 落地路线与任务拆解 (Implementation Roadmap)

- [x] **阶段一：网关号池兼容与安全防护加固（已完成）**
  - [x] 在 `internal/provider/grok_login.go` 中硬性禁用无头登录，输出指引阻断；
  - [x] 优化 `ParseGrokCardFile` 兼容说明行与 Base64 特殊字符；
  - [x] 修复 `internal/bootstrap/sync.go` 与 `grok_provider.go`：正确将 JWT 会话组装为 `sso=...; sso-rw=...` 并激活入池；
  - [x] 运行时遇到 401/403 自动将账号标记为 `StatusExpired` 并触发无损 Failover。
- [ ] **阶段二：Grok & ChatGPT 自动化注册原型脚本（`scripts/registrar/`）**
  - [ ] 编写 `scripts/registrar/mail_cf.go` / `mail_temp.go` 临时邮件收信适配层；
  - [ ] 编写基于打码平台 API 的纯协议注册客户端；
  - [ ] 注册成功后自动调用本地 `/admin/api/v1/accounts/import` 接口热推入网关。
- [ ] **阶段三：低水位事件告警与自动补号联动**
  - [ ] 控制台支持展示注册机状态与一键“补给号池”按钮；
  - [ ] 支持配置 Webhook 自动触发注册机补号。

---

## 4. 结果与影响 (Consequences)

### 积极收益
- **彻底消除对第三方卡商的依赖**：摆脱格式多变、批量跑路、假冒提取工具的劣质生态；
- **全生命周期自动化**：“注册 -> 提权 -> 入池 -> 故障转移 -> 过期淘汰 -> 自动补号”全链路闭环；
- **生产网关安全合规**：生产核心保持纯 Go 零 CGO 极简部署，避免在服务端硬撼 Cloudflare 风控造成反噬。

### 限制与权衡
- 纯协议打码模式需要用户自行配置第三方打码平台的 API Key（极小额度充值）；
- 本地屏外浏览器模式需要运行在支持桌面 GUI 与家宽 IP 的宿主机上。
