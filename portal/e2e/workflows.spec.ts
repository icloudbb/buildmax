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
      nodes: [{ id: "only", type: "agent_task", agent: { id: agent.id }, input: { instruction: "Reply with exactly: deployment smoke ok" } }],
    }),
  })
  reportLeftovers(current.spaceId, [`agent ${agent.id}`, `workflow ${workflow.id}`])

  await page.goto(`/#/spaces/${current.spaceId}/workflows`)
  await expect(page.getByRole("heading", { name: "Workflows", exact: true })).toBeVisible()
  await expect(page.getByRole("button", { name: "New Workflow" })).toHaveCount(1)
  const list = page.getByRole("region", { name: "Workflow list" })
  await expect(list.getByText(name, { exact: true })).toBeVisible()

  // The detail route carries the id, so linking to it is the same claim as
  // reaching it by clicking — and it is the one an operator pastes to a
  // colleague.
  await page.goto(`/#/spaces/${current.spaceId}/workflows/${workflow.id}`)
  // The detail page now titles itself with the workflow's own name, so the
  // heading is the name rather than a fixed "Workflow Detail" label. The page is
  // organized into tabs like the agent detail page; a freshly created workflow is
  // a draft, so it opens on its Definition tab.
  await expect(page.getByRole("heading", { name, level: 1 })).toBeVisible()
  // The breadcrumb is the reader's orientation cue, so once the workflow has
  // loaded it names the workflow rather than its opaque id.
  await expect(page.getByLabel("Breadcrumb").getByText(name, { exact: true })).toBeVisible()
  await expect(
    page.locator(".detail-tabs").getByRole("tab", { name: "Definition" }),
  ).toHaveAttribute("aria-selected", "true")
  // The opaque public id is not shown on the detail page: the name and
  // breadcrumb orient the reader, and history is a tab of its own.
  await expect(page.getByText(workflow.id, { exact: true })).toHaveCount(0)
  await expect(page.getByText("Workflow not found.")).toHaveCount(0)
})

test("the detail page organizes into tabs, and lifecycle actions live on the Definition tab", async ({
  page,
}) => {
  const current = await session(page)

  const agent = await postJSON<{ id: string }>(page, `${current.space}/agents`, current, {
    name: tagged("Workflow lifecycle agent"),
    description: "Created by the Portal browser tests.",
    instructions: "Reply with exactly: deployment smoke ok",
  })
  const name = tagged("Workflow lifecycle probe")
  const workflow = await postJSON<{ id: string }>(page, `${current.space}/workflows`, current, {
    name,
    description: "Created by the Portal browser tests to exercise the tabbed detail layout.",
    definition: JSON.stringify({
      schema_version: 1,
      nodes: [{ id: "only", type: "agent_task", agent: { id: agent.id }, input: { instruction: "Reply with exactly: deployment smoke ok" } }],
    }),
  })
  reportLeftovers(current.spaceId, [`agent ${agent.id}`, `workflow ${workflow.id}`])

  const tabs = page.locator(".detail-tabs")
  await page.goto(`/#/spaces/${current.spaceId}/workflows/${workflow.id}`)

  // A draft opens on its Definition tab. The lifecycle actions live in that panel
  // and name the state they reach — Publish (primary) and Save as draft — with no
  // status control. Run Workflow is in the header but disabled, because the
  // runtime refuses to run an unpublished definition.
  await expect(tabs.getByRole("tab", { name: "Definition" })).toHaveAttribute("aria-selected", "true")
  await expect(page.getByRole("combobox", { name: "Status" })).toHaveCount(0)
  await expect(page.getByRole("button", { name: "Publish" })).toHaveClass(/bm-button--primary/)
  await expect(page.getByRole("button", { name: "Save as draft" })).toHaveClass(/bm-button--secondary/)
  await expect(page.getByRole("button", { name: "Run Workflow" })).toBeDisabled()

  // Publishing makes the workflow runnable: the status pill reads Published and
  // Run Workflow enables. The tab stays put — publishing is a save, not a mode.
  await page.getByRole("button", { name: "Publish" }).click()
  await expect(page.getByText("Published", { exact: true }).first()).toBeVisible()
  await expect(page.getByRole("button", { name: "Run Workflow" })).toBeEnabled()

  // The Overview tab presents the read-only plan; the Runs tab lists executions.
  await tabs.getByRole("tab", { name: "Overview" }).click()
  await expect(page.getByRole("heading", { name: "Plan", exact: true })).toBeVisible()
  await tabs.getByRole("tab", { name: "Runs" }).click()
  await expect(page.getByRole("heading", { name: "Recent Runs" })).toBeVisible()

  // Version history is its own tab. Each revision row summarizes as
  // "<name> · <status>"; publishing created a second revision, listed here.
  await tabs.getByRole("tab", { name: "Revisions" }).click()
  await expect(page.getByText(new RegExp(`${name} · Published`)).first()).toBeVisible()

  // Archive is an explicit lifecycle action on the Definition tab. It takes the
  // workflow out of the runnable state; the action is then absent because the
  // workflow is already archived.
  await tabs.getByRole("tab", { name: "Definition" }).click()
  await page.getByRole("button", { name: "Archive" }).click()
  await expect(page.getByText("Archived", { exact: true }).first()).toBeVisible()
  await expect(page.getByRole("button", { name: "Archive" })).toHaveCount(0)
  // Saving as draft brings an archived workflow back into the draft lifecycle.
  await page.getByRole("button", { name: "Save as draft" }).click()
  await expect(page.getByText("Draft", { exact: true }).first()).toBeVisible()
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
      nodes: [
        {
          id: "only",
          type: "agent_task",
          agent: { id: agent.id },
          input: { instruction: "Reply with exactly: deployment smoke ok" },
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
  await expect(page.getByRole("heading", { level: 1 })).toContainText("Workflow run probe")
  await expect(page.getByText("Workflow run not found.")).toHaveCount(0)

  // What the run view is for: which step ran, how it ended, and what it
  // produced. The API smoke never renders any of it, and a handler test cannot
  // reach a step whose output came back from a real worker.
  const steps = page.locator(".issues-page__panel").filter({
    has: page.getByRole("heading", { name: "Steps" }),
  })
  const step = steps.locator(".workflow-page__step").first()
  await expect(step.getByText("only", { exact: true })).toBeVisible()
  await expect(step.locator(".issues-page__status")).toHaveText("Succeeded")
  // The direct child, not any descendant: the agent's instructions are drawn in
  // the same kind of block inside a disclosure, and they say what was asked
  // rather than what came back. Matching both would pass on a step that
  // produced nothing.
  await expect(step.locator(".workflow-page__step-body > .workflow-page__step-output")).toContainText(
    "deployment smoke ok"
  )
})

test("a workflow with an input_schema runs from its generated input form", async ({ page }) => {
  test.setTimeout(RUN_TIMEOUT_MS + 60_000)

  const current = await session(page)

  const agent = await postJSON<{ id: string }>(page, `${current.space}/agents`, current, {
    name: tagged("Workflow input agent"),
    description: "Created by the Portal browser tests.",
    instructions: "Reply with exactly: deployment smoke ok",
  })
  const workflow = await postJSON<{ id: string }>(page, `${current.space}/workflows`, current, {
    name: tagged("Workflow input probe"),
    description: "Created by the Portal browser tests to exercise run input.",
    definition: JSON.stringify({
      schema_version: 1,
      input_schema: {
        type: "object",
        additionalProperties: false,
        properties: { topic: { type: "string" } },
        required: ["topic"],
      },
      nodes: [
        { id: "only", type: "agent_task", agent: { id: agent.id }, input: { instruction: "Reply with exactly: deployment smoke ok" } },
      ],
    }),
  })
  await patchJSON(page, `${current.space}/workflows/${encodeURIComponent(workflow.id)}`, current, {
    status: "published",
  })
  reportLeftovers(current.spaceId, [`agent ${agent.id}`, `workflow ${workflow.id}`])

  await page.goto(`/#/spaces/${current.spaceId}/workflows/${workflow.id}`)
  // A published workflow opens in its operating layout, whose Plan panel leads
  // the body in place of the editing form.
  await expect(page.getByRole("heading", { name: "Plan", exact: true })).toBeVisible()
  // Run opens a drawer that carries the input form generated from input_schema;
  // the required field is present there and the run cannot start until it is
  // filled.
  await page.getByRole("button", { name: "Run Workflow" }).click()
  const runDialog = page.getByRole("dialog", { name: "Run Workflow" })
  await expect(runDialog).toBeVisible()
  const input = runDialog.locator(".workflow-run-input input[type='text']")
  await expect(input).toBeVisible()
  await input.fill("markets")
  await runDialog.getByRole("button", { name: "Start run" }).click()

  // The run view is reached after the POST, and the run's stored input is the
  // proof the generated form's value crossed the boundary and was admitted.
  await expect(page.getByRole("heading", { level: 1 })).toContainText("Workflow input probe")
  await expect
    .poll(
      async () => {
        const runs = await page.request.get(`${current.space}/workflows/${encodeURIComponent(workflow.id)}/runs`, {
          headers: { Authorization: `Bearer ${current.token}` },
        })
        if (!runs.ok()) return `HTTP ${runs.status()}`
        const body = (await runs.json()) as { runs: { id: string; status: string; input?: { topic?: string } }[] }
        const run = body.runs[0]
        if (!run) return "no run"
        if (run.status === "failed") return "failed"
        return `${run.status}:${run.input?.topic ?? ""}`
      },
      { timeout: RUN_TIMEOUT_MS, intervals: [2000] }
    )
    .toBe("succeeded:markets")
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
      nodes: [
        { id: "collect", type: "agent_task", agent: { id: agent.id }, input: { instruction: "Reply with exactly: deployment smoke ok" } },
        {
          id: "summarize",
          type: "agent_task",
          needs: ["collect"],
          agent: { id: agent.id },
          input: {
            instruction: "Summarize the research below.",
            bindings: [{ name: "research", source: "node.collect.output", pointer: "/text" }],
          },
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
  await expect(stepStatuses).toHaveText(["Succeeded", "Succeeded"])
})
