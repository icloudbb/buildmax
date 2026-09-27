# Hook 系统 v2

> **翻译说明：** 本文是[英文原文](../../design/hook-system.md)的简体中文派生翻译。若中英文存在语义冲突，以英文原文为准。

## 目录

- [状态](#状态)
- [1. 目的](#1-目的)
- [2. 方向](#2-方向)
- [3. 架构形态](#3-架构形态)
- [4. 配置](#4-配置)
- [5. Hook 类型（驱动）](#5-hook-类型驱动)
- [6. 事件覆盖范围](#6-事件覆盖范围)
- [7. HookManager](#7-hookmanager)
- [8. 运行时流程](#8-运行时流程)
- [9. 分层](#9-分层)
- [10. 实施步骤](#10-实施步骤)
- [11. 风险与要点](#11-风险与要点)
- [12. 验收](#12-验收)
- [13. 建议的交付顺序](#13-建议的交付顺序)

## 状态

- roadmap_priority：`P0.5`
- status：`implemented`——16 个事件与全部四种传输方式均已交付；可选的检查器和 frontmatter 集成仍延后实现
- follows：[trust-harness.md](./信任保障.md)
- roadmap：[ROADMAP.md](../ROADMAP.md)
- created_at：`2026-05-23`

## 1. 目的

前一代 Hook 系统随信任保障设计 §3.1 一起交付，只覆盖五个事件、一种 shell 传输方式，以及一个全局配置位置。本设计对 Hook 做了扩展，使其在保真度上接近 Claude Code 文档中描述的 Hook 设计：

- 除全局的 `settings.yaml` 之外，新增工作区范围的定义。
- 支持多种 Hook 传输方式：`command`、`http`、`mcp_tool`、`prompt`。
- 提供保真度更高的事件集，锚定在 BuildMax 实际拥有的节点上（提示提交、会话生命周期、工具结果成功/失败、批准通知、子代理启动/停止、主 Run 停止/失败、压缩）。
- 提供一个中央 `HookManager`，与现有的 `MCPManager` 模式一致：持有合并后的配置、维护驱动注册表、暴露 `Status`、`Refresh` 和 `Close`，并实现 `agent.HookRunner`，因此其余运行时代码无需改动。

目标是让用户和运维人员只需定义一次策略、格式化、审计和外部审批流程，就能统一应用到 CLI、Desktop、Worker 和子代理运行中。

## 2. 方向

P0.5 §3.1 把 Hook 作为基础引入。v2 让这个基础能够支撑真实的自动化用例，而不必承接 Claude Code 文档化 Hook 产品的全部范围。具体而言：

- 工作区 Hook 是必需的：项目范围的策略在今天还无法交付。
- 多种传输方式让每项工作都能用上成本最低且正确的工具（格式化器 → command，中央策略 → http，LLM 判断 → prompt，已有的 MCP 工具 → mcp_tool）。
- 更宽的事件集解锁了当前单一 `RunEnd` 事件无法表达的审计与审批流程。

任何涉及 UX 界面的内容（`/hooks` 检查器、异步 Hook、会修改工具参数的 Hook 输出）都推迟到 P0.5 §3.4 的活动视图上线之后。

## 3. 架构形态

v2 复用了仓库中现有的 **MCP** 布局，让各子系统的模式保持一致：

| 层级 | 现有 MCP | Hooks v2 |
|---|---|---|
| 领域契约 | `core/llm`（Tool、ToolCall） | `core/agent/hook.go`（HookEvent、HookInput/Output、HookRunner） |
| 外部系统实现 | `infra/mcp`（传输、注册表） | `infra/hook`（每种类型一个驱动） |
| 应用组装 | `agentapp/mcp_manager.go`（生命周期、Status、Refresh） | `agentapp/hook_manager.go`（生命周期、合并配置、分发） |
| 配置 | `config/mcp.go` + `<workspace>/.buildmax/mcp.json` | `config/hooks.go` + `<workspace>/.buildmax/hooks.yaml` |

`HookManager` 实现了 `agent.HookRunner`，因此现有的 RunLoop 集成保持不变。

## 4. 配置

### 4.1 文件布局

| 范围 | 路径 | 格式 |
|---|---|---|
| 全局（用户） | `<BUILDMAX_HOME>/settings.yaml`（`hooks:` 块——已有） | YAML |
| 工作区 | `<workspace>/.buildmax/hooks.yaml`（新增） | YAML |
| Skill frontmatter | Skill YAML frontmatter 中的 `hooks:` | YAML（延后，见 §10） |
| Subagent frontmatter | Agent 定义中的 `hooks:` | YAML（延后，见 §10） |

**合并规则。** 各层的条目按 `(global, workspace)` 的顺序针对每个事件拼接起来。两层都会对同一事件执行。第一个给出阻断决策的 Hook 决定这道门的结果，但所有匹配的 Hook 仍会全部执行，以便观察者和审计 Hook 能看到每一次调用。

这与 Claude Code 文档中描述的 Plugin/用户/项目合并的追加式行为一致。

### 4.2 多态的 `HookEntry`

`config.HookEntry` 变成一个带 `type` 判别字段、并按类型携带各自字段的 tagged union。未知类型会在加载时记录一条警告并被跳过。

```yaml
hooks:
  pre_tool_use:
    - type: command          # default if omitted
      matcher: "writefile|editfile"
      command: "./.buildmax/hooks/policy.sh"
      timeout: 5

    - type: http
      matcher: "bash"
      url: "https://policy.internal/check"
      headers: { Authorization: "Bearer $POLICY_TOKEN" }
      allowed_env: [POLICY_TOKEN]
      timeout: 10

    - type: mcp_tool
      matcher: "writefile"
      server: "code-scanner"
      tool: "scan_file"
      input: { path: "${tool_args.path}" }

    - type: prompt
      matcher: "bash"
      model: ""               # empty = default fast model
      prompt: |
        The agent is about to run: $ARGUMENTS
        Reply with JSON {"decision":"allow"|"block","reason":"..."}.
```

每个条目共有的字段：`type`、`matcher`、`timeout`。

按类型区分的字段：

- `command`：`command`、`args`、`shell`（默认为 `bash`）。
- `http`：`url`、`headers`、`allowed_env`（在 header 和 url 中插值 `$VAR` 时使用的白名单）。
- `mcp_tool`：`server`、`tool`、`input`（其中的 `${field}` 会替换为 `HookInput` 中的字段）。
- `prompt`：`prompt`（其中 `$ARGUMENTS` 占位符代表序列化后的 `HookInput`）、可选的 `model` 覆盖值。

### 4.3 加载

- `config.LoadSettings()` 继续加载全局的 `hooks:` 块。
- 新增 `config.LoadWorkspaceHooks(workspace string) (HooksConfig, error)`，读取 `<workspace>/.buildmax/hooks.yaml`；文件不存在时返回 `(HooksConfig{}, nil)`。
- 新增 `config.MergeHooks(global, workspace HooksConfig) HooksConfig`，执行 §4.1 所述的按事件拼接。

## 5. Hook 类型（驱动）

| 类型 | 驱动 | 依赖 | 状态 |
|---|---|---|---|
| `command` | `infra/hook/command.go`（对现有 shell.go 的重构） | 无 | v2 |
| `http` | `infra/hook/http.go` | `net/http` | v2 |
| `mcp_tool` | `infra/hook/mcp.go` | `HookMCPCaller`（由 agentapp 包装 MCPManager 实现） | v2 |
| `prompt` | `infra/hook/prompt.go` | `HookLLMCaller`（由 agentapp 包装 LLMClientCache 实现） | v2 |
| `agent` | 延后（Claude Code 将其标记为实验性） | 子代理 runner | 未来 |

### 5.1 驱动契约

驱动完整地位于 `infra/hook` 之下，让 core 保持纯粹。`Driver` 接口以及镜像配置的 `Entry` 结构体定义在 `infra/hook/driver.go` 中；`core/agent` 不导入 config。

```go
// infra/hook/driver.go

type Driver interface {
    Type() string
    Run(ctx context.Context, entry Entry, in agent.HookInput) agent.HookOutput
}

// MCPCaller / LLMCaller decouple the MCP and prompt drivers from the
// concrete MCPManager / LLMClientCache. Implementations live in agentapp.
type MCPCaller interface {
    CallMCPTool(ctx context.Context, server, tool string, input map[string]any) (string, error)
}
type LLMCaller interface {
    CompleteHookPrompt(ctx context.Context, model, prompt string) (string, error)
}
```

### 5.2 输出 schema

目前所有驱动都会归一化为 `agent.HookOutput{Decision, Reason}`。

- `command`：退出码 0 表示允许，退出码 2 表示阻断（stderr 作为原因），其他退出码表示失败开放（fail open）。stdout 上可选的 JSON 会覆盖默认决策。
- `http`：2xx 表示允许，4xx/5xx 表示失败开放，响应体可以是 `{"decision":"block","reason":"..."}`。没有响应体时，专门的 422 状态码会被当作阻断处理。
- `mcp_tool`：如果工具返回的文本结果看起来像 Hook 输出，就解析为 JSON；否则按允许处理。
- `prompt`：LLM 的响应会被解析为 JSON；解析失败则失败开放。

Claude Code 更宽泛的输出 schema（`continue`、`stopReason`、`suppressOutput`、`systemMessage`、`hookSpecificOutput.additionalContext`、`modifiedToolInput`）有意没有在 v2 中实现。其中每一项都意味着一个目前尚不存在的 UI 或控制界面。这个 schema 是可追加式的——以后可以在不破坏现有 Hook 的前提下加入这些字段。

## 6. 事件覆盖范围

v2 把事件集从 5 个扩展到了 13 个，工作树生命周期又带来了最后 3 个，合计 16 个。事件名沿用 Claude Code 的驼峰命名；YAML 键则按照 CLAUDE.md §6.1 继续使用蛇形命名。

| 事件 | 锚点 | 是否门控？ | 变化 |
|---|---|---|---|
| `SessionStart` | `agentapp.OpenSession` | 否 | 新增 |
| `SessionEnd` | `SessionManager.Finalize`/关闭 | 否 | 新增 |
| `UserPromptSubmit` | `agentapp.RunPrompt`，在 `sess.Append` 之前 | **是**——阻断会中止本轮 | 新增 |
| `PreToolUse` | 已有节点 | 是 | 保留 |
| `PostToolUse` | `applyPolicyAndExecute` 的成功路径 | 否 | 保留（收窄为仅成功路径） |
| `PostToolUseFailure` | `applyPolicyAndExecute` 的错误路径 | 否 | 新增 |
| `Notification` | `applyPolicyAndExecute` 中 action=Ask 时，以及发生 PermissionDenied 时 | 否 | 新增 |
| `PreCompact` | 已有节点 | 是 | 保留 |
| `PostCompact` | 已有节点 | 否 | 保留 |
| `SubagentStart` | 进入 `subagent_runner.RunSubAgent` 时 | 否 | 新增 |
| `SubagentStop` | `subagent_runner.RunSubAgent` 成功退出时 | 否 | 新增 |
| `Stop` | `RunLoop` 成功退出（仅限主 Run） | 否 | 新增（取代 `RunEnd` 的成功路径） |
| `StopFailure` | `RunLoop` 出错退出（主 Run 或子代理） | 否 | 新增（取代 `RunEnd` 的错误路径） |
| `WorktreeCreate` | `worktree.Manager` 创建完工作树之后 | 否 | 新增 |
| `WorktreeRemove` | `worktree.Manager` 移除完工作树之后 | 否 | 新增 |
| `CwdChanged` | 会话的工作区根发生移动时，包括移入某个工作树 | 否 | 新增 |

`RunEnd` 是被直接移除而非保留别名——它和信任保障的工作在同一批交付，且没有外部消费者。

`HookInput` 新增了：

- `Prompt string`——在 `UserPromptSubmit` 时填充。
- `AgentType string`——在 `SubagentStart/Stop` 时填充；在子代理内运行时，也会盖在每一个事件上，便于审计 Hook 做归因。
- `IsSubagent bool`——无需解析 `AgentType` 即可区分 `Stop` 和 `SubagentStop`。
- `NotificationKind string`——取值为 `approval_required`、`permission_denied` 或 `user_question`（有一个 `AskUser` 问题在等待作答；见 [Agent 向用户提问](Agent向用户提问.md)）。

`WorktreeCreate`、`WorktreeRemove` 和 `CwdChanged` 之前之所以被推迟，是因为依赖一项 BuildMax 当时还没有的能力；现在这项能力已经具备，三者都随[工作区根与工作树](工作区根与工作树.md)一文所述的工作树生命周期一并交付。三者都只是提示性（advisory）事件。请求创建工作树的工具调用本身已经经过 `PreToolUse`，针对同一个决策再加一道门，只会导致某个工作树处于半创建状态；而且一个失败的 Hook 也无法撤销一个已经发生的移动。想知道“这个会话现在在哪里工作”应该订阅 `CwdChanged`；另外两个事件说明的是工作树本身发生了什么变化。

仍然处于延后状态（与针对 Claude Code 的差距分析一致）的有：`Setup`、`UserPromptExpansion`、`SpacemateIdle`、`TaskCreated`、`TaskCompleted`、`FileChanged`、`Elicitation`、`ElicitationResult`、`ConfigChange`、`PostToolBatch`、`InstructionsLoaded`、`PermissionRequest`。它们要么在我们的模型中已经被其他事件覆盖，要么依赖 BuildMax 尚不具备的功能。

## 7. HookManager

```
internal/agentapp/hook_manager.go
```

```go
type HookManager struct {
    cfg      config.HooksConfig          // already merged global + workspace
    drivers  map[string]hook.Driver      // "command" → CommandDriver, etc.
    matchers map[string]*regexp.Regexp   // compiled lazily, shared cache
    mu       sync.Mutex
}

type HookManagerDeps struct {
    Workspace string
    MCPCaller hook.MCPCaller   // built from MCPManager
    LLMCaller hook.LLMCaller   // built from LLMClientCache
}

func NewHookManager(ctx context.Context, cfg config.HooksConfig, deps HookManagerDeps) (*HookManager, error)

func (m *HookManager) Run(ctx context.Context, in agent.HookInput) agent.HookOutput
func (m *HookManager) Refresh(ctx context.Context, cfg config.HooksConfig) error
func (m *HookManager) Status() HookStatus
func (m *HookManager) Close() error
```

`Run` 内部的分发流程：

1. `entries := m.cfg.Entries(in.Event)`——按声明顺序合并后的列表。
2. 按 matcher 过滤（对 `in.ToolName` 做正则匹配；空 matcher 匹配任何内容；非工具类事件会跳过 matcher 非空的条目）。
3. 对每个条目，查找 `m.drivers[entry.Type]`；找不到驱动就记录日志并跳过。
4. 调用 `driver.Run(ctx, entry, in)`。
5. 聚合结果：第一个 `HookDecisionBlock` 成为管理器的最终输出；其余条目仍会继续执行（便于审计）。

`MCPCaller` 和 `LLMCaller` 的适配器放在 `agentapp/hook_callers.go` 中，让 `infra/hook` 不必依赖 agentapp：

```go
// agentapp/hook_callers.go
type mcpCaller struct{ m *MCPManager }
func (c *mcpCaller) CallMCPTool(ctx context.Context, server, tool string, input map[string]any) (string, error) { ... }

type llmCaller struct{ cache *LLMClientCache; defaultModel string }
func (c *llmCaller) CompleteHookPrompt(ctx context.Context, model, prompt string) (string, error) { ... }
```

## 8. 运行时流程

本节讲解启动阶段以及单次事件触发时实际发生的过程，属于参考性说明；权威定义是上文的各项契约。

### 8.1 启动——构建管理器

```
                  +---------------------------+
   YAML files →   |  config.LoadSettings()    |   →  global HooksConfig
                  +---------------------------+
                              |
                  +---------------------------+
   YAML files →   |  LoadWorkspaceHooks(ws)   |   →  workspace HooksConfig
                  +---------------------------+
                              |
                              v
                  +---------------------------+
                  |  config.MergeHooks(g, w)  |   →  merged HooksConfig
                  +---------------------------+   (additive: global then
                                                   workspace, per event,
                                                   in declared order)
                              |
                              v
   +-----------------------------------------------------------+
   |                AgentApp.NewAgentApp(cfg)                  |
   |                                                           |
   |   builds       MCPManager   ─┐                            |
   |                LLMClientCache ┼─ deps                     |
   |                                                           |
   |   wraps deps in adapters:                                 |
   |       mcpCaller{m: MCPManager}                            |
   |       llmCaller{cache: LLMClientCache}                    |
   |                                                           |
   |   hook.NewDriverRegistry(deps) →   {                      |
   |       "command":  CommandDriver{}                         |
   |       "http":     HTTPDriver{}                            |
   |       "mcp_tool": MCPDriver{mcpCaller}                    |
   |       "prompt":   PromptDriver{llmCaller}                 |
   |   }                                                       |
   |                                                           |
   |   NewHookManager(merged, drivers)  ──►  implements        |
   |                                          agent.HookRunner |
   +-----------------------------------------------------------+
                              │
                              v
              Passed into every agent.RunLoopOpts.Hooks
```

对运行时其余部分而言，管理器就是一个实现了 `agent.HookRunner` 的对象。驱动的多态性在这一层之上是不可见的。

### 8.2 单次事件触发——分发路径

```
   agent.RunLoop / agentapp.RunPrompt / SubAgentRunner / SessionManager
                              │
                              │  in := agent.HookInput{Event: ..., ...}
                              v
                  HookManager.Run(ctx, in)
                              │
       entries := cfg.Entries(in.Event)
              │  (global, then workspace, in declared order)
              v
         for each entry:
              │  matcher applies?
              │   PreToolUse/PostToolUse →
              │     regex on in.ToolName
              │   other events →
              │     skip entries with a non-empty matcher
              │
              │  driver := drivers[entry.Type]
              v
         driver.Run(ctx, entry, in)
              │
              │           ┌────────────┐
              │           │  command   │ exec sh -c, stdin=JSON
              │           │  http      │ POST, body=JSON
              │           │  mcp_tool  │ MCPCaller.CallMCPTool(...)
              │           │  prompt    │ LLMCaller.CompleteHookPrompt(...)
              │           └────────────┘
              v
         normalize → agent.HookOutput{Decision, Reason}
              │
              └──────► aggregate
                       │  first Block wins;
                       │  every matching hook still runs
                       v
              return HookOutput
                              │
                              v
              caller decides: gate vs advisory
```

`PreToolUse` 是在权限层已经允许该调用**之后**才触发的，因此 Hook 看到的只是策略、审批以及任何会话级授权放行之后的结果。它无法把一次拒绝重新变为允许；它是最后一道门，而不是一个覆盖层。分层关系参见[工具权限](./工具权限.md)。

自从工具调用开始出现重叠执行（见[并行工具执行](./并行工具执行.md)）之后，有两个顺序性质发生了变化。当一批只读调用被归为一组时，`PreToolUse` 会在这组里**每一个**成员执行之前触发——也就是说，一个在多次调用之间检查文件系统状态的 Hook，看到的是整组执行之前的状态，而不是每次调用之前的状态。这是不可避免的：这些调用本来就是设计成重叠执行的，并且只出现在工具自己声明为只读的调用上，而按定义只读调用不会产生写入。`PostToolUse` 和 `PostToolUseFailure` 会在这组调用汇合之后触发，顺序仍然是调用顺序，因此审计 Hook 看到的顺序不变，代价是要等到这组里最慢的那个调用完成。

门控类事件（`PreToolUse`、`PreCompact`、`UserPromptSubmit`）会检查 `out.Blocked()` 并短路。提示性事件（`PostToolUse`、`Notification`、`Stop`、`SessionStart` 等）则会丢弃这个决策并继续执行。

### 8.3 一次具体的对话轮次——触发顺序

用户运行 `buildmax` 并提交提示——“请把结果写入 out.txt”。Hook 已经配置好，模型决定调用 `writefile`。

```
  T0   AgentApp.OpenSession(...)
       └─ HookManager.Run(SessionStart{session_id, workspace})        [advisory]

  T1   AgentApp.RunPrompt(ctx, sess, "please write...")
       │
       ├─ HookManager.Run(UserPromptSubmit{prompt, session_id})       [GATING]
       │     • PromptDriver runs the LLM judge → {decision:"allow"}
       │     • CommandDriver runs ./scan-prompt.sh → exit 0
       │     • not blocked → proceed
       │
       └─ agent.RunLoop(...)
           │
           ├─ LLM call 1 → tool_calls=[writefile(path="out.txt", ...)]
           │
           ├─ applyPolicyAndExecute("writefile", args)
           │   │
           │   ├─ permission: Allow (see tool-permissions.md for the layering)
           │   ├─ HookManager.Run(PreToolUse{tool, args})              [GATING]
           │   │     • HTTPDriver POSTs to https://policy.internal
           │   │           ← 200 {"decision":"allow"}
           │   │     • CommandDriver runs ./.buildmax/hooks/policy.sh
           │   │           ← exit 0
           │   │     • not blocked → execute tool
           │   │
           │   ├─ tool.Execute(...) → writes file, returns "ok"
           │   │
           │   └─ HookManager.Run(PostToolUse{tool, args, result})     [advisory]
           │         • CommandDriver runs "gofmt -w ." → exit 0
           │
           ├─ LLM call 2 → final reply "done."
           │
           └─ HookManager.Run(Stop{stats, is_subagent=false})          [advisory]

  T2   SessionManager.Finalize(sess)
       └─ HookManager.Run(SessionEnd{session_id, stats})               [advisory]
```

同一条路径还覆盖了以下几种情形：

- **在 `PreToolUse` 中被阻断。** HTTPDriver 返回 `{"decision":"block","reason":"forbidden path"}`。`applyPolicyAndExecute` 直接短路，产生 `error: tool call "writefile" denied by hook: forbidden path`，并发出 `EventToolDenied`（reason=`hook`），LLM 收到的就是这条错误字符串。
- **工具执行失败。** 路径相同，但 `tool.Execute` 返回了一个错误。这时触发的提示性 Hook 是 `PostToolUseFailure`（而不是 `PostToolUse`），负载里带有 `tool_error`。
- **审批关卡。** 当策略解析结果为 `Ask` 时，管理器会在调用 `ApprovalHandler` **之前**先触发 `Notification{kind="approval_required", tool, args}`。如果被拒绝，则触发 `Notification{kind="permission_denied"}`。
- **子代理。** 子代理 runner 会在该子代理产生的每一个事件上盖上 `IsSubagent=true` 和 `AgentType="<def-name>"`。生命周期是 `SubagentStart → ... → SubagentStop`（或 `StopFailure`），而不是 `Stop`。
- **压缩。** `PreCompact`（门控）可以跳过一轮压缩；成功时会触发 `PostCompact{summarized, kept, summary}`。

### 8.4 失败模式——按设计失败开放

| 失败点 | 结果 | 是否记录日志 |
|---|---|---|
| `entry.Type` 找不到对应驱动 | 跳过该条目 | 警告 |
| matcher 正则表达式无效 | 该条目永远不会匹配 | 警告 |
| command 以状态码 2 退出 | **阻断**，stderr 作为原因 | info |
| command 以其他非零状态码退出 | **允许**（失败开放） | 警告 |
| command 超时 | **允许**（失败开放） | 警告 |
| HTTP 5xx / 连接错误 | **允许**（失败开放） | 警告 |
| MCP server 不可达 | **允许**（失败开放） | 警告 |
| PromptDriver 的 LLM 调用出错 | **允许**（失败开放） | 警告 |
| PromptDriver 返回非 JSON 内容 | **允许**（未作出决策） | debug |

一个坏掉的 Hook 永远不会默默弄坏 Agent 循环。唯一能让一次 Run 停下来的方式，是一个明确的阻断决策。

## 9. 分层

```
internal/core/hook/config.go            # Entry, Config, event and transport constants
internal/core/agent/hook.go             # HookRunner, HookEvent, HookInput/Output
internal/config/hooks.go                # load, merge, and the plugin layer
internal/infra/hook/driver.go           # Driver, caller interfaces, NewDriverRegistry(deps)
internal/infra/hook/command.go          # CommandDriver
internal/infra/hook/http.go             # HTTPDriver
internal/infra/hook/mcp.go              # MCPDriver
internal/infra/hook/prompt.go           # PromptDriver
internal/infra/hook/output.go           # decode a driver's textual output into a decision
internal/agentapp/hook_manager.go       # HookManager (implements agent.HookRunner)
internal/agentapp/hook_callers.go       # mcpCaller / llmCaller adapters
```

条目和配置类型落在了 `internal/core/hook` 里，而不是最初计划的 `internal/config`，这样运行时契约就不需要经过负责加载文件的那个包。

架构测试的合规情况：

- `core/agent` 不导入 infra/config（未变）。
- `infra/hook` 导入 `core/agent` 和 `core/hook`——二者层级都更低。
- `agentapp` 导入 `infra/hook`、`infra/mcp`、`config`——本来就是如此。

## 10. 实施步骤

每个阶段都可以独立交付；每个阶段都以测试作为门禁。

### 阶段 A——工作区配置

A1. 在 `config.HookEntry` 上新增 `Type` 字段（留空时默认为 `"command"`）。
A2. 新增 `config.LoadWorkspaceHooks(workspace)`，读取 `<workspace>/.buildmax/hooks.yaml`；文件缺失即视为空。
A3. 新增 `config.MergeHooks(global, workspace)`——按事件拼接。
A4. 测试：仅工作区、仅全局、合并后、文件缺失、文件格式错误。

### 阶段 B——驱动注册表 + 传输方式

B1. 创建 `infra/hook/driver.go`（`Driver`、镜像结构体 `Entry`、`MCPCaller`、`LLMCaller`）。
B2. 把现有的 `infra/hook/shell.go` 重构为 `infra/hook/command.go`，实现 `Driver`。原有测试必须继续通过。
B3. 实现 `HTTPDriver`，`$VAR` 插值受 `allowed_env` 限制。
B4. 实现 `MCPDriver`，使用 `MCPCaller`；把 `HookInput` 中的字段替换进 `entry.Input` 里的 `${field}` 引用。
B5. 实现 `PromptDriver`，使用 `LLMCaller`；用 `HookInput` 的 JSON 替换 `$ARGUMENTS`；解析响应 JSON 得到 `{decision, reason}`。
B6. `infra/hook/driver.go`：`NewDriverRegistry(deps) map[string]Driver`。
B7. 为每个驱动编写测试，MCP 和 LLM 调用方用 stub 代替。

### 阶段 C——HookManager

C1. `agentapp/hook_manager.go`：自持合并后的配置、驱动注册表、matcher 缓存；实现 `agent.HookRunner`。
C2. `agentapp/hook_callers.go`：来自 `MCPManager` 和 `LLMClientCache` 的适配器。
C3. `NewAgentApp` 用 `(globalSettings.Hooks, workspaceHooks)` 和相应依赖构建管理器，取代原来对 `hook.NewShellRunner(...)` 的直接调用。
C4. `HookManager.Status()` 返回一个结构体，供未来的 `buildmax hooks` 检查器使用。
C5. 端到端测试：工作区文件 + 全局设置 + 多种类型，在一个真实的 `AgentApp` 中都能正确触发。

### 阶段 D——事件覆盖

D1. 新增 `HookEvent` 常量。给 `HookInput` 扩充 `Prompt`、`AgentType`、`IsSubagent`、`NotificationKind`。
D2. 移除 `RunEnd`（调用点改为 `Stop` / `SubagentStop` / `StopFailure`）。
D3. RunLoop：把 `IsSubagent` 透传进 `RunLoopOpts`；成功时发出 `Stop` 或 `SubagentStop`；出错时发出 `StopFailure`。
D4. `applyPolicyAndExecute`：把 `PostToolUse` 拆成成功/失败两条路径；在 `Approval.RequestApproval` 之前触发 `Notification(kind=approval_required)`；被拒绝时触发 `Notification(kind=permission_denied)`。
D5. `agentapp.RunPrompt`：在写入历史记录之前触发 `UserPromptSubmit`；如果被阻断，直接把 reason 作为回复返回，不再调用 LLM。
D6. `SessionManager`：在 `OpenSession` 时触发 `SessionStart`；在 `Finalize`/关闭时触发 `SessionEnd`。
D7. `subagent_runner`：在 `RunLoopOpts` 上盖 `IsSubagent=true`、`AgentType`；在 RunLoop 之前触发 `SubagentStart`。
D8. 用已有的 `recordingHookRunner` 模式为每个新事件编写测试。

### 阶段 E——文档与开发者体验

E1. 扩充 `config-examples/settings.example.yaml`，每种类型都给一个示例。
E2. 新增面向工作区级别的 `config-examples/hooks.workspace.example.yaml`。
E3. 更新 `design/trust-harness.md`：§3.1 标记为 ✅，并附上已交付的事件列表。
E4. 更新 CLAUDE.md，指向新的事件和位置。

### 阶段 F——延后的后续工作

F1. `agent` 类型的 Hook（Claude Code 将其标记为实验性）。
F2. Skill / 子代理 frontmatter Hook（生命周期与会话绑定；组件退出时清理）。
F3. `async` command Hook 标志（即发即弃；可选的 `asyncRewake`）。
F4. `buildmax hooks` CLI 检查器与 Desktop 视图（接入 §3.4 的活动视图）。
F5. 出现初始化/维护模式时补上 `Setup` 事件。
F6. `InstructionsLoaded`、`ConfigChange`——等到出现明确价值时再做。
F7. 出现相应 UX 之后再做输出 schema 扩展（`continue`、`stopReason`、`additionalContext`、`modifiedToolInput`）。

## 11. 风险与要点

- **输出 schema 比 Claude Code 窄。** v2 只保留了 `{decision, reason}`。CC 那些额外输出字段中的每一项都意味着一个我们目前还没有的 UI/控制界面。以后可以方便地追加。
- **没有 `modifiedToolInput`。** v2 中 Hook 无法在执行前改写工具参数。这项能力跨过了一条线（Hook 改写模型意图），不应该在没有明确 opt-in 的情况下跨过去。
- **PromptDriver 的成本。** 一个 `prompt` Hook 就是一次逐事件的 LLM 调用——很容易拖慢每一轮对话。默认使用设置里最便宜的模型；在示例文件中注明这项成本。
- **HTTP 请求头里的密钥。** `headers: {Authorization: "Bearer $TOKEN"}` 只会插值每个条目 `allowed_env` 中列入白名单的环境变量，堵住了任意 `${VAR}` 展开带来的意外泄漏风险。
- **子代理的继承。** 子代理本来就会继承父级的 Hook。v2 在每个发出的事件上都盖上 `IsSubagent`/`AgentType`，方便审计 Hook 做归因。子代理 frontmatter Hook（为每个子代理单独增加 Hook）要等到 F2 中会话范围的生命周期设计完成后才会做。
- **工作区信任。** 工作区级别的 Hook 文件是随仓库一起分发的可执行配置。要把这层含义写进文档；如果后续有需要，收紧到“需要显式 opt-in 才加载工作区 Hook”是一个可能的后续动作。

## 12. 验收

v2 达到以下标准即视为成功：

- 一个工作区可以自带 Hook，让任何在该工作区内运行 BuildMax 的人自动应用这些 Hook，并叠加在用户全局 Hook 之上。
- 运维人员可以用 `http` Hook 接入单个策略服务，统一管理 CLI、Desktop 和 Worker 运行中的 PreToolUse 决策。
- 一个格式化器或审计日志记录器，不需要写定制代码就能表达为一个 `command` Hook。
- 一个基于 LLM 的判断器，只需要在设置文件里加一条 `prompt` Hook 条目就能表达。
- 审计日志能够区分主 Agent 停止、子代理停止，以及因失败而停止这几种情况。
- 审批请求通过 `Notification` 事件对 Hook 消费者可见，且不绕过既有的 `ApprovalHandler`。
- Agent 循环不会因为一个配置错误或运行失败的 Hook 而默默被打断（每种传输方式的默认行为都是失败开放）。

## 13. 建议的交付顺序

1. 阶段 A——工作区配置。改动很小，能立刻解锁项目自带 Hook。
2. 阶段 D——拆分事件 + 新增事件。用户可见收益最大的一步；不依赖 B/C。
3. 阶段 B + C——驱动注册表 + HookManager。属于重构，除了让新类型可用之外不改变行为。
4. 阶段 E——文档。
5. 阶段 F 按需推进。

A → D → B/C 的顺序，把见效快的改动和更大的重构穿插在了一起。
