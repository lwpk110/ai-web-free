# AGENTS.md

本文件为在本仓库（`ai-web-free`）中协作开发的 Coding Agents 提供项目结构、工程约束、构建测试命令与代码规范指引。

## 1. 项目概览

- **定位**：OpenAI 兼容的多供应商免费号池与逆向网关（Go 1.26 后端 + `go:embed` 内嵌 React/TypeScript 管理台）。
- **核心入口**：
  - `main.go` / `api/router.go`：调用 `bootstrap.Init()` 初始化号池、代理池、Provider 插件与 Gin 路由。
  - 协议覆盖：`/v1/chat/completions`（含 `<tool_call>` 工具调用模拟）、`/v1/responses`、`/v1/files`、`/v1/images/{generations,edits,variations}`、`/v1/audio/{speech,transcriptions,translations}`、`/v1/models`、`/auth/*`、`/backend-api/conversation`，以及 `/admin/api/v1/*` 管理端接口。

---

## 2. 必须遵守的工程约束

1. **严格零 CGO 与纯静态编译（`CGO_ENABLED=0`）**
   - 生产镜像基于 `gcr.io/distroless/static-debian12:nonroot`（见 `Dockerfile`），发行版矩阵包含多平台静态构建（见 `.github/workflows/build.yml`）。
   - **禁止引入任何 CGO 依赖**（如 SQLite C 绑定），且运行期不得假设存在 shell 或必须可写的持久化数据库目录；状态与审计采用纯 Go 内存结构或环境变量显式开启的可选文件持久化（如 `AUDIT_LOG_FILE`）。

2. **前端与 `go:embed` 强绑定构建顺序（`web/embed.go`）**
   - Go 通过 `//go:embed all:dist` 和 `//go:embed system-prompt.txt` 将 `web/dist` 与 `web/system-prompt.txt` 编译进二进制。
   - `web/embed_test.go` 校验内嵌的 `dist/index.html`（含 `<div id="root"></div>`）及 `system-prompt.txt`（需包含自定义 OpenUI 组件 `PoolOverviewCard`、`AccountDiagnosticTable`、`ImageStudioCard`）。
   - **任何修改 `web/src/` 或 OpenUI 组件库后，必须先执行 `cd web && npm run build`，再运行 `go test ./...` 或 `go build`**。

3. **双层前端 UI 与 OpenUI 局部作用域（`openspec/specs/ui/design-plan.md`）**
   - 全局控制台遵循 Tailwind CSS + shadcn/ui Zinc 设计语言与方案 A 布局（可折叠分组侧栏 + 顶栏面包屑），默认中性浅色主题并支持暗色切换。
   - Thesys OpenUI（`@openuidev/react-lang`、`@openuidev/react-ui`）**严格收敛在对话操场 `#/chat` 消息流渲染域**；后端仅在请求携带 `X-OpenUI-Lang: true` 时前置注入 `web.OpenUISystemPrompt()`，不得影响标准 OpenAI API 响应。

4. **粘性防风控与无损容灾边界（`openspec/specs/gateway/pool-resilience-and-ops.md`）**
   - 每个账号绑定独立 TLS 指纹配置（`internal/accounts/fingerprint_profiles.go`、`bogdanfinn/tls-client`）与固定代理；常规请求保持粘性绑定，仅在代理节点连续失败触发熔断或管理员更新代理列表时迁移代理并重建客户端。
   - 网关处理器（`internal/handler/`）的请求级 Failover **必须在向下游写出 HTTP Header / SSE 首包之前完成**。

5. **Spec-Driven 变更习惯**
   - 修改架构、UI 骨架、账号状态机、Failover 策略或 `/admin/api/v1/*` 契约前，先阅读 `openspec/specs/` 下对应规范并在实施后保持同步。

---

## 3. 目录结构与模块职责

| 路径 | 职责说明 |
| :--- | :--- |
| `main.go`, `api/router.go` | CLI 与 Serverless 入口，启动 Gin 服务（`endless`）并加载 `.env` / 环境变量 |
| `internal/bootstrap/` | 启动初始化：加载账号/卡密/代理文本文件、健康检查巡检、临时账号 GC、注入路由 |
| `internal/config/` | 环境变量读取与默认值（端口、代理、流式、工具调用、`ADMIN_TOKEN`、`AUDIT_LOG_*` 等） |
| `internal/accounts/` | 账号结构、JWT 解析、TOTP 计算、卡密与 `Sub2API` JSON 导入/导出、能力矩阵、分层冷却与 `WSS` Actor |
| `internal/proxy/` | 代理池分配、延迟/连通性探测、连续失败熔断 |
| `internal/audit/` | 零 CGO 定长请求/运维事件环形缓冲（`RingBuffer`）与可选大小轮转 JSONL 写入 |
| `internal/provider/` | 多供应商插件注册表、ChatGPT Provider 运维动作、纯 Go `chromedp` 卡密登录及串行续期队列 |
| `internal/chatgpt/` | ChatGPT Web 逆向协议（会话 SSE/WSS、Sentinel/Turnstile、生图、文件上传、TTS、STT） |
| `internal/handler/` | OpenAI `/v1/*` 路由、Failover 调度、会话状态缓存、`/auth/*` 与 `/admin/api/v1/*` 管理接口 |
| `internal/httpstream/`, `internal/sseparser/`, `internal/toolcall/` | SSE 流式解析、OpenAI 增量输出、`<tool_call>` 解析与纠偏恢复 |
| `conversion/`, `typings/`, `httpclient/`, `middlewares/`, `util/` | OpenAI ↔ ChatGPT 请求/响应转换、数据结构、TLS HTTP 客户端封装、鉴权/CORS 中间件与工具函数 |
| `web/` | Vite + React 19 + TypeScript 前端、`scripts/export-prompt.mjs` 与 `embed.go` |
| `scripts/` | 独立 Python 卡密辅助脚本（`kami_login.py` 及对应单测 `test_kami_login.py`） |
| `openspec/specs/` | UI 与号池运维系统设计规范 |

---

## 4. 常用构建、测试与运行命令

严格按照 CI（`.github/workflows/ci.yml`）与静态发布流程执行验证：

```bash
# 1. 前端类型检查与构建（生成 web/dist 与 web/system-prompt.txt）
cd web && npm ci && npx tsc --noEmit && npm run build

# 2. 运行全部 Go 单元测试（依赖 web/dist 与 web/system-prompt.txt）
go test ./...

# 3. 编译纯静态二进制（与 Dockerfile / Release 一致）
CGO_ENABLED=0 go build -o ai-web-free .

# 4. 运行 Python 辅助脚本单元测试
python3 -m unittest discover -s scripts -p "test_*.py" -v
```

### 本地调试提示

- **配置模板**：环境变量参考 `env.template`（支持 `.env` 自动加载）。默认监听 `0.0.0.0:18080`；若需使用其他端口，可通过 `SERVER_PORT=19080 ./ai-web-free` 启动。
- **Vite 开发代理**：`cd web && npm run dev` 默认将 `/admin`、`/v1`、`/auth`、`/backend-api` 代理至 `http://localhost:18080`（见 `web/vite.config.ts`，可通过 `VITE_API_TARGET` 覆盖）。
- **卡密浏览器登录**：`internal/provider/kami_login.go` 会自动探测 `CHROME_PATH` 或系统中的 `google-chrome` / `chromium` / Playwright 缓存二进制。

---

## 5. 安全、凭据与编码约定

- **严禁提交真实凭据或构建产物**：`.gitignore` 已忽略 `*.txt`（如 `access_tokens.txt`、`refresh_tokens.txt`、`session_tokens.txt`、`free_tokens.txt`、`kami_accounts.txt`、`proxies.txt`）、`.env`、`ai-web-free` 二进制与临时日志；切勿将真实账号、密码、TOTP 密钥、代理或 Token 写入源码、测试或示例文件。
- **凭据脱敏原则**：列表/快照接口仅暴露掩码预览；`Sub2API` JSON 导出默认不输出明文 `password` / `totp_secret`，仅在显式请求 `include_secrets=true` 时包含。若设置了 `ADMIN_TOKEN`，除 `/admin/api/v1/auth/status` 外的 `/admin/api/v1/*` 均须校验管理令牌。
- **代码风格与测试要求**：
  - 共用逻辑提取为明确助手函数，避免在聊天、图像、音频等 `handler` 中复制粘贴相同重试或转换逻辑。
  - 修改任何 Go 包或前端后，务必运行对应层及全量检查确认通过，再报告结果。
