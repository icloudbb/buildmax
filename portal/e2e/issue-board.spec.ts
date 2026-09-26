import { expect, test, type Page } from "@playwright/test"

import { getJSON, patchJSON, postJSON, reportLeftovers, session, tagged, type Session } from "./fixtures"

/**
 * The Issue Board is a projection of the Issue collection: one lane per status,
 * each its own filtered query, and a move is the ordinary versioned PATCH. What
 * only a browser proves is that lanes stay truthful under failure, that the
 * named move action works and keeps focus, that a stale card is refused rather
 * than overwriting, and that the URL reproduces the projection.
 */

interface CreatedIssue {
  id: string
  version: number
  created_by: string
}

async function createIssue(page: Page, current: Session, title: string, body: Record<string, unknown> = {}) {
  const issue = await postJSON<CreatedIssue>(page, `${current.space}/issues`, current, {
    title,
    description: "Created by the Portal browser tests to exercise the Issue Board.",
    ...body,
  })
  reportLeftovers(current.spaceId, [`issue ${issue.id}`])
  return issue
}

function lane(page: Page, name: string) {
  return page.getByRole("region", { name: new RegExp(`^${name}\\b`) })
}

function card(page: Page, title: string) {
  return page.locator(".issue-board__card").filter({ has: page.getByRole("link", { name: title, exact: true }) })
}

test("a card moves between lanes through its named action, keeps focus, and never schedules a run", async ({ page }) => {
  const current = await session(page)
  const title = tagged("Board move probe")
  const issue = await createIssue(page, current, title)

  await page.goto(`/#/spaces/${current.spaceId}/issues?view=board`)
  await expect(page.getByRole("button", { name: "Board", exact: true })).toHaveAttribute("aria-pressed", "true")
  await expect(lane(page, "To do").getByRole("link", { name: title, exact: true })).toBeVisible()
  await expect(card(page, title).getByText("Unowned")).toBeVisible()
  await expect(card(page, title).getByText("No executor")).toBeVisible()

  await card(page, title).getByRole("button", { name: "Move to In progress" }).click()
  await expect(page.getByRole("status").filter({ hasText: `Moved “${title}” to In progress.` })).toBeVisible()
  const moved = lane(page, "In progress").getByRole("link", { name: title, exact: true })
  await expect(moved).toBeVisible()
  await expect(moved).toBeFocused()
  await expect(lane(page, "To do").getByRole("link", { name: title, exact: true })).toHaveCount(0)

  // The server holds the new status, and the move started nothing.
  const flow = await getJSON<{ issue: { status: string }; runs: unknown[] | null; agent_tasks: unknown[] | null }>(
    page,
    `${current.space}/issues/${issue.id}/flow`,
    current,
  )
  expect(flow.issue.status).toBe("in_progress")
  expect(flow.runs ?? []).toHaveLength(0)
  expect(flow.agent_tasks ?? []).toHaveLength(0)

  // The view is navigation state, so a reload reproduces it.
  await page.reload()
  await expect(lane(page, "In progress").getByRole("link", { name: title, exact: true })).toBeVisible()
})

test("a card whose Issue changed since the board loaded is refused, reloaded, and can then be moved", async ({ page }) => {
  const current = await session(page)
  const title = tagged("Board conflict probe")
  const issue = await createIssue(page, current, title)

  await page.goto(`/#/spaces/${current.spaceId}/issues?view=board`)
  await expect(lane(page, "To do").getByRole("link", { name: title, exact: true })).toBeVisible()

  // Someone else edits the Issue after the board read it.
  await patchJSON(page, `${current.space}/issues/${issue.id}`, current, {
    version: issue.version,
    description: "Edited elsewhere while the board was open.",
  })

  await card(page, title).getByRole("button", { name: "Move to Done" }).click()
  await expect(page.getByRole("alert").filter({ hasText: "changed since the board loaded it" })).toBeVisible()
  const stayed = lane(page, "To do").getByRole("link", { name: title, exact: true })
  await expect(stayed).toBeVisible()
  await expect(stayed).toBeFocused()
  await expect(lane(page, "Done").getByRole("link", { name: title, exact: true })).toHaveCount(0)

  // The reload carried the new version, so deciding again succeeds.
  await card(page, title).getByRole("button", { name: "Move to Done" }).click()
  await expect(lane(page, "Done").getByRole("link", { name: title, exact: true })).toBeVisible()
})

test("a lane that fails to load reads as failed, not empty, and recovers on retry", async ({ page }) => {
  const current = await session(page)
  const title = tagged("Board partial probe")
  await createIssue(page, current, title)

  let failDone = true
  await page.route(/\/issues\?.*status=done/, (route) =>
    failDone
      ? route.fulfill({ status: 500, contentType: "application/json", body: JSON.stringify({ error: "lane probe failure" }) })
      : route.continue(),
  )

  await page.goto(`/#/spaces/${current.spaceId}/issues?view=board`)
  await expect(lane(page, "To do").getByRole("link", { name: title, exact: true })).toBeVisible()
  await expect(page.getByText("Board incomplete")).toBeVisible()
  const done = lane(page, "Done")
  await expect(done.locator(".state-alert--error")).toBeVisible()
  await expect(done.getByText(/^No issues in/)).toHaveCount(0)

  failDone = false
  await done.getByRole("button", { name: "Retry" }).click()
  await expect(done.locator(".state-alert--error")).toHaveCount(0)
  await expect(page.getByText("Board incomplete")).toHaveCount(0)
})

test("List and Board apply the same filters, and the URL carries them", async ({ page }) => {
  const current = await session(page)
  const unowned = await createIssue(page, current, tagged("Board filter unowned probe"))
  const mineTitle = tagged("Board filter mine probe")
  await createIssue(page, current, mineTitle, { owner_id: unowned.created_by })
  const unownedTitle = tagged("Board filter unowned probe")

  await page.goto(`/#/spaces/${current.spaceId}/issues`)
  await page.getByLabel("Owner").selectOption("me")
  await expect(page).toHaveURL(/\/issues\?owner=me$/)
  const list = page.getByRole("region", { name: "Issue list" })
  await expect(list.getByText(mineTitle, { exact: true })).toBeVisible()
  await expect(list.getByText(unownedTitle, { exact: true })).toHaveCount(0)

  // Switching view keeps the filter, and every lane query carries it.
  const laneRequests: string[] = []
  page.on("request", (req) => {
    if (/\/issues\?.*status=/.test(req.url())) laneRequests.push(req.url())
  })
  await page.getByRole("button", { name: "Board", exact: true }).click()
  await expect(page).toHaveURL(/\/issues\?view=board&owner=me$/)
  await expect(lane(page, "To do").getByRole("link", { name: mineTitle, exact: true })).toBeVisible()
  await expect(page.getByRole("link", { name: unownedTitle, exact: true })).toHaveCount(0)
  expect(laneRequests.length).toBeGreaterThanOrEqual(3)
  for (const url of laneRequests) {
    expect(url).toContain("owner=me")
    expect(url).toContain("parent_id=none")
  }

  await page.getByRole("button", { name: "Clear filters" }).click()
  await expect(page).toHaveURL(/\/issues\?view=board$/)
  await expect(lane(page, "To do").getByRole("link", { name: unownedTitle, exact: true })).toBeVisible()
})
