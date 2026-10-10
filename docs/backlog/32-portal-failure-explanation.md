---
id: portal-failure-explanation
title: Explain a failed run in the person's terms and lead with the fix, not a retry that cannot succeed
roadmap: R6
source: docs/design/ui-experience-program.md#phases
depends_on: []
verification: ["./make test", "./make check portal", "./make e2e visual", "./make e2e kind"]
claim:
pr:
---

## Outcome

When a run fails because of something the person can fix, Portal names the
cause in their terms (which Secret, which Agent) and makes the fix the primary
action. It warns before starting a run that is known to fail. Agent success
rates stop counting stopped runs as failures. This task carries Portal audit
findings P2 (Major) and P6.

## Findings

**P2 (Major, error recovery). A failed run shows only a raw internal error
and offers a retry that cannot succeed.**

- Screen: the Task page, Issue Discussion, Task details, and Run details.
- Reproduction, as `alice@buildmax.local` in the fixture Space "BuildMax QA":
  1. Set the Executor of the Issue "Unassigned backlog item" to
     **QA Blocked Agent**. That Agent's Secret grant is disabled.
  2. Choose **Run agent**.
- Actual:
  - The run fails in about 0 s.
  - The transcript shows an italic message in the Agent's bubble:
    `secret grant unavailable: worker API GET /api/worker/task-runs/<id>/secrets: secret is disabled (409)`.
  - The Issue's Latest Outcome (now **Latest run**) says only "Failed".
  - **Details** (Task details) lists the Agent, the status, and a 0 s
    duration, but no cause.
  - **Run details** says "no trace was recorded for this run", and the
    browser logs a 404.
  - The most prominent actions are **Retry last run** and **Retry Run**, which
    fail the same way.
  - Nothing links to the Agent, although the Agent's page already says
    "⚠ 1 secret grant no longer resolve. Fix in config".
  - **Run agent** started the run without warning about that known
    configuration problem.
- Expected: the cause in the person's terms, and the fix as the primary action.

**P6 (Minor). The Agent success rate counts canceled runs as failures.**

- Screen: the Agents list KPI and Agent Detail.
- Code: `portal/src/pages/agents/AgentList.tsx` uses `taskRunFailed` from
  `features/conversations/thread.ts`, which treats `CANCELED` as failed.
  `AgentDetail.tsx` computes its rate the same way.
- Observed: QA Writer has 18 runs (14 Done, 2 Stopped, 2 "Needs your answer")
  and shows **88%**. The two stopped runs count against it.
- An Agent whose only finished runs were canceled therefore shows 0%. The
  audit confirmed this from the code and the arithmetic. It did not observe
  it on screen, because the mock finished each run before **Stop** could be
  reached.

## Verified Facts

These were checked on `main` at `e36fa722`:

- `internal/core/task/task.go` already classifies run failures
  (`FailureSpaceConfiguration`, `FailureModel`, `FailureDispatch`, and others).
  Space-scoped Task and TaskRun responses do not expose the class. Only
  Administration's aggregates and Workflow runs carry a `failure_class`.
- The Agent pages already compute the unresolved Secret grant warning.

## Scope

- Expose the run's failure class on the Space-scoped TaskRun response, plus a
  structured cause where the server knows it, such as the Secret and the
  Agent. Update `openapi.json` and the handler tests.
- Map each class to an explanation and a primary action. A Space
  configuration failure leads with "Open the Agent to fix its Secret grant",
  and retry becomes secondary. The raw server text stays available under
  details, as the program's
  [localization decision](../design/ui-experience-program.md#d5-localization-through-a-shared-typed-catalog)
  allows.
- Before **Run agent** starts a run on an Agent with an unresolved grant, show
  the same warning the Agent page shows.
- When a run has no trace, Run details says so without making a request that
  fails.
- Leave stopped runs out of the success rate. A rate with no succeeded or
  failed runs shows "—". `taskRunFailed` keeps its meaning for chat card tone.

## Out Of Scope

- Administration's wording for the same failure, which is
  [task 44](44-portal-administration-labels.md).
- Changing whether a disabled Secret should fail the run.

## Acceptance Criteria

- Reproducing P2 shows "This Agent's Secret grant is disabled", or equivalent
  wording that names the Secret and the Agent. The fix is the primary action,
  and retry is not primary.
- **Run agent** on the blocked Agent warns before it starts the run.
- Opening Run details for that run logs no 404.
- An Agent whose finished runs were all canceled shows "—", not 0%. QA Writer's
  rate excludes its two stopped runs. Unit tests cover the rate rule.

## Verification

1. `./make test` for the response and the handler.
2. `./make check portal`, then `./make e2e visual`.
3. Use the kind loop with `./make kind fixtures --runs` (it seeds "QA Blocked
   Agent" and "Unassigned backlog item"), then `./make e2e kind`.

## Notes

Audit context: the phase 0 Portal audit ran on 2026-10-06 against `main` at
`e98efc7a`, on an ephemeral kind cluster with the mock model. The operator was
an Agent with repository knowledge. The full report is kept in history at
[`718a6ab3`](https://github.com/icloudbb/buildmax/blob/718a6ab3969d35bdba7f66013d8ccfefe7549af8/docs/contribute/exploratory-runs/2026-10-06-portal-ui-journey-audit.md).
