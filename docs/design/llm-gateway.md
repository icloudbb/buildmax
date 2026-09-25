# Managed LLM Gateway

> **简体中文：** [阅读中文镜像](../zh-CN/design/LLM网关.md)

> **Audience:** contributors · **Status:** shipped for every surface; strict
> quota remains open
>
> Shipped: `internal/service/llmgateway` (catalog, name resolution, capability
> set, router, error classes), the `llm_model` catalog and `llm_call` ledger rows
> in `internal/infra/db`, the `internal/infra/llmremote` client,
> `buildmax-server model`, and all three routes in
> `internal/server/handlers/routes.go` including SSE streaming. A catalog target
> may declare any of the three wire protocols in
> [llm-provider-adapters.md](llm-provider-adapters.md). Quota runs at the §10
> visibility/soft-enforcement stage.
>
> **Sections 1, 4.2, 7, 10, and 12 were revised after this was written**, and the
> text below still describes the earlier shape in places. What replaced it:
>
> - A client is in one of two mutually exclusive modes, decided by whether
>   `auth.json` exists. Transport is a property of the session, not of a model
>   entry, and the two lists are never merged. `settings.yaml` describes only
>   what a signed-out session runs on.
> - Models are global to a deployment. Per-space model policy is **withdrawn**,
>   not pending, and the alias layer is gone: a client names a model by
>   `llm_model.name`, and `server.yaml` `llm.default_model` names the default.
> - The gateway routes are `/api/llm/models` and `/api/llm/completions`. Being
>   signed in is their whole authorization.
> - The `llm_call` ledger is attributed to a user. A run's space is reached
>   through `task_run_id`; a foreground call belongs to no space and is metered
>   against none.
>
> Task runs reach the gateway under `worker.llm.transport: buildmax`. The worker
> entry point `POST /api/worker/task-runs/{task_run_id}/llm/completions`
> authenticates with a run token — see
> [worker-run-token.md](worker-run-token.md) — so user, space, task, and run all
> come from the credential, and it accepts a call only while the run is
> executing. Such a worker is handed no upstream provider key. The server states
> the transport and model in the run's worker-API response; a run never chooses
> its own model.
>
> Not shipped: reserved quota enforcement and concurrency control (§10) do not
> exist; do not claim a strict spending ceiling.

This is an active P3 design. It follows the deployment direction in
[enterprise-deployment.md](enterprise-deployment.md), depends on worker trust
work from [trust-harness.md](trust-harness.md), and supplies model governance
data needed by [space-governance.md](space-governance.md).

## Contents

- [1. Decision](#1-decision)
- [2. Product Goal](#2-product-goal)
- [3. Current Baseline](#3-current-baseline)
- [4. Why A BuildMax Gateway](#4-why-a-buildmax-gateway)
- [5. Options Considered](#5-options-considered)
- [6. Architecture And Ownership](#6-architecture-and-ownership)
- [7. Model Catalog And Resolution](#7-model-catalog-and-resolution)
- [8. Managed Protocol](#8-managed-protocol)
- [9. Streaming, Retry, And Idempotency](#9-streaming-retry-and-idempotency)
- [10. Call Ledger, Quota, And Audit](#10-call-ledger-quota-and-audit)
- [11. Authentication And Security](#11-authentication-and-security)
- [12. Client Configuration And UX](#12-client-configuration-and-ux)
- [13. Provider Strategy](#13-provider-strategy)
- [14. Operational Consequences](#14-operational-consequences)
- [15. Delivery Plan](#15-delivery-plan)
- [16. Validation](#16-validation)
- [17. Out Of Scope For The Initial Release](#17-out-of-scope-for-the-initial-release)
- [18. Open Questions](#18-open-questions)
- [19. Recommended First Change](#19-recommended-first-change)

## 1. Decision

BuildMax will support two explicit LLM connection modes:

| Mode | LLM transport | Provider credential owner | Intended use |
|---|---|---|---|
| `direct` | CLI, Desktop, or worker calls an OpenAI-compatible endpoint | The machine running the Agent | Local-first, offline, BYOK, and deployments that already operate a gateway |
| `managed` | A BuildMax remote LLM client calls the BuildMax Server | The BuildMax deployment | Central credentials, space model policy, usage, quota, and audit metadata |

Managed mode is optional. Direct mode remains a complete, first-class path and
does not require a BuildMax Server. There is no automatic fallback between the
two modes because silently changing the path taken by prompts and source code
would violate operator expectations.

The Server endpoint is a **BuildMax inference protocol**, not a public
OpenAI-compatible passthrough. A new remote implementation of
`internal/core/llm.LLMClient` translates between the in-process contract and
that protocol. The Agent loop and tools remain where the run is executing.

A catalog target names the wire protocol it speaks, and the gateway builds the
matching client. OpenRouter, LiteLLM, or another operator-managed compatible
gateway still provides broad provider coverage under `openai_compatible`; a
target may instead speak OpenAI Responses (`openai`) or Anthropic Messages
(`anthropic`). The test a further adapter must pass is in section 13, and it is
unchanged: an adapter is justified by a capability the shared contract can
represent, not by a vendor having an API. See
[llm-provider-adapters.md](llm-provider-adapters.md).

The reason to own this gateway is **BuildMax governance**, not provider count:

- provider credentials do not need to be distributed to users or workers;
- a space selects stable model aliases instead of provider model identifiers;
- every managed call can be authorized, metered, limited, and correlated with
  a BuildMax run;
- provider routing can change without rewriting client configuration.

## 2. Product Goal

An operator of a private BuildMax deployment should be able to provide approved
models to a space without distributing provider keys. The operator should know
which space and user initiated a managed call, which approved model target was
used, how many tokens were reported, and whether the call succeeded.

A local user must still be able to run CLI or Desktop with only
`settings.yaml` and a provider or local inference endpoint. Managed mode must
not turn Server availability, authentication, or space membership into a
requirement for local use.

## 3. Current Baseline

The shared boundary already exists:

- `internal/core/llm.LLMClient` is the contract consumed by the Agent loop;
- `internal/infra/llm` is its OpenAI-compatible implementation and owns
  streaming, retries, timeouts, error classification, and usage capture;
- `internal/agentapp` resolves configured models and caches clients;
- `settings.yaml` contains the model list used by CLI and Desktop;
- `server.yaml` contains one `conversation.model`, used by the Server for Tier
  1 conversations and by workers for task runs.

The current call paths differ:

| Surface | Where the Agent loop runs | Where the provider call originates | Credential source |
|---|---|---|---|
| CLI / TUI | Local process | Local process | `settings.yaml` |
| Desktop | Desktop process | Desktop process | `settings.yaml` |
| Portal Tier 1 | Server | Server | `server.yaml` |
| Task run | Worker | Worker | `server.yaml` plus environment overrides |

Current quota usage is run-oriented. It aggregates task-run token fields and
task-title token fields. Tier 1 conversation calls and local calls do not
produce a complete server-side LLM call ledger. A gateway does not fix that by
itself; call-level persistence is part of the chosen design.

Authentication also has limits that the design must not hide. CLI and Desktop
keep the access and refresh tokens in the OS credential store by default, or in
`auth.json` itself on a machine with none — either way, a refresh flow now
exists, so a login survives longer than one access token, and a session can be
revoked. The access token itself still cannot be: there is no revocation list,
so an issued one works until it expires. Space membership is checked
server-side and can remove access to a space, but the access token must not be
described as an independently revocable gateway credential.

## 4. Why A BuildMax Gateway

### 4.1 Credential Custody

Direct mode requires the execution machine to hold an upstream credential.
That is appropriate for BYOK but unsuitable when an operator wants centrally
rotated credentials or wants workers to run without general provider access.

Managed mode replaces the upstream credential with a BuildMax credential whose
authority is limited by Server authorization and space model policy. It does not
make the client secretless.

### 4.2 Space Model Policy

Local model entries currently choose the provider URL and provider model. A
managed client instead chooses a stable alias such as `default`, `fast`, or
`reasoning`. The Server maps that alias to an operator-defined target and
rejects aliases not available to the space.

Clients must never submit an upstream URL, provider credential, or unrestricted
provider model identifier to the managed endpoint. This keeps model selection
inside the space authorization boundary and prevents the gateway from becoming
an authenticated SSRF proxy.

### 4.3 Usage And Quota

Every managed invocation creates or completes one call-ledger record. The
record supports usage display, quota evaluation, incident investigation, and
eventual cost reporting without storing prompt bodies.

A simple pre-call quota check is not a strict spending guarantee: concurrent
calls can all pass before their final usage is written, and the exact output
size is unknown before inference. The first version may reject spaces already
over quota and reconcile reported usage after the call. Strict enforcement
requires reservations, per-call output limits, and concurrency control before
the gateway can be described as multi-tenant-safe.

### 4.4 Provider Independence

Provider breadth alone is not a reason to build this component. The existing
client can already point at OpenRouter, LiteLLM, local inference, or another
OpenAI-compatible endpoint.

The Gateway still creates a useful seam: clients use a BuildMax protocol and
stable aliases, while the Server-side model router chooses an implementation of
`LLMClient`. A future native adapter can be introduced behind the router
without changing CLI, Desktop, worker, or Agent Core transport code.

## 5. Options Considered

| Option | Strength | Limitation | Decision |
|---|---|---|---|
| Keep direct calls only | Smallest system; best local privacy and availability | Provider keys, policy, and usage stay distributed | Retain as `direct` mode |
| Distribute model config from Server | Central aliases and onboarding | Credentials still reach clients; calls remain unmetered | Not a separate solution |
| OpenAI-compatible transparent proxy | Existing clients can change only `api_url`; useful to third-party callers | Exposes an unnecessarily broad public contract, leaks upstream semantics, and makes BuildMax policy/error evolution difficult | Not the initial endpoint |
| BuildMax managed inference protocol | Stable internal contract, explicit authorization, normalized errors, and room for provider capability evolution | Requires a remote client and protocol tests | Chosen |
| Implement all native provider SDKs now | Maximum access to provider-specific features | Permanent compatibility surface with no current core requirement | Deferred |
| Require LiteLLM and build no Server gateway | Outsources provider normalization | Does not integrate BuildMax identity, spaces, runs, or quota | Supported deployment option, not the product decision |

An OpenAI-compatible public endpoint can be added later if BuildMax intends to
serve third-party applications. That is a separate product and security
decision from supplying models to BuildMax clients.

## 6. Architecture And Ownership

```text
CLI / Desktop ── RemoteLLMClient ─┐
                                  │
Worker ──────── RemoteLLMClient ──┼─ HTTP auth entry points
                                  │          │
Portal Tier 1 ────────────────────┘          ▼
                                      LLM Gateway service
                                             │
                                model policy + call ledger
                                             │
                                             ▼
                                        Model router
                                      ┌──────┼──────┐
                                      │      │      │
                              OpenAI-compatible   future native adapters
                                      │
                         OpenRouter / LiteLLM / provider / local model
```

The diagram shows logical reuse, not an HTTP self-call. Server-owned Tier 1
inference calls the model router and metering service in-process. It must not
send a request back through its own public HTTP listener. A remote worker uses
the worker-authenticated HTTP entry point so that it does not need an upstream
credential.

| Concern | Owner |
|---|---|
| Messages, tool definitions, tool calls, usage contract | `internal/core/llm` |
| Agent loop and local tool execution | `internal/core/agent` |
| Local model assembly and remote-client selection | `internal/agentapp` |
| OpenAI-compatible provider calls | `internal/infra/llm` |
| Remote BuildMax protocol client | `internal/infra/llmremote` or equivalent infrastructure package |
| Alias resolution, space policy, quota coordination, call ledger | new `internal/service/llmgateway` |
| User and worker HTTP adapters | `internal/server/handlers` |
| Process wiring and provider credentials | `internal/bootstrap` and `internal/config` |

`internal/core` must not import Server, configuration, or provider packages.
The gateway consumes the core contract; it does not move network policy or
infrastructure into the domain layer.

## 7. Model Catalog And Resolution

The Server needs an operator-owned model catalog separate from the single Tier
1 model. Each catalog entry has:

- an opaque catalog ID;
- an operator-facing name;
- a provider type or client factory;
- the upstream endpoint and credential reference;
- the upstream model identifier;
- context window and timeout;
- declared capabilities;
- enabled state.

Space policy maps one or more stable aliases to catalog entries and identifies a
default. Aliases are the only model identifiers accepted from managed clients.
Resolution is therefore:

```text
(space_id, alias) -> authorized catalog entry -> LLMClient
```

The catalog is the `llm_model` table, edited with `buildmax-server model`. It is
not configuration: it holds provider credentials, which must not travel in a
file that a Kubernetes ConfigMap carries, and it changes while the server runs.
Credentials are stored in the row and read by one query, the one that builds a
provider client, so an operator's backup policy — not a config file — is what
governs them at rest.

Space policy belongs in the database too, because Space is its ownership boundary.
Until that exists, a deployment-wide alias map in `server.yaml` exposes a small
operator-approved set to every space. `conversation.model` remains the server's
own bootstrap model, so a deployment answers conversations before its catalog
has a single row.

Provider capabilities must be explicit rather than inferred from provider
names. The first capability set matches the current core contract: text chat,
tool calls, streaming text, and usage reporting. Requests requiring an
unsupported capability fail before an upstream call.

## 8. Managed Protocol

The exact route becomes authoritative only when it is registered in
`internal/server/handlers/routes.go`. The proposed user entry points are:

```text
GET  /api/spaces/{space_id}/llm/models
POST /api/spaces/{space_id}/llm/completions
```

The worker entry point is scoped to a task run:

```text
POST /api/worker/task-runs/{task_run_id}/llm/completions
```

Both HTTP adapters call the same service. The user route authenticates the
current user and verifies space membership on every call. The worker route
derives the space, task, and run from server state; it does not trust attribution
fields supplied by the worker.

### 8.1 Request

The versioned request DTO contains only fields BuildMax understands:

```json
{
  "call_id": "client-generated idempotency key",
  "model": "fast",
  "messages": [],
  "tools": [],
  "stream": true,
  "call_profile": "agent_turn",
  "metadata": {
    "surface": "desktop",
    "session_id": "optional correlation value"
  }
}
```

The DTO mirrors the semantic content of `core/llm`, but it is a versioned wire
contract rather than a direct JSON exposure of Go structs. Unknown request
fields are rejected in the initial version. Provider-specific request bodies,
credentials, URLs, and arbitrary generation parameters are not accepted.

`call_id` identifies one logical invocation. It supports duplicate detection
when a connection fails before a client knows whether the Server accepted the
call. It is not permission to replay a partially streamed generation.

`call_profile` is what the caller says the call is for — `agent_turn`, `title`,
`compaction`, `evaluation`, or `probe`. It is operational input, not
authorization input: the Server combines it with the approved target's own cache
policy, so naming a profile cannot select a cache mode, a retention, or a cache
key the operator did not approve. The field is additive and may be omitted; the
Server then has no evidence the prefix will be read back, which is not a reason
to buy a cache write. A non-empty value the Server does not know is rejected
rather than absorbed, so a newer client cannot believe it asked for one thing
and be charged for another. See
[prompt-cache-control.md](prompt-cache-control.md).

Metadata is correlation context, not authorization input. The Server derives
user ID and space ID from authentication, and derives task-run identity on the
worker route.

### 8.2 Non-Streaming Response

```json
{
  "content": "...",
  "tool_calls": [],
  "usage": {
    "prompt_tokens": 100,
    "completion_tokens": 20,
    "total_tokens": 120
  }
}
```

The response deliberately returns BuildMax tool-call and usage shapes. It does
not expose an upstream response body or provider credential-bearing headers.

### 8.3 Streaming Response

Streaming uses typed SSE events, not byte-for-byte upstream chunks:

| Event | Payload | Meaning |
|---|---|---|
| `delta` | content delta | Text to deliver to the local stream sink |
| `result` | final content, tool calls, usage | Completes the `LLMClient` call |
| `error` | stable code, safe message, retryable flag | Terminates the call with an error |

The existing core contract exposes content deltas during streaming and returns
assembled tool calls after the stream finishes. The managed protocol should
preserve that contract instead of exposing provider-specific tool-call chunks.

Every event is flushed promptly. Client disconnect cancels the upstream
context. Reverse proxies used in supported deployments must disable response
buffering and allow an idle timeout longer than the configured model call
timeout.

### 8.4 Errors

The Server returns stable BuildMax error codes for authentication, space access,
unknown alias, unsupported capability, quota, timeout, cancellation, rate
limit, upstream authentication, upstream availability, and malformed upstream
responses.

Safe provider detail may be recorded in Server diagnostics, but raw provider
error bodies are not returned by default. They can contain account identifiers,
endpoint details, request fragments, or policy information. The remote client
maps the stable error classification into the same human-readable behavior the
local provider client provides.

## 9. Streaming, Retry, And Idempotency

Only the component directly calling the upstream provider owns provider retry
policy. In managed mode that is the Server-side provider client. The remote
client must not add another independent retry loop around a completed or
partially streamed gateway call.

The following rules apply:

1. The Server may retry an upstream call according to provider-client policy
   until the first response delta is emitted.
2. No layer automatically retries after a delta reaches the caller.
3. A duplicate `call_id` that is already running attaches only if the protocol
   explicitly supports attachment; otherwise it returns a conflict.
4. A duplicate completed `call_id` may return its stored terminal metadata, but
   replaying full generated content is not required in the first version.
5. Client cancellation propagates through the handler and router to the
   upstream request.
6. Each attempt and the logical call are distinguishable in Server diagnostics
   so retries do not inflate user-visible call counts.

Backpressure must be bounded. A slow or disconnected caller must not allow an
unbounded queue of provider deltas in Server memory.

## 10. Call Ledger, Quota, And Audit

Every managed logical call records at least:

| Field group | Data |
|---|---|
| Identity | call ID, space ID, authenticated user ID or task-run ID |
| Correlation | surface, session ID when supplied, task and run IDs when derived |
| Model | requested alias, resolved catalog ID, provider type, upstream model identifier |
| Timing | accepted, upstream started, first delta, completed timestamps |
| Outcome | status, stable error class, retry/attempt count |
| Usage | prompt, completion, and total tokens; whether usage is reported, estimated, or unavailable |

Prompt text, tool arguments, tool results, source code, and generated content
are excluded from the call ledger by default. Durable local traces continue to
own run detail. Any future opt-in body capture requires a separate retention,
redaction, access-control, and data-classification decision.

The ledger is the accounting source for managed calls. Existing task-run token
fields remain useful summaries, but aggregation must avoid counting the same
worker call once in the ledger and again through task-run totals.

Quota enforcement evolves in stages:

| Stage | Behavior | Guarantee |
|---|---|---|
| Visibility | Record actual reported usage after calls | Accounting only |
| Soft enforcement | Reject when recorded usage is already over limit; cap request duration and output | Concurrent calls may overshoot |
| Reserved enforcement | Reserve estimated budget before dispatch and reconcile after completion | Bounded overshoot |
| Multi-tenant control | Add per-space and global concurrency/rate limits | Protects Server capacity and limits noisy neighbors |

Until reserved enforcement and concurrency control exist, documentation must
not claim that the Gateway provides a strict spending ceiling or is safe for an
untrusted multi-tenant SaaS deployment.

Audit events record configuration and authorization actions such as model
policy changes, denied model use, and credential administration. High-volume
per-call operational records belong in the call ledger, not in a governance
audit log.

## 11. Authentication And Security

Managed inference increases the Server's data classification because prompts,
tool schemas, and tool results pass through it. The threat model must include:

- prompt and source-code exposure in logs, traces, crash reports, and metrics;
- credential leakage through configuration, headers, or upstream errors;
- cross-space model access;
- unbounded request bodies, tool schemas, streams, and concurrency;
- upstream endpoint SSRF caused by user-controlled routing;
- a worker using its infrastructure credential outside its assigned run;
- long-lived calls outlasting an access token.

Required controls are:

- TLS outside trusted local development;
- authentication and space authorization on every user call;
- server-derived task, run, space, and model policy on worker calls;
- operator-configured upstream endpoints only;
- request, message, tool-schema, response, timeout, and concurrency bounds;
- no prompt bodies in normal access logs or call-ledger records;
- redaction of credentials and safe error normalization;
- cancellation propagation and bounded stream queues;
- explicit retention policy for call metadata.

The shared worker token was not an adequate credential for a model gateway
because it was not scoped to one run, so the worker route never accepted it. It
takes a run token instead: a short-lived credential naming the user, space, and
run, minted when the run is dispatched. See
[worker-run-token.md](worker-run-token.md). The shared token has since been
removed from every worker route, so a run token is the only credential a worker
holds.

The user access token defaults to seven days, which is acceptable for early
trusted-deployment experiments but not a complete managed-client lifecycle.
Refresh has since shipped, so a login outlives one access token and a session
can be revoked. What is still missing before the feature is described as
production-ready is native secret storage, an absolute session lifetime, and an
audience-scoped credential — see
[client sessions and API credentials](../proposals/client-sessions-and-api-credentials.md).

## 12. Client Configuration And UX

Managed mode must be explicit in model configuration. Reusing `api_url` and
placing a login JWT in `api_key` would mix two credential systems, duplicate
tokens into `settings.yaml`, and make direct and managed behavior hard to
explain.

An illustrative shape is:

```yaml
models:
  - name: Space Fast
    model: fast
    transport: buildmax
    server_url: https://buildmax.example.com
    space_id: tm_example
```

The final fields become user documentation only when implemented. The remote
client reads authentication from `auth.json`; the model entry does not copy the
credential. `buildmax login` and the Desktop login flow establish the Server
identity, while model discovery lists the aliases available to the selected
space.

Direct entries keep their existing provider URL and API key shape. If a direct
and managed model have the same display name, the UI must also show transport
and Server/space context so the user can tell where data will go.

Fallback from managed to direct is never implicit. An operator or user may
configure two entries and choose between them, but a Server outage must not
silently redirect governed traffic to a personal provider key.

## 13. Provider Strategy

The model router constructs a client for the protocol the resolved target
declares — `openai_compatible`, `openai`, `anthropic`, or `ollama`. Operators
may point a compatible target at OpenRouter, LiteLLM, a compatible provider
endpoint, or local inference. BuildMax does not promise that every
provider-specific feature survives an OpenAI-compatible intermediary.

`ollama` is the one target type with **no credential**: it names a local runtime
the deployment reaches directly, and what authorizes the call is being able to
reach the daemon, which is a property of the deployment's network rather than of
the catalog. `llm.ProviderNeedsCredential` is where that exemption is
stated, and it is deliberately one provider wide — a hosted target missing its
key is a misconfiguration that must fail at selection rather than send an
unauthenticated request. Everything else about such a target is unchanged: an
operator adds it, a space reaches it only through an alias, and every call lands
in the ledger. See [local-ollama-provider.md](local-ollama-provider.md).

The adapters are specified in
[llm-provider-adapters.md](llm-provider-adapters.md). They changed nothing about
this protocol: `llmwire` carries messages, tool definitions, tool calls, and
usage, so it already described every protocol the gateway can serve.

A native provider adapter is justified only when all of the following hold:

1. the shared Agent product requires a capability, rather than one surface
   requesting a provider-specific shortcut;
2. `core/llm` has a provider-neutral representation of that capability;
3. the capability materially changes correctness, quality, latency, or cost;
4. supported compatible upstreams cannot provide it reliably;
5. the adapter has contract, streaming, usage, error, and tool-call tests.

Provider capability examples include structured content blocks, prompt-cache
controls, reasoning metadata, multimodal input, and provider-specific usage.
They are not part of the initial gateway merely because a vendor API exposes
them.

## 14. Operational Consequences

Managed mode turns the Server into an inference data plane. Compared with its
current coordination workload, it adds long-lived responses, more open
connections, upstream egress, backpressure, provider rate limits, and a larger
blast radius for Server outages.

Deployment work must therefore cover:

- reverse-proxy streaming and buffering configuration;
- request, upstream, idle, and graceful-shutdown timeouts;
- connection, goroutine, and memory sizing;
- global and per-space concurrency limits;
- provider egress policy and secret injection;
- health signals that distinguish Server, router, and upstream failures;
- metrics for call rate, first-token latency, duration, usage availability,
  cancellations, and stable error classes.

The additional network hop is expected to be small relative to inference but
compounds across an agentic run. Measurements, rather than that assumption,
must determine whether the supported deployment topology is acceptable.

Two operator needs surfaced once the catalog was in use. A provider's own error
never reaches the caller or the ledger, which keep only the stable class; it is
still the one clue to whether a key, a model id, or the provider is at fault, so
the gateway logs it server-side with the ledger row's ID. And a catalog key has
to be replaceable in place: rotating a leaked or expired key by adding a
renamed model would change the name every client selects. The row's credential
is therefore writable on its own (`set-key` on both command surfaces, `PUT
/api/admin/llm/models/{model_id}/credential`), and the router keys its client
cache on the row's revision so a replaced key reaches the next call.

## 15. Delivery Plan

### M1. Contract And In-Process Router

- Define provider-neutral router and model-resolution interfaces outside
  `internal/core`.
- Add an operator model catalog and deployment-wide default aliases.
- Reuse the existing OpenAI-compatible client behind the router.
- Route Server-owned Tier 1 calls through the service in-process without
  changing external behavior.
- Test aliases, disabled targets, unsupported capabilities, and secret-safe
  errors.

### M2. Call Ledger And Non-Streaming Gateway

- Add the call-ledger persistence contract and database implementation.
- Register the proposed user route with JWT and space authorization.
- Implement non-streaming managed calls and stable errors.
- Add a remote `LLMClient` and contract tests against the local implementation.
- Record usage and add visibility before claiming quota enforcement.

### M3. Streaming, Cancellation, And Idempotency

- Add typed SSE events and prompt flushing.
- Propagate disconnect cancellation.
- Enforce the retry rules and duplicate `call_id` behavior.
- Test content deltas, final tool calls, usage, upstream mid-stream failure,
  slow clients, and disconnects.

### M4. Client Discovery And Explicit Managed Mode

- Add space model discovery.
- Add explicit direct/managed configuration and selection to CLI and Desktop.
- Read managed credentials from `auth.json`, not `settings.yaml`.
- Document data flow, model precedence, token expiry, and failure behavior.

### M5. Worker Adoption And Governance

Done: the run-scoped entry point and credential, removing the provider
credential from a managed worker, soft quota enforcement, and audit events for
model-policy changes (model create, enable, disable). Space-run token counting
stays separate from the call ledger, so quota still aggregates `task_run` totals
only — do not add the ledger to that sum without resolving the double count.

Remaining:

- Add bounded requests, concurrency limits, and operator diagnostics.
- Add audit events for credential changes and denied model use.
- Pin an approved alias to a task or workflow at creation time, so a run's model
  is reproducible rather than whatever the deployment default is at dispatch.

### M6. Strict Quota Or Native Providers When Justified

- Add reservation and reconciliation if a strict spending bound becomes a
  product requirement.
- Add routing or failover only with explicit policy and accounting semantics.
- Add native provider adapters only when the criteria in section 13 are met.

Each milestone must leave direct mode working without a Server. User-visible
configuration and routes require corresponding updates to `guide/`,
`reference/`, the OpenAPI document, configuration examples, and architecture
documents when they ship.

## 16. Validation

The implementation is not complete until the following behaviors are covered:

- direct mode runs with no Server URL or login state;
- evaluation runs under `evaluation/` use direct mode, so results are not
  influenced by space model policy, alias resolution, quota state, or Server
  availability — the CLI adapter refuses a managed subject outright rather than
  measuring a different transport than the manifest claims;
- managed blocking and streaming calls produce the same core content, tool
  calls, and usage shape as a direct call to the same target;
- a user cannot invoke an alias outside the selected space's policy;
- the caller cannot choose an upstream URL or provider credential;
- Server-owned Tier 1 inference does not make an HTTP self-call;
- worker attribution is derived from the task run rather than request metadata;
- prompt and tool content do not enter normal logs or the call ledger;
- upstream retry stops after the first emitted delta;
- disconnect cancellation reaches the upstream request;
- slow consumers cannot grow memory without bound;
- incomplete or unavailable provider usage is represented honestly;
- one managed worker call is not counted twice;
- access-token expiry and space-membership removal fail with a clear error;
- direct and managed model entries are distinguishable in CLI, TUI, and Desktop.

Use repository task-runner checks in proportion to each implementation change.
The eventual gateway integration suite needs a deterministic fake upstream and
must not require a real model API key.

## 17. Out Of Scope For The Initial Release

- A public OpenAI-compatible API for arbitrary third-party clients.
- Native Anthropic, Bedrock, Vertex, or other provider adapters.
- Provider-specific features not represented in `core/llm`.
- Embeddings, image, audio, and other non-chat inference endpoints.
- Semantic response caching.
- Currency-denominated billing.
- User-supplied provider-key vaulting.
- Automatic direct fallback when the Server or provider is unavailable.
- Untrusted public multi-tenant SaaS operation.

## 18. Open Questions

These choices need resolution before their milestone begins:

1. Refresh has shipped, so a managed client's login now survives access-token
   expiry. What is still unresolved is where a native client keeps the refresh
   token, whether a session has an absolute lifetime, whether an access token
   carries an audience and scopes, and how an unattended caller authenticates at
   all — see
   [client sessions and API credentials](../proposals/client-sessions-and-api-credentials.md).
2. What minimum database shape represents space aliases? The catalog is settled —
   `llm_model`, credential in the row — but aliases are still a deployment-wide
   map in `server.yaml`.
3. Should the first quota milestone reserve an estimated maximum or explicitly
   ship as soft enforcement?
4. How long should terminal call metadata be retained, and which space roles may
   inspect it?
5. Which context identifiers may a local client attach for correlation without
   allowing it to impersonate a Server-owned task run?

## 19. Recommended First Change

Start with M1 only: introduce the model catalog, provider-neutral resolver, and
in-process router; place the existing OpenAI-compatible client behind it; and
route Tier 1 through that service without adding an HTTP endpoint.

This proves package ownership, alias resolution, capability checks, and secret
handling before the Server becomes an inference data plane. The gateway route,
remote client, call ledger, and worker credential then arrive as reviewable
steps instead of one coupled change.
