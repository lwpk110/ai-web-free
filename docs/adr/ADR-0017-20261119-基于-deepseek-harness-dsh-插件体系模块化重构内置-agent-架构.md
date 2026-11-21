# ADR-0017: 基于 DeepSeek Harness (dsh) 插件体系模块化重构内置 Agent 架构

<!-- grill-me-session: dsh-agent-refactor:0 -->
<!-- grill-me-topic-hash: dsh0017plugin -->

| 字段 | 内容 |
| :--- | :--- |
| 状态 | Proposed |
| 日期 | 2026-11-19 |
| 来源会话 | 架构演进规划（衔接 ADR-0001、ADR-0013、ADR-0014、ADR-0016） |
| 触发方式 | 架构规划归档（DeepSeek Harness `dsh` 内置 Agent 模块化重构方案） |

---

## 1. 背景与问题定义 (Context & Problem Statement)

在 ADR-0014 确立「内外 Agent 共用统一 MCP 注册表（`internal/mcp`）」之后，当前项目已经具备了：
1. **统一 Tool / Resource 注册中心**：`internal/mcp/registry.go` 与 `internal/mcp/copilot.go` 已支持按 `Scope`（`ops` / `studio` / `drama` / `all`）与 `Mode`（`readonly` / `readwrite`）导出 OpenAI `tools` 列表并进程内直调 `ExecuteOpenAIToolCall`。
2. **多供应商插件体系**：`internal/provider/plugin.go` 已抽象出统一的 `ProviderPlugin` 与 `Registry`。
3. **长耗时编排底座**：`internal/drama/engine.go` 实现了可中断恢复的 DAG 剧本编排引擎，`internal/handler/admin_bulk_task.go`（`a584bad` 引入）实现了号池异步批量运维任务系统（`batch_tasks`）。

然而，随着平台能力从单纯的逆向网关扩展为覆盖**号池自愈运维、对话操场 Copilot、多模态工坊（生图/TTS/视频/爬虫）与短剧工坊**的 Agent 驱动平台，现有内置 Agent 逻辑仍存在以下结构性痛点：
- **Agent Loop 与 Handler 耦合**：对话工作台（`ChatWorkspace`）、内置运维 Copilot、短剧自动编排各自定义状态流转与工具回环逻辑，缺少统一的 Harness（循环控制、上下文预算裁剪、工具调用纠偏、中止与状态回放）。
- **内部子域尚未彻底插件化**：现有 `internal/provider/` 只插件化了上游模型供应商；号池诊断修复、创意提示词检索、短剧编排、媒体工作流等内部能力虽然已接入 `internal/mcp`，但缺少统一的 **Agent Skill / Hook / Plugin 生命周期契约**。
- **可观测性与安全护栏分散**：工具执行前后的脱敏校验、高危动作二次确认（`dry_run` / `confirm: true`）、Token/轮次预算熔断以及推理步进事件推送（SSE / WebSocket）尚未收敛到统一引擎层。

因此，本 ADR 规划引入 **DeepSeek Harness (`dsh`) 架构思想**，在严格遵守 `CGO_ENABLED=0` 纯静态编译与双层 UI 边界（`openspec/specs/ui/design-plan.md`）的前提下，通过插件化的 Harness 内核统一收编内部 Agent 编排与自优化能力。

---

## 2. 核心设计原则 (Design Drivers & Constraints)

1. **严格零 CGO 与纯 Go 单二进制内嵌（遵循 `AGENTS.md`）**
   - `dsh` 运行时内核作为纯 Go 模块（建议位于 `internal/harness/`）直接编译进 `ai-web-free` 单二进制，不引入 Node/Python 子进程作为核心调度器，不破坏 `CGO_ENABLED=0` 与 `distroless` 部署形态。
2. **与 `internal/mcp` 协同而非另起炉灶（衔接 ADR-0014）**
   - `dsh` 不替代 `internal/mcp/Registry`，而是将 `internal/mcp` 作为其标准 **Tool / Resource Substrate（能力底座）**，并通过 `OpenAITools` 与 `ExecuteOpenAIToolCall` 进程内直调复用同一套 Schema、域过滤、脱敏规则与审计日志。
3. **推理引擎与业务能力插件解耦**
   - 将推理上游（本地逆向网关 `/v1/chat/completions` / `PoolChatExecutor` / 外部兼容模型）抽象为 `LLMDriver`，将各业务域（号池运维、工坊创作、短剧 DAG、提示词广场）抽象为 `dsh` **Capability Plugin**，支持热插拔与按场景裁剪上下文。
4. **OpenUI 渲染边界严格隔离（遵循 `openspec/specs/ui/design-plan.md`）**
   - `dsh` 输出的结构化诊断卡片与交互视图（如 `PoolOverviewCard`、`AccountDiagnosticTable`、`ImageStudioCard`）继续通过 `X-OpenUI-Lang: true` 严格收敛在 `#/chat` 消息流中，不影响标准 `/v1/*` OpenAI 协议兼容性。
5. **高危动作护栏与可中断恢复**
   - 写/删/批量重登等高危插件动作强制走 `PolicyGate`（支持 `readonly` 只读演练、`dry_run` 预览与显式确认）；长耗时动作无缝桥接 `admin_bulk_task` 与 `drama.Engine`，返回异步句柄（`task_id` / `run_id`）而非阻塞会话线程。

---

## 3. `dsh` 分层架构蓝图 (Target Architecture)

```text
+-------------------------------------------------------------------+
|                        Entry & Surfaces                           |
|  #/chat Copilot | /admin/api/v1/agent/* | Scheduled Self-Heal     |
|  External Agents (Claude Code / Cursor) -> POST /admin/api/v1/mcp |
+---------------------------------+---------------------------------+
                                  |
                                  v
+-------------------------------------------------------------------+
|                   DeepSeek Harness Core (internal/harness)        |
|  +--------------+  +---------------+  +------------------------+  |
|  | Session &    |  | Agent Loop    |  | Context & Budget       |  |
|  | State Store  |  | Orchestrator  |  | Compactor (Token/Step) |  |
|  +--------------+  +---------------+  +------------------------+  |
|  +--------------+  +---------------+  +------------------------+  |
|  | Hook Bus     |  | PolicyGate    |  | Event Streamer         |  |
|  | (Pre/Post)   |  | (RBAC/Confirm)|  | (SSE / OpsLog / Audit) |  |
|  +--------------+  +---------------+  +------------------------+  |
+---------------------------------+---------------------------------+
                                  |
         +------------------------+------------------------+
         v                        v                        v
+------------------+   +--------------------+   +-------------------+
| LLMDriver Adapter|   | Capability Plugins |   | Internal MCP Core |
| - PoolChatExec   |   | - OpsHealPlugin    |   | internal/mcp      |
| - ToolCallParser |   | - StudioPlugin     |   | - Scope/Mode      |
|   (XML/Native)   |   | - DramaPlugin      |   | - AuditRecorder   |
+------------------+   | - PromptPlugin     |   +-------------------+
                       +--------------------+
```

### 3.1 核心原语拆分 (`internal/harness/`)

| 模块 | 职责定位 | 复用现有基础设施 |
| :--- | :--- | :--- |
| **`Runner / Orchestrator`** | 驱动标准 Think → Act → Observe 多步循环；支持最大步数、超时与客户端断连 Cancel 传播 | 复用 `internal/toolcall` 的 `<tool_call>` 纠偏与原生 `tool_calls` 兼容解析 |
| **`PluginRegistry`（能力插件）** | 定义统一 `HarnessPlugin` 接口（元数据、系统提示词片段、挂载工具域、前后置 Hook、健康诊断方法） | 复用 `internal/mcp.Registry` 作为工具实际注册与执行载体 |
| **`LLMDriver`** | 屏蔽模型推理差异（ChatGPT / Grok 逆向池或指定路由模型），支持流式增量文本与工具调用提取 | 复用 `NewPoolChatExecutor` 与 `internal/accounts/pool.go` Failover |
| **`PolicyGate & HookBus`** | 提供 `BeforeToolCall` / `AfterToolCall` / `OnStepError` 钩子；统一拦截高危写操作、校验 `confirm`、脱敏返回值 | 复用 `internal/audit.RingBuffer` 与 `internal/opslog.Buffer` |
| **`ContextManager`** | 管理工作区记忆、历史工具输出压缩（长日志/大 JSON 摘要截断）、按场景动态拼装最小 System Prompt | 复用 `internal/vectorstore` 与 `internal/db`（SQLite/PG 双驱动） |
| **`EventSink`** | 标准化输出 `step_start`、`tool_call`、`tool_result`、`approval_required`、`openui_block`、`done` 事件流 | 对接前端 `#/chat` SSE 与管理台批量任务状态轮询 |

### 3.2 内置插件矩阵规划 (Plugin Matrix)

1. **`ops-healer`（号池与网关自愈插件）**
   - **挂载域**：`mcp.ScopeOps` + `admin_bulk_task`
   - **核心能力**：诊断 401/403/Turnstile/代理熔断根因，生成结构化诊断报告（OpenUI `AccountDiagnosticTable` / `PoolOverviewCard`），在策略允许下触发异步批量探活、代理迁移或 `grok_login_task` / `kami_worker` 续期任务。
2. **`studio-creator`（多模态工坊创作插件）**
   - **挂载域**：`mcp.ScopeStudio`
   - **核心能力**：协同 `promptplaza` 热门提示词检索、文生图/图生图（`/v1/images/*`）、Edge/Cloud/Jianying TTS 与声音复刻（`/v1/audio/*`）、Meta Muse / DashScope 视频生成（`/v1/videos/*`）与媒体爬虫，输出画廊资产引用。
3. **`drama-director`（短剧工作流编排插件）**
   - **挂载域**：`mcp.ScopeDrama`
   - **核心能力**：面向 `internal/drama/engine.go`，支持自动拆解分镜、角色音色绑定、Patch 修改 DAG 节点、触发运行与失败节点强制重试，并实时订阅时间线进度。
4. **`gateway-inspector`（只读排障与自我审视插件）**
   - **挂载域**：`mcp.ModeReadOnly`
   - **核心能力**：只读检索 `opslog`、`audit`、`health`、`gatewaykeys` 统计与配置状态，供日常对话查询平台健康度，零副作用风险。

---

## 4. 自主编排与内部优化机制 (Autonomous Orchestration & Self-Optimization)

1. **统一异步任务握手协议**
   - 当 Agent 触发耗时超过阈值（如批量号池巡检、Headed 过盾重登、长视频渲染、短剧整集生成）的动作时，插件统一提交到 `admin_bulk_task` 或 `drama.Engine` 并返回标准化句柄 `{ "task_id": "...", "status": "queued", "poll_tool": "..." }`，避免 LLM 上下文长时间阻塞挂起。
2. **工具调用自纠偏与模型自动降级**
   - 当上游免费模型输出畸形 JSON 参数或遗漏必填字段时，`dsh` 的 `OnToolValidationError` Hook 自动将结构化 Schema 错误反馈给模型进行单次轻量重试；若当前账号/模型连续不支持工具调用，自动切换至已验证支持 `tool_calls` 的后备渠道。
3. **上下文防膨胀（Context Budget Guard）**
   - 限制单次挂载的 Plugin 数量（按意图路由，例如运维问题只注入 `ops-healer` + `gateway-inspector`），并对大体积日志/账号列表自动分页与摘要，避免挤爆免费模型的上下文窗口。
4. **可审计的人机协同审批流（Human-in-the-Loop）**
   - 对清空号池、批量删除作品、修改全局代理、覆盖配置等破坏性动作，由 `PolicyGate` 默认拦截并返回 `approval_required` 事件卡片；用户在 `#/chat` 点击确认后，携带签名 `confirm_token` 恢复执行。

---

## 5. 分阶段演进路径 (Phased Implementation Roadmap)

### Phase 1：抽出 `internal/harness` 最小内核与统一对话 Copilot
- 提取当前散落在 `#/chat` 与 `ChatHandler` 中的 Copilot 工具循环逻辑至 `internal/harness/runner.go`。
- 封装 `LLMDriver` 对接 `PoolChatExecutor`，封装 `MCPToolAdapter` 对接 `internal/mcp.Registry`。
- 支持标准 SSE 事件流输出及客户端取消联动，保持现有前端零破坏升级。

### Phase 2：业务域插件化与 `PolicyGate` 安全护栏落地
- 定义 `HarnessPlugin` 接口，拆分实现 `ops-healer`、`studio-creator`、`drama-director`、`gateway-inspector` 四个内置插件。
- 落地 `BeforeToolCall` / `AfterToolCall` Hook：强制零明文凭据脱敏、高危动作 `dry_run` / `approval_required` 暂停恢复机制，以及统一写入 `internal/audit`。

### Phase 3：自主巡检编排与外部标准对齐
- 接入后台定时自愈 Trigger（低活跃时段触发只读诊断，异常率超阈值时生成运维建议工单或按策略执行安全自愈）。
- 对外保持 `POST /admin/api/v1/mcp`（Streamable HTTP）与内部 `dsh` 插件共用同一套工具与状态存储（SQLite/Neon PG 双驱动持久化运行记录），实现**一套能力插件同时服务内部自主 Agent 与外部编程/运维 Agent**。

---

## 6. 影响与权衡 (Consequences)

- **正面收益**：
  - 将「推理驱动」「循环控制」「安全审批」「域能力实现」彻底解耦，后续新增工坊或运维动作只需实现一个 `HarnessPlugin` 并注册到 `internal/mcp` 即可被内外 Agent 自动复用。
  - 严格保持单 Go 二进制静态分发（`CGO_ENABLED=0`）与 `web/embed.go` 内嵌部署零复杂度。
- **需要注意的约束与风险**：
  - 免费逆向账号池在多步 Agent Loop 并发下消耗更快，必须通过 `ContextManager` 严格控制工具列表体积与单会话最大步数（如默认 `max_steps <= 8`），防止死循环耗尽号池配额。
  - 插件异步任务需统一清理与超时回收，避免异常中断导致后台孤儿任务堆积。
