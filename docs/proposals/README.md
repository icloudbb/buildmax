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
| [Risk-driven end-to-end verification expansion](risk-driven-e2e-expansion.md) | Verification | Which small set of new end-to-end journeys most reduces the remaining Beta risk, and which boundary should prove each one? | Normal-path coverage is broad; targeted worker-loss, restart, dependency-denial, partial-work cancellation, and unified evidence remain open |
| [Enterprise capabilities and commercial boundaries — memo](enterprise-capabilities-and-commercial-boundaries.md) | Operations and Deployment | Which capabilities should an enterprise deployment provide, and where might community, paid additions, and services divide? | Discussion memo only; links existing foundations and proposals without approving an enterprise edition |
| [Single-maintainer Agent development workflow](single-maintainer-agent-development.md) | Verification | How can one maintainer increase accepted development throughput with coding Agents without becoming the workflow bottleneck? | The supporting workflow pieces exist, but readiness revalidation, leases, changed-scope verification, and independent acceptance do not form one closed loop |
| [System administration operations](system-administration-operations.md) | Operations and Deployment | How should the operator CLI and Portal provide safe outcome parity for administration and runtime health? | Core admin surfaces shipped; transactional audit, CLI session parity, quota assignment, and richer runtime operations remain open |
| [Client sessions and API credentials](client-sessions-and-api-credentials.md) | Trust and Security | Which credentials should interactive, native, and unattended clients receive? | Session-chain listing and revocation and OS-credential-store secret storage shipped; explicit session state, expiry, claims, and self-service remain open |
| [Enterprise identity and access](enterprise-identity-and-access.md) | Trust and Security | How should a private deployment connect corporate identity to BuildMax Spaces and roles? | Reviewable OIDC design drafted from current code and standards; no product implementation or accepted roadmap slice |
| [Durable Agent sessions](durable-agent-sessions.md) | Local Experience | Should authenticated local Agent sessions become revisioned Server resources? | Nothing; no Server route serves a Session resource |
| [Assistant orchestration and the Workflow boundary](assistant-orchestration-and-workflow-boundary.md) | Product and Execution Model | Does a manager Agent justify an Assistant product, and should Workflow narrow toward deterministic Automation? | Nothing; Agents cannot admit durable child Space Agent Tasks |
| [Agent self-regulation capabilities](agent-self-regulation-capabilities.md) | Agent Runtime and Models | Which runtime-visible meta-capabilities materially improve an Agent's ability to regulate its own work without creating a model-owned control plane? | Existing goals, events, tools, permissions, traces, checkpoints, delegation, and memory are candidate foundations; no unified self-regulation contract exists |
| [Local Issue work bridge](local-issue-work-bridge.md) | Local Experience | How should connected local surfaces work with Space Issues? | R5 item 1 schedules the remaining Phase 1 decision; the durable Issue-to-Session link and later phases remain open |
| [Session tree, agent mailbox, and branched workspaces](session-tree-and-agent-mailbox.md) | Local Experience | Should sessions fork isolated workspaces and resume parents through a durable mailbox? | Nothing |
| [Issue topic coordination and Agent Blackboard](issue-topic-coordination.md) | Product and Execution Model | Should child Issue participants share a parent-scoped information feed while addressed delivery and synchronization remain separate? | Existing Issue comments and scoped Agent read/report tools are the proposed validation substrate; no cross-child Topic feed exists |
| [Agent 原生协作底座](agent-native-collaboration-substrate.md) | Product and Execution Model | 不同规模和不同专业背景的参与者，是否需要一套统一的意图、执行、提议、证据、决策、集成与知识生命周期？ | 尚未建设；当前 Issue、Task/TaskRun、Artifact、Space 与本地 workspace 是待验证的基础构件 |

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
