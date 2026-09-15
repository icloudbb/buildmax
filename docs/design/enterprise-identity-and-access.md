# Enterprise Identity And Access

> **简体中文：** [阅读中文镜像](../zh-CN/design/企业身份与访问.md)
>
> **Audience:** contributors, operators, product reviewers, and security reviewers
>
> **Lifecycle:** Accepted 2026-09-13. Built so far: Phase 1 (durable sessions and the Portal cookie credential) and Phase 2 (OIDC login and association, with Okta the named provider). Phase 3 qualification is open.
>
> **Primary domain:** Trust and Security

This record supersedes the retired *Enterprise Identity And Access* proposal. It
keeps that paper's direction and evidence — the original repository behavior
inspected at `cceba61c` against OpenID Connect Core/Discovery, OAuth 2.0 Security
BCP, and the browser/native guidance linked below — and records three decisions
taken on acceptance. The shipped status and current-code table were rechecked
against the working tree on 2026-09-14.

1. **Native and SSO are an emergent, validated posture, not a mode enum.** The
   switch is the two orthogonal knobs `oidc.enabled` and `local_login`, not a new
   `auth_mode` field. See §4 and §11.
2. **SSO provisions just-in-time by default.** A verified first login creates the
   BuildMax account automatically, bounded by a required non-empty allowed-email
   domain list. Native deployments keep operator-created accounts. See §4 and §5.
3. **One external protocol now, SAML as a clean later extension.** Only OIDC is
   implemented; no SAML field, column, or config exists today. The extension path
   is recorded in §8a so adding SAML is addition, not rework.

The open items in §19 are the per-deployment inputs for the remaining provider
qualification and native-client slices, not questions about whether to build.
Git history keeps the full proposal.

Related: [roadmap](../ROADMAP.md) R5,
[current state](../current-state.md),
[deployment authentication](../deploy/authentication.md),
[Space membership lifecycle](space-membership-lifecycle.md),
[system administration](system-administration.md),
[client sessions and API credentials](../proposals/client-sessions-and-api-credentials.md), and
[enterprise requirements inventory](../proposals/enterprise-capability-requirements.md).

## Contents

- [1. Acceptance Context And Evidence](#1-acceptance-context-and-evidence)
- [2. Essential User Outcome](#2-essential-user-outcome)
- [3. Verified Current Constraints](#3-verified-current-constraints)
- [4. Minimum Design](#4-minimum-design)
- [5. Identity And Account Association](#5-identity-and-account-association)
- [6. Space Membership, First Login, And Invitations](#6-space-membership-first-login-and-invitations)
- [7. BuildMax Session Lifecycle](#7-buildmax-session-lifecycle)
- [8. OIDC Protocol And Browser Flow](#8-oidc-protocol-and-browser-flow)
- [8a. Extensibility And Future Protocols](#8a-extensibility-and-future-protocols)
- [9. Data Model And Ownership](#9-data-model-and-ownership)
- [10. HTTP API And Portal Changes](#10-http-api-and-portal-changes)
- [11. Configuration And Deployment](#11-configuration-and-deployment)
- [12. Failure, Rotation, Recovery, And Audit](#12-failure-rotation-recovery-and-audit)
- [13. Authorization And Agent-Execution Effects](#13-authorization-and-agent-execution-effects)
- [14. CLI And Desktop Scope](#14-cli-and-desktop-scope)
- [15. Threat Model](#15-threat-model)
- [16. Alternatives Considered](#16-alternatives-considered)
- [17. Delivery And Verification](#17-delivery-and-verification)
- [18. Non-Goals](#18-non-goals)
- [19. Open Per-Deployment Inputs](#19-open-per-deployment-inputs)
- [20. Documentation And Delivery Status](#20-documentation-and-delivery-status)

## 1. Acceptance Context And Evidence

This record defines the smallest coherent corporate sign-in path for a private
BuildMax deployment. The direction is accepted and it does not move SSO ahead of
the private-deployment Beta gate. Phases 1 and 2 are implemented; the Phase 3
qualification (§17, §20) is still open.

The design rests on three kinds of evidence:

1. The originating evidence inspected repository behavior at `cceba61c`,
   including account and refresh-token stores, authentication services and
   handlers, the central access guard, Space membership and invitations, audit,
   configuration, Portal login/session code, and their tests. The implemented
   status and §3 were rechecked against the current tree on 2026-09-14.
2. Current documentation and code show the shipped OIDC browser flow, the lack
   of an application-owned login rate limiter, Space membership as the resource
   boundary, System Administrator as a separate deployment authority, and local
   execution that does not require a Server.
3. The protocol baseline is OpenID Connect Core and Discovery, OAuth 2.0
   Security Best Current Practice, and the browser/native guidance linked in
   this record.

The direction is settled and Okta is the named target provider, but the real
provider test tenant, offboarding service-level objective, and native-client
requirement are still per-deployment inputs, not details an implementation
should guess. They gate the remaining qualification and native-client slices
and are listed in §19; they do not reopen whether to build.

## 2. Essential User Outcome

An employee should authenticate through the identity system their organization
already operates, reach only the BuildMax Spaces their stored memberships
allow, and lose the ability to start or access work within a stated bound when
their access is removed. An operator must be able to distinguish and recover
failures in the identity provider, BuildMax account association, and BuildMax
authorization without acquiring access to Space content.

The acceptance question is therefore not “does the login page have an SSO
button?” It is:

> Can an operator explain which corporate identity became which BuildMax
> account, which BuildMax session it opened, why that account can reach a
> Space, how and when each authority ends, and what still works during an IdP
> outage?

## 3. Verified Current Constraints

The following is current code, not inferred future behavior:

| Concern | Current fact | Design consequence |
|---|---|---|
| Account identity | `external_identity` binds unique `(issuer, subject)` to a user; an administrator can inspect or unlink it after disabling the account | The stable identity is the provider pair; verified email is used only for first association or JIT provisioning |
| Account creation | Native `CreateUser` and OIDC JIT provisioning atomically create the account, personal Space, owner membership, and—when applicable—the identity link | Both creation paths preserve the same account invariant |
| Login | `/api/auth/login` accepts a password or operator-issued single-use code; `/api/auth/oidc/*` implements the browser OIDC flow | Every proof opens the same BuildMax session and authorization plane |
| Access token | HMAC JWT carries `sub`, `typ`, `sid`, `jti`, `iat`, and `exp`; the configured default lifetime remains seven days, and the guard checks the `sid` session on every request | Durable session state already bounds logout and revocation; a shorter bearer default remains an open hardening choice |
| Refresh token | Opaque, hashed, rotating rows belong to an `auth_session`; inactivity expiry is subordinate to the session's absolute expiry | Retain rotation and the absolute session ceiling |
| Revocation | Logout and administrator revocation retire the `auth_session` and its refresh rows; the guard rejects an access JWT tied to that session on the next request | The durable session is the revocation authority |
| Account disablement | Every authenticated route re-reads `user.disabled_at` | Offboarding through BuildMax disablement is immediate at the user API boundary |
| Portal storage | The refresh token is delivered in a `Secure`, `HttpOnly`, `SameSite=Strict` cookie; the access token is held in memory | The renewable credential is not exposed to Portal JavaScript |
| Authorization | The central guard derives Space roles and System Administrator grants from database state | Ignore IdP role/group claims in the first slice |
| Invitation | A Space invitation targets an account that already exists | Existing-only onboarding can invite before first SSO login; JIT users become invitable only after provisioning |
| Audit | Identity link, JIT creation, and unlink events are written transactionally with their identity changes; ordinary login and many other events remain best-effort | The identity lifecycle has an atomic record, but the broader audit system still does not justify a compliance claim |
| Deployment | Portal and API normally share one public origin; the signed Server-owned OIDC transaction cookie and shared database work without process-local callback affinity | Multi-replica behavior exists in code; deployed real-provider qualification remains open |
| Local surfaces | CLI/TUI and Desktop run locally without any Server; signed-in mode is optional | Corporate SSO must not make local execution depend on the Server |

An IdP disabling a person is not currently visible to BuildMax. OIDC alone does
not provide a directory synchronization or prompt deprovisioning channel. Any
design that says “SSO solves leavers” without a bounded reauthentication policy
or provisioning integration is making a claim the protocol does not support.

## 4. Minimum Design

The accepted direction is:

1. **One native OIDC provider per deployment.** BuildMax Server is a
   confidential relying party using Authorization Code Flow with PKCE. SAML,
   trusted proxy headers, and multiple simultaneous issuers are not in the
   first implementation; §8a records how SAML is added later without rework.
2. **OIDC proves authentication only.** The verified `(issuer, subject)` pair
   identifies an external person. BuildMax still owns accounts, sessions,
   System Administrator grants, Space memberships, roles, and every resource
   authorization decision.
3. **Link through verified identity, never a role claim.** A new
   `external_identity` record binds the OIDC subject to one existing BuildMax
   user. Verified email is used only for the first association or optional JIT
   creation; later logins resolve by `(issuer, subject)`.
4. **SSO provisions just-in-time by default; native keeps operator-created
   accounts.** In an SSO deployment a verified first login for an unknown but
   allowed-domain email creates the normal account and personal Space
   automatically (`provisioning: jit`). JIT grants no shared-Space membership or
   system role, and an empty `allowed_email_domains` list refuses JIT rather than
   meaning "every domain." An operator may instead set `existing_only` to accept
   only accounts it pre-created. A native deployment (`oidc.enabled: false`) has
   no external login at all: System Administrators create and assign accounts
   exactly as today.
5. **Issue BuildMax credentials, not IdP credentials.** The callback validates
   and discards the provider tokens, then opens the same BuildMax session model
   every other login uses. Provider tokens never reach Portal, CLI, Desktop,
   Agent input, logs, or traces.
6. **Make the latent session a real authority record.** `sid` names an
   `auth_session` row checked with the user on authenticated requests. Logout,
   administrator revocation, absolute expiry, and later provider logout can
   therefore stop an issued BuildMax access token immediately.
7. **Use browser-appropriate credential delivery.** Portal keeps a short-lived
   BuildMax access token in memory and a rotating refresh token in a Secure,
   HttpOnly cookie. The renewable credential leaves `localStorage` for both
   OIDC and local fallback login.
8. **Bound IdP-only offboarding honestly.** OIDC sessions have a fixed maximum
   age and must return through the provider after it. A recommended starting
   value is 12 hours, with a 15-minute BuildMax access-token default. Prompt
   joiner/leaver automation requires a later SCIM or provider-specific
   lifecycle integration.
9. **Preserve an audited break-glass path.** In an SSO deployment, local
   password/login-code authentication defaults to System Administrators only.
   The database-direct `buildmax-server` bootstrap commands remain the recovery
   root.
10. **Switch native versus SSO with two orthogonal knobs, not a mode enum.**
    `oidc.enabled` turns external login on or off; `local_login`
    (`all | system_admins | off`) governs who may still use the local username
    and password plane. The deployment's posture is the validated combination of
    the two — native is `oidc.enabled: false` with `local_login: all`; SSO is
    `oidc.enabled: true` with `local_login: system_admins` — reported on the admin
    status surface. No separate `auth_mode` field is introduced: because
    break-glass requires local login to keep working for administrators even
    under SSO, a single native/sso enum could not stand alone and would duplicate
    `local_login`. Keeping the two axes orthogonal is also what lets a future
    protocol (§8a) be a third, independent axis rather than a new enum value.

The design deliberately introduces only two durable concepts:
`external_identity`, because issuer identity cannot safely live on `user.email`,
and `auth_session`, because the existing `sid` already names a session whose
revocation and expiry are otherwise not authoritative. Everything else reuses
the current account, Space, token, and audit models.

## 5. Identity And Account Association

### 5.1 Stable key

The external identity key is the exact OIDC `iss` plus `sub` pair. Both are
case-sensitive protocol values. Email, name, preferred username, groups, and
tenant labels are attributes, never identity keys.

The callback validates the ID Token before consulting an account. At minimum it
validates signature, exact issuer, audience, authorized party when applicable,
expiry, issued-at bounds, nonce, and the authorization transaction. A first
association additionally requires a syntactically valid `email` and
`email_verified=true`. If the provider returns email through UserInfo, the
UserInfo `sub` must equal the ID Token `sub`.

### 5.2 Association algorithm

After validation, one transaction applies these rules in order:

1. If `(issuer, subject)` already has a link, use that user. A changed email
   cannot move the identity to another account.
2. Refuse a link whose user is disabled. Enabling the user is an explicit
   operator decision.
3. If no link exists and a live user has the verified email, link it only when
   that user has no identity for this issuer. This is the migration path for
   operator-created accounts.
4. If that email's account is already linked to another subject from this
   issuer, refuse and require operator investigation. Never replace either
   link automatically.
5. If no account exists, `existing_only` refuses with one generic
   “not authorized for this deployment” response. `jit` creates the user,
   personal Space, owner membership, and identity link atomically.
6. Only after the account/link transaction commits may BuildMax create a
   session.

The issuer is operator-trusted, but email matching remains a security-sensitive
one-time link. `jit` is the default because the accepted outcome is that a
verified corporate login provisions its own account; its safety comes from the
required non-empty `allowed_email_domains` list, not from an operator having
pre-named every account. An empty list refuses JIT rather than meaning "every
domain," and domain comparison is canonical and exact, not a string suffix
check. `existing_only` remains available for deployments that prefer the
operator to name every account the provider may claim; it refuses an unknown
email with one generic "not authorized for this deployment" response. Either
way, rules 1–4 above still apply first, so JIT never replaces or moves an
existing subject link.

### 5.3 Attribute drift and recovery

`external_identity.last_seen_email` and optional display-name snapshot update
after a successful login. The first slice does not automatically rewrite
`user.email`: it is already the local account handle used by administration and
invitations, and changing it safely needs collision and notification semantics.
Portal administration should show a mismatch so an operator can reconcile it.

A System Administrator may list a user's identity links. Unlinking is permitted
only while the user is disabled, is recorded atomically with the deletion, and
does not delete the user, memberships, work, or audit history. The operator then
corrects the external directory or local account state and re-enables the user
before a new first association. There is no self-service linking or unlinking
in the first slice.

## 6. Space Membership, First Login, And Invitations

OIDC does not assign Space authority.

| Journey | Result |
|---|---|
| Existing-only onboarding | System Administrator creates the account; a Space owner may invite it before first login; the first verified OIDC login links by email; the user accepts the existing invitation |
| JIT onboarding | First verified OIDC login creates the account and personal Space; a Space owner can invite it afterwards |
| Existing member enables SSO | First verified OIDC login links the current account; every existing membership remains unchanged |
| IdP email changes | Login continues through `(issuer, subject)`; memberships do not move; administration shows attribute drift |
| IdP group changes | No effect in the first slice |
| Membership removal | The next Space authorization read refuses that Space; the user's other Spaces and session remain valid |
| Account disablement | Every user API request is refused; memberships remain for provenance and return if the account is deliberately enabled |

This preserves the membership design's separation of account existence and
Space invitation. It also avoids a group-mapping reconciler, protected-owner
rules, and a second source of truth for roles before any target organization has
shown it needs them.

## 7. BuildMax Session Lifecycle

### 7.1 One session model

Every password, login-code, and OIDC authentication creates one
`auth_session`. The record owns:

- user and public `sid`;
- client platform;
- authentication method (`password`, `login_code`, or `oidc`);
- external identity when the method is OIDC;
- creation, last activity, absolute expiry, and revocation time.

Refresh-token rows belong to this session instead of being the session. Access
JWT verification requires `typ=access` and a non-empty `sid`, then the central
guard checks both the active user and active session. Space roles and grants
remain later database reads and do not enter the JWT.

### 7.2 Lifetimes and reauthentication

Recommended defaults for acceptance testing are:

| Boundary | Default | Meaning |
|---|---:|---|
| BuildMax access token | 15 minutes | Maximum bearer replay if the session-state check is unavailable or deliberately bypassed by a future route |
| Refresh inactivity | 30 days | Current rotating-token usability window |
| Local human session absolute age | 90 days | An active local login cannot renew forever |
| OIDC session absolute age | 12 hours | Maximum interval before BuildMax requires another provider authorization |

An operator may tune these, but the admin status surface reports the effective
values and warns about a multi-day access token or an unbounded session. A new
OIDC authorization uses `max_age` when configured and validates `auth_time`, so
restarting a BuildMax session cannot silently accept arbitrarily old provider
authentication.

OIDC-only deprovisioning is bounded, not immediate: without SCIM or a validated
provider logout event, a person disabled only at the IdP may retain the current
BuildMax session until its OIDC absolute expiry, plus at most the remaining
access-token window. The operator runbook must state that number.

### 7.3 Portal credential handling

Portal receives the current rotating BuildMax refresh token only as a cookie:

- `Secure` and `HttpOnly` in supported deployments;
- `SameSite=Strict` for the renewable session cookie;
- restricted to the Portal session endpoints;
- never returned in JSON;
- replaced on every refresh and cleared on logout.

Portal obtains a short-lived access token after login and page reload, holds it
in memory, and continues to send it as the current Bearer credential. That keeps
the existing API and WebSocket authorization shape while removing renewable
authority from JavaScript-readable storage. Local password/login-code Portal
login uses the same delivery path, so there is not a weaker “fallback” session.

Session-establishing cookie endpoints require exact same-origin `Origin`
validation, JSON POST where applicable, and no permissive CORS. This is in
addition to SameSite cookies, not a substitute for them.

### 7.4 Logout and revocation

BuildMax logout always revokes `auth_session`, refresh tokens, and the Portal
cookie before reporting success. The central guard then refuses an already
issued access token for that `sid`.

The first slice means “sign out of BuildMax,” not “sign out of every application
at the provider.” If Discovery advertises `end_session_endpoint`, RP-Initiated
Logout may be added after interoperability evidence, with local revocation
happening first even when the provider redirect fails. Front-channel and
back-channel logout are separate optional capabilities, not implied by OIDC
login.

## 8. OIDC Protocol And Browser Flow

BuildMax uses only Authorization Code Flow with a confidential Server client:

1. Portal navigates to `GET /api/auth/oidc/start` with a relative return path.
2. Server generates transaction-specific `state`, `nonce`, and PKCE verifier,
   sends `S256`, and binds the values plus the return path to the browser in a
   short-lived, Secure, HttpOnly, SameSite=Lax, integrity-protected cookie.
3. The browser visits the discovered authorization endpoint with scopes
   `openid email profile` and one exact registered redirect URI derived from
   `public_base_url`.
4. The callback verifies the cookie and `state`, exchanges the code from the
   Server, validates the ID Token and `nonce`, and optionally calls UserInfo as
   described in §5.1.
5. The association transaction and session creation run. Provider access and
   ID tokens are then discarded; BuildMax does not request `offline_access`.
6. Server sets the BuildMax refresh cookie and redirects to a fixed Portal
   completion route. No provider token, BuildMax token, email, or error detail
   appears in the URL.
7. Portal exchanges the cookie for an in-memory access token and current user,
   then applies the validated relative return path.

The temporary cookie is self-contained and integrity-protected with a key
derived and domain-separated from the required deployment JWT secret. It may
contain the PKCE verifier because TLS, Secure, and HttpOnly protect it in
transit and from page script; its integrity prevents a browser from swapping
transaction values. This avoids a process-local or Redis-only transaction
store, so any Server replica may receive the callback. The cookie expires in at
most ten minutes and is cleared on every callback outcome. Replaying it cannot
replay an authorization code the provider has already consumed.

The implementation must use a maintained OIDC library for discovery, token
exchange, JOSE validation, and claim rules. Hand-written JWT or OAuth parsing is
not an acceptable small implementation.

Normative references:

- [OpenID Connect Core 1.0](https://openid.net/specs/openid-connect-core-1_0.html)
- [OpenID Connect Discovery 1.0](https://openid.net/specs/openid-connect-discovery-1_0.html)
- [OAuth 2.0 Security Best Current Practice (RFC 9700)](https://www.rfc-editor.org/info/rfc9700/)
- [OAuth 2.0 for Browser-Based Applications (RFC 10017)](https://www.rfc-editor.org/rfc/rfc10017.html)
- [RP-Initiated Logout 1.0](https://openid.net/specs/openid-connect-rpinitiated-1_0.html)

## 8a. Extensibility And Future Protocols

SAML and any other enterprise login protocol are out of the first slice, and no
SAML field, column, configuration block, or `auth_method` value exists today.
This section records the intended extension path so that adding one later is
deliberate addition rather than rework — it does **not** authorize building any
of it now, and nothing below should be implemented ahead of a named deployment
that requires it.

The design is protocol-extensible because authentication and authorization are
separate axes (§4, §13). Only the handler layer is protocol-specific; the
external-identity key, the `auth_session` model, credential delivery, and the
central guard are not. A later protocol therefore slots onto three existing
axes without a fourth concept:

1. **A new value on the authentication axis.** `auth_session.auth_method` gains
   `saml`. `local_login` and the OIDC settings are untouched, because "who may
   use local login" is independent of which external protocol is configured.
   This is exactly why §4 keeps native/SSO as orthogonal knobs rather than an
   `auth_mode` enum: a protocol is a value on one axis, not a new top-level mode.
2. **A protocol discriminator on `external_identity`.** The stable key
   generalizes from OIDC's `(issuer, subject)` to `(protocol, issuer, subject)`,
   where SAML supplies `(saml, IdP EntityID, NameID)`. The discriminator is
   **not** added now — with one protocol it would be a field with no concrete
   requirement — but the uniqueness constraints in §9.1 are documented as
   "within the configured issuer" so the later migration adds a column rather
   than reinterpreting existing rows. A persistent, non-transient `NameID` is
   required; a transient NameID cannot be a stable account key.
3. **A sibling provider configuration block.** A `saml:` block sits beside
   `oidc:` with its own metadata URL or IdP certificate, entity IDs, NameID
   format, and assertion-signature requirements, validated the same way the OIDC
   block is. Assertion validation uses a maintained SAML library, never
   hand-written XML or signature parsing, matching the OIDC library rule above.

Two questions are explicitly deferred to that later review, not answered here:
running more than one external protocol or issuer simultaneously (provider-choice
UX, link-conflict, and issuer-migration behavior — see §16), and whether a target
IdP that cannot speak OIDC justifies SAML at all. The single-external-issuer
assumption in this record holds until then.

## 9. Data Model And Ownership

### 9.1 `external_identity`

| Field | Requirement |
|---|---|
| `id`, `public_id` | Internal key plus normal BuildMax public identity |
| `user_id` | Existing BuildMax account; deletion is not cascaded because users are not deleted |
| `issuer` | Exact verified issuer, case-sensitive |
| `subject` | Exact verified subject, case-sensitive |
| `last_seen_email` | Verified attribute snapshot for operator diagnosis, not lookup after linking |
| `last_seen_name` | Optional display snapshot |
| `last_login_at`, `created_at` | Lifecycle evidence |

Unique constraints are `(issuer, subject)` and `(issuer, user_id)`, scoped
within the single configured issuer. The second keeps one account from silently
accumulating two subjects from that issuer. A second protocol or issuer would
add a discriminator column ahead of these keys rather than reinterpret existing
rows (§8a). The row has no provider access token, ID token, group list, role, or
Space ID.

### 9.2 `auth_session`

| Field | Requirement |
|---|---|
| `id`, `public_id` | Internal key plus public `sid` |
| `user_id` | Account whose authority the session exercises |
| `external_identity_id` | Nullable; set only for OIDC authentication |
| `platform`, `auth_method` | Client and proof used |
| `created_at`, `last_seen_at` | Session lifecycle metadata |
| `absolute_expires_at`, `revoked_at` | Authoritative validity |

`user_refresh_token` references `auth_session` and retains only token rotation
state and expiry. The session row is not a third credential. It is the
authoritative state the already-issued `sid` currently lacks.

### 9.3 ownership

- `internal/core/identity` owns the types, validation-independent store
  contracts, session state, and permanent audit action names.
- `internal/service/identity` owns association, provisioning, authentication,
  session creation, and revocation workflows.
- `internal/infra/db` owns the two singular tables and the transaction that
  creates a JIT user with its personal Space and identity link.
- `internal/server/handlers/auth` owns OIDC HTTP state, cookies, redirects,
  provider protocol adaptation, and BuildMax token issuance.
- `internal/server/access` remains the single request authentication and
  authorization funnel.
- Portal only presents methods and stores in-memory account state; it never
  parses an ID Token or maps claims.

No new “organization,” “tenant,” “IdP group,” or SSO-specific user type is
introduced. A private deployment, an external issuer, a BuildMax account, and
one or more Spaces already express the required ownership boundaries.

## 10. HTTP API And Portal Changes

Shipped public routes:

```text
GET  /api/auth/methods
GET  /api/auth/oidc/start
GET  /api/auth/oidc/callback

POST /api/auth/portal/login
POST /api/auth/portal/session
POST /api/auth/portal/logout

GET    /api/admin/users/{user_id}/identities
DELETE /api/admin/users/{user_id}/identities/{identity_id}
```

`GET /api/auth/methods` returns only whether local login and OIDC are available
and the operator-set OIDC display label. It returns no issuer metadata, client
identifier, domain allowlist, or policy detail an anonymous caller does not
need.

The three Portal endpoints are credential-delivery adapters over the same
identity service used by current `/api/auth/login`, `/api/auth/token/refresh`,
and `/api/auth/logout`. The existing routes remain the native-client JSON transport; the
Portal routes never serialize a refresh token. Credential verification and
session transitions are not duplicated.

Portal's login page reads the method document and shows only configured choices.
An SSO button navigates rather than opening an embedded login frame. After a
callback or reload, the auth context calls the Portal session endpoint instead
of hydrating bearer credentials from `localStorage`. The UI distinguishes:

- provider unavailable or misconfigured;
- the browser transaction expired and must restart;
- the verified identity is not authorized for this deployment; and
- the linked BuildMax account is disabled.

Detailed provider errors and claims stay in redacted Server logs with a request
correlation ID, not in the browser response.

## 11. Configuration And Deployment

Keep the current top-level authentication settings for this slice rather than
restructuring all of `server.yaml` incidentally. The native-versus-SSO posture
is the validated combination of the existing plane (`local_login`) and the new
`oidc` block (§4, decision 10); there is no separate `auth_mode` field. Add one
enum and one OIDC block:

```yaml
jwt_secret: ""                 # existing; normally injected
access_token_ttl: 15m          # existing field, proposed safer default
refresh_token_ttl: 720h        # existing inactivity window
session_absolute_ttl: 2160h    # new; local/login-code/password sessions
local_login: all               # all | system_admins | off; migration default

oidc:
  enabled: false               # false => native: operator-created accounts only
  display_name: Company SSO
  issuer: https://id.example.com
  client_id: buildmax
  client_secret: ""            # inject with BUILDMAX_OIDC_CLIENT_SECRET
  provisioning: jit            # jit (default) | existing_only
  allowed_email_domains: []    # required and non-empty for jit
  session_max_age: 12h
```

The two documented postures are:

- **Native (default):** `oidc.enabled: false` with `local_login: all`. System
  Administrators create and assign accounts through the `buildmax-server`
  bootstrap commands exactly as today; there is no external login.
- **SSO:** `oidc.enabled: true` with the recommended `local_login: system_admins`
  for break-glass. `provisioning: jit` auto-creates an account on a verified
  first login from an `allowed_email_domains` address; set `existing_only` to
  accept only pre-created accounts.

`provisioning` and `allowed_email_domains` are ignored when `oidc.enabled` is
false, and startup refuses `oidc.enabled: true` with `provisioning: jit` and an
empty `allowed_email_domains`.

The callback is exactly
`<public_base_url>/api/auth/oidc/callback`. `public_base_url` is required and
must be HTTPS when OIDC is enabled, except an explicit loopback development
mode. It is never derived from `Host` or forwarded headers, which avoids an
attacker influencing redirect construction.

`local_login` means:

| Value | Password/login-code session policy |
|---|---|
| `all` | Current behavior; migration default until an operator opts into SSO enforcement |
| `system_admins` | Recommended with OIDC; a verified local credential opens a session only for an active System Administrator |
| `off` | No local HTTP login; startup warns that recovery requires changing configuration and restarting |

`allow_signup` is invalid unless `local_login: all`; JIT belongs to the verified
OIDC path and does not reopen unverified email signup.

The client secret is the only new secret in the first slice. It is injected at
deployment time, redacted in every config/status response, never passed to a
worker, and rotated by replacing the mounted secret and rolling Server replicas.
The initial client authentication method is `client_secret_basic`; add
`private_key_jwt` only when a target IdP or threat model requires it.

## 12. Failure, Rotation, Recovery, And Audit

### 12.1 Discovery and JWKS

The configured issuer is the only URL trust root. Discovery must return the
exact same issuer and HTTPS authorization, token, UserInfo when used, and JWKS
endpoints. Request parameters never supply an issuer or endpoint, which bounds
SSRF to operator configuration.

Discovery and JWKS honor cache lifetimes. An unknown signing `kid` causes one
immediate JWKS refresh before rejection, supporting ordinary provider key
overlap. Signature algorithms are restricted to those safely supported by the
library and provider metadata; `none` and using the client secret as an ID Token
HMAC key are refused.

A network fetch failure does not make `/readyz` fail: existing BuildMax sessions
and local work remain usable. New OIDC starts or callbacks answer a retryable
503 if no usable cached metadata/key exists. The admin system view reports
configured, available/degraded, last successful metadata/JWKS refresh, and a
redacted last error.

### 12.2 Rotation and configuration changes

| Change | Required behavior |
|---|---|
| Provider signing key | Automatic JWKS refresh; overlap verified in tests |
| OIDC client secret | Add new secret at IdP while old remains valid, roll BuildMax, verify login, retire old; otherwise accept a bounded login interruption |
| BuildMax JWT secret | Current access JWTs fail; active refresh/session rows can mint replacements; record and test the operator procedure |
| Issuer change | Treated as a different identity namespace; never relink by subject or email silently; existing BuildMax sessions live until normal revocation/expiry |
| OIDC disabled | Stops new OIDC login; does not silently revoke current BuildMax sessions |

Changing issuer therefore requires an explicit migration plan: preserve the old
issuer until users link through the new one, or disable/unlink/re-enable accounts
under operator control. Supporting two active issuers to smooth that migration
is a later requirement, not hidden in the one-provider slice.

### 12.3 Break glass and IdP outage

Before enforcing `local_login: system_admins`, the runbook creates at least two
System Administrator accounts with strong local passwords stored through the
operator's emergency-credential process and verifies one login. The existing
database-direct create, login-code, grant, and revoke commands remain available
when the IdP or HTTP administration surface fails.

An IdP outage has this declared result:

- existing valid BuildMax sessions continue;
- new OIDC sessions and expired OIDC sessions cannot start;
- local System Administrator recovery remains available;
- local CLI/TUI and Desktop direct mode is unaffected;
- readiness stays healthy, while admin diagnostics show OIDC degraded.

### 12.4 Audit

Reuse `user.login` with detail `oidc` for a successful session. Add permanent
actions for `user.external_identity_linked` and
`user.external_identity_unlinked`; JIT creation also records the existing
`user.created` action with OIDC detail. Identity link/unlink and their audit row
must commit in the same database transaction because they change who can become
an account. Login success and operational failures may retain the current
best-effort policy, with the known limitation stated in operator documentation.

Anonymous callback failures remain structured operational logs, not audit
events keyed by untrusted email or claims. Logs contain a correlation ID and a
bounded reason class, never authorization code, provider token, client secret,
PKCE verifier, nonce, cookie, or raw claim set. Audit export and retention keep
their existing authority and policy.

## 13. Authorization And Agent-Execution Effects

OIDC changes who can open a human session, not what that session may do.

- Every Space route continues to resolve current membership and role.
- System Administrator remains an explicit BuildMax grant; no IdP group may
  create it.
- Removing a Space membership takes effect on the next Space request and does
  not end the person's sessions or other memberships.
- Disabling a BuildMax account refuses user requests immediately, revokes its
  sessions, causes queued-but-unstarted work to fail, and pauses schedules as
  current code does.
- A TaskRun already executing under its run-scoped credential is not silently
  cancelled by SSO or membership changes. Its external side effects cannot be
  undone, and its result remains Space-owned. An operator who needs it stopped
  uses the existing cancellation path.
- Continuing, retrying, or starting later work rechecks active account and
  current Space authority rather than inheriting the old run's caller access.

SCIM or future group reconciliation must preserve these boundaries. In
particular, a directory group cannot become a token claim that bypasses
`internal/core/space.Allows`, and reconciliation must never leave an ordinary Space with
no owner.

## 14. CLI And Desktop Scope

The first accepted slice is **Portal browser SSO only**.

- CLI/TUI and Desktop local/direct mode stays fully independent of Server and
  IdP availability.
- Their existing password/login-code Server login can continue only when
  `local_login` permits the account. With the recommended
  `system_admins` setting, ordinary connected native clients cannot use managed
  Server mode in the first slice.
- No IdP page is embedded in Desktop and no IdP password is collected by a
  native client.

Add native SSO only after a named deployment requires connected CLI/Desktop.
The preferred path is Authorization Code with PKCE in the system browser and a
loopback redirect, following
[RFC 8252](https://www.rfc-editor.org/info/rfc8252/). A browserless terminal may
justify a BuildMax-mediated device flow following
[RFC 8628](https://www.rfc-editor.org/info/rfc8628/), including short-lived
codes, explicit approval, polling bounds, and rate limiting. Do not implement
both speculatively; choose from the target environment.

Neither path is a PAT or service-account design. Unattended clients remain the
separate decision in the client-credentials proposal.

## 15. Threat Model

| Threat or failure | Required control | Residual limit |
|---|---|---|
| Login CSRF or code injection | Transaction-specific state, nonce, PKCE S256, browser binding, exact callback | Compromised browser profile remains trusted as that user |
| Authorization-code interception | Server-side exchange, confidential client, PKCE; no code/token in Portal storage | IdP or TLS compromise is outside BuildMax |
| Issuer mix-up | One configured issuer, exact Discovery/ID Token issuer and audience validation | Multi-issuer support needs a new review |
| Open redirect | Only a validated relative Portal path stored inside protected transaction state | External post-login redirects are not supported |
| Account takeover by email match | Verified email, required non-empty allowed-domain list for JIT, atomic uniqueness, never replace an existing subject link | Reassigned corporate email can claim a never-linked stale local account unless the operator disabled it; with JIT a new allowed-domain mailbox provisions a fresh account but gains no shared-Space or system authority |
| Group/role escalation | Ignore IdP groups and roles; derive grants and memberships locally | Directory-driven roles await an explicit reconciler design |
| Stolen BuildMax access token | Short TTL plus active-session and active-user check | A route that bypasses the central guard would bypass revocation and must fail architecture tests |
| Stolen refresh cookie | Secure/HttpOnly/SameSite, rotation, reuse detection, exact-origin session endpoints | A fully compromised browser origin can act as the user |
| OIDC token leakage | Server-only exchange; discard provider tokens; redact logs/errors/traces | Provider observes its own login transaction |
| IdP outage | Existing sessions continue, OIDC login degrades, local admin break glass | Expired ordinary users cannot sign in until recovery |
| IdP disables user | Fixed OIDC session age; manual BuildMax disable for immediate effect | OIDC alone cannot meet a shorter offboarding SLO |
| JWKS rotation | Cache plus one unknown-key refresh, exact issuer and algorithm restrictions | Bad provider rollout can interrupt login |
| Client-secret compromise | Deployment-secret injection, redaction, overlap rotation runbook | Symmetric client authentication remains weaker than `private_key_jwt` |
| XSS in Portal | No renewable credential in JavaScript-readable storage; short access token in memory | XSS can act during the current page/session |
| Brute force on local fallback | Existing external ingress rate limit remains mandatory; local login restricted to admins | Built-in distributed auth rate limiting is still absent |
| Lost audit write | Identity link/unlink event is transactional | Ordinary login audit remains best-effort |

SSO does not make public internet exposure supported by itself. The current lack
of built-in login throttling remains a declared prerequisite or accepted
deployment limit even when local login is restricted.

## 16. Alternatives Considered

| Option | Why not the first slice |
|---|---|
| Authenticating reverse proxy headers | Trust, header stripping, issuer validation, logout, and session semantics vary by proxy; identity would become deployment-specific implicit state |
| SAML first | More protocol and metadata complexity without evidence that the first target requires it; add only for a named IdP that cannot provide OIDC |
| OIDC and SAML together | Doubles callback, metadata, key/certificate, and test matrices before one integration is proven |
| Multiple OIDC issuers | Requires provider discovery UX, link conflict and issuer-migration behavior; one private deployment currently has no demonstrated need |
| Treat verified email as identity forever | Email can change or be reassigned; it cannot safely preserve account continuity |
| Copy IdP groups into JWT roles | Makes stale external claims an authorization owner and bypasses immediate local membership changes |
| JIT plus automatic shared-Space assignment | Creates a second membership authority and protected-owner reconciliation before demand exists |
| Store provider refresh tokens | Expands secret custody and still does not replace SCIM for directory lifecycle; BuildMax needs only the authentication result |
| Keep Portal refresh token in `localStorage` | Preserves renewable bearer authority in the browser solely to minimize the patch |
| Require IdP availability for readiness | Turns a login dependency outage into an outage for already-authenticated work and local recovery |
| Implement SCIM in the SSO slice | It is a separate inbound provisioning API, credential, reconciliation lifecycle, and authorization surface; build it only for an offboarding SLO OIDC cannot meet |

## 17. Delivery And Verification

Phases 1 and 2 are implemented. The remaining Phase 3 qualification and any
native-client work become independently reviewable tasks only when the relevant
deployment inputs and acceptance criteria in §19 are settled:

### Phase 0 — evidence and decision (partly settled)

- Name the first supported IdP and record its Discovery, claim, client-auth,
  logout, and test-environment behavior.
- Confirm the offboarding bound, the JIT allowed-domain list (or `existing_only`
  if a deployment needs it), local fallback mode, and whether Portal-only scope
  is usable.
- Threat-model the exact deployment and approve the configuration contract.

### Phase 1 — session and Portal credential foundation (implemented)

- Add `auth_session`, require active `sid`, add absolute expiry, and migrate
  current session listing/revocation onto it.
- Open hardening choice: shorten the configured default access-token lifetime;
  it remains seven days while the durable session guard provides prompt
  revocation.
- Move Portal refresh delivery to the HttpOnly cookie adapter for existing
  password and login-code flows; remove auth tokens from `localStorage`.
- Force existing sessions to reauthenticate during the schema transition
  rather than inventing unverifiable authentication provenance.

### Phase 2 — OIDC login and association (implemented)

- Add config validation, Discovery/JWKS caching, browser transaction, callback,
  claim validation, `external_identity`, existing-only association, and JIT
  behind its explicit policy.
- Add Portal method discovery and SSO presentation.
- Add atomic identity-link audit and admin read/recovery surfaces.

### Phase 3 — operations and qualification (open)

- Redacted OIDC status and degraded diagnostics are implemented.
- Exercise IdP outage, JWKS rotation, client-secret rotation, issuer-change
  refusal, break glass, disablement, and rollback.
- Update current state, authentication/configuration/operator documentation,
  OpenAPI, deployment manifests, examples, and the Beta/support boundary only
  to the level the evidence proves.

### Required verification

| Scope | Evidence |
|---|---|
| Core/service | Table tests for every association branch, disabled users, collisions, expiry, revoke, and authorization non-effects |
| Handler | State/nonce/PKCE, issuer/audience/signature/time checks, cookie attributes, origin checks, safe errors, no token in redirect |
| Database | Real-MySQL tests for uniqueness, concurrent first login, JIT account/personal-Space/link atomicity, session revocation, and transactional audit |
| Portal | Component tests plus browser login, reload, refresh, logout, error, return-path, and no-token-in-storage assertions |
| Protocol | A deterministic adversarial fake for negative cases plus end-to-end login against one pinned standards-compliant provider |
| Deployment | Multi-replica kind callback, IdP/JWKS outage, key and client-secret rotation, break glass, and existing-session continuity |
| Authorization | Existing Space and system-admin matrices pass unchanged; explicit tests prove IdP groups/claims grant nothing |
| Documentation | `./make check docs` and `git diff --check`; configuration and routes remain generated-source aligned |

An in-process fake proves BuildMax's branches, not provider interoperability. A
single happy-path provider login proves neither rotation nor offboarding. Both
forms of evidence are required before claiming support for that provider.

## 18. Non-Goals

- Public multi-tenant SaaS signup or organization hierarchy.
- SAML, LDAP bind, social login, trusted proxy headers, or multiple simultaneous
  OIDC providers in the first slice.
- SCIM, IdP group-to-Space mapping, custom roles, or IdP-derived System
  Administrator grants.
- BuildMax-managed MFA or a claim that an IdP used MFA unless an accepted
  provider-specific assurance policy verifies it.
- Provider-wide logout, front-channel logout, or back-channel logout without
  separate interoperability evidence.
- Self-service identity linking, email change, account merge, or account
  deletion.
- PATs, service accounts, or unattended-client credentials.
- Native connected-client SSO without a target journey; local CLI/TUI and
  Desktop remain supported independently.
- A new identity-provider database entity, dynamic client registration, or an
  admin UI that mutates `server.yaml` and its secret.
- Changing Space role policy, ownership rules, invitation semantics, TaskRun
  authority, or worker credentials.
- Claiming compliance-grade audit while ordinary event writes remain
  best-effort.

## 19. Open Per-Deployment Inputs

The direction is accepted. The list below records which deployment facts have
been resolved and which still gate the remaining qualification or native-client
slices:

1. **Target provider.** **Chosen: Okta.** It supplies exact-issuer Discovery,
   PKCE S256, a verified `email`/`email_verified`, standard UserInfo, and a
   confidential client with `client_secret_basic`. The provider-agnostic core is
   built against OpenID Connect Core/Discovery and verified with a deterministic
   adversarial fake issuer; the pinned real-Okta end-to-end and its
   `max_age`/secret-overlap specifics are the Phase 3 qualification, which needs a
   reproducible Okta test tenant.
2. **Provisioning domains.** JIT is the accepted SSO default; which exact email
   domains are corporate authority, and does any target deployment instead need
   `existing_only`?
3. **Offboarding objective.** Is a 12-hour reauthentication bound plus manual
   immediate disablement acceptable? If not, the requested outcome requires
   SCIM, validated back-channel logout, or another named lifecycle channel.
4. **Local fallback.** Will operators maintain and drill two local System
   Administrator credentials, and is `system_admins` the right enforcement
   default for the target deployment?
5. **Native managed mode.** Must ordinary users connect CLI/Desktop to managed
   models in the first customer journey? If yes, choose browser loopback or
   device authorization from actual environment constraints before accepting
   Portal-only delivery.
6. **Requirement scope.** Which target deployment outcomes need SSO beyond the
   accepted Portal browser slice? The enterprise-requirements inventory can
   guide discovery but cannot change the security or interoperability bar here.
7. **Rate limiting.** Which shared limiter protects the remaining local login
   and OIDC transaction endpoints in the supported topology? Until answered,
   current external rate-limiting guidance remains mandatory.

Decision evidence is a written operator journey, IdP configuration export or
equivalent reproducible facts, an agreed joiner/leaver bound, a threat-model
review, and a provider test plan. Feature comparison tables or “enterprise
tools usually have SSO” are not enough.

## 20. Documentation And Delivery Status

This record is the accepted direction; the originating proposal is retired to Git
history. Phase 1's durable-session and Portal-cookie foundation (with the
configured access-token default still seven days) and Phase 2
(OIDC login and association, with **Okta** the named target provider) are
implemented: the `oidc` configuration and the orthogonal `local_login` knob; a
Discovery/JWKS provider with asymmetric-only ID-token verification; the
`external_identity` table and the section 5.2 association with JIT provisioning;
the admin identity routes; and the `/api/auth/oidc/start` and
`/api/auth/oidc/callback` browser flow with the Portal sign-in button. The
provider-agnostic core is verified against a deterministic adversarial fake
issuer.

Phase 3 remains open and needs a reproducible Okta test tenant: the pinned
real-Okta end-to-end, JWKS and client-secret rotation, issuer-change refusal,
break-glass and IdP-outage continuity, and RP-initiated logout. It does not
exist yet.

`docs/current-state.md`, `docs/deploy/authentication.md`,
`docs/reference/configuration.md`, and OpenAPI describe the shipped behavior;
each remaining Phase 3 slice updates them in the same contribution that ships it.
