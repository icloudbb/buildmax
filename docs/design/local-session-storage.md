# Local Session Storage

> **简体中文：** [阅读中文镜像](../zh-CN/design/本地会话存储.md)

> **Audience:** contributors and security reviewers · **Status:** implemented
>
> Related: [session architecture](../contribute/architecture/session.md),
> [sessions and traces](../../manual/sessions-and-traces.md),
> [durable run trace](durable-run-trace.md),
> [context durability](context-durability.md), and
> [durable Agent sessions](../proposals/durable-agent-sessions.md).

## Contents

- [1. Decision](#1-decision)
- [2. Current State And Problem](#2-current-state-and-problem)
- [3. Goals And Non-Goals](#3-goals-and-non-goals)
- [4. Record Ownership](#4-record-ownership)
- [5. `meta.json`](#5-metajson)
- [6. `history.jsonl`](#6-historyjsonl)
- [7. Commit And Recovery Semantics](#7-commit-and-recovery-semantics)
- [8. Rewind And Fork](#8-rewind-and-fork)
- [9. Subagent Sessions](#9-subagent-sessions)
- [10. Traces](#10-traces)
- [11. Session Content And Privacy](#11-session-content-and-privacy)
- [12. Index, Locking, And Lifecycle](#12-index-locking-and-lifecycle)
- [13. Journal Growth](#13-journal-growth)
- [14. Ownership And Interfaces](#14-ownership-and-interfaces)
- [15. Alpha Cutover](#15-alpha-cutover)
- [16. Alternatives](#16-alternatives)
- [17. Verification](#17-verification)
- [18. Delivery Phases](#18-delivery-phases)
- [19. Open Questions](#19-open-questions)

## 1. Decision

Replace each local session's mutable whole-file JSON with a session-owned
directory whose records each carry their own contract:

```text
<BUILDMAX_HOME>/sessions/
  index.json

  <session_id>/
    meta.json
    history.jsonl
    traces/
      <run_id>.jsonl
    writer.lock
```

- `meta.json` is the small, atomically replaced current metadata record.
- `history.jsonl` is the authoritative append-only resumable conversation
  journal.
- `traces/<run_id>.jsonl` keeps the existing bounded, redacted, fail-open run
  diagnostics, one file per run.
- `writer.lock` enforces one mutable owner without treating stale process state
  as durable metadata.
- `index.json` is a rebuildable projection for the session picker.

The design borrows continuous item persistence and linked conversation history
from JSONL-based coding agents without multiplexing metadata, resumable
content, and run diagnostics into one unbounded file.

## 2. Current State And Problem

`internal/core/session.Session` currently serializes directly to
`<BUILDMAX_HOME>/sessions/<id>.json`. It contains:

- user, assistant, and tool messages;
- assistant tool calls and tool results;
- provider-owned reasoning state and non-text parts;
- accumulated prompt and completion tokens;
- compaction boundary and summary;
- durable notes and todos; and
- the additional system prompt.

`SessionManager.Save` marshals the complete object and replaces the file after
a completed turn. A separate `sessions.json` stores title, workspace, creation
time, and pin state for listing. The selected model lives only in
`SessionContext`, so reopening a session currently falls back to a default.

This shape has four problems, worst first:

1. A long Agent turn may complete many model and tool steps before the session
   is saved. A crash loses the distinction between work that never started and
   work that may already have changed the world.
2. A flat `Messages` array has no durable identity for a rewind or fork point.
3. Direct overwrite could leave the only file unparsable after interruption,
   disk exhaustion, or machine failure. Phase 0 retired this one: both files
   are now replaced through `util.WriteFileAtomic`.
4. Every turn rewrites all previous messages, tool results, provider state, and
   image data.

The order matters. Write amplification is the most visible problem and the
least important one: on a local single-user session it is wasteful, not
incorrect, and on its own it would justify a buffer, not a format. Uncertain
tool outcomes and missing item identity are what the current shape cannot
express at all, and they are what the rest of this document is for.

Atomic replacement was worth doing on its own and has landed, but it changes
only the third problem. It does not solve incremental durability, tool-outcome
uncertainty, or linked history, which is why it is a stop-gap rather than an
answer.

## 3. Goals And Non-Goals

### 3.1 Goals

- Persist each completed resumable item once instead of rewriting the complete
  history.
- Preserve exact `llm.Message` values, including tool calls, provider state,
  content parts, and provenance.
- Commit enough state during a turn to recover safely after interruption.
- Give every history item a stable identity and logical parent.
- Support resume, rewind, and fork without making the run trace authoritative.
- Keep title lookup and session listing fast.
- Isolate metadata, history, trace, and writer-lock failure policies.
- Keep the stored form neutral about which model provider produced a turn, and
  extensible to records this version does not define.
- Give foreground, background, and nested subagents private durable sessions
  without exposing them as ordinary user sessions.
- Preserve the single-binary local experience and keep Agent Core independent
  of storage implementations.

### 3.2 Non-goals

- Replaying or rolling back arbitrary external side effects.
- Capturing or restoring workspace file state. Rewind moves the conversation;
  it does not undo what the conversation did.
- Persisting credentials, approval grants, process handles, or shell sessions.
- Making a local transcript tamper-proof audit evidence.
- Implementing Server synchronization, Space sharing, or remote authorization.
- Sharing a physical history prefix between forked sessions in the first
  implementation.
- Supporting simultaneous writers to one session.
- Preserving or migrating the Alpha whole-file JSON format.

## 4. Record Ownership

| Data | Authority | Why |
|---|---|---|
| Session ID, kind, creation time | `meta.json` and history header | The directory and journal remain independently identifiable |
| Title, pin | `meta.json` | Presentation metadata does not affect model context |
| Selected model | `meta.json` | Current choice for the next turn |
| Workspace root for the next turn | `meta.json` | A current selection, exactly like the selected model |
| Model and workspace root a turn ran under | `turn_started` history record | Facts about a specific turn |
| Token totals | `meta.json` | Local aggregate reporting; per-call evidence remains elsewhere |
| Messages and tool results | `history.jsonl` | Required to reconstruct provider history |
| Tool crossed execution boundary | `history.jsonl` | Required to classify interrupted effects safely |
| Compaction summary and boundary | `history.jsonl` | Changes the model-visible history on a particular branch |
| Notes, todos, additional prompt | `history.jsonl` | Durable state injected into later model requests |
| Current history head | `head_selected` history record | Derived on load and never stored as metadata, so it cannot disagree with the branch it names |
| LLM/tool timing, working directory, sandbox, plugins, denial diagnostics | per-run trace | Bounded operational evidence, not resume input |
| Active writer | OS lock plus `writer.lock` diagnostics | A process fact that must become stale automatically |

`meta.json` and `history.jsonl` intentionally do not have one transaction, and
neither is a projection of the other. They hold different kinds of authority:

- `history.jsonl` is authoritative for everything that reconstructs a
  conversation: messages, tool outcomes, compaction, and durable state.
- `meta.json` is authoritative for the session's current selections and
  running aggregates: title, pin, workspace, selected model, token totals, and
  accumulated cost. These are settings and counters, not summaries of the
  journal. No amount of replay recovers them, and no history record needs to.
- `index.json` is the only pure projection in this design, and the only file
  claimed to be rebuildable by scanning.

Two authorities are safe here because their failures are not symmetric: losing
a current selection degrades presentation and defaults, while losing history
breaks resume. A stale title or index row must never make conversation recovery
incorrect.

The split only works if nothing straddles it, so `meta.json` holds nothing that
`history.jsonl` also determines. The current head is a `head_selected` record
and is derived on load (§6.2); it is not mirrored into metadata. A
last-sequence counter is not stored either. Both are known for free the moment a
session
opens, because opening replays the journal, so caching them in metadata
would buy no speed and cost a consistency rule.

Immutable duplication is different and is allowed: session ID, kind, and
creation time appear in both `meta.json` and the history header, which is what
makes each file independently identifiable. Values fixed at creation cannot
drift, and §6.1 already defines a mismatch between them as corruption rather
than as a stale copy to reconcile. The rule is therefore about mutable state:
two records may repeat a constant, and must not repeat a variable.

## 5. `meta.json`

The per-session metadata file is small enough to replace atomically:

```json
{
  "version": 1,
  "id": "<uuid>",
  "kind": "user",
  "created_at": "2026-08-23T10:00:00Z",
  "updated_at": "2026-08-23T10:30:00Z",
  "title": "Design local session storage",
  "workspace": "/repo",
  "pinned": false,
  "selected_model": "anthropic/claude-opus",
  "prompt_tokens": 12000,
  "completion_tokens": 1800,
  "cache_read_tokens": 9000,
  "cache_write_tokens": 1200,
  "cost": {"currency": "USD", "total": 4200},
  "cost_incomplete": false
}
```

`kind` is `user` or `subagent`. A child adds immutable lineage metadata:

```json
{
  "kind": "subagent",
  "parent_session_id": "<uuid>",
  "parent_run_id": "<run id>",
  "parent_tool_call_id": "call_1",
  "agent_type": "explorer",
  "delegation_depth": 1,
  "hidden": true
}
```

A forked user session adds:

```json
{
  "forked_from": {
    "session_id": "<parent uuid>",
    "head_id": "<history item id>"
  }
}
```

`cost` is `llm.Cost` verbatim, including its cache and baseline breakdown.
`cost_incomplete` says a turn ran against an unpriced model or in a currency
this build does not convert, because a total that silently dropped half a
session is worse than one labelled incomplete. Cache token counts break
`prompt_tokens` down rather than add to it; a reader totalling a session must
not sum all three.

Title, pin, workspace, and selected-model changes write a temporary file, sync
it, and atomically replace `meta.json`. They do not append conversation events.

If metadata is damaged, the session recovers with conservative defaults: no
title, no pin, the configured default model, and zeroed aggregates. Reported
usage is then wrong, which is acceptable, because it is reporting.

None of those defaults can make recovery incorrect, because nothing here
selects model-visible state. The current head is the value that would, and it
is not in this file: after any rewind the last physical record belongs to the
branch the user abandoned, so a metadata copy that failed to keep up would
silently resume abandoned work. §6.2 derives the head from the journal instead,
which removes the disagreement rather than defining a winner for it.

`workspace` is a current selection like `selected_model`, not presentation
metadata: it decides where the next turn's tools run and which workspace
`AGENTS.md` is appended to the system prompt. Losing it therefore degrades a
resumed session to the configured default root, which is visible to the user
and correctable, not silent.

If history is damaged, valid metadata may keep the session visible but cannot
make it resumable.

`selected_model` means what the next turn should use. `turn_started.model`
records what a completed or interrupted turn actually used. This distinction
preserves provenance without turning every UI setting change into conversation
history.

## 6. `history.jsonl`

### 6.1 Header

The first line is an immutable, self-identifying header:

```json
{"type":"history","version":1,"session_id":"<uuid>","created_at":"2026-08-23T10:00:00Z"}
```

The loader rejects an unsupported version before interpreting records. A
header session ID that disagrees with the directory or `meta.json` is
corruption, not an alternate spelling.

### 6.2 Physical and logical order

Every later line has both a physical sequence and logical history identity:

```json
{
  "seq": 42,
  "id": "<history item id>",
  "parent_id": "<parent history item id>",
  "ts": "2026-08-23T10:01:02.123456Z",
  "type": "message",
  "turn_id": "<run id>",
  "data": {}
}
```

| Field | Contract |
|---|---|
| `seq` | Physical append order starting at 1; also the optimistic-concurrency token in §14's `Append` |
| `id` | Stable identity of this history item |
| `parent_id` | Previous logical item on this branch; omitted only for the root |
| `ts` | UTC RFC 3339 diagnostic instant; never the ordering key |
| `type` | Closed discriminator for resumable-history semantics |
| `required` | Whether a reader that cannot interpret `type` must refuse the session rather than skip the record; see §6.4 |
| `turn_id` | Optional correlation to the run and its trace |
| `data` | Type-specific payload with explicit `snake_case` fields |

Physical order stays append-only after rewind. Logical order is the parent
chain ending at the current head:

```text
physical append: A B C D E F

logical history:
  A -> B -> C -> D
       \-> E -> F   (current head)
```

This keeps abandoned branches available without truncating or rewriting the
journal.

The head is derived rather than stored beside the journal, and the derivation
is one rule: **the head is the last physical record.**

Rewind needs no special case because it is expressed in the parent links rather
than beside them. Every record chains to its physical predecessor except
`head_selected`, whose parent is the item being returned to. Appending one
therefore redirects the chain, and everything after it descends from that
target. Reading the head costs looking at the final line, and no second place
holds an answer that could disagree with it.

### 6.3 Record vocabulary

| Type | Payload | Resume effect |
|---|---|---|
| `turn_started` | Run ID, effective model, workspace root, context window, input kind | Opens a turn and fixes its runtime identity |
| `message` | One complete `llm.Message` | Adds user or assistant provider history |
| `tool_execution_started` | Tool-call ID and tool name | Marks that an approved call crossed the side-effect boundary |
| `tool_result` | Tool-call ID, status, full text, content parts | Projects one tool-role message and closes the call |
| `compaction` | Covered head/range, full accumulated summary | Replaces the model-visible prefix on this branch |
| `notes_replaced` | Complete stamped note list | Replaces durable notes |
| `todos_replaced` | Complete stamped todo list | Replaces durable todos |
| `additional_prompt_set` | Complete validated text | Replaces the durable additional system prompt |
| `head_selected` | Reason; the item returned to is its `parent_id` | Redirects the parent chain to an earlier item |
| `turn_finished` | Terminal status — `completed`, `failed`, `canceled`, or `interrupted` — and optional error classification | Closes the turn |
| `turn_recovered` | Interrupted turn and uncertain tool-call IDs | Makes cold recovery explicit before new work |

`message` preserves the full portable `llm.Message`, not only text. An
assistant record therefore includes tool calls and opaque provider state. A
user record preserves background provenance and content parts.

`tool_result` is stored as its own typed history item rather than duplicated in
a separate generic message. The reducer projects it to the `role: tool`
message required by provider adapters.

Title, pin, workspace, selected-model preference, usage totals, plugins,
sandbox policy, and timing are deliberately absent. They belong to metadata or
run diagnostics rather than the conversation journal.

### 6.4 Provider neutrality and forward compatibility

Two properties matter more here than they would in a single-vendor transcript,
because BuildMax targets many providers and expects to grow features this
document does not foresee.

**Neutral about the producing protocol.** History stores the portable
`llm.Message`, never a provider's wire payload. Roles, tool calls, tool
results, content parts, and the turn lifecycle are BuildMax's own vocabulary,
so a journal written while talking to one provider replays against another.

The one provider-shaped value a record carries is `ProviderState`, and it is
tagged with the protocol that produced it precisely so a reader can tell
whether it still applies. Resuming under a different protocol — a switched
model, a changed gateway, a re-pointed config — drops provider state whose tag
does not match the active adapter instead of forwarding it. Dropping it loses
cached reasoning, which is a degradation; forwarding it would send one vendor's
opaque bytes to another vendor's endpoint, which is a defect. A session whose
model changed mid-conversation therefore holds mixed-protocol messages, and
each is judged on its own tag rather than on a session-wide assumption.

**Extensible without breaking older readers.** `type` is a closed discriminator
for the semantics defined here, but the format still has to admit records this
version has never seen: a later feature, or a kind contributed by a plugin.
Every record therefore declares whether a reader that does not understand it
may skip it.

- A record that only adds information is skippable. An older binary logs it and
  continues.
- A record that changes what the model sees is required. A reader that cannot
  interpret it must refuse the session rather than resume a conversation it is
  silently mis-reducing.

A concrete case shows why the bit is decided per record rather than per format
version. Today a run cannot move between directories: Bash executes every call
at the workspace root, and file tools reject paths that escape it, so no record
is needed to say where a tool ran. If the runtime later gains a persistent
shell working directory, a change to it is model-visible and would arrive as a
required record — and older binaries would correctly refuse a session they
cannot place, instead of replaying it against the wrong tree.

That one bit is what lets §7.2 stay strict without turning the format into a
version trap. Unknown-and-skippable is a warning; unknown-and-required is
treated exactly like corruption, because in both cases the reduced history
would be wrong. Extension is therefore a decision about whether a new record
is model-visible, which is a question this design already asks of every record
type in §6.3.

## 7. Commit And Recovery Semantics

### 7.1 Turn commit boundaries

```text
accept prompt
  append + sync turn_started
  append + sync user message

call model
  append + sync assistant message

for each approved tool call
  append + sync tool_execution_started
  execute external effect
  append + sync tool_result

before next model request
  verify the current history prefix is durable

finish turn
  append + sync state changes and turn_finished
  atomically refresh meta.json and index.json
```

An item affecting resume must not become visible to Agent Core until history
accepted it. On persistence failure the turn stops. Session persistence cannot
use the run trace's fail-open behavior.

Batching is allowed only across boundaries that do not change failure
classification. `tool_execution_started` reaches stable storage before a tool
may change the world. Its result reaches stable storage before another model
request consumes it.

The strength of that guarantee is exactly the strength of the sync beneath it,
and the primitive is `os.File.Sync`. The Go runtime already maps it to the
strongest one each platform offers — `F_FULLFSYNC` on macOS, which waits for
the device instead of returning at the drive's write cache, `fsync(2)` on Linux
and the BSDs, `FlushFileBuffers` on Windows. Calling a platform primitive
directly would reimplement that mapping without improving it.

Process crash and power loss are different guarantees and this design does not
conflate them. The §7.3 classification is sound under power loss wherever the
platform's strongest primitive is honoured, and degrades to crash-only where it
is not: macOS falls back to `fsync` when `F_FULLFSYNC` returns `ENOTSUP`, which
some network and virtual filesystems do, and no software primitive survives a
device that reports a flush it never performed. With a cache-only flush a
`tool_execution_started` record can be lost after the tool already ran, and
recovery would then classify as `not_started` work that changed the world. Both
degradations belong to the filesystem underneath rather than to the protocol,
and neither is silent: the fallback is observable where it happens.

Directory entries are a separate question with a cheaper answer. Appending to
an existing journal does not change its directory entry, so the hot path needs
no directory sync at all. Creating a session directory does. Replacing
`meta.json` deliberately does not: §4 already establishes that losing a
metadata write degrades a current selection rather than recovery, so paying a
directory sync every turn to protect it would buy the wrong thing.

That choice is a latency budget as much as a correctness one. This protocol
syncs at least five times per turn plus twice per approved tool call, so a turn
with ten parallel tool calls crosses roughly twenty-five sync points on the
interactive path. That is the cost a user pays every turn, and it is the one to
measure first if this ever needs tuning — opening a session is a cost paid once.
Neither is instrumented today.

### 7.2 Torn tail and corruption

The loader validates the header, schema, unique IDs, parent references, and
record relationships. An incomplete final line is a torn tail and may be
truncated to the last newline while acquiring the writer role. Invalid JSON, an
unknown required type, or an invalid relationship before the tail is corruption
and fails load with a path and sequence number.

A gap in `seq` is not on that list. With one writer and an append-only file
only the tail can tear, so a gap means either that something outside BuildMax
truncated or edited the journal — §3.2 puts tamper-evidence out of scope — or
that a record is missing, which parent-chain validation already rejects
wherever it could change the reduction. A gap confined to an abandoned branch
changes nothing the model sees. A second signal for a failure the first one
already reports buys strictness rather than safety, so `seq` stays an ordering
and concurrency device and is not a corruption check.

Read-only inspection never repairs files. Opening for continuation repairs the
torn tail and appends recovery state before accepting a new prompt.

Refusing to continue is not the same as offering no way out. A corrupt record
before the tail fails the load for continuation, and the loader also exposes a
read-only salvage mode that reduces the valid prefix up to the first bad record
and exports it as a new session directory. The damaged original is never
rewritten in place. A local session that can only be reported as broken, with
no path to the work inside it, is a worse outcome than a conservative refusal
plus an explicit recovery command.

### 7.3 Unknown tool outcome

An assistant tool call proves the model requested work. A
`tool_execution_started` record proves BuildMax approved the call and was about
to cross into the tool. A `tool_result` records the observed outcome.

| Durable history | Recovery classification |
|---|---|
| Assistant call only | `not_started` |
| Assistant call plus `tool_execution_started` | `outcome_unknown` |
| Matching `tool_result` | Known `completed`, `failed`, `denied`, or recovery-written `unknown` outcome |

`outcome_unknown` is never retried automatically. Recovery tells the model to
verify possible effects before retrying anything non-idempotent.

That repair is a history write, not a reduce-time fiction. Opening an
interrupted session appends one `turn_recovered` record naming the interrupted
turn and its uncertain tool-call IDs, followed by one `tool_result` with status
`unknown` for each of them. The synthetic result the model sees is therefore
itself durable and appears exactly once: a second interrupted open extends the
branch instead of replaying the same repair, and no reader has to know which
tool results were observed and which were reconstructed, because the record
says so.

The journal records the Agent's observed state. It cannot prove the filesystem,
a shell process, an HTTP endpoint, or a remote database matches that state.

### 7.4 Cancellation and shutdown

A turn stops in one of two ways, and the axis that separates them is not what
anyone intended but whether a process was alive to say so.

While BuildMax is running it closes the turn itself. A person stopping the turn
gets `turn_finished` with status `canceled`; a shutdown arriving mid-turn gets
`interrupted`. Those are the two causes
[`graceful-shutdown.md`](graceful-shutdown.md) already separates at the run
level as `ErrRunCanceled` and `ErrRunInterrupted`, on reasoning that applies
here unchanged: a stop the process knows about in advance lets the run say what
happened instead of going silent and being declared abandoned later. The
journal keeps the distinction for the same reason the Portal does — the two are
not the same event and must not read the same.

A closed turn is not an interrupted one. The next open sees `turn_finished`,
appends no `turn_recovered`, and starts work rather than repair.

Closing a turn does not resolve the tools inside it. A call that crossed
`tool_execution_started` without returning is `outcome_unknown` whether the turn
was cancelled, interrupted, or lost with its process: BuildMax knows it entered
the tool and not whether the effect landed, and stopping deliberately does not
make that knowable. So the live process writes exactly what §7.3 says recovery
would have written — one `tool_result` with status `unknown` per uncertain call
— while it still holds the call IDs, rather than inventing a certainty it does
not have.

The two paths converge instead of branching. If the process dies during its own
cancellation, on a second interrupt or a `SIGKILL`, nothing special is needed:
the turn is left open and the next open recovers it exactly as §7.2 and §7.3
describe. Graceful closure is an optimisation over recovery, not an alternative
to it.

## 8. Rewind And Fork

### 8.1 Why there is no checkpoint

An earlier draft had a `checkpoint` record naming a head, so it could be
returned to later. It is gone, and the reason is recorded rather than left to
be rediscovered.

A checkpoint is a promise about state. Everywhere else the word is used, it
means "put things back the way they were" — files included. This design cannot
keep that promise: BuildMax has no versioned workspace capability and no design
record for one, the earlier record having been withdrawn rather than
implemented, as [`trust-harness.md`](trust-harness.md) states.

A conversation-only checkpoint would therefore have been a name that promises
more than the thing does, over a capability rewind already provides. Worse, the
name would hide the sharp edge instead of exposing it: rewinding past a turn
that edited files, ran Bash, or called a network service leaves every one of
those effects in place. The model's history returns to an earlier point and the
world does not, so the model then reasons from a picture of the workspace that
is no longer true.

That is a real hazard, and it is rewind's to carry and to surface (§8.2), not
something to be smoothed over by a reassuring word. Restoring what an agent
changed is a capability worth having, but it is a different design record, and
naming a slot for it here would describe a withdrawn capability as upcoming.

### 8.2 Rewind

Rewind takes one of the user's own prompts back. The chosen message leaves the
branch along with everything after it, and its text returns to the input box to
be edited and sent again — which is what the operation is for, and why it is
exclusive of the message rather than inclusive of it.

Rewind appends a `head_selected` record whose parent is the record before the
rewound message. That is the entire operation.

The landing record is the physical predecessor on the branch, not the previous
message: notes, todos, a compaction, and the `turn_finished` of the turn before
all sit between two messages and belong to work that is being kept. The one
record stepped over is `turn_started`, because §7.1 opens a turn before the
prompt that starts it, and landing there would leave the branch inside the turn
being dropped. A prompt with nothing before it — the first of a session — has no
landing and is not offered; a new session says the same thing without inventing
an empty branch.

What a surface may offer follows from that. Rewind offers the prompts the user
typed, since a reply has nothing to hand back and a background event arrives as
a message the user never wrote. Fork offers every message a turn ended on, the
head included, but not a reply that asked for a tool: a branch ending there
holds a call with no result, which only some provider adapters prune. The target is not repeated in the
payload: this is the one record that deliberately points somewhere other than
its physical predecessor, so the parent link already says everything a target
field would, and storing it twice would only create a pair that could
disagree. Because the head is derived rather than
stored, there is no second write to keep in step and no window in which a crash
could leave the selection half-applied. The old tail remains in
`history.jsonl`, and a new prompt appended after rewind names the selected head
as its parent, creating a new logical branch without truncation.

Rewind moves the conversation and nothing else. It does not restore the
workspace (§8.1), and it does not change the workspace root: that root is a
current selection in `meta.json`, so rewinding to a turn that ran somewhere
else leaves the next turn where the user last put it. The `turn_started` record
of the rewound-to turn still says which root it used, which is what makes the
difference inspectable rather than silent.

Compaction is branch-scoped. A summary records exactly which history head or
range it covers. A summary produced after the fork point cannot be reused by a
child that does not contain the summarized records. Raw history remains until
all rewind and fork retention rules permit physical compaction.

### 8.3 Fork

Fork physically copies the history prefix through the selected message into a
new session directory. It preserves item IDs, gives the child an independent
session ID and metadata record, and writes fork provenance into `meta.json`.

What is copied is the *branch* through that message, not the physical prefix: a
parent that was rewound holds abandoned records too, and a child carrying those
would hold history its own parent chain never reaches. Sequence numbers are not
preserved — `seq` is a record's position in the journal holding it, so the child
is renumbered from one, and a gap in the parent leaves no trace. Item IDs are,
because they are the identity that makes a child's records recognisable as the
same work the parent did.

The child starts its own usage and cost totals at zero. Inheriting the parent's
would double-count the same money as soon as anyone added the two sessions up.
Title and workspace are carried, because they describe the conversation being
continued rather than the run that produced it. The model is not: the child
selects the forking surface's default model, as any new session does. Carrying
the workspace means a fork shares the parent's working directory; there is no
fork-time isolation, and a fork that needs its own tree enters one with the
`Worktree` tool ([workspace root and worktrees](workspace-root-and-worktrees.md)).

Physical copying is O(n), but it gives the Alpha implementation clear
ownership:

- parent deletion cannot break the child;
- loading never walks another session;
- authorization and retention stay session-local; and
- no reference-counted history graph is required.

A fork copies the journal prefix and nothing else, because history holds all
of a session's content inline (§11). There is no separate content store for a
child to share, and therefore no reference counting to get wrong.

Shared prefixes, copy-on-write segments, and database-backed history graphs are
not planned. Each buys a smaller copy at the price of deletion, retention, and
garbage-collection rules that the physical copy does not need at all.

Traces are not copied into the child resume context. Fork metadata names the
session and head it came from; new child runs create new trace files.

## 9. Subagent Sessions

Every foreground, background, or nested subagent gets the same directory shape
and its own history writer. It never writes into the parent's journal or state
store.

Subagent sessions are `kind: subagent`, hidden from the ordinary picker, and
excluded from `--continue`. Their header and metadata record the immediate
parent session, run, tool call, Agent type, and delegation depth. The parent
Task result remains the model-facing return path.

The private journal removes the current behavior where a subagent's entire
`session.Session` is discarded on return. It does not turn a current one-shot
subagent into a user-visible fork. A parent crash does not automatically restart
the Task tool: parent history classifies the call, while the child journal says
whether the child itself reached a terminal turn.

A sealed child is retained with its parent under the chosen retention policy.
A subagent that needs its own tree gets one through the `Task` tool's
`worktree` option (workspace root and worktrees, D7). Persistent fork
supervision, durable mailboxes, and automatic parent resume are not planned;
[local background jobs](local-background-jobs.md#durability-is-not-planned)
records why.

## 10. Traces

The existing run-trace contract remains unchanged except for physical
co-location:

```text
<session_id>/traces/<run_id>.jsonl
```

One file per run preserves independent caps, terminal `run_end`, retention,
upload, and parent/child trace correlation. A single `trace.jsonl` for an entire
session is rejected because concurrent child or background runs, per-run
cleanup, and corruption would share one unbounded append target.

Where a tool ran belongs here too. A run cannot move between directories
(§6.4), but a single Bash command can still `cd` inside its own invocation or
name an absolute path, so the directory a call actually touched is operational
evidence rather than resume input. Recording it per call in history would stamp
mutable per-call context onto every record, which is the practice §16.6
rejects; the trace is bounded and already the place for it.

The trace stays bounded, redacted, and fail-open. History stays lossless for
resume and fail-closed at commit boundaries. A trace may summarize a history
item ID for correlation, but a resume path never reads trace content as state.

## 11. Session Content And Privacy

History stores content inline: ordinary text, structured tool results, and
non-text parts alike. There is no separate content store, no size threshold,
and no reference to resolve on load — a session's conversation is in the one
file that owns it. Nothing here bounds how large that gets; §13 says what that
costs and what is not done about it.

Session content is not redacted, because redaction would change what the
resumed model sees. Session directories use private permissions (`0700`
directories and `0600` files where supported). Credentials and approval grants
never enter metadata, history, or traces as session state.

## 12. Index, Locking, And Lifecycle

`sessions/index.json` is the picker projection. It contains user-visible
sessions only, with ID, title, workspace, pin, kind, update time, and enough
lineage to group forks. It is written through temporary-file sync and atomic
rename, and rebuilt by scanning `meta.json` files when stale or damaged.

Listing reads `index.json` and does not scan. The projection exists so that
opening the picker costs one small file instead of one `meta.json` per session,
and that saving disappears if a stale projection quietly falls back to walking
directories. Rebuilding is therefore an explicit repair — triggered when the
index is missing, unparsable, or fails its own consistency check — and it is
the only path that walks session directories.

The session journal has one writer. The existing in-process turn guard remains,
and the file backend adds a cross-process lock. `writer.lock` may contain PID,
process start time, or owner diagnostics, but those values are not authority:
the OS lock decides ownership, and an abandoned file cannot keep a session
permanently busy.

That cross-process lock is an OS advisory lock on `writer.lock`: `flock(2)` on
macOS and Linux, `LockFileEx` with `LOCKFILE_EXCLUSIVE_LOCK |
LOCKFILE_FAIL_IMMEDIATELY` on Windows, both through `golang.org/x/sys`, which
is already a dependency.

The portable alternative loses on the requirement above. An `O_CREAT|O_EXCL`
lock file needs no platform code at all and cannot clear itself: it outlives
the process that made it, so recovering from a crash would mean judging whether
a recorded PID is still alive — the stale process state this design refuses to
treat as authority. A kernel lock is released when the owning process exits,
however it exits, which is the only version of "an abandoned file cannot keep a
session permanently busy" that needs no heuristic.

The lock is taken on `writer.lock` rather than on `history.jsonl`, because a
reader must still be able to inspect a stable prefix while a writer holds the
session. Locking the journal itself would either block those readers or, with
POSIX record locks, expose the rule that closing any descriptor for a file
drops every lock the process holds on it. A separate file has neither problem,
and it is the same reason `flock` is preferred over `fcntl` on Unix.

Advisory locking is not universally available: some network filesystems refuse
it, and some accept it and do nothing. A failure to acquire is reported rather
than assumed successful, and a filesystem that cannot lock is one where
BuildMax says so instead of pretending a session is held exclusively.

Read-only inspection may read a stable prefix while a writer is active. A
second process cannot resume, rewind, fork from an unstable head, or append
until it owns the writer lock.

Deleting a session removes its directory outright. It can, because a fork is a
physical copy (§8.3): no other session's content lives in this directory, and
nothing outside it points in. Because this is a material destructive operation,
the caller identifies the exact session rather than matching on a prefix.

## 13. Journal Growth

Nothing bounds a journal's size. It grows with the conversation and with every
inline tool result, and it is never rewritten.

A rewind-heavy session grows faster than its visible conversation suggests, and
opens more slowly for a reason that is easy to misread: replay parses the whole
file to find the live branch, so open cost tracks total bytes on disk —
abandoned branches included — not the length of the conversation the model
actually sees. Two sessions showing the same transcript can open at very
different speeds.

Physical pruning is what would bound this, and it does not ship. A prefix
cannot be removed while a branch or a fork export depends on it, and no pruning
should ship before that reachability rule and its recovery behaviour are
specified and tested. Neither is, and no growth measurement is collected today
either, so this section describes a known cost rather than a managed one.

## 14. Ownership And Interfaces

```text
internal/core/session
  history item types, validation, linked-history reducer, recovery analysis

internal/agentapp
  SessionManager, commit orchestration, metadata/index policy, lifecycle

internal/infra/sessionstore
  JSONL codec, atomic metadata files, locking, append/sync, tail repair
```

`internal/core/agent` continues to depend on history and durable-state
interfaces. It does not import files, JSONL, databases, or Server clients.
The persistence seam expresses session semantics rather than paths, and it lives
in `internal/core/session` itself — expressed purely in terms of core types, it
carries no dependency on infra, which is what lets `internal/infra/sessionstore`
depend on it rather than the reverse:

```go
type Store interface {
    Create(ctx context.Context, meta Meta) error
    Open(ctx context.Context, id string) (Writer, error)
    Load(ctx context.Context, id string, mode LoadMode) (Loaded, error)
    UpdateMeta(ctx context.Context, id string, update MetaUpdate) error
    List(ctx context.Context, includeHidden bool) ([]ItemSummary, error)
}

type Writer interface {
    Loaded() Loaded
    Append(ctx context.Context, items ...Item) error
    Close() error
}
```

Append is not a flat, stateless call keyed by an `expectedSeq` a caller passes
in. Building the seam surfaced why: the writer lock is what rules out a second
writer, and it protects a whole turn, not one call. A single `Append(ctx, id,
expectedSeq, items...)` would have to choose between holding that lock across
every call a caller makes — which the signature does not express — or
re-acquiring it call by call, which would let a second process's turn interleave
into the middle of the first's. `Open` acquires the lock and returns a `Writer`
that holds it for exactly as long as one turn is committing; `Writer.Close`
releases it. `expectedSeq` becomes unnecessary once the lock is held for that
whole span: nothing else can move the tail out from under a caller mid-turn, so
`Append` instead checks that each item's `Seq` and `ParentID` continue exactly
from where this `Writer`'s own view of the branch left off. A mismatch is
therefore a caller bug to report, not a race to retry.

`Open` is also where recovery happens, mechanically. It repairs a torn tail
(§7.2) and, only when the branch it finds still has calls left uncertain by an
interruption, appends the repair records §7.3 describes before returning. The
trigger is "is there an uncertain call to resolve", not "was the turn left
open": a session already repaired by an earlier `Open` still shows an open turn
on a later one — no `turn_finished` is retroactively added — but with nothing
left uncertain, so nothing is written twice. `Writer.Loaded().Recovery` reports
what was just repaired for a caller that wants to tell the user, even though the
branch it sits beside is already caught up.

`Load` never acquires the lock and never repairs — a writer may be open
concurrently, and inspection only ever sees a stable prefix (§7.2). It still
computes `Recovery`, so a caller that only wants to display a session's state
can learn it was left mid-turn without anything being written.

Exact names may change further. The boundaries may not: core owns meaning and
deterministic reduction; AgentApp owns when state commits; infra owns physical
durability.

Mutation APIs must prevent callers from changing resumable exported fields
without appending history. The materialized `Session` may remain a read model,
but messages, compaction, notes, todos, and additional prompt changes go through
the committing session context.

## 15. Alpha Cutover

Existing `<session_id>.json`, `sessions.json`, and the separate trace layout are
not migrated. The new runtime does not read, import, or dual-write the old
format.

CLI, TUI, Desktop, eval, worker restore, run-global upload, task conversation
projection, rename, pin, delete, documentation, and tests change together. No
production path continues treating the whole-file session JSON as current
storage.

Old files may remain inert until explicitly removed. Their presence does not
make them visible in the new index or reserve an identity in the new store.

## 16. Alternatives

### 16.1 Atomic whole-file JSON

Useful as a stop-gap against partial overwrite. It still rewrites complete
history and loses in-flight tool state, stable item identity, and cheap fork
points.

### 16.2 One multiplexed JSONL file

A single append stream gives metadata, history, runtime state, and diagnostics
one total order and makes a session easy to copy. It also couples lossless
resume data to redacted fail-open traces, repeats mutable metadata, prevents
independent retention, and grows one schema and file without bound.

This is appropriate for a single-writer transcript product that values one-file
portability above policy separation. BuildMax already has distinct session,
trace, artifact, worker, and Portal contracts, so the directory bundle is the
better boundary.

### 16.3 Flat message-only JSONL

This removes whole-history rewrites and supports resume. Without stable item
IDs, parent links, tool execution boundaries, compaction, and durable memory,
it cannot safely implement interruption recovery, rewind, or fork.

### 16.4 SQLite for all local sessions

SQLite gives transactions, indexing, and concurrent readers. It also adds a
database schema and migration runtime to the single-binary local path before
query or concurrency evidence requires it. It would be a new dependency rather
than a reuse of an existing one: `internal/infra/db` is the server store and
speaks MySQL through GORM, so no local path links a SQLite driver today.

The linked-history domain contract stays backend-neutral so SQLite can be
qualified later.

### 16.5 Shared fork prefixes in phase 1

Reference sharing reduces copy cost but makes child load, parent deletion,
authorization, retention, and garbage collection interdependent. Physical
prefix copying is intentionally simpler for Alpha.

### 16.6 Field-level compatibility with existing JSONL agents

Matching another coding agent's record fields — so that a BuildMax journal
opens in its tools, or its sessions replay here — is rejected because it
points the wrong way for this project. BuildMax is an open, multi-provider
runtime.
Its stored form should stay neutral about which model or protocol produced a
turn, and stay free to grow records for features no vendor's format
anticipates. Adopting one vendor's schema trades both away: it couples an open
runtime to a format it does not control, inherits that format's churn, and
turns every future extension into a compatibility question about somebody
else's product. §6.4 is the property being protected.

Scope reinforces the same answer. Compatibility only earns its constraint when
files actually cross the boundary, and importing foreign transcripts is nowhere
in the roadmap.

The structural lessons are taken, and §1 says so: parent-linked records instead
of a flat array, one durable record per completed item, and a self-identifying
first line. Those are properties of a well-formed journal, not of anyone's
product. Two common practices are deliberately not taken. One is the single
multiplexed stream, rejected in §16.2. The other is stamping mutable
per-session context — working directory, branch, build version — onto every
record, which inflates the journal and creates as many answers to a question as
there are lines; §4 keeps that in `meta.json` and §6.2 keeps the record header
minimal.

The execution boundary in §7.3 has no counterpart in the formats this design
learned from. A record proving BuildMax was about to enter a tool, distinct
from the record proving the tool returned, is what makes `outcome_unknown`
expressible at all. That difference is the reason this is not a port.

## 17. Verification

Implementation is incomplete until tests establish:

- replay reconstructs messages, provider state, parts, compaction, notes,
  todos, additional prompt, and tool results exactly;
- logical parent validation rejects duplicate IDs, cycles, missing parents, and
  unsupported versions, and a journal whose `seq` skips a value on an abandoned
  branch still loads;
- torn final lines preserve every earlier committed record while earlier
  corruption fails load;
- failure before and after each tool boundary yields `not_started`, known
  outcome, or `outcome_unknown` as specified;
- parallel tool completion preserves committed history order and call IDs;
- rewind selects an earlier head without deleting the abandoned branch, and
  returns the prompt it removed;
- rewind reports the tool calls on the span it moved past, including one left
  in flight by an interruption, and reports nothing for a conversation-only
  span;
- rewind re-reduces rather than unwinding: durable state written on the
  abandoned branch is gone from the resumed session, and a turn appended after
  a rewind extends the chosen branch;
- a rewind survives reopening — the resumed session is the branch that was
  chosen, not the one last in the file;
- only `head_selected` may name a parent other than the current head, and only
  one this session already holds;
- fork copies exactly the selected branch prefix and survives parent deletion;
- a fork of a rewound session copies the live branch, not the abandoned one;
- a forked journal loads through the ordinary reader, renumbered from one with
  its item ids intact;
- parent and child diverge: writing to one changes nothing in the other, and
  the child is listed as its own session carrying its lineage;
- compaction summaries never cross a branch they did not summarize;
- resuming under a different protocol drops provider state whose tag does not
  match the active adapter instead of forwarding it, and a history holding
  messages from more than one protocol still replays;
- an unknown skippable record loads with a warning and an unknown required
  record fails exactly like corruption;
- rewinding a prompt reproduces exactly the state the record before it reduces
  to, and leaves workspace files untouched;
- foreground, background, and nested subagents write isolated hidden bundles
  with immediate-parent lineage;
- hidden subagents never appear in the picker or become `--continue` targets;
- a second writer cannot mutate a live session, and the lock clears with no
  intervention when the holding process dies, including by `SIGKILL`;
- a turn closed as `canceled` or `interrupted` is not recovered on the next
  open, while a tool call left in flight by that stop still resolves to
  `unknown` rather than to a fabricated result;
- a process killed while cancelling leaves the turn open, and the next open
  recovers it by the ordinary path;
- stale metadata and global indexes repair without changing history;
- a damaged `meta.json` recovers as a visible session with zeroed aggregates
  rather than hiding it, and a session whose metadata cannot be read is skipped
  when the index is rebuilt rather than guessed at;
- opening an interrupted session appends its repair once, and opening the same
  session again appends nothing further while still reporting what was repaired;
- an append that does not continue the branch the writer opened is refused
  before anything reaches the file;
- a rewind followed by metadata loss resumes the selected branch rather than
  the abandoned physical tail, with no metadata field needing repair for it to;
- head derivation returns the same item as a full branch walk across journals
  with no rewind, one rewind, and repeated rewinds to different depths;
- a session whose runs used different workspace roots records the root each
  turn ran under, and replay does not restate earlier turns against the
  current root;
- salvage export recovers the valid prefix of a corrupt journal without
  modifying the original file;
- repeated interrupted opens append one recovery record per interruption and
  never duplicate a synthetic `unknown` tool result;
- old Alpha files are ignored and no code path dual-writes them; and
- all tests use the isolated `BUILDMAX_HOME` supplied by `./make test`.

Property tests generate linked histories and compare incremental reduction with
full replay from every reachable head. Crash tests use real temporary files and
inject failure around file sync, metadata rename, tool execution, and fork
copying.

## 18. Delivery Phases

### Phase 0: Stop-gap atomic replacement — done

- Replace direct whole-session and index overwrite with temp-write, sync, and
  atomic rename if the final implementation does not land immediately.

Landed. `util.WriteFileAtomic` writes a temp file in the target's directory,
syncs it, and renames over the target; the session file, the session index, and
the worker's restore of a previous run's session all use it. It syncs the file
but not the directory entry, so a write is all-or-nothing rather than durable —
§7.1 now names the primitive phase 1 uses and says what it does and does not
claim.

### Phase 1: Session bundle and linked history — done

- Add metadata schema, history header/items, reducer, JSONL backend, writer
  lock, and tail repair.
- Route message, tool, compaction, durable state, and additional-prompt changes
  through the committing context.
- Move traces under the session bundle, one file per run.
- Give subagents isolated hidden bundles.
- Cut CLI, TUI, Desktop, eval, worker paths, docs, and tests to the new format
  together.

Landed, across the three seams it was split into: the core item types and
reducer, then the `sessionstore` codec and lock, then the surface cutover. Only
the third touched user-facing behaviour, and it stayed atomic across CLI, TUI,
Desktop, eval, and worker paths because §15 rules out dual-writing — but
nothing about that constraint required the two layers beneath it to arrive in
the same review.

Two things the record described turned out to be more complicated than they
needed to be, and were corrected against the implementation rather than worked
around: head derivation collapsed to "the last physical record" once
`head_selected` chained to its target (§6.2), and `Append` became a method on a
`Writer` that holds the lock for a whole turn rather than a flat call carrying
`expectedSeq` (§14).

One thing the record did not anticipate: `AddCompaction`, `SetNotes`, and
`SetTodos` returned no error, so a durable history had no way to report a failed
commit. All three now return one, because §7.1 requires the turn to stop rather
than continue against state the next turn will not find.

### Phase 2: Rewind and physical-copy fork — done

- Add conversation rewind: the operation and the surfaces that offer it,
  including what it reports as *not* undone (§8.2). Landed, with `/rewind` in
  the TUI and the History picker in Desktop.
- Add independent child-session fork. Landed, with `/fork` in the TUI and the
  same Desktop picker, which offers both. Each surface shares one list between
  the two operations and changes only what it says about the choice.

Head selection and branch-aware replay are not listed: they landed in phase 1
as the mechanism rewind is built from, and are covered by the property tests
there.

This is the last phase. What was once a fourth — externalized content, shared
history segments, a database-backed store — is not planned, and is not
described here as though it were. Each was an optimization with no measurement
behind it, and §13 is honest that no measurement is collected. A design record
that carries unscheduled work reads like a commitment; if storage pressure ever
shows up, the case for changing this can be made then, against evidence.

## 19. Open Questions

Nothing here blocks a phase, because every phase has shipped. These are the
questions the implementation did not have to answer to work, and would have to
answer to do more.

### 19.1 Open

- Which tools may declare an interrupted call safe for automatic retry, and how
  is that idempotency qualified? Until this is answered, an interrupted call is
  reported to the user (§8.2) and never retried on their behalf.
- How long are hidden subagent bundles retained after the parent receives their
  result? They are kept indefinitely today.
- What reachability rule makes a journal prefix safe to prune, given branches
  and fork exports? Until it exists, §13 bounds nothing — which is a cost, not
  a defect, and the reason no pruning ships.

### 19.2 Resolved

**How does a surface tell a user what a rewind did not undo?** By naming the
tools that ran in the span being dropped while the point is still highlighted,
so the consequence is visible before the choice, and repeating it afterwards.
Both surfaces do this, and both reuse the same computation for fork with the
opposite wording: the original loses nothing, but the copy will not know that
work happened. A surface offering either without this is hiding the hazard §8.1
makes it responsible for.

Which completed content unit streams to history — a provider response item, a
portable `llm.Message`, or both through one adapter boundary — is already
answered by the existing type. `llm.Message` carries `ProviderState`, a
`{protocol, data}` pair holding the opaque reasoning state the producing
protocol requires back verbatim, alongside `Parts` for non-text content and
`Source` for background provenance. It round-trips a resume losslessly without
a second stored representation.

Journals that persist raw provider items do so because their portable message
type cannot carry opaque protocol state. That is not the situation here, and
storing wire payloads would also couple the journal to one vendor's format —
the neutrality §6.4 exists to protect. History stores the portable message, and
§6.3 needs no parallel vocabulary.
