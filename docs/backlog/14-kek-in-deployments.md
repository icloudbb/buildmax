---
id: kek-in-deployments
title: Configure and document the deployment KEK
roadmap: R2
source: docs/design/space-secrets.md
depends_on: []
verification: ["./make test", "./make check docs"]
claim: gougoujiang 2026-09-26
pr:
---

## Outcome

A production-reference or DigitalOcean deployment can store managed-model
credentials and Space Secrets. Today neither sets `secret.kek_file`, so
`model add --api-key` is refused, and no operator document says the KEK exists
or must be backed up separately from the database.

## Scope

- `deployment/production/` and the DigitalOcean deploy
  (`tools/mk/ocean_deploy.go`, `deployment/ocean/`) mount a KEK Secret and set
  `secret.kek_file`, following the kind setup.
- `docs/reference/configuration.md` documents `secret.kek_file` and its file
  format; the deployment docs say how to generate it, that it is backed up
  separately from the database dump, and that losing it makes sealed credentials
  unreadable.

## Out Of Scope

Re-encryption and key retirement (task 16).

## Acceptance Criteria

- Both deployment paths configure a KEK; `ocean model init` can add a
  credentialed model.
- Configuration and deployment docs cover the KEK.

## Verification

Unit tests for any `tools/mk` change, `./make check docs`, `./make lint`.
