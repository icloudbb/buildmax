---
id: space-assistant-validation
title: Validate a department-shaped Space Assistant end to end
roadmap: R5
source: docs/design/space-assistants.md#15-phasing
depends_on: [68-assistant-release-contracts.md, 70-assistant-readable-files.md, 72-assistant-escalation.md, 74-assistant-schedule-delivery.md]
verification: ["kind", "exploratory"]
claim:
pr:
---

## Outcome

Evidence for whether Space Assistants work well enough to widen: a real
Telegram bot, an HR-shaped Assistant, and measured accuracy, leakage, latency,
and escalation.

## Scope

- On an ephemeral kind cluster with a real model (GPT-5.6 Luna by default), set
  up an HR-shaped Assistant: readable policy files, one roster Agent, one
  roster Workflow, an `all_users` audience, and a real Telegram bot.
- Run a fixed question set (answer accuracy), a scripted red-team set (leakage
  outside scope and of other people's data), and measure front-door and worker
  latency and escalation rate.
- Ask a Space owner, shown only the publish statement, what the audience can
  learn; record whether they are right.
- Follow docs/contribute/exploratory-testing.md; record model and cost.
- Update the design record's status and answer its open questions 2 and 3 from
  the evidence; fix or file what the run finds.

## Out Of Scope

- New capability beyond fixes the run finds.

## Acceptance Criteria

- Measurements recorded in the design record (and zh mirror).
- Every defect found is fixed or has its own backlog task.

## Verification

`BUILDMAX_KIND_EPHEMERAL=1 ./make kind up`, the exploratory run, then
`./make kind down`.
