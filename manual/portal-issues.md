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
trace, while the page header uses readable status words such as **Done**. When
the agent stopped to ask you something, the status reads **Needs your answer**
and the questions close its output; answer them with **Continue**, in your own
words.

## Create an issue

An **issue** is the user-facing unit of work — the thing you actually want done.
Open **Issues** in the sidebar and choose **New Issue**. An issue has:

- **Title** — a short statement of the work.
- **Description** — the detail an agent needs to act on it.
- **Business Status** — `todo`, `in progress`, or `done`. You set this yourself;
  it is not changed automatically by a run.
- **Owner** — the person accountable for the issue (see below).
- **Executor** — the agent or published workflow selected to do the work (see
  below). In a space that has neither yet, the field says so and offers
  **Create an Agent**: the agent is created without leaving the dialog, and it
  becomes the new issue's executor. A member who cannot create agents is told
  to ask a space owner or admin.

Issues can be nested: from an issue you can add **sub-issues** to break the work
down. Sub-issue status is tracked independently — closing a parent while
sub-issues are still open is allowed and never rolls their status up.

If the Issue is created but saving its initial status, owner, or executor fails,
the dialog says that the Issue already exists and offers **Open created issue**
to finish setup. It does not offer a second Create action for the same Issue.

You can discuss an issue in its comments, where both people and agents leave notes.

## List and Board

**Issues** shows top-level issues as a **List** by default. Choose **Board** to see
the same issues in three lanes — **To do**, **In progress**, and **Done** — each
with its own total and a **Show more** button when it holds more than fit. A
parent's card shows how many of its sub-issues are done; the sub-issues
themselves stay on the parent's detail page.

Filter either view by **Owner** (including **Me**) or **Executor**. The view
and filters are part of the page address, so a reload or a shared link opens
the same projection.

To change an issue's status from the board, use **Move to** on its card. That
is the same status change as editing the issue: it never starts a run and never
changes the owner or executor. If someone else changed the issue after the
board loaded it, the move is refused and the board reloads so you can decide
again. If a lane fails to load, it says so and offers **Retry** — an empty lane
always means the lane really has no matching issues.

## Owner, executor, and running the work

Owner and executor are independent choices, and either, both, or neither can
be set at once:

- **Owner** — the accountable person, including *Me*. Setting an owner never
  starts a run; it only records who is responsible.
- **Executor** — what performs the work, one of:
  - **None** — nothing selected yet.
  - **An agent** — a saved [agent](portal-agents-workflows.md) can run the
    issue in the background.
  - **A workflow** — a published [workflow](portal-agents-workflows.md) can
    run its steps for the issue. Only space owners and admins assign one.

Choose **Edit issue** to change fields, then **Save changes**. Saving only records
the fields you chose. It never starts a run
and never spends your space's execution quota — you can change either as often
as you like while you get the issue ready.

Once an agent or workflow executor is saved, **Run agent** or **Run workflow**
appears on the read view. That button is the only thing that starts a
background run on a worker: it materializes the space's files, runs the agent,
writes any outputs, and reports back — without tying up your browser. A
successful Run keeps you on the issue: it says the run started and links to its
task or workflow run, and the Overview follows the run until it finishes.
Starting a run does not change the issue's business status.

Until an executor is saved, **Run** is shown disabled with the reason and the
next step: **Choose executor** opens the edit form at the Executor field, and
in a space with no agent or published workflow, **Create an Agent** creates one
in place and selects it as the executor for you to save.

## Issue Detail

Open an issue to see its detail view, split into four tabs:

The title, status, owner, executor, and **Latest run** appear before the tabs and
the edit form. Select **Edit issue** when you need to change fields. Run is
available from the read view, so an unsaved executor change cannot start the
wrong work.

**Latest run** is the issue's one answer to "did it work, and what did it
produce?". It is the newest run of either kind — an Agent run or a Workflow
run — and shows its status, what it produced, and a link to open it. What it
produced is the files the run published, and its text: the agent's reply, the
workflow's declared result, or, for a workflow that declares none, the output
of its last step. A long reply is shortened here; **Results** has all of it.
While the run is in flight, it says so and updates until the run finishes.
Runs are ordered by when they started, so retrying an older run does not move
it to the top: the retry stays in that run's place in **Runs**.

- **Overview** — the owner and executor, status, description, and sub-issues.
- **Discussion** — the comment thread, where both people and agents leave notes.
- **Results** — what the runs produced: every [artifact](portal-overview.md) a
  run published, which you can open or download, and, for a run whose only
  output was text, that text, marked as text only. A workflow's declared result
  is listed here too.
- **Runs** — one list of every run on the issue, Agent runs and Workflow runs
  together, newest first, with one count. Open a run for its steps, trace, and
  diagnostics.

From **Latest run** or the Runs tab, an Agent run offers:

- **Stop Run** — while a run is pending or running, you can stop it. A run nobody
  has picked up yet ends immediately; a run a worker is executing is asked to stop
  and finishes as *canceled*, usually within seconds. Either way it keeps whatever
  it had already produced.
- **Retry Run** — once a run is over, you can repeat it with the same
  instructions, which is how you recover from a worker that died or a model that
  timed out without retyping anything. A retry counts against your space's quota
  and leaves the original run's record intact. A run that is a workflow step is
  retried by re-running its workflow, not from here.

The agent's final reply is posted to **Discussion** as its report, with an
**Open Task** link to the run's task. Comments render Markdown. When the agent
stopped to ask you something, the run reads **Needs your answer** and the report
says so. Open the task with **Answer in Task** or the report's **Open Task**,
and reply there with **Continue** — a comment on the issue does not reach the
agent.

An issue a space assistant escalated says so under its description: which
assistant, and for whom (someone outside the space when the person asking is not
a member). Its **Discussion** offers **Reply to requester** beside **Comment**:
it sends what you wrote, up to 4000 characters, to the person's chat through
the assistant's bot and records it on the issue as your comment. It is refused
while the assistant is paused, has no bot, or would no longer answer that
person. The reply does not reach the assistant's model.

## Work an issue on your machine

Issues you own can also be worked locally, where your files and tools are.

- **CLI** — `buildmax issue list` shows your open issues, and
  `buildmax issue start <id>` opens a session the agent scopes to that issue; see
  [the `buildmax issue` commands](cli.md#buildmax-issue).
- **Desktop** — while signed in to a server, the sidebar shows **Issues**: the
  open issues you own across spaces, with each one's description, sub-issues,
  and discussion. **Start chat** switches to the project you pick and opens a
  new chat there with the issue already in the message box, so you can edit it
  before sending. From the same view you can move the issue's status and post a
  comment. The Issues entry does not appear when Desktop is not signed in.

Either way, returning the work is up to you: a comment says what was done, and
a status change says whether it is finished. Planning and assigning work stays
in Portal.

## Next

- Define the agents and plans you assign here: [Agents & workflows](portal-agents-workflows.md).
- Get oriented in the rest of the app: [Portal overview](portal-overview.md).
