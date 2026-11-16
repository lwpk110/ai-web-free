# 生产级专业 AI 对话工作台（Chat Studio）与插件化向量扩展设计规范

## 1. 概述与背景 (Overview & Objectives)

在当前版本中，AI-Web-Free 控制台的对话功能（`#/chat`，即 `GenerativeWorkbench.tsx`）主要扮演两重角色：
1. 智能运维 Copilot（处理 `/diagnose`、`/fix` 等平台根因诊断自愈指令）；
2. Thesys OpenUI 动态卡片 DSL 渲染器。

然而，现有实现仅将对话暂存在单一 `session_key="default"` 的全局槽位中，缺少会话列表与组织抽屉，不支持会话历史回溯、重命名、置顶、导入导出；消息级交互亦缺少重新生成、流式打断（Stop）、Token 消耗与联网引用来源展示。

本规范旨在将 `#/chat` 升级为对标业界顶尖产品（ChatGPT、Claude、TypingMind、Open WebUI）的**全功能、高生产力专业 AI 对话工作台（Chat Studio）**，并在**严格恪守系统零 CGO 与单二进制发布红线**的前提下，确立历史检索与向量数据库的工程边界。

---

## 2. 业界产品调研与关键架构权衡 (Benchmarking & Trade-offs)

### 2.1 业界优秀产品调研矩阵

| 标杆产品 | 核心架构特征 | 交互亮点 | 本项目采纳策略 |
| :--- | :--- | :--- | :--- |
| **ChatGPT / Claude.ai** | 云端微服务 + SSE 流式 + 会话树 | 按时间维度（今天/昨天/7天前）分组、中途随时中断生成、编辑上一轮提问、引用来源浮层 | 采纳其经典会话组织抽屉、消息中途打断、提问编辑与重新生成交互 |
| **Open WebUI** | Python FastAPI + SvelteKit + SQLite/Chroma | 会话管理抽屉、角色预设广场（Presets）、模型横向对比、多模态附件 | 采纳预设角色模板体系与侧边栏折叠抽屉，舍弃其重型 Python/Chroma 依赖 |
| **LibreChat** | Node.js + MongoDB + Redis + MeiliSearch + pgvector | 深度会话分支树（Forking）、插件系统、完整导入导出 | 采纳 Markdown/JSON 导入导出格式，避免其繁复的重型中间件矩阵 |
| **TypingMind** | Local-First 纯前端 + IndexedDB + 本地倒排索引 | 毫秒级极速响应、离线可用、按文件夹/标签分类、纯内存全文搜索 | 采纳 Local-First 理念与前端毫秒级倒排索引，与服务端 SQLite 组成双写镜像 |

### 2.2 中间件与向量数据库（LanceDB）选型剖析

针对是否引入 LanceDB、Redis 或外部向量数据库的重大疑问，规范确立以下准则：

1. **坚决排斥嵌入式 LanceDB（红线约束）**：
   - 本项目核心红线：`CGO_ENABLED=0` 纯静态编译，基于 `distroless/static-debian12:nonroot`。
   - LanceDB 底层为 Rust 开发，其 Go SDK 依赖 CGO / C-FFI 绑定。在纯静态编译模式下**无法构建**。
   - 强行引入 LanceDB 独立容器会极大增加用户部署门槛，违背单二进制极简私有化交付原则。
2. **全文检索（FTS）优先覆盖 99% 的核心诉求**：
   - 历史会话查找的核心场景为精准定位特定代码、报错、配置等字词（例如搜索包含“dockerfile”或“turnstile”的会话）。
   - 纯 Go SQLite 原生 FTS5 或前端 MiniSearch 倒排索引在**零外部中间件、零额外开销**下即可实现毫秒级高亮检索，性价比远超向量检索。
3. **插件化 VectorStore 适配器模式（可扩展但不强绑）**：
   - 后端抽象标准 `VectorStore` 驱动接口。
   - 默认启用 `memory` / `sqlite` 纯 Go 基础驱动（零 CGO，基于余弦相似度）。
   - 环境变量可选外接企业已有的 `qdrant` / `pgvector` / `milvus`（基于 HTTP REST 客户端通信，无 CGO 依赖），按需开启。

---

## 3. 系统架构与数据模型 (System Architecture & Models)

```
+-----------------------------------------------------------------------------------------+
|                                    前端展现与交互层                                     |
|  [ 会话抽屉侧边栏 ]      [ 顶部模型/预设控制栏 ]      [ 消息流渲染区 (OpenUI / Markdown) ]  |
|  - 今天/昨天/7天前       - 模型切换 / 角色预设        - 编辑/重试/中途打断/复制/Token    |
|  - 置顶/重命名/删除      - 全局全文检索 (Ctrl+K)      - 联网搜索引用角标 (Citations)      |
+-----------------------------------------------------------------------------------------+
                                          | 双写镜像与离线缓存 (Hybrid Sync)
                                          v
+-----------------------------------------------------------------------------------------+
|                            前端存储：IndexedDB (Local-First)                            |
|                            - MiniSearch 纯前端内存倒排索引                              |
+-----------------------------------------------------------------------------------------+
                                          | RESTful 增量同步
                                          v
+-----------------------------------------------------------------------------------------+
|                              后端服务层 (/admin/api/v1/chat/*)                          |
|  - 会话 CRUD 调度        - 批量导入导出               - SQLite FTS5 全文搜索             |
+-----------------------------------------------------------------------------------------+
                                          |
                                          +-----------------------------------------------+
                                          v                                               v
              +---------------------------------------+       +---------------------------+
              | 核心持久化存储 (GORM 纯 Go 驱动)      |       | 可选插件: VectorStore     |
              | - conversations 表                    |       | - 默认: memory / sqlite   |
              | - chat_message_records 表             |       | - 可选外接: Qdrant REST   |
              | - chat_presets 表                     |       | - 可选外接: PGVector      |
              +---------------------------------------+       +---------------------------+
```

### 3.1 实体模型定义

```go
// Conversation 会话主表
type Conversation struct {
	ID            string    `gorm:"primaryKey;size:64" json:"id"`
	Title         string    `gorm:"size:255;index" json:"title"`
	Model         string    `gorm:"size:64" json:"model"`
	SystemPrompt  string    `gorm:"type:text" json:"system_prompt,omitempty"`
	PresetID      string    `gorm:"size:64;index" json:"preset_id,omitempty"`
	HeadMessageID string    `gorm:"size:64" json:"head_message_id,omitempty"`
	CustomJSON    string    `gorm:"type:text" json:"custom_json,omitempty"`
	IsPinned      bool      `gorm:"default:false;index" json:"is_pinned"`
	IsArchived    bool      `gorm:"default:false;index" json:"is_archived"`
	Tags          string    `gorm:"size:255" json:"tags,omitempty"`
	ClientIP      string    `gorm:"size:64" json:"client_ip,omitempty"`
	MessageCount  int       `gorm:"default:0" json:"message_count"`
	CreatedAt     time.Time `gorm:"index" json:"created_at"`
	UpdatedAt     time.Time `gorm:"index" json:"updated_at"`
}

// ChatMessageRecord 消息明细表（支持基于 ParentID 的分支树与多模态 Parts）
type ChatMessageRecord struct {
	ID              string    `gorm:"primaryKey;size:64" json:"id"`
	ConversationID  string    `gorm:"size:64;index;not null" json:"conversation_id"`
	ParentID        string    `gorm:"size:64;index" json:"parent_id,omitempty"`
	Role            string    `gorm:"size:32" json:"role"` // "user" | "assistant" | "system"
	Content         string    `gorm:"type:text" json:"content"`
	PartsJSON       string    `gorm:"type:text" json:"parts_json,omitempty"`
	AttachmentsJSON string    `gorm:"type:text" json:"attachments_json,omitempty"`
	MetadataJSON    string    `gorm:"type:text" json:"metadata_json,omitempty"`
	StatusJSON      string    `gorm:"type:text" json:"status_json,omitempty"`
	Model           string    `gorm:"size:64" json:"model,omitempty"`
	OpenUIMode      bool      `gorm:"default:false" json:"openui_mode"`
	Skill           string    `gorm:"size:32" json:"skill,omitempty"`
	TokensPrompt    int       `gorm:"default:0" json:"tokens_prompt,omitempty"`
	TokensComp      int       `gorm:"default:0" json:"tokens_comp,omitempty"`
	LatencyMs       int64     `gorm:"default:0" json:"latency_ms,omitempty"`
	Citations       string    `gorm:"type:text" json:"citations,omitempty"` // 存放引用来源 JSON
	CreatedAt       time.Time `gorm:"index" json:"created_at"`
}

// ChatPreset 角色与系统提示词预设表
type ChatPreset struct {
	ID           string    `gorm:"primaryKey;size:64" json:"id"`
	Name         string    `gorm:"size:128;not null" json:"name"`
	Description  string    `gorm:"size:255" json:"description"`
	Icon         string    `gorm:"size:64" json:"icon"`
	SystemPrompt string    `gorm:"type:text;not null" json:"system_prompt"`
	DefaultModel string    `gorm:"size:64" json:"default_model"`
	IsBuiltin    bool      `gorm:"default:false" json:"is_builtin"`
	SortOrder    int       `gorm:"default:0" json:"sort_order"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}
```

---

## 4. 后端 API 规范与端点契约

所有 API 归属于 `/admin/api/v1/chat/*`，由 `ADMIN_TOKEN` 鉴权保护：

1. **`GET /admin/api/v1/chat/conversations`**
   - 查询参数：`page` (默认 1), `limit` (默认 30), `q` (标题检索), `archived` (布尔值，默认 false)。
   - 返回格式：`{ "conversations": [...], "total": 120, "page": 1, "limit": 30 }`。
   - 排序规则：`is_pinned DESC, updated_at DESC`。

2. **`POST /admin/api/v1/chat/conversations`**
   - 载荷参数：`{ "title"?: string, "model"?: string, "system_prompt"?: string, "preset_id"?: string }`。
   - 行为：生成 UUID，初始化会话元数据，返回创建成功的 `Conversation` 对象。

3. **`GET /admin/api/v1/chat/conversations/:id`**
   - 返回指定会话元数据及全量消息列表：`{ "conversation": {...}, "messages": [...] }`。

4. **`PATCH /admin/api/v1/chat/conversations/:id`**
   - 支持动态更新标题、切换模型、修改预设、修改置顶状态（`is_pinned`）或归档状态（`is_archived`）。

5. **`DELETE /admin/api/v1/chat/conversations/:id`**
   - 级联物理删除会话及关联的所有 `ChatMessageRecord`。

6. **`POST /admin/api/v1/chat/conversations/:id/messages`** 与 **`POST /admin/api/v1/chat/conversations/:id/messages/append`**
   - 全量覆盖或单节点幂等增量写入（Upsert），支持 `parentId`、`partsJson`、`attachmentsJson`、`metadataJson`、`statusJson` 与 `move_head` 同步更新 `Conversation.HeadMessageID`、`MessageCount` 与 `UpdatedAt`。

7. **`GET /admin/api/v1/chat/search`**
   - 查询参数：`q=关键词`。
   - 行为：基于 SQLite FTS5 或 LIKE 索引跨全量历史消息检索，返回命中的会话摘要及关键词高亮匹配片段（Snippets）。

8. **`GET /admin/api/v1/chat/presets` & `POST /admin/api/v1/chat/presets`**
   - 角色预设的列表拉取与自定义预设保存。

9. **`POST /admin/api/v1/chat/conversations/export` & `import`**
   - 导出为标准 Markdown（含时间戳、角色与对话文本）或 JSON 归档文件；支持一键导入恢复。

---

## 5. 前端交互与组件增强规范

### 5.1 会话抽屉侧栏 (Conversation Drawer)
- **多维度时间分组**：
  - 置顶会话（Pinned，常驻置顶区带 📌 徽章）
  - 今天（Today）
  - 昨天（Yesterday）
  - 过去 7 天（Previous 7 Days）
  - 更早历史（Older）
- **会话快捷菜单**：
  - 双击标题或点击编辑图标原地重命名
  - 快捷切换置顶/取消置顶
  - 单条删除与清空会话

### 5.2 消息流高频交互体系
- **上一轮提问编辑（Edit & Retry）**：
  - 鼠标悬浮在用户气泡时浮现「编辑」按钮。
  - 点击后提问文本直接回填至输入框，用户修改后重新发送，系统自动截断该轮以后的历史并启动流式生成。
- **重新生成（Regenerate）与模型热切**：
  - 助手消息底部提供「重新生成」与下拉「使用其他模型重新生成」按钮。
- **流式即时中断（Stop Generating）**：
  - 流式 SSE 传输中，输入框右侧发送按钮动态变更为红色停止方块。
  - 点击即时触发 `AbortController.abort()`，前端保留已生成的文字并保存至当前快照。
- **消息元数据与指标 Badges**：
  - 助手回答底部展示小号灰色文本：`850ms · 320 tokens · model: gpt-4o`。
  - 联网搜索时展示 `🔍 引用了 3 个网页来源` 折叠面板，展开可点击查看标题与外部链接。
- **平台诊断 Copilot 与 OpenUI 无缝嵌入**：
  - 保留 `/diagnose`、`/fix` 快捷命令补全。
  - 会话中生成的 OpenUI DSL 组件保持交互能力与自愈按钮。

### 5.3 离线优先双写同步 (Hybrid Sync)
- 前端通过 IndexedDB（`ai_web_free_studio_db`）本地维护 `conversations` 与 `chat_messages`。
- 用户操作（新建会话、输入消息、重命名）优先在本地 IndexedDB 生效并立即触发 React 状态重渲染（0 毫秒卡顿）。
- 后台通过微任务异步向 `/admin/api/v1/chat/*` 增量推送变更；离线或网络故障时打上本地脏标记，恢复后自动合并同步。

---

## 6. 验证与约束符合性 (Verification & Constraints)

1. **严格零 CGO**：全套方案基于纯 Go GORM + 纯 Go SQLite（`glebarez/sqlite`），无任何外部动态链接依赖。
2. **前后端构建一致性**：
   - 前端代码修改后执行：`cd web && npm ci && npx tsc --noEmit && npm run build`。
   - Go 后端验证：`go test ./...`。
   - 静态构建验证：`CGO_ENABLED=0 go build -o ai-web-free .`。
