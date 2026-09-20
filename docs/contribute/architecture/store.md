# Store

> **简体中文：** [阅读中文镜像](../../zh-CN/contribute/architecture/store.md)
> **Audience:** contributors · **Status:** current

## Purpose

`internal/infra/db` provides the MySQL/GORM persistence implementation for
domain repository contracts. Most shared contracts live in the per-domain
`internal/core/*` packages — `core/task`, `core/space`, `core/issue`, and the
rest. Capability-specific ports may live with their consumer, such as the
Marketplace catalog and activation ports in `internal/service/plugin`.

The active persistence model is space-scoped for shared work:

- user / auth_session / external_identity / login_code / user_refresh_token / user_webhook_key / system_grant
- space / space_member / space_invitation / secret
- conversation / conversation_message
- issue
- agent / agent_revision
- workflow / workflow_revision / workflow_run / workflow_node_run / schedule
- task / task_run / workspace_checkpoint / plugin_environment
- artifact (durable space files; see data-model.md)
- quota_tier
- llm_model / llm_call / audit_event / plugin / plugin_release / plugin_activation

Table names are singular per project convention. For every column, index, and
relationship, the row structs in `internal/infra/db` are authoritative. See
[data-model.md](data-model.md) for the key relationships and schema rationale.

There is no usage table. `SpaceUsageInWindow` aggregates on read: it counts
`task_run` rows joined to `task` by space and sums their prompt and completion
tokens, plus the title-generation tokens recorded on tasks created in the same
window. Metering therefore has no separate write path to keep in sync. It
resolves the space's handle once and is numeric after that, which is why both
halves are answered from indexes without reading a row.

## Key Boundaries

| Layer | Package | Role |
|-------|---------|------|
| Shared contracts/entities | `internal/core/<domain>` | Shared structs and cross-service repository interfaces, one package per domain |
| Consumer-owned ports | `internal/service/*` | Narrow persistence capabilities used by one orchestrator |
| GORM implementation | `internal/infra/db` | MySQL-backed store implementing those interfaces |
| Object storage | `internal/infra/objectstore` | Space home files, run-scoped BUILDMAX_HOME state, and artifact content; local FS or S3/MinIO |

## The Translation Boundary

This package is where a caller's handle becomes a row key, and the only place
either representation meets the other.

A repository interface speaks public IDs, because the `internal/core/*` domain
packages hold nothing else. Inside, `lookupKey` resolves one to the `bigint` the
schema joins on, `publicIDForKey` goes back the other way, and
`createWithPublicID`
generates a handle and retries when the unique index rejects that particular
value — telling a generated collision from a duplicate email by the index the
error names.

A read that returns handles joins for them rather than resolving them one row
at a time. Each entity has one `xxxSelect` builder holding its join set, shared
by every read of that entity so the detail read and the listings cannot drift
apart, and each join is a primary-key lookup. The read struct holds its row as
a **named** field tagged `gorm:"embedded"`: GORM reads an anonymous embedded
struct that carries its own `TableName` as an association and scans none of its
columns, which produces a zero-valued row and no error.

A locking read never joins. `SELECT ... FOR UPDATE` over a join locks the
joined rows too, so a token rotation that resolved its owner inside the locking
read would hold the account for the length of the transaction.

Rationale and the table-by-table decisions:
[../../design/entity-identity.md](../../design/entity-identity.md).

## Notes

- A public handle is 96 bits of crypto-random data, stored as its 20-character
  lowercase base32 text in `char(20) ascii_bin`. `internal/util/id.go` is the
  codec.
- Session IDs are the exception: they are internal and use UUIDs.
- JSON/API fields use `snake_case`, and a resource names its own handle `id`.
- `internal/bootstrap/server.go` opens the DB and injects the store into handlers and services.
- See also: [Server](server.md), [Configuration](config.md).
