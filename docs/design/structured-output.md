# Provider-Neutral Structured Output

> **简体中文：** [阅读中文镜像](../zh-CN/design/结构化输出.md)

> **Audience:** contributors, product designers, and operators · **Status:** accepted — Phases 1-3 in progress: the runtime contract, all provider mappings (except the deferred prompted fallback), and the run boundary. Recorded as an R5 prerequisite by [orchestration and continuity decisions](orchestration-and-continuity-decisions.md) §7 and named in [`ROADMAP.md`](../ROADMAP.md) R5; this record is its design.

Related: [workflow runtime](workflow-runtime.md),
[agent execution and Task threads](agent-execution-and-task-threads.md),
[LLM gateway](llm-gateway.md),
[LLM provider adapters](llm-provider-adapters.md),
[data model](../contribute/architecture/data-model.md), and
[orchestration and continuity decisions](orchestration-and-continuity-decisions.md).

Created: 2026-09-06

## Contents

- [1. Decision](#1-decision)
- [2. Problem And Current Baseline](#2-problem-and-current-baseline)
- [3. Goals And Non-Goals](#3-goals-and-non-goals)
- [4. The Contract](#4-the-contract)
- [5. Structured Output Is The Run's Final Value](#5-structured-output-is-the-runs-final-value)
- [6. The Schema Subset](#6-the-schema-subset)
- [7. Provider Mapping](#7-provider-mapping)
- [8. Capability Honesty And Fallback](#8-capability-honesty-and-fallback)
- [9. Validation And Failure](#9-validation-and-failure)
- [10. Streaming](#10-streaming)
- [11. Where It Surfaces](#11-where-it-surfaces)
- [12. Delivery Plan](#12-delivery-plan)
- [13. Verification](#13-verification)
- [14. Alternatives Considered](#14-alternatives-considered)
- [15. Open Questions](#15-open-questions)

## 1. Decision

The shared Agent runtime gains a **provider-neutral structured-output contract**:
a caller may declare a JSON Schema for a run's final answer, and the runtime
returns a value that has been **validated against that schema** or a typed
failure. The schema and the validated value travel through one interface
(`internal/core/llm`) regardless of which provider served the call; each adapter
maps the contract to its provider's native mechanism, and where a provider has
none, the runtime falls back and **records which mechanism actually applied**.

Structured output is **additive to the run's text output, not a replacement for
it**. A run still produces the whole assistant turn as text (see
[agent execution — the run output is the whole turn](agent-execution-and-task-threads.md));
the structured value is a separate, machine-readable field alongside it. The
human reads the text; a Workflow route, a planner, or an evaluator reads the
structured value.

The boundary is the same one Workflow already draws: **the model proposes a
value; the runtime validates it against the declared schema and records it.** A
value that does not validate is not silently accepted.

This unblocks what several accepted designs are waiting on: Workflow's typed
routes, bounded planners, and evaluator loops
([workflow runtime §13](workflow-runtime.md)), whose node output envelope carries
a `structured` field that is `null` until this contract exists
([workflow runtime §9](workflow-runtime.md)), and a richer Task result than free
text.

## 2. Problem And Current Baseline

The shared runtime cannot ask a model for a machine-checkable answer.

- `internal/core/llm.Request` carries `Messages`, `Tools`, `Profile`, and
  `CacheScope` — no output schema.
- `internal/core/llm.Completion` carries `Content`, `ToolCalls`, `Usage`, and
  `ProviderState` — no structured value.
- No adapter (`anthropic.go`, `ollama.go`, `openai_chat.go`,
  `openai_responses.go`) builds a `response_format`, a forced `tool_choice`, a
  `format`, or any JSON-mode field. A grep for those keywords across the LLM
  packages returns nothing.

So every answer is free text. A consumer that needs a value parses the text and
hopes. Workflow's adaptive tier is designed around a `structured` field it can
trust, and it stays `null`; a Task result cannot carry a typed outcome. This is
the one missing primitive under both the adaptive-Workflow and the
richer-result-envelope directions.

## 3. Goals And Non-Goals

### 3.1 Goals

- One provider-neutral way to request a schema-constrained final value and
  receive a validated one or a typed failure.
- Adapters map the contract to each provider's native mechanism; the caller does
  not branch on provider.
- Honest capability reporting: the run records whether the schema was enforced
  natively, coerced through a forced tool, or only prompted-and-parsed — never
  a claim of enforcement that did not happen.
- The structured value is additive to the text output and does not suppress it.
- A supported, documented JSON Schema subset, shared with Workflow's existing
  input-schema subset so a definition validates once and means the same thing at
  both boundaries.
- Validation lives in the runtime, once, so a Portal parser or a Workflow
  handler never re-implements it.

### 3.2 Non-Goals

- A general grammar/regex constrained-decoding engine. The contract is JSON
  Schema (a subset), not arbitrary CFG output.
- Structured output on *every* intermediate LLM call. The contract constrains a
  run's **final** answer, not the tool-calling turns on the way there (§5).
- Streaming a partially-built structured object to the UI as it forms (§10).
- Replacing tool calling. Tools remain how a model acts; structured output is
  how a run reports (§14.1).
- Inventing a schema language. JSON Schema (subset) is the contract.
- A per-provider feature matrix in this record; capabilities are discovered and
  recorded at run time (§8), not frozen in a document.

## 4. The Contract

Two additions to `internal/core/llm`, provider-neutral:

```go
// On Request: an optional schema the final answer must satisfy.
type OutputSchema struct {
    Name   string          // a stable name for the schema (some providers require one)
    Schema json.RawMessage // the JSON Schema, in the supported subset (§6)
}

type Request struct {
    Messages   []Message
    Tools      []ToolDef
    Profile    CallProfile
    CacheScope string
    Output     *OutputSchema // nil = free text, today's behavior
}

// On Completion: the validated value plus how it was obtained.
type Structured struct {
    Value    json.RawMessage       // validated against the schema, or nil on failure
    Mode     StructuredMode        // native | forced_tool | prompted (§7, §8)
    Enforced bool                  // true only when the provider guaranteed the shape
    Err      *StructuredError      // set when the model's value did not validate
}

type Completion struct {
    Content       string
    ToolCalls     []ToolCall
    Usage         Usage
    ProviderState *ProviderState
    Structured    *Structured // set only when the Request asked for output
}
```

`Content` (the text) is unchanged and still produced. `Structured.Value` is the
machine-readable answer. A caller that asked for output reads `Structured`; a
caller that did not is unaffected — `Output` nil is exactly today's behavior, so
the change is additive and every existing call site keeps working.

The `LLMClient` interface signatures do not change: `Request` and `Completion`
grow fields. `ContextWindow` is untouched.

## 5. Structured Output Is The Run's Final Value

An agent run is a loop: the model calls tools, reads results, calls more tools,
and eventually writes a final answer. Structured output constrains **that final
answer**, not the intermediate turns — a turn that calls a tool is acting, not
reporting, and forcing a schema onto it would fight the tool call.

So the contract is a property the **run** carries, resolved by `RunLoop`:

- while the model is still calling tools, calls proceed as today (tools, free
  text narration — which the run keeps, per the run-output-is-the-whole-turn
  decision);
- when the model produces a terminating answer (no tool calls) and the run
  requested output, the runtime constrains that terminating answer to the schema
  and validates it.

Concretely, `RunLoopOpts` gains an optional `Output *llm.OutputSchema`. `RunLoop`
applies it to the call that could terminate the loop and returns the validated
`Structured` alongside the text reply. The text reply remains the whole turn;
the structured value is the final answer in machine form.

This keeps structured output orthogonal to tool use and means one run yields
both a human-readable transcript and one typed result.

## 6. The Schema Subset

The contract accepts a **single named JSON Schema subset that both this contract
and Workflow's `input_schema` reference** ([workflow runtime §6.1](workflow-runtime.md)),
defined once so a definition validates identically at both boundaries and cannot
drift between them (§15 D1). Neither boundary has implemented its subset yet, so
this is one shared definition, not two byte-identical copies. A schema authored
for a Workflow node's `output_schema` validates identically here. Publication of
a Workflow that declares an output schema fails if the schema leaves the subset,
exactly as an unsupported input schema does today.

The subset is documented with the API when implemented. It is deliberately
narrow first — objects, a fixed set of scalar types, enums, arrays, `required`,
and `additionalProperties: false` — because every keyword must be expressible in
every provider's native mechanism *and* checkable by the runtime's own
validator. A keyword that only one provider supports is not in the subset; the
runtime is the floor, not any single provider's ceiling.

## 7. Provider Mapping

Each adapter maps `Output` to its provider's native mechanism and reports the
`Mode` it used:

| Provider family | Native mechanism | `Mode` |
|---|---|---|
| OpenAI (chat and responses) | `response_format: {type: "json_schema", …, strict: true}` | `native` |
| Anthropic | a single forced tool whose input schema is the output schema, `tool_choice` pinned to it; the tool input is the value | `forced_tool` |
| Ollama | `format` set to the JSON Schema (or `json` when only object-ness is needed) | `native` where the model honors it, else `prompted` |
| any without a usable mechanism | schema rendered into the prompt, output parsed | `prompted` |

The adapter is the only place that knows the provider mechanism. Above it, a
caller sees `Structured` with a `Mode` and an `Enforced` flag and never branches
on provider. The forced-tool mapping for Anthropic reuses the existing tool
plumbing (`toolcalls.go`): the runtime adds one synthetic tool, forces it, and
reads its arguments as the value — invisible to the caller, who asked only for a
schema.

## 8. Capability Honesty And Fallback

Not every model enforces a schema. The runtime must not claim it did. This
follows the project's trust-boundary rule: **record the boundary that actually
applied**, never an implied guarantee ([product vision — trust boundaries are
visible](product-vision.md)).

- `Structured.Mode` records the mechanism; `Structured.Enforced` is true only
  when the provider guaranteed the shape (`native`, and `forced_tool` for a
  provider that validates tool input against its schema).
- A `prompted` fallback is best-effort: the runtime still validates the parsed
  value against the schema itself, so a bad value is a typed failure (§9) rather
  than silent garbage — but `Enforced` is false, and that reaches the trace and
  any consumer that cares.
- A caller that requires enforcement (a Workflow route whose branch selection
  must be trustworthy) can demand `Enforced` and treat `prompted` as a failure.
  This is a policy the consumer sets, not a default the runtime hides.

A provider/model's structured-output capability is discovered from the adapter
and the model record, not frozen in this document — the feature matrix moves too
fast to hard-code, and §2's grep already shows there is nothing to preserve.

## 9. Validation And Failure

The runtime owns validation, once:

1. the adapter returns the model's candidate value (from `response_format`, the
   forced tool's arguments, or a parse of prompted text);
2. the runtime validates it against the `OutputSchema` with its own validator —
   even for `native` mode, so a provider bug cannot smuggle an off-schema value
   past the contract; and
3. on success `Structured.Value` is the validated value; on failure
   `Structured.Err` is a typed error and `Value` is nil.

Failure handling is the **consumer's** policy, and it matches Workflow's:
[workflow runtime §13.1](workflow-runtime.md) already says invalid structured
output follows the node's explicit retry policy and otherwise fails the node,
and its verification pins that "no undeclared edge executes" on invalid output.
This record supplies the validated value and the typed failure; Workflow decides
retry-versus-fail. A direct Agent run surfaces the failure on its TaskRun; it
does not pretend a value exists.

The model proposes; the runtime validates and records. Nothing downstream
re-validates or re-parses.

## 10. Streaming

Structured output and token streaming coexist without streaming a half-built
object as "the value":

- the run still streams its **text** deltas live, as today — the narration a
  run produces on the way to its answer is text and streams normally;
- the **structured value** is resolved at the terminating answer and delivered
  once, validated, when the run completes. A partially-formed JSON object is
  never presented as the value.

So the Task page keeps its live text stream (the streamed narration), and the
structured result appears when the run finishes — the same moment the text
settles. No new streaming protocol is introduced.

## 11. Where It Surfaces

- **Agent runtime:** `RunLoopOpts.Output` requests it; `RunLoop` returns the
  validated `Structured` beside the text reply (§5).
- **TaskRun:** the run's output envelope gains a `structured` field beside the
  text output. This is the `structured` that [workflow runtime §9](workflow-runtime.md)
  reserves; it is `null` for a run that did not request output. The `xxxRow`
  structs in `internal/infra/db` remain the schema source of truth; a nullable
  structured column is added when this lands.
- **Workflow:** an `agent_task` node's `output_schema` becomes a real
  constraint. The runtime validates the node's structured output before the node
  succeeds, and a typed route or planner reads `/structured/...` from the
  envelope ([workflow runtime §13.1–§13.2](workflow-runtime.md)). No Portal
  parser validates; the runtime already did.
- **Task result envelope:** a Task result may carry the structured value in
  addition to its text, so a caller that wanted a typed outcome gets one and a
  human still gets the readable answer.

## 12. Delivery Plan

BuildMax is Alpha; each phase changes the `llm` types, the adapters, and the
consumers together, with no compatibility interpreter for the old shapes.

**Phase 1 — the runtime contract.** Add `OutputSchema`/`Structured` to
`internal/core/llm`, the runtime validator over the §6 subset, and the OpenAI `native` mapping in the two OpenAI adapters. Prove request→validated-value on a
scripted provider. No consumer yet; `Output` nil is unchanged behavior.

**Phase 2 — the other providers.** Anthropic `forced_tool` and Ollama `format`,
with honest `Mode`/`Enforced` reporting. The `prompted` fallback is **deferred**:
all four current providers have a native mechanism, so nothing exercises it, and
its trigger (a provider with no mechanism, or capability detection for an
OpenAI-compatible endpoint that rejects `response_format`) is its own unresolved
design. It stays the documented floor (§7, §14.2) and is added when a consumer
needs it, per Occam's razor. A model with no mechanism will then validate against
the runtime's own validator.

**Phase 3 — the run boundary.** `RunLoopOpts.Output` (and `RunPromptOpts.Output`
above it), applied to the terminating answer by re-issuing one constrained call
once the model produces a no-tool-call answer (§5), returning the validated
`Structured` beside the reply on `RunResult`. Re-issue on termination rather than
sending `Output` on every turn is what keeps structured output from fighting tool
use on a provider that maps it to a forced tool: the loop runs free, and only the
settled answer is rendered as the value.

The **TaskRun structured field and its persistence move to Phase 4**: nothing
sets `Output` on a TaskRun until the Workflow consumer does, so persisting the
value belongs with the producer that writes it rather than as an always-null
column added ahead of need (Occam's razor). Phase 3 is the reusable run-level
primitive; a direct caller that sets `RunPromptOpts.Output` already receives the
value on `RunResult.Structured`.

**Phase 4 — the consumers.** Workflow `output_schema` enforcement, typed routes,
and planner/map reading `/structured/...`; the TaskRun structured column and its
persistence; the Task result envelope's structured field. This is the R5 Workflow
work this record unblocks, and it lands the persistence beside the first producer.

Phases 1–3 are the shared primitive; Phase 4 is its first real consumer and
lands with the Workflow slices that need it, including where the value is stored.

## 13. Verification

- **Runtime validator:** the §6 subset — objects, scalars, enums, arrays,
  `required`, `additionalProperties: false` — accepts valid values and rejects
  each violation with a typed error; a `native` provider's value is still
  re-validated.
- **Adapter mapping (scripted provider):** each `Mode` is exercised —
  `native` request carries `response_format`; `forced_tool` adds and forces one
  synthetic tool and reads its arguments; `prompted` renders the schema and
  parses; each reports the right `Mode`/`Enforced`.
- **Run boundary:** a run that calls tools then returns a schema-constrained
  answer yields both the whole-turn text and a validated `Structured`; an
  off-schema answer yields a typed failure, not a value.
- **Consumer:** a Workflow node with an `output_schema` succeeds only on a
  validated value; an invalid value follows the node's retry/fail policy and no
  undeclared route edge executes (the existing
  [workflow verification](workflow-runtime.md) case, now real).
- **Enforcement honesty:** a `prompted` run records `Enforced=false`, and a
  consumer that demands enforcement treats it as a failure.
- Real-model behavior (how reliably a given model honors a schema) is measured
  by evaluation, never asserted by a deterministic test.

## 14. Alternatives Considered

### 14.1 Reuse tool calling instead of a distinct contract

A caller could define a tool whose input is the schema and read its arguments —
which is exactly the Anthropic *mapping* (§7). Rejected as the *contract*
because it leaks provider mechanism into every caller, conflates "the model
acts" with "the run reports", and gives OpenAI/Ollama no way to use their native
`response_format`/`format`. The forced tool is an implementation detail of one
adapter, not the interface.

### 14.2 Parse free text with a prompt convention

Ask for JSON in the prompt and parse the reply — no type changes. This is the
`prompted` fallback (§8), not the contract: it never enforces, gives no honest
capability signal, and pushes validation into every consumer. Kept only as the
floor for providers with no mechanism.

### 14.3 A full JSON Schema / grammar engine

Support all of JSON Schema, or arbitrary CFG constrained decoding. Rejected as
the first step: the subset must be checkable by the runtime *and* expressible in
every provider mechanism, and Occam's razor says add keywords when a consumer
needs them, not before. The subset can grow; a grammar engine is a separate
design if a use case ever needs one.

### 14.4 Validate in the consumer (Portal/Workflow handler)

Let each consumer validate the model's JSON. Rejected: validation would be
re-implemented and drift between callers, and Workflow explicitly wants the
runtime — not a Portal parser — to validate before a node succeeds
([workflow runtime §9](workflow-runtime.md)). One validator, in the runtime.

## 15. Open Questions

### Decided

- **D1 — one named shared subset.** The schema subset is a single named
  definition that both this contract and Workflow's `input_schema` reference
  (§6), not two byte-identical copies. Neither boundary has implemented its
  subset yet, so there is nothing to reconcile — the definition is authored once
  and both reference it, which is why §14.4's drift concern does not arise. The
  first-version keywords are objects, the scalar types
  (`string`/`number`/`integer`/`boolean`), `enum`, `array` with `items`,
  `required`, `additionalProperties: false`, and `description` (needed for both
  Portal form generation and prompt rendering). Keywords with no cross-provider
  mapping or no runtime check — `oneOf`/`anyOf`/`allOf`, `$ref`,
  `patternProperties`, `format` validation — are out of the first version and
  added only when a consumer needs one (§14.3).
- **D2 — no `Strict` field; always validate-or-typed-failure.** The contract has
  a single behavior: the runtime validates the candidate value against the
  schema and returns either the validated value or a typed failure (§9). There
  is no best-effort shape mode. `Enforced` (§8) already carries the only nuance
  that matters — whether the provider guaranteed the shape (`native`,
  `forced_tool`) or the value only survived the runtime's own validation after a
  `prompted` fallback. A "best-effort" value that does not fail on an off-schema
  result would give a consumer nothing trustworthy, so it is not worth a second
  mode.

### Still open

- How a run requests structured output for its *final* answer without the model
  emitting the forced-tool call too early on a provider that maps to a tool —
  i.e. the prompt/stop conditions that keep §5's "final answer only" true in
  practice. Resolved when the Anthropic `forced_tool` mapping lands (Phase 2).
- Whether the Task result envelope's structured field needs its own retention
  and redaction rules distinct from the text output, given it may carry
  extracted data.
- Whether reasoning-carrying providers (`ProviderState`) interact with
  structured output in a way the adapter must sequence.
