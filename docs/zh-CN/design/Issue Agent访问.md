# Issue Agent 访问：Agent 对自己工单能说什么

> **English:** [Read the English original](../../design/issue-agent-access.md)

## 目录

- [状态](#状态)
- [1. 决定](#1-决定)
- [2. Agent 永远不能声明的内容](#2-agent-永远不能声明的内容)
- [3. 本地 Agent 的报告是一种主张，并如实标注](#3-本地-agent-的报告是一种主张并如实标注)
- [4. 不可信输入始终是数据](#4-不可信输入始终是数据)
- [5. 范围之外](#5-范围之外)
- [6. 未决问题](#6-未决问题)

## 状态

- roadmap_priority：`unscheduled` —— 它决定的是已实现的 Issue 模型有意留待单独
  处理的 Agent 变更问题；未列入 [../../ROADMAP.md](../../ROADMAP.md)
- status：`产品边界生效；机制已被取代` —— 下面的规则在两个平面上都成立（从某个
  Issue 启动的 worker 运行，以及用 `buildmax issue start` 启动的本地会话）。但
  *机制* 不再是两个进程内工具：Agent 通过 `Bash` 运行 `buildmax issue` 来读取并
  报告它的 Issue，而每个 Issue 的护栏落在服务端路由上。本记录如今只拥有产品
  边界 —— Agent 对空间所属工作能够断言什么 —— 这与传输方式无关。
- superseded_by（机制）：[agent-bridge-cli.md](./Agent 桥接 CLI.md) —— 它用
  `buildmax` 命令面取代了 `GetIssue` / `ReportToIssue` 工具，把范围移到运行令牌、
  把评论预算与正文上限移到 `issue.Service.CreateComment`。它的 §8 在命令面世界里
  重述了下面的边界。
- relates：[tool-permissions.md](./工具权限.md)、
  [unified-artifacts.md](./统一工件.md)、
  [surface-positioning.md](./界面定位.md)、
  [portal-execution-model.md](./Portal执行模型.md)
- precedes：[../proposals/local-issue-work-bridge.md](../proposals/local-issue-work-bridge.md)，
  其仍然未决的问题（持久的 Issue↔会话链接、工作区映射、本地结果记录类型）本记录
  不作决定
- touches：`internal/service/issue`、`internal/server/handlers/work`、
  `internal/server/handlers/worker`
- created_at：`2026-08-29`

## 1. 决定

工作某个 Issue 的 Agent 对它恰好能做两件事：**读取** 它被启动去处理的那个有界
Issue —— 描述、子 Issue 与最近的讨论 —— 以及在该 Issue 的线程上 **发布一条有界
报告**。关于该 Issue 的其他一切都不是 Agent 可写的。

它如何触达该 Issue 是 [agent-bridge-cli.md](./Agent 桥接 CLI.md) 的事：
`buildmax issue show` / `buildmax issue comment`，在 worker 中由运行令牌、在本地
由用户登录把范围限定到该运行的唯一 Issue。本记录只决定 *边界* —— Agent 能断言
什么 —— 无论传输方式如何，它都成立。

三条规则构成这条边界：

1. **状态、负责人、执行者与层级永远不是 Agent 可写的。** §2。
2. **本地 Agent 的报告以主张形式存储**，标为 `local_agent`，而非 `agent`。§3。
3. **Issue 文本是数据，绝不是提示层。** §4。

## 2. Agent 永远不能声明的内容

`status`、`owner_id`、`executor_kind`、`executor_id` 与 `parent_issue_id` 不能被
任何 Agent 路径写入。创建子 Issue 也不行。任何命令在任何上下文都不把这些暴露为
Agent 可用的写操作。

这保留了产品早已持有的不变式 —— 代码库中没有任何东西会自行移动 Issue 的状态 ——
而不是发明一个新的。理由在于代价的不对称：`done` 是空间用来规划的依据，其含义是
*有人接受了这项工作*。如果模型能写它，这个词就不再承载那层含义，损失是一次空间
协调失败。而让模型能写它所节省的，不过是本来就要看结果的那个人的一次点击。

一个认为工作已完成的 Agent 在它的报告里如此说明。由人来移动 Issue。

## 3. 本地 Agent 的报告是一种主张，并如实标注

worker 运行的报告以 `agent` 存储：写下它的运行令牌就是 Agent 自己的凭证，它所指
的任务和运行都是部署持有的记录。本地会话没有这些。它持有的是 *某个人* 的会话，
运行在部署没有调度、没有为之核算配额、也没有为之记录任何追踪的机器上。

把两者都存为 `agent` 会让 Portal 的读者以为部署为它从未见过的东西背书。因此本地
报告存为 `local_agent`，作者是转达它的那个人 —— 服务器验证过的唯一身份，也是可
问责的那一个。它不指向任何任务与运行，因为根本没有。Portal 将其显示为“转达”，
而非“声明”。

空间评论路由只接受 `author_kind` 为缺省或 `local_agent`。一个人的会话不能写
`agent` 或 `system`：那些是部署自己的声音，由运行令牌和服务器写下。

这并不使本地报告成为证据。它使这条主张作为主张而可读，这是客户端报告所能诚实
达到的极限。

## 4. 不可信输入始终是数据

Issue 的描述与评论是第三方文本。空间里的任何人都能写，未来的入站连接器也可能把它
们从外部跟踪器带进来。它们与 `WebFetch` 的输出属于同一信任类别。

两个后果，都是硬约束：

- **它们作为 Agent 读取的数据到达，绝不作为系统提示层。** 这同时是安全规则和缓存
  规则：`AGENTS.md` 用一组对一次运行稳定的有界指令层固定系统提示，好让它们成为可
  缓存前缀。层里放一个可变的 Issue 快照，会在每次编辑时打破该前缀，也会把评论洗成
  指令。命令输出落在 stdout 上，Agent 把它作为 `Bash` 结果读取 —— 本就是数据。
- **渲染出的线程为每条评论标注作者类型。** 一个分不清同伴评论与自己委托人指令的
  模型，就没有区别对待它们的依据。

启动 worker 运行时仍会把 Issue 扁平化进该运行的初始消息。那是输入，并保持为输入；
这里的任何东西都不把它移进层。

## 5. 范围之外

- 评论之外的任何 Issue 变更 —— 状态、指派、改父、创建子项、删除或归档。
- 寻址被限定之外的 Issue，包括同级或被限定子项的父级。传输层强制此点（运行令牌只
  命名一个 Issue；本地命令是用户自己的权限）；无论如何本记录都把它作为边界禁止。
- 命令面、传输与每个 Issue 的护栏放置，均由
  [agent-bridge-cli.md](./Agent 桥接 CLI.md) 拥有。
- 持久的 Issue↔会话链接与离线发件箱，由
  [../proposals/local-issue-work-bridge.md](../proposals/local-issue-work-bridge.md) 拥有。

## 6. 未决问题

1. **无运行会话的结果出现在哪里？** 本地会话不产生运行，所以它发布的制品没有可挂靠
   的任务运行行。要么产出聚合学会识别一个源自会话的来源，要么由桥为本地工作创建
   记录。这归桥提案回答。
2. **Portal Tier 1 会话是否获得 Issue 访问，以何种范围？** 会话是对用户的单一声音，
   却不被限定到某一个 Issue，因此上面的边界会需要另一套范围叙事。
3. **被限定的子 Issue 是否需要看到它的父级？** 向上读取比“摆在面前的工单”范围更宽，
   而真正的需求往往就写在父级的描述里。
4. **`local_agent` 是否需要一个自己的会话才能成为证据？** §3 决定的是本地报告如何
   记录，而非它能被信任到什么程度；使它成为证据是持久 Agent 会话的问题，不是本记录
   的问题。
