# ai-web-free 生产部署与两级存储运维指南

本指南详细介绍 `ai-web-free` 在生产环境下的部署架构、单二进制部署实践、Docker 容器化部署、七牛云两级分层缓存机制以及跨机器一键灾备恢复流程。

---

## 一、架构与核心特性

- **纯静态单二进制（严格零 CGO）**：通过 Go 1.16+ 的 `//go:embed` 技术，将 React/TypeScript 全功能控制台（`web/dist`）与系统提示词完全打入单一二进制文件。目标服务器**无需安装 Node.js、npm 或 Nginx**。
- **两级分层缓存架构（Tiered Storage Engine）**：
  - **一级缓存（本地磁盘）**：毫秒级响应，零网络延迟。
  - **二级存储（七牛云 Kodo）**：持久化安全归档，支持断点与版本备份。
  - **Read-Through 自动回填**：本地未命中的图片/音频/视频，在用户请求时自动从云端拉取并持久化至本地磁盘，后续访问直接走本地。
- **零外部数据库依赖**：默认采用纯 Go 实现的 SQLite（`modernc.org/sqlite` / `glebarez/sqlite`），WAL 模式保证高并发读写安全。
- **冷启动自动灾备恢复**：新服务器部署时若本地没有数据库，只需配置七牛云凭据，服务启动时将**自动检测并拉取云端最新的数据库快照**。

---

## 二、缓存与数据存储目录规划

二进制程序默认**以“当前运行的工作目录”为基准**创建并读写数据目录：

```text
/opt/ai-web-free/                    <-- 推荐的服务器部署根目录
├── ai-web-free                      <-- 纯静态二进制程序
├── .env                             <-- 环境变量与凭据配置（权限建议 0600）
├── access_tokens.txt (可选)          <-- 账号 Token 列表
└── data/                            <-- 本地持久化与一级缓存目录（自动生成）
    ├── ai-web-free.db               <-- SQLite 数据库（用户信息、短剧项目、提示词）
    ├── ai-web-free.db-wal           <-- SQLite WAL 预写日志
    ├── storage/                     <-- 多媒体一级缓存
    │   ├── images/                  <-- 图片工坊、分镜关键帧、角色图
    │   ├── audio/                   <-- TTS 配音、台词音频
    │   └── voice_samples/           <-- 预置音色试听样本
    └── videos/                      <-- Muse / 视频工坊合成的短剧 MP4 视频
```

### 环境变量自定义路径（生产最佳实践）
如需将数据与多媒体挂载到独立云盘或外部存储路径，可在 `.env` 中指定：
```env
# 自定义 SQLite 数据库文件绝对路径
DATABASE_URL=/var/lib/ai-web-free/data/ai-web-free.db

# 自定义多媒体存储目录绝对路径
STORAGE_DIR=/var/lib/ai-web-free/data/storage

# 自定义服务监听端口与主机
SERVER_HOST=0.0.0.0
SERVER_PORT=18080
```

---

## 三、部署方式 A：单二进制部署（生产推荐）

### 1. 本地交叉编译
在开发机完成编译（支持从任意操作系统跨平台交叉编译）：

```bash
# 1. 确保前端静态资源已完成编译打包
cd web && npm run build && cd ..

# 2. 编译 Linux 64位无依赖纯静态单二进制 (Ubuntu / Debian / CentOS / Alpine / Distroless 通用)
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o ai-web-free .

# (可选) 若目标服务器为 ARM64 架构 (如 AWS Graviton / 树莓派 / 苹果芯片云主机):
# CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="-s -w" -o ai-web-free-arm64 .

# (可选) 若目标服务器为 Windows:
# CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o ai-web-free.exe .
```

### 2. 服务器部署与权限设置
将编译得到的 `ai-web-free` 和你的 `.env` 配置文件传输至目标服务器：

```bash
# 创建部署目录并赋权
sudo mkdir -p /opt/ai-web-free
sudo chown -R $USER:$USER /opt/ai-web-free
cd /opt/ai-web-free

# 上传文件后赋予可执行权限
chmod +x ai-web-free
chmod 600 .env
```

### 3. 数据恢复（新机器冷启动）
在 `.env` 中配置七牛云凭据后，你可以选择：
- **方案 1（全自动冷启动）**：直接启动 `./ai-web-free`，服务检测到本地无 SQLite 数据库时，会自动从七牛云下载 `_backups/ai-web-free.db`。
- **方案 2（手动拉取）**：运行内嵌恢复命令：
  ```bash
  # 拉取云端 .env 与 SQLite 数据库
  ./ai-web-free restore

  # (可选) 若需要一次性全量下载全部历史多媒体缓存（图片、音频、视频）：
  ./ai-web-free restore --all
  ```

### 4. 配置 Systemd 守护进程
在服务器创建系统服务配置文件 `/etc/systemd/system/ai-web-free.service`：

```ini
[Unit]
Description=ai-web-free Gateway & Studio Service
After=network.target

[Service]
Type=simple
User=root
WorkingDirectory=/opt/ai-web-free
ExecStart=/opt/ai-web-free/ai-web-free
Restart=always
RestartSec=5
LimitNOFILE=65535
Environment=PORT=18080

[Install]
WantedBy=multi-user.target
```

加载并启动服务：
```bash
sudo systemctl daemon-reload
sudo systemctl enable --now ai-web-free

# 查看运行状态与实时日志
sudo systemctl status ai-web-free
sudo journalctl -u ai-web-free -f
```

---

## 四、部署方式 B：Docker / Docker Compose 部署

### 1. Dockerfile 说明
项目根目录下的 `Dockerfile` 采用多阶段纯静态编译：
- **构建阶段**：基于 `node:20` 构建前端，基于 `golang:1.24` 编译纯静态 Go 二进制。
- **运行阶段**：基于安全极简的 `gcr.io/distroless/static-debian12:nonroot` 镜像，无 shell、无漏洞面、镜像体积仅约 30MB。

### 2. 单容器运行
```bash
docker build -t ai-web-free:latest .

docker run -d \
  --name ai-web-free \
  -p 18080:18080 \
  -v $(pwd)/.env:/app/.env:ro \
  -v $(pwd)/data:/app/data \
  --restart unless-stopped \
  ai-web-free:latest
```

### 3. Docker Compose 编排
参考项目根目录的 `docker-compose.yml`：
```yaml
version: '3.8'

services:
  ai-web-free:
    build: .
    container_name: ai-web-free
    ports:
      - "18080:18080"
    volumes:
      - ./.env:/app/.env:ro
      - ./data:/app/data
      - ./access_tokens.txt:/access_tokens.txt:ro
    restart: unless-stopped
```
启动命令：
```bash
docker-compose up -d --build
```

---

## 五、七牛云两级缓存与数据灾备机制

### 1. 云存储配置项（`.env`）
```env
# 七牛云对象存储与两级缓存
QINIU_ACCESS_KEY=your_qiniu_access_key
QINIU_SECRET_KEY=your_qiniu_secret_key
QINIU_BUCKET=ai-web-free-storage
QINIU_DOMAIN=your_bucket_domain.clouddn.com
QINIU_REGION=z0
QINIU_UPLOAD_URL=https://upload.qiniup.com
```

### 2. 读写流转原理
```
[客户端请求图片/音频/视频]
          │
          ▼
┌──────────────────┐  存在   ┌─────────────────────────┐
│ 本地 data/storage│ ───────▶│ 立即读取返回 (零云端延迟) │
└─────────┬────────┘          └─────────────────────────┘
          │ 不存在 (新机器或缓存被清)
          ▼
┌──────────────────┐  下载   ┌─────────────────────────┐
│  七牛云对象存储桶  │ ───────▶│ 写入本地 data/ (自动回填)│ ──▶ 返回客户端
└──────────────────┘          └─────────────────────────┘
```

### 3. 数据备份与恢复命令清单

| 操作目标 | 单二进制直接执行（生产服务器） | Makefile 快捷方式（源码开发机） |
| :--- | :--- | :--- |
| **全量/增量备份** | `./ai-web-free backup` | `make backup` (或 `make sync-push`) |
| **基础恢复 (DB + .env)** | `./ai-web-free restore` | `make restore` (或 `make sync-pull`) |
| **全量多媒体拉取** | `./ai-web-free restore --all` | `go run scripts/sync_qiniu.go pull --all` |
| **查看 CLI 用法** | `./ai-web-free help` | `make help` |

> **备份特性**：
> - 备份 SQLite 前会自动执行 `PRAGMA wal_checkpoint(TRUNCATE)` 确保 WAL 缓存完全刷盘。
> - 多媒体备份为**增量比对**：云端已存在的文件直接跳过（秒级完成），仅上传新生成的文件。

---

## 六、常见运维问题排查

1. **服务启动报 `bind: address already in use`**：
   - 检查端口占用：`lsof -i :18080` 或 `netstat -tlpn | grep 18080`。
   - 可通过 `PORT=19080 ./ai-web-free` 临时切换监听端口。
2. **七牛云回填报域名解析或证书问题**：
   - 检查 `QINIU_DOMAIN` 是否配置正确。若使用的是七牛云赠送的测试域名（有效期 30 天），建议在七牛控制台绑定自建域名或 CDN 域名。
3. **更换数据库路径后数据未显示**：
   - 检查 `DATABASE_URL` 是否拥有文件读写权限；系统在目录不存在时会自动执行 `mkdir -p` 创建父目录。
