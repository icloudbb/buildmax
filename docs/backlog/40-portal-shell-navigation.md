---
id: portal-shell-navigation
title: Make Portal's sidebar real navigation, say each page title once, and follow the OS theme
roadmap: R6
source: docs/design/ui-experience-program.md#phases
depends_on: [16-gui-shared-primitives.md]
verification: ["./make check gui", "./make check portal", "./make check desktop", "./make e2e visual", "./make e2e local"]
claim:
pr:
---

## Outcome

The Portal shell tells a person where they are only once, behaves like web
navigation, and does not log expected failures. Both apps start in the
person's OS theme. This task carries Portal audit findings P16, P21, P23, and
P27, and Desktop finding D20, which shares P27's cause.

## Findings

**P16 (Minor). Sidebar navigation is not exposed as navigation.**

- Screen: the sidebar on every Space page.
- Reproduction: open any Space page and inspect the sidebar items.
- Actual: the items are `<button>` elements with no `aria-current`, so the
  current page is shown only visually. Because they are not links,
  open-in-new-tab and copy-link do not work.
- Expected: links to the hash routes, with `aria-current="page"` on the
  current one.

**P21 (Cosmetic). Titles repeat.**

- Screen: Chat, Issue Detail, and the narrow-width header.
- Actual:
  - On Chat, the breadcrumb and the h1 both say "Chat". At 390 px the mobile
    header repeats it a third time.
  - **Back to Issues** duplicates the breadcrumb.
  - The Chat placeholder is clipped mid-line at 390 px.
- Expected: the page-system rule that the shell states scope once and the page
  owns one real H1, through task 16's `PageHeader` and `DetailHeader`.

**P23 (Cosmetic). Expected failures are logged as errors.**

- Reproduction: open any page as a non-administrator, then open the
  signed-out page.
- Actual: every non-admin page load logs `403 /api/admin/me`, and the
  signed-out page logs a burst of 401s.
- Expected: Portal learns the person's deployment authority without an error
  response. Pick the smallest coherent change, for example asking only after
  sign-in and taking authority from an endpoint that answers every signed-in
  account. Keep `openapi.json` in step with any route change.

**P27 (Cosmetic), and Desktop D20 (Cosmetic). The theme ignores the OS
preference.**

- Reproduction: set the OS to dark, clear `localStorage`, and open Portal or
  Desktop.
- Actual: both start light. `gui/src/ThemeContext.tsx` reads only the
  `buildmax_theme` key.
- Expected: with no stored choice, follow `prefers-color-scheme`. A stored
  choice still wins. Both apps use gui's `ThemeProvider`, so one change fixes
  both.

## Scope

- Render sidebar destinations as links with `aria-current`, keeping their
  current look.
- Adopt task 16's anatomy components in the shell and in the Chat and Issue
  Detail headers. Remove the duplicate back link and the narrow-header
  repetition, and fix the placeholder clipping.
- Remove the expected 401 and 403 requests.
- Make the theme default follow the OS.

## Out Of Scope

- The landing page's content, which is [task 38](38-portal-needs-me-landing.md).
- Visual restyling of the sidebar.

## Acceptance Criteria

- Each sidebar item is a link that opens in a new tab. Exactly one item
  carries `aria-current="page"`.
- Chat and Issue Detail state their title once at 390, 768, and 1280 px.
- Loading any page as a non-administrator, or signed out, logs no 401 or 403.
- With no stored theme, Portal and Desktop follow the OS theme. A stored choice
  overrides it, covered by a gui vitest test.

## Verification

1. `./make check gui`, `./make check portal`, `./make check desktop`.
2. `./make e2e visual`. Update the baselines if the shell's look changes on
   purpose. The harness must still pin the theme it captures.
3. `./make e2e local`.
4. If a server route changes, run `./make test`, then the kind loop and
   `./make e2e kind`.

## Notes

Audit context: the phase 0 Portal audit ran on 2026-10-06 against `main` at
`e98efc7a`, on an ephemeral kind cluster with the mock model. The operator was
an Agent with repository knowledge. The full reports are kept in history:
[Portal](https://github.com/icloudbb/buildmax/blob/718a6ab3969d35bdba7f66013d8ccfefe7549af8/docs/contribute/exploratory-runs/2026-10-06-portal-ui-journey-audit.md)
and
[Desktop](https://github.com/icloudbb/buildmax/blob/718a6ab3969d35bdba7f66013d8ccfefe7549af8/docs/contribute/exploratory-runs/2026-10-06-desktop-ui-journey-audit.md).

This task depends on task 16 only for P21. If task 16 stalls, P16, P23, and
P27 can ship as their own change.
