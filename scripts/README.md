# `scripts/` 目录指南

本目录存放 `ai-web-free` 的开发联调、VPS 部署与内网穿透、数据同步与号池运维、外置自动注册机以及 Python 单元测试脚本。所有脚本按职责分目录维护，避免根目录堆积临时调试代码。

## 目录结构

```text
scripts/
├── README.md                                  # 本说明文档
├── dev/                                       # 本地开发与进程管理
│   ├── dev.sh / dev.ps1                       # 一键启动 Go 后端 + Vite 前端热更新
│   └── stop.sh / stop.ps1                     # 清理本项目占用的端口与残留进程
├── deploy/                                    # 生产部署、仓库治理与内网穿透
│   ├── setup_vps.sh                           # Linux/阿里云 VPS 幂等初始化（Mihomo + systemd）
│   ├── setup_github_branch_protection.sh      # 使用 gh CLI 配置 main 分支 PR + CI 保护规则
│   └── cf-tunnel.sh / cf-tunnel.ps1 / .bat    # Cloudflare Tunnel 一键公网暴露
├── ops/                                       # 数据同步、音色预热与号池辅助工具
│   ├── sync_qiniu.go                          # 七牛云全量/增量备份与跨机恢复
│   ├── pull_local_tts.sh / pull_local_tts.ps1 # 启动本地 Kokoro TTS 并预拉取音色与试听样音
│   └── kami_login.py                          # ChatGPT 卡密自动登录、Token 提取与 TOTP 工具
├── registrar/                                 # 外置自动注册机（独立 Go 模块与测试）
│   ├── cmd/main.go                            # CLI 入口（支持 Grok / ChatGPT 注册推入号池）
│   ├── registrar.go                           # 注册流程与网关导入封装
│   ├── mail_provider.go                       # 临时邮箱适配器（Mail.tm / Mock）
│   ├── captcha_solver.go                      # 验证码打码适配器（CapSolver / Mock）
│   └── registrar_test.go                      # 单元测试（随 `go test ./...` 执行）
└── tests/                                     # Python 脚本单元测试（随 CI 与 `make test-py` 执行）
    ├── __init__.py
    ├── test_kami_login.py                     # `ops/kami_login.py` 单元测试
    └── test_grill_me_adr.py                   # `.agents/hooks/grill_me_adr.py` 单元测试
```

---

## 1. `dev/` — 本地开发与联调

日常开发推荐优先通过根目录 `Makefile` 调用，脚本内部会自动定位仓库根目录并处理进程清理。

| 脚本 | 适用平台 | 对应 `Makefile` 目标 | 说明 |
| :--- | :--- | :--- | :--- |
| `dev/dev.sh` | Linux / macOS | `make dev` | 先清理旧进程，再拉起 Go 后端（默认 `18080`）并在就绪后启动 Vite 前端（默认 `5173`） |
| `dev/dev.ps1` | Windows (PowerShell) | `make dev` | Windows 版前后端联合启动脚本，退出时自动回收后端进程 |
| `dev/stop.sh` | Linux / macOS | `make stop` | 按端口（`PORT`、`WEB_PORT`、`5174`）及进程名清理残留服务 |
| `dev/stop.ps1` | Windows (PowerShell) | `make stop` | Windows 版端口与进程清理脚本 |

**直接运行示例：**

```bash
# Linux / macOS
PORT=18080 WEB_PORT=5173 bash scripts/dev/dev.sh
PORT=18080 WEB_PORT=5173 bash scripts/dev/stop.sh

# Windows PowerShell
powershell -ExecutionPolicy Bypass -File scripts/dev/dev.ps1 -Port 18080 -WebPort 5173
powershell -ExecutionPolicy Bypass -File scripts/dev/stop.ps1 -Port 18080 -WebPort 5173
```

---

## 2. `deploy/` — 部署、CI 治理与内网穿透

### `deploy/setup_vps.sh`
- **用途**：Linux / 阿里云 VPS 一键幂等初始化脚本，由 `.github/workflows/deploy.yml` 在自动部署阶段调用，也可手动执行。
- **职责**：
  1. 创建 `/opt/ai-web-free/data/storage` 与 `/etc/mihomo` 目录；
  2. 自动检测架构（`amd64` / `arm64`）安装 Mihomo（Clash Meta 内核）；
  3. 生成仅监听 `127.0.0.1` 的初始 `/etc/mihomo/config.yaml`；
  4. 安装并启用 `mihomo.service` 与 `ai-web-free.service`。
- **常用环境变量**：`APP_DIR`（默认 `/opt/ai-web-free`）、`MIHOMO_DIR`、`MIHOMO_VERSION`。

```bash
sudo APP_DIR=/opt/ai-web-free bash scripts/deploy/setup_vps.sh
```

### `deploy/setup_github_branch_protection.sh`
- **用途**：通过 GitHub CLI (`gh`) 为指定分支（默认 `main`）启用强制 PR 与 `test-and-build` CI 状态检查，禁止直接 `push` 到主分支。
- **前置条件**：已安装 `gh` 并执行 `gh auth login`，且具有仓库管理员权限。

```bash
bash scripts/deploy/setup_github_branch_protection.sh main
```

### `deploy/cf-tunnel.{sh,ps1,bat}`
- **用途**：一键编译/启动 `ai-web-free` 并通过 Cloudflare Tunnel（支持临时 Quick Tunnel 或固定 `--tunnel-token` 域名模式）安全暴露至公网，默认会自动生成 `Authorization` 与 `ADMIN_TOKEN` 防止裸奔。
- **前置条件**：本地已安装 `cloudflared`（或由脚本引导）。

```bash
# Linux / macOS（查看完整参数）
./scripts/deploy/cf-tunnel.sh --help
./scripts/deploy/cf-tunnel.sh --port 18080

# Windows PowerShell / 双击 bat
powershell -ExecutionPolicy Bypass -File scripts/deploy/cf-tunnel.ps1 -Port 18080
scripts\deploy\cf-tunnel.bat
```

---

## 3. `ops/` — 数据同步、资源拉取与号池辅助

### `ops/sync_qiniu.go`
- **用途**：将本地 SQLite 数据库、`.env` 与 `data/storage`、`data/videos` 同步备份至七牛云，或在新机器一键拉取恢复。
- **前置条件**：`.env` 中已配置 `QINIU_ACCESS_KEY`、`QINIU_SECRET_KEY`、`QINIU_BUCKET` 等变量。
- **对应 `Makefile` 目标**：`make sync-push` / `make backup`、`make sync-pull` / `make restore`。

```bash
go run scripts/ops/sync_qiniu.go push
go run scripts/ops/sync_qiniu.go pull
go run scripts/ops/sync_qiniu.go pull --all   # 同时拉取全部历史多媒体文件
```

### `ops/pull_local_tts.{sh,ps1}`
- **用途**：可选拉起本地 Kokoro-FastAPI TTS 容器（`docker compose --profile local-tts`），并调用网关 `/v1/audio/voices/pull-local` 预拉取常用音色列表与试听样音到本地缓存。

```bash
# Linux / macOS
START_DOCKER=1 GATEWAY_URL=http://127.0.0.1:18080 LIMIT=24 bash scripts/ops/pull_local_tts.sh

# Windows PowerShell
powershell -ExecutionPolicy Bypass -File scripts/ops/pull_local_tts.ps1 -StartDocker -Limit 24
```

### `ops/kami_login.py`
- **用途**：ChatGPT 卡密账号（`邮箱----密码----2FA密钥`）辅助工具，支持独立 CLI 运行：
  - Playwright 浏览器自动登录并提取 `accessToken` / `session_token`；
  - 生成 `Sub2API` JSON 或直接推送到 `ai-web-free` 管理接口；
  - 计算实时 6 位 2FA TOTP 动态码（`--mode totp`）与卡密文本清洗（`--mode clean`）。

```bash
python3 scripts/ops/kami_login.py --help
python3 scripts/ops/kami_login.py --mode totp --file kami_accounts.txt
```

---

## 4. `registrar/` — 独立外置自动注册机

基于 ADR-0015 设计的独立外置注册机原型（Go package：`ai-web-free/scripts/registrar`），面向家宽/住宅代理环境运行，注册完成后通过 `POST /admin/api/v1/accounts/import` 推入主网关号池。

```bash
# 模拟演练（使用本地 Mock 邮箱与打码，不向网关提交）
go run ./scripts/registrar/cmd --provider grok --count 1 --mock-mail --dry-run

# 运行单元测试
go test ./scripts/registrar/...
```

---

## 5. `tests/` — Python 自动化测试

包含仓库内 Python 工具与钩子脚本的单元测试，统一通过 `unittest discover` 执行（已集成到 `.github/workflows/ci.yml`、`.github/workflows/deploy.yml` 与 `make test-py`）：

```bash
python3 -m unittest discover -s scripts -p "test_*.py" -v
```

---

## 维护规范

1. **按职责归类**：新增脚本请放入 `dev/`、`deploy/`、`ops/`、`registrar/` 或 `tests/`，不要直接堆放在 `scripts/` 根目录。
2. **禁止提交一次性调试脚本**：临时验证脚本（如 `e2e_*_demo.go`、`test_*.go`）请勿提交到仓库；`*.py` 默认被 `.gitignore` 忽略，若需纳入版本控制请在 `.gitignore` 中显式放行并补充单测。
3. **路径解析稳健**：脚本位于二级子目录（`scripts/<category>/`），定位仓库根目录时应向上跳转两级（`../..`）。
4. **同步文档**：新增、重命名或移除脚本后，请同步更新本 `scripts/README.md` 以及关联的 `Makefile` / CI Workflow。
