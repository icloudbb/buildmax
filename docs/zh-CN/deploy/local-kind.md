# 本地 Kubernetes 部署

> **翻译说明：** 本文是[英文原文](../../deploy/local-kind.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
> **受众：** 贡献者和运维人员 · **状态：** Beta
>
> Kubernetes Worker Job、RBAC、Ingress、MinIO、清单，以及行为跨越浏览器、API、入口、支撑服务或 Worker 执行的实质性 Portal/服务器改动，都应使用此路径。较快的 [Compose 冒烟测试](compose.md) 仍适合内层迭代，但不能证明这些 Kubernetes 边界。

## 要求

- Docker，至少有 6 GB 可用空间
- kubectl

不必预先安装 kind：`tools/mk` 固定其版本并通过 `go run` 运行，确保所有集群都由同一版本创建、检查和删除。命令不会安装系统软件包或启动后台端口转发，始终通过明确的 kubectl Context 访问选定集群。

## 启动并验证

```bash
./make kind up
```

该命令创建 `buildmaxdev` 集群，并以 [Cilium](https://cilium.io) 代替 kind 默认的 kindnet 作为网络插件，然后：

1. 安装 ingress-nginx、MySQL 和 MinIO——由于 MinIO 已停止发布镜像，服务端和 `mc`
   镜像来自社区 MinIO 分支 [SILO](https://silo.pgsty.com)
2. 分别通过集群内 Job 创建 `bmstore` 存储桶并扩展 MySQL 开发权限
3. 构建并加载服务器、Portal 和确定性模拟模型镜像
4. 生成临时本地 Secret，应用 BuildMax 清单
5. 等待每个 Deployment 就绪
6. 创建真实 TaskRun，在 Kubernetes Worker Job 中执行，并通过 API 验证 Artifact

Cilium 在内核中执行 NetworkPolicy，包括 Worker API 边界。kindnet 的用户态策略引擎在长期运行的集群上会退化：新 pod 得不到保护，其他 pod 的 DNS 和 API 请求会超时，直到重启它才恢复。清单以 vendored 形式放在 `deployment/kind/cilium.yaml`，文件中附有生成它的命令。在此改动之前创建的集群仍运行 kindnet；`kind up` 会提示这一点，执行 `./make kind down` 再执行 `./make kind up` 即可用 Cilium 重建。`./make kind fixtures` 可恢复 QA 数据。

集群配置和应用的依赖清单位于 `deployment/kind/`；编排位于 `tools/mk/kind.go`。它们只用于开发，不属于真实部署。

打开 <http://localhost:8080>。Portal 与 API 同源，无需修改 `/etc/hosts` 或配置 CORS 配对。验证后，命令为 `deployment-smoke@buildmax.local` 输出新的单次使用验证码。

该源也是 Desktop 应用和 `buildmax login` 的 **Server URL**。两者提供的默认地址 `http://localhost:5678` 是本机直接启动服务器时监听的端口；这里不发布该端口，因为入口是唯一访问途径。

验证码首次使用即消耗，并且只打印一次。丢失后，`./make kind info` 会签发新的验证码，不会也无法显示旧验证码。

## 日常命令

```bash
./make kind smoke   # rerun the end-to-end assertions without rebuilding
./make kind smoke managed  # the same, with task runs reaching models through the gateway
./make kind drill rotation # rotate every credential; ephemeral clusters only
./make kind seed    # put the models in .local/settings.yaml into the cluster's catalog
./make kind fixtures # seed idempotent business data for automated testing
./make kind use-model "Claude Sonnet 5"  # run the cluster's own inference on a seeded model
./make kind mock    # switch the cluster's own inference back to the free mock
./make kind reload  # rebuild and load local images, then restart the deployments
./make kind reload server  # the same, for just the server (or portal)
./make kind info    # endpoints, plus a fresh login code for the smoke account
./make kind login   # the same code as JSON on stdout, for a script instead of a human
./make kind forward # forward the in-cluster MySQL and MinIO to 127.0.0.1
./make kind status  # read-only summary of the cluster, ingress, and workloads
./make kind logs    # pods, jobs, events, server, Portal, and worker logs
./make kind logs server  # just the server's logs (or portal, worker, mysql, minio, ingress)
./make kind drill restore  # rehearse a paired backup, wipe, and restore (ephemeral clusters only)
./make kind down    # delete the selected cluster
```

`drill restore` 具有破坏性，且独立于 `smoke`：它在备份后删除 `db`、`storage` 和 `buildmax` 命名空间，因此只在用 `BUILDMAX_KIND_EPHEMERAL=1` 创建的集群上运行，在其他地方一律拒绝。它证明什么、以及该流程如何用于真实部署，见 [backup-restore.md](backup-restore.md)。

`smoke managed` 将 `buildmax-config` ConfigMap 替换为 `deployment/smoke/server.kind.managed.yaml`，重启服务器，并在 TaskRun 推理经过网关的条件下重跑相同断言。它证明默认运行无法证明的一点：Worker Job 不持有提供商凭证也能完成真实 Task，其 Run 令牌通过 Job spec 传到 Pod。之后集群保持托管模式；重新运行 `./make kind up` 可恢复直连模式。

`drill rotation` 演练[凭证轮换手册](credential-rotation.md)：它通过修改 Secret 并滚动重启 Server，依次轮换 JWT 密钥、数据库密码、存储密钥、一个托管模型的密钥和 KEK；断言每个旧凭证都被拒绝，而会话、已存储的 Artifact 和新运行不受影响，最后输出滚动耗时和实测中断的表格。它有意让一个运行跨越 JWT 轮换，因此该运行会以 `FAILED` 结束。由于它会替换所有凭证，只有当前工作树用 `BUILDMAX_KIND_EPHEMERAL=1 ./make kind up` 创建了集群时才会运行；结束后用 `./make kind down` 删除该集群。

`info` 输出集群、Portal URL 及其健康状态、MinIO 凭证，并签发单次使用登录码。默认账户是 `deployment-smoke@buildmax.local`，也可通过 `./make kind info alice@example.com` 指定。`login` 省略面向人的横幅，改为输出 `{"email","code","portal_url"}` JSON；账户不存在时会先创建。`drive-portal` skill（`.buildmax/skills/drive-portal/`）使用它登录无头浏览器，无需人工复制验证码。

`fixtures` 填充**业务数据**，`seed` 填充**模型目录**。运行
`./make kind fixtures`，再通过 `./make kind login alice@buildmax.local` 登录。
主要场景位于 **BuildMax QA**，长列表场景位于 **BuildMax QA Pagination**。

| 功能 | 测试数据 |
|---|---|
| 账户与隔离 | Alice、Bob 保留有数据的个人 Space；Carol、Dave 的个人 Space 为空；Alice 持有系统管理员权限，以便访问管理界面 |
| 协作 | Alice 为 owner，Bob 为 admin，Carol 为 member，Dave 有待接受邀请；邮箱后缀均为 `@buildmax.local` |
| Issue | 三种状态；未分配、人、Agent、Workflow 分配；父 Issue 与进度不同的两个子 Issue；Markdown、中文、空描述及评论 |
| Agent / Workflow | 个人 Docs Writer/Release Notes；共享 QA Writer/QA Reviewer；两步骤 Workflow 的 draft、published、archived 状态与生命周期修订记录 |
| 文件 | `fixtures/` 下五个文件，包含嵌套 Markdown、CSV、JSON、中文文件名和空文本 |
| Artifact | 合成文本、HTML 沙箱预览、二进制下载 |
| Space 设置 | 非空 Agent instructions、active/disabled 的虚构 Secret；账户 Webhook 密钥；API 操作自然产生审计事件 |
| 插件与 Marketplace | 将 `sample-plugins/` 三个插件发布到部署目录，其中一个在 BuildMax QA 中启用，其余保留供启用 |
| 定时任务 | 具有不同 cron 表达式与时区的循环 Agent 定时任务，部分处于暂停状态（位于 BuildMax QA Pagination） |
| 分页与规模 | 独立 Space 中有 105 个 Issue（每种状态 35 个）并有 25 条评论的线程，另有可翻页/滚动的长列表：12 个 Agent、覆盖三种状态的 9 个 Workflow、60 个 Artifact（超过“加载更多”阈值）、8 个额外 Secret 与 8 个定时任务 |
| 管理规模 | 60 个合成账户（约每八个禁用一个），使管理员 Accounts 页面跨多页且其状态筛选有对应分组；每个账户也会获得个人 Space |
| 执行（`--runs`） | Conversation 对话、含 Continue/Retry 的 Task、Issue Agent 结果、两步骤 Workflow 结果、worker trace 与 workspace checkpoint |

```bash
./make kind fixtures --runs
```

`--runs` 会执行 Kubernetes worker，仅接受参考部署的免费 mock 配置。
检测到模型选择覆盖或自定义 server 配置时会拒绝，不会自动切换模型。
之前使用过 `kind use-model` 时，先执行 `./make kind mock`。不会调用付费模型。
失败或取消的执行会报错，不会伪造成功；修复原因并重试相应 Task 后可再次初始化。

重跑按名称/标题、Artifact 文件名、文件路径和 Conversation 首条消息复用资源。
列表读取全部分页，评论逐条按正文补齐，支持中断恢复。
保留已有 Issue 状态、描述、文件内容、Agent 定义和成员角色；校准测试 Issue 的
分配、Workflow 生命周期状态、Secret 状态、定时任务的暂停/启用状态与账户禁用状态，
仅在 Space instructions 为空时填入。Webhook 密钥与批量账户分别按名称和邮箱匹配，
重跑时只补齐缺失部分。已发布的插件版本和已存在的启用记录保持原样，不会重新发布。
不要重命名希望复用的测试资源。该命令不是并发事务，应一次运行一个实例。
不支持服务端幂等键的创建请求若丢失响应，下次运行通过稳定的测试资源标识查找恢复。

测试数据不等于所有功能均已验证：不初始化需要目录来源的托管模型授权，
不发送 webhook，不伪造 running/failed/canceled 状态。worker 边界和取消检查使用
`kind smoke`，托管网关使用 `kind smoke managed`，浏览器流程使用 `e2e kind`。
Artifact 分享应从已有测试 Artifact 手动或通过测试创建，初始化不生成公开链接。

`status` 不修改状态。它输出选定集群和 Context，通过入口探测 <http://localhost:8080/healthz>，并列出节点以及 `ingress-nginx`、`db`、`storage` 和 `buildmax` 中的 Deployment、Job 和 Pod。在阅读更长的 `kind logs` 输出前，可用它区分集群不存在还是不健康。

其他贡献者或任务正在使用默认集群时，请使用独立集群名称：

```bash
BUILDMAX_KIND_CLUSTER=buildmax-my-change \
BUILDMAX_KIND_PORTAL_PORT=18080 \
BUILDMAX_KIND_TLS_PORT=18443 \
  ./make kind up
```

默认集群使用宿主机端口 `8080` 和 `8443`。可连同集群名设置 `BUILDMAX_KIND_PORTAL_PORT` 和 `BUILDMAX_KIND_TLS_PORT` 来更改；之后针对该集群的每条 `kind` 或 `e2e kind` 命令都需传入相同三个值。Compose 栈默认也在 `8080` 发布 Portal，因此可以如上调整 kind 端口，或调整 Compose：

```bash
BUILDMAX_PORTAL_PORT=8081 ./make compose up
```

无需同步更改其他内容：`cors_origin` 和 Portal 的 API base 均由 `deployment/compose/.env` 中的端口推导。

## 读取运行写入的数据

MySQL 和 MinIO 使用 ClusterIP Service，集群只发布入口端口，因此本机无法直接访问它们。

```bash
./make kind forward     # publishes both to 127.0.0.1 until you stop it
```

该命令将 MySQL 转发到 `3306`，MinIO 转发到 `9000`（API）和 `9001`（控制台），并输出各自连接方式。转发输出的每行都标记来源目标。如果某个目标的宿主机端口已占用，会警告并跳过该目标；例如本地 MySQL 占用 `3306` 只会影响 MySQL 转发，不影响 MinIO。警告会给出将目标转发到自选端口的 kubectl 命令。

转发运行期间，可使用任意客户端连接 MySQL，例如 `mysql -h 127.0.0.1 -P 3306 -ubuildmax -pbuildmax buildmax`，或 DSN `buildmax:buildmax@tcp(127.0.0.1:3306)/buildmax`。这些是 `deployment/kind/mysql.yaml` 中的开发凭证；数据库使用 `emptyDir`，随集群删除。该账户可以使用任意 schema，不限于 `buildmax`，因此本地 `server.yaml` 中的 `database.name` 可自由指定，服务器首次启动会创建目标 schema。将同一 DSN 设置为 `BUILDMAX_TEST_DSN`，即可让 `internal/infra/db` 下的存储集成测试针对真实 MySQL 运行。

MinIO 控制台位于 <http://127.0.0.1:9001>，凭证为 `minio` / `minio123`，运行 Artifact 位于 `bmstore` 存储桶。这是 MinIO 的管理员账户；Server 和 Worker 使用各自的用户 `buildmax` / `buildmax-storage`，由 `deployment/kind/minio-init.yaml` 创建，且只能访问 `bmstore`——这与真实部署的存储身份形态一致，也是轮换演练所轮换的身份。

只执行一条查询时，可跳过转发，直接使用 kubectl：

```bash
kubectl --context kind-buildmaxdev -n db exec deployment/mysql -- \
  mysql -ubuildmax -pbuildmax buildmax -e "select * from task_run\G"
```

## 冒烟测试与真实提供商

本地命令有意覆盖使用 `deployment/smoke/server.kind.yaml` 和集群内 OpenAI 兼容模拟服务，保证贡献检查确定性，并确保 CI 不需要提供商凭证。

私有部署可将 `deployment/buildmax-deploy.yaml` 作为可读基线，并配置真实模型端点。`./make setup local` 从 `deployment/buildmax-secret.example.yaml` 生成 `.local/buildmax-secret.yaml`；填写后自行执行 `kubectl apply -f`。`./make kind up` 从不读取该文件，而是生成自己的临时 Secret，因此不要在本地验证之外使用生成的冒烟测试 Secret 或模拟模型。

### 使用自己的模型驱动集群

`./make kind seed` 将 `.local/settings.yaml` 中每个提供商模型加入集群目录。这样 CLI 和 Desktop 无需托管部署，就能针对真实推理验证托管传输 `transport: buildmax`。

```bash
./make kind up      # the stack, still answering from the mock
./make kind seed    # your models in its catalog
```

命令只通过 `buildmax-server model add` 添加各模型：目录行一旦存在即可调用，无需重启或修改配置。使用 `buildmax login` 登录 <http://localhost:8080>；保存登录后客户端进入托管模式，`buildmax models` 读取部署目录，不再使用本地 `settings.yaml` 的模型条目。

模型以添加时的 `name` 命名，即 `.local/settings.yaml` 中的显示名称，没有显示名称时使用模型 ID。登录后，`buildmax models` 会显示可用名称。

`seed` 有意不改动集群自己的推理。`conversation.model` 和 Worker 仍由集群内模拟服务响应，保持 Portal Conversation 和 `./make kind smoke` 确定且免费。重跑是安全的：目录中已有同名模型时，保留其行和 ID。修改已填充模型的端点或凭证，需要在 `.local/settings.yaml` 中重命名，或重建集群；`add` 不更新现有行。

要让集群自己的 Portal Conversation 和 TaskRun 使用已填充模型响应，需明确切换：

```bash
./make kind use-model "Claude Sonnet 5"   # conversations + task runs via the gateway
./make kind mock                          # back to the free in-cluster mock
```

`use-model` 在服务器 Deployment 上设置 `BUILDMAX_WORKER_LLM_TRANSPORT`、`BUILDMAX_LLM_DEFAULT_MODEL` 和 `BUILDMAX_CONVERSATION_MODEL_TARGET`，并重启它；已提交的 ConfigMap 不变，因此清除这些值的 `mock` 可精确恢复原状。此操作调用真实提供商并消耗配额，所以是独立于 `seed` 的步骤。

`.local/settings.yaml` 中的真实凭证会以明文写入集群 MySQL。该数据库随集群销毁，此路径仅供本地验证。

### 无需提供商密钥的真实模型

在模拟模型和托管提供商之间还有第三种选择：让部署访问**本机**的 Ollama 守护进程。真实推理、真实工具调用，无凭证、无账单；网关、`llm_call` 账本和配额也都真实运行。

不要将守护进程放进集群。Pod 无法访问宿主机 GPU，推理会回退到承载集群的虚拟机 CPU。让它留在宿主机上，并给部署一个可访问的地址：Docker Desktop 下为 `host.docker.internal`，该名称可在 Pod 内解析，即使守护进程绑定宿主机回环地址也能转发。`kind seed` 会自动将回环地址改写为这个名称；手工配置如下：

```bash
# a catalog target; it is callable by name as soon as the row exists
kubectl --context kind-buildmaxdev -n buildmax exec deployment/buildmax-server -- \
  buildmax-server model add --name "Host Ollama" --provider ollama \
      --api-url http://host.docker.internal:11434 \
      --model qwen3:8b --context-window 32000
```

`deployment/buildmax-deploy.yaml` 为 `conversation.model` 提供了相同的注释配置块。Linux 宿主机上应使用 Docker bridge 网关地址（`docker network inspect kind`），守护进程需要设置 `OLLAMA_HOST=0.0.0.0`。

## 为什么仍保留 Compose

Compose 和 kind 验证相同的用户可见流程，但验证的执行契约不同：

| 路径 | Worker | 存储 | 适用场景 |
|---|---|---|---|
| Compose | 服务器容器内的本地进程 | 共享本地文件系统 | 部署边界不变时的快速 Portal 和 API 迭代 |
| kind | 每个 TaskRun 一个 Kubernetes Job | 服务器和 Worker 共享 MinIO | 实质性 Portal/服务器集成、Job、RBAC、Ingress、对象存储和清单 |

保留两者可以让内层迭代快速进行，同时避免误把它当成完整部署证据。只要结论跨越浏览器、API、入口、支撑服务或 Worker 执行，就应最终在 kind 中验证。
