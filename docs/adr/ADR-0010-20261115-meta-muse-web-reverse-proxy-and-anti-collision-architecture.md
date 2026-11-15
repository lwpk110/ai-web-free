# ADR-0010: Meta Muse Web 逆向工程、多模态视频生成与抗串号架构

| 字段 | 内容 |
| :--- | :--- |
| 状态 | Accepted |
| 日期 | 2026-11-15 |
| 关联技术规范 | [docs/meta-muse-reverse-proxy.md](../meta-muse-reverse-proxy.md) |
| 核心实现 | `internal/provider/meta_muse_provider.go`, `internal/handler/video_handler.go` |

---

## 1. 背景与问题定义 (Context & Problem Statement)

Meta AI Muse (muse.ai) 提供业界顶级的免算力高保真音画一体 AI 视频生成能力。但在将其逆向并接入 `ai-web-free` 统一 OpenAI 兼容网关时，遇到了严峻的技术挑战：
1. **网络层不可逆向性**：基于 Hatch 虚拟机私有加密流（WebSocket `/v1/noise` 双向二进制协议，Ed25519 签名与 notary_token 强公证约束），无法使用传统 HTTP 抓包重放；
2. **凭据强校验与工程约束**：需要维持 13 项包含 HttpOnly 与主机锁定的全量 Cookie，且必须严格遵循项目**零 CGO、纯静态编译、零第三方外部语言（No Python/Node）**的容器部署红线；
3. **出片检测假性超时**：未播放状态下 `<video>` 元素 `duration: NaN` / `readyState: 0`，导致传统检测逻辑死等超时；
4. **吉祥物 Avatar 动作截胡**：页面右下角常驻互动毛绒数字人小部件，发送后第 20 秒产生 7 秒（338KB）动作 Blob，导致网关提前把小人当成片抓走；
5. **多轮对话历史视频残留引发“串号”**：连续生成多个任务时，新任务常误拉取到上一个任务（如小橘猫、古牌坊）的视频。

---

## 2. 关键架构决策 (Decision Drivers & Choices)

### 决策 1：放弃 HTTP 模拟，采用纯 Go CDP (Chrome DevTools Protocol) 自动化链路
- **决策内容**：使用 `chromedp` 直接拉起并驱动真实无头 Chromium 实例，通过 CDP 协议注入 13 项全量 Cookie，维持完整的 WebSocket / Hatch VM 水合环境。
- **理由**：避免对抗官方频繁变动的 Ed25519 加签算法与动态虚拟机公证机制，以极高的稳定度保证会话存活。

### 决策 2：彻底拔除外部 Python 脚本环境，全链路零 CGO 纯 Go 闭环
- **决策内容**：彻底移除了临时解密本地宿主机 Chrome 的 Python 脚本，转为在控制台进行扫码/账密登录时，由纯 Go 通过 `network.GetCookies` 原生捕获全量 HttpOnly Cookie，直接持久化到纯 Go 驱动的 SQLite。
- **理由**：生产镜像必须兼容 `gcr.io/distroless/static-debian12:nonroot`（约 20MB），没有任何 Python、Shell 或外部动态链接库，确保单二进制任意环境直接部署。

### 决策 3：底层 Hook `URL.createObjectURL` 与长效 Context 超时重构
- **决策内容**：
  1. 在页面执行前注入 `URL.createObjectURL` 拦截器，直接捕获上游 WebCodecs 组装产出的音视频 Blob；
  2. 针对 Muse 生成特性，超时时间由 120s 宽限至 360s（6分钟），高频 2 秒一次双通道探测。
- **理由**：绕过未播放状态下 `duration: NaN` 的 DOM 特征陷阱，使成片可在渲染完成的 0.1 秒内秒级捕获。

### 决策 4：抗串号与防截胡“四重绝对防线”
- **决策内容**：
  1. **Prompt 核心关键词语义强绑定**：从本次提示词提取关键词（如“赛博朋克”、“大洋”），AI 的出片消息必须同时包含“做好了”与本次关键词；
  2. **强制开启独立全新会话（New Chat）**：每次出片前强制点击 `a[aria-label="聊天"]`（`/`），物理清空任何历史对话流；
  3. **发送前 DOM 物理打标**：遍历已有 `<video>` 统一标记 `data-muse-old="true"`，并记录发送时间戳 `__muse_send_ts` 与历史 Blob 黑名单；
  4. **体积与 Avatar 过滤**：设定 `> 1.0MB` 体积阈值，严格排除所有 `data-hatch-avatar` 部件，杜绝吉祥物小人截胡。
- **理由**：在多轮连续出片时达到 100% 精准对应，杜绝任何串号与错位。

### 决策 5：原生 HTTP 206 / Range 流媒体中继
- **决策内容**：在浏览器沙箱内无损导出 Base64 并落盘至 `data/videos/{task_id}.mp4`，路由层提供支持 `Range: bytes` 的分片中继接口 `/v1/videos/files/:id`（支持 GET 与 HEAD 预检）。
- **理由**：使用户在 React 前端播放器或第三方播放器中能够即点即播、顺畅拖拽进度条。

---

## 3. 结果与影响 (Consequences)

### 积极收益
- **高可用出片**：在生产环境实测多次文生视频任务（无量玉壁 7.4MB、小橘猫打滚 5.04s、雨夜赛博朋克 10.27MB、狂暴大洋巨浪），成片质量与画面完全精准匹配，平均耗时仅 20~30 秒；
- **架构极简纯粹**：单二进制直接运行，彻底免除 Python、Node 等外部语言环境搭建与部署地狱；
- **OpenAI 协议兼容**：下游客户端无感知接入（标准 `POST /v1/videos/generations` 与 `GET /v1/videos/tasks/:id`）。

### 注意事项与限制
- 宿主机或容器环境需要预装 `google-chrome` 或 `chromium`；若在极简 Distroless 容器中运行，建议采用无状态 Chromium Headless Sidecar。
