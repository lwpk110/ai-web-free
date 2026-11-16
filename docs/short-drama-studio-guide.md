# 短剧创作工坊（Short Drama Studio）使用与编排教程

短剧创作工坊（前端路由 `#/drama`）是基于 **Go 后端并发 DAG 引擎 + 版本化结构化工程 JSON + React 无限画布** 的一站式 AI 短剧制作系统。只需输入一段剧本梗概，后台即可自动完成多阶段拆本、角色定妆、场景锚点、并发分镜生图、TTS 配音、视频生成与时间线/剪映草稿装配，同时支持外部 Agent 通过 REST 与 MCP 协议自由编排。

---

## 1. 准备工作：能力探测与算力配置

进入 `#/drama` 后，工坊会自动调用 `GET /admin/api/v1/drama/capabilities` 探测当前可用能力，也可以随时点击顶栏「配置云端 API Key」或通过环境变量配置：

| 环节 | 推荐免费 / 云端通道 | 说明 |
| :--- | :--- | :--- |
| **拆剧本（LLM）** | 网关 ChatGPT 号池 | 自动完成节拍拆解、高光挑拣、角色/场景圣经与分镜编写 |
| **生图（定妆/场景/分镜）** | `SILICONFLOW_API_KEY`（`flux-schnell`）、`DASHSCOPE_API_KEY`（`wanx-v1`）、即梦（`jimeng-2.1`）、ChatGPT 付费账号（`gpt-image-1` 多参考图锁定） | 并发上限自动匹配活跃生图账号/Key 数量（上限 8） |
| **配音（TTS）** | 内置纯 Go **Edge-TTS**（免鉴权零消耗）、`LOCAL_TTS_BASE_URL`（Kokoro）、硅基流动、阿里云百炼 | 支持霸总、大女主、战神、甜宠、方言等音色及自定义克隆音色 |
| **视频生成（I2V / 对口型）** | Meta Muse（`muse-talk` 对口型 / `muse-video`）、`SILICONFLOW_API_KEY`（`THUDM/CogVideoX-5b`）、百炼（`wan-video-v1`）、即梦（`jimeng-video-2.0`） | 无可用视频凭据或模型失败时，诚实降级为高清关键帧 **Ken Burns** 运镜 |
| **成片 MP4 渲染（可选）** | `RENDER_SERVICE_URL`（或 `docker compose --profile render up -d`） | 未配置时由 `timeline.assemble` 产出时间线 + SRT + 剪映草稿，并支持浏览器端 Canvas+MediaRecorder 合成 |

---

## 2. 快速上手：从一句话梗概到成片

### 第一步：选择视图与新建工程
- 顶栏支持在 **无限画布（Canvas）** 与 **分步向导（Wizard）** 之间切换，两者实时共享同一份后端工程数据。
- 点击「新建工程」可从内置模板（如都市逆袭、悬疑反转等）初始化，或直接修改剧本源节点（`Script`）。

### 第二步：填写剧本并选择路由策略
在 **剧本源节点** 中配置：
- **剧名 / 题材 / 视觉画风 / 画幅**（竖屏 `9:16`、横屏 `16:9` 或 `1:1`）
- **剧情梗概**：输入故事大纲或台本
- **路由策略**：
  - `free_first`（永久免费优先）
  - `quality_first`（画质优先，优先调用多参考图锁定与高阶视频模型）
  - `fast`（极速直出）

### 第三步：拆解剧本与一键生成
- **仅拆剧本**：点击剧本节点上的「拆解剧本」，后端依次执行 5 个阶段：
  1. `script.analyze`：拆解故事节拍与冲突点、爆点评分；
  2. `script.highlights`：按目标时长挑出高光节拍（支持 `until: "script.highlights"` 停在检查点微调）；
  3. `bible.characters` & `bible.scenes`：并行生成角色外貌圣经（自动绑定音色）与场景锚点；
  4. `storyboard.shots`：逐高光节拍生成紧凑分镜。
  > 每一阶段均带 JSON 结构校验与自动修复重试（最多 3 次）。
- **一键制片**：点击顶栏「一键制片」，后台 DAG 引擎会并行执行所有角色定妆图、场景锚点图，待设定图就绪后**全并发**生成所有分镜首帧与 TTS 配音，随后推进 `video.clip:*` 视频生成与 `timeline.assemble` 时间线装配。
- **关闭页面不中断**：所有任务在 Go 服务端后台运行，进度通过 SSE 实时推送；刷新或关闭重开页面会自动重连当前活跃任务；若服务端重启，未完成的任务会自动恢复并按已记录的 `task_id` 继续轮询。

### 第四步：成片预览与导出
`timeline.assemble` 节点完成后，工程即已成片，可在终端节点（`Composer`）或时间线播放器中：
- 音画同步连播预览（支持调节 BGM 音量、字幕开关）
- 一键导出 **ZIP 工程归档包**（含画面、音频、`.srt` 字幕与剪映 `draft_content.json` 草稿）
- 导出 **完整 MP4 视频**（若配置了 `RENDER_SERVICE_URL` 则由服务端 `render.mp4` 节点渲染；否则在浏览器端直接合成下载）

---

## 3. 节点状态保护、跳过规则与局部重跑

为了解决重复触发全流程浪费额度或覆盖满意产物的问题，引擎内置了 3 层状态保护：

1. **输入指纹跳过（`input_hash`）**
   - 每个节点记录输入指纹。再次点击「一键制片」时，状态为 `ready` / `degraded` 且指纹未变的节点**自动跳过**。
2. **上游变更连带失效（`stale`）**
   - 当你修改某个角色的外貌描述、音色，或修改某个场景环境光影后，该节点以及所有依赖它的下游分镜首帧/视频会自动标记为「待更新（`stale`）」，无关角色与镜头保持 `ready` 不受影响。
3. **产物锁定保护（`locked`）**
   - 对已经满意的角色定妆或分镜首帧调用锁定（`POST .../nodes/:nodeKey/lock`），锁定的节点**永远不会被跳过校验覆盖或重跑**，即使上游发生变化也保持原产物。
4. **节点级故障隔离与补跑**
   - 单个镜头生图或视频失败**不会中断**其他并行镜头；整轮结束会报告为 `partial`（部分完成）。
   - 排查账号/Key 后，直接再次点击「一键制片」或单节点重试按钮，引擎**只会补跑失败的节点**。
5. **连续镜头控制（`continuous`）**
   - 默认分镜首帧只依赖角色定妆与场景锚点以实现最高并发；若相邻两镜需要严格承接上一镜构图，可在分镜数据中将 `continuous` 设为 `true`，引擎会自动将其串接至前一镜首帧之后。

---

## 4. 进阶：结构化工程 JSON 与 Agent / MCP 自由编排

每个短剧工程在数据库中都以结构化领域 JSON（`DramaProject`，`schema_version: 1`）作为唯一事实源，画布坐标独立保存在 `layout` 中，不干扰业务数据。

### 核心节点键（`nodeKey`）命名约定
- `script.analyze` / `script.highlights` / `bible.characters` / `bible.scenes` / `storyboard.shots`
- `image.portrait:<char_id>`（角色定妆）
- `image.scene:<scene_id>`（场景锚点）
- `image.keyframe:<shot_id>`（分镜首帧）
- `tts.line:<shot_id>`（分镜配音）
- `video.clip:<shot_id>`（分镜视频/对口型）
- `timeline.assemble`（时间线、SRT 与剪映草稿装配）
- `render.mp4`（可选外部 FFmpeg MP4 渲染）

### 方式一：REST + RFC 6902 JSON Patch 编排

```bash
# 1. 读取结构化工程（含当前 revision）
curl -s http://localhost:18080/admin/api/v1/drama/projects/drama_default_main \
  -H "Authorization: Bearer $ADMIN_TOKEN"

# 2. 使用 JSON Patch 修改第 1 镜动作并触发连带 stale 传播（带乐观锁 If-Match）
curl -X PATCH http://localhost:18080/admin/api/v1/drama/projects/drama_default_main \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "If-Match: 3" \
  -H "Content-Type: application/json" \
  -d '[{"op":"replace","path":"/shots/0/action","value":"缓缓摘下墨镜，眼神锐利地看向镜头"}]'

# 3. 仅触发受影响或指定节点运行（例如只重跑第 1 镜首帧与视频，自动跳过其他 ready 镜头）
curl -X POST http://localhost:18080/admin/api/v1/drama/projects/drama_default_main/runs \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"targets":["image.keyframe:shot-1","video.clip:shot-1","timeline.assemble"]}'

# 4. 订阅 SSE 实时进度
curl -N http://localhost:18080/admin/api/v1/drama/projects/drama_default_main/runs/<run_id>/events \
  -H "Authorization: Bearer $ADMIN_TOKEN"
```

### 方式二：标准 MCP（JSON-RPC 2.0）接入 Agent

后端暴露了标准 MCP 端点 `POST /admin/api/v1/drama/mcp`，支持任意兼容 MCP 的 Agent（如 Claude、Cursor、自研 Agent）直接编排短剧：

| 工具名 | 用途 |
| :--- | :--- |
| `drama_list_projects` | 列出所有短剧工程摘要（ID、标题、分镜数、revision） |
| `drama_get_project` | 读取完整 `DramaProject` 结构化 JSON |
| `drama_patch_project` | 以 RFC 6902 JSON Patch 增删镜头、修改台词或提示词 |
| `drama_run` | 启动后台 DAG 流水线（支持 `targets`、`force`、`until`） |
| `drama_lock_node` | 锁定或解锁指定产物节点 |

**MCP 调用示例（列出工具与触发局部编排）：**

```bash
# 查看可用 MCP 工具
curl -X POST http://localhost:18080/admin/api/v1/drama/mcp \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'

# 调用 drama_run 只运行到高光节拍检查点
curl -X POST http://localhost:18080/admin/api/v1/drama/mcp \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "jsonrpc":"2.0",
    "id":2,
    "method":"tools/call",
    "params":{
      "name":"drama_run",
      "arguments":{
        "project_id":"drama_default_main",
        "targets":["script.*","bible.*","storyboard.*"],
        "until":"script.highlights"
      }
    }
  }'
```

---

## 5. 常见问题排查（FAQ）

- **Q：为什么视频节点显示 `Ken Burns` 而不是真实视频？**
  - A：当 `meta_muse`、`siliconflow`、`dashscope`、`jimeng` 均无可用凭据，或上游视频任务失败/超时时，网关不会返回假样片，而是诚实降级为高清关键帧 + Ken Burns 运镜动画，保证全片依然可以完整连播与导出。
- **Q：提示 `revision conflict`（409）怎么办？**
  - A：说明在读取工程后，后台 Run 或其他客户端已经更新了工程 `revision`。重新 `GET /projects/:id` 获取最新 `revision` 后再提交 `PATCH` 即可。
- **Q：旧版本保存的工程还能用吗？**
  - A：可以。首次通过接口或页面打开旧 `graph_json` 工程时，后端会自动无损迁移为 `DramaProject` Schema，并保留所有已生成的图片、音频和视频产物，再次触发运行会直接跳过已完成节点。
