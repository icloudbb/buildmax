# CLI & TUI reference

The full command surface for the `buildmax` binary: subcommands, flags, the
interactive TUI, output modes for scripting, and the exit-code contract.

```text
buildmax [flags]              open the TUI, or run one prompt with -p
buildmax <command> [flags]
```

## Commands

| Command | Purpose |
|---|---|
| `buildmax` | Open the terminal UI (default) |
| `buildmax init` | Write a starter `settings.yaml` under `BUILDMAX_HOME` |
| `buildmax doctor` | Check local setup before the first run |
| `buildmax version` | Print the version |
| `buildmax login` | Log in to a BuildMax server and store credentials |
| `buildmax logout` | Clear stored credentials |
| `buildmax me` | Show current login status |
| `buildmax models` | List the models the current mode uses and their prompt destination; `--local` also lists what a local Ollama daemon holds |
| `buildmax tools status` | Inspect the tools currently available to the agent |
| `buildmax info [session-id]` | Show what a session spent and did, and what its project remembers; `--json` for the full record |
| `buildmax usage` | Sum token and cost totals across local sessions, grouped by `--group-by day\|workspace\|model`, narrowed by `--since`; `--json` for the report |
| `buildmax project list` | List the local projects and mark the ones whose locator no longer resolves |
| `buildmax project relink <project-id>` | Point an existing project, and the memory and sessions on it, at this directory |
| `buildmax project forget <name>` / `--all` | Delete one of this project's memories, or all of them; sessions are untouched |
| `buildmax sandbox status` | Print the resolved sandbox config and which layer set each value |
| `buildmax sandbox deps` | Check host-side sandbox dependencies (`bwrap`, `sandbox-exec`, `socat`) |
| `buildmax sandbox enable` / `disable` | Set `sandbox.enabled` in `settings.yaml` |
| `buildmax sandbox mode <auto_allow\|regular>` | Set `sandbox.auto_allow_bash_if_sandboxed` |
| `buildmax issue list` | List the issues you own, across every space you are in; `--status`, `--limit` |
| `buildmax issue show <id>` | Show one issue: what it asks for, its sub-issues, and recent discussion |
| `buildmax issue status <id> <status>` | Move an issue to `todo`, `in_progress`, or `done` |
| `buildmax issue comment <id>` | Post a report on an issue; body from `-m` or stdin |
| `buildmax issue start <id>` | Work a space issue in this session: the agent can read it and report back |
| `buildmax admin list` | List deployment administrators; `--all` includes revoked grants (System Administrator only) |
| `buildmax admin grant <email>` | Grant deployment-administrator authority to an existing account |
| `buildmax admin revoke <email>` | Revoke an account's administrator authority (refuses the last one) |
| `buildmax admin user list` | List accounts; `--search` filters by email substring |
| `buildmax admin user create <email>` | Create an account (grants no way in; issue a login code next) |
| `buildmax admin user login-code <email>` | Issue a single-use login code, printed once |
| `buildmax admin user disable` / `enable <email>` | Disable an account (revoking its sessions) or re-enable it |
| `buildmax admin model list` | List catalog models, enabled or not, and which is the default |
| `buildmax admin model add` | Add a model to the catalog; `--api-key` is stored encrypted and never read back |
| `buildmax admin model enable` / `disable <model_id>` | Enable or retire a catalog model |
| `buildmax plugin list` | List installed plugins, where each came from, and whether it loads |
| `buildmax plugin status [name]` | Show what a plugin contributes, its checkout or release, and what shadowed it |
| `buildmax plugin validate [path]` | Parse a plugin directory and report every problem; non-zero if any would stop it loading |
| `buildmax plugin enable` / `disable <name>` | Let a plugin load, or stop it loading without removing it |
| `buildmax plugin install <name>` | Download a release from the deployment's Marketplace and install it |
| `buildmax plugin update <name>` | Replace an installed Marketplace plugin with a newer release |
| `buildmax plugin uninstall <name>` | Remove an installed plugin |
| `buildmax plugin publish <path>` | Pack a directory and publish it (System Administrator only) |
| `buildmax plugin activations --space <space-id>` | List the exact plugin releases a Space has activated for background runs |

## Flags

| Flag | Default | Purpose |
|---|---|---|
| `-p`, `--print QUERY` | — | Run one prompt and print the reply; no TUI |
| `-r`, `--resume ID` | — | Resume a session by id (TUI or print mode) |
| `-c`, `--continue` | — | Resume this directory's most recent session |
| `--project` | off | With `--continue`, widen the search to every directory of this project and run in the one that session recorded |
| `--session-id UUID` | — | Use a specific session id; loads it if it exists, otherwise creates it |
| `--model ID\|NAME` | first entry in `settings.yaml` | Pick a model from `models:` |
| `--workspace DIR` | current directory | Directory the agent operates in |
| `--no-project-memory` | off | Neither read nor write this project's memory for this run |
| `--sandbox` | off | Require the Bash sandbox for this run without changing settings; fail if its backend is unavailable |
| `--sandbox-mode auto_allow\|regular` | configured mode | Select the approval mode for this run; requires `--sandbox` |
| `--max-iterations N` | `agent.max_iterations`, else 200 | Cap this run's model calls; range 1-5000 |
| `--append-system-prompt TEXT` | — | Text appended to this run's system prompt |
| `--append-system-prompt-file PATH` | — | Same, read from a file |
| `--agent NAME` | — | Append the body of a definition from `.buildmax/agents/` or `~/.buildmax/agents/` |
| `--output text\|json\|jsonl` | `text` | Output format for `-p` |
| `--no-stream` | off | Do not stream the reply to stdout in print mode |
| `-q`, `--quiet` | off | Suppress the stats footer in print text mode |
| `--include-deltas` | off | Include `llm_delta` events in `--output jsonl` (verbose) |
| `-v`, `--version` | — | Print the version and exit |
| `-h`, `--help` | — | Help |

`--output json` and `--output jsonl` make print mode machine-readable, which is
what you want when calling BuildMax from a script or another program.

`--sandbox` applies to both the TUI and print mode. It can enable confinement
for one run, but cannot disable it; operator `policy.yaml` remains authoritative
over both flags. See [Sandbox](sandbox.md).

`--max-iterations` also applies to both, and outranks `agent.max_iterations` in
`settings.yaml`. It is the bound on how many times one prompt may call the model
before the run stops; a run that reaches it exits `7` and reports `agent: max
iterations exceeded`. Raise it for a long unattended task, and lower it to put a
hard ceiling on what one prompt can spend. Sub-agents keep their own, smaller
cap either way.

Both carry `trace_id` and `trace_path`, naming the durable trace that run wrote.
Use them rather than looking for the newest file under
`<BUILDMAX_HOME>/sessions/<session_id>/traces/`: a session holds one trace per
run, so the newest file is a race against any other run in the same session. Both
are empty when tracing is off. See [Sessions & traces](sessions-and-traces.md).

### Adding to the system prompt

The three flags below fill one slot: free text appended to the system prompt,
after the runtime prompt and both `AGENTS.md` layers. It is additive — it never
replaces the runtime prompt — and it is sent with every model call, so unlike
anything you type in the conversation it does not fade as the context fills up.
Use it for what the agent is and what it must never do.

```bash
buildmax --append-system-prompt "You are a release engineer. Never push to main."
buildmax --append-system-prompt-file ./roles/release-engineer.md
buildmax --agent law-consultant
```

`--append-system-prompt` and `--append-system-prompt-file` are mutually
exclusive. Prefer the file when the text is long, multi-line, or private: an
argument on the command line is readable by every process on the machine.

`--agent NAME` is a convenience over the text — it loads the body of a
definition file, the same files the `Task` tool delegates to, from
`<workspace>/.buildmax/agents/` and then `~/.buildmax/agents/`. Only the body is
used: it supplies prompt text, and does **not** switch the model or restrict the
tool set the way the same definition does for a subagent. Combining it with
`--append-system-prompt` appends the ad-hoc text after the definition.

The text is capped at 8192 characters; more is rejected rather than truncated,
because this layer is sent whole on every call and has no way to degrade. If it
contains an `## Invariants` section, that section is also restated at the end of
every request, close to where the model is generating — an instruction sitting
in the system prompt still loses ground once the context fills with tool output.

Resuming a session without one of these flags keeps the text the session already
ran under. Passing one replaces it for that run onward.

### `buildmax init` flags

| Flag | Default | Purpose |
|---|---|---|
| `--api-key KEY` | a placeholder to edit | API key for the configured model |
| `--model ID` | `openai/gpt-4o-mini` | Model id to configure as the default |
| `--api-url URL` | `https://openrouter.ai/api/v1` | OpenAI-compatible base URL |
| `--name NAME` | the model id | Display name shown in the TUI and `--model` |
| `--context-window N` | provider-appropriate | Context window in tokens |
| `--ollama` | off | Configure a local Ollama model instead of a hosted provider |
| `--force` | off | Replace an existing `settings.yaml` |

The file is written with mode `600` because it holds an API key. Without
`--force`, an existing file is left untouched and the command exits `2`.

`--ollama` writes an entry with no `api_key` at all, pointed at
`http://localhost:11434`. When the daemon is running it configures a model that
is already pulled and reads that model's context window; when it is not, the
file is still written and the output names what to start and what to pull. See
[Models & modes](models-and-modes.md).

### `buildmax plugin`

A plugin is a directory under `<BUILDMAX_HOME>/plugins` holding `skills/`,
`agents/`, `mcp.json`, and `hooks.yaml` — the same content a workspace
`.buildmax/` directory supports. Clone one there and it loads on the next run.
See [Plugins](plugins.md).

`plugin status` accepts `--workspace` and `--fetch`. Without `--fetch` nothing
here touches the network, and a checkout's drift from its upstream is only as
current as the last fetch was; `--fetch` contacts the remote to refresh it.

`disable` records a flag rather than moving anything, so a Git working tree is
never touched. A run already in flight keeps the plugins it started with.

`install` and `update` take `--version` for an exact release, and
`--allow-yanked` for one that was withdrawn. Without `--version` they take the
newest release that is not a prerelease, not withdrawn, and not newer than this
build supports. Both refuse to replace a Git checkout, and `uninstall` refuses
to delete one without `--force`: a working tree can hold work that exists
nowhere else.

`publish` takes the version from the directory's own `plugin.yaml` and needs a
System Administrator grant on the server you are signed in to.

`activations` is read-only and requires a login. It reports the Space's curation
mode and each activated release; activation changes stay in Portal, where they
are visible with the Space's other shared automation and audit history.

### `buildmax issue`

`buildmax issue list` is the receiving end of space work: it shows what you
own, so you can start on it here instead of reading a board in a browser. Sign
in with `buildmax login` first.

```bash
buildmax issue list                    # everything you own
buildmax issue list --status todo      # only what has not been started
```

One row per issue, with the space it belongs to. A space that cannot be read is
reported as a warning and the rest of the inbox still prints.

Managing the work — creating issues, assigning them, changing status, splitting
them up — stays in Portal. This command reads.

Read one before starting:

```bash
buildmax issue show i_7Kq2...
```

Post a report on one, signed in as you:

```bash
buildmax issue comment i_7Kq2... -m "adapter written and tested"
git log --oneline | buildmax issue comment i_7Kq2...   # body from stdin
```

The comment is recorded as a **local agent report** (see below): the same
statement the in-process report tool makes, reachable by any agent that can run
a command. Status, owner, and sub-issues stay yours to change with
`buildmax issue status`.

To work on one, start a session scoped to it:

```bash
buildmax issue start i_7Kq2...            # TUI, working that issue
buildmax issue start i_7Kq2... -p "..."   # one print-mode run
```

The agent gains two tools: `GetIssue` reads the issue, its sub-issues, and
recent discussion; `ReportToIssue` posts a short report on the thread, at most
three times in a run. Neither can change the issue's status, owner, executor,
or sub-issues — the agent says what it believes should happen and a person
decides.

A report from your machine is recorded as a **local agent report**, attributed
to you, and Portal shows it as reported rather than said. It is not the same as
a comment from a run the deployment scheduled: nothing here was queued, counted
against quota, or traced. `issue start` scopes one run; it is not remembered.

Before the first model call the session prints which server, space, and issue it
is working, and where prompts go — space work crossing to a personal model
should be visible before it crosses, not inferable afterwards.

When you are done, say so:

```bash
buildmax issue status i_7Kq2... done
```

That is yours to run, not the agent's. Status is what the space plans around and
`done` means a person accepted the work, so the agent can say it believes the
work is finished and you decide. The change carries the version the issue was
read at; if someone else moved it meanwhile, this refuses instead of
overwriting them. See [Portal issues](portal-issues.md).

### `buildmax admin`

`buildmax admin` manages who can operate a deployment, as the signed-in
administrator, over the same API the Portal administration area uses. Sign in
with `buildmax login` first; a caller without a system grant is refused by the
server.

```bash
buildmax admin list                 # active administrators
buildmax admin list --all           # include revoked grants
buildmax admin grant alex@corp.com  # give an existing account admin authority
buildmax admin revoke alex@corp.com # take it away
```

Accounts are named by email; `grant` and `revoke` resolve the address to one
account and refuse an ambiguous or unknown one rather than act on a guess.
Revoking the deployment's last administrator is refused here — that is a
deliberate, database-authorized act, done with `buildmax-server admin revoke`
on the machine that runs the server. Creating the first administrator and
recovering a deployment that has lost every administrator likewise stay in
`buildmax-server admin`, which reaches the database directly; `buildmax admin`
is the routine, authenticated peer, not the break-glass path.

`buildmax admin user` manages accounts over the same API — the authenticated
peer of `buildmax-server user`, which reaches the database directly for bootstrap
and recovery:

```bash
buildmax admin user list --search corp.com          # accounts, filtered by email
buildmax admin user create alex@corp.com            # no way in yet
buildmax admin user login-code alex@corp.com        # printed once
buildmax admin user disable alex@corp.com           # also revokes their sessions
buildmax admin user enable  alex@corp.com
```

Creating an account and issuing a login code are separate steps on purpose: a
new account cannot sign in until a code is issued, and the code is shown once
and recoverable nowhere. There is no `set-password` here; issuing a login code
lets the person choose their own password and is the safer equivalent.

`buildmax admin model` manages the model catalog over the same API:

```bash
buildmax admin model list                              # every catalog model
buildmax admin model add --name "Sonnet" --api-url https://api.example.com/v1 \
  --model vendor/sonnet --api-key sk-… --context-window 200000
buildmax admin model disable lm_7Kq2                   # retire one, by id
buildmax admin model enable  lm_7Kq2                   # bring it back
```

The `--api-key` travels in the request body and is stored encrypted at rest; no
read returns it, and a deployment with no encryption key configured refuses a
model that carries one. `add` takes the same fields as `buildmax-server model
add`; run `buildmax admin model add --help` for the full set.

### `buildmax doctor`

`doctor` checks the local setup without contacting an LLM provider:

- `BUILDMAX_HOME` and `settings.yaml`
- configured models and placeholder API keys
- current workspace and git availability
- the local project this directory belongs to and the state of its memory
- sandbox dependencies when `sandbox.enabled` is set

`buildmax doctor --workspace DIR` checks the specified workspace directory.
The default is `""`.

It exits `2` when a required first-run prerequisite is missing. Warnings, such
as running outside a git branch or leaving the local sandbox disabled, are
reported but do not make the command fail. See [Troubleshooting](troubleshooting.md).

### `buildmax project`

A project is the local unit of work a session belongs to: one Git repository
including every one of its worktrees, or one plain folder. It is what
`--continue`, the session picker, and
[project memory](project-instructions.md) are scoped to.

A project is found again by a locator — a repository's common Git directory, or
a folder's path. Moving a repository leaves the old project unreachable, and the
next run there registers a new, empty one; that run says so and names the
projects that no longer resolve, because otherwise the duplicate looks like the
feature working.

```bash
buildmax project list                    # (missing) marks an unresolved locator
buildmax project relink <project-id>     # point it at the current directory
buildmax project relink <id> --workspace ../moved

buildmax project forget rejected-sse-transport
buildmax project forget --all             # sessions are untouched
```

Relinking names the project explicitly. The alternative is a heuristic, and a
heuristic that joined two memory domains would leave no trace of having done so.

### `buildmax info`

`info` reports one session and the project it belongs to: what the session
spent, what it did, where its context went, and what the project remembers. With
no argument it reads the most recent session by creation time.

The two halves have different owners and different lifetimes. The statistics are
the session's and end with it; the memories are the project's and every session
of that project sees them. A session that belongs to no project — one written
before projects existed, or by a worker — prints no memory section rather than
an empty one.

It reads two records, and says which is which because they answer different
questions:

- The **session file** holds tokens and cost. They accumulated turn by turn at
  the rates in force for each, so nothing recomputes them on read. It is also
  where the per-tool output bytes come from — the number that says which tool
  is filling the context window.
- The **run traces** hold everything time-shaped: run count, wall clock, the
  model-versus-tools split, per-tool duration, denials, tool calls that could
  not complete, and how much of the run a delegation did.

Where a trace is missing — tracing failed open, or the run was killed before it
wrote an end record — the affected lines say so instead of reporting zero, and
a warning at the bottom names what the totals do not cover. The same applies to
money: a session no model priced says `not priced` rather than `0.000000`, and
a saving is reported only where caching actually saved.

The memory half lists each memory's name, type, and description — which is
exactly what a run carries on every model call — with the index size against its
budget, because that pair is what you prune against rather than the count. It
does not print bodies: twenty of them is not a listing. The directory is printed
so you can open one, and files that could not be parsed are named, since such a
memory is silently absent from every run until it is repaired.

`--json` emits the whole record under `stats` and `project_memory`, including
the tools the table truncates and, under `stats.turns`, the per-turn breakdown
recorded in the session file — one entry per metered turn, summing to the
totals. The table surfaces read the totals, not this journal. Bodies stay out of
it for the same reason they stay out of the table.

In the TUI, `/info` shows both halves as tabs — `tab` and `←`/`→` switch, and on
the memory tab `enter` opens a body. The statistics tab folds the **live**
session rather than the file — a session is persisted after each assistant
reply, so reading it back would answer about the turn before the one you are
looking at — and both halves are a snapshot taken when the panel opens, not live
counters.

### `buildmax usage`

Where `info` answers for one session, `usage` answers for many: what a week of
sessions cost, in one figure instead of one session at a time. It reads the
session index — the same projection the picker lists from — so the report costs
one file read no matter how many sessions it covers, and never re-opens a
session to build a number.

Group the report to ask a different question of the same sessions:

- `--group-by day` (the default) — when the spend happened, one row per calendar
  day in your local zone, oldest first.
- `--group-by workspace` — where it happened, heaviest first.
- `--group-by model` — which model it went to, by each session's selected model,
  heaviest first.

`--since` narrows the report to sessions that **started** within a window: a Go
duration (`72h`), a day or week count (`7d`, `2w`), or a date (`2026-09-01`). A
session belongs to its start day, so the same instant scopes the window and keys
the `day` grouping.

The numbers are each session's own running totals, accumulated at the rates in
force when they ran; nothing is repriced on read. A session no model priced
still counts its tokens, and the row and total say `not priced` or `(partial)`
and warn that the money understates the real spend, rather than showing a zero
that reads as free. `--json` emits the grouped report and its total.

Only your own sessions are counted; a subagent's private session never reaches
the index, and worker runs keep their own run-scoped home, so their spend is the
managed ledger's concern rather than this local view's.

## TUI slash commands

Typed into the input line:

| Command | What it does |
|---|---|
| `/model` | Opens the model picker (from `settings.yaml`) |
| `/compact` | Summarize the conversation so far and continue from the summary |
| `/rewind` | Take one of your prompts back to edit and send again |
| `/fork` | Branch a new session off an earlier message |
| `/sessions` | Opens the session picker |
| `/tools` | Lists the tools available this run |
| `/skills` | Lists the discovered skills |
| `/mcp` | Lists connected MCP servers and their status |
| `/diff` | Shows the working-tree diff for the workspace |
| `/info` | Two tabs: this session's spend, context use, and heaviest tools; and what this project remembers, with `enter` to read a memory |
| `/tasks` | Lists background jobs: state, age, command; `s` stops the selected one |
| `/worktree` | Lists this repository's worktrees, which session is in each, and what each holds uncommitted; `d` removes the selected one after a confirm |
| `/agents` | Lists the agent types the `Task` tool can delegate to |
| `/plugins` | Lists installed plugins, their state, and what each contributes |

Slash commands are unavailable while the agent is running.

## Typing while the agent works

The input stays open during a run. `Enter` queues what you typed; the transcript
shows it as `⏸ queued #n` and the footer counts what is waiting. The agent picks
it up at its next step — usually as soon as the tool it is running finishes,
without waiting for the whole run to end — and the transcript then shows it as a
sent message. Up to ten messages can wait. `Esc` clears the input, or takes back
the last queued message when the input is already empty.

## Examples

```bash
# first run: write ~/.buildmax/settings.yaml with a working key
buildmax init --api-key sk-your-key-here
buildmax doctor

# or run against a local model, with no key and no network
buildmax init --ollama
buildmax models --local

# one-shot question, quiet, in another directory
buildmax -p "list the exported symbols" --workspace ../lib -q

# machine-readable run for a script
buildmax -p "run the tests and summarize failures" --output json

# continue where you left off in this directory
buildmax --continue

# pick a bigger model for one run
buildmax --model gpt-4o -p "review this diff for race conditions"
```

## Exit codes

Print mode returns a non-zero exit code when the run fails, so it composes with
shell scripts and CI steps. The codes are a stable contract:

| Code | Meaning |
|---|---|
| `0` | The run finished |
| `1` | An error with no more specific code |
| `2` | Bad flag, or missing configuration — no model configured, for instance |
| `3` | A tool was blocked by the configured policy |
| `4` | The model or the agent run failed: an unreachable provider, a refused credential, a run that could not continue |
| `5` | Reserved for tool errors; nothing returns it yet |
| `6` | Cancelled — `Ctrl+C`, or the context ended |
| `7` | The run reached its iteration cap — see `--max-iterations` |

`--output json` and `--output jsonl` carry the same fact as an `error` object
with a `kind` (`usage`, `policy_denied`, `model_error`, `tool_error`,
`cancelled`, `iteration_cap`, or `error`) and a message, so a caller does not
have to map the number back.

`7` is deliberately not `4`. A model error is a fault worth retrying; an
exhausted iteration budget is an answer, and retrying it pays for the same cap
again. Whatever the run wrote before it stopped is still on disk.

## Related

- [Sessions & traces](sessions-and-traces.md) — resuming, rewinding, and run traces
- [Quickstart](quickstart.md) — first run
