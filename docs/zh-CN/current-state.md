# BuildMax 当前状态

> **英文原文：** [BuildMax Current State](../current-state.md)
>
> **读者：** 用户、运维人员与贡献者 · **状态：** 截至 2026-09-12 当前有效
>
> 本文是英文原文的简体中文镜像；如有差异，以英文原文为准。

本次评估对照了仓库 `0bd7e5bf` 的代码，描述已实现行为、测试覆盖和剩余限制。
优先级与后续顺序由[路线图](ROADMAP.md)维护，本页不再另列一套优先级。
设计记录解释决策；其中尚未勾选的清单不能证明代码尚未实现。

## 评估结论与证据范围

BuildMax 仍处于 Alpha。本地 Agent 执行与私有 Space 执行链路已经实现，
包括独立 Task 线程、持久工作区检查点、托管推理和运维管理。
这些不等于已具备生产多租户服务能力，也不证明 Beta 候选版本已通过验证。
[Beta 就绪记录](deploy/beta-readiness.md)仍未合格。

受支持的无人值守 worker 配置现在失败关闭地禁用 stdio MCP：解析到的 stdio server
在装配期、命令运行前、首次模型调用前让 worker 运行失败，而远程传输与本地界面不受
影响；TaskRun 诊断在记录的边界旁于 Portal 运行详情中展示该处理。R0——受支持的
worker 契约——已关闭：[信任保障](design/信任保障.md) §6.1 把每项控制（Bash 隔离、
worker API 隔离、stdio MCP 失败关闭、进程限制与 hook 边界）映射到其证据，其中 Bash
与 worker API 隔离经真实部署 worker 路径证明。经由运维旅程的不可变候选资格认证仍是
单独的 Beta 关卡。整个 worker 的出站网络是首个私有 Beta 已记录并接受的限制。
持久 Workflow 状态协调、轨迹保留，以及候选版本的故障与恢复证据仍待完成。
共享 Redis 协调已经实现，包括消息历史写入对分布式租约 fencing token 的校验。
worker API 已有独立监听器、TLS 支持和已交付的入站 NetworkPolicy；
不能把这部分网络边界与尚未限制的 worker 出站网络混为一谈。

本次复核检查实现、组装、部署清单和测试断言，不将旧版全量构建结果、覆盖率、
变异测试结论或成熟度百分比当作当前版本的证据。本次实际验证列在文末。
测试文件存在表示已实现对应测试，不表示此次复核运行了其所需的部署或数据库环境。

## 共享 runtime 与本地界面

CLI/TUI、Desktop 与 worker 组装共享 Agent runtime。核心包含流式模型/工具循环、
工具错误恢复、只读工具并行执行、权限、审批、压缩与检查点、hook、有界脱敏轨迹、
用量统计、Session、笔记、待办、Project Memory、subagent、worktree 和后台任务。
模型组装支持 OpenAI-compatible chat、OpenAI Responses、Anthropic 与 Ollama。

Desktop 交互式回合现在使用 `agentapp.RunScheduler`：按 Session key 串行执行一个 Run、
依序排队后续提示，并让排队的后台事件使用相同生命周期。Server TaskRun 调度器仍是
另一项持久执行平面职责。

本地检查已有 `buildmax info`、TUI `/info` 和面向单个会话的 Desktop `/info` 面板；
`buildmax usage` 跨会话汇总 token 与成本总额，可按天、工作区或模型分组。
Desktop 的 Memory 界面有意保持只读：用户直接编辑 Markdown 文件，用
`buildmax project forget` 删除或清空，并用 `--no-project-memory` 为单次运行禁用；
不再计划单独的 Desktop 编辑/删除/启用控件。

本地执行不需要 Server。已登录客户端可使用托管模型目录；Server 拒绝凭证时，
客户端将其视为登录过期，执行 `buildmax logout` 可回到本地模式。
参见[模型来源解析](../../internal/interface/auth/models.go)及其
[测试](../../internal/interface/auth/models_test.go)。

共享 LLM 请求契约已提供与提供商无关的结构化输出 schema：`Request.Output`
schema 会按 [`internal/core/jsonschema`](../../internal/core/jsonschema) 中的
共享子集校验，结果——校验通过的值、其模式、提供商是否强制，或一个带类型的
失败——回到 `Completion.Structured`。每个提供商都映射到各自的原生机制——
OpenAI Chat 与 Responses 用 `response_format` `json_schema`、Anthropic 用单个
强制工具、Ollama 用 `format`——并且 Client 会对候选值统一校验一次（即便 native）。
将其接入一次运行的最终答案和某个消费方，以及已推迟的 prompted 兜底，仍待完成
（[结构化输出](design/结构化输出.md)第 3-4 阶段）。工具参数的 JSON schema
是另一项能力；参见
[LLM 契约](../../internal/core/llm/llm.go)。

## Task、结果与工作区连续性

直接运行 Agent 会创建 Task 与 TaskRun，不需要 Conversation。
Continue 向同一个 Task 追加 Run；Retry 创建带有明确重试关联的新尝试。
TaskRun 是结果的权威来源。Conversation 可以创建 Task，但不是其授权或存储父级。
旧结果投递队列与强制前台摘要尝试已经移除。

**独立 Task 的流式输出已实现。** worker 按 Task ID 追加增量，Task SSE handler
订阅该 ID，Portal 的 Task 页面读取流并轮询持久 Run 状态。这条路径不依赖 Conversation。
页面也能打开已存储的 Run 轨迹。代码依据：

- [worker handler](../../internal/server/handlers/worker/worker.go)
- [Task SSE handler](../../internal/server/handlers/work/stream.go)
- [Portal Task 页面](../../portal/src/pages/tasks/TaskDetail.tsx)

流行为取决于协调模式：local 模式在内存中缓冲，Redis 模式跨副本共享有界且会过期的流。
两者都不是无限期重放日志。[Portal Task 线程测试](../../portal/e2e/task-thread.spec.ts)
通过 UI 覆盖直接执行、Continue 与 Retry；
[双副本流测试](../../internal/server/handlers/work/stream_multireplica_test.go)
使用 miniredis 验证跨副本增量与缓冲输出。这些测试不能证明所有流式输出、轨迹或
托管用量故障场景均已验证。

Task 工作区以不可变检查点保存在对象存储中。首个 Run 建立基线；Continue 使用
Task 的工作区 head，Retry 使用被重试 Run 的基线。成功的结果检查点推进 head；
部分检查点保留失败或取消后的工作，但不推进 head。worker 记录恢复状态，终态报告
完成可用检查点的提交。检查点提交失败不会改写 Run 的结果。

实现和测试涉及[worker 检查点](../../internal/agentapp/taskrun/checkpoint.go)、
[检查点服务](../../internal/service/workspace/checkpoint.go)、
[数据库实现](../../internal/infra/db/workspace_checkpoint.go)与 worker 检查点 handler。
worker Job 有临时存储限制，孤儿与保留期清理回收未引用的数据。
Portal 只读展示检查点与恢复状态。这些机制不能证明数据库/存储桶配对恢复或升级回滚合格。

## worker 执行与网络边界

### Bash 沙箱与子进程

[`config.WorkerSandboxSurface`](../../internal/config/sandbox.go) 在存在
`BUILDMAX_SANDBOX_BACKEND_INSTALLED` 时选择严格 worker 基线。
官方镜像安装 `bubblewrap`、`socat` 并设置该标记；这也包括官方镜像内以
local process 方式启动的 Compose worker。未标记的裸机在没有额外配置时继承 CLI
基线；代码并未让所有可能的 worker 启动方式都默认在隔离不可用时拒绝执行。

所选沙箱综合解析设置、策略、Run 覆盖与 Agent 层级。
worker handler 解析并固定实际 Agent/Space 层级以供审计。
选用 fail-closed 策略时，后端自检会拒绝不可用的强制机制。
资源控制通过在封装命令前添加 shell 限制实现；macOS 不实施内存限制。
command hook 使用 Bash 封装与净化后的环境，HTTP hook 查询允许的主机策略。

Kubernetes worker 安全上下文为 **root，并添加 `SYS_ADMIN`**，
同时使用只读根文件系统和提供的 Localhost seccomp 配置。
Linux Bash 封装将容器的 `/proc` 重新绑定为只读。
这不是非 root Pod，也不是将整个 worker 隔离到与命令沙箱相同的边界。
依据：[Job 构建](../../internal/infra/k8s/job.go)、
[Linux Bash 沙箱](../../internal/infra/sandbox/bwrap_linux.go)和
[seccomp 部署说明](../../deployment/seccomp/README.md)。

部署冒烟包含实际 worker Bash 探针，检查命令成功执行和工作区外写入被拒绝
（[部署冒烟实现](../../tools/mk/deploy_smoke.go)）。这是已实现的端到端覆盖，
不表示此次复核运行了集群冒烟。

剩余限制：

- MCP stdio 服务通过 `exec.Command` 启动，未经过 Bash 沙箱
  （[MCP transport](../../internal/infra/mcp/transport.go)）。受支持的 worker 配置必须
  约束或拒绝它们；这项失败关闭处理尚未实现。
- 即使 Bash 命令已隔离，`local_process` 仍与 Server 处于同一主机信任域。
- `buildmax sandbox overrides` 未实现。Portal 可设置 Agent 层级和 Space 默认值；
  Run Details 会显示轨迹记录的边界和解析后的 Plugin 固定版本，但不会把请求/解析后的
  层级对或 stdio MCP 处理作为独立诊断字段展示。
- Job 构建器未接入 worker RuntimeClass 选择。gVisor 是条件触发的 Beta 后加固，
  不是已交付且受支持的 worker 配置，也不是首个 Beta 要求。

### worker API 边界

**已实现：** 公共路由与 worker 路由使用独立 mux 和监听器。
公共监听器不存在 worker 路由，与调用方持有什么 token 无关。
Server bootstrap 构建 worker 监听器的 TLS 配置；可选客户端 CA 配置启用原生 mTLS。
worker 可以使用配置的 CA 与客户端身份，仍须通过每次 Run 的身份认证。

基础与生产 Kubernetes 清单包含 worker API Service 和 Server 入站 NetworkPolicy，
仅允许同一 namespace 中匹配的 worker Pod 访问 worker 端口。
该策略仍允许集群流量访问公共 API 端口。实施需要支持 NetworkPolicy 的 CNI；
清单存在不能单独证明策略实际生效。

实现与覆盖：[Server 组装](../../internal/server/server.go)、
[监听器边界测试](../../internal/server/listener_boundary_test.go)、
[worker TLS](../../internal/bootstrap/worker_tls.go)与
[生产清单](../../deployment/production/buildmax.yaml)。

**首个 Beta 接受的限制：** worker 出站 NetworkPolicy 尚未实现。Server 入站策略不会限制 worker
所有出站流量，不会隔离 MCP 进程，也不会隐藏 worker 使用的存储凭证。
支持 TLS 也不表示所有本地开发配置都强制使用 TLS。

## Server 拓扑与持久化

### 共享协调已实现

`coordination.mode: local` 仍是单实例默认值。Redis 模式通过
[Server 适配器](../../internal/server/coordination/coordination.go)与
[Redis 基础实现](../../internal/infra/coordination)接入共享 Task 流、连接事件分发和
可续租的 Conversation 回合租约。配置的 Redis 不可达时，bootstrap 拒绝启动，
不会静默回退到 local 模式。

基础/kind 与生产清单现在均配置 Redis 和两个 Server 副本。
架构测试拒绝没有协调后端的多副本部署。多副本流与租约行为已有自动化测试，且一个
部署级 kind 探针（[`kindCoordinationProbe`](../../tools/mk/coordination_probe.go)，
由 `./make kind smoke` 运行）现在会手动驱动两个副本、对着真实 Redis 证明候选拓扑上的
跨副本流投递、跨副本 turn lease 串行化以及 Redis 重启后的恢复。
租约提供 fencing token，且消息历史写入会校验它：携带的 token 低于会话已接受值的
写入会被拒绝，因此租约丢失后的陈旧写入者无法追加到新持有者之后。续租本身会丢弃
Redis 错误，所有权丢失时也不会取消正在运行的回合，因此这样的回合会一直跑到写入
被拒绝，而不是被提前停止。参见[服务器协调设计](design/服务器协调.md)。

调度器每个实例只有一个并发 dispatch 槽位。local-process 模式下，该槽位在执行期间
持续占用；Kubernetes dispatch 创建 Job 后就返回，因此这一设置**不意味着集群只能
同时运行一个 worker**。参见[调度器](../../internal/server/scheduler/scheduler.go)与
[Job runner](../../internal/infra/k8s/job.go)。

### 数据库覆盖与迁移

`./make test mysql` 要求提供 DSN，创建并删除隔离数据库，并拒绝因缺失 DSN 而跳过测试。
CI 提供固定版本的 `mysql:8.0` 服务。默认测试在没有 DSN 时仍跳过依赖数据库的用例。

数据库测试覆盖比旧评估更广：

| 已有数据库测试的行为 | 证据 |
|---|---|
| 重试关联与原始尝试保留 | [task_run_retry_test.go](../../internal/infra/db/task_run_retry_test.go) |
| Continue 与 Retry 的工作区基线选择 | [task_run_base_test.go](../../internal/infra/db/task_run_base_test.go) |
| 独立 Task、续跑与幂等键 | [task_direct_test.go](../../internal/infra/db/task_direct_test.go) |
| Task 领取、Run 转换、单个活跃 Run 及取消/报告竞争 | [concurrency_test.go](../../internal/infra/db/concurrency_test.go) |
| Artifact 软删除、并发删除、过期、字节统计与清理生命周期 | [artifact_retention_test.go](../../internal/infra/db/artifact_retention_test.go) |
| 检查点 head 推进与部分检查点保留 | [workspace_checkpoint_test.go](../../internal/infra/db/workspace_checkpoint_test.go) |
| Workflow 受保护的 Run/步骤转换与原子失败收口 | [workflow_test.go](../../internal/infra/db/workflow_test.go) |
| Workflow 初始修订与修订查询 | [revision_query_test.go](../../internal/infra/db/revision_query_test.go) |
| Secret 的 Space 隔离与独立邀请 | [secret_test.go](../../internal/infra/db/secret_test.go)、[space_invitation_test.go](../../internal/infra/db/space_invitation_test.go) |

受保护的转换会拒绝非法终态改写，并将失败步骤、后续步骤阻塞和 Run 失败收口原子化。
它们尚未形成持久协调器：推进仍依赖 callback，因此重启与 callback 丢失后的恢复仍未完成。
这些测试也不能穷尽证明跨 Space 存储行为或编辑及并发下的 Workflow 修订推进。
外部依赖恢复仍需具体场景证据。已移除的结果投递队列不再有独立的重启恢复义务。

**显式迁移列表已经不为空。** [migration.go](../../internal/infra/db/migration.go)
包含 `system_grant_live_marker` 和 `llm_model_credential_encryption`。
后者删除旧明文凭证列而不迁移其中的值；受影响的模型需要重新添加。
迁移测试覆盖账本记录与第二次运行跳过。该测试或设计文档中的 N-1 策略，都不能证明
旧模式升级与二进制回滚已实际演练。“迁移历史为空，无法建立 fixture”的旧理由已过时。

每份轨迹都有字段与记录数量上限，但轨迹目录没有保留期清扫。长期运行的进程目前需要
外部容量管理或手动删除；BuildMax 尚不能记录旧轨迹是因策略而被移除。

## 账号、Space 与扩展界面

账号创建、一次性登录码、密码登录、系统管理员授权、面向已有账号的 Space 邀请、
角色变更、所有权转移和成员级恢复均已实现。注册默认关闭；创建账号本身不发放凭证。
每次登录都会开启一个持久 Session（`auth_session`），请求守卫在每次调用时都会检查它，
因此登出、管理员撤销和禁用会在访问令牌较短的寿命之内、而不是等到过期时让一个已签发的
访问令牌停止；Session 还带有一个绝对寿命。SSO 尚未实现。
参见[身份服务](../../internal/service/identity/account.go)与
[Space 服务](../../internal/service/space/service.go)。

`buildmax admin` 提供经过身份认证的管理员、账号和模型目录操作；
`buildmax-server` 保留直连数据库的引导与恢复命令。
模型凭证由部署级密钥加密；未配置加密时拒绝存储带凭证的模型。
Space Secret 与 Agent Secret 使用声明也有存储和 worker 投递实现，采用 Run 级授权。
这些实现不能将已投递的 Secret 与消费它的 worker 进程隔离。

系统管理、配额、审计、角色检查和 Space 生命周期 UI 已存在。
管理方面仍缺少权限变更的事务性审计、管理 CLI 的 Session 列出/撤销能力对齐、
配额层级分配，以及诊断队列与 worker 的运行元数据。
这些记录在[系统管理操作提案](proposals/system-administration-operations.md)中；
提案状态不能当成已实现功能。插件发布仍仅通过 CLI，Portal 已能检查、退役、恢复插件
以及撤回发布版本。

Space 审批流程仍未实现且明确不在范围内；这不能被视为邀请或所有权转移功能未完成。

Workflow 定义仍是线性的 `agent_task` 步骤，具有版本化定义和持久 Run/步骤记录，
但定义契约没有分支、并行图、人工审批、循环或类型化输入/输出映射
（[Workflow 契约](../../internal/core/workflow/workflow.go)）。

Portal 与入站 webhook 执行已组装。Telegram 仍只是渠道词汇，
webhook 回调发送器未组装进 Server。周期性 schedule 通过 `schedule` 触发来源与
`/api/spaces/{space_id}/schedules` API 在 Task 平面上运行 Agent，由常驻循环分发：
每个到期时刻跨副本只认领一次，错过的触发合并为一次补触发，连续五次触发失败或
创建者被禁用时暂停该 schedule。Portal 在 Agent 详情页创建和管理 schedule，并在
Schedules 页面列出 Space 内的全部 schedule；暂停原因只写日志，不展示。它们不是
对话渠道（[`internal/core/schedule`](../../internal/core/schedule/schedule.go)、
[`internal/server/scheduler`](../../internal/server/scheduler)、
[设计记录](design/定时Agent执行.md)）。Space 插件激活支持 skill/subagent 内容，
但拒绝包含 hook 或 MCP 服务的发布版本
（[激活服务](../../internal/service/plugin/activation.go)）。前台 Conversation 不加载 Space 插件。

## 资格验证与运行证据

评估契约、构建产物驱动的本地和 worker 适配器、grader、重复/配对实验及固定版本的
Harbor 适配器均已实现。[evaluation/suite](../../evaluation/suite)包含三个自有任务。
这个范围不能验证所有受支持界面。历史 oracle/canary 报告不是当前修订的基准结果；
本次未运行真实模型评估或 Terminal-Bench 协议，不报告分数。

Portal 浏览器测试现已覆盖独立 Task 线程、工作区、规范 Space 路由、加载/错误/权限状态、
响应式布局、无障碍与运行来源。Desktop 有 bridge 与
[浏览器 UI 测试](../../desktop/frontend/e2e)，并在 macOS 和 Windows CI 中运行打包应用
启动冒烟。启动冒烟证明构建出的 bundle 能启动并短暂存活，但不会驱动或视觉检查原生窗口。
Portal 路由仍采用直接导入，当前源码没有路由级懒加载。本次未重新测量 bundle 大小或吞吐量。

部署冒烟包含重试、托管推理及调用账本、运行中 worker 的取消、worker 丢失恢复
（一次 worker 在执行到一半被删除的 Run，会落到一个可诊断的终态 FAILED 并且仍可取回）、
数据库中断的降级与恢复（运行期丢失 MySQL 会让 /readyz 报告数据库故障、把服务端移出
Service 而不重启它，且访问一旦恢复它会自行恢复）、对象存储的降级与恢复（运行期丢失桶同理：
/readyz 的对象存储检查失败，并在桶完好的情况下恢复）和 Bash 隔离探针。调度器单元测试覆盖
失联 Run 处理和清理，包括了结一次 worker 已经静默的 Run 的存活性巡检——这条硬丢失路径
无法从部署侧复现，因为内核不会把容器内发给 PID 1 的 SIGKILL 投递给它，而 kubelet 的任何
删除都从 worker 会据以自报的 SIGTERM 开始。这些不等于候选版本已经演练 worker 在存储被拒时的
对象存储写路径、配对恢复、凭证轮换和模式回滚。

Compose、kind、生产 Kubernetes 清单、发布验证、SBOM、镜像扫描及来源证明工作流已存在。
它们的存在不能替代尚未签署的 [Beta 就绪记录](deploy/beta-readiness.md)。

## 本次复核的验证

本次是源码与测试复核，不是重新进行部署资格验证。
最新 `main` 在 `0bd7e5bf` 上的 CI、CodeQL、Windows 与部署冒烟工作流均已通过。
本次文档更新在本地通过了 `./make check docs`、`./make check portal`、
`./make test ./internal/architecture`、注释有改动的 Go 包测试以及
`git diff --check`。文档检查覆盖链接与格式，不证明运行时行为。

本次未运行真实 MySQL 测试（未提供 `BUILDMAX_TEST_DSN`）、全量构建、
前端/浏览器测试、Compose/kind 部署冒烟、外部恢复演练或付费模型评估。
上文数据库测试的断言经过阅读，未宣称实际执行。
因此，没有将历史覆盖率或部署结果沿用为当前测量值。
