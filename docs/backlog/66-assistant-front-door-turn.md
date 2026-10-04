---
id: assistant-front-door-turn
title: Answer requesters through an Assistant's bot as its service account
roadmap: R5
source: docs/design/space-assistants.md#10-the-front-door-turn
depends_on: [64-space-assistant-entity.md]
verification: ["./make test", "./make test mysql", "./make check docs"]
claim: gougoujiang 2026-10-04
pr:
---

## Outcome

A linked BuildMax user in an Assistant's audience can message its bot and get an
answer from the Assistant's persona, which can start roster Agents and run roster
Workflows in the Space as the service account, with the requester recorded.

## Scope

- Gateway handling for Assistant connectors, all before any model call: private
  chat only; link lookup or pairing offer; sign-in window; Assistant and binding
  active; service account active and sponsored; requester active; requester in
  audience. Each refusal is a fixed reply with no Space data.
- `conversation.assistant_id`; Assistant conversations keyed by Assistant, chat,
  and requester; `user_id` is the requester, Space is the Assistant's. Each
  stored message records the Assistant revision.
- Tier 1 turn profile in `internal/service/conversation`: Assistant
  instructions replace the personal opening line while tool guidance and fixed
  disclosure rules stay; Assistant model or the default conversation model;
  separate metered user (requester) and acting user (service account) in the
  turn input; tools limited to the roster with server-side roster checks; no
  ListSpaces; `/new` and `/help` only.
- First reply in a conversation names the operating Space and says it can review
  the conversation.
- Task provenance: `task.requested_by`, `task.assistant_id`,
  `task.assistant_revision`; `created_by` is the service account.

- Starting point: the Gateway already routes a linked person's message on an
  Assistant's bot to `FrontDoor.Answer` in `internal/service/assistant`
  (pairing and the sign-in window already checked), which today only reports
  whether the Assistant is paused. Replace its "not answering yet" branch;
  `Service.Availability` already covers the Assistant, service account, and
  sponsor checks.

## Out Of Scope

- Release filtering of GetTask/GetWorkflowRun output and outcome reports
  (68-assistant-release-contracts.md); until it lands, Assistant conversations
  send no outcome report and Task tools return status only.
- Readable files tool (70), Escalate (72).

## Acceptance Criteria

- Tests for each pre-model refusal, each proving no model call happened.
- A requester outside the Space, in an `all_users` Assistant, starts a roster
  Agent; the Task is in the Assistant's Space with `created_by` = service
  account and `requested_by` = requester; model usage is attributed to the
  requester in that Space.
- A model-supplied agent or workflow id outside the roster is refused by the
  server.
- The personal assistant's tests and prompt are unchanged.
- `docs/design/instant-messaging-channels.md` and
  `docs/design/agent-execution-and-task-threads.md` (and zh mirrors) note the
  Assistant path; `docs/current-state.md` updated.

## Verification

`./make test ./internal/service/channel/... ./internal/service/conversation/...`
first, then `./make test`; `./make test mysql` for new columns;
`./make check docs`.
