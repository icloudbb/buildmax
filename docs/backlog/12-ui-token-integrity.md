---
id: ui-token-integrity
title: Define every referenced design token and lint against raw colors
roadmap: R6
source: docs/design/ui-experience-program.md#d3-guardrails-before-rework
depends_on: []
verification: ["./make check gui", "./make check portal", "./make e2e desktop-ui core", "./make e2e local"]
claim:
pr:
---

## Outcome

Styles that today silently do nothing start working. Danger, warning, success,
and surface colors follow the theme in both light and dark mode. A new raw
color or an undefined variable fails CI instead of shipping.

## Scope

- **Define the missing tokens.** These custom properties are referenced
  without a definition or fallback:
  - `--border-color`
  - `--color-warning`
  - `--panel-bg`
  - `--color-error`
  - `--color-focus`
  - `--color-status-success`
  - `--color-bg-subtle`
  - `--color-surface`
  - `--muted-text`

  For each one, either define the semantic token in `gui/src/theme.css` for
  both themes, or rewrite the reference to an existing token. Prefer the
  rewrite when an existing token already means the same thing. Also define
  `--font-mono` and `--color-accent`, which are referenced with fallbacks.
- **Replace hard-coded colors.** Replace the hard-coded hex and `rgb()` colors
  in Portal, Desktop, and gui CSS with tokens, including the five different
  danger reds.
- **Add the lint rule.** Add stylelint to `gui`, `portal`, and
  `desktop/frontend`. It must reject:
  - color literals outside `gui/src/theme.css`
  - custom properties that no stylesheet defines

  `desktop/frontend/src/lib/terminalTheme.js` is the single named exception.
  Wire it into the existing `./make check` scopes.
- **Delete dead CSS.** Delete the confirmed-dead `.artifact-modal__*` rules in
  `portal/src/css/modal.css`.

## Out Of Scope

- Converting literal spacing, radius, and font-size values to tokens. Page
  rework converts them as it touches each page; this task only stops new color
  debt.
- Choosing a new palette, which is design phase 2.

## Acceptance Criteria

- Stylelint passes in all three packages with the two rules on.
- No custom property is referenced without a definition, verified by the lint
  rule rather than by a one-off grep.
- `/specimen` and the Desktop golden-path views render the same in the light
  theme before and after. The dark theme shows corrected colors where
  literals were replaced. Both are checked by screenshot.

## Verification

Run, in order:

1. `./make check gui`
2. `./make check portal`
3. `./make e2e desktop-ui core`
4. `./make e2e local` for the browser loop

Run `./make check docs` if documentation changes.

## Notes

The `--color-danger` token is `#b42318`. The same value also appears as a raw
literal 5 times. Check the inventory again on the current `main` before
editing; the counts above are from `d56f0cdc`.
