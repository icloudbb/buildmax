# 长时间运行的工作区 Environment

> **翻译说明：** 本文是[英文原文](../../proposals/long-running-workspace-environments.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
>
> **受众：** 贡献者、产品设计者与运维人员 · **状态：** 提案——讨论中
>
> **发起日期：** 2026-10-01
>
> **主要领域：** 产品与执行模型

相关文档：[路线图](../ROADMAP.md)、[当前状态](../current-state.md)、
[Agent 执行与 Task 线程](../design/Agent执行与Task线程.md)、
[Task 工作区检查点](../design/Task工作区检查点.md)、
[Remote Control](../design/远程控制.md)、
[Agent 沙箱策略](../design/Agent沙箱策略.md)、
[插件分发](../design/Space插件分发.md)、
[客户端界面收敛](client-surface-convergence.md)以及
[Server 架构](../contribute/architecture/server.md)。

## 目录

- [1. 决策问题](#1-决策问题)
- [2. 用户结果与证据](#2-用户结果与证据)
- [3. 当前约束](#3-当前约束)
- [4. 目标与非目标](#4-目标与非目标)
- [5. 建议模型](#5-建议模型)
- [6. 生命周期与持久化](#6-生命周期与持久化)
- [7. 通过 Remote Control 交互](#7-通过-remote-control-交互)
- [8. 授权与信任边界](#8-授权与信任边界)
- [9. 供应、协调与失败](#9-供应协调与失败)
- [10. API、Portal 与运维界面](#10-apiportal-与运维界面)
- [11. 方案与权衡](#11-方案与权衡)
- [12. 最小验证切片](#12-最小验证切片)
- [13. 开放问题与决策证据](#13-开放问题与决策证据)
- [14. 获采纳后的可能归宿](#14-获采纳后的可能归宿)

## 1. 决策问题

BuildMax 是否应该增加一个独立、归属 Space 的 **Environment 执行平面**，用于
长时间运行的 Agent 工作区，同时复用 Remote Control 完成实时交互？

本文建议验证这个方向。Environment 不是一个永久不结束的 TaskRun，也不替代
Task/TaskRun。它拥有已供应的计算资源、持久的私有工作区，以及恢复交互式 Agent
Session 所需的运行时状态。Task/TaskRun 继续作为定时任务、Workflow、Issue 和
后台工作所使用的持久化、有界执行平面。

第一版产品应当收窄为托管在远端的 Agent Session，而不是完整的浏览器 IDE。
一个 Environment 在一个私有工作区上运行一个受监督的交互式 Agent Session；
Portal 通过 Remote Control 的交互通道观察并引导它。终端、任意应用、协同编辑
与服务公开访问都需要各自的证据。

## 2. 用户结果与证据

用户应能创建 Environment，关闭浏览器，并在数小时或数天后回到同一个工作区和
Agent 历史，而不需要假装一个 TaskRun 一直在执行。Environment 运行期间，测试
服务器、watcher、下载或其他后台进程可以在浏览器断开后继续运行。计算资源停止
或被替换后，文件系统与 Agent Session 仍可恢复，但进程不可恢复。

动机场景包括：工作跨越多轮交互、需要保持热状态的工具链或代码仓库，或者依赖
生命周期长于一次有界 Agent turn 的进程。现有
[客户端界面收敛提案](client-surface-convergence.md)已识别出同一种云端形态；已经
交付的 Remote Control 证明 BuildMax 能通过 Portal 中继一个在线 Agent Session。
但两者都不能证明需求频率足以承担存储、隔离与运维成本。促成本文的需求属于产品
证据，而不是经过测量的使用证据。

因此，第一次决策需要一个有界原型和实际旅程观察，而不是通用远程开发平台。
有价值的证据包括：用户是否会回到同一 Environment、真正重要的是保留进程还是
仅保留文件、资源实际会保持多久，以及没有完整终端时，收窄的 Agent 界面是否足够。

## 3. 当前约束

- **Task/TaskRun 有意保持有界。** TaskRun 物化工作区，完成一次 turn 或 attempt，
  提交结果与检查点后终止。Task 的连续性来自持久状态，而不是常驻进程。保持一个
  TaskRun 永不结束，会削弱其取消、结果、配额、恢复与 worker 回收语义。
- **Worker 计算资源是临时的。** 受支持的 Kubernetes 路径为一次运行启动一个 Job。
  可写根目录属于该次运行；持久边界是对象存储中的 Task 工作区检查点，而不是 Pod
  卷。
- **Remote Control 提供实时交互，而不是托管。** 它注册归属账户的本地 Session，
  中继有界事件、prompt、approval、question 与 cancel，不保存权威 transcript。
  它使用用户 JWT 和账户归属，这适合个人笔记本，不适合归属 Space 的远端
  Environment。
- **Space 是 Portal 的所有权与授权边界。** 服务端 Environment 必须且只能归属一个
  Space。个人使用仍落在 personal Space 中，无需另一套产品模型。
- **运行时已经共享。** `internal/agentapp` 为现有各个界面组装 model、tool、MCP、
  hook、sandbox、trace、Skill、Session 和工作区解析。本文增加的是 host 生命周期，
  而不是另一套 Agent loop。
- **网络可达执行采用 worker 的信任姿态，而不是本地 CLI 的姿态。** 沙箱必须
  fail-closed；运行时状态与凭证必须位于工具可写工作区之外；缺少必要边界时应让
  Environment 不可用，不能静默扩大访问权限。
- **这些能力目前均未交付。** 当前没有 Environment 实体、provisioner、持久化
  Environment 卷、lease、Environment 凭证或 Portal 管理界面。

## 4. 目标与非目标

### 4.1 目标

- 在浏览器断开和计算资源重启后，继续使用同一个私有工作区及其 Agent Session。
- Environment 处于 ready 时，即使没有浏览器连接，后台进程也能继续运行。
- 为创建、启动、停止、续租、失败和删除提供明确、可查看的状态，并约束资源使用。
- 复用 Remote Control 的类型化事件与命令通道，承载对话、流式输出、approval、
  question、cancel、presence 与 reconnect。
- 保持 Space 授权、显式 Plugin 激活、托管推理、trace 脱敏和 fail-closed 沙箱。
- 向用户和运维人员明确区分哪些状态持久、哪些状态不持久。

### 4.2 非目标

- 让 TaskRun 永久运行、把普通 Task 调度到 Environment，或改变 Task 结果的权威性。
- 保证进程跨越停止、挂起、节点丢失、镜像替换或控制平面恢复而继续存活。
- 同步用户笔记本文件系统，或静默把 Environment 变更写回可变的 Space 文件。
- 交付完整远程桌面、VS Code 替代品、任意浏览器应用，或向公网暴露 Environment
  内部进程。
- 提供协同 shell，或让所有 Space 成员天然成为另一位成员 Environment 的操作者。
- 仅仅因为 Remote Control 中继了交互，就在 Server 上持久保存 transcript。
- 第一阶段支持所有部署拓扑。无法强制执行必要隔离与持久化的拓扑，应报告能力
  不可用。

## 5. 建议模型

本文只增加一个持久化产品实体：**Environment**。它是一个归属 Space 的资源预约，
拥有私有工作区以及可能挂载到工作区的计算资源。它不是 Agent、Task、TaskRun、
本地 Project 或通用的版本化文件系统。

| 概念 | 拥有什么 | 不拥有什么 |
|---|---|---|
| Environment | Space、交互操作者、期望与观测到的生命周期、资源规格、lease、持久工作区引用、host 健康状态 | Task 结果、无限期运行的模型调用或公开服务端点 |
| Environment host | allocation 内的协调心跳与一个受监督的交互式 Agent runtime | 持久化产品身份或授权策略 |
| Agent Session | 存于 Environment runtime home 的模型可见历史与 compaction 状态 | Environment 生命周期或 Space membership |
| Remote Control 注册 | 在线 Agent Session 的临时 presence、stream key 与命令路由 | 工作区或 transcript 持久性 |

Environment 归属一个 Space，第一阶段只有一个交互操作者。操作者必须持续拥有该
Space 的 membership。Space owner 与 admin 可以出于治理目的停止或删除
Environment，但不会因此静默取得 shell 或 Agent Session 访问权。若证据证明需要
共享，应在将来增加显式 grant；Space membership 本身不代表共享。

allocation 包含三个具有强制边界的文件系统区域：

```text
持久 workspace/         Agent 可见，也是工具唯一可写根目录
持久 buildmax-home/     Session、trace、setting、解析后的 Plugin；对工具隐藏
临时 scratch/           socket、cache 与进程本地临时状态
```

`buildmax-home/` 需要持久化，因为用户结果包含 Agent Session 连续性，但它不能位于
可写工作区之下。Environment host 是 allocation 内的进程，不是第二个 Server，也
不是新的 domain 实体。第一阶段只监督一个 Agent Session；多 Session 并发会引入
尚未被需求证明的调度、资源和展示语义。

## 6. 生命周期与持久化

Environment 具有异步的期望状态和观测状态。具体存储表示属于后续设计，但用户
可见生命周期至少要区分：

```text
create -> provisioning -> ready <-> stopping -> stopped
                         |                    |
                         +------ failed <-----+

stopped / failed -> deleting -> gone
```

- **Create** 先预约资源并供应存储，然后才能报告 ready。请求成功返回不代表计算
  资源已经就绪。
- **Ready** 表示 host 已连接，且所需 sandbox、storage 与 Server 通道通过启动检查；
  浏览器是否在线与此无关。
- **Stop** 在宽限期后终止计算资源，保留持久 workspace 与 runtime home；它不承诺
  挂起进程。
- **Start** 把计算资源挂载到同一份持久状态，并在接受 prompt 前恢复 Agent Session。
  恢复失败必须可见且 fail-closed，不能以旧身份静默新建 Session。
- **Delete** 先使计算资源不可达，再依据明确的 retention policy 销毁 Environment
  持久存储。Delete 与 Stop 不同，Portal 必须要求破坏性操作确认。

“长时间运行”表示计算资源可以跨越多轮交互与浏览器断开保持 ready，并不表示无限
或永生。每个活动 Environment 都有可续期的 wall-clock lease。经过认证的使用或
显式续租，可以在 Space 与部署限制内延长 lease。host heartbeat 本身不能续租，
否则每个被遗忘的 Environment 都会变成永久预约。第一阶段应使用明确的到期时间，
而不是根据 CPU 或终端活动猜测 idle。

持久化承诺有意保持收窄：

| 事件 | Workspace 与 Agent Session | 后台进程 |
|---|---|---|
| 浏览器断开或 Server replica 变化 | 保留 | 继续运行 |
| 同一 allocation 内 host 进程干净重启 | 保留 | 可能丢失 |
| Stop 后 Start，或重新挂载存储的 workload 替换 | 保留 | 丢失 |
| 存储丢失 | 除非将来提供备份策略，否则不可用 | 丢失 |
| 超过 retention 边界后的 Delete | 销毁 | 丢失 |

## 7. 通过 Remote Control 交互

Remote Control 是交互底座，但有一条重要规则：**复用协议与 relay，不复用其账户
归属模型**。

Environment host 向 Server 建立出站 WebSocket，并复用现有类型化 envelope、
有界且脱敏的 run event、heartbeat、buffered stream、跨 replica 命令路由，以及
入站 prompt、approval、question 与 cancel 消息。同一套 Portal 组件可以展示
stream 与待处理决策。出站连接也避免把每个 Environment 变成网络可直接访问的
Server。

它与笔记本 Session 的 admission 路径不同：

- host 使用绑定到单个 Environment 与 Space 的短期、可撤销 Environment 凭证，
  不能使用创建者的 refresh token 或通用 worker token；
- 注册将在线 Session 关联到 Environment；浏览器操作先通过 Environment 与 Space
  membership 授权，再进入共享 relay；
- 现有 account-scoped RemoteSession 规则继续适用于本地设备。实现可以泛化在线
  Session registry，但不能让 Space membership 变成进入成员笔记本的路径；
- Environment readiness 与在线 Agent Session presence 是两项独立事实。
  Environment 可以在 Agent runtime 重启时仍然健康；Session offline 本身不能授权
  资源回收。

现有 Remote Control 只回放一段很短的在线 buffer，并且有意不保存持久 transcript。
因此，Environment 必须从自身持久化 Agent Session 恢复历史。最小扩展是在 viewer
attach 时由 Environment host 生成一个有界 history snapshot 或 replay；Server 仍是
relay，而不是第二个 transcript store。原型必须证明：普通 relay buffer 过期后，
页面 reload 仍能重建可理解的 Session。

这种复用形成了 [Remote Control 设计](../design/远程控制.md)已识别的窄云端象限：
cloud host 加 Agent Session 界面。更宽的 codespace 界面——终端、文件、任意应用——
只有在收窄 Environment 被证明有价值后才应增加。

## 8. 授权与信任边界

Environment 执行模型选定命令的时间远长于 worker Job。时间会扩大暴露面，并不构成
放松边界的理由。

- **授权：** 每个控制操作都解析 Environment 的 Space。交互操作者仍为成员时才能
  attach。owner/admin 可以治理生命周期和配额，但不会得到 transcript 或 shell
  访问权。操作者退出 Space 或被停用后，应撤销新的交互并请求停止；具体宽限策略
  仍是开放问题。
- **凭证：** host 只获得一个窄 scope 的 Environment 凭证，并通过控制平面交换或
  续期。它不获得数据库、对象存储、模型服务商、用户 refresh token 或集群凭证。
- **推理与应用：** 托管推理和未来的应用 broker 继续由 Server 中介。不能为了保持
  Environment 温热，就把 secret 物化到 Agent 可见工作区。
- **文件系统：** tool 只看到 `workspace/`。持久 runtime home 与临时控制文件位于
  tool root 之外，与 worker invariant 一致。
- **sandbox 与外层 runtime：** 必需的命令 confinement 默认开启并 fail-closed。
  workload 还需要经过验证的 Pod/container 边界、资源限制、只读镜像根与网络策略。
  模型不能选择或削弱这些控制。
- **Plugin、hook 与 MCP：** 只有 Space 显式、由 Server 解析的 Plugin activation
  可以进入 Environment。解析发生在明确边界，例如 Environment start 或新 Agent
  Session start；运行中的进程不 hot-load。缺少 confinement 方案时，不受支持的
  stdio MCP 继续 fail-closed 禁用。
- **audit 与 trace：** create/start/stop/renew/delete 与 Remote Control 操作记录 actor
  和 Environment。Agent 执行在 Environment runtime home 中保存有界、脱敏 trace。
  relay 故障对 Agent run 仍 fail-open；凭证或沙箱故障对 Environment readiness
  fail-closed。

向公网暴露 workspace 内进程并不是 Remote Control 的小扩展。它引入 routing、TLS、
认证、滥用防护、hostname 与数据泄漏策略，继续保持非目标。

## 9. 供应、协调与失败

供应是持久化 reconciliation 问题，而不是一个长时间不返回的 HTTP 请求。Server
记录期望状态，controller 收敛计算与存储，Environment host 上报健康。Server 重启
不能终止健康 Environment；callback 丢失也不能让资源永久滞留在 `provisioning`
或 `stopping`。

第一个受支持的部署应为每个 Environment 使用一个隔离 Kubernetes workload 和一个
持久卷。这符合私有部署拓扑，也使 CPU、memory、storage、security context、network
policy 与回收可检查。它不要求复用 Task worker Job 或其 run token。本地进程原型
可以测试交互，但不能作为受支持多租户边界的证据。

reconciliation 至少需要处理：

| 故障 | 必要结果 |
|---|---|
| host ready 前 provisioning 失败 | Environment 进入可诊断的 `failed`；retry 不会创建重复存储或 workload |
| workload 仍在但 host heartbeat 超时 | Environment 变为 unavailable；controller 检查或重启 workload，不删除持久状态 |
| workload 或节点消失 | replacement 重新挂载同一存储；明确报告进程丢失；Session restore 是 readiness gate |
| Server 重启或更换 replica | workload 继续运行；registration 重连；持久化期望状态驱动 reconciliation |
| Stop 与 Start 或 Delete 竞争 | 一个串行化的期望状态获胜；过期 callback 不能复活计算资源 |
| lease 到期 | 即使没有浏览器连接，也请求并最终强制 Stop |
| 存储无法挂载或恢复 | 不报告 ready；错误指出失败边界 |
| Delete 只完成一部分 | reconciliation 持续进行，直到计算不可达，并且 retention/storage cleanup 到达有记录的终态 |

controller 还需要双向 orphan detection：有数据库 row 但无 workload 时需要协调；有
labelled workload 或 volume 但无存活 Environment row 时，应根据文档化策略隔离并回收。
恢复绝不自动重放 Agent Task；Environment 是交互状态，而不是幂等 Job。

## 10. API、Portal 与运维界面

后续设计可以调整命名，但能力至少需要等价的 Space-scoped 操作：

```text
POST   /api/spaces/{space_id}/environments
GET    /api/spaces/{space_id}/environments
GET    /api/spaces/{space_id}/environments/{environment_id}
POST   /api/spaces/{space_id}/environments/{environment_id}/start
POST   /api/spaces/{space_id}/environments/{environment_id}/stop
POST   /api/spaces/{space_id}/environments/{environment_id}/lease
DELETE /api/spaces/{space_id}/environments/{environment_id}
```

创建和生命周期命令应幂等并返回持久资源；客户端观察 readiness，而不是让请求一直
保持。Session stream 与命令操作应在 Environment 授权后委托给泛化的 Remote
Control 通道，不能新建并行的非类型化 chat 协议。

Portal 第一阶段只需要三个界面：

1. Environment 列表，展示 operator、lifecycle、lease expiry、resource profile、
   最新 host signal 与清晰的运行成本提示；
2. Environment 详情页，包含 Start、Stop、Renew、Delete、diagnostics 与内嵌的 Remote
   Control Agent Session 视图；
3. 管理界面，展示 active count、资源总量、failure、expired lease，并允许在无法查看
   transcript 的前提下强制 stop/delete。

运维配置需要显式 enablement flag、镜像与 resource profile、最大 active Environment
数量、每 Space 限制、lease 边界、storage class 与大小、startup timeout，以及必要
runtime/sandbox policy。默认值不得静默分配无界计算资源。不受支持的部署应报告功能
不可用，而不是以无限运行的本地 worker 模拟。

## 11. 方案与权衡

| 方案 | 有价值的属性 | 成本或失败 |
|---|---|---|
| **A. 独立 Environment 平面加共享 Remote Control（建议验证）** | 符合持久工作区和交互生命周期；复用已交付控制通道而不改变 Task 语义 | 增加已供应计算、持久存储、reconciliation、quota 与更强的长时间信任边界 |
| **B. 保持一个 TaskRun 永不结束** | 表面上的 schema 变更最少 | 没有权威终态结果；lease 与交互成为 worker 特例；worker 丢失语义含混；TaskRun 回收不再表达原意 |
| **C. 在临时 worker 上通过 checkpoint Continue** | 复用持久 Task 模型，idle 时零计算成本 | 保留文件和 Agent 历史，但不保留后台进程或 warm state；每次仍是有界 turn |
| **D. 集成外部 codespace 服务商** | 把 provisioning 和浏览器 IDE 外包 | 在证明窄 Agent 界面有价值前，先引入服务商凭证、可用性、成本和可移植性约束 |
| **E. 在 Server 或共享 worker 内运行长时间 Session** | 避免逐 Environment workload | 把不可信执行与控制平面或租户混合，削弱资源隔离，让一次失败影响无关 Environment |

方案 C 已经服务于只需要 turn 间连续性的工作。只有在保留进程、warm state 或即时
交互式回归具有实质价值时，新实体才成立。这是方案 A 的核心证据检验。

## 12. 最小验证切片

第一个原型应有意省略终端和浏览器 IDE 功能。在一个隔离的 Kubernetes 测试部署中，
它应当：

1. 从一个 Space 创建一个 Environment，通过持久 reconciliation 到达 `ready`；
2. 在私有工作区中启动或恢复一个交互式 Agent Session；
3. 用 Remote Control stream 发送 prompt、观察输出、回答一次 approval 或 question，
   并取消一个 turn；
4. 断开 Portal，让一个有界后台进程继续运行；在普通 relay buffer 已消失后重新连接，
   恢复可理解的 Session 与同一工作区；
5. Stop 再 Start，证明文件和 Agent 历史保留，同时明确报告后台进程丢失；
6. 分别重启 Server 和杀死 Environment workload，证明文档规定的 reconciliation 与
   持久化结果；
7. 证明跨 Space 和非 operator 交互被拒、凭证撤销、quota 拒绝、lease 到期与
   fail-closed 沙箱启动；
8. 删除 Environment，并验证计算与存储已回收，且没有 orphaned 外部可达 Session。

原型应记录 time-to-ready、reconnect 时间、active duration、storage growth、
restart/restore failure、计算成本，以及哪些操作使用户真正需要终端。还应通过 Task
Continue 完成同一个多轮任务作对比。如果保留进程或 warm state 不改变结果，现有
Task 平面更简单，应继续作为答案。

这个切片不会带来用户文档、兼容性承诺或 GA 声明。它的用途是决定 Environment
平面是否值得获得产品与运维所有权。

## 13. 开放问题与决策证据

- 第一个 workspace 由什么初始化：Space 文件快照、repository clone，还是显式上传？
  是否有路径确实需要写回，还是通过 Artifact 导出已经足够？
- 每个 Environment 一个交互 operator 是否足够？哪个被证明的旅程需要显式共享？
  admin 在不能取得 Session 访问权时，必须能检查什么？
- 哪种 lease 最小值、最大值、预警与续期策略符合观察到的工作，同时不会使遗忘的
  计算资源永久存在？
- host 生成的有界 history snapshot 是否足以 reconnect，还是需求实际上指向独立的
  [持久化 Agent Session 提案](durable-agent-sessions.md)？
- 哪些 Plugin 与 workspace 变更需要重启 Agent Session，哪些需要重启整个 Environment？
- 第一种受支持部署能诚实提供哪些 storage durability、backup、retention 与 deletion
  guarantee？
- 受支持边界是否要求 gVisor 或其他 outer runtime？准确的 Environment image 与嵌套
  command sandbox 能否不靠例外通过 trust harness？
- operator 失去 Space membership 或被停用时，运行中的计算应立即停止，还是提供短暂
  恢复窗口？
- 证据是否支持增加 terminal 和 file browser，还是收窄的 Remote Control 界面已经
  覆盖真实用户结果？
- 是否存在让 Task 定向到 Environment 的合理原因？这会不会重新引入两个执行权威，
  并混淆 TaskRun recovery？

决定继续前进，至少需要：一个已命名、且 Task Continue 无法满足的用户旅程；受支持
部署拓扑上的生命周期和隔离证据；经过测量的资源 envelope；以及明确的回收与事件
响应 owner。单纯展示一个持久 Pod 的技术 demo 并不足够。

## 14. 获采纳后的可能归宿

若获采纳，稳定边界——Environment 作为 Space-scoped 执行平面、与 Task/TaskRun
分离、复用 Remote Control、生命周期、持久化与信任模型——移入“产品与执行模型”
设计记录。只有当实现细节足以支撑独立文档时，Kubernetes provisioning 与运维策略
才另建“运维与部署”规范。

路线图随后先安排收窄的 cloud Agent Session，再考虑宽 codespace 界面；只有验证
门槛与安全边界获采纳后，拆解后的实现工作才进入
[backlog](../../backlog/README.md)。客户端界面提案继续拥有共享 UI 与传输收敛，不拥有
Environment 生命周期。本提案的已采纳理由迁移后即删除；若证据支持 Task Continue
或外部服务商，则无替代地删除。
