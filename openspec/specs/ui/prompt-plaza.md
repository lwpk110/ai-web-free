# 图像创作工坊 · 热门提示词灵感广场（Prompt Plaza）设计规范

## 1. 设计目标与工程约束

本规范定义 `ai-web-free` 图像创作工坊（`#/image`）中「热门提示词灵感广场（Prompt Plaza）」的采集管线、AI 策展流程、管理端接口契约与前端交互标准。

- **严格零 CGO 与只读容器兼容**：
  - 全量采集、去重、调度、缓存与 AI 提炼均在纯 Go（`internal/promptplaza/`）内实现，不引入 SQLite 等 CGO 依赖，保证 `CGO_ENABLED=0` 静态二进制与 `distroless/static-debian12:nonroot` 无缝构建运行。
  - **编译期精选种子库兜底**：通过 `go:embed` 内嵌 30+ 条高分中英双语精选提示词模板，确保首次冷启动、未配置外网代理或号池尚未就绪时，提示词广场仍可秒级完整可用。
  - **内存缓存与可选文件落盘**：运行时聚合结果常驻内存；配置环境变量 `PROMPT_PLAZA_FILE` 时按原子替换策略落盘为 JSON 文件以便重启保留，未配置时不依赖可写磁盘目录。
  - **浏览器本地收藏与作品分离**：用户个人「⭐ 收藏」列表持久化至前端本地存储（`localStorage` / `IndexedDB`），与后端共享广场列表解耦。
- **网络与风控边界**：
  - 外部社区数据源抓取优先复用网关代理池（`internal/proxy/`）的健康节点。
  - 调用本机 ChatGPT 号池执行 AI 提炼、联网趋势搜索与灵感扩写时，复用账号池调度与安全边界，严禁将任何账号凭据暴露给广场数据或前端响应。

---

## 2. 多源公共网络采集与本机号池 AI 策展管线

采用「**4 大公共动态数据源 + 本机号池 AI 策展官**」协同管线，保证提示词兼具**真实社区好评热度、高质量预览效果图、自然语言适配度与当前流行趋势**。

### 2.1 四大采集与生成渠道

| 渠道标识 (`source`) | 获取方式与接口 | 提取内容 | 处理策略 |
| :--- | :--- | :--- | :--- |
| `civitai` | **Civitai 公开热榜 API**<br>`GET https://civitai.com/api/v1/images?sort=Most+Reactions&period=Week&nsfw=None&limit=50` | `heartCount` / `likeCount` 真实社区热度、高清预览图 `url`、原始宽高、`meta.prompt` | 严格过滤 `nsfw=None` 与含有效 `meta.prompt` 的高赞条目；识别原始宽高推算推荐画幅；对标签串式 Prompt 交由 AI 策展重写为自然语言 |
| `lexica` | **Lexica 视觉检索引擎 API**<br>`GET https://lexica.art/api/v1/search?q={topic}` | 按摄影写实、3D 微缩、海报排版、产品设计、插画水彩等美学主题返回的高审美预览图与英文描述性 Prompt | 轮换多类主题词抓取并按视觉相关性去重，补齐各风格分类下的高品质预览图与写实长句 Prompt |
| `reddit` / `huggingface` | **Reddit 周热帖 & HuggingFace 数据集**<br>• `r/dalle2` / `r/midjourney` / `r/ChatGPT` 公开 `.json` Feed（`top.json?t=week`）<br>• `https://datasets-server.huggingface.co/rows` 高分开源生图数据集 | 社区高赞热帖标题、配图与评论区/正文 Prompt；开源经过审美评分过滤的专业生图 Prompt | 提取贴文评分（`ups`）与配图；由本机号池 AI 从杂乱贴文/评论中剥离出干净、可直接复用的生图指令 |
| `ai_search` | **本机 ChatGPT 号池原生联网趋势搜索**<br>调用 `internal/chatgpt` 会话链路并注入 `system_hints: ["search"]` | 检索近期社交平台与创作者社区流行的多模态生图玩法（如微缩胶囊、3D 键帽、拍立得撕拉片、材质重塑、字体海报等） | 由号池 AI 直接结合最新趋势生成符合 `gpt-image-2.5-*` / `dall-e-3` 表达习惯的结构化双语模板 |

### 2.2 本机号池 AI 策展与结构化提炼

采集到的候选条目会批量交由本机可用 ChatGPT 号池进行统一策展清洗（若号池暂时不可用则优雅降级为规则清洗与内置种子库兜底）：

1. **自然语言升维**：将传统扩散模型逗号堆砌的标签串（如 `masterpiece, 1girl, ...`）或论坛讨论贴文，改写为适合 `gpt-image-2.5-*` / `dall-e-3` 的高质量自然语言描述（强化主体、构图、材质、光影与文字排版指令）。
2. **中英双语生成**：产出简洁直观的中文标题（`title_zh`）、英文原生生图 Prompt（`prompt_en`）以及语义对齐的中文 Prompt（`prompt_zh`）。
3. **场景与参数映射**：
   - 归一化至 6 大标准风格分类（`category`）：
     - `摄影写实`
     - `3D 与微缩`
     - `海报与文字排版`
     - `插画与二次元`
     - `产品与 UI`
     - `参考图编辑`
   - 自动匹配推荐生图模式（`generations` / `edits`）、推荐模型档位（如 `gpt-image-2.5-sunburst` / `gpt-image-2.5` / `gpt-image-2.5-flare` / `dall-e-3`）与推荐画幅（`1024x1024` / `1792x1024` / `1024x1792`）。
4. **可交互变量槽位提取（`variables`）**：为高频可定制模板提取 `1~3` 个占位符变量（例如 `[主体]`、`[光影材质]`、`[色调风格]`），附带默认填充值与若干快捷可选项（`options`）。

---

## 3. 调度策略、数据结构与 API 契约

### 3.1 调度与环境变量配置

| 环境变量 | 默认值 | 说明 |
| :--- | :--- | :--- |
| `PROMPT_SYNC_INTERVAL_HOURS` | `6` | 后台定时巡检与热榜同步周期（单位：小时）；设置为 `0` 时关闭后台定时任务，仅保留内置种子库与手动触发同步 |
| `PROMPT_PLAZA_FILE` | 空（仅内存） | 可选的广场数据 JSON 持久化文件路径；配置后同步完成自动写入并在启动时优先加载，未配置时不写盘 |

- **去重与排序**：跨源合并时按标准化 Prompt 语义指纹/内容哈希去重；热度评分 `score` 综合社区点赞（`likes` / `hearts`）、时效衰减与来源权重计算，同批次保留各分类均衡覆盖。

### 3.2 核心数据结构 (`PromptItem`)

```json
{
  "id": "civitai-1234567",
  "title_zh": "微缩生态玻璃胶囊",
  "category": "3D 与微缩",
  "prompt_en": "A miniature [主体] inside a crystal glass capsule resting on a mossy wooden desk, soft morning sunlight, macro tilt-shift photography, [光影材质]...",
  "prompt_zh": "放在长满青苔木桌上的水晶玻璃胶囊内的微缩[主体]，柔和晨光，微距移轴摄影，[光影材质]...",
  "preview_url": "https://...",
  "recommended_mode": "generations",
  "recommended_model": "gpt-image-2.5-sunburst",
  "recommended_size": "1024x1024",
  "variables": [
    {
      "key": "主体",
      "label": "核心微缩场景",
      "default_value": "rainy Kyoto ramen stall",
      "options": ["rainy Kyoto ramen stall", "floating bonsai island", "tiny cozy bookshop"]
    },
    {
      "key": "光影材质",
      "label": "质感与氛围",
      "default_value": "warm amber rim lighting and crisp glass refraction",
      "options": ["warm amber rim lighting and crisp glass refraction", "cyberpunk neon mist", "dreamy pastel overcast"]
    }
  ],
  "source": "civitai",
  "stats": {
    "likes": 342,
    "hearts": 128,
    "score": 96.5
  },
  "updated_at": "2026-11-14T10:30:00Z"
}
```

### 3.3 管理端 API 契约（`/admin/api/v1/prompts/*`）

均受全局 `ADMIN_TOKEN` 鉴权中间件约束（与 `/admin/api/v1/*` 一致）。

1. **`GET /admin/api/v1/prompts`**
   - **查询参数**：
     - `category`：风格分类筛选（`all` 或上述 6 大分类之一）
     - `mode`：生图模式筛选（`all` | `generations` | `edits`）
     - `source`：数据源筛选（`all` | `seed` | `civitai` | `lexica` | `reddit` | `huggingface` | `ai_search`）
     - `q`：关键词模糊匹配（检索 `title_zh`、`prompt_en`、`prompt_zh` 与 `category`）
     - `sort`：排序方式（`popular` 按热度评分降序，默认；`latest` 按 `updated_at` 降序）
   - **响应体**：返回 `items: PromptItem[]`、各分类统计计数 `categories`、最近同步时间 `last_synced_at` 及当前同步任务状态 `sync_status`（`idle` / `running` / `last_error`）。

2. **`POST /admin/api/v1/prompts/sync`**
   - **行为**：异步触发一次多源公共爬虫抓取 + 本机号池 AI 提炼与联网趋势搜索任务；若已有任务正在执行中，返回当前运行状态避免并发重复抓取。
   - **响应体**：返回任务接受状态、`sync_status` 与已入队/执行中的渠道摘要。

3. **`POST /admin/api/v1/prompts/expand`**
   - **请求体**：
     ```json
     {
       "idea": "赛博朋克猫咪在霓虹雨夜喝咖啡",
       "mode": "generations",
       "size": "1792x1024"
     }
     ```
   - **行为**：调用本机可用 ChatGPT 号池，结合目标模式与画幅要求，将简短中文想法扩写为大师级中英双语生图 Prompt，并自动抽取 `1~3` 个可交互变量槽位（若号池当前全部不可用，则返回明确错误信息而非伪造响应）。
   - **响应体**：返回结构化的 `title_zh`、`category`、`prompt_en`、`prompt_zh`、`recommended_model`、`variables` 等字段，供左侧参数台直接填入与联动变量替换。

---

## 4. 前端 UI 布局与交互规范（`web/src/views/ImageStudio.tsx`）

严格遵循 `openspec/specs/ui/design-plan.md` 的 Porcelain / Obsidian 精工设计语言与双层 UI 边界，将能力收敛在图像创作工坊 `#/image` 视图内：

### 4.1 右侧点阵画布主区：双 Tab 切换

右侧 `.studio-grid-bg` 主画布顶栏提供清晰的双 Tab 切换（保留左侧参数区上方精简的 Top 热门快捷胶囊用于快速切换）：

- **Tab 1 · `🔥 提示词广场`**：
  - **顶栏工具条**：分类筛选胶囊（`全部`、`⭐ 我的收藏`、`摄影写实`、`3D 与微缩`、`海报与文字排版`、`插画与二次元`、`产品与 UI`、`参考图编辑`）、数据源/排序下拉（`热门度` / `最新`）、关键词搜索框，以及「🔄 立即同步热榜」按钮（展示同步旋转态与最近更新时间戳）。
  - **卡片网格展示**：
    - **视觉预览头**：优先展示高清 `preview_url`；若外部图片因网络/防盗链加载失败或来源无图（如 `ai_search`），优雅降级为按 `category` 定制的精致渐变背景 + 几何/风格 SVG 视觉色卡，避免破图体验。
    - **元数据徽章**：左上角/右上角展示来源标（如 `Civitai` / `Lexica` / `AI 趋势` / `精选`）、真实社区热度数字（👍/❤️）及分类/推荐画幅徽章。
    - **双语内容与收藏**：展示 `title_zh`、支持卡片内「中 / 英」一键切换预览 `prompt_zh` / `prompt_en`，以及右上角本地「⭐ 收藏」切换按钮。
    - **核心动作按钮**：
      1. **「做同款」**（全参数联动）：一键将该卡片的 `recommended_mode`（`文生图` / `参考图编辑`）、`recommended_model`、`recommended_size`（`1:1` / `16:9` / `9:16`）、当前选定语言的 Prompt 以及 `variables` 槽位定义同步写入左侧参数台。
      2. **「立即生成」**（一键直出，适用于文生图条目；若条目推荐为 `edits` 参考图编辑且当前未上传参考图，则自动聚焦引导上传参考图）：联动填入全套参数并直接发起生图请求，同时自动切回 `🖼️ 我的作品库` 展示生成进度与占位卡片。

- **Tab 2 · `🖼️ 我的作品库`**：
  - 保留既有由浏览器 `IndexedDB`（`ai_web_free_studio_db`）驱动的持久化作品画廊、生图池就绪状态徽章、分栏灯箱预览、原图下载与 Remix 复刻能力。

### 4.2 左侧参数台增强（`336px` 独立创作面板）

1. **Prompt 标题栏辅助动作**：
   - 在 `Prompt 提示词` 标签右侧提供 **「✨ AI 扩写」** 与 **「🎲 随机灵感」** 两个轻量操作按钮。
   - 点击 **「✨ AI 扩写」**：以当前输入框内容为 `idea`，带上当前选定的 `mode` 与 `size` 请求 `POST /admin/api/v1/prompts/expand`，回填扩写后的高精细英文/中文 Prompt 并激活对应变量槽位。
   - 点击 **「🎲 随机灵感」**：从已加载的广场高分池（或当前筛选/收藏池）随机抽取一条提示词并执行「做同款」全参数联动。
2. **可交互「变量槽位填空」**：
   - 当应用的提示词（来自广场卡片或 AI 扩写）包含 `variables` 槽位（形如 `[主体]`）时，在快捷胶囊下方、输入框上方自动渲染紧凑的变量填空条。
   - 每个变量槽位提供文本小输入框与推荐值下拉/快选胶囊（`options`）；用户修改任意变量值时，Prompt 文本框中对应位置实时同步替换，用户也可继续手动精修整段文本。
