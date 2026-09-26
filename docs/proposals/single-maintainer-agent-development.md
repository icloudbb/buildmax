# Single-Maintainer Agent Development Workflow

> **简体中文：** [阅读中文镜像](../zh-CN/proposals/single-maintainer-agent-development.md)
>
> **Audience:** maintainers, contributors, and coding-Agent workflow authors · **Status:** proposal — under discussion
>
> **Opened:** 2026-09-06

**Accepted slice:** Main-line planning now uses the in-repository
[backlog](../backlog/README.md) instead of GitHub Issues: one file per ready
task, ordered by filename prefix, with a single manual claim in its `claim`
frontmatter, its pull request in `pr`, and a derived status view from
`./make board`. An architecture test rejects malformed task frontmatter.
Externally contributable work remains in GitHub Issues under the
one-item-one-place rule. The backlog document is the current authority for that
planning and execution loop. This proposal remains open only for what is not
built: changed-scope verification (`verify changed`), automated readiness
revalidation, pull-request delivery preparation (`pr ready`), lease expiry and
overlap detection with workspace reclamation, a fresh-context verifier stage,
and graduated automatic merge. Where later text says "implementation-ready
Issue," read it as the ready work item selected for its audience: a backlog task
for maintainer-and-Agent work or a GitHub Issue for external contributors, never
both.

Related: [roadmap](../ROADMAP.md),
[current-state assessment](../current-state.md),
[testing guide](../contribute/testing.md),
[verification program](../design/verification-program.md),
[evaluation system](../design/evaluation-system.md),
[workspace root and worktrees](../design/workspace-root-and-worktrees.md),
[repository Agent guide](../../AGENTS.md), and
[this repository's BuildMax configuration](../../.buildmax/README.md).

## Contents

- [1. Question And Recommendation](#1-question-and-recommendation)
- [2. Current Evidence](#2-current-evidence)
- [3. Maintainer Outcome And Constraints](#3-maintainer-outcome-and-constraints)
- [4. Goals And Non-Goals](#4-goals-and-non-goals)
- [5. Options And Trade-Offs](#5-options-and-trade-offs)
- [6. Proposed Development Loop](#6-proposed-development-loop)
- [7. Task Readiness And Portfolio Control](#7-task-readiness-and-portfolio-control)
- [8. Agent Roles And Concurrency](#8-agent-roles-and-concurrency)
- [9. Verification And Independent Acceptance](#9-verification-and-independent-acceptance)
- [10. Candidate Repository Automation](#10-candidate-repository-automation)
- [11. Maintainer Cadence And Measures](#11-maintainer-cadence-and-measures)
- [12. Delivery Sequence](#12-delivery-sequence)
- [13. Future Operations Extension](#13-future-operations-extension)
- [14. Risks And Failure Modes](#14-risks-and-failure-modes)
- [15. Open Questions And Evidence Needed](#15-open-questions-and-evidence-needed)
- [16. Likely Destination If Accepted](#16-likely-destination-if-accepted)

## 1. Question And Recommendation

How can one BuildMax maintainer use multiple modern coding Agents to develop a
large project quickly without turning task preparation, review, conflict
resolution, and cleanup into a larger manual burden than writing the code?

The recommendation is to optimize for **maintainer attention per accepted
change**, not Agent count or generated output:

1. Keep a small, continuously validated pool of implementation-ready Issues.
2. Give every active task one lease and one primary writing Agent.
3. Encode test selection and delivery evidence in the repository task runner.
4. Separate implementation from acceptance with a fresh verifier context.
5. Let Agents carry routine work to a reviewable pull request; reserve human
   attention for priorities, real design choices, exceptions, and merge or
   release authority.
6. Convert every repeated human intervention into a task-contract rule,
   deterministic check, regression test, or Agent evaluation task.

This is a development-process proposal. Section 13 records how the same
principles could later support supervised AI operations, but operations are not
the current implementation priority.

## 2. Current Evidence

BuildMax already provides an unusually strong base for Agent-assisted
development:

- the repository task runner gives build, test, check, end-to-end, deployment,
  evaluation, and release work one cross-platform command surface;
- the [testing guide](../contribute/testing.md) maps changes to proportional
  unit, MySQL, browser, Compose, and kind evidence;
- tests isolate `BUILDMAX_HOME`, and owned end-to-end environments avoid
  colliding with a maintainer's persistent state;
- pull-request CI covers Go, frontend, open-source policy, real MySQL behavior,
  and the health of the latest deployment smoke on `main`;
- scheduled and post-merge deployment verification exercises Compose, kind,
  worker execution, managed inference, and Portal browser journeys;
- the Issue template asks for goal, scope, code areas, acceptance,
  verification, and constraints;
- `agent-ready` is defined as an Issue property rather than a claim about the
  contributor using it;
- the root Agent guide records product principles, architecture boundaries,
  runtime invariants, verification expectations, and change rules;
- the evaluation harness can measure built CLI and worker subjects and retain
  failure bundles.

The remaining bottleneck is coordination and evidence closure:

- At the review on 2026-09-06, the repository had ten open Issues and only
  three carrying `agent-ready`. Much near-term work remained embedded in the
  roadmap and design records rather than available as an implementation queue.
- Issue [#55](https://github.com/icloudbb/buildmax/issues/55) was labelled
  `agent-ready` while still naming the old `cmd/mk` location after the task
  runner had moved to `tools/mk`. Readiness can therefore become stale without
  losing its label.
- Pull requests [#401](https://github.com/icloudbb/buildmax/pull/401) and
  [#402](https://github.com/icloudbb/buildmax/pull/402) independently
  designed Task workspace continuity. Both records briefly reached `main`
  before [#404](https://github.com/icloudbb/buildmax/pull/404) reconciled
  them into one accepted direction and deleted the losing proposal. The
  resolution was correct, but parallel production still created avoidable
  arbitration and cleanup work for the maintainer.
- The [verification program](../design/verification-program.md) asks for a
  fresh acceptance pass and a structured behavior review block, but neither is
  yet a normal executable stage of every behavior-changing contribution.
- The testing matrix is precise human-readable guidance. An Agent must still
  interpret it, choose commands, and explain omitted evidence independently on
  every change.
- The product-owned evaluation suite proves its architecture with three tasks;
  it does not yet measure the common ways repository-maintenance Agents require
  human rescue.

The evidence argues against spending the next increment on more prose or more
concurrent implementers. BuildMax first needs a thin control loop around the
automation it already has.

## 3. Maintainer Outcome And Constraints

The essential outcome is:

> The maintainer decides what matters and resolves genuine ambiguity. Agents
> perform the routine investigation, implementation, verification, delivery
> preparation, and cleanup needed to turn that decision into a reviewable
> change.

A successful workflow should let the maintainer review five things rather than
reconstruct a whole Agent session:

1. the requested user outcome;
2. the important decision or assumption;
3. the meaningful part of the diff;
4. the independent acceptance verdict;
5. the verification evidence and remaining gap.

The constraints today are:

- one person owns product direction, architecture arbitration, merge policy,
  release decisions, and incident accountability;
- the repository spans Go, React, persistence, model protocols, local
  interfaces, Server and Portal behavior, workers, and deployment boundaries;
- some checks are fast and hermetic, while kind, real providers, and external
  qualification have machine, time, credential, or cost effects;
- unrelated parallel work must survive in shared checkouts;
- current Alpha policy prefers coherent breaking corrections over compatibility
  layers;
- generated code, tests, and documents still need evidence independent of the
  context that produced them.

## 4. Goals And Non-Goals

### Goals

- Reduce maintainer interventions between a ready Issue and a reviewable pull
  request.
- Keep enough ready work available that an Agent does not wait for the
  maintainer to restate context.
- Detect stale or contradictory tasks before implementation starts.
- Prevent two writing Agents from unknowingly owning the same task or design
  decision.
- Make proportional verification reproducible and machine-readable.
- Give acceptance a context independent from implementation.
- Bound concurrent work so integration remains cheaper than the work it saves.
- Make worktree, branch, temporary environment, and task-lease cleanup part of
  completion.
- Measure the workflow by maintainer attention and accepted outcomes.

### Non-Goals

- Adding a second planning system beside the backlog, GitHub Issues for
  external contributions, pull requests, and the repository task runner.
- Creating a human-company simulation with many permanent Agent titles.
- Allowing an Agent to resolve an actual product or security trade-off by
  silently choosing one option.
- Requiring every small fix to gain a proposal or design record.
- Running every expensive or stateful suite for every change.
- Automatically merging architecture, security, persistence, deployment, or
  other high-impact changes in the first slice.
- Treating volume of commits, tokens, or parallel Agents as productivity.
- Building autonomous production operations before the development loop proves
  the same task, evidence, authority, and recovery disciplines locally.

## 5. Options And Trade-Offs

### Option A: Continue With Ad Hoc Agent Sessions

The maintainer opens tasks when needed, gives each Agent context, reviews its
result, and decides which checks or follow-ups remain.

This has no new machinery and works for one task at a time. It does not scale
because task preparation, repeated repository explanation, test selection,
stale work detection, and cleanup remain human work. Adding more Agents makes
those costs concurrent.

### Option B: Maximize Parallel Agent Count

Give many Agents broad objectives, let each create worktrees or pull requests,
and reconcile their output later.

This increases speculative output but not necessarily accepted throughput.
Overlapping designs, adjacent edits, duplicated investigation, inconsistent
verification, and large review queues consume the one resource that cannot be
parallelized: the maintainer's attention.

### Option C: Build A Bounded Contribution Loop — Recommended

Keep backlog tasks (GitHub Issues for externally contributable work) and pull
requests as the work and integration records. Add a small amount of executable policy around readiness, leases, changed-scope
verification, independent acceptance, and delivery preparation.

Agents remain replaceable workers. Roles are workflow phases, not new product
entities. Most work stops at an evidence-complete pull request until the
workflow has earned broader authority through measured results.

This option adds some tooling, but each proposed mechanism removes a repeated
human decision rather than creating a parallel abstraction.

## 6. Proposed Development Loop

The normal contribution path becomes:

```text
roadmap, defect, feedback, or test gap
                 |
                 v
       implementation-ready Issue
                 |
        lease + overlap check
                 |
                 v
       isolated implementation
                 |
       changed-scope verification
                 |
                 v
        fresh acceptance pass
                 |
      repair, decision, or delivery
                 |
                 v
     evidence-complete pull request
                 |
       CI repair within a bound
                 |
                 v
         maintainer merge decision
                 |
     lease and workspace reclamation
```

The loop has three human-facing queues.

### Decision Needed

This queue contains product direction, architecture boundaries, security and
data choices, or alternatives whose trade-offs can materially change the
result. An Agent may investigate and recommend, but implementation waits for
one recorded decision.

A decision request should normally contain only:

- user outcome;
- current evidence;
- current constraints;
- recommended option;
- one meaningful alternative;
- the exact decision requested from the maintainer.

Large speculative design documents should not be the default. The maintainer
should be able to decide before reviewing hundreds of lines that assume a
direction.

### Ready For Agent

This queue is the [backlog](../backlog/README.md). It keeps a short ready
horizon — enough unblocked tasks for the next few Agent sessions — whose
direction, scope, acceptance, and verification are clear. A planning Agent may
draft or refresh these tasks, but the maintainer's priority decides which enter
the queue and in what order.

### Ready For Review

The Agent delivers a pull request with an independent verdict and evidence, not
a conversation ending with a list of work the maintainer must still perform.
The maintainer reviews the outcome, decision, critical diff, evidence, and
remaining risk.

## 7. Task Readiness And Portfolio Control

An implementation-ready Issue should have:

| Field | Purpose |
|---|---|
| Stable work ID | Correlate Issue, lease, branch, worktree, pull request, and evidence |
| Observable outcome | Define what becomes true for a user, operator, or contributor |
| Motivation | Explain why this work matters now |
| In scope | Bound the required behavior and surfaces |
| Out of scope | Stop adjacent cleanup from expanding the change |
| Current source anchors | Point to live code, tests, and current documentation |
| Accepted decision | Remove unresolved product or architecture choices |
| Acceptance criteria | State checkable positive, negative, and failure outcomes |
| Verification | Name the narrow starting checks and special environment needs |
| Effect profile | Declare filesystem, Docker, network, provider, deployment, and external-system effects |
| Budget | Bound time, Agent turns, expensive trials, and automated repair attempts |

Readiness is not permanent. The backlog already drops a stale task from the
queue until it is refreshed, but that refresh is manual. A scheduled check
should re-evaluate every ready task when its referenced files, commands, design
decision, dependency, or base branch changes.

The backlog `claim` field implements the single manual claim: at most one live
claim per task, set before work starts and cleared if the work is abandoned,
with `pr` marking a claim that has reached review and deletion of the task file
on merge releasing it. What remains is detection: expiring a claim whose Agent
disappeared, and checking open pull requests, active branches, worktrees, and
other tasks for the same stable work ID or decision key before a claim is taken.

The first version does not need semantic conflict prediction. Stable IDs,
explicit affected areas, exact branch relationships, and a conservative
same-capability write limit remove the common conflicts without introducing a
new planning engine.

## 8. Agent Roles And Concurrency

Roles are short-lived execution profiles:

| Role | Responsibility | Default authority |
|---|---|---|
| Planner | Turn current roadmap items, defects, and verification gaps into Issue drafts; refresh stale tasks | Read repository and GitHub state; draft only |
| Investigator | Reproduce, trace, or compare alternatives before a decision or implementation | Read-only unless the task explicitly requests a disposable reproduction |
| Implementer | Complete one leased Issue in one isolated checkout | Write the scoped repository; run authorized local checks |
| Verifier | Try to falsify the claimed behavior from the Issue, public interfaces, and diff | Read-only by default; may add a focused acceptance test when authorized |
| Integrator | Detect overlap, order related changes, inspect CI, and prepare cleanup | Branch and pull-request coordination; no authority to redefine the outcome |

One task usually needs an Implementer and a Verifier. Additional Agents are
justified by independent work, not by task size alone.

An initial concurrency policy for one maintainer is:

- no more than two or three active writing tasks;
- no more than one writing task in the same capability at a time;
- investigation of the next task may run beside implementation;
- acceptance of the previous task may run beside both;
- one accepted direction per design decision;
- stateful deployment suites are serialized unless their commands own isolated
  environments;
- a red shared boundary pauses new changes to that boundary until it is
  classified.

This yields useful parallelism without making the maintainer the merge queue.

## 9. Verification And Independent Acceptance

### Changed-Scope Verification

The [testing guide](../contribute/testing.md) should remain the explanatory
source. The task runner should encode its current path-to-evidence decisions so
two Agents do not independently reinterpret the same matrix.

Given a base revision and the current diff, changed-scope verification should
report:

- which checks are required and why;
- which checks ran and their exact outcomes;
- which checks did not run and why;
- which prerequisite or authorization is missing;
- retained logs, screenshots, traces, and other failure artifacts;
- the commit and dirty state against which the evidence was produced.

It must not claim that a skipped real dependency passed. Expensive provider
qualification and external deployment rehearsals remain explicit evidence
classes rather than hidden side effects of a generic check.

### Fresh Acceptance

The verifier receives the original Issue, relevant public interfaces, the diff,
and produced evidence. It does not receive the implementer's private reasoning
as its starting explanation.

Its result follows the behavior block already selected by the
[verification program](../design/verification-program.md):

```text
Behavior:
Observable result:
Persistent result:
Forbidden side effects:
Failure cases:
Tests added:
Not tested:
Verdict:
```

The verifier checks for omitted surfaces, implementation-coupled assertions,
unsupported documentation claims, weakened tests, unhandled failure paths, and
work outside the Issue. Authorization, state-machine, sandbox, migration, and
other high-risk changes include a meaningful negative or mutation check.

The verdict is one of:

- `accept`: the evidence supports the Issue's acceptance criteria;
- `reject`: a concrete defect or missing proof must return to implementation;
- `needs-decision`: the remaining question changes product intent or authorized
  scope and therefore belongs to the maintainer.

### Bounded Repair

An Agent may repair deterministic test or CI failures within the Issue's scope.
The workflow limits automatic repair attempts. Repeated failure, a changed
design assumption, an environmental incident, or a proposed weakening of the
oracle leaves the loop and requests classification rather than consuming an
unbounded number of turns.

## 10. Candidate Repository Automation

The following are candidate task-runner capabilities, not implemented command
contracts.

### Changed Verification

A `verify changed` capability would derive proportional checks from a base
revision and emit a machine-readable evidence summary. It is the highest-value
first addition because it removes a decision repeated on every change and
makes omissions visible to both the verifier and maintainer.

### Issue Readiness Check

An `issue check` capability would validate required sections, referenced live
paths, known task-runner commands, decision status, dependencies, effect
profile, acceptance criteria, and overlap with active work. GitHub automation
would apply or remove `agent-ready` from that result rather than treating the
label as a permanent manual assertion.

### Pull-Request Delivery Check

A `pr ready` capability would:

- inspect the diff for unrelated work;
- invoke changed-scope verification;
- check documentation and changelog obligations;
- record untested behavior without converting it to success;
- prepare the pull-request summary from the real diff and evidence;
- link the Issue, lease, acceptance verdict, and artifacts;
- verify that no temporary state is mistaken for a committed artifact.

### Lease And Workspace Reclamation

The coordination helper would create or associate one task lease with its
branch and worktree, report stale owners, and propose cleanup after merge or
abandonment. Destructive cleanup still resolves exact targets and follows the
repository's safety rules.

### Maintainer Digest

A scheduled read-only report would identify:

- roadmap work not represented by ready Issues;
- ready Issues whose references or dependencies became stale;
- overlapping design directions or active changes;
- merged or abandoned worktrees and branches eligible for cleanup;
- failing, skipped, or flaky verification;
- documentation drift;
- evaluation gaps and recurring human interventions.

The digest proposes changes. It does not reprioritize the roadmap or delete
state by itself.

## 11. Maintainer Cadence And Measures

The workflow should make maintainer attention predictable.

### Daily Or Per Work Session

At the beginning of a work session, the maintainer:

1. reviews only new decision requests and Planner drafts;
2. selects the next two or three ready tasks;
3. records the one assumption that would otherwise cause divergence.

At the end, the maintainer:

1. reviews `needs-decision` or rejected acceptance outcomes;
2. reads evidence-complete pull requests;
3. merges, redirects, or closes work;
4. classifies any intervention that should become a reusable rule or test.

### Weekly

The Agent-prepared digest lets the maintainer refresh the ready queue, resolve
one or two decision bottlenecks, inspect persistent verification failures, and
approve exact cleanup targets.

### Measures

Track:

- maintainer interventions per ready Issue;
- maintainer review minutes per accepted change;
- time from lease to reviewable pull request;
- first-pass CI rate;
- independent-acceptance rejection rate and cause;
- reopened or reverted changes;
- stale ready Issues;
- duplicate or conflicting active work;
- automatic repair attempts per accepted change;
- Agent evaluation pass rate for repository-maintenance tasks;
- model cost per accepted, rejected, and abandoned change.

Agent count, generated lines, pull-request count, and tokens spent are capacity
signals, not success measures.

## 12. Delivery Sequence

### Phase 0: Process Without New Product Code

- Keep the backlog's short ready horizon genuinely ready (in use).
- Use one stable work ID and one primary writer per task (the task's `id` and
  `claim`; in use).
- Limit active writing tasks to two or three.
- Require a fresh acceptance pass for behavior changes.
- Shorten decision requests and stop parallel implementation at unresolved
  product choices.
- Record why the maintainer intervened.

This phase tests whether the proposed control points reduce review burden
before automating them.

### Phase 1: Compile Verification

- Encode changed-path verification selection in the task runner.
- Emit a bounded evidence summary with omissions and prerequisites.
- Reuse the existing commands and suites rather than wrapping them in a second
  test framework.
- Add tests showing that relevant path mutations select the expected evidence.

### Phase 2: Compile Readiness And Delivery

- Add readiness validation beyond the frontmatter shape the architecture test
  already enforces, and periodic revalidation.
- Add claim expiry and conservative overlap checks on top of the backlog
  `claim`.
- Add pull-request delivery preparation.
- Reclaim merged and abandoned workspaces through exact, reviewable targets.

### Phase 3: Measure Independent Acceptance

- Run the verifier in a fresh context.
- Add repository-maintenance evaluation tasks from real interventions.
- Compare accepted changes, escaped defects, review time, and Agent cost against
  the pre-change baseline.
- Permit bounded automatic CI repair only after the failure classifications are
  useful.

### Phase 4: Expand Authority From Evidence

Low-risk documentation, test, and mechanical maintenance may move from
evidence-complete pull request to automatic merge only after a sustained sample
shows low escape and revert rates. Architecture, security, persistence,
deployment, and product decisions retain maintainer review.

## 13. Future Operations Extension

The development loop and a future supervised operations loop should share four
properties: a typed task, bounded authority, independent success evidence, and
an auditable result. That does not make AI operations the next development
priority.

If BuildMax later adopts automated operations, the safe progression is:

1. read-only health and incident evidence collection;
2. remediation proposals with explicit preconditions and rollback;
3. execution in disposable or staging environments;
4. production execution only for pre-approved, reversible runbooks with a
   bounded blast radius and automatic verification;
5. human authorization for destructive, identity, credential-root, data
   restore, broad network, and material spending decisions.

The Agent should call narrow operational outcomes such as drain, inspect,
canary, or rollback rather than receive unrestricted cluster and database
authority. Deterministic code owns authorization, idempotency, budgets,
preconditions, rollback, and audit.

The first useful future vertical slice is to turn the manual
[Beta readiness record](../deploy/beta-readiness.md) into repeatable evidence
collection for a pinned candidate. It already names the operator journey,
failure drills, restore, upgrade, rollback, credential rotation, and evidence
that such a workflow must preserve.

This section is retained only so development automation does not choose a shape
that cannot later extend to operations. It does not place operations ahead of
the current roadmap.

## 14. Risks And Failure Modes

| Risk | Consequence | Mitigation |
|---|---|---|
| Too many writing Agents | Conflicts and review queues erase parallel gains | Start with two or three writers and one writer per capability |
| Stale ready work | Agent implements yesterday's architecture correctly | Revalidate paths, commands, dependencies, and decision status |
| Speculative design volume | Maintainer reads more than the decision is worth | Require a compact decision request before a large record |
| Self-validating implementation | Tests prove the author's assumptions rather than behavior | Fresh verifier context and public outcome oracles |
| Broad automated repair | Agent changes requirements or weakens tests to turn CI green | Bound retries and reject oracle weakening |
| Hidden expensive checks | Routine work mutates infrastructure or spends provider quota | Effect profiles and explicit evidence classes |
| Duplicate planning systems | Backlog, roadmap, Agent state, and local notes drift | Keep roadmap, backlog task, pull request, and code as their existing authorities |
| Permanent specialist hierarchy | More configuration and stale prompts than useful work | Treat roles as short-lived phases |
| Automatic merge too early | Fast defects reach `main` and consume more recovery time | Earn authority by risk class from measured results |
| Metrics reward volume | Agents optimize commits or lines rather than accepted outcomes | Measure maintainer attention, acceptance, escapes, and cost |

## 15. Open Questions And Evidence Needed

1. What active-writing limit minimizes elapsed time without increasing merge
   conflicts: two, three, or a capability-specific value?
2. Is the backlog task's frontmatter plus its required sections sufficient
   for automated readiness checks, or does readiness need more machine-readable
   fields?
3. Which path-to-verification decisions are stable enough to encode now, and
   which still require judgment from the testing guide?
4. Should the verifier be strictly read-only, or may it commit an acceptance
   test to the same pull request under a separately attributed step?
5. Which failure classes are safe for automatic repair, and what attempt bound
   prevents loops without stopping useful recovery?
6. What diff, risk, or subsystem threshold should require a maintainer-approved
   design decision before implementation?
7. Which low-risk change class, if any, should be the first automatic-merge
   experiment?
8. The claim now lives in the checked-in backlog frontmatter. Does expiry and
   cleanup state belong there too, or only in Git and pull-request state?

Evidence should come from at least twenty ready Issues across more than one
subsystem. Record task age, interventions, conflicts, acceptance outcomes, CI
repairs, review time, escaped defects, and model cost. Compare the bounded loop
with recent ad hoc Agent-assisted work before adding more orchestration.

The direction should be rejected or narrowed if task preparation and control
metadata consume more maintainer time than they save, or if independent
acceptance does not reduce review time or escaped defects.

## 16. Likely Destination If Accepted

The stable contributor workflow and command behavior would move into
[contributor documentation](../contribute/README.md) and the task runner's own
help. Verification rationale and accepted evidence policy would update the
[verification program](../design/verification-program.md). Agent-facing
invariants would enter the root Agent guide only when they apply to every task;
details would remain in executable commands and scoped documentation.

Accepted main-line implementation work now becomes focused backlog tasks;
externally contributable work becomes focused GitHub Issues, never a mirror in
both places. The remaining automation would move into contributor documentation
and task-runner help only if maintainer-efficiency evidence justifies it. Once
those open decisions are accepted or rejected, this proposal is deleted and git
history preserves the discussion.
