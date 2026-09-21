# Sessions & traces

BuildMax keeps two different records of what happened, for two different
questions: the session is your resumable conversation, and the trace is the
detailed log of everything one run actually did.

| | Session | Trace |
|---|---|---|
| Answers | "What did we talk about?" | "What did it actually do?" |
| Contains | The conversation, resumable | Every LLM call and tool call in one run |
| Lives in | `<BUILDMAX_HOME>/sessions/<id>/` | the same folder, under `traces/` |
| Read by | You, and the agent on resume | You, when something went wrong |

## Sessions

Each session is a folder, and everything about it lives inside:

```text
<BUILDMAX_HOME>/sessions/
  index.json                    what the picker reads
  <id>/
    meta.json                   title, workspace, model, token and cost totals
    history.jsonl               the conversation, one record per line
    traces/<run_id>.jsonl       one file per run
```

`history.jsonl` is append-only: each message, tool call, and tool result is
written as it happens rather than the whole file being rewritten at the end of a
turn. That is what lets an interrupted run be picked back up — and what lets
BuildMax tell a tool call that never started from one that may already have
changed something on disk. When it resumes a session that stopped mid-tool, it
says so rather than assuming either way.

Token counts and cost cover the whole session: every turn, the title-generation
call, each compaction, and any work the run delegated to a subagent.

Titles are generated from the first exchange, which is why `/sessions` is
readable rather than a list of ids.

### Resuming

```bash
buildmax --continue              # this directory's most recent session
buildmax --continue --project    # widen to every directory of this project
buildmax --resume <session-id>   # a specific one
buildmax --session-id <uuid>     # load if it exists, otherwise create it
```

`--continue` means the newest session recorded in the directory you are in. The
newest session on the machine is rarely the one you want, because it follows
whichever repository you touched last — and the newest one in a *sibling
worktree* is not it either, since continuing there would move your working root
out from under you. When this directory has no sessions but the project does,
`--continue` says how many and names `--project`; widening then prints the
directory it will run in before the first turn.

The `/sessions` picker is scoped to the project — a repository including all of
its worktrees, or a plain folder — and may cross directories, because you are
reading the list. A session recorded in another tree is marked with that tree's
name, and resuming it says which root it will actually run in.

`--resume <id>` still looks a session up anywhere, but it resumes in the
directory that session ran in, and refuses to continue a session that belongs to
a different project rather than moving it.

`--session-id` is the one to script with: it makes a run idempotent against a
known id instead of depending on what happens to be most recent.

In the TUI, `/sessions` opens the picker, showing this project's sessions;
press `a` for every project on the machine. The conversation is written as it
happens, so an interrupted run keeps everything up to the moment it stopped —
not just up to the last completed reply.

A session can be open in one place at a time. Opening one that another window or
process is already running reports that it is in use rather than letting two
runs write over each other.

### Rewinding

`/rewind` takes one of your prompts back so you can say it differently. Pick it
from the list, and it leaves the conversation along with everything that came
after it — and comes back in the input box, ready to edit and send again.

The list holds the prompts you typed. Not the agent's replies, since there is
nothing to hand back from one; not background events, which arrive as messages
you never wrote; and not the first prompt of the session, which has nothing
before it to return to — start a new session for that. If the input box already
holds a draft, that draft is kept and the rewound prompt is not restored; the
report says so. Only the text comes back, so images the prompt carried are named
as not having.

**It moves the conversation. It does not undo what the conversation did.** A
turn that edited a file, ran a command, or called an API leaves all of that in
place — the agent's history goes back, the workspace does not. The picker says
which tools ran in the part you are about to remove, before you choose, and
repeats it afterwards, so you can put those effects right yourself if you need
to.

Nothing is deleted. The messages you rewind past stay on disk, and a new reply
after a rewind starts a new branch rather than overwriting the old one.

### Forking

`/fork` copies the history up to a chosen message into a **new session** and
switches you to it, instead of changing this one. The original is untouched and
still there.

Its list is the wider one: any message a turn ended on, yours or the agent's,
including the newest — branching off from where you already are is the common
case. A reply that asked for a tool is mid-turn and is not offered, because a
copy that stopped there would hold a tool call with no result.

Use it to try a second approach without losing the first. The two sessions are
independent from that point on — deleting one never affects the other.

The same caveat applies from the other side. Work that happened after the fork
point really did touch the workspace, and the new session's history does not
contain it, so the agent there will not know it happened. The picker names those
tools before you choose.

### Where to find them

In the TUI they are the `/rewind` and `/fork` commands. In Desktop they are one
**History** button in the chat status bar, with a tab for which of the two you
want; each tab lists the messages that operation can be pointed at.

Desktop refuses both while a run is in flight and says so; stop the run first.
Opening the picker still works — it only reads.

### Compaction

When a conversation approaches the model's context window, older messages are
compacted into a summary, recorded in the journal as a `compaction` record
naming exactly which messages it covers. The `pre_compact` hook can block this,
and `post_compact` observes it — see [Hooks](hooks.md).

`/compact` in the TUI does the same thing when you ask rather than when the
window fills, and keeps a much shorter tail verbatim. Use it before handing the
agent a new task in a long session: what the summary covers is no longer
quotable, only summarized. The summarizing model call is charged to the session
like any other, and the messages themselves stay in the session file — the
summary replaces what is sent to the model, not what was recorded.

## Traces

Every run writes one JSONL file:

```text
<BUILDMAX_HOME>/sessions/<session_id>/traces/<run_id>.jsonl
```

Run ids are prefixed `rt_`. The file opens with a `run_start` record and three
records describing what the run started with, then one record per event, then a
terminal `run_end`:

| Record | Emitted when |
|---|---|
| `run_start` | The run begins |
| `sandbox_boundary` | Always, right after `run_start` — reports the boundary the run actually ran under. `"sandboxed": false` means nothing confined the run's `Bash` commands |
| `context_sources` | Always — every source the run was assembled from, named by kind: the instruction layers and how large each was, the project memory index it carried with its entry count and rendered size, and whether a compaction summary stood in for messages. Counts and sizes only; no content. Which memory bodies a run went on to read, and any write, are tool calls in the session journal rather than a second description here |
| `plugins` | Always — which plugins the run loaded, and for one installed from a Git checkout its commit and whether the working tree was dirty |
| `llm_start` / `llm_end` | Each model call |
| `tool_start` / `tool_end` | Each tool call |
| `tool_denied` | A tool call was blocked — by a hook, or by permission |
| `context_compacted` | The conversation was compacted, with what the summarization itself cost |
| `run_end` | The run finished, with its totals, an error message if it failed, and a `delegated` breakdown when it started subagent runs |

Because it is JSONL, ordinary tools work:

```bash
# every tool call in the last run
jq -r 'select(.type=="tool_start") | .tool_name' \
   ~/.buildmax/sessions/<session>/traces/<run>.jsonl

# what got blocked
jq 'select(.type=="tool_denied")' ~/.buildmax/sessions/<session>/traces/*.jsonl

# how the run ended
jq 'select(.type=="run_end")' ~/.buildmax/sessions/<session>/traces/<run>.jsonl
```

### Bounded and redacted

Free-text fields are truncated and common secret shapes are redacted before
anything is written. A trace is bounded in record count too — once the cap is
reached, further records are dropped, but `run_end` is still written so the file
always terminates properly.

Tracing **fails open**: a trace that cannot be written never breaks or slows the
run.

### Coverage and disabling

Traces attach at the single point every surface runs through, so CLI, TUI,
Desktop, evaluation, and worker runs all produce them.

```bash
BUILDMAX_TRACE_DISABLED=1 buildmax -p "…"
```

On by default. Turn it off if you have a reason; it is the record you will want
when a run does something surprising.

A subagent's run is traced beside its parent's, in the same session directory,
with `is_subagent: true` and `parent_run_id` naming the run that delegated to it.

## Statistics

`buildmax info` folds both records into one answer for a session, and adds what
the session's project remembers:

```bash
buildmax info                   # the most recent session
buildmax info <session-id>      # a specific one
buildmax info --json            # the full record, including the truncated tail
```

It reports spend with the cache breakdown and what caching saved, how close the
session came to its context window, how many bytes each tool put back into that
window, the split between model time and tool time, and how much of the run a
delegation did.

In the TUI and Desktop, `/info` has three tabs. `session` shows the same figures
for the session on screen, from the live session rather than the last saved
copy. `tree` shows the complete local fork tree containing that session, marks
the current branch, and keeps a missing-source placeholder when a parent was
deleted. It derives the tree from the session index instead of reopening every
session. `memory` lists what the project remembers; in the TUI, `enter` opens a
memory to read the reason behind it. See
[Project instructions & memory](project-instructions.md).

Tokens and cost come from the session file; timings and per-tool detail come
from the traces. Where a trace is missing, those lines say so rather than
reporting zero — see [CLI & TUI reference](cli.md).

## Related

- [Hooks](hooks.md) — acting on events rather than recording them
- [CLI & TUI reference](cli.md) — `--output jsonl` for live events on stdout
