---
id: ui-i18n-remaining-pages
title: Translate the remaining Portal and Desktop pages into Simplified Chinese
roadmap: R6
source: docs/design/ui-experience-program.md#d5-localization-through-a-shared-typed-catalog
depends_on: []
verification: ["./make check portal", "./make check desktop", "./make e2e local", "./make e2e desktop-ui"]
claim:
pr:
---

## Outcome

A person who chooses 简体中文 sees Chinese on every Portal and Desktop
screen. Today only the shells, Help, and the pilot journeys are translated,
so the rest of the product switches back to English mid-journey.

## Scope

Move every user-visible string into the area catalogs under
`portal/src/i18n/` and `desktop/frontend/src/i18n/`, with Chinese, one area
per commit or pull request. Follow the pattern and glossary in the design
record's D5, and the pilot areas as examples (`issues.ts`, `runs.ts`,
`chat.ts`, `home.js`).

- **Portal areas:**
  - sign-in
  - Chat and conversations, Task detail
  - Agents
  - Workflows and the visual editor
  - Schedules
  - Files and Artifacts, including the public share page
  - Marketplace and plugins
  - Space settings, including members, secrets, service accounts,
    Assistants, and audit
  - Account
  - Administration
  - Remote Control
  - the not-found page and the shared state components (`Alert`,
    `EmptyState`, `ResourceUnavailable`)
  - relative times from `formatRelativeTime`
- **Desktop areas:**
  - sign-in
  - Schedules
  - Issues
  - Explorer, file, diff, and browser views
  - terminal chrome
  - the Jobs and Memory drawers
  - History

## Out Of Scope

- Server-generated text and Agent output.
- Wording changes in English. English strings stay byte-identical so
  existing e2e selectors hold.

## Acceptance Criteria

- No hard-coded user-visible English remains in Portal or Desktop. Each area's
  pull request lists the literals that remain and why (product names, units,
  raw identifiers).
- The catalog tests pass: every key has Chinese with identical placeholders.
- Text composed in effects and async callbacks uses `useStableT`, so a
  language switch reloads nothing.
- For each area, a Portal or Desktop browser check renders one screen in
  Chinese.

## Verification

Run `./make check portal` and `./make check desktop`, then `./make e2e local`
and `./make e2e desktop-ui`.

## Notes

The relative-time helper in `portal/src/lib/api/mappers.ts` formats at mapping
time. It needs the locale at render time instead, so a switch does not leave
stale English.
