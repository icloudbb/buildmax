# Issue 主题协调与 Agent 黑板

> **翻译说明：** 本文是[英文原文](../../proposals/issue-topic-coordination.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。

> **受众：** 产品设计者、贡献者与早期采用者 · **状态：** 提案 — 讨论中
>
> **开启时间：** 2026-09-13

相关文档：[路线图](../ROADMAP.md)、[当前状态](../current-state.md)、
[Agent 原生协作底座](agent-native-collaboration-substrate.md)、
[Session 树与 Agent 邮箱](session-tree-and-agent-mailbox.md)、
[Issue Agent 访问](../design/Issue Agent访问.md)、
[Agent 桥接 CLI](../design/Agent 桥接 CLI.md)、
[Agent 执行与 Task thread](../design/Agent执行与Task线程.md)、
[Workflow 运行时](../design/Workflow运行时.md)，以及
[Portal 工作与执行体验](../design/Portal工作与执行体验.md)。

## 目录

- [1. 摘要](#1-摘要)
- [2. 用户结果、证据与约束](#2-用户结果证据与约束)
- [3. 术语与心智模型](#3-术语与心智模型)
- [4. 用户场景](#4-用户场景)
- [5. 目标](#5-目标)
- [6. 非目标](#6-非目标)
- [7. 区分信息、投递与同步](#7-区分信息投递与同步)
- [8. 主题范围与参与者](#8-主题范围与参与者)
- [9. 黑板内容与权威](#9-黑板内容与权威)
- [10. 读取、投递与 Agent 上下文](#10-读取投递与-agent-上下文)
- [11. 点对点信号与广播](#11-点对点信号与广播)
- [12. 同步与工作所有权](#12-同步与工作所有权)
- [13. 安全、治理与成本](#13-安全治理与成本)
- [14. 一致性与失败语义](#14-一致性与失败语义)
- [15. Workspace 与变更集成](#15-workspace-与变更集成)
- [16. 界面体验](#16-界面体验)
- [17. 方案与权衡](#17-方案与权衡)
- [18. 分阶段验证](#18-分阶段验证)
- [19. 原型验收标准](#19-原型验收标准)
- [20. 待解决问题](#20-待解决问题)
- [21. 采纳前所需的证据](#21-采纳前所需的证据)
- [22. 架构落点](#22-架构落点)
- [23. 若被采纳后的去向](#23-若被采纳后的去向)
- [24. 候选方向](#24-候选方向)

## 1. 摘要

多个 Agent 为同一个结果工作时，有时仅靠相互独立的 Issue 分派并不够。某个
参与者发现了约束、改变了假设、产出了证据或遇到阻塞，其他参与者需要及时知道，
否则会重复劳动，或继续沿用已经过期的前提。

本提案评估一种主题范围内的协调能力，并刻意把它分成三个部分：

1. **Issue 主题黑板**是一个持久、受限、带来源的共享信息流，供同一个父 Issue
   下的参与者使用。
2. **点对点 Signal**要求特定参与者采取行动，因此具有投递、确认和可选唤醒语义。
3. **汇合条件或 Workflow 条件**决定依赖工作何时可以继续。它是持久协调状态，
   不是隐藏在自由文本中的约定。

第一个验证切片不应创建新的 `Blackboard` 实体。父 Issue 已经表达共同结果，子
Issue 已经表达分工，而 Issue 评论已经是人与 Agent 都可读的持久线程。最小且
有用的实验，是让子 Issue 的 Agent 读取父 Issue Discussion 的受限视图，并向
其中报告。

只有反复使用暴露出评论无法解决的具体问题，才应考虑独立的 coordination entry
存储、单调游标、订阅或类型化投影。即便如此，黑板也应是主题信息流，而不是共享
可变字典或任意 Agent 聊天网络。

本文记录候选方向，不描述已交付行为，也不构成路线图承诺。

## 2. 用户结果、证据与约束

### 2.1 核心用户结果

核心结果是：

> 为同一目标做贡献的人和 Agent 能足够早地发现共享理解发生了哪些相关变化，
> 从而避免重复工作、互相矛盾的假设和不协调的交付；同时，系统仍能让行动请求
> 与同步保持可靠、可问责。

目标不是“Agent 可以发消息”。消息只是机制。用户看到的结果应是来源清楚的并行
协作，而不是一个无人值守、可以自行循环的对话网络。

### 2.2 当前产品中的证据

BuildMax 已有一些基础，说明以 Issue 为中心的协调界面可能成立：

- Issue 是主要共享工作对象，并支持一层子 Issue。
- Owner 与 Executor 已分开，因此人工责任与 Agent 或 Workflow 执行可以共存。
- Issue Discussion 持久保存用户、Server 观察到的 Agent run、本地 Agent 声明
  和系统评论；可用时还保留来源 Task 与 TaskRun。
- 正在处理 Issue 的 Agent 通过 Bash 运行 `buildmax issue show` 和
  `buildmax issue comment`。在 worker run 中，run token 与经由 run bridge 访问的
  worker route 把两者限定在该 run 自己的 Issue 上，并拒绝 Issue ID 参数；Agent
  读取受限的近期评论并写入受限报告，而不需要选择 Issue ID。在本地，这些命令以
  该人自己的凭据运行，并接受一个 ID。
- Task 与 TaskRun 已经拥有持久执行和结果；Artifact 已经拥有不可变证据。
- Workflow 已被确定为依赖、就绪、等待、重试和整体完成的权威所有者。

这些基础证明参与者能够留下并读取 Issue 范围的报告，但尚未证明跨 Issue 的主题
信息流能够改进真实工作。

### 2.3 当前缺口

Agent 只被限定到其 Task 所关联的 Issue。当一个父 Issue 被拆成多个子 Issue 并
交给不同 Agent 时，每个 Agent 只能看到自己的 Discussion，看不到与兄弟工作共享
的持久主题视图。各自的终态摘要可能直到工作结束才进入不同子线程，错过最有价值
的传播时机。

Issue 评论也不具备“协调”一词有时暗含的更强语义：

- 没有明确收件人或处理确认；
- 不保证正在运行的 Agent 会看到新评论；
- 没有每个参与者的游标或未读状态；
- 没有明确的屏障、汇合、截止时间或失败策略；
- 除了自由文本，没有区分观察、提议、阻塞和权威决定；
- 不提供安全共享 workspace 或变更集成语义。

### 2.4 当前约束

- Space 仍是 Portal 资源的所有权与授权边界。
- Issue 拥有共享工作和结果上下文，不拥有执行或 Workflow 状态。
- Task 加 TaskRun 仍是唯一持久 Agent 执行平面。
- Issue 状态、Owner、Executor 选择与层级仍是由人或 service 授权的状态；Agent
  报告不得隐式修改它们。
- Agent 生成的内容是证据，不具有 system 或 user 权威，并可能包含其读取材料中的
  prompt injection。
- 运行中的 Agent 不能在 assistant/tool batch 中途被安全打断。
- 并行写入者需要隔离的 workspace 和可审查的变更集成；通信不能让同一个目录安全
  地支持多写入者。
- 当前路线图优先保障运营可靠性。本提案必须以证据赢得优先级，而不是先引入一个
  宽泛平台抽象。

## 3. 术语与心智模型

### 3.1 Topic

Topic 是多个工作项共同协调的目标。在候选第一切片中，Topic 就是现有顶层 Issue。
子 Issue 通过 `parent_issue_id` 推导 Topic；顶层 Issue 自身就是 Topic。

Topic 最初是一种关系和用户心智模型，不是新的数据库实体。

### 3.2 Participant

Participant 是有权在 Topic 中读取或贡献的人、Task、本地关联 Session，或未来受
监督的 Session。Agent definition 本身不是参与者：同一份 Agent 配置可以同时执行
许多互不相关的 Task。

### 3.3 Blackboard

Blackboard 是 Topic 的共享、以追加为主的信息流。它保存受限的陈述与引用，而不
保存完整 transcript、文件内容或可变全局变量。

“Blackboard”适合作为产品隐喻；如果未来确实需要新的持久概念，架构层宜称其为
coordination feed 或 entry，避免让人误以为任意值都能被原地擦除和改写。

### 3.4 Signal

Signal 是发送给一个收件人或 supervisor 的持久、带来源、有地址事件。它与
Blackboard entry 不同：它有投递与处理生命周期，并可在明确策略下使暂停的参与者
重新变为可运行状态。

### 3.5 Join condition

Join condition 是确定性状态，用来说明依赖执行何时可以继续，例如所有子项进入终态、
任一成功、达到截止时间或等待人工决定。Workflow 或 Session supervisor 拥有它；
Blackboard 可以展示它，但不实现它。

### 3.6 Topic snapshot

Topic snapshot 是在确定时间点提供给 Agent 的受限视图，包括父 Issue 目标、子项
状态摘要、近期相关 entry、遗漏数量和来源信息。它不是实时共享内存。

## 4. 用户场景

### 4.1 并行调查发现共享约束

一个父 Issue 被拆成 API 设计、持久化和 Portal 体验三个子项。持久化 Agent 发现，
如果没有单调序列，提议的排序无法安全实现。它把 finding 发布到父 Topic。其他
参与者在下一次协调边界看到它，并调整方案。

发布 finding 本身不会自动重启任何 Agent。

### 4.2 一个参与者需要明确回答

Portal Agent 需要 API Agent 决定未读数量采用精确值还是近似值。把 question 发到
Blackboard 能让问题可见，但不能证明 API Agent 会采取行动。点对点 Signal 引用该
Blackboard entry，并请求该参与者回答。

如果回答与整个 Topic 有关，它可以再成为一条 Blackboard entry。

### 4.3 Fan-in 等待多个结果

一个协调 Workflow 派发三个子 Task，并且只有三者全部进入终态后才综合。每个 Task
在运行过程中都可以发布有用发现，但 Workflow reconciliation 根据 TaskRun 状态判断
是否就绪。诸如“我完成了”的文字永远不能满足 join。

### 4.4 新决定取代旧假设

一个人接受了某种 API 形状。Topic 将被接受的决定显示为当前事实，并把旧提议显示为
已被取代。新 Agent snapshot 包含当前决定并保留旧记录链接；历史 TaskRun 仍保留其
实际使用的旧基础。

### 4.5 子项产生 workspace 变更

子项报告结论，并引用 change set 或 Artifact。其他参与者可以查看证据，但该报告不会
把文件合并进他们的 workspace。获得授权的集成路径仍与信息报告分开。

## 5. 目标

- 为一个 Issue 目标下的参与者提供受限共享信息面。
- 减少并行子 Issue 之间的重复工作和冲突假设。
- 为每一条 Agent 贡献保留作者、执行来源、时间与证据引用。
- 将广泛发布信息与要求特定对象行动分开。
- 将信息交换与持久同步、就绪决定分开。
- 只在可复现、安全的上下文边界向 Agent 提供更新。
- 复用 Issue、Task/TaskRun、Artifact、Workflow 和 mailbox 的现有职责，不引入
  重叠的执行模型。
- 限制上下文、通知、写入、自动执行与存储成本。
- 在创建新 Server 实体之前，用最小有用扩展验证需求。

## 6. 非目标

- Agent 之间的任意实时群聊。
- 由多个运行中 model loop 共享的可变 key-value store。
- 通过模型选择的目标 ID，直接向兄弟参与者发送命令。
- 把每条 Blackboard entry 当作对所有参与者的保证投递。
- 每次更新都唤醒所有参与者。
- 在评论中编码 Workflow 屏障、重试或完成状态。
- 让 Agent 改变 Issue 状态、Owner、Executor 或层级。
- 把无界 Topic 历史重放到每次模型调用中。
- 把完整 Task 输出、transcript、diff 或文件复制进 feed。
- 让 Blackboard 成为 Artifact、Wiki、Project Memory、审计日志或 Agent Session
  历史。
- 让同一 workspace 的并发写入变得安全。
- 分布式 exactly-once 执行或投递。

## 7. 区分信息、投递与同步

一个通用消息抽象在局部看起来更简单，却会在完整生命周期中增加隐含状态。发给特定
对象的问题需要确认，面向整个主题的 finding 不需要；join 必须在重启和并发下可靠，
评论却不应决定 join。如果把它们合在一起，每条 post 都不得不携带收件人、订阅、
唤醒策略、屏障状态和重试语义。

候选模型明确分开三种职责：

| 用户需要 | 权威概念 | 必须具备的语义 |
|---|---|---|
| 分享与整个 Topic 有关的信息 | Blackboard / coordination feed | 持久追加、来源、受限读取 |
| 请求特定参与者行动 | Mailbox Signal | 地址、持久投递、确认、可选且受限的唤醒 |
| 等待工作达到某个条件 | Workflow 或 Session supervisor | 就绪、截止时间、重试、失败、恢复 |

这些概念可以相互引用。Signal 可以指向促成它的 Blackboard entry，Topic 视图可以
显示 Workflow join。引用不等于所有权。

## 8. 主题范围与参与者

### 8.1 从 Issue 层级推导第一版 Topic

第一切片确定性地推导 Topic：

```text
top-level Issue        -> topic_issue_id = issue.id
child Issue            -> topic_issue_id = issue.parent_issue_id
```

现有层级只有一层子项，因此不存在有歧义的祖先遍历。如果将来层级加深，Issue service
必须继续成为 root 解析的唯一所有者。

### 8.2 构造时限定范围

worker run 中面向 Agent 的命令不得让模型传入 `topic_issue_id`、`issue_id`、
participant ID 或 Space ID。run token 指定一个 TaskRun 及其 Issue，worker route
解析该 Issue 与推导出的 Topic，因此范围存在于凭据和 route 中，而不是客户端参数中。

这延续了 `buildmax issue show` 与 `buildmax issue comment` 的安全模式——
[Agent 桥接 CLI](../design/Agent 桥接 CLI.md) 第 3 节把它从工具构造器移到了凭据
与 worker route 上——而不是给模型一个 Topic 浏览器。去掉任意目标参数，可防止恶意
评论把一次看似普通的命令调用变成跨 Issue 数据外泄路径。本地上下文按设计有所不同：
那里的命令以该人自己的凭据运行并接受 Issue ID，由该人对 Agent 报告的内容负责。

### 8.3 参与资格与生命周期

- Space member 按现有 Issue 授权读取 Topic。
- worker Task 仅在其 run credential 有效时参与，并且只能通过 Task 的 Issue 关系参与。
- 如果本地 Issue 关联提案被接受，本地 Session 通过已认证用户明确且持久的 Issue link
  参与。
- 移动或移除子 Issue 只影响未来 capability，不改写已经接受的 entry 来源。
- Agent definition 在 Task 或 Session 之外没有常驻订阅。

## 9. 黑板内容与权威

### 9.1 第一切片使用现有评论

第一个实验应把选定的父 Issue 评论与子项状态摘要投影到 Agent 的受限 Topic snapshot。
子 Agent 可以通过构造时限定范围的 capability 向父 Discussion 发布报告。

这会先验证用户结果，再决定是否承诺新 schema。当前 comment 字段已经保存正文、作者
类型、作者身份、来源 Task、来源 TaskRun、创建时间和编辑状态。

UI 可以把过滤后的视图标成“Coordination”或“Blackboard”，但不能声称存在一个独立
持久资源。

### 9.2 仅在证据要求时引入类型化 entry

自由文本评论最终可能无法支持过滤与摘要。如果确实如此，最小 append-only entry
可能需要：

| 字段 | 缺少时失败的要求 |
|---|---|
| `id` | 供 Signal、decision、trace 与 UI 稳定引用 |
| `topic_issue_id` | 唯一持久 Topic 范围与授权路径 |
| `sequence` | 在时间戳冲突下仍可稳定增量读取 |
| `kind` | 无需解释自由文本即可进行受限过滤和投影 |
| `author_kind`, `author_id` | 信任与问责 |
| `source_task_id`, `source_task_run_id` | Server 观察到的执行来源 |
| `body` | 人和模型可读的受限陈述 |
| `artifact_ids` | 不嵌入内容的稳定证据引用 |
| `supersedes_entry_id` | 通过追加修正过期信息 |
| `created_at` | 历史与新鲜度 |

在真实工作流未证明缺少某字段就会失败之前，不应加入它。尤其是 recipient、ack 状态、
join 状态和 workspace 内容都不属于广播 entry。

### 9.3 候选类型

一个小型封闭集合可以包括：

- `finding`：由证据支持的观察；
- `proposal`：建议采用的共享假设或行动方向；
- `blocker`：阻止继续推进的条件；
- `question`：尚未回答的 Topic 级问题；
- `notice`：不属于以上类型的简短协调更新。

`decision` 最初不应允许 Agent 写入。决定会改变参与者有权当作“已接受”的内容，因此
需要经授权的人、Workflow transition 或未来明确的 decision service。Agent 可以
发布一条 proposal，说明它建议做出某项决定。

### 9.4 追加而非改写

Agent entry 不可变。更正通过追加一条 supersede 旧 entry 的新记录完成。人的评论仍
可以保留 Discussion 编辑行为，但未来的权威协调投影不应静默改写已完成 TaskRun
所依据的内容。

## 10. 读取、投递与 Agent 上下文

### 10.1 Snapshot，而非实时共享内存

Agent 在 TaskRun 开始时，或显式调用读取工具时获得 Topic snapshot。snapshot 说明
采集时间和遗漏历史数量。之后的 post 不会改变当前 run 已经采用的上下文。

### 10.2 不在 batch 中途注入

Blackboard 更新不得打断 `assistant(tool_calls) -> tool results` 序列。如果以后证明
向运行中 run 投递有价值，supervisor 只能在完整 Agent-loop iteration 边界提供更新，
并把该边界写入 trace。

### 10.3 先 pull，后 push

第一切片使用受限 pull：

- 初始 Topic snapshot；
- 通过限定范围的读取工具显式刷新；
- 后续 TaskRun 启动时刷新。

这足以测试共享信息是否有帮助。在观察证明 pull 太迟之前，不建设持久订阅、每参与者
游标、未读数量、digest 和自动唤醒。

### 10.4 受限投影

面向模型的投影应依次优先呈现：

1. Topic 标题与目标；
2. 当前子项标题和状态；
3. 如果未来存在 accepted decision，则呈现当前决定；
4. 最近未解决的 blocker 和 question；
5. 其他 entry 的受限尾部和遗漏数量。

所有 Agent 内容都必须标为 report 或 evidence，而不是 user 或 system instruction。

## 11. 点对点信号与广播

Blackboard post 对有权参与 Topic 的对象可见，但不证明任何特定参与者已经读取或处理。
对于共享 finding 与状态上下文，这是正确语义。

如果发送者需要某个收件人行动，系统应使用 Signal。Signal 可以引用 Blackboard
entry，而不是复制正文。mailbox 提案拥有最终投递状态、幂等性、唤醒策略与 parent /
supervisor 限制。

第一版 Blackboard 不应增加任意 Signal 地址。安全顺序是：

1. 先验证直接 child-to-parent report 与 parent join；
2. 了解 Topic feed 仍未解决哪些真实协调场景；
3. 只增加这些场景所需的固定寻址路径；
4. 继续禁止模型在 tool 参数中选择任意 Task、Session、Agent 或 Issue ID。

广播 entry 最多为 Topic 产生一次低成本 UI 通知或 digest，默认不得为每个参与者创建
一次模型调用。否则一条 entry 会把 token 消耗、工具副作用和后续回复放大成无人值守
的反馈循环。

## 12. 同步与工作所有权

### 12.1 Blackboard 不是 barrier

“完成”“等待”或“批准”等陈述只是参与者证据，不是权威执行状态。Workflow 或
Session supervisor 根据持久 TaskRun、request、timeout 和 cancellation 事实评估
join。

### 12.2 使用 Issue 表达已声明分工

第一切片不应在 Blackboard 上引入自由形式的工作 claim。父子 Issue 已经表达人可见
的分工、Owner、Executor 与状态，应首先通过这个模型解决重复工作。

如果真实动态 swarm 后来证明工作产生速度太快，来不及使用 Issue assignment，可以
把带 lease 和 expiry 的受限 claim 作为独立协调概念评估。只有文字、没有原子所有权
的 `claim` entry，只会让重复劳动看上去像已经协调。

### 12.3 一个明确的综合权威

并行参与者可能给出互相冲突的发现。必须明确由 parent Agent、Workflow evaluator
或某个人负责综合。Blackboard 记录输入，不会自动产生共识。

## 13. 安全、治理与成本

### 13.1 投影必须保留信任标签

当前对 Server 观察到的 Agent report 和本地 Agent claim 的区分必须保持可见。Topic
聚合不得抹去部署是否观察到产生该陈述的 TaskRun。

### 13.2 Report 是不可信输入

Agent 可能从网页、仓库、文档或其他评论复制恶意指令。Blackboard 内容只能以带来源
的不可信证据进入接收模型，不能作为 system prompt、user request、tool grant 或
approval。

### 13.3 最小权限

- 工具在构造时限定到一个 Topic。
- 发布不能改变 Issue 或 Workflow 状态。
- Artifact 引用必须位于同一授权 Space，且不能暴露 object-store 或本地文件路径。
- 子项 grant 不会流向兄弟、父项或未来 run。
- Blackboard entry 不能提升收件人的执行策略。

### 13.4 限额

系统需要明确限制：

- 每个 TaskRun 的 entry 数量；
- entry 正文和 Artifact 引用数量；
- 返回给模型的 entry 数量；
- 每 Topic 每时间窗口的通知数量；
- 自动处理 run 与 token 消耗；
- 运营类 entry 的保留或压缩。

长内容继续留在 TaskRun output 或 Artifact 中。有用的 feed 应是一组提炼后的协调陈述，
而不是 run archive 的副本。

## 14. 一致性与失败语义

### 14.1 先持久化，再通知

entry 或 Signal 必须先持久化，再发送 WebSocket、Desktop event 或 scheduler wake-up。
通知只是投影；它可以重试或丢失，但不能导致底层陈述丢失。

### 14.2 幂等追加

Agent 发布需要由调用者生成、限定在 TaskRun 或 Session 内的 idempotency key。网络
重试返回已接受的 entry，不创建重复内容。

### 14.3 顺序

如果实验最终超越 comment，每个 Topic 需要由 store 分配的单调序列。并发写入时，
仅靠 `created_at` 无法形成安全游标。

sequence 表示观察顺序，不表示因果。依赖另一条 entry 的记录应明确引用它，或记录
自己基于哪个 snapshot sequence。

### 14.4 至少一次处理

不要求分布式 exactly-once delivery。持久 consumer 可能多次看到 notification，并
按 entry 或 Signal ID 去重。只有确实要求某个 consumer 处理的 addressed Signal，
才记录 processing state。

### 14.5 延迟与过期信息仍然可见

在 deadline、cancellation 或 accepted decision 之后到达的 report 仍属于历史，并
标记为迟到或基于旧 snapshot。它不会重启被取消的工作，也不会静默替换已接受状态。

## 15. Workspace 与变更集成

Blackboard 协调理解，不拥有文件系统。

- 并行可写 Task 使用隔离 workspace 或 worktree。
- Report 引用不可变 Artifact 或未来可审查的 change set。
- 接受 finding 与应用其 workspace 变更是两个动作。
- 应用一个子项的变更不会改写另一个子项的 snapshot。
- 冲突检测与集成属于 workspace capability，而不是 comment 或 coordination-entry store。

缺少 workspace 隔离的 Blackboard 可以改善并行调研和规划，但不能被描述为安全的并行
实现能力。

## 16. 界面体验

### 16.1 父 Issue

父 Issue 继续作为工作中心，现有区域含义不变：

- Overview 显示目标、所有权、Executor、状态与最新结果；
- Discussion 保留完整、可读的协作线程；
- Results 显示 TaskRun output 与 Artifact；
- Runs 显示执行历史与诊断。

候选 Coordination 视图过滤并汇总与 Topic 有关的 post、子项状态、未解决 blocker
和 accepted decision。它是投影，不是第五个事实来源。

### 16.2 子 Issue 与 Task

子界面显示：

- 哪个父 Topic 提供协调上下文；
- 当前可见 snapshot 最近何时刷新；
- 如果系统能如实计算，则显示遗漏或未读 entry 数量；
- 执行报告动作之前，明确它将发布到哪里。

### 16.3 Agent tool 反馈

发布成功后，tool output 应说明持久 entry 或 comment ID、Topic，以及它只是被发布，
还是还被 addressed Signal 引用。在系统没有可证明的通知契约时，绝不能声称“所有人
都已收到通知”。

## 17. 方案与权衡

### 17.1 方案 A：保持评论只属于各自 Issue

该方案不增加概念，并保留当前窄授权。它无法让兄弟工作在不经人工复制的情况下共享
发现，因此没有解决目标用户结果。

### 17.2 方案 B：创建通用 Blackboard 实体

独立于 Issue 的 Blackboard 可以服务任意 Session、Task、Project 和临时群组；但它
也要求在一个真实 Topic 工作流被验证前，就定义新的所有权、成员、生命周期、发现、
保留、授权和关系语义。

不建议把它作为第一方向。

### 17.3 方案 C：推导 Issue Topic feed，并保持 mailbox 与 join 独立

该方案复用现有工作中心、子项分解、评论来源、`buildmax issue` 命令面与 Space 授权，只增加子项
参与父级协调所缺少的能力。如果评论确实不够，后续可在相同 Topic 体验背后引入类型化
entry。

主要风险是让 Discussion 充满运营噪声，因此必须严格控制写入预算、使用过滤投影，并
从基于 comment 的切片收集证据。

这是候选推荐。

### 17.4 方案 D：建设任意 Agent 消息总线

通用 bus 看似统一了点对点、群组、广播、命令与问题，但它们的授权、确认、唤醒、成本
和失败语义并不相同。表面的灵活性会把复杂性推给每个发送者和接收者，也很容易形成
自治循环。

不建议采用。

### 17.5 方案 E：把所有协调编码进 Workflow

Workflow 可以可靠拥有声明的依赖和 fan-in，但不应把每个发现、假设变化、问题或证据
引用都建模为 node transition。对于信息共享部分，这个方案过于僵硬。

## 18. 分阶段验证

### Phase 0：观察当前替代流程

- 用一个父 Issue 和多个子 Issue 执行有界的多 Agent 旅程。
- 观察何时需要人工复制评论、重述约束，或直到综合阶段才发现重复劳动。
- 记录缺失的是广泛感知、定向行动、同步，还是 workspace 集成。

### Phase 1：通过现有评论提供父 Issue 协调

- 从 parent 推导子 Issue 的 Topic。
- 让子 Agent 通过构造时限定范围的 capability 读取受限父 Topic snapshot。
- 允许其向父 Discussion 发布少量报告。
- 使用现有 author 与 TaskRun provenance 展示报告。
- 不增加订阅、自动唤醒、类型化 entry 或新表。
- 不给模型 Issue 或 participant ID 参数。

### Phase 2：过滤投影与显式刷新

- 在父 comment 与子状态之上增加 Coordination 视图。
- 明确显示 snapshot 时间和遗漏历史。
- 允许人和 Agent 显式刷新。
- 衡量哪些 post 有用、被忽略、重复或到达太迟。

### Phase 3：仅在必要时引入 typed feed 与 cursor

- 只有自由文本 comment 无法支持有效过滤、增量读取或不可变修正时，才引入
  append-only coordination entry。
- 增加 per-Topic sequence、幂等 append 和 `supersedes_entry_id`。
- 让 Discussion 继续作为人可读投影，或明确决定哪些 entry 同时出现在两个视图中。

### Phase 4：Addressed Signal 与受限订阅

- 当一个收件人必须行动时，增加引用 entry 的 Signal。
- 从 mailbox 提案中受限的 parent/supervisor route 开始。
- 只有 pull 被证明太迟时，才增加 per-participant cursor 或 digest。
- 自动模型唤醒保持 opt-in、受预算和生命周期约束。

### Phase 5：Join 集成

- Workflow 或 Session supervisor 可以展示和引用 Topic finding，但仍独占 join 权威。
- 每个已满足 join 只恢复一次综合，而不是每条 Blackboard post 恢复一次。
- 验证 Server 重启和重复 notification 后的恢复。

## 19. 原型验收标准

Phase 1 或 Phase 2 原型只有满足以下条件才算成功：

1. 子 Issue 的 Task 无需提供 Issue ID，即可读取父目标、兄弟状态摘要和受限的近期
   Topic discussion。
2. 子项可以向父 Topic 发布受限报告，并带有正确的 Space、Agent、Task 与 TaskRun
   provenance。
3. 即使恶意 Topic 文本要求这样做，capability 也不能读写无关 parent、child、Issue
   或 Space。
4. Agent report 不能改变 Issue 状态、Owner、Executor、层级、Workflow 状态或其他
   参与者的执行策略。
5. 网络重试不会创建重复可见报告。
6. 新 post 不会打断运行中的 assistant/tool 序列，也不会自动唤醒全部参与者。
7. 长历史受到限制，tool 与 UI 都说明被省略的内容。
8. Agent 内容以带来源的不可信证据进入另一个模型。
9. Workflow join 仍根据权威执行状态判断就绪，永远不依赖 report 文字。
10. 用户能够区分分享 finding、请求某个参与者行动和等待多个参与者。

## 20. 待解决问题

### 产品

- 用户能否理解 Blackboard 是过滤后的 Issue 视图，还是会期待独立持久对象？
- 每个顶层 Issue 都应隐式成为 Topic，还是只在存在多个子执行时启用协调？
- 父子两层 Issue 是否足以服务真实协调群组？
- 哪些更新值得在终态结果之前共享？

### 内容与权威

- comment 约定是否足以验证体验，还是有效过滤一开始就需要 typed entry？
- 哪个 actor 可以发布 accepted decision，其权威位于哪里？
- 被 supersede 的 entry 应继续出现在普通 Discussion 中，还是只在历史中出现？
- Topic snapshot 应包含哪些 Artifact relation？

### 投递与调度

- pull-based coordination 可以容忍多旧，才值得增加持久订阅？
- 哪些参与者确实需要 per-Topic cursor？
- Addressed Signal 是否只能沿 parent/supervisor 关系发送，还是 Issue executor 足以
  支持另一种固定 route？
- 哪些更新可以触发 synthesis run，谁授权其预算？

### 所有权

- Issue service 是否拥有 Topic 推导和第一版 comment-backed projection？
- 如果 typed entry 落地，它属于 Issue capability，还是需要独立 pure core coordination
  package？
- Workflow 是通过只读投影向 Topic 暴露 join 状态，还是在状态变化时追加 system
  entry？
- 本地 Issue-linked Session 如何保存最后观察到的 Topic basis？

### 生命周期

- 子 Issue 移到另一个 parent 后，Topic entry 如何处理？
- 运营 notice 应显示多久？
- 已完成 Topic 能否重新打开，旧参与者是否恢复 capability？
- 如何表示已删除或脱敏 entry，而不伪造先前 TaskRun 的 provenance？

## 21. 采纳前所需的证据

- 真实父子 Issue 旅程会反复产生在终态综合前就很重要的跨子项 finding。
- 与隔离的子 Discussion 相比，共享 Topic context 能减少重复工作或冲突假设。
- 无需人工持续复制消息，参与者也会读取并使用 feed。
- 有用 post 是简短且有来源的协调陈述，而不是原始 transcript 或 progress 噪声。
- Pull-based snapshot 能揭示 push、subscription 或 unread cursor 是否确实需要。
- 用户能够区分已发布信息、addressed request 和 durable join。
- Prompt-injection 与授权测试证明 Topic 文本不能选择目标、获得权限或外泄其他
  Issue 数据。
- 成本数据证明上下文与唤醒策略不会让模型调用增长快于协调收益。
- Crash 与 retry 测试证明已接受 post 不会丢失或重复。
- Workspace 实验能区分哪些失败属于通信，哪些需要隔离或 change integration。

有价值的指标包括：

- 每 TaskRun 的 Topic snapshot 读取与显式刷新次数；
- 每参与者 post 数量，以及在最终综合中被引用的比例；
- 功能前后的重复工作发现次数；
- 相关 post 到另一参与者观察到它之间的时间；
- 未读或遗漏 entry 数量；
- addressed Signal 与 broad post 的比例；
- 协调导致的模型唤醒与 token；
- 后续工作使用过期或被取代 finding 的次数；
- 新鲜感消失后，同一团队是否反复使用。

## 22. 架构落点

如果证据支持实现，候选所有权如下：

| 职责 | 候选所有者 |
|---|---|
| 从 Issue 层级推导 Topic | `internal/service/issue`，基于 `internal/core/issue` 的纯规则 |
| Comment-backed Topic snapshot 与 report 授权 | `internal/service/issue` |
| 面向模型的限定范围读取/报告命令 | `internal/interface/cli` 中的 `buildmax` 子命令，经由 worker route 与 run bridge（`internal/infra/runbridge`） |
| Worker credential 与由 TaskRun 推导的范围 | worker client 与 Server worker handler |
| 本地认证范围 | `internal/interface/client` 与 `internal/agentapp` |
| 可选 typed entry domain | `internal/core/issue`，除非证据证明它有独立变化原因 |
| Typed entry persistence | `internal/infra/db` |
| Addressed delivery | 由 mailbox/supervisor 提案决定的 service |
| Join 与 readiness | `internal/core/workflow`、`internal/service/workflow`，或 Session supervisor |
| Portal 的 Topic projection | Portal，基于 Issue 与 coordination service response |
| Workspace isolation 与 apply | workspace capability，绝不属于 feed store |

Agent Loop 只接收一次受限投影并发出 tool call。它不拥有 Topic membership、delivery、
subscription 或 join state。

## 23. 若被采纳后的去向

如果证据支持该方向：

1. 在 `ROADMAP.md` 中安排获采纳的切片及其前置条件。
2. 将持久的 Topic、authority、delivery 与 synchronization 决策移入各自相关设计记录，
   不留下一个包揽全部问题的宽泛设计。
3. 与 Session mailbox 和 Agent-native collaboration 提案对齐边界，退役或收窄被取代
   的提案内容。
4. 分别为 comment-backed prototype、确有必要时的 typed feed、mailbox integration、
   UI projection 与 recovery evidence 创建 backlog item。
5. 只有相关行为实际变化时，才更新 Issue、Task、Workflow、tool、Server、data model、
   Portal、CLI 与 Desktop 文档。
6. 在受支持界面交付后增加用户文档。
7. 一旦被接受、拒绝或取代，就删除本提案；Git 历史保留讨论。

如果证据表明用户只需要终态综合，应改进现有 Task result 与 Workflow fan-in。如果只
需要 addressed question 而不需要广泛感知，应实现受限 mailbox，不创建 Blackboard。
如果只需要更清晰的人类讨论，则改进 Issue Discussion，不引入 Agent 协调子系统。

## 24. 候选方向

候选方向是：

> BuildMax 应把顶层 Issue 视为其子工作项的协调 Topic。其 Blackboard 是受限、带
> 来源、以追加为主的信息流；最初应从现有 Issue comment 投影，而不是引入新实体。
> feed 用于共享信息，但不保证投递，也不决定 readiness。定向行动使用持久 mailbox
> Signal；同步使用由 Workflow 或 supervisor 拥有的 join condition；workspace 变更
> 保持隔离和可审查。Agent capability 在构造时限定范围，report 始终是不可信证据，
> 自动处理必须明确授权并受预算约束。

这一方向比通用协作底座更窄，也比任意 Agent 消息总线更安全。是否值得实施，取决于
共享 Topic 信息能否在终态综合之前真正改善并行 Issue 工作。
