# Server

> **简体中文：** [阅读中文镜像](../../zh-CN/contribute/architecture/server.md)
> **Audience:** contributors · **Status:** current
>
> The live route list is the API's own `GET /openapi.json`, browsable at `/swagger/`.

## Purpose

`internal/server` provides the Go HTTP backend for Portal and worker callbacks.
It is started by `cmd/buildmax-server` through `internal/bootstrap/server.go`.

The server owns route registration, middleware, WebSocket handling, worker API
callbacks, and scheduler startup. Business workflows are delegated to
`internal/service/*`.

The root handler constructs its route-group handlers and their application
services once in `handlers.NewHandler`. Requests reuse those instances; helper
methods do not assemble fresh service graphs per call.

`server.New` builds two muxes on two listeners. `handlers.RegisterPublic` puts
every route except the worker package on the public listener (`Config.Addr`);
`handlers.RegisterWorker` puts only `/api/worker/*` on the worker listener
(`Config.WorkerAddr`, default `127.0.0.1:5679`). The worker routes are absent
from the public mux, so the public socket returns `404` for them regardless of
the token presented. `handlers.Register` composes both onto one mux for tests
and single-listener embeddings; the server never uses it. CORS wraps only the
public listener; request logging wraps both. On shutdown the public listener
closes before the worker one, so a worker can still report while the public
surface drains. The `Config.WorkerAddr`-empty case (used by handler tests)
builds the worker handler but opens no second socket. When `Config.WorkerTLS`
is set the worker listener serves HTTPS (`ListenAndServeTLS`); the public
listener never carries TLS because it terminates at the Ingress. The worker
side builds one `workerclient` HTTP client from its configured trust and uses
it for every call back, managed inference included. See
[design/worker-api-network-boundary.md](../../design/worker-api-network-boundary.md).

## Key Areas

| Area | Package / File | Role |
|------|----------------|------|
| Server wrapper | `internal/server/server.go` | Builds `http.Server`, middleware, static OpenAPI/Swagger routes |
| Handlers | `internal/server/handlers` | Portal API, worker API, webhook, WebSocket handlers |
| Scheduler | `internal/server/scheduler` | Claims pending task runs and launches workers; also runs the background sweeps — expired credentials, abandoned runs, audit retention, and Workflow recovery (reconciling due runs stranded by a lost callback or restart) |
| Bootstrap | `internal/bootstrap/server.go` | Wires DB, storage, LLM, quota, handlers, scheduler |

## Main Route Groups

- Health and API description: `/healthz`, `/openapi.json`, `/swagger/`
- Auth: `/api/auth/otp`, `/api/auth/login`, `/api/auth/token/refresh`, `/api/auth/logout`
- Liveness and readiness: `/healthz`, `/readyz`
- Spaces and members: `/api/spaces...`
- Agents: `/api/spaces/{space_id}/agents...`
- Issues: `/api/spaces/{space_id}/issues...`
- Workflows: `/api/spaces/{space_id}/workflows...`
- Files: `/api/spaces/{space_id}/upload`, `/files...`
- Conversations: `/api/spaces/{space_id}/conversations...`
- Tasks: `POST /api/spaces/{space_id}/tasks` creates a Space-owned Task and its
  first TaskRun directly, from a typed `agent_id` — no Conversation is
  created or consulted. `GET .../tasks/{task_id}` and `GET
  .../tasks/{task_id}/runs` read a Task's thread; `POST
  .../tasks/{task_id}/runs` is Continue, creating a new-input TaskRun on the
  same Task. `POST .../tasks/{task_id}/cancel` — see "Cancelling a run" below
  — and `POST .../tasks/{task_id}/retry` — see "Retrying a run" — round out
  the set. `GET`/`POST .../agents/{agent_id}/tasks` are the Agent-nested
  convenience routes: same Task service, same resource. See
  [agent execution and Task threads](../../design/agent-execution-and-task-threads.md)
- Artifacts: `/api/artifacts/{artifact_id}` and `/content`, with
  `/api/spaces/{space_id}/artifacts` for the space's listing and upload, and
  `POST /api/artifacts` for a client that has a login but has not chosen a space
  — an optional `?space_id=` is honoured and no space means the caller's personal
  one, which is how CLI and Desktop publish. The
  ID-addressed routes take the space from the record rather than the path, so
  they use `Guard.MemberOfResourceSpace` and answer a non-member with `404` — an
  artifact ID is an identifier, not a credential, and a `403` would make the route
  an oracle for which IDs exist. See
  [../../design/unified-artifacts.md](../../design/unified-artifacts.md)
- Run outputs (the compatibility surface):
  `/api/spaces/{space_id}/task-runs/{task_run_id}/artifacts...`
- Run trace: `/api/spaces/{space_id}/task-runs/{task_run_id}/trace`
- Managed model calls: `/api/spaces/{space_id}/task-runs/{task_run_id}/llm-calls` —
  what a run spent and on which model, without prompts or the operator's catalog
  routing. Authorizing the run authorizes its ledger: the rows carry no space of
  their own
- Managed gateway (**not** space-scoped): `/api/llm/models` and
  `/api/llm/completions`. Every catalog model is available to every signed-in
  user, and a call is attributed to the person who made it. See
  [../../design/client-modes.md](../../design/client-modes.md)
- Usage: `/api/usage`, `/api/spaces/{space_id}/usage`
- Audit trail (owner only): `/api/spaces/{space_id}/audit-events`, and
  `/audit-events/export` for the whole trail as CSV or JSONL. The export is
  itself recorded, and pages by keyset cursor rather than offset so a table
  written to while it streams cannot skip a record
- Webhook keys (user-scoped, not space-scoped): `/api/webhook-keys...`
- Chat-account links (user-scoped): `/api/channel-links...` to list, confirm a
  bot's link code, and unlink, plus `GET /api/channel-link-pairings?code=` to
  preview which chat account a code would link. See
  [instant-messaging channels](../../design/instant-messaging-channels.md).
- WebSocket: `/api/spaces/{space_id}/ws`
- Worker API (**internal listener only**, not the public port):
  `/api/worker/task-runs/{task_run_id}...`, including `/llm/completions` so a
  worker needs no provider credential and `/artifacts` so a run's agent can keep
  a file for the space. The worker never says which space it is writing to: the
  run token names the run, the run names the task, and the task names the space.
  Each route also enforces the run's lifecycle (`requireRunning`): everything but
  the `GET` poll is refused unless the run is RUNNING, so a leaked but unexpired
  token cannot act before the claim or after the run is terminal. See
  docs/design/worker-api-network-boundary.md §8
- Inbound webhook: `/api/webhook`

## Route Conventions

These rules decide where a new route lives and how it is named, so the surface
stays coherent without a per-route argument. They govern HTTP routes only;
`internal/tool/names.go` remains authoritative for LLM-facing tool names.

- **Path syntax.** Segments are kebab-case and collections plural (`task-runs`,
  `webhook-keys`, `audit-events`).
- **Path parameters.** `{resource_id}` for a public identifier; a descriptive
  name (`{plugin_name}`, `{version}`, `{revision}`) where the segment is a
  natural key rather than a `NewPublicID`. See
  [entity identity](../../design/entity-identity.md).
- **Top-level vs space-scoped.** A resource managed within one Space is
  space-scoped (`/api/spaces/{space_id}/...`). A top-level route (`/api/...`) is
  reserved for the acting account and its cross-Space view — "everything I own or
  can see" aggregates such as `/api/usage`, the invitations I received, and
  `/api/webhook-keys` (account-owned; the handler lives in
  `internal/server/handlers/account`).
- **Collection vs single entity.** Collection operations (create, list) hang off
  the parent path; an entity with a durable id is read or mutated by the flat
  `.../{entity}-runs/{id}` form. This is why `task-runs` and `workflow-runs` are
  created under a parent but read by their own id.
- **State transitions.** A transition that only sets a stored lifecycle flag is a
  state sub-resource, `PUT .../state` (Space secrets, admin `users`/`llm models`/
  `plugins`/`releases` state). `POST .../{verb}` is reserved for an operation
  `set attribute = X` cannot express — one that creates a new entity or acts on a
  live execution (`.../cancel`, `.../retry`, `.../accept`, `.../restore`). The
  boundary is idempotence and side effects, not the English verb.
- **Authorization by record.** A route addressed by a globally-unique id takes
  the Space from the record, not the path (Artifacts are the reference pattern).
- **Auth grouping.** Session and credential routes for the acting subject share
  the `/api/auth/` prefix.
- **No URL versioning.** There is no out-of-band consumer to bridge — every
  client ships from this repository and deploys with the server — so the surface
  changes in lockstep with its clients rather than carrying a `/v1/` that never
  gets a successor. Introduce versioning only for an evidence-backed pinned
  consumer, as a versioned public subset rather than a global prefix.
- **OpenAPI is split along the listener boundary.** The public document
  (`internal/server/static/openapi.json`) and the worker document
  (`openapi-worker.json`) each correspond to one `Register*` method, and the
  "spec matches routes" architecture check exercises those registration methods
  on separate muxes rather than inferring ownership from path prefixes. It also
  checks that each document declares only security schemes its operations use;
  every worker operation must declare run-token authentication. `info.version`
  is stamped from the build version at serve time, not a hand-maintained literal.
  See [worker API network boundary](../../design/worker-api-network-boundary.md).

## Conversation Turns

One turn per conversation runs at a time. The turn queue
(`internal/server/turnqueue`) owns a queue per conversation
and serializes foreground entry paths — WebSocket messages, the HTTP
`POST .../messages` and `POST .../conversations` routes, and chat-platform
messages the channel gateway delivers through `Handler.RunChannelTurn` (see
[instant-messaging channels](../../design/instant-messaging-channels.md)). TaskRun completion
broadcasts durable-state invalidation; it does not enqueue a summary turn. It is server-scoped rather than connection-scoped because one
conversation is reachable from several connections at once.

A message that arrives while a turn is running is queued, up to 10 per
conversation, and runs as its own turn afterwards. WebSocket clients see
`conversation.message.queued`, then `conversation.message.dequeued` when it
starts; `conversation.message.completed` carries `queued_remaining`. Past the cap
the message is refused with `conversation.error` carrying `code: "queue_full"`
(HTTP: `429`), which does not end the turn in flight. Queues are in memory. See
[Queued messages](../../design/queued-messages.md).

## Where A Run Came From

`GET /api/spaces/{space_id}/task-runs/{task_run_id}` answers one run's
provenance: who or what asked, through which trigger, repeating which earlier
attempt, and the conversation message it was asked for in, quoted next to the
instruction the worker was given. Those last two are different texts — the
instruction is what Tier 1 decided to send — and holding both is the only way to
tell a constraint the model dropped from one the user never gave.

It also names the agent definition the run executed under, by revision. An
agent's instructions are resolved when its worker asks for the run, so editing
an agent changes what its next run does; the recorded revision is what says
which text produced a given outcome, and the response reports the definition's
current revision alongside it so a reader can see when the two have diverged.

It is a separate route from the trace because it survives a different absence: a
run that failed before an agent started wrote no trace and still came from
somewhere. A message that cannot be read, or that belongs to another
conversation, is left out rather than failing the request.

## Reporting A Finished Run

A run that reaches a terminal status announces one thing
(`internal/server/handlers/task_result.go`): every WebSocket connection on the
task's space receives `task.status.changed`, an invalidation and not the
outcome. A client answers it by re-reading the task from `task_run`, which is
authoritative and requires no separate delivery mechanism, model call, or
Conversation to be readable — a direct Agent Task has no Conversation at all.
An earlier design routed every finished run through a Tier 1 turn and a
durable `task_result_delivery` retry queue so a Conversation always received a
summary sentence; that forced path has been removed. See
[agent execution and Task threads](../../design/agent-execution-and-task-threads.md).

## Cancelling A Run

`POST /api/spaces/{space_id}/tasks/{task_id}/cancel` stops the task's run. What
happens next depends on whether a worker already holds it:

- **Not dispatched yet** (`PENDING`): one database transaction moves the run to
  `CANCELED`, projects that terminal state onto its task, and returns `200`.
- **Dispatched** (`SCHEDULED` or `RUNNING`): the request is recorded on the run
  (`cancel_requested_at`, `cancel_requested_by`) and the response is `202`. The
  worker polls `GET /api/worker/task-runs/{task_run_id}`, sees `cancel_requested`,
  ends its agent loop, uploads what the run produced, and PATCHes `CANCELED`.
- **Already finished**: `409`. Cancelling twice while a run is stopping is not an
  error — the second call answers `202` again.

The server never ends a started run itself: only the run's own process can stop
its agent loop, and a status written from outside would describe a run that is
still executing. `StaleRunReaper` is the backstop for a worker that never
confirms, and finishes such runs as `CANCELED` after a grace period.

That same poll is what tells the server a worker is alive. The route records
`task_run.last_seen_at` on every call, so a `RUNNING` run that goes silent for
longer than the reaper's liveness grace is failed as having lost its worker —
which is what a SIGKILL, an OOM kill, or a lost node looks like from here, none
of which leave the worker a chance to report. `worker.run_timeout` stays the
backstop for what that sweep cannot see: a run that never reached `RUNNING`, and
one with no recorded signal at all. Nothing is re-run: a reaped run had a worker
that may already have caused side effects, and the server cannot know whether
the task was safe to repeat.

A canceled run keeps its output and artifacts. It stopped early, but what it
produced is real work, and discarding it would make cancelling more expensive
than waiting.

`service/task.RequestRunCancel` owns the shared request-and-pending-finalization
operation used by the Task HTTP handler and Workflow reconciliation. A failed
or canceled Workflow node first commits `failing` or `canceling` on its run and
blocks pending nodes. The reconciler then requests cancellation for each active
sibling TaskRun and waits for terminal facts before ending the Workflow. That
durable state stays in the recovery sweep across restarts. Node state and output
reflect the actual TaskRun outcome even when success races with cancellation;
the Workflow retains its original failure or cancellation outcome.

## Retrying A Run

`POST /api/spaces/{space_id}/tasks/{task_id}/retry` runs the task's most recent
run again. It takes no body: the new run carries the previous run's input, and
records it in `retry_of_task_run_id` with `trigger_source` `task_retry`.

The input comes from the run rather than from the task because a task's later
runs can carry follow-up instructions, and repeating one means running that
again.

Three states answer `409`, each with its own reason:

- a run is already in flight — one task holds at most one active run, and the
  answer to a run taking too long is to cancel it first
- the task has never finished a run — there is nothing to repeat
- the task belongs to a workflow step — the workflow advances or fails its run
  from that step's outcome, so a retry started outside it would report a second
  outcome for a step that is already settled

`retry` creates a run the same way `POST /tasks/{task_id}/runs` does, so space
quota applies to it identically.

## Notes

- User-facing Portal APIs are space-scoped wherever work ownership matters.
- Worker APIs use the run token the scheduler minted for that task run rather
  than user JWT auth. The token carries the user, space, task, and run, and every
  route derives its resource scope from those claims. It is the only credential
  those routes accept: the old shared worker token has been removed — see
  [design/worker-run-token.md](../../design/worker-run-token.md).
- Signing in returns two credentials. The access token is a signed JWT the
  server does not store; the refresh token is a `user_refresh_token` row, which
  is what makes a session revocable. `internal/service/identity` owns the
  workflow and `internal/server/handlers/auth` its routes, and every rotation
  stays inside the session named by the access token's `sid` claim.
- Who the caller is, which space the request is about, and whether they may
  proceed are all answered by `internal/server/access`. Its `Guard` writes the
  refusal itself, so a route reads as a list of gates; the role/action decision
  it consults is `space.Allows` in `internal/core/space/policy.go`, the one
  implementation the space service shares with it.
- `POST /api/auth/login` accepts a password or an operator-issued, single-use login
  code. The latter is the account-claim and recovery path because BuildMax has
  no mail channel — see
  [deploy/authentication.md](../../deploy/authentication.md).
- See also: [Store](store.md), [Portal](portal.md), [Boundaries](packages.md).
