# Portal Execution Model: Tier 1, Tier 2, And What Carries Them

> **简体中文：** [阅读中文镜像](../zh-CN/design/Portal执行模型.md)

## Contents

- [Status](#status)
- [1. Decision](#1-decision)
- [2. What Divides Tier 1 From Tier 2](#2-what-divides-tier-1-from-tier-2)
- [3. Transcript And Execution Events Stay Distinct](#3-transcript-and-execution-events-stay-distinct)
- [4. What Shipped](#4-what-shipped)
- [5. What Is Deferred, And Why](#5-what-is-deferred-and-why)
- [6. Models Not Recommended](#6-models-not-recommended)
- [7. Rejected Alternatives](#7-rejected-alternatives)
- [8. Questions Still Requiring Evidence](#8-questions-still-requiring-evidence)

## Status

- roadmap_priority: `P2 follow-on`
- status: `partially superseded specification` — §5.1's target (Space-owned
  Task, `conversation_id` optional) has shipped, and the §4.2 result-delivery
  mechanism it describes has been removed rather than kept transitional. The
  surviving outcome-projection rationale remains current. The mandatory Tier
  1-to-Tier 2 hierarchy and Conversation-owned Task shape are superseded by
  [agent execution and Task threads](agent-execution-and-task-threads.md),
  which is authoritative for the current execution model; this record is kept
  for the Tier 1/Tier 2 boundary reasoning in §1-§3
- follows: [product-vision.md](./product-vision.md),
  [surface-positioning.md](./surface-positioning.md), and
  [agent-execution-and-task-threads.md](./agent-execution-and-task-threads.md)
- roadmap: [../ROADMAP.md](../ROADMAP.md)
- created_at: `2026-08-23`

Opened as a proposal on 2026-08-22 and accepted; this record replaced it. Its
outcome-projection decision remains current; its result-delivery decision does
not — see §4.2. The later Agent execution design rejects its mandatory
hierarchy: Conversation is an independent foreground caller, while Space-owned
Task and TaskRun carry direct and Conversation-originated Agent execution
alike.

Current schema is in
[../contribute/architecture/data-model.md](../contribute/architecture/data-model.md)
and the request path is in
[../contribute/architecture/server.md](../contribute/architecture/server.md);
this record keeps the reasoning.

## 1. Decision

This section describes the implemented model that produced the shipped outcome
and delivery work. For new execution paths, the later
[Agent execution and Task threads](agent-execution-and-task-threads.md) record
is authoritative: Tier 1 is optional, a direct Agent run creates Task and
TaskRun without Conversation, and result presentation does not require Tier 1
synthesis.

Four things, described separately because conflating them is what produced the
defects this record closes:

1. **Tier 1 — foreground orchestrator agent.** Stays in contact with the user
   and owns interaction, intent, bounded foreground work, decomposition,
   executor selection, dispatch, and result synthesis.
2. **Tier 2 — execution agent plane.** Focused background work. Today one
   general-purpose agent; the shape allows specialized ones later without a
   Portal-only runtime.
3. **Persistent execution substrate.** Task, TaskRun, the scheduler, and workers
   carry Tier 2 and persist status, output, artifacts, traces, usage, and
   failure. They are not agents.
4. **Outcome projection.** Conversation task cards, issue results, and
   notifications are *derived from* durable state. Their existence must not
   depend on a live browser connection or a successful extra model call.

Point 4 is the load-bearing one. Most of what was wrong came from a projection
that was manufactured at announcement time instead of derived from a record.

## 2. What Divides Tier 1 From Tier 2

Not difficulty, and not model strength. Execution properties:

| Dimension | Tier 1 foreground | Tier 2 background |
|---|---|---|
| User relationship | Answers inside the current interaction | Continues with nobody connected |
| Time | Predictable short latency budget | May take an extended period |
| Durability | Needs no independent retry or cancel | Must be durable, retryable, cancelable |
| Isolation | Lightweight shared foreground policy | Its own worker, workspace, resource policy |
| Specialization | General interaction and coordination | Specific tools, model, memory, or agent |
| Parallelism | A few operations inside one turn | Several independent parallel tasks |

Dispatching to Tier 2 does not mean Tier 1 could not do the work. It means the
work's execution properties do not fit a foreground turn.

Tier 1 must never: guarantee durable delivery, use model memory as the source of
task state, guess completion without structured state, relabel worker output as
a user message, become the mandatory path for task creation from Issue,
Workflow, or webhook, or decide whether a result card exists by whether its own
summary succeeded. Each of those was true at some point and each produced a bug.

## 3. Transcript And Execution Events Stay Distinct

User messages and assistant replies are the transcript. A task card is a
structured execution projection. A card does not masquerade as a message, and a
message is not synthesized to carry execution state.

They are merged for display by `created_at`, with a stable type priority for the
same second — a task after a message, because the turn read the message and then
started the task. If a strict total order is ever needed, a per-conversation
sequence can be designed then; the first task card does not justify an event
store.

Content types stay distinct by trust level:

| Content | Producer | User instruction | In LLM history by default |
|---|---|---:|---:|
| User message | User | Yes | Yes |
| Normalized instructions | Tier 1, server-validated | Derived | Only in the target worker run |
| Worker output | Tier 2 agent and tools | No | No; untrusted data for the presenter |
| Task state, usage, trace refs | Runtime | No | Through structured tools |
| Presenter summary | Presenter model | No | May persist as an assistant reply |

Worker output is never replayed as `role=user`. A presenter cannot start or
continue a task without a separately authorized orchestration turn.

## 4. What Shipped

### 4.1 Outcome Projection Is Derived, Not Announced

A conversation carries one card per task it started, read from the tasks route
and reloaded on every invalidation the socket reports. Status, output, files,
run details, stop, and run again. The cards survive a refresh, a dropped socket,
and a summary that never arrives.

A terminal run broadcasts `task.status.changed` to every connection on the
space — an invalidation, not the outcome. It used to pick the creator's first
socket, which announced nothing when they had none and told one tab when they
had three.

The transcript then excluded the system channel, so a `[Task Result]` message
was no longer drawn as the user's own. Once a terminal run stopped writing that
message (§4.2), nothing wrote the system channel at all, and it was deleted
with its transcript filter and the tool gates that recognised it.

### 4.2 Delivery Is Durable — Superseded

This subsection described the original mechanism: a row (`task_result_delivery`)
per run, claimed so two servers could not present one run twice, retried by a
sweep, abandoned after a bounded number of attempts with the reason kept, with
the presentation text derived from the run on each attempt rather than stored.

That mechanism has been removed, not kept as a transitional path. A terminal
run now only broadcasts an invalidation event (`task.status.changed`); nothing
enqueues a presentation attempt, retries one, or requires a foreground model
call before a result is durable or visible. The boundary this subsection
argued for is now structural rather than a retry policy: **the TaskRun result
is durable without a sentence about it**, because the Conversation task card
reads `task_run` directly and no code path ever needed to write one. See
[agent execution and Task threads §4](agent-execution-and-task-threads.md#4-supported-entry-paths)
and §12.

### 4.3 Intent Is Traceably Connected To Execution

`task_run.source_message_id` names the conversation message a run was asked for
in. Each run records its own: a task's first run names the message that created
it, a continuation names the message that asked for it. It is bound per turn
rather than passed per tool call, so the model cannot choose or omit what its
work is attributed to.

`GET /api/spaces/{space_id}/task-runs/{task_run_id}` quotes that message next to
the instruction the worker was given. They are different texts and that is the
point: a constraint missing from the instruction is either one the model dropped
or one the user never gave, and nothing else can tell those apart.

### 4.4 Which Definition Ran Is Recorded

`task_run.agent_revision` names the agent definition a run was served.
Instructions are resolved when a worker asks for its run, so editing an agent
changes what its next run does — intended, and the reason the number is needed:
two runs of one task can execute different text. The first write wins, so an
edit during a run cannot rewrite what that run was given.

**Selection source needed no column.** Every site that picks an agent pairs
one-to-one with a distinct `trigger_source`: Tier 1's `StartTask` is
`portal_conversation`, the task route is `portal_task_create`, an issue run is
`issue_agent_run`, a workflow step is `workflow_step`. A second record of the
same fact would only give it a way to drift. It stops being derivable the first
time one trigger admits two ways of choosing — a conversation run where the user
pins an agent and Tier 1 passes it through — and that is when to add it.

### 4.5 Synthetic Conversations Are Gone, Not Just Hidden — Superseded

A workflow step and an issue agent run used to each create a conversation
because Task required one, hidden from `ListConversationsBySpace` by a channel
filter. Neither creates one any more: both create a Space-owned Task directly
(§5.1). The `workflow` and `issue_agent` synthetic channels, `SyntheticChannels()`,
and the list filter that excluded them have been deleted rather than left in
place with nothing to produce them.

## 5. What Is Deferred, And Why

### 5.1 Conversation Was The Mandatory Parent Of Every Execution — Shipped

This was deferred here as not a nullable-column change: it needed
`ConversationID` addressed in worker directories and object-storage keys,
artifact handlers that authorized through Conversation, session restoration,
and API paths that assumed Conversation. That cutover has since shipped in one
change, as the deferral note said it had to: `task.space_id` is required and
authoritative for ownership, `task.conversation_id` is a nullable origin
relation, run storage is addressed by space/task/task-run rather than creator
and Conversation, an issue run and a workflow step each create a Task directly,
and Task/TaskRun/Artifact/trace/model-call authorization all resolve through
`task.space_id`. See
[agent execution and Task threads §13.1](agent-execution-and-task-threads.md#131-ownership-cutover).

### 5.2 The Tier 2 Catalog

Which capability, tool, input/output-contract, and execution-profile fields an
agent definition should carry is **evidence-gated**. Existing definitions and
usage should first establish which categories are stable. Writing the fields now
would be inventing demand.

Do not predefine `ResearchAgent`, `CodingAgent`, or similar Go types before that
evidence exists.

What already works: Tier 1 recommends an agent from the space's summaries via
`StartTask`, and Workflow, Issue, and the task route each pin one explicitly.
Space membership is validated; model and execution policy are not.

### 5.3 Tier 1's Foreground Budget And ExecutionSpec

Foreground time, tool, permission, and resource budgets, the context Tier 1 may
read, and a validated ExecutionSpec are all open.

Deferred safely because Tier 1 is already bounded in practice: ten iterations, a
non-interactive policy, and a small set of task and workflow tools with no
filesystem or shell access. It starts and observes durable work — background
tasks, and published workflow runs it may list, start, and read — but cannot
touch the filesystem or shell, and cannot author a workflow (see
[assistant orchestration and the workflow boundary §9.5](../proposals/assistant-orchestration-and-workflow-boundary.md#95-invoking-a-callable-unit-from-the-conversation-surface)).
The budget becomes a **prerequisite** the moment Tier 1 is allowed to read space
files, issues, and results — not before.

Attachments and context references are also unrecorded. `source_message_id` is
the anchor they would hang from, and it exists now.

### 5.4 A Structured Summary During Tier 2 Execution

Generating the summary inside the worker run would save a model call. It stays
an evaluation rather than a decision: nobody has measured that call as a cost.

### 5.5 Product Language

Defining Tier 1 and Tier 2 as foreground and background tiers separated by
lifecycle, and describing Task, TaskRun, scheduler, and worker as infrastructure
rather than another agent type, is naming work with no behavior attached. It
belongs after §5.1, which changes what those objects are.

## 6. Models Not Recommended

- copying complete output into a `run_outcome` row for every terminal run;
- conversation-level task status separate from TaskRun state;
- encoding a task card as a Markdown assistant message;
- one general polymorphic event table for every Portal object;
- automatically promoting every task into an issue.

## 7. Rejected Alternatives

| Alternative | Why not |
|---|---|
| Keep the original Tier 1 → Tier 2 path unchanged | Offline reply loss, untraceable rewriting, a mandatory extra model call, synthetic conversations |
| Merge into one synchronous agent | Long work blocks the conversation; a browser's lifetime leaks into execution; durable retry, cancel, and isolation are lost |
| Suspend and resume a single agent run | Needs durable continuation, checkpoints, and cross-process streaming far beyond this problem. Kept as later research |

## 8. Questions Still Requiring Evidence

- **Which foreground capabilities Tier 1 should have.** Which requests users
  expect answered in the conversation rather than dispatched.
- **Whether stable specialized Tier 2 agents already exist.** Usage should name
  the categories, not this document.
- **Whether users want automatic summaries at all**, or prefer the card and an
  on-demand explanation. This decides how much §4.2 is worth.
- **The product relationship between Conversation and Issue** — when a chat
  request should become tracked work.
- **Whether a strict per-conversation timeline order is necessary**, or whether
  `created_at` with a type priority is enough. So far it is enough.
