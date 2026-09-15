# 人员停用与执行权限

> **翻译说明：** 本文是[英文原文](../../proposals/personnel-deactivation-lifecycle.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
>
> **受众：** 维护者、运维人员与安全评审者 · **状态：** 提案 — 讨论中
>
> **讨论开始：** 2026-09-15
>
> **主要领域：** 运维与部署

相关文档：[企业功能要求](enterprise-capability-requirements.md)、
[企业身份与访问](../design/企业身份与访问.md)、
[Space 成员生命周期](../design/Space成员生命周期.md)、
[Agent 执行与 Task 线程](../design/Agent执行与Task线程.md)、
[定时 Agent 执行](../design/定时Agent执行.md)及
[系统管理操作](system-administration-operations.md)。

## Contents

- [1. 决策问题](#1-决策问题)
- [2. 核心结果与证据](#2-核心结果与证据)
- [3. 已核实的当前约束](#3-已核实的当前约束)
- [4. 不变量](#4-不变量)
- [5. 方案](#5-方案)
- [6. 推荐契约](#6-推荐契约)
- [7. 执行资格](#7-执行资格)
- [8. 竞争、失败与恢复](#8-竞争失败与恢复)
- [9. 所有权恢复](#9-所有权恢复)
- [10. API、数据与服务归属](#10-api数据与服务归属)
- [11. 交付切片](#11-交付切片)
- [12. 验证](#12-验证)
- [13. 非目标](#13-非目标)
- [14. 开放问题与决策证据](#14-开放问题与决策证据)
- [15. 采纳后的归宿](#15-采纳后的归宿)

## 1. 决策问题

当 System Administrator 停用账户，或 Space owner 移除一项成员资格时，哪些人员凭证
和无人值守执行应停止、多久内停止，以及人员无法继续登录后哪些持久工作仍应由组织访问？

建议建立一项统一权限契约，而不是新增员工或离职子系统：

> `user.disabled_at` 是部署级执行资格闸门，`space_member` 记录不存在是 Space 级
> 执行资格闸门。所有准入或派发工作都检查这两个事实。撤权取消尚未完成的工作，保留
> 历史与结果，并且权限恢复时绝不静默恢复由原权限产生的自动化。

有引导的离职体验只是这些既有状态的投影与协调使用方式，不是新的 `organization`、
`employee`、`offboarding_job` 或策略语言实体。

## 2. 核心结果与证据

运维人员在提交人员变更前必须能回答：

1. 哪些访问会立即停止；
2. 哪些无人值守工作会暂停或取消；
3. 哪个 Space 需要继任 owner；
4. 人员无法继续登录后，哪些结果和审计历史仍然保留。

此需求不是从企业功能清单推断出来的。OIDC 登录和账户关联已经存在，而企业功能要求
盘点把完整 leaver journey 识别为第一个尚未闭合的衔接面：Session、机器凭证、邀请、
成员资格、Schedule、排队中与运行中的 TaskRun，以及保留结果，目前并不共享一项明确
的生命周期契约。

首个获采纳的部署目标仍是私有网络内受信任的 Space。目标是可预测的权限边界与有界
停止，不是面向公网多租户的雇员管理系统。

## 3. 已核实的当前约束

当前代码已经提供了契约中的部分能力：

| 关注点 | 当前行为 | 剩余缺口 |
|---|---|---|
| 用户请求 | `access.Guard` 在每次请求中检查活跃账户和活跃 `auth_session` | 人员请求侧无缺口 |
| 登录与 refresh | 停用账户会被拒绝 | 若没有外部生命周期通道，仅在 IdP 停用人员时，BuildMax 要到重新认证才会感知 |
| Session | 管理员停用处理器先设置 `disabled_at`，然后撤销全部 Session | 两次写入不是同一状态变更；权威闸门已改变后，后续失败会返回错误 |
| Webhook key | 已停用 owner 的 key 被拒绝但仍保留 | 重新启用账户会让旧 key 再次可用；界面不呈现 leaver 决策 |
| Space 成员资格 | 删除成员记录后，该人员的下一次 Space 请求被拒绝；账户停用会保留成员记录 | Schedule、Workflow 与派发路径并非都重新检查成员资格 |
| Schedule | Schedule 到期时，如创建者已停用，dispatcher 会暂停它 | 不检查创建者是否仍为成员；低频 Schedule 可能直到下次到期前仍显示为启用 |
| PENDING TaskRun | scheduler 在启动 worker 前把已停用创建者的 pending run 标为失败 | 账户查询失败时放行、不检查成员资格，且把撤权标成失败而不是取消 |
| 运行中 TaskRun | Worker 轮询持久取消请求，reaper 为无响应请求提供上界 | 账户停用和成员移除不会发出取消请求 |
| WorkflowRun | reconciliation 会持续以原 run 创建者身份派发后续步骤 | 账户停用或成员移除后仍可能准入下一步骤 |
| Run 凭证 | run token 限定于一个 TaskRun | 即使后续 TaskRun 由另一位成员发起，用户 claim 仍来自 `task.created_by` |
| 结果 | Task、TaskRun、Artifact、trace 和模型调用记录归 Space 所有，通过当前成员资格读取 | 这是正确的保留边界，不应改为创建者所有权 |

其他约束决定了最小设计：

- 一个账户可以属于多个 Space，并有一个 personal Space。
- Space owner、Space admin/member 和 System Administrator 是相互独立的权限；系统授权
  不意味着可访问 Space 内容。
- TaskRun 的准入环境不可变。撤权无法撤销已经发生的外部副作用，也不能热修改运行中进程。
- `CANCELED` 是与 `FAILED` 不同的 TaskRun 与 WorkflowRun 终态；部分输出和 Artifact
  仍应作为证据保留。
- PAT 和 service account 尚不存在。目前唯一账户级无人值守凭证是 webhook key。

## 4. 不变量

1. **停用优先于便利性。** `disabled_at` 提交后，新的人员请求、webhook 准入、Schedule
   触发、Workflow 步骤、Continue、Retry 或 worker 派发都不得再取得该用户权限。
2. **在执行边界检查成员资格。** 移除成员资格后，即使工作源自持久 Schedule 或
   Workflow 而非 HTTP handler，也不得在该 Space 启动新工作。
3. **一个 TaskRun 对应一个发起主体。** 执行资格和 run token 使用
   `task_run.created_by`；`task.created_by` 仍是持续 Task 的历史来源，而不是之后所有
   turn 的权限来源。
4. **已经启动的工作要停止，而不是被改写。** 撤权记录取消请求。Worker 通过正常优雅
   停止路径进入 `CANCELED`；无响应 worker 由 stale-run 后备机制处理。
5. **Space 数据长于人员访问。** 成员记录、Task、结果、Artifact、trace、用量和审计
   记录不会被删除或改写归属。剩余成员仍通过 Space 访问它们。
6. **恢复必须显式。** 启用账户或重新加入成员，不会复活已撤销 Session、已取消 run、
   已暂停 Schedule 或已经终止的 WorkflowRun。
7. **安全闸门失败时关闭。** 在准入或派发时无法确定账户或成员资格，则拒绝或推迟工作；
   不能乐观假定权限可能仍然有效后就启动工作。
8. **历史保留原 actor。** 停用不会把创建者 ID 换成运维人员或继任者。恢复动作另行记录
   自身的类型化审计 actor。

## 5. 方案

### 方案 A：仅记录当前尽力而为的行为

这是代码改动最小的方案，但被移除的成员仍可驱动 Schedule 和 Workflow 后续步骤，
scheduler 的账户检查与 worker 启动之间也仍有竞争窗口，无法满足目标。

### 方案 B：删除或重分配该人员创建的所有内容

删除工作会丢失组织证据，自动修改创建者 ID 会伪造来源，把每个对象都转给别人也混淆
作者与 Space 所有权。因此否决。

### 方案 C：增加持久 Offboarding Job 实体

Job 可以记录大规模级联、重试和部分完成。当前没有部署证明其规模或审批过程需要另一个
状态机。权威闸门可以保证收尾安全，有界 reconciler 可以保证最终收敛。只有简单模型经
运维验证确实不足时才重新考虑此方案。

### 方案 D：闸门、取消、保留并协调

使用账户和成员状态作为权限，集中执行资格判断，为活跃工作请求取消，暂停未来触发，
并保留 Space 所有的历史。增加只读影响投影和严格受限的 owner 恢复。这是推荐方案。

## 6. 推荐契约

部署级账户停用和单个 Space 成员移除，对同一资源有不同处理：

| 资源或动作 | 账户已停用 | 从 Space 移除成员资格 |
|---|---|---|
| 密码、登录码、OIDC 登录、refresh | 拒绝所有新 Session；撤销已有 BuildMax Session；保留 external-identity link 供显式恢复 | 不变 |
| 已签发 access token 与 WebSocket | 下一次认证请求或连接检查时拒绝 | 下一次指向该 Space 的请求时拒绝 |
| Webhook key | 停用期间立即拒绝；有引导 leaver flow 默认永久撤销 | 不变，除非未来存在 Space 所有的 key |
| PENDING invitation | 保留为不构成权限的记录；停用账户无法接受 | 其他 Space 的邀请不变；被移除的 Space 不存在已接受成员资格 |
| 成员记录 | 为来源和可能的显式返回保留 | 只删除指定 Space 的成员资格 |
| 该人员创建且启用的 Schedule | 以 `creator_disabled` 原因暂停 | 该 Space 内以 `creator_not_member` 原因暂停 |
| 该人员发起的 PENDING TaskRun | 派发前进入 `CANCELED` | 仅取消被移除 Space 所有的 run |
| SCHEDULED 或 RUNNING TaskRun | 记录取消请求，由 worker/reaper 收敛 | 同样处理，但仅限被移除 Space |
| WorkflowRun | 不派发下一步骤；取消活跃 TaskRun，并把 WorkflowRun 收敛为 `canceled` | 同样处理，但仅限被移除 Space |
| 已完成 TaskRun 与 WorkflowRun | 保持不变 | 保持不变 |
| Task、Issue、Artifact、trace、用量和审计 | 继续归 Space 所有；人员停用时无法读取 | 保留；剩余成员继续访问 |
| Personal Space | 完整保留，账户停用期间不可访问 | 不能作为普通共享 Space 成员资格移除 |

Leaver flow 仅把**暂停**与**凭证退役**作为运维选择区分，而不新增账户状态。两者都以
`disabled_at` 为闸门。暂停可以保留已被拒绝的 webhook key，以便之后显式恢复；有引导
离职路径默认撤销这些 key，因为必须比个人存在更久的集成需要单独设计的机器主体，而
不是被遗忘的个人凭证。

仅在 IdP 侧处理离职仍然只有时间上界，而不是立即生效。没有 SCIM 或已验证的提供方
logout 通道时，Okta 停用人员不会通知 BuildMax。因此受支持的即时流程是停用 BuildMax
账户；OIDC Session 绝对时长是仅靠 IdP 时的最大上界，必须由 Phase 3 Okta 验收记录。

## 7. 执行资格

引入一个应用层问题，概念上为：

```go
type ExecutionEligibility interface {
    Check(ctx context.Context, userID, spaceID string) error
}
```

它针对已停用或不存在的账户、缺失成员资格或权限存储不可用返回类型化拒绝。它不回答
角色专属管理问题；按照当前 Space 策略，普通成员资格足以运行工作。

Task 应用服务在 Create、Continue、Retry、Schedule 准入和 Workflow step 准入时调用它。
HTTP guard 仍是尽早返回的 adapter，但不再是唯一强制点。Schedule dispatcher 与 Workflow
reconciler 在产生后续工作前调用同一规则。

Scheduler 在领取 PENDING run 后、签发 run token 或启动 worker 前执行最终检查。Worker
首次获取 run 时再检查一次，因此与 scheduler 并发的停用或成员移除无法越过边界。权限
存储错误会让 run 以可诊断方式保持未派发，而不是把暂时故障标为永久撤权。

Run token 使用 TaskRun 发起者。首次 run 通常与 Task 创建者一致；另一位成员 Continue
时，凭证会如实携带该成员，停用原 Task 创建者不会取消一位仍合资格同事发起的后续 run。

## 8. 竞争、失败与恢复

撤权和收尾承担不同的正确性职责：

1. 账户停用或成员移除首先作为权威闸门提交，并写入相应审计事件。
2. 基于集合的收尾会暂停受影响 Schedule、取消 PENDING run，并为 SCHEDULED/RUNNING
   run 请求取消。
3. 有界 reconciliation loop 查找发起主体已不合资格的启用 Schedule 和 active run，并
   重复这些幂等动作。它不需要 Offboarding Job，因为执行资格本身就是持久的未完成工作
   判定条件。
4. Workflow reconciliation 在每次派发下一 step 前检查资格；拒绝会取消 WorkflowRun，
   并阻塞其余 pending step。

该顺序使收尾失败只造成不便，而不会重新授权：所有新准入和派发路径仍会拒绝。Admin
响应分别报告权限闸门结果和收尾计数，因此超时不会诱使运维人员盲目重复或撤销停用。

取消上界是 worker 取消轮询间隔，加上 stale-run 后备机制收敛无响应 run 前的配置化
cancel grace。运维界面必须显示这些有效值。在 worker 观察请求前，正在执行的工具调用
或外部写入可能完成。BuildMax 不宣称可以回滚外部副作用。

Reconciliation 按批次限制且幂等。并发的停用、成员移除、派发、worker 完成和显式取消
通过既有受保护状态转换收敛；终态 run 永远不会改变。

## 9. 所有权恢复

计划内 leaver 影响报告列出目标作为唯一有效 owner 的每个共享 Space。正常路径是由该
owner 在账户停用前转移所有权。

紧急停用不能因为所有权问题而被阻止，因此可能留下记录 owner 无法登录的共享 Space。
恢复需要一项范围严格的 System Administrator 动作：

- 仅在共享 Space 的所有已记录 owner 均已停用时适用；
- 继任者必须已经是该 Space 的已启用成员；
- 不能操作 personal Space，也不能创建成员资格；
- 原子地执行既有所有权转移；
- 不赋予管理员 Space 成员资格或内容访问权；
- 事务性记录 `space.ownership_recovered` 审计事件，包含已停用 owner、继任者及人员或
  operator actor。

Portal 与 `buildmax admin` 可以通过 Admin API 暴露这项只操作元数据的恢复能力。公共
Server 或 IdP 不可用时，数据库相邻的 `buildmax-server` 命令可以调用同一服务执行
break glass。它不是管理员转移健康 Space 的一般权力。

## 10. API、数据与服务归属

不新增顶级实体。

既有账户状态路由仍是唯一账户启停变更。在其前增加只读影响投影：

```text
GET /api/admin/users/{user_id}/deactivation-impact
```

它只返回元数据与计数：活跃 Session 与 webhook key；成员资格与角色；唯一所有的共享
Space；按 Space 划分的启用 Schedule；按状态划分的 active TaskRun/WorkflowRun；以及配置
的取消上界。它不返回 prompt、input、output、Artifact 名称、trace、原始错误或 Secret。

`PUT /api/admin/users/{user_id}/state` 分别报告：

- 账户闸门是否改变；
- 撤销的 Session 与 webhook key；
- 暂停的 Schedule；
- 取消的 PENDING run；
- 已请求取消的 active run。

结果由服务而非 handler 协调。范围明确的 `internal/service/accountlifecycle` 能力可以依赖
账户、Session、webhook key、Schedule、TaskRun、WorkflowRun、Space 和审计端口。各领域
package 保留自身状态转换验证；orchestrator 不重复实现它们。

为运维诊断增加两个小型来源字段有明确依据：

- `schedule.pause_reason`：`manual`、`creator_disabled`、
  `creator_not_member` 或 `consecutive_failures`；重新启用时清空。
- `task_run.cancel_reason`：`user_requested`、`creator_disabled` 或
  `creator_not_member`；首次请求取消后不可变。

准确 actor 继续使用已有 actor 字段和审计记录。Reconciler 可以把 `cancel_requested_by`
留空并设置原因；发起停用或移除的事件仍通过用户和 Space 元数据关联。不存储自由文本
人员原因。

成员移除仍归 `internal/service/space` 所有。它提交成员状态变更和审计，然后为该用户与
Space 调用共享收尾能力。账户停用调用部署级形式。执行资格检查共享；两个服务都不导入
对方的 orchestrator。

## 11. 交付切片

提案获采纳后，每个切片可独立评审：

1. **集中执行资格。** 增加服务级准入检查、感知成员资格的 Schedule/Workflow 派发、
   最终 worker-start 闸门和基于 TaskRun 发起者的 run token。无需新 UI 即可关闭权限绕过。
2. **可收敛的取消与暂停。** 增加原因字段、集合化收尾和有界 reconciler；把撤权结果从
   `FAILED` 改为 `CANCELED`。
3. **账户停用影响与凭证处理。** 增加只返回元数据的影响路由，把停用编排移出 handler，
   保证 Session 撤销不复活，并提供永久退役 webhook key 的选择。
4. **Space owner 恢复。** 增加仅适用于 owner 已停用的恢复服务、已认证客户端、事务审计
   与 MySQL 并发测试。
5. **Portal 引导与运维流程。** 展示影响、继任者阻塞、明确的 in-flight 上界、收尾结果和
   事后验证。
6. **Okta 验收。** 在 Phase 3 提供方环境中验证 BuildMax 停用、仅依靠 IdP 时的上界、
   IdP 故障、break glass，以及恢复后权限不被意外复活。

本提案本身不会把这些任务加入 backlog，也不会调整 R2/R3 顺序。何时进入执行仍由路线图
和维护者决定。

## 12. 验证

| 范围 | 所需证据 |
|---|---|
| Core/service | 对账户状态、成员资格、trigger source、TaskRun 发起者和所有生命周期结果做表格化矩阵测试 |
| Handler | 影响响应只包含元数据；停用和被移除调用者在所有相关路由上被拒绝 |
| MySQL | 停用/移除与 Schedule claim、Workflow reconcile、PENDING claim、run 完成、取消及所有权恢复竞争后收敛到唯一合法状态 |
| Worker | scheduler claim 与 worker fetch 之间撤权时不启动 Agent；运行中 Agent 观察取消并保留部分证据 |
| 多副本 | 撤权后两个 Server 都不能派发后续 step 或 Schedule；跨副本收尾保持幂等 |
| Portal | 计划内和紧急 leaver journey 展示影响、阻塞项、有界 in-flight 状态和最终验证，同时不泄露 Space 内容 |
| OIDC | 手动 BuildMax 停用立即生效；仅 IdP 停用保持在文档化绝对 Session 上界内；IdP 故障时 break glass 可用 |
| 授权 | System Administrator 恢复不能读取 Space 内容、转移健康 Space、创建成员或操作 personal Space |
| 文档 | 当前状态、认证、管理、调度、Task 执行、OpenAPI 与 Enterprise identity Phase 3 证据保持一致 |

主验收场景是一位人员拥有两个 Session、一个 webhook key、两个共享 Space 的成员资格、
其中一个 Space 的唯一所有权、两个 Space 中的 Schedule、一个 PENDING run、一个 RUNNING
run 和一个多步骤 WorkflowRun。测试必须展示账户停用后、owner 恢复后，以及显式重新启用
后的准确状态。

## 13. 非目标

- SCIM、SAML、目录同步或 IdP group 到 Space 的映射。
- 删除账户、重分配邮箱、合并账户或转移 personal Space。
- PAT、service account、workload identity 或自动转移个人 webhook key。
- 改写创建者字段或删除已完成工作。
- 通用雇佣、HR、Organization、审批或策略引擎。
- 保证回滚在观察取消前已经开始的模型调用、工具调用或外部副作用。
- 账户重新启用或重新邀请后自动恢复 Schedule 或 retry 已取消工作。
- 在权限转换及其审计记录具备所需事务语义前宣称合规级审计。

## 14. 开放问题与决策证据

1. 对首个明确企业部署而言，worker poll 加 cancel grace 的上界是否足够，还是 runner 必须
   在更短紧急上界后删除或终止 worker？
2. 临时账户暂停是否需要恢复 webhook 集成，还是每次停用都应永久撤销账户级机器凭证？
3. 仅限 owner 已停用时由 System Administrator 恢复是否可接受，还是每个部署都必须在
   离职前准备第二位 Space owner？
4. 目标部署是否需要托管 CLI/Desktop SSO，从而把原生 Session 和本地凭证清理纳入同一
   journey？
5. 在何种实际规模下，执行资格 reconciler 才需要持久工作队列，而非有界 SQL scan？

采纳需要明确的运维人员和目标部署、双方同意的离职与运行中工作上界、计划内与紧急离职
流程 walkthrough、对 worker 取消的 threat-model review，以及只返回元数据的影响响应不
泄露 Space 内容的证据。

## 15. 采纳后的归宿

把持久执行资格和保留决策移入 Agent 执行、定时执行、Space 成员与 Enterprise identity
设计记录；把运维流程放入部署认证/管理文档；把获采纳优先级写入路线图；并按交付切片
创建 backlog task。随后删除本提案，由 Git 历史保留决策过程。
