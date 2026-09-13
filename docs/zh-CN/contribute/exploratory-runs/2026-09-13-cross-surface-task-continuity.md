# Desktop、Portal、server 与 worker 连续性 — 2026-09-13

> **翻译说明：** 本文是[英文原文](../../../contribute/exploratory-runs/2026-09-13-cross-surface-task-continuity.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。

**章程。** 旅程：（1）已配置的 Desktop 用户创建一个短小本地任务，批准写入，离开并重新启动后恢复会话；（2）Portal 用户发送一轮会话消息，离开后重开，再创建一个 Agent 支撑的 Task，并以第二个 TaskRun 继续；（3）已部署的 server 与 worker 边界通过真实 MySQL、对象存储、Kubernetes Job、沙箱和内部 worker API 承载这些动作。理由：这是贯穿全部指定已交付界面的最短连接旅程，不虚构覆盖。角色：已配置的本地 Desktop 用户与普通 Portal 用户。允许的变更：一个一次性本地项目、一个测试专用 Portal Agent/Task、一个任务独占的临时 kind 集群及被 git 忽略的证据。主动探索约 75 分钟；最终浏览器套件尝试前有一段较长空闲时间。

**环境。** 独立 worktree `buildmax-exploratory-surfaces-20260913`、分支 `codex/exploratory-surfaces-20260913`，起始提交 `f894e0fc` 且干净。macOS 26.6.2 arm64；Go 1.26.6；Node 24.19.0；Docker 29.7.2；kind 0.31.0；kubectl 1.35.1。Desktop 经构建后通过 Wails 开发桥运行。Portal/server/worker 运行于本任务独占的 kind 集群 `buildmax-eph-2fd7a1ed`（`http://localhost:51415`），包含两个 server 副本、真实 MySQL 和 MinIO，以及每轮一个 worker Job。Portal 起始数据为 deployment-smoke 账号的个人 Space；新 Agent 与 Task 使用本会话唯一名称。

**模型。** Desktop 用已配置、经 OpenRouter 的真实 `openai/gpt-5.6-luna` 做了一次小型试验：8,666 输入 / 84 输出 token，4,165 cache-read / 4,254 cache-write token，界面显示费用 0.001297 USD、缓存节省 0.000537 USD。Portal Task 与会话使用仓库内免费的集群内 `BuildMax smoke` 模型；检查的 run 报告 3 输入 / 3 输出 token，且按集群 direct mock 模式预期没有 managed gateway 计费。提示词与文件只含测试短语。

**探索过程**（动作 → 观察 → 下一个问题）：

1. 构建 Desktop，打开一次性项目 `Desktop Explore Worktree`，提交“Create desktop-worktree.txt containing exactly DESKTOP-WORKTREE-OK…” → Write 审批展示路径和内容；`Allow once` 生成完全一致的文件和 `DESKTOP-DONE`。Session `da4bc3e4-23d7-4c19-9a10-fe7a02723d02`，run `3swnft26yvqzoaidrc7a`。
2. 离开到 New Chat，重开会话，停止并重启 Desktop 浏览器驱动，再从 Recent chats 重开 → 用户轮次、Write 结果、助手回复、模型、token/缓存总量、费用与 run 摘要均仍可用。
3. 以普通 deployment-smoke 账号登录临时 Portal，从首页输入框发送“How is this deployment doing?” → 真实 WebSocket 会话返回 `deployment smoke ok`。离开到另一个 Space 页面，回到 Chat，重开最近会话，再刷新其直接 URL → 双方消息都保留。
4. 创建 Agent `Exploratory continuity 20260913`，以初始输入 `Verify direct Task continuity.` 运行，并从 Agent 的 Runs 表及直接 URL 重开 Task `3zozxlh4dydi4ayomcmq` → Task 经 worker Job `ocef3i24db3bxh722tya` 完成，并在刷新后保留。
5. 以 `Second direct turn for continuity.` 继续同一 Task → 新 TaskRun `gazixnq742fyden2h2da` 成功；会话展示两轮用户输入与两轮结果；Task details 展示两个 run；runs API 用 `previous_task_run_id` 将第二轮关联到第一轮。
6. 检查 Task 与 Run details → 可见沙箱决策（`bwrap`、`auto_allow`）、worker 输入、模型、token、耗时、workspace restore/checkpoint、MCP 状态与 managed-call 模式。与 runs API 对照后发现问题 1。
7. 两次执行 deployment smoke → 两次的 Portal、鉴权、Space 授权、存储、scheduler、worker 执行、artifact、retry 与 cancellation 均通过。第二次边界探针通过带标签 worker 的允许断言与无标签 pod 的拒绝断言，随后暴露问题 2。
8. 从宿主机和一个无标签集群内 pod 请求公开的 worker 形状路由 → 两处实际都收到 HTTP 404。server/worker 日志还显示两次探索 TaskRun 只到达内部 worker listener，完成 workspace restore/checkpoint、结果流式回传并成功终止。

**发现。**

- **继续后的 TaskRun 详情展示 Task 的首轮输入** —— Portal 诊断，中等影响，高置信。复现：以输入 A 创建直接 Task，以输入 B Continue，再为最新 run 打开 `Details` → `View trace`。Run id `gazixnq742fyden2h2da` 显示 `Sent to the worker Verify direct Task continuity.`（A），而 `GET /api/spaces/{space}/tasks/3zozxlh4dydi4ayomcmq/runs` 对同一 run 返回输入 B，并正确关联首个 run。期望（依据：TaskRun 拥有一次轮次/尝试，面板标识特定 run）：面板展示 B。错误输入会把诊断或审计带向错误轮次；实际执行正确使用了 B。
- **kind worker 路由探针拒绝正确的 404** —— 验证工具，中等影响，高置信。`./make kind smoke` 先报告全部部署断言通过，随后以 `BM_NOT404` 失败。公开路由从宿主机和无标签 pod 请求都实际返回 404。BusyBox `wget` 输出 `wget: server returned error: HTTP/1.1 404 Not Found`，但 `kindExpectPublicWorkerRoute404` 只搜索子串 ` 404 `，该输出不含此子串。期望：真实 404 令断言通过。结果：边界正确时 `kind up`/`kind smoke` 仍可非零退出，掩盖其他部署检查是否健康。
- **普通 Portal 登录产生可避免的 403 控制台错误** —— Portal 诊断，低影响，高置信。Ingress 证据把浏览器错误关联到 `/api/admin/me`（普通用户为 403）；首次 Space 初始化还请求 `/api/spaces/null/conversations?limit=100` 并收到 403。期望：普通用户成功登录时既不发送 null Space id 请求，也不把预期授权结果暴露为控制台错误。未观察到可见流程损失。
- **Desktop 工具卡片在 1280×720 下过度压缩内容** —— Desktop 展示，低影响，高置信。成功的 Write 卡片把 `Write` 从 `t` 与 `e` 之间断行，并把参数截成 `DESKTOP-WORKTRE…`；`DESKTOP-DONE` 也在连字符处换行。期望：一次短工具调用的记录在普通视口下仍容易扫读。完整详情仍可从别处获得，因此属于展示而非数据丢失。

**正向观察。** Desktop 的审批、精确文件结果、会话重开、重启连续性，以及费用/缓存详情均一致。Portal 会话历史和直接 Task URL 在导航与刷新后保留。同一 Task 接受第二轮输入并生成独立且关联的 TaskRun。无需访问集群，Task details 即可清楚展示沙箱、workspace checkpoint、模型、token 与 worker 执行。尽管自动探针解析错误，公开与 worker listener 的实际隔离仍成立。

**脚本化检查。** `./make e2e desktop` 通过；`./make e2e desktop-ui` 7/7 通过；按文档先重建后，`./make e2e desktop-launch` 通过。`./make test ./internal/server/... ./internal/infra/k8s/...` 通过。第一次组合窄测把已存在包误写成 `internal/infra/kubernetes`；server 包当次仍通过，改正后的完整命令通过。最终 `./make e2e kind` 未进入 Playwright，见下方环境限制。

**未覆盖 / 受阻。** macOS 处于锁屏状态，因此无法驱动 Desktop 原生窗口、原生文件选择器、原生焦点/命中测试与打包窗口精确布局。项目选择使用真实 Wails `OpenProject` binding，此后所有产品动作使用渲染控件；打包启动 smoke 只证明应用保持运行。Portal admin、Issues、Workflows、Schedules、插件、secrets、取消控件及响应式/移动布局不在本次有界章程内。手工 kind 旅程成功后，宿主 Docker VM 被三个既有集群及本任务集群挤满（Docker 内存 8 GiB、宿主负载 11.66、各 kind 节点约 80–157% CPU）。任务集群的 CoreDNS 健康检查超时，controller-manager/scheduler 重启，两个 server 副本转为 NotReady，server 对 MySQL 的 DNS 查询失败。因此最终两次 `./make e2e kind` 在预检/账号准备阶段终止。这些尝试属于环境受阻，不能算通过证据，也未被归类为 BuildMax 产品缺陷。

**清理。** 只删除本任务所属的 kind 集群 `buildmax-eph-2fd7a1ed`；未触碰 `buildmaxdev`、`luminadev` 或另一位贡献者的临时集群。停止 Desktop/Portal 驱动以及本 worktree 遗留的 Vite/esbuild 进程。将一次性 Desktop 项目和复制了真实模型配置的 sandbox 移入废纸篓，仍可恢复；报告不保留登录码或 token。被 git 忽略的截图、脱敏 Desktop 元数据、trace 与输出保留在 `.artifacts/exploratory-20260913-{desktop,portal-server}/`，供本地分诊。

**后续。** 分别分诊四项发现。其中错误 TaskRun 输入与 kind 探针误报优先级更高；本次探索本身不授权修复，也不声称界面已全面就绪。
