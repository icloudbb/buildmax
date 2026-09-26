# 桌面 / Web / 移动端的客户端界面收敛

> **翻译说明：** 本文是[英文原文](../../proposals/client-surface-convergence.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。

> **受众：** 贡献者、产品设计师和早期采用者 · **状态：** 提案 — 讨论中
>
> **开启时间：** 2026-09-20

相关文档：[路线图](../ROADMAP.md)、[界面定位](../design/界面定位.md)、
[客户端模式](../design/客户端模式.md)、
[Desktop 架构](../contribute/architecture/desktop.md)、
[Server 架构](../contribute/architecture/server.md)、
[远程控制](../design/远程控制.md)，以及
[客户端 Session 与 API 凭证](client-sessions-and-api-credentials.md)。

## 目录

- [1. 摘要](#1-摘要)
- [2. 引出这份提案的问题](#2-引出这份提案的问题)
- [3. 问题与当前背景](#3-问题与当前背景)
- [4. 为什么换框架解决不了目标](#4-为什么换框架解决不了目标)
- [5. 云端、Web 与移动端真正需要什么](#5-云端web-与移动端真正需要什么)
- [6. 目标](#6-目标)
- [7. 非目标](#7-非目标)
- [8. 收敛模型](#8-收敛模型)
- [9. 可切换的数据层](#9-可切换的数据层)
- [10. Environment 平面与 Desktop-in-Cloud 形态](#10-environment-平面与-desktop-in-cloud-形态)
- [11. 移动端作为薄客户端](#11-移动端作为薄客户端)
- [12. 方案与权衡](#12-方案与权衡)
- [13. 待解决问题与所需证据](#13-待解决问题与所需证据)
- [14. 获采纳后的可能归宿](#14-获采纳后的可能归宿)

## 1. 摘要

BuildMax 目前有两个 React 客户端：Desktop 是一个 Wails 应用，其前端通过进程内
IPC 与本地 Go 后端通信；Portal 是一个浏览器应用，通过 HTTP 和 WebSocket 与
`buildmax-server` 通信。两者已经通过 `@buildmax/gui` 共享展示层，但仅此而已。

出现了两个相邻的诉求：把 Desktop 体验放到云端运行，让用户用浏览器访问；以及让
用户从手机访问 BuildMax。一个自然的第一反应是重新考虑桌面外壳——比如从 Wails
迁移到被认为支持移动端的 Tauri。本文主张：对这两个诉求而言，桌面外壳的选择都是
错误的杠杆。Wails 与 Tauri 属于同一架构类别——本地后端 + 原生 WebView + 进程内
IPC 桥——两者都不会把后端暴露给远程浏览器。真正能触达云端、Web 与移动端的杠杆
是一套网络 API 加一个 Web 前端，而这在 Portal 与 `buildmax-server` 中 BuildMax
已经具备。本提案建议把客户端的**数据层**作为接缝：一套共享 UI，构建在一个既能说
本地 IPC、也能说远程 HTTP/WebSocket 的数据接口之上，从而让同一界面服务于
本地原生、云端 Web 与薄移动端三种模式，无需第二套 UI，也无需更换桌面框架。
云端 Web 模式最清晰的实例就是 **Desktop-in-Cloud** 形态（§10）：一个长运行的、
Space 级的环境，通过 Web 提供 Desktop 体验，在 Portal 上统一管理，但运行在一个
独立于其任务式 Task/TaskRun 模型的执行平面上。

## 2. 引出这份提案的问题

问题很具体：Tauri 的架构与 BuildMax 使用的 Wails 不同；如果我们希望 Desktop
在云端独立运行并通过 Web 访问，Tauri 是否更有优势？后续又补充说，Tauri 被认为
拥有更强的移动端支持。

作为框架事实，这两点部分为真。Tauri 与 Wails 并不完全相同，Tauri 2.0 确实提供了
Wails 所没有的一等 iOS/Android 目标。本文的主张更窄，也更具决定性：对于**本**
代码库而言，这两点都不改变"在云端运行、服务浏览器、服务手机"所需要的东西。把
推理记录在此，可避免在没有新证据的情况下重新提起框架问题。

## 3. 问题与当前背景

在共享 UI 之下，两个客户端结构截然不同：

- **Desktop 是 Wails + 进程内 IPC，而非 HTTP。** 入口二进制是
  `cmd/buildmax-desktop/main.go`；`wails.Run` 绑定整个 `App` 结构体
  （`internal/interface/desktop/run.go`），因此前端通过注入的 `window.go` 对象
  调用导出的 Go 方法，如 `SendMessageStream`、`ListProjects`
  （`internal/interface/desktop/app.go`），Go 侧用 `runtime.EventsEmit` 推回
  流。UI 资源经 `//go:embed` 编译，由 WebView 进程内 asset server 提供；Desktop
  进程不监听任何本地可达的 HTTP 端口。其唯一的出站网络是作为 managed server 的
  客户端（登录、managed-server URL）。
- **Portal 是通过 HTTP/WebSocket 的浏览器客户端。** `buildmax-server`
  （`cmd/buildmax-server/main.go`）运行标准 `http.Server`
  （`internal/server/server.go`），路由在 `internal/server/handlers/routes.go`，
  含按 space 的 WebSocket 升级。Portal 通过 `portal/src/lib/api/*` 访问它。
- **只有展示层被共享。** `@buildmax/gui`（`gui/`）是一个纯展示型的 React 组件与
  主题包，两个应用通过 `file:` 依赖引入。它不含任何数据、网络或认证逻辑；每个
  应用各自拥有数据层——Desktop 走 `window.go`/`window.runtime`，Portal 走
  `lib/api/*`。

因此，"在云端运行、用浏览器访问"所需的能力——一个能服务于任意远程浏览器、并通过
网络与后端通信的前端——今天只存在于一处（Portal + server），而 Desktop 从构造上
就没有它。

## 4. 为什么换框架解决不了目标

Wails 与 Tauri 共享同一架构：本地后端、原生 OS WebView，以及注入该 WebView 的
IPC 桥。在 Wails 里，桥是 `window.go` 加 `runtime` 事件；在 Tauri 里，是
`invoke` 加事件。这个桥只注入到**本地**那个 WebView 中，不会跨越网络。把任一外壳
指向远程 URL，只是把远程页面加载进本地 WebView；它不会让后端命令对一个独立的
远程浏览器可达。因此 Tauri 与 Wails 一样，没有"后端在云端、UI 在用户浏览器"的
原生路径。迁移会付出巨大的、横切式的成本，却把真正的目标原地留在起点。

对 BuildMax 而言，换框架比"中性"更糟。后端是端到端的 Go，且 AGENTS.md 把 Go
定为主实现语言，要求 CLI/TUI 为单一二进制、不引入 Node。Wails 让 Desktop 到
Server 保持在一套 Go 栈里。Tauri 的核心是 Rust；采用它会分叉后端语言，把逻辑从
`internal/*` 中割裂出去，而这份成本不会被云端/Web/移动端目标偿还——因为它们根本
都不在桌面外壳上运行。

## 5. 云端、Web 与移动端真正需要什么

三个诉求都归结为同一个要求：一套网络 API（HTTP/WebSocket），加上一个说这套 API
的前端。

- **云端 + Web** 就是 Portal 的形态本身：后端跑在服务器上，浏览器是客户端。
- **移动端**，对一个重活是 Go Agent runtime——工具循环、沙箱、子进程、本地 Git
  操作——的产品来说，几乎注定是一个**薄客户端**。手机不是那个 runtime 运行的
  地方；现实的手机用途是观察 + 轻交互（看任务与 Issue、审批、给 Agent 发消息、
  看运行流），runtime 在服务端执行。薄客户端同样只需要网络 API。

桌面外壳与这三者正交。跨界面变化的是数据层的传输方式，而不是 UI，也不是框架。

## 6. 目标

- 一套构建在 `@buildmax/gui` 上的 React UI，在本地原生（Desktop）、云端 Web
  （Portal）与薄移动端三种模式下渲染同一批核心界面。
- 一个客户端侧的数据接口，带两个可互换实现——本地 IPC 与远程 HTTP/WebSocket——
  由模式在运行时选择。
- 为只有本地外壳能做的事保留 Wails：本地项目与文件系统访问、本地终端、OS 集成。
- 让"在云端运行、用浏览器访问"成为既有 Portal + `buildmax-server` 路径的自然
  延伸，而非新建基础设施。
- 命名并落地 **Desktop-in-Cloud** 形态（§10），作为一个独立的 **Environment
  平面**：一台长运行的、Space 级的机器，在其自有 workspace 上通过 Web 提供
  Desktop 体验，在 Portal 上统一管理——复用其认证、Space 授权与插件分发——但
  区别于 Task/TaskRun 执行模型，拥有自己的申请、租约、资源管理与回收。
- 给移动端一条清晰、低成本的路径（响应式 Portal，然后 PWA，然后可选的薄原生
  壳），复用同一套 UI 与数据接口。

## 7. 非目标

- 将桌面外壳迁移到 Tauri 或任何其他框架。
- 在手机上或远程浏览器标签页内运行完整的 Agent runtime——沙箱、工具循环、
  子进程执行。
- 把 Desktop 进程本身暴露给远程浏览器，或把 Wails 变成网络服务器。
- 将 Portal 与 Desktop 合并为一个可部署物；它们仍是各自独立的客户端，收敛于共享
  UI 与共享数据契约，遵循[界面定位](../design/界面定位.md)。
- 改变认证或授权模型；本文假定沿用既有的 Session 与凭证工作（见
  [客户端 Session 与 API 凭证](client-sessions-and-api-credentials.md)）。

## 8. 收敛模型

三层，接缝刻意划定：

1. **展示层** —— `@buildmax/gui` 与应用级组合视图。跨每个界面共享。它已经是纯
   展示的，因此是天然的公共层。
2. **数据层** —— 一个客户端侧接口（列项目、发消息并接收流、响应审批、取消运行
   等），带两个实现：面向本地 Wails 外壳、走 `window.go`/`window.runtime` 的
   IPC 适配器，以及面向浏览器与移动端、走 server API 的 HTTP/WebSocket 适配器。
   这是新的接缝。
3. **外壳** —— 本地原生用 Wails；Web 用浏览器；移动端用可选的薄原生壳
   （Capacitor 或 React Native）。外壳选择哪个数据层实现生效，并只提供外壳专属
   能力。

同一套 UI 随后在三种模式下运行而无需第二套代码库：本地 Wails WebView 走 IPC；
同一套 UI 走 HTTP/WebSocket 连 managed server（这正是"Desktop 在云端、用浏览器
访问"的具体含义——它就是 Portal）；以及薄移动端客户端走同一套 API。

## 9. 可切换的数据层

今天与 Wails 的耦合完全存在于 Desktop 的数据层中：前端直接读
`window.go`/`window.runtime`。工作是定义一个 UI 依赖的数据接口，然后在其背后
提供 IPC 与 HTTP/WebSocket 两个适配器。Portal 的 `lib/api/*` 实质上已经是第二个
适配器；任务是把两者在一个共享契约之下调和，而不是发明一种传输。

有两处形态需要留心，因为 IPC 与 HTTP 本质不同：

- **流式。** Desktop 用 Wails 事件（`runtime.EventsEmit`）；Portal 用 WebSocket。
  接口应表达"订阅某次运行的流"，让每个适配器用自己的原生机制满足它。
- **请求/响应与错误。** IPC 的方法调用语义与 HTTP 的请求/响应，必须向 UI 呈现
  一致的错误与结果形态，使 UI 不因传输而分叉。

这是对既有接缝的渐进重构，而非重写，除了确保 server API 覆盖共享 UI 所需的界面
之外，无需触碰 Go 后端。

## 10. Environment 平面与 Desktop-in-Cloud 形态

收敛模型让一个旗舰形态变得具体、值得作为目标命名：一台**个人云端机器**。用户
用浏览器访问一个**长运行的远端环境**，就得到一个在其上运行的、与 Desktop 等价
的体验：项目、聊天会话、终端、文件与 diff 视图、审批以及实时运行流。它是把
Codespaces/Gitpod 模式套用到 BuildMax——Agent runtime 与 workspace 住在这台
机器上，浏览器是走 §9 那个 HTTP/WebSocket 数据适配器的薄客户端。

为什么它是既有部件的组合，而非一个新产品：

- runtime 已经共享——`internal/agentapp` 为 CLI、Desktop、评测与 worker 组装
  同一套模型、工具、MCP、hooks、sandbox、trace 与 sessions，因此让它在一个
  长运行环境上运行是原样复用。
- UI 已经通过 `@buildmax/gui` 共享；Desktop 的 React 界面在浏览器里走
  HTTP/WebSocket 适配器运行。
- 传输已经存在于 Portal + `buildmax-server`；终端天然映射到 WebSocket，
  已交付的 Desktop 终端 PTY 管理器（`internal/interface/desktop/terminal.go`）就是它的种子。

**这是第二个执行平面，不是 Task 平面。** Task 加 TaskRun 是一个**有界 turn**
模型：一次 run 把 Space 文件 materialize 进 run-scoped 的 `workspace/`，执行一个
turn 或一次尝试，记录权威结果，然后拆除，跨 run 的状态靠 checkpoint 续接。而
一个长运行环境在种类上正相反——一台持续存在、被交互使用的机器，带活的终端、
处理中的文件与超出任何单个 turn 的后台进程。把它硬塞进 TaskRun 会同时扭曲两者。
它是一个独立的 **Environment 平面**，有自己的资源、生命周期与管理面，绝不应
继承任务式的执行模型。

**Portal 统一管理，而非统一执行。** Environment 平面原样复用 Portal 既有的
控制面：

- **认证与授权** —— 同一套 JWT 与单次登录码，以及 **Space 作为授权边界**：
  一个环境是 Space 级资源，因此谁能申请、访问或销毁它，遵循 Space 成员与角色，
  与 Task、Artifact、Workflow 完全一致。
- **插件分发** —— 与 worker 已获得的同一份服务端解析、Space 显式的激活，注入
  run-scoped 的 `BUILDMAX_HOME`。

随后它加入任务式模型完全没有的概念，而新的设计工作正在这里：

- **申请 / 请求** —— 成员请求一个环境；服务端分配容器、其 workspace 以及一个
  Space 级的 `BUILDMAX_HOME`。
- **租约** —— 环境在一个带 idle 超时的租约下持有，因使用而续期，因此"长运行"
  不等于"永远运行"。
- **资源管理** —— CPU、内存与存储上限，以及按 Space 的配额，因为一个闲置环境
  仍然烧钱，这与 ephemeral Job 不同。
- **回收** —— idle 时休眠，租约到期时停止，并对"跨休眠与重启什么留下、回收时
  什么丢失"给出明确答案。

有两条性质定义了这个形态，必须刻意决定：

- **workspace 是这台机器的，不是用户的笔记本。** 项目住在那台机器上（在其上
  clone 或挂载）；这是 Codespaces 语义，纯本地的 OS 集成不会照搬。它是这个形态
  的定义性特征，而非缺口，但必须明说，以免"和 Desktop 一样"被读成"你的本地
  文件"。
- **网络暴露改变了信任姿态。** 一个网络可达的 Agent 拥有 shell 与文件系统访问
  权限，因此 Bash sandbox 应默认开启并 fail-closed 强制执行，采用 worker 的姿态
  而非本地 CLI 的默认。见 [Agent 沙箱策略](../design/Agent沙箱策略.md)。

**它区别于 Portal 的任务式模型，而非区别于 Portal。** Environment 平面与 Task
平面共享认证、Space 授权、插件分发、runtime、UI 与传输；它们不共享执行模型及其
资源，谁也不吞并谁。一个构建在同一 `agentapp` runtime 之上的独立单租户 server
仍是同一形态的一种可能封装，但把 Environment 平面托管在 Portal 内更可取：它复用
上述多租户控制面，而不是重新发明它们。

本提案提出之后，[远程控制](../design/远程控制.md)已被接受并交付了前几个阶段。它把
自身与这里的 Environment 平面放在“运行宿主 × 交互界面”的同一网格上，并构建了
窄界面、本地宿主这一象限——通过服务器操控用户本机上的会话——完全不需要上述
环境底座。

## 11. 移动端作为薄客户端

一旦有了网络 API，移动端就有一条分级路径，先易后难，每一步都复用共享 UI 与
数据层：

1. **响应式 Portal。** Portal 本就是浏览器应用；把其核心界面做成移动端自适应，
   即可在手机浏览器中使用，无需新框架。
2. **PWA。** 加 manifest 与 service worker，获得可安装性、离线壳与推送——接近
   原生体验而无需应用商店。
3. **薄原生壳。** 为上架分发或更深的原生能力（推送、生物识别、深链），用
   Capacitor 或 React Native 包一层 Web UI，复用 `@buildmax/gui` 与
   HTTP/WebSocket 适配器。

Tauri 真实的移动端优势在此帮不上忙：它服务于在设备上运行 Rust 应用的团队，而
BuildMax 的手机界面是一个连 Go 服务端的薄客户端。这份移动端优势落在本代码库的
形态之外。

## 12. 方案与权衡

- **A —— 收敛到可切换的数据层（本提案）。** 一套 UI、两个数据适配器，保留
  Wails。复用度最高；云端/Web 路径已存在；移动端是增量。成本：设计一个能同时
  干净适配 IPC 与 HTTP 的数据接口，并让 server API 对共享 UI 保持完备。
- **B —— 把 Desktop 迁到 Tauri。** 横切式大工程；把后端分叉进 Rust，违背 Go
  单一栈原则；却仍不产生任何远程浏览器或薄移动端能力。因未触及目标而否决。
- **C —— Desktop 与 Portal 完全分开，再建第三个移动客户端。** 要维护三套 UI 与
  三套数据层；展示与行为漂移。长期成本最高，一致性最弱。
- **D —— 什么都不做。** Portal 今天已服务 Web；云端/Web 诉求部分被满足，移动端
  只是手机浏览器体验。当下成本最低，但 Desktop 的 Wails 耦合仍是承重的，每一次
  跨界面改动都要为这道割裂付两遍代价。

## 13. 待解决问题与所需证据

- 当前 server API 是否已覆盖共享 UI 所需的每个界面，还是存在按设计必须保持
  IPC-only 的 Desktop 专属能力（本地项目、文件系统、终端）？枚举其差距。
- 共享的流式抽象应是什么形态，才能让一个接口干净地映射到 Wails 事件与
  WebSocket 两者，而不泄露任一方？
- 移动端的哪一步（响应式 Portal、PWA、原生壳）由最早的真实用户需求所证明其
  正当性，以及什么证据能把决定推过"响应式 Portal"这一步？
- §10 已确立 Environment 平面独立于 Task 平面、并在 Portal 内统一管理；仍待
  决定的是具体策略——默认租约与 idle 休眠时长、按 Space 的配额、跨休眠与回收
  究竟什么留下、workspace 如何准备（启动时 clone、挂载，或挂接一个已有仓库），
  以及一个可交互访问的环境的沙箱默认值。
- 证明可切换数据层的最小切片是什么——例如，一个来自 `@buildmax/gui`、在 IPC
  适配器与 HTTP 适配器之上均原样运行的界面？

## 14. 获采纳后的可能归宿

若获采纳，数据层接缝与界面收敛成为一份[设计记录](../design/设计文档索引.md)——
最自然是作为[界面定位](../design/界面定位.md)与[客户端模式](../design/客户端模式.md)
的延伸——而渐进重构与移动端路径进入 [ROADMAP.md](../ROADMAP.md)，成为拆解后的
[backlog](../../backlog/README.md) 条目。"桌面框架迁移不是杠杆"这一判断记录在那里，
以便在没有新证据时不被重新翻案。随后删除本提案。
