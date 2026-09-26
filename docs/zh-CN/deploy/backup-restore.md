# 备份与恢复部署

> **翻译说明：** 本文是[英文原文](../../deploy/backup-restore.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
> **受众：** 运维人员 · **状态：** 当前——已在 kind 上演练，尚未在 Beta 候选版本上验证
>
> 如何为 BuildMax 服务器部署做出可恢复的备份，如何将其恢复到恢复环境，并证明恢复完整。

BuildMax 没有导出或导入命令。数据库和对象存储桶由你用自己的工具备份；本页说明备份必须包含什么、按什么顺序复制，以及如何检查结果。`./make kind drill restore` 会端到端演练这一流程（见[在 kind 上演练](#在-kind-上演练)）；首个 Beta 候选版本仍需针对自己的依赖执行一遍（见 [beta-readiness.md](beta-readiness.md#恢复与维护)）。

## 需要备份什么

| 项目 | 位置 | 原因 |
|---|---|---|
| 数据库 schema | `server.yaml` 中的 `database.name`：服务器迁移的所有表 | 全部记录：账户、Space、Task、TaskRun、Artifact、审计、用量、加密封存的 Secret 与模型凭证 |
| 存储桶前缀 | `storage.minio.bucket` 下的 `storage.minio.prefix`（默认 `workspaces`） | Artifact 内容、Run 轨迹与会话包、工作区 checkpoint 载荷、插件包，以及 Space 文件 |
| KEK 文件 | `secret.kek_file`，从 `buildmax-kek` Secret 挂载 | 解封 Space Secret 和托管模型凭证 |
| `server.yaml` | `buildmax-config` ConfigMap | 指明 schema、存储桶、前缀、保留窗口以及恢复后服务器必须一致的其他所有设置 |

其中两项需要特别注意：

- **Space 文件只存在于存储桶中。** 没有数据库行指向它们，因此它们的恢复点是存储桶复制读取它们的时刻，而不是数据库快照时刻，`storage verify` 也无法检查它们。
- **KEK 备份要与数据库转储分开保存。** 单独任何一个都无害；放在一起就能解封所有已存储的凭证。没有配套 KEK 恢复的转储可以启动，但其中的 Secret 和模型凭证再也无法解密。见[密钥加密密钥](../../../deployment/production/README.md#key-encryption-key)。

其他内容无需备份。Redis 只保存实时协调状态（流、连接事件、轮次租约），启动时为空。JWT 密钥和 worker API TLS 证书在恢复环境中重新生成：新的 JWT 密钥会让之前的所有会话失效，恢复本来就应如此。

## 执行备份

### 先数据库，后存储桶

1. 对整个 schema 做**一次事务一致的快照**。MySQL 上即 `mysqldump --single-transaction`（加上 `--routines --triggers --events --set-gtid-purged=OFF`），或使用云服务商的时间点快照。记录快照开始的时间：这就是**恢复点**。
2. **然后**复制存储桶前缀，例如 `mc mirror <alias>/<bucket>/<prefix> <destination>`。

绝不要先复制存储桶。数据库指向对象；存储桶副本必须至少包含快照所指向的每个对象。快照之后写入的对象只是无害的多余数据，而快照指向却不在副本中的对象就丢失了。

### 复制存储桶期间保持禁止删除窗口

从快照开始到存储桶复制结束，任何东西都不得删除快照仍然指向的对象。BuildMax 会在三处删除对象；每一处都要推迟到长于复制所需的时间：

- `storage.artifact_purge_after_days` 至少为 `1`，让已删除 Artifact 的字节比复制活得更久；
- `storage.checkpoint_orphan_grace_days` 至少为 `1`，这样快照之后其行消失的 checkpoint 载荷不会在复制中途被回收；
- 复制期间不修剪轨迹：保持 `trace.retention_days` 为 `0`，或者让备份远离每小时修剪过期轨迹的清理。

这三项都是 `server.yaml` 设置，修改需重启服务器；见[配置参考](../../reference/configuration.md)。这个窗口只消耗存储空间。

### 默认在线备份，升级前先静默

常规备份**在线**进行。快照时正在执行的 Run 会以 RUNNING 状态被恢复，但背后没有 worker；恢复后的服务器的存活回收器会在宽限期（两分钟未上报）后将其关闭为 `FAILED`，其结果就是在线备份**接受的损失**。恢复后请重试它。

升级之前，以及任何你不希望丢失 Run 的时候，先**静默**：停止创建新工作，等到没有 `RUNNING` 或 `SCHEDULED` 的 TaskRun、没有活动的 worker Job，然后在快照前将服务器 Deployment 缩容到零。恢复后的数据库若仍有 `PENDING` 的 Run，恢复服务器一启动就会分派它们，已启用的 Schedule 也会触发。

## 恢复到恢复环境

恢复到一个**空的恢复环境**，绝不要覆盖在线环境：

- **使用它自己的存储桶。** 恢复服务器的 checkpoint 孤儿清理会在启动时运行，删除其数据库未指向的载荷。若指向在线存储桶，它会删除恢复点之后写入的所有 checkpoint。
- **不要提供 Telegram bot token。** 持有在线部署 token 的恢复服务器会长轮询同一个 bot，抢走其用户的消息。在恢复环境成为在线环境之前，保持 `channels.telegram.bot_token` 和 `BUILDMAX_TELEGRAM_BOT_TOKEN` 未设置。

然后，在**任何服务器启动之前**：

1. 创建一个空数据库，用服务器所用的同样 MySQL 用户授权加载转储。
2. 创建恢复存储桶，并将备份复制到同一前缀下，例如 `mc mirror <backup> <recovery-alias>/<bucket>/<prefix>`。
3. 用**原始** KEK 文件创建 `buildmax-kek` Secret，新建 `buildmax-secret`（新的 `BUILDMAX_JWT_SECRET`，以及恢复环境的数据库与存储凭证），并生成新的 worker API TLS 材料。
4. 用备份的 `server.yaml` 创建 `buildmax-config` ConfigMap，只修改恢复环境不同的部分（端点、存储桶）。
5. 部署与备份时**相同的镜像标签**。更新的二进制会把 schema 向前迁移；更旧的二进制会拒绝在其上启动。见[升级](../../../deployment/production/README.md#upgrades)。

先创建 ConfigMap，再创建服务器 Deployment：`deployment/buildmax-deploy.yaml` 自带一个默认的 `buildmax-config`，在它之上启动的服务器会用错误的设置打开恢复后的数据库。

## 验证恢复

1. 在服务器容器中运行引用检查，它读取同一个 `server.yaml`：

   ```sh
   kubectl exec -n buildmax deploy/buildmax-server -- \
     buildmax-server storage verify --checksums
   ```

   它遍历数据库指向的每个存活 Artifact、工作区 checkpoint 载荷、Run 轨迹和插件包，按记录 ID 报告每个缺失、被改动或无法读取的对象，发现任何问题就以非零状态退出。它不修复任何东西。在依赖某个备份之前，先在源环境上也运行一次，这样之后的发现就能确认来自恢复。
2. 签发新的登录码（`buildmax-server user login-code <email>`），登录，并与备份前记录的内容比较：Space、Task 和 TaskRun 的标识符与状态，轨迹、审计事件和用量记录，以及每个 Artifact 列出的和下载得到的 SHA-256。
3. 继续（Continue）一个备份前的 Task。它的 Run 会恢复备份的工作区 checkpoint 和会话历史，这证明这些对象可用，而不仅是存在。

报告的恢复时间从空环境开始，到第一个校验和匹配的 Artifact 下载完成为止。恢复点是数据库快照。说明接受的损失：快照时正在执行的 Run，以及快照之后写入的所有内容。

## 在 kind 上演练

`./make kind drill restore` 在临时 kind 集群上演练整个流程，在其他地方会拒绝运行：

```sh
BUILDMAX_KIND_EPHEMERAL=1 ./make kind up
./make kind drill restore
./make kind down
```

它会种入一个带有继续 Run 的 Task（轨迹、checkpoint、会话包、一个已发布的 Artifact）、一个已上传并分享的 Artifact、一个 Space 文件、一个被 Agent 使用的 Space Secret，以及这些操作产生的审计行。它通过 API 为它们生成指纹，运行 `storage verify --checksums`，静默服务器，先执行 `mysqldump --single-transaction` 再执行 `mc mirror`，连同摘要写入 `.local/drill/restore-<time>/`，并保存 KEK 和 `server.yaml`。随后它删除 `db`、`storage` 和 `buildmax` 命名空间（MySQL 和 MinIO 使用 `emptyDir`，数据随之消失），在任何服务器启动之前恢复数据库和存储桶，并用原始 KEK 和 `server.yaml`、新的 JWT 密钥和新的 worker TLS 重建服务器。

只有同时满足以下条件才算通过：`storage verify --checksums` 无任何发现，每张表的行数一致，每个备份对象都以相同摘要存在，每个指纹条目都未改变，备份前的会话 token 被拒绝，Continue 恢复了备份的 checkpoint 和会话历史，且 Agent 的 Run 在恢复后的 KEK 下仍能收到备份前的 Secret 值。它会打印恢复点、恢复时间、每个阶段的耗时，以及备份之后新增的内容。kind 是对流程的演练，不是候选版本的资格验证证据。

## 相关文档

- [deployment/production/README.md](../../../deployment/production/README.md)——依赖契约、KEK 与升级
- [beta-readiness.md](beta-readiness.md)——候选版本必须通过的恢复项
- [local-kind.md](local-kind.md)——演练所在的 kind 集群
