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
| [Risk-driven end-to-end verification expansion](risk-driven-e2e-expansion.md) | Verification | Which small set of new end-to-end journeys most reduces the remaining Beta risk, and which boundary should prove each one? | Graceful worker-loss plus MySQL and object-storage readiness outage/recovery probes ship; Server restart/reconnect, worker write denial, partial-work cancellation, and unified candidate evidence remain open |
| [Enterprise capability requirements inventory](enterprise-capability-requirements.md) | Operations and Deployment | Which candidate requirements matter for enterprise deployments, and what evidence would validate each one? | Requirements inventory only; links existing foundations without defining an enterprise edition or feature boundary |
| [Personnel deactivation and execution authority](personnel-deactivation-lifecycle.md) | Operations and Deployment | When account or Space authority is removed, which credentials and unattended executions stop, how fast, and what remains Space-owned? | Proposed contract verified against current account, Schedule, TaskRun, Workflow, and Space behavior; no unified lifecycle is implemented |
| [Single-maintainer Agent development workflow](single-maintainer-agent-development.md) | Verification | How can one maintainer increase accepted development throughput with coding Agents without becoming the workflow bottleneck? | The supporting workflow pieces exist, but readiness revalidation, leases, changed-scope verification, and independent acceptance do not form one closed loop |
| [System administration operations](system-administration-operations.md) | Operations and Deployment | How should the operator CLI and Portal provide safe outcome parity for administration and runtime health? | Core admin surfaces, OIDC diagnostics, and external-identity administration ship; transactional audit, CLI session parity, quota assignment, and richer runtime operations remain open |
| [Client sessions and API credentials](client-sessions-and-api-credentials.md) | Trust and Security | Which credentials should interactive, native, and unattended clients receive? | Durable session state, absolute expiry, per-request revocation, Portal cookie auth, and native OS secret storage ship; scopes, signing-key rotation, self-service, PATs, and service accounts remain open |
| [Durable Agent sessions](durable-agent-sessions.md) | Local Experience | Should authenticated local Agent sessions become revisioned Server resources? | Nothing; no Server route serves a Session resource |
| [Assistant orchestration and the Workflow boundary](assistant-orchestration-and-workflow-boundary.md) | Product and Execution Model | Does a manager Agent justify an Assistant product, and should Workflow narrow toward deterministic Automation? | Nothing; Agents cannot admit durable child Space Agent Tasks |
| [Agent self-regulation capabilities](agent-self-regulation-capabilities.md) | Agent Runtime and Models | Which runtime-visible meta-capabilities materially improve an Agent's ability to regulate its own work without creating a model-owned control plane? | Existing goals, events, tools, permissions, traces, checkpoints, delegation, and memory are candidate foundations; no unified self-regulation contract exists |
| [Local Issue work bridge](local-issue-work-bridge.md) | Local Experience | How should connected local surfaces work with Space Issues? | R5 item 1 schedules the remaining Phase 1 decision; the durable Issue-to-Session link and later phases remain open |
| [Session tree, agent mailbox, and branched workspaces](session-tree-and-agent-mailbox.md) | Local Experience | Should sessions fork isolated workspaces and resume parents through a durable mailbox? | Nothing |
| [Portal Issue board as a derived work view](portal-issue-board-view.md) | Product and Execution Model | Should Portal project Space Issues into a fixed three-lane board without creating a second planning model? | Existing Issue statuses, versioned updates, top-level filtering, Owner/Executor filters, and derived child progress are sufficient foundations; no Board view ships |
| [Issue topic coordination and Agent Blackboard](issue-topic-coordination.md) | Product and Execution Model | Should child Issue participants share a parent-scoped information feed while addressed delivery and synchronization remain separate? | Existing Issue comments and scoped Agent read/report tools are the proposed validation substrate; no cross-child Topic feed exists |
| [Agent 原生协作底座](agent-native-collaboration-substrate.md) | Product and Execution Model | 不同规模和不同专业背景的参与者，是否需要一套统一的意图、执行、提议、证据、决策、集成与知识生命周期？ | 尚未建设；当前 Issue、Task/TaskRun、Artifact、Space 与本地 workspace 是待验证的基础构件 |
| [Desktop workspace tabs and the Explorer sidebar](desktop-workspace-tabs.md) | Local Experience | Should Desktop reshape around one center surface of heterogeneous tabs (chat, terminal, file, diff) fed by a project-scoped Explorer sidebar, with the local terminal as one tab kind? | An exploratory prototype of the terminal transport (Go PTY session manager plus an xterm tab), provisionally placed as a bottom panel; the tab surface, Explorer reframe, and file/diff tabs are not built, and concurrent agent tabs remain gated on workspace isolation |
| [Client surface convergence across Desktop, Web, and mobile](client-surface-convergence.md) | Local Experience | Should one shared UI over a switchable data layer serve local-native, cloud-web, and thin-mobile modes, rather than migrating the desktop shell (for example to Tauri)? | `@buildmax/gui` shares presentation across Desktop and Portal; Portal plus `buildmax-server` already provide the network path, but Desktop's data layer stays Wails-only and no switchable data interface or mobile client exists |

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
