---
id: kek-rewrap
title: Re-encrypt sealed data under the current KEK and gate key retirement
roadmap: R2
source: docs/design/space-secrets.md
depends_on: []
verification: ["./make test", "./make test mysql"]
claim: gougoujiang 2026-09-26
pr:
---

## Outcome

An operator can retire or respond to a compromised KEK: every sealed row can be
re-wrapped under the current key, and the server refuses to run with a row whose
key is not loaded. Maintainer decision 2026-09-26: required before Beta.

## Scope

Implement the designed `buildmax-server secret rewrap` (space-secrets.md,
rewrap section): re-wrap the DEKs of Space Secrets and managed-model credentials
under `current`; a startup check that every referenced `key_id` is loaded; and
the refusal to drop a key still referenced. Model-credential `key_id` lives in
the sealed JSON blob, so either scan it or add a column — decide in the PR.

## Out Of Scope

Deployment configuration of the KEK (task 14). The rotation drill (task 24).

## Acceptance Criteria

- MySQL-scope tests: rewrap moves every row to the current key and stays
  readable; startup refuses an unloaded referenced key with a clear error.
- Operator documentation of the rotation procedure.

## Verification

`./make test ./internal/infra/...`, `./make test mysql`, `./make lint`,
`./make check docs`.
