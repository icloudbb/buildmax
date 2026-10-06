# Portal UI 旅程审查（阶段 0）

> **翻译说明：** 本文是[英文原文](../../../contribute/exploratory-runs/2026-10-06-portal-ui-journey-audit.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
> **受众：** Portal 与 `@buildmax/gui` 贡献者、维护者 · **状态：** 待分诊

本文是 [UI 体验专项](../../design/UI体验专项.md)阶段 0 审查的 Portal 部分。Desktop 部分见 [2026-10-06-desktop-ui-journey-audit.md](2026-10-06-desktop-ui-journey-audit.md)。

**操作者是一个了解仓库的 Agent，而不是新用户。** 它知道路由结构、fixture 数据和代码位置，并用这些知识搭建环境、解释结果；每一步都尽量只按屏幕上能看到的内容来判断。本次审查是关于产品的证据，不是“非作者操作者旅程”（Q7）。

## 章程

| 字段 | 值 |
|---|---|
| 旅程 | 1. 首次登录与认识 Space。2. 创建 Issue 并运行到出结果。3. 从 Issue 和系统管理两处诊断一次失败的运行。4. 编写并运行 Workflow。5. 设置定时任务。6. 找到共享的 Artifact 或文件。7. 邀请成员并修改其角色。 |
| 为什么 | 至今每条 Portal 旅程都只由其实现者检查过。审查结果决定哪些页面重做（阶段 3），以及视觉语言是否改变（阶段 2）。 |
| 角色与初始数据 | `nora@buildmax.local`：只有个人 Space 的新账户。`alice@buildmax.local`：Space 所有者兼系统管理员，带 `./make kind fixtures --runs` 数据。 |
| 成功标准 | 每条旅程都能通过可见控件完成；每一步都能看出工作处于什么状态、下一步该做什么。 |
| 截图 | 每个关键页面在 390、768、1280 px 下各截亮色与暗色，另在 1280 px（亮色）下用简体中文截一遍。 |
| 范围与改动 | 仅限任务自有的临时集群。在其中创建了测试资源（一个 Agent、一个 Issue、一个 Workflow、一个定时任务、一个邀请，以及一次随后撤回的角色修改）。 |
| 模型 | 只用集群内的 mock 模型（免费、确定性）。 |
| 预算 | 约 75 分钟操作。会话因 API 限流中断一夜，之后在同一集群上继续。 |

## 环境

- **源码：** `main` 的 `e98efc7a`，已包含 UI 专项、token 修复和完整的中文界面。除认领 backlog 的提交外，工作树干净。
- **部署：** 临时 kind 集群 `buildmax-eph-1981bb29`，由 `BUILDMAX_KIND_EPHEMERAL=1 ./make kind up` 创建，server 与 Portal 镜像都从该提交构建。Portal 地址为 `http://localhost:54421`。种子数据来自 `./make kind fixtures --runs`。
- **浏览器：** 无头 Chromium 1243（仓库的 Playwright 安装），通过 CDP 驱动，每一步运行一个脚本。截图是原始视口，没有展开页面。主题和语言通过界面自己写入的同一组 `localStorage` 键（`buildmax_theme`、`buildmax_locale`）设置。
- **属于准备工作、不属于旅程：** 登录码用 `./make kind login <email>` 生成，再输入登录码表单。这是受支持的运维路径；新用户会从管理员处拿到登录码。
- **时间：** 2026-10-06 14:45–15:00 与 2026-10-07 05:59–06:12（UTC+8）。

## 探索过程

1. **首次登录。** 登录页首先提供 Okta 和邮箱/密码。“忘记密码，或已有登录码？”链接打开登录码表单。登录后 Nora 落在 “My Space” 的**对话**页：有一个输入框、一对“最近的对话 / 文件”标签，侧栏分为工作、资源、管理三组。页面上没有任何内容解释 Space、Issue 或 Agent 是什么。任何宽度下都没有出现横向溢出（`p03-first-landing-*`）。
2. **从 Issue 到结果。** 创建 Issue 很顺利（`p05`、`p06`），运行它却不顺利：执行者列表里只有“None”，提示只提到已发布的 Workflow。Nora 只有找到 **Agents**、新建一个 Agent、再回来编辑 Issue，才能运行。之后 **Run agent** 跳转到 Task 页，mock 几秒内给出回答（`p11`–`p13`）。回到 Issue，结果出现在五个互相矛盾的地方（发现 P1）。
3. **失败的运行。** 以 Alice 身份把 “Unassigned backlog item” 的执行者设为 “QA Blocked Agent”。该 Agent 的密钥授权已被停用，其 Agent 页已显示警告。运行约 0 秒即失败，原因只以原始 worker 错误呈现（P2）。在**系统管理**里，这次失败只被计入“Space 配置 → Space 所有者”，没有通往这次运行的入口，这与“只看元数据”的设计一致（P5）。
4. **Workflow。** 在带 React Flow 画布的“新建 Workflow”弹窗中编写了一个 Workflow（`p21`、`p22`），保存为草稿。**运行 Workflow** 按钮处于禁用状态，没有说明原因。**发布**后运行成功，运行页却显示“没有产生结果”（`p26`、P1/P10/P11）。
5. **定时任务。** 为该 Workflow 创建了每周一 09:00 的定时任务（`p29`、`p31`）。无效的 cron 文本和时区都被拒绝，提示是服务器原始文本，并且一次只报一个错误（P12）。
6. **共享的 Artifact 或文件。** 文件页的根目录叫 “home”（P7）。Artifact 列表有三个文件，但看不出哪个已共享；只有逐个打开 Artifact 的**分享**对话框才能知道（P19）。
7. **成员。** 对已有账户的**邀请**可以完成，但新的待接受行只显示一个不透明的 ID（P4）。不存在的邮箱会得到面向运维的 API 提示（P13）。通过角色下拉框把 Carol 改为 Admin 立即生效，刷新后仍然保持（P14），之后已改回原角色。
8. **中文检查。** 每个截过的页面都在 1280 px 下以简体中文渲染了一遍，没有页面溢出。翻译相关发现并入下文各条，并在[中文界面](#中文界面)中汇总。

## 发现

严重度遵循专项的评分标准：Blocker、Major、Minor、Cosmetic。ID 来自 nora 的个人 Space（`e5ffwpdprxbc22m3ix7a`）和 fixture Space “BuildMax QA”（`vnriocemqxlnxwptmwoq`）。两者都在已删除的集群上，只用于对应截图，不是在线资源。

| 严重度 | 数量 |
|---|---|
| Blocker | 0 |
| Major | 4 |
| Minor | 16 |
| Cosmetic | 6 |

### Major

**P1. Issue 的结果出现在五个互相矛盾的地方。** *状态可读性。*

- 页面：Issue 详情、结果标签页、运行标签页和 Workflow 运行页。
- 复现：
  1. 创建一个 Issue。
  2. 设置 Agent 执行者，点 **Run agent**。
  3. Task 显示 Done 后回到 Issue。
- 实际：
  - 概览卡片写着 **Latest result: No result yet.**
  - 下方的 **Latest Outcome** 区块写着 **Succeeded**，并显示 Task ID。
  - **结果**标签页写着 “No results produced yet”。
  - **运行**标签页先写 “Run History 0 total … No runs yet.”，紧接着是 “Agent Run Sequence 1 tasks … Succeeded”。
  - Agent 的实际输出（“deployment smoke ok”）只作为一条**讨论**评论出现。
- Workflow 运行页重复同样的模式：“Result: Succeeded — No result was produced”，而该步骤的输出在页面更下方（`p26`）。
- 中文界面中两个标签都叫**最新结果**，一个写“暂无结果”，一个写“成功”（`p14-issue-with-result-zh-1280`）。
- 期望：Issue 上对“成功了吗、产出了什么”只有一个答案。
- 种子观察“Latest Outcome 出现两次”**属实**，而且比重复更糟：两处说法互相矛盾。
- 证据：`p14-issue-with-result-{390,768,1280}-{light,dark}`、`p15-issue-tab-results`、`p15-issue-tab-runs`。

**P2. 运行失败时只显示原始内部错误，并提供一个不可能成功的重试。** *错误恢复。*

- 页面：Task 页、Issue 讨论、Task 详情和运行详情。
- 复现（Alice，在 BuildMax QA 中）：
  1. 把 “Unassigned backlog item” 的执行者设为 **QA Blocked Agent**。
  2. 点 **Run agent**。
- 实际：
  - 对话记录里，Agent 的气泡中出现一条斜体消息：`secret grant unavailable: worker API GET /api/worker/task-runs/<id>/secrets: secret is disabled (409)`。
  - Issue 的 Latest Outcome 只写 “Failed”。
  - **Details**（Task 详情）列出了 Agent、状态和 0 秒耗时，但没有原因。
  - **运行详情**写着 “no trace was recorded for this run”，浏览器同时记录了一个 404。
  - 最醒目的操作是 **Retry last run** 和 **Retry Run**，重试会以同样的方式失败。
  - 没有链接指向该 Agent，而它的页面早已显示 “⚠ 1 secret grant no longer resolve. Fix in config”。
  - **Run agent** 在启动运行前没有就这个已知的配置问题给出任何提醒。
- 期望：用用户能理解的话说明原因（哪个密钥、哪个 Agent），并把修复作为首要操作。
- 证据：`a12-failed-task-1280-light`、`a13-failed-task-details`、`a14-issue-after-failure-*`、`a15-run-details-from-issue`。

**P3. 新用户找不到从 Issue 通往“能运行它的东西”的路径。** *可发现性。*

- 页面：新建 Issue 对话框与 Issue 详情。
- 复现：
  1. 用新账户登录，打开 **Issue**。
  2. 点**新建 Issue**。
- 实际：
  - 执行者下拉框里只有 “None”。
  - 提示写 “What runs the work. Only published workflows are available.”，没有提到 Agent；即使 Agent 已存在并出现在列表中，这句提示也不变。
  - 执行者为 None 的 Issue 页既没有运行操作，也没有提示。
  - Nora 只有离开 Issue、找到 Agent、新建一个、再回到**编辑 Issue**，才能运行。
- 期望：说明需要一个 Agent 或已发布的 Workflow，并给出创建入口。
- 证据：`p05-new-issue-*`、`p06-issue-detail-new-*`、`p07-agents-empty`。

**P4. 待接受的邀请只用不透明 ID 标识。** *状态可读性。*

- 页面：Space 设置 → 成员。
- 复现：
  1. 以 Member 身份**邀请** `nora@buildmax.local`。
- 实际：新的一行写着 `3n4sj7muxl32i3ejxscq — Invited as member, expires 10/10/2026, 06:09:40`，旁边是更早的 `q6p6c6vl2454y2xv547q`。两行都没有写被邀请人，所有者无法判断该**撤销**哪一个。
- 期望：显示被邀请人的邮箱或姓名。
- 证据：`a28-invite-sent-1280-light`、`a25-members-*`。

### Minor

**P5. 系统管理页显示原始标识符，对失败也不给下一步。**

- 页面：系统管理 → 概览与 Space。
- 页面上的原始值：
  - 健康项标签 `DATABASE`、`OBJECT_STORAGE` 与 `ok`；
  - Task 运行计数 `CANCELED`、`FAILED`、`RUNNING`、`SUCCEEDED`；
  - 角色 `system_admin`、worker 模式 `k8s_job`、传输方式 `direct`、等级 `pro` 与 `free_trial`；
  - “需要关注的 Space” 表格中的运行 ID。
- 这些在中文界面中同样未翻译。配额 “258 / 10000000” 没有格式化，“1 running” 换行成 “1 runnin g”。
- FAILED 单元格（“3 space configuration”）写了谁来处理，却没写该告诉对方什么。“只看元数据”是设计如此，所以缺的是措辞而不是访问权限。
- 种子观察“系统管理显示原始标识符”**属实**。
- 证据：`a06-admin-overview-*`、`a07`、`a08`、`a17-admin-space-detail-*`。

**P6. Agent 成功率把取消的运行算作失败。**

- 页面：Agent 列表的指标与 Agent 详情。
- `AgentList.tsx` 和 `AgentDetail.tsx` 使用 `taskRunFailed`，它把 `CANCELED` 视为失败。
- 观察到：QA Writer 有 18 次运行（14 次 Done、2 次 Stopped、2 次 “Needs your answer”），显示 **88%**，即 14 ÷ 16，两次停止的运行被算作失败。
- 因此，唯一结束的运行都被取消的 Agent 会显示 0%。操作者没能在屏幕上直接观察到这一点：mock 几秒内就完成运行，来不及点**停止**。
- 种子观察“只有已取消运行时显示 0%”**由代码与 88% 的算式证实**，但未在屏幕上复现。

**P7. 文件根目录叫 “home”，所在文件夹也不进入 URL。**

- 英文界面中，目录树根和面板标题都写 “home”。
- 中文界面中，面板标题是“根目录”，目录树根仍是 “home”。
- 打开文件夹不会改变 `#/…/files`，刷新后回到根目录。
- 种子观察**属实**。
- 证据：`a18-files-*`、`a18-files-zh-1280`。

**P8. Task 的命名不稳定。**

- Task 标题由模型生成。用 mock 时每次运行都叫 “deployment smoke ok”，连启动前就失败的运行也是。
- 从 Issue 进入时面包屑显示 Issue 标题，刷新后只显示泛化的 “Issue”。
- Agent 的运行列表混用生成的标题和原始提示词，例如 “Agent: QA Reviewer Description: Reviews acceptance…”。
- mock 放大了这个问题，但运行从不以它所服务的 Issue 或 Workflow 命名。

**P9. 运行 Issue 会离开 Issue。**

- **Run agent** 会跳转到 Task 页。
- 运行成功后 Issue 状态仍是 “To do”。
- 保存提示写 “use Run to schedule one”，但按钮名是 **Run agent**，“schedule” 又与定时任务功能撞名。

**P10. Workflow 编写密集，且使用开发者术语。**

- 图编辑器放在 600 px 宽的弹窗里。
- 步骤 ID 由机器生成（`step_a5fd8c28`）。
- Agent 选项带原始 ID：“Onboarding summarizer (vgivleuxrkt2av5233xa)”。
- “Issue 访问权限” 的选项是原始值 `none`、`if_bound`、`required`。
- 草稿状态下 **运行 Workflow** 被禁用，没有任何说明（没有 title，也没有描述）。
- 在 390 px 下画布几乎无法使用（`p23-workflow-detail-390-light`）。

**P11. Workflow 步骤的输出没有标签。**

- 输出是一个裸文本块，紧贴在折叠的 “Resolved input this node received” 摘要下方，读起来像是输入（`p27`）。

**P12. 定时任务表单的校验和预览都很弱。**

- 时区默认 `UTC`，而不是浏览器所在时区。
- 没有下次运行预览，Desktop 有。
- 错误是服务器原始文本（`timezone "Shanghai": unknown time zone Shanghai`），而且一次只报一个：无效的 cron “every monday” 直到时区改对后才被报告。
- “全部暂停” 和 “全部恢复” 同时显示。
- 定时任务行只有**暂停**，看不到编辑或删除。
- 证据：`p29`、`p30`、`p31-*`。

**P13. 邀请不存在的邮箱时，错误是写给运维的。**

- 内容为 “…ask a system administrator to create one (POST /api/admin/users or buildmax-server user create), then invite it”。

**P14. 从下拉框修改角色立即生效，既无确认也无“已保存”反馈。**

- 修改确实已保存（刷新后检查过）。一次误选就会静默授予 Admin。

**P15. Agent 对话框的文案与 Agent 的作用范围矛盾。**

- 对话框写 “Agents are personas or task templates you can use across your account”。
- 页面写的是 “space agents”，路由也在 Space 之下。

**P16. 侧栏导航没有以导航的形式暴露。**

- 各项是 `<button>`，没有 `aria-current`，当前页只有视觉标识。
- 它们不是链接，无法在新标签页打开，也无法复制链接。

**P17. 工作视图中出现原始 ID。**

- “Latest agent task: f4qupcltm5roh4o5snrq”。
- Task 详情：“Task czdpj4…”。
- Workflow 步骤：“Task: 7yzq… / Run: ruw5…”。

**P19. 无法找到已共享的 Artifact。**

- Artifact 列表不显示共享状态，也没有搜索或筛选。
- 要找到共享的 Artifact，只能逐个打开它的**分享**对话框（`a21`、`a22`、`a23`）。

**P20. 390 px 下 Space 设置的标签页被藏起来。**

- 横向滚动的胶囊只露出 8 个标签中的 3 个，且没有可滚动的提示（`a25-members-390-light`）。

**P22. Portal 发布一个 1.3 MB 的脚本，且未压缩。**

- `index-*.js` 为 1,300,313 字节（另有 151 KB CSS）。
- 经 kind ingress 以 `gzip` 请求时收到的字节数相同，说明文件没有被压缩。
- 没有阻断任何旅程，但每次首次加载都要付出这一成本。
- 种子观察“一个约 1 MB 的 chunk”**属实**：实际为 1.3 MB，并且在 kind 上未压缩。生产环境 ingress 是否压缩没有检查。

### Cosmetic

- **P18.** Issue 列表摘要显示原始 Markdown（“## Acceptance criteria - [ ] …”）。
- **P21.** 标题重复：面包屑与 h1 重复 “Chat”，390 px 下移动端头部又重复第三次。**返回 Issue 列表**与面包屑重复。390 px 下对话输入框的占位符在行中被截断。
- **P23.** 每次非管理员加载页面都会记录 `403 /api/admin/me`，未登录页面会记录一串 401。
- **P24.** 系统管理中的 Space 链接使用浏览器默认的蓝色下划线，与其他链接都不一致。
- **P25.** 语法错误：“1 tasks”；“1 secret grant no longer resolve”。
- **P27.** 主题不跟随系统设置，总是从亮色开始；`ThemeContext` 只读取 `localStorage`。

### 中文界面

- **布局：** 1280 px 下所有页面都没有溢出，也没有发现截断。
- **术语：** Space、Issue、Agent、Workflow、Artifact 保持英文，符合专项 D5 的规定。
- **未翻译文本**仅限于：
  - 服务器生成的文本（如 Task 提示词 “Work on this issue.” 和错误信息），D5 允许；
  - P5 中系统管理的标识符，D5 不允许；
  - 全大写的 “TOKEN” 标签。
- **误导性措辞：** P1 中两个**最新结果**区块，以及 P7 中“根目录”与 “home” 不一致。

## 种子观察

| 观察 | 结论 | 证据 |
|---|---|---|
| Issue 详情显示两次 “Latest Outcome / 最新结果” | 属实，且两处互相矛盾（P1）。 | `p14-*` |
| 只有已取消运行时 Agent 成功率显示 0% | 由代码和算式证实；因 mock 完成太快，未在屏幕上复现（P6）。 | `AgentList.tsx`，QA Writer 的 88% |
| 文件树根显示 “home”，面板标题却是 根目录/root | 中文界面中属实；英文界面两处都写 “home”（P7）。 | `a18-files-zh-1280` |
| 系统管理概览显示原始标识符 | 属实（P5）。 | `a06-*` |
| Portal 发布一个约 1 MB 的 JS chunk | 属实：1.3 MB，且在 kind 上未压缩（P22）。 | 网络测量 |

## 视觉语言建议

**打磨现有的中性风格，不采用新风格。** 四个 Major 发现都不是颜色、字体或布局风格造成的，而是信息架构与文案问题：结果放在哪里（P1）、失败如何自我解释（P2）、如何找到执行者（P3）、记录如何命名（P4）。两种主题下页面都清晰可读，任何宽度都没有溢出。

剩下的视觉问题是一致性缺口，正是阶段 1 的基础组件要解决的：

- 全大写原始状态词与有样式的标签并存（P5）——用一个基于统一状态词汇的 `StatusLabel`；
- 默认蓝色的管理链接（P24）、文件页的 emoji 文件夹图标、重复的头部（P21）——用页面结构组件和统一图标集；
- 新建 Issue 用弹窗、编辑 Issue 用内联表单，Workflow 用密集的图弹窗（P10）——用表单字段与布局基础组件。

换一套新的视觉语言，所有 Major 发现都会原样留下。

## 默认落地页证据

- **新用户。** Nora 的空个人 Space 落在**对话**页。对话是唯一能立即操作的页面：Issue 和 Agent 都是空的，而运行 Issue 需要先创建 Agent（P3）。
- **有工作的团队成员。** Alice 负责一个 Issue，所在团队 Space 里有一次失败的运行、2 个处于 “Needs your answer” 的 Task 运行和 1 个 Workflow 输入请求。她的对话落地页只显示一条旧对话，上述内容一概没有。Issue 页同样不显示这些待处理请求。它们只能通过 Agents → QA Writer → 运行，或系统管理里的计数找到。
- **切换 Space** 会保留当前栏目（在 Issue 页切换后仍是 Issue 页），所以落地页只在首次进入和访问裸 `#/` 时起作用。
- **结论：** 对团队成员来说，对话页和现在的 Issue 页都不显示“需要我处理的事”。对话页适合空 Space；团队 Space 需要一个能列出本人负责的 Issue 和待处理请求的落地页，而现在的 Issue 视图要补上这些，才更适合做默认页。

## 未覆盖或无法观察

- 在 Portal 对话中发送消息，以及使用真实模型的对话。Portal 的所有运行都用 mock，P8 中泛化的 Task 标题也来自 mock。
- Okta/OIDC 登录、密码登录与设置密码。
- 上传文件与 Artifact：需要原生文件选择器，无头驱动无法显示。
- 屏幕阅读器输出、纯键盘操作和 200% 缩放。无障碍相关发现来自 DOM 检查。
- 在运行结束前取消运行（P6）。
- 回答 Workflow 输入请求或 AskUser 问题。
- 生产环境 ingress 是否压缩脚本包。

## 清理

- 临时集群已用 `./make kind down` 删除。命令报告 “Deleted nodes”，并删除了 `.local/kind-ephemeral.env`。
- Chromium 配置目录已删除，没有残留的后台进程。
- 截图只保留在被 git 忽略的 `.artifacts/ui-audit/` 下。

## 后续

按旅程顺序，先根据 P1–P4 起草阶段 3 任务：

1. Issue 详情采用单一的结果模型，并延续到 Workflow 运行页。
2. 解释失败原因，并以修复作为首要恢复操作。
3. 从 Issue 表单提供通往执行者的路径。
4. 让邀请可以辨认。

P5、P6、P12、P22 是小而独立的修复。P6 是简单修复候选：把 `CANCELED` 从成功率分母中排除。
