# Portal Frontend Page System

> **简体中文：** [阅读中文镜像](../zh-CN/design/portal-frontend-page-system.md)
>
> **Audience:** Portal contributors · **Status:** active plan — partial
>
> **Opened:** 2026-09-19

Related: [roadmap](../ROADMAP.md), [product vision](product-vision.md),
[Portal architecture](../contribute/architecture/portal.md),
[navigation and Space context](portal-navigation-and-space-context.md),
[work and execution experience](portal-work-and-execution-experience.md),
[state and permission feedback](portal-state-and-permission-feedback.md),
[responsive and accessible interaction](portal-responsive-and-accessible-interaction.md),
and the separate [Issue board proposal](../proposals/portal-issue-board-view.md).

## Contents

- [Decision](#decision)
- [User outcome, evidence, and constraints](#user-outcome-evidence-and-constraints)
- [Recommended visual direction](#recommended-visual-direction)
- [Action and button grammar](#action-and-button-grammar)
- [Page anatomy and templates](#page-anatomy-and-templates)
- [States, responsive behavior, and accessibility](#states-responsive-behavior-and-accessibility)
- [Ownership and migration](#ownership-and-migration)
- [Options and non-goals](#options-and-non-goals)
- [Evidence and acceptance](#evidence-and-acceptance)
- [Open questions](#open-questions)

## Decision

Should Portal adopt one small, enforceable page and component system so the same
action has the same prominence and behavior across collections, details, editors,
and diagnostics? **Yes.** Keep the existing neutral visual
language, define action hierarchy and page anatomy first, and migrate the work
journey before secondary management pages. Do not redesign by applying a new
palette to the existing screens: the current information hierarchy and duplicate
actions would remain.

This is an accepted direction with a partial implementation. The current-state
document distinguishes shipped behavior from the remaining migration. This
decision does not change the product model or the roadmap's Beta gate.

## User outcome, evidence, and constraints

The essential outcome is that a Space participant can scan a page, find its one
next action, understand the state of work, and predict how a control will behave
without relearning each page. An operator can distinguish Space work from
deployment administration. The same journey must work at wide, compact, and
narrow widths and in both themes.

Read-only inspection on 2026-09-19 used the resident local QA Portal at
`http://localhost:8080`, with the served asset names matching `portal/dist` in
checkout `95e3f0be`. It covered Chat, Issues and Issue Detail, Agent and Task,
Workflow and Workflow Run, Schedules, Files, Artifacts, Space settings, and
Administration at 1280 and 390 CSS pixels. No product data was changed. This
was a sampled walkthrough, not a user study or a complete end-to-end Issue run.

Concrete findings:

| Surface | Observed friction | Source of the pattern |
|---|---|---|
| Schedules empty collection | The header's white **New schedule** and empty state's black **New schedule** appear together. They have the same effect but conflicting prominence. | `SchedulesPage.tsx` supplies both actions; `EmptyState.tsx` forces its action to `.btn--primary`, while `.page-activity__action-btn` is secondary. Issues, Workflows, and Agents have the same possible duplication. |
| Buttons across Portal | `.btn`, `.page-activity__action-btn`, `.modal__btn`, `.admin-button`, `.tp-btn`, and local button rules independently set geometry, color, and focus. Some create/submit actions use secondary styling. | `portal/src/css/`, `gui/src/modal.css`, and call sites. |
| Issue Detail | The first viewport is dominated by editable title and an eight-row description. The latest outcome follows the form and sub-issues. On narrow screens, compact title, breadcrumb, generic **Issue Detail**, and the form title repeat orientation content. | `IssueDetail.tsx` Overview ordering and `Layout.tsx` compact header. |
| Workflow Run | Run IDs and metadata precede the result; graph node text clips inside a fixed 52px node. | `WorkflowRunDetail.tsx`, `WorkflowGraph.tsx`, and `workflows.css`. |
| Global administration | The Space switcher remains active while Administration describes deployment-wide authority. | Shared `SidebarNavContent` in `Layout.tsx`. |
| Labels and loading | Human-facing rows and forms show machine values such as `todo`, `in_progress`, `succeeded`, `workflow_step`, and `agent_task`; a collection can briefly show “0” and pagination while loading. | Page-local rendering across Issues, Task, Workflow Run, and collections. |

Existing strengths remain foundations: Space-scoped routes, the Issue's four
user-level areas, explicit Save and Run, state/permission vocabulary, responsive
drawer, and Artifact preview. Portal and Desktop share presentation through
`@buildmax/gui`; the shared package must stay free of Portal routing, data,
authentication, and domain policy. Issue is the primary shared work object;
Task and TaskRun retain execution authority. The accepted responsive and state
records remain authoritative.

## Recommended visual direction

Use a restrained operational interface: neutral canvas, clear type hierarchy,
few borders, and semantic color only for meaning. Keep the existing light/dark
theme inversion. A filled primary button may be dark in light mode and light in
dark mode; its **role** must be consistent even when its literal color changes.
Avoid using a solid fill merely to make a sparse page feel occupied.

Start from the existing `@buildmax/gui/theme.css` colors. Add only tokens needed
to remove repeated page-level decisions:

| Token family | Initial contract |
|---|---|
| Color | Canvas, raised surface, subdued surface, primary/secondary text, border, focus, primary action, destructive action, and status tones. Page CSS consumes semantic tokens, not new hex values. |
| Type | One body size, one compact metadata size, section heading, and page/object heading. Opaque IDs and code use a monospace style only when exposed as technical detail. |
| Space | A short 4/8/12/16/24/32px scale for component gaps and page gutters. Wide gutters are 32px; narrow gutters are 16px. |
| Shape and motion | One control radius, one panel radius, restrained elevation for overlays, and reduced-motion support. No arbitrary page-specific transition. |

The specimen and browser review choose final token values before migration.
Text contrast, focus visibility, and status meaning are tested in both themes.
Token names should describe roles; avoid a token per component or page.

## Action and button grammar

One presentational `Button`/`IconButton` family owns size, focus, hover,
disabled, busy, and icon alignment. It supports button and link semantics
without changing the visual rule. Domain code supplies the action, permission,
label, and loading state.

| Role | Appearance | Placement and examples |
|---|---|---|
| Primary | Filled, high contrast | One next action for the current scope: **New issue** on a collection, **Run Agent** on a ready Issue, **Save changes** in an editor. |
| Secondary | Outlined or quiet raised surface | Parallel but less consequential actions: **Edit**, **Download**, **Cancel**. |
| Tertiary | Text/ghost | Navigation and utilities: **Back**, **Refresh**, **View details**. Breadcrumbs remain links. |
| Destructive | Explicit danger outline and confirmation where required | **Delete**, **Revoke**, **Destroy**. It never borrows the normal primary styling. |

Rules that page components enforce:

1. A view has one primary action for its current task. A form may have its own
   primary Save action after entering edit mode; the read view and edit form are
   distinct scopes. A status badge is never styled as a button.
2. Do not render the same action in the page header and an empty state within
   one visible view. The header owns collection creation; the empty state gives
   context and may point to that action. If the header action is absent, the
   empty state may own the single primary action. A long independently scrolling
   region may repeat an action only when the first control is no longer visible
   and the repetition is explicitly tested.
3. Visual priority follows the user's next step, not component origin. A
   generic `EmptyState` must not force every supplied action to primary.
4. A disabled or busy action explains why, prevents duplicate submission, and
   retains its width. Icon-only actions have a name and at least a 44px target.
5. Labels use verbs and object names. Save persists; Run schedules work. An
   unsaved executor change cannot silently affect or disagree with Run.

For the current Schedules empty page, keep one filled **New schedule** in the
header. The empty panel reads “No schedules yet. Schedule an Agent to run at a
set time.” and contains no second button. Apply the same decision to Issues,
Workflows, and Agents. In creation dialogs, make **Create** primary and
**Cancel** secondary; today some creation submit buttons are secondary.

## Page anatomy and templates

The shell communicates scope once. On wide and compact screens it shows the
current Space and resource navigation. On a global Administration route it
shows a deployment context instead of an active Space switcher. On narrow
screens it shows Space plus a menu control; the page owns one real H1. Detail
pages show a back/parent breadcrumb, without repeating the current title in
three stacked places.

| Template | Anatomy | Portal examples |
|---|---|---|
| Collection | Scope and H1 → one primary creation action → optional filters/count → rows or a specific empty/error state → pagination. Do not add “All X” when the page contains only one collection. | Issues, Agents, Workflows, Schedules, Artifacts |
| Object detail | Object title/status → owner/executor and next action → latest outcome or useful summary → related content → history/diagnostics. Read mode precedes edit mode. | Issue, Agent, Workflow, Artifact |
| Work/run detail | Result and artifacts → current state/recovery action → execution steps → provenance and trace. IDs are secondary copyable metadata. | Task, Workflow Run, Issue Runs |
| Editor | Named object and dirty state → grouped fields with local help/errors → sticky Save/Cancel on narrow screens. Save success identifies the object and effect. | Issue edit, Workflow edit, Agent config, settings |
| Administration | Deployment scope cue → section navigation → status summary → task-specific tables/forms. Security-sensitive controls keep explicit authority and feedback. | Administration, Space settings (Space scope) |

These are compositional templates, not new persistent entities. Prefer a small
Portal `PageHeader`, `CollectionFrame`, `DetailHeader`, `StatusLabel`, and
existing `EmptyState`/`Alert` over a universal page component with dozens of
flags. Tabs and disclosure patterns have one semantic implementation. Shared
presentation primitives belong in `@buildmax/gui` only when they are neutral
enough for Desktop; Portal owns object vocabulary and page composition.

## States, responsive behavior, and accessibility

- Use the existing resource-state model. Loading keeps the shell stable and
  shows progress; it does not claim a zero count or an empty collection before
  the request succeeds. Empty, error, forbidden, stale, and refreshing remain
  distinct with local next actions.
- Use one presentation vocabulary for business status, run status, and
  lifecycle status. Each has text plus shape/icon where useful; color alone
  never carries meaning. Technical enum values remain available in details.
- At 390, 768, and 1280 CSS pixels, the same action hierarchy survives reflow.
  Header actions may move below the title or into a named menu but keep their
  priority and accessible name. A page must not introduce horizontal document
  scrolling at 390px or 200% zoom.
- Focus, keyboard order, announcements, contrast, reduced motion, modal focus,
  and 44px targets follow the existing responsive/accessibility design. The
  design specimen includes light, dark, loading, empty, error, forbidden,
  disabled, busy, and long-content states before page migration.

## Ownership and migration

1. **Audit and specimen.** Inventory button class families and duplicate
   actions; build a reviewable component/page specimen using current Portal
   content at 390/768/1280px in light and dark themes. Record the baseline
   journey and exact visual defects. This is a design artifact, not a new app.
2. **Foundation.** Add semantic tokens and the shared presentational button
   family to `@buildmax/gui`; retain Portal-only composition in `portal/`.
   Replace legacy button classes rather than making them permanent aliases.
   Migrate modal submit actions, empty states, and focus/busy behavior together.
3. **Core work pages.** Migrate Issues collection/detail, Task, Workflow Run,
   and Chat. Fix result hierarchy, status labels, duplicate actions, and graph
   clipping. Where UI correctness depends on create/run contracts, change the
   owning service/API coherently rather than masking ambiguity in the frontend.
4. **Remaining pages.** Migrate Agents, Workflows, Schedules, Files, Artifacts,
   Space settings, Administration, and Marketplace. Remove obsolete CSS rules
   after each slice; avoid an indefinite old/new style layer.
5. **Docs and verification.** Update the user manual and its Chinese mirror,
   contributor Portal architecture, current-state description, and changelog
   for shipped behavior. Run `./make check gui`, `./make check portal`, focused
   browser journeys, and `./make e2e kind` for browser-visible behavior. Review
   with an operator who did not build the UI before claiming the journey is
   easier. The existing Beta qualification remains a separate gate.

The first implementation slice covers shared buttons, creation actions in the
four work collections, read-first Issue Detail, Workflow Run result hierarchy,
and deployment scope in Administration. A second slice brings the same action
roles and human run status to Task Detail and conversation task cards, and gives
the Chat start page a visible heading, keyboard-operated tabs, and a real Files
link. Chat also keeps failed Task actions attached to their card and names a
failed Task-list load without hiding the conversation. Workflow Detail now
uses the shared actions and readable lifecycle labels, with Publish, Run,
or Save taking priority according to the visible mode. Agent Detail and its
Schedules tab now use the same action roles; the tablist
works by keyboard, and failed triggered-task loads can be retried in their card.
Artifacts now use the shared roles for upload, download, sharing, and deletion;
the collection does not claim a count before loading, and previews have local
retry.
The remaining Issue Detail actions, Administration, Space settings, Files,
Marketplace, sign-in, and the shared unavailable, copy, and revision controls
then moved to the same roles, and the legacy `.btn`, `.page-activity__action-btn`,
`.admin-button`, `.tp-btn`, and page-local button rules were deleted rather than
aliased. Issue creation now sends status, Owner, and Executor with the create
request, so the API writes them in one step and the dialog no longer needs a
partial-create recovery path.
The audit-and-specimen step was skipped as a token-selection gate: migration
reused the existing theme values instead of choosing tokens from a specimen. A
component specimen has since been built, served at `/specimen` outside the
authenticated shell — not to re-choose tokens, but as a standing review artifact
that renders the action grammar, semantic tokens, status vocabulary, and every
resource state once, in both themes and reviewable at 390/768/1280px, reusing the
real components so a divergence there is a divergence in Portal. The independent,
non-author operator review of the Issue-to-result and failure-to-diagnosis
journeys remains. Each further slice needs its own observable outcome and
verification.

## Options and non-goals

| Option | Trade-off |
|---|---|
| Page-by-page CSS polish | Fast local fixes, but keeps competing button systems and duplicate-action rules. Insufficient for the observed cross-page problem. |
| **Small system plus staged migration (recommended)** | Creates one action grammar and five page templates; lets core work improve first and removes obsolete styles as pages migrate. |
| New visual framework or broad component library | Adds dependency and conceptual surface before current Portal patterns are settled. No observed user outcome requires it. |

This design does not add a Dashboard, Board, new work entity, brand campaign,
or Desktop page redesign. The Board question remains in its own proposal.
Desktop should receive a shared primitive change only when its presentation is
reviewed and verified; Portal-specific layout must not leak into `@buildmax/gui`.

## Evidence and acceptance

The first complete slice is accepted only when:

- Schedules, Issues, Workflows, and Agents empty collections show at most one
  visible creation control for the same action, with the same role and styling
  as their loaded collections.
- Every primary/secondary/destructive control in the migrated pages uses the
  single button contract; no page CSS restyles its geometry or focus behavior.
- A participant can open an Issue, identify status, Owner, Executor, latest
  outcome, and the available next action without first traversing an edit form.
  Save and Run remain separate; unsaved changes cannot schedule an unexpected
  executor. A failed create cannot ambiguously leave a partially created Issue.
- A completed Task or Workflow Run leads to its result and Artifact before
  trace-level details. The Workflow graph shows every node's label and status
  without clipping at reference widths and 200% zoom.
- Global Administration does not imply that its authority changes with the
  selected Space. No raw machine enum or opaque ID is the primary label of a
  work object or action.
- Keyboard-only paths, focus restoration, accessible names, light/dark
  contrast, and loading/error/empty states pass the existing Portal browser
  matrix. One non-author operator completes the Issue-to-result and
  failure-to-diagnosis journeys; report steps, obstacles, and limits rather
  than inventing a satisfaction score.

## Open questions

1. Should the default Space destination stay Chat or become Issues after an
   operator journey? The page system does not need to decide this first.
2. Which real Issue results and failure states should seed the specimen and
   operator review? Existing QA data did not complete an Issue-to-result run
   during this inspection.
3. Does the Issue board proposal earn priority after the improved filtered
   list is tested? The page system should support either answer.
