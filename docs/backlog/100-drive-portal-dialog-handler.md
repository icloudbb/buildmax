---
id: drive-portal-dialog-handler
title: Let the drive-portal driver drive window.confirm-gated actions
roadmap: none
source: direct
depends_on: []
verification: []
claim:
---

## Outcome

An agent exploring Portal with the drive-portal skill can exercise the many
management actions gated on a browser `window.confirm()` — for example removing
a schedule, disabling or destroying a Space Secret, removing a comment or an
artifact, and the Administration account, model, administrator, and plugin
operations. Today Playwright auto-dismisses every such dialog, so these actions
silently no-op: the click lands, `console errors` stays clean, and nothing
changes, which reads as a product failure when it is a tool limitation.

## Scope

Add a dialog-handling command to `.buildmax/skills/drive-portal/driver.mjs` and
document it in `.buildmax/skills/drive-portal/SKILL.md`.

- Register a Playwright `page.on('dialog', ...)` path the driver controls: a
  command such as `confirm` that accepts the next dialog (and optionally
  `confirm off` / a "reject" mode), rather than the current implicit
  auto-dismiss.
- Document the gotcha in the command table and the Gotchas section: a
  `window.confirm()`-gated action does nothing until the tester arms dialog
  acceptance, distinct from the already-noted icon-only-close-with-Escape case.

## Out Of Scope

Any change to Portal itself (the confirm-gating is intentional product UX) and
to the pass/fail Playwright suites, which set up their own dialog handlers. This
is a manual-exploration convenience for the ad hoc driver only, like
96-drive-portal-authenticated-api-probe.md.

## Acceptance Criteria

- With the new command armed, deleting a schedule (or disabling/destroying a
  secret) via the driver actually performs the action against a running
  deployment.
- SKILL.md's command table documents the command and the auto-dismiss pitfall.

## Verification

No automated suite covers the ad hoc drivers. Verify by hand: against a running
kind deployment, create a schedule, arm the new command, delete it through the
driver, and confirm it is gone (and that without arming it, the delete no-ops).

## Notes

Observed 2026-09-13 on `main` cceba61c across two rounds: schedule delete
(`portal/src/features/schedules/SchedulesSection.tsx:219`) and secret destroy
(`portal/src/features/spaceSecrets/SpaceSecrets.tsx:438`) both left the item in
place with `console errors` = none. `driver.mjs` registers `console` and
`pageerror` handlers but no `dialog` handler, so Playwright's default
auto-dismiss cancels every `window.confirm()`. Other confirm-gated call sites
seen in code: `issues/IssueDiscussion.tsx`, `artifacts/confirmDelete.ts`, and
`features/admin/*`.
