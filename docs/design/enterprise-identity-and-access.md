# Enterprise Identity And Access

> **简体中文：** [阅读中文镜像](../zh-CN/design/企业身份与访问.md)
>
> **Audience:** contributors, operators, product reviewers, and security reviewers
>
> **Lifecycle:** Accepted 2026-09-13; amended 2026-10-11 with native CLI/Desktop sign-in through Portal confirmation (§14). Built so far: Phase 1 (durable sessions and the Portal cookie credential) and Phase 2 (OIDC login and association, with Okta the named provider). Phase 3 qualification is open; the native sign-in amendment (Phase 4) is decided and not built.
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

The 2026-10-11 amendment adds a fourth decision:

4. **Native clients sign in through Portal confirmation.** The CLI and Desktop
   start a short-lived sign-in request, and the person confirms it in Portal
   after signing in there however their account signs in. One mechanism,
   shaped after RFC 8628 device authorization, serves desktops and headless
   hosts alike; there is no loopback redirect. See §14.

The open items in §19 are the per-deployment inputs for the remaining provider
qualification, not questions about whether to build. Git history keeps the full proposal.

Related: [roadmap](../ROADMAP.md) R5,
[current state](../current-state.md),
[deployment authentication](../deploy/authentication.md),
[Space membership lifecycle](space-membership-lifecycle.md),
[system administration](system-administration.md),
[Agent execution identity and delegation](../proposals/agent-execution-identity-and-delegation.md), and
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
- [14. CLI And Desktop Sign-In](#14-cli-and-desktop-sign-in)
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
provider test tenant and offboarding service-level objective are still
per-deployment inputs, not details an implementation should guess. They gate
the remaining qualification and are listed in §19; they do not reopen whether
to build. The native-client requirement was settled on 2026-10-11 (§14).

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
| Access token | HMAC JWT carries `sub`, `typ`, `sid`, `jti`, `iat`, and `exp`; the configured default lifetime is 15 minutes, and the guard checks the `sid` session on every request | Durable session state bounds logout and revocation; the short default bounds replay where that check is unavailable |
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
the current account, Space, token, and audit models. The native sign-in
amendment (§14) adds one short-lived record, `native_sign_in`. It exists
because different parties start, confirm, and redeem a request, possibly on
different replicas. It is deleted once redeemed or expired.

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
- external identity when the method is OIDC (not built: the shipped row has no
  such column, so the link is found through the user);
- the client label a native client reports, such as `alice-mbp · macOS`
  (planned with §14);
- creation, last activity, absolute expiry, and revocation time.

Refresh-token rows belong to this session instead of being the session. Access
JWT verification requires `typ=access` and a non-empty `sid`, then the central
guard checks both the active user and active session. Space roles and grants
remain later database reads and do not enter the JWT. The access JWT carries no
issuer, audience, scope, or client identifier, so every access token holds the
user's full authority; a narrower, audience-restricted credential is not built.

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
OIDC authorization should send `max_age` when configured and validate
`auth_time`, so restarting a BuildMax session cannot silently accept
arbitrarily old provider authentication. That is not built: the shipped flow
sends neither and reads no `auth_time`. The bound today is the session's
absolute expiry, measured from the BuildMax sign-in.

A native session confirmed in Portal inherits the confirming session's
absolute expiry (§14.5), so these bounds apply to it unchanged.

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

### 7.4 No machine credential as a login side effect

A successful login returns a session credential pair only. A longer-lived
machine credential — a personal access token or service-account credential —
requires a separate, explicit operation that names it, chooses or accepts its
scopes, and chooses an expiry.

This gives logout an unambiguous meaning: it ends the selected human session.
It neither leaves behind a hidden token created at login nor unexpectedly
deletes an automation credential created for a different purpose.

### 7.5 Logout and revocation

BuildMax logout always revokes `auth_session`, refresh tokens, and the Portal
cookie before reporting success. The central guard then refuses an already
issued access token for that `sid`. An administrator can list and revoke a
user's sessions. Self-service session list and revoke routes are not built yet.
The native sign-in amendment adds them (§14.5), because it lets a person create
sessions for other machines.

The first slice means “sign out of BuildMax,” not “sign out of every application
at the provider.” If Discovery advertises `end_session_endpoint`, RP-Initiated
Logout may be added after interoperability evidence, with local revocation
happening first even when the provider redirect fails. Front-channel and
back-channel logout are separate optional capabilities, not implied by OIDC
login.

## 8. OIDC Protocol And Browser Flow

BuildMax uses only Authorization Code Flow with a confidential Server client:

1. Portal navigates to `GET /api/auth/oidc/start` with a relative return path.
   The return path is not built yet: the shipped callback always lands on `/`.
   The Portal confirmation page (§14.4) is the first route that needs it.
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
| `external_identity_id` | Nullable; set only for OIDC authentication. Not built: the shipped row has no such column. |
| `platform`, `auth_method` | Client and proof used |
| `client_label` | Planned with §14: nullable, at most 64 printable characters, as the native client reported it; never trusted |
| `created_at`, `last_seen_at` | Session lifecycle metadata |
| `absolute_expires_at`, `revoked_at` | Authoritative validity |

`user_refresh_token` references `auth_session` and retains only token rotation
state and expiry. The session row is not a third credential. It is the
authoritative state the already-issued `sid` currently lacks.

### 9.3 `native_sign_in`

Planned with §14. One row per pending or decided native sign-in request:

| Field | Requirement |
|---|---|
| `id` | Internal key. There is no public ID, because nothing addresses a request except its code or poll secret. |
| `user_code_hash` | Unique SHA-256 of the normalized user code |
| `poll_secret_hash` | Unique SHA-256 of the poll secret |
| `platform` | `cli` or `desktop` |
| `client_label` | The claimed label, at most 64 printable characters |
| `state` | `pending`, `confirmed`, or `denied` |
| `interval_seconds`, `last_polled_at` | Per-request poll pacing (`slow_down`) |
| `decided_by_user_id`, `decided_session_id`, `decided_at` | Set by the confirmation or denial. The session ID is the confirming Portal session whose method and expiry the native session inherits. |
| `expires_at`, `created_at` | Indexed expiry; creation time |

The row holds no token, email, or IP address. Redemption deletes it in the
transaction that creates the session; `CredentialCleaner` deletes the rest after
expiry.

### 9.4 Ownership

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
- For native sign-in (§14), `internal/core/identity` owns the request type,
  states, lifetime, code format, and store contract.
  `internal/service/identity` owns start, poll, confirm, deny, and redemption,
  including the admission re-check and the inherited expiry.
  `internal/infra/db` owns `native_sign_in` and the redemption transaction.
  `internal/server/handlers/auth` owns the routes. Portal owns the confirmation
  page, and the CLI and Desktop own only presentation and polling.

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

Planned routes for native sign-in (§14). They are not built and not yet in
`openapi.json`:

```text
POST   /api/auth/native-sign-ins            anonymous: start a request
POST   /api/auth/native-sign-ins/poll       anonymous: poll with the poll secret; redeem once confirmed
GET    /api/auth/native-sign-ins?code=      Portal session: look a pending request up
POST   /api/auth/native-sign-ins/confirm    Portal session: {code}
POST   /api/auth/native-sign-ins/deny       Portal session: {code}

GET    /api/auth/sessions                   the acting account's live sessions
DELETE /api/auth/sessions/{session_id}      revoke one of them
```

Confirm and deny are `POST` verbs rather than a `PUT .../state`, because they
act on a live exchange that a polling client is waiting on, and because the
only handle the confirming person holds is the code, which must stay out of paths and
logs. That is the same reason channel pairing confirms with
`POST /api/channel-links {code}`.

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

Native sign-in (§14) adds two permanent actions:

- `auth.native_sign_in_confirmed` and `auth.native_sign_in_denied` record the
  decision. The actor is the confirming user; the target is the confirming
  `auth_session`. The detail is the platform and the claimed client label,
  bounded and marked as claimed.
- Redemption writes the ordinary `user.login` event, with the native platform
  as target and detail `portal_confirmation`. The session row's `auth_method`
  keeps the inherited method, so an operator sees both the provenance and the
  underlying proof.
- Self-service revocation reuses `user.session_revoked`, with the user as
  actor.

These events follow the login policy and are best-effort, like `user.login`;
they are not committed with the decision. Anonymous starts, polls, and expiries
are not audit events. They are operational logs with a bounded reason class,
and never carry the user code, poll secret, or tokens.

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

## 14. CLI And Desktop Sign-In

The first slice shipped **Portal browser SSO only**. That left a gap: native
CLI/Desktop sign-in is the password/login-code `POST /api/auth/login`, which
`local_login` refuses for every ordinary account once a deployment sets the
recommended `system_admins` (or `off`). On an SSO deployment an employee could
use Portal but could not connect the CLI or Desktop to managed models, Issues,
or Remote Control at all.

**Decision (amendment, 2026-10-11).** The maintainer decided that the CLI and
Desktop must be able to sign in to a server whose accounts use corporate SSO,
and chose one mechanism: a BuildMax-mediated sign-in request, shaped after
OAuth 2.0 Device Authorization
([RFC 8628](https://www.rfc-editor.org/info/rfc8628/)), that the person
confirms in Portal. There is no loopback redirect and no second mechanism. The
rest of this section is the design; the request's table is §9.3, its routes
§10, its audit §12.4, and its threats §15.

These rules from the first slice still hold:

- CLI/TUI and Desktop local/direct mode stays fully independent of Server and
  IdP availability. Signing in is how a client enters managed mode
  ([client modes](client-modes.md)); nothing here touches local mode.
- No IdP page is embedded in Desktop and no IdP password is collected by a
  native client. The person signs in to Portal in their own browser, however
  their account signs in there.
- This is a human sign-in. It is not a PAT, service-account, or unattended
  credential design (§7.4, §18).

### 14.1 Why Portal confirmation

The essential outcome is that a person who can sign in to Portal can give a
CLI or Desktop on a machine they control a BuildMax session of their own,
without that client handling their IdP credentials. The constraints are:
Portal already owns every sign-in method (password, login code, OIDC, and any
later protocol under §8a); the IdP knows one confidential client with one
exact redirect URI (§8); clients run on laptops, but also on SSH hosts and
containers with no browser; and the server has no application-owned login
rate limiter.

Portal confirmation satisfies all of them with one mechanism:

- **It needs no new IdP integration.** The IdP still sees only Portal's
  existing authorization-code flow. There is no second redirect URI to
  register, no public client, and no per-client PKCE at the IdP. A future
  protocol, or a password account, works with no change here.
- **It covers headless hosts.** The person confirms on any device with a
  browser, so an SSH session needs no port forwarding. A loopback redirect
  ([RFC 8252](https://www.rfc-editor.org/info/rfc8252/)) works only when the
  browser and the client share a host. It would still need a device-style
  fallback for headless hosts, which is exactly the "both" this record
  refused to build speculatively.
- **The person's decision is explicit and visible.** Portal names the
  requesting client before anything is issued. That is the defense the
  channel pairing precedent already relies on
  ([instant-messaging channels §6](instant-messaging-channels.md#6-pairing)).

The cost is the known weakness of every device-authorization flow: a person
can be talked into confirming someone else's request (consent phishing,
RFC 8628 §5.4). §14.4 and §15 bound it; they cannot remove it.

Vocabulary: the record calls the stored object a **native sign-in request**,
and the person **confirms** or **denies** it. "Device" already names a Remote
Control viewer, and "approval" names a tool approval
([Remote Control](remote-control.md)), so neither word is used for this flow in
routes, audit actions, or interface text.

### 14.2 Flow

1. The client calls `POST /api/auth/native-sign-ins` (anonymous) with its
   platform (`cli` or `desktop`) and a client label it composes from the host
   name and operating system, such as `alice-mbp · macOS`.
2. The server answers with:
   - a **user code** such as `WDJB-MJHT`;
   - a **poll secret**;
   - `expires_in` and `interval` in seconds;
   - `verification_uri`, the Portal page where a code is typed; and
   - `verification_uri_complete`, the same page with the code already filled in.
3. Desktop, and a CLI on a machine with a desktop session, open the system
   browser at `verification_uri_complete`. Every client also shows the URL and
   the code, because the browser may not open or may be on another machine.
4. The person reaches Portal and signs in if needed, by SSO or by password
   where `local_login` allows it. Portal shows the request (§14.4). The person
   checks that the code matches the one on their client, then chooses
   **Confirm** or **Deny**.
5. Meanwhile the client polls `POST /api/auth/native-sign-ins/poll` with the
   poll secret, at the interval the server set.
6. After a confirmation, the next poll creates the session and returns the same
   JSON token pair as `POST /api/auth/login`. The client stores it where it
   already stores a native login, and is signed in as that account.
7. A denial, an expiry, or a cancellation ends the request. No session exists
   unless step 6 happened.

The Portal page is a hash route, `<base>/#/sign-in/<code>` for the complete URI
and `<base>/#/sign-in` for typing a code. `<base>` is `public_base_url` when
configured, otherwise the origin the client called. A hash fragment never
reaches a server or proxy log, the reason channel pairing links use one. The
URL is returned to the caller who started the request, never used for a
server-side redirect, so deriving it from the request origin lets a forged
`Host` mislead only that caller.

### 14.3 The sign-in request

| Property | Decision | Why |
|---|---|---|
| Storage | One MySQL row per request, `native_sign_in` (§9.3) | Start, confirmation, and polls can land on different replicas. All credential and pairing state already lives in the database. The coordination store holds only streams, fan-out, and leases, and it has no durable expiring-record API; in `local` mode it is process-local. |
| Lifetime | 10 minutes from creation, fixed, not configurable | It matches channel pairing and the OIDC transaction cookie. That is long enough to sign in through an IdP and short enough to bound phishing and guessing. Confirmation must also complete within it. |
| User code | 8 characters from `ABCDEFGHJKMNPQRSTUVWXYZ23456789` (31 symbols, about 40 bits). It is shown as `XXXX-XXXX` and accepted in any case, ignoring hyphens and spaces. | This is the channel pairing alphabet and normalization, shared as one implementation rather than copied. It has no ambiguous characters (`0/O`, `1/I/L`) and can be read aloud or typed on a phone. |
| Poll secret | `bmxsignin_` followed by 32 random bytes in base64url | It is the bearer that redeems the session, so it carries full entropy. The prefix makes it recognizable to secret scanners, as `bmxlogin_` and `bmxrefresh_` already are. |
| At rest | Only the SHA-256 of the normalized user code and of the poll secret is stored, each under a unique index. A collision on creation draws a new code. | A database read cannot recover either one. |
| Single use | A request is `pending`, then `confirmed` or `denied`. The first decision wins. A confirmed request is redeemed once: the poll that creates the session deletes the row in the same transaction. | A replayed poll secret or a second decision finds nothing to act on. |
| Poll interval | Starts at 5 seconds. A poll earlier than the stored interval answers `slow_down` and raises the stored interval by 5 seconds, up to 30. | These are RFC 8628 §3.5 semantics. The request row holds the per-request state, so no limiter infrastructure is needed. |
| Cleanup | The existing hourly `CredentialCleaner` deletes expired requests | The same owner already sweeps login codes and refresh tokens |

Polls use the server's existing error body, `{"error": "<code>"}`, which is
also the OAuth error shape. The codes are the RFC 8628 names:

| Answer | Meaning | Client does |
|---|---|---|
| `200` with the token pair | Confirmed and redeemed | Store the pair and report the account |
| `400 authorization_pending` | Not decided yet | Wait `interval` and poll again |
| `400 slow_down` | Polled too early | Add 5 seconds to its interval |
| `400 access_denied` | The person denied it, or the confirming account or session stopped being usable | Stop and say the sign-in was not allowed |
| `400 expired_token` | Expired, already redeemed, or an unknown secret, which are indistinguishable on purpose | Stop and offer to start again |

The client also backs off on network errors and `5xx` (doubling to at most 30
seconds) and stops at `expires_in`. It never polls after the request expires.

### 14.4 Confirmation in Portal

Portal shows the confirmation page to a signed-in Portal session. The page
renders before the Space gate, so an account with no usable Space can still
confirm. If no session exists, Portal's sign-in gate runs first and the person
returns to the page afterwards. For local sign-in the hash route already
survives the gate. SSO needs the validated relative return path §8 describes:
the shipped callback always lands on `/` and would drop the code.

The page looks the request up with `GET /api/auth/native-sign-ins?code=`. The
request log already redacts query keys named `code`. The page then shows:

- the client kind (**BuildMax CLI** or **BuildMax Desktop**) and its client
  label, marked **reported by the client, not verified**. The server cannot
  verify anything a client says about itself; the label exists so the person
  can notice something unexpected, not to prove where the request came from;
- the code, large, with "Check that this matches the code your CLI or Desktop
  shows";
- how long ago the request started and when it expires;
- the account it will sign in as, which is the current Portal account, and when
  the resulting session will end (§14.5); and
- a warning above the buttons: "Only confirm if you started this sign-in
  yourself, just now. If someone sent you this link or code, deny it."

The page does not show the requester's network address. The server has no
trusted-proxy setting, so behind the shared ingress `RemoteAddr` is the proxy,
and a forwarded header is whatever the client wrote. Showing either would
present an unverified fact as evidence. §19 item 9 records what would change
that.

Nothing is decided on page load. With the code prefilled, the person still
clicks **Confirm** (`POST /api/auth/native-sign-ins/confirm`, body `{code}`)
or **Deny** (`POST /api/auth/native-sign-ins/deny`, body `{code}`). A wrong,
expired, or already-decided code shows one "this code is not valid any more"
state; it does not reveal which case applied. After a decision the page tells
the person to return to their client, and removes the code from the URL as the
chat-pairing page does.

Who may confirm:

- **Only a Portal session.** The route refuses any session whose platform is
  not `portal`. Otherwise a stolen CLI token could mint more native sessions,
  and each one would survive the revocation of the token that minted it.
  Confirmation is a browser act, made on the page that carries the warnings.
- **Only an account that could sign in now.** Confirmation re-applies today's
  admission rule for the confirming session's method. A `password` or
  `login_code` session confirms only while `local_login` admits the account.
  An `oidc` session confirms only while OIDC is enabled. The account must be
  active and not a service account. A session opened before an operator
  tightened `local_login` therefore cannot launder itself into a fresh native
  session.

**No recent-sign-in requirement.** Recency does not defend against the threat
that matters: a phished person confirms deliberately, however recently they
signed in. The lifetime bound in §14.5 already keeps the native session from
outliving the confirming sign-in. A step-up re-authentication would add a
second Portal sign-in mode, and for SSO a redirect the IdP usually satisfies
silently, without a corresponding gain. When less than one hour of the
confirming session remains, the page says so and offers **Sign in again**,
which signs Portal out and returns to this page, so the person can choose a
full-length session.

### 14.5 The resulting session

The session is created when the confirmed request is redeemed, not at
confirmation, so no session ever exists that no client holds. Redemption is
one transaction: lock the request; check that it is confirmed and unexpired;
check that the account is active and the confirming session is still active;
create the `auth_session` and its refresh token through the same
`openSession` path every login uses; delete the request.

| Field | Value |
|---|---|
| User | The confirming account |
| `platform` | The request's platform, `cli` or `desktop`. The new route validates it, unlike the free-form value `/api/auth/login` accepts today. |
| `auth_method` | The confirming session's method (`password`, `login_code`, or `oidc`). The proof behind the session is that sign-in. |
| `absolute_expires_at` | The confirming session's `absolute_expires_at`, never later |
| `client_label` | The claimed label from the request (new column, §9.2) |

**It never outlives the authentication that confirmed it.** A native session
confirmed from an OIDC Portal session ends when that session's OIDC
`session_max_age` (default 12 hours) ends. One confirmed from a password
session ends at that session's `session_absolute_ttl` ceiling. `local_login`
therefore applies through the confirming sign-in: the flow is available under
every `local_login` value, because the person reached Portal through whichever
method that value already admits. Under `off` with OIDC enabled, it is the only
way a native client signs in. Under `system_admins`, it is how ordinary SSO
users connect the CLI and Desktop.

After issue the session is independent. It rotates refresh tokens, follows the
30-day inactivity window, and ends on `buildmax logout`, Desktop sign-out,
administrator revocation, account disablement, or its absolute expiry. Signing
out of the Portal session that confirmed it does not end it. Portal sign-out
means "this browser", and the native session is a separately listed, separately
revocable session.

The response is the same `LoginResponse` the clients already parse
(`access_token`, `refresh_token`, `expires_in`, `user`), so storage, refresh,
and the expired/disabled/unavailable classes in
[client modes §8](client-modes.md#8-an-expired-login-does-not-silently-fall-back) do not change. After
success both clients print or show the account they are now signed in as. That
is how a person notices that a code was confirmed by the wrong account (§15).

**Listing and revocation.** The administrator session list shows the new
`client_label` beside platform and method. This flow also makes the person,
not an operator, the one who creates sessions for other machines, so they must
be able to see and end those sessions themselves. Add a self-service list and
revoke under the acting subject's prefix, `GET /api/auth/sessions` and
`DELETE /api/auth/sessions/{session_id}`. Expose them in Portal under
**Account → Sessions**, with the current browser session marked. This replaces
the §7.5 statement that self-service routes are not built. `/api/auth/login`
and both clients also accept and send `client_label`, so a password-signed
native session is just as recognizable in that list.

### 14.6 Abuse bounds

There is still no application-owned login rate limiter (§15, §19 item 7). This
flow therefore bounds itself with the state it already stores, plus the
external per-IP ingress limit that remains mandatory for every anonymous
`/api/auth/*` route:

| Surface | Bound | Response |
|---|---|---|
| Start, anonymous | At most 500 unexpired `pending` requests deployment-wide, counted in the database | `503` `{"error":"too many pending sign-in requests"}` with `Retry-After` |
| Poll, anonymous | Per request through `interval` and `slow_down`. The 256-bit poll secret needs no guessing limit. | §14.3 |
| Lookup, confirm, deny (signed in) | At most 10 unknown-code attempts per account per 10 minutes, per replica, in process (like the channel pairing throttle) | `429` |

The numbers are constants, not configuration, until a deployment shows a need.
A per-source limit on starts is not built, because the server cannot identify
the source behind the ingress (§14.4). A start flood can exhaust the global cap
and briefly refuse native sign-in to everyone. It cannot create sessions,
touch accounts, or affect Portal sign-in. That residual is listed in §15.

The 40-bit code is enough because guessing is authenticated, attributable, and
throttled. With at most 500 live codes, one guess succeeds with probability
below 10⁻⁹, and a successful guess only lets the guesser confirm a stranger's
client into the guesser's own account, which that client then displays.

### 14.7 Client experience

Interface text exists in English and Simplified Chinese for Portal and Desktop,
per [UI experience program D5](ui-experience-program.md#d5-localization-through-a-shared-typed-catalog).
CLI output stays English like every other CLI command, and API error text
stays English.

**CLI.** `buildmax login` keeps its server-URL prompt. It then starts a sign-in
request by default, because that is the one path that works for every account
posture, and it keeps passwords out of terminals. It prints:

```text
To sign in, open this page in a browser and confirm the request:

  https://buildmax.example.com/#/sign-in

Check that the page shows this code: WDJB-MJHT
(The request expires in 10 minutes. Press Ctrl-C to cancel.)

Waiting for confirmation…
Signed in to https://buildmax.example.com as alice@example.com.
```

- It opens the system browser at the complete URI, reusing the
  `openExternalURL` helper the app-connector OAuth flow already uses. It skips
  that step when `SSH_CONNECTION` or `SSH_TTY` is set, or when a Linux/BSD host
  has neither `DISPLAY` nor `WAYLAND_DISPLAY`. When it opens the browser it
  prints the complete URI as well.
- `--no-browser` forces print-only.
- `--password` keeps today's email, password, and login-code prompts against
  `/api/auth/login` (§14.8).
- Denied, expired, and refused requests each print one plain sentence and exit
  non-zero. Ctrl-C stops polling and leaves the request to expire.

**Desktop.** The sign-in page keeps the server address field, then reads
`GET /api/auth/methods` for that server through a new binding:

| `local_login` | Page shows |
|---|---|
| `all` | The password form first, as today. Below the "or" divider, **Sign in with your browser**. |
| `system_admins` | **Sign in with your browser** as the primary action. The password form sits behind "Administrator sign-in with a password". |
| `off` | **Sign in with your browser** only |

When OIDC is enabled, the browser button names the provider: **Continue with
{display_name} in your browser** / **在浏览器中通过 {display_name} 继续**.
Otherwise it is **Sign in with your browser** / **在浏览器中登录**.

Choosing it starts a request, opens the system browser at the complete URI
through the Wails `BrowserOpenURL` runtime call, and replaces the form with a
waiting panel:

- the code, large;
- "Confirm this sign-in in the browser window that opened. Check that it
  shows this code." / "请在已打开的浏览器窗口中确认此次登录，并核对页面显示的代码与此一致。";
- the actions **Open browser again** / **重新打开浏览器**, **Copy link** /
  **复制链接**, and **Cancel** / **取消**;
- polling runs in Go and reports completion as an event.

On a denial or an expiry the panel says so ("Sign-in was denied in the
browser." / "已在浏览器中拒绝此次登录。"; "The sign-in request expired. Start
again." / "登录请求已过期，请重新开始。") and returns to the form. If the
methods request fails, the page shows the existing unreachable-server state
rather than guessing which methods to offer.

**Portal.** The `#/sign-in` page and its states have strings in both locales:

- the request card;
- the "reported by the client, not verified" marker;
- the warning;
- **Confirm sign-in** / **确认登录** and **Deny** / **拒绝**;
- the "return to your CLI or Desktop" result; and
- the invalid-code state.

**`GET /api/auth/methods` does not change.** Confirmation is available whenever
anyone can sign in to Portal, so a flag for it could only ever be true. Clients
and server ship in lockstep (no URL versioning), so there is no older server
for a client to detect.

### 14.8 Password and login-code native login

`POST /api/auth/login` stays, unchanged and governed by `local_login`. It is
the one native path that does not depend on Portal or a second device:

- operator break-glass (§12.3) when the Portal or IdP path is the thing that
  failed;
- the CLI and Desktop end-to-end suites; and
- scripted sign-in on a native deployment.

The clients keep it as the explicit alternative (`buildmax login --password`,
the Desktop password form) but no longer make it the only option. Removing it
would leave break-glass dependent on Portal and buy nothing `local_login: off`
does not already provide for a deployment that wants no native passwords.

### 14.9 Failure modes

| Situation | Result |
|---|---|
| The person closes the browser or never confirms | The request expires after 10 minutes, the client reports it, and no session exists |
| The client exits after confirmation, before its next poll | The confirmed request expires unredeemed, and no session is created |
| The token response is lost in transit | The request is already redeemed, so the next poll answers `expired_token`. The person starts again. The orphaned session has never been used, appears in the session lists, and ends at its expiry or on revocation. |
| Denied | `access_denied`; the client stops |
| The account is disabled, or the confirming session is revoked, before redemption | `access_denied`, and no session is created |
| The confirming session's method is no longer admitted (`local_login` tightened, OIDC disabled) | Confirmation is refused with `403` and a message to sign in again |
| Confirmed by the wrong Portal account | The client shows the account it signed in as. The person runs `buildmax logout` or signs out of Desktop, and that session is revoked. |
| Code typed wrong, expired, or already used | One "not valid any more" state in Portal, counted toward the lookup throttle |
| IdP outage | A still-valid Portal session can confirm. Otherwise Portal cannot be reached through SSO, and native sign-in waits with it, as §12.3 already declares for new SSO sign-ins. |
| Redis or the coordination store unavailable | No effect; the flow uses only the database |
| A different replica serves each step | No effect; every step reads the shared row |
| The pending-request cap is reached | Start answers `503` with `Retry-After`; existing sessions and Portal sign-in are unaffected |
| The browser opens on a host that cannot reach `<base>` | The person opens the printed URL on another device, or the operator sets `public_base_url` |

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
| IdP disables user | Fixed OIDC session age; chat links stop after `channels.sign_in_window` without a new sign-in ([instant-messaging channels §6](instant-messaging-channels.md#6-pairing)); manual BuildMax disable for immediate effect | OIDC alone cannot meet a shorter offboarding SLO; chat access lasts up to the sign-in window, not the OIDC session age |
| JWKS rotation | Cache plus one unknown-key refresh, exact issuer and algorithm restrictions | Bad provider rollout can interrupt login |
| Client-secret compromise | Deployment-secret injection, redaction, overlap rotation runbook | Symmetric client authentication remains weaker than `private_key_jwt` |
| XSS in Portal | No renewable credential in JavaScript-readable storage; short access token in memory | XSS can act during the current page/session |
| Brute force on local fallback | Existing external ingress rate limit remains mandatory; local login restricted to admins | Built-in distributed auth rate limiting is still absent |
| Lost audit write | Identity link/unlink event is transactional | Ordinary login audit remains best-effort |
| Native sign-in consent phishing: an attacker starts a request and persuades a person to confirm it (§14) | No decision on page load. The page names the client and its claimed label, asks the person to compare the code, and warns against links someone sent. The request lives 10 minutes, only a Portal session can confirm, the decision is audited, and the native session is listed with its label and revocable by the person. | A person who confirms anyway gives the attacker a session until the confirming sign-in's expiry, up to 12 hours under SSO; this is RFC 8628 §5.4's inherent limit |
| Spoofed client label or platform | The label is shown as "reported by the client, not verified", and nothing authorizes on it | A malicious client can claim any name |
| Session donation: a guessed or leaked code confirmed by a different account | A ~40-bit code that lives 10 minutes; lookups are authenticated and throttled; clients display the account they signed in as | A person who ignores that account works in the wrong one until they sign out |
| Poll-secret theft | 256-bit secret, hashed at rest, redeemed once, sent only by the starting client | Whoever holds the secret when the request is confirmed receives the session |
| A stolen token used to mint more sessions | Only a `portal` session can confirm; the confirming method is re-admitted; native sessions inherit the confirming session's expiry | Portal XSS can confirm an attacker's request and obtain a session that outlives the page, up to the Portal session's expiry |
| Anonymous start flood | A global cap of 500 pending requests, the mandatory external per-IP ingress limit, and nothing written beyond the request row | A flood can refuse native sign-in to everyone until requests expire; no per-source limit exists without a trusted client address |
| Code guessing through lookup | Authenticated, attributable, at most 10 unknown codes per account per 10 minutes per replica | The per-replica count multiplies with replicas; one guess still succeeds with probability below 10⁻⁹ |

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
| Native sign-in through a system-browser loopback redirect (RFC 8252) | It works only where the browser and the client share a host. SSH and container hosts would need a second, device-style mechanism, and the OIDC leg would need its own transaction, return handling, and client-bound callback. Portal confirmation covers both with one mechanism. |
| Loopback and device flow together | Two mechanisms, two test matrices, and two sets of failure modes, for an outcome one mechanism meets |
| The IdP's own device-authorization grant | Not every IdP offers it. It would make native clients public OIDC clients with a second IdP registration, and it bypasses Portal's password and break-glass paths. BuildMax only needs to know which BuildMax user confirmed. |
| Copy-paste a token from Portal into the client | It puts a renewable bearer credential on the clipboard and in terminal history, and it is a PAT in all but name (§7.4, §18) |
| Native session lifetime independent of the confirming sign-in | Confirming from an 11-hour-old SSO session would mint a fresh 12-hour session with no provider involvement, defeating §7.2's reauthentication bound |
| Require a recent Portal sign-in to confirm | It does not stop a phished person who confirms deliberately. It adds a step-up sign-in mode, and inheritance already bounds the lifetime (§14.4). |
| Remove password and login-code native login | That would leave break-glass and the client suites dependent on Portal, for no gain over `local_login: off` (§14.8) |

## 17. Delivery And Verification

Phases 1 and 2 are implemented. The remaining Phase 3 qualification becomes
independently reviewable tasks only when the relevant deployment inputs and
acceptance criteria in §19 are settled. Phase 4, native sign-in, is decided and
decomposed into backlog tasks:

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
- Shorten the configured default access-token lifetime to 15 minutes; the
  durable session guard provides prompt revocation within it.
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

### Phase 4 — native sign-in through Portal confirmation (decided, not built)

The [backlog](../backlog/README.md) tasks
[60](../backlog/60-native-sign-in-server.md),
[62](../backlog/62-portal-native-sign-in-confirmation.md),
[64](../backlog/64-cli-native-sign-in.md),
[66](../backlog/66-desktop-native-sign-in.md), and
[68](../backlog/68-self-service-sessions.md) carry it. The work is:

- the `native_sign_in` request, its routes, the redemption transaction,
  abuse bounds, audit, OpenAPI, and the `client_label` session column;
- the Portal confirmation page, and the OIDC return path it needs;
- `buildmax login` through confirmation, with `--password` and `--no-browser`;
- Desktop sign-in that reads `GET /api/auth/methods` and offers the browser
  path; and
- self-service session list and revoke.

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
| Native sign-in | Service tests for every request state, `slow_down`, expiry, single redemption, the first decision winning, the `portal`-only confirmer, the admission re-check, and inherited expiry. Real-MySQL tests for concurrent confirm/redeem and cleanup. A kind run in which the CLI signs in through the mock OIDC provider's Portal session, and the Desktop browser path. |
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
- PATs, credentials for service accounts, or unattended-client credentials.
  PATs are reconsidered only for a named personal scripting or API use case.
  Space-owned service accounts exist
  ([Space Assistants §6](space-assistants.md#6-service-accounts)): a `user` row
  of kind `service` with no email, password, login code, SSO link, or session,
  which every sign-in path refuses and which holds no credential. Any wider
  automation-principal lifecycle belongs to
  [Agent execution identity and delegation](../proposals/agent-execution-identity-and-delegation.md).
  The existing webhook key is not a PAT to widen: it has a name, owner, hash,
  and creation time but no scopes, audience, expiry, last-used time, or revoked
  state, and stays an inbound-webhook credential.
- A native client that authenticates to the IdP itself (loopback redirect, the
  IdP's device grant, or an embedded IdP page). Native clients sign in only
  through Portal confirmation (§14), and local CLI/TUI and Desktop remain
  supported independently.
- A new identity-provider database entity, dynamic client registration, or an
  admin UI that mutates `server.yaml` and its secret.
- Changing Space role policy, ownership rules, invitation semantics, TaskRun
  authority, or worker credentials.
- Claiming compliance-grade audit while ordinary event writes remain
  best-effort.

## 19. Open Per-Deployment Inputs

The direction is accepted. The list below records which deployment facts have
been resolved and which still gate the remaining qualification:

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
5. **Native managed mode.** **Decided 2026-10-11: yes.** Ordinary users must
   be able to connect the CLI and Desktop to an SSO server. The maintainer chose
   one mechanism: a BuildMax-mediated, device-authorization-shaped request
   confirmed in Portal, with no loopback redirect (§14).
6. **Requirement scope.** Which target deployment outcomes need SSO beyond
   Portal browser sign-in and native sign-in through Portal confirmation? The enterprise-requirements inventory can
   guide discovery but cannot change the security or interoperability bar here.
7. **Rate limiting.** Which shared limiter protects the remaining local login
   and OIDC transaction endpoints in the supported topology? Until answered,
   current external rate-limiting guidance remains mandatory. Native sign-in
   bounds itself without one (§14.6). A shared limiter would replace its
   in-process lookup throttle, not add a second one.
8. **Password-change session policy.** Should changing a password revoke all of
   the account's sessions, only password-authenticated sessions, or none? Today
   existing sessions survive a password change (see
   [deployment authentication](../deploy/authentication.md#passwords)).
   Should native sessions confirmed from a password Portal session follow the
   same answer? By §14.5 they inherit that session's method, so the same rule
   would cover them.
9. **Trusted client address.** BuildMax derives no client IP behind the
   ingress, so the confirmation page shows no network address and native
   sign-in starts have no per-source limit (§14.4, §14.6). A `trusted_proxies`
   setting would let the server read the forwarded address only from the
   ingress. That would enable both, and the login rate limiter in item 7. It
   is a cross-cutting change, so it waits for that limiter or for a deployment
   that asks for the address.

Decision evidence is a written operator journey, IdP configuration export or
equivalent reproducible facts, an agreed joiner/leaver bound, a threat-model
review, and a provider test plan. Feature comparison tables or “enterprise
tools usually have SSO” are not enough.

## 20. Documentation And Delivery Status

This record is the accepted direction; the originating proposal is retired to Git
history. Phase 1's durable-session and Portal-cookie foundation (with a 15-minute
access-token default) and Phase 2
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

Phase 4, native CLI/Desktop sign-in through Portal confirmation (§14), was
decided on 2026-10-11 and is not built. Until it ships, a native client signs
in only through `POST /api/auth/login`, which `local_login` governs.

`docs/current-state.md`, `docs/deploy/authentication.md`,
`docs/reference/configuration.md`, and OpenAPI describe the shipped behavior;
each remaining Phase 3 slice updates them in the same contribution that ships it.
