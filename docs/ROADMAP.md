# BuildMax Roadmap

> **简体中文：** [阅读中文镜像](zh-CN/ROADMAP.md)
>
> **Audience:** users, operators, and contributors · **Status:** current — Alpha
> **Last reviewed:** 2026-09-13

BuildMax is an open-source Agent runtime for local work and private Space
deployment. CLI/TUI, Desktop, and Server/Portal use the same Go Agent Core.
You can use the local tools without deploying a Server.

**The next milestone is a dependable private-deployment Beta:** an operator can
deploy, run work, understand failures, and recover using documented procedures.
BuildMax has not passed that gate. No Beta release date is committed here;
release readiness depends on evidence, not the number of features implemented.

## At A Glance

| Horizon | User outcome | Current position |
|---|---|---|
| Available in Alpha | Run Agents locally or in a private Space, with managed models, background and scheduled work, shared results, and diagnostic traces. | Implemented capabilities have different limits; see the [current-state assessment](current-state.md) and [user manual](../manual/introduction.md). |
| Next: private-deployment Beta | Trust the worker boundary, supported Server topology, persistence, and recovery procedures. | The worker boundary contract (R0) and durable-state correctness (R1) are closed and evidenced, R1 including a deployed cross-replica coordination exercise; long-running recovery (R2) and candidate operating evidence for one immutable deployment (R3) remain open. |
| Later: evidence-led expansion | Richer Workflows, integrations, and local experiences that solve demonstrated user problems. | Candidate directions, not release commitments. |

This roadmap owns priority, sequencing, and release gates. Implementation
evidence belongs in [current state](current-state.md), design rationale in
[design records](design/README.md), and release proof in the
[Beta readiness record](deploy/beta-readiness.md). Approved work decomposed into
ready-to-execute units lives in the [backlog](backlog/README.md). “Implemented”
does not mean qualified in a real deployment. Earlier P0–P4 phase names in design records are
historical capability groupings; the R0–R5 order below governs current work.

## Active Priority Order

R0 closed the supported worker contract and R1 closed durable state correctness.
R2 closes the remaining release-blocking engineering gap: bounding long-running
operation and recovery. R3 then qualifies one immutable candidate through the
documented operator journey.
R4–R5 are post-Beta, evidence-led work rather than prerequisites hidden inside
the release path. These are priorities, not claims that someone is currently
assigned to every item.

Each priority below opens with a machine-readable `**Status:**` line —
`open`, `in-progress`, `candidate-proof-remains`, or `done` — that `./make board`
reports and an architecture test requires. It is the one-word position; the
prose that follows still carries the real detail and the `Done when:` gate.

### R0. Close The Supported Worker Contract

**Status:** done

**Done.** Official worker images select and probe the worker sandbox baseline;
Bash confinement, process limits, hook transport policy, worker API isolation,
trace boundary presentation, and resolved plugin presentation are implemented.
The supported unattended-worker profile disables stdio MCP fail-closed: a
resolved stdio server fails the run during assembly, before its command runs,
and the treatment is legible in TaskRun diagnostics beside the run boundary. Each
control is mapped to its evidence in
[trust harness](design/trust-harness.md) §6.1: Bash confinement and worker API
isolation are proven through the deployed worker path (the deployment smoke's
`assertWorkerSandboxConfines` and the kind worker-API boundary check), and the
stdio MCP, process-limit, and hook controls are proven where each executes on
that same proven worker profile.

**Evidence recorded:** no stdio MCP child runs outside the boundary claimed by
the supported worker profile; unavailable required enforcement fails closed; the
actual boundary and MCP treatment are legible in TaskRun diagnostics; and
deployment evidence covers those claims at the level trust harness §6.1 records.
Pod-wide destination control and an outer runtime sandbox remain accepted
first-Beta limits, not R0 gates. Full immutable-candidate qualification through
the operator journey is R3 and the [Beta readiness record](deploy/beta-readiness.md),
which stay open; R0 closing the worker contract does not assert them.

Design: [trust harness](design/trust-harness.md),
[worker API network boundary](design/worker-api-network-boundary.md), and
[sandbox boundaries](design/sandbox-boundaries.md).

### R1. Close Durable State Correctness

**Status:** done

**Done.** Redis mode supplies shared streams, connection events, and Conversation
turn leases, and the reference manifests run two coordinated Server replicas.
Workflow run and step-run transitions use guarded compare-and-set writes, with
failed-step finalization made atomic. Message-history writes enforce lease
fencing, so a stale writer is rejected rather than corrupting a conversation. A
task is no longer refused for the tokens its generated title spent, so a space
under its run limit no longer strands a Conversation on a token refusal. The
linear Workflow precursor is durably reconciled: a Server-owned recovery loop
sweeps due runs from stored state, so a lost callback or a restart no longer
strands one, proven against real MySQL.

**Evidence recorded:** the deployed candidate exercise the bar required now
exists. [`kindCoordinationProbe`](../tools/mk/coordination_probe.go), run by
`./make kind smoke` against the two-replica + Redis kind stack, drives the two
replicas by hand and proves the delivery, serialization, and recovery the bar
names: a task's worker output reaches a stream opened on either replica; a turn
holding the conversation lease on one replica keeps a turn on the other replica
from reaching the model until it releases; and both recover after the Redis pod
restarts. Stale writers cannot commit (fencing), refused work leaves no orphan
record (the title-token fix), and persisted work converges after interruption
without duplicate execution (the Server-owned reconciliation loop, proven against
real MySQL). Propagating a lost lease to the running turn and a Server
rolling-update drill remain open, but are R3 deployment-qualification concerns
rather than durable-state-correctness gaps; R1 closing does not assert full
immutable-candidate qualification, which stays with R3 and the
[Beta readiness record](deploy/beta-readiness.md).

Design: [Server coordination](design/server-coordination.md) and
[Workflow runtime](design/workflow-runtime.md).

### R2. Bound Long-Running Operation And Recovery

**Status:** in-progress

**Test infrastructure implemented; lifecycle evidence remains.** The MySQL
scope runs on pull requests and covers critical authorization, TaskRun state,
checkpoint, Artifact, and Workflow transition behavior. Deployment smoke covers
ordinary execution, cancellation, and now worker-loss recovery: a run whose
worker is deleted mid-execution settles to a diagnosable terminal FAILED and
stays retrievable. That drill exercises the graceful-termination path a rollout,
eviction, or drained node takes; the silent hard-loss path the liveness reaper
settles cannot be reproduced from the deployment and is covered at the store
level instead. A server now expires persisted run traces on an operator-set
window, defaulting to keep-forever and recording each prune; no candidate has
yet proved dependency denial, paired restore, schema upgrade, binary rollback,
or credential rotation.

**Next:** the remaining lifecycle evidence — dependency denial, paired
database-and-bucket restore, a schema upgrade and binary rollback fixture, and
credential rotation — several of which land as the R3 operator journey.
Real-MySQL coverage for [quota windows](https://github.com/icloudbb/buildmax/issues/498)
and cross-Space store scoping, and the deployed worker-loss drill, are done.
Retire plans for removed mechanisms, including the old result-delivery queue,
rather than recreate them for a checklist.

**Done when:** a long-lived deployment has bounded or explicitly capacity-planned
trace storage, critical persistence paths have real-database regression tests,
and the candidate has durable evidence for failure, restore, upgrade, rollback,
and rotation behavior.

Design: [verification program](design/verification-program.md) and
[end-to-end testing](design/end-to-end-testing.md).

### R3. Qualify One Private-Deployment Candidate

**Status:** candidate-proof-remains

**Product path implemented; the evidence record is empty.** Account bootstrap,
login-code recovery, Space membership, managed models, Agent and Workflow runs,
artifacts, traces, usage, audit, Compose/kind, and the production reference all
exist. None of that substitutes for exercising the immutable Server, worker,
and Portal artifacts proposed for release with external dependencies.

**Next:** pin the candidate image digests and have an operator who did not build
the features perform the documented account, Space, execution, diagnosis,
failure, restore, upgrade, rollback, and rotation journeys. Fix only gaps that
the journey demonstrates. Transactional authority audit, admin CLI Session
parity, quota-tier assignment, and richer runtime metadata remain proposal work
unless they block this outcome.

**Done when:** every required row in the Beta readiness record has durable
evidence, failures and accepted limits are explicit, and the qualification
operator, engineering owner, and release owner sign the decision.

Design: [Space membership lifecycle](design/space-membership-lifecycle.md) and
[Space governance](design/space-governance.md).

### R4. Measure Product Quality Beyond The Beta Gate

**Status:** in-progress

**Post-Beta; framework implemented and coverage limited.** Three
BuildMax-owned tasks and a one-task external canary establish the evaluation
path, not platform-wide reliability or a Terminal-Bench score. Public benchmark
breadth is not a prerequisite for qualifying the private-deployment contract.

**Next:** expand product-owned local, worker, Conversation, trust-boundary, and
deployment scenarios from observed failures. Collect performance and soak
evidence separately. Run the pinned Harbor canary before the full benchmark
protocol; publish a score only with the completed protocol and its conditions.

**Done when:** product changes can be compared across representative,
reproducible scenarios, with uncertainty, failures, and limits reported
explicitly.

Design: [evaluation system](design/evaluation-system.md).

### R5. Deepen Product Capability From Evidence

**Status:** open

**Later; scope depends on demand and qualification results.** Candidate work
includes durable Workflow reconciliation and typed dataflow, real channel
adapters, executable Space plugins, Portal performance, Desktop automation,
and throughput. Local CLI/TUI and Desktop improvements remain welcome when they
address concrete problems; the Beta focus does not make Portal the only product.

Conditional security hardening also belongs here rather than in the Beta gate:
Pod-wide destination policy, a dedicated egress proxy, and an outer runtime such
as gVisor should be selected only when deployment evidence or a stronger threat
model requires them. Reopen that work for untrusted multi-tenant operation,
untrusted repositories, or workers holding high-value credentials; do not make
a particular CNI or proxy an unconditional BuildMax dependency without that
evidence.

Corporate SSO has an accepted direction in the
[enterprise identity and access](design/enterprise-identity-and-access.md) design
record (OIDC, external-identity linking, and a native-versus-SSO posture), but no
ordered R5 slice and nothing implemented. Each build slice waits on the
per-deployment inputs that record names — chiefly a target provider, an
offboarding bound, the JIT domain policy, and whether native connected clients
are required.

After the Beta gate, evaluate and deliver the previously unplanned local and
plugin follow-ons in this order. Each step still needs its stated evidence; an
ordered place here is not permission to skip a proposal's acceptance decision.

1. Decide the remaining [Local Issue work bridge](proposals/local-issue-work-bridge.md)
   Phase 1 contract, then complete the durable Issue-to-Session link and local
   result projection if accepted. Evaluate its decomposition and governance
   phases only after the receive/work/return path has adoption evidence.
2. Add [Space Secret](design/space-secrets.md) credential-file delivery before
   widening plugins that commonly need credential files.
3. Add [executable Space plugin content](design/plugin-space-distribution.md)
   only after R0 has a supported, confined hook/MCP process and network boundary
   — the unattended-worker profile disables stdio MCP today rather than confining
   it — while preserving release eligibility, exact pins, and run-scoped
   materialization.
4. Add Task-scoped autonomous plugin acquisition only after fixed plugin
   environments and executable distribution are proven. It creates a later
   TaskRun and never hot-loads a running process.
5. Consider short-lived credential exchange, external Secret providers, and
   workload identity in that order, and only for a concrete provider and
   operator journey.

Workflow expansion starts with reconciliation and typed dataflow before graph
breadth. A provider-neutral structured-output contract in the shared runtime is
a prerequisite for typed routes, planners, evaluators, and richer Task results.
Channel names or partial adapters do not count as delivered integrations.

Design: [Workflow runtime](design/workflow-runtime.md) and
[orchestration and continuity decisions](design/orchestration-and-continuity-decisions.md).
The ordered follow-ons above are specified by the linked records rather than
being duplicated here.

## Beta Gate

The first Beta targets **one trusted Space on a private network**. It is not a
claim of public multi-tenant readiness. Qualification uses the same immutable
Server, worker, and Portal artifacts proposed for release.

| Required proof | Acceptance outcome |
|---|---|
| Candidate deployment | Deploy pinned image digests with external MySQL, S3, and TLS; record versions, configuration, operator, and date. |
| Execution boundary and topology | Prove the supported sandbox, resource limits, hook/MCP treatment, and Server topology. Unrestricted Bash with a recorded `none` boundary does not pass, and stdio MCP must be disabled unless its child process is confined by the declared worker boundary. Record residual Pod-wide egress and storage-credential limits explicitly. |
| Persistence and failure behavior | Attach passing critical MySQL tests; exercise cancellation, worker loss, database outage, and storage denial. Runs reach documented terminal states and retain available results and diagnostic evidence. |
| Recovery and maintenance | Restore the database and bucket together; exercise a schema upgrade and binary rollback, plus credential rotation. Record recovery time, data checks, and accepted loss. |
| Operator journey | An operator who did not implement the feature can sign in, execute and retry work with a managed model, and diagnose results from TaskRun, Artifacts, traces, usage, and audit history. |
| Release verification | Attach current CI, direct and managed Compose/kind smoke, Portal browser E2E, archive verification, image scans, SBOMs, and provenance. |

Engineering closes the supported worker contract first, then the remaining
state-correctness and long-running recovery gaps. External candidate
qualification follows and ends with a signed readiness record. Broader model
evaluation and public benchmarks do not block that decision.

The [Beta readiness record](deploy/beta-readiness.md) holds the detailed
procedure and evidence. Passing unit tests or local smoke does not replace
candidate restore, failure, and upgrade exercises. Desktop polish, SSO,
executable Space plugin content, additional providers, and general durable
Session sync are outside the first Beta gate.

## How To Help

Start with [CONTRIBUTING.md](../CONTRIBUTING.md) and the
[testing guide](contribute/testing.md). You do not need to tackle an entire
priority to make a useful contribution.

| If you want to… | A useful contribution |
|---|---|
| Make a first contribution | Follow a local setup or operator journey and improve unclear documentation; browse [good first issues](https://github.com/icloudbb/buildmax/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22). |
| Improve reliability | Reproduce a failure and add a focused regression test, especially for the R1–R2 state and recovery paths. |
| Help qualify private deployment | Run a documented deployment journey and report versions, topology, expected/actual behavior, and redacted evidence. |
| Shape a feature | Describe the user problem, a concrete example, and why existing behavior is insufficient in [Discussions](https://github.com/icloudbb/buildmax/discussions). |

Search [existing issues](https://github.com/icloudbb/buildmax/issues) before
opening a bug or implementation proposal. For substantial work, link the
relevant R priority and design record and discuss scope before implementing it.
An entry here does not imply an assigned owner or an open implementation issue.

Maintainers should update this page when a priority, completion criterion, or
release gate changes, and keep the Chinese mirror in sync. Routine implementation
details belong in the linked evidence and issue, rather than growing this page
into another implementation inventory.
