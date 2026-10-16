# @buildmax/gui

Shared React UI components and styles for BuildMax portal and desktop app. Implement once, use in both.

## Exports

- **Theme**: `ThemeProvider`, `useTheme`, `ThemeToggle`, and type `Theme` (`"light" | "dark"`).
- **Styles**: `theme.css` — CSS variables for light/dark (`data-theme`). Import as `@buildmax/gui/theme.css`. It is the only stylesheet that may hold color literals.
- **Stylelint config**: `stylelint/tokens.mjs` — shared by the `stylelint.config.mjs` of gui, Portal, and Desktop; rejects color literals outside `theme.css` and `var()` references to undefined custom properties. Run with `npm run lint`.
- **Button, ButtonLink, and IconButton**: Presentational controls with primary, secondary, tertiary, and danger roles; `Button` also supports busy and compact states. Use `ButtonLink` for navigation. Import `button.css` after `theme.css`.
- **BaseModal**: Presentational modal component; props: `open`, `title`, `titleId`, `onClose`, optional `className`, optional `hideHeader`, `children`. Type `BaseModalProps` is exported for TypeScript.
- **Drawer**: A slide-in panel; props: `open`, `title`, `titleId`, `onClose`, optional `id`, optional `side` (`"start"`, the default, for navigation from the left; `"end"` for a side panel from the right), optional `className` (for example a width), `children`. Import `drawer.css`.
- **Overlay behavior**: `BaseModal` and `Drawer` move focus inside on open (to the element marked `data-autofocus`, else the first focusable one), keep Tab inside, close on Escape, lock body scroll, and return focus to the element that opened them. Overlays stack: one opened from inside another is the only one that answers Escape and Tab until it closes. The hook behind this is internal; use it through these two components.
- **Icons**: The one icon set for Portal, Desktop, and gui, such as `ChatIcon`, `CloseIcon`, `SearchIcon`, and `SettingsIcon`, and type `IconProps`. Each is a decorative 24-unit line icon in `currentColor`, with an 18px default size that the caller's CSS or `width`/`height` overrides, and the class `bm-icon` plus any `className` given. A control names itself with its own text or `aria-label`, not the icon.
- **FormModal**: Form-oriented modal shell and its field/select configuration types; its fields render through `Field`.
- **Field, Input, Select, and Textarea**: `Field` pairs a label with one control and wires its hint and error (`aria-describedby`, `aria-invalid`); give an explicit id to `Field`, not the control. `group` renders a fieldset and legend for a set of checkboxes or radios. The three controls share one geometry, focus, disabled, and invalid look, and are ordinary named controls outside a `Field`. Import `field.css`.
- **Menu**: A menu button. `trigger` renders the button from the props it is given; `MenuItem`, `MenuItemRadio`, `MenuGroup`, `MenuSeparator`, and `MenuSection` build the panel. Arrow keys, Home, and End move between items; Escape closes and returns focus to the button without closing an enclosing dialog; Tab or a click outside closes it. Import `menu.css`; a surface positions a particular menu with `className`.
- **ToastProvider and useToast**: Transient confirmation of an effect the page already shows, in a live region that waits while hovered or focused. Never the only evidence of a result: failures stay inline.
- **Spinner and Skeleton**: `role="status"` progress for a section still loading, and placeholder rows for content whose shape is known. Neither claims a count.
- **statusLabel, useStatusLabel, statusTone, and StatusLabel**: The one status vocabulary. `statusLabel` maps a business, run, or lifecycle status to its label in either case the server stores it; `StatusLabel` renders the label with a tone mark. Import `feedback.css` for these and the toast and progress styles.
- **PageHeader, DetailHeader, CollectionFrame, and EditorActions**: The page anatomy of [the page-system record](../docs/design/portal-frontend-page-system.md#page-anatomy-and-templates). `CollectionFrame` shows rows or the empty state, never both, and no count or pagination while loading; `EditorActions` becomes a sticky Save/Cancel bar on narrow screens. Import `page.css`.
- **Avatar**: Shared avatar presentation and `getInitials` helper.
- **ChatComposer**: Shared chat input and submission presentation.
- **ChatThread**: Shared transcript presentation and item types.
- **Modal styles**: `modal.css` — base modal layout (overlay, `.modal`, `.modal--large`, `.modal__header`, `.modal__title`, `.modal__close`, `.modal__body`, `.modal__actions`). Uses theme variables; import `theme.css` first, then `@buildmax/gui/modal.css`. It holds no input styling: form controls come from `field.css`.
- **Widget styles**: `widgets.css` — styles for the shared avatar and chat components.

## Local dependency (portal & desktop)

From `portal/` or `desktop/frontend/` add to `package.json`:

```json
"dependencies": {
  "@buildmax/gui": "file:../gui"
}
```

From `desktop/frontend/` the path is `file:../../gui`.

1. Use Node 24 and npm 11 (see the root `.node-version`).
2. Build the package: `cd gui && npm ci && npm run build`
3. In the app: `npm ci`
4. Import: `import { ThemeProvider, useTheme, ThemeToggle, BaseModal, Button } from '@buildmax/gui'`, `import '@buildmax/gui/theme.css'`, `import '@buildmax/gui/button.css'`, and the stylesheets of the components you use (`modal.css`, `drawer.css`, `field.css`, `menu.css`, `feedback.css`, `page.css`, `widgets.css`)

Consumers keep their own layout/sidebar and any `.theme-toggle` button styles; the package provides the component and theme variables.
