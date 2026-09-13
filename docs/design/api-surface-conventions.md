# API Surface Conventions

> **简体中文：** [阅读中文镜像](../zh-CN/design/API表面约定.md)

> **Audience:** contributors adding HTTP routes · **Status:** Active plan — the
> conventions govern new routes immediately; the reconciliation in §6 is planned
> work decomposed into backlog tasks, none yet shipped

How the BuildMax server's HTTP surface is named, placed, and documented. Two
halves, both in this record: the forward-looking conventions that decide where a
new route lives (§3–§5), and the reconciliation plan that converges the few
divergent existing routes onto them (§6). The reconciliation is written out in
full here — every current route that changes, mapped to its target — so a
reviewer sees the whole surface delta in one place before it is split into
backlog tasks.

This record replaces the *API surface conventions* proposal, which has been
retired. It keeps that paper's two settled decisions (no URL versioning, split
OpenAPI along the listener boundary), promotes its naming rules to durable
conventions, and adds the one decision the proposal named but left open: a
common prefix for authentication routes (§3.7). Git history holds the proposal.

Related: [Worker API network boundary](worker-api-network-boundary.md),
[Unified artifacts](unified-artifacts.md), [Space secrets](space-secrets.md),
[Entity identity](entity-identity.md),
[server architecture](../contribute/architecture/server.md),
[current state](../current-state.md), [ROADMAP.md](../ROADMAP.md), and the
[backlog](../backlog/README.md).

## Contents

- [1. Problem And Scope](#1-problem-and-scope)
- [2. Goals And Non-Goals](#2-goals-and-non-goals)
- [3. Naming And Placement Conventions](#3-naming-and-placement-conventions)
- [4. Decision: No URL Versioning Yet](#4-decision-no-url-versioning-yet)
- [5. Decision: Split OpenAPI Along The Listener Boundary](#5-decision-split-openapi-along-the-listener-boundary)
- [6. Reconciliation Plan](#6-reconciliation-plan)
- [7. Options Considered](#7-options-considered)
- [8. Resolved Questions](#8-resolved-questions)
- [9. Status And Rollout](#9-status-and-rollout)

## 1. Problem And Scope

The server exposes roughly 150 route registrations across two listeners. Their
composition is authoritative in each handler subpackage's `Register` method,
composed in `internal/server/handlers/routes.go` through `RegisterPublic` and
`RegisterWorker`, and mirrored exactly by `internal/server/static/openapi.json`.

The surface grew feature by feature. Most of it already agrees — kebab-case path
segments, plural collections, `{xxx_id}` path parameters — but a handful of
shapes were chosen locally and now disagree with each other. Because BuildMax is
Alpha with no frozen API contract, and every client (Portal, Desktop, CLI,
workers) ships from this repository and deploys with the server, this is the
cheapest moment to converge the surface rather than carry the divergence
forward. The right correction is coherent — server, OpenAPI, clients, and tests
in one change per route — not a compatibility alias that leaves both shapes
live.

This record governs the addressable HTTP surface and its documentation only. It
does not touch handler internals, authorization logic, storage, or the
two-listener network boundary, which is already decided in
[Worker API network boundary](worker-api-network-boundary.md).
`internal/tool/names.go` remains authoritative for LLM-facing tool names; these
conventions do not reach them.

## 2. Goals And Non-Goals

Goals:

- State a small set of rules that decide, without further debate, where a new
  route lives and how it is named.
- Settle URL versioning and the OpenAPI document shape.
- Enumerate the complete old→new mapping for the divergent routes, reviewable in
  one place, so the surface converges on one style rather than accumulating a
  third.

Non-goals:

- A public, externally supported API contract. There is no out-of-band consumer
  today; committing to one is a separate, evidence-driven decision.
- Rewriting handler internals, authorization, or storage.
- Changing the two-listener network boundary.

## 3. Naming And Placement Conventions

These rules are effective for every new route immediately. Most were already
followed; writing them down removes the per-route argument.

### 3.1 Path Syntax

Path segments are kebab-case and collections are plural: `task-runs`,
`webhook-keys`, `audit-events`. Already consistent; now a rule.

### 3.2 Path Parameters

A path parameter is `{resource_id}` for a public identifier, and a descriptive
name (`{plugin_name}`, `{version}`, `{revision}`) where the segment is a natural
key rather than a `NewPublicID`. See [Entity identity](entity-identity.md).

### 3.3 Top-Level Versus Space-Scoped Ownership

A resource is space-scoped — `/api/spaces/{space_id}/...` — when it is managed
within one Space. A top-level route — `/api/...` — is reserved for the acting
subject, the authenticated account, and its cross-Space view: "everything I own
or can see" aggregates such as `/api/usage` and the invitations I received.

`webhook-keys` is account-owned — the `user_webhook_key` table keys every row by
`user_id` — so it belongs at the top level exactly where it is and does not
move. It only read as ownerless because the rule was unwritten. (Its handler
currently lives in the `space` package despite being account-scoped; relocating
it is a follow-up in §6.5, not a route change.)

### 3.4 Collection Versus Single-Entity Addressing

Collection operations (create, list) hang off the parent path. Reading or
mutating one entity that owns a durable id uses the flat
`.../{entity}-runs/{id}` form, which needs no parent to locate it. This is why
`task-runs` and `workflow-runs` are created and listed under a task or workflow
(`.../tasks/{task_id}/runs`) but read by their own id
(`.../task-runs/{task_run_id}`). One rule now covers both.

### 3.5 State Transitions

A transition that only sets a stored lifecycle flag — a boolean or small enum
the resource already carries — is a state sub-resource, `PUT .../state`, the
shape Space secrets already use. A `POST .../{verb}` action is reserved for an
operation `set attribute = X` cannot express: one that creates a new entity or
acts on a live execution. The boundary is idempotence and side effects, not the
English verb. Two enable/disable POSTs collapse into one idempotent `PUT`, so
this also shrinks the surface.

### 3.6 Authorization By Record

Where a resource is globally identified, the route addressed by its id takes the
Space from the record, not the path. Artifacts are the reference pattern:
`/api/artifacts/{artifact_id}` carries no `space_id` because the record does. See
[Unified artifacts](unified-artifacts.md). Do not add a redundant Space segment
to a route the record already authorizes.

### 3.7 Authentication Route Grouping

Session and credential routes for the acting subject group under a common
`/api/auth/` prefix. Today they are scattered directly under `/api`
(`/api/login`, `/api/logout`, `/api/password`, `/api/otp/request`) with only
`/api/token/refresh` grouped, so the auth surface has no single place to find or
document. Grouping them gives the auth listener slice one prefix and one OpenAPI
`tag`. This is the one decision the retired proposal named as an inconsistency
but did not resolve; §6.2 maps it and it is the review point that goes beyond the
proposal's approved rules.

## 4. Decision: No URL Versioning Yet

URL versioning (`/api/v1/...`) is a compatibility tool for a consumer you cannot
redeploy together with the server. That consumer does not exist: Portal,
Desktop, CLI, and workers all ship from this repository and deploy with the
server, and the N-1 rollback promise has been withdrawn, so there is no version
skew to bridge. Adding `/v1/` now would ship a version that never gets a
successor, because the surface can change in lockstep with its clients — a
concept with exactly one value, which Occam's razor rejects.

Introduce versioning only when there is evidence of an out-of-band consumer that
pins a version — a public API, a third-party integration, or a published SDK.
That is an evidence-driven decision, not a calendar one, and its likely shape is
a versioned public subset, not a global `/v1/` prefix over the whole internal
surface.

The `info.version` field in `openapi.json` is spec metadata, not a URL version.
Today it is a hand-set literal (`0.0.7`) tied to nothing and read by nothing, so
it drifts. Decided: the build stamps it from the single application-version
source — the git tag `tools/mk` already injects into the `config.Version` build
variable at link time — rather than by hand. OpenAPI 3.0 makes `info.version`
required, so this ties it to a real source rather than dropping it; whether the
build rewrites the served spec or the `GET /openapi.json` handler injects
`config.Version` at serve time is an implementation choice for the task.

## 5. Decision: Split OpenAPI Along The Listener Boundary

Today one `openapi.json` documents both listeners, including `/api/worker/*`.
This erases, in documentation, a boundary the code and the network deliberately
maintain: the public listener cannot dispatch a worker route, the two use
different authentication (user JWT versus run token), and they run on separate
sockets. See [Worker API network boundary](worker-api-network-boundary.md).

Split the specification along that existing boundary into a public document and a
worker document, each corresponding to one `Register*` method. This is not a new
concept — it follows an invariant already enforced in code — and it lets the
"spec matches routes exactly" check run per listener instead of reconciling one
large file against two route sets by hand.

Do not split finer. Admin, shared-artifact, and auth routes share the public
listener and one authentication scheme (or an explicit unauthenticated one);
separating them into more documents would multiply artifacts without a boundary
to justify it. Group those sub-audiences with OpenAPI `tags` inside the public
document.

## 6. Reconciliation Plan

Every route below is a current registration that changes. Routes not listed
already conform and stay as they are; §6.4 records the reference patterns that
must not be "fixed." Each change is coherent — server route, `openapi.json`,
every client that calls it, and the route/spec tests move together — with no
alias left behind, per §1.

### 6.1 State-Transition Renames

Two enable/disable POSTs per resource collapse into one idempotent
`PUT .../state`, following §3.5. All four are on the admin listener slice.

| Current route(s) | Target | Stored flag |
|---|---|---|
| `POST /api/admin/users/{user_id}/disable`, `.../enable` | `PUT /api/admin/users/{user_id}/state` | `disabled` |
| `POST /api/admin/llm/models/{model_id}/enable`, `.../disable` | `PUT /api/admin/llm/models/{model_id}/state` | enabled/disabled |
| `POST /api/admin/plugins/{plugin_name}/archive`, `.../unarchive` | `PUT /api/admin/plugins/{plugin_name}/state` | `archived` |
| `POST /api/admin/plugins/{plugin_name}/releases/{version}/yank` | `PUT /api/admin/plugins/{plugin_name}/releases/{version}/state` | `yanked` |

### 6.2 Authentication Route Grouping

Following §3.7. Bodies, methods, and authentication are unchanged; only the path
moves under `/api/auth/`.

| Current route | Target |
|---|---|
| `POST /api/otp/request` | `POST /api/auth/otp` |
| `POST /api/login` | `POST /api/auth/login` |
| `POST /api/logout` | `POST /api/auth/logout` |
| `POST /api/password` | `POST /api/auth/password` |
| `POST /api/token/refresh` | `POST /api/auth/token/refresh` |

### 6.3 Actions That Stay Actions

These are `POST .../{verb}` and remain so under §3.5: each creates a new entity
or commands a live execution, which `PUT .../state` cannot express. Listed so a
later reader does not mistake them for the renames in §6.1.

| Route | Why it stays an action |
|---|---|
| `POST /api/spaces/{space_id}/tasks/{task_id}/cancel` | commands a live run |
| `POST /api/spaces/{space_id}/tasks/{task_id}/retry` | creates a new run |
| `POST /api/invitations/{invitation_id}/accept` | creates a membership |
| `POST /api/spaces/{space_id}/agents/{agent_id}/revisions/{revision}/restore` | creates a new revision |
| `POST /api/spaces/{space_id}/workflows/{workflow_id}/revisions/{revision}/restore` | creates a new revision |

### 6.4 Reference Patterns — No Change

These already embody the conventions and must not be altered:

- **Dual run addressing (§3.4).** `task-runs` and `workflow-runs` create and list
  under a parent and read by durable id. Keep both forms.
- **Authorization by record (§3.6).** `/api/artifacts/{artifact_id}` and its
  sub-routes take the Space from the record. Keep.
- **Top-level account aggregates (§3.3).** `/api/usage`, `/api/invitations`, and
  `/api/webhook-keys` are the acting subject's cross-Space view. Keep at the top
  level.
- **State sub-resource (§3.5).** `PUT /api/spaces/{space_id}/secrets/{secret_id}/state`
  and `PUT /api/spaces/{space_id}/plugin-curation` are already the target shape.
- **Partial update.** `PATCH` on a resource
  (`.../agents/{agent_id}`, `.../plugin-activations/{plugin_name}`, `.../secrets/{secret_id}`)
  is the correct verb for editing fields and is unaffected by §3.5.

### 6.5 Non-Route Follow-Ups

- The `webhook-keys` handler lives in the `space` package though its routes are
  account-scoped (§3.3). Relocate it to a fitting account-level package. This is
  an internal move; the route does not change and clients are unaffected.

### 6.6 OpenAPI And Version Metadata

- Split `openapi.json` into a public and a worker document along
  `RegisterPublic`/`RegisterWorker` (§5), and make the "spec matches routes"
  check run per listener.
- Stamp `info.version` from `config.Version` (§4).

## 7. Options Considered

- **Versioning: adopt `/api/v1/` now.** Rejected: no consumer needs it, and it
  freezes today's shape as "v1" during Alpha, contrary to the product
  principles.
- **Versioning: header- or content-negotiation-based.** Same objection — it
  solves a skew problem that does not exist yet — with more machinery than a URL
  prefix.
- **OpenAPI: keep one document.** Rejected: it documents the worker control plane
  to public-surface readers and makes the match-the-routes check reconcile one
  file against two disjoint route sets.
- **OpenAPI: one document per handler subpackage.** Rejected: subpackages are an
  internal decomposition, not an external boundary; the listener split is the
  boundary that matters to a reader and to network policy.
- **Reconciliation: keep both old and new shapes behind aliases.** Rejected: no
  external consumer needs the old path, and a live alias is exactly the
  divergence this record removes. Alpha lets the change be coherent instead.
- **Auth grouping: leave routes flat under `/api`.** Rejected: the auth surface
  then has no single prefix or tag, which the proposal already flagged as the
  reason it reads as ad hoc.

## 8. Resolved Questions

- Do the two OpenAPI documents live as separate committed files, or as one source
  generated into two views? **Decided: two committed files** —
  `internal/server/static/openapi.json` (public) and `openapi-worker.json`
  (worker). The spec is hand-maintained beside its handlers, so there is no
  generator to add; two files keep the match-the-routes check reading each
  document against its listener's routes directly. The worker document is a
  committed, test-validated artifact; it is not served on the worker listener,
  which stays minimal (the listener-boundary test keeps `/openapi.json` off it).
  Per-operation `tags` grouping the public document's sub-audiences (admin, auth,
  shared-artifact) is a documentation nicety left for a follow-up; the split
  itself is the boundary the checks enforce.

## 9. Status And Rollout

The conventions in §3–§5 govern new routes now. The §6 reconciliation is planned;
nothing has shipped. It decomposes into backlog tasks, each self-contained and
verifiable:

1. Split `openapi.json` along the listener boundary and run the match-the-routes
   check per listener (§6.6, resolving §8).
2. Stamp `info.version` from `config.Version` (§6.6).
3. Admin state-transition renames to `PUT .../state` (§6.1).
4. Group authentication routes under `/api/auth/` (§6.2).
5. Relocate the `webhook-keys` handler to an account-level package (§6.5).
6. Fold §3–§5 into
   [server architecture](../contribute/architecture/server.md) as the place a
   contributor adding a route looks, and delete this record when the
   reconciliation has merged. Git history keeps the rationale.
