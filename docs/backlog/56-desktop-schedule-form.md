---
id: desktop-schedule-form
title: Make the Desktop schedule dialog and list readable, correct, and fully reachable
roadmap: R6
source: docs/design/ui-experience-program.md#phases
depends_on: [20-desktop-gui-convergence.md]
verification: ["./make test", "./make check desktop", "./make e2e desktop-ui", "./make e2e visual"]
claim:
pr:
---

## Outcome

Creating a local schedule shows its buttons and errors without scrolling, and
shows times in the zone the person chose. Errors are stated in words and
clear once fixed. The list shows what matters about each schedule. This task
carries Desktop audit findings D10, D11, D12, and D13.

## Findings

**D10 (Minor). The New Schedule dialog hides its own buttons.**

- Screen: Schedules → **New Schedule**.
- Actual: at 1280×860 the footer (**Create schedule**, Cancel) and the
  validation message sit below the dialog's scroll fold. The native default
  window is shorter still, at 800 px.

**D11 (Minor). Schedule times are hard to read.**

- Actual:
  - The timezone defaults to `UTC`, while "Next runs" lists local times with
    no zone label. `0 9 * * *` UTC appears as "17:00".
  - The working directory defaults to the home folder, even with a project
    open.
  - A paused task still shows "Next: 14:50".

**D12 (Minor). Schedule errors are raw and stale.**

- Reproduction: submit the dialog empty, then enter the cron "every 5
  minutes".
- Actual:
  - The cron error is parser text: `cron "every 5 minutes": expected exactly
    5 fields, found 3: [every 5 minutes]`.
  - "a prompt is required" stays on screen after the prompt is filled in.

**D13 (Minor). The Schedules list cuts off what matters.**

- Actual:
  - The working-directory path is cut at its end, which hides the folder name.
  - The cron expression is shown raw.
  - The run status is the raw uppercase `DONE`.

## Scope

- **D10.** The dialog is `InfoModal` from `components/Modals.jsx`, which task
  20 rebuilt on gui's `BaseModal` in its large form (`modal--large`) with gui
  `Button`s in a `modal__actions` row. It is not a `FormModal`: the browse
  button, the model list that keeps an unavailable pinned model, and the live
  cron preview are more than `FormModal`'s text, select, and textarea fields.
  The large modal grows to the window height less 2 rem and then scrolls the
  whole form, the action row included; gui pins the action row to the bottom
  only below 768 px. Checked after task 20 (macOS Chromium through the visual
  harness, five next runs shown, no error): at 1280×860 **Create schedule**
  ends 99 px above the window's bottom edge, and at 1280×800 it is visible
  with 9 px of the dialog's height to spare, so one line of validation
  message pushes the footer into the scrolling region there. Make the footer
  and the message visible together at 800 px, for example by keeping the
  large modal's action row stuck to the bottom of its scrolling body, which
  would also serve Portal's large forms.
- **D11.** Default the timezone to the system zone, and label "Next runs" with
  the schedule's zone. Default the working directory to the open project when
  there is one. A paused schedule shows "Paused", not a next time.
- **D12.** Report cron and required-field errors in words, all at once, and
  clear each one when its field changes. Keep the Go side's validation
  authoritative, and map its conditions to catalog messages.
- **D13.** Truncate paths from the start, so the folder name stays. Show the
  next run, or "Paused", beside the cron. Show run status through a translated
  label, using gui's `StatusLabel`.

## Out Of Scope

- Which model a schedule runs. A schedule already keeps the model it was saved
  with and its card shows it, as
  [current state](../current-state.md) describes; keep that line on the card.
- Portal's schedule form, which is [task 48](48-portal-schedule-form.md).
  Keep the same timezone and error rules on both surfaces.

## Acceptance Criteria

- At an 800 px window height, **Create schedule** and the validation message
  are visible without scrolling.
- A new schedule defaults to the system zone, and its next runs show that
  zone. A paused schedule shows "Paused".
- The audit's empty submit and invalid cron produce word-level errors that
  disappear once each field is fixed.
- List rows show the folder name, the next run or "Paused", and a translated
  status.

## Verification

1. `./make test`, if Go-side validation messages change.
2. `./make check desktop`, including `SchedulesView.test.jsx`.
3. `./make e2e desktop-ui`, then `./make e2e visual`.

## Notes

This task depends on task 20 for the dialog's move onto gui's modal.

Audit context: the phase 0 Desktop audit ran on 2026-10-06 against `main` at
`e98efc7a`. It drove `./make run desktop-dev` through the browser bridge on
macOS. A valid `*/5 * * * *` Asia/Shanghai schedule was created, fired,
paused, and deleted through the UI. Deletion asked for confirmation and named
what would be lost. The operator was an Agent with repository knowledge. The
full report is kept in history at
[`718a6ab3`](https://github.com/icloudbb/buildmax/blob/718a6ab3969d35bdba7f66013d8ccfefe7549af8/docs/contribute/exploratory-runs/2026-10-06-desktop-ui-journey-audit.md).
