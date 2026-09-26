# DigitalOcean 验证基础设施

> **翻译说明：** 本文是[英文原文](../../deploy/digitalocean.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
> **受众：** 运维人员 · **状态：** 当前
>
> 这是为 BuildMax Beta 门槛准备的最低成本外部基础设施。它创建基础设施并部署固定的试用版本；是否构成 Beta 门槛证据，仍由运维使用流程决定。

`./make ocean` 使用 OpenTofu 管理可销毁的 DOKS 集群和托管 MySQL 集群。它有意复用由运维人员创建并在 OpenTofu 外维护的三个资源：

| 资源 | 默认值 | 归属 |
|---|---|---|
| DigitalOcean Project | `buildmax-beta` | 持久保留，运维人员管理 |
| VPC | `sgp1` 中的 `buildmax-beta` | 持久保留，运维人员管理 |
| Spaces 存储桶 | `sgp1` 中的 `buildmax-beta` | 持久保留，运维人员管理 |
| DOKS 集群 | `buildmax-beta-doks`，一个 `s-2vcpu-4gb` 节点 | 可销毁，OpenTofu 管理 |
| 托管 MySQL | `buildmax-beta-mysql`，一个 `db-s-1vcpu-1gb` 节点 | 可销毁，OpenTofu 管理 |
| MySQL 防火墙和 `buildmax` 数据库 | 附属于托管 MySQL | 可销毁，OpenTofu 管理 |

DOKS 高可用、自动升级和 surge upgrade 均明确关闭。在 `./make ocean down` 成功前，DOKS 和 MySQL 资源持续计费；Project 和 VPC 免费，持久化 Spaces 订阅独立持续。创建资源前请确认提供商当前价格。

## 前置条件

安装 [OpenTofu](https://opentofu.org/docs/intro/install/) 和 `kubectl`，然后创建本地配置目录，将三个凭证加入 `.local/env`：

```bash
./make setup local
```

DigitalOcean API 令牌只需这些资源使用的能力：

- 账户和 actions：读取
- regions 和 sizes：读取
- Kubernetes：创建、读取、更新、删除、访问集群
- Tags：创建（OpenTofu 提供商会给默认 DOKS 节点池打标签）
- 数据库：创建、读取、更新、删除、查看凭证
- Project：读取和分配资源
- VPC：读取

不需要 Project 和 VPC 的创建、更新或删除权限，因为 OpenTofu 将它们作为已有资源读取。将 Spaces 密钥限制在 `buildmax-beta` 存储桶，授予读、写和删除权限；验证运行中的 BuildMax Worker 需要这三项权限。

在不调用 DigitalOcean、不写入文件的情况下验证本地设置：

```bash
./make ocean doctor
```

## 创建和检查

审查创建计划，然后应用：

```bash
./make ocean plan
./make ocean up
```

`up` 会再次生成计划，并要求输入 Project 名称才会应用。完成后：

```bash
./make ocean info
./make ocean status
export KUBECONFIG="$HOME/.buildmax/qualification/ocean/kubeconfig.yaml"
kubectl get nodes
```

`info` 输出资源名称和端点，绝不输出数据库密码或 kubeconfig 内容。

OpenTofu 源码位于 [`deployment/ocean/`](../../../deployment/ocean/)。无需修改源码即可调整名称和区域：

| 变量 | 默认值 |
|---|---|
| `BUILDMAX_OCEAN_PROJECT` | `buildmax-beta` |
| `BUILDMAX_OCEAN_VPC` | `buildmax-beta` |
| `BUILDMAX_OCEAN_BUCKET` | `buildmax-beta` |
| `BUILDMAX_OCEAN_REGION` | `sgp1` |
| `BUILDMAX_OCEAN_DATABASE_VERSION` | `8.4` |
| `BUILDMAX_OCEAN_STATE_DIR` | `~/.buildmax/qualification/ocean` |

三个持久资源都必须使用配置的区域。

## 部署应用试用版本

应用阶段需要完整主机名和至少一个可信客户端网络。Caddy 边缘代理执行 CIDR 限制，同时保持自动证书质询可访问：

```bash
BUILDMAX_OCEAN_HOSTNAME=buildmax.beta.cloudbb.io
BUILDMAX_OCEAN_ALLOWED_CIDRS=203.0.113.7/32
```

将这些值加入 `.local/env`，使用将执行验证的机器或私有网络的公网 CIDR。多个 CIDR 用逗号分隔。缺少允许列表会直接报错：Beta 限制不允许将应用直接暴露到不可信公共网络。

部署固定的试用镜像：

```bash
./make ocean deploy
./make ocean model init
./make ocean app-status
./make ocean show all
```

`show all` 使用 `ocean up` 写入、仅所有者可访问的 kubeconfig，运行 `kubectl get all --namespace buildmax --output wide`。它是只读操作，不依赖贡献者当前的 Kubernetes Context。

`model init` 从 `.local/env` 读取 `OPENROUTER_API_KEY`，在模型名称尚不存在时添加配置的模型，选择其生成的目录 ID 用于 Tier 1 Conversation，然后只重启 BuildMax Server Deployment。密钥通过 stdin 传给运维命令，绝不打印或渲染到 Kubernetes 清单中。重复执行会复用已有目录行。

默认使用仓库的低成本 OpenRouter 基线模型 `GPT-5.6 Luna`（`openai/gpt-5.6-luna`）。可用 `.env.example` 中记录的 `BUILDMAX_OCEAN_MODEL_*` 变量覆盖元数据；价格变化时应在初始化目录前更新，以保证调用成本记录准确。随时查看脱敏后的目录：

```bash
./make ocean model list
```

默认采用 `v0.2.0-alpha.4` 发布的不可变多平台摘要，以及固定的 Caddy 2.10.2 镜像。覆盖时只能使用另一个摘要，不能使用可变标签：

| 变量 | 默认产物 |
|---|---|
| `BUILDMAX_OCEAN_IMAGE` | `ghcr.io/icloudbb/buildmax@sha256:64e6775796b4bf0cb1145e3aaa79084e170f1ec340bd5af1cddc1a28cc0336dd` |
| `BUILDMAX_OCEAN_PORTAL_IMAGE` | `ghcr.io/icloudbb/buildmax-portal@sha256:82165de877e4cae3c5a1c598b6f39b37a94db114ab6ce315b237d5913f7e2e2b` |
| `BUILDMAX_OCEAN_EDGE_IMAGE` | `caddy:2.10.2-alpine@sha256:4c6e91c6ed0e2fa03efd5b44747b625fec79bc9cd06ac5235a779726618e530d` |

`deploy` 刷新 OpenTofu 的只读数据库 CA 输出，将该 CA 与镜像的公共信任证书包合并，再以 `database.tls: "true"` 启动 BuildMax。因此服务器会验证 DigitalOcean MySQL 和公共 HTTPS 依赖，绝不使用 `skip-verify`。数据库、Spaces 和生成的 JWT 凭证通过内存组装的 Secret 传入 Kubernetes。渲染后的 Secret 不会写入检出目录。

首次 `deploy` 还会在状态目录中生成部署密钥加密密钥（KEK）`kek.json`，之后每次部署都把同一文件作为 `buildmax-kek` Secret 下发，以只读、`0400` 权限只挂载进 server pod 的 `/etc/buildmax/kek/kek.json`（位于 `BUILDMAX_HOME` 之外），并由 `secret.kek_file` 指向。Server 用它封存 `model init` 添加的模型凭证，因此 `model init` 需要一个已经具备 KEK 的部署：如果是从早于 KEK 的部署升级而来，请先再运行一次 `deploy`。该密钥绝不会重新生成。如果 `kek.json` 缺失而集群中仍有 `buildmax-kek` Secret，`deploy` 会拒绝执行而不是替换密钥；请从备份恢复该文件。文件格式见 [KEK 参考](../reference/configuration.md#部署密钥加密密钥)。

命令最后输出 DigitalOcean Load Balancer IP。请在 Route 53 中手动添加记录：

```text
buildmax.beta.cloudbb.io  A  <Load Balancer IP>
```

公共 DNS 生效后，Caddy 获取证书。Load Balancer 处于 pending 时可重复运行 `app-status`，然后从允许的网络验证：

```bash
curl -I https://buildmax.beta.cloudbb.io/
```

应用阶段还会创建一个 DigitalOcean Load Balancer，以及保存 Caddy 证书状态的 1 GiB 块存储卷声明。两者都收费，并关联到可销毁的 DOKS 集群，因此 `./make ocean down` 会随集群一起删除它们。

部署步骤无需模型凭证，即可验证外部 MySQL、Spaces、Kubernetes、Load Balancer 和 TLS 路径。`model init` 是单独、明确的步骤，在 Beta 运维使用流程前选择已批准的托管模型。

## 在本地检查 MySQL

托管数据库仅接受 DOKS 集群流量，`info` 输出其私有 VPC 主机名。保留这一防火墙边界，通过 Kubernetes API 建立隧道，不要添加公共数据库规则：

```bash
./make ocean info --show-secrets
./make ocean database forward
```

第一条命令明确输出数据库用户名和密码；普通 `info` 会隐藏它们。第二条创建不持有凭证的代理 Deployment，将 DigitalOcean CA 写入仅所有者可访问的 Ocean 状态目录，并将 MySQL 转发到 `127.0.0.1:13306`，直到被中断。它输出一条可直接运行、会验证数据库 CA 的 `mysql` 命令。使用 `BUILDMAX_OCEAN_DATABASE_LOCAL_PORT` 覆盖本地端口。

代理没有 Service，不会让 MySQL 在 Kubernetes API 之外可访问。其 Deployment 随可销毁的 DOKS 集群一起删除。

## 状态和密钥

OpenTofu 状态包含生成的 MySQL 密码和 DOKS kubeconfig，因此命令拒绝使用 Git 检出目录内的状态目录。状态、保存的计划、提供商工作目录和生成的 kubeconfig 都位于 `BUILDMAX_OCEAN_STATE_DIR` 下，目录及敏感文件权限均设为仅所有者可访问。

请将此目录视为凭证：

- 绝不上传到应用的 Spaces 存储桶
- 托管资源存在期间安全备份
- 不要在 `./make ocean down` 前删除
- 如果泄露，轮换数据库凭证和 Kubernetes 访问凭证

该目录还保存 `kek.json`，即封存托管数据库中模型凭证和 Space Secret 的密钥。请将它与任何数据库备份分开备份：同时包含两者的备份等于没有保护，而在没有对应 KEK 的情况下恢复的数据库，其凭证无法读取，任何 BuildMax 命令都无法找回。`ocean down` 会保留该文件，下次部署会复用它。

`.local/env` 同样保留在本地并被 gitignore 忽略。OpenTofu 从 `./make` 填充的环境变量读取凭证，不会向 OpenTofu 源码写入凭证。

## 销毁

验证会话结束后：

```bash
./make ocean down
```

命令展示销毁计划，并再次要求输入 Project 名称。它销毁 `deployment/ocean` 声明的临时栈：DOKS、托管 MySQL、MySQL 防火墙/数据库、集群的 Project 分配，以及与该 DOKS 集群关联的 DigitalOcean 资源。现有 Project、VPC、Spaces 存储桶和全部 Route 53 记录都会保留。

清理后检查 DigitalOcean 控制面板。如果应用部署在此 OpenTofu 状态之外创建了负载均衡器，请保留 Beta 门槛证据后单独移除。

## 下一步验证

基础设施和应用部署建立了真实的外部依赖边界，但 Beta 门槛仍要求实际演练固定候选版本。记录已批准的托管模型，然后执行下述运维使用流程、故障演练、备份恢复和回滚。

将最终部署、冒烟测试结果、故障演练、恢复和回滚记录在 [Beta 就绪记录](beta-readiness.md)中。
