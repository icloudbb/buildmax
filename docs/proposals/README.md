# Proposals

> **简体中文：** [阅读中文镜像](../zh-CN/proposals/README.md)
>
> **Audience:** contributors and early adopters · **Status:** current

Proposals are short papers for cross-cutting directions that are worth
discussing before they become roadmap work. They are not commitments, product
announcements, or user documentation.

## How This Directory Works

| Artifact | Purpose |
|---|---|
| [../ROADMAP.md](../ROADMAP.md) | Prioritized and accepted work |
| [../design/](../design/README.md) | Accepted rationale and active plans |
| This directory | Open questions and options before a direction is accepted |
| GitHub Discussions | Early community feedback and alternatives |
| GitHub Issues | Implementable work with an owner and acceptance criteria |

Every proposal opens with `Status: proposal — under discussion` and an
`Opened: YYYY-MM-DD` date, identifies related current documents, and separates
goals, non-goals, options, and open questions. It should be narrow enough that
readers can agree, disagree, or offer evidence without first
reverse-engineering the repository.

When a decision is made, update [../ROADMAP.md](../ROADMAP.md), the matching
design record, or a GitHub Issue. Then delete the proposal. Rejected and
superseded proposals are also deleted rather than archived; git history keeps
their context.

## Open Proposals

A paper stays open until its direction is accepted, which is not the same as
nothing being built. Where an early slice shipped ahead of the decision, the
last column says so, and the paper's own delivery phases hold the detail.

| Proposal | Primary domain | Question | Built so far |
|---|---|---|---|
| [Enterprise capability requirements inventory](enterprise-capability-requirements.md) | Operations and Deployment | Which candidate requirements matter for enterprise deployments, and what evidence would validate each one? | Inventory only; no requirement is validated by a named deployment. Linked foundations ship (OIDC sign-in, administration surfaces, guided account deactivation with execution-eligibility gates, and disabled-owner recovery); quota, suspension, and incident journeys are unexamined |
| [Single-maintainer Agent development workflow](single-maintainer-agent-development.md) | Verification | How can one maintainer increase accepted development throughput with coding Agents without becoming the workflow bottleneck? | The in-repo backlog with single-claim frontmatter, the `./make board` status view, and its frontmatter check ship; changed-scope verification, automated readiness revalidation, a pull-request delivery check, independent acceptance, and workspace reclamation are not built |
| [Cross-Space work visibility for deployment administrators](admin-cross-space-work-visibility.md) | Operations and Deployment | Should administrators enter every Space or see cross-Space work in Administration, and which operational facts can they see without Space membership? | Proposal only; Administration already shows Space metadata, usage, disabled-owner recovery, cross-Space audit, the LLM call ledger, and TaskRun status counts. No global Agent, Workflow, Schedule, or Issue inventory, and no membership-scoped cross-Space view |
| [Client sessions and API credentials](client-sessions-and-api-credentials.md) | Trust and Security | Which credentials should interactive, native, and unattended clients receive? | Durable session state, absolute expiry, per-request revocation, Portal cookie auth, and native OS secret storage ship; scopes, signing-key rotation, self-service, PATs, and service accounts remain open |
| [Durable Agent sessions](durable-agent-sessions.md) | Local Experience | Should authenticated local Agent sessions become revisioned Server resources? | Nothing; no revisioned Server Session resource or route exists. Task-scoped worker session bundles persist in run storage, and Remote Control relays a live local session without storing its transcript |
| [Assistant orchestration and the Workflow boundary](assistant-orchestration-and-workflow-boundary.md) | Product and Execution Model | Does a manager Agent justify an Assistant product, and should Workflow narrow toward deterministic Automation? | Portal chat can list, run, and observe published Workflows (§9.5); no bounded Agent-to-Agent delegation — Agents cannot admit durable child Space Agent Tasks |
| [Agent self-regulation capabilities](agent-self-regulation-capabilities.md) | Agent Runtime and Models | Which runtime-visible meta-capabilities materially improve an Agent's ability to regulate its own work without creating a model-owned control plane? | Existing goals, events, tools, permissions, traces, checkpoints, delegation, and memory are candidate foundations; no unified self-regulation contract exists |
| [Local Issue work bridge](local-issue-work-bridge.md) | Local Experience | How should connected local surfaces work with Space Issues? | `buildmax issue list/show/status/start/comment` ship, and Agents read and report through `buildmax issue` ([Agent Bridge CLI](../design/agent-bridge-cli.md)); R5 item 1 schedules the remaining Phase 1 decision — the durable Issue-to-Session link, workspace mapping, local-result projection, and any Desktop Issue surface remain open |
| [Session tree, agent mailbox, and branched workspaces](session-tree-and-agent-mailbox.md) | Local Experience | Should sessions fork isolated workspaces and resume parents through a durable mailbox? | Local physical-copy fork with `forked_from` provenance, a read-only fork tree in `buildmax info` and TUI/Desktop `/info`, and Agent-managed Git worktrees ship; no fork-time workspace isolation, parent inbox, durable mailbox, `ReportToParent`, or supervised resume exists |
| [Portal Issue board as a derived work view](portal-issue-board-view.md) | Product and Execution Model | Should Portal project Space Issues into a fixed three-lane board without creating a second planning model? | Existing Issue statuses, versioned updates, top-level filtering, Owner/Executor filters, and derived child progress are sufficient foundations; no Board view ships |
| [Issue topic coordination and Agent Blackboard](issue-topic-coordination.md) | Product and Execution Model | Should child Issue participants share a parent-scoped information feed while addressed delivery and synchronization remain separate? | Existing Issue comments plus `buildmax issue show`/`comment` (scoped to one Issue in a worker run) are the proposed validation substrate; no cross-child Topic feed exists |
| [Agent-native collaboration substrate](agent-native-collaboration-substrate.md) | Product and Execution Model | Do participants of different scale and expertise need one lifecycle for intent, execution, proposals, evidence, decisions, integration, and knowledge? | Nothing; Issue, Task/TaskRun, Artifact, Space, and local workspaces are the foundations to validate. |
| [Client surface convergence across Desktop, Web, and mobile](client-surface-convergence.md) | Local Experience | Should one shared UI over a switchable data layer serve local-native, cloud-web, and thin-mobile modes, rather than migrating the desktop shell (for example to Tauri)? | `@buildmax/gui` shares presentation; Portal plus `buildmax-server` provide the network path, Portal's narrow-width layouts ship, and Remote Control lets a phone browser watch and steer a local session; Desktop's data layer is still Wails-only, and no switchable data interface, Environment plane, PWA, or native mobile client exists |
| [Agent delegation to user applications](agent-app-delegation.md) | Local Experience and Trust | Can a workspace-centered Agent become a useful entry point to connected applications while keeping authorization and approval understandable? | Plugin connector CLI (`buildmax connect`, `buildmax app`) with OAuth and PKCE, fixed HTTP operations, and a Gmail sample; remote `connect mcp` and the `buildmax mcp` tools, schema, and call commands; no per-run grants, call audit, application-first connect, or hardened Agent boundary |

Retired proposals do not remain in this live index. Accepted rationale moves to
[design records](../design/README.md), and rejected or superseded discussion
remains available in Git history.

## Starting A Proposal

Use a semantic filename. Start from the sections used by the existing papers:

1. Problem and current context.
2. Goals and non-goals.
3. Options and trade-offs.
4. Open questions and evidence needed for a decision.
5. Likely destination if accepted.

Choose the primary domain where a contributor would look for the question, add
the row to [Open Proposals](#open-proposals), and keep the last column true as
slices land. A reader who consults only the index and finds "Built so far"
stale will conclude the wrong thing about the whole directory.

When a question needs independently authored Agent positions, use a semantic
directory with a `README.md` that owns the question and decision process, plus
one explicitly attributed `<agent-name>-view.md` per contributor. Index only
the directory's README here. Contributors do not edit one another's positions;
a later synthesis preserves disagreements and moves accepted rationale into the
normal design record.

Do not create a proposal for a focused bug, a documentation correction, or an
implementation task that already has acceptance criteria. Use an Issue instead.
