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
| [风险驱动的端到端验证扩展](risk-driven-e2e-expansion.md) | 验证 | 哪一小组新增端到端旅程最能降低剩余 Beta 风险，以及每个旅程应由哪一层边界证明？ | 优雅 worker 丢失及 MySQL/对象存储 readiness 中断恢复探针已交付；Server 重启/重连、worker 写拒绝、部分工作取消与统一候选证据仍待完成 |
| [企业功能要求盘点](enterprise-capability-requirements.md) | 运维与部署 | 企业部署可能需要哪些候选要求，应以什么证据逐项验证？ | 仅盘点要求；关联既有基础，不定义企业版或功能边界 |
| [人员停用与执行权限](personnel-deactivation-lifecycle.md) | 运维与部署 | 移除账户或 Space 权限时，哪些凭证和无人值守执行应停止、多久内停止，以及哪些内容继续归 Space 所有？ | 已对照当前账户、Schedule、TaskRun、Workflow 和 Space 行为提出契约；统一生命周期尚未实现 |
| [单一维护者的 Agent 开发工作流](single-maintainer-agent-development.md) | 验证 | 一位维护者如何借助编码 Agent 提升被接受的开发吞吐量，同时不成为工作流瓶颈？ | 支撑流程的构件已经存在，但就绪性复核、租约、变更范围验证与独立验收尚未形成闭环 |
| [系统管理操作](system-administration-operations.md) | 运维与部署 | 运维 CLI 与 Portal 应如何为管理和运行时健康提供安全且一致的结果？ | 核心管理界面、OIDC 诊断与外部身份管理已交付；事务审计、CLI Session 对齐、配额分配与更丰富的运行时操作仍待完成 |
| [部署管理员的跨 Space 工作可见性](admin-cross-space-work-visibility.md) | 运维与部署 | 管理员应进入所有 Space，还是在 Administration 查看跨 Space 工作？没有 Space 成员身份时能看到哪些运维事实？ | 已交付 Admin Space 元数据、用量、跨 Space 审计、LLM 调用账本及 TaskRun 状态计数；尚无全局 Agent、Workflow、Schedule 或 Issue 清单 |
| [客户端 Session 与 API 凭证](client-sessions-and-api-credentials.md) | 信任与安全 | 交互式、原生与无人值守客户端应获得哪些凭证？ | 持久 Session 状态、绝对过期、逐请求撤销、Portal cookie 认证与原生 OS Secret 存储已交付；scope、签名密钥轮换、自助管理、PAT 与服务账号仍待决定 |
| [持久化 Agent Session](durable-agent-sessions.md) | 本地体验 | 已认证的本地 Agent Session 是否应成为带 revision 的 Server 资源？ | 尚未开始；没有 Server 路由提供 Session 资源 |
| [Assistant 编排与 Workflow 边界](assistant-orchestration-and-workflow-boundary.md) | 产品与执行模型 | 管理者 Agent 是否足以支持 Assistant 产品，Workflow 是否应收窄为确定性的 Automation？ | 尚未开始；Agent 无法承接持久的子级 Space Agent Task |
| [Agent 自我调节能力](agent-self-regulation-capabilities.md) | Agent Runtime and Models | 哪些 runtime 可见元能力能够显著改善 Agent 调节自身工作的能力，同时不创建模型拥有的控制平面？ | 现有目标、事件、工具、权限、trace、checkpoints、委派与 memory 是候选基础；尚无统一的自我调节契约 |
| [本地 Issue 工作桥接](local-issue-work-bridge.md) | 本地体验 | 已连接的本地界面应如何处理 Space Issue？ | R5 第 1 项安排剩余的 Phase 1 决策；持久 Issue-to-Session 关联与后续阶段仍待完成 |
| [Session 树、Agent 邮箱与分支工作区](session-tree-and-agent-mailbox.md) | 本地体验 | Session 是否应 fork 隔离工作区，并通过持久邮箱恢复父 Session？ | 尚未开始 |
| [作为派生工作视图的 Portal Issue 看板](portal-issue-board-view.md) | 产品与执行模型 | Portal 是否应把 Space Issue 投影为固定三列看板，而不创建第二套规划模型？ | 现有 Issue 状态、带版本更新、顶层过滤、Owner/Executor 过滤与派生子项进度已构成充分基础；Board 视图尚未交付 |
| [Issue 主题协调与 Agent 黑板](issue-topic-coordination.md) | 产品与执行模型 | 子 Issue 参与者是否应共享父级范围的信息流，同时保持定向投递与同步语义相互独立？ | 现有 Issue 评论与限定范围的 Agent 读取/报告工具是候选验证底座；跨子项 Topic feed 尚未建设 |
| [Agent 原生协作底座](agent-native-collaboration-substrate.md) | 产品与执行模型 | 不同规模和不同专业背景的参与者，是否需要一套统一的意图、执行、提议、证据、决策、集成与知识生命周期？ | 尚未建设；当前 Issue、Task/TaskRun、Artifact、Space 与本地 workspace 是待验证的基础构件 |
| [Desktop 工作区 Tab 与 Explorer 侧边栏](desktop-workspace-tabs.md) | 本地体验 | Desktop 是否应围绕一个异构 tab 的中间界面（聊天、终端、文件、diff）重塑，由一个 project 级 Explorer 侧边栏喂入，并把本机终端作为一种 tab 类型？ | 终端传输的探索性原型（Go PTY 会话管理器 + xterm tab），临时放置为底部面板；tab 面、Explorer 重塑与文件/diff tab 尚未建设，并发 agent tab 仍以工作区隔离为前置 |
| [桌面 / Web / 移动端的客户端界面收敛](client-surface-convergence.md) | 本地体验 | 是否应以一套共享 UI 加可切换数据层来服务本地原生、云端 Web 与薄移动端三种模式，而不是迁移桌面外壳（例如迁到 Tauri）？ | `@buildmax/gui` 已在 Desktop 与 Portal 间共享展示层；Portal 加 `buildmax-server` 已提供网络路径，但 Desktop 数据层仍为 Wails-only，可切换数据接口与移动端客户端均尚未存在 |
| [Agent 代表用户使用应用](agent-app-delegation.md) | 本地体验与信任 | 以工作区为中心的 Agent 能否成为连接应用的有用入口，同时让授权与批准易于理解？ | 插件连接器 CLI 的 OAuth 与 Gmail 样例；远端 MCP 连接和调用 CLI；尚无单次运行授权或加固的 Agent 边界 |

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
