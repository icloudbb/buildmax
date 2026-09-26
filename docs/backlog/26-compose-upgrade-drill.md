---
id: compose-upgrade-drill
title: Rehearse upgrading from a tagged release in Compose
roadmap: R2
source: docs/deploy/beta-readiness.md
depends_on: [10-refuse-start-on-newer-schema.md, 20-predecessor-schema-upgrade-test.md]
verification: [compose]
claim: gougoujiang 2026-09-26
pr:
---

## Outcome

Before a release, the real predecessor binary is upgraded to the candidate with
real data, and the old binary is shown to refuse the upgraded database.

## Scope

A dispatch or release-time command: Compose up at the source tag, seed, swap to
the candidate image, verify through the API, then start the source image again
and assert it refuses to start (task 10); recover by restoring the pre-upgrade
backup.

## Acceptance Criteria

- The drill runs from a workflow dispatch or release-prepare and records its result.

## Verification

The drill locally against Compose.
