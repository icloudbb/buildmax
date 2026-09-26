# System Administration

> **简体中文：** [阅读中文镜像](../zh-CN/design/系统管理.md)

## Contents

- [Status](#status)
- [1. Decision](#1-decision)
- [2. Product Goal](#2-product-goal)
- [3. Current Baseline](#3-current-baseline)
- [4. Principals And What Each May Reach](#4-principals-and-what-each-may-reach)
- [5. Authority Model](#5-authority-model)
- [6. Bootstrap And Lockout Recovery](#6-bootstrap-and-lockout-recovery)
- [7. Server Administration API](#7-server-administration-api)
- [8. Account Disablement Semantics](#8-account-disablement-semantics)
- [9. Audit Events](#9-audit-events)
- [10. Portal Administration Surface](#10-portal-administration-surface)
- [11. The Authorization Matrix](#11-the-authorization-matrix)
- [12. Out Of Scope](#12-out-of-scope)
- [13. Backend Plan](#13-backend-plan)
- [14. Frontend Plan](#14-frontend-plan)
- [15. Validation](#15-validation)
- [16. Risks](#16-risks)
- [17. Open Questions](#17-open-questions)

## Status

- roadmap_priority: `P4`
- status: `implemented` — M1–M6 are shipped: the grant model, the operator
  command, admin route authorization, the account routes, account disablement,
  the system status and redacted configuration routes, the cross-space audit
  search and space metadata routes, the Portal administration area, and the
  model catalog surface. What remains is in §17, and none of it is a gap in
  the first slice. Later grant integrity, Portal discoverability, pagination,
  model creation, authenticated `buildmax admin` operations, the operator
  surface split (§6.1), and the account deactivation lifecycle with execution
  eligibility and Space owner recovery (§8) have also shipped
- follows: [space-governance.md](./space-governance.md) and
  [enterprise-deployment.md](./enterprise-deployment.md)
- relates to: [enterprise identity and access](enterprise-identity-and-access.md),
  the accepted record that owns OIDC/SCIM and must not be duplicated here
- roadmap: [../ROADMAP.md](../ROADMAP.md)
- created_at: `2026-08-18`

## 1. Decision

BuildMax gets one deployment-scoped principal, the **System Administrator**,
persisted as a revocable grant on a user and never derived from membership in a
Space.

The three alternatives were weighed and rejected:

- **Leave operator work in the CLI, the database, and the cluster.** This is
  where it is today. It makes routine account management require the database
  password, and it produces no accountable record of who did what.
- **Treat a Space owner as a global administrator.** This collapses the one
  boundary the product actually has. Space is the ownership boundary for
  issues, conversations, artifacts, and traces; an owner who can read across
  spaces makes that boundary advisory.
- **Configure one static administrator email.** It bootstraps in one line and
  then fails at everything after the first day: no second admin, no
  revocation without a redeploy, and an audit trail that can only name a
  string from a config file.

A persisted grant is the only option that supports more than one admin,
revocation without a redeploy, and an audit record naming a real account.

The grant is an **authority to operate the deployment, not a key to its
contents**. A System Administrator can create and disable accounts, read
system status, search the audit trail across spaces, and see space and quota
metadata. They cannot read a space's prompts, tool output, artifacts, files, or
run traces without being a member of that space. That is enforced by the same
space checks every other caller passes, and proved by a test, not by a promise
in this document.

## 2. Product Goal

An operator of a private deployment should be able to answer these without a
database client, a `kubectl exec`, or the JWT secret:

- Who has an account, when did they last sign in, and can they still sign in?
- Someone is leaving today — how do I stop their access now?
- Someone forgot their password — how do I get them back in?
- Is this deployment healthy, on which version, with which schema applied?
- Who changed access, models, or configuration, in which space, and when?
- Which spaces exist, how large are they, and what are they using?

And they should be unable to answer, through this surface, "what is that space
working on".

## 3. Current Baseline

What exists today, with the anchors this design builds on:

- Space roles and the space authorization helper:
  `internal/core/space/space.go`, `internal/core/space/policy.go`.
- The single funnel for user identity on every JWT route: `Guard.ActiveUser` in
  `internal/server/access`. Every authenticated handler reaches a user id
  through it.
- Route ownership: each handler subpackage's `Register` method, with a coverage
  test in `space_authz_matrix_test.go` that reads every one of them and fails
  when a space-scoped route has no authorization row.
- The append-only audit trail: `internal/core/audit/audit.go`,
  `internal/service/audit`, `internal/infra/db/audit.go`, and the space-scoped,
  owner-only `GET /api/spaces/{space_id}/audit-events`.
- Operator commands that already run with database credentials:
  `internal/bootstrap/user_admin.go` (`buildmax-server user create |
  login-code`) and `internal/bootstrap/model_admin.go`
  (`buildmax-server model add | list | enable | disable`).
- Sessions: `model.RefreshTokenStore` can revoke one session
  (`RevokeSession`), and the access token is a signed JWT the server never
  stores.
- Liveness, readiness, and version: `internal/server/health.go`,
  `readinessChecks` in `internal/bootstrap/server.go`, and
  `config.VersionString()`.
- Schema state: the `schema_migration` table in `internal/infra/db/migration.go`.

Five gaps follow from that list, and they are what the first slice closes:

1. There is no deployment-scoped principal at all. `system_admin` does not
   exist in any form, including a flag on `user`.
2. `buildmax-server user ...` writes **no audit events**. Account creation,
   password setting, and code issuance — the most sensitive actions an
   operator takes — leave no record. `model_admin.go` does record its changes,
   so this is an inconsistency rather than a policy.
3. Nothing disables an account. A user row exists or it does not.
4. Nothing lists or revokes a user's sessions. `docs/deploy/authentication.md`
   already says so: signing someone out today means deleting
   `user_refresh_token` rows by hand.
5. A space's quota tier is set once, from `default_quota_tier`, at space
   creation. `internal/infra/db/quota_tier.go` has `GetQuotaTier` and
   `SeedDefaultQuotaTiers` and no way to assign a different tier afterwards.

## 4. Principals And What Each May Reach

The threat model is a table of principals, because the failure this design has
to prevent is one principal quietly acquiring another's reach.

| Principal | Authenticated by | May reach | May not reach |
|---|---|---|---|
| **User** | Access token from a password or login code | Resources of spaces they belong to, at their role | Anything in a space they are not in |
| **Space owner** | The same token, plus an `owner` membership row | Membership, shared automation, and the audit trail of *that* space | Any other space; any deployment-scoped surface |
| **System Administrator** | The same token, plus an active grant row | Accounts, grants, system status, redacted configuration, cross-space **metadata** and audit, model catalog state | Prompts, messages, tool output, artifacts, files, and run traces of spaces they are not in |
| **Worker run** | A run token naming one run | The four `/api/worker/*` routes for that run | Every user route; every other run |
| **Infrastructure operator** | Database, cluster, and secret access | Everything, by construction | — |

Two consequences are worth stating rather than implying.

**A System Administrator is not the top of a ladder.** They are a principal
with a different axis of authority. A space owner has depth in one space; an
admin has breadth over the deployment's operation and no depth anywhere. The
authority is not additive with membership: an admin who is also a member of a
space gets exactly the member's reach in it.

**The infrastructure operator is still more powerful than the System
Administrator**, and this design does not change that. It reduces how often
anyone needs to *be* the infrastructure operator, which is the actual goal: an
operator who runs routine work through an audited surface stops handing out the
database password for it.

Two attacks shape the specifics below:

- **A compromised admin session.** The blast radius is bounded by what the API
  can return: metadata and status, never content. Every action it takes is in
  the audit trail with the actor's user id. The session can be ended by any
  other admin, or by the operator command, revoking that user's sessions.
- **A cross-space read attempt.** An admin calling a space content route without
  membership is refused by the existing space check, because the admin routes
  are a separate tree and the grant is never consulted by `authorizeSpaceAction`.
  §11 makes that a test rather than an assertion.

## 5. Authority Model

### 5.1 One Role, Persisted As A Grant

`internal/core/identity/system_grant.go` owns `SystemGrant` and
`SystemGrantStore`. The public ID is an opaque string, `GrantedAt` is
`time.Time`, and `RevokedAt` is an optional `time.Time`. Numeric relational keys
stay inside the store; see [entity identity](entity-identity.md) and
[timestamp representation](timestamp-representation.md).

`systemGrantRow` persists on `system_grant`. Its unique index is
`(user_id, role, live_marker)`: the marker is fixed and non-NULL while live, and
cleared with `revoked_at` on revocation. A unique key containing nullable
`revoked_at` would allow duplicate live grants in MySQL and is not the current
mechanism.

`RevokeSystemRole` checks `keepLastHolder` and revokes atomically. The API
preserves an effective holder, defined as an unrevoked grant on an enabled
account; account disablement uses the same protection. The database-direct
operator path can deliberately revoke the final grant for recovery. MySQL
concurrency evidence is in `internal/infra/db/system_grant_test.go`.

Three shape decisions, each of which was the other way at some point:

- **A `role` column, not a boolean.** Question 1 of the proposal asked whether
  a read-only `system_observer` and a support role are needed now. They are
  not — nobody has asked for them, and inventing an unused role is the mistake
  §16 warns about. But the column costs one `varchar` today and is the
  difference between adding a role later and migrating a boolean later. Only
  `system_admin` is accepted by the store until a second role has a caller.
- **Not a flag on `user`.** A flag has no granting actor, no timestamp, and no
  history. Those three are the entire point.
- **No `superuser`.** A permission whose scope is not written down grows
  silently. Every admin route names the role it needs, and §11 fails the build
  if one does not.

### 5.2 How It Is Checked

A separate helper alongside the space one, never inside it:

```go
// internal/server/handlers/system_authz.go
func (h *Handler) requireSystemAdmin(w http.ResponseWriter, r *http.Request) (userID string, ok bool)
```

It calls `requireAuth`, then `ActiveSystemRoles`, and on refusal records
`access.denied` with an empty `space_id` before writing the response.

`authorizeSpaceAction` is not modified, and must not be. A grant is not an
argument to a space check; if it ever becomes one, the boundary in §4 stops
being true and no test would notice.

### 5.3 Status Codes

- No credential, or an invalid one: **401**, same as every other route.
- A valid credential without a grant: **403**.

Not 404. Hiding the existence of `/api/admin/*` is not achievable — the routes
are in open-source route registrations and in the Portal bundle — and pretending
otherwise costs the clarity of a correct status code. What must not leak is
data, and the 403 carries none. This is the same shape as the existing
owner-only audit route, which Portal already reads a 403 from as "not for
you" rather than as a bug.

## 6. Bootstrap And Lockout Recovery

The first grant comes from an operator command, not from configuration:

```text
buildmax-server admin grant <email>     # grant system_admin
buildmax-server admin revoke <email>    # revoke it
```

It joins `user` and `model` under the same rule those two already follow: it
reads the same `server.yaml` the server reads, so it works unchanged in a
container or a pod, and it requires the database credentials — which is the
correct price for minting deployment authority.

This answers proposal question 2, and it answers it by **not** adding a
configuration value. A `system_admins:` list in `server.yaml` would be a second
source of authority that the audit trail cannot describe, that revocation
cannot reach without a redeploy, and that would have to win or lose against the
table in every edge case. One source, one story.

Recovery follows from the same choice: the command works whether zero or ten
admins exist, needs no running server, and needs no Portal account. There is no
break-glass credential to store, rotate, or leak, because the recovery path is
the ordinary path.

One invariant guards the gap between the two:

- The **API** refuses to revoke or disable the last effective `system_admin`
  holder, with a message naming the command. An admin cannot leave the
  deployment with none by clicking.
- The **command** allows it, and prints what it means. The operator whose
  admin left the company needs exactly that, and they already hold the
  credentials that make the restriction meaningless.

Self-revocation through the API is allowed when another admin remains. Refusing
it would only mean asking a colleague to do the same thing.

### 6.1 Operator Surfaces

Operator surfaces are split by the authority a caller can present, not by
feature:

| Surface | Use | Authentication | Availability |
|---|---|---|---|
| `buildmax-server` | Break glass and bootstrap only, plus running the Server | Direct access to Server configuration, the database, and the signing key | Works without Portal and without a healthy public Server |
| Admin API (`/api/admin/*`) | Stable programmatic contract for routine administration | User session plus a live system grant | Requires the Server |
| `buildmax admin` | Scriptable routine administration | The Admin API, reusing the `buildmax` client login | Requires the Server |
| Portal | Guided routine administration | The Admin API | Requires the Server and Portal |

`buildmax admin` and Portal are peer clients of one Admin API, and neither
touches the database. `buildmax-server` keeps only what must sit next to the
database or the signing key:

- `admin grant | revoke` — the first grant, recovery from zero administrators,
  and final-holder force revocation (§6);
- `run-token` — a diagnostic credential for one run's worker routes;
- `space recover-owner` — the disabled-owner-only Space recovery of §8.4 when
  the public Server or the IdP is unavailable;
- `user create | login-code` and `model add | set-key | list | enable |
  disable` — seeding a fresh deployment before any client can authenticate.
  They are bootstrap primitives, not a routine surface; routine account and
  model work goes through the Admin API.

An authenticated surface does not offer an operation only for these reasons,
stated where it would otherwise appear:

- the first grant and recovery from zero administrators cannot depend on an
  already authenticated administrator;
- final-holder force revocation is deliberately a database-authorized action;
- a run token is a bearer credential for worker-route diagnosis, not a routine
  management outcome;
- process-start configuration cannot truthfully be edited through one Server
  replica (§7.2).

The rule is one owning service per mutation. HTTP handlers are adapters over
it, and `buildmax-server` calls the same service directly as a system actor, so
bootstrap and recovery work while the HTTP API is down. Grants belong to
`internal/service/systemadmin`, account deactivation to
`internal/service/accountlifecycle`, owner recovery to
`internal/service/spacerecovery`, model creation and credentials to
`internal/service/llmcatalog`, and the plugin catalog to
`internal/service/plugin`. Convergence is not complete: account creation, login
codes, and session revocation are still single store calls made by the admin
handlers and by `buildmax-server user`, each recording its own audit event, and
`buildmax-server model list | enable | disable` bypasses the catalog service
that the Admin API uses.

## 7. Server Administration API

All routes are `/api/admin/*`, all require `system_admin`, and none takes a
`space_id` path parameter — an admin route that looked space-scoped would invite
exactly the confusion §4 exists to prevent.

### 7.1 Routes

The table is the registered surface; `internal/server/handlers/admin` owns it.

| Route | Returns | Refuses to return |
|---|---|---|
| `GET /api/admin/me` | The caller's grant: role, granted at, granted by | — |
| `GET /api/admin/grants` | Active grants, and revoked ones on request | — |
| `POST /api/admin/grants` | Grants `system_admin` to a user id | A grant to an account that does not exist |
| `DELETE /api/admin/grants/{user_id}` | Revokes it | The last effective holder (§6) |
| `GET /api/admin/users` | Accounts, newest first, `?q=` on email, paged | Password hashes, login codes, token values |
| `GET /api/admin/users/{user_id}` | One account: email, name, quota tier, last login and platform, `has_password`, `disabled_at`, space memberships with roles, active session count | Everything in the row above |
| `POST /api/admin/users` | Creates an account and its personal space | — |
| `POST /api/admin/users/{user_id}/login-code` | Issues a single-use code, shown once | A code that can be read back later |
| `GET /api/admin/users/{user_id}/deactivation-impact` | What a disable would stop (§8.3): live sessions, webhook keys, shared-Space memberships and roles, sole-owned shared Space IDs, enabled Schedules, active runs by status, the cancellation bound | Prompts, inputs, outputs, Artifact names, traces, raw errors, secrets |
| `PUT /api/admin/users/{user_id}/state` | Sets the account's `disabled` flag (§8). A disable accepts `retire_webhook_keys` and returns the reloaded account with its cleanup counts | — |
| `GET /api/admin/users/{user_id}/identities` | The account's external-identity links: issuer, subject, last-seen email and name, last login | — |
| `DELETE /api/admin/users/{user_id}/identities/{identity_id}` | Unlinks one, audited in the same transaction | An unlink while the account is enabled |
| `GET /api/admin/users/{user_id}/sessions` | Live login chains: session ID, platform, creation, rotation, expiry | Token values |
| `DELETE /api/admin/users/{user_id}/sessions/{session_id}` | Revokes one login chain's refresh tokens | A session belonging to another account |
| `DELETE /api/admin/users/{user_id}/sessions` | Revokes every refresh session, returns the count | — |
| `GET /api/admin/system` | Version, commit, schema migrations applied, readiness checks and their status, worker runner mode, signup and sandbox settings, run counts by status | Anything with a credential in it |
| `GET /api/admin/config` | The effective configuration, redacted, plus computed warnings | Every secret — **presence only**. Not a length, not a prefix, not a hash: each of those narrows a search for someone who has the response and wants the secret |
| `GET /api/admin/spaces` | Team spaces with member count, quota tier, created at; personal spaces excluded | Space contents of any kind, and every account's personal space |
| `GET /api/admin/spaces/{space_id}` | The same, plus members and roles, plus usage against the tier | Issues, conversations, artifacts, files, traces |
| `GET /api/admin/audit-events` | The trail across every space, filtered by `space_id`, `actor_id`, `action`, `since`, `until`, paged | Anything the event does not already hold |
| `GET /api/admin/audit-events/export` | The same filtered trail as a CSV or JSONL download; the export is itself audited | The same |
| `PUT /api/admin/spaces/{space_id}/owner` | Promotes an enabled member to owner when every recorded owner is disabled (§8.4) | A healthy Space, a personal Space, a successor who is not already a member |
| `POST /api/admin/llm/models` | Creates a model, encrypting a write-only credential | Credential material in the response |
| `GET /api/admin/llm/models` | The catalog: name, provider, model, capabilities, enabled | `api_key`, in any form |
| `PUT /api/admin/llm/models/{model_id}/state` | Sets the model's `enabled` flag, retiring or restoring it | — |
| `PUT /api/admin/llm/models/{model_id}/credential` | Replaces the upstream key in place, write-only | The key, in any form |
| `GET /api/admin/llm/calls` | The managed call ledger across every space, filtered by `user_id`, `model`, `status`, `surface`, `since`, `until`, paged | Prompts, tool arguments, generated content — the ledger never held them |
| `GET /api/admin/plugins`, `POST /api/admin/plugins` | Lists or creates plugin catalog entries | — |
| `GET /api/admin/plugins/{plugin_name}/releases`, `POST /api/admin/plugins/{plugin_name}/releases` | Lists releases, or publishes one from a streamed archive | — |
| `PUT /api/admin/plugins/{plugin_name}/releases/{version}/state` | Yanks a release; the only supported transition | Any other transition |
| `PUT /api/admin/plugins/{plugin_name}/state` | Archives or restores a catalog entry | — |

`POST /api/admin/users` returns the created account and **no credential**. An
operator who wants the person to sign in issues a login code as a second,
separately audited call. Creating an account and minting a way into it are two
decisions, and the CLI already separates them for the same reason.

### 7.2 What Is Not In It, And Why

- **Model creation and provider credentials — later implemented.**
  `POST /api/admin/llm/models`, Portal, and `buildmax admin model add` now accept
  a write-only provider credential encrypted under the deployment KEK.
  A credentialed model is refused when that encryption boundary is unavailable.
  `buildmax-server model add` remains a database-direct bootstrap primitive.

- **Configuration writes.** `GET /api/admin/config` is read-only, and the
  reason is mechanical rather than cautious: `server.yaml` is read at process
  start, and a multi-replica deployment has one file per replica. A write
  through Portal would change one replica's view of the world, or none.
  Configuration stays source-controlled until there is a config store, which
  is separate work. This answers proposal question 6.
- **Logs.** No log endpoint in this slice. This answers proposal question 4,
  and the answer is: raw logs stay in the deployment's own observability
  system. Building a redacted log viewer means owning a redactor whose failure
  mode is leaking a prompt or a key into a surface designed to be safe, and
  `GET /api/admin/system` plus the audit trail plus the existing run views
  cover the questions §2 lists without it. If evidence from a real incident
  shows a gap, that evidence names the specific field to add — which is a
  better input than a log viewer built on a guess.
- **Cross-space content.** Nothing here reads a prompt, an artifact, a file, or
  a trace. This answers proposal question 5 with "no, not in this slice". A
  support path that reaches space content needs its own design covering
  request, space consent, expiry, redaction, and a distinct audit action; it is
  not a parameter on a route in this table.
- **Quota tier assignment.** `GET /api/admin/spaces/{space_id}` shows the tier
  and the usage against it. Changing it needs a store method that does not
  exist (§3, gap 5), and it is the one item here that is a feature rather than
  an operator's window into existing state. It is unbuilt and waits for a
  deployment that needs it. When built, it assigns an existing tier only —
  tier definitions stay seeded until evidence says otherwise — through
  `internal/service/quota` rather than raw stores in the handler, refuses an
  unknown tier, takes effect at the next quota check without terminating running
  work, and records `space.quota_tier_changed` naming the actor, the Space, and
  the old and new tier. The duplicate user-level quota tier goes with it if
  nothing else still reads it.

## 8. Account Disablement Semantics

A nullable `disabled_at` timestamp on `userRow`, nil for an ordinary account, is
the deployment-wide gate. The absence of a `space_member` row is the per-Space
gate. Every path that admits or dispatches work checks both (§8.2). Offboarding
a person is a coordinated use of these two existing states, not an employee,
offboarding-job, or policy entity.

The two gates differ in kind, and keeping them apart is what makes recovery
predictable:

- **Account disablement is a reversible gate over retained state.**
  Memberships, Space data, and unretired webhook keys are kept and refused while
  `disabled_at` holds. Enabling reopens them with no restoration step. `email`
  stays a unique key, so a disabled account keeps its address: the same person
  returns by re-enabling that account, and an address is never rebound to a
  different person. Freeing one would take account deletion, which does not
  exist (§16).
- **Membership removal is a destructive change to one relationship.** The
  `space_member` row is hard-deleted, and a later re-invitation is a fresh join
  with a new `created_at`. See
  [Space membership lifecycle](space-membership-lifecycle.md) §5.5.

Three invariants hold across both:

- **Data outlives access.** Memberships, Tasks, results, Artifacts, traces,
  usage, and audit records are neither deleted nor reassigned. Remaining members
  keep access through the Space, which is the retention boundary, not the
  creator.
- **Restoration is explicit.** Enabling an account or re-inviting a member does
  not resurrect revoked sessions, retired webhook keys, canceled runs, or paused
  Schedules.
- **History keeps the original actor.** Creator IDs are never rewritten to the
  operator or a successor. Recovery actions record their own actor.

### 8.1 Credentials And Work

| What | Effect of disabling | Why |
|---|---|---|
| **Password login** | Refused after the password verifies, with `account_disabled` and 403 | Checking before verification would tell an unauthenticated caller which addresses are registered. Checking after tells the truth to the one person who could already prove the account is theirs |
| **Login code** | Same, and any outstanding code is spent | A code issued before the disable must not be a way back in |
| **Refresh** | Refused; all sessions revoked at disable time | This is the credential that can actually be revoked, so it is revoked immediately |
| **Access token** | Refused on the next request | See below |
| **Webhook keys** | Refused at `POST /api/webhook` while disabled. A suspension keeps them, so a deliberate re-enable restores the integration unchanged; a leaver disable with `retire_webhook_keys` revokes them permanently. The operator opts in: the API field and Portal's checkbox both default to keeping them | An integration that must outlive a person needs a separately designed machine principal, not a forgotten personal credential |
| **Enabled Schedules the account created** | Paused with `pause_reason = creator_disabled` | Otherwise a Schedule would keep admitting work under withdrawn authority at every due time |
| **PENDING runs the account initiated** | The disable records a cancel request; the dispatch gate (§8.2) moves the claimed run to `CANCELED` with `cancel_reason = creator_disabled` before a run token or worker exists | Nothing has started, and withdrawn authority is a cancellation, not a failure |
| **SCHEDULED and RUNNING runs** | Cancel requested with `cancel_reason = creator_disabled`. The worker stops on its next cancel poll and reports `CANCELED`; the stale-run backstop settles a run whose worker never answers | Work under way is stopped, not rewritten. Partial output and Artifacts remain evidence; a tool call or external write already in progress may complete, and BuildMax does not roll back external side effects |
| **Completed runs** | Unchanged | They are the Space's history |
| **Run tokens already minted** | Not revocable | A signature, not a row. Bounded by scope and by `worker.run_token_ttl`, as [deploy/authentication.md](../deploy/authentication.md) documents. The worker-fetch gate (§8.2) keeps a token minted just before the disable from starting an Agent |

The access token is the interesting one. It is a signed JWT the server does not
store, so "revoke it" means "stop honouring it", and the only place that can
happen is where the identity is resolved: `requireAuth`. Adding the check there
costs one primary-key read per authenticated request.

That cost is affordable and it is worth being precise about why: every
space-scoped route in the product already calls `ListSpaceMembers` on every
request, which is a wider read than this one. A disabled check that lands in
`requireAuth` is strictly cheaper than work the same request already does. If a
profile ever says otherwise, the fix is a short-TTL cache in front of it — with
the honest note that a cache is a window in which a disabled account still
works. No cache in the first implementation.

The alternative — wait out the access token TTL, which defaults to seven days —
was rejected. "Disable this account" that means "in about a week" is not the
feature.

Enabling reverses the gate and nothing else. Sessions stay revoked, canceled
runs stay canceled, paused Schedules stay paused, retired keys stay retired, and
the person signs in again. Undo is not a goal.

### 8.2 Execution Eligibility

`internal/core/eligibility` owns one question: may this user run work in this
Space now? `Checker.Check(ctx, userID, spaceID)` returns nil,
`ErrAccountDisabled` (disabled or missing account), `ErrNotSpaceMember`, or
`ErrUnavailable` when an authority store failed. Ordinary membership is enough;
it decides no role-specific management right.

The principal is always the run's initiator, `task_run.created_by`, which is
also the run token's user claim. `task.created_by` is provenance for the
continuing Task, not authority for its later turns, so a Continue by an eligible
colleague runs even after the Task's creator is disabled. See
[Agent execution and Task threads](agent-execution-and-task-threads.md) §10.

Unattended work is checked at four points:

| Checkpoint | Where | Ineligible | Authority store unavailable |
|---|---|---|---|
| Schedule fire | `ScheduleDispatcher`, before claiming the due fire | Pauses the Schedule with `creator_disabled` or `creator_not_member`; admits nothing | Does not fire or claim; the due time stays due, no failed fire is counted, and the next tick retries |
| Dispatch | `Scheduler`, before claiming a PENDING run | `CANCELED` with the matching `cancel_reason` | Does not dispatch; the run stays `PENDING` and the next poll retries |
| Worker fetch | The worker's `GET` of its run | Records a cancel request with the reason, so the worker starts no Agent and reports `CANCELED` | Answers `503` and hands out nothing; a starting worker retries with backoff for up to two minutes |
| Reconciler | `EligibilityReconciler`: every minute, active runs with no cancel request, in batches of 200 | Requests cancel with the reason | Leaves the run alone; the next sweep re-checks |

An unavailable authority store fails closed without destroying work: no gate
starts work whose initiator it cannot confirm, and none cancels or pauses on a
guess. A lasting outage therefore stalls unattended work instead of running it.
The dispatcher takes the oldest PENDING run first, so an undeterminable run
holds the queue until the store answers. A starting worker that exhausts its
retry budget exits non-zero, which fails the run under the local runner and
spends the Job's backoff under Kubernetes. Every refusal is logged with the
run or Schedule and the initiator.

Human requests keep their own per-request checks: `access.Guard` for the account
and session, the Space authorization helper for membership. IM channel messages
call the same Checker and refuse on every error, including an unavailable store.

The reconciler needs no offboarding record, because eligibility itself is the
durable unfinished-work predicate: while an ineligible initiator owns active
work, a sweep finds it. Every action is idempotent and goes through the existing
guarded run transitions, so concurrent disable, removal, dispatch, completion,
and cancellation converge and a terminal run is never changed.

Deviations from the stated contract, recorded rather than hidden:

- **Task admission is not re-checked in the service layer.** Create, Continue,
  and Retry rely on the HTTP guards; the Task service does not call the Checker.
- **Workflow reconciliation does not check eligibility.** A later step's
  TaskRun is admitted as the WorkflowRun's creator and stopped at dispatch or
  worker fetch; its `CANCELED` step settles the WorkflowRun as `canceled`.
- **Membership removal triggers no immediate cleanup.** The removed member's
  active runs in that Space are reached by the reconciler within a sweep, and
  their Schedules there pause at their next due time. Until then an infrequent
  Schedule stays visibly enabled.

### 8.3 Deactivation Orchestration And Impact

`internal/service/accountlifecycle` sequences a disable; the handler does not.
It commits the account gate first, then revokes sessions, retires webhook keys
when asked, pauses the account's enabled Schedules, and requests cancellation
for its active runs. The gate is the authority, so a cleanup failure is
inconvenient rather than authorizing: every admission and dispatch path already
refuses, and the reconciler converges on the runs. A failing step does not stop
the steps after it. `PUT /api/admin/users/{user_id}/state` returns the reloaded
account and the cleanup counts (`sessions_revoked`, `webhook_keys_retired`,
`schedules_paused`, `runs_canceled`) as separate facts. Once the gate commits
the call answers 200 and records `user.disabled` even if cleanup failed: the
response then names the failed steps in `cleanup_failed` (`sessions`,
`webhook_keys`, `schedules`, `runs`) and the event's detail reads
`cleanup incomplete: <steps>`. Disabling again is safe — every step acts only on
what is still live — and is the retry: Portal offers it as "Retry cleanup", and
`buildmax admin user disable` prints the retry command and exits nonzero. An
error status means the gate did not commit, except a failed read-back of the
account, whose message says the change took effect.

`GET /api/admin/users/{user_id}/deactivation-impact` is the read-only projection
an operator sees first: live sessions, webhook keys, shared-Space memberships
and roles, the shared Spaces the account solely owns, enabled Schedules, active
runs by status, and the cancellation bound. It returns counts and IDs only — no
prompt, input, output, Artifact name, trace, raw error, or secret. The bound is
a fixed value, the worker's five-second cancel poll plus the stale-run
backstop's two-minute cancel grace, not a reading of configuration. Portal shows
this projection in `DeactivationImpactModal` before the disable and offers the
webhook-key retirement choice there.

IdP-only offboarding stays bounded rather than immediate: without SCIM or a
validated logout channel, BuildMax learns nothing when the provider disables a
person. The supported immediate procedure is BuildMax disablement; the OIDC
absolute-session lifetime is the IdP-only bound, recorded by the
[enterprise identity](enterprise-identity-and-access.md) Phase 3 qualification.

### 8.4 Space Owner Recovery

The planned path is for a departing sole owner to transfer ownership first; the
impact projection names the Spaces that need it. An emergency disable is never
blocked by ownership and may leave a shared Space whose owners cannot sign in.
`internal/service/spacerecovery` is the one narrow System Administrator action
for that case:

- it applies only when every recorded owner of a shared Space is disabled;
- the successor must already be an enabled member;
- it refuses a personal Space and creates no membership;
- it reuses the ordinary ownership transfer, demoting one disabled owner to
  admin;
- it gives the administrator no membership and no content access; and
- it records `space.ownership_recovered` naming the actor, the successor, and
  the demoted owner.

It is reachable as `PUT /api/admin/spaces/{space_id}/owner`, as Portal's "Make
owner" in Administration → Spaces, and as the break-glass
`buildmax-server space recover-owner <space_id> <successor_email>` (actor
`buildmax-server`) when the public Server or the IdP is unavailable. It is not a
general power to transfer a healthy Space. The audit event is best-effort after
the transfer commits, like every other audit write (§9), not transactional.

## 9. Audit Events

New actions, permanent once written:

```go
AuditSystemAdminGranted = "system.admin_granted"
AuditSystemAdminRevoked = "system.admin_revoked"
AuditUserCreated        = "user.created"
AuditUserDisabled       = "user.disabled"
AuditUserEnabled        = "user.enabled"
AuditLoginCodeIssued    = "user.login_code_issued"
AuditSessionsRevoked    = "user.sessions_revoked"
AuditModelEnabled       // exists
AuditModelDisabled      // exists
AuditAccessDenied       // exists; reused for admin routes, with space_id empty
```

All are written with `space_id` empty, because none of them is space-scoped. The
`AuditEvent` shape already allows that — it was designed for `user.login` — so
no schema change is needed. Owner recovery (§8.4) later added
`space.ownership_recovered`, which is written with the recovered Space's id so
the Space's own trail shows it.

Two things fall out of adding these:

**The operator commands record.** `buildmax-server user create` and
`login-code` write the same actions as their API equivalents, with
`actor_type: system` and `actor_id: "buildmax-server"` —
the actor id `model_admin.go` already writes, so one act does not acquire two
names. This closed gap 2 of §3 in M1. `model_admin.go` already does this; `user_admin.go` will match it. An
operator action that leaves no record is worse than one that names the machine
instead of a person, and naming the machine is the honest description of what
happened.

**The trail needs a reader that is not space-scoped.**
`ListAuditEvents(spaceID, ...)` cannot return a login or a grant, because those
have no space. `GET /api/admin/audit-events` therefore needs a second store
method with optional filters:

```go
SearchAuditEvents(ctx context.Context, f AuditFilter, limit, offset int) ([]AuditEvent, int, error)
```

`AuditFilter` holds optional `SpaceID`, `ActorID`, `Action`, `Since`, `Until`.
The existing owner-only space route keeps its narrow method: a space owner asks a
narrower question and must not accidentally acquire the wider one.

The failure policy does not change. A failed audit write is logged and dropped
rather than failing the action, exactly as `internal/service/audit` documents
today. That policy is decided rather than pending — see
[space-governance.md](./space-governance.md) §12 question 2 — and this design
does not reopen it. What it does is sharpen the one part still open: the
actions added here are where the argument for best-effort is weakest, because a
grant that was made and not recorded is the case an investigation most needs.
Whether a grant is the action that earns a transactional record is that
residue, and this design makes it more urgent rather than answering it.

## 10. Portal Administration Surface

Administration is separate from Space settings and appears as a first-level
sidebar destination only after `GET /api/admin/me` confirms a grant. Entering it
puts the sidebar on the deployment scope: the Space switcher is replaced by a
static "Deployment" label, and the sidebar lists the Administration sections
themselves (from the shared `ADMIN_NAV` in `portal/src/features/admin/nav.ts`)
followed by a "Back to space" return. The page renders only the active section,
not its own tablist. Navigation is presentation; the Server authorizes each
request independently.

`portal/src/pages/admin/AdminSettings.tsx` and `portal/src/features/admin` own
eight sections:

1. **Overview** — build/readiness, worker mode, run counts, caller grant, and
   collapsible redacted configuration.
2. **Administrators** — current and historical grants, grant by account email,
   and revoke with last-effective-holder protection.
3. **Accounts** — paginated filters, reloadable detail, creation, login codes,
   disable/enable, and live-session listing with single or bulk revocation.
   Disable first shows the deactivation impact (§8.3) and the webhook-key
   retirement choice.
4. **Spaces** — paginated metadata, membership and usage, and "Make owner" for
   the disabled-owner-only recovery (§8.4), without content access or
   quota-tier mutation.
5. **Models** — list, create with a write-only encrypted credential, enable and
   disable. No read returns the credential.
6. **LLM calls** — the managed call ledger across every Space: model, tokens,
   cost, and status per call, filtered by user, model, status, and surface. No
   prompts or generated content.
7. **Plugins** — catalog and release inspection, retirement, restoration, and
   yanking; Portal publication remains deferred.
8. **Audit** — cross-Space metadata search/export; time-bound filters remain
   API-only.

Session revocation affects refresh tokens. It does not invalidate an already
issued access token, and a last-rotation time is not a live presence signal.
The session listing is covered in `portal/e2e/admin.spec.ts`; single revocation
is exercised by `TestAdminSessionsListAndSingleRevoke`, not by that browser
scenario, which deliberately preserves its own login.

## 11. The Authorization Matrix

The space route matrix in `space_authz_matrix_test.go` exists because the checks
live in handler helpers rather than in one middleware, so a route that forgets
to call one is not a compile error. Admin routes have exactly that property, so
they get exactly that treatment — a sibling `system_authz_matrix_test.go`:

1. **Every `/api/admin/*` route the package registers has a row.** A route without one
   fails the build, and a row naming a route that no longer exists fails too,
   because a dead row reads as coverage. Shipped in M1, and verified by adding
   an unguarded route and watching it fail — a coverage test that has never
   fired is not known to work.
2. **Four callers drive every route as real requests**: a system admin, a space
   owner with no grant, an ordinary user, and an anonymous caller. Expected:
   200-ish, 403, 403, 401.
3. **A revoked grant is a non-admin.** The same user, after
   `RevokeSystemRole`, gets 403 on every row. This is what proves revocation is
   a live check rather than a startup read.
4. **A grant is not a space key.** A system admin with no membership drives the
   space-scoped content routes and gets 403 on every one. This is the test that
   makes §4's table true, and it is the one to look at first if anybody
   proposes consulting the grant inside `authorizeSpaceAction`. M4 restates it
   at the surface that most looks like an exception: an administrator who can
   read a space's name, size, and quota still cannot open its issues.
5. **No admin response contains a secret.** A response-body assertion over the
   account and config routes for `api_key`, `password`, `hash`, `secret`, and
   `token`. Crude, and it catches the realistic failure: someone returns a row
   struct instead of a response struct. Shipped in M2, which is where the first
   route that could carry one appeared.

Items 1–4 shipped in M1, along with two the sketch did not name: a grant store
error denies rather than admits, and a refused admin request is recorded with an
empty space while an unauthenticated one records nothing — there is no actor to
name, and an event keyed by an unauthenticated request would let anyone write
rows.

The proposal's "evidence needed" list asked for an end-to-end authorization
matrix and a lockout exercise. Items 1–4 are the matrix. The lockout exercise
is a store-level test: grant, revoke through the API until one remains, watch
the API refuse, revoke the last one with the command, then grant again — all
without touching the database directly.

## 12. Out Of Scope

- A second or third system role, until one has a caller (§5.1).
- Organization hierarchy, custom roles, per-resource ACLs.
- OIDC, SAML, SCIM. The [enterprise identity and access](enterprise-identity-and-access.md)
  record owns those. The only constraint this design places on it: an identity
  provider may *add* grants, and the operator command must keep working, so an
  IdP outage cannot lock a deployment out of its own administration.
- A log viewer, a log search, or anything SIEM-shaped (§7.2).
- Configuration writes (§7.2).
- Cross-space content access of any kind (§7.2).
- Audit export, retention policy, and deletion controls. They are open in
  [space-governance.md](./space-governance.md) §12 and this design does not
  change their answer.
- Billing, and any claim of strict spending control. The `llm_call` ledger
  records what was spent after the fact; reservations do not exist.

## 13. Backend Plan

### M1. Grant Model, Command, And Authorization — DONE

`identity.SystemGrant` and `SystemGrantStore`, `systemGrantRow` on `system_grant`,
opaque public IDs, `buildmax-server admin grant | revoke`,
`requireSystemAdmin`, the audit actions, and the first route —
`GET /api/admin/me` — so that the matrix test had something to cover from the
start.

Acceptance met: a grant can be made, listed, and revoked from the command line;
a granted user gets 200 on `/api/admin/me` and an ungranted one gets 403; every
transition is in the audit trail.

Two things landed differently from the sketch above, both deliberate:

- **The audit actor is `buildmax-server`, not `cli`.** `model_admin.go` was
  already writing that string for the same kind of act, and a trail with two
  names for one actor is worse than a name that only says which binary ran.
  `system_grant.granted_by` carries the same string for the same reason.
- **`requireSystemAdmin` authenticates before it checks the store**, the
  reverse of `withUserAndStore`. Checking the store first would answer an
  anonymous caller with "system administration not configured", which says
  something about the deployment to someone who has proved nothing about
  themselves.

### M2. Accounts — DONE

The `/api/admin/users` routes, `disabled_at`, the login/refresh/webhook
refusals, session revocation, the scheduler guard, the `requireActiveUser`
check, and the account audit actions. The `/api/admin/grants` routes landed
here too rather than in M1: the last-grant invariant needs a route to enforce
it on, and account lifecycle and authority lifecycle are the same operator's
job.

Acceptance met: an operator can create an account, issue a code, disable it,
and see the disabled account refused on the next request — with each step in
the trail.

Three things landed differently from the sketch:

- **Pending runs failed rather than canceled.** `CANCELED` did not exist yet.
  The deactivation lifecycle later replaced that with cancellation carrying
  `cancel_reason` (§8.1).
- **`requireActiveUser` refuses only an account that exists and is disabled.**
  A token naming an account the store does not have is allowed through
  unchanged. Nothing deletes accounts, so this is not a state a deployment
  reaches, and deciding what an unknown subject means on every route at once is
  a separate change from disablement.
- **An administrator cannot disable their own account.** They would lock
  themselves out mid-request with no way back except the operator command, for
  a mistake that is easy to make and pointless to allow.

### M3. System Status And Redacted Configuration — DONE

`GET /api/admin/system` and `GET /api/admin/config`. The readiness checks
bootstrap already registers are converted into the admin API's probe shape and
reported rather than re-invented, so the status page and `/readyz` cannot
disagree. Redaction is a whitelist of fields that may be shown, not a blacklist
of fields that must be hidden — a blacklist fails open on every field added
later.

Acceptance met, with three decisions worth recording:

- **The redacted view lives in `internal/config`**, as `ServerConfig.Redacted()`,
  not in the handler. When someone adds a configuration field, the decision
  about whether it may be shown is then in the file they are already editing.
  A test walks `ServerConfig` for fields whose names mark them as credentials
  and fails when one is unaccounted for, so the whitelist cannot silently fall
  behind the struct.
- **Version and deployment facts arrive from bootstrap.** `internal/server` may
  not import `internal/config` — the architecture test enforces it — and the
  same rule that keeps infrastructure detail out of the readiness endpoint
  keeps configuration detail out of this one.
- **`sandbox_surface` is reported empty**, and no longer because there is
  nothing to report: `internal/agentapp/taskrun` now passes
  `config.WorkerSandboxSurface()`. What a worker resolves to depends on the
  environment that worker runs in, and this endpoint answers from the
  server's. They agree in every deployment shipped today — both come from the
  same image — but agreeing by construction is not observing it, and a
  security surface that infers a boundary it cannot see can name the wrong
  one. Reporting the boundary a run actually resolved belongs with that run,
  which already pins its tiers for audit; wiring it into this endpoint is
  open work rather than a decision this record has made.

`GET /api/admin/system` degrades rather than failing: each of its reads is
best-effort, because a status page that answers 500 when one of its five
questions cannot be answered tells an operator nothing during exactly the
outage they opened it for. A failed dependency is named and not explained,
matching `/readyz` — connection errors carry DSNs, endpoints, and bucket names,
and the reason belongs in the log where an operator already has to be.

### M4. Cross-Space Audit And Space Metadata — DONE

`SearchAuditEvents` with `AuditFilter`, `GET /api/admin/audit-events`, and the
two space metadata routes. The existing space route is untouched, deliberately: a
space owner asks a narrower question, and handing that reader the wider method is
how a space-scoped route quietly acquires a deployment-scoped answer.

Acceptance met. Two details worth recording:

- **`space_id=none` is a filter value.** An empty `space_id` already means "any
  space", so the events no space-scoped reader can ever see — logins, grants,
  account actions — needed a spelling of their own to ask for.
- **`AuditFilter` has no free-text field, and should not grow one.** The trail
  holds who did what to which object; a text query would invite a `Detail LIKE`
  scan over a column whose whole purpose is to stay small and structured.

`AdminSpace` carries no field a member wrote or an agent produced. The pressure
on that boundary is real and will arrive as a reasonable-sounding request — an
issue count, then a list, then titles — so the test asserts the response
mentions no issue, conversation, artifact, task, workflow, or trace at all.

### M5. Portal `/admin` — DONE

The initial Portal slice shipped and has since expanded to §10's eight
sections, with first-level navigation, grant management, pagination, account
session controls, and redacted configuration. The old five-page plan no longer
describes the current Portal.

Acceptance met: an admin sees the area, a non-admin sees no entry and is sent
home rather than to a forbidden screen, and the server refuses either way.

Three things worth recording:

- **Every page states what it does not show.** The spaces page says members and
  capacity, not work; the account detail says spaces are listed by name and role
  only. This is not decoration — an operator who assumes otherwise eventually
  reports a missing feature that is actually the boundary.
- **There is no link from a space into its contents.** A link that 403s reads
  as a bug rather than as a boundary, so the page does not offer one.
- **The destructive actions say what they will do before they do it.**
  Disabling names every credential it stops, says sessions are revoked and
  queued work will not start, and says it is not deletion. A login code says it
  is shown once and recoverable nowhere. Discovering either afterwards is how
  an operator learns to distrust the surface.

Browser coverage is `portal/e2e/admin.spec.ts`, which needs the test account to
hold a grant — `./make e2e` now issues one alongside the login code.

### M6. Model Catalog Read And Toggle — DONE

`GET /api/admin/llm/models` and the enable/disable routes, reusing the audit
actions `model_admin.go` already writes. Model creation subsequently shipped
through the Admin API, Portal, and `buildmax admin model add`; the database-direct
command remains for bootstrap.

Acceptance met: a model can be retired from Portal, the catalog response
contains no key, and the trail does not distinguish a CLI toggle from an API
one except by actor — a command names the binary, the route names a person.

One addition the sketch did not have: **the response says which model name is
the deployment default**. Every enabled catalog entry is callable by every user
of the deployment, and its unique name is the stable client-facing identifier;
the alias layer and per-Space model policy were withdrawn by
[client-modes.md](client-modes.md).

Quota tier assignment, if it is wanted, comes after M6 with its own store
method and its own audit action. It is the one item in §7.1 that was a feature
rather than a window onto existing state, and nothing has asked for it yet.

## 14. Frontend Plan

1. **Gate and shell.** `getAdminMe`, the `admin` route segment, and a nav entry
   rendered only on success. A 403 is an expected answer, not an error toast —
   the same convention `features/audit/api.ts` already documents.
2. **Overview.** Status cards from `GET /api/admin/system`, with a failed
   readiness check named and unexplained, matching what `/readyz` does and for
   the same reason.
3. **Accounts.** A searchable list, a detail panel, and confirm dialogs on
   disable and on session revocation that say what will happen to sessions and
   to pending runs. A login code is shown once, with that stated before it is
   generated rather than after.
4. **Spaces.** Metadata table with the scope line from §10.
5. **Audit.** The cross-space trail reusing `features/audit/describe.ts`, which
   already renders an unrecognised action verbatim — the property that keeps a
   Portal older than its server from hiding events.
6. **Models.** Catalog table with enable/disable and a model-creation form.
   The earlier CLI-only addition restriction was superseded by encrypted
   write-only credential handling in the Admin API.
7. **LLM calls.** A filterable list over `GET /api/admin/llm/calls`, pricing each
   row from its own rate snapshot the way the space-scoped run view does, and
   showing the routing (`target_id`, `provider_type`, `upstream_model`) that view
   hides because here the reader is the operator who set it.

## 15. Validation

Backend:

```sh
./make test ./internal/server/handlers/... ./internal/service/audit ./internal/bootstrap
./make test mysql
```

Frontend:

```sh
cd portal && npm run build && npm test
```

Full:

```sh
./make check ci
```

Manual scenarios, each of which is a claim in this document:

1. Grant, list, and revoke from the command line against an empty deployment.
2. A granted user reaches `/admin`; a space owner without a grant does not.
3. A system admin calling a space content route without membership is refused.
4. Revoking a grant ends the admin's access on the next request, not at
   token expiry.
5. The API refuses to revoke the last grant; the command allows it and says so.
6. Disabling an account: an in-flight request fails, refresh fails, the
   password login says `account_disabled`, a webhook key is refused, Schedules
   pause with `creator_disabled`, and pending and running runs end `CANCELED`
   with `cancel_reason = creator_disabled`.
7. Every step above appears in the cross-space audit trail with the right actor.
8. `buildmax-server user create` now appears in the trail as a system actor.
9. No admin response contains an API key, a hash, or a token.

## 16. Risks

- **The grant becomes a shortcut.** The first time a handler is hard to
  authorize, someone will reach for the grant inside a space check. §11 item 4
  is the test that fails, and it should keep the comment explaining why.
- **The admin API grows into an infrastructure console.** The proposal's
  non-goal is right: BuildMax should report its own state, not become a
  replacement for `kubectl`. Every addition should be checkable against §2's
  question list.
- **Metadata creep toward content.** "Just the issue titles" is metadata by one
  reading and content by another. The rule: if a space member wrote it or an
  agent produced it, it is content.
- **Bootstrapping through a config value.** It will be proposed again, because
  it is convenient in a Helm chart. §6 is the answer, and the cost is a second
  authority the trail cannot describe.
- **A best-effort audit write on a grant.** The weakest point in this design
  (§9). It is inherited rather than introduced: the best-effort policy itself
  is settled, and what should be raised again is the residue of space-governance
  question 2 — whether a grant is the action that earns a transactional
  record.
- **Disable is not delete.** Nothing here deletes an account or its data, and
  an operator who reads "disable" as "remove" will be wrong. Deletion belongs
  with space-governance question 10, which has no answer today.

## 17. Open Questions

1. Does a `system_observer` role have a real caller? The column exists (§5.1);
   the role should not, until someone needs to give a person status and audit
   without account control.
2. ~~Should `disabled_at` also block a personal space's existing task runs from
   being retried by a *spacemate*?~~ **Decided: no.** The space owns the work,
   and authority follows each run's initiator (§8.2). Disabling withdraws that
   account's ability to ask for work; it does not quarantine the backlog of a
   space whose remaining members are in good standing. Eligibility keys on the
   run's own `created_by`, so a rerun by someone else is a new run by a
   non-disabled actor and dispatches. What is genuinely unsettled sits a
   level down and is a space-model question rather than an administration one:
   nothing stops an owner adding members to a *personal* space, which is the
   only reason this case is reachable.
3. Does the admin API need rate limiting before it needs anything else? Login
   is unthrottled today ([deploy/authentication.md](../deploy/authentication.md)),
   and account enumeration through `GET /api/admin/users?q=` is a much smaller
   problem than that one — but it is a new surface with a search parameter.
4. What does an admin see for a deployment with no database? Every store is
   nil-able by design and the routes will answer 503; whether Portal should
   show the area at all in that state is unresolved.
5. When OIDC lands, does a grant follow the account or the identity? If an IdP
   subject is re-linked to a new user row, an unfollowed grant is a lockout and
   a followed one is a privilege transfer nobody approved.
6. ~~Should a run stopped by a disable be distinguishable from one that
   failed?~~ **Decided: yes.** Withdrawn authority ends a run `CANCELED` with
   `task_run.cancel_reason` (`creator_disabled` or `creator_not_member`), which
   is immutable once recorded and distinct from `user_requested` (§8.1).
7. ~~Should an account's webhook keys survive a disable?~~ **Decided: the
   operator chooses.** A suspension keeps them, refused while disabled and
   usable again on re-enable; a leaver disable with `retire_webhook_keys`
   revokes them permanently (§8.1).
8. ~~Should model aliases become editable through the API?~~ **Retired.**
   [Client modes](client-modes.md) removed the alias layer: clients name the
   unique deployment-wide catalog entry directly, and the admin response names
   the configured default.
9. Is the cancellation bound — the worker's cancel poll plus the stale-run
   cancel grace — acceptable for the first named enterprise deployment, or must
   the runner delete or terminate the worker after a shorter emergency bound?
10. Is disabled-owner-only recovery (§8.4) enough, or must a deployment require
    a second Space owner before offboarding?
11. Does a target deployment need managed CLI and Desktop SSO, which would add
    native session and local credential cleanup to the leaver journey?
12. At what observed cardinality does the eligibility reconciler need a durable
    work queue rather than bounded scans of active runs?
13. Should `buildmax admin` reach parity with Portal and the Admin API? It has
    no session list or single-session revoke verb, no deactivation impact or
    webhook-key retirement choice, and no owner recovery; §6.1 names no reason
    to keep those off the automation surface.
14. Portal's audit search and export omit the API's `since` and `until` bounds.
    Adding them needs only a caller that wants them.
15. Portal plugin publication is deferred by decision: `buildmax plugin publish`
    stays the release surface, and Portal inspects, archives, restores, and
    yanks. Reopen it only if an operator without a checkout needs to publish.
16. Which runtime aggregates are actionable without revealing Space content —
    TaskRuns by status and age, oldest pending age, cancel requests and
    stale-run candidates, worker heartbeat freshness, Spaces near a quota
    limit? Any such view must take deployment-wide meaning from durable state or
    the shared coordinator, never one process's memory; group failures by a
    safe error class, never raw error text; and keep global dispatch pause,
    force-cancel, and cross-Space retry out unless each gets its own authority
    and multi-instance consistency decision and audit action.
17. Which session metadata is useful without becoming a device-fingerprinting
    surface? Platform and timestamps exist; IP address and user agent are not
    recorded.
18. Should authority changes get transactional audit? `internal/service/systemadmin`
    records grant and revoke audit after the store call commits, so a committed
    change does not guarantee its audit row (§9, §16). Acceptance would need
    either a rollback or an explicit reported outcome when audit storage fails,
    proved against MySQL.
