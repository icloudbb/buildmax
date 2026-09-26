# Tool Permissions

> **简体中文：** [阅读中文镜像](../zh-CN/design/工具权限.md)

## Contents

- [Status](#status)
- [1. Purpose](#1-purpose)
- [2. Current Baseline](#2-current-baseline)
- [3. Gaps](#3-gaps)
- [4. Direction](#4-direction)
- [5. In Scope](#5-in-scope)
- [6. Effective Behavior](#6-effective-behavior)
- [7. Out Of Scope](#7-out-of-scope)
- [8. Implementation Steps](#8-implementation-steps)
- [9. Acceptance](#9-acceptance)
- [10. Open Questions](#10-open-questions)

## Status

- roadmap_priority: `unscheduled` — proposed as a P0.5 follow-on to
  [trust-harness.md](./trust-harness.md); not yet placed in
  [../ROADMAP.md](../ROADMAP.md)
- status: `implemented` (§8 phases 1-5 landed; open items in §7 and §10)
- follows: [trust-harness.md](./trust-harness.md),
  [sandbox-boundaries.md](./sandbox-boundaries.md),
  [hook-system.md](./hook-system.md)
- relates: [sandbox-boundaries.md](./sandbox-boundaries.md) and
  [agent-sandbox-policy.md](./agent-sandbox-policy.md) define the worker
  boundary; this record decides the in-process gate in front of every tool
  call. They meet in §5.7.
- precedes: [parallel-tool-execution.md](./parallel-tool-execution.md), which
  consumes the `Access` classification defined here in §5.1;
  [space-governance.md](./space-governance.md), which is where operator control
  over autonomous surfaces has to land — §7
- touches: `internal/core/llm`, `internal/core/agent`, `internal/tool`,
  `internal/infra/mcp`, `internal/config`, `internal/interface/cli`,
  `internal/interface/desktop`
- created_at: `2026-08-20`

## 1. Purpose

BuildMax asks the user before a tool call in exactly two situations: the path
looks sensitive, or the shell command looks risky. Everything else runs
unannounced — a `Write` over a source file, an `Edit`, a `CallMcpTool` against
a third-party server the user configured a month ago and has not thought about
since.

That is not a considered position. It is what falls out of a permission layer
that was designed and then left empty. `llm.PolicyProvider` — the interface for
"what should this tool default to" — is implemented by **no tool in the
repository**. `ToolPolicy`, the configured override above it, is a stub:
`interactivePolicy.Check` and `nonInteractivePolicy.Check` both
`return ToolActionAllow` unconditionally (`internal/agentapp/policy.go:12`,
`:21`). Of the five layers `resolveAction` walks
(`internal/core/agent/agent.go:489`), two are dead and one is answering a
different question than its name suggests.

This record fills the empty layer. It decides:

- what each builtin tool defaults to, and why that is derived rather than
  hand-assigned;
- how an MCP tool is classified, where the only available signal is a hint
  from the server being classified;
- what a surface with no human attached does with an `Ask` — the question that
  determines whether this design is shippable or a worker-breaking regression;
- where a user or an operator overrides any of it, and how either sees what is
  in force.

It also defines `Access`, the effect classification that
[parallel-tool-execution.md](./parallel-tool-execution.md) consumes to decide
which calls may overlap. That record was drafted first and carried the
classification itself; it belongs here, with permission as its first consumer.

## 2. Current Baseline

**The resolution order exists and is mostly hollow.** `resolveAction` plus
`applyPolicyAndExecute` walk five layers:

| # | Layer | Implemented by | What it decides today |
|---|---|---|---|
| 1 | `ToolPolicy.Check` | `interactivePolicy`, `nonInteractivePolicy` | nothing — both return `Allow` |
| 2 | `ArgChecker.CheckArgs` | `Read`, `Write`, `Edit`, `Grep`, `Bash` | sensitivity and command risk |
| 3 | `PolicyProvider.DefaultAction` | **nothing** | — |
| 4 | `PreToolUse` hook | `HookManager` | works; user-authored gate |
| 5 | default | — | `Allow` |

**Layer 2 answers a different question than a permission default.**
`ReadFile.CheckArgs` and `WriteFile.CheckArgs` are byte-for-byte identical
(`internal/tool/read_file.go:57`, `internal/tool/write_file.go:51`) — both
return `Ask` for a sensitive path and `Allow` otherwise. The axis is
*sensitivity*, not *effect*. A read of `~/.ssh/id_rsa` prompts; a write to
`main.go` does not.

**`Ask` has one meaning, and on half the surfaces it means `Deny`.** Interactive
surfaces pass an `ApprovalHandler` (`internal/interface/cli/tui.go:27`,
`internal/interface/desktop/app.go:163`). Print mode, the worker
(`internal/agentapp/taskrun/runtime.go:332`), eval, and the Portal conversation
runtime pass none, and `applyPolicyAndExecute` collapses `Ask` to `Deny` when
`opts.Approval == nil` (`agent.go:426`).

**The MCP surface is unguarded, and its one signal is discarded.** The LLM
reaches MCP through `CallMcpTool(server, tool_name, arguments)`, which
implements neither optional interface — every MCP call is `Allow`. Meanwhile
`serverState.toolsByName` holds `map[string]*mcpsdk.Tool`
(`internal/infra/mcp/registry.go:27`), and `mcpsdk.Tool.Annotations` carries
`ReadOnlyHint`, populated from `tools/list` on every connect. The registry
reads `.Description` and `.InputSchema` off that struct and drops
`.Annotations`. The data is already fetched and in memory; nothing reads it.

**Containment already substitutes for a prompt, in one place.** When the
sandbox is enabled in `auto_allow` mode and would contain the command,
`Bash.CheckArgs` demotes a risky command's `Ask` to `Allow`
(`internal/tool/bash.go:107`). This design generalizes the principle without
widening where it applies (§5.7).

**There is an operator file, and on the surface that needs it, it is dead.**
`config.PolicyFile` reads `<BUILDMAX_HOME>/policy.yaml`, and its doc comment
reserves the file "for other operator-controlled subsystems in later phases"
(`internal/config/sandbox.go:104`). Its purpose is a filesystem permission
boundary, not a configuration layer — a separate file is the only way to say
"the user may read these keys but not edit them", which is why
`manual/sandbox.md` frames it as *"use the policy file when the machine's
owner and the machine's user are different people."*

That framing is exactly right, and it is why the file cannot serve this design.
On a local CLI the owner and the user are the same person: they can edit
`settings.yaml` and `policy.yaml` alike, so the second file buys nothing. On a
worker — the surface where owner and user genuinely differ — `BUILDMAX_HOME`
is `RuntimeTaskRunGlobalDir`, a directory `ensureRunDirs` creates fresh with
`os.MkdirAll` on every run (`internal/agentapp/taskrun/runtime.go:222`, `:227`,
`:324`). Nothing provisions a `policy.yaml` into it — space files are
materialized into `runHome`, not `runGlobal` — so `LoadPolicySandbox` always
reads an empty config there.

The file is therefore effective only on the surface that does not need it. This
design does not use it (§5.6), and the same limitation applies to the existing
sandbox policy layer — see §10.

**`ResolveSandbox` is still the right shape to copy.** Surface default, then
settings, with a `Sources` list for display (`internal/config/sandbox.go:170`).
This record follows that layering, minus the layer that does not work.

## 3. Gaps

### 3.1 The declaration layer is empty

No tool states what it is. `PolicyProvider` has no implementations, so the
runtime cannot distinguish a file read from a file write, and layer 3 of a
five-layer resolution is dead code.

### 3.2 Writes are silent

`Write`, `Edit`, and every MCP call execute without a prompt unless the path
happens to trip the sensitivity check. For a trusted local workspace that is a
defensible default; the problem is that it is not a *default*, it is an
absence.

### 3.3 MCP is trusted by omission

A third-party server gets the same unannounced execution as a builtin file
read, and the `readOnlyHint` that would let the runtime tell the two apart is
parsed by the SDK and thrown away.

### 3.4 Adding any default would break every worker

This is the gap that decides the design. Today a worker writes files because
`WriteFile.CheckArgs` returns `Allow`. Give `Write` a category default of
`Ask`, and on a worker — no `ApprovalHandler` — that resolves to `Deny`. Task
runs stop being able to write files at all.

The inverse fix is worse. Making autonomous surfaces resolve `Ask` to `Allow`
would also promote the *risk-based* `Ask` from `Bash.CheckArgs`, so a worker
would begin executing the risky commands it currently refuses. A single
"what does `Ask` mean here" scalar cannot serve both.

### 3.5 A user has no override, and an operator has no reachable one

A user cannot say "stop asking me about `Write`" — `ToolPolicy` is the layer
for that and is a stub. An operator cannot say "no MCP calls on workers"
either, and per §2 the file that looks like it should carry that instruction
does not reach a worker at all. The user's gap is fixable here; the operator's
is not, and §7 says so rather than shipping a knob that silently does
nothing.

### 3.6 Nobody can see what is in force

`/tools` lists names and descriptions (`internal/interface/cli/chat_tools.go`).
Nothing shows the resolved action per tool, the way `buildmax sandbox status`
shows a resolved sandbox.

## 4. Direction

- **P1 — Declare effect, derive permission.** A tool states what it *does*. The
  runtime decides what to *ask*. A tool that could self-assign `Allow` would,
  eventually, and the classification would stop meaning anything.
- **P2 — `Ask` means "ask a human", and the category tier only exists where
  there is one.** A surface with nobody attached does not get a category
  prompt at all — §5.3. Risk-based `Ask` keeps today's collapse to `Deny`.
- **P3 — No silent regression.** Every builtin's effective action on every
  surface is stated in §6 and covered by a test. The shipped defaults change
  interactive surfaces only; autonomous surfaces are byte-identical.
- **P4 — MCP is untrusted by default.** `readOnlyHint` informs a prompt
  default. It never grants anything. Only an operator or user allowlist grants.
- **P5 — Containment may substitute for a prompt, where it actually contains.**
  Generalize the principle behind `Bash` auto-allow; do not widen its reach on
  faith.
- **P6 — Overridable where the override can actually land.** `settings.yaml`
  for the user, and one command that prints the resolved table with its
  sources. Operator control over an autonomous surface is server-delivered and
  space-scoped, which is P4 work, not a local file — §7.

## 5. In Scope

### 5.1 `Access`: the classification

Two questions decide what a call is, and they are not the same question:

1. **Does the call change anything the user owns?** This drives permission.
2. **Is `Execute` goroutine-safe?** This drives scheduling, and does not follow
   from the first.

`Access` answers the first. It lives in `internal/core/llm/tool.go` beside
`ArgChecker` and `PolicyProvider`:

```go
// Access describes what a tool call does to the world. The zero value is
// AccessWrite, so a tool that declares nothing — or one that returns a value
// this runtime does not recognise — is treated conservatively.
type Access uint8

const (
    // AccessWrite: the call changes durable or process state.
    AccessWrite Access = iota
    // AccessReadOnly: the call observes and returns. It writes no file, no
    // session state, and no unsynchronised process state.
    AccessReadOnly
)

// AccessDeclarer is implemented by tools that classify their own calls.
// Arguments are passed, as with ArgChecker, so the answer can depend on the
// call: a read-only shell command is a different act from a destructive one.
type AccessDeclarer interface {
    Tool
    Access(args map[string]any) Access
}
```

An undeclared tool is `AccessWrite`. No existing tool has to change for the
classification to land.

### 5.2 Deriving the default, and the two tools that prove it needs an override

The derived default is one line: **read-only → `Allow`, write → `Ask`.**

Derived rather than declared, per P1 — a tool that could name its own action
would eventually name `Allow`, and the classification would decay into a
formality.

But derivation alone is wrong for two tools, and they are worth stating
because they are the clearest evidence that permission and scheduling are
different mappings over the same fact:

| Tool | `Access` | Right permission | Right scheduling |
|---|---|---|---|
| `TodoWrite` | write | `Allow` | not parallel-eligible |
| `NoteWrite` | write | `Allow` | not parallel-eligible |

Both mutate `Session` through `NoteStoreFromContext`, and `Session.SetNotes`
has no lock (`internal/core/session/session.go:139`) — so they genuinely write
process state, and genuinely cannot overlap. But what they write is the
agent's own scratch state, not anything the user owns. Prompting a user to
approve the agent writing itself a todo would be absurd.

So the derivation is the *fallback*, and `PolicyProvider.DefaultAction` — the
interface that has been sitting empty since it was written — becomes the
*override*. `TodoWrite` and `NoteWrite` are its first two implementations, and
the reason it exists.

The resolution order becomes:

| # | Layer | Change |
|---|---|---|
| 1 | configured **deny** — a prohibition, wins outright | now real (§5.6) |
| 2 | `ArgChecker.CheckArgs` — arg-level risk | unchanged |
| 3 | configured **allow/ask** — the user's category preference | now real |
| 4 | `PolicyProvider.DefaultAction` — explicit tool default | now implemented |
| 5 | **derived from `Access`** | **new** |
| 6 | `PreToolUse` hook, then default `Allow` | unchanged |

`ToolPolicy` splits across layers 1 and 3 rather than sitting on top, and the
split is the point. `Read: allow` means "stop asking me about reads"; it must
not also mean "open `~/.ssh/id_rsa` without telling me". Only a configured
`deny` outranks the risk check. The configured preference sits above the tool's
own default because the user outranks the tool author.

**`ToolPolicy.Check` returns `(action, bool)`.** `ToolActionAllow` means
*abstain* at every other layer, so a policy that returned it could never say
"allow this, stop asking" — which is the only reason to configure one. The bool
carries whether the policy has an opinion at all. This is the same trap MCP hit
in §5.4, in a second place.

### 5.3 The surface baseline

Layer 4 — and only layer 4 — is gated on the surface having a human:

```go
// interactive reports whether a human can answer a permission prompt.
func (o RunLoopOpts) interactive() bool { return o.Approval != nil }
```

This started as a named `PermissionSurface` threaded through the five `RunLoop`
call sites, mirroring `config.SandboxSurface`. Implementation showed that was
the wrong shape: the approval handler's presence *already is* the fact. The
interactive surfaces set one and the autonomous ones do not, and
`applyPolicyAndExecute` has always read the same nil check to decide whether an
`Ask` can be answered at all. A parallel field would be a second source of
truth for one fact, and the only states it could add are incoherent — a surface
declaring itself interactive with no way to prompt. Derivation cannot drift.

On an autonomous surface the derived category `Ask` is not produced at all. It
is not "resolved to allow" and not "resolved to deny" — it never exists,
because there is nobody it could inform. Layers 1–3 still run, so a risk-based
`Ask` from `Bash.CheckArgs` still collapses to `Deny` exactly as it does today.

This is what makes §3.4 tractable. It also answers the shape of the question
rather than the symptom: a category prompt is a *conversation with a user*, and
you cannot resolve a conversation with a default. An operator who wants
workers restricted uses layer 1, where the decision is explicit and auditable —
not a scalar that silently reinterprets every `Ask` in the system.

### 5.4 MCP

`CallMcpTool` implements **both** optional interfaces, and needs both. An
earlier draft used only `ArgChecker`; implementation showed that cannot produce
the §6 row.

```go
func (t *callMCPToolTool) Access(args map[string]any) llm.Access {
    if t.readOnly(args) { return llm.AccessReadOnly }
    return llm.AccessWrite
}

func (t *callMCPToolTool) CheckArgs(args map[string]any) llm.ToolAction {
    if t.readOnly(args) { return llm.ToolActionAllow }
    return llm.ToolActionAsk
}
```

The two cover different halves, because `Allow` at layer 2 means *abstain*, not
*permit*:

| Call | Layer 2 | Layer 4 | Result |
|---|---|---|---|
| read-only, either surface | abstains | `AccessReadOnly` → no ask | allow |
| write, interactive | `Ask` | not reached | prompt |
| write, autonomous | `Ask` | gated off | deny (no handler) |

With only `CheckArgs`, a read-only call would abstain at layer 2 and then be
asked about at layer 4, because `Access` defaults to write. With only `Access`,
a write would never be refused on an autonomous surface, since layer 4 does not
run there. `Registry.ToolIsReadOnly` reads the annotation the registry already
holds and previously dropped; `LoadMcpTools` declares `AccessReadOnly`.

**`AccessReadOnly` cannot imply concurrency safety.** This is where that became
clear: `CallMcpTool` reports read-only on a third party's word, which this
runtime cannot underwrite. The interface doc now says so, and a scheduler must
require concurrency safety as a separate condition — see
[parallel-tool-execution.md](./parallel-tool-execution.md) §5.1.

Three constraints on that hint:

**Absent and false are indistinguishable.** In go-sdk v1.7.0
`ToolAnnotations.ReadOnlyHint` is a `bool`, not a `*bool`, so a server that
omits the annotation decodes identically to one that says `false`. Both land on
`Ask`. That is the safe direction, and it means a well-behaved read-only server
that simply omits the hint will prompt. The escape is the §5.6 allowlist, not a
looser default.

**The hint is the server's claim about itself.** It informs a prompt default
and grants nothing (P4). A server that lies gets `Allow` on a call that writes —
which is why the allowlist, not the hint, is the trust mechanism.

This record originally specified an approval-prompt line attributing the claim
to the server (`server github reports: not read-only`). **Not implemented.** It
needs a tool-to-prompt channel that would exist to carry one line of text, and
the prompt already shows `server` and `tool_name` from the arguments. The
question it answers — why is this being asked — is what `buildmax tools status`
(§5.6) answers directly. Revisit if users ask.

**Autonomous surfaces are unchanged by all of this.** Per §5.3 the derived
tier does not run there, and `CheckArgs` returning `Ask` for a non-read-only
MCP call collapses to `Deny` — which is a *tightening* of today's blanket
`Allow`. It is the one place this design changes autonomous behavior, it
changes it in the safe direction, and §6 states it explicitly rather than
letting it arrive as a surprise.

### 5.5 Session grants

A prompt that cannot be remembered is a prompt that gets disabled. The approval
prompt gains three outcomes:

| Choice | Key | Effect |
|---|---|---|
| allow once | `y` | this call only |
| allow for this session | `a` | grant held in memory for the scope below |
| deny | `n` / `Esc` | today's denial |

Grants live in a `SessionGrants` store, never touch disk, and die with the
process. Persisting "always allow" means writing to `settings.yaml` on the
user's behalf and is deliberately out of scope (§7) — the in-memory tier is
what makes the feature usable; the durable tier is what makes it dangerous.

**Consulted after resolution, not at layer 1.** A grant is the cached answer to
a question that was already put to the user, and a `Deny` was never a question.
Applying it ahead of resolution would let one approval walk past a policy or a
sensitivity check, so it applies only to an `Ask`:

```go
action := resolveAction(...)
if action == llm.ToolActionAsk && opts.Grants.granted(scope) {
    action = llm.ToolActionAllow
}
```

**Scope is the tool name, narrowed by the tool when it dispatches.** The name is
what the prompt showed the user, so it is what they think they approved.
`llm.GrantScoper` lets a tool that reaches somewhere else say so —
`CallMcpTool` returns `server/tool_name`, because otherwise approving one MCP
call for the session would approve every tool on every configured server, and
`BrowserNavigate` returns the URL's origin, because otherwise one approval would
admit every site the model later names. The approval prompt shows that target,
so the user sees what "allow for session" covers.

An earlier draft keyed grants on a prefix of the loop guard's
`toolFingerprint`. That was abandoned: an arg-derived key is opaque at the
prompt, so the user cannot tell what they are granting, and any prefix choice
is arbitrary. Granting `Write` for a session should not require re-approving
every path, which is exactly what a name-scoped grant gives.

**Held per session, not per run.** `AgentApp.grantsFor(sessionID)` owns the
stores. Desktop rebuilds its `SessionContext` on every message, so a grant kept
on that wrapper would not outlive the turn it was given in.

### 5.6 Configuration and visibility

One source, in `settings.yaml`:

```yaml
tools:
  permissions:
    Write: allow                    # allow | ask | deny
    CallMcpTool: ask
    "CallMcpTool:github/*": allow   # server/tool qualifier
```

`config.ResolvePermissions` layers surface default, then settings, and returns
a `Sources` list — the same shape as `ResolveSandbox`
(`internal/config/sandbox.go:170`) minus the policy layer, for the reason in
§2.

No `policy.yaml` block. Adding one would put an operator-facing knob in a file
that a worker never reads, which is worse than not offering it: the operator
writes the rule, the status command confirms it, and the worker ignores it.
Where operator control over autonomous surfaces belongs is §7.

Visibility, mirroring `buildmax sandbox status`:

```text
$ buildmax tools status
TOOL          ACCESS      ACTION  SOURCE
Read          read-only   allow   derived
Write         write       allow   settings
Bash          write       ask     derived
CallMcpTool   write       ask     derived
  github/*    read-only   allow   settings
```

The same resolved column is added to the `/tools` panel, which today shows only
name and description.

### 5.7 Where this meets the sandbox

`Bash` auto-allow (`internal/tool/bash.go:107`) is the existing instance of
P5: when the OS boundary contains the command, the prompt adds nothing, so it
is demoted. That behavior is kept exactly as it is.

It is deliberately **not** extended to `Write` and `Edit`. The sandbox's
purpose is to contain writes *outside* the workspace; a write inside the
workspace is precisely what it permits. Containment that does not contain the
act in question cannot stand in for asking about it.

Which boundary a worker runs inside is
[sandbox-boundaries.md](./sandbox-boundaries.md); Agent and Space selection is
defined by [agent-sandbox-policy.md](./agent-sandbox-policy.md). This record
assumes whatever boundary is in force and gates the call in front of it.

## 6. Effective Behavior

The P3 proof. Every builtin, before and after, on both surfaces.

| Tool | `Access` | Interactive before | Interactive after | Autonomous before | Autonomous after |
|---|---|---|---|---|---|
| `Read` | read-only | allow (ask if sensitive) | unchanged | allow (deny if sensitive) | unchanged |
| `Glob` | read-only | allow | unchanged | allow | unchanged |
| `Grep` | read-only | allow (ask if sensitive) | unchanged | allow (deny if sensitive) | unchanged |
| `Skill` | read-only | allow | unchanged | allow | unchanged |
| `WebFetch` | read-only | allow | unchanged | allow | unchanged |
| `Write` | write | allow (ask if sensitive) | **ask** | allow (deny if sensitive) | unchanged |
| `Edit` | write | allow (ask if sensitive) | **ask** | allow (deny if sensitive) | unchanged |
| `Bash` | write | ask if risky, deny if catastrophic | unchanged¹ | deny if risky | unchanged |
| `TodoWrite` | write | allow | unchanged (§5.2 override) | allow | unchanged |
| `NoteWrite` | write | allow | unchanged (§5.2 override) | allow | unchanged |
| `Task` | write, or read-only per agent type² | allow | **ask** unless the agent type is read-only | allow | unchanged |
| `LoadMcpTools` | read-only | allow | unchanged | allow | unchanged |
| `CallMcpTool` | write | allow | **ask** unless `readOnlyHint` | allow | **deny** unless `readOnlyHint` |

Four new interactive prompts, one autonomous tightening, nothing else moves.

² `Task` was a flat write here. It now answers per call, following
[parallel-tool-execution.md](./parallel-tool-execution.md) §5.7.1, which needed
to know whether a delegated run writes: a `subagent_type` whose whole tool set
declares `AccessReadOnly` — the built-in `explore`, or a user-defined agent
restricted the same way — is read-only, and the derived tier then allows it.
The two consumers agree here rather than disagreeing as they do for
`TodoWrite`: a read-only sub-agent can only reach tools that would not have
prompted on their own, and its nested loop carries no `ApprovalHandler`, so a
sensitive path inside it is denied rather than asked. Approving such a
delegation would be approving reads. Delegations to `general`, `shell`, or any
type that can reach one writing tool still prompt.

¹ `Bash` stays untouched only because it declares
`PolicyProvider.DefaultAction() = Allow`. Implementation showed the derived tier
would otherwise apply on top of its risk classifier and prompt for every `ls`
and `git status` — the fastest possible way to make a permission prompt
something people switch off. The derived tier is a fallback for tools with no
judgement of their own; `Bash` has one, and a sharper one.

`CallMcpTool` on autonomous surfaces is the one row that changes without a
human deciding it. It is stated here and called out in the changelog. It is
also the row that most wants an operator override — which per §7 does not exist
yet on that surface, so a deployment relying on non-read-only MCP calls inside
task runs has to say so through the server-delivered channel when P4 builds it.
Deny is the safe direction to be stuck on in the meantime.

## 7. Out Of Scope

- **Persistent "always allow" grants.** Writing to `settings.yaml` on the
  user's behalf; the in-memory tier ships first and tells us what people
  actually grant.
- **Changing `Bash` risk classification.** `isRiskyBashCommand` and
  `isCatastrophicBash` keep their current behavior and remain the authority for
  shell commands.
- **Operator control over autonomous surfaces.** Deliberately not solved here,
  and not solved with a local file. A worker's `BUILDMAX_HOME` is created fresh
  per run (§2), so the only channel that reaches it is the one it already uses
  for its model policy and run token: server delivery, scoped to the space. That
  is [space-governance.md](./space-governance.md) P4 work, and this record's
  §5.1–§5.4 are its prerequisite — the server has to have something to deliver
  before it can deliver it. Until then, an autonomous surface runs the derived
  defaults in §6 and nothing overrides them.
- **A permission mode switch** ("accept edits", "bypass all"). Reachable by
  setting every tool to `allow` in `settings.yaml`; a named mode is sugar and
  can wait for evidence people want it.
- **Deny as a first-class user affordance.** `deny` is in the config grammar
  because the operator needs it; a user-facing "block this tool" UX is not
  designed here.

## 8. Implementation Steps

### Phase 1 — the classification and the derivation

- Add `llm.Access` and `llm.AccessDeclarer`; implement `Access` on every
  builtin per §6.
- Implement `PolicyProvider.DefaultAction` on `TodoWrite` and `NoteWrite`.
- Add layer 4 to `resolveAction`, gated on `RunLoopOpts.interactive()`.
- Export `ResolveToolAction` and `DeclaredAccess` — the status command in
  phase 4 and the scheduler in parallel-tool-execution.md need the same answer
  the loop computes, and neither should re-derive it.
- The §6 table becomes a table-driven test, plus one that asserts every
  autonomous action equals the pre-change resolution. It is the acceptance gate
  for the whole phase.

### Phase 2 — session grants

- `SessionGrants` store, the three-outcome prompt in TUI and Desktop, layer 1
  consultation.
- Without this, phase 1 is a regression in usability for anyone who edits
  files. The two phases can land together; phase 1 must not ship alone.

### Phase 3 — MCP

- `Registry.ToolIsReadOnly` reading the annotations already in memory.
- `CallMcpTool.CheckArgs`; `LoadMcpTools` declares `AccessReadOnly`.
- Prompt text that attributes the read-only claim to the server.

### Phase 4 — configuration and visibility

- `tools.permissions` in `settings.yaml`; `config.ResolvePermissions` layering
  surface default then settings, with `Sources`.
- `buildmax tools status`; resolved action column in the `/tools` panel.

### Phase 5 — documentation

- `manual/` — a task-oriented page on what prompts and how to stop it.
- `docs/reference/configuration.md` — `tools.permissions`.
- `docs/contribute/architecture/tools.md` — `Access`, and what a tool author
  declares.
- `docs/design/sandbox-boundaries.md` — cross-reference §5.7.
- `CHANGELOG.md` under `## [Unreleased]`, appended to the end of its section,
  calling out the `CallMcpTool` autonomous tightening by name.

## 9. Acceptance

- The §6 table passes as a table-driven test across both surfaces.
- A worker task run writes files, edits files, and runs non-risky shell
  commands with no approval handler present — the pre-change transcript and the
  post-change transcript are identical.
- A risky shell command is still denied on a worker.
- An MCP call to a server advertising `readOnlyHint: true` runs unprompted; the
  same server's non-read-only tool prompts interactively and is denied on a
  worker.
- A session grant for `Write` survives subsequent calls in the run and is gone
  in the next run.
- `tools.permissions` in `settings.yaml` overrides the derived default, and
  `buildmax tools status` names the source for every row.
- `PreToolUse` still blocks a call that every earlier layer allowed.

## 10. Open Questions

- **The existing sandbox policy layer has the same defect.** §2 establishes
  that `<BUILDMAX_HOME>/policy.yaml` is unreachable on a worker, and
  `LoadPolicySandbox` is subject to it too: the operator lock-out documented in
  [sandbox-boundaries.md](./sandbox-boundaries.md) §4.1 and
  `manual/sandbox.md` works only on local CLI, where owner and user are the
  same person. That is a defect in shipped behavior, not in this design, and it
  wants its own fix — either provisioning the file into `runGlobal` or moving
  sandbox policy onto the same server-delivered channel as §7. Flagged here
  because this investigation found it; tracking it is separate work.
- **What is the right grant granularity?** Per tool is too coarse (`Write`
  anywhere for the session), per exact arguments too fine (every path
  re-approved). §5.5 proposes tool plus a stable prefix of the fingerprint; the
  prefix needs to be chosen against real transcripts.
- **Does `Task` deserve a category prompt?** §6 says yes for a type that can
  write — a subagent runs an unbounded number of tool calls, and its own gate
  is the same one, so approving the parent is approving a policy rather than an
  act. Read-only types are exempt (footnote 2), which answers the noise half of
  the question for the delegation the model makes most. What is still open is
  whether the remaining prompt should carry the agent type: it has no
  `GrantScope`, so approving one `shell` delegation for the session approves
  every later `general` one too.
- **Should the sensitivity check fold into `Access`?** Both end in `Ask` and
  the layering is what keeps them apart. Worth revisiting once layer 4 exists,
  and worth *not* doing in the same change.
