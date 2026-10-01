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
[客户端模式](../design/客户端模式.md)、
[Agent 浏览器能力](../design/Agent 浏览器能力.md)、
[Agent 沙箱策略](../design/Agent沙箱策略.md)、
[插件分发](../design/Space插件分发.md)、
[客户端界面收敛](client-surface-convergence.md)以及
[Server 架构](../contribute/architecture/server.md)。

## 目录

- [1. 决策问题](#1-决策问题)
- [2. 定位：属于 Agent 的云端机器](#2-定位属于-agent-的云端机器)
- [3. 用户结果与证据](#3-用户结果与证据)
- [4. 当前约束](#4-当前约束)
- [5. 目标与非目标](#5-目标与非目标)
- [6. 建议模型](#6-建议模型)
- [7. 归属与授权](#7-归属与授权)
- [8. 机器生命周期与持久化](#8-机器生命周期与持久化)
- [9. 通过 Remote Control 交互](#9-通过-remote-control-交互)
- [10. 信任边界](#10-信任边界)
- [11. 供应、协调与失败](#11-供应协调与失败)
- [12. API、Portal 与运维界面](#12-apiportal-与运维界面)
- [13. 方案与权衡](#13-方案与权衡)
- [14. 最小验证切片](#14-最小验证切片)
- [15. 开放问题与决策证据](#15-开放问题与决策证据)
- [16. 获采纳后的可能归宿](#16-获采纳后的可能归宿)

## 1. 决策问题

BuildMax 是否应该分配并管理长时间运行的通用云端机器——**Environment**——让用户只
通过 Agent 对话让它工作，并经由现有的 Remote Control 通道接入？

本文建议验证这个方向。系统新增的职责是机器管理：分配、启动、停止、续租、回收和
删除一台带持久工作区的机器。机器运行起来以后，用法与用户自己那台开启 Remote
Control 的笔记本完全相同：机器上运行标准的 `buildmax` 运行时，注册一个实时
Session，Portal 观察并引导这个 Session。不引入新的 Agent 循环、聊天协议或执行
平面，Task/TaskRun 保持不变。

## 2. 定位：属于 Agent 的云端机器

BuildMax 当前有两种工作模式：

| 模式 | 由谁执行工作 | 机器 | 交互方式 |
|---|---|---|---|
| Task 与 Workflow | Server 把有界的 TaskRun 派发到临时 worker | 按次分配，运行结束后回收 | 提交、观察、继续；Run 的结果是权威结果 |
| Remote Control | 用户自己的长时间运行机器 | 用户的笔记本，BuildMax 不管理它 | 经 Server 中继的实时 Agent Session |
| **Environment（本提案）** | 系统分配的长时间运行机器 | 云端机器——初期为一个带持久卷的 Kubernetes Pod | 与笔记本相同的 Remote Control Session |

Environment 是一台位于云端、属于 Agent 的通用计算机：用户描述要做的工作，Agent
操作机器去完成。BuildMax 是通用 Agent 运行时，因此工作并不限于软件开发，还包括：
采集与分析数据并产出报告，借助浏览器在 Web 上做调研和其他工作，批量处理文档或
媒体，长时间的下载、转换与计算，以及构建或运行软件。

云 IDE——Cloud9、Codespaces、Gitpod——是机器管理这一半的先例，而不是产品范围。其
生命周期原样沿用：从某个来源创建工作区、机器可以停止而磁盘保留、闲置回收、配额与
删除。变化的是交互方式。云 IDE 给开发者一个编辑器和一个终端，让他们亲手操作机器。
现在 Agent 能替任何人完成这些操作，因此交互界面收缩为 Agent 对话：提示、流式进度、
审批、提问、取消，以及对 Agent 产出的只读审阅。

由此得出两个结论，决定了本文其余部分的形态：

- **机器管理是已知问题。** 生命周期、持久化、配额与回收沿用成熟的云 IDE 实践，
  而不是重新设计。
- **交互已经存在。** Remote Control 本来就是为引导一台 BuildMax 无法直接触达的
  长时间运行机器而建。云端机器同样是长时间运行的机器，复用这条通道正是要点，而非
  优化。

## 3. 用户结果与证据

用户应能创建 Environment，让其中的 Agent 开展工作，关闭浏览器，并在数小时或数天
后回到同一个工作区和同一段对话。Environment 运行期间，Agent 启动的抓取、分析、
下载、构建或服务无论是否有人在看都会继续运行。机器停止或被
替换后，文件与 Agent Session 得以保留，进程则不会。

动机场景包括：工作跨越多轮交互、需要保持热状态——已安装的工具、已下载的数据、
已准备好的代码仓库——或者依赖生命周期长于一次有界 Agent turn 的进程——恰好是
用户原本需要让笔记本开着 Remote
Control 的那些场景。云端机器把笔记本从这个图景中移除。

现有证据是一项产品需求加上已上线的 Remote Control，而不是实测使用数据。因此决策
需要一个有界原型和可观察的用户旅程：用户是否会重新连接到同一个 Environment，保留
进程还是只保留文件更重要，Environment 实际保持活跃多久，以及没有终端时只读审阅是否
足够。

## 4. 当前约束

- **Task/TaskRun 被刻意设计为有界。** 一个 TaskRun 物化工作区，执行一个 turn 或
  一次尝试，提交结果与检查点状态，然后终止。让它常驻会破坏其取消、结果、配额、
  恢复与回收语义。
- **Worker 计算资源是临时的。** 受支持的 Kubernetes 路径每次 Run 启动一个 Job；
  持久化边界是对象存储中的 Task 工作区检查点，而不是 Pod 卷。Server 目前只管理
  Job，不管理长期工作负载或持久卷。
- **Remote Control 归属账号，并且由 TUI 显式开启。** 一个实时 Session 属于一个
  用户，只有该用户能访问；它通过交互式 TUI 的 `--remote-control` 参数、在登录托管
  Server 的情况下开启。Server 只保留一个短的回放缓冲，从不保存持久的对话记录。
  目前没有无界面（headless）承载 Remote Control Session 的方式。
- **Space 是 Portal 中资源、配额与治理的归属边界。** Remote Control 是一个经过
  记录的有意例外，因为笔记本没有 Space。
- **运行时已经共享。** `internal/agentapp` 为所有界面组装模型、工具、MCP、hooks、
  沙箱、trace、Skills、Session 与工作区解析。托管模式已经经由 Server 路由推理。
- **浏览器能力仅限本地。** CLI 与 Desktop 驱动一个由 Go 管理的 Chromium；worker
  与其他无人值守的 Run 没有这项能力。
- **外部凭证只通过显式授权进入 Run。** Worker 通过 Run 级授权获得 Space Secret；
  不继承任何环境中的隐式凭证。
- **可经网络访问的执行采用 worker 的信任姿态。** 沙箱强制 fail-closed，运行时状态
  与凭证保留在工具可写工作区之外。
- **目前没有任何类似能力上线。** 不存在 Environment 实体、供应器、持久卷、租约、
  机器凭证或管理界面。

## 5. 目标与非目标

### 5.1 目标

- 以显式、可检查的状态和有界的资源使用，分配、启动、停止、续租、回收与删除云端
  机器。
- Environment 保持持久，而闲置时计算资源缩到零；已停止的 Environment 只占存储，
  不占逐 Environment 的 CPU 或内存。
- 让一个私有工作区及其 Agent Session 在浏览器断开和机器重启后仍可使用。
- 机器运行期间，Agent 启动的进程持续运行，与是否有人观看无关。
- 只通过 Remote Control Session 交互，让云端机器和笔记本在 Portal 中的外观与行为
  一致。
- 保持显式 Plugin 激活、托管推理、trace 脱敏与 fail-closed 的沙箱强制。
- 让持久与非持久状态对用户和运维人员一目了然。

### 5.2 非目标

- **为用户提供直接操作机器的编辑器、终端或文件浏览器。** 这是产品原则而非推迟：
  Agent 就是用户的手。用户仍以只读方式审阅结果（§9.3）。
- 让 TaskRun 永久运行，或让 Task 以 Environment 为执行目标。这会产生两个执行权威，
  并使 TaskRun 恢复语义含混。
- 保证进程在停止、节点丢失、镜像替换或控制面恢复后存活。
- 同步笔记本文件系统，或把 Environment 的修改写回可变的 Space 文件。结果以 Artifact
  形式离开机器；当工作是一个代码仓库时，也可以通过 Git。
- 对机器内进程提供公网入口或端口转发。Agent 改用浏览器能力查看 Web 内容，包括
  它自己启动的服务（§9.3）。
- 共享交互访问，或让 Space 成员身份隐含对其他成员 Environment 的访问。
- 在 Server 上持久保存交互对话记录。
- 支持所有部署拓扑。无法强制下文所述隔离与持久化的拓扑应报告该能力不可用。

## 6. 建议模型

本提案只新增一个持久实体 **Environment**：一台已分配云端机器的记录。它拥有机器的
生命周期，从不拥有 Agent 的行为。

| 概念 | 拥有 | 不拥有 |
|---|---|---|
| Environment | Space、操作者、期望与观测到的生命周期、资源规格、租约、工作区来源、持久卷引用、机器健康 | Agent Session、Task 结果或公网端点 |
| 机器 | 标准的 `buildmax` 运行时，以无界面方式承载一个 Remote Control Session | 产品身份或授权策略 |
| Agent Session | 模型可见的历史与压缩状态，保存在机器的运行时 home 中 | 机器生命周期 |
| Remote Control Session | 实时 Session 的在线状态、流与命令路由 | 工作区或对话记录的持久性 |

机器内运行的是用户在笔记本上运行的同一个运行时，处于托管模式并开启 Remote
Control。没有单独的"Environment host"组件：笔记本需要用户手动启动的东西，机器在
开机时自行启动。唯一新增的运行时能力是一种无需 TUI 即可承载一个 Remote Control
Session 的无界面模式——Remote Control 设计已经把 Desktop 与 print 界面的开启方式
列为可追加项。

机器有三个边界受强制的文件系统区域：

```text
persistent workspace/       Agent-visible, the only writable tool root
persistent buildmax-home/   sessions, traces, settings, resolved Plugins; hidden from tools
ephemeral scratch/          sockets, caches, process-local temporary state
```

`buildmax-home/` 需要持久化，因为目标结果包含 Session 连续性；它保留在工具根目录
之外，与 worker 不变量一致。机器只承载一个 Session；并发 Session 会引入调度与展示
语义，而目前没有证据表明需要它们。

工作区默认为空，由 Agent 按工作需要自行获取内容。创建时可以选择用 Git 仓库、Space
文件快照或上传的文件初始化。不做任何自动写回；Agent 发布 Artifact，当工作是代码
仓库时也可以推送到 Git。

## 7. 归属与授权

这是该模型引出的主要决策。开启 Remote Control 的笔记本归属账号，没有 Space。云端
机器消耗运营方的资源，需要配额、成本归属与治理，而 Portal 把这些放在 Space 上。
有两种形态：

| 方案 | 特性 | 代价 |
|---|---|---|
| **归属账号**，与笔记本相同 | 最接近"我的另一台机器"；Remote Control 的归属规则原样适用 | 配额、成本与管理员治理没有 Space 可挂靠；Portal 中出现第二套账号级资源模型 |
| **归属 Space 且按 Space 交互** | 与其他 Portal 资源一致 | Space 成员身份成为进入实时 Session 的途径，而这正是 Remote Control 刻意拒绝的；实时 Session 注册表必须泛化为 Space 授权 |

**建议：把资源归属与交互分开。** Environment 记录属于一个 Space，由 Space 承载其
配额、成本、生命周期治理与审计。交互属于一个人——操作者：机器的实时 Session 注册为
此人的 Remote Control Session，作为其机器之一出现在其 Session 列表中。Remote
Control 的规则完全不变：实时 Session 只能由其所属账号访问。Remote Control
Session 只新增一个指向其 Environment 的可选引用。

由此得出的规则：

- 操作者在仍是 Space 成员期间可以接入。失去成员身份或被停用会停止机器；默认立即
  停止，文件保留到 owner 删除该 Environment。
- Space owner 与 admin 可以查看、停止和删除 Space 内的任何 Environment，并调整
  配额，但不获得对话记录或 Session 访问权。
- 共享交互访问如有用户旅程需要，将作为未来的显式授权，绝不由成员身份隐含。

## 8. 机器生命周期与持久化

第一条生命周期规则是：长时间运行的 Environment **并不等于**长时间运行的 Pod。
Environment 记录及其持久存储比计算资源活得更久；Pod 是可替换的附件，只在机器就绪
或正在转换状态时存在。为每个已停止 Environment 保留一个小型 sleeper 或 supervisor
Pod，会保留这套生命周期原本要消除的成本。

Environment 具有异步的期望状态与观测状态。具体存储表示留给后续设计；用户可见的
生命周期至少需要区分：

```text
create -> provisioning -> ready <-> stopping -> stopped
                         |                    |
                         +------ failed <-----+

stopped / failed -> deleting -> gone
```

- **Create** 先供应存储并初始化工作区，再报告就绪。请求成功并不表示机器已就绪。
- **Ready** 表示机器已运行，所需的沙箱、存储与 Server 通道通过了启动检查，并且其
  Remote Control Session 已注册。浏览器是否在线无关紧要。
- **Stop** 在宽限期后终止计算资源，确认已无 Pod 存在，并保留卷。它不承诺挂起进程。
- **Start** 重新挂载同一个卷，并在接受提示前恢复 Agent Session。恢复失败会显式
  呈现并 fail-closed；绝不会在旧身份下悄悄启动一个新 Session。
- **Delete** 先让机器不可达，再按显式的保留策略销毁其存储；在 Portal 中需要破坏性
  操作确认。

### 8.1 运行与存储层级

同一个 Environment 可以在不同资源层级之间移动，而不改变身份：

| 层级 | 计算资源 | 持久状态 | 适用场景 |
|---|---|---|---|
| **Ready** | 一个 Pod | 已挂载持久卷 | 交互式 turn 与获准的后台工作 |
| **Stopped** | 零个 Pod | 保留的持久卷 | 普通闲置后的快速恢复 |
| **Archived**（后续仅在实测存储成本证明合理时加入） | 零个 Pod | CSI snapshot，或对象存储中加密的 workspace 与 runtime-home archive；在线卷已释放 | 很少使用、可接受较慢恢复的 Environment |
| **Gone** | 零个 Pod | 策略期限后不保留任何内容 | 显式删除 |

Archive 是同一 Environment 上的存储策略，不是第二种 workspace 实体。恢复时重新进入
`provisioning`，创建或恢复卷，并由 Agent Session 恢复决定是否就绪。第一切片只需要
Ready 和 Stopped；在加入 Archive 前，必须先测量保留卷的实际成本。

这遵循 Kubernetes 自身的分离：Pod 是临时资源，持久卷则可以重新挂载到替换计算资源。
Kubernetes 也支持 StatefulSet 缩容时保留 claim，CSI driver 可以提供标准
VolumeSnapshot。这些是底层能力，并不要求把每个 Environment 建模成 StatefulSet。
参见 Kubernetes 的 [Pod 生命周期](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/)、
[StatefulSet claim 保留](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#persistentvolumeclaim-retention)
与 [VolumeSnapshot](https://kubernetes.io/docs/concepts/storage/volume-snapshots/)。

### 8.2 闲置、保持唤醒与唤醒

长时间运行不等于永不终止。有效的唤醒截止时间取以下三个有界信号中的最晚者：

```text
max(
  最近一次操作者行为 + idle grace,
  当前 Agent turn 截止时间,
  具名后台工作的已批准 keep-awake 截止时间
) <= 部署硬性上限
```

接入、发送提示、回答问题或显式续租都算操作者行为。Agent 启动长分析、下载、构建或
服务时，可以带可见理由请求一个有界 keep-awake 截止时间；策略或操作者授予它，部署
上限仍然优先。Agent 不能让自己永久续租。浏览器在线、机器心跳、CPU 使用、网络使用，
以及未分类的子进程都不能续租：这些信号要么很容易被遗留，要么无法区分用户需要的工作
与泄漏。

所有信号都消失后，controller 请求 Stop，给 runtime 一个有界 quiescence 窗口，然后
删除计算资源。通用机器也容易引出无人续租的无人值守服务——监控或机器人；那属于托管
而不是交互式工作，不在第一版范围内（§15）。

发送给 Stopped Environment 的提示是一条持久化唤醒请求，而不是发给一个不存在 Pod
的请求。Server 先提交带幂等 key 的提示并请求 `ready`；controller 创建计算资源；
runtime 恢复并重新注册同一个关联 Environment 的 Remote Control Session；此后
Server 才把提示恰好投递一次。常驻 Server 是 activation buffer，因此不需要逐
Environment sleeper Pod。

| 事件 | 工作区与 Agent Session | 进程 |
|---|---|---|
| 浏览器断开或 Server 副本切换 | 保留 | 继续 |
| 同一机器上运行时进程重启 | 保留 | 可能丢失 |
| 停止后再启动，或重新挂载卷替换工作负载 | 保留 | 丢失 |
| 卷丢失 | 不可用，除非后续有备份策略 | 丢失 |
| 超过保留期后删除 | 销毁 | 丢失 |

### 8.3 技术选择与冷启动预算

第一版实现应使用满足契约的最少机制：

| 技术 | 作用 | 本提案中的定位 |
|---|---|---|
| Environment reconciler 管理一个 Pod 与一个独立管理的持久卷 | 显式的零或一个计算实例、持久唤醒请求、生命周期与故障所有权 | **第一切片** |
| HPA scale-to-zero 或 KEDA | 由外部指标或事件源唤醒多副本 workload | 仅在出现非用户唤醒来源后可选；Portal 或 Remote Control 显式激活不需要它们 |
| Vertical Pod Autoscaler 推荐 | 用 CPU、内存、峰值与 OOM 历史数据，为下次启动选择 small/medium/large 规格 | 在原型中测量；不要让它产生任意的逐用户规格 |
| 节点自动扩缩与 consolidation | Environment Pod 归零后释放节点，并高效装箱活跃机器 | 运维关注点；规模化时集群级节省所必需 |
| lazy image pulling 或节点 image prefetch | 降低冷启动时间 | 仅在 image pull 测量表明它重要后采用 |
| CRIU/container 或 microVM 内存快照 | 恢复进程，而不只是文件与 Agent 历史 | 研究方向，不是第一切片的可移植性或持久性承诺 |

Kubernetes 1.37 可以让 HPA 从 object 或 external metric 缩到零，不能依赖 CPU 或
内存指标；KEDA 可在更旧或更丰富的环境中提供类似的事件驱动 0 到 1 激活与 cooldown。
BuildMax 已经拥有更强的信号——经过认证的用户命令，因此在出现另一种唤醒来源之前
加入任一组件，都会与 Environment reconciler 重复。参见
[Kubernetes HPA scale-to-zero](https://kubernetes.io/docs/concepts/workloads/autoscaling/horizontal-pod-autoscale/#scaling-to-zero)
与 [KEDA scaling](https://keda.sh/docs/latest/concepts/scaling-deployments/)。

right-sizing 与节点回收是两个独立层次。Kubernetes VPA recommender 分析历史资源使用
与 OOM 事件；运维人员可以用这些建议调整 small/medium/large 规格。节点 autoscaler
或云厂商特定的 consolidator 随后可以在 Pod 消失后移除空节点或低利用率节点。参见
[Vertical Pod Autoscaling](https://kubernetes.io/docs/concepts/workloads/autoscaling/vertical-pod-autoscale/)，
以及作为云厂商特定示例的
[Karpenter consolidation](https://karpenter.sh/docs/concepts/disruption/)。

冷启动优化应按实测瓶颈依次进行：保持 image 精简并把工具放进不可变 layer；在活跃
节点预拉取通用 image；保留足够的共享节点容量以满足已接受的 wake SLO；然后再评估
[containerd stargz snapshotter](https://github.com/containerd/stargz-snapshotter)
一类 lazy pulling。若证据确实要求 warm pool，它只能包含空白 sandbox 或节点容量，
不能包含用户 volume、credential 或 Session；并且使用部署级总上限，而不是为每个
Environment 保留一个 warm Pod。

Container checkpoint 不是默认 suspend 机制。Kubernetes checkpoint API 已是 Beta，
但会暴露内存页（其中可能包含 secret），普通且可移植的 Pod restore 仍依赖 runtime。
Firecracker 可以恢复 microVM snapshot，但 snapshot 文件、磁盘、网络重连、CPU 兼容性
与生命周期都会变成 BuildMax 的职责。只有在“丢失在线进程”被证明是主要问题后，二者
才作为后续实验。参见
[Kubelet Checkpoint API](https://kubernetes.io/docs/reference/node/kubelet-checkpoint-api/)
与 [Firecracker snapshot support](https://github.com/firecracker-microvm/firecracker/blob/main/docs/snapshotting/snapshot-support.md)。

## 9. 通过 Remote Control 交互

### 9.1 复用的部分

机器通过现有的 Agent WebSocket 向 Server 外连，使用其类型化信封、有界且脱敏的
事件、心跳、带缓冲的流、跨副本命令路由，以及入站的提示、审批、提问与取消。Portal
用现有的 Remote Control 视图渲染它。外连意味着没有任何 Environment 是可被直接访问
的网络服务器。

### 9.2 与笔记本的不同

- **凭证。** 笔记本用其用户的登录凭证认证。机器获得一个短期、可撤销、绑定到一个
  Environment 及其操作者的凭证，由控制面签发并续期。它从不是操作者的 refresh
  token 或 worker token，也无法访问任何其他账号资源。
- **开启方式。** 机器始终承载 Remote Control，这是它唯一的用途。保护笔记本的逐
  Session 开启被"创建 Environment"这一显式动作取代。
- **就绪与在线。** 机器可以在其运行时重启期间保持健康。Session 离线本身从不授权
  回收；租约才授权回收。

### 9.3 两类宿主共享的 Remote Control 改进

有两个缺口对云端机器更重要，但并非其特有，因此属于 Remote Control 本身，同样惠及
笔记本：

- **重新接入时的历史。** Remote Control 只回放一个短缓冲。数天后回来的用户需要
  完整对话，而运行时已在其持久化 Session 中保存了它。最小的修复是由运行时在观察者
  接入时发送一个有界的历史快照；Server 仍只做中继，不存储对话记录。
- **只读审阅。** 没有编辑器和终端，用户仍需判断 Agent 的工作。对话必须以通用
  形式承载只读结果：通过 Artifact 交付的文件与报告、截图，以及当工作是代码时的
  工作区 diff。Remote Control 设计已把工作区 diff 列为窄界面的一部分；原型必须
  确认 Portal 当前实际渲染了什么，并补齐缺口。

浏览器能力是核心而非附带：大量通用工作发生在 Web 上，而查看 Agent 启动的 Web 应用
也是同一件事。它取代了端口转发。该能力目前仅限本地；Environment 镜像必须在
Environment 的沙箱、导航与网络限制下携带它，这是一个验证项。

## 10. 信任边界

Environment 执行模型选择的命令的时间远长于 worker Job。时间增加了暴露面，并不能
成为放宽边界的理由。

- **凭证：** 机器凭证（§9.2）是它唯一的 Server 凭证。机器不获得数据库、对象存储、
  模型供应商、refresh token 或集群凭证。托管推理仍由 Server 中介，计入 Space 与
  操作者。
- **文件系统：** 工具只能看到 `workspace/`。持久的运行时 home 与长期存在的工具
  进程同处一机，是相对 worker 的新风险；trust harness 必须证明工具在长 Session
  中无法读取它，而不仅仅是证明该路径被排除。
- **沙箱与外层运行时：** 命令约束启用并 fail-closed。工作负载具备合格的 Pod 或
  容器边界、资源限制、只读镜像根与网络策略。模型不能选择或削弱它们。
- **出网：** 通用工作需要比编程 worker 访问包仓库更广的互联网访问，而一台具备
  广泛出网能力的长期机器是数据外泄与滥用的攻击面。出网默认值及其按 Space 或按
  Environment 的策略由运维人员显式决定，绝不由模型决定。
- **外部凭证：** 发送邮件或调用第三方 API 的工作只能通过 Server 中介的授权获得
  凭证——Space Secret 授权或应用代理——绝不为了让机器保持就绪而把凭证落进工作区。
- **Plugin、hooks 与 MCP：** 只有 Space 显式、由 Server 解析的 Plugin 激活会进入
  机器，在 Environment 启动或 Session 启动时解析；不热加载任何东西。stdio MCP
  在有约束方案之前保持 fail-closed 禁用。
- **控制面权限：** 管理长期工作负载与持久卷会把 Server 的集群权限扩展到创建 Job
  之外。这项扩展属于安全评审的一部分，并限定在专用命名空间内。
- **审计与 trace：** 生命周期操作与远程命令记录操作者与 Environment。运行时在其
  home 中保留有界、脱敏的 trace。中继失败对 Agent 是 fail-open；凭证或沙箱失败对
  就绪是 fail-closed。

## 11. 供应、协调与失败

供应是持久化的协调过程，而不是一个长 HTTP 请求。Server 记录期望状态；控制器收敛
计算与存储；机器报告健康。Server 重启不能终止健康的机器，丢失的回调也不能让机器
永远卡在 `provisioning` 或 `stopping`。

首个受支持部署由 Environment reconciler 为每个 Environment 管理一个隔离 Pod 和一个
独立管理的持久卷。期望状态 `ready` 表示 Pod 存在；期望状态 `stopped` 表示没有 Pod、
卷仍保留。reconciler 本身就是 workload controller，因此第一切片不需要为每个
Environment 创建 Deployment、StatefulSet、HPA、KEDA 对象或常驻 sidecar，除非后续
证据表明其中某项能提供独立且必要的行为。它不复用 worker Job 及其 run token。本地
进程原型可以验证交互，但不构成多租户边界的证据。

| 失败 | 要求的结果 |
|---|---|
| 机器就绪前供应失败 | 进入带诊断信息的 `failed`；重试不产生重复的存储或工作负载 |
| 工作负载存在但心跳中断 | 标记为不可用；控制器检查或重启工作负载，不触碰卷 |
| 工作负载或节点消失 | 替换实例挂载同一个卷；进程声明为丢失；Session 恢复决定是否就绪 |
| Server 重启或副本切换 | 机器继续运行；Remote Control 重连；期望状态驱动协调 |
| Stop 与 Start 或 Delete 竞争 | 一个串行化的期望状态胜出；过期回调不能复活计算 |
| 达到闲置超时或硬性上限 | 在没有浏览器连接时也会请求并强制停止 |
| 提示到达已停止的 Environment | 唤醒前先提交提示与幂等 key；恢复后的 Session 在就绪后恰好接收一次 |
| 卷无法挂载或 Session 无法恢复 | 不就绪；错误指明失败的边界 |
| Archive 或 archive restore 失败 | 不提前删除现有保留状态；若没有可用卷，Environment 保持可诊断且不就绪 |
| Delete 部分成功 | 协调持续进行，直到记录下终态结果 |

孤儿检测双向进行：没有工作负载的记录会被协调，没有有效记录的带标签工作负载或卷会
被隔离，并按文档化策略回收。恢复从不重放 Agent 工作；Environment 是交互状态，
不是幂等任务。

## 12. API、Portal 与运维界面

后续设计可以改名，但该能力需要等价于以下的 Space 级生命周期操作：

```text
POST   /api/spaces/{space_id}/environments
GET    /api/spaces/{space_id}/environments
GET    /api/spaces/{space_id}/environments/{environment_id}
POST   /api/spaces/{space_id}/environments/{environment_id}/start
POST   /api/spaces/{space_id}/environments/{environment_id}/stop
POST   /api/spaces/{space_id}/environments/{environment_id}/lease
DELETE /api/spaces/{space_id}/environments/{environment_id}
```

生命周期命令是幂等的并返回资源；客户端观察就绪状态。交互不新增路由：它就是操作者
现有的 Remote Control Session。

Portal 需要三个界面：

1. Environment 列表，显示操作者、状态、计算与存储层级、租约到期时间、资源规格、
   最近一次机器信号，以及醒目的运行成本与保留存储成本指示；
2. Environment 详情页，提供 Start、Stop、Renew、Delete 与诊断，并为操作者打开
   现有的 Remote Control Session 视图；
3. 管理视图，显示活跃数量、资源总量、失败，以及强制停止或删除，不提供 Session
   访问。

运维配置需要启用开关、机器镜像与资源规格、最大活跃 Environment 数、每个 Space 的
限制、闲置与最长租约边界、保留卷与可选 archive 的阈值、存储类别与容量、archive
backend、启动超时与 wake SLO，以及所需的运行时与沙箱策略。默认关闭；不会悄悄
分配计算资源。

## 13. 方案与权衡

| 方案 | 有用的特性 | 代价或失败 |
|---|---|---|
| **A. 经 Remote Control 接入的托管云端机器（建议用于验证）** | 机器管理沿用云 IDE 实践；交互、Agent 运行时与 Portal 视图均被复用；Task 语义不受影响 | 增加供应、持久存储、协调、配额与长期信任边界 |
| **B. 让一个 TaskRun 常驻** | 表面上 schema 改动最小 | 没有权威结果；租约与交互成为 worker 特例；worker 丢失时语义含混 |
| **C. 在临时 worker 上使用 Task Continue** | 复用持久的 Task 模型，闲置时零成本 | 保留文件与历史，但从不保留进程或热状态；每次只有一个有界 turn |
| **D. 带编辑器与终端的经典云 IDE** | 熟悉；能覆盖 Agent 做不到的任何事 | 重建了 Agent 所取代的东西；为直接 shell 访问成倍扩大界面与信任边界 |
| **E. 集成外部 codespace 供应商** | 外包供应工作 | 在窄界面被证明之前就引入供应商凭证、成本、可用性与可移植性约束 |
| **F. 在 Server 或共享 worker 内运行长期 Session** | 不需要每个 Environment 一个工作负载 | 把不可信执行与控制面或其他租户混在一起 |
| **G. 为每个已停止 Environment 保留小型 sleeper Pod** | 避免唤醒时重建进程 | 仍然预留内存并增加逐 Environment 控制面负载；保留了 scale-to-zero 原本要消除的浪费 |

方案 C 已经服务于只需要跨 turn 连续性的工作。只有当保留进程、热状态或即时重新进入
会实质性改变结果时，新实体才有必要。这是核心的证据检验。

## 14. 最小验证切片

有两部分可以在任何机器管理存在之前构建和评估，因为它们同样服务于笔记本：

1. `buildmax` 二进制的无界面 Remote Control 承载模式；
2. 重新接入时的历史快照，以及 Session 视图中的只读审阅产出（§9.3）。

随后的 Environment 原型省略云 IDE 为直接操作所增加的一切。它运行两个用户旅程：
一个非编程旅程——Agent 用数小时采集并分析 Web 数据，最后交付一份报告；一个编程
旅程——Agent 构建一个项目，并用浏览器验证运行中的服务。在一个隔离的 Kubernetes
测试部署中，它应当：

1. 创建一个 Environment，通过持久化协调达到 `ready`，并让其 Session 出现在操作者
   的 Remote Control 列表中；
2. 向 Agent 发送提示、观察输出、回答一次审批或提问，并取消一个 turn；
3. 让 Agent 运行每个旅程中的长进程，在 Environment 的网络策略下使用浏览器能力；
4. 断开 Portal，让该进程继续运行，在中继缓冲过期后重新连接，并恢复对话、工作区与
   已产出的 Artifact；
5. 让闲置租约到期，证明 Environment 达到零 Pod，而卷、文件与历史仍然保留；向已停止
   的 Environment 发送提示，唤醒机器，恰好投递一次，并报告旧进程已丢失；
6. 分别重启 Server 和杀掉工作负载，证明文档化的协调结果；
7. 证明非操作者与跨 Space 访问被拒绝、凭证撤销、配额拒绝、闲置到期、失去成员身份
   后停止，以及沙箱启动 fail-closed；
8. 删除 Environment，验证计算与存储被回收，且没有遗留可访问的 Session。

原型记录冷启动与热启动的就绪耗时、重连耗时、活跃 Pod 小时、Environment 生命周期
中处于零 Pod 的比例、保留卷成本、存储增长、image pull 耗时、请求的规格与观测到的
CPU 和内存、恢复失败，以及每一次用户想要编辑器或终端的时刻。它把这些数字与常驻
Pod 对比，并用 Task Continue 运行这两个旅程。如果保留进程与热状态没有改变结果，
Task 平面就是更简单的答案。

由此不产生任何用户文档、兼容性承诺或可用性声明。该切片用于决定机器管理是否值得
承担产品与运维所有权。

## 15. 开放问题与决策证据

由 §2 与 §7 的定位决定、有待评审确认：

- 不提供直接操作用的编辑器、终端或文件浏览器；审阅是只读的。
- 不允许 Task 以 Environment 为执行目标。
- Environment 归属 Space；交互经由 Remote Control 归属操作者的账号。
- 失去成员身份立即停止机器；文件保留。
- 工作区默认为空，可选用 Git、Space 文件或上传初始化，不自动写回。

仍然开放：

- 什么样的闲置超时与硬性上限符合实际工作？停止前需要提前多久提醒用户？哪些长操作
  值得获准一个 keep-awake 截止时间？
- 已停止的 Environment 应保留在线卷多久再进入可选 archive？实测存储价格是否值得
  构建这一层级？
- 怎样的唤醒耗时 SLO 可以接受？在接受 warm pool 或 lazy pull 依赖前，image 大小、
  预拉取与共享节点容量能否达到它？
- 在出现第二种唤醒来源前，是否应继续由 reconciler 驱动唤醒（建议）？还是观察到的
  webhook、schedule 或 queue 旅程足以证明 KEDA 或 HPA external metric 的必要性？
- 有界历史快照是否足以支撑数天后的重新连接，还是这一需求指向
  [持久 Agent Session 提案](durable-agent-sessions.md)？
- 什么样的只读审阅集合——Artifact、截图、diff——足够？是否仍有观察到的用户旅程
  要求用户直接动手？
- 什么样的出网默认值既适合通用工作，又不会让长期机器变成开放代理？谁来设定按 Space
  的例外？
- 观察到的用户旅程需要哪些外部凭证？Space Secret 授权是否足够，还是需要应用代理？
- 是否有证据表明需要在 Environment 上运行无人值守服务？如果有，它与现有的 Agent
  Schedule（已在 Task 平面上执行周期性工作）如何分工，又用什么租约模型取代操作者
  续租？
- 浏览器能力能否在 Environment 的沙箱与网络策略下运行，且不削弱二者？
- 哪些 Plugin 或工作区变更需要重启 Session，哪些需要重启机器？
- 首个部署能如实承诺怎样的存储持久性、备份与保留？
- 是否有任何旅程足够重视在线进程恢复，值得承担 container 或 microVM checkpoint 的
  secret 处理、兼容性与运维成本，而不是采用普通的零 Pod 重启？
- 该边界是否需要 gVisor 或其他外层运行时？带嵌套命令沙箱的机器镜像能否无例外地
  通过 trust harness？

决定推进至少需要：一个 Task Continue 无法满足的具名用户旅程、来自受支持拓扑的
生命周期与隔离证据、一个实测的资源包络，以及回收与事故响应的明确负责人。仅有一个
持久 Pod 的演示是不够的。

## 16. 获采纳后的可能归宿

如获采纳，稳定的边界——Environment 作为托管云端机器、Space 归属与账号交互的拆分、
Remote Control 复用、生命周期、持久化与信任——将移入一份产品与执行模型设计记录。
无界面承载模式与重新接入历史的工作直接扩展
[Remote Control 设计](../design/远程控制.md)，因为笔记本同样会用到它们。
Kubernetes 供应与运维策略仅在细节足够多时才获得独立的运维与部署规范。

拆解后的工作在验证门槛与安全边界被接受后进入[待办](../../backlog/README.md)。客户端
界面提案继续负责共享 UI 与传输收敛，不负责机器生命周期。本提案在其理由迁移完成后
删除；如果证据更支持 Task Continue 或外部供应商，则直接删除而不留替代。
