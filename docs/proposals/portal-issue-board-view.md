# Portal Issue Board As A Derived Work View

> **简体中文：** [阅读中文镜像](../zh-CN/proposals/portal-issue-board-view.md)
>
> **Audience:** product reviewers, Portal contributors, and early adopters · **Status:** proposal — under discussion
>
> **Opened:** 2026-09-16

Related: [roadmap](../ROADMAP.md),
[product vision](../design/product-vision.md),
[surface positioning](../design/surface-positioning.md),
[Portal work and execution experience](../design/portal-work-and-execution-experience.md),
[Portal state and permission feedback](../design/portal-state-and-permission-feedback.md),
[Portal responsive and accessible interaction](../design/portal-responsive-and-accessible-interaction.md),
[data model](../contribute/architecture/data-model.md),
[Portal Issue manual](../../manual/portal-issues.md), and the
[Local Issue work bridge proposal](local-issue-work-bridge.md).

## Contents

- [1. Decision Question And Candidate Recommendation](#1-decision-question-and-candidate-recommendation)
- [2. User Outcome And Current Evidence](#2-user-outcome-and-current-evidence)
- [3. Relationship To Existing Decisions](#3-relationship-to-existing-decisions)
- [4. Goals And Non-Goals](#4-goals-and-non-goals)
- [5. Product Model And Authority](#5-product-model-and-authority)
- [6. Board Experience](#6-board-experience)
- [7. Loading, Pagination, And Ordering](#7-loading-pagination-and-ordering)
- [8. Status Mutation And Consistency](#8-status-mutation-and-consistency)
- [9. Interaction And Accessibility](#9-interaction-and-accessibility)
- [10. Options And Trade-Offs](#10-options-and-trade-offs)
- [11. Candidate First Slice](#11-candidate-first-slice)
- [12. Evidence And Delivery](#12-evidence-and-delivery)
- [13. Acceptance Criteria](#13-acceptance-criteria)
- [14. Open Questions](#14-open-questions)
- [15. Likely Destination If Accepted](#15-likely-destination-if-accepted)

## 1. Decision Question And Candidate Recommendation

Should Portal offer a board mode for Space Issues, and if so, how much board
product should BuildMax own before evidence justifies a planning system?

The candidate recommendation is:

> Add an evidence-gated **Board** view as another projection of the existing
> Issue collection. Use the three authoritative Issue statuses as fixed lanes,
> let an explicit user action move an Issue between them, and keep List as an
> equal view. Do not create a Board entity, configurable columns, independent
> card order, WIP limits, sprints, or automatic movement from execution state.

This direction fits Portal's role as the Space operation layer, but it is not
part of the current private-deployment Beta gate. It should enter the roadmap
only when an operator journey or early adoption shows that the existing list
makes shared work materially harder to scan or advance. A familiar visual
pattern by itself is not sufficient evidence.

## 2. User Outcome And Current Evidence

The essential outcome is:

> A Space participant can see how shared work is distributed across business
> states, identify who or what owns the next action, and deliberately change
> that state without opening every Issue or confusing work status with runtime
> status.

Current foundations make a narrow board possible without a new domain model:

- Issue is the primary user-facing shared work object, while Task and TaskRun
  own execution.
- Issue has exactly three business statuses: `todo`, `in_progress`, and `done`.
- A human Owner and an Agent or Workflow Executor are independent, so a card
  can show accountability and execution without inventing a combined assignee.
- One level of sub-Issues provides decomposition, with derived child progress
  already available on parent responses.
- The Space Issue route already filters by status, Owner, Executor, and parent,
  and returns a total for each filtered collection.
- Every Issue update carries a version, so a stale card can be refused rather
  than overwriting a newer edit.

Portal currently renders one ten-item page of top-level Issues. That page is a
useful reading surface, but grouping it in memory would not produce a truthful
board: a lane could look empty only because its Issues fall outside the current
unfiltered page. A board therefore begins with collection semantics, not card
styling.

## 3. Relationship To Existing Decisions

This proposal owns one narrow question: the collection-level presentation and
status interaction for Space Issues in Portal. It does not reopen these nearby
decisions:

| Existing document | What it owns | Relationship to this proposal |
|---|---|---|
| [Product vision](../design/product-vision.md) and [surface positioning](../design/surface-positioning.md) | Issue as the primary shared work object; Portal as the Space operation layer | Establish where a whole-Space work view belongs, but do not decide that it must be a board |
| [Portal work and execution experience](../design/portal-work-and-execution-experience.md) | Issue detail, Owner/Executor/Trigger, explicit Run, results, and provenance | Supplies status and execution boundaries that Board must preserve |
| [Portal state and permission feedback](../design/portal-state-and-permission-feedback.md) | Shared remote-resource states, stale data, errors, permission feedback, and retry | Remains authoritative; this proposal only applies it independently to each lane |
| [Portal responsive and accessible interaction](../design/portal-responsive-and-accessible-interaction.md) | Portal-wide keyboard, focus, target-size, feedback, motion, and narrow-layout rules | Remains authoritative; this proposal only adds Board-specific interaction requirements |
| [Local Issue work bridge proposal](local-issue-work-bridge.md) | How CLI/TUI and Desktop receive, execute, and report Space Issue work | Its matrix assumes full Portal filters and a board to draw a surface boundary; that is supporting context from another open proposal, not an accepted Board decision |
| [Issue topic coordination proposal](issue-topic-coordination.md) | Parent/child Issue coordination information for people and Agents | Shares the “derived view, not another source of truth” principle, but does not own status lanes or whole-Space work management |

Keeping this decision separate prevents a Portal collection view from inheriting
the Local Issue bridge's lifecycle and prevents that cross-surface proposal from
absorbing unrelated card, pagination, and interaction choices.

## 4. Goals And Non-Goals

Goals:

- Make waiting, active, and completed Space work legible at a glance.
- Preserve one Issue identity, lifecycle, authorization boundary, and update
  path across List, Board, detail, CLI/TUI, and Desktop.
- Show Owner, Executor, and child progress without treating any of them as the
  status authority.
- Keep large collections honest through per-lane totals and incremental loading.
- Let a user change status through the existing optimistic-concurrency contract.
- Learn whether the view improves real coordination before adding planning
  concepts.

Non-goals:

- Creating a `board`, `lane`, `card`, `sprint`, `backlog`, or `rank` entity.
- Adding custom columns, WIP limits, estimates, priorities, labels, due dates,
  saved views, sprints, or manual lane order.
- Automatically moving an Issue because an Agent started, a TaskRun finished,
  a Workflow failed, or all children became `done`.
- Starting an Agent or Workflow run by moving a card.
- Showing sub-Issues as peers of their parents on the whole-Space board.
- Rebuilding the Portal board in CLI/TUI or Desktop.
- Treating the board as a Beta prerequisite without operator evidence.

## 5. Product Model And Authority

Board is a read and mutation surface over Issue. It owns no durable business
state.

| Concern | Authority | Board responsibility |
|---|---|---|
| Issue identity, content, status, Owner, Executor, hierarchy, and version | Existing Issue service and store | Render returned state and call existing mutations |
| Lane membership | `issue.status` | Derive one lane; never persist a second lane value |
| Status change | Existing Issue authorization and version check | Make an explicit request and present success, denial, or conflict |
| Execution lifecycle | Task, TaskRun, and Workflow services | Link to execution detail; never infer lane movement |
| Child progress | Derived Issue child counts | Show progress; never store or roll up status |
| Filters | Existing list query contract | Apply the same filter set to every lane |

Switching between List and Board cannot create, copy, or reclassify an Issue.
A card uses the same Issue detail URL as the list.

## 6. Board Experience

### 6.1 Fixed lanes

The first board has exactly three lanes in domain order:

| Lane label | Machine status | Meaning |
|---|---|---|
| To do | `todo` | The Space has not declared work active |
| In progress | `in_progress` | The Space declares work active; this does not prove a process is alive |
| Done | `done` | The Space considers the work complete; execution remains separately inspectable |

The labels use the shared presentation vocabulary rather than raw enum strings.

### 6.2 Scope and cards

The whole-Space board contains top-level Issues only, matching the current list
and keeping one card equal to one shared outcome. A parent card shows derived
child progress and links to detail for the child breakdown. Parent and child
statuses remain independent.

A first-slice card shows title, human Owner or **Unowned**, Agent or Workflow
Executor or **No executor**, child progress, and update time. Description,
discussion, results, runs, and complete children remain on Issue detail.
Runtime status may later appear as secondary metadata, but it never chooses the
lane or substitutes for Issue status.

### 6.3 Filters and navigation

List and Board share one filter vocabulary. The candidate first slice supports
Owner, including **Me**, and Executor because the Server already owns those
semantics. Every active filter applies identically to all lanes.

View and filters live in Portal route or query state so reload and a copied link
reproduce the projection. That is navigation state, not a saved-view entity.
Whether the browser remembers a person's last view outside the URL remains an
open usability question.

## 7. Loading, Pagination, And Ordering

The first implementation uses the existing list contract rather than adding a
board endpoint:

```text
GET /api/spaces/{space_id}/issues?parent_id=none&status=todo
GET /api/spaces/{space_id}/issues?parent_id=none&status=in_progress
GET /api/spaces/{space_id}/issues?parent_id=none&status=done
```

The requests run concurrently and carry the same Owner and Executor filters.
Each lane independently owns its items, total, pagination, and resource state.
The shared Portal state model governs loading, empty, stale, error, forbidden,
and retry presentation. A failed lane must not silently become an empty one;
successfully loaded lanes may remain visible only with an explicit partial-board
warning.

Issues retain the collection's `updated_at` descending order. A move updates an
Issue and therefore places it near the top of the destination lane after
refresh. There is no manual rank or persisted within-lane drag position.

An aggregate endpoint is justified only if measurement shows that three
filtered requests create material latency or database load. It would remain a
read projection over Issue, not a second write model.

## 8. Status Mutation And Consistency

A move is an ordinary versioned Issue status update:

1. the user chooses a destination through drag, keyboard, or **Move to…**;
2. Portal sends the existing `PATCH` with the card's version and new status;
3. only an accepted response becomes the card's new authoritative state; and
4. the affected lane totals are refreshed or reconciled.

A move changes no Owner or Executor and never invokes Run. The card cannot move
again while its request is pending.

A version conflict is not retried automatically. Portal explains that the
Issue changed, reloads authoritative state, and asks the user to decide again.
Authorization, validation, and network failures keep the card in its
authoritative lane and follow the existing mutation-feedback contract.

Separately loaded lanes may briefly disagree during concurrent moves. Board is
an eventually refreshed operational view, not a transactionally frozen
snapshot, and must not claim that all lanes were observed at one database
instant.

## 9. Interaction And Accessibility

The existing Portal responsive and accessibility record remains authoritative.
Board adds three specific requirements:

- drag is an enhancement, not the contract; every move is available through a
  named **Move to…** action for keyboard, assistive technology, touch, and
  pointer;
- success, conflict, and failure restore or retain focus on a meaningful card
  or lane target and announce the result; and
- a narrow layout may use horizontal lanes or one selected lane, but lane
  identity, total, move actions, and access to List remain visible.

The first slice does not need drag. It may ship only when it has interaction and
test parity with the non-drag path.

## 10. Options And Trade-Offs

### Option A: Keep The List Only

This is sufficient for Spaces with few active Issues and adds no interaction
surface. As the collection spans pages, status distribution and repeated status
changes become harder to understand. Keep this option if real use does not show
that cost.

### Option B: Client-Group One Unfiltered Page

Fetch the current list page once and group its items into columns. Pagination
would occur before grouping, making lane contents and counts invisibly
incomplete. This option is rejected.

### Option C: Derived Three-Lane Issue Board — Candidate Recommendation

Query the existing collection once per status and use the existing update path
for moves. This closes the scan-and-move outcome without schema or durable
concepts, at the cost of more Portal collection state and careful concurrency
and accessibility handling.

### Option D: Configurable Planning Board

Add custom columns, rank, WIP rules, saved views, swimlanes, and sprints. No
demonstrated BuildMax outcome currently requires these independent concepts,
and they risk competing with dedicated planning products. Reject this option
until separate evidence supports it.

## 11. Candidate First Slice

1. Add a List / Board switch without changing the default view.
2. Extend the Portal Issue client to express existing top-level, status, Owner,
   and Executor filters.
3. Load the first page and total for all three lanes concurrently.
4. Render top-level cards with title, Owner, Executor, child progress, and
   update time.
5. Add the accessible **Move to…** action through the existing versioned Issue
   patch; omit drag unless it has full parity.
6. Give every lane independent pagination and shared-model resource states.
7. Preserve filters and view in navigation state and link cards to Issue detail.

This slice adds no database migration, Server entity, write route, manual
ordering, saved view, or automatic execution behavior.

## 12. Evidence And Delivery

Before roadmap commitment, observe the R3 operator journey and early Space use
for repeated attempts to compare Issues by status, locate the next Owner or
Executor, change several statuses, or maintain an external board because Portal
cannot show shared work state. Record the shape of active top-level Issue
collections, but do not use an arbitrary item-count threshold as the decision.

If evidence supports the direction:

1. validate a read-only prototype with real per-lane pagination and filters;
2. test whether participants distinguish Issue business status, Owner,
   Executor, and TaskRun state;
3. add the accessible status action and exercise concurrent edits, partial
   failure, slow requests, permission denial, and active filters; and
4. consider drag only if observation shows that it materially improves repeated
   status changes.

## 13. Acceptance Criteria

- List and Board show the same Issue identities under the same filters.
- Board has exactly the three existing statuses and persists no lane state
  outside Issue.
- Each lane is queried and paginated independently and displays its total.
- Failed or stale data is visibly distinct from an empty lane.
- A move uses the Issue version the card was loaded with, never overwrites a
  concurrent edit, and never schedules execution.
- A version conflict reloads authoritative state and requires a new decision.
- Top-level cards show derived child progress without changing parent or child
  status.
- Every drag action, if drag exists, has an equivalent named non-drag action.
- Narrow layouts retain lane identity, totals, move actions, and List access.
- Component, API, and Portal browser tests cover lane state, filters, conflict
  feedback, partial failure, and accessible movement at their owning boundary.
- The Portal manual and current-state assessment change only when the feature
  ships.

## 14. Open Questions

1. Do real Spaces accumulate enough concurrently relevant top-level Issues for
   lanes to improve decisions over filters and a denser list?
2. Is Board primarily a scan surface, or do users repeatedly change several
   statuses in one visit?
3. Should List remain the universal default, or should the browser remember the
   last view locally?
4. Are Owner and Executor filters sufficient, or is text search a prerequisite
   for large-Space navigation?
5. Is update-time order stable enough, or does observed work justify a separate
   priority concept? Drag alone is not evidence for manual rank.
6. Do teams need a parent-scoped child board, or is top-level progress plus
   Issue detail sufficient?
7. Are three concurrent filtered requests acceptable at realistic Issue counts
   on the qualified private deployment?
8. Which narrow-screen presentation performs better: horizontal lanes or one
   selected lane?

Evidence should come from operator journeys, bounded usability sessions,
support or discussion reports, and measured Portal/API behavior. Competitive
feature lists and Kanban's prevalence elsewhere do not establish a BuildMax
requirement.

## 15. Likely Destination If Accepted

Acceptance would:

1. add an evidence-backed Portal Issue-board outcome to the post-Beta roadmap;
2. move the durable rationale — Board is a projection, status remains explicit,
   and execution never moves it implicitly — into
   [Portal work and execution experience](../design/portal-work-and-execution-experience.md);
3. create focused backlog work for collection state, presentation, mutation and
   accessibility behavior, and browser verification;
4. update the Portal manual and current-state assessment when the slice ships;
   and
5. delete this proposal.

Rejection would retain List and its filters as the complete Portal Issue
collection surface. Evidence for external planning integration would be
evaluated separately rather than used to keep this proposal open indefinitely.
