# Agent Runtime Introspection And Context Control

> **简体中文：** [阅读中文镜像](../zh-CN/proposals/agent-runtime-introspection-and-control.md)
>
> **Audience:** contributors and product reviewers · **Status:** proposal — under discussion
>
> **Opened:** 2026-09-13

Related: [roadmap](../ROADMAP.md), [current state](../current-state.md),
[Agent Loop](../contribute/architecture/agent-loop.md),
[tools](../contribute/architecture/tools.md),
[context durability](../design/context-durability.md),
[session usage statistics](../design/session-usage-stats.md), and
[durable run traces](../design/durable-run-trace.md).

## Contents

- [1. Question And Provisional Recommendation](#1-question-and-provisional-recommendation)
- [2. User Outcome And Current Evidence](#2-user-outcome-and-current-evidence)
- [3. First-Principles Boundary](#3-first-principles-boundary)
- [4. Goals And Non-Goals](#4-goals-and-non-goals)
- [5. Candidate Tool Contract](#5-candidate-tool-contract)
- [6. Context Accounting](#6-context-accounting)
- [7. Safe Compaction Semantics](#7-safe-compaction-semantics)
- [8. State, Concurrency, And Layering](#8-state-concurrency-and-layering)
- [9. Surface, Permission, And Security Boundaries](#9-surface-permission-and-security-boundaries)
- [10. Options And Trade-Offs](#10-options-and-trade-offs)
- [11. Failure Modes](#11-failure-modes)
- [12. Evidence Program And Staged Delivery](#12-evidence-program-and-staged-delivery)
- [13. Open Questions](#13-open-questions)
- [14. Likely Destination If Accepted](#14-likely-destination-if-accepted)

## 1. Question And Provisional Recommendation

A long-running Agent can inspect files, external systems, jobs, and delegated
work, but it cannot directly ask the runtime a simpler set of questions about
itself: how much context is occupied, how long this run has been active, how
much iteration budget remains, whether compaction is available, or which parts
of its own task list are complete. The user-facing surfaces can answer much of
this through `buildmax info`, TUI and Desktop `/info`, traces, and `/compact`;
the Agent making the next decision cannot.

The question is whether BuildMax should expose bounded runtime introspection and
context control to the model, and where authority should remain when it does.

The provisional recommendation is:

1. Add one read-only `RuntimeStatus` tool that returns a small, explicitly
   qualified snapshot of the current run, context, budget, and existing Todo
   state.
2. Evaluate a separate `ContextCompact` tool whose call requests compaction;
   the Agent Loop performs the operation only at a safe iteration boundary.
3. Keep automatic compaction authoritative. Agent-requested compaction is an
   optimization hint, never the mechanism that prevents context exhaustion.
4. Reuse existing runtime events, `RunStats`, Todo state, compaction, and trace
   facts. Do not introduce a second run ledger, task list, or control plane.
5. Register only capabilities the surface can actually serve, and never expose
   raw prompts, memory bodies, secrets, or unrestricted trace content through
   the status tool.

This paper proposes a direction to test, not shipped behavior or roadmap work.

## 2. User Outcome And Current Evidence

The essential user outcome is not “the Agent can print diagnostics.” It is:

> A long-running Agent can adapt its strategy before it runs out of time,
> iterations, or usable context, without being granted authority over runtime
> correctness or private operational data.

That matters in several ordinary situations:

- An Agent exploring a large repository should stop broad discovery and start
  synthesis when little iteration budget remains.
- Before voluntarily discarding detailed history, it should checkpoint durable
  decisions and confirm that compaction will recover useful room.
- After a long sequence of tool calls, it should distinguish current context
  occupancy from cumulative tokens already paid for.
- A resumed Session should be able to discover that completed Todo entries
  already exist instead of repeating work whose original tool result was
  compacted away.
- A supervisor prompt or evaluation should be able to require “inspect your
  remaining budget before expanding scope” using a real runtime fact rather
  than a guess based on message length.

BuildMax already holds most of the required facts:

| Existing source | Facts already available | Current consumer |
|---|---|---|
| Agent Loop iteration | current and maximum iterations | logs and loop control |
| `EventLLMStart` | estimated context tokens and context window | trace and interactive status |
| `RunStats` | tool calls, prompt/completion/cache tokens, cost | run result and trace |
| run start time | turn wall duration | run result and trace summaries |
| durable Todo state | pending, in-progress, and completed entries | the model's resident session-state block and user surfaces |
| `Compact` / `compactOnce` | automatic and on-demand compaction | Agent Loop and TUI `/compact` |

The gap is therefore an access path and an authority boundary, not a missing
telemetry subsystem.

## 3. First-Principles Boundary

Three concerns must stay separate:

| Concern | Question answered | Authority |
|---|---|---|
| Observation | What is true about this run now? | Runtime measures; Agent reads |
| Semantic adaptation | Given that state, what work is still worthwhile? | Agent decides |
| Lifecycle correctness | When may history change, a run stop, or a limit be enforced? | Runtime decides and records |

The Agent is well placed to change semantic strategy. It is not the authority
for token accounting, history integrity, cancellation, quota, or TaskRun state.
Giving it observations does not require handing it those transitions.

“Current state” also has several lifetimes that must not be flattened:

- **Run state** begins when one `RunLoop` call starts: elapsed time, iteration,
  this run's model and tool calls.
- **Session state** survives turns: cumulative usage, notes, Todos, and the
  compaction boundary.
- **Task and TaskRun state** is the durable Server execution plane. A Todo is
  not a Task, and “completed Todos” must not be reported as completed TaskRuns.
- **Process and deployment state** belongs to operator diagnostics and is not
  automatically safe or useful in the model context.

The proposed tools name and preserve these boundaries.

## 4. Goals And Non-Goals

Goals:

- Let an Agent observe the smallest set of facts that can materially improve
  decisions during a long run.
- Make every number's scope, freshness, and precision legible.
- Let an Agent request an early compaction without mutating history from inside
  a tool worker.
- Keep behavior consistent across CLI, TUI, Desktop, worker, Conversation, and
  subagent loops wherever the underlying capability exists.
- Preserve automatic limits, hooks, tracing, metering, and compaction
  persistence as the authoritative mechanisms.
- Measure whether the capability improves outcomes enough to justify its tool
  schema and extra model calls.

Non-goals:

- A general runtime administration API for the model.
- Letting the Agent increase its context window, iteration cap, deadline,
  quota, permissions, or sandbox authority.
- Returning environment variables, raw system prompts, Secret material,
  memory bodies, full message history, or arbitrary trace records.
- Creating a second Todo or Task representation called “processed tasks.”
- Replacing automatic compaction with model discipline.
- Promising exact provider tokenization when the provider does not expose a
  compatible preflight tokenizer.
- Persisting rapidly changing status snapshots as new Session or Server
  entities.

## 5. Candidate Tool Contract

### 5.1 `RuntimeStatus`

`RuntimeStatus` is a read-only tool. Its default call takes no arguments and
returns a compact summary. An optional `detail: "tasks"` includes the bounded
existing Todo entries when the Agent needs to inspect completed work; it does
not query Server Tasks.

Candidate result shape:

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

The exact JSON is not decided by this proposal. The required semantic
properties are:

- integer source values are returned alongside, not replaced by, a rounded
  percentage;
- context precision says `estimated`, `provider_reported`, or `unavailable`;
- a snapshot names the iteration at which context was measured;
- run and Session totals are not blended;
- absent facts are reported as unavailable, not as zero;
- task detail reuses the bounded Todo store and clearly labels it `todos`.

The result should be written for the model, with a short actionable sentence
after the structured facts when a limit is close. It must not prescribe work
when the runtime has no evidence that the threshold is relevant.

### 5.2 `ContextCompact`

`ContextCompact` takes no tuning parameters. The Agent may explain its own
reasoning before the call, but the runtime does not need a free-text `reason`
field to perform or audit the transition. The tool returns one of three
outcomes:

- compaction was scheduled for the next safe boundary;
- a request is already pending; or
- compaction is unavailable or would summarize nothing useful.

It does not accept target token counts, arbitrary message ranges, replacement
summaries, or a “force” flag. Those inputs would let the least reliable part of
the loop redefine history integrity and duplicate decisions already owned by
`splitForCompaction`, the compactor, and `CompactionHistory`.

The subsequent boundary emits the existing compaction event and uses the same
metering, checkpoint, hook, summary clamp, and durable commit path as automatic
and user-requested compaction.

## 6. Context Accounting

Context occupancy is not cumulative prompt usage. A run may have sent 500,000
prompt tokens across cached iterations while its current request occupies
60,000 tokens. `RuntimeStatus` must never label the first number “context
used.”

The first implementation should establish one authoritative request estimator
used by Agent events, `AgentApp.EstimateRunUsage`, user-facing status, and the
new tool. It should count every request component BuildMax can observe:

- effective system prompt and compaction block;
- model-visible history after safe trimming;
- resident memory index and Session state;
- tool definitions and their schemas; and
- provider framing overhead represented by the estimator.

Where exact tokenization is unavailable, the result remains an estimate even
if the context window itself is exact. Provider-reported usage from the last
call is accounting evidence, but it may include provider-specific caching or
hidden framing and must not silently replace a differently scoped estimator.

A status tool runs after the model has already received the current request.
Its last prepared request count is therefore necessarily a snapshot, and its
own tool result will make the next request slightly larger. The contract must
say this rather than claiming an impossible perfectly live count. A future
projected-next-request field is useful only if it can avoid a circular estimate
of its own output.

## 7. Safe Compaction Semantics

A normal tool executes while an assistant tool-call group is still being
completed:

```text
assistant(tool_calls)
  -> gate calls
  -> execute calls, possibly in parallel
  -> append every tool result in call order
  -> next Agent Loop iteration
```

Changing the compaction boundary inside `ContextCompact.Execute` could split
the assistant/tool relationship, race sibling read-only tools, or make the
persisted history disagree with the history the loop continues to use.

The safe candidate flow is:

```text
Agent calls ContextCompact
  -> tool records one pending request and returns
  -> the whole tool-call batch is committed
  -> loop reaches the post-batch boundary
  -> requested compaction runs through compactOnce
  -> queued user input is injected
  -> next model request is built from the committed compacted view
```

The order relative to queued input is deliberate. Input that arrived while the
previous tool batch ran should remain a fresh user instruction, not be
immediately folded into a lossy summary because the Agent requested compaction
before seeing it.

The current assistant/tool group must remain a valid atomic tail even when it
alone exceeds the normal manual reserve. Compaction failure follows existing
semantics: a summary failure leaves history intact and is reported to the
Agent; failure to persist a produced boundary is fatal because continuing
would make the in-memory and durable views disagree. A `PreCompact` hook may
still block the operation.

Automatic compaction runs independently at the existing threshold. A pending
request that reaches a boundary where automatic compaction already ran is
satisfied by that pass rather than causing a second one.

## 8. State, Concurrency, And Layering

The tool registry is cached and shared, so neither proposed tool may retain a
Session or run pointer in its struct. The existing Note and memory tools show
the appropriate shape: `internal/core/agent` defines a small runtime-facing
interface and carries the current run handle through `context.Context`;
`internal/tool` adapts that handle to LLM tool definitions; callers assemble
the handle for each run.

The handle needs only bounded ephemeral state:

- a monotonic run start time;
- current iteration and maximum;
- this-run counters already held by `RunStats`;
- the latest prepared-request context snapshot;
- compaction count and pending-request bit; and
- access to the current run's existing Todo store where available.

It does not own those facts. The Agent Loop remains the writer and updates the
snapshot at the same transitions that emit events. This avoids an event
consumer becoming a new source of truth.

`RuntimeStatus` declares read-only access and must be safe when called beside
other read-only tools. Its snapshot therefore needs a mutex or immutable
atomic replacement. `ContextCompact` declares write access so it is a
scheduling barrier even though its immediate mutation is only a pending bit.
The actual history mutation stays on the loop goroutine.

Conversation and subagent loops may use the same core contract. A surface with
no compactor should still be able to expose status while omitting
`ContextCompact` or reporting it unavailable; registering a callable control
that can never succeed would teach the model a false capability.

## 9. Surface, Permission, And Security Boundaries

The status tool is about the current run only. It does not accept Session,
Task, TaskRun, user, or Space identifiers. Scope comes from the run context, so
the model cannot use it to enumerate neighboring work.

Candidate permission behavior:

| Tool | Access | Default | Reason |
|---|---|---|---|
| `RuntimeStatus` | read-only | allow | bounded facts about the calling run; safe for parallel reads |
| `ContextCompact` | write | allow, subject to policy and `PreCompact` | changes only model-visible scratch history through an operation the runtime already performs automatically |

The default for `ContextCompact` remains an open decision because early
compaction is lossy and incurs a model call. Requiring interactive approval
would make the tool unusable on workers; unconditional permission without a
runtime floor could invite waste. One candidate is to allow requests only when
the history has enough summarizable material to recover a configured minimum
amount of context, returning a no-op otherwise. That is a runtime invariant,
not a prompt instruction.

Status output must exclude:

- raw instructions, compaction summaries, notes, memory bodies, and message
  contents;
- Secret values and environment variables;
- filesystem paths not already model-visible;
- other runs' activity, budgets, identities, or costs; and
- process-wide health details that belong to operator diagnostics.

Trace records should capture that status or compaction was requested through
the ordinary tool events. They should not duplicate the returned snapshot into
a second unredacted diagnostic format.

## 10. Options And Trade-Offs

| Option | Benefit | Cost or failure |
|---|---|---|
| A. Keep status user-only; automatic compaction only | no new tool schema or model behavior | Agent continues guessing about remaining budget and cannot deliberately compact before a phase change |
| B. Inject status into every model request | no tool call needed; always visible | permanently consumes context and attention, rapidly changes a cacheable prefix, and reports facts the Agent often does not need |
| C. One `Runtime` tool with `status`, `compact`, and future actions | one visible tool name | mixes read and write permissions, encourages a runtime control grab bag, and makes parallel scheduling argument-dependent |
| D. Separate `RuntimeStatus` and boundary-scheduled `ContextCompact` | clear authority, permissions, and on-demand cost | two schemas and a new per-run control handle |
| E. `RuntimeStatus` first; keep compaction runtime-only | validates the observation value with the smallest mutation surface | cannot test whether phase-aware early compaction helps |

The recommended experiment is E followed by D only when status usage shows
that the Agent can act sensibly on the facts. This sequencing is intentionally
more conservative than treating both tools as one indivisible feature.

## 11. Failure Modes

| Failure | Required response |
|---|---|
| Model polls status every iteration | tool description discourages polling; loop guard still applies; evaluation measures schema overhead and call frequency |
| Estimated context appears exact | always return precision and measurement iteration; UI and tool use the same terminology |
| Status result itself increases context | document snapshot semantics; keep the result small and omit task detail by default |
| Agent compacts too early and loses evidence | runtime minimum useful-recovery floor; checkpoint first; no force or custom-range argument |
| Compaction requested during a parallel batch | request is only a synchronized flag; mutation occurs after the complete batch commits |
| Queued user input is summarized before the Agent sees it | run requested compaction before pending-input injection |
| Hook blocks compaction | return a bounded, actionable refusal and leave history unchanged |
| Compactor or persistence fails | preserve existing `compactOnce` error semantics; never claim space was recovered when it was not |
| Todo counts are mistaken for Server work | label them Todo state; expose no Task/TaskRun list through this tool |
| Status leaks another Session | derive scope only from the per-run context; accept no caller-selected identifiers |
| Surface has no compactor or context window | omit control or report unavailable; do not invent zero values |

## 12. Evidence Program And Staged Delivery

Acceptance should depend on behavior, not on the tools merely returning JSON.

### Phase 0 — unify observation

- Define the authoritative context-estimation input and reconcile differences
  among `EventLLMStart`, `EstimateRunUsage`, traces, and UI status.
- Add deterministic tests covering system prompt, compaction summary, resident
  state, history, tool schemas, and unavailable context windows.
- Record a baseline of long scripted runs without an LLM-facing status tool.

### Phase 1 — read-only prototype

- Add `RuntimeStatus` through a per-run context-carried handle.
- Cover concurrent reads, cancellation, absent Todo state, resumed Sessions,
  subagents, Conversation history, and worker assembly.
- Run scripted and real-model tasks that require a phase change under a tight
  iteration or context budget.

Evidence needed:

- completion rate and repeated-work rate versus the baseline;
- number of status calls and tokens added by its definition and results;
- whether small and large models distinguish context occupancy from cumulative
  usage; and
- whether the Agent changes behavior at useful times rather than narrating the
  numbers.

### Phase 2 — boundary-scheduled compaction prototype

- Add the pending request and post-batch safe point using the existing
  `compactOnce` implementation.
- Test multiple calls in one batch, concurrent sibling tools, queued input,
  automatic/requested collision, hook denial, summarizer failure, persistence
  failure, and cancellation.
- Compare tasks with automatic compaction only against tasks allowed to request
  phase-aware compaction.

Evidence needed:

- fewer context-limit failures or repeated investigations;
- no message-pairing or durable-boundary corruption;
- recovered context and added compaction cost;
- rate of premature or ineffective compactions; and
- whether checkpointed notes and Todos preserve the facts later phases need.

### Phase 3 — decision

Accept the status tool only if it improves decisions enough to pay for its
resident schema and calls. Accept Agent-requested compaction separately only if
it improves outcomes over the existing automatic policy without unacceptable
information loss or cost. A negative result leaves the user-facing diagnostic
and automatic compaction paths unchanged.

## 13. Open Questions

1. Should `RuntimeStatus` be available to every subagent, or only when its
   iteration/context budget is large enough for the information to matter?
2. Is the latest prepared-request snapshot sufficient, or do models need a
   separately labelled projection of the next request?
3. Should optional task detail list all completed Todos, only counts, or the
   most recent bounded subset? What demonstrated decision requires the text?
4. What minimum summarizable tokens or recoverable share should permit an
   Agent-requested compaction?
5. Should `ContextCompact` default to allow on unattended workers, or must an
   Agent definition explicitly opt in because the operation is lossy and
   billable?
6. Does elapsed time improve model behavior when the run has no deadline, or
   should status expose a deadline/remaining duration only where one exists?
7. Which cumulative Session usage facts, if any, materially affect an Agent's
   in-run choices rather than merely satisfying curiosity?
8. Should a near-limit status response contain an advisory sentence, or are
   raw facts less likely to oversteer the model?

## 14. Likely Destination If Accepted

This proposal's primary domain is **Agent Runtime and Models**.

If accepted:

- the durable authority and safe-boundary rationale belongs in
  [context durability](../design/context-durability.md) or a focused design
  record if that document would become less coherent;
- [Agent Loop](../contribute/architecture/agent-loop.md) documents the live
  snapshot and post-batch control boundary;
- [tools](../contribute/architecture/tools.md), the English and Chinese user
  manuals, and permission documentation describe the shipped tools exactly;
- the accepted priority enters [ROADMAP.md](../ROADMAP.md) and decomposed work
  enters the backlog; and
- this proposal is deleted once the decision and durable rationale have moved
  to their authoritative homes.
