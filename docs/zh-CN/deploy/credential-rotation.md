# 轮换部署凭证

> **翻译说明：** 本文是[英文原文](../../deploy/credential-rotation.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
> **受众：** 运维人员 · **状态：** 当前——已在 kind 上演练，尚未在 Beta 候选版本上演练
本手册逐一替换 BuildMax Kubernetes 部署持有的每个凭证，无需重新构建任何内容。对每个凭证，它说明新旧值如何重叠、旧值作废前需要排空什么、用户和运行会察觉到什么，以及如何确认旧值已失效。

命令假定使用[生产参考部署](../../../deployment/production/README.md)：命名空间 `buildmax`、`buildmax-server` Deployment，以及 `buildmax-secret` 和 `buildmax-kek` 两个 Secret。如果由密钥管理器（External Secrets、Vault、sealed-secrets）管理这些 Secret，请在那里修改值并等待同步，而不是直接修改 Secret。

## 目录

- [开始之前](#开始之前)
- [JWT 密钥](#jwt-密钥)
- [数据库密码](#数据库密码)
- [对象存储密钥](#对象存储密钥)
- [模型提供商密钥](#模型提供商密钥)
- [密钥加密密钥](#密钥加密密钥)
- [Worker API 证书与 CA](#worker-api-证书与-ca)
- [OIDC 客户端密钥、Telegram Bot 令牌与 Redis 密码](#oidc-客户端密钥telegram-bot-令牌与-redis-密码)
- [实测影响](#实测影响)

## 开始之前

Server 只在启动时读取下列每个凭证一次。因此修改后的 Secret 在 Server 滚动重启后生效：

```sh
kubectl -n buildmax rollout restart deployment/buildmax-server
kubectl -n buildmax rollout status deployment/buildmax-server
```

滚动发布以 `maxUnavailable: 0` 逐个替换 Pod，API 保持可用。当 `rollout status` 返回且没有旧 Server Pod 仍在终止时（`kubectl -n buildmax get pods -l app=buildmax-server`），滚动完成。

有两个凭证——对象存储密钥和直连提供商密钥——还会在创建每个 worker Job 时**按值**传给它。Job 在整个运行期间都保留启动时拿到的密钥，因此在停用其中任何一个旧密钥之前，要等滚动之前创建的所有 worker Job 结束。以下命令列出仍在运行的 worker Job 及其启动时间：

```sh
kubectl -n buildmax get jobs -l app.kubernetes.io/name=buildmax-worker \
  -o jsonpath='{range .items[?(@.status.active)]}{.metadata.name}{"  "}{.metadata.creationTimestamp}{"\n"}{end}'
```

一次只轮换一个凭证，验证通过后再开始下一个，这样出现故障时可以定位到单一变更。

## JWT 密钥

`buildmax-secret` 中的 `BUILDMAX_JWT_SECRET` 用于签发用户访问令牌、worker Run 令牌，以及进行中 OIDC 登录的状态。同一时间只有一个签名密钥，因此**没有重叠期**：新 Pod 开始服务的那一刻新密钥即生效，旧密钥签发的所有令牌都不再通过校验。

1. 按需排空。如果执行中的运行很重要，请等到没有活跃的 worker Job。没有暂停派发的开关，因此请选择空闲时段。
2. 替换密钥并滚动：

   ```sh
   kubectl -n buildmax patch secret buildmax-secret --type merge \
     -p "{\"stringData\":{\"BUILDMAX_JWT_SECRET\":\"$(openssl rand -hex 32)\"}}"
   kubectl -n buildmax rollout restart deployment/buildmax-server
   kubectl -n buildmax rollout status deployment/buildmax-server
   ```

用户和运行会察觉到：

- **已登录用户保持登录。** 刷新令牌是存储行而非签名，因此不受影响。Portal、CLI、Desktop 和 Remote Control 会话都会把第一次 `401` 当作信号，刷新一次后重试。
- **执行中的运行会丢失。** worker 的 Run 令牌由旧密钥签发，因此无法再上报。存活宽限期（2 分钟）和下一次巡检过后，该运行被结算为 `FAILED`，原因为 "this run lost its worker"；请以新运行重试。worker Job 本身会继续工作到运行结束，然后在无法上报结果的情况下退出。
- 处于重定向与回调之间的 OIDC 登录会失败，需要重新开始。
- 轮换该密钥**不会**让任何人登出。要结束 Session，请撤销它们——见 [authentication.md](authentication.md)。

验证：使用轮换前签发的访问令牌的请求返回 `401`；使用轮换前签发的刷新令牌刷新返回 `200`；新运行成功。

## 数据库密码

MySQL 8.0.14 及以上版本每个账户可保留两个密码，因此新密码先与旧密码并存，Server 切换到新密码，之后才删除旧密码。Server 连接池中的连接已完成认证，不受任一变更影响。worker 从不连接数据库，因此无需排空。

1. 使用有权修改 BuildMax 用户的账户，添加新密码并保留当前密码：

   ```sql
   ALTER USER 'buildmax'@'%' IDENTIFIED BY '<new-password>' RETAIN CURRENT PASSWORD;
   ```

2. 把新密码写入 `BUILDMAX_DATABASE_PASSWORD` 并滚动：

   ```sh
   kubectl -n buildmax patch secret buildmax-secret --type merge \
     -p '{"stringData":{"BUILDMAX_DATABASE_PASSWORD":"<new-password>"}}'
   kubectl -n buildmax rollout restart deployment/buildmax-server
   kubectl -n buildmax rollout status deployment/buildmax-server
   ```

3. 没有旧 Server Pod 残留后，丢弃旧密码：

   ```sql
   ALTER USER 'buildmax'@'%' DISCARD OLD PASSWORD;
   ```

预期影响：无。验证使用旧密码登录 MySQL 被拒绝（`Access denied`），Server Pod 的重启次数没有变化，且其 `/readyz` 一直为 `200`。

不支持双密码的数据库服务改用两个账户轮换：创建一个授权相同的第二个账户，把 Server ConfigMap 中的 `database.user` 和 `BUILDMAX_DATABASE_PASSWORD` 指向它，滚动，然后删除第一个账户。

## 对象存储密钥

如果 Server 和 worker 通过 IRSA、workload identity 或实例配置文件访问存储桶，请跳过本节：该身份由平台负责轮换。

静态密钥通过让两个拥有相同存储桶权限的身份重叠来轮换——同一 IAM 用户的两个访问密钥，或绑定同一策略的两个 MinIO 用户。worker 按值持有密钥，因此旧密钥要一直保持启用，直到携带它的 Job 全部结束。

1. 创建拥有相同权限的新密钥。在 AWS 上，对同一用户执行 `aws iam create-access-key`；在 MinIO 上：

   ```sh
   mc admin user add <alias> <new-access-key> <new-secret-key>
   mc admin policy attach <alias> <bucket-policy> --user <new-access-key>
   ```

2. 写入 Secret 并滚动：

   ```sh
   kubectl -n buildmax patch secret buildmax-secret --type merge -p \
     '{"stringData":{"BUILDMAX_STORAGE_MINIO_ACCESS_KEY":"<new-access-key>","BUILDMAX_STORAGE_MINIO_SECRET_KEY":"<new-secret-key>"}}'
   kubectl -n buildmax rollout restart deployment/buildmax-server
   kubectl -n buildmax rollout status deployment/buildmax-server
   ```

3. 等待滚动之前创建的所有 worker Job 结束（见[开始之前](#开始之前)）。
4. 停用旧密钥——`aws iam update-access-key --status Inactive`，或 `mc admin user disable <alias> <old-access-key>`——确认不再有任何使用后将其删除。

预期影响：无。如果某个运行的 worker 在旧密钥被停用时仍持有它，该运行会在下一次存储写入时失败，原因点明被拒绝的写入。验证用旧密钥签名的请求返回 `403`，轮换前存储的 Artifact 仍能以相同校验和下载，且新运行成功。

## 模型提供商密钥

提供商密钥的重叠由提供商自身提供：切换前先在提供商处创建新密钥，等 BuildMax 不再使用旧密钥后再吊销它。

**托管目录模型**的密钥密封保存在数据库中。原地替换它——密钥从标准输入读取，而非命令行：

```sh
buildmax admin model set-key <model-id>
# or, database-direct from a server pod:
kubectl -n buildmax exec -i deploy/buildmax-server -- buildmax-server model set-key --id <model-id>
```

该变更会更新目录行的修订版本，网关从下一次调用起使用新密钥：无需重启，托管 worker 也从未持有该密钥。已在进行中的调用会用旧密钥完成，因此请在超过该模型的调用超时后再到提供商处吊销旧密钥。验证经该模型的调用成功，且提供商显示旧密钥不再被使用。

**直连模型密钥**（`conversation.model.api_key`，通过 `BUILDMAX_CONVERSATION_MODEL_API_KEY` 设置）由 Server 在启动时读取，并按值传给直连提供商的 worker。修改 Secret、滚动 Server、等待滚动之前创建的 worker Job 结束，然后在提供商处吊销旧密钥。

## 密钥加密密钥

`buildmax-kek` 中的 KEK 用于密封托管模型密钥和 Space Secret。密钥文件可同时容纳多个密钥，并用 `current` 指明新写入使用哪一个，因此轮换依次是：添加密钥、切换 `current`、重新封装已存储的行，最后才删除旧密钥。每一步都要滚动一次，因为每个 Server Pod 都必须能打开任何其他 Pod 写入的内容。

1. 导出文件，添加新密钥并滚动。`current` 仍为旧密钥：

   ```sh
   kubectl -n buildmax get secret buildmax-kek -o jsonpath='{.data.kek\.json}' | base64 -d > kek.json
   # add "file:root:2": "<output of: openssl rand -base64 32>" under "keys"
   kubectl -n buildmax create secret generic buildmax-kek --from-file=kek.json=kek.json \
     --dry-run=client -o yaml | kubectl apply -f -
   kubectl -n buildmax rollout restart deployment/buildmax-server
   kubectl -n buildmax rollout status deployment/buildmax-server
   ```

2. 把 `"current"` 设为新密钥 id，以同样方式应用文件并滚动。现在就备份该文件——它是新密钥唯一的副本。
3. 在新密钥下重新封装所有已存储的行，然后阅读其报告：

   ```sh
   kubectl -n buildmax exec deploy/buildmax-server -- buildmax-server secret rewrap
   ```

   除当前密钥外，每个密钥都必须显示 `0 (no row uses it; it can be removed from the key file)`。仍在使用的密钥说明有某行是由尚未加载新 `current` 的 Pod 写入的；请再次运行该命令。
4. 从文件中删除旧密钥，应用并滚动。

只要有已存储的行引用了密钥文件中没有的密钥，Server 就拒绝启动。因此如果最后一步的滚动无法完成，说明还有行被遗漏：放回旧密钥、滚动并再次重新封装。整个过程不解密也不重新加密任何值，用户和运行都不会察觉。

把旧密钥的材料与重新封装之前的数据库备份放在一起保存：那些转储仍引用它，没有它就无法读取从中恢复的数据。见 [KEK 参考](../../reference/configuration.md#the-deployment-key-encryption-key)。

## Worker API 证书与 CA

Server 使用 `buildmax-worker-api-tls` Secret 中的叶证书提供 worker API，该证书在启动时加载。每个 worker 根据 `buildmax-worker-api-ca` ConfigMap 中的 CA 包校验它，Job 在启动时挂载并读取该 CA 包。

**同一 CA 签发的新叶证书**只需滚动：替换 `tls.crt` 和 `tls.key`（cert-manager 会自动续期），然后在旧叶证书过期前滚动 Server。运行中的 worker 仍信任该 CA，因此不会有损失。

**新 CA** 没有重叠期。把新 CA 写入 `worker-api-ca.crt`，把叶证书换成由它签发的证书，然后滚动 Server。变更前启动的 worker 只信任旧 CA，无法再连接 Server，其运行会像 JWT 轮换那样被结算为 `FAILED`；请重试这些运行。要避免这种损失，先发布同时包含新旧 CA 的证书包，等在此之前启动的 Job 结束，再切换叶证书，稍后删除旧 CA。

## OIDC 客户端密钥、Telegram Bot 令牌与 Redis 密码

kind 演练不覆盖这些凭证；它们都遵循相同的“修改 Secret 并滚动”流程。

| 凭证 | 重叠 | 流程 | 影响 |
|---|---|---|---|
| `BUILDMAX_OIDC_CLIENT_SECRET` | 多数身份提供商（包括 Okta）允许同时存在两个有效的客户端密钥 | 在提供商处创建第二个密钥，修改 Secret 并滚动，然后在提供商处停用旧密钥 | 预期无 |
| `BUILDMAX_TELEGRAM_BOT_TOKEN` | 无：BotFather 的 `/revoke` 会立即使旧令牌失效 | 吊销并获取新令牌，修改 Secret 并滚动 | 滚动完成前 Bot 无响应；Telegram 会保留未读消息最多 24 小时，Server 之后会读取它们 |
| `BUILDMAX_COORDINATION_REDIS_PASSWORD` | Redis 6+ 的 ACL 用户可持有多个密码 | `ACL SETUSER <user> >new-password`，修改 Secret 并滚动，然后执行 `ACL SETUSER <user> <old-password` | 预期无；已建立的连接保持认证状态 |

## 实测影响

`./make kind drill rotation` 在一次性 kind 集群上演练上文的 JWT、数据库、对象存储、托管模型和 KEK 流程，并断言每一步的结果。它是流程演练，而非资格验证：[Beta 就绪记录](beta-readiness.md)需要在固定候选版本自己的 MySQL、S3 和 Ingress 上完成同样的演练。2026-09-26 的演练实测结果如下：

| 凭证 | Server 滚动 | 旧凭证 | 中断 |
|---|---|---|---|
| JWT 密钥 | 22 秒 | 旧访问令牌以及用旧密钥新签发的令牌：`401`；旧刷新令牌：`200`，且得到可用的访问令牌 | 跨越轮换的执行中运行在 Secret 修改后 3 分 5 秒被结算为 `FAILED`；新运行成功 |
| 数据库密码 | 24 秒 | 旧密码：`Access denied` | 无：经 Ingress 的 86 次 API 读取全部成功，`/readyz` 一直为 `200`，没有 Pod 重启 |
| 对象存储密钥 | 24 秒 | 停用后的旧密钥：`403` | 无：46 次 API 读取全部成功；轮换前的 Artifact 以相同校验和下载；新运行成功 |
| 托管模型密钥 | 无 | 行修订版本已变化；下一次调用发送了新密钥 | 无 |
| KEK | 3 次滚动，每次 22–24 秒 | `rewrap` 移动了所有行；旧密钥已删除 | 无：Server 在没有旧密钥的情况下正常重启，密封的模型密钥仍可打开 |
