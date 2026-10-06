---
id: portal-issue-result
title: Give an Issue and a Workflow run one consistent answer to "did it work and what did it produce"
roadmap: R6
source: docs/design/ui-experience-program.md#phases
depends_on: []
verification: ["./make test", "./make check portal", "./make e2e visual", "./make e2e kind"]
claim:
pr:
---

## Outcome

After a run finishes, the Issue says once, in one place, whether it worked and
what it produced. Its tabs agree with that answer. A Workflow run page does the
same and labels each step's output as output. This task carries Portal audit
findings P1 (Major) and P11.

## Findings

**P1 (Major, state legibility). An Issue's result is described in five places
that contradict each other.**

- Screen: Issue Detail (Overview, Results tab, Runs tab) and the Workflow run
  page.
- Reproduction:
  1. Create an Issue.
  2. Set an Agent executor and choose **Run agent**.
  3. When the Task shows Done, return to the Issue.
- Actual:
  - The Overview card says **Latest result: No result yet.**
  - The lower **Latest Outcome** section says **Succeeded** and shows the
    Task ID.
  - The **Results** tab says "No results produced yet".
  - The **Runs** tab says "Run History 0 total … No runs yet.", then
    "Agent Run Sequence 1 tasks … Succeeded".
  - The Agent's actual output ("deployment smoke ok" from the mock) appears
    only as a **Discussion** comment.
  - In Chinese, both labels read **最新结果**: one says "暂无结果" and the
    other "成功".
  - The Workflow run page repeats the pattern. It says "Result: Succeeded — No
    result was produced", while the step's output sits further down.
- Expected: one answer to "did it work and what did it produce?" on the Issue.
  The duplicate "Latest Outcome" is worse than a duplicate, because the two
  sections disagree.

**P11 (Minor). A Workflow step's output is unlabeled.**

- Screen: the Workflow run page, inside a step.
- Actual: the output appears as a bare block directly under the collapsed
  "Resolved input this node received" summary, so it reads as the input.
- Expected: the output is labelled as output and kept apart from the input.

## Scope

- **Issue Overview.** One latest-outcome element shows status, a short form of
  what the run produced, and a link to the run. The second, conflicting
  section goes. The
  [work experience record](../design/portal-work-and-execution-experience.md#issue-experience)
  defines the areas: Overview has the latest outcome, Results has published
  Artifacts and structured outcomes, and Runs has Task and TaskRun history.
- **Results tab.** It shows what the runs produced. When a run's only output
  was text, the tab says so and shows or links that text. It does not claim
  that nothing was produced.
- **Runs tab.** One list with one count. "Run History" and "Agent Run
  Sequence" no longer disagree.
- **Workflow run page.** When no Workflow-level result is declared, the page
  says that and points to the step outputs. It does not say "No result was
  produced". Step output gets an Output label.
- **Data.** If the Issue's result needs data that the Issue endpoints do not
  return, change the owning service and API. Do not assemble it from several
  client requests. The page-system record requires this for UI correctness
  that depends on run contracts.

## Out Of Scope

- Naming runs and removing raw IDs, which is [task 42](42-portal-work-naming.md).
- Failure presentation, which is [task 32](32-portal-failure-explanation.md).

## Acceptance Criteria

- After the reproduction above, every Issue surface agrees: the Overview, the
  Results tab, and the Runs tab all show a succeeded run and what it produced.
  This holds in English and zh-CN, with distinct labels.
- After a published Workflow run, the run page shows its outcome and each
  step's labelled output, with no "No result was produced" when a step produced
  output.
- A browser test asserts the agreement for an Agent run and a Workflow run.

## Verification

1. `./make test`, if an API changes.
2. `./make check portal`, then `./make e2e visual`. The detail template baseline
   is likely to change on purpose.
3. Use the kind loop with the mock model, then `./make e2e kind`.

## Notes

Task 16 also edits `IssueDetail.tsx` to adopt `DetailHeader` and `StatusLabel`.
If both tasks are in flight, coordinate rather than merging conflicting
layouts.

Audit context: the phase 0 Portal audit ran on 2026-10-06 against `main` at
`e98efc7a`, on an ephemeral kind cluster with the mock model. The operator was
an Agent with repository knowledge. The full report is kept in history at
[`718a6ab3`](https://github.com/icloudbb/buildmax/blob/718a6ab3969d35bdba7f66013d8ccfefe7549af8/docs/contribute/exploratory-runs/2026-10-06-portal-ui-journey-audit.md).
