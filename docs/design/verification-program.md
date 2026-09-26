# Verification Program

> **简体中文：** [阅读中文镜像](../zh-CN/design/验证计划.md)

> **Audience:** maintainers and contributors · **Status:** partially implemented — remaining evidence backlog

Related records: [Local end-to-end verification](end-to-end-testing.md),
[evaluation and qualification](evaluation-system.md), and the
[current-state assessment](../current-state.md).

## Contents

- [Status](#status)
- [1. Decision](#1-decision)
- [2. Verification Matrix](#2-verification-matrix)
- [3. Critical User Journeys](#3-critical-user-journeys)
- [4. Pull-Request MySQL Gate](#4-pull-request-mysql-gate)
- [5. Deterministic End-To-End Verification](#5-deterministic-end-to-end-verification)
- [6. Failure Injection](#6-failure-injection)
- [7. Scheduled Deployment Verification](#7-scheduled-deployment-verification)
- [8. Release-Candidate Qualification](#8-release-candidate-qualification)
- [9. Quality Controls For AI-Generated Tests](#9-quality-controls-for-ai-generated-tests)
- [10. Fuzzing And Mutation Testing](#10-fuzzing-and-mutation-testing)
- [11. Product Evaluation And Dogfooding](#11-product-evaluation-and-dogfooding)
- [12. Evidence Contract](#12-evidence-contract)
- [13. Delivery Order And Completion Criteria](#13-delivery-order-and-completion-criteria)

## Status

- roadmap_priority: `R0–R4` — this program supplies the evidence required by
  worker containment, topology correctness, persistence, account/space closure,
  and qualification breadth in [../ROADMAP.md](../ROADMAP.md)
- status: `partially_implemented` — the repository already has extensive unit
  and contract tests, fast CLI and Desktop E2E suites, Portal Playwright
  coverage, Compose and kind smoke, release evidence, and a black-box
  evaluation framework. §4.1's command surface is shipped as `./make test
  mysql` and runs on every pull request. §4.2's contention cases cover Task
  claiming, run transitions, cancellation, one-active-run admission, and
  idempotency; Artifact tombstoning and retention, retry lineage, direct Task
  admission, checkpoint head advancement, initial Workflow revisions, the
  Workflow reconciliation lease (due-run discovery plus concurrent
  claim/renew/release with takeover), Workflow revision advancement under
  edits and contention, and the Workflow recovery loop finishing a run stranded
  by a lost callback also have real-MySQL coverage. Cross-Space store isolation,
  quota windows, durable auth sessions, external identities, structured TaskRun
  results, and trace-retention discovery are now covered too. Remaining database
  work is any uncovered store-specific cross-Space path and fixtures for the
  candidate's declared starting schema. The unified matrix, several failure
  paths, and complete release rehearsal described here are not implemented
- principle: implementation code, tests, and documentation can share the same
  mistaken assumption when all are generated from one context. Acceptance must
  therefore assert independently observable outcomes across public interfaces,
  durable state, and forbidden side effects
- non-goal: maximizing a repository-wide coverage percentage or putting every
  container, browser, cluster, and real-model test on every pull request
- touches: `tools/mk`, `.github/workflows`, `internal/infra/db`,
  `internal/testsupport/mockllm`, `deployment/smoke`, `portal/e2e`,
  `.artifacts`, and contributor testing documentation
- created_at: `2026-08-30`

## 1. Decision

BuildMax will maintain one risk-weighted verification program with four gates:

| Gate | Purpose | Dependencies | Expected cadence |
|---|---|---|---|
| Pull request | Reject deterministic code, contract, authorization, persistence, and critical-journey regressions quickly. | Go, Node for frontend scopes, and hermetic MySQL in CI. | Every change. |
| Scheduled deployment | Exercise real processes, browsers, storage, direct and managed inference, worker lifecycle, and controlled failures. | Compose and kind. | After merge, scheduled, or manually dispatched. |
| Release candidate | Prove immutable artifacts in a private deployment with external dependencies, recovery, upgrade, rollback, and credential rotation. | Kubernetes, external MySQL, S3, TLS. | Every candidate. |
| Product evaluation | Measure how reliably supported models use a correctly wired product. | Real model credentials and an explicit token budget. | Deliberate qualification runs. |

The gates answer different questions. A real-model score cannot prove that
authorization, persistence, or cancellation is wired correctly. A deterministic
smoke cannot prove that a model reliably uses the behavior. Neither can replace
an external restore and upgrade exercise.

The program extends the existing task-runner and workflow surfaces. New test
behavior belongs under `tools/mk` and existing workflow families; it must not
introduce a parallel shell-script command surface.

## 2. Verification Matrix

Create `docs/contribute/verification-matrix.md` when the first journey below is
wired. It is the status-bearing index from product claims to evidence, not a
directory of test files.

Each matrix row records:

| Field | Meaning |
|---|---|
| Journey ID | Stable `Vxx` identifier from this record. |
| User outcome | What a user or operator can observe, without naming an implementation function. |
| Surfaces | CLI, Desktop, Portal, Server, worker, or deployment boundaries involved. |
| Risk | Security, data integrity, execution safety, reliability, or product quality. |
| Deterministic evidence | Exact test or suite that proves the normal path. |
| Real dependencies | MySQL, object storage, browser, Kubernetes, or model required by that evidence. |
| Failure evidence | Exact controlled failure and expected terminal state. |
| Gate | Pull request, scheduled deployment, release candidate, or evaluation. |
| Last proof | Commit, date, environment, and evidence-bundle reference. |
| Gap | `missing` plus a concrete reason when a required layer does not exist. |

Rules:

- every roadmap capability marked complete maps to at least one journey;
- an empty cell is invalid — use `not applicable` or an explicit gap;
- a passing unit test is not deployment or operating evidence;
- the matrix links evidence rather than copying logs or status prose;
- automated jobs update machine-readable result data, while maintainers update
  the status-bearing matrix when a capability boundary changes;
- obsolete journeys are removed with the behavior they described; git history
  is the archive.

## 3. Critical User Journeys

The first matrix contains the following journeys. They are deliberately
cross-package and user-visible.

| ID | Journey | Required observable result | Minimum gate |
|---|---|---|---|
| V01 | CLI changes a workspace file | The requested content changes, paths outside the workspace do not, session history and trace explain the tool use. | Pull request |
| V02 | CLI resumes and compacts a session | The selected session resumes, context sources remain correct, compaction preserves required state, and the task completes. | Pull request |
| V03 | Desktop completes the same local task | The Desktop bridge assembles the shared runtime, surfaces approval and events, and persists the session consistently with CLI. | Pull request |
| V04 | A user signs in and ends a session | A login code is single-use and expires; refresh rotation, logout, and revoked credentials behave against the real store. | Pull request with MySQL |
| V05 | Spaces are isolated | A member of Space A cannot read, mutate, stream, or download Space B resources even when given valid public IDs. | Pull request with MySQL |
| V06 | Portal completes a foreground conversation turn | Streaming, durable history, refresh, reconnect, and final presentation agree. | Scheduled deployment |
| V07 | Multiple turns target one conversation | Turns execute in submission order, the queue is bounded, nothing is lost, and no two turns mutate one conversation concurrently. | Pull request and scheduled multi-process proof |
| V08 | An Issue produces a background result | Task, TaskRun, worker claim, model/tool execution, artifacts, and Issue result projection form one explainable chain without requiring a Conversation. | Pull request with MySQL and scheduled deployment |
| V09 | A direct Task result survives reconnect and restart | TaskRun remains the authoritative result, a refreshed Task page reconstructs its thread, and reconnect or Server restart creates no duplicate run, output, Artifact, or usage record. | Scheduled deployment |
| V10 | A running task is canceled | The run reaches `CANCELED`, stays terminal, stops further work, retains available evidence, and exposes no unavailable artifact. | Scheduled deployment |
| V11 | A failed task is retried | Retry creates an explicit new attempt, preserves the old attempt, and does not duplicate outputs, usage, or Artifacts. | Pull request with MySQL |
| V12 | A workflow revision runs linearly | The run uses the revision captured at start; step output and failure propagation are ordered; cancellation blocks remaining steps. | Pull request with MySQL |
| V13 | Managed inference serves local and worker clients | No provider credential reaches the client or worker; per-run authorization, usage ledger, and quota are correct. | Scheduled deployment |
| V14 | Artifacts preserve authorization and integrity | Upload, list, preview, and download succeed for the owning space; cross-space access fails; bytes and metadata agree. | Pull request with MySQL |
| V15 | A space activates a safe plugin release | Exactly the pinned skill/subagent content materializes; disallowed hook or MCP releases are rejected; nothing is inherited implicitly. | Pull request and worker smoke |
| V16 | Server shutdown drains work | New claims and turns stop, in-flight work reports its documented outcome, and restart does not strand a running record. | Scheduled deployment |
| V17 | A worker disappears without reporting | Heartbeats expire, the lost-worker path closes the run predictably, partial evidence remains, and retry is explicit. | Scheduled deployment |
| V18 | Database or object storage becomes unavailable | Readiness changes, user-visible state is honest, no phantom artifact is published, and recovery or retry has an unambiguous path. | Scheduled deployment |
| V19 | A database and bucket pair is restored | Relational records and stored objects agree after restore; every retained artifact reference resolves or is explicitly tombstoned. | Release candidate |
| V20 | A candidate upgrades and rolls back | The declared schema path, supported binary rollback or destructive-cutover recovery, drain, and credential rotation follow the documented contract without ambiguous data loss. | Release candidate |

Every journey test uses the same assertion structure:

1. arrange state through supported bootstrap or public interfaces;
2. perform the user or operator action through the public surface;
3. assert the public response and visible result;
4. assert durable MySQL and object-storage state;
5. assert trace, audit, artifact, and managed-call evidence where applicable;
6. assert forbidden side effects, including cross-space reads, duplicate rows,
   duplicate usage, files outside the workspace, and non-terminal runs;
7. clean up or use an isolated namespace so order cannot affect the result.

## 4. Pull-Request MySQL Gate

### 4.1 Command Surface

**Shipped** as `./make test mysql` (`tools/mk/test_mysql.go`), covered by its
command-surface tests, documented in `./make help test` and
[../contribute/testing.md](../contribute/testing.md).

The command must:

- require an explicit test DSN and refuse to fall back to contributor data;
- create a uniquely named temporary database;
- run `AutoMigrate` and every explicit migration;
- run `internal/infra/db` plus the service/handler integration scopes selected
  by the matrix;
- fail when a test in this scope skips because the DSN is absent;
- drop only the validated temporary database it created;
- print the database name, migration version, failing package, and reproduction
  command without printing credentials.

CI supplies a pinned MySQL service container and invokes the same task-runner
command contributors use. The local command may attach to MySQL started by the
contributor; it must not silently start Docker as a side effect of `./make test`.

### 4.2 First Persistence Cases

Covered today: user and space creation, login codes, refresh tokens, public IDs,
system grants, plugin activation, LLM models and calls, audit search, revision
queries, TaskRun transitions and claiming, retry lineage, direct Tasks,
checkpoint head advancement, Artifact retention, issues, conversations, the
Space invitation and ownership-transfer lifecycle, Workflow revision advancement
under edits and contention (`TestWorkflowRevisionContention`, a guarded
compare-and-set on the workflow row that appends its revision in the same
transaction; mutation-checked by removing the revision predicate), the linear
Workflow reconciler in `internal/service/workflow`
(`TestWorkflowReconcile...`, which folds a running step's terminal TaskRun —
read from the TaskRun store, not a callback — into the guarded step and run
transitions, distinguishes failure from cancel while blocking later steps,
recovers a lost callback from persisted state, and leaves one outcome and one
next Task under concurrent reconciliation), and — in
`internal/infra/db/concurrency_test.go` — four store methods under contention:
`ClaimTask`, `TransitionTaskRun`, and `RequestTaskRunCancel` each as a
conditional UPDATE, and `CreateTaskRun`'s one-active-run-per-task and
idempotency-key guarantees as a locking read on the task row followed by a
count-then-insert inside one transaction, since neither guarantee is a
conditional UPDATE on a row that exists yet. The removed Tier 1
result-delivery queue has no remaining persistence obligation; its former tests
are historical context rather than a case to recreate.

Each of those four was checked by mutation rather than assumed: removing the
locking clause or replacing the conditional UPDATE with a read-then-write
makes the corresponding test fail. That step is the point. The first attempt
at the task-claim test passed against a deliberately broken implementation,
because MySQL reports rows *changed* rather than rows *matched*, so seven
losing callers writing the status it already held were counted as zero
affected rows. A concurrency test that has not been shown to fail is not
evidence.

Still to write:

- restart recovery cases for durable Task/TaskRun/checkpoint state;
- migration fixtures for the declared starting schema. The three explicit
  migrations include `issue_owner_executor_split`, whose old-column backfill
  already has a MySQL test. This is not an old-binary rollback fixture. Alpha
  may drop compatibility; test the claimed upgrade/rollback version pair or
  destructive-cutover recovery, as required by the Beta readiness record;
- Agent revision authority through the Workflow reconciler (R2): that a step
  sends the pinned Agent revision even when the live Agent is edited mid-run.
  The linear reconciler itself, idempotent step admission, lost/concurrent
  terminal callbacks, and the Server-owned recovery loop (startup sweep, per-run
  reconcile, and end-to-end restart recovery of a run stranded by a lost
  callback) are now covered (above).

Cross-Space lookup rejection at the store — distinct from the role matrix the
handler tests already assert — is now covered in the MySQL scope by
[`cross_space_test.go`](../../internal/infra/db/cross_space_test.go). It proves
the store's own space guards, not the edge: a workflow update and an issue
update addressed from another space are silent no-ops that neither mutate the
row nor reveal it exists (each given the real revision or version, so the space
guard, not a stale precondition, is what rejects), and a plugin activation is
invisible to another space's read, list, and suspend — the cross-space suspend
answering `ErrNotFound` rather than reaching across the boundary. The existing
`publicid_test.go` oracle, that a foreign handle answers identically to an
unknown one, still carries the list and aggregate paths.

One item from this list is withdrawn rather than pending. **Quota reservation
and charging boundaries** describes a design that does not exist: there is no
reservation. `internal/service/quota.Check` reads a rolling window through
`SpaceUsageInWindow` and compares, and the store holds only `GetQuotaTier` and
`SeedDefaultQuotaTiers`. Concurrent runs can therefore overshoot a limit, and
that is a property of the current design rather than a defect a test should
pin. What is worth covering there is the window-boundary arithmetic of the
usage query — query correctness, not a concurrency guarantee — now covered in
the MySQL scope by
[`quota_usage_test.go`](../../internal/infra/db/quota_usage_test.go): the
inclusive `[since, until]` boundary, per-space scoping of the join, title
tokens billed by the task's own `created_at`, NULL token columns folded to
zero, and an unknown space returning zero rather than an error. Reinstate the
reservation bullet only if reservation is ever built.

### 4.3 Acceptance

- pull-request CI contains no `BUILDMAX_TEST_DSN not set` skip in the MySQL job;
- repeated and parallel runs do not depend on test order or shared IDs;
- two claims cannot win one run;
- repeated cancel, report, and retry operations preserve legal state;
- a fixture for the declared starting schema exercises the supported upgrade
  or destructive-cutover recovery and passes the critical journeys;
- failure output identifies the relevant SQL or state transition without
  leaking secrets;
- coverage is reported per critical package. The program does not impose a
  repository-wide coverage target.

## 5. Deterministic End-To-End Verification

System wiring uses the committed deterministic model harness. Real models are
reserved for §11 because their variability makes them a poor oracle for
authorization, persistence, and delivery.

### 5.1 Harness Capabilities

Extend `internal/testsupport/mockllm` and `deployment/smoke/mock-llm` only as
required by matrix journeys. The shared scenario vocabulary must support:

- fixed streamed responses and fixed tool-call sequences;
- a failure on a selected request or iteration;
- a stall released explicitly by the test;
- malformed tool arguments and unknown tool names;
- a stream that disconnects partway through;
- delayed and duplicate protocol messages;
- per-run controls so parallel tests do not interfere;
- bounded recording of prompts, tools, model metadata, and request order for
  assertions and diagnosis.

The harness must not reproduce Agent Core decision logic. It supplies model
protocol events; product code still owns every state transition and policy
decision under test.

### 5.2 First Pull-Request Journeys

Wire V04, V05, V07, V08, V11, V12, V14, and V15 first. These journeys exercise
identity, authorization, durable work, revision capture, artifacts, and plugin
selection without requiring an externally deployed cluster.

A deployment journey never stops at HTTP success. V08, for example, asserts:

- Task and TaskRun public state;
- one successful claim and one reporter identity;
- the expected model and tool transcript;
- exact artifact bytes and downloadable authorization;
- Issue `latest_result` and output projection;
- trace, audit, and managed-call linkage when the mode supplies them;
- absence of duplicate TaskRuns, outputs, Artifacts, and usage rows.

### 5.3 Existing Suite Placement

- CLI and Desktop remain fast deterministic suites included by `./make test`;
- Portal browser behavior runs through the existing Playwright and `e2e`
  surfaces;
- `./make e2e local` owns its Compose stack;
- `./make e2e compose` and `./make e2e kind` remain guests of a deployment
  another command or workflow started;
- deployment smoke and browser E2E share journey IDs and scenario definitions
  where possible, rather than becoming two unrelated claims about the same
  behavior.

## 6. Failure Injection

Failures must be controlled and reproducible. Randomly killing services without
recording the injection point produces noise, not evidence.

Every fault control provides:

- a stable name, the target run, process, or dependency, and explicit arm and
  release points;
- a bounded deadline;
- an observable marker proving the fault occurred, so the test can tell a
  deliberate injection from an environment that was already unhealthy;
- idempotent cleanup; and
- a failure when the test completes without exercising the armed fault.

Controls may delay, disconnect, terminate, pause, or deny. They never inspect
prompt content to decide what the product probably intended. A control that
must target one run is keyed to a run-owned opaque identifier; if no such
control exists without changing product authority, the case returns to design
rather than adding per-run model selection as test plumbing
([end-to-end-testing.md §6.1](end-to-end-testing.md#61-what-the-server-and-worker-paths-must-add)).

The kind deployment smoke now implements three bounded slices from this
section: graceful worker loss during execution, runtime MySQL denial and
recovery, and runtime object-storage readiness denial and recovery. The silent
hard-loss reaper remains store-tested because Kubernetes Job deletion delivers
`SIGTERM`; worker artifact writes under storage denial, Server restart/reconnect,
partial-work cancellation, and graceful shutdown under load remain open.
The Server restart/reconnect case restarts the serving path while a direct Task
or foreground turn is observable; after reconnect, durable state must
reconstruct the same work with no duplicate TaskRuns, outputs, Artifacts, usage,
or message-history writes.

### 6.1 Worker Lifecycle

Add controls and cases for terminating a worker:

- after claim and before model execution;
- during a tool call;
- while an artifact is being uploaded;
- after producing output but before terminal report;
- after terminal report but before an optional Conversation projection.

Each case asserts the documented terminal state, heartbeat and reaper timing,
partial evidence retention, absence of a phantom result, and the explicit path
to retry. No run may remain `RUNNING` beyond the bounded recovery interval.
The same cases must prove that a direct Agent Task needs no Conversation
delivery in order to reach or expose its terminal result.

### 6.2 MySQL

The Compose failure suite pauses or denies database access while:

- a worker claims a run;
- a service writes a state transition;
- the Server records or reads TaskRun state during a restart boundary;
- the Server drains during shutdown.

Assertions cover `/readyz`, bounded retries, idempotency after reconnection,
terminal state, and error classification. The test must distinguish a deliberate
injection from an unrelated infrastructure failure.

### 6.3 Object Storage

Provide controlled failure modes for:

- rejected upload;
- rejected download;
- stored bytes followed by a lost response;
- metadata committed while the object is absent;
- object stored while relational association fails.

No failed path may advertise a downloadable artifact that does not exist. A
recovery operation must reconcile or tombstone inconsistent state explicitly.

### 6.4 Model And Streaming

Cover rate limits, server errors, timeouts, partial streams, invalid tool calls,
duplicate tool-call IDs, context exhaustion, and WebSocket disconnect/reconnect.
Results classify failures as Agent/model, product, grader, or infrastructure;
they must not collapse every failure into a false capability score.

## 7. Scheduled Deployment Verification

Extend the existing deployment-smoke workflow rather than adding an unrelated
orchestrator.

The scheduled matrix is:

| Environment | Mode | Required scope |
|---|---|---|
| Compose | direct | Server, local-process worker, object storage, Portal, artifacts, and direct provider path. |
| Compose | managed | Managed gateway, per-run authorization, call ledger, and quota. |
| kind | direct | Ingress, Kubernetes Job worker, pod boundary reporting, artifacts, and cancellation. |
| kind | managed | Job worker token, managed gateway, ledger, and quota through the deployed path. |
| Compose | failure | Planned model, worker-write storage, and shutdown injections from §6. |
| kind | lifecycle | Cancellation plus shipped graceful worker-loss and dependency-readiness probes; Server restart, reconnect, and silent hard-loss deployment proof remain open. |

Workflow rules:

- build or select images for the exact commit under test and record their
  digests; never prove a commit with an older mutable image;
- use unique space, user, task, run, and artifact identifiers for every matrix
  cell;
- do not retry a failed job into green. A diagnostic retry may run, but the
  original failure remains in the result;
- upload the §12 evidence bundle on success and failure;
- label each failure product, test/harness, or infrastructure before it can be
  ignored for release qualification;
- a quarantined flaky test remains visible in the matrix with an owner and
  reason; quarantine is not a silent pass.

## 8. Release-Candidate Qualification

Every Beta or later candidate is exercised with the immutable artifacts being
considered for release.

### 8.1 Environment

- private Kubernetes environment;
- external MySQL and S3-compatible storage;
- TLS at the published endpoint;
- Kubernetes Job workers;
- two Server replicas with Redis coordination, while explicitly exercising
  cross-replica delivery, concurrent turns, reconnects, and Redis
  failure/recovery;
- no fixed development login or provider credential;
- direct and managed inference qualified separately;
- exact image digests, dependency versions, configuration digest, operator,
  and date recorded before execution.

### 8.2 Operator Journey

The operator must:

1. bootstrap the first System Administrator;
2. create or provision a normal account and space;
3. sign in through Portal;
4. create Agent, Issue, and Workflow resources;
5. complete a foreground conversation;
6. complete a background task and inspect its result;
7. download an artifact and inspect trace, audit, managed-call, and quota data;
8. cancel an in-flight run and retry a failed run;
9. restart the Server gracefully;
10. kill a worker without a graceful report;
11. interrupt database access and object-storage access separately;
12. restore the database and bucket as a pair;
13. exercise the declared schema path and supported binary rollback or
   destructive Alpha cutover recovery;
14. rotate JWT, database, object-storage, and provider credentials using the
   documented drain/restart procedure.

### 8.3 Acceptance

- every run reaches an explainable terminal state;
- no dangling run exceeds the documented recovery interval;
- every retained artifact reference resolves after restore or is explicitly
  tombstoned;
- database and bucket state agree after paired recovery;
- rollback behavior matches the supported migration contract;
- an operator who did not implement the change can diagnose every injected
  failure using documented surfaces and collected evidence;
- [../deploy/beta-readiness.md](../deploy/beta-readiness.md) references the
  immutable evidence bundle and records every accepted limitation.

## 9. Quality Controls For AI-Generated Tests

Every behavior-changing contribution includes the following review block in
its issue, change notes, or pull-request description:

```text
Behavior:
Observable result:
Persistent result:
Forbidden side effects:
Failure cases:
Tests added:
Not tested:
```

Implementation and acceptance use separate contexts. The implementation pass
may add focused tests. A fresh acceptance pass receives the requirement, public
interfaces, and diff, and tries to falsify the behavior by finding missing
outcomes, forbidden side effects, and shared assumptions.

Review rules:

- authorization, execution policy, quota, migration, and state-machine changes
  contain at least one negative case;
- a bug fix first gains a failing regression case with an independent oracle;
- public API, filesystem, database, trace, audit, or artifact state is preferred
  over private implementation calls as the acceptance oracle;
- mocks supply dependencies and protocol events; they do not reimplement the
  rule under test;
- `err == nil` alone is not a behavior assertion;
- tests must show useful diagnosis when they fail;
- reviewers ask whether removing or reversing the changed guard would still let
  the test pass.

## 10. Fuzzing And Mutation Testing

These techniques are targeted at high-risk invariants, not run indiscriminately
over the repository.

### 10.1 Fuzz Targets

Add Go fuzz targets for:

- workflow-definition parsing and validation;
- public IDs and path containment;
- tool argument decoding;
- webhook payload and message-path extraction;
- trace redaction and bounded serialization;
- configuration-layer merge and environment parsing;
- session/history decoding and recovery;
- state-transition inputs that can be isolated as pure domain logic.

Every discovered crash, hang, path escape, or invariant violation becomes a
committed regression seed.

### 10.2 Mutation Targets

Start with space policy, Task/TaskRun transition rules, login-code consumption,
quota, workflow validation, sandbox policy, and artifact authorization.

The important mutations are deleted authorization checks, reversed role
comparisons, changed terminal-state predicates, quota boundary changes,
cancellation followed by success, and removed space filters. These mutations may
not survive. The program does not require a global mutation score for DTOs,
generated structures, or simple adapters.

## 11. Product Evaluation And Dogfooding

### 11.1 Product-Owned Evaluation

Expand the current three tasks to a representative 12–20 task suite before
using evaluation for release qualification. Cover:

- file search and multi-file editing;
- answering without unnecessary Bash;
- safe recovery from a permission denial;
- compaction and session resume;
- Project Memory read and update;
- subagent delegation;
- artifact production;
- worker access to space files;
- tool failure and malformed result recovery;
- cancellation and timeout response;
- result explanation through the trace.

Prefer deterministic graders: file hashes, JSON schemas, commands, directory
state, trace events, forbidden tool calls, and database terminal state. Use a
model grader only for genuinely subjective quality after calibration against
human judgments.

Real-model runs select a small supported model set, repeat each task enough to
show variability, use an explicit token budget, and report Agent, product,
grader, and infrastructure failures separately. External benchmarks remain a
coordinate, not the product's release gate.

### 11.2 Dogfooding

Maintain a lightweight manual verification log containing commit, surface,
model, real task, success, interventions, confusing behavior, and linked issue.
Repeat a small set of useful tasks: a Go bug fix, a multi-file configuration
change, a Portal Issue completed by a worker, cancellation and retry, session
resume, and artifact generation/download.

Manual use is not a substitute for automation. It detects the different class
of failure where every asserted fact is true but the product remains confusing
or impractical.

## 12. Evidence Contract

Every scheduled or release run writes a bounded bundle under:

```text
.artifacts/verification/<commit>/<environment>/<journey>/
  manifest.json
  result.json
  server.log
  worker.log
  portal.log
  trace.jsonl
  audit.json
  db-state.json
  artifacts.json
  screenshots/
  playwright-trace/
  junit.xml
```

Files that do not apply are omitted and recorded as not applicable in
`manifest.json`; empty placeholder evidence is not success.

The manifest records:

- commit and dirty state;
- image digests;
- OS and architecture;
- MySQL, object-storage, Kubernetes, and browser versions;
- direct or managed inference mode;
- journey and scenario versions;
- start and end time;
- injected fault and injection point;
- final pass/fail classification;
- bounded references to private logs or artifacts that cannot be exported.

Bundles are private by default, redact credentials and model-sensitive content,
and follow an explicit retention limit. A CI summary links the bundle and shows
the terminal classification without requiring a maintainer to download every
log.

## 13. Delivery Order And Completion Criteria

Implement in dependency order:

1. create the verification matrix with V01–V20 and link existing evidence;
2. add the pull-request MySQL command and CI job;
3. wire deterministic V04, V05, V07, V08, V10, V11, V14, and V15 journeys;
4. add worker, MySQL, object-storage, model, and shutdown failure controls;
5. make scheduled Compose/kind runs emit the evidence contract;
6. execute the external Kubernetes restore, upgrade, rollback, and credential
   rotation rehearsal;
7. add targeted fuzzing, mutation checks, evaluation breadth, and continuous
   dogfooding.

Gate rules:

- pull-request deterministic checks pass without retry;
- scheduled failures remain visible until classified, and an unclassified
  failure blocks promotion;
- a release candidate repeats every required proof with its own immutable
  digests;
- a roadmap capability is complete only when its matrix row has unit/contract
  evidence and deterministic end-to-end evidence appropriate to its boundary;
- Beta additionally requires real deployment, failure, recovery, and operator
  evidence;
- real-model evaluation is reported separately from deterministic system
  correctness.

This program is complete when every V01–V20 row has the evidence required by
its minimum gate, the release rehearsal has been performed by an operator who
did not implement the candidate, and no production-readiness claim depends on a
test that silently skipped its real dependency.
