# Agent 浏览器能力

> **翻译说明：** 本文是[英文原文](../../design/agent-browser-capability.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
>
> **受众：** 贡献者 · **状态：** 活动计划 · **复核日期：** 2026-09-22

本记录确定了"给 Agent 一个真实、可控的浏览器"这件事的架构与信任规则。它是继
[浏览器能力提案](../proposals/browser-capability.md)之后作出的决定；那份提案
记录了权衡过的备选方案与同类产品调研。相关文档：
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
- [8. 延后事项](#8-延后事项)

## 1. 决定与范围

核心结果是让 Agent 能**针对真实渲染的页面验证一处变更**：导航到路由、观察
页面及其控制台、点击和输入，并依据观察到的状态行动，而不是从源代码或 HTTP
响应推断成功。`WebFetch` 返回静态 HTML，无法满足这一点。

这是共享 Agent 运行时的能力，而非某个界面的能力。它首先在 **CLI 上以
headless 方式**交付，由 Go 自持的 Chromium 进程通过 Chrome DevTools
Protocol（CDP）驱动。随后 Desktop 增加了**可见窗口**：以 headful 启动浏览器，
让用户看到 Agent 正在操作的页面，并显示一个把每个 session 关联到其当前页面的
简洁活动指示。把页面渲染进工作区 **tab 内**、以及用户接管/页面共享，仍然延后。
提案中的同类产品调研已确立此工作流的价值，因此这项工作证明的是架构、可移植性
与信任——不是需求。

锁定的决策：

- **浏览器来源：** 发现已安装的系统 Chrome/Edge；找不到时返回清晰、可操作的
  错误。托管 Chrome for Testing 下载与打包留待以后。
- **界面：** CLI（headless）与 Desktop（headful，浏览器自己的可见窗口 + 活动
  指示）。把页面嵌入工作区 tab 延后。
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
- 行动前按策略检查当前来源，并拒绝过期元素引用而非猜测目标；
- 在页面文字与控制台摘录进入模型上下文及 trace 前对其截断；
- 遵守上下文取消与每次调用的超时。

交互工具声明 `AccessWrite`；观察工具声明 `AccessReadOnly`。来源准入使用既有
审批路径，且必须写明确切来源、操作与当前页面——对某一来源的 Session 授权绝不
授权另一来源。

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
  拒绝）、来源准入、过期引用拒绝，以及取消/关闭时的清理；在不需要真实浏览器
  处用 fake `BrowserController`。
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

## 8. 延后事项

明确不在范围之内，各自是后续单独的决定：内嵌视图的**交互式**接管（经 CDP
转发输入）与页面共享、若只读 screencast 不够再上原生内嵌引擎（CEF/Electron）、
托管 Chrome for Testing 或打包浏览器、为 worker/Portal/定时运行启用该能力、
subagent 浏览器访问、上传/下载、任意 JavaScript，以及操作已登录的第三方网站。
用户可见的行为与设置随各自交付移入 manual/reference 文档。
