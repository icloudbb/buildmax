# Agent Core Trust Harness

> **简体中文：** [阅读中文镜像](../zh-CN/design/信任保障.md)

## Contents

- [Status](#status)
- [1. Purpose](#1-purpose)
- [2. Direction](#2-direction)
- [3. Key Capabilities To Support](#3-key-capabilities-to-support)
- [4. Explicitly Out Of Scope For Now](#4-explicitly-out-of-scope-for-now)
- [5. Suggested Priority](#5-suggested-priority)
- [6. Acceptance](#6-acceptance)

## Status

- roadmap_priority: `R0`
- status: `done` — hooks, durable traces, the Bash sandbox, process limits,
  Agent/Space sandbox tiers, worker baseline selection, and worker API ingress
  isolation are implemented and evidenced (§6.1). Portal presents the boundary
  recorded by a TaskRun trace and its resolved plugin pins. The unattended-worker
  profile refuses stdio MCP fail-closed before any child or model call, and
  Portal Run Details presents that treatment beside the boundary. The supported
  worker contract is closed; §6.1 maps each control to its evidence. Pod-wide
  egress and an outer runtime remain accepted, conditional post-Beta hardening,
  and full immutable-candidate qualification is the separate R3/Beta gate
- follows: P0 Agent Core stability, P1 Local agent experience, and P2 Portal outcome surface — all complete; their plans were retired (see git history)
- roadmap: [../ROADMAP.md](../ROADMAP.md)
- created_at: `2026-05-23`

## 1. Purpose

The completed P0 work made the shared Agent Core stable enough for CLI, Desktop,
Portal, and worker task runs. R0 then closed the stdio MCP path around the worker
boundary and evidenced the supported contract's controls (§6.1).

This document intentionally stays at the product-capability level. It records
the trust capabilities delivered and the evidence that closes R0.

## 2. Direction

R0 centered on the supported unattended-worker profile. CLI, Desktop, Portal,
and worker expose shared core capabilities in surface-appropriate ways.

The goal is:

> Users and operators can understand, control, and debug Agent runs.

## 3. Key Capabilities To Support

### 3.1 Runtime Hooks — shipped ✅

The runtime hook system is implemented; detail design lives in
[hook-system.md](./hook-system.md). Highlights:

**Configuration locations** — entries from both layers merge additively:
- Global: `<BUILDMAX_HOME>/settings.yaml` under `hooks:`
- Workspace: `<workspace>/.buildmax/hooks.yaml`

**Transports** — every entry chooses one via `type:`:
- `command` (default) — shell command, JSON on stdin
- `http` — POST JSON to a URL
- `mcp_tool` — invoke a tool on a connected MCP server
- `prompt` — single-turn LLM judge

**Events shipped (13)**:

| Event | Gating? | Anchor |
|---|---|---|
| `SessionStart` / `SessionEnd` | no | `agentapp.OpenSession` / `CloseSession` |
| `UserPromptSubmit` | **yes** | `agentapp.RunPrompt` (before history append) |
| `PreToolUse` | **yes** | `applyPolicyAndExecute` (after policy, before exec) |
| `PostToolUse` / `PostToolUseFailure` | no | tool success / error paths |
| `Notification` | no | around the approval flow (`approval_required`, `permission_denied`) |
| `PreCompact` | **yes** | before context compaction |
| `PostCompact` | no | after a successful compaction |
| `SubagentStart` / `SubagentStop` | no | subagent runner |
| `Stop` | no | main-agent successful exit |
| `StopFailure` | no | any error exit (main or subagent) |

Use cases unlocked: formatting / linting (`post_tool_use`), policy checks
(`pre_tool_use`), external approvals (`notification` + `pre_tool_use`),
audit export (`stop` / `subagent_stop` / `post_tool_use_failure`).

**Subagent inheritance**: subagents share the parent HookManager; every
event payload from a subagent run is stamped with `is_subagent` and
`agent_type` so audit hooks can attribute. Subagents cannot bypass parent
hooks.

**Deferred** to a follow-up when a run needs it: skill / subagent frontmatter
hooks (session-scoped lifetime). See [hook-system.md](./hook-system.md) §10
(implementation phase F). The `agent` transport, the `async` command flag, and a
`buildmax hooks` inspector are out of R0 scope (§4): the 13 events over four
transports already cover the demonstrated hook use cases, and hook configuration
is small and file-authored, so a dedicated inspector command earns nothing yet.

### 3.2 Sandbox And Execution Boundaries — shipped ✅

Explicit sandbox modes for command execution now exist. Detail design lives in
[sandbox-boundaries.md](./sandbox-boundaries.md);
its phases A–E are implemented. The sandbox isolates **bash subprocesses**
(Seatbelt on macOS, `bwrap` on Linux/WSL2, unavailable elsewhere); non-bash
tools keep their existing permission boundary. Config resolves from
`settings.yaml` + `policy.yaml` + `BUILDMAX_SANDBOX_ENABLED` with per-surface
defaults, and `buildmax sandbox status|deps|mode|enable|disable` plus the TUI
footer make the active mode visible.

Boundary coverage against the list above:

- workspace filesystem access — ✅ OS backend bind/profile rules
- external directory access — ✅ `filesystem.allow_write` / `deny_read` etc.
- network access — ✅ Go-side HTTP/SOCKS proxy with domain allow/deny
- environment variable exposure — ✅ secret-shaped vars scrubbed from bash env
- process execution limits — ✅ `sandbox.process.{max_cpu_seconds,
  max_memory_mb,max_processes,max_open_files}` become `ulimit` statements
  prefixed onto the wrapped command, one per limit; verified against real
  Alpine and macOS shells, including a CPU-time limit actually killing a
  busy loop on Linux (`max_memory_mb` is a documented no-op on macOS —
  Darwin's `setrlimit` has no `RLIMIT_AS`). See
  [sandbox-boundaries.md](./sandbox-boundaries.md) §13 phase D.
- worker/container execution mode — ✅ **wired and verified against the
  production pod security context**: `agentapp/taskrun` sets
  `SandboxSurface: config.WorkerSandboxSurface()`, which selects
  `SandboxSurfaceWorker` only when `BUILDMAX_SANDBOX_BACKEND_INSTALLED` is
  set (an `ENV` line in both worker Dockerfiles) — selecting the strict
  baseline unconditionally was tried first and broke every worker task on a
  bare Linux host or native Windows outright (`fail_if_unavailable: true`
  with no backend to satisfy it), caught by CI rather than by local
  development on a Mac, where Seatbelt always exists. `internal/infra/k8s/job.go`'s
  `RuntimeDefault` seccomp profile — which drops `bwrap`'s required syscalls
  once the worker pod's capabilities are empty — is replaced by a `Localhost`
  profile built for this
  ([deployment/seccomp/README.md](../../deployment/seccomp/README.md)
  has the full root-cause chain, including a second, independent kernel
  restriction on mounting `/proc` under `--unshare-pid` inside a container).
  Verified against a real pod carrying the worker's exact security context
  and the profile as the reference `DaemonSet` actually distributes it, and
  by an organic end-to-end run the deployment smoke now performs
  automatically: it arms its mock model to make a real dispatched task call
  `Bash` through the actual server → worker → Job path and asserts on the
  tool result, not the task's scripted final text; see
  [sandbox-boundaries.md](./sandbox-boundaries.md) §13 phase F.

`command` and `http` hook transports now consult `SandboxView` too — a hook
mirrors the same `WrapBashCommand`/`HostAllowed` calls `Bash`/`WebFetch`
make, with no `dangerously_disable_sandbox`-equivalent escape hatch, since
hooks are config-authored automation rather than an LLM-chosen call an
operator is watching turn by turn. Verified against a real `sandbox.Manager`
(Seatbelt), not only a test double. The enforcement engine, the worker surface,
process limits, downgrade marking, and the hook boundary have all landed, so
§3.2 is closed. The one command left unbuilt, `buildmax sandbox overrides`
([sandbox-boundaries.md](./sandbox-boundaries.md) §8), is out of R0 scope (§4):
it only writes `allow_unsandboxed_commands`, an operator lock already set in
`policy.yaml`, so no run needs a per-session convenience toggle to close the
boundary.

### 3.3 Durable Run Trace — phase 1 shipped ✅

A durable run trace now persists the runtime event stream for every run. Detail
design lives in [durable-run-trace.md](./durable-run-trace.md).
Phase 1 shipped: a bounded, redacted JSONL trace written at the single
`agentapp.RunPrompt` chokepoint, so CLI/TUI, Desktop, eval, and worker runs all
produce traces with no per-surface code. Each run writes
`<DataDir>/sessions/<session_id>/traces/<run_id>.jsonl` (run id prefix `rt_`) with a
`run_start` record, a `sandbox_boundary` record, per-iteration
`llm_*`/`tool_*`/`context_compacted` records, and a terminal `run_end`. Disable via `BUILDMAX_TRACE_DISABLED`. Fail-open: a
trace failure never breaks or slows a run.

Traces are bounded and redacted so they are useful for debugging without
leaking secrets.

Coverage against the full §3.3 target (✅ = in phase 1):

- model calls — ✅ `llm_start` / `llm_end`
- tool calls — ✅ `tool_start` / `tool_end` / `tool_denied`
- context compaction — ✅ `context_compacted`
- errors — ✅ `run_end.error`; retries are not surfaced by the event stream yet
- token usage and timing — ✅ per-call tokens, tool duration, record timestamps
- approval decisions — partial: only `tool_denied` (reason `hook`/`user`)
- hook execution — ❌ needs dedicated hook events
- file changes — ❌ needs file-change events
- subagent parent/child relationships — ✅ each subagent trace carries its
  immediate parent's `parent_run_id`
- sandbox mode and boundary decisions — partial: a `sandbox_boundary` record is
  written for every run with the resolved enabled/mode/backend and the source
  chain, including an explicit `sandboxed: false` when nothing confined the run.
  Per-command boundary decisions and violations are still ❌ (see §3.2)
- memory and instruction sources used for the run — ❌ deferred

The records marked ❌ above (hook execution, file changes, per-command boundary
decisions, and the memory/instruction sources a run loaded) are the trace work
that genuinely remains; see [durable-run-trace.md](./durable-run-trace.md) §7.
They enrich diagnostics but are not R0 acceptance gates (§6). A trace
activity-view UI, a `buildmax trace` inspector, and retention/GC of the traces
directory are out of R0 scope (§4): the JSONL trace is already bounded and
directly readable, and Portal Run Details already surfaces a run's boundary and
MCP treatment, so a second viewer earns nothing until trace volume forces GC.

### 3.4 Activity Views

Support lightweight activity views in local surfaces.

TUI and Desktop should let users inspect:

- what the Agent is doing now
- what tools were used
- which approvals happened
- what changed
- why a run failed or stopped

Normal chat should remain clean; activity should be progressive disclosure.

### 3.5 Doctor And Diagnostics

Support a local diagnostic flow for setup and runtime problems.

It should check:

- model configuration
- workspace permissions
- git availability
- active sandbox mode
- active memory and instruction sources
- MCP configuration and health
- skill and subagent discovery
- hook configuration
- BuildMax data directory health

Diagnostics should produce actionable messages and a redacted summary that can
be shared when debugging.

### 3.6 Memory And Instructions

Keep three contracts distinct: instructions are normative protocol, memory is
fallible Agent-curated recall, and session history is the ordered evidence of a
conversation. A compaction summary is a lossy history projection, not a
long-term memory merely because it helps recall.

Memory should be scoped and visible. Session notes and todos are the shipped
working-memory scope. Shared CLI/Desktop Project identity and bounded Project
Memory are implemented as specified in
[local-project-memory.md](local-project-memory.md).
Global user memory, space memory, and reusable Agent memory remain separate
future scopes rather than meanings assigned to `AGENTS.md` or agent
instructions.

The Agent should expose which instruction, memory, and history-projection
sources were loaded for a run. Users should be able to inspect, update, delete,
or disable memory. BuildMax should avoid silently persisting sensitive or
surprising information, and Memory must never override instruction sources such
as `AGENTS.md`, skills, subagent definitions, or agent instructions.

### 3.7 Subagent Traceability

Support clearer visibility for subagent execution.

Users should be able to see:

- which subagent ran
- why it was invoked
- what memory and instruction sources it received
- what tools it used
- what runtime boundaries applied
- what result it returned
- how it relates to the parent run

Subagents must not bypass parent runtime policy.

### 3.8 Safer Worker Execution

Support worker-specific trust behavior for non-interactive runs.

Worker runs should:

- fail closed when approval would be required
- run with explicit sandbox boundaries
- record enough trace data for Portal diagnostics
- load only the memory and instructions appropriate for the space/run scope
- make denied actions understandable
- avoid hiding local/remote capability drift

### 3.9 Pod-Wide Worker Egress — deferred conditional hardening

The shipped command boundary already constrains model-selected Bash and WebFetch
network access, and hook transports consult the same policy. The worker control
channel also has its own TLS listener and an ingress NetworkPolicy; see
[worker-api-network-boundary.md](./worker-api-network-boundary.md). These controls
do not impose a destination allow-list on every process in the Pod. General
worker egress and the storage identity available to the worker remain explicit
residual limits.

That wider boundary is not required for the first Beta, which supports one
trusted Space on a private network. Making it an R0 gate would introduce a CNI
or operated proxy before BuildMax has evidence for the legitimate destinations
real runs need, and an allowed Git host, package registry, model endpoint, or
upload service could still receive intentionally exfiltrated data. The Beta
candidate must record the residual limit rather than imply containment it does
not provide.

Reopen Pod-wide destination enforcement when evidence changes the supported
threat model, including any of these conditions:

- mutually untrusted tenants share worker infrastructure;
- arbitrary untrusted repositories are a supported input;
- workers hold high-value credentials whose exfiltration requires a Pod-wide
  control rather than the existing command and hook boundaries; or
- an operator requires and is prepared to operate a deployment-wide egress
  allow-list.

Before selecting an implementation, record the destinations representative runs
actually need and the deployment environments that must support enforcement.
Portable Kubernetes `NetworkPolicy`, Cilium FQDN rules, and a dedicated egress
proxy have materially different portability, hostname-authority, protocol, and
operating costs. None is an unconditional BuildMax dependency until that evidence
selects it. Per-Space policy, alternate-DNS and direct-IP bypass qualification,
and an outer runtime such as gVisor belong to the same conditional hardening
decision, not the current R0.

Stdio MCP child processes launch outside the current worker command boundary.
The supported unattended-worker profile now rejects them: `AppConfig.UnattendedWorker`
carries the profile as an explicit fact, independent of the sandbox backend
marker, and a resolved stdio server fails construction before any child process
or model call (`internal/agentapp/mcp_manager.go`). Remote transports and local
surfaces are unaffected. The run's trace records that treatment in an
`mcp_boundary` record beside `sandbox_boundary`, and Portal Run Details presents
it; a run refused before any trace exists still leaves a terminal TaskRun error
naming the policy. Disabling an unsupported transport is sufficient for the
first Beta; BuildMax does not need a general Pod-egress product to make that
narrower contract truthful.

## 4. Explicitly Out Of Scope For Now

Do not include these in the current R0 trust-boundary scope:

- Pod-wide destination policy, a dedicated egress proxy, or CNI selection
- gVisor or another outer worker runtime
- user-selectable checkpoint rollback or timeline restore
- automatic workspace write-back or merging
- full Portal audit product
- workflow engine rewrite
- plugin marketplace
- IDE extension
- container/seccomp implementation in Go
- broad versioned workspace implementation

The first two above are conditional hardening: reopen them when the supported
threat model changes (§3.9). The rest are separate product questions. The items
below are different — convenience surfaces the enforced boundary and the bounded,
readable trace already make redundant. They are cut on Occam's razor, not
deferred; add one only when a concrete operator or run task cannot be done
without it:

- `buildmax sandbox overrides`, a `buildmax hooks` inspector, and a `buildmax
  trace` inspector — the sandbox boundary is enforced from config and
  `policy.yaml`, hook configuration is small and file-authored, and the JSONL
  trace is already bounded and directly readable, so none of these second command
  surfaces earns its keep
- a trace activity-view UI and trace retention/GC — the trace is bounded at the
  source and Portal Run Details already surfaces a run's boundary and MCP
  treatment, so a viewer and a collector earn nothing until trace volume forces
  GC
- the `agent` hook transport and the `async` command flag — the 13 shipped events
  over four transports cover the demonstrated hook use cases

Task workspace checkpointing and restore-before-Continue have since shipped
under [task-workspace-checkpoints.md](task-workspace-checkpoints.md). Generic
workspace history, user-selected rollback, merging, and automatic Space-file
write-back remain separate product questions and do not block R0.

## 5. Suggested Priority

The R0 implementation order was:

1. Reject worker stdio MCP unless the child can use the declared boundary, and
   present that treatment beside the boundary already visible for a TaskRun. —
   done.
2. Exercise the command, hook, MCP, resource, and worker API boundaries against
   the supported worker profile. — done; §6.1 maps each to its evidence.

Other trust-harness improvements follow demonstrated user or operator needs and
do not block the first private Beta.

## 6. Acceptance

The R0 trust-boundary work is accepted; each criterion holds:

- no stdio MCP child runs outside the boundary claimed by the supported worker
  profile — the profile fails a resolved stdio server during assembly;
- missing required enforcement stops the run before model execution — the
  unattended-worker profile fails closed;
- an operator can see the actual boundary and MCP treatment for a TaskRun —
  Portal Run Details presents both; and
- deployment evidence covers the supported Bash, hook, MCP, process-limit, and
  worker API controls at the level §6.1 records.

Pod-wide egress and outer-runtime isolation remain truthful, accepted limits for
the first private Beta. They become release gates only if the supported threat
model changes. Full immutable-candidate qualification through the operator
journey is the separate R3 / [beta-readiness](../deploy/beta-readiness.md) gate;
R0 closing the worker contract does not assert it.

### 6.1 Evidence

Each supported worker control mapped to its strongest current evidence. Bash and
worker API isolation are proven through the deployed worker path; the rest are
proven where the control actually executes, on a worker profile those first two
prove is real.

| Control | Claim | Evidence |
|---|---|---|
| Bash confinement | A sandboxed Bash command runs and a write outside the workspace is denied on the deployed worker | Deployment smoke `assertWorkerSandboxConfines` ([tools/mk/deploy_smoke.go](../../tools/mk/deploy_smoke.go)): a real dispatched task calls Bash through the server → worker → Job path and asserts the probe ran and the out-of-workspace write was denied — not the scripted final text |
| Worker API isolation | The worker control API is unreachable from the public Service and admits only labeled workers | kind boundary check ([tools/mk/kind.go](../../tools/mk/kind.go)): `/api/worker/*` 404s on the public Service, a labeled worker reaches the worker port, an unlabeled pod is denied; plus deterministic route/token/TLS tests ([worker-api-network-boundary.md](./worker-api-network-boundary.md) §13.1) |
| stdio MCP fail-closed | A resolved stdio MCP server fails the run before model execution, legibly | Assembly-time enforcement ([internal/agentapp/mcp_manager.go](../../internal/agentapp/mcp_manager.go)); the decision is environment-independent — it fails construction before any child or model call — and is proven at the taskrun-runtime, agentapp, trace, and server-handler levels (`worker_mcp_policy_test.go`, `mcp_boundary_test.go`, and the work-handler trace test) and presented in Portal Run Details |
| Process limits | The ulimit-wrapping path bounds a sandboxed command | The worker deliberately sets no process limit — the Kubernetes Job `resources` own that ([internal/config/sandbox.go](../../internal/config/sandbox.go)); the ulimit-wrapping mechanism is verified on real Alpine and macOS shells ([sandbox-boundaries.md](./sandbox-boundaries.md) §13.1) |
| Hook boundary | A `command`/`http` hook cannot escape the sandbox | Command hooks execute the same `WrapBashCommand` path the deployed Bash probe exercises; verified against a real Seatbelt `sandbox.Manager` ([sandbox-boundaries.md](./sandbox-boundaries.md) §13.1) |

Two controls are accepted, documented first-Beta limits rather than gates:
pod-wide worker egress destination policy and an outer runtime (§3.9).
