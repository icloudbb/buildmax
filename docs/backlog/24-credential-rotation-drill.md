---
id: credential-rotation-drill
title: Document and rehearse deployment credential rotation
roadmap: R2
source: docs/deploy/beta-readiness.md
depends_on: [12-native-refresh-on-401.md, 14-kek-in-deployments.md, 16-kek-rewrap.md]
verification: [kind]
claim:
pr:
---

## Outcome

Operators have a rotation procedure per credential with its measured effect,
rehearsed on kind.

## Scope

- A new credential-rotation runbook under docs/deploy: JWT secret, MySQL password (dual
  password), object-storage keys (overlap then drain), managed and direct model
  keys, KEK (add, switch, rewrap, retire), worker TLS leaf and CA bundle; runbook
  entries only for OIDC, Telegram, and Redis. Drain semantics: JWT and TLS-CA
  accept in-flight run loss; storage and provider keys wait for no active runs.
- `./make kind drill rotation` rotating JWT, DB password, storage keys, model
  credential, and KEK through Secret patch + rollout restart, asserting old
  credentials rejected, sessions recovered via refresh, data intact, and
  recording the measured disruption. Kind MinIO needs a non-root user first.

## Acceptance Criteria

- The drill passes on an ephemeral cluster; the runbook matches it.

## Verification

The drill on `BUILDMAX_KIND_EPHEMERAL=1`.
