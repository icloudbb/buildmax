---
id: workflow-detail-state-driven-ux
title: Compose the Workflow detail page by lifecycle state
roadmap: none
source: docs/design/workflow-runtime.md#17-portal-and-operational-experience
depends_on: []
verification: []
claim: gougoujiang 2026-09-19
pr: 663
---

## Outcome

A member opening a Workflow lands in the layout that matches what the Workflow
is for: a `draft` opens in an authoring layout whose primary action is Publish,
a `published` one opens in an operating layout whose primary action is Run with
editing behind an explicit Edit affordance. Today one long page shows the edit
form, a disabled Run button, revision history, and recent runs at once
regardless of state, so a published Workflow surrounds a reader who only wants
to run it with an editable form (easy to change by accident), and a draft
dangles a Run control the runtime always refuses.

## Scope

Restructure `portal/src/pages/workflows/WorkflowDetail.tsx` (and extract
components as needed) so the detail page composes by `workflow.status`:

- `draft`/`archived` → authoring layout: Definition form + steps editor +
  read-only topology; primary action Publish; no manual Run.
- `published` → operating layout: read-only topology (Plan) and Recent Runs lead
  the body; primary action Run; Definition editing retreats behind an Edit
  affordance; a member without manage capability sees this layout without Edit.
- Run input generated from `input_schema` moves into a Run drawer opened by the
  Run action, replacing the inline `WorkflowRunInputForm` that renders whenever a
  schema exists.
- Revision history (`RevisionHistory`) becomes on-demand secondary information
  reached from the header, not a resident grid column; restore stays available
  there.
- Reuse the existing read-only `WorkflowGraph` (from `features/workflows`) to
  render the topology; derive its nodes from the workflow definition the same
  way the run page derives them from node runs.

Keep the existing capability gating (`canManageWorkflows`), the run-on-published
invariant, and all current save/publish/restore/run API calls unchanged; this is
a presentation and composition change, not a contract change.

## Out Of Scope

- Direct canvas manipulation of nodes (drag, connect, add/remove on the graph):
  that is the evidence-gated follow-up in
  `docs/design/workflow-runtime.md#17-portal-and-operational-experience` item 6,
  and needs its own design (node coordinate persistence, interaction model)
  before it becomes a task. Do not add graph editing here.
- Test Run of a `draft` (item 4's authoring test-run capability): the runtime
  still only runs `published` definitions, so a draft shows no Run until that
  capability lands separately. Do not relax the run-on-published invariant.
- Any change to the Workflow definition contract, run/node state machines, or
  server routes.

## Acceptance Criteria

- A `draft` Workflow shows the authoring layout with Publish and no Run control;
  a `published` Workflow shows the operating layout with Run and an Edit
  affordance.
- A member who cannot manage Workflows sees the operating layout for a published
  Workflow, can Run it, and has no Edit affordance.
- Running a published Workflow with an `input_schema` collects input in a drawer,
  then navigates to the run page exactly as today.
- The definition topology renders read-only via `WorkflowGraph` on the detail
  page in both layouts.
- Revision history and restore are reachable on demand from the header and no
  longer occupy a resident column; restore still creates a new revision.
- `WorkflowDetail`'s existing states — loading, `ResourceUnavailable` for
  404/403/error, and the capability-denied/failed/unknown messages — are
  preserved.

## Verification

`./make check portal` for the component and type changes, then the local kind
loop with `./make e2e kind` to exercise the browser flow against real routes
(sign in as `alice@buildmax.local` via `./make kind login`, open the seeded
"Release Notes" Workflow, toggle draft↔published, run it, open history). Update
or add the Workflow-detail Playwright spec under `portal/e2e/` for the
state-driven layout and the Run drawer, since the selectors move.

## Notes

Direction approved with the maintainer in an interactive Portal review of the
seeded "Release Notes" Workflow (fixtures space, `alice@buildmax.local`). The
approved composition is written into
`docs/design/workflow-runtime.md#17-portal-and-operational-experience`; its
authoring-order items 3 (read-only topology) and 6 (direct canvas manipulation,
deferred) bound this task's scope. Low-fidelity mockups of the three states
(draft authoring, published operating, history drawer) were reviewed during that
session and are not committed.

The read-only `WorkflowGraph` already exists and is used only on
`WorkflowRunDetail.tsx` today; this task is its first use on the authoring/detail
side. Current steps editing stays the form-based `WorkflowStepsEditor`.

After merge, refresh `docs/current-state.md` if it describes the Workflow detail
layout, and add a changelog entry (`./make changelog new changed <slug>`) since
this is user-visible.
