---
id: ui-visual-and-a11y-guardrails
title: Add visual regression, contrast checking, and jsx-a11y to the frontends
roadmap: R6
source: docs/design/ui-experience-program.md#d3-guardrails-before-rework
depends_on: []
verification: ["./make check gui", "./make check portal", "./make e2e local", "./make e2e desktop-ui"]
claim:
pr:
---

## Outcome

Later UI rework cannot change a screen's appearance, or drop below WCAG AA
contrast, without a reviewer seeing it in CI. Today a change is only noticed
when an e2e selector breaks or a person happens to look.

## Scope

- **Portal screenshot comparisons.** Add Playwright screenshot comparisons for
  `/specimen` and one representative page per Portal template: collection,
  detail, editor, diagnostics, and settings. Cover light and dark themes at
  1280 and 390 px. Use deterministic fixture data and freeze time and
  animation.
- **Desktop screenshot comparisons.** Add the same for Desktop's golden-path
  views through the `desktop-ui` suite.
- **Baselines.** Decide the baseline platform and storage, and record the
  decision in the design record's open questions and in
  `docs/contribute/testing.md`. The expected answer is Linux CI only, with a
  `./make` command to refresh the baselines.
- **Contrast.** Enable axe `color-contrast` in
  `portal/e2e/accessibility.spec.ts` and fix every violation it reports.
- **Lint.** Add `eslint-plugin-jsx-a11y` to `gui`, `portal`, and
  `desktop/frontend` and fix its findings.

## Out Of Scope

- A 200% zoom check and screen-reader automation. Those stay as the responsive
  record describes.
- Storybook. `/specimen` is the component review surface.

## Acceptance Criteria

- An intentional style change to a covered page fails the screenshot check
  until its baseline is updated with the documented command.
- axe runs with `color-contrast` enabled and reports zero violations.
- jsx-a11y reports zero violations in all three packages.
- `docs/contribute/testing.md` explains how to run, update, and review the
  screenshot baselines.

## Verification

Run, in order:

1. `./make check gui`
2. `./make check portal`
3. `./make e2e local`
4. `./make e2e desktop-ui`

Then confirm the screenshot job runs in CI on the pull request.

## Notes

Portal's testing split is intentional: browser-level assertions belong in
Playwright, and gui's vitest suite proves shared component behavior (see the
testing guide, "Frontend Component Tests"). Do not add a jsdom render layer to
Portal.
