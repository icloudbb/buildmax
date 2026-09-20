# Desktop Workspace Tabs and the Explorer Sidebar

> **简体中文：** [阅读中文镜像](../zh-CN/proposals/desktop-workspace-tabs.md)
>
> **Audience:** contributors, product designers, and early adopters · **Status:** proposal — under discussion
>
> **Opened:** 2026-09-19

Related: [roadmap](../ROADMAP.md), [surface positioning](../design/surface-positioning.md),
[client modes](../design/client-modes.md),
[agent sandbox policy](../design/agent-sandbox-policy.md),
[sandbox boundaries](../design/sandbox-boundaries.md),
[session trees, agent mailboxes, and branched workspaces](session-tree-and-agent-mailbox.md),
[local project memory](../design/local-project-memory.md), and the
[Desktop architecture](../contribute/architecture/desktop.md).

## Contents

- [1. Summary](#1-summary)
- [2. Problem and Current Context](#2-problem-and-current-context)
- [3. User Scenarios](#3-user-scenarios)
- [4. Terms and Mental Model](#4-terms-and-mental-model)
- [5. Goals](#5-goals)
- [6. Non-Goals](#6-non-goals)
- [7. Design Principles](#7-design-principles)
- [8. The Tab Model](#8-the-tab-model)
- [9. Tab Kinds](#9-tab-kinds)
- [10. The Explorer Sidebar](#10-the-explorer-sidebar)
- [11. Terminal Tab Semantics](#11-terminal-tab-semantics)
- [12. Concurrency and the Agent-Writer Boundary](#12-concurrency-and-the-agent-writer-boundary)
- [13. Persistence and Lifecycle](#13-persistence-and-lifecycle)
- [14. Security, Sandbox, and Cost](#14-security-sandbox-and-cost)
- [15. Split and Grid Layout](#15-split-and-grid-layout)
- [16. Information Architecture](#16-information-architecture)
- [17. Architecture Landing Areas](#17-architecture-landing-areas)
- [18. Options and Trade-Offs](#18-options-and-trade-offs)
- [19. Candidate First Slice](#19-candidate-first-slice)
- [20. Delivery Phases](#20-delivery-phases)
- [21. Prototype Acceptance Criteria](#21-prototype-acceptance-criteria)
- [22. Open Questions and Evidence Needed](#22-open-questions-and-evidence-needed)
- [23. Likely Destination if Accepted](#23-likely-destination-if-accepted)
- [24. Candidate Direction](#24-candidate-direction)

## 1. Summary

BuildMax Desktop is a Wails application whose center is a single session chat.
One project is open, one session fills the center panel, files and diffs live in a
right-hand inspector, and — by the restriction recorded in the
[session-tree proposal](session-tree-and-agent-mailbox.md) §2.4 — at most one
Agent runs per Project. Real work on a project is not one serial conversation: a
user reads a file, runs a command, watches an Agent, inspects a change, and
returns to the thread. The current surface has three unrelated mechanisms for
that — the center chat, the right inspector, and (from the prototype that
accompanied this paper's earlier draft) a bottom terminal — and none of them lets
the user hold more than one live activity in view.

This proposal evaluates reshaping Desktop around **one center surface of
heterogeneous tabs, fed by an Explorer sidebar** — the arrangement developers
already know from editors like VS Code, adapted to BuildMax's project/session
model. The essence is a single new concept, the tab, whose *content* is one of a
few activity kinds:

- a **chat tab** — one Agent session, the surface Desktop has today;
- a **terminal tab** — an interactive local shell the user drives directly;
- a **file tab** — the content of one workspace file; and
- a **diff tab** — one changed file's diff.

Navigation moves to a left **Explorer** — a single sidebar section with two
display modes, Directory and Changes, that indexes the active project's workspace
and its modified files. The Explorer only browses; opening a concrete file or
change happens by clicking, which opens (or focuses) a content tab in the center.
That is how browsing and content connect: the sidebar is the index, the center is
what you are doing.

The reframe is a simplification, not an addition: it collapses three parallel UI
mechanisms into one tab surface plus one sidebar, so a user's several activities
proceed in parallel as tabs. This proposal records the model, its boundaries,
staged delivery, and the evidence needed before the direction earns a roadmap
slot. It supersedes the earlier "Desktop terminal tabs" draft, folding the local
terminal into one tab kind. An exploratory prototype of the terminal transport
already exists (see §19); its provisional placement as a bottom panel is not the
target and would be replaced by a terminal tab.

## 2. Problem and Current Context

### 2.1 Three unrelated surfaces for one workspace

Desktop (`desktop/frontend/src/App.jsx`) presents a project's work through three
separate mechanisms:

- the **center** shows exactly one session's chat;
- the **right inspector** toggles between Files, Changes (diff), and Session
  Info; and
- an interactive terminal exists only as the exploratory bottom panel added while
  drafting this proposal.

Each is its own layout, its own state, and its own affordance. A file and a
conversation cannot sit side by side; a terminal is a mode, not a peer of the
chat; switching from "read this file" to "watch this diff" to "talk to the Agent"
means moving between three unlike surfaces.

### 2.2 One live activity at a time serializes the user

The center holds one session. There is no way to keep a file open while chatting,
to run a command while reading a diff, or to hold two sessions in view. The user
serializes on the UI, not on the work — the human waits on a single-focus surface
for activities that are naturally concurrent. External editors and terminals fill
the gap, splitting the project's context across windows that do not know about the
workspace the Agent operates on.

### 2.3 The parallel direction already assumes a multi-activity surface

The local-experience direction is toward parallel work. The
[session-tree proposal](session-tree-and-agent-mailbox.md) forks child sessions
into isolated worktrees and fans results back; its §15.1 sketches a Desktop that
shows children, inboxes, and result cards, and notes that "concurrent execution
is exposed only after workspace isolation exists." That surface needs to display
several live activities. A tab surface is the honest, minimal form of it, and it
is useful immediately — for files, terminals, and diffs — long before the harder
concurrency machinery lands.

### 2.4 The building blocks exist

Desktop already renders a session (chat), a file tree, and a diff view; it already
streams bytes to the frontend over Wails events; and the Go side already vendors
`github.com/creack/pty` and owns a Bash sandbox. What is missing is not the
content renderers but the organizing surface — a tab model and an Explorer — that
lets them coexist and multiply.

## 3. User Scenarios

### 3.1 A file, a terminal, and a conversation, side by side

A user reads `store.go` in a file tab, runs `./make test ...` in a terminal tab,
and asks the Agent about a failure in a chat tab. Switching among the three is one
click on the tab strip, not a move between three unlike surfaces. Nothing forces
the file closed to see the conversation.

### 3.2 Browse, then open

The user switches the Explorer to Directory, clicks `internal/infra/db/store.go`,
and a file tab opens. They switch the Explorer to Changes, click a modified file,
and a diff tab opens beside it. The sidebar stays put as the index; the content
accumulates as tabs.

### 3.3 Two conversations in view

Two Agent sessions on two tasks are two chat tabs. The user reads one while the
other streams, answers an approval in one without closing the other. (Whether both
may *run* at once is gated separately; see §12.)

### 3.4 Compare two files in a split

The user drags a diff tab into a second pane so the change sits next to the file
it touches. The same tabs, tiled — no new activity kind, just a layout (§15).

## 4. Terms and Mental Model

### 4.1 Tab

A **tab** is one independently alive, switchable activity in the center surface,
with its own creation, focus, and disposal. It is the single new organizing
concept. A tab renders one content kind; it is not itself the content's backing.

### 4.2 Tab kinds

The content kinds in scope are **chat** (a session), **terminal** (a PTY),
**file** (a workspace file's content), and **diff** (a changed file's diff). The
model is open to more kinds later, but every kind must earn its place by a
concrete activity, not by symmetry.

### 4.3 Explorer

The **Explorer** is the left sidebar section that indexes the active project's
workspace. It has two display modes — **Directory** (the file tree) and
**Changes** (modified files) — analogous to an editor's Explorer and Source
Control, unified as two views of one section. It browses; it does not hold
content. Clicking an entry opens a content tab.

### 4.4 Pane

A **pane** is a rectangular region of the center surface that displays one tab at
a time. Today there is one pane; §15 tiles several. A tab's backing is
pane-independent, so a tab can move between panes without disturbing its content.

## 5. Goals

- Make the center one surface of heterogeneous tabs — chat, terminal, file, diff
  — so several activities are held and switched at once.
- Let the user open a local terminal and run commands without leaving the app.
- Let the user open workspace files and diffs as first-class content beside a
  conversation.
- Give the left sidebar a project-scoped Explorer with Directory and Changes
  modes, connected to the center by click-to-open.
- Scope tabs to the active project, matching the existing project/session model
  and the local-project workspace boundary.
- Keep the surface ready for split/grid layout without making that a new concept.
- Key runs per session so agent tabs can run concurrently, and leave isolating
  concurrent writers (a per-session worktree) to the user (§12).

## 6. Non-Goals

- A general extensible editor platform or plugin-contributed tab kinds. The kinds
  are a small, closed set until a concrete activity justifies another.
- A full code editor. A file tab is a viewer that also supports a plain edit-and-
  save on text files; language services, multi-cursor, and the like are out of
  scope.
- A remote or server-side terminal, or SSH into a worker or Space. Terminals are
  local only (§11, §14).
- Platform-enforced workspace isolation between concurrent agent runs. Runs are
  keyed per session so they *can* run at once; keeping two from clobbering one
  workspace (a per-session worktree) is the user's call (§12).
- Replacing the Agent's Bash tool with the user terminal (§11.4).
- A persistent terminal daemon, or reconstructing terminal scrollback across app
  restarts.
- A shared `@buildmax/gui` tab framework before a second surface needs one.
- Changing Portal, which is a browser surface with a different trust boundary
  (§16.3).

## 7. Design Principles

### 7.1 One organizing concept

The center is tabs; the sidebar is the Explorer. Two mechanisms, not five. A new
activity is a new tab kind, not a new surface. This is the whole reason the
reframe is a simplification.

### 7.2 The sidebar indexes; the center holds content

The Explorer browses the workspace and its changes; it never renders file content
or a diff itself. Content always lives in a center tab, opened by a click. Keeping
navigation and content in separate homes stops the directory browser from being
two things in two places.

### 7.3 Tabs are project-scoped

A project owns one workspace (`internal/core/localproject`: one repo or
directory). Terminals and files are of that workspace, and sessions are of that
project. So the tab set belongs to the active project; a chat tab is one session
rendered, and terminal, file, and diff tabs are its workspace siblings. Switching
projects switches the tab set.

### 7.4 Opening is idempotent

Clicking a file that is already open focuses its tab rather than opening a
duplicate. A single-click browse may reuse one preview tab; an explicit open pins
a tab. The surface must not grow a tab per click.

### 7.5 A backing is not a pane

A tab renders a backing — a session, a PTY, a file, a diff — that exists
independently of which pane shows it. This keeps split/grid a layout concern and
lets a tab move between panes without losing state.

### 7.6 Human parallelism is free; writer safety is the user's

Several tabs, and several read-only or idle activities, are safe to show at once.
Runs are keyed per session, so several agent tabs may also run at once; whether
that is *safe* depends on whether they share a workspace, which the user controls
with a per-session worktree. The surface expresses the concurrency; the user owns
isolating writers (§12).

### 7.7 Prototype to learn, propose to decide

The terminal transport prototype validates PTY mechanics. It does not decide the
surface. Evidence of real, repeated use earns a roadmap slot.

## 8. The Tab Model

A tab has a stable id, a kind, a title, a focus state, a dirty/activity indicator,
and a disposal. The center surface owns creation, switching, reordering, closing,
and — later — moving a tab between panes. Each backing owns its own lifecycle.

Opening rules:

- opening an activity that is already open focuses the existing tab;
- a browse click may populate a reused preview tab; a double-click or explicit
  action pins it as a durable tab;
- closing a tab disposes its view and, for a terminal, terminates its shell; for a
  chat tab it hides the session, which remains persisted; for a file or diff tab it
  simply closes the view.

The tab set is project-scoped: it is the set of activities open for the active
project. Switching the active project shows that project's tabs. Whether a
project's open tab set is remembered across restarts is UI state, decided in §13.

## 9. Tab Kinds

Each kind shares the tab surface but not its backing:

| Kind | Backing | Driver | Concurrency | Persistence |
|---|---|---|---|---|
| Chat | Agent session + run | User prompt, then the Agent Loop | One run per session; sessions run concurrently, the user isolates writers (§12) | Existing session persistence |
| Terminal | Interactive local PTY | The user, keystroke by keystroke | Freely parallel (isolated processes) | None across restart |
| File | A workspace file's content | Read, plus edit-and-save on text files | Freely parallel; a save writes to disk | None; re-read from disk |
| Diff | One changed file's diff | Read | Freely parallel (read) | None; derived from workspace state |

Session Info, today a right-inspector mode, becomes a property view of the active
chat tab rather than its own center content kind — it describes a session, so it
belongs to that session's tab, not to a peer tab.

The kinds are intentionally few. A new kind (for example a search results view)
must be justified by a concrete activity, per Occam, not added for symmetry with
an editor.

## 10. The Explorer Sidebar

### 10.1 Structure

The left sidebar, top to bottom, is **Projects** then **Explorer**. Projects is
the existing project list, with its sessions. Explorer is scoped to the active
project and has two display modes:

- **Directory** — the workspace file tree; and
- **Changes** — the workspace's modified files, as a change list.

The two are modes of one Explorer section, not two independent panels, so the
sidebar has one browsing home that switches what it indexes.

### 10.2 Click-to-open connects the sidebar to the center

The Explorer browses; it does not render content. Interaction:

- clicking a file in Directory opens (or focuses) a **file tab**;
- clicking a changed file in Changes opens (or focuses) a **diff tab**; and
- a browse click may reuse a single preview tab, per §7.4, so scanning the tree
  does not spray tabs.

This is the whole connection between navigation and content: the sidebar is the
index, the center is the content, and a click is the link.

### 10.3 What moves out of the right inspector

Today's right-inspector Files and Changes modes move into the Explorer sidebar;
their *content* (a file's text, a file's diff) moves into center tabs. Session
Info moves onto the chat tab (§9). The right inspector as a separate mechanism is
retired, not relocated — its responsibilities are absorbed by the sidebar and the
tab surface.

## 11. Terminal Tab Semantics

A terminal tab is one shell strand: an interactive local PTY the user drives.

### 11.1 Spawn and working directory

A terminal starts the user's `$SHELL` (falling back to a sensible default) at the
active project's workspace root, inheriting the user's environment. The terminal
is the user's own authority — not an Agent tool call — so its environment is not
scrubbed the way an Agent child's is.

### 11.2 The PTY channel

The Go side allocates a PTY, starts the shell attached to it, and pumps output to
the frontend as a byte stream over Wails events keyed by tab id. The frontend
writes keystrokes back through a bound method and sends resize (columns and rows)
on geometry change so the controlling process sees a correct window size. Control
sequences, colors, and binary output pass through untransformed; the frontend
emulator interprets them.

### 11.3 Lifecycle

Scrollback lives in the emulator for the tab's life and is not persisted. When the
shell exits, the tab shows the exit and stops. Closing the tab terminates the
shell. No shell outlives the Desktop process: on shutdown the Go side reaps every
PTY it owns.

### 11.4 A user terminal is not the Agent's Bash tool

A terminal tab is the user's direct, uncapped, interactive authority; the Agent's
Bash tool is scoped, sandboxable, capped, and part of a run's authority and trace.
Keeping them distinct avoids routing user shell input through Agent policy and
avoids laundering Agent authority through a "user" terminal. Whether a co-located
Agent may observe a terminal's output is a separate, later decision; the first
slice keeps the terminal private to the user.

## 12. Concurrency and the Agent-Writer Boundary

Runs are keyed per session, not per project: one run per session is in flight at a
time (a second prompt to the same session queues), and different sessions in a
project run **concurrently**. Opening several chat tabs and letting them run at
once is therefore a real capability, not just a viewing convenience.

The remaining hazard is two agents writing one workspace and corrupting each
other's state ([session-tree](session-tree-and-agent-mailbox.md) §2.4). Rather
than have the platform forbid concurrency until it can force isolation, this
design leaves isolation to the **user**, who already has the tool for it: a
session may run in its own Git worktree distinct from the project default
(`session.Meta.Workspace`, which the Desktop file bindings already resolve
per session). A user who runs two agents in one shared workspace owns that
choice, exactly as they would running two terminals against it. The platform's
job is to key runs per session so the surface can express the concurrency, and to
route each run's stream events (tagged with their `session_id`) to the right chat
tab; deciding when concurrent runs are safe is the user's.

Terminal, file, and diff tabs carry no writer question either — terminals are
isolated processes, a file save is a direct user-authored write to disk, and diff
tabs are read-only — so they parallelize freely.

Each chat tab is a self-contained `ChatSession` bound to one session: it owns that
session's transcript and run state and handles only the events tagged with its id.
A brand-new chat starts with no id and adopts the real one from a dedicated
`session-adopted` event the backend emits at the run's start (only new-chat runs
emit it, and at most one new chat exists per project, so the adoption is
unambiguous even while other sessions stream). One caveat remains: tool-approval
prompts are keyed per project, not per session, so a pending prompt is shown by
each of the project's running chat tabs and any of them can answer it — adequate
because approvals are brief and rare, but session-scoped approvals are a later
refinement.

## 13. Persistence and Lifecycle

- A chat tab keeps the session persistence Desktop already has; closing it hides
  the session, which stays stored.
- A terminal tab has no durable state; it dies with the tab or the window, and its
  scrollback is not persisted.
- A file or diff tab is a derived view; it is re-read from the workspace on open
  and needs no persistence.
- A project's layout is remembered as UI state (per project, in local storage), so
  a restart or project switch reopens the same chat, file, and diff tabs in the
  same pane grid. Terminal tabs are excluded from the save — a dead process cannot
  be reopened, only a new one started — and any pane left empty by that exclusion
  is dropped on restore.
- No tab backing outlives the Desktop process where it should not: sessions
  persist, but PTYs are reaped on shutdown so no shell is orphaned.

## 14. Security, Sandbox, and Cost

### 14.1 Sandbox posture

The Bash sandbox "defaults off for CLI and on with fail-closed enforcement for
official workers" ([agent sandbox policy](../design/agent-sandbox-policy.md)). A
user terminal tab is the interactive-local analogue of the CLI: the user's own
authority, sandbox off by default. This is a deliberate posture, to be recorded in
the design record if accepted; a user opt-in to sandbox a terminal is a later
option, not a first-slice requirement.

### 14.2 Local-only boundary

Terminal, file, and diff tabs are local Desktop capabilities over the project
workspace. They are not exposed to Portal, workers, or any network surface, and
this proposal creates no route, socket, or server binding; the terminal transport
is the in-process Wails bridge only.

### 14.3 Resource hygiene

Each terminal is a real child process with a file descriptor and an output pump;
closing a tab reaps its process and stops its pump, and shutdown reaps all. Output
per event is bounded so a runaway process floods neither the bridge nor the
emulator unboundedly. File and diff tabs read bounded content and hold no process.

### 14.4 Cost

Terminal, file, and diff tabs spend no model tokens. Chat-tab parallelism now
lets several runs spend tokens at once; that spend is the user's to govern, as
running several agents always is. The tab surface itself introduces no new cost.

## 15. Split and Grid Layout

Grid is a presentation layer over the tab model, not a new concept. The center
surface may split into several panes, each showing one tab, tiled so the user sees
multiple activities at once — a file beside its diff, a terminal beneath a chat.

Because a tab's backing is pane-independent (§7.5), tiling only decides which pane
renders which tab; it changes no backing, lifecycle, or authority. The model holds
the constraint regardless of layout: every tab kind's backing is decoupled from
its pane, so adding panes needs no rework of sessions, PTYs, or file views.

### 15.1 Implemented: a grid of panes

The workspace is a grid — rows of panes. Each pane is its own tab strip with its
own active tab, and one pane is *focused*. Newly opened activities — a file or
diff from the Explorer, a new terminal — land in the focused pane. Each pane's
tab strip carries **Split right** (add a pane to this row) and **Split down** (add
a new row) controls; either inserts a new empty pane and focuses it. An empty
pane is transient: it is reaped as soon as focus leaves it, so the layout never
accumulates blank panes. Closing a pane's last tab removes the pane and focuses a
neighbour.

A tab is **dragged** from its strip and dropped onto another pane to move it
there. Because a tab's backing is pane-independent (§7.5), the move carries the
live content, not just the descriptor. Terminals make this concrete: every
terminal's emulator is kept mounted in a hidden host and *portalled* into
whichever pane shows it (`TerminalHost`), so a dragged terminal keeps its
scrollback and cursor — the DOM node is relocated, never recreated. This is why
the model insists a backing is decoupled from its pane.

The tab strip carries the ordinary editor gestures. The same drag that moves a
tab across panes also **reorders** it within a strip — dropped before the tab
under the pointer, or at the end past the last one. A **right-click** opens a
context menu with *Close*, *Close others*, and *Close tabs to the right*; each
spares a non-closable tab (the current chat), just as the per-tab close button
does. On a file or diff tab the menu also offers *Copy relative path* and *Copy
absolute path*: the relative form is the workspace-root path, while the absolute
form is resolved in Go against the session's own workspace root — a worktree when
the session has one — so it names the file the panel actually reads, and the copy
goes through the native clipboard.

A status bar spans the bottom of the workspace as a global surface — present on
the Home screen and in a project alike. Its controls read left to right as
status, then the workspace actions when a project is open, and the theme toggle
pinned at the far right so its position never shifts as project controls appear
and disappear. One of those workspace actions is a single icon button that
**toggles** between the two shapes so neither has to be built by hand: from one
tabbed pane it *tiles* every tab into its own pane laid out in a near-square grid
capped at three columns (`tile`), and from a grid it *collapses* every pane back
into one tabbed pane in reading order (`collapse`). Both are pure operations on
the same model, and because backings are pane-independent a tiled terminal keeps
its session exactly as a dragged one does.

### 15.2 Deferred: resizable splitters

Panes currently share space equally (flex); a draggable divider to resize them,
and arbitrary nested splits beyond rows-of-panes, remain future work. The layout
itself is persisted per project (§13).

## 16. Information Architecture

### 16.1 Desktop, before and after

| Concern | Today | Proposed |
|---|---|---|
| Conversation | Single center chat | Chat tab (one of many) |
| Terminal | Prototype bottom panel | Terminal tab |
| Browse files | Right inspector "Files" | Explorer → Directory |
| Browse changes | Right inspector "Changes" | Explorer → Changes |
| View a file | — | File tab (opened from Explorer) |
| View a diff | Right inspector diff view | Diff tab (opened from Explorer) |
| Session info | Right inspector "Info" | Property of the chat tab |
| Layout | Fixed panels | Tabs, later split/grid panes |

### 16.2 CLI and TUI

No change. The CLI already runs in the user's terminal; a terminal-in-a-terminal
is not a goal, and the CLI is not tabbed.

### 16.3 Portal

Out of scope. Portal is a browser surface with a different trust boundary; a
browser terminal into a server-side shell in particular would expose remote
command execution across the network authorization surface and belongs to a
separate, security-reviewed proposal. Any convergence with
[portal navigation](../design/portal-navigation-and-space-context.md) is later
work, not part of this local-Desktop paper.

## 17. Architecture Landing Areas

If accepted, candidate ownership boundaries are:

| Responsibility | Candidate owner |
|---|---|
| Tab model, center surface, and (later) pane layout | `desktop/frontend/src` |
| Explorer sidebar (Directory and Changes modes) | `desktop/frontend/src`, over existing file/diff bindings |
| Interactive PTY session manager (spawn, pump, write, resize, reap) | `internal/interface/desktop` in the first slice; extract to `internal/infra/terminal` only when a second surface needs it |
| Terminal Wails bindings and byte-stream events | `internal/interface/desktop` |
| File and diff content bindings | Existing `internal/interface/desktop` file and diff methods |
| Shared tab or terminal presentation, if a second surface appears | `@buildmax/gui` (`gui/src`), not before |
| Terminal sandbox opt-in, if added | `internal/infra/sandbox` `WrapBashCommand`, wrapping the shell invocation |

The PTY session manager stays out of `internal/core`: it is infrastructure, not
domain code. Per Occam, it lives with its single consumer until a second one
demonstrably needs it, rather than being introduced as a shared package
speculatively.

## 18. Options and Trade-Offs

| Option | Strength | Main concern |
|---|---|---|
| A. Keep fixed panels; add a terminal panel only | Smallest change | Leaves three unrelated surfaces; no parallel activities; the terminal is a mode, not a peer |
| B. Center tab surface + Explorer sidebar (this paper) | One organizing concept; parallel activities; matches editor mental model | A larger UI change than a bolt-on panel |
| C. B plus split/grid immediately | The full parallel vision | Split adds layout complexity before the tab model has evidence |
| D. A full extensible editor platform with plugin tab kinds | Maximum flexibility | Vast scope; violates Occam; no demonstrated need |

The candidate recommendation is **B, with split/grid (C) deferred** as a pure
layout layer once tabs prove out. A is the status quo the reframe replaces. D is
rejected: the tab kinds are a small closed set until a concrete activity justifies
another.

## 19. Candidate First Slice

The first slice makes the center a tab surface and stands up the Explorer, with
the smallest set of kinds that proves the model:

- a tab surface in the center that opens, focuses, switches, and closes tabs, with
  idempotent open (§7.4);
- the existing chat lifted into a **chat tab**;
- a **terminal tab** backed by the PTY session manager (open at the workspace
  root, pump output, accept keystrokes and resize, close and reap) — the transport
  for which is already prototyped (below);
- a **file tab** and a **diff tab** opened by clicking in the Explorer; and
- the **Explorer sidebar** with Directory and Changes modes, replacing the right
  inspector's Files and Changes.

The initial prototype excluded more; since built on top of it are the pane grid
(§15), per-project layout persistence (§13), file editing, and concurrent chat
tabs (runs keyed per session, §12). Still excluded: sandbox opt-in and Agent
observation of a terminal.

The terminal transport already exists as an exploratory prototype from this
paper's earlier draft: a Go PTY session manager
(`internal/interface/desktop/terminal.go`) with Wails bindings and byte-stream
events, an `xterm` frontend, and tests that drive a real shell. Its current
placement as a bottom panel is provisional; folding it into a terminal tab is the
target. The prototype validates the transport, not the surface.

## 20. Delivery Phases

### Phase 0: Validate the user problem

- Observe whether Desktop users keep external editors and terminals open, and for
  what.
- Learn whether the felt need is a terminal beside chat, files beside chat,
  several sessions, or all of these.
- Confirm the tab surface and Explorer, not a single bolt-on panel, is the shape
  users want.

### Phase 1: Tab surface, chat and terminal tabs, Explorer

- Center tab surface with idempotent open and close.
- Chat lifted into a chat tab; terminal tab on the prototyped PTY transport.
- Explorer sidebar with Directory and Changes; file and diff tabs opened by
  clicking; right inspector retired.

### Phase 2: Read-quality file and diff tabs

- Syntax-aware file viewing, diff rendering, and large-file bounds.
- Preview-versus-pinned tab behavior.
- Reopen a project's chat, file, and diff tabs across restart.

### Phase 3: Multiple concurrent chat tabs

- Several chat tabs open, switchable, and each running its own session
  concurrently: runs keyed per session, every stream event tagged with its
  `session_id` and routed to its tab.
- Isolation between concurrent writers is the user's, via a per-session worktree
  (§12).

### Phase 4: Split and grid

- Tile the center into panes; move tabs between panes.
- Keep every backing pane-independent.

### Phase 5: Refinements

- Optional terminal sandbox opt-in; optional, explicitly-decided Agent observation
  of a terminal. (File editing has since been built — see §13.)

## 21. Prototype Acceptance Criteria

The Phase 1 slice must show that:

- the center opens, focuses, switches, and closes tabs, and opening an
  already-open activity focuses it rather than duplicating it;
- a chat tab preserves today's session behavior;
- a terminal tab opens at the workspace root with the user's shell, passes
  keystrokes, control characters, colors, and binary output correctly, resizes the
  PTY to the pane, and runs interactive programs;
- closing a terminal tab reaps its shell, and closing the window reaps all shells
  with no orphaned processes;
- clicking a file in Directory opens a file tab and clicking a change in Changes
  opens a diff tab, each reusing an open tab rather than duplicating it;
- the Explorer switches between Directory and Changes as modes of one section; and
- a terminal runs with the user's own authority, not routed through Agent policy.

## 22. Open Questions and Evidence Needed

### Product and UX

- Which activities do users open most — terminals, files, or multiple sessions?
  The answer orders the phases.
- Should browse clicks use a single reused preview tab (editor-style), or should
  every click pin a tab?
- Resolved: a new chat is created from the project list's new-chat action (and any
  session opens as a chat tab from the sidebar); at most one not-yet-sent new chat
  exists per project.

### Behavior and transport

- What default shell and startup mode best matches user expectation across macOS,
  Linux, and Windows (ConPTY)?
- What per-event output bound keeps a runaway process from flooding the bridge
  without harming throughput?
- What are the size and rendering bounds for a file tab before it degrades or
  refuses?

### Boundary and security

- Should a terminal tab ever be sandboxable, and if so opt-in or policy-driven?
- If an Agent may later observe a terminal, what authority and consent model
  governs it, given the terminal carries the user's full environment?
- Resolved: file tabs support edit-and-save; the write runs with the user's own
  authority (not the Agent's), clamped to the resolved workspace root, and is
  refused for binary or truncated content (§13, §14).

### Concurrency and layout

- Resolved: concurrent agent tabs run per session; isolating writers is the user's
  responsibility via a per-session worktree, not a platform-forced gate (§12).
- Resolved: the grid layout is persisted per project (terminals excluded); see §13.

### Evidence needed before acceptance

- Design partners keep several tabs open during real work, not once out of
  novelty.
- The terminal and file/diff viewers are correct enough for daily use.
- Browsing in the Explorer and opening tabs is faster than the current inspector,
  not merely different.
- No orphaned shell processes survive normal or abnormal Desktop exit.
- The surface measurably reduces window-switching, rather than duplicating tools
  users keep anyway.

## 23. Likely Destination if Accepted

If evidence supports the direction:

1. Record the accepted tab model, Explorer structure, and terminal-sandbox posture
   in a Local Experience design record, alongside
   [surface positioning](../design/surface-positioning.md) and the
   [Desktop architecture](../contribute/architecture/desktop.md).
2. Add its sequence to [ROADMAP.md](../ROADMAP.md), noting that concurrent agent
   tabs run per session with user-owned worktree isolation, and how that relates
   to the session-tree workspace work.
3. Create implementation issues for the tab surface, the Explorer, the terminal
   tab, file and diff tabs, and any split/grid or sandbox opt-in.
4. Update the Desktop and surface-positioning architecture documents and the user
   manual as slices land.
5. Add a changelog entry when the user-visible surface ships.
6. Delete this proposal once its decision has moved to roadmap, design, and
   implementation records; Git history preserves the discussion.

If evidence supports only a terminal beside chat, accept the terminal tab and
defer the Explorer reframe. If it shows users only need Agent parallelism and
never a manual terminal or file view, invest in the session-tree surface instead.

## 24. Candidate Direction

The candidate direction is:

> BuildMax Desktop should present one center surface of heterogeneous tabs — chat,
> terminal, file, and diff — fed by a project-scoped Explorer sidebar with
> Directory and Changes modes. The sidebar indexes the workspace; a click opens a
> content tab; the center holds what the user is doing. The tab is the one new
> concept, split/grid is a later layout over it, and agent tabs run concurrently
> per session with the user owning writer isolation via a per-session worktree. The
> terminal is one tab kind, not a special surface.

This offers more than a bolted-on terminal and less than an extensible editor
platform. Whether it earns a roadmap slot depends on repeated evidence that users
keep several tabs open during real work and that the surface, not the novelty,
changes their rhythm.
