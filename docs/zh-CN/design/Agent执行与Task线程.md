# Agent 执行与 Task 线程

> **翻译说明：** 本文是[英文原文](../../design/agent-execution-and-task-threads.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。

> **受众：** 贡献者、产品设计者与运维人员 · **状态：** 进行中 — 所有权切换（§13.1）、直接 Agent 准入（§13.2）、Task 线程后端与 Portal 的 Task 页面（§13.3），以及移除合成 Conversation（§13.4）均已交付，且各自都附有 MySQL 并发争用或浏览器测试证据。§14 逐项精确记录了已验证的内容与仍然遗留的部分；不要在别处重复那份清单。

相关文档：[产品愿景](产品愿景.md)、[界面定位](界面定位.md)、[Portal 执行模型](Portal执行模型.md)、[本地会话存储](本地会话存储.md)，以及[服务器架构](../contribute/architecture/server.md)。

创建时间：2026-09-02

本记录是已退场的"两层 Agent 架构"圆桌讨论的综合成果（参见[提案索引](../proposals/README.md)；该讨论留下的四份立场文件保存在 git 历史中，随本记录一并退场）。§1 的决定——当用户或产品已经选定要用的能力时就直接执行，并通过不可变的 Task/TaskRun 授权边界来完成准入——正是那场圆桌讨论中"反方意见"自身提出的建议，如今已经过评估并交付。它并未回答圆桌讨论遗留的更大的开放性问题：显式的 Agent 主体与授权、拓扑无关的工作基底，或受治理的对等/黑板式协作。这些问题在圆桌讨论退场时被搁置，也不在本文讨论范围之内。

## 目录

- [1. 决策](#1-决策)
- [2. 当前耦合](#2-当前耦合)
- [3. 领域边界](#3-领域边界)
- [4. 支持的入口路径](#4-支持的入口路径)
- [5. Task 是 Agent 的执行线程](#5-task-是-agent-的执行线程)
- [6. 继续、重试与新建 Task](#6-继续重试与新建-task)
- [7. Conversation 是独立的前台界面](#7-conversation-是独立的前台界面)
- [8. 数据、API 与存储形态](#8-数据api-与存储形态)
- [9. Agent 版本与运行时解析](#9-agent-版本与运行时解析)
- [10. 授权与信任边界](#10-授权与信任边界)
- [11. Portal 体验](#11-portal-体验)
- [12. 故障、恢复与并发](#12-故障恢复与并发)
- [13. 实施顺序](#13-实施顺序)
- [14. 验证](#14-验证)
- [15. 被拒绝的替代方案](#15-被拒绝的替代方案)
- [16. 遗留问题](#16-遗留问题)

## 1. 决策

Agent 定义可以被直接调用。直接调用会创建一个归属于 Space 的 Task 及其第一个 TaskRun，然后复用现有的调度器（scheduler）、Worker 与共享的 Agent 运行时。它不会创建 Conversation，也不会要求前台 Conversation 模型去选择用户已经选定的 Agent。

Task 是围绕一个 Agent 目标的持久交互线程；它的 TaskRun 则是这条线程内按顺序排列的执行轮次与尝试。一次运行结束后，Task 页面会把此前的输入与输出呈现为类似聊天记录的历史，并接受新的用户输入。提交这份输入会创建一个新的 TaskRun 并恢复该 Task 的 Agent 会话，从而继续同一个 Task。

Conversation 仍然是一项独立的 Portal 前台能力。当语义路由、任务拆解或脱离前台的执行有意义时，它可以直接作答，也可以创建一个由 Agent 支撑的 Task。Conversation 只是 Task 的一个可选的发起来源与展示界面，绝不是 Task 的所有者、执行容器、授权父级、存储命名空间，也不是必须经过的触发点。

因此依赖方向是：

```text
Agent page / Issue / Workflow / API
                 |
                 v
          Task + TaskRun
                 |
                 v
        Scheduler + Worker
                 |
                 v
       Result + Artifacts + Trace

Conversation --------------------> Task + TaskRun
     optional origin                same execution plane
```

反向依赖并不存在：一次直接的 Agent 运行不会为了开始、继续、完成或变得可见而回头依赖 Conversation。

## 2. 当前耦合

当前 Portal 的 Agent 操作并不是一个类型化的执行请求，而是：

1. 创建一个 Portal Conversation；
2. 把选中 Agent 的 id、描述与指令序列化进一条用户消息；
3. 请求前台 Conversation 的 Agent 调用 `StartTask`；
4. 依赖这第二个模型把预期的 `agent_id` 正确传入 Task 创建流程。

这相当于把一个本已明确的选择转译成自然语言，再指望模型把它还原回来——这会增加延迟、令牌成本、指令的重复，还多出一个可能出错的环节。它也可能与实际选中的 Agent 产生偏差，并且会把 Agent 指令写进用户消息历史，即便 Worker 之后收到的是同一份指令、只是以系统提示层的形式出现。

这种耦合比 Portal 的这一条路由更深，它已经渗透进了数据模型本身：

- `task.conversation_id` 是非空的；
- 读取 Task 时会把 Conversation 作为必需的父级一并 join 出来；
- 创建 Task 之前必须先解析出一个 Conversation，否则无法写入；
- Issue 的 Agent 运行与 Workflow 运行只是为了满足这个要求，才创建出合成的 Conversation；
- Task 的授权在多条路径上仍然要经由 Conversation 完成；
- Worker 的工作目录与对象存储的 key 中都包含创建者与 Conversation 信息；
- 结果投递机制假定每一个 TaskRun 都欠 Conversation 一句话式的答复。

Worker 一旦拿到 TaskRun，确实是直接执行共享的 Agent 运行时的——缺陷并不在于 Conversation 内部又跑了第二套 Agent 循环，而在于准入、所有权、授权、存储、导航与结果投递全都把一个交互的发起来源当成了执行的所属父级。

## 3. 领域边界

这些稳定对象各自有独立存在的理由：

| 对象 | 拥有 | 不拥有 |
|---|---|---|
| Agent | 可复用的身份、描述、指令、版本，以及声明式的运行时策略 | 某个用户的历史记录、某次执行的状态，或可变的工作区 |
| Task | 归属 Space 的目标、选定的 Agent 身份、持久的 Agent 会话谱系，以及面向用户可见的线程生命周期 | 单次尝试的可变执行状态 |
| TaskRun | 一次输入、一次执行尝试、实际使用的确切版本与策略、状态、用量、输出、轨迹与 Artifact | 永久性的 Agent 身份，或跨运行的所有权 |
| Conversation | 前台消息、参与者、简短的交互轮次，以及可选的 Task 投影 | Worker 租约、Task 状态、Agent 会话，或执行授权 |
| Issue | 共享的工作与结果上下文 | 私有的 Agent 历史，或 Worker 生命周期 |
| Workflow | 确定性的计划、步骤推进与完成策略 | 强制性的 Conversation，或隐式的、由模型主导的状态机 |

Agent 可以被直接执行，并不意味着一行 Agent 记录就变成了一个正在运行的进程：同一个 Agent 可以同时服务许多用户和许多 Task。每一次调用仍然会拿到一个显式的 Task 与 TaskRun 信封，这样取消、重试、配额、轨迹、Artifact 与审计才始终有一个权威的归属者。

Space 对每一项 Portal 执行资源都是权威的所有者；Conversation、Issue、Workflow 步骤、webhook，以及定时任务，都只是 Space 之内的、类型化的发起来源或结果投递目的地。

## 4. 支持的入口路径

### 4.1 直接 Agent 运行

当用户选定一个 Agent 并给出输入时，系统已经同时知道了执行者与目标，不需要再引入语义路由器。

```text
User selects Agent + enters input
                 |
                 v
Task application service validates Space, Agent, quota, and input
                 |
                 v
Task + first TaskRun are committed atomically
                 |
                 v
Scheduler -> Worker -> shared Agent runtime
```

响应中会标出这个 Task 与它的第一个 TaskRun；Portal 立即导航到 Task 页面，并在那里观察持久化的状态。

### 4.2 Issue Agent 运行

被指派给某个 Agent 的 Issue 会直接创建一个 Task。该 Task 会把这个 Issue 记录为发起来源与结果投影目标，而不会创建隐藏的 Conversation。

### 4.3 Workflow 步骤

Workflow 的一个步骤会直接创建一个 Task，携带该步骤快照下来的 Agent 选择与指令。Workflow 的推进只对持久化的 TaskRun 状态作出反应，不需要用一个合成的 Conversation 来承载这个 Task。

### 4.4 由 Conversation 发起的 Task

Conversation 可以通过其受限工具调用同一个 Task 应用服务，把自己的 id 与源消息作为可选的溯源信息一并提供。这样创建出来的 Task，与从 Agent 页面发起的 Task 在其他方面完全一致。

只有在用户尚未做出类型化选择、或者用户明确要求它来协调时，Conversation 才可以自行选择 Agent。一个已经提供了 `agent_id` 的客户端，不应该把它编码进自然语言散文，再交给另一个模型去解读。

### 4.5 API、Webhook 与定时任务

非会话型的调用方通过同一个服务创建归属于 Space 的 Task。每一种来源都会记录一个类型化的触发来源，以及在其边界上可获得的调用者身份；没有谁会为了存储或授权而凭空发明一个 Conversation。

## 5. Task 是 Agent 的执行线程

Task 比一次后台作业更持久，它是由一个 Agent 身份承载的目标所对应的稳定线程：

```text
Task
  agent_id
  stable session_id
  objective and title
  optional origin relations
  |
  +-- TaskRun 1: initial input -> output
  +-- TaskRun 2: follow-up input -> output
  +-- TaskRun 3: follow-up input -> output
```

每个 TaskRun 都独立地被调度、设限、可取消、计量、追踪，并最终进入终态。Task 在等待下一次用户输入期间不占用任何 Worker；一旦恢复了持久化的会话状态，下一个 TaskRun 就可以在任何符合条件的 Worker 上执行。

Task 页面上的聊天历史，只是对执行事实的一种投影：

| 展示项 | 来源 |
|---|---|
| 用户轮次 | `task_run.input`，加上操作者与创建时间 |
| Agent 轮次 | 终态的 `task_run.output`、状态与结束时间 |
| 运行中状态 | 当前 TaskRun 的状态与流式增量 |
| 文件 | 该 TaskRun 明确发布的 Artifact |
| 详情 | 轨迹、模型调用用量、运行时版本、策略与失败信息 |

这份投影不会把 TaskRun 的输出复制进 Conversation 的消息表，也不会暴露模型内部隐藏的推理过程；工具调用与诊断事件仍然只保留在轨迹里，只有通过明确的详情视图才能看到。

## 6. 继续、重试与新建 Task

这三个操作对应不同的产品意图，在领域模型里也始终是不同的操作。

### 6.1 继续

"继续"在一个已存在的 Task 上接受新的用户输入，并创建一个新的 TaskRun。它会保留：

- Task 与 Space 的所有权；
- Agent 身份；
- Task 的会话谱系；
- 此前对模型可见的会话历史（会受压缩影响）；
- 该 Task 拥有的、最初从 Space 文件播种而来的工作区检查点；
- 该 Task 当前不可变的 Plugin 环境；
- 指向此前各次运行输出与 Artifact 的链接。

"继续"不需要、也不会创建 Conversation。当 Task 已经存在处于 `PENDING`、`SCHEDULED` 或 `RUNNING` 状态的活动 TaskRun 时，"继续"会被拒绝。

### 6.2 重试

"重试"把某个已选定的终态 TaskRun 的输入原样作为另一次尝试重新执行。它会记录 `retry_of_task_run_id`，且不会宣称用户提供了新消息。它在 Task 服务所定义的重试规则下，沿用同一个 Task 与同一条会话谱系。

### 6.3 新建 Task

"新建 Task"会开启一个全新的目标与一条全新的 Agent 会话，即便使用的是同一个 Agent 定义。当此前的上下文不应影响下一次运行时，用户会选择这个操作。

### 6.4 连续性承诺

已交付的实现所承诺的是持久的 Agent 会话连续性，而不是一个永久运行的进程或者一个粘性 Worker。一旦"继续"成为面向用户的承诺，会话恢复失败就必须被记录并可见——它不能在 UI 声称线程已经继续的同时，悄悄地退化成一个全新的会话。

[Task 工作区检查点](Task工作区检查点.md) 规划了这份承诺在文件系统层面对应的另一半：继续会恢复该 Task 独有的、不可变的一份 `workspace/` 检查点；重试会回到被重试那次运行所记录的基线；两条路径都不会在恢复失败时默默回退到当前的 Space 文件。同一份记录还把扩展后的 `buildmax-home/plugins/` 目录树当作一种可重建的投影来处理：继续会使用该 Task 的 Plugin 环境头部，重试会重建被重试那次运行的基线，而自主安装只有跨越一个新的 TaskRun 边界之后才会生效。这条狭窄的恢复谱系，并不会让此前已经撤回的"通用的、带版本的工作区"或"按时间线回溯"这类产品重新出现。浏览器 profile 的保留、面向用户可见的工作区历史、变更集、回滚与合并，仍然在这两份记录的范围之外。

## 7. Conversation 是独立的前台界面

Conversation 仍然是面向 Web 的前台聊天能力，拥有自己的消息记录，以及一个适合交互、澄清、给出轻量级答案与编排的、受限的 Agent 循环。

它可以：

- 不启动任何后台工作，直接作答；
- 询问缺失的信息；
- 在用户尚未选定 Agent 时代为选择；
- 创建一个或多个 Task；
- 通过受限工具查看结构化的 Task 状态；
- 为它发起的 Task 渲染链接或卡片。

它不可以：

- 成为某个 Task 的必需父级；
- 用自然语言改写一个已经类型化的 Agent 选择；
- 拥有 Worker 状态、租约、取消、重试或 Artifact；
- 仅因为某个 Task 提到了自己的 id，就授权对该 Task 的访问；
- 把 Worker 的原始输出当作一条用户消息接收；
- 在结果变得持久或可见之前，要求再多一次前台模型调用；
- 代替一个并未主动要求的 Issue、Workflow、webhook、直接 Agent 运行或 API 调用方去创建 Conversation。

一个由 Conversation 发起的 Task，可以把一张确定性的状态/结果卡片投射回它的发起 Conversation；这层关系是可选的。一个另行论证过其必要性的展示层可以生成一段助手摘要，但展示层的失败不能隐藏或篡改 TaskRun 的结果，也不能在没有新的授权轮次的情况下启动另一次执行。

## 8. 数据、API 与存储形态

### 8.1 Task 的所有权与来源

目标形态的 Task 让 Space 所有权变得显式，同时让 Conversation 变为可选：

```text
Task
  id
  space_id                  required, authoritative owner
  agent_id                 required for Agent-backed execution
  conversation_id          optional origin/presentation relation
  issue_id                 optional shared-work relation
  workflow_step_run_id     optional deterministic-plan relation
  created_by               required actor
  session_id               stable Task session
  title / objective
  status / last_run_id
```

`conversation_id` 这个字段名可以在保持可选的同时不变；它表达的含义是发起来源与展示关系，而不是亲子关系。如果将来一个 Task 需要投影到多个 Conversation，投递机制应当获得属于它自己的关系，而不必改变 Task 的所有权模型。

所有传入的来源 id 都必须能在 `space_id` 之内解析；缺失来源属于正常的直接执行，而不是错误。

### 8.2 TaskRun 的溯源信息

每个 TaskRun 至少会记录：

- Task 的 id；
- 提供会话连续性的、不可变的前一个 TaskRun；
- 输入与创建者；
- 触发来源；
- 存在时的源消息；
- 适用时的重试谱系；
- 实际使用的 Agent 版本；
- 基线以及可选的结果 Plugin 环境版本；
- 用来解释这次运行所需的沙箱、模型与凭证授权证据；
- 状态、时间戳、用量、输出、轨迹与 Artifact；
- 会话恢复是成功、降级，还是根本没有被请求过。

`previous_task_run_id` 让这条线性的延续边可被查询，并且在 `task.last_run_id` 前移到新一次运行之前，先固定住会话来源。仅凭 Task 的顺序加一个共享的会话 id 是不够的——如果未来某个功能允许分支，那需要一份独立的、经过认可的设计。

### 8.3 API 方向

权威的创建入口应当挂在 Space 范围下，而不是嵌套在 Conversation 之下。可能的形态是：

```text
POST /api/spaces/{space_id}/tasks
  { agent_id, input, optional origin fields }

GET  /api/spaces/{space_id}/tasks/{task_id}
GET  /api/spaces/{space_id}/tasks/{task_id}/runs
POST /api/spaces/{space_id}/tasks/{task_id}/runs
  { input }
```

第一个 POST 原子地创建 Task 及其第一个 TaskRun；最后一个 POST 表示"继续"，会创建一个带新输入的 TaskRun；"重试"仍然是一个独立的操作，它需要指明被重试的是哪一次运行。

出于可发现性的考虑，可以存在一条嵌套在 Agent 之下的便捷路由，但它必须委托给同一个 Task 服务，返回同一个 Task 资源，不能拥有一套独立的执行规则。

以上这些路由是目标语义，并非已经交付的 API；路由注册表始终是事实来源，OpenAPI 必须与之同步变化。

### 8.4 存储命名空间

Worker 目录与对象存储按持久所有权与执行身份来寻址：

```text
spaces/{space_id}/tasks/{task_id}/runs/{task_run_id}/...
```

确切的前缀属于基础设施层面的决定，但创建者 id 与 Conversation id 都不应该出现在规范的命名空间里。一个 Task 在创建者账号发生变化之后依然存在，也不需要依赖 Conversation 才能定位它的 Session、Artifact、轨迹或运行级别的全局数据。

本项目处于 Alpha 阶段，不会通过双读或双写来保留旧的 key 形态。所有权切换会把行模型、领域类型、线上 DTO、key 构造器、调用方、文档与测试一并改掉。

## 9. Agent 版本与运行时解析

Task 绑定的是稳定的 Agent 身份，而每个 TaskRun 都会为那一轮对话快照下实际使用的 Agent 版本与执行策略。

目标解析顺序是：

1. 准入阶段校验该 Agent 属于这个 Space 且处于激活状态；
2. 创建 TaskRun 时快照 Agent 版本，以及决定"应该运行什么"所需的稳定执行声明；
3. Worker 认领时，会针对这份不可变的授权，把动态取值、凭证与部署位置具体化；
4. Worker 收到的是一份执行规格，无法将其扩大。

在准入阶段就快照 Agent 版本，可以避免管理员在 Worker 认领某个排队中的运行之前编辑了 Agent，从而导致这次运行发生变化——这是在有意收紧目前"在认领那一刻才生效"的行为。

"继续"会保留 Agent 身份，并使用新 TaskRun 被准入那一刻处于激活状态的版本。这样一来，历史记录在身份维度上始终连贯一致，同时每一轮又如实标明了自己所使用的指令。一个正在运行或已经被准入的 TaskRun 永远不会被后续的编辑改写。

删除或停用一个 Agent 会阻止新的 Task 与新的"继续"运行；一个已经被准入的 TaskRun 会保留自己的快照并可以正常完成。恢复到某个旧版本，会在既有的版本规则下创建一个更新的 Agent 版本，而不会改写 TaskRun 的历史记录。

## 10. 授权与信任边界

Task 的访问权限通过 `task.space_id` 与当前的 Space 成员身份来授权。处理程序不会为了证明 Task 的归属而去查询 Conversation；这条规则同样适用于 TaskRun、Artifact、轨迹、模型调用账本、取消、重试与"继续"。

一个可选关系永远不会赋予权限，具体而言：

- Conversation 的 id 不会授予对其 Task 的访问权限；
- Issue 关系不能绕开 Space 成员资格检查；
- 源消息不能用来选择另一个 Space 或 Agent；
- Worker 运行令牌的作用范围始终限定在一个 TaskRun 之内；
- Worker 从 Server 端状态推导 Space、Task、Agent 与来源数据，而不是取自模型提供的参数。

执行权限属于 TaskRun，而不属于 Task。`task_run.created_by` 是接受资格检查的主体，运行令牌携带的也是它的用户 id；`task.created_by` 只是持续线程的来源记录。因此另一位成员发起的"继续"以该成员身份运行，禁用 Task 的创建者不会让它停止。无人值守的工作会在 Schedule 触发、派发、worker 首次拉取，以及每分钟一次针对活跃运行的对账器中，再次检查发起者的账户是否启用且仍是该 Space 的成员；权限被收回的运行以带 `cancel_reason` 的 `CANCELED` 结束，而不是 `FAILED`。该规则及其已知缺口见[系统管理](系统管理.md) §8.2。

Task 历史会按信任级别区分内容：用户输入是一条指令；Worker 输出是不受信任的结果数据；运行时状态与策略证据则是结构化的事实。把它们投影到同一个页面上，并不会让它们变成同一种消息角色，也不会让它们自动进入未来某次模型上下文。

"继续"是通过 Task 的会话包来重建对模型可见的历史的，共享的 Agent 运行时会在其中应用自己一贯的压缩与工具结果处理规则。Portal 不能靠拼接渲染出来的 HTML 或 TaskRun 输出，来自行重建一个模型会话。

## 11. Portal 体验

### 11.1 Agent 页面

一张 Agent 卡片提供的是 `Run` 或 `New task`，而不是一个唯一行为就是打开一个通用 Conversation 的按钮。输入弹窗展示的是用户的任务输入，而不会把 Agent 的描述或指令复制进可编辑的用户文本里。

提交后会导航到新的 Task 页面，选中的 Agent id 会作为一个类型化的请求字段一并携带过去。

`Chat with coordinator` 仍然可以通过独立的 Conversation 入口使用。如果日后的产品需要一种绑定到单个 Agent 的前台聊天，那应当是一个带有结构化绑定的、显式的 Conversation 模式，而不是靠提示文本要求另一个 Agent 去做选择。

### 11.2 Task 页面

Task 页面会展示：

- Agent 身份，以及每次运行所使用的版本；
- Task 的标题、来源与当前状态；
- 按时间顺序排列的用户输入与 Agent 输出轮次；
- 当前活跃 TaskRun 的实时流式输出；
- 每一轮的状态、用量、轨迹、Artifact 与失败详情；
- 各自独立的"停止"与"重试"操作；
- 底部用于"继续"的输入框。

运行处于活跃状态时输入框会被禁用；成功、失败或取消之后，只要 Agent 可用、Space 授权与配额允许，就会重新接受新消息。发送会创建一个新的 TaskRun，此前的各轮内容保持不可变。

### 11.3 Conversation 页面

Conversation 会继续渲染自己的用户与助手记录。从这里发起的 Task 会以结构化卡片或链接的形式，排列在消息旁边；点开卡片会导航到 Task 页面，那里才有完整的 TaskRun 历史与"继续"输入框。

一个直接发起的 Agent Task 不会出现在与它无关的 Conversation 列表里；要发现它，需要通过该 Agent 的执行历史，或是某个 Space 的任务/历史界面。

### 11.4 Agent 执行历史

Agent 详情界面会按最新在前的顺序列出该 Agent 的各个 Task，附带状态、来源、创建者、最近活动时间、运行次数与最新结果摘要；选中其中一个会打开对应的 Task 页面。这份列表按 Space 限定范围并分页展示，不会去扫描 Conversation 消息或轨迹。

## 12. 故障、恢复与并发

现有的 TaskRun 生命周期依然是权威的；本设计在此之上新增以下要求：

- 创建 Task 及其第一个 TaskRun 是原子操作；
- 每个 Task 至多同时存在一个活跃的 TaskRun；
- "继续"在 API 边界上是幂等的，客户端的重试不会因此产生两轮对话；
- Server 重启不会丢失一次已经提交的"继续"请求；
- 会话恢复的结果会在执行真正开始之前被记录下来；
- 恢复失败必须可见，并遵循一套明文记录的回退策略或失败关闭（fail-closed）策略；
- 流式增量只是一次性的展示内容，终态输出始终从 TaskRun 状态中读取；
- 直接发起的运行会到达终态，并且即便没有任何 Conversation 或结果投递记录，也依然可以被检查；
- 一张由 Conversation 发起的结果卡片是从 Task 状态派生出来的，即便可选的展示层出现故障也是如此。

Task 的状态是其当前或最近一次运行的投影。一个已经处于终态的 Task，可以在"继续"提交了一个新的 TaskRun 之后重新变为活跃；历史上的各个 TaskRun 始终保持终态、不可变。

首个发布版本让 Task 保持线性：分支、并发的子任务与会话合并都不会从聊天界面中被自动推断出来，它们仍然在本设计的讨论范围之外。

## 13. 实施顺序

### 13.1 所有权切换

在一次所有权变更中，让 Task 归属于 Space、让 Conversation 变为可选，涉及：

- 领域层的 Task 与创建输入；
- `taskRow`、各种读取、join 与存储查询；
- 服务层的校验与授权；
- Worker 的线上类型与 task-run 作用域；
- 文件系统与对象存储的 key 构造器；
- Artifact、轨迹、模型调用、重试、取消与结果查询；
- Issue 与 Workflow 的 Task 创建逻辑；
- 测试、OpenAPI、数据模型、服务器架构与当前状态文档。

不保留第二套"源自 Conversation"的所有权规则或兼容适配层：既有的 Task 会直接拿到自己早已确定的 Space id；本项目没有已发布的、需要双重表示的持久化数据需要照顾。

### 13.2 直接 Task 准入

新增一个归属于 Space 的 Task 创建操作，并让 Agent 页面调用它；移除 Agent 预览提示以及"先创建 Conversation"这一绕行步骤；在 TaskRun 准入阶段记录 Agent 版本与执行声明。

### 13.3 Task 线程界面

新增 Task 历史查询与 Task 详情页面；新增直接的"继续"操作，遵循与 Conversation 工具相同的 Task 服务规则；让"重试"在视觉与语义上都与"继续"明显区分开。

### 13.4 移除合成 Conversation

把 Issue 的 Agent 执行与 Workflow 步骤执行改为直接创建 Task；只为那些明确指定了 Conversation 投递关系的 Task 建立结果投递义务；移除那些仅仅为了隐藏合成 Conversation 而存在的过滤器与通道。

### 13.5 Conversation 投影

把源自 Conversation 的 Task 卡片保留为一种投影；移除自动的原始结果回放，以及任何被当作"完成前提条件"的前台模型调用；把可选的展示层单独评估。

每一项实施改动本身都保持内部完整——尤其是所有权切换，它会把所有调用方一次性整体迁移过去，而不是让新旧两套授权或 key 规则并行运行。

## 14. 验证

只有当自动化证据至少覆盖以下各项时，本实现才算被接受。每一项都标注了当前状态；带有未完成事项的条目会明确点出那项工作，而不是把缺口隐而不谈。

1. **已完成。** 从 Agent 的 Portal 卡片启动会创建一个 Task 和一个 TaskRun，且不会创建 Conversation。见 `portal/e2e/task-thread.spec.ts`。
2. **已完成。** 精确类型化的 `agent_id` 与被准入的版本会直接送达 Worker，中途没有前台 Conversation 模型调用——这一点由结构本身保证：直接准入从不打开 Conversation，也不会调用前台模型；同一份测试规格中还直接断言了 `task.agent_id`。
3. **部分完成。** 在 `conversation_id` 缺失的情况下，终态输出、取消与重试都能正常工作——由同一份测试规格以及 Task 页面的"停止/重试"操作覆盖。Task 页面现在通过 `streamTaskOutput` 消费 `GET /api/spaces/{space_id}/tasks/{task_id}/stream`，同时仍以每 1.5 秒一次的轮询获取生命周期状态。**遗留：** 直接（无 Conversation）Task 的流式传输/重连验收证据，以及 Artifact/轨迹/用量证据；仅仅接入 SSE 并不能证明这些流程本身成立。
4. **已完成。** 刷新后的 Task 页面会从 TaskRun 记录中重建出每一条用户输入与 Agent 输出——该页面不持有任何无法通过 `GET .../tasks/{task_id}/runs` 在刷新后重建出来的状态。
5. **已完成。** 从 Task 页面提交会在同一个 Task 上创建一个带新输入的 TaskRun。`TestContinueRunSendsRestoredHistoryToModel` 在各自独立的目录中通过对象存储执行两次 Worker 运行，证明第二次模型请求中包含了第一轮的交互内容；`task-thread.spec.ts` 则证明被准入的这次运行把第一次运行记为自己不可变的前驱。
6. **已完成。** "重试"会重复执行选定的运行，并且在数据层面（`retry_of_task_run_id`）与 UI 层面（各自独立的操作）都能与"继续"区分开。
7. **已完成。** 并发的"继续"请求最多只会准入一个活跃的运行，幂等的客户端重试也不会造成重复——见 `internal/infra/db` 中的 `TestCreateTaskRunHasOneActiveWinnerUnderContention` 与 `TestCreateTaskRunIsIdempotentByKey`，均针对真实 MySQL 运行，并通过变异测试核验。
8. **部分完成。** 对 Agent 的编辑不会影响一个已经准入的运行（沿用既有的"先写入者获胜"的 `agent_revision` 保护机制，本设计未作改动）。**遗留：** Task 页面尚未展示某次运行实际使用的是哪个版本（见 §11.2）。
9. **已完成。** 已删除的 Agent 无法再接受新的工作——见 `internal/service/task` 中的 `TestCreateTaskRefusesADeletedAgent` 与 `TestCreateRunRefusesWhenTheTasksAgentWasDeleted`。这套代码库里没有单独的"已停用"状态，删除是唯一的一种。至于一个已准入的运行能在自己的快照下正常完成，这一点源自 Worker 在运行过程中从不重新检查 Agent 是否存在，因而没有为此单独编写测试。
10. **已完成。** Issue 的 Agent 执行与 Workflow 执行都不再创建合成 Conversation——`workflow`/`issue_agent` 这两个通道及其配套的过滤机制已被彻底删除，而不只是被隐藏。
11. **未经验证，但大概率不受影响。** Conversation 可以创建一个 Task，并获得一张持久卡片，而不会成为该 Task 的授权或存储父级——这条路径未被本设计改动，`portal/e2e/conversation.spec.ts` 依然通过，但这一轮新增的内容没有直接对它做验证。
12. **对本设计新增或改动的路由而言已经完成。** `space_authz_matrix_test.go` 覆盖了 Task/TaskRun/Artifact/trace/llm-call/取消/重试各路由的跨 Space 拒绝场景。
13. **遗留。** Worker 丢失、Server 重启，以及会话恢复失败后应留下可解释的状态——这些尚未针对直接（无 Conversation）Task 专门验证。`StaleRunReaper` 以及既有的重启安全机制在本设计之前就已存在，没有证据表明被本设计破坏，但这一轮也没有新增专门指向这条路径的测试。会话恢复失败的可见性本身还是一个悬而未决的设计问题——见 §16。
14. **已完成。** MySQL 集成测试覆盖了这个可为空的关系（`TestCreateTaskDirectHasNoConversation`）、直接创建、运行并发（见上文第 7 项），以及 Space 授权（同一测试中的跨 Space 场景），均通过 `./make test mysql` 运行。
15. **部分完成。** Portal 浏览器测试覆盖了直接运行、历史记录重新加载、继续与重试（`task-thread.spec.ts`，已针对真实的 Compose 部署运行两次且没有失败）。**遗留：** 这一轮没有新增浏览器测试来覆盖由 Conversation 发起的 Task 卡片。

范围内的文档、OpenAPI 精确匹配测试、架构测试、`git diff --check`，以及相关的 `./make check` 范围，都必须随同一次改动一起通过。

## 15. 被拒绝的替代方案

### 15.1 保留强制性 Conversation 并隐藏合成记录

隐藏自动生成的 Conversation 只是修好了一个列表，并没有修好所有权模型本身；存储、授权、结果投递，以及未来每一种新的执行来源，都仍然会被耦合到一个根本没人使用的对象上。

### 15.2 让前台模型解读已选定的 Agent

当选择本身还不确定时，模型确实有用；但它并不是传递一个用户早已选定的 id 的可靠通道。已经类型化的意图，就应该继续保持类型化。

### 15.3 绕开 Task 与 TaskRun 直接从 Agent 执行

这会让取消、重试、状态、配额、Artifact、轨迹与审计变成第二套执行系统。Agent 是可复用的配置，Task 与 TaskRun 才是执行的信封。

### 15.4 把 Task 的往返记录存成 Conversation 消息

这两类记录在所有权、生命周期、信任级别与失败语义上都不相同。TaskRun 已经拥有执行的输入与输出；把它们再复制进 Conversation 的消息里，会制造出两个事实来源，并有把 Worker 输出当作用户指令重放的风险。

### 15.5 让一个可变会话归属于 Agent

Agent 是共享的，可以并发运行；如果把一个可变会话挂在 Agent 上，会在用户、Task 或 Space 之间泄漏上下文，也会让版本与并发的语义变得不连贯。会话谱系应当归属于 Task。

### 15.6 让 Task 成为一个永久运行的 Worker

连续性是逻辑上的、持久化的承诺，而不是承诺某个进程会一直驻留。Worker 始终保持弹性，每个 TaskRun 只具体化自己需要的状态，然后终止。

## 16. 遗留问题

核心边界已经确定，以下问题仍需实现或使用层面的证据来回答，但它们都不会重新打开这个边界：

- Task 的会话恢复应当失败关闭，还是应当带着明确的"连续性已降级"状态继续下去；
- 保留可写的工作区或浏览器状态是否有足够的价值，值得为此单独立项设计；
- 用户是否需要为新的"继续"运行钉住某个较旧的 Agent 版本；
- 一个 Task 将来是否可以分支出多条延续；
- 不活跃的 Task 会话包在归档之前应该保温多久；
- 一个前台的 Conversation 展示层，是否能比确定性的 Task 卡片带来实质性更好的效果；
- Agent 历史是否需要在状态、来源、创建者与时间之外的更多筛选维度。

证据应当衡量"继续"的使用频率、会话恢复的成功率、直接执行相对于路由所避免的准确性损耗、模型调用与延迟的下降幅度、结果卡片的实用程度，以及真正需要 Conversation 主导协调的 Task 所占的比例。
