# BuildMax Agent Guide

This file is loaded into every Agent session in this repository. It contains
the product principles, non-negotiable boundaries, and working rules that must
always be in context. Follow its links for subsystem detail instead of treating
this file as a duplicate of the documentation.

## Product And Design Principles

BuildMax is a general-purpose AI Agent runtime. It should be quick to run,
portable, configurable across models and tools, and suitable for local or
private deployment.

The project is in Alpha, as [`README.md`](README.md) says. Nothing is released
that has to stay compatible: no frozen API contract, persisted data worth
preserving, or deployment owed a migration path. Design for the stable system,
not around today's accidental shape. When a stored shape, API, or command is
wrong, fix it coherently across code, documentation, and tests instead of
adding a compatibility layer.

Current code plus [`docs/ROADMAP.md`](docs/ROADMAP.md) wins when an older design
record disagrees. Documentation describes the project; it does not bound it.
When documentation disagrees with code, verify the behavior and fix the
documentation. When it is silent, use judgment and state the assumption.

### First Principles Thinking

Begin every feature or system design by stating the essential user outcome,
the evidence that it matters, and the constraints that exist today. Do not
begin with an existing schema, API, old design, or familiar pattern and make
the problem fit it. Reason through the whole lifecycle — ownership, state,
interfaces, authorization, failure, and operation — and derive the design from
those facts. Precedent is evidence about trade-offs, not authority: recover its
rationale and keep it only when that rationale still holds against current
user needs and external conditions.

### Occam's Razor

Entities should not be multiplied beyond necessity. Among designs that fully
satisfy the demonstrated user outcome and current constraints, choose the one
with the fewest independent concepts and the least state. For every field,
abstraction, entity, or feature, name the concrete requirement that fails
without it; if none does, leave it out. Do not build for hypothetical future
requirements. Simplicity is measured across the whole system and lifecycle,
not by the size of the immediate patch: removing a wrong concept everywhere is
often simpler than preserving it behind another layer.

Apply both principles within the architecture boundaries and runtime invariants
below. If the right design requires changing one, propose that change
explicitly and update its source of truth; never route around it.

The primary implementation language is Go. The CLI/TUI must remain a single
binary without Node. Portal and Desktop use React intentionally.
`evaluation/harbor/src/` is the only other language exception: Python required
by Harbor's custom-Agent interface. It is evaluation tooling, not shipped code,
and nothing under `cmd/` or `internal/` may import it.

## Sources Of Truth

- Task-oriented documentation guide: [`docs/README.md`](docs/README.md)
- Complete bilingual file index: [`docs/index.md`](docs/index.md)
- Current shipped state: [`docs/current-state.md`](docs/current-state.md)
- Active priorities: [`docs/ROADMAP.md`](docs/ROADMAP.md)
- Ready, decomposed work items: [`docs/backlog/`](docs/backlog/README.md)
- Architecture index: [`docs/contribute/architecture/`](docs/contribute/architecture/README.md)
- Repository layout and dependency direction:
  [`docs/contribute/repo-layout.md`](docs/contribute/repo-layout.md)
- Code, naming, IDs, tool output, and commit conventions:
  [`docs/contribute/conventions.md`](docs/contribute/conventions.md)
- Testing and verification: [`docs/contribute/testing.md`](docs/contribute/testing.md)
- Configuration and environment variables:
  [`docs/reference/configuration.md`](docs/reference/configuration.md)
- Design rationale: [`docs/design/`](docs/design/README.md)
- Workspace skills, subagents, and MCP configuration:
  [`.buildmax/README.md`](.buildmax/README.md)

Do not restate the repository tree outside `repo-layout.md`. Read the relevant
architecture document before a cross-package change. Design records explain
rationale; current behavior belongs in user or contributor documentation.

## Architecture Boundaries

The dependency direction is:

```text
bootstrap --> interface / server / service / agentapp / infra --> core
```

- `internal/core` is pure domain code. It must not import config, infra,
  service, server, agentapp, or interface packages.
- `internal/config` loads files and environment; it does not assemble
  infrastructure.
- `cmd/` contains thin entry points for shipped binaries. Build and test tools
  live in `tools/`, which nothing under `cmd/` or `internal/` may import.
- A package owns one business capability or one precise infrastructure concern.
  Each state transition, validation rule, authorization decision, and default
  has one authoritative implementation; handlers, commands, and workers
  delegate to it. Do not restructure ownership incidentally to unrelated work.

Important ownership boundaries:

- `internal/core/agent` owns the shared LLM/tool-calling loop.
- `internal/agentapp` assembles the runtime used by CLI, Desktop, evaluation,
  and workers: models, tools, MCP, hooks, sandbox, traces, skills, sessions,
  and workspace resolution.
- `internal/service/conversation` owns Portal foreground chat and optional
  semantic orchestration. A Conversation may create a Task, but is not its
  execution, authorization, or storage parent.
- Task plus TaskRun is the durable Agent execution plane. Task owns the
  continuing thread; TaskRun owns one turn or attempt and its authoritative
  result. See
  [`docs/design/agent-execution-and-task-threads.md`](docs/design/agent-execution-and-task-threads.md).
- Space is the ownership and authorization boundary for Portal resources. Issue
  is the primary user-facing work object; Workflows are space-scoped reusable
  linear plans.
- Local `Project` is owned by `internal/core/localproject`: one Git repository,
  including its worktrees, or one directory. It is not a server entity.

These files are authoritative within their concerns:

- LLM-facing runtime tool names: `internal/tool/names.go`.
- HTTP routes: each handler subpackage's `Register` method, composed in
  `internal/server/handlers/routes.go` and `internal/server/server.go`.
  `internal/server/static/openapi.json` must match them exactly.
- Bootstrap environment variables: `internal/config/env_spec.go`.
- Database schema: the `xxxRow` structs in `internal/infra/db`, applied by
  `AutoMigrate` in `store.go`. GORM stays in that package; above it, a missing
  row is `apierr.ErrNotFound`.
- Test-only code: `internal/mock` and `internal/testsupport`; production code
  must not import either.

Architecture tests under `internal/architecture` enforce these boundaries.

## Runtime Invariants

- CLI commands and the Bubble Tea TUI live in `internal/interface/cli`.
- Runtime settings use `<BUILDMAX_HOME>/settings.yaml`; server settings use
  `<BUILDMAX_HOME>/server.yaml`. Contributor-local repository configuration
  belongs in the single gitignored `.local/` directory created by
  `./make setup local`; `tools/mk/local.go` owns its contents.
- The system prompt is additive: runtime, `<BUILDMAX_HOME>/AGENTS.md`,
  workspace-root `AGENTS.md`, optional Space instructions for Portal background
  runs, then this run's additional prompt. Compaction summaries are appended by
  `RunLoop`, never inserted into a layer.
- Runtime hooks merge global settings with `<workspace>/.buildmax/hooks.yaml`
  and fail open. See [`docs/design/hook-system.md`](docs/design/hook-system.md).
- The Bash sandbox defaults off for CLI and on with fail-closed enforcement for
  official workers. A worker must never silently run unsandboxed because its
  backend is unavailable. See
  [`docs/design/agent-sandbox-policy.md`](docs/design/agent-sandbox-policy.md)
  and [`deployment/seccomp/README.md`](deployment/seccomp/README.md).
- Plugins are resolved once per runtime. Workers receive only the space's
  explicit, server-resolved activation in a run-scoped `BUILDMAX_HOME`; agents
  inherit no plugins implicitly. See
  [`docs/design/plugin-space-distribution.md`](docs/design/plugin-space-distribution.md).
- Every run records a bounded, redacted JSONL trace by default. Trace failure
  is fail-open.
- Server authentication requires a JWT secret. Login codes are single-use and
  signup defaults off. Never reintroduce or document a fixed development OTP.
- Worker runs materialize the space's files into a run-scoped `workspace/` — the
  Agent's cwd and single writable tool root — and use a run-scoped
  `BUILDMAX_HOME` kept outside that root.
- Portal and Desktop share presentation through `@buildmax/gui`, not data,
  authentication, or routing logic. Both use React 19.

Do not infer shipped or planned behavior from an old design record. Verify it
against code, [`docs/current-state.md`](docs/current-state.md), and the roadmap;
never describe an unverified capability or evaluation score as existing.

## Build, Test, And Verification

Building and verification are part of implementation, not optional handoff
work. Before changing code, use
[`docs/contribute/testing.md`](docs/contribute/testing.md) to select the suites
for that subsystem. `./make help` is the source of truth for the current command
surface; details belong there rather than being copied into this file.

- Run repository checks through the cross-platform task runner from the root.
  Narrow Go tests with `./make test`, never bare `go test`, because only the
  task runner isolates `BUILDMAX_HOME` from the contributor's real data.
- Start with the narrowest relevant check, then run every scope needed to prove
  the change. A passing unrelated or skipped scope is not evidence.
- For autonomous product exploration or hands-on review of a user journey,
  follow [`docs/contribute/exploratory-testing.md`](docs/contribute/exploratory-testing.md).
  Choose a bounded journey, adapt actions to observations, and preserve
  reproducible findings and untested limits. This complements the required
  suites; it does not replace them. Exploratory testing may use configured
  real models, including paid inference, within the task's scope and budget
  without separate confirmation for each run. Record the model and available
  usage/cost evidence as the guide describes.
- Any `internal/infra/db` change requires the MySQL scope against a real MySQL;
  the ordinary suite skips those tests.
- When a Portal, worker, or deployment outcome crosses boundaries that unit
  tests cannot prove, produce the appropriate end-to-end evidence yourself.
- For substantive Portal or server changes, the local kind cluster is the
  preferred end-to-end environment: it exercises the shared ingress, deployed
  images, real MySQL and object storage, and Kubernetes worker Jobs together.
  Use `kind reload` for the edit loop, then the deployment smoke and Portal
  browser suite as the change requires. Compose is a faster inner loop, not a
  substitute when the claim depends on those boundaries.
- Create a kind cluster on demand, not per task. The resident `buildmaxdev`
  cluster is a person's for manual testing; a task that needs its own runs
  `BUILDMAX_KIND_EPHEMERAL=1 ./make kind up` to get an isolated cluster (unique
  name, free ports) recorded in `.local/`, which every later kind command in the
  worktree reuses, and `./make kind down` to remove it when finished. Only spin
  one up when the change actually needs the kind boundary; use Compose or unit
  tests otherwise, so few clusters run at once. See
  [`docs/contribute/testing.md`](docs/contribute/testing.md).
- Real-model smoke tests, cache qualification, and evaluation measure behavior
  and may spend money; they are not deterministic tests or default handoff
  checks. Run them only when the task calls for that evidence.
- On Windows use `make.bat`. Commands are implemented under `tools/mk`; do not
  create a parallel shell-script workflow.
- Inspect help before any command that installs software, starts infrastructure,
  publishes, releases, or otherwise changes the machine or an external system,
  and run it only when the task authorizes that effect.

## Change Rules

- Preserve unrelated work in a dirty worktree. Never reset or overwrite it to
  make checks pass.
- Before opening a pull request based on a design record, fetch `origin` and
  check whether `main` changed that record. Rebase and recheck before merging.
- Persisted JSON uses explicit `snake_case` tags. Database table names are
  singular. Server entities use `NewPublicID` from `internal/util`; see
  [`docs/design/entity-identity.md`](docs/design/entity-identity.md).
- Tool output is written for the LLM and must be meaningful on success and
  failure.
- Comments explain background and decisions, not what the code already says.
  Longer rationale belongs in a design record.
- User documentation is task-oriented, contributor architecture is factual,
  and rationale belongs in design records. Follow
  [`docs/contribute/documentation.md`](docs/contribute/documentation.md).
- After implementing a feature or behavior change, review the related
  documentation against the final code and update it in the same contribution
  before declaring the work complete. Check affected user guides, reference
  documentation, architecture, examples, current-state documentation, and
  roadmap entries; remove or correct stale descriptions. In the handoff, name
  the documentation updated, or explain why no documentation change is needed.
- Documents under `docs/proposals/` and `docs/design/` open with an accurate
  `## Contents` list of their top-level sections.
- Decomposed, ready-to-execute main-line work lives as one file per task in
  [`docs/backlog/`](docs/backlog/README.md), ordered by filename prefix. Create
  a task when its source design is approved; delete the file when the work
  merges. Keep a work item in exactly one place: main-line and agent work in the
  backlog, externally contributable work in GitHub issues. When asked to take
  backlog work, claim the first unclaimed task whose dependencies are complete
  before editing, then deliver its acceptance criteria, verification, and
  documentation; the maintainer owns queue priority.
- Add a changelog file for user-visible changes with
  `./make changelog new <added|changed|fixed|security> <slug>`.
- Commit subjects are imperative lines. Pull request titles carry the
  Conventional Commits prefix. Do not add assistant, model, or tool
  attribution, generated-by footers, session links, or tooling trailers.
  `Co-authored-by` may credit substantive human collaboration. The full rules
  are in [`docs/contribute/conventions.md`](docs/contribute/conventions.md).
- Dependency changes include lockfiles and license checks. Assess
  security-sensitive changes against [`SECURITY.md`](SECURITY.md) and the
  sandbox and hook trust boundaries.
- Do not commit `.vibe/`; it is local scratch state.

## Definition Of Done

A contribution is ready when the requested behavior is implemented, relevant
tests and scoped checks pass, documentation and examples match the code,
generated and lock files are intentional, and `git diff --check` is clean.
Report checks that were not possible rather than treating them as passed.
