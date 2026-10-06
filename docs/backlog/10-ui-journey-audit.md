---
id: ui-journey-audit
title: Audit the Portal and Desktop core journeys and grade the findings
roadmap: R6
source: docs/design/ui-experience-program.md#d2-agent-driven-validation-recorded-as-such
depends_on: []
verification: ["kind", "./make check docs"]
claim:
pr:
---

## Outcome

The maintainer gets a graded, reproducible picture of where Portal and Desktop
fail a new person. That picture decides which screens are reworked and whether
the visual language changes. Today every UI journey has been checked only by its
implementer.

## Scope

Write the charters first. Then run them under
[exploratory testing](../contribute/exploratory-testing.md) and commit one
report per surface under `docs/contribute/exploratory-runs/` (English plus the
`zh-CN` mirror).

- **Portal**: use an ephemeral kind cluster
  (`BUILDMAX_KIND_EPHEMERAL=1 ./make kind up`, then `./make kind down`). Run
  these journeys:
  - first sign-in and Space orientation
  - creating an Issue and running it to a result
  - diagnosing a failed run from the Issue and from Administration
  - authoring and running a Workflow
  - setting up a Schedule
  - finding a shared Artifact or File
  - inviting a member and changing their role
- **Desktop**: use `wails dev`'s browser bridge. Run these journeys:
  - first launch with no project
  - opening a project and running a chat that edits a file
  - reviewing the diff
  - using the terminal tab
  - creating a local Schedule
  - signing in and reaching the Issues view
- **Capture**: take each journey at 390, 768, and 1280 px, in light and dark
  themes. Keep screenshots under `.artifacts/`. Inline only the evidence the
  report needs.
- **Grade**: grade every finding Blocker, Major, Minor, or Cosmetic against the
  rubric in the design record. Give each one its screen, step, and
  reproduction.
- **Close the report** with:
  - a visual-language recommendation (refine or new) and the findings that
    drive it
  - evidence on the default-landing-page question

## Out Of Scope

- Fixing findings. Rework tasks are drafted from this report (design phases
  3–4). A trivial defect may be fixed in a separate pull request.
- Locale coverage, because no catalog exists yet. The re-audit in phase 5
  covers it.
- The CLI/TUI.

## Acceptance Criteria

- Two committed reports exist, Portal and Desktop. Each has a charter, the
  environment and commit, the model used, and graded findings with
  reproductions.
- Each report says that the operator was an Agent with repository knowledge.
- Each report lists what the browser bridge could not observe.
- The reports end with the visual-language recommendation and the
  landing-page evidence.
- Both reports are added to the Records table in
  `docs/contribute/exploratory-runs/README.md` and its mirror.

## Verification

Run `./make check docs`. The kind cluster this task created is removed with
`./make kind down`.

## Notes

These existing specs and documents already cover parts of the journeys and can
seed charters:

- Portal: `portal/e2e/golden-path-viewports.spec.ts` and
  `work-journey-responsive.spec.ts`
- Desktop: `desktop/frontend/e2e/golden-path.spec.js`
- The Q2/Q7 journeys in `docs/deploy/beta-readiness.md`

The reports stay in place until phase 3–4 tasks are drafted from them, then
they are deleted, following the exploratory-runs lifecycle.
