---
id: portal-issue-executor-path
title: Lead a new person from an Issue to something that can run it, and keep them on the Issue when it runs
roadmap: R6
source: docs/design/ui-experience-program.md#phases
depends_on: []
verification: ["./make check portal", "./make e2e visual", "./make e2e local"]
claim: gougoujiang 2026-10-10
pr:
---

## Outcome

A person who creates their first Issue learns from the Issue itself that an
Agent or a published Workflow runs it, and reaches one in a step. Starting a
run keeps them on the Issue, where they can see that work started. This task
carries Portal audit findings P3 (Major), P9, and P15.

## Findings

**P3 (Major, discoverability). A new person has no path from an Issue to
something that can run it.**

- Screen: the New Issue dialog and Issue Detail.
- Reproduction:
  1. Sign in as a new account (`nora@buildmax.local`, only a personal Space)
     and open **Issues**.
  2. Choose **New Issue**.
- Actual:
  - The Executor select offers only "None".
  - Its hint reads "What runs the work. Only published workflows are
    available.", which does not mention Agents. The hint stays the same after
    an Agent exists and is listed.
  - With Executor None, the Issue page shows no run action and no hint.
  - Nora ran the Issue only by leaving it, finding **Agents**, creating an
    Agent, and returning to **Edit issue**.
- Expected: say that an Agent or a published Workflow is needed, and link to
  creating one.

**P9 (Minor). Running an Issue leaves the Issue.**

- Reproduction: set an Agent executor on an Issue and choose **Run agent**.
- Actual:
  - **Run agent** navigates to the Task page.
  - The Issue status stays "To do" after a successful run.
  - The save message says "use Run to schedule one", but the button is
    labelled **Run agent**, and "schedule" collides with the Schedules
    feature.

**P15 (Minor). The Agent dialog's copy contradicts Agent scope.**

- Screen: the New Agent dialog on **Agents**.
- Actual: the dialog says "Agents are personas or task templates you can use
  across your account", while the page says "space agents" and the route is
  Space-scoped.

## Scope

- The Executor hint names both kinds of executor in both locales. When the
  Space has no Agent and no published Workflow, it links to creating an Agent.
- With no executor, Issue Detail shows Run disabled, with the specific reason
  and a way to set an executor. This follows the
  [work experience record](../design/portal-work-and-execution-experience.md#issue-experience):
  "Run is disabled with a specific reason until an executor and all required
  inputs are valid."
- **Run agent** keeps the person on the Issue. The Overview shows the run's
  state, with a link to the Task. The same record requires that a successful
  run "links directly to the new Task or run".
- Rewrite the save message without "schedule", and use the run button's label.
- Fix the Agent dialog's scope copy.

## Out Of Scope

- Changing an Issue's status automatically when a run starts. Issue status is
  set by people today, so that would be a product decision. If the
  implementation shows it is needed, return it to the maintainer.
- The Issue's result presentation, which is
  [task 28](28-portal-issue-result.md).

## Acceptance Criteria

- In a fresh personal Space, a person goes from **New Issue** to a started run
  without leaving the Issue except to create the Agent, and the path says what
  to do at each step.
- After **Run agent**, the URL is still the Issue's, and the Overview shows the
  run in progress with a link to its Task.
- No Issue or Agent copy mentions "schedule" for a manual run, or says that
  Agents are account-wide.
- English and zh-CN catalogs carry every new string.

## Verification

1. `./make check portal`.
2. `./make e2e visual`. Update the baselines if the Issue templates change on
   purpose.
3. `./make e2e local`, with a browser test for the new-person path: new Space,
   New Issue, create Agent, Run.

## Notes

Audit context: the phase 0 Portal audit ran on 2026-10-06 against `main` at
`e98efc7a`, on an ephemeral kind cluster with the mock model and
`./make kind fixtures --runs`. The operator was an Agent with repository
knowledge. The full report is kept in history at
[`718a6ab3`](https://github.com/icloudbb/buildmax/blob/718a6ab3969d35bdba7f66013d8ccfefe7549af8/docs/contribute/exploratory-runs/2026-10-06-portal-ui-journey-audit.md).
