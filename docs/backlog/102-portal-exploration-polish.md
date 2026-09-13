---
id: portal-exploration-polish
title: Fix three small Portal consistency issues found in exploration
roadmap: none
source: direct
depends_on: []
verification: ["./make check portal"]
claim:
---

## Outcome

Three small, independently trivial Portal inconsistencies found during
exploratory testing are corrected: a member count reads correctly at one, tab
strips expose a consistent accessible role, and a form's inputs carry real
labels. None is a functional defect; together they are a bounded cleanup a
single session can finish, and each removes a small wrong-signal (a grammar slip,
a role a test cannot target, an input a label-based tool cannot reach).

## Scope

- **"1 members" grammar.** `portal/src/pages/settings/shared.tsx:495` renders
  `{members.length} members` unconditionally, so a one-member space reads
  "1 members". `portal/src/features/admin/AdminSpaces.tsx:112` already does the
  singular/plural form correctly (`member{n === 1 ? "" : "s"}`); apply the same
  there (and reuse a shared helper if one is warranted).
- **Agent-detail tab roles.** `portal/src/pages/agents/AgentDetail.tsx:345`
  renders the section tabs as `<nav aria-label> <button aria-current>`, while
  `portal/src/pages/settings/SpaceSettings.tsx:113` uses `role="tab"` for the
  same kind of tab strip. `aria-current` is not wrong, but the inconsistency
  means `getByRole('tab')` reaches one and not the other. Align agent-detail on
  the same tablist/tab pattern the settings tabs use.
- **New-secret item inputs lack labels.** In the new-secret form
  (`portal/src/features/spaceSecrets/SpaceSecrets.tsx`), the per-item name and
  value inputs expose an accessible name only via `placeholder`, so
  `getByLabel` cannot focus them (only `getByRole('textbox', {name})` does).
  Associate a real `<label>` (or `aria-label`) so label-based drivers and
  assertions reach them.

## Out Of Scope

Any behavior change; these are presentation, accessibility-consistency, and
markup fixes only. The F3 null-space fetch and F4 secret audit gap are separate
tasks (92, 98).

## Acceptance Criteria

- A one-member space shows "1 member"; a two-member space shows "2 members".
- The agent-detail section tabs are reachable by `getByRole('tab')` like the
  space-settings tabs.
- The new-secret item name and value inputs are reachable by their accessible
  label.

## Verification

`./make check portal` for lint/type and any updated component test. If a Portal
spec asserts on these (a tab role or the members string), extend it; otherwise
the browser suites already cover that the views render.

## Notes

Observed 2026-09-13 on `main` cceba61c as alice on an ephemeral kind cluster.
The members-string and tab-role inconsistencies are notable because the correct
pattern already exists elsewhere in the same codebase (AdminSpaces pluralizes;
SpaceSettings uses `role="tab"`), so this is alignment, not new design.
