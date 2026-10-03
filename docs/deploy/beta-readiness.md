# Beta Readiness Record

> **简体中文：** [阅读中文镜像](../zh-CN/deploy/beta-readiness.md)
> **Audience:** operators and release managers · **Status:** current — not qualified

BuildMax has **not passed the Beta gate**. This document defines the supported
profile for the first private-deployment Beta, the procedure that qualifies one
immutable candidate for that profile, and the evidence record for the decision.
Automated tests show that a candidate is ready to exercise; only results
produced with the same immutable artifacts proposed for release count as
qualification evidence.

The first Beta is a promise about a bounded deployment profile, not every
implemented feature. Core-profile items below are release blockers. Local
surfaces keep their own release regression gates. Experimental profiles may
ship beside the candidate, but they neither inherit the Beta claim nor block it
when they are disabled as recorded below.

Do not change the status above to `qualified` until every required item passes,
every evidence link is durable and readable by the release space, and the final
decision is signed. A failed item stays in the record with its diagnosis and
the later successful rerun.

## Qualification Contract

The first Beta serves **one trusted organization on a private network**. The
qualification environment has at least two Spaces and two users with different
roles, so the Space authorization boundary is exercised rather than assumed.
This is not a claim of public, untrusted multi-tenant readiness.

### Core Private-Deployment Profile

The candidate qualifies this profile as one system:

- Portal, two coordinated Server replicas, Redis, Kubernetes worker Jobs,
  external MySQL and S3-compatible storage, ingress, and TLS;
- native password and login-code authentication, durable sessions, System
  Administration, Space membership, account disablement, and owner recovery;
- managed models, quotas, Conversations, Issues, Agents, direct Tasks,
  Continue, Retry, cancellation, deferred `AskUser`, Artifacts, Space files,
  workspace checkpoints, traces, usage, audit, and runtime diagnostics;
- published graph Workflows with parallel nodes, input bindings, structured
  output, durable human requests, retry/timeout policy, run cancellation,
  reconciliation, and failure/cancellation drain;
- Agent and Workflow Schedules;
- Space Secret environment delivery and redaction;
- the implemented non-executable Space Plugin profile: exact-release
  activation plus skill/subagent materialization; and
- authenticated inbound webhooks.

If one of these capabilities is removed from the candidate profile, update the
support matrix and user documentation before qualification. Silently omitting
a visible Beta capability from the journey does not narrow the promise.

### Release Regression Profiles

CLI print mode, TUI, local sessions and traces, and Desktop — including its
signed-in Issues bridge — are not hosted by the private-deployment candidate.
Their documented release checks still block the release because they ship from
the same revision and share the Agent Core, but they do not need to be repeated
inside the Kubernetes operator journey.

### Experimental Profiles

Remote Control, Telegram, Portal OIDC, local app connectors, remote MCP, and
Browser capability are experimental for this decision. Record whether each is
disabled or enabled. An enabled experimental profile needs its own evidence
and accepted limits, but a result does not qualify it as Beta. A disabled
profile must not weaken or disrupt the core profile.

### Outside The First Beta

The first Beta does not promise direct public-internet exposure, untrusted
multi-tenant hosting, multi-region operation, automatic re-dispatch of a lost
worker run, binary rollback, Pod-wide worker egress allow-listing, an outer
runtime such as gVisor, MFA, qualified SSO, Workflow conditions/loops/manual
approvals, executable Space Plugin hooks or MCP servers, stable HTTP APIs,
stored-shape compatibility, or signed/notarized Desktop installers.

## Candidate

Fill this table before starting. Tags alone are not immutable evidence.

The first exercise ran on DigitalOcean on 2026-10-02 and 2026-10-03. It began on
v0.2.0-alpha.18; every defect it found was fixed and released as alpha.19,
alpha.20, and alpha.21 in turn, and each fix was rerun on the deployment that
carried it. The table records alpha.21, the last of those releases. Most gates
ran only on alpha.18, so this evidence does not yet meet the same-artifact rule
above for alpha.21.

| Field | Recorded value |
|---|---|
| Version and commit | v0.2.0-alpha.21 @ `d7c161b8`; exercise began on v0.2.0-alpha.18 @ `ee8fbf2f` |
| Server image digest | `ghcr.io/icloudbb/buildmax@sha256:04e81f59e8e27cf5a63bb79abdbcc65a54c001073f03ae494aafffa099383853` (alpha.18: `sha256:2014c71b…`) |
| Worker image digest | Same image as the Server |
| Portal image digest | `ghcr.io/icloudbb/buildmax-portal@sha256:fabed2b4d46aa0d74d8382028cdf538d960361b31f4e01dad007d90855134e91` (alpha.18: `sha256:0219f60f…`) |
| Enabled/disabled feature manifest | Signup off; Remote Control, Telegram, OIDC, remote MCP, and browser tools not configured; worker profile refuses stdio MCP and hook- or MCP-bearing Plugin releases |
| Operator | Project owner (@gougoujiang); journeys driven by the implementing agent with probe scripts, not by an independent operator |
| Exercise date and environment | 2026-10-02 to 2026-10-03, DigitalOcean `sgp1`, deployed with `./make ocean` ([DigitalOcean](digitalocean.md)) |
| Kubernetes version and distribution | DOKS 1.36.3-do.5, two `s-2vcpu-4gb` nodes |
| CNI and enforced NetworkPolicy behavior | Cilium v1.19.3 (DigitalOcean-managed); Server-ingress, Redis, and worker-egress policies enforced and probed |
| Server replica count and coordination mode | Two replicas on separate nodes with a PodDisruptionBudget; Redis coordination |
| Redis product and version | `redis:7.4.11-alpine`, one in-cluster replica |
| MySQL product and version | DigitalOcean Managed MySQL 8.4.8 over TLS with the CA verified |
| S3 product, version or service, and region | DigitalOcean Spaces, `sgp1`, reached through the VPC endpoint |
| TLS termination and ingress | Caddy 2.10.2 with Let's Encrypt behind a DigitalOcean TCP load balancer with a CIDR allow-list |
| Managed provider, protocol, and model alias | OpenRouter, OpenAI chat completions, `openai/gpt-5.6-luna` as catalog model GPT-5.6 Luna; workers call it through the managed gateway |
| Worker sandbox, seccomp, and AppArmor profile | bubblewrap 0.12.0 with its own network namespace; Localhost seccomp `buildmax/worker-bwrap.json`; AppArmor `Unconfined`; root plus `SYS_ADMIN` and `NET_ADMIN`, all dropped before Bash |
| Current KEK id | `file:root:2`, rotated from `file:root:1` during Q6 |
| Trace, audit, Artifact, and checkpoint retention | Audit and traces kept indefinitely; deleted Artifact bytes reclaimed on the hourly sweep; checkpoint orphan sweep hourly; finished worker Jobs removed after 1h |
| Expected users, Spaces, and concurrent runs | About 10 accounts, 6 shared Spaces plus personal Spaces, up to 3 concurrent worker Jobs |
| Target RPO and RTO | RPO: the last `mysqldump --single-transaction` snapshot; RTO: 10 minutes to the first Artifact with a matching checksum (measured 237s) |
| Configuration snapshot, with secrets redacted | Rendered `server.yaml` archived with the local evidence bundle; template `deployment/ocean/buildmax.yaml.tmpl` |
| Starting schema/commit and supported upgrade or clean-install path | Clean install on alpha.18, then in-place upgrades to alpha.21; upgrade drill from alpha.16 through a real schema change |

## Accepted Limits

Record that the operator and participants accepted all of these limits:

- [ ] The deployment is not exposed directly to an untrusted public network.
- [ ] Official worker images select and probe the strict OS sandbox baseline;
  the candidate demonstrates confined Bash. Stdio MCP is disabled in the
  supported worker profile unless its child process is confined by that
  declared boundary. The native worker pod uses root plus `SYS_ADMIN` and
  `NET_ADMIN` with the documented seccomp/AppArmor profile, and `bwrap` drops
  every capability before Bash runs; an outer runtime such as gVisor is not
  required for this Beta.
- [ ] The sandbox confines Bash writes to the run workspace, not its reads: Bash
  can read the pod's read-only filesystem, including the mounted server
  configuration, which must therefore hold no credentials. Bash does not inherit
  the worker's own environment, and the worker marks itself non-dumpable, so the
  re-bound container `/proc` does not expose it through `/proc/<pid>/environ`
  either. Reserved `BUILDMAX_*` environment names cannot be Secret-grant targets.
- [ ] General worker egress has no enforced Pod-level destination allow-list: the
  worker process itself reaches object storage and the Server. Bash under any
  network tier but `open` runs in a network namespace of its own whose only way
  out is the sandbox proxy, which enforces the tier; under `open` it shares the
  pod's network. The worker-port `NetworkPolicy` restricts control-channel
  ingress; it does not restrict outbound traffic.
- [ ] Storage credentials or projected storage identity are available to the
  worker because it reads and writes run state and Artifacts directly.
- [ ] Hooks fail open as documented, and the worker profile disables
  unsupported executable Plugin and stdio MCP content rather than claiming it
  is confined.
- [ ] SSO, multi-region operation, automatic re-dispatch of lost runs, binary
  rollback, and the experimental profiles above are not part of this Beta.
- [ ] HTTP and configuration compatibility remain Alpha contracts; rollback
  means restoring the paired pre-upgrade database and bucket with matching
  binaries.

## Q0. Scope And Preflight

- [ ] Pin all three candidate image digests and archive the rendered deployment
  configuration and enabled/disabled feature manifest with secrets redacted.
- [ ] Deploy two Server replicas with Redis coordination, external MySQL and
  S3 over TLS, and a CNI that enforces the candidate's NetworkPolicies.
- [ ] Create at least two Spaces and two users with different roles. Include a
  personal and a shared Space if the candidate exposes both.
- [ ] Record the worker CPU, memory, and ephemeral-storage requests and limits.
  Deliberately supply one invalid quantity and record that the candidate
  refuses to start and names the key.
- [ ] Record the intended user, Space, and concurrent-run envelope, plus an RPO
  and RTO that the restore exercise will measure.
- [ ] Record and exercise trace, audit, Artifact, and checkpoint retention and
  capacity policy. Keep-forever settings require explicit volume sizing,
  monitoring, and an operator cleanup procedure.
- [ ] Take a coordinated database and bucket backup and record their identifiers
  before any upgrade exercise.
- [ ] Record the candidate's `./make check ci`, MySQL, Windows, CLI/Desktop,
  direct and managed Compose/kind smoke, Portal browser E2E, Desktop package,
  and release-archive verification URLs.
- [ ] Record Server, worker, and Portal image scan results, SBOM locations, and
  provenance-attestation verification results.

## Q1. Identity, Authorization, And Governance

- [ ] Bootstrap the System Administrator, keep signup disabled, create or
  invite the two users, and exercise password and single-use login-code sign-in.
- [ ] Prove refresh, logout, administrator Session revocation, and absolute
  Session expiry through documented user and operator surfaces.
- [ ] In two Spaces, prove member/owner access and cross-Space refusal for
  Issues, Tasks, Workflows, Artifacts, Secrets, and Plugin activations.
- [ ] Remove a member and prove that new work, Schedule firing, dispatch, and a
  worker fetch re-check eligibility and fail closed.
- [ ] Disable an account after reading its deactivation impact. Confirm its
  Sessions are revoked, in-flight runs are canceled, Schedules are paused, and
  repeating the cleanup is safe.
- [ ] Recover a shared Space whose owners are all disabled without exposing
  Space content or allowing recovery of a personal Space.
- [ ] Exercise one quota refusal and prove that it leaves no orphan Task or
  TaskRun and that the usage view explains the refusal.
- [ ] Confirm that every sensitive action above creates the documented audit
  event and that System Administration exposes no Space content beyond its
  documented authority.

## Q2. Core Product Journeys

Use documented UI and operator surfaces. The person performing these journeys
must not need source-code knowledge.

- [ ] Run a foreground Conversation and use it to create background work.
- [ ] Run a direct Agent Task through a managed model in a Kubernetes worker
  Job. View its streamed and durable result, trace, managed-call ledger, usage,
  audit, workspace checkpoint, and downloadable Artifact.
- [ ] Continue the Task and prove it restores the latest workspace and Session;
  Retry the Task's latest run and prove it uses that run's original base
  rather than its result. Each creates a new TaskRun with its own evidence
  rather than mutating history.
- [ ] Cancel a running Task and confirm its partial output, Artifact, and
  checkpoint semantics match the documented contract.
- [ ] Let a worker run call `AskUser`, confirm the Task says it needs an answer,
  answer through Continue, and verify that the successor run receives the
  answer while the original run remains immutable.
- [ ] Create an Issue with Owner and Executor in one request, run its executor,
  and follow the result, status, and discussion back to the Issue.
- [ ] Publish and run a Workflow with a parallel branch, a JSON-pointer binding,
  structured output, and an authoritative result. Exercise a `human_input` node
  and an Agent node that ends on `AskUser`; answer one, decline or expire one,
  and prove no worker is held while the run waits.
- [ ] Exercise Workflow per-attempt retry/backoff, node timeout, run deadline,
  and whole-run cancellation. Then fail one node and prove admitted siblings
  drain, pending nodes block, and restart recovery converges without duplicate
  Tasks or attempts beyond the published policy.
- [ ] Fire both an Agent Schedule and a Workflow Schedule. Restart the Server
  around a due time and prove one catch-up, no duplicate fire, and a visible
  pause reason after repeated failure or creator ineligibility.
- [ ] Store a Space Secret, grant one item to an Agent, use it in a worker, and
  prove the value is absent from trace, stream, tool results, and logs.
- [ ] Activate a Plugin release containing only the supported skill/subagent
  profile, select it on an Agent, and prove the worker materializes the exact
  version and digest recorded on the run. Activating a release that
  contributes hooks or MCP servers must be refused.
- [ ] Send an authenticated inbound webhook and prove the Conversation turn
  runs as the webhook key's owner and remains Space-scoped.
- [ ] Use Administration to find intentionally stalled and failing work, name
  the `failure_class`, identify who acts next, and reach the underlying run
  without reading unrelated Space content.

## Q3. Execution And Secret Boundaries

- [ ] Inspect a live worker Job. Record its read-only root filesystem, dropped
  capabilities (all but `SYS_ADMIN` and `NET_ADMIN`, none reaching Bash),
  seccomp and AppArmor profiles, absent
  service-account token, effective CPU/memory/ephemeral-storage resources,
  minimized environment, and per-run credential.
- [ ] Prove Bash cannot write outside the run workspace, does not inherit the
  worker's credentials, and that the trace reports the actual sandbox boundary
  rather than a configured intent.
- [ ] Prove the public Service exposes no worker routes, an unlabelled pod is
  denied by the worker `NetworkPolicy`, and a labelled worker uses the internal
  TLS listener.
- [ ] Prove a run token cannot access another run, cannot act before claim or
  after terminal state, and grants only the documented worker routes.
- [ ] Prove a Plugin release with executable content is refused at activation,
  and that a stdio MCP server resolved in the worker profile from any layer,
  such as the workspace's `.buildmax/mcp.json`, fails assembly before a command
  or model call rather than silently running outside the boundary.
- [ ] Prove Space Secret values are redacted across trace, stream, tool results,
  and retained diagnostics while the consuming worker can use them.
- [ ] Let the finished-Job TTL delete the Job and pod; confirm the TaskRun,
  trace, checkpoints, result, and Artifacts remain retrievable.

## Q4. Durable And Distributed Correctness

- [ ] Drive the two Server replicas directly and prove cross-replica Task
  streaming, connection events, and Conversation turn serialization.
- [ ] Let one replica lose its Conversation lease and prove a stale writer
  cannot append behind the new holder.
- [ ] Restart Redis and prove streams, leases, and normal service recover
  without durable-state loss or a silent fallback to local coordination.
- [ ] Roll the two Server replicas. `/readyz` must enter draining before a pod
  exits; accepted work finishes or reaches its documented terminal state, and
  Workflow reconciliation and Schedules resume without duplicate execution.
- [ ] Exercise concurrent Continue, Retry, cancel, Workflow reconciliation,
  and Schedule claims at the intended concurrency envelope. No illegal state,
  duplicate durable execution, or unexplained orphan record may remain.
- [ ] At the end of the qualification window, every old `PENDING`, `SCHEDULED`,
  or `RUNNING` item must be either progressing within its documented bound or
  classified with an operator action.

## Q5. Failure Drills

Run these against the pinned candidate. Save timestamps, relevant logs, status
screens, TaskRun JSON, trace, audit rows, and Artifact listings for each case.

- [ ] Cancel a run while the Agent is executing. It reaches `CANCELED` within
  the configured grace and preserves the output and Artifacts produced before
  cancellation.
- [ ] Gracefully terminate one worker and hard-kill another so it cannot report.
  The first follows shutdown policy; the liveness reaper settles the second as
  `FAILED`, names lost worker contact, performs no hidden retry, and an explicit
  retry succeeds as a new run.
- [ ] Interrupt database access. `/readyz` and System Status show the dependency
  failure, the Server does not require rebuilding, and service recovers after
  access is restored.
- [ ] Interrupt Redis. Coordination-dependent work refuses or degrades as
  documented, durable records remain intact, and two-replica operation resumes
  without duplicate turns or runs after recovery.
- [ ] Deny worker object-storage writes. The run fails with an understandable
  cause, retains safe evidence, and leaves no record claiming a missing object
  is downloadable.
- [ ] Deny Server object-storage reads, then restore them. Readiness and System
  Status expose the outage and a completed Artifact becomes readable again
  without rewriting run data.
- [ ] Exercise provider timeout, rate limit, and credential refusal. TaskRun
  failure classification, managed-call status, logs, and operator action must
  agree, without leaking the provider credential.

## Q6. Recovery And Maintenance

- [ ] Follow [the backup and restore runbook](backup-restore.md) to restore the
  coordinated database and bucket backup into an empty recovery
  environment. Run `buildmax-server storage verify --checksums` and compare the
  identifiers and meaningful state of accounts, Sessions, grants, memberships,
  Conversations/messages, Issues/comments, Agents/revisions, Workflows/runs/
  nodes/results, Schedules, Tasks/TaskRuns, traces, audit, usage, Artifacts,
  checkpoints, Secrets, model catalog entries, Plugin packages/activations, and
  the migration ledger. Compare Space files separately because no database row
  lets `storage verify` enumerate them.
- [ ] With the original KEK, use a restored managed model credential and Space
  Secret, Continue a pre-backup Task from its checkpoint, and download every
  sampled Artifact with its original checksum. State measured RPO, RTO, and
  accepted loss.
- [ ] Exercise the declared upgrade path in the
  [production deployment guide](../../deployment/production/README.md) through
  a real schema change. Start the
  previous binary against the upgraded schema and prove its documented refusal;
  recover by restoring the paired pre-upgrade database and bucket. Database
  down-migrations and binary rollback are not supported.
- [ ] Follow [the credential rotation runbook](credential-rotation.md) to rotate
  the JWT secret, database credential, storage identity or credential,
  managed-model credential, KEK, and worker API certificate/CA. Record effects
  on Sessions, in-flight runs, existing Jobs, stored data, and new work. Old
  credentials must be refused when their overlap window closes.
- [ ] Exercise trace, audit, Artifact, checkpoint-orphan, and finished-Job
  retention. Live records must keep valid pointers; deleted objects must no
  longer be claimed as retrievable; every audited prune must be visible.
- [ ] Record the exact Kubernetes, CNI, Redis, MySQL, S3, ingress, TLS, provider,
  and model versions exercised. This is the first Beta's tested set, not a
  promise about a broad compatibility matrix.

## Q7. Operating Window And Product Quality

- [ ] Run the candidate for at least 24 hours under the recorded modest load,
  crossing Schedule, liveness, reconciliation, retention, and Job-cleanup
  intervals. During the window roll the Server and restart Redis. End with no
  stranded run, duplicate Schedule fire, missing object, or unclassified
  failure.
- [ ] Record database, bucket, trace, and audit growth; demonstrate that the
  configured retention and capacity plan covers the declared operating window.
- [ ] Correlate one user action through request, Task, TaskRun, worker, managed
  call, trace, and audit records using documented identifiers and available
  logs. Record where the operator must use infrastructure logs because BuildMax
  supplies no metrics or alerting integration.
- [ ] Have an operator who did not implement the features complete the core
  journeys and diagnose the injected failures using only documented surfaces.
- [ ] Run the product-owned evaluation suite against the selected real managed
  model and record pass rate, uncertainty, usage, cost, and every unscored
  failure. Platform/runtime errors and trust violations block qualification;
  the small suite is not presented as a general benchmark score.
- [ ] Confirm the release regression profiles pass their documented checks:
  CLI/TUI, Desktop bridge and UI, packaged Desktop launch, and all supported
  provider contract tests.

## Evidence

Add one row per journey or drill. A CI summary page is not enough when pod logs,
restored identifiers, checksums, or screenshots are the actual proof.

The rows below record the 2026-10-02 DigitalOcean exercise. Its evidence
(per-item results, probe scripts, logs, and rendered configuration) is a local
bundle that names the deployment's hosts, allow-listed address, and test
accounts; it is not yet published where the release space can read it, which
this record requires before qualification.

| Gate or exercise | Result | Evidence URL or artifact | Notes and follow-up |
|---|---|---|---|
| Q0 candidate scope, configuration, and supply chain | Passed on alpha.18 | Local evidence bundle | Pinned digests, provenance verified with `gh attestation verify`, Trivy scans and SBOMs from the release runs. The alpha.19 Portal image scan caught a fixed pcre2 CVE kept by a cached layer, fixed by #812. |
| Q1 identity, authorization, and governance | Passed on alpha.18 | Local evidence bundle | Signup, sessions, roles, cross-Space refusal, disable, removal, owner recovery, quota refusal. Strict request decoding (#808) rerun on alpha.19 found no client sending unknown fields. |
| Q2 core product journeys | Passed on alpha.18 | Local evidence bundle | Real-model journeys. A Workflow Schedule accepted input its Workflow could not accept, and the model asked in prose instead of AskUser; both fixed by #806 and passed on alpha.19 (AskUser 5 of 5). |
| Q3 execution and Secret boundaries | Passed, one partial | Local evidence bundle | On alpha.18 Bash held `CAP_SYS_ADMIN`, could read `/proc/1/environ`, and bypassed the network tier; fixed by #807 and passed on alpha.19. Partial: a run token before claim could not be isolated, since the scheduler claims within seconds. |
| Q4 durable and distributed correctness | Passed on alpha.18 | Local evidence bundle | Cross-replica streaming and turn serialization, Redis restart, concurrent Continue/Retry/cancel, rolling both replicas under a run. |
| Running cancellation and graceful worker loss | Passed on alpha.18 | Local evidence bundle | Canceled within grace with output kept; a SIGTERMed worker reports `interrupted` without a hidden retry. |
| Hard worker loss and explicit retry | Passed on alpha.18 | Local evidence bundle | A SIGKILL from the node settled `worker_lost` after about 179s; explicit retry ran a new Job. The killed Job reports success because the worker's exit code reports dispatch, not the run. |
| MySQL, Redis, and object-storage outages | Failed on alpha.18; passed on rerun | Local evidence bundle | A Redis outage silently dropped a Conversation turn (fixed by #806: 503 on alpha.19). An Artifact download during a storage outage hung (#806 bounded it, #814 closed dead pooled connections: 503 in 17s on alpha.20). |
| Provider failures | Passed on alpha.18 | Local evidence bundle | 401, 429, 503, slow answer, and unroutable provider: run and managed-call classes and logs agree; no credential leaked. |
| Paired database and bucket restore | Passed on alpha.18 | Local evidence bundle | All 1860 identified rows from before the recovery point present, `storage verify --checksums` clean, RTO 237s. |
| Declared schema path and paired-restore rollback | Passed | Local evidence bundle | alpha.16 to alpha.18 through a real schema change; alpha.16 refuses the newer schema; rollback by paired restore. |
| Credential and worker-TLS rotation | Passed; one item waived | Local evidence bundle | JWT, worker certificate and CA, and KEK on alpha.18; Spaces key on alpha.20. The model key failed on alpha.20 (task titles kept the startup key), fixed by #817 and passed on alpha.21. Database password rotation waived below. |
| Retention and capacity | Partial | Local evidence bundle | Artifact purge and audit pruning exercised. Audit and traces are kept indefinitely with no sizing or monitoring; over 24h the database grew 3.85 to 9.50 MB and the bucket prefix 1.24 to 5.83 MB, including all qualification probing. |
| 24-hour operating window | Passed | Local evidence bundle | 2026-10-02T01:54Z to 2026-10-03T01:54Z: 216 of 216 scheduled fires on time and succeeded, nothing stranded, across upgrades from alpha.18 to alpha.21, Server rolls, and Redis and storage outages. |
| Non-author operator journey | Waived | Local evidence bundle | See the waivers below. |
| Real-model product evaluation | Passed | Local evidence bundle | `./make eval` with GPT-5.6 Luna: 9 of 9 scored trials, 95% CI 70–100%, 0 unscored, 0.0077 USD. A small suite, not a benchmark. |
| Local and Desktop release regressions | Partial | Local evidence bundle | Candidate CI covers CLI/TUI, provider contract tests, and the Desktop UI build; packaged Desktop launch and the Desktop UI suite need a native window and were not run. |

## Decision

Current decision: **NOT READY FOR BETA**.

After the 2026-10-02 exercise no product defect is open. Before signing, the
gates that ran only on alpha.18 have to pass on the candidate proposed for
release, the evidence has to be published where the release space can read it,
and the partial rows above have to pass or be accepted as limits.

Qualification requires every core gate to pass, with no authorization escape,
unexplained data loss, stranded durable work, database pointer to a missing
object, or unresolved critical/high security finding. A waiver must name the
unmet behavior, user impact, compensating operator control, owner, and expiry;
it is an explicit release decision rather than an implied pass.

Waivers recorded by the project owner on 2026-10-02:

| Unmet behavior | User impact | Compensating control | Owner | Expiry |
|---|---|---|---|---|
| Database password rotation not exercised | A wrong rotation procedure would surface during a real rotation | Documented procedure in [DigitalOcean](digitalocean.md); the managed database is reachable only from the cluster | @gougoujiang | This candidate |
| No independent operator completed the journeys | Documentation gaps a new operator would hit are not measured | Every journey ran against the documented surfaces with probe scripts | @gougoujiang | This candidate |

| Role | Name | Date | Decision or waiver link |
|---|---|---|---|
| Qualification operator | Not signed | — | — |
| Engineering owner | Not signed | — | — |
| Release owner | Not signed | — | — |
