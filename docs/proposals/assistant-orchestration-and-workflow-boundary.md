# Assistant Orchestration And The Workflow Boundary

> **简体中文：** [阅读中文镜像](../zh-CN/proposals/assistant-orchestration-and-workflow-boundary.md)
>
> **Audience:** contributors and product reviewers · **Status:** proposal — under discussion
> **Opened:** 2026-09-05

Related: [roadmap](../ROADMAP.md),
[product vision](../design/product-vision.md),
[Workflow runtime](../design/workflow-runtime.md),
[Agent execution and Task threads](../design/agent-execution-and-task-threads.md),
[Portal execution model](../design/portal-execution-model.md), and
[skills and subagents](../../manual/skills-and-subagents.md).

## Contents

- [1. Question And Recommendation](#1-question-and-recommendation)
- [2. Current Context](#2-current-context)
- [3. First-Principles Model](#3-first-principles-model)
- [4. Candidate Product Concepts](#4-candidate-product-concepts)
- [5. Comparative Analysis](#5-comparative-analysis)
- [6. When Delegation Creates Real Value](#6-when-delegation-creates-real-value)
- [7. User Experience And Information Architecture](#7-user-experience-and-information-architecture)
- [8. Candidate Runtime Contract](#8-candidate-runtime-contract)
- [9. Relationship To Workflow](#9-relationship-to-workflow)
- [10. Options](#10-options)
- [11. Risks And Failure Modes](#11-risks-and-failure-modes)
- [12. Evidence Program](#12-evidence-program)
- [13. Staged Delivery](#13-staged-delivery)
- [14. Goals And Non-Goals](#14-goals-and-non-goals)
- [15. Open Questions](#15-open-questions)
- [16. Likely Destination](#16-likely-destination)

## 1. Question And Recommendation

BuildMax has accepted a durable adaptive Workflow design centered on a
revision-pinned execution graph. A competing product direction is now worth
testing: let one configured Agent act as a manager that may invoke a bounded
roster of existing Space Agents and dynamically decide how to complete an
objective. This paper calls that provisional user-facing concept an
**Assistant**.

The decision is not whether a sufficiently capable model can call another
model. It is whether exposing and persisting an Assistant creates enough value
over one strong Agent to justify another product concept, and whether that
changes the scope or priority of Workflow.

The provisional recommendation is:

1. Treat one strong Agent as the default baseline for open-ended work.
2. Test bounded Agent-to-Agent delegation as an optional Agent capability,
   not as a new top-level domain entity.
3. Promote that capability to an Assistant product concept only when
   differentiated specialists, parallelism, or governance produce measured
   value that a single Agent does not.
4. Retain Workflow for durable, repeatable automation whose trigger, policy,
   approvals, and important side effects must be deterministic.
5. Do not prioritize a general-purpose graph editor. If Assistant evidence is
   positive, make Assistant the default interface for adaptive work and narrow
   Workflow's user-facing role toward Automation. (A visual editor for the
   static Workflow DAG has since shipped under its own
   [design record](../design/portal-workflow-visual-editor.md); it edits the
   existing definition and sits outside this paper's question.)

The resulting product principle would be:

> Use an Agent for direct work, bounded delegation for adaptive spacework, and
> Workflow for reliable automation. All three execute through Task and
> TaskRun.

This is a proposal, not a reversal of the accepted
[Workflow runtime](../design/workflow-runtime.md). The durability, authority,
idempotency, and recovery decisions in that record remain useful under every
option. What is reopened here is the default authoring model, the amount of
graph breadth worth shipping, and the Portal vocabulary.

## 2. Current Context

### 2.1 What The Product Exposes

Portal's Space sidebar groups Chat, Issues, Agents, Workflows, and Schedules
under **Work**, and Files and Artifacts under **Resources**. Chat starts a
Conversation; an Agent can also be invoked directly through a durable Task and
TaskRun. Workflow is a Space-scoped reusable plan: a static DAG of Agent Task
nodes joined by `needs` edges.

The Workflow editor is a visual node-and-edge canvas with a raw JSON view (see
[Portal Workflow visual editor](../design/portal-workflow-visual-editor.md)).
It authors that static graph — nodes, dependencies, bindings, and result
contracts — but the definition has no conditional branches, retries, or human
waits for it to express.

The accepted Workflow design addresses runtime correctness by replacing the
linear callback sequencer with a durable graph. It deliberately says that a
semantic form should precede a general canvas and that a one-off open-ended
objective should normally remain one Agent Task.

### 2.2 The New Assistant Hypothesis

The proposed Assistant is not a different kind of model loop. It is an Agent
definition with a bounded delegation capability:

```text
User objective
  -> coordinating Agent
       -> permitted specialist Agent
       -> permitted specialist Agent
       -> coordinating Agent returns one result
```

The coordinating Agent owns semantic decomposition and synthesis. The system
still owns admission, permissions, budget, concurrency, deadlines, durable
child execution, cancellation, and audit.

This resembles the manager pattern described by the
[OpenAI Agents SDK](https://openai.github.io/openai-agents-python/multi_agent/):
specialists are exposed as tools while one manager retains control and owns the
final response. It is distinct from a handoff, where the selected specialist
becomes the active user-facing Agent.

### 2.3 Ecosystem Signal

The ecosystem does not point to one universal orchestration model:

- [Anthropic distinguishes workflows from agents](https://www.anthropic.com/engineering/building-effective-agents):
  predefined code paths provide predictability for well-defined work, while
  model-directed agents provide flexibility for open-ended work. Its guidance
  also starts with the simplest solution because agentic complexity trades
  latency and cost for task performance.
- The
  [OpenAI Agents SDK orchestration guide](https://openai.github.io/openai-agents-python/multi_agent/)
  presents LLM-decided and code-decided orchestration as composable choices,
  not exclusive architectures. It also makes specialist Agents callable as
  tools, which is close to the Assistant hypothesis.
- [LangGraph](https://docs.langchain.com/oss/python/langgraph/overview)
  treats durable execution, persistence, human intervention, and observability
  as runtime concerns that remain necessary for both workflows and agents.
- [Genspark Workflow](https://www.genspark.ai/helpcenter/workflows) uses
  natural-language creation, continued conversational refinement, simulated
  test runs, and explicit activation. This is useful authoring evidence, but
  its public documentation does not specify the internal durability, dataflow,
  versioning, or recovery contract.

The common signal is a hybrid boundary: models decide semantic work; software
retains authority over durable state and consequential actions.

## 3. First-Principles Model

An Agent system must solve three different problems. Treating them as one
problem creates either needless ceremony or an unsafe control plane.

| Problem | Dominant concern | Best initial authority |
|---|---|---|
| Semantic execution | Understand intent, search, use tools, revise an approach | One capable Agent |
| Adaptive decomposition | Decide which specialist work is useful from intermediate results | A coordinating Agent within a bounded roster |
| Stateful coordination | Persist facts, enforce policy, admit work, recover, cancel, and audit | Deterministic application code |

Increasing model capability may collapse the first two rows: one model may be
able to perform work itself or decide when another context is beneficial. It
does not eliminate the third row. A more capable model does not become the
transaction log, authorization service, lease manager, quota ledger, or
side-effect owner.

This leads to two independent axes rather than a hierarchy of intelligence:

| Axis | Low end | High end |
|---|---|---|
| Semantic adaptivity | Published path | Model decides useful next work |
| Operational authority | Model proposes | System validates, commits, and records |

BuildMax may choose high semantic adaptivity without granting high operational
authority. Assistant is viable only with that separation.

## 4. Candidate Product Concepts

### 4.1 Agent

An Agent remains reusable execution configuration: instructions, model policy,
tools, plugins, sandbox and Secret consumption, hooks, and output behavior. A
Task is one durable objective for that Agent; TaskRun owns each turn or attempt.

The strongest default should be one Agent with all capabilities that are safe
and relevant to its objective. Introducing more Agents must earn its cost.

### 4.2 Assistant

Assistant is a provisional product label for an Agent revision that declares:

- a stable responsibility and completion contract;
- a bounded catalog of callable Agent revisions under role aliases;
- delegation, parallelism, depth, budget, and deadline limits;
- rules for consequential operations and human confirmation; and
- the structured result the coordinating Agent must return.

An Assistant is therefore not intrinsically more intelligent than an Agent.
It is a reusable Space interface with a delegation and governance envelope.

Initially, Assistant should not be a separate table, execution plane, run type,
or model loop. The internal name can remain a coordinating or delegation-enabled
Agent until product evidence justifies a distinct projection.

### 4.3 Specialist

Specialist is a possible user-facing label for an ordinary Agent made
available to an Assistant. It becomes useful only when it has a meaningful
difference in at least one of these dimensions:

- tools or data access;
- domain instructions or retained context;
- sandbox or permission boundary;
- required input and output contract;
- model and cost profile; or
- independent work that benefits from parallel execution.

Splitting one prompt into several Agents with the same model, tools, context,
and permissions is not specialization. It is extra inference and coordination.

### 4.4 Workflow Or Automation

Workflow remains a revision-pinned deterministic coordinator over Task and
TaskRun. If the Assistant hypothesis succeeds, the clearer user-facing label
may be **Automation**: a repeatable process whose trigger, fixed gates,
approvals, delivery, and side-effect boundaries are known before a run starts.

An Automation may call an Assistant as one node. Its deterministic envelope
does not imply that all semantic work inside that Assistant follows a static
graph.

## 5. Comparative Analysis

### 5.1 Product And Runtime Comparison

| Dimension | One strong Agent | Assistant with bounded delegation | Workflow / Automation |
|---|---|---|---|
| User definition | Objective plus Agent selection | Objective plus coordinating policy and specialist roster | Trigger, typed stages, bindings, policy, and result |
| Next semantic action | Agent | Coordinating Agent | Published definition, optionally informed by typed model output |
| Path known before run | No | No | Yes within declared bounded expansion |
| Best fit | One-off and open-ended work | Open-ended work needing real specialization or parallelism | Repeated, governed, event-driven work |
| Setup cost | Lowest | Medium | Highest |
| Model calls | Baseline | Usually higher | Depends on declared nodes |
| Latency predictability | Medium | Lowest unless bounded carefully | Highest |
| Cost predictability | Medium | Lowest unless bounded carefully | Highest |
| Adaptation to novel evidence | High | Highest | Bounded by the definition |
| Reproducibility | Low | Low | Highest |
| Permission separation | Coarse per Agent | Per coordinator and specialist | Per declared node and run policy |
| Failure diagnosis | One trace and Task | Parent decision ledger plus child Tasks | Declared topology plus NodeRuns |
| Durable recovery need | Task/TaskRun | Parent Task plus durable child Tasks | WorkflowRun, NodeRuns, Tasks, and TaskRuns |
| Primary UX | Ask this Agent | Give work to this managed space | When this happens, reliably do this |

### 5.2 What A Stronger Model Changes

A stronger general Agent weakens several common arguments for multi-Agent
systems. It can keep a larger context, select tools, plan, critique its own
work, and produce a unified result without handoff loss. For many tasks this
is cheaper and more reliable than a manager plus specialists.

It does not erase all delegation value. Separate child contexts can isolate
large investigations, parallel Tasks can reduce wall-clock time, and distinct
permissions can reduce blast radius. These are system properties rather than
claims that several weaker models necessarily reason better than one stronger
model.

The correct baseline is therefore not a weak monolithic Agent. Every
delegation evaluation must compare against the best affordable single-Agent
configuration with equivalent safe access to tools and information.

### 5.3 Definition Burden

| Configuration | One strong Agent | Assistant | Workflow |
|---|---|---|---|
| Goal and instructions | Required | Required | Required per Agent node |
| Tool and permission selection | Required | Required for coordinator and specialists | Required for every referenced Agent |
| Specialist roster | None | Required | Optional; nodes reference Agents directly |
| Data bindings | Implicit in Agent context | Proposed by coordinator and recorded by runtime | Declared and validated |
| Branches and parallelism | Dynamic and implicit | Dynamic and explicit in child Tasks | Declared and versioned |
| Trigger | Caller starts a Task | Caller starts a Task | First-class automation concern |
| Publish-time graph review | None | No static graph; review roster and limits | Required |

Assistant reduces graph authoring only if roster configuration remains
materially smaller and more stable than the graph it replaces. A large roster
with elaborate role instructions, routing descriptions, and inter-Agent
contracts can become a graph encoded badly in prose.

## 6. When Delegation Creates Real Value

### 6.1 Strong Cases

Delegation is most likely to beat one Agent when one or more of these are true:

1. **Context isolation.** A specialist needs to inspect a large corpus or
   workspace and can return a bounded result instead of polluting the parent
   context.
2. **True parallel work.** Several independent investigations dominate elapsed
   time and their results can be synthesized later.
3. **Capability isolation.** Different Agents require distinct tools, Secrets,
   sandboxes, or data access that should not be granted to one broad Agent.
4. **Stable specialist contract.** A frequently reused domain role has a
   measurable input/output contract and can be improved independently.
5. **Cost routing.** Narrow subtasks can safely use cheaper models while the
   coordinator retains a stronger model for synthesis.
6. **Independent accountability.** Users or operators need to inspect which
   role produced a conclusion, Artifact, or side effect.

### 6.2 Weak Cases

Assistant is unlikely to justify itself when:

- all participants use the same model, instructions, tools, and context;
- the manager merely paraphrases a request and later summarizes the answer;
- the work is short enough for one context;
- sequencing is stable and can be expressed more cheaply as a Workflow;
- coordination cost dominates specialist work;
- the output cannot be evaluated beyond subjective preference; or
- the roster exists only to make a multi-Agent product claim.

### 6.3 The Dominance Rule

For any class of work, select the least complex mechanism that meets its
requirements:

```text
Can one capable Agent complete it within policy and quality targets?
  yes -> use one Agent
  no  -> does real specialization or parallelism address the gap?
           yes -> allow bounded delegation
           no  -> is the desired path stable and repeatable?
                    yes -> use Workflow / Automation
                    no  -> improve the Agent, tools, context, or task definition
```

This is a product default, not a prohibition. Evaluation may demonstrate that
a different mechanism dominates for a specific workload.

## 7. User Experience And Information Architecture

### 7.1 Do Not Add Three Peer Navigation Items

Putting Workflows, Agents, and Assistants beside each other makes an internal
type distinction into a prerequisite for doing work. Users would have to learn
which one to create first, which one to invoke, and why an Assistant is both an
Agent and a peer of Agents.

If Assistant earns a product surface, the information architecture should
communicate containment:

```text
SPACE
  Home
  Issues
  Artifacts

BUILD
  AI Space
    Assistants
    Specialists
  Automations
```

This is a direction for usability testing, not committed navigation or naming.
`AI Space`, `Assistant`, `Specialist`, and `Automation` are provisional labels.

### 7.2 Home Is The Default Work Surface

Home should let a user state an objective without first understanding the
configuration taxonomy. If a Space has one default coordinating Agent, the
composer can name it directly. If several exist, the composer may offer a
small selector near the input rather than requiring navigation into a catalog.

The normal user mental model becomes:

- an Assistant is who receives adaptive work;
- a Specialist is a configured capability the Assistant may use; and
- an Automation is when and under what fixed policy mature work runs again.

Administrators may inspect the underlying Agent identity and revisions. Those
details do not need to lead the ordinary work-starting experience.

### 7.3 Assistant Authoring

An Assistant editor should ask for semantic and governance choices rather than
a graph:

- responsibility and examples of appropriate work;
- completion criteria and structured result;
- permitted specialists, shown by role and capability;
- maximum child count, concurrency, total budget, deadline, and depth;
- whether consequential specialist calls require approval; and
- whether the Assistant may finish without delegating.

The last point is important. Delegation is available, not mandatory. The
coordinating Agent should answer directly when delegation would not improve the
result.

### 7.4 Automation Authoring

Automation should lead with a readable rule:

```text
Every Monday at 09:00
  -> start the Sales Review Assistant
  -> request approval for the final report
  -> after approval, deliver it to the selected channel
```

The runtime may compile this into a graph, but the primary editor need not be
a canvas. Natural-language creation can propose a draft; semantic controls,
validation, a safe test run, and an explicit publish or enable action remain
the maintainable source of user confidence.

## 8. Candidate Runtime Contract

This section is intentionally narrower than an implementation design. It
defines what must be true before a delegation experiment can be trusted.

### 8.1 Agent Revision Extension

A delegation-enabled Agent revision may declare a catalog such as:

```json
{
  "delegates": [
    {
      "role": "researcher",
      "agent_id": "agt_researcher",
      "agent_revision": 4,
      "description": "Investigates a bounded question and returns cited findings"
    },
    {
      "role": "analyst",
      "agent_id": "agt_analyst",
      "agent_revision": 2,
      "description": "Evaluates structured evidence against supplied criteria"
    }
  ],
  "delegation_policy": {
    "max_depth": 1,
    "max_children": 8,
    "max_parallel": 3
  }
}
```

The exact persisted shape is deferred. The important invariants are:

- publication pins every delegate Agent revision;
- the model selects a role alias, never an arbitrary Agent id;
- authorization may narrow the published catalog at admission but never widen
  it;
- a child receives only explicitly constructed input, not the parent's hidden
  prompt or unrestricted session;
- the parent cannot grant tools, Secrets, plugins, sandbox access, or Issue
  access that the child definition and caller do not permit; and
- total depth, child count, concurrency, deadline, and spend remain bounded by
  system-enforced policy.

### 8.2 Delegation Is A Durable Operation

The coordinating Agent may propose operations resembling:

```text
delegate(role, objective, input)
inspect_child(task_id)
request_human_input(question)
finish(result)
```

These are logical capabilities, not committed LLM-facing tool names.

`delegate` must admit a normal child Task through the Task service. The child
Task and each TaskRun remain authoritative for execution, trace, usage,
Artifacts, cancellation, and result. The parent-child relation and idempotency
key must be durable so a retry cannot create duplicate child work.

Waiting for children must not hold a worker. The parent Task needs a durable
waiting or resumable condition, and child completion must wake it through a
recoverable reconciliation path rather than an in-memory callback. A resumed
parent turn receives bounded, typed child results and references to full
Artifacts and traces.

### 8.3 Decision Ledger

A dynamic plan is not known before execution, so observability must record
decisions as they occur. At minimum, operators need to answer:

- why the coordinator requested a specialist;
- which pinned Agent revision ran;
- what bounded input it received;
- whether the request was admitted, refused, or required approval;
- what the child produced and consumed;
- whether the coordinator used or ignored the result; and
- why the parent declared completion or requested more work.

The decision ledger is evidence, not a second execution authority. Task and
TaskRun state remain authoritative.

### 8.4 Initial Boundaries

The first experiment should enforce:

- maximum delegation depth of one;
- only ordinary worker Agents in the delegate catalog;
- no Assistant-to-Assistant recursion;
- no dynamically discovered Space Agent;
- no mutation of the catalog during a run;
- no direct child side-effect permission inherited from the parent;
- no worker held while a parent waits; and
- one structured final result owned by the parent TaskRun.

These limits remove several interesting demos. They also make cost, recovery,
authorization, and causality testable.

## 9. Relationship To Workflow

### 9.1 What Remains Shared

Both adaptive delegation and deterministic Workflow require:

- immutable execution-sensitive revisions;
- idempotent Task and TaskRun admission;
- deadlines, quotas, cancellation, and retry ownership;
- durable waits and restart recovery;
- structured inputs and results;
- Artifact references rather than lossy summaries;
- human authorization for consequential actions; and
- an inspectable event history.

The accepted Workflow design remains valuable because it specifies much of
this substrate. Assistant should reuse those invariants instead of creating a
second scheduler or callback chain.

### 9.2 What Must Stay Different

| Concern | Assistant | Workflow / Automation |
|---|---|---|
| Semantic path | Emerges during the run | Declared or bounded by the published definition |
| Reuse unit | Responsibility, roster, and policy | Trigger, graph, bindings, and policy |
| Change review | Agent and roster revision diff | Definition and topology diff |
| Completion | Model proposes a structured result; runtime validates it | Declared result node and state machine determine readiness |
| Replay expectation | Re-execution may choose different work | Same revision preserves the permitted topology and transitions |
| Primary failure | Bad decomposition or synthesis | Bad definition or deterministic coordination failure |

Assistant is not a Workflow with invisible edges. Workflow is not an Assistant
whose choices were cached. They may share a coordinator implementation, but
their user contracts and debugging models differ.

### 9.3 Automation Around An Assistant

A useful composition is a deterministic envelope around adaptive semantic
work:

```text
inbound event or schedule
  -> deterministic filter and input mapping
  -> Assistant completes an open-ended objective
  -> deterministic validation and approval
  -> consequential delivery action
```

The envelope fixes trigger deduplication, authorization, deadline, approval,
and delivery. The Assistant remains free to decide whether and how to delegate
inside its bounded execution.

### 9.4 Discovery Then Hardening

Assistant and Workflow can form a lifecycle rather than competing catalogs:

```text
one capable Agent explores a new task
  -> bounded delegation handles recurring complexity
  -> traces reveal a stable repeated process
  -> user proposes or generates an Automation draft
  -> test, review, and publish harden it
```

BuildMax should not automatically convert one successful trace into an active
Workflow. A trace contains contingent decisions and data, not a safe reusable
contract. Conversion must produce a reviewable draft with typed inputs,
bindings, limits, Agent revisions, and side-effect policy.

### 9.5 Invoking A Callable Unit From The Conversation Surface

The principle in section 1 — Agent, delegation, and Workflow all execute
through Task and TaskRun — has a consequence for the foreground conversation.
From the caller's side, a direct Agent Task and a Workflow run are the same
shape: a typed objective admitted onto Task and TaskRun that yields a durable,
observable result. The foreground surface should treat "run this Agent" and
"run this Workflow" as two ways to launch a **callable unit**, not two
unrelated features.

When this section was written they were asymmetric: the foreground
conversation agent built only four Task tools — start, list, get, and continue
a Task — so an agent asked to run a Space's published Workflow could neither
see it nor start it, even though the Workflow already lands on the same Task
plane the agent uses for a direct Task.

That gap is now closed. The conversation surface has **invoke-and-observe**
access to Workflows, mirroring the Task tools, and authoring is withheld. The
space-scoped tools live in `internal/service/conversation`:

| Foreground tool | Task-tool analog | What it does |
|---|---|---|
| `ListWorkflows` | List Tasks | Enumerate the Space's published Workflows with their input schema |
| `RunWorkflow` | Start Task | Start a run of one published Workflow with typed input |
| `GetWorkflowRun` | Get Task | Read a run's status, result, and node states |

Locally, `buildmax workflow list`, `run`, and `status` give a person the same
invoke-and-observe surface. Four constraints are load-bearing:

- Only a published Workflow is a callable unit. A draft or archived definition
  is not listable or runnable from the conversation.
- The model must supply input that conforms to the Workflow's published input
  schema. Invocation validates the input and returns a typed failure the model
  can correct, never a silent or untyped error — the same validate-or-typed
  result rule the run boundary already applies to structured output.
- Issue linkage is an explicit, optional parameter. The conversation does not
  infer an Issue from its own context, preserving the rule that a Conversation
  may originate work without becoming its ownership or authorization parent.
- The run is an ordinary WorkflowRun on the Space's Task plane. The
  conversation observes durable state; it does not own or mutate node
  execution. This is the same authority boundary section 11.4 requires of
  delegation.

Authoring stays out. A conversational tool that emitted a Workflow definition
would erase the very property that distinguishes a Workflow from an Agent trace
(section 9.2): a diffable plan reviewed before it can run. Authoring belongs to
the discovery-then-hardening path in section 9.4 — a foreground agent may
propose a reviewable draft, but activation remains a human publish action, not
a tool call.

The same asymmetry exists for Issue, the primary user-facing work object, which
also has no conversation tool. Extending the identical invoke-and-observe
treatment to Issue is the obvious next symmetry once the Workflow surface is
proven, but it is out of scope for this section.

## 10. Options

### 10.1 Option A: Continue With Workflow-First Product Development

Build the accepted durable graph, semantic form, topology view, and bounded
adaptive patterns before adding Space Agent delegation.

**Advantages**

- follows an accepted design with explicit correctness criteria;
- produces predictable repeated execution;
- has a strong fit for governance and operational automation; and
- avoids another user-facing concept.

**Costs**

- complex authoring remains expensive;
- users must anticipate paths that a capable model could choose at run time;
- graph breadth may be built before demand is demonstrated; and
- open-ended knowledge work may fit one Agent better.

### 10.2 Option B: Add Assistant As A Separate First-Class Entity

Create Assistant, AssistantRevision, and AssistantRun beside Agent and
Workflow and expose all three in Portal.

**Advantages**

- makes the managed-space concept explicit;
- permits independent Assistant lifecycle and product presentation; and
- can optimize APIs and analytics around orchestration.

**Costs**

- duplicates Agent identity, revision, execution, or result concepts unless
  boundaries are exceptionally disciplined;
- creates immediate navigation and vocabulary ambiguity;
- commits to a product taxonomy before value is measured; and
- risks a second execution plane.

This option is not recommended for the first experiment.

### 10.3 Option C: Add Bounded Delegation To Agent, Then Decide The Product

Extend an Agent revision with an optional pinned delegate catalog and run every
delegation as a durable child Task. Keep one strong Agent as the default. If
evidence is positive, present selected delegation-enabled Agents as Assistants
inside an `AI Space` surface and narrow Workflow toward Automation.

**Advantages**

- tests the hard value proposition before adding a new domain entity;
- reuses Agent, Task, TaskRun, worker, trace, Artifact, and policy boundaries;
- keeps direct Agent execution intact;
- supports an evidence-based Portal taxonomy; and
- preserves Workflow for deterministic cases.

**Costs**

- requires durable parent-child Task semantics not yet designed;
- an Agent definition gains another capability dimension;
- Assistant-specific reporting may later require a projection or migration;
  and
- the Workflow roadmap must avoid assuming graph breadth is the next priority.

This is the recommended experiment.

### 10.4 Option D: Use One Strong Agent And Do Not Add Delegation

Continue improving the main Agent's model, tools, context engineering, skills,
and direct Task experience. Retain Workflow only for deterministic automation.

**Advantages**

- lowest product and runtime complexity;
- lowest coordination latency and failure surface;
- no Agent-versus-Assistant vocabulary problem; and
- likely best for many ordinary objectives as models improve.

**Costs**

- no child-context isolation or independent parallel work;
- coarse capability and permission boundaries;
- large investigations compete for one context; and
- no reusable Space roster contract.

This is not a rejected fallback. It is the baseline that Option C must beat.

## 11. Risks And Failure Modes

### 11.1 Coordination Tax Without Quality Gain

The manager may restate the request, wait for workers, and summarize outputs
without adding information. Cost and latency rise while quality stays flat or
falls through lossy handoffs.

Mitigation is empirical: compare against one strong Agent, expose delegation
counts and cost, and let the coordinator finish without delegation.

### 11.2 Prompt-Encoded Hidden Graph

A large roster description may become a fragile routing program written in
natural language. It is harder to diff and test than the Workflow graph it was
meant to avoid.

When a path becomes stable, move it into Automation rather than accumulating
more routing prose.

### 11.3 Unbounded Recursive Work

Assistants calling Assistants can produce exponential task creation, cost,
ambiguous cancellation, and cycles that are invisible before execution.

Initial depth one and system-enforced total budgets are mandatory. Deeper
delegation remains an evidence-gated extension.

### 11.4 Confused Authority

If the parent mutates child state directly or treats its conversation context
as authoritative, restart and retry semantics become unreliable.

Every child must be an ordinary Task admitted by the Task service. The parent
observes durable results; it does not own worker state.

### 11.5 Permission Amplification

A coordinator with a broad roster can become a confused deputy: untrusted
content persuades it to invoke a specialist with privileges the initiating
user did not intend.

Admission must intersect caller authority, parent policy, child policy, and
Space policy. Consequential delegation may require human approval even when the
child Agent is in the catalog.

### 11.6 UX Taxonomy Becomes The Architecture

Prematurely adding Assistant to navigation can force long-lived distinctions
that users do not understand. Conversely, hiding meaningful permission and
cost boundaries behind one generic Agent label can make control impossible.

The experiment should test both behavior and comprehension before committing
names or navigation.

### 11.7 Model Progress Invalidates The Split

A future model may absorb tasks that currently benefit from specialists. A
static multi-Agent topology could then become permanent overhead.

The roster must remain optional, and evaluation must be rerun when the default
model changes. Product value should rest on context isolation, parallelism,
permissions, or accountability, not on an assumption that the coordinator is
too weak to do specialist work.

## 12. Evidence Program

### 12.1 Evaluation Arms

Every candidate workload should run through three comparable arms:

| Arm | Configuration |
|---|---|
| A | Best affordable single Agent with equivalent safe information and tool access |
| B | The same coordinator class with a bounded, differentiated specialist roster |
| C | A deterministic Workflow when the task has a plausible stable decomposition |

Models, total budget, available data, success criteria, and side-effect policy
must be recorded. Artificially weakening Arm A would make the result useless.

### 12.2 Workload Classes

The first suite should include:

- broad research where independent lines of inquiry can run in parallel;
- one large-context investigation where a child can return a bounded result;
- cross-tool work with distinct permission domains;
- a simple task expected to favor one Agent;
- a stable repeated process expected to favor Workflow; and
- a deceptive or malformed input that attempts permission amplification.

At least one workload must contain a server restart or lost completion signal
while a parent waits for child Tasks. Otherwise the experiment measures a demo,
not the proposed durable product.

### 12.3 Metrics

Measure:

- task completion and output quality against workload-specific graders;
- unsupported claims and lost information across delegation boundaries;
- user setup time and correction turns;
- model and tool cost;
- median and tail completion latency;
- number of child Tasks and unused child results;
- policy refusals and unauthorized-action attempts;
- successful recovery after parent, worker, or server interruption;
- variance across repeated runs; and
- operator time to explain a failure from persisted evidence.

Assistant earns a product surface only if it materially improves a target
workload over Arm A without an unacceptable safety, reliability, cost, or
usability regression. Thresholds should be set per workload before running the
evaluation; this proposal does not invent one universal percentage.

### 12.4 UX Research

Show participants three navigation and creation models without explaining the
architecture first:

1. Agents, Assistants, and Workflows as peer items;
2. Assistants and Specialists nested under `AI Space`, plus Automations; and
3. one Agent catalog with optional delegation settings, plus Workflows.

Ask participants to start an open-ended analysis, configure a reusable space,
and schedule a governed repeat run. Record first-click success, completion
time, taxonomy errors, and their explanation of each concept afterward.

The peer-navigation model should not ship merely because participants can be
taught it. The test is whether the product communicates the distinction before
instruction.

## 13. Staged Delivery

### 13.1 Phase 0: Preserve Baselines And Instrumentation

- Add evaluation tasks that represent the workload classes in section 12.
- Record direct-Agent quality, cost, latency, tool use, and intervention rate.
- Define the expected parent-child evidence and fault-injection cases.
- Do not change Portal navigation or introduce an Assistant entity.

### 13.2 Phase 1: Bounded Delegation Primitive

- Let an Agent revision reference a small pinned delegate catalog.
- Add one server-owned delegation capability that admits durable child Tasks.
- Enforce depth one, child count, concurrency, deadline, budget, and authority.
- Suspend and resume the parent without holding a worker.
- Expose child Tasks and decisions in the existing Task detail experience.
- Run the comparative evaluation before expanding the feature.

This phase proves runtime value. It does not promise an Assistant product.

### 13.3 Phase 2: Product Projection If Evidence Is Positive

- Present qualifying delegation-enabled Agents as Assistants.
- Test the `AI Space` grouping and `Specialist` vocabulary.
- Let a Space select a default Assistant for Home.
- Add semantic roster and policy authoring without a graph editor.
- Preserve direct execution for ordinary Agents and simple work.

### 13.4 Phase 3: Revisit Workflow Scope

Use observed work rather than forecasts to decide whether to:

- continue the full static graph phases in the accepted Workflow design;
- narrow the primary Portal concept and label to Automation;
- permit an Automation to invoke one Assistant node;
- generate reviewable Automation drafts from repeated Assistant traces; or
- keep Workflow internal until a trigger or governance use case earns a
  dedicated user surface.

Any accepted change updates the Workflow design, product vision, roadmap, and
Portal terminology together. This proposal is then retired.

## 14. Goals And Non-Goals

### 14.1 Goals

- Determine whether bounded delegation has durable product value over one
  strong Agent.
- Define a safe experiment that reuses Task and TaskRun rather than inventing
  another execution plane.
- Separate adaptive semantic planning from scheduling and policy authority.
- Clarify the lasting role of Workflow if adaptive work becomes Agent-led.
- Prevent Portal navigation from exposing an incoherent Agent, Assistant, and
  Workflow taxonomy.
- Establish evidence that can change roadmap priority responsibly.

### 14.2 Non-Goals

- Declaring multi-Agent execution inherently better than one Agent.
- Renaming Portal navigation before usability evidence.
- Replacing the accepted Workflow runtime design in this paper.
- Allowing arbitrary Agent discovery, recursive delegation, or model-granted
  permissions.
- Building a new Assistant execution loop, scheduler, trace, or Artifact store.
- Treating a successful run trace as a safe executable definition.
- Adding a general visual DAG editor. (The static-DAG editor that has since
  shipped is governed by
  [its own design record](../design/portal-workflow-visual-editor.md), not
  this paper.)
- Claiming that stronger future models remove the need for durable execution,
  authorization, or audit.

## 15. Open Questions

1. Does a separate child Task preserve enough parent context while maintaining
   a useful trust and context boundary?
2. Should the parent Task enter a new durable waiting state, or can waiting be
   represented without expanding the public Task state machine?
3. What minimum structured input and result contract is necessary before a
   Space Agent can appear in a delegate catalog?
4. Can a child Task continue independently when its parent is canceled, or
   must cancellation always propagate downward?
5. Who may add a privileged Specialist to a coordinating Agent revision, and
   what publication review is required?
6. Is `Assistant` clearer than `Space`, `Coordinator`, or simply a named Agent
   in user research?
7. Is `Automation` clearer than `Workflow` once the latter may invoke an
   adaptive Assistant?
8. Which real workloads show quality, latency, permission, or context benefits
   large enough to justify delegation?
9. Does a stable pattern extracted from Assistant traces become a Workflow,
   an Agent skill, or neither?
10. Which parts of the accepted Workflow reconciler can coordinate dynamic
    child Tasks without merging Assistant and Workflow semantics?
11. Should every published Workflow be visible to the foreground conversation
    agent, or should a Space curate which Workflows the conversation may list
    and start (section 9.5)?
12. Does returning a Workflow's full input schema inline scale for listing, or
    should the surface return a compact descriptor and fetch the schema
    separately when the agent commits to a run (section 9.5)?

## 16. Likely Destination

If evidence supports bounded delegation, the durable decisions should be split
across existing semantic records rather than preserved as one cross-cutting
paper:

- Agent roster, revision pinning, parent-child Task ownership, and result
  propagation belong in
  [Agent execution and Task threads](../design/agent-execution-and-task-threads.md).
- The user-facing role of Assistant, AI Space, and Home belongs in
  [product vision](../design/product-vision.md) and the relevant Portal design.
- The deterministic Automation boundary, Assistant node contract, and delivery
  sequence belong in [Workflow runtime](../design/workflow-runtime.md).
- Accepted priority belongs only in [the roadmap](../ROADMAP.md).

If bounded delegation does not beat one strong Agent, retire this proposal and
retain the simpler product: direct Agent execution for adaptive work and
Workflow for deterministic automation. That outcome is a valid design result,
not a failed implementation.
