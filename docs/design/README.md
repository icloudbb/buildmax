# Design Records

> **简体中文：** [阅读中文镜像](../zh-CN/design/设计文档索引.md)

> **Audience:** contributors · **Status:** current · **Progress reviewed:** 2026-09-26

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

The progress column is a concise implementation snapshot:

- **Complete** — the record's accepted scope has shipped. Deliberately excluded
  follow-ups do not make it partial.
- **Partial** — a usable slice has shipped, but accepted scope in the record
  remains unimplemented or unqualified.
- **Not started** — the design exists, but its implementation has not begun.
- **Decision only** — the record sets direction and has no implementation gate.
- **Conditional** — implementation is intentionally not committed until the
  record's evidence gate is met.
- **Superseded in part** — only the surviving rationale should be used; a linked
  record owns the replacement behavior.

These are intentionally not percentages: the remaining slices differ too much
in size and risk for a number to be meaningful. `ROADMAP.md` remains the source
of priority and release gates, while each record owns the detailed shipped and
remaining list. Update this snapshot when either changes.

## Product and Execution Model

| Document | Lifecycle | Progress | Covers |
|---|---|---|---|
| [Product vision](product-vision.md) | Direction | Decision only | Long-range product model, ownership boundaries, and rules for future bets |
| [Surface positioning](surface-positioning.md) | Direction | Decision only | How Agent Core, CLI, Desktop, and Portal relate |
| [Agent execution and Task threads](agent-execution-and-task-threads.md) | Direction | Partial | Task and TaskRun as the durable Agent execution plane, independent of Conversation |
| [Orchestration and continuity decisions](orchestration-and-continuity-decisions.md) | Direction | Decision only | Decisions connecting Task continuity, Space ownership, structured output, and orchestration |
| [Portal execution model](portal-execution-model.md) | Specification | Superseded in part | Outcome-projection rationale; execution ownership is superseded by Agent execution and Task threads |
| [Scheduled Agent execution](scheduled-agent-execution.md) | Specification | Complete | Recurring Agent and Workflow runs on the Task plane: the Schedule entity, exactly-once firing across replicas, and runaway control |
| [Instant-messaging channels](instant-messaging-channels.md) | Active plan | Partial | Chat platforms as a transport into Space Conversations: pairing, per-message authorization, the receive lease, and outcome reports; Telegram direct messages ship, groups and other platforms remain |

## Agent Runtime and Models

| Document | Lifecycle | Progress | Covers |
|---|---|---|---|
| [Context durability](context-durability.md) | Active plan | Complete | Compaction, durable notes, checkpoints, and additional prompts |
| [Managed LLM gateway](llm-gateway.md) | Active plan | Partial | Managed inference, model resolution, usage, and quota boundaries |
| [Prompt cache control](prompt-cache-control.md) | Active plan | Partial | Provider-native prompt caching and its telemetry |
| [Parallel tool execution](parallel-tool-execution.md) | Active plan | Complete | Safe concurrency for read-only tools and subagents |
| [Agent Bridge CLI](agent-bridge-cli.md) | Specification | Complete | One `buildmax` command surface from Agent to Server, replacing the in-process Issue tools; local user credential and worker run-token bridge |
| [Structured output](structured-output.md) | Active plan | Partial | Provider-neutral schema-constrained model results; runtime, providers, run persistence, and the Workflow graph consumer ship, while prompted fallback and typed routing remain |
| [ACP interoperability boundary](acp-interoperability.md) | Direction | Decision only | How BuildMax may expose its native Agent Core to ACP clients without making ACP an internal or external-executor contract |
| [Client modes: local and managed](client-modes.md) | Specification | Complete | Login-derived mode selection, model inventory, and usage attribution |
| [LLM provider adapters](llm-provider-adapters.md) | Specification | Complete | Canonical messages and provider protocol differences |
| [Hook system](hook-system.md) | Specification | Complete | Runtime events, transports, failure behavior, and trust boundaries |
| [Queued messages](queued-messages.md) | Specification | Complete | Queueing and mid-run message injection across interactive surfaces |
| [Durable run trace](durable-run-trace.md) | Specification | Partial | Bounded, redacted JSONL evidence for every run |
| [Remote Control](remote-control.md) | Active plan | Partial | Observing and steering a device-resident session from another device through an outbound-brokered control plane |

## Local Experience

| Document | Lifecycle | Progress | Covers |
|---|---|---|---|
| [Local Projects and Project Memory](local-project-memory.md) | Specification | Complete | Shared local Project identity and bounded cross-session memory |
| [Local session storage](local-session-storage.md) | Specification | Complete | Atomic session bundles, linked history, rewind, and fork |
| [Session usage stats](session-usage-stats.md) | Specification | Complete | Per-session and cross-session usage reporting |
| [Local background jobs](local-background-jobs.md) | Active plan | Complete | Process-scoped command, subagent, and monitor jobs |
| [Local Ollama provider](local-ollama-provider.md) | Active plan | Complete | Credential-free local model discovery and inference |
| [Workspace root and worktrees](workspace-root-and-worktrees.md) | Active plan | Complete | Mutable workspace roots and Agent-managed Git worktrees |
| [Agent browser capability](agent-browser-capability.md) | Active plan | Partial | Go-owned Chromium over CDP so an Agent can verify against a real rendered page; CLI headless, Desktop window, and the read-only Desktop tab view ship, workers stay off |

## Space Platform

| Document | Lifecycle | Progress | Covers |
|---|---|---|---|
| [Task workspace checkpoints](task-workspace-checkpoints.md) | Direction | Partial | Durable workspace continuity for Task and TaskRun execution |
| [Portal navigation and Space context](portal-navigation-and-space-context.md) | Specification | Complete | Canonical Space routes, scoped navigation, switching, and orientation |
| [Portal work and execution experience](portal-work-and-execution-experience.md) | Specification | Complete | Issue-centered work, the List and Board views, explicit execution, and trustworthy provenance |
| [Portal frontend page system](portal-frontend-page-system.md) | Active plan | Partial | Shared actions, page anatomy, and staged migration of Portal work views |
| [Portal workflow visual editor](portal-workflow-visual-editor.md) | Specification | Complete | Graph-first workflow authoring with a visual canvas and raw JSON |
| [Portal state and permission feedback](portal-state-and-permission-feedback.md) | Active plan | Partial | Loading, empty, error, stale, and authorization presentation |
| [Portal data and plugin surfaces](portal-data-and-plugin-surfaces.md) | Specification | Complete | Files, Artifacts, Marketplace, and scoped plugin actions |
| [Portal responsive and accessible interaction](portal-responsive-and-accessible-interaction.md) | Specification | Complete | Narrow layouts, keyboard behavior, dialogs, and viewport evidence |
| [Issue agent access](issue-agent-access.md) | Specification | Superseded in part | What an Agent may assert about the Issue it works; the `buildmax` mechanism is owned by [Agent Bridge CLI](agent-bridge-cli.md) |
| [Space governance](space-governance.md) | Active plan | Complete | Roles, quota, workflow lifecycle, audit, and retention |
| [System administration](system-administration.md) | Specification | Complete | Deployment-wide authority and operator surfaces |
| [Plugin distribution and private marketplace](plugin-marketplace.md) | Active plan | Partial | Publishing, installing, and managing plugins |
| [Space and worker plugin distribution](plugin-space-distribution.md) | Active plan | Partial | Space activation, Agent selection, and worker delivery |
| [Entity identity and relational keys](entity-identity.md) | Active plan | Complete | Public identifiers, relational keys, and store boundaries |
| [Workflow runtime](workflow-runtime.md) | Active plan | Partial | Durable adaptive graphs over Task and TaskRun |
| [Unified artifacts](unified-artifacts.md) | Active plan | Complete | Space-owned artifact storage and Agent upload |
| [Artifact public sharing and preview](artifact-public-sharing-and-preview.md) | Active plan | Complete | Revocable public links and safe rich previews |
| [Space membership lifecycle](space-membership-lifecycle.md) | Specification | Complete | Invitation, role change, ownership transfer, and recovery |
| [Timestamp representation](timestamp-representation.md) | Specification | Complete | Canonical persisted and API timestamp representation |

## Trust and Security

| Document | Lifecycle | Progress | Covers |
|---|---|---|---|
| [Agent Core trust harness](trust-harness.md) | Active plan | Complete | Containment, observability, and evidence across Agent execution surfaces |
| [Worker API network boundary](worker-api-network-boundary.md) | Specification | Complete | Separation and authorization of public and worker traffic |
| [gVisor worker runtime](gvisor-worker-runtime.md) | Direction | Conditional | Conditional worker Pod isolation behind an optional RuntimeClass |
| [Agent-scoped sandbox policy](agent-sandbox-policy.md) | Active plan | Complete | Agent revisions, Space defaults, and claim-time sandbox selection |
| [Space Secrets and run delivery](space-secrets.md) | Active plan | Partial | Space-owned credentials and run-scoped materialization |
| [Tool permissions](tool-permissions.md) | Active plan | Complete | Runtime tool allow, deny, and approval policy |
| [Sandbox boundaries](sandbox-boundaries.md) | Specification | Complete | Local and worker command containment boundaries |
| [Worker run token](worker-run-token.md) | Specification | Complete | The run-scoped credential accepted by worker routes |
| [Enterprise identity and access](enterprise-identity-and-access.md) | Direction | Partial | Durable sessions, Portal cookie auth, OIDC sign-in and external-identity linking ship; real-provider rotation, outage, offboarding, and break-glass qualification remain |

## Operations and Deployment

| Document | Lifecycle | Progress | Covers |
|---|---|---|---|
| [Server coordination](server-coordination.md) | Active plan | Complete | Shipped Redis fan-out, turn leases, write fencing, and deployed qualification |
| [Enterprise deployment](enterprise-deployment.md) | Active plan | Partial | Supported private deployment shape and operating gaps |
| [Graceful shutdown](graceful-shutdown.md) | Active plan | Complete | Draining, quiescing, worker interruption, and bounded shutdown |

## Verification

| Document | Lifecycle | Progress | Covers |
|---|---|---|---|
| [Evaluation and qualification](evaluation-system.md) | Active plan | Partial | Black-box adapters, graders, suites, and external benchmarks |
| [Local end-to-end verification](end-to-end-testing.md) | Active plan | Partial | Deterministic local, Desktop, Portal, and deployment journeys |
| [Verification program](verification-program.md) | Active plan | Partial | Risk-based evidence from pull request checks through release rehearsal |

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
4. Mark its lifecycle as Direction, Active plan, or Specification, and add its
   concise progress label using the definitions above.
5. If it ships something a user configures, write the user-facing half in the
   `manual/` or `reference/` and link it from the record.

When it stops describing the current direction, **delete it** and remove its
row. Git history keeps it; see
[contribute/documentation.md](../contribute/documentation.md).
