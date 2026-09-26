# 工具

> **翻译说明：** 本文是[英文原文](../../../contribute/architecture/tools.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。
> **受众：** 贡献者 · **状态：** 当前有效
>
> 面向用户的工具指南：[manual/tools.md](../../../../manual/tools.md)

## 用途

`internal/tool` 包提供了 Agent 可以调用的运行时工具。每个工具都实现 `internal/core/llm.Tool` 接口，并通过 `internal/agentapp` 注册。工具是为 LLM 消费而设计的——它们的结果会以 tool 角色消息的形式发回给模型。

## 关键类型与接口

| 名称 | 种类 | 作用 |
|------|------|------|
| **llm.Tool** | 接口 | 契约：`Name()`、`Description()`、`Parameters()`、`Execute(ctx, args)` |
| **llm.AccessDeclarer** | 接口 | 可选：`Access(args) Access`——这次调用是否改变了什么 |
| **llm.ArgChecker** | 接口 | 可选：`CheckArgs(args) ToolAction`——参数级风险 |
| **llm.PolicyProvider** | 接口 | 可选：`DefaultAction() ToolAction`——覆盖推导出的默认值 |
| **llm.GrantScoper** | 接口 | 可选：`GrantScope(args) string`——缩小一次 Session 授权所覆盖的范围 |
| **ReadFile** | 结构体 | 读取某个根目录下的文件 |
| **WriteFile** | 结构体 | 在某个根目录下创建/覆盖文件 |
| **EditFile** | 结构体 | 在文件中执行精确的字符串替换 |
| **WebFetch** | 结构体 | 抓取 URL，将 HTML 转换为 markdown |
| **WebSearch** | 结构体 | 通过 Firecrawl 搜索公开网页 |
| **Bash** | 结构体 | 在工作区中运行 shell 命令 |
| **Glob** | 结构体 | 列出匹配 glob 模式的文件 |
| **Grep** | 结构体 | 按正则表达式搜索文件内容 |
| **TodoWrite** | 结构体 | 记录 Session 的任务列表 |
| **NoteWrite** | 结构体 | 记录持久化的 Session 笔记 |
| **MemoryRead** | 结构体 | 打开 project memory 索引行背后的正文 |
| **MemoryWrite** | 结构体 | 创建、替换或删除一条 project memory |
| **SkillTool** | 结构体 | 加载一个被发现的 Skill 的说明（`Skill`） |
| **TaskTool** | 结构体 | 运行指定类型的子代理（`Task`） |
| **UploadArtifact** | 结构体 | 把一个已完成的工作区文件发布为不可变的 Artifact |
| **Worktree** | 结构体 | 管理主运行的 Git worktree 及当前根目录 |
| **GetIssue** | 结构体 | 读取绑定到某个 Issue 范围运行上的 Issue |
| **ReportToIssue** | 结构体 | 向该 Issue 发布一份有限制的进度报告 |
| **JobList**、**JobOutput**、**JobStop** | 结构体 | 查看并停止本地后台任务 |
| **Monitor** | 结构体 | 把一条被监视的命令作为本地后台任务启动 |
| MCP 网关 | 结构体 | `LoadMcpTools` 与 `CallMcpTool` |

## 工具清单

### ReadFile（`Read`）

- **参数**：`path`（必需）、`offset`（可选，从 1 开始计数的行号）、`limit`（可选，默认 1000）
- **行为**：以带行号的格式（`LINE|CONTENT`）读取文件内容。支持通过 offset/limit 处理大文件。路径必须位于配置的根目录之下。
- **错误处理**：针对路径超出根目录、文件不存在等情况，返回清晰的错误信息。

### WriteFile（`Write`）

- **参数**：`path`（必需）、`content`（必需）
- **行为**：创建或覆盖一个文件，按需创建父目录。路径必须位于根目录之下。

### EditFile（`Edit`）

- **参数**：`path`（必需）、`old_string`（必需）、`new_string`（必需）、`replace_all`（可选，布尔值）
- **行为**：在文件中执行精确的字符串替换。默认只替换唯一的第一处匹配；`replace_all` 会替换所有出现的位置。如果找不到 `old_string`，或它存在歧义（在未指定 `replace_all` 的情况下有多处匹配），则调用失败。

### WebFetch（`WebFetch`）

- **参数**：`url`（必需）
- **行为**：抓取一个 URL，把 HTML 转换为 markdown。会缓存结果（默认 TTL 为 15 分钟）。可以选择使用 LLM 对内容进行处理/总结。

### WebSearch（`WebSearch`）

- **参数**：`query`（必需）
- **行为**：向 Firecrawl 发送有长度限制的查询，最多返回五条来源 URL、标题和短摘要。可免密钥调用；本地 `settings.yaml` 的密钥或 worker 的 `FIRECRAWL_API_KEY` Secret 授权可用于认证。网络沙箱会检查提供商域名。

### Bash（`Bash`）

- **参数**：`command`（必需）、`timeout`（可选，默认 120 秒，最长 600 秒）
- **行为**：在工作区根目录中运行一条 shell 命令。返回合并后的 stdout+stderr。输出在 3 万字符处截断。

### Glob（`Glob`）

- **参数**：`pattern`（必需）
- **行为**：列出根目录下匹配某个 glob 模式的文件。返回的路径按修改时间排序（最新的在前）。不以 `**/` 开头的模式会被自动加上前缀，以支持递归搜索。

### Grep（`Grep`）

- **参数**：`pattern`（必需），另有可选的 `path`、`glob`、`type`、`output_mode`、`-A`、`-B`、`-C`、`-i`、`multiline`、`head_limit`、`offset`
- **行为**：按正则表达式模式搜索文件内容。支持的输出模式有：`content`（匹配的行及上下文）、`files_with_matches`（仅文件路径）、`count`（匹配计数）。支持 glob/type 过滤、上下文行数、大小写不敏感，以及多行模式。

### TodoWrite（`TodoWrite`）

- **参数**：`todos`（必需，元素为 {id, content, status} 的数组）
- **行为**：替换 Session 的任务列表。状态包括：pending、in_progress、completed；至多有一条处于 in_progress。

### NoteWrite（`NoteWrite`）

- **参数**：`notes`（必需，字符串数组）
- **行为**：替换该 Session 的持久笔记。最多 15 条、每条 200 字符；超出限制的调用会失败，并在消息中指明具体限制。

### MemoryRead（`MemoryRead`）

- **参数**：`names`（必需，slug 数组）
- **行为**：返回这些 memory 的正文。不存在的名称会在结果中报告出来，而不会让整次调用失败。运行时会记录它所返回的每一份正文的摘要指纹，这正是让后续替换操作得以被拒绝的依据。

### MemoryWrite（`MemoryWrite`）

- **参数**：`name`（必需）、`content`（必需，可以为空）、`description`、`type`
- **行为**：恰好创建或替换一条 memory，每个 project 最多 20 条，description 至多 100 字符，正文至多 2,000 字符。`content` 为空则删除该条目。创建一个尚不存在的名称总会被接受；替换一条已有的，则要求本次运行读取过它——未经读取的替换和已经过期的替换，会被拒绝，且提示消息不同，因为前者需要一次读取，后者需要一次合并。schema 中不出现任何版本标记：比对完全留在运行时内部完成。
- **注册条件**：这两个工具只在满足以下条件的本地主运行上注册——其 Session 属于某个 project，且用户没有传入 `--no-project-memory`。它们是在各个 Agent 类型构建完成*之后*才追加进去的，因此任何子代理定义都无法指名它们，被委派方也不会携带这份索引。参见 [design/local-project-memory.md](../../design/本地项目记忆.md) §9。

### 按 surface 注册的工具

这些工具只有在当前 surface 提供了它们所需要的服务时，才会被注册。缺失某个工具，意味着该能力在这次运行中不可用，而不是一次权限拒绝。

| 工具 | 何时注册 | 参数 | 行为 |
|---|---|---|---|
| `UploadArtifact` | 该 surface 拥有一个 Artifact 发布器 | `path`（必需）；`title`、`purpose`、`share`（可选） | 把工作区内一个已完成、可读的普通文件，发布为一个不可变的 Artifact。 |
| `Worktree` | CLI 或 TUI 的主运行；子代理永不注册 | `action`（必需）；`name`、`path`、`discard_changes` 视具体 action 而定 | 创建、进入、离开、列出或移除 Git worktree，并让 Session 根目录随之移动。 |
| `GetIssue` | 主运行绑定到某一个 Issue，且拥有一个 Issue 客户端 | 无 | 返回所绑定 Issue 的快照与讨论内容。它无法选择另一个 Issue。 |
| `ReportToIssue` | 主运行绑定到某一个 Issue，且拥有一个 Issue 客户端 | `summary`（必需）；`artifact_ids`（可选） | 向所绑定的 Issue 发布一份有限制的进度报告。一次运行至多发布三份报告。 |
| `JobList` | 已启用本地后台任务（TUI 或 Desktop） | 无 | 列出由运行时启动的各项任务。 |
| `JobOutput` | 已启用本地后台任务（TUI 或 Desktop） | `job_id`（必需）；`stream`、`cursor`（可选） | 读取某个任务标准输出或标准错误流中的一段有限、增量的内容。 |
| `JobStop` | 已启用本地后台任务（TUI 或 Desktop） | `job_id`（必需） | 停止一个由运行时启动的后台任务。 |
| `Monitor` | 已启用本地后台任务（TUI 或 Desktop）；子代理永不注册 | `command`（必需）；`description`、`timeout`、`persistent`、`react`（可选） | 在 Bash 的风险与沙箱规则下运行一条被监视的命令。它的输出与生命周期由上述任务相关的工具处理。 |

Portal 的后台运行，可能会在所选 Agent 的附加系统提示词之前，再加入一层 Space 指令。这两者在该次运行中都是稳定的；附加提示词的 `## Invariants` 小节，会在这些工具所渲染进的同一个区块中被重述一遍。参见 [design/context-durability.md](../../design/上下文持久性.md)。

这两者（TodoWrite 与 NoteWrite）写入的是持久化的 Session 状态，而不只是返回一个格式化字符串就完事。这份状态保存在 `session.Session` 上，通过 context（`agent.CtxWithNoteStore`）来访问——因为工具注册表是按模型缓存、并跨 Session 共享的——并且每次调用后都会由 `agent.RenderSessionState` 重新渲染在消息列表之后。因此它从不会被裁剪，也从不会在历史记录中累积。一次子代理运行指向的是它自己的 Session，因此它不可能覆盖委派给它的那次运行的状态。参见 [design/context-durability.md](../../design/上下文持久性.md)。

memory 相关的工具遵循同样的“由 context 携带”模式（`agent.CtxWithMemoryStore`），但生命周期不同：这些 memory 属于 project，而不属于 Session，`agent.RenderMemoryIndex` 会把索引放在 Session 状态区块*之前*，这样当前 Task 所做的决定就始终离生成端最近。只有索引常驻，正文则以普通工具结果的形式到达。子代理两者都不继承——它的 context 会被 `agent.CtxWithoutMemoryStore` 移除这个 store。

面向 LLM 的名称，是 `names.go` 中的一组驼峰式（camelCase）常量；上面的清单涵盖了其中的每一个。`LoadMcpTools` 和 `CallMcpTool` 则单独声明在 `mcp_gateway.go` 中。这些常量是唯一的事实来源，因为 hook 的匹配规则和子代理的 `tools:` 字段，都会按照它们的精确字符串进行匹配。

## 一个工具如何声明自身

除 `llm.Tool` 之外，还有四个可选接口会影响权限层。完整的分层说明见：[design/tool-permissions.md](../../design/工具权限.md)。

**`Access(args)` 是每个工具都应该实现的那一个。** 它回答的是这次调用是否改变了用户拥有的任何东西。它的零值是 `AccessWrite`，因此不实现它是安全的，但信息量为零——这个工具会在交互式 surface 上弹出提示，却说不出任何理由。

它*不*代表以下两件事：

- **它不是一种并发保证。** `AccessReadOnly` 表示这次调用不改变任何东西，但并不承诺 `Execute` 可以安全地在多个 goroutine 上并发调用。`CallMcpTool` 是根据第三方自己的说法来报告只读的，而这一点本运行时是无法背书的，这正是它在工具层面声明 `AccessWrite`、转而在 `CheckArgs` 中逐次调用做判断的原因。
- **它不是权限的最终答案。** 权限是从它*推导*出来的，而这一步推导刻意不交给工具自己来做。一个能够自己指定动作的工具，最终总会把自己指定为 `allow`。

**`CheckArgs` 针对的是风险，而不是类别。** `ReadFile` 和 `WriteFile` 从它那里返回相同的结果——敏感路径为 `Ask`，否则为 `Allow`——因为这里衡量的维度，是*这次*调用有多危险，而不是它属于哪一类行为。注意这里的 `Allow` 意味着*弃权*：解析会继续交给后面的层级。

**`DefaultAction` 会覆盖推导结果，因此需要一个理由。** 有三个工具实现了它，全都是不能弹出提示的写操作：

| 工具 | 原因 |
|---|---|
| `TodoWrite`、`NoteWrite` | 写的是 Agent 自己的草稿状态，而不是用户的文件 |
| `Bash` | 在 `CheckArgs` 中自有一套更精细的判断；类别层面的默认值会导致连一次 `ls` 都会弹出提示 |

**`GrantScope` 是给那些做分发的工具用的。** 没有它，一次针对 `CallMcpTool` 的 Session 授权，就会覆盖每个已配置 server 上的每一个工具；一次针对 `BrowserNavigate` 的授权就会准入每一个来源（它通过 `BrowserOrigin` 返回 URL 的来源）。循环把返回的目标传给审批处理器，TUI 与 Desktop 的提示会显示它。

### 并发

调度器会把同一条模型消息中相邻的 `AccessReadOnly` 调用同时运行，因此声明只读还附带着第二项义务，而这项义务是类型系统无法检查的：**`Execute` 必须可以安全地被多个 goroutine 同时调用。** 效果意义上的只读并不蕴含这一点。值得记住的一个例子是 `WebFetch`——它不改变用户拥有的任何东西，之所以可以被调度并发执行，仅仅是因为它写入的响应缓存受 `cacheMu` 保护。去掉这把互斥锁，它依然是只读的，却不再能安全地批量运行。

这在实践中意味着：不能有未经同步的包级或结构体级可变状态，也不能假设同组的其他调用不会碰到同一个文件。检查手段是 `./make test race`；请写出能够捕获这类问题的测试。

如果一个工具本质上是只读的，但确实无法并发运行，就声明 `AccessWrite`，并在注释中说明原因。`TodoWrite` 和 `NoteWrite` 正是这么做的——它们只写 Agent 自己的草稿状态，这也是它们在权限上声明 `DefaultAction() = Allow` 的原因，但那份状态没有加锁，因此正是这个写操作分类，把它们排除在批量执行之外。

`Access` 接收的是这次调用的参数，因此一个针对不同参数做不同事情的工具，会逐次调用来作答。`Task` 就是这样的工具：当请求的 `subagent_type` 解析出的工具集合中每一个都是只读时，它就返回 `AccessReadOnly`——内置的 `explore`，以及任何被限制为只能读取的用户自定义 Agent，都是这种情况。能够触达任意一个写工具的类型、未知类型，以及 `run_in_background`，全都是写操作。子代理的嵌套循环同样继承父级的 `max_parallel_tools`，因此一个只读 Agent 既会与自己的同组读操作重叠执行，也会与其兄弟调用重叠执行。

### 新增一个工具

声明 `Access`，并在选择 `AccessReadOnly` 之前，先阅读上面提到的并发义务。如果某些参数比其他参数更危险，就加上 `CheckArgs`。只有当这个工具确实比类别默认值更了解情况时，才使用 `DefaultAction`，并在注释中说明原因。然后在 [design/tool-permissions.md](../../design/工具权限.md) 第 6 节的表格中加上一行——`internal/tool/permission_test.go` 是基于这张表做表驱动测试的，在你添加之前会一直失败。

## 工作方式

1. `internal/agentapp` 解析工作区根目录，并构建基础工具注册表。
2. 基础工具包括文件操作、bash、glob/grep、web fetch、web search、todo、skill，以及可选的 MCP 网关工具。
3. `internal/core/agent.RunLoop` 接收一个 `llm.ToolRegistry`。
4. 在循环过程中，当 LLM 返回工具调用时，Agent 会按名称查找每个工具、解析 JSON 参数，并调用 `Execute()`。
5. 结果（或错误）会以 tool 角色消息的形式追加到当前的历史记录中。

## 依赖

- **使用**：`internal/core/llm`（工具契约）、`internal/infra/llm`（供 WebFetch 的 LLM 调用使用）、`internal/infra/mcp`（供 MCP 网关使用）
- **使用方**：`internal/agentapp`（构建注册表）、`internal/core/agent`（执行工具调用）

## 说明

- 所有工具都强制执行路径安全——文件操作必须位于配置的根目录之下。
- 工具输出是为 LLM 消费而设计的：无论成功还是失败，都要给出有意义的消息。
- 错误消息在发送给 LLM 时，会被 Agent 加上 `error:` 前缀。
- 另见：[Agent 循环](agent-loop.md)、[CLI](cli.md)、[manual/tool-permissions.md](../../../../manual/tool-permissions.md)。
