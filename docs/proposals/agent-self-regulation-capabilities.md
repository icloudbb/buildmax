# Agent Self-Regulation Capabilities

> **简体中文：** [阅读中文镜像](../zh-CN/proposals/agent-self-regulation-capabilities.md)
>
> **Audience:** contributors and product reviewers · **Status:** proposal — under discussion
>
> **Opened:** 2026-09-13

Related: [roadmap](../ROADMAP.md), [current state](../current-state.md),
[product vision](../design/product-vision.md),
[Agent execution and Task threads](../design/agent-execution-and-task-threads.md),
[context durability](../design/context-durability.md),
[tool permissions](../design/tool-permissions.md),
[durable run traces](../design/durable-run-trace.md),
[local Project Memory](../design/local-project-memory.md).

## Contents

- [1. Purpose And Recommendation](#1-purpose-and-recommendation)
- [2. Essential User Outcome](#2-essential-user-outcome)
- [3. What Self-Regulation Means](#3-what-self-regulation-means)
- [4. Current BuildMax Foundations](#4-current-buildmax-foundations)
- [5. Capability Classes](#5-capability-classes)
- [6. Runtime And Agent Authority](#6-runtime-and-agent-authority)
- [7. Product And Tool Shape](#7-product-and-tool-shape)
- [8. Priority And Candidate Workstreams](#8-priority-and-candidate-workstreams)
- [9. Options And Trade-Offs](#9-options-and-trade-offs)
- [10. Risks And Failure Modes](#10-risks-and-failure-modes)
- [11. Evidence Needed](#11-evidence-needed)
- [12. Goals And Non-Goals](#12-goals-and-non-goals)
- [13. Open Questions](#13-open-questions)
- [14. Likely Destination](#14-likely-destination)

## 1. Purpose And Recommendation

Two candidate Agent capabilities motivate this proposal:

- **runtime introspection** — an Agent can observe context occupancy, elapsed
  time, remaining iterations, compaction state, and its existing Todo state;
- **capability discovery** — an Agent can search the tools available to this
  run instead of receiving every tool schema up front or guessing that a
  capability does not exist.

Both are forms of self-awareness, but neither alone creates a self-regulating
Agent. An Agent can know that it is low on context and still pursue the wrong
goal. It can discover the perfect tool and still lack permission, misunderstand
its effects, or declare completion without evidence.

The provisional recommendation is not to create one general `Reflect` tool or
a new self-management entity. BuildMax should instead evaluate a small family
of typed meta-capabilities that let an Agent answer ten concrete questions:

1. What outcome am I responsible for?
2. What is proven, assumed, contradicted, or unknown?
3. What resources and limits remain?
4. What capabilities are available?
5. Which actions am I authorized to take?
6. What has this run changed?
7. Is the result actually verified against the outcome?
8. What survives interruption, compaction, or migration?
9. What delegated work is outstanding?
10. What knowledge should persist, be revalidated, or be forgotten?

Each answer should come from the component that owns the fact. The Agent reads
bounded projections and proposes actions; deterministic runtime code enforces
limits, commits transitions, records effects, and decides what is durable.

This proposal frames a capability family and candidate priorities; it does not
approve a platform layer or claim that any proposed interface has shipped.

## 2. Essential User Outcome

The user outcome is:

> An Agent can notice when its current approach is unlikely to satisfy the
> requested outcome, discover a safer or more capable next action, and prove
> completion or ask for help before exhausting its authority and resources.

This is more specific than “the model reflects.” Reflection expressed only as
another prose prompt is not observable, cannot be distinguished from
narration, and often repeats the same unsupported judgment. The useful form of
self-regulation closes a loop over external facts:

```text
authoritative objective and constraints
                  |
                  v
        observe state and evidence
                  |
                  v
      identify gap or uncertainty
                  |
                  v
   discover capability and authority
                  |
                  v
       preview or perform action
                  |
                  v
   inspect effects and verify outcome
          |                 |
       continue       finish / escalate
```

A stronger model improves decisions inside this loop. It does not replace the
loop's authoritative inputs, permission checks, durable journal, or acceptance
evidence.

## 3. What Self-Regulation Means

Self-regulation is the ability to adapt work using observable state while
remaining inside system-owned boundaries. It is not unrestricted autonomy.

Four distinctions are load-bearing.

### 3.1 Observation is not authority

Knowing an iteration cap does not grant permission to raise it. Seeing that a
tool exists does not activate its plugin. Knowing that an operation was denied
does not authorize another transport that reaches the same effect.

### 3.2 A plan is not the objective

The user or owning product object supplies the objective and constraints. A
Todo list is an Agent-authored plan that may be revised or discarded. Treating
the plan as the goal lets the Agent declare success by completing steps it
invented while missing the requested outcome.

### 3.3 A trace is not evidence of correctness

A trace proves that calls and transitions were recorded. It does not prove that
the observed result supports a claim, that a test covers the relevant boundary,
or that acceptance criteria were satisfied.

### 3.4 Self-regulation is not hidden reasoning disclosure

BuildMax does not need private chain-of-thought to make Agent behavior
inspectable. It needs bounded externalized claims: objective, hypothesis,
evidence reference, effect, verification result, unresolved question, and
requested action. These are useful to the Agent, user, evaluator, and recovery
path without retaining unrestricted reasoning text.

## 4. Current BuildMax Foundations

BuildMax already implements much of the deterministic substrate. The gaps are
mostly model-facing projections and contracts between existing facts.

| Capability area | Existing foundation | Important gap |
|---|---|---|
| Objective and plan | Task objective, prompts, durable notes and Todos | no explicit objective-to-acceptance-to-evidence completion contract |
| Runtime status | Agent events, `RunStats`, usage views, context estimation, automatic/manual compaction | no bounded LLM-facing status view |
| Capability discovery | assembled tool registry, Skills, subagents, plugins, `LoadMcpTools` / `CallMcpTool` | no general search or progressive disclosure across already resolved tools |
| Evidence | tool results, durable run trace, notes, Artifacts | no bounded relation between a claim, its evidence, and contradictory evidence |
| Authority | tool policy, approvals, hooks, sandbox, Space and run scoping | the Agent cannot inspect a safe explanation of available versus approval-bound capability before acting |
| Effects | tool events, worktrees, Task workspace checkpoints, external tool results | no unified view of attempted, confirmed, reversible, and unresolved effects |
| Verification | tests and domain-specific tools can be called | no completion protocol that binds a claim to acceptance evidence |
| Continuity | Sessions, compaction summaries, state checkpoints, Task/TaskRun, workspace checkpoints, queued input | the Agent has no compact recovery-readiness view of what is durable and what remains transient |
| Delegation | subagents, background Jobs, Monitor, durable Task execution | no single bounded parent view of outstanding work, overlap, budget, and synthesis readiness |
| Learning | Session notes and Project Memory with bounds and stale-write protection | limited provenance, revalidation, scope, and intentional forgetting semantics |

The existence of a foundation does not prove that exposing it to the model is
valuable. It does mean new proposals should extend the current owner instead of
creating parallel state.

## 5. Capability Classes

### 5.1 Objective and acceptance awareness

An Agent needs an authoritative, bounded answer to:

- what outcome the current run or Task owes;
- which constraints are invariant;
- which acceptance criteria are required;
- which criteria are verified, failed, or unverified; and
- whether later user input revised the contract.

This is not a second prompt and not an Agent-owned Todo list. It is a projection
of the request and the owning Task, Issue, Workflow step, or Session turn. The
Agent may propose a decomposition; it may not silently weaken acceptance.

The most common failure this addresses is productive work on the wrong problem
followed by a completion claim measured against the Agent's own plan.

### 5.2 Runtime and budget awareness

The Agent should be able to inspect the current Run's elapsed time, iteration
budget, current context occupancy, compaction state, model/tool call counts, and
relevant deadline. It must distinguish current context occupancy from
cumulative prompt tokens and mark estimates as estimates.

This enables phase changes such as stopping exploration, checkpointing a
decision, compacting before synthesis, or returning a partial result before a
hard limit. Automatic enforcement remains runtime-owned.

Two candidate tools make the boundary concrete without committing their names
or schemas as API.

#### Read-only runtime status

A read-only `RuntimeStatus` tool would return a small summary by default. An
optional task-detail mode may return the existing bounded Todo state; it must
not query or relabel durable Server Tasks.

```json
{
  "snapshot": {
    "run_elapsed_ms": 183420,
    "iteration": 12,
    "max_iterations": 50,
    "remaining_iterations": 38,
    "model_calls": 12,
    "tool_calls": 27
  },
  "context": {
    "tokens": 81200,
    "window": 128000,
    "utilization_percent": 63,
    "count_kind": "estimated",
    "measured_at_iteration": 12,
    "automatic_compaction_percent": 80,
    "compactions": 1,
    "compaction_available": true
  },
  "todos": {
    "pending": 3,
    "in_progress": 1,
    "completed": 7
  }
}
```

The exact JSON remains a design decision. The proposal requires the following
semantics:

- return integer source values beside, not instead of, rounded percentages;
- label token counts `estimated`, `provider_reported`, or `unavailable`;
- name the iteration at which a context snapshot was measured;
- keep Run and Session totals separate;
- report unavailable facts as unavailable rather than zero; and
- label Todo detail as Todo state, not as processed Tasks or TaskRuns.

The result is written for the model and may add one bounded actionable sentence
when a hard limit is close. It should not prescribe a strategy when the runtime
has no evidence that the threshold matters to the current task.

The latest prepared request is necessarily a snapshot: the model has already
received it before calling the status tool, and the tool's own result will make
the next request larger. A future next-request projection is useful only if it
can avoid recursively estimating its own output.

#### Honest context accounting

One authoritative request estimator should feed Agent events,
`AgentApp.EstimateRunUsage`, interactive status, traces, and runtime
introspection. It should count every request component BuildMax can observe:

- effective system prompt and compaction block;
- model-visible history after safe trimming;
- resident memory index and Session state;
- visible tool definitions and their schemas; and
- provider framing overhead represented by the estimator.

Provider-reported usage from the last call is accounting evidence, but may
include provider-specific caching or hidden framing. It must not silently
replace a differently scoped estimate. A run may cumulatively send 500,000
prompt tokens while its current request occupies 60,000; calling the former
“context used” is a correctness defect.

#### Boundary-scheduled context control

A separate `ContextCompact` call would request compaction but never rewrite
history inside `Tool.Execute`. At that moment an assistant/tool group may still
be executing or waiting to commit, including parallel siblings. Mutating the
boundary there could split a required message pair, race another tool, or make
durable and in-memory history disagree.

The safe candidate flow is:

```text
Agent requests ContextCompact
  -> tool records one pending request and returns
  -> the whole tool-call batch commits in order
  -> the Agent Loop reaches the post-batch boundary
  -> requested compaction runs through the existing compactOnce path
  -> queued user input is injected
  -> the next request uses the committed compacted view
```

Queued input comes after requested compaction so a message that arrived during
the previous batch remains a fresh instruction rather than being summarized
before the Agent sees it. The current assistant/tool group remains an atomic
tail even when it exceeds the normal reserve. A pending request satisfied by an
automatic compaction does not cause a second pass.

The call takes no target token count, arbitrary message range, replacement
summary, or `force` flag. It reports scheduled, already pending, unavailable,
or nothing useful to summarize. Actual compaction reuses the existing
checkpoint, `PreCompact` and `PostCompact` hooks, metering, summary clamp,
event, and durable commit semantics. A failed summary leaves history intact;
failure to persist a produced boundary remains fatal because continuing would
make durable and in-memory views disagree.

Automatic compaction remains authoritative at the existing threshold. An
Agent-requested pass is an optimization hint whose value and information loss
must be measured separately.

### 5.3 Capability discovery and progressive tool disclosure

The Agent should be able to search the capabilities already resolved for its
run by intent, inspect a small result, load the exact schema it needs, and then
call the selected tool through its normal name and permission path.

The candidate lifecycle is:

```text
resolved registry: every capability this run may know exists
        |
        +-- always-visible kernel tools
        |
        +-- ToolSearch(query) -> bounded matches
                                  |
                                  v
                         ToolLoad(selected names)
                                  |
                                  v
                 next request's visible tool registry
```

Search does not install a plugin, activate a Space integration, connect an
account, or widen permissions. It discovers only the run's already resolved
capability set.

BuildMax's MCP gateway is an early form of progressive disclosure: the model
receives gateway tools and loads one MCP tool's full schema on demand. A general
design must decide whether to extend that dispatch pattern or promote selected
tools into the direct LLM tool list. Direct promotion provisionally looks
safer for argument quality and for preserving existing tool names, hooks, and
permission semantics; it needs per-run visible-registry state rather than a
mutation of the shared cached registry.

### 5.4 Epistemic and evidence awareness

The Agent should distinguish observed facts, hypotheses, inferences,
contradictions, and unknowns when the distinction materially affects future
work. A candidate bounded record is:

```json
{
  "claim": "the failure is caused by transaction contention",
  "status": "hypothesis",
  "evidence": ["trace:abc#event-27"],
  "contradictions": [],
  "next_check": "reproduce against real MySQL"
}
```

This is not a universal knowledge graph. It is a small working set of claims
that would otherwise be incorrectly promoted to facts or forgotten across
compaction. Evidence references should point to existing trace events,
Artifacts, files, test results, or external records rather than copying their
full contents into another store.

### 5.5 Authority awareness

Capability discovery must be paired with a safe explanation of authority:

- available without approval;
- available with approval;
- denied by policy;
- unavailable on this surface; or
- argument-dependent and not knowable until a concrete call is proposed.

This view must not reveal neighboring resources or hidden plugins. It helps the
Agent select a less privileged path and stops repeated calls whose refusal is a
stable boundary. A denial remains authoritative; the Agent cannot reinterpret
an explanation as a grant.

### 5.6 Effect and reversibility awareness

Before and after consequential actions, the Agent should know what it intends
to change, what the runtime observed, whether the effect is confirmed, and
whether it is reversible or safely retryable.

```json
{
  "kind": "external_comment",
  "target": "current_issue",
  "status": "confirmed",
  "reversible": false,
  "idempotency": "unknown"
}
```

An effect ledger should be derived from authoritative tool boundaries and
domain transitions, not from Agent narration. It need not make unrelated
effects look transactionally atomic. Its value is to prevent duplicate side
effects, distinguish changed files from published state, and make unresolved
outcomes visible before a retry or completion claim.

### 5.7 Verification and completion awareness

The Agent should not equate an implementation action with the requested
outcome:

```text
file written              != behavior works
command exited zero       != relevant boundary was tested
HTTP returned success     != durable state is correct
artifact was generated    != a person can use it
all Agent-authored Todos done != user acceptance satisfied
```

A completion protocol should require the Agent to bind each authoritative
acceptance criterion to evidence or explicitly mark it unverified. Depending
on risk, deterministic checks or an independent verifier may challenge the
claim. The system should permit a truthful partial result; it should not force
the Agent to manufacture certainty to reach a terminal state.

This is likely the highest-value missing contract. It closes the loop that
runtime introspection and Tool Search only help execute.

### 5.8 Continuity and recovery awareness

An Agent should know what will survive compaction, cancellation, worker loss,
or resume:

- objective and constraints;
- committed notes and Todos;
- compaction boundary and summary;
- durable workspace checkpoint;
- confirmed external effects; and
- transient facts still present only in the active context.

This does not require another checkpoint type. It requires a bounded readiness
projection over the Session, TaskRun, and workspace mechanisms that already own
durability. A recovery path should resume from committed facts and identify
uncertain side effects before retrying them.

### 5.9 Delegation and coordination awareness

A parent Agent needs a bounded view of child work: objective, state, remaining
budget, result availability, overlap, and whether synthesis can begin. It
should not receive every child's complete context by default.

Without this view, multi-Agent execution multiplies uncertainty: parents wait
for completed children, repeat delegated work, or synthesize before a critical
result arrives. The runtime owns child lifecycle and cancellation; the parent
chooses semantic decomposition and synthesis inside configured limits.

### 5.10 Learning, revalidation, and forgetting

Durable memory should carry provenance, scope, and a reason to remain useful.
An Agent needs to know when a remembered fact is stale, contradicted, or outside
the current Project. It should be able to propose replacement or deletion,
subject to the current store's concurrency and size rules.

More memory is not automatically more intelligence. Unbounded accumulation
makes outdated conclusions permanently salient. Intentional forgetting and
revalidation are capabilities, not cleanup after the “real” memory system.

## 6. Runtime And Agent Authority

The family follows one ownership rule:

> Runtime facts and transitions stay with their existing deterministic owner.
> The Agent receives bounded projections, makes semantic judgments, and submits
> requests that the owner may validate, refuse, commit, and record.

| Subject | Agent may | Agent may not |
|---|---|---|
| Objective | restate, decompose, flag ambiguity | silently rewrite user constraints or acceptance |
| Runtime budget | inspect and adapt strategy | increase limits or falsify accounting |
| Tools | search and request visibility | install, activate, or self-grant capability |
| Evidence | associate a claim with references | turn an inference into an observed fact |
| Permissions | inspect bounded availability and request approval | bypass a denial through another transport |
| Effects | preview, perform through tools, verify | declare an uncertain external effect committed |
| Completion | submit a claim with evidence | define success solely as completion of its own Todos |
| Recovery | checkpoint through supported mechanisms | invent a durable state the store did not commit |
| Delegation | propose work and synthesize results | exceed roster, budget, depth, or authority limits |
| Memory | read, propose write/delete, revalidate | accumulate unbounded or cross-scope knowledge |

This separation lets model capability improve without making a more capable
model the transaction log, authorization service, quota ledger, or recovery
coordinator.

## 7. Product And Tool Shape

These capability classes should not become ten permanent tools. The correct
surface depends on how often the Agent needs the information and whether the
operation changes state.

| Shape | Use when | Candidate examples |
|---|---|---|
| Bounded resident projection | every next decision is unsafe without it | active objective, hard constraints, current Todo |
| On-demand read tool | detail is occasionally useful and costs context | runtime status, Tool Search, evidence references, effect status |
| Separate control request | action is lossy, billable, or state-changing | compact context, load tool schema, request approval, checkpoint |
| Deterministic completion gate | correctness cannot rest on model discipline | acceptance-to-evidence check, unresolved-effect check |
| User or supervisor escalation | missing authority or ambiguity changes the outcome | clarify objective, approve effect, select trade-off |

One omnibus `Reflect` tool is the wrong abstraction. Its output would be model
prose rather than an authoritative fact, its permission semantics would become
unclear as actions accumulate, and no test could say which capability failed.

Always injecting every status, tool catalog, claim, effect, and child record is
also wrong. It consumes context and attention on every call and weakens the
cacheable prompt prefix. Progressive disclosure is part of the correctness
model, not only a token optimization.

The shared tool registry cannot hold one Session or Run's mutable state. A
runtime-facing interface owned by `internal/core/agent`, carried through the
current call's `context.Context`, provisionally fits both runtime status and
boundary-scheduled control. The Agent Loop writes the facts at the same
transitions that emit events; the handle is a projection, not a second owner.
The existing Note and memory tool pattern demonstrates this layering.

The per-run projection may retain only bounded ephemeral state: monotonic start
time, current and maximum iteration, this-run counters, latest request-context
snapshot, compaction count and pending bit, and access to the current Todo
store where available. A read tool needs an immutable atomic replacement or a
mutex because it may run beside other read-only tools. A control request is a
write-class scheduling barrier even when its immediate mutation is only a
pending bit; the Agent Loop performs the consequential transition.

The same distinction applies to Tool Search. The resolved registry is the
complete capability set already assembled and authorized for this run; the
visible registry is the smaller projection sent on one model request. Loading
a tool changes the latter at an iteration boundary and never mutates the
shared cached registry.

Candidate permission behavior illustrates why read and control surfaces stay
separate:

| Candidate | Access | Default direction | Constraint |
|---|---|---|---|
| runtime status | read-only | allow | calling run only; safe for parallel reads |
| Tool Search | read-only | allow | searches only already resolved, safely disclosable capability |
| tool load | write-class scheduling barrier | allow | changes only the next request's visible definitions |
| context compact | write-class scheduling barrier | unresolved | lossy and billable; runtime recovery floor and hooks still apply |

Requiring interactive approval for every compaction would make the capability
unusable on workers; allowing every early request could waste money and detail.
A candidate runtime invariant permits it only when enough old material exists
to recover a minimum useful amount of context. That decision belongs in the
focused design and evaluation, not in the model prompt.

A surface exposes only controls it can serve. A loop without a compactor may
still offer status while omitting context control. Meta-tools accept no caller-
selected Session, Task, user, or Space identifier; scope comes from the active
run so the model cannot enumerate neighboring work.

Meta-tool output excludes raw instructions, compaction summaries, notes, memory
bodies, message contents, Secret values, environment variables, hidden paths,
other runs' identities or costs, and process-wide health details. Ordinary tool
events record that introspection or control was requested; a new unredacted
trace format must not duplicate the returned snapshot.

## 8. Priority And Candidate Workstreams

Importance and implementation order are not identical. Goal and verification
are the semantic center; runtime status and Tool Search may be cheaper
experiments because BuildMax already owns most of their inputs.

Recommended decision order:

1. **Goal and completion contract.** Define the authoritative relationship
   among objective, Agent-authored plan, acceptance criteria, evidence, and a
   partial or complete result.
2. **Runtime introspection.** Test whether models change phase usefully when
   given honest budget and context state, then evaluate boundary-scheduled
   Agent-requested compaction separately.
3. **Capability discovery.** Measure progressive disclosure against the best
   fixed-tool baseline before designing a universal catalog.
4. **Effect and authority projection.** Make proposed and confirmed side
   effects legible without weakening the existing permission boundary.
5. **Recovery readiness.** Project what is durable and which effects remain
   uncertain across interruption.
6. **Delegation coordination.** Add only when durable or concurrent child work
   demonstrates a parent-visibility gap.
7. **Epistemic records and memory lifecycle.** Add narrowly for claims that
   must survive compaction or Sessions; avoid a general knowledge substrate
   until the bounded form proves insufficient.

Candidate workstreams should remain separately acceptable:

- runtime introspection and context control;
- tool discovery and progressive disclosure;
- objective, evidence, and outcome verification;
- effect tracking and retry safety; and
- recovery and coordination projections.

This proposal supplies common principles and evaluation language. It should not
make acceptance of one workstream imply acceptance of the others.

## 9. Options And Trade-Offs

| Option | Benefit | Cost or failure |
|---|---|---|
| A. Prompt the model to reflect more | nearly no runtime work | produces more prose without authoritative state, evidence, or enforcement |
| B. Add isolated tools as needs appear | small local changes | overlapping scopes and contradictory sources of truth accumulate |
| C. Add one general self-management API | one apparent concept | becomes a model-owned control plane and mixes read, write, permission, and durability semantics |
| D. Inject a complete self-model every iteration | always available | high recurring context and attention cost; fast-changing state damages caching |
| E. Define typed capability classes, then expose the smallest resident, query, control, and gate surfaces | coherent authority and independently testable slices | requires cross-cutting design discipline and evidence before broad implementation |

Option E is the provisional recommendation. Occam's razor applies inside it:
when an existing event, Todo, Task transition, checkpoint, or permission rule
already owns a fact, add a projection or reference rather than another record.

For the runtime-introspection slice specifically:

| Option | Benefit | Cost or failure |
|---|---|---|
| keep status user-only and rely on automatic compaction | no new model schema | the Agent still guesses about remaining budget and cannot request a phase-aware early pass |
| inject live status into every request | no discovery call | recurring context and attention cost; fast-changing data harms caching |
| combine status and compaction in one `Runtime` action tool | one tool name | mixes read and write semantics and creates an extensible control grab bag |
| expose separate status and compaction tools together | clear permissions and behavior | assumes both capabilities earn their cost before either is measured |
| test read-only status first, then admit requested compaction independently | smallest initial mutation surface | delays evidence about phase-aware early compaction |

The final option is preferred. Acceptance of observation must not silently
approve lossy control.

## 10. Risks And Failure Modes

### 10.1 Reflection loops consume the budget they inspect

An Agent may repeatedly call status, search, critique, or verification tools
instead of acting. Tool descriptions, loop guards, bounded outputs, and
evaluation should measure polling and meta-work share. A generic mandate to
“always reflect” is likely harmful.

### 10.2 Self-reports become fake authority

Agent-authored confidence, completion, effect, or permission claims must not
overwrite runtime records. Every projection distinguishes observed,
provider-reported, inferred, proposed, and unavailable facts.

### 10.3 Capability search becomes capability acquisition

Tool Search must not install plugins, activate integrations, connect accounts,
or reveal denied neighboring capability. Those are user or administrator
decisions outside the search contract.

### 10.4 Alternate tools bypass stable denials

Search can make it easier to find a second route to the same external effect.
Permission is therefore enforced at every tool boundary and effect scope, not
only on the first tool name the Agent tried.

### 10.5 An effect ledger promises atomicity it does not have

Recording two effects together does not make them a transaction. Each entry
retains its owner, confirmation semantics, reversibility, and idempotency. The
ledger is a projection for reasoning and audit, not a distributed commit log.

### 10.6 Verification rewards easy checks

An Agent may select a cheap passing test rather than evidence relevant to the
acceptance boundary. Verification must start from authoritative criteria and
name untested limits; test count or exit status alone is not proof.

### 10.7 Durable cognition becomes permanent contamination

Claims and memory that lack provenance, scope, bounds, revalidation, and
deletion become a source of repeated error. The default is ephemeral; durable
storage must earn its cost.

### 10.8 More meta-capability obscures the user experience

Users should not have to understand internal self-regulation machinery to ask
for work. Surface detail belongs in traces and diagnostics; ordinary results
show decisions, material effects, evidence, and limitations in plain language.

### 10.9 Runtime controls damage the state they inspect

Runtime introspection and control have several specific integrity failures that
the focused design must preserve:

| Failure | Required behavior |
|---|---|
| status result increases context | report snapshot scope and keep the default result small |
| estimated count appears exact | return precision and measurement iteration |
| compaction requested in a parallel batch | record a synchronized request; mutate only after the complete batch commits |
| queued input is summarized before it is seen | perform the prior request before pending-input injection |
| hook blocks compaction | return a bounded refusal and leave history unchanged |
| summarizer fails | report failure and retain the original history |
| compaction boundary cannot persist | stop rather than continue with disagreeing durable and in-memory views |
| surface lacks a compactor or context window | omit the control or report unavailable; never invent zero |

The existing automatic compaction path remains the safety net. A model failing
to inspect status or request an early pass must not cause the runtime to abandon
its hard protections.

## 11. Evidence Needed

Every workstream should compare against the strongest simple baseline, not a
deliberately under-equipped Agent.

### 11.1 Scenario classes

- long repository investigations that must switch from exploration to
  synthesis under a tight context or iteration limit;
- large tool catalogs containing several plausible but differently privileged
  capabilities;
- tasks with explicit acceptance criteria and tempting but insufficient tests;
- consequential actions with a timeout or ambiguous external result;
- interrupted and resumed runs with both durable and transient state;
- delegated work containing overlap, disagreement, late results, and partial
  failure; and
- Project Memory containing a stale or contradicted fact.

### 11.2 Outcome metrics

- user acceptance satisfied, not merely Agent self-reported completion;
- repeated or unnecessary work;
- unsupported claims and missed contradictory evidence;
- premature completion and unreported verification gaps;
- unauthorized or duplicate side effects;
- recovery correctness after interruption;
- model calls, prompt/tool-definition tokens, elapsed time, and cost;
- meta-capability calls and share of iterations spent on them; and
- frequency and quality of escalation to the user.

### 11.3 Architecture invariants

- one authoritative owner for every fact and transition;
- no raw Secret, instruction, memory, neighboring-resource, or hidden tool
  disclosure through meta-capabilities;
- no mutation of a shared cached registry by one run;
- tool-call groups and compaction boundaries remain valid under concurrency;
- status absence is not reported as zero;
- an Agent cannot widen its own limits, authority, plugin activation, or
  durable scope; and
- every claimed completion and consequential effect can cite or explicitly
  lack authoritative evidence.

### 11.4 Candidate staged experiments

The workstreams remain independently rejectable, but a practical experiment
order reuses evidence instead of introducing every meta-capability at once.

**Stage 0 — observation baseline.** Reconcile context estimation across
`EventLLMStart`, `EstimateRunUsage`, traces, interactive status, system prompt,
resident state, history, and tool schemas. Record long scripted-run outcomes
without any LLM-facing status or search capability.

**Stage 1 — read-only runtime status.** Test concurrent reads, cancellation,
unknown context windows, absent Todo stores, resumed Sessions, Conversation
history, subagents, and worker assembly. Measure whether small and large models
distinguish current occupancy from cumulative usage and change phase at useful
times rather than merely narrating the numbers.

**Stage 2 — capability search.** Compare a fixed direct tool set, the existing
MCP gateway pattern, and search-plus-direct-promotion. Measure definition
tokens, extra discovery turns, correct tool and argument selection, futile
search, permission-aware selection, and behavior when search returns nothing.

**Stage 3 — boundary-scheduled compaction.** Cover repeated requests,
concurrent sibling tools, automatic/requested collision, queued input, hook
denial, summarizer and persistence failures, and cancellation. Measure recovered
context, added cost, premature compaction, information loss, repeated work, and
whether checkpointed notes and Todos preserved what synthesis needed.

**Stage 4 — objective and verification contract.** Require explicit acceptance
criteria and evidence on tasks where an easy passing check is insufficient.
Compare completion correctness, truthful partial results, missed verification
limits, escalation quality, cost, and latency with the best ordinary Agent
baseline.

Later effect, recovery, delegation, and memory experiments should be admitted
only when earlier evidence identifies the corresponding failure rather than as
automatic consequences of accepting the family.

## 12. Goals And Non-Goals

Goals:

- establish a common vocabulary for Agent self-regulation capabilities;
- state which facts and transitions remain runtime-owned;
- identify the smallest high-value workstreams and their dependencies;
- prevent runtime introspection and Tool Search from becoming isolated feature
  islands; and
- define evidence that can reject capabilities whose complexity exceeds their
  benefit.

Non-goals:

- approve implementation or roadmap priority;
- define final tool names, JSON schemas, database tables, or UI;
- expose model chain-of-thought;
- create a universal planner, critic, knowledge graph, or control plane;
- make every capability available on every surface;
- replace user judgment for ambiguous goals or consequential trade-offs; or
- infer that more autonomy, memory, tools, or Agents is automatically better.

## 13. Open Questions

1. What is the smallest objective and acceptance contract that works for both a
   local Session turn and a durable Task without making one own the other?
2. Should completion verification run inside the working Agent, through an
   independent verifier, through deterministic criteria, or as a risk-based
   combination?
3. Which tools must remain in an always-visible kernel so a failed Tool Search
   cannot strand the Agent?
4. Should Tool Search reveal a denied capability's existence with no detail,
   or omit it entirely to avoid both information leakage and futile planning?
5. Can effect scope be expressed consistently across files, Bash, MCP, Issues,
   messages, and future integrations without pretending they share a
   transaction model?
6. Which epistemic claims deserve durable storage rather than ordinary notes
   or a compaction summary?
7. When should the runtime require recovery-readiness or outcome-verification
   checks, and when would that ceremony harm simple conversational work?
8. How should a parent Agent recognize duplicated or conflicting child work
   without loading every child trace into its own context?
9. Which self-regulation facts should appear in ordinary user results, and
   which belong only in trace and diagnostic views?
10. What proportion of tokens or iterations spent on meta-work is evidence of
    useful control rather than self-observation overhead?
11. Is the latest prepared-request context snapshot sufficient, or does a model
    need a separately labelled next-request projection?
12. Should runtime status expose completed Todo text, counts only, or a recent
    bounded subset, and which demonstrated decision requires that detail?
13. What minimum summarizable token count or recoverable context share should
    permit Agent-requested compaction, and should an unattended Agent definition
    have to opt in because the operation is lossy and billable?
14. Does elapsed time improve behavior when no deadline exists, or should the
    Agent see remaining duration only when a real deadline applies?

## 14. Likely Destination

This proposal's primary domain is **Agent Runtime and Models**, with related
questions in Product and Execution Model, Trust and Security, and Verification.

If its framing is accepted:

- durable cross-cutting principles move into a focused design record or the
  existing [product vision](../design/product-vision.md);
- each independently valuable workstream receives its own design decision,
  evidence plan, roadmap placement, and backlog tasks;
- Agent Loop and tool architecture documents describe only shipped runtime
  boundaries;
- user manuals describe only capabilities users can actually invoke or
  configure; and
- this proposal is deleted after its accepted rationale has moved to authoritative
  records, or deleted outright if evidence rejects the family framing.
