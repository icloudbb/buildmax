# Client Sessions And API Credentials

> **简体中文：** [阅读中文镜像](../zh-CN/proposals/client-sessions-and-api-credentials.md)
>
> **Audience:** contributors, product reviewers, operators, and security reviewers · **Status:** proposal — under discussion
>
> **Opened:** 2026-08-24

Related: [roadmap](../ROADMAP.md) P3 and P4,
[deployment authentication](../deploy/authentication.md),
[managed LLM gateway design](../design/llm-gateway.md),
[client modes design](../design/client-modes.md),
[worker run token design](../design/worker-run-token.md),
[data model](../contribute/architecture/data-model.md), and the
[enterprise identity and access proposal](../design/enterprise-identity-and-access.md).

## Contents

- [Decision Question](#decision-question)
- [Problem And Current Context](#problem-and-current-context)
- [Current Code State](#current-code-state)
- [Credential Responsibilities And Threat Model](#credential-responsibilities-and-threat-model)
- [Goals](#goals)
- [Non-Goals](#non-goals)
- [Design Principles](#design-principles)
- [Recommended Human Client Flows](#recommended-human-client-flows)
- [Machine Credentials](#machine-credentials)
- [Data Model Implications](#data-model-implications)
- [HTTP API Implications](#http-api-implications)
- [CLI, Desktop, And Portal UX](#cli-desktop-and-portal-ux)
- [Options And Trade-Offs](#options-and-trade-offs)
- [Code And Documentation Findings](#code-and-documentation-findings)
- [Staged Direction If Accepted](#staged-direction-if-accepted)
- [Product Decisions Needed](#product-decisions-needed)
- [Evidence Needed For A Decision](#evidence-needed-for-a-decision)
- [Likely Destination If Accepted](#likely-destination-if-accepted)

## Decision Question

After an interactive login, should BuildMax return a third long-lived token in
addition to the access token and refresh token so that CLI/TUI and Desktop can
keep calling the managed LLM gateway?

The likely direction is:

> No. A human login should create one revocable client session. Short-lived
> access tokens authorize API calls, and a rotating refresh token keeps that
> session usable. Long-lived machine credentials are created explicitly as
> personal access tokens or service-account credentials, never as a side effect
> of signing in. If the managed gateway needs a narrower credential, the client
> obtains a short-lived, audience-restricted token on demand rather than a
> long-lived gateway token at login.

This is not an accepted roadmap commitment. It makes the credential boundary
concrete enough to accept, change, or reject before the current Alpha login
shape hardens into an API other clients depend on.

## Problem And Current Context

BuildMax has two different needs that look similar only because both require a
Bearer header:

1. A person signs in on Portal, CLI/TUI, or Desktop and expects the session to
   survive access-token expiry without repeatedly asking an operator for a
   login code.
2. A script, integration, CI job, or shared service may need to call an API
   while no person is present.

The first is a human session. The second is machine authority. Returning a
static long-lived token from every successful login would merge them: ordinary
sign-in would silently create an automation credential, logout would have no
obvious meaning for it, and the audit trail could not tell an interactive
client from an unattended caller.

The managed LLM gateway does not create a third need. A local CLI/TUI or Desktop
Agent may run for longer than one access token, but it can renew the session
before each request. A streaming completion is authorized when the request is
accepted; it does not need an access token whose lifetime exceeds every
possible model call. A reconnect can obtain a fresh access token first.

This distinction matters more in BuildMax than in a conventional API client.
The local Agent can execute model-selected commands, and Bash sandboxing is off
by default. A credential stored as a file readable by the same OS user is
protected from another user account, but not necessarily from code executing
inside the Agent's own trust domain. Adding another long-lived bearer secret
would increase that exposure without improving session continuity.

## Current Code State

### Interactive Login

`POST /api/login` accepts a password or an operator-issued, single-use login
code. Either proof creates a new session ID and returns:

- `access_token`, plus the legacy duplicate field `token`;
- `refresh_token` when a refresh-token store is configured;
- `expires_in`; and
- the user's public identity fields.

Each login creates its own session. The refresh token is an opaque random
secret stored only as a SHA-256 hash in `user_refresh_token`. Rotation spends
the presented row and creates a replacement in the same `session_id` chain.
Reuse outside `refresh_rotation_grace` revokes the whole chain and records an
`auth.refresh_reuse` audit event.

The defaults are:

| Setting | Current default | Current meaning |
|---|---:|---|
| `access_token_ttl` | 7 days | How long an unstored access JWT remains usable |
| `refresh_token_ttl` | 30 days | How long the current refresh-token row may be exchanged |
| `refresh_rotation_grace` | 30 seconds | How long a spent token may be exchanged again by a racing client process |

Rotation assigns each replacement `now + refresh_token_ttl`. The 30-day value
is therefore an inactivity window, not an absolute session lifetime. A client
that refreshes regularly can keep the session alive indefinitely.

Logout revokes the refresh-token chain. It does not invalidate an access token
already issued from that chain. Disabling the account is immediate because
every authenticated route resolves the user row and refuses a non-null
`disabled_at`.

### Access Token Shape

The user JWT currently contains:

| Claim | Meaning |
|---|---|
| `sub` | User public ID |
| `typ` | `access` |
| `sid` | Refresh-token session chain |
| `jti`, `iat`, `exp` | Registered token identity and lifetime claims |

It has no enforced issuer, audience, client identity, or scopes. `typ` prevents
a run token from substituting for a user access token, but a valid user access
token is otherwise general-purpose across the user API.

### Managed Clients

The existence of `<BUILDMAX_HOME>/auth.json` selects managed mode. CLI/TUI and
Desktop fetch the deployment's model list, build one remote LLM client, and
request a credential before each managed request. `TokenForServer`:

- refuses to send the credential to a Server URL other than the one stored by
  login;
- returns the current access token when it is still usable;
- refreshes it shortly before expiry; and
- ends the local login when the server rejects the refresh token.

The remote client therefore already survives access-token expiry. No fixed
token is baked into a long-lived TUI or Desktop process.

The current gateway routes are:

```text
GET  /api/llm/models
POST /api/llm/completions
```

Both call `ActiveUser`. Being signed in is their whole authorization: every
enabled catalog model is deployment-global, and foreground calls are
attributed to the person rather than to a Space.

### Client Storage

CLI/TUI and Desktop now keep the access and refresh tokens in the OS credential
store by default (`internal/interface/auth/secretstore.go`, via go-keyring),
falling back to a `0600` file when no store is available; `auth.json` retains
the Server URL and non-secret user metadata. The shared credential is also why
the server has a rotation grace window: two processes may read and exchange the
same refresh token concurrently.

Portal stores both credentials in `localStorage` and coordinates refreshes
inside one browser tab. That is a different threat model from a native client
and should not force the native storage design.

### Worker Authentication

Task-run workers already use the credential shape this proposal wants to
preserve for machine execution: the scheduler mints a short-lived run token
whose claims name one user, Space, task, and TaskRun. Every worker route checks
that the path names the run in the credential, and managed inference additionally
requires the run to be executing.

The run-token design explicitly rejects reusing a person's access token. A
worker executes model-selected commands, while a user token opens every Space
and resource that person may reach. Attribution does not require
impersonation. The same least-privilege principle applies when designing PATs
and service accounts.

## Credential Responsibilities And Threat Model

The credential classes should remain distinct:

| Credential or proof | Responsibility | Expected lifetime | Primary threat |
|---|---|---:|---|
| Password, login code, OIDC authorization, or device authorization | Prove a person may start a session | One authentication transaction | Phishing, brute force, account linking, or code interception |
| Access token | Authorize a bounded set of API calls | Minutes, not days | Direct replay until expiry; current session revocation does not stop it |
| Refresh token | Continue one client session and mint new access tokens | Days of inactivity, with an absolute cap | Theft creates renewable authority; rotation detects only eventual reuse |
| Personal access token | Let one person's unattended client perform explicitly selected actions | Explicit, expiring grant | Static replay, forgotten credentials, excessive scopes, person leaving |
| Service-account credential | Authenticate a non-human principal owned by a Space or deployment | Policy-controlled or workload-bound | Orphaned ownership, broad shared secrets, weak attribution |
| Run token | Let one dispatched TaskRun use only its worker routes | One run | Leakage from process or Job state before run end or token expiry |

A refresh token is already the long-lived half of a human session. It is safer
than a general long-lived API token for that job because it is accepted only at
the token endpoint, stored as a hash, individually revocable, rotated on use,
and linked to a session family. It is still a high-value secret and must not be
treated as harmless merely because it cannot call the gateway directly.

The OAuth 2.0 Security Best Current Practice requires a public client's refresh
tokens to be sender-constrained or rotated, and recommends restricting access
tokens to the minimum necessary audience and privileges. BuildMax implements
rotation today but not audience or scope restriction. See
[RFC 9700](https://www.rfc-editor.org/info/rfc9700/) and
[RFC 8707](https://www.rfc-editor.org/info/rfc8707/).

## Goals

- Keep a human login usable across short access-token lifetimes without adding
  a third long-lived login credential.
- Limit the effect of an access token leaking from a managed LLM request,
  client process, log, or local runtime.
- Make logout, session expiry, credential rotation, and account disablement
  have explicit and testable meanings.
- Keep refresh tokens out of Agent-visible configuration, prompts, tools,
  hooks, MCP, subagents, logs, and traces.
- Give scripts and services an explicit machine-credential path with scopes,
  expiry, ownership, and individual revocation.
- Preserve the run-scoped worker credential and direct local mode.
- Leave room for the OIDC direction being evaluated by the enterprise identity
  proposal without making OIDC a prerequisite for current private deployments.
- Keep Space membership and System Administrator grants as server-derived
  authorization, not claims a client may invent.

## Non-Goals

- Implementing OAuth, OIDC, device authorization, PATs, or service accounts in
  this proposal.
- Making public internet exposure supported before login throttling, SSO or a
  second factor, and the other limits in the support matrix are resolved.
- Turning the managed gateway into a public OpenAI-compatible API.
- Adding per-Space model policy or Space attribution to foreground managed calls;
  the client-modes design explicitly withdrew both.
- Making access tokens carry complete Space membership or system-role state.
  Those authorities can change and remain server-derived.
- Treating secure local storage as protection from a fully compromised client
  process. A process that can use a secret may be able to abuse its broker; the
  goal is to prevent casual file disclosure and reduce replay outside that
  process.
- Replacing run tokens with PATs or service-account credentials.

## Design Principles

### One Human Login, One Session

Every interactive login creates exactly one independently visible and
revocable client session. CLI, Desktop, and Portal should not silently share a
refresh-token family merely because they run on the same machine. The session
records which client and device created it; `platform` stops being only an
operator-facing label.

### No Machine Credential As A Login Side Effect

A successful login response returns a session credential pair only. Creating a
PAT or service-account credential requires a separate, explicit operation that
names the credential, chooses or accepts its scopes, and chooses an expiry.

This gives logout an unambiguous meaning: it ends the selected human session.
It neither leaves behind a hidden token created at login nor unexpectedly
deletes an automation credential the user created for a different purpose.

### Short-Lived Access, Renewable Session

Long-running clients do not need long-running access tokens. They need a safe
way to obtain another short-lived token. The client refreshes before starting a
request whose expected duration approaches expiry. Once the server has accepted
an SSE completion, expiry alone does not terminate the in-flight call; a new or
reconnected request must authenticate again.

Proposed policy defaults for discussion are:

| Lifetime | Proposed default | Reasoning |
|---|---:|---|
| User access token | 15-30 minutes | Bounds an unstored bearer token while keeping refresh traffic modest |
| Gateway-only access token, if introduced | 5-15 minutes | It is requested immediately before model traffic and needs only two scopes |
| Refresh inactivity | 30 days | Keeps the current usability expectation |
| Human session absolute lifetime | 90 days | Prevents activity from renewing one grant forever |
| PAT | 30 days, with an operator-defined maximum | Makes unattended authority explicit and forces a rotation policy |

These values are product and operator-policy decisions, not commitments. A
trusted private deployment may choose a longer access-token lifetime during a
transition, but the current seven-day default should not be the production
target while a session logout cannot invalidate it.

### Audience And Scope Are Enforced At The Route

At minimum, a user access token should carry and the server should verify:

| Claim | Proposed meaning |
|---|---|
| `iss` | Canonical identity of the issuing BuildMax deployment |
| `aud` | Intended resource, initially `buildmax-api` or a canonical API URI |
| `scope` | Operations granted to this client token |
| `client_id` | `buildmax-cli`, `buildmax-desktop`, `buildmax-portal`, or another registered client |
| `sub` | User or service-account public ID |
| `sid` | Human session, omitted for machine credentials |
| `typ` | Credential class, preserving substitution checks |
| `jti`, `iat`, `nbf`, `exp` | Token identity and lifetime |

The minimum managed-gateway scopes are:

```text
llm.models.read
llm.completions.create
```

Space membership, Space role, current account state, model enabled state, quota,
and System Administrator grants remain server reads. A token scope says what
kind of action this client grant may attempt; it does not assert that the
subject owns a resource.

Two audience shapes remain viable:

1. **One BuildMax API audience.** `aud=buildmax-api`, with route-level scopes.
   This is the smallest change while Portal, local clients, and the gateway are
   one resource server.
2. **A separate managed-inference audience.** A credential broker uses the
   human session to request a short-lived `aud=buildmax-llm` token. Gateway
   routes reject the general API token and every other route rejects the LLM
   token. This reduces cross-route replay but adds a token-exchange contract.

The second is defense in depth, not a reason to issue a long-lived gateway
token. The first is a reasonable initial slice if scopes, short lifetimes, and
secure refresh-token storage land with it.

### Revocation Has Two Layers

Short access-token expiry remains the outer bound if the session store is
unavailable. When it is available, an explicit session row lets the normal
authenticated-request check refuse an access token whose `sid` has been
revoked. BuildMax already reads the user row on every authenticated request to
make account disablement immediate, so adding session state does not introduce
the first database dependency in that path.

The server should distinguish:

- revoke one session;
- revoke all of one user's sessions;
- disable the account, which refuses sessions and machine credentials;
- revoke one PAT or service-account credential; and
- rotate a deployment signing key, which is an operational event rather than a
  substitute for session revocation.

Password change and identity-provider deprovisioning need an explicit policy:
revoke all sessions, revoke only password-authenticated sessions, or leave
sessions intact. The current password change leaves sessions alive.

### Credential Storage Is Outside Agent Authority

The preferred native-client arrangement is:

```text
Agent runtime ── asks for an access token ──> credential broker
                                                  │
                                                  ├─ access token in memory
                                                  └─ refresh token in OS secret store
```

**Shipped.** CLI/TUI and Desktop now store tokens in the native OS credential
store — Keychain on macOS, Credential Manager on Windows, and Secret Service on
supported Linux desktops — via `internal/interface/auth/secretstore.go`, with a
`0600` file as the reported fallback. `auth.json` retains non-secret metadata
such as Server URL, subject, session ID, and selected storage backend.

If a platform has no usable secret store, a `0600` file may remain an explicit
fallback for Alpha, but the surface must report that weaker storage mode. There
is no useful encryption-at-rest fallback whose decryption key sits beside the
ciphertext.

Access and refresh tokens must never be copied into:

- `settings.yaml` or workspace configuration;
- process arguments or shell history;
- Agent environment variables;
- prompts, tool arguments, hook input, MCP input, or subagent context;
- normal logs, errors, traces, or analytics; or
- session transcripts.

The credential broker also coordinates refresh. Separate CLI and Desktop
sessions remove cross-application races. Multiple CLI processes can use an OS
lock, a credential-helper transaction, or a local broker so the server no
longer needs a broad grace window solely because every process reads one file.

## Recommended Human Client Flows

### Current Password And Login-Code Flow

Until enterprise identity is accepted and implemented:

1. CLI or Desktop sends the password or operator-issued login code to
   `POST /api/login` over TLS.
2. The server creates an explicit client session and returns an access token
   plus rotating refresh token.
3. The client moves the refresh token into its secret store and keeps the
   access token in memory where practical.
4. Managed model discovery requests an access token with
   `llm.models.read`.
5. Each completion requests an access token with
   `llm.completions.create`; refresh happens first when needed.
6. Logout revokes the session and clears local secret and metadata state even
   if the server cannot be reached. The UI reports when server-side revocation
   could not be confirmed.

A deployment that cannot store refresh sessions may retain an access-only login
for development compatibility, but a managed client should report that the
login cannot renew. A deployment presenting managed inference as an operator
service should require the session store rather than turn a seven-day access
token into its availability mechanism.

### Future Native OIDC Flow

The enterprise identity proposal's likely direction is native OIDC. If
accepted:

- Desktop and a terminal with a usable browser open the system browser and use
  Authorization Code with PKCE and an exact registered redirect;
- the native app never embeds an identity-provider login form or handles the
  user's IdP password;
- a remote or browserless terminal may use Device Authorization, displaying a
  short-lived user code and verification URI; and
- either grant ends by creating the same BuildMax client session and token
  lifecycle described above.

Device Authorization is a login bootstrap, not a PAT and not a long-lived
credential. It needs short code lifetimes, polling bounds, and rate limiting.
Native browser and device guidance is standardized in
[RFC 8252](https://www.rfc-editor.org/info/rfc8252/) and
[RFC 8628](https://www.rfc-editor.org/info/rfc8628/).

### Portal Session

Portal should share the server-side session model but not blindly share the
native storage mechanism. Its current `localStorage` refresh token is available
to JavaScript in the origin. When deployment topology permits, the safer target
is a same-origin Backend-for-Frontend or server session using a `Secure`,
`HttpOnly`, and appropriate `SameSite` cookie, with CSRF protection. A browser
client that continues to hold tokens directly still needs short scopes and
lifetimes plus refresh rotation. See
[RFC 10017](https://www.rfc-editor.org/info/rfc10017/).

Whether Portal moves to a cookie/BFF model is a separate implementation and
deployment decision; it should not block removing refresh secrets from native
client files.

## Machine Credentials

### Personal Access Tokens

A PAT is appropriate when a person wants a script or external integration to
act under their identity and accepts that the grant ends when their account is
disabled. It is not the recommended credential for an interactive TUI or
Desktop login.

A PAT should be:

- explicitly created and named;
- returned in plaintext once and stored only as a hash;
- limited to enumerated scopes and one audience;
- given an expiry, subject to an operator-configured maximum;
- individually listed and revoked;
- stamped with creation, last-used, expiry, and revocation metadata;
- recognizable by a distinct secret prefix for logs and secret scanners; and
- refused by password, session-management, credential-administration, and
  System Administration routes unless a separately reviewed scope permits it.

Creating, revoking, and changing the policy of a PAT belongs in the governance
audit trail. High-volume API calls belong in operational records, not one audit
event per request.

The existing webhook key is not a PAT implementation to widen. It has a name,
owner, hash, and creation time, but no scopes, audience, expiry, last-used time,
revoked state, or general route-authentication contract. It should remain an
inbound-webhook credential until a deliberate consolidation design proves that
one table can preserve both products' semantics.

### Service Accounts

A service account is appropriate when authority belongs to a Space or deployment
rather than to the employment and session lifecycle of one person. It should be
a distinct principal with:

- an opaque public ID and display name;
- a Space or deployment owner;
- explicit role and scopes;
- enabled/disabled state;
- created-by and governance audit records; and
- one or more independently rotatable credentials.

Where the execution environment can present workload identity, OIDC federation
or another asymmetric proof is preferable to a static shared secret. A static
credential is a fallback, shown once, hashed at rest, expiring, and individually
revocable.

A service account must not log in with a password, receive a human refresh
session, own a personal Space implicitly, or inherit every Space membership of
the person who created it. Calls and audit events identify the service account
as the actor and preserve `created_by` separately.

### Task-Run Workers

Workers continue using run tokens. A service-account or PAT design does not
broaden `/api/worker/*`, because those routes already have a better revocation
and scope boundary: one TaskRun plus its server state.

## Data Model Implications

The Alpha policy permits fixing stored shapes everywhere at once rather than
preserving an incorrect contract. If this direction is accepted, the likely
relational model is:

### `auth_session`

One row per human login:

| Field | Purpose |
|---|---|
| Public ID | Stable `sid` and API handle |
| User ID | Session owner |
| Client ID and platform | Enforced client class rather than an informational label |
| Device name | User-recognizable session listing |
| Created, last-used | Lifecycle and diagnostics |
| Idle expiry | Maximum inactivity before refresh is refused |
| Absolute expiry | Maximum lifetime regardless of rotation |
| Revoked time and reason | Immediate session retirement and audit context |

`user_refresh_token` rows reference this session and keep token hash,
rotation/replacement, expiry, use, and revocation evidence. The session row
answers listing and revocation without reconstructing a family from every
rotation row.

### `personal_access_token`

One row per user-created machine credential:

| Field | Purpose |
|---|---|
| Public ID, secret prefix, secret hash | Management handle and one-way credential lookup |
| User ID, name | Owner and recognizable purpose |
| Audience, scopes | Enforced least privilege |
| Created by, created at | Provenance |
| Expires, last used | Rotation and incident response |
| Revoked time and reason | Retained lifecycle evidence rather than hard deletion |

### `service_account` And Credential Rows

These should be added only if Space- or deployment-owned automation is an
accepted product need. Principal metadata and credentials should be separate so
one service account can rotate credentials without changing identity or audit
history.

### Signing-Key State

The current single `jwt_secret` signs user access tokens and run tokens. A
production rotation design needs a current signing key, verification keys kept
for already-issued tokens, a key ID in new tokens, and an operational procedure
that does not use key rotation as session revocation. Separating user and run
signing keys would further contain a key-specific failure, but adds operator
configuration and must be evaluated with the deployment model.

## HTTP API Implications

Exact routes become authoritative only in
`internal/server/handlers/routes.go`. A likely API shape is:

```text
POST   /api/login
POST   /api/token/refresh
POST   /api/logout

GET    /api/sessions
DELETE /api/sessions/{session_id}
DELETE /api/sessions

POST   /api/personal-access-tokens
GET    /api/personal-access-tokens
DELETE /api/personal-access-tokens/{token_id}
```

OIDC, Device Authorization, service-account administration, or token exchange
routes should be added only with their respective accepted design. They are not
implied by the route sketch above.

The login and refresh DTOs should expose the token type and server-calculated
expiry. Refresh must preserve or narrow the original session's audiences and
scopes; a client cannot request an upgrade. If Alpha clients are cut over
together, the legacy duplicate `token` response field can be removed instead of
being preserved indefinitely.

Every authenticated route declares its allowed credential types, audience, and
required scopes. A PAT presented to `/api/token/refresh`, a gateway-only token
presented to an Issue route, a user access token presented to a worker route, or
a run token presented to a user route all fail before resource authorization.

## CLI, Desktop, And Portal UX

### CLI/TUI

The minimum user-visible set is:

- `buildmax login` identifies the Server, authentication method, and credential
  storage backend;
- `buildmax whoami` reports account, Server, session expiry, client session,
  and whether secure or file fallback storage is in use;
- `buildmax logout` revokes and removes one session, retaining the current
  behavior that local state is cleared when the Server is unreachable;
- session list and revoke commands let a person retire another device without
  asking a System Administrator; and
- PAT creation is a separate command that requires a name, expiry, and explicit
  scopes and prints the secret exactly once.

`buildmax login` remains the switch to managed mode, and logout remains the
explicit switch back to local models. Credential expiry never causes implicit
fallback.

### Desktop

Desktop uses the same backend session and credential broker, exposes the
current device and other sessions in account settings, and labels an expired or
revoked login without silently entering local mode. Future OIDC opens the
system browser rather than embedding the identity-provider page.

### Portal

Account settings list sessions and PAT metadata, but never plaintext secrets
after creation. System Administration retains revoke-all and account-disable
recovery paths. Service-account administration belongs with its Space or system
owner, not in personal session settings.

## Options And Trade-Offs

| Option | Strength | Cost or failure | Direction |
|---|---|---|---|
| Add a third long-lived token to every login | Superficially simple for clients | Duplicates refresh responsibility, creates hidden machine authority, ambiguous logout and audit, large leak window | Reject |
| Use a static PAT for TUI/Desktop | No refresh implementation needed | Interactive clients hold a directly usable long-lived secret; weak reuse detection and session UX | Reject |
| Keep access + rotating refresh, one API audience | Smallest change; current clients already refresh | A leaked access token can cross API areas allowed by its scopes; secure storage and session state still needed | Viable first slice |
| Add on-demand gateway token exchange | Strong audience separation and short LLM credential | More protocol, caching, failure, and discovery behavior | Preferred hardening after the base session model |
| Make every access token stateful | Immediate revocation | Database/cache check on every request and availability coupling | Partly favored: check explicit session state where a session store exists |
| Sender-constrain native tokens with DPoP | Stolen token alone is less useful | Key lifecycle and cross-platform implementation complexity; same-process compromise can use the key | Later hardening if deployment evidence justifies it |
| Add PATs only | Solves personal scripting with a small principal model | Encourages human-owned automation; does not solve Space-owned services | Useful when a real scripting use case exists |
| Add service accounts first | Correct owner for shared automation | Larger authorization, provisioning, and UI surface | Wait for a Space-owned automation requirement |
| Browser PKCE for every native client | Standard SSO-capable flow | Awkward on remote/headless terminals | Use where a browser is available |
| Device Authorization for every TUI | Works remotely | Polling, phishing/code UX, and more endpoints when a browser would be simpler | Fallback for browserless terminals |

## Code And Documentation Findings

The proposal relies on current code where older documents disagreed. None of
these corrections waited on the product decision, so all of them have been
made:

- `docs/contribute/architecture/desktop.md` described the removed Desktop
  mode-state mechanism and the deleted `UseLocalMode` and `ConnectToServer`
  bindings. Corrected: `auth.json` presence is the mode.
- `docs/contribute/architecture/tui.md` said the CLI has no app-level mode and
  that transport belongs to each model entry, and printed a `direct` footer tag
  the TUI does not render. Corrected: the tag is `local` or the deployment host,
  and it is a property of the app.
- `internal/server/static/openapi.json` documented `/api/spaces/{space_id}/llm/*`
  and a bare `/api/conversations`, none of which are registered. Corrected to
  the deployment-global `/api/llm/*` and the space-scoped conversation route;
  every documented path now matches `routes.go`.
- `docs/contribute/architecture/server.md` said the old shared worker token
  remained as an upgrade fallback. Corrected: it has been removed, and the run
  token is the only credential a worker route accepts.
- `docs/contribute/architecture/data-model.md` said no server path writes the
  user's last-login metadata. Corrected: the login handler calls
  `UpdateLoginMeta`.
- `docs/design/llm-gateway.md` listed refresh versus a scoped client token as an
  open question, and called the access token a 24-hour JWT. Corrected: refresh
  is implemented and the default is seven days; the unresolved parts are secure
  storage, absolute lifetime, audience/scope, and machine identity.
- The P3 roadmap sentence could be read as saying CLI, TUI, Desktop, and task
  runs all use a per-run credential. Corrected: only task runs use run tokens,
  and interactive clients use the human session.

## Staged Direction If Accepted

### Stage 1: Harden The Existing Two-Token Session

- Add explicit session state and absolute expiry.
- Shorten the access-token default.
- Add and enforce issuer, audience, client, and scope claims.
- Separate CLI and Desktop sessions.
- Move native refresh secrets behind a credential-store interface.
- Add self-service session listing and revocation.
- Define signing-key rotation.
- Correct current documentation and OpenAPI drift.

This stage changes no managed-mode product semantics: login still selects the
deployment's models, gateway calls remain user-attributed, and direct mode still
requires no Server.

### Stage 2: Enterprise Interactive Login

If the enterprise identity proposal is accepted, add external-browser OIDC with
PKCE and Device Authorization for browserless terminals. Both create the same
BuildMax session from Stage 1.

### Stage 3: Explicit Machine Identity

Add PATs when a supported personal scripting/API use case is named. Add service
accounts only when Space- or deployment-owned unattended work has a concrete
owner and authorization requirement. Do not widen worker authentication.

### Stage 4: Audience-Specific Or Sender-Constrained Tokens

Add gateway token exchange or sender-constrained tokens if incident evidence,
deployment topology, or an external API product justifies their complexity.

## Product Decisions Needed

1. Is the managed LLM gateway permanently a first-party BuildMax-client API, or
   will third-party applications become a supported product surface?
2. Should every signed-in account continue to receive managed inference, or is
   a deployment entitlement needed even while model selection stays global?
3. What access, refresh-idle, session-absolute, and PAT lifetimes are the
   supported defaults and operator-configurable limits?
4. Must logout invalidate the current access token immediately through a
   session-state check, or is a short expiry sufficient for some deployments?
5. Is an OS secret store a requirement for supported managed mode, or is a
   visible `0600` file fallback supported on headless systems?
6. Should Portal move to a cookie/BFF session, or remain a token-holding browser
   client?
7. Which first PAT scopes correspond to an actual supported automation use
   case? Is managed inference one of them?
8. Is unattended authority owned by a person, a Space, or the deployment, and
   therefore is a PAT sufficient or is a service account required?
9. Should signing keys be separated by user and run token type, and where does
   a private deployment keep the verification key ring during rotation?
10. Does OIDC/device authorization move ahead of its current post-Beta roadmap
    position because native managed clients need enterprise login, or remain a
    later identity milestone?

## Evidence Needed For A Decision

- A threat-model walkthrough for token theft from `auth.json`, local
  model-selected commands, hooks, MCP servers, browser JavaScript, logs, and
  worker environments.
- A cross-platform spike proving a single-binary CLI can use Keychain,
  Credential Manager, and a practical Linux/headless fallback without adding a
  Node requirement.
- Concurrency tests for multiple CLI processes refreshing one session, including
  a lost refresh response and replay outside the grace window.
- Route-matrix tests covering credential type, audience, scope, account disable,
  session revoke, Space authorization, and System Administrator separation.
- A deployment exercise that rotates signing keys without interrupting refresh
  sessions or in-flight runs.
- Product evidence for the first non-interactive caller before choosing PAT,
  service account, or both.
- An end-to-end native OIDC and device-flow trial if the enterprise identity
  proposal is accepted.

## Likely Destination If Accepted

An accepted decision would:

- add a durable client-session and credential specification under
  `docs/design/`;
- update the P3/P4 roadmap with the selected stages and evidence gates;
- update deployment authentication, configuration, support, CLI, Desktop,
  Portal, data-model, and OpenAPI documentation alongside implementation;
- create focused implementation Issues for session state, native secret
  storage, access-token claims and route scopes, session UX, and signing-key
  rotation; and
- leave PATs, service accounts, gateway token exchange, and OIDC/device
  authorization as separate implementation Issues only when their corresponding
  product decisions are accepted.
