---
id: assistant-workflow-outcome-report
title: Report Workflow runs an Assistant started to the requester
roadmap: R5
source: docs/design/space-assistants.md#11-results-escalation-and-delivery
depends_on: []
verification: ["./make test", "./make test mysql", "kind"]
claim:
pr:
---

## Outcome

A requester who asks an Assistant to submit something through a roster Workflow
hears how it ended, as they already do for a roster Agent's Task. In the
validation run the leave request Workflow succeeded in 20 s with a releasable
decision, but the requester heard nothing after "submitted for review" and
would have to ask again to learn it.

## Scope

- When a Workflow run started from an Assistant conversation ends, send the
  requester its releasable fields under the Assistant's current roster entry,
  or the fixed failure or cancellation sentence, through the Assistant's bot,
  with the same checks as a Task's outcome report.
- Durable across replicas and restarts, like Schedule delivery.

## Out Of Scope

- Phrasing the result in a follow-up model turn; the validation run found the
  deterministic report clear enough (design §17, question 2).

## Acceptance Criteria

- A succeeded run reports exactly its releasable fields once; a failed or
  canceled one reports the fixed sentence; nothing is sent while the Assistant
  would not answer that requester.
- Unit tests for each case; a kind run shows the report reaching the chat.

## Verification

`./make test`, `./make test mysql` for any store change, then a kind run with
the mock Telegram double.

## Notes

Evidence: docs/design/space-assistants.md §18 (validation run, 2026-10-04).
