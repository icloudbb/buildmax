# Entity Identity And Relational Keys

> **简体中文：** [阅读中文镜像](../zh-CN/design/实体身份.md)

> **Audience:** contributors and database reviewers · **Status:** implemented,
> amended — §17 moves storage from `BINARY(12)` to the canonical text form

How a BuildMax server entity is named. Two identifiers, one role each: a
`bigint unsigned` primary key that is the relational key inside MySQL, and a
random opaque `public_id` that is the only handle allowed to leave the process.

This record replaces the *Entity identity and relational keys* proposal, which
has been retired. It keeps that paper's direction, answers its four open
questions with evidence from the current tree, and corrects three places where
it was written against an older schema. Git history holds it.

Related: [data model](../contribute/architecture/data-model.md),
[store](../contribute/architecture/store.md),
[util](../contribute/architecture/util.md),
[conventions](../contribute/conventions.md), and the
[Beta gate](../ROADMAP.md#beta-gate), whose versioned-migration requirement
should start from this schema rather than preserve the Alpha one.

**Current-schema note (2026-09):** The inventories and PR sequence below record
the identity migration as designed at the time; they are not an inventory of
today's tables. In particular, `workflow_node_run` replaced
`workflow_step_run`, and refresh tokens now name an `auth_session.public_id`
instead of an `as_` login-chain ID. Read the row structs and the
[data model](../contribute/architecture/data-model.md) for current columns and
relationships. The public-ID and relational-key decisions remain in force.

## Contents

- [1. Problem](#1-problem)
- [2. Goals And Non-Goals](#2-goals-and-non-goals)
- [3. Decisions](#3-decisions)
- [4. Public ID Format](#4-public-id-format)
- [5. Schema Shape](#5-schema-shape)
- [6. Table-By-Table Decision](#6-table-by-table-decision)
- [7. Which References Stay Opaque](#7-which-references-stay-opaque)
- [8. Database Foreign Keys](#8-database-foreign-keys)
- [9. The Store Boundary](#9-the-store-boundary)
- [10. API Contract](#10-api-contract)
- [11. Non-HTTP Boundaries](#11-non-http-boundaries)
- [12. Alpha Transition](#12-alpha-transition)
- [13. Execution Plan](#13-execution-plan)
- [14. Measured Plans](#14-measured-plans)
- [15. Validation](#15-validation)
- [16. Resolved And Open Questions](#16-resolved-and-open-questions)
- [17. Amendment: The Text Form Is The Stored Form (2026-08)](#17-amendment-the-text-form-is-the-stored-form-2026-08)

## 1. Problem

Most server entities carry two identifiers: an auto-increment `id`, and a
public `varchar(64)` prefixed ID such as `t_9f3k2m8x1qwe7rt4zy0p`. The numeric
key is internal but it is not the relational key — every foreign reference,
join, authorization filter, and quota aggregation uses the public string. The
schema pays for two identifiers and receives the benefit of neither: identity
is duplicated, and relationships are wide, collation-dependent strings.

A full inventory of the current tree confirms the scale. Across the 28 row
structs in `internal/infra/db`, **99 columns are declared `varchar(64)`**. A
handful are values — a tier name, an audit action, a version, a digest, a model
alias — and the rest are identifiers, most of them relational references
repeated and re-indexed on every child row. The tables carrying the most of them are the ones that grow
with activity — `task`, `task_run`, `llm_call`, `conversation_message`,
`workflow_step_run`, `issue_comment`, `audit_event`, `artifact` — not the ones
that grow with administrative configuration.

Three further costs follow:

- MySQL compares those identifiers under the database's `utf8mb4` collation.
  The creator in `database.go` selects `utf8mb4` and no column overrides it, so
  identity comparison is a collation decision rather than a byte comparison.
- The core models carry both `ID uint` (tagged `json:"-"`) and a public field
  such as `TaskID string`. A MySQL implementation detail is visible in
  `internal/core/model`, which every mock and alternative store must reproduce.
- The type prefix is a convention, not a boundary. Nothing in production code
  dispatches or validates on it: the only three `HasPrefix` checks in the tree
  are in tests (`llm_call_test.go`, `routes_test.go`, `service_test.go`).

BuildMax is in Alpha and makes no identifier compatibility commitment. This is
the last cheap moment to decide whether the current format is a contract.

## 2. Goals And Non-Goals

**Goals.** Ordinary relationships use database-native keys. Public identifiers
stay opaque, non-enumerable, and independent of database topology. Each
identifier has exactly one role. No database primary key reaches an API
response, URL, JWT claim, log line, trace, workflow definition, or
object-storage path. One coherent Alpha schema change, with no dual-read path.

**Non-goals.** An identifier is not an authorization credential — every lookup
still enforces space membership and returns the same `404` regardless of
identifier entropy. Ordering never uses a public identifier; durable ordering
stays `created_at` plus the numeric key as tie-breaker. Provider IDs, tool-call
IDs, agent session IDs, trace run IDs, and idempotency keys are owned outside
the entity model and are untouched. Hiding raw IDs behind names in Portal is a
separate presentation change. The repository's no-database-foreign-key rule is
not silently reversed; §8 decides it explicitly.

## 3. Decisions

`internal/core/model` names the package the domain structs lived in while this
was written. It has since been split into one package per domain — `core/task`,
`core/space`, `core/issue`, `core/conversation`, and the rest — per
[conventions](../contribute/conventions.md); every decision below applies to
those packages together.

| # | Decision |
|---|---|
| D1 | `id` is `bigint unsigned auto_increment` and is the relational key. `public_id` is the external handle, present only where one is needed. |
| D2 | A public ID is 96 bits of crypto-random data, rendered as 20 lowercase base32 characters. Amended by §17: the canonical text is also the stored form, `char(20)` ascii_bin, not the raw bytes. |
| D3 | Strict single-type relationships become `uint64` columns. No `FOREIGN KEY` constraints in this change. |
| D4 | The core domain structs carry public identity only. A struct's own handle is `ID string`, serialized as `id`. |
| D5 | 18 tables carry a `public_id`, 8 carry none, 2 keep a natural key. |
| D6 | A reference stays an opaque string only when it is polymorphic, externally owned, or a value. |
| D7 | Public-to-numeric translation happens inside `internal/infra/db` and never produces an N+1 read. |
| D8 | No compatibility layer. Alpha databases and object stores are reset. |
| D9 | One identifier format. A type prefix survives only where a bare identifier reaches a reader in free prose. |

## 4. Public ID Format

### 4.1 The Value

96 bits from `crypto/rand`, stored as its canonical text form in
`char(20) CHARACTER SET ascii COLLATE ascii_bin` (§17; the original decision
stored the raw 12 bytes in `BINARY(12)`).

At one billion values in one table the birthday-bound collision probability is
about `6.3e-12`. The unique index is still the final guard: a create that hits
a duplicate on `uq_<table>_public_id` regenerates and retries within a small
fixed limit, and every other duplicate-key error is returned unchanged. With a
billion live values, a uniform guess names one of them with probability about
`1.3e-20`. That opacity is defense in depth behind space authorization and
indistinguishable not-found responses, never a replacement for them.

The value is not UUIDv7, carries no timestamp, and reveals neither insertion
order nor an approximate resource count. Index locality comes from the
auto-increment key; the random value is one secondary unique index rather than
a clustered key copied into every other index.

Public IDs are unique per table. BuildMax has no global resource resolver, and
a route or JSON field always supplies the type context. A cross-table registry
would add a write bottleneck and buy no authorization or collision benefit.

### 4.2 The Text Form

RFC 4648 base32, no padding, lowercased: alphabet `a-z2-7`, exactly 20
characters, decoding to exactly 12 bytes.

```text
ivyoh5qcfu6ypfkhyedq
```

The proposal specified 16 characters of base64url. Base32 costs four more
characters and removes an entire class of boundary hazards that the current
lowercase-base36 format silently protects against and base64url would not:

| Boundary | Base64url (`A-Za-z0-9-_`) | Base32 (`a-z2-7`) |
|---|---|---|
| Kubernetes Job name — `util.WorkerJobNameForTaskRun` lowercases the run ID and replaces every non-DNS-1123 character with `-` | Case and `-`/`_` collapse. Two distinct runs created in the same second can produce one Job name, because the only suffix is a second-resolution timestamp | Passes through the sanitizer unchanged |
| Local-FS object store on a case-insensitive filesystem (macOS default), which keys artifact content as `spaces/<space>/artifacts/<id>/content` | Two IDs differing only in case silently share one path. The database unique index is on the raw bytes and cannot see the collision | Cannot occur |
| Copy, paste, and dictation of a bare ID | Case-sensitive, and `_` is invisible under an underline | Case-insensitive, one double-click word |

The residual entropy under case folding is what makes the local-FS hazard real
rather than theoretical: folding base64url leaves a 38-symbol alphabet, so a
billion values in one space fold-collide with probability around `2.6e-8` — four
orders of magnitude worse than the raw bound, and the failure mode is a silent
content overwrite rather than a database error.

### 4.3 The Codec

`internal/util/id.go` is two functions and nothing else (§17 folded the
original parse/format byte pair into one, since no caller holds bytes any
more):

```go
func NewPublicID() (string, error)              // 20-char canonical text
func CanonicalPublicID(s string) (string, bool) // any-case text -> canonical text
```

`NewPublicID` returns an error rather than panicking: `crypto/rand` failure
must surface as a failed create, not as a process abort inside a request.

`CanonicalPublicID` accepts input case-insensitively and canonicalizes to
lowercase, so an ID retyped from a title-cased document still resolves. It
rejects everything else, and guarantees exactly one canonical text form per
value by decoding, re-encoding, and requiring the result to match. That check
is what closes base32's trailing-bit ambiguity: 20 characters carry 100 bits,
and the final four must be zero — and, with the text stored, what keeps one
value from occupying sixteen distinct unique-index entries.

One visible consequence of that check: a canonical ID always ends in `a` or
`q`, because those are the only two characters whose low four bits are zero. An
invented fixture that ends in anything else is not a public ID. Take test
values from the generator, not from imagination.

`FormatPublicID` on a slice that is not 12 bytes is a programming error and
returns the empty string, which the store's read path treats as corruption.

### 4.4 Prefixes Are Retired

The prefix constants in `id.go` are deleted along with the prefix table in
[conventions.md](../contribute/conventions.md#entity-ids-are-opaque-public-handles),
and so is `NewPrefixedID` itself. A login chain, a trace file, and a Desktop
project are not database rows, but that was never the argument for a prefix —
it was only the boundary of this change. None of them is read by a person or
dispatched on, so none keeps one.

One does. A background job's ID reaches the model as a bare string inside tool
output — `job: jb_ivyoh5qcfu6ypfkhyedq` beside a command line and a file path —
and free prose is the one place the surrounding context does not already name
the type. That prefix lives in `internal/agentapp/job`, which owns the concept,
rather than in `internal/util`.

## 5. Schema Shape

```sql
CREATE TABLE task (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id       CHAR(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    conversation_id BIGINT UNSIGNED NOT NULL,
    space_id         BIGINT UNSIGNED NOT NULL,
    issue_id        BIGINT UNSIGNED NULL,
    agent_id        BIGINT UNSIGNED NULL,
    last_run_id     BIGINT UNSIGNED NULL,
    created_by      BIGINT UNSIGNED NOT NULL,
    -- status, input, title, output, timings, session_id unchanged
    PRIMARY KEY (id),
    UNIQUE KEY uq_task_public_id (public_id),
    KEY idx_task_space_created (space_id, created_at),
    KEY idx_task_conversation (conversation_id)
);

CREATE TABLE task_run (
    id                   BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    public_id            CHAR(20) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
    task_id              BIGINT UNSIGNED NOT NULL,
    previous_task_run_id BIGINT UNSIGNED NULL,
    retry_of_task_run_id BIGINT UNSIGNED NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_task_run_public_id (public_id),
    KEY idx_task_run_task (task_id)
);
```

The names carry the roles:

| Name | Meaning |
|---|---|
| `id` | This row's internal numeric primary key |
| `public_id` | This row's opaque external handle, only where one is needed |
| `<entity>_id` | Numeric relation to that entity inside the database |
| `<concept>_public_id` | A deliberately stored external or polymorphic handle. Rare, and named explicitly |

Two mechanical rules go with it. Persisted numeric fields are `uint64`, never
architecture-dependent `uint`. And the Go field for a public ID is `string`
with `gorm:"type:char(20) CHARACTER SET ascii COLLATE ascii_bin"` (§17) — the
store writes only what the codec generated or canonicalized, so exactly one
lowercase spelling of each value ever reaches the column, and `ascii_bin`
keeps the comparison memcmp rather than a collation decision.

Numeric conversion is also the moment to make composite indexes right. A
space-scoped list ordered by `created_at` wants `(space_id, created_at)`, not the
single-column `space_id` index the string model left behind.

## 6. Table-By-Table Decision

At the time of this migration plan, 28 tables were in `AutoMigrate`. The
following inventory records those decisions; newer tables and later Workflow
and authentication changes are described in the current data model.

### 6.1 Tables With A `public_id` — 18

Each replaces its current prefixed public column with `public_id BINARY(12)`.

| Table | Numeric relations it gains |
|---|---|
| `user` | none — no entity parent |
| `space` | `personal_for_user_id`, `created_by` |
| `issue` | `space_id`, `user_id`, `parent_issue_id`, `created_by` |
| `issue_comment` | `issue_id`, `source_task_id`, `source_task_run_id` |
| `agent` | `user_id`, `space_id` |
| `conversation` | `user_id`, `space_id`, `created_by` |
| `conversation_message` | `conversation_id` |
| `task` | `conversation_id`, `space_id`, `issue_id`, `agent_id`, `last_run_id`, `created_by` |
| `task_run` | `task_id`, `previous_task_run_id`, `retry_of_task_run_id`, `cancel_requested_by` |
| `workflow` | `space_id`, `created_by` |
| `workflow_run` | `workflow_id`, `issue_id`, `conversation_id`, `created_by` |
| `workflow_step_run` | `workflow_run_id`, `target_agent_id`, `task_id`, `task_run_id` |
| `user_webhook_key` | `user_id` |
| `llm_model` | none — no entity parent |
| `llm_call` | `space_id`, `user_id`, `task_id`, `task_run_id` |
| `audit_event` | `space_id` |
| `system_grant` | `user_id` |
| `artifact` | `space_id` |

`workflow_step_run` keeps a public ID despite having no detail route: issue
outputs persist it as a source correlation handle. `system_grant` keeps one
because the admin API and the operator CLI return the grant record as durable
authority history. `artifact` keeps one and depends on it more than any other
table — `GET /api/artifacts/{artifact_id}` is addressed without a space in the
path and takes the space from the record before authorizing.

### 6.2 Tables With No Public ID — 8

| Table | Identity |
|---|---|
| `space_member` | Join row. `space_id` and `user_id` become numeric |
| `agent_revision` | Drop `agent_revision_id`. Identified by numeric `agent_id` plus `revision`; `created_by` numeric |
| `workflow_revision` | Drop `workflow_revision_id`. Identified by numeric `workflow_id` plus `revision`; `created_by` numeric |
| `task_run_artifact` | Numeric `task_run_id`, still paired with the relative path |
| `login_code` | Numeric `user_id`. The code hash remains the lookup key |
| `user_refresh_token` | Numeric `user_id`. The token hash and the `as_` login-chain session ID keep their authentication formats |
| `plugin` | Drop `plugin_id`. `name` is the manifest-owned slug and every route addresses it — `GET /api/plugins/{plugin_name}` |
| `plugin_release` | Drop `plugin_release_id`. Addressed by `(plugin_name, version)`, which is already the immutability constraint; `plugin_id` becomes numeric and `plugin_name` stays denormalised |

Dropping the plugin public IDs is new relative to the proposal, which predates
those tables. It is safe: Portal uses `plugin_id` and `plugin_release_id` only
as React list keys, which become `name` and `${plugin_name}@${version}`.

Two things follow from a row losing its handle, and both are decisions rather
than consequences to discover later:

- An audit record naming that row names it by its natural key instead. A plugin
  audit event carries the plugin's `name`, and a release event carries
  `name@version`, which is immutable and is what every route already addresses.
- A local record of provenance does the same. `PluginState.CatalogID` in
  `<BUILDMAX_HOME>` records which catalog entry an installed copy came from and
  now holds the plugin's name. The field is then arguably redundant with the
  state's own key; removing it is a separate change, because it also appears in
  a run's origin record.

### 6.3 Natural-Key Tables — 2

| Table | Reason |
|---|---|
| `quota_tier` | `tier_name` is a configuration-owned natural key. `user.quota_tier` and `space.quota_tier` keep referencing it by name |
| `schema_migration` | `id` is the migration's permanent authored name |

## 7. Which References Stay Opaque

A column becomes numeric when it names exactly one server entity type and the
application treats the relationship as part of the live data model. Everything
below fails that test and keeps its string column, renamed to
`<concept>_public_id` where the value is a public handle:

| Column | Why it stays |
|---|---|
| `audit_event.actor_id` / `target_id` | Polymorphic. `actor_type` and `target_type` carry the type; one numeric column cannot address rows in several tables |
| `issue.executor_id` | Polymorphic. `executor_kind` is `agent` or `workflow`. `issue.owner_id` is not here: an owner is always a user row, so it is a resolved numeric reference |
| `issue_comment.author_id` | Polymorphic under `author_kind` |
| `artifact.created_by_id`, `artifact.source_id` | Polymorphic under `created_by_type` and `source_type` |
| `task_run.created_by` | Typed by `created_by_type`, which admits `user`, `webhook`, and `system` |
| `system_grant.granted_by` | The operator granting deployment authority is not necessarily a user row |
| `task.session_id`, `task_run.session_id`, `llm_call.session_id` | Agent session IDs name a file under a run's `BUILDMAX_HOME`, not a row |
| `task_run.trace_path` | Names a trace file |
| `conversation_message.tool_call_id` | A provider's tool-call ID |
| `llm_call.target_id`, `llm_call.alias`, `llm_call.upstream_model` | See below |
| `login_code.code_hash`, `user_refresh_token.token_hash` / `session_id`, `user_webhook_key` secret hash | Formats owned by their authentication contracts |
| Step IDs inside a workflow definition JSON | Authored inside the definition, not rows |

`task.created_by`, `conversation.created_by`, `issue.created_by`,
`workflow.created_by`, `workflow_run.created_by`, both revision tables'
`created_by`, `plugin.created_by`, and `plugin_release.published_by` /
`yanked_by` **do** become numeric: none has an accompanying type column, and
each is unconditionally a user row. `task.created_by` is the task's owner even
for webhook and system runs, which carry a configured user identity.

Historical data alone is not a reason to keep a string. With no foreign keys, a
nullable numeric reference can still record a deleted row's former key.

## 8. Database Foreign Keys

No GORM relations and no `FOREIGN KEY` constraints. Numeric references are
plain indexed columns, and referential integrity stays an application
responsibility.

This was left open pending a deletion-semantics review. That review is below,
and it closes the question rather than deferring it again.

**BuildMax does not delete parents.** The store has five delete methods, and
none of them removes a row anything else references:

| Method | What it removes |
|---|---|
| `RevokeKey` | A webhook credential its owner revoked |
| `DeleteIssueComment` | One comment — the schema's only hard delete of user content |
| `PruneAuditEvents` | Events past the retention window, by age, never by target |
| `DeleteExpiredLoginCodes` | Codes that can no longer be redeemed |
| `DeleteExpiredRefreshTokens` | Tokens past their expiry |

`DeleteAgent` is a rename: it sets `deleted_at` so records that name the agent
still resolve. Everything else that goes away goes away the same way —
`deleted_at` on an artifact, `archived_at` on a plugin, `revoked_at` on a
grant, `disabled_at` on an account. There is no path that deletes a user, a
space, a task, a run, a conversation, an issue, or a workflow.

So `RESTRICT` would guard a hazard that does not exist, and would charge for it
immediately. Every fixture that creates a user would have to tear down seven
tables in dependency order — `deleteTestUser` in the store tests already does
exactly that, by hand, and it is the shape every cleanup path would take. That
is a real cost against a benefit that is currently zero.

The answer changes when a real deletion feature arrives — "delete this space and
everything in it" is the obvious one. That feature has to specify an order
whether or not the database enforces it, and the constraints are worth adding
in the same change, where the order is being written down anyway. Adding them
first would only mean writing the order twice.

Numeric references must not be read as implying `CASCADE` in the meantime.
`TestNoDatabaseForeignKeys` in `internal/architecture` keeps that true: a
bigint column beside a row struct is one relation tag away from a constraint
`AutoMigrate` would emit, and nothing else in the tree would notice.

## 9. The Store Boundary

Public IDs cross process and persistence-system boundaries. Numeric IDs do not.

| Boundary | Representation |
|---|---|
| HTTP request and response | Public ID |
| Portal route and client state | Public ID |
| Access token `sub`; run token `sub`, `tid`, `rid`, `kid` | Public ID |
| Logs, traces, tool output | Public ID |
| Object-storage key | Public ID |
| Workflow definition JSON | Public ID |
| Kubernetes Job name | Derived from the public ID |
| The domain packages under `internal/core` and every repository interface | Public ID |
| Row structs and SQL inside `internal/infra/db` | Numeric ID |

The core packages must not grow a second internal-ID API to save a lookup. That
would push a MySQL implementation detail above `internal/infra/db` and force
every mock and alternative store to reproduce it. Row structs own both
representations; domain models own only the public one.

### 9.1 Resolving

One unexported helper resolves a public ID to a numeric key, and typed wrappers
name the table:

```go
func lookupKey(ctx context.Context, tx *gorm.DB, table, publicID string) (uint64, error)
```

A public ID that fails `ParsePublicID`, and one that names no row, both return
`apierr.ErrNotFound` — the handler layer has already rejected malformed input
with `400`, and a well-formed unknown value must not become an existence
oracle.

### 9.2 Reading Without An N+1

A read rooted at a public resource selects its numeric key once and joins for
every related public ID it must return. Each join is a primary-key lookup.
The task aggregate — the prototype the proposal asked for — is:

```sql
SELECT  t.*,
        c.public_id  AS conversation_public_id,
        tm.public_id AS space_public_id,
        i.public_id  AS issue_public_id,
        a.public_id  AS agent_public_id,
        lr.public_id AS last_run_public_id,
        u.public_id  AS created_by_public_id
FROM        task         t
JOIN        conversation c  ON c.id  = t.conversation_id
JOIN        space         tm ON tm.id = t.space_id
JOIN        user         u  ON u.id  = t.created_by
LEFT JOIN   issue        i  ON i.id  = t.issue_id
LEFT JOIN   agent        a  ON a.id  = t.agent_id
LEFT JOIN   task_run     lr ON lr.id = t.last_run_id
WHERE   tm.public_id = ? AND t.public_id = ?;
```

In Go this is a read struct holding the row struct plus the joined handles, and
one `taskSelect()` builder shared by the detail read, the list read, and the
run-output read, so the join set cannot drift between them.

The row must be a **named** field tagged `gorm:"embedded"`, never an anonymous
one. GORM reads an anonymous embedded struct that carries its own `TableName`
as an association and scans none of its columns — the joined handles arrive and
the row's own fields stay at their zero values, with no error anywhere. A space
read returns a nameless space rather than failing, so nothing catches it until a
test asserts on a field. The list form
is the same statement with `WHERE tm.public_id = ? ORDER BY t.created_at DESC,
t.id DESC LIMIT ?`, which is why `idx_task_space_created` exists.

Writes go the other way: resolve the root once, then take every other key from
the row already read. Creating a task resolves the conversation and reads its
`space_id` from that same row rather than resolving the space a second time.

A locking read never joins. `SELECT ... FOR UPDATE` over a join locks the
joined rows too, so resolving a refresh token's owner inside its locking read
would hold a lock on the account for the length of the rotation and serialize
every session that account has open. Where a locked row must report a handle,
the handle is read separately by key.

### 9.3 Keyset Cursors

A keyset cursor is the one place a caller legitimately needs the tie-break that
the row key provides. The audit export walks in pages ordered by `created_at`
plus the row key, because `created_at` has one-second resolution and several
events routinely share a second.

`model.AuditCursor` therefore names the last event of the previous page by its
**public** handle, and the store resolves that handle to the row key inside the
keyset predicate:

```sql
WHERE created_at < ?
   OR (created_at = ? AND id < (SELECT id FROM audit_event WHERE public_id = ?))
```

One indexed lookup per page, not per row, and no round trip: the alternative --
handing the caller the row key so the next request can send it back -- is
exactly the leak this design exists to prevent. Any future cursor follows the
same shape.

### 9.4 Aggregation

`SpaceUsageInWindow` currently joins `task ON task.task_id = task_run.task_id
AND task.space_id = ?` — two string comparisons per row. After the space's public
ID is resolved once at the top, both become numeric and the query never touches
a string again. This is the query whose `EXPLAIN` output is required evidence
in §12.

### 9.5 Collision Retry

`isDuplicateKey` in `llm_call.go` does not say which index was violated, so it
cannot distinguish a public-ID collision (regenerate and retry) from a real
uniqueness conflict such as a duplicate email or an idempotency key (return
unchanged). It gains an index-aware sibling that reads the index name out of
MySQL's `1062` message, and creates retry at most three times on
`uq_<table>_public_id` alone.

Generation moves inside `internal/infra/db` for every entity. Today
`internal/service/artifact/service.go` mints the artifact ID above the store,
which leaves the retry loop with no way to reach the generator.

## 10. API Contract

The HTTP resource topology does not change. Every path in
[routes.go](../../internal/server/handlers/routes.go) and the `space`, `work`,
`artifact`, `admin`, and `worker` handler packages keeps its shape and its
semantic parameter names; only the parameter's value format changes.

```text
GET  /api/spaces/{space_id}/tasks/{task_id}
GET  /api/spaces/{space_id}/task-runs/{task_run_id}/trace
GET  /api/artifacts/{artifact_id}
POST /api/worker/task-runs/{task_run_id}/llm/completions
```

`public_id` is a database name and never appears in JSON. A resource represents
its own handle as `id`; relationships keep semantic names:

```json
{
  "id": "ivyoh5qcfu6ypfkhyedq",
  "space_id": "lzmomgl6mzg2bve3bgka",
  "conversation_id": "2l73hqqx6fcl7eecggda",
  "status": "SUCCEEDED"
}
```

This renames the self-handle field on every entity response —
`task_id` → `id`, `task_run_id` → `id`, and so on — across `internal/core/model`
JSON tags, `internal/server/websocket/protocol.go`,
`internal/infra/workerclient/api_types.go`, Portal's `lib/api/types.ts` and
`lib/api/mappers.ts`, and Desktop. Relationship fields such as `space_id` and
`conversation_id` are unaffected. Create responses that return a related handle
under a semantic name keep it; this change does not require unrelated
response-shape churn.

Agent and workflow revision responses lose their `id`: a revision is identified
by its parent plus its revision number, and `POST .../revisions/{revision}/restore`
already addresses it that way. Revision path parameters stay integers.

One shared codec validates IDs from path parameters, query parameters, and JSON
bodies:

| Input | Result |
|---|---|
| Not canonical text | `400 Bad Request` |
| Canonical, names no row | `404 Not Found` |
| Canonical, names a row outside the caller's space | `404 Not Found` |

A space-scoped lookup resolves the space's public ID first, then constrains the
resource by both its public ID and the numeric space key, so a valid public ID
cannot become a cross-space existence oracle.

Access-token `sub` and run-token `sub`, `tid`, `rid`, `kid` continue to carry
public IDs, in the new format. Worker routes still compare the canonical `rid`
with the route's `task_run_id`. `/api/webhook` still authenticates with the
webhook secret, never with the key's public ID.

The cutover invalidates every access token, refresh session, run token,
Portal link, and bookmark. That is the intended cost of having no dual-read
path, and it is affordable exactly once, in Alpha.

## 11. Non-HTTP Boundaries

| Surface | Change |
|---|---|
| Object storage | `PersistObjectKey`, `RunOutputResultKey`, `RunOutputFileKey`, `TaskBuildmaxObjectKey`, `RunGlobalObjectKey`, `RunArtifactsObjectKey`, and `ArtifactObjectKey` keep their shapes and their public-ID segments. Existing objects become unreachable; §12 resets the store |
| Kubernetes | `util.WorkerJobNameForTaskRun` keeps its DNS-1123 sanitizer, which a base32 ID passes through unchanged. A test asserts that identity, so a future format change cannot silently reintroduce collapsing |
| Traces | Run traces already carry `rt_` file IDs and public entity IDs; only the format of the latter changes |
| WebSocket | `internal/server/websocket/protocol.go` follows the §10 field rename |
| Seed and sample data | `sample-data/` and any fixture holding a prefixed ID is regenerated |

## 12. Alpha Transition

No compatibility layer, no dual read, no mixed formats.

Alpha databases and object stores are reset. There is no deployment holding
data anyone needs: the local reference environment is rebuilt from scratch with
`./make kind down && ./make kind up`, and `./make e2e local` owns its Compose
stack for one run. So the cutover carries no compatibility burden at all.

That is what makes the reset the right instrument rather than a migration.
`AutoMigrate` cannot express this change — it is a rename, a retype, and a
backfill of every relation column at once — and writing it as
`schema_migration` entries would encode a data shape no deployment is required
to preserve, then keep that code alive forever for the benefit of nobody.

The `migrations` list in `internal/infra/db/migration.go` is append-only and
its three existing entries (`0001`–`0003`) describe a schema that ceases to
exist. They are deleted with the reset: keeping them would leave migrations
that cannot run against any database this binary can create.

## 13. Execution Plan

Six stacked changes, all landed. Each compiles, passes `./make test`, and is
reviewable on its own. Only PR 3 onwards required a database reset.

### PR 1 — The codec

`internal/util/id.go` gains `NewPublicID`, `ParsePublicID`, `FormatPublicID`
and keeps `NewPrefixedID`. Nothing else changes; no caller uses the new
functions yet.

- Tests: alphabet, exact 20-character length, canonical round-trip, rejection
  of non-canonical trailing bits, case-insensitive input, wrong length, empty
  string, non-alphabet characters, and a `crypto/rand` failure surfacing as an
  error rather than a panic.
- A test asserting `WorkerJobNameForTaskRun` leaves a base32 ID unchanged.
- Checks: `./make test ./internal/util`, `./make check go`.

### PR 2 — Model identity shape

Pure rename, no format change and no schema change. `internal/core/model` drops
every `ID uint`, renames each entity's self-handle field to `ID string`, and
tags it `json:"id"`. Revision structs lose their public ID field.

Ripples: `internal/infra/db` mappers (row structs unchanged — the existing
`task_id` column now maps to `task.Task.ID`), `internal/mock`,
`internal/service`, `internal/server/handlers`, `websocket/protocol.go`,
`workerclient/api_types.go`, Portal `lib/api/types.ts`,
`lib/api/mappers.ts` and its 172 `*_id` references, and Desktop.

- This is the largest diff of the six and the only one that changes the API's
  response shape. Doing it before the storage work keeps shape churn and
  storage churn in separate reviews.
- Checks: `./make check go`, `./make check portal`, `./make check desktop`,
  `./make e2e cli`, `./make e2e desktop`.
- Changelog: `./make changelog new changed api-entity-id-field`.

### PR 3 — Format switch

The store generates `NewPublicID()` instead of `NewPrefixedID(prefix)`. Columns
stay `varchar(64)` and joins stay string joins; only the value format changes.
Entity prefix constants are deleted from `id.go`, leaving `rt_`, `as_`, `p_`.
The three `HasPrefix` assertions in tests become codec assertions.

- Requires a database reset. First PR that does.
- Verifies the format end to end — routes, tokens, object keys, Job names,
  Portal — before any relational change is layered on top.
- Carries the *prescriptive* half of the documentation: `conventions.md`,
  `AGENTS.md`, and `util.md` tell a contributor what to do, and leaving them
  saying `NewPrefixedID` for the span of PRs 4 and 5 would mislead whoever adds
  a table in between. The descriptive half stays in PR 6.
- Deletes migrations `0001`–`0003` per §12, and with them the append-only guard
  that named them: the reset is what makes those IDs stop being permanent facts
  about a database that exists.
- Checks: `./make check go`, `./make e2e local`.
- Changelog: `./make changelog new changed opaque-entity-ids`.

### PR 4 — Relational keys

The core change, confined almost entirely to `internal/infra/db` because PR 2
already removed numeric IDs from the models and PR 3 already settled the
format. Mocks are unaffected: they implement interfaces that speak public IDs.

Four sub-PRs, in dependency order. A reference converts when its **target**
table does, not when the table holding it does — so `task.issue_id` and
`task.agent_id` stay handles through 4b and become numeric in 4c, and 4c edits
`task` even though `task` is a 4b table. Grouping by target is what keeps each
sub-PR's join set stable while it lands.

| | Tables |
|---|---|
| 4a | `user`, `space`, `space_member`, `login_code`, `user_refresh_token`, `user_webhook_key`, `system_grant` |
| 4b | `conversation`, `conversation_message`, `task`, `task_run`, `task_run_artifact` |
| 4c | `issue`, `issue_comment`, `agent`, `agent_revision`, `workflow`, `workflow_revision`, `workflow_run`, `workflow_step_run` |
| 4d | `llm_model`, `llm_call`, `audit_event`, `artifact`, `plugin`, `plugin_release` |

Each sub-PR carries, for its tables: `uint64` keys, `public_id BINARY(12)` with
`uq_<table>_public_id`, numeric relation columns, the composite indexes of §5,
read structs and shared select builders per §9.2, `lookupKey` resolution,
index-aware duplicate detection with bounded retry, and artifact ID generation
moved down from `internal/service/artifact`.

- 4b lands the quota aggregation rewrite of §9.4.
- Checks per sub-PR: `./make test ./internal/infra/db`, `./make test race`,
  `./make check go`.

### PR 5 — Enforcement and evidence

- Architecture tests under `internal/architecture`: no row struct declares a
  `varchar` column for a strict entity relationship; every `public_id` is
  `binary(12)`; no core domain struct has a numeric ID field.
- Boundary tests: no numeric ID appears in an API response body, a JWT claim,
  an object key, a trace record, or a WebSocket frame.
- Authorization tests: cross-space lookups still return `404`, and a valid
  foreign public ID is indistinguishable from an unknown one.
- `EXPLAIN` output for quota aggregation, the space task list, the conversation
  message list, and the run-output read, captured before and after, recorded in
  the PR body — this is the measurement the proposal listed as required
  evidence and this design does not pre-judge.
- Checks: `./make check ci`.

### PR 6 — Documentation

- [data-model.md](../contribute/architecture/data-model.md): rewrite the
  identifier conventions, correct the table count from 22 to 28, and update
  every per-table column listing.
- [store.md](../contribute/architecture/store.md): replace the prefix list with
  the two-identifier rule and the translation boundary.
- `conventions.md`, `AGENTS.md`, and `util.md` were done in PR 3, because they
  prescribe rather than describe.
- [deploy/local-kind.md](../deploy/local-kind.md) and
  [contribute/testing.md](../contribute/testing.md): say that crossing this
  cutover needs `./make kind down && ./make kind up` rather than an upgrade.
- Note the schema baseline in [ROADMAP.md](../ROADMAP.md), so the Beta
  versioned-migration gate starts here.

The design index row and the proposal's retirement landed with this record and
are not part of PR 6.

### Follow-up — one format

The six above left `NewPrefixedID` alive for four identifiers that are not
database rows. Three of them — a login chain, a trace file, a Desktop project —
kept a prefix for no reason beyond the scope line this design drew, which is
not an argument. They became ordinary public IDs, and `NewPrefixedID` went with
them, taking the base36 encoder and a dependency.

The fourth stayed, and moved. See §4.4.

## 14. Measured Plans

The five queries the plan named, read back from MySQL 8 after the conversion.
Every one resolves a handle at most once, through its unique index, and joins
on row keys after that.

| Query | Access path |
|---|---|
| Quota: run count | `task` on `idx_task_space_created` (`ref`, covering), then `task_run` on `idx_task_run_task_created` (`ref`, covering) |
| Quota: title tokens | `task` on `idx_task_space_created` (`range`, index condition) |
| Space task list | `task` on `idx_task_space_created` as a **backward index scan** — no sort — then all six joins `eq_ref` on `PRIMARY` |
| Conversation messages | `conversation` on `uq_conversation_public_id` (`const`, covering), then `conversation_message` on `idx_conversation_message_conversation` (`ref`) |
| Run output read | `conversation` on `uq_conversation_public_id` (`const`), then `user`, `task`, `task_run`, `task_run_artifact` each on a key |

Three things in that table are the conversion paying for itself. The task list
sorts by reading its composite index backwards rather than sorting rows, which
the single-column `space_id` index the string model left behind could not do.
Both quota queries are answered from indexes alone — `Using index`, no row
reads — because a `bigint` space reference fits in one. And every join is
`eq_ref` on a primary key, which is what makes a listing one query rather than
one query per row.

The handle appears exactly once per query, as a `const` lookup on the unique
index over `BINARY(12)`. That is the translation boundary in an execution plan:
one indexed lookup at the root, numeric everywhere after it.

## 15. Validation

The work is complete when all of the following hold:

- A fresh database contains no `varchar` relation column for a strict entity
  relationship, and no legacy prefixed public ID anywhere.
- Architecture tests reject both, so a regression fails in CI rather than in
  review.
- Repository authorization tests still prevent cross-space lookups, and a
  well-formed foreign public ID is indistinguishable from an unknown one.
- Quota, artifact, workflow, issue-hierarchy, revision, and audit queries join
  numerically wherever the relationship is strict.
- API, Portal, worker, trace, and object-store tests prove no numeric ID
  crosses its boundary.
- Codec tests cover randomness failure, duplicate retry, alphabet, exact
  length, canonical parsing, and malformed input.
- `EXPLAIN` plans for quota aggregation and the conversation and run-output
  reads use the intended numeric indexes.
- `./make check ci` and `./make e2e local` pass.

## 16. Resolved And Open Questions

The retired proposal left four questions. All four are answered.

| Question | Answer |
|---|---|
| Normalize creator fields, or keep them typed and opaque? | Split by whether a type column exists. `task_run.created_by`, `issue_comment.author_id`, `audit_event.actor_id`/`target_id`, `artifact.created_by_id`, and `issue.executor_id` are polymorphic and stay opaque. The nine unconditional-user creator columns of §7 become numeric, and `issue.owner_id` later joins them once the owner/executor split gives it a single-table meaning. Normalizing the actor model is separate work |
| Is `llm_call.target_id` a live relationship or a snapshot? | A snapshot, and it stays a string. `Target.ID` in `internal/service/llmgateway/catalog.go` is a catalog identifier whose namespace may be owned by configuration rather than by the `llm_model` table — `store_catalog.go` is one catalog implementation, not the only one. A numeric `llm_model` reference would be wrong for a config-backed target |
| Include `RESTRICT` constraints? | No, and §8 now says why rather than deferring: the store deletes no parent rows, so constraints would guard a hazard that does not exist and charge every fixture for it. The answer changes when a real deletion feature specifies an order |
| Is an Alpha reset acceptable for every deployment? | Yes. There is no formal deployment, and the local reference environment is regenerated with `./make kind up`. No export/import bridge is written, and none should be added later on the strength of this design |

One question this design adds: nothing currently records which durable
identifier format a database was created with. A `schema_migration` row
asserting the identity baseline would let a future binary refuse a
pre-cutover database instead of failing at the first query. It is cheap, and
it is deliberately out of scope here.

## 17. Amendment: The Text Form Is The Stored Form (2026-08)

D2 originally stored the raw 12 bytes in `BINARY(12)` and produced the text
form only in Go. The storage clause is reversed: `public_id` is
`char(20) CHARACTER SET ascii COLLATE ascii_bin`, holding exactly the 20
lowercase characters every other boundary already shows. Sections that narrate
the original conversion (§5's original DDL, §6.1, §13, §14) remain the record
of that work as executed; where they say `BINARY(12)`, this amendment applies.

The reversal is an operational argument, and it is the same argument that
picked base32 over base64url in §4.2: the format follows the ID to every place
it is read. The original decision weighed storage width and never priced
readability. In practice the database is one of those places people read IDs —
`SELECT` in a MySQL client during every debugging and support session — and
`BINARY(12)` rendered every handle as an unreadable blob there. The
alternatives tried first — hand-installed stored functions doing base32 in
SQL, `HEX()` wrappers per query — put a translation step in front of every
direct query forever. Storing the text removes the translation boundary's
storage half entirely; `lookupKey` still canonicalizes input, but nothing
converts on the way out.

What the original decision bought, and what of it is kept:

- **Width.** 8 bytes per value, on the column and its unique index. At this
  project's row counts that is noise; it was never measured as a constraint.
- **memcmp identity.** Kept, by `ascii_bin`: comparison is byte equality, no
  case folding, no collation expansion. The §1 objection to string identity
  was aimed at `utf8mb4` general collations, not at a fixed-width ascii_bin
  column carrying a closed alphabet.
- **One spelling per value.** Kept, and it now matters more: the codec's
  canonical form (lowercase, zero slack bits) is enforced before every write
  and lookup, so the unique index sees exactly one spelling — where a lax
  store would let sixteen spellings of one value coexist as different rows.

The codec consequence is in §4.3: with no caller holding bytes,
`ParsePublicID`/`FormatPublicID` collapsed into `CanonicalPublicID`. The
architecture test that enforced `binary(12)` now enforces
`char(20) CHARACTER SET ascii COLLATE ascii_bin` and a `string` field.

Existing databases are not migrated, in line with D8 and §16's reset answer:
there is still no formal deployment, and the local environments are
regenerated (`./make kind down && ./make kind up`, or dropping the Compose
volume). A pre-amendment database fails at the first `public_id` read;
recreate it.
