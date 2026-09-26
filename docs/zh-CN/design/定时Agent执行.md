# 定时 Agent 执行

> **翻译说明：** 本文是[英文原文](../../design/scheduled-agent-execution.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。

> **受众：** 贡献者与运维人员 · **状态：** 已实现。`Schedule` 领域模型与存储、调度分发器、Space 级 REST API、Portal 界面，以及 `ChannelCron` 占位符的移除，已于 2026-09-11 交付（pull request #548–#553）。彼时 schedule 只能触发 Agent；§15 记录了后续将其推广为"执行器"（Agent 或已发布的 Workflow）的工作。第 13 节记录了有意保留的未决问题。

相关记录：[Agent 执行与 Task 线程](Agent执行与Task线程.md)（§4.5 将 schedule 列为类型化的触发来源）、[Workflow 运行时](Workflow运行时.md)（schedule 是启动 workflow 运行的触发来源之一；§15）、[系统管理](系统管理.md)（创建者被禁用与配额处理）、[服务器协调](服务器协调.md)（多副本认领安全）。

## 目录

- [1. 目的](#1-目的)
- [2. 背景](#2-背景)
- [3. 目标](#3-目标)
- [4. 非目标](#4-非目标)
- [5. 第一性原理下的形态](#5-第一性原理下的形态)
- [6. Schedule 实体](#6-schedule-实体)
- [7. 触发：认领、准入、推进](#7-触发认领准入推进)
- [8. 时间语义](#8-时间语义)
- [9. 授权、配额与失控保护](#9-授权配额与失控保护)
- [10. 界面](#10-界面)
- [11. `ChannelCron` 的处置](#11-channelcron-的处置)
- [12. 决策](#12-决策)
- [13. 未决问题](#13-未决问题)
- [14. 交付](#14-交付)
- [15. 扩展：Workflow 执行器](#15-扩展workflow-执行器)

## 1. 目的

一个 Space 可以让某个 Agent 按周期性的时间表自动运行——"每个工作日 09:00，总结新的 Issue"——无需有人逐次触发。本记录说明在既有 Task 执行平面上交付这一能力所需的最小概念集合，以及为何否决了其他方案。

## 2. 背景

在此之前，系统里不存在任何由时间驱动的东西。没有任何 `xxxRow` 结构或领域类型带有 `next_fire_at`、`cron_expr` 或 `run_at` 字段。`internal/service/conversation/channel` 定义了 `ChannelCron = "cron"`，但背后没有适配器、解析器或计时器：这是一个放错平面的空占位符（§11）。`internal/server/scheduler` 是一个分发轮询器，只把已处于 `PENDING` 的 TaskRun 交给 worker，从不创建运行。

该功能所接入的执行平面已经存在，这正是新界面能保持很小的原因。[Agent 执行与 Task 线程](Agent执行与Task线程.md) §4.5 把 API、webhook 与 schedule 声明为类型化、非对话式的触发来源，它们通过同一个服务创建 Space 所拥有的 Task，并记录类型化的触发来源。本记录填补的就是这个已命名的位置。

## 3. 目标

- Space 成员可以创建一个周期性触发器，用固定输入、在选定时区、按 cron 时间表运行选定的 Agent。
- 每次触发通过既有的 Task 应用服务产生一个普通的 Task 与 TaskRun，并带有类型化的 `schedule` 触发来源，因此状态、取消、配额、轨迹、Artifact 与审计只有一个所有者——与其他所有运行相同。
- 多个 Server 副本下触发是安全的：到期的 schedule 在每个到期时刻恰好触发一次；不会因为两个副本都以为对方会触发而一次都不触发，也不会因为都认领而触发两次。
- Server 跨越到期时刻重启时，既不会静默跳过工作，也不会回放一堆历史触发。
- 一个 schedule 的历史就是它创建的 Task 列表；schedule 行本身保持很小。

## 4. 非目标

- **延续一条长期线程。** 每次触发都是一个全新目标与全新的 Agent 会话（一个新 Task），而不是在不断增长的线程上 Continue。在没有证明需要之前，线程累积只会增加上下文与成本（§13）。
- **本地 CLI 定时。** CLI 是单次运行的进程，没有常驻循环或多副本协调。想按定时运行本地二进制的用户，使用操作系统自带的 cron、launchd 或任务计划程序。桌面应用是另一种情况：它作为常驻 GUI，自带一个小型的进程内调度器，在应用打开期间触发本地任务，与本设计不共享任何状态（见 [`docs/current-state.md`](../../current-state.md)）。本记录所讲的服务端定时能力，放在已经有常驻、协调、多用户进程的地方：Server。
- **事件与 webhook 触发。** 入站事件是另一种类型化来源（`webhook` 作为触发来源已存在）。本记录只涉及时间。
- **亚分钟粒度。** 最小间隔是一分钟；更细的节奏属于流式/事件问题，而不是 schedule。
- **最初不含 Workflow 级定时。** 最初的切片把一个 schedule 绑定到一个 Agent。§15 随后把目标泛化，使 schedule 也可以触发一个 Workflow。

## 5. 第一性原理下的形态

本质结果是"在没有人在场的时刻发生一次运行"。剥离到必须成立的部分：

1. 需要某个持久的东西记住运行*什么*（Space 中的 Agent + 输入）以及*何时*（重复规则 + 时区）。这就是一个新实体：`Schedule`。
2. 需要某个常驻的东西发现到期时刻并准入运行。Server 已经在 `internal/server/scheduler` 里运行常驻轮询循环（分发器、过期运行回收器、保留清理器）。schedule 分发器只是同一形态的又一个循环，而不是新的子系统。
3. 准入已经存在。`task.Service.CreateTask` 校验 Space、Agent、配额与输入，原子地提交 Task 及其首个 TaskRun，并标记 `trigger_source`。一次触发就是用 `trigger_source = schedule` 调用它。

除此之外不需要任何东西。一次触发不需要"schedule run"表：它产生的运行*就是*一个 TaskRun，后者已记录输入、触发来源、状态、用量、输出、轨迹与 Artifact。一个 schedule 的触发记录，就是 `schedule_id` 指向它的那些 Task。再加一种并行的执行记录类型，只会重复 Agent 执行记录已经统一的平面。

```text
Schedule（Space 拥有，时间触发）
        |
        v
  task.Service.CreateTask   <-- Issue、Workflow、API、Portal 调用的同一个服务
        |
        v
   Task + 首个 TaskRun
        |
        v
  Scheduler -> Worker -> 共享 Agent 运行时
```

## 6. Schedule 实体

`Schedule` 由 Space 拥有，与 Space 对所有执行资源都是权威所有者的规则一致。领域模型位于 `internal/core/schedule`（纯领域：无 cron 库、无基础设施）；存储位于 `internal/infra/db`，使用单数表名 `schedule`。

```text
Schedule
  id                      NewPublicID
  space_id                必填，权威所有者
  executor_kind           "agent" 或 "workflow"——该 schedule 触发什么（§15）
  executor_id             必填，执行器；由 executor_kind 决定其含义的不透明句柄
  created_by              必填，操作者；带到每次触发上
  name                    人类可读标签
  input                   每次触发运行的固定输入（Agent 为提示词，Workflow 为运行输入 JSON）
  cron_expr               重复规则（标准五字段）
  timezone                IANA 名称，例如 "Asia/Shanghai"
  enabled                 布尔；暂停的 schedule 保留其行与下次时间
  pause_reason            被禁用的 schedule 为何暂停：manual、creator_disabled、
                          creator_not_member、consecutive_failures 或 invalid_cron；
                          启用时为空
  next_fire_at            分发器据以认领的 UTC 时刻；到期索引
  last_fire_at            最近一次触发的 UTC 时刻（可空）
  last_fire_ref           最近一次触发产生的东西——按 executor_kind，是 Task（agent）或 workflow 运行（workflow）（可空）
  consecutive_failures    约束失控成本（§9）
  created_at / updated_at
```

`next_fire_at` 是分发器查询的唯一事实，也是让触发恰好一次的 compare-and-swap 目标（§7）。schedule 不存储历史触发列表；对 agent schedule 那是一次 Task 查询，对 workflow schedule 那是一次 workflow 运行查询（§15）。core 不解析 cron：调用方用 `github.com/robfig/cron/v3`（仅作解析器）从 `cron_expr` 与 `timezone` 计算 `next_fire_at`；循环本身是 BuildMax 自己的。

持久化 JSON 使用显式的 `snake_case` 标签，表名为单数，id 使用 `NewPublicID`，遵循[实体标识](实体身份.md)。

## 7. 触发：认领、准入、推进

`internal/server/scheduler` 中的 `ScheduleDispatcher` 以较粗的间隔轮询（给定分钟粒度，一分钟足够）。每个 tick，对每个到期的 schedule 执行一次 compare-and-swap 认领，与运行轮询器用 `TransitionTaskRun` 认领运行的方式完全一致：

```text
tick:
  candidates = store.DueSchedules(now)          # enabled 且 next_fire_at <= now
  for s in candidates:
    next = cronNext(s.cron_expr, s.timezone, now)
    claimed = store.ClaimSchedule(s.id,
                expectedNextFireAt = s.next_fire_at,
                newNextFireAt      = next)       # 条件 UPDATE
    if not claimed: continue                     # 另一副本已认领
    task.Service.CreateTask(CreateTaskCmd{
        SpaceID: s.space_id, AgentID: s.agent_id,
        CreatedBy: s.created_by, Input: s.input,
        TriggerSource: RunTriggerSourceSchedule,
        ScheduleID: s.id,
    })
    store.RecordFire(s.id, task.id, outcome)       # last_fire_at, last_task_id
```

条件更新——只有当 `next_fire_at` 仍等于读取到的值时才推进它——让触发在多副本间恰好一次，复用了运行调度器所依赖的乐观并发模式。认领在准入*之前*推进时钟，因此 `CreateTask` 失败不会让 schedule 永远卡在同一个到期时刻；它被记录为一次失败的触发，schedule 继续走向下一个时刻。§9 约束每次都失败的 schedule。

`RunTriggerSourceSchedule` 加入 `internal/core/task/task.go` 中的 `RunTriggerSource*` 常量。`Task` 带有可选的 `schedule_id` 来源关系，与它已有的可选 `issue_id` 和 `workflow_step_run_id` 完全一样——只是来源，绝不是授权父级。

## 8. 时间语义

- **时区。** cron 在 schedule 的 IANA 时区中求值，因此"09:00"能跨越夏令时。存储与所有比较都使用 UTC。Server 二进制内嵌时区数据库，最小化容器镜像也能解析 IANA 名称。
- **错过的触发会合并。** 如果 Server 在一个或多个到期时刻期间停机，下一个 `cronNext` 从 `now` 计算，而不是从过期的 `next_fire_at`。一个本应在 09:00 与 10:00 触发、而停机在 10:30 结束的 schedule，会立即触发一次，然后恢复到下一个常规时刻。没有回填风暴，也没有静默跳过一整天。
- **每个到期时刻恰好一次**在健康运行下由 compare-and-swap 认领（§7）保证。

## 9. 授权、配额与失控保护

- **授权。** 创建、编辑、启用、禁用与删除 schedule 通过 `schedule.space_id` 与普通 Space 成员资格授权——与 Task 使用同一规则。触发产生的 Task 的 `created_by` 是 schedule 的创建者，因此配额、审计与运行令牌都归属到真实的操作者，与 Issue 发起的运行一致。
- **创建者被禁用。** 运行调度器会取消创建者已被禁用或已被移出 Space 的待执行运行，并记录 `task_run.cancel_reason`（`creator_disabled` 或 `creator_not_member`）。对*重复*触发器而言，那会每次触发都铸造一个被取消的 Task，因此创建者不再具备资格时的触发会改为暂停 schedule（`enabled = false`，`pause_reason` 为 `creator_disabled` 或 `creator_not_member`）；重新启用是显式操作。这复用了系统管理中的禁用账号概念，而不是发明 schedule 专属的授权。
- **配额。** 每次触发都经过 `task.Service` 中既有的配额检查。被配额拒绝的触发是一次失败的触发，本身不是让 schedule 停止的错误。
- **失控保护。** 一个每次触发都失败的 schedule 会无限消耗配额。连续五次触发失败（`maxConsecutiveScheduleFailures`）后，分发器暂停该 schedule 并记录原因。这是唯一一项被纳入而非推迟的保护，因为"一个永远失败的无人值守触发器"是具体的成本故障，不是假设。暂停原因存储为 `pause_reason = consecutive_failures`；Portal 尚未展示它（§13）。

## 10. 界面

- **API。** Space 级，与 Task 路由平级并以同样方式注册（各 handler 子包的 `Register`，在 `routes.go` 中组合，与 `openapi.json` 完全匹配）：

  ```text
  POST   /api/spaces/{space_id}/schedules                 { executor_kind, executor_id, name, input, cron_expr, timezone }
  GET    /api/spaces/{space_id}/schedules
  GET    /api/spaces/{space_id}/schedules/{id}
  PATCH  /api/spaces/{space_id}/schedules/{id}            { enabled?, input?, cron_expr?, timezone?, name? }
  DELETE /api/spaces/{space_id}/schedules/{id}
  GET    /api/spaces/{space_id}/schedules/{id}/tasks      # agent schedule 触发的 Task
  GET    /api/spaces/{space_id}/schedules/{id}/runs       # workflow schedule 触发的运行（§15）
  ```

  `cron_expr` 与 `timezone` 在写入时校验；无效表达式是 `KindInvalid` 拒绝，绝不会成为一条在触发时静默失败的行。

- **Portal。** schedule 在其执行器自己的详情页创建与管理——agent schedule 在 Agent 详情页，workflow schedule 在 Workflow 详情页（§15）：该区块列出这个执行器的 schedule，显示下次与上次触发、启用状态、Enable/Disable、Delete，以及每个 schedule 产生的 Task 或运行，并有一个添加表单。侧边栏的 Space 级 **Schedules** 页面展示跨所有 Agent 与 Workflow 的每个 schedule，让 owner 看到有哪些无人值守的自动化在运行并可暂停，也可在此创建——同一个表单，外加一个"运行什么"的选择器。

- **删除 schedule** 只移除触发器。它已创建的 Task 是独立的执行历史，不受影响——它们不是 schedule 的子对象。

## 11. `ChannelCron` 的处置

`ChannelCron` 曾位于 *Conversation* 渠道枚举中。那是错误的平面：[Agent 执行与 Task 线程](Agent执行与Task线程.md) §13.4 已移除 Issue 与 Workflow 运行曾经创建的合成 Conversation，而定时运行同样直接创建 Task，绝不创建 Conversation。定时运行的类型化来源是 TaskRun 的 `trigger_source`，而不是对话传输方式。

因此 `ChannelCron` 从 `internal/service/conversation/channel` 与 `ValidChannels()` 中移除，取而代之的是 Task 平面上的 `RunTriggerSourceSchedule`。这是 Alpha 的"连贯地修正错误形态、不加兼容层"规则：占位符被移到它所属的平面，而不是在原地被接线。

## 12. 决策

| 决策 | 选择 | 被否决的替代方案及原因 |
|---|---|---|
| 每次触发的执行记录 | 复用 Task + TaskRun | 专用 `schedule_run` 表会重复统一的执行平面；运行已记录一切。 |
| 线程模型 | 每次触发新建 Task | 延续线程会在没有证明需要的情况下增加上下文与成本；作为一种模式推迟。 |
| 输入 | 固定字符串 | 模板化增加一份契约，而首个切片没有证明需要。 |
| 触发循环的归属 | `internal/server/scheduler` 中的新循环 | 独立服务/二进制为一个轮询循环增加进程与协调面。 |
| 恰好一次 | 对 `next_fire_at` 做 compare-and-swap | 领导者锁或外部调度器（Temporal、cron sidecar）引入 Workflow 记录明确拒绝作为默认的依赖。 |
| 失控响应 | 连续五次失败后暂停 | 限流仍在花钱；静默跳过会隐藏故障。 |
| 权限 | 普通 Space 成员资格 | 单独的运维能力会为同一 Space 所有资源引入第二套授权规则。 |
| 占位符 | 移除 `ChannelCron`，新增 `RunTriggerSourceSchedule` | 实现 cron Conversation 适配器会把错误的平面固化。 |
| 本地 CLI 定时 | 范围之外；使用操作系统 cron | 常驻 CLI 守护进程重复了 Server 已有的常驻协调循环。 |
| 错过的触发 | 合并为一次补触发 | 回填每个错过的时刻可能引发风暴；静默跳过会丢失信号。 |
| cron 解析 | `robfig/cron/v3`，仅作解析器 | 对标准五字段而言，在包内自写解析器没有收益；循环仍是 BuildMax 自己的。 |

## 13. 未决问题

- **暂停原因的展示。** 每次暂停都会把原因存入 `pause_reason`（§6），API 也会返回它。在 Portal 的暂停状态旁展示它，是更晚的 Portal 侧切片。
- **长时间停机抑制。** 长时间停机是否应抑制那一次补触发（最大陈旧度上限），而非总是触发一次。在有证据表明陈旧运行造成危害之前，默认仍是一次补触发（§8）。
- **延续线程模式。** 固定目标切片有了使用量之后，是否值得增加"每次触发延续同一个 Task"的模式，以及如何约束其上下文增长。在此记录是为了让默认值成为有意的选择，而不是遗漏。

## 14. 交付

五个切片按顺序落地，各自独立测试：

1. **core + 存储**（#548）：`internal/core/schedule`、`schedule` 表与带 `DueSchedules`、`ClaimSchedule`、`RecordFire` 的存储、MySQL 范围的认领竞争测试、`RunTriggerSourceSchedule` 与 `Task.schedule_id`。
2. **分发器**（#549）：由假时钟驱动的循环，含 cron 解析、合并补触发、创建者禁用暂停与连续失败暂停。
3. **API + OpenAPI**（#550）：Space 级路由及其授权矩阵覆盖。
4. **占位符移除**（#551）：`ChannelCron` 连同其测试与文档一并移除。
5. **Portal**（#552、#553）：Agent 详情区块与 Space 级 Schedules 页面，附浏览器覆盖。时区数据库内嵌进 Server 二进制（#554）。

## 15. 扩展：Workflow 执行器

最初的切片把 schedule 绑定到单个 Agent。很自然的下一个问题——Space 希望像运行 Agent 一样"每个工作日 09:00 运行分诊*工作流*"——的答案是推广 schedule 触发的对象，而不是新增一个并行实体。schedule 的目标是*一个执行器*，正是 [Issue](Portal工作与执行体验.md) 已经用 `executor_kind`（`agent` 或 `workflow`）与不透明 `executor_id` 建模的那个概念。`Schedule` 复用了这套词汇：`agent_id` 变为 `executor_kind` + `executor_id`，`last_task_id` 变为 `last_fire_ref`（Agent 触发产生的 Task，或 Workflow 触发产生的 workflow 运行），于是整套 cron、恰好一次认领、补触发、资格校验与连续失败机制都被共享而非重复。Alpha 阶段没有兼容负担；迁移 `schedule_agent_to_executor` 回填已有行并删除旧列。

触发只在必须之处按 `executor_kind` 分支：分发器的 `startExecutor`。agent schedule 一如既往地通过 Task 服务准入一个 Task；workflow schedule 通过 `workflow.Service.StartWorkflowRun` 启动一次运行，并带上该 schedule，使运行记录其触发来源。两者都归属到 schedule 的创建者并按其工作计量，且都把各自的失败纳入同一套连续失败暂停。无法启动其执行器的触发——包括 schedule 创建后又被取消发布或归档的 workflow（`StartWorkflowRun` 会拒绝它）——会被记为一次失败触发，并在达到上限后暂停 schedule，与 agent 准入失败完全一致。

两条规则让 workflow schedule 保持诚实。其一，workflow 必须**已发布**才能被定时：服务在创建时校验这一点（草稿或已归档的 workflow 永远无法启动运行），Portal 也只提供已发布的 workflow。其二，workflow 的 `input` 是其 `input_schema` 声明的运行输入，冻结在 schedule 上，并在每次触发时由 `StartWorkflowRun` 校验，与手动运行的输入一样；Portal 生成与手动"运行"对话框相同的输入表单。workflow 运行记录启动它的 `schedule_id`，与 `Task.schedule_id` 对应，因此 schedule 可以列出其触发历史（`GET /api/spaces/{space_id}/schedules/{schedule_id}/runs`），正如 agent schedule 列出其触发的 Task。

界面与 agent 情形对称：workflow 详情页上的 Schedules 区块（固定为该 workflow），以及 Space 级 Schedules 页面——现在为两种类型列出并创建 schedule，并按执行器类型标注每一条。管理 schedule 仍是成员级（`manage_schedules`），不受编写 workflow 所需的 owner/admin 能力限制。
