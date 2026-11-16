# ADR-0014: 本项目最终目的是agent 驱动，不管是外部还是内部的agent,那么是否所有的api都需要mcp工程化

<!-- grill-me-session: 6b6cc136-cc0a-4983-a052-6e22ea16cc74:0 -->
<!-- grill-me-topic-hash: 462d880fabd3 -->

| 字段 | 内容 |
| :--- | :--- |
| 状态 | Accepted |
| 日期 | 2026-11-16 |
| 来源会话 | `6b6cc136-cc0a-4983-a052-6e22ea16cc74` (step `0`) |
| 触发方式 | `/grill-me` 架构访谈自动归档 |

## 1. 背景与问题定义 (Context & Problem Statement)

- **原始 `/grill-me` 诉求**：/grill-me 本项目最终目的是agent 驱动，不管是外部还是内部的agent,那么是否所有的api都需要mcp工程化

## 2. 决策树访谈记录 (Decision Drivers & Grill-Me Q&A)

#### 决策分支 1（单选）：当前项目已有三类接口：1) `/v1/*` OpenAI 数据面与流式/静态资源；2) `/admin/api/v1/*` 号池运维控制面（账号/代理/诊断/日志）；3) 工坊与资产能力（短剧/画廊/对话工作台/媒体爬虫/多模态）。从 Agent 驱动目标出发，应该如何界定 MCP 化的范围？

候选方案：
- (Recommended) 分层暴露 + 意图聚合：排除 /v1/* 数据面与静态/流式端点，仅将「运维控制面」和「工坊/资产能力」按业务意图聚合为粗粒度 MCP Tools/Resources
- 仅高频核心场景 MCP 化：只保留短剧/图像/运维诊断等少数高频工作流做 MCP，其余 Admin API 保持纯 REST
- 全量 1:1 MCP 化：把 router.go 中几乎所有 /admin/api/v1/* 和 /v1/* 接口都自动或手动映射为 MCP Tool

> **已选决策**：我觉的应该大部分mcp化，从内部agent出发可以做内部agnet 驱动运维甚至开发，例如通过chat 指挥agent 做产品，故障诊断，而外部agent可以通过mcp 诊断当前用户的故障信息，产品创作

#### 决策分支 2（单选）：针对「大部分 MCP 化」的具体粒度与边界：1）`/v1/chat/completions` 等大模型推理管道本身是 Agent 的“大脑”，静态资源/流式下载是传输层；2）多模态生成（生图/TTS/视频/爬虫）是“产品创作”核心能力；3）`/admin/api/v1/*` 目前有 60+ 细粒度 REST 接口。应该如何处理粒度与 `/v1/*` 的取舍？

候选方案：
- (Recommended) 排除对话推理与静态流式端点，将多模态生成（图/音/视频/爬虫）纳入 MCP，并将 60+ Admin REST 按域聚合为约 15~25 个意图级 Tool + Read-Only Resource
- 仅将 /admin/api/v1/* 全部 1:1 映射为 Tool（保留粒度），/v1/* 全部不进 MCP
- 连同 /v1/chat/completions、/v1/models 在内的全部业务端点均 1:1 生成 MCP Tool

> **已选决策**：(Recommended) 排除对话推理与静态流式端点，将多模态生成（图/音/视频/爬虫）纳入 MCP，并将 60+ Admin REST 按域聚合为约 15~25 个意图级 Tool + Read-Only Resource

#### 决策分支 3（单选）：关于「内部 Agent（如 `#/chat` Copilot / 后台运维 Agent）与外部 Agent（如 Claude Code / Cursor / Antigravity）」的调用架构，两者应该如何共享这些 MCP 能力？

候选方案：
- (Recommended) 统一 Go Tool/Resource 注册表（In-Process 共用核心）：内部 Agent/Copilot 进程内直调，外部 Agent 通过 MCP Streamable HTTP 接入，共用同一套 Schema、鉴权与审计日志
- HTTP 回环统一：内部 Agent 也通过本地 HTTP 调用 MCP 端点，保持协议层完全一致
- 内外分离：外部 Agent 走 MCP 协议，内部 Chat Copilot 继续由前端或专用 REST 驱动

> **已选决策**：(Recommended) 统一 Go Tool/Resource 注册表（In-Process 共用核心）：内部 Agent/Copilot 进程内直调，外部 Agent 通过 MCP Streamable HTTP 接入，共用同一套 Schema、鉴权与审计日志

#### 决策分支 4（单选）：目前仓库已存在 `POST /admin/api/v1/drama/mcp`。扩展到运维诊断与全工坊创作后，MCP 端点拓扑与上下文防膨胀策略应该如何设计？

候选方案：
- (Recommended) 统一入口 + 域过滤（如 `/admin/api/v1/mcp` 支持 `?scope=ops|studio|drama|all`），同时保留 `/admin/api/v1/drama/mcp` 作为兼容别名
- 按域彻底拆分多个独立 MCP 端点（ops/mcp、drama/mcp、studio/mcp 等），不提供统一聚合入口
- 单一全量 `/mcp` 端点，始终返回全部工具不做域过滤

> **已选决策**：(Recommended) 统一入口 + 域过滤（如 `/admin/api/v1/mcp` 支持 `?scope=ops|studio|drama|all`），同时保留 `/admin/api/v1/drama/mcp` 作为兼容别名

#### 决策分支 5（单选）：`AGENTS.md` 规定了凭据脱敏与 `ADMIN_TOKEN` 鉴权，而运维与资产管理中存在删除作品/项目、清空日志、覆盖代理、批量登录/修复等高危副作用操作。MCP 化的安全与副作用边界应如何控制？

候选方案：
- (Recommended) 零明文凭据 + 权限分级（支持 `?mode=readonly`）+ 破坏性/高危动作强制 `dry_run` 或 `confirm: true` 二次确认并记入审计日志
- 仅依赖 `ADMIN_TOKEN` 鉴权：只要持有管理令牌，MCP 工具直接执行所有读写/删除/导出动作，不做额外护栏
- 运维域 MCP 严格只读（仅允许查日志与诊断），所有写操作与修复动作仅限创作工坊或人工后台执行

> **已选决策**：(Recommended) 零明文凭据 + 权限分级（支持 `?mode=readonly`）+ 破坏性/高危动作强制 `dry_run` 或 `confirm: true` 二次确认并记入审计日志

#### 决策分支 6（单选）：最后一个架构细节：考虑到部分外部 Agent 客户端对 `MCP Resources` 支持不完整，以及视频生成、卡密登录、短剧编排等长耗时任务的特性，MCP 原语（Tools/Resources）与异步任务交互模式应如何设计？

候选方案：
- (Recommended) 双模只读 + 异步任务句柄：状态/日志/能力矩阵同时暴露为 MCP Resource 与只读 Tool（兼容仅支持 Tool 的客户端）；长耗时任务（生视频/卡密登录/短剧运行/全池探测）一律返回 task_id 并配合状态查询 Tool
- 严格正交分离：所有读取仅用 MCP Resource，所有动作仅用 MCP Tool；长耗时任务在 Tool 内同步阻塞等待完成
- 纯 Tool 模式：不实现 MCP Resources，全部读写与状态查询统一用 MCP Tools

> **已选决策**：(Recommended) 双模只读 + 异步任务句柄：状态/日志/能力矩阵同时暴露为 MCP Resource 与只读 Tool（兼容仅支持 Tool 的客户端）；长耗时任务（生视频/卡密登录/短剧运行/全池探测）一律返回 task_id 并配合状态查询 Tool

## 3. 已收敛的架构决策摘要 (Confirmed Decisions Summary)

- **决策 1**：当前项目已有三类接口：1) `/v1/*` OpenAI 数据面与流式/静态资源；2) `/admin/api/v1/*` 号池运维控制面（账号/代理/诊断/日志）；3) 工坊与资产能力（短剧/画廊/对话工作台/媒体爬虫/多模态）。从 Agent 驱动目标出发，应该如何界定 MCP 化的范围？ → **我觉的应该大部分mcp化，从内部agent出发可以做内部agnet 驱动运维甚至开发，例如通过chat 指挥agent 做产品，故障诊断，而外部agent可以通过mcp 诊断当前用户的故障信息，产品创作**
- **决策 2**：针对「大部分 MCP 化」的具体粒度与边界：1）`/v1/chat/completions` 等大模型推理管道本身是 Agent 的“大脑”，静态资源/流式下载是传输层；2）多模态生成（生图/TTS/视频/爬虫）是“产品创作”核心能力；3）`/admin/api/v1/*` 目前有 60+ 细粒度 REST 接口。应该如何处理粒度与 `/v1/*` 的取舍？ → **(Recommended) 排除对话推理与静态流式端点，将多模态生成（图/音/视频/爬虫）纳入 MCP，并将 60+ Admin REST 按域聚合为约 15~25 个意图级 Tool + Read-Only Resource**
- **决策 3**：关于「内部 Agent（如 `#/chat` Copilot / 后台运维 Agent）与外部 Agent（如 Claude Code / Cursor / Antigravity）」的调用架构，两者应该如何共享这些 MCP 能力？ → **(Recommended) 统一 Go Tool/Resource 注册表（In-Process 共用核心）：内部 Agent/Copilot 进程内直调，外部 Agent 通过 MCP Streamable HTTP 接入，共用同一套 Schema、鉴权与审计日志**
- **决策 4**：目前仓库已存在 `POST /admin/api/v1/drama/mcp`。扩展到运维诊断与全工坊创作后，MCP 端点拓扑与上下文防膨胀策略应该如何设计？ → **(Recommended) 统一入口 + 域过滤（如 `/admin/api/v1/mcp` 支持 `?scope=ops|studio|drama|all`），同时保留 `/admin/api/v1/drama/mcp` 作为兼容别名**
- **决策 5**：`AGENTS.md` 规定了凭据脱敏与 `ADMIN_TOKEN` 鉴权，而运维与资产管理中存在删除作品/项目、清空日志、覆盖代理、批量登录/修复等高危副作用操作。MCP 化的安全与副作用边界应如何控制？ → **(Recommended) 零明文凭据 + 权限分级（支持 `?mode=readonly`）+ 破坏性/高危动作强制 `dry_run` 或 `confirm: true` 二次确认并记入审计日志**
- **决策 6**：最后一个架构细节：考虑到部分外部 Agent 客户端对 `MCP Resources` 支持不完整，以及视频生成、卡密登录、短剧编排等长耗时任务的特性，MCP 原语（Tools/Resources）与异步任务交互模式应如何设计？ → **(Recommended) 双模只读 + 异步任务句柄：状态/日志/能力矩阵同时暴露为 MCP Resource 与只读 Tool（兼容仅支持 Tool 的客户端）；长耗时任务（生视频/卡密登录/短剧运行/全池探测）一律返回 task_id 并配合状态查询 Tool**

## 4. 落地方案与影响范围 (Implementation Plan & Consequences)

经过访谈确认，结论是：**不需要将所有 API 1:1 机械包装为 MCP，而应采用「分层过滤 + 意图聚合 + 统一注册表」的 MCP 工程化架构**。

### 架构决策总结

1. **API 分层与 MCP 边界（不全部 1:1 映射）**
   - **排除出 MCP**：`/v1/chat/completions`、`/v1/responses`、`/v1/models`（大模型推理管道本身是 Agent 的“大脑”而非工具），以及 `/storage/*`、`/health*`、`/auth/*`、`OPTIONS`（静态资源与传输底座）。
   - **纳入 MCP（产品创作域）**：多模态生成与媒体能力（生图 `/v1/images/*`、TTS/音色 `/v1/audio/*`、视频 `/v1/videos/*`、媒体爬虫 `/v1/media/crawl*`），使内外 Agent 都能直接驱动产品创作。
   - **聚合进 MCP（运维与工坊域）**：将 [router.go](file:///home/luwei/workspace/github/ai-web-free/internal/handler/router.go#L200-L299) 中 60+ 细粒度 `/admin/api/v1/*` 接口按领域与业务意图聚合为约 **15~25 个粗粒度 MCP Tools**（覆盖运维诊断、号池/代理、画廊、对话工作台、短剧编排等），避免 Tool 数量爆炸挤占 LLM 上下文。

2. **内外 Agent 统一驱动架构**
   - 在 Go 后端建设**统一的 Tool / Resource 注册表（In-Process Registry）**：
     - **内部 Agent（`#/chat` Copilot / 后台运维 Agent）**：进程内直接调度该注册表与业务 Service，零 HTTP 回环开销。
     - **外部 Agent（Claude Code / Cursor / Antigravity 等）**：通过 MCP Streamable HTTP 协议接入同一注册表，确保 Schema、权限校验与审计日志完全一致。

3. **端点拓扑与作用域过滤**
   - 提供统一入口 `POST /admin/api/v1/mcp`，支持通过 `?scope=ops|studio|drama|all` 按需挂载工具集，控制上下文体积。
   - 保留现有 [drama_mcp.go](file:///home/luwei/workspace/github/ai-web-free/internal/handler/drama_mcp.go) 的 `POST /admin/api/v1/drama/mcp` 作为 `scope=drama` 的向后兼容别名。

4. **安全、脱敏与高危副作用护栏**
   - **零明文凭据**：MCP 工具输出与 Resource 严禁返回明文 `password`、`totp_secret`、`access_token`、`refresh_token` 等敏感凭据。
   - **分级与二次确认**：支持 `?mode=readonly` 只读接入模式（适合外部故障排查 Agent）；为工具补充 `readOnlyHint` / `destructiveHint` 注解；删除、清空、覆盖代理、批量登录/修复等高危动作强制要求 `dry_run` 预览或显式 `confirm: true` 参数，并全量记入运维审计日志。

5. **MCP 原语与长耗时异步模式**
   - **双模只读**：系统概览、诊断报告、日志、能力矩阵同时暴露为 **MCP Resource** 与**只读 Tool**，兼容仅支持 `tools/call` 的外部客户端。
   - **异步句柄**：视频生成、卡密登录、短剧编排运行、全池探测等长耗时操作一律立即返回 `task_id` / `run_id`，配合状态查询 Tool 跟踪进度。

---

如果你希望基于这份决策继续推进，我可以下一步为你：
1. 输出各域（`ops` / `studio` / `drama`）的详细 **MCP Tool & Resource 清单与 JSON Schema 规范**；或
2. 直接开始搭建 `internal/mcp/` 统一注册表并重构接入现有 [drama_mcp.go](file:///home/luwei/workspace/github/ai-web-free/internal/handler/drama_mcp.go) 与核心运维/创作工具。
