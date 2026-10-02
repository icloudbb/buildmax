# Beta 就绪记录

> **翻译说明：** 本文是[英文原文](../../deploy/beta-readiness.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
> **受众：** 运维人员和发布负责人 · **状态：** 当前——尚未通过资格验证

BuildMax **尚未通过 Beta 门槛**。本文定义首次私有部署 Beta 的受支持配置、用于验证同一不可变候选版本的流程，以及发布决策所需的证据记录。自动化测试只能证明候选版本已经可以开始演练；只有使用拟发布的同一组不可变产物生成的结果，才算资格验证证据。

首次 Beta 是对一个有边界的部署配置作出的承诺，而不是对每项已实现功能的承诺。下列核心配置项目会阻塞发布；本地表面保留各自的发布回归门槛。Experimental 配置可以与候选版本一起发布，但不会继承 Beta 承诺；按本文记录为关闭时，也不会阻塞 Beta。

在所有必需项目通过、每个证据链接都能持久保存并可由发布 Space 读取、最终决策完成签署之前，不要将上述状态改为 `qualified`。失败项目应保留在记录中，同时附上诊断和后续成功重跑结果。

## 资格验证合同

首次 Beta 面向**私有网络中的一个可信组织**。资格环境至少包含两个 Space 和两个不同角色的用户，确保 Space 授权边界经过实际验证，而不是被假定为正确。这并不表示系统已经适合公共、不可信的多租户环境。

### 核心私有部署配置

候选版本必须把以下内容作为同一个系统完成验证：

- Portal、两个受协调的 Server 副本、Redis、Kubernetes Worker Job、外部 MySQL、外部 S3 兼容存储、Ingress 和 TLS；
- 原生密码和 login code 身份验证、持久 Session、System Administration、Space membership、账户停用及 owner recovery；
- 托管模型、quota、Conversation、Issue、Agent、直接 Task、Continue、Retry、取消、延迟式 `AskUser`、Artifact、Space 文件、workspace checkpoint、trace、usage、audit 和运行诊断；
- 已发布的图形 Workflow，包括并行节点、输入 binding、structured output、持久人工请求、retry/timeout policy、整条 Run 取消、reconciliation 及失败/取消 drain；
- Agent 和 Workflow Schedule；
- Space Secret 环境变量交付和脱敏；
- 已实现的非可执行 Space Plugin 配置：精确 release 激活和 skill/subagent 物化；
- 经过身份验证的 inbound webhook。

如果候选配置移除其中某项能力，必须在资格验证前同步更新支持矩阵和用户文档。不能通过静默跳过一个可见的 Beta 能力来缩小承诺范围。

### 发布回归配置

CLI print mode、TUI、本地 Session 和 trace、Desktop（包括其登录后的 Issues bridge）不运行在私有部署候选环境中。因为它们从同一 revision 发布并共享 Agent Core，其文档规定的检查仍会阻止发布，但无需在 Kubernetes 运维旅程中重复执行。

### Experimental 配置

Remote Control、Telegram、Portal OIDC、本地 app connector、remote MCP 和 Browser capability，在本次决策中属于 Experimental。记录每项是关闭还是开启。开启的 Experimental 配置需要自己的证据和已接受限制，但其结果不会使该配置获得 Beta 资格；关闭的配置不得削弱或干扰核心配置。

### 首次 Beta 范围之外

首次 Beta 不承诺直接暴露公网、不可信多租户、multi-region、丢失 Worker Run 的自动重新派发、binary rollback、Worker Pod 全面出站 allow-list、gVisor 等外层 runtime、MFA、已完成资格验证的 SSO、Workflow condition/loop/manual approval、可执行 Space Plugin hook 或 MCP server、稳定 HTTP API、存储形状兼容性，以及签名或公证的 Desktop 安装包。

## 候选版本

开始前填写此表。仅有标签不构成不可变证据。

| 字段 | 记录值 |
|---|---|
| 版本和提交 | 未记录 |
| Server 镜像摘要 | 未记录 |
| Worker 镜像摘要 | 未记录 |
| Portal 镜像摘要 | 未记录 |
| 已启用/禁用功能清单 | 未记录 |
| 操作人员 | 未记录 |
| 演练日期和环境 | 未记录 |
| Kubernetes 版本和发行版 | 未记录 |
| CNI 和实际执行的 NetworkPolicy 行为 | 未记录 |
| Server 副本数和 coordination mode | 未记录 |
| Redis 产品和版本 | 未记录 |
| MySQL 产品和版本 | 未记录 |
| S3 产品、版本或服务及区域 | 未记录 |
| TLS 终止和 Ingress | 未记录 |
| 托管 provider、协议和模型别名 | 未记录 |
| Worker sandbox、seccomp 和 AppArmor 配置 | 未记录 |
| 当前 KEK id | 未记录 |
| Trace、audit、Artifact 和 checkpoint 保留策略 | 未记录 |
| 预期用户数、Space 数和并发 Run 数 | 未记录 |
| 目标 RPO 和 RTO | 未记录 |
| 已脱敏的配置快照 | 未记录 |
| 起始 schema/commit 及受支持的升级或全新安装路径 | 未记录 |

## 已接受的限制

记录运维人员和参与者已接受以下全部限制：

- [ ] 部署不直接暴露给不可信公共网络。
- [ ] 官方 Worker 镜像选择并探测严格 OS sandbox 基线；候选版本证明 Bash 受约束。受支持的 Worker 配置会禁用 stdio MCP，除非其子进程受声明边界约束。原生 Worker Pod 按文档使用 root、`SYS_ADMIN`、`NET_ADMIN` 以及 seccomp/AppArmor 配置，`bwrap` 会在 Bash 运行前丢弃所有 capability；本次 Beta 不要求 gVisor 等外层 runtime。
- [ ] Sandbox 将 Bash 的写入限制在 Run workspace 内，但不限制读取：Bash 可以读取 Pod 的只读文件系统，包括挂载的 server 配置，因此该配置不得包含任何凭证。Bash 不继承 Worker 自身的环境，而且 Worker 会把自身标记为不可转储，因此重新绑定的容器 `/proc` 也不会通过 `/proc/<pid>/environ` 暴露它。保留的 `BUILDMAX_*` 环境变量名不能作为 Secret 授权目标。
- [ ] 一般 Worker 出站没有强制 Pod 级目标 allow-list：Worker 进程自身需要访问对象存储和 Server。除 `open` 外的任一网络档位下，Bash 都运行在独立的网络命名空间中，唯一出口是强制执行该档位的沙箱代理；`open` 档下与 Pod 共享网络。Worker 端口 `NetworkPolicy` 限制控制通道入站，但不限制出站流量。
- [ ] Worker 可以获得存储凭证或投射的存储身份，因为它直接读写运行状态和 Artifact。
- [ ] Hook 按文档 fail open；Worker 配置会禁用不受支持的可执行 Plugin 和 stdio MCP 内容，而不是宣称它们受到约束。
- [ ] SSO、multi-region、丢失 Run 自动重新派发、binary rollback 和以上 Experimental 配置不属于此次 Beta。
- [ ] HTTP 和配置兼容性仍采用 Alpha 合同；回退意味着使用匹配的二进制恢复升级前配对备份的数据库和 bucket。

## Q0. 范围与预检

- [ ] 固定三个候选镜像摘要，归档已脱敏的渲染部署配置和启用/禁用功能清单。
- [ ] 部署两个使用 Redis coordination 的 Server 副本、通过 TLS 访问的外部 MySQL 和 S3，以及能执行候选 `NetworkPolicy` 的 CNI。
- [ ] 创建至少两个 Space 和两个不同角色的用户。候选版本同时暴露 personal 和 shared Space 时，两者都要包含。
- [ ] 记录 Worker CPU、内存和 ephemeral storage 的 request/limit。故意提供一个无效资源数量，记录候选版本拒绝启动并指出配置键。
- [ ] 记录预期用户数、Space 数和并发 Run 范围，以及恢复演练将测量的 RPO 和 RTO。
- [ ] 记录并演练 trace、audit、Artifact 和 checkpoint 的保留与容量策略。永久保留配置必须附带明确容量规划、监控和运维清理流程。
- [ ] 在任何升级演练前协调备份数据库和 bucket，并记录其标识符。
- [ ] 记录候选版本的 `./make check ci`、MySQL、Windows、CLI/Desktop、直连和托管 Compose/kind smoke、Portal 浏览器 E2E、Desktop package 及 release archive 验证 URL。
- [ ] 记录 Server、Worker 和 Portal 镜像扫描结果、SBOM 位置和 provenance attestation 验证结果。

## Q1. 身份、授权与治理

- [ ] 引导 System Administrator，保持 signup 关闭，创建或邀请两个用户，并演练密码和单次 login code 登录。
- [ ] 通过已有用户和运维表面证明 refresh、logout、管理员 Session revoke 及绝对 Session 过期。
- [ ] 在两个 Space 中证明 member/owner 访问，以及 Issue、Task、Workflow、Artifact、Secret 和 Plugin activation 的跨 Space 拒绝。
- [ ] 移除一个成员，证明新工作、Schedule firing、dispatch 和 Worker fetch 都重新检查 eligibility 并 fail closed。
- [ ] 读取 deactivation impact 后停用一个账户。确认其 Session 被撤销、执行中 Run 被取消、Schedule 被暂停，并且重复清理是安全的。
- [ ] 恢复所有 owner 均已停用的 shared Space，且不会暴露 Space 内容或允许恢复 personal Space。
- [ ] 演练一次 quota 拒绝，证明不会留下 orphan Task/TaskRun，usage 视图能解释拒绝原因。
- [ ] 确认以上每项敏感操作都产生文档规定的 audit event，System Administration 不会读取超出其明确权限的 Space 内容。

## Q2. 核心产品旅程

使用文档规定的 UI 和运维表面。执行者不应需要了解源代码。

- [ ] 运行前台 Conversation，并通过它创建后台工作。
- [ ] 通过托管模型在 Kubernetes Worker Job 中运行直接 Agent Task，查看 stream 和持久结果、trace、托管调用 ledger、usage、audit、workspace checkpoint 和可下载 Artifact。
- [ ] Continue 此 Task，证明恢复最新 workspace 和 Session；Retry 该 Task 的最新 Run，证明使用该 Run 的原始 base 而不是其结果。两者都创建有独立证据的新 TaskRun，而不修改历史。
- [ ] 取消运行中的 Task，确认 partial output、Artifact 和 checkpoint 语义符合文档合同。
- [ ] 让 Worker Run 调用 `AskUser`，确认 Task 显示需要回答；通过 Continue 回答，并验证 successor Run 收到答案而原始 Run 保持不可变。
- [ ] 在一个请求中使用 Owner 和 Executor 创建 Issue，运行其 executor，并将 result、status 和 discussion 追踪回 Issue。
- [ ] 发布并运行包含并行分支、JSON Pointer binding、structured output 和 authoritative result 的 Workflow；演练 `human_input` 节点和以 `AskUser` 结束的 Agent 节点，回答其中一个，拒绝或让另一个过期，并证明等待期间不占用 Worker。
- [ ] 演练 Workflow 每次尝试的 retry/backoff、node timeout、run deadline 及整条 Run 取消；随后让一个节点失败，证明已接纳 sibling 完成 drain、pending node 被阻止，重启恢复最终收敛，且不会创建重复 Task 或超过发布 policy 的 attempt。
- [ ] 分别触发 Agent Schedule 和 Workflow Schedule。围绕 due time 重启 Server，证明只有一次 catch-up、没有重复 fire，并在连续失败或创建者失去资格后看到 pause reason。
- [ ] 存储 Space Secret，将一个 item 授权给 Agent 并在 Worker 中使用，证明其值不出现在 trace、stream、tool result 或日志中。
- [ ] 激活仅含受支持 skill/subagent 配置的 Plugin release，在 Agent 上选择它，证明 Worker 物化 Run 所记录的精确 version 和 digest；激活包含 hook 或 MCP server 的 release 必须被拒绝。
- [ ] 发送 authenticated inbound webhook，证明 Conversation turn 以 webhook key owner 身份运行且保持 Space scope。
- [ ] 使用 Administration 找到人为制造的 stalled 和 failing work，指出 `failure_class`、下一步行动者，并在不读取无关 Space 内容的情况下进入底层 Run。

## Q3. 执行与 Secret 边界

- [ ] 检查一个运行中的 Worker Job，记录只读 root filesystem、已移除 capabilities（除 `SYS_ADMIN` 和 `NET_ADMIN` 外，且都不会到达 Bash）、seccomp/AppArmor 配置、缺失的 ServiceAccount token、实际 CPU/内存/ephemeral storage 资源、最小环境变量和 per-run credential。
- [ ] 证明 Bash 无法写入 Run workspace 之外的位置、不继承 Worker 的凭证，并且 trace 报告实际 sandbox boundary 而不是配置意图。
- [ ] 证明公共 Service 不暴露 Worker route、无标签 Pod 被 Worker `NetworkPolicy` 拒绝、有标签 Worker 使用内部 TLS listener。
- [ ] 证明 run token 不能访问其他 Run，不能在 claim 前或 terminal 后执行操作，并且只能访问文档规定的 Worker route。
- [ ] 证明包含可执行内容的 Plugin release 在激活时被拒绝；并证明在 Worker 配置中从任一层（例如 workspace 的 `.buildmax/mcp.json`）解析到的 stdio MCP server 会让 assembly 在执行命令或首次模型调用前失败，而不是静默在边界外运行。
- [ ] 证明 consuming Worker 能使用 Space Secret，但其值在 trace、stream、tool result 和保留诊断中都被脱敏。
- [ ] 让 finished-Job TTL 删除 Job 和 Pod；确认 TaskRun、trace、checkpoint、result 和 Artifact 仍可读取。

## Q4. 持久化与分布式正确性

- [ ] 直接驱动两个 Server 副本，证明跨副本 Task stream、connection event 和 Conversation turn serialization。
- [ ] 让一个副本失去 Conversation lease，证明 stale writer 无法在新持有者之后追加消息。
- [ ] 重启 Redis，证明 stream、lease 和正常服务恢复，没有 durable state 丢失，也没有静默回退到 local coordination。
- [ ] 滚动两个 Server 副本。Pod 退出前 `/readyz` 必须进入 draining；已接纳工作完成或达到文档规定的 terminal state，Workflow reconciliation 和 Schedule 恢复且不重复执行。
- [ ] 在预期并发范围内同时执行 Continue、Retry、cancel、Workflow reconciliation 和 Schedule claim，不得留下非法状态、重复持久执行或无法解释的 orphan record。
- [ ] 资格窗口结束时，每个较老的 `PENDING`、`SCHEDULED` 或 `RUNNING` 项目都必须在文档时间界限内推进，或已分类并给出 operator action。

## Q5. 故障演练

针对固定候选版本执行。每个场景保存时间戳、相关日志、状态界面、TaskRun JSON、trace、audit row 和 Artifact 列表。

- [ ] 在 Agent 执行时取消 Run。它在配置宽限期内达到 `CANCELED`，并保留取消前产生的 output 和 Artifact。
- [ ] 优雅终止一个 Worker，再硬终止另一个使其无法报告。前者遵循 shutdown policy；liveness reaper 将后者结算为 `FAILED`、指出失去 Worker 联系、不执行隐藏重试；显式 Retry 作为新 Run 成功。
- [ ] 中断数据库访问。`/readyz` 和 System Status 显示依赖故障，Server 无需重建，恢复访问后服务自行恢复。
- [ ] 中断 Redis。依赖 coordination 的工作按文档拒绝或降级，durable record 保持完整；恢复后双副本运行继续且不重复 turn 或 Run。
- [ ] 拒绝 Worker 写入对象存储。Run 以可理解原因失败，保留安全证据，不留下声称缺失对象可下载的记录。
- [ ] 拒绝 Server 读取对象存储再恢复。Readiness 和 System Status 显示故障，完成的 Artifact 无需重写 Run 数据即可恢复读取。
- [ ] 演练 provider timeout、rate limit 和 credential refusal。TaskRun failure classification、managed-call status、日志和 operator action 必须一致，且不泄露 provider credential。

## Q6. 恢复与维护

- [ ] 按[备份与恢复手册](../../deploy/backup-restore.md)将协调备份的数据库和 bucket 恢复到空白环境。运行 `buildmax-server storage verify --checksums`，比较 account、Session、grant、membership、Conversation/message、Issue/comment、Agent/revision、Workflow/run/node/result、Schedule、Task/TaskRun、trace、audit、usage、Artifact、checkpoint、Secret、模型目录项、Plugin package/activation 以及 migration ledger 的标识符和有效状态。Space 文件没有可供 `storage verify` 枚举的数据库记录，必须单独比较。
- [ ] 使用原始 KEK 实际使用恢复后的 managed model credential 和 Space Secret，从 checkpoint Continue 一个备份前 Task，并下载抽样 Artifact 验证原始 checksum。记录实际 RPO、RTO 和接受的数据损失。
- [ ] 按[生产部署指南](../../../deployment/production/README.md)通过真实 schema 变化演练声明的升级路径。在升级后的 schema 上启动旧 binary，证明它按文档拒绝；通过恢复升级前配对数据库和 bucket 完成回退。数据库 down migration 和 binary rollback 不受支持。
- [ ] 按[凭证轮换手册](../../deploy/credential-rotation.md)轮换 JWT secret、数据库 credential、存储 identity 或 credential、managed-model credential、KEK 以及 Worker API certificate/CA。记录对 Session、执行中 Run、已有 Job、存储数据和新工作的影响；overlap window 结束后旧 credential 必须被拒绝。
- [ ] 演练 trace、audit、Artifact、checkpoint orphan 和 finished Job 保留策略。存活记录必须保留有效 pointer；已删除对象不得继续被声明为可读取；每次 audited prune 都必须可见。
- [ ] 记录实际演练的 Kubernetes、CNI、Redis、MySQL、S3、Ingress、TLS、provider 和 model 精确版本。这是首次 Beta 的已测试集合，不是对广泛兼容矩阵的承诺。

## Q7. 运行窗口与产品质量

- [ ] 按记录的适度负载运行候选版本至少 24 小时，跨过 Schedule、liveness、reconciliation、retention 和 Job cleanup 周期。期间滚动一次 Server 并重启一次 Redis。结束时不得存在 stranded Run、重复 Schedule fire、缺失对象或未分类失败。
- [ ] 记录数据库、bucket、trace 和 audit 增长；证明配置的 retention 与 capacity plan 覆盖声明的运行窗口。
- [ ] 使用文档标识符和可用日志，将一个用户操作关联到 request、Task、TaskRun、Worker、managed call、trace 和 audit。记录 BuildMax 没有 metrics/alerting integration，operator 必须在哪些位置使用基础设施日志。
- [ ] 让没有实现这些功能的 operator 只使用文档表面完成核心旅程并诊断注入的故障。
- [ ] 使用选定的真实托管模型运行产品自有 evaluation suite，记录 pass rate、不确定性、usage、cost 和每个 unscored failure。平台/runtime error 和 trust violation 会阻止资格验证；不得把小型 suite 表述为通用 benchmark 分数。
- [ ] 确认发布回归配置通过文档规定的检查：CLI/TUI、Desktop bridge 和 UI、打包 Desktop launch，以及所有受支持 provider contract test。

## 证据

每个旅程或演练添加一行。如果真正证明是 Pod 日志、恢复后标识符、checksum 或截图，仅有 CI 摘要页面不够。

| Gate 或演练 | 结果 | 证据 URL 或产物 | 说明和后续事项 |
|---|---|---|---|
| Q0 候选范围、配置与供应链 | 未运行 | — | — |
| Q1 身份、授权与治理 | 未运行 | — | — |
| Q2 核心产品旅程 | 未运行 | — | — |
| Q3 执行与 Secret 边界 | 未运行 | — | — |
| Q4 持久化与分布式正确性 | 未运行 | — | — |
| 执行中取消和 Worker 优雅丢失 | 未运行 | — | — |
| Worker 硬丢失和显式 Retry | 未运行 | — | — |
| MySQL、Redis 和对象存储故障 | 未运行 | — | — |
| Provider 故障 | 未运行 | — | — |
| 数据库与 bucket 配对恢复 | 未运行 | — | — |
| 声明的 schema 路径与配对恢复回退 | 未运行 | — | — |
| Credential 和 Worker TLS 轮换 | 未运行 | — | — |
| Retention 和 capacity | 未运行 | — | — |
| 24 小时运行窗口 | 未运行 | — | — |
| 非作者 operator 旅程 | 未运行 | — | — |
| 真实模型产品 evaluation | 未运行 | — | — |
| 本地和 Desktop 发布回归 | 未运行 | — | — |

## 决策

当前决策：**尚未达到 BETA 就绪条件**。

资格验证要求所有核心 Gate 通过，不得存在 authorization escape、无法解释的数据损失、stranded durable work、数据库 pointer 指向缺失对象，或未解决的 critical/high security finding。任何 waiver 都必须指出未满足行为、用户影响、补偿性 operator control、owner 和到期时间；它是明确的发布决策，而不是暗示通过。

| 角色 | 姓名 | 日期 | 决策或 waiver 链接 |
|---|---|---|---|
| Qualification operator | 未签署 | — | — |
| Engineering owner | 未签署 | — | — |
| Release owner | 未签署 | — | — |
