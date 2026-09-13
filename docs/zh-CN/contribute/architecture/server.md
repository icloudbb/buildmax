# Server

> **翻译说明：** 本文是[英文原文](../../../contribute/architecture/server.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
> **受众：** 贡献者 · **状态：** 当前有效
>
> 实时路由列表就是该 API 自身的 `GET /openapi.json`，可在 `/swagger/` 浏览。

## 用途

`internal/server` 为 Portal 和 worker 回调提供 Go HTTP 后端。它由 `cmd/buildmax-server` 通过 `internal/bootstrap/server.go` 启动。

该 Server 拥有路由注册、中间件、WebSocket 处理、worker API 回调，以及调度器的启动。具体业务流程都委托给 `internal/service/*`。

根 handler 在 `handlers.NewHandler` 中一次性构造出各个路由分组的 handler 及其应用服务。请求会复用这些实例；辅助方法不会在每次调用时重新组装一套新的服务图。

`server.New` 在两个监听器上构建两个 mux。`handlers.RegisterPublic` 把除 worker 包之外的每一条路由都挂到公共监听器上（`Config.Addr`）；`handlers.RegisterWorker` 只把 `/api/worker/*` 挂到 worker 监听器上（`Config.WorkerAddr`，默认为 `127.0.0.1:5679`）。worker 路由不存在于公共 mux 中，因此无论出示什么令牌，公共套接字对这些路由一律返回 `404`。`handlers.Register` 会把两者组合到同一个 mux 上，供测试和单监听器的嵌入场景使用；Server 本身从不使用它。CORS 只包裹公共监听器；请求日志则同时包裹两者。关闭时，公共监听器会先于 worker 监听器关闭，这样在公共层面完成排空的同时，worker 仍能继续上报。`Config.WorkerAddr` 为空的情形（用于 handler 测试）会构建出 worker handler，但不会开启第二个套接字。设置了 `Config.WorkerTLS` 时，worker 监听器会以 HTTPS 提供服务（`ListenAndServeTLS`）；公共监听器则从不携带 TLS，因为它在 Ingress 处终止。worker 一侧会根据自己配置的信任关系构建一个 `workerclient` HTTP 客户端，并用它发起每一次回调，包括托管推理调用。参见 [design/worker-api-network-boundary.md](../../design/Worker API网络边界.md)。

## 关键领域

| 领域 | 包 / 文件 | 作用 |
|------|----------------|------|
| Server 封装 | `internal/server/server.go` | 构建 `http.Server`、中间件、静态 OpenAPI/Swagger 路由 |
| Handler | `internal/server/handlers` | Portal API、worker API、webhook、WebSocket handler |
| 调度器（Scheduler） | `internal/server/scheduler` | 认领 pending 的 TaskRun 并启动 worker；同时运行后台清扫——过期凭证、被遗弃的运行，以及审计保留 |
| Bootstrap | `internal/bootstrap/server.go` | 组装数据库、存储、LLM、配额、handler、调度器 |

## 主要路由分组

- 健康检查与 API 描述：`/healthz`、`/openapi.json`、`/swagger/`
- 鉴权：`/api/auth/otp`、`/api/auth/login`、`/api/auth/token/refresh`、`/api/auth/logout`
- 存活与就绪探针：`/healthz`、`/readyz`
- Space 与成员：`/api/spaces...`
- Agent：`/api/spaces/{space_id}/agents...`
- Issue：`/api/spaces/{space_id}/issues...`
- Workflow：`/api/spaces/{space_id}/workflows...`
- 文件：`/api/spaces/{space_id}/upload`、`/files...`
- Conversation：`/api/spaces/{space_id}/conversations...`
- Task：`POST /api/spaces/{space_id}/tasks` 直接根据一个带类型的 `agent_id`，创建一个归属 Space 的 Task 及其第一个 TaskRun——不会创建或查询任何 Conversation。`GET .../tasks/{task_id}` 与 `GET .../tasks/{task_id}/runs` 读取一个 Task 的线程；`POST .../tasks/{task_id}/runs` 就是 Continue，会在同一个 Task 上创建一个带新输入的 TaskRun。`POST .../tasks/{task_id}/cancel`——见下文“取消运行”——以及 `POST .../tasks/{task_id}/retry`——见“重试运行”——补齐了这一整套路由。`GET`/`POST .../agents/{agent_id}/tasks` 是嵌套在 Agent 下的便捷路由：背后是同一个 Task 服务、同一份资源。参见[Agent 执行与 Task 线程](../../design/Agent执行与Task线程.md)
- Artifact：`/api/artifacts/{artifact_id}` 及其 `/content`，配合 `/api/spaces/{space_id}/artifacts` 用于该 Space 的列表与上传，另有 `POST /api/artifacts` 供已登录但尚未选择 Space 的客户端使用——可选的 `?space_id=` 会被遵从，不带 Space 则默认为调用者的个人 Space，这正是 CLI 和 Desktop 发布内容的方式。按 ID 寻址的路由从记录本身而非路径中取得所属 Space，因此它们使用 `Guard.MemberOfResourceSpace`，对非成员一律以 `404` 作答——一个 Artifact ID 是一个标识符，不是凭证，用 `403` 回应会让这条路由变成一个可以探知哪些 ID 存在的探针。参见 [../../design/unified-artifacts.md](../../design/统一工件.md)
- 运行产出（兼容层）：`/api/spaces/{space_id}/task-runs/{task_run_id}/artifacts...`
- 运行轨迹：`/api/spaces/{space_id}/task-runs/{task_run_id}/trace`
- 托管模型调用：`/api/spaces/{space_id}/task-runs/{task_run_id}/llm-calls`——一次运行花费了多少、用的是哪个模型，不含提示词内容，也不含运维人员的目录路由信息。对该运行的授权即是对其账本的授权：这些行本身并不携带自己的 Space
- 托管网关（**不**按 Space 划分范围）：`/api/llm/models` 与 `/api/llm/completions`。目录中的每一个模型，对每一个已登录用户都可用，一次调用会归属到发起它的那个人身上。参见 [../../design/client-modes.md](../../design/客户端模式.md)
- 用量：`/api/usage`、`/api/spaces/{space_id}/usage`
- 审计记录（仅所有者）：`/api/spaces/{space_id}/audit-events`，以及以 CSV 或 JSONL 形式导出整份记录的 `/audit-events/export`。这次导出本身也会被记录，并且按 keyset 游标而非按偏移量分页，因此在流式导出期间被写入的表不会因此漏掉某条记录
- Webhook key（按用户划分范围，而非按 Space）：`/api/webhook-keys...`
- WebSocket：`/api/spaces/{space_id}/ws`
- Worker API（**仅内部监听器**，不在公共端口上）：`/api/worker/task-runs/{task_run_id}...`，其中包括 `/llm/completions`——让 worker 不需要持有提供方凭证——以及 `/artifacts`——让一次运行中的 Agent 能为该 Space 保留一个文件。worker 从不自行声明自己在为哪个 Space 写入：run token 指名运行，运行指名 Task，Task 指名 Space。每条路由还会强制执行该运行的生命周期（`requireRunning`）：除 `GET` 轮询外的一切操作，只要运行不处于 RUNNING 状态就会被拒绝，因此一个泄露但尚未过期的令牌，既不能在认领之前生效，也不能在运行终止之后生效。参见 docs/design/worker-api-network-boundary.md §8
- 入站 webhook：`/api/webhook`

## Conversation 回合

每个 Conversation 一次只运行一个回合。回合队列（`internal/server/turnqueue`）为每个 Conversation 各自维护一个队列，串行化 WebSocket 消息及 HTTP 的 `POST .../messages` 和 `POST .../conversations` 前台入口。TaskRun 完成广播持久状态失效通知，不排入摘要回合。它的作用域是整个 Server，而不是单个连接，因为一个 Conversation 可能同时被多个连接访问到。

在一个回合运行期间到达的消息会被排队，每个 Conversation 最多排队 10 条，之后各自作为独立的回合运行。WebSocket 客户端会看到 `conversation.message.queued`，等它开始运行时再看到 `conversation.message.dequeued`；`conversation.message.completed` 会携带 `queued_remaining`。超出上限的消息会被 `conversation.error` 拒绝，并携带 `code: "queue_full"`（HTTP：`429`），但这不会终止正在进行的那个回合。队列保存在内存中。参见[排队消息](../../design/排队消息.md)。

## 运行的来源

`GET /api/spaces/{space_id}/task-runs/{task_run_id}` 回答的是一次运行的来龙去脉：是谁或什么发起的、经由哪种触发方式、重复的是哪一次更早的尝试，以及它是在哪条 Conversation 消息中被请求的——这条消息会与 worker 拿到的指令并排引用。这两段文本是不同的——指令是 Tier 1 决定发送的内容——同时保留两者，是区分“模型遗漏的约束”与“用户从未给出的约束”的唯一办法。

它还会按修订版本，指出这次运行所依据的 Agent 定义。一个 Agent 的指令是在其 worker 请求这次运行时才被解析的，因此编辑一个 Agent 会改变它下一次运行的行为；记录下来的修订版本，说明的正是哪段文本产生了这个结果，而响应还会一并报告该定义的当前修订版本，好让读者看出这两者何时已经出现了分歧。

它之所以是一条独立于轨迹的路由，是因为它要挺过的是另一种“缺失”：一次在 Agent 启动之前就失败的运行，不会写下任何轨迹，但它依然是从某处发起的。一条无法读取、或属于另一个 Conversation 的消息，会被省略，而不会让整个请求失败。

## 报告已结束的运行

一次运行到达终态时，只会宣告一件事（`internal/server/handlers/task_result.go`）：该 Task 所属 Space 上的每一个 WebSocket 连接都会收到 `task.status.changed`——这是一次失效通知，而不是结果本身。客户端要回应它，做法是重新从 `task_run` 读取该 Task，这份数据本身就是权威的，读取它不需要任何单独的投递机制、模型调用，或 Conversation——一个直接的 Agent Task 根本没有 Conversation。此前的设计会把每一次已结束的运行都路由经过一个 Tier 1 回合，以及一个持久化的 `task_result_delivery` 重试队列，好让 Conversation 总能收到一句摘要；这条强制路径已经被移除。参见[Agent 执行与 Task 线程](../../design/Agent执行与Task线程.md)。

## 取消运行

`POST /api/spaces/{space_id}/tasks/{task_id}/cancel` 会停止该 Task 的运行。接下来发生什么，取决于是否已经有 worker 领取了它：

- **尚未派发**（`PENDING`）：一次数据库事务把运行状态改为 `CANCELED`，把这个终态投影到它所属的 Task 上，并返回 `200`。
- **已派发**（`SCHEDULED` 或 `RUNNING`）：这次请求会记录在该运行上（`cancel_requested_at`、`cancel_requested_by`），响应为 `202`。worker 轮询 `GET /api/worker/task-runs/{task_run_id}` 时会看到 `cancel_requested`，随即结束自己的 Agent 循环，上传这次运行已经产出的内容，并 PATCH 为 `CANCELED`。
- **已经结束**：`409`。在一次运行正在停止的过程中再取消一次并不是错误——第二次调用依然会得到 `202`。

Server 从不自己终结一次已经开始的运行：只有运行自身的进程才能停止它的 Agent 循环，从外部写入的状态只会去描述一个其实仍在执行中的运行。`StaleRunReaper` 是那种从不确认的 worker 的最后一道防线，会在一段宽限期之后把这类运行判为 `CANCELED`。

正是同一次轮询，让 Server 得知一个 worker 还活着。这条路由在每次调用时都会记录 `task_run.last_seen_at`，因此一个处于 `RUNNING` 状态、却沉默超过回收器存活宽限期的运行，会被判定为失去了自己的 worker——从这里看，SIGKILL、OOM kill 或者节点丢失，表现出来都是这个样子，它们都不会给 worker 留下上报的机会。`worker.run_timeout` 依然是这次清扫所看不到的情形的最后防线：一个从未到达 `RUNNING` 的运行，以及一个完全没有记录过任何信号的运行。这两种情况都不会被重新运行：一个被回收的运行，其 worker 可能已经造成了副作用，Server 无法得知重复执行这个 Task 是否安全。

一次被取消的运行会保留它的产出和 Artifact。它只是提早停止了，但它产出的内容是真实的工作成果，丢弃它只会让取消这个动作，比等待运行结束的代价更大。

## 重试运行

`POST /api/spaces/{space_id}/tasks/{task_id}/retry` 会把该 Task 最近一次的运行再执行一遍。它不需要请求体：新的运行会携带上一次运行的输入，并把它记录在 `retry_of_task_run_id` 中，`trigger_source` 为 `task_retry`。

输入取自这次运行本身，而不是取自 Task，是因为一个 Task 之后的运行可能携带后续追加的指令，重复某一次运行，意味着把那一次的内容再运行一遍。

有三种状态会得到 `409`，各自有各自的理由：

- 已经有一次运行在进行中——一个 Task 至多持有一个活跃的运行，如果某次运行耗时过长，正确的做法是先取消它
- 该 Task 从未完成过任何一次运行——没有什么可以重复的
- 该 Task 属于某个 Workflow 步骤——Workflow 会根据这一步的结果来推进或判定失败，因此在 Workflow 之外发起的一次重试，会为一个已经落定的步骤报告出第二个结果

`retry` 创建运行的方式，与 `POST /tasks/{task_id}/runs` 完全相同，因此 Space 配额对它的适用方式也完全一致。

## 说明

- 面向用户的 Portal API，只要涉及工作归属，就都按 Space 划分范围。
- Worker API 使用的是调度器为该 TaskRun 铸造的 run token，而不是用户的 JWT 鉴权。这个令牌携带用户、Space、Task 和运行信息，每条路由都从这些声明中推导出自己的资源范围。它是这些路由唯一接受的凭证：旧有的共享 worker 令牌已经被移除——见 [design/worker-run-token.md](../../design/Worker运行令牌.md)。
- 登录会返回两份凭证。access token 是一个 Server 不会存储的签名 JWT；refresh token 则是一行 `user_refresh_token` 记录，这正是让一个 Session 可被撤销的原因。`internal/service/identity` 拥有这套流程，`internal/server/handlers/auth` 拥有它的路由，每一次轮换都停留在 access token 的 `sid` 声明所指名的那个 Session 之内。
- 调用者是谁、请求关于哪个 Space，以及是否可以放行，这些问题都由 `internal/server/access` 回答。它的 `Guard` 会自己写出拒绝响应，因此一条路由读起来就像一份关卡清单；它所依据的角色/动作判定，是 `internal/core/space/policy.go` 中的 `space.Allows`——这是 Space 服务与它共用的唯一实现。
- `POST /api/auth/login` 接受密码，或是一个由运维人员签发的一次性登录码。后者是账号认领与找回的路径，因为 BuildMax 没有邮件通道——见 [deploy/authentication.md](../../deploy/authentication.md)。
- 另见：[Store](store.md)、[Portal](portal.md)、[边界](packages.md)。
