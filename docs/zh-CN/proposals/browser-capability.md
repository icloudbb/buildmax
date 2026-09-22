# Agent 浏览器能力技术调研

> **翻译说明：** 本文是[英文原文](../../proposals/browser-capability.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
>
> **受众：** 贡献者与产品设计者 · **状态：** 提案，讨论中
>
> **讨论开始：** 2026-09-21 · **证据核对：** 2026-09-21

相关文档：[路线图](../ROADMAP.md)、[当前状态](../current-state.md)、
[Desktop 架构](../contribute/architecture/desktop.md)、
[工具架构](../contribute/architecture/tools.md)、
[工具权限](../design/工具权限.md)、
[沙箱边界](../design/沙箱边界.md)、
[Agent 桥接 CLI](../design/Agent 桥接 CLI.md)，以及
[Desktop 工作区 Tab](desktop-workspace-tabs.md)。

## 目录

- [1. 结论与决策边界](#1-结论与决策边界)
- [2. 用户结果与范围](#2-用户结果与范围)
- [3. 同类产品如何实现](#3-同类产品如何实现)
- [4. BuildMax 当前基础](#4-buildmax-当前基础)
- [5. 技术方案](#5-技术方案)
- [6. 候选架构](#6-候选架构)
- [7. 信任与故障边界](#7-信任与故障边界)
- [8. 原型实验与所需证据](#8-原型实验与所需证据)
- [9. 开放问题与后续归宿](#9-开放问题与后续归宿)
- [10. 外部资料](#10-外部资料)

## 1. 结论与决策边界

BuildMax 已能抓取和搜索网页，但 `WebFetch` 返回的是静态 HTML：不执行脚本、
无渲染状态、无控制台、无交互。缺少的能力是让 Agent **针对真实渲染的页面
验证一处变更**——观察它的 DOM 与控制台、截图、点击和输入——并依据观察到的
状态行动，而不是从源代码或 HTTP 响应推断成功。这是 Agent **运行时**的能力，
而非某个界面的能力：同一个"验证并修复"闭环在 CLI 和 Desktop 都有价值，因此
这项能力不应在设计上被绑死到 Desktop。

这个闭环的价值已被发货的同类产品证明——Codex、Claude Code、Cursor 和
VS Code 都已提供——因此开放问题不是工作流是否有用，而是架构与可移植、安全的
交付：Go 自持的控制器能否在不引入 Node 运行时的前提下驱动真实浏览器，以及
哪些界面应当启用它。

候选的首个实现是一个使用隔离 Profile 的 **Go 自持 Chromium 进程**，由 Go 通过
Chrome DevTools Protocol（CDP）驱动，由 `agentapp` 作为**可选能力**装配，并
**首先在本地运行（CLI 与 Desktop）以用户自身权限**启用。它支持 headless；
可见窗口对于验证结果不是必需的。在 Desktop 上，控制器可以额外呈现一个可见
窗口并把 Session 与其页面关联，但这种呈现是可选增强，不是构建此能力的理由。

两个决策可分离。**现在**要决定的是：是否构建一个 Go 自持的 CDP 浏览器控制器，
作为在本地界面启用的运行时能力。**可工作的原型完成后**再决定：把**同一个可
操作页面**渲染进 Desktop 工作区 Tab，是否值得承担第二个原生视图、浏览器引擎
分发、焦点与布局处理，以及更大的安全面。没有同类产品能证明 Wails v2 会开箱
提供这第二步。本文是调研与候选方向，不是已采纳架构，也不是路线图承诺。

## 2. 用户结果与范围

### 核心用户结果

Agent 能通过操作真实渲染的页面来验证一处对本地 Web 应用的变更：它导航到
路由，观察页面及其控制台的有界快照，与已识别的元素交互，并报告准确的路由、
交互步骤、观察结果和未覆盖范围。它依据实际观察到的页面状态行动，而不是源代码
或 HTTP 响应。这个结果**与界面无关**——在 CLI 上运行和在 Desktop 上一样有
价值——而且不需要有人盯着看。

### Desktop 可选增强

在 Desktop 上，同一个页面可以显示在可见窗口中，让用户观看，并可自行选择接管。
这是锦上添花，不是立论依据：在常见的"验证并修复"闭环里，Agent 是在无人值守
下走完旅程的，而用户到底多大概率会接管本身尚无证据。第一阶段必须在不依赖有人
观看的前提下交付验证结果；可见窗口与接管是一个 Desktop 专属的层，等证据支持
时再加，而不是第一阶段的必要条件。

### 第一阶段目标

- 打开 `http://localhost` 或其他经明确批准的 HTTP(S) 来源。
- 返回有大小上限的当前页面快照，标识可交互元素；基于这些元素点击、输入，
  并拒绝已经过期的引用。
- 从同一页面获取截图和最近的控制台错误。
- 浏览器状态与任何用户常用浏览器 Profile、其他 BuildMax Session 隔离。
- 取消运行时及时停止操作；在运行、Session 和进程关闭三个边界释放自身管理的
  进程与临时 Profile。

### 第一阶段非目标

- 自动操作用户日常 Chrome Profile，或悄悄继承其中的 Cookie、密码、历史和
  Tab。
- 向 Agent 提供任意 JavaScript 执行、原始 CDP、文件上传/下载，或不受限制的
  已登录交易操作。
- 替代 `WebSearch`、`WebFetch`，成为通用网页研究工具。
- 为 worker、Portal 或无人值守定时运行启用浏览器——它们的网络出口与沙箱问题
  尚未解决（见第 7 节）。
- 可见的工作区内浏览器 Tab，或仅为增加一个这样的 Tab 而把 Desktop 迁到
  Electron/Wails v3。

## 3. 同类产品如何实现

这些产品证明了此工作流有价值且已发货，因此 BuildMax 无需再验证需求。它们的
**浏览器宿主**不同，其中一点差异对我们重要：VS Code 的共享 `WebContentsView`
模型是围绕"用户和 Agent 观看同一个 Tab"构建的。由于 BuildMax 不把人类接管
当作第一阶段的必要条件，那套共享页面模型对我们是先例，不是架构模板。我们的
参照点是"Agent 依据观察到的渲染状态行动"，headless 控制器在任何界面上都能
满足它。公开文档支持下表的区分，并未揭示每一项私有实现细节。

| 产品 | 浏览器宿主与 Agent 控制 | 重要边界 |
|---|---|---|
| ChatGPT Desktop 中的 Codex | 内置浏览器使用与日常浏览器分开的 Profile；Computer Use 可以打开、点击、输入、检查和截图。开发者模式可在批准后开放完整 CDP；扩展可连接支持的外部浏览器。 | Codex CLI 和 IDE 扩展没有这个内置浏览器。网站访问与敏感操作分别批准。[OpenAI 官方文档][openai-browser] |
| Claude Code | Claude in Chrome 是可从 Claude Code 使用的 Chrome 扩展。它能读取/操作页面，并向开发流程提供 DOM、网络与控制台信息。扩展声明 `debugger`、`scripting` 和 Tab 权限；Chrome 将 `debugger` 说明为 CDP 传输。 | 这是借助扩展控制 Chrome，不能据此推断 Claude Code CLI 嵌入了浏览器引擎。Claude Cowork 另有已公开的内置浏览器。[Anthropic][claude-chrome]、[Chrome][chrome-debugger] |
| VS Code | 开源集成浏览器是 Electron `WebContentsView`。主进程拥有页面与 Session；共享进程运行 Playwright，经 CDP 代理访问页面；工作台持有状态镜像。 | Agent 创建的页面相互隔离。用户打开的 Tab 在显式共享前对 Agent 保持私有；这套共享模型正是为人类共同观看服务的。[架构源码][vscode-architecture]、[用户文档][vscode-tools] |

共同模式是：真实浏览器引擎持有渲染状态；控制器提供有界的导航、观察与交互；
Agent 循环接收工具结果；在有 UI 的地方，它可以向用户呈现同一个页面和权限
状态。抓取工具没有持久的渲染页面，单独的 `iframe` 也没有 Agent 控制器。

## 4. BuildMax 当前基础

- 共享的 Go `agentapp` 运行时同时支撑 CLI 与 Desktop 的本地聊天。Desktop 是
  Wails v2/React 外壳，其 Go 桥接层向前端发送工具和审批事件；CLI 驱动同一个
  运行时但没有这层桥接。因此加到 `agentapp` 的运行时能力可同时到达两个界面。
  见 [Desktop 架构](../contribute/architecture/desktop.md)、
  [`internal/interface/desktop/app.go`](../../../internal/interface/desktop/app.go) 与
  [`internal/interface/desktop/approval.go`](../../../internal/interface/desktop/approval.go)。
- `agentapp` 已按"存在与否"装配可选能力：在 `buildBaseTools` 中，artifact
  publisher 为 nil、jobs 管理器为 nil 或使用 noop 沙箱时，会直接省略对应工具
  或参数，而不是提供一个只会返回"不可用"的工具。可选浏览器能力沿用同一规则。
  见 [`internal/agentapp/assembly.go`](../../../internal/agentapp/assembly.go)。
- 基础工具包含 `WebFetch`、`WebSearch`，Desktop 还启用 MCP。它们都不拥有
  浏览器页面，也不提供 DOM、截图、控制台或交互工具。见
  [`internal/agentapp/assembly.go`](../../../internal/agentapp/assembly.go) 与
  [`internal/agentapp/app.go`](../../../internal/agentapp/app.go)。
- `llm.MultimodalTool` 与图片 `ContentPart` 已能通过 Agent 循环和模型适配器
  承载截图。浏览器需要有界的图片生成与展示，而不必重新设计通用消息格式。见
  [`internal/core/llm/tool.go`](../../../internal/core/llm/tool.go) 与
  [`internal/core/llm/llm.go`](../../../internal/core/llm/llm.go)。
- 工具注册表按模型缓存，可被多个 Session 复用。运行上下文携带 Session ID；
  每次浏览器操作都必须按这个 ID 检查页面归属，不能以任何前端当前选中的 Tab
  为准。见 [`internal/agentapp/app.go`](../../../internal/agentapp/app.go) 与
  [`internal/core/session`](../../../internal/core/session)。
- Desktop 工作区已有聊天、终端、文件与 diff 的 Tab/Pane 状态模型。它以后可以
  显示浏览器元数据或原生浏览器视图，但增加 `browser` Tab 类型本身不会产生
  可控浏览器，headless 的第一阶段也不需要它。见
  [`desktop/frontend/src/lib/tabs.js`](../../../desktop/frontend/src/lib/tabs.js)。
- 当前权限解析可以在写操作前询问用户，并借助 `llm.GrantScoper` 限定一次
  Session 授权的范围。Desktop 目前提供通用工具审批：允许一次、允许本次
  Session、拒绝；CLI 有自己的审批路径。浏览器来源准入在发起提示的那个界面上
  仍需要明确语义；批准某一个来源，不应顺带授权所有网站。见
  [`internal/core/agent/agent.go`](../../../internal/core/agent/agent.go)、
  [`internal/core/llm/tool.go`](../../../internal/core/llm/tool.go) 与
  [`desktop/frontend/src/components/ApprovalPanel.jsx`](../../../desktop/frontend/src/components/ApprovalPanel.jsx)。

Wails 复用各平台原生 WebView，不自带浏览器引擎。它的 `WindowExecJS` 针对
应用窗口，而不是独立的跨平台 CDP 浏览器目标。React `iframe` 也不是可取的
捷径：网站可用 CSP `frame-ancestors` 禁止嵌入，页面控制和 Wails Go 绑定的
边界仍未解决。这些判断基于 [Wails 架构][wails-intro]、
[窗口运行时][wails-window]与 [CSP 说明][mdn-frame-ancestors]；它们只关系到
将来的应用内视图，与 headless 控制器无关。

## 5. 技术方案

| 方案 | 能验证或交付什么 | 主要成本或缺口 | 判断 |
|---|---|---|---|
| Go 自持 Chromium，经 CDP 操作（支持 headless） | 一个任何本地界面都能驱动的真实页面；Go 拥有 Session 映射、工具与生命周期，运行时不需要 Node；Desktop 可选地把它显示出来 | 需要发现或分发浏览器、保护 CDP、隔离 Profile，并验证跨平台打包 | 首选的首个产品原型 |
| 现有浏览器 MCP Server | 用当前 Desktop MCP 网关快速试一下工具人机工程 | 同类产品已证明工作流价值，因此这不再需要用来验证有用性；且 BuildMax 不会拥有 Profile 隔离、生命周期与一致的审批体验 | 可选，不在主路径上 |
| Wails WebView/React `iframe` | 在现有 UI 内显示允许嵌入的页面 | 并非通用浏览器；网站可能禁止 frame；不同原生引擎需要不同自动化路径；不可信内容靠近应用绑定会产生信任问题 | 不用作 Agent 浏览器后端 |
| 原生内嵌 Chromium 视图或 Electron/CEF 外壳 | 最接近 VS Code 式的应用内共享页面 | 外壳集成、进程模型、焦点与浮层、包体、跨平台分发和迁移成本 | 仅在 headless 原型证明存在对应用内视图的需求后重估 |
| 用户浏览器的 Chrome 扩展 | 可访问已有登录状态和熟悉的 Chrome UI | 扩展权限、分发、本机通信与更广泛的个人数据授权 | 独立的后续能力；本地应用验证不依赖它 |

Go CDP 客户端（例如 [chromedp][chromedp]）是可考虑的实现依赖，尚未选型。
CDP 提供导航、DOM/无障碍状态、输入、截图与控制台事件
（[协议参考][cdp]）。控制器可使用支持的已安装 Chrome/Edge，或固定版本的
Chrome for Testing。[Chrome for Testing][chrome-for-testing] 面向自动化；
[Playwright 浏览器文档][playwright-browsers]也展示了所有依赖浏览器的方案
都要解决的打包与版本问题。

## 6. 候选架构

```text
CLI 界面 -------------------------\
                                   >-- agentapp 装配可选 Browser 工具
Desktop 界面 --- Wails 事件 ------/           |
   （可选可见窗口 + 状态）                     |
                                     浏览器控制器（infra，Go）
                                              |
                                     隔离 Chromium + CDP
                                              ^
Agent 循环 -> 按 Session 限定的控制器（归属以运行的 Session ID 为键）
```

1. **所有权。** 浏览器控制器是 Go 里的基础设施关注点。它负责进程启停、Profile
   路径、CDP 传输，以及 BuildMax Session ID 到浏览器上下文/页面的映射。无论
   哪个界面发起了运行，它都是权威状态。Agent 工具调用从运行上下文取得
   Session ID。
2. **装配与启用。** `agentapp` 仅在可选能力存在时注册浏览器工具，沿用
   `buildBaseTools` 现有的"按存在装配"规则。**哪些界面启用它是一条独立的策略
   决定。** 首先在本地运行（CLI 与 Desktop）以用户自身权限启用，与
   [Agent 桥接 CLI](../design/Agent 桥接 CLI.md) 中本地 CLI 的信任姿态一致。
   worker、Portal 和定时运行在出口沙箱问题解决前保持关闭。工具名由
   [`internal/tool/names.go`](../../../internal/tool/names.go) 统一管理。原型
   阶段再决定首版是一件按 action 分支的工具，还是少数几件聚焦的工具；不应为
   每个 CDP 命令建立一件工具。
3. **观察。** 优先返回有大小上限的无障碍/DOM 快照，元素引用只在当前页面
   版本内有效；需要视觉判断时再截图。每次观察附带 URL、标题、视口和快照
   版本。导航或 DOM 替换后，交互应拒绝过期引用，而非猜测目标。网页文字与
   控制台摘录进入模型上下文及 trace 前必须截断。
4. **交互。** 首个旅程只需要明确的小集合：导航、检查、点击、输入、截图和
   最近的控制台错误。每次调用都检查页面归属、当前来源、权限、超时和取消。
   在显示可见窗口的地方，用户操作可能改变页面；此后 Agent 必须重新观察。
5. **展示（Desktop，可选）。** Desktop 可通过 Wails 事件镜像页面 URL、标题、
   加载状态、归属和审批状态，并显示浏览器自身的可见窗口，用简洁活动标记把
   当前 Session 与页面关联。若以后采用工作区内嵌方式，原生页面必须仍是 CDP
   操作的同一个页面；静态截图 Tab 只能算预览，不等于交互式内置浏览器。CLI
   没有展示层，也不需要。

这符合仓库的依赖方向：浏览器进程控制属于基础设施；`agentapp` 装配可选工具；
Desktop 界面只负责其可选可见窗口的生命周期以及将来可能的页面共享授权；core
保留现有工具与消息契约。原型应先证明验证流程所需的最小接口，再把新抽象
固定下来。

## 7. 信任与故障边界

| 边界 | 候选规则与原因 |
|---|---|
| Profile 与身份 | 始终用独立 user-data 目录启动，不连接 Chrome 默认 Profile。Chrome 136+ 有意禁止对默认目录远程调试，并建议使用非默认目录。[Chrome 安全说明][chrome-remote-debugging] |
| 页面与 Session 归属 | Agent 创建的页面只归该 BuildMax Session，每次操作都以运行的 Session ID 校验。"用户打开 Tab 再共享"的边界仅在 Desktop 显示共享窗口时才适用，属于 Desktop 后续的关注点，不在 headless 第一阶段之内。[VS Code 文档][vscode-tools] |
| 网站访问 | 导航前及重定向后检查规范化来源。工具级 Session 授权不等于所有来源的许可。首版可把交互限定于本地开发来源，同时评估公开网站场景。 |
| Agent 输入 | 页面文字、ARIA 名称、控制台输出与截图均为不可信数据。Agent 不能把网页里的指令当作用户指令。登录与后果较大的操作需要单独由用户决定；仅凭一个点击选择器无法可靠判断是否无害。 |
| 应用桥接 | 在存在展示层的地方，不可信页面不能获得 Wails Go 绑定、BuildMax 认证 Token 或 React 应用状态的直接访问权。浏览器自动化留在 Go 控制器中，只向 Agent 返回有界结果。 |
| CDP 传输 | 若选定驱动支持私有进程传输，应优先使用。若使用调试端口，只绑定回环地址、选新端口，不把地址交给网页或写入 trace，并随浏览器进程关闭。CDP 足以检查 Cookie 与页面内部状态。 |
| 网络可达性 | 浏览器导航许可不是完整的出站网络沙箱：已加载页面可请求子资源，浏览器也拥有本机网络能力。在本地运行中，这一出口以用户自身权限被接受，与本地 CLI 的信任模型一致；它**不是** worker 级隔离——这正是 worker、Portal 与定时运行在第一阶段保持关闭的原因。 |
| 下载与文件 | 首个工具契约禁用 Agent 驱动的上传/下载，以及 `file:`、`javascript:`、`data:` 和浏览器内部地址导航。以后启用时，每项都需要独立的文件与审批边界。 |
| 故障与恢复 | 浏览器崩溃、CDP 断连、网站拦截、超时或过期元素都是 Agent 可诊断的工具错误。关闭与取消必须释放进程和临时 Profile。绝不能悄悄重连到另一浏览器/Profile。 |

来源准入在发起提示的那个界面上仍需要明确语义。在 Desktop 上，现有审批面板
可作为 UI 起点，但仅展示 `Browser` 与原始参数不足以表达网站授权：审批应显示
准确来源、请求的操作、当前页面，以及授权是一次还是本 Session 有效。更丰富的
持久允许列表需要另行决策，不能成为 `ApprovalAllowSession` 的意外副作用。

## 8. 原型实验与所需证据

### 范围明确的原型

使用隔离测试 Profile，在随机回环端口运行小型本地 Web 应用。应用包含有状态
表单、路由变化和一个故意产生的控制台错误。Agent 旅程，headless（或在
Desktop）运行：

1. Agent 启动或发现本地 Server，打开路由。
2. 它读取有界页面快照，在表单中输入并点击，然后报告结果状态、URL 和控制台
   错误。
3. 分别取消运行、关闭 Session、关闭进程，确认每个边界的浏览器进程与临时
   Profile 都得到清理。

若测试可选的 Desktop 窗口，则追加：显示可见窗口，让用户中途介入一次，确认
Agent 下一步重新观察，而非继续使用过期元素引用。

### 采纳方向前需要的证据

同类产品已证明工作流的有用性，因此原型衡量的是架构、可移植性与信任，而非
需求。

- **正确性：** 导航、重定向、跨来源变化、过期元素、对话框、新 Tab、控制台
  错误、崩溃和取消都有明确结果；任何调用都不能使用另一个 Session 的页面。
- **信任：** 页面无法调用 Wails 绑定（在存在展示层的地方）、获知 CDP 访问
  细节、读取另一浏览器 Profile，或凭某站授权取得所有网站的权限。故意加入
  恶意网页指令，验证其仅作为数据处理。
- **运行（主要风险）：** 在受支持的 macOS、Windows、Linux 目标上运行原型，
  记录已安装浏览器发现、启动时间、内存、包体、版本兼容和失败提示。仅有
  macOS 原型是有用证据，但不等于证明可移植交付；打包正是大部分交付成本与
  风险所在。
- **验证：** Go 单元测试覆盖 Session/来源/操作边界与清理；真实浏览器本地
  集成旅程覆盖 CDP 行为；仅当构建了可选的 Desktop 展示层时，才需要 Desktop
  桥接与 UI 检查；若更改已发布 Wails 包，再提供打包应用的启动证据。开工时按
  [测试指引](../contribute/testing.md)与 `./make help` 选实际命令。

本报告没有修改产品代码，也没有运行浏览器原型。以上是外部资料核查与仓库
代码阅读的结论，并非对 BuildMax 性能的实测或已验证的安全边界。

## 9. 开放问题与后续归宿

1. 主要任务是**本地 Web 应用验证**，还是首版就需要操作已登录的第三方网站？
   后者会改变 Profile、登录、权限与后果较大操作的要求。
2. 首先在哪些界面启用该能力——CLI、Desktop 还是两者——以及 Desktop 首版就
   发布可见窗口，还是先保持 headless 加一个状态指示，等观察证据支持再上
   窗口？
3. 哪种浏览器可执行文件属于支持契约：发现系统 Chrome/Edge、按需管理
   Chrome for Testing，还是随应用打包？安装、更新、许可检查与离线行为取决
   于此。
4. 浏览器状态是每个 Session 临时存在、跨该 Session 的多个 turn 保留，还是
   跨重启保留？应从观察到的旅程所需的最少状态开始，不要悄悄建立第二套持久
   Session 存储。
5. 哪些权限归现有工具策略，哪些属于浏览器特有的网站/页面决策？原型应同时
   覆盖两者，不把工具授权当成来源授权。

若方向获采纳，应在[路线图](../ROADMAP.md)记录优先级，把已确定的架构和信任
理由移入带必需中文镜像的 `docs/design/` 记录，并在实施工作分解充分、依赖
完成后创建就绪 backlog 项。用户可见的浏览器行为与设置随后写入 manual/
reference 文档。作出决定后撤下本提案。

## 10. 外部资料

以下产品与技术资料均于 2026-09-21 核对。公开产品文档可证明公开的使用行为；
只有 VS Code 源码较详细地描述了其内部架构。

- [OpenAI 官方文档：Browser][openai-browser]
- [Anthropic：Get started with Claude in Chrome][claude-chrome]
- [Chrome 扩展：debugger API][chrome-debugger]
- [VS Code：集成浏览器架构源码][vscode-architecture]
- [VS Code：Agent 浏览器工具][vscode-tools]
- [Wails v2：简介][wails-intro]与[窗口运行时][wails-window]
- [Chrome DevTools Protocol 参考][cdp]
- [chromedp 仓库][chromedp]
- [Chrome：远程调试 Profile 改动][chrome-remote-debugging]
- [Chrome for Testing][chrome-for-testing]
- [Playwright：浏览器二进制与安装][playwright-browsers]
- [MDN：CSP `frame-ancestors`][mdn-frame-ancestors]

[openai-browser]: https://learn.chatgpt.com/docs/browser
[claude-chrome]: https://support.claude.com/en/articles/12012173-get-started-with-claude-in-chrome
[chrome-debugger]: https://developer.chrome.com/docs/extensions/reference/api/debugger
[vscode-architecture]: https://github.com/microsoft/vscode/blob/main/.github/skills/integrated-browser/SKILL.md
[vscode-tools]: https://code.visualstudio.com/docs/agents/run/browser-tools
[wails-intro]: https://v2.wails.io/docs/introduction/
[wails-window]: https://v2.wails.io/docs/reference/runtime/window/
[mdn-frame-ancestors]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Content-Security-Policy/frame-ancestors
[cdp]: https://chromedevtools.github.io/devtools-protocol/
[chromedp]: https://github.com/chromedp/chromedp
[chrome-remote-debugging]: https://developer.chrome.com/blog/remote-debugging-port
[chrome-for-testing]: https://developer.chrome.com/docs/automation-and-testing/chrome-for-testing
[playwright-browsers]: https://playwright.dev/docs/browsers
