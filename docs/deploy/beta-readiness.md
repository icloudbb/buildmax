# Beta Readiness Record

> **简体中文：** [阅读中文镜像](../zh-CN/deploy/beta-readiness.md)
> **Audience:** operators and release managers · **Status:** current — not qualified
BuildMax has **not passed the Beta gate**. This document is both the procedure
and the evidence record for the first private-deployment Beta. Automated tests
show that the candidate is ready to exercise; only results produced with the
same immutable artifacts proposed for release count as qualification evidence.

Do not change the status above to `qualified` until every required item below
passes, every evidence link is durable and readable by the release space, and
the final decision is signed. A failed item stays in the record with its
diagnosis and the later successful rerun.

## Candidate

Fill this table before starting. Tags alone are not immutable evidence.

| Field | Recorded value |
|---|---|
| Version and commit | Not recorded |
| Server image digest | Not recorded |
| Worker image digest | Not recorded |
| Portal image digest | Not recorded |
| Operator | Not recorded |
| Exercise date and environment | Not recorded |
| Kubernetes version and distribution | Not recorded |
| MySQL product and version | Not recorded |
| S3 product, version or service, and region | Not recorded |
| TLS termination and ingress | Not recorded |
| Configuration snapshot, with secrets redacted | Not recorded |
| Starting schema/commit and supported upgrade or clean-install path | Not recorded |

## Accepted Limits

The first Beta is for one trusted space on a private network. Record that the
operator and participants accepted all of these limits:

- [ ] The deployment is not exposed directly to an untrusted public network.
- [ ] Official worker images select and probe the strict OS sandbox baseline;
  the candidate must demonstrate confined Bash. Stdio MCP is disabled in the
  supported worker profile unless its child process is confined by that
  declared boundary. The native worker pod uses root plus `SYS_ADMIN` with the
  documented seccomp/AppArmor profile; an outer runtime such as gVisor is not
  required for this Beta.
- [ ] General worker egress has no enforced Pod-level destination allow-list.
  The shipped worker-port `NetworkPolicy` restricts control-channel ingress;
  it does not restrict all outbound traffic.
- [ ] Storage credentials or projected storage identity are available to the
  worker because it reads and writes run state and artifacts directly.
- [ ] SSO, multi-region operation, and automatic re-dispatch of lost runs are
  not part of this Beta.

## Preflight

- [ ] Pin all three candidate image digests and archive the rendered deployment
  configuration with secrets redacted.
- [ ] Record the configured worker CPU and memory requests and limits. The
  server validates them at startup, so deliberately supply one invalid quantity
  and record that the candidate refuses to start and names the key, rather than
  coming up and scheduling an unbounded Job.
- [ ] Record and exercise the trace retention and capacity policy. BuildMax now
  supplies an audited `trace.retention_days` sweep; `0` still means keep forever
  and therefore requires explicit volume sizing, monitoring, and an operator
  cleanup procedure.
- [ ] Confirm MySQL and S3 are external to the BuildMax cluster and reached over
  TLS with the same identities the candidate server and workers will use.
- [ ] Take a coordinated database and bucket backup and record their identifiers
  before any upgrade exercise.
- [ ] Record the candidate's `./make check ci`, direct and managed Compose/kind
  smoke, Portal browser E2E, and release-archive verification URLs.
- [ ] Record the server, worker, and Portal image scan results, SBOM locations,
  and provenance-attestation verification results.

## Operator Journey

Use documented UI and operator surfaces. The person performing this journey
must not need source-code knowledge.

- [ ] Bootstrap the System Administrator, create or invite the account, sign in,
  create a space, and verify access boundaries with a second role.
- [ ] Configure an approved managed model without distributing its provider key
  to the client, then create work and run it in a Kubernetes worker Job.
- [ ] Inspect the live Job. Record its read-only root filesystem, dropped
  capabilities (all but `SYS_ADMIN`), seccomp and AppArmor profiles, absent
  service-account token, effective CPU/memory resources, minimized
  environment, and per-run credential.
- [ ] View the completed output and download its artifact. Record an artifact
  checksum and confirm it matches the object in external storage.
- [ ] Explain the run from the TaskRun outcome and artifacts, stored trace,
  managed-call model/token ledger, quota view, and audit history. The trace must
  report the actual sandbox boundary; artifacts are separate stored objects and
  are not claimed to be embedded in the trace.
- [ ] Retry the work and confirm the retry is a new TaskRun with its own trace,
  usage entry, audit trail, and artifact rather than a mutation of the first run.

## Failure Drills

Run these against the pinned candidate. Save timestamps, relevant logs, status
screens, TaskRun JSON, trace, audit rows, and artifact listings for each case.

- [ ] Cancel a run while the agent is executing. It reaches `CANCELED` within
  the configured grace and preserves the output and artifacts produced before
  cancellation.
- [ ] Hard-kill a worker so it cannot report. The liveness reaper settles the
  run as `FAILED` after the configured grace, the failure names lost worker
  contact, no hidden retry occurs, and an explicit retry succeeds as a new run.
- [ ] Interrupt database access. `/readyz` and System Status show the dependency
  failure, the server does not require rebuilding, and normal service returns
  after database access is restored.
- [ ] Deny the worker object-storage write. The run fails with an operator-
  understandable cause, retains the evidence it can safely retain, and leaves
  no artifact record that claims a missing object is downloadable.
- [ ] Deny object-storage reads to the server, then restore them. Readiness and
  System Status expose the outage, and the previously completed artifact is
  readable again without rewriting run data.

## Recovery And Maintenance

- [ ] Restore the coordinated database and bucket backup into an empty recovery
  environment. Sign in and compare space, task, TaskRun, trace, audit, usage, and
  artifact identifiers and checksums with the pre-backup record. State the
  measured recovery time and any accepted data loss.
- [ ] Name the starting schema and commit; do not assume a released predecessor.
  Exercise the candidate's claimed upgrade path through a real schema change.
  Where binary rollback is supported, run the previous Server and worker
  against the upgraded schema and repeat the operator journey. Where Alpha
  deliberately drops compatibility, record the affected fields and use a
  clean-install/paired-restore procedure, exercising that recovery instead.
  Unsupported rollback is an explicit candidate limit, not a passed rollback
  test. Database down-migrations are not supported.
- [ ] Rotate the JWT secret, database credential, storage identity or credential,
  and model credential using a documented drain/restart procedure. Record what
  happens to existing browser sessions, in-flight runs, and already-created
  worker Jobs; no hot-rotation claim is required.
- [ ] Record the exact Kubernetes, MySQL, S3, ingress, and TLS versions exercised.
  This is the first Beta's tested set, not an unsupported promise about a broad
  compatibility matrix.

## Evidence

Add one row per journey or drill. A CI summary page is not enough when pod logs,
restored identifiers, or checksums are the actual proof.

| Exercise | Result | Evidence URL or artifact | Notes and follow-up |
|---|---|---|---|
| Candidate checks and supply chain | Not run | — | — |
| Operator journey | Not run | — | — |
| Running cancellation | Not run | — | — |
| Hard worker loss and explicit retry | Not run | — | — |
| Database outage and recovery | Not run | — | — |
| Object-storage denial and recovery | Not run | — | — |
| Paired database and bucket restore | Not run | — | — |
| Declared schema path and rollback or destructive-cutover recovery | Not run | — | — |
| Credential rotation | Not run | — | — |

## Decision

Current decision: **NOT READY FOR BETA**.

Qualification requires all checklist items to pass. Any waiver must name the
unmet behavior, impact, compensating operator control, owner, and expiry; it is
then an explicit release decision rather than an implied pass.

| Role | Name | Date | Decision or waiver link |
|---|---|---|---|
| Qualification operator | Not signed | — | — |
| Engineering owner | Not signed | — | — |
| Release owner | Not signed | — | — |
