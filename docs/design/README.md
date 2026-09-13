# Design Records

> **简体中文：** [阅读中文镜像](../zh-CN/design/设计文档索引.md)

> **Audience:** contributors · **Status:** current

Why BuildMax is built the way it is. These are **rationale, not user
documentation** — when a design ships something configurable, the user-facing
half lives in the [manual](../../manual) or [../reference/](../reference/), and the
design record keeps the trade-offs and open gaps.

Documents use stable, semantic filenames. Browse by the problem you are working
on; each record has one primary domain for discovery and a lifecycle that says
how to read it. A primary domain is a navigation choice, not an exclusive
ownership boundary. Cross-domain relationships belong in the record itself.

## Browse By Domain

| Domain | Start here | Covers |
|---|---|---|
| [Product and Execution Model](#product-and-execution-model) | [Product vision](product-vision.md) | Product boundaries, surfaces, Agent execution, and continuity decisions |
| [Agent Runtime and Models](#agent-runtime-and-models) | [Agent execution and Task threads](agent-execution-and-task-threads.md) | Context, models, tools, hooks, traces, and shared runtime behavior |
| [Local Experience](#local-experience) | [Local Projects and Project Memory](local-project-memory.md) | CLI, TUI, Desktop, sessions, workspaces, and local models |
| [Space Platform](#space-platform) | [Space governance](space-governance.md) | Space-owned work, collaboration, workflows, plugins, and artifacts |
| [Trust and Security](#trust-and-security) | [Agent Core trust harness](trust-harness.md) | Sandboxes, credentials, permissions, and worker boundaries |
| [Operations and Deployment](#operations-and-deployment) | [Enterprise deployment](enterprise-deployment.md) | Server coordination, private deployment, and process lifecycle |
| [Verification](#verification) | [Verification program](verification-program.md) | Evaluation, end-to-end evidence, and release confidence |

The lifecycle column has three values:

- **Direction** — a durable decision spanning more than one roadmap phase.
- **Active plan** — planned or partly implemented work tracked by
  [ROADMAP.md](../ROADMAP.md). The record states what has shipped and what
  remains.
- **Specification** — durable rationale for an implemented or partly
  implemented subsystem, kept aligned with the code.

Roadmap priority and detailed implementation status remain in `ROADMAP.md` and
the individual record rather than being duplicated here.

## Product and Execution Model

| Document | Lifecycle | Covers |
|---|---|---|
| [Product vision](product-vision.md) | Direction | Long-range product model, ownership boundaries, and rules for future bets |
| [Surface positioning](surface-positioning.md) | Direction | How Agent Core, CLI, Desktop, and Portal relate |
| [Agent execution and Task threads](agent-execution-and-task-threads.md) | Direction | Task and TaskRun as the durable Agent execution plane, independent of Conversation |
| [Orchestration and continuity decisions](orchestration-and-continuity-decisions.md) | Direction | Decisions connecting Task continuity, Space ownership, structured output, and orchestration |
| [Portal execution model](portal-execution-model.md) | Specification | Outcome-projection rationale; execution ownership is superseded by Agent execution and Task threads |
| [Scheduled Agent execution](scheduled-agent-execution.md) | Specification | Recurring Agent runs on the Task plane: the Schedule entity, exactly-once firing across replicas, and runaway control |

## Agent Runtime and Models

| Document | Lifecycle | Covers |
|---|---|---|
| [Context durability](context-durability.md) | Active plan | Compaction, durable notes, checkpoints, and additional prompts |
| [Managed LLM gateway](llm-gateway.md) | Active plan | Managed inference, model resolution, usage, and quota boundaries |
| [Prompt cache control](prompt-cache-control.md) | Active plan | Provider-native prompt caching and its telemetry |
| [Parallel tool execution](parallel-tool-execution.md) | Active plan | Safe concurrency for read-only tools and subagents |
| [Structured output](structured-output.md) | Active plan | Provider-neutral schema-constrained model results |
| [ACP interoperability boundary](acp-interoperability.md) | Direction | How BuildMax may expose its native Agent Core to ACP clients without making ACP an internal or external-executor contract |
| [Client modes: local and managed](client-modes.md) | Specification | Login-derived mode selection, model inventory, and usage attribution |
| [LLM provider adapters](llm-provider-adapters.md) | Specification | Canonical messages and provider protocol differences |
| [Hook system](hook-system.md) | Specification | Runtime events, transports, failure behavior, and trust boundaries |
| [Queued messages](queued-messages.md) | Specification | Queueing and mid-run message injection across interactive surfaces |
| [Durable run trace](durable-run-trace.md) | Specification | Bounded, redacted JSONL evidence for every run |

## Local Experience

| Document | Lifecycle | Covers |
|---|---|---|
| [Local Projects and Project Memory](local-project-memory.md) | Specification | Shared local Project identity and bounded cross-session memory |
| [Local session storage](local-session-storage.md) | Active plan | Atomic session bundles, linked history, rewind, and fork |
| [Session usage stats](session-usage-stats.md) | Specification | Per-session and cross-session usage reporting |
| [Local background jobs](local-background-jobs.md) | Active plan | Process-scoped command, subagent, and monitor jobs |
| [Local Ollama provider](local-ollama-provider.md) | Active plan | Credential-free local model discovery and inference |
| [Workspace root and worktrees](workspace-root-and-worktrees.md) | Active plan | Mutable workspace roots and Agent-managed Git worktrees |

## Space Platform

| Document | Lifecycle | Covers |
|---|---|---|
| [Task workspace checkpoints](task-workspace-checkpoints.md) | Direction | Durable workspace continuity for Task and TaskRun execution |
| [Portal navigation and Space context](portal-navigation-and-space-context.md) | Specification | Canonical Space routes, scoped navigation, switching, and orientation |
| [Portal work and execution experience](portal-work-and-execution-experience.md) | Specification | Issue-centered work, explicit execution, and trustworthy provenance |
| [Portal state and permission feedback](portal-state-and-permission-feedback.md) | Active plan | Loading, empty, error, stale, and authorization presentation |
| [Portal data and plugin surfaces](portal-data-and-plugin-surfaces.md) | Specification | Files, Artifacts, Marketplace, and scoped plugin actions |
| [Portal responsive and accessible interaction](portal-responsive-and-accessible-interaction.md) | Specification | Narrow layouts, keyboard behavior, dialogs, and viewport evidence |
| [Issue agent access](issue-agent-access.md) | Active plan | Scoped Issue context and reporting for local and worker runs |
| [Space governance](space-governance.md) | Active plan | Roles, quota, workflow lifecycle, audit, and retention |
| [System administration](system-administration.md) | Active plan | Deployment-wide authority and operator surfaces |
| [Plugin distribution and private marketplace](plugin-marketplace.md) | Active plan | Publishing, installing, and managing plugins |
| [Space and worker plugin distribution](plugin-space-distribution.md) | Active plan | Space activation, Agent selection, and worker delivery |
| [Entity identity and relational keys](entity-identity.md) | Active plan | Public identifiers, relational keys, and store boundaries |
| [Workflow runtime](workflow-runtime.md) | Active plan | Durable adaptive graphs over Task and TaskRun |
| [Unified artifacts](unified-artifacts.md) | Active plan | Space-owned artifact storage and Agent upload |
| [Artifact public sharing and preview](artifact-public-sharing-and-preview.md) | Active plan | Revocable public links and safe rich previews |
| [Space membership lifecycle](space-membership-lifecycle.md) | Specification | Invitation, role change, ownership transfer, and recovery |
| [Timestamp representation](timestamp-representation.md) | Specification | Canonical persisted and API timestamp representation |

## Trust and Security

| Document | Lifecycle | Covers |
|---|---|---|
| [Agent Core trust harness](trust-harness.md) | Active plan | Containment, observability, and evidence across Agent execution surfaces |
| [Worker API network boundary](worker-api-network-boundary.md) | Specification | Separation and authorization of public and worker traffic |
| [gVisor worker runtime](gvisor-worker-runtime.md) | Direction | Conditional worker Pod isolation behind an optional RuntimeClass |
| [Agent-scoped sandbox policy](agent-sandbox-policy.md) | Active plan | Agent revisions, Space defaults, and claim-time sandbox selection |
| [Space Secrets and run delivery](space-secrets.md) | Active plan | Space-owned credentials and run-scoped materialization |
| [Tool permissions](tool-permissions.md) | Active plan | Runtime tool allow, deny, and approval policy |
| [Sandbox boundaries](sandbox-boundaries.md) | Specification | Local and worker command containment boundaries |
| [Worker run token](worker-run-token.md) | Specification | The run-scoped credential accepted by worker routes |

## Operations and Deployment

| Document | Lifecycle | Covers |
|---|---|---|
| [Server coordination](server-coordination.md) | Active plan | Shipped Redis fan-out, turn leases, and write fencing; candidate qualification remains |
| [Enterprise deployment](enterprise-deployment.md) | Active plan | Supported private deployment shape and operating gaps |
| [Graceful shutdown](graceful-shutdown.md) | Active plan | Draining, quiescing, worker interruption, and bounded shutdown |

## Verification

| Document | Lifecycle | Covers |
|---|---|---|
| [Evaluation and qualification](evaluation-system.md) | Active plan | Black-box adapters, graders, suites, and external benchmarks |
| [Local end-to-end verification](end-to-end-testing.md) | Active plan | Deterministic local, Desktop, Portal, and deployment journeys |
| [Verification program](verification-program.md) | Active plan | Risk-based evidence from pull request checks through release rehearsal |

## Where The Designs Land

| Area | Package |
|---|---|
| Shared agent runtime assembly | `internal/agentapp` |
| Task-run execution runtime | `internal/agentapp/taskrun` |
| Tier 1 conversation orchestration | `internal/service/conversation` |
| Issue, task, workflow, and quota services | `internal/service/*` |
| HTTP API handlers | `internal/server/handlers` |
| Scheduler | `internal/server/scheduler` |
| Worker API client and updater | `internal/infra/workerclient` |
| Runtime agent tools | `internal/tool` |

Full tree: [contribute/repo-layout.md](../contribute/repo-layout.md).

## Adding One

1. Choose a short semantic filename such as `execution-policy.md`.
2. State the problem, options considered, chosen approach, status, and phases.
3. Choose the primary domain where a contributor would look for it, and add it
   to exactly one domain table above.
4. Mark its lifecycle as Direction, Active plan, or Specification.
5. If it ships something a user configures, write the user-facing half in the
   `manual/` or `reference/` and link it from the record.

When it stops describing the current direction, **delete it** and remove its
row. Git history keeps it; see
[contribute/documentation.md](../contribute/documentation.md).
