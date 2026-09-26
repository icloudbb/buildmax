# Portal Work and Execution Experience

> **简体中文：** [阅读中文镜像](../zh-CN/design/Portal工作与执行体验.md)
> **Audience:** Portal, service, and execution-plane contributors · **Status:**
> implemented — all seven slices shipped, including the owner/executor split
> against a real MySQL and the derived Issue Board.

This record defines the Portal experience from an Issue through Agent execution
to a durable result. It is an implemented foundation for the R3 candidate
operator journey; it does not replace the execution model or reorder the
roadmap.

## Contents

- [Outcome](#outcome)
- [Evidence and constraints](#evidence-and-constraints)
- [Decision](#decision)
- [Issue experience](#issue-experience)
- [Workflow authoring](#workflow-authoring)
- [Execution provenance](#execution-provenance)
- [Implementation slices](#implementation-slices)
- [Acceptance criteria](#acceptance-criteria)
- [Alternatives rejected](#alternatives-rejected)
- [Related records](#related-records)

## Outcome

A user starts from the work to be accomplished, deliberately launches execution,
and can understand what happened without reading internal orchestration graphs.
Every Task and TaskRun visibly preserves who or what initiated it, what executed
it, and which Issue, Conversation, or Workflow it belongs to.

## Evidence and constraints

- Issue is the primary user-facing work object. Task and TaskRun are the durable
  execution plane; a Conversation can create a Task but does not own execution.
- The current Issue form saves assignment changes separately from its Run
  actions, while the user manual implies that assignment schedules execution.
  That disagreement risks accidental expectations around quota-consuming work.
- A current Task can have both an `agent_id` and an Issue, Conversation, or
  Workflow origin. Showing the Agent first in breadcrumbs describes the
  executor, not the origin.
- Task inputs are currently rendered as if a human called “You” wrote them,
  including workflow- and Issue-generated instructions.
- Issue Detail exposes overlapping execution summaries, flow steps, timelines,
  and run histories. The detail is useful for diagnosis but overwhelms the work
  outcome.
- This design does not add a new execution entity. Task and TaskRun remain the
  authoritative thread and attempt/result objects defined by
  [Agent execution and Task threads](agent-execution-and-task-threads.md).

## Decision

Issue remains the work hub. Portal presents execution as a deliberate action
from that hub and presents results before runtime mechanics.

Saving an Issue never starts a run. A user-visible **Run** action is the only
form action that schedules work and spends execution quota. If policy later
supports automatic triggers, the trigger must be an explicit persisted rule,
visible beside the Issue execution configuration and in the resulting TaskRun
provenance.

The experience uses three distinct concepts:

- **Owner** is the accountable human or team membership for the Issue.
- **Executor** is an Agent or Workflow selected to perform work.
- **Trigger** is the actor or rule that requested a specific run.

A combined assignee field could not represent a human owner and an Agent
executor at the same time. The API and storage model separate owner from
executor rather than adding more variants to a single field: `issue.owner_id`
and `issue.executor_kind`/`issue.executor_id` are independent columns, and
either, both, or neither may be set. See slice 6 below.

No handler, Portal form, or scheduler independently infers execution from a
save. The service that creates TaskRuns remains the single authority for quota,
authorization, trigger metadata, and scheduling.

## Issue experience

Issue Detail is organized into four user-level areas:

| Area | Purpose |
|---|---|
| Overview | Status, owner, executor, next action, and latest outcome |
| Discussion | Human and Agent collaboration about the work |
| Results | Published artifacts and structured outcomes |
| Runs | Task and TaskRun history, including failures and diagnostics |

The default Overview answers what is being done, who is accountable, what will
execute, and what the latest outcome was. Flow steps and trace-level events live
under the relevant run rather than appearing as parallel Issue summaries.

Save and Run have separate labels, progress, errors, and success feedback. Run
is disabled with a specific reason until an executor and all required inputs are
valid. A successful scheduling response links directly to the new Task or run.

Status labels use one shared presentation vocabulary across Issue, Task,
TaskRun, Workflow, and cards. API enum values remain stable machine values but
are not exposed as untranslated primary labels.

### Issue collection: List and Board

The Space Issue collection has two equal views over the same query: List, the
default, and Board. Board lets a participant see how top-level work is spread
across business states and deliberately change a state without opening each
Issue. It is a projection, not a planning model, and owns no durable state:

| Concern | Authority | Board behavior |
|---|---|---|
| Lane membership | `issue.status` | Exactly three fixed lanes — To do, In progress, Done — in domain order; no stored lane value |
| Status change | The existing versioned Issue update | A move sends the card's loaded version and new status only; Owner and Executor are untouched |
| Execution | Task, TaskRun, and Workflow | Never moves a card and is never started by a move |
| Child progress | Derived child counts | Shown on the parent card; parent and child statuses stay independent |
| Filters | The list query contract | View, Owner, and Executor live in the URL and apply identically to List and every lane |

Each lane is its own filtered request with its own total, incremental paging,
and resource state. Grouping one fetched page is rejected because pagination
would happen before grouping, making a lane look empty only because its Issues
fell off the page. A lane that fails is shown as failed with a local retry, and
loaded lanes carry an explicit "board incomplete" warning. Lanes keep the
collection's `updated_at` descending order, so a moved Issue appears near the
top of its destination; there is no manual rank or persisted drag position.
Separately loaded lanes are an eventually refreshed view, not one database
snapshot.

A version conflict is not retried: Portal says the Issue changed, reloads every
lane, and leaves the decision to the reader. The named **Move to** action is
the move contract for keyboard, assistive technology, touch, and pointer, and
focus returns to the moved card or its lane afterward. Drag, if ever added, is
only an enhancement with parity to that action. The whole-Space board shows
top-level Issues only; a parent's breakdown stays on Issue Detail.

## Workflow authoring

The normal Workflow editor exposes only concepts the runtime supports. While
`agent_task` is the only executable step type, the editor presents an Agent
step rather than a free-form Type field. It generates a stable step identifier
and keeps that identifier out of the primary form unless a concrete linking or
diagnostic task requires it.

Raw definition JSON remains available through an explicitly labeled advanced
mode for contributors and operators who need exact inspection. It is not shown
beside the normal form as an equivalent editing path. Both modes call the same
domain validation and show errors against the affected step before Save is
enabled. Portal does not invent future step types ahead of runtime support.

## Execution provenance

Every TaskRun presentation derives four fields from authoritative metadata:

| Field | Example |
|---|---|
| Origin | Issue “Fix import failures” |
| Trigger | Jiang manually selected Run |
| Executor | Agent “Repository maintainer” |
| Attempt | Run 3, retry of Run 2 |

Breadcrumbs are origin-first: an Issue-origin Task navigates through the Issue;
a Conversation-origin Task navigates through the Conversation; a Workflow run
navigates through its Workflow run. The Agent is displayed as executor metadata
and linked separately.

Input attribution follows the trigger, not a hard-coded user avatar:

- direct Conversation continuation may be labeled with the sending member;
- manual Issue execution identifies the member and Issue;
- Workflow execution identifies the Workflow step and initiating run;
- API or system execution uses its authenticated actor or named system trigger;
- migrated data with missing provenance is labeled “Unknown origin,” never
  guessed as “You.”

Provenance is captured when the TaskRun is created. Portal does not reconstruct
it from optional foreign keys after the fact.

## Implementation slices

The following slices are independently useful and can merge in order:

1. **Truthful presentation.** Fix origin-first breadcrumbs and input attribution
   using existing metadata; label unknown data honestly.
2. **Explicit action contract.** Align Portal and manual wording so Save only
   persists and Run schedules; standardize mutation feedback.
3. **Issue information architecture.** Consolidate sections into Overview,
   Discussion, Results, and Runs without changing service behavior.
4. **Workflow authoring.** Replace free-form step Type and editable IDs with the
   supported Agent-step form; move raw JSON into explicit advanced mode.
5. **Persisted provenance.** Add the minimal trigger fields to TaskRun creation,
   API responses, traces, and tests, with one authoritative constructor.
6. **Owner/executor split.** Replace the combined assignee model coherently in
   domain, store, API, Portal, fixtures, and documentation. Because this changes
   `internal/infra/db`, it requires the real-MySQL test scope.
7. **Issue Board.** Add the List / Board switch, URL-carried Owner and Executor
   filters, per-status lanes over the existing list route, and the versioned
   **Move to** action. No schema, route, or Server entity changes.

## Acceptance criteria

- Saving any Issue edit does not create a Task or TaskRun.
- Starting execution requires an explicit action or a separately visible,
  persisted automatic trigger.
- Every newly created TaskRun records origin, trigger, executor, and retry
  relationship where applicable.
- Task breadcrumbs navigate to the true origin even when an Agent is present.
- No non-human generated instruction is labeled “You.”
- Issue Overview exposes latest outcome and next action without duplicating run
  internals; full diagnostics remain reachable within two navigation actions.
- Normal Workflow authoring cannot persist an unsupported step type, and both
  normal and advanced modes use the same validation.
- Service tests prove authorization, quota, provenance, and no-run-on-save.
  Portal tests prove each trigger label and the Issue-to-result journey.
- List and Board show the same top-level Issue identities under the same
  filters; each lane shows its own total, and a failed lane never reads as an
  empty one.
- A Board move uses the loaded version, never overwrites a concurrent edit,
  never schedules execution, and on conflict reloads and requires a new
  decision.

## Alternatives rejected

- **Treat assignment as execution.** Editing responsibility and spending quota
  are different user intentions and require different failure handling.
- **Keep owner, executor, and trigger in one assignee union.** It cannot express
  simultaneous accountability and automation and makes provenance ambiguous.
- **Infer provenance in Portal.** Foreign-key presence is insufficient when a
  Task participates in multiple relationships, and inference changes over time.
- **A configurable planning board.** Custom columns, rank, WIP limits, saved
  views, swimlanes, and sprints are independent concepts no demonstrated
  BuildMax outcome requires, and they would compete with dedicated planning
  products.
- **Move a card from execution state.** An Agent starting or a run finishing
  does not declare the business outcome; only an explicit status change does.
- **Create a separate Outcome entity now.** TaskRun already owns the
  authoritative result; artifacts and structured outputs can be presented from
  that contract without another lifecycle.

## Related records

- [Product vision](product-vision.md)
- [Agent execution and Task threads](agent-execution-and-task-threads.md)
- [Issue Agent access](issue-agent-access.md)
- [Workflow runtime](workflow-runtime.md)
- [Portal state and permission feedback](portal-state-and-permission-feedback.md)
- [Portal responsive and accessible interaction](portal-responsive-and-accessible-interaction.md)
