# 无 Space 成员身份的运维事故演练 — 2026-09-28

> **翻译说明：** 本文是[英文原文](../../../contribute/exploratory-runs/2026-09-28-portal-admin-operator-incident-drill.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。

**章程。** 即[跨 Space 工作可见性](../../proposals/admin-cross-space-work-visibility.md) §8 的第一步：在多 Space、双 Server 部署中，一个不属于任何 Space 的系统管理员，能否只凭现有 Administration 视图回答"BuildMax 是否在处理工作、进展停在哪里、谁能处理"？对每个注入的事故，记录运维答不上来的问题、缺失的字段、采取的动作，以及回答它是否需要 Space 内容。角色：`ops@drill.local`，持有 `system_admin` 授权，不属于任何 Space。界面：Portal Administration（用 `drive-portal` 驱动）、Admin API 与 `buildmax admin`；之后是运维现实中的兜底手段 `kubectl` 与 server 日志。准备完成后主动演练约 25 分钟。未覆盖：提案的第二个问题（观察真实的多 Space 成员），这需要真人而非 Agent 走查。

**环境。** worktree 位于 `cc1a41d1`（main），干净。macOS arm64、Go 1.26.6、kind 0.31.0、kubectl 1.35.1。任务独占的临时集群 `buildmax-eph-bc6fca9f`，由 `BUILDMAX_KIND_EPHEMERAL=1 ./make kind up` 创建：两个 `buildmax-server` 副本、MySQL、MinIO、Kubernetes worker Job，`worker_llm_transport: direct`。起始数据：`./make kind fixtures --runs`（BuildMax QA 与 QA Pagination），外加演练创建的两个团队 Space——Drill Payments（所有者 `pat@drill.local`）与 Drill Research（所有者 `rin@drill.local`）；其任务输入带有可识别标记（`LEAKMARK-*`、`ACME-CONFIDENTIAL`、`NIGHTJAR`）供泄漏检查。Alice 通过 `POST /api/admin/grants` 为 `ops@drill.local` 授权，属于准备步骤。

**模型。** 仅集群内 mock（`deployment smoke ok`），无费用。用 mock 的 stall 控制让一个 run 停留在 RUNNING。

## 探索过程

| 事故（注入方式） | 实际情况 | Administration 显示 | 运维兜底 |
|---|---|---|---|
| I1 容量：命名空间 `ResourceQuota pods=<当前数>`，随后在 Payments 提交两个、Research 提交一个任务 | 3 个 run 处于 SCHEDULED，Job 已创建，pod 因 `exceeded quota` 被拒 | Health 显示 **Ready**（绿色）；Task runs 为 `SCHEDULED 3`，无时长；Spaces 列表不显示状态；Space 详情只有"本周期 run 3/1000" | `kubectl get jobs`（0/1）与 `FailedCreate` 事件指明原因；删除 quota 后三个 run 在提交约五分钟后自动恢复，无需 BuildMax 侧动作 |
| I2 worker 失联：mock stall，run 进入 RUNNING 后对 worker 进程 `kill -STOP` | 2 分 21 秒后被回收器置为失败："this run lost its worker: nothing was heard from it for 2m0s" | `RUNNING 1` 变为 `FAILED` +1；没有任何地方指出 Space、原因，或回收器的动作 | 一条 WARN 日志 `failed a task run whose worker stopped reporting`，只带 `task_run_id` |
| I3 已有失败（fixtures 与 `kind up` 演练） | 4 个 FAILED：3 个在 smoke 账号的**个人** Space（worker 被关闭；两次对象存储 `PutObject` 失败）——平台故障；1 个在 BuildMax QA（Secret 被禁用）——Space 配置问题 | `FAILED 4`，无法区分 | 原因和 Space 只能靠 SQL：`task_run` 关联 `task` 与 `space` |
| I4 排队：在三个 Space 同时提交 30 个任务 | PENDING 30 → 0 约 80 秒排空，约 0.4 run/s | 只有一个 `PENDING` 数字；单次查看无法区分"正在排空"与"卡住"，多次刷新对比才能看出 | 不需要 |

泄漏检查：`ops@drill.local` 读取 `/api/admin/{system,config,spaces,spaces/{id},audit-events,llm/calls,users}`，没有出现任何标记。Space 内容路由（`/agents`、`/schedules`、任务详情）返回 `space not found`。内容边界成立。

## 发现

**F1 — 运维无法从 Administration 察觉工作停滞。**
可用性障碍 / 缺少运维元数据；对事故处置影响大，每个相关事故各复现一次。I1 中 Overview 显示 Ready，而任何 worker 都无法启动；`SCHEDULED 3` 不带时长，健康部署在任意时刻也可能是这个数字。I4 中静态的 `PENDING 30` 在排空和卡住时看起来一样。答不上来的问题是"工作在推进吗？从什么时候起不推进了？"。缺失字段：最老的 PENDING 与最老的 SCHEDULED run 的等待时长，以及 `last_seen_at` 已过期的 RUNNING run。它们都可以从 `task_run` 的持久列（`created_at`、`k8s_job_created_at`、`started_at`、`last_seen_at`）推导，因此在两个副本下同样真实。**不需要任何 Space 内容。** 这正是系统管理设计 §17 问题 16 的聚合项，现在有了观察到的缺口作为证据。

**F2 — 失败无法区分平台故障与 Space 问题。**
可用性障碍；影响大，因为它决定运维是否需要介入。I3 中三个平台失败和一个 Space 配置错误都藏在同一个 `FAILED 4` 后面。run 的 `error_message` 能区分二者，但不能原样展示：其中含内部服务 URL 和对象存储会话路径。缺失字段：由让 run 失败的组件设置的**安全失败类别**（例如 worker 失联、被放弃、创建失败、存储、Space 配置、模型/提供方、Agent 错误），按类别和时间窗口计数。它需要新增一列，或在记录失败处推导出一个枚举；**不需要**内容。

**F3 — 不用 SQL 就无法指出受影响的 Space 或其所有者。**
可用性障碍；中等影响——I1 的恢复不需要它，但通知正确的人需要。Admin 的每个 Space 数据只有成员、档位和本周期 run 总数，没有任何视图按卡住或失败的 run 列出 Space。调度器、k8s runner 和回收器的日志带 `task_run_id`，不带 `space_id`，而 `task_run` 行只能经由 `task` 找到所属 Space。一旦知道 Space id，Admin 的 Space 详情会给出所有者，从而回答"谁能处理"。缺失字段：按 Space 统计的活跃、卡住和失败 run 数（Space id、名称、所有者本来就是 Admin 元数据）。不涉及内容；提案 §5 提醒 Space *名称*可能透露工作内容，但 Admin 已经在展示名称。

**F4 — 个人 Space 在 Administration 中不可见，其中的失败也一样。**
相对运维目标的确认缺口；中等影响。Spaces 列表写明"Personal spaces are omitted"；搜索 `My Space` 返回 0。而 I3 的四个失败中有三个——即平台故障——都在个人 Space 里，Overview 却把它们计入了。`GET /api/admin/spaces/{id}` 对个人 Space 确实会返回（`personal: true` 与用量），但前提是已经知道 id。任何 F3 视图都必须包含个人 Space，否则个人独自工作的部署会把事故藏起来。

**F5 — run 生命周期事故不留审计或运维可见的痕迹。**
产品问题；中低影响。回收器的决定（worker 失联、被放弃、取消宽限）和调度器失败（创建失败、令牌签发失败）只作为日志存在于执行动作的那个副本上，审计记录中一条都没有。它们应进入审计（动作记录）还是 F1 的元数据视图，是设计选择；演练表明运维至少需要其中一种。

**F6 — 被回收 run 的冻结 worker pod 继续存在（疑似资源泄漏）。**
疑似缺陷；影响低，只观察到一次。I2 中回收器将 run 置为失败后，其 pod 仍是 `Running`。Job 有 `ttlSecondsAfterFinished: 300`，但没有 `activeDeadlineSeconds`，回收器也不删除 Job。发生网络分区或进程冻结后，worker 会一直占用 pod，直到自行恢复。解冻后它记录 `run already claimed; this worker has nothing to do`，终态仍为 FAILED，因此没有状态正确性问题，只有容量问题。未验证：一个处于分区中但仍在运行的 worker 是否会继续消耗模型调用。

**观察 — 派发吞吐量是固定的。** I4 以约 0.4 run/s 排空，与每副本 `maxConcurrentDispatch = 1`、5 秒轮询一致。队列排空了，但部署的派发速率只随 Server 副本数增长。这使得 F1 中"最老 PENDING 时长"成为运维察觉派发饱和所需的信号。

## 提案决策证据

- 运维缺少的全部是**可从持久 run 状态推导的运维元数据**：停滞时长（F1）、安全失败类别（F2）、包括个人 Space 在内的按 Space 计数（F3、F4）。**没有任何事故需要读取 Agent、Workflow、Schedule 或 Issue，也不需要任何 Space 内容。** I1 与 I2 的恢复需要的是平台权限（kubectl），而非 Space 权限。
- 这支持提案中运维那一半采用方案 C，并为系统管理设计 §17 问题 16 提供了具体的切片。对于运维，它不支持方案 A 或 B——全局的 Agent、Workflow、Schedule 或 Issue 清单。
- 成员那一半（跨自己所属 Space 的合并视图）**未测试**，仍需要观察真实的多 Space 用户。

## 清理

`./make kind down` 删除了临时集群和 `.local/kind-ephemeral.env`。原始截图和日志位于被 git 忽略的 `.artifacts/explore-admin-drill/`，复现本报告的任何内容都不需要它们。演练未触碰任何共享集群（`buildmaxdev` 未动）。

## 后续

把 F1–F4 转化为[系统管理设计](../../design/系统管理.md)中一个已接受的运行时运维切片，用本证据解决其 §17 问题 16，再落为 backlog 任务。该任务需要提案 §8 列出的授权测试、响应泄漏测试和双副本 kind 检查。在该决策中一并处理 F5。F6 作为单独的小修复或 backlog 项：当回收器让 run 失败时删除 worker Job，或为其设置时限。上述工作落地后移除本报告。
