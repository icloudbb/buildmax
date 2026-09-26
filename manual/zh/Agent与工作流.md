# Agent 与 Workflow

Agent 和 Workflow 是你分派工作的可复用构建块。Agent 是关于*一个 Agent
应如何行事*的已保存定义；Workflow 是由依赖关系连接 Agent 步骤的图。
两者都存在于当前 space 中，并保留一份带编号的历史。

## 创建一个 Agent

在侧栏打开 **Agents** 并创建一个新 Agent。一个 Agent 定义有：

- **Name** 和 **description** —— 它在你分派时的显示方式。
- **Instructions** —— 告诉 Agent 如何工作的常驻提示词。这是定义的核心。
  （在一次后台运行中，来自 **Space → Overview** 的 space 共享 Agent instructions
  会先发送，然后才是这些。）
- **Model** —— 它在部署的哪个模型上运行。
- **Plugins** —— Agent 可以使用的可选 [插件](插件.md)。
- **Sandbox tiers** —— 其 `Bash` 工具的文件系统和网络约束。
  见 [沙箱](沙箱.md)。

保存定义使其可被分派。你在 Issue 的 Overview 标签页中把一个 Agent 设置为
该 Issue 的 **Executor**——见 [对话与 Issue](对话与Issue.md)。
在 Agent 页面，**Run agent** 会启动一个 Task。**Configuration** 标签页提供
**Save changes** 和 **Delete agent**；标签页也支持左右方向键。窄屏时可横向滚动
标签栏以进入后面的区块。

### 版本

每次你保存一个 Agent，BuildMax 都会记录一个新的带编号版本以及编写者是谁。
你可以 **Restore** 一个较早的版本，这会记录一个*新*版本，
而不是抹掉此后的那些——因此历史保持完整。一次 Workflow 运行会记下
每个步骤所运行的确切 Agent 版本，因此即使定义后来发生变化，
过去的运行仍然可读。

删除一个 Agent 会将它从 space 中移除，但保留其背后的记录，
因此已经引用它的运行和历史仍然可读。一个仍被某个已发布 Workflow 使用的
Agent，在该 Workflow 被修改或归档之前无法删除。

## 创建一个 Workflow

在侧栏打开 **Workflows** 并选择 **New Workflow**。Workflow 的详情页按标签页
组织，与 Agent 页面风格一致：**Overview**（只读地查看计划）、**Definition**
（编辑器与生命周期操作）、**Runs**、**Schedules** 和 **Revisions**。草稿默认
打开 Definition 标签页；已发布的 Workflow 默认打开 Overview。**Run Workflow**
位于页头，在 Workflow 发布后可用。

Workflow 是一个可复用的执行计划，你可以手动运行它或将它分派给一个 Issue。
在 **Definition** 标签页使用可视化图编辑器构建它：

- 用 **Add step** 创建 Agent 步骤。选择节点后，在右侧面板编辑它的 id、目标 Agent、
  prompt、Issue 访问模式和输入绑定。`agent_task` 是目前运行时唯一支持的节点类型。
- 从一个节点的右边缘拖到另一个节点的左边缘，让后者依赖前者。一个节点会在所有
  依赖节点成功后运行；互不依赖的节点可以并行运行。**Max parallel** 设置每次运行
  的并行上限。**Re-layout** 只重新排布图，不改变执行规则。
- **Edit raw JSON** 展示同一份定义，供精确检查或编辑可视化界面尚未提供的字段，
  包括 `input_schema`、`result` 和节点的 `output_schema`。**Visual editor** 返回
  图视图。保存时两种视图使用同一套校验规则。

### 草稿、发布、归档

一个 Workflow 有一个状态：

- **Draft** —— 仍在编辑中。
- **Published** —— 可供使用。一个 Workflow 必须先发布，你才能手动运行它
  或把它分派给一个 Issue。
- **Archived** —— 已停用。

在 **Definition** 标签页设置状态，其操作各自命名所到达的状态：**Publish**
（主要操作）使 Workflow 可运行，**Save as draft** 保留改动但不发布，**Archive**
将其停用，**Discard changes** 丢弃未保存的改动。编辑一个已发布的 Workflow 并
保存会写入一个新修订。**Revisions** 标签页保存旧版本。编辑器中的删除步骤与删除
输入使用危险操作样式。

### 运行一个 Workflow

Workflow 一经发布，就可用 **Run Workflow** 运行它。你会被带到运行的详情视图，
每个步骤在执行时都会显示自己的状态。你也可以把 Workflow 分派给一个 Issue，
使其作为该 Issue 的工作来运行——见
[对话与 Issue](对话与Issue.md)。

和 Agent 一样，Workflow 也保留一份带编号的历史，一次运行会记录它所展开的
Workflow 版本，从而使过去运行的记录保持准确。

某个步骤失败后，运行会显示 **Stopping after failure**，并要求其他执行中的
步骤停止。取消某个步骤的 Task 后，Workflow 同样会进入 **Canceling**。
尚未启动的步骤会被阻止。所有已创建的 TaskRun 都结束后，运行才会变成
**Failed** 或 **Canceled**；已有输出仍可在步骤和 Task 中查看。详情页在
等待期间会继续刷新。Server 重启后会继续收尾；失联 worker 由现有的
TaskRun 恢复机制处理。

## 定时运行一个 Agent

Agent 可以按时间表运行，无需有人点击 Run。打开 Agent 的详情视图，使用
**Schedules** 区块：给 schedule 起个名字，填写每次要交给 Agent 的输入、
一个五字段的 cron 表达式（例如 `0 9 * * 1-5`），以及解读该表达式所用的
IANA 时区（例如 `Asia/Shanghai`）。

每次触发都会为该 Agent 创建一个普通的 Task，因此它会出现在 Task 列表中，
拥有自己的状态、轨迹与 Artifact。该区块列出每个 schedule 的下次与上次触发
时间和它创建的 Task，并允许你禁用、重新启用或删除它。删除 schedule 会保留
它已经创建的 Task。
如果已触发 Task 列表加载失败，schedule 卡片会显示错误并提供
**Retry triggered tasks**。

连续五次触发都未能启动 Task，或创建者的账号被禁用时，schedule 会自行暂停；
排除原因后重新启用即可。如果服务器在某个触发时刻处于停机状态，恢复后它会
触发一次，然后回到常规时间表，而不会回放每一个错过的时刻。

## 定时运行一个 Workflow

已发布的 Workflow 也能以同样的方式定时运行。打开该 Workflow 的详情页，
切换到它的 **Schedules** 标签页：Workflow 已经选定，你只需给 schedule 起名、
填写其输入表单要求的运行输入（与手动"运行"对话框相同的表单；没有输入的
Workflow 则无需填写）、cron 表达式与时区。每次触发启动一次 workflow 运行，
在 **Show triggered runs** 下列出并显示状态，运行可像其他运行一样打开。
只有已发布的 Workflow 才能被定时——先发布草稿。暂停、连续失败处理与错过
触发的行为，与 Agent schedule 完全一致。

侧边栏中的 **Schedules** 入口展示该 Space 中所有 Agent 与 Workflow 的全部
schedule，让你看到设置了哪些无人值守的工作，并可暂停其中任何一个；你也可以
在此创建一个并选择它运行什么。**Pause all** 与 **Resume all** 可一键翻转该
Space 中的全部 schedule，无需逐行操作即可停止或重启所有无人值守的工作。

## 来自 Marketplace 的插件

顶部栏中的 **Marketplace** 图标列出此部署发布的插件——技能、子 Agent、
MCP 服务器和钩子。它是一个浏览界面：安装发生在 Agent 实际运行的地方，
因此目录交给你的是安装命令，而不是一个按钮。见 [插件](插件.md)。

## 下一步

- 把这些分派到实际工作中：[对话与 Issue](对话与Issue.md)。
- 调整一个 Agent 能运行什么：[沙箱](沙箱.md) 和 [工具权限](工具权限.md)。
