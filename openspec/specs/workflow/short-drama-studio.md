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

- **协议实现**：纯 Go 实现微软 Edge 浏览器大声朗读 WebSocket 协议（`wss://speech.platform.bing.com/consumer/speech/synthesize/readaheadwork/v1`），支持直接流式接收或缓冲为完整音频响应。
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
