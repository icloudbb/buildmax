---
id: self-service-sessions
title: List and revoke your own sessions in Portal
roadmap: R5
source: docs/design/enterprise-identity-and-access.md#145-the-resulting-session
depends_on: [60-native-sign-in-server.md]
verification: ["./make test", "./make test mysql", "./make check portal", "./make e2e visual", "./make e2e kind"]
claim:
pr:
---

## Outcome

A person can see every session signed in to their account (browsers, CLIs,
and Desktops, each with its platform and claimed label) and end any of them
without an operator. Native sign-in lets a person create sessions for other
machines, so recovering from a mistaken or phished confirmation must also be
self-service (design §14.5, §15).

## Scope

- `GET /api/auth/sessions` lists the acting account's live sessions:
  - fields: session ID, platform, method, `client_label`, created, last seen,
    and absolute expiry;
  - a `current` flag marks the session that made the request.
- `DELETE /api/auth/sessions/{session_id}` revokes one of the acting account's
  own sessions. Another account's session gets `404`. It uses the same store
  revocation the admin route uses, and audits `user.session_revoked` with the
  user as actor.
- `openapi.json` for both routes.
- Portal **Account → Sessions**: the list, with the label marked as reported by
  the device and the current browser marked. Each other session gets a revoke
  action. Revoking the current session signs this browser out. Strings in
  English and Simplified Chinese.
- Update `docs/deploy/authentication.md` ("There is no self-service
  session-management page"), the design record §7.5 wording, and `manual/`.

## Out Of Scope

- Revoking every session at once, and an admin CLI session verb.
- Any change to administrator session routes beyond what
  [60](60-native-sign-in-server.md) adds.

## Acceptance Criteria

- A CLI session created through native sign-in appears in its owner's list with
  platform `cli` and its label.
- Revoking it there makes the CLI's next request fail as a revoked session. The
  CLI reports that the login expired, per client modes §8.
- A user cannot list or revoke another user's sessions.

## Verification

- `./make test` for handler authorization and the OpenAPI checks.
- `./make test mysql` if the store gains a per-user, per-session query.
- `./make check portal` and `./make e2e visual` for the Account page.
- `./make e2e kind` for a browser case that revokes a native session.
