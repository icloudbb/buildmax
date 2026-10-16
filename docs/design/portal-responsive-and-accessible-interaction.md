# Portal Responsive and Accessible Interaction

> **简体中文：** [阅读中文镜像](../zh-CN/design/Portal响应式与无障碍交互.md)
> **Audience:** Portal and shared GUI contributors · **Status:** implemented —
> all five slices (shell and navigation, shared overlays, core work journey,
> data and management, and the automated regression matrix) are shipped. The
> Acceptance Criteria's screen-reader announcement checks stay manual QA, not
> an automated slice, by their own description

This record defines the interaction invariants that keep key Portal journeys
usable at narrow widths and with keyboard or assistive technology. It is an
independently deliverable presentation improvement. Full breadth belongs to R5;
an earlier roadmap item may adopt a bounded part when its operator journey
cannot otherwise be completed.

## Contents

- [Outcome](#outcome)
- [Evidence and constraints](#evidence-and-constraints)
- [Supported layout ranges](#supported-layout-ranges)
- [Interaction decisions](#interaction-decisions)
- [Accessibility requirements](#accessibility-requirements)
- [Implementation slices](#implementation-slices)
- [Acceptance criteria](#acceptance-criteria)
- [Alternatives rejected](#alternatives-rejected)
- [Related records](#related-records)

## Outcome

The same essential Portal journeys can be completed on desktop, compact, and
narrow layouts without clipped controls, hidden context, two-dimensional page
scrolling, or inaccessible modal and navigation behavior.

## Evidence and constraints

- The current narrow rule stacks the shell and constrains the entire sidebar to
  a short scroll region. Logo, Space switcher, navigation, and account actions
  then compete for the same vertical area.
- Files uses a fixed-width tree beside its content, with no narrow
  interaction model.
- Shared modal tabs use a fixed side rail and long modal content can be clipped
  by overflow behavior.
- Some settings tabs scroll horizontally, but that pattern is not applied
  consistently and does not by itself preserve action visibility.
- Portal browser tests do not currently set representative narrow viewports.
- Portal and Desktop intentionally share presentation through `@buildmax/gui`.
  Shared primitives must remain data- and routing-neutral.

## Supported layout ranges

The design defines behavioral ranges rather than device-specific layouts:

| Range | Reference width | Shell behavior |
|---|---:|---|
| Wide | 1280 px and above | Persistent navigation and multi-column content where useful |
| Compact | 768–1279 px | Collapsible navigation and reduced secondary detail |
| Narrow | 320–767 px | Single content column with navigation in an overlay drawer |

The reference widths are test points, not assumptions about exact devices.
Content must continue to reflow between them and at 200% browser zoom. A feature
may use an earlier breakpoint when its content establishes the need; breakpoints
are owned by layouts and components rather than copied page by page.

## Interaction decisions

At narrow widths the shell presents a compact header containing the current
Space and a menu control; the page's own H1 names the page, so the header does
not repeat it ([page system](portal-frontend-page-system.md#page-anatomy-and-templates)).
The menu opens an accessible overlay
drawer containing the complete scoped navigation and account actions. Selecting
a destination closes it and restores focus predictably. The persistent desktop
sidebar is not squeezed into the document flow.

Breadcrumbs preserve the current object and nearest useful parent. Earlier
ancestors may collapse into an overflow control, but the Space context remains
visible in the shell. Primary page actions stay near the title or in a labeled
overflow menu; they do not disappear solely because of width.

Files uses progressive navigation on narrow layouts: the user sees
either the folder list or the selected item, with a clear Back action. Wide
layouts may keep the tree and content side by side. No narrow page requires
simultaneous horizontal and vertical scrolling to select a file.

Tables choose one of three explicit narrow behaviors: reflow to labeled cards,
retain a clearly indicated horizontal scroll region, or hide secondary columns
behind a row detail. Arbitrary clipping is not a behavior. Dense execution
traces may scroll horizontally inside their own labeled region without making
the entire page scroll.

Modals become full-height sheets or near-full-viewport dialogs on narrow
layouts. Their header and primary actions remain reachable while the body
scrolls. Side-tab dialogs change to a horizontal tab list or sequential panels.

## Accessibility requirements

- Navigation drawer, menus, tabs, dialogs, and disclosure controls expose names,
  state, and relationships with semantic elements and appropriate ARIA.
- Dialogs trap focus while open, close with Escape where safe, restore focus to
  the opener, and prevent background interaction.
- All actions are keyboard reachable in a logical order with a visible focus
  indicator. Reflow does not reorder focus differently from visual reading.
- Interactive targets are at least 44 by 44 CSS pixels unless adjacent spacing
  provides an equivalent target area.
- Status and mutation feedback is announced without moving focus unexpectedly.
- Color is not the only signal for status, error, permission, or selection.
- Motion respects `prefers-reduced-motion`; zoom and text resizing do not hide
  controls or require horizontal page scrolling at the narrow reference width.

## Implementation slices

1. **Shell and navigation.** Introduce the compact header and drawer, then test
   Space switching and all primary destinations.
2. **Shared overlays.** Correct focus management, body scrolling, side tabs, and
   narrow dialog geometry in `@buildmax/gui` without importing Portal policy.
3. **Core work journey.** Reflow Chat, Issues, Issue Detail, Task Detail, and
   their actions at compact and narrow widths.
4. **Data and management.** Add progressive Files navigation and explicit table
   behavior for Artifacts, Plugins, membership, audit, and administration.
5. **Regression matrix.** Add automated viewport and keyboard coverage to the
   existing Portal browser suite.

Each slice can merge independently if it does not regress already supported
journeys. A component migration includes its focus, zoom, and narrow-width
tests; a global stylesheet rule alone is not evidence of support.

## Acceptance criteria

- At 390, 768, and 1280 CSS pixels, a user can switch Space, start Chat, open an
  Issue and its latest run, browse Files, and reach Space settings.
- There is no horizontal document scroll at 390 pixels or at 200% zoom; any
  component-local horizontal region is labeled and keyboard operable.
- Navigation and modal interactions pass keyboard-only tests, including focus
  entry, containment, closing, and restoration.
- Primary actions and current Space remain discoverable in every layout range.
- File browsing does not render a fixed side tree beside unreadably narrow
  content.
- Automated browser coverage runs representative viewport tests and a focused
  accessibility scan for the shell, one dialog, and one tabbed surface.
- Manual checks cover screen-reader announcements for route change, error,
  mutation completion, and dialog title.

## Alternatives rejected

- **Shrink the desktop sidebar.** It preserves every control by making all of
  them harder to scan and does not solve focus or scroll behavior.
- **Support only a minimum desktop width.** Operator work still arrives through
  tablets, split windows, and zoomed desktops; the core journeys must reflow.
- **Use one global mobile media query.** Files, tables, traces, and dialogs need
  different interaction decisions that cannot be derived from width alone.
- **Treat accessibility as later polish.** Drawer, modal, and tab architecture
  determines semantics and focus behavior; retrofitting it duplicates work.

## Related records

- [Portal navigation and Space context](portal-navigation-and-space-context.md)
- [Portal state and permission feedback](portal-state-and-permission-feedback.md)
- [Surface positioning](surface-positioning.md)
- [Local end-to-end verification](end-to-end-testing.md)

