---
id: portal-schedule-form
title: Validate and preview a Portal schedule before it is saved, and let a row be edited or deleted
roadmap: R6
source: docs/design/ui-experience-program.md#phases
depends_on: []
verification: ["./make test", "./make check portal", "./make e2e visual", "./make e2e kind"]
claim:
pr:
---

## Outcome

Creating a schedule shows when it will next run in the person's own time zone,
and reports every problem at once in plain words. An existing schedule can be
edited or deleted from its row. This task carries Portal audit finding P12.

## Findings

**P12 (Minor). The Schedule form is weak at validation and preview.**

- Screen: Schedules → New schedule, and the Schedules list.
- Reproduction: create a Monday 09:00 schedule for a published Workflow. Enter
  the cron text "every monday" and the timezone "Shanghai", then save.
- Actual:
  - The timezone defaults to `UTC`, not the browser's zone.
  - There is no next-run preview. Desktop's schedule dialog has one.
  - Errors are raw server text, such as `timezone "Shanghai": unknown time
    zone Shanghai`, and they arrive one at a time. The invalid cron "every
    monday" was not reported until the timezone was fixed.
  - "Pause all" and "Resume all" are shown together.
  - A schedule row offers only **Pause**, with no visible edit or delete.

## Scope

- Default the timezone to the browser's IANA zone.
- Show the next few run times, labelled with their zone, before saving.
- Report every invalid field at once, in person-facing words, next to the
  field.
- Keep one authoritative cron and timezone implementation: the server's. Get
  the preview and the validation from the server, for example a validate or
  preview response that returns all field errors and the next runs. Do not add
  a second cron parser in TypeScript. Keep `openapi.json` in step with the
  change.
- Show only the bulk action that applies: Pause all while any schedule is
  active, Resume all while all are paused.
- Add Edit and Delete to each row, using the existing `PATCH` and `DELETE`
  schedule routes. Delete asks for confirmation and names what is lost.

## Out Of Scope

- Desktop's local schedule dialog, which is
  [task 54](54-desktop-schedule-form.md). Use the same rules there.
- New schedule capabilities.

## Acceptance Criteria

- A new schedule form opens with the browser's timezone.
- Entering a valid cron shows the upcoming runs with a zone label before save.
- The audit's reproduction reports both the cron error and the timezone error
  together, in words, with no Go parser text as the primary message.
- Each row offers Edit and Delete, and only one bulk action is visible.

## Verification

1. `./make test` for the validation and preview response.
2. `./make check portal`, then `./make e2e visual`.
3. Use the kind loop, then `./make e2e kind`.

## Notes

Audit context: the phase 0 Portal audit ran on 2026-10-06 against `main` at
`e98efc7a`, on an ephemeral kind cluster with the mock model. The operator was
an Agent with repository knowledge. The full report is kept in history at
[`718a6ab3`](https://github.com/icloudbb/buildmax/blob/718a6ab3969d35bdba7f66013d8ccfefe7549af8/docs/contribute/exploratory-runs/2026-10-06-portal-ui-journey-audit.md).
