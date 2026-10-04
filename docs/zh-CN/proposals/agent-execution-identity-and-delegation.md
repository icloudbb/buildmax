# Agent 执行身份、连接器与委托策略

> **翻译说明：** 本文是[英文原文](../../proposals/agent-execution-identity-and-delegation.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
>
> **受众：** 维护者、产品评审者、企业运维者与安全评审者 · **状态：** proposal — under discussion
>
> **发起日期：** 2026-10-01 · **Primary domain：** 信任与安全

相关文档：[路线图](../ROADMAP.md) R5、
[Worker Run Token](../design/Worker运行令牌.md)、
[Agent 执行与 Task 线程](../design/Agent执行与Task线程.md)、
[Space Secret](../design/Space密钥.md)、
[定时 Agent 执行](../design/定时Agent执行.md)、
[企业身份与访问](../design/企业身份与访问.md)、
[Agent 代表用户使用应用](agent-app-delegation.md)，以及
[企业功能要求盘点](enterprise-capability-requirements.md)。

本备忘录盘点截至 2026-10-03 的行业与社区方向，将其放到 BuildMax
现有架构下检验，并列出可选方案。它不是路线图承诺，也不意味着仍处于实验期的
厂商能力已经成为稳定标准。

本文也集中记录企业 SSO、Agent 身份、应用连接器与受控执行之间的产品讨论。
连接器的连接体验、传输声明和本地原型仍由
[Agent 代表用户使用应用](agent-app-delegation.md)维护；本备忘负责它们与执行权限
之间的战略关系，不另建一份连接器实现规范。

## 目录

- [决策问题](#决策问题)
- [执行摘要与建议](#执行摘要与建议)
- [核心用户结果与当前约束](#核心用户结果与当前约束)
- [身份问题其实是一个元组](#身份问题其实是一个元组)
- [用户委托与独立授权](#用户委托与独立授权)
- [为什么现在必须回答](#为什么现在必须回答)
- [企业 SSO、连接器与可执行工作](#企业-sso连接器与可执行工作)
- [大厂与社区的当前方向](#大厂与社区的当前方向)
- [已经收敛与尚未收敛的部分](#已经收敛与尚未收敛的部分)
- [BuildMax 当前所处位置](#buildmax-当前所处位置)
- [典型实际场景](#典型实际场景)
- [战略方案选择](#战略方案选择)
- [建议的目标架构](#建议的目标架构)
- [授权生命周期](#授权生命周期)
- [若获采纳的分阶段方向](#若获采纳的分阶段方向)
- [威胁模型与失败语义](#威胁模型与失败语义)
- [决策标准与所需证据](#决策标准与所需证据)
- [待回答的产品问题](#待回答的产品问题)
- [获采纳后的可能归宿](#获采纳后的可能归宿)
- [参考资料](#参考资料)

## 决策问题

当 BuildMax Agent 读取企业数据或修改外部系统时，究竟由哪个主体提供权限、哪个
工作负载执行动作、什么凭证可以进入该工作负载，以及运维者如何撤销并解释结果？

哪些连接器操作使业务结果可以执行？企业 SSO、这些操作与 runtime 执行约束如何
共同发挥作用？

最直接的答案是“让每个 Agent 都成为一个用户”，但这只解决了命名。它没有回答：
交互请求是否应受 Alice 个人权限限制；财务月度任务是否应在创建者离职时停止；
子 Agent 能否继承权限；任意 Bash 是否可以读取提供方的 refresh token。因此，
本提案讨论的是执行权限与凭证托管，而不只是增加一个 `agent_id` 字段。

## 执行摘要与建议

BuildMax 应把自己定位成私有化的 **Agent 执行与治理平面**，而不是另一个个人
Agent，也不应试图替代企业 IdP。长期差异化能力应是：在组织控制边界内运行
模型与工具中立的 Agent，并能可靠回答：

> 谁提出请求、哪个 Agent revision 与 TaskRun 真正执行、依据谁的权限、作用于哪个
> 资源、具有什么能力与批准、权限持续多久，以及什么证据可以证明这一切？

建议采用混合式长期模型：

1. 已认证人类或获得明确组织授权的非人主体是策略上的权限主体。Agent 自身可以
   成为该主体；单独的 Space-owned automation principal 需要独立的生命周期需求。
2. 稳定的 Agent 身份用于标识受治理的执行者及其 revision 系谱；身份存在本身不
   自动带来广泛权限。
3. 每个 TaskRun 获得临时工作负载身份，并继续作为权威执行单元。
4. 凭证 broker 根据上述证据换取绑定单一 audience、resource 与能力集合的短期
   凭证。类型化 connector 与远端 MCP 调用应优先由 broker 代为调用，使模型无法
   看到可重复使用的凭证。
5. 高风险操作使用与具体 operation 和参数绑定的批准，而不是语义模糊的一句聊天
   “同意”。
6. 对不能经过 broker 的工具，保留环境变量或文件交付作为兼容模式，但明确标记为
   更弱的安全边界。

不应一次性建设全部目标。合理顺序是：先拆开来源与权限语义；验证一个短期凭证
交换；对真实共享自动化先验证直接向 Agent 授权，仅在业务授权需要独立于执行
Agent 的生命周期时才增加单独的 automation principal；随后
用 TaskRun workload federation 与 broker 消除已经量化的凭证风险。多跳委托应等到
出现真实的跨信任域 Agent-to-Agent 用例后再做。

## 核心用户结果与当前约束

### 核心用户结果

企业应能允许有价值的自主工作，同时不向 LLM 进程交付无边界的人类凭证。运维者
复盘事件时，应能从持久证据重建触发事件、责任 owner、执行软件、实际权限、目标、
批准和结果。

### 为什么重要

- Agent 已经从生成文本走向访问邮件、代码库、工单、财务与生产系统。
- Schedule、webhook 和 Agent-to-Agent 调用发生时，原始用户可能离线，甚至已经离职。
- Prompt injection 改变了威胁模型：Agent 读取的数据可能反过来影响它尝试执行的
  带权限操作。
- 企业身份厂商与云平台正在明确区分用户委托、自主工作负载权限、Agent 注册、凭证
  broker、sponsor 与每次运行身份。这证明控制平面存在真实缺口，但不代表当前产品
  形态已经成熟。

### 当前约束

- Space 是 BuildMax 的资源归属与 Portal 授权边界。
- Task 代表持续线程；TaskRun 代表一次执行尝试及其权威结果。执行权限属于 TaskRun。
- Worker 已使用短期 run token，而不是用户 access token。
- Space Secrets 可以把环境值按 run 交付，但运行中的模型所选进程可以读取这些值。
  短期提供方交换与 workload identity 已有设计，尚未实现。
- 官方 Worker 的 Bash sandbox 不可用时必须 fail closed，但通用出站网络与全部凭证
  外泄路径尚未受到约束。
- 本地 CLI 有意支持宽泛、可移植的工具面，Bash sandbox 默认关闭。在任意 shell 与
  网络可以绕过 connector 的情况下，connector policy 不能被宣称为强安全边界。
- BuildMax 必须适合私有部署、保持模型中立，并且不能强制依赖某一家云厂商的身份
  产品。
- 产品仍处于 Alpha。若 `created_by` 语义过载，应一致地修正领域模型，而不是增加
  兼容层。

## 身份问题其实是一个元组

以下概念回答不同问题，不能合并：

| 概念 | 回答的问题 | 示例 |
|---|---|---|
| Initiator | 是什么直接触发了本次运行？ | Alice、Schedule、Webhook 或另一个 TaskRun |
| Accountable owner / sponsor | 哪个人持续承担业务决策责任？ | 财务自动化的 sponsor |
| Authority principal | 以谁的 grant 作为权限上界？ | Alice、明确获授权的 Agent，或独立 automation principal |
| Agent identity | 哪个受治理的 Agent 定义正在执行？ | `invoice-reconciler` revision 17 |
| Execution identity | 眼下具体哪个 workload 在运行？ | 某个 Worker Job 中的 TaskRun `tr_...` |
| Credential lease | 可以向单一目标出示什么证明？ | 某仓库五分钟有效的 GitHub token |
| Approval grant | 哪个具体提权操作获得批准？ | 在 commit `abc123` 合并 PR 418 |
| Delegation chain | 权限经过了哪些 actor？ | Alice → 管理者 Agent → 部署 Agent |

Authentication 证明身份，Authorization 决定能做什么，Delegation 解释为什么某个
actor 可以行使另一个主体的权限，Credential delivery 决定 actor 是否能把权限复制
到别处，Audit 记录系统当时相信什么以及发生了什么。把这些都叫作“Agent 身份”，
最终只会得到权限过大或无法使用的自动化。

最小授权判断在概念上是：

```text
initiator + authority principal + Agent revision + TaskRun
+ target audience/resource + requested capabilities + expiry
+ approval evidence + delegation provenance
```

这是证据 envelope，而不是要求把所有内容塞进一个 JWT、一张表或一套通用策略语言。
只有当缺少某项持久状态会让具体授权、撤销或审计要求失败时，才应增加该状态。

## 用户委托与独立授权

核心结果是让 Agent 在明确的业务授权范围内行动，并保留谁授予权限、谁执行、谁
承担责任的证据。个人邮件与团队财务对账体现了不同的业务授权；仅靠用户账户或
Agent 目录条目都不能同时回答两者。这是可选的权限模式，不是互斥的 Agent 类型。

| 问题 | 用户委托权限 | 独立组织权限 |
|---|---|---|
| 谁的 grant 允许动作？ | 已认证用户的 grant，同时受 consent、目标系统权限和执行策略限制 | 向非人主体明确授予的组织 grant，受目标和执行策略限制 |
| 谁是执行者？ | Agent 与具体 TaskRun，与用户分别记录 | Agent 与具体 TaskRun，无论 Agent 是否也作为权限主体 |
| 适用场景 | Alice 处理个人邮件或提交报销 | 财务对共享业务资源执行月度对账 |
| 什么使执行资格终止？ | 用户停用、失去目标权限、撤销委托或到期 | 主体或 grant 停用、到期，或不满足 sponsor/review 策略 |
| 人类 sponsor 表示什么？ | 对 Agent 负责，不自动委托 sponsor 的权限 | 对组织 grant 负责，不默认成为凭证 subject |

同一个 Agent 可以在不同调用中使用任一种模式。用户失去访问权时，用户委托执行
不能悄悄切换到组织权限。Provider 支持时，审计应同时保留 subject 与 actor；不
支持时，BuildMax 仍须保留两者的区别以及 provider correlation。

将 Agent 与员工同等治理，适用于清单、负责人、权限复核、审计和退役；不意味着
使用人类认证方式、继承员工的全部角色，或为每个 Agent 配置邮箱/用户账户。注册
Agent 不授予权限，明确的独立授权是另一个决定。

### 什么时候值得增加独立自动化主体？

如果身份与权限可以共用生命周期，先向受 Space 治理的 Agent 直接授予有界组织
权限。创建者离职本身证明需要组织归属，并不证明需要新增主体实体：直接获授权
的 Agent 也可以通过 sponsor 转移在创建者离开后继续运行。

只有当具名业务授权必须在执行 Agent 被替换后保留，或必须授权多个分别标识的
Agent 时，单独的 Space-owned automation principal 才有理由存在。增加前须证明
直接向 Agent 授权及显式重新授权不能满足该场景。独立主体必须定义哪些 actor
有资格使用它；替换 Agent 绝不能自动继承前一个 Agent 的权限。

即使同一稳定身份同时承担 principal 与 actor，证据模型仍需区分 policy subject、
Agent actor 与 TaskRun 三种角色。独立概念不要求独立数据库实体。第一个组织授权
切片必须明确决定生命周期边界；本文后续使用独立 automation principal 的例子
都是有条件的方案，不是自主执行的既定前提。

## 为什么现在必须回答

个人 Agent 产品围绕“一个用户带着自己的上下文和连接跨应用工作”进行优化，因此
on-behalf-of 是自然默认值：Agent 是用户既有权限的新界面。

企业私有 Agent 平台的重心不同：

- 工作通常归属 Space 或团队，而不是个人；
- Schedule 与事件触发需要可持续的权限；
- 多个 Agent 和人员可能继续同一 Task；
- 执行发生在受控制的 Worker 与网络中；
- 组织需要清单、生命周期、职责分离、证据以及退出提供方的能力；
- 同一 Agent 的一个调用可能需要用户权限，另一个调用可能需要组织权限。

如果 BuildMax 聚焦于此，它就有长期优势：在一个私有平面内连接身份、执行隔离、
workspace materialization、工具策略、凭证交换、批准和 trace 证据。劣势是大厂已经
拥有更成熟的身份和应用生态。BuildMax 应集成这些系统并提供可移植的内部契约，
而不是重造它们的目录、邮箱、consent screen 与 conditional access 引擎。

## 企业 SSO、连接器与可执行工作

### 用户结果与证据边界

员工应能委托一个有界的跨应用业务结果，检查实际发生了什么，并保留组织对访问
和操作效果的控制。例如，解决支持工单可能需要读取工单、查询相关日志、检查
仓库并创建 PR。身份本身不会开放这些操作；连接器目录本身也不会授权使用它们，
或证明工作已经完成。

Agent 与连接器相辅相成：Agent 理解目标并选择行动路径，连接器提供可执行的
业务操作和可靠反馈。任务覆盖同时依赖推理能力与可用操作。这是基于本文场景的
产品假设，不是已经测量的连接器平台需求，也不意味着每项 Agent 任务都需要应用
连接器。本地文件和代码工作可以使用普通运行时工具。

### 同一任务中的职责

| 关注点 | 职责 | 边界 |
|---|---|---|
| 企业身份与 SSO | 认证人类和非人主体，提供可信身份上下文，治理身份生命周期 | 可以登录不等于可以执行所有 API 操作 |
| Agent 权限 | 标识 actor、用户或组织 grant、具体 run、target 和 approval | 注册 Agent 或指定 sponsor 不授予业务访问权限 |
| 应用连接器 | 提供具名操作及输入、输出、效果、目标/账户选择和 provider 错误语义 | Connection 通过获授权路径提供凭证，不是向所有 Agent 授予 grant |
| Agent 推理与编排 | 理解目标、选择操作、响应结果并提出提权动作 | 模型输出不能签发权限或批准自己的操作 |
| Runtime 与调用执行约束 | 调用时检查 grant 和 approval，执行凭证托管策略，持久化进度并关联结果 | 强制边界要求限制绕过路径 |
| 目标应用 | 执行自身业务规则和资源权限，报告权威操作状态 | Runtime 不能重造或绕过应用规则 |

这些是职责，不是新增六个服务或实体的要求。SSO、provider token exchange 和
连接器的业务操作语义应能组合，而不混成一个过载概念。

### 企业 SSO 带来的变化

XAA/ID-JAG 探索将已有企业 SSO 信任关系扩展到跨应用授权。在支持它的系统中，
这可以减少重复用户 consent 和连接设置。但目标 API 与授权服务器仍须支持；
应用支持 SSO，不会自动变得可由 Agent 调用。稳定资源标识、操作语义和目标业务
权限仍属于应用集成工作。

战略假设包括：

- 企业从只检查应用成员资格，进一步复核哪个 Agent 能在何种权限和任务下执行
  哪种操作。
- 单个 Agent 可以将既有应用组合成工作流程，同时保留各目标的独立 grant，而
  非获得一个通用 token。
- 当自主 workflow 的业务归属需要在员工离职后延续时，非人主体清单和 sponsor
  转移变得必要。
- 单独获准的操作可能组合成未获授权的结果：CRM 读取加邮件发送，不等于允许
  向外部导出客户数据。经验证的场景可能需要跨调用的数据用途和接收方约束，
  超出单独 OAuth scope 的范围。
- Runtime 限制直接凭证和网络访问后，broker/gateway 才能强制约束实际调用。
  撤权停止后续工作，但不会撤回已完成效果或清除已经复制的数据。

### 连接器是业务能力契约

有用的连接器必须覆盖完成流程所需的操作，而不只是认证或搜索。发票对账可能
需要读取发票、查询订单和收货、登记异常及验证结果；付款是具有独立权限和批准
要求的另一项操作效果。

对选定操作，应检查：

- 业务含义、输入/输出结构、前置条件和读写效果；
- 在实际 grant 下选择账户、租户、资源和凭证；
- 分页、provider 限制、schema 漂移和可指导下一步的错误报告；
- 超时及部分成功语义、provider 支持时的安全重试或幂等，以及判断效果是否已经
  发生的方法；
- provider operation ID、审计关联，以及业务支持时的恢复或补偿动作。

适当的传输可以是 HTTP、MCP 或其他经审查的集成。通用工具协议不会消除 provider
语义。Skill 可以描述如何组合操作，plugin 可以打包实现；安装和 workflow 指令
都不会授予访问权。同一连接器可以服务不同 Agent，各自使用不同操作和 grant。
连接器覆盖增加时，更多任务可以执行；真实 Agent 使用又会暴露缺失操作和薄弱的
恢复语义。完成的业务流程比连接器数量更有意义。

### 产品机会与第一个证据闸门

以下机会是战略推论，不是路线图承诺：

| 参与方 | 待验证的机会 |
|---|---|
| 已有企业 IdP | 复用目录与应用信任，治理 Agent 身份、委托和凭证交换 |
| 应用提供方或连接器维护者 | 提供访问有界、效果明确、结果可验证的 Agent 业务操作 |
| BuildMax 等 Agent runtime | 将企业权限连接到私有执行、具体批准、恢复和完整证据 |
| 安全与治理提供方 | 发现无人负责的 Agent、过大的组合权限、异常数据流及撤权缺口 |

对 BuildMax，先用组织已有 IdP 和少量操作，验证工单 → 日志 → 仓库 → PR 的
完整流程。每次调用保留 subject、Agent revision、TaskRun 和 provider correlation；
验证访问拒绝、具体动作批准、结果不确定的超时，以及下次调用或恢复执行前撤权。
明确哪些控制分别由 IdP、连接器、runtime 和目标系统执行。

测量设置及维护成本、完成与人工恢复的任务、授权准确性、重试后的重复效果、
撤权延迟和审计覆盖率。由这些证据选择下一个连接器契约和权限切片，不从通用
manifest、大型目录或通用策略语言开始。标准化访问可能降低连接胶水的价值；
可靠且可治理地完成真实工作，是需要验证的战略假设。

## 大厂与社区的当前方向

### Microsoft：一等 Agent 目录对象与 sponsor

Microsoft Entra Agent ID 将 Agent identity 与人类、普通 application identity 分开。
其文档模型同时支持 autonomous access 与 delegated user access，记录可问责的 sponsor，
生成 Agent 专用审计；只有当目标系统要求 mailbox 等人类资源时，才可选地配对一个
Agent user account。后续治理材料又增加 sponsor 转移、生命周期控制、access package，
以及针对 autonomous / on-behalf-of 的不同策略。

这并不表示 BuildMax 应复制 Entra 对象模型。真正的信号是：稳定清单、技术 owner、
业务责任、执行模式与兼容性用户账号属于不同概念。合成用户账号是旧资源模型的
兼容例外，不应成为 Agent 身份默认值。

### Google Cloud：受证明的 Agent workload、凭证 broker 与 gateway

Google Cloud Agent Identity 使用与托管 Agent 资源绑定的 SPIFFE 加密身份，明确支持
用户委托、Agent 自身 cloud authority、machine-to-machine 访问，以及由 auth manager
管理的外部凭证。在 gateway 路径中，最终用户凭证可只在 gateway 解密，使 Agent
永远拿不到原始凭证。Agent Registry、Agent Identity 和 Agent Gateway 分别承担清单、
认证和执行约束。

这是最清晰的大厂分层案例之一：workload identity 表明“哪段代码在运行”；brokered
credential 表明“当前可以调用什么”；gateway 即使面对已受攻击的模型也能约束调用。

### AWS：绑定用户与 workload，再访问出站凭证

Amazon Bedrock AgentCore 记录了 user-delegated、M2M 与 on-behalf-of 模式。其 workload
access token 可以同时绑定最终用户与 Agent workload identity，并且只用于 AgentCore
第一方服务，包括 outbound credential provider。由 Runtime 管理的 Agent 不能直接
提取此 token。AWS 还记录了更弱的 caller-supplied `userId` 路径，并建议生产环境使用
经过密码学验证的 JWT 身份。

对 BuildMax 的启示是：未经验证的 `user_id` 只是来源信息，不是证明。用户主体必须
来自已认证边界，不能接受模型或 webhook payload 声称“我代表 Alice”。

### OpenAI：区分 workspace 用户凭证、service account 与运行控制

OpenAI 已公开的企业材料没有只给出一种通用 Agent 身份。Workspace Agent access
token 仅限 Workspace Agents API；Codex access token 可以代表其 workspace 创建者；
service account 则提供独立角色、group、plugin、过期与审计生命周期的非人 workspace
身份。ChatGPT Work cloud 文档还区分 connected app 的 individual、shared 与
Agent-owned account，并指出连接账号可能不同于提出任务的用户。

这里的关键信号是必须显式选择凭证 subject。“用户触发了运行”并不证明所有下游
动作都使用该用户账号；无人值守自动化需要独立生命周期。

### Okta 与 Auth0：IdP 正在成为委托 broker

Okta Agent token exchange 支持用户与机器权限、resource connection、Agent-to-Agent
调用、audience/scope 限制，并在多跳中保留原始 service identity。Auth0 Token Vault
把 broker 放在 Agent 和下游 OAuth token 之间，使 refresh token 和原始用户凭证不必
暴露给 Agent 代码。

Okta 在 2026 年 9 月发布的 Agent SSO 将一等 Agent 身份扩展到支持 Cross-App
Access（XAA）的集成。XAA 利用受 SSO 信任的 IdP 协调跨应用授权，其 ID-JAG
机制见下文。产品支持某种集成，不代表所有下游 API 都兼容，也不代表草案已经
成为正式标准。

因此 BuildMax 的集成边界应是一套 provider-neutral credential exchange interface。
企业可以选择 Vault、IdP STS、GitHub App、cloud STS 或 BuildMax 本地 provider；领域
模型描述“需要什么权限”，而不是嵌入某一家厂商的 flow。

### 社区标准：有用的积木，还没有完整的 Agent 权限模型

SPIFFE 提供受证明的 workload identity 与短期 SVID，并明确建议不要把易变化的 role
和 access policy 放入长期身份声明。OAuth 2.0 Token Exchange 区分 subject token 与
actor token，可按 resource、audience 和 scope 缩窄新 token；但交换本身不会自动让
输入和输出 token 的撤销联动。IETF WIMSE 正在研究 workload identity、OAuth、JWT、
SPIFFE 与多跳上下文之间的互操作性——这既证明问题真实，也说明组合模型尚未定型。

RFC 8693 的 JWT `sub` 与 `act` claim 可以区分权限主体和当前执行者，嵌套的 `act`
记录先前执行者。但历史记录本身不会强制权限收缩：issuer 必须执行交换策略；
consumer 根据当前 actor 与顶层 claim 判断权限，不能把先前 actor 当成额外授权。

**Cross-App Access 与 ID-JAG（工作草案）。** IETF OAuth 工作组的 Identity
Assertion JWT Authorization Grant 建立在 token exchange 和 JWT authorization
grant 之上。IdP 为已通过 SSO 信任它的下游授权服务器签发 assertion，client 按
参与系统的策略交换目标 access token。这允许跨域用户委托，无需在每个目标授权
服务器重复直接用户批准步骤；它不授予无边界访问，也不定义自主业务 workflow
的归属。截至本次核查，ID-JAG 是活跃 Internet-Draft，尚未成为已发布的 RFC。

**可验证 capability 委托（研究原型）。** AIP 论文为 MCP、A2A 与 HTTP 提出
Invocation-Bound Capability Tokens，单跳使用 signed JWT，多跳使用 Biscuit
policy chain。其参考实现探索可验证来源和持有者主动收缩权限。这是研究提案，
不是协议要求或成熟互操作标准。BuildMax 应先用真实 child-Task 场景评估这些
性质，再决定 token 格式；论文中的结论不能作为 BuildMax 的验收证据。

MCP authorization 为远端 MCP server 标准化 OAuth discovery 与 audience binding，
并明确禁止把 MCP client token 原样传给上游 API。A2A 通过 Agent Card 声明传输层认证，
要求 server 自行授权，但把资源和业务动作授权留给实现。两者都不会替企业决定：
长期 schedule 归谁所有，或某次 transaction 是否可以使用用户权限。

## 已经收敛与尚未收敛的部分

### 正在收敛

1. **非人身份成为一等清单对象。** 它们有 owner/sponsor、状态、grant、生命周期和
   audit，而不是匿名 API key。
2. **用户委托与自主权限是不同模式。** 同一个 Agent 可以兼用，但每次调用必须知道
   当前是哪一种。
3. **稳定身份与临时执行身份并存。** 目录或 Agent 定义负责治理，短期 workload
   identity 代表正在运行的实例。
4. **凭证在运行时缩窄。** 调用目标时才确定 audience、resource、scope/capability
   与 expiration。
5. **可复用 Secret 逐步移入 broker/gateway。** Agent 最好拿到 capability 或调用结果，
   而不是 refresh token。
6. **委托同时保留 subject 与 actor。** 原始权限和当前软件 actor 在 token exchange
   或 Agent-to-Agent hop 中都应可见。
7. **人类责任仍不可替代。** Sponsor/owner 生命周期存在，是因为 autonomous identity
   不能承担组织责任。
8. **互操作协议不会解决业务授权。** MCP、A2A、OAuth、SPIFFE 提供组件；企业 runtime
   仍需负责 admission、policy、approval 和 evidence。

### 尚未收敛

- 是每个 Agent 都应有目录对象，还是只有部署后且获自主权限的 Agent 才需要；
- Agent identity 对应定义、部署、tenant instance、revision 还是 runtime；
- 比粗粒度 OAuth scope 更细的可移植 capability 词汇；
- 如何把 approval evidence 绑定自然语言意图和最终参数；
- 异步多 Agent 工作中 delegation chain 应传播多远；
- 撤销如何跨越独立签发 token 与离线系统；
- 任意代码工具是否允许让凭证进入 Agent memory；
- 跨厂商通用 Agent registry 与生命周期模型。

所以 BuildMax 应采用稳定原语，但不要把今天某个厂商的对象名称暴露成永久产品模型。

## BuildMax 当前所处位置

### 已具备的良好基础

- TaskRun 已经是权威执行尝试，也是天然的临时 execution subject。
- Worker run token 是短期 token，通过 token type 区分 audience，绑定 Space、Task 与
  TaskRun，并有意比用户 token 权限更窄。
- Space 是可信的组织归属边界。
- Agent revision 与 TaskRun provenance 可以标识真正执行的代码/配置。
- Space Secrets 集中加密凭证材料、审计交付，并从 trace 中 redact 已知 Secret。
- 官方 Worker 有 fail-closed sandbox 和单一可写 workspace root。
- Plugin、MCP、hook、tool permission 与 trace 都可以成为策略和证据附着点。

### 缺口与语义过载

1. `task_run.created_by` 同时承担 provenance 和 execution eligibility。个人任务可以，
   但无法表达“创建者离职后仍应继续”的组织 schedule。
2. Run token 以用户作为 `sub`，没有独立表达 authority mode、automation principal、
   Agent actor、target 或 capability。
3. Space Secrets 主要把凭证交进运行。Redaction 可以减少意外泄漏，不能阻止被攻击的
   Agent 读取并导出值。
4. 目标服务通常只看到 provider credential，看不到导致调用的 BuildMax TaskRun 与
   Agent revision。
5. 当前 approval 与 tool permission 还不是通用、参数绑定的外部动作授权契约。
6. Schedule `created_by` 保留来源，却会在没有新归属模式时把长期组织权限绑定到个人。
7. 通用 shell 与网络可以绕过 typed connector 或 gateway。
8. 目前没有一等 non-human principal 生命周期、sponsor 转移、grant review 或 disable。
9. Parent TaskRun 跨信任边界创建 child TaskRun 时，没有有界 delegation chain。

这些缺口不意味着立刻增加九个实体，而是界定分阶段设计必须回答的问题。

## 典型实际场景

### 1. 交互式代码助手：以用户身份执行

Alice 要求 Agent 检查她可访问的 repository 并创建 pull request。BuildMax 认证 Alice，
将她同时记录为 initiator 与 authority principal，为 TaskRun 分配独立 workload identity，
再申请只限一个 organization、具备必要 read/write capability 的短期 GitHub credential。
本地 draft 无需批准；远端创建 PR 可按 Space policy 要求确认。

价值：Alice 的 repository membership 变化会影响后续访问；审计能区分 Alice、Agent
revision 与具体 run；Worker 内没有可复用的 Alice refresh token。

### 2. 每月财务对账：以组织身份执行

财务管理员发布每月 workflow：读取共享邮箱、将发票与 ERP 对账、生成异常报告并
提出付款。如果使用创建者个人身份，创建者离职后要么流程中断，要么以过期个人权限
悄悄继续。

Finance 明确向 Agent 授权，设置 human sponsor、固定资源以及过期/复核规则。
如果业务授权必须独立于这个 Agent 延续，Finance 则使用单独的 Space-owned
automation principal，并明确哪些 actor 有资格使用它。每次 schedule TaskRun 获得
短期 identity 与 brokered credential。报告可自动生成；付款需要另一项 capability，并将批准绑定 payee、amount、
currency 与源记录。

价值：人员变化不再模糊业务连续性的归属，高风险权限不与普通对账权限捆绑。

### 3. Incident Response Agent：临时提权

On-call 工程师要求 Agent 排查生产故障。普通 grant 只允许读 metrics 与 logs。Agent
提出重启 `prod-sg` cluster 的 `payments-api`，并展示依据。Approver 只批准这一操作，
有效期十分钟。Broker 签发或执行单次 action-bound capability；变更 cluster、service
或 operation 都会使批准失效。

价值：快速干预不需要向整个 run 提供通用生产管理员凭证；诊断、提案、批准、动作和
观测结果形成一个证据链。

### 4. 客服分流：一次运行混合多种权限

Agent 先用组织 grant 读取 Space-owned support queue，然后需要用发起客服
主管的身份读取受限客户 case，最后再以 Space-owned bot 发布脱敏摘要。

价值：权限按目标调用选择；系统不会假装一个 run 只有一个通用身份，也不会把最高
权限 credential 复制给整个进程。

### 5. 管理者 Agent 委托专业 Agent

规划 Agent 要求部署专家验证 release。Child 只获得 repository read、test environment
deploy 与回报结果能力，不继承 production deploy。Chain 记录人类或 automation
principal、parent TaskRun、child Agent revision 和经过削弱的 grant。除非 policy 允许
另一个有界 hop，child 不能继续委托。

价值：multi-Agent composition 变得可解释、least privilege，而不是传递 bearer token。
在 BuildMax 有真实 durable child Task 且可验证 attenuation 之前，不应交付此能力。

### 6. 使用既有 Vault 与 IdP 的私有部署

受监管客户在 Kubernetes 上运行 BuildMax，以自己的 OIDC provider 管人、SPIFFE/SPIRE
管 workload、Vault 签发数据库 lease。BuildMax 把可移植 authority envelope 映射到
这些系统，不强迫客户采用 BuildMax directory。另一套离线部署可使用 BuildMax 签名的
TaskRun JWT 与本地 secret provider，同时保持同一领域契约。

价值：私有部署从“部署选项”变成架构优势。企业保留 root of trust，BuildMax 提供
Agent 专用执行和证据平面。

## 战略方案选择

| 方案 | 最适合 | 优势 | 结构性缺陷 |
|---|---|---|---|
| A. 保持 `created_by` 权限，只改进 Secret 交付 | Alpha 简化与个人自动化 | 概念少，复用现有资格检查 | 共享自动化仍绑定个人，来源与权限继续过载 |
| B. 每个 Agent 都是持久 principal | 企业 autonomous Agent 清单 | 便于禁用、grant、审计 | 定义身份与运行实例混淆，容易累积宽泛 standing privilege |
| B1. 直接向选定的受 Space 治理的 Agent 授权 | grant 与 Agent identity 共用生命周期的自主工作 | 复用 Agent 身份，不新增业务授权实体 | 替换 Agent 需显式重新授权；不适合业务授权必须独立于执行者延续的场景 |
| C. 始终代表发起用户执行 | 交互式个人助手 | 自然 consent，复用既有 entitlement | Schedule、webhook、共享任务与离职无解；用户凭证价值过高 |
| D. 增加 Space-owned automation principal | 共享 Schedule 与团队操作 | 非人生命周期、sponsor 与稳定权限清晰 | 新增生命周期和恢复 UX；不当默认会让每个 Agent 都变 service account |
| E. 只使用临时 TaskRun workload identity | 联邦基础设施与 S2S | 爆炸半径小，每 run 审计强 | 下游必须支持 federation，仍缺稳定 policy subject |
| F. 凭证置于 broker/tool gateway 后 | Typed connector 与高价值 API | 模型看不到可复用凭证，策略与审计集中 | 任意 Bash 若不受限仍可绕过，provider 集成成本高 |
| G. 稳定 principal + Agent actor + 临时 run + broker | 混合交互/自主企业任务 | 不混淆用户、组织、run 与 target | 组件更多，必须按证据切片交付 |

方案 A 是合理近期状态，不是长期企业答案。B 或 C 单独采用都会过拟合一种产品形态。
B1 是更简单的独立授权基线；只有确需独立生命周期时才选择 D。
D 解决归属但不解决 runtime attestation 和 Secret 暴露。E 解决执行身份但不解决业务
权限。F 是最强凭证边界，却无法诚实覆盖不受限本地工具。G 是建议目标；它的每个概念
都对应一个已经明确的生命周期失败，通过延迟未被证据触发的切片来控制成本。

## 建议的目标架构

### 1. 三层身份

**Policy principal。** 已认证用户或明确获授权的非人身份：受 Space 治理的 Agent
自身，或满足前述生命周期闸门后新增的 Space-owned automation principal。它拥有
grant 并接受 active 状态检查。独立组织权限需要 human sponsor、purpose、expiry
或 review date，以及 disable 路径。创建或发布 Agent definition 本身不会授予权限。

**Agent actor。** 稳定 Agent identity 与 immutable revision 标识哪套软件配置执行，
用于 inventory、allow/deny policy、incident search 与 rollout。识别 actor 不等于
证明权限；即使 Agent 同时是 principal，也需要明确的 grant。

**TaskRun workload。** 短期身份标识精确执行，绑定 Space、Task、Agent revision、
runtime profile 与 expiry。它可以请求 capability，不能扩大 capability。

### 2. 显式 authority mode

先使用封闭枚举，而不是通用策略语言：

| 模式 | 权限来源 | 用途 |
|---|---|---|
| `user_delegated` | 已认证人类及 provider consent/grant | 个人或用户受限资源的交互任务 |
| `organization_grant` | 向受 Space 治理的 Agent 或确有必要的独立 automation principal 明确授予的 grant | 共享 Schedule、Webhook、团队后台工作 |
| `approved_elevation` | 既有 principal 加 operation-bound approval | 正常 grant 之外的一次高风险效果 |
| `system_internal` | 部署运维策略 | 只用于狭窄 BuildMax 维护，不能作为访问业务数据的捷径 |

Schedule 或 webhook 是 initiator，不是 authority mode；它必须指向有资格的 policy
principal。Child Agent 是 actor，不是新权限来源；它从 parent 获得 attenuated grant。

### 3. 可移植 authority envelope

控制平面应能生成并持久化等价于下列示例的 envelope。此例使用独立 automation
principal；直接向 Agent 授权时，`authority.principal_id` 则指向 Agent，同时继续
将 actor 与具体 run 作为独立证据记录：

```json
{
  "initiator": {"type": "schedule", "id": "sch_..."},
  "authority": {
    "mode": "organization_grant",
    "principal_id": "ap_...",
    "grant_id": "gr_..."
  },
  "workload": {
    "space_id": "spc_...",
    "agent_id": "agt_...",
    "agent_revision_id": "ar_...",
    "task_id": "tsk_...",
    "task_run_id": "tr_..."
  },
  "target": {
    "audience": "github",
    "resource": "repo:acme/payments",
    "capabilities": ["contents:read", "pull_requests:write"]
  },
  "approval": {
    "id": "apr_...",
    "operation_digest": "sha256:..."
  },
  "expires_at": "2026-10-01T10:05:00Z"
}
```

它可以分布在 DB snapshot、run token claim、broker request、provider token 与 audit
record 中。不要把易变 sponsor role 或通用 policy 放入长期 identity certificate；
每次签发 lease 前由 authorization service 检查最新状态。

### 4. 凭证交换，而不是凭证交付

建议 flow：

```text
authenticated trigger
  -> TaskRun authority snapshot
  -> attested TaskRun requests target capability
  -> broker rechecks principal, Agent, grant, target, and approval
  -> broker exchanges or invokes using a short-lived target credential
  -> outcome and provider correlation ID join the TaskRun trace
```

Provider 实现可以是 GitHub App installation token、Vault dynamic lease、cloud STS、
OAuth token exchange、remote MCP authorization server 或 on-prem custom provider。
BuildMax 通用接口应表达 audience、resource、capabilities、最大 lifetime 与 user/
automation subject，不假设全部提供方都是 OAuth。

Typed tool 优先采用 brokered invocation，使 provider credential 不进入 Agent address
space。必须给任意 CLI credential 时，签发尽可能窄的短期 lease，并标记该 run 已暴露
credential。静态环境变量注入应是最后一级兼容方案。

### 5. Approval 是 capability，不是聊天消息

Approval 应绑定：

- authority principal 与 approver；
- Agent revision 与 TaskRun，或明确可恢复的 operation；
- operation name、target resource 与规范化 parameter digest；
- 最大效果、expiration 与 use count；
- policy version 和展示给人的 intent。

相关参数变化时重新请求批准。批准一次操作，不会把 run 剩余部分变成管理员。

### 6. 每个关键调用记录内外两种身份视图

内部视图记录完整 BuildMax envelope 与 decision；外部视图记录真正执行动作的 provider
principal/token 和 correlation ID。下游可能只看到 GitHub App 或 service account，而
BuildMax 知道原始用户、Agent revision 与 TaskRun；运维者需要同时看到两者。

## 授权生命周期

1. **Register：** 发布或激活 Agent revision，可关联 Space、runtime profile、target 与
   sponsor。注册本身不授予外部权限。
2. **Grant：** 用户委托选定 provider access，或管理员给有资格的非人主体授予
   有界 capability。记录 approver、原因、资源、review/expiry 和是否可继续
   委托。
3. **Admit：** Trigger 创建 TaskRun。Service 认证 initiator，解析 authority principal
   与 mode，检查当前资格，并 snapshot decision input。
4. **Attest：** Worker 证明自己是获准 TaskRun 和正确 runtime。第一阶段可用 run token，
   后续可用 OIDC 或 SPIFFE workload identity。
5. **Exchange / invoke：** 每个 target 调用前，broker 重查当前状态、削弱 capability、
   验证 approval，再取得短期 credential 或代为调用。
6. **Renew：** 长运行只有再次通过资格和策略检查才续租。续租不是原始 principal 仍然
   有效的自动证明。
7. **Record：** Decision、target、operation、parameter/digest、provider correlation、
   output classification 与 result 进入有界 trace 和持久 audit stream。
8. **Revoke：** Disable user、automation principal、Agent、grant、connection 或 Space
   后立即停止签发新 lease。既有 lease 尽快过期或在 provider 支持时撤销；运行明确
   进入 cancelled / authority-lost，而不是静默继续。
9. **Review / retire：** Sponsor 定期复核 standing grant。Retire Agent 或 automation
   principal 时撤销 connection，但保留不可变历史 attribution。

## 若获采纳的分阶段方向

### Stage 0：修正词汇与证据模型

- 在设计与 trace 中拆开 `initiator`、`authority_principal`、`authority_mode` 与 Agent
  actor；在代码变更获接受前保留 `created_by` 作为 provenance。
- 定义固定 capability request 与 authority snapshot，不引入通用 policy DSL。
- 文档明确说明 schedule creator 不一定是长期 authority owner。
- 定义 authority loss 与 credential renewal failure 的终态。

成功证据：本文六个场景都可表达，并且不把 webhook 或 Agent definition 当成人类
principal。

### Stage 1：验证一个短期 provider exchange

实现 Space Secrets 已预期的一个高价值 provider，例如 GitHub App installation token
或 Vault dynamic lease。将签发绑定 TaskRun、target、maximum lifetime 与 capability，
增加脱敏 audit 与 revocation test。

成功证据：该 journey 中没有静态 provider credential 进入 Worker；被盗 token 在
provider 能力允许时不能用于其他 audience/resource；disable grant 后无法续租。

### Stage 2：组织权限与主体生命周期决定

先用直接向 Agent 授权验证具名的组织 workflow。部门级
[Space Assistant](space-assistants.md#8-授权)的请求者在该 Space 中没有任何权限，
它是一个具体的交互式候选场景。至少需要 sponsor、purpose、
active/disabled、grant set、创建/更新 provenance，以及 review 或 expiry。只有
业务授权必须跨 Agent 替换延续或覆盖多个 actor，且显式重新授权不能满足场景时，
才新增独立 automation principal。定义并检查合格 actor，不能自动向替代 Agent
转移 grant。个人 schedule 保持 `user_delegated`；团队 schedule 必须显式转换为
`organization_grant`。

成功证据：creator offboarding 会停止个人自动化，但不会让明确组织拥有的 workflow
变孤儿；sponsor transfer 与 disable 易于理解且有 audit。替换 Agent 必须显式
授予新权限，或审计独立主体的合格 actor 变更。

### Stage 3：TaskRun workload federation

实现 rotating issuer/JWKS 与 audience-bound TaskRun token，或者为已有 SPIRE 的部署
提供 bridge。可移植 BuildMax claim 应只包含 immutable run/workload property，不包含
易变 role。

成功证据：至少一个真实 relying party 信任 TaskRun federation；key rotation 与 expiry
有效；移除一项静态 Worker credential，而不是单纯再增加 token。

### Stage 4：Brokered connector 与 approval path

把一个类型化高价值 connector 或 remote MCP action 放到持有 refresh/static
credential、评估 policy 并执行/交换调用的服务后。高风险写操作绑定规范化参数。

成功证据：Agent 无法读取 reusable credential；direct network bypass 在 hardened
runtime profile 中要么被阻止，要么明确报告；audit 可以连接内部身份和 provider 身份。

### Stage 5：有界 Agent-to-Agent delegation

只有当 durable child Agent Task 跨越授权边界后，才记录带 maximum depth、audience、
能力只减不增的 delegation chain。绝不能把 upstream bearer token 直接转发作为机制。

成功证据：Child 不能获得 parent grant 不含的 capability；每一跳可见；loop/depth
有界；撤销后无法签发新的 downstream lease。

### 明确推迟

- 通用企业 policy language；
- 全球跨组织 Agent identity network；
- 为每个 Agent 自动创建用户账号、inbox 或 mailbox；
- 为所有外部应用统一 transaction semantics；
- 让 Agent 自己选择 authority mode 或 sponsor；
- 声称 connector policy 可以保护不受限本地 Bash。

## 威胁模型与失败语义

| 威胁 | 必须采取的响应 |
|---|---|
| Prompt injection 要求输出 Secret | 优先 brokered invocation；reusable credential 不进入 model context 或 trace |
| Confused deputy 把合法 token 交给错误服务 | 绑定并验证 audience/resource；禁止 token passthrough |
| Webhook 或模型声称自己是 Alice | 只从已认证 server context 推导 user；payload identity 是不可信数据 |
| Creator 离职但 Schedule 继续 | 个人权限失去资格；组织权限只有获授权的非人主体 active 且 sponsor policy 满足时继续 |
| Shared service account 掩盖权限来源 | 除外部 shared principal 外保留 initiator、Agent、TaskRun、target 与 provider correlation |
| 被盗 run/provider token replay | 短 expiry、单一 audience、窄 resource、可用时采用 proof-of-possession，并关联 run/lease |
| Approval 被用于另一动作 | 绑定 operation、规范化参数、resource、expiry 与 use count |
| Approval 后参数变化 | 执行时重算 digest；不匹配则 fail closed 并重新批准 |
| Child Agent 扩大权限 | 每一跳交换 attenuated grant；限制 depth，默认不可继续委托 |
| 长运行期间 grant 被撤销 | Broker 拒绝新 lease/续租；runtime 返回 `authority_lost` 并停止相关工作 |
| IdP、Vault、STS 不可用 | 不回退到静态或更宽凭证；只在 lease 安全窗口内有限重试，然后显式失败 |
| Signing key 轮换 | 验证 key 有重叠窗口，受损 key 停止签发，token lifetime 保持短暂 |
| 任意 Bash 绕过 broker | Hardened profile 限制 egress/tool；否则说明边界只是 advisory，并尽量缩窄暴露 lease |
| Credential 出现在 log/trace | Redact 已知材料，结构化排除 secret field，扫描 fixture；trace failure 与 credential policy 分离 |

授权和凭证签发必须 fail closed，即使普通 trace 写入仍保持 fail-open。可观测性丢失不能
静默授予权限；权限丢失不能被报告为成功的 Agent 结果。

## 决策标准与所需证据

### 产品标准

- 用户在执行前能看懂 run 是“代表我”还是“代表组织”。
- Sponsor 可以找到、禁用、复核、转移其负责的所有 non-human authority。
- 一个 TaskRun 可以针对不同 target 使用不同 authority subject，而拿不到 reusable
  credential。
- 私有部署可以集成现有 IdP、Vault、cloud STS 或 SPIFFE，无需采用 BuildMax 托管的
  identity root。
- 本地宽工具 workflow 仍可使用，但更弱的 credential isolation 必须明确说明。

### 安全验收旅程

1. **交互式 GitHub：** Alice 能在一个 repository 创建 PR；移除 repository access 后，
   下次 exchange/call 停止。
2. **组织 Schedule：** 月度对账只有在显式转换为组织归属且 sponsor 有效后，才能在
   creator 离开后继续。
   Sponsor 转移不扩大 grant。替换 Agent 需要新的直接 grant，或由独立主体明确
   授权新 actor；旧 actor 退役后失去资格。
3. **生产提权：** Restart approval 不能用于 deploy、其他 cluster、其他 service 或
   变更后的参数。
4. **Audience theft：** 复制到其他 service/run 的 TaskRun 或 target token 被拒绝。
5. **Credential custody：** Brokered path 中 reusable credential 不进入 Agent address
   space，因此 Agent 无法打印 refresh token 或长期 key。
6. **Delegation attenuation：** Parent-to-child 权限只减不增，且不能超过 chain depth。

### 运维证据

- 正常与峰值的 issuance/broker latency；
- revocation latency，包括 provider 无法撤销既有 token 的情况；
- issuer、Vault、IdP、network 与 clock 故障时的行为；
- signing-key rotation 和加密 connection state 的 backup/restore；
- BuildMax 与 provider correlation ID 的 audit join rate；
- approval、denial、abandoned run 与 bypass request 的比例；
- 私有及 air-gapped deployment profile 的支持负担。

### 决策闸门

- 不能仅为独立于 creator 而增加单独的 automation principal。须证明业务授权需要
  独立于执行 Agent 的生命周期，以及直接授权和显式重新授权为何不能满足需求。
- 没有真实 relying party 消费 workload identity，且不能因此移除静态 credential 或
  获得可量化 enforcement 前，不增加 workload issuer。
- 不先建设 generic broker；先验证一个 provider 和一个 journey。
- 在 BuildMax 没有跨 trust/policy boundary 接纳 durable child Agent 前，不做 multi-hop。
- 固定的 authority mode、resource、capability、expiry、approval 足以表达已验证需求时，
  不发明 policy DSL。
- 所宣称的 runtime profile 中 egress 与任意 tool 仍能绕过边界时，不称其为 hardened。

## 待回答的产品问题

1. 第一个组织授权场景能否直接向 Agent 授权？如果需要独立 Space-owned automation
   principal，哪种业务授权必须独立于 actor 延续？主体成为可见产品对象，还是限制在
   Workflow/Schedule 背后？
2. 每个 published Agent 都需要稳定 identity，还是只有获 autonomous access 的 Agent
   才需要？什么生命周期事件创建和 retire 它？
3. 混合用户与组织权限的 TaskRun 应展示一个 primary mode，还是按 call 展示 authority
   timeline？
4. 第一个 provider 选 GitHub App、Vault、cloud STS 还是 remote MCP OAuth，哪个最能以
   最低负担验证架构？
5. BuildMax 是否在某些部署中成为 automation principal 的 source of truth，还是已有
   enterprise IdP 时始终做 mapper？
6. 哪些 hardened runtime profile 可以诚实保证 raw credential 与 direct network path
   对 Agent code 不可用？
7. 什么是可以安全跨 pause/resume、同时阻止 TOCTOU substitution 的最小 approval object？
8. 另一个 Space member 继续 Task 时，新工作应始终使用新成员权限、保留组织权限，
   还是针对每个 target 显式选择？
9. 没有 manager hierarchy 或自动 identity provisioning 的部署中，sponsor 离职如何处理？
10. Authority envelope 的哪些部分进入有界 trace、持久 audit log、provider token 与
    运维 UI？
11. 哪条跨应用流程能同时证明连接器覆盖有用、企业统一授权有效？哪个缺失操作
    实际阻止了工作完成？
12. 哪些连接器语义必须跨传输共享？哪些 provider 特有的重试、业务规则和恢复
    行为应保留在集成里，而不抽象成通用 runtime 能力？

## 获采纳后的可能归宿

获采纳的理由应进入一份聚焦 Agent execution identity 与 authority 的设计记录。具体
切片还应更新：

- [Agent 执行与 Task 线程](../design/Agent执行与Task线程.md)：provenance、authority mode
  与 TaskRun admission；
- [Worker Run Token](../design/Worker运行令牌.md)：workload claim 与 issuer/audience rule；
- [Space Secret](../design/Space密钥.md)：provider exchange、workload federation 与
  credential custody；
- [定时 Agent 执行](../design/定时Agent执行.md)：个人与组织 ownership；
- [Agent 代表用户使用应用](agent-app-delegation.md)：brokered connector 与
  action-bound approval；
- 只有证据闸门选择出可实施切片后，才进入 roadmap 与 backlog。

## 参考资料

以下资料用于证明当前方向，不代表 BuildMax 将复制某厂商能力。

### 企业平台与身份厂商

- Microsoft：[Agent identity 是什么](https://learn.microsoft.com/en-us/entra/agent-id/what-are-agent-identities)、
  [Agent identity 概念](https://learn.microsoft.com/en-us/entra/agent-id/key-concepts)、
  [管理 Agent identity](https://learn.microsoft.com/en-us/entra/agent-id/manage-agent-identities-admin)。
- Google Cloud：[Agent Identity overview](https://docs.cloud.google.com/iam/docs/agent-identity-overview)。
- AWS：[AgentCore workload access token](https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/get-workload-access-token.html)。
- OpenAI：[Workspace Agent access token](https://learn.chatgpt.com/workspace-agents/authentication)、
  [service account](https://learn.chatgpt.com/docs/enterprise/service-accounts)、
  [access token](https://learn.chatgpt.com/docs/enterprise/access-tokens)、
  [ChatGPT Work cloud security](https://learn.chatgpt.com/docs/enterprise/chatgpt-work-cloud-security)。
- Okta：[AI Agent token exchange](https://developer.okta.com/docs/guides/ai-agent-token-exchange/secret/main/)
  与 [AI Agent lifecycle](https://developer.okta.com/docs/api/secures-ai/ai-agents)。
- Okta：[Agent SSO 公告](https://www.okta.com/newsroom/press-releases/okta-brings-first-class-identity-to-ai-agents-with-agent-sso/)。
- Auth0：[Token Vault](https://auth0.com/features/token-vault)。

### 社区协议与标准

- SPIFFE：[核心概念](https://spiffe.io/docs/latest/spiffe/concepts/)与
  [SPIFFE ID/SVID](https://spiffe.io/docs/latest/spiffe-specs/spiffe-id/)。
- IETF：[WIMSE](https://datatracker.ietf.org/group/wimse/about/)。
- IETF：[OAuth 2.0 Token Exchange, RFC 8693](https://www.rfc-editor.org/rfc/rfc8693.html)。
- IETF OAuth 工作组：[ID-JAG / Cross-App Access](https://datatracker.ietf.org/doc/draft-ietf-oauth-identity-assertion-authz-grant/)
  （截至 2026-10-03 为活跃 Internet-Draft，尚未成为已发布的 RFC）。
- Model Context Protocol：
  [2026-07-28 specification release](https://blog.modelcontextprotocol.io/posts/2026-07-28/)
  与 [Authorization specification](https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/docs/specification/2025-06-18/basic/authorization.mdx)。
- A2A Project：[协议规范](https://github.com/a2aproject/A2A/blob/main/docs/specification.md)与
  [Enterprise-ready security guidance](https://github.com/a2aproject/A2A/blob/main/docs/topics/enterprise-ready.md)。

### 研究原型

- Sunil Prakash：[AIP: Agent Identity Protocol for Verifiable Delegation Across MCP and A2A](https://arxiv.org/abs/2603.24775)
  （2026-03-25；附参考实现的研究论文，不是已接受的标准）。
