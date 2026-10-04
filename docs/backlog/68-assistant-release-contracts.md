---
id: assistant-release-contracts
title: Release only contracted result fields to Assistant requesters
roadmap: R5
source: docs/design/space-assistants.md#8-disclosure-boundary
depends_on: [66-assistant-front-door-turn.md]
verification: ["./make test", "./make check docs"]
claim:
pr:
---

## Outcome

Requesters outside the Space learn only what the Space marked releasable from
the work an Assistant dispatched: never raw run output, error text, transcripts,
or Portal links.

## Scope

- StartTask from an Assistant turn passes the roster entry's `output_schema` as
  the Task's `output_schema`; RunWorkflow relies on the Workflow's.
- GetTask, ListTasks, and GetWorkflowRun in an Assistant turn return status and
  releasable top-level fields of the structured result only.
- Outcome reports for Assistant conversations: releasable fields on success,
  fixed text on failure or cancellation, no link; sent through the Assistant's
  bot. Personal reports unchanged.
- A scripted red-team test set (prompts trying to obtain raw output, other
  requesters' Tasks, non-releasable fields, Space data outside the roster) run
  against a fake model that complies, proving the server, not the model, holds
  the boundary.

## Out Of Scope

- Rephrasing results through a follow-up model turn (design open question 2).

## Acceptance Criteria

- A run whose structured result has releasable and non-releasable fields
  reaches the requester and the front-door model with only the releasable ones.
- A failed run's error text never reaches the requester.
- Red-team tests pass.
- Manual and design (with zh) describe release contracts.

## Verification

`./make test ./internal/service/conversation/... ./internal/service/channel/...`,
then `./make test`; `./make check docs`.
