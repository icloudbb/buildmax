---
id: predecessor-schema-upgrade-test
title: Upgrade a real predecessor schema in the MySQL scope
roadmap: R2
source: docs/deploy/beta-readiness.md
depends_on: [10-refuse-start-on-newer-schema.md]
verification: ["./make test mysql"]
claim:
pr:
---

## Outcome

Every pull request proves the candidate upgrades a named predecessor's real
schema and data, not only the current structs.

## Scope

A `tools/mk` command that runs a tagged image (default: the previous Alpha tag,
declared per candidate) against scratch MySQL, seeds a fixed dataset, and dumps
schema, data, and ledger into `internal/infra/db/testdata/schema/<tag>.sql`; a
MySQL-scope test that loads it, runs the candidate's `db.New`, and asserts the
named entities survive and the ledger holds the new IDs. Start with
0.2.0-alpha.14 (its upgrade exercises `schedule_agent_to_executor`).

## Acceptance Criteria

- The fixture and test run in the CI MySQL job.
- The release process names the upgrade source and refreshes the dump.

## Verification

`./make test mysql`.
