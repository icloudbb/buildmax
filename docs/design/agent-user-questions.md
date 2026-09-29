# Agent Questions To The User

> **简体中文：** [阅读中文镜像](../zh-CN/design/Agent向用户提问.md)

> **Audience:** contributors · **Status:** implemented on the TUI (answerable
> from Remote Control viewers too) and Desktop project chats, and in a deferred
> form for worker TaskRuns. Open questions are under [Deferred](#deferred).

## Contents

- [Problem](#problem)
- [Decision](#decision)
- [Contract](#contract)
- [Where It Is Registered](#where-it-is-registered)
- [Surfaces](#surfaces)
- [Lifecycle And Failure](#lifecycle-and-failure)
- [Alternatives Considered](#alternatives-considered)
- [Deferred](#deferred)

## Problem

An Agent regularly reaches a decision that belongs to the user: an ambiguous
requirement, a choice between approaches with different trade-offs, or a fact
it cannot find. Before this change it could only ask in prose and end its turn.
The runtime then had to guess that a question had been asked: the turn digest
offers a suggested reply only when `asksUser` finds a question mark near the end
of the reply. The user read the question, typed an answer, and started a new
turn.

The demand comes from use rather than inference. Claude Code ships a
multiple-choice question tool, and the maintainer uses it routinely. Picking one
of the Agent's options is markedly faster than reading prose and typing a reply,
and it keeps the Agent moving on decisions it should not make alone.

## Decision

Add one tool, `AskUser`. It blocks inside the tool call until the person at the
session answers, and the answer comes back as the tool's result, so the turn
continues. This matches the interaction the maintainer relies on, and keeps the
question and its answer together as one call and one result in the history.

Blocking is acceptable because it happens only where someone is present. An
unattended worker TaskRun must not block: that would hold a worker and its
lease with nobody to release them, against the
[Task thread](agent-execution-and-task-threads.md) rule that a Task waiting for
input consumes no worker, and
[Workflow runtime §14](workflow-runtime.md#14-durable-human-requests), which
says a human request is not a blocking Agent call. A worker therefore gets the
tool in a deferred form: asking ends the turn, the run finishes with the
questions, and the user answers in their own words by continuing the Task. The
answer arrives as the next user message rather than as the tool's result, which
is the one thing the deferred form gives up, and the Task thread already
carries that message.

The mechanism mirrors tool approval rather than extending it. Approval answers
one of three fixed decisions from the permission gate; a question carries free
text and options and is asked by a tool. The two share a shape (a per-run
handler, a request id, cancellation through the run context) but not a type.

## Contract

- **Arguments.** `questions` holds one to four questions, asked together as
  one set. An Agent often needs several decisions at once, each in a different
  form, and one set spares the user a round trip per decision. Each question
  has a required `question` of at most 1000 characters and an optional
  `header` of at most 24 characters, shown as its tab. It may also have up to
  four `options`, each a `label` of at most 80 characters and an optional
  `description` of at most 200, and `multi_select`, which needs at least two
  options. With options and without `multi_select`, the user picks one
  option. With `multi_select`, the user checks several. Without options, the
  user answers in their own words, which every form also allows. Question
  texts and option labels must be distinct, compared without case.
  `agent.ValidateQuestions` owns these bounds.
- **Result.** `agent.Answer.Values` holds one answer per question, in order: a
  label, the checked labels joined with `, `, or the user's own words. The
  tool returns `The user answered:` followed by a `Q:`/`A:` pair per question,
  so each answer stays attached to the question it answers. A dismissal covers
  the whole set and returns an instruction not to ask again and to proceed on
  stated judgment. A run with no questioner returns an
  instruction to proceed on stated judgment instead of failing.
- **Permission.** `DefaultAction` is `Allow`, because a prompt asking
  permission to show a prompt helps nobody. A configured deny still wins.
- **Scheduling.** `Access` is `AccessWrite`, so the call never shares a
  parallel group: it holds the run, and a surface shows one question set at a
  time.
- **Plumbing.** `agent.UserQuestioner` is the per-run interface. A surface
  passes it as `RunPromptOpts.Questioner`, and `RunLoop` installs it on the
  context the tool reads. The registry is cached per model, so the tool cannot
  hold one. `RunLoop` always installs the value, including nil. A subagent runs
  on its parent's tool-call context, and a nil there clears the inherited
  questioner instead of letting a delegate reach the parent's user.
- **Prompt layer.** A run with the tool also gets an `ask_user` system-prompt
  layer. It tells the model to use `AskUser` instead of ending its reply with
  a question, to ask several related decisions in one call, and to put its
  recommended option first. The layer exists
  because the tool description alone was not enough. In a real-model Desktop
  run (GPT-5.6 Luna, 2026-09-27), "Add a LICENSE file to this project" got a
  prose question listing four licenses and an ended turn. With the layer, the
  same prompt got an `AskUser` call with three options; the typed answer came
  back as the result and the Agent wrote the file in the same turn.
- **Hook.** Before the question goes up, `RunLoop` fires `Notification` with
  kind `user_question`, carrying the call id and the question texts, as the approval
  gate fires `approval_required`. A notifier can therefore tell "needs a
  decision" apart from "done".

## Where It Is Registered

`AppConfig.AskUser` registers the tool after `BuildAgentTypes`, so no subagent
definition can name it, and says which form: `AskUserInteractive` or
`AskUserDeferred`.

| Run | Registered | Why |
|---|---|---|
| TUI | interactive | A person is at the terminal |
| Desktop project chat | interactive | A person is at the chat |
| Worker TaskRun | deferred, when the server allows it | Nobody is at the run, but someone continues the Task |
| Workflow step TaskRun | deferred | The question becomes a request on its workflow run, whose answer continues the Task |
| Desktop scheduled fire, projectless session | no | Directory-hosted apps have no approval or question handler |
| `buildmax run` (print mode) | no | Nobody answers mid-run; a script reads the final reply |
| Subagent | no | It reports to its parent, which decides whether to ask |
| Portal Conversation, evaluation | no | Its reply already reaches the user; evaluation has nobody to answer |

The server sets `ask_user` on every run `GET /api/worker/task-runs/{id}`
hands out. A Workflow step's questions become a durable request on its run
([Workflow runtime §14](workflow-runtime.md#14-durable-human-requests)): the
node waits without a worker, and the answer, given in the run view, continues
the step's Task, which cannot be continued directly. An evaluation control
plane sends no such field, so the tool stays off there.

A run on an enabled app that supplies no questioner still offers the tool, and
the tool tells the model that nobody can answer.

## Surfaces

**TUI.** `TUIQuestionHandler` sends the question set to the program and
blocks on a buffered channel. While the panel is up it owns the keyboard. Each
question has a row per option and a last row that holds its own answer field,
so an answer of the user's own is typed under the question it answers, not in
the chat input. That fixes the first version, where typed answers went to the
chat input at the bottom of the screen, far from the question. The chat input
and its "queue message" hint are hidden while the panel is up, so there is
one place to type. Up and down
move between rows, and the field takes typing when the cursor is on it.
Enter picks the highlighted option or sends the typed answer. On a
multi-select question, Space or a digit checks an option, and Enter sends
what is checked plus anything typed. Outside the field, a digit picks an
option outright. With several questions, a tab row shows each header with a
check mark once answered. Answering moves to the next unanswered question,
and Tab or Shift+Tab switch between questions. The set is delivered once
every question has an answer. Esc dismisses the set. Each question and its
answer are printed to scrollback.

**Desktop.** `runQuestioner` is bound per run beside `runApprover`. It emits
`desktop/question-request` with `question_id`, `project_id`, `session_id`, and
`questions`. The frontend answers with
`RespondQuestion(question_id, answers, declined)`, one answer per question. Pending questions share the
per-id bookkeeping (`pendingAnswers[T]`) that approvals use, so an id is
answered at most once and a stale answer reaches no run. An empty answer that
is not a dismissal is refused, because the Agent would read it as the user
choosing to say nothing. The question panel frames `QuestionForm` from
`@buildmax/gui`, the form Portal's Remote Control page also uses, and sits
above the composer. It shows
one question at a time, with tabs when there are several. Option buttons pick
an option, or check it on a multi-select question, which a Confirm button
then sends. Each question also has its own answer field, and a Dismiss button
dismisses the whole set. A digit picks or checks an option unless focus is in
a text field. Like approval keys, question keys work only in the
focused pane. A chat that is not on screen still says it is waiting: an amber
dot marks its tab when another tab is active, its session and project rows in
the sidebar, and the collapsed Projects header. A pending approval sets the
same dot, since both stop the run on the user.

**Remote Control.** A TUI session with Remote Control on forwards each set,
exactly as it forwards approvals: `agent.question` carries the set in the
`agent.Question` shape, `agent.question_resolved` retires it, and a viewer's
`agent.question_response` answers it. The Portal session page renders the same
form Desktop does, `QuestionForm` from `@buildmax/gui`, so the two surfaces
cannot drift. The first answer wins: the local panel and the remote copy both
deliver through one pending entry, the loser reaches no run, and the settled
set is announced so the other side dismisses it. A remote answer is checked
against the set it claims to answer (one non-empty answer per question) before
it is delivered, and it is printed to the TUI scrollback like a local one.

**Workers.** A worker TaskRun the server allows gets `NewDeferredAskUser`, whose
description says calling it ends the turn, and a prompt layer that tells the
model to ask only when blocked and to decide and state an assumption
otherwise. Its questioner records the set and answers `Deferred`. The loop
ends the turn once the current tool batch finishes, with no further model
call and no structured-output extraction. The run then finishes `SUCCEEDED`:
the questions are appended to its output under "Waiting for your answer", and
they travel as `questions` on the terminal report. The server keeps a bounded
JSON array only from a successful run, stores it on `task_run.questions`, and
projects `awaiting_answer` onto the Task. The next run clears it as it is
created.

The user answers by continuing the Task with plain text. The session restore
puts the question, the tool result, and the answer in order in front of the
model. Every reader of a run's output shows the questions with nothing new to
render: the Task thread, the Issue report comment, and a chat channel's outcome
message, which says the task is waiting for an answer. Portal labels such a
Task "Needs your answer" and changes the composer placeholder accordingly, on
the Task page and on an Issue's latest outcome. The Issue and channel reports
keep the end of a long output, where the questions are, rather than cutting it.
The Issue report also says the answer goes to the Task: its thread is where the
questions appear, but a comment there does not continue the run.

## Lifecycle And Failure

- **Cancellation.** A cancelled run withdraws its pending question and returns
  the context error. The TUI dismisses the panel. On Desktop the stream's end
  clears the panel, and a late `RespondQuestion` fails.
- **No timeout.** A question waits as long as the run does. A person who
  stepped away is still the one who has to decide, and cancelling the turn is
  always available.
- **History and trace.** The call and its result form an ordinary tool pair, so
  the question and answer survive in the session history and the bounded,
  redacted trace, with no separate store.
- **Loop guard.** An identical repeated question is blocked like any other
  repeated call.
- **Unanswered in a worker.** Nothing expires and nothing holds a worker. The
  Task stays waiting until someone continues it or starts over. A worker's
  report is untrusted input, so a malformed or oversized question set is
  dropped and the run's output still carries the questions.

## Alternatives Considered

- **End the turn with the question everywhere.** The answer would arrive as
  the user's next message, on every surface, with no pending state. It was the
  proposal's recommendation. The maintainer chose the blocking form for the
  interactive surfaces, because it is the interaction they use daily. Ending
  the turn is kept where it is the only safe option: worker TaskRuns.
- **Widen `ApprovalHandler`.** This would turn a three-value permission
  decision into a general prompt channel and couple two concerns with
  different callers: the permission gate for approval, and a tool for
  questions.
- **A durable human request for every question.** Workflow §14's request
  entity is the answer path for a Workflow step, because nobody continues a
  step's Task directly. For an ordinary Task, continuing it with the answer
  already is that path, so a second entity would duplicate it.

## Deferred

- **Asking rate in unattended runs.** A worker Agent that asks where it could
  have decided stops work nobody resumes until they read it. The deferred
  description and prompt layer steer against that; evaluation keeps the tool
  off, so it measures acting, not asking. Measuring the asking rate on real
  Space work is still open.
