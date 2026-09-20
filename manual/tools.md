# Tools

Tools are what turn a language model into an agent. Every run gets the same
built-in set; MCP servers, skills, and subagents add to it.

Tool names are what you write in a hook `matcher` and what shows up in `/tools`,
so they are worth knowing exactly.

## Built-in tools

| Name | Does | Key arguments |
|---|---|---|
| `Read` | Read a file, with line numbers | `file_path`, `offset`, `limit` (default 1000 lines) |
| `Write` | Create or overwrite a file, creating parent directories | `file_path`, `content` |
| `Edit` | Exact string replacement in a file | `file_path`, `old_string`, `new_string`, `replace_all` |
| `Glob` | List files matching a pattern, newest first | `pattern`, `path` |
| `Grep` | Regex search over file contents | `pattern`, `path`, `glob`, `type`, `output_mode`, `before_context`, `after_context`, `context`, `case_insensitive`, `line_numbers`, `multiline`, `head_limit`, `offset` |
| `Bash` | Run a shell command in the workspace | `command`, `timeout` (milliseconds; default 120000, max 600000), `dangerously_disable_sandbox`, `run_in_background` and `deliver_result` (TUI and Desktop) |
| `WebFetch` | Fetch a URL as markdown, optionally summarized by the model | `url`, `prompt` |
| `WebSearch` | Search the public web for source URLs and excerpts | `query` |
| `TodoWrite` | Track multi-step progress | `todos[]` of `{content, status, active_form}` |
| `NoteWrite` | Keep durable notes that survive compaction | `notes[]` of strings |
| `Skill` | Load a skill's instructions | `skill`, `args` |
| `Task` | Delegate to a subagent | `description`, `prompt`, `subagent_type`, `run_in_background` and `deliver_result` (TUI and Desktop), `worktree` (TUI) |
| `UploadArtifact` | Publish one finished file as a durable artifact | `path`, `title`, `purpose`, `share` |
| `JobList` | List background jobs: ID, kind, state, age, command | — |
| `JobOutput` | Read a background job's status and output incrementally | `job_id`, `stream`, `cursor` |
| `JobStop` | Stop a background job (kills the whole process tree) | `job_id` |
| `Monitor` | Watch logs, files, or CI: each stdout line becomes a bounded event | `command`, `description`, `timeout`, `persistent`, `react` |
| `Worktree` | Create, enter, leave, list, or remove a Git worktree, moving the session into it | `action`, `name`, `path`, `discard_changes` |
| `LoadMcpTools` / `CallMcpTool` | Discover and invoke MCP server tools | see [MCP](mcp.md) |
| `MemoryRead` | Open the bodies of project memories. Available on a local run with project memory. | `names` |
| `MemoryWrite` | Create or replace a project memory. Available on a local run with project memory. | `name`, `description`, `type`, `content`, `verified_at` |

Run `/tools` in the TUI to see the set active for the current run — it varies
with what is configured.

Reading and reporting on a space issue is not a tool. When a run is working an
issue — a worker run started from one, or a local `buildmax issue start`
session — the agent uses the `buildmax issue` commands through `Bash` instead:
`buildmax issue show` to read it and `buildmax issue comment` to post a report.
See [cli.md](cli.md).

`UploadArtifact` is the one built-in that is not always there. It needs a
BuildMax server to publish to, so it appears when you are logged in
(`buildmax login`) and when a worker runs a task for a space. A local session
running straight against a model provider has no artifact store, and rather
than offering a tool that could only fail, the agent is not given one — it
keeps writing files where it already does.

The `Job` tools and `Monitor` follow the same rule. Background jobs need a
live interactive process to own them, so `run_in_background` on `Bash` and
`Task`, `Monitor`, and the three `Job` tools exist only in the TUI and
Desktop — not in print mode (`buildmax -p`), eval, or worker runs, and never
inside a subagent. `Monitor` runs its command under exactly Bash's risk,
permission, and sandbox rules; its lines are rate-limited, truncated, and
delivered as untrusted observations, and dropped lines are counted rather
than silently discarded.
Backgrounding changes when a call returns, not what it is allowed to do: the
permission check runs before the job detaches, and a background subagent that
would need an approval is denied, exactly as a foreground subagent is. A
background job shares the workspace with the conversation — avoid delegating
edits that would race yours — and quitting the application stops every job it
started. A background subagent's final reply appears in `JobOutput` when it
completes.

## Worktrees

Ask for one in the conversation — "open a worktree and do the refactor there" —
and the agent creates it, moves into it, and works there. Nothing needs a path
prefix afterwards: `Read`, `Edit`, `Grep`, and `Bash` all resolve inside the
worktree, `/diff` shows that tree, and the footer shows which one you are in.
Committing and pushing from it are ordinary Bash.

Worktrees live in `.buildmax/worktrees/<name>` on a `worktree/<name>` branch,
created from the current `HEAD`. The directory is excluded through your clone's
`.git/info/exclude`, so it never shows up in `git status` and your `.gitignore`
is left alone. Uncommitted changes do not come along — the agent lists what
stayed behind rather than moving it, because a stash is shared with every other
session in the repository.

Everything the workspace decides moves with you: the worktree's own
`<workspace>/.buildmax/hooks.yaml`, its skills and subagent definitions, its
MCP servers, and its `AGENTS.md` all take effect when the session enters, and
the tree you came from stops applying.

Some things are deliberately not automatic:

- **Creating and entering do not prompt**; removing does, and removing a
  worktree that holds uncommitted files or commits no other branch reaches is
  refused outright unless you say the work can be discarded.
- **Nothing is ever deleted for you** — not when the session ends, and not
  later for one a crashed session left. Use `Worktree` with `action: "list"` to
  see what exists, and remove what you no longer want.
- **Two sessions cannot share one worktree.** A tree another live session is
  working in is refused, naming who holds it; the lock is released when that
  process exits, however it exits, so nothing stays blocked by a session that
  died.

A delegate can have one too. Pass `worktree` to `Task` and the subagent runs
in a worktree of that name with its tools rooted there, while your session
stays where it is. Nothing forces it — the agent decides per delegation, and
for a read-only exploration a shared workspace is the cheaper answer, since the
tree is left on disk afterwards like any other. The reply says where the
delegate's changes are.

Like the `Job` tools, `Worktree` is a TUI capability: print mode, eval, and
worker runs do not get it, and neither do subagents, which share the parent's
root for the length of their run. `buildmax --workspace <dir>` still starts a
session anywhere you like, including in a worktree you made yourself.

When the agent asks for several tools at once, the read-only ones run at the
same time: `Read`, `Glob`, `Grep`, `Skill`, `WebFetch`, `WebSearch`, and a `Task` handed to
a read-only sub-agent such as `explore`. Anything that changes something runs
alone and in order, so a batch does the same thing however it is scheduled.
Tune it with `agent.max_parallel_tools`.

## Behavior worth knowing

**`Edit` fails loudly on ambiguity.** If `old_string` matches more than once and
`replace_all` is not set, the edit is rejected rather than guessing. Give it more
surrounding context.

**`Grep` has three output modes.** `content` returns matching lines with context,
`files_with_matches` returns paths only, `count` returns counts. The model
usually picks well, but the modes are why grep results sometimes look different
between runs.

**`Bash` output is truncated at 30 000 characters** and combines stdout and
stderr. A command producing more than that should be redirected to a file the
agent then reads in ranges.

**`WebFetch` caches for 15 minutes** and converts HTML to markdown. On a
cross-host redirect it returns the redirect URL instead of following it, so the
agent decides whether to fetch the new host.

**`WebSearch` sends queries to Firecrawl.** It returns at most five URLs with
titles and short excerpts; use `WebFetch` to read a source before relying on a
detail. Searches work without setup while the provider accepts keyless requests.
If keyless access is limited, set `web_search.api_key` in local `settings.yaml`.
An unattended Space Agent can instead declare a Secret grant named
`FIRECRAWL_API_KEY`. Network sandbox domain rules also apply to the request.
Provider quotas or charges are separate from BuildMax model-usage reporting.

**`Read` returns the first 1000 lines by default.** Large files are read in
ranges via `offset` and `limit` rather than all at once. A successful read of a
zero-byte file returns `(file is empty)` so it is distinguishable from missing
tool output.

**`Write` confirms the resolved path and UTF-8 byte count.** This lets the
agent verify which file changed and how much content was written without a
follow-up read.

**`NoteWrite` and `TodoWrite` outlive the conversation history.** Both replace
what they store rather than adding to it, so each call carries the complete
list. What they hold is shown to the agent on every turn and is not part of the
message history, which means it survives the compaction that eventually
discards the messages that produced it. Notes are capped at 15 entries of 200
characters; a longer list is rejected and the agent is asked to merge it. A
session that never writes either one carries nothing extra.

**`UploadArtifact` publishes; it does not save.** It hands one file to the space
and returns an opaque reference anyone with access can open. Content is
immutable, so a corrected version is a second artifact rather than a change to
the first. The agent chooses the file: nothing is uploaded automatically, which
is what keeps `.env` files, caches, and intermediate output out of the space's
artifact list. A symlink whose target is outside the workspace is refused even
though the link itself is inside it. An operator sets `storage.max_artifact_mb`,
the per-file limit.

Set `share: true` to also create a public link the agent receives and can hand
to a person: it opens without a BuildMax login and renders in the Portal — a
Markdown document as formatted text, an HTML file as a live page. The link is
revocable and expires. It needs the deployment to have `public_base_url` set;
without it the file still publishes and the tool reports that no link could be
made. A space member can also create or revoke a link from the artifact's Portal
page.
In Portal, open **Artifacts** to upload a file, open an existing artifact by
name, download it, or manage its public links. The artifact page shows a
preview when supported and offers **Retry preview** if that content fails to
load. **Delete** removes the artifact for people allowed to manage it.

## The path boundary

File tools resolve every path against the workspace root and refuse anything
outside it. This boundary is independent of the sandbox — it applies whether or
not the sandbox is enabled, and it applies to `Read`, `Write`, `Edit`, `Glob`,
and `Grep`.

`UploadArtifact` applies it twice: once to the path you name, and again to what
that path resolves to, so a link inside the workspace cannot publish a file
outside it.

`Bash` is the exception: a shell command can reach anywhere the process can.
That is exactly what [the sandbox](sandbox.md) is for, and why it only covers
`Bash`.

## Failure is information

Tools are written for the model, not for a terminal. A failure returns a
specific message — `path outside allowed root`, `file not found`,
`old_string not found` — because the model's next move depends on which one it
was. When you see the agent recover gracefully from a bad edit, this is why.

## Extending the set

| Mechanism | Adds | Guide |
|---|---|---|
| MCP servers | Tools from any MCP-compatible service | [MCP](mcp.md) |
| Skills | Instructions, invoked on demand rather than filling the prompt | [Skills and subagents](skills-and-subagents.md) |
| Subagents | A separate agent with its own tool subset and prompt | [Skills and subagents](skills-and-subagents.md) |

## Related

- [Tool permissions](tool-permissions.md) — which of these stop and ask before running
- [Hooks](hooks.md) — gate tool calls by name, using the names above
- [Sandbox](sandbox.md) — confine what `Bash` can reach
