# 配置参考

> **翻译说明：** 本文是[英文原文](../../reference/configuration.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
> **受众：** 用户和运维人员 · **状态：** 当前

BuildMax 通过**数据目录内的 YAML 文件**进行配置，而不是通过一长串环境变量。只有极少数引导阶段的值留在环境变量中，因为它们必须在任何文件被读取之前就已知。

| 文件 | 读取方 | 用途 |
|---|---|---|
| `<BUILDMAX_HOME>/settings.yaml` | CLI、Desktop | 模型、hook、沙箱、日志级别 |
| `<BUILDMAX_HOME>/server.yaml` | Server、Worker | 端口、认证、数据库、存储、worker、Tier 1 模型 |
| `<BUILDMAX_HOME>/policy.yaml` | CLI、Desktop、Worker | 运维人员策略：覆盖 `settings.yaml` 的沙箱设置，以及允许加载哪些 plugin 来源 |
| `<workspace>/.buildmax/hooks.yaml` | CLI、Desktop | 按工作区叠加的 hook 配置，在全局 hook 基础上追加 |
| `<BUILDMAX_HOME>/mcp.json` | CLI、Desktop、Worker | MCP 服务器，与工作区文件合并 |
| `<workspace>/.buildmax/mcp.json` | CLI、Desktop | 按工作区配置的 MCP 服务器；服务器 id 重复时以此为准 |
| `<BUILDMAX_HOME>/plugins/<name>/` | CLI、Desktop、Worker | 一个已安装的本地 plugin，或某个 Space 激活的确切版本，被物化到某次运行范围内的 worker home 中；见 [manual/plugins.md](../../../manual/plugins.md) |
| `<workspaces_dir>/.marketplace/` | Server | 已发布的 plugin 包，用于部署未接入对象存储的情况 |

`BUILDMAX_HOME` 默认值为 `~/.buildmax`。从 [`config-examples/`](../../../config-examples/) 复制起始模板：

```bash
mkdir -p ~/.buildmax
cp config-examples/settings.example.yaml ~/.buildmax/settings.yaml
cp config-examples/server.example.yaml   ~/.buildmax/server.yaml   # server/worker only
cp config-examples/policy.example.yaml   ~/.buildmax/policy.yaml   # operator policy, optional
cp config-examples/mcp.example.json      ~/.buildmax/mcp.json      # MCP servers, optional
```

`mcp.example.json` 携带一个 `_comment` 键，其中保存着自身的说明文档；使用前请删除该键。

## 环境变量

以下是完整列表。`internal/config/env_spec.go` 是权威来源；未在此列出的变量都不会被 BuildMax 读取。

| 变量 | 默认值 | 用途 |
|---|---|---|
| `BUILDMAX_HOME` | `~/.buildmax` | 数据目录；用于定位 `settings.yaml` 和 `server.yaml`。必须是环境变量——在它已知之前，其他任何东西都无法被找到。 |
| `BUILDMAX_SERVER_URL` | — | 该进程用来访问 `buildmax-server` 的地址。为 CLI/Desktop 覆盖 `settings.yaml` 中的 `server_url`，为 worker 覆盖 `server.yaml` 中的 `worker.server_url`。 |
| `BUILDMAX_JWT_SECRET` | — | 覆盖 `server.yaml` 中的 `jwt_secret`。请在部署时注入该值，而不要把密钥提交到文件中。 |
| `BUILDMAX_CORS_ORIGIN` | — | 覆盖 `server.yaml` 中的 `cors_origin`。它必须写明 Portal 所在的来源（origin），也就是部署所选择的宿主端口——Compose 编排栈会从 `BUILDMAX_PORTAL_PORT` 推导出它，因此更改该端口只需改一处而不是两处。 |
| `BUILDMAX_PUBLIC_BASE_URL` | — | 覆盖 `server.yaml` 中的 `public_base_url`：人们打开 BuildMax 所使用的外部可达来源（origin）。Artifact 的公开分享链接由它构建；不设置则保持公开分享关闭。它与 `BUILDMAX_SERVER_URL`（进程用来*访问*服务器的地址）是不同的概念。 |
| `BUILDMAX_WORKER_LLM_TRANSPORT` | — | 覆盖 `worker.llm.transport`（`direct` 或 `buildmax`）。在 task run 直接调用提供商与使用受管网关之间切换。Server 读取该值，并按每次运行把使用哪种传输方式告诉对应的 worker，因此这一选择始终留在 server 端；worker 永远不会拿到这个变量。 |
| `BUILDMAX_LLM_DEFAULT_MODEL` | — | 覆盖 `llm.default_model`——受管运行以及任何未指定模型的调用方最终解析到的目录模型名称。若名称不在目录中，server 会在启动时停止。 |
| `BUILDMAX_CONVERSATION_MODEL_TARGET` | — | 覆盖 `conversation.model_target`——用于 Tier 1 conversation 的目录模型名称或 ID。结合上面两项，仅凭环境变量即可让一个运行中的集群在 mock 模型和某个已 seed 的模型之间切换；`./make kind use-model` 和 `./make kind mock` 正是这样做的。 |
| `BUILDMAX_SANDBOX_ENABLED` | — | 覆盖用户级 `sandbox.enabled`，优先级低于按次运行的 CLI 选项和运维人员策略。接受 `1/true/yes/on` 或 `0/false/no/off`。 |
| `BUILDMAX_SANDBOX_BACKEND_INSTALLED` | — | 不是运维人员设置项。通过 `Dockerfile.buildmax`/`Dockerfile.release` 中的 `ENV` 设置，出现在由这两个镜像构建出的每个容器中；标记该镜像已安装沙箱所需的操作系统层后端（`bwrap`+`socat`），`config.WorkerSandboxSurface` 正是据此判断是否启用 worker 的严格沙箱基线。 |
| `BUILDMAX_TRACE_DISABLED` | — | 为真值时禁用持久化运行 trace。Trace 默认开启。 |
| `BUILDMAX_CREDENTIAL_STORE` | — | 设为 `file` 可将 CLI 或 Desktop 登录的 access token 和 refresh token 保存在 `auth.json` 中，而不是保存在操作系统凭据存储（Keychain、Credential Manager、Secret Service）中。`buildmax login`、`buildmax me` 和 `buildmax doctor` 会报告某次登录实际使用了哪一种。 |
| `BUILDMAX_RUN_TOKEN` | — | 某次 task run 用于访问所有 `/api/worker/*` 路由的凭据。由调度器按每次运行铸造，并放入 worker 进程或 Job pod 中——不是运维人员要设置的值。 |
| `BUILDMAX_RUN_INTERRUPT_GRACE` | — | 被要求停止的 worker 用来汇报其运行产出所能花费的时长。由调度器根据 `shutdown_grace` 按每次分发设置，因此两个时间窗口是嵌套的——不是运维人员要设置的值。 |
| `BUILDMAX_TEST_DSN` | — | 用于 store 集成测试的 MySQL DSN。不设置则跳过这些测试。 |
| `BUILDMAX_CACHE_QUALIFY_PROVIDER` | — | `./make cache-qualify` 使用的提供商，该命令会调用真实的付费提供商。不设置则跳过该测试套件。 |
| `BUILDMAX_CACHE_QUALIFY_MODEL` | — | 该测试套件使用的模型标识符。 |
| `BUILDMAX_CACHE_QUALIFY_API_KEY` | — | 该测试套件使用的凭据。 |
| `BUILDMAX_CACHE_QUALIFY_BASE_URL` | — | 该测试套件的端点覆盖。 |
| `BUILDMAX_CACHE_QUALIFY_SLOW` | — | 包含需要等待保留期过期的验证场景。仅接受真值；这些场景会耗费数分钟的实际时间。 |

### 凭据覆盖

以下每个字段都会覆盖 `server.yaml` 中对应的条目。它们的存在是为了让部署可以从 Kubernetes Secret、Docker secret 或 CI 变量注入凭据，而不必把凭据写入磁盘。未设置的变量不会影响文件中的值。

| 变量 | 覆盖对象 |
|---|---|
| `BUILDMAX_DATABASE_PASSWORD` | `database.password` |
| `BUILDMAX_STORAGE_MINIO_ACCESS_KEY` | `storage.minio.access_key` |
| `BUILDMAX_STORAGE_MINIO_SECRET_KEY` | `storage.minio.secret_key` |
| `BUILDMAX_CONVERSATION_MODEL_API_KEY` | `conversation.model.api_key` |
| `BUILDMAX_COORDINATION_REDIS_PASSWORD` | `coordination.redis.password` |
| `BUILDMAX_OIDC_CLIENT_SECRET` | `oidc.client_secret` |

理想的划分方式是：**`server.yaml` 携带形状和非敏感的值；环境变量携带凭据。** `deployment/buildmax-deploy.yaml` 正是按这种方式组织的——用 ConfigMap 承载文件内容，用 Secret 承载这些变量。

### Worker 会收到什么

一次 task-run worker（无论以本地进程还是 Kubernetes Job 的形式运行）只会拿到它需要读取的变量：

| 变量 | Worker 为什么需要它 |
|---|---|
| `BUILDMAX_HOME` | 本次运行范围内的数据目录 |
| `BUILDMAX_SERVER_URL` | 访问拥有该 task run 的 server |
| `BUILDMAX_STORAGE_MINIO_ACCESS_KEY` / `_SECRET_KEY` | 读写运行状态和 artifact |
| `BUILDMAX_CONVERSATION_MODEL_API_KEY` | 直接调用提供商——当 `worker.llm.transport` 为 `buildmax` 时会被**扣留** |
| `BUILDMAX_SANDBOX_ENABLED`、`BUILDMAX_SANDBOX_BACKEND_INSTALLED`、`BUILDMAX_TRACE_DISABLED` | 运行时开关 |

`BUILDMAX_RUN_TOKEN` 通过另一条路径传给 worker。它不是从 server 继承而来的——上面的过滤逻辑会把它剥离，因此不会带上一个过期的旧值——而是在分发时添加到进程或 pod 中，并指明它所授权的那一次运行。它是 worker 在每个 `/api/worker/*` 路由上出示的凭据，也是这些路由唯一接受的凭据，因此一次运行只能读写它自己的记录。分发时若没有该令牌，运行会在启动时失败；见 [design/worker-run-token.md](../design/Worker运行令牌.md)。

Worker 在读取到 `BUILDMAX_RUN_TOKEN` 后会将其从自身环境中清除，只在内存中保留该值。沙箱本应从子进程中剥离形如密钥的变量，但沙箱默认关闭，因此模型选择执行的 `printenv` 原本会把它打印出来。

`BUILDMAX_JWT_SECRET` 和 `BUILDMAX_DATABASE_PASSWORD` 被刻意扣留。Worker 从不读取它们——它通过 HTTP、凭借自己的 run token 访问 server，从不直接接触数据库——而且它会执行模型选择的 shell 命令，因此持有签名密钥就能让它为任意用户铸造 token，持有数据库密码就能让它获得每个 space 的数据。未被识别的 `BUILDMAX_` 变量同样会被扣留，这样一来，添加到 server 端却尚未就是否发给 worker 做出决定的变量，就会留在 server 一侧。

`internal/config/env_spec.go` 中的 `WorkerNeeds` 是权威来源。在那里标记一个变量，就是让它被发送给 worker 的方式。

### Worker Pod 是如何被限制的

每个 worker Job pod 创建时都没有 service account token，使用 `Localhost` seccomp 配置文件（`deployment/seccomp/worker-bwrap.json`，由一个 `DaemonSet` 分发）、`Unconfined` 的 AppArmor 配置文件、只读根文件系统加一个可写的 `/tmp`，并且除 `SYS_ADMIN` 外的所有 Linux capability 都被移除。以上这些都不可配置：worker 执行的是模型选择的 shell 命令，因此即便提交该 task 的 space 是可信的——驱动这些命令的提示、仓库内容和工具输出却并不可信——它仍被当作运行不可信代码来对待。真正约束这些命令的是 `bwrap` 自身的沙箱，它正是凭借上述 seccomp、AppArmor 和 capability 授权在这个 pod 内部构建起来的；至于为什么需要每一项，见 [`deployment/seccomp/README.md`](../../../deployment/seccomp/README.md)——每一项都是针对真实集群上一次 `Operation not permitted` 失败逐一排查出来的。

该 pod 以 root（uid 0）身份运行，而非非 root：容器运行时赋予非 root pod 的某个 capability（此处是 `SYS_ADMIN`，在更早、后来被回退的一次尝试中是 `SETUID`/`SETGID`）只会落入该 pod capability 的 *bounding* 集合，而在 exec 时永远不会进入其 *effective* 集合——这一点已在真实集群上验证过——而 `bwrap` 需要该 capability 处于 effective 状态而不仅仅是 permitted，才能构建起自己的沙箱。Root 没有这个缺口。因此该 pod 的限制完全来自上述 capability/seccomp/AppArmor 的组合，加上 `bwrap` 自身对 worker Bash 调用的、限定在工作区范围内的沙箱化处理，而不是来自 pod 自身的 uid。

`worker.k8s` 下有一项设置仍由运维人员掌控：

| 设置 | 默认值 | 用途 |
|---|---|---|
| `resources.cpu_request` / `cpu_limit` / `memory_request` / `memory_limit` | 无——必填 | Kubernetes 数量字符串，例如 `500m`、`2`、`512Mi` 或 `4Gi`。在 `k8s_job` 下全部为必填项；BuildMax 不会替你选定数字，因为合适的值取决于该部署所运行的工作内容。 |
| `resources.ephemeral_storage_request` / `ephemeral_storage_limit` | 无——必填 | 限定 worker pod 的本地临时磁盘——包括可写层以及每一个 emptyDir，物化后的工作区、暂存的检查点内容和工具输出都落在这里。该上限同时会作为该 pod 每个 emptyDir 卷的 `sizeLimit`，因此失控的工作区会被干净地驱逐，而不是把节点填满。 |

若某个上限缺失、不是合法的 Kubernetes 数量、为零或负数，或者某个 limit 低于对应的 request，server 会拒绝启动。错误信息会指出需要修改哪个键。这是刻意为之：不受限的 worker pod 会执行模型选择的 shell 命令，一次失控的构建就会拖垮节点上的其他一切；而一个因拼写错误被悄悄丢弃的限制，看起来与真正生效的限制一模一样。

这适用于 `run_mode: k8s_job`。在 `local_process` 下，worker 是 server 的子进程——同一台主机、同一个 uid、同一个文件系统——因此两者从结构上就属于同一个信任域。上面的 `BUILDMAX_*` 过滤逻辑依然适用，但该前缀之外的一切都会从 server 进程继承而来，因此运维人员碰巧导出的某个凭据同样会到达 worker。这两点都不值得单独修复：一个存心去找的 worker，无论拿到什么都能读到 server 的环境变量和 `server.yaml`。单机部署就是在这样的前提下被支持的。需要把 server 与模型选择的代码隔离开的部署，应使用 `k8s_job`，这道边界正是在那里构建的；`local_process` 被刻意不朝着这个方向加固。

这是另一个独立的问题：`local_process` worker 自身的 Bash 命令是否被限制在该次运行的工作区内。答案是肯定的：只要镜像安装了沙箱后端（`BUILDMAX_SANDBOX_BACKEND_INSTALLED`，现已纳入 `WorkerNeeds`，因此 `local_process` worker 被过滤后的环境中也会带上它），`local_process` 就会获得与 `k8s_job` worker 相同的 `SandboxSurfaceWorker` 基线——在 Compose 部署中，server 容器需要与 worker Job pod 相同的 seccomp 覆盖设置，`bwrap` 才能真正构建出该沙箱；见 `deployment/compose/compose.yaml` 中的 `security_opt` 以及 [`deployment/seccomp/README.md`](../../../deployment/seccomp/README.md)。`local_process` 得不到、而 `k8s_job` 能得到的，是 worker 完全作为一个与 server *不同的进程*运行这件事本身。

### 贡献者本地文件：`.local/`

贡献者为自己的机器所做的一切配置，都放在仓库根目录下一个 gitignore 掉的目录里。`./make setup local` 会创建它，并用已提交的模板填充，然后打印出还有哪些内容需要你自己填写；`./make doctor` 会报告它是否存在。重复运行该命令永远不会覆盖你已经编辑过的文件，`.local/README.md` 描述了每个文件，方便站在该目录里的读者查阅。

| 文件 | 读取方 | 模板 |
|---|---|---|
| `.local/env` | `./make` 和 `make.bat`，在运行任何任务之前 | [`.env.example`](../../../.env.example) |
| `.local/settings.yaml` | `./make models`、`./make kind seed` | [`config-examples/settings.example.yaml`](../../../config-examples/settings.example.yaml) |
| `.local/buildmax-secret.yaml` | 没有自动读取方；你自己 `kubectl apply -f` 它，用于你自己的 Kubernetes 部署 | [`deployment/buildmax-secret.example.yaml`](../../../deployment/buildmax-secret.example.yaml) |

有一个本地文件被刻意排除在外。`deployment/compose/.env` 与其 `compose.yaml` 放在一起，因为 Compose 会从那个目录读取它，快速上手流程也是直接在那里运行 `docker compose` 的；本节末尾会介绍它。

`./make` 和 `make.bat` 在运行任何任务之前都会加载 `.local/env`，因此一个本地的 `BUILDMAX_*` 值无需在你的 shell 中导出，就能应用于每个任务。这**仅仅是开发时的便利**——已发布的二进制文件从不读取它，它只读取自己被赋予的环境变量。

已提交的 [`.env.example`](../../../.env.example) 列出了开发者和运维人员任务所使用的可选个人凭据；只填写你会用到的条目。它不会重复 `settings.yaml` 和 `server.yaml` 中已支持的 BuildMax 配置面。只把真正属于你这台机器的内容放进 `.local/env`：

```bash
# Point the local server and worker at a scratch data directory.
BUILDMAX_HOME=./testing-sandbox

# Run the MySQL-backed store tests instead of skipping them.
BUILDMAX_TEST_DSN=root:pass@tcp(127.0.0.1:3306)/buildmax_test?parseTime=true

# Anything from the tables above, for `./make run server`.
BUILDMAX_JWT_SECRET=dev-only-secret
BUILDMAX_DATABASE_PASSWORD=...
```

有两个变量是由任务运行器本身读取的，而不是由 BuildMax 读取：

| 变量 | 默认值 | 用途 |
|---|---|---|
| `BUILDMAX_KIND_CLUSTER` | `buildmaxdev` | `./make kind …` 创建并操作哪一个 kind 集群。每次 `kubectl` 调用都会使用该集群的显式 context。 |
| `BUILDMAX_IMAGE_PLATFORM` | 宿主平台 | `./make kind reload` 的目标平台——例如在 Apple Silicon 上设为 `linux/amd64`。 |

DigitalOcean 验证命令读取以下这些任务运行器变量。其完整生命周期和凭据范围见 [deploy/digitalocean.md](../deploy/digitalocean.md)：

| 变量 | 默认值 | 用途 |
|---|---|---|
| `DIGITALOCEAN_TOKEN` | — | 管理临时的 DOKS 和 MySQL 资源，并读取持久化的 Project 和 VPC。 |
| `SPACES_ACCESS_KEY_ID` | — | 读取持久化的 Spaces bucket，之后用于让 BuildMax 对其进行身份认证。 |
| `SPACES_SECRET_ACCESS_KEY` | — | 该 bucket 范围密钥的私密部分。 |
| `BUILDMAX_OCEAN_PROJECT` | `buildmax-beta` | 要复用的既有 DigitalOcean Project。 |
| `BUILDMAX_OCEAN_VPC` | `buildmax-beta` | 要复用的既有 VPC。 |
| `BUILDMAX_OCEAN_BUCKET` | `buildmax-beta` | 要复用的既有 Spaces bucket。 |
| `BUILDMAX_OCEAN_REGION` | `sgp1` | 既有资源和临时资源共用的区域。 |
| `BUILDMAX_OCEAN_DATABASE_VERSION` | `8.4` | 固定的 DigitalOcean Managed MySQL 版本。 |
| `BUILDMAX_OCEAN_STATE_DIR` | `~/.buildmax/qualification/ocean` | Git 之外、仅所有者可访问的目录，保存状态、计划、provider 和 kubeconfig。 |
| `BUILDMAX_OCEAN_HOSTNAME` | — | `./make ocean deploy` 所服务的完整主机名。 |
| `BUILDMAX_OCEAN_ALLOWED_CIDRS` | — | 允许通过 HTTPS 边缘的客户端网络，逗号分隔；部署时必填。 |
| `BUILDMAX_OCEAN_IMAGE` | 固定摘要 `v0.2.0-alpha.4` | 不可变的 server 与 worker 镜像覆盖。可变标签会被拒绝。 |
| `BUILDMAX_OCEAN_PORTAL_IMAGE` | 固定摘要 `v0.2.0-alpha.4` | 不可变的 Portal 镜像覆盖。可变标签会被拒绝。 |
| `BUILDMAX_OCEAN_EDGE_IMAGE` | 固定的 Caddy 2.10.2 摘要 | 不可变的 HTTPS 边缘镜像覆盖。可变标签会被拒绝。 |
| `BUILDMAX_OCEAN_MODEL_NAME` | `GPT-5.6 Luna` | 验证流程所配置模型的显示名称。 |
| `BUILDMAX_OCEAN_MODEL_PROVIDER` | `openai` | 保存在模型条目上的 provider 标签。 |
| `BUILDMAX_OCEAN_MODEL_API_URL` | `https://openrouter.ai/api/v1` | 该模型调用的 OpenAI 兼容 base URL。 |
| `BUILDMAX_OCEAN_MODEL_ID` | `openai/gpt-5.6-luna` | 发送给 provider 的模型 id。 |
| `BUILDMAX_OCEAN_MODEL_CONTEXT_WINDOW` | `1050000` | 上下文窗口，单位为 token；必须是正整数。 |
| `BUILDMAX_OCEAN_MODEL_CURRENCY` | `USD` | 以下价格所使用的货币单位。 |
| `BUILDMAX_OCEAN_MODEL_INPUT_PRICE` | `0.2` | 每百万 token 的输入价格。 |
| `BUILDMAX_OCEAN_MODEL_CACHE_READ_PRICE` | `0.02` | 每百万 token 的缓存读取价格。 |
| `BUILDMAX_OCEAN_MODEL_CACHE_WRITE_PRICE` | `0.25` | 每百万 token 的缓存写入价格。 |
| `BUILDMAX_OCEAN_MODEL_OUTPUT_PRICE` | `1.2` | 每百万 token 的输出价格。 |
| `BUILDMAX_OCEAN_DATABASE_LOCAL_PORT` | `13306` | 隧道数据库连接监听的本地端口。 |

Compose 编排栈是独立的，不读取 `.local/env`。它使用 `deployment/compose/.env`，该文件由 `deployment/compose/generate-env.sh` 创建，其中包含生成的密钥以及宿主端口 `BUILDMAX_SERVER_PORT` 和 `BUILDMAX_PORTAL_PORT`；`./make compose up` 会在首次运行时生成它。这两个端口是自包含的——`compose.yaml` 会据此推导出 Portal 的 API base 和 server 的 `BUILDMAX_CORS_ORIGIN`。见 [deploy/compose.md](../deploy/compose.md)。

## `settings.yaml` —— CLI 和 Desktop

```yaml
log_level: info                      # debug | info | warn | error | off
server_url: http://localhost:5678    # default offered by `buildmax login`;
                                      # BUILDMAX_SERVER_URL overrides it

models:                              # first entry is the default model
  - model: openai/gpt-3.5-turbo
    name: GPT-3.5-turbo
    api_url: https://openrouter.ai/api/v1
    api_key: your-api-key
    context_window: 16385            # 0 = built-in default (32000)
    call_timeout: 300                # seconds; 0 = default (300)

  # - model: claude-sonnet-4-5       # Anthropic's own endpoint, not a gateway
  #   name: Claude Sonnet 4.5
  #   provider: anthropic
  #   api_url: https://api.anthropic.com
  #   api_key: your-api-key
  #   context_window: 200000
  #   max_tokens: 8192               # 0 = built-in default (8192)

default_model: GPT-5.6 Luna          # which entry a new session starts with;
                                     # omit and the first one is used

hooks: {}                            # see guide/hooks.md
sandbox: {}                          # see guide/sandbox.md
```

| 键 | 默认值 | 说明 |
|---|---|---|
| `log_level` | `info` | 日志只写入 `<BUILDMAX_HOME>/logs/buildmax.log`，从不输出到终端，以保持 TUI 界面干净。 |
| `server_url` | — | 仅用作 `buildmax login` 的提示默认值；`BUILDMAX_SERVER_URL` 会覆盖它。 |
| `models[]` | — | CLI 在未登录状态下可运行的模型。用 `--model <id or name>` 为某次运行单独选择一个。 |
| `default_model` | 第一个条目 | 新会话默认使用哪个条目，按名称或模型 id 指定。仅在未登录状态下生效；部署会指定自己的默认值。 |
| `models[].provider` | `openai_compatible` | 该端点所使用的通信协议——见下文。 |
| `models[].max_tokens` | `0` | 单次响应的上限。`0` 表示使用协议自身的默认值；`anthropic` 要求必须填写该字段，因此在那里 `0` 会发送内置的 8192。 |
| `models[].reasoning` | `off` | 模型在回答前进行多少推理：`off`、`low`、`medium` 或 `high`——见下文。对 `openai_compatible` 无影响，该协议不携带任何推理相关字段。 |
| `models[].cache_control` | `auto` | 哪些调用会请求提供商缓存该请求中稳定的前缀部分，以及缓存多久——见下文。 |
| `models[].pricing` | — | 该模型的计费方式，使运行结果能够报告花费——见下文。 |
| `models[].integration` | — | 一个经过认证的 OpenAI 兼容网关。目前没有任何网关通过认证，因此任何取值目前都会被拒绝。 |
| `models[].vision` | `false` | 该模型是否接受图像输入。保持关闭时，工具返回的图像会以文本描述形式呈现，而不会被发送。 |
| `models[].keep_alive` | — | 本地运行时在一次调用后保持模型加载状态的时长：可以是像 `30m` 这样的时长、`0` 表示立即卸载、`-1` 表示常驻内存。只有 `ollama` 会读取它。 |

### 模型提供商

`provider` 命名的是端点所使用的**通信协议**，而不是厂商。该用哪个值取决于端点的 API，而不是模型出自谁手：通过 OpenRouter 提供的 Claude 是 `openai_compatible`，而由 `api.anthropic.com` 提供的 Claude 是 `anthropic`。

| 取值 | API | 典型端点 |
|---|---|---|
| `openai_compatible` | OpenAI Chat Completions | OpenRouter、LiteLLM、vLLM、LM Studio 及其他兼容网关。这是默认值，也是该选项出现之前写下的每个条目仍在沿用的值。 |
| `openai` | OpenAI Responses | OpenAI 自己的 `api.openai.com`。以无状态方式运行：BuildMax 每次调用都会发送整段对话，服务端不保存任何内容。 |
| `anthropic` | Anthropic Messages | `api.anthropic.com` |
| `ollama` | Ollama 自己的 `/api/chat` | 本地的 Ollama 守护进程，默认地址为 `http://localhost:11434`。不需要 `api_key`。见[使用 Ollama 的本地模型](#使用-ollama-的本地模型)。 |

文本、工具调用、流式输出和 token 用量统计，在这四者上表现一致。推理、提示缓存和图像输入也是如此，下文会分别介绍——不同之处只在于每种协议各自能做到多少。

### 推理

`reasoning` 设定模型在回答前进行多少推理，并把这段推理延续下去，因此一次跨越多个工具调用的运行不会在每一步都从头开始，而是保持思路的连贯。级别分为 `off`（默认）、`low`、`medium` 和 `high`。

| 提供商 | 除 `off` 外的级别会做什么 |
|---|---|
| `openai_compatible` | 什么都不做。该协议没有推理状态。 |
| `openai` | 设置推理强度（reasoning effort），请求加密的推理内容，并在后续轮次中重放它。 |
| `anthropic` | 以对应强度启用自适应扩展思考（extended thinking），并重放思考块。 |
| `ollama` | 为支持思考的模型打开思考功能。该协议的开关没有级别区分，因此 `low`、`medium`、`high` 都表示开启，并且不携带任何可重放的状态。 |

默认关闭，因为它会改变一次调用的花费，而且一些较旧的模型会直接拒绝这样的请求。为不支持它的模型开启会导致调用以提供商自身的错误失败，而不是悄无声息地什么都不做。无法识别的级别会在发出任何调用之前就被拒绝。

推理内容本身从不进入对话记录。BuildMax 将其作为不透明状态与 assistant 消息一起存储，并原样发回、不做读取——签名覆盖了内容本身，因此编辑它比省略它更糟。状态会标注产生它的协议，因此在不同 provider 下继续一个会话时，会丢弃该 provider 无法使用的部分，同时保留其余一切。CLI 的会话文件和 Portal 的 conversation 都会持久化它，受管网关也会携带它，因此一次在重启后恢复的运行能够保持其连贯性。

### 提示缓存

`cache_control` 请求提供商缓存请求中在多次调用之间不变的部分——工具定义和系统提示——从而让一次运行接下来的部分能以更低的费率使用它们。

```yaml
models:
  - name: sonnet
    provider: anthropic
    model: claude-sonnet-5
    cache_control:
      mode: auto             # auto (default), off, force
      ttl: provider_default  # provider_default (default), 5m, 1h
```

`mode` 决定**哪些调用**会发起缓存请求：

| Mode | Agent 轮次 | 一次性调用（标题、压缩、探测） |
|---|---|---|
| `auto`（默认） | 请求 | 不请求 |
| `off` | 不请求 | 不请求 |
| `force` | 请求 | 请求 |

正是这种区分让 `auto` 可以安全地作为默认值。写入一条缓存记录的成本高于不缓存，只有当后续调用真正读取它时才能回本，因此在同一个稳定前缀上多次调用的运行会因此受益，而单次短调用则会因此吃亏。一次 Agent 轮次的前缀会在下一次迭代中再次发出；而一个已生成的标题永远不会。`force` 适用于调用方知道某些运行时无法察觉的信息的场景。

`ttl` 用于选择保留时长，且仅在提供商有相应文档说明时才生效。在未记录该项的提供商上使用非 `provider_default` 的值，会在启动时被拒绝，而不是被发送后遭到忽略。

| 提供商 | 请求携带的内容 | 保留时长 |
|---|---|---|
| `anthropic` | 在工具和系统提示之后、以及请求末尾设置断点。除非请求中指明位置，否则不会缓存任何内容。 | `provider_default`、`5m`、`1h` |
| `openai` | 一个限定范围的 `prompt_cache_key`。Responses 会自行进行缓存，因此该键并不能开启缓存——它只是说明前缀属于哪个分桶。 | `provider_default`、`24h` |
| `openai_compatible` | 什么都不携带。使用该协议并不意味着承诺实现其缓存字段，未经测试的网关可能会拒绝或忽略它们。 | 仅 `provider_default` |
| `ollama` | 什么都不携带。本地运行时会在多次调用之间自行复用其缓存，没有请求侧的控制，也没有计数可供报告。 | 仅 `provider_default` |

保留时长的表述是按提供商各自定义的，而不是全局统一的：`5m` 和 `1h` 对 Anthropic 有意义，对 Responses API 毫无意义；`24h` 则反过来。在未记录该值的地方请求它，会在启动时被拒绝，而不是被发送后遭到忽略。

`prompt_cache_key` 是派生出来的，而不是配置出来的。它是一个不透明的摘要，由凭据、模型、space（仅限受管调用）以及系统提示和工具定义的指纹组成——这些都必须匹配，提供商才能命中缓存。它不以可读形式携带其中任何一项内容，其中任意一项发生变化时它都会跟着变化，并且从不写入账本、trace、日志或 CLI。被授予同一模型的两个 space 会共享同一份凭据，键中的 space 部分正是用来把它们各自的提示分隔在不同分桶中的。

`mode: force` 在任何不接受缓存指令的提供商上都会在启动时被拒绝。若将其当作“完全不缓存”来处理，就等于回答了一个没人问过的问题。`mode: auto` 在任何地方都可以接受，因为大多数模型就是这种情况。

缓存的 token 会被报告为 `cache_read_tokens` 和 `cache_write_tokens`，它们是对 prompt 计数的**细分**，而不是额外叠加的部分。如果一份花费报告把它们和 `prompt_tokens` 简单相加，就会把同一批 token 重复计算两次。

在任何能看到一次运行 token 情况的地方都能看到它们：CLI 打印一行 `Cache(read/write)`，并在 TUI 状态栏中显示相同的数字，`--format json` 把它们放在 `usage` 之下，运行 trace 会记录它们，会话文件保存按会话累计的总量，受管部署会把它们记录在 `llm_call` 账本行上，供 Portal 的运行花费视图使用。

以上每一处都只会在提供商实际报告了细分数据时才会展示。大多数提供商完全不报告，若始终显示一个 `0 / 0`，会被误读为“测量到零”，而不是“未测量”。

### 模型计费

`pricing` 是某个模型的收费方式，使一次运行能够说明其花费。没有它时，一次运行会把花费报告为 `unavailable`，而不是零——BuildMax 并不知道任何提供商的实际收费，把猜测数字包装成结果比不给出结果更糟。

```yaml
models:
  - name: sonnet
    provider: anthropic
    model: claude-sonnet-5
    pricing:
      currency: USD
      input_per_mtok: "3.00"
      cache_read_per_mtok: "0.30"
      cache_write_per_mtok: "3.75"
      output_per_mtok: "15.00"
```

费率是按每百万 token 计价的十进制字符串，按提供商公开发布的方式书写，以便配置的值能不经计算就与官方价目表核对。这四项之所以分开，是因为缓存对它们的定价不同：缓存读取比全新输入更便宜，缓存写入则更贵，这正是缓存需要权衡取舍、而非白拿的全部原因。

价目表必须完整到值得信赖的程度。有费率却没有 `currency`，或有 `currency` 却没有费率，都会在加载时被拒绝——由半份价目表拼凑出的估算看起来权威，实则不然。费率为 `"0"` 是一个真实的价格，会被接受。

花费出现的位置：

| 位置 | 展示内容 |
|---|---|
| CLI | 一次运行后的 `Cost(session)` 行，若缓存带来了节省，也会显示节省了多少 |
| CLI `--format json` | `usage.cost`，以该货币的纳单位（nano-units）表示 |
| 会话文件 | 会话运行过程中累积的总计 |
| 运行 trace | `llm_end` 上记录每次调用自身的花费，`run_end` 上记录整次运行的花费——不仅是总数，还能看出哪一轮花费高 |
| Portal 运行视图 | 每次运行的预估花费，以及相对未缓存基线所节省的部分 |

会话总计是按轮次逐步累加的，而不是在读取时重新计算，因为模型——以及相应的费率——可能在会话进行中发生变化。若在之后按当时的配置重新推算总计，会用一个不同的价格去重新计算已经付过费的轮次。当会话的某一部分无法计价，或出现第二种货币时，总计会被标记为“部分”（partial），而不是悄悄低估这次运行的花费。

对于受管部署，运维人员通过 `buildmax-server model add` 上的 `--currency`、`--input-price`、`--cache-read-price`、`--cache-write-price` 和 `--output-price`，为每个目录模型设置这四项费率。调用被接受时，当时生效的费率会被复制到对应的 `llm_call` 行上，因此重新为某个模型定价不会改写某个 space 已经花费的记录。

只有当缓存确实带来节省时，才会报告节省。一次运行写入了缓存却没有任何调用读取它，实际花费比不缓存时更高，这会被如实展示为它实际花费的数字，而不是被展示成一次小小的收益。

### 图像输入

`vision: true` 表示该模型接受图像。这一点很重要，因为 MCP 服务器可能会返回图像——一张截图、一张渲染出的图表——而接下来会发生什么，取决于模型能否读取它。

| `vision` | 模型收到的内容 |
|---|---|
| `false`（默认） | 一行说明返回内容的文本，例如 `(image: image/png, 43.2 KB)`。图像本身不会被发送。 |
| `true` | 同样的文本，加上图像本身。 |

默认关闭，是因为不支持图像的模型会**拒绝**携带图像的请求，而不是忽略它。两种情况下都会发送一个可用的工具结果，因此打开它是在声明一种能力，而不是修复一个问题。

图像落在哪里取决于协议：`anthropic` 会把它放进工具结果内部，而 OpenAI 系协议和 `ollama` 做不到这一点，会紧随其后把它作为一条简短的 user 轮次发送。受管部署通过目录模型上的 `image_input` 能力来声明这一点。

### 使用 Ollama 的本地模型

`provider: ollama` 针对本地的 [Ollama](https://ollama.com) 守护进程运行：没有密钥、没有网络请求、没有账单。用下面的方式写入该条目：

```bash
buildmax init --ollama          # configure a model the daemon already holds
buildmax models --local         # list what is installed, and what it can do
buildmax doctor                 # daemon up? model pulled? can it call tools?
```

它写入的条目不带 `api_key` 这一行，因为没有凭据需要保存：

```yaml
models:
  - model: qwen3:8b
    name: Qwen3 8B (local)
    provider: ollama
    api_url: http://localhost:11434   # the daemon root, not its /v1 endpoint
    context_window: 32000
```

**对本地守护进程要使用 `ollama`，而不是 `openai_compatible`。** 同一个守护进程还在 `/v1` 上提供一个 OpenAI 兼容端点，但该端点无法设置上下文窗口：运行时随后会套用自己的默认值，并截断较长的提示而不是拒绝它。被截掉的是请求的*前部*——系统提示和工具定义——因此模型会停止调用工具，转而开始描述它打算做什么。`provider: ollama` 会在每次调用时都发送该窗口值，因此 BuildMax 用来裁剪历史记录的数字和守护进程实际使用的数字是同一个。

`context_window` 就是这个数字。留空不设置时，BuildMax 会询问守护进程该模型的训练窗口是多少，并取其与内置默认值中较小的一个，因为满长度的窗口可能超出机器所能分配的内存。调大它只需改一处；`buildmax doctor` 会把模型的最大值和当前配置值并排打印出来。

本地模型可能出错的两件事，都会由 `doctor` 报告并给出对应的修复命令：模型未被拉取（`ollama pull <model>`），或者它完全无法调用工具，这一点无论怎么调整提示都无济于事。请在 `buildmax models --local` 中选择一个能力包含 `tools` 的模型。

其余一切的行为与别处相同：`max_tokens`、`vision` 和 `reasoning` 含义不变，`cache_control` 不起作用，因为本地运行时不接受缓存指令；`keep_alive` 控制守护进程在两次调用之间将模型保留在内存中的时长——在重新加载一个大模型比一轮对话本身更耗时的机器上，这个值值得设置。

一个部署同样可以提供本地模型：在目录目标上使用 `--provider ollama`，或在 `server.yaml` 的 `conversation.model` 下使用 `provider: ollama`，两种情况都不需要凭据。见[部署中的本地模型](#部署中的本地模型)。

### 受管模型

登录会把这台机器切换到某个部署的模型，登出则切回：

```bash
buildmax login        # models now come from that deployment
buildmax models       # what it offers, and that prompts go there
buildmax logout       # back to the models in settings.yaml
```

这方面没有什么需要配置的。部署持有提供商凭据，其目录在每次启动时被拉取，因此 `settings.yaml` 只描述未登录会话所运行的模型。部署提供的每个模型都对其每个用户可用——space 是协作边界，而不是模型授权边界。

凭据从不写入 `settings.yaml`。它来自 `buildmax login`，且只使用针对当前所调用 server 的那次登录——不匹配时会直接失败，而不是把 token 发给某个被指名的主机。

某个模式内的模型选择是按名称或模型 id 的首次匹配，`default_model` 指定新会话默认使用哪个条目。在受管模式下，由部署指定自己的默认值。

在依赖这一点之前，有三件事值得了解：

- **两种模式从不混合，也互不兜底。** 已登录的会话只能看到该部署的模型；未登录的会话只能看到 settings.yaml。当 server 宕机时，不会悄悄退化为本地调用，因为那样会把本应受治理的流量转向个人提供商密钥——该会话会拒绝启动并给出说明。`buildmax logout` 才是通往本地模型的途径，这是一个主动决定，而不是一种兜底行为。
- **登录会自动续期，直到无法续期为止。** access token 会在每次即将使用一个过期 token 的调用之前自动刷新，因此长期存在的会话无需再次输入登录码就能继续工作。当 refresh token 本身过期，或其会话被撤销时，会话会停止，并要求你重新登录或登出。
- **Worker 跟随部署，而评估框架始终保持直连。** 一次 task-run worker 使用 `worker.llm.transport`：`buildmax` 会给它一个仅限本次运行的凭据、不给任何提供商密钥，而 `direct` 会给它部署已配置好的提供商访问权限。评估流程始终保持直连，这样评估结果就不会随部署的目录或配额而变化。

在受管模式下，提示、工具 schema 和工具结果都会经过 server。这正是它的设计意图，也确实改变了你的数据流向——这也是为什么 `buildmax models`、模型选择器和 TUI 页脚都会标明当前所处的模式。
| `hooks` | 空 | 生命周期 hook。参考文档：[manual/hooks.md](../../../manual/hooks.md)。 |
| `sandbox` | 关闭 | Bash 沙箱化。参考文档：[manual/sandbox.md](../../../manual/sandbox.md)。 |
| `tools.permissions` | 空 | 按工具的审批规则。见下文。 |
| `agent.max_parallel_tools` | `4` | 一条模型消息中的只读工具调用最多可以同时运行多少个。范围 1-16；设为 1 表示关闭该功能。 |
| `agent.max_iterations` | `200` | 一次运行停止之前，一次提示最多可以调用模型多少次。范围 1-5000。 |
| `agent.turn_digest.recap` | `true` | 在回复下方打印一行暗淡的摘要，说明该轮做了什么。 |
| `agent.turn_digest.suggest` | `true` | 当一轮以向你提问结束时，把可能的答案以幽灵文字的形式提供出来。 |

### `tools.permissions`

BuildMax 会在会更改某些内容的工具调用之前发起询问，前提是所在界面上有人能够回答——即 CLI TUI 和 Desktop。开箱即用的情况下，`Write`、`Edit`、`Task` 以及非只读的 MCP 调用会弹出询问；只读工具不会，而 `Bash` 遵循它自己的风险分类器，而不是所属类别的默认行为。委派给只读 agent 类型（例如 `explore`）的 `Task` 被视为只读，不会弹出询问——它只能触及那些原本自己单独调用时也不会弹出询问的工具。

设置一条规则来改变这一行为：

```yaml
tools:
  permissions:
    Write: allow                        # stop asking before file writes
    Task: ask
    Bash: deny                          # no shell at all
    "CallMcpTool:github/*": allow       # trust one server's tools
    "CallMcpTool:jira/delete_issue": deny
```

| 字段 | 含义 |
|---|---|
| key | 一个工具名，或者工具加上它所分发到的目标，可带一个末尾的 `*`。大小写不敏感。 |
| value | `allow`、`ask` 或 `deny`。无法识别的取值会被忽略，`buildmax tools status` 会将其列出。 |

最匹配的规则优先：先是精确匹配的目标，然后是最长匹配的模式，最后是裸工具名。

有两个限制值得了解：

- **`allow` 只是关闭了该类别的询问，而不是关闭安全检查。** 读取敏感路径和运行高风险 shell 命令仍然会询问。只有 `deny` 的优先级高于这些检查。
- **`ask` 意味着必须有人来查看**，因此在没有人的界面上——print 模式、worker、Portal conversation——该调用会被直接拒绝，而不是被执行。

回答一次询问时选择 `a`，会在本次会话剩余时间里允许该工具，而不需要写一条规则。会话级授权保存在内存中，进程退出后即消失。

运行 `buildmax tools status` 可以查看每个工具的分类、其最终生效的动作，以及是哪一层做出的决定。设计文档：[design/tool-permissions.md](../design/工具权限.md)。

### `agent.max_parallel_tools`

当模型在一条消息中请求多个工具调用时，BuildMax 可以同时运行它们：

```yaml
agent:
  max_parallel_tools: 4     # 1 disables it; range 1-16
```

只有工具自身声明为只读的调用才会并发——`Read`、`Glob`、`Grep`、`Skill`、`WebFetch`，以及委派给只读 agent 类型（例如 `explore`）的 `Task`。写操作、shell 命令、可写的 `Task`，以及 MCP 调用始终单独运行，且调用永远不会被重新排序，因此无论该设置为何，一批调用的含义都是一样的：一次运行产生的消息历史在任何限制值下都完全相同。`buildmax tools status` 会显示哪些工具是只读的。

该限制在子 agent 内部同样生效，因此一次委派出去的探索会自行调度自己的读取操作，而不是逐个串行执行。

在读密集、存储较慢或有大量 `WebFetch` 调用的场景下可以调高它。调到 1 可以让一次运行严格复现为一次只执行一个调用。设计文档：[design/parallel-tool-execution.md](../design/并行工具执行.md)。

### `agent.max_iterations`

一次提示会运行模型、执行它请求的工具，再把结果交回模型，如此往复，直到模型给出回答而不是再调用工具为止。该设置限定这个过程最多可以循环多少次：

```yaml
agent:
  max_iterations: 200       # range 1-5000
```

达到上限的运行会以 `agent: max iterations exceeded` 停止，并以退出码 `7` 退出——这是它自己专属的代码，方便调用方分辨是预算耗尽还是提供商出错。已经完成的工作会保留：最后一次迭代是完整跑完的，因此它所做的文件编辑和执行的命令都已经落盘。

对于长时间无人值守的任务——例如一次通宵作业或一次基准测试——可以调高它，此时没有人在旁边说“继续”。调低它可以限定单次提示能消耗掉你凭据的额度上限。`buildmax --max-iterations N` 为单次运行设置该值，并且优先级高于该文件中的设置。子 agent 拥有自己更小的上限，任何一侧的设置都不会提高另一侧的上限。

### `agent.turn_digest`

一轮结束时，CLI TUI 和 Desktop 可以额外花一次小规模的模型调用来描述这一轮做了什么：

```yaml
agent:
  turn_digest:
    recap: true             # dim summary of the turn, printed under the reply
    suggest: true           # predicted answer offered as ghost text; tab accepts
```

在 TUI 中，回顾摘要会以暗淡的 `❯❯` 行出现在滚动记录中；在 Desktop 中，它会作为一段暗淡的旁注出现在该对话串的末尾。建议内容出现在输入框内部，呈灰色，且仅在输入框为空时显示：按 `tab` 接受它并按 `enter` 发送，或者直接开始输入以忽略它。

这两者都不属于对话内容的一部分。模型在之后的轮次中永远看不到某次回顾摘要或某个建议——它们是写给你看的，用完即弃。

在无法产生任何内容的轮次上，该调用会被跳过：没有运行任何工具且回答简短的轮次不会生成回顾摘要，以没有向你提问结束的轮次也不会生成建议。它所花费的部分会计入该会话的用量——TUI 中的 `/info`，Desktop 中的状态栏。将任意一个键设为 `false` 即可单独关闭对应的一半功能，两者都设为 `false` 则该轮结束时完全不会产生额外调用。

## `server.yaml` —— Server 和 Worker

```yaml
log_level: info
port: 5678
jwt_secret: ""                       # inject via BUILDMAX_JWT_SECRET in production
# allow_signup: true                 # default false; accounts are created with `buildmax-server user create`
# local_login: all                   # all（默认）| system_admins（应急通道）| off；管控原生密码/登录码登录
# oidc:                              # 基于 OpenID Connect 的企业登录（首选 Okta）；见 docs/deploy/authentication.md
#   enabled: true
#   display_name: Okta
#   issuer: https://example.okta.com  # https；唯一的 URL 信任根
#   client_id: 0oaExampleClientId
#   client_secret: ""                 # 通过 BUILDMAX_OIDC_CLIENT_SECRET 注入
#   provisioning: jit                 # jit（默认，需要 allowed_email_domains）| existing_only
#   allowed_email_domains: [example.com]
#   session_max_age: 12h              # SSO 会话上限；默认 12h
access_token_ttl: 15m                # signed; the server checks the session it names each request, so this is the max replay window
refresh_token_ttl: 720h              # a stored row, so a session can be revoked before it expires
refresh_rotation_grace: 30s          # window for processes sharing one credentials file to refresh at once
session_absolute_ttl: 2160h          # hard ceiling on a login's life regardless of refresh; reaching it needs a new sign-in
shutdown_grace: 25s                  # whole budget for an orderly stop; keep below the orchestrator's kill deadline
cors_origin: http://localhost:5173   # or inject via BUILDMAX_CORS_ORIGIN where the Portal's port is chosen
public_base_url: ""                  # externally reachable origin for artifact share links; empty keeps sharing off
workspaces_dir: /data/buildmax/workspaces
default_quota_tier: free_trial

conversation:                        # Tier 1 model used by the Portal agent loop
  model:
    model: openai/gpt-4o
    api_url: https://openrouter.ai/api/v1
    api_key: your-api-key
    context_window: 128000
  # provider: openai_compatible      # wire protocol; see settings.yaml above
  # max_tokens: 0                    # cap on one response; 0 = protocol default
  # model_target: "Claude Sonnet 5" # a catalog id or model name for Tier 1 instead

# llm:                               # catalog is in the DB; this names a default
#   default_model: Fast              # a --name from `model list`

database:                            # MySQL
  host: localhost
  port: 3306
  user: buildmax
  password: buildmax
  name: buildmax                     # created on first start if it is missing

webhook:
  message_path: message              # JSON path to the prompt in the request body
  user_id: webhook                   # fallback identity for webhook-created runs

worker:
  binary: buildmax-worker
  run_mode: local_process            # or k8s_job
  server_url: http://127.0.0.1:5679  # the worker control listener below, not
                                      # the public port; BUILDMAX_SERVER_URL
                                      # overrides it
  allow_insecure_http: true          # required for a k8s_job over http://
  # server_ca_file: ""               # CA for the worker listener's certificate
  # client_cert_file: ""             # optional native mTLS client identity
  # client_key_file: ""
  k8s:
    namespace: buildmax
    image: buildmax:local
    config_map: buildmax-config      # ConfigMap holding server.yaml for worker pods
    home_dir: /buildmax              # BUILDMAX_HOME inside a worker pod

worker_api:                          # the internal listener serving /api/worker/*
  listen: 127.0.0.1:5679             # loopback by default; :5679 on Kubernetes
  tls:
    cert_file: ""                    # server certificate; empty serves plain HTTP
    key_file: ""
    client_ca_file: ""               # optional native mTLS

# audit:                             # governance trail retention
#   retention_days: 365              # default 0 — keep every event forever

storage:
  persist_backend: local_fs          # or minio — space uploads
  artifact_backend: local_fs         # or minio — artifact content
  max_artifact_mb: 0                 # per-file upload cap; 0 uses the default
  artifact_share_ttl_hours: 0        # public share link lifetime bound; 0 uses the default (30 days)
  artifact_purge_after_days: 0       # 0 — reclaim a deleted artifact's bytes
                                     # on the next hourly sweep
  minio:
    endpoint: http://localhost:9000
    region: us-east-1
    access_key: minio
    secret_key: minio123
    bucket: bmstore
    prefix: workspaces
```

一个可运行的 server 必须具备：`jwt_secret`（或 `BUILDMAX_JWT_SECRET`）和 `database`。其余一切都有适用于本地开发的可用默认值。Worker 本身不需要任何凭据——`jwt_secret` 正是用来签发 server 在分发时交给它的 run token 的。

这两个 token 的有效期并不可以互换。Access token 是签名后从不落盘的，但 server 会在每个请求上解析它所指名的持久 Session（`auth_session`），因此登出、管理员撤销和账户禁用会在 `access_token_ttl`（默认 **15m**）之内生效，而不是等到 token 自身过期。Refresh token 是属于该 Session 的一行数据库记录，因此 `refresh_token_ttl` 是一次 Session 可以被续期多久。`session_absolute_ttl`（默认 **90 天**）为一次登录的整体寿命设定上限，无论其 refresh token 轮换多频繁：越过该上限后，Session 即失效，用户需重新登录。见 [deploy/authentication.md](../deploy/authentication.md)。

`shutdown_grace` 是有序停止 server 的整体预算，默认是 **25s**。收到 SIGINT 或 SIGTERM 时，server 会先停止报告就绪状态，以便负载均衡器将其摘除，然后结束正在监视某次运行的流，让 Portal 转而在别处重新订阅，再排空已经接受的请求，最后停止其后台循环。各个阶段的时长都是从这一个数字推导出来的，而不是逐项单独配置的。

请把它设置得比任何“超时就直接杀掉进程”的机制更短——Kubernetes 上的 `terminationGracePeriodSeconds`、systemd 下的 `TimeoutStopSec`——也包括任何 `preStop` hook。[`deployment/`](../../../deployment/) 下的参考清单文件把两者放在一起设置。设计文档：[design/graceful-shutdown.md](../design/优雅关闭.md)。

人们使用电子邮件地址和密码登录。`allow_signup` 默认是 **false**，因此没有人可以自行注册；账户由 server 端创建后，把登录码交给对方，对方兑换该登录码后再设置自己的密码——见 [deploy/authentication.md](../deploy/authentication.md)：

```bash
buildmax-server user create alice@example.com
buildmax-server user login-code alice@example.com
```

同样的登录码也是忘记密码的人重新登录的方式。登录尝试没有限流；在把 server 暴露到不受信任的网络之前，请先阅读该文档中的警告。

部署也可以用 `oidc` 块启用基于 OpenID Connect 的企业登录（首个支持的提供方为 Okta），并用 `local_login`（`all`、`system_admins` 或 `off`）独立于 SSO 管控原生登录。客户端密钥通过 `BUILDMAX_OIDC_CLIENT_SECRET` 注入。配置方法与账号关联规则见 [deploy/authentication.md](../deploy/authentication.md)。

Worker 读取同一份 `server.yaml`，至少需要 `worker.server_url`（或 `BUILDMAX_SERVER_URL`）、`workspaces_dir` 以及 `storage` 配置块——它直接与对象存储通信，而不是通过 server 代理。

Server 对外暴露两个 HTTP 监听端口。`port` 上的公开端口服务于 Portal、用户 API、webhook、健康检查和 OpenAPI。Worker 控制 API（`/api/worker/*`）只在 `worker_api` 监听端口上提供服务，该端口默认绑定在 `127.0.0.1:5679`，这样一次意外的部署也不会开放任何新的集群端口；Kubernetes 部署会将其绑定到 `:5679`，并用自己的内部 Service 加以封装。`worker.server_url` 必须指向这个 worker 监听端口，而不是公开端口——即便携带有效的 run token，公开端口对 worker 路由也只会返回 `404`。两个监听端口必须使用不同的端口号，若两者冲突，或者 TLS 密钥对只设置了一半，server 会拒绝启动。

设置 `worker_api.tls.cert_file` 和 `key_file` 可以让 worker 监听端口以 TLS 提供服务；其证书必须携带 worker 用来校验的内部 Service DNS 名称。Worker 会用 `worker.server_ca_file`（留空时使用系统根证书）构建出一个 HTTP 客户端，并在每次调用时据此校验 server 身份——没有“跳过校验”这种不安全模式，因此错误的主机名或不在该 CA 范围内的证书都会被拒绝。明文 HTTP 对 `local_process`、Compose 和 kind 开发环境依然可用；若某个 `k8s_job` 的 `server_url` 是 `http://`，除非设置了 `worker.allow_insecure_http`，否则会在启动时被拒绝，因为 `.cluster.local` 和回环地址只是路由层面的事实，并不能证明网络是保密的。设置 `worker_api.tls.client_ca_file`（以及 worker 端的 `client_cert_file` / `client_key_file`）会在 run token 之外额外开启可选的原生 mTLS。

在 Kubernetes 上，参考清单文件用两个 Service 分别对外暴露这两个监听端口——`buildmax-api`（公开，位于 Ingress 之后）和 `buildmax-worker-api`（内部 `ClusterIP`，端口 5679）——并配有一条 `NetworkPolicy`，只允许打有 `app.kubernetes.io/name: buildmax-worker` 标签的 pod 访问 worker 端口。Worker API 的 CA 证书通过 `worker.k8s.ca_config_map` 下发给 worker pod，即挂载为只读、路径为 `worker.server_ca_file` 的一个 ConfigMap。Ingress 只指向 `buildmax-api`，因此 worker API 永远不会暴露到公网。见 [design/worker-api-network-boundary.md](../design/Worker API网络边界.md)。

`storage.max_artifact_mb` 限定单个 artifact 上传的大小上限。默认值为 **0**，此时使用内置的 100 MB 限制。它是按单个文件设定的限制，而不是 space 的存储配额：配额由 space 配额档位上的 `max_storage_bytes` 决定，二者回答的是不同的问题——一千个小文件可以逐一通过这个上限检查，却仍然可能填满配额。某个档位若把 `max_storage_bytes` 留在 **0**（已 seed 的档位就是如此），就等于没有任何配额限制；把它设为某个值，可以让 BuildMax 拒绝会使某个 space 超出该配额的上传，返回与运行数或 token 数限制相同的 429 错误。

`storage.artifact_purge_after_days` 延迟回收一个已删除 artifact 所占用的字节。默认值为 **0**，即在下一次每小时的保留清扫中回收：删除一个 artifact 会立即在授权边界上生效，之后继续保留该对象只是成本和风险，而不是安全保障。只有当你需要给自己对象存储自带的工具留出一个恢复窗口时，才设置具体天数——BuildMax 本身不提供撤销删除的功能，被回收的 artifact 也无法在其原来那个不透明的引用下被恢复。

同一个清扫任务也是唯一读取 artifact 过期时间的地方。带有过期时间的 artifact 到期后会被打上删除标记（tombstone），记为一条 `artifact.expired` 事件并指明该 artifact，其字节随后会像任何其他删除一样进入宽限期等待。每一次实际回收了内容的清扫，都会写入一条 `artifact.purged` 事件，记录数量和字节数。除了对失败上传的回滚之外，BuildMax 中没有其他任何地方会移除 artifact 内容。

`audit.retention_days` 使审计轨迹中的事件过期。默认值为 **0**，即保留全部记录：尚未选定保留策略的部署，就等于尚未决定要丢弃证据。设置该值后会启动一次每小时的清扫，移除超出窗口期的旧事件，每一次实际移除了内容的清扫都会写入一条 `audit.pruned` 事件，记录被移除的范围和数量——这样一来，一份从中途开始的记录就能说明是策略缩短了它，而不是让读者去猜测。除此之外，BuildMax 中没有其他任何地方会删除 audit 事件，也没有办法单独删除某一条。

Space 所有者可以从 space 设置中下载该 space 自己的审计轨迹，而 System Administrator 可以从 `#/admin` 下载整个部署范围内的审计轨迹并加以筛选。两者都可以导出为 CSV 或 JSONL，且这两种操作本身都会被记录在审计轨迹中，记为 `audit.exported`——阅读整份记录本身也是对它的一次操作。

### 对接你已经在运行的依赖

一次私有部署通常已经有现成的数据库和对象存储。三项设置决定了 BuildMax 是否会按照该环境所期望的方式去访问它们。

**`database.tls`** 是 go-sql-driver 的 TLS 模式。不设置时默认为 `preferred`：只要 server 提供 TLS 就使用它，但不校验证书。这样可以免费升级集群内部的连接，而在完全没有 TLS 的 server 上则表现得如同一个明文连接。

若对接的是托管数据库——RDS、Aurora、Cloud SQL——请将其设为 `true`，这会强制要求 TLS 并对照系统根证书校验证书。`skip-verify` 强制要求 TLS，但接受任意证书；`false` 则从不使用 TLS。

**`storage.minio.endpoint`** 决定 BuildMax 对接的是哪一种存储。若使用自建或某个厂商的 S3 兼容服务，请设置它。若使用 AWS S3，请留空，让 SDK 自行解析区域端点。

它同时决定 bucket 的寻址方式，因为这两种场景需要相反的答案：兼容存储需要 bucket-in-path 的形式，而 AWS S3 从 2020 年之后创建的 bucket 已经不再支持这种形式。`storage.minio.path_style` 可以覆盖这一自动推断，这只在某个采用虚拟主机寻址方式的兼容存储上才需要用到。

**`storage.minio.access_key` / `secret_key`** 可以都留空。此时客户端会回退使用 AWS SDK 的默认凭据链，这正是 pod 通过 IRSA、workload identity 或实例 profile 访问 bucket 的方式——不需要为该部署保存、下发给 worker 或轮换任何长期有效的密钥。只有面对没有这类机制的存储（例如 MinIO）时，才需要设置它们。

### 受管模型 —— `llm_model` 表和 `llm` 策略

[design/llm-gateway.md](../design/LLM网关.md) 中设计的受管 LLM 网关分为两个部分，且被刻意分隔开：

- **目录（catalog）** 是数据库中的 `llm_model` 表：有哪些模型存在、各自在哪里访问、使用什么凭据。它不放在 `server.yaml` 中，因为它保存提供商密钥，并且在 server 运行期间会发生变化。
- **`llm` 配置块** 如下所述，决定调用方在未指定模型时会得到目录中的哪一个。它不是一份授权列表：该部署启用的每个模型对其每个用户都可用。

在已经持有数据库凭据的机器上，用 `buildmax-server model` 编辑目录：

```bash
buildmax-server model add --name Fast \
    --api-url https://openrouter.ai/api/v1 \
    --api-key your-openrouter-api-key \
    --model openai/gpt-4o-mini --context-window 128000

buildmax-server model add --name Claude --provider anthropic \
    --api-url https://api.anthropic.com \
    --api-key your-anthropic-api-key \
    --model claude-sonnet-4-5 --context-window 200000 --max-tokens 8192 \
    --reasoning medium --prompt-cache --vision

buildmax-server model list
buildmax-server model disable --id lm_xxxxxxxxxxxxxxxxxxxx
```

`--provider` 是上游所使用的通信协议——与 `settings.yaml` 使用的是同样的三个取值，参见[模型提供商](#模型提供商)一节。默认值为 `openai_compatible`，因此该选项出现之前写下的目录仍能照常工作。`--max-tokens` 限定单次响应的上限；留空表示使用协议自身的默认值，对 `anthropic` 而言就是内置的 8192。`--reasoning`、`--prompt-cache` 和 `--vision` 分别对应 `settings.yaml` 中在[推理](#推理)、[提示缓存](#提示缓存)和[图像输入](#图像输入)几节中描述过的那些键在目录层面的等价物。在正在运行的 server 上修改任意一项，都会在下一次调用时生效：router 会为目标连接细节发生了变化的模型重新构建客户端。

| 键 | 含义 |
|---|---|
| `llm.default_model` | 调用方未指定模型时得到的 `--name`。留空时使用目录中第一个启用的模型，因此单模型部署这里什么都不用填。 |
| `conversation.model_target` | 让 Tier 1 使用目录中的某个模型运行，而不是 `conversation.model`——由 server 自己挑选模型，而不是被授予某一个。可以填 `llm_model` 的 ID，也可以填添加时用的 `--name`；名称的好处是运维人员可以在该行的运行时 ID 存在之前就先记下它。`BUILDMAX_CONVERSATION_MODEL_TARGET` 会覆盖它。 |

没有 `llm` 配置块的 server，会以目录中第一个启用的模型作为默认模型提供服务。`conversation.model` 仍然是引导阶段的路径——一个全新的部署，在其目录还一行记录都没有之前，就已经能够响应 conversation，并以名称的方式提供该模型。

`default_model` 指定一个不存在的模型会导致 **server 在启动时停止**。它本可以顺利解析通过，却会让每个会话的第一次调用都失败，这看起来像模型服务中断，而不是拼写错误。空目录不算错误：行是在 server 运行期间被逐渐添加进去的。

受管调用需要数据库，原因有两个：目录存放在那里，并且每次调用都会被记录到 `llm_call` 账本中。没有存储时，这些路由会返回 `503`，而不是提供一次无法计入账本的推理。

凭据保存在 `llm_model` 表中，只被一处查询读取，即用来构建提供商客户端的那处查询。它们从不会出现在模型列表、API 响应或错误信息中。请留意这一点在运维层面的含义：数据库备份和只读副本都会携带提供商密钥，因此应当像对待数据库密码一样对待它们。

### 部署中的本地模型

一个部署可以像 CLI 那样对接一个 Ollama 守护进程，但有一处不同会决定其余一切：**该守护进程必须能从 server 端访问到，而在容器内部，`localhost` 指的是容器自身。**

两个位置都可以配置它，且都不需要凭据：

```bash
# a catalog target spaces can be granted
buildmax-server model add --name "Local Qwen" --provider ollama     --api-url http://ollama.ollama.svc.cluster.local:11434     --model qwen3:8b --context-window 32000
```

```yaml
# or Tier 1 conversation, in server.yaml
conversation:
  model:
    model: qwen3:8b
    provider: ollama
    api_url: http://ollama.ollama.svc.cluster.local:11434
    context_window: 32000
```

该提供商不需要 `--api-key`，也不会为它保存任何内容。即便传入了某个 key 也会被忽略。

**访问宿主机上的守护进程。** 对于本地 Kubernetes 集群，把守护进程运行在宿主机上、再让部署指向它，通常比在 pod 内运行它更好：pod 无法使用宿主机的 GPU，因此推理会退回到集群所在虚拟机的 CPU 上执行。

| 宿主 | 从 pod 访问它的地址 | 还需要什么 |
|---|---|---|
| Docker Desktop（macOS、Windows） | `http://host.docker.internal:11434` | 不需要——网关会自动转发到宿主机的回环地址 |
| Linux，集群跑在 Docker 中（kind、k3d） | 网桥网关地址，`http://172.x.0.1:11434`——`docker network inspect <net>` 会打印出来 | `OLLAMA_HOST=0.0.0.0`，否则守护进程只监听回环地址 |
| 真实集群 | 守护进程自己的 Service，或某个能路由到它的地址 | —— |

有两个特性值得明确说明，而不是留给使用者自己发现：

- **该端点由运维人员提供，绝不会取自客户端请求。** 一个指向回环地址或链路本地地址的目标，指的是 *server 自身*的网络，这是一项部署决策。只有 System Administrator 才能添加这样的目标。
- **受管调用依然会被计量。** 一个本地目标不产生每 token 的费用，但它同样会被记录到 `llm_call` 账本中，这正是它可以在不产生实际费用的情况下，用来验证网关、配额和 audit 相关路径的原因。

### 用于 task run 的受管模型 —— `worker.llm` 配置块

Task run 默认自行调用某个提供商。改为指向网关，可以让 worker 不再需要上游密钥：

| 键 | 含义 |
|---|---|
| `worker.llm.transport` | `direct`（默认）或 `buildmax`。在 `buildmax` 下，`BUILDMAX_CONVERSATION_MODEL_API_KEY` 会被扣留、不发给 worker。`BUILDMAX_WORKER_LLM_TRANSPORT` 会覆盖它，因此同一个镜像无需重新编写挂载的文件就能在两者之间切换。 |
| `worker.llm.model` | 某次运行按 `--name` 调用目录中的哪个模型。留空则使用 `llm.default_model`。 |
| `worker.llm.context_window`、`worker.llm.call_timeout` | 向该次运行描述该模型；协议本身不会在每次调用时报告它们。 |
| `worker.run_token_ttl` | 一次运行凭据的有效期。默认 24h。无论是否受管，每次运行都会拿到一个。 |
| `worker.run_timeout` | 一次运行可以停留在 `SCHEDULED` 或 `RUNNING` 状态多久，超过后 server 会将其记为已放弃（abandoned）。默认 6h。这是兜底机制，而不是通常的检测路径：一个处于 `RUNNING` 状态、其 worker 已停止汇报的运行会在几分钟内被判定为失败，而被要求停止的 worker 会自行汇报其结果。这个超时机制留给的是那种从未进入 `RUNNING` 状态、或者从未做过任何汇报的运行。 |

Server 决定传输方式和模型；worker 从不自行选择模型，除此之外也不会被告知关于它的任何信息——端点、上游标识符和凭据都留在 server 一侧。每次运行都会被分发一个专属于它自己的凭据，放在 `BUILDMAX_RUN_TOKEN` 中，该凭据只授权这一次运行，不授权任何其他事情。

`./make compose smoke managed` 会针对一个 mock 上游跑通整条路径，且不需要任何提供商密钥。

在启用它之前，有两件事需要了解：

- `worker.llm.model` 指定一个目录中没有的模型，会导致 **server 在启动时停止**，与 `llm.default_model` 的处理方式相同。这样的配置本可以顺利解析通过，却会让每次运行的第一次模型调用都失败。
- Run token 不可续期。`run_token_ttl` 必须长于你最长的一次运行；超出该时长的运行会失去其剩余的模型调用能力。

## 数据目录结构

```text
<BUILDMAX_HOME>/
├── settings.yaml       CLI and Desktop configuration
├── server.yaml         Server and worker configuration
├── policy.yaml         Optional operator sandbox policy (overrides settings.yaml)
├── mcp.json            Optional MCP servers, merged with a workspace file
├── skills/<name>/      Skills available in every workspace
├── agents/             Subagent definitions available in every workspace
├── plugins/<name>/     Installed plugins; .state.json holds their source
├── sessions/           index.json plus one folder per session
│   └── <id>/           meta.json, history.jsonl, traces/<run>.jsonl
└── logs/               Rotating buildmax.log
```

`./make test` 会设置 `BUILDMAX_HOME=./testing-sandbox`，因此测试永远不会触碰真实的数据目录。

## 优先级

对于在多处出现的同一个值：

```text
environment variable  >  policy.yaml  >  settings.yaml / server.yaml  >  built-in default
```

沙箱是安全相关的例外情况：`policy.yaml` > 单次运行的 CLI 选项 > `BUILDMAX_SANDBOX_ENABLED` > `settings.yaml` > 界面默认值。一次运行可以开启沙箱并选择它的审批模式，但不能关闭沙箱，也不能覆盖运维人员策略。

沙箱这一配置块是唯一带有工作区级别这一层的；hook 是唯一以叠加方式合并（全局 hook 和工作区 hook 都会运行）而不是相互覆盖的配置块。
