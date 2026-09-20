# Core concepts

BuildMax is one Go agent runtime exposed through three surfaces. Understanding
which surface you are on, and which objects exist there, explains most of the
product.

## One runtime, three surfaces

| Surface | Binary / directory | What it is for |
|---|---|---|
| **CLI / TUI** | `buildmax` | One user, one local directory, one terminal |
| **Desktop** | Wails app, built from source | The same local capability with a richer UI |
| **Portal** | `buildmax-server` + `portal/` | A space: shared work, background execution, results |

All three run the **same agent loop, the same tools, and the same MCP, skill,
and subagent behavior**. Differences between them come from environment and
permissions, not from separate agent implementations. You can use only the
local surfaces, deploy only the Portal, or use both.

## Two operating profiles, two model transports

BuildMax has two product operating profiles:

- **Local Workbench:** CLI/TUI or Desktop runs an agent in a directory on one
  machine. No BuildMax Server is required.
- **Space Platform:** Server, Portal, and workers add shared work, background
  execution, managed models, results, and governance for a private deployment.

These profiles are separate from the way a model call travels. A local CLI or
Desktop may call a provider directly, or it may use models approved by a
BuildMax deployment:

| Agent execution | Model transport | Typical use |
|---|---|---|
| Local CLI/Desktop | `direct` | Personal endpoint, BYOK, or local inference |
| Local CLI/Desktop | `buildmax` | Local files and tools with enterprise-managed models |
| Worker | `buildmax` | Centrally authorized and accounted background execution |
| Worker | `direct` | A deployment that already distributes or injects provider access |

The transport is always explicit. BuildMax never falls back from a managed
model to a direct entry, because that would silently change where prompts,
source code, and tool results go.

## The agent loop

Every run, on every surface, is the same cycle:

```text
prompt → LLM → tool calls → execute tools → results back to LLM → … → reply
```

The tools are ordinary local operations: `Read`, `Write`, `Edit`, `Bash`,
`Glob`, `Grep`, `WebFetch`, `WebSearch`, `TodoWrite`, plus skills, subagents, and any tools
exposed by connected MCP servers — see [Tools](tools.md).

Two mechanisms sit around this loop and are worth knowing about early:

- **Hooks** can observe or *block* events — a prompt, a tool call, a
  compaction. See [Hooks](hooks.md).
- **The sandbox** confines `Bash` subprocesses by filesystem path and network
  domain. See [Sandbox](sandbox.md).

Every run also writes a **durable trace** — a redacted JSONL record of the LLM
calls and tool calls — inside its session's folder, under
`<BUILDMAX_HOME>/sessions/<session_id>/traces/`. See
[Sessions and traces](sessions-and-traces.md).

## Local objects

| Object | Meaning |
|---|---|
| **Workspace** | The directory the agent operates in. Defaults to the current directory; set with `--workspace`. |
| **Session** | A multi-turn conversation with its message history, saved under `<BUILDMAX_HOME>/sessions/`. Resume with `--continue` or `--resume <id>`. |
| **Project** | The local unit of work a session belongs to: one Git repository including all its worktrees, or one plain folder. It is what `--continue` and the session picker are scoped to, and what project memory belongs to. |
| **`AGENTS.md`** | Optional file at the workspace root, appended to the system prompt so the agent picks up project conventions. See [Project instructions](project-instructions.md). |

## Space objects (Portal)

The Portal adds a shared model on top of the same runtime. For deeper Portal
usage, see [Portal overview](portal-overview.md).

| Object | Meaning |
|---|---|
| **Space** | The ownership boundary. Everything below belongs to a space. Personal use is a single-member space called `My Space`. |
| **Conversation** | How a user talks to the system. This is the front door. |
| **Issue** | The user-facing unit of work — what someone actually wants done. |
| **Agent** | A saved agent definition a space can reuse. |
| **Workflow** | A reusable execution plan; currently a linear sequence of steps. Lifecycle: `draft`, `published`, `archived`. |
| **Task / TaskRun** | The low-level execution record. One task can have several runs. Users rarely see these directly. |

Space roles are `owner`, `admin`, and `member`. Uploaded files, issues,
workflows, conversations, and tasks are all space-scoped.

Owners and admins can set shared Agent instructions under **Space → Overview**.
They are sent to every background Agent run in that Space, before the selected
Agent's own instructions, and do not change the foreground Conversation
coordinator. Because the text is sent with every model call, keep it concise and
never put passwords, API keys, or other secrets in it.

Deleting an agent removes it from the space but keeps the record behind it, so
runs and history that already name it stay readable, and a workflow run in
flight finishes. An agent a published workflow still uses cannot be deleted
until that workflow is changed or archived.

Agents and workflows keep a numbered history. Every edit records the definition
it produced, along with who wrote it, and an earlier version can be restored —
which records a new version rather than erasing the ones since. A workflow run
notes the workflow version it expanded and the agent version each step ran
under, so a past run stays readable after the definitions move on.

## Two tiers

The Portal separates foreground chat from durable Agent execution:

```text
conversation ──may create──▶ task ──contains──▶ task_run
                                  └───────────▶ result / artifacts

agent / issue / workflow / API ──may create──▶ task
```

- **Conversation** is foreground chat. It can answer directly or start
  background work when coordination is useful.
- **Task** is a durable Agent execution thread. A Task can also start directly
  from an Agent, Issue, Workflow, or API without a Conversation.
- **TaskRun** is one execution turn or attempt. A worker materializes the
  space's files, runs the shared Agent runtime, writes artifacts, and records
  the result on the TaskRun.

A Conversation that started a Task may show its result as a card or link. That
projection is optional: the TaskRun result remains complete and inspectable on
its own. Direct Agent execution and the Task-thread Continue surface are the
accepted direction and are not implemented yet.

## How work actually executes

```text
Portal ──▶ server ──▶ task_run (PENDING)
                          │
                     scheduler claims it
                          │
                          ▼
                  buildmax-worker process
                     ├─ materialize space files → run home/
                     ├─ prepare AGENTS.md
                     ├─ run the shared agent runtime
                     └─ write artifacts/ → report back
```

The scheduler runs inside the server process. The worker talks to blob storage
directly rather than proxying files through the server, and reports status back
over a token-authenticated worker API.

A run in flight can be stopped: Issue Detail offers **Stop Run** while a task is
pending or running. A run nobody has picked up ends immediately. A run a worker
is executing is asked to stop, and finishes as `canceled` once that worker
stops — usually within seconds. Either way the run keeps whatever it had
produced by then, so stopping early costs you the rest of the work, not the part
already done.

A finished run can be repeated: Issue Detail offers **Retry Run** once a run is
over. The retry runs the same instructions the last run had, so recovering from
a worker that died or a model that timed out does not mean retyping them. It
counts against your space's quota like any other run, and it leaves the run it
repeats untouched — the record of what went wrong stays readable. A task that
is a workflow step cannot be retried this way: the workflow owns that step's
outcome, and re-running the workflow is what repeats it.

## Next

- Run something locally: [Quickstart](quickstart.md)
- Work with issues in the Portal: [Portal issues](portal-issues.md)
- Agents and workflows in the Portal: [Portal agents and workflows](portal-agents-workflows.md)
