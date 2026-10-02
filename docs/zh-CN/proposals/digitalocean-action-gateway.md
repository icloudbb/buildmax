# DigitalOcean Action Gateway 架构评估

> **翻译说明：** 本文是[英文原文](../../proposals/digitalocean-action-gateway.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
>
> **受众：** 贡献者、产品设计者、运维人员与安全审查者 · **状态：** 提案——讨论中
>
> **开启日期：** 2026-10-02

相关文档：[DigitalOcean 托管 Agent 基础设施评估](digitalocean-managed-agent-infrastructure.md)、
[Agent 执行身份与委托](agent-execution-identity-and-delegation.md)、
[Agent 代表用户使用应用](agent-app-delegation.md)、
[工具权限](../design/工具权限.md)、[Space 密钥](../design/Space密钥.md)、
[工具架构](../contribute/architecture/tools.md)与
[MCP Server](../../../manual/mcp.md)。

## 目录

- [1. 决策问题](#1-决策问题)
- [2. 必要结果与当前约束](#2-必要结果与当前约束)
- [3. 研究范围与可信度](#3-研究范围与可信度)
- [4. Action Gateway 是什么](#4-action-gateway-是什么)
- [5. 重建的架构](#5-重建的架构)
- [6. 资源与权限模型](#6-资源与权限模型)
- [7. 工具发现与模型上下文](#7-工具发现与模型上下文)
- [8. 策略与审批](#8-策略与审批)
- [9. 凭证代理](#9-凭证代理)
- [10. 执行与失败语义](#10-执行与失败语义)
- [11. 运维、隐私、限制与成本](#11-运维隐私限制与成本)
- [12. 与 BuildMax 的比较](#12-与-buildmax-的比较)
- [13. 对 BuildMax 的启示](#13-对-buildmax-的启示)
- [14. BuildMax 候选方向](#14-buildmax-候选方向)
- [15. 产生证据的实验](#15-产生证据的实验)
- [16. 风险与开放问题](#16-风险与开放问题)
- [17. 若获采纳的可能归宿](#17-若获采纳的可能归宿)

## 1. 决策问题

DigitalOcean Action Gateway 中哪些设计具有架构价值？BuildMax 是否应把它作为可选的
外部工具代理来使用？哪些概念值得吸收，同时又不把一个 Public Preview 服务变成产品
的必需组成部分？

候选答案是：

1. 最重要的设计不是托管 MCP endpoint，而是把工具资格、执行权限、外部账号授权、
   模型上下文暴露、结果投影与执行可靠性分开。
2. BuildMax 应继续让 TaskRun 成为权威的授权与 provenance envelope。Gateway Session
   是解析后的 capability attachment，不是 TaskRun 或 Session 的替代品。
3. BuildMax 应首先把 Action Gateway 当成可选的远端 MCP 与凭证 broker 后端评估。在
   真实工具规模和复用需求出现前，不应自建 provider catalog 或新增 Toolbelt 产品。
4. 最值得原生吸收的经验，是把 provider Connection 与 Secret 区分开。Secret 把字节
   交给 run；Connection 让 broker 在不向 run 暴露字节的情况下行使权限。

本文记录研究和候选方向，不把集成写进路线图。

## 2. 必要结果与当前约束

### 2.1 必要用户结果

BuildMax 用户应能让 Agent 代表外部账号执行操作：权限范围可理解、后果性动作在正确
边界审批、审计记录与 TaskRun 关联；如果 brokered call 已足够，则不必把可复用的
provider token 交给 Agent。

运维人员应能在托管 broker、私有部署 MCP Server 与直接 run-level credential 之间
选择，而不改变 Task、TaskRun、审批和结果 provenance 的含义。

### 2.2 问题值得研究的证据

BuildMax 已有三部分答案：

- 远端/stdio MCP 配置；`LoadMcpTools` 与 `CallMcpTool` 避免把每个 schema 都放入初始
  工具列表；
- `allow`、`ask`、`deny` 工具策略，逐次调用风险检查、Session grant 与交互式审批；
- 由不可变 Agent revision 选择并向 TaskRun 物化的 Space Secret。

这些基础仍留下一个已经证实的缺口。Space Secret 会交给 run，Agent 能读取它。实验性
本地 app connector 把 OAuth credential 留在 OS 存储并暴露固定操作，但它不是
Space-scoped、per-run、可审计的委托路径。远端 MCP 认证依赖 bearer-token 环境变量，
没有 OAuth、refresh、多账号选择或 Server-owned Connection 生命周期。

Action Gateway 的价值在于，它把这些缺失部分视为一个完整执行边界，而不是更多环境
变量。

### 2.3 当前约束

- BuildMax 必须继续适用于本地和私有部署。任何 DigitalOcean 资源都不能成为必需的
  领域实体。
- Space 是 Server 所有权和授权边界。自由格式的外部 actor identifier 不能取代
  BuildMax account、membership 或 execution identity。
- TaskRun 拥有一次 turn/attempt、其授权快照和权威结果。外部调用日志只能补充，不能
  取代它。
- 当前优先级仍是私有部署 Beta qualification。除非能直接关闭 qualification 中的
  安全风险，否则可选 managed gateway 集成应排在该门槛之后。
- BuildMax 仍是 Alpha。若 Connection 或 capability model 获采纳，应整体实现，而不应
  隐藏在兼容层后面。

## 3. 研究范围与可信度

本评估于 2026-10-02 核验，只使用 DigitalOcean 官方文档与 API reference。主要来源：

- Action Gateway 的[概览](https://docs.digitalocean.com/products/managed-agents/action-gateway/concepts/overview/)、
  [Session](https://docs.digitalocean.com/products/managed-agents/action-gateway/concepts/sessions/)、
  [Actor](https://docs.digitalocean.com/products/managed-agents/action-gateway/concepts/actors/)与
  [Connection](https://docs.digitalocean.com/products/managed-agents/action-gateway/concepts/connections/)；
- [工具策略](https://docs.digitalocean.com/products/managed-agents/action-gateway/concepts/tool-policies/)、
  [Server-side 审批](https://docs.digitalocean.com/products/managed-agents/action-gateway/how-to/configure-server-side-approval/)与
  [可靠执行](https://docs.digitalocean.com/products/managed-agents/action-gateway/concepts/reliable-execution/)；
- [工具搜索](https://docs.digitalocean.com/products/managed-agents/action-gateway/concepts/tool-search/)、
  [invoke](https://docs.digitalocean.com/products/managed-agents/action-gateway/concepts/invoke-tool/)、
  [代码执行](https://docs.digitalocean.com/products/managed-agents/action-gateway/concepts/code-execution-tool/)、
  [Toolbelt](https://docs.digitalocean.com/products/managed-agents/action-gateway/concepts/toolbelts/)与
  [Output View](https://docs.digitalocean.com/products/managed-agents/action-gateway/how-to/add-tool-output-view/)；
- [自定义 provider](https://docs.digitalocean.com/products/managed-agents/action-gateway/how-to/add-custom-tool-provider/)、
  [生产指南](https://docs.digitalocean.com/products/managed-agents/action-gateway/how-to/use-in-production/)与
  [公开 API](https://docs.digitalocean.com/reference/api/reference/action-gateway/)；
- [限制](https://docs.digitalocean.com/products/managed-agents/action-gateway/details/limits/)、
  [定价](https://docs.digitalocean.com/products/managed-agents/action-gateway/details/pricing/)、
  [使用 Insights](https://docs.digitalocean.com/products/managed-agents/action-gateway/how-to/track-usage/)与
  [数据隐私](https://docs.digitalocean.com/products/managed-agents/action-gateway/details/data-privacy/)。

DigitalOcean 公开的是外部行为，而不是私有实现。第 5 节根据这些 contract 重建了满足
行为所需的最小架构，并明确标为推断。由于产品处于 Public Preview，所有能力在生产
决策前都必须重新 qualification。

本文没有进行付费或带凭证的 Action Gateway 调用。依赖真实 provider、approval
client、故障注入或 rate limit 的运维行为，仍是第 15 节实验需要验证的假设。

## 4. Action Gateway 是什么

Action Gateway 是托管的工具控制面和执行面。它不依赖 Harness Runtime：Agent 可以
运行在笔记本、其他云或应用框架中，然后连接一个 Session 专属的 Streamable HTTP
MCP endpoint。Python 与 TypeScript SDK 也可创建 Session，并直接调用已知工具。

它组合了六类能力：

1. 版本化 provider operation 目录；
2. 工具 schema 的按需发现与可选 preload；
3. Session-scoped selection 与 `allow` / `ask` / `deny` 策略；
4. 面向外部账号的 OAuth 或 API-key Connection；
5. brokered execution、retry、output projection 与 trace 聚合；
6. 用于计算和 programmatic tool calling 的临时 Python sandbox。

它不是：

- Agent loop 或 conversation store；
- 确定性 workflow engine；
- 跨工具 transaction coordinator；
- 使用它的应用的 identity provider；
- 能收窄 provider 原生 token 过宽 scope 的机制；
- 为全部 provider call 提供的通用 VPC proxy；
- 对同一 Agent 可在 Gateway 外访问的工具实施约束的边界。

最后一点很重要：限制严格的 Action Gateway Session 不会约束 Agent 已经拥有的其他
MCP Server、shell command、browser 或 provider SDK。

## 5. 重建的架构

### 5.1 控制面与数据面

文档描述的行为至少隐含两个平面：

```text
控制面

  Public Provider -----> 版本化 Tool -----> Toolbelt
          |                    |                 |
  Custom MCP Server            +-- Output View  |
          |                                      |
  Team / DO User ---> Actor ---> Connection      |
          |                 Provider Account      |
          +----------------------+----------------+
                                 |
                           Gateway Session
                  Selection + Policy + Context Config
                                 |
                           Managed MCP URL

数据面

  Agent / Application
          |
          v
  验证 Session owner 与 Team
          |
  解析不可变 Session 配置
          |
  检查工具 Selection
          |
  评估 Policy，并在需要时绑定 Approval
          |
  解析 Actor Connection 与 Provider Credential
          |
  参数校验 -> 执行 -> 有界 Retry
          |
  应用 Output View -> Trace -> 返回结果
```

这是根据公开 contract 作出的推断，不是对 DigitalOcean 内部服务的断言。但它仍是有用
的架构读法，因为每个阶段有不同的所有权与失败语义。

### 5.2 为什么必须分开

传统 MCP client 往往把所有内容压进一个配置项：Server URL、token、可用 schema 和
执行一起出现。Action Gateway 刻意拆开以下维度：

| 维度 | 问题 | Action Gateway 控制 |
|---|---|---|
| Catalog eligibility | 这个 Session 是否能触达该操作？ | Selected Tool 或 Toolbelt |
| 模型可见性 | 模型是否在搜索前看到 schema？ | Preload 或 Search |
| 调用权限 | 这次具体调用是否可执行？ | Policy 与可选参数匹配 |
| 外部身份 | 哪个 provider account 执行？ | Actor 选择的 Connection |
| 结果暴露 | 哪些返回字段抵达 client/model？ | Output View |
| 重放行为 | 瞬时故障是否可安全 retry？ | Catalog execution policy 与 provider idempotency |

这些控制不能互相替代。Preload 不会授予权限；`allow` 规则不能扩大工具 Selection；
审批不会创建 OAuth Connection；Output View 不会缩小 provider scope；retry policy 也
不会让多工具 workflow 具备事务性。

### 5.3 不可变 Gateway Session

Gateway Session 把 Actor、工具 Selection、Policy、Instructions、Preload、Output
View 和可选 VPC attachment 绑定到返回的 MCP URL。配置在创建后固定；变更需要创建
替代 Session，并让 client 使用新 URL。

这为审计与 cacheability 提供了强属性：URL 指向稳定的 capability environment。但它
不是 durable execution state。复用 URL 不会保留 `action_code` 文件、变量、对话或
Agent run。

## 6. 资源与权限模型

### 6.1 资源图

最小有用模型是：

```text
DigitalOcean Team
  |
  +-- DigitalOcean User（拥有 Session 与 Connection）
  |      |
  |      +-- Actor ID ----------------------+
  |      |                                  |
  |      +-- Connection -> Provider Account |
  |                                         |
  +-- Provider -> Tool Version              |
  |       `-- 可选 Custom MCP Server        |
  +-- Toolbelt Version                      |
  +-- Output View                           |
  `-- Gateway Session <---------------------+
          `-- MCP URL
```

公开 API 暴露 Provider/Tool、Custom MCP Server、Tool Health、Toolbelt、Output
View、Connection、Session、Actor/User 与 Actor-specific limit。它们被刻意赋予不同
生命周期，而不是一个实体。

### 6.2 Actor 是选择器，不是认证

Actor ID 是应用选择的稳定 identifier。Session 用它选择代表某个人或 workload 的
provider Connection。它不是 DigitalOcean login、provider username、Session ID 或
bearer credential。

DigitalOcean 还要求 Session 与 Connection 具有相同的 DO owner、Team 和 Actor ID。
另一位 Team member 仅提供相同 Actor 字符串，不能复用该 Connection。

应用仍需认证自己的用户并选择 Actor。若允许不可信请求任意提交 Actor ID，就会产生
account-confusion 漏洞。Gateway 可以检查所选记录的所有权，但无法知道应用是否选择了
正确用户。

### 6.3 Provider、Credential 与 Connection 不同

- Provider 定义操作及其 schema。
- Credential 提供 OAuth client material、OAuth authorization 或 API key。
- Connection 把某个 provider account 的授权与一个 owner/actor 关联。

Team-reusable credential 让另一名 Team member 创建自己的 Connection；它不会把原用户
的 Connection 变成共享权限。每个 owner/actor/provider 组合只有一个 Connection。
同一 provider 的不同账号需要不同 Actor ID。

这比把 `GITHUB_TOKEN` 同时视为 integration、account identity 与 authority grant 更
精确。

### 6.4 Toolbelt 是版本化集合，不是 Workflow

Toolbelt 把有目录上限的一组工具组织起来，用于 Selection、group policy 与可选
Preload。`toolbelt:research@1` 这类引用固定成员；发布新版本不会改变既有 Session。

Agent 仍逐个调用成员工具。Toolbelt 不提供顺序、依赖、事务、Provider Connection 或
共享审批。它是配置复用原语，不是编排原语。

## 7. 工具发现与模型上下文

### 7.1 三个 Meta-tool

默认 MCP endpoint 只暴露三个 meta-tool，而不是数千个 schema：

- `action_search` 接收一到五个自然语言 use case 和可选 provider/tag filter，返回有界
  的 name、version、description 与 input schema；
- `action_invoke` 校验并执行一个或多个具名工具；同一请求中的独立调用并行执行；
- `action_code` 在全新 sandbox 中运行 Python，并可通过 helper 调用 catalog tool。

Search 不是 Invoke。它会把结果限定在 Session 可达且可用的 catalog 内，但返回候选
不等于对具体参数的授权。执行时仍会重新运行 Policy 与 Connection 解析。

### 7.2 Preload 与直接调用

已知且高频的工具可 preload 到 MCP tool list。这样省去 discovery turn，但 name、
description 和 schema 会长期占用模型上下文。Preload 既不授予权限，也不妨碍发现其他
eligible tool。

若应用已经知道 operation 和 arguments，可通过 SDK 直接调用，不涉及 model、search
或 `action_code`。DigitalOcean 明确建议：如果 workflow 必须按固定顺序运行，应把顺序
保留在应用代码中。

因此有用的设计是一条连续谱，而不是唯一工具路径：

```text
固定应用步骤       -> 直接 SDK call
已知 Agent 操作    -> Preloaded Tool
开放式 Agent 任务  -> Search 后 Invoke
数据密集型组合     -> 临时代码调用受治理工具
```

### 7.3 Output View

Output View 是针对某个工具版本、不可变的 object result 投影。Team-created view 保留
选定的嵌套字段；DigitalOcean 也可提供带转换的 public view。Session 将一个 View 绑定
到一个工具，所有 direct、meta-tool 与 `action_code` 调用都会收到该 shape。Client 无法
针对单次调用请求 full output。

View 在执行后减少数据和模型 token，不会改变 provider operation、input、provider
scope 或工具费用。删除正在被既有 Session 使用的 View 会使调用失败，不会 fallback
到 full output。

架构启示不只是节省 token。Result projection 是数据最小化边界，但前提是被省略字段
不影响正确性，而且 projection 本身可版本化、可测试。

## 8. 策略与审批

### 8.1 评估顺序

文档给出的评估顺序是：

1. 拒绝不在 Session Selection 中的工具。
2. 评估 Default Action 与匹配的 Tool、Toolbelt、参数规则。
3. 采用最具体规则；同等具体时 `deny` 优先于 `ask`，`ask` 优先于 `allow`。
4. 若结果为 `ask`，获得有效的 inline 或 out-of-band approval。
5. 继续 provider authorization 与 execution。

省略 Default Action 时解析为 `ask`，但 DigitalOcean 建议显式配置。严格 Session 还须
允许预期使用的 meta-tool；只 allow 被发现的 provider tool，不一定同时 allow discovery
或 dispatch mechanism。

Direct call、`action_invoke` 与 `action_code` 内的调用都受同一 Selection 与 Policy
控制。Python 不是绕过治理的逃生口。

### 8.2 Approval 语义

Inline approval 依赖 client 支持 MCP elicitation。仅能连接远端 MCP，并不证明 prompt
可用。若 inline elicitation 不受支持或等待超时，Gateway 返回 approval request，供
人通过独立路径决策。

Out-of-band approval 绑定：

- Team；
- Gateway Session；
- Tool Version；
- 精确 Arguments；
- Expiry。

它只消费一次。审批后，client 以相同参数再次调用同一工具。Deny 不可被覆盖，过期
approval 不可复用，approval 也不会授予 provider scope 或创建 Connection。

Agent 不能批准自己的 request。Decision endpoint 需要 Session owner 的 DigitalOcean
权限，但集成应用仍必须让这份 credential 远离 Agent。

### 8.3 Policy 不能证明什么

Gateway Policy 只控制经由 Gateway 的调用。它不能证明：

- provider token 已经 least-privileged；
- selected tool 被 provider 正确分类；
- Agent 没有访问同一 API 的其他路径；
- 审批人理解 provider 侧实际影响；
- 后续相似调用已被此前 approval 覆盖。

因此 least privilege 同时需要 provider scope 与 Gateway Policy；二者不能相互补救。

## 9. 凭证代理

### 9.1 执行时解析

Action Gateway 把 connected-account credential 存入 DigitalOcean Secrets Manager，并
在工具执行时解析。模型得到 tool input 和 result，而不是存储的 OAuth token 或 API
key。

这与 run-level Secret delivery 有本质不同：

```text
Run-level Secret
  Secret -> Worker Environment/File -> Agent Process -> Provider

Brokered Connection
  Agent -> Typed Tool Request -> Gateway -> Stored Credential -> Provider
```

Brokered path 降低 credential 从 Agent environment 泄漏的风险，但不会移除委托权限：
被攻破的 Agent 仍可滥用 Session 允许的每个 operation 和 argument。

### 9.2 认证方式与生命周期

Provider 可使用 DigitalOcean OAuth app、客户自己的 OAuth app 或 API key。Connection
可预先授权，也可在交互调用报告需要授权时创建。无人值守工作必须预授权，因为它不能
停下来等待 provider sign-in。

授权可能过期、被撤销、缺少所需 scope 或需要 provider-specific setting。这些是
Connection failure，与 Tool Denial 和 Human Approval 不同。批准调用不会 refresh OAuth
或扩大 scope。

### 9.3 自定义 Provider

Team 可注册可达的 HTTPS Streamable HTTP MCP endpoint，认证可为 none、stored API key
或 per-actor OAuth。Tool discovery 把 schema 导入 catalog，之后 Team 只启用所需工具。
Gateway 为 slug 添加 Provider name 前缀。

OAuth authorization/token URL 必须使用 HTTPS，并与 MCP endpoint 属于同一 domain。
Session VPC attachment 不会让 provider execution path 能访问私有 Custom MCP Server；
Public Preview 限制仍要求可达的 HTTPS endpoint。

Custom MCP support 让 Action Gateway 可作为私有集成代码的治理包装，但也产生供应链
边界。Gateway 能治理调用，却不能证明远端 Server 安全地实现了宣称的语义。

## 10. 执行与失败语义

### 10.1 校验与并行

`action_invoke` 按 catalog schema 校验 name、version 与 arguments。同一请求中的多个
调用并行运行，数组顺序不表达依赖。依赖调用需要新请求或 `action_code`。

每个底层调用仍独立受 Selection、Policy、Approval、Connection、Provider Rate Limit
和 Billing 约束。Batch 不是绕过限制的方法，也不是原子单元。

### 10.2 临时代码执行

`action_code` 接收 Python source，返回 stdout、stderr 与 exit code。每次调用都启动
全新 sandbox；即使在同一个 Gateway Session，文件、变量和安装包也不持久。Timeout
包含调用其他工具所用时间。

Programmatic tool calling 可减少模型 round trip，并避免把中间 provider result 放进
对话，适合 filter、join 与 calculation。但它不提供 rollback：后续 exception 不会
撤销已完成的 provider action。

Session VPC attachment 只适用于该 code sandbox。它不会把全部 catalog-provider traffic
送入 VPC，也不会替代私有服务认证或网络策略。

### 10.3 Retry 与 Idempotency

Retry behavior 属于每个 catalog tool，而不是 model 或 Session Policy。符合条件的瞬时
故障可使用有界 attempt、deadline-aware backoff/jitter，以及 provider `Retry-After`。
Validation error、missing Connection 与 policy denial 不应 retry。

只有 catalog configuration 建立安全 contract 时才自动 replay，例如 read-only semantics
或 provider idempotency-key header。一个 logical invocation 在 automatic retry 间复用
生成的 key。新的 `action_invoke` 请求或重新运行 `action_code` 是新的 logical
invocation，不获得连续性保证。

Timeout 是模糊结果：provider 可能已经完成 write，只是 response 丢失。恢复必须检查
provider state，并只 retry 缺失 effect。成功 retry 也不提供 exactly-once 或跨工具事务。

## 11. 运维、隐私、限制与成本

### 11.1 可观测性

每次 Gateway tool call 都会被 trace。Insights 对 1/7/30 天窗口聚合 request volume、
success rate、provider/tool usage、status 与 provider P50/P95 latency。

它是运维视图，不是权威 execution ledger：

- 数据最多可比实时活动延迟 15 分钟；
- 繁忙时 detail chart 使用最多 5,000 行的 raw sample；
- aggregate total 仍使用完整窗口；
- 文档中的产品视图没有建立 BuildMax TaskRun trace 那样的 ownership 与 lineage。

### 11.2 限制与预算

Team limit 和更低的 Actor-specific limit 分别作用于 Exa Search、Exa Fetch、
DigitalOcean Action 与普通第三方 Action。Provider 自己的 rate/concurrency limit 继续
生效。Batch 或 Python workflow 中的每个调用都单独消耗 limit。

Action Gateway 当前没有 per-session 或 per-product spend cap。普通 SaaS/MCP invocation
文档价格为每 1,000 次 $0.10；search 已包含在内，code execution、付费 provider tool、
model inference 与 Harness Runtime 另行收费。循环或被攻破的 Agent 可消耗 prepaid
balance；余额归零只阻止要求 prepayment 的工具。

所以 rate limit 不是 budget boundary。生产使用仍需应用侧 call budget、loop guard 与
cancellation path，不能只依赖 Gateway RPM。

### 11.3 隐私与地域

Gateway 会处理 tool input、result、connected-account credential 与 usage data。
DigitalOcean 表示不会用 service content、tool input/result 或 Agent output 训练通用模型。
它会收集有限的 search signal，用 identifier 与 status flag 改进工具搜索，不含 prompt、
argument 或 result。

Public Preview 期间 Insights 默认开启，只能联系 support 关闭。文档称 service content
与 Insights telemetry 在美国 region 处理，Insights 不提供 regional localization。
发送到第三方 provider 的数据仍受对方条款与 retention 约束。

Public Preview 的 data、record 与 execution state 可能丢失。私有或受监管部署不能从
credential isolation 直接推导 compliance；residency、subprocessor、prohibited data、
deletion 与 backup posture 需要单独决策。

## 12. 与 BuildMax 的比较

| 关注点 | Action Gateway | BuildMax 当前状态 | 结论 |
|---|---|---|---|
| Agent runtime | 外部 client；无 Agent loop | Local、Desktop、Eval、Worker 共享 Go loop | 互补，不是 runtime 替代品 |
| 持久权限 | 不可变 Gateway Session 配置 | TaskRun authorization snapshot 与权威结果 | Gateway config 应附着到 TaskRun |
| Tool exposure | Selected Tool/Toolbelt、Search、Preload | Runtime registry、Agent/Subagent tool selection、两个 MCP meta-tool | 相似 late binding；BuildMax 没有大型 semantic catalog |
| MCP schema context | Search 返回有界相关 schema | `LoadMcpTools` 内含 name/description catalog，按 server/tool 精确加载一个 full schema | 小规模下当前形状足够 |
| Permission | Selection、Default/Rule、Argument、allow/ask/deny | Configured policy、argument risk、declared access、tool default、hook | BuildMax 覆盖 builtin；Gateway 对 catalog call 更丰富 |
| Approval | 单次 action、精确 argument/version/session、inline 或带外 | Per-run prompt ID；allow once 或内存 scope grant | BuildMax UI routing 良好，但 session grant 更宽，无持久 action-bound record |
| 外部身份 | Application Actor 选择 user-owned Connection | Account/Space/TaskRun identity；实验性本地 App OAuth | 不要把 free-form Actor 当权限；从 BuildMax identity 派生 binding |
| Credential | 调用时由 broker 解析，不进入 model context | Space Secret 物化进 run；远端 MCP bearer 来自环境 | Brokered invocation 关闭真实暴露缺口 |
| 多账号 | 不同 Actor ID 选择不同 provider account | 无 Server-side MCP OAuth 或 multi-account Connection | 应用委托的明确缺口 |
| 结果最小化 | 不可变 per-tool Output View | 有界/截断 output 与 Secret redaction，无 typed projection | 稳定 schema 和大结果证明需求时才有价值 |
| 可靠性 | Catalog-specific retry 与 idempotency | Tool 自有行为、Agent loop guard；无通用 MCP retry contract | 中央策略有价值，但必须验证 tool semantics |
| Audit | 延迟的 Gateway aggregate Insights | 有界、脱敏 per-run JSONL trace 与 TaskRun provenance | BuildMax 必须把外部 fact 纳入自己的 trace |
| 成本控制 | Actor RPM、prepaid balance，无 per-session spend cap | Task/Run quota、model usage、loop guard；无 Gateway budget | 保留 BuildMax admission 与 per-run bound |
| 私有部署 | Managed US service | 本地/私有产品原则 | Adapter 必须可选 |

### 12.1 BuildMax 已有优势

BuildMax 已避开一个常见上下文问题：它只暴露两个 MCP gateway tool，而不是把所有远端
tool schema 注册给模型。`LoadMcpTools` 中只放 name 与短 description catalog，只有模型
选择 server/tool 后才取 full schema。这与 Action Gateway search/invoke 属于同一设计
家族，只是使用确定性的精确 lookup，而不是 semantic search。

BuildMax tool policy 还统一覆盖本地文件、Bash、Browser、Builtin 与 MCP；Action
Gateway 只治理自己的执行面。`GrantScope` 已确保批准一个 `CallMcpTool` target 不会
批准所有 Server 上的所有 Tool。

TaskRun provenance、portable checkpoint、有界 trace、Secret redaction 与产品自有 work
model，也比只有 gateway operational telemetry 更强。

### 12.2 结构性缺口

重要缺口是：

1. 没有 Server-side Connection resource，在不向 run 暴露 credential 的情况下代表
   provider authorization；
2. 没有与 TaskRun 权限关联的 brokered tool-execution path；
3. 远端 MCP/Portal Agent 没有 OAuth、refresh 与 multi-account lifecycle；
4. 没有把 Tool Version、Connection Binding、Policy Digest 与 Result Shape Contract
   一起记录的 immutable resolved capability manifest；
5. 没有适用于 Worker 暂停并等待独立人工决策的 action-bound durable approval；
6. 远端工具调用没有 provider-aware retry 与 idempotency contract。

这些缺口不等于需要六个新顶层产品。多个问题可以属于一个 resolved TaskRun capability
attachment 和一个 broker adapter。

## 13. 对 BuildMax 的启示

### 13.1 分开六个维度

BuildMax 应显式区分：

1. **Availability**：本次 run 永远能触达哪些工具；
2. **Visibility**：初始向模型显示哪些 schema；
3. **Permission**：这次 call 和 arguments 是否可执行；
4. **Credential binding**：哪个外部账号执行；
5. **Result projection**：哪些返回数据抵达 Agent；
6. **Replay policy**：Executor 可自动 retry 什么。

合并任意两个都会产生典型错误：preloaded tool 意外变成 grant、approval 悄悄选择账号、
output truncation 被误认为 data minimization，或 generic retry 复制 write。

### 13.2 把 Connection 与 Secret 分开

任务本身需要 credential bytes 时应使用 Secret，例如 compiler 拉取私有 module、`git`、
用户脚本或任意 CLI。当受限 broker 可以执行 typed operation，而 run 需要的是权限而非
字节时，应使用 Connection。

差异不是存储 UI，而是信任边界：

| Secret Grant | Connection Grant |
|---|---|
| 向 TaskRun 物化字节 | 字节留在 Broker custody |
| 支持任意 command | 支持声明过的 provider operation |
| Agent 可外泄 value | Agent 只能滥用 allowed operation |
| Rotation 改变交付材料 | Rotation 隐藏在稳定 Connection identity 后 |

BuildMax 不应把 Space Secret 改造成同时承担两种概念。如果具体 provider journey 证明
需要，应引入独立 Connection resource，明确 ownership、revocation、health 与 audit。

### 13.3 在 TaskRun admission 解析 Capability

BuildMax 需要的稳定概念不是另一个通用 Session，而是在 TaskRun admission 时解析的
immutable capability manifest，只放已经证明必要的字段，例如：

```text
Subject 与 Authority Mode
Selected Tool Reference 与 Version
Connection Binding
Effective Policy Digest
Output Shape Reference
Broker Backend Reference
Per-run Call 与 Cost Bound
```

TaskRun 拥有该快照。Gateway-specific Session ID 或 MCP URL 是 attachment 中的 infra
detail。Rotation/revocation 可以让将来调用失败，但不能悄悄改写 TaskRun 原先被允许尝试
的内容。

### 13.4 派生外部 Actor Binding

若 adapter 需要 DigitalOcean Actor ID，BuildMax 应从 authenticated subject 与
connection purpose 派生 opaque、stable value。不能接受 caller-supplied arbitrary actor
作为授权决策，不能把 email 放进 identifier，也不能把外部字符串相等视为 BuildMax
ownership 的证据。

BuildMax 仍负责决定 TaskRun 可使用 personal Connection 还是 Space-owned automation
Connection。DigitalOcean 的 user/team ownership check 是 defense in depth，不是该决策。

### 13.5 把 Approval 绑定到 Proposed Action

BuildMax prompt ID 已能防止 stale answer 解决后续 prompt。对于 durable worker
approval，更强的目标应是把 approval record 绑定 TaskRun、Tool Identity/Version、
Canonical Arguments Hash、Connection、Policy Revision、Expiry 与一次 consumption。

这比当前可选的内存 “本 Session 允许这个 scope” grant 更窄。现有便利可以保留在本地，
但不应成为无人值守委托 action 的 contract。

### 13.6 只有 Catalog Scale 证明需求后才采用 Search

`LoadMcpTools` 已经让 full schema late-bound。Semantic search 会引入 ranking uncertainty、
evaluation burden 与 search-signal privacy 问题。BuildMax 应先让 deterministic catalog
有界且可检查。只有当用户频繁面对足够多 enabled tool，以至精确 provider/tool selection
失败时，才增加 semantic discovery。

证据门槛应是 realistic catalog 下的 task success、context saving 与 wrong-tool rate，
而不是 embedding index 已经可用。

### 13.7 把 Output View 当成版本化 Contract

Generic truncation 可以保护 context size，却可能删掉后续 write 所需 identifier。Typed
projection 只有在绑定 Tool Version、按 output schema 校验、出现在 run resolved config
中，并用 empty/optional field 测试时才更安全。

BuildMax 不应为了假设的节省新增全局 OutputView entity。第一个 provider adapter 可在
代码中拥有 fixed projection；等两个独立 integration 都需要复用时再 generalize。

### 13.8 只有存在安全 Contract 才集中 Replay

远端执行适合统一 timeout、backoff、`Retry-After` 与 idempotency key。Gateway 不能根据
HTTP method、仅凭 MCP read-only hint 或 LLM 决策推断安全。Tool adapter 需要显式 retry
class；write 还需 provider-supported idempotency 或 reconciliation operation。

Trace 必须区分 logical invocation、provider attempt、ambiguous completion、
reconciliation 与 manual retry。

### 13.9 保留 BuildMax 权威 Trace

外部 gateway call 应发出 BuildMax requested、denied、waiting approval、dispatched、
provider-attempted（若可见）、succeeded、failed 与 completion-unknown event。外部
invocation/approval ID 只作为 correlation data，不能成为唯一记录。

Provider result 进入 history、stream、TaskRun result 或 Artifact 前，仍须经过 BuildMax
bound 与 redaction。“Gateway 会 trace” 不是削弱这些控制的理由。

### 13.10 不要把工具治理与 Sandbox 混淆

Broker 让 provider credential 远离 Agent，但不约束 filesystem、shell、browser 或
network。反过来，microVM 能约束 process effect，却不决定一次 Stripe refund 是否获得
授权。

BuildMax 必须继续把 sandbox、egress、tool policy、Connection scope、human approval 与
provider authorization 作为分开的 defense。

## 14. BuildMax 候选方向

### 14.1 选项 A——只吸收经验，不集成

保留当前 MCP path，用研究结果改善 native capability resolution 与 approval。它没有
provider dependency，对已有私有 MCP integration 的部署可能已足够。

除非 BuildMax 自建或采用其他 broker，否则它不会关闭 connected-account credential
缺口。

### 14.2 选项 B——把 Action Gateway 配成普通远端 MCP

Operator 在 BuildMax 外创建 Gateway Session，把 URL 注册成 remote MCP Server。Agent
通过 BuildMax 现有 `LoadMcpTools` / `CallMcpTool` pair 看到 Action Gateway 的三个
meta-tool。

这是最小技术实验，适合验证 interoperability。但它不是完整产品 journey，因为
DigitalOcean login、Session ownership、Actor choice、Policy、Connection lifecycle 与
Approval 都在 BuildMax 外。它还产生 nested discovery：

```text
LoadMcpTools -> CallMcpTool(action_search)
             -> CallMcpTool(action_invoke)
```

Spike 可以接受这项成本，最终模型接口未必应接受。

### 14.3 选项 C——First-class 可选 Broker Adapter

BuildMax 通过 API 创建和解析 Action Gateway Session，经由 native broker interface
暴露 selected provider tool，并把 gateway event 映射进 TaskRun trace。Adapter 拥有
DigitalOcean-specific identity、session、authentication 与 error translation。

只有实验能证明真实 provider journey 时，这才是候选生产方向。Portable interface 应
描述 BuildMax 需要的 operation——create resolved capability、invoke、await approval、
revoke、inspect health——而不是镜像每个 DigitalOcean resource。

### 14.4 推荐顺序

1. **记录并加固 native boundary。** 在不添加 provider 的情况下定义 resolved per-run
   capability fact 与 action-bound approval semantics。
2. **运行 remote-MCP interoperability spike。** 测量 nested discovery、client auth、
   approval、trace correlation 与 result handling。
3. **选择一个具体 connected-account journey。** GitHub 或 Jira 比 generic catalog demo
   更有价值，因为 ownership、OAuth refresh、approval 与 revocation 都可观察。
4. **证据为正时构建 provider-specific adapter。** 第一阶段不要做 generic Connection
   marketplace。
5. **出现第二个 backend 或 provider 后再 generalize。** 到那时 shared Connection、
   projection 或 broker interface 才有证据。

任何步骤都不要求 Toolbelt、Actor、OutputView 或 GatewaySession 成为 BuildMax 顶层实体。

## 15. 产生证据的实验

### 15.1 Interoperability 与 Context 实验

通过 BuildMax remote MCP 连接一个 Action Gateway Session，分别用 exact preloaded tool
和 search-based discovery 执行相同任务。

记录：

- 初始 tool-schema token；
- model/tool round trip 数；
- correct-tool 与 wrong-tool rate；
- end-to-end latency 与 tool charge；
- nested `LoadMcpTools` / `action_search` 对模型可靠性的影响。

测试小、中、大 selected catalog。成功标准是 adapter shape 可理解，额外 indirection
后测得收益仍存在。

### 15.2 Identity 与 Connection Isolation 实验

创建两个应用用户、两个 Actor Binding，以及同一 Provider 的不同账号。尝试跨用户
Actor substitution、另一 Team member 的 Session、revoked OAuth、missing scope 与
connection refresh。

成功标准是每个 refusal 可归因；credential 不进入 model/tool output 或 BuildMax trace；
BuildMax 不依赖 Actor 字符串，就能解释哪个 authenticated subject 选择了哪个 external
account。

### 15.3 Approval 实验

测试 allow、deny、inline ask、out-of-band ask、expiry、argument change、tool-version
change、duplicate decision、cancellation 与 resumed TaskRun。

成功标准是一次 approval 只授权一项 intended call；stale/changed call 无法消费；deny
不可被软化；两个系统的记录都关联到同一个 TaskRun 和 tool call。

### 15.4 Failure 与 Idempotency 实验

使用可在 write 前失败、commit 后失败、response transmission 中失败、rate limit 下失败，
以及返回 `Retry-After` 的 test provider。

成功标准是 read retry 有界；provider-idempotent write 不重复；ambiguous write 进入
reconciliation state；新的 Agent call 不会被误认为旧 logical invocation 的 automatic
retry。

### 15.5 数据最小化实验

使用包含 required identifier、optional field、sensitive metadata 与 large body 的
provider result。比较 full output、gateway Output View 与 BuildMax-side projection/
redaction。

成功标准是 Agent 用更少 token 完成 follow-up；省略字段不从 trace 或 alternate return
path 泄漏；Tool/View version 改变会显式失败，而不是悄悄改变语义。

### 15.6 Budget 与 Abuse 实验

在低 Actor limit 与 BuildMax per-run budget 下触发重复 search、invoke、parallel batch
与 `action_code` loop。

成功标准是外部 prepaid balance 成为唯一制动前，BuildMax 已停止 TaskRun；报告 partial
effect；区分 Gateway、Provider 与 BuildMax limit failure。

### 15.7 私有部署决策实验

对一个候选客户画像记录 region、data class、telemetry、subprocessor、provider term、
outage behavior、export、deletion 与 fallback 到 private MCP Server 的方式。

成功标准是 operator 能做显式 deployment choice。通过 functional demo 不足以证明受监管
或私有使用可接受。

## 16. 风险与开放问题

### 16.1 产品与供应商风险

- Action Gateway 是 Public Preview；API、limit、pricing、retention 与 availability 都可
  变化。
- Managed catalog 引入 provider-version 与 semantic supply-chain risk。
- Session configuration 不可变，但删除 Output View 可破坏已绑定 Session，因此
  immutability 不等于完整 dependency closure。
- User-owned DigitalOcean Connection 可能不适合 Space-owned unattended automation，
  除非先明确 service-authority design。
- 没有 per-session spend cap，意味着外部成本安全依赖 BuildMax 与 operator control。
- US-only processing 与 Insights residency 可能排除原本需要 credential broker 的部署。

### 16.2 架构问题

1. 第一个 required Connection 是 personal、Space-owned，还是两者都要？哪个真实 journey
   能证明？
2. BuildMax 能否在不把宽权限 DigitalOcean token 交给 Worker/Agent 的情况下创建
   least-privilege Gateway Session？
3. TaskRun 被取消、member 失去权限或 Policy 改变时，Gateway Session 如何撤销或替换？
4. BuildMax 为 `ask`、Gateway 为 `allow`，或反过来时谁是权威？更严格答案应获胜，但
   Approval UX 与 error mapping 仍需证明。
5. Action Gateway approval 能否由 Portal 解决，同时保留 BuildMax 作为用户可见 audit
   authority？
6. Tool search 是否返回足够的 version/effect metadata，让 BuildMax 在审批前解释 proposed
   action？
7. External invocation identifier 是否足够一致，可用于 Gateway Insights 与 TaskRun trace
   reconciliation？
8. Broker outage 而 private MCP alternative 存在时，availability/fallback contract 是什么？
9. Insights 保留哪些 result data、多长时间，关闭后有什么变化？
10. `action_code` 节省的 latency/token 是否值得把中间 provider data 送入另一 managed
    execution boundary？

## 17. 若获采纳的可能归宿

本文不应永久成为平行架构文档。若证据支持 native brokered invocation，获采纳的理由
应移入：

- [Agent 执行身份与委托](agent-execution-identity-and-delegation.md)：Subject、Authority、
  Delegation 与 Action-bound Approval；
- [Space 密钥](../design/Space密钥.md)或聚焦 Connection 的设计记录：delivered Secret 与
  brokered credential 的边界；
- [工具权限](../design/工具权限.md)：effective-policy composition；
- [工具架构](../contribute/architecture/tools.md)及面向用户的 MCP/Connection 文档：已交付
  行为。

只有具体 journey、acceptance evidence 与 private-deployment trade-off 获接受后，
DigitalOcean adapter 才应成为 backlog item。若证据不支持集成，则把通用启示保留在
相关 design record 中并删除本提案；Git 历史会保留 vendor assessment。
