# 对话与 Issue

对话是你在 Portal 中与 BuildMax 交流的方式；Issue 是工作被跟踪并交给
Agent 的方式。本页会介绍两者。

## 开始一段对话

**Chat** 是入口。在编辑框中输入你想完成的工作——例如
*"Help me analyze last month's sales data"*——然后发送（Enter 发送，
Shift+Enter 换行）。一段对话可以直接回答你，或者，当工作规模更大时，
启动后台工作并在结果就绪时展示给你。

最近的对话会列在 Chat 上，方便你重新接续。来自 Portal 之外的对话会标出来源，
例如[聊天应用](聊天应用.md)中的对话标记为 **Telegram**。助手在当前 Space 中工作，
也能列出你的所有 Space；要在别的 Space 工作，请在侧边栏切换。
**Recent Conversations** 与 **Files** 标签页分别展示对话列表和 Space 工作文件的入口；
键盘焦点位于标签时，可用左右方向键切换。对话启动的后台 Task 会在对话中显示状态与输出；
运行时可用 **Stop**，结束后可用 **Run again**，**Run details** 用于查看轨迹。
操作失败时，错误会留在对应卡片上；Task 列表无法加载时，Chat 会显示提示与
**Retry tasks**，但仍保留对话内容。

直接运行 Agent 时，Task 页面按轮次展示输入与输出。**Continue** 发送新指令；
**Retry last run** 重复上一轮。**Details** 收纳来源、时间、ID 和轨迹；
页头使用 **Done** 等可读状态词。

## 创建一个 Issue

**Issue** 是面向用户的工作单元——就是你真正想完成的事情。
在侧栏打开 **Issues** 并选择 **New Issue**。一个 Issue 有：

- **Title** —— 对该工作的简短陈述。
- **Description** —— Agent 处理它所需的细节。
- **Business Status** —— `todo`、`in progress` 或 `done`。由你自己设置；
  它不会因一次运行而自动改变。
- **Owner** —— 对该 Issue 负责的人（见下文）。
- **Executor** —— 被选定来完成这项工作的 Agent 或 Workflow（见下文）。

Issue 可以嵌套：你可以从一个 Issue 添加**子 Issue**来分解工作。
子 Issue 的状态独立跟踪——在子 Issue 仍未关闭时关闭父 Issue 是允许的，
并且绝不会把它们的状态向上汇总。

如果 Issue 已创建，但初始状态、Owner 或 Executor 保存失败，弹窗会明确说明该 Issue
已经存在，并提供 **Open created issue** 继续设置；此时不会再次提供创建按钮。

你可以在 Issue 的评论中讨论它，人和 Agent 都会在那里留下笔记。

## List 与 Board

**Issues** 默认以 **List** 显示顶层 Issue。选择 **Board** 可以在三列中查看同一批 Issue——
**To do**、**In progress** 和 **Done**——每列都有自己的总数，放不下时提供 **Show more** 按钮。
父 Issue 的卡片会显示其子 Issue 已完成多少；子 Issue 本身仍在父 Issue 的详情页中查看。

两种视图都可以按 **Owner**（包括 **Me**）或 **Executor** 过滤。视图和过滤条件是页面地址的
一部分，因此刷新或分享链接会打开相同的视图。

要在看板上改变 Issue 的状态，请使用卡片上的 **Move to**。它与编辑 Issue 时改变状态完全相同：
永远不会启动运行，也不会改变 Owner 或 Executor。如果在看板加载之后有人修改了该 Issue，
这次移动会被拒绝，看板会重新加载，让你重新决定。某一列加载失败时会明确说明并提供 **Retry**——
空列始终表示该列确实没有匹配的 Issue。

## Owner、Executor 与运行工作

Owner 与 Executor 是两个相互独立的选择，可以同时都设置、只设置一个，或都不设置：

- **Owner** —— 负责该 Issue 的人，包括 *Me*。设置 Owner 绝不会启动一次运行，
  它只记录谁对此负责。
- **Executor** —— 实际执行工作的对象，为以下之一：
  - **Unassigned** —— 尚未选定。
  - **An agent** —— 一个已保存的 [Agent](Agent与工作流.md) 可以在后台运行该 Issue。
  - **A workflow** —— 一个已发布的 [Workflow](Agent与工作流.md) 可以为该 Issue 运行其步骤。

选择 **Edit issue** 修改字段，再点 **Save changes**。保存只会记录你选择的字段，绝不会启动一次运行，
也不会消耗你 space 的执行配额——在把 Issue 准备好之前，你可以随意更改两者。

一旦 Executor 被保存为某个 Agent 或 Workflow，阅读视图就会显示
**Run workflow** 或 **Run agent** 按钮。只有这个按钮才会在 worker 上安排一次
后台运行：它会物化 space 的文件、运行 Agent、写入任何输出，并汇报结果——
而不会占用你的浏览器。成功发起的 Run 会直接把你带到它启动的那次运行。

## Issue 详情

打开一个 Issue 查看它的详情视图，其中分为四个标签页：

标题、状态、Owner、Executor 和最近结果先于标签页与编辑表单显示。需要修改时选择
**Edit issue**。Run 位于阅读视图，未保存的 Executor 更改不会启动错误的工作。

- **Overview** —— Owner 与 Executor、状态、描述、子 Issue，以及最近一次运行的摘要。
- **Discussion** —— 评论线程，人和 Agent 都会在那里留下笔记。
- **Results** —— 最新结果，以及一次运行产出的所有已保存
  [Artifact](Portal概览.md)。较大的输出会作为 Artifact 存储，你可以打开或下载。
- **Runs** —— 该 Issue 的完整执行历史。

在 Overview 或 Runs 标签页中，一次正在进行的运行会提供：

- **Stop Run** —— 当一次运行处于 pending 或 running 状态时，你可以停止它。
  尚无人接管的运行会立即结束；正在被某个 worker 执行的运行会被请求停止，
  并以 *canceled* 结束，通常在几秒内。无论哪种方式，它都会保留已经产出的内容。
- **Retry Run** —— 一次运行结束后，你可以用相同的指令重复它，
  这样你就能从死掉的 worker 或超时的模型中恢复，而无需重新输入任何内容。
  一次重试会计入你 space 的配额，并保留原始运行的记录不变。
  作为 Workflow 步骤的运行是通过重新运行其 Workflow 来重试的，而不是从这里。

## 在本机处理 Issue

你负责的 Issue 也可以在本地处理，那里有你的文件和工具。

- **CLI** —— `buildmax issue list` 显示你未完成的 Issue，`buildmax issue start <id>`
  打开一个由 Agent 限定到该 Issue 的会话；见 [`buildmax issue` 命令](命令行.md#buildmax-issue)。
- **Desktop** —— 登录服务器后，侧边栏会显示 **Issues**：跨 Space 列出你未完成的 Issue，以及每个
  Issue 的描述、子 Issue 和讨论。**Start chat** 会在你选择的 Project 中新开聊天，并把 Issue 预先
  放进输入框，你可以在发送前修改。在同一视图中还可以改变 Issue 的状态并发表评论。Desktop 未登录时
  不会出现 Issues 入口。

无论哪种方式，交回工作都由你决定：评论说明做了什么，状态变更说明是否完成。规划和指派工作仍在
Portal 中进行。

## 下一步

- 定义你在此处分派的 Agent 和计划：[Agent 与 Workflow](Agent与工作流.md)。
- 熟悉应用的其余部分：[Portal 概览](Portal概览.md)。
