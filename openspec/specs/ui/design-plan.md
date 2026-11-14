# AI-Web-Free 多供应商免费号池网关与 UI 架构设计方案（方案 A + 双层标准）

## 1. 仓库与技术栈基线

- **项目名称**：`ai-web-free`（AI Web Free Pool & Multi-Provider Reverse Proxy Hub）
- **远程仓库**：`https://github.com/lwpk110/ai-web-free`
- **技术栈架构**：
  - **后端**：100% 纯 Golang（Module: `ai-web-free`，严格保持 `CGO_ENABLED=0` 静态二进制编译与 `embed.FS` 打包）。
  - **前端（双层 AI 全栈 UI 标准）**：
    - **全局控制台层（Tailwind CSS + Porcelain / Obsidian 精工令牌体系）**：采用方案 A「可折叠分组左侧栏 + 面包屑顶栏」骨架，默认 **Porcelain 极简亮色**（`#f8fafc` / `#ffffff` / `rgba(15,23,42,0.08)` 发丝线 + 顶部高光层）并支持一键切换 **Obsidian 精致暗色**（`#09090b` / `#18181b` / `rgba(255,255,255,0.08)`），主题选择持久化至 `localStorage`；侧栏收起时底部状态卡自动收敛为居中的 `Pool Pulse` 呼吸圆点（悬停 Tooltip 展示实时健康配额）。
    - **前端本地持久化（Web IndexedDB `ai_web_free_studio_db`）**：采用零 CGO 约束下的浏览器 IndexedDB 方案，将图像创作工坊（`#/image`）生成的 PNG `Blob` / 远程链接及提示词元数据与对话操场（`#/chat`）会话历史持久化至本地，刷新页面无损恢复；同时保留多模态资产下载与导出能力。
    - **生成式交互层（Thesys OpenUI 局部作用域）**：保留 `@openuidev/react-lang`、`@openuidev/react-ui` 与预生成的 `web/system-prompt.txt`，但**严格收敛在对话操场（`#/chat`）的消息流 `<Renderer />` 域内**（`<ThemeProvider>` + `.openui-scope`），避免对话主题污染控制台全局组件。

---

## 2. 方案 A 信息架构与 6 大核心视图

导航分两大分组，支持手动点击折叠/展开左侧栏（状态完全由用户控制，切换至图像创作工坊 `#/image` 等路由时不自动收缩），顶栏提供 `48px` 半透明毛玻璃面包屑、`⌘K` 快速命令面板、Porcelain/Obsidian 主题切换、`/v1` 实时端点状态胶囊与 `ADMIN_TOKEN` 安全锁入口：

| 分组 | 路由 | 视图名称 | 核心能力与接口联动 |
| :--- | :--- | :--- | :--- |
| **监控与运维** | `#/overview` | **网关总览大盘** | 号池可用率、卡密串行自动续期队列实时态、代理节点健康度、最近 1000 次请求自愈率 Sparkline KPI、真实审计日志 + 代理延迟聚合的双指标柱状图（`DualThroughputChart`）、点击直达详情与 `/v1` 能力矩阵 |
| **监控与运维** | `#/pools` | **号池与供应商** | 多供应商切换（ChatGPT Web 已激活 + 规划中槽位）；首字母头像、掩码 Token + `tls_profile` 标签、动态色彩 TTL 进度条与悬停无边框静默操作（`.hover-reveal`）；分级冷却状态展示与倒计时；表头全选与筛选结果全量选择、批量代理编辑、异步串行卡密重登进度与逐账号结果、批量测活/导入/**导出 (`sub2api_json` / `access_tokens` / `kami`)**；30 秒 SVG 动态进度环实时 2FA TOTP 弹窗（`TotpRingModal`） |
| **监控与运维** | `#/network` | **代理与接入指南** | `proxies.txt` 代理节点健康矩阵（延迟、可用状态、绑定账号数、连续失败数）；一键延迟测活 (`POST /admin/api/v1/proxies/probe`)；保存热更新校绑；实时聚合 `GET /v1/models?include_builtins=true` 目录刷新（覆盖对话、`gpt-image-*`/`dall-e-3` 图像及 `tts-1`/`whisper-1` 音频能力）、分类筛选与 `cURL` / `Python` / `TypeScript` 调用示例 |
| **监控与运维** | `#/logs` | **请求日志与 Failover** | 采用 Helicone / Langfuse 风格主从分栏追踪视口（`lg:grid-cols-[1fr_370px]` Split Trace Inspector），基于零 CGO 内存环形缓冲支持筛选、状态切换与右侧实时诊断/切号轨迹检查器 |
| **模型操场** | `#/chat` | **AI Copilot · 平台诊断与 OpenUI** | 统一多轮对话 Copilot（支持最近 12 轮历史、`/` 技能胶囊 `/diagnose` `/fix` `/openui` `/image` `/clear` 及运维关键词自动意图识别）；混合渲染格式化 Markdown 与局部 `.openui-scope` 内嵌 `<Renderer />` 卡片；当号池可用账号耗尽或网关熔断时自动降级为基于 `/admin/api/v1/diagnostics` 的离线诊断分析与内联自愈控制台 |
| **模型操场** | `#/image` | **图像创作工坊** | 左侧 `336px` 独立参数台支持 `gpt-image-1` / `gpt-image-2.5-sunburst` / `flare` / `2.6` / `dall-e-3` 文生图、SVG 比例线框选择器、多图参考编辑（`/v1/images/edits`）、自动 Failover 切号提示、Prompt 标题栏「AI 魔法扩写」/「随机灵感」与可交互「变量槽位填空」；右侧点阵网格（`.studio-grid-bg`）支持 `🔥 提示词广场`（接入 Civitai / Lexica / Reddit / HuggingFace 多源定时爬虫 + 本机号池 AI 提炼与联网搜索，支持分类筛选、双语切换、星标收藏与全参数联动「做同款 / 立即生成」）与 `🖼️ 我的作品库`（IndexedDB 二进制 `Blob` 持久化、Remix 复刻与分栏灯箱预览）双 Tab 切换（详见 [prompt-plaza.md](./prompt-plaza.md)） |

---

## 3. Thesys OpenUI (`openui-lang`) 局部规范

- **扩展组件库（`web/src/lib/openui-library.tsx`）**：在 `@openuidev/react-ui` 的 `openuiChatLibrary` 基础上通过 `zod/v4` 注册 `PoolOverviewCard`、`AccountDiagnosticTable`、`ImageStudioCard` 与 `PlatformDiagnosisCard`。
- **对外 `/v1/chat/completions` 兼容**：默认保持标准 OpenAI Chat Completions 行为；携带请求头 `X-OpenUI-Lang: true` 时由 Go 网关自动前置注入内嵌的 `web/system-prompt.txt`。
