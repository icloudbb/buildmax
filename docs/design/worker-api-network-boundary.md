# Worker API Network Boundary

> **简体中文：** [阅读中文镜像](../zh-CN/design/Worker API网络边界.md)

> **Audience:** contributors and operators · **Status:** shipped

Related: [Worker run token](worker-run-token.md), [Agent Core trust
harness](trust-harness.md) §3.9, [Agent-scoped sandbox
policy](agent-sandbox-policy.md), [Graceful shutdown](graceful-shutdown.md), and
[Enterprise deployment](enterprise-deployment.md).

## Contents

- [1. Status](#1-status)
- [2. Problem](#2-problem)
- [3. Decision](#3-decision)
- [4. Security Boundary](#4-security-boundary)
- [5. Listener And Route Model](#5-listener-and-route-model)
- [6. Transport Authentication And Encryption](#6-transport-authentication-and-encryption)
- [7. Kubernetes Topology](#7-kubernetes-topology)
- [8. Run Authorization Still Applies](#8-run-authorization-still-applies)
- [9. Lifecycle And Availability](#9-lifecycle-and-availability)
- [10. Configuration Shape](#10-configuration-shape)
- [11. Options Considered](#11-options-considered)
- [12. Implementation Plan](#12-implementation-plan)
- [13. Validation](#13-validation)
- [14. Risks And Open Questions](#14-risks-and-open-questions)
- [15. Documentation Changes](#15-documentation-changes)

## 1. Status

- roadmap_priority: `R0` — candidate verification of the shipped worker API
  boundary
- status: shipped. M1 serves the worker control API on a second in-process
  listener with its own mux and fail-closed configuration; M2 makes that
  listener speak TLS while the worker reaches it through one explicit HTTP
  client built from the configured trust, refusing an http `k8s_job` URL unless
  opted into; M3 gives the reference production and kind manifests the
  `buildmax-api` and `buildmax-worker-api` Services, the worker port, the
  `NetworkPolicy` admitting only labelled worker pods, an Ingress pointing only
  at `buildmax-api`, and the CA mount into worker Jobs; M4 makes every worker
  route enforce its allowed TaskRun states — claim before Secret, the pinned
  revision's consumption, and no capability after a run is terminal; and M5
  makes the kind smoke generate the worker-listener certificate, run the worker
  over HTTPS, and prove the boundary in the same deployment — a labelled worker
  pod reaches the worker port, an unlabelled one is denied, and `/api/worker`
  answers `404` on the public Service. Wider Pod-level worker egress is
  conditional post-Beta hardening in [trust-harness.md](trust-harness.md) §3.9.
- decision_date: `2026-09-05`
- scope: isolate the Server's worker control channel from its public HTTP
  surface and authenticate its transport

This record answers one bounded part of the cluster-network gap in
[trust-harness.md](trust-harness.md) §3.9: how a worker reaches the Server. It
does not decide which Git hosts, package registries, model endpoints, or other
destinations a run may reach. Standard Kubernetes `NetworkPolicy` cannot
express the domain policy the in-process sandbox proxy enforces, so that wider
egress decision remains where it is.

## 2. Problem

The Server currently exposes one HTTP listener. Portal, user, webhook, and
worker routes are all registered on it. The production Ingress forwards the
whole `/api` prefix to that listener, so `/api/worker/*` is internet-reachable
even though only worker Jobs should call it.

Kubernetes workers use the same Service and listener over plain HTTP:

```text
Internet / Portal
       |
       | HTTPS
       v
Ingress ----------------------+
                              v
Worker Pod -- plain HTTP --> buildmax Service :5678 --> one route set
                                                        |- user API
                                                        |- Portal API
                                                        `- worker API
```

The run token still authenticates every worker request, which prevents an
unauthenticated caller from using a worker route. It does not provide transport
confidentiality or server authentication. A bearer token, task input, streamed
output, and a Space Secret response all cross the pod network in plaintext in
the reference topology.

The shared listener also prevents Kubernetes network policy from expressing
the intended boundary. A Service and a `NetworkPolicy` match addresses and
ports, not HTTP paths. They cannot admit a worker to `/api/worker/*` while
denying that same source the rest of `/api` when both use one port.

The current shape therefore has four avoidable properties:

| Property | Consequence |
|---|---|
| Worker routes share the public listener | The public Ingress exposes them |
| Worker traffic uses HTTP | Network observation or redirection can disclose a bearer token and response data |
| Every API uses one port | Layer 3/4 policy cannot distinguish worker traffic |
| A run token is the only caller boundary | A leaked token can be replayed from any network location that reaches the listener |

## 3. Decision

The Server will expose two independently routed HTTP listeners:

| Listener | Default address | Routes | Intended callers |
|---|---|---|---|
| Public | existing configured port, normally `:5678` | Every non-worker route, including Portal, user API, webhooks, WebSocket, health, OpenAPI, and Swagger | Ingress, operators, CLI, Desktop |
| Worker | `127.0.0.1:5679` unless explicitly configured | `/api/worker/*` only | `local_process` workers on the same host, or worker Pods through the internal Service |

Their canonical Kubernetes Service names are `buildmax-api` and
`buildmax-worker-api`, respectively. Listener names in logs and internal code
are `api` and `worker-api`; `public` describes reachability but is not part of
the resource name, because a `ClusterIP` behind an Ingress is not itself a
public Service type.

The secure default binds the worker listener to loopback. A Kubernetes
deployment must deliberately bind it to `:5679`, configure TLS, and create the
internal Service. An accidental default deployment therefore does not expose a
new unauthenticated cluster port.

The production topology becomes:

```text
Internet / Portal
       |
       | HTTPS
       v
Ingress --> buildmax-api Service :5678 --> public listener

Worker Pod
       |
       | HTTPS + run token
       v
buildmax-worker-api ClusterIP :5679 ------> worker listener
```

Both listeners initially remain in the same `buildmax-server` process and use
the same stores and services. This creates a network and route boundary, not a
process-isolation boundary. A later need to isolate public-handler compromise
from scheduler and worker authority may move the worker listener and scheduler
into another binary or Deployment without changing the protocol decided here.

## 4. Security Boundary

### 4.1 Threats This Decision Addresses

- An unauthenticated internet caller cannot reach a worker handler through the
  public listener, even if an Ingress rule forwards a broad `/api` prefix.
- An ordinary cluster Pod cannot connect to the worker listener when the
  production `NetworkPolicy` does not select it as a worker.
- A run token copied outside the worker network is insufficient on its own to
  reach the worker listener.
- Server identity is verified before a worker sends its bearer token or accepts
  task and Secret data.
- Worker control traffic is encrypted inside the cluster.

### 4.2 Threats This Decision Does Not Address

- A valid worker can read the Space Secrets and task data its run is authorized
  to receive.
- A compromised Server process owns both listeners and the scheduler.
- A Kubernetes administrator, node administrator, CNI administrator, or holder
  of the Server's ServiceAccount remains trusted.
- A worker Pod's general egress remains whatever the cluster and the
  agent-scoped sandbox policy allow. This design restricts access *to the
  worker listener*; it does not claim a domain-aware egress boundary.
- Run-token storage in a Job spec, token lifetime, object-store credentials,
  and root plus `SYS_ADMIN` worker containment are separate security debts.

### 4.3 Boundary Composition

No one control replaces another:

| Control | Establishes |
|---|---|
| Separate listener and route set | The public socket cannot dispatch a worker route |
| Internal `ClusterIP` Service | No deliberate external Kubernetes service exposure |
| `NetworkPolicy` | Only selected worker Pods may connect to the worker port |
| TLS | Server identity and confidentiality in transit |
| Run token | User, Space, Task, and TaskRun application authority |
| TaskRun state checks | Whether that authority is currently exercisable |

## 5. Listener And Route Model

### 5.1 Route Registration

The route source of truth remains each handler subpackage's `Register` method.
Composition changes from one root mux to two:

- the public mux registers every existing route except the worker handler;
- the worker mux registers only `internal/server/handlers/worker`;
- common transport middleware such as request IDs, bounded logging, recovery,
  and HTTP timeouts wraps both;
- browser CORS middleware wraps the public listener only;
- user access-token middleware is not made a fallback on the worker listener.

An unknown route returns `404` on either listener. In particular:

- `/api/worker/task-runs/...` on the public listener returns `404`, even with a
  valid run token;
- `/api/spaces/...`, `/api/auth/login`, `/api/webhook`, `/swagger`, and
  `/openapi.json` on the worker listener return `404`.

The public OpenAPI document may continue to describe the complete protocol for
contributors, but serving that document does not register the worker routes on
the public mux. The exact-route architecture test must compare the union of
both route sets with the document and separately enforce listener placement.

### 5.2 The Service Is Not The Boundary By Itself

Creating a second Kubernetes Service that targets the existing `:5678` port
would only add another name for the same socket. It would not stop the public
Service, the Ingress, a direct Pod-IP request, or another cluster Pod from
reaching worker handlers.

The distinct port and distinct mux are mandatory. The Service and
`NetworkPolicy` make that application boundary enforceable by the cluster.

### 5.3 No Server-Initiated Pod Connection

This change preserves the current direction of communication. The Server
creates a Job through the Kubernetes API; it never opens an application
connection to the Pod. The worker initiates metadata reads, claim and terminal
updates, cancellation polling, stream delivery, artifact publication, plugin
download, Secret materialization, and managed inference through the internal
listener.

## 6. Transport Authentication And Encryption

### 6.1 Required Server Authentication

The production worker listener uses TLS. Its certificate must contain the
internal Service DNS name, normally
`buildmax-worker-api.buildmax.svc.cluster.local`. The worker verifies that
name and a configured CA; `InsecureSkipVerify` is not a supported production
mode.

The first implementation supports:

- system trust roots when the internal certificate chains to one; and
- an explicit CA file mounted read-only into each worker Pod.

The server certificate and private key are mounted only into Server Pods. The
CA certificate is public material and may be distributed through a ConfigMap.
Certificate rotation initially takes effect on Server restart; live reload is
not required for the first implementation and must be stated in operator docs.

### 6.2 Development HTTP

Plain HTTP remains available for `local_process`, Compose, and local kind
development only through an explicit `allow_insecure_http` setting. A
`k8s_job` configuration with an `http://` Server URL fails validation unless
that setting is true. The production reference never enables it.

This is explicit rather than inferred from a hostname: `.cluster.local`,
loopback, and private addresses are routing facts, not evidence that a network
is confidential.

### 6.3 Mutual TLS

Run tokens remain the mandatory client authentication mechanism. Native mTLS
is an optional hardening mode in the first design because a shared client
certificate would introduce another deployment-wide credential into every
worker, while per-Pod certificate issuance needs workload identity the current
Job does not have.

Where a service mesh, SPIFFE issuer, or platform workload identity already
issues per-Pod identities, the internal listener may require their client
certificate in addition to the run token. BuildMax does not accept an mTLS
identity as TaskRun authority: it identifies the workload class, while the run
token identifies the run.

## 7. Kubernetes Topology

### 7.1 Services And Ingress

The production manifest defines:

- `buildmax-api`, selecting Server Pods and targeting the public port;
- `buildmax-worker-api`, a `ClusterIP` selecting the same Server Pods and
  targeting the worker port; and
- an Ingress whose backends include `buildmax-api` only.

The broad public `/api` Ingress rule can remain because the public mux does not
contain worker routes. An operator-specific reverse proxy path rule is useful
defense in depth but is not the authoritative separation.

The worker Job receives
`https://buildmax-worker-api.buildmax.svc.cluster.local:5679` as its Server URL.
The Server certificate key is never inherited by a worker.

### 7.2 Stable Labels

Every dynamically created worker Job and Pod carries stable labels owned by the
Kubernetes runner, including:

```yaml
app.kubernetes.io/name: buildmax-worker
app.kubernetes.io/component: worker
```

The TaskRun ID may be carried in an annotation for correlation, but it is not a
security selector and must not contain credential material. A worker has no
ServiceAccount token and cannot relabel itself through the Kubernetes API.

### 7.3 NetworkPolicy

A production `NetworkPolicy` selects Server Pods and expresses two ingress
rules:

- the public port remains reachable by the deployment's normal public path;
- the worker port is reachable only from Pods carrying the worker labels in
  the selected execution namespace.

Allowing all sources to the public port is acceptable in the portable reference
because public API authentication remains at the application layer and Ingress
exposure is intentional. Operators should narrow it to their ingress-controller
namespace where their CNI and health-probe behavior are known.

This policy protects direct Pod-IP access as well as Service access. A
`ClusterIP` Service without it is discoverability, not authorization.

The design does not add a default-deny worker egress policy. Doing that while
preserving Git, registry, model, and object-storage access requires the
conditional hardening decision in [trust-harness.md](trust-harness.md) §3.9.
The reference may add a narrow worker egress rule later if deployment evidence
reopens that path.

### 7.4 Namespace Boundary

The first implementation keeps Server and worker Jobs in the configured
namespace. A dedicated execution namespace is compatible with this design and
would improve containment, but it changes scheduler RBAC, Secret and ConfigMap
distribution, network selectors, and object-storage identity. It is follow-up
work rather than a hidden prerequisite.

## 8. Run Authorization Still Applies

Moving a handler to an internal listener does not make it trusted. Every worker
route continues to require a run token, match its `rid` claim to the path, and
derive Space and user attribution from Server state and signed claims rather
than a request body.

The listener work must not encode the current lifecycle gaps as intended
behavior. This lifecycle authorization is now enforced on the worker routes
themselves (M4), on top of the run token:

- Secret materialization requires the run to be RUNNING — the claimed, live
  state — and the worker claims the run before it fetches Secret values;
- materialization reads the consumption config from the Agent revision pinned
  onto the TaskRun, not the agent's current revision, so a config edited
  mid-run cannot widen what an in-flight run receives;
- a terminal run cannot stream, publish an artifact, read a Secret, add an
  Issue comment, download a plugin, or make a managed model call — a leaked but
  unexpired run token is refused once the run is over; and
- `getTaskRun` remains the one exemption, because a restarted worker must read a
  run in any status to discover it is already terminal.

A route×state matrix test enumerates every worker route and its allowed TaskRun
states, including the leaked-token-after-completion case. This is application
authorization on top of the network boundary, agreeing with [Space Secrets and
run delivery](space-secrets.md) §7.

## 9. Lifecycle And Availability

### 9.1 Startup

The Server constructs both route sets before opening either listener. When
worker execution is enabled, failure to bind or configure the worker listener
fails Server startup; accepting user tasks while no worker can report them is
not a degraded mode.

The worker listener's TLS configuration is validated before the scheduler
starts. A Server must not schedule Jobs with an HTTP URL and discover the
misconfiguration only inside the Pod.

### 9.2 Shutdown

Graceful shutdown preserves the ability of a running worker to report:

1. mark public readiness false and stop scheduling new runs;
2. quiesce and drain public requests and conversation turns;
3. keep the worker listener open for the worker-reporting portion of the
   existing shutdown budget;
4. drain the worker listener; and
5. close stores and exit.

The worker listener therefore closes after the public listener, not alongside
it. Raising its drain window must remain inside the Pod's
`terminationGracePeriodSeconds` contract in
[graceful-shutdown.md](graceful-shutdown.md).

### 9.3 Multiple Replicas

The internal Service may load-balance successive requests from one worker
across Server replicas. Run-token verification and durable TaskRun transitions
already use shared state and must remain replica-independent. This record does
not repair the in-memory WebSocket and turn-queue limitations tracked by
`ROADMAP.md` R1.

## 10. Configuration Shape

The intended `server.yaml` shape is:

```yaml
port: 5678

worker_api:
  listen: "127.0.0.1:5679"
  tls:
    cert_file: ""
    key_file: ""
    client_ca_file: ""       # optional native mTLS

worker:
  run_mode: k8s_job
  server_url: https://buildmax-worker-api.buildmax.svc.cluster.local:5679
  allow_insecure_http: false
  server_ca_file: /buildmax/tls/worker-api-ca.crt
  client_cert_file: ""       # optional native mTLS
  client_key_file: ""
```

The exact field names become part of
`internal/config/server_config.go`, `config-examples/server.example.yaml`, and
[Configuration reference](../reference/configuration.md) in one change. No
environment-variable alternative is added: listener and trust configuration
are structured deployment policy, not bootstrap secrets.

Validation rules:

| Condition | Result |
|---|---|
| `worker_api.listen` equals the public listener | Refuse startup |
| Exactly one of TLS certificate or key is set | Refuse startup |
| Worker URL is HTTPS but no usable trust roots exist | Refuse startup |
| `k8s_job` uses HTTP without `allow_insecure_http` | Refuse startup |
| mTLS client certificate and key are incomplete | Refuse startup |
| Worker URL points at the public listener | Refuse startup when both addresses can be resolved from configuration |

Paths are configuration values and may differ between Server and worker
mounts. The production manifest mounts the Server key only into Server Pods and
the CA into workers.

## 11. Options Considered

### 11.1 Deny `/api/worker` At The Ingress

Rejected as the boundary. Ingress behavior is controller-specific, direct
Service and Pod-IP access bypass it, and the public listener would still
contain the handlers. It remains useful defense in depth.

### 11.2 Add A Second Service On The Existing Port

Rejected. Both Services would name the same socket and route set, so there
would be no authorization boundary for Kubernetes to enforce.

### 11.3 One Listener With mTLS On Worker Paths

Rejected. Go TLS client authentication is negotiated before an HTTP path is
known; requiring a client certificate on one path but not another needs another
TLS terminator or listener. It also leaves worker handlers registered on the
public mux.

### 11.4 Two Listeners In One Process

Chosen. It creates a real port and route boundary with a small operational
change, preserves one scheduler and service graph, and can later be split into
another process without changing the worker protocol.

### 11.5 Separate Worker-Control Deployment Immediately

Deferred. It provides the strongest process boundary, but duplicates bootstrap,
health, rollout, and database connectivity before deployment evidence shows
that cost is necessary. The two-listener design keeps this migration possible.

## 12. Implementation Plan

### M1. Route And Listener Separation — shipped

- Build distinct public and worker muxes.
- Run two coordinated `http.Server` instances with the existing timeout and
  logging policy.
- Register worker routes only on the internal mux.
- Add configuration parsing and fail-closed validation.
- Preserve the exact OpenAPI route test over the union, with a new placement
  assertion.

Acceptance: a valid run token cannot reach a worker handler through the public
listener, and a valid user token cannot reach a public handler through the
worker listener.

### M2. TLS And Worker Client Trust — shipped (CA mount into Jobs is M3)

- Add native TLS to the worker listener.
- Give `workerclient` an explicit, reusable HTTP client built from the configured
  trust roots rather than `http.DefaultClient`.
- Mount the private CA certificate into worker Jobs.
- Refuse insecure Kubernetes worker URLs by default.

Acceptance: the worker completes a run through HTTPS, rejects the wrong server
name and wrong CA, and never falls back to HTTP.

### M3. Kubernetes Boundary — shipped in the reference manifests (kind HTTPS is M5)

- Add the internal Service and worker port to production and kind manifests.
- Point Ingress only at the `buildmax-api` Service.
- Label generated Jobs and Pods consistently.
- Add the Server ingress `NetworkPolicy` for the worker port.
- Keep worker ServiceAccount-token automount disabled.

Acceptance: a labeled worker Pod reaches the worker listener; an unlabeled Pod
in the same namespace cannot; neither can reach a worker handler through the
`buildmax-api` Service.

### M4. Route Lifecycle Authorization — shipped

- Claim a run before releasing Space Secret material.
- Enforce a status matrix for every worker route.
- Resolve Secret consumption from the pinned Agent revision.
- Refuse every worker capability after terminal state except a deliberately
  idempotent terminal report already committed by that run.

Acceptance: the route-table test proves both credential scope and lifecycle
scope, including a leaked but unexpired token used after completion.

### M5. Deployment Evidence — shipped

- Update Compose for explicit development HTTP.
- Update kind to exercise HTTPS and the NetworkPolicy denial case.
- Update the production reference and its configuration-parsing test.
- Run the normal worker TaskRun and managed-inference smoke through the internal
  Service.

Acceptance: the successful smoke and the denied cross-Pod probe are artifacts
of the same deployment, not separate hand-built reproductions.

## 13. Validation

### 13.1 Deterministic Tests

- Public mux contains no route whose pattern starts `/api/worker/`.
- Worker mux contains only worker route patterns.
- Every worker route rejects no token, a user token, another run's token, an
  expired token, and a token signed by another deployment.
- Every worker route enforces its allowed TaskRun states.
- Listener configuration rejects port collision and incomplete TLS material.
- Worker TLS rejects a wrong hostname, wrong CA, expired certificate, and an
  absent client certificate when mTLS is enabled.
- The Kubernetes Job carries worker labels, the internal URL and CA mount, but
  not the Server private key or database/JWT credentials.

### 13.2 Deployment Tests

The kind smoke must prove all of the following in one installed topology:

1. public API and Portal traffic still enter through the `buildmax-api`
   Service;
2. `/api/worker/*` on the `buildmax-api` Service returns `404`;
3. a labeled worker completes a TaskRun over HTTPS;
4. an unlabeled probe Pod cannot connect to the worker port;
5. the worker rejects a certificate not valid for the Service DNS name;
6. cancellation, heartbeat, stream, artifact, Secret, plugin, and managed LLM
   paths still work through the internal listener; and
7. graceful shutdown leaves enough time for an in-flight worker terminal
   report.

The production manifest parsing check must assert the two ports, HTTPS URL,
TLS mounts, Service types, Ingress backend, labels, and policy selectors. A
YAML file that merely parses is not evidence the boundary is connected.

## 14. Risks And Open Questions

| Question | Initial answer |
|---|---|
| Does a second listener imply a second binary? | No. Start with one process and two muxes; split only with deployment evidence |
| Is `ClusterIP` sufficient without `NetworkPolicy`? | No. It prevents deliberate external service exposure but does not prevent direct cluster access |
| Is TLS without mTLS sufficient? | It protects the token and verifies the Server; NetworkPolicy plus the run token authenticates the caller. Per-Pod mTLS is preferred where workload identity exists |
| Should the public OpenAPI describe worker routes? | Yes initially, provided registration tests prove description is not reachability |
| Can worker egress become default-deny in this change? | No. Pod-wide destination enforcement is conditional post-Beta hardening in `trust-harness.md` §3.9 |
| Should Server and workers move to different namespaces? | Compatible follow-up; not required to create the listener boundary |
| How are certificates issued and rotated? | Operator/platform supplied; restart-based rotation first, live reload only if operating evidence requires it |

The largest implementation risk is producing two names for one socket and
calling it isolation. The acceptance tests are framed around routes and ports
for that reason.

## 15. Documentation Changes

When the design ships:

- [Configuration reference](../reference/configuration.md) documents the two
  listeners and TLS fields;
- [Production deployment](../../deployment/production/README.md) explains the
  certificate, Service, and policy contract;
- [Server architecture](../contribute/architecture/server.md) records the two
  muxes and shutdown order;
- [Worker run token](worker-run-token.md) stops describing network reachability
  as if token scope were the whole boundary;
- [Trust harness](trust-harness.md) marks the Server-control-channel part of
  §3.9 closed while deferring general worker egress to evidence; and
- [Support matrix](../../manual/support.md) describes the deployed boundary only
  after the kind denial probe is part of the normal smoke.
