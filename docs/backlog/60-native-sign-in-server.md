---
id: native-sign-in-server
title: Serve native sign-in requests that a Portal session confirms
roadmap: R5
source: docs/design/enterprise-identity-and-access.md#14-cli-and-desktop-sign-in
depends_on: []
verification: ["./make test", "./make test mysql", "./make kind smoke", "./make check docs"]
claim:
pr:
---

## Outcome

A native client can start a sign-in request and redeem it for an ordinary
BuildMax session once a signed-in Portal user confirms it. This is the server
half of the 2026-10-11 decision that lets the CLI and Desktop sign in to an SSO
deployment, where `local_login: system_admins` or `off` refuses their only
current path, `POST /api/auth/login`.

## Scope

Implement §14.2–§14.6, §9.2–§9.4, §10, and §12.4 of the design record:

- `internal/core/identity`:
  - the native sign-in request type with its `pending`, `confirmed`, and
    `denied` states;
  - the 10-minute lifetime, the poll-interval rules, and the store contract.
- One user-code implementation shared with channel pairing: 8 characters from
  `ABCDEFGHJKMNPQRSTUVWXYZ23456789`, shown as `XXXX-XXXX`, with the same
  normalization. Move the generator out of `internal/service/channel` rather
  than copying it.
- `internal/infra/db`:
  - the singular `native_sign_in` table, with SHA-256 hashes of the user code
    and of the `bmxsignin_` poll secret, each under a unique index;
  - the nullable `auth_session.client_label` column;
  - a redemption transaction that locks the request, re-checks it, creates the
    session and refresh token, and deletes the request;
  - expired-request deletion in `CredentialCleaner`.
- `internal/service/identity`:
  - start, with the global cap of 500 live pending requests;
  - poll, with the `authorization_pending`, `slow_down`, `access_denied`, and
    `expired_token` outcomes;
  - lookup, confirm, and deny. Confirm accepts only a `portal` session,
    re-applies the admission rule for the confirming session's method
    (`local_login` for `password`/`login_code`, OIDC enabled for `oidc`), and
    lets the first decision win;
  - redemption. The session inherits the confirming session's `auth_method`
    and `absolute_expires_at`, and takes the request's validated platform
    (`cli` or `desktop`) and claimed `client_label`.
- `internal/server/handlers/auth`:
  - the five `/api/auth/native-sign-ins` routes, with error bodies in the
    existing `{"error": "<code>"}` shape;
  - `verification_uri` and `verification_uri_complete` built from
    `public_base_url`, or from the request origin when it is unset, as
    `<base>/#/sign-in` and `<base>/#/sign-in/<code>`;
  - the in-process lookup throttle of 10 unknown codes per account per 10
    minutes, returning `429`;
  - an optional `client_label` on `POST /api/auth/login`.
- The `auth.native_sign_in_confirmed` and `auth.native_sign_in_denied` audit
  actions, and a `user.login` event with detail `portal_confirmation` on
  redemption.
- Show `client_label` in the admin session list response.
- `internal/server/static/openapi.json` for every new route and field.
- Update `docs/deploy/authentication.md`, `docs/contribute/architecture/server.md`,
  and `docs/current-state.md` for the server behavior, and add a changelog
  entry.

## Out Of Scope

- The Portal confirmation page and the OIDC return path:
  [62](62-portal-native-sign-in-confirmation.md).
- CLI and Desktop client changes: [64](64-cli-native-sign-in.md),
  [66](66-desktop-native-sign-in.md).
- Self-service session routes: [68](68-self-service-sessions.md).
- A shared login rate limiter or trusted-proxy client address (design §19
  items 7 and 9).
- Changing `POST /api/auth/login` admission or removing password/login-code
  native login (design §14.8).

## Acceptance Criteria

- Start returns `user_code`, `poll_secret`, `expires_in` (600), `interval` (5),
  `verification_uri`, and `verification_uri_complete`. Only hashes are
  persisted, and the codes appear in no log line.
- A poll before the interval answers `slow_down` and raises the stored interval
  by 5 seconds, up to 30. A poll after expiry, after redemption, or with an
  unknown secret answers `expired_token`.
- Confirming from a `cli` or `desktop` session is refused. Confirming from a
  `password` Portal session after `local_login` stopped admitting that account
  is refused. A second decision on the same code reports the code as not valid.
- The redeemed session has the request's platform and label, the confirming
  session's method, and exactly the confirming session's absolute expiry. Its
  response body matches `POST /api/auth/login`.
- Redemption refuses (`access_denied`) when the account was disabled or the
  confirming session was revoked after confirmation, and creates no session.
- Concurrent redemption of one confirmed request creates exactly one session.
- The 501st live pending request answers `503` with `Retry-After`.
- The OpenAPI architecture tests pass with the new routes.

## Verification

- `./make test` for core, service, and handler table tests covering every state
  and refusal above, and the OpenAPI and architecture checks.
- `./make test mysql` for the `native_sign_in` and `auth_session` changes:
  uniqueness, the redemption transaction under concurrency, and cleanup.
- `./make kind smoke` on an ephemeral cluster
  (`BUILDMAX_KIND_EPHEMERAL=1 ./make kind up`) to drive start, confirm with an
  SSO Portal session from the mock OIDC provider, and redeem across the two
  server replicas.
- `./make check docs`.

## Notes

The channel pairing code (`internal/service/channel/links.go`,
`internal/infra/db/channel_identity.go`) is the closest precedent for code
format, hashing, and locked consumption. `openSession` in
`internal/service/identity/login.go` takes a TTL today. Redemption needs an
absolute expiry, so pass the inherited timestamp rather than recomputing it.
The auth handler has no client-IP source behind the ingress; do not add
`X-Forwarded-For` parsing in this task.
