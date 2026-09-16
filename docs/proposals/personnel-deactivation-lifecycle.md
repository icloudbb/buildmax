# Personnel Deactivation And Execution Authority

> **简体中文：** [阅读中文镜像](../zh-CN/proposals/personnel-deactivation-lifecycle.md)
>
> **Audience:** maintainers, operators, and security reviewers · **Status:** proposal — under discussion
>
> **Opened:** 2026-09-15
>
> **Primary domain:** Operations and Deployment

Related: [enterprise capability requirements](enterprise-capability-requirements.md),
[enterprise identity and access](../design/enterprise-identity-and-access.md),
[Space membership lifecycle](../design/space-membership-lifecycle.md),
[Agent execution and Task threads](../design/agent-execution-and-task-threads.md),
[scheduled Agent execution](../design/scheduled-agent-execution.md), and
[system administration operations](system-administration-operations.md).

## Contents

- [1. Decision Question](#1-decision-question)
- [2. Essential Outcome And Evidence](#2-essential-outcome-and-evidence)
- [3. Verified Current Constraints](#3-verified-current-constraints)
- [4. Invariants](#4-invariants)
- [5. Options](#5-options)
- [6. Recommended Contract](#6-recommended-contract)
- [7. Execution Eligibility](#7-execution-eligibility)
- [8. Races, Failure, And Recovery](#8-races-failure-and-recovery)
- [9. Ownership Recovery](#9-ownership-recovery)
- [10. API, Data, And Service Ownership](#10-api-data-and-service-ownership)
- [11. Delivery Slices](#11-delivery-slices)
- [12. Verification](#12-verification)
- [13. Non-Goals](#13-non-goals)
- [14. Open Questions And Decision Evidence](#14-open-questions-and-decision-evidence)
- [15. Likely Destination If Accepted](#15-likely-destination-if-accepted)

## 1. Decision Question

When a System Administrator disables an account, or a Space owner removes one
membership, which human credentials and unattended executions stop, how fast do
they stop, and what durable work remains available to the organization?

The recommendation is one authority contract rather than a new employee or
offboarding subsystem:

> `user.disabled_at` is the deployment-wide eligibility gate and the absence of
> a `space_member` row is the Space eligibility gate. Every path that admits or
> dispatches work checks those two facts. Revocation cancels work that has not
> completed, preserves history and results, and never silently resumes derived
> automation when authority is restored.

A guided leaver experience is a projection and a coordinated use of these
existing states. It is not a new `organization`, `employee`, `offboarding_job`,
or policy-language entity.

## 2. Essential Outcome And Evidence

An operator must be able to answer, before committing a personnel change:

1. what access stops immediately;
2. which unattended work will be paused or canceled;
3. which Space needs a successor owner; and
4. which results and audit history remain after the person can no longer sign
   in.

The need is not inferred from an enterprise feature checklist. OIDC login and
account association now exist, while the enterprise capability inventory names
the complete leaver journey as the first unclosed seam: sessions, machine
credentials, invitations, memberships, schedules, queued and running TaskRuns,
and retained results currently do not share one stated lifecycle.

The first accepted deployment target remains a trusted private Space. The goal
is a predictable authority boundary and bounded shutdown, not public
multi-tenant employment management.

## 3. Verified Current Constraints

Current code already supplies useful parts of the contract:

| Concern | Current behavior | Remaining gap |
|---|---|---|
| User requests | `access.Guard` checks both the active account and active `auth_session` on every request | No gap for human requests |
| Login and refresh | Disabled accounts are refused | IdP-only disablement is invisible until BuildMax reauthentication unless an external lifecycle channel is added |
| Sessions | The admin disable handler revokes all sessions after setting `disabled_at` | The two writes are not one transition; a failure returns an error after the authoritative gate has already changed |
| Webhook keys | A disabled owner's keys are refused but retained | Enabling the account makes old keys usable again; no leaver decision is presented |
| Space membership | Removing a row blocks the person's next Space request; account disablement keeps memberships | Schedule, Workflow, and dispatch paths do not all re-check membership |
| Schedules | The dispatcher pauses a due Schedule when its creator is disabled | It does not check whether the creator remains a member, and an infrequent Schedule may stay visibly enabled until its next due time |
| Pending TaskRuns | The scheduler marks a disabled creator's pending run failed before spawning a worker | It fails open on the account lookup, does not check membership, and reports an authority withdrawal as a failure rather than a cancellation |
| Running TaskRuns | Workers poll a durable cancel request and a reaper bounds an unanswered request | Account disablement and membership removal do not request cancellation |
| WorkflowRuns | Reconciliation durably dispatches later steps as the original run creator | It can admit another step after that account is disabled or its membership is removed |
| Run credential | A run token is scoped to one TaskRun | Its user claim is derived from `task.created_by`, even when a later TaskRun was initiated by another member |
| Results | Tasks, TaskRuns, Artifacts, traces, and model-call records are Space-owned and read through current membership | This is the correct retention boundary and must not be replaced with creator ownership |

Other constraints shape the minimum design:

- An account can belong to many Spaces and has one personal Space.
- Space owner, Space admin/member, and System Administrator are independent
  authorities. A system grant does not imply access to Space content.
- A TaskRun has an immutable admitted environment. Revocation cannot undo an
  external side effect that already happened or hot-edit a running process.
- `CANCELED` is a terminal TaskRun and WorkflowRun outcome distinct from
  `FAILED`; partial output and Artifacts remain evidence.
- PATs and service accounts do not exist. The only account-scoped unattended
  credential today is the webhook key.

## 4. Invariants

1. **Disablement beats convenience.** After `disabled_at` commits, no new human
   request, webhook admission, Schedule firing, Workflow step, Continue, Retry,
   or worker dispatch may acquire that user's authority.
2. **Membership is checked at execution boundaries.** Removing membership
   stops new work in that Space even when the work originated from a durable
   Schedule or Workflow rather than an HTTP handler.
3. **One TaskRun, one initiating principal.** Eligibility and the run token use
   `task_run.created_by`; `task.created_by` remains historical provenance for
   the continuing Task, not authority for every later turn.
4. **Already-started work is stopped, not rewritten.** Revocation records a
   cancel request. The worker reaches `CANCELED` through its ordinary graceful
   path; the stale-run backstop handles a worker that never answers.
5. **Space data outlives a person's access.** Memberships, Tasks, results,
   Artifacts, traces, usage, and audit records are not deleted or reassigned.
   Remaining members retain access through the Space.
6. **Restoration is explicit.** Enabling an account or re-adding a member does
   not resurrect revoked sessions, canceled runs, paused Schedules, or terminal
   WorkflowRuns.
7. **Security gates fail closed.** An inability to determine account or
   membership eligibility at admission or dispatch refuses or defers work; it
   never starts work on the optimistic assumption that authority probably
   remains.
8. **History keeps the original actor.** Deactivation does not replace creator
   IDs with the operator or successor. Recovery actions add their own typed
   audit actor.

## 5. Options

### Option A: Document The Existing Best-Effort Behavior

This is the smallest code change, but it leaves removed members able to drive
Schedules and later Workflow steps, and leaves a race between the scheduler's
account check and worker start. It does not satisfy the stated outcome.

### Option B: Delete Or Reassign Everything The Person Created

Deleting work loses organizational evidence. Automatically changing creator
IDs falsifies provenance, and transferring every object confuses authorship
with Space ownership. This option is rejected.

### Option C: Add A Durable Offboarding Job Entity

A job could track a large cascade, retries, and partial completion. No current
deployment has demonstrated a scale or approval process that requires another
state machine. The authoritative gates can make cleanup safe and a bounded
reconciler can make it convergent. This option remains available only if the
simple model proves operationally insufficient.

### Option D: Gate, Cancel, Preserve, And Reconcile

Use the account and membership states as authority, centralize execution
eligibility, request cancellation for active work, pause future triggers, and
retain Space-owned history. Add a read-only impact projection and narrowly
scoped owner recovery. This is the recommended option.

## 6. Recommended Contract

Two authority axes of different natures underlie the whole contract, and keeping
them distinct is what makes recovery predictable:

- **Deployment-wide account disablement is a reversible gate over retained
  state.** Memberships, Space data, and webhook keys are all kept and simply
  refused while `disabled_at` holds. Enabling reopens them with no resurrection
  step: the memberships were never removed, so they are effective again at once.
  A disabled account keeps its unique email: `email` stays a unique key, so the
  address cannot be registered to a second account while the first exists. The
  same person returns by re-enabling that account, not by creating a new one,
  and an email is never rebound to a different person. Freeing an address would
  require account deletion, which stays a non-goal (§13).
- **One-Space membership removal is a destructive change to a single
  relationship.** The `space_member` row is deleted, so a later re-invitation is
  a fresh join with a new `created_at`, not a restoration of the prior one.

Eligibility ANDs the two facts: the account is not disabled *and* a membership
row exists. Treat disablement as reversible suspension and removal as a
deleted-then-new relationship; do not expect the two to be symmetric.

The same resource has different treatment under deployment-wide account
disablement and one-Space membership removal:

| Resource or action | Account disabled | Membership removed from Space |
|---|---|---|
| Password, login code, OIDC login, refresh | Refuse all new sessions; revoke existing BuildMax sessions; keep the external-identity link for deliberate recovery | Unchanged |
| Existing access token and WebSocket | Refuse on the next authenticated request or connection check | Refuse on the next request naming that Space |
| Webhook keys | Refuse immediately while disabled; the guided leaver flow defaults to revoking them permanently | Unchanged unless a future Space-owned key exists |
| Pending invitation | Keep as non-authority; a disabled account cannot accept it | Invitations in other Spaces are unchanged; the removed Space has no accepted membership |
| Membership rows | Preserve for provenance and possible deliberate return | Remove only the named Space membership |
| Enabled Schedule created by the person | Pause with reason `creator_disabled` | Pause those in the removed Space with reason `creator_not_member` |
| PENDING TaskRun initiated by the person | Transition to `CANCELED` before dispatch | Cancel only those owned by the removed Space |
| SCHEDULED or RUNNING TaskRun | Record a cancellation request and let worker/reaper settle it | Same, limited to the removed Space |
| WorkflowRun | Do not dispatch another step; cancel its active TaskRun and settle the WorkflowRun as `canceled` | Same, limited to the removed Space |
| Completed TaskRun and WorkflowRun | Keep unchanged | Keep unchanged |
| Task, Issue, Artifact, trace, usage, and audit | Keep under Space ownership; the person cannot read them while disabled | Keep; remaining members retain access |
| Personal Space | Keep intact and inaccessible while the account is disabled | Not removable as an ordinary shared-Space membership |

The leaver flow distinguishes **suspension** from **credential retirement** only
as an operator choice, not as another account state. Both use `disabled_at` as
the gate. This is decided, not left open: a temporary suspension **retains** the
denied webhook keys so a deliberate re-enable restores the integration
unchanged, while the guided leaver path **permanently retires** them. The
default is retirement because an integration that must outlive a person needs a
separately designed machine principal, not a forgotten personal credential.

IdP-only offboarding remains bounded rather than immediate. Without SCIM or a
validated provider logout channel, BuildMax learns nothing when Okta disables a
person. The supported immediate procedure is therefore BuildMax account
disablement; the OIDC absolute-session lifetime is the maximum IdP-only bound
and must be recorded by the Phase 3 Okta qualification.

## 7. Execution Eligibility

Introduce one application-level question, conceptually:

```go
type ExecutionEligibility interface {
    Check(ctx context.Context, userID, spaceID string) error
}
```

It returns a typed refusal for a disabled or missing account, absent membership,
or an unavailable authority store. It does not answer role-specific management
questions; ordinary membership is enough to run work under current Space
policy.

The Task application service calls it for Create, Continue, Retry, Schedule
admission, and Workflow step admission. HTTP guards remain an early response
adapter, not the only enforcement. The Schedule dispatcher and Workflow
reconciler call the same rule before producing later work.

The scheduler performs a final check after claiming a PENDING run and before
minting a run token or starting a worker. The worker's initial run fetch repeats
the check so a disable or membership removal racing the scheduler cannot cross
the boundary. An authority-store error leaves the run diagnosably undispatched;
it does not mark a transient outage as permanent authorization loss.

The run token uses the TaskRun initiator. For a first run this normally matches
the Task creator. A Continue by another member truthfully carries that member,
and disabling the original Task creator does not cancel a later run initiated
by an eligible colleague.

## 8. Races, Failure, And Recovery

Authority revocation and cleanup have different correctness roles:

1. The account disable or membership removal commits first as the authoritative
   gate, with its audit event.
2. A set-based cleanup pauses affected Schedules, cancels PENDING runs, and
   requests cancellation for SCHEDULED/RUNNING runs.
3. A bounded reconciliation loop finds enabled Schedules and active runs whose
   initiating principal is no longer eligible and repeats those idempotent
   actions. It needs no offboarding job record because eligibility itself is the
   durable unfinished-work predicate.
4. Workflow reconciliation checks eligibility before every next-step dispatch;
   a refusal cancels the WorkflowRun and blocks remaining pending steps.

This ordering makes a cleanup failure inconvenient rather than authorizing:
all new admission and dispatch paths still refuse. The admin response reports
the gate outcome separately from cleanup counts so a timeout cannot make an
operator repeat or reverse the disablement blindly.

The cancellation bound is the worker cancel-poll interval plus the configured
cancel grace before the stale-run backstop settles an unanswered run. The
operator surface must report those effective values. Until the worker observes
the request, a tool call or external write already in progress may complete.
BuildMax does not claim rollback of external side effects.

Reconciliation is batch-bounded and idempotent. Concurrent disable, member
removal, dispatch, worker completion, and explicit cancellation resolve through
existing guarded transitions; a terminal run is never changed.

## 9. Ownership Recovery

A planned leaver impact report lists every shared Space for which the target is
the sole effective owner. The normal path is for that owner to transfer
ownership before account disablement.

Emergency disablement must not be blocked by an ownership problem. It may leave
a shared Space whose recorded owner cannot sign in. Recovery therefore needs
one narrow System Administrator action:

- it applies only when every recorded owner of a shared Space is disabled;
- the successor must already be an enabled member of that Space;
- it cannot target a personal Space or create a membership;
- it performs the existing ownership transfer atomically;
- it gives the administrator no Space membership or content access; and
- it records a transactional `space.ownership_recovered` audit event naming the
  disabled owner, successor, and human or operator actor.

Portal and `buildmax admin` may expose this metadata-only recovery through the
Admin API. A database-adjacent `buildmax-server` command may call the same
service for break glass when the public Server or IdP is unavailable. This is
not a general administrator power to transfer a healthy Space.

## 10. API, Data, And Service Ownership

No new top-level entity is proposed.

The existing account state route remains the single account enable/disable
mutation. Add a read-only impact projection before it:

```text
GET /api/admin/users/{user_id}/deactivation-impact
```

It returns metadata and counts only: live sessions and webhook keys; memberships
and roles; sole-owned shared Spaces; enabled Schedules by Space; active
TaskRuns/WorkflowRuns by status; and the configured cancellation bound. It
returns no prompt, input, output, Artifact name, trace, raw error, or Secret.

`PUT /api/admin/users/{user_id}/state` reports separately:

- whether the account gate changed;
- sessions and webhook keys revoked;
- Schedules paused;
- PENDING runs canceled; and
- active runs whose cancellation was requested.

The owning service, not the handler, coordinates this outcome. A narrow
`internal/service/accountlifecycle` capability may depend on account, session,
webhook-key, Schedule, TaskRun, WorkflowRun, Space, and audit ports. Domain
packages keep their own transition validation; the orchestrator does not
reimplement it.

Two small provenance fields are justified by operator diagnosis:

- `schedule.pause_reason`: `manual`, `creator_disabled`,
  `creator_not_member`, or `consecutive_failures`; enabling clears it.
- `task_run.cancel_reason`: `user_requested`, `creator_disabled`, or
  `creator_not_member`; it is immutable once cancellation is first requested.

The exact actor continues to use existing actor fields and audit records. A
reconciler may leave `cancel_requested_by` empty and set the reason; the initiating
disable or removal event remains linked through user and Space metadata. No
free-text personnel reason is stored.

Membership removal remains owned by `internal/service/space`. It commits the
membership transition and audit, then invokes the shared cleanup capability for
that user and Space. Account disablement invokes the deployment-wide form. The
eligibility check is shared; neither service imports the other's orchestration.

Removal stays a hard delete of the single `space_member` row, matching the
current store. A departure's provenance lives in the paired
`space.member_removed`/`space.member_added` audit events, not in a retained or
soft-deleted row, so a returning member is a new row with a new `created_at`,
consistent with explicit, non-resurrecting restoration (Invariant 6). No
soft-delete column is added: nothing in this contract needs one, and a retained
row would collide with the `space_member` unique index on `(space_id, user_id)`.

## 11. Delivery Slices

Each slice is independently reviewable after the proposal is accepted:

1. **Central execution eligibility.** Add service-level admission checks,
   membership-aware Schedule/Workflow dispatch, the final worker-start gate,
   and TaskRun-initiator run tokens. This closes authority bypasses without a
   new UI.
2. **Convergent cancellation and pause.** Add reason fields, set-based cleanup,
   and the bounded reconciler. Change authority-withdrawal outcomes from
   `FAILED` to `CANCELED`.
3. **Account deactivation impact and credential handling.** Add the metadata-only
   impact route, move disable orchestration out of the handler, keep session
   revocation non-resurrecting, and offer permanent webhook-key retirement.
4. **Space owner recovery.** Add the disabled-owner-only recovery service,
   authenticated clients, transactional audit, and MySQL concurrency tests.
5. **Guided Portal and operator procedure.** Present impact, successor blockers,
   explicit in-flight limits, cleanup outcome, and post-action verification.
6. **Okta qualification.** Exercise BuildMax disablement, the IdP-only bound,
   IdP outage, break glass, and return without authority resurrection in the
   Phase 3 provider environment.

This proposal does not itself admit those tasks to the backlog or reorder R2/R3.
The roadmap and maintainer still decide when each slice enters execution.

## 12. Verification

| Scope | Required evidence |
|---|---|
| Core/service | Table-driven matrix for account state, membership, trigger source, TaskRun initiator, and every lifecycle outcome |
| Handler | Impact responses are metadata-only; disabled and removed callers are refused on every relevant route |
| MySQL | Disable/removal racing Schedule claim, Workflow reconcile, PENDING claim, run completion, cancellation, and ownership recovery converges to one legal state |
| Worker | A revocation between scheduler claim and worker fetch starts no Agent; a running Agent observes cancellation and preserves partial evidence |
| Multi-replica | Two Servers cannot dispatch a later step or Schedule after authority is withdrawn; cleanup is idempotent across replicas |
| Portal | Planned and emergency leaver journeys show impact, blocker, bounded in-flight state, and final verification without Space content leakage |
| OIDC | Manual BuildMax disablement is immediate; IdP-only disablement stays within the documented absolute-session bound; break glass works during IdP outage |
| Authorization | System Administrator recovery cannot read Space content, transfer a healthy Space, create a member, or touch a personal Space |
| Documentation | Current state, authentication, administration, scheduling, Task execution, OpenAPI, and the Enterprise identity Phase 3 evidence remain aligned |

The principal acceptance scenario is one person with two Sessions, one webhook
key, memberships in two shared Spaces, sole ownership of one, Schedules in both,
a PENDING run, a RUNNING run, and a multi-step WorkflowRun. The test must show
the exact state after account disablement, after owner recovery, and after a
deliberate re-enable.

A second acceptance scenario exercises the Space axis symmetrically to the
re-enable path. A member with an enabled Schedule, a PENDING run, and a RUNNING
run in one shared Space is removed from it. The test must show that Space's
derived work paused or canceled with reason `creator_not_member`, that the same
person's work and access in the other Space is untouched, that Space-owned
results stay readable to remaining members, and that a later re-invitation
produces a fresh membership with a new `created_at` that resurrects none of the
paused Schedules or canceled runs.

## 13. Non-Goals

- SCIM, SAML, directory synchronization, or IdP group-to-Space mapping.
- Account deletion, email reassignment, account merge, or transfer of a personal
  Space.
- PATs, service accounts, workload identity, or automatic transfer of personal
  webhook keys.
- Reassigning creator fields or deleting completed work.
- A generic employment, HR, Organization, approval, or policy engine.
- Guaranteed rollback of model calls, tool calls, or external side effects that
  began before cancellation was observed.
- Automatically resuming Schedules or retrying canceled work after re-enable or
  re-invitation.
- Claiming compliance-grade audit before authority transitions and their audit
  records have the required transactional semantics.

## 14. Open Questions And Decision Evidence

1. Is the worker poll plus cancel-grace bound acceptable for the first named
   enterprise deployment, or must the runner also delete/terminate the worker
   after a shorter emergency bound?
2. Is disabled-owner-only System Administrator recovery acceptable, or must
   every deployment require a second Space owner before offboarding?
3. Does the target deployment require managed CLI/Desktop SSO, adding native
   Session and local credential cleanup to the same journey?
4. At what observed cardinality would the eligibility reconciler need a durable
   work queue rather than bounded SQL scans?

Acceptance requires a named operator and target deployment, an agreed
offboarding and running-work bound, a walkthrough of planned and emergency
departure, a threat-model review of worker cancellation, and evidence that the
metadata-only impact response reveals no Space content.

## 15. Likely Destination If Accepted

Move the durable eligibility and retention decisions into the Agent execution,
scheduled execution, Space membership, and Enterprise identity design records.
Put the operator procedure in deployment authentication/administration
documentation, place accepted priority in the roadmap, and create one backlog
task per delivery slice. Then delete this proposal; git history preserves the
decision process.
