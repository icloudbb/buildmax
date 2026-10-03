# Workspace Root And Worktrees

> **简体中文：** [阅读中文镜像](../zh-CN/design/工作区根与工作树.md)

> **Audience:** contributors · **Status:** implemented, phases 1-5

Related: [roadmap](../ROADMAP.md) step 5, [hook system](hook-system.md),
[sandbox boundaries](sandbox-boundaries.md),
[tool permissions](tool-permissions.md),
[local background jobs](local-background-jobs.md),
[context durability](context-durability.md),
[Agent Loop](../contribute/architecture/agent-loop.md),
[Session architecture](../contribute/architecture/session.md),
[CLI architecture](../contribute/architecture/cli.md).

## Contents

- [1. What This Record Decides](#1-what-this-record-decides)
- [2. Why](#2-why)
- [3. Scope](#3-scope)
- [4. The Root Is Not Just A Path](#4-the-root-is-not-just-a-path)
- [5. Decisions](#5-decisions)
- [6. What The User Sees](#6-what-the-user-sees)
- [7. Landing Areas](#7-landing-areas)
- [8. Staged Delivery](#8-staged-delivery)
- [9. Open Questions](#9-open-questions)
- [10. User Documentation Obligations](#10-user-documentation-obligations)

## 1. What This Record Decides

The workspace root becomes session state instead of construction state, and an
agent can create a Git worktree, move the session into it, work there, leave,
and clean up — without the user doing any of it by hand.

All five phases are implemented. The root is session state every tool consults
per call, a `Worktree` tool creates, enters, leaves, lists, and removes them,
the configuration the root decides follows a move, three hook events announce
what happened, and a delegate can be given a worktree of its own. This record
exists so the implementation does not have to re-derive which of the root's
dependents move with it and which do not, because that question, not the
worktree command, is where this feature is easy to get subtly wrong.

## 2. Why

### 2.1 The decision arrives mid-conversation

Starting a session in a worktree already works: `buildmax --workspace
../repo-refactor` gives file tools, Bash, hooks, skills, and `/diff` that
directory as their root, with nothing missing. That option is only available
before the first turn.

The need this record answers is the opposite case, and it is the common one:
several turns into a conversation, after the constraints and the shape of the
change are established, it becomes clear the work should proceed on a separate
tree. Starting over in a new session recovers the worktree and loses the
context that made it necessary. Asking the user to run the Git commands, open a
terminal, and restart the agent recovers both and costs the interruption the
agent was supposed to remove.

### 2.2 What blocks it today

| Blocker | Where |
|---|---|
| The root is fixed when the runtime is assembled, and nothing changes it afterwards | `internal/agentapp/app.go`, `--workspace` in `internal/interface/cli/root.go` |
| Read, Write, Edit, Glob, and Grep refuse any path outside the root | `internal/tool/base.go` via `ResolvePath` in `internal/util/workspace.go` |
| Every Bash call — foreground and background job — starts in the root again, so `cd` never survives | `internal/tool/bash.go` |
| No surface can switch; the TUI has no command for it | `internal/interface/cli/chat.go` |
| Subagents and background jobs share the parent's root by construction | `internal/tool/task.go`, `internal/agentapp/job` |
| The sandbox binds the workspace, and only the workspace, writable | `internal/infra/sandbox/manager.go` |

`git` is not in the risky-prefix list in `internal/tool/safety.go`, and Bash
validates commands rather than paths, so an agent can already create a worktree
and address it as a foreign directory: every command re-prefixed with `cd` or
`git -C`, every read through `cat` instead of Read, every search through `grep`
instead of Grep. A model that forgets the prefix once writes to the wrong tree,
and nothing in the system notices. That failure mode is the reason this is a
capability rather than a prompt instruction.

## 3. Scope

**In scope.** One session, one root at a time, moved on the user's instruction
or the agent's own initiative: create, enter, work, leave, remove. Every tool
follows the current root with no new syntax. The user can always see which root
the session is in.

**Out of scope.** Forking the session or branching its context. Fork shipped
separately as a history copy that shares the parent's workspace
([local session storage](local-session-storage.md) §8.3); a fork that needs its
own tree enters one with this record's tool. Also out: several concurrent
sessions under one supervisor, automatic merge or rebase of a worktree's
changes, worktree paths in Portal or worker runs (neither exposes a local
path), and versioned snapshots for non-Git workspaces, whose design record was
withdrawn and is not pending.

Giving a subagent or a background job its own worktree is in scope and never
mandatory — see D7. It answers a different request than the rest of this
record, delegating work to another tree rather than working in one, but it
reuses the same mechanism: `Task` takes an optional `worktree` name, the tree
is created and held without the parent's root moving, and that agent type's
tools are rebuilt against it.

## 4. The Root Is Not Just A Path

`internal/agentapp/app_builder.go` resolves workspace hooks, MCP servers,
skills, and subagent definitions from the root once, at assembly. The sandbox
manager is built from it. Each turn, `BuildEffectiveSystemPrompt` reads the
workspace `AGENTS.md` from it. `/diff` reads the Git workspace at
`App.WorkspaceRoot()`, and the TUI reports that path as where the session is.

Every one of those needs an answer, and the answers are not the same:

| Derived from the root | Decision |
|---|---|
| Workspace hooks (`<workspace>/.buildmax/hooks.yaml`) | Re-load on switch |
| MCP servers | Re-resolve, then restart only the servers whose definition changed — normally a no-op between worktrees of one repository |
| Skills and subagent definitions | Re-resolve on switch |
| `AGENTS.md` prompt layer | Re-read; see D2 |
| Sandbox writable bind | Must follow the root, or the sandbox denies every write in the new tree |
| `/diff` | Follows the root; shows the current tree only, never a merged view of two |
| TUI header | Must follow the root, or the user is shown the wrong tree |
| Plugins | Unaffected: they load from `<BUILDMAX_HOME>/plugins/`, not from the workspace |

## 5. Decisions

### D1. Worktrees live at `<repo>/.buildmax/worktrees/<name>`

Registered in `.git/info/exclude` rather than the repository's `.gitignore`.
`.buildmax/` is a tracked directory in repositories that use it — this one
tracks `README.md`, `agents/`, `mcp.json`, and `skills/` — so an ignore entry
is required, and `.git/info/exclude` is per-clone: it keeps the worktrees
invisible to Git without producing a commit the user has to review or a diff
they did not ask for.

Rejected: a central location under `<BUILDMAX_HOME>`, which is easy to clean up
centrally but puts the tree far from the repository and out of reach of the
user's editor; and a sibling `../<repo>-<name>`, which needs no ignore but
pollutes the parent directory and collides across repositories.

### D2. The `AGENTS.md` prompt layer is re-read on switch

The four prompt layers are stable for a session precisely so they form the
cacheable prefix. Switching the root changes layer 3, and there are only two
honest options: re-read it and invalidate the prefix, or freeze it and accept
that the loaded instructions describe a different directory than the one being
written to.

This record chooses re-read. A session switches roots a few times at most, so
the cost is bounded and visible in the usage stats; the frozen-layer failure
raises no error and produces wrong changes quietly. The other three layers —
the runtime prompt, `<BUILDMAX_HOME>/AGENTS.md`, and the run's additional
system prompt — do not change, and the compaction summary is appended after the
layers by `RunLoop`, so it is unaffected.

### D3. The root may only move to a worktree of the current repository

Verified against `git worktree list`. `ResolvePath` refusing to leave the root
is a security property today, and a movable root turns a fixed boundary into a
movable one; this constraint keeps the set of reachable roots closed and
enumerable rather than "any directory the model names". Within a root, path
containment is unchanged.

A session may enter a worktree it did not create, but only while no live
session is in it. An idle tree — left by an earlier session, or created by this
one before a resume — may be entered. A tree another live session currently
holds as its root is refused, and the refusal names the holder.

Two sessions writing one tree is the race this whole feature exists to avoid,
so it is excluded rather than merely reported. Refusing costs little: the
second session can create its own worktree, which is one tool call. Forbidding
entry outright would cost more — a resumed session could not return to its own
work, and an abandoned branch could never be picked up again.

Occupancy is defined by D10. Nothing here turns on which session *created* a
worktree: no decision in this record needs that, so nothing records it.

### D4. Creating and entering are allowed; removing asks

Following [tool permissions](tool-permissions.md), the tiers are deliberately
asymmetric:

| Action | Tier |
|---|---|
| Create a worktree, enter it | Allow |
| Remove a worktree | Ask |
| Remove a worktree holding uncommitted changes or unmerged commits | Refuse, unless the user has explicitly said to discard them |

Creating an empty directory on a new branch is not destructive and should not
interrupt the user — that autonomy is the point of the feature. Deleting a tree
that may hold the only copy of work is a different risk, and it keeps its
prompt.

### D5. Worktrees are never removed automatically

If the session ends while still inside a worktree, the user is asked whether to
keep or remove it, and the default is keep. This is the one place where the
agent's autonomy could destroy the user's work, so the default is the
non-destructive one.

The same answer covers a worktree left behind by a session that crashed: there
is no reaper, no age-based sweep, and no cleanup on next launch. A process that
died is not evidence that the work in its tree is finished, and that is exactly
the case where the tree is the only copy. Cleanup is the user's, and the
affordance BuildMax owes them is visibility rather than automation — listing
the repository's worktrees, which of them a live session is in, and what each
one holds that is not committed.

### D6. A dirty starting tree is reported, not resolved

The worktree is created from the current `HEAD`, and the uncommitted files that
did not come along are listed explicitly. Silence is not acceptable: the
conversation may describe code the new tree cannot see.

The worktree is not populated by an automatic stash. Worktrees of one
repository share a single stash stack, so an automatic stash is a hazard to
every other session working in that repository. Refusing to create the worktree
was also rejected: the common case is a clean or irrelevantly dirty tree, and
refusing would force the user into exactly the manual Git detour this feature
removes. A future fork-time worktree faces the same question and should carry
the same answer unless it finds a reason that applies only to forks.

### D7. Delegates inherit the current root unless the model gives them their own

Subagents and background jobs started after a switch use the current root by
default. Those already running keep the root they launched under and say so in
their output — a job already records its workspace. Refusing to switch while
jobs are running would block a switch during a test run, which is when it is
most wanted; silently reparenting a running process is not an option.

A delegate may instead be given its own worktree. This is offered, never
imposed: nothing forces one worktree per subagent, and the model decides per
delegation whether the isolation is worth it. Two writers in one tree is a real
hazard — [local background jobs](local-background-jobs.md) accepted it
deliberately because worktree isolation was not available — but so is a tree
per read-only exploration, which costs disk and a cleanup the user has to do by
hand under D5. The judgement is the model's, on the same footing as deciding
whether a subagent is warranted at all.

### D8. CLI and TUI get the full capability; Desktop only displays the root

The TUI gets the runtime tool, a `/worktree` command, and the current root in
its header. The command is the user-facing side of D5's visibility: it lists
each worktree, which session holds it, and what it holds uncommitted, and it
lets the user remove one from the same view — the only action that follows from
seeing a tree nothing else will reap. Removal there obeys D4: the current tree
and one another session holds are refused, a clean tree is removed on a
confirm, and a tree with work is removed only after a confirm that names what
will be discarded. This is the user's explicit cleanup, not the automation D5
rejects. Desktop shows the current root and does not switch. A Desktop
`Project` is local UI state, not a server entity, and deciding whether a
worktree becomes a Project, a state within one, or nothing Desktop shows is a
product question this record does not need to answer to be useful.

### D9. The model names the worktree and its branch

Names are derived from the work, by the model, at creation time. A fixed scheme
would produce `worktree-1`, `worktree-2`, which tells a user listing them
nothing about which tree holds what — and the model is the only party that
knows what the branch is for at the moment it is created. The user can still
supply a name; the model's is the default, not an override of an explicit one.

Collision is handled by failing, not by silently suffixing: creating a worktree
whose name or branch already exists returns a tool result that says which one
collided, and the model picks another. A silent `-2` suffix would let a model
that meant to return to an existing tree create a second one instead.

### D10. Occupancy is an advisory lock in the worktree's git admin directory

A linked worktree's `.git` is a file pointing at `<repo>/.git/worktrees/<name>/`,
where Git keeps that worktree's own `HEAD`, `index`, and `locked`. That
directory is the right home for an occupancy marker: it is outside the working
tree, so it never appears in the user's `git status`, and `git worktree remove`
and `git worktree prune` delete it along with the worktree, so the marker
cannot outlive what it describes. Other tools already use it the same way —
Claude Code writes a `CLAUDE_BASE` file there.

The marker is an advisory file lock held open by the session's process for as
long as it is in the tree, not a recorded process ID. The operating system
releases it when the process exits, a crash included, so "idle" needs no
staleness heuristic and no defence against PID reuse — which is what makes
D3's refusal safe to enforce and D5's "the user reclaims it" true rather than
a tree locked forever by a session that died. What the marker *contains* —
session ID, start time — exists to name the holder in a refusal message, not
to decide liveness.

Not `git worktree lock`: that is a guard against pruning, persistent by design
and unrelated to which process is working where. Reusing it would conflate two
meanings and leave trees marked locked long after everyone left.

The cost is per-OS code — `flock` against `LockFileEx` — which
`internal/infra/sandbox` already carries for the same reason.

## 6. What The User Sees

A sketch, not a specification of the wording:

```text
user  > 这个重构不小，开个 worktree 并行做

agent > Created .buildmax/worktrees/refactor-tool-roots on branch
        worktree/refactor-tool-roots, from HEAD (a1b2c3d).
        Two uncommitted files stayed behind in the original tree:
        internal/tool/bash.go, docs/ROADMAP.md.
        This session is now working in the worktree.

        … ordinary work: Read, Edit, Grep, Bash, /diff all in the new tree …

user  > 提交了，回主目录吧

agent > Left the worktree; it and its branch are kept at
        .buildmax/worktrees/refactor-tool-roots.
        This session is back in /Users/…/buildmax.
```

The header line showing the current root is what makes D4's allow tier
defensible: the user is never in a position where they cannot tell which tree
the agent is writing to.

## 7. Landing Areas

| Responsibility | Owner |
|---|---|
| The workspace contract both tools and the sandbox resolve against | `internal/util` (`Workspace`, `FixedRoot`) |
| Current root as session state | `internal/agentapp` (`MovableRoot`) |
| Tools reading the root per call rather than per construction | `internal/tool` (`workspaceTool`, `Bash`, `Monitor`, `Task`) |
| Git worktree mechanics: add, list, remove, exclude, and what removal would lose | `internal/infra/git` |
| The occupancy lock the OS releases on exit | `internal/infra/flock` |
| Lifecycle for one session: what may be entered, who occupies it, and the move | `internal/agentapp/worktree` |
| Re-resolving what the root decides, as part of the move | `internal/agentapp` (`sessionRoot`) |
| Runtime tool surface and its permission tiers | `internal/tool` (`Worktree`) |
| `/worktree` panel, footer, and `/diff` following the root | `internal/interface/cli` |
| Rebuilding an agent type's tools against a delegate's root | `internal/agentapp` (`agentTypeToolsAt`) |
| Sandbox writable bind following the root | `internal/infra/sandbox` |
| Recorded workspace and resume behavior | `internal/agentapp/session_manager.go`, `internal/agentapp/session_context.go` |

`internal/core` is unaffected: the root is runtime assembly state, not domain
state, and nothing here gives the agent loop a new concept.

## 8. Staged Delivery

**Phase 1 — the root becomes session state. Implemented.** `util.Workspace` is
the contract, `agentapp.MovableRoot` the state, and the file tools, Bash,
`Monitor`, `Task`, the sandbox's writable bind, the TUI footer, and `/diff` all
read it per call. `util.FixedRoot` serves every surface and test that never
moves. No worktree capability yet, and `--workspace` behaves exactly as before.
This is the phase that could regress existing behavior, so it shipped and is
tested on its own.

**Phase 2 — the worktree lifecycle. Implemented.** `internal/agentapp/worktree`
owns it, `internal/infra/git` the Git mechanics, and `internal/infra/flock` the
occupancy lock. The `Worktree` tool and the TUI's `/worktree` panel are the two
surfaces. D3's containment check, D4's tiers, D5's refusal to delete anything on
its own, D6's dirty report, D9's naming, and D10's lock are all in force.

**Phase 3 — derived configuration. Implemented.** Moving the root and
re-resolving what it decides are one operation, in `sessionRoot.Set`: anything
that moved the root without it would leave the session running one tree's
hooks and skills against another tree's files, and nothing about that looks
wrong from the outside. Hooks re-merge, skills and subagent definitions
re-resolve, the cached tool registries drop, and MCP reconciles. The
`AGENTS.md` layer needed no reload — the prompt is built from the current root
every turn, so it already followed; a test holds that rather than a comment
claiming it. Failures are logged rather than unwound: the move has happened,
and stranding the session between two trees is worse than a degraded layer.

Phase 2 shipped one release ahead of phase 3, which was tolerable only because
a worktree of the same repository normally carries identical workspace
configuration, so the stale snapshot was usually the same snapshot. That
argument does not survive relaxing D3, and it no longer has to: phase 3 has
landed.

**Phase 4 — the rest of the surface. Implemented.** `WorktreeCreate`,
`WorktreeRemove`, and `CwdChanged` fire from the lifecycle, all advisory: the
tool call already passed `PreToolUse`, so a second gate could only leave a
worktree half-created, and a hook that fails never undoes a move that has
happened. Desktop gained only a read-only worktree listing
(`GetSlashWorktrees`) — it renders the project folder, which is its root, and it
does not enable or switch worktrees, so that root cannot move. The user
documentation is in [manual/tools.md](../../manual/tools.md) and
[manual/hooks.md](../../manual/hooks.md).

**Phase 5 — worktrees for delegates. Implemented.** `Task` takes an optional
`worktree` name. The tree is created and its occupancy lock held for as long
as the delegate runs, foreground or background, and the parent's root does not
move: delegating into a tree is not switching to one. The delegate's tools are
rebuilt against it, since the parent's instances hold the parent's root and
handing those over would defeat the isolation. The reply names the tree, which
matters more here than anywhere else — nothing removes it afterwards, so a
delegate's changes would otherwise be somewhere the parent never learns.

## 9. Open Questions

- The measured prompt-cache cost of D2, which should be recorded once the usage
  stats can show it rather than argued from first principles.
- Whether listing is enough to keep worktrees from accumulating. D5 removes
  none of them automatically and delegates can now create more, so the only
  counterweight is that the user can see what exists and what it holds. This
  is the question real use will answer first; if listing turns out to be
  insufficient, the answer is a better listing, not a reaper.

## 10. User Documentation Obligations

This record is rationale. When the phases land, the user-facing half belongs in
`manual/` — how to ask for a worktree, what the agent does with uncommitted
changes, and how cleanup works — and the `/worktree` command belongs in
[manual/cli.md](../../manual/cli.md), whose coverage is enforced by a test.
Neither should restate the decisions above.
