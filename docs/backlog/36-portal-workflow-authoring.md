---
id: portal-workflow-authoring
title: Make Workflow authoring readable without developer knowledge, and explain why Run is disabled
roadmap: R6
source: docs/design/ui-experience-program.md#phases
depends_on: []
verification: ["./make check portal", "./make e2e visual", "./make e2e local"]
claim:
pr:
---

## Outcome

A Space member can author a Workflow in the person-facing terms the runtime
supports, with room to see the graph. They can tell why a Draft cannot run
yet. This task carries Portal audit finding P10.

## Findings

**P10 (Minor). Workflow authoring is dense and uses developer terms.**

- Screen: the New Workflow modal and Workflow Detail.
- Reproduction: create a Workflow with an Agent step and save it as Draft.
- Actual:
  - The React Flow graph editor lives in a 600 px modal.
  - Step IDs are machine-made, such as `step_a5fd8c28`.
  - Agent options carry raw IDs, such as "Onboarding summarizer
    (vgivleuxrkt2av5233xa)".
  - "Issue access" offers the raw values `none`, `if_bound`, and `required`.
  - **Run Workflow** is disabled on a Draft with no explanation: no title and
    no description.
  - At 390 px the canvas is barely usable.

## Scope

- Give the graph editor a surface sized for a graph, not a 600 px modal.
- Generate step identifiers and keep them out of the primary form, as the
  [work experience record](../design/portal-work-and-execution-experience.md#workflow-authoring)
  specifies. They stay visible where linking or diagnostics need them.
- List Agent options by name. Show a disambiguating detail only when two
  Agents share a name.
- Give each "Issue access" value a person-facing label and a one-line hint, in
  both locales.
- When **Run Workflow** is disabled, give the specific reason, such as
  "Publish this Workflow to run it". This follows the page-system action rule
  that a disabled action explains why.
- At 390 px a person can read the steps and their order and reach Run. If
  editing the graph needs a wider window, the page says so.

## Out Of Scope

- New step types or runtime capability.
- The run page's result and output presentation, which is
  [task 28](28-portal-issue-result.md).

## Acceptance Criteria

- Authoring a two-step Workflow never shows a step ID, an Agent ID, or a raw
  Issue access value as a primary label.
- A disabled **Run Workflow** states its reason in text that a screen reader
  announces.
- At 390 px the Workflow page has no horizontal document scroll, and its
  steps are readable.

## Verification

1. `./make check portal`.
2. `./make e2e visual`. The editor template baseline may change on purpose.
3. `./make e2e local`, including the existing Workflow editor tests.

## Notes

The [Workflow visual editor record](../design/portal-workflow-visual-editor.md)
remains the specification for the graph editor.

Audit context: the phase 0 Portal audit ran on 2026-10-06 against `main` at
`e98efc7a`, on an ephemeral kind cluster with the mock model. The operator was
an Agent with repository knowledge. The full report is kept in history at
[`718a6ab3`](https://github.com/icloudbb/buildmax/blob/718a6ab3969d35bdba7f66013d8ccfefe7549af8/docs/contribute/exploratory-runs/2026-10-06-portal-ui-journey-audit.md).
