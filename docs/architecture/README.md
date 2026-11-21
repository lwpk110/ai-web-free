# 技术架构索引

本文档集用于快速理解当前仓库的运行结构。面向 Agent 的推荐入口是本页；面向评审者，可先读系统边界图，再按改动范围查看专题文档。

## 阅读路径

| 需要了解 | 文档 | 主要源码入口 |
| --- | --- | --- |
| 服务由哪些组件构成、依赖如何注入 | [系统边界与模块图](./system-overview.md) | `main.go`、`internal/bootstrap/bootstrap.go`、`internal/handler/router.go` |
| 一个 OpenAI 请求如何经过认证、选号、重试并返回 | [网关请求生命周期](./gateway-request-lifecycle.md) | `internal/handler/router.go`、`internal/handler/failover.go`、`internal/handler/chat_handler.go` |
| 数据存在哪里、哪些状态会丢、SQLite 与云库如何同步 | [数据与持久化模型](./data-and-persistence.md) | `internal/db/`、`internal/storage/`、`internal/accounts/` |
| Web 管理台如何构建进 Go 二进制 | [前端嵌入与发布](./frontend-and-release.md) | `web/`、`web/embed.go`、`Dockerfile`、`.github/workflows/ci.yml` |

## 快速定位

- **启动和依赖装配**：`main.go` → `internal/bootstrap/bootstrap.go` → `internal/handler.RegisterRouterWithDeps`。
- **HTTP 契约**：`internal/handler/router.go` 注册 `/v1/*`、`/admin/api/v1/*`、`/auth/*` 与静态资源路由；处理器实现位于同目录。
- **账号与代理调度**：`internal/accounts/`、`internal/proxy/`；上游协议适配主要位于 `internal/chatgpt/` 与 `internal/provider/`。
- **数据库模型和迁移**：`internal/db/models.go`、`internal/db/db.go`；物化同步实现位于 `internal/db/sync_engine.go`。
- **设计约束**：`openspec/specs/`；历史决策记录：`docs/adr/README.md`。

## 维护约定

1. 本目录说明已落地的代码结构，不替代 API 契约、OpenSpec 或 ADR。发生冲突时，先检查源码和对应规范，并在变更中同步修正文档。
2. 修改路由、状态机、存储语义、启动顺序或前端构建链路时，更新对应专题页中的流程与源码链接。
3. 文档中的图表达责任边界，不承诺所有外部服务均已配置或可用。可选能力需以配置和初始化结果为准。

## 关键架构边界

- 单个 Go 进程承载 API 网关和内嵌管理台；账号池、代理池和不少任务/缓存为进程内运行态。
- Provider 同时包含可作为上游适配器的供应商实现，以及运维/登录/媒体能力；“已注册”不等同于每个 Provider 都参与 `/v1` 文本模型号池调度。
- 数据库后端可为纯 Go SQLite、PostgreSQL 或 MySQL；远程数据库启用本地物化模式时，SQLite 是服务请求使用的本地库，后台同步器负责与云端对齐。
- 媒体对象使用独立 `StorageEngine` 接口；关系元数据和媒体字节并非同一存储层。
- 修改 `web/src/` 后必须先构建前端，使 `web/dist` 与 OpenUI system prompt 更新，再运行 Go 校验。
