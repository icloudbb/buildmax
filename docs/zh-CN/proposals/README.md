# 提案

> **翻译说明：** 本文是[英文原文](../../proposals/README.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。

> **受众：** 贡献者与早期采用者 · **状态：** 现行有效

提案是关于跨 domain 方向的简短文档，值得在成为路线图工作之前先行讨论。它们
不是承诺、产品公告或用户文档。

## 本目录的运作方式

| 产物 | 用途 |
|---|---|
| [ROADMAP.md](../ROADMAP.md) | 已排定优先级并被采纳的工作 |
| [design/](../design/设计文档索引.md) | 已采纳的理由与活动计划 |
| 本目录 | 某个方向获采纳前的开放问题与可选方案 |
| GitHub Discussions | 早期社区反馈与替代方案 |
| GitHub Issues | 有负责人、有验收标准、可实施的工作 |

每份提案都以 `Status: proposal — under discussion` 开头，并附有
`Opened: YYYY-MM-DD` 日期，指明相关的现行文档，并将目标、非目标、可选方案与
开放问题分开陈述。范围应足够聚焦，使读者无需先反向拆解整个代码库，就能表示
赞同、反对或提供证据。

一旦做出决定，就更新 [ROADMAP.md](../ROADMAP.md)、对应设计记录或 GitHub Issue，
然后删除提案。被否决和被取代的提案同样删除而不归档；Git 历史保留其上下文。

## 开放提案

一份文档在方向获采纳前保持开放，这并不意味着期间没有任何建设。如果早期切片
先于决定交付，最后一列会说明，具体细节留在提案自身的交付阶段中。

| 提案 | Primary domain | 问题 | 目前已构建的内容 |
|---|---|---|---|
| [企业功能要求盘点](enterprise-capability-requirements.md) | 运维与部署 | 企业部署可能需要哪些候选要求，应以什么证据逐项验证？ | 仅盘点要求；尚无任何要求经具名部署验证。相关基础已交付（OIDC 登录、管理界面、带执行资格闸门的引导式账户停用、已停用 owner 的恢复）；配额、暂停与事件处置旅程尚未检视 |
| [单一维护者的 Agent 开发工作流](single-maintainer-agent-development.md) | 验证 | 一位维护者如何借助编码 Agent 提升被接受的开发吞吐量，同时不成为工作流瓶颈？ | 仓库内 backlog（单一认领 frontmatter）、`./make board` 状态视图及其 frontmatter 检查已交付；变更范围验证、自动就绪性复核、pull request 交付检查、独立验收与工作区回收尚未建设 |
| [部署管理员的跨 Space 工作可见性](admin-cross-space-work-visibility.md) | 运维与部署 | 管理员应进入所有 Space，还是在 Administration 查看跨 Space 工作？没有 Space 成员身份时能看到哪些运维事实？ | 仅为提案；Administration 已展示 Space 元数据、用量、已停用 owner 的恢复、跨 Space 审计、LLM 调用账本及 TaskRun 状态计数。尚无全局 Agent、Workflow、Schedule 或 Issue 清单，也无按成员身份限定的跨 Space 视图 |
| [客户端 Session 与 API 凭证](client-sessions-and-api-credentials.md) | 信任与安全 | 交互式、原生与无人值守客户端应获得哪些凭证？ | 持久 Session 状态、绝对过期、逐请求撤销、Portal cookie 认证与原生 OS Secret 存储已交付；scope、签名密钥轮换、自助管理、PAT 与服务账号仍待决定 |
| [持久化 Agent Session](durable-agent-sessions.md) | 本地体验 | 已认证的本地 Agent Session 是否应成为带 revision 的 Server 资源？ | 尚未开始；没有带 revision 的 Server Session 资源或路由。Task 级 worker session bundle 持久化在运行存储中，Remote Control 中继本地活动 Session 但不保存其对话记录 |
| [Assistant 编排与 Workflow 边界](assistant-orchestration-and-workflow-boundary.md) | 产品与执行模型 | 管理者 Agent 是否足以支持 Assistant 产品，Workflow 是否应收窄为确定性的 Automation？ | Portal 聊天可列出、运行并观察已发布的 Workflow（§9.5）；尚无有界的 Agent 间委派——Agent 无法承接持久的子级 Space Agent Task |
| [Agent 自我调节能力](agent-self-regulation-capabilities.md) | Agent Runtime and Models | 哪些 runtime 可见元能力能够显著改善 Agent 调节自身工作的能力，同时不创建模型拥有的控制平面？ | 现有目标、事件、工具、权限、trace、checkpoints、委派与 memory 是候选基础；尚无统一的自我调节契约 |
| [本地 Issue 工作桥接](local-issue-work-bridge.md) | 本地体验 | 已连接的本地界面应如何处理 Space Issue？ | `buildmax issue list/show/status/start/comment` 已交付，Agent 通过 `buildmax issue` 读取与报告（[Agent 桥接 CLI](../design/Agent 桥接 CLI.md)）；R5 第 1 项安排剩余的 Phase 1 决策——持久 Issue-to-Session 关联、工作区映射、本地结果投影及任何 Desktop Issue 界面仍待完成 |
| [Session 树、Agent 邮箱与分支工作区](session-tree-and-agent-mailbox.md) | 本地体验 | Session 是否应 fork 隔离工作区，并通过持久邮箱恢复父 Session？ | 本地物理复制 fork 及 `forked_from` 来源、`buildmax info` 与 TUI/Desktop `/info` 中的只读 fork 树、Agent 管理的 Git worktree 已交付；尚无 fork 时的工作区隔离、父级收件箱、持久邮箱、`ReportToParent` 或受监督的恢复 |
| [作为派生工作视图的 Portal Issue 看板](portal-issue-board-view.md) | 产品与执行模型 | Portal 是否应把 Space Issue 投影为固定三列看板，而不创建第二套规划模型？ | 现有 Issue 状态、带版本更新、顶层过滤、Owner/Executor 过滤与派生子项进度已构成充分基础；Board 视图尚未交付 |
| [Issue 主题协调与 Agent 黑板](issue-topic-coordination.md) | 产品与执行模型 | 子 Issue 参与者是否应共享父级范围的信息流，同时保持定向投递与同步语义相互独立？ | 现有 Issue 评论加上 `buildmax issue show`/`comment`（在 worker 运行中限定到单个 Issue）是候选验证底座；跨子项 Topic feed 尚未建设 |
| [Agent 原生协作底座](agent-native-collaboration-substrate.md) | 产品与执行模型 | 不同规模和不同专业背景的参与者，是否需要一套统一的意图、执行、提议、证据、决策、集成与知识生命周期？ | 尚未建设；当前 Issue、Task/TaskRun、Artifact、Space 与本地 workspace 是待验证的基础构件 |
| [桌面 / Web / 移动端的客户端界面收敛](client-surface-convergence.md) | 本地体验 | 是否应以一套共享 UI 加可切换数据层来服务本地原生、云端 Web 与薄移动端三种模式，而不是迁移桌面外壳（例如迁到 Tauri）？ | `@buildmax/gui` 共享展示层；Portal 加 `buildmax-server` 提供网络路径，Portal 窄屏布局已交付，Remote Control 让手机浏览器观察并引导本地 Session；Desktop 数据层仍为 Wails-only，尚无可切换数据接口、Environment 平面、PWA 或原生移动客户端 |
| [Agent 代表用户使用应用](agent-app-delegation.md) | 本地体验与信任 | 以工作区为中心的 Agent 能否成为连接应用的有用入口，同时让授权与批准易于理解？ | 插件连接器 CLI（`buildmax connect`、`buildmax app`），含 OAuth 与 PKCE、固定 HTTP 操作和 Gmail 样例；远端 `connect mcp` 及 `buildmax mcp` 的 tools、schema、call 命令；尚无单次运行授权、调用审计、以应用为先的连接或加固的 Agent 边界 |

已退役提案不留在当前索引中。获采纳的理由移入
[设计记录](../design/设计文档索引.md)，被拒绝或取代的讨论仍可通过 Git 历史查阅。

## 发起一份提案

使用语义化文件名。可以从现有提案的以下结构入手：

1. 问题与当前背景。
2. 目标与非目标。
3. 可选方案与权衡。
4. 开放问题与做出决定所需的证据。
5. 获采纳后的可能归宿。

选择贡献者查找该问题时最可能进入的 primary domain，将其加入
[开放提案](#开放提案)，并在切片交付过程中保持最后一列真实。只查阅索引却发现
“目前已构建的内容”已经过时的读者，会对整个目录得出错误结论。

当一个问题需要多个独立署名的 Agent 立场时，使用一个语义化目录：其中
`README.md` 负责问题、决策过程与证据标准，每位贡献者各自提供一份明确署名的
`<agent-name>-view.md`。此处只索引该目录的 README。贡献者不得编辑彼此的立场；
后续综合应保留分歧，并将获采纳的理由移入通常的设计记录。

不要为聚焦的缺陷、文档更正或已有验收标准的实施任务创建提案；请使用 Issue。
