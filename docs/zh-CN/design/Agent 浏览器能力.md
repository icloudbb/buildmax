# Agent 浏览器能力

> **翻译说明：** 本文是[英文原文](../../design/agent-browser-capability.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
>
> **受众：** 贡献者 · **状态：** 活动计划 —— 部分已交付 · **复核日期：** 2026-09-26
>
> **已交付：** CLI headless 工具（#714、#715）、Desktop 可见窗口（#717）、不可信页面
> 测试（#718）、只读的 Desktop 实时视图 tab（#719），以及按来源准入与按来源限定的
> Session 授权（§3）。**尚余：** Linux 与 Windows 可移植性证据、worker/Portal/定时
> 运行（在出口沙箱就绪前关闭）、交互式接管与页面共享，以及托管浏览器下载（§9）。

本记录确定了"给 Agent 一个真实、可控的浏览器"这件事的架构与信任规则；§8 记录了
权衡过的备选方案与同类产品调研。相关文档：
[工具架构](../contribute/architecture/tools.md)、
[工具权限](工具权限.md)、
[沙箱边界](沙箱边界.md)、
[Agent 桥接 CLI](Agent 桥接 CLI.md)，以及
[Desktop 架构](../contribute/architecture/desktop.md)。

## 目录

- [1. 决定与范围](#1-决定与范围)
- [2. 架构](#2-架构)
- [3. 工具契约](#3-工具契约)
- [4. 浏览器发现与生命周期](#4-浏览器发现与生命周期)
- [5. 信任与故障边界](#5-信任与故障边界)
- [6. 验证](#6-验证)
- [7. Desktop 呈现](#7-desktop-呈现)
- [8. 考虑过的备选方案](#8-考虑过的备选方案)
- [9. 延后事项](#9-延后事项)

## 1. 决定与范围

核心结果是让 Agent 能**针对真实渲染的页面验证一处变更**：导航到路由、观察
页面及其控制台、点击和输入，并依据观察到的状态行动，而不是从源代码或 HTTP
响应推断成功。`WebFetch` 返回静态 HTML，无法满足这一点。

这是共享 Agent 运行时的能力，而非某个界面的能力。它首先在 **CLI 上以
headless 方式**交付，由 Go 自持的 Chromium 进程通过 Chrome DevTools
Protocol（CDP）驱动。随后 Desktop 增加了**可见窗口**：以 headful 启动浏览器，
让用户看到 Agent 正在操作的页面，并显示一个把每个 session 关联到其当前页面的
简洁活动指示，并能在工作区 tab 内渲染该页面的只读实时视图（§7）。用户接管与
页面共享仍然延后。同类产品调研（§8）已确立此工作流的价值，因此这项工作证明的
是架构、可移植性与信任——不是需求。

锁定的决策：

- **浏览器来源：** 发现已安装的系统 Chrome/Edge；找不到时返回清晰、可操作的
  错误。托管 Chrome for Testing 下载与打包留待以后。
- **界面：** CLI（headless）与 Desktop（headful，浏览器自己的可见窗口、活动
  指示，以及一个只读 screencast tab）。对页面的交互式接管延后。
- **工具契约：** 几个聚焦工具，一个动词一个——不用单一 action 复用工具，也
  绝不为每个 CDP 命令建一个工具。
- **CDP 客户端：** 选定依赖 [chromedp](https://github.com/chromedp/chromedp)。

## 2. 架构

依赖方向保持不变：浏览器进程控制属于基础设施；`agentapp` 装配可选工具；
`core` 保留现有工具与消息契约，不 import 任何浏览器代码。

- **控制器（`internal/infra/browser`）。** 负责可执行文件发现、进程启停、隔离
  user-data 目录、CDP 传输，以及 BuildMax Session ID 到浏览器上下文及其页面的
  映射。无论哪个界面发起了运行，它都是权威状态。浏览器启动是惰性的：首个
  浏览器工具调用之前不启动进程。
- **端口（`internal/tool`）。** 工具包定义它所依赖的 `BrowserController`
  接口，方式与既有的 `ArtifactPublisher` 相同；`internal/infra/browser`
  实现它。这让 `core`/`tool` 不引入具体 CDP 依赖，测试也可替换为 fake。
- **装配。** 控制器走既有的可选能力漏斗：`AppConfig`
  （`internal/agentapp/app.go`）新增字段，在 `buildAgentApp`
  （`internal/agentapp/app_builder.go`）中拷贝，传入 `buildToolRegistry` 与
  `buildBaseTools`（`internal/agentapp/assembly.go`）。控制器为 nil 时浏览器
  工具**完全省略**，沿用 `publisher != nil` 的先例——只会返回"不可用"的工具
  白白多一次往返，对模型毫无信息。
- **界面启用是策略，与能力本身分离。** CLI 交互与 print 配置传入真实控制器。
  无人值守 worker 配置（`UnattendedWorker: true`）与 delegate/subagent 工作区
  传 nil：worker 在出口沙箱问题解决前保持关闭，subagent 遵循与后台作业相同的
  规则——能力由主运行持有。
- **生命周期。** `AgentApp.Close` 在关闭 jobs 和 worktree 的同时关闭控制器，
  在运行时关闭时释放每个浏览器进程与临时 Profile。

## 3. 工具契约

一个小而明确的集合覆盖首个验证旅程。每个都是普通 `llm.Tool`；截图工具额外
实现 `llm.MultimodalTool`。工具名是 `internal/tool/names.go` 中的常量，必须
出现在[工具架构](../contribute/architecture/tools.md)中；面向用户的条目还要
写入 `manual/tools.md`。

| 工具 | 用途 | 结果 |
|---|---|---|
| `BrowserNavigate` | 打开一个经批准的 HTTP(S) 来源 | 最终 URL、标题、HTTP 状态与加载结果 |
| `BrowserSnapshot` | 有界的无障碍/DOM 快照，元素引用仅在当前页面版本内有效 | 文本快照加 URL、标题、视口与版本 |
| `BrowserClick` | 点击引用的元素 | 新的观察结果，或导航/DOM 替换后的过期引用错误 |
| `BrowserType` | 向引用的元素输入 | 同上 |
| `BrowserScreenshot` | 截取当前页面 | 带一行文字与一个图片 `ContentPart`（base64）的 `ToolResult`，照 MCP 网关 `formatCallToolContent` 模板 |
| `BrowserConsole` | 最近的控制台错误 | 有界、脱敏的摘录 |

每次调用：

- 读取 `session.SessionIDFromContext`，只操作该 Session 的页面；调用未携带
  匹配页面即为错误，绝不使用另一个 Session 的页面；
- 拒绝过期元素引用而非猜测目标；
- 在页面文字与控制台摘录进入模型上下文及 trace 前对其截断；
- 遵守上下文取消与每次调用的超时。

交互工具声明 `AccessWrite`；观察工具声明 `AccessReadOnly`，因此交互走普通审批
路径：交互式会话在 `BrowserNavigate`、`BrowserClick` 或 `BrowserType` 之前询问，
`BrowserNavigate` 的提示会显示请求的 URL。

**来源准入。** `BrowserNavigate` 是准入来源的唯一位置，并且写明确切来源：

- `tool.BrowserOrigin` 是来源的唯一定义：一个带主机的绝对 `http`/`https` URL，
  按浏览器的方式序列化——scheme 与主机小写，省略默认端口，丢弃凭据、路径、
  查询与片段。其他 scheme 在加载任何内容前即被拒绝。
- `BrowserNavigate` 以该来源实现 `llm.GrantScoper`，因此"本 Session 允许"恰好
  覆盖一个来源：`http://localhost:3000/a` 与 `HTTP://LocalHost:3000/b` 共用一个
  授权；`http://localhost:3001`、`https://localhost:3000` 与
  `http://127.0.0.1:3000` 各自会再次询问。TUI 与 Desktop 的审批提示会写明该授权
  将覆盖的来源，`settings.yaml` 规则也可以指定一个来源
  （`BrowserNavigate:<origin>`）。
- 控制器把请求的来源记为该 Session 页面的已准入来源。服务器重定向到别处的来源
  不被准入：提示从未显示过它。

**交互限定在已准入来源。** 在已批准页面上的点击或表单提交可能导航到任何地方。
只要页面不在已准入来源上，控制器就拒绝 `BrowserClick` 与 `BrowserType`，拒绝
信息会告诉模型用该页面的 URL 调用 `BrowserNavigate`——这会让新来源经过它自己的
按来源审批。每个报告页面状态的结果（navigate、snapshot、click、type、
screenshot）都会在页面离开已准入来源时说明。该检查执行两次：一次针对控制器最后
观察到的 URL，一次在 click/type 脚本内部针对实时的 `location.origin`，因此一个在
快照之后自行导航的页面——可能导航到植入了匹配 `data-bm-ref` 的文档——绝不会被
操作。

这里选择限定交互，而不是按来源限定 `BrowserClick`/`BrowserType` 的授权。按来源
限定点击授权需要在 `GrantScope` 中拿到该 Session 的当前页面，而它只能看到参数，
其提示也只会显示一个光秃秃的元素引用；把每个新来源送回 `BrowserNavigate`，复用
的是那个已经显示 URL 的提示，也不增加第二种授权键。有两处限制是有意为之：离开
来源的那次导航在被报告时已经发生——阻止它需要请求拦截；对新页面的观察仍然允许，
因为页面内容是数据（§5）。

## 4. 浏览器发现与生命周期

- **发现。** 依次在各 OS 的惯用位置查找系统 Chrome 或 Chromium，再找 Edge
  （macOS 应用包、Windows 注册表/Program Files 路径、Linux `PATH` 与已知包
  路径）。每个控制器解析一次。找不到时返回一个清晰错误，写明搜索了什么、
  如何安装或指定浏览器；绝不静默降级到另一条自动化路径。
- **Profile。** 始终用临时位置下全新、隔离的 user-data 目录启动，绝不用用户
  默认 Chrome Profile（Chrome 136+ 禁止对默认目录远程调试）。关闭时删除。
- **CDP 传输。** 优先用 chromedp 的管道传输；若使用端口，只绑定回环、选新
  端口，且不把地址交给页面或写入 trace。随进程关闭。
- **状态寿命。** 第一阶段浏览器状态每个 Session 临时存在：不跨 turn 或重启
  持久化，也不建立第二套持久 Session 存储。

## 5. 信任与故障边界

- **页面与 Session 归属**在每次操作时以运行的 Session ID 为键，而非任何前端的
  当前状态。
- **Agent 输入是不可信数据。** 页面文字、ARIA 名称、控制台输出与截图可能包含
  指令；Agent 不能把它们当作用户指令。后果较大的操作需要单独由用户决定。
- **不可触达应用内部。** 控制器只返回有界结果；页面绝不获得进程绑定、凭证或
  CDP 传输细节。（在将来的 Desktop 呈现层上，这延伸到 Wails 绑定与认证
  Token。）
- **网络可达性不是沙箱。** 已加载页面会以本机网络能力发起子资源请求。在本地
  运行中，这一出口以用户自身权限被接受，与 [Agent 桥接 CLI](Agent 桥接 CLI.md)
  中本地 CLI 的信任模型一致。它**不是** worker 级隔离——这正是 worker、Portal
  与定时运行在本阶段保持关闭的原因。
- **首个工具契约禁用：** 任意 JavaScript 执行、原始 CDP、文件上传/下载，以及
  `file:`、`javascript:`、`data:` 和浏览器内部地址导航。以后启用时，每项都需要
  独立的文件与审批边界。
- **故障是可诊断的工具错误。** 崩溃、CDP 断连、网站拦截、超时或过期元素都返回
  有意义的错误。关闭与取消释放进程和临时 Profile；控制器绝不静默重连到另一
  浏览器或 Profile。

## 6. 验证

- Go 单元测试覆盖可执行文件发现、按 Session 限定的归属（跨 Session 调用被
  拒绝）、URL scheme 准入、经由真实 Agent 循环的按来源 Session 授权、限定在来源内
  的交互（包括一个页面在快照后自行重定向的真实浏览器用例）、过期引用拒绝，以及
  取消/关闭时的清理；在不需要真实浏览器处用 fake `BrowserController`。
- 一条真实浏览器本地集成旅程，无 Chrome/Edge 时 skip：提供一个带有状态表单、
  路由变化和一个故意控制台错误的小型本地应用；Agent 打开路由、观察、输入并
  点击、报告状态/URL/控制台错误，然后取消运行并确认清理。
- 一个恶意页面指令测试，证明页面内容作为数据而非权限处理。
- 跨 macOS、Windows、Linux 的运行证据：发现、启动时间、内存与失败提示。单一
  平台结果是证据，不等于可移植交付的证明。
- 从[测试指引](../contribute/testing.md)与 `./make help` 选实际命令。加入
  chromedp 需要干净的 `go mod tidy`、通过的 `go-licenses check`，以及重新生成
  的 `NOTICE-THIRD-PARTY`。

## 7. Desktop 呈现

Desktop 以 headful 启用该能力，浏览器是它自己的可见 OS 窗口，用户可以观看。
控制器接受一个可选的 `Observer`（经 `AppConfig.BrowserObserver` 设置，仅 Desktop
接线），在导航和关闭时上报页面 `Event`。Desktop 把每个事件转发为
`desktop/browser/state` 的 Wails 事件，前端在状态栏显示每个 session 当前页面的
简洁指示，页面释放时清除。事件只携带 URL、标题、session 和 closed 标记——不含
页面内容，不可信页面绝不触达 Go↔前端桥。CLI 不设 observer、保持 headless。

Desktop 还能在工作区 tab 内**内嵌一个实时视图**。Wails v2 无法原生托管真实
Chromium 页面，因此不引入第二个原生引擎，而是用 CDP screencast：当存在帧
observer（`AppConfig.BrowserFrameObserver`，仅 Desktop）时，导航即启动
`Page.startScreencast`，每个 JPEG 帧经 observer 流向 `desktop/browser/frame`
Wails 事件、由一个 `browser` tab 渲染。它展示的是 CDP 控制的**同一个**页面——
静态截图无法满足的不变量——且不需要原生内嵌。本阶段视图只读（帧出、无输入
入）；帧只携带图像和尺寸，绝不含绑定或页面脚本。CLI 不设帧 observer，因此那里
不跑 screencast。

## 8. 考虑过的备选方案

| 方案 | 未被选中的原因 |
|---|---|
| Go 自持的 Chromium 经 CDP | **选定。** 任何本地界面都能驱动的同一个真实页面；Go 负责 Session 映射、工具与生命周期，无需 Node 运行时；Desktop 能展示同一页面。 |
| Wails WebView 或 React `iframe` | 不是通用浏览器：网站会用 CSP [`frame-ancestors`][mdn-frame-ancestors] 拒绝被嵌入，`iframe` 没有 Agent 控制器，各平台原生 WebView 需要各自的自动化路径（[Wails][wails-intro] 复用操作系统 WebView；其 [window runtime][wails-window] 面向应用窗口，而非 CDP 浏览器目标），且不可信内容会紧挨着 Wails Go 绑定。 |
| 现成的浏览器 MCP 服务器 | 可经 MCP 网关试探工具手感，但 BuildMax 无法掌控 Profile 隔离、生命周期或一致的审批体验，而同类产品已证明此工作流的价值。 |
| 原生内嵌 Chromium 或 Electron/CEF 外壳 | 最接近 VS Code 式的应用内共享页面，代价是外壳集成、进程模型、二进制体积与打包。仅当只读 screencast（§7）不够用时再考虑。 |
| 用户浏览器里的 Chrome 扩展 | 能触达已登录的 tab，但需要扩展权限、分发、native messaging，以及范围大得多的个人数据授权。是以后单独的能力，验证本地应用并不需要。 |

**同类产品调研（2026-09-21 核对）。** 同类产品已确立"Agent 依据观察到的渲染页面
行动"有价值且已交付；它们的差别在浏览器宿主。ChatGPT 桌面应用中的 Codex 有一个
内置浏览器，Profile 与用户日常浏览器分离，Codex CLI 中不可用
（[OpenAI][openai-browser]）。Claude Code 通过 Claude in Chrome 扩展操作 Chrome，
该扩展声明了 `debugger` 权限——即 Chrome 的 CDP 传输（[Anthropic][claude-chrome]、
[Chrome][chrome-debugger]）。VS Code 的集成浏览器是 Electron `WebContentsView`，
主进程拥有页面，共享进程经 CDP 代理运行 Playwright
（[架构源码][vscode-architecture]、[用户文档][vscode-tools]）；它的用户/Agent 共享
tab 模型服务于人类共同观看，而 BuildMax 未把这一点作为首阶段需求，所以它是先例
而非模板。共同模式——真实引擎拥有渲染状态，控制器暴露有界的导航、观察与交互，
任何 UI 展示同一页面——正是所选设计遵循的。

[mdn-frame-ancestors]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Content-Security-Policy/frame-ancestors
[wails-intro]: https://v2.wails.io/docs/introduction/
[wails-window]: https://v2.wails.io/docs/reference/runtime/window/
[openai-browser]: https://learn.chatgpt.com/docs/browser
[claude-chrome]: https://support.claude.com/en/articles/12012173-get-started-with-claude-in-chrome
[chrome-debugger]: https://developer.chrome.com/docs/extensions/reference/api/debugger
[vscode-architecture]: https://github.com/microsoft/vscode/blob/main/.github/skills/integrated-browser/SKILL.md
[vscode-tools]: https://code.visualstudio.com/docs/agents/run/browser-tools

## 9. 延后事项

明确不在范围之内，各自是后续单独的决定：内嵌视图的**交互式**接管（经 CDP
转发输入）与页面共享、在跨来源导航加载前将其阻止（请求拦截；§3 只限定交互）、
Linux 与 Windows 运行证据（§6）、若只读 screencast 不够再上原生内嵌引擎（CEF/Electron）、
托管 Chrome for Testing 或打包浏览器、为 worker/Portal/定时运行启用该能力、
subagent 浏览器访问、上传/下载、任意 JavaScript，以及操作已登录的第三方网站。
用户可见的行为与设置随各自交付移入 manual/reference 文档。
