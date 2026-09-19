# BuildMax Documentation

> **简体中文：** [阅读中文镜像](zh-CN/README.md)

Organized by what you are trying to do.

## Use It

The end-user manual lives in [`manual/`](../manual) — installation, the quickstart,
core concepts, every CLI command, the built-in tools, skills and subagents, MCP,
plugins, hooks, the sandbox, tool permissions, and the Portal walkthrough. It
ships inside the Portal image and is served in-app under **Help**;
[`manual/manifest.json`](../manual/manifest.json) is its table of contents.

| | |
|---|---|
| [Introduction](../manual/introduction.md) | What BuildMax is, and the three surfaces |
| [Install](../manual/install.md) | Get the binaries |
| [Quickstart](../manual/quickstart.md) | First agent run, in five minutes |
| [Support matrix](../manual/support.md) | Supported platforms, surfaces, deployment paths, and non-goals |
| [../sample-data/](../sample-data/README.md) | Fifteen throwaway datasets — upload them into a space workspace, or point the CLI at one |

## Run It For A Space

| | |
|---|---|
| [deploy/compose.md](deploy/compose.md) | A space deployment on one machine, in about five minutes |
| [deploy/overview.md](deploy/overview.md) | Topology, requirements, configuration, containers |
| [deploy/authentication.md](deploy/authentication.md) | **Read before exposing a server** — accounts, login codes, what is missing |
| [deploy/local-kind.md](deploy/local-kind.md) | One-command local cluster and Kubernetes Job smoke |
| [deploy/digitalocean.md](deploy/digitalocean.md) | Disposable external DOKS and MySQL for beta qualification |
| [deploy/beta-readiness.md](deploy/beta-readiness.md) | Qualify a pinned private-deployment candidate and record the evidence |

## Look It Up

| | |
|---|---|
| [reference/configuration.md](reference/configuration.md) | Every config file field and environment variable |
| [CLI reference](../manual/cli.md) | Commands, flags, slash commands (in the user manual) |
| [reference/webhook.md](reference/webhook.md) | Triggering runs from external systems |

The HTTP API describes itself: `GET /openapi.json`, browsable at `/swagger/`.

## Change It

| | |
|---|---|
| [../CONTRIBUTING.md](../CONTRIBUTING.md) | Prerequisites, build, test, code boundaries, pull requests |
| [contribute/areas.md](contribute/areas.md) | Pick a contribution area and find work that matches your experience |
| [contribute/first-pr.md](contribute/first-pr.md) | Clone to pull request, start to finish, no API key needed |
| [contribute/conventions.md](contribute/conventions.md) | Naming, IDs, tool output, commit messages, changelog entries |
| [contribute/repo-layout.md](contribute/repo-layout.md) | The repository tree and dependency direction |
| [contribute/testing.md](contribute/testing.md) | Which suite to run for a change, what it needs, and what CI runs when |
| [contribute/exploratory-testing.md](contribute/exploratory-testing.md) | Explore user journeys autonomously and leave reproducible findings beyond fixed test cases |
| [contribute/agent-autonomous-e2e-assessment.md](contribute/agent-autonomous-e2e-assessment.md) | The 2026-09-09 field assessment of autonomous verification and the remediation that followed |
| [evaluation/README.md](../evaluation/README.md) | How to measure a build: the local suite, the external benchmark, and what a bundle holds |
| [changelog/README.md](changelog/README.md) | How to add a changelog entry, and how a release folds them |
| [contribute/architecture/](contribute/architecture/README.md) | How each subsystem works today |
| [contribute/documentation.md](contribute/documentation.md) | Documentation conventions |
| [contribute/dependency-licenses.md](contribute/dependency-licenses.md) | License audit and how to re-run it |
| [contribute/releasing.md](contribute/releasing.md) | Versioning, publishing, verification, and release recovery |

## Why It Is Like This

| | |
|---|---|
| [current-state.md](current-state.md) | Code-based implementation and readiness assessment |
| [ROADMAP.md](ROADMAP.md) | Active priorities and sequencing |
| [backlog/](backlog/README.md) | The maintainer-and-Agent planning loop and its prioritized, ready-to-execute tasks |
| [design/](design/README.md) · [简体中文](zh-CN/design/设计文档索引.md) | Design rationale browsed by domain and marked by lifecycle |
| [../SECURITY.md](../SECURITY.md) | Vulnerability disclosure and operator responsibilities |

## Explore Future Directions

| | |
|---|---|
| [proposals/](proposals/README.md) | Early cross-cutting directions that are open for discussion, not roadmap commitments |

## Conventions

Every document opens with its audience and status, so you can tell in one line
whether it can be trusted:

```markdown
> **Audience:** operators · **Status:** current
```

There is no archive directory — retired documents are deleted, and git history
keeps them. The rules for writing and retiring documentation are in
[contribute/documentation.md](contribute/documentation.md).
