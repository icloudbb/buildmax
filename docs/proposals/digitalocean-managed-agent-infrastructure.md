# DigitalOcean Managed Agent Infrastructure Assessment

> **Simplified Chinese:** [Read the derived translation](../zh-CN/proposals/digitalocean-managed-agent-infrastructure.md)
>
> **Audience:** contributors, operators, product designers, and security reviewers · **Status:** proposal — under discussion
>
> **Opened:** 2026-10-02

Related: [roadmap](../ROADMAP.md), [current state](../current-state.md),
[Agent execution and Task threads](../design/agent-execution-and-task-threads.md),
[Task workspace checkpoints](../design/task-workspace-checkpoints.md),
[LLM gateway](../design/llm-gateway.md),
[sandbox boundaries](../design/sandbox-boundaries.md),
[Space Secrets](../design/space-secrets.md),
[Agent delegation to user applications](agent-app-delegation.md),
[durable Agent sessions](durable-agent-sessions.md), and
[long-running workspace Environments](long-running-workspace-environments.md).

## Contents

- [1. Decision Question](#1-decision-question)
- [2. User Outcome, Evidence, And Current Constraints](#2-user-outcome-evidence-and-current-constraints)
- [3. Research Scope And Source Quality](#3-research-scope-and-source-quality)
- [4. DigitalOcean Product Architecture](#4-digitalocean-product-architecture)
- [5. Harness Runtime](#5-harness-runtime)
- [6. Action Gateway](#6-action-gateway)
- [7. Inference Engine And Agent Platform](#7-inference-engine-and-agent-platform)
- [8. Comparison With BuildMax](#8-comparison-with-buildmax)
- [9. Lessons For BuildMax](#9-lessons-for-buildmax)
- [10. Integration Options](#10-integration-options)
- [11. Candidate Direction](#11-candidate-direction)
- [12. Risks And Product Limits](#12-risks-and-product-limits)
- [13. Evidence-Producing Experiments](#13-evidence-producing-experiments)
- [14. Open Questions](#14-open-questions)
- [15. Likely Destination If Accepted](#15-likely-destination-if-accepted)

## 1. Decision Question

Should BuildMax use DigitalOcean's Inference Engine, Action Gateway, or Harness
Runtime as optional infrastructure backends, and which architectural lessons
should BuildMax adopt without making a public-preview cloud service part of its
domain model or weakening local and private deployment?

The candidate answer is deliberately asymmetric:

1. Treat DigitalOcean Inference as an ordinary model provider first.
2. Evaluate Action Gateway as an external credential and tool broker because it
   addresses a demonstrated gap in application delegation.
3. Evaluate Harness Runtime only through a bounded executor prototype after the
   private-deployment Beta gate.
4. Keep Space, Task, TaskRun, Workflow, authorization, authoritative results,
   checkpoints, and audit under BuildMax ownership in every option.

This paper does not commit any integration to the roadmap. It records an
external product assessment and names the evidence required before a direction
could be accepted.

## 2. User Outcome, Evidence, And Current Constraints

### 2.1 Essential outcome

A BuildMax deployment should be able to choose managed infrastructure for model
serving, governed tools, or isolated execution without changing the meaning of
a Task, losing authoritative run history, or making the deployment impossible
to operate locally or privately.

The concrete user outcome is portability of operation, not portability as an
abstract virtue: an operator may prefer Kubernetes they control, a managed
microVM service, or a mixture, while users still see the same Task history,
Artifacts, approvals, and failure semantics.

### 2.2 Evidence that the question matters

DigitalOcean made Managed Agents generally available as a public preview on
2026-09-21. It combines a microVM-based Harness Runtime with Action Gateway, a
managed MCP and credential-brokering service, while Inference Engine supplies
serverless, batch, routed, and dedicated model execution. The product directly
addresses work BuildMax currently carries in its Kubernetes worker, sandbox,
MCP, Secret, and managed-model paths.

The overlap is not evidence that BuildMax should outsource those paths. It is
evidence that their boundaries are becoming recognizable cloud product
boundaries and that BuildMax should be able to consume them when they improve a
real deployment.

### 2.3 Constraints that exist today

- BuildMax's current priority is a dependable private-deployment Beta. R2 and
  R3 qualification work takes precedence over an optional provider integration.
- The shared Go Agent runtime is used by CLI/TUI, Desktop, evaluation, and
  workers. Replacing it with a provider harness would split behavior across
  surfaces.
- TaskRun is the authoritative execution result and authorization envelope.
  An external session or run cannot replace it.
- Task workspace state persists as portable object-store checkpoints. An
  external machine snapshot may accelerate restoration but cannot be the only
  copy.
- The supported unattended worker refuses stdio MCP and permits remote MCP.
  This makes a remote Action Gateway integration materially closer than a new
  local tool process.
- BuildMax is intended for local and private deployment. A DigitalOcean adapter
  must remain optional, and its absence must not reduce the supported core.

## 3. Research Scope And Source Quality

This assessment was verified on 2026-10-02 against DigitalOcean's official
product and documentation pages, especially:

- [Managed Agents](https://docs.digitalocean.com/products/managed-agents/)
  and its [architecture](https://docs.digitalocean.com/products/managed-agents/agent-harness-runtime/concepts/architecture/);
- Harness Runtime concepts for
  [environments](https://docs.digitalocean.com/products/managed-agents/agent-harness-runtime/concepts/environments/),
  [sessions](https://docs.digitalocean.com/products/managed-agents/agent-harness-runtime/concepts/sessions/),
  [adapters](https://docs.digitalocean.com/products/managed-agents/agent-harness-runtime/concepts/agent-adapters/),
  [secrets](https://docs.digitalocean.com/products/managed-agents/agent-harness-runtime/concepts/secrets/), and
  [egress](https://docs.digitalocean.com/products/managed-agents/agent-harness-runtime/concepts/egress/);
- Action Gateway concepts for
  [sessions, actors, and connections](https://docs.digitalocean.com/products/managed-agents/action-gateway/concepts/overview/),
  [tool policy](https://docs.digitalocean.com/products/managed-agents/action-gateway/concepts/tool-policies/), and
  [reliable execution](https://docs.digitalocean.com/products/managed-agents/action-gateway/concepts/reliable-execution/);
- [Inference](https://docs.digitalocean.com/products/inference/),
  [Inference Router](https://docs.digitalocean.com/products/inference/how-to/use-inference-router/), and
  [Inference features](https://docs.digitalocean.com/products/inference/details/features/).

Only official sources are used for product claims. Marketing claims such as
tool counts and resume latency are treated as product descriptions, not as
BuildMax acceptance evidence. No live DigitalOcean account, paid inference,
Harness Runtime session, or Action Gateway connection was exercised for this
paper. Public-preview documentation changes quickly and contains inconsistencies
noted in section 12; observed API behavior must decide any implementation.

## 4. DigitalOcean Product Architecture

DigitalOcean presents an AI-native cloud with independently useful layers,
rather than a strict call stack:

```text
Application / CLI / webhook / schedule
                   |
          +--------+---------+
          |                  |
          v                  v
  Inference Agent       Managed Agents
      Platform       +------------------+
                     | Harness Runtime  |
                     | Action Gateway   |
                     +--------+---------+
                              |
                 +------------+------------+
                 v            v            v
             Inference      Storage       Data
                 |
        serverless / batch /
        dedicated / router
```

Managed Agents itself contains two services that can be used together or
independently:

- Harness Runtime runs an agent or code in a session-scoped microVM.
- Action Gateway exposes governed tools through MCP or SDKs and executes calls
  with credentials held outside the agent.

Inference Engine is a sibling service, not a mandatory dependency. Harness
Runtime can use an external provider key, and Action Gateway can be used by an
agent running outside Harness Runtime.

Inference's Agent Platform is a separate RAG/chat-agent product. It manages
instructions, models, knowledge bases, guardrails, function routes, child-agent
routing, evaluations, and a chat-compatible endpoint. It should not be confused
with Harness Runtime's general code-execution environment.

## 5. Harness Runtime

### 5.1 Environment, Session, And Run

Harness Runtime separates three lifecycles:

| Concept | Owns | Lifecycle |
|---|---|---|
| Environment | Adapter, template, size, skills, tools, credentials, permissions, and egress | Immutable after creation; reused to create sessions |
| Session | One sandbox, workspace state, event history, and supported agent history | Create, pause, resume, checkpoint, fork, rollback, remove |
| Run | One agent turn inside a session | Start, stream/watch, cancel, complete or fail |

One environment may create many sessions, and one session may accumulate many
runs. This is the closest infrastructure-level analogy to BuildMax's resolved
Agent environment, Task execution continuity, and TaskRun, but their ownership
semantics differ as section 8 explains.

### 5.2 Isolation And persistence

Each session receives a lightweight Firecracker microVM. Hypervisor isolation,
not container namespaces, separates sessions. A session sandbox supplies CPU,
memory, filesystem, networking, and a toolchain. The session is the unit of
isolation: processes within one session share its files and ordinary injected
credentials.

Pausing freezes processes, memory, and filesystem state and stops compute
charges. Checkpoints capture machine state; forks create independent sessions;
rollback replaces the live sandbox while retaining the session identifier.
Support varies by adapter, and a fork starts a fresh transcript even though it
inherits machine state.

Workspace is also available as a separate mounted drive whose lifecycle can
outlive the session. DigitalOcean therefore offers three persistence forms:
live session state, whole-machine checkpoints, and a separately durable file
volume.

### 5.3 Adapters And templates

Named adapters include Codex CLI, Claude Code, OpenCode, Hermes, LangGraph, and
an OpenAI-managed Codex environment. A custom adapter accepts a supplied image
and entrypoint but has less structured event translation. BYOT images are built
on supported DigitalOcean base templates so the platform can provide runtime
wiring and event handling.

Adapter choice determines capability. The OpenAI-managed Codex variant retains
sandbox lifecycle and billing but does not provide DigitalOcean's event stream,
permission enforcement, approvals, checkpoints, forks, or rollback because
DigitalOcean does not own the Agent loop. This is strong evidence for explicit
backend capability negotiation rather than one Boolean saying an executor is
supported.

### 5.4 Permissions, approvals, egress, and credentials

Harness permission rules resolve Agent actions to `allow`, `ask`, or `deny`.
Approvals can be answered inline or out of band. They are distinct from network
egress and provider authorization.

Sandbox egress is unrestricted when omitted. Naming one host activates an
allowlist, after which unnamed destinations are denied and required platform,
adapter, and configured inference hosts may be merged in. Action Gateway calls
do not require the provider host in sandbox egress because the gateway, rather
than the sandbox, makes the provider request.

Harness distinguishes configuration and credentials by whether the real value
enters the sandbox:

| Declaration | Stored safely outside the saved spec | Real value readable in sandbox |
|---|---:|---:|
| `env` | No | Yes |
| ordinary `secrets` | Yes | Yes |
| scoped secret | Yes | No; the sandbox receives a handle bound to one HTTPS destination |
| Action Gateway connection | Yes | No value or handle is delivered to the sandbox |

Scoped-secret redemption was still rolling out at the time of review and must
be qualified before production use.

### 5.5 Triggers, observability, limits, and billing

Cron and signed webhook triggers can create a fresh session for each firing or
reuse a paused session. Trigger executions are de-duplicated and record their
session. Unattended policies have surprising behavior: the trigger rejects an
explicit `ask` policy at creation, but an unexpected runtime prompt on a reused
session may be auto-approved so the run can finish. A consequential action must
therefore be denied, not merely set to ask, for unattended work.

DigitalOcean Insights exposes session lifecycle, resource, token, and approval
metrics. Public-preview limits include account-dependent concurrent-session
caps, a default fifteen-minute idle pause, and paused sessions continuing to
occupy active-session capacity. Run-lifecycle webhooks are not currently
available, so an external control plane must observe or poll rather than depend
on a terminal callback.

## 6. Action Gateway

### 6.1 Resource model

Action Gateway separates integration definition, external identity, credential
authorization, and per-client policy:

```text
Provider -> Tools
             |
Actor -> Connection -> provider account and credentials
  |
  +-> Gateway Session
        |- selected tools or versioned Toolbelts
        |- allow / ask / deny policy
        |- optional argument conditions
        |- preloaded tools
        |- output views
        `- managed MCP URL
```

An actor is an application-chosen identity that selects connections; it is not
a login or credential. A connection authorizes a provider account for one actor
and is also owned by the DigitalOcean user who created it. Matching an actor ID
does not grant access to another DigitalOcean user's connection.

The provider's credential scopes remain the outer authority. Gateway policy
cannot reduce an overbroad token's provider-side privileges; it only controls
which operations the session may request through the gateway.

### 6.2 Discovery and model context

The default MCP surface exposes three meta-tools rather than every catalog
schema:

- `action_search` finds relevant eligible tools and returns their schemas;
- `action_invoke` invokes one or more named tools, including parallel calls;
- `action_code` runs ephemeral Python and can combine governed tool calls.

Known tools may be preloaded. Output views project a tool's result to selected
fields before it reaches the model. These are separate controls: preloading
changes schema context, output views change result context, tool selection
bounds eligibility, and policy controls execution.

### 6.3 Governance and reliable execution

Tool selection and permission are independent. A tool outside the selection is
denied even if a policy rule would allow it. `deny` cannot be overridden by an
approval. Out-of-band approval is bound to the team, session, tool version, and
arguments; it is consumed once.

Retries are tool-specific and bounded. Automatic replay is permitted only when
the catalog establishes a safety contract such as read-only behavior or a
provider idempotency key. A timeout does not prove a write failed, and separate
tool requests or repeated code executions are separate logical invocations.
Multi-tool code is not a transaction.

Every gateway call is traced, with aggregate request, success, tool, provider,
and latency views. These operational views can lag and are not a replacement
for BuildMax's authoritative TaskRun provenance.

## 7. Inference Engine And Agent Platform

Inference Engine combines:

- Serverless Inference for synchronous and asynchronous model APIs;
- Batch Inference for high-volume non-real-time jobs;
- Dedicated Inference for managed GPU endpoints and supported BYOM models;
- Inference Router for task classification, model pools, cost/latency policies,
  cache-aware selection, ordered fallback, and failover;
- model and router evaluation; and
- the separate Agent Platform.

The serverless API uses `https://inference.do-ai.run` and offers OpenAI- and
Anthropic-shaped endpoints including Chat Completions, Responses, Messages,
embeddings, image, audio, and text-to-speech operations. Compatibility is not
complete for every provider-specific feature, so capability qualification is
still necessary.

Dedicated Inference manages Kubernetes, ingress, vLLM, model storage, RDMA,
multi-node serving, autoscaling, prefix-aware routing, and parallelism. These
are model-serving infrastructure concerns BuildMax has no reason to reproduce
without a specific private-model requirement.

Agent Platform manages a hosted conversational/RAG Agent and exposes an
agent-specific endpoint. Its function routing, child-agent routing, knowledge
bases, guardrails, and evaluations overlap with some BuildMax capabilities but
do not provide arbitrary repository work in an isolated execution environment.

## 8. Comparison With BuildMax

| Concern | DigitalOcean | BuildMax today | Assessment |
|---|---|---|---|
| Product ownership | Cloud resources, environments, sessions, provider tools | Space, Issue, Agent, Task, TaskRun, Workflow, Schedule | BuildMax owns the richer work and authorization model |
| Agent loop | Multiple adapters and external harnesses | One Go runtime shared by all surfaces | Replacing it would fragment behavior |
| Immutable setup | Environment config | Agent revision, model resolution, plugin pins, sandbox tiers, Secret consumption | Same intent, but BuildMax's resolved inputs are distributed |
| Continuing work | Session plus sandbox and history | Task plus session bundle and workspace head | Similar infrastructure shape, different domain authority |
| One turn | Run | TaskRun | Closest mapping; TaskRun additionally owns authorization and authoritative result |
| Isolation | One Firecracker microVM per session | Worker pod plus Bash sandbox; accepted outer-runtime limits | Harness is materially stronger for untrusted execution |
| Durable workspace | Live disk, machine checkpoint, optional Workspace volume | Immutable object-store base/result/partial checkpoints | BuildMax is more portable and explicit about lineage |
| Tools | Managed catalog, MCP, SDK, brokered execution | Built-in tools, remote/stdio MCP, plugins | Gateway offers breadth and credential isolation |
| Tool discovery | Search, preload, invoke, code, output views | Lightweight catalog plus `LoadMcpTools` and `CallMcpTool` | BuildMax already has a useful two-meta-tool foundation |
| External identity | Actor and user-owned Connection | Space Secret and experimental local app connection | This is BuildMax's clearest product gap |
| Approval | Adapter and gateway approval | Core tool policy, local approval, Remote Control, deferred worker questions | BuildMax has richer cross-surface ownership; gateway can become an enforcement point |
| Workflow | Cron/webhook triggers with fresh or reused sessions | Durable graph, bindings, retries, timeouts, human nodes, schedules | BuildMax should not replace this with provider triggers |
| Inference | Broad model catalog, modalities, routing, batch, dedicated serving | Provider-neutral clients, managed gateway, quota and call ledger | Consume as a provider; do not rebuild GPU serving |
| Observability | Adapter events and delayed Insights | Bounded redacted trace, TaskRun result, Artifact and model-call ledger | External events should feed, not replace, BuildMax records |
| Deployment | DigitalOcean-managed public cloud | Local binary and private deployment | Provider integration must remain optional |

The most accurate correspondence is:

```text
BuildMax resolved Agent and run policy  ~= DigitalOcean Environment
BuildMax Task's live execution instance ~= DigitalOcean Session
BuildMax TaskRun                         ~= DigitalOcean Run
```

The approximation ends at ownership. A DigitalOcean Session is infrastructure;
a BuildMax Task is a Space-owned user work thread. A DigitalOcean Run is an
agent turn; a TaskRun is also the immutable admission, authorization, metering,
result, trace, Artifact, and Workflow-progression boundary.

## 9. Lessons For BuildMax

### 9.1 Preserve the control plane and make infrastructure replaceable

The durable value of BuildMax is not creation of a Kubernetes Job. It is the
meaning and governance around the work. Task and TaskRun must remain stable if a
deployment uses local processes, Kubernetes, or a future managed executor.

The existing `scheduler.WorkerRunner` is a useful seam but is launch-oriented
and returns Kubernetes-shaped metadata. It should be generalized only after a
real external-executor experiment identifies the minimum lifecycle contract.

### 9.2 Resolve one immutable run environment

DigitalOcean Environment demonstrates the operational value of treating an
execution's adapter, tools, policy, credentials, egress, and resources as one
immutable input. BuildMax already pins the important parts separately.

A future internal `ResolvedRunEnvironment` should normalize the selected Agent
revision, model, plugin releases, MCP/tool selection, sandbox policy, Secret
grants, workspace base, resource limits, and execution backend. It need not be
a new user-facing entity or database table. A canonical digest plus the fields
needed for execution and diagnosis may be sufficient.

The concrete requirements are replayability, one input to an external backend,
and an answer to "why did this run have this capability?" If an implementation
cannot demonstrate one of those requirements, it should not add the concept.

### 9.3 Model Connection separately from Secret

Space Secrets correctly describe values delivered to a run and explicitly
state that the Agent can read them. A user-authorized GitHub, Jira, or Slack
account has a different lifecycle: provider consent, scopes, refresh,
revocation, account selection, and a principal on whose behalf a call occurs.

The Agent app-delegation proposal already distinguishes a Connection from a
runtime grant. Action Gateway strengthens that direction with concrete actor,
connection, session, and policy boundaries. BuildMax should not stretch Secret
into an application identity resource.

### 9.4 Separate four authorization layers

Effective external authority is an intersection:

```text
provider credential scopes
  AND selected operations
  AND BuildMax allow / ask / deny policy
  AND sandbox network and filesystem reach
```

These layers should remain distinct in storage and execution, then be presented
together in TaskRun diagnostics. A plugin effect annotation or MCP read-only
hint is evidence for policy evaluation, not authority by itself.

### 9.5 Negotiate backend capabilities

An executor or adapter should declare support for at least the capabilities an
admitted run may require: event streaming, cancellation, workspace restoration,
checkpointing, forking, interactive approval, browser, private networking, and
credential brokering. Admission must fail when a required capability is absent.
It must not silently restart from scratch, auto-approve, or label an incomplete
trace as complete.

### 9.6 Keep portable checkpoints authoritative

Whole-machine snapshots can make continuation fast, but they are provider- and
adapter-specific. BuildMax's object-store checkpoint remains the portable base,
result, retry, and recovery record. An external session or machine checkpoint
may be a cache. It cannot be the only copy and cannot advance a Task's workspace
head independently of a successful BuildMax checkpoint finalization.

### 9.7 Do not add TaskRun pause merely because a backend supports it

Pausing a microVM is an infrastructure action. Pausing an authoritative TaskRun
would need answers for deadlines, quota, credential expiry, authority
revocation, Workflow capacity, stale-run reaping, and provider-state loss.
BuildMax already satisfies much of the user outcome through terminal TaskRuns,
portable checkpoints, and Continue. A user journey must demonstrate the need
before a new TaskRun state is added.

### 9.8 Scale tool context only when evidence requires it

BuildMax already exposes two MCP gateway tools and loads an individual schema
on demand. This avoids registering every full schema with the model. If real
catalogs make the remaining name-and-description catalog too large, the next
step is searchable discovery, selective preload, and schema-bound output
projection. It is not a speculative rewrite before that pressure exists.

### 9.9 Treat external tool retries as uncertain effects

Connector execution should distinguish a logical invocation from its provider
attempts, reuse an idempotency key only where the provider contract supports
it, and represent timeout-after-write as an unknown outcome rather than a clean
failure. Verification guidance or a compensating action belongs beside that
outcome. A multi-tool Agent action is not a transaction.

### 9.10 Consume inference infrastructure instead of reproducing it

DigitalOcean Inference can enter the existing model catalog as a provider or
router endpoint. BuildMax should retain Space policy, quota, credential
ownership, call ledger, structured-output validation, and TaskRun attribution.
It should not build GPU scheduling, RDMA, vLLM autoscaling, or an equivalent
commercial-model marketplace without a concrete private-deployment need.

## 10. Integration Options

| Option | User value | BuildMax work | Main risk | Candidate order |
|---|---|---|---|---:|
| DigitalOcean Inference provider | Additional models, modalities, and routing under existing managed inference | Provider configuration and capability qualification | Partial OpenAI compatibility and provider-specific feature gaps | 1 |
| Action Gateway remote MCP | Large connector catalog and credentials kept out of workers | Session provisioning, actor mapping, approvals, audit linkage, revocation | DigitalOcean identity ownership and public-preview dependency | 2 |
| Harness Runtime executor | Stronger isolation and managed session infrastructure | External lifecycle, event ingestion, result/checkpoint bridge, cleanup, cost control | Largest semantic and operational mismatch | 3 |
| Adopt DigitalOcean Agent Platform as a native Agent type | Hosted RAG/chat agents | Second Agent definition and session model | Duplicated concepts and fragmented runtime behavior | Do not pursue without a named use case |
| Replace Kubernetes workers with Harness Runtime | Less worker infrastructure | Migration of the supported execution boundary | Vendor lock-in and loss of private deployment | Reject as a product-wide direction |

Action Gateway and Harness Runtime must remain independently selectable. A
deployment may use BuildMax workers with Action Gateway, Harness Runtime with
BuildMax's own remote tools, DigitalOcean Inference alone, or none of them.

## 11. Candidate Direction

The candidate architecture keeps BuildMax's control plane authoritative:

```text
BuildMax control plane
  Space / Issue / Agent / Task / TaskRun / Workflow
  authorization / quota / audit / result / Artifact / checkpoint
                         |
                 resolved run environment
                         |
              +----------+-----------+
              |          |           |
              v          v           v
          local       Kubernetes   optional managed executor
                                      |
                           optional Action Gateway

BuildMax LLM gateway
              +----------+-----------+
              |                      |
              v                      v
       existing providers    DigitalOcean Inference
```

No DigitalOcean identifier becomes a BuildMax ownership parent. External IDs
are execution handles and provenance. The BuildMax reconciler determines the
TaskRun terminal state from durable facts and provider observations. Provider
events are inputs to the trace, not the trace's source of truth.

The current roadmap should not change for this proposal. If experiments
produce positive evidence after the Beta gate, accepted work belongs under R5
or its successor rather than being inserted into R2 or R3.

## 12. Risks And Product Limits

### 12.1 Public-preview durability and API change

DigitalOcean warns that public-preview data, records, and execution state may
be lost. BuildMax must retain its own authoritative data and cleanup records.
Environment fields, adapter capabilities, limits, and pricing may change before
general availability.

### 12.2 Documentation inconsistencies

At review time, the egress documentation describes VPC attachment fields while
the Harness Runtime limits page says private VPC access and peering are not
supported. The scoped-secret declaration is documented while redemption is not
enabled everywhere. These are reasons to qualify behavior, not to guess which
page will become authoritative.

### 12.3 Capacity and financial control

Paused sessions still count toward concurrency. Default account limits are not
capacity guarantees, and the services do not provide a per-session or
per-product spend limit. A looping Agent can consume compute, tool, and model
balances. BuildMax quota, admission control, deadlines, orphan reaping, and
external-resource cleanup remain necessary.

### 12.4 Missing terminal callbacks

Harness Runtime does not currently support webhook notifications for run
lifecycle events. An executor integration needs polling or event-stream
observation plus durable reconciliation, and must tolerate lost observations
and duplicated requests.

### 12.5 Credential and identity mismatch

Action Gateway sessions and connections are DigitalOcean-user-owned within a
team. BuildMax needs an explicit answer for personal connections, Space-owned
service connections, offboarding, and unattended runs. Reusing one operator's
connection for every Space would violate BuildMax's ownership model.

### 12.6 Data location and private-deployment expectations

Managed Agents processing and Insights telemetry have documented regional
constraints, including United States processing for current services. A
private deployment may be prohibited from sending source, prompts, tool data,
or telemetry to that boundary. The adapter must be an opt-in deployment choice
with an explicit data-flow description.

### 12.7 Capability fragmentation

An adapter can provide an isolated sandbox while omitting policy enforcement,
events, approvals, or checkpoints. Product surfaces must show the actual
boundary and evidence, not the best capability another adapter could provide.

## 13. Evidence-Producing Experiments

### 13.1 Experiment A: Inference provider qualification

**Outcome:** one existing BuildMax managed-model path uses DigitalOcean
Inference without changing TaskRun semantics.

Verify:

- streaming Chat or Responses requests;
- tool calls and structured output for one supported model;
- usage and error mapping into the BuildMax call ledger;
- timeout, rate-limit, overload, and authentication failures;
- capability refusal for unsupported provider-specific features; and
- quota remains enforced by BuildMax.

Do not include Dedicated Inference provisioning, Batch, or router management in
the first experiment. A pre-created router may be treated as a model identifier.

### 13.2 Experiment B: Action Gateway application delegation

**Outcome:** a person connects GitHub, an Agent reads one resource without
receiving the credential, and a write requires a BuildMax-visible approval.

Verify:

- stable BuildMax principal to Action Gateway actor mapping;
- separate personal and unattended-service connection ownership;
- explicit selected tools and deny-by-default behavior;
- allow for a read, ask for a test write, and deny for a destructive action;
- an approval is bound to exact tool arguments and cannot override deny;
- credential values do not appear in worker environment, prompts, traces,
  outputs, or stored session state;
- revocation stops subsequent calls;
- provider timeout after a write produces an uncertain outcome; and
- Gateway invocation identity and cost are correlated with one TaskRun trace.

This experiment should inform the existing Agent app-delegation proposal rather
than create a second connector product model.

### 13.3 Experiment C: Harness Runtime executor

**Outcome:** one TaskRun executes through a custom BuildMax worker image and
still completes through the normal BuildMax result, Artifact, trace, and
checkpoint paths.

Verify:

- an admitted TaskRun creates exactly one external session/run despite retries;
- the BuildMax run token reaches only the intended execution;
- output and useful events stream to the existing Task stream;
- cancellation yields one explainable terminal state;
- a successful portable checkpoint advances the Task workspace head;
- failure preserves an available partial checkpoint without advancing the head;
- external session loss and DigitalOcean API outage are reconciled;
- an orphaned session is found and removed;
- Agent revision or policy change produces a new resolved environment rather
  than mutating an existing session invisibly;
- unsupported capabilities fail admission; and
- compute, storage, tool, inference, and cleanup cost evidence is recorded.

Only after this experiment should `WorkerRunner` be redesigned. The experiment
should first use an adapter-shaped implementation beside the current interface
so it can reveal the contract without making that provisional contract public.

## 14. Open Questions

- Is the first Action Gateway connection personal, Space-owned, or a service
  identity, and how does offboarding affect in-flight and future TaskRuns?
- Can an Action Gateway approval be projected through BuildMax Remote Control
  without giving the approving client a DigitalOcean credential?
- Does a custom Harness Runtime adapter expose enough structured events to
  preserve the current trace and diagnostic claims?
- Is a Harness session scoped to a Task, to one TaskRun, or used only as an
  ephemeral execution detail? What measured startup or continuity benefit
  justifies reuse?
- How is an external session fenced when two reconcilers race after a lost
  lease or restart?
- Which parts of a resolved run environment must be persisted as columns, which
  belong in a canonical manifest, and which are already derivable from pinned
  records?
- Is the remaining MCP name-and-description catalog large enough in real use to
  justify semantic tool search?
- Which private-deployment customers may legally or operationally send source
  code and tool results to a public managed service?
- What availability, retention, residency, and support commitments would be
  required before a managed executor can be described as supported rather than
  experimental?

## 15. Likely Destination If Accepted

The three integration questions may resolve independently:

- An accepted DigitalOcean Inference adapter belongs in the LLM provider
  architecture, configuration reference, managed-model documentation, and
  provider qualification suite.
- An accepted Action Gateway path should update the Agent app-delegation
  proposal or replace it with a design record for Connection, actor, per-run
  grant, approval, revocation, and tool-call audit.
- An accepted managed executor should produce a provider-neutral execution
  backend design, update Server architecture and TaskRun provenance, and add a
  backlog item only after the prototype defines acceptance evidence.

If the experiments do not demonstrate a better user or operator outcome than
the existing provider, MCP, and Kubernetes paths, retire this paper and retain
the architectural lessons without adding a DigitalOcean-specific product
surface.
