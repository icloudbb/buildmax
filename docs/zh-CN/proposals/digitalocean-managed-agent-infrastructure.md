# DigitalOcean 托管 Agent 基础设施评估

> **翻译说明：** 本文是[英文原文](../../proposals/digitalocean-managed-agent-infrastructure.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
>
> **受众：** 贡献者、运维人员、产品设计者与安全审查者 · **状态：** 提案——讨论中
>
> **开启日期：** 2026-10-02

相关文档：[路线图](../ROADMAP.md)、[当前状态](../current-state.md)、
[Agent 执行与 Task 线程](../design/Agent执行与Task线程.md)、
[Task 工作区检查点](../design/Task工作区检查点.md)、
[LLM 网关](../design/LLM网关.md)、[沙箱边界](../design/沙箱边界.md)、
[Space 密钥](../design/Space密钥.md)、
[Action Gateway 架构评估](digitalocean-action-gateway.md)、
[Agent 代表用户使用应用](agent-app-delegation.md)、
[持久化 Agent Session](durable-agent-sessions.md)与
[长时间运行的工作区 Environment](long-running-workspace-environments.md)。

## 目录

- [1. 决策问题](#1-决策问题)
- [2. 用户结果、证据与当前约束](#2-用户结果证据与当前约束)
- [3. 研究范围与来源质量](#3-研究范围与来源质量)
- [4. DigitalOcean 产品架构](#4-digitalocean-产品架构)
- [5. Harness Runtime](#5-harness-runtime)
- [6. Action Gateway](#6-action-gateway)
- [7. Inference Engine 与 Agent Platform](#7-inference-engine-与-agent-platform)
- [8. 与 BuildMax 的比较](#8-与-buildmax-的比较)
- [9. 对 BuildMax 的启示](#9-对-buildmax-的启示)
- [10. 集成选项](#10-集成选项)
- [11. 候选方向](#11-候选方向)
- [12. 风险与产品限制](#12-风险与产品限制)
- [13. 产生证据的实验](#13-产生证据的实验)
- [14. 开放问题](#14-开放问题)
- [15. 若获采纳的可能归宿](#15-若获采纳的可能归宿)

## 1. 决策问题

BuildMax 是否应将 DigitalOcean 的 Inference Engine、Action Gateway 或
Harness Runtime 用作可选基础设施后端？哪些架构经验值得吸收，同时又不把一个公开
预览中的云服务写进 BuildMax 领域模型，也不削弱本地和私有部署能力？

候选答案有明确的非对称顺序：

1. 首先把 DigitalOcean Inference 当成普通模型 provider。
2. 将 Action Gateway 作为外部凭证和工具 broker 进行评估，因为它直接回应了应用
   委托中已经出现的缺口。
3. 只有在私有部署 Beta 门槛之后，才用一个有界的 executor 原型评估 Harness
   Runtime。
4. 无论选择哪项，Space、Task、TaskRun、Workflow、授权、权威结果、checkpoint
   与审计都继续由 BuildMax 拥有。

本文不把任何集成写入路线图。它记录外部产品评估，并说明方向获采纳前必须获得的
证据。

## 2. 用户结果、证据与当前约束

### 2.1 必要用户结果

BuildMax 部署应能为模型服务、受治理工具或隔离执行选择托管基础设施，而不改变
Task 的含义、不丢失权威运行历史，也不让本地或私有部署变得不可用。

具体用户结果是运维方式可替换，而不是抽象地追求可移植：运维人员可以选择自己
管理的 Kubernetes、托管 microVM 或混合模式，用户看到的仍是同一套 Task 历史、
Artifact、审批和失败语义。

### 2.2 问题值得研究的证据

DigitalOcean 于 2026-09-21 将 Managed Agents 开放为 Public Preview。它把基于
microVM 的 Harness Runtime 与托管 MCP/凭证代理 Action Gateway 组合起来，
Inference Engine 则提供 serverless、batch、routing 与 dedicated 模型执行。这组
产品直接覆盖 BuildMax 当前 Kubernetes worker、sandbox、MCP、Secret 和托管模型
路径中的工作。

这种重叠并不能证明 BuildMax 应将上述路径外包，但它说明这些边界正在成为可识别
的云产品边界；在确实改善某个部署时，BuildMax 应能消费它们。

### 2.3 当前约束

- BuildMax 当前优先事项是可靠的私有部署 Beta；R2、R3 资格工作高于可选 provider
  集成。
- CLI/TUI、Desktop、evaluation 和 worker 共用 Go Agent runtime。用 provider
  harness 取代它会造成 surface 间行为分裂。
- TaskRun 是权威执行结果和授权 envelope，外部 Session 或 Run 不能取代它。
- Task 工作区通过可移植的对象存储 checkpoint 持久化。外部机器快照可加速恢复，
  但不能成为唯一副本。
- 受支持的无人值守 worker 拒绝 stdio MCP、允许远端 MCP，因此 Action Gateway
  集成比新增本地工具进程更接近现有边界。
- BuildMax 目标包括本地和私有部署；DigitalOcean adapter 必须可选，缺少它不能
  降级受支持核心能力。

## 3. 研究范围与来源质量

本评估于 2026-10-02 依据 DigitalOcean 官方产品与文档核验，主要来源包括：

- [Managed Agents](https://docs.digitalocean.com/products/managed-agents/)及其
  [架构](https://docs.digitalocean.com/products/managed-agents/agent-harness-runtime/concepts/architecture/)；
- Harness Runtime 的
  [Environment](https://docs.digitalocean.com/products/managed-agents/agent-harness-runtime/concepts/environments/)、
  [Session](https://docs.digitalocean.com/products/managed-agents/agent-harness-runtime/concepts/sessions/)、
  [Adapter](https://docs.digitalocean.com/products/managed-agents/agent-harness-runtime/concepts/agent-adapters/)、
  [Secret](https://docs.digitalocean.com/products/managed-agents/agent-harness-runtime/concepts/secrets/)与
  [Egress](https://docs.digitalocean.com/products/managed-agents/agent-harness-runtime/concepts/egress/)概念；
- Action Gateway 的
  [Session、Actor 与 Connection](https://docs.digitalocean.com/products/managed-agents/action-gateway/concepts/overview/)、
  [工具策略](https://docs.digitalocean.com/products/managed-agents/action-gateway/concepts/tool-policies/)与
  [可靠执行](https://docs.digitalocean.com/products/managed-agents/action-gateway/concepts/reliable-execution/)；
- [Inference](https://docs.digitalocean.com/products/inference/)、
  [Inference Router](https://docs.digitalocean.com/products/inference/how-to/use-inference-router/)与
  [Inference 功能](https://docs.digitalocean.com/products/inference/details/features/)。

产品事实只使用官方来源。工具数量、恢复延迟等营销说法只视为产品描述，不视为
BuildMax 验收证据。本评估没有使用真实 DigitalOcean 账号、付费 inference、
Harness Runtime Session 或 Action Gateway Connection。Public Preview 文档变化很快，
且存在第 12 节指出的不一致；实际 API 行为必须决定任何实现。

## 4. DigitalOcean 产品架构

DigitalOcean 展示的是一组可以独立使用的 AI-native cloud 层，而不是严格调用栈：

```text
Application / CLI / webhook / schedule
                   |
          +--------+---------+
          |                  |
          v                  v
  Inference Agent       Managed Agents
      Platform       +------------------+
                     | Harness Runtime  |
                     | Action Gateway   |
                     +--------+---------+
                              |
                 +------------+------------+
                 v            v            v
             Inference      Storage       Data
                 |
        serverless / batch /
        dedicated / router
```

Managed Agents 自身包含两个可以组合或单独使用的服务：

- Harness Runtime 在 Session 级 microVM 中运行 Agent 或代码；
- Action Gateway 通过 MCP 或 SDK 暴露受治理工具，并使用不进入 Agent 的凭证执行
  调用。

Inference Engine 是同级服务而非强制依赖。Harness Runtime 可以使用外部 provider
key，Action Gateway 也可以被运行在 Harness Runtime 之外的 Agent 使用。

Inference 下的 Agent Platform 是另一种 RAG/chat-agent 产品，管理 instructions、
模型、knowledge base、guardrail、function route、子 Agent 路由、评估与聊天 endpoint。
它不等于 Harness Runtime 的通用代码执行环境。

## 5. Harness Runtime

### 5.1 Environment、Session 与 Run

Harness Runtime 分离三种生命周期：

| 概念 | 拥有的内容 | 生命周期 |
|---|---|---|
| Environment | Adapter、template、size、skills、tools、credentials、permissions、egress | 创建后不可变；可重复创建 Session |
| Session | 一个 sandbox、工作区状态、事件历史和受支持的 Agent 历史 | create、pause、resume、checkpoint、fork、rollback、remove |
| Run | Session 内一次 Agent turn | start、stream/watch、cancel、complete 或 fail |

一个 Environment 可以创建多个 Session，一个 Session 可以累积多个 Run。在基础设施
层面，它最接近 BuildMax 的已解析 Agent 环境、Task 执行连续性与 TaskRun，但二者
所有权语义不同。

### 5.2 隔离与持久化

每个 Session 获得一个轻量 Firecracker microVM。Session 之间由 hypervisor 而非
container namespace 隔离。Session sandbox 提供 CPU、内存、文件系统、网络和
toolchain。Session 是隔离单位；同一 Session 内的进程共享文件与普通注入凭证。

Pause 会冻结进程、内存和文件系统并停止 compute 计费。Checkpoint 捕获机器状态；
fork 创建独立 Session；rollback 在保持 Session ID 的同时替换 live sandbox。支持
程度因 adapter 而异；fork 会继承机器状态，但启动新的 transcript。

Workspace 还可以作为生命周期长于 Session 的独立挂载盘。因此 DO 实际提供三种
持久化：live Session、整机 checkpoint 和独立持久文件卷。

### 5.3 Adapter 与 template

具名 adapter 包括 Codex CLI、Claude Code、OpenCode、Hermes、LangGraph 和 OpenAI
托管的 Codex 环境。custom adapter 接受自带 image 与 entrypoint，但结构化事件较少。
BYOT image 构建在受支持的 DO base template 上，以获得 runtime wiring 与事件处理。

Adapter 决定能力。OpenAI 托管 Codex 变体保留 sandbox lifecycle 与计费，但不提供
DO event stream、权限执行、审批、checkpoint、fork 或 rollback，因为 Agent loop
不归 DO 所有。这强烈支持显式 backend capability negotiation，而不是只用一个
“支持 executor”的布尔值。

### 5.4 权限、审批、egress 与凭证

Harness 权限规则把 Agent action 解析为 `allow`、`ask` 或 `deny`。审批可内联或带外
回答，并与网络 egress、provider authorization 相互独立。

未声明时 sandbox egress 完全开放。只要列出一个 host 就开启 allowlist，其余目标
拒绝，平台可能合并 adapter、inference 和运行必需 host。Action Gateway 调用不需要
sandbox 直接放行 provider host，因为真正请求由 Gateway 发出。

Harness 按真实值是否进入 sandbox 区分配置与凭证：

| 声明方式 | 保存的 spec 外安全存储 | Sandbox 可读真实值 |
|---|---:|---:|
| `env` | 否 | 是 |
| 普通 `secrets` | 是 | 是 |
| scoped secret | 是 | 否；sandbox 得到绑定到单一 HTTPS 目标的 handle |
| Action Gateway connection | 是 | 不向 sandbox 交付值或 handle |

评估时 scoped-secret redemption 仍在逐步 rollout，生产使用前必须单独验证。

### 5.5 Trigger、可观测性、限制与计费

Cron 和签名 webhook trigger 可以每次创建 fresh Session，也可复用 paused Session。
Trigger execution 去重并记录 Session。无人值守策略有值得警惕的行为：显式 `ask`
会在创建 trigger 时被拒绝，但复用 Session 若在运行时意外出现 prompt，平台可能
自动批准以便结束运行。因此无人值守工作中绝不能发生的动作必须 `deny`，不能只
设为 `ask`。

DigitalOcean Insights 提供 Session lifecycle、资源、token 与审批指标。Public
Preview 限制包括依账号而定的并发 Session 上限、默认 15 分钟 idle pause，且 paused
Session 仍占 active capacity。当前没有 Run lifecycle webhook，因此外部控制面必须
observe 或 polling，不能依赖终态 callback。

## 6. Action Gateway

聚焦的 [Action Gateway 架构评估](digitalocean-action-gateway.md)深入分析其身份、凭证、
策略、审批、上下文、可靠性以及与 BuildMax 的集成边界。本节只保留与 Harness Runtime
和 Inference 对比所需的产品级摘要。

### 6.1 资源模型

Action Gateway 分离集成定义、外部身份、凭证授权与每个 client 的策略：

```text
Provider -> Tools
             |
Actor -> Connection -> provider account and credentials
  |
  +-> Gateway Session
        |- selected tools or versioned Toolbelts
        |- allow / ask / deny policy
        |- optional argument conditions
        |- preloaded tools
        |- output views
        `- managed MCP URL
```

Actor 是应用选择、用于定位 Connection 的身份，不是 login 或 credential。Connection
为一个 Actor 授权一个 provider account，同时也归创建它的 DO user 所有。仅 Actor
ID 相同不会获得另一 DO user 的 Connection。

Provider credential scope 始终是外层权限。Gateway policy 无法收窄 provider 侧
过宽 token 的真实权限；它只控制 Session 可以经由 Gateway 请求哪些操作。

### 6.2 Discovery 与模型上下文

默认 MCP surface 只暴露三个 meta-tool，而不是整个 catalog schema：

- `action_search` 搜索相关且有资格的工具并返回 schema；
- `action_invoke` 调用一个或多个具名工具，可并行；
- `action_code` 运行临时 Python，并组合受治理的工具调用。

已知工具可 preload。Output view 在结果进入模型前只投影所需字段。这些是不同控制：
preload 改变 schema context，output view 改变 result context，tool selection 限定
资格，policy 控制执行。

### 6.3 治理与可靠执行

Tool selection 与 permission 相互独立。未被选择的工具即使 policy 会 allow 也会被
拒绝；`deny` 不能通过审批覆盖。带外审批绑定 team、Session、tool version 和参数，
且只消费一次。

Retry 按工具配置并有界。只有 catalog 明确建立安全契约时才自动重放，例如 read-only
或 provider idempotency key。Timeout 不证明写失败；独立工具请求或再次运行代码都是
新的 logical invocation；多工具代码不是事务。

每次 Gateway 调用都有 trace，并提供请求、成功率、工具、provider 和 latency 聚合。
这些运维视图可能延迟，不能取代 BuildMax TaskRun provenance。

## 7. Inference Engine 与 Agent Platform

Inference Engine 组合以下能力：

- Serverless Inference：同步与异步模型 API；
- Batch Inference：大规模非实时任务；
- Dedicated Inference：托管 GPU endpoint 与受支持的 BYOM；
- Inference Router：任务分类、model pool、成本/延迟策略、cache-aware selection、
  顺序 fallback 与 failover；
- 模型和 Router evaluation；
- 独立的 Agent Platform。

Serverless API 使用 `https://inference.do-ai.run`，提供 OpenAI/Anthropic 风格的
Chat Completions、Responses、Messages、embedding、image、audio 与 TTS。并非每项
provider 专有能力都完全兼容，因此仍需能力验证。

Dedicated Inference 管理 Kubernetes、ingress、vLLM、模型存储、RDMA、多节点 serving、
autoscaling、prefix-aware routing 和并行策略。若没有明确的私有模型需求，BuildMax
没有理由复制这些模型 serving 基础设施。

Agent Platform 管理托管 conversational/RAG Agent 并暴露专用 endpoint。它的
function route、child-agent routing、knowledge base、guardrail 和 evaluation 与
BuildMax 部分能力重叠，但不提供隔离环境中的任意仓库工作。

## 8. 与 BuildMax 的比较

| 关注点 | DigitalOcean | BuildMax 当前状态 | 判断 |
|---|---|---|---|
| 产品所有权 | 云资源、Environment、Session、provider tool | Space、Issue、Agent、Task、TaskRun、Workflow、Schedule | BuildMax 拥有更完整的工作与授权模型 |
| Agent loop | 多 adapter 和外部 harness | 所有 surface 共用 Go runtime | 替换会分裂行为 |
| 不可变配置 | Environment config | Agent revision、model resolution、plugin pin、sandbox tier、Secret consumption | 意图相同，但 BuildMax 解析结果分散 |
| 持续工作 | Session、sandbox、history | Task、session bundle、workspace head | 基础设施形状相近，领域权威不同 |
| 一次 turn | Run | TaskRun | 最接近；TaskRun 还拥有授权与权威结果 |
| 隔离 | 每 Session 一个 Firecracker microVM | worker pod 加 Bash sandbox；接受 outer-runtime 限制 | Harness 对不可信执行明显更强 |
| 持久工作区 | live disk、machine checkpoint、Workspace volume | 不可变对象存储 base/result/partial checkpoint | BuildMax 可移植性和 lineage 更明确 |
| 工具 | 托管 catalog、MCP、SDK、broker execution | 内建工具、远端/stdio MCP、plugin | Gateway 提供规模与凭证隔离 |
| 工具 discovery | search、preload、invoke、code、output view | lightweight catalog、`LoadMcpTools`、`CallMcpTool` | BuildMax 已有良好双 meta-tool 基础 |
| 外部身份 | Actor 与 user-owned Connection | Space Secret 与实验性本地 app connection | 这是 BuildMax 最明显缺口 |
| 审批 | Adapter 与 Gateway approval | Core policy、本地审批、Remote Control、worker deferred question | BuildMax 跨 surface 所有权更强；Gateway 可成为执行闸门 |
| Workflow | fresh/reuse cron/webhook trigger | 持久 graph、binding、retry、timeout、human node、schedule | 不应被 provider trigger 取代 |
| Inference | 广泛 model catalog、模态、routing、batch、dedicated | provider-neutral client、managed gateway、quota、ledger | 应消费 provider，不重建 GPU serving |
| 可观测性 | Adapter event 与延迟 Insights | 有界脱敏 trace、TaskRun result、Artifact、model-call ledger | 外部事件应补充而非取代 BuildMax 记录 |
| 部署 | DO 托管公有云 | 本地单二进制与私有部署 | Provider integration 必须可选 |

最准确的对应是：

```text
BuildMax resolved Agent and run policy  ~= DigitalOcean Environment
BuildMax Task 的 live execution instance ~= DigitalOcean Session
BuildMax TaskRun                         ~= DigitalOcean Run
```

对应关系在所有权处终止。DO Session 是基础设施，BuildMax Task 是 Space 拥有的用户
工作线程；DO Run 是一次 Agent turn，而 TaskRun 还是不可变 admission、authorization、
metering、result、trace、Artifact 和 Workflow progression 边界。

## 9. 对 BuildMax 的启示

### 9.1 保留控制面，让基础设施可替换

BuildMax 的持久价值不是创建 Kubernetes Job，而是围绕工作建立的含义与治理。部署
采用 local process、Kubernetes 或未来托管 executor 时，Task 和 TaskRun 必须保持
稳定。

现有 `scheduler.WorkerRunner` 是有用 seam，但只面向 launch，返回值仍是 Kubernetes
形状。只有真实 external-executor 实验逼出最小 lifecycle contract 后才应泛化。

### 9.2 解析一个不可变 run environment

DO Environment 说明把 adapter、tool、policy、credential、egress 和 resource 作为
一次执行的整体不可变输入有运维价值。BuildMax 已分别 pin 住主要部分。

未来内部 `ResolvedRunEnvironment` 可规范化所选 Agent revision、model、plugin release、
MCP/tool selection、sandbox policy、Secret grant、workspace base、resource limit 与
execution backend。它不必成为用户实体或数据库表；canonical digest 加上执行和诊断
需要的字段可能已经足够。

它必须解决 replay、给外部 backend 单一输入、以及“为什么这次 run 有这项能力”。
若实现不能证明其中一项，就不应引入该概念。

### 9.3 Connection 与 Secret 分开建模

Space Secret 正确描述了交付给 run 的值，并明确 Agent 可读取。用户授权的 GitHub、
Jira 或 Slack 账号拥有不同生命周期：provider consent、scope、refresh、revocation、
account selection 和代表谁行动的 principal。

现有 app delegation proposal 已区分 Connection 与 runtime grant。Action Gateway 用
Actor、Connection、Session、policy 给出可验证的具体形状。BuildMax 不应把 Secret
拉伸成应用身份资源。

### 9.4 区分四层授权

有效外部权限是交集：

```text
provider credential scopes
  AND selected operations
  AND BuildMax allow / ask / deny policy
  AND sandbox network and filesystem reach
```

这些层应分别存储、分别执行，再在 TaskRun diagnostics 中共同展示。Plugin effect
annotation 或 MCP read-only hint 只是 policy evaluation 的证据，不是权威。

### 9.5 协商 backend capabilities

Executor 或 adapter 至少应声明 admitted run 可能要求的能力：event streaming、cancel、
workspace restore、checkpoint、fork、interactive approval、browser、private network、
credential broker。缺少必需能力时 admission 必须失败，不能静默从头运行、自动批准，
或把不完整 trace 标为完整。

### 9.6 可移植 checkpoint 继续作为权威

整机 snapshot 能快速继续，但绑定 provider 和 adapter。BuildMax object-store checkpoint
继续作为可移植 base、result、retry 和 recovery 记录。外部 Session 或 machine
checkpoint 只能是 cache，不能成为唯一副本，也不能绕过成功 checkpoint finalization
独立推进 Task workspace head。

### 9.7 不因 backend 支持就给 TaskRun 增加 pause

暂停 microVM 是基础设施动作；暂停权威 TaskRun 必须回答 deadline、quota、credential
expiry、authority revocation、Workflow capacity、stale-run reaping 与 provider-state
loss。BuildMax 已通过 terminal TaskRun、portable checkpoint 与 Continue 满足大部分
结果。新增状态前必须有用户旅程证明需求。

### 9.8 只在证据要求时扩展 tool context

BuildMax 已暴露两个 MCP gateway tool，并按需加载单个 schema，避免把所有完整 schema
注册给模型。若真实 catalog 使剩余 name/description 列表过大，再引入 searchable
discovery、selective preload 和 schema-bound output projection；不要为假设压力先重写。

### 9.9 把外部工具 retry 视为不确定副作用

Connector 应区分 logical invocation 与 provider attempt，只在 provider contract 支持
时复用 idempotency key，并把 write 后 timeout 表示为 unknown outcome，而不是确定
failure。验证建议或 compensating action 应与该结果一起记录。多工具 Agent action
不是事务。

### 9.10 消费 inference 基础设施，不重复构建

DO Inference 可以作为 provider 或 router endpoint 进入现有 model catalog。BuildMax
继续拥有 Space policy、quota、credential ownership、call ledger、structured-output
validation 和 TaskRun attribution。除非出现具体私有部署需求，不应构建 GPU scheduling、
RDMA、vLLM autoscaling 或同类商业模型 marketplace。

## 10. 集成选项

| 选项 | 用户价值 | BuildMax 工作 | 主要风险 | 候选顺序 |
|---|---|---|---|---:|
| DigitalOcean Inference provider | 在现有 managed inference 下增加模型、模态和 routing | Provider config 与 capability qualification | OpenAI 兼容不完整、专有能力缺口 | 1 |
| Action Gateway remote MCP | 大型 connector catalog，凭证不进 worker | Session provisioning、actor mapping、approval、audit linkage、revocation | DO identity ownership 与 Public Preview 依赖 | 2 |
| Harness Runtime executor | 更强隔离与托管 Session 基础设施 | 外部 lifecycle、event ingestion、result/checkpoint bridge、cleanup、cost control | 语义和运维错配最大 | 3 |
| 把 DO Agent Platform 作为原生 Agent type | 托管 RAG/chat Agent | 第二套 Agent 定义与 Session 模型 | 概念重复、runtime 行为分裂 | 没有具名用例时不做 |
| 用 Harness Runtime 替换 Kubernetes worker | 少维护 worker infra | 迁移受支持执行边界 | Vendor lock-in、失去私有部署 | 拒绝作为全产品方向 |

Action Gateway 与 Harness Runtime 必须可独立选择。部署可以使用 BuildMax worker 加
Action Gateway、Harness Runtime 加 BuildMax 自有远端工具、只用 DO Inference，或
完全不用 DO。

## 11. 候选方向

候选架构保持 BuildMax 控制面权威：

```text
BuildMax control plane
  Space / Issue / Agent / Task / TaskRun / Workflow
  authorization / quota / audit / result / Artifact / checkpoint
                         |
                 resolved run environment
                         |
              +----------+-----------+
              |          |           |
              v          v           v
          local       Kubernetes   optional managed executor
                                      |
                           optional Action Gateway

BuildMax LLM gateway
              +----------+-----------+
              |                      |
              v                      v
       existing providers    DigitalOcean Inference
```

任何 DO ID 都不成为 BuildMax ownership parent。外部 ID 只是 execution handle 与
provenance。BuildMax reconciler 根据 durable facts 与 provider observation 决定
TaskRun 终态；provider event 是 trace 输入，不是权威来源。

本提案不应改变当前路线图。若 Beta gate 后实验给出正面证据，获采纳工作应进入 R5
或其后继，而不是插入 R2、R3。

## 12. 风险与产品限制

### 12.1 Public Preview 的耐久性与 API 变化

DO 明确提示 Public Preview 的 data、record 与 execution state 可能丢失。BuildMax
必须保留权威数据与 cleanup record。Environment field、adapter capability、limit 与
pricing 在 GA 前都可能变化。

### 12.2 文档不一致

评估时 egress 文档描述 VPC attachment field，而 Harness Runtime Limits 仍称不支持
private VPC access 和 peering；scoped-secret declaration 已有文档，但 redemption 尚未
全面启用。这些情况要求验证行为，不能猜测哪一页将成为权威。

### 12.3 容量与财务控制

Paused Session 仍占 concurrency。默认账号 limit 不是容量保证，服务也没有 per-session
或 per-product spend limit。循环 Agent 可消耗 compute、tool 和 model balance。
BuildMax quota、admission、deadline、orphan reaping 与外部资源 cleanup 仍不可缺少。

### 12.4 缺少终态 callback

Harness Runtime 当前没有 Run lifecycle webhook notification。Executor integration
需要 polling 或 event-stream observation 加 durable reconciliation，并容忍丢失观察与
重复请求。

### 12.5 Credential 与身份错配

Action Gateway Session 和 Connection 归 team 内 DO user 所有。BuildMax 必须明确个人
Connection、Space service Connection、offboarding 和无人值守 run。用一个 operator
Connection 服务所有 Space 会违反 BuildMax ownership model。

### 12.6 数据位置与私有部署预期

Managed Agents processing 与 Insights telemetry 存在区域约束，包括当前服务的美国
处理。私有部署可能禁止将源码、prompt、tool data 或 telemetry 发送到该边界。Adapter
必须是 opt-in deployment choice，并明确描述 data flow。

### 12.7 能力碎片化

Adapter 可能提供隔离 sandbox，却不提供 policy enforcement、event、approval 或
checkpoint。产品 surface 必须展示真实 boundary 和 evidence，不能展示另一个 adapter
能达到的最佳能力。

## 13. 产生证据的实验

### 13.1 实验 A：Inference provider qualification

**结果：** 一条现有 BuildMax managed-model 路径使用 DO Inference，且不改变 TaskRun
语义。

验证：

- streaming Chat 或 Responses；
- 一个受支持模型的 tool call 与 structured output；
- usage 与 error 进入 BuildMax call ledger；
- timeout、rate limit、overload、authentication failure；
- 对不支持的 provider 专有能力明确拒绝；
- quota 仍由 BuildMax 执行。

第一次实验不包含 Dedicated provisioning、Batch 或 Router 管理；预先创建的 Router
可被当作 model identifier。

### 13.2 实验 B：Action Gateway 应用委托

**结果：** 用户连接 GitHub，Agent 在不获得 credential 的情况下读取一个资源，一次
写操作需要 BuildMax 可见的审批。

验证：

- 稳定的 BuildMax principal 到 Gateway actor 映射；
- 个人 Connection 与无人值守 service Connection 所有权分开；
- 显式 selected tools 与 deny-by-default；
- read allow、测试 write ask、destructive action deny；
- 审批绑定精确参数且不能覆盖 deny；
- credential 不出现在 worker env、prompt、trace、output 或 session state；
- revoke 阻止后续调用；
- provider write 后 timeout 形成 uncertain outcome；
- Gateway invocation identity 与 cost 关联一个 TaskRun trace。

该实验应为已有 app-delegation proposal 提供证据，而不是创建第二套 connector 模型。

### 13.3 实验 C：Harness Runtime executor

**结果：** 一个 TaskRun 通过自定义 BuildMax worker image 执行，仍经由普通 BuildMax
result、Artifact、trace 与 checkpoint 路径完成。

验证：

- 重试情况下 admitted TaskRun 仍只创建一个外部 Session/Run；
- BuildMax run token 只到达目标 execution；
- output 与有用 event 进入现有 Task stream；
- cancel 产生一个可解释终态；
- 成功 portable checkpoint 推进 Task workspace head；
- failure 保留 partial checkpoint 且不推进 head；
- external session loss 与 DO API outage 可 reconciliation；
- orphaned session 可发现并清理；
- Agent revision 或 policy 改变产生新的 resolved environment，而不是隐式修改已有
  Session；
- 缺失 capability 时 admission 失败；
- compute、storage、tool、inference 与 cleanup cost 有记录。

只有实验完成后才应重新设计 `WorkerRunner`。实验应先在当前 interface 旁使用临时
adapter，让实际行为揭示 contract，而不是先把 provisional contract 变成公共设计。

## 14. 开放问题

- 第一个 Action Gateway Connection 应归个人、Space 还是 service identity？
  Offboarding 如何影响 in-flight 与未来 TaskRun？
- Action Gateway approval 能否经 BuildMax Remote Control 投影，而不让审批 client
  获得 DO credential？
- custom Harness adapter 是否能提供足够结构化事件以保留当前 trace 与诊断承诺？
- Harness Session 应对应 Task、TaskRun，还是仅为 ephemeral execution detail？哪项
  实测 startup/continuity 收益足以支持复用？
- lease 丢失或重启后两个 reconciler 竞争时，如何 fence external Session？
- resolved run environment 的哪些部分必须成为 column，哪些进入 canonical manifest，
  哪些已可从 pinned record 推导？
- 真实 MCP name/description catalog 是否已大到需要 semantic tool search？
- 哪些私有部署客户在法律和运维上允许将源码与 tool result 发往公有托管服务？
- 托管 executor 在被描述为 supported 而非 experimental 前，需要怎样的 availability、
  retention、residency 与 support commitment？

## 15. 若获采纳的可能归宿

三项集成问题可以独立解决：

- 获采纳的 DO Inference adapter 应进入 LLM provider architecture、configuration
  reference、managed-model 文档和 provider qualification suite。
- 获采纳的 Action Gateway 路径应更新 app-delegation proposal，或用 Connection、
  actor、per-run grant、approval、revocation 与 tool-call audit 的设计记录取代它。
- 获采纳的 managed executor 应产生 provider-neutral execution backend 设计，更新
  Server architecture 与 TaskRun provenance，并且只有原型定义验收证据后才进入
  backlog。

若实验不能证明它比现有 provider、MCP 与 Kubernetes 路径带来更好的用户或运维
结果，就退役本文，只保留架构经验，不增加 DigitalOcean 专有产品 surface。
