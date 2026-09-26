# Local End-to-End Verification

> **简体中文：** [阅读中文镜像](../zh-CN/design/端到端测试.md)

## Contents

- [Status](#status)
- [1. Decision](#1-decision)
- [2. Why](#2-why)
- [3. Current Baseline](#3-current-baseline)
- [4. Prerequisite: The Deterministic Model Harness](#4-prerequisite-the-deterministic-model-harness)
- [5. Local Harness Contract](#5-local-harness-contract)
- [6. Golden Paths](#6-golden-paths)
- [7. AI Agent Workflow](#7-ai-agent-workflow)
- [8. CI Policy](#8-ci-policy)
- [9. Delivery Order](#9-delivery-order)
- [10. Success Criteria](#10-success-criteria)

## Status

- roadmap_priority: `R2` — the deployed lifecycle probes supply evidence for
  the recovery priority in [../ROADMAP.md](../ROADMAP.md); the completed local
  productivity work does not form a separate roadmap item
- status: `in progress` — §9 steps 1, 2, 3, 5, and 6 are done: the model harness of
  §4 is `internal/testsupport/mockllm` and serves the deployment smokes too; the
  CLI suite covers print mode, answers an approval on a pseudo-terminal, and
  resumes a session both by id and by `-c`, including the refusal a second
  process gets when one is already open, proves a batched turn produces one
  canonical history at concurrency limits 1 and 8, and reports a refused
  credential with an exit code, a message naming the fix, and no retry; the
  deployment workflow is post-merge,
  scheduled, and dispatchable and reports what verified a commit; the suites
  have names, preflight, artifacts, and an owning local mode; and the runbook is
  [../contribute/testing.md](../contribute/testing.md). Step 4's browser half is
  done — agent execution and its trace, workflows and their execution, a
  conversation turn over the deployment's WebSocket, files, space settings, and
  the role matrix — and its deployment half landed retry and the space boundary
  in the smoke. Step 5 landed the Desktop bridge, including rewind and fork,
  and CI now packages and launch-smokes the desktop app on macOS and Windows.
  The smoke proves that the packaged process starts and stays alive briefly; it
  does not drive or visually inspect the native window. Cancellation (§6.2),
  graceful worker-loss recovery (§6.3), database degradation/recovery (§6.4),
  object-storage readiness degradation/recovery (§6.5), and worker
  object-storage write denial (§6.6) have landed since.
  Open in this record: the partial-output survival half of cancellation that
  §6.2 leaves for later. Broader candidate-only lifecycle gaps stay in the
  [verification program](verification-program.md) and Beta readiness record
- depends on: [tool-permissions.md](./tool-permissions.md), whose approval gate
  the CLI and Desktop paths exist to drive, and which decides what a surface
  with no human attached does with an `Ask`;
  [parallel-tool-execution.md](./parallel-tool-execution.md), whose promise that
  concurrency changes nothing observable is itself a golden path, and whose
  batching the model harness must be able to script — see §4 and §6
- relates: [llm-provider-adapters.md](./llm-provider-adapters.md) — the three
  wire protocols the harness has to serve
- touches: `internal/testsupport`, `deployment/smoke/mock-llm`, `tools/mk`,
  `portal/e2e`, `internal/interface/cli`, `.github/workflows`
- created_at: `2026-08-21`

## 1. Decision

End-to-end (E2E) verification is a local development and AI-agent feedback
loop. It is not a required pull-request gate.

Pull requests keep their fast, deterministic checks: build, lint, unit tests,
integration tests, and scoped frontend tests. A contributor or agent runs the
smallest relevant E2E suite after changing a user flow, then uses its artifacts
to diagnose a failure. Comprehensive deployment verification runs after merge,
on a schedule, or when explicitly dispatched; it provides release confidence
without making every iteration wait for containers, browsers, or a cluster.

"Comprehensive" means every important user outcome has at least one
deterministic end-to-end path. It does not mean rebuilding the unit-test suite
through the UI. The latter is slower, less diagnosable, and makes the feedback
loop worse.

## 2. Why

BuildMax has four execution surfaces with boundaries that ordinary tests cannot
prove together:

| Surface | Boundary an E2E test must prove |
|---|---|
| Portal | The published browser bundle, runtime API configuration, authentication, the socket a conversation turn rides in both directions, and the visible space workflow work against a real deployment. |
| Server and worker | The deployed server, storage, scheduler, worker, artifact path, and managed-model transport cooperate. |
| CLI and TUI | The released binary, terminal interaction, the approval gate, the local workspace, session persistence, policy, and trace behavior cooperate. |
| Desktop | The React UI, Wails bindings and events, local runtime, approvals, and persistent project/session state cooperate. |

The normal test pyramid remains the primary regression mechanism. A pure
display decision belongs in a frontend unit test, a service contract in a Go
test, and a handler rule in an HTTP/integration test. E2E tests are reserved
for a boundary that would otherwise remain unproved.

## 3. Current Baseline

The repository already has useful deployment evidence, and one gap that is
larger than it looks.

- `./make compose smoke` and `./make kind up` exercise a real deployment with
  a deterministic mock model. They cover login, a personal space, file storage,
  a conversation task, worker completion, and artifact retrieval. Managed-mode
  smoke additionally proves that a worker reaches the model through the
  gateway.
- `./make e2e <cli|desktop|local|compose|kind|all>` selects a suite. The Portal ones run
  serial Chromium Playwright checks in `portal/e2e/`: login, session
  restoration, the Portal shell, runtime API configuration, audit and admin
  routes, the run-trace view, space files, workflows, and what an ungranted
  account is shown. `kind` and `compose` attach to a deployment that is already
  running, `local` owns a Compose stack for one run, and `cli` needs no
  deployment at all.
- `./make agent-smoke` is **not** deterministic verification. It builds the CLI
  and runs `buildmax -p "/smoke 0"`, which drives the `.buildmax/skills/smoke`
  skill: a real model executes the tool checks and reports its own PASS/FAIL
  table. It therefore needs a provider API key, produces a different transcript
  every run, judges itself, and returns an exit code that reflects only whether
  the process finished. It is a useful manual agent exercise, and it is not the
  CLI suite: that is `internal/e2e/cli`, which drives the built binary against a
  scripted model. It was called `./make smoke` until the name collided with two
  deterministic things, and it now says what it needs before it starts.
- `./make eval` runs the built CLI against the CLI tasks in `evaluation/suite/`.
  Worker tasks are opt-in with `--surface worker`, and `--surface all` runs both.
  This is behavioral evaluation of agent quality, not boundary verification,
  and it is deliberately outside the suites below — but it runs the same binary
  in a temporary workspace, so the CLI suite and the eval runner must share
  their process-launch and isolation helpers rather than grow two incompatible
  ones.
- Desktop has Go bridge tests and frontend unit tests, but no UI E2E suite.
- `gui` has neither tests nor an ESLint step in CI; the Portal browser suite is
  today the only thing that executes the shared component package at all. Since
  Portal and Desktop both consume it, moving E2E out of PR gating removes that
  package's only pull-request-time coverage. See §8.

Two recent designs changed what these suites have to cover and how they can be
driven:

- **The approval gate is real.** [tool-permissions.md](./tool-permissions.md)
  filled the empty permission layer, so a write tool now asks before it runs.
  `tools.permissions` in `settings.yaml` pins any tool to allow, ask, or deny,
  which is what lets a suite reach an approval prompt deterministically instead
  of hoping the model produces one. It also constrains how: a non-interactive
  surface has no approval handler, so an `Ask` collapses to `Deny` there. Print
  mode is one of those surfaces — see §6.
- **A turn carries several tool calls.**
  [parallel-tool-execution.md](./parallel-tool-execution.md) runs adjacent
  read-only calls concurrently and promises that nothing observable changes:
  the same message history, hook sequence, and approval prompts, in the same
  order, whatever the scheduler does. That promise is exactly the kind of claim
  a unit test cannot hold, and it makes batched replies a requirement of the
  model harness rather than a nicety.

The Portal architecture document is the source of truth for the existing
browser suite and its deliberate division from the API deployment smoke; see
[contribute/architecture/portal.md](../contribute/architecture/portal.md).

The current deployment-smoke workflow runs Compose and kind from relevant pull
requests. This design changes the intended policy: comprehensive deployment
and browser E2E must move out of required PR gating. They remain valuable
post-merge, scheduled, and manually dispatched evidence.

## 4. Prerequisite: The Deterministic Model Harness

Every suite below depends on a mock model, and the existing one cannot carry
them. `deployment/smoke/mock-llm` answers a single wire protocol with one
hard-coded sentence and holds no state. That is sufficient for "a task
completed"; it cannot express a tool call, a batch of them, a second turn, a
refusal, or a failure. The golden paths that matter most — approving and
denying a tool call, verifying an intended file change, resuming a session,
handling an approval in Desktop — are all multi-turn tool-calling paths.
Treating the mock as a solved primitive would leave the hardest design problem
undesigned inside a one-line contract row, so it is the first delivery rather
than an unexamined detail of a later one.

The harness must satisfy the following.

| Concern | Required behavior |
|---|---|
| Scripted turns | A scenario fixture declares the ordered replies for one run: assistant text, tool calls with exact arguments, a streaming shape, a provider error, or a token/usage payload. The mock replays them in order and never infers a reply from request content. |
| A turn is a batch | One scenario entry can carry several tool calls, because the runtime now schedules them concurrently. A format that assumes one call per turn cannot express the case §6 exists to protect, and retrofitting it would invalidate every committed scenario. |
| Exhaustion is a failure | A scenario that ends with unconsumed entries fails the suite. This is what catches the agent that stopped calling the model one turn early — a bug no assertion on the final output can see. |
| Every wire protocol | The mock serves all three protocols in `internal/infra/llm` — Anthropic Messages, OpenAI Chat Completions, and OpenAI Responses — selected by route, and each suite declares which one it runs. A single-protocol mock silently exempts two of the three adapters described in [llm-provider-adapters.md](./llm-provider-adapters.md), which is precisely the code most likely to break on a provider change. |
| No container requirement | It runs as a plain Go process for the CLI, Desktop, and Go-level suites, and as the existing image for Compose and kind. One implementation, two packagings; the deployment smoke keeps working unchanged, including the reply it asserts today. |
| Fixtures live with their suite | Scenarios are committed next to the suite that uses them and are readable without running anything, so a failure can be diagnosed by reading the script against the retained transcript. |

The harness is test support, so it belongs in `internal/testsupport`, which
production code may not import and a test in `internal/architecture` enforces.
That rule scans `internal/`, so the thin `main` under `deployment/smoke/` that
packages the harness as a container is outside it and may import the package.
This is deliberate rather than an oversight, and a change that moves either
half should say so.

The harness landed as `internal/testsupport/mockllm`. It serves all three
protocols both blocking and streaming, and a step describes one reply whichever
way it is fetched — a suite that switches between them does not script it
twice. Its tests drive the real adapters in `internal/infra/llm`, because a
mock checked against a hand-written parser only proves it agrees with itself.

`deployment/smoke/mock-llm` is now a packaging of the same code rather than a
second implementation, so a reply shape is only right or wrong in one place,
and a deployment smoke can replay a scenario by mounting one. Its default
scenario repeats its single step: a deployment smoke asserts on what a run
produced, not on how many model calls producing it took. Repeating is opt-in
for exactly that reason — everywhere else, the call past the end of the script
is the finding.

## 5. Local Harness Contract

Every local E2E suite must meet these conditions.

| Concern | Required behavior |
|---|---|
| Determinism | Use the model harness above and its committed scenarios. No provider API key, paid model, personal account, or external SaaS is required. A suite that cannot meet this is not a suite; it is a manual exercise and must be named as one. |
| Isolation | Create a temporary `BUILDMAX_HOME`, workspace, and uniquely named test resources for each run. Never use a contributor's real home, sessions, credentials, or workspace. |
| Policy is written, not hoped for | A suite that depends on an approval pins the tool with `tools.permissions` in its temporary `settings.yaml`. Reaching a prompt by guessing what the model will call, or what a builtin currently defaults to, makes the suite fail the day a default changes for a good reason. |
| Test data | Fixture helpers create the minimum account, role, space, project, and content through public boundaries where practical. They clean up what they create, or report an exact safe cleanup target when a deployment must retain evidence. |
| Lifecycle | A suite can either attach to a named running deployment for diagnosis or own its disposable local deployment lifecycle. The command must say which mode it chose. |
| Diagnostics | On failure retain Playwright traces/screenshots, command output, redacted server/worker logs, the model scenario and the transcript it produced, and a short reproduction command under one predictable artifact directory. |
| Portability | Add task-runner commands under `tools/mk`; do not create an OS-specific shell-script testing path. Platform-specific native Desktop smoke is an explicit exception and reports its unsupported platforms. |
| Time | Each suite declares a normal duration and a timeout, and holds to the budget in §5.1. A failed preflight names the missing dependency or service rather than timing out later in a browser assertion. |
| Naming | A command name states what it is. `smoke` meant three different things — a model-driven skill run, a deterministic deployment check, and a future CLI suite — so the model-driven one is now `./make agent-smoke` and announces its API-key requirement in preflight. |

### 5.1 Duration Budget

A budget is part of the design, not an outcome to be measured afterwards. A
suite that misses its budget is a defect in the suite: the loop it was built to
serve stops being run.

| Suite | Normal | Timeout | Why this number |
|---|---|---|---|
| CLI + TUI | under 60 s | 3 min | It must be cheap enough to join the default `./make test` loop. A five-minute CLI suite would be correct and unused. The budget covers the pseudo-terminal paths §6 requires, so a terminal driver that costs seconds per keystroke is out of scope by arithmetic. |
| Desktop bridge | under 5 min | 10 min | One Wails development process plus a handful of streamed interactions. |
| Portal + Compose | under 10 min | 20 min | Includes bringing the stack up; this is the daily deployment default. |
| Portal + kind | under 20 min | 35 min | Cluster creation and image loading dominate; run it for deployment, ingress, storage, or worker changes. |

### 5.2 Fixture Isolation Versus The Attached Deployment

Unique-per-run resources and the existing attach-to-deployment mode pull in
opposite directions, and the existing behavior was deliberate: the deployment
smoke and `./make e2e` share the fixed account `deployment-smoke@buildmax.local`,
tolerate "already exists" and "already holds" as success, and pass in a
single-use login code out of band because a browser cannot fetch one. Playwright
then runs one worker over one shared session for the same reason.

The two modes therefore take different rules rather than one compromise:

- **Owned lifecycle.** The suite created the deployment, so every account,
  space, and resource is unique per run and nothing needs cleaning up — the
  deployment is discarded whole.
- **Attached.** The suite is a guest. It keeps the existing fixed diagnostic
  account, creates only uniquely named resources beneath it, cleans those up,
  and never assumes it is the only client. Its command output states which
  deployment it attached to and what it left behind.

## 6. Golden Paths

The first paths are deliberately few and representative. New functionality
adds an E2E path only when its cross-boundary outcome has no lower, faster
test. The lowest boundary that can prove a claim owns it:

| Claim | Authoritative evidence boundary |
|---|---|
| Pure validation, state transition, authorization rule, or rendering decision | Unit, component, handler, or real-MySQL test |
| Published Portal bundle, browser routing, session restoration, accessibility, or visible recovery state | Portal Playwright against a real deployment |
| Server, worker, storage, scheduler, gateway, or process lifecycle cooperation | Compose deployment smoke or failure suite |
| Ingress, Kubernetes Job lifecycle, pod network or security context, Service removal, or cross-replica behavior | kind lifecycle suite |
| External dependency restore, upgrade, rollback, TLS, or credential rotation | Pinned release-candidate qualification |

A new browser case is justified only when removing the browser would leave the
claim unproved; a browser case never repeats a fault injection that deployment
smoke already owns.

| Suite | Initial golden paths |
|---|---|
| Portal + Compose | Sign in; create and use space resources; start a run; read its output, trace, and artifact; verify the important role-specific views. Compose is the daily default because it is the fastest real deployment. |
| Portal + kind | Prove the same published-bundle contract through ingress and the Kubernetes worker path. Run locally for deployment, ingress, storage, or worker changes. |
| Server + worker | Preserve the existing direct and managed deployment smoke, then add the deployment-level half of cancellation, retry, authorization denial, and failure recovery — see §6.1. |
| CLI + TUI | Start the built binary in an isolated workspace with the mock model; send a prompt; approve and deny a pinned tool call; verify an intended file change, session resume, trace, and a useful failure. Prove that a batched turn produces one canonical history and prompt order. |
| Desktop bridge | Drive the bound methods with the scripted model: create a project; stream a response; handle an approval and a denial; reopen a session; and surface a provider failure. It asserts on the events the frontend would have received, because those are the whole of what the React app can know. |
| Native Desktop smoke | On supported native runners, launch the packaged app and prove one critical interaction. This is lower frequency than the bridge suite because native WebViews differ by platform, and it is what covers the window, the webview, and the React app — none of which the bridge suite reaches. |

Portal paths should prefer semantic accessible locators. Setup that is not the
subject of a test may use a fixture API, but the asserted user outcome must go
through the surface being tested. This preserves the existing run-trace pattern
without turning every setup form into a dependency of every assertion.

**A pseudo-terminal is required, not optional.** Approval is an interactive
capability: a surface with no approval handler collapses an `Ask` into a
`Deny`, and print mode is such a surface. So `buildmax -p` can prove a denial
and can never prove an approval, and the CLI suite's central path — a write
tool that asks, is approved, and changes a file — only exists under a terminal.
Print mode still carries everything that is not gated: output shape, session
persistence and resume, trace contents, and failure text. Splitting the suite
this way is what keeps the terminal-driven part small enough to stay inside the
§5.1 budget: two tests answer the prompt, and everything else stays cheap.

A pseudo-terminal has no emulator behind it, so the harness plays that part and
answers the capability queries the TUI writes on startup. Left unanswered they
cost five seconds a test while the TUI waits out its own timeout, which reads
as the agent being slow rather than as the terminal being absent.

**The Desktop bridge needed a seam, and that is worth saying out loud.** Wails
emits to the frontend through an interface that lives in one of its internal
packages, so nothing outside that module can stand in for the frontend. The app
now emits through a one-line indirection of its own, which production never
reassigns and a test replaces with a recorder. Without it the Desktop surface
would have had no deterministic coverage at all — the alternative being a
running `wails dev` and a display, which is the packaged-app smoke, not a suite
anyone runs while changing code.

**Concurrency must be proved to change nothing.**
[parallel-tool-execution.md](./parallel-tool-execution.md) promises identical
observable behavior whatever the scheduler does. A batched scenario — several
read-only calls in one turn, and a batch mixing a gated write with read-only
neighbours — asserts the resulting message history, hook sequence, and prompt
order against a fixture. Nothing below this level can make that assertion,
because nothing below it runs the real scheduler against the real approval
gate.

### 6.1 What The Server And Worker Paths Must Add

Cancellation, retry, and authorization denial already have handler-level Go
tests over a real router and store. Repeating their rules here would be exactly
the unit-test rebuilding this design rejects. Each path must therefore name the
deployment-level fact no handler test can reach:

- **Cancellation** — the signal reaches a live execution and stops it: a
  running Kubernetes Job or worker child process ends, and the run's terminal
  state matches what the deployment actually did.
- **Retry** — a second run is really executed rather than recorded: a new run
  token is issued, a second worker runs, and the managed path leaves a second
  ledger row attributed to the same user and space.
- **Authorization denial** — the denial holds at the deployment edge, through
  ingress and the published routes, not only in the handler under test.
- **Failure recovery** — a worker that dies mid-run leaves the run in a
  terminal, diagnosable state with its artifacts and logs retrievable.

Retry and authorization denial were covered by the deployment smoke first.
Cancellation is covered now, and §6.2 records how — the mechanism is not the one
this section originally proposed, and the reason it is not is worth keeping.
Failure recovery is covered too; §6.3 records how, and why the deployment can
prove only the worker-reported half of it while the store test carries the rest.

That proposal was to let the deployment mock serve more than one scenario,
selected by the model alias a run uses. It cannot be built as written: nothing
in the Portal path chooses a model per run. A task carries an input and an
optional `agent_id`; an `agentdef.Agent` carries instructions and plugins and no
model at all. The model comes from deployment configuration — `llm.default_model`,
or `worker.llm.model` for a managed run — so every run in a stack answers from
the same alias by construction. Selecting a scenario by alias would mean adding
per-run model selection, which is a product decision about who may pick a model
and how it is authorized, not test plumbing. Nothing here should force that
decision.

Both assertions poll rather than read once. A task reports `SUCCEEDED` before
its run output is queryable, so a single read fails on a run that did
everything right; that ordering is a property of the deployment, and an
assertion that ignores it measures the clock instead of the system.

### 6.2 Making A Run Slow On Purpose

A negative case has to act on a run while the run is still going. The scripted
model answers immediately, so a smoke that asks a deployment to cancel a run
usually finds it already finished — and a case that passes because the run ended
on its own proves nothing about cancellation.

The duration of a scripted turn is therefore controllable, and it is the only
thing about a turn that is. A step may carry `delay_ms`, and a mock a suite
cannot rescript — one already built into an image, answering a stack the suite
reaches only over HTTP — takes a stall from `POST /control/stall`. Neither
infers anything from what a request contains. The §4 rule stands unchanged: a
mock that answers what it thinks was asked stops being evidence. How long an
answer takes says nothing about what the agent did, which is exactly why it is
the safe dial.

The stall is armed out of band and applies to every later reply, so it cannot
be aimed at one run — or even at one caller. Creating a task titles it with a
model call of its own, so the create waits out the stall as well; the smoke
gives that request a client patient enough rather than trying to arm the stall
after the create. Arming it later would be the flakier choice by far: a smoke
run measured 30 milliseconds between a worker spawning and its run succeeding,
and a case that has to slip an HTTP call between those two events is a coin
toss. That is why the cancellation case runs last in the
deployment smoke: once it is armed, anything after it would be waiting on the
mock rather than on the deployment. A case that needs a stall aimed at one run
among several concurrent ones needs a different mechanism, and should say so
rather than depend on the order calls happen to arrive in.

Only the control route is published outside the stack — a published port under
Compose, a single `Exact` ingress path under kind. The model protocol itself
stays on the internal network, because a run reaching it is part of what the
smoke is proving. Both are smoke-only; nothing under `deployment/production/`
exposes a test double.

What the cancellation case asserts is the deployment-level half named in §6.1:
the run reaches `CANCELED` rather than `FAILED` or nothing, it is still
`CANCELED` when asked again, and every artifact it lists can be downloaded. It
does **not** yet prove that partial output produced before the cancellation
survives, because a run stalled on its first model call has produced none. That
half needs a scenario that does work before it stalls, which needs the stall
aimed at one run — see above.

### 6.3 Losing A Worker Mid-Run

The failure-recovery case (`kindWorkerLossProbe` in `tools/mk`) holds a run at
its model call, deletes the Kubernetes Job running it, and asserts the run
reaches a terminal `FAILED` naming the worker loss and that its provenance still
answers `FAILED` when asked afterward. That is the §6.1 fact no handler test can
reach: a run whose worker is gone settles to a diagnosable terminal state rather
than staying `RUNNING` forever, and its record survives the loss.

Deleting the Job terminates the worker pod through the kubelet with `SIGTERM`,
which the worker catches and reports as an interrupted run. That is the common
worker-loss shape in this deployment — a rollout, an eviction, a drained node —
so it is the one worth a deployment assertion. The other shape, a worker that
vanishes without reporting (a node crash, a kernel OOM-kill), is settled instead
by the server's liveness sweep, and that path cannot be reproduced from here:
the kernel will not deliver an in-container `SIGKILL` to the worker's PID 1, and
any orderly deletion the kubelet performs starts with the `SIGTERM` the worker
reports on, so the run is always reclaimed by its own report before the sweep's
grace elapses. The sweep is covered at the store level by
`TestReaperClosesRunsWhoseWorkerWentSilent` in
`internal/server/scheduler/stale_runs_test.go`; both paths land the run in the
same terminal, diagnosable state this case asserts.

Unlike cancellation, this case arms the stall *after* the task is created, not
before. It must: creating a task titles it with a model call of its own, and a
stall armed first would hang the create rather than the run. It can, because the
worker is a Job the scheduler dispatches only after the create returns — the
seconds of Job scheduling and pod startup before the run reaches its model call
are a comfortable margin to arm the stall in, where the near-simultaneous
create-and-run of §6.2 left none. The case still waits for `RUNNING` before it
deletes the Job, so it acts only on a run it actually caught mid-flight; a run
that finished first is never mistaken for a stranded one.

### 6.4 Losing The Database

The database-outage probe (`kindDBOutageProbe` in `tools/mk`) proves the
dependency-degradation contract the beta-readiness record names: when the server
loses its database at runtime, it reports the failure through `/readyz` — which
the System Status admin view mirrors, reading the same probes — and is taken out
of the Service rather than restarted, and it recovers on its own once the
database returns. This is the deployment-level fact behind the manifests' split
of readiness (`/readyz`, which pings MySQL and probes object storage) from
liveness (`/healthz`, which checks nothing): a liveness probe that failed on a
dependency outage would restart a healthy server and turn a recoverable outage
into a crash loop.

Interrupting the database is less obvious than it sounds. A deny-all
`NetworkPolicy` on the MySQL pod blocks new connections, but the server's pooled
connection is already established and the pool sets no lifetime, so it is reused
indefinitely and never notices the block — `/readyz` stays ready. The probe
therefore also bounces MySQL (a graceful `mysqladmin shutdown`, after which the
kubelet restarts the container in place so its emptyDir data survives), which
drops the established connection; the server's reconnection then hits the policy
and the outage holds until the probe removes it. Blocking access rather than
deleting the pod is what lets "normal service returns after the database is
restored" be a real assertion: the schema and rows are still there, so a sign-in
afterward reads and writes the same database. The probe reads `/readyz` straight
from a server pod, not through the ingress, because a not-ready pod leaves the
Service and the ingress would answer its own endpoint-less 503 instead of the
app's 503 that names the failed check; the recovery sign-in does go through the
ingress, because that is the "whole path returns" claim.

This is the runtime-loss path. Losing the database at process start is a
separate, deliberate fail-fast: bootstrap connects and migrates with no retry,
so the server exits and crash-loops until the database is reachable. That
asymmetry is intended — a candidate that cannot reach its database at boot has
nothing to serve — and it is not what this probe exercises.

### 6.5 Losing Object Storage

The object-storage probe (`kindStorageDenialProbe` in `tools/mk`) is §6.4's twin
for the other stateful dependency: `/readyz` probes real bucket reachability
(`persist.ListFiles` against a sentinel prefix), so when the server loses object
storage at runtime the `object_storage` check fails, the pod leaves the Service
without being restarted, and it recovers on its own — bucket intact — once
access returns.

The interruption needs the same two steps as the database, and for the same
reason: a deny-all `NetworkPolicy` on the MinIO pod blocks new connections, but
the S3 client's established keep-alive connection is grandfathered through and
reused, so the readiness probe never notices. MinIO has no session-kill to
borrow the way MySQL does, so the probe drops the connection by bouncing MinIO
in place — a `SIGTERM` to its PID 1 exits the container and the kubelet restarts
it in the same pod, so the emptyDir bucket survives while the server's
reconnection hits the policy. MinIO restarts quickly and has no readiness gate,
so it is serving again by the time the policy is removed, and recovery is a fast
reconnection. Blocking access rather than deleting the pod is what lets recovery
find the same bucket: a lost emptyDir would leave a fresh MinIO with no bucket,
which the running server — it creates the bucket at startup — would not restore.
That the `object_storage` check returns healthy after recovery is itself the
evidence the bucket came back with its contents, since the probe lists the
bucket rather than assuming it.

The worker's own write path under a storage denial is a distinct case this
probe does not cover; §6.6 does.

### 6.6 Denying The Worker's Object-Storage Writes

The worker-storage probe (`kindWorkerStorageDenialProbe` in `tools/mk`) covers
what §6.5 cannot reach. A worker writes to the bucket with its own client — a
Task's seed and result checkpoints, and the run's state: trace, session bundle,
and logs — so denying the worker alone never shows on the server's `/readyz`.
Published Artifacts are not among those writes: they go through the server's
worker API, which stores the object before it records the Artifact, so their
path is the server's.

The denial is a Cilium L7 policy on the MinIO pod: worker pods may `GET` and
`HEAD`, the Envoy proxy answers every other method with `403`, and every other
pod keeps full access. Denying writes rather than all traffic is what lets a run
restore its base and reach its agent, so the refusal lands on the run's own
writes instead of its first read. A plain `NetworkPolicy` cannot separate the
two. The policy is in place before any worker it governs starts, so no
connection predates it. Before a run depends on it, a worker-labelled pod
confirms that its write is refused by the proxy while its read reaches MinIO,
and an unlabelled pod confirms that its write still reaches MinIO — the marker
that the fault in force is the one armed, and only for workers.

Two write points are then exercised, with `/readyz` sampled every two seconds
throughout and the server pods left unrestarted:

- a new Task's first run captures its seed before the agent starts, and fails
  closed there with a message naming the refused checkpoint upload;
- a Continue of a Task that already has a head restores it, publishes an
  Artifact through the server, answers, and then cannot store its run state. It
  must end `FAILED` with a message naming object storage, keep its reply, list
  an Artifact that downloads with the bytes it published, and record no trace
  pointer, so its trace reads as never recorded rather than lost.

With the policy removed, a new Task must succeed and its trace must read back.

The Continue case found a real defect. The worker logged a refused run-state
upload and still reported the run `SUCCEEDED`, with a trace pointer storage did
not hold and a session bundle the next turn would silently start without —
the open question [unified-artifacts.md](unified-artifacts.md) §12 recorded.
`internal/agentapp/taskrun` now records a trace pointer only once the trace is
stored, stops at the first refused upload rather than waiting out every file
under an outage, and fails a run whose state could not be stored while keeping
its reply and usage. The rejected-download, lost-response, and
metadata-without-object modes of
[verification-program.md](verification-program.md) §6.3 remain open.

## 7. AI Agent Workflow

The harness is a first-class tool for code-changing agents, not merely a CI
afterthought. A contributor-facing guide and the workspace `AGENTS.md` must
tell an agent:

1. which suite maps to a changed subsystem or user flow;
2. which command is safe to run and whether it owns Docker, kind, a Wails
   process, or only a temporary directory;
3. the expected duration and the test's deterministic fixture assumptions;
4. where the failure artifacts and redacted logs are; and
5. when to run a broader suite instead of repeatedly retrying a narrow one.

An agent starts with the narrowest affected path. It reads the retained artifact
before changing code, fixes the named boundary, and reruns the same suite. It
does not replace an E2E failure with a hand-waved manual test or run the full
matrix by default.

The runbook itself is current behavior, not rationale, so it lives in
[`../contribute/testing.md`](../contribute/testing.md), with `AGENTS.md`
carrying only the suite-selection summary and a link. This design record keeps
the trade-offs; it must not become the runbook.

## 8. CI Policy

| Trigger | Verification |
|---|---|
| Pull request | Fast existing checks only. E2E is not a required gate. |
| Merge to `main` | Deployment and browser E2E, with artifacts retained on failure. |
| Scheduled run | Full direct and managed deployment matrix plus the platform-native Desktop smoke that the available runner supports. |
| Manual dispatch | Any expensive suite for release preparation or a suspected environment regression. |

Path filters may decide which post-merge suite is worth running, but they must
never silently turn a skipped E2E result into proof that a user journey passed.
The workflow summary must identify tests as run, skipped by policy, or failed.

Moving E2E off the PR gate has costs, and they have to be paid explicitly
rather than discovered later.

- **Concurrency must stop cancelling evidence.** `deployment-smoke.yml`
  currently groups on `github.ref` with `cancel-in-progress: true`, which is
  right for a pull request and wrong for `main`: the ref is constant there, so
  a second merge cancels the first one's verification and no commit is ever
  proved. The post-merge job must either not cancel, or group per commit SHA.
  A cancelled run reports as cancelled, never as skipped-by-policy.
- **A red `main` needs an owner and a deadline — and something that enforces
  it.** A post-merge E2E failure is triaged by the author of the merge that
  broke it; if it is not fixed or reverted within one working day, the merge is
  reverted. This bullet was written as a rule and shipped as a sentence, and it
  held for exactly as long as someone happened to be watching: the suite went
  red on one merge and stayed red through four more, none of which had any
  reason to notice, until an unrelated change looked at the run.

  So the red state now stops the next merge. `ci.yml`'s **Deployment smoke
  health** job reads the last completed `deployment-smoke.yml` run on `main` and
  fails the pull request when that run failed, naming the run and the commit
  that left it red. It verifies nothing about the pull request in front of it —
  it refuses to add to an unverified `main`, which is the pressure this bullet
  assumed and never had. A pull request repairing the suite carries the
  `deployment-smoke-fix` label and is let through.

  The alternative was moving the browser suite back onto the pull-request gate.
  That would have caught this particular break earlier, at the cost of a stack
  in front of every pull request — and it treats the problem as late detection.
  The problem was that nobody owned a break everyone could see.
- **Flakes are quarantined, not retried.** A test that fails intermittently is
  moved out of the gating set with an issue attached, on the same day. Blanket
  retries are not added: the browser suite sets `retries: 0` deliberately, and
  a hidden flake is worse than a visible one.
- **The `gui` package loses its only PR-time coverage.** That was accepted here
  only because it was cheap to replace, and it has been: `gui` has its own
  component tests and the frontend job runs them, so a shared-component
  regression is caught before merge again. A lint step for the package is still
  missing, and stays blocked until typescript-eslint supports the TypeScript
  version it builds with.

## 9. Delivery Order

1. **Build the deterministic model harness.** Scripted multi-turn scenarios,
   batched tool calls, all three wire protocols, runnable as a plain process
   and as the existing image. Every suite that involves a tool call waits on
   this.
2. **Add the CLI path first among the surfaces.** The CLI is the primary
   single-binary surface, it has no deterministic verification today, and it is
   the only suite that can run in seconds without Docker — so it is the one
   that actually delivers the feedback loop this design is for. Real binary,
   temporary home, pinned permissions, scripted model; print mode for the
   ungated paths and a small pseudo-terminal suite for approval and
   TUI-specific behavior. Ordering it after the container suites would postpone
   the whole point of the work.
3. **Stabilize the existing Portal and deployment suite.** This can run
   alongside step 2 if two people are available: isolated fixtures under the
   two lifecycle modes above, cleanup, reproducible artifacts, preflight
   diagnostics, and one local lifecycle-owning wrapper alongside the current
   attach-to-deployment mode. Move the comprehensive workflow from PR gating to
   post-merge, scheduled, and manual triggers, and fix the concurrency and
   ownership rules named in §8 in the same change.
4. **Add Portal golden paths** for issue/agent execution, workflow execution,
   files, settings, and the member/owner/system-administrator authorization
   matrix. Keep the original API smoke focused on deployment behavior, and give
   the server and worker paths the deployment-level assertions in §6.1.
5. **Add the Wails bridge suite**, then a small native packaged-app smoke on
   supported platform runners. **Done.** The bridge suite landed as
   `TestBridge*` in `internal/interface/desktop`, run by `./make e2e desktop`.

   A second suite, `./make e2e desktop-ui`, has landed since: it drives
   desktop/frontend's real React app and its bound Go methods through `wails
   dev`'s own browser dev server (see tools/mk/desktop_ui.go). That covers the
   React app and the bridge together, against a real (if headless-launched)
   Chromium — narrower than what this step originally asked for, on purpose:
   it says nothing about how the native window renders that same page, and
   nothing about the signed, packaged build. `wails dev` opens a native window
   too, as a side effect of starting, but the suite makes no assertion about
   it.

   The packaged-build launch smoke now runs as `./make e2e desktop-launch`
   after `./make build desktop` on macOS and Windows CI. It proves that the
   packaged process starts and remains alive for a short dwell; it does not
   drive the window or assert its rendered contents.

   That step turned out to have a prerequisite this record did not name. Until
   `desktop-package.yml`, **no CI job produced a packaged app at all**:
   `go build ./...` compiles the desktop packages through the `!desktop` half
   of the build tag, and the frontend job builds `desktop/dist` without ever
   embedding it. A smoke has nothing to launch until something builds one, so
   the first half of the step is the build — now running post-merge, weekly,
   and on demand on macOS and Windows via `./make build desktop`.

   Signing is not part of that prerequisite, which is worth stating because it
   reads like it should be. Gatekeeper refuses a bundle that arrives with a
   download's quarantine attribute; one built and launched on the same machine
   has none, which is why `./make build` then `./make run desktop` is the
   documented contributor path. A runner is that same case. Signing and
   notarization gate **distribution** — which is why `.goreleaser.yaml` leaves
   the app out, and why the build job publishes no artifact — and they do not
   gate a smoke.

   The hosted macOS and Windows jobs now answer the launch question. Visual
   native-window interaction remains a different, higher-cost test and is not
   implied by this smoke.
6. **Publish the contributor and agent runbook** in `docs/contribute/`,
   including suite selection, prerequisite checks, artifact locations, and a
   release-time full-matrix command. Landed as
   [`../contribute/testing.md`](../contribute/testing.md), with `AGENTS.md`
   carrying the summary and the link, and `./make e2e all` as the matrix.

Renaming the model-driven `./make smoke` rode along with step 3. Giving `gui`
tests rode along with step 4, which is when Portal paths started depending on
the shared components; a lint step for it is blocked until typescript-eslint
supports the TypeScript version it builds with.

## 10. Success Criteria

This direction is complete when a developer or AI agent can start from a clean
checkout, select the relevant local suite without hidden credentials, execute
it without touching personal data, and receive enough retained evidence to
repair a failure. Concretely:

- no suite requires a provider API key, and no command named like a test
  silently does;
- the CLI suite runs inside the default `./make test` loop within its budget,
  including the terminal-driven approval path;
- a batched turn is proved to produce one canonical history and prompt order;
- every post-merge run reports each suite as run, skipped by policy, cancelled,
  or failed, and no commit reaches a release with its verification silently
  cancelled;
- a failure hands back the scenario, the transcript, the redacted logs, and one
  command that reproduces it.

Pull-request latency remains governed by fast checks, while post-merge and
scheduled runs continuously verify the broader deployment and native-surface
claims.
