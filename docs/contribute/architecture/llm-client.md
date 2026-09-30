# LLM Client

> **简体中文：** [阅读中文镜像](../../zh-CN/contribute/architecture/llm-client.md)
> **Audience:** contributors · **Status:** current

## Purpose

`internal/infra/llm` implements the `llm.LLMClient` contract over the four LLM
wire protocols BuildMax speaks. It translates between BuildMax types and each
protocol's wire format, and owns everything that makes a real network call
survivable: timeouts, retries, error classification, and usage capture.

The **contract** lives in `internal/core/llm`; this package is one
implementation of it. The agent loop only ever sees the interface.

| `Config.Provider` | Protocol | Adapter |
|---|---|---|
| `openai_compatible` (default) | OpenAI Chat Completions | `openai_chat.go` |
| `openai` | OpenAI Responses | `openai_responses.go` |
| `anthropic` | Anthropic Messages | `anthropic.go` |
| `ollama` | Ollama `/api/chat` (local) | `ollama.go`, `ollama_inventory.go` |

Those four values are `llm.Provider*` in `internal/core/llm`, along with
`Providers`, `KnownProvider`, and `ProviderNeedsCredential`. One protocol name
is read from `settings.yaml`, stored in the model catalog, and recorded in the
call ledger, so it has one definition rather than one per surface.
`config.KnownLLMProvider` adds only the configuration boundary's own reading,
that an unset value means the default.

`client.go` is the only entry point: it selects an adapter and owns the parts a
caller depends on — the per-call timeout, the retry loop, and error
classification — so four protocols cannot drift apart on them. An adapter
performs one attempt and nothing else.

Rationale and the phases beyond this one:
[design/llm-provider-adapters.md](../../design/llm-provider-adapters.md) and
[design/local-ollama-provider.md](../../design/local-ollama-provider.md).

## The Contract It Implements

```go
// internal/core/llm
type LLMClient interface {
    ChatCompletionBlocking(ctx, req Request) (Completion, error)
    ChatCompletionStreaming(ctx, req Request, onDelta func(string)) (Completion, error)
    ContextWindow() int   // 0 = no windowing configured
}

type Request struct {
    Messages []Message
    Tools    []ToolDef
    Profile  CallProfile      // what the call is for
}

type Completion struct {
    Content       string
    ToolCalls     []ToolCall
    Usage         Usage
    ProviderState *ProviderState   // reasoning state, when the protocol has any
}
```

`Message`, `ToolDef`, `ToolCall`, `Usage`, `Completion`, and `ProviderState` are
all defined in `internal/core/llm` — not in this package, and not in the
`internal/core/*` domain packages, which hold domain entities and repository
contracts instead.

`Completion` is a struct rather than a longer return list because every
capability the contract has gained wanted another slot, and a fifth positional
value is where that stops being readable. `Completion.AssistantMessage()` is the
history entry a turn becomes, so the agent loop appends it verbatim and no layer
in between has to know reasoning state exists.

`Request` is a struct for the mirror-image reason on the way in. It exists to
carry `CallProfile`: what the call is *for*, which the request itself cannot
show. A title generation and the first turn of a long tool-calling run send the
same shape of messages, and prompt caching charges them differently — a cache
write costs more than ordinary input and only repays itself if a later call
reads it. The profile is the caller's answer to "will anything read this again".

| Profile | Set by |
|---|---|
| `agent_turn` | `core/agent.RunLoop` — the prefix goes out again next iteration |
| `title` | `agentapp.SessionManager.GenerateTitle` |
| `compaction` | `agentapp.LLMCompactor` and the note checkpointer |
| `evaluation` | a harness calling *about* a run rather than *as* one |
| `probe` | a single question with no reuse: `WebFetch`, a hook's model call |

It is a typed field rather than a `context.Context` value because a charged
provider behavior has to be visible to the callers and tests that reason about
it. `CallProfile.Valid()` refuses an unknown value rather than defaulting: the
default it would fall to is the one that spends money.

## Construction

```go
client, err := llm.NewClient(llm.Config{
    Provider:      m.Provider,        // "" = openai_compatible
    APIKey:        m.APIKey,
    BaseURL:       m.APIURL,
    Model:         m.Model,
    Surface:       "cli",            // cli, desktop, server, or worker
    ContextWindow: m.ContextWindow,   // 0 = no windowing
    MaxTokens:     m.MaxTokens,       // 0 = the adapter's own default
    CallTimeout:   d,                 // 0 = DefaultCallTimeoutSecs
})
```

`Config` is this package's own struct, populated from a `models:` entry in
`settings.yaml`, the `conversation.model` block in `server.yaml`, or a catalog
target resolved by `internal/service/llmgateway`. When `ContextWindow` is zero,
`lookupContextWindow` falls back to a built-in table of known model sizes — that
table is keyed by OpenRouter-style identifiers, so a native model id normally
needs `context_window` set explicitly. The Ollama provider is the exception: it
asks the daemon instead, because a local daemon can answer for the model it
actually holds.

An unknown provider is an error rather than a fallback: a model that cannot be
reached the way it was configured fails at selection instead of sending its
prompt somewhere the operator did not name.

Every provider request identifies BuildMax as `buildmax/<version> (<surface>)`
in its `User-Agent`. The surface is runtime-owned rather than user-configurable:
CLI, Desktop, the managed server, and workers send their respective origin. A
managed gateway preserves the original CLI, Desktop, or worker surface and adds
`; gateway`, so its upstream request reads, for example,
`buildmax/0.1.0 (cli; gateway)`.

## Normalizing History

Canonical history is one permissive shape: a system message, user and assistant
turns, and one `role: "tool"` message per result. Each adapter turns that into a
valid request for its protocol, and the Anthropic adapter carries most of the
work — it lifts system messages into the top-level parameter, merges each run of
tool results into one user message, drops a tool call whose result was trimmed
away and a result whose call was, skips empty text, and supplies the required
`max_tokens`.

Those repairs live in the adapter deliberately. Making `core/llm`, `TrimHistory`,
or compaction enforce the strictest protocol's rules would charge the other two
for constraints they do not have.

The Responses adapter runs **stateless**: it sends the whole input every call and
sets `store: false`. BuildMax owns history, trimming, compaction, and session
persistence, so server-side conversation state would compete with all four.

The Ollama adapter carries the other repair. Its protocol has no tool-call
identifiers: a result is answered by tool name, so the adapter resolves each
`ToolCallID` against the calls of the assistant message before it and drops a
result whose call is gone. Identifiers on the way back are minted by position in
the conversation — `call_<n>` continuing past whatever the request already
contained — so a session it writes stays unambiguous for a protocol that does
pair by identifier.

## The Local Context Window

The Ollama protocol is the one that silently truncates rather than refusing an
over-long prompt, so its adapter sends `num_ctx` on **every** call, and it is
the same number `ContextWindow()` reports. An entry that sets `context_window`
decides it; one that does not gets the daemon's answer for that model, capped at
`config.DefaultContextWindow`, because a model's full trained length can exceed
what the machine can allocate. A failed probe falls back to the default and logs
— what it must never do is leave the field out.

`ollama_inventory.go` also serves diagnostics rather than runs: `OllamaInventory`
lists what is pulled and `OllamaShow` reports one model's window and
capabilities, which is what `buildmax doctor` and `buildmax models --local` read.

## Reasoning State

`Config.Reasoning` is an effort level — `off`, `low`, `medium`, `high` — and any
level but off also replays the reasoning on later turns. Anthropic gets adaptive
extended thinking at that effort with `display: omitted`; Responses gets the
effort plus `include: ["reasoning.encrypted_content"]`, which is the only way to
replay reasoning when nothing is stored server-side. Chat Completions has no such
state and ignores the setting. An unrecognized level fails `NewClient` rather
than reaching a provider.

What comes back is recorded on the assistant message as `ProviderState`, an
opaque payload tagged with the protocol that produced it. Three properties
follow, and each is load-bearing:

- **It is never read outside its adapter.** A signature covers the content, so
  rewriting it is worse than dropping it.
- **It never becomes content.** Thinking is not an answer; putting it in the
  transcript would make it indistinguishable from a conclusion.
- **A foreign tag is discarded, not sent.** That is what lets a session stay
  portable across providers while carrying state that is not: continuing under a
  different protocol loses reasoning continuity and nothing else.

State that cannot be encoded is dropped rather than half-written, and a stored
payload that no longer parses replays as no state at all. In both cases the turn
proceeds without continuity, which is exactly what a protocol without reasoning
does anyway.

## Prompt Caching

`Config.CacheControl` is the target's policy — `auto` (the default), `off`, or
`force`, plus a retention — and `Request.Profile` is what the individual call is
for. `resolveCacheDecision` combines them with the protocol's capability, and
only the result reaches a request.

The profile is the half configuration cannot supply. Under `auto`, only an
`agent_turn` asks for caching: its prefix goes out again on the next iteration,
which is the case a cache write is priced for. A title, a compaction summary, or
a probe is asked once and never asked again with the same prefix, so a write
bought for one is a straight loss. A profile this build does not recognise is
treated as unknown reuse, which does not justify a write; a caller that means to
pay for one says `force`.

Capability belongs to a target, and a direct entry has only its provider to go
on:

| Provider | Request controls | Reported as | Retention |
|---|---|---|---|
| `anthropic` | Breakpoints — nothing is cached unless the request says where | `supported` | `5m`, `1h` |
| `openai` | A scoped `prompt_cache_key`; Responses caches on its own either way | `supported` | `24h` |
| `openai_compatible` | None — speaking the protocol is not a promise to implement its cache fields | `unsupported` | — |
| `ollama` | None — a local runtime reuses its own cache | `unsupported` | — |

Retention vocabulary is per protocol. `5m` and `1h` mean something to Anthropic
and nothing to the Responses API, and `24h` the other way round, so each refuses
the other's rather than passing it through to be ignored.

`force` on a target with no request controls is refused at construction: serving
it as no caching at all would answer a question nobody asked. `auto` is accepted
everywhere, because most targets are like this and erroring would make the
default mode unusable. A retention the protocol does not document is refused for
the same reason — better a named failure than a field silently served at some
other length.

### The OpenAI cache key

The Responses API caches whether or not it is asked to, so the key BuildMax
sends does not turn caching on — it decides which prefixes are looked up
together. That makes it a correctness concern rather than a security one, and
the failure mode is a bucket shared by prompts that never match, which is a
bucket that never hits.

`deriveCacheKey` hashes exactly the things that all have to match for a hit to
be possible: the credential, the model, the caller's scope, and fingerprints of
the system prompt and the tool definitions in the order they are sent. Fields
are length-delimited so two different splits of the same bytes cannot collide,
and the whole thing carries a version prefix so a build that changes the
derivation cannot share a bucket with one that has not.

Nothing goes in that would leak or fragment for no reason. The credential is
hashed rather than carried; raw prompts, messages, workspace paths, and
usernames stay out entirely. The result is derived per request and never
persisted, logged, or returned — it appears in one outbound field and nowhere
else.

`Request.CacheScope` is the caller's bucket discriminator. It is empty for a
direct call, where the credential is already the user's own account. For a
managed call the gateway sets it from the authenticated space, because spaces
granted the same approved model share one credential and would otherwise share
one bucket. It is never accepted from a client: a caller that could name its own
scope could aim at another space's.

### Anthropic breakpoint placement

On Anthropic the resulting request carries two breakpoints: a static one on the
system prompt, which covers the tools and instructions that are identical on
every call in a run, and the top-level rolling one, so the next turn reads the
whole prefix rather than only the part before the conversation. The second does
not replace the first — automatic lookback only finds a prefix that was
previously written near the rolling endpoint. With no system prompt the static
breakpoint moves to the last tool definition, because otherwise the only
cacheable boundary left lands after a user message that changes every turn.

Cached counts are reported by all three and land on `core/llm.Usage` as
`CacheReadTokens` and `CacheWriteTokens`. They are a **breakdown of
`PromptTokens`, not an addition to it**. Anthropic reports cached input apart
from `input_tokens`, so its adapter adds it back before reporting a prompt total;
the OpenAI protocols already include it. Getting that wrong in either direction
misreports what a run cost.

From there the counts follow the same path as the prompt and completion totals:
`agent.RunStats` accumulates them across a run, `agent.Event` carries the running
figures on `llm_start`/`llm_end`, the JSONL trace records them on `llm_end` and
`run_end`, the session file keeps the per-session totals, and `agentapp.RunResult`
and `RunUsage` hand them to the CLI, Desktop, and any other surface. A managed
call carries the same counts over `llmwire.Usage` and onto the `llm_call` ledger
row, which the space run-ledger route and Portal's run-spend view read back.

Zero is not a miss. A provider that reports no cache counts is indistinguishable
from one that missed, so surfaces show the breakdown only where a provider
actually sent one rather than printing a `0 / 0` nobody measured.

## Image Input

`Config.Vision` says the model accepts images. It exists because a model without
image support rejects a request carrying one rather than ignoring it, and the
producer — an MCP server returning a screenshot — cannot know which model it is
talking to.

A message carries images in `Parts`, with `Content` holding the text that
describes them. When `Vision` is false, an adapter sends the text alone, which is
still a complete tool result. When it is true, placement follows the protocol:

| Protocol | Where a tool's image goes |
|---|---|
| Anthropic | Inside the `tool_result` block, where the protocol accepts it |
| Chat Completions, Responses, Ollama | A short user turn immediately after the tool result, because none accepts image content on a tool message |

The follow-up turn carries a one-line preamble. Without it the images arrive as a
user turn with no explanation, which reads as though the user sent them.

## Retries

Both call methods retry up to `maxRetryAttempts` (3) with a backoff of 1s, 2s,
4s:

| Retried | Never retried |
|---|---|
| Rate limit (429) | Context cancellation or deadline — the caller gave up |
| Server errors (500, 502, 503, 504) | Auth errors (401, 403) — needs user action |
| Network-level errors (connection refused, DNS) | Bad request (400) — retrying cannot help |
| | A failure an adapter marked permanent: a local daemon that is not running, a model that is not pulled |

**Streaming stops retrying once a delta has been emitted.** Retrying after the
user has already seen partial output would duplicate it, so a mid-stream failure
surfaces as an error rather than a second attempt.

Errors are wrapped by `wrapLLMError` with a human-readable classification, which
is why a bad key produces a comprehensible message instead of a raw HTTP error.

Both the retry decision and the classification read `apiError`, a neutral shape
each adapter converts its own library's failure into. The original error is kept
and unwrapped, so a caller that does know a specific library's error type can
still reach it.

Only the caller knows where a refused key is fixed, so `Config.CredentialHint`
supplies the next step for a 401 or 403. Empty means a local model entry and
reads "check api_key in settings.yaml"; the server's client factory names the
catalog model and its `model set-key` command, or `conversation.model.api_key`
for the derived conversation target.

A caller outside this package tells refusals apart with `errors.Is` against
`core/llm.ErrProviderAuth` (401, 403) and `core/llm.ErrProviderRateLimited`
(429), which `apiError` matches by status. The managed gateway classifies with
them, so it never reads the provider's text or imports this package.

## Usage Capture

The Chat Completions library does not surface token usage from stream chunks, so
`usageCaptureTransport` inspects the response body as it streams past and parses
the usage block when one appears. This is why streamed runs still report token
counts.

The workaround is confined to that one adapter. The Responses and Anthropic
adapters read usage from their own event streams and need nothing like it.

Usage is normalized to `core/llm.Usage` by each adapter. The Anthropic protocol
reports no total, so its adapter computes one — metering reads `TotalTokens`,
and leaving it zero would report a call that cost nothing. `CacheReadTokens` and
`CacheWriteTokens` are part of that canonical shape and travel with every result,
blocking and streamed alike.

## Per-Call Timeout

`CallTimeout` wraps each individual attempt in `context.WithTimeout` — it bounds
one call, not the whole run. A run with many tool-calling iterations is bounded
by `MaxIter` in the agent loop, not here.

## Dependencies

- **Uses**: `internal/core/llm` (contract and message types),
  `github.com/sashabaranov/go-openai` (both OpenAI protocols),
  `github.com/anthropics/anthropic-sdk-go`. The Ollama adapter needs no client
  library: three endpoints and one newline-delimited stream are not worth a
  module dependency.
- **Used by**: `internal/agentapp` (client cache), `internal/bootstrap`
  (Tier 1 conversation client)

## Notes

- Any OpenAI-compatible endpoint works by changing `BaseURL` alone — OpenRouter,
  Azure, a local vLLM or LM Studio. `Provider` is only needed when the endpoint
  speaks a different protocol. A local Ollama daemon serves one of those too,
  but `ollama` is the right value for it: the compatible endpoint cannot set the
  context window.
- The load-bearing test is the cross-adapter conformance suite in
  `conformance_test.go`: one logical reply is encoded by each protocol's fixture
  and read back through each adapter, and the canonical content, tool calls, and
  usage must come out identical.
- Because the agent depends on the interface rather than this struct, tests
  substitute a fake client without touching the network.
- See also: [Agent Loop](agent-loop.md), [Configuration](config.md).
