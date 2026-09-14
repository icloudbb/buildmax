# Enterprise Deployment Loop

> **简体中文：** [阅读中文镜像](../zh-CN/design/企业部署.md)

## Contents

- [Status](#status)
- [1. Decision](#1-decision)
- [2. Product Goal](#2-product-goal)
- [3. Current Baseline](#3-current-baseline)
- [4. Main Gaps](#4-main-gaps)
- [5. In Scope](#5-in-scope)
- [6. Out Of Scope](#6-out-of-scope)
- [7. Target Deployment Architecture](#7-target-deployment-architecture)
- [8. Configuration Shape](#8-configuration-shape)
- [9. Implementation Plan](#9-implementation-plan)
- [10. Validation](#10-validation)
- [11. Risks](#11-risks)
- [12. Open Questions](#12-open-questions)
- [13. Initial Delivery Slice — Done](#13-initial-delivery-slice-done)

## Status

- roadmap_priority: `P3`
- status: `in_progress` — M1 (config contract cleanup), M2 (kind end-to-end
  path), M4 (production reference), and M5 (operator bootstrap) are done; M3
  (health and readiness) is mostly done. Shared Redis coordination and the
  two-Server reference topology were added by the later
  [Server coordination](server-coordination.md) decision. The remaining work
  is candidate operating evidence and the configuration checks named in §4.3
  and §12.
- follows: P2 Portal outcome surface (complete; plan retired)
- roadmap: [../ROADMAP.md](../ROADMAP.md)
- created_at: `2026-05-17`

## 1. Decision

After P2 makes Portal results first-class, the next platform move is P3:
make private deployment boring, repeatable, and diagnosable.

BuildMax already has the main runtime pieces:

- server binary
- worker binary
- scheduler
- Portal image
- MySQL persistence
- local FS and MinIO/S3 blob storage
- kind setup
- Kubernetes deployment YAML

The gap is deployment closure. A new environment should be able to reach this
happy path without reading code:

1. start infrastructure
2. start server and Portal
3. log in
4. create space work
5. run a worker task
6. view the produced result

## 2. Product Goal

A company or self-hosting user can deploy BuildMax privately with a recommended
path that is explicit, observable, and recoverable.

The deployment loop should feel like:

> I can install it, verify it, and understand failures from clear messages.

not:

> I need to inspect bootstrap code, manifests, and historical docs to discover
> which variables still matter.

## 3. Current Baseline

Current deployment assets:

- `./make kind up` owns the local Kubernetes loop: it creates the pinned kind
  cluster, installs MySQL, MinIO, and ingress-nginx, builds and loads images,
  applies the manifests, and runs the deterministic deployment smoke.
- `./make kind reload`, `smoke`, `status`, `logs`, `forward`, and `down` expose
  the narrower lifecycle operations without a parallel script workflow.
- `deployment/docker/Dockerfile.buildmax` builds the Go binaries into a
  container image.
- `deployment/docker/Dockerfile.portal` builds and serves the Portal static app.
- `docs/deploy/local-kind.md` documents the local kind path.
- `internal/bootstrap/server.go` starts DB, storage, HTTP server, and scheduler.
- `internal/bootstrap/worker.go` loads `server.yaml`, gets a scheduled run from
  the server, executes the agent runtime, and uploads artifacts.
- `internal/config/server_config.go` is the active server/worker config loader.

Current config reality:

- `BUILDMAX_HOME` locates the application data directory.
- `BUILDMAX_HOME/server.yaml` is the main server and worker config file.
- `BUILDMAX_JWT_SECRET` can override `jwt_secret` in `server.yaml`.
- `internal/config/env_spec.go` intentionally lists only bootstrap env vars.
- Docs and manifests were realigned to `server.yaml` (see §4.1).

## 4. Main Gaps

### 4.1 Config Contract Drift — RESOLVED

**This gap is closed.** It was worse than described here: the manifest's env
vars were not merely confusing, they were dead. Nothing read `BUILDMAX_DB_*`,
`BUILDMAX_MINIO_*`, or `BUILDMAX_WORKSPACES_DIR` any more, no `server.yaml` was
supplied to the container, and a deployed server fell back to built-in defaults
and crash-looped against MySQL on `localhost`. Worker Jobs had the same problem.

What shipped:

- `deployment/buildmax-deploy.yaml` carries a `buildmax-config` ConfigMap with
  `server.yaml`, mounted by subPath so the rest of `BUILDMAX_HOME` stays writable
- credentials come from `buildmax-secret` through env overrides on
  `database.password`, `storage.minio.access_key`/`secret_key`,
  and `conversation.model.api_key`
- worker pods mount the same ConfigMap via `worker.k8s.config_map`, with
  `BUILDMAX_HOME` set to `worker.k8s.home_dir`
- runtime configuration stays in `docs/reference/configuration.md`; the root
  `.env.example` lists only personal credentials consumed by repository tasks
  and does not duplicate the runtime configuration surface
- `TestDeploymentConfigMapLoads` fails the build if the manifest and
  `internal/config/server_config.go` drift apart again

Not verified against a live cluster — see §10.

### 4.2 Missing Recommended Deployment Shape — RESOLVED

**This gap is closed.** `deployment/production/` is the blessed shape: one
plain-YAML manifest for a cluster that already runs its own MySQL, object
storage, ingress, and certificates, plus a README stating the contract each
dependency has to meet. It is deliberately not a chart, so it converts to
whatever a cluster is already managed with, and every dependency address is a
placeholder so an unedited `kubectl apply` fails rather than coming up against
the wrong database.

The architecture it fixes:

- Server Deployment
- Redis coordination Deployment and Service
- Portal Deployment
- Worker Jobs launched by the scheduler
- external MySQL
- external S3-compatible storage
- ConfigMap for non-secret `server.yaml`
- Secret for JWT, LLM API key, DB password, S3 secret
- Ingress for Portal and API on one origin
- two Server replicas using `coordination.mode: redis`

What the shape does *not* yet have is operational evidence: no restore
exercise, no upgrade/rollback exercise across a schema change, no metrics, and
no run against a real cloud account. Those are open questions 6–9 below, not
gaps in the shape itself.

### 4.3 Health And Startup Diagnostics Are Thin

Resolved in part. `GET /readyz` now reports whether the server can serve
traffic, and `/healthz` keeps its liveness meaning — see open question 2 below.
The reference manifest points the readiness probe at `/readyz` and leaves
liveness on `/healthz`.

Shipped:

- database reachable
- object storage reachable

Still open:

- worker launch mode valid
- LLM config available for conversation title/runtime paths where required

Both are already validated at **bootstrap** (§5.4): an invalid worker mode or a
model the deployment cannot serve fails startup (`internal/bootstrap/server.go`).
What is open is surfacing them as distinct `/readyz` checks rather than only as
fatal startup errors.

Storage write permission is a **deployment-initialization** concern, not a
readiness concern. The production reference requires read, write, and list
access on the dedicated bucket/prefix, while `/readyz` deliberately verifies
only read-only dependency availability. Writing from every readiness interval
would make the kubelet probe own objects and their retention policy.

BuildMax does not prove those permissions itself, and will not. The bucket, its
policy, and the workload identity are provisioned by whoever runs the
deployment, and their own tooling establishes that grant better than a startup
probe restating it. The kind smoke exercises the path against a development
MinIO instance; an adapted production manifest is the operator's to verify.

### 4.4 Worker End-To-End Path Needs A Single Verification

The acceptance path is not just "server starts".

It must prove:

- scheduler claims pending task runs
- worker starts in the chosen mode
- worker can call server worker API
- worker can materialize space home
- worker can write `artifacts/result.md`
- server can surface the result through artifact endpoints

## 5. In Scope

### 5.1 Deployment Config Standardization

Define one active deployment config contract:

- `server.yaml` is the canonical file for server and worker config.
- env vars are only for:
  - `BUILDMAX_HOME`
  - secret overrides like `BUILDMAX_JWT_SECRET`
  - test-only values
- Kubernetes uses:
  - ConfigMap for `server.yaml`
  - Secret for sensitive values
  - env vars only where the code explicitly supports overrides

### 5.2 Local Kind Path

Make the local kind path fully end-to-end:

```sh
./make kind up
./make e2e kind
```

Then verify:

- Portal opens at `http://localhost:8080`
- API health works at `http://localhost:8080/healthz`
- login works
- a task can run through a worker Job
- a result is visible in Portal

### 5.3 Production Reference Path

Add production-oriented deployment docs:

- required services
- config file example
- secrets example
- image tags
- migration expectations
- storage persistence expectations
- ingress/TLS expectations
- backup boundaries

This does not need to become a full Helm chart in the first slice.

### 5.4 Startup Errors And Health Checks

Improve startup and health diagnostics so common misconfiguration has a clear
failure mode.

Required checks:

- `jwt_secret` present
- `workspaces_dir` present
- DB connection succeeds
- storage backend config is valid
- MinIO/S3 client can reach bucket when configured
- worker mode is valid
- `worker.binary` exists for `local_process`
- Kubernetes job creator is available for `k8s_job`
- `worker.llm.transport` names a model policy the deployment can actually serve

### 5.5 Initial Admin / Space / Quota Story

The bootstrap story is implemented. Self-registration is closed by default. An
operator creates an account and its personal Space with `buildmax-server user
create`, issues a single-use login code, and grants deployment authority
separately with `buildmax-server admin grant`. The default quota tier and Space
owner membership are created without a database edit. See M5 and
[deployment authentication](../deploy/authentication.md). Separately, an
enabled OIDC deployment can JIT-provision ordinary accounts under its explicit
domain policy; that path is owned by
[enterprise identity and access](enterprise-identity-and-access.md).

## 6. Out Of Scope

- Full Helm chart as the first required deliverable.
- Multi-region deployment.
- HA MySQL design.
- Enterprise SSO in this deployment-loop record; the browser OIDC flow is now
  implemented under the separate
  [enterprise identity and access](enterprise-identity-and-access.md) design.
- Billing.
- Advanced secrets manager integration.
- A policy or approval platform beyond the shipped audit trail and governance
  foundation.

## 7. Target Deployment Architecture

Recommended MVP topology:

```text
User
  |
  v
Ingress
  |---------------------------|
  v                           v
Portal Service                API Service
Portal Deployment             buildmax-server Deployment
                              |
                              v
                         Scheduler
                              |
                              v
                         Worker Job(s)

Shared dependencies:
  - MySQL
  - MinIO/S3
  - Kubernetes Secret
  - Kubernetes ConfigMap containing server.yaml
```

The server process owns HTTP API and scheduler. Workers are separate processes
or Jobs. Both server and worker read the same config contract.

## 8. Configuration Shape

### 8.1 `server.yaml`

The deployment ConfigMap should mount a complete `server.yaml`.

Example shape:

```yaml
port: 5678
cors_origin: "https://buildmax.example.com"
workspaces_dir: "/buildmax/workspaces"
default_quota_tier: "free_trial"

database:
  host: "mysql"
  port: 3306
  user: "buildmax"
  password: ""
  name: "buildmax"

storage:
  persist_backend: "minio"
  artifact_backend: "minio"
  minio:
    endpoint: "http://minio:9000"
    region: "us-east-1"
    access_key: ""
    secret_key: ""
    bucket: "bmstore"
    prefix: "workspaces"

worker:
  run_mode: "k8s_job"
  server_url: "http://buildmax.buildmax.svc.cluster.local:5678"
  run_token_ttl: 24h
  run_timeout: 6h
  k8s:
    namespace: "buildmax"
    image: "buildmax:local"
    resources:
      cpu_request: "500m"
      cpu_limit: "2"
      memory_request: "1Gi"
      memory_limit: "4Gi"

conversation:
  model:
    model: "openai/gpt-4o-mini"
    api_url: "https://openrouter.ai/api/v1"
    api_key: ""
    context_window: 0
    call_timeout: 60
```

The production reference keeps shape and non-secret values in the ConfigMap and
injects secret fields through the supported environment overrides:

- non-secrets in ConfigMap
- secrets in Secret
- small bootstrap code support for secret env overrides where necessary

### 8.2 Secret Values

Secret values:

- `jwt_secret`
- DB password
- S3 access key and secret key
- conversation model API key

The supported overrides are `BUILDMAX_JWT_SECRET`,
`BUILDMAX_DATABASE_PASSWORD`, `BUILDMAX_STORAGE_MINIO_ACCESS_KEY`,
`BUILDMAX_STORAGE_MINIO_SECRET_KEY`, and
`BUILDMAX_CONVERSATION_MODEL_API_KEY`. Their precedence and complete names live
in [the configuration reference](../reference/configuration.md); the design does
not duplicate a second authoritative list.

## 9. Implementation Plan

### M1. Config Contract Cleanup — DONE

- ✅ Deployment manifests mount `server.yaml` from a ConfigMap.
- ✅ Stale env vars removed; the ones that remain are overrides the code binds.
- ✅ Documented sample: `config-examples/server.example.yaml` plus
  `docs/reference/configuration.md`.
- ✅ Runtime configuration is not duplicated into `.env.example`; that file is
  reserved for personal credentials consumed by repository tasks.
- ✅ `README.md`, `CONTRIBUTING.md`, and the kind guide (now
  `docs/deploy/local-kind.md`) match the YAML config contract.

Acceptance, both met:

- a new reader can tell exactly where each config value comes from —
  file for shape, environment for credentials
- `deployment/buildmax-deploy.yaml` matches `internal/config/server_config.go`,
  now enforced by a test rather than by review

### M2. Kind End-To-End Path — DONE

`./make kind up` owns the current local Kubernetes path: it creates or reuses
the pinned kind cluster, installs its backing MySQL/MinIO/ingress, builds and
loads the images, applies the deployment and deterministic model configuration,
then runs the smoke. The basic manifest runs two Servers with Redis
coordination. The smoke signs in, proves a space boundary, creates and runs
work, reads its artifact, proves that retry creates a second executed run, and
exercises cancellation. The managed variant also proves the run-scoped
credential and call ledger.

The old `./make setup && ./make deploy` spelling is gone: `tools/mk` no longer
answers either name, and `./make kind up` is the only path.

Acceptance met:

- `./make kind up` reaches a visible Portal and a successful worker result;
- post-merge and scheduled CI run the same kind and Compose smoke paths.

### M3. Health And Readiness

- Keep `/healthz` as a cheap liveness check.
- Add a readiness endpoint or enrich health output behind an authenticated/admin
  route.
- Check DB and storage dependencies.
- Add clear startup errors for worker-mode and storage misconfiguration.
- Add Kubernetes readiness/liveness probes.

Acceptance:

- a broken DB, missing bucket, or invalid worker mode has
  a clear failing check

### M4. Production Reference Guide — DONE

`deployment/production/` carries the manifest and the guide: images, config,
secrets, storage, database, ingress/TLS, worker mode, backup boundaries, and
upgrade steps, with what the reference deliberately does not cover stated
rather than left to be discovered. `manual/support.md` carries the
compatibility half — what an upgrade may and may not do to a schema, an API,
a config key, and stored data. `internal/architecture` parses the manifest's
ConfigMap the way the server parses its own config, so the two cannot drift.

Acceptance met: a private deployment can be planned from these docs without
reading Go bootstrap code. Not met, and tracked as open questions rather than
as part of this milestone: the reference has never been applied against a real
managed database or object store.

### M5. Admin Bootstrap Story — DONE

Private deployments close self-registration by default. An operator creates an
account and its personal Space with `buildmax-server user create`, issues a
single-use login code, and grants deployment authority separately with
`buildmax-server admin grant`. The commands audit their actions; the Portal then
handles ordinary Space administration. The exact procedure and its recovery
semantics are in [deployment authentication](../deploy/authentication.md).

Acceptance met: operators can create the first user, Space, role, quota tier,
and System Administrator without modifying the database.

## 10. Validation

Code validation:

```sh
./make test ./internal/config ./internal/bootstrap ./internal/server/handlers ./internal/server/scheduler
```

Full validation:

```sh
./make test
```

Deployment validation:

```sh
./make kind up
./make e2e kind
```

Manual product validation:

1. Open Portal.
2. Log in.
3. Create or select a space.
4. Create an issue or conversation task.
5. Run work.
6. Confirm the worker completes.
7. Confirm result/artifact is visible.

## 11. Risks

- **Config split confusion**: YAML plus env overrides can become unclear unless
  docs and code define precedence precisely.
- **Secret leakage**: development manifests contain clearly labeled placeholder
  values; production must use the documented environment overrides backed by a
  deployment Secret.
- **Kind success hiding production gaps**: the local path should be a reference,
  not the only documented shape.
- **Worker mode drift**: local process and Kubernetes Job modes must use the
  same run lifecycle and storage assumptions.
- **Silent storage failure**: storage checks must be explicit because result
  visibility depends on artifacts.

## 12. Open Questions

1. ~~Should BuildMax support env overrides for all secret fields in
   `server.yaml`, or mount a rendered secret-backed config file?~~ **Decided:
   environment overrides.** The production manifest sources them from a Secret,
   while the ConfigMap carries the non-secret shape.
2. ~~Should P3 introduce `GET /readyz`, or should `/healthz` become dependency
   aware?~~ **Decided: a separate `/readyz`.** Making `/healthz` dependency
   aware would have fed the same answer to both probes, and Kubernetes acts on
   them very differently: a failed readiness check stops traffic, while a
   failed liveness check restarts the container. A shared endpoint would have
   turned every database blip into a restart of a server that was working.
3. ~~Should Redis remain in setup if the current server path does not require
   it?~~ **Superseded by the later Server-coordination decision: yes for the
   basic/kind and production multi-replica references; no for single-Server
   Compose.** Both cluster manifests now configure Redis and two Server
   replicas. See [server-coordination.md](server-coordination.md).
4. ~~Should the recommended production path use Kubernetes Jobs only, or document
   `local_process` as a single-node option?~~ **Decided: Kubernetes Jobs are the
   recommended production path, and `local_process` stays supported as a
   single-machine option with its trust domain stated.** A local worker is a
   child process of the server under the same uid, so no amount of narrowing
   what it inherits turns it into a boundary. Rather than harden a topology that
   cannot hold one, the deployment documentation says server and workers share a
   trust domain there, and a deployment that needs them separated runs `k8s_job`.
5. ~~Should private deployments allow self-signup by default?~~ **Decided:
   no.** `allow_signup` defaults false; an operator creates accounts and issues
   single-use login codes.

The remaining questions came from the retired *Private production operations*
proposal. The reference topology it asked for now exists; what it asked for and
did not get is evidence that the topology can be operated. The required
exercises and their still-open evidence live in the
[Beta readiness record](../deploy/beta-readiness.md):

6. What availability and recovery targets are realistic for the first Beta? The
   deployment reference states a recovery *procedure* — restore from backup,
   redeploy the previous image tag — without stating an objective it meets.
7. Has a restore actually been exercised? Recovering a space and a completed run
   needs the database and the bucket restored *together*, and nothing has proven
   that the pair comes back consistent.
8. Has the candidate's declared schema path and recovery been exercised?
   Alpha migrations can drop compatibility. Name the starting schema and test
   any claimed binary rollback, or prove the destructive-cutover restore path;
   `manual/support.md` does not promise blanket N-1 compatibility.
9. ~~Which metrics make a deployment supportable?~~ **Decided for the first
   Beta: a metrics endpoint is not a prerequisite.** The minimum diagnostic set
   is logs, `/readyz`, System Status, TaskRun and artifact state, the run trace,
   the managed-call ledger, and audit history. The qualification drills must
   prove that set explains every required outcome. Add `/metrics` later only
   when an exercise names a concrete signal that the existing surfaces cannot
   provide.
10. How are JWT signing keys, access/refresh sessions, per-run tokens, database,
    storage, and model credentials **rotated**? Injection is settled — env
    overrides sourced from a Secret — but nothing documents what a rotation
    does to sessions, in-flight task runs, or a worker Job that already holds a
    run token.
11. Which versions of Kubernetes, MySQL, and S3-compatible storage form the
    supported matrix? `manual/support.md` grades surfaces and platforms but
    names no dependency versions, and `deployment/production/README.md` states
    a behavioural contract instead.

## 13. Initial Delivery Slice — Done

The first P3 slice fixed the config/deployment mismatch:

1. Added a deployment `server.yaml` sample.
2. Mounted it in the deployment manifests.
3. Removed unsupported environment configuration.
4. Kept secrets explicit and development-only values labeled.
5. Updated deployment documentation.
6. Replaced the old setup/deploy path with the owned kind workflow and
   dependency-aware `/readyz` verification.

That slice established the deployment contract used by the later milestones.
