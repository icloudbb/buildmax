# Issue Topic Coordination and Agent Blackboard

> **简体中文：** [阅读中文镜像](../zh-CN/proposals/issue-topic-coordination.md)
>
> **Audience:** product designers, contributors, and early adopters · **Status:** proposal — under discussion
>
> **Opened:** 2026-09-13

Related: [roadmap](../ROADMAP.md), [current state](../current-state.md),
[Agent-native collaboration substrate](agent-native-collaboration-substrate.md),
[Session trees and Agent mailboxes](session-tree-and-agent-mailbox.md),
[Issue Agent access](../design/issue-agent-access.md),
[Agent bridge CLI](../design/agent-bridge-cli.md),
[Agent execution and Task threads](../design/agent-execution-and-task-threads.md),
[Workflow runtime](../design/workflow-runtime.md), and
[Portal work and execution experience](../design/portal-work-and-execution-experience.md).

## Contents

- [1. Summary](#1-summary)
- [2. User Outcome, Evidence, And Constraints](#2-user-outcome-evidence-and-constraints)
- [3. Terms And Mental Model](#3-terms-and-mental-model)
- [4. User Scenarios](#4-user-scenarios)
- [5. Goals](#5-goals)
- [6. Non-Goals](#6-non-goals)
- [7. Separate Information, Delivery, And Synchronization](#7-separate-information-delivery-and-synchronization)
- [8. Topic Scope And Membership](#8-topic-scope-and-membership)
- [9. Blackboard Content And Authority](#9-blackboard-content-and-authority)
- [10. Reading, Delivery, And Agent Context](#10-reading-delivery-and-agent-context)
- [11. Point-To-Point Signals And Broadcast](#11-point-to-point-signals-and-broadcast)
- [12. Synchronization And Work Ownership](#12-synchronization-and-work-ownership)
- [13. Security, Governance, And Cost](#13-security-governance-and-cost)
- [14. Consistency And Failure Semantics](#14-consistency-and-failure-semantics)
- [15. Workspace And Change Integration](#15-workspace-and-change-integration)
- [16. Surface Experience](#16-surface-experience)
- [17. Options And Trade-Offs](#17-options-and-trade-offs)
- [18. Staged Validation](#18-staged-validation)
- [19. Prototype Acceptance Criteria](#19-prototype-acceptance-criteria)
- [20. Open Questions](#20-open-questions)
- [21. Evidence Needed Before Acceptance](#21-evidence-needed-before-acceptance)
- [22. Architecture Landing Areas](#22-architecture-landing-areas)
- [23. Destination If Accepted](#23-destination-if-accepted)
- [24. Candidate Direction](#24-candidate-direction)

## 1. Summary

Several Agents working toward one outcome sometimes need more than independent
Issue assignments. They need to learn that another participant discovered a
constraint, changed an assumption, produced evidence, or became blocked before
they repeat work or proceed from stale premises.

This proposal evaluates a topic-scoped coordination capability with three
deliberately separate parts:

1. An **Issue Topic Blackboard** is a durable, bounded, source-attributed feed
   of information relevant to every participant working under one parent Issue.
2. A **point-to-point Signal** asks a specific participant to act and therefore
   has delivery, acknowledgement, and optional wake-up semantics.
3. A **join or Workflow condition** decides when dependent work may proceed. It
   is durable coordination state, not a convention hidden in free-form text.

The first validation slice should not create a new `Blackboard` entity. A parent
Issue already names the shared outcome, child Issues already name divisions of
work, and Issue comments already form a durable human- and Agent-readable
thread. The smallest useful experiment is to let a child Issue's Agent read and
report to a constructor-scoped view of the parent Issue discussion.

Only repeated use that exposes concrete failures in comments should justify a
separate coordination-entry store, monotonic cursors, subscriptions, or typed
projections. Even then, the Blackboard should be a topic feed rather than a
shared mutable dictionary or an arbitrary Agent chat network.

This proposal records a candidate direction, not shipped behavior or roadmap
commitment.

## 2. User Outcome, Evidence, And Constraints

### 2.1 Essential user outcome

The essential outcome is:

> People and Agents contributing to the same objective can discover relevant
> changes in shared understanding early enough to avoid duplicate work,
> contradictory assumptions, and uncoordinated delivery, while the system keeps
> action requests and synchronization reliable and accountable.

The outcome is not “Agents can send messages.” Messaging is a mechanism. The
user-visible result is coherent parallel work with understandable provenance
and without an unattended conversation loop.

### 2.2 Evidence in the current product

BuildMax already contains evidence that an Issue-centered coordination surface
is plausible:

- Issue is the primary shared work object and may contain one level of child
  Issues.
- Owner and executor are separate, so accountability and Agent or Workflow
  execution can coexist.
- Issue Discussion contains durable comments from people, server-observed Agent
  runs, local Agent claims, and the system, with source Task and TaskRun
  provenance where available.
- An Agent working an Issue runs `buildmax issue show` and
  `buildmax issue comment` through Bash. In a worker run the run token and
  worker routes, reached over the run bridge, scope both to the run's own Issue
  and an Issue id argument is refused; it reads bounded recent comments and
  adds a bounded report without choosing an Issue identifier. Locally the
  commands run with the person's own credential and take an id.
- Task and TaskRun already own durable execution and results. Artifact already
  owns immutable evidence.
- Workflow is the accepted owner for declared dependencies, readiness, waits,
  retries, and aggregate completion.

These foundations prove that participants can leave and read Issue-scoped
reports. They do not prove that a cross-Issue topic feed will improve real work.

### 2.3 The current gap

An Agent is scoped to the Issue its Task names. When a parent Issue is divided
into child Issues for different Agents, each child can see its own discussion,
not a durable topic-level view shared with its siblings. Terminal run summaries
may return to separate child discussions after the information would have been
useful elsewhere.

Issue comments also lack the stronger semantics sometimes implied by the word
“coordination”:

- no addressed recipient or processing acknowledgement;
- no guarantee that an active Agent observes a new comment;
- no per-participant cursor or unread state;
- no explicit barrier, join, deadline, or failure policy;
- no distinction between an observation, proposal, blocker, and authoritative
  decision beyond prose; and
- no safe shared-workspace or change-integration meaning.

### 2.4 Constraints that exist today

- Space remains the ownership and authorization boundary for Portal resources.
- Issue owns shared work and result context, not execution or Workflow state.
- Task plus TaskRun remains the only durable Agent execution plane.
- Issue status, ownership, executor selection, and hierarchy remain human- or
  service-authorized state; an Agent report must not mutate them implicitly.
- Agent-authored content is evidence, not system or user authority, and may
  contain prompt injection from material the Agent read.
- A running Agent cannot safely be interrupted inside an assistant/tool batch.
- Parallel writers require isolated workspaces and reviewable change
  integration; communication does not make one directory multi-writer safe.
- The current roadmap prioritizes operational reliability. This proposal must
  earn priority with evidence rather than by introducing a broad platform
  abstraction.

## 3. Terms And Mental Model

### 3.1 Topic

A Topic is the shared objective around which several work items coordinate. In
the candidate first slice, the Topic is an existing top-level Issue. A child
Issue derives its Topic from `parent_issue_id`; a top-level Issue is its own
Topic.

Topic is initially a relationship and a user mental model, not a new database
entity.

### 3.2 Participant

A Participant is a person, Task, local linked Session, or future supervised
Session entitled to read or contribute within the Topic. An Agent definition is
not itself a participant: one Agent configuration may execute many unrelated
Tasks concurrently.

### 3.3 Blackboard

The Blackboard is the shared, append-oriented information feed for a Topic. It
contains bounded statements and references, not complete transcripts, file
contents, or mutable global variables.

“Blackboard” is useful as a product metaphor, but the architecture should call
the durable concept a coordination feed or entry if a new persisted concept is
eventually justified. That avoids implying that arbitrary values can be erased
or overwritten in place.

### 3.4 Signal

A Signal is a durable, source-attributed, addressed event intended for one
recipient or supervisor. Unlike a Blackboard entry, it has a delivery and
processing lifecycle and may cause a paused participant to become runnable
under explicit policy.

### 3.5 Join condition

A Join condition is deterministic state describing when dependent execution may
continue: for example all children terminal, any success, a deadline, or a
manual decision. Workflow or a Session supervisor owns it; the Blackboard may
display it but does not implement it.

### 3.6 Topic snapshot

A Topic snapshot is the bounded view supplied to an Agent at a defined point.
It includes the parent Issue objective, child status summaries, recent relevant
entries, omitted counts, and provenance. It is not live shared memory.

## 4. User Scenarios

### 4.1 Parallel investigation discovers a shared constraint

A parent Issue is divided into three children for API design, persistence, and
Portal experience. The persistence Agent discovers that the proposed ordering
cannot be implemented safely without a monotonic sequence. It posts a finding
to the parent Topic. The other participants see it at their next coordination
boundary and adjust their proposals.

No Agent is automatically restarted merely because the finding was posted.

### 4.2 One participant needs a specific answer

The Portal Agent needs the API Agent to decide whether unread counts are exact
or approximate. Posting a question to the Blackboard makes it visible, but does
not prove that the API Agent will act. A point-to-point Signal references the
Blackboard entry and asks that participant to answer.

The answer may become another Blackboard entry when it matters to the Topic.

### 4.3 Fan-in waits for several results

A coordinating Workflow dispatches three child Tasks and must synthesize only
after all are terminal. Each Task may post useful findings during execution,
but Workflow reconciliation decides readiness from TaskRun state. A prose line
such as “I am done” never satisfies the join.

### 4.4 A decision supersedes an earlier assumption

A person accepts one proposed API shape. The Topic shows the accepted decision
and the earlier proposal as superseded. New Agent snapshots contain the current
decision and preserve a link to the earlier record; historical TaskRuns retain
the older basis they actually used.

### 4.5 A child produces workspace changes

A child reports a conclusion and references a change set or Artifacts. Other
participants may inspect that evidence, but the report does not merge files
into their workspaces. The authorized integration path remains separate.

## 5. Goals

- Give participants under one Issue objective a bounded shared information
  surface.
- Reduce duplicated work and contradictory assumptions across parallel child
  Issues.
- Preserve author, execution source, time, and evidence references for every
  Agent contribution.
- Keep broad information publication separate from addressed action requests.
- Keep information exchange separate from durable synchronization and
  readiness decisions.
- Deliver Agent-visible updates only at reproducible, safe context boundaries.
- Reuse Issue, Task/TaskRun, Artifact, Workflow, and mailbox responsibilities
  instead of introducing an overlapping execution model.
- Bound context, notifications, writes, automatic execution, and storage.
- Validate the need with the smallest useful extension before creating a new
  server entity.

## 6. Non-Goals

- Arbitrary real-time group chat between Agents.
- A mutable key-value store shared by running model loops.
- Direct sibling-to-sibling commands through model-selected target IDs.
- Treating every Blackboard entry as guaranteed delivery to every participant.
- Waking every participant for every update.
- Encoding Workflow barriers, retries, or completion in comments.
- Letting an Agent change Issue status, owner, executor, or hierarchy.
- Replaying an unbounded Topic history into every model call.
- Copying full Task output, transcripts, diffs, or files into the feed.
- Making the Blackboard an Artifact, Wiki, Project Memory, audit log, or Agent
  session history.
- Making concurrent writes to one workspace safe.
- Distributed exactly-once execution or delivery.

## 7. Separate Information, Delivery, And Synchronization

One general message abstraction appears simpler locally but multiplies hidden
state across its lifecycle. A recipient-specific question needs acknowledgement;
a broad finding does not. A join must survive restart and concurrency; a comment
must not decide it. Combining them makes every post carry recipients,
subscriptions, wake policy, barrier state, and retry semantics whether it needs
them or not.

The candidate model keeps three responsibilities explicit:

| User need | Authoritative concept | Required semantics |
|---|---|---|
| Share information relevant to the whole Topic | Blackboard / coordination feed | Durable append, provenance, bounded reads |
| Ask a particular participant to act | Mailbox Signal | Addressing, durable delivery, acknowledgement, optional bounded wake-up |
| Wait until work reaches a condition | Workflow or Session supervisor | Readiness, deadline, retry, failure, recovery |

The concepts may reference each other. A Signal can name the Blackboard entry
that motivated it, and a Topic view can show a Workflow join. Reference is not
ownership.

## 8. Topic Scope And Membership

### 8.1 Derive the first Topic from Issue hierarchy

The first slice derives the Topic deterministically:

```text
top-level Issue        -> topic_issue_id = issue.id
child Issue            -> topic_issue_id = issue.parent_issue_id
```

The existing hierarchy is only one child level deep, so this rule has no
ambiguous ancestor traversal. If Issue hierarchy later grows deeper, the Issue
service must remain the single owner of root resolution.

### 8.2 Scope by construction

An Agent-facing command in a worker run must not accept `topic_issue_id`,
`issue_id`, participant ID, or Space ID from the model. The run token names
one TaskRun and its Issue, and the worker route resolves that Issue and its
derived Topic, so the scope lives in the credential and the route rather than
in a client-side argument.

This extends the safety pattern of `buildmax issue show` and
`buildmax issue comment`, which
[Agent bridge CLI §3](../design/agent-bridge-cli.md#3-why-this-reverses-issue-agent-access)
moved from tool constructors to the credential and worker route. It does not
grant a model a Topic browser. Removing arbitrary target parameters prevents
one malicious comment from turning an otherwise ordinary command into a
cross-Issue data-exfiltration path. The local context is different by design:
there the commands run with the person's own credential and accept an Issue
id, and the person is accountable for what the Agent reports.

### 8.3 Membership and lifetime

- A Space member reads the Topic according to existing Issue authorization.
- A worker Task participates only while its run credential is valid and only
  through its Task's Issue relation.
- A local Session participates through the authenticated person's explicit,
  durable Issue link if that proposal is accepted.
- Removing or moving a child Issue affects future capabilities; it does not
  rewrite the provenance of entries already accepted.
- An Agent definition has no standing subscription outside a Task or Session.

## 9. Blackboard Content And Authority

### 9.1 The first slice uses existing comments

The first experiment should project selected parent Issue comments and child
status summaries into an Agent's bounded Topic snapshot. A child Agent may post
a report to the parent Discussion through a constructor-scoped capability.

This intentionally tests the user outcome before committing to a new schema.
The current comment fields already preserve body, author kind, author identity,
source Task, source TaskRun, creation time, and edit state.

The UI may label this filtered view “Coordination” or “Blackboard” without
claiming that a separate persisted resource exists.

### 9.2 Candidate typed entries, only if evidence requires them

Free-form comments may eventually fail to support filtering and summaries. If
that happens, a minimal append-only entry could need:

| Field | Requirement that fails without it |
|---|---|
| `id` | Stable reference from Signals, decisions, traces, and UI |
| `topic_issue_id` | One durable Topic scope and authorization path |
| `sequence` | Stable incremental reads under timestamp collisions |
| `kind` | Bounded filtering and projection without interpreting prose |
| `author_kind`, `author_id` | Trust and accountability |
| `source_task_id`, `source_task_run_id` | Server-observed execution provenance |
| `body` | The bounded human- and model-readable statement |
| `artifact_ids` | Stable evidence references without embedding content |
| `supersedes_entry_id` | Append-only correction of stale information |
| `created_at` | History and freshness |

No field should land until an observed workflow fails without it. In
particular, recipients, acknowledgement state, join state, and workspace content
do not belong on a broadcast entry.

### 9.3 Candidate kinds

A small closed set might distinguish:

- `finding`: an observation supported by evidence;
- `proposal`: a suggested shared assumption or course of action;
- `blocker`: a condition preventing progress;
- `question`: an unanswered Topic-level question; and
- `notice`: a concise coordination update that fits none of the above.

`decision` should not initially be Agent-writable. A decision changes what
participants are entitled to treat as accepted and therefore needs an
authorized person, Workflow transition, or later explicit decision service.
An Agent may post a proposal that says it recommends a decision.

### 9.4 Append rather than rewrite

Agent entries are immutable. A correction appends a new entry that supersedes
the earlier one. Human comment editing can remain a Discussion behavior, but a
future authoritative coordination projection should not silently rewrite the
basis of an already completed TaskRun.

## 10. Reading, Delivery, And Agent Context

### 10.1 Snapshot, not live shared memory

An Agent receives a Topic snapshot when a TaskRun starts or when it explicitly
calls a read tool. The snapshot states when it was taken and how much history
was omitted. Later posts do not alter the context of a run already reasoning
from that snapshot.

### 10.2 No mid-batch injection

A Blackboard update never breaks an `assistant(tool_calls) -> tool results`
sequence. If later delivery to an active run is valuable, the supervisor may
offer the update only at a complete Agent-loop iteration boundary and must
record that boundary in the trace.

### 10.3 Pull before push

The first slice uses bounded pull:

- initial Topic snapshot;
- explicit refresh through a scoped read tool; and
- refresh on a later TaskRun.

This is enough to test whether shared information helps. Durable subscriptions,
per-participant cursors, unread counts, digests, and automatic wake-up should
not be built until observation shows that pull is too stale.

### 10.4 Bounded projection

The model-facing projection should prefer:

1. the Topic title and objective;
2. current child titles and statuses;
3. accepted current decisions if that concept later exists;
4. recent unresolved blockers and questions; and
5. a bounded tail of other entries with an omitted count.

The projection labels all Agent-authored material as reports or evidence, not
user or system instructions.

## 11. Point-To-Point Signals And Broadcast

A Blackboard post is visible to eligible Topic participants but does not prove
that any particular participant read or processed it. That is the correct
meaning for shared findings and status context.

When the sender needs action from one recipient, the system uses a Signal. The
Signal may reference one Blackboard entry instead of copying its body. The
mailbox proposal owns the eventual delivery states, idempotency, wake policies,
and parent/supervisor restrictions.

The first Blackboard slice should not add arbitrary Signal addressing. A safe
sequence is:

1. validate direct child-to-parent reports and parent join behavior;
2. learn which real coordination cases remain unsolved by the Topic feed;
3. add only the addressed routes those cases require; and
4. keep model-selected arbitrary Task, Session, Agent, or Issue IDs out of tool
   parameters.

A broadcast entry may cause one cheap UI notification or digest for the Topic.
It must not create one model call per participant by default. Otherwise one
entry can multiply token spend, tool side effects, and further replies into an
unattended feedback loop.

## 12. Synchronization And Work Ownership

### 12.1 A Blackboard is not a barrier

Statements such as “finished,” “waiting,” or “approved” are evidence from a
participant. They are not authoritative execution state. Workflow or the
Session supervisor evaluates joins from durable TaskRun, request, timeout, and
cancellation facts.

### 12.2 Use Issues for declared division of work

The first slice should not introduce free-form work claiming on the Blackboard.
Parent and child Issues already express the human-visible division of work,
owner, executor, and status. Duplicate work should first be addressed through
that model.

If real dynamic swarms later show that work is created too quickly for Issue
assignment, a bounded claim with lease and expiry may be evaluated as a separate
coordination concept. A prose `claim` entry without atomic ownership would only
make duplicate work look coordinated.

### 12.3 One synthesizing authority

Parallel participants may contribute conflicting findings. A parent Agent,
Workflow evaluator, or person must be explicitly responsible for synthesis.
The Blackboard records inputs; it does not make consensus emerge automatically.

## 13. Security, Governance, And Cost

### 13.1 Trust labels survive projection

The current distinction between a server-observed Agent report and a local
Agent claim must remain visible. Topic aggregation must not erase whether the
deployment observed the TaskRun that produced a statement.

### 13.2 Reports are untrusted input

An Agent may copy malicious instructions from a web page, repository, document,
or another comment. Blackboard content enters the receiving model as attributed
untrusted evidence, never as a system prompt, user request, tool grant, or
approval.

### 13.3 Least authority

- Tools are scoped by construction to one Topic.
- Posting cannot change Issue or Workflow state.
- Artifact references must resolve inside the same authorized Space and must
  not expose object-store or local filesystem paths.
- Child grants do not flow to siblings, parents, or future runs.
- A Blackboard entry cannot upgrade a recipient's execution policy.

### 13.4 Bounds

The system needs explicit limits on:

- entries per TaskRun;
- entry body and Artifact reference count;
- entries returned to a model;
- notifications per Topic and time window;
- automatic processing runs and token spend; and
- retention or compaction of operational entries.

Long content remains in TaskRun output or Artifact. A useful feed is a set of
distilled coordination statements, not a duplicate run archive.

## 14. Consistency And Failure Semantics

### 14.1 Persist before notify

An entry or Signal is durable before any WebSocket, Desktop event, or scheduler
wake-up. Notification is a projection and may be retried or lost without losing
the underlying statement.

### 14.2 Idempotent append

Agent posting requires a caller-generated idempotency key scoped to its TaskRun
or Session. A network retry returns the existing accepted entry rather than
creating a duplicate.

### 14.3 Ordering

If the experiment graduates beyond comments, each Topic needs a monotonically
ordered sequence assigned by its store. `created_at` alone cannot define a safe
cursor under concurrent writes.

The sequence establishes observation order, not causality. An entry that
depends on another names it explicitly or records the snapshot sequence it was
based on.

### 14.4 Delivery is at-least-once processing

Exactly-once distributed delivery is not required. Durable consumers may see a
notification more than once and deduplicate by entry or Signal ID. Processing
state is recorded only where a consumer is actually expected to process an
addressed Signal.

### 14.5 Late and stale information remains visible

A report arriving after a deadline, cancellation, or accepted decision remains
part of the history and is marked late or based on an older snapshot. It does
not restart canceled work or silently replace the accepted state.

## 15. Workspace And Change Integration

The Blackboard coordinates understanding, not filesystem ownership.

- Parallel writable Tasks use isolated workspaces or worktrees.
- Reports reference immutable Artifacts or a future reviewable change set.
- Accepting a finding is separate from applying its workspace changes.
- Applying one child's changes does not rewrite another child's snapshot.
- Conflict detection and integration belong to the workspace capability, not
  the comment or coordination-entry store.

A Blackboard shipped without workspace isolation can improve parallel research
and planning, but must not be described as safe parallel implementation.

## 16. Surface Experience

### 16.1 Parent Issue

The parent Issue remains the work hub. Its existing areas retain their meaning:

- Overview shows objective, ownership, executor, status, and latest outcome;
- Discussion remains the complete human-readable thread;
- Results shows TaskRun outputs and Artifacts; and
- Runs shows execution history and diagnostics.

A candidate Coordination view filters and summarizes Topic-relevant posts,
child state, unresolved blockers, and accepted decisions. It is a projection,
not a fifth source of truth.

### 16.2 Child Issue and Task

A child surface shows:

- which parent Topic supplies coordination context;
- when the visible snapshot was last refreshed;
- how many entries were omitted or remain unread, if the system can state that
  truthfully; and
- where a report will be posted before the action occurs.

### 16.3 Agent tool feedback

A successful post reports the durable entry or comment ID, Topic, and whether
it was only published or also referenced by an addressed Signal. It never says
“everyone was notified” unless the system has a concrete notification contract
that proves it.

## 17. Options And Trade-Offs

### 17.1 Option A: Keep Issue comments scoped to each Issue

This adds no concepts and preserves today's narrow authorization. It does not
let sibling work share discoveries without a person copying them, so it leaves
the proposed user outcome unsolved.

### 17.2 Option B: Create a general Blackboard entity

A Blackboard independent of Issue could serve arbitrary Sessions, Tasks,
projects, and ad hoc groups. It also requires new ownership, membership,
lifecycle, discovery, retention, authorization, and relation semantics before
one real Topic workflow has been validated.

This is not recommended as the first direction.

### 17.3 Option C: Derive an Issue Topic feed and keep mailbox and joins separate

This reuses the existing work hub, child decomposition, comment provenance,
the `buildmax issue` command surface, and Space authorization. It adds only the missing ability for a
child to participate in parent-scoped coordination. Later typed entries can be
introduced behind the same Topic experience if comments prove insufficient.

The main concern is overloading Discussion with operational noise. Strict write
budgets, filtered projections, and evidence from the comment-based slice are
therefore required.

This is the candidate recommendation.

### 17.4 Option D: Build an arbitrary Agent message bus

A general bus makes point-to-point, group, broadcast, command, and question
messages look uniform. Their authorization, acknowledgement, wake-up, cost, and
failure semantics are not uniform. The apparent flexibility moves complexity
into every sender and recipient and makes autonomous loops easy to create.

This is not recommended.

### 17.5 Option E: Encode all coordination in Workflow

Workflow can reliably own declared dependencies and fan-in. It should not need
to model every discovery, changed assumption, question, or evidence reference
as a node transition. This option is too rigid for the information-sharing half
of the problem.

## 18. Staged Validation

### Phase 0: Observe the current workaround

- Run bounded multi-Agent Issue journeys with a parent and several children.
- Observe when people copy comments, restate constraints, or discover duplicate
  work only during synthesis.
- Record whether the missing capability is broad awareness, addressed action,
  synchronization, or workspace integration.

### Phase 1: Parent Issue coordination through existing comments

- Derive a child Issue's Topic from its parent.
- Let a child Agent read a bounded parent Topic snapshot through a
  constructor-scoped capability.
- Let it post a small number of reports to the parent Discussion.
- Display those reports with existing author and TaskRun provenance.
- Do not add subscriptions, automatic wake-up, typed entries, or a new table.
- Do not give the model an Issue or participant ID parameter.

### Phase 2: Filtered projection and explicit refresh

- Add a Coordination view over parent comments and child state.
- Make snapshot time and omitted history visible.
- Let people and Agents explicitly refresh.
- Measure which posts are useful, ignored, duplicated, or too late.

### Phase 3: Typed feed and cursor, only if earned

- Introduce append-only coordination entries only if free-form comments cannot
  support useful filtering, incremental reads, or immutable corrections.
- Add per-Topic sequence, idempotent append, and `supersedes_entry_id`.
- Preserve Discussion as the human-readable projection or explicitly decide
  which entries belong in both views.

### Phase 4: Addressed Signals and bounded subscriptions

- Add a Signal that references an entry when one recipient must act.
- Start with the mailbox proposal's restricted parent/supervisor routes.
- Add per-participant cursors or digests only where pull demonstrably arrives
  too late.
- Keep automatic model wake-up opt-in, budgeted, and lifecycle-aware.

### Phase 5: Join integration

- Let Workflow or a Session supervisor display and reference Topic findings
  while retaining sole authority over joins.
- Resume synthesis once per satisfied join rather than once per Blackboard
  post.
- Verify recovery after Server restart and duplicate notifications.

## 19. Prototype Acceptance Criteria

A Phase 1 or Phase 2 prototype is successful only if:

1. A Task for a child Issue can read the parent objective, sibling status
   summary, and bounded recent Topic discussion without supplying an Issue ID.
2. The child can post a bounded report to the parent Topic with correct Space,
   Agent, Task, and TaskRun provenance.
3. The capability cannot read or write an unrelated parent, child, Issue, or
   Space even when malicious Topic text asks it to.
4. An Agent report cannot mutate Issue status, owner, executor, hierarchy,
   Workflow state, or another participant's execution policy.
5. A network retry does not create duplicate visible reports.
6. New posts do not interrupt a running assistant/tool sequence or
   automatically wake every participant.
7. Long history is bounded and the tool and UI disclose omitted content.
8. Agent-authored content reaches another model as attributed untrusted
   evidence.
9. A Workflow join still derives readiness from authoritative execution state,
   never from report prose.
10. People can distinguish sharing a finding, asking a participant to act, and
    waiting for several participants.

## 20. Open Questions

### Product

- Do users understand Blackboard as a filtered Issue view, or do they expect a
  separately named persistent object?
- Should every top-level Issue implicitly be a Topic, or should coordination be
  enabled only when it has multiple child executions?
- Is parent-plus-child Issue depth sufficient for real coordination groups?
- Which updates are useful enough to share before terminal results?

### Content and authority

- Can conventions over comments validate the experience, or does useful
  filtering require typed entries immediately?
- Which actor may publish an accepted decision, and where does that decision's
  authority live?
- Should a superseded entry remain in the normal Discussion view or only in
  history?
- Which Artifact relations should appear in a Topic snapshot?

### Delivery and scheduling

- How stale can pull-based coordination be before a durable subscription is
  justified?
- Which participants, if any, need per-Topic cursors?
- Is an addressed Signal permitted only through parent/supervisor relations, or
  do Issue executors justify another fixed route?
- Which updates may trigger a synthesis run, and who authorizes its budget?

### Ownership

- Does the Issue service own Topic derivation and the first comment-backed
  projection?
- If typed entries arrive, are they an Issue capability or a separate pure core
  coordination package?
- Does Workflow expose join state into the Topic through a read projection, or
  append system entries when its state changes?
- How does a local Issue-linked Session preserve its last observed Topic basis?

### Lifecycle

- What happens to Topic entries when a child is moved to another parent?
- How long should operational notices remain visible?
- Can a completed Topic reopen, and do old participants regain any capability?
- How are deleted or redacted entries represented without falsifying prior
  TaskRun provenance?

## 21. Evidence Needed Before Acceptance

- Real parent/child Issue journeys repeatedly produce cross-child findings that
  matter before terminal synthesis.
- Shared Topic context reduces duplicated work or contradictory assumptions
  compared with isolated child discussions.
- Participants read and act on the feed without a person continually copying
  messages.
- The useful posts are short, attributable coordination statements rather than
  raw transcripts or progress noise.
- Pull-based snapshots reveal whether push, subscriptions, or unread cursors
  are genuinely needed.
- Users distinguish published information from addressed requests and durable
  joins.
- Prompt-injection and authorization tests show that Topic text cannot choose a
  target, gain authority, or exfiltrate another Issue's data.
- Cost measurements show that context and wake-up policies do not multiply
  model calls faster than the useful coordination gained.
- Crash and retry tests show accepted posts are not lost or duplicated.
- Workspace experiments show which failures belong to communication and which
  require isolation or change-integration work instead.

Useful measurements include:

- Topic snapshot reads and explicit refreshes per TaskRun;
- posts per participant and proportion referenced during final synthesis;
- duplicate-work findings before and after the feature;
- time from relevant post to another participant observing it;
- unread or omitted entry counts;
- addressed Signal rate versus broad-post rate;
- model wake-ups and tokens caused by coordination;
- stale or superseded findings used by later work; and
- repeated use by the same teams after the novelty period.

## 22. Architecture Landing Areas

If evidence supports implementation, candidate ownership is:

| Responsibility | Candidate owner |
|---|---|
| Topic derivation from Issue hierarchy | `internal/service/issue` over pure `internal/core/issue` rules |
| Comment-backed Topic snapshot and report authorization | `internal/service/issue` |
| Model-facing scoped read/report commands | `buildmax` subcommands in `internal/interface/cli`, over worker routes and the run bridge (`internal/infra/runbridge`) |
| Worker credential and TaskRun-derived scope | worker client and Server worker handlers |
| Local authenticated scope | `internal/interface/client` and `internal/agentapp` |
| Optional typed entry domain | `internal/core/issue` unless evidence shows an independent reason to change |
| Typed entry persistence | `internal/infra/db` |
| Addressed delivery | the mailbox/supervisor service decided by its own proposal |
| Join and readiness | `internal/core/workflow` and `internal/service/workflow`, or the Session supervisor |
| Topic projection in Portal | Portal over Issue and coordination service responses |
| Workspace isolation and apply | the workspace capability, never the feed store |

The Agent Loop receives one bounded projection and emits tool calls. It does not
own Topic membership, delivery, subscription, or join state.

## 23. Destination If Accepted

If the evidence supports the direction:

1. Place the accepted slices and their prerequisites in `ROADMAP.md`.
2. Move durable Topic, authority, delivery, and synchronization decisions into
   the relevant design records rather than leaving one broad design behind.
3. Reconcile the accepted boundary with the Session mailbox and Agent-native
   collaboration proposals, retiring or narrowing superseded proposal text.
4. Create separate backlog items for the comment-backed prototype, typed feed
   if earned, mailbox integration, UI projection, and recovery evidence.
5. Update Issue, Task, Workflow, tool, Server, data-model, Portal, CLI, and
   Desktop documentation only as their behavior actually changes.
6. Add user documentation after a supported surface ships.
7. Delete this proposal once accepted, rejected, or superseded; Git history
   preserves the discussion.

If evidence shows that users only need terminal synthesis, improve existing
Task results and Workflow fan-in instead. If they need addressed questions but
not broad awareness, implement the restricted mailbox without a Blackboard. If
they only need cleaner human discussion, improve Issue Discussion and do not
introduce an Agent coordination subsystem.

## 24. Candidate Direction

The candidate direction is:

> BuildMax should treat a top-level Issue as the coordination Topic for its
> child work. Its Blackboard is a bounded, source-attributed, append-oriented
> feed, initially projected from existing Issue comments rather than introduced
> as a new entity. The feed shares information but does not guarantee delivery
> or decide readiness. Addressed action uses durable mailbox Signals;
> synchronization uses Workflow or supervisor-owned join conditions; workspace
> changes remain isolated and reviewable. Agent capabilities are scoped by
> construction, reports remain untrusted evidence, and automatic processing is
> explicit and budgeted.

This is narrower than a general collaboration substrate and safer than an
arbitrary Agent message bus. Whether it earns implementation depends on evidence
that shared Topic information improves real parallel Issue work before terminal
synthesis.
