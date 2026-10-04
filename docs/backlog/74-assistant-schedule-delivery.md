---
id: assistant-schedule-delivery
title: Deliver a Schedule's result to a requester through an Assistant
roadmap: R5
source: docs/design/space-assistants.md#11-results-escalation-and-delivery
depends_on: [68-assistant-release-contracts.md]
verification: ["./make test", "./make test mysql", "./make check portal", "./make check docs"]
claim: gougoujiang 2026-10-04
pr:
---

## Outcome

A department's notifications move to its Assistant: a Schedule in the Space can
send its result to a person through the Assistant's bot.

## Scope

- Optional Schedule delivery target: Assistant plus one requester who already has
  a conversation with that Assistant (Telegram bots can only message people who
  started a chat). Validated on save.
- When a fired run ends, send the releasable result through the binding to the
  requester's chat; skip with a recorded reason when the Assistant is paused,
  the requester is no longer in its audience, or the link is inactive.
- Schedule API, OpenAPI, and Portal schedule form.

## Out Of Scope

- Chat or group targets; Workflow-level (non-Schedule) delivery targets.

## Acceptance Criteria

- A fired Schedule with a target delivers releasable fields only.
- Each skip reason is tested and visible on the Schedule's run history.

## Verification

`./make test` on schedule and channel packages, then `./make test`;
`./make test mysql`; `./make check portal`; `./make check docs`.
