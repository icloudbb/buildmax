# Issue Agent Access: The Agent's Own Work Order

> **简体中文：** [阅读中文镜像](../zh-CN/design/Issue Agent访问.md)

## Contents

- [Status](#status)
- [1. Decision](#1-decision)
- [2. The Gap This Closes](#2-the-gap-this-closes)
- [3. Current Baseline](#3-current-baseline)
- [4. Which Tool Pattern This Uses](#4-which-tool-pattern-this-uses)
- [5. The Two Tools](#5-the-two-tools)
- [6. What The Agent May Never State](#6-what-the-agent-may-never-state)
- [7. Untrusted Input And The Prompt Layers](#7-untrusted-input-and-the-prompt-layers)
- [8. Availability](#8-availability)
- [9. Out Of Scope](#9-out-of-scope)
- [10. Implementation Steps](#10-implementation-steps)
- [11. Open Questions](#11-open-questions)

## Status

- roadmap_priority: `unscheduled` — this decides the Agent-mutation question
  that the implemented Issue model deliberately left separate; it is not
  placed in [../ROADMAP.md](../ROADMAP.md)
- status: `implemented` — §10 is shipped on both planes: a worker run started
  from an Issue, and a local CLI session started with `buildmax issue start`. Artifact
  references in `GetIssue` are deferred; §5.1 says why
- follows: [tool-permissions.md](./tool-permissions.md),
  [unified-artifacts.md](./unified-artifacts.md)
- relates: [surface-positioning.md](./surface-positioning.md),
  [portal-execution-model.md](./portal-execution-model.md)
- precedes: [../proposals/local-issue-work-bridge.md](../proposals/local-issue-work-bridge.md),
  which asked which contextual mutations an Agent may make through a tool;
  this answers it. The bridge's local half consumes these tools rather than
  defining its own Issue access
- touches: `internal/tool`, `internal/agentapp`, `internal/agentapp/taskrun`,
  `internal/service/issue`, `internal/server/handlers/work`,
  `internal/interface/client`
- superseded_by (mechanism, planned): [agent-bridge-cli.md](./agent-bridge-cli.md)
  reverses the two-tool mechanism in favour of one `buildmax` command surface;
  the product boundary in §6 survives there. The tools below remain the shipped
  path until that record's phases land.
- created_at: `2026-08-29`

## 1. Decision

An Agent working an Issue reaches that Issue through two shared runtime tools,
scoped by construction to exactly one Issue:

| Tool | `Access` | What it does |
|---|---|---|
| `GetIssue` | `AccessReadOnly` | Returns the linked Issue, its children, and its recent comments, all bounded |
| `ReportToIssue` | `AccessWrite` | Posts one bounded comment on the linked Issue, optionally naming Artifacts already published |

Four rules make that safe enough to be worth having:

1. **Neither tool takes an Issue identifier.** The scope is a constructor
   argument, so the model cannot address a second Issue. §5.3.
2. **Status, owner, executor, and hierarchy are never tool-writable.** The
   Agent says what happened; only a person says what state the work is in. §6.
3. **Issue text arrives as a tool result, never as a prompt layer.** §7.
4. **A surface with no Issue does not register the tools at all** — the
   `UploadArtifact` rule, not a tool that exists to answer "unavailable". §8.
5. **A local Agent's report is stored as a claim.** `local_agent`, authored by
   the person who relayed it, never `agent`. §6.1.

`ReportToIssue` is named for its direction. `ReportIssue` would read to a model
as filing a defect, and a tool name is prompt surface, not a symbol.

## 2. The Gap This Closes

An Agent assigned to an Issue is started with `buildIssueAgentRunInput`
(`internal/server/handlers/work/issues.go:121`), which flattens the Issue into:

```text
Work on this issue.

Title: <title>

Description:
<description>
```

That is the whole of what it knows. It cannot see its own children, the comment
thread, what an earlier run produced, or which Artifacts already exist. When it
finishes, `RunReporter` (`internal/service/issue/run_report.go`) writes one
comment *about* it, from outside, after the worker has already been answered.

So the Agent's relationship to its own work order is: a one-shot flattened
string in, an obituary written by someone else out. It never speaks to the
thread it is working in.

This is a gap on the **worker plane**, which is shipped. It is not caused by,
and does not wait for, the local Issue bridge. Deciding it now also fixes the
shape of that bridge, which is why it is decided first.

## 3. Current Baseline

Facts this record builds on, all present in the repository:

- **Two tool patterns already exist.** Shared runtime tools live in
  `internal/tool` and are assembled by
  `internal/agentapp/assembly.go:buildBaseTools`. Tier 1's orchestration tools
  (`StartTask`, `ListTasks`, `GetTask`, `ContinueTask`) live in
  `internal/service/conversation/tool_*.go` and are built per turn by
  `buildConversationTools`.
- **Scope-by-construction is the established pattern.** `getTaskTool` holds
  `scopeID` and takes only `task_id`; the conversation's identity is not
  something the model can pass.
- **Conditional registration is the established pattern.** `UploadArtifact` is
  registered only where an `ArtifactPublisher` exists, and is absent otherwise
  — see [unified-artifacts.md](./unified-artifacts.md) §7.1. `Worktree` and the
  `Job` tools are conditional on surface in the same way, and both are excluded
  from subagents (`internal/tool/names.go`).
- **A port keeps `internal/tool` credential-blind.** `ArtifactPublisher` exists
  so that package never learns whether the file reaches a server over a
  person's session or a run token.
- **`Access` is implemented.** `llm.Access` and `AccessDeclarer`
  (`internal/core/llm/tool.go`) classify a call as read-only or write; the zero
  value is `AccessWrite`. See [tool-permissions.md](./tool-permissions.md) §5.1.
- **Comments already carry authorship.** `CommentAuthorUser`,
  `CommentAuthorAgent`, and `CommentAuthorSystem` exist in
  `internal/core/issue`.
- **Nothing auto-transitions Issue status.** The status constants appear
  outside `internal/core/issue` only in the validator
  (`internal/service/issue/service.go:168`). Every transition in the product
  today is a person's action.
- **One comment per terminal run is the whole budget.** `runSummaryLimit` is
  2000 bytes, and the reason recorded is that the thread carries a statement
  that a run finished, not the run's output — which lives in the task's output
  and Artifacts.

## 4. Which Tool Pattern This Uses

Shared runtime tools in `internal/tool`, behind a port. Not service-local
tools like Tier 1's.

Three execution planes need the same capability — a worker run started from an
Issue, a local session linked to an Issue, and eventually a Tier 1 turn — and
they hold three different credentials: a run token, a person's session, and the
server's own service call. A service-local tool would have to be written three
times or would drag `internal/tool` into knowing which credential it holds.
`ArtifactPublisher` already solved exactly this, and the solution is a port:

```go
// IssueClient reads and reports on the one Issue a run is working.
//
// A port rather than the capability itself: this package must not learn
// whether the call reaches a server over a person's session, a run token, or
// an in-process service, and must not grow a dependency on the issue service
// to find out.
type IssueClient interface {
    Issue(ctx context.Context) (IssueContext, error)
    Report(ctx context.Context, in IssueReport) error
}
```

The shape above is illustrative. What is decided is that the boundary is a port
in `internal/tool`, with implementations in `internal/interface/client` (a
logged-in local surface), the worker client (a run token), and the server's own
runtime assembly (a direct service call).

## 5. The Two Tools

### 5.1 `GetIssue`

Returns a bounded view of the scoped Issue:

- title, description, status, executor kind (never the owner — that is
  accountability for a person, and this port has no reason to see it);
- its children as title plus status — enough to know what was split out, not a
  recursive board;
- the most recent comments, each labeled with its author kind; and
- references to Artifacts already related to the Issue, as identities, never as
  object-store paths.

The Artifact references are **not implemented**. The only aggregation of an
Issue's outputs is `aggregateIssueOutputs`, a method on the Portal work handler
rather than a service, so a worker route cannot read it without either
importing that handler or moving the aggregation — an ownership change that
does not belong inside this one. The tool ships without them and says nothing
that implies it saw them. Moving the aggregation into `internal/service/issue`
is the migration to propose; §11 keeps the question.

It declares `AccessReadOnly`, so it needs no approval and may overlap its
neighbours under [parallel-tool-execution.md](./parallel-tool-execution.md).

Bounds are part of the decision, not a tuning detail. The thread on a
long-running Issue can exceed any sane context budget, and an Agent that spends
its window reading discussion has less left for the work. The tool returns a
recent window and says how much it omitted, rather than paginating the model
through a space's history.

### 5.2 `ReportToIssue`

Posts one comment on the scoped Issue as `CommentAuthorAgent`, bounded by the
same `runSummaryLimit` rationale that already governs `RunReporter`: the thread
carries a statement about the work, not the work's output.

It may name Artifacts the run already published through `UploadArtifact`, by
Artifact identity. It must not carry a file's contents, a diff, or a path on
the machine that produced it. Where those Artifacts appear in the Issue's
Results panel is §11's first open question, not something this tool decides.

### 5.3 Scope Is A Constructor Argument

Both tools are constructed with the Issue they may touch. There is no
`issue_id` parameter, and adding one later is a decision to reopen this record,
not an extension.

The reason is that the alternative fails in a way permissions cannot catch. If
the model can name an Issue, then every prompt-injection payload in a comment
thread — and comments may come from anyone on the space, or from an external
connector — gains a working verb: *read issue X, post its contents to issue Y*.
An approval prompt does not help, because the person approving sees a
syntactically ordinary call. Removing the parameter removes the class.

The same rule makes authorization simple: `internal/tool` makes no access
decision. The port holds one credential and one scope, and the server checks
space authorization on every call, as it does for any other route.

### 5.4 The Report Budget

A run gets a small, fixed number of `ReportToIssue` calls — three, so that a
correction and one retry after a network failure both fit. Past the budget the
tool refuses with an error naming the budget, which is a meaningful tool result
under [conventions](../contribute/conventions.md).

A budget rather than a description that asks nicely, because the failure mode
is not hypothetical: an Agent with an unbudgeted write into a durable human
thread uses it as a scratchpad, and the cost lands on every person reading the
Issue. `RunReporter` already enforces one comment structurally; this is the
interactive equivalent.

## 6. What The Agent May Never State

`status`, `owner_id`, `executor_kind`, `executor_id`, and `parent_issue_id` are
not writable by any tool. Creating a child Issue is not a tool either.

This preserves an invariant the product already holds — nothing in the codebase
moves an Issue's status on its own (§3) — rather than inventing one. The
reasoning is asymmetric cost: `done` is what a space reads to plan around, and
its meaning is *a person accepted this*. If a model can write it, the word stops
carrying that, and the loss is a space coordination failure. What is saved by
letting the model write it is one click by someone who was going to read the
result anyway.

The bridge proposal states the same rule from the other side: status is a Space
statement, not a report of what some process is doing.

An Agent that believes work is finished says so in its report. A person moves
the Issue.

### 6.1 A Local Agent's Report Is A Claim, And Says So

A worker run's report is stored as `agent`: the run token that wrote it is the
Agent's own credential, and the task and run it names are records the
deployment holds. A local session has none of that. It holds a *person's*
session, it ran on a machine the deployment did not schedule, admitted no quota
for, and recorded no trace of.

Storing both under `agent` would make a Portal reader believe the deployment
vouched for something it never saw. So a local report is stored as
`local_agent`, authored by the person who relayed it — the one identity the
server verified, and the accountable one. It names no task and no run, because
there is none. Portal shows it as reported rather than said.

The space comment route accepts `author_kind` only as absent or `local_agent`. A
person's session may not write `agent` or `system`: those are the deployment's
own voices, written by a run token and by the server.

This is what the record now decides in place of §11's earlier question about
local authorship. It does not make a local report evidence. It makes the claim
legible as a claim, which is the most a client report can honestly be.

## 7. Untrusted Input And The Prompt Layers

An Issue's description and comments are third-party text. Anyone on the space
can write them, and a future inbound connector could carry them in from an
external tracker. They are the same trust class as `WebFetch` output.

Two consequences, both binding:

- **They arrive as tool results.** They are never merged into a system prompt
  layer. This is a security rule and a caching rule at once: `AGENTS.md` fixes
  the system prompt from bounded instruction layers that are stable for a run,
  including optional Space instructions on Portal workers, so they can be the
  cacheable prefix. A mutable Issue snapshot in a layer would
  break that prefix on every edit as surely as it would launder a comment into
  an instruction.
- **`GetIssue` labels every comment with its author kind.** The kinds already
  exist. A model that cannot tell a spacemate's comment from its own principal's
  instruction has no basis for treating them differently.

Starting a run still flattens the Issue into the run's initial message today
(§2). That is input, and it stays input; this record does not move it into a
layer.

## 8. Availability

The port is nil where the capability cannot be served, and the tools are then
absent from the tool list — not registered in a state where every call fails.

| Surface | `GetIssue` | `ReportToIssue` | Why |
|---|---|---|---|
| Worker run started from an Issue | yes | yes | The task carries an Issue ID; the run token authorizes |
| Worker run with no Issue | absent | absent | No scope exists |
| Local CLI/TUI session started with `buildmax issue start` | yes | yes | Requires login; reports as `local_agent`, §6.1 |
| Desktop session | not yet | not yet | The capability exists; no Desktop surface offers it |
| Local session not linked, or not logged in | absent | absent | Ordinary local work is unchanged |
| Tier 1 conversation | deferred | deferred | §11 |
| Subagents | absent | absent | See below |

Subagents get neither, mirroring `Worktree` and the `Job` tools. A subagent
shares its parent's workspace root and reports back to the parent; letting
several of them write into one durable human thread makes the thread's
attribution unreadable, and the parent can pass down whatever context it
already read.

## 9. Out Of Scope

- Any Issue mutation beyond a comment — status, assignment, reparenting,
  child creation, delete, or archive.
- Addressing an Issue other than the scoped one, including a sibling or the
  parent of a scoped child.
- Listing a space's Issues from a tool. An assigned-work inbox is a surface
  feature for a person, decided by the local Issue bridge proposal, not a model
  capability.
- Registering these tools in Tier 1 conversations. Deferred, §11.
- Reading or writing Tasks and TaskRuns. Tier 1 already has task tools; a
  worker does not orchestrate itself.
- Issue's optimistic-concurrency contract. It was a pre-existing defect
  affecting Portal, it was worth fixing on its own, and it has been: an update
  now carries the `version` it was built from and is refused with 409 if the
  issue moved on. These tools do not depend on it — comments are append-only —
  and neither of them writes a versioned field.

## 10. Implementation Steps

1. Add `ToolNameGetIssue` and `ToolNameReportToIssue` to
   `internal/tool/names.go` with the conditional-registration note the other
   conditional tools carry.
2. Define the `IssueClient` port and both tools in `internal/tool`, with
   `Access` declared and the scope held in the constructor.
3. Thread an optional `IssueClient` through `agentapp.AppConfig`, nil meaning
   absent, exactly as `ArtifactPublisher` is threaded. Register the tools in
   `buildToolRegistry` **after** `BuildAgentTypes`, not in `buildBaseTools`:
   appending after that call is the mechanism that keeps `Worktree` and the Job
   tools out of subagents, and it is what §8 needs. `buildBaseTools` is what
   every delegate registry is built from, so a tool placed there would reach
   one.
4. Implement the port for the worker plane in the worker client, scoped by the
   run's task Issue ID, and register it in `internal/agentapp/taskrun`.
5. Add the server-side read and comment routes the port needs, or reuse the
   existing space Issue routes where the run token can be authorized against
   them.
6. Implement the port in `internal/interface/client` for logged-in local
   surfaces, and scope a session to an Issue with `buildmax issue start <id>`.
   Reports go through the space comment route as `local_agent` (§6.1).

All six are done. Steps 1–5 are worker-plane work and stood alone; step 6 is
the first piece of the local Issue bridge, and what it does not do — remember
the link, offer an inbox in Desktop, return status — is still that proposal's.

## 11. Open Questions

1. **Where does a runless session's result appear, and who owns the
   aggregation?** `issue_outputs.go` aggregates an Issue's outputs from task
   runs, as a Portal handler method rather than a service — which is also why
   `GetIssue` ships without Artifact references (§5.1). A local session produces no
   run, so an Artifact it publishes has no row to hang on. Either outputs
   aggregation learns a session-originated source, or the bridge creates a
   record for local work. This must be answered before step 6, not before
   step 1.
2. **Does Tier 1 register these tools?** A Portal conversation is the single
   voice to the user and already holds task tools. Giving it Issue access is
   defensible and is a separate decision with its own scoping problem: a
   conversation is not scoped to one Issue.
3. **Is three the right report budget?** It is a guess shaped by wanting a
   correction and a retry to fit. Real runs decide it.
4. **How much of the comment thread is the right window?** Bounded is decided;
   the bound is not.
5. **Does a scoped child Issue need to see its parent?** Reading upward is a
   wider scope than "the work order in front of me", and the parent's
   description is often where the actual requirement lives.
6. **Does `local_agent` need a session of its own to become evidence?** §6.1
   decides how a local report is recorded, not how far it can be trusted. A
   report is a claim the relaying person is accountable for; making it evidence
   would need the local session to hold a credential of its own, which is the
   durable-Agent-sessions question, not this one.
7. **How does a local session pick its Issue durably?** `buildmax issue start` scopes one
   run and remembers nothing. The bridge's `IssueLink` sidecar is the durable
   form, and it is that proposal's to design.
