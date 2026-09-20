# Plugins

A plugin is a directory of skills, subagents, MCP servers, hooks, or an
experimental app connector, packaged so
it can be shared. Checking `.buildmax/` into a repository gives a workflow to
everyone who runs the agent *there*. A plugin gives it to everyone who installs
it, in every workspace.

Skills, subagents, MCP servers, and hooks retain their workspace formats. The
connector has its own bounded declaration; see [App connections](app-connections.md).

## Install one

Clone it into the plugins directory:

```bash
git clone git@code.example.com:agents/code-review.git \
  ~/.buildmax/plugins/code-review
```

That is the whole installation. The next run picks it up; a run already in
flight keeps the plugins it started with.

```bash
buildmax plugin list
buildmax plugin status code-review
```

BuildMax never pulls for you. Git owns branches, credentials, and merge
conflicts, and a plugin's working tree stays yours to manage:

```bash
git -C ~/.buildmax/plugins/code-review pull
```

`buildmax plugin status --fetch` contacts each checkout's remote and reports how
far behind it is. Without that flag nothing here touches the network, so the
drift it reports is only as current as your last fetch.

To stop a plugin loading without deleting it:

```bash
buildmax plugin disable code-review
buildmax plugin enable code-review
```

## Install one from your deployment

If your BuildMax server publishes a plugin catalog, installing is a name rather
than a URL:

```bash
buildmax login                    # once, if you have not
buildmax plugin install code-review
```

You get the newest release that is not a prerelease, not withdrawn, and not
newer than your build supports. `--version` takes an exact one — including a
prerelease, which the default deliberately skips.

Before the bytes land, BuildMax checks them twice: the digest the server sent
against the one the catalog published, and then the bytes themselves against
that same record. The first catches a server serving something else; the second
catches a download cut short under a header that described the whole thing.

A withdrawn release still installs with `--allow-yanked`, and says why it was
withdrawn when you ask for it without. Yanking takes a release out of the
default choice; it does not delete it, and a copy you already have keeps
working.

```bash
buildmax plugin update code-review
buildmax plugin uninstall code-review
```

Installing never replaces a Git checkout, and `uninstall` will not delete one
without `--force`. A working tree can hold work that exists nowhere else, and
neither command is one you expect to lose it to.

## Use one in Space background runs

Portal's Space Plugins page controls which Marketplace releases are available to
that Space's background Agents. A Space can curate an explicit activation list or
use open catalog mode. In either mode, an Agent loads only the plugins its
definition names; naming none loads none. The release version belongs to the
Space activation, not to each Agent.

The Agent definition API accepts plugin catalog names. The Portal Agent modal
does not expose that field yet. To inspect the Space side from a terminal:

```bash
buildmax plugin activations --space <space-id>
```

When a worker claims a run, the server resolves and records exact release pins.
The worker downloads only those packages with its run credential and
materializes them before the Agent runtime starts, so changing an activation
cannot alter a run already in flight.

This first Space slice accepts plugins that contribute skills and subagents.
Releases containing executable hooks or MCP servers cannot be activated yet,
and BuildMax does not yet deliver Space secrets to plugins.
An experimental `connector.yaml` is used only by the local CLI. A worker has
no interactive OAuth connection or local user's app credential.

## Publish one

Publishing needs a System Administrator grant on the server you are signed in
to:

```bash
buildmax plugin publish ./code-review
```

The version comes from the directory's own `plugin.yaml`, so a release is a
line in a commit rather than an argument in one person's shell history.
Publishing a version that already exists is refused even for identical bytes: a
release is what somebody reviewed and what somebody else downloaded.

The server does not take your word for any of it. It hashes what it receives,
unpacks it, and reads it with the same parsers a run uses — so a package that
would not load cannot be published. If the directory is a Git checkout, the
remote, commit, and whether the tree was dirty travel with it and are recorded
as the publisher's claim beside a digest the server calculated itself.

## What a plugin contains

```text
code-review/
├── plugin.yaml
├── README.md
├── skills/          one directory per skill, each with SKILL.md
├── agents/          one markdown file per subagent
├── mcp.json         MCP server definitions
├── connector.yaml   experimental local app connection
├── hooks.yaml       hook definitions
└── hooks/           scripts hooks.yaml refers to
```

| Path | Behaves exactly like | Documented in |
|---|---|---|
| `skills/<name>/SKILL.md` | `<workspace>/.buildmax/skills/` | [Skills & subagents](skills-and-subagents.md) |
| `agents/<name>.md` | `<workspace>/.buildmax/agents/` | [Skills & subagents](skills-and-subagents.md) |
| `mcp.json` | `<workspace>/.buildmax/mcp.json` | [MCP servers](mcp.md) |
| `hooks.yaml` | `<workspace>/.buildmax/hooks.yaml` | [Hooks](hooks.md) |
| `connector.yaml` | Local connector CLI declaration | [App connections](app-connections.md) |

A plugin may ship any subset. A skill-only plugin is normal; so is one that
contributes only MCP configuration. There is **no** nested `.buildmax/`
directory — the content sits at the plugin root.

Anything else in the directory is ignored, and `buildmax plugin validate`
says so, because a misplaced directory that loads nothing should not look like
a working feature.

## `plugin.yaml`

Only `name` is required:

```yaml
name: code-review
version: 1.2.0
description: Company code review skills and agents.

display_name: Code Review
homepage: https://code.example.com/agents/code-review
maintainer: Platform Space <platform@example.com>
license: Apache-2.0

min_buildmax_version: 0.9.0

env:
  GITHUB_TOKEN:
    description: Token the github MCP server authenticates with.
  REVIEW_WEBHOOK_URL:
    description: Where the post_tool_use hook posts review results.
    required: false
```

| Field | Meaning |
|---|---|
| `name` | The plugin's identity. Lowercase words joined by single hyphens |
| `version` | Semantic version. Optional until you publish; no leading `v`, no ranges |
| `description` | One line saying what it is for |
| `display_name` | Title for lists and panels. Defaults to `name` |
| `homepage`, `maintainer`, `license` | Shown, never acted on |
| `min_buildmax_version` | Oldest BuildMax this plugin works on. A single lower bound |
| `env` | Environment variables the plugin expects, keyed by name |

The manifest's `name` is the identity, not the directory. Cloning into a
differently named directory works and says so; two directories claiming one
name is an error and neither loads.

An `env` entry declares a **name and prose only**. There is no place to put a
value, because a manifest is checked into a repository and a value there is
a leaked secret. `buildmax plugin status` reports which declared variables are
unset — the usual reason a plugin looks installed and does nothing.

Unknown fields load with a warning, so a newer plugin still runs on an older
BuildMax — and a misspelled `descripton:` still gets pointed out.

## Reaching files the plugin ships

A hook or MCP server usually needs to run something bundled with the plugin.
One variable resolves to the directory holding `plugin.yaml`:

```yaml
# hooks.yaml
post_tool_use:
  - type: command
    matcher: "Write|Edit"
    command: "${BUILDMAX_PLUGIN_ROOT}/hooks/format.sh"
```

```json
{"mcpServers": {"review": {
  "type": "stdio",
  "command": "${BUILDMAX_PLUGIN_ROOT}/bin/review-server"
}}}
```

Each plugin's files are expanded with its own root, so two plugins can write the
same line and each get their own directory. BuildMax supplies the value; a
process environment variable of the same name cannot redirect it elsewhere.

`${WORKSPACE_ROOT}` keeps its meaning in `mcp.json`: the workspace the agent is
working on, not the plugin.

In `hooks.yaml` this is the only variable BuildMax substitutes. Every other
`$VAR` stays literal for the shell — or, in an HTTP hook's headers, for
`allowed_env` at call time.

## Which definition wins

Your own configuration outranks a plugin, so installing one can never quietly
replace something you wrote:

```text
workspace .buildmax  >  <BUILDMAX_HOME>  >  plugins
```

| Content | How the layers combine |
|---|---|
| Skills, subagents | First layer to define a name wins |
| MCP servers | Merged; a later layer replaces a server id |
| Hooks | Additive — global, then plugins, then workspace, all run |

Two plugins are the same layer, so nothing can rank them. A skill, subagent, or
MCP server contributed by two plugins **loads from neither**, and both are named
so you can decide which to keep. Alphabetical order exists to make loading
deterministic, not to pick a winner.

When your workspace overrides part of a plugin, `buildmax plugin status` says so
under `shadowed:` rather than showing the plugin as fully active.

## When your deployment restricts sources

An operator can require that only certain kinds of plugin load on a machine
they manage:

```yaml
# <BUILDMAX_HOME>/policy.yaml
plugins:
  allowed_sources: ["marketplace"]
```

`buildmax plugin list` says when a restriction is in force, and a plugin it
excludes shows as `refused` with the reason rather than disappearing. A
directory whose provenance cannot be established — a Marketplace copy whose
record was lost, say — does not pass a policy that names one: unknown is not
the source the operator asked for.

This constrains where plugins come from, not what they may do. What a plugin
that does load may do is [Tool permissions](tool-permissions.md) and
[Sandbox](sandbox.md).

## Before you trust one

A plugin runs with the same reach you have. Its skills and subagents are
instructions that can cause tool use; its MCP servers can start processes with
your credentials; its hooks can execute local programs and reach the network.

Installing one is like running any other code from that source. Read
`buildmax plugin status` to see what it contributes and what it wants, and treat
the repository behind it the way you would treat any dependency.

A plugin cannot grant itself permission. Tool permissions, hook gating, the
sandbox, and sensitive-path checks apply to a plugin's contributions exactly
as they apply to your own configuration — see
[Tool permissions](tool-permissions.md) and [Sandbox](sandbox.md).

## Writing one

Develop against a checkout in place; there is nothing to build or package:

```bash
git clone git@code.example.com:agents/code-review.git \
  ~/.buildmax/plugins/code-review
$EDITOR ~/.buildmax/plugins/code-review/skills/review/SKILL.md
buildmax plugin validate ~/.buildmax/plugins/code-review
```

`validate` takes any path, so you can check a repository before installing
it anywhere. It parses the manifest and every payload, reports each problem
against the line it came from, and exits non-zero if anything would stop the
plugin loading.

Every run records which plugins it loaded, and for a checkout its commit and
whether the working tree was dirty — see
[Sessions & traces](sessions-and-traces.md).

## Related

- [CLI](cli.md) — every `buildmax plugin` command
- [Skills & subagents](skills-and-subagents.md) — writing the content
- [MCP servers](mcp.md) · [Hooks](hooks.md) — the other two kinds
