---
id: native-refresh-on-401
title: Recover sessions after a JWT secret rotation without re-login
roadmap: R2
source: docs/design/enterprise-identity-and-access.md
depends_on: []
verification: ["./make test", portal]
claim: gougoujiang 2026-09-26
pr:
---

## Outcome

Rotating the JWT secret no longer logs CLI, Desktop, and Remote Control users out
for up to a week. In-flight runs are still lost on rotation (accepted, documented;
maintainer decision 2026-09-26).

## Scope

- Native clients (`internal/interface/auth` and its callers, including the
  Remote Control relay) force one refresh on a 401 and retry once, instead of
  mapping every 401 to `ErrLoginExpired`.
- Portal WebSocket refreshes its token before reconnecting after a rejected
  upgrade, so it does not loop on a stale token.
- `access_token_ttl` defaults to 15m as documented; today the viper default of
  168h shadows the core default (`internal/config/server_config.go`).
- Fix the contradictory rotation descriptions in
  `deployment/production/buildmax.yaml` and `docs/deploy/authentication.md`.

## Out Of Scope

A multi-key JWT ring (not needed for Beta). The rotation drill (task 24).

## Acceptance Criteria

- Tests prove a native client recovers from a 401 with a valid refresh token and
  still reports login-expired when the refresh fails.
- A Portal test proves the WebSocket refreshes before reconnecting.
- The default TTL is 15m in code and docs agree.

## Verification

`./make test` for touched packages, the Portal scope, `./make lint`,
`./make check docs`.
