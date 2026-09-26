# Agent-Native Collaboration Substrate Memo

> **简体中文：** [阅读中文镜像](../zh-CN/proposals/agent-native-collaboration-substrate.md)
>
> **Audience:** product designers, maintainers, and early adopters · **Status:** proposal — under discussion
>
> **Opened:** 2026-09-06

Related: [product vision](../design/product-vision.md),
[current state](../current-state.md), [roadmap](../ROADMAP.md),
[unified Artifacts](../design/unified-artifacts.md),
[local Project Memory](../design/local-project-memory.md),
[Agent execution and Task threads](../design/agent-execution-and-task-threads.md),
[Agent orchestration and Task workspace continuity decisions](../design/orchestration-and-continuity-decisions.md), and
[workspace root and worktrees](../design/workspace-root-and-worktrees.md).

## Contents

- [1. Memo Conclusion](#1-memo-conclusion)
- [2. User Outcome, Current Evidence, And Constraints](#2-user-outcome-current-evidence-and-constraints)
- [3. Starting From The Wiki Question](#3-starting-from-the-wiki-question)
- [4. Boundaries Between Artifact, Wiki, Git, And Figma](#4-boundaries-between-artifact-wiki-git-and-figma)
- [5. The Shifting Boundary Between Technical And Non-Technical People](#5-the-shifting-boundary-between-technical-and-non-technical-people)
- [6. The Collaboration Loop Shared By Projects Of Every Scale](#6-the-collaboration-loop-shared-by-projects-of-every-scale)
- [7. Candidate Collaboration Substrate](#7-candidate-collaboration-substrate)
- [8. Where A Versioned Workspace Fits](#8-where-a-versioned-workspace-fits)
- [9. Direction Options And Trade-Offs](#9-direction-options-and-trade-offs)
- [10. Product And Architecture Implications](#10-product-and-architecture-implications)
- [11. Goals And Non-Goals](#11-goals-and-non-goals)
- [12. Key Questions To Validate](#12-key-questions-to-validate)
- [13. Suggested Validation Path](#13-suggested-validation-path)
- [14. Where This Could Land If The Direction Holds](#14-where-this-could-land-if-the-direction-holds)

## 1. Memo Conclusion

BuildMax may genuinely be missing a large collaboration capability, but there
is not yet enough evidence to define it as a standalone Wiki, nor enough
evidence that a general-purpose versioned workspace could support all
collaboration in small, medium, and large projects.

The hypothesis more worth validating is:

> BuildMax needs an Agent-native collaboration lifecycle in which people and
> Agents start from intent, propose changes inside a controlled environment,
> support them with inspectable evidence, pass them through an authorized
> decision into shared state, and settle the outcome into knowledge that can be
> reused later.

Wiki, Git, Figma, Artifact, and workspaces may each carry part of this
lifecycle, but no single storage or editing tool should be presupposed as the
whole collaboration system.

Until there is usage evidence, the recommendation is:

1. Do not create a standalone Wiki product and data model.
2. Do not describe a versioned workspace as a complete collaboration solution.
3. First validate the cross-content-type loop `Intent → Execution → Proposal →
   Evidence → Decision → Integration → Knowledge`.
4. Prefer making Git, external knowledge systems, and design systems pluggable
   authoritative sources; consider BuildMax storing its own editable knowledge
   pages only if external sources cannot satisfy the core flow.

## 2. User Outcome, Current Evidence, And Constraints

### 2.1 Essential user outcome

Regardless of project scale or participant background, what users need is not
one particular collaboration tool, but this:

> Multiple people and Agents can safely change shared state around a common
> goal, understand what each other is doing, judge results with sufficient
> evidence, and know clearly what has been accepted and published.

### 2.2 What the current product already proves

The current product model already provides several necessary building blocks:

| Current concept | Responsibility it already carries |
|---|---|
| Space | Ownership and authorization boundary for Portal resources; older records called it Team |
| Issue | Primary user-facing work object; expresses work and links discussion and execution |
| Conversation | Foreground interaction and optional orchestration; not a mandatory parent of execution |
| Task / TaskRun | A continuing Agent thread and one concrete execution or attempt |
| Result / Artifact | Visible results, stable references, immutable products, and provenance |
| Local Project / Workspace | Local project identity and the directory one execution actually uses |
| Project Memory | Small, inspectable, forgettable cross-Session recall for CLI/Desktop |

These objects cover most of the vertical chain from goal through execution to
result, but they do not automatically prove how people and Agents jointly
propose, review, accept, and integrate a change that spans content types.

### 2.3 Current constraints

- The roadmap's near-term focus is operational trustworthiness, not adding a
  new large feature family.
- Artifact's core contract is one immutable file; adding in-place editing, page
  trees, and a current-version pointer to it would weaken stable references
  and evidence semantics.
- Project Memory is local, low-authority Agent recall, and by design explicitly
  does not cover Space, Portal, or worker knowledge.
- BuildMax has no general-purpose versioned workspace service today, and does
  not promise that arbitrary file changes can be restored; Task workspace
  checkpoints address the narrow problem of execution continuity, not general
  collaboration history.
- Space is the authorization boundary for shared Portal resources. Any
  knowledge, workspace, or change-proposal capability that becomes a Server
  capability must continue to obey that boundary.
- The CLI/TUI must remain a single Go binary; the collaboration model must not
  make the shared Agent Core depend on a Portal-specific implementation.

## 3. Starting From The Wiki Question

The discussion began from an intuition: BuildMax can already have Agents
execute work and produce results, yet lacks a natural place to turn those
results into long-lived team knowledge.

The current chain is roughly:

```text
Conversation → Issue → Task / TaskRun → Result / Artifact
```

The loop that may be missing is:

```text
Conversation → Work → Outcome → Knowledge → Future Agent work
                                      ↑_____________|
```

This gap is real, but "missing a knowledge loop" does not mean "must build our
own Wiki". Existing objects cannot fully replace a Wiki:

- Issue expresses work to be done, not knowledge that stays valid long-term.
- Conversation is an interaction record, not curated and verified fact.
- Artifact is an immutable product, not a continuously evolving authoritative
  page.
- Project Memory is useful Agent recall, not a Space's formal knowledge.
- `AGENTS.md` is normative instruction and cannot become a container for
  ordinary team knowledge.

Conversely, mature Wikis, Git repositories, and existing knowledge platforms
already provide editing, versioning, permissions, history, search, and
organization. If BuildMax merely copied those capabilities, it would not create
distinctive value commensurate with an Agent execution platform.

## 4. Boundaries Between Artifact, Wiki, Git, And Figma

### 4.1 Artifact and Wiki

In one sentence:

> An Artifact records "what this run produced"; a Wiki expresses "what the team
> currently believes is correct".

| Dimension | Artifact | Wiki page |
|---|---|---|
| Primary use | Delivered results and evidence | Maintaining current knowledge |
| Unit of content | One file of any type | An editable, structured document |
| Identity semantics | One ID always maps to the same content | Page ID is stable; the body gets new revisions |
| How it changes | A change creates a new Artifact | Revisions are saved under the same page |
| Organization | Lists, provenance, and attachment relationships | Hierarchy, links, backlinks, and search |
| Lifecycle | Publish, share, retain, expire, delete | Edit, review, verify, archive |

The two can connect: a TaskRun's Artifact is raw evidence, and a page in some
knowledge source can absorb its conclusions while keeping provenance back to
the Artifact, Issue, and TaskRun. They should not become the same product
object. Underlying storage and preview mechanisms can be reused, but that must
not blur their identity and mutability contracts.

### 4.2 Git as the substrate for knowledge and outcomes

For project-related Markdown, HTML, design tokens, configuration, and code, Git
already provides:

- version history and diffs;
- parallel branches and isolated changes;
- review, ownership conventions, and merge control;
- local, offline, and portable storage;
- a textual form that Agents can easily read and modify;
- the ability for knowledge, prototypes, and implementation to evolve together
  in one change.

Git's gaps are mainly in human interaction and cross-source knowledge, not in
the versioning mechanism itself. Non-technical people need not learn
command-line operations; Agents can create branches, commit, resolve conflicts,
and merge on their behalf, while BuildMax presents "change proposals, diffs,
accept, undo, and publish" in terms users can understand.

### 4.3 Agent-generated HTML and Figma

Agent-generated HTML will replace part of traditional prototyping work,
especially responsive pages, real interactions, user testing, and prototypes
that may evolve into production implementations. BuildMax's HTML Artifact
preview and sharing fit these immutable published snapshots; continuously
evolving source files belong in Git or another source with an explicit
versioning contract.

This need not imply that Figma disappears. A canvas remains well suited to
non-linear exploration while requirements are fuzzy, laying many options side
by side, fine visual adjustment, design systems, and multi-person commenting.
The more likely outcome is two-way conversion between natural language,
runnable code, and visual canvas, rather than any one of them permanently
becoming the sole authoritative source for every stage.

Figma already offers both an MCP connection that Agents can read and write and
publishable functional prototypes, which shows that the market itself is
moving toward code and canvas working together rather than simply replacing
each other:
[Figma MCP](https://help.figma.com/hc/en-us/articles/39216419318551-Get-started-with-the-Figma-MCP-server),
[Figma Make](https://help.figma.com/hc/en-us/articles/31304586129559-Publish-update-or-unpublish-a-functional-prototype-or-web-app).

## 5. The Shifting Boundary Between Technical And Non-Technical People

Traditional division of labor often treats "can you operate the implementation
tools" as the boundary between technical and non-technical people. Agents are
lowering the barrier of code syntax, Git commands, deployment steps, and tool
operation, so a new generation of product designers, operators, and domain
experts may directly generate and modify executable systems.

A more accurate judgment is:

> What is being flattened is mainly the boundary of tool operation and
> implementation syntax, not the boundaries of domain knowledge, judgment of
> results, and responsibility.

Future division of labor is more likely to form around these questions:

- who can state goals and constraints accurately;
- who can judge whether a result is correct in a particular domain;
- who understands the impact and risk of failure;
- who has the authority to accept, publish, or revert a change;
- who is accountable for the final result.

Non-technical people may never learn `rebase`, `cherry-pick`, or the textual
format of a merge conflict, but they will understand versions, change
proposals, diffs, acceptance, publishing, and rollback. Agents translate these
user concepts into underlying operations:

| User concept | Possible Git implementation |
|---|---|
| Create a change proposal | Create a branch or worktree |
| Save a change | commit |
| View what changed | diff and semantic preview |
| Accept a proposal | merge after review |
| Abandon a proposal | Close the isolated workspace |
| Return to a past state | revert, or create a new change from an old version |
| Publish | Merge, deploy, and create an Artifact |

The product should therefore not duplicate execution capability into
"developer features" and "non-technical features". A more stable distinction
is interaction density, where the work happens, authority, and risk level.

## 6. The Collaboration Loop Shared By Projects Of Every Scale

A versioned workspace can support shared state, change isolation, history,
parallel options, and recovery, but on its own it cannot answer why to change
something, who is responsible, what counts as done, what the dependencies on
other work are, who can approve, and what evidence is sufficient.

Regardless of project scale, collaboration can first be abstracted into the
same loop:

```text
Intent → Decomposition → Execution → Verification → Decision → Integration → Consolidation
```

| Stage | Question it must answer |
|---|---|
| Intent | What outcome should become real, why does it matter, and what is the completion criterion? |
| Decomposition | Which work can proceed independently, and how does it depend on other work? |
| Execution | Who, or which Agent, did what within which boundary? |
| Verification | What changes, previews, tests, reports, or other evidence exist? |
| Decision | Who accepts, rejects, or requests changes, and why? |
| Integration | How does an accepted result enter shared state and get published? |
| Consolidation | Which decisions and knowledge are worth reusing later, and where is the authoritative source? |

Scale changes topology and policy strength, not the basic semantics:

| Aspect | Small project | Large project |
|---|---|---|
| Decomposition | A few peer pieces of work | Multi-level goals and dependencies |
| Responsibility | One person holds several roles | Explicit owners, reviewers, and approvers |
| Concurrency | A few changes | Many parallel, cross-team proposals |
| Verification | People look at the result | Automated checks, tiered review, and policy gates |
| Integration | Direct acceptance | Cross-team coordination, release windows, and rollback plans |
| Information flow | Participants learn naturally | Subscriptions, notifications, and aggregate views |
| Consolidation | Ad hoc notes | Owners, validity periods, audit, and retrieval |

The substrate does not need two sets of "small project" and "large project"
semantics. Scaling should come mainly from the number of relationships, policy
strength, aggregate views, and automation.

## 7. Candidate Collaboration Substrate

The following are collaboration semantics that may hold across project scales
and content types. They are first a protocol; they do not mean each must become
a new database entity:

| Semantic | Question it answers | Existing or candidate carrier |
|---|---|---|
| Scope | Who jointly owns the work and results? | Space; whether a Server Project is ever needed remains open |
| Intent | Why do it, and what is the completion criterion? | Issue |
| Responsibility | Who drives it, and who has the authority to decide? | Issue Owner/Executor, Space role, and specific capabilities |
| Execution | Who actually did what, and when? | Task / TaskRun |
| Proposal | How is shared state proposed to change? | Candidate protocol; the substrate can be Git, Figma, a Wiki, or an API |
| Evidence | Why should this result be trusted? | Diffs, tests, Artifacts, Traces |
| Decision | Is it accepted, and why? | Candidate lifecycle; cannot be equated with a comment |
| Outcome | What was ultimately produced or changed? | Result, Artifact, or external system state |
| History | How did the whole process evolve? | Timeline, Trace, and Audit, each with different authority |
| Knowledge | Which conclusions are for future reuse? | Git, an external Wiki, a design system, or a future built-in source |

A possible common backbone is:

```text
Issue
  └── Task / TaskRun
        ├── raises a Proposal
        ├── attaches Evidence
        └── awaits an authorized Decision
              ├── accept and Integration
              ├── request changes
              └── reject
```

Proposal implementations need not be the same:

- code and project documentation can be a Git branch/commit;
- an HTML prototype can be a Git change plus an Artifact preview;
- Confluence or Notion can be a page draft or suggested revision;
- Figma can be a controlled change in a design file;
- a configuration change can be a set of not-yet-applied API operations;
- pure research may not change shared state at all; its reviewable result is
  itself the Proposal.

If BuildMax takes this direction, what it should unify is how changes are
presented, verified, authorized, decided, and given provenance, not copying all
content into one general-purpose store.

## 8. Where A Versioned Workspace Fits

A versioned workspace is an important part of the candidate substrate, but it
covers only part of the collaboration loop.

It is well suited to owning:

- the shared state that work is based on;
- isolation of parallel changes;
- diffs between old and new versions;
- integration of accepted changes;
- recovery after failure and reproducibility.

It does not naturally own:

- goals and acceptance criteria;
- work decomposition and cross-team dependencies;
- responsibility and approval authority;
- whether the evidence for a result is sufficient;
- decision rationale and notifications;
- knowledge discovery across repositories, Wikis, and design systems.

So we should not first build an "all-purpose versioned workspace" and then
migrate every collaboration problem into it. The safer direction is to have
different authoritative sources implement a limited set of collaboration
operations — for example, read current state, create an isolated proposal, show
a diff, verify, accept, or withdraw — and state explicitly what each source
cannot guarantee.

## 9. Direction Options And Trade-Offs

### 9.1 Option A: a standalone BuildMax Wiki

Pros: a closed loop inside the deployment, consistent Space authorization, and
easy unification of Agent reads/writes with TaskRun provenance.

Cons: requires an editor, versioning, search, hierarchy, links, permissions,
import/export, conflicts, notifications, and retention policy; competes
redundantly with mature Wikis and Git; the concept and state cost is too high
before users have shown they need BuildMax to be the authoritative knowledge
source.

Current judgment: should not be prioritized.

### 9.2 Option B: centered on a general-purpose versioned workspace

Pros: documents, code, HTML, and configuration can share versioning, diff,
isolation, and recovery mechanisms; well suited to opening real project changes
to more roles once Agents operate Git on their behalf.

Cons: cannot by itself express intent, responsibility, dependencies,
verification, approval, or cross-source knowledge; if the abstraction exceeds
the semantics real sources such as Git can guarantee, it easily becomes a
hidden versioning system that cannot recover honestly.

Current judgment: worth researching as an execution and integration capability,
but not a complete collaboration product model.

### 9.3 Option C: centered on the collaboration lifecycle

Pros: first unifies the intent, execution, proposal, evidence, decision,
integration, and knowledge loop that all projects share; Git, Wikis, Figma, and
APIs can each remain their own authoritative source; aligns with BuildMax's
existing Issue, TaskRun, Artifact, Trace, and Space boundaries.

Cons: the minimal semantics of Proposal and Decision are not yet proven;
failure, permissions, and consistency across source adapters will be complex;
if designed too abstractly, it can also become an "all-purpose work model" with
no concrete user experience.

Current judgment: the primary hypothesis most worth validating, but not yet an
accepted direction.

### 9.4 Option D: source connections only, no collaboration protocol

Pros: the least new state; Agents can quickly search Git, Confluence, Notion,
or Figma.

Cons: solves only reading, not change, review, acceptance, or closing the
provenance loop; output from different tools keeps living in one-off
conversations.

Current judgment: can be an early validation technique, but is not necessarily
enough to be the long-term substrate.

## 10. Product And Architecture Implications

If the primary hypothesis holds, BuildMax should be positioned neither as "a
Coding Agent for engineers" nor as "a no-code tool for non-technical people",
but more accurately as:

> A governable Agent execution and outcome system for knowledge workers.

This carries several design implications:

1. **Design by work mode and risk, not by job title.** CLI, Desktop, and
   Portal can have different interaction densities, but important execution
   semantics come from the same Agent Core.
2. **Permissions are organized around actions.** Reading, proposing changes,
   executing, accessing Secrets, accepting, publishing, and managing policy are
   different capabilities and cannot simply be replaced by a "technical user"
   role.
3. **Git can be the engine rather than the interface.** The Agent performs the
   underlying operations; BuildMax shows users change proposals, semantic
   diffs, previews, acceptance, and rollback.
4. **Artifact stays immutable.** It suits being the evidence and snapshot of a
   proposal or a release, and should not become a continuously editable
   workspace.
5. **External sources stay authoritative.** BuildMax should record the source
   versions it actually read and changed, and must not quietly copy and
   elevate their authority for the sake of a unified interface.
6. **Agent operations must be reviewable.** When the user believes something
   is "just a preview", the Agent must not secretly merge, publish, or change
   an external system.
7. **Proposal and Decision may be the gap, not a Wiki.** They connect an
   Issue's intent, a TaskRun's execution, and an Artifact's evidence to real
   changes in shared state.

## 11. Goals And Non-Goals

### 11.1 Goals

- Find the minimal collaboration semantics that do not depend on project scale,
  job role, or content type.
- Validate whether non-technical people can use versioning substrates such as
  Git through Agents without learning tool operation.
- Define how an Agent change is proposed, inspected, approved, integrated, and
  traced.
- Preserve Space authorization, TaskRun's authoritative result, Artifact
  immutability, and the shared Agent Core.
- Use real workflow evidence to decide the later boundaries of Wiki, Knowledge
  Source, and versioned workspace.

### 11.2 Non-goals

- Committing now to build a built-in Wiki, rich-text editor, or enterprise
  search.
- Building a full project-management suite, Gantt charts, Sprints, or an
  unlimited custom-field system.
- Claiming that all knowledge workers will master traditional Git operations.
- Claiming that HTML will fully replace Figma or other visual design systems.
- Copying all the content of Git, Confluence, Notion, and Figma into one
  general-purpose store.
- Making Conversation the mandatory parent of all execution and collaboration
  objects.
- Promising that any change can be undone without an explicit recovery
  contract.

## 12. Key Questions To Validate

### 12.1 Users and workflows

1. Can non-technical participants understand a Git-backed workflow purely
   through goals, previews, diffs, acceptance, and rollback, or do they still
   need to edit pages directly?
2. Which work naturally revolves around one repository, and which belongs to a
   Space or to cross-project knowledge?
3. Is what users review the source diff, the visual difference, the behavior
   change, the test evidence, or a combination?
4. Does one Issue usually correspond to one Proposal, or does it produce
   several competing or staged Proposals?
5. Can pure reports, external API operations, and file changes share a
   sufficiently consistent decision lifecycle?

### 12.2 Authority and responsibility

1. Who can have an Agent read, propose changes, apply, publish, and revert?
2. Does a Proposal inherit the source system's permissions, or does the Space
   need an additional policy?
3. After an Agent resolves a conflict, which cases must regain human
   acceptance?
4. At what risk level is automated verification sufficient to replace human
   approval?

### 12.3 Data and authority

1. Does BuildMax store only source references and evidence, or must it hold the
   Proposal content?
2. When an external source changes after execution, how do the original
   Proposal, evidence, and decision stay explainable?
3. Is knowledge updated in place, created as a new page, or only suggested?
   Who decides its level of authority?
4. Do Git-backed documents cover enough non-code knowledge to defer a
   built-in Wiki?

### 12.4 Scale and operations

1. Going from small to large projects, what fails first: the number of
   relationships, notifications, search, permissions, or execution throughput?
2. Which capabilities can scale through aggregate views over the same
   semantics, and which must introduce new objects?
3. Are cross-team dependencies and multi-level approval a common need, or a
   policy extension for a few deployments?
4. When a source is unavailable, credentials expire, an apply half-fails, or
   changes happen concurrently, what state does the user see?

## 13. Suggested Validation Path

Do not start from a database schema or a general-purpose abstraction. Choose a
real scenario that runs through existing product capabilities: a product
designer, through Portal, has an Agent create and iterate on an HTML prototype.

Candidate journey:

1. The user states the goal and acceptance conditions in an Issue.
2. The Agent creates HTML/CSS/JS in an isolated Git workspace.
3. BuildMax publishes an HTML Artifact that the user can preview without
   downloading.
4. The user requests changes through a Conversation or an Issue comment.
5. The Agent creates a new proposal; Portal shows old and new previews, the
   semantic changes, and the source-file diff where needed.
6. The user accepts, requests changes, or rejects; after acceptance the Agent
   performs a controlled integration.
7. The published Artifact, Git revision, TaskRun, Issue, and decision remain
   traceable.
8. Observe whether the user still persistently needs a repository-independent
   page space that they can organize and maintain directly.

The evidence this journey should collect includes:

- whether users understand "proposal, version, diff, accept, and publish"
  without Git terminology;
- at which steps users ask to edit directly rather than keep instructing the
  Agent;
- which of visual preview, behavior tests, and text diff most influences the
  acceptance decision;
- how much human intervention is needed when the Agent handles conflicts and
  merges;
- whether decisions and knowledge naturally stay in the Issue/Git or quickly
  become undiscoverable;
- where existing objects fail first as project participants and parallel
  Proposals grow.

Only when this journey, or an equally complete one, proves that built-in
knowledge storage is missing should design move on to Wiki pages, versioning,
search, and an editor. Only when several sources need the same isolation and
integration contract should the versioned workspace be promoted to a shared
platform concept.

## 14. Where This Could Land If The Direction Holds

If evidence supports the "collaboration lifecycle" primary hypothesis:

1. The roadmap should schedule it after the current operational
   trustworthiness priorities, or have explicit customer evidence adjust its
   priority, rather than this memo committing to it directly.
2. The stable semantics should go into a new design record that states the
   precise relationship of Proposal, Evidence, Decision, and Integration to
   Issue, Task, TaskRun, Artifact, and Space.
3. Source adaptation, permissions, and failure behavior should each go into
   the corresponding architecture documents, so that no single abstraction
   absorbs all the differences between Git, Wikis, Figma, and APIs.
4. User-facing operating instructions go into `manual/` only after a real
   feature ships.
5. This proposal is deleted once the direction is accepted, rejected, or
   superseded by a narrower question; Git history preserves the discussion.

Until a decision is made, this memo records only one product judgment awaiting
validation: what BuildMax may need is not a bigger content tool, but a
collaboration substrate that connects intent, Agent execution, reviewable
changes, authorized decisions, shared outcomes, and long-term knowledge.
