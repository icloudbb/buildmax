# Desktop 浏览器能力技术调研

> **翻译说明：** 本文是[英文原文](../../proposals/desktop-browser-capability.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
>
> **受众：** 贡献者与产品设计者 · **状态：** 提案，讨论中
>
> **讨论开始：** 2026-09-21 · **证据核对：** 2026-09-21

相关文档：[路线图](../ROADMAP.md)、[当前状态](../current-state.md)、
[Desktop 架构](../contribute/architecture/desktop.md)、
[工具架构](../contribute/architecture/tools.md)、
[工具权限](../design/工具权限.md)、
[沙箱边界](../design/沙箱边界.md)，以及
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

BuildMax 已能抓取和搜索网页内容，缺少的是一个**共享的真实渲染页面**：
Desktop 用户能看到它，Agent 能在同一次运行中检查并操作它。最先值得验证的
用户结果是本地 Web 应用闭环：启动应用、打开路由、走一段用户流程、读取结果
与控制台错误、修复问题，再次执行流程。

候选的首个实现是由 Desktop 管理一个使用隔离 Profile 的可见 Chromium 进程，
由 Go 通过 Chrome DevTools Protocol（CDP）驱动。页面最初可以出现在独立
浏览器窗口中，这足以验证 Agent 与浏览器之间的契约，而无需先更换 Desktop
外壳。让**同一个可操作页面**显示在 Desktop 工作区 Tab 内，是另一项产品和
原生视图决策。本文是调研与候选方向，不是已采纳架构，也不是路线图承诺。

现在要决定的是：本地验证旅程是否值得拥有一个由 Desktop 管理的浏览器。
**可工作的原型完成后**再决定：工作区内嵌浏览器带来的价值，是否足以承担
第二个原生视图、浏览器引擎分发、焦点与布局处理，以及更大的安全面。同类
产品的实现并不能证明 Wails v2 会开箱提供 BuildMax 所需的第二步。

## 2. 用户结果与范围

### 核心用户结果

用户要求 Desktop Agent 验证正在运行的本地 Web 应用变更。用户能看到 Agent
使用的页面，能接手检查，并收到准确的路由、交互步骤、观察结果和未覆盖范围。
Agent 必须依据实际观察到的页面状态行动，而不能仅凭源代码或 HTTP 响应推断
成功。

[VS Code 浏览器工具][vscode-tools]和
[Claude Code 与 Claude in Chrome][claude-chrome]都记录了类似流程。这证明此类
工作流有用途，却不能证明 BuildMax 用户需要一个功能齐全的同款浏览器产品。
应以范围明确的本地原型检验这一假设。

### 第一阶段目标

- 打开 `http://localhost` 或其他经明确批准的 HTTP(S) 来源。
- 返回有大小上限的当前页面快照，标识可交互元素；基于这些元素点击、输入，
  并拒绝已经过期的引用。
- 从同一页面获取截图和最近的控制台错误。
- 用户能在可见的浏览器窗口中观看并介入。
- 浏览器状态与用户常用浏览器 Profile、其他 BuildMax Session 隔离。
- 取消运行时及时停止操作；Desktop 退出时关闭自身管理的资源。

### 第一阶段非目标

- 自动操作用户日常 Chrome Profile，或悄悄继承其中的 Cookie、密码、历史和
  Tab。
- 向 Agent 提供任意 JavaScript 执行、原始 CDP、文件上传/下载，或不受限制的
  已登录交易操作。
- 替代 `WebSearch`、`WebFetch`，成为通用网页研究工具。
- 默认让 CLI、worker、Portal 或无人值守定时运行拥有浏览器。
- 仅为增加浏览器 Tab 而把 Desktop 迁到 Electron 或 Wails v3。

## 3. 同类产品如何实现

这些产品都有相似的 **Agent 闭环**，但浏览器宿主不同。公开文档支持下表的
区分，并未揭示每一项私有实现细节。

| 产品 | 浏览器宿主与 Agent 控制 | 重要边界 |
|---|---|---|
| ChatGPT Desktop 中的 Codex | 内置浏览器使用与日常浏览器分开的 Profile；Computer Use 可以打开、点击、输入、检查和截图。开发者模式可在批准后开放完整 CDP；扩展可连接支持的外部浏览器。 | Codex CLI 和 IDE 扩展没有这个内置浏览器。网站访问与敏感操作分别批准。[OpenAI 官方文档][openai-browser] |
| Claude Code | Claude in Chrome 是可从 Claude Code 使用的 Chrome 扩展。它能读取/操作页面，并向开发流程提供 DOM、网络与控制台信息。扩展声明 `debugger`、`scripting` 和 Tab 权限；Chrome 将 `debugger` 说明为 CDP 传输。 | 这是借助扩展控制 Chrome，不能据此推断 Claude Code CLI 嵌入了浏览器引擎。Claude Cowork 另有已公开的内置浏览器。[Anthropic][claude-chrome]、[Chrome][chrome-debugger] |
| VS Code | 开源集成浏览器是 Electron `WebContentsView`。主进程拥有页面与 Session；共享进程运行 Playwright，经 CDP 代理访问页面；工作台持有状态镜像。 | Agent 创建的页面相互隔离。用户打开的 Tab 在显式共享前对 Agent 保持私有。[架构源码][vscode-architecture]、[用户文档][vscode-tools] |

共同模式是：真实浏览器引擎持有渲染状态；控制器提供有界的导航、观察与交互；
Agent 循环接收工具结果；UI 向用户呈现同一个页面和权限状态。抓取工具没有
持久的渲染页面，单独的 `iframe` 也没有 Agent 控制器。

## 4. BuildMax 当前基础

- Desktop 是 Wails v2/React 外壳，本地聊天通过共享 Go `agentapp` 运行时执行。
  Go 桥接层向前端发送工具和审批事件。见
  [Desktop 架构](../contribute/architecture/desktop.md)、
  [`internal/interface/desktop/app.go`](../../../internal/interface/desktop/app.go) 与
  [`internal/interface/desktop/approval.go`](../../../internal/interface/desktop/approval.go)。
- 基础工具包含 `WebFetch`、`WebSearch`，Desktop 还启用 MCP。它们都不拥有
  浏览器页面，也不提供 DOM、截图、控制台或交互工具。见
  [`internal/agentapp/assembly.go`](../../../internal/agentapp/assembly.go) 与
  [`internal/agentapp/app.go`](../../../internal/agentapp/app.go)。
- `llm.MultimodalTool` 与图片 `ContentPart` 已能通过 Agent 循环和模型适配器
  承载截图。浏览器需要有界的图片生成与展示，而不必重新设计通用消息格式。见
  [`internal/core/llm/tool.go`](../../../internal/core/llm/tool.go) 与
  [`internal/core/llm/llm.go`](../../../internal/core/llm/llm.go)。
- Desktop 工作区已有聊天、终端、文件与 diff 的 Tab/Pane 状态模型。它能显示
  浏览器元数据或将来的原生浏览器视图，但增加 `browser` Tab 类型本身不会
  产生可控浏览器。见
  [`desktop/frontend/src/lib/tabs.js`](../../../desktop/frontend/src/lib/tabs.js)。
- 工具注册表按模型缓存，可被多个 Session 复用。运行上下文携带 Session ID；
  每次浏览器操作都必须按这个 ID 检查页面归属，不能以当前前端选中的 Tab 为准。
  见 [`internal/agentapp/app.go`](../../../internal/agentapp/app.go) 与
  [`internal/core/session`](../../../internal/core/session)。
- 当前权限解析可以在写操作前询问用户，并借助 `llm.GrantScoper` 限定一次
  Session 授权的范围。Desktop 目前提供通用工具审批：允许一次、允许本次
  Session、拒绝。浏览器来源准入和页面共享仍需要明确语义；批准一种浏览器
  操作，不应顺带授权所有网站或页面。见
  [`internal/core/agent/agent.go`](../../../internal/core/agent/agent.go)、
  [`internal/core/llm/tool.go`](../../../internal/core/llm/tool.go) 与
  [`desktop/frontend/src/components/ApprovalPanel.jsx`](../../../desktop/frontend/src/components/ApprovalPanel.jsx)。

Wails 复用各平台原生 WebView，不自带浏览器引擎。它的 `WindowExecJS` 针对
应用窗口，而不是独立的跨平台 CDP 浏览器目标。React `iframe` 也不是可取的
捷径：网站可用 CSP `frame-ancestors` 禁止嵌入，页面控制和 Wails Go 绑定的
边界仍未解决。这些判断基于 [Wails 架构][wails-intro]、
[窗口运行时][wails-window]与 [CSP 说明][mdn-frame-ancestors]；它们并不意味着
应用内视图不可能实现。

## 5. 技术方案

| 方案 | 能验证或交付什么 | 主要成本或缺口 | 判断 |
|---|---|---|---|
| 现有浏览器 MCP Server | 用当前 Desktop MCP 网关快速试验，无需产品代码 | 用户必须自行配置外部 Server/运行时；BuildMax 不拥有一致的页面可见性、Profile 与审批体验 | 仅用于工作流探针 |
| Desktop 管理 Chromium，经 CDP 操作 | 用户和 Agent 共享同一个真实可见页面；Go 拥有 Session 映射、工具与生命周期，运行时不需要 Node | 需要发现或分发浏览器、保护 CDP、隔离 Profile，并验证跨平台打包 | 首选的首个产品原型 |
| Wails WebView/React `iframe` | 在现有 UI 内显示允许嵌入的页面 | 并非通用浏览器；网站可能禁止 frame；不同原生引擎需要不同自动化路径；不可信内容靠近应用绑定会产生信任问题 | 不用作 Agent 浏览器后端 |
| 原生内嵌 Chromium 视图或 Electron/CEF 外壳 | 最接近 VS Code 的应用内共享页面 | 外壳集成、进程模型、焦点与浮层、包体、跨平台分发和迁移成本 | 仅在外置窗口原型证明需求后重估 |
| 用户浏览器的 Chrome 扩展 | 可访问已有登录状态和熟悉的 Chrome UI | 扩展权限、分发、本机通信与更广泛的个人数据授权 | 独立的后续能力；本地应用验证不依赖它 |

Go CDP 客户端（例如 [chromedp][chromedp]）是可考虑的实现依赖，尚未选型。
CDP 提供导航、DOM/无障碍状态、输入、截图与控制台事件
（[协议参考][cdp]）。控制器可使用支持的已安装 Chrome/Edge，或固定版本的
Chrome for Testing。[Chrome for Testing][chrome-for-testing] 面向自动化；
[Playwright 浏览器文档][playwright-browsers]也展示了所有依赖浏览器的方案
都要解决的打包与版本问题。

## 6. 候选架构

```text
Desktop React UI <--- Wails 事件/绑定 ---> Desktop App
                                           |
                                      Go 浏览器管理器
                                           |
                                  隔离 Chromium + CDP
                                           ^
Agent 循环 -> 仅 Desktop 注册的 Browser 工具 -> 按 Session 限定的控制器
```

1. **所有权。** Desktop 进程负责启动浏览器、关闭进程、管理 Profile 路径，
   并维护 BuildMax Session ID 到浏览器上下文/页面的映射。Agent 工具调用从
   运行上下文取得 Session ID。Go 管理器是权威状态；React 通过 Wails 事件
   镜像 URL、标题、加载状态、归属和审批状态。
2. **工具装配。** `agentapp` 接收 Desktop 提供的可选浏览器能力，仅在可用时
   注册工具，沿用现有按界面限定工具的规则。CLI 与 worker 不会得到一个
   永远返回不可用的工具，也不会启动浏览器进程。工具名由
   [`internal/tool/names.go`](../../../internal/tool/names.go) 统一管理。
   原型阶段再决定首版是一件按 action 分支的工具，还是少数几件聚焦的工具；
   不应为每个 CDP 命令建立一件 Agent 工具。
3. **观察。** 优先返回有大小上限的无障碍/DOM 快照，元素引用只在当前页面
   版本内有效；需要视觉判断时再截图。每次观察附带 URL、标题、视口和
   快照版本。导航或 DOM 替换后，交互应拒绝过期引用，而非猜测目标。网页
   文字与控制台摘录进入模型上下文及 trace 前必须截断。
4. **交互。** 首个旅程只需要明确的小集合：导航、检查、点击、输入、截图和
   最近的控制台错误。每次调用都检查页面归属、当前来源、权限、超时和取消。
   用户可直接操作可见页面；此后 Agent 必须重新观察。
5. **展示。** 初期使用浏览器自身的可见窗口，Desktop 内以简洁活动标记把
   当前 Session 与页面关联。若以后采用工作区内嵌方式，原生页面必须仍是
   CDP 操作的同一个页面。静态截图 Tab 只能算预览，不等于交互式内置浏览器。

这符合仓库的依赖方向：浏览器进程控制属于基础设施；`agentapp` 装配可选
工具；Desktop 界面负责用户可见的生命周期和授权；core 保留现有工具与消息
契约。原型应先证明所需的最小接口，再把新抽象固定下来。

## 7. 信任与故障边界

| 边界 | 候选规则与原因 |
|---|---|
| Profile 与身份 | 始终用独立 user-data 目录启动，不连接 Chrome 默认 Profile。Chrome 136+ 有意禁止对默认目录远程调试，并建议使用非默认目录。[Chrome 安全说明][chrome-remote-debugging] |
| 页面可见性 | Agent 创建的页面只归该 BuildMax Session。用户打开的页面，在显式共享前不交给 Agent；共享可撤销。VS Code 提供了这一边界的例子，BuildMax 仍须自行实现。[VS Code 文档][vscode-tools] |
| 网站访问 | 导航前及重定向后检查规范化来源。工具级 Session 授权不等于所有来源的许可。首版可把交互限定于本地开发来源，同时评估公开网站场景。 |
| Agent 输入 | 页面文字、ARIA 名称、控制台输出与截图均为不可信数据。Agent 不能把网页里的指令当作用户指令。登录与后果较大的操作需要单独由用户决定；仅凭一个点击选择器无法可靠判断是否无害。 |
| 应用桥接 | 不可信页面不能获得 Wails Go 绑定、BuildMax 认证 Token 或 React 应用状态的直接访问权。浏览器自动化留在 Go 控制器中，只向 Agent 返回有界结果。 |
| CDP 传输 | 若选定驱动支持私有进程传输，应优先使用。若使用调试端口，只绑定回环地址、选新端口，不把地址交给网页或写入 trace，并随浏览器进程关闭。CDP 足以检查 Cookie 与页面内部状态。 |
| 网络可达性 | 浏览器导航许可不是完整的出站网络沙箱：已加载页面可请求子资源，浏览器也拥有本机网络能力。不能把第一版原型描述成 worker 级隔离。 |
| 下载与文件 | 首个工具契约禁用 Agent 驱动的上传/下载，以及 `file:`、`javascript:`、`data:` 和浏览器内部地址导航。以后启用时，每项都需要独立的文件与审批边界。 |
| 故障与恢复 | 浏览器崩溃、CDP 断连、网站拦截、超时或过期元素都是 Agent 可诊断的工具错误。关闭与取消必须释放进程和临时 Profile。绝不能悄悄重连到另一浏览器/Profile。 |

现有 Desktop 审批面板可作为 UI 起点，但仅展示 `Browser` 与原始参数，无法
清楚表达网站授权。审批应显示准确来源、请求的操作、当前页面，以及授权是
一次还是本 Session 有效。更丰富的持久允许列表需要另行决策，不能成为
`ApprovalAllowSession` 的意外副作用。

## 8. 原型实验与所需证据

### 范围明确的原型

使用隔离测试 Profile，在随机回环端口运行小型本地 Web 应用。应用包含有状态
表单、路由变化和一个故意产生的控制台错误。用户旅程如下：

1. Desktop Agent 启动或发现本地 Server，在可见浏览器里打开路由。
2. 它读取有界页面快照，在表单中输入并点击，然后报告结果状态、URL 和
   控制台错误。
3. 用户中途介入一次；Agent 下一步重新观察，而非继续使用过期元素引用。
4. 分别取消运行、关闭 Session、关闭 Desktop，确认每个边界的浏览器进程与
   临时 Profile 都得到清理。

### 采纳方向前需要的证据

- **效用：** 一项可复现的修复与验证任务依据实际浏览器观察完成；工具调用
  与截图可在 Desktop 对话中审阅。与仅使用 `WebFetch`/`WebSearch` 的同一
  任务比较。
- **正确性：** 导航、重定向、跨来源变化、过期元素、对话框、新 Tab、控制台
  错误、崩溃和取消都有明确结果；任何调用都不能使用另一个 Session 的页面。
- **信任：** 页面无法调用 Wails 绑定、获知 CDP 访问细节、读取另一浏览器
  Profile，或凭某站授权取得所有网站的权限。故意加入恶意网页指令，验证其
  仅作为数据处理。
- **运行：** 在受支持的 macOS、Windows、Linux 目标上运行原型，记录已安装
  浏览器发现、启动时间、内存、包体、版本兼容和失败提示。仅有 macOS 原型
  是有用证据，但不等于证明可移植交付。
- **验证：** Go 单元测试覆盖 Session/来源/操作边界与清理；真实浏览器本地
  集成旅程覆盖 CDP 行为；Desktop 桥接和 UI 检查覆盖绑定、事件、审批；
  若更改已发布 Wails 包，再提供打包应用的启动证据。开工时按
  [测试指引](../contribute/testing.md)与 `./make help` 选实际命令。

本报告没有修改产品代码，也没有运行浏览器原型。以上是外部资料核查与仓库
代码阅读的结论，并非对 BuildMax 性能的实测或已验证的安全边界。

## 9. 开放问题与后续归宿

1. 主要任务是**本地 Web 应用验证**，还是首版就需要操作已登录的第三方网站？
   后者会改变 Profile、登录、权限与后果较大操作的要求。
2. 第一版接受独立的可见浏览器窗口吗？若不接受，哪项用户观察足以支持在
   验证 Agent/浏览器契约前先实现内嵌原生视图？
3. 哪种浏览器可执行文件属于 Desktop 的支持契约：发现系统 Chrome/Edge、
   按需管理 Chrome for Testing，还是随应用打包？安装、更新、许可检查与
   离线行为取决于此。
4. 浏览器状态是每个 Session 临时存在、跨该 Session 的多个 turn 保留，还是
   跨 Desktop 重启保留？应从观察到的旅程所需的最少状态开始，不要悄悄建立
   第二套持久 Session 存储。
5. 哪些权限归现有工具策略，哪些属于浏览器特有的网站/页面共享决策？原型
   应同时覆盖两者，不把工具授权当成来源授权。

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
