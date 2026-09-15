# BuildMax Current State

> **简体中文：** [阅读中文镜像](zh-CN/current-state.md)
>
> **Audience:** users, operators, and contributors · **Status:** current as of 2026-09-14

This assessment was checked against repository code at `938f85de`. It describes
implemented behavior, test coverage, and remaining limits. Priority and future
sequencing belong in the [roadmap](ROADMAP.md), not in a second priority list
here. Design records explain decisions; their unfinished checklists are not
proof that code is missing.

## Assessment And Evidence Scope

BuildMax remains Alpha. Local Agent execution and the private Space execution
path are implemented, including direct Task threads, persistent workspace
checkpoints, managed inference, and operator administration. This is not yet
proof of production multi-tenant readiness or of a qualified Beta candidate.
The [Beta readiness record](deploy/beta-readiness.md) remains unqualified.

The supported unattended-worker profile now disables stdio MCP: a resolved
stdio server fails a worker run during assembly, before its command runs and
before the first model call, while remote transports and local surfaces are
unaffected. TaskRun diagnostics present that treatment beside the recorded
boundary in Portal Run Details. R0 — the supported worker contract — is closed:
[trust-harness](design/trust-harness.md) §6.1 maps each control (Bash
confinement, worker API isolation, stdio MCP fail-closed, process limits, and
the hook boundary) to its evidence, with Bash and worker API isolation proven
through the deployed worker path. Immutable-candidate qualification through the
operator journey stays the separate Beta gate. Worker-wide network
egress is a documented, accepted limit for the first private Beta. The linear
Workflow reconciler now folds terminal facts and dispatches from durable state,
and a Server-owned recovery loop sweeps due runs at startup and on an interval,
so a lost terminal callback or a Server restart no longer strands a run. A step
may bind an earlier step's whole output into its input as labelled, untrusted
context, so a multi-step Workflow can pass one Agent's result to the next, and
the Portal step form authors those bindings directly rather than only through
advanced JSON. A step may also declare an `output_schema`: its run is constrained
to that schema, the validated value is persisted, and the step succeeds only on a
value that validates (see the shared runtime below). A definition now declares a
`"schema_version": 1`, and may declare an `input_schema` (validated against the
shared JSON Schema subset at publication) and a `result` selector (a source and
pointer into a step's output, detailed below); publication rejects an unknown
version, an out-of-subset input schema, or a result naming a missing step. Starting a run now
admits an immutable input validated against that `input_schema` and freezes it onto
the run, and the Portal generates the run's input form from the schema. Each per-step
record is now a `WorkflowNodeRun` (`node_id`, `node_index`, `node_type`) that persists
the full resolved input its node received and the complete output its accepted TaskRun
produced. A step input binding now selects a value with a `source` (`workflow.input`
or an earlier step's `node.<id>.output` envelope of text, structured output, and
Artifact references) and an RFC 6901 `pointer` into it, instead of injecting the whole
upstream output. A definition may declare a `result` selector (the same source/pointer
grammar naming a step's output); a succeeding run resolves it once and stores it as the
run's authoritative `result_json`, surfaced on the run and the issue it belongs to. The
definition now describes a graph of `nodes` joined by `needs` edges: a node becomes
ready when every node it needs has succeeded, so dependencies — not list position —
decide the order, and publication rejects a cyclic graph, a `needs` edge to a missing
node, or a binding that reads a node which is not a predecessor. Execution stays
fail-fast and dispatches one ready node at a time; concurrent dispatch of ready nodes
and typed `/structured/...` routing remain open.
Automatic re-dispatch of a worker TaskRun lost after it was claimed is a
documented, accepted first-Beta limit, distinct from that Workflow-progression
recovery. A Server can now expire old run traces on an operator-set retention
window and records each successful prune; keep-forever remains the default.
Deployment smoke now exercises graceful worker loss and the Server's readiness
degradation and recovery across runtime MySQL and object-storage outages.
Paired restore, schema upgrade and binary rollback, credential rotation, and
the worker's object-storage write path under denial remain open. Shared Redis
coordination is implemented, including distributed lease fencing at
message-history writes. The worker API already has a separate listener, TLS
support, and a shipped ingress NetworkPolicy; that bounded network slice must
not be confused with unrestricted worker egress.

This review inspected implementation, assembly, manifests, and test assertions.
It does not reuse old full-build results, coverage percentages, mutation-test
claims, or maturity percentages as evidence for this revision. The verification
performed for this update is recorded at the end. A test file's existence means
coverage is implemented, not that its deployment or database prerequisites were
exercised in this review.

## Shared Runtime And Local Surfaces

CLI/TUI, Desktop, and workers assemble the shared Agent runtime. The core has a
streamed model/tool loop, tool error recovery, parallel read-only tool execution,
permissions, approvals, compaction and checkpoints, hooks, bounded redacted
traces, usage statistics, sessions, notes, todos, Project Memory, subagents,
worktrees, and background jobs. Model assembly supports OpenAI-compatible chat,
OpenAI Responses, Anthropic, and Ollama.

Interactive Desktop turns now use `agentapp.RunScheduler`, which serializes one
run per session key, queues later prompts in order, and gives queued background
events the same lifecycle. The Server TaskRun scheduler remains a separate
durable execution-plane concern.

Local inspection includes `buildmax info`, TUI `/info`, and the Desktop `/info`
panel for one session; `buildmax usage` sums token and cost totals across
sessions, grouped by day, workspace, or model. Desktop is intentionally
read-only for memory: users edit the Markdown files directly, delete or clear
with `buildmax project forget`, and disable memory for one run with
`--no-project-memory`; separate Desktop edit/delete/enable controls are not
planned.

Local execution does not require a Server. Signed-in clients can use the managed
model catalog; a server-rejected credential is treated as an expired login, and
`buildmax logout` returns the client to local mode. See
[`internal/interface/auth/models.go`](../internal/interface/auth/models.go) and
its [tests](../internal/interface/auth/models_test.go).

The shared LLM request contract exposes a provider-neutral structured-output
schema: a `Request.Output` schema is validated against the shared subset in
[`internal/core/jsonschema`](../internal/core/jsonschema), and the result — the
validated value, its mode, whether the provider enforced it, or a typed failure —
comes back on `Completion.Structured`. Every provider maps it to its native
mechanism — OpenAI Chat and Responses to `response_format` `json_schema`,
Anthropic to a single forced tool, Ollama to `format` — and the Client validates
the candidate once, even for a native provider. A run requests it through
`RunPromptOpts.Output`/`RunLoopOpts.Output`: the loop runs free, and once the
model produces a no-tool-call answer the runtime re-issues one constrained call
to render that settled answer as the value, returned on `RunResult.Structured`.
A Workflow `agent_task` step consumes it: a step may declare an `output_schema`
(rejected at publication if outside the subset), the step's Task carries it so
the run is constrained, the validated value is persisted on the TaskRun and
folded onto the node run, and the step succeeds only when the run returned a
value that validated — otherwise the step fails. Still open: typed `/structured/...`
routing and planners (adaptive Workflow), the Portal step-form editor for
`output_schema`, and the deferred prompted fallback. Tool-argument JSON schemas
are a separate capability; see
[`internal/core/llm/llm.go`](../internal/core/llm/llm.go).

## Tasks, Results, And Workspace Continuity

Direct Agent execution creates a Task and TaskRun without requiring a
Conversation. Continue appends a run to that Task; Retry creates a new attempt
with explicit lineage. TaskRun is authoritative for the result. A Conversation
may create a Task but is not its authorization or storage parent. The previous
result-delivery queue and mandatory foreground summary attempt are removed.

**Direct Task streaming is implemented.** The worker appends deltas by Task ID,
the Task SSE handler subscribes to that ID, and Portal's Task page reads the
stream while polling durable run state. This path does not depend on a
Conversation. The page also opens a stored run trace. Sources:

- [`internal/server/handlers/worker/worker.go`](../internal/server/handlers/worker/worker.go)
- [`internal/server/handlers/work/stream.go`](../internal/server/handlers/work/stream.go)
- [`portal/src/pages/tasks/TaskDetail.tsx`](../portal/src/pages/tasks/TaskDetail.tsx)

Stream behavior depends on the coordination mode: local mode buffers in memory;
Redis mode shares bounded streams across replicas with expiration. Neither is
an indefinite replay log. The [Portal Task-thread test](../portal/e2e/task-thread.spec.ts)
covers direct execution, Continue, and Retry through the UI. The
[two-replica streaming test](../internal/server/handlers/work/stream_multireplica_test.go)
uses miniredis to exercise cross-replica deltas and buffered output. These tests
do not prove every streaming, trace, or managed-usage failure scenario.

Task workspaces persist as immutable object-store checkpoints. The first run
seeds a base; Continue uses the Task's workspace head, while Retry uses the
repeated run's base. Successful result checkpoints advance the head; partial
checkpoints preserve failed or canceled work without advancing it. The worker
records restoration status, and terminal reporting finalizes available
checkpoints. Checkpoint finalization failure does not rewrite the run outcome.

Implementation and tests span
[`internal/agentapp/taskrun/checkpoint.go`](../internal/agentapp/taskrun/checkpoint.go),
[`internal/service/workspace/checkpoint.go`](../internal/service/workspace/checkpoint.go),
[`internal/infra/db/workspace_checkpoint.go`](../internal/infra/db/workspace_checkpoint.go),
and the worker checkpoint handlers. Worker Jobs have ephemeral-storage limits;
orphan and retention sweeps reclaim unreferenced payloads. Portal displays
checkpoint and restoration state read-only. These mechanisms do not establish
paired database/bucket restore or upgrade-rollback qualification.

## Worker Execution And Network Boundaries

### Bash Sandbox And Child Processes

[`config.WorkerSandboxSurface`](../internal/config/sandbox.go) selects the strict
worker baseline when `BUILDMAX_SANDBOX_BACKEND_INSTALLED` is present. Official
images install `bubblewrap` and `socat` and set this marker. This includes Compose
workers launched as local processes inside the official image. An unmarked bare
host inherits the CLI baseline unless configured otherwise; the code does not
make every possible worker launch fail closed by default.

The selected sandbox resolves settings, policy, run overrides, and Agent tiers.
Worker handlers resolve and pin the effective Agent/Space tiers for audit.
The backend self-test refuses unavailable enforcement when fail-closed policy
is selected. Resource controls prefix wrapped commands with shell limits;
the memory limit is not enforced on macOS. Command hooks use the Bash wrapper
and scrubbed environment; HTTP hooks consult the allowed-host policy.

The Kubernetes worker security context is **root with `SYS_ADMIN` added**, with
a read-only root filesystem and a supplied Localhost seccomp profile. The Linux
Bash wrapper rebinds the container's `/proc` read-only. This is not a non-root
pod or whole-worker isolation equivalent to the command sandbox. Sources:
[`internal/infra/k8s/job.go`](../internal/infra/k8s/job.go),
[`internal/infra/sandbox/bwrap_linux.go`](../internal/infra/sandbox/bwrap_linux.go),
and [seccomp deployment instructions](../deployment/seccomp/README.md).

Deployment smoke contains an actual worker Bash probe that checks successful
execution and denial of an out-of-workspace write
([`tools/mk/deploy_smoke.go`](../tools/mk/deploy_smoke.go)). This is implemented
end-to-end coverage, not a claim that this review ran the cluster smoke.

Remaining limits:

- MCP stdio servers launch with `exec.Command` and do not pass through the Bash
  sandbox ([`internal/infra/mcp/transport.go`](../internal/infra/mcp/transport.go)).
  The unattended-worker profile refuses them fail-closed before any child or
  model call ([`internal/agentapp/mcp_manager.go`](../internal/agentapp/mcp_manager.go));
  local surfaces still run stdio inside that same unsandboxed boundary.
- `local_process` remains in the Server's host trust domain even when its Bash
  commands are sandboxed.
- `buildmax sandbox overrides` is not implemented. Portal exposes Agent tiers
  and Space defaults. Run Details shows the boundary recorded by the trace, the
  resolved plugin pins, and the worker MCP treatment (stdio disabled by the
  profile, plus the resolved remote transports), but not the requested/resolved
  tier pair as a distinct diagnostic field.
- No worker RuntimeClass selection is wired in the Job builder. gVisor remains
  conditional post-Beta hardening, not a shipped supported worker profile or a
  first-Beta requirement.

### Worker API Boundary

**Implemented:** public and worker routes use separate muxes and listeners.
Worker routes are absent from the public listener, independently of a caller's
token. Server bootstrap builds the worker listener's TLS configuration; optional
client-CA configuration enables native mTLS. Workers can use a configured CA and
client identity. Per-run authentication remains required.

The basic and production Kubernetes manifests include a worker API Service and
a Server-ingress NetworkPolicy admitting the worker port only from matching
worker pods in the namespace. The public API port remains open to cluster
traffic under that policy. Enforcement requires a CNI that implements
NetworkPolicy; manifest presence alone is not proof of enforcement.

Sources and coverage:
[`internal/server/server.go`](../internal/server/server.go),
[listener boundary tests](../internal/server/listener_boundary_test.go),
[`internal/bootstrap/worker_tls.go`](../internal/bootstrap/worker_tls.go), and
[production manifest](../deployment/production/buildmax.yaml).
The served [public OpenAPI document](../internal/server/static/openapi.json)
contains only public-listener routes; the Worker control plane has a separate
[OpenAPI document](../internal/server/static/openapi-worker.json). Architecture
tests compare each document to the routes registered on its listener.

**Accepted first-Beta limit:** a worker egress NetworkPolicy is absent. The Server-ingress policy does
not constrain all outbound traffic from a worker, sandbox MCP processes, or hide
the storage credentials used by the worker. TLS support also does not mean every
local development configuration requires TLS.

## Server Topology And Persistence

### Shared Coordination Is Implemented

`coordination.mode: local` remains the single-instance default. Redis mode wires
shared Task streams, connection-event fan-out, and renewable Conversation turn
leases through [Server adapters](../internal/server/coordination/coordination.go)
and [Redis primitives](../internal/infra/coordination). Bootstrap rejects an
unreachable configured Redis rather than silently falling back to local mode.

Both the basic/kind and production manifests now configure Redis and two Server
replicas. Architecture tests reject multiple replicas without coordination.
Multi-replica streaming and lease behavior have automated tests, and a deployed
kind probe ([`kindCoordinationProbe`](../tools/mk/coordination_probe.go), run by
`./make kind smoke`) now drives the two replicas by hand against real Redis to
prove cross-replica stream delivery, cross-replica turn-lease serialization, and
recovery after a Redis restart on the candidate topology. The lease exposes a
fencing token, and message-history writes enforce it:
a write carrying a token below the one the conversation has accepted is rejected,
so a stale writer after lease loss cannot append behind the new holder. Lease
renewal itself discards Redis errors and does not cancel the running turn when
ownership is lost, so such a turn runs to a rejected write rather than being
stopped early. See the [coordination design](design/server-coordination.md).

The scheduler has one concurrent dispatch slot per instance. In local-process
mode that slot remains occupied during execution. Kubernetes dispatch returns
after creating a Job, so the same setting does **not** limit the cluster to one
running worker. See
[`internal/server/scheduler/scheduler.go`](../internal/server/scheduler/scheduler.go)
and [`internal/infra/k8s/job.go`](../internal/infra/k8s/job.go).

### Database Coverage And Migrations

`./make test mysql` requires a DSN, creates and drops an isolated database, and
rejects missing-DSN skips. CI supplies a pinned `mysql:8.0` service. A default test
run without a DSN still skips database-dependent tests.

The database coverage is broader than the previous assessment reported:

| Behavior covered by database tests | Evidence |
|---|---|
| Retry lineage and original attempt preservation | [task_run_retry_test.go](../internal/infra/db/task_run_retry_test.go) |
| Continue versus Retry workspace base selection | [task_run_base_test.go](../internal/infra/db/task_run_base_test.go) |
| Direct Tasks, continuation, and idempotency keys | [task_direct_test.go](../internal/infra/db/task_direct_test.go) |
| Task claiming, run transitions, one active run, and cancellation/report races | [concurrency_test.go](../internal/infra/db/concurrency_test.go) |
| Artifact soft deletion, concurrent deletion, expiry, byte accounting, and purge lifecycle | [artifact_retention_test.go](../internal/infra/db/artifact_retention_test.go) |
| Checkpoint head advancement and partial checkpoint retention | [workspace_checkpoint_test.go](../internal/infra/db/workspace_checkpoint_test.go) |
| Workflow guarded run/step transitions and atomic failure finalization | [workflow_test.go](../internal/infra/db/workflow_test.go) |
| Idempotent Workflow Task admission, replay, payload conflict, space scope, and its contention winner | [task_admission_test.go](../internal/infra/db/task_admission_test.go) |
| Workflow due-run discovery and reconciliation lease claim/renew/release under contention | [workflow_reconciliation_test.go](../internal/infra/db/workflow_reconciliation_test.go) |
| Linear reconciler folding terminal TaskRun facts: step advance, final success, failure/cancel distinction and later-step blocking, lost-callback recovery, and one outcome under concurrent reconciliation | [reconcile_mysql_test.go](../internal/service/workflow/reconcile_mysql_test.go), [service_test.go](../internal/service/workflow/service_test.go) |
| Server-owned Workflow recovery loop: startup sweep, per-run reconcile, tolerance of a due-scan error, Start/Stop lifecycle, and end-to-end restart recovery of a run stranded by a lost callback | [workflow_recovery_test.go](../internal/server/scheduler/workflow_recovery_test.go), [workflow_restart_recovery_mysql_test.go](../internal/server/scheduler/workflow_restart_recovery_mysql_test.go) |
| Step output binding: publication validation (earlier step, unique names), the run's binding snapshot round-tripping the store, and a bound downstream step dispatched with the upstream step's full output as labelled untrusted input | [binding_test.go](../internal/service/workflow/binding_test.go), [workflow_test.go](../internal/infra/db/workflow_test.go) |
| Workflow revision advancement under edits and contention (guarded compare-and-set) | [workflow_test.go](../internal/infra/db/workflow_test.go) |
| Workflow initial revision and revision queries | [revision_query_test.go](../internal/infra/db/revision_query_test.go) |
| Space isolation for secrets and independent invitations | [secret_test.go](../internal/infra/db/secret_test.go), [space_invitation_test.go](../internal/infra/db/space_invitation_test.go) |
| Cross-Space rejection for Workflow and Issue updates and plugin activation reads/writes | [cross_space_test.go](../internal/infra/db/cross_space_test.go) |
| Quota usage-window boundaries, title-token accounting, null usage, and Space isolation | [quota_usage_test.go](../internal/infra/db/quota_usage_test.go) |
| Durable auth-session activity, expiry, revocation, refresh-token cascade, touch throttling, listing, and counts | [auth_session_test.go](../internal/infra/db/auth_session_test.go) |
| External-identity lookup and uniqueness, atomic JIT account/Space/link creation, concurrent first login, disable-before-unlink, and transactional audit | [external_identity_test.go](../internal/infra/db/external_identity_test.go) |
| Task output-schema and validated TaskRun structured-value persistence | [task_run_structured_test.go](../internal/infra/db/task_run_structured_test.go) |
| Expired run-trace discovery and idempotent trace-pointer clearing | [task_run_trace_retention_test.go](../internal/infra/db/task_run_trace_retention_test.go) |

The guarded transitions prevent illegal terminal rewrites and make failed-step,
later-step blocking, and failed-run finalization atomic. Workflow step dispatch
now admits its Task idempotently through `AdmitTask`, keyed
`workflow/<workflow_run_id>/node/<node_id>` and unique within the space, so a
retried or concurrent dispatch — the crash window between admitting the Task and
linking it onto the node run — resolves to the one Task instead of duplicating
the agent's execution. The store also discovers due non-terminal runs and hands
out a bounded, takeover-safe reconciliation lease, so the durable state a
reconciler needs exists. A concurrent Workflow edit now loses a compare-and-set
and receives a conflict rather than overwriting a newer definition or leaking a
duplicate-key error. The linear reconciler now exists:
[`Service.Reconcile`](../internal/service/workflow/service.go) claims that lease,
reads the run and its steps, folds a running step's terminal TaskRun — read from
the TaskRun store, not a pushed callback — into the guarded step and run
transitions, dispatches the next pending step by re-admitting its Task under the
stable key (recovering the crash window between admitting a Task and linking it),
and sets the run's next reconcile time while work remains.
`StartWorkflowRun` dispatches its first step through the same `Reconcile`, and
`HandleTaskRunTerminal` is now only a wake-up that maps the finished TaskRun to
its run and reconciles, so a lost callback loses a wake-up rather than the
outcome and a later `Reconcile` recovers it from persisted state alone. A
Server-owned recovery loop
([`WorkflowRecoveryLoop`](../internal/server/scheduler/workflow_recovery.go))
now drives that recovery without a callback: it sweeps the store's due-run query
once at startup and then on a fixed interval, reconciling each due run, and is
stopped with the other background loops on graceful shutdown. Every replica runs
it; the reconciliation lease, not process-local election, keeps two from
advancing one run. Automatic re-dispatch of a worker TaskRun lost after it was
claimed is an accepted first-Beta limit, distinct from this progression
recovery. The explicit cross-Space store tests cover Workflow and Issue updates
and plugin activations, not every store method. External dependency recovery
still needs scenario-specific evidence. The removed result-delivery queue has
no remaining restart-recovery obligation of its own.

**The explicit migration list is no longer empty.**
[`internal/infra/db/migration.go`](../internal/infra/db/migration.go) contains
`system_grant_live_marker` and `llm_model_credential_encryption`.
The latter drops the old plaintext credential column without migrating its
values; affected models must be re-added. The migration test covers ledger
recording and skipping on a second run. Neither that test nor an N-1 policy in
a design document establishes an exercised old-schema upgrade and binary
rollback. The old explanation that a fixture is blocked by an empty migration
history is obsolete.

Each trace is bounded by field and record caps. When an operator sets
`trace.retention_days` above zero, a Server-owned hourly sweep deletes traces
for runs that ended before the cutoff, clears their TaskRun pointers, and
records a `traces.pruned` audit event. Zero remains the keep-forever default, so
an operator who leaves it unchanged still owns capacity planning. Deletion and
pointer clearing are retryable, but this mechanism is not evidence that a
candidate's chosen retention and capacity policy has been exercised.

## Account, Space, And Extension Surfaces

Account creation, single-use login codes, password sign-in, system administrator
grants, Space invitations to existing accounts, role changes, ownership
transfer, and member-scoped recovery are implemented. Signup defaults off;
creating an account does not itself issue a credential. Each login opens a
durable session (`auth_session`) that the request guard checks every call, so
logout, administrator revocation, and disablement stop an already-issued access
token on its next request; sessions also carry an absolute lifetime. Portal
keeps the renewable refresh credential in a Secure,
HttpOnly, SameSite=Strict cookie and holds the short-lived access token only in
memory; CLI and Desktop retain the JSON credential flow. Corporate sign-in over
OpenID Connect is implemented: a
deployment configures an `oidc` block (Okta is the first supported provider), and
a verified sign-in links to an account by `(issuer, subject)` — reusing an
existing link, linking an operator-created account by verified email, or creating
one just in time within `allowed_email_domains`. Native password and login-code
sign-in are gated independently by `local_login` (`all`, `system_admins`, `off`).
Portal discovers the enabled methods and completes the authorization-code flow
through Server-owned state, nonce, PKCE, callback validation, and a server-side
token exchange; provider tokens are not retained. The association and protocol
branches have service, handler, provider-fake, and real-MySQL coverage. The
pinned real-Okta end-to-end and key/secret-rotation drills are not yet done.
See the [identity service](../internal/service/identity/account.go), the
[OIDC provider](../internal/infra/oidc/provider.go), and the
[Space service](../internal/service/space/service.go).

`buildmax admin` provides authenticated administrator, account, and model-catalog
operations. `buildmax-server` retains database-direct bootstrap and recovery
commands. Model credentials are encrypted under the deployment key-encryption
key; credentialed model creation refuses to store a key without encryption.
Space secrets and Agent secret-consumption declarations also have storage and
worker delivery implementations, with run-scoped authorization. Their presence
does not isolate delivered secrets from the worker process that consumes them.

System administration, quota, audit, role checks, and Space lifecycle UI exist.
Remaining administration gaps include transactional authority audit, admin CLI
Session listing/revocation parity, quota-tier assignment, and runtime metadata
for queue/worker diagnosis. These are tracked in the
[administration operations proposal](proposals/system-administration-operations.md);
proposal status must not be confused with an implemented feature. Plugin
publication remains CLI-only, while Portal can inspect, retire, restore, and
yank catalog releases.

Space approval workflows remain unimplemented and deliberately out of scope;
that is not evidence of an unfinished invitation or ownership-transfer feature.

Workflow definitions are a graph of `agent_task` nodes joined by `needs` edges,
with versioned definitions and durable run/node records. A node can constrain its
result with `output_schema`, and a pointer binding can pass a selected value from
the run input or a predecessor node's output into a node's input. The definition
contract still has no typed conditional routing, concurrent dispatch of ready
nodes, manual approval, or loops
([`internal/core/workflow/workflow.go`](../internal/core/workflow/workflow.go)).

Portal and inbound webhook execution are assembled. Telegram remains channel
vocabulary, and the webhook callback sender is not assembled into the Server.
Recurring schedules run an Agent on the Task plane through a `schedule` trigger
source and the `/api/spaces/{space_id}/schedules` API, dispatched by a resident
loop that claims each due time once across replicas, coalesces missed firings
into one catch-up, and pauses a schedule after five consecutive failed firings
or when its creator is disabled. Portal creates and manages schedules on the
Agent detail page and lists every schedule in a Space on a Schedules page; the
pause reason is logged, not shown. They are not a conversation channel
([`internal/core/schedule`](../internal/core/schedule/schedule.go),
[`internal/server/scheduler`](../internal/server/scheduler),
[design](design/scheduled-agent-execution.md)). Space plugin activation supports skill/subagent content but rejects
releases containing hooks or MCP servers
([activation service](../internal/service/plugin/activation.go)). Foreground
Conversations do not load Space plugins.

## Qualification And Operating Evidence

The evaluation contract, built-binary local and worker adapters, graders,
repeated and paired experiments, and pinned Harbor adapter are implemented.
[`evaluation/suite`](../evaluation/suite) contains three product-owned tasks.
That scope cannot qualify all supported surfaces. Historical oracle/canary
reports are not a benchmark result for this revision; this review ran neither
real-model evaluation nor a Terminal-Bench protocol and reports no score.

Portal browser tests now cover direct Task threads, workspaces, canonical Space
routes, loading/error/permission states, responsive layouts, accessibility, and
run provenance. Desktop has bridge and browser-based UI suites under
[`desktop/frontend/e2e`](../desktop/frontend/e2e), plus a packaged-application
launch smoke on macOS and Windows CI. The launch smoke proves that the built
bundle starts and stays alive briefly; it does not drive or visually inspect the
native window. Portal routes remain eagerly imported, with no route-level lazy
loading in the current source. No fresh bundle size or throughput number was
measured in this review.

Deployment smoke includes retry, managed inference and its call ledger,
cancellation of a running worker, worker-loss recovery (a run whose worker is
deleted mid-execution settles to a diagnosable terminal FAILED and stays
retrievable), database-outage degradation and recovery (a runtime loss of MySQL
flips /readyz to report the database failed and pulls the server out of the
Service without restarting it, and it recovers on its own once access returns),
object-storage degradation and recovery (the same for a runtime loss of the
bucket — /readyz's object-storage check fails and recovers with the bucket
intact), and the Bash confinement probe. Scheduler unit tests cover stale-run
handling and cleanup, including the liveness sweep that settles a run whose
worker went silent — the hard-loss path the deployment cannot reproduce, since
the kernel drops an in-container SIGKILL to PID 1 and any kubelet deletion starts
with the SIGTERM the worker reports on. These are not equivalent to candidate
exercises for the worker's object-storage write path under denial, paired
restore, credential rotation, and schema rollback.

Compose, kind, production Kubernetes manifests, release verification, SBOM,
image scanning, and provenance workflows exist. Their presence does not fill
the unsigned [Beta readiness record](deploy/beta-readiness.md).

## Verification For This Review

This is a source-and-tests reassessment, not a fresh deployment qualification.
For this documentation update, `./make test`, `./make check docs`, and
`git diff --check` passed locally at `938f85de`. The ordinary test scope includes
the architecture, runtime, provider, identity, handler, scheduler, CLI, and
Desktop bridge suites. Documentation checks cover links and formatting; neither
scope proves a deployed candidate.

The review did not run the real-MySQL scope (no `BUILDMAX_TEST_DSN` was supplied),
full builds, frontend/browser suites, Compose/kind deployment smoke, external
recovery drills, or paid model evaluation. Database test assertions above were
read, not claimed as executed against MySQL. Hosted CI state, historical
coverage, and earlier deployment results have not been carried forward as
current measurements.
