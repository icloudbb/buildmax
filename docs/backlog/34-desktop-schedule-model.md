---
id: desktop-schedule-model
title: Keep a Desktop schedule on the model it was saved with, and show that model
roadmap: R6
source: docs/design/ui-experience-program.md#phases
depends_on: []
verification: ["./make test", "./make e2e desktop", "./make check desktop", "./make e2e desktop-ui"]
claim:
pr:
---

## Outcome

A person can see which model a Desktop schedule will run before it fires.
Signing in to a server never silently moves unattended work to a different
model. This task carries Desktop audit finding D4 (Major).

## Findings

**D4 (Major, the person is misled). A local schedule silently runs on a
different model after sign-in.**

- Screen: Schedules and the schedule's run view.
- Reproduction:
  1. In local mode, create a schedule with Model "Default (GPT-5.6 Luna)".
     The audit used `*/5 * * * *` in Asia/Shanghai.
  2. Sign in to a server before the schedule fires.
- Actual:
  - The run's composer shows the model **BuildMax smoke** (the server's mock),
    and the reply is "deployment smoke ok".
  - Nothing on the schedule card says which model will run, or that signing
    in changed it.
- Unattended work moved to another provider's model without notice.

## Verified Facts

These were checked on `main` at `e36fa722`.
`internal/interface/desktop/schedule.go` stores `Model`, which is empty when
"Default" is chosen. At fire time it calls `sess.SetModel(r.Model)` only when
`Model` is not empty. "Default" is therefore resolved when the schedule
fires. After sign-in it resolves to the server's managed default.

## Scope

- When a schedule is saved with "Default", resolve it to the concrete model
  current at save time, store that model, use it at fire time, and show it on
  the schedule. The maintainer chose this "show and pin" behavior on
  2026-10-07, accepting that a later change of the default does not move
  existing schedules.
- Show the schedule's model on its card and in the edit dialog.
- If the stored model cannot run in the current mode, for example after
  sign-in, the card says so before the next fire. The fire then fails with
  that reason instead of substituting another model.
- Record the model used on each run, so the run view and Recent runs agree
  with the card.

## Out Of Scope

- The rest of the schedule dialog and list, which is
  [task 54](56-desktop-schedule-form.md).
- Portal's server-side schedules.

## Acceptance Criteria

- A schedule saved with "Default (GPT-5.6 Luna)" in local mode shows GPT-5.6
  Luna on its card. After sign-in it either still runs GPT-5.6 Luna, or its
  card says before the next fire that it cannot, and that fire fails with the
  reason.
- A Go test covers resolving at save time and using the stored model at fire
  time across a mode change.

## Verification

1. `./make test` for `internal/interface/desktop`.
2. `./make e2e desktop`, because schedules cross the bridge.
3. `./make check desktop`, then `./make e2e desktop-ui`.

## Notes

Audit context: the phase 0 Desktop audit ran on 2026-10-06 against `main` at
`e98efc7a`. It drove `./make run desktop-dev` through the browser bridge on
macOS. Its `settings.yaml` was seeded from the contributor's home, so real
models were configured. Sign-in used the Portal audit's kind cluster as
`alice@buildmax.local`. The schedule fired at 14:45:55 and was deleted
afterwards. The operator was an Agent with repository knowledge. The full
report is kept in history at
[`718a6ab3`](https://github.com/icloudbb/buildmax/blob/718a6ab3969d35bdba7f66013d8ccfefe7549af8/docs/contribute/exploratory-runs/2026-10-06-desktop-ui-journey-audit.md).
