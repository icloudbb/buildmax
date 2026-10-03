# BuildMax Product Vision

> **简体中文：** [阅读中文镜像](../zh-CN/design/产品愿景.md)

> **Audience:** contributors, product designers, and operators · **Status:** current decision

Related: [roadmap](../ROADMAP.md),
[surface positioning](surface-positioning.md),
[data model](../contribute/architecture/data-model.md), and
[Portal execution model](portal-execution-model.md).

## Contents

- [Product Promise](#product-promise)
- [Product Model](#product-model)
- [Runtime Principles](#runtime-principles)
- [Current Concepts](#current-concepts)
- [Direction For New Product Bets](#direction-for-new-product-bets)
- [Decision Test](#decision-test)

## Product Promise

BuildMax is an out-of-the-box, privately deployable enterprise Agent platform
powered by one shared Go Agent Core.

The same runtime serves three operating profiles:

- direct local execution through CLI/TUI and Desktop;
- space collaboration and governance through Server and Portal; and
- durable background execution through worker TaskRuns.

A Server is optional for local use. Connecting a local client adds identity,
managed models, Space work, and publication to the private deployment; it does
not turn local execution into a remote-only product.

## Product Model

### One Agent Core, Several Surfaces

Important Agent capability belongs in the shared runtime first. CLI, Desktop,
Portal conversations, and workers may present and authorize that capability
differently, but they must not grow separate tool-calling loops or incompatible
execution semantics.

[Surface positioning](surface-positioning.md) owns the detailed division:

- CLI/TUI is the fastest local Agent surface;
- Desktop is the local personal Agent workbench;
- Portal is the enterprise operation layer; and
- workers execute durable background runs without speaking directly to users.

### Space Owns Shared Resources

Space is the ownership and authorization boundary for Portal resources. Roles,
quota, workflows, issues, conversations, tasks, artifacts, plugins, audit, and
usage are interpreted within that boundary. Deployment-wide administration is
a separate grant and does not imply access to another Space's content.

Issue is the primary user-facing work object. It states the work, relates its
discussion and execution, and makes results easy to find without requiring a
user to understand scheduler internals.

### Foreground Interaction And Durable Execution Stay Distinct

A Conversation is an independent foreground chat and optional orchestrator. It
may answer directly or create an Agent-backed Task when work needs durable
execution. It is not the mandatory parent of that Task.

Task plus TaskRun is the durable execution plane. An Agent may be invoked
directly through it, and a Task retains the Agent session across later
TaskRuns. A Conversation, Issue, Workflow, API request, or webhook may be the
origin, but Space remains the owner and no origin becomes an execution or
authorization parent. The full boundary and continuation model are in
[agent-execution-and-task-threads.md](agent-execution-and-task-threads.md).

### Outcomes Are First-Class

Users ask for outcomes, not internal task graphs. BuildMax therefore treats a
result summary, Artifact, and provenance as product objects rather than log
fragments. Task, run, step, trace, and model-call pages are explanations and
drill-down surfaces behind that outcome.

Artifacts are explicit durable publications. A worker output directory or a
local file does not become an Artifact merely because it exists; an authorized
producer publishes the file intentionally.

## Runtime Principles

### Local And Managed Modes Are Honest

A local client either calls a locally configured provider or, after login,
calls models supplied by its BuildMax deployment. The two modes do not merge
catalogs or credentials. Direct local mode remains useful without a Server;
managed mode keeps provider credentials server-side.

### Trust Boundaries Are Visible

Tool permissions, hooks, sandboxing, traces, worker credentials, and plugin
pins must describe the boundary that actually applied. A recorded downgrade is
better than an implied sandbox that did not run, but security-sensitive policy
may still choose to fail closed. BuildMax must not claim containment, egress
restriction, approval, or recovery behavior it does not enforce.

Every run produces bounded, redacted trace evidence by default. Trace failure
is fail-open for execution, but the surface should state when evidence is
missing.

### Durability Is Scoped, Not Magical

Each durable object names its own persistence and recovery contract:

- local Sessions use atomic bundles and linked history;
- TaskRuns and their result delivery are Server records;
- Artifacts have stable identities and explicit retention behavior; and
- audit and model-call records explain shared operations.

BuildMax does **not** have a generic versioned-workspace service, hidden Git
state engine, activity timeline restore, or promise that every file change is
reversible. Session rewind changes conversation history only; it does not undo
tools or restore workspace files.

[Task workspace checkpoints](task-workspace-checkpoints.md) are the accepted,
narrow exception in the execution plane: a planned TaskRun may restore one
immutable `workspace/` checkpoint and publish the next so Task continuation does not
pair remembered Agent history with missing files. That is execution recovery,
not a user-visible workspace history. Change sets, arbitrary rollback,
cross-device reconstruction, and applying Task state to shared Space files still require
a separately accepted design, ownership model, and roadmap priority.

### Configuration And Execution Remain Portable

The Go core and CLI/TUI remain usable as a single binary without Node. Portal
and Desktop may use their React frontends, but the runtime must stay suitable
for local and private deployment across supported platforms and model
providers.

## Current Concepts

| Concept | Product role | Authority |
|---|---|---|
| Local workspace | Directory the local Agent reads and changes | Local user and operating system |
| Local Session | Resumable interaction with one local Agent | Local session bundle |
| Desktop Project | Local UI state around a workspace | Desktop only; not a Server entity |
| Space | Ownership and authorization boundary | Server |
| Issue | Primary shared work object | Space |
| Workflow | Reusable durable execution plan; currently linear, targeting a revision-pinned adaptive graph | Space |
| Conversation | Independent foreground chat and optional orchestrator | Space |
| Task / TaskRun | Durable Agent thread and its execution turns or attempts | Space and scheduler |
| Artifact | Explicit durable output with stable identity | Space |
| Plugin environment | Space/Agent activations plus Task-scoped autonomous additions; expanded run directories are projections | Immutable package pins and environment revision recorded on TaskRun |

The table is a product map, not a database schema. Current fields and
relationships live in the [data model](../contribute/architecture/data-model.md).

## Direction For New Product Bets

Open proposals are options, not extensions already promised by this vision.
The current candidates are listed in the [proposals index](../proposals/README.md).

Acceptance means updating the roadmap, moving durable rationale into a design
record, and deleting the proposal. Until then, user and operator documentation
must describe only what ships.

## Decision Test

A proposed capability fits BuildMax when it strengthens at least one operating
profile without weakening the shared runtime or the Space boundary, and when its
authority, durability, failure behavior, and evidence can be explained plainly.

Prefer changes that make Agent outcomes easier to obtain, trust, and reuse.
Reject shortcuts that create a Portal-only Agent, make a local client a second
administration surface, make Conversation a mandatory execution parent, let a
background run impersonate a user's message, or present an unimplemented
recovery or security boundary as product behavior.
