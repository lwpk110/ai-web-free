# AI Web Free

[English README](README_EN.md)

AI Web Free 将 ChatGPT Web 后端能力转换为类 OpenAI API，支持聊天、Responses、文件问答、图片生成、图片变体、语音转文字、文字转语音、模型列表，以及通过 `refresh_token` / `session_token` 获取可用的 ChatGPT `access_token`。

## 接口文档

完整接口、鉴权、token 换取和 curl 示例请查看：[API.md](API.md)

## 功能

- OpenAI 风格的 `/v1/chat/completions`，支持流式和非流式返回，支持 `temperature`/`top_p`/`max_tokens`/`stop`/`reasoning_effort`/`response_format`/`stream_options.include_usage` 等参数。
- 工具调用 (Tool Calling) 模拟 — ChatGPT Web 不原生支持 function calling，AI Web Free 通过 `<tool_call>` 文本协议模拟该能力，支持 `tools`/`tool_choice` 字段，自动注入 system prompt 并解析模型输出中的 `<tool_call>` 块为标准 OpenAI 格式的 `tool_calls`。
- OpenAI 风格的 `/v1/responses`，支持字符串输入、消息数组、`instructions`、流式事件、`previous_response_id` 多轮续接和 Responses 格式 Tool Calling，以及 `reasoning.effort`/`text.query.format`/`temperature` 等参数。
- `/v1/files` 文件上传，上传后可在聊天或 Responses 请求中携带 `file_id` 做文件问答。
- `/v1/images/generations` 图片生成，模型列表包含 `gpt-image-2`，支持 SSE 流式返回，支持 URL 或 `b64_json`。
- `/v1/images/edits` 改图 + `/v1/images/variations` 图生图（变体）。
- `/v1/audio/speech` 语音合成（TTS），兼容常见 OpenAI voice 和输出格式。
- `/v1/audio/transcriptions` 语音转文字，支持 mp3/wav/m4a/ogg/flac/webm 格式。
- `/v1/audio/translations` 音频翻译为英文。
- `/v1/models` 模型列表接口。
- `/auth/refresh`：传入 OpenAI `refresh_token` 获取 `access_token`。
- `/auth/session`：传入 ChatGPT `session_token` 获取新的 `session_token` 和 `access_token`。
- `/backend-api/conversation` 原始 ChatGPT conversation 请求透传。

- 支持 `access_tokens.txt` 账号池、`free_tokens.txt` 免费 UUID 池、`refresh_tokens.txt` OAuth 刷新令牌池、`session_tokens.txt` 会话令牌池、自动生成免费账号、代理池、TLS。
- **完整账号管理体系**：每个账号拥有独立的 TLS Client、代理 IP、浏览器指纹和 WSS 连接，支持三类型账号（TypeNoAuth / TypeFree / TypePUID），六种生命周期状态。
- **Token 去重**：通过解析 JWT payload 中的 `chatgpt_account_id` 去重，同一 ChatGPT 账号多次出现不会重复占用池位置。
- **Capability 能力控制**：10 种按账号类型控制的能力（Chat / Responses / ToolCalling / ImageGenerate / ImageEdit / ImageVariation / TTS / Transcribe / FileUpload / WebSocket），noauth 仅可访问 chat/responses，其余需登录。
- **后台健康检查**：每 10 分钟自动续期已过期的 `session_token` / `refresh_token` 账号。
- **外部 access_token 支持**：`ENABLE_EXTERNAL_TOKEN=true` 时接受外部传入的 ChatGPT access_token，自动创建隔离的临时账号（含独立 TLS Client + UA + 代理 + 浏览器指纹），10 分钟无请求自动释放。
## 部署与本地启动

### 本地启动（源码编译）

**环境要求**：Go `1.26+`；如需修改或重新打包前端管理台，需 Node.js 与 `npm`。

1. **构建前端静态资源（内嵌进 Go 二进制）**

   后端通过 `go:embed` 将 `web/dist` 与 `web/system-prompt.txt` 编译进单一可执行文件；若仓库中尚未生成或修改过 `web/src/`，需先构建前端：

   ```bash
   cd web
   npm ci
   npx tsc --noEmit
   npm run build
   cd ..
   ```

2. **编译纯静态二进制并启动**

   ```bash
   # 可选：复制环境变量模板并按需修改
   cp env.template .env

   # 零 CGO 纯静态编译
   CGO_ENABLED=0 go build -o ai-web-free .

   # 启动服务（默认监听 0.0.0.0:18080）
   ./ai-web-free
   ```

   > 若本机 `18080` 端口已被占用，可通过环境变量指定其他端口启动：
   > ```bash
   > SERVER_PORT=19080 ./ai-web-free
   > ```

3. **访问验证**

   - **Web 管理台与对话操场**：`http://localhost:18080/`
   - **OpenAI API 端点**：`http://localhost:18080/v1/models`、`http://localhost:18080/v1/chat/completions`

4. **前端热更新开发模式（可选）**

   需要调试前端界面时，可先启动后端，再启动 Vite 开发服务器（默认监听 `5173`，并将 `/admin`、`/v1`、`/auth`、`/backend-api` 代理至 `http://localhost:18080`，可在 `web/vite.config.ts` 中调整）：

   ```bash
   cd web && npm run dev
   ```

5. **使用 Makefile 快捷指令（推荐）**

   项目根目录提供了 [Makefile](Makefile)，可直接执行常用开发与构建操作：

   ```bash
   make help                    # 查看全部可用指令
   make dev                     # 本地联调：同时启动 Go 后端(18080)与 Vite 前端热更新(5173)
   make dev-backend             # 仅启动 Go 后端开发服务
   make dev-frontend            # 仅启动 Vite 前端热更新（代理至 18080）
   make build                   # 构建前端静态资源 + 编译零 CGO 纯静态二进制
   make run                     # 编译并启动单二进制服务
   make stop                    # 停止本项目所有本地运行进程
   make test                    # 运行 Go 与 Python 全部测试
   ```

6. **运行测试（可选）**

   ```bash
   go test ./...
   python3 -m unittest discover -s scripts -p "test_*.py" -v
   ```

### Docker 部署

```bash
docker build -t ai-web-free:latest .
docker run -d \
  --name ai-web-free \
  -p 18080:18080 \
  -v $(pwd)/access_tokens.txt:/access_tokens.txt:ro \
  ai-web-free:latest
```

> 准备 `access_tokens.txt` 到当前目录（一行一个 access_token），通过 `-v` 挂载进容器。其他文件同理：`-v $(pwd)/free_tokens.txt:/free_tokens.txt:ro`、`-v $(pwd)/proxies.txt:/proxies.txt:ro`。

### Docker Compose 部署

```bash
# 1. 准备好 access_tokens.txt（每行一个 token）
# 2. docker-compose.yml 已包含 ./access_tokens.txt 的挂载，按需取消注释 free_tokens.txt 或 proxies.txt
docker-compose up -d --build
```

## 配置

默认情况无需额外配置。可以通过 `.env`、系统环境变量或同名部署平台环境变量配置：

```env
# 服务监听
SERVER_HOST=0.0.0.0
SERVER_PORT=18080
PORT=18080

# 鉴权
Authorization=your_authorization

# 免费账号
FREE_ACCOUNTS=true
FREE_ACCOUNTS_NUM=1024

# HTTPS
TLS_CERT=path_to_your_tls_cert
TLS_KEY=path_to_your_tls_key

# 代理
PROXY_URL=your_proxy_url
http_proxy=

# 转发代理（可选，给 backend-api / files 端点单独配置）
API_REVERSE_PROXY=
FILES_REVERSE_PROXY=

# 自定义 BASE_URL（默认 https://chatgpt.com/backend-api）
BASE_URL=

# 流式响应开关（设为 false 时 Chat Completions 强制返回完整响应）
STREAM_MODE=true

# 是否在请求中保留历史上下文（设为 true 启用）
ENABLE_HISTORY=false

# 工具调用模拟
TOOL_CALLING_ENABLED=true
REFUSAL_RETRIES=3
# DEBUG_TOOL_LOG=tool_debug.log

# 外部 access_token（接受外部传入的 ChatGPT access_token）
ENABLE_EXTERNAL_TOKEN=true
```

说明：

- `SERVER_HOST` / `SERVER_PORT`：服务监听地址和端口。`PORT` 为 `SERVER_PORT` 的 fallback。
- `Authorization`：服务访问 key。配置后，请求头需携带 `Authorization: Bearer your_authorization`。
- `FREE_ACCOUNTS`：是否自动生成免费 UUID 账号，默认关闭。
- `FREE_ACCOUNTS_NUM`：自动生成免费 UUID 账号数量，默认 1024。
- `TLS_CERT` / `TLS_KEY`：同时配置时启用 HTTPS。
- `PROXY_URL`：代理池地址。`http_proxy` 为备用代理地址。
- `API_REVERSE_PROXY` / `FILES_REVERSE_PROXY`：分别给 `/backend-api/*` 和 `/files` 端点单独配置转发代理，不配置时走默认代理。
- `BASE_URL`：自定义上游 ChatGPT API 地址，默认 `https://chatgpt.com/backend-api`。
- `STREAM_MODE`：设为 `false` 时强制关闭 Chat Completions 流式返回，默认 `true`。
- `ENABLE_HISTORY`：设为 `true` 时在请求中保留对话历史上下文。
- `TOOL_CALLING_ENABLED`：设为 `false` 时忽略请求中的 `tools` 字段，关闭工具调用模拟。
- `REFUSAL_RETRIES`：模型陷入 "sandbox 拒绝" 循环时的最大重试次数，默认 `3`。
- `ENABLE_EXTERNAL_TOKEN`：设为 `true` 时接受外部传入的 ChatGPT access_token，自动创建隔离的临时账号（含独立 TLS Client、UA、代理、指纹），10 分钟无请求自动释放。默认 `true`。
- `DEBUG_TOOL_LOG`：设为文件路径时，将每次工具解析的详细 trace 写入该文件（调试用）。

本地账号文件：

- `access_tokens.txt`：每行一个 ChatGPT `access_token`，用于需要登录账号的能力。
- `free_tokens.txt`：每行一个 UUID device id，作为免费账号池。
- `refresh_tokens.txt`：每行一个 OpenAI `refresh_token`（支持 `token:team_id` 格式），启动时自动交换为 `access_token`，过期后自动续期。
- `session_tokens.txt`：每行一个 ChatGPT `session_token`（支持 `token:team_id` 格式），启动时自动交换为 `access_token`，过期后自动续期。
- `proxies.txt`：每行一个代理 URL（需要包含端口），与 `PROXY_URL` 一起组成代理池。
## 注意事项

- 图片、TTS、文件能力依赖登录态 access token，免费 UUID 账号不可用。
- `STREAM_MODE=false` 时会强制关闭 Chat Completions 流式返回。
- 本项目是 ChatGPT Web 能力转换服务，接口形状尽量兼容 OpenAI API，但并非 OpenAI 官方服务。

## License

MIT License
