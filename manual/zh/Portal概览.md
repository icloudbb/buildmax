# Portal 概览

Portal 是 BuildMax 的 Web 应用。它为团队提供一个共享的场所来启动工作、
在后台运行 Agent 并收集结果——全部由命令行所使用的同一套 Agent 运行时支撑。
本页帮助你了解界面；后面两页会介绍日常任务。

## 登录

打开你的部署提供的 Portal URL 并登录。BuildMax 签发的是一次性登录码，
而不是永久密码；是否允许新用户注册由运维人员的配置决定。如果你无法登录，
那是运维人员的问题，不是你在浏览器里能改变的。

登录之后，你看到的一切都属于某个 **space**（见下文），应用会记住你上次所在的位置。

## 布局

**左侧栏** —— 你的主导航：

- 顶部的 **space 切换器**。你的个人 space 列在 *Personal* 下
  （在你重命名之前它叫 *My Space*）；共享 space 列在 *Spaces* 下。**+** 按钮可创建一个新 space。
- **Home** —— 入口。在这里描述你想完成的工作来开始一段对话。见
  [对话与 Issue](对话与Issue.md)。
- **Issues** —— 当前 space 中工作项的列表。
- **Workflows** —— 可复用的分步计划。见
  [Agent 与 Workflow](Agent与工作流.md)。
- **Agents** —— 已保存、可复用的 Agent 定义。
- **Artifacts** —— 运行产生的文件和输出。
- **Administration** —— 部署级别的设置。仅当你持有系统管理员授权时才会出现。

**顶部栏** —— 在右侧你会看到一个 **Help** 图标（本手册）、一个
**Marketplace** 图标（此部署发布的插件），以及一个明暗主题切换开关。

**用户菜单** —— 侧栏底部的按钮可打开 **Account**、
**Space** 设置、**Help** 和 **Sign Out**。

## Space 与角色

**space** 是所有权边界：Issue、对话、Agent、Workflow、
上传的文件和运行结果都属于某一个 space，永远不会从另一个 space 看到。
在侧栏中切换 space 会改变你看到的一切。

Space 有三种角色：

- **Owner** —— 完全控制，包括 secret。
- **Admin** —— 管理成员和 space 设置。
- **Member** —— 在 space 中开展工作。

你的个人 *My Space* 是一个始终属于你自己的单成员 space。

## Space 设置

从用户菜单打开 **Space**（owner 和 admin 可以修改这些）：

- **Overview** —— space 的基本信息及其共享的 **Agent instructions**。
  你在此处填写的文本会发送给此 space 中*每一次*后台 Agent 运行，
  在所选 Agent 自己的指令之前。请保持简短，并且切勿在其中放入密码、
  API key 或其他 secret，因为它会随每一次模型调用一起发送。
- **Members** —— 邀请和管理人员及其角色。
- **Sandbox defaults** —— 此 space 运行中 `Bash` 的默认约束。
  见 [沙箱](沙箱.md)。
- **Secrets** —— 运行可以使用的值，由 owner 管理。
- **Service accounts** —— 由此 space 拥有、供其自动化工作以其身份运行的身份，
  这样工作就不依赖某一个人的账号。owner 和 admin 可以创建、重命名、停用和重新
  启用它们。每个 service account 都有一位 **sponsor**（担保人），即对其负责的
  owner 或 admin；当担保人不再担任该角色或被停用时，该账号会显示
  **Needs a sponsor**，直到某位 owner 或 admin 选择 **Take sponsorship**。
  service account 只是此 space 的成员，不能登录，也不会出现在选择人员的地方，
  例如 Issue 的负责人。个人 space 不能拥有它们。
- **Assistants** —— 此 space 通过自己的 Telegram bot 发布的服务入口，例如 HR
  助手或审计助手，面向 space 本身工作以外的人。owner 和 admin 为每个 Assistant
  设定指令、它可以运行的 Agent 和已发布的 Workflow（以及哪些结果字段可以展示
  给提问者）、它可以读取的已上传文件（Artifacts），以及谁可以提问：此 space 的成员，或所有活跃
  用户。它的工作以一个 service account 的身份运行，除非你另行选择，否则会按它
  的名称自动创建。Assistant 创建后处于暂停状态。**Publish** 会准确列出谁可以
  提问、它能读取和运行什么（包括这些 Agent 持有的 Secret，以及它们运行时能读取的 space Files），并请你确认，因为它
  能触及的一切都等同于披露给所有可以提问的人；对已发布的 Assistant 修改这些内容
  时会再次请你确认。**Bind bot** 接受来自 [@BotFather](https://t.me/BotFather)
  的 token；它会被加密存储，因此部署需要配置 `secret.kek_file`，已经接入
  BuildMax 的 bot 会被拒绝。人们需要先关联自己的 Telegram 账号才能联系这个 bot
  （见[聊天应用](聊天应用.md)）。已发布的 Assistant 在私聊中回答其受众的问题，
  只能启动名册中的 Agent 和 Workflow，并在第一次回复时告诉对方由哪个 space 运营、
  该 space 可以查看这段对话；这段对话会出现在 space 的对话列表中，但只能在聊天中
  继续。`/new` 开始新对话，`/help` 介绍这个 Assistant。它会直接根据可读文件作答，
  无需启动工作；它只读取文本文件（Markdown、纯文本、CSV、JSON、YAML 等），每个不超过
  128 KiB。它的 Agent 和 Workflow 步骤看不到这些上传：它们读取的是 space 的
  **Files**，所以两者都需要的政策文件要分别上传到两处。它启动的每个 Agent 和 Workflow
  步骤都会被告知提问者的姓名和邮箱，取自其 BuildMax 账号，而不是对方在聊天里写的
  内容，因此可以在查询个人记录的 Agent 的指令中要求它只为这个人作答。对于工作结果，Assistant
  和提问者只能看到名册条目标为可放行的字段，看不到原始输出、错误文本或链接；Agent
  的 task 结束时，对方会在聊天中收到这些字段，失败时则收到一句简短说明。它无法回答
  时会升级：在 space 中创建一个 issue，并告诉对方会有人跟进；成员在该 issue 上用
  **Reply to requester** 跟进（见[对话与 Issue](对话与Issue.md#issue-详情)）。schedule
  也可以通过它把结果发给某人（见[Agent 与 Workflow](Agent与工作流.md#通过-assistant-把结果发给某人)）。已暂停的会
  说明它处于暂停状态。个人 space 不能拥有它们。
- **Audit** —— 此 space 中所发生事件的记录。

## 模型如何被选择

Portal 中的运行使用你的部署所管理的模型，因此你无需把 API key 粘贴到浏览器中。
哪些模型可用、以及如何计费，由你的运维人员设置。关于直接发往提供商的模型调用
与经过 BuildMax 部署的模型调用之间的区别，见
[模型与模式](模型与模式.md)。

## 下一步

- 启动并跟踪工作：[对话与 Issue](对话与Issue.md)。
- 构建可复用的 Agent 和计划：[Agent 与 Workflow](Agent与工作流.md)。
- 理解界面背后的对象：[核心概念](核心概念.md)。
