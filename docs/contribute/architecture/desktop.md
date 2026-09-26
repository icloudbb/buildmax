# Desktop

> **简体中文：** [阅读中文镜像](../../zh-CN/contribute/architecture/desktop.md)
> **Audience:** contributors · **Status:** current
>
> Build and usage notes: [`cmd/buildmax-desktop/README.md`](../../../cmd/buildmax-desktop/README.md)

## Purpose

The Desktop app is the native local surface for the same agent runtime used by
the CLI. Wails hosts a React frontend and binds it to
`internal/interface/desktop.App`; it does not call the Portal backend for local
chat execution.

A `Project` is the local unit of work a session belongs to: one Git repository
including every one of its worktrees, or one plain folder. Desktop owns no
Project record of its own -- it resolves through the same
`agentapp.ProjectManager` the CLI uses, so both surfaces opened on one
repository are the same Project. It is not the server's space, issue, or project
domain model.

## Modes

Desktop runs in one of two modes, the same two the CLI has:

| Mode | What it is |
|---|---|
| `local` | The agent runs here against the models in `settings.yaml`. No server. |
| `server` | The same local agent, plus a signed-in BuildMax account — managed models, and the bridge to a space's work. |

Neither mode changes where the agent runs: chat is always executed locally by
`agentapp`. A server adds identity and the models it manages, which is why
Portal login is a connector rather than a gate — see
[surface positioning](../../design/surface-positioning.md).

`GetAuthStatus` returns the signed-in account, and there is no mode field: the
credentials are the mode. A stored login in `<BUILDMAX_HOME>/auth.json` reports
`server`, no login reports `local`, and nothing is remembered alongside them,
because a second record of one fact is a second source of truth for it. A login
the server no longer honours, or an account it disabled, reports expired: the
app stays in managed mode and refuses to run rather than quietly using local
models, leaving signing in again and signing out as the two ways forward. A
deployment that cannot be reached reports unavailable instead: the login is
kept, the workbench stays open under a banner, and the status is re-read every
15 seconds until the deployment answers. `Logout` revokes the session and
removes the credentials, and that removal is the whole switch back to local. See
[client modes](../../design/client-modes.md) sections 3 and 8.

## Layers

| Path | Responsibility |
|---|---|
| `cmd/buildmax-desktop/main.go` | Thin process entry point, logging, embedded-asset guard |
| `internal/interface/desktop` | Wails lifecycle, Go bindings, project/session state, streaming, approvals, and terminal PTYs |
| `desktop/frontend` | React UI and generated Wails bindings |
| `desktop/assets_embed.go` | Production frontend embed under the `desktop` build tag |
| `cmd/buildmax-desktop/wails.json` | Wails build configuration |

`App` creates one `agentapp.AgentApp` lazily per project folder and caches it
for the process lifetime. Runs are scheduled per session: the scheduler key is `runKey`, the
project plus the session id, so at most one run per session is in flight while
different sessions of one project run concurrently. Shutdown reaps every
terminal shell, cancels active runs, and closes all cached runtimes.

## Data And Runtime Flow

1. The frontend calls a generated Wails binding such as `SendMessageStream`.
2. `App` resolves the local project and creates its shared `AgentApp` when
   needed.
3. The core run emits LLM, tool, usage, and stream events.
4. The bridge forwards those events through Wails (`desktop/*` event names).
5. The React frontend renders deltas and returns approval decisions through
   `RespondApproval`, quoting the `approval_id` of the request it answers.
6. Session persistence and durable traces are handled by `agentapp`, exactly as
   for the CLI.

At most one run per session is in flight. A prompt submitted to a session while
its run is going is queued: `SendMessageStream` returns its 1-based queue
position (0 means it started a run), and `QueuedMessages` re-reads a session's
queue. A brand-new chat keys on an empty session id, so new chats in one project
still serialize until one has an id. The queue is
passed to the run as `RunPromptOpts.Pending`, so a queued prompt usually joins the
turn in progress at its next iteration boundary; the run goroutine's turn loop
picks up anything queued after that. Either way the frontend hears
`desktop/message-dequeued`, and `desktop/message-blocked` when a hook refuses one
— a refusal that leaves the run itself going. `CancelRun` discards the queue
before cancelling. See
[Queued messages](../../design/queued-messages.md).

A finished turn also emits `desktop/turn-digest`: a short recap of what the turn
did, and the answer the user is likely about to type when the turn ended by
asking them something. It is one event per turn rather than one per run, because
a run that drains a queue runs several turns and each recap describes only its
own — so it fires where the turn ended, while the session is still held, not
beside `desktop/stream-done`. Neither half is part of the conversation, which is
why the frontend holds it beside `messages` rather than in it —
`desktop/stream-done` reloads that list from the session, and nothing in the
digest is in the session. The recap renders as a `notice` row closing the
thread; the suggestion becomes the `ChatComposer` ghost, offered as a
placeholder while the input is empty and accepted with Tab.
`agent.turn_digest` in `settings.yaml` switches either half off. See
[tui.md](tui.md) for the same feature in the terminal.

## Command Palette

The chat input offers the same slash commands as the TUI, typed rather than
clicked: a leading `/` opens a palette (`ChatInput.jsx`) listing the commands
and the project's skills, filtered by what follows the slash. Selecting a
command opens its panel or runs its action; selecting a skill drops its `/name`
into the composer to send. There is no longer a row of status-bar buttons — the
model picker, git branch, and run status stay, and everything else is a command.

The command set is the shared `internal/interface/slashcmd` registry, read over
the `GetSlashCommands` binding, so the TUI and Desktop offer the same commands
and descriptions from one source. Each command maps to a binding that already
existed or a thin new one: `/info` to `GetSlashInfo` (session statistics; the
memory half reuses `ProjectMemory`), `/tools` to `GetSlashTools`, `/worktree` to
`GetSlashWorktrees`, and `/compact` to `CompactProjectSession`, which takes the
session's writer lock the way a run does and is refused while one is in flight.
`/sessions` is the one command Desktop does not offer, because the session list
is always visible in the sidebar; the registry records that per surface.

## Session Ownership

Desktop holds no session between calls. A run opens one, owns it for its whole
life including queued prompts, and releases it before it emits
`desktop/stream-done` — so a frontend acting on that event finds the session
free. Everything else either reads without the writer lock (`GetSession`,
`GetRunStatus`, `GetHistoryPoints`) or opens transiently and closes again
(`RewindSession`, `ForkSession`).

That is what makes "not while a run is in flight" enforce itself rather than
needing a flag: a history move takes the writer lock, and a run holding it is
how the move discovers it cannot proceed. The bindings translate that into a
message naming the session as busy. Neither fires a session lifecycle hook —
nothing is starting or ending when a user edits history, and the transient open
is an artifact of Desktop's ownership model, not an event a hook should see.

Project metadata is stored under `<BUILDMAX_HOME>/projects/<project_id>/`,
with one file per memory under `memory/` beside it, shared with the CLI and
described in [local project memory](../../design/local-project-memory.md) §8. Sessions stay
top-level under `<BUILDMAX_HOME>/sessions/` and name their Project by id;
settings, traces, auth, and logs use the regular paths under `BUILDMAX_HOME`,
and project source files stay in the user-selected folder.

The **memory** tab of the `/info` panel lists what the project remembers and
shows one memory's body, over the same store the CLI and TUI read. It is
intentionally read-only: a memory is a Markdown file the user can edit directly,
the tab prints that directory, `buildmax project forget` owns deletion and
clearing, and `--no-project-memory` owns one-run disablement. The completed
[local project memory](../../design/local-project-memory.md) §11.5 keeps those
as the authoritative control paths instead of adding Desktop-specific writes.

Desktop opens a Project at its default workspace, so one Project is one root
here and the runtime cache is keyed by Project alone. Adding a folder resolves
rather than creates: a worktree of a repository already in the list opens that
repository's Project. Deleting a Project and deleting its sessions are separate
decisions -- `DeleteProject` refuses a Project that still owns sessions until
the caller says to take them too.

## Workspace Tabs, Panes, And Terminals

A project's center surface is a grid of tabs. A tab renders one activity of a
kind -- `chat`, `terminal`, `file`, `diff`, or `browser` -- and is identified by
its `(kind, ref)` pair (`desktop/frontend/src/lib/tabs.js`), so opening an
activity that is already open focuses it instead of duplicating it. A chat's ref
is its session id, a terminal's its PTY id, a file or diff tab's its
workspace-relative path, and a browser tab's the session whose page it shows. A
project has at most one unadopted new chat; the tab strip's `+` starts it. Chat
and terminal tabs can be renamed: a chat renames its session, a terminal only
its tab.

The Explorer sidebar section indexes the project's workspace and only browses.
**Directory** lists the tree one level at a time (`ListWorkspaceDir`, `.git`
hidden); **Changes** lists the workspace diff (`GetWorkspaceDiff`). A single
click opens a preview file or diff tab, which the next browse click replaces; a
double-click opens a pinned one.

`desktop/frontend/src/lib/panes.js` lays the tabs out as rows of panes, each
pane a `tabs.js` state. The focused pane receives newly opened tabs. Split right
adds a pane to the focused pane's row and split down adds a row; both create an
empty pane that is removed once focus leaves it still empty. Dragging a tab moves
it to another pane or reorders it within a strip, and a pane emptied by the move
is removed. Tile spreads every tab into its own pane in a near-square grid of at
most three columns; collapse gathers them back into one pane. Panes are
separated by visible dividers but cannot be resized by dragging; the sidebar
width is the only drag-resizable split in the workbench.

A terminal keeps its emulator across tab switches, pane moves, tiling, and
project switches. `TerminalHost` portals each xterm into its own host element
that never changes; `App` moves that element between a pane slot and a hidden
park, because changing a portal's container would remount the emulator and lose
its scrollback. A parked or hidden terminal measures 0×0, so `TerminalPane`
skips a fit whose proposed size is degenerate -- reflowing the buffer to a
sliver would evict scrollback and resize the PTY -- and refits when its tab
becomes active. Switching projects stashes the outgoing layout with its shells
parked, not killed.

Each project's layout is saved in the webview's `localStorage` under
`bm.desktop.workspace.<project_id>`. A terminal tab is saved with its title and
a stable restore key but without its PTY id, since the shell dies with the
process. On the next launch each saved terminal is reopened as a fresh shell in
the project workspace with its snapshot written above the new prompt as static
text; a shell that fails to open is dropped with any pane it empties, and
snapshots whose restore key is no longer in the layout are pruned.

`terminal.go` owns the PTYs. `TerminalOpen` starts the user's `$SHELL -i`
(falling back to `/bin/bash`) at the project's default workspace with the
user's unscrubbed environment: a terminal tab is the user's own authority, not
the Agent's Bash tool, and has no route or socket beyond the Wails bridge (see
[surface positioning](../../design/surface-positioning.md) §5.4). Output goes
out as base64 chunks of at most 32 KiB on `desktop/terminal/data` and the exit
code on `desktop/terminal/exit`, both keyed by strand id; `TerminalWrite`,
`TerminalResize` (non-positive sizes ignored), and `TerminalClose` act on one
strand. The output pump is the only caller of the process's `Wait`, so closing a
tab kills the shell and waits for the pump to reap it. The frontend serializes
up to 1,000 lines of scrollback 1.5 seconds after output settles and calls
`SaveTerminalSnapshot`; `internal/infra/localterminalsnapshotstore` keeps every
snapshot in `<BUILDMAX_HOME>/terminal-snapshots.json`, keyed by project and
restore key and capped at 256 KiB by keeping the tail.

File tabs read through `ReadWorkspaceFile`, a preview bounded to 512 KiB that
reports binary content (a NUL byte) instead of returning it. Paths are
normalized so `..` cannot climb above the root, which is the session's own
workspace when it has one (a worktree) and otherwise the project's default
workspace. `WriteWorkspaceFile` saves to the same root and returns a fresh
preview; `FileView` offers editing only for a preview that is neither binary nor
truncated, so a save never rewrites a file it holds only in part.

Each chat tab is a `ChatSession` bound to one session: it owns that session's
transcript and run state and handles only events tagged with its
`session_id`. A new chat has no id until its run starts; the run emits
`desktop/session-adopted` with the created id once, before any of its stream
events, and only runs that began as a new chat emit it, so the pending tab
adopts the right id while other sessions stream.

Tool approvals are per run. Each project run gets its own approval handler, and
`App` holds every unanswered request under a fresh `approval_id`.
`desktop/approval-request` carries that id with the project and the run's
session id; a new chat has already adopted its id by then, so two new chats
never share a prompt. The frontend keeps one pending request per session, shows
it only in that session's chat tab (and still has it when a hidden tab is
shown), and answers with `RespondApproval(approval_id, decision)`. An id answers
once: an unknown, answered, or withdrawn id returns an error and reaches no run.
Cancelling a run withdraws only its own request, and the frontend drops a
session's request when its run ends. Approval shortcuts are active only in the
focused pane, so one key press never answers two sessions. "Allow for session"
grants are held per session by `agentapp`, so they never carry to another
session of the project.

A browser tab shows a read-only live view of the Agent's browser page for one
session, rendered from the `desktop/browser/frame` screencast.

The status bar is global: on Home and in a project it holds the Launchpad and
the theme toggle, and with a project open it adds new-terminal and grid/tab
controls. The Launchpad is a list of quick-launch entries -- an application,
executable, document, or URL, with optional arguments -- stored in
`<BUILDMAX_HOME>/launchpad.json` by `internal/infra/locallaunchpadstore`. Entries
are global rather than per project. `LaunchEntry` hands the target to the
operating system (`open` on macOS, `start` on Windows, `xdg-open` or the target
itself on Linux) and does not wait for it, so a pinned website opens in the
default browser, not in a tab.

## Build Boundary

`desktop/frontend` is a nested Go module so root Go commands do not traverse
npm packages. Vite outputs to `desktop/dist`, outside that nested module, which
allows `desktop/assets_embed.go` to embed it.

`desktop/frontend` must dedupe react, react-dom, and react/jsx-runtime in its
Vite config. `@buildmax/gui` is a symlinked `file:` dependency that externalises
react and has react installed as a peer, so without dedupe the bundle carries
two React instances, the hook dispatcher of the one that rendered is null, and
the window opens blank. `internal/architecture` has a test for it.

The embed file is guarded by the `desktop` build tag. Without the tag, a stub
keeps `go build ./...`, `go vet ./...`, and `go test ./...` valid on a fresh
clone. A runnable native app must be built with the tag; `./make build` performs
the frontend build and invokes the Wails version pinned by `go.mod`.

## Change Checklist

- Keep Wails bindings in `internal/interface/desktop`; do not assemble another
  agent runtime in the frontend or command package.
- Keep shared presentation in `gui` and Desktop-specific state in
  `desktop/frontend`.
- Preserve JSON field names and the project file format when changing persisted
  desktop state.
- Test Go bridge changes with `./make test`; test frontend changes with
  `./make check desktop`; use `./make build` for the native packaging boundary.

## Related

- [Overview](overview.md)
- [Agent loop](agent-loop.md)
- [Session](session.md)
- [CLI](cli.md)
- [Shared GUI and frontends](../repo-layout.md#frontends)
