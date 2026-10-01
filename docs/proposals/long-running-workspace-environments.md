# Long-Running Workspace Environments

> **简体中文：** [阅读中文镜像](../zh-CN/proposals/long-running-workspace-environments.md)
>
> **Audience:** contributors, product designers, and operators · **Status:** proposal — under discussion
>
> **Opened:** 2026-10-01
>
> **Primary domain:** Product and Execution Model

Related: [roadmap](../ROADMAP.md), [current state](../current-state.md),
[Agent execution and Task threads](../design/agent-execution-and-task-threads.md),
[Task workspace checkpoints](../design/task-workspace-checkpoints.md),
[Remote Control](../design/remote-control.md),
[Agent sandbox policy](../design/agent-sandbox-policy.md),
[Plugin distribution](../design/plugin-space-distribution.md),
[client surface convergence](client-surface-convergence.md), and
[server architecture](../contribute/architecture/server.md).

## Contents

- [1. Decision Question](#1-decision-question)
- [2. User Outcome and Evidence](#2-user-outcome-and-evidence)
- [3. Current Constraints](#3-current-constraints)
- [4. Goals and Non-Goals](#4-goals-and-non-goals)
- [5. Proposed Model](#5-proposed-model)
- [6. Lifecycle and Persistence](#6-lifecycle-and-persistence)
- [7. Interaction Through Remote Control](#7-interaction-through-remote-control)
- [8. Authorization and Trust Boundary](#8-authorization-and-trust-boundary)
- [9. Provisioning, Reconciliation, and Failure](#9-provisioning-reconciliation-and-failure)
- [10. API, Portal, and Operator Surface](#10-api-portal-and-operator-surface)
- [11. Options and Trade-Offs](#11-options-and-trade-offs)
- [12. Smallest Validation Slice](#12-smallest-validation-slice)
- [13. Open Questions and Decision Evidence](#13-open-questions-and-decision-evidence)
- [14. Likely Destination if Accepted](#14-likely-destination-if-accepted)

## 1. Decision Question

Should BuildMax add a distinct, Space-scoped **Environment plane** for a
long-running Agent workspace, while reusing Remote Control for live interaction?

This paper recommends validating that direction. An Environment is not a
permanently open TaskRun and does not replace Task/TaskRun. It owns provisioned
compute, a persistent private workspace, and the runtime state needed to resume
interactive Agent sessions. Task/TaskRun remains the durable bounded-execution
plane for scheduled, workflow, issue, and background work.

The narrow first product is a remotely hosted Agent session, not a complete
browser IDE. One Environment runs one supervised interactive Agent session over
one private workspace. Portal observes and steers it through the Remote Control
interaction path. Terminal, arbitrary applications, shared editing, and public
service exposure require separate evidence.

## 2. User Outcome and Evidence

A person should be able to create an Environment, leave the browser, and return
hours or days later to the same workspace and Agent history without pretending
that one TaskRun is still executing. While the Environment is running, a test
server, watcher, download, or other background process may continue after the
browser disconnects. When compute stops or is replaced, the filesystem and
Agent session remain recoverable even though processes do not.

The motivating cases are work that crosses many interactive turns, needs a warm
toolchain or repository, or depends on a process whose useful lifetime is longer
than one bounded Agent turn. The existing
[client surface convergence proposal](client-surface-convergence.md) identified
the same cloud-hosted shape, and shipped Remote Control proves that BuildMax can
relay a live Agent session through Portal. Neither is evidence that people need
this often enough to justify its storage, isolation, and operating cost. The
request behind this paper is product evidence, not measured usage evidence.

The first decision therefore needs a bounded prototype and observed journeys,
not a general remote-development platform. Useful evidence is whether people
reconnect to the same Environment, whether retained processes or only retained
files matter, how long reservations actually remain active, and whether the
narrow Agent surface is sufficient without a full terminal.

## 3. Current Constraints

- **Task/TaskRun is deliberately bounded.** A TaskRun materializes a workspace,
  performs one turn or attempt, commits result and checkpoint state, and
  terminates. Task continuity is durable state, not process residence. Keeping a
  TaskRun alive would weaken its cancellation, result, quota, recovery, and
  worker-reclamation semantics.
- **Worker compute is ephemeral.** The supported Kubernetes path launches a Job
  for a run. Its writable root is run-scoped, and the Task workspace checkpoint
  in object storage—not a Pod volume—is the durable boundary.
- **Remote Control is live interaction, not hosting.** It registers an
  account-scoped local session, relays bounded events, prompts, approvals,
  questions, and cancellation, and keeps no authoritative transcript. Its user
  JWT and account ownership are correct for a person's laptop, not for a
  Space-owned remote Environment.
- **Space is the Portal ownership and authorization boundary.** Any server-side
  Environment must belong to exactly one Space. Solo use remains a personal
  Space and does not require a second product model.
- **The runtime is already shared.** `internal/agentapp` assembles models, tools,
  MCP, hooks, sandbox, traces, Skills, sessions, and workspace resolution for
  the existing surfaces. The proposal adds a host lifecycle, not another Agent
  loop.
- **Network-reachable execution has the worker trust posture, not the local CLI
  posture.** Sandbox enforcement must fail closed, runtime state and credentials
  stay outside the tool-writable workspace, and an unavailable required
  boundary must make the Environment unavailable rather than silently broadening
  access.
- **Nothing like this ships today.** There is no Environment entity,
  provisioner, persistent Environment volume, lease, Environment credential,
  or Portal management surface.

## 4. Goals and Non-Goals

### 4.1 Goals

- Keep one private workspace and its Agent session usable across browser
  disconnects and compute restarts.
- Let background processes continue while the Environment is ready, independent
  of whether a browser is connected.
- Give creation, start, stop, lease renewal, failure, and deletion explicit,
  inspectable states with bounded resource use.
- Reuse Remote Control's typed event and command path for conversation,
  streaming, approvals, questions, cancellation, presence, and reconnect.
- Preserve Space authorization, explicit Plugin activation, managed inference,
  trace redaction, and fail-closed sandbox enforcement.
- Make the durable and non-durable boundaries obvious to users and operators.

### 4.2 Non-Goals

- Making TaskRun permanent, dispatching ordinary Tasks into an Environment, or
  changing Task result authority.
- Guaranteeing that processes survive stop, suspension, node loss, image
  replacement, or control-plane recovery.
- Synchronizing a user's laptop filesystem or silently writing Environment
  changes back into mutable Space files.
- Shipping a full remote desktop, VS Code replacement, arbitrary browser
  applications, or public ingress to processes inside the Environment.
- Collaborative shell access or making every Space member an implicit operator
  of another member's Environment.
- Durable server storage of the interactive transcript merely because Remote
  Control relays it.
- Supporting every deployment topology in the first slice. A topology that
  cannot enforce the required isolation and persistence should report the
  capability unavailable.

## 5. Proposed Model

The proposal adds one durable product entity: **Environment**. It is the
Space-owned reservation for a private workspace and the compute that may attach
to it. It is not an Agent, Task, TaskRun, local Project, or generic versioned
filesystem.

| Concept | Owns | Does not own |
|---|---|---|
| Environment | Space, interactive operator, desired and observed lifecycle, resource profile, lease, persistent workspace reference, host health | A Task result, an indefinitely running model call, or a public service endpoint |
| Environment host | Reconciliation heartbeat and one supervised interactive Agent runtime inside the allocation | Durable product identity or authorization policy |
| Agent session | Model-visible history and compaction state stored inside the Environment's runtime home | Environment lifecycle or Space membership |
| Remote Control registration | Transient presence, stream key, and command routing for the live Agent session | Workspace or transcript durability |

The Environment belongs to one Space and initially has one interactive
operator. The operator must remain a member of that Space. Space owners and
admins may stop or delete the Environment for governance, but do not silently
gain interactive access to its shell or Agent session. Sharing is an explicit
future grant if evidence requires it; it is not implied by membership.

The allocation has three filesystem areas with enforced boundaries:

```text
persistent workspace/       Agent-visible, the only writable tool root
persistent buildmax-home/   sessions, traces, settings, resolved Plugins; hidden from tools
ephemeral scratch/          sockets, caches, process-local temporary state
```

`buildmax-home/` is persistent because the outcome includes Agent-session
continuity, but it is not placed below the writable workspace. The Environment
host is a process inside the allocation, not a second server and not a new
domain entity. The first slice supervises one Agent session; multiple concurrent
sessions would add scheduling, resource, and presentation semantics that have
not been demonstrated.

## 6. Lifecycle and Persistence

An Environment has asynchronous desired state and observed status. The exact
stored representation belongs in a later design, but the user-visible lifecycle
must distinguish at least:

```text
create -> provisioning -> ready <-> stopping -> stopped
                         |                    |
                         +------ failed <-----+

stopped / failed -> deleting -> gone
```

- **Create** reserves the resource and provisions storage before reporting it
  ready. A request returning successfully does not claim that compute is ready.
- **Ready** means the host is connected and the required sandbox, storage, and
  server channels passed their startup checks. Browser presence is irrelevant.
- **Stop** terminates compute after a grace period and keeps the persistent
  workspace and runtime home. It does not promise process suspension.
- **Start** attaches compute to the same durable state and restores the Agent
  session before accepting a prompt. Restore failure is visible and fails
  closed; it does not silently create a fresh session under the old identity.
- **Delete** first makes compute unreachable, then destroys the Environment's
  durable storage according to an explicit retention policy. It is distinct
  from Stop and requires a destructive-action confirmation in Portal.

“Long-running” means compute may remain ready across many turns and browser
disconnects; it does not mean unbounded or immortal. Each active Environment has
a renewable wall-clock lease. Authenticated use or an explicit renewal may
extend it within the Space and deployment limits. A host heartbeat alone does
not renew it, because that would turn every abandoned Environment into a
permanent reservation. The first slice should use a clear expiry rather than
guessing idleness from CPU or terminal activity.

The persistence promise is intentionally narrow:

| Event | Workspace and Agent session | Background processes |
|---|---|---|
| Browser disconnect or Server replica change | Preserved | Continue |
| Clean host-process restart in the same allocation | Preserved | May be lost |
| Stop then Start, or workload replacement with storage reattached | Preserved | Lost |
| Storage loss | Unavailable unless a later backup policy exists | Lost |
| Delete after the retention boundary | Destroyed | Lost |

## 7. Interaction Through Remote Control

Remote Control is the interaction substrate, with one important rule: **reuse
the protocol and relay, not its account-scoped ownership model**.

The Environment host opens an outbound WebSocket to the Server and uses the
existing typed envelope, bounded/redacted run events, heartbeats, buffered
stream, cross-replica command routing, and inbound prompt, approval, question,
and cancel messages. The same Portal components can render the stream and
pending decisions. Outbound dialing also avoids making each Environment a
directly reachable network server.

The admission path differs from a laptop session:

- the host authenticates with a short-lived, revocable Environment credential
  bound to one Environment and Space, never with the creating user's refresh
  token or a general worker token;
- registration attaches the live session to the Environment, and browser
  operations authorize through Environment and Space membership before reaching
  the shared relay;
- the existing account-scoped RemoteSession rule remains correct for local
  devices. Implementation may generalize the live-session registry, but it must
  not make Space membership a route into a member's laptop;
- environment readiness and live Agent-session presence are separate facts. An
  Environment can be healthy while its Agent runtime is restarting, and an
  offline session does not by itself authorize reclamation.

Existing Remote Control replays only a short live buffer and deliberately keeps
no durable transcript. An Environment must therefore restore history from its
own persisted Agent session. The smallest extension is a bounded history
snapshot or replay generated by the Environment host when a viewer attaches;
the Server remains a relay rather than a second transcript store. The prototype
must prove that a reload after the relay buffer expires still reconstructs an
understandable session.

This reuse yields the narrow cloud-host quadrant already identified by the
[Remote Control design](../design/remote-control.md): cloud host plus Agent
session surface. A broad codespace surface—terminal, files, arbitrary
applications—can be added only after the narrow Environment proves useful.

## 8. Authorization and Trust Boundary

An Environment executes model-selected commands for much longer than a worker
Job, so time increases exposure; it does not justify a weaker boundary.

- **Authorization:** every control operation resolves the Environment's Space.
  The interactive operator can attach while still a member. Owners/admins may
  govern lifecycle and quota without receiving transcript or shell access.
  Removing or disabling the operator revokes new interaction and requests a
  stop; the exact grace policy is an open question.
- **Credential:** the host receives one narrowly scoped Environment credential
  and exchanges or renews it through the control plane. It receives no database,
  object-store, model-provider, user refresh-token, or cluster credential.
- **Inference and applications:** managed inference and any future application
  broker remain server-mediated. Secrets are never materialized into the
  Agent-visible workspace merely to keep the Environment warm.
- **Filesystem:** tools see only `workspace/`. Persistent runtime home and
  ephemeral control files stay outside the tool root, matching the worker
  invariant.
- **Sandbox and outer runtime:** required command confinement is enabled and
  fail-closed. The workload also needs a qualified Pod/container boundary,
  resource limits, a read-only image root, and network policy. The model cannot
  select or weaken these controls.
- **Plugins, hooks, and MCP:** only the Space's explicit server-resolved Plugin
  activation enters the Environment. Resolution happens at a documented
  boundary such as Environment start or new Agent-session start; nothing
  hot-loads into a running process. Unsupported stdio MCP remains disabled
  fail-closed until it has a confinement story.
- **Audit and trace:** create/start/stop/renew/delete and remote control actions
  record actor and Environment. Agent execution keeps a bounded redacted trace
  in the Environment runtime home; relay failure remains fail-open for the Agent
  run, but credential or sandbox failure is fail-closed for Environment
  readiness.

Public ingress to a process inside the workspace is not a small extension of
Remote Control. It adds routing, TLS, authentication, abuse, hostname, and data
exfiltration policy and remains out of scope.

## 9. Provisioning, Reconciliation, and Failure

Provisioning is a persisted reconciliation problem, not one long HTTP request.
The Server records desired state; a controller converges compute and storage;
the Environment host reports health. A Server restart must not terminate a
healthy Environment, and a lost callback must not strand one forever in
`provisioning` or `stopping`.

The first supported deployment should use one isolated Kubernetes workload and
one persistent volume per Environment. This matches the private-deployment
topology and makes CPU, memory, storage, security context, network policy, and
reclamation inspectable. It does not require reusing the Task worker Job or its
run token. A local-process prototype may test interaction, but it is not
evidence for the supported multi-tenant boundary.

At minimum reconciliation handles these cases:

| Failure | Required outcome |
|---|---|
| Provisioning fails before a host is ready | Environment becomes diagnosably `failed`; retry does not create duplicate storage or workloads |
| Host heartbeat lapses while workload exists | Environment becomes unavailable; controller inspects or restarts the workload without deleting durable state |
| Workload or node disappears | Replacement attaches the same storage; processes are declared lost; session restore gates readiness |
| Server restarts or changes replica | Workload continues; registration reconnects; persisted desired state drives reconciliation |
| Stop races with Start or Delete | One serialized desired state wins; stale callbacks cannot resurrect compute |
| Lease expires | Stop is requested and eventually enforced even if no browser is connected |
| Storage cannot attach or restore | Environment is not reported ready; error names the failed boundary |
| Delete partly succeeds | Reconciliation continues until compute is unreachable and retention/storage cleanup reaches a recorded terminal outcome |

The controller also needs orphan detection in both directions: a database row
with no workload is reconciled, and a labeled workload or volume with no live
Environment row is quarantined and reclaimed by documented policy. Automatic
Agent-task replay is never part of recovery; an Environment is interactive
state, not an idempotent job.

## 10. API, Portal, and Operator Surface

A later design may change names, but the capability requires Space-scoped
operations equivalent to:

```text
POST   /api/spaces/{space_id}/environments
GET    /api/spaces/{space_id}/environments
GET    /api/spaces/{space_id}/environments/{environment_id}
POST   /api/spaces/{space_id}/environments/{environment_id}/start
POST   /api/spaces/{space_id}/environments/{environment_id}/stop
POST   /api/spaces/{space_id}/environments/{environment_id}/lease
DELETE /api/spaces/{space_id}/environments/{environment_id}
```

Creation and lifecycle commands are idempotent and return the durable resource;
clients observe readiness rather than holding the request open. Session stream
and command operations should delegate to the generalized Remote Control path
after Environment authorization, not create a parallel untyped chat protocol.

Portal needs only three initial surfaces:

1. an Environment list with operator, lifecycle, lease expiry, resource profile,
   last host signal, and a clear running-cost indicator;
2. an Environment detail page with Start, Stop, Renew, Delete, diagnostics, and
   the embedded Remote Control Agent-session view; and
3. administration visibility for active counts, resource totals, failures,
   expired leases, and forced stop/delete without transcript access.

Operator configuration needs an explicit enablement flag, image and resource
profile, maximum active Environments, per-Space limits, lease bounds, storage
class and size, startup timeout, and required runtime/sandbox policy. Defaults
must not silently allocate unbounded compute. Unsupported deployments report the
feature unavailable rather than emulating it with an endless local worker.

## 11. Options and Trade-Offs

| Option | Useful property | Cost or failure |
|---|---|---|
| **A. Distinct Environment plane plus shared Remote Control (recommended for validation)** | Matches the persistent workspace and interactive lifecycle; reuses the shipped control path without changing Task semantics | Adds provisioned compute, persistent storage, reconciliation, quotas, and a stronger long-lived trust boundary |
| **B. Keep one TaskRun alive** | Smallest apparent schema change | No authoritative terminal result, lease and interaction become worker exceptions, worker loss is ambiguous, and TaskRun reclamation no longer means what it says |
| **C. Continue through checkpoints on ephemeral workers** | Reuses the durable Task model and costs nothing while idle | Preserves files and Agent history but never background processes or warm state; still one bounded turn at a time |
| **D. Integrate an external codespace provider** | Outsources provisioning and browser IDE work | Adds provider credentials, availability, cost, and portability constraints before proving that the narrow Agent surface is useful |
| **E. Run long-lived sessions inside the Server or a shared worker** | Avoids per-Environment workloads | Mixes untrusted execution with the control plane or tenants, weakens resource isolation, and makes one failure affect unrelated Environments |

Option C already serves work that only needs continuity between turns. The new
entity is justified only when retained processes, warm state, or immediate
interactive re-entry materially matter. That is the central evidence test for
Option A.

## 12. Smallest Validation Slice

The first prototype should deliberately omit terminal and browser-IDE features.
In one isolated Kubernetes test deployment it should:

1. create one Environment from one Space and reach `ready` through persisted
   reconciliation;
2. start or restore one interactive Agent session in its private workspace;
3. use the Remote Control stream to prompt it, observe output, answer one
   approval or question, and cancel a turn;
4. disconnect Portal, leave a bounded background process running, reconnect
   after the ordinary relay buffer is gone, and recover an understandable
   session plus the same workspace;
5. stop and start the Environment, proving files and Agent history persist while
   the background process is reported lost;
6. restart the Server and kill the Environment workload separately, proving the
   documented reconciliation and persistence outcomes;
7. prove cross-Space and non-operator interaction refusal, credential
   revocation, quota refusal, lease expiry, and fail-closed sandbox startup; and
8. delete the Environment and verify compute and storage reclamation with no
   orphaned externally reachable session.

The prototype should record time to ready, time to reconnect, active duration,
storage growth, restart/restore failures, compute cost, and which actions made a
user want a terminal. It should also compare the same multi-turn task through
Task Continue. If retained processes or warm state do not change the outcome,
the existing Task plane is simpler and should remain the answer.

No user documentation, compatibility promise, or general availability claim
follows from this slice. Its purpose is to decide whether the Environment plane
deserves product and operational ownership.

## 13. Open Questions and Decision Evidence

- What seeds the first workspace: a snapshot of Space files, a repository clone,
  or an explicit upload? Does any path need write-back, or is export through
  Artifacts sufficient?
- Is one interactive operator per Environment enough? What demonstrated journey
  requires explicit sharing, and what must an admin be able to inspect without
  gaining session access?
- What lease minimum, maximum, warning, and renewal policy matches observed work
  without making abandoned compute permanent?
- Is a bounded host-generated history snapshot sufficient for reconnect, or
  does the need point to the separate
  [durable Agent sessions proposal](durable-agent-sessions.md)?
- Which Plugin and workspace changes require an Agent-session restart versus a
  full Environment restart?
- What storage durability, backup, retention, and deletion guarantees can the
  first supported deployment honestly make?
- Does the supported boundary require gVisor or another outer runtime, and can
  the exact Environment image and nested command sandbox pass the trust harness
  without exceptions?
- When the operator loses Space membership or is disabled, should running
  compute stop immediately or after a short recovery window?
- Does evidence justify a terminal and file browser, or does the narrow Remote
  Control surface cover the real outcome?
- Is there ever a reason for a Task to target an Environment, or would that
  recreate two execution authorities and confuse TaskRun recovery?

A decision to proceed needs at least one named user journey where Task Continue
is insufficient, successful lifecycle and isolation evidence from the supported
deployment topology, a measured resource envelope, and an explicit owner for
reclamation and incident response. A technical demo of a persistent Pod alone
is not enough.

## 14. Likely Destination if Accepted

If accepted, the stable boundary—Environment as a Space-scoped execution plane,
Task/TaskRun separation, Remote Control reuse, lifecycle, persistence, and trust
model—moves into a Product and Execution Model design record. Kubernetes
provisioning and operating policy receive their own Operations and Deployment
specification only if implementation detail is large enough to justify it.

The roadmap then sequences a narrow cloud Agent session before any broad
codespace surface, and decomposed implementation work enters
[the backlog](../backlog/README.md) only after the validation gate and security
boundary are accepted. The client-surface proposal continues to own shared UI
and transport convergence; it does not own Environment lifecycle. This proposal
is deleted once its accepted rationale has moved, or deleted without replacement
if the evidence favors Task Continue or an external provider.
