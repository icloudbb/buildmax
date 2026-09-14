# Timestamp Representation

> **简体中文：** [阅读中文镜像](../zh-CN/design/时间戳表示.md)

> **Audience:** contributors and database reviewers · **Status:** implemented

How BuildMax spells "this happened at a moment in time". One rule across three
layers: `time.Time` in Go, `DATETIME(6)` in MySQL, RFC 3339 in JSON.

This record reverses the previous rule, which mandated Unix seconds in
`bigint` columns and said never `DATETIME`. The rule as built now lives in
[data model](../contribute/architecture/data-model.md) and
[conventions](../contribute/conventions.md); what follows is why.

Related: [data model](../contribute/architecture/data-model.md),
[store](../contribute/architecture/store.md),
[conventions](../contribute/conventions.md),
[entity identity](entity-identity.md) — the precedent for a single coherent
Alpha schema change — and
[configuration](../reference/configuration.md) for the database DSN.

## Contents

- [1. Problem](#1-problem)
- [2. Goals And Non-Goals](#2-goals-and-non-goals)
- [3. Decisions](#3-decisions)
- [4. Why `DATETIME(6)`](#4-why-datetime6)
- [5. Scope](#5-scope)
- [6. Existing Databases](#6-existing-databases)
- [7. Status And Open Questions](#7-status-and-open-questions)

## 1. Problem

Every persisted instant in the server was an `int64` count of seconds since the
epoch. The rule was deliberate and documented; it was also the wrong default
once the schema grew large enough to be queried by hand and consumed by two
frontends.

The tree this replaced. `internal/core/model` was the domain package at the
time; it has since been split into one package per domain, so the counts below
are a snapshot of what the change touched rather than a map of the tree today:

| Where | Count |
|---|---|
| Timestamp fields in `internal/infra/db` row structs | 63 |
| Of those, filled by GORM `autoCreateTime` / `autoUpdateTime` | 29 |
| Timestamp fields in `internal/core/model` | 73 |
| `XxxAt int64` declarations across `internal`, non-test | 222 in 69 files |
| Non-test call sites converting through `Unix()` / `time.Unix()` | 116 |
| Portal and Desktop files reading an `*_at` field | 18 |

Five costs follow from the representation, not from any one call site.

**A raw query is unreadable.** `SELECT created_at FROM task_run ORDER BY
created_at DESC LIMIT 5` answers with five ten-digit integers. Every ad-hoc
diagnosis wraps columns in `FROM_UNIXTIME`, every hand-written predicate wraps
its bound in `UNIX_TIMESTAMP`, and a wrapped column cannot use its index. The
people most likely to run those queries — an operator diagnosing a stuck run —
are the ones least served by the encoding.

**Second resolution collides.** A burst of `conversation_message` or
`workflow_step_run` rows lands inside one second and becomes indistinguishable
by time. Stable ordering is a separate fix (§3, D6), but the encoding
guarantees the collision rather than merely permitting it.

**The type does not carry the meaning.** `int64` is also the type of a
duration, a token count, a retry attempt, and a millisecond timestamp. Nothing
at a package boundary distinguishes `EndedAt` from `TimeoutSeconds`; a
milliseconds-instead-of-seconds mistake type-checks, stores, and reads back as
a date in the year 57 000.

**Absence is spelled two ways.** Most nullable timestamps are `*int64` and
absent means `NULL`. But `plugin.archived_at` and `plugin_release.yanked_at`
are `not null;default:0`, where `0` means "not archived" — a sentinel that is
also a legitimate instant, in a schema that already had the nullable idiom
available.

**The repository already runs a second convention.** Session files carry
`CreatedAt time.Time` and an RFC 3339 string
(`internal/core/session/session.go`); traces and job logs write an RFC 3339 Nano
`ts` (`internal/infra/trace/record.go`). So the file layer already chose the
representation this record proposes for the database, and every boundary
between them converts. The conversion is also not uniform: `trace/joblog.go`
stamps `time.Now()` in local time while `trace/record.go` stamps
`time.Now().UTC()`.

BuildMax is in Alpha and owes no compatibility to a released API or an existing
database, which is what makes this cheap now and expensive later — the same
argument, and the same window, that [entity identity](entity-identity.md) used.

## 2. Goals And Non-Goals

**Goals.** One representation per layer, derivable from the field's meaning
rather than from its neighbours. SQL that a human can read and write without a
conversion function. Sub-second precision. Absence expressed as `NULL`, once.
Every write in UTC, with the connection pinned so the database agrees. One
coherent schema change with no dual-read path.

**Non-goals.** Durations, quotas, retry counts, and token counts are not
instants and do not change (§3, D7). The session and trace file formats already
use RFC 3339 and are untouched. Ordering semantics do not change: microsecond
precision is not a licence to sort on a timestamp alone (D6). No database
foreign keys — [entity identity](entity-identity.md) §8 owns that decision and
this record does not reopen it. No timezone-aware "local wall time"
type: BuildMax stores instants, and a user's timezone is a presentation
concern.

## 3. Decisions

| # | Decision |
|---|---|
| D1 | A persisted instant is `time.Time` in Go. Optional means `*time.Time`, and `NULL` means the event has not happened — `ended_at IS NULL` is still "running", not "unknown". |
| D2 | The MySQL column is `DATETIME(6)`. Not `TIMESTAMP`, not `BIGINT`, not `DATE`. §4 gives the reasoning. |
| D3 | The API renders RFC 3339 with a `Z` offset. Go's `encoding/json` already marshals `time.Time` this way; a nil `*time.Time` renders `null`. |
| D4 | Every write is UTC — `time.Now().UTC()`. `db.New` normalizes whatever DSN it is handed: `loc=UTC` so the driver stops reading `DATETIME` values in the process's local zone, and a `time_zone` of `+00:00` so `NOW()` and `CURRENT_TIMESTAMP` agree with the application. Enforced there rather than where the DSN is built, so an operator-supplied or test DSN cannot opt out. |
| D5 | Sentinel zeros end. `plugin.archived_at` and `plugin_release.yanked_at` become `*time.Time`, and "not archived" is `NULL`. |
| D6 | List ordering and keyset pagination stay `created_at, id`. Microseconds narrow collisions; they do not make a single-column sort stable, and a page boundary that compares only a timestamp can still skip or repeat a row. |
| D7 | Durations, quotas, and counts stay `int64` / `BIGINT` / JSON number, and the field name carries the unit: `TimeoutSeconds`, `duration_ms`. A field named for a unit is never a `time.Time`. |
| D8 | A pure calendar date — a value with no meaningful time of day — is `DATE` and `"2026-08-23"`. The schema has none today; the rule exists so the first one does not become a `DATETIME(6)` at midnight. |
| D9 | An architecture test enforces D1, D2, D5, and D7, in the style of `internal/architecture/entity_identity_test.go`: no `XxxAt int64` in a row struct or a core model, no timestamp column declared `bigint`, no `not null;default:0` timestamp. |

GORM's `autoCreateTime` and `autoUpdateTime` work natively on `time.Time`, so
the 29 tagged columns keep their behavior and lose their `Unix()` conversion.

## 4. Why `DATETIME(6)`

Against `BIGINT` seconds — the rule this replaced — the case is §1: readability,
precision, and a type that means something.

Against `TIMESTAMP`, which is the other MySQL instant type:

| | `DATETIME(6)` | `TIMESTAMP(6)` |
|---|---|---|
| Stored value | The literal value written | Converted from the session timezone to UTC on write, back on read |
| Correctness depends on connection state | No | Yes — the same row reads differently under a different `time_zone` |
| Range | Year 1000 to 9999 | 1970-01-01 to 2038-01-19 |
| Legacy auto-behavior | None | `DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP` applied implicitly to the first column, depending on server mode |
| Storage | 8 bytes | 7 bytes |

`TIMESTAMP` makes the stored value depend on the timezone of whichever client
wrote it — an operator's `mysql` shell and the server process do not
necessarily agree — and 2038 is inside the plausible life of stored audit data.
D4 pins the session timezone anyway, which removes `TIMESTAMP`'s one advantage
while leaving its range limit.

The byte the extra range costs is not a real cost: `DATETIME(6)` is 8 bytes,
exactly what the `BIGINT` it replaces occupies today, so index and row sizes
are unchanged.

## 5. Scope

| Layer | What changes |
|---|---|
| `internal/infra/db` | 63 row-struct fields to `time.Time` / `*time.Time`; the aggregations in `quota_usage.go` and audit search take `time.Time` bounds; `schema_migration.applied_at` included |
| `internal/infra/db` connection | `utcDSN` rewrites the DSN's `loc` and `time_zone`; `store.New` asks the driver for `DATETIME(6)` rather than GORM's default `DATETIME(3)` |
| `internal/core/model` | 73 fields; JSON tags keep their `snake_case` names and change type only |
| Services, handlers, scheduler, bootstrap | 116 `Unix()` / `time.Unix()` conversions disappear or move to a display boundary. The audit `since` / `until` query parameters now take RFC 3339, not epoch seconds |
| `internal/mock` | 11 declarations, to keep the in-memory store type-compatible |
| Portal, Desktop, `gui` | 18 files; `new Date(x * 1000)` becomes `new Date(x)`, and the API types change `number` to `string`. Desktop needed nothing: its Wails bindings already spoke RFC 3339 |
| `internal/infra/workerclient` | `api_types.go` `CreatedAt` — the worker API is a wire contract between two BuildMax processes and moves with them |
| Docs and tests | The timestamp paragraph in `data-model.md` and its 62 column rows, a new rule in `conventions.md`, `internal/architecture/timestamp_test.go`, a changelog entry |

All of it landed as one change. A half-converted tree does not compile, and a
dual-read path is exactly what Alpha exists to avoid.

Two things the inventory did not predict. `Note.WrittenAt` and `Todo.WrittenAt`
in `internal/core/agent` were never instants — they count loop iterations — and
are now `WrittenIteration`, which is what D7 asks of a name. And the Portal's
access-token expiry deadline stays a number: it is computed from `expires_in`
and held with the access token in module memory, not persisted as an instant in
any of the three layers this record governs.

## 6. Existing Databases

`AutoMigrate` owns additive DDL only, and a `bigint` to `DATETIME(6)` column
change is not additive: MySQL would attempt to read `1755950000` as a datetime
literal and either reject it or store zero. `var migrations []Migration` in
`internal/infra/db/migration.go` is empty today — the entity identity change
took the same position — and the recorded-migration hook runs *after*
`AutoMigrate`, which is too late to convert a column `AutoMigrate` has already
altered.

So: an Alpha database is recreated, not converted. No conversion migration
ships. An operator who wants to keep a particular database converts it by hand
before upgrading, column by column, with a new `DATETIME(6)` column,
`UPDATE t SET new_col = FROM_UNIXTIME(old_col)`, a drop, and a rename. This is
stated so nobody discovers it during an upgrade.

If a future change needs a genuine pre-`AutoMigrate` step, that ordering hook is
its own decision, not something to add quietly here.

## 7. Status And Open Questions

Implemented. `internal/architecture/timestamp_test.go` enforces D1, D2, D5, and
D7 against the row structs, the core models, and the agent package.

**Open — RFC 3339 Nano's variable precision.** Go marshals `time.Time` as
RFC 3339 Nano, which trims trailing zeros: `2026-08-23T14:30:00Z` and
`2026-08-23T14:30:00.123456Z` both appear, depending on the value. Every
consumer in the tree parses with `Date` or `time.Parse`, which handle both, so
the default stands. If a consumer ever needs fixed-width output, that is a
custom marshaller on one type, not a reason to keep integers.

**Open — CLI and TUI display.** The commands that printed `time.Unix(...)`
now format a `time.Time` in local time — `util.FormatMinute`, the operator
grant table, the login-code expiry line. That was a mechanical conversion, not a
redesign: what each command should show a person is still a per-command
question.
