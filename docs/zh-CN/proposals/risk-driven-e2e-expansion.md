# 风险驱动的端到端验证扩展

> **翻译说明：** 本文是[英文原文](../../proposals/risk-driven-e2e-expansion.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
>
> **受众：** 维护者、贡献者、发布运维人员与验证作者 · **状态：** 提案 — 讨论中；本提案中的优雅 worker 丢失与依赖 readiness kind 探针已经交付，其余增量与候选证据仍待完成
>
> **开启日期：** 2026-09-13

相关文档：[路线图](../ROADMAP.md)、
[当前状态评估](../current-state.md)、
[测试指南](../contribute/testing.md)、
[本地端到端验证](../design/端到端测试.md)、
[验证计划](../design/验证计划.md)、
[信任保障](../design/信任保障.md)、
[优雅关闭](../design/优雅关闭.md)，以及
[Beta 就绪记录](../deploy/beta-readiness.md)。

## 目录

- [1. 决策问题与建议](#1-决策问题与建议)
- [2. 问题与当前证据](#2-问题与当前证据)
- [3. 用户结果与约束](#3-用户结果与约束)
- [4. 目标](#4-目标)
- [5. 非目标](#5-非目标)
- [6. 测试归属规则](#6-测试归属规则)
- [7. 方案与权衡](#7-方案与权衡)
- [8. 建议范围](#8-建议范围)
- [9. 交付顺序](#9-交付顺序)
- [10. Harness 与证据契约](#10-harness-与证据契约)
- [11. CI 与发布策略](#11-ci-与发布策略)
- [12. 验收标准](#12-验收标准)
- [13. 风险与控制](#13-风险与控制)
- [14. 开放问题与决策证据](#14-开放问题与决策证据)
- [15. 采纳后的可能归宿](#15-采纳后的可能归宿)

## 1. 决策问题与建议

BuildMax 已经拥有覆盖面较广的确定性端到端基线。当前的开放问题不是是否笼统地
增加更多测试，而是：

> 哪一小组新增端到端旅程最能降低剩余的 Beta 风险，以及每个旅程应在哪一层边界
> 上完成断言？

建议批准一个**风险驱动的部署生命周期增量**，遵循四条规则：

1. 不为增加浏览器测试广度而继续扩张 Portal CRUD 与表现层 happy path。
2. 把已交付的优雅 worker 丢失与依赖 readiness 用例保留为回归，再补充 Server
   重启/重连、worker 写拒绝与已有部分工作的取消。
3. 针对候选产物验证受支持的 worker 契约，包括必须 fail-closed 的负向路径。
4. 每个新增用例只绑定一个关键旅程、一个可独立观测的结果和一个证据包。测试数量
   不是验收指标。

本提案不重新讨论路线图与验证记录已经采纳的 R0–R2 结果。它请求维护者采纳下一
个实现增量的范围、归属边界和交付顺序。若这一形态获采纳，实现工作应进入 backlog，
随后退役本提案。

## 2. 问题与当前证据

现有测试集合对正常运行已有广泛覆盖：

- CLI/TUI 套件在隔离 home 中驱动真实构建的二进制文件，覆盖批准、拒绝、Session
  恢复、并行工具调用、provider 故障以及 trace/Session 持久化；
- Desktop bridge 套件覆盖绑定方法、流式事件、批准、history、rewind、fork 与
  provider 故障；
- Desktop UI 套件通过真实 Wails 开发 bridge 启动 React 应用，并覆盖少量本地模式
  UI 流程；
- Portal 套件覆盖认证、路由、Space 切换与角色、Task、Continue 与 Retry、Workflow、
  文件、插件、管理、审计、响应式布局、可访问性和依赖状态呈现；
- Compose 与 kind smoke 覆盖常规 direct/managed 执行、存储、真实 worker、Artifact
  读取、Retry、授权拒绝、取消、基础 Bash confinement 探针、优雅 worker 丢失，以及
  运行时 MySQL/对象存储 readiness 降级与恢复。

这套基线留下的是另一类风险。一次成功运行不能证明进程消失、依赖拒绝操作，或持久
结果必须跨重启保留时系统的行为。路线图已将候选 worker 证明、持久协调与生命周期
恢复列为 R0–R2。仓库现在已经有优雅 worker 丢失常见路径，以及 MySQL/对象存储
readiness 中断恢复的可重复 kind 证据。这些是开发环境回归，不是已经填写的候选记录：
Beta 就绪行仍为“未运行”，worker 写拒绝、成对恢复、升级、回滚与凭证轮换仍无对应证明。

当前取消测试体现了这一区别：deployment smoke 在第一次模型调用上 stall，因此能
证明取消到达实时执行并保持终态，但运行在取消前尚未产生输出或 Artifact，无法证明
部分证据得到保留。

仓库也缺少一份现行验证矩阵，将 V01–V20 旅程映射到确切自动化证据和显式缺口。
缺少这份矩阵时，增加测试数量可能改善统计数字，却不降低发布风险。

## 3. 用户结果与约束

核心结果是：

> 运维人员可以中断、丢失、重启 BuildMax 部署的某个部分，或暂时移除其依赖访问，
> 同时仍能获得诚实的终态、保留的证据，以及明确的恢复或 Retry 路径，且不会产生
> 重复工作或虚假数据。

当前约束包括：

- Portal、Server、worker、MySQL、对象存储、Redis、ingress 和模型传输跨越不同
  故障边界；
- CLI 与 Desktop 快速套件必须足够便宜，才能留在日常测试循环；
- 确定性模型 harness 必须无需凭证，且不得根据 prompt 推断回复；
- 使用能证明断言的最低自有故障注入环境。已交付的 readiness 探针依赖 Pod 网络、
  Service 移除与 Kubernetes Job，因此必须使用 kind；不需要这些边界的故障仍可使用 Compose；
- release-candidate 的恢复、升级、回滚、TLS 与凭证轮换需要外部生产形态依赖，
  不能由本地 mock stack 声称完成；
- R1 Workflow reconciliation 已拥有权威恢复行为，并有真实 MySQL 重启证据；剩余
  端到端断言是在部署后的多副本拓扑中完成恢复；
- 昂贵的部署套件继续作为合并后、定时或有意执行的发布证据，而不是通用 PR gate。

## 4. 目标

- 用最少的新用例关闭风险最高的部署旅程缺口。
- 让每个用例的 oracle 同时覆盖公开状态、持久状态、保留证据与禁止副作用。
- 让浏览器测试只聚焦必须由浏览器证明的事实。
- 让 deployment smoke 聚焦下层无法证明的进程、网络、存储、scheduler 与 worker
  协作行为。
- 让故障注入具备确定性、明确目标，并能与无关基础设施故障区分。
- 保留有用的部分输出、trace、审计记录与有效 Artifact，同时不宣称未存储的数据存在。
- 在保留证据中呈现源代码、镜像、环境、注入故障与最终分类。
- 所有套件选择与执行入口继续归属 `./make`。

## 5. 非目标

- 最大化 Playwright spec 数量或仓库整体覆盖率。
- 通过浏览器重测每条 handler 或 store 规则。
- 为每个 CRUD 操作添加 Portal happy path。
- 在第一增量中引入随机 chaos testing。
- 使用真实模型作为确定性生命周期行为的正确性 oracle。
- 把本地 kind 集群当作外部 MySQL、S3、TLS、恢复或凭证轮换的 Beta 资格证据。
- 在 R1 service/store 拥有 Workflow 恢复语义前，先在测试代码里定义它。
- 仅为方便测试控制而扩展产品行为，例如加入逐 run 模型选择。
- 用更慢的端到端覆盖替换聚焦的单元、真实 MySQL、组件或 handler 测试。

## 6. 测试归属规则

每个建议用例都必须选择能够证明其独有断言的最低边界：

| 断言 | 权威证据边界 |
|---|---|
| 纯验证、状态转换、授权规则或渲染决定 | 单元、组件、handler 或真实 MySQL 测试 |
| 已发布 Portal bundle、浏览器路由、Session 恢复、可访问性或可见恢复状态 | 针对真实部署的 Portal Playwright |
| Server、worker、存储、scheduler、gateway 或进程生命周期协作 | Compose deployment smoke 或 failure suite |
| ingress、Kubernetes Job 生命周期、Pod security context 或跨副本行为 | kind lifecycle suite |
| 外部依赖恢复、升级、回滚、TLS 或凭证轮换 | 固定版本的 release-candidate 资格验证 |

只有移除浏览器后会让断言失去证明时，才应新增浏览器用例。无关 setup 可以使用公开
fixture API，但被断言的用户结果必须跨越用例命名的边界。

## 7. 方案与权衡

### 方案 A：继续广泛扩张 Portal 测试

每当增加页面或操作时就加入浏览器用例，包括普通 CRUD 与表现变体。

该方案直观且产生可见覆盖，但会扩大最慢的共享套件，重复组件与 handler 断言，
对开放的 worker 丢失、依赖故障和恢复风险帮助很小。

### 方案 B：增加大型统一 Chaos 套件

构建一个 runner，在 Compose 与 kind 中随机杀死服务、扰乱时序并执行大量并发旅程。

它可能发现涌现缺陷，但不适合作为第一层正确性 gate。随机注入使故障难以复现和分类，
一个大 runner 也会在各边界的独立 oracle 稳定前耦合不相关的产品边界。

### 方案 C：增加定向生命周期旅程——推荐

引入少量具名、确定性的故障控制。每个控制作用于一个被记录的时点，每个旅程拥有
有界终态预期，每个断言同时覆盖保留证据与禁止副作用。

这比再加一个浏览器测试需要更多 harness 设计，但能直接服务 R0–R2，并可在以后
转化为候选资格证据。它也允许 Compose 与 kind 承担不同断言，而不假装二者可互换。

### 方案 D：停止扩展，等待候选资格验证

依赖当前套件，在外部 Beta 演练期间发现剩余缺口。

这避免了推测性的 harness 工作，但会把可重复的本地故障推迟到昂贵候选环境中。
若在那里发现 worker 丢失或存储拒绝缺陷，发布工作停止后仍没有低成本回归路径。

## 8. 建议范围

### 8.1 候选 Worker 契约

只在候选证据缺失的位置扩展现有 trust/deployment 探针：

- 所需沙箱 enforcement 不可用时，worker 拒绝执行；
- worker 选择的进程限制真实存在，并产生文档约定的诊断结果；
- command 与 HTTP Hook transport 遵循受支持的 worker 策略；
- 已解析的 stdio MCP 在 assembly 阶段失败，且发生在任何子进程或模型调用之前，
  同时允许的远程 transport 保持可诊断；
- public listener 不提供 worker 路由，worker listener 仍要求受支持拓扑约定的
  run-scoped authority；
- TaskRun 诊断报告实际 boundary 与 MCP treatment，而不是请求值或假设值。

本地 smoke 证明可重复机制；固定版本候选运行证明不可变镜像与部署配置。

### 8.2 Kind 生命周期

两个确定性旅程中的第一个已经部分交付：

1. **Worker 丢失：** 已交付的 kind 探针在 claim 后、terminal report 前终止
   Kubernetes worker Job，并证明持久、可诊断的 `FAILED` 结果。Job 删除会发送
   `SIGTERM`，所以这是 rollout/eviction/drain 的优雅路径；无声硬故障 liveness
   reaper 仍由 store 层测试覆盖，不冒充部署证明。
2. **Server 重启与重连：** 在 direct Task 或前台 turn 可观测期间重启 serving path。
   重连后，持久状态重建同一份工作，不产生重复 TaskRun、输出、Artifact、usage 或
   message-history 写入。

Workflow 变体以已交付的 Server-owned recovery loop 及其真实 MySQL 契约为权威。
端到端用例只补充这些测试无法证明的部分：部署后的 worker update、Server 重启和
无重复执行的多副本恢复。

### 8.3 依赖故障

由于断言依赖 Pod 网络与 readiness 行为，以下两个定向控制已在 kind 中交付：

- MySQL 暂时不可用，覆盖 readiness 失败及无需重建 Server 的恢复；以及
- 对象存储读/readiness 拒绝后恢复，并保留原始 bucket。

以下控制仍待完成：

- 对象存储写拒绝，覆盖诚实的运行失败和不存在虚假可下载 Artifact；
- 有工作进行时优雅关闭 Server，覆盖 drain 行为，以及重启后不存在搁置的运行记录。

每个控制记录 arm 与 release 的时间。测试必须区分预期注入拒绝与注入前已经存在的
不健康环境。

### 8.4 有部分工作的取消

扩展确定性模型控制，使一个被选择的 run 能够：

1. 产生已知输出或 Artifact；
2. 在命名的后续时点阻塞；
3. 接收取消；
4. 为 teardown 干净释放。

结果必须保持 `CANCELED`，只保留实际已提交的证据，不宣称缺失对象存在，并在取消
成为权威状态后不再执行任何工具或模型工作。

控制必须按 run 拥有的不透明标识符定位，不得根据模型 alias 或 prompt 内容定位。
如果不改变产品 authority 就无法添加安全的 run-scoped 控制，此用例应暂停并返回
设计讨论，而不是把逐 run 模型选择作为测试 plumbing 加入产品。

### 8.5 Portal 恢复呈现

只为部署旅程引入或发现的运维可见状态增加浏览器覆盖，例如 Task 页面诚实呈现
worker 失联，或依赖故障后 System Status 恢复。如果 deployment smoke 已拥有故障
注入，则不要再通过 Playwright 重复注入本身。

### 8.6 明确延后的候选演练

数据库与 bucket 成对恢复、schema 升级与二进制回滚或破坏式 cutover 恢复，以及
凭证轮换仍是 release-candidate 演练。本增量可以构建复用的证据收集能力，但本地
套件通过不得把这些 Beta 条目标记为完成。

## 9. 交付顺序

建议顺序遵循路线图，而不是测试实现便利性：

1. **映射现有证据。** 创建 V01–V20 验证矩阵，链接确切现有测试并显式标记缺口。
   这不增加新断言，而是防止重复覆盖并建立 journey ID。
2. **关闭 R0 候选 worker 探针。** 增加验证 fail-closed worker 行为所需的可重复
   负向控制，再在固定版本候选环境复用相同断言。
3. **增加部署后的 R1 拓扑证据。** 在候选拓扑中验证 worker update、重连、并发 turn、
   Redis 故障和已交付的 Workflow recovery loop。现有真实 MySQL 契约继续作为
   reconciliation 语义的权威。
4. **增加 kind 生命周期旅程。** 优雅 worker 丢失路径已交付；Server 重启、重连、
   已部署拓扑中的 Workflow 恢复，以及任何可复现的无声硬故障证明仍待完成。
5. **增加依赖旅程。** kind MySQL 与对象存储 readiness 中断/恢复探针已交付。
   worker 对象存储写拒绝和负载下 shutdown 仍待实现，具体归属由其所需边界决定。
6. **增加部分工作取消。** 落地最小的 run-scoped harness 能力和保留/禁止副作用断言。
7. **演练固定版本候选。** 使用不可变 image digest 与外部依赖执行 Beta 就绪的运维、
   故障、恢复、升级、回滚或 cutover 及轮换流程。

步骤 2–6 在采纳后应成为独立 backlog 任务。只有 ownership 与依赖不冲突时才能并行；
采纳本提案不会静默重排维护者现有 plugin backlog，也不会重新创建已完成的 Workflow
恢复任务。

## 10. Harness 与证据契约

### 10.1 故障控制

每个故障控制必须提供：稳定名称与版本；显式 arm 点和 release/teardown 操作；目标
run、进程或依赖；有界 deadline；证明故障真实发生的 marker；幂等清理；以及当测试
结束却未实际触发已 arm 故障时失败。

控制可以延迟、断连、终止、暂停或拒绝，但不得检查 prompt 内容后猜测产品意图。

### 10.2 断言

每个生命周期旅程断言：

1. 公开与运维可见状态；
2. 权威持久状态；
3. 适用的 trace、audit、usage、日志与 Artifact 证据；
4. 文档约定的终态 deadline；
5. 禁止的重复、虚假 Artifact、隐藏 Retry、取消后工作、跨 Space 暴露和搁置中的
   running 记录；
6. 受支持的 Retry 或恢复路径。

只有 `err == nil`、HTTP 已接受或单一终态并不足够。

### 10.3 证据包

定时生命周期运行应在 `.artifacts/verification/` 下产出现有验证计划定义的契约。
manifest 至少记录 source commit 与 dirty state、image digest、环境与依赖版本、
journey/scenario 版本、注入故障及时间戳、终态与分类、证据路径与清理结果，以及显式
skip 和未测试限制。

成功运行同样需要 manifest。只在失败时上传 trace，无法在之后证明究竟是哪一个不可变
候选或环境通过了测试。

## 11. CI 与发布策略

- 快速 CLI、Desktop bridge、组件、handler 与真实 MySQL 测试保持现有 PR 位置。
- Portal 浏览器测试继续作为不重试的部署证据；只有证明浏览器特有恢复结果时才新增
  用例。
- Compose failure 与 kind lifecycle 套件在合并后、定时和手动触发时运行，不成为
  通用 PR gate。
- 失败的定时生命周期用例在被分类为产品、harness 或基础设施故障前保持可见；诊断
  重跑不抹去第一次结果。
- 当 `./make e2e all` 仍未包含 kind、managed 路径、Desktop UI/native packaging、
  真实 MySQL 与候选演练时，不得把它当作完整发布证据。应修正其文档名称，或引入一
  个不同的 intent-level 命令，报告每项 included、skipped 与 external-only scope。
- Beta 资格验证消费固定候选证据，不从最近成功的 `main` workflow 推导通过。

## 12. 验收标准

建议增量在满足以下条件时完成：

当前进度只满足下面与优雅 worker 丢失和依赖 readiness 有关的部分；它尚未完成本提案，
也没有关闭任何候选记录行。

- 验证矩阵把每个 V01–V20 旅程映射到确切证据或显式缺口；
- 候选 worker 探针针对受支持 profile 覆盖 Bash enforcement、进程限制、Hook/MCP
  treatment、worker API isolation 与诚实诊断；
- 被硬杀的 worker 产生有界、可诊断的终态运行，并能显式成功 Retry，且不存在隐藏
  或重复执行；
- Server 重启与重连保留一份权威工作 history，不重复 run、输出、Artifact、usage
  或消息；
- MySQL 与对象存储拒绝用例呈现诚实 readiness/status，不产生虚假数据，并在无需
  重建或重写成功状态的情况下恢复；
- 有部分工作的取消保留已提交证据，且不再执行后续工作；
- 每个注入故障都证明其真实发生、得到清理，并留下经脱敏且绑定源代码与环境的 manifest；
- 新套件保持在文档时长预算内；候选专属边界的例外必须有明确理由；
- 没有新增 Portal 测试仅仅重复组件、handler 或 store 断言；
- 只有运维人员实际执行并记录外部资格演练后，才能关闭固定候选的 Beta 就绪条目。

## 13. 风险与控制

| 风险 | 控制 |
|---|---|
| 故障注入退化为易抖动的时序编排 | arm 具名控制，等待可观测 marker，使用有界轮询，未触发故障则失败 |
| 测试向产品暴露专用控制面 | 只在 test support 与 smoke overlay 中编译或部署控制；生产 manifest 不暴露 |
| 一个全局模型 stall 影响无关 run | 优先使用 run-scoped 不透明控制；无法隔离时只串行执行该窄用例 |
| 浏览器套件运行时间持续增长 | 要求浏览器专属断言，并用 fixture API 完成无关 setup |
| E2E 在 domain ownership 前定义行为 | 先要求单元/真实 MySQL 权威，尤其是 Workflow 恢复 |
| 本地 smoke 被误认为发布证明 | 记录环境类别，并让外部 Beta 条目保持显式开放 |
| 保留日志泄漏凭证或创作内容 | 复用脱敏边界，默认私有保存证据，并测试 secret-like query/header 移除 |
| 组合命令隐藏被跳过的 scope | 按套件输出明确的 run、skip、external-only 与 failure 结果 |

## 14. 开放问题与决策证据

在接纳实现任务前，维护者应决定：

1. 第一批已采纳切片是否包含验证矩阵，还是把它作为已经接受的验证计划维护工作独立
   落地？
2. 剩余的 worker 写入与 shutdown 故障用例应作为另一个 matrix cell 扩展
   `deployment-smoke.yml`，还是在具有不同耗时与 ownership 策略的独立 workflow 中
   运行？readiness 中断探针已经归属 kind，因为它们断言 Pod 网络与 Service 行为。
3. worker 开始模型执行前，可用的最小安全 run-scoped 模型控制标识符是什么？
4. 第一个 Server 重启旅程应是 direct Task、前台 Conversation turn，还是两者？选择
   应由更大的未证明持久性风险驱动，而不是追求界面对称。
5. 哪些 worker contract 检查可作为确定性本地探针，哪些必须由运维人员检查固定候选
   Pod 与 NetworkPolicy？
6. 使用当前 heartbeat、reaper 与 readiness 间隔时，硬性 worker 丢失和依赖恢复的
   合理时长预算是什么？

决策所需证据包括：一个定向故障控制的实测原型、当前定时 workflow 的耗时与 flake
历史、已交付的 R1 reconciliation 契约及其重启测试，以及针对 Compose 与 kind 的
evidence manifest 试运行。如果原型无法区分注入故障与环境不稳定，则 harness 尚未
准备好拆解为 backlog。

## 15. 采纳后的可能归宿

采纳后不应让本提案继续成为第二事实源：

- 持久的归属与断言规则进入[本地端到端验证](../design/端到端测试.md)；
- journey 覆盖与证据要求进入[验证计划](../design/验证计划.md)；
- 已采纳的优先级或顺序变更进入[路线图](../ROADMAP.md)；
- 可独立执行的切片按维护者决定的顺序进入 [backlog](../../backlog/README.md)，且不
  重复现有 R1 任务；
- 只有命令和前置条件实际存在时才更新[测试指南](../contribute/testing.md)；
- 候选结果记录在 [Beta 就绪记录](../deploy/beta-readiness.md)。

随后删除本提案及其索引条目；Git 历史保留讨论。
