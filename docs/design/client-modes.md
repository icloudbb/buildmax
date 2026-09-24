# Client Modes: Local And Managed

> **简体中文：** [阅读中文镜像](../zh-CN/design/客户端模式.md)

> **Audience:** contributors · **Status:** implemented
>
> This record revised decisions in [llm-gateway.md](llm-gateway.md); where the
> two disagree, this one is current. Sections 7, 12, and parts of 1, 4.2, and 10
> of that record are superseded — see [§10](#10-what-this-supersedes).
>
> It is kept rather than folded away because what it decides spans more than the
> gateway: which models a client has at all, what `settings.yaml` is for, how
> Desktop opens, and who a call is booked against. Code across seven packages
> cites its section numbers.

CLI and Desktop run in one of two modes. Which one is in effect is decided by a
single fact — whether a login exists — and everything else follows from it.

## Contents

- [1. Decision](#1-decision)
- [2. What This Fixed](#2-what-this-fixed)
- [3. Mode Resolution](#3-mode-resolution)
- [4. Model List](#4-model-list)
- [5. Models Are Global To The Deployment](#5-models-are-global-to-the-deployment)
- [6. The Alias Layer Is Removed](#6-the-alias-layer-is-removed)
- [7. The Default Model](#7-the-default-model)
- [8. An Expired Login Does Not Silently Fall Back](#8-an-expired-login-does-not-silently-fall-back)
- [9. Usage Is Attributed To A Person](#9-usage-is-attributed-to-a-person)
- [10. What This Supersedes](#10-what-this-supersedes)
- [11. Where It Lives](#11-where-it-lives)
- [12. Not In Scope](#12-not-in-scope)
- [13. A Session Stays In The Mode It Began In](#13-a-session-stays-in-the-mode-it-began-in)

## 1. Decision

| | Local mode | Managed mode |
|---|---|---|
| Trigger | no stored login | a stored login (§3) |
| Model list | `settings.yaml` `models[]` | fetched from the server |
| Provider credential | on this machine | never leaves the server |
| LLM call | this machine calls the provider | this machine calls the server |

The two are **mutually exclusive**. A managed session does not see local model
entries, and a local session does not see server models. Leaving managed mode
is `buildmax logout`; entering it is `buildmax login`.

Three properties this buys, in order of importance:

- **One fact decides the mode.** No mode flag, no remembered preference, no
  precedence rule between them. A user asking "which mode am I in" has one
  place to look, and so does the code.
- **No merged list.** A union of local and server models would need conflict
  rules for duplicate names, a precedence order, and a story for what `--model`
  means when both match. None of that has to be designed if the list has one
  source.
- **No implicit fallback survives.** A server outage cannot redirect a prompt
  to a personal provider key, because in managed mode there is no local entry
  to fall back to. This was already the gateway's rule; a single-source list
  makes it structural rather than enforced.

The cost is that working offline requires `buildmax logout`. That is accepted:
it is one explicit command, and it is honest about what changed — prompts now
go somewhere else.

## 2. What This Fixed

Managed mode used to be configured by hand-editing `settings.yaml`:

```yaml
- name: Space default
  model: default
  transport: buildmax
  server_url: https://buildmax.example.com
  space_id: tm_example
```

Every field except `model` was something the login had already established, and
`buildmax models --space <space_id>` printed this block for the user to copy —
the design gap stated out loud.

The root cause was structural: **`settings.yaml` is load-only.** The only writer
in the repository is `buildmax init`, which renders the whole file from a
template; `internal/config` has no save function. So a stateful action (login)
and a configuration edit (using a space model) were never connected, and could
not be.

It also produced dangling state. After `buildmax logout`, the managed entries
stayed in `settings.yaml` and `buildmax doctor` reported them as missing a
login. Configuration held something that depended on session state, and the two
went out of step exactly as that always does.

## 3. Mode Resolution

A stored login is the only input. It is present or it is not.

`auth.json` was that login whole when this record was written. The OS
credential store since took the access and refresh tokens, leaving the file the
non-secret half — server URL, user, email — so "is there a login" is now
`auth.Info` reading both halves rather than one `os.Stat`. What the mode turns
on did not change; where the secret sits did.

Desktop's third mode flag is gone: `mode.go`, `desktopState`, and the
`UseLocalMode` / `ConnectToServer` bindings were deleted. The comment that used
to sit in `GetAuthStatus` — "a usable login wins over a remembered local choice"
— was a patch over having two sources of truth, and it went with the second
source. Desktop now opens straight into the agent, as the CLI always has, and
signing in is an action in the account menu rather than a gate on startup.

Startup therefore has no mode question to ask. A stored login means managed;
nothing means local.

**Stored, not usable.** `auth.Info` reports a login whose credentials are spent
as signed out, which is the right answer for a command deciding whether to offer
an account. Reading the mode from it would turn an expired session into local
mode on its own — the silent redirection §1 and §8 exist to prevent. So the mode
comes from `auth.StoredLogin`, which answers only whether credentials exist;
whether they still work is [§8](#8-an-expired-login-does-not-silently-fall-back).

## 4. Model List

The server returns **a `ModelEntry` list with the credential fields removed**:
`name`, `context_window`, `vision`, the current `pricing`, and which one is the
default. No `api_url`,
no `api_key`, no endpoint, no upstream model identifier.

One shape means the model panel, the default-model rule, the status line, and
`buildmax models` have one code path, and the modes differ only in where the
list came from.

Only what the client acts on crosses. It compacts a session against the context
window and sends an image only to a model that can read one, so those two must
be known locally. Reasoning effort, output cap, and cache policy are the
operator's target policy and stay on the server.

Price crosses too. It first stayed on the server, on the reasoning that the
ledger already records the cost and a local figure would be a second answer.
Exploratory testing on 2026-09-24 found the cost of that: every signed-in
surface — the session footer, `buildmax info`, `buildmax usage`, Desktop's
`/info` — reported "not priced" for calls the ledger had priced, and no view a
user could open showed the spend. Rates are not secret; a user sees what their
calls cost either way. So the list carries each model's rates as decimal
strings, and the client prices each call with the same `EstimateCost` the
ledger uses. For the same usage and rates the two agree; the ledger, which
snapshots the rates at acceptance, stays authoritative when a price changes
during a session, and the Portal usage page reads the ledger (§9).

`transport` on a model entry went away with all this. It was a per-entry
property because one list held both kinds; the mode says which it is now, and a
list has one source.

## 5. Models Are Global To The Deployment

A space is a collaboration boundary — shared issues, agents, workflows. It is
**not** a model authorization boundary. Every model in the catalog is available
to every user of the deployment.

This is a deliberate narrowing of [llm-gateway.md §4.2](llm-gateway.md), which
planned per-space model policy in the database. That capability is withdrawn,
not deferred. Gating models per space would mean a user who belongs to several
spaces has to declare which space a prompt is for — a workspace-selection concept
this product does not have and is not adding.

The code was already deployment-wide when this was decided: `llm.aliases` in
`server.yaml` exposed the same set to every space, and the per-space database
policy was never built. So this deleted an unbuilt abstraction rather than
changing behavior.

## 6. The Alias Layer Is Removed

`llm.aliases` and `llm.default_alias` are gone from `server.yaml`. Clients name
a model by `llm_model.name`. The `llm:` block itself stays, holding the one key
that survives: `default_model` ([§7](#7-the-default-model)).

The alias layer existed to give spaces stable names that survive provider
routing changes. `llm_model.Name` already does that job: it is
`uniqueIndex; not null`, operator-facing, and unique across the deployment.
Repointing a name at a different upstream is editing that row's `api_url` and
`model` — the name never moves. An alias-to-ID map on top of a unique name is
indirection with no second job, and the resolver already conceded as much: it
described aliases as the space-facing half of something that could address
catalog entries directly.

With spaces out of the resolution path, that half has nothing left to be.
Resolution goes from:

```text
(space_id, alias) -> catalog ID -> LLMClient
```

to:

```text
name -> llm_model row -> LLMClient
```

## 7. The Default Model

Both modes need to answer "which model does a new session start with", and both
fall back to the first entry when nothing says otherwise.

- **Local:** a new top-level `default_model` key in `settings.yaml`, naming an
  entry in `models[]`.
- **Managed:** `llm.default_model` in `server.yaml`, naming a row in
  `llm_model`. The server resolves it and marks that entry in the model list it
  returns, so the client still just reads a flag.

The default is configuration, not catalog state. [§7 of
llm-gateway.md](llm-gateway.md) argues the catalog is not configuration, but its
reason is that the catalog holds provider credentials, which must not travel in
a file a ConfigMap carries. A model name is not a credential, so that reason
does not reach this field.

Two things do reach it. First, `server.yaml` already holds the deployment's
other model choices — `conversation.model` for Tier 1 and `worker.llm.model`
for task runs. Putting the client default anywhere else splits one question,
"what does this deployment default to", across two places. Second,
`worker.llm.model` falls back to this value when empty; if the default lived in
the database, `server.yaml` could no longer explain its own behavior without a
query.

Restarting to change it is not a new cost: `conversation.model` already
requires one, and promoting a new default model is a deliberate,
everyone-sees-it change rather than a routine operation.

The resolution chain is therefore: an explicit name, else `llm.default_model`,
else the first catalog row. A `default_model` naming a row that does not exist
**stops the server at startup**, for the same reason the empty-catalog check
does — it parses cleanly and would otherwise fail every session at its first
call.

`/model` stays session-scoped and is not persisted, which is what the code
already did. A model choice is a property of the conversation you are having,
not a setting; the next session starts from the default again. Nothing needs to
be written to disk to switch models, which is what makes switching cheap.

## 8. An Expired Login Does Not Silently Fall Back

A login can be present but dead: the refresh token expired, or the session was
revoked. Managed mode must not quietly become local mode when that happens —
that is precisely the redirection of governed traffic that the no-fallback rule
exists to prevent.

The surface says the login expired and offers one action: return to local mode.
Taking it **deletes `auth.json`**, which is what actually returns the client to
local mode, since the file's presence is the mode. Until the user takes it, the
session does not run.

On the CLI that action is `buildmax logout`, named in the error rather than
prompted for — a command reads the same in a terminal and a script, and a
prompt would have nothing to read from in the second. Desktop offers a button,
since it already has somewhere to put one.

Deleting the file rather than marking it stale is what keeps §3 true. A retained
but invalid `auth.json` would be a third state, and every reader would need to
know the difference between "has a login" and "has a working login".

Only the user deletes it. The client once cleared `auth.json` itself when the
server rejected a refresh, which made the *next* command run in local mode
without anyone choosing it — the redirection this section forbids, one command
late. A rejected refresh is now reported as `ErrLoginExpired` and the file
stays until the user signs in again or out.

An unreachable deployment is not an expired login. The login still works, and
the offered way out of an ended one — signing out — would discard it. So the
failure has three classes, each with its own next step: `ErrLoginExpired` (sign
in again, or out), `ErrAccountDisabled` (an administrator must re-enable it, or
sign out), and `ErrServerUnavailable` (try again; the login is kept). Desktop
shows the ended-login screen only for the first two; an outage is a banner that
retries on its own. Exploratory testing on 2026-09-24 found Desktop treating
every failure as the first class, which pushed users to destroy a working
login during a transient outage.

## 9. Usage Is Attributed To A Person

The `llm_call` ledger records the user. `space_id` is **dropped from the row**,
not made nullable: the ledger already carries `task_run_id`, and run → task →
space reconstructs the space whenever the question is asked. A space column on
every call would be a denormalization that only a foreground call — which
belongs to no space — could fail to fill.

The composite unique index `idx_llm_call_client` was rebuilt to lead with the
user key, which is also the right scope for the idempotency key it guards: the
key belongs to the caller who sent it.

Reading a run's ledger is authorized by authorizing the run — it belongs to
exactly one space — so `ListLLMCallsByTaskRun` takes only the run and the handler
checks ownership first.

A foreground call belonging to no space also means no space's usage includes it.
`GET /api/usage`, the caller's personal view, therefore adds `managed_calls`: the
caller's own calls outside any task run over the same period, summed by rate
snapshot and priced with the ledger's formula. The space-scoped route does not,
because one member's sessions are not the space's spend.

Quota per space is out of scope here. It returns only if space workspace
selection is ever added, because a per-space ceiling needs each call to belong to
exactly one space, and today a foreground call does not.

## 10. What This Supersedes

In [llm-gateway.md](llm-gateway.md):

| Section | Was | Is now |
|---|---|---|
| §1 Decision | transport is a per-model-entry property | mode is per client, decided by `auth.json` |
| §4.2 Space Model Policy | per-space model authorization in the database | withdrawn; models are deployment-global |
| §7 Model Catalog And Resolution | `(space_id, alias)` resolution, alias map in `server.yaml` | `name` resolution against `llm_model` |
| §10 Call Ledger | space-scoped ledger rows | user-scoped; space reached through the run |
| §12 Client Configuration | hand-written `transport: buildmax` entries | server-supplied list, no client-side model config |

The gateway record keeps its own reasoning for everything those sections did not
decide, and its status block summarises what changed here.

## 11. Where It Lives

| Decision | Code |
|---|---|
| Mode resolution, and fetching what a deployment offers | `internal/interface/auth/models.go` |
| Transport per app rather than per entry | `AppConfig.ManagedServerURL`, `LLMClientCache.build` |
| `settings.yaml` shape, `default_model` | `internal/config/config.go` |
| Name resolution, no alias layer | `internal/service/llmgateway/resolve.go` |
| `llm.default_model`, startup validation | `internal/config/server_config.go`, `internal/bootstrap` |
| Global gateway routes | `internal/server/handlers/routes.go`, `internal/server/handlers/llm.go` |
| User-scoped ledger | `internal/infra/db/llm_call.go`, `internal/core/llmgateway/call.go` |
| Expired login, disabled account, unreachable deployment | `auth.ErrLoginExpired`, `auth.ErrAccountDisabled`, `auth.ErrServerUnavailable`, `internal/interface/cli/mode.go`, Desktop `AuthStatus.Expired` / `Unavailable` |
| Pricing on the model list | `llmwire.ModelPricing`, `internal/server/handlers/llm.go` |
| Personal managed-call totals | `SummarizeForegroundLLMCalls`, `internal/server/handlers/space/usage.go` |
| A session stays in its mode | `session.Meta.PromptDestination`, `AgentApp.bindPromptDestination` |

The user-facing half is
[manual/models-and-modes.md](../../manual/models-and-modes.md); the fields are in
[reference/configuration.md](../reference/configuration.md).

One thing the implementation found that the design did not anticipate:
`auth.Info` reports a login whose credentials are spent as signed out, which is
the right answer for a command deciding whether to offer an account. Reading the
mode from it would therefore have turned an expired session into local mode on
its own — exactly the silent redirection §1 and §8 exist to prevent. Stored
credentials decide the mode; whether they still work is a separate question with
its own answer. `auth.StoredLogin` is that distinction, and
`TestResolveModelSourceReportsAnExpiredLogin` is the guard.

## 12. Not In Scope

- **Space workspace selection.** Without it, per-space quota and per-space model
  policy cannot be built, and both stay out. A per-space ceiling needs each call
  to belong to exactly one space, and a foreground call belongs to none.
- **Migration.** BuildMax is alpha; `AutoMigrate` applied the new row shapes and
  no compatibility path was owed to an existing deployment.
- **A merged model list.** Rejected in §1, not deferred.
- **Caching the fetched list.** Managed mode fetches on each start, which costs
  one request and keeps the answer current. A cache would be a third place the
  mode is recorded, and stale entries would misreport what a deployment offers.

## 13. A Session Stays In The Mode It Began In

§1 makes the modes exclusive for new prompts, but a session carries history, and
resuming one sends that history again. Exploratory testing on 2026-09-24 resumed
a session written against a deployment after `buildmax logout`, named a local
model, and replayed the governed conversation to a personal key with no notice.
The reverse — a local conversation handed to a deployment after signing in — is
the same redirection in the other direction.

A session therefore records where its prompts go, `prompt_destination` in its
metadata: `local`, or the deployment's URL. The first turn sets it and nothing
changes it. Every later turn, on every surface, passes through
`AgentApp.runTurn`, which refuses one bound elsewhere and says to continue in the
mode the session began in or to start a new session. Recording on the first turn
rather than at creation means a session that has never run binds to whichever
mode first runs it.

Only sessions of a local Project are bound. A task run's session belongs to the
deployment that owns the run, and a worker continuing it may reach that
deployment by another address; binding it to a URL would break Continue without
protecting anything.

