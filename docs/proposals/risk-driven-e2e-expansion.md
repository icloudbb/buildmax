# Risk-Driven End-to-End Verification Expansion

> **简体中文：** [阅读中文镜像](../zh-CN/proposals/risk-driven-e2e-expansion.md)
>
> **Audience:** maintainers, contributors, release operators, and verification authors · **Status:** proposal — under discussion; the graceful worker-loss and dependency-readiness kind probes proposed here have shipped, while the remaining increment and candidate evidence are still open
>
> **Opened:** 2026-09-13

Related: [roadmap](../ROADMAP.md),
[current-state assessment](../current-state.md),
[testing guide](../contribute/testing.md),
[local end-to-end verification](../design/end-to-end-testing.md),
[verification program](../design/verification-program.md),
[trust harness](../design/trust-harness.md),
[graceful shutdown](../design/graceful-shutdown.md), and the
[Beta readiness record](../deploy/beta-readiness.md).

## Contents

- [1. Decision Question And Recommendation](#1-decision-question-and-recommendation)
- [2. Problem And Current Evidence](#2-problem-and-current-evidence)
- [3. User Outcome And Constraints](#3-user-outcome-and-constraints)
- [4. Goals](#4-goals)
- [5. Non-Goals](#5-non-goals)
- [6. Placement Rule](#6-placement-rule)
- [7. Options And Trade-Offs](#7-options-and-trade-offs)
- [8. Proposed Scope](#8-proposed-scope)
- [9. Delivery Sequence](#9-delivery-sequence)
- [10. Harness And Evidence Contract](#10-harness-and-evidence-contract)
- [11. CI And Release Policy](#11-ci-and-release-policy)
- [12. Acceptance Criteria](#12-acceptance-criteria)
- [13. Risks And Controls](#13-risks-and-controls)
- [14. Open Questions And Decision Evidence](#14-open-questions-and-decision-evidence)
- [15. Likely Destination If Accepted](#15-likely-destination-if-accepted)

## 1. Decision Question And Recommendation

BuildMax already has a broad deterministic end-to-end baseline. The open
question is not whether to add more tests in general. It is:

> Which small set of new end-to-end journeys most reduces the remaining Beta
> risk, and at which boundary should each journey be asserted?

The recommendation is to approve a **risk-driven deployment-lifecycle
increment**, with four rules:

1. Do not expand Portal CRUD and presentation happy paths merely to increase
   browser-test breadth.
2. Keep the shipped graceful worker-loss and dependency-readiness cases as
   regressions, then add Server restart/reconnect, worker write denial, and
   cancellation after partial work.
3. Prove the supported worker contract against candidate artifacts, including
   the negative paths that must fail closed.
4. Bind every new case to one critical journey, one independently observable
   outcome, and one evidence bundle. A test count is not an acceptance metric.

This proposal does not reopen the R0–R2 outcomes already accepted by the
roadmap and verification records. It asks maintainers to accept the scope,
placement, and delivery order of the next implementation increment. If that
shape is accepted, implementation work moves to the backlog and this proposal
is retired.

## 2. Problem And Current Evidence

The existing suite is broad on normal operation:

- the CLI/TUI suite drives the built binary, approvals, denial, session resume,
  parallel tool calls, provider failures, and trace/session persistence in an
  isolated home;
- the Desktop bridge suite covers bound methods, streamed events, approvals,
  history, rewind, fork, and provider failure;
- the Desktop UI suite boots the React application through the real Wails
  development bridge and exercises a small set of local-mode UI flows;
- the Portal suite covers authentication, routing, Space switching and roles,
  Tasks, Continue and Retry, Workflows, files, plugins, administration, audit,
  responsive layouts, accessibility, and dependency presentation states;
- Compose and kind smoke cover ordinary direct and managed execution, storage,
  a real worker, Artifact retrieval, retry, authorization denial,
  cancellation, a Bash confinement probe, graceful worker loss, and runtime
  MySQL/object-storage readiness degradation and recovery.

That baseline leaves a different class of risk. A successful run does not prove
what happens when a process disappears, a dependency refuses an operation, or
a durable result must survive a restart. The roadmap identifies candidate
worker proof, durable reconciliation, and lifecycle recovery as R0–R2. The
The repository now has repeatable kind evidence for the common graceful
worker-loss path and for MySQL and object-storage readiness outage/recovery.
Those are development-environment regressions, not a filled-in candidate
record: the Beta readiness rows remain `Not run`, and worker write denial,
paired restore, upgrade, rollback, and credential rotation still have no such
proof.

One known cancellation limit makes the distinction concrete. The current
deployment smoke stalls the first model call, so it proves that cancellation
reaches a live execution and remains terminal. It cannot prove preservation of
partial output or Artifacts because the run has produced neither before it is
canceled.

The repository also lacks one current verification matrix that maps the V01–V20
journeys to exact automated evidence and explicit gaps. Without it, adding more
tests can improve the count while leaving release risk unchanged.

## 3. User Outcome And Constraints

The essential outcome is:

> An operator can interrupt, lose, restart, or temporarily deprive part of a
> BuildMax deployment and still obtain an honest terminal state, retained
> evidence, and an explicit recovery or retry path without duplicate work or
> phantom data.

The constraints today are:

- Portal, Server, worker, MySQL, object storage, Redis, ingress, and the model
  transport cross different failure boundaries;
- the CLI and Desktop fast suites must remain cheap enough for the ordinary
  test loop;
- the deterministic model harness must remain credential-free and must not
  infer replies from prompts;
- use the lowest owned failure-injection environment that proves the claim. The
  shipped readiness probes require kind because they depend on pod network,
  Service removal, and Kubernetes Jobs; Compose remains suitable for failures
  that do not need those boundaries;
- release-candidate restore, upgrade, rollback, TLS, and credential rotation
  require external, production-shaped dependencies and cannot be claimed by a
  local mock stack;
- R1 Workflow reconciliation now owns authoritative recovery behavior and has
  real-MySQL restart evidence; the remaining end-to-end claim is recovery in
  the deployed multi-replica topology;
- expensive deployment suites remain post-merge, scheduled, or deliberate
  release evidence rather than universal pull-request gates.

## 4. Goals

- Close the highest-risk missing deployment journeys with the fewest new cases.
- Give each case an oracle across public state, durable state, retained
  evidence, and forbidden side effects.
- Keep browser tests focused on facts only a browser can prove.
- Keep deployment smoke focused on process, network, storage, scheduler, and
  worker behavior that lower layers cannot prove.
- Make fault injection deterministic, targeted, and distinguishable from an
  unrelated infrastructure failure.
- Preserve useful partial output, traces, audit records, and valid Artifacts
  without advertising data that was never stored.
- Make source, image, environment, injected fault, and final classification
  visible in retained evidence.
- Keep the suite selection and execution surface under `./make`.

## 5. Non-Goals

- Maximizing the number of Playwright specs or repository-wide coverage.
- Re-testing every handler or store rule through a browser.
- Adding a Portal happy path for every CRUD operation.
- Making random chaos testing part of the first increment.
- Using a real model as the correctness oracle for deterministic lifecycle
  behavior.
- Treating a local kind cluster as Beta qualification evidence for external
  MySQL, S3, TLS, restore, or credential rotation.
- Defining Workflow recovery semantics in test code before the R1 services and
  stores own them.
- Expanding product behavior, such as per-run model selection, solely to make a
  test harness easier to control.
- Replacing focused unit, real-MySQL, component, or handler tests with slower
  end-to-end coverage.

## 6. Placement Rule

Every proposed case must identify the lowest boundary that can prove its unique
claim:

| Claim | Authoritative evidence boundary |
|---|---|
| Pure validation, state transition, authorization rule, or rendering decision | Unit, component, handler, or real-MySQL test |
| Published Portal bundle, browser routing, session restoration, accessibility, or visible recovery state | Portal Playwright against a real deployment |
| Server, worker, storage, scheduler, gateway, or process lifecycle cooperation | Compose deployment smoke or failure suite |
| Ingress, Kubernetes Job lifecycle, pod security context, or cross-replica behavior | kind lifecycle suite |
| External dependency restore, upgrade, rollback, TLS, or credential rotation | Pinned release-candidate qualification |

A new browser case is justified only when removing the browser would leave the
claim unproved. Setup may use public fixture APIs, but the asserted user outcome
must cross the boundary named by the case.

## 7. Options And Trade-Offs

### Option A: Continue Broad Portal Expansion

Add browser cases whenever a page or action is added, including ordinary CRUD
and presentation variants.

This is easy to understand and produces visible coverage. It also makes the
slowest shared suite larger, duplicates component and handler assertions, and
does little for the open worker-loss, dependency-failure, and recovery risks.

### Option B: Add A Large Unified Chaos Suite

Build one runner that randomly kills services, corrupts timing, and exercises
many concurrent journeys across Compose and kind.

This may find emergent defects, but it is a poor first correctness gate. Random
injection makes failures difficult to reproduce and classify, and one broad
runner couples unrelated product boundaries before their individual oracles
are stable.

### Option C: Add Targeted Lifecycle Journeys — Recommended

Introduce a small number of named, deterministic failure controls. Each control
acts at a recorded point, each journey has a bounded terminal expectation, and
each assertion covers both retained evidence and forbidden side effects.

This requires more harness design than another browser test, but it directly
serves R0–R2 and can later become candidate qualification evidence. It also lets
Compose and kind carry different claims without pretending they are
interchangeable.

### Option D: Stop Expanding Until Candidate Qualification

Rely on the current suites and discover remaining gaps during the external Beta
rehearsal.

This avoids speculative harness work, but it pushes repeatable local failures
into an expensive candidate environment. A hard worker-loss or storage-denial
defect found there would have no cheap regression path until after release work
had already stopped.

## 8. Proposed Scope

### 8.1 Candidate Worker Contract

Extend the existing trust and deployment probes only where candidate evidence
is missing:

- a worker with required sandbox enforcement unavailable refuses to execute;
- process limits selected for the worker are present and produce the documented
  diagnostic outcome;
- command and HTTP hook transport follows the supported worker policy;
- resolved stdio MCP fails during assembly before any child process or model
  call, while allowed remote transports remain legible;
- the public listener cannot serve worker routes, and the worker listener still
  requires the run-scoped authority expected by the supported topology;
- TaskRun diagnostics report the actual boundary and MCP treatment rather than
  a requested or assumed one.

Local smoke proves the repeatable mechanisms. The pinned candidate run proves
the immutable images and deployed configuration.

### 8.2 Kind Lifecycle

The first of these two deterministic journeys is partially delivered:

1. **Worker loss:** the shipped kind probe terminates a Kubernetes worker Job
   after claim and before terminal report and proves a durable, diagnosable
   `FAILED` result. Job deletion delivers `SIGTERM`, so this is the graceful
   rollout/eviction/drain path; the silent hard-loss liveness reaper remains
   covered at store level and is not misrepresented as deployed proof.
2. **Server restart and reconnect:** restart the serving path while a direct
   Task or foreground turn is observable. After reconnect, durable state
   reconstructs the same work without duplicate TaskRuns, outputs, Artifacts,
   usage, or message-history writes.

The Workflow variant uses the shipped Server-owned recovery loop and its
real-MySQL contract as authority. The end-to-end case adds only what those tests
cannot prove: deployed worker updates, Server restart, and multi-replica
recovery without duplicate execution.

### 8.3 Dependency Failures

Two targeted controls have shipped in kind because the claim depends on pod
network and readiness behavior:

- temporary MySQL unavailability, covering readiness failure and recovery
  without rebuilding the Server; and
- object-storage read/readiness denial followed by recovery, preserving the
  original bucket.

The following controls remain open:

- object-storage write denial, covering honest run failure and the absence of a
  phantom downloadable Artifact;
- graceful Server shutdown while work is in flight, covering drain behavior and
  restart without a stranded running record.

Each control records when it was armed and released. A test must distinguish an
expected injected refusal from an unhealthy environment discovered before the
injection.

### 8.4 Cancellation After Partial Work

Extend the deterministic model control so one selected run can:

1. produce a known output or Artifact;
2. block at a named later point;
3. receive cancellation; and
4. release cleanly for teardown.

The result must remain `CANCELED`, preserve only evidence actually committed,
leave no advertised missing object, and perform no further tool or model work
after cancellation becomes authoritative.

The control must be keyed to a run-owned opaque identifier rather than model
alias or prompt content. If no safe run-scoped control can be added without
changing product authority, this case pauses and returns to design rather than
adding per-run model selection as test plumbing.

### 8.5 Portal Recovery Presentation

Add browser coverage only for an operator-visible state introduced or found by
the deployment journeys—for example, a Task page honestly presenting lost
worker contact or System Status recovering after a dependency outage. Do not
duplicate the injection itself through Playwright when deployment smoke already
owns it.

### 8.6 Explicitly Deferred Candidate Drills

Paired database and bucket restore, schema upgrade and binary rollback or
destructive-cutover recovery, and credential rotation remain release-candidate
drills. This increment may build reusable evidence collection for them, but a
local passing suite must not mark those Beta rows complete.

## 9. Delivery Sequence

The proposed order follows the roadmap rather than test implementation
convenience:

1. **Map existing evidence.** Create the V01–V20 verification matrix, link the
   exact existing tests, and mark gaps explicitly. This adds no new claim; it
   prevents duplicate coverage and establishes journey identifiers.
2. **Close R0 candidate-worker probes.** Add the repeatable negative controls
   needed to exercise fail-closed worker behavior, then use the same assertions
   in the pinned candidate environment.
3. **Add deployed R1 topology evidence.** Exercise worker updates, reconnects,
   concurrent turns, Redis failure, and the shipped Workflow recovery loop in
   the candidate topology. The existing real-MySQL contracts remain the
   authority for reconciliation semantics.
4. **Add kind lifecycle journeys.** The graceful worker-loss path is shipped;
   Server restart, reconnect, Workflow recovery in the deployed topology, and
   any reproducible silent-hard-loss proof remain.
5. **Add dependency journeys.** The kind MySQL and object-storage readiness
   outage/recovery probes are shipped. Worker object-storage write denial and
   shutdown-under-load remain, with placement chosen by the boundary they need.
6. **Add partial-work cancellation.** Land the minimum run-scoped harness
   capability and the preservation/forbidden-side-effect assertions.
7. **Rehearse the pinned candidate.** Execute the Beta readiness operator,
   failure, restore, upgrade, rollback or cutover, and rotation procedures using
   immutable image digests and external dependencies.

Steps 2–6 should become separate backlog tasks after acceptance. Their ordering
may overlap only when ownership and dependencies do not conflict; acceptance of
this proposal does not silently reorder the maintainer's existing plugin
backlog or recreate the completed Workflow recovery tasks.

## 10. Harness And Evidence Contract

### 10.1 Fault Controls

Every fault control must provide:

- a stable name and version;
- an explicit arm point and release or teardown operation;
- the target run, process, or dependency;
- a bounded deadline;
- an observable marker proving the fault occurred;
- idempotent cleanup; and
- a failure when the test completed without exercising the armed fault.

Controls may delay, disconnect, terminate, pause, or deny. They must not inspect
prompt content and decide what the product probably intended.

### 10.2 Assertions

Each lifecycle journey asserts:

1. the public and operator-visible state;
2. the authoritative durable state;
3. trace, audit, usage, logs, and Artifact evidence that apply;
4. the documented terminal-state deadline;
5. forbidden duplicates, phantom Artifacts, hidden retries, post-cancel work,
   cross-Space exposure, and stranded running records; and
6. the supported retry or recovery path.

`err == nil`, HTTP acceptance, or one terminal status alone is insufficient.

### 10.3 Evidence Bundle

Scheduled lifecycle runs should emit the existing verification-program contract
under `.artifacts/verification/`. At minimum, a manifest records:

- source commit and dirty state;
- image digests when images are involved;
- owned or attached environment and dependency versions;
- journey and scenario versions;
- injected fault, target, and timestamps;
- terminal result and failure classification;
- retained evidence paths and cleanup result; and
- explicit skips and untested limits.

Successful runs need the manifest too. Uploading traces only on failure cannot
later prove which immutable candidate or environment passed.

## 11. CI And Release Policy

- Fast CLI, Desktop bridge, component, handler, and real-MySQL tests keep their
  existing pull-request placement.
- Portal browser tests remain non-retrying deployment evidence. New cases join
  them only when they prove a browser-specific recovery outcome.
- Compose failure and kind lifecycle suites run after merge, on schedule, and
  by manual dispatch. They do not become universal pull-request gates.
- A failed scheduled lifecycle case remains visible until classified as
  product, harness, or infrastructure failure. A diagnostic rerun does not
  erase the first result.
- `./make e2e all` must not be treated as complete release evidence while it
  omits kind, managed paths, Desktop UI/native packaging, real MySQL, and
  candidate drills. Either rename its documented claim or introduce a distinct
  intent-level command that reports every included, skipped, and external-only
  scope.
- Beta qualification consumes pinned candidate evidence; it does not infer a
  pass from the latest successful `main` workflow.

## 12. Acceptance Criteria

The proposed increment is complete when:

Current progress satisfies only the graceful worker-loss and dependency-
readiness portions below. It does not complete this proposal or any candidate
row.

- the verification matrix maps every V01–V20 journey to exact evidence or an
  explicit gap;
- candidate worker probes exercise Bash enforcement, process limits, hook and
  MCP treatment, worker API isolation, and truthful diagnostics against the
  supported profile;
- a hard-killed worker produces a bounded, diagnosable terminal run and an
  explicit successful Retry without hidden or duplicate execution;
- Server restart and reconnect preserve one authoritative work history without
  duplicate runs, output, Artifacts, usage, or messages;
- MySQL and object-storage denial cases expose honest readiness/status, avoid
  phantom data, and recover without rebuilding or rewriting successful state;
- cancellation after partial work preserves committed evidence and performs no
  later work;
- every injected fault proves that it occurred, is cleaned up, and leaves a
  redacted source- and environment-bound manifest;
- the new suites remain within their documented duration budgets, with any
  exception justified by a candidate-only boundary;
- no new Portal test merely repeats a component, handler, or store assertion;
  and
- the pinned-candidate Beta readiness rows remain open until an operator runs
  and records the external qualification exercises.

## 13. Risks And Controls

| Risk | Control |
|---|---|
| Fault injection becomes flaky timing orchestration | Arm a named control, wait for its observable marker, use bounded polling, and fail if the fault was not exercised |
| Tests add product-only control surfaces | Compile or deploy controls only in test support and smoke overlays; production manifests expose none |
| One global model stall affects unrelated runs | Prefer run-scoped opaque controls; serialize only the narrow case when isolation is impossible |
| Browser suite runtime keeps growing | Require a browser-only claim and use fixture APIs for unrelated setup |
| E2E defines behavior before domain ownership exists | Require unit/real-MySQL authority first, especially for Workflow recovery |
| Local smoke is mistaken for release proof | Record environment class and keep external Beta rows explicitly open |
| Retained logs expose credentials or authored content | Reuse redaction boundaries, keep bundles private by default, and test secret-like query and header removal |
| A combined command hides skipped scopes | Emit explicit run, skip, external-only, and failure results per suite |

## 14. Open Questions And Decision Evidence

The maintainers should decide these before implementation tasks are admitted:

1. Should the first accepted slice include the verification matrix, or should
   that matrix land independently as already-approved verification-program
   maintenance?
2. Should the remaining worker-write and shutdown failure cases extend
   `deployment-smoke.yml` as another matrix cell, or run in a separate workflow
   with a different duration and ownership policy? The readiness outage probes
   already belong to kind because they assert pod-network and Service behavior.
3. What is the smallest safe run-scoped model control identifier available
   before the worker begins model execution?
4. Which Server restart journey is first: a direct Task, a foreground
   Conversation turn, or both? The choice should follow the larger unproved
   durability risk, not surface symmetry.
5. Which worker-contract checks can be deterministic local probes, and which
   require operator inspection of the pinned candidate pod and network policy?
6. What duration budgets are realistic for hard worker loss and dependency
   recovery using the current heartbeat, reaper, and readiness intervals?

Evidence needed to decide includes one measured prototype of a targeted fault
control, current scheduled-workflow runtime and flake history, the shipped R1
reconciliation contract and restart test, and a dry run of the evidence
manifest against both Compose and kind. If the prototype cannot distinguish an
injected fault from environment instability, the harness design is not ready
for backlog decomposition.

## 15. Likely Destination If Accepted

Acceptance should not leave this proposal as a second source of truth.

- Durable placement and assertion rules move into
  [local end-to-end verification](../design/end-to-end-testing.md).
- Journey coverage and evidence requirements move into the
  [verification program](../design/verification-program.md).
- Accepted priority or sequencing changes appear in the
  [roadmap](../ROADMAP.md).
- Independently executable slices enter [the backlog](../backlog/README.md), in
  maintainer-selected order and without duplicating existing R1 tasks.
- Contributor commands and prerequisites update the
  [testing guide](../contribute/testing.md) only when they exist.
- Candidate outcomes are recorded in the
  [Beta readiness record](../deploy/beta-readiness.md).

The proposal and its index entry are then deleted; git history preserves the
discussion.
