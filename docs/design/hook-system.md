# Hook System v2

> **简体中文：** [阅读中文镜像](../zh-CN/design/Hook系统.md)

## Contents

- [Status](#status)
- [1. Purpose](#1-purpose)
- [2. Direction](#2-direction)
- [3. Architectural shape](#3-architectural-shape)
- [4. Configuration](#4-configuration)
- [5. Hook types (drivers)](#5-hook-types-drivers)
- [6. Event coverage](#6-event-coverage)
- [7. HookManager](#7-hookmanager)
- [8. Runtime flow](#8-runtime-flow)
- [9. Layering](#9-layering)
- [10. Implementation steps](#10-implementation-steps)
- [11. Risks and call-outs](#11-risks-and-call-outs)
- [12. Acceptance](#12-acceptance)
- [13. Suggested ship order](#13-suggested-ship-order)

## Status

- roadmap_priority: `P0.5`
- status: `implemented` — 16 events and all four transports are shipped;
  optional inspector and frontmatter integrations remain deferred
- follows: [trust-harness.md](./trust-harness.md)
- roadmap: [../ROADMAP.md](../ROADMAP.md)
- created_at: `2026-05-23`

## 1. Purpose

The predecessor hook system (shipped alongside §3.1 of the trust-harness
design) covered five events with a single shell transport and a single global
configuration location. This design expands hooks to approximate parity with
Claude Code's documented hook design:

- Workspace-scoped definitions in addition to the global `settings.yaml`.
- Multiple hook transports: `command`, `http`, `mcp_tool`, `prompt`.
- A higher-fidelity event set anchored at the points BuildMax actually owns
  (prompt submit, session lifecycle, tool result success/failure, approval
  notification, subagent start/stop, main run stop/failure, compaction).
- A central `HookManager` that mirrors the existing `MCPManager` pattern:
  owns merged configuration, holds a driver registry, exposes `Status`,
  `Refresh`, and `Close`, and implements `agent.HookRunner` so the rest of the
  runtime is unchanged.

The goal is to let users and operators define policy, formatting, audit, and
external-approval flows once and have them apply uniformly to CLI, desktop,
worker, and subagent runs.

## 2. Direction

P0.5 §3.1 introduced hooks as a foundation. v2 makes that foundation usable
for real automation use cases without taking on the full surface area of
Claude Code's documented hook product. Specifically:

- Workspace hooks are essential: project-scoped policy cannot ship today.
- Multiple transports unlock the cheapest correct tool for each job
  (formatters → command, central policy → http, LLM judge → prompt, existing
  MCP tooling → mcp_tool).
- A broader event set unlocks audit and approval flows that the current
  single `RunEnd` event cannot represent.

Anything that crosses into UX surfaces (`/hooks` inspector, async hooks, hook
output that modifies tool arguments) is deferred until P0.5 §3.4 activity
views land.

## 3. Architectural shape

v2 mirrors the existing **MCP** layout in the repo so the patterns stay
consistent across subsystems:

| Layer | MCP today | Hooks v2 |
|---|---|---|
| Domain contract | `core/llm` (Tool, ToolCall) | `core/agent/hook.go` (HookEvent, HookInput/Output, HookRunner) |
| External-system impl | `infra/mcp` (transport, registry) | `infra/hook` (one driver per type) |
| Application assembly | `agentapp/mcp_manager.go` (lifecycle, Status, Refresh) | `agentapp/hook_manager.go` (lifecycle, merge configs, dispatch) |
| Config | `config/mcp.go` + `<workspace>/.buildmax/mcp.json` | `config/hooks.go` + `<workspace>/.buildmax/hooks.yaml` |

The `HookManager` implements `agent.HookRunner` so the existing RunLoop
integration stays as-is.

## 4. Configuration

### 4.1 File layout

| Scope | Path | Format |
|---|---|---|
| Global (user) | `<BUILDMAX_HOME>/settings.yaml` (`hooks:` block — existing) | YAML |
| Workspace | `<workspace>/.buildmax/hooks.yaml` (new) | YAML |
| Skill frontmatter | `hooks:` in skill YAML frontmatter | YAML (deferred, see §10) |
| Subagent frontmatter | `hooks:` in agent def | YAML (deferred, see §10) |

**Merge rule.** Entries are concatenated per event in the order
`(global, workspace)`. Both layers run for the same event. The first hook
that returns a block decision wins for the gate, but every matching hook
still executes so observers and audit hooks see every call.

This matches the additive behavior Claude Code documents for plugin/user/
project merge.

### 4.2 Polymorphic `HookEntry`

`config.HookEntry` becomes a tagged union with a `type` discriminator and
per-type fields. Unknown types log a warning and are skipped at load time.

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

Shared keys on every entry: `type`, `matcher`, `timeout`.

Per-type keys:

- `command`: `command`, `args`, `shell` (default `bash`).
- `http`: `url`, `headers`, `allowed_env` (whitelist for `$VAR` interpolation
  inside headers and url).
- `mcp_tool`: `server`, `tool`, `input` (with `${field}` substitution from
  `HookInput`).
- `prompt`: `prompt` (with `$ARGUMENTS` placeholder for the serialized
  `HookInput`), `model` (optional override).

### 4.3 Loading

- `config.LoadSettings()` continues to load the global `hooks:` block.
- New `config.LoadWorkspaceHooks(workspace string) (HooksConfig, error)`
  reads `<workspace>/.buildmax/hooks.yaml`; a missing file returns
  `(HooksConfig{}, nil)`.
- New `config.MergeHooks(global, workspace HooksConfig) HooksConfig` performs
  the per-event concat described in §4.1.

## 5. Hook types (drivers)

| Type | Driver | Deps | Status |
|---|---|---|---|
| `command` | `infra/hook/command.go` (refactor of today's shell.go) | none | v2 |
| `http` | `infra/hook/http.go` | `net/http` | v2 |
| `mcp_tool` | `infra/hook/mcp.go` | `HookMCPCaller` (impl in agentapp wraps MCPManager) | v2 |
| `prompt` | `infra/hook/prompt.go` | `HookLLMCaller` (impl in agentapp wraps LLMClientCache) | v2 |
| `agent` | deferred (CC marks experimental) | subagent runner | future |

### 5.1 Driver contract

Drivers live entirely under `infra/hook` so the core stays pure. The
`Driver` interface and a config-mirror `Entry` struct are defined in
`infra/hook/driver.go`; `core/agent` does not import config.

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

### 5.2 Output schema

All drivers normalize to `agent.HookOutput{Decision, Reason}` for now.

- `command`: exit 0 = allow, exit 2 = block (stderr → reason), other = fail
  open. Optional JSON on stdout overrides the default decision.
- `http`: 2xx = allow, 4xx/5xx = fail open, body may be `{"decision":"block",
  "reason":"..."}`. A dedicated 422 status is treated as block when no body.
- `mcp_tool`: tool text result is parsed as JSON if it looks like a hook
  output; otherwise treated as allow.
- `prompt`: LLM response is parsed as JSON; if parsing fails, fail open.

Claude Code's broader output schema (`continue`, `stopReason`, `suppressOutput`,
`systemMessage`, `hookSpecificOutput.additionalContext`, `modifiedToolInput`)
is intentionally not implemented in v2. Each of those implies a UI/control
surface that does not yet exist. The schema is additive — these can be added
later without breaking existing hooks.

## 6. Event coverage

v2 grew the event set from 5 to 13, and the worktree lifecycle added the last
three for 16. CamelCase event names match Claude Code; YAML keys remain
snake_case per CLAUDE.md §6.1.

| Event | Anchor point | Gating? | Change |
|---|---|---|---|
| `SessionStart` | `agentapp.OpenSession` | no | new |
| `SessionEnd` | `SessionManager.Finalize`/close | no | new |
| `UserPromptSubmit` | `agentapp.RunPrompt`, before sess.Append | **yes** — block aborts the turn | new |
| `PreToolUse` | existing | yes | kept |
| `PostToolUse` | success path of `applyPolicyAndExecute` | no | kept (narrowed to success) |
| `PostToolUseFailure` | error path of `applyPolicyAndExecute` | no | new |
| `Notification` | `applyPolicyAndExecute` when action=Ask, and on PermissionDenied | no | new |
| `PreCompact` | existing | yes | kept |
| `PostCompact` | existing | no | kept |
| `SubagentStart` | `subagent_runner.RunSubAgent` entry | no | new |
| `SubagentStop` | `subagent_runner.RunSubAgent` exit (success) | no | new |
| `Stop` | `RunLoop` exit (success, main only) | no | new (replaces `RunEnd` happy path) |
| `StopFailure` | `RunLoop` exit (error path, main or subagent) | no | new (replaces `RunEnd` error path) |
| `WorktreeCreate` | `worktree.Manager` after a tree is created | no | new |
| `WorktreeRemove` | `worktree.Manager` after a tree is removed | no | new |
| `CwdChanged` | the session's workspace root moves, including into a worktree | no | new |

`RunEnd` is removed rather than aliased — it shipped in the same trust-harness
work and has no external consumers.

`HookInput` gains:

- `Prompt string` — populated for `UserPromptSubmit`.
- `AgentType string` — populated for `SubagentStart/Stop`; also stamped on
  every event when running inside a subagent so audit hooks can attribute.
- `IsSubagent bool` — distinguishes `Stop` vs `SubagentStop` without parsing
  `AgentType`.
- `NotificationKind string` — `approval_required` | `permission_denied` |
  `user_question` (an `AskUser` question is waiting; see
  [agent-user-questions.md](agent-user-questions.md)).

`WorktreeCreate`, `WorktreeRemove`, and `CwdChanged` were deferred for
depending on a feature BuildMax did not have; it has it now, and all three
ship with the worktree lifecycle in
[workspace root and worktrees](workspace-root-and-worktrees.md). All three are
advisory. The tool call that asks for a worktree already passes `PreToolUse`,
so a second gate over the same decision could only leave one half-created, and
a hook that fails never undoes a move that has already happened. `CwdChanged`
is the one to subscribe to for "where is this session working now"; the other
two say what became of the tree itself.

Still deferred (matching the gap analysis against Claude Code):
`Setup`, `UserPromptExpansion`, `SpacemateIdle`, `TaskCreated`,
`TaskCompleted`, `FileChanged`, `Elicitation`, `ElicitationResult`,
`ConfigChange`, `PostToolBatch`, `InstructionsLoaded`, `PermissionRequest`.
They are either duplicated by other events in our model, or depend on features
BuildMax does not yet have.

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

Dispatch flow inside `Run`:

1. `entries := m.cfg.Entries(in.Event)` — merged list in declared order.
2. Filter by matcher (regex on `in.ToolName`; empty matcher matches anything;
   non-tool events skip entries with a non-empty matcher).
3. For each entry, look up `m.drivers[entry.Type]`; missing driver → log +
   skip.
4. Call `driver.Run(ctx, entry, in)`.
5. Aggregate: first `HookDecisionBlock` becomes the manager's output;
   remaining entries still execute (audit-friendly).

`MCPCaller` and `LLMCaller` adapters live in `agentapp/hook_callers.go` so
`infra/hook` stays free of agentapp imports:

```go
// agentapp/hook_callers.go
type mcpCaller struct{ m *MCPManager }
func (c *mcpCaller) CallMCPTool(ctx context.Context, server, tool string, input map[string]any) (string, error) { ... }

type llmCaller struct{ cache *LLMClientCache; defaultModel string }
func (c *llmCaller) CompleteHookPrompt(ctx context.Context, model, prompt string) (string, error) { ... }
```

## 8. Runtime flow

This section walks through what actually happens at startup and at one
event. It is a reference; the contracts above are authoritative.

### 8.1 Startup — building the manager

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

The manager is one object the rest of the runtime sees as
`agent.HookRunner`. Driver polymorphism is invisible above this layer.

### 8.2 One event firing — dispatch path

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

`PreToolUse` fires after the permission layer has already allowed the call, so
a hook sees only what policy, approval, and any session grant let through. It
cannot un-deny a refusal; it is the last gate, not an override. Layering:
[tool-permissions.md](./tool-permissions.md).

Two ordering properties changed when tool calls started overlapping
([parallel-tool-execution.md](./parallel-tool-execution.md)). When a batch of
read-only calls is grouped, `PreToolUse` fires for **every** member of the group
before **any** of them executes — a hook that inspects the filesystem between
calls sees the state before the whole group, not before each call. This is
unavoidable: the calls overlap by construction. It is confined to calls a tool
declared read-only, which by that design do not write. `PostToolUse` and
`PostToolUseFailure` fire once the group joins, still in call order, so an audit
hook's sequence is unchanged at the cost of waiting for the group's slowest
member.

Gating events (`PreToolUse`, `PreCompact`, `UserPromptSubmit`) check
`out.Blocked()` and short-circuit. Advisory events
(`PostToolUse`, `Notification`, `Stop`, `SessionStart`, etc.) discard the
decision and continue.

### 8.3 A concrete turn — what fires, in order

User runs `buildmax` and submits *"please write the result to out.txt"*.
Hooks are configured; the model decides to call `writefile`.

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

Variations the same path covers:

- **Block in PreToolUse.** HTTPDriver returns
  `{"decision":"block","reason":"forbidden path"}`.
  `applyPolicyAndExecute` short-circuits to
  `error: tool call "writefile" denied by hook: forbidden path`, emits
  `EventToolDenied` (reason=`hook`), and the LLM gets the error string.
- **Tool execution fails.** Same path but `tool.Execute` returns an error.
  The advisory hook fired is `PostToolUseFailure` (not `PostToolUse`), with
  `tool_error` in the payload.
- **Approval gate.** When policy resolves to `Ask`, the manager fires
  `Notification{kind="approval_required", tool, args}` *before* invoking
  `ApprovalHandler`. On deny, `Notification{kind="permission_denied"}` fires.
- **Subagent.** The subagent runner stamps `IsSubagent=true` and
  `AgentType="<def-name>"` on every event from that subagent. Lifecycle is
  `SubagentStart → ... → SubagentStop` (or `StopFailure`), not `Stop`.
- **Compaction.** `PreCompact` (gating) can skip a compaction round; on
  success `PostCompact{summarized, kept, summary}` fires.

### 8.4 Failure modes — fail-open by design

| What fails | Result | Logged? |
|---|---|---|
| Driver missing for `entry.Type` | entry skipped | warn |
| Matcher regex invalid | entry never matches | warn |
| Command exit 2 | **block**, stderr → reason | info |
| Command other non-zero | **allow** (fail open) | warn |
| Command timeout | **allow** (fail open) | warn |
| HTTP 5xx / connection error | **allow** (fail open) | warn |
| MCP server unreachable | **allow** (fail open) | warn |
| PromptDriver LLM error | **allow** (fail open) | warn |
| PromptDriver returns non-JSON | **allow** (no decision) | debug |

A broken hook never silently breaks the agent loop. The only way a hook
stops the run is an explicit block decision.

## 9. Layering

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

The entry and config types landed in `internal/core/hook` rather than in
`internal/config` as planned, so the runtime contract does not travel through
the package that loads files.

Architecture-test compliance:

- `core/agent` does not import infra/config (unchanged).
- `infra/hook` imports `core/agent` and `core/hook` — both lower.
- `agentapp` imports `infra/hook`, `infra/mcp`, `config` — already does.

## 10. Implementation steps

Each phase is independently shippable. Tests gate each phase.

### Phase A — Workspace config

A1. Add `Type` field on `config.HookEntry` (default `"command"` if empty).
A2. New `config.LoadWorkspaceHooks(workspace)` reading
    `<workspace>/.buildmax/hooks.yaml`; missing file = empty.
A3. New `config.MergeHooks(global, workspace)` — per-event concat.
A4. Tests: workspace-only, global-only, merged, missing file, malformed file.

### Phase B — Driver registry + transports

B1. Create `infra/hook/driver.go` (`Driver`, `Entry` mirror struct,
    `MCPCaller`, `LLMCaller`).
B2. Refactor today's `infra/hook/shell.go` → `infra/hook/command.go`
    implementing `Driver`. Tests must keep passing.
B3. Implement `HTTPDriver` with `$VAR` interpolation gated by `allowed_env`.
B4. Implement `MCPDriver` using `MCPCaller`; substitute `${field}`
    references from `HookInput` into `entry.Input`.
B5. Implement `PromptDriver` using `LLMCaller`; replace `$ARGUMENTS` with
    `HookInput` JSON; parse response JSON for `{decision, reason}`.
B6. `infra/hook/driver.go`: `NewDriverRegistry(deps) map[string]Driver`.
B7. Per-driver tests with stubs for MCP and LLM callers.

### Phase C — HookManager

C1. `agentapp/hook_manager.go`: own merged config, driver registry, matcher
    cache; implements `agent.HookRunner`.
C2. `agentapp/hook_callers.go`: adapters from `MCPManager` and
    `LLMClientCache`.
C3. `NewAgentApp` builds the manager from
    `(globalSettings.Hooks, workspaceHooks)` and the deps. Replaces the
    direct `hook.NewShellRunner(...)` call.
C4. `HookManager.Status()` returns a struct usable by a future
    `buildmax hooks` inspector.
C5. End-to-end test: workspace file + global settings + multiple types fire
    correctly via a real `AgentApp`.

### Phase D — Event coverage

D1. Add new `HookEvent` constants. Extend `HookInput` with `Prompt`,
    `AgentType`, `IsSubagent`, `NotificationKind`.
D2. Remove `RunEnd` (call sites become `Stop` / `SubagentStop` /
    `StopFailure`).
D3. RunLoop: pass `IsSubagent` through `RunLoopOpts`; emit `Stop` or
    `SubagentStop` on success; `StopFailure` on error.
D4. `applyPolicyAndExecute`: split `PostToolUse` success/failure; fire
    `Notification(kind=approval_required)` before `Approval.RequestApproval`;
    fire `Notification(kind=permission_denied)` on deny.
D5. `agentapp.RunPrompt`: fire `UserPromptSubmit` before history append; if
    blocked, return reply = reason without invoking the LLM.
D6. `SessionManager`: fire `SessionStart` on `OpenSession`; fire
    `SessionEnd` from `Finalize`/close.
D7. `subagent_runner`: stamp `IsSubagent=true`, `AgentType` on
    `RunLoopOpts`; fire `SubagentStart` before RunLoop.
D8. Tests for every new event using the existing `recordingHookRunner`
    pattern.

### Phase E — Docs and DX

E1. Expand `config-examples/settings.example.yaml` with one example per
    type.
E2. New `config-examples/hooks.workspace.example.yaml` for workspace-level.
E3. Update `design/trust-harness.md`: §3.1 marks hooks ✅
    with the shipped event list.
E4. Update CLAUDE.md to point at the new events and locations.

### Phase F — Deferred follow-ups

F1. `agent` hook type (CC marks experimental).
F2. Skill / subagent frontmatter hooks (session-scoped lifetime; cleanup on
    component exit).
F3. `async` command-hook flag (fire-and-forget; optional `asyncRewake`).
F4. `buildmax hooks` CLI inspector and desktop view (ties into §3.4 Activity
    Views).
F5. `Setup` event when an init/maintenance mode lands.
F6. `InstructionsLoaded`, `ConfigChange` — when value emerges.
F7. Output-schema extensions (`continue`, `stopReason`, `additionalContext`,
    `modifiedToolInput`) when corresponding UX exists.

## 11. Risks and call-outs

- **Narrower output schema than Claude Code.** v2 keeps
  `{decision, reason}`. Each of CC's extra output fields implies a UI/control
  surface we do not yet have. Easy additive extension later.
- **No `modifiedToolInput`.** Hooks cannot rewrite tool arguments before
  execution in v2. That feature crosses a line (hooks rewriting model intent)
  we should not cross without an explicit opt-in.
- **PromptDriver cost.** A `prompt` hook is an LLM call per event — easy to
  make turns slow. Default to the cheapest model from settings; document the
  cost in the example file.
- **HTTP secrets in headers.** `headers: {Authorization: "Bearer $TOKEN"}`
  interpolates only env vars whitelisted in `allowed_env` per entry. Closes
  the accidental-leak hole that comes with arbitrary `${VAR}` expansion.
- **Subagent inheritance.** Subagents already inherit parent hooks. v2 stamps
  every emitted event with `IsSubagent`/`AgentType` so audit hooks can
  attribute. Subagent-frontmatter hooks (adding new hooks per subagent) are
  deferred until the session-scoped lifecycle in F2 is designed.
- **Workspace trust.** A workspace-level hooks file is executable
  configuration shipped with the repo. Document the implication; a stricter
  posture (require explicit opt-in to load workspace hooks) is a follow-up if
  needed.

## 12. Acceptance

v2 is successful when:

- A workspace can ship hooks that apply automatically to anyone running
  BuildMax inside that workspace, merged on top of the user's global hooks.
- An operator can wire a single policy service via `http` hooks and have it
  govern PreToolUse decisions across CLI, desktop, and worker runs.
- A formatter or audit logger can be expressed as a `command` hook without
  custom code.
- An LLM-based judge can be expressed as a `prompt` hook with one entry in
  the settings file.
- Audit logs can distinguish main-agent stop, subagent stop, and stop on
  failure.
- Approval requests are visible to hook consumers via the `Notification`
  event without bypassing the existing `ApprovalHandler`.
- The agent loop never silently breaks because of a misconfigured or failing
  hook (fail-open is the default for every transport).

## 13. Suggested ship order

1. Phase A — workspace config. Tiny, unblocks project-shipped hooks
   immediately.
2. Phase D — events split + new events. Biggest user-visible parity win;
   does not depend on B/C.
3. Phase B + C — driver registry + HookManager. Refactor without behavior
   change beyond enabling new types.
4. Phase E — docs.
5. Phase F as needed.

A → D → B/C interleaves fast wins with the larger refactor.
