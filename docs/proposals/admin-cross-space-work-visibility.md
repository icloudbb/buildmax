# Cross-Space Work Visibility for Deployment Administrators

> **简体中文：** [阅读中文镜像](../zh-CN/proposals/admin-cross-space-work-visibility.md)
>
> **Audience:** operators, product reviewers, and security reviewers · **Status:** proposal — under discussion
>
> **Opened:** 2026-09-21

Related: [roadmap](../ROADMAP.md),
[current state](../current-state.md),
[system administration design](../design/system-administration.md),
[system administration operations proposal](system-administration-operations.md),
[Space governance](../design/space-governance.md), and
[server architecture](../contribute/architecture/server.md).

## Contents

- [1. Decision Question and Candidate Direction](#1-decision-question-and-candidate-direction)
- [2. User Outcome and Evidence](#2-user-outcome-and-evidence)
- [3. Current Boundaries](#3-current-boundaries)
- [4. Goals and Non-Goals](#4-goals-and-non-goals)
- [5. What Each Caller Needs](#5-what-each-caller-needs)
- [6. Candidate Product and Authority Model](#6-candidate-product-and-authority-model)
- [7. Options and Trade-Offs](#7-options-and-trade-offs)
- [8. First Slice and Verification](#8-first-slice-and-verification)
- [9. Open Questions and Decision Evidence](#9-open-questions-and-decision-evidence)
- [10. Likely Destination If Accepted](#10-likely-destination-if-accepted)

## 1. Decision Question and Candidate Direction

Should a System Administrator be able to see and manage Agents, Workflows,
Schedules, and Issues across every Space? If so, should the administrator enter
each Space with implicit authority, or use a deployment-wide Administration page?

The candidate direction is a **split by user outcome**:

1. Administration shows deployment-wide **operational metadata**: whether work
   is moving, failing, or consuming capacity, and which Space needs attention.
   It does not list the four Space-owned content collections merely because a
   System Administrator opened the page.
2. A person who belongs to several Spaces may get one navigation or work view
   over **their authorized Spaces**. Opening an item and changing it continue to
   use its ordinary Space route and role check, even if reached from a combined
   view. The System Administrator grant adds nothing to that set.
3. A non-member's need to inspect or change one Space's content is a separate
   support-access decision. No implicit entry to every Space is proposed here.

This is a proposal about the boundary and evidence for such views, not a
commitment to add all three surfaces. It preserves the accepted distinction:
deployment administration operates the system; Space membership authorizes
access to a Space's work.

## 2. User Outcome and Evidence

The essential operator outcome is to answer, during an incident, **whether
BuildMax is processing work, where progress stopped, and who can act**, without
reading a team's instructions or work product. A different outcome belongs to
a person responsible for several Spaces: find and manage their own authorized
work without repeatedly changing Space context.

The question arose from product discussion about one place for administrators
to see Agents, Workflows, Schedules, and Issues across Spaces. That is evidence
of a navigation and operations question, not evidence that deployment
administrators need the content of every Space. No measured incident, operator
walkthrough, or multi-Space work study yet distinguishes the two needs. The
proposal therefore keeps the broader access decision open and defines the
smallest evidence that could justify each slice.

## 3. Current Boundaries

- The accepted [system administration design](../design/system-administration.md)
  gives a System Administrator deployment authority, not Space membership.
  System administrators without membership cannot read Space prompts, Issue
  text, generated output, files, artifacts, or traces.
- Administration already lists Space metadata, membership, quota tier, and
  aggregate usage; it searches cross-Space audit records and the managed LLM
  call ledger. Its system view includes TaskRun counts by status. These are
  bounded operational projections, not Agent, Workflow, Schedule, or Issue
  inventories.
- Agents, Workflows, Schedules, and Issues use Space-scoped routes and
  authorization. A browser navigation change cannot safely change that
  authority. A global Admin page returning their names or definitions would
  grant content access just as surely as a bypass on the Space routes.
- The [system administration operations proposal](system-administration-operations.md)
  already owns general runtime health, capacity, and operator-surface parity.
  This paper owns the narrower question of crossing from that metadata into
  named Space work and of presenting work from Spaces where a user is already
  a member. It does not duplicate the runtime dashboard plan.
- [R2 and R3](../ROADMAP.md) still require lifecycle and candidate operating
  evidence. A new cross-Space surface becomes a Beta requirement only if an
  operator journey demonstrates that the existing views cannot support a
  required diagnosis or recovery.

## 4. Goals and Non-Goals

Goals:

- Give an operator a truthful deployment-wide signal about work progress and
  capacity, with a route to the responsible Space or operator.
- Let a person with access to several Spaces find their authorized work without
  widening what the System Administrator grant can read or change.
- Make the authority in each view legible: deployment grant for operational
  metadata; Space membership and role for work content and mutations.
- Specify evidence and a verification boundary before adding broad queries or
  new administrative actions.

Non-goals:

- Making System Administrator a universal Space owner, silently enrolling
  administrators as members, or bypassing Space authorization in an Admin page.
- A deployment-wide search of Agent prompts, Workflow definitions, Schedule
  prompts, Issue titles or descriptions, files, outputs, or traces.
- Bulk edit, publish, pause, cancel, retry, or delete across Spaces.
- A support-access grant, break-glass content access, or a new global role.
  Such authority needs its own consent, expiry, revocation, and audit design.
- A second persistent copy of Space work or a generic Admin store over every
  domain table.

## 5. What Each Caller Needs

| Caller and job | Useful view | Authority |
|---|---|---|
| Deployment operator diagnosing stalled or costly execution | Deployment-wide TaskRun status and age, safe failure classes, dependency health, and Space-level capacity pressure | System Administrator grant; no Space content |
| Person managing work in several Spaces | Combined navigation or work list limited to their memberships; ordinary detail and actions | Space membership and role checked for every item |
| Space owner or admin governing one Space | Existing Agent, Workflow, Schedule, and Issue pages | That Space's role policy |
| Support person needing to inspect another Space's content | No general access is established by this proposal | Separate, evidence-backed support-access decision |

The four proposed resource types are not interchangeable operational records.
An Agent may contain instructions; a Workflow may contain task prompts and
bindings; a Schedule contains an instruction to run; an Issue contains a
person's description and results. Their names can themselves reveal work.
Resource type alone is therefore not a safe rule for an Admin response. The
existing admin response allowlist permits run statuses, timestamps, aggregate
usage, and opaque identifiers as operational metadata; each additional field
still needs a concrete operator question and a leak review.

## 6. Candidate Product and Authority Model

### 6.1 Administration remains an operations view

The Administration Overview and Spaces area answer the operator's question
from deployment-wide, durable facts. The existing TaskRun counts and per-Space
usage are the starting point. If an incident drill shows they are insufficient,
the [runtime operations proposal](system-administration-operations.md) can add
bounded status and age aggregates, safe error classes, or other metadata that
shortens diagnosis. It should not acquire four content list endpoints merely
to fill four tabs.

A projection must describe the whole deployment under multiple Server replicas,
not one replica's in-memory state. Partial dependency failures should be shown
as unavailable data, not as zero work. Dedicated response types should exclude
user-authored and Agent-produced content, including raw errors and trace text.

### 6.2 Authorized work may be combined without a new grant

If multi-Space users demonstrate a navigation problem, Portal may offer an
"Across my Spaces" entry point. Its set of Spaces derives from the user's
current memberships. Results identify their Space, and detail or mutation goes
through the existing Space-scoped contract with a fresh role check. A revoked
membership removes access on the next request; a stale card cannot confer
authority. This view need not live in Administration, because it is useful to
any multi-Space member.

The first implementation should reuse per-Space queries unless measured scale
requires a dedicated aggregate query. If one is justified, it must apply the
same membership filter and Space policy on the server; filtering a global
response only in the browser is not authorization.

### 6.3 Exceptional content access is a separate decision

The current design permits an administrator to recover ownership for a shared
Space whose owners are all disabled, promoting an existing enabled member. It
does not grant that administrator content access. If a real support or emergency
journey requires direct access, decide its scope, who authorizes it, duration,
revocation, notification to the Space, audit, and interaction with current
membership first. Do not disguise that new authority as an Admin page link.

## 7. Options and Trade-Offs

| Option | What it solves | Cost or failure mode |
|---|---|---|
| A. Implicitly let System Administrators enter every Space | Direct access to every existing work page | Reverses the accepted Space content boundary; every Space route and action becomes a global privilege |
| B. List and manage all four resource types in Administration | One location for an administrator | Still grants the same cross-Space content and mutation authority through different routes; duplicates Space workflows |
| C. Split operational metadata from membership-scoped work (candidate) | Answers deployment incidents and multi-Space navigation separately | Requires evidence to choose each projection and clear labels for the authority in use |
| D. Keep current surfaces only | No new authorization or query work | Leaves any demonstrated cross-Space operator or member navigation problem unsolved |

Option C adds the least authority and state that can satisfy the two demonstrated
kinds of question. Options A or B would require an explicit change to the
accepted system-administration decision, with a new threat model and policy;
putting content in Admin does not make it less sensitive.

## 8. First Slice and Verification

First run a bounded operator journey: diagnose a queued, failed, or apparently
stuck run in a multi-Space, two-Server deployment using the current
Administration and Space views. Record the question the operator could not
answer, the missing field, the action taken, and whether it required content.
If the gap is operational, add only the corresponding metadata projection to
the existing runtime-operations work and repeat the journey. Independently,
observe a user who manages several Spaces before building combined work
navigation.

Any implemented slice needs:

- Authorization tests for a System Administrator without membership, a Space
  owner without a system grant, a multi-Space member, a revoked member, and an
  anonymous caller. Admin status must never make a Space content route pass.
- Response leak tests using distinctive Agent instructions, Workflow inputs,
  Schedule prompts, Issue text, generated output, and raw errors. Neither
  bodies, logs, audit entries, nor UI state should reveal them through an
  operations projection.
- Pagination and query bounds on any new listing; data from durable state or a
  deployment-wide coordinator so a two-Server view is truthful.
- A Portal journey proving a membership-scoped combined view, if built, loses
  access after membership revocation and cannot mutate through a stale result.

No product behavior changes in this proposal itself, so it adds no changelog
entry or new Beta gate.

## 9. Open Questions and Decision Evidence

- Which operator incident requires more than current TaskRun counts, Space
  usage, audit, and the planned runtime health aggregates? What exact metadata
  would have shortened time to diagnosis?
- Does a multi-Space member actually need one combined work list, or is faster
  Space switching enough? Which of Agents, Workflows, Schedules, and Issues do
  they navigate across in the same task?
- If the operator needs to stop a runaway run without Space membership, what
  authority, audit, notification, and multi-replica semantics justify that
  specific action? This paper does not authorize it.
- Is there a demonstrated support case that cannot be handled by a Space owner
  or the existing ownership-recovery operation? If so, what explicit temporary
  access model would the Space accept?
- What fields remain useful when names, prompts, raw errors, and content are
  removed from an Admin result? Test with realistic, sensitive fixtures rather
  than a field-name checklist alone.

Acceptance requires an operator drill or multi-Space user observation that
identifies a missing outcome, plus a privacy and authorization review of the
smallest response that solves it. Familiarity with a global table is not
evidence of a need for global content authority.

## 10. Likely Destination If Accepted

Record any accepted authority decision in the
[system administration design](../design/system-administration.md) and the
affected Space governance record. Put an evidenced runtime metadata slice in
the [system administration operations proposal](system-administration-operations.md)
or its accepted successor; put any member-only work view in Portal's Space
navigation plan. Add roadmap and backlog work only after the corresponding
decision is accepted. A support-access design, if justified, remains its own
decision. Then retire this proposal; Git history keeps the discussion.
