# Conversations & issues

Conversations are how you talk to BuildMax in the Portal; issues are how work gets
tracked and handed to an agent. This page walks through both.

## Start a conversation

**Chat** is the front door. Type what you want done in the composer — for example,
*"Help me analyze last month's sales data"* — and send it (Enter to send,
Shift+Enter for a new line). A conversation can answer you directly, or, when the
work is bigger, start background work and show you the result when it's ready.

Recent conversations are listed on Chat so you can pick one back up. One that
started outside Portal is marked with where it came from, such as **Telegram**
for a [chat app](chat-apps.md) conversation. The assistant works in the current
space and can list your spaces; switch spaces in the sidebar to work elsewhere.
Use the **Recent Conversations** and **Files** tabs to switch between the list
and a link to the space's working files; Left and Right Arrow switch tabs when
one has keyboard focus. A background task started by a conversation appears
in the thread with its status and output. Its card offers **Stop** while it is
running, **Run again** when it ends, and **Run details** for the trace.
If its action fails, the card keeps the error visible. If the task list cannot
load, Chat shows a warning and **Retry tasks** without hiding the conversation.

When you run an agent directly, its Task page keeps the input and output for
each turn together. **Continue** sends new instructions; **Retry last run**
repeats the previous turn. **Details** holds the run's origin, timing, ID, and
trace, while the page header uses readable status words such as **Done**.

## Create an issue

An **issue** is the user-facing unit of work — the thing you actually want done.
Open **Issues** in the sidebar and choose **New Issue**. An issue has:

- **Title** — a short statement of the work.
- **Description** — the detail an agent needs to act on it.
- **Business Status** — `todo`, `in progress`, or `done`. You set this yourself;
  it is not changed automatically by a run.
- **Owner** — the person accountable for the issue (see below).
- **Executor** — the agent or workflow selected to do the work (see below).

Issues can be nested: from an issue you can add **sub-issues** to break the work
down. Sub-issue status is tracked independently — closing a parent while
sub-issues are still open is allowed and never rolls their status up.

If the Issue is created but saving its initial status, owner, or executor fails,
the dialog says that the Issue already exists and offers **Open created issue**
to finish setup. It does not offer a second Create action for the same Issue.

You can discuss an issue in its comments, where both people and agents leave notes.

## Owner, executor, and running the work

Owner and executor are independent choices, and either, both, or neither can
be set at once:

- **Owner** — the accountable person, including *Me*. Setting an owner never
  starts a run; it only records who is responsible.
- **Executor** — what performs the work, one of:
  - **Unassigned** — nothing selected yet.
  - **An agent** — a saved [agent](portal-agents-workflows.md) can run the
    issue in the background.
  - **A workflow** — a published [workflow](portal-agents-workflows.md) can
    run its steps for the issue.

Choose **Edit issue** to change fields, then **Save changes**. Saving only records
the fields you chose. It never starts a run
and never spends your space's execution quota — you can change either as often
as you like while you get the issue ready.

Once an agent or workflow executor is saved, **Run workflow** or **Run agent**
appears on the read view. That button is the only thing that schedules a
background run on a worker: it materializes the space's files, runs the agent,
writes any outputs, and reports back — without tying up your browser. A
successful Run takes you straight to the run it started.

## Issue Detail

Open an issue to see its detail view, split into four tabs:

The title, status, owner, executor, and latest result appear before the tabs and
the edit form. Select **Edit issue** when you need to change fields. Run is
available from the read view, so an unsaved executor change cannot start the
wrong work.

- **Overview** — the owner and executor, status, description, sub-issues, and
  a summary of the most recent run.
- **Discussion** — the comment thread, where both people and agents leave notes.
- **Results** — the latest result and every saved [artifact](portal-overview.md)
  a run produced. Larger outputs are stored as artifacts you can open or
  download.
- **Runs** — the full execution history for the issue.

From the Overview or Runs tab, a run in progress offers:

- **Stop Run** — while a run is pending or running, you can stop it. A run nobody
  has picked up yet ends immediately; a run a worker is executing is asked to stop
  and finishes as *canceled*, usually within seconds. Either way it keeps whatever
  it had already produced.
- **Retry Run** — once a run is over, you can repeat it with the same
  instructions, which is how you recover from a worker that died or a model that
  timed out without retyping anything. A retry counts against your space's quota
  and leaves the original run's record intact. A run that is a workflow step is
  retried by re-running its workflow, not from here.

## Next

- Define the agents and plans you assign here: [Agents & workflows](portal-agents-workflows.md).
- Get oriented in the rest of the app: [Portal overview](portal-overview.md).
