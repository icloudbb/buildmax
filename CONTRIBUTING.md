# Contributing to BuildMax

Thanks for helping improve BuildMax. Small, focused contributions are the
easiest to review and keep the shared runtime dependable across CLI, Desktop,
Portal, and worker execution.

## Before You Start

- Search existing issues and [design documents](docs/design/) before proposing a
  large change. [docs/README.md](docs/README.md) explains how the documentation
  is organized.
- Open an issue or discussion first for product changes, new runtime providers,
  new tools, or changes that affect security, persistence, or public APIs.
- Never include credentials, customer data, or private deployment details in an
  issue, pull request, test fixture, or commit.
- Report vulnerabilities privately as described in [SECURITY.md](SECURITY.md).
- Use [SUPPORT.md](.github/SUPPORT.md) to choose the right channel, and follow
  [CODE_OF_CONDUCT.md](.github/CODE_OF_CONDUCT.md) in every project space.
- Project decisions and maintainer responsibilities are described in
  [GOVERNANCE.md](.github/GOVERNANCE.md) and [MAINTAINERS.md](.github/MAINTAINERS.md).
- The rules review will hold a pull request to — persisted JSON naming, table
  names, entity IDs, tool output, commit subjects, changelog entries — are in
  [docs/contribute/conventions.md](docs/contribute/conventions.md). Read it once
  before your first pull request.
- Maintainers follow [docs/contribute/releasing.md](docs/contribute/releasing.md)
  for version tags, release verification, and recovery.

## First Contributions

**[docs/contribute/first-pr.md](docs/contribute/first-pr.md) walks the whole
path end to end** — clone, build, test, change one thing, open the pull request
— in about fifteen minutes, without a model API key.

If you know the kind of problem you want to work on but not where it belongs,
[choose a contribution area](docs/contribute/areas.md). It connects testing,
Agent Runtime, local experience, enterprise platform, trust and security, and
documentation work to the current source of truth and public issue labels.

**Tests are the sharpest current need.** A large and growing share of this
code is written with AI assistance, which raises how fast the codebase changes
without raising test coverage to match, so a pull request that adds regression
coverage for existing, currently-untested behavior does not need an issue
filed first — see [Testing](docs/contribute/areas.md#testing).

Good first pull requests are small, easy to verify, and close to an existing
issue. Look for issues labeled `good first issue`, `help wanted`, or
`documentation`; if none are available, start with a bug report, documentation
gap, or discussion instead of opening a large speculative PR.

## AI-First Development And Accountability

BuildMax is an Alpha project that evolves quickly and is developed with
AI-first workflows. Agent-native contribution is welcome, whether it produces a
small test, a documentation update, or a larger code change. The project aims
to make those workflows effective: architecture, task scripts, documentation,
tests, and issue acceptance criteria should let an Agent discover its boundary,
make a focused change, and verify the result without guesswork.

The person opening a pull request must be able to explain the change, assess its
effects, run the relevant checks, and address review feedback. AI-generated code
and documentation do not change that responsibility; they meet the same
requirements as any other contribution:

- tests and checks appropriate to the change;
- accurate user and contributor documentation;
- dependency-license and security requirements; and
- a focused, reviewable pull request with its limitations stated plainly.

Changes to security boundaries, permissions, credentials, data migrations, or
public APIs need deliberate human review in addition to automated checks. Open
an Issue or Discussion before starting one when the direction is not already
settled.

Task-specific disclosure of AI assistance in pull request prose is encouraged
when it helps reviewers understand the scope or validation performed, but it is
not a separate quality label. Do not add assistant, model, or tool attribution
trailers, generated-by footers, or assistant-session links; the pull request
description must still explain the problem, approach, verification, and
remaining limitations.

When improving contributor tooling, prefer changes that make the repository
more legible and verifiable to both people and Agents: a named source of truth,
deterministic checks, bounded task scope, and actionable failures. Do not add a
second agent implementation or a workflow that bypasses the shared runtime just
to optimize one surface.

Before picking work, read the [support matrix](manual/support.md). It names
the surfaces and deployment paths the project supports today, plus the alpha
non-goals that should not become surprise PR scope. Documentation fixes, focused
tests, small CLI/TUI usability fixes, and reproducible bug fixes are the best
entry points.

Use the issue templates to give maintainers the context they need:

- **Bug report:** reproducible behavior that differs from docs or expectations.
- **Feature request:** a user problem and expected outcome, before implementation.
- **Documentation:** missing, unclear, or stale docs.
- **Implementation task:** scoped work with acceptance criteria, likely code
  areas, and verification commands; this is the best template for work that is
  ready for a contributor to pick up.

## Prerequisites

**You do not need a model API key to contribute.** `./make build`, `./make
test`, `./make lint`, and both deployment smokes run without one — the smokes
use a deterministic mock model in `deployment/smoke/mock-llm`, and CI has no
model credentials at all. A key is needed only to actually talk to a model:
running the agent yourself and `./make eval`.

| Tool | Needed for |
|---|---|
| **Go** — the version in `go.mod` | Everything. The CLI, server, worker, and desktop backend are all Go; the CLI has no Python or Node runtime dependency. |
| **Node 24 and npm 11** — pinned by `.node-version` and `packageManager` | The frontends — `gui/`, `portal/`, `desktop/frontend/` — and the Markdown lint in `./make check docs`, so a documentation-only change needs it too. Use `npm ci`; Go work does not. |
| **Docker** | The Compose deployment smoke and container changes. |
| **kubectl** | Kubernetes worker, RBAC, shared-storage, or manifest changes. |
| **shellcheck** | Workflow changes. actionlint skips its shell script pass without it, and says nothing; `./make doctor` reports whether you have it. |
| **An LLM API key** | Running the agent for real, and `./make eval`. Add a model to `~/.buildmax/settings.yaml`; see [docs/reference/configuration.md](docs/reference/configuration.md). |

That table is the whole list. Several tools a contributor might expect to
install — golangci-lint, govulncheck, actionlint, gitleaks, go-licenses,
GoReleaser, kind, and the Wails CLI — are pinned in [`tools/mk`](tools/mk) and run
through `go run`, so the version you get is the version CI runs and there is
nothing to keep up to date.

If you use [mise](https://mise.jdx.dev), `mise install` fetches the pinned Go
and Node from `mise.toml` — which carries the same versions as `go.mod` and
`.node-version`, and a test fails when it drifts from them. Then run
`corepack enable` once so npm follows the `packageManager` pin. Neither is
required: `./make` cares about the versions, not how they arrived.

The task runner reports missing tools; it does not install system packages for
you. Go is the one prerequisite it cannot even report, because the task runner
is itself Go: `./make` checks for it first and prints where to get it. Free
OpenRouter models rate-limit with HTTP 429 when called too frequently; if agent
runs start failing in bursts, that is usually why.

Run `./make doctor` after cloning. It checks the Go/git path, reports optional
tools, and warns about local changes without modifying the workspace. Use
`./make doctor all` before full or frontend work to require the pinned Node/npm
versions, and `./make doctor harbor` before running the external Terminal-Bench
target. `./make setup harbor` is the one command that installs rather than
reports: it installs uv, the pinned Harbor, and the Linux CLI a trial uploads,
and leaves the trial sandbox to you. A global Wails install is optional: production builds run the version
pinned by `go.mod` through Go.

## Build, Test, and Run

The root `./make` script is the primary local workflow — on Windows, `make.bat`.
Both are one-line shims around the task runner in [`tools/mk`](tools/mk), so every
platform runs the same code. Run `./make setup local` once in a fresh clone: it
creates `.local/`, the one gitignored directory holding your own configuration,
and prints what is left to fill in.
[docs/reference/configuration.md](docs/reference/configuration.md#contributor-local-files-local)
lists what each file is for. `.local/env` is loaded into the environment of
every task automatically.

```bash
./make doctor         # read-only toolchain and workspace diagnosis
./make build          # strict build: CLI, server, worker, gui, Portal, desktop
./make build cli      # CLI only
./make fmt            # gofmt every tracked Go file
./make test           # go test ./... with BUILDMAX_HOME=./testing-sandbox
./make test race      # the same tests with the race detector
./make test ./internal/tool -run TestX   # one package or one test
./make check docs     # scoped gate: go, portal, desktop, docs, all, or ci
./make run server     # run the already-built buildmax-server
./make run portal     # Portal dev server (builds gui if needed)
./make run desktop    # run the already-built Wails desktop app
./make compose smoke  # full local-process TaskRun and artifact smoke
./make kind up        # full Kubernetes stack plus the same smoke assertions
./make clean          # binaries, desktop build dir, node_modules, dist
```

`./make help` lists every command — development, advanced, deployment, and
release — with the common contributor path under it; `./make help <command>`,
which `<command> --help` also reaches, shows that command's arguments, examples,
and caveats. To add or change a command, edit `tools/mk` rather than the shims — a
new command belongs in the help tables and needs a page of its own, which a test
enforces.

`doctor`, `build cli`, `test`, `lint`, and scoped `check` are safe local
defaults. `install`, `release`, `compose`, `kind`, and publication tasks
change the machine, repository, or external systems; use them only when that
effect is intended. `./make build` is strict: a GUI, Portal, Desktop frontend,
Wails, or copy failure fails the command instead of producing a partial success.

`./make test` writes to `./testing-sandbox` instead of `~/.buildmax`, so tests
never touch your real data directory. The sandbox is created on demand and is
gitignored. Narrow a run by passing packages and `go test` flags — packages
first — rather than reaching for a bare `go test`, which sets no `BUILDMAX_HOME`
at all. `config.DataDir` panics under test instead of falling back to your real
home, so that mistake now says so instead of quietly reading your own sessions
and credentials.

Frontend packages also build from their own directories; see
[README.md](README.md), [portal/README.md](portal/README.md), and
[gui/README.md](gui/README.md).

`./make agent-smoke` and `./make run server` are for manual local checks. Do
not use them in automated CI. `agent-smoke` drives the agent's tools with a
real model and needs an API key: a model executes the checks and reports its
own PASS/FAIL table, so its exit code says only that the process finished. The
deterministic suites are `./make test` and `./make e2e <suite>`.

### The `desktop` Build Tag

The desktop frontend bundle lives in `desktop/dist/`, a Vite build
artifact that is not checked in. Embedding it unconditionally would make
`go build ./...`, `go vet ./...`, and `go test ./...` fail on a fresh clone,
so the `//go:embed` directive sits behind the `desktop` build tag:

- without the tag, `desktop/assets_stub.go` compiles and embeds nothing, which
  is what every standard Go command and your editor use
- with `-tags desktop`, `desktop/assets_embed.go` compiles and embeds the bundle

Vite writes to `desktop/dist/` rather than into `desktop/frontend/` because
`desktop/frontend/` is a separate Go module — see
[repo-layout.md](docs/contribute/repo-layout.md#nested-go-modules) — and
`//go:embed` cannot reach across a module boundary.

`./make build` passes the tag and builds the frontend first, so you rarely think
about it. A desktop binary built without the tag refuses to start and prints how
to rebuild it, rather than opening a blank window.

One exception is worth knowing before you touch this area: Wails treats
`desktop` as one of its own reserved mode tags and strips it from the throwaway
binary it compiles to generate JS/TS bindings. That binary therefore always sees
`Embedded == false`, and the refuse-to-start guard in
`cmd/buildmax-desktop/main.go` skips it via the `bindings` build tag. See
`cmd/buildmax-desktop/bindings_on.go` — without it, every `wails build` fails
during binding generation.

## Local Infrastructure

Choose the smallest deployment path that exercises your change.

For server, Portal, API, scheduler, or local-process worker changes, use
Compose. It runs MySQL, Portal, server, and a deterministic mock model; workers
share the server container and local filesystem storage:

```bash
./make compose smoke
./make compose status
./make compose logs
./make compose down
```

For Kubernetes Job, RBAC, Ingress, MinIO, or deployment manifest changes, use
kind. You do not install kind: `./make` runs the pinned version through Go, so
one version creates, inspects, and deletes every cluster. Docker and kubectl are
still yours to install. `up` creates the cluster, builds and loads images,
deploys every dependency, and runs the same TaskRun and artifact assertions:

```bash
./make kind up
./make kind smoke
./make kind status
./make kind logs
./make kind down
```

[docs/deploy/local-kind.md](docs/deploy/local-kind.md) documents what it
installs; the manifests it applies are in `deployment/kind/` and exist only
for local development. Override the cluster name with `BUILDMAX_KIND_CLUSTER`
(default `buildmaxdev`); every kubectl call uses that cluster's explicit
context.

### Container Images

The Dockerfiles live in `deployment/docker/`: `Dockerfile.buildmax` for the Go
binaries, `Dockerfile.portal` for the Portal, and `Dockerfile.release` for the
GoReleaser-built published image. All three take the repository root as their
build context. To build the first two, load them into the kind cluster, and
restart the deployments so a code change takes effect:

```bash
./make kind reload
```

Set `BUILDMAX_IMAGE_PLATFORM` to cross-build — for example
`BUILDMAX_IMAGE_PLATFORM=linux/amd64 ./make kind reload` on Apple Silicon.

`deployment/buildmax-deploy.yaml` is the readable Kubernetes baseline. It
carries no credentials: `./make setup local` writes
`.local/buildmax-secret.yaml` from `deployment/buildmax-secret.example.yaml`;
fill it in and `kubectl apply -f` it separately for a non-smoke deployment.
`.local/` is gitignored — never commit real values. `./make kind up` does not
read that file; it generates a throwaway secret of its own.

## Code Boundaries

Keep changes aligned with the existing layering:

- shared agent behavior belongs in `internal/core/agent` or `internal/agentapp`
- infrastructure adapters belong in `internal/infra`
- user-facing orchestration belongs in `internal/service` or `internal/interface`
- `internal/core` must not import application, infrastructure, or interface layers

The layering rules are enforced by tests in `internal/architecture`, so a
violating import fails the build rather than sliding through review.

Naming and format rules — `snake_case` persisted JSON, singular table names,
prefixed entity IDs, LLM-facing tool output — are in
[docs/contribute/conventions.md](docs/contribute/conventions.md).

## Pull Requests

**Pull requests are merged with a merge commit**, so every commit on the branch
lands on `main`. Two things follow from that, and they are the two most common
review comments on a first contribution:

- **Every commit subject is public.** Write each one as a single imperative line
  that reads on its own in `git log --oneline` — `Add a login-code expiry
  check`, not `fixed login stuff`, `WIP`, or a bare issue number. Tidy the
  branch before review rather than after; `wip` and `address review` are
  permanent once merged.
- **One coherent change per pull request.** The merge commit is the revert
  handle, so a pull request that does three unrelated things cannot have one of
  them backed out. Two changes, two pull requests.

The rest:

- **The title takes a type prefix.** Since it becomes the merge commit subject,
  write it as a [Conventional Commits](https://www.conventionalcommits.org/)
  type and then the same imperative line — `feat: Add a login-code expiry
  check`, `docs: …`, `fix(portal): …`, or `!` before the colon for a change that
  breaks existing public behavior. Only the title; commit subjects stay bare.
- `Co-authored-by` may credit substantive human collaboration. Do not use it for
  an assistant, model, or tool; do not add `Claude-Session` or other tooling
  trailers, a "Generated with …" footer, or an assistant session link. The
  reasoning is in
  [conventions.md](docs/contribute/conventions.md#commit-messages-and-pull-requests).
- Add or update focused tests for behavioral changes.
- Add a changelog entry when a user or operator would notice the change: new or
  changed behavior, new configuration, removals, fixes to released behavior.
  `./make changelog new fixed <slug>` writes the file under
  [`docs/changelog/`](docs/changelog/README.md) with the shape a release
  expects. Internal refactors, test-only changes, and documentation edits do not
  need one.
- Update documentation alongside the code:
  - behavior or package boundaries change → update the matching document in
    [docs/contribute/architecture/](docs/contribute/architecture/README.md)
  - a package moves → update
    [docs/contribute/repo-layout.md](docs/contribute/repo-layout.md), which is
    the only place the tree is written down
  - direction changes → add or update a semantic design record in
    [docs/design/](docs/design/README.md), and delete the superseded one
  - user-facing behavior or configuration changes → update `manual/`,
    `docs/reference/`, and `config-examples/`

  The full rules are in
  [docs/contribute/documentation.md](docs/contribute/documentation.md).
- Explain the problem, the approach, verification performed, and any remaining
  limitations in the pull request description.
- Preserve existing public behavior unless the pull request explicitly documents
  a breaking change.

Every pull request must pass three stable jobs: `Go (build, vet, test)`,
`Frontend (gui, portal, desktop)`, and `Open source policy`. Together they run
`gofmt`, a `go mod tidy` cleanliness check, build, vet, a Windows
cross-compile, golangci-lint, govulncheck, the Go test suite with `-race`, all
three frontend builds, frontend lint and tests, a Git-history secret scan,
dependency-license checks, and Markdown lint.

Every pull request also runs `Visual (Portal, Desktop)`. It compares Portal and
Desktop screenshots with the committed baselines and runs axe, contrast
included. When a change to how a page looks is intended, refresh the baselines
with `./make e2e visual --update` and commit them with the change; see
[docs/contribute/testing.md](docs/contribute/testing.md#screenshot-baselines).

Additional pull-request checks are path-scoped rather than universal. Release
configuration changes run `goreleaser check`, and Portal image changes build
the image. The native Windows suite runs after Go, task-runner, and Windows
changes merge to `main`, daily, and on demand; a failure there is fixed forward.
CodeQL runs after pushes to `main`, weekly, and on demand. Pushes to `main` and
manual runs also build and smoke-test a non-publishing release snapshot on
Linux, macOS, and Windows. Deployment-related changes run the Compose and kind
end-to-end smoke paths after merge.

Locally:

```bash
./make fmt               # gofmt every tracked Go file, the fix `check go` asks for
./make test              # go test with BUILDMAX_HOME=./testing-sandbox
./make test race         # the CI race suite with the same isolated home
./make lint              # golangci-lint and govulncheck, CI's pinned versions
./make build             # every binary, including the frontends
./make check all         # all local Go, frontend, and documentation gates
./make check ci          # required PR suite plus the conditional release check
```

`check ci` is for when CI minutes are scarce or the feedback loop matters more
than the wait. On top of `check all` it lints the workflows, scans Git history
for secrets, checks Go and npm production dependency licenses, validates the
GoReleaser configuration, and cross-compiles for Windows. It intentionally runs
the path-scoped checks even when the current diff would not trigger them on a
pull request. It uses the tool versions the workflows pin; a test fails if the
local and hosted versions drift. Three gaps remain:

- The post-merge Windows job runs the suite on Windows. CI and the local step
  only cross-compile.
- `npm ci` runs only when `node_modules` is missing, so lockfile drift needs an
  explicit `npm ci` in the frontend you touched.
- CI checks out clean and ends with `git diff --exit-code`. Locally that would
  fail on unrelated work in progress, so `check ci` reports only files its own
  steps changed.

When editing a workflow, lint it the way CI does — `./make check ci` does this
for you. `go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12` silently
skips its shellcheck pass when shellcheck is not on your PATH, so a `run:` block
can pass locally and fail on the runner. The published image carries shellcheck:

```bash
docker run --rm -v "$PWD:/repo" -w /repo rhysd/actionlint:1.7.12 -color
```

The linter set and the reasoning behind each exclusion are in
[`.golangci.yml`](.golangci.yml). For the frontends, `npm run lint` in `portal/`
and `desktop/frontend/`; `gui/` has no ESLint step because typescript-eslint
does not yet support the TypeScript version it builds with, and its own build
type-checks it.

A govulncheck failure usually means the Go toolchain or a dependency needs a
patch release. Bump the `go` directive in `go.mod` or the module, do not add a
suppression.

## Contribution License

By submitting a contribution, you agree that it is your original work and that
you license it under the [Apache License 2.0](LICENSE). This is the default
contribution grant described in Section 5 of that license; no separate CLA is
required at this stage.
