# ADR-0018: CI/CD 自动发布、多环境 Profile 代理池与 Clash/Mihomo 多端口网关架构

<!-- grill-me-session: aad89a2f-c316-469e-acb2-c930c217f78e:0 -->
<!-- grill-me-topic-hash: a8c3d9e10018 -->

| 字段 | 内容 |
| :--- | :--- |
| 状态 | Accepted |
| 日期 | 2026-11-21 |
| 来源会话 | `aad89a2f-c316-469e-acb2-c930c217f78e` (step `0`) |
| 触发方式 | `/grill-me` 架构访谈自动归档 |

## 1. 背景与问题定义 (Context & Problem Statement)

- **原始 `/grill-me` 诉求**：现在开始做cicd 自动发布，和开发流程标准化，main 分支禁止直接提交，只能通过pr合并，合并到main 分支后触发ci 流程自动化，将项目部署到vps 服务器，另外有个问题,由于本项目需要翻墙，部署到 aliyun 等vps 需要配置代理服务器，我现在有 clash verge 订阅机场可以使用，给我一个方案，最好使用这个机场，然后充分使用它的节点优势例如通过端口的不同使用他的不同节点，加入本项目代理池，如何做到，由于本项目开发运行在多个运行时环境，需要区分运行时 profile，最好配置在本地.env 中，代理数据放到数据库中，例如代理是这样的，在家里是 windows 环境，本地代理一般是127.0.0.1：10809， 在公司是 192.168.2.12：10809，在vps 需要安装 代理服务器暴露多节点也存储数据库，然后根据本地.env 的profile 值自动切换代理数据

### 初始代码探索与现状基线

1. **现有 CI/Release 流程**：仓库已有 `.github/workflows/ci.yml`（PR 和 `main` push 时运行前端构建、Go 测试与 Python 单测）、`build.yml` 与 `build_docker.yml`（仅在 `v*` Tag 触发），尚无 `main` 合入自动发布 VPS 的 CD 工作流与分支保护脚本。
2. **现有代理池与数据库模型**：`internal/db/models.go` 中的 `ProxyNodeRecord` 仅有全局 `url`、`protocol`、`latency_ms`、`status` 字段，未区分 `profile`（`home` / `office` / `vps`），且 `PoolAccountRecord.Proxy` 是全局单一字段；多环境共用云数据库（或物化同步）时，不同环境的代理地址会互相覆盖。
3. **Clash Verge 机场多节点池化诉求**：阿里云等国内 VPS 无法直连海外 AI 接口，且单端口代理无法满足多账号粘性绑定不同出口 IP 的防风控需求，需结合 Mihomo（Clash Meta 内核）的 `listeners` 特性将机场订阅转换为本地多端口代理池（如 `127.0.0.1:20001..20040`）。

## 2. 决策树访谈记录 (Decision Drivers & Grill-Me Q&A)

#### 决策分支 1（单选）：1. 首先确认 VPS（如阿里云）上的生产部署形态：你倾向于采用哪种方式在 VPS 上运行本项目与代理服务？

候选方案：
- (Recommended) Docker Compose 容器化部署（内置主服务 + 独立 Mihomo 多端口代理网关容器 + 可选无头浏览器/反向代理，环境完全一致）
- Systemd 裸机二进制部署（GitHub Actions 编译纯静态二进制 SCP 上传到 VPS，配合宿主机 systemd 运行）
- 混合模式（主服务跑宿主机二进制方便调用宿主机 Chrome，Mihomo 代理服务跑 Docker 容器）

> **已选决策**：Systemd 裸机二进制部署（GitHub Actions 编译纯静态二进制 SCP 上传到 VPS，配合宿主机 systemd 运行）

#### 决策分支 2（单选）：2. 关于 GitHub 分支保护与 CD 触发时机：PR 合并进 `main` 分支后，你希望采用哪种发布节奏？

候选方案：
- (Recommended) 每次 PR 合并到 main 分支后立即触发全自动编译 + 测试 + SSH/SCP 滚动部署到 VPS（同时可配置自动打 patch 版本号或保留 tag 发布）
- PR 合并到 main 仅运行 CI 并上传预编译产物；只有手动点击 Workflow Dispatch 或推送 v* Tag 时才真正部署到 VPS
- PR 合并到 main 后先自动部署到预发/测试目录，经人工审批（GitHub Environment Approval）后再切换生产 systemd 服务

> **已选决策**：(Recommended) 每次 PR 合并到 main 分支后立即触发全自动编译 + 测试 + SSH/SCP 滚动部署到 VPS（同时可配置自动打 patch 版本号或保留 tag 发布）

#### 决策分支 3（单选）：3. 关于「机场订阅转多端口代理池」方案：Mihomo（Clash Verge 内核）原生支持 `listeners` 配置，可以把订阅里的 N 个节点分别映射到本机连续端口（例如 `127.0.0.1:20001` -> 香港01、`127.0.0.1:20002` -> 日本01...），从而让号池里的每个账号粘性绑定不同出口 IP。你希望如何管理这个订阅转换与入库流程？

候选方案：
- (Recommended) 本项目内置「机场订阅同步与 Mihomo 多端口配置生成器」：配置 `CLASH_SUB_URL` 与起始端口（如 20001~20050）后，一键/定时拉取订阅、过滤无效/信息节点，生成 `listeners` 映射配置并自动将 `127.0.0.1:20001..2000N`（带节点地区/名称标签）同步写入数据库当前 Profile（如 `vps`）的代理池，同时热重载本地 `mihomo.service`
- 提供独立运维脚本（如 Python/Shell 脚本 `scripts/sync_clash_multiport.py`）：在 VPS 上由 systemd timer 定时拉取机场订阅生成 Mihomo 多端口配置，并调用网关 `/admin/api/v1/proxies` 接口写入数据库
- 仅通过 Mihomo External Controller API 动态切换单一端口的节点（不支持多账号同时绑定不同出口 IP，不适合并发号池粘性防风控）

> **已选决策**：(Recommended) 本项目内置「机场订阅同步与 Mihomo 多端口配置生成器」：配置 `CLASH_SUB_URL` 与起始端口（如 20001~20050）后，一键/定时拉取订阅、过滤无效/信息节点，生成 `listeners` 映射配置并自动将 `127.0.0.1:20001..2000N`（带节点地区/名称标签）同步写入数据库当前 Profile（如 `vps`）的代理池，同时热重载本地 `mihomo.service`

#### 决策分支 4（单选）：4. 关于「多环境 Runtime Profile 与数据库代理隔离」：假设家里（`APP_PROFILE=home`，代理 `127.0.0.1:10809`）、公司（`APP_PROFILE=office`，代理 `192.168.2.12:10809`）、阿里云服务器（`APP_PROFILE=vps`，代理 `127.0.0.1:20001..20030`）共用同一个数据库（或通过物化同步共享云端 Neon PG），当不同环境启动时，账号的「粘性代理绑定」应该如何处理？

候选方案：
- (Recommended) 按 Profile 隔离的粘性映射（`proxy_node_records` 增加 `profile` 与 `node_name`/`region` 字段；账号按 `(profile, account_id)` 维度独立保存粘性代理绑定，切换 `.env` 的 `APP_PROFILE` 时自动使用该环境专属粘性代理，且不会污染云库中 VPS 环境的账号代理绑定）
- 全局单字段覆盖（启动或切换 Profile 时直接改写 `pool_account_records.proxy` 字段，最新启动的实例会覆盖云数据库里的代理绑定）
- 仅在 `vps` 生产环境持久化粘性代理绑定，`home`/`office` 开发环境在内存中按当前 Profile 代理池动态分配（不写回云库 `pool_account_records.proxy`）

> **已选决策**：(Recommended) 按 Profile 隔离的粘性映射（`proxy_node_records` 增加 `profile` 与 `node_name`/`region` 字段；账号按 `(profile, account_id)` 维度独立保存粘性代理绑定，切换 `.env` 的 `APP_PROFILE` 时自动使用该环境专属粘性代理，且不会污染云库中 VPS 环境的账号代理绑定）

#### 决策分支 5（单选）：5. 关于「管理台 UI 对多 Profile 代理池与 Clash 订阅转换的可视化支持」：你希望前端管理台（`#/proxies` 或系统设置）提供哪种交互体验？

候选方案：
- (Recommended) 支持在管理台动态切换 + `.env` 默认值（启动时读取 `.env` 的 `APP_PROFILE`，默认 `home`；管理台代理页面顶部增加 Profile 切换器与「机场订阅转多端口」配置面板，可查看/编辑任意 Profile 的代理列表，也可一键热切换当前实例激活的 Profile）
- 严格只读 `.env` 的 `APP_PROFILE`（运行期不可在 UI 切换当前激活 Profile，但管理台可以查看和编辑不同 Profile 下的代理节点数据）
- 完全通过 `.env` 和后端配置文件管理，前端 UI 仅展示当前激活 Profile 的代理节点，不做多 Profile 视图切换

> **已选决策**：(Recommended) 支持在管理台动态切换 + `.env` 默认值（启动时读取 `.env` 的 `APP_PROFILE`，默认 `home`；管理台代理页面顶部增加 Profile 切换器与「机场订阅转多端口」配置面板，可查看/编辑任意 Profile 的代理列表，也可一键热切换当前实例激活的 Profile）

#### 决策分支 6（单选）：6. 关于 VPS 上 Mihomo（Clash 内核）的进程安装与配置热重载方式：当网关拉取你的机场订阅并生成多端口 `config.yaml`（例如 `20001..20050` 端口映射）后，你希望网关如何让 VPS 上的 Mihomo 生效？

候选方案：
- (Recommended) 全自动托管（提供一键初始化脚本 `scripts/setup_vps.sh` 在 VPS 安装 `mihomo` 与 `ai-web-free` systemd 服务；网关内置 Mihomo 配置文件写出与 `systemctl reload/restart mihomo` 或调用 Mihomo RESTful API `PUT /configs` 热重载能力，无需手动登服务器改配置）
- 仅通过 Mihomo External Controller HTTP API（`PUT http://127.0.0.1:9090/configs?force=true`）热加载生成的配置文件，不执行 `systemctl` 命令（避免主服务需要 sudo 权限）
- 仅生成 Mihomo 多端口 YAML 配置文件到指定目录，由宿主机定时任务或管理员手动重启 Mihomo

> **已选决策**：(Recommended) 全自动托管（提供一键初始化脚本 `scripts/setup_vps.sh` 在 VPS 安装 `mihomo` 与 `ai-web-free` systemd 服务；网关内置 Mihomo 配置文件写出与 `systemctl reload/restart mihomo` 或调用 Mihomo RESTful API `PUT /configs` 热重载能力，无需手动登服务器改配置）

#### 决策分支 7（单选）：7. 关于「机场订阅节点过滤与地区优选策略」：机场订阅通常包含「剩余流量/到期时间」等假节点，以及部分对 ChatGPT/Claude 风控较严或不支持的地区（如香港 HK 节点有时不原生解锁 OpenAI）。在把订阅转换为多端口代理池时，你希望采用什么过滤策略？

候选方案：
- (Recommended) 智能筛选 + 地区优先级排序（自动剔除「剩余流量/到期时间/官网」等非代理信息节点；默认优先排除极易触发 OpenAI/Cloudflare 403 或不支持地区的节点如 CN/HK/MO/RU，优先分配 US/JP/SG/TW/KR/EU 节点，并支持在 UI/配置自定义排除/包含正则与端口上限）
- 全量映射 + 自动测活淘汰（除「剩余流量/到期」信息节点外全部映射端口入库，随后自动调用网关 `ProbeAll` 探测 `https://chatgpt.com/cdn-cgi/trace`，把不可达或被拦截的节点标记为冷却）
- 完全不做过滤，按机场订阅原始顺序前 N 个节点直接映射端口入库

> **已选决策**：(Recommended) 智能筛选 + 地区优先级排序（自动剔除「剩余流量/到期时间/官网」等非代理信息节点；默认优先排除极易触发 OpenAI/Cloudflare 403 或不支持地区的节点如 CN/HK/MO/RU，优先分配 US/JP/SG/TW/KR/EU 节点，并支持在 UI/配置自定义排除/包含正则与端口上限）

#### 决策分支 8（单选）：8. 最后确认「GitHub Actions 自动部署到阿里云 VPS 的运维细节与分支保护自动化」：关于 VPS 上的 `.env` 配置管理与发布回滚，你倾向哪种方案？

候选方案：
- (Recommended) 双模式支持（首次部署或管理员手动点击时在 VPS 执行 `setup_vps.sh` 初始化 systemd、Mihomo、无头 Chromium 与 `/opt/ai-web-free/.env`；日常 PR 合并到 `main` 触发 CD 时，仅替换二进制、原子备份旧版、重启 `ai-web-free.service` 并通过 `/admin/api/v1/overview` 健康检查，失败自动回滚上一版二进制；同时允许在 GitHub Secrets 配置 `VPS_ENV_CONTENT` 可选同步 `.env`）
- 每次 CD 部署都由 GitHub Actions 全量覆盖 VPS 上的 `.env` 文件与 systemd 配置（所有配置全部存放在 GitHub Secrets 中）
- GitHub Actions 只负责上传二进制和重启服务，完全不管 VPS 环境初始化与失败回滚

> **已选决策**：每次 CD 部署都由 GitHub Actions 全量覆盖 VPS 上的 `.env` 文件与 systemd 配置（所有配置全部存放在 GitHub Secrets 中）

#### 决策分支 9（单选）：9. 核心决策树（部署形态、PR 合并即部署、GitHub Secrets 全量下发 `.env` 与 systemd、内置 Clash 订阅转 Mihomo 多端口网关、多环境 `APP_PROFILE` 数据库隔离与粘性绑定、智能节点优选）已全部对齐。接下来你希望我们如何推进？

候选方案：
- (Recommended) 方案已完整清晰，现在请输出完整架构设计方案、归档 ADR 并直接开始编码落地！
- 先输出完整架构设计方案与归档 ADR，我确认后再开始写代码实现
- 还有其他补充需求或细节想调整

> **已选决策**：先输出完整架构设计方案与归档 ADR，我确认后再开始写代码实现

## 3. 已收敛的架构决策摘要 (Confirmed Decisions Summary)

- **决策 1**：1. 首先确认 VPS（如阿里云）上的生产部署形态：你倾向于采用哪种方式在 VPS 上运行本项目与代理服务？ → **Systemd 裸机二进制部署（GitHub Actions 编译纯静态二进制 SCP 上传到 VPS，配合宿主机 systemd 运行）**
- **决策 2**：2. 关于 GitHub 分支保护与 CD 触发时机：PR 合并进 `main` 分支后，你希望采用哪种发布节奏？ → **(Recommended) 每次 PR 合并到 main 分支后立即触发全自动编译 + 测试 + SSH/SCP 滚动部署到 VPS（同时可配置自动打 patch 版本号或保留 tag 发布）**
- **决策 3**：3. 关于「机场订阅转多端口代理池」方案：你希望如何管理这个订阅转换与入库流程？ → **(Recommended) 本项目内置「机场订阅同步与 Mihomo 多端口配置生成器」：配置 `CLASH_SUB_URL` 与起始端口（如 20001~20050）后，一键/定时拉取订阅、过滤无效/信息节点，生成 `listeners` 映射配置并自动将 `127.0.0.1:20001..2000N`（带节点地区/名称标签）同步写入数据库当前 Profile（如 `vps`）的代理池，同时热重载本地 `mihomo.service`**
- **决策 4**：4. 关于「多环境 Runtime Profile 与数据库代理隔离」：当不同环境启动时，账号的「粘性代理绑定」应该如何处理？ → **(Recommended) 按 Profile 隔离的粘性映射（`proxy_node_records` 增加 `profile` 与 `node_name`/`region` 字段；账号按 `(profile, account_id)` 维度独立保存粘性代理绑定，切换 `.env` 的 `APP_PROFILE` 时自动使用该环境专属粘性代理，且不会污染云库中 VPS 环境的账号代理绑定）**
- **决策 5**：5. 关于「管理台 UI 对多 Profile 代理池与 Clash 订阅转换的可视化支持」：你希望前端管理台（`#/proxies` 或系统设置）提供哪种交互体验？ → **(Recommended) 支持在管理台动态切换 + `.env` 默认值（启动时读取 `.env` 的 `APP_PROFILE`，默认 `home`；管理台代理页面顶部增加 Profile 切换器与「机场订阅转多端口」配置面板，可查看/编辑任意 Profile 的代理列表，也可一键热切换当前实例激活的 Profile）**
- **决策 6**：6. 关于 VPS 上 Mihomo（Clash 内核）的进程安装与配置热重载方式：你希望网关如何让 VPS 上的 Mihomo 生效？ → **(Recommended) 全自动托管（提供一键初始化脚本 `scripts/setup_vps.sh` 在 VPS 安装 `mihomo` 与 `ai-web-free` systemd 服务；网关内置 Mihomo 配置文件写出与 `systemctl reload/restart mihomo` 或调用 Mihomo RESTful API `PUT /configs` 热重载能力，无需手动登服务器改配置）**
- **决策 7**：7. 关于「机场订阅节点过滤与地区优选策略」：在把订阅转换为多端口代理池时，你希望采用什么过滤策略？ → **(Recommended) 智能筛选 + 地区优先级排序（自动剔除「剩余流量/到期时间/官网」等非代理信息节点；默认优先排除极易触发 OpenAI/Cloudflare 403 或不支持地区的节点如 CN/HK/MO/RU，优先分配 US/JP/SG/TW/KR/EU 节点，并支持在 UI/配置自定义排除/包含正则与端口上限）**
- **决策 8**：8. 最后确认「GitHub Actions 自动部署到阿里云 VPS 的运维细节与分支保护自动化」：关于 VPS 上的 `.env` 配置管理与发布回滚，你倾向哪种方案？ → **每次 CD 部署都由 GitHub Actions 全量覆盖 VPS 上的 `.env` 文件与 systemd 配置（所有配置全部存放在 GitHub Secrets 中）**
- **决策 9**：9. 核心决策树已全部对齐。接下来你希望我们如何推进？ → **先输出完整架构设计方案与归档 ADR，我确认后再开始写代码实现**

## 4. 落地方案与影响范围 (Implementation Plan & Consequences)

详细架构规范见 [`openspec/specs/gateway/cicd-and-profile-proxy-pool.md`](../../openspec/specs/gateway/cicd-and-profile-proxy-pool.md)，核心实施清单如下：

1. **Git 分支保护与 GitHub Actions CD 流水线**
   - 新增 `.github/workflows/deploy.yml`：当 PR 合入 `main`（或手动 `workflow_dispatch`）时，自动完成前端 `npm run build`、Go 单测、`CGO_ENABLED=0` 静态编译 `linux/amd64` 二进制；通过 SSH/SCP 将二进制、`deploy/systemd/ai-web-free.service`、`deploy/systemd/mihomo.service`、`scripts/setup_vps.sh` 上传至 VPS，并用 GitHub Secret `VPS_ENV_CONTENT` 全量覆写 `/opt/ai-web-free/.env`，执行滚动重启与 `/admin/api/v1/auth/status` 健康检查（失败自动回滚 `ai-web-free.bak`）。
   - 新增 `scripts/setup_github_branch_protection.sh`：一键通过 GitHub CLI (`gh api`) 设置 `main` 分支保护（禁止直推、强制 PR 与 `test-and-build` CI 检查通过）。
   - 新增 `scripts/deploy/setup_vps.sh` 与 `deploy/systemd/*.service`：在阿里云 VPS 上自动安装/校验 `mihomo`（Clash Meta 内核）与 `ai-web-free` systemd 服务；支持通过 `--cf-token` 参数或 `.env` 中的 `CF_TUNNEL_TOKEN`（默认 `CF_TUNNEL_PROTOCOL=http2`）自动安装并托管 `cloudflared.service` 域名穿透。

2. **多环境 Runtime Profile 与数据库代理隔离**
   - `internal/config/config.go` & `env.template`：新增 `APP_PROFILE`（默认 `home`）及 Clash 订阅转换相关环境变量（`CLASH_SUB_URL`、`CLASH_MULTIPORT_START=20001`、`CLASH_MULTIPORT_MAX=40` 等）。
   - `internal/db/models.go` & `internal/db/proxies.go` & `internal/db/sync_types.go`：
     - `ProxyNodeRecord` 扩展 `Profile`、`NodeName`、`Region`、`Source`、`Port` 字段，主键按 `sha1(profile + "|" + url)` 计算，支持同一数据库存储 `home`（`127.0.0.1:10809`）、`office`（`192.168.2.12:10809`）、`vps`（`127.0.0.1:20001..2000N`）等多套环境代理。
     - 新增 `AccountProfileProxyRecord`（`account_profile_proxies` 表，纳入物化同步）：按 `(profile, account_key)` 独立持久化账号粘性代理映射，切换 `.env` 的 `APP_PROFILE` 时自动切换该环境的粘性代理且不污染其他环境。

3. **内置 Clash 订阅转 Mihomo 多端口网关（`internal/proxy/clash_sub.go`）**
   - 支持拉取并解析 Clash YAML 与通用 Base64 节点订阅。
   - 自动剔除「剩余流量/到期/官网」等伪节点及 `CN/HK/MO/RU` 等高风控地区，按 `US > JP > SG > TW > KR > EU` 地区优先级排序，生成 Mihomo `listeners` 多端口（`127.0.0.1:20001..2000N`）配置写入 `/etc/mihomo/config.yaml`。
   - 自动调用 Mihomo External Controller `PUT /configs?force=true` 或 `systemctl reload-or-restart mihomo` 热加载，并将端口节点同步写入数据库对应 Profile（`vps`）及内存代理池。

4. **管理台 API 与前端 `#/proxies` 页面升级**
   - 后端新增 `POST /admin/api/v1/proxies/profile`（热切换当前运行 Profile）、`POST /admin/api/v1/proxies/clash-sync`（一键拉取订阅生成多端口并入库），扩展 `GET/POST /admin/api/v1/proxies` 支持按 `profile` 查看与编辑。
   - 前端 `#/proxies` 页面增加顶部 Profile 切换栏（`home` / `office` / `vps` 及「+ 新建 Profile」自定义创建，支持直接配置手动代理或 Clash 机场订阅转多端口字段）、Clash 订阅转多端口配置卡片及节点名称/地区/端口列展示。

5. **本地开发机实测 Clash 多端口方案（已归档）**
   - 本地 `.env` 设置 `MIHOMO_CONFIG_PATH=data/mihomo_test.yaml` 后在 `#/proxies` 触发生成配置；
   - 临时关闭桌面版 Clash Verge 避免 `7890`/`9090` 端口冲突，使用其自带内核（如 `verge-mihomo.exe -d data -f data/mihomo_test.yaml`）启动本地多端口监听，即可在管理台直接并发探活 `127.0.0.1:20001..2000N` 验证上游可达性与延迟（详细步骤见 OpenSpec 第 7 节）。

### 关联原型与规范产物

- `cicd-and-profile-proxy-pool.md`（`d:\workspace\github\ai-web-free\openspec\specs\gateway\cicd-and-profile-proxy-pool.md`）：CI/CD 自动发布、多环境 Profile 代理池与 Clash/Mihomo 多端口网关架构规范
