---
id: auth-route-grouping
title: Group authentication routes under the /api/auth/ prefix
roadmap: none
source: docs/design/api-surface-conventions.md#62-authentication-route-grouping
depends_on: []
verification: ["./make test ./internal/server/handlers/auth", "./make e2e cli", "./make e2e kind"]
claim: gougoujiang 2026-09-13
pr:
---

## Outcome

Session and credential routes for the acting subject share one `/api/auth/`
prefix and one OpenAPI tag, so the auth surface has a single place to find and
document, following design §3.7. Today they are scattered directly under `/api`
with only refresh grouped.

## Scope

Move the five auth routes under `/api/auth/` (design §6.2), coherently across
server, OpenAPI, and every client, with no alias left behind:

| Current route | Target |
|---|---|
| `POST /api/otp/request` | `POST /api/auth/otp` |
| `POST /api/login` | `POST /api/auth/login` |
| `POST /api/logout` | `POST /api/auth/logout` |
| `POST /api/password` | `POST /api/auth/password` |
| `POST /api/token/refresh` | `POST /api/auth/token/refresh` |

- Update `internal/server/handlers/auth` route registration.
- Update the public OpenAPI document and tag these paths `auth`.
- Update every client login/logout/refresh/password/OTP call: Portal, Desktop,
  and the CLI login flow.
- Update handler and architecture route/spec tests.

## Out Of Scope

Any change to request bodies, authentication mechanism, JWT/refresh semantics, or
the single-use login-code rules. This is a path move only.

## Acceptance Criteria

- The five routes are served under `/api/auth/` and the old paths no longer
  exist.
- Login, logout, token refresh, password set, and OTP request all work end to end
  from the CLI and Portal against the new paths.
- OpenAPI and the route set match; the architecture check passes.

## Verification

`./make test ./internal/server/handlers/auth` for the handlers and route/spec
match; `./make e2e cli` for the CLI login flow; `./make e2e kind` for the Portal
login round trip against the deployed surface.

## Notes

Auth routes run before a caller has a session (design §1, §3.7); the prefix move
does not change that. Server authentication still requires a JWT secret and login
codes stay single-use (AGENTS.md runtime invariants).
