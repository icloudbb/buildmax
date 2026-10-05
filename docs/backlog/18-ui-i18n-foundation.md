---
id: ui-i18n-foundation
title: Add the shared locale catalog and an English/Chinese switch to Portal and Desktop
roadmap: R6
source: docs/design/ui-experience-program.md#d5-localization-through-a-shared-typed-catalog
depends_on: []
verification: ["./make check gui", "./make check portal", "./make e2e local", "./make e2e desktop-ui"]
claim:
pr:
---

## Outcome

Portal and Desktop can present Simplified Chinese. The mechanism is proven end
to end on real screens, so later work can extract and translate every remaining
string. Today both apps hard-code English, and only the Help page switches
language.

## Scope

- **Shared mechanism in `gui`.** Add a `LocaleProvider` and a `useT()` lookup
  over typed message catalogs, with interpolation and an English plural rule.
  Add no third-party i18n dependency. gui's own strings, such as
  `QuestionForm` and `ChatComposer`, move into gui's catalog.
- **Locale preference.** The preference is per device: `localStorage` in
  Portal, Desktop's local settings in Desktop. It defaults from
  `navigator.language`. Each app sets `<html lang>` from the active locale.
- **Language switch.** Add one in Portal's account menu and in Desktop's
  settings.
- **Help page.** Move its inline English/Chinese table into the catalog, and
  make the page follow the interface locale.
- **Pilot extraction.** Extract and translate the shell (sidebar, header,
  account menu) and one full journey per surface:
  - Portal: Issue list → Issue detail → Run.
  - Desktop: home → project chat.
- **Missing-key check.** Add a check, part of `./make check`, that reports
  catalog keys missing from `zh-CN`.
- **Glossary first.** Before translating, settle the Chinese glossary with the
  maintainer (an open question in the design record) and record it in the
  design record.

## Out Of Scope

- Extracting the remaining pages. Later tasks do that, one per page group,
  after design phase 2 so that strings are not extracted twice.
- Translating server error text.
- An account-level locale preference.

## Acceptance Criteria

- Switching the locale changes every string in the shell and in the pilot
  journeys, with no reload of authenticated state.
- A missing `zh-CN` key falls back to English, and the check lists it.
- The golden-path e2e for the pilot journeys passes in both locales.
- The glossary is recorded in the design record.
- `docs/contribute/architecture/portal.md` and the Desktop architecture
  document describe the catalog.

## Verification

Run, in order:

1. `./make check gui`
2. `./make check portal`
3. `./make e2e local`
4. `./make e2e desktop-ui`
5. `./make check docs`

Add a changelog entry (`added`), because the language switch is user-visible.

## Notes

The `manual/zh/` translation and its naming conventions are prior art for
terminology. `documentation.md` §Languages keeps capitalized domain names in
English in the documentation mirror; the glossary decision says whether the UI
follows that rule.
