# Decision: Agent Orchestration And Task Workspace Continuity

> **简体中文：** [阅读中文镜像](../zh-CN/design/编排与连续性决策.md)

> **Audience:** contributors, product designers, and operators · **Status:** decision record — decisions taken on the points below; the orchestration model (§3) is deliberately deferred. It reconciles the records listed and, where it decides a question, supersedes their overlap.

Related and reconciled here:
[product vision](product-vision.md),
[agent execution and Task threads](agent-execution-and-task-threads.md),
[workflow runtime](workflow-runtime.md),
[assistant orchestration and workflow boundary](../proposals/assistant-orchestration-and-workflow-boundary.md),
[task workspace checkpoints](task-workspace-checkpoints.md),
[portal execution model](portal-execution-model.md), and
[session tree and agent mailbox](../proposals/session-tree-and-agent-mailbox.md).

Created: 2026-09-06

## Contents

- [1. Why This Record Exists](#1-why-this-record-exists)
- [2. What Everyone Already Agrees On](#2-what-everyone-already-agrees-on)
- [3. Orchestration Model — Deferred](#3-orchestration-model--deferred)
- [4. Workspace Continuity — Checkpoints Only](#4-workspace-continuity--checkpoints-only)
- [5. Sub-Points Settled](#5-sub-points-settled)
- [6. Terminology — Space](#6-terminology--space)
- [7. Structured Output — Implemented Foundation](#7-structured-output--implemented-foundation)
- [8. Actions](#8-actions)
- [9. Deferred And Evidence-Gated](#9-deferred-and-evidence-gated)

## 1. Why This Record Exists

Several records written within days of each other described overlapping — and in
one case contradictory — designs for how an Agent runs, how foreground chat
relates to durable execution, and how a Task keeps its files across runs. Two of
them proposed the same capability two ways. This record states the decisions so
the codebase carries one direction per question rather than a shelf of
alternatives a reader must adjudicate.

It does not reopen what the source records settled: Task plus TaskRun is the one
durable execution plane; Conversation is an optional foreground origin, never an
execution parent; Workflow is a deterministic graph over that plane, not a
second engine. Those stand.

## 2. What Everyone Already Agrees On

- **One execution plane.** Every path — a direct Agent run, an Issue or Workflow
  node, a Conversation-started Task, an API caller — creates an owning
  Team/Space-scoped Task and TaskRun and executes through the shared runtime.
  Nothing grows a second tool-calling loop, scheduler, trace, or artifact store.
- **The model proposes; the runtime decides.** A model output is data until a
  deterministic transition validates and records it.
- **Continuity is narrow, not a versioned workspace.** No workspace history,
  arbitrary rollback, change sets, branch/merge, or automatic write-back to
  shared files. One state moves forward per thread.
- **Recovery is honest.** Session and workspace restore from one named
  predecessor before the Agent starts, and every outcome is a queryable fact. A
  run never presents itself as a continuation while silently starting fresh.

## 3. Orchestration Model — Deferred

**Decision: deferred.** Whether to expose bounded Agent-to-Agent delegation, and
later whether that earns a first-class "Assistant" concept, is **not decided
now**. The [assistant/workflow boundary proposal](../proposals/assistant-orchestration-and-workflow-boundary.md)
stays under discussion; its Option C delegation experiment is not scheduled by
this record.

What already holds regardless (from the source records, unchanged): a single
strong Agent is the baseline for open-ended work; Conversation stays a
foreground surface and never a Task's owner; Workflow stays the durable
deterministic automation plane at R5. Only the *new* delegation/Assistant
question is deferred.

## 4. Workspace Continuity — Checkpoints Only

**Decision.** [`task-workspace-checkpoints.md`](task-workspace-checkpoints.md)
is the single workspace-continuity record. The competing
`task-workspace-continuity.md` is **withdrawn in full**, with **no fold-ins**:
the checkpoints design is adopted as written.

**Why checkpoints, not the alternative:**

- It is already load-bearing: `product-vision.md` names Task workspace
  checkpoints as the accepted narrow exception in the execution plane, and
  `agent-execution-and-task-threads.md` §6.4 references it as the filesystem
  half of the continuity contract. The alternative was a dangling companion.
- It is the more complete design: the `seed`/`successful`/`partial` checkpoint
  taxonomy, content-addressed storage with archive-safety rules, Kubernetes
  storage profiles, an RPO/RTO matrix, and — decisively — two subsystems the
  alternative was silent on: **Plugin environment revisions** and **`buildmax-home/`
  typed reconstruction**.

The alternative's ideas (a separate live view of current shared files, an
ignore/size policy, a prior-artifact read tool) are **not** carried over. §5.1
records the specific behavior this settles.

## 5. Sub-Points Settled

### 5.1 Shared files are frozen at seed

A Task seeds its private `workspace/` from the owner's shared files at first run
and captures that as the seed checkpoint. **Later changes to the shared files do
not flow into an already-started Task.** A continued run does not receive a
separate live read-only view of current shared files; the Task works from its
own carried checkpoint. This is the checkpoints design as written, and the
deliberate answer to the one place the two records diverged.

### 5.2 Restore failure fails closed

A Continue that names a predecessor and cannot restore its session or workspace
does not start the Agent; presenting a continuation while starting fresh is
forbidden. Degraded continuation exists only as an explicit, recorded user
recovery action. Both source records agreed on this.

### 5.3 Plugin environment and `buildmax-home/`

A Task carries an immutable Plugin environment revision; Continue uses the head,
Retry reconstructs the repeated run's base, and an autonomous install takes
effect only across a new TaskRun boundary. `buildmax-home/` is a rebuildable
projection from typed state, not a snapshotted directory. Adopted as specified
in the checkpoints record.

### 5.4 Retry returns to base, recovery outcomes are typed facts

Retry restores the repeated run's recorded base, never its partial result. Every
restore and commit outcome is a queryable per-dimension field (workspace,
session, plugin environment), not a log line.

## 6. Terminology — Space

**Decision.** The ownership and authorization boundary is named **Space**, not
Team, everywhere: domain, database, API, public identifiers, tests, fixtures,
documentation, and UI. This was a deliberate rename made under the Alpha rule
that no compatibility layer is owed.

The rename has landed: the domain package, the `space` table with `space_id`
columns, the `/api/spaces` routes and the OpenAPI exact-match test, handlers and
worker wire types, mocks, fixtures, docs, and the UI all say Space, with no
`team_id` remnant in `internal/`. It was a large but mechanical change that
shipped as its own dedicated pull request rather than folded into any other.
Older records that still read "Team" name the term that was replaced, not a
second concept.

## 7. Structured Output — Implemented Foundation

**Decision.** A provider-neutral structured-output contract in the shared Agent
runtime is a named roadmap prerequisite for Workflow's typed routes, planners,
evaluators, and richer Task results. That foundation is now implemented:
provider mappings, final-answer validation, TaskRun persistence, and a linear
Workflow step's `output_schema` consumer ship. Typed bindings and adaptive
control remain under R5. See [structured output](structured-output.md) for the
current implementation boundary.

## 8. Actions

Landed with this record (one pull request):

1. Withdraw `task-workspace-continuity.md` and remove its design-README row,
   leaving `task-workspace-checkpoints.md` as the single continuity record.
2. Add the structured-output roadmap item under R5. Its shared runtime and
   first Workflow consumer subsequently shipped; typed dataflow remains.

Tracked as separate, dedicated changes:

3. **The Space rename** (§6) — shipped in its own pull request.
4. **SSE for the Task page** — shipped. The Portal Task detail page now
   subscribes to `GET /api/spaces/{…}/tasks/{…}/stream` (layered over a poll).
   A shared pub/sub for multi-instance remains the separate R1 work and is not a
   prerequisite.
5. **The workspace-checkpoint implementation** — shipped. Continue restores the
   Task head, Retry restores the repeated run's base, and restore/checkpoint
   outcomes are recorded and surfaced. Candidate restore qualification remains
   in R2–R3 rather than reopening the implementation design.

No action reintroduces a compatibility layer, per the Alpha change rules.

## 9. Deferred And Evidence-Gated

- **The orchestration model (§3):** bounded delegation and any "Assistant"
  concept — measure value before deciding.
- **Naming:** "Assistant" vs a named Agent vs "Coordinator"; "Workflow" vs
  "Automation." Decide with the concept, not before it.
- **Session tree and agent mailbox:** the
  [proposal](../proposals/session-tree-and-agent-mailbox.md) describes a richer
  branched-workspace and automatic-resume model that the adopted narrow
  continuity deliberately does not build; its fan-out/fan-in must converge with
  Workflow's planner/map before either expands. It stays a proposal.
