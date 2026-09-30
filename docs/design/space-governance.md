# Space Governance Foundation

> **简体中文：** [阅读中文镜像](../zh-CN/design/Space治理.md)

## Contents

- [Status](#status)
- [1. Decision](#1-decision)
- [2. Product Goal](#2-product-goal)
- [3. Current Baseline](#3-current-baseline)
- [4. Main Gaps](#4-main-gaps)
- [5. In Scope](#5-in-scope)
- [6. Out Of Scope](#6-out-of-scope)
- [7. Permission Matrix](#7-permission-matrix)
- [8. Backend Plan](#8-backend-plan)
- [9. Frontend Plan](#9-frontend-plan)
- [10. Validation](#10-validation)
- [11. Risks](#11-risks)
- [12. Open Questions](#12-open-questions)
- [13. Recommended First PR](#13-recommended-first-pr)

## Status

- roadmap_priority: `P4`
- status: `implemented` — roles, quota, workflow lifecycle, the authorization
  matrix, the audit trail, its retention window, its export, and quota alerting
  are shipped; the §4.4 second slice of actions (space creation, webhook keys,
  agent definitions, and workflow lifecycle) now records, and open question 7's
  run correlation is answered by the `task_run_id` an event inherits from the
  run it was recorded on behalf of. Open questions 3–5 and 9–10 remain policy
  decisions, not missing implementation
- follows: [enterprise-deployment.md](./enterprise-deployment.md)
- roadmap: [../ROADMAP.md](../ROADMAP.md)
- created_at: `2026-05-17`

## 1. Decision

P4 should turn BuildMax's existing space controls into a practical governance
foundation for private space operation.

Several foundations already exist:

- space roles: `owner`, `admin`, `member`
- space-scoped quota service
- space usage endpoint
- centralized handler authorization helper
- workflow lifecycle: `draft`, `published`, `archived`
- workflow assignment and execution checks

The remaining work is not a giant enterprise policy platform. It is making the
existing controls visible, tested, and traceable enough that space admins can
trust shared automation.

## 2. Product Goal

Admins should understand:

- who can do what
- what shared resources are governed
- what capacity the space is using
- which workflows are draft, published, or archived
- which sensitive assets changed over time

The product should provide confidence without forcing users into a heavy admin
console.

## 3. Current Baseline

Backend anchors:

- roles in `internal/core/space/space.go`
- the role/action decision in `internal/core/space/policy.go`, applied to a
  request by the `Guard` in `internal/server/access`
- quota service in `internal/service/quota/service.go`
- quota routes in `internal/server/handlers/space/usage.go`
- workflow lifecycle in `internal/core/workflow/workflow.go`
- workflow lifecycle enforcement in `internal/service/workflow/service.go`
- space persistence in `internal/infra/db/space.go`
- default quota tier seeding in `internal/infra/db/quota_tier.go`

Frontend anchors:

- space settings in `portal/src/pages/settings/SpaceSettings.tsx`
- shared settings UI in `portal/src/pages/settings/shared.tsx`
- workflow pages in `portal/src/pages/workflows`
- issues assignment UI in `portal/src/pages/issues`

Current action model:

- `owner` manages space members.
- `owner` and `admin` manage agents, workflows, and workflow assignment.
- `owner`, `admin`, and `member` can run workflows.

## 4. Main Gaps

### 4.1 Governance Is Not Visible Enough

The backend has roles and checks, but the UI needs clearer admin-facing
explanation:

- what each role means
- why an action is unavailable
- which workflow states are runnable
- current space usage and quota limits

### 4.2 Permission Boundaries Need Broader Tests — RESOLVED

**This gap is closed.** A matrix drives real requests through the mux for an
owner, an admin, a member, a member of another space, and an anonymous caller,
covering every space-scoped route the server registers.
Driving requests rather than unit-testing the authorization helper is the
point. The role rules now have one implementation, `core/space.Allows`, with its
own table test; what the matrix proves is the other half — that each route
actually asks. A rule with one owner that a route never consults is still an
open route. A second test reads every route registration and fails when a
space-scoped route has no entry — and when an entry names a route that no longer
exists, because a dead row reads as coverage.

What it was built to prove:

- members cannot mutate shared automation assets
- admins cannot manage ownership-sensitive membership actions
- owners can manage members
- workflow lifecycle restrictions apply consistently
- space-scoped resources cannot leak across spaces

### 4.3 Workflow Lifecycle Needs Product Polish

The lifecycle exists, but it should be obvious in Portal:

- `draft`: editable, not assignable/runnable for shared work
- `published`: assignable and runnable
- `archived`: retained for history, not used for new work

The UI should avoid making users learn this by failed requests.

### 4.4 Sensitive Assets Are Not Traceable — RESOLVED

**This gap is closed.** Of the shared assets that affect space execution:

- space members and roles — **recorded**
- the model catalog, which holds provider credentials — **recorded**
- space creation, with its quota tier — **recorded** (`space.created`)
- webhook keys — **recorded** (`webhook_key.created`, `webhook_key.revoked`)
- agent definitions — **recorded** (`agent.created`, `agent.updated`,
  `agent.deleted`; a revision restore is an update)
- workflows, including publish and archive — **recorded** (`workflow.created`,
  `workflow.updated`, `workflow.published`, `workflow.archived`,
  `workflow.unpublished`)

The first slice was chosen for what a compromise costs rather than for how often
the asset changes: a membership change grants access to everything a space holds,
and a catalog change moves prompts and spending. The second slice was the rest —
each one a `Record` call at the point of change plus a permanent action string.

Quota tier is carried in the detail of `space.created`, where the tier a space
starts on is decided. A later change is a System Administrator's, not a space
role's: it is recorded as `space.quota_tier_changed` with the old and new tier —
see [system-administration.md](./system-administration.md) §7.2 and open
question 4.

## 5. In Scope

### 5.1 Space Quota UI And Documentation

Make quota visible where admins expect it:

- current space usage
- tier name
- rolling period
- run limit
- token limit
- over-limit behavior

Document:

- quota is space-scoped
- personal use is represented by the default personal space
- task creation/rerun checks the active space

### 5.2 Role And Permission Boundary Tests

Add a table-driven permission matrix for server handlers and services.

Minimum actions:

- manage space members
- create/update/delete agent
- create/update/publish/archive workflow
- assign issue to workflow
- run workflow
- read space resources
- create normal work

### 5.3 Workflow Lifecycle UI

Polish workflow list/detail copy and controls:

- show lifecycle badge
- disable unavailable actions with clear text
- hide archived workflows from default assignment choices
- keep archived workflows inspectable
- make publish/archive transitions explicit

### 5.4 Small Audit/Event Model — SHIPPED

`audit.Event` in `internal/core/audit/audit.go` is the shipped shape. It
differs from what this section originally sketched in three ways that were
decisions, not drift:

- **`ActorType` plus `ActorID`, not `ActorUserID`.** A worker and the system
  itself take meaningful actions, and typing the actor was cheaper than
  inventing a user id for them.
- **No `MetadataJSON`.** A free-form JSON column is where prompts, request
  bodies, and credentials end up. `Detail` is a short non-sensitive note — a
  role name, a model alias — and nothing more.
- **A `SpaceID` that may be empty.** A login is not space-scoped, and forcing one
  would have meant inventing a space for the event.
- **A `TaskRunID` that may be empty.** It names the run an action was taken on
  behalf of, when there is one, so a governed action a run caused is reachable
  from the run. See §5.9.

The event carries no prompts, no generated content, no tool output, and no
credentials: only who did what to which object. Run diagnostics live in the
durable run trace and per-call accounting in the `llm_call` ledger, because
those are different questions with different retention answers.

The `Store` interface is append-only — there is no update or delete, since a
record that can be edited is not evidence. Action strings are persisted and
therefore permanent; renaming one rewrites history for every reader filtering
on it.

The action list is the `const` block in `internal/core/audit/audit.go`, which
is the source of truth; it began with identity and model-catalog actions and now
covers the §4.4 second slice as well. Two rules are worth stating for anyone
extending it: a failed login is deliberately *not* recorded, because it says
nothing about who the actor was and would turn the trail into a place to write
arbitrary strings; and `access.denied` is the one action written on failure,
because a denial is what shows someone probing at a boundary.

A failed write is logged and dropped rather than failing the action that
triggered it. That is the decision, not a gap: refusing the action would turn a
logging outage into an outage of the thing being logged. It is a real limit and
`manual/support.md` states it — the trail records what happened while the
database was reachable, which is not the same as guaranteeing every action was
recorded. Whether any one action should instead be recorded transactionally is
the residue of open question 2.

### 5.5 Event Visibility — SHIPPED

`GET /api/spaces/{space_id}/audit-events` serves a space's trail newest-first with
limit/offset, and Portal renders it as an audit section in space settings
(`portal/src/features/audit/`).

It is **owner-only**, in the API and in the UI. The trail names who did what
including who was refused, which is administrative rather than collaborative
information — a member does not need to see that a colleague was denied
something. This answers what was open question 1.

### 5.6 Retention — SHIPPED

`audit.retention_days` in `server.yaml` expires events older than the window.
It defaults to **0**, which keeps everything, because a deployment that never
chose a policy has not decided to discard evidence.

The sweep is the only thing in BuildMax that removes an audit event, and the
narrow `AuditPruneStore` interface exists so that it stays that way: every
reader and writer of the trail holds `AuditStore`, and none of them can reach a
delete. Nothing can remove a *particular* record — the sweep takes a cutoff and
a batch size, and that is the whole of its vocabulary.

Every sweep that removed anything writes an `audit.pruned` event naming the
range and the count. That record is the reason deleting is defensible at all: a
trail that begins partway through has to say whether policy shortened it or
somebody truncated it, and those look identical without it. The event is
younger than the cutoff that produced it, so it survives its own sweep, and by
the time the window passes it a later sweep says the same about a later stretch.

This answers open question 6: retention is configuration, defaulting to keep,
applied by the deployment rather than by a space.

### 5.7 Export — SHIPPED

`GET /api/spaces/{space_id}/audit-events/export` gives a space owner their trail,
and `GET /api/admin/audit-events/export` gives a System Administrator the
deployment's under the same filters the search takes. Both stream CSV or JSONL.

Three decisions are worth keeping:

- **It is a pull, not a delivery.** Open question 8 asked whether export needs
  at-least-once delivery and how a consumer would detect gaps. A file someone
  downloads has neither problem, and shipping the pull first means the event
  shape does not have to be final before anyone can get their data out. A push
  integration would still have to answer question 8; this does not.
- **The space route takes no filters.** The reason to export is to hold the
  record elsewhere, and a filter applied on the way out is a decision the file
  cannot show it made. The admin route does take them, because there they are
  an operator narrowing a read they already hold.
- **An export is recorded**, as `audit.exported`, with the count that actually
  left and whether it stopped at the cap. Reading the whole record is an action
  on it; an export that left no trace would be the one way to consult the trail
  without appearing in it. An admin export narrowed to one space is recorded in
  that space's trail too, so its owner can see that the deployment read it.

Paging uses a keyset cursor rather than an offset. An export reads across many
round trips while rows are appended at one end and, under retention, removed at
the other, and either shifts every offset behind it — in an evidence export a
skipped page is the worst kind of bug, because the file still looks complete.

### 5.8 Quota Alerting — SHIPPED

`QuotaService.Check` records two actions: `quota.threshold_reached` when a space
passes 80% of a limit, and `quota.exceeded` when work is refused. They are
separate because they call for different responses — one is a heads-up, the
other is work not happening.

Both are written at most once per limit per period, deduplicated against the
trail itself, so a space that keeps submitting does not turn its own record into
a log of retries. The actor is the system, not whoever submitted the work that
tipped the total over: a quota belongs to the space, and naming the last member
to submit would read as blame for a shared budget.

The admission path is where this has to live. Usage is a rolling window, so
there is no period boundary at which a sweep could notice a space sitting at
80%. Neither the read nor the write may change the admission decision — a
deployment whose audit table is unreachable still runs work, and still enforces
the limit.

Portal states the same thing in space settings, computed from the usage figures
it already has. That is the fast answer; the trail is the durable one.

### 5.9 Run Correlation — SHIPPED

An event carries an optional `task_run_id`: the run an action was taken on behalf
of, when there is one. It answers open question 7 — an investigation that starts
at an audit event can now reach the run that caused it, and the reverse, every
governed action a run caused, is a filter over the trail.

It is stored the way the actor and the target are: an opaque public handle, not a
foreign key. The trail deliberately does not join to the execution plane, whose
rows have their own retention — so a run that has since been pruned does not
break the evidence, and reaching the run's trace and `llm_call` ledger is by that
one id rather than a join. An architecture test records this as a deliberate
exception to the numeric-reference rule.

It is populated from the context, not by every call site. A run-scoped worker
request is tagged with its run once, in the worker middleware, so any event a
route records downstream — a worker uploading an artifact, the gateway meeting a
quota while serving a run — inherits the id. An event that names a run explicitly
keeps its own, because a caller that knows the run better than the ambient
context should not be overridden by it. The events a person takes directly carry
no run, which is most of them.

What this does *not* do is retrofit a run onto actions that have none: a login,
a membership change, and a model-catalog edit are not run-caused, so they stay
uncorrelated rather than being given a run they did not have.

## 6. Out Of Scope

- Custom roles.
- Policy DSL.
- Approval workflows.
- Per-agent or per-workflow permission lists.
- Immutable compliance archive.
- Pushed audit delivery to an external sink. The pull export in §5.7 shipped;
  a push would have to answer open question 8, which it does not.
- Billing.
- Organization hierarchy.

## 7. Permission Matrix

Recommended starting matrix:

| Action | Owner | Admin | Member |
|---|---:|---:|---:|
| View space resources | yes | yes | yes |
| Create issue/conversation work | yes | yes | yes |
| Run assigned workflow | yes | yes | yes |
| Manage agents | yes | yes | no |
| Manage workflows | yes | yes | no |
| Assign issue to workflow | yes | yes | no |
| Manage space members | yes | no | no |
| Change member roles | yes | no | no |
| View space usage | yes | yes | yes |
| Change quota tier | no | no | no |
| View activity events | yes | yes | no |

This matrix intentionally stays simple. If later enterprise customers need more
control, build from observed needs rather than inventing custom RBAC now.
Changing a quota tier is no space role's: a tier is deployment capacity, so a
System Administrator assigns it ([system-administration.md](./system-administration.md)
§7.2).

## 8. Backend Plan

### M1. Permission Tests — DONE

Shipped as a route matrix rather than tests around the role predicate, for the
reason given in §4.2: the predicate cannot show that a route consulted it.
Every space-scoped route the server registers has a row naming who may call it, the
rows are driven as real requests for five callers including a member of another
space, and a route without a row fails the build.

Acceptance met: the permission matrix is enforced by tests, and a new
space-scoped route cannot ship without someone deciding who may call it.

### M2. Governance Service Boundary

The current handler helper is acceptable for the first cut. If action checks
keep spreading, move the policy into a small service/package.

Target API:

```go
type SpaceAuthorizer interface {
	Authorize(ctx context.Context, spaceID, userID string, action SpaceAction) (role string, err error)
}
```

Keep it boring: no policy DSL.

### M3. Space Event Store — DONE

Shipped as `audit.Event` and `audit.Store`
(`RecordAuditEvent`/`ListAuditEvents`) in `internal/core/audit`, with
`auditEventRow` in
`internal/infra/db` on the singular table `audit_event`. The
naming landed on *audit* rather than *space event* because a login is not
space-scoped and the trail is evidence rather than an activity feed. See §5.4 for
the shape and for what was dropped from the sketch here.

### M4. Event Writes — DONE

Events are written after the mutation succeeds. A failed write is logged and
dropped, so a governance record never fails the action it describes — the
trade-off is stated in §5.4 and in `manual/support.md` rather than left for
an operator to discover during an investigation. The compact-JSON metadata rule
did not survive: there is no metadata column, only a short `Detail` string.

### M5. Event API — DONE

Shipped as:

```text
GET /api/spaces/{space_id}/audit-events
```

Authorization is **owner-only**, narrower than the owner/admin sketched here,
for the reason in §5.5. Response is `{"events": [], "total": 0}` with
limit/offset paging.

## 9. Frontend Plan

### M1. Role Copy And Disabled States

Update Portal copy:

- space member role descriptions
- disabled action text for member/admin limits
- workflow lifecycle explanations

### M2. Space Usage Panel

In space settings, show:

- tier
- period
- runs used
- tokens used
- limits
- no-limit fallback when tier is unknown

### M3. Workflow Lifecycle UI

In workflow pages:

- badge state
- publish/archive actions for owner/admin
- unavailable run action for draft/archived
- assignment UI that defaults to published workflows only

### M4. Activity Section — DONE

Shipped in space settings as "Audit trail" (`portal/src/features/audit/`), with
concise labels and paging. A non-owner sees the section explain why it is empty
for them rather than seeing nothing, so the boundary is legible instead of
looking like a missing feature.

## 10. Validation

Backend:

```sh
go test ./internal/server/handlers ./internal/service/quota ./internal/service/workflow ./internal/infra/db
```

Frontend:

```sh
cd portal && npm run build
```

Full:

```sh
./make test
```

Manual scenarios:

1. Owner can add/remove members.
2. Admin can manage workflows but cannot manage members.
3. Member cannot manage workflows or agents.
4. Published workflow can be assigned and run.
5. Draft/archived workflow cannot be assigned for new work.
6. Space usage is visible and matches active space.
7. Sensitive actions appear in Space Activity.

## 11. Risks

- **Too much governance too early**: avoid custom roles and approvals until
  basic traceability lands.
- **Audit noise**: only record meaningful sensitive actions in the first slice.
- **Action/event mismatch**: keep event action names stable and documented.
- **UI clutter**: make governance visible in settings and workflow pages, not
  everywhere.
- **Silent event failures**: log event write errors with enough context.

## 12. Open Questions

1. ~~Should members be able to view Space Activity, or is it admin-only?~~
   **Decided: owner-only**, narrower than either option. The trail records who
   was refused a request, and that is administrative information — see §5.5.
2. ~~Should event writes be best-effort or required for sensitive actions?~~
   **Decided: best-effort**, and `internal/service/audit` carries the reason:
   refusing a login because an audit insert failed turns a logging outage into
   an authentication outage. A failed write is logged at error with the action
   and the actor, so a dropped event still leaves a mark somewhere. What
   remains open is narrower — whether any *single* action deserves a
   must-succeed record written in the same transaction as the action it
   describes. [system-administration.md](./system-administration.md) §9 argues
   the grant actions are where the best-effort case is weakest, because a grant
   that was made and not recorded is the one an investigation most needs.
   Answering that means deciding what the caller sees when the action succeeded
   and the record did not. See §5.4.
3. Should workflow publish/archive require owner or allow admin?
4. ~~Should quota tier changes be implemented in P4 or only documented?~~
   **Implemented** as System Administrator tier assignment, not a space-owner
   action; see [system-administration.md](./system-administration.md) §7.2.
5. Should webhook key creation/revocation require owner/admin only?

The remaining questions came from the retired *Audit and data governance*
proposal. Its recommended direction — an internal space ledger first, export
later — is what shipped; these are the parts that were not settled by shipping
it:

6. ~~What retention applies to audit events, and is it configuration or an
   operational responsibility?~~ **Decided: configuration, defaulting to keep
   everything** — `audit.retention_days`, applied by the deployment rather than
   by a space, with each sweep recording what it removed. See §5.6.
7. ~~What correlation identifiers may connect a task, worker, model call, and
   artifact?~~ **Answered.** A run's trace and the `llm_call` rows it produced
   were already joined in Portal's run details; the audit trail now joins to them
   too, through the optional `task_run_id` an event carries. An investigation
   that starts at an audit event can reach the run that caused it, and every
   governed action a run caused is a filter over the trail. The id is populated
   from a run-scoped context rather than by every call site, and is not
   retrofitted onto actions that have no run. See §5.9.
8. ~~Does export need at-least-once delivery, and how would a consumer detect
   gaps?~~ **Sidestepped, not answered.** §5.7 ships a pull export, which has
   neither problem. The question stands for any future push integration, and a
   best-effort write (question 2) still means a gap is not always
   distinguishable from nothing having happened.
9. Who may read run traces, artifacts, and model usage? The audit trail's
   answer is settled; these three were never decided together, and they carry
   more than the trail does — a trace holds tool output.
10. Which deletion controls does a space get? There is no export or import
    command and nothing deletes a space's records, so "delete our data" has no
    answer beyond dropping the database and the bucket.

## 13. Recommended First PR

The first P4 PR should make existing governance explicit:

1. Add the permission matrix tests.
2. Polish space settings role/quota UI copy.
3. Polish workflow lifecycle UI states.
4. Add missing handler tests for forbidden role paths.

Then the second PR can add the small `space_event` model and Space Activity UI.
