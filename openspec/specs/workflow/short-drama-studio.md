# 短剧创作工坊与音视频网关（Short Drama Studio & A/V Gateway）设计规范

## 1. 架构定位与工程约束

本规范定义 `ai-web-free` 中「免费 TTS、免费云端视频生成（Muse）与短剧工作流（Short Drama Studio）」的系统架构、网关协议、角色一致性保障体系与前端工坊设计。

- **全栈打通架构**：
  - 后端：扩展 OpenAI 兼容的音频生成（`/v1/audio/speech` 多引擎分流）与视频异步生成（`/v1/videos/generations`、`/v1/videos/:id`）标准接口。
  - 前端：新增可视化「短剧创作工坊（`#/drama`）」，支持五步向导式流水线、时间轴预览播放与一键导出剪映草稿工程。
- **严格零 CGO 与纯静态编译约束（`CGO_ENABLED=0`）**：
  - 生产镜像基于 `gcr.io/distroless/static-debian12:nonroot`，禁止引入任何 CGO 依赖或外部可执行程序假设（无本地 ffmpeg、无本地 python、无本地 C 语言音频编解码器绑定）。
  - TTS 生成与视频任务调度完全由纯 Go 运行时驱动。
- **零本地硬件依赖，100% 云端 API / 免费平台驱动**：
  - 不要求宿主机具备本地 GPU、CUDA 环境或重型本地模型；
  - 视频生成（MuseTalk / Meta AI Muse / 图生视频）完全通过云端免费免密端点（如 HuggingFace Spaces、公开 Web 端点）或赠送额度的云端聚合平台（SiliconFlow、Fal.ai、Replicate）以号池形式调度。
- **双层轻量持久化机制**：
  - 前端 IndexedDB（`ai_web_drama_db`）实时存盘，支撑用户多工程管理、富媒体 Blob 离线缓存与纯前端秒级导出打包；
  - 网关支持配置可选的环境变量 `DRAMA_DATA_DIR`，以纯 JSON 文件原子读写实现项目服务端备份，未配置时保持无状态运行。

---

## 2. 免费 TTS 多引擎网关设计 (`/v1/audio/speech`)

### 2.1 多引擎路由架构

网关对标准 OpenAI `/v1/audio/speech` 请求提供透明的多引擎调度：

```
客户端请求 POST /v1/audio/speech
           │
           ▼
    【Voice / Model 路由检查】
     ├── voice in [zh-CN-*, 晓晓, 云希, 云扬...] ──► 【Edge-TTS 纯 Go 引擎】(免鉴权、零额度消耗)
     ├── voice in [alloy, echo, fable, onyx, ...] ──► 【ChatGPT 逆向 TTS】(兼容 OpenAI 官方音色)
     └── voice in [clone:*] ──────────────────────► 【自定义/外部克隆适配器】(预留插槽)
```

### 2.2 纯 Go Edge-TTS 引擎规格

- **协议实现**：
  - **微软 Edge-TTS（主免费通道）**：纯 Go 实现 `readaloud/edge/v1` WebSocket 协议（`Sec-MS-GEC` v2 签名 + `MUID` Cookie + 51+ 实时音色目录同步），支持长文本智能分句并缓冲输出完整 MP3。
  - **本地自托管 OpenAI 兼容 TTS（零外网长期推荐）**：支持配置 `LOCAL_TTS_BASE_URL` 接入本地 Docker Kokoro-FastAPI（`docker compose --profile local-tts up -d`）或 OpenedAI-Speech，实现无公网风控、音色固定一致的本地配音合成。
  - **本地音色与样音缓存**：通过 `StorageEngine` 将 `voices/catalog.json` 与 `voice_samples/*.mp3` 缓存在本地，支持 `POST /v1/audio/voices/pull-local` 批量拉取与 `GET /v1/audio/voices/:id/preview` 秒开试听。
- **输出格式**：支持 `mp3`（默认 `audio/mpeg`）、`aac`（`audio/aac`）、`wav`、`opus`。
- **短剧主力音色表**：
  - `zh-CN-XiaoxiaoNeural`（晓晓）：短剧大女主、温柔知性、旁白解说首选。
  - `zh-CN-YunxiNeural`（云希）：沉浸式故事解说、男主对白、情感叙事。
  - `zh-CN-YunyangNeural`（云扬）：专业影视播报、冷酷霸总、男性权威角色。
  - `zh-CN-YunxiaNeural`（云夏）：活泼少女、调皮千金、萌系配角。
  - `zh-CN-YunjianNeural`（云健）：热血少年、修仙反派、武侠剑客。
  - `zh-CN-liaoning-XiaobeiNeural`（辽宁晓北）：特色幽默东北话短剧角色。
  - `zh-HK-HiuGaaiNeural` / `zh-TW-HsiaoChenNeural`：港风剧与台偶剧特色方言音色。
- **参数支持**：
  - `speed`（倍速：`0.5` ~ `2.0`，映射为 SSML `rate` 参数如 `+20%`）。
  - `pitch`（音调：支持 `+10Hz` / `-5st` 微调）。
  - 支持直接在 `input` 中透传 `<speak>` SSML 标签，实现停顿（`<break time="500ms"/>`）与多角色情感强调。

---

## 3. 云端视频生成网关设计 (`/v1/videos/*`)

### 3.1 接口契约规范 (OpenAI 风格异步视频接口)

#### 提交视频生成任务：`POST /v1/videos/generations`
- **请求体（JSON）**：
  ```json
  {
    "model": "muse-talk",
    "prompt": "极具电影质感的微表情特写，眼神坚定，光线柔和",
    "image_url": "https://.../character_face.png",
    "audio_url": "https://.../speech_line.mp3",
    "aspect_ratio": "9:16",
    "duration": 5
  }
  ```
- **同步返回**：
  ```json
  {
    "id": "vid_20261114_abc123",
    "object": "video.generation",
    "status": "queued",
    "created_at": 1731564000,
    "model": "muse-talk"
  }
  ```

#### 轮询任务状态：`GET /v1/videos/:id`
- **返回结构**：
  ```json
  {
    "id": "vid_20261114_abc123",
    "status": "succeeded",
    "progress": 100,
    "video_url": "https://.../output.mp4",
    "thumbnail_url": "https://.../output_thumb.jpg",
    "duration": 5.2,
    "error": null
  }
  ```
- **状态迁移**：`queued`（排队中） -> `processing`（云端渲染中） -> `succeeded`（完成） / `failed`（失败并返回可读诊断信息）。

### 3.2 云端视频 Provider 号池调度

- **免费云端渠道接入**：
  - **Meta AI / Muse 网页端及云端免密端点**：通过逆向会话池模拟请求，提取生成直链。
  - **HuggingFace Spaces 公开免算力实例**：基于 Gradio Client API 封装，将角色立绘与音频送入公开的 MuseTalk Space 生成口型同步视频。
- **高可用三方 API 聚合**：
  - 支持配置带有免费赠送额度的云端聚合平台（如 SiliconFlow 硅基流动、Fal.ai、Replicate）。
  - 纳入 `ai-web-free` 现有的 `internal/proxy` 代理检测与容灾熔断体系，单渠道超时或限流自动 Failover 切换。

---

## 4. 角色外观与视觉风格一致性保障体系 (Consistency Matrix)

短剧连贯性的最大瓶颈在于「不同分镜中主角脸崩、衣服样式突变、场景画风割裂」。系统建立混合三层保障机制：

```
┌─────────────────────────────────────────────────────────────────────────┐
│ 层级 1：项目级风格预设锁 (Style Preset Lock)                             │
│ • 国风 3D / 电影胶片 / 现代写实 / 赛博朋克 / 水墨国潮                    │
│ • 自动生成全局负向词与画质增强后缀 (Negative & Quality Prompts)           │
├─────────────────────────────────────────────────────────────────────────┤
│ 层级 2：角色外貌圣经自动注入 (Character Bible Injection)                 │
│ • 为每个角色建立不可变的外貌标签特征库 (Hair, Eyes, Outfit, Accessories)│
│ • 分镜拆解生成提示词时，按说话角色自动拼装于生图 Prompt 核心主体区      │
├─────────────────────────────────────────────────────────────────────────┤
│ 层级 3：Muse 对口型强制锚定定妆立绘 (Anchor Portrait For Lip-Sync)      │
│ • 特写对白镜头直接以定妆正面高清立绘作为基准 Source Image                │
│ • 纯粹由音频驱动嘴部与微表情运动，避免视频重绘模型带来的脸部形变         │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## 5. 前端短剧创作工坊五步流水线设计 (`#/drama`)

在前端构建面向 AI 短剧创作者的一站式可视化工作流视图（`ShortDramaStudio.tsx`）：

```
[1. 剧本与大纲] ──► [2. 角色定妆与音色] ──► [3. 分镜拆解与编辑] ──► [4. 批量云端渲染] ──► [5. 时间轴与导出]
```

### 步骤 1：剧本与大纲拆解 (Script & Outline)
- 提供预设短剧爆款模版库（赘婿逆袭、豪门复仇、末日求生、都市修真、时空穿越）。
- 支持输入一句话梗概或粘贴粗略大纲，调用 `/v1/chat/completions` 自动拆解为「出场角色列表」「场景列表」与「剧情发展节点」。

### 步骤 2：角色定妆与音色匹配 (Character & Voice Casting)
- 自动提取剧本中的主要人物（角色名、性别、年龄、性格、外貌特征描述）。
- **角色立绘生成**：调用 `/v1/images/generations` 生成标准正面高清定妆图（支持点选重新生成、微调描述或本地上传立绘）。
- **音色匹配与试听**：为角色分配 Edge-TTS 音色（如男主->云扬，女主->晓晓，旁白->云希），提供内置的试听播放器（Audition Player），支持针对角色单独设定语速（`speed`）与语调（`pitch`）。

### 步骤 3：结构化分镜拆解 (Storyboard Breakdown)
- 剧本智能拆解为按秒级或台词划分的独立分镜卡片：
  - 字段包括：镜头序号（`shot_id`）、景别（特写 / 中景 / 全景）、运镜指令（推进 / 环绕 / 摇移）、说话角色、对白台词、画面 Prompt（自动注入角色外貌圣经与环境描述）。
- 用户可在卡片墙中自由增删分镜、拖拽调序、微调台词与生图描述。

### 步骤 4：批量云端渲染管线 (Batch Cloud Rendering)
- **并发批量合成配音**：调用 `/v1/audio/speech` 批量生成全部分镜台词音频，获取每句配音的真实物理时长。
- **分镜视觉生成与动态化**：
  - 对白镜头（Talking Head）：调用云端 Muse 将角色定妆图 + 对应台词音频渲染为对口型视频。
  - 场景/动作镜头（Action Scene）：调用云端文生图/图生视频生成动态环境空镜。
- 单镜头卡片提供渲染进度百分比指示，支持单个分镜一键重试与更换素材。

### 步骤 5：多轨时间轴播放器与剪映工程导出 (Timeline & Export)
- **多轨道时间轴交互**：
  - 视频轨（分镜 MP4 片段无缝拼接）。
  - 音频轨（台词配音 MP3 + 可选背景音乐 BGM）。
  - 字幕轨（根据台词与音频时长自动对齐渲染的字幕预览）。
- **剪映 / CapCut 草稿一键导出（核心亮点）**：
  - 前端依照剪映草稿规范组装 `draft_content.json`、轨道配置（Video Tracks、Audio Tracks、Subtitle Tracks）与素材索引。
  - 用户下载草稿文件夹后，可直接粘贴到电脑剪映草稿目录（如 `JianyingPro Drafts/`）中打开，二次添加音效、转场特效与滤镜。
- **全套素材包 ZIP 导出**：
  - 前端利用 `jszip` 纯浏览器打包，输出结构清晰的 ZIP 文件：
    ```
    drama_project_20261114.zip
    ├── script.md              # 完整中英双语分镜剧本
    ├── subtitles.srt          # 自动对齐的时间戳字幕
    ├── draft_content.json     # 剪映/CapCut 导入草稿
    ├── characters/            # 角色高清定妆图
    ├── audio/                 # 各分镜 MP3 台词音频
    └── video/                 # 各分镜 MP4 视频片段
    ```

---

## 6. 数据持久化方案与接口定义

### 6.1 前端 IndexedDB 存储架构 (`ai_web_drama_db`)

- **数据库名称**：`ai_web_drama_db`（版本 1）
- **对象仓库（Object Stores）**：
  - `projects`：存储工程元数据（`id`, `title`, `genre`, `created_at`, `updated_at`, `style_preset`, `status`）。
  - `characters`：存储角色配置（`id`, `project_id`, `name`, `appearance_prompt`, `avatar_blob`, `voice_config`）。
  - `storyboards`：存储分镜清单（`id`, `project_id`, `sequence`, `dialogue`, `speaker_id`, `image_blob`, `audio_blob`, `video_blob`, `duration`, `status`）。

### 6.2 网关服务端可选持久化接口 (`/admin/api/v1/drama/*`)

当配置环境变量 `DRAMA_DATA_DIR`（例如 `DRAMA_DATA_DIR=/data/drama`）时开启，由纯 Go 标准库读写 JSON 文件：

| 方法 | 路径 | 描述 |
| :--- | :--- | :--- |
| `GET` | `/admin/api/v1/drama/projects` | 列出服务端归档的短剧项目摘要 |
| `GET` | `/admin/api/v1/drama/projects/:id` | 获取指定短剧项目完整 JSON 结构 |
| `POST` | `/admin/api/v1/drama/projects` | 上传/备份短剧工程 JSON 至服务端归档目录 |
| `DELETE` | `/admin/api/v1/drama/projects/:id` | 删除服务端归档的项目文件 |

---

## 7. 分阶段演进实施路线 (Roadmap)

1. **Phase 1（免费 TTS 引擎接入与音色试听组件）**：
   - 编写纯 Go Edge-TTS 客户端模块，接入 `/v1/audio/speech`。
   - 映射短剧解说与角色常用中文音色，提供语速/音高微调支持。
   - 在控制台提供独立的轻量音色试听与选择弹窗。
2. **Phase 2（云端视频网关抽象与号池接入）**：
   - 抽象 `/v1/videos/generations` 与 `/v1/videos/:id` 异步任务契约。
   - 接入云端 Muse 系列免密通道与三方聚合平台号池，支持任务排队与容灾回退。
3. **Phase 3（短剧创作工坊上线）**：
   - 前端新增 `ShortDramaStudio.tsx`，建立五步流水线向导与 IndexedDB 存储。
   - 实现角色外貌圣经自动注入、分镜批量并发渲染。
   - 构建多轨道时间轴播放器，落地剪映草稿工程导出与完整素材包 ZIP 打包。

---

## 8. v2.0 无限画布工作台与全平台一键连贯短片架构（Infinite Canvas）

在 Phase 3 原型基础上，系统将 `#/drama` 升级为**双模式协同、全平台能力打通的无限画布短剧创作平台**：

### 8.1 核心设计决策
1. **双模式协同**：默认以基于 `@xyflow/react`（React Flow v12）的无限画布为主工作台，同时保留一键切换至「五步流水线 / 分镜列表」精简视图，两者共享同一工程与持久化数据。
2. **可切换断点的全自动 DAG 流水线**：
   - 支持从一句话梗概「一键生成连贯剧情短片」，自动依次编排「AI 编剧拆解 → 角色定妆与音色绑定 → 场景锚点 → 运镜润色 → 多参考图首帧生成 → 真实 TTS 配音与物理时长测量 → I2V 图生视频 / MuseTalk 对口型 → 时间轴装配」。
   - 支持开关「断点检查点模式」（在角色定妆或分镜首帧完成时自动暂停等待创作者微调，确认后续跑下游），任意单节点支持局部重生成与 A/B 多版本平行分支对比。
3. **跨镜头视觉与声线连贯性保障**：
   - 使用 `/v1/images/generations` 生成角色正面定妆图与场景参考图，并入库 `/admin/api/v1/gallery`。
   - 画廊作品统一记录用途分类（角色设定、场景设定、分镜首帧、参考素材、通用生图）、来源、短剧工程、角色/镜头及可编辑标签；作品集支持按分类、来源、工程筛选，并可按提示词、工程、角色和标签搜索。
   - 短剧画布生成的角色定妆图、场景锚点和分镜首帧登记为 `drama_canvas` 来源，关联工程及角色/镜头信息；作品详情可调整分类与标签，标签变更写回服务端图库。
   - 分镜首帧优先经 `/v1/images/edits` 传入「角色定妆图 + 场景锚点图（及前序镜头衔接参考）」生成高度一致的分镜关键帧；无参考编辑渠道时自动降级为外貌圣经 + 风格锁 Prompt 注入。
   - 视频阶段将分镜首帧与真实配音分别传入 `muse-talk`（对白口型同步）或 `jimeng-video-2.0` / `THUDM/CogVideoX-5b` / `muse-video` / `wan-video-v1`（I2V 图生视频）。
4. **全平台能力节点与跨工坊联动**：
   - 接入提示词广场（`/admin/api/v1/prompts`）、AI 运镜扩写（`/admin/api/v1/prompts/video-expand` + `VIDEO_CAMERA_PRESETS`）、`PromptChatAssistant`、DashScope 自然语言捏音色（`/v1/audio/voices/design`）与声音克隆（`/v1/audio/voices/clone`）、Whisper 语音转台词（`/v1/audio/transcriptions`）。
   - 支持从 `#/chat`、`#/image`、`#/tts`、`#/video` 一键「发送到短剧画布」（`dramaCanvasBridge`）。
5. **能力矩阵预检与诚实降级**：
   - 出片前实时检测各供应商状态（`GET /admin/api/v1/drama/capabilities`），支持「永久免费优先 / 画质优先 / 极速直出」路由偏好。
   - 若请求启用 `no_mock_fallback: true` 且无真实可用视频账号/Key，网关明确返回 `no_video_provider` 错误而非伪装样片，前端自动降级为「真实分镜高清首帧 + Ken Burns 运镜动画 + 真实配音」，保持剧情连贯。
6. **混合双写持久化、同源媒体缓存与纯前端视频合成（`CGO_ENABLED=0`）**：
   - 新增 GORM `DramaProjectRecord` 表与 `/admin/api/v1/drama/*` 接口持久化多工程与画布拓扑 `graph_json`，刷新页面可恢复视频任务轮询。
   - 后端 `StorageEngine` 与 `/v1/videos/files/:id` 提供图片/视频同源代理与本地落盘缓存（支持本地 `/storage/images/...` 参考图直接解码传递与 CORS 安全输出）。
   - 前端成片终端节点支持多轨音画同步连播、纯前端 `Canvas + AudioContext + MediaRecorder` 合成单个完整连续视频文件，以及打包真实 `.mp4` / `.mp3` / `.png` / `.srt` / `draft_content.json` 的 ZIP 下载包。
7. **短剧全套音色池、纯云端密钥即配与 Chat 爬虫资产联动**：
   - `BUILTIN_VOICE_OPTIONS` 与 `GET /v1/audio/voices` 覆盖霸总、大女主、战神、甜宠千金、反派、悬疑旁白、方言喜剧、粤语港风、台偶与出海多语种音色；通过 `#/chat` (`/voice` / `/crawl`) 抓取并缓存/克隆的 `custom_*` 系统音色、角色立绘与视频片段同步出现在角色检视器与全平台导入抽屉中，可直接点击顶栏「配置云端 API Key」激活 `SILICONFLOW_API_KEY` / `DASHSCOPE_API_KEY` / `JIMENG` / `META_COOKIE`（纯云端，无本地 GPU/模型部署依赖）。

---

## 8. 后端 DAG 并发引擎、版本化 Schema 与 MCP 编排架构（已实施）

1. **版本化领域 JSON 唯一事实源（`internal/drama/schema.go`、`patch.go`、`hash.go`）**：
   - 工程以结构化 `DramaProject`（`schema_version: 1`、`revision` 乐观锁、`script` / `analysis` / `characters` / `scenes` / `shots` / `assets` / `nodes` / `timeline`）持久化在 `DramaProjectRecord.ProjectJSON`，画布坐标等 UI 状态独立存放于 `LayoutJSON`，由前端动态派生 React Flow 画布。
   - 读取旧版 `graph_json` 工程时自动无损迁移至 `DramaProject` Schema，保留已生成产物与 `ready` 状态。
2. **输入指纹跳过、连带失效与节点锁定保护**：
   - 每个产物节点记录确定性 `input_hash`；重复触发 Run 时，状态为 `ready`/`degraded` 且指纹一致的节点直接跳过；上游修改（含 RFC 6902 `PATCH /admin/api/v1/drama/projects/:id`）自动将受影响的下游标为 `stale`；`locked: true` 的节点永不被覆盖或重跑。
3. **后端并发 DAG 调度与断点续跑（`internal/drama/engine.go`）**：
   - 支持多阶段拆剧本（`script.analyze` → `script.highlights` → `bible.characters`/`bible.scenes` → `storyboard.shots`，含 JSON 校验与修复重试及 `until` 截断）、按生图账号池并发的 `image.*`、并行 `tts.line:*`、异步视频 `video.clip:*`（支持按 `task_id` 续轮询与 `ken_burns` 降级）及终点装配 `timeline.assemble`（输出 `Tracks`、`SRT` 与剪映草稿）。
   - 每个工程同时只允许一个活跃 Run（重复触发幂等返回）；节点状态与 `task_id` 实时落库，服务重启后自动恢复中断的 Run；通过 `GET /admin/api/v1/drama/projects/:id/runs/:rid/events` 实时 SSE 推送进度。
4. **MCP 自由编排与可选外部 MP4 渲染服务**：
   - 暴露 `POST /admin/api/v1/drama/mcp`（JSON-RPC 2.0，提供 `drama_list_projects`、`drama_get_project`、`drama_patch_project`、`drama_run`、`drama_lock_node` 工具）。
   - 可选配置 `RENDER_SERVICE_URL`（或通过 `docker compose --profile render up -d` 启用外部 FFmpeg 渲染服务）执行 `render.mp4` 节点；未配置时诚实降级并由前端 Canvas+MediaRecorder 合成。
