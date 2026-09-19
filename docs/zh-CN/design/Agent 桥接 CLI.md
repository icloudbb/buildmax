# Agent 桥接 CLI：从 Agent 到 Server 的统一命令入口

> **翻译说明：** 本文是[英文原文](../../design/agent-bridge-cli.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。

## 目录

- [状态](#状态)
- [1. 决策](#1-决策)
- [2. 本设计弥补的缺口](#2-本设计弥补的缺口)
- [3. 为何本设计反转 Issue Agent 访问](#3-为何本设计反转-issue-agent-访问)
- [4. 一个入口，两种上下文](#4-一个入口两种上下文)
- [5. 现在权限范围与护栏的归属](#5-现在权限范围与护栏的归属)
- [6. 凭据处理](#6-凭据处理)
- [7. 命令入口](#7-命令入口)
- [8. Agent 永远不能做的事](#8-agent-永远不能做的事)
- [9. 本设计取代了什么](#9-本设计取代了什么)
- [10. 范围之外](#10-范围之外)
- [11. 实施阶段](#11-实施阶段)
- [12. 开放问题](#12-开放问题)

## 状态

- roadmap_priority：`unscheduled` —— 本记录决定的是 Agent 究竟如何触达 Server
  资源；它尚未进入[路线图](../ROADMAP.md)，将在排期时对应到 R5。
- status：`accepted direction, not started` —— 维护者已于 `2026-09-19` 接受：由单一的
  `buildmax` 命令入口成为 Agent 触达 Server 的方式，**完全取代**进程内的 Issue 工具，
  而不是与之并存。目前尚无代码据此落地。
- reverses：[issue-agent-access.md](./Issue Agent访问.md) —— 反转其机制（进程内的
  `GetIssue` / `ReportToIssue` 工具）。其产品边界（本文 §8）保持不变。
- follows：[worker-run-token.md](./Worker运行令牌.md)、
  [client-modes.md](./客户端模式.md)、
  [worker-api-network-boundary.md](./Worker API网络边界.md)
- relates：[tool-permissions.md](./工具权限.md)、
  [sandbox-boundaries.md](./沙箱边界.md)、
  [unified-artifacts.md](./统一工件.md)、
  [客户端会话与 API 凭据（提案）](../proposals/client-sessions-and-api-credentials.md)
- folds：[本地 Issue 工作桥接（提案）](../proposals/local-issue-work-bridge.md)
  中的客户端命令部分
- touches：`internal/interface/cli`、`internal/interface/client`、
  `internal/interface/auth`、`internal/agentapp/taskrun`、`internal/bootstrap`、
  `internal/config`、`internal/server/handlers/work`、
  `internal/server/handlers/worker`、`internal/tool`
- created_at：`2026-09-19`

## 1. 决策

Agent 通过**一个命令入口**触达 BuildMax Server —— 也就是它本就在运行的 `buildmax`
二进制文件 —— 以普通子进程的形式经由 `Bash` 工具调用。不存在针对 Server 资源的、
按能力划分的进程内工具。读取 Issue、对其评论、发布 Artifact、触发另一个 Agent、
列出工作 —— 全部都是 `buildmax` 的子命令。

同一批子命令既服务于终端前的人，也服务于运行中的 Agent。在两者之间，命令名称与输出
都不会改变。底层改变的是**由哪个凭据来鉴权这次调用**，进而决定**调用方被允许访问哪些
路由和哪些资源** —— 这由运行上下文自动解析，绝不由模型选择。

这反转了 [issue-agent-access.md](./Issue Agent访问.md) 中此前的决策 ——
即给 Agent 两个专用的进程内工具。§3 追溯该记录的理由，并说明为何在当前条件下命令入口
能更好地满足它。

## 2. 本设计弥补的缺口

如今，Agent 只能通过编译进运行时的 Go 工具触达 Server：`GetIssue`、`ReportToIssue`、
`UploadArtifact` 以及托管推理。Agent 应能使用的每一项新 Server 能力 —— 触发一次运行、
列出分配给它的 Issue、启动一个 Workflow —— 都是一个新工具，带着各自的注册、权限条目和
提供商往返。这个入口每增加一项能力就多一个概念。

有两个事实让这变得代价高昂：

1. **非原生 Agent 根本没有任何工具。** Harbor 自定义 Agent、shell 脚本，或任何不是
   BuildMax 自身循环的执行器，都无法调用 Go 工具。它能运行命令。如果 Server 访问是一个
   工具，这些执行器就被拒之门外；如果它是一个命令，它们就是一等公民。
2. **如今运行中的子进程没有回到 Server 的通路。** 运行令牌被读取一次后立即从环境中
   抹除（`internal/bootstrap/worker.go` 中的 `takeEnv`），而 `FilterWorkerEnv`
   （`internal/config/env_spec.go`）会从子进程中剥离非自有的 `BUILDMAX_*` 变量。
   Go 层面的能力保存在进程内；`Bash` 派生的命令能使用的东西并不存在。命令入口必须弥合
   这个缺口，而弥合它正是让该入口保持统一的原因。

一个命令入口消除了“每项能力一个工具”的膨胀，并接纳每一个能运行程序的执行器。

## 3. 为何本设计反转 Issue Agent 访问

[issue-agent-access.md](./Issue Agent访问.md) 出于三个刻意的理由选择了两个进程内工具。
每一个理由今天依然重要；但没有一个需要用工具来实现。

**范围由构造保证。** `GetIssue` / `ReportToIssue` 不接受 Issue 标识符 —— 范围是
构造函数参数，因此模型无法寻址第二个 Issue。命令入口保留了这一点，但把这一保证从工具
构造函数转移到了凭据上。在 Worker 运行中，运行令牌恰好指明一个 TaskRun 及其唯一的
Issue；无论传入什么参数，worker 路由都会拒绝任何其他 Issue（见
[worker-run-token.md](./Worker运行令牌.md)）。不带参数的 `buildmax issue view` 会
解析到*那个* Issue，因为它没有别的被允许寻址的 Issue。范围在权限真正所在之处得到强制
执行 —— 令牌和路由 —— 而不是在一个第二个客户端可以绕过的客户端侧构造函数里。

**护栏。** 评论预算（`issueReportBudget = 3`）和正文长度限制（2000 字符）如今位于
工具层，这意味着它们只对经过工具的调用方生效。迁移到命令入口迫使它们进入 Server 路由，
在那里它们对工具、CLI、Portal 以及任何未来的客户端一视同仁地生效。这正是
[AGENTS.md](../../../AGENTS.md) 中的单一权威实现规则：验证规则属于 Server，而不应在每个
客户端里复制。这次反转*改善*了护栏的位置。

**不可信输入始终是数据。** Issue 文本必须作为工具结果到达，绝不作为提示词层级，这样
评论就无法注入指令。命令输出本就是数据 —— 它落在 stdout 上，Agent 把它作为 `Bash`
结果读取，恰是工具所提供的那个性质。没有退化。

于是工具的三项保证都得到保留，其中两项还转移到了更强的位置。工具独有的两点 ——
无需 `PATH` 上存在二进制即可使用，以及在工具权限策略中作为具名条目出现 —— 分别在
§7 和 §11 中处理。

## 4. 一个入口，两种上下文

`buildmax` 二进制会检测自己的上下文，并在没有模型参与的情况下选择传输方式和凭据。

**本地上下文** —— 某个人的 `buildmax` 会话，或在此人机器上、于该会话内运行的 Agent。
调用使用已登录用户的凭据，来自既有的 auth broker（`internal/interface/auth`，
`TokenForServer`），触达 public listener，并以该用户身份被鉴权。这正是人类的
`buildmax issue` 如今的工作方式；调用同一命令的 Agent 继承同一条通路。可用范围是该用户
的全部权限（接受的风险见 §6）。

**Worker 上下文** —— 位于 Worker 运行内部的 Agent，此时运行令牌已从环境中抹除。
Worker 进程运行一个小型**桥接（bridge）**：一个位于运行内部的 Unix 域套接字，其路径以
`BUILDMAX_BRIDGE_SOCK` 导出给子进程。`buildmax` 二进制看到该变量后，会把它的 Server
调用发送到该套接字；桥接附加进程内的运行令牌，并代理到内部的 worker listener
（[worker-api-network-boundary.md](./Worker API网络边界.md)）。令牌从不进入子进程的
环境、参数或输出。可用范围恰好是那些 worker 路由：这次运行、它唯一的 Issue、它的
Artifact、它的密钥、它的托管推理 —— 仅此而已。

上下文选择依据存在性：设置了 `BUILDMAX_BRIDGE_SOCK` → Worker 上下文；否则若有已存储的
登录 → 本地上下文；两者皆无 → 命令说明它未连接。某上下文不允许的命令（例如运行令牌下的
`agent trigger`，而 worker 路由刻意不暴露它）会以清晰、对 LLM 有意义的消息失败 ——
绝不静默降级，也绝不为强求对称而新开一条 worker 路由。

## 5. 现在权限范围与护栏的归属

鉴权只有一个权威归属：Server 路由，以凭据为键。

- **Worker 上下文**由运行令牌和 worker 路由集合限定边界。边界就是路由的*缺失*：
  没有 Issue 更新路由、没有成员关系路由、没有 task 创建路由，因此运行令牌下的 Agent
  无论输入什么都触达不到它们。这与 [worker-run-token.md](./Worker运行令牌.md) 相比
  毫无变化；桥接只是携带令牌，它不额外授予任何东西。
- **本地上下文**由用户的 Space 授权限定边界，在每次调用时检查，与 Portal 完全一样。
  移除成员关系即停止后续访问。
- **按 Issue 的护栏迁移到 Server 侧。** 公有路由与 worker 路由都汇聚到同一个服务函数
  `issue.Service.CreateComment`，因此 Agent 正文长度限制（`AgentCommentBodyLimit`，
  在追加 Artifact 引用之前作用于 `agent`/`local_agent` 作者）与每-run 预算
  （`RunCommentBudget`，按 `source_task_run_id` 计数）都在此处强制执行——一个权威实现，
  约束运行时工具、CLI 以及任何未来客户端。这条更严格的 Agent 限制不影响人的评论，人仍受
  通用的 `CommentBodyLimit`。工具层的常量在工具被移除时一并移除。

## 6. 凭据处理

[客户端会话与 API 凭据（提案）](../proposals/client-sessions-and-api-credentials.md)
中的支配性规则是：凭据绝不能被模型所能看到的任何东西触及。

- **Worker 上下文完全遵守它。** 运行令牌留在 Worker 进程内；子进程只收到一个套接字
  路径。模型读写的任何东西都不携带令牌。
- **本地上下文接受一项有记录的风险。** 维护者于 `2026-09-19` 选择：让本地 Agent
  直接使用**用户自己的凭据**，而不是一个范围收窄的会话令牌。因为本地 `Bash` 沙箱默认
  关闭，能运行 `buildmax` 的 Agent 本就能以用户的全部权限行事 —— 读取用户能读的任何
  Issue、触发用户能触发的任何 Agent，并且如果用户是管理员，还能触达 admin 路由。这被
  接受为在用户自己机器上、其自身信任域之内。此处将其记录为一项有意识的权衡，而非疏漏；
  一个收窄的本地凭据（路由级 `scope`/`aud`/`client_id`，按凭据提案仍未构建）是在这份
  权限日后需要被限定时的升级路径，§12 对此进行追踪。

`buildmax` 二进制绝不通过参数、需要被交给它的环境变量、提示词文本或工具输入来接收
令牌。在本地它读取 broker；在 Worker 中它使用套接字。除上述本地全权限权衡之外，两者
都把秘密置于模型触及范围之外。

## 7. 命令入口

该入口就是既有的 `buildmax` 命令树，加以扩展，使 Agent 需要的 Server 能力全部可触达。
命令名仅为示意；权威列表是代码和 [CLI 参考](../../../manual/cli.md)。要点在于形态，而非
确切写法。

**组织方式。** Server 资源命令保留在顶层，作为单数资源名词——`buildmax issue`、
`buildmax agent`、`buildmax task`、`buildmax artifact`、`buildmax workflow`、
`buildmax run`——扩展既有的 `buildmax issue` 组，而**不**收拢到某个包装层（如
`buildmax connect …`）之下。资源名词本身就是分组：`buildmax issue --help` 就回答了
“我能对一个 Issue 做什么”。三个理由决定了这一点：

1. **一套入口，人与 Agent 共用。** §1 要求人的终端与 Agent 的 `Bash` 使用完全相同的
   命令名。`connect` 命名空间要么给已发布的 `buildmax issue` 命令改名，要么用两种方式
   做同一件事，还会暗示存在一种特殊“模式”，而实际上只有被解析出的上下文（§4）。
2. **分组的轴是资源，而非传输。** 一条命令是否触达 Server，是其资源的属性、按路由逐条
   决定的，而不是调用方主动进入的一种模式。`connect` 读起来是一个动作（正是
   `buildmax login` 已在做的），把名词嵌套在它之下是范畴错误。
3. **Agent 使用体验。** Agent 是通过 `Bash` **打出**这些命令的；每多一个必填段就是
   多一个 token、多一次出错机会。`buildmax issue view` 优于
   `buildmax connect issues view`。

为让较长的顶层保持可读，命令在 `--help` 中以 cobra command group 排序
（`cmd.AddGroup` / `GroupID`，vendored 的 cobra `v1.10.2` 已支持）：一个 “Server” 组
（`issue`、`agent`、`task`、`run`、`artifact`、`workflow`、`admin`、`plugin`、`usage`）
和一个 “Local” 组（`init`、`doctor`、`version`、`sandbox`、`tools`、`project`）。分组
只改变 `--help` 如何呈现命令，调用路径不变。这样用户无需一层模型还得复现的命名树，就能
看出一组命令属于同一类。

| 命令 | 本地（用户权限） | Worker（运行范围） |
|---|---|---|
| `issue view` | 用户可读的任意 Issue | 本次运行唯一的 Issue |
| `issue list` | 分配给该用户的 Issue | 不可用 |
| `issue comment` | Server 强制的预算/限制 | 同上，作用于本次运行的 Issue |
| `artifact publish <path>` | 进入一个具名的 Space | 进入本次运行的 Space |
| `run status` | 用户可读的某次运行 | 本次运行（状态、取消标志） |
| `agent trigger` / `task create` | 是 | 不可用（单次运行边界） |
| `workflow run` | 是 | 不可用 |

两条规则让该入口保持诚实：

1. **没有命令接受凭据。** 由上下文解析它（§4）。
2. **不可用是显式的。** 某上下文不允许的命令会明确说明，并以为模型撰写的消息非零退出，
   遵循 [AGENTS.md](../../../AGENTS.md) 中的工具输出规则。它不会静默降级，也绝不会为了让
   本地命令在运行令牌下工作而拓宽 worker 路由集合。

输出首先为 LLM 读者撰写：成功和失败时都有意义、稳定、且不含凭据。为脚本化 Agent 提供
机器可读输出（`--json`）。

## 8. Agent 永远不能做的事

以下不变量继承自 [issue-agent-access.md](./Issue Agent访问.md)，且无论传输方式如何
都成立：

- **状态、所有者、执行者和层级不可由 Agent 写入。** Agent 通过评论陈述发生了什么；
  只有人能陈述工作处于何种状态。在任一上下文中，都没有命令把这些暴露为 Agent 可用的
  写操作。
- **本地 Agent 的报告作为一项声明存储**（`local_agent` 作者身份），而非作为经 Worker
  验证的结果。
- **Issue 与评论文本是数据，绝非提示词层级**（§3）。

命令入口改变的是 Agent 访问 Server 的*机制*，而非 Agent 可以就 Space 拥有的工作做出
何种断言的*边界*。

## 9. 本设计取代了什么

- **[issue-agent-access.md](./Issue Agent访问.md) 的机制。** 当 §11 交付时，
  `GetIssue` / `ReportToIssue` 工具及其 `internal/tool` 注册被移除；该记录被削减为存留
  的产品边界（§8）或退役，由 §8 拥有那些规则。在此之前，它的工具仍是已交付的通路，
  该记录对当前代码保持准确。
- **[本地 Issue 工作桥接（提案）](../proposals/local-issue-work-bridge.md)
  中的客户端命令部分。** 该提案关于本地客户端如何读取、报告和返回工作的问题，在此得到
  解答。它仍未决的问题（持久的 Issue↔Session 关联、工作区映射、本地结果记录类型）不由
  本记录决定，并使该提案在决定之前保持开放。

## 10. 范围之外

- 路由级 `scope` / `aud` / `client_id` 以及收窄的本地凭据。由凭据提案追踪；§6 暂时
  接受本地的完整用户权限。
- 用于运行之外无头非交互使用的个人访问令牌和服务账号。一个具名用例会重启凭据提案的
  Stage 3。
- 桥接提案中持久的 Issue↔Session 关联和离线发件箱。
- 任何新的 worker 路由。本记录不新增任何路由；它只是把既有的运行令牌带给一个子进程。

## 11. 实施阶段

1. **先做 Server 侧护栏。** 把 Issue 评论预算和正文长度限制移入
   `issue.Service.CreateComment`——公有路由与 worker 路由都会到达的那一个函数——并配以
   测试，使其在任何客户端停止强制执行之前就已生效。**已交付：**
   `AgentCommentBodyLimit`、`RunCommentBudget`（由新增的 store 方法
   `CountIssueCommentsBySourceTaskRun` 按 run 计数）以及 Artifact 引用的拼装都已移入
   service。
2. **本地命令入口。** 在既有 auth broker 之上，用广度命令（§7）扩展
   `internal/interface/cli` 和 `internal/interface/client`；确认 Agent 的 `Bash`
   子进程能以用户凭据触达它们。价值最高，新增管道最少。**已交付部分：**
   `buildmax issue comment`（经 `CommentOnIssue` 发一条 `local_agent` 报告）、
   `buildmax agent trigger` 与 `buildmax task status`（触发并观察这对命令，经
   `TriggerAgent`/`GetTask`，并由 `FindAgent`/`FindTask` 跨 space fan-out——因为没有
   环境 space）、`buildmax artifact publish`（经 `httpclient.UploadFile` 用
   `PublishArtifact` 上传文件，打印可用于 `Artifacts:` 引用的 id）、
   `buildmax workflow run`/`list`/`status`（启动已发布 workflow 并跟踪，经
   `RunWorkflow`/`ListWorkflows`/`GetWorkflowRun`，并由 `FindWorkflow` fan-out），
   以及 `--help` 的命令分组（Server 与 Local，root.go 的 `groupTopLevelCommands`）。
   剩余：`task create`、`run status`。
3. **Worker 桥接。** 在 worker 运行内跑一个 Unix socket 反向代理，注入 run token 并转发
   到 worker listener，使子进程无需持有 token 即可访问 worker API。**已交付部分：** 传输
   层——`internal/infra/runbridge`，在 `bootstrap.RunWorker` 中随运行启动，导出
   `BUILDMAX_BRIDGE_SOCK` 与 `BUILDMAX_TASK_RUN_ID`（Bash 子进程自动继承；二者都不是机密，
   且 `_SOCK`/`_ID` 名称能通过沙箱环境擦除，因此无需改动 `FilterWorkerEnv`）。它只转发
   `/api/worker/` 路径，且 fail-open（桥接起不来的运行仍能执行）。剩余：CLI 检测
   `BUILDMAX_BRIDGE_SOCK` 并把 worker 上下文命令经它路由，以及 kind 验证单次运行隔离
   （一次运行的桥接无法触达另一次运行的路由——这由 run token 本身保证，桥接只是携带它）。
4. **退役工具。** 从 `internal/tool` 移除 `GetIssue` / `ReportToIssue`，更新
   `internal/tool/names.go`，并按 §9 削减或退役
   [issue-agent-access.md](./Issue Agent访问.md)。确保 `buildmax` 二进制位于 worker
   镜像内的 `PATH` 上，使该入口在 Agent 运行之处存在。
5. **文档与证据。** 更新 [CLI 参考](../../../manual/cli.md)、
   [当前状态](../current-state.md)、工具清单，并添加一条 changelog 片段；运行 kind
   端到端路径，展示运行内部的 Agent 使用 CLI 只触达它自己的运行。

## 12. 开放问题

1. **工具权限粒度。** 具名工具在 [tool-permissions.md](./工具权限.md) 中逐个出现；
   把 Server 访问收拢进 `Bash` 会使那份策略变粗。桥接或某个感知 `buildmax` 的权限
   匹配器是否需要重新暴露按命令的审批，还是 `Bash` 策略加 Server 授权就足够？
2. **二进制的普遍可用性。** worker 镜像必须携带 `buildmax`；所有执行界面（评估适配器、
   第三方执行器）是否都能在 `PATH` 上获得它，而当它们没有时故障模式是什么？
3. **何时收窄本地权限。** §6 接受本地的完整用户权限。哪个具体用例最先需要路由级
   `scope`/`aud` 和收窄的本地凭据，从而把那项工作移出 §10？
4. **`--json` 契约的稳定性。** 如果脚本化 Agent 依赖机器输出，哪些命令承诺稳定的
   JSON 形态，以及该契约记录在何处？
