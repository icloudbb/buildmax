# Agent Questions To The User

> **简体中文：** [阅读中文镜像](../zh-CN/design/Agent向用户提问.md)

> **Audience:** contributors · **Status:** implemented on the TUI and Desktop
> project chats. Remote Control relay, workers, and richer question shapes are
> deferred (see [Deferred](#deferred)).

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

Blocking is acceptable because the tool is registered only where someone is
present. It is not registered on unattended runs, where blocking would hold a
worker and its lease with nobody to release them. That is the reason workers are
out of scope. It also follows the [Task thread](agent-execution-and-task-threads.md)
rule that a Task waiting for input consumes no worker, and
[Workflow runtime §14](workflow-runtime.md#14-durable-human-requests), which
says a human request is not a blocking Agent call.

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

`AppConfig.EnableAskUser` registers the tool after `BuildAgentTypes`, so no
subagent definition can name it.

| Run | Registered | Why |
|---|---|---|
| TUI | yes | A person is at the terminal |
| Desktop project chat | yes | A person is at the chat |
| Desktop scheduled fire, projectless session | no | Directory-hosted apps have no approval or question handler |
| `buildmax run` (print mode) | no | Nobody answers mid-run; a script reads the final reply |
| Subagent | no | It reports to its parent, which decides whether to ask |
| Worker TaskRun, Portal Conversation, evaluation | no | Unattended; blocking would hold resources with nobody to release them |

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
choosing to say nothing. The question panel sits above the composer. It shows
one question at a time, with tabs when there are several. Option buttons pick
an option, or check it on a multi-select question, which a Confirm button
then sends. Each question also has its own answer field, and a Dismiss button
dismisses the whole set. A digit picks or checks an option unless focus is in
a text field. Like approval keys, question keys work only in the
focused pane. A chat that is not on screen still says it is waiting: an amber
dot marks its tab when another tab is active, its session and project rows in
the sidebar, and the collapsed Projects header. A pending approval sets the
same dot, since both stop the run on the user.

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

## Alternatives Considered

- **End the turn with the question.** The answer would arrive as the user's
  next message. This works on every surface, including workers, with no pending
  state. It was the proposal's recommendation. The maintainer chose the
  blocking form for the interactive surfaces: it is the interaction they use
  daily, and workers were deferred anyway, which removed its main advantage.
- **Widen `ApprovalHandler`.** This would turn a three-value permission
  decision into a general prompt channel and couple two concerns with
  different callers: the permission gate for approval, and a tool for
  questions.
- **A durable human request.** Workflow §14's request entity, with responders,
  expiry, and audit, is the right shape for governed approvals but too large
  for a clarifying question. It waits on Space governance.

## Deferred

- **Remote Control.** A TUI session watched from another device shows no
  question, and a remote user cannot answer one; they can still cancel the
  turn. Relaying the question needs a frame beside `agent.approval` and a
  Portal panel.
- **Workers.** An unattended run cannot block. If Portal Tasks need
  questions, the likely shape is to end the TaskRun with the question in its
  result and answer it by Continue. First measure whether the tool makes
  unattended Agents ask instead of act.
