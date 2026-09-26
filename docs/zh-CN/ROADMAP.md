# BuildMax 路线图

> **英文原文：** [BuildMax Roadmap](../ROADMAP.md)
>
> **读者：** 用户、运维人员与贡献者 · **状态：** 当前有效 — Alpha
> **最近复核：** 2026-09-26
>
> 本文是英文原文的简体中文镜像；如有差异，以英文原文为准。

BuildMax 是面向本地工作与私有 Space 部署的开源 Agent runtime。
CLI/TUI、Desktop 和 Server/Portal 使用同一个 Go Agent Core。
使用本地工具不需要部署 Server。

**下一个里程碑是可靠的私有部署 Beta：** 运维人员能够依照文档完成部署、
执行工作、理解故障并恢复服务。BuildMax 尚未通过这一门槛。
本路线图没有承诺 Beta 发布日期；发布准备程度取决于证据，而非已实现功能的数量。

## 一览

| 阶段 | 用户能获得什么 | 当前情况 |
|---|---|---|
| Alpha 已可用 | 在本地或私有 Space 中运行 Agent，使用托管模型、后台与定时工作、共享结果与诊断轨迹。 | 各项能力的限制不同，参见[当前状态评估](current-state.md)与[用户手册](../../manual/introduction.md)。 |
| 下一步：私有部署 Beta | 能够信任 worker 边界、受支持的 Server 拓扑、持久化与恢复流程。 | worker 边界契约（R0）与持久状态正确性（R1）已关闭并有证据，R1 含一次部署级跨副本协调演练；长期运行恢复（R2）与一个不可变部署的候选运行证据（R3）仍待补齐。 |
| 后续：依据证据扩展 | 更丰富的 Workflow、集成与本地体验，解决已得到验证的用户问题。 | 候选方向，不是发布承诺。 |

路线图负责优先级、顺序与发布门槛。实现证据放在[当前状态](current-state.md)，
设计理由放在[设计记录](design/设计文档索引.md)，发布验证放在
[Beta 就绪记录](deploy/beta-readiness.md)。已通过的工作拆分成的可独立执行单元
放在 [backlog](../backlog/README.md)。“已实现”不等于通过真实部署验证。
旧设计中的 P0–P4 是历史能力分组；当前工作以以下 R0–R5 顺序为准。

## 当前优先顺序

R0 已关闭受支持的 worker 契约，R1 已关闭持久状态正确性。R2 关闭剩余的发布阻塞型
工程缺口：约束长期运行与恢复闭环。R3 随后依据文档中的运维流程验证一个不可变候选版本。
R4–R5 是 Beta 之后、由证据驱动的工作，不是藏在发布路径中的前置条件。
这些是优先级，不代表每项都已有负责人正在开发。

下面每个优先项都以机器可读的 `**Status:**` 行开头——取值为 `open`、
`in-progress`、`candidate-proof-remains` 或 `done`——供 `./make board` 报告，
并由架构测试强制。它只是一词定位；随后的正文仍承载真正的细节与 `完成标准`。

### R0. 关闭受支持的 worker 契约

**Status:** done

**已完成。** 官方 worker 镜像选择并探测 worker 沙箱基线；Bash 隔离、进程限制、
hook 传输策略、worker API 隔离、轨迹边界展示和已解析 Plugin 展示均已实现。受支持
的无人值守 worker 配置失败关闭地禁用 stdio MCP：解析到的 stdio server 在装配期、
在命令运行之前让运行失败，该处理在 TaskRun 诊断信息中于运行边界旁清晰可见。每项
控制都在[信任保障](design/信任保障.md) §6.1 映射到其证据：Bash 隔离与 worker API
隔离经真实部署 worker 路径证明（部署冒烟的 `assertWorkerSandboxConfines` 与 kind
的 worker-API 边界检查），stdio MCP、进程限制与 hook 控制则在各自执行的层面、于同
一已证明为真实的 worker 配置上证明。

**证据已记录：** 没有 stdio MCP 子进程在受支持 worker 配置所声明的边界之外运行；
必要的强制机制不可用时失败关闭；实际边界与 MCP 处理在 TaskRun 诊断信息中清晰可见；
部署证据以信任保障 §6.1 记录的层级覆盖这些声明。Pod 级目的地控制与外层 runtime
沙箱仍是首个 Beta 明确接受的限制，而非 R0 门槛。经由运维旅程的完整不可变候选资格
认证属于 R3 与 [Beta 就绪记录](deploy/beta-readiness.md)，二者仍未关闭；R0 关闭
worker 契约并不代表已通过它们。

设计：[信任保障](design/信任保障.md)、
[worker API 网络边界](design/Worker API网络边界.md)与
[沙箱边界](design/沙箱边界.md)。

### R1. 关闭持久状态正确性缺口

**Status:** done

**已完成。** Redis 模式提供共享流、连接事件和 Conversation 回合租约，参考清单运行
两个协调后的 Server 副本。Workflow run 与 step-run 转换使用带保护的 compare-and-set
写入，失败步骤的收口也已原子化。消息历史写入已执行租约 fencing，陈旧写入者会被拒绝
而非损坏会话。任务不再因其生成标题所花的 token 而被拒绝，因此在 run 额度之内的 space
不会再因 token 拒绝而留下孤立 Conversation。线性 Workflow 前身已实现持久化协调：一个
由 Server 拥有的恢复循环从持久化状态中扫描到期的 run，因此 callback 丢失或重启不再让
其搁浅，并已用真实 MySQL 验证。

**已记录证据：** 完成标准所要求的部署级候选演练现已存在。由 `./make kind smoke` 对着
两副本 + Redis 的 kind 栈运行的 [`kindCoordinationProbe`](../../tools/mk/coordination_probe.go)
手动驱动两个副本，证明完成标准所指的投递、串行化与恢复：一个任务的 worker 输出能到达
在任一副本上打开的流；一个在某副本上持有会话租约的回合，会让在另一副本上发起的回合
在它释放前无法到达模型；以及在 Redis Pod 重启后两者都能恢复。旧持有者不能提交写入
（fencing），被拒绝的工作不留下孤立记录（标题 token 修复），持久工作在中断后收敛且不会
重复执行（由 Server 拥有的、用真实 MySQL 验证的协调循环）。把失租信号传递给正在运行的
回合以及 Server 滚动更新演练仍属未决，但它们是 R3 部署资格验证的问题，而非持久状态
正确性缺口；R1 关闭并不断言完整的不可变候选资格认证，那仍归属 R3 与
[Beta 就绪记录](deploy/beta-readiness.md)。

设计：[服务器协调](design/服务器协调.md)与
[Workflow runtime](design/Workflow运行时.md)。

### R2. 约束长期运行并完成恢复闭环

**Status:** in-progress

**测试基础设施已实现；生命周期证据待补齐。** MySQL scope 已在 pull request 中运行，
覆盖关键授权、TaskRun 状态、检查点、Artifact 与 Workflow 转换行为。部署冒烟覆盖正常
执行、取消，现在还覆盖 worker 丢失恢复：一次 worker 在执行到一半被删除的 Run，会落到
一个可诊断的终态 FAILED，并且仍可取回。该演练走的是一次滚动更新、驱逐或节点排空所采用
的优雅终止路径；由存活性巡检了结的静默硬丢失路径无法从部署侧复现，改由存储层测试覆盖。
部署冒烟还覆盖数据库中断的降级与恢复：运行期丢失 MySQL 会让 `/readyz` 报告数据库故障、把
服务端移出 Service 而不重启它，且访问一旦恢复它会自行恢复；对象存储同理：运行期丢失桶会让
`/readyz` 的对象存储检查失败，并在桶完好的情况下恢复。它还会在服务端路径保持健康的同时
只拒绝 worker 的对象存储写入：种子或运行状态被存储拒收的 Run 会以 FAILED 结束，原因点明
被拒绝的写入，保留其回复以及经服务端发布的 Artifact，并且不记录指向不存在对象的 trace
指针。服务端现在会按运维设定的窗口过期持久化的运行轨迹，默认永久保留并记录每次清理。
不支持二进制回滚：数据库被更新版本迁移后，二进制会拒绝启动，恢复方式是配对恢复。每个拉取
请求的 MySQL 作业都会升级真实前序版本的模式与数据，即声明的升级来源的 server 镜像写下的
转储。凭据轮换已有[操作手册](../deploy/credential-rotation.md)和 kind 演练
`./make kind drill rotation`：JWT 密钥、数据库密码、对象存储密钥、托管模型密钥与 KEK
分别通过修改 Secret 并滚动发布完成轮换，旧值均被拒绝，会话通过刷新恢复，已存储数据保持
完好，唯一实测中断是跨越 JWT 轮换的执行中 Run，被结算为 FAILED。配对恢复已有
[操作手册](deploy/backup-restore.md)和 kind 演练 `./make kind drill restore`：静默后先
`mysqldump --single-transaction`、再 `mc mirror` 备份，清空数据库、存储桶与服务端命名空间，
用原始 KEK 恢复，`storage verify --checksums` 无任何发现，API 指纹、行数与对象摘要均未改变，
并实测恢复时间。kind 演练不算候选版本证据。尚无候选版本验证过数据库与存储桶配对恢复、
发布时的 Compose 升级演练或凭据轮换。

**下一步：** 剩余的生命周期证据——在候选版本自己的依赖上完成数据库与存储桶配对恢复、从前序版本二进制
出发的 Compose 升级演练，以及候选版本上的凭据轮换——其中数项会作为 R3 的运维旅程落地。
[配额窗口](https://github.com/icloudbb/buildmax/issues/498)与跨 Space 存储层作用域的真实
MySQL 覆盖，以及部署级的 worker 丢失演练、数据库中断演练、对象存储就绪状态中断/恢复演练与
worker 对象存储写入拒绝演练，均已完成。删除针对已移除机制的计划，包括旧结果
投递队列；不要为了检查表重新引入机制。

**完成标准：** 长期运行部署的轨迹存储有边界，或具有明确的容量规划；关键持久化路径
具备真实数据库回归测试；候选版本具有故障、恢复、升级、回滚与轮换的持久证据。

设计：[验证计划](../design/verification-program.md)与
[端到端测试](../design/end-to-end-testing.md)。

### R3. 验证一个私有部署候选版本

**Status:** candidate-proof-remains

**产品路径已实现；证据记录仍为空。** 账号引导、登录码恢复、Space 成员管理、
托管模型、Agent 与 Workflow 运行、Artifact、轨迹、用量、审计、Compose/kind 和生产
参考均已存在。这些都不能替代使用外部依赖，对拟发布的不可变 Server、worker 与 Portal
制品进行验证。

**下一步：** 固定候选镜像摘要，让未参与功能实现的运维人员完成文档中的账号、Space、
执行、诊断、故障、恢复、升级、回滚与轮换流程。只修复流程实际暴露的缺口。
事务性权限审计、管理 CLI 的 Session 能力对齐、配额层级分配和更丰富的运行元数据，
除非阻塞这一结果，否则仍是[系统管理](design/系统管理.md)记录中的开放问题。

**完成标准：** Beta 就绪记录的每个必要条目都有持久证据，失败与接受的限制清晰可见，
资格验证运维人员、工程负责人和发布负责人共同签署结论。

设计：[Space 成员生命周期](design/Space成员生命周期.md)与
[Space 治理](../design/space-governance.md)。

### R4. 衡量 Beta 门槛之外的产品质量

**Status:** in-progress

**Beta 之后；框架已实现但覆盖有限。** 三个 BuildMax 自有任务与一次外部单任务
canary 只能证明评估链路成立，不能证明平台整体可靠，也不构成 Terminal-Bench 分数。
公共基准覆盖面不是验证私有部署契约的前置条件。

**下一步：** 根据观察到的失败扩充产品自有的本地、worker、Conversation、信任边界
与部署场景，单独采集性能与长时间运行证据。先运行固定版本的 Harbor canary，再运行
完整基准协议；只有完成协议并披露条件后，才能发布分数。

**完成标准：** 产品改动可以在代表性、可复现的场景中比较，并明确报告不确定性、
失败与限制。

设计：[评估系统](../design/evaluation-system.md)。

### R5. 依据证据深化产品能力

**Status:** open

**后续方向；范围取决于需求与验证结果。** 持久 Workflow 状态协调、图执行和
类型化数据绑定已实现。候选工作包括条件路由、更多聊天平台适配器、可执行 Space 插件、
Portal 性能、Desktop 应用内定时任务之外的自动化和吞吐量。第一个真实渠道适配器已交付：按照
[即时通讯渠道](design/即时通讯渠道.md)设计，Telegram 私聊可以进入 Space Conversation；
该设计的后续阶段（飞书与群聊、流式回复、Remote Control 推送、更多平台）仍由需求驱动。
解决具体问题的 CLI/TUI 与 Desktop 改进仍然受欢迎；Beta 的重点不意味着 Portal 是唯一产品。

[Agent 浏览器能力](design/Agent 浏览器能力.md)作为一项本地能力已被接受并正在
实施：一个 Go 自持的 Chromium over CDP，让 Agent 针对真实渲染页面验证变更，
已在 CLI 上以 headless 交付、在 Desktop 上以可见窗口交付，并可在 Desktop 工作区
tab 中只读实时查看页面；worker 在出口沙箱问题解决前保持关闭。交互式接管与托管浏览器
下载仍待完成。它的证据门是可移植的跨平台交付与该记录中的信任边界，而非需求——
同类产品已发货此工作流。

[Remote Control](design/远程控制.md) 已交付前四个阶段：以 `--remote-control` 启用的
本地 CLI/TUI Session 可以在另一台设备上通过 Portal 观察、发送提示、审批并停止，
而执行始终留在本机。带逐设备信任的推送通知、Desktop 与 print 模式启用，以及
[流可观测性](../backlog/92-remote-control-stream-observability.md)仍待完成。

条件触发的安全加固也属于这里，而不是 Beta 门槛：只有部署证据或更强的威胁模型提出
要求时，才选择 Pod 级目的地策略、专用出口代理或 gVisor 这类外层 runtime。
当支持不互信多租户、不可信仓库，或 worker 持有高价值凭证时重新开启这项工作；
没有这些证据，不把特定 CNI 或代理变成 BuildMax 的无条件依赖。

企业 SSO 不仅已在[企业身份与访问](design/企业身份与访问.md)设计记录中确定方向，
前两个阶段也已经实现：持久可撤销 Session、Portal HttpOnly refresh cookie、OIDC 授权码登录、
外部身份关联、受限 JIT 创建账号，以及独立配置的原生登录姿态。Phase 3 仍是 R5 资格验证切片，
需要可复现的真实 Okta 租户，以及该记录列出的 offboarding、轮换、故障与 break-glass 输入。
原生 CLI/Desktop OIDC 和设备授权仍不在已交付的浏览器流程中。

本地 Issue 工作桥接已按其有限范围决定并交付：CLI 的 `buildmax issue` 命令，以及登录时
Desktop 的 Issues 视图——接收自己负责的工作、从中开始本地聊天，并交回评论和状态变更。
持久的 Issue↔Session 关联、工作区映射、本地结果投影、拆解与治理已决定暂不做，直到使用中
出现需要；见[界面定位](design/界面定位.md#55-本地-issue-工作)。

Beta 门槛通过后，按以下顺序评估并交付此前尚未排期的插件与凭证后续项。每一步仍需
满足其所述证据；在此获得一个有序位置，并不意味着可以跳过提案的接受决策。

1. 在扩展通常需要凭证文件的插件之前，先加入 [Space Secret](design/Space密钥.md)
   的凭证文件交付。
2. 只有在 R0 具备受支持的 hook/MCP 进程与网络边界后，才加入
   [可执行 Space 插件内容](design/Space插件分发.md)；保留版本资格、精确 pin 和
   Run 范围物化机制。
3. 只有在固定的插件环境和可执行插件分发得到验证后，才加入 Task 范围的插件自主获取。
   它创建后续 TaskRun，绝不热加载正在运行的进程。
4. 按短期凭证交换、外部 Secret 提供方、workload identity 的顺序考虑后续能力，
   并且只为具体的提供方和运维流程实施。

共享 runtime 中与提供商无关的结构化输出契约、提供商映射、TaskRun 持久化、
Workflow `output_schema`、图执行和 JSON Pointer 绑定已经实现。类型化条件路由、
规划器、评估器与 Portal Schema 编辑器仍待完成；渠道名称或部分适配代码不能算作
已经交付的集成。

设计：[Workflow runtime](../design/workflow-runtime.md)与
[编排和连续性决策](../design/orchestration-and-continuity-decisions.md)。
以上有序后续项的具体契约以各自链接的记录为准，不在此重复。

## Beta 门槛

首个 Beta 面向**私有网络中的一个可信 Space**，不代表已具备公共多租户服务能力。
验证必须使用与拟发布版本完全相同的不可变 Server、worker 和 Portal 制品。

| 必要证据 | 验收结果 |
|---|---|
| 候选版本部署 | 固定镜像摘要，使用外部 MySQL、S3 与 TLS 部署；记录版本、配置、运维人员与日期。 |
| 执行边界与拓扑 | 验证受支持的沙箱、资源限制、hook/MCP 处理和 Server 拓扑。仅记录 `none` 边界的无限制 Bash 不合格；除非 stdio MCP 子进程受声明的 worker 边界约束，否则必须禁用 stdio MCP。明确记录剩余 Pod 级出站网络与存储凭证限制。 |
| 持久化与故障行为 | 附上通过的关键 MySQL 测试；演练取消、worker 丢失、数据库中断与存储拒绝访问。Run 达到文档规定的终态，并保留可获得的结果与诊断证据。 |
| 恢复与维护 | 配对恢复数据库与存储桶；演练模式升级、前一版本二进制对已升级数据库的拒绝，以及凭证轮换。记录恢复时间、数据检查与接受的损失。 |
| 运维流程 | 未参与实现的运维人员能够登录、使用托管模型执行和重试工作，并通过 TaskRun、Artifact、轨迹、用量与审计历史诊断结果。 |
| 发布验证 | 附上当前 CI、直接与托管模式 Compose/kind 冒烟、Portal 浏览器 E2E、归档验证、镜像扫描、SBOM 与来源证明。 |

工程上先关闭受支持的 worker 契约，再处理剩余的状态正确性与长期运行恢复缺口。
随后验证外部环境中的候选版本，最后签署就绪记录。更广泛的模型评估与公共基准
不阻塞这一决策。

[Beta 就绪记录](deploy/beta-readiness.md)保存详细步骤与证据。
单元测试或本地冒烟通过不能替代候选版本的恢复、故障与升级演练。
Desktop 打磨、SSO、可执行 Space 插件内容、更多模型提供商及通用持久 Session
同步不属于首个 Beta 门槛。

## 如何参与

先阅读[贡献指南](../../CONTRIBUTING.md)与[测试指南](../contribute/testing.md)。
不必承担一整个优先项，也可以做出有用的贡献。

| 你想做什么 | 有用的贡献 |
|---|---|
| 第一次贡献 | 跟随本地安装或运维流程，改进不清楚的文档；浏览 [good first issue](https://github.com/icloudbb/buildmax/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22)。 |
| 提高可靠性 | 复现故障并添加聚焦的回归测试，尤其是 R1–R2 的状态与恢复路径。 |
| 帮助验证私有部署 | 执行文档中的部署流程，报告版本、拓扑、预期与实际行为及脱敏证据。 |
| 参与功能方向讨论 | 在 [Discussions](https://github.com/icloudbb/buildmax/discussions) 描述用户问题、具体例子，以及现有行为为何不足。 |

提交缺陷或实现建议前，先搜索[已有 issue](https://github.com/icloudbb/buildmax/issues)。
较大改动应关联对应 R 优先项与设计记录，并在实现前讨论范围。
路线图条目不表示已有负责人或对应的实现 issue。

维护者应在优先级、完成标准或发布门槛变化时更新本页，并同步中文镜像。
日常实现细节放在关联证据与 issue 中，避免本页再次变成实现清单。
