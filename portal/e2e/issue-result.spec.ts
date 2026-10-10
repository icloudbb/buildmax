import { expect, test, type Page } from "@playwright/test"

import { getJSON, patchJSON, postJSON, reportLeftovers, session, tagged, type Session } from "./fixtures"

/**
 * After a run finishes, an Issue answers "did it work and what did it
 * produce" once, and its Overview, Results, and Runs agree with that answer; a
 * Workflow run page says the same and labels each step's output as output.
 * Only a real render with a real worker proves this: the disagreement the
 * audit found was between surfaces that each read the API correctly.
 */

// The worker has to start, run the Task, and record its answer; the Workflow
// specs allow the same.
const RUN_TIMEOUT_MS = 150_000
const REPLY = "deployment smoke ok"

interface FlowRun {
  kind: "agent" | "workflow"
  task?: { id: string; status: string; error_message?: string | null }
  run?: { id: string; status: string; error_message?: string | null }
}

/** Wait until the Issue's latest run is the expected kind and has succeeded. */
async function waitForLatestRun(page: Page, current: Session, issueId: string, kind: FlowRun["kind"]): Promise<string> {
  let id = ""
  await expect
    .poll(
      async () => {
        const flow = await getJSON<{ runs: FlowRun[] }>(page, `${current.space}/issues/${issueId}/flow`, current)
        const latest = flow.runs[0]
        if (!latest) return "no runs"
        if (latest.kind !== kind) return `latest is a ${latest.kind} run`
        const item = latest.task ?? latest.run
        id = item?.id ?? ""
        const status = (item?.status ?? "").toLowerCase()
        return status === "failed" ? `failed: ${item?.error_message ?? "no message"}` : status
      },
      { timeout: RUN_TIMEOUT_MS, intervals: [2000] },
    )
    .toBe("succeeded")
  return id
}

async function createReplyingAgent(page: Page, current: Session, name: string): Promise<{ id: string }> {
  return postJSON<{ id: string }>(page, `${current.space}/agents`, current, {
    name: tagged(name),
    description: "Created by the Portal browser tests.",
    instructions: `Reply with exactly: ${REPLY}`,
  })
}

test("an Agent run's outcome agrees across Overview, Results, and Runs", async ({ page }) => {
  test.setTimeout(RUN_TIMEOUT_MS + 60_000)
  const current = await session(page)
  const agent = await createReplyingAgent(page, current, "Issue result agent")
  const title = tagged("Issue result agent probe")
  const issue = await postJSON<{ id: string }>(page, `${current.space}/issues`, current, {
    title,
    executor_kind: "agent",
    executor_id: agent.id,
  })
  reportLeftovers(current.spaceId, [`agent ${agent.id}`, `issue ${issue.id}`])

  await page.goto(`/#/spaces/${current.spaceId}/issues/${issue.id}`)
  await expect(page.getByRole("heading", { name: title, level: 1 })).toBeVisible()
  await page.getByRole("button", { name: "Run agent" }).click()
  await expect(page.getByRole("status").filter({ hasText: "Run started." })).toBeVisible()
  const taskId = await waitForLatestRun(page, current, issue.id, "agent")

  // The page follows the run while it is in flight; reloading proves the
  // finished state, not one the poll happened to catch.
  await page.reload()
  const latest = page.getByRole("region", { name: "Latest run" })
  await expect(latest.locator(".issues-page__status")).toHaveText("Succeeded")
  await expect(latest.getByRole("figure", { name: "Agent reply" })).toContainText(REPLY)
  await expect(latest.getByRole("link", { name: "Open Task" })).toHaveAttribute(
    "href",
    `#/spaces/${current.spaceId}/tasks/${taskId}`,
  )

  const tabs = page.getByRole("navigation", { name: "Issue sections" })
  await tabs.getByRole("button", { name: "Results" }).click()
  const results = page.locator("section").filter({ has: page.getByRole("heading", { name: "Results", exact: true }) })
  await expect(results.getByRole("heading", { name: "Text output" })).toBeVisible()
  await expect(results.getByText("This run produced text only.")).toBeVisible()
  await expect(results.getByRole("figure", { name: "Agent reply" })).toContainText(REPLY)
  await expect(results.getByText("Nothing has run on this Issue yet", { exact: false })).toHaveCount(0)

  await tabs.getByRole("button", { name: "Runs" }).click()
  const runs = page.locator("section").filter({ has: page.getByRole("heading", { name: "Runs", exact: true }) })
  await expect(runs.getByText("1 run", { exact: true })).toBeVisible()
  const rows = runs.locator(".workflow-page__run-row")
  await expect(rows).toHaveCount(1)
  await expect(rows.first()).toContainText("Agent run")
  await expect(rows.first().locator(".issues-page__status")).toHaveText("Succeeded")
})

test("a published Workflow run reports its outcome and labelled step output, and a newer Agent run becomes the latest", async ({
  page,
}) => {
  test.setTimeout(2 * RUN_TIMEOUT_MS + 60_000)
  const current = await session(page)
  const agent = await createReplyingAgent(page, current, "Issue result workflow agent")
  const workflowName = tagged("Issue result workflow")
  const workflow = await postJSON<{ id: string }>(page, `${current.space}/workflows`, current, {
    name: workflowName,
    description: "Created by the Portal browser tests to exercise an Issue's Workflow result.",
    definition: JSON.stringify({
      schema_version: 1,
      nodes: [{ id: "only", type: "agent_task", agent: { id: agent.id }, input: { instruction: `Reply with exactly: ${REPLY}` } }],
    }),
  })
  await patchJSON(page, `${current.space}/workflows/${encodeURIComponent(workflow.id)}`, current, { status: "published" })
  const issue = await postJSON<{ id: string }>(page, `${current.space}/issues`, current, {
    title: tagged("Issue result workflow probe"),
    executor_kind: "workflow",
    executor_id: workflow.id,
  })
  reportLeftovers(current.spaceId, [`agent ${agent.id}`, `workflow ${workflow.id}`, `issue ${issue.id}`])

  await postJSON(page, `${current.space}/issues/${issue.id}/workflow-runs`, current, {})
  const workflowRunId = await waitForLatestRun(page, current, issue.id, "workflow")

  // The run page: no declared result is not "nothing produced", and the step's
  // output is labelled as output, apart from the input it received.
  await page.goto(`/#/spaces/${current.spaceId}/workflow-runs/${workflowRunId}`)
  await expect(page.getByRole("heading", { level: 1 })).toContainText("Issue result workflow")
  await expect(
    page.getByText("This Workflow declares no overall result. Each step's output is shown under Steps below."),
  ).toBeVisible()
  await expect(page.getByText("No result was produced.")).toHaveCount(0)
  const step = page
    .locator(".issues-page__panel")
    .filter({ has: page.getByRole("heading", { name: "Steps" }) })
    .locator(".workflow-page__step")
    .first()
  await expect(step.locator(".issues-page__status")).toHaveText("Succeeded")
  await expect(step.getByRole("figure", { name: "Output" })).toContainText(REPLY)

  // The Issue answers with the same run and the same output.
  await page.goto(`/#/spaces/${current.spaceId}/issues/${issue.id}`)
  const latest = page.getByRole("region", { name: "Latest run" })
  await expect(latest).toContainText("Workflow run")
  await expect(latest.locator(".issues-page__status")).toHaveText("Succeeded")
  await expect(latest.getByRole("figure", { name: "Output of the last step, only" })).toContainText(REPLY)
  await expect(latest.getByRole("link", { name: "Open Run Detail" })).toHaveAttribute(
    "href",
    `#/spaces/${current.spaceId}/workflow-runs/${workflowRunId}`,
  )

  // A newer Agent run on the same Issue is the latest, whatever ran before it.
  // The run may have moved the Issue on, so the edit sends its current version.
  const { version } = await getJSON<{ version: number }>(page, `${current.space}/issues/${issue.id}`, current)
  await patchJSON(page, `${current.space}/issues/${issue.id}`, current, {
    version,
    executor_kind: "agent",
    executor_id: agent.id,
  })
  const created = await postJSON<{ id: string }>(page, `${current.space}/issues/${issue.id}/agent-runs`, current, {})
  reportLeftovers(current.spaceId, [`task ${created.id}`])
  const taskId = await waitForLatestRun(page, current, issue.id, "agent")
  expect(taskId).toBe(created.id)

  await page.reload()
  await expect(latest).toContainText("Agent run")
  await expect(latest.getByRole("figure", { name: "Agent reply" })).toContainText(REPLY)
  await expect(latest.getByRole("link", { name: "Open Task" })).toHaveAttribute(
    "href",
    `#/spaces/${current.spaceId}/tasks/${taskId}`,
  )
  await page.getByRole("navigation", { name: "Issue sections" }).getByRole("button", { name: "Runs" }).click()
  const runs = page.locator("section").filter({ has: page.getByRole("heading", { name: "Runs", exact: true }) })
  await expect(runs.getByText("2 runs", { exact: true })).toBeVisible()
  const rows = runs.locator(".workflow-page__run-row")
  await expect(rows).toHaveCount(2)
  await expect(rows.nth(0)).toContainText("Agent run")
  await expect(rows.nth(1)).toContainText("Workflow run")
})
