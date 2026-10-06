# UI 体验专项

> **翻译说明：** 本文是[英文原文](../../design/ui-experience-program.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。

> **受众：** Portal、Desktop 与 `@buildmax/gui` 贡献者 · **状态：** 活动计划 —— 未开始
>
> **开启：** 2026-10-05

相关文档：[路线图](../ROADMAP.md) R6、
[Beta 就绪记录](../deploy/beta-readiness.md)、
[界面定位](界面定位.md)、
[Portal 前端页面体系](portal-frontend-page-system.md)、
[Portal 状态与权限反馈](Portal状态与权限反馈.md)、
[Portal 响应式与无障碍交互](Portal响应式与无障碍交互.md)、
[客户端界面收敛提案](../proposals/client-surface-convergence.md)、
[探索性测试](../contribute/exploratory-testing.md)，以及
[Portal 架构](../contribute/architecture/portal.md)。

## 目录

- [决策](#决策)
- [用户结果](#用户结果)
- [证据](#证据)
- [决定](#决定)
- [阶段](#阶段)
- [与现有记录的关系](#与现有记录的关系)
- [非目标](#非目标)
- [完成标准](#完成标准)
- [开放问题](#开放问题)

## 决策

BuildMax 开设一个专项，用来验证并改造 Portal 与 Desktop 的用户界面。
v0.2.0-alpha.22 通过了功能层面的 Beta 验证，但除了构建某条流程的人之外，还没有人
检查过这些流程是否好用。专项的做法是：

1. **先审计，再重新设计。** 由 Agent 驱动走查核心流程，产出分级问题清单。这份
   清单决定哪些页面需要改造，以及视觉语言是否需要改变。
2. **先建护栏。** token 完整性、lint 规则、视觉回归与对比度检查先于任何页面改造
   落地，这样改造过程中出现的退化不会被悄悄放过。
3. **Portal 与 Desktop 收敛到同一展示层。** 两端共享 `@buildmax/gui` 的 token 与
   基础组件。Desktop 不再手写浮层、按钮和图标。
4. **两端都提供英文与简体中文界面。**

范围是 Portal 与 Desktop，不包括 CLI/TUI。

## 用户结果

从未接触过 BuildMax 的人，无需帮助就能走完 Portal 与 Desktop 的核心流程。每一步
他都能看清自己的工作处于什么状态、出了什么问题。界面使用他的语言，英文或简体中文。
两个界面看起来、用起来都像同一个产品。

## 证据

以下事实取自 2026-10-05 的 `main`，提交为 `d56f0cdc`。

**验证缺口。** 所有 Beta 验证流程都由实现它的 Agent 用探针脚本驱动。非作者运维流程
（Q7）被豁免。打包后的 Desktop 启动与 Desktop UI 套件需要原生窗口，因此被接受为部分
完成。页面体系记录自己列出的验收项，即由非作者审视 Issue 到结果、失败到诊断这两条
流程，至今没有完成。已提交的探索性测试只有 2026-09-13 的一轮跨界面走查。大多数 UI
缺陷是浏览器冒烟测试和 Agent 演练发现的，不是通过观察真人使用发现的。

**设计记录大多已落地。** 七份 Portal 设计记录中有五份已经完成。仍未完成的是：

- 页面体系组件：`PageHeader`、`CollectionFrame`、`DetailHeader`、`StatusLabel`，
  以及窄屏编辑器上吸附的 Save/Cancel。
- 统一的状态词汇。目前状态标签同时来自 `lib/statusLabels.ts` 与
  `features/conversations/thread.ts`。
- Issue Detail 上按任务定位的变更错误。
- 对比度检查。axe 套件关闭了 `color-contrast`。
- 自动化的 200% 缩放检查。

交互模型不需要重新发明。缺的是两样东西：证明它对新用户确实可用，以及支撑它的共享
基础。

**Token 体系。**

- `gui/src/theme.css` 定义了颜色、间距、字号与圆角 token，但实际采用几乎只限于颜色：
  CSS 中约有 515 处字面量 `font-size` 和 217 处字面量 `border-radius`，对应的 token
  使用只有 20 处和 13 处。
- 有九个自定义属性被引用约 60 次，却没有任何地方定义，也没有回退值，所以这些声明
  会静默失效。它们是 `--border-color`、`--color-warning`、`--panel-bg`、
  `--color-error`、`--color-focus`、`--color-status-success`、
  `--color-bg-subtle`、`--color-surface` 与 `--muted-text`，出现在 Portal 页面 CSS
  和 Desktop 的 `schedules.css` 中。
- 至少有五种不同的危险红色被硬编码。两个应用的 CSS 中约有 165 个十六进制字面量
  不跟随暗色主题。

**Desktop 是一座独立的展示孤岛。**

- Desktop 是无类型的 JSX。
- 它从 `@buildmax/gui` 只导入了 `ThemeProvider`、`useTheme`、`Avatar`、
  `ChatThread`、`ChatComposer` 与 `QuestionForm`。它渲染了约 100 个原生
  `<button>`，从未使用共享的 `Button`。
- `components/Modals.jsx` 中的模态框处理了 Escape，但没有焦点陷阱。它们重复实现了
  `BaseModal` 与 `useOverlayA11y`，却没有提供那些保证。它的抽屉沿用 diff 的样式类，
  而不是使用 `Drawer`。
- Portal、Desktop 与 gui 各有一套图标。
- `App.jsx` 用 1,415 行承载状态与标签页编排。

**缺少共享基础组件。** 两个应用都没有共享的表单字段、下拉选择、菜单或弹出层、
toast、加载指示或骨架屏、表格。Portal 页面借用模态框的样式类来给输入框加样式。
菜单在各处各自重写：共有七个 `role="menu"` 实现。两个应用里一共有 58 处手写的
“Loading…”。

**没有防止视觉退化的护栏。** 没有截图比对，没有 `eslint-plugin-jsx-a11y`，也没有
禁止原始颜色或未定义自定义属性的 lint 规则。之前每一轮迁移之后，都跟着一串 e2e
选择器修复。

**没有本地化。** 两个应用都硬编码英文，并设置 `lang="en"`。只有 Help 页内嵌了一张
中英文对照表。用户手册已经提供了完整的 `manual/zh/` 翻译。

## 决定

### D1. 先审计，再重新设计

是保留并打磨当前的中性视觉语言，还是采用新的视觉语言，依据审计证据决定，而不是
依据个人喜好。审计报告的结尾给出建议及其依据的问题，由维护者在阶段 2 拍板。在此
之前，基础工作对这个选择保持中立：token 与基础组件会让两种结果都更容易实现。

### D2. 由 Agent 驱动验证，并如实记录

维护者选择了由 Agent 执行验证，而不是招募真人测试者。审计与复审都遵循
[探索性测试指南](../contribute/exploratory-testing.md)：

- **Portal** 在临时 kind 集群上运行，确定性流程使用 mock 模型。生成内容会影响体验
  的地方，使用已配置的真实模型。
- **Desktop** 通过 `wails dev` 的浏览器桥运行，与 `./make e2e desktop-ui` 走同一
  路径。
- 每条流程都在 390、768 与 1280 px 下，以亮色和暗色主题截图。阶段 1 提供多语言
  之后，也覆盖两种语言。

问题按严重程度分级：

- **Blocker：** 无法完成流程。
- **Major：** 用户被误导，或者只能依靠他不该具备的知识才能恢复。
- **Minor：** 摩擦或不一致。
- **Cosmetic：** 外观问题。

每个问题都注明页面、步骤与复现方式。评分维度包括：可发现性、状态可读性、错误恢复、
一致性、密度与层级、无障碍、文案。

这次审计是关于产品的证据，不能替代 Q7。报告会写明执行者是一个了解代码库的 Agent，
并列出浏览器桥看不到的部分：原生窗口焦点、文件选择器与打包后的布局。

### D3. 先建护栏，再改造

阶段 1 加入能让后续阶段保持诚实的检查：

- **Stylelint** 拒绝 `gui/src/theme.css` 之外的原始颜色字面量（xterm 主题是唯一
  具名例外），也拒绝任何样式表都没有定义的自定义属性。
- **视觉回归** 用 Playwright 对 `/specimen` 页面以及每种 Portal 模板各一个代表页面
  截图，并在 CI 中比对。Desktop 的黄金路径视图也做同样处理。
- **无障碍检查：** 开启 axe 的 `color-contrast`，并修复它发现的每个违规，而不是
  压制。三个包都运行 `eslint-plugin-jsx-a11y`。

这些护栏不改变 Portal 的测试分工。浏览器层面的断言仍然放在 Playwright 中，gui 用
vitest 一次性证明共享组件的行为。

### D4. 两端共用一个展示层

`@buildmax/gui` 负责 token、图标集，以及两端都需要的基础组件：

- 表单字段、输入框、下拉选择与多行文本框
- 菜单与弹出层
- toast
- 加载指示与骨架屏
- 基于统一状态词汇的 `StatusLabel`
- 页面体系记录中列出的页面骨架组件

只有当两端都会用到某个基础组件，或者 Portal 在不止一种模板中用到它时，它才进入
gui；否则留在本地。

Desktop 改用 gui 的 `Button`、`BaseModal`、`Drawer` 与图标，并删除自己的浮层 CSS。
这只涉及展示层。Desktop 的数据层仍然使用 Wails 绑定；共享数据接口仍是
[客户端界面收敛提案](../proposals/client-surface-convergence.md)中的开放问题。

### D5. 通过共享的类型化文案目录实现本地化

两端都提供英文与简体中文。

- **归属。** `@buildmax/gui` 提供一个小型 locale provider，以及基于类型化文案
  目录的 `useT()` 查找。每个应用拥有自己的目录文件。gui 自身的文案（例如
  `QuestionForm` 与 `ChatComposer` 中的文案）放在 gui 的目录里。
- **不引入库。** 只需要插值与英文复数规则。引入依赖带来的概念比它省掉的还多。
- **语言选择。** 语言是按设备保存的偏好：Portal 与 Desktop 的 webview 都把它与主题
  一起存于 `localStorage`，默认取自 `navigator.language`。账户级偏好先不做，等有人需要在多台设备
  上保持同一语言时再加。
- **Help 页。** 内嵌的对照表迁入目录，并跟随界面语言。
- **服务端消息。** API 错误文本保持英文。界面翻译它能识别的情形，其余情况显示
  服务端原文。
- **术语。** 界面沿用中文手册的约定。BuildMax 实体名保留英文：Agent、Space、Issue、
  Task、TaskRun、Workflow、Artifact、Portal、Desktop、MCP 与 Webhook。用户在手册、
  CLI 与 API 错误中都会遇到它们，同一个名字才能处处通用。通用术语则翻译：
  Conversation 与 Chat 译为“对话”，Session 译为“会话”，Run 译为“运行”，Schedule
  译为“定时任务”，Plugin 译为“插件”，Marketplace 译为“插件市场”，Secret 译为
  “密钥”，Files 译为“文件”，Assistant 译为“助手”，Remote Control 译为“远程控制”，
  Skill 译为“技能”，Audit 译为“审计”，Service account 译为“服务账号”，
  Administration 译为“系统管理”。对话记录里 AI 一方的发言人在两种语言中都标为
  Agent，因为“助手”只用来指 Space Assistant 功能。
- **日期与时间。** 日期与时间跟随界面语言。英文沿用系统的地区格式；中文使用中文格式，
  让文字与数字保持一致。

英文为权威语言。中文缺失的键回退到英文，并有检查报告缺失的键。在 effect 或异步回调中
组合的文本（例如加载失败时的兜底错误）使用 `useStableT`，它的引用在切换语言后保持
不变，因此切换语言不会重新加载数据或重新订阅事件。

Portal 与 Desktop 的所有页面都已提供两种语言；只有 `/specimen` 设计样张页保持英文。
页面提取没有等待阶段 2：文案会在改造时随组件一起移动，先提取不会造成重复工作。日期也
跟随界面语言，在渲染时格式化，而不是存成映射好的英文标签。

## 阶段

| 阶段 | 结果 | 可执行工作 |
|---|---|---|
| 0. 审计 | Portal 与 Desktop 核心流程的分级问题报告，并附视觉语言建议 | [backlog 10](../../backlog/10-ui-journey-audit.md) |
| 1. 基础 | 有 lint 强制的完整 token；视觉回归与对比度护栏；共享基础组件；i18n 基础设施；Desktop 改用 gui 基础组件 | backlog [14](../../backlog/14-ui-visual-and-a11y-guardrails.md)、[16](../../backlog/16-gui-shared-primitives.md)、[20](../../backlog/20-desktop-gui-convergence.md) |
| 2. 视觉语言决定 | 维护者依据阶段 0 报告选择“打磨”或“新视觉语言”，并更新本记录与页面体系记录 | 决定，不是任务 |
| 3. Portal 改造 | 按流程顺序解决 Blocker 与 Major 问题 | 依据阶段 0 报告拆分任务 |
| 4. Desktop 改造 | Desktop 同样处理；问题涉及 `App.jsx` 的地方顺带拆分 | 依据阶段 0 报告拆分任务 |
| 5. 中文与复审 | 完整的 `zh-CN` 目录（已交付）；用两种语言重跑阶段 0 的流程，并与基线对比 | 复审在阶段 3–4 之后拆分 |

阶段 0 与 token、护栏、i18n 任务彼此独立，可以并行。基础组件与 Desktop 收敛工作
依赖 token 完整性。改造任务要等审计结果出来再拆分，因为任务内容就是审计的产出。

## 与现有记录的关系

各份 Portal 记录仍是各自领域的规范：页面骨架与操作语法、状态反馈、响应式与无障碍
交互、导航，以及工作体验。本记录负责围绕它们的专项本身：执行顺序、验证、跨界面
展示层与本地化。页面体系记录中剩余的事项（骨架组件、吸附操作栏、统一状态词汇、
运维审视）在本专项中执行，每落地一项就在那份记录中标记为已交付。

## 非目标

- CLI/TUI 界面。
- Desktop 数据层、PWA 或移动客户端。这些属于
  [客户端界面收敛提案](../proposals/client-surface-convergence.md)。
- 新的产品能力。改造只改变现有能力的呈现方式与到达路径。
- 翻译服务端生成的文本、Agent 输出，或现有范围之外的文档。
- Portal 性能工作。它仍是 R5 候选项，除非审计发现性能阻碍了某条流程。
- 替代 Beta 就绪记录中的非作者运维流程（Q7）。

## 完成标准

- 阶段 0 审计中的每个 Blocker 与 Major 问题都已解决，或由维护者带记录理由接受。
  复审没有发现新的 Blocker 或 Major。
- lint 保证没有引用未定义的自定义属性，token 文件之外没有原始颜色。
- 视觉回归、包含 `color-contrast` 的 axe 检查与 jsx-a11y 在 CI 中覆盖 Portal、
  Desktop 与 gui。
- Desktop 不再渲染手写浮层、自有按钮体系或独立图标集。
- Portal、Desktop 与 gui 中每条用户可见文案都来自目录。`zh-CN` 没有缺失的键。
  黄金路径 e2e 流程在两种语言下都通过。
- 用户手册、Portal 与 Desktop 架构文档以及当前状态文档描述的都是已交付的结果。

## 开放问题

- **视觉语言。** 打磨当前的中性风格，还是采用新风格？在阶段 2 依据审计决定。
- **Desktop TypeScript。** Desktop 是否迁移到 TypeScript？默认只转换收敛工作涉及的
  文件。只有当审计或收敛工作表明无类型的 props 正在导致缺陷时，才做全量迁移。
- **截图基线。** Linux CI 与 macOS 的字体渲染不同，基线在哪个平台生成？存放在哪里？
  由阶段 1 决定，预期答案是只以 Linux CI 作为基线平台。
- **Space 默认落地页。** 应该是 Chat 还是 Issues？这个问题继承自页面体系记录，审计
  应当为它提供证据。
