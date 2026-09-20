# Store

> **翻译说明：** 本文是[英文原文](../../../contribute/architecture/store.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
> **受众：** 贡献者 · **状态：** 当前有效

## 用途

`internal/infra/db` 为领域仓储契约提供 MySQL/GORM 持久化实现。大多数共享契约位于各领域的 `internal/core/*` 包中，包括 `core/task`、`core/space`、`core/issue` 等。特定能力的端口可以与使用方放在一起，例如 `internal/service/plugin` 中的 Marketplace 目录和激活端口。

当前共享工作的持久化模型以 Space 为范围：

- user / auth_session / external_identity / login_code / user_refresh_token / user_webhook_key / system_grant
- space / space_member / space_invitation / secret
- conversation / conversation_message
- issue
- agent / agent_revision
- workflow / workflow_revision / workflow_run / workflow_node_run / schedule
- task / task_run / workspace_checkpoint / plugin_environment
- artifact（Space 的持久文件；见 data-model.md）
- quota_tier
- llm_model / llm_call / audit_event / plugin / plugin_release / plugin_activation

按项目约定，表名使用单数。`internal/infra/db` 中的 Row 结构体是全部列、索引和
关系的权威来源；关键关系和 schema 理由见 [data-model.md](data-model.md)。

没有 usage 表。`SpaceUsageInWindow` 在读取时聚合：统计通过 `task` 按 Space 关联的 `task_run` 行，汇总其 prompt 和 completion token，再加上同一时间窗口内所创建 Task 上记录的标题生成 token。因此，计量无需维护独立的写入路径。它只解析一次 Space 句柄，此后都使用数字键，所以两部分查询都可以仅通过索引回答，无需读取数据行。

## 关键边界

| 层 | 包 | 职责 |
|-------|---------|------|
| 共享契约/实体 | `internal/core/<domain>` | 共享结构体与跨服务仓储接口，每个领域一个包 |
| 使用方拥有的端口 | `internal/service/*` | 单个编排器使用的窄范围持久化能力 |
| GORM 实现 | `internal/infra/db` | 基于 MySQL、实现这些接口的 Store |
| 对象存储 | `internal/infra/objectstore` | Space home 文件、运行范围的 BUILDMAX_HOME 状态和 Artifact 内容；使用本地文件系统或 S3/MinIO |

## 转换边界

调用方的句柄在此包中转换为行键，这是两种表示唯一相遇的地方。

仓储接口使用公开 ID，因为 `internal/core/*` 领域包只持有这种标识。内部由 `lookupKey` 将其解析为 schema 关联使用的 `bigint`，`publicIDForKey` 进行反向转换；`createWithPublicID` 生成句柄，并在唯一索引拒绝该值时重试——通过错误中指出的索引区分生成值碰撞与邮箱重复。

需要返回句柄的读取通过 join 获得句柄，而不是逐行解析。每个实体都有一个保存 join 集合的 `xxxSelect` 构建器，该实体的所有读取共享它，以免详情读取和列表读取发生偏差；每次 join 都是主键查询。读取结构体以带有 `gorm:"embedded"` 标签的**具名**字段保存行：如果匿名嵌入的结构体有自己的 `TableName`，GORM 会将其视为关联，不扫描其中任何列，产生全零值行且不报错。

加锁读取绝不 join。对 join 执行 `SELECT ... FOR UPDATE` 也会锁定关联行，因此，如果令牌轮换在加锁读取中解析所有者，就会在整个事务期间锁住账户。

理由与各表的决策见 [../../design/entity-identity.md](../../design/实体身份.md)。

## 说明

- 公开句柄是 96 位密码学随机数据，以 20 个字符的小写 base32 文本存储于 `char(20) ascii_bin`。编解码实现是 `internal/util/id.go`。
- 会话 ID 是例外：它们属于内部标识，使用 UUID。
- JSON/API 字段使用 `snake_case`，资源自身句柄的字段名为 `id`。
- `internal/bootstrap/server.go` 打开数据库，并将 Store 注入 handler 和服务。
- 另见：[服务器](server.md)、[配置](config.md)。
