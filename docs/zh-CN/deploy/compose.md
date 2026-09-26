# Compose 快速入门

> **翻译说明：** 本文是[英文原文](../../deploy/compose.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
> **受众：** 运维人员 · **状态：** 当前
>
> 在一台机器上约五分钟即可部署一个 Space。各组件的作用以及其他环境的运行方式见[部署概览](overview.md)，可以先读或稍后再读；本页提供最短路径。

## 部署内容

三个容器：MySQL、服务器（自行启动 Worker）和 Portal。不包含 MinIO：服务器与 Worker 共享同一容器和卷，本地文件系统后端已足够。如果 Worker 在其他位置运行，则需要双方都能访问的 Blob 存储。

共享容器也是此栈的安全取舍：TaskRun 是调度它的服务器的子进程，使用相同 uid，两者属于同一个信任域。对于已经信任所有工作提交者的 Space，这种方式可以接受。如果需要隔离服务器与模型选择执行的代码，请使用 Kubernetes Job 运行 Worker，见[部署概览](overview.md)。

## 启动

对于贡献开发，一条命令即可使用确定性的模拟模型启动栈，并通过真实 Worker 进程验证完整流程：

```bash
./make compose smoke
```

该检查覆盖 Portal 可访问性、账户引导、Space 存储、Conversation 和 TaskRun 创建、调度执行、模型响应、Artifact 获取，以及取消正在执行的运行。无需提供商密钥。失败时使用 `./make compose logs` 检查，再用 `./make compose down` 停止栈。成功时会为冒烟测试账户输出一个新的单次使用 Portal 登录码。

同一流程也可让 TaskRun 推理经过托管网关，而非直接调用提供商：

```bash
./make compose smoke managed
```

此变体验证默认模式无法证明的一点：Worker 在不持有提供商凭证的情况下完成真实 Task。它检查运行轨迹是否将目录模型名称记录为所用模型；暗中使用提供商密钥的 Worker 无法产生这个结果。两种模式使用独立的栈，因为传输方式属于启动配置，一个服务器不能同时服务两种模式，而两者都需要端到端覆盖。切换模式会重新创建服务器容器。

`./make compose status` 不修改任何状态。它列出栈内所有服务（包括已退出的服务），并探测宿主机上的服务器和 Portal 端口，因此无需先读日志就能区分“从未启动”与“服务器反复崩溃重启”。

如需用真实模型进行交互式评估，启动常规栈：

```bash
cd deployment/compose
./generate-env.sh          # writes .env with generated secrets
docker compose up -d
```

首次 `up` 从当前检出构建镜像，需要几分钟。发行版发布镜像后，可改用 `docker compose pull` 获取。

等待服务器报告健康：

```bash
docker compose ps
```

## 创建账户

用户无法自行注册，因为[注册默认关闭](authentication.md)。在服务器容器内创建首个账户：

```bash
docker compose exec server buildmax-server user create you@example.com
docker compose exec server buildmax-server user login-code you@example.com
```

第二条命令输出单次使用验证码。打开 <http://localhost:8080>，选择“Forgot your password, or have a login code?”（忘记密码或已有登录码？），输入邮件地址和验证码。随后在账户设置中设置密码，此后即可正常使用密码登录。

Desktop 应用和 `buildmax login` 登录同一账户，但直接调用 API，因此它们的 **Server URL** 为 <http://localhost:5678>，不是 Portal 的端口。

## 添加模型

Portal 可以在没有模型的情况下运行，但配置密钥前，Conversation 无法访问模型。将密钥写入 `.env`：

```bash
BUILDMAX_CONVERSATION_MODEL_API_KEY=sk-your-key-here
```

然后执行 `docker compose up -d` 应用配置。端点和模型 ID 位于 Compose 文件旁的 `server.yaml` 中，支持任何 OpenAI 兼容提供商。

## 向工作区添加内容

新 Space 最初为空，没有内容可读的 Agent 很难展示能力。每个 Space 都有持久化文件空间，即工作区；Worker 会将其物化到每次 TaskRun 中，所以放入其中的内容就是 Agent 工作时看到的内容。

打开 <http://localhost:8080/#/explore> 的 **Files** 页面。**Upload Files** 上传单独文件；**Upload Folder** 保留目录结构，此处应使用后者。

仓库附带了一组适合此场景的数据集：

```text
sample-data/sales/       revenue by region and quarter, across nested year folders
sample-data/access_log/  a web access log
sample-data/orders/      e-commerce orders, with a README describing the columns
```

将 `sample-data/sales/` 作为文件夹上传，然后开始 Conversation，提出需要读取数据的问题，例如“2024 到 2025 年哪个地区增长最快？请展示你使用的数据”。足够复杂、需要转为后台 Task 的工作会在 Worker 中使用同一工作区的副本执行，并将结果报告回发起它的 Conversation。

[sample-data/README.md](../../../sample-data/README.md) 列出了全部十五个数据集。它们只是普通文件，没有特殊机制；你自己的任何文件夹也能以相同方式使用。

## 对外使用前需要调整的内容

此栈面向笔记本环境。向其他人开放前：

- **端口发布到宿主机。** 只要能访问该机器，就可能访问 `8080`（网关：Portal 与 API 同源）和 `5678`（服务器直连端口，供 CLI 使用）。
- **没有 TLS。** 全部使用明文 HTTP。如需 TLS，请在前方放置反向代理；浏览器已通过网关访问单一源。
- **Agent 会执行 shell 命令。** Worker 在服务器容器内执行模型要求的命令。官方镜像会选择 worker 沙箱基线；此 Compose local-process 路径不会形成独立的宿主机信任边界。参见[沙箱边界](../../../manual/sandbox.md)。
- **数据存储在 Docker 卷中。** `docker compose down -v` 会删除其中所有工作区、Artifact 和账户。

## 更改宿主机端口

只需修改 `.env` 中的 `BUILDMAX_PORTAL_PORT`（浏览器打开的网关端口）和 `BUILDMAX_SERVER_PORT`（服务器直连端口）。浏览器通过同一个网关源访问 Portal 与 API,因此不存在需要保持一致的跨源配对；服务器的 `cors_origin` 仍从 `BUILDMAX_PORTAL_PORT` 推导,只是因为 WebSocket 升级仍会用它校验请求来源。

调整 `BUILDMAX_PORTAL_PORT` 也可让此栈与发布固定 `8080` 端口的 [kind 集群](local-kind.md) 并行运行。

## 常见问题

| 症状 | 原因 |
|---|---|
| `run ./generate-env.sh first` | 缺少 `.env`；Compose 会拒绝启动，而不是使用空密钥 |
| Portal 能打开，但页面短暂出现 502 | 网关仍在预热或正在跟随刚重启的上游；刷新即可 |
| `signup is disabled on this server` | 符合预期。使用 `user create` 创建账户 |
| `invalid otp` | 验证码只能使用一次，一小时后过期；请重新签发 |
| 服务器反复重启 | 通常与 MySQL 有关，运行 `docker compose logs mysql` |

## 升级

运行固定的发布版本：在 `.env` 中把 `BUILDMAX_VERSION` 设为其标签，例如 `0.2.0-alpha.15`。server 启动时会向前迁移 schema，且不支持回滚镜像。较旧的镜像会拒绝连接已被更新版本迁移的数据库启动；0.2.0-alpha.15 及更早的镜像早于这项拒绝，会直接损坏数据库。因此每次升级前都要备份，并先停止 server，使数据库与卷保持一致：

```bash
mkdir -p backup
docker compose stop server
docker compose exec -T mysql sh -c 'MYSQL_PWD="$MYSQL_PASSWORD" mysqldump -ubuildmax --single-transaction --no-tablespaces --hex-blob buildmax' > backup/buildmax.sql
docker run --rm -v buildmax_server-data:/data alpine tar -cf - -C /data . > backup/server-data.tar
cp server.yaml .env backup/
```

`buildmax_server-data` 是保存 Artifact 与运行状态的卷；它以 Compose 项目命名，可用 `docker volume ls` 查看。然后在 `.env` 中设置新标签并启动：

```bash
docker compose pull
docker compose up -d
```

如果升级出错，恢复备份的两部分，并运行写下它们的标签：

```bash
docker compose stop server
docker compose exec -T mysql sh -c 'MYSQL_PWD="$MYSQL_PASSWORD" mysql -ubuildmax -e "DROP DATABASE buildmax; CREATE DATABASE buildmax"'
docker compose exec -T mysql sh -c 'MYSQL_PWD="$MYSQL_PASSWORD" mysql -ubuildmax buildmax' < backup/buildmax.sql
docker run --rm -i -v buildmax_server-data:/data alpine sh -c 'find /data -mindepth 1 -delete && tar -xf - -C /data' < backup/server-data.tar
# set BUILDMAX_VERSION in .env back to the old tag
docker compose up -d
```

每个发布候选都会用 `./make compose upgrade-drill` 从上一个发布版本演练这一流程；参见[发布流程](../contribute/releasing.md#准备)。

## 清理

```bash
docker compose down       # keep the data
docker compose down -v    # delete it
```

## 相关文档

- [部署概览](overview.md)：拓扑和逐字段配置
- [身份认证](authentication.md)：账户、登录码和尚缺的能力
- [本地 kind](local-kind.md)：Kubernetes 部署路径
