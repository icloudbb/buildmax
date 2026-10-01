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
[client modes](../design/client-modes.md),
[Agent browser capability](../design/agent-browser-capability.md),
[Agent sandbox policy](../design/agent-sandbox-policy.md),
[Plugin distribution](../design/plugin-space-distribution.md),
[client surface convergence](client-surface-convergence.md), and
[server architecture](../contribute/architecture/server.md).

## Contents

- [1. Decision Question](#1-decision-question)
- [2. Positioning: The Agent's Own Cloud Machine](#2-positioning-the-agents-own-cloud-machine)
- [3. User Outcome and Evidence](#3-user-outcome-and-evidence)
- [4. Current Constraints](#4-current-constraints)
- [5. Goals and Non-Goals](#5-goals-and-non-goals)
- [6. Proposed Model](#6-proposed-model)
- [7. Ownership and Authorization](#7-ownership-and-authorization)
- [8. Machine Lifecycle and Persistence](#8-machine-lifecycle-and-persistence)
- [9. Interaction Through Remote Control](#9-interaction-through-remote-control)
- [10. Trust Boundary](#10-trust-boundary)
- [11. Provisioning, Reconciliation, and Failure](#11-provisioning-reconciliation-and-failure)
- [12. API, Portal, and Operator Surface](#12-api-portal-and-operator-surface)
- [13. Options and Trade-Offs](#13-options-and-trade-offs)
- [14. Smallest Validation Slice](#14-smallest-validation-slice)
- [15. Open Questions and Decision Evidence](#15-open-questions-and-decision-evidence)
- [16. Likely Destination if Accepted](#16-likely-destination-if-accepted)

## 1. Decision Question

Should BuildMax allocate and manage long-running, general-purpose cloud
machines—**Environments**—that a person puts to work only through an Agent
conversation, reached through the existing Remote Control path?

This paper recommends validating that direction. The system's new
responsibility is machine management: allocate, start, stop, renew, reclaim, and
delete a machine with a persistent workspace. Once the machine is running, it is
used exactly like the person's own laptop under Remote Control: the standard
`buildmax` runtime runs on it, registers a live session, and Portal observes and
steers that session. No new Agent loop, chat protocol, or execution plane is
introduced, and Task/TaskRun is unchanged.

## 2. Positioning: The Agent's Own Cloud Machine

BuildMax today has two working modes:

| Mode | Who runs the work | Machine | Interaction |
|---|---|---|---|
| Task and Workflow | The server dispatches a bounded TaskRun to an ephemeral worker | Allocated per run, reclaimed after it | Submit, observe, continue; the run's result is authoritative |
| Remote Control | The person's own long-running machine | The person's laptop; BuildMax does not manage it | A live Agent session relayed through the server |
| **Environment (this proposal)** | A long-running machine the system allocates | A cloud machine—initially one Kubernetes Pod with a persistent volume | The same Remote Control session as a laptop |

An Environment is a general-purpose computer in the cloud that belongs to the
Agent: the person describes the work, and the Agent operates the machine to do
it. BuildMax is a general-purpose Agent runtime, so the work is not limited to
software. It includes collecting and analyzing data and producing reports,
research and other work on the web through the browser, processing large
batches of documents or media, long downloads, conversions, and computations,
and building or running software.

The cloud IDE—Cloud9, Codespaces, Gitpod—is the precedent for the
machine-management half, not the product's scope. Its lifecycle carries over
unchanged: a workspace created from a source, a machine that can stop while its
disk persists, idle reclamation, quotas, and deletion. What changes is the
interaction. A cloud IDE gave a developer an editor and a terminal to operate
the machine by hand. An Agent can now do that operating for anyone, so the
interaction surface contracts to the Agent conversation: prompts, streamed
progress, approvals, questions, cancellation, and read-only review of what the
Agent produced.

Two consequences follow and shape the rest of this paper:

- **Machine management is a known problem.** Lifecycle, persistence, quota, and
  reclamation follow established cloud-IDE practice rather than a new design.
- **The interaction already exists.** Remote Control was built to steer a
  long-running machine BuildMax cannot reach. A cloud machine is a long-running
  machine too; reusing that path is the point, not an optimization.

## 3. User Outcome and Evidence

A person should be able to create an Environment, ask its Agent to do work,
close the browser, and return hours or days later to the same workspace and
conversation. While the Environment is running, a crawl, analysis, download,
build, or service the Agent started keeps running whether or not anyone is
watching. When the machine stops or is replaced, the files and Agent
session survive even though processes do not.

The motivating cases are work that crosses many interactive turns, needs warm
state—installed tools, downloaded data, a prepared repository—or depends on a
process whose useful life is longer than one bounded Agent turn—exactly the cases where a person would otherwise
keep their laptop open under Remote Control. The cloud machine removes the
laptop from that picture.

The evidence is a product request plus the shipped Remote Control, not measured
usage. The decision therefore needs a bounded prototype and observed journeys:
whether people reconnect to the same Environment, whether retained processes
or only retained files matter, how long Environments actually stay active, and
whether read-only review is enough without a terminal.

## 4. Current Constraints

- **Task/TaskRun is deliberately bounded.** A TaskRun materializes a workspace,
  performs one turn or attempt, commits result and checkpoint state, and
  terminates. Keeping one alive would break its cancellation, result, quota,
  recovery, and reclamation semantics.
- **Worker compute is ephemeral.** The supported Kubernetes path launches a Job
  per run; the durable boundary is the Task workspace checkpoint in object
  storage, not a Pod volume. The Server today manages Jobs, not long-lived
  workloads or persistent volumes.
- **Remote Control is account-scoped and opt-in from the TUI.** A live session
  belongs to one user, is reachable only by that user, and is enabled by the
  interactive TUI's `--remote-control` flag while logged in to a managed server.
  The server keeps only a short replay buffer, never a durable transcript.
  There is no headless way to host a Remote Control session.
- **Space is the Portal ownership boundary** for resources, quota, and
  governance. Remote Control is a deliberate, documented exception because a
  laptop has no Space.
- **The runtime is already shared.** `internal/agentapp` assembles models, tools,
  MCP, hooks, sandbox, traces, Skills, sessions, and workspace resolution for
  every surface. Managed mode already routes inference through the server.
- **The browser capability is local-only.** CLI and Desktop drive a Go-owned
  Chromium; workers and other unattended runs do not get it.
- **External credentials reach runs only by explicit grant.** Workers receive
  Space Secrets through run-scoped grants; nothing ambient is inherited.
- **Network-reachable execution has the worker trust posture.** Sandbox
  enforcement fails closed, and runtime state and credentials stay outside the
  tool-writable workspace.
- **Nothing like this ships today.** There is no Environment entity,
  provisioner, persistent volume, lease, machine credential, or management
  surface.

## 5. Goals and Non-Goals

### 5.1 Goals

- Allocate, start, stop, renew, reclaim, and delete cloud machines with
  explicit, inspectable states and bounded resource use.
- Keep one private workspace and its Agent session usable across browser
  disconnects and machine restarts.
- Let processes the Agent started keep running while the machine is up,
  independent of any viewer.
- Interact only through the Remote Control session, so a cloud machine and a
  laptop look and behave the same in Portal.
- Preserve explicit Plugin activation, managed inference, trace redaction, and
  fail-closed sandbox enforcement.
- Make durable and non-durable state obvious to users and operators.

### 5.2 Non-Goals

- **An editor, terminal, or file browser for the person to operate the machine
  directly.** This is a product principle, not a deferral: the Agent is the
  person's hands. The person still reviews results, read-only (§9.3).
- Making TaskRun permanent, or letting a Task target an Environment. That would
  create two execution authorities and make TaskRun recovery ambiguous.
- Guaranteeing that processes survive stop, node loss, image replacement, or
  control-plane recovery.
- Synchronizing a laptop filesystem, or writing Environment changes back into
  mutable Space files. Results leave as Artifacts, or through Git when the work
  is a repository.
- Public ingress or port forwarding to processes inside the machine. The Agent
  inspects web content, including servers it started, with the browser
  capability instead (§9.3).
- Shared interactive access, or Space membership implying access to another
  member's Environment.
- Durable server storage of the interactive transcript.
- Every deployment topology. A topology that cannot enforce the isolation and
  persistence below reports the capability unavailable.

## 6. Proposed Model

The proposal adds one durable entity, **Environment**: the record of one
allocated cloud machine. It owns the machine's lifecycle, never the Agent's
behavior.

| Concept | Owns | Does not own |
|---|---|---|
| Environment | Space, operator, desired and observed lifecycle, resource profile, lease, workspace source, persistent volume reference, machine health | The Agent session, a Task result, or a public endpoint |
| The machine | The standard `buildmax` runtime, hosting one Remote Control session headlessly | Product identity or authorization policy |
| Agent session | Model-visible history and compaction state, stored in the machine's runtime home | Machine lifecycle |
| Remote Control session | Presence, stream, and command routing for the live session | Workspace or transcript durability |

Inside the machine runs the same runtime the person runs on a laptop, in
managed mode, with Remote Control enabled. There is no separate "Environment
host" component: what a laptop needs a person to start, the machine starts at
boot. The only new runtime affordance is a headless way to host one Remote
Control session without a TUI—a mode the Remote Control design already lists as
additive for Desktop and print surfaces.

The machine has three filesystem areas with enforced boundaries:

```text
persistent workspace/       Agent-visible, the only writable tool root
persistent buildmax-home/   sessions, traces, settings, resolved Plugins; hidden from tools
ephemeral scratch/          sockets, caches, process-local temporary state
```

`buildmax-home/` is persistent because the outcome includes session continuity,
and it stays outside the tool root, matching the worker invariant. The machine
hosts one session; concurrent sessions would add scheduling and presentation
semantics nothing has demonstrated a need for.

The workspace starts empty by default; the Agent can fetch what the work needs.
Creation may optionally seed it from a Git repository, a snapshot of Space
files, or an upload. Nothing is written back automatically; the Agent publishes
Artifacts, or pushes to Git when the work is a repository.

## 7. Ownership and Authorization

This is the main decision the model raises. A laptop under Remote Control is
account-owned and has no Space. A cloud machine consumes the operator's
resources and needs quota, cost attribution, and governance, which Portal places
on Space. Two shapes are possible:

| Option | Property | Cost |
|---|---|---|
| **Account-owned**, like a laptop | Closest to "another of my machines"; the Remote Control ownership rule applies unchanged | Quota, cost, and admin governance have no Space to attach to; a second, account-level resource model appears in Portal |
| **Space-owned and Space-interactive** | Uniform with other Portal resources | Space membership becomes a route into a live session, which Remote Control deliberately refuses; the live-session registry must be generalized to Space authorization |

**Recommendation: split resource ownership from interaction.** The Environment
row belongs to a Space, which carries its quota, cost, lifecycle governance, and
audit. Interaction belongs to one person, the operator, and the machine's live
session registers as that person's Remote Control session—it appears in their
session list as one of their machines. The Remote Control rule stays exactly as
it is: a live session is reachable only by its owning account. The Remote
Control session gains only an optional reference to its Environment.

The resulting rules:

- The operator can attach while a member of the Space. Losing membership or
  being disabled stops the machine; the default is immediate, with the files
  retained until an owner deletes the Environment.
- Space owners and admins can see, stop, and delete any Environment in the Space
  and adjust quota. They gain no transcript or session access.
- Sharing interactive access is a future explicit grant if a journey requires
  it, never implied by membership.

## 8. Machine Lifecycle and Persistence

An Environment has asynchronous desired state and observed status. The stored
representation belongs in a later design; the user-visible lifecycle must
distinguish at least:

```text
create -> provisioning -> ready <-> stopping -> stopped
                         |                    |
                         +------ failed <-----+

stopped / failed -> deleting -> gone
```

- **Create** provisions storage and seeds the workspace before reporting
  ready. A successful request does not claim the machine is ready.
- **Ready** means the machine is up, its required sandbox, storage, and server
  channels passed startup checks, and its Remote Control session is registered.
  Browser presence is irrelevant.
- **Stop** terminates compute after a grace period and keeps the volume. It does
  not promise process suspension.
- **Start** reattaches the same volume and restores the Agent session before
  accepting a prompt. Restore failure is visible and fails closed; it never
  silently starts a fresh session under the old identity.
- **Delete** first makes the machine unreachable, then destroys its storage
  according to an explicit retention policy, behind a destructive-action
  confirmation in Portal.

Long-running does not mean immortal. Each running Environment has an idle
timeout measured on the operator's actions—attaching, prompting, answering, or
an explicit renewal—plus a hard maximum, both within Space and deployment
limits. A machine heartbeat or CPU activity never renews it; otherwise every
abandoned machine becomes a permanent reservation. A long-running process the
operator wants kept alive needs the operator to renew. A general-purpose
machine invites unattended services—a monitor, a bot—that no person renews;
those are hosting, not interactive work, and stay outside the first slice
(§15).

| Event | Workspace and Agent session | Processes |
|---|---|---|
| Browser disconnect or Server replica change | Preserved | Continue |
| Runtime process restart on the same machine | Preserved | May be lost |
| Stop then Start, or workload replacement with the volume reattached | Preserved | Lost |
| Volume loss | Unavailable unless a later backup policy exists | Lost |
| Delete after retention | Destroyed | Lost |

## 9. Interaction Through Remote Control

### 9.1 What Is Reused

The machine dials out to the Server over the existing Agent WebSocket and uses
the typed envelope, bounded and redacted events, heartbeats, the buffered
stream, cross-replica command routing, and inbound prompt, approval, question,
and cancel. Portal renders it with the existing Remote Control view. Outbound
dialing means no Environment is a directly reachable network server.

### 9.2 What Differs From a Laptop

- **Credential.** A laptop authenticates with its user's login. The machine
  receives a short-lived, revocable credential bound to one Environment and its
  operator, issued and renewed through the control plane. It is never the
  operator's refresh token or a worker token, and it cannot reach any other
  account resource.
- **Opt-in.** The machine always hosts Remote Control; that is its only purpose.
  The per-session opt-in that protects a laptop is replaced by the explicit act
  of creating the Environment.
- **Readiness versus presence.** A machine can be healthy while its runtime is
  restarting. An offline session alone never authorizes reclamation; the lease
  does.

### 9.3 Remote Control Improvements Both Hosts Share

Two gaps matter more for a cloud machine but are not specific to it, so they
belong to Remote Control and benefit laptops equally:

- **History on reattach.** Remote Control replays only a short buffer. A person
  returning after days needs the conversation, which the runtime already holds
  in its persisted session. The smallest fix is a bounded history snapshot the
  runtime sends when a viewer attaches; the Server remains a relay and stores no
  transcript.
- **Read-only review.** With no editor or terminal, the person still has to
  judge the Agent's work. The conversation must carry read-only results in
  general form: produced files and reports through Artifacts, screenshots, and,
  when the work is code, a workspace diff. The Remote Control design already
  names a workspace diff as part of the narrow surface; the prototype must
  confirm what Portal renders today and fill the gap.

The browser capability is central rather than incidental: much general work
happens on the web, and inspecting a web application the Agent started is the
same act. It replaces port forwarding. That capability is local-only today; the
Environment image must carry it under the Environment's sandbox, navigation,
and network limits, which is a validation item.

## 10. Trust Boundary

An Environment runs model-selected commands for much longer than a worker Job.
Time increases exposure; it does not justify a weaker boundary.

- **Credential:** the machine credential (§9.2) is its only server credential.
  The machine receives no database, object-store, model-provider, refresh-token,
  or cluster credential. Managed inference stays server-mediated, attributed to
  the Space and operator.
- **Filesystem:** tools see only `workspace/`. A persistent runtime home sitting
  next to a long-lived tool process is the new risk relative to a worker; the
  trust harness must prove tools cannot read it over a long session, not only
  that the path is excluded.
- **Sandbox and outer runtime:** command confinement is enabled and fail-closed.
  The workload has a qualified Pod or container boundary, resource limits, a
  read-only image root, and network policy. The model cannot select or weaken
  them.
- **Network egress:** general work needs wider internet access than a coding
  worker's package registries, and a long-lived machine with broad egress is an
  exfiltration and abuse surface. The egress default and its per-Space or
  per-Environment policy are an explicit operator decision, never a model's.
- **External credentials:** work that sends mail or calls third-party APIs
  receives credentials only through server-mediated grants—Space Secret grants
  or an application broker—never by materializing them in the workspace to keep
  the machine warm.
- **Plugins, hooks, and MCP:** only the Space's explicit, server-resolved Plugin
  activation enters the machine, resolved at Environment start or session start;
  nothing hot-loads. Stdio MCP stays disabled fail-closed until it has a
  confinement story.
- **Control-plane privilege:** managing long-lived workloads and persistent
  volumes extends the Server's cluster permissions beyond creating Jobs. That
  expansion is part of the security review, scoped to a dedicated namespace.
- **Audit and trace:** lifecycle actions and remote commands record actor and
  Environment. The runtime keeps its bounded, redacted trace in its home. Relay
  failure is fail-open for the Agent; credential or sandbox failure is
  fail-closed for readiness.

## 11. Provisioning, Reconciliation, and Failure

Provisioning is persisted reconciliation, not one long HTTP request. The Server
records desired state; a controller converges compute and storage; the machine
reports health. A Server restart must not terminate a healthy machine, and a
lost callback must not strand one in `provisioning` or `stopping`.

The first supported deployment is one isolated Kubernetes workload and one
persistent volume per Environment. It does not reuse the worker Job or its run
token. A local-process prototype may exercise interaction, but it is not
evidence for the multi-tenant boundary.

| Failure | Required outcome |
|---|---|
| Provisioning fails before the machine is ready | `failed` with a diagnosis; retry creates no duplicate storage or workload |
| Heartbeat lapses while the workload exists | Unavailable; the controller inspects or restarts the workload without touching the volume |
| Workload or node disappears | Replacement attaches the same volume; processes declared lost; session restore gates readiness |
| Server restarts or changes replica | Machine continues; Remote Control reconnects; desired state drives reconciliation |
| Stop races with Start or Delete | One serialized desired state wins; stale callbacks cannot resurrect compute |
| Idle timeout or hard maximum reached | Stop is requested and enforced with no browser connected |
| Volume cannot attach or session cannot restore | Not ready; the error names the failed boundary |
| Delete partly succeeds | Reconciliation continues to a recorded terminal outcome |

Orphan detection runs in both directions: a row with no workload is
reconciled, and a labeled workload or volume with no live row is quarantined
and reclaimed by documented policy. Recovery never replays Agent work; an
Environment is interactive state, not an idempotent job.

## 12. API, Portal, and Operator Surface

A later design may rename these, but the capability needs Space-scoped
lifecycle operations equivalent to:

```text
POST   /api/spaces/{space_id}/environments
GET    /api/spaces/{space_id}/environments
GET    /api/spaces/{space_id}/environments/{environment_id}
POST   /api/spaces/{space_id}/environments/{environment_id}/start
POST   /api/spaces/{space_id}/environments/{environment_id}/stop
POST   /api/spaces/{space_id}/environments/{environment_id}/lease
DELETE /api/spaces/{space_id}/environments/{environment_id}
```

Lifecycle commands are idempotent and return the resource; clients observe
readiness. Interaction adds no routes: it is the operator's existing Remote
Control session.

Portal needs three surfaces:

1. an Environment list with operator, state, lease expiry, resource profile,
   last machine signal, and a visible running-cost indicator;
2. an Environment detail page with Start, Stop, Renew, Delete, and diagnostics,
   which opens the existing Remote Control session view for the operator; and
3. administration visibility for active counts, resource totals, failures, and
   forced stop or delete, without session access.

Operator configuration needs an enablement flag, machine image and resource
profiles, maximum active Environments, per-Space limits, idle and maximum
lease bounds, storage class and size, startup timeout, and the required
runtime and sandbox policy. The default is disabled; nothing allocates compute
silently.

## 13. Options and Trade-Offs

| Option | Useful property | Cost or failure |
|---|---|---|
| **A. Managed cloud machines reached through Remote Control (recommended for validation)** | Machine management follows cloud-IDE practice; interaction, Agent runtime, and Portal view are reused; Task semantics untouched | Adds provisioning, persistent storage, reconciliation, quota, and a long-lived trust boundary |
| **B. Keep one TaskRun alive** | Smallest apparent schema change | No authoritative result; leases and interaction become worker exceptions; worker loss is ambiguous |
| **C. Task Continue on ephemeral workers** | Reuses the durable Task model and costs nothing idle | Keeps files and history, never processes or warm state; one bounded turn at a time |
| **D. Classic cloud IDE with editor and terminal** | Familiar; covers anything the Agent cannot do | Rebuilds what the Agent replaces; multiplies the surface and the trust boundary for direct shell access |
| **E. Integrate an external codespace provider** | Outsources provisioning | Provider credentials, cost, availability, and portability before the narrow surface is proven |
| **F. Long-lived sessions inside the Server or a shared worker** | No per-Environment workload | Mixes untrusted execution with the control plane or tenants |

Option C already serves work that only needs continuity between turns. A new
entity is justified only when retained processes, warm state, or immediate
re-entry materially change the outcome. That is the central evidence test.

## 14. Smallest Validation Slice

Two pieces can be built and judged before any machine management exists,
because they serve laptops too:

1. a headless Remote Control host mode of the `buildmax` binary; and
2. history snapshot on reattach, plus read-only review outputs in the session
   view (§9.3).

The Environment prototype then omits everything a cloud IDE would add for
direct operation. It runs two journeys: a non-coding one—the Agent collects and
analyzes web data over hours and delivers a report—and a coding one—the Agent
builds a project and verifies a running server with the browser. In one
isolated Kubernetes test deployment it should:

1. create one Environment and reach `ready` through persisted reconciliation,
   with the session appearing in the operator's Remote Control list;
2. prompt the Agent, observe output, answer one approval or question, and
   cancel a turn;
3. have the Agent run each journey's long process, using the browser capability
   under the Environment's network policy;
4. disconnect Portal, leave the process running, reconnect after the relay
   buffer is gone, and recover the conversation, workspace, and produced
   Artifacts;
5. stop and start the Environment, proving files and history persist and the
   process is reported lost;
6. restart the Server and kill the workload separately, proving the documented
   reconciliation outcomes;
7. prove non-operator and cross-Space refusal, credential revocation, quota
   refusal, idle expiry, membership-loss stop, and fail-closed sandbox startup;
   and
8. delete the Environment and verify compute and storage reclamation with no
   orphaned reachable session.

The prototype records time to ready, time to reconnect, active duration,
storage growth, restore failures, compute cost, and every moment a user wanted
an editor or terminal. It also runs both journeys through Task Continue. If retained processes and warm state do not change the outcome, the
Task plane is the simpler answer.

No user documentation, compatibility promise, or availability claim follows.
The slice decides whether machine management deserves product and operational
ownership.

## 15. Open Questions and Decision Evidence

Decided by the positioning in §2 and §7, subject to review:

- No editor, terminal, or file browser for direct operation; review is
  read-only.
- No Task targets an Environment.
- The Environment belongs to a Space; interaction belongs to the operator's
  account through Remote Control.
- Losing membership stops the machine immediately; files are retained.
- The workspace starts empty by default, with optional seeding from Git, Space
  files, or an upload, and no automatic write-back.

Still open:

- What idle timeout and hard maximum match observed work, and how far in
  advance does a person need warning before a stop?
- Is a bounded history snapshot enough for reconnect after days, or does the
  need point to the [durable Agent sessions proposal](durable-agent-sessions.md)?
- What read-only review set—Artifacts, screenshots, diffs—is sufficient, and
  does any observed journey still require the person to act directly?
- What egress default fits general work without turning a long-lived machine
  into an open proxy, and who sets per-Space exceptions?
- Which external credentials do observed journeys need, and are Space Secret
  grants enough or does the work require an application broker?
- Is there demonstrated need for unattended services on an Environment? If so,
  how would they relate to the existing Agent Schedules, which already run
  recurring work on the Task plane, and what lease model replaces operator
  renewal?
- Can the browser capability run inside the Environment's sandbox and network
  policy without weakening either?
- Which Plugin or workspace changes require a session restart versus a machine
  restart?
- What storage durability, backup, and retention can the first deployment
  honestly promise?
- Does the boundary require gVisor or another outer runtime, and can the
  machine image with the nested command sandbox pass the trust harness without
  exceptions?

A decision to proceed needs at least one named journey where Task Continue is
insufficient, lifecycle and isolation evidence from the supported topology, a
measured resource envelope, and an explicit owner for reclamation and incident
response. A persistent Pod demo alone is not enough.

## 16. Likely Destination if Accepted

If accepted, the stable boundary—Environment as managed cloud machines, the
split between Space ownership and account interaction, Remote Control reuse,
lifecycle, persistence, and trust—moves into a Product and Execution Model
design record. The headless host mode and history-on-reattach work extend the
[Remote Control design](../design/remote-control.md) directly, since laptops use
them too. Kubernetes provisioning and operating policy get their own Operations
and Deployment specification only if the detail justifies it.

Decomposed work enters [the backlog](../backlog/README.md) after the validation
gate and security boundary are accepted. The client-surface proposal keeps
shared UI and transport convergence; it does not own machine lifecycle. This
proposal is deleted once its rationale has moved, or deleted without
replacement if the evidence favors Task Continue or an external provider.
