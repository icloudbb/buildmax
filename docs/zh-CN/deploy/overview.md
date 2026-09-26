# 部署概览

> **翻译说明：** 本文是[英文原文](../../deploy/overview.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
> **受众：** 运维人员 · **状态：** 当前
>
> **BuildMax 处于 Alpha 阶段。** 将服务器放到不可信人员可访问的位置前，请阅读[身份认证](authentication.md)。当前部署支持边界见 [manual/support.md](../../../manual/support.md)。

为 Space 部署 BuildMax，需要运行两个 Go 二进制程序和两个支撑服务。无需向 Agent 运行时本身安装任何东西：Worker 使用与 CLI 相同的运行时，由服务器启动。

如果想先看到系统运行，再阅读细节，可使用较快的本地进程路径 [Compose 冒烟测试](compose.md)，或 Kubernetes Job 路径 [kind 冒烟测试](local-kind.md)。

这些开发检查不能证明发行版可用于生产。要针对外部依赖、故障场景、恢复和回滚测试固定的候选版本，请准备可销毁的 [DigitalOcean 验证基础设施](digitalocean.md)，并使用 [Beta 就绪记录](beta-readiness.md)。

## 拓扑

```text
             ┌────────────┐
  browser ──▶│   Portal   │  static React bundle
             └─────┬──────┘
                   │ HTTP + WebSocket
             ┌─────▼────────────────────┐        ┌──────────┐
             │     buildmax-server      │───────▶│  MySQL   │
             │  HTTP API + scheduler    │        └──────────┘
             └─────┬────────────────────┘
                   │ spawns one process (or k8s Job) per run
             ┌─────▼────────────────────┐        ┌──────────────────┐
             │     buildmax-worker      │───────▶│ Blob storage     │
             │  one task run, then exit │        │ local FS or S3   │
             └──────────────────────────┘        └──────────────────┘
```

| 组件 | 职责 |
|---|---|
| `buildmax-server` | HTTP API、身份认证、Space 数据、WebSocket 广播，以及认领 `PENDING` 运行的进程内调度器 |
| `buildmax-worker` | 恰好执行一次 TaskRun 后退出。**直接**读写 Blob 存储，不经过服务器。 |
| MySQL | Space、Conversation、Issue、Workflow、Task、Run 和用量 |
| Blob 存储 | Space 文件上传（`persist_backend`）和运行 Artifact（`artifact_backend`）。支持本地文件系统或 MinIO 等任意 S3 兼容服务。 |
| Portal | 从 `portal/` 构建的静态前端，通过 HTTP 与服务器通信 |

## 要求

- 服务器可访问 MySQL
- **服务器和每个 Worker** 都能访问 Blob 存储
- Worker 作为本地进程运行时，服务器与 Worker 共享可写的 `workspaces_dir`
- 服务器（Tier 1 Conversation 模型）和 Worker（实际运行使用的模型）都能访问 LLM 端点

## 配置

全部配置位于 `<BUILDMAX_HOME>/server.yaml`。从示例开始，填写四个主要块：`database`、`storage`、`worker` 和 `conversation`：

```bash
mkdir -p /etc/buildmax
export BUILDMAX_HOME=/etc/buildmax
cp config-examples/server.example.yaml $BUILDMAX_HOME/server.yaml
```

在部署时注入密钥，不要写入文件：

```bash
export BUILDMAX_JWT_SECRET="$(openssl rand -hex 32)"
```

若要存储受管模型的提供商凭据或 Space Secret，还需挂载一个密钥加密密钥文件——只读、仅 server 可读、位于 `BUILDMAX_HOME` 之外——并让 `secret.kek_file` 指向它。没有它，添加带 API key 的模型会被拒绝。该文件只生成一次，并与数据库分开备份：丢失它会使所有已封存的凭据无法读取。格式和生成命令见 [KEK 参考](../reference/configuration.md#部署密钥加密密钥)。

逐字段说明见[配置参考](../reference/configuration.md)。

## 运行

```bash
buildmax-server                 # honours port from server.yaml, or --port
```

调度器随服务器启动。它启动 `worker.binary`，因此 `buildmax-worker` 必须位于 `PATH` 中或服务器二进制旁边，且 Worker 必须能携带服务器签发的 Run 令牌访问 `worker.server_url`。在默认的 `local_process` 模式下，Worker 是服务器同一 uid 下的子进程，两者处于同一信任域，见[运行边界](#运行边界)。

设置 `worker.run_mode: k8s_job` 后，调度器会使用 `worker.k8s.namespace` 和 `worker.k8s.image`，为每次运行创建 Kubernetes Job，而非本地进程。此模式还要求完整配置四个 `worker.k8s.resources` 边界；否则服务器拒绝启动，不会调度资源无界的 Worker。

此模式中，Worker Pod 需要与服务器相同的 `server.yaml`。`worker.k8s.config_map` 指定包含 `server.yaml` 键的 ConfigMap，调度器将其挂载到每个 Worker Pod 的 `worker.k8s.home_dir`，并将 Pod 的 `BUILDMAX_HOME` 设为该目录。凭证通过继承的 `BUILDMAX_*` 环境变量传入 Worker Pod。如果 `config_map` 为空，Worker Pod 会回退到内置默认值，这几乎不会是你想要的配置。

部署相关改动会在 CI 中运行端到端 kind 检查，创建真实 Worker Job 并验证返回的 Artifact；单元测试继续检查生成的 Job 和清单契约。

检查服务存活：

```bash
curl localhost:5678/healthz   # the process is up
curl localhost:5678/readyz    # its dependencies answer too
```

API 在 `/openapi.json` 提供自描述，在 `/swagger/` 提供可浏览界面。

## Portal

Portal 是静态资源包。运行已发布镜像：

```bash
docker run -p 8080:80 \
  -e BUILDMAX_API_BASE=https://api.example.com \
  ghcr.io/icloudbb/buildmax-portal:<version>
```

`BUILDMAX_API_BASE` 在容器启动时应用，而非构建时，因此同一镜像可用于所有部署。由此有两点要求：

- **浏览器直接调用该 URL。** 它必须是用户机器能够访问的地址，不能是集群内部 Service 名称。
- **服务器的 `cors_origin` 必须指定 Portal 自身的源**，否则浏览器会阻止所有请求。两者应一起配置。

将 Portal 和服务器放在同一主机名的反向代理后即可消除这个问题：设置 `BUILDMAX_API_BASE=/`，Portal 就会调用同源地址，无需允许跨源请求。

镜像标签对应其构建的发行版，因此 `ghcr.io/icloudbb/buildmax-portal:0.1.0` 与 `ghcr.io/icloudbb/buildmax:0.1.0` 配套使用。

也可以自行构建资源包：

```bash
cd portal && npm install && npm run build     # → portal/dist
```

使用任意静态托管服务提供 `portal/dist`。手动构建的资源包在构建时从 `VITE_API_BASE` 获取 API URL，默认是 `http://localhost:5678`。

## 容器

| 镜像 | 内容 |
|---|---|
| `ghcr.io/icloudbb/buildmax` | CLI、服务器和 Worker 二进制 |
| `ghcr.io/icloudbb/buildmax-portal` | nginx 提供的静态前端 |

两者分别通过 `.goreleaser.yaml` 和 `.github/workflows/portal-image.yml`，按发行标签发布。它们由不同的工作流构建，前端失败不会阻塞二进制发布。

`deployment/docker/Dockerfile.buildmax` 从源码构建 Go 二进制，供本地使用；`deployment/buildmax-deploy.yaml` 是可运行的 Kubernetes 清单，包含 namespace、Secret、Deployment、Service 和 Ingress，由 `./make kind up` 用于本地 kind 集群。它自带 MySQL 和 MinIO，并硬编码集群内地址，因此属于开发环境，而非模板。

对于使用现有依赖服务的私有部署，从 [`deployment/production/`](../../../deployment/production/README.md) 开始：其中包含一份纯 YAML 清单和各依赖必须满足的契约。它供阅读和调整，而不是直接应用；使用纯 YAML，正是为了便于转换到集群现有的管理方式。

## 运行边界

Agent 运行时执行模型选择的 shell 命令和文件编辑。请将每个部署视为执行边界：

- 为服务器和 Worker 分配专用、最小权限凭证
- 将 `workspaces_dir` 和 Blob 存储放在重要宿主机路径之外
- 明确决定 Worker 的网络策略；[沙箱](../../../manual/sandbox.md) 可限制 Bash 出站流量。官方 worker 镜像会选择它，本地 CLI/Desktop 默认关闭；worker API/集群策略是独立边界
- 不要把凭证提交到版本控制中的 `server.yaml`
- 清楚所用边界：`local_process` 在同一宿主机上把 Worker 作为服务器子进程运行，属于一个信任域；减少继承内容并不会改变这一点。`k8s_job` 才会隔离服务器与模型选择的代码

漏洞披露见 [SECURITY.md](../../../SECURITY.md)。

## 相关文档

- [Compose 快速入门](compose.md)：单机完整栈，服务器与 Worker 处于同一信任域
- [身份认证](authentication.md)：创建账户和签发登录码
- [本地 kind](local-kind.md)：一条命令启动本地开发集群
- [DigitalOcean](digitalocean.md)：用于 Beta 验证的可销毁外部 DOKS 和 MySQL
- [凭证轮换](credential-rotation.md)：逐一轮换部署凭证及其影响
- [Webhook 参考](../reference/webhook.md)：由外部系统触发运行
