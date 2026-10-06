---
id: gui-shared-primitives
title: Add the shared form, menu, feedback, and status primitives to @buildmax/gui
roadmap: R6
source: docs/design/ui-experience-program.md#d4-one-presentation-layer-for-both-surfaces
depends_on: []
verification: ["./make check gui", "./make check portal", "./make e2e local"]
claim:
pr:
---

## Outcome

The same control looks and behaves the same everywhere it appears. Rework
tasks compose shared parts instead of copying page-local markup. Today menus are
rebuilt in seven places, inputs borrow modal classes, and each site writes its
own loading text.

## Scope

Add these components to `gui`, each with its vitest behavior tests and a
`/specimen` entry:

- `Field`, combining label, hint, and error, plus `Input`, `Select`, and
  `Textarea`
- `Menu` and `Popover`, with keyboard navigation and focus return
- `Toast`
- `Spinner` and `Skeleton`
- `StatusLabel`, over one status vocabulary
- the page-system anatomy components: `PageHeader`, `CollectionFrame`, and
  `DetailHeader`

Move Portal's two status-label sources (`lib/statusLabels.ts` and
`features/conversations/thread.ts`) onto the single vocabulary.

Migrate the Portal call sites of each primitive in the same change, so no
second implementation remains. Before merging, check the inventory again for
any primitive that turned out to be used only once, and remove it again.

Add a sticky Save/Cancel bar for narrow editors, as the page-system record
specifies.

## Out Of Scope

- Desktop adoption, which is
  [task 20](20-desktop-gui-convergence.md).
- A table component. Add one only if the phase 0 audit shows that tables need
  it.
- Visual restyling beyond tokens. That waits for design phase 2.

## Acceptance Criteria

- Each primitive has vitest coverage of its keyboard and ARIA behavior and
  appears on `/specimen`.
- Portal contains no page-local menu, input styling borrowed from modal
  classes, or hand-written "Loading…" text where a primitive applies.
- One function maps a status to its label.
- The page-system record marks these items shipped.

## Verification

Run, in order:

1. `./make check gui`
2. `./make check portal`
3. `./make e2e local`
4. The screenshot check from task 14, if that task has landed

## Notes

The page-system record already specifies anatomy and action grammar. Implement
it; do not redesign it.
