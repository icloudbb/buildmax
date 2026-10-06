# Code And Commit Conventions

> **简体中文：** [阅读中文镜像](../zh-CN/contribute/conventions.md)
> **Audience:** contributors · **Status:** current

Project-wide rules that are not obvious from reading the code, and that review
will hold a pull request to. Layering rules live in
[repo-layout.md](repo-layout.md#dependency-direction) and are enforced by tests
in `internal/architecture`; documentation rules live in
[documentation.md](documentation.md).

## Packages Own Capabilities

> Packages are organized around business capabilities.
> Files are organized for readability.
> Functions are organized around a single responsibility.

A package is a semantic ownership boundary, not a folder that keeps files
short. Each one owns a responsibility describable as a business capability or
one precise infrastructure concern — `task`, `workflow`, `identity`,
`pluginarchive` — never a repository-wide technical grouping such as `models`,
`services`, `repositories`, or `interfaces`.

Finding an ownership problem is not permission to fix it here. Do not
restructure packages as an incidental part of a feature unless the feature is
wrong without it. Record the finding and propose the migration on its own.

A package's size is never an argument, in either direction. The only test is
whether it has a reason to change that its neighbours do not. `core/apierr` is
under 200 lines and earns its boundary, because what an error means to the
whole application changes for reasons nothing else shares. Split a file when
navigation improves; split a package only when a reason to change diverges.

Do not name a package for what it contains rather than what it owns. `common`,
`shared`, `util`, `helpers`, `base`, `misc`, and `model`/`models` name a
container, so nobody can tell what belongs inside and everything drifts there.
Cross-cutting infrastructure is allowed, but it takes a precise name and a
narrow responsibility.

`internal/core` is a dependency-layer prefix, not a package name. The packages
beneath it each carry a capability: `core/agent`, `core/llm`, `core/session`.
`internal/util` is the shape this rule forbids — a container name holding
several unrelated capabilities. It predates the rule and is known debt, not a
pattern to copy or add to. `internal/core/model` was the other example and has
since been split into one package per domain: `core/task`, `core/space`,
`core/issue`, `core/conversation`, and the rest.

Never resolve an import cycle by moving unrelated code into a general package.
A cycle is evidence that ownership or dependency direction is wrong. Move the
smallest shared concept to its true owner, or pass an ID or an input contract
instead.

## Every Business Rule Has One Owner

A state transition, a validation rule, an authorization decision, a lifecycle
constraint, retry eligibility, a defaulting rule, and an error's meaning each
have exactly one authoritative implementation. HTTP handlers, CLI commands,
workers, schedulers, and stores delegate to it; they never restate it. Task-run
status is the shape to copy: `core/task` owns the legal transitions and
`infra/db.TransitionTaskRun` only applies one atomically.

Before adding a rule of any of those kinds, search the whole repository for one
with the same semantic responsibility, identify the package that owns the
concept today, and extend that owner when ownership is clear. Write a second
implementation only when it is a genuinely different concept.

The search has to be repository-wide, because a rule reimplemented in a handler
is invisible from inside that handler's package. This applies to business
rules. A helper with no business meaning does not need it.

## Duplication Is Classified Before It Is Removed

Name what you found before deciding anything. There are three kinds:

- **Textual** — similar syntax, unrelated meaning. Usually leave it.
- **Structural** — parallel types, interfaces, validators, mappers, or errors.
  Sometimes a boundary translation, sometimes an accident.
- **Knowledge** — the same business fact implemented in more than one place.
  This is the one that matters, and it normally gets one owner.

Then say which relationship the two sites are in: shared knowledge, legitimate
boundary translation, intentional local duplication, or coincidental
similarity. Share code only when both sites are the same concept, have the same
owner, and change for the same reason.

Prefer small, explicit local duplication over a shared abstraction that
misleads. Two things that merely look alike get forced apart later, and by then
the seam is in the wrong place.

These pairs already look alike and are already separate on purpose. Each has a
record that says why; none of them is cleanup waiting to happen.

| Looks like | Is not | Why, in |
|---|---|---|
| Local Job | durable Task and TaskRun | [design/local-background-jobs.md](../design/local-background-jobs.md) |
| Local Session | Portal Conversation | [design/surface-positioning.md](../design/surface-positioning.md) |
| Configured model | catalog record, resolved target | [design/llm-gateway.md](../design/llm-gateway.md) |
| Artifact | run output, space home file | [design/unified-artifacts.md](../design/unified-artifacts.md) |
| Domain entity | `db` row, wire DTO | [design/entity-identity.md](../design/entity-identity.md) |
| Task-run status | workflow-run and Issue status | three packages, three vocabularies |
| User session | worker run token | [design/worker-run-token.md](../design/worker-run-token.md) |
| Plugin package storage | space Artifact storage | [design/plugin-marketplace.md](../design/plugin-marketplace.md) |
| Gateway protocol errors | `core/apierr` refusals | [design/llm-gateway.md](../design/llm-gateway.md) |

A small package is not on this list because it is small. `llmwire`,
`pluginwire`, `authtoken`, `plugininspect`, and the handler trust boundaries are
small because their reasons to change are narrow, which is the test the rest of
this section applies.

An interface exists where substitution is required, and it belongs near its
consumer. Do not mirror every concrete implementation with an interface, and do
not merge two consumer-owned interfaces because their methods match:
`service/task` and `service/llmgateway` each declaring a quota checker is
correct, because admitting a task and admitting a model call may diverge.

Boundary models are worth their mapping cost when they protect a real
transport, domain, persistence, or external-API boundary. A domain entity, a
`db` row, and a wire DTO are three different things and stay that way. A chain
of request, input, params, command, payload, and domain types copying identical
fields without enforcing anything is not a boundary.

Do not add base services, generic repositories, universal mappers, or helper
frameworks to reduce line count. Fewer lines is not the goal.

## Ownership Changes Move Every Caller At Once

When a change establishes or corrects a canonical owner:

1. Find every implementation and call site.
2. Decide the canonical owner.
3. Classify the duplication, as above.
4. Put tests around the behavior that must not change.
5. Move the concept **and every caller in the same change**.
6. Delete the superseded implementation in that same change.
7. Run `./make fmt`, `./make lint`, `./make test`, and the relevant
   `./make check` scope.

Step 5 is not negotiable. This project is Alpha and owes no compatibility
window, so there is no reason to run two definitions of one fact side by side,
and `TestNoInternalTypeAliases` already refuses the usual shim. Incrementality
belongs *between* changes — one capability per pull request — never inside one.

When reporting architectural work, say which package now owns each affected
concept, what duplicated knowledge was removed, what similar code was
deliberately kept apart and why, and how the behavior was verified.

The goal is not the most packages or the fewest repeated lines. It is a
repository where the owner of a concept is easy to find and each business rule
has one authoritative implementation. Apart from the architecture tests named
above, review holds these rules; each one is a judgment, so argue it in the
pull request rather than applying it mechanically.

## Persisted Data Uses snake_case

Everything this project writes to disk — session files, config, any JSON — uses
the same naming style.

- **snake_case** for JSON object keys: `created_at`, `tool_call_id`,
  `tool_calls`.
- Give every struct that is serialized to disk explicit `json:"snake_case"`
  tags. Do not rely on Go's default, which is the Go field name.

## Instants Are `time.Time`, `DATETIME(6)`, RFC 3339

A persisted moment in time is a `time.Time` in Go, a `DATETIME(6)` column in
MySQL, and an RFC 3339 string on the wire. Optional means `*time.Time` and a
`NULL` column; absence is never a sentinel zero. Write with
`time.Now().UTC()`.

A duration, a quota, a retry count, and a token count are not instants: they
stay `int64` / `bigint` / a JSON number, and the field name carries the unit —
`TimeoutSeconds`, `duration_ms`. A value with no meaningful time of day is a
`DATE` and `"2026-08-23"`.

The reasoning, and what the rule replaced, are in
[../design/timestamp-representation.md](../design/timestamp-representation.md).

## Database Tables Are Singular

One table per entity type, named in the singular: `user`, `agent`,
`conversation`, `task`, `task_run`. Never `users` or `tasks`. This applies to
every table the project creates or migrates.

## Entity IDs Are Opaque Public Handles

A server entity has two identifiers with one role each. `id` is a
`bigint unsigned` primary key, is the relational key inside MySQL, and never
leaves `internal/infra/db`. `public_id` is the handle every boundary sees: 96
bits of crypto-random data, written as 20 lowercase base32 characters and
stored in that same text form (`char(20) ascii_bin`), so a direct database
query reads the handle every other boundary shows.

```text
ivyoh5qcfu6ypfkhyedq
```

Generate one with `util.NewPublicID`, which returns an error rather than
panicking, and validate one with `util.CanonicalPublicID`, which accepts either
case and rejects any non-canonical spelling. Type prefixes are gone: the route, the
JSON field, and the column already name the type, and nothing dispatched on the
prefix.

Not every row earns a handle. A join row, a revision, and a catalog release are
addressed by their parent plus a natural key instead. Which tables have one,
which references become numeric, and which stay opaque strings are decided in
[../design/entity-identity.md](../design/entity-identity.md); read it before
adding a table.

In JSON, a resource names its own handle `id` and keeps semantic names for
relationships — `{"id": ..., "space_id": ..., "conversation_id": ...}`. Order
rows by `created_at` with the row key as tie-breaker, never by a public handle:
microsecond timestamps narrow collisions but do not remove them, and a page
boundary that compares only a timestamp can still skip or repeat a row.

There is one identifier format. A login chain, a trace file, and a Desktop
project use it unprefixed, the same as any entity. The single exception is a
local background job, which reads as `jb_<public id>`: its ID reaches the model
as a bare string inside tool output, and free prose is the one place a type
prefix says something the surrounding context does not. Agent session IDs are
UUIDs, because they name a file rather than anything this codec identifies.

## Tool Output Is Written For The LLM

A tool's return value goes back to the model as a tool-role message. It is not a
log line and not a user-facing string.

- **Succeed loudly.** Say what was done or what was found, concisely enough to
  be worth its tokens.
- **Fail usefully.** `path outside allowed root` and `file not found` let the
  model decide whether to retry, adjust, or tell the user. `error` alone does
  not. The agent prefixes tool errors with `error:` on the way to the model.

## Logs Are Written For Triage

One rule decides the level, so a threshold selects something meaningful:

| Level | Means |
|---|---|
| `Error` | A unit of user work failed or was lost |
| `Warn` | Degraded, but the work finished — including a fail-open path that ignored something the user configured |
| `Info` | Lifecycle and state transitions |
| `Debug` | Per-item detail |

Warn outnumbering Error is expected here rather than a smell: hooks, traces, the
sandbox, and skill loading all fail open by design, and each of those is a
genuine "degraded but finished".

Identity goes in attributes, never in the message text. A subsystem sets
`component` once — `slog.With("component", "scheduler")` — and the message
describes only the event. Build that logger where it is used rather than in a
package variable, which would capture `slog.Default()` before `infra/log`
installs the real handler at startup.

Attribute keys are `snake_case`, and an error is always `"err"`.

Correlation travels on the context. `infra/log.With(ctx, ...)` adds attrs that
the handler puts on every record logged with that context, so call sites use the
stdlib `slog.*Context` functions and import nothing. That indirection is what
lets `internal/core` carry a run's identifiers without importing infra. The
server puts `request_id` on every request and returns it in `X-Request-Id`; a
worker puts `component` and `task_run_id` on the default logger, because it runs
exactly one task run.

Never log a credential, and never log a provider's raw error body — it can carry
account identifiers and request fragments.

## Colors Come From Theme Tokens

`gui/src/theme.css` is the only stylesheet in `gui`, `portal`, and
`desktop/frontend` that holds a color literal. Every other rule takes its color,
shadow, and overlay from a semantic token there, such as `--color-danger`,
`--color-warning`, `--shadow-popover`, or `--color-overlay`, so the dark theme
follows without a per-page override. A tint is `color-mix()` over a token, not a
new literal. When no existing token means what a rule needs, add one to
`theme.css` for both themes. Do not add a synonym for a token that already
exists.

A `var()` must name a custom property that some stylesheet defines. An
undefined one, with no fallback, makes the whole declaration silently do
nothing; with a fallback, it hides a literal.

Stylelint enforces both rules in each package's `npm run lint`, which
`./make check gui|portal|desktop` runs. The shared configuration and the
undefined-property rule live in `gui/stylelint/tokens.mjs`. The xterm palette
in `desktop/frontend/src/lib/terminalTheme.js` is JavaScript handed to xterm,
and is the one named exception.

## Commit Messages And Pull Requests

The repository's public record carries project content: the change, rationale,
verification, and the people accountable for substantive collaboration.
Repeating assistant, model, or tool attribution is noise, and a trailer cannot
provide the structured provenance needed to reproduce or audit an Agent run.

- Commit subjects are a **single imperative line** — `Move the Dockerfiles into
  deployment/docker`, not `moved dockerfiles` or `fix stuff`. Add a body when
  the reason is not obvious from the diff.
- **Pull requests are merged with a merge commit, so every commit on the branch
  lands on `main`.** Each subject follows the rule above, not just the pull
  request title: `Fix the worker artifact path` — not `Bug fix`, not `WIP`, not
  an issue number alone. Organize the branch into the commits you want in the
  history, and rewrite it before merge rather than appending `fix review
  comments` — that commit is permanent.
- **The pull request title becomes the merge commit subject.** It is what the
  history shows for the change as a whole, so write it as one imperative line
  that reads without the branch under it.
- **The title carries a [Conventional Commits](https://www.conventionalcommits.org/)
  type prefix**, so `main`'s merge commits classify themselves at a glance:
  `feat: Add the black-box worker trial adapter`, `docs: Document AI-first
  contribution practices`, `fix: Derive the Compose stack's cors_origin from its
  Portal port`. The types are `feat`, `fix`, `docs`, `refactor`, `perf`, `test`,
  `build`, `ci`, `chore`, and `revert`. Add a scope when it narrows the title
  usefully — `feat(portal): …` — and a `!` before the colon when the change
  breaks existing public behavior — `refactor(server)!: …`.
- **The prefix is on the title only.** Commit subjects on the branch stay bare
  imperative lines, and nothing parses the prefix: changelog entries are
  hand-written files under `docs/changelog/`, and the release version is chosen,
  not derived. The prefix is a label for readers of `git log --oneline`.
- Keep the branch to **one coherent change**. The merge commit is what a revert
  targets, so a pull request that does three unrelated things cannot have one of
  them backed out later. Two changes, two pull requests.
- `Co-authored-by` may credit a person who contributed substantively to the
  change. Do **not** use it for an assistant, model, provider, or tool.
- Do **not** add `Claude-Session` or other tooling trailers to commits.
- Do **not** add a "Generated with …" footer or an assistant session link to a
  pull request description. When review benefits from task-specific AI context,
  describe that context in ordinary pull request prose.
- Follow [`.github/pull_request_template.md`](../../.github/pull_request_template.md)
  for the shape of a pull request description.
- Add a changelog entry for anything a user or operator would notice: new or
  changed behavior, new configuration, removals, and fixes to released behavior.
  Internal refactors, test-only changes, and documentation edits do not need one.
- **An entry is a new file**, `docs/changelog/<category>/<slug>.md`, holding the
  one Markdown list item it will become. Branches that each add a file never
  conflict; branches that each added a line to `## [Unreleased]` in
  `CHANGELOG.md` conflicted every time. `./make changelog` previews the section
  they will fold into; the full rules are in
  [`docs/changelog/README.md`](../changelog/README.md).

## Related

- [CONTRIBUTING.md](../../CONTRIBUTING.md) — prerequisites, build and test, pull requests
- [repo-layout.md](repo-layout.md) — the tree and the dependency direction
- [documentation.md](documentation.md) — what to update when behavior changes
