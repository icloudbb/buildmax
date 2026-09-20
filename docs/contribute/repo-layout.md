# Repository Layout

> **简体中文：** [阅读中文镜像](../zh-CN/contribute/repo-layout.md)
> **Audience:** contributors · **Status:** current
>
> **This file is the single source of truth for the repository tree.** README,
> `AGENTS.md`, and the architecture reference link here rather than repeating
> it, so there is one place to update when a package moves.

## Top Level

```text
buildmax/
├── cmd/                  Entry points for the shipped binaries (main.go only)
├── tools/                Build and test tooling; never shipped
├── internal/             All Go implementation
├── portal/               Portal web app (React 19 + Vite + TypeScript)
├── desktop/frontend/     Desktop frontend (React 19 + Vite); src/lib holds the
│                      pure helpers, src/components the panels and modals
├── gui/                  Shared React package @buildmax/gui, used by both
├── docs/                 Documentation
├── config-examples/      settings.yaml / server.yaml / hooks.yaml examples
├── deployment/           Deployment manifests, Compose, Dockerfiles, local kind
├── evaluation/           Evaluation and qualification system
├── sample-data/          Seed datasets to upload into a workspace or point the CLI at
├── .github/              CI workflows, issue and PR templates, community health files
├── .buildmax/            This repository's own workspace agent config — see .buildmax/README.md
├── make, make.bat        One-line shims around the task runner in tools/mk
└── *.md, LICENSE         README, CONTRIBUTING, SECURITY, CHANGELOG, AGENTS
```

Generated and never committed: `.local/` (your own configuration, written by
`./make setup local`), `bin/` (`./make build` output), `dist/` (GoReleaser),
`NOTICE-THIRD-PARTY`, `testing-sandbox/` (`./make test` data directory), and
every `node_modules/` and frontend `dist/`.

Root Markdown, and who each file is for:

| File | Audience |
|---|---|
| `README.md` | Anyone landing on the repository |
| `CONTRIBUTING.md` | Contributors — prerequisites, build, test, pull requests |
| `SECURITY.md` | Vulnerability reporters and operators |
| `CHANGELOG.md` | Users and operators, per release |
| `AGENTS.md` | The agent, on every run in this workspace ([agents.md](https://agents.md/) convention). `CLAUDE.md` points at it. |
| `.github/CODE_OF_CONDUCT.md`, `SUPPORT.md`, `GOVERNANCE.md`, `MAINTAINERS.md`, `TRADEMARKS.md` | Community health files. GitHub surfaces them from `.github/` exactly as it does from the root. |

`deployment/` holds everything needed to run a deployment rather than to build
one binary:

| Path | Contents |
|---|---|
| `deployment/docker/` | `Dockerfile.buildmax` (Go binaries from source), `Dockerfile.portal` (Portal via nginx), `Dockerfile.release` (packages GoReleaser's cross-compiled binaries). All three take the **repository root** as their build context. |
| `deployment/compose/` | Single-machine Compose stack — a **real deployment path**, running published GHCR images; see [deploy/compose.md](../deploy/compose.md) |
| `deployment/kind/` | Manifests that stand up the **local development** kind cluster — kind config, ingress-nginx, MySQL, MinIO. Never part of a real deployment; applied by `tools/mk/kind.go` behind `./make kind up`. |
| `deployment/ocean/` | OpenTofu for the disposable DigitalOcean beta-qualification infrastructure. It reads the persistent Project, VPC, and Spaces bucket and owns only the temporary DOKS and MySQL resources behind `./make ocean`. |
| `deployment/production/` | The private deployment reference: one plain-YAML manifest written to be read and adapted, plus the dependency contract it assumes. Deliberately not a chart or a kustomize base, so it converts to whatever a cluster is already managed with. Nothing applies it; `internal/architecture` parses it so it cannot rot |
| `deployment/smoke/` | Overlays and the mock model that make the Compose and kind smokes deterministic |
| `deployment/buildmax-deploy.yaml` | Working Kubernetes manifest used by `./make kind up` |

`kind/` is still local test infrastructure, not a supported deployment path:
the short name matches the `./make kind` command, while this table and
[deploy/local-kind.md](../deploy/local-kind.md) define its scope. `compose/` is
different because an operator is meant to run it — its audience is operators,
`README.md` files it under "Running it for a space", and `compose.yaml` pulls
`ghcr.io/icloudbb/buildmax`. `smoke/` is test scaffolding shared by both
smokes.

There is no `scripts/` directory. Repository tooling — release-archive
verification, third-party notice generation, npm license checks — lives in
`tools/mk` as `./make` commands, so every task runs the same way on macOS, Linux,
and Windows and is covered by the same tests. CI and GoReleaser invoke those
commands as `go run ./tools/mk <task>`, which needs no shell.

## Nested Go Modules

One kind of directory sits outside the root module, with its own `go.mod`:

- **`gui/`, `portal/`, `desktop/frontend/`** contain no Go code. Their `go.mod`
  is a boundary. The Go tool has no special case for `node_modules` the way it
  does for `testdata`, so without it every `go build ./...`, `go vet ./...`,
  `go test ./...`, and `go mod tidy` at the root compiles whatever Go sources
  npm packages happen to ship — the `flatted` package, pulled in transitively
  by ESLint, ships one. Any directory that installs npm dependencies needs one;
  `internal/architecture` has a test that enforces this.
`evaluation/suite/*/state/` needs no such boundary today: the committed task
fixtures hold data and text rather than Go sources. One that ships a Go module
would need its own `go.mod` for the same reason as the frontends — otherwise
`go build ./...` compiles a fixture that is meant to be broken.

## Binaries

| Path | Binary | Role |
|---|---|---|
| `cmd/buildmax` | `buildmax` | CLI/TUI |
| `cmd/buildmax-server` | `buildmax-server` | HTTP API + in-process scheduler |
| `cmd/buildmax-worker` | `buildmax-worker` | Executes one task run, then exits |
| `cmd/buildmax-desktop` | — | Wails desktop app; embeds `desktop/frontend` |

Every `cmd/*` package is a thin `main.go` that delegates to `internal/`.

## `tools/`

`tools/` holds the Go programs that build and test this repository. They are the
same language as the rest of the project so that every task runs on macOS,
Linux, and Windows without a shell, but they are not part of the product: no
release builds them, and nothing under `cmd/` or `internal/` may import them.
The dependency runs one way — a tool may reach into `internal/`, and
`tools/eval` does, but it measures the CLI as a black box by running
the built binary.

| Path | Binary | Role |
|---|---|---|
| `tools/mk` | — | Task runner behind `./make` and `make.bat`, plus the repository tooling CI and GoReleaser call |
| `tools/eval` | `eval` | Evaluation runner: measures a built binary against `evaluation/suite/` as a black box |
| `tools/mcp` | — | Small MCP server, so MCP wiring has something real to connect to in tests and in `/mcp` |

The split from `cmd/` is what "not shipped" is written down as, and
`go-licenses` reads it that way: it checks `./cmd/...`, because a dependency
nothing redistributes carries no attribution obligation. Today the scope loses
nothing either way — every third-party module under `tools/` is already
reachable from a shipped binary, and `tools/mk` depends on the standard library
alone.

## `internal/`

```text
internal/
├── bootstrap/          Process startup and dependency wiring (server, worker, objectstore)
├── config/             YAML + env config loading and path resolution
│
├── core/               Pure domain layer — no infra imports
│   ├── apierr/         Why a service refused: a Kind a transport maps to a
│   │                   status, plus ErrNotFound, what a store says when the
│   │                   row or object a caller named is not there
│   ├── llm/            LLM contracts (Message, ToolDef, ToolCall, Usage, LLMClient),
│   │                   the Tool contract, ToolRegistry, and tool policy
│   ├── jsonschema/     The JSON Schema subset structured output and Workflow
│   │                   input schemas share: compile-time subset check and value
│   │                   validation
│   ├── hook/           The hooks configuration shape, its events and transports
│   ├── mcp/            The mcp.json document shape and its validation rules
│   ├── agent/          The tool-calling loop, events, hooks, sandbox contract
│   ├── plugin/         Plugin manifest, version arithmetic, and the layer
│   │                   vocabulary discovery, resolution, and publication share,
│   │                   plus the catalog entry, its releases, and a space's
│   │                   activations
│   ├── subagent/       The subagent definition file shape and its frontmatter
│   ├── space/           The Space, its members, its store, and the one
│   │                   role/action decision the HTTP guard and the space
│   │                   service both enforce
│   ├── artifact/       The Artifact: a file somebody chose to keep, its record,
│   │                   its store, and how its content object is addressed
│   ├── llmgateway/     What a deployment brokers and records: the model catalog
│   │                   entry, the call ledger entry, and their stores
│   ├── quota/          Tier limits and the usage window a refusal is measured
│   │                   against
│   ├── conversation/   The durable Conversation and its messages: what Tier 1
│   │                   orchestrates and stores, distinct from a local session
│   ├── workflow/       A space's reusable linear plan, its revisions, and the
│   │                   run and step-run state its execution moves through
│   ├── agentdef/       The Agent a space defined and its revisions -- what an
│   │                   Agent is configured to be, not the loop that runs it
│   ├── issue/          The Issue, its hierarchy, and its owner/executor
│   │                   vocabulary, plus the comments people and agents leave on it
│   ├── audit/          The append-only trail: what an event is, the actions
│   │                   worth recording, and how it is read and pruned
│   ├── secret/         The Space Secret: a group of named items, its lifecycle,
│   │                   the sealed-bytes and store contracts — no crypto, no
│   │                   persistence
│   ├── schedule/       A space's recurring time trigger: what a Schedule is, its
│   │                   due-time and firing lifecycle, and its store contract —
│   │                   no cron parsing, no timers
│   ├── task/           Tier 2 durable work: the Task, its runs and their one
│   │                   legal set of transitions, run output, and delivery
│   ├── identity/       Who a caller is: the account, its credentials, its
│   │                   rotating sessions, and the deployment roles it holds
│   ├── eligibility/    May this account run work in this Space right now: the
│   │                   account-not-disabled and still-a-member gate the durable
│   │                   dispatch paths share with the HTTP guard
│   ├── schema/         What a database says has been done to it: the applied
│   │                   migrations infra/db reports and the admin route reads
│   ├── session/        Local session model; persistence lives in agentapp
│   └── localproject/   The local Project: the identity CLI, TUI, and Desktop
│                       sessions share for one repository or directory, and the
│                       scope its cross-session memory will belong to
│
├── agentapp/           Agent runtime assembly: LLM client cache, tool registry,
│   │                   MCP, hooks, sandbox, traces, skills, sessions, workspace
│   ├── job/            Local background jobs: identity, state, output, shutdown
│   ├── worktree/       Git worktree lifecycle for one session: what may be
│   │                   entered, who occupies it, and the root it moves
│   └── taskrun/        One task run inside its run-scoped directory (worker)
│
├── service/            Application services: coordinate stores, enforce rules
│   ├── conversation/   Portal foreground chat and optional Task orchestration
│   │   └── channel/    Normalized turn types and channel adapters (webhook)
│   ├── agent/          Agent definitions, their revisions, and the delete guard
│   ├── artifact/       Durable files a space keeps; knows no producer
│   ├── llmcatalog/     What the model catalog accepts and what changing it
│   │                   records; the shell and the admin route both call it
│   ├── systemadmin/    Who holds a deployment-scoped role; the last-holder
│   │                   rule turns on the caller's authority, not a flag
│   ├── accountlifecycle/ Sequences an account disable/enable and its cleanup —
│   │                   sessions, webhook keys, schedules, in-flight runs — and
│   │                   projects the deactivation impact
│   ├── spacerecovery/  The disabled-owner-only ownership recovery: promote an
│   │                   enabled member when every recorded owner is disabled
│   ├── identity/       What proves who a caller is: verifying a credential and
│   │                   opening the session it earns
│   ├── issue/          Issue service
│   ├── task/           Task and task_run service
│   ├── schedule/       Cron parsing and timezone handling for recurring
│   │                   schedules — the library and DST logic core excludes
│   ├── workspace/      Cross-storage commit of Task workspace checkpoints:
│   │                   validate a payload descriptor, confirm its bytes are
│   │                   durable, then record the authoritative pointer
│   ├── workflow/       Workflow and workflow-run orchestration
│   ├── audit/          Records that a sensitive action happened (governance,
│   │                   not diagnostics — see the package doc)
│   ├── plugin/         Marketplace publication and catalog lifecycle
│   ├── plugininspect/  Sanitized inspection of what a plugin archive contributes
│   ├── secret/         Space Secret lifecycle: validate items, seal them through
│   │                   a Sealer, store metadata and sealed bytes; no reveal path
│   ├── space/           Membership: who is in a space and who may change that
│   ├── quota/          Space quota enforcement
│   └── llmgateway/     Model catalog, name resolution, routing, and managed calls
│
├── tool/               Runtime agent tools: Read, Write, Edit, Bash, Glob, Grep,
│                       WebFetch, TodoWrite, NoteWrite, Skill, Task, and the MCP
│                       gateway, plus UploadArtifact, the Job tools, and Monitor
│                       where the surface serves them. names.go is the single
│                       source of truth for tool names.
│
├── infra/              External-system implementations
│   ├── coordination/   Shared Redis primitives for a multi-replica server:
│   │                   publish/subscribe, per-conversation leases, per-task
│   │                   replayable streams. Free of server types.
│   ├── db/             MySQL/GORM implementation of the core repositories
│   ├── objectstore/    Local FS and S3/MinIO storage: space home, run output,
│   │                   and artifact content — three key spaces, one backend
│   ├── llm/            LLMClient over the wire protocols BuildMax speaks:
│   │                   OpenAI Chat Completions, OpenAI Responses, Anthropic Messages
│   ├── llmwire/        Versioned wire contract for managed inference
│   ├── llmremote/      LLM client that calls a BuildMax managed gateway
│   ├── mcp/            MCP protocol, client transport, registry
│   ├── oidc/           OpenID Connect provider: cached Discovery and JWKS, and
│   │                   asymmetric-only ID-token verification, over go-oidc
│   ├── hook/           Hook transports: command, http, mcp_tool, prompt
│   ├── pluginwire/     Wire contract for the private plugin Marketplace
│   ├── pluginarchive/  Packing and hardened extraction of plugin archives
│   ├── wsarchive/      Task workspace checkpoint payload: tar.zst.v1 packing
│   │                   and hardened, adversarial-validated extraction
│   ├── proc/           Process supervision for local background jobs:
│   │                   group spawn, bounded output rings, tree termination
│   ├── secret/         Space Secret cryptography: envelope encryption of the
│   │                   item map, and the KEK providers that wrap the DEKs
│   ├── sandbox/        Seatbelt/bwrap backends, egress proxy, violations
│   ├── sessionstore/   Session journal file backend: JSONL codec, single-writer
│   │                   lock, tail repair, salvage
│   ├── localprojectstore/ Local Project file backend: the bundle, the
│   │                   rebuildable catalog projection, and the writer lock
│   ├── locallaunchpadstore/ Desktop's launchpad (quick-launch apps) JSON file backend
│   ├── localschedulestore/ Desktop's local scheduled-task JSON file backend
│   ├── localterminalsnapshotstore/ Desktop's terminal buffer snapshots (restore on restart)
│   ├── trace/          Durable run-trace recorder (bounded, redacted JSONL)
│   ├── k8s/            Kubernetes worker job launcher
│   ├── workerclient/   Worker-side HTTP client for the server worker API
│   ├── runbridge/      Per-run Unix-socket reverse proxy to the worker API, so a
│   │                   subprocess reaches it without holding the run token
│   │                   (docs/design/agent-bridge-cli.md)
│   ├── httpclient/     Decodes the server's error envelope for its Go clients
│   ├── flock/          Advisory file lock the OS releases when the holder exits
│   ├── git/            Branch, diff, and worktree helpers
│   └── log/            slog + lumberjack logging
│
├── interface/          Local user-facing entry points
│   ├── cli/            Cobra CLI, Bubble Tea TUI, print mode
│   ├── desktop/        Wails app bridge
│   ├── slashcmd/       Shared source of truth for the chat's slash-command set,
│   │                   read by both the TUI and Desktop
│   ├── pluginmgr/      Installing, publishing, and removing plugins locally —
│   │                   the mechanism the CLI and Desktop both run
│   ├── auth/           Login client and credential persistence
│   └── client/         HTTP client for the BuildMax server API
│
├── server/             HTTP API for Portal and worker callbacks
│   ├── handlers/       Route handlers
│   │   ├── account/    What the acting account owns across spaces: webhook keys
│   │   ├── admin/      Deployment-scoped routes; a Config that cannot reach a space
│   │   ├── artifact/   Artifacts, addressed by opaque ID; space comes from the record
│   │   ├── auth/       Establishing a session: login, refresh, logout, password
│   │   ├── auditexport/  CSV export shared by the space and admin audit routes
│   │   ├── llmcallview/  Prices a call ledger row, shared by the space and admin routes
│   │   ├── llmhttp/    Managed gateway over HTTP, shared by the space and worker routes
│   │   ├── runterminal/  Announces a finished run to whoever is watching
│   │   ├── space/       What a space owns: members, agents, usage, audit
│   │   ├── work/       Issues, workflows, tasks, conversations, and their runs
│   │   └── worker/     Worker API; authenticates with a run token, not a session
│   ├── access/         Who is calling, which space, and whether they may
│   ├── coordination/   Redis-backed adapters that make the stream hub, connection
│   │                   events, and turn serialization work across replicas
│   ├── authtoken/      Signs and verifies the run token a worker presents
│   ├── httputil/       Shared request/response helpers
│   ├── scheduler/      Claims pending task runs and spawns workers
│   ├── websocket/      The live connection, the stream hub, and the protocol
│   ├── turnqueue/      Serializes a conversation's turns across both paths
│   └── static/         Embedded OpenAPI and Swagger assets
│
├── architecture/       Architectural constraint tests (import boundaries)
├── e2e/                End-to-end suites that drive a built binary
│   └── cli/            CLI golden paths: real binary, temporary home, scripted model
├── mock/               Test-only in-memory stores
├── testsupport/        Test-only helpers that must not ship (JWT signing)
│   └── mockllm/        Scripted model replies over the three LLM wire protocols
└── util/               Public ID codec, prefixed IDs, workspace path resolution,
    │                   small string and time helpers
    └── secretscan/     Recognizes common secret shapes; the run trace redacts
                        what it finds, project memory refuses to persist it
```

## `evaluation/`

The evaluation and qualification system. It sits outside `internal/` because it
is contributor and operator tooling rather than product code, and it reaches no
product binary.

```text
evaluation/
├── contract/           Task, subject, trial-bundle, grader-result, and
│                       experiment types, the failure taxonomy, and the bundle
│                       directory layout
├── adapter/            Black-box execution: runs a trial through a built
│                       binary and collects its evidence. The CLI adapter drives
│                       `buildmax -p`; the worker adapter dispatches
│                       `buildmax-worker` against a control plane it serves
├── grader/             Deterministic outcome, task-supplied command, and
│                       trace/policy graders
├── runner/             Suite loading, preflight, repetition, statistics,
│                       paired comparison, and the report. Summarize turns any
│                       set of bundles into one subject's result vector, so an
│                       imported benchmark and a local run share the arithmetic
├── trace/              Reading a run's durable JSONL trace, under one set of
│                       bounds, for the adapters, the trace grader, and the
│                       Harbor importer
├── harbor/             The external Terminal-Bench 4.0 target: pinned harness,
│                       dataset, and adapter versions, the Python agent Harbor
│                       loads to run the built CLI in a task container, and the
│                       importer that files a finished job as trial bundles
└── suite/<task>/       task.json, plus state/ materialized into the trial
                        workspace and graders/ and oracle/ that never are
```

The Go packages use the standard library alone, so evaluation adds nothing to
the product's `go.mod`. Being inside the root module, they are covered by
`./make test`, `vet`, `lint`, and `govulncheck` without a second pipeline.

`evaluation/harbor/src/` is the repository's only Python, and the exception is
narrow: Harbor's custom-Agent boundary is a Python class, so an adapter for it
cannot be written in Go. It is not built, not shipped, not imported by any Go
package, and not part of any `./make check` scope; the Go core and the CLI stay
a single binary with no Python or Node. The Go files beside it pin the versions
a result depends on and hold the Python to the trial-home shape
`evaluation/adapter` writes. See
[evaluation/harbor/README.md](../../evaluation/harbor/README.md).

A trial bundle is a directory rather than a file: most of its evidence — the
JSONL trace, workspace state, produced artifacts — is already files, and keeping
one failure's evidence together is the reproduction path a failed trial owes a
contributor. Bundles are written under `.artifacts/evaluation/` and are not
committed. How to run either path, and what a task and a bundle hold, is in
[evaluation/README.md](../../evaluation/README.md); the remaining ownership
areas are in [design/evaluation-system.md](../design/evaluation-system.md).

## Dependency Direction

```text
bootstrap ──▶ interface / server / service / agentapp / infra ──▶ core
```

- `core` imports nothing from `config`, `infra`, `service`, `server`,
  `agentapp`, or `interface`. It is pure domain.
- `config` does env and file loading only; it does not import infra
  implementations.
- `infra` imports nothing from `bootstrap`, `interface`, or `server`.
- `server` imports nothing from `bootstrap`, `config`, or `interface`.
- `service` imports nothing from `agentapp`, `bootstrap`, `interface`, or `server`. A
  service is reached by a transport and never reaches back for one.
- `agentapp` imports nothing from `bootstrap`, `interface`, or `server`. Every
  surface that assembles it sits above it.
- `gorm.io` is imported only by `infra/db`. Above that boundary, "no such row"
  is `apierr.ErrNotFound`, which the store translates to.
- `mock` and `testsupport` are imported only from `_test.go` files. Neither may
  be reached from code that ships.
- `internal/tool` is not pure — it imports infra (MCP, git) as needed.

These rules are enforced by tests in `internal/architecture`. If a change trips
one, the import is the problem, not the test.

## Frontends

| Directory | Package | Notes |
|---|---|---|
| `gui/` | `@buildmax/gui` | Shared presentational React components and theme. Build output in `gui/dist/`. |
| `portal/` | — | Depends on gui via `"@buildmax/gui": "file:../gui"` |
| `desktop/frontend/` | — | Depends on gui via `file:../../gui` |

Portal and Desktop share **widgets**, not logic — data, auth, and routing are
each app's own. Both run React 19.

## Related

- [architecture/](architecture/README.md) — what each subsystem does
- [CONTRIBUTING.md](../../CONTRIBUTING.md) — build, test, and run
