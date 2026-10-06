---
id: portal-work-naming
title: Name runs and work items by what they serve, not by IDs, raw prompts, or raw Markdown
roadmap: R6
source: docs/design/ui-experience-program.md#phases
depends_on: []
verification: ["./make test", "./make check portal", "./make e2e visual", "./make e2e kind"]
claim:
pr:
---

## Outcome

Every run and work item is labelled with the name a person knows it by: the
Issue or Workflow it serves, or a readable title. Opaque IDs become secondary
metadata that can be copied. This task carries Portal audit findings P8, P17,
P18, and P25.

## Findings

**P8 (Minor). Task naming is unstable.**

- Screen: the Task page, its breadcrumb, and Agent run lists.
- Actual:
  - The Task heading is a generated title. With the mock model it is
    "deployment smoke ok" for every run, including runs that failed before
    starting.
  - The breadcrumb shows the Issue title when the Task is reached from the
    Issue, but a generic "Issue" after a reload.
  - Agent run lists mix generated titles with raw prompts, such as
    "Agent: QA Reviewer Description: Reviews acceptance…".
  - Runs are never named after the Issue or Workflow they serve. The mock
    exaggerates the problem, but the problem is real.

**P17 (Minor). Raw IDs appear in work views.**

- Issue Detail: "Latest agent task: f4qupcltm5roh4o5snrq".
- Task details: "Task czdpj4…".
- Workflow step: "Task: 7yzq… / Run: ruw5…".

**P18 (Cosmetic). Issue list snippets show raw Markdown**, for example
"## Acceptance criteria - [ ] …".

**P25 (Cosmetic). Grammar.**

- "1 tasks": `issues.runs.tasks` in `portal/src/i18n/issues.ts` has no plural
  form.
- "1 secret grant no longer resolve": the `one` forms in
  `portal/src/i18n/agents.ts` use the plural verb.

## Scope

- Label each Task from its authoritative origin, following the
  [execution provenance](../design/portal-work-and-execution-experience.md#execution-provenance)
  rules. An Issue-origin Task shows the Issue's title, a Workflow-origin Task
  shows the Workflow and step, and a generated title is secondary. Breadcrumbs
  name the origin after a reload too. If the Task response lacks the origin's
  name, add it in the owning service rather than with an extra client lookup
  per row.
- Show a run that failed before it started under its origin's name, never
  under a generated title.
- Make human labels primary in Issue Detail, Task details, and Workflow steps.
  IDs stay available as secondary, copyable metadata, as the page-system record
  specifies.
- Render Issue list snippets as plain text with the Markdown syntax removed.
- Fix the two plural messages in both locales.

## Out Of Scope

- How title generation itself works for the real model.
- The Issue result layout, which is [task 28](28-portal-issue-result.md). Both
  tasks touch the Issue Overview, so coordinate if both are in flight.

## Acceptance Criteria

- With the mock model, a run started from an Issue is listed and headed by the
  Issue's title on the Task page, in Agent run lists, and in the breadcrumb,
  including after a reload.
- No work view shows an opaque ID as the primary label.
- Issue list snippets contain no Markdown syntax.
- "1 task" and "1 secret grant no longer resolves" render correctly.

## Verification

1. `./make test`, if the Task response changes.
2. `./make check portal`, then `./make e2e visual`.
3. Use the kind loop with `./make kind fixtures --runs`, then `./make e2e kind`.

## Notes

Audit context: the phase 0 Portal audit ran on 2026-10-06 against `main` at
`e98efc7a`, on an ephemeral kind cluster with the mock model. The operator was
an Agent with repository knowledge. The full report is kept in history at
[`718a6ab3`](https://github.com/icloudbb/buildmax/blob/718a6ab3969d35bdba7f66013d8ccfefe7549af8/docs/contribute/exploratory-runs/2026-10-06-portal-ui-journey-audit.md).
