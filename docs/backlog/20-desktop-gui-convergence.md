---
id: desktop-gui-convergence
title: Move Desktop onto the shared gui buttons, overlays, and icons
roadmap: R6
source: docs/design/ui-experience-program.md#d4-one-presentation-layer-for-both-surfaces
depends_on: []
verification: ["./make check gui", "./make e2e desktop-ui", "./make build desktop", "./make e2e desktop-launch"]
claim:
pr:
---

## Outcome

Desktop dialogs trap focus and restore it on close, and its buttons and icons
match Portal's. Today the modals in `components/Modals.jsx` handle Escape but
not focus. Desktop renders about 100 raw buttons and has its own icon set and
overlay CSS.

## Scope

- **Shared controls.** Replace Desktop's `.modal-overlay` / `.modal-panel` /
  `.modal-btn` system (`desktop/frontend/src/css/layout.css`) and the modals in
  `Modals.jsx` and `HistoryModal.jsx` with gui's `BaseModal` or `FormModal`.
  Replace the `JobsDrawer` and `MemoryDrawer` with gui's `Drawer`.
- **Buttons.** Use gui's `Button` and `IconButton` for actions. Keep raw
  `<button>` only where a specialized control, such as a tab strip or a
  terminal control, needs it.
- **Icons.** Merge Portal's `src/icons`, Desktop's `components/icons.jsx`, and
  gui's inline SVGs into one gui icon export used by both apps.
- **Dead CSS.** Delete the CSS that the migration leaves unused.
- **TypeScript.** Convert a file to TypeScript only where this change already
  rewrites most of it. Whole-app migration stays an open question in the
  design record.

## Out Of Scope

- Desktop's data layer and Wails bindings.
- Decomposing `App.jsx` beyond what the overlay migration requires. Phase 4
  rework does that, driven by the audit.
- Menus, fields, and toasts. Desktop adopts them after
  [task 16](16-gui-shared-primitives.md) lands, as part of phase 4.

## Acceptance Criteria

- Desktop contains no hand-rolled overlay. Every dialog and drawer traps focus
  and returns it to the trigger, covered by a Desktop vitest test per overlay
  kind.
- One icon set serves Portal, Desktop, and gui.
- `./make e2e desktop-ui` and the packaged build pass.

## Verification

Run, in order:

1. `./make check gui`
2. `npm --prefix desktop/frontend test`
3. `./make e2e desktop-ui`
4. `./make build desktop`
5. `./make e2e desktop-launch`

Also run `./make check portal` and `./make e2e local`, because the icon
consolidation touches Portal.

## Notes

`useOverlayA11y` and `bodyScrollLock` are internal to gui. Use them through
`BaseModal` and `Drawer`; do not re-export them.

The phase 0 Desktop audit (D18, Cosmetic) found an emoji 💬 chat-tab icon
among line icons in the workbench tab bar. The single icon set fixes it;
replace that emoji too. The same finding's duplicated "/ for commands" hint
is [task 22](22-desktop-chat-turn-echo.md). The New Schedule dialog's hidden
footer (D10) is checked against this task's `FormModal` migration in
[task 56](56-desktop-schedule-form.md).
