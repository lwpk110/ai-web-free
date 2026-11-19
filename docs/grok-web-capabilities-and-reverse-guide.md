# xAI Grok Web 免费生态全能力矩阵与逆向实战指南

本指南系统性梳理并归档 xAI Grok Web Free（网页免费版）的底层协议、凭据机制、多模态全功能矩阵与在 `ai-web-free` 网关中的集成调用方案，以便后续开发、扩展与号池运维查阅。

---

## 1. 核心能力矩阵与特色全景

| 能力维度 | 底层模型 / 引擎 | 特性描述 | 核心业务场景 | 相比其他供应商优势 |
| :--- | :--- | :--- | :--- | :--- |
| **实时全网与推特检索** | `grok-2`, `grok-3` | 穿透检索 X (Twitter) 全网推文与实时舆情，自带无缝 Web 搜索 | 突发新闻追踪、社交舆论监测、实时热搜、金融行情 | **全网独家** 直连 Twitter 实时流，信息时效达分钟级 |
| **DeepSearch 深度研报** | `grok-3-deepsearch` | 自主拆解多步子查询，翻阅数十篇网页与推文并综合聚合研报 | 行业行研、学术文献调研、竞品深度对比分析 | 自动化多轮深入调研与信源交叉验证（对标 OpenAI Deep Research） |
| **Think 深度长链推理** | `grok-3-thinking` | 独立思考链（Chain-of-Thought），逐步推导演算 | 复杂高难算法、奥数证明、系统架构论证、复杂代码排错 | 思考过程可视，逻辑推导严谨（对标 OpenAI o1 / o3-mini） |
| **Aurora 高清生图** | Aurora / FLUX.1 | 一次性生成多张高分辨率真实或艺术插画，支持风格迁移 | 商业插图设计、自媒体配图、写实摄影、短剧分镜生成 | 提示词理解深刻，写实细节丰富，审核尺度更为宽容 |
| **Vision 多模态视觉** | Grok Vision 引擎 | 支持传入图像/截图，进行像素级图表与结构解析 | 设计稿/截图还原前端代码、财报数据图表抽取、高精度 OCR | 免费账号即开放高精度视觉能力 |
| **长文档与代码评审** | Grok Context QA | 支持长文本、代码仓库片段与 PDF/TXT 文档挂载问答 | 代码架构审计、长篇合同分析、技术文档问答 | 长上下文保留度高，提炼精准 |

---

## 2. Grok Web 底层逆向协议与 Payload 规范

Grok Web 的官方会话端点为 `POST https://grok.com/rest/app-chat/conversations/new`，采用流式 SSE（Server-Sent Events）向下游输出响应。

### 2.1 典型请求头（Headers）
```http
POST /rest/app-chat/conversations/new HTTP/1.1
Host: grok.com
User-Agent: Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/134.0.0.0 Safari/537.36
Content-Type: application/json
Accept: text/event-stream
Origin: https://grok.com
Referer: https://grok.com/
Cookie: sso=<JWT_TOKEN>; sso-rw=<JWT_TOKEN>
```

### 2.2 请求体参数矩阵（Payload Fields）
```json
{
  "temporary": true,
  "modelName": "grok-3",
  "message": "用户提示词内容",
  "fileAttachments": [],
  "imageAttachments": [],
  "disableSearch": false,
  "enableImageGeneration": true,
  "returnImageBytes": false,
  "returnBuffers": true,
  "deepsearchPreset": "default",
  "isPreset": false
}
```

#### 关键参数控制逻辑：
- `temporary`: 设为 `true` 表示隐身无痕会话，不污染账号历史记录列表，避免触发账号侧会话堆积配额。
- `disableSearch`: 默认应设为 `false`，模型遇到时效性或实时事实时会自动触发 X / 全网搜索引用。
- `enableImageGeneration`: 设为 `true` 时，若提示词包含生图意图，模型将自动调用生图管道输出生图元数据与图片 CDN 链接。
- `modelName`: 支持传入 `grok-2`、`grok-2-mini`、`grok-3`、`grok-3-mini`、`grok-3-deepsearch`、`grok-3-thinking`。

---

## 3. 凭据体系机制与卡商卡密奥秘

### 3.1 凭据本质剖析
市面上批量采购的 `grok-free.txt` 卡密标准格式通常为：
```text
邮箱|密码|eyJ0eXAiOiJKV1QiLCJhbGciOiJIUzI1NiJ9.eyJzZXNzaW9uX2lkIjoi...
```
- **核心真相**：第三列以 `eyJ...` 开头的 JWT 字符串（解密 payload 包含 `{"session_id":"..."}`），**在 Grok 官方底层正是 `sso` 与 `sso-rw` Cookie 的真实值（Value）**。
- **直接组装规则**：
  只要将卡密第三列提取并组装为：
  `Cookie: sso=<JWT>; sso-rw=<JWT>`
  发往 `https://grok.com/rest/app-chat/conversations?limit=1`，Grok 官方服务器即返回 `HTTP 200 OK`，无需经过账号密码重新登录流程。

### 3.2 测活与健康状态定义
- **健康探测端点**：`GET https://grok.com/rest/app-chat/conversations?limit=1`
- **返回 HTTP 200**：凭据活跃，状态置为 `Active`，可分发对话或生图。
- **返回 HTTP 401 / 403**：Session 已被官方主动注销或过期，状态置为 `Expired`，需通过卡密重登或更换新 Cookie。
- **返回 HTTP 429**：触发单号短期频控（如 2 小时窗口限制），状态置为 `RateLimited`，自动进入退避冷却队列。

---

## 4. 网关（ai-web-free）调用与多协议转换方案

在 `ai-web-free` 网关中，已将 Grok 号池标准化映射至 OpenAI 兼容协议：

### 4.1 聊天与推理对话 (`POST /v1/chat/completions`)
- 请求模型指定为以 `grok` 开头的模型标识（如 `grok-3`、`grok-2`、`grok-3-deepsearch`）。
- 网关通过 `GrokProvider` 自动从健康池中轮询活跃账号，并将 OpenAI Messages 数组拼接后发往 Grok Web 会话端点。
- 支持流式增量解析（SSE Chunk 输出）与单次完整文本聚合输出。

### 4.2 图像生成与工坊绘制 (`POST /v1/images/generations`)
- 指定模型为 `grok-image` 或 `grok`；
- 网关向 Grok 会话发送生图指令，捕获响应流中生成的图片链接并提取持久化至图像工坊画廊。

---

## 5. 常见风控机制与避坑实践

1. **Cloudflare Turnstile 网页端风控**：
   - `accounts.x.ai/sign-in` 登录入口部署有人机验证盾。服务端纯无头浏览器（Headless Chrome）在云厂商机房 IP 下会被 100% 拦截并触发批量封禁。
   - **工程最佳实践**：严禁在服务端开启无头自动重登；凭据补给应优先采用直接导入 SSO Cookie 或使用本地有头（Headed）模式登录提取。
2. **多账号粘性代理绑定**：
   - 维持单账号与固定出口代理（Proxy）的粘性绑定，避免同一账号在短时间内因 IP 频繁切换跨国节点引发风控挑战。
3. **滚动淘汰用完即弃（以注册代续期）**：
   - 免费号具有生命周期短、注册成本低的特点。当号池出现 401 失效时，推荐直接剔除并通过自动化注册批量补位，保持热池规模稳定。
