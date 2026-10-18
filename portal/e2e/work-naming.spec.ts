import { expect, test, type Page } from "@playwright/test"

import { getJSON, patchJSON, postJSON, reportLeftovers, session, tagged, type Session } from "./fixtures"

/**
 * A run is named by the work it serves (docs/design/portal-work-and-execution-experience.md,
 * "Execution provenance"). The mock model titles every Task "deployment smoke
 * ok", so a run started from an Issue that still reads as its Issue — on the
 * Task page, in the breadcrumb after a reload, and in the Agent's run lists —
 * proves the label comes from the origin the server read with the Task, not
 * from the generated title or from where the reader came from.
 */

const RUN_TIMEOUT_MS = 150_000
const GENERATED_TITLE = "deployment smoke ok"

interface TaskRead {
  status: string
  error_message?: string | null
  origin?: { kind: string; id: string; name?: string }
}

async function waitForStatus(page: Page, current: Session, taskId: string, want: string): Promise<TaskRead> {
  let last: TaskRead | null = null
  await expect
    .poll(
      async () => {
        last = await getJSON<TaskRead>(page, `${current.space}/tasks/${encodeURIComponent(taskId)}`, current)
        return last.status
      },
      { message: `task ${taskId} did not reach ${want}`, timeout: RUN_TIMEOUT_MS, intervals: [1000] },
    )
    .toBe(want)
  return last!
}

test("a run started from an Issue is headed, listed, and trailed by the Issue's title", async ({ page }) => {
  test.setTimeout(RUN_TIMEOUT_MS + 90_000)
  const current = await session(page)
  const agentName = tagged("Work naming agent")
  const agent = await postJSON<{ id: string }>(page, `${current.space}/agents`, current, {
    name: agentName,
    description: "Created by the Portal browser tests.",
    instructions: `Reply with exactly: ${GENERATED_TITLE}`,
  })
  const issueTitle = tagged("Work naming issue")
  const issue = await postJSON<{ id: string; version: number }>(page, `${current.space}/issues`, current, {
    title: issueTitle,
    description: "## Acceptance criteria\n\n- [ ] The run reads as this Issue",
  })
  reportLeftovers(current.spaceId, [`agent ${agent.id}`, `issue ${issue.id}`])
  await patchJSON(page, `${current.space}/issues/${encodeURIComponent(issue.id)}`, current, {
    version: issue.version,
    executor_kind: "agent",
    executor_id: agent.id,
  })
  const task = await postJSON<{ id: string }>(page, `${current.space}/issues/${encodeURIComponent(issue.id)}/agent-runs`, current, {})
  reportLeftovers(current.spaceId, [`task ${task.id}`])

  // The server names the origin on the Task itself.
  const settled = await waitForStatus(page, current, task.id, "SUCCEEDED")
  expect(settled.origin).toEqual({ kind: "issue", id: issue.id, name: issueTitle })

  // Reached straight by its address, with nothing learned from the Issue page.
  await page.goto(`/#/spaces/${current.spaceId}/tasks/${encodeURIComponent(task.id)}`)
  const crumbs = page.getByLabel("Breadcrumb")
  for (const pass of ["first load", "reload"]) {
    await expect(page.getByRole("heading", { level: 1, name: issueTitle }), pass).toBeVisible()
    await expect(crumbs.getByRole("link", { name: issueTitle }), pass).toHaveAttribute(
      "href",
      `#/spaces/${current.spaceId}/issues/${issue.id}`,
    )
    await expect(page.locator(".task-thread__header .bm-detail-header__meta"), pass).toContainText(agentName)
    if (pass === "first load") await page.reload()
  }
  await expect(page.getByRole("heading", { level: 1, name: GENERATED_TITLE })).toHaveCount(0)

  // The details say where the run came from by name; its ID is secondary and copyable.
  await page.getByRole("button", { name: "Details" }).click()
  const details = page.getByRole("dialog", { name: "Task details" })
  await expect(details.getByRole("link", { name: issueTitle })).toBeVisible()
  await expect(details.getByText(task.id, { exact: true })).toBeVisible()
  await expect(details.getByRole("button", { name: "Copy Task ID" })).toBeVisible()
  await page.keyboard.press("Escape")

  // The Agent's run table and the Agents page's recent activity use the same label.
  await page.goto(`/#/spaces/${current.spaceId}/agents/${encodeURIComponent(agent.id)}`)
  const runRow = page.locator("table.agent-runs tbody tr").first()
  await expect(runRow.locator(".agent-runs__title")).toContainText(issueTitle)
  await page.goto(`/#/spaces/${current.spaceId}/agents`)
  const activity = page.locator(".agent-activity__row").filter({ hasText: agentName }).first()
  await expect(activity.locator(".agent-activity__row-title")).toHaveText(issueTitle)

  // The Issue list shows the description's words, not its Markdown. The
  // executor filter keeps this Issue on the first page of a busy Space.
  await page.goto(`/#/spaces/${current.spaceId}/issues?executor=${encodeURIComponent(`agent:${agent.id}`)}`)
  const row = page.locator(".issues-page__row").filter({ hasText: issueTitle })
  await expect(row.locator(".issues-page__row-desc")).toHaveText("Acceptance criteria The run reads as this Issue")
})
