# Agent 运行时自省与上下文控制

> **翻译说明：** 本文是[英文原文](../../proposals/agent-runtime-introspection-and-control.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
>
> **受众：** 贡献者与产品评审者 · **状态：** 提案——讨论中
>
> **开启日期：** 2026-09-13

相关文档：[路线图](../ROADMAP.md)、[当前状态](../current-state.md)、
[Agent Loop](../contribute/architecture/agent-loop.md)、
[工具](../contribute/architecture/tools.md)、
[上下文持久性](../design/上下文持久性.md)、
[Session 用量统计](../design/会话用量统计.md)和
[持久化运行轨迹](../design/持久化运行轨迹.md)。

## 目录

- [1. 问题与暂定建议](#1-问题与暂定建议)
- [2. 用户结果与当前证据](#2-用户结果与当前证据)
- [3. 第一性原理边界](#3-第一性原理边界)
- [4. 目标与非目标](#4-目标与非目标)
- [5. 候选工具契约](#5-候选工具契约)
- [6. 上下文计量](#6-上下文计量)
- [7. 安全压缩语义](#7-安全压缩语义)
- [8. 状态、并发与分层](#8-状态并发与分层)
- [9. Surface、权限与安全边界](#9-surface权限与安全边界)
- [10. 选项与权衡](#10-选项与权衡)
- [11. 失败模式](#11-失败模式)
- [12. 证据计划与分阶段交付](#12-证据计划与分阶段交付)
- [13. 开放问题](#13-开放问题)
- [14. 若获采纳的可能归宿](#14-若获采纳的可能归宿)

## 1. 问题与暂定建议

长时间运行的 Agent 可以检查文件、外部系统、后台 Job 和委派工作，却不能直接向
运行时询问一组更简单的自身问题：上下文占用了多少、这次运行已经持续多久、还剩
多少迭代预算、能否执行压缩，或者自身任务列表中的哪些部分已经完成。用户侧通过
`buildmax info`、TUI 与 Desktop 的 `/info`、trace 和 `/compact` 已经能回答其中
许多问题；负责做出下一个决定的 Agent 却不能。

问题在于：BuildMax 是否应该向模型暴露有界的运行时自省与上下文控制，以及在
暴露之后哪些权限仍必须保留在运行时。

暂定建议如下：

1. 增加一个只读的 `RuntimeStatus` 工具，返回当前 Run、上下文、预算与现有 Todo
   状态的小型、明确限定语义的快照。
2. 评估一个独立的 `ContextCompact` 工具；它的调用只提出压缩请求，由 Agent
   Loop 在安全迭代边界执行实际操作。
3. 自动压缩继续拥有权威。Agent 请求的压缩是优化提示，绝不是避免上下文耗尽的
   机制。
4. 复用现有运行时事件、`RunStats`、Todo 状态、压缩和 trace 事实，不引入第二套
   Run 账本、任务列表或控制平面。
5. 只注册各 surface 确实能够提供的能力；状态工具绝不暴露原始 prompt、memory
   正文、Secret 或不受限的 trace 内容。

本文提出的是待验证方向，而不是已交付行为或路线图工作。

## 2. 用户结果与当前证据

必要的用户结果不是“Agent 能打印诊断信息”，而是：

> 长时间运行的 Agent 可以在时间、迭代次数或可用上下文耗尽之前调整策略，同时
> 不获得对运行时正确性或私有运维数据的控制权。

这个结果在以下常见场景中很重要：

- 探索大型代码库的 Agent 应在迭代预算不多时停止宽泛发现并开始综合。
- 在主动丢弃详细历史之前，它应保存持久决策，并确认压缩能够回收有用空间。
- 经历长串工具调用之后，它应区分当前上下文占用与已经付费的累计 token。
- 恢复的 Session 应能发现已经存在的已完成 Todo，避免重复执行其原始工具结果
  已被压缩掉的工作。
- 监督 prompt 或 evaluation 应能用真实运行时事实要求“扩张范围前检查剩余预算”，
  而不是根据消息长度猜测。

BuildMax 已经持有大部分所需事实：

| 现有来源 | 已有事实 | 当前消费者 |
|---|---|---|
| Agent Loop 迭代 | 当前及最大迭代次数 | 日志与循环控制 |
| `EventLLMStart` | 估算上下文 token 与上下文窗口 | trace 与交互式状态 |
| `RunStats` | 工具调用、prompt/completion/cache token、成本 | Run 结果与 trace |
| Run 开始时间 | 单轮 wall duration | Run 结果与 trace 汇总 |
| 持久 Todo 状态 | 待处理、进行中与已完成条目 | 模型常驻 Session 状态块与用户界面 |
| `Compact` / `compactOnce` | 自动与按需压缩 | Agent Loop 与 TUI `/compact` |

因此，缺口是访问路径与权限边界，而不是缺少遥测子系统。

## 3. 第一性原理边界

三个关注点必须保持分离：

| 关注点 | 回答的问题 | 权威方 |
|---|---|---|
| 观察 | 此刻这个 Run 的真实情况是什么？ | 运行时测量；Agent 读取 |
| 语义适应 | 基于该状态，哪些工作仍然值得做？ | Agent 决定 |
| 生命周期正确性 | 历史何时可变、Run 何时停止、限制如何执行？ | 运行时决定并记录 |

Agent 很适合改变语义策略，却不是 token 计量、历史完整性、取消、配额或 TaskRun
状态的权威。赋予它观察能力，并不要求把这些状态转换交给它。

“当前状态”也包含不可被压平的多种生命周期：

- **Run 状态**从一次 `RunLoop` 调用开始：已运行时间、迭代次数、当前 Run 的模型
  与工具调用。
- **Session 状态**跨 turn 存活：累计用量、notes、Todos 与压缩边界。
- **Task 与 TaskRun 状态**属于 Server 的持久执行平面。Todo 不是 Task，“已完成
  Todos”不能被报告成已完成 TaskRuns。
- **进程与部署状态**属于运维诊断，不能自动认定为可以安全放入模型上下文或对
  模型有用。

候选工具应明确命名并保留这些边界。

## 4. 目标与非目标

目标：

- 让 Agent 观察能够实质改善长时间运行决策的最小事实集合。
- 使每个数字的范围、新鲜度与精度清晰可辨。
- 允许 Agent 请求提前压缩，同时不在工具 worker 内部修改历史。
- 在 CLI、TUI、Desktop、worker、Conversation 和 subagent loop 中，只要底层
  能力存在，就保持行为一致。
- 继续让自动限制、hooks、trace、计量与压缩持久化充当权威机制。
- 测量该能力能否产生足够改善，以抵偿其工具 schema 与额外模型调用。

非目标：

- 面向模型的通用运行时管理 API。
- 允许 Agent 增大上下文窗口、迭代上限、deadline、配额、权限或 sandbox 权限。
- 返回环境变量、原始 system prompt、Secret、memory 正文、完整消息历史或任意
  trace 记录。
- 创建另一种名为“已处理任务”的 Todo 或 Task 表示。
- 用模型自律替代自动压缩。
- 在 provider 没有提供兼容的预检 tokenizer 时承诺精确 token 数。
- 把快速变化的状态快照持久化为新的 Session 或 Server 实体。

## 5. 候选工具契约

### 5.1 `RuntimeStatus`

`RuntimeStatus` 是只读工具。默认调用无参数，返回紧凑摘要。可选的
`detail: "tasks"` 在 Agent 需要检查已完成工作时包含现有且有界的 Todo 条目；
它不查询 Server Task。

候选结果形状：

```json
{
  "snapshot": {
    "run_elapsed_ms": 183420,
    "iteration": 12,
    "max_iterations": 50,
    "remaining_iterations": 38,
    "model_calls": 12,
    "tool_calls": 27
  },
  "context": {
    "tokens": 81200,
    "window": 128000,
    "utilization_percent": 63,
    "count_kind": "estimated",
    "measured_at_iteration": 12,
    "automatic_compaction_percent": 80,
    "compactions": 1,
    "compaction_available": true
  },
  "todos": {
    "pending": 3,
    "in_progress": 1,
    "completed": 7
  }
}
```

本文不决定确切 JSON。必须具备的语义属性是：

- 整数源值与四舍五入后的百分比同时返回，而不是被百分比替代；
- 上下文精度明确说明 `estimated`、`provider_reported` 或 `unavailable`；
- 快照标明上下文测量时所属的迭代；
- 不混合 Run 与 Session 总计；
- 不可用事实报告为 unavailable，而不是零；
- 任务详情复用有界 Todo store，并明确标为 `todos`。

结果应面向模型编写：在接近限制时，可以在结构化事实之后添加一句简短且可执行的
提示；没有证据表明阈值与当前工作相关时，不应规定 Agent 要做什么。

### 5.2 `ContextCompact`

`ContextCompact` 不接受调优参数。Agent 可以在调用前解释自己的理由，但运行时
无需自由文本 `reason` 字段来执行或审计转换。工具返回三种结果之一：

- 已安排在下一个安全边界压缩；
- 已有待处理请求；或
- 压缩不可用，或没有足够值得总结的内容。

它不接受目标 token 数、任意消息范围、替换 summary 或 `force` 标志。这些输入会
让循环中最不可靠的部分重新定义历史完整性，并重复 `splitForCompaction`、compactor
和 `CompactionHistory` 已拥有的决策。

后续边界发出现有压缩事件，并使用与自动压缩及用户请求压缩相同的计量、checkpoint、
hook、summary 截断与持久提交路径。

## 6. 上下文计量

上下文占用不是累计 prompt 用量。一个 Run 可能跨多个缓存迭代发送了 500,000 个
prompt token，而当前请求仅占用 60,000 个 token。`RuntimeStatus` 绝不能把前一个
数字标为“已使用上下文”。

第一版实现应建立唯一的权威请求估算器，并由 Agent 事件、
`AgentApp.EstimateRunUsage`、用户状态界面和新工具共同使用。它应计算 BuildMax
能够观察到的所有请求组成部分：

- 有效 system prompt 与压缩块；
- 安全裁剪后的模型可见历史；
- 常驻 memory index 与 Session 状态；
- 工具定义及其 schema；以及
- 估算器所表达的 provider framing 开销。

无法获得精确 tokenization 时，即使上下文窗口本身精确，结果仍然是估算值。上次
调用的 provider-reported usage 是计费证据，但可能包含 provider 特有的缓存或隐藏
framing，不能静默替代范围不同的估算器。

状态工具运行时，模型已经收到当前请求。因此它只能返回最近一次准备好的请求快照，
而它自身的工具结果会使下一次请求稍大。契约必须诚实说明这一点，不能声称存在不可
能实现的完全实时数字。只有在能避免递归估算自身输出时，未来的“下一次请求预测”
字段才有价值。

## 7. 安全压缩语义

普通工具执行时，assistant 工具调用组仍在完成过程中：

```text
assistant(tool_calls)
  -> gate calls
  -> execute calls, possibly in parallel
  -> append every tool result in call order
  -> next Agent Loop iteration
```

如果在 `ContextCompact.Execute` 内改变压缩边界，可能拆散 assistant/tool 关系、与
同批只读工具发生竞态，或者让持久历史与循环接下来使用的历史不一致。

安全的候选流程是：

```text
Agent calls ContextCompact
  -> tool records one pending request and returns
  -> the whole tool-call batch is committed
  -> loop reaches the post-batch boundary
  -> requested compaction runs through compactOnce
  -> queued user input is injected
  -> next model request is built from the committed compacted view
```

它与排队输入的顺序是有意设计的：上一个工具批次运行期间到达的输入应继续作为新的
用户指令，而不应仅仅因为 Agent 在看到它之前请求了压缩，就立刻被折叠进有损
summary。

即便当前 assistant/tool 组自身已经超过通常的手动保留量，它仍必须作为有效原子
尾部保留。压缩失败沿用现有语义：summary 失败时保持历史不变并向 Agent 报告；
已经产出边界却无法持久化属于致命错误，因为继续运行会让内存视图与持久视图不一致。
`PreCompact` hook 仍可以阻止操作。

自动压缩继续在现有阈值独立运行。如果一个待处理请求到达边界时，自动压缩已经完成，
该次自动操作就满足此请求，不再进行第二次压缩。

## 8. 状态、并发与分层

工具 registry 会被缓存和共享，因此两个候选工具都不能在自身 struct 中保存 Session
或 Run 指针。现有 Note 与 memory 工具展示了合适形态：`internal/core/agent`
定义小型运行时接口，并通过 `context.Context` 携带当前 Run handle；
`internal/tool` 将该 handle 适配为 LLM 工具定义；调用者为每个 Run 组装 handle。

handle 只需要有界的临时状态：

- 单调时钟记录的 Run 开始时间；
- 当前与最大迭代次数；
- `RunStats` 已经持有的本 Run 计数；
- 最近一次准备请求的上下文快照；
- 压缩次数与待处理请求位；以及
- 在可用时访问当前 Run 已有 Todo store 的能力。

它不拥有这些事实。Agent Loop 仍是写入者，在发出事件的同一状态转换上更新快照，
从而避免让事件消费者成为新的事实来源。

`RuntimeStatus` 声明只读访问，且必须能与其他只读工具并发调用，因此快照需要 mutex
或不可变的 atomic replacement。`ContextCompact` 声明写访问，所以即便它立即修改
的只是 pending bit，也会成为调度屏障。实际历史修改仍留在 loop goroutine。

Conversation 与 subagent loop 可以使用同一 core 契约。没有 compactor 的 surface
仍可暴露状态，同时省略 `ContextCompact` 或报告 unavailable；注册一个永远不可能
成功的控制能力，会向模型教授虚假能力。

## 9. Surface、权限与安全边界

状态工具只涉及当前 Run，不接受 Session、Task、TaskRun、用户或 Space 标识符。
scope 来自 Run context，因此模型不能借它枚举相邻工作。

候选权限行为：

| 工具 | Access | 默认 | 理由 |
|---|---|---|---|
| `RuntimeStatus` | read-only | allow | 关于调用 Run 的有界事实；可安全并发读取 |
| `ContextCompact` | write | allow，受 policy 和 `PreCompact` 约束 | 只通过运行时本就会自动执行的操作改变模型可见 scratch history |

`ContextCompact` 的默认值仍是开放决策，因为提前压缩有损且会产生模型调用成本。
要求交互式审批会使该工具无法在 worker 上使用；没有运行时下限的无条件许可又可能
造成浪费。候选方案之一是：只有历史中存在足够可总结材料、能够回收达到配置下限的
上下文时才允许请求，否则返回 no-op。这是运行时 invariant，不是 prompt 指令。

状态输出必须排除：

- 原始指令、压缩 summary、notes、memory 正文与消息内容；
- Secret 值和环境变量；
- 尚未对模型可见的文件系统路径；
- 其他 Run 的活动、预算、身份或成本；以及
- 属于运维诊断的进程级健康详情。

trace 应通过普通工具事件记录状态查询或压缩请求，不应把返回快照复制进第二种未脱敏
诊断格式。

## 10. 选项与权衡

| 选项 | 收益 | 成本或失败 |
|---|---|---|
| A. 状态仅面向用户；只自动压缩 | 没有新工具 schema 或模型行为 | Agent 继续猜测剩余预算，也不能在阶段切换前主动压缩 |
| B. 在每次模型请求中注入状态 | 无需工具调用；始终可见 | 永久消耗上下文和注意力，频繁改变可缓存前缀，并报告 Agent 通常不需要的事实 |
| C. 一个带 `status`、`compact` 和未来 action 的 `Runtime` 工具 | 只暴露一个工具名 | 混合读写权限，鼓励形成运行时控制杂物箱，并让并行调度依赖参数 |
| D. 分离的 `RuntimeStatus` 与边界调度的 `ContextCompact` | 权限、语义与按需成本清晰 | 两个 schema 与一个新的 per-run control handle |
| E. 先做 `RuntimeStatus`，压缩仍只由运行时控制 | 以最小变更面验证观察价值 | 无法测试能感知阶段的提前压缩是否有帮助 |

建议的实验顺序是先 E，只有状态使用表明 Agent 能合理运用这些事实时再进入 D。两种
工具不应被视为不可拆分的一项功能。

## 11. 失败模式

| 失败 | 必须采取的响应 |
|---|---|
| 模型每次迭代都轮询状态 | 工具描述劝阻轮询；loop guard 仍适用；evaluation 测量 schema 开销与调用频率 |
| 估算上下文看起来像精确值 | 始终返回精度与测量迭代；UI 和工具使用相同术语 |
| 状态结果自身增加上下文 | 记录快照语义；保持结果小型，默认省略任务详情 |
| Agent 过早压缩并丢失证据 | 设置最小有效回收量；先 checkpoint；不提供 force 或自定义范围参数 |
| 并行批次中请求压缩 | 请求只是同步 flag；整个批次提交后才修改历史 |
| 排队用户输入在 Agent 看到之前被总结 | 在 pending-input 注入之前执行请求的压缩 |
| hook 阻止压缩 | 返回有界、可执行的拒绝说明，保持历史不变 |
| compactor 或持久化失败 | 保持现有 `compactOnce` 错误语义；未回收空间时绝不声称成功 |
| Todo 数量被误认作 Server 工作 | 明确标为 Todo 状态；不通过该工具暴露 Task/TaskRun 列表 |
| 状态泄露其他 Session | 只从 per-run context 推导 scope；不接受调用方选择的标识符 |
| surface 没有 compactor 或上下文窗口 | 省略控制或报告 unavailable；不捏造零值 |

## 12. 证据计划与分阶段交付

是否采纳应取决于行为，而不是工具能否返回 JSON。

### Phase 0——统一观察

- 定义权威上下文估算输入，消除 `EventLLMStart`、`EstimateRunUsage`、trace 与 UI
  状态之间的差异。
- 添加确定性测试，覆盖 system prompt、压缩 summary、常驻状态、历史、工具
  schema 与不可用的上下文窗口。
- 记录没有 LLM 状态工具时的长脚本 Run 基线。

### Phase 1——只读原型

- 通过 per-run、context-carried handle 添加 `RuntimeStatus`。
- 覆盖并发读取、取消、缺失 Todo 状态、恢复 Session、subagent、Conversation 历史
  与 worker 组装。
- 运行要求在紧张迭代或上下文预算下切换阶段的脚本任务与真实模型任务。

所需证据：

- 相对基线的完成率与重复工作率；
- 状态调用次数，以及其定义和结果增加的 token；
- 小模型与大模型能否区分上下文占用和累计用量；以及
- Agent 是否在有用时机改变行为，而不只是复述数字。

### Phase 2——边界调度压缩原型

- 使用现有 `compactOnce` 实现 pending request 与批次后的安全点。
- 测试同一批次多次调用、并发同级工具、排队输入、自动/请求压缩冲突、hook 拒绝、
  summarizer 失败、持久化失败与取消。
- 比较仅允许自动压缩的任务与允许按阶段请求压缩的任务。

所需证据：

- 更少的上下文限制失败或重复调查；
- 不出现消息配对或持久边界损坏；
- 回收的上下文与增加的压缩成本；
- 过早或无效压缩的比例；以及
- checkpoint 后的 notes 和 Todos 能否保留后续阶段需要的事实。

### Phase 3——决策

只有当状态工具改善决策的收益足以覆盖其常驻 schema 和调用成本时，才采纳它。只有
当 Agent 请求的压缩相对现有自动策略能改善结果，且没有不可接受的信息丢失或成本时，
才单独采纳该能力。负面结果不改变用户侧诊断与自动压缩路径。

## 13. 开放问题

1. `RuntimeStatus` 应提供给所有 subagent，还是只提供给迭代/上下文预算大到足以
   体现价值的 subagent？
2. 最近一次准备请求的快照是否足够，还是模型需要一个单独标注的下一次请求预测？
3. 可选任务详情应列出所有已完成 Todos、仅列数量，还是最近的有界子集？哪项已
   证实的决策需要这些文本？
4. 允许 Agent 请求压缩的最小可总结 token 或可回收比例应是多少？
5. `ContextCompact` 在无人值守 worker 上是否默认 allow，还是因其有损且计费而
   必须由 Agent definition 显式启用？
6. Run 没有 deadline 时，已运行时间能否改善模型行为？或者只应在存在 deadline
   时暴露 deadline/remaining duration？
7. 哪些累计 Session 用量事实能够实质影响 Agent 的 Run 内选择，而不只是满足
   好奇心？
8. 接近限制时，状态结果是否应包含一句建议，还是原始事实更不容易过度引导模型？

## 14. 若获采纳的可能归宿

本提案的 primary domain 是 **Agent Runtime and Models**。

若获采纳：

- 持久权威与安全边界理由进入[上下文持久性](../design/上下文持久性.md)，或在加入
  后会破坏该文档一致性时进入一份聚焦设计记录；
- [Agent Loop](../contribute/architecture/agent-loop.md)记录 live snapshot 与批次后
  控制边界；
- [工具](../contribute/architecture/tools.md)、英文及中文用户手册与权限文档精确描述
  已交付工具；
- 被接受的优先级进入[路线图](../ROADMAP.md)，分解后的工作进入 backlog；以及
- 决策与持久理由移入权威位置后删除本提案。
