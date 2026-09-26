---
id: refuse-start-on-newer-schema
title: Refuse to start against a schema newer than the binary
roadmap: R2
source: direct
depends_on: []
verification: ["./make test", "./make test mysql"]
claim: gougoujiang 2026-09-26
pr:
---

## Outcome

Starting an older binary against a newer database no longer corrupts it. Today
its AutoMigrate re-adds columns a newer migration dropped (rolling alpha.15 back
to alpha.14 re-adds `schedule.agent_id NOT NULL` filled with 0, so every
schedule silently disappears and new schedules fail after rolling forward).

## Scope

- Decision (maintainer, 2026-09-26): binary rollback is not supported; recovery
  is a coordinated database and bucket restore with matching binaries.
- `db.New` reads the `schema_migration` ledger **before** AutoMigrate and
  refuses when it holds IDs this binary does not know, naming them and the
  restore procedure. An explicit override exists for deliberate recovery. The
  same refusal covers every `db.New` caller (server, admin commands).
- Replace the withdrawn N-1 wording in `migration.go` comments, the log text, and
  the `TestWarnIfSchemaIsAhead` comment.
- Add the missing MySQL test for the `schedule_agent_to_executor` backfill,
  following `TestIssueOwnerExecutorSplitMigration`.
- Documentation: `manual/support.md` Compatibility and
  `docs/contribute/architecture/data-model.md` state the restore-only contract
  and the refusal; fix the migration counts in `docs/current-state.md` and
  `docs/design/verification-program.md`; add a changelog fragment, and note the
  data loss and rollback limit of alpha.13 and alpha.15 as `data-model.md`
  requires.

## Out Of Scope

The predecessor-schema upgrade fixture (task 20) and the release-time upgrade
drill (task 26).

## Acceptance Criteria

- A MySQL-scope test proves a database carrying an unknown migration ID is
  refused before any DDL runs, and that the override lets it start.
- The schedule backfill has a MySQL test.
- No code or documentation still promises N-1 compatibility.

## Verification

`./make test ./internal/infra/db/...`, `./make test mysql`, `./make lint`,
`./make check docs`.

## Notes

Ledger and migrations: `internal/infra/db/migration.go`; `db.New` in
`store.go`. N-1 was withdrawn in commit d85879e9.
