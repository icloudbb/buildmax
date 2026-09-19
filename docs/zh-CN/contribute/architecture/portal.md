# Portal

> **翻译说明：** 本文是[英文原文](../../../contribute/architecture/portal.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
> **受众：** 贡献者 · **状态：** 当前有效

## 用途

Portal 是 `portal/` 下的 Space 协作界面。它是一个 React 19、Vite 和 TypeScript 应用，通过 HTTP 和 WebSocket 与 Go 服务器通信。

Portal 负责云端/Space 使用场景：

- 登录/注册
- Space、Space 切换与设置
- Conversation
- Issue
- Workflow 与 Workflow 运行
- 运行诊断：TaskRun 使用了什么、接触了什么、花费多少、为何结束，以及受到何种约束
- 停止正在进行的运行：Issue 详情为仍处于 pending 或 running 的 Task 提供 Stop Run；按钮保留到服务器响应，因为已启动的运行只有在 Worker 确认后才进入 `canceled`
- 重复已结束的运行：无论以何种状态结束，同一行都会在运行结束后提供 Retry Run；服务器拒绝时显示其给出的理由
- 面向所有者的 Space 审计记录
- Agent
- Space 级 Agent 指令：每次后台 Agent 运行都会继承，并在 Worker 领取时确定修订版本；不影响 Tier 1 协调器
- Artifact：Space 的持久文件，通过自身不透明地址列出和打开，而非经由产生它的运行访问
- Space 文件
- 用量与 webhook key

## 当前结构

- 路由位于 `portal/src/router.ts`。
- 页面位于 `portal/src/pages/*`。
- API 调用位于 `portal/src/features/*/api.ts` 和 `portal/src/lib/api`。
- 共享展示组件来自 `@buildmax/gui`。
- 共享 `Button` 和 `IconButton` 负责操作的外观、点击区域、焦点和忙碌状态。
  Portal 负责位置与权限判断。集合页在页头提供创建入口，空状态说明当前缺少什么。
  Issue 详情默认展示阅读视图，结果与下一步操作先于编辑表单。
  Task 详情和对话 Task 卡片使用相同操作层级；Chat 起始页负责自己的标题与可用键盘操作的标签页。
  Workflow 详情按生命周期视图安排主要操作：草稿发布、已发布阅读视图运行、编辑已发布版本时保存。
  Agent 详情同样安排运行、配置保存和创建 schedule 的优先级；标签页支持键盘操作，schedule 卡片自身管理重试状态。
  Artifacts 列表以上传为主要操作；详情及分享控件使用共享操作层级，预览在本区块内重试。
- 横切状态位于 `portal/src/contexts/`：`AppContext`、`AuthContext`、`SpaceContext`，以及承载 Conversation 流式传输的 `WebSocketContext`。
- HTTP 层是 `portal/src/lib/api/`（`client`、`mappers`、`types`，以及用于流式传输的 `sse` 和 `ws`）。
- `portal/src/features/conversations/` 绘制对话记录，并在同一线程中为 Conversation 启动的每个后台 Task 显示一张卡片。卡片从 tasks 路由读取，socket 每次报告失效通知时都会重新加载，因此运行产出了什么不依赖 Tier 1 对它撰写的摘要。`thread.ts` 决定顺序。
- `portal/src/features/runs/` 读取 TaskRun 的轨迹摘要、运行来源，以及部署为它服务的托管模型调用；`portal/src/features/audit/` 读取 Space 审计记录。两者都将显示决策放在纯模块中——`summary.ts`、`spend.ts` 和 `describe.ts`——而非组件内部，因为 Portal 没有 DOM 测试环境，而值得固定下来的判断恰恰是否则会缺乏测试的内容：未使用沙箱的运行必须明确说明；未记录的边界不等于没有约束；空的模型调用账本不等于运行没有花费；当前 Portal 不认识的审计动作必须原样显示，不能隐藏。

## 测试

单元测试通过 Vitest 测试纯模块，`vite.config.ts` 将 `e2e/` 排除在外。Portal 没有 DOM 测试环境，因此显示决策放在纯模块中：`features/runs/summary.ts`、`features/runs/spend.ts`、`features/audit/describe.ts`、`features/usage/pressure.ts`、`features/conversations/thread.ts`、`features/runs/origin.ts`、`features/artifacts/display.ts`，无需 DOM 即可断言。Artifact 模块镜像服务器的授权规则，以决定是否显示删除按钮，因此要固定两个方向的行为：镜像发生偏差时，要么显示会被拒绝的按钮，要么隐藏本可成功操作的按钮。

`portal/e2e/` 存放 Playwright 测试规格，由 `./make e2e` 针对一个部署运行。它们只覆盖浏览器能够展示的内容：发布后的 bundle 能否与真实服务器配合工作。API 级流程属于 `./make kind smoke`，在这里重复只会更慢，不会带来更多信息。

登录码由 `./make e2e` 签发，因为它按设计通过带外渠道送达，浏览器无法获取。Playwright 全局 setup 登录并为每个角色保存会话：部署管理员和不持有任何授权的账户。因此登录不是单独的测试规格：登录故障会在第一个测试开始前使整个套件失败。需要两个账户，是因为某个角色专属的视图只能由不具备该角色的人来证明其边界。

`./make e2e` 默认针对 kind 部署；`./make e2e compose` 针对快速入门栈运行相同规格；`./make e2e local` 启动 Compose 栈、运行测试后再销毁，适合尚无部署运行时使用。两个附着目标都在 `deployment-smoke.yml` 中，因为浏览器能观察到它们的差异。kind 通过同一 ingress 提供 Portal 和服务器，因此 bundle 的 API base 是同源地址；Compose 将它们发布到不同端口，因此 API base 是绝对地址。需要知道这一点的测试规格通过 `BUILDMAX_E2E_API_BASE` 获取信息，不假设任何一种形式；任务运行器传入它刚刚为浏览器指定的目标。每次运行将证据写入 `.artifacts/e2e/portal/`，写入前先清空，避免失败轨迹与旧运行混杂；同时写入说明，列出部署和复现命令。

`run-trace.spec.ts` 是“不预置数据”的例外。运行轨迹视图只能从 Issue 的输出打开，而 API 级冒烟测试创建的是 Conversation Task，没有这种输出，因此该规格先通过 API 创建 Issue 和 Agent 运行，再通过 UI 读取结果。这使纯模块对边界的断言在实际展示位置也得到验证：`summary.ts` 证明措辞，此测试证明真实运行能够到达那里。

## 产品边界

Portal 是云端 Space 工作区。CLI 和 Desktop 负责本地执行。Desktop 将来可能连接 Portal，但 Space 管理、Issue/Workflow 管理、Space 文件、治理和云端结果仍属于 Portal。
