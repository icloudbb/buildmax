# BuildMax

[![CI](https://github.com/icloudbb/buildmax/actions/workflows/ci.yml/badge.svg)](https://github.com/icloudbb/buildmax/actions/workflows/ci.yml)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

**An open-source Agent runtime for local work and private space deployment.**

Run BuildMax locally through CLI/TUI or Desktop with your own model endpoint,
or deploy it for a space with centrally managed models, background workers,
shared results, and governance. Both use the same Go Agent Core, so moving from
one user to an organization does not mean adopting a different agent.

> **One Agent Core. From one developer to an entire organization.**

- **[Try it locally](#try-buildmax-locally)** — one user, one directory, no
  BuildMax Server required
- **[Run it for a space](#run-buildmax-for-a-space)** — private deployment,
  shared work, managed models, and background execution
- **[Help shape it](#help-shape-buildmax)** — contribute to the runtime, local
  experience, enterprise platform, or trust boundaries

> **Status: Alpha.** Interfaces, deployment guidance, and runtime behavior may
> change quickly before a stable release. Password sign-in and operator-assisted
> account recovery are available, but login is not rate limited and there is no
> SSO or second factor. Read
> [docs/deploy/authentication.md](docs/deploy/authentication.md) before exposing
> a server, and [CONTRIBUTING.md](CONTRIBUTING.md) before contributing.

## Why BuildMax

- **Local without a control plane.** CLI/TUI and Desktop can call your own
  provider, compatible gateway, or local inference endpoint. A BuildMax Server,
  account, and space are optional.
- **Enterprise without a second agent.** A private deployment adds space
  identity, centrally approved model aliases, workers, shared results, usage,
  and audit around the same runtime used locally.
- **Portable by construction.** The core is Go, the CLI is a single binary,
  models are not tied to one vendor, and tools can be extended through MCP,
  skills, subagents, hooks, and plugins.

The user-facing surfaces have distinct jobs:

| Surface | What it is for |
|---|---|
| **CLI/TUI** | Fast local execution in a terminal, including sessions and scripting |
| **Desktop** | A local personal workbench for workspaces, sessions, and results |
| **Portal** | Space work, workflows, background runs, shared outputs, and governance |

## Try BuildMax Locally

Download a binary from [Releases](https://github.com/icloudbb/buildmax/releases),
or:

```bash
go install github.com/icloudbb/buildmax/cmd/buildmax@latest
```

Configure a model — this writes `~/.buildmax/settings.yaml`:

```bash
buildmax init --api-key sk-your-key-here
buildmax doctor
```

That sets up `openai/gpt-4o-mini` through OpenRouter. Any OpenAI-compatible
endpoint works; `buildmax init --model llama3.1 --api-url http://localhost:11434/v1`
points it at a local one instead. Omit `--api-key` to fill the key in later.
`buildmax doctor` checks the local setup without contacting a model provider.

Then run it against a directory:

```bash
buildmax -p "Summarize what this project does"   # one prompt, print the answer
buildmax                                         # interactive TUI
```

The current directory is the agent's workspace — it reads, greps, edits files,
and runs shell commands there, for real. Start in a git tree you can revert, or
in [`sample-data/`](sample-data/README.md) — fifteen throwaway datasets that
exist so you can point the agent at something and watch it work.

Full walkthrough: **[manual/quickstart.md](manual/quickstart.md)**.

## Run BuildMax For A Space

A space deployment adds the Server, Portal, and workers around the same Agent
Core. The fastest complete path is Docker Compose:

```bash
git clone https://github.com/icloudbb/buildmax.git
cd buildmax
./make compose smoke
```

The smoke uses a deterministic model, needs no provider key, and proves a full
conversation, background TaskRun, and artifact round trip. Compose is the
single-machine evaluation and contributor path. For an interactive deployment
or a private cluster, start with the
[Compose quickstart](docs/deploy/compose.md), the
[deployment overview](docs/deploy/overview.md) and the readable Kubernetes
reference under [`deployment/production/`](deployment/production/README.md).
The [support matrix](manual/support.md) states the current Alpha/Beta
boundaries; do not expose a deployment before reading the
[authentication](docs/deploy/authentication.md) and
[sandbox](manual/sandbox.md) guidance.

## Documentation

**[docs/](docs/README.md)** is the task-oriented guide. The
**[single-page index](docs/index.md)** lists every file with direct English and
Chinese links for compact browsing.

| | |
|---|---|
| [Install](manual/install.md) · [Quickstart](manual/quickstart.md) · [Support matrix](manual/support.md) · [Concepts](manual/concepts.md) | Getting started |
| [Hooks](manual/hooks.md) · [Sandbox](manual/sandbox.md) | Controlling what the agent may do |
| [Compose quickstart](docs/deploy/compose.md) · [Local kind](docs/deploy/local-kind.md) · [Deployment](docs/deploy/overview.md) · [Authentication](docs/deploy/authentication.md) | Running it for a space |
| [Configuration](docs/reference/configuration.md) · [CLI](manual/cli.md) · [Webhook](docs/reference/webhook.md) | Reference |
| [docs/ROADMAP.md](docs/ROADMAP.md) · [Design records](docs/design/README.md) · [简体中文设计文档](docs/zh-CN/design/设计文档索引.md) | Where it is going, and why |
| [Contributing](CONTRIBUTING.md) · [Support](.github/SUPPORT.md) · [Changelog](CHANGELOG.md) | Project participation and releases |

## Help Shape BuildMax

BuildMax is early enough that important runtime and product decisions are still
being made in public.

**Tests are the sharpest current need.** The codebase evolves quickly, so a pull
request that adds regression coverage for existing, currently-untested behavior
is as valuable as new capability and does not need a design discussion first:
see [Testing](docs/contribute/areas.md#testing).

Contributions are also welcome in four main areas:

- **Agent Runtime** — tool calling, context durability, models, MCP, skills,
  subagents, and traces
- **Local Experience** — CLI/TUI, Desktop, workspaces, sessions, and results
- **Enterprise Platform** — Portal, workers, managed models, deployment, and
  space governance
- **Trust And Security** — sandboxing, permissions, credentials, hooks, audit,
  and observable execution boundaries

Start with the [contribution areas](docs/contribute/areas.md), then choose a
[`good first issue`](https://github.com/icloudbb/buildmax/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22),
[`help wanted`](https://github.com/icloudbb/buildmax/issues?q=is%3Aissue+is%3Aopen+label%3A%22help+wanted%22),
or
[`agent-ready`](https://github.com/icloudbb/buildmax/issues?q=is%3Aissue+is%3Aopen+label%3A%22agent-ready%22)
task. The last label means the issue has explicit scope, acceptance criteria,
and verification commands; it does not require using an AI agent.

The complete first-contribution path takes about fifteen minutes and needs no
model API key: [Your First Pull Request](docs/contribute/first-pr.md).

## Build From Source

```bash
./make doctor     # check contributor tool versions without changing anything
./make build cli  # just the CLI — Go is the only tool this needs
./make test       # go test ./... against ./testing-sandbox
./make check go   # the Go half of what a pull request runs
./make check ci   # everything a pull request runs, except the Windows job
./make build      # everything, including the three frontends: also needs Node
./make run server # run the already-built buildmax-server
./make run portal # Portal dev server
```

The Go in `go.mod` and git are enough for `./make doctor`, `./make build cli`,
`./make test`, and `./make check go` — a complete Go contribution loop. Anything
that builds a frontend needs the Node in `.node-version` as well: `./make build`,
`./make check ci`, and `./make run portal`. On Windows use `make.bat` with the
same commands — both forward to the Go task runner in `tools/mk`. `./make help`
lists every command, grouped by what it is for, with the contributor path under
it, and `./make help <command>` — or `<command> --help` — shows one command's
arguments and examples. None of build, test, check, or lint needs a model API
key.

Two directories in the tree are fixtures rather than product code:
[`sample-data/`](sample-data/README.md) holds the datasets above — upload them
into a space workspace to give a fresh Portal deployment something to work on, or
point the CLI at one — and `evaluation/suite/` holds the evaluation tasks, each
with the state a trial starts from and the graders it is judged by. Run the CLI
tasks with `./make eval`; select worker tasks explicitly with
`./make eval --surface worker`, or both with `--surface all`.

New here? **[docs/contribute/first-pr.md](docs/contribute/first-pr.md)** is the
whole path from clone to pull request. Repository tree:
[docs/contribute/repo-layout.md](docs/contribute/repo-layout.md).

## Security

BuildMax invokes model-selected tools and shell commands. Treat every runtime
configuration as an execution boundary: dedicated credentials, least-privilege
workspace access, an explicit network policy. The [bash
sandbox](manual/sandbox.md) and [runtime hooks](manual/hooks.md) tighten
that boundary, but do not replace reviewing what a deployment is allowed to
reach. Never commit credentials.

Report vulnerabilities privately: [SECURITY.md](SECURITY.md).

## Community

Use [GitHub Discussions](https://github.com/icloudbb/buildmax/discussions)
for setup questions, early product ideas, deployment experience, and show and
tell. Confirmed bugs and contributor-ready work belong in
[Issues](https://github.com/icloudbb/buildmax/issues).

Read [CONTRIBUTING.md](CONTRIBUTING.md) for development checks, architectural
boundaries, and pull request guidance. Community participation follows the
[Code of Conduct](.github/CODE_OF_CONDUCT.md); support routes and project
decision rules are documented in [SUPPORT.md](.github/SUPPORT.md) and
[GOVERNANCE.md](.github/GOVERNANCE.md).

## License And Name

Licensed under the [Apache License 2.0](LICENSE). The BuildMax name and logo are
not granted by that license; see [TRADEMARKS.md](.github/TRADEMARKS.md).
