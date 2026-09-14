import { expect, test } from "@playwright/test"

import { patchJSON, postJSON, reportLeftovers, session, tagged } from "./fixtures"

/**
 * A workflow is space-scoped and reusable, so the list and the detail view are
 * where an operator meets one. Both are browser-only: the API smoke never
 * renders a workflow, and the handler tests never route to it.
 *
 * Executing one is browser-only for a stronger reason. The step dispatches a
 * task to a real worker, and the run view is the only place that says which
 * step ran, how it ended, and what it produced. A handler test can assert the
 * rows a run wrote; it cannot assert that a worker filled them.
 */

test("a workflow is listed, and its detail view opens by URL", async ({ page }) => {
  const current = await session(page)

  const agent = await postJSON<{ id: string }>(page, `${current.space}/agents`, current, {
    name: tagged("Workflow probe agent"),
    description: "Created by the Portal browser tests.",
    instructions: "Reply with exactly: deployment smoke ok",
  })
  const name = tagged("Workflow probe")
  const workflow = await postJSON<{ id: string }>(page, `${current.space}/workflows`, current, {
    name,
    description: "Created by the Portal browser tests to exercise the workflow views.",
    definition: JSON.stringify({
      schema_version: 1,
      steps: [{ step_id: "only", type: "agent_task", target_agent_id: agent.id, prompt: "Reply with exactly: deployment smoke ok" }],
    }),
  })
  reportLeftovers(current.spaceId, [`agent ${agent.id}`, `workflow ${workflow.id}`])

  await page.goto(`/#/spaces/${current.spaceId}/workflows`)
  await expect(page.getByRole("heading", { name: "Workflows", exact: true })).toBeVisible()
  const list = page.locator(".issues-page__panel").filter({
    has: page.getByRole("heading", { name: "All Workflows" }),
  })
  await expect(list.getByText(name, { exact: true })).toBeVisible()

  // The detail route carries the id, so linking to it is the same claim as
  // reaching it by clicking — and it is the one an operator pastes to a
  // colleague.
  await page.goto(`/#/spaces/${current.spaceId}/workflows/${workflow.id}`)
  await expect(page.getByRole("heading", { name: "Workflow Detail" })).toBeVisible()
  // The breadcrumb is the reader's orientation cue, so once the workflow has
  // loaded it names the workflow rather than its opaque id. The Definition
  // panel is where the id itself belongs, as secondary metadata.
  await expect(page.getByLabel("Breadcrumb").getByText(name, { exact: true })).toBeVisible()
  const definition = page.locator(".issues-page__panel").filter({
    has: page.getByRole("heading", { name: "Definition" }),
  })
  await expect(definition.getByText(workflow.id, { exact: true })).toBeVisible()
  await expect(page.getByText("Workflow not found.")).toHaveCount(0)
})

// The worker has to start, run the step's task, and record its output. The API
// smoke allows two minutes for the same shape of work; this adds the polling
// that follows it.
const RUN_TIMEOUT_MS = 150_000

test("a workflow runs, and the run view reports each step's outcome", async ({ page }) => {
  test.setTimeout(RUN_TIMEOUT_MS + 60_000)

  const current = await session(page)

  const agent = await postJSON<{ id: string }>(page, `${current.space}/agents`, current, {
    name: tagged("Workflow run agent"),
    description: "Created by the Portal browser tests.",
    instructions: "Reply with exactly: deployment smoke ok",
  })
  const workflow = await postJSON<{ id: string }>(page, `${current.space}/workflows`, current, {
    name: tagged("Workflow run probe"),
    description: "Created by the Portal browser tests to exercise workflow execution.",
    definition: JSON.stringify({
      schema_version: 1,
      steps: [
        {
          step_id: "only",
          type: "agent_task",
          target_agent_id: agent.id,
          prompt: "Reply with exactly: deployment smoke ok",
        },
      ],
    }),
  })
  // A workflow is created as a draft and a draft is refused a run, so this is a
  // precondition of the next call rather than a separate assertion.
  await patchJSON(page, `${current.space}/workflows/${encodeURIComponent(workflow.id)}`, current, {
    status: "published",
  })
  const started = await postJSON<{ run: { id: string } }>(
    page,
    `${current.space}/workflows/${encodeURIComponent(workflow.id)}/runs`,
    current,
    {}
  )
  const runId = started.run.id
  reportLeftovers(current.spaceId, [`agent ${agent.id}`, `workflow ${workflow.id}`, `workflow run ${runId}`])

  // Poll the run rather than the view. A run that never finishes should fail
  // here, naming the status it stuck in, instead of as a missing string on a
  // screenshot two minutes later.
  await expect
    .poll(
      async () => {
        const res = await page.request.get(`${current.space}/workflow-runs/${encodeURIComponent(runId)}`, {
          headers: { Authorization: `Bearer ${current.token}` },
        })
        if (!res.ok()) return `HTTP ${res.status()}`
        const body = (await res.json()) as { run: { status: string; error_message?: string | null } }
        return body.run.status === "failed" ? `failed: ${body.run.error_message ?? "no message"}` : body.run.status
      },
      { timeout: RUN_TIMEOUT_MS, intervals: [2000] }
    )
    .toBe("succeeded")

  await page.goto(`/#/spaces/${current.spaceId}/workflow-runs/${runId}`)
  // exact: the panel below carries the workflow's own name, and this run's
  // workflow is called "Workflow run probe …", which a substring match finds too.
  await expect(page.getByRole("heading", { name: "Workflow Run", exact: true })).toBeVisible()
  await expect(page.getByText("Workflow run not found.")).toHaveCount(0)

  // What the run view is for: which step ran, how it ended, and what it
  // produced. The API smoke never renders any of it, and a handler test cannot
  // reach a step whose output came back from a real worker.
  const steps = page.locator(".issues-page__panel").filter({
    has: page.getByRole("heading", { name: "Steps" }),
  })
  const step = steps.locator(".workflow-page__step").first()
  await expect(step.getByText("only", { exact: true })).toBeVisible()
  await expect(step.locator(".issues-page__status")).toHaveText("succeeded")
  // The direct child, not any descendant: the agent's instructions are drawn in
  // the same kind of block inside a disclosure, and they say what was asked
  // rather than what came back. Matching both would pass on a step that
  // produced nothing.
  await expect(step.locator(".workflow-page__step-body > .workflow-page__step-output")).toContainText(
    "deployment smoke ok"
  )
})

test("a workflow binds one step's output into the next step's input", async ({ page }) => {
  test.setTimeout(RUN_TIMEOUT_MS + 60_000)

  const current = await session(page)

  const agent = await postJSON<{ id: string }>(page, `${current.space}/agents`, current, {
    name: tagged("Workflow binding agent"),
    description: "Created by the Portal browser tests.",
    instructions: "Reply with exactly: deployment smoke ok",
  })
  // The second step binds the first step's output into its input. Its dispatch
  // reads the first step's finished output and fails if the binding cannot
  // resolve, so a run that reaches "succeeded" is proof the whole binding path
  // ran end to end -- validation, the run's binding snapshot, and the resolve at
  // dispatch. Both steps target one agent to keep the model answer deterministic.
  const workflow = await postJSON<{ id: string }>(page, `${current.space}/workflows`, current, {
    name: tagged("Workflow binding probe"),
    description: "Created by the Portal browser tests to exercise step output binding.",
    definition: JSON.stringify({
      schema_version: 1,
      steps: [
        { step_id: "collect", type: "agent_task", target_agent_id: agent.id, prompt: "Reply with exactly: deployment smoke ok" },
        {
          step_id: "summarize",
          type: "agent_task",
          target_agent_id: agent.id,
          prompt: "Summarize the research below.",
          bindings: [{ name: "research", from_step: "collect" }],
        },
      ],
    }),
  })
  await patchJSON(page, `${current.space}/workflows/${encodeURIComponent(workflow.id)}`, current, {
    status: "published",
  })
  const started = await postJSON<{ run: { id: string } }>(
    page,
    `${current.space}/workflows/${encodeURIComponent(workflow.id)}/runs`,
    current,
    {}
  )
  const runId = started.run.id
  reportLeftovers(current.spaceId, [`agent ${agent.id}`, `workflow ${workflow.id}`, `workflow run ${runId}`])

  await expect
    .poll(
      async () => {
        const res = await page.request.get(`${current.space}/workflow-runs/${encodeURIComponent(runId)}`, {
          headers: { Authorization: `Bearer ${current.token}` },
        })
        if (!res.ok()) return `HTTP ${res.status()}`
        const body = (await res.json()) as { run: { status: string; error_message?: string | null } }
        return body.run.status === "failed" ? `failed: ${body.run.error_message ?? "no message"}` : body.run.status
      },
      { timeout: RUN_TIMEOUT_MS, intervals: [2000] }
    )
    .toBe("succeeded")

  await page.goto(`/#/spaces/${current.spaceId}/workflow-runs/${runId}`)
  const steps = page.locator(".issues-page__panel").filter({
    has: page.getByRole("heading", { name: "Steps" }),
  })
  const stepStatuses = steps.locator(".workflow-page__step .issues-page__status")
  await expect(stepStatuses).toHaveText(["succeeded", "succeeded"])
})
