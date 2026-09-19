# @buildmax/gui

Shared React UI components and styles for BuildMax portal and desktop app. Implement once, use in both.

## Exports

- **Theme**: `ThemeProvider`, `useTheme`, `ThemeToggle`, and type `Theme` (`"light" | "dark"`).
- **Styles**: `theme.css` — CSS variables for light/dark (`data-theme`). Import as `@buildmax/gui/theme.css`.
- **Button, ButtonLink, and IconButton**: Presentational controls with primary, secondary, tertiary, and danger roles; `Button` also supports busy and compact states. Use `ButtonLink` for navigation. Import `button.css` after `theme.css`.
- **BaseModal**: Presentational modal component; props: `open`, `title`, `titleId`, `onClose`, optional `className`, optional `hideHeader`, `children`. Type `BaseModalProps` is exported for TypeScript.
- **FormModal**: Form-oriented modal shell and its field/select configuration types.
- **Avatar**: Shared avatar presentation and `getInitials` helper.
- **ChatComposer**: Shared chat input and submission presentation.
- **ChatThread**: Shared transcript presentation and item types.
- **Modal styles**: `modal.css` — base modal layout (overlay, `.modal`, `.modal--large`, `.modal__header`, `.modal__title`, `.modal__close`, `.modal__body`). Uses theme variables; import `theme.css` first, then `@buildmax/gui/modal.css`.
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
4. Import: `import { ThemeProvider, useTheme, ThemeToggle, BaseModal, Button } from '@buildmax/gui'`, `import '@buildmax/gui/theme.css'`, `import '@buildmax/gui/button.css'`, and (if using modals) `import '@buildmax/gui/modal.css'`

Consumers keep their own layout/sidebar and any `.theme-toggle` button styles; the package provides the component and theme variables.

## Consolidation candidates

Future components to consider moving into this package (presentational only; each app keeps its own data and callbacks):

- **Icons** — Shared icon set or sprite so both apps use the same symbols.
