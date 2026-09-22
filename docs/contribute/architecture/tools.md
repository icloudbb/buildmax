# Tools

> **简体中文：** [阅读中文镜像](../../zh-CN/contribute/architecture/tools.md)
> **Audience:** contributors · **Status:** current
>
> User-facing tool guide: [manual/tools.md](../../../manual/tools.md)

## Purpose

The `internal/tool` package provides the runtime tools the agent can invoke.
Each tool implements the `internal/core/llm.Tool` interface and is registered
through `internal/agentapp`. Tools are designed for LLM consumption — their
results are sent back to the model as tool-role messages.

## Key Types and Interfaces

| Name | Kind | Role |
|------|------|------|
| **llm.Tool** | interface | Contract: `Name()`, `Description()`, `Parameters()`, `Execute(ctx, args)` |
| **llm.AccessDeclarer** | interface | Optional: `Access(args) Access` — does this call change anything |
| **llm.ArgChecker** | interface | Optional: `CheckArgs(args) ToolAction` — argument-level risk |
| **llm.PolicyProvider** | interface | Optional: `DefaultAction() ToolAction` — override the derived default |
| **llm.GrantScoper** | interface | Optional: `GrantScope(args) string` — narrow what one session grant covers |
| **ReadFile** | struct | Reads files under a root directory |
| **WriteFile** | struct | Creates/overwrites files under a root directory |
| **EditFile** | struct | Performs exact string replacements in files |
| **WebFetch** | struct | Fetches URLs, converts HTML to markdown |
| **WebSearch** | struct | Searches the public web through Firecrawl |
| **Bash** | struct | Runs shell commands in the workspace |
| **Glob** | struct | Lists files matching glob patterns |
| **Grep** | struct | Searches file contents by regex |
| **TodoWrite** | struct | Records the session task list |
| **NoteWrite** | struct | Records durable session notes |
| **MemoryRead** | struct | Opens the bodies behind project-memory index lines |
| **MemoryWrite** | struct | Creates, replaces, or deletes one project memory |
| **SkillTool** | struct | Loads a discovered skill's instructions (`Skill`) |
| **TaskTool** | struct | Runs a subagent of a named type (`Task`) |
| **UploadArtifact** | struct | Publishes a finished workspace file as an immutable artifact |
| **Worktree** | struct | Manages the primary run's Git worktrees and current root |
| **JobList**, **JobOutput**, **JobStop** | structs | Inspect and stop local background jobs |
| **Monitor** | struct | Starts a watched command as a local background job |
| **BrowserNavigate**, **BrowserSnapshot**, **BrowserClick**, **BrowserType**, **BrowserScreenshot**, **BrowserConsole** | structs | Verify against a real rendered page over a Go-owned headless browser |
| MCP gateway | structs | `LoadMcpTools` and `CallMcpTool` |

## Tool Inventory

### ReadFile (`Read`)

- **Parameters**: `path` (required), `offset` (optional, 1-based line), `limit` (optional, default 1000)
- **Behavior**: Reads file content with line numbers (`LINE|CONTENT` format). Supports offset/limit for large files. Path must be under the configured root.
- **Error handling**: Returns clear errors for path-outside-root, file-not-found, etc.

### WriteFile (`Write`)

- **Parameters**: `path` (required), `content` (required)
- **Behavior**: Creates or overwrites a file. Creates parent directories as needed. Path must be under root.

### EditFile (`Edit`)

- **Parameters**: `path` (required), `old_string` (required), `new_string` (required), `replace_all` (optional bool)
- **Behavior**: Performs exact string replacement in a file. By default replaces the first unique match; `replace_all` replaces all occurrences. Fails if `old_string` is not found or is ambiguous (multiple matches without `replace_all`).

### WebFetch (`WebFetch`)

- **Parameters**: `url` (required)
- **Behavior**: Fetches a URL, converts HTML to markdown. Caches results (default 15 min TTL). Optionally uses LLM to process/summarize content.

### WebSearch (`WebSearch`)

- **Parameters**: `query` (required)
- **Behavior**: Sends a bounded query to Firecrawl and returns up to five source URLs, titles, and short excerpts. Keyless calls are possible; a local `settings.yaml` key or a worker's `FIRECRAWL_API_KEY` Secret grant can authenticate. The network sandbox gates the provider host.

### Bash (`Bash`)

- **Parameters**: `command` (required), `timeout` (optional, default 120s, max 600s)
- **Behavior**: Runs a shell command in the workspace root. Returns combined stdout+stderr. Output truncated at 30k characters.

### Glob (`Glob`)

- **Parameters**: `pattern` (required)
- **Behavior**: Lists files matching a glob pattern under the root. Returns paths sorted by modification time (newest first). Patterns not starting with `**/` are auto-prefixed for recursive search.

### Grep (`Grep`)

- **Parameters**: `pattern` (required), plus optional `path`, `glob`, `type`, `output_mode`, `-A`, `-B`, `-C`, `-i`, `multiline`, `head_limit`, `offset`
- **Behavior**: Searches file contents by regex pattern. Supports output modes: `content` (matching lines with context), `files_with_matches` (file paths only), `count` (match counts). Supports glob/type filters, context lines, case-insensitive, and multiline mode.

### TodoWrite (`TodoWrite`)

- **Parameters**: `todos` (required array of {id, content, status})
- **Behavior**: Replaces the session task list. Statuses: pending, in_progress, completed; at most one entry is in_progress.

### NoteWrite (`NoteWrite`)

- **Parameters**: `notes` (required array of strings)
- **Behavior**: Replaces the session's durable notes. At most 15 entries of 200 characters; an over-limit call fails with a message naming the limit.

### MemoryRead (`MemoryRead`)

- **Parameters**: `names` (required array of slugs)
- **Behavior**: Returns those memory bodies. Names that do not exist are reported in the result rather than failing the call. The runtime records the digest of every body it returns, which is what lets a later replacement be refused.

### MemoryWrite (`MemoryWrite`)

- **Parameters**: `name` (required), `content` (required, may be empty), `description`, `type`
- **Behavior**: Creates or replaces exactly one memory, at most 20 per project with a 100-character description and a 2,000-character body. Empty `content` deletes it. Creating a name that does not exist is always accepted; replacing one requires that this run read it — an unread replacement and a stale one are refused with different messages, because one needs a read and the other a merge. No version token appears in the schema: the comparison stays inside the runtime.
- **Registration**: both are registered only on a local primary run whose session belongs to a project and whose user did not pass `--no-project-memory`. They are appended after the agent types are built, so no subagent definition can name them, and a delegate carries no index either. See [design/local-project-memory.md](../../design/local-project-memory.md) §9.

### Surface-scoped tools

These tools are registered only when the current surface provides the service
they need. A missing tool means that capability is unavailable in that run; it
is not a permission denial.

| Tool | Registered when | Parameters | Behavior |
|---|---|---|---|
| `UploadArtifact` | The surface has an artifact publisher | `path` (required); `title`, `purpose`, `share` (optional) | Publishes one finished, readable regular file inside the workspace as an immutable artifact. |
| `Worktree` | CLI or TUI primary run; never a subagent | `action` (required); `name`, `path`, `discard_changes` as required by the action | Creates, enters, leaves, lists, or removes Git worktrees and moves the session root with them. |
| `JobList` | Local background jobs are enabled (TUI or Desktop) | None | Lists jobs started by the runtime. |
| `JobOutput` | Local background jobs are enabled (TUI or Desktop) | `job_id` (required); `stream`, `cursor` (optional) | Reads a bounded, incremental slice of a job's standard output or error stream. |
| `JobStop` | Local background jobs are enabled (TUI or Desktop) | `job_id` (required) | Stops one background job started by the runtime. |
| `Monitor` | Local background jobs are enabled (TUI or Desktop); never a subagent | `command` (required); `description`, `timeout`, `persistent`, `react` (optional) | Runs a watched command under the Bash risk and sandbox rules. Its output and lifecycle are handled by the job tools. |
| `BrowserNavigate` | The run enables the browser and a system Chrome/Edge is found (CLI first; never the unattended worker or a subagent) | `url` (required) | Opens an http(s) URL in the session's headless page and reports the resulting URL, title, and status. |
| `BrowserSnapshot` | As `BrowserNavigate` | None | Returns a bounded snapshot of the current page: interactive elements with revision-scoped references plus visible text. |
| `BrowserClick` | As `BrowserNavigate` | `ref` (required) | Clicks a referenced element, rejecting a stale reference. |
| `BrowserType` | As `BrowserNavigate` | `ref`, `text` (required) | Types text into a referenced element, rejecting a stale reference. |
| `BrowserScreenshot` | As `BrowserNavigate` | None | Captures the current page as an image part (multimodal). |
| `BrowserConsole` | As `BrowserNavigate` | None | Returns recent, bounded console errors from the current page. |

Reaching a space Issue is not a tool. An Agent reads and reports on the Issue it
is working by running `buildmax issue` through `Bash` — the run bridge in a
worker, the user's login locally. An issue-linked run gets an `issue` prompt
layer that points it there; there is nothing in the tool list to discover. See
[design/agent-bridge-cli.md](../../design/agent-bridge-cli.md).

Portal background runs may add a Space instruction layer before the selected
Agent's additional system prompt. Both are stable for that run; the additional
prompt's `## Invariants` section is restated in the same block these tools render
into. See [design/context-durability.md](../../design/context-durability.md).

Both write durable session state rather than returning a formatted string and
nothing else. The state lives on `session.Session`, is reached through the
context (`agent.CtxWithNoteStore`) because the tool registry is cached per model
and shared across sessions, and is re-rendered after the message list on every
call by `agent.RenderSessionState`. It is therefore never trimmed and never
accumulates in the history. A subagent run is pointed at its own session, so it
cannot overwrite the state of the run that delegated to it. See
[design/context-durability.md](../../design/context-durability.md).

The memory tools follow the same context-carried pattern
(`agent.CtxWithMemoryStore`) over a different lifetime: the memories belong to
the project, not the session, and `agent.RenderMemoryIndex` places the index
*before* the session-state block, so what the current task decided stays closest
to generation. Only the index is resident; bodies arrive as ordinary tool
results. A subagent inherits neither — its context has the store removed by
`agent.CtxWithoutMemoryStore`.

LLM-facing names are the camelCase constants in `names.go`; the inventory above
accounts for every one. `LoadMcpTools` and `CallMcpTool` are declared separately
in `mcp_gateway.go`. These constants are the source of truth because hook
matchers and subagent `tools:` fields match against their exact strings.

## What A Tool Declares About Itself

Beyond `llm.Tool`, four optional interfaces feed the permission layer. Full
layering: [design/tool-permissions.md](../../design/tool-permissions.md).

**`Access(args)` is the one every tool should implement.** It answers whether
the call changes anything the user owns. The zero value is `AccessWrite`, so
omitting it is safe but uninformative — the tool will prompt on interactive
surfaces for no stated reason.

Two things it does *not* mean:

- **It is not a concurrency claim.** `AccessReadOnly` says the call changes
  nothing; it does not promise `Execute` is safe on several goroutines.
  `CallMcpTool` reports read-only on a third party's word, which this runtime
  cannot underwrite, which is why it declares `AccessWrite` at the tool level and
  makes its per-call decision in `CheckArgs` instead.
- **It is not the permission answer.** Permission is *derived* from it, and the
  derivation is deliberately not the tool's to make. A tool that could name its
  own action would eventually name `allow`.

**`CheckArgs` is for risk, not category.** `ReadFile` and `WriteFile` return
identical results from it — `Ask` for a sensitive path, `Allow` otherwise —
because the axis is how dangerous *this* call is, not what kind of act it is.
Note that `Allow` here means *abstain*: resolution continues to later layers.

**`DefaultAction` overrides the derivation, and needs a reason.** Three tools
implement it, all writes that must not prompt:

| Tool | Why |
|---|---|
| `TodoWrite`, `NoteWrite` | write the agent's own scratch state, not the user's files |
| `Bash` | has a sharper judgement of its own in `CheckArgs`; the category default would prompt for every `ls` |

**`GrantScope` is for tools that dispatch.** Without it, one session grant for
`CallMcpTool` would cover every tool on every configured server.

### Concurrency

The scheduler runs adjacent `AccessReadOnly` calls from one model message at the
same time, so declaring read-only carries a second obligation the type system
cannot check: **`Execute` must be safe to call from several goroutines at
once.** Read-only in the effect sense does not imply it. `WebFetch` is the case
to keep in mind — it changes nothing a user owns, and is only schedulable
because the response cache it writes is guarded by `cacheMu`. Remove that mutex
and it stays read-only and stops being safe to run in a batch.

What that means in practice: no unsynchronised package-level or struct-level
mutable state, and no assumption that a sibling is not touching the same file.
`./make test race` is the check; write the test that would catch it.

If a tool is read-only but genuinely cannot run concurrently, declare
`AccessWrite` and say why in the comment. `TodoWrite` and `NoteWrite` do exactly
that — they write only the agent's own scratch state, which is why they declare
`DefaultAction() = Allow` for permission, but that state has no lock, so the
write classification is what keeps them out of a batch.

`Access` takes the call's arguments, so a tool that does different things for
different arguments answers per call. `Task` is the one that does: it returns
`AccessReadOnly` when the requested `subagent_type` resolves to a tool set
whose every member is read-only, which is true of the built-in `explore` and
of any user-defined agent restricted to reading tools. A type that can reach
one writing tool, an unknown type, and `run_in_background` are all writes.
A sub-agent's nested loop also inherits the parent's `max_parallel_tools`, so
a read-only agent overlaps its own reads as well as its siblings'.

### Adding a tool

Declare `Access`, and read the concurrency obligation above before choosing
`AccessReadOnly`. Add `CheckArgs` if some arguments are riskier than others.
Reach for `DefaultAction` only when the tool genuinely knows better than the
category, and say why in the comment. Then add a row to the table in
[design/tool-permissions.md](../../design/tool-permissions.md) section 6 —
`internal/tool/permission_test.go` is table-driven against it and will fail
until you do.

## How It Works

1. `internal/agentapp` resolves the workspace root and builds the base tool registry.
2. Base tools include file operations, bash, glob/grep, web fetch, web search, todo, skill, and optional MCP gateway tools.
3. `internal/core/agent.RunLoop` receives a `llm.ToolRegistry`.
4. During the loop, when the LLM returns tool calls, the agent looks up each tool by name, parses JSON arguments, and calls `Execute()`.
5. Results (or errors) are appended to the active history as tool-role messages.

## Dependencies

- **Uses**: `internal/core/llm` (tool contracts), `internal/infra/llm` (for WebFetch's LLM caller), `internal/infra/mcp` (for MCP gateway)
- **Used by**: `internal/agentapp` (builds registries), `internal/core/agent` (executes tool calls)

## Notes

- All tools enforce path security — file operations must be under the configured root directory.
- Tool output is designed for LLM consumption: meaningful messages on both success and failure.
- Error messages are prefixed with `error:` by the agent when sent to the LLM.
- See also: [Agent Loop](agent-loop.md), [CLI](cli.md), [manual/tool-permissions.md](../../../manual/tool-permissions.md).
