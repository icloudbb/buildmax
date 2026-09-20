# Introduction

BuildMax is an open-source AI Agent runtime. You point it at a directory and it
reads, searches, and edits real files and runs real shell commands to do the
work you ask for — on your own machine, or in a private team deployment.

## What BuildMax does

At its core, BuildMax runs one loop:

```text
your prompt → the model → tool calls → run the tools → results back to the model → … → a reply
```

The tools are ordinary operations on your files and system — reading and writing
files, searching a codebase, running commands, fetching a URL. Because the agent
acts on the real directory rather than a copy, it can investigate a project, make
a change, run the tests, and report back.

You bring your own model. BuildMax talks to any OpenAI-compatible endpoint,
OpenAI, Anthropic, or a local Ollama install, so you can run it against a hosted
provider or entirely on your own hardware.

## One runtime, three surfaces

The same agent, the same tools, and the same behavior are exposed three ways:

| Surface | What it is | Best for |
|---|---|---|
| **CLI / TUI** | The `buildmax` command — one-shot answers or an interactive terminal session | Everyday local work in a single directory |
| **Desktop** | A local app with a richer UI (unsigned release download or source build) | The same local work with a graphical workbench |
| **Portal** | A web app backed by a server and background workers | A team: shared work, background runs, and results |

You can use only the local surfaces, deploy only the Portal, or use both. The
differences between them come from environment and permissions, not from
separate agent implementations.

## Two ways to run it

- **Local workbench.** The CLI/TUI or Desktop runs an agent in a directory on
  one machine. Nothing else is required — install the binary, point it at a
  provider, and go.
- **Space platform.** A server, the [Portal](portal-overview.md), and workers add
  shared work, background execution, managed models, saved results, and
  governance for a private deployment.

## A note on maturity

BuildMax is in Alpha. The local agent is broadly useful today; the team platform
is further along in some areas than others. Before you rely on a capability, check
[Supported platforms & status](support.md), which spells out exactly what is
supported, what is beta, and what is not built yet.

## Where to start

- New here and want to try it locally: [Install BuildMax](install.md), then the
  [Quickstart](quickstart.md).
- Want the mental model first: [Core concepts](concepts.md).
- Using a team deployment through the browser: [Portal overview](portal-overview.md).
