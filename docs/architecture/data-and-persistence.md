# 数据与持久化模型

本项目同时使用数据库、媒体对象存储、凭据文件和进程内运行态。部署与恢复时不要把它们当成同一个数据层。

## 数据层次

| 数据 | 主要位置 | 重启后保留 | 说明 |
| --- | --- | --- | --- |
| 账号、系统设置、Provider 渠道、对话、图库元数据、短剧项目/运行等关系数据 | `internal/db/` 的 GORM 模型 | 是，取决于配置的数据源 | SQLite、PostgreSQL、MySQL；表由 `CoreModels()` 和启动迁移管理 |
| 媒体字节 | `internal/storage/` | 取决于后端 | 本地 `StorageEngine`；配置七牛后使用分层本地/远端存储。DB 中通常保存 URL、路径和元数据 |
| 传统凭据/限制/代理输入 | `*.txt` 文件和数据库对齐逻辑 | 是，文件或数据库可用时 | 启动时将文件账号与 DB 账号合并，不是所有文件都为每次运行的唯一事实源 |
| 账号池、代理健康、模型目录缓存、健康巡检状态 | `internal/accounts/`、`internal/proxy/`、`internal/health/` | 否或仅部分重建 | 来自持久化数据/配置重新初始化 |
| 请求审计和运维日志缓冲、登录/批处理任务进度 | `internal/audit/`、`internal/opslog/`、handler/provider worker | 默认否 | 审计可选 JSONL 落盘；异步任务状态需逐项确认 |
| 向量索引 | `internal/vectorstore/` | 取决于实现 | 支持内存和 Qdrant REST 实现；Router 缺省回退内存向量库 |

## 数据库模式

`internal/db/db.go` 使用 GORM 支持纯 Go SQLite、PostgreSQL 和 MySQL。SQLite 默认路径为 `data/ai-web-free.db`，使用 WAL；打开目录失败或数据库初始化失败时，应用可回退到内存 SQLite，因此需要从启动日志和 `/admin/api/v1/system/status` 核实持久化是否实际生效。

远程 DSN 且启用 `DB_MATERIALIZE_ENABLED` 时，启动代码把本地 SQLite 作为业务请求连接的主库，再由 `internal/db/sync_engine.go` 将注册表中的表异步双向同步到云库。同步器有 outbox、变更日志/删除标记、状态和重试退避；这不是每个请求同步写入云端的强一致事务。具体同步范围由 `DefaultSyncTables()` 和注册表决定。

远程 DSN 未启用物化模式时，业务连接直接指向远程数据库。应用也可能在直连远程 PostgreSQL 启动时尝试将本地历史 SQLite 数据迁移到云端。部署前应检查 `DATABASE_URL`、`DB_MATERIALIZE_ENABLED`、本地路径和备份策略。

## 媒体与备份

`StorageEngine` 统一媒体保存、删除和存在性检查。启动时优先创建本地存储；配置有效七牛凭据后使用 tiered 存储；本地目录不可用时可能回退内存存储。内存回退适合降级，不代表数据可持久化。

`backup` / `restore` 子命令由 `main.go` 提供，`internal/storage/` 负责数据库、凭据文件和媒体备份/恢复。七牛冷启动恢复数据库快照是可选能力，要求相应配置和远端对象存在。备份验证至少应确认数据库快照和媒体对象都能恢复，避免只恢复关系记录。

## Agent 变更规则

- 新增 GORM 模型时，检查 `CoreModels()`、迁移和（如启用）同步表注册、同步测试及数据恢复路径。
- 新增数据库记录后确认多数据库方言兼容；不要直接使用特定引擎专属 SQL 而忽略 SQLite 测试。
- 引入 SQLite 驱动时遵循纯 Go / `CGO_ENABLED=0` 构建约束。
- 新增媒体数据时，将字节存储与关系元数据分开设计，并覆盖孤儿对象清理和恢复行为。
- 文档或 UI 显示“已保存”前，确认写入的是可重启恢复的后端，而非仅内存 fallback。
