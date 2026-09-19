# Agent 与 Workflow

Agent 和 Workflow 是你分派工作的可复用构建块。Agent 是关于*一个 Agent
应如何行事*的已保存定义；Workflow 是一个按顺序运行一个或多个 Agent 的有序计划。
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

在侧栏打开 **Workflows** 并选择 **New Workflow**。Workflow 是一个可复用的分步
执行计划，你可以手动运行它或将它分派给一个 Issue。用**步骤**来构建它：

- 用 **Add Agent Step** 添加一个步骤。每个步骤都是一个 Agent 步骤——这是目前运行时
  唯一支持执行的类型——所以没有别的可选；每个步骤只需指向一个 **agent**，
  并携带一个描述该步骤应做什么的 **prompt**。
- 步骤按顺序运行；该计划目前是一个线性序列。
- 步骤的 id 是自动生成的，不需要手动填写。编辑器把它作为次要技术信息展示，
  方便把某次运行追溯到产生它的步骤。
- **Advanced: edit raw JSON** 以 JSON 形式展示同一份定义，用于精确检查，
  或者做一些步骤表单暂时还表达不了的改动。它不是与表单并列存在的第二种
  构建方式——Save 会用同一套规则检查两条路径，所以无论走哪条路径都不可能
  留下一个运行时无法执行的步骤。

### 草稿、发布、归档

一个 Workflow 有一个状态：

- **Draft** —— 仍在编辑中。
- **Published** —— 可供使用。一个 Workflow 必须先发布，你才能手动运行它
  或把它分派给一个 Issue。
- **Archived** —— 已停用。

从 Workflow 的详情视图设置状态。
草稿以 **Publish** 为主要操作，**Save** 则保留草稿继续编辑。已发布的 Workflow
以 **Run Workflow** 为主要操作；**Edit** 打开定义表单，**Save** 写入新修订。
**History** 可以查看旧版本。编辑器中的删除步骤与删除输入使用危险操作样式。

### 运行一个 Workflow

Workflow 一经发布，就可用 **Run Workflow** 运行它。你会被带到运行的详情视图，
每个步骤在执行时都会显示自己的状态。你也可以把 Workflow 分派给一个 Issue，
使其作为该 Issue 的工作来运行——见
[对话与 Issue](对话与Issue.md)。

和 Agent 一样，Workflow 也保留一份带编号的历史，一次运行会记录它所展开的
Workflow 版本，从而使过去运行的记录保持准确。

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

侧边栏中的 **Schedules** 入口展示该 Space 中所有 Agent 的全部 schedule，
让你看到设置了哪些无人值守的工作，并可暂停其中任何一个。

## 来自 Marketplace 的插件

顶部栏中的 **Marketplace** 图标列出此部署发布的插件——技能、子 Agent、
MCP 服务器和钩子。它是一个浏览界面：安装发生在 Agent 实际运行的地方，
因此目录交给你的是安装命令，而不是一个按钮。见 [插件](插件.md)。

## 下一步

- 把这些分派到实际工作中：[对话与 Issue](对话与Issue.md)。
- 调整一个 Agent 能运行什么：[沙箱](沙箱.md) 和 [工具权限](工具权限.md)。
