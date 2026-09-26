# Agent Execution And Task Threads

> **简体中文：** [阅读中文镜像](../zh-CN/design/Agent执行与Task线程.md)

> **Audience:** contributors, product designers, and operators · **Status:** in progress — the ownership cutover (§13.1), direct Agent admission (§13.2), the Task thread backend and Portal Task page (§13.3), and synthetic-Conversation removal (§13.4) have shipped, each with MySQL contention or browser evidence. §14 tracks exactly what is verified and what remains open, item by item; do not duplicate that list here.

Related: [product vision](product-vision.md),
[surface positioning](surface-positioning.md),
[Portal execution model](portal-execution-model.md),
[local session storage](local-session-storage.md), and
[server architecture](../contribute/architecture/server.md).

Created: 2026-09-02

This record is the synthesis of the retired two-tier Agent architecture
roundtable (see [proposals index](../proposals/README.md); git history holds
its four position papers, retired with this record). §1's decision — direct
execution when the user or product already selected the capability, admitted
through an immutable Task/TaskRun authorization boundary — is that
roundtable's own contrarian-view recommendation, evaluated and shipped. It
does not answer the roundtable's wider open questions: explicit Agent
principals and grants, a topology-neutral work substrate, or governed
peer/blackboard coordination. Those were left open when the roundtable
retired and are out of scope here.

## Contents

- [1. Decision](#1-decision)
- [2. The Current Coupling](#2-the-current-coupling)
- [3. Domain Boundaries](#3-domain-boundaries)
- [4. Supported Entry Paths](#4-supported-entry-paths)
- [5. Task Is The Agent Execution Thread](#5-task-is-the-agent-execution-thread)
- [6. Continue, Retry, And New Task](#6-continue-retry-and-new-task)
- [7. Conversation Is An Independent Foreground Surface](#7-conversation-is-an-independent-foreground-surface)
- [8. Data, API, And Storage Shape](#8-data-api-and-storage-shape)
- [9. Agent Revision And Runtime Resolution](#9-agent-revision-and-runtime-resolution)
- [10. Authorization And Trust Boundaries](#10-authorization-and-trust-boundaries)
- [11. Portal Experience](#11-portal-experience)
- [12. Failure, Recovery, And Concurrency](#12-failure-recovery-and-concurrency)
- [13. Implementation Sequence](#13-implementation-sequence)
- [14. Verification](#14-verification)
- [15. Rejected Alternatives](#15-rejected-alternatives)
- [16. Deferred Questions](#16-deferred-questions)

## 1. Decision

An Agent definition can be invoked directly. Direct invocation creates a
Space-owned Task and its first TaskRun, then uses the existing scheduler, worker,
and shared Agent runtime. It does not create a Conversation and does not ask a
foreground Conversation model to select the Agent the user already selected.

A Task is the durable interaction thread for one Agent objective. Its TaskRuns
are the ordered execution turns and attempts within that thread. After a run
finishes, the Task page presents the prior inputs and outputs as a chat-like
history and accepts another user input. Submitting that input continues the same
Task by creating a new TaskRun and restoring the Task's Agent session.

A Conversation remains an independent Portal foreground capability. It may
answer directly or create an Agent-backed Task when semantic routing,
decomposition, or detached execution is useful. A Conversation is an optional
origin and presentation surface for a Task, never the Task's owner, execution
container, authorization parent, storage namespace, or mandatory trigger.

The dependency direction is therefore:

```text
Agent page / Issue / Workflow / API
                 |
                 v
          Task + TaskRun
                 |
                 v
        Scheduler + Worker
                 |
                 v
       Result + Artifacts + Trace

Conversation --------------------> Task + TaskRun
     optional origin                same execution plane
```

The reverse dependency does not exist. A direct Agent run does not return to a
Conversation in order to start, continue, finish, or become visible.

## 2. The Current Coupling

The current Portal Agent action is not a typed execution request. It:

1. creates a Portal Conversation;
2. serializes the chosen Agent's id, description, and instructions into a user
   message;
3. asks the foreground Conversation Agent to call `StartTask`; and
4. relies on that second model to pass the intended `agent_id` into Task
   creation.

This turns an already known selection into natural language and asks a model to
recover it. It adds latency, token cost, instruction duplication, and another
failure point. It can also drift from the selected Agent and places Agent
instructions in user-message history even though the worker later receives the
same instructions as a system-prompt layer.

The schema makes the coupling deeper than the Portal route:

- `task.conversation_id` is non-null;
- Task reads join Conversation as a required parent;
- Task creation must resolve a Conversation before it can write the Task;
- Issue Agent runs and Workflow runs create synthetic Conversations only to
  satisfy that requirement;
- Task authorization still reaches through Conversation on several routes;
- worker directories and object-store keys include creator and Conversation;
  and
- result delivery assumes every TaskRun owes a sentence to a Conversation.

The worker does execute the shared Agent runtime directly once a TaskRun
exists. The defect is not a second Agent loop inside Conversation. The defect is
that admission, ownership, authorization, storage, navigation, and delivery all
treat an interaction origin as the execution parent.

## 3. Domain Boundaries

The stable objects have separate reasons to exist:

| Object | Owns | Does not own |
|---|---|---|
| Agent | Reusable identity, description, instructions, revision, and declared runtime policy | One user's history, one execution's state, or a mutable workspace |
| Task | Space-owned objective, selected Agent identity, durable Agent session lineage, and user-visible thread lifecycle | One attempt's mutable execution state |
| TaskRun | One input, one execution attempt, the exact revision and policy used, status, usage, output, trace, and artifacts | Permanent Agent identity or cross-run ownership |
| Conversation | Foreground messages, participants, short interactive turns, and optional Task projections | Worker lease, Task state, Agent session, or execution authorization |
| Issue | Shared work and result context | Private Agent history or worker lifecycle |
| Workflow | Deterministic plan, step progression, and completion policy | A mandatory Conversation or implicit model-owned state machine |

Agent being directly executable does not mean an Agent row becomes a running
process. One Agent can serve many users and many Tasks concurrently. Every
invocation still receives an explicit Task and TaskRun envelope so cancellation,
retry, quota, trace, artifacts, and audit retain one authoritative owner.

Space is authoritative for every Portal execution resource. Conversation,
Issue, Workflow step, webhook, and schedule are typed origins or result
destinations within that Space.

## 4. Supported Entry Paths

### 4.1 Direct Agent Run

When a user selects an Agent and supplies an input, the system already knows
both the executor and objective. No semantic router is needed.

```text
User selects Agent + enters input
                 |
                 v
Task application service validates Space, Agent, quota, and input
                 |
                 v
Task + first TaskRun are committed atomically
                 |
                 v
Scheduler -> Worker -> shared Agent runtime
```

The response identifies the Task and first TaskRun. Portal navigates to the
Task page immediately and observes durable status there.

### 4.2 Issue Agent Run

An Issue assigned to an Agent creates a Task directly. The Task records the
Issue as an origin and result projection target. It does not create a hidden
Conversation.

### 4.3 Workflow Step

A Workflow step creates a Task directly with the step's snapshotted Agent
selection and instructions. Workflow advancement reacts to durable TaskRun
state. It does not need a synthetic Conversation to hold the Task.

### 4.4 Conversation-Started Task

A Conversation may call the same Task application service through its bounded
tools. It supplies its own id and source message as optional provenance. The
created Task is otherwise identical to one started from the Agent page.

The Conversation may select an Agent only when the user did not already make a
typed selection or when the user explicitly asked it to coordinate. A client
that supplies `agent_id` must not encode it into prose for another model to
interpret.

### 4.5 API, Webhook, And Schedule

Non-conversational callers create Space-owned Tasks through the same service.
Each records a typed trigger source and the caller identity available at its
boundary. None invents a Conversation for storage or authorization.

## 5. Task Is The Agent Execution Thread

A Task is more durable than one background job. It is the stable thread for an
objective carried by one Agent identity:

```text
Task
  agent_id
  stable session_id
  objective and title
  optional origin relations
  |
  +-- TaskRun 1: initial input -> output
  +-- TaskRun 2: follow-up input -> output
  +-- TaskRun 3: follow-up input -> output
```

Each TaskRun is independently scheduled, bounded, cancelable, metered, traced,
and terminal. A Task consumes no worker while it waits for the next user input.
The next TaskRun can execute on any eligible worker after restoring the durable
session state.

The Task page's chat history is a projection of execution facts:

| Display item | Source |
|---|---|
| User turn | `task_run.input` plus actor and creation time |
| Agent turn | terminal `task_run.output`, status, and end time |
| Running state | current TaskRun state and streamed delta |
| Files | Artifacts explicitly published by that TaskRun |
| Details | Trace, model-call usage, runtime revision, policy, and failure data |

This projection does not copy TaskRun output into a Conversation message table.
It also does not expose hidden model reasoning. Tool calls and diagnostic
events remain in the trace and are shown only through explicit detail views.

## 6. Continue, Retry, And New Task

The three operations are different product intents and remain different domain
operations.

### 6.1 Continue

Continue accepts new user input on an existing Task and creates a new TaskRun.
It retains:

- Task and Space ownership;
- Agent identity;
- Task session lineage;
- prior model-visible session history, subject to compaction;
- the Task-owned workspace checkpoint initially seeded from Space files;
- the Task's current immutable Plugin environment; and
- links to prior run outputs and Artifacts.

Continue does not require or create a Conversation. It is refused while the
Task already has an active `PENDING`, `SCHEDULED`, or `RUNNING` TaskRun.

### 6.2 Retry

Retry repeats a selected terminal TaskRun's input as another attempt. It records
`retry_of_task_run_id` and does not claim that the user supplied a new message.
It uses the same Task and session lineage under the retry rules owned by the
Task service.

### 6.3 New Task

New Task starts a separate objective and a separate Agent session, even if it
uses the same Agent definition. A user chooses it when prior context should not
influence the next run.

### 6.4 Continuity Contract

The shipped implementation promises durable Agent-session continuity, not a
permanently running process or a sticky worker. Session restore failure must be
recorded and visible once Continue is a user-facing contract; it cannot
silently become a fresh session while the UI claims the thread continued.

[Task workspace checkpoints](task-workspace-checkpoints.md) plan the matching
filesystem half of that contract: Continue restores one immutable Task-owned
`workspace/` checkpoint, Retry returns to the repeated run's recorded base, and
neither path falls back to current Space files silently. The same record makes
an expanded `buildmax-home/plugins/` tree a rebuildable projection: Continue
uses the Task Plugin environment head, Retry reconstructs the repeated run's
base, and an autonomous install takes effect only across a new TaskRun boundary.
That narrow recovery lineage does not reintroduce the withdrawn generic
versioned-workspace or timeline-restore product. Browser profile retention,
user-visible workspace history, change sets, rollback, and merging remain
outside both records.

## 7. Conversation Is An Independent Foreground Surface

Conversation remains the Web-facing foreground chat capability. It owns a
message transcript and a bounded Agent loop suitable for interaction,
clarification, lightweight answers, and orchestration.

It may:

- answer without starting background work;
- ask for missing information;
- choose an Agent when the user has not selected one;
- create one or more Tasks;
- inspect structured Task state through bounded tools; and
- render links or cards for Tasks it originated.

It must not:

- become the required parent of a Task;
- rewrite a typed Agent selection through natural language;
- own worker state, leases, cancellation, retries, or artifacts;
- authorize Task access merely because a Task names its id;
- receive raw worker output as a user message;
- require another foreground model call before a result is durable or visible;
  or
- create a Conversation on behalf of an Issue, Workflow, webhook, direct Agent
  run, or API caller that did not ask for one.

A Conversation-started Task may project a deterministic status/result card
back into its origin Conversation. That relation is optional. A separately
justified presenter may generate an assistant summary, but presentation failure
cannot hide or alter the TaskRun result and cannot start another execution
without a new authorized turn.

## 8. Data, API, And Storage Shape

### 8.1 Task Ownership And Origins

The target Task shape makes Space ownership explicit and Conversation optional:

```text
Task
  id
  space_id                  required, authoritative owner
  agent_id                 required for Agent-backed execution
  conversation_id          optional origin/presentation relation
  issue_id                 optional shared-work relation
  workflow_step_run_id     optional deterministic-plan relation
  created_by               required actor
  session_id               stable Task session
  title / objective
  status / last_run_id
```

`conversation_id` may remain the field name while optional; its meaning is
origin and presentation relation, not parenthood. If later one Task can project
to several Conversations, delivery receives its own relation without changing
Task ownership.

All supplied origin ids must resolve inside `space_id`. A missing origin is a
normal direct execution, not an error.

### 8.2 TaskRun Provenance

Each TaskRun records at least:

- Task id;
- the immutable previous TaskRun that supplies session continuity;
- input and creator;
- trigger source;
- source message when one exists;
- retry lineage when applicable;
- the Agent revision actually used;
- the base and optional result Plugin environment revisions;
- sandbox, model, and credential-grant evidence required to explain the run;
- status, timestamps, usage, output, trace, and Artifacts; and
- whether session restoration succeeded, degraded, or was not requested.

`previous_task_run_id` makes the linear continuation edge queryable and fixes
the session source before `task.last_run_id` advances to the new run. Task order
and a shared session id would not be enough if a later feature allowed
branching; that feature would need a distinct accepted design.

### 8.3 API Direction

The authoritative creation surface should be Space-scoped rather than nested
under Conversation. A likely shape is:

```text
POST /api/spaces/{space_id}/tasks
  { agent_id, input, optional origin fields }

GET  /api/spaces/{space_id}/tasks/{task_id}
GET  /api/spaces/{space_id}/tasks/{task_id}/runs
POST /api/spaces/{space_id}/tasks/{task_id}/runs
  { input }
```

The first POST creates a Task and first TaskRun atomically. The last POST means
Continue and creates a new-input TaskRun. Retry remains a distinct operation
that names the run being repeated.

An Agent-nested convenience route may exist for discoverability, but it must
delegate to the same Task service and return the same Task resource. It cannot
own separate execution rules.

These routes are target semantics, not shipped API. Route registration remains
the implementation source of truth and OpenAPI must change with it.

### 8.4 Storage Namespace

Worker directories and object storage are addressed by durable ownership and
execution identity:

```text
spaces/{space_id}/tasks/{task_id}/runs/{task_run_id}/...
```

The exact prefix is an infrastructure decision, but neither creator id nor
Conversation id belongs in the canonical namespace. A Task survives creator
account changes and does not need a Conversation to locate its Session,
Artifacts, trace, or run-global data.

This project is Alpha and does not preserve the old key shape through a dual
read or dual write. The ownership cutover changes row models, domain types,
wire DTOs, key builders, callers, documentation, and tests together.

## 9. Agent Revision And Runtime Resolution

Task binds the stable Agent identity. Every TaskRun snapshots the exact Agent
revision and execution policy used for that turn.

The target resolution sequence is:

1. admission validates that the Agent belongs to the Space and is active;
2. TaskRun creation snapshots the Agent revision and stable execution
   declarations needed to determine what should run;
3. worker claim materializes dynamic values, credentials, and deployment
   placement against that immutable authorization; and
4. the worker receives one execution specification and cannot widen it.

Snapshotting the Agent revision at admission avoids a queued run changing
because an administrator edits the Agent before a worker claims it. This
intentionally tightens the current claim-time behavior.

Continue keeps the Agent identity and uses the active revision at the time the
new TaskRun is admitted. History therefore remains coherent by identity while
each turn truthfully names the instructions it used. A running or already
admitted TaskRun is never rewritten by a later edit.

Deleting or disabling an Agent prevents new Tasks and new Continue runs. An
already admitted TaskRun retains its snapshot and may finish. Restoring an old
revision creates a later Agent revision under the existing revision rules; it
does not rewrite TaskRun history.

## 10. Authorization And Trust Boundaries

Task access is authorized through `task.space_id` and current Space membership.
Handlers do not fetch Conversation to prove Task ownership. The same rule
applies to TaskRuns, Artifacts, traces, model-call ledgers, cancellation, retry,
and Continue.

An optional relation never grants authority. In particular:

- a Conversation id does not grant access to its Task;
- an Issue relation does not bypass Space membership;
- a source message cannot select a different Space or Agent;
- a worker run token remains scoped to one TaskRun; and
- the Worker derives Space, Task, Agent, and origin data from Server state, not
  from model-provided arguments.

Execution authority belongs to the TaskRun, not the Task. `task_run.created_by`
is the principal whose eligibility is checked and whose user id the run token
carries; `task.created_by` is provenance for the continuing thread only. A
Continue by another member therefore runs as that member, and disabling the
Task's creator does not stop it. Unattended work re-checks that the initiator's
account is enabled and still a member of the Space at Schedule fire, at
dispatch, at the worker's first fetch, and in a one-minute reconciler over
active runs; withdrawn authority ends a run `CANCELED` with a `cancel_reason`
rather than `FAILED`. The rule and its known gaps are in
[system administration](system-administration.md) §8.2.

Task history distinguishes content by trust level. User input is an
instruction. Worker output is untrusted result data. Runtime state and policy
evidence are structured facts. Projecting them into one page does not make them
the same message role or automatically place them in a future model context.

Continue reconstructs model-visible history through the Task's session bundle,
where the shared Agent runtime applies its normal compaction and tool-result
rules. Portal must not rebuild a model session by concatenating rendered HTML
or TaskRun outputs.

## 11. Portal Experience

### 11.1 Agent Page

An Agent card offers `Run` or `New task`, not a button whose only behavior is to
open a generic Conversation. The input modal shows the user's task input. It
does not copy Agent description or instructions into editable user text.

Submitting navigates to the new Task page. The selected Agent id is carried as
a typed request field.

`Chat with coordinator` remains available through the separate Conversation
entry point. If a later product needs a foreground chat bound to one Agent,
that is an explicit Conversation mode with a structured binding, not prompt
text asking another Agent to select it.

### 11.2 Task Page

The Task page shows:

- Agent identity and the revision used by each run;
- Task title, origin, and current status;
- chronological user-input and Agent-output turns;
- in-progress streaming for the active TaskRun;
- per-turn status, usage, trace, artifacts, and failure details;
- separate Stop and Retry actions; and
- an input at the bottom for Continue.

The input is disabled while a run is active. After success, failure, or
cancellation it accepts a new message subject to Agent availability, Space
authorization, and quota. Sending creates a new TaskRun and leaves prior turns
immutable.

### 11.3 Conversation Page

Conversation continues to render its own user and assistant transcript. Tasks
started there appear as structured cards or links ordered beside messages.
Opening a card navigates to the Task page, where the complete TaskRun history
and Continue input live.

A direct Agent Task does not appear in an unrelated Conversation list. It is
discoverable through the Agent's execution history and a Space task/history
surface.

### 11.4 Agent Execution History

The Agent detail surface lists Tasks for that Agent, newest first, with status,
origin, creator, last activity, run count, and latest result summary. Selecting
one opens its Task page. Listing is Space-scoped and paginated; it does not scan
Conversation messages or traces.

## 12. Failure, Recovery, And Concurrency

The existing TaskRun lifecycle remains authoritative. This design adds the
following requirements:

- creating a Task and first TaskRun is atomic;
- at most one active TaskRun exists per Task;
- Continue is idempotent at the API boundary so a client retry cannot create
  two turns;
- a Server restart does not lose a committed Continue request;
- session restoration outcome is recorded before execution proceeds;
- failure to restore is visible and follows one documented fallback or
  fail-closed policy;
- streamed deltas are disposable presentation, while terminal output is read
  from TaskRun state;
- direct runs reach a terminal state and remain inspectable without any
  Conversation or result-delivery row; and
- a Conversation-originated result card is derived from Task state even if an
  optional presenter fails.

Task status is a projection of its current or latest run. A terminal Task can
become active again when Continue commits a new TaskRun. Historical TaskRuns
remain terminal and immutable.

The first release keeps a Task linear. Branching, concurrent children, and
merging sessions are not inferred from the chat UI and remain outside this
design.

## 13. Implementation Sequence

### 13.1 Ownership Cutover

Make Task Space-owned and Conversation optional in one ownership change:

- domain Task and creation inputs;
- `taskRow`, reads, joins, and store queries;
- service validation and authorization;
- worker wire types and task-run scope;
- filesystem and object-store key builders;
- Artifact, trace, model-call, retry, cancellation, and result queries;
- Issue and Workflow Task creation;
- tests, OpenAPI, data-model, server architecture, and current-state docs.

Do not retain a second Conversation-derived ownership rule or compatibility
adapter. Existing Tasks receive their already known Space id; the project has no
released persisted data requiring a dual representation.

### 13.2 Direct Task Admission

Add the Space-scoped Task creation operation and make the Agent page call it.
Remove the Agent-preview prompt and the create-Conversation detour. Record Agent
revision and execution declarations at TaskRun admission.

### 13.3 Task Thread Surface

Add Task history queries and the Task detail page. Add direct Continue with the
same Task service rules the Conversation tool uses. Make Retry visibly and
semantically distinct.

### 13.4 Remove Synthetic Conversations

Change Issue Agent and Workflow step execution to create Tasks directly. Create
result-delivery obligations only for Tasks that name a Conversation delivery
relation. Remove filters and channels that existed solely to hide synthetic
Conversations.

### 13.5 Conversation Projection

Keep Conversation-originated Task cards as projections. Remove automatic raw
result replay and any foreground model call treated as a prerequisite for
completion. Evaluate optional presentation separately.

Each implementation change remains internally complete. In particular, the
ownership cutover moves every caller together rather than running old and new
authorization or key rules side by side.

## 14. Verification

The implementation is accepted when automated evidence covers at least the
following. Each item's current state is marked; an item with open work names
it rather than leaving the gap implicit.

1. **Done.** Starting an Agent from its Portal card creates one Task and one
   TaskRun and creates no Conversation. `portal/e2e/task-thread.spec.ts`.
2. **Done.** The exact typed `agent_id` and admitted revision reach the worker
   without a foreground Conversation model call. True by construction — direct
   admission never opens a Conversation or calls a foreground model — and
   `task.agent_id` is asserted directly in the same spec.
3. **Partly done.** Terminal output, cancellation, and retry work with
   `conversation_id` absent — covered by the same spec and by the Task page's
   Stop/Retry actions. The Task page now consumes
   `GET /api/spaces/{space_id}/tasks/{task_id}/stream` through
   `streamTaskOutput`, while polling every 1.5s for lifecycle state. **Open:**
   direct-Task streaming/reconnect acceptance evidence and Artifacts/trace/usage
   evidence; SSE wiring alone does not prove those journeys.
4. **Done.** A refreshed Task page reconstructs every user input and Agent
   output from TaskRun records — the page holds no state a reload cannot
   rebuild from `GET .../tasks/{task_id}/runs`.
5. **Done.** Submitting from the Task page creates one new-input TaskRun on the
   same Task. `TestContinueRunSendsRestoredHistoryToModel` executes two worker
   runs in separate directories through object storage and proves that the
   second model request contains the first exchange; `task-thread.spec.ts`
   proves the admitted run names the first as its immutable predecessor.
6. **Done.** Retry repeats the selected run and is distinguishable from
   Continue in data (`retry_of_task_run_id`) and UI (separate actions).
7. **Done.** Concurrent Continue requests admit at most one active run, and an
   idempotent client retry does not duplicate it — `TestCreateTaskRunHasOneActiveWinnerUnderContention`
   and `TestCreateTaskRunIsIdempotentByKey` in `internal/infra/db`, run against
   real MySQL and checked by mutation.
8. **Partly done.** Agent edits do not change an admitted run (the existing
   first-write-wins `agent_revision` guard, unchanged by this design). **Open:**
   the Task page does not display which revision a run used (§11.2).
9. **Done.** A deleted Agent cannot accept new work —
   `TestCreateTaskRefusesADeletedAgent` and
   `TestCreateRunRefusesWhenTheTasksAgentWasDeleted` in
   `internal/service/task`. There is no separate "disabled" state in this
   codebase; deleted is the only one. An admitted run finishing under its
   snapshot follows from the worker never re-checking Agent existence
   mid-run, and is not separately tested.
10. **Done.** Issue Agent and Workflow execution create no synthetic
    Conversation — the `workflow`/`issue_agent` channels and their filtering
    machinery were deleted, not merely hidden.
11. **Unverified, likely unaffected.** A Conversation can create a Task and
    receives a durable card without becoming the Task's authorization or
    storage parent — this path is unchanged by this design and
    `portal/e2e/conversation.spec.ts` still passes, but nothing added this
    round exercises it directly.
12. **Done for the routes this design added or changed.**
    `space_authz_matrix_test.go` covers cross-space refusal for
    Task/TaskRun/Artifact/trace/llm-call/cancel/retry routes.
13. **Open.** Worker loss, Server restart, and session-restore failure leaving
    an explainable state, verified specifically for a direct (Conversation-less)
    Task. `StaleRunReaper` and the existing restart-safety mechanisms predate
    this design and are not known to be broken by it, but this round added no
    test naming this path. Session-restore failure visibility is itself an
    open design question — see §16.
14. **Done.** MySQL integration tests exercise the nullable relation
    (`TestCreateTaskDirectHasNoConversation`), direct creation, run concurrency
    (§7 above), and Space authorization (the cross-space case in the same test)
    using `./make test mysql`.
15. **Partly done.** Portal browser coverage exercises direct Run, history
    reload, Continue, and Retry (`task-thread.spec.ts`, run twice against a
    real Compose deployment with no failures). **Open:** a
    Conversation-originated Task card is not covered by a new browser test
    this round.

The scoped documentation, OpenAPI exact-match test, architecture tests,
`git diff --check`, and relevant `./make check` scopes must pass with the same
change.

## 15. Rejected Alternatives

### 15.1 Keep Mandatory Conversation And Hide Synthetic Rows

Hiding generated Conversations fixes a list, not the ownership model. Storage,
authorization, result delivery, and every new execution origin would remain
coupled to an object nobody is using.

### 15.2 Let The Foreground Model Interpret A Selected Agent

A model is useful when selection is unknown. It is not a reliable transport for
an id the user already selected. Typed intent stays typed.

### 15.3 Execute Directly From Agent Without Task And TaskRun

This would make cancellation, retries, status, quota, artifacts, trace, and
audit a second execution system. Agent is reusable configuration; Task and
TaskRun remain the execution envelope.

### 15.4 Store Task Turns As Conversation Messages

The records have different ownership, lifecycle, trust, and failure semantics.
TaskRun already owns execution input and output. Copying them into Conversation
messages creates two sources of truth and risks replaying worker output as user
instruction.

### 15.5 Make One Mutable Session Belong To Agent

An Agent is shared and can run concurrently. A mutable session on the Agent
would leak context between users, Tasks, or Spaces and would make revision and
concurrency semantics incoherent. Session lineage belongs to Task.

### 15.6 Make Task A Permanently Running Worker

Continuity is logical and durable, not a promise of process residence. Workers
remain elastic; each TaskRun materializes the state it needs and terminates.

## 16. Deferred Questions

The core boundary is decided. These questions require implementation or usage
evidence and do not reopen it. (Task session restoration failing closed is no
longer among them: the code commits to fail-closed restore, per
[task-workspace-checkpoints.md](task-workspace-checkpoints.md) and
[orchestration-and-continuity-decisions.md](orchestration-and-continuity-decisions.md)
§5.2.)

- whether retained writable workspace or browser state is valuable enough for
  a separate design;
- whether users need to pin an older Agent revision for a new Continue run;
- whether one Task may later branch into several continuations;
- how long inactive Task session bundles remain warm before archival;
- whether a foreground Conversation presenter materially improves outcomes
  over deterministic Task cards; and
- whether Agent history needs filters beyond status, origin, creator, and
  recency.

Evidence should measure Continue frequency, restoration success, routing
accuracy avoided by direct execution, model-call and latency reduction, result
card usefulness, and the share of Tasks that genuinely need Conversation-led
coordination.
