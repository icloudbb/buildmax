# UI Experience Program

> **简体中文：** [阅读中文镜像](../zh-CN/design/UI体验专项.md)
>
> **Audience:** Portal, Desktop, and `@buildmax/gui` contributors · **Status:** active plan — not started
>
> **Opened:** 2026-10-05

Related: [roadmap](../ROADMAP.md) R6,
[Beta readiness record](../deploy/beta-readiness.md),
[surface positioning](surface-positioning.md),
[Portal frontend page system](portal-frontend-page-system.md),
[Portal state and permission feedback](portal-state-and-permission-feedback.md),
[Portal responsive and accessible interaction](portal-responsive-and-accessible-interaction.md),
[client surface convergence](../proposals/client-surface-convergence.md),
[exploratory testing](../contribute/exploratory-testing.md), and
[Portal architecture](../contribute/architecture/portal.md).

## Contents

- [Decision](#decision)
- [User Outcome](#user-outcome)
- [Evidence](#evidence)
- [Decisions](#decisions)
- [Phases](#phases)
- [Relationship To Existing Records](#relationship-to-existing-records)
- [Non-Goals](#non-goals)
- [Done When](#done-when)
- [Open Questions](#open-questions)

## Decision

BuildMax runs a dedicated program to validate and rebuild the Portal and
Desktop user interfaces. v0.2.0-alpha.22 passed the functional Beta
qualification, but nobody other than the people who built a journey has checked
whether it is usable. The program:

1. **Audits before redesigning.** Agent-driven walkthroughs of the core journeys
   produce graded findings. Those findings decide which screens get reworked and
   whether the visual language needs to change.
2. **Builds the guardrails first.** Token integrity, lint rules, visual
   regression, and contrast checks land before any page rework, so the rework
   cannot quietly regress.
3. **Converges Portal and Desktop on one presentation layer.** Both surfaces
   share `@buildmax/gui` tokens and primitives. Desktop stops hand-rolling
   overlays, buttons, and icons.
4. **Ships English and Simplified Chinese user interfaces** for both surfaces.

The scope is Portal and Desktop. The CLI/TUI is excluded.

## User Outcome

A person who has not seen BuildMax before can complete the core Portal and
Desktop journeys without help. At every step they can tell what state their
work is in and what went wrong. The interface is in their language, English or
Simplified Chinese. The two surfaces look and behave like one product.

## Evidence

The facts below were taken from `main` at `d56f0cdc` on 2026-10-05.

**Validation gap.** Every Beta qualification journey was driven by the
implementing Agent with probe scripts. The non-author operator journey (Q7) was
waived. The packaged Desktop launch and the Desktop UI suite were accepted as
partial limits because they need a native window. The page-system record's
own acceptance item, a non-author review of Issue-to-result and
failure-to-diagnosis, has not been produced. Committed exploratory runs cover
one cross-surface pass on 2026-09-13. Most UI defects were found by browser
smoke tests and Agent drills, not by watching someone use the product.

**Design records are mostly shipped.** Five of the seven Portal design records
are complete. What remains:

- Page-system components: `PageHeader`, `CollectionFrame`, `DetailHeader`,
  `StatusLabel`, and a sticky Save/Cancel on narrow editors.
- One status vocabulary. Labels still come from both `lib/statusLabels.ts` and
  `features/conversations/thread.ts`.
- Per-task mutation errors on Issue Detail.
- Contrast checking. The axe suite disables `color-contrast`.
- An automated 200% zoom check.

The interaction model does not need to be invented again. What is missing is
proof that it works for a new person, plus the shared foundation underneath it.

**Token system.**

- `gui/src/theme.css` defines color, spacing, type, and radius tokens. Adoption
  is mostly limited to color: CSS contains about 515 literal `font-size` values
  and 217 literal `border-radius` values, against 20 and 13 token uses.
- Nine custom properties are referenced about 60 times but defined nowhere and
  given no fallback, so those declarations silently do nothing. The affected
  properties are `--border-color`, `--color-warning`, `--panel-bg`,
  `--color-error`, `--color-focus`, `--color-status-success`,
  `--color-bg-subtle`, `--color-surface`, and `--muted-text`. They appear in
  Portal page CSS and in Desktop's `schedules.css`.
- At least five different danger reds are hard-coded. About 165 hex literals in
  the two apps' CSS do not follow the dark theme.

**Desktop is a separate presentation island.**

- Desktop is untyped JSX.
- It imports only `ThemeProvider`, `useTheme`, `Avatar`, `ChatThread`,
  `ChatComposer`, and `QuestionForm` from `@buildmax/gui`. It renders about 100
  raw `<button>` elements and never uses the shared `Button`.
- Its modals in `components/Modals.jsx` handle Escape but have no focus trap,
  which duplicates `BaseModal` and `useOverlayA11y` without their guarantees.
  Its drawers reuse a diff class instead of `Drawer`.
- Portal, Desktop, and gui each have their own icon set.
- `App.jsx` holds 1,415 lines of state and tab orchestration.

**Missing shared primitives.** Neither app has a shared form field, select,
menu or popover, toast, spinner or skeleton, or table. Portal pages style
inputs with modal classes. Menus are rebuilt per site: there are seven
`role="menu"` implementations. Both apps contain 58 hand-written "Loading…"
strings.

**No guardrails against visual regression.** There is no screenshot
comparison, no `eslint-plugin-jsx-a11y`, and no lint rule against raw colors or
undefined custom properties. Each earlier migration wave was followed by a
cluster of e2e selector fixes.

**No localization.** Both apps hard-code English and set `lang="en"`. Only the
Help page carries an inline English/Chinese table. The user manual already
ships a complete `manual/zh/` translation.

## Decisions

### D1. Audit before redesign

Whether to keep and refine the current neutral visual language or adopt a new
one is decided from audit evidence, not taste. The audit report ends with a
recommendation and the findings behind it. The maintainer makes the call in
phase 2. Until then, foundation work stays neutral to that choice: tokens and
primitives make either outcome cheaper.

### D2. Agent-driven validation, recorded as such

The maintainer chose Agent-executed validation over recruited human testers.
The audit and the re-audit follow the
[exploratory testing guide](../contribute/exploratory-testing.md):

- **Portal** runs on an ephemeral kind cluster with the mock model for
  deterministic journeys. A configured real model is used where generation
  shapes the experience.
- **Desktop** runs through `wails dev`'s browser bridge, the same path as
  `./make e2e desktop-ui`.
- Each journey is captured at 390, 768, and 1280 px, in light and dark themes,
  and in both locales once phase 1 provides them.

Findings are graded by severity:

- **Blocker:** the journey cannot be completed.
- **Major:** the person is misled, or recovers only with knowledge they would
  not have.
- **Minor:** friction or inconsistency.
- **Cosmetic.**

Each finding cites the screen, the step, and a reproduction. The rubric covers
discoverability, state legibility, error recovery, consistency, density and
hierarchy, accessibility, and copy.

The audit is evidence about the product, not a substitute for Q7. The report
states that the operator was an Agent with repository knowledge. It also lists
what the browser bridge cannot see: native window focus, the file chooser, and
packaged layout.

### D3. Guardrails before rework

Phase 1 adds the checks that keep later phases honest:

- **Stylelint** rejects raw color literals outside `gui/src/theme.css` (the
  xterm theme is a named exception) and rejects custom properties that no
  stylesheet defines.
- **Visual regression** uses Playwright screenshots of the `/specimen` page and
  one representative page per Portal template, compared in CI. Desktop gets the
  same for its golden-path views.
- **Accessibility checks:** axe `color-contrast` is enabled, and every violation
  it finds is fixed, not suppressed. `eslint-plugin-jsx-a11y` runs on all three
  packages.

These guardrails do not change Portal's testing split. Browser-level
assertions stay in Playwright, and `gui` proves shared component behavior once
under vitest.

### D4. One presentation layer for both surfaces

`@buildmax/gui` owns the tokens, the icon set, and the primitives both surfaces
need:

- form field, input, select, and textarea
- menu and popover
- toast
- spinner and skeleton
- `StatusLabel` over one status vocabulary
- the page-anatomy components named in the page-system record

A primitive moves into gui only when both surfaces use it, or when Portal uses
it on more than one template. Otherwise it stays local.

Desktop adopts gui's `Button`, `BaseModal`, `Drawer`, and icons, and deletes its
own overlay CSS. This is presentation only. Desktop's data layer stays on Wails
bindings; the shared data interface remains the open question of the
[client surface convergence proposal](../proposals/client-surface-convergence.md).

### D5. Localization through a shared typed catalog

Both surfaces ship English and Simplified Chinese.

- **Ownership.** `@buildmax/gui` provides a small locale provider and a
  `useT()` lookup over typed message catalogs. Each app owns its own catalog
  files. gui's own strings (for example `QuestionForm` and `ChatComposer`) live
  in gui's catalog.
- **No library.** The only needs are interpolation and an English plural rule.
  A dependency would add more concepts than it removes.
- **Locale choice.** The locale is a per-device preference, stored in
  `localStorage` for Portal and in Desktop's local settings. It defaults from
  `navigator.language`. An account-level preference is left out until a person
  needs the same locale across devices.
- **Help page.** Its inline table moves into the catalog, and it follows the
  interface locale.
- **Server-originated messages.** API error text stays English. The UI
  translates the conditions it recognizes and shows the server text otherwise.
- **Terminology.** Domain nouns follow one glossary, see
  [Open Questions](#open-questions).

English stays authoritative. A missing Chinese key falls back to English, and a
check reports missing keys.

## Phases

| Phase | Outcome | Ready work |
|---|---|---|
| 0. Audit | A graded findings report for the Portal and Desktop core journeys, with a visual-language recommendation | [backlog 10](../backlog/10-ui-journey-audit.md) |
| 1. Foundation | Defined tokens with lint enforcement; visual-regression and contrast guardrails; shared primitives; i18n infrastructure; Desktop on gui primitives | backlog [12](../backlog/12-ui-token-integrity.md), [14](../backlog/14-ui-visual-and-a11y-guardrails.md), [16](../backlog/16-gui-shared-primitives.md), [18](../backlog/18-ui-i18n-foundation.md), [20](../backlog/20-desktop-gui-convergence.md) |
| 2. Visual-language decision | The maintainer accepts "refine" or "new language" from the phase 0 report; this record and the page-system record are updated | Decision, not a task |
| 3. Portal rework | Blocker and Major findings resolved, in journey order | Tasks drafted from the phase 0 report |
| 4. Desktop rework | Same for Desktop; `App.jsx` decomposed where the findings touch it | Tasks drafted from the phase 0 report |
| 5. Chinese and re-audit | Complete `zh-CN` catalogs; the phase 0 journeys re-run in both locales and compared with the baseline | Tasks drafted after phases 3–4 |

Phase 0 and the token, guardrail, and i18n tasks are independent and can run in
parallel. Primitive and Desktop convergence work depends on token integrity.
Rework tasks are not drafted before the audit exists, because their content is
the audit's output.

## Relationship To Existing Records

The Portal records remain the specifications for their concerns: page anatomy
and action grammar, state feedback, responsive and accessible interaction,
navigation, and work experience. This record owns the program around them: its
sequence, validation, the cross-surface presentation layer, and localization.
The page-system record's remaining items (anatomy components, sticky actions,
one status vocabulary, the operator review) are executed here. They are marked
shipped in that record as they land.

## Non-Goals

- The CLI/TUI interface.
- Desktop's data layer, a PWA, or a mobile client. These belong to the
  [client surface convergence proposal](../proposals/client-surface-convergence.md).
- New product capability. Rework changes how existing capability is presented
  and reached.
- Translating server-generated text, Agent output, or documentation beyond what
  already exists.
- Portal performance work. It stays an R5 candidate unless the audit finds
  performance blocking a journey.
- Replacing the non-author operator journey (Q7) in the Beta readiness record.

## Done When

- Every Blocker and Major finding from the phase 0 audit is resolved, or
  accepted by the maintainer with a recorded reason. The re-audit finds no new
  Blocker or Major.
- Lint enforces that no undefined custom property is referenced and no raw
  color appears outside the token file.
- Visual regression, axe including `color-contrast`, and jsx-a11y run in CI for
  Portal, Desktop, and gui.
- Desktop renders no hand-rolled overlay, button family, or icon set.
- Every user-visible string in Portal, Desktop, and gui comes from a catalog.
  `zh-CN` has no missing keys. The golden-path e2e journeys pass in both
  locales.
- The user manual, the Portal and Desktop architecture documents, and the
  current-state document describe the shipped result.

## Open Questions

- **Visual language.** Refine the current neutral style or adopt a new one?
  This is decided in phase 2 from the audit.
- **Chinese terminology.** The documentation mirror keeps capitalized domain
  names (Space, Issue, Task, Workflow, Agent) in English. Should the interface
  do the same, or translate them, for example 空间 or 事项? The glossary is
  decided before the first catalog is translated and applies to the manual's
  Chinese pages too.
- **Desktop TypeScript.** Should Desktop migrate to TypeScript? The default is
  to convert only the files the convergence touches. A full migration is taken
  only if the audit or convergence work shows untyped props causing defects.
- **Screenshot baselines.** Which platform renders the baselines, given that
  Linux CI and macOS render fonts differently? Where are baselines stored?
  Phase 1 decides; the expected answer is Linux CI as the only baseline
  platform.
- **Default Space landing page.** Should it be Chat or Issues? This question is
  inherited from the page-system record, and the audit should produce evidence
  for it.
