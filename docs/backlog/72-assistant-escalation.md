---
id: assistant-escalation
title: Escalate unanswerable Assistant requests to an Issue and reply from it
roadmap: R5
source: docs/design/space-assistants.md#11-results-escalation-and-delivery
depends_on: [66-assistant-front-door-turn.md]
verification: ["./make test", "./make check portal", "./make e2e kind", "./make check docs"]
claim: gougoujiang 2026-10-04
pr:
---

## Outcome

When an Assistant cannot answer, a person in the Space picks the request up as
an Issue and the requester hears back in the same chat.

## Scope

- Escalate tool: opens an Issue in the Assistant's Space created by the service
  account, recording requester, Assistant, and conversation; tells the requester
  a person will follow up.
- Issue action "Reply to requester" (API + Portal) for Space members: sends text
  through the Assistant's bot to the requester's chat, recorded on the Issue and
  audited. Refused if the Assistant is paused or the binding is gone.

## Out Of Scope

- Routing replies back into the model's conversation context.

## Acceptance Criteria

- Escalation creates exactly one Issue per call with provenance visible in
  Portal.
- A member's reply reaches the requester's chat and appears on the Issue.
- Browser test covers the reply action.

## Verification

`./make test`, `./make check portal`, `./make e2e kind`, `./make check docs`.
