import { expect, test, type Page } from "@playwright/test"

import { postJSON, reportLeftovers, session, tagged, type Session } from "./fixtures"

// Narrow-width (320–767px) coverage for Slice 3 of
// docs/design/portal-responsive-and-accessible-interaction.md: Chat, Issues,
// Issue Detail, and Task Detail must reflow without page-level horizontal
// scroll and keep their primary actions reachable. A global stylesheet rule
// is not evidence of support on its own, so this runs alongside the slice
// rather than waiting for a later regression matrix.

test.use({ viewport: { width: 390, height: 844 } })

async function expectNoHorizontalOverflow(page: Page): Promise<void> {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(0)
}

test("the New Chat page has no horizontal overflow and its composer and tabs are reachable", async ({ page }) => {
  await session(page)

  await expect(page.getByRole("heading", { name: "Chat", level: 1 })).toBeVisible()
  await expect(page.getByRole("textbox", { name: "What would you like to do?" })).toBeVisible()
  const conversationsTab = page.getByRole("tab", { name: "Recent Conversations" })
  const filesTab = page.getByRole("tab", { name: "Files" })
  await expect(conversationsTab).toBeVisible()
  await expect(filesTab).toBeVisible()
  await conversationsTab.focus()
  await page.keyboard.press("ArrowRight")
  await expect(filesTab).toHaveAttribute("aria-selected", "true")
  await expect(filesTab).toBeFocused()
  await expect(page.getByRole("link", { name: "Open Files" })).toBeVisible()
  await page.keyboard.press("ArrowLeft")
  await expect(conversationsTab).toBeFocused()
  await expectNoHorizontalOverflow(page)
})

test("Issues and Issue Detail reflow to one column with reachable actions", async ({ page }) => {
  const current = await session(page)
  const issue = await postJSON<{ id: string }>(page, `${current.space}/issues`, current, {
    title: tagged("Responsive layout probe"),
    description: "Created by the Portal browser tests to exercise narrow-width layout.",
  })
  reportLeftovers(current.spaceId, [`issue ${issue.id}`])

  await page.goto(`/#/spaces/${current.spaceId}/issues`)
  await expect(page.getByRole("button", { name: "New Issue" })).toHaveCount(1)
  await expectNoHorizontalOverflow(page)

  await page.goto(`/#/spaces/${current.spaceId}/issues/${issue.id}`)
  await expect(page.getByRole("link", { name: "Back to Issues" })).toBeVisible()
  await expect(page.getByRole("button", { name: "Edit issue" })).toBeVisible()
  await page.getByRole("button", { name: "Edit issue" }).click()
  await expect(page.getByRole("button", { name: "Save changes" })).toBeVisible()
  // The detail grid already collapses to one column before Narrow width (see
  // the comment in issues.css); confirm that is still true right down at 390.
  const columns = await page
    .locator(".issue-detail-page__grid")
    .evaluate((el) => getComputedStyle(el).gridTemplateColumns.split(" ").length)
  expect(columns).toBe(1)
  await expectNoHorizontalOverflow(page)
})

async function startAgentRun(page: Page, current: Session): Promise<string> {
  const agentName = tagged("Responsive layout probe")
  const agent = await postJSON<{ id: string }>(page, `${current.space}/agents`, current, {
    name: agentName,
    description: "Created by the Portal browser tests.",
    instructions: "Reply with exactly: deployment smoke ok",
  })
  reportLeftovers(current.spaceId, [`agent ${agent.id}`])

  await page.goto(`/#/spaces/${current.spaceId}/agents`)
  await page.getByRole("button", { name: `Run ${agentName}` }).click()
  const modal = page.getByRole("dialog", { name: `Run ${agentName}` })
  await expect(modal).toBeVisible()
  await modal.getByLabel("Task").fill("Narrow layout probe")
  await modal.getByRole("button", { name: "Start" }).click()
  await page.waitForURL(/#\/spaces\/[^/]+\/tasks\//, { timeout: 15_000 })
  return decodeURIComponent(page.url().split("/tasks/")[1] ?? "")
}

test("Task Detail reflows: header actions wrap under the title and the composer stays reachable", async ({
  page,
}) => {
  const current = await session(page)
  const taskId = await startAgentRun(page, current)
  reportLeftovers(current.spaceId, [`task ${taskId}`])

  await expect(page.getByRole("textbox", { name: "Continue task" })).toBeVisible()
  await expect(page.getByRole("button", { name: "Details" })).toBeVisible()
  await expectNoHorizontalOverflow(page)

  // The fixed 5rem side margins that exist on wide screens must not survive
  // down to Narrow width — they alone would leave only a couple hundred
  // pixels for the thread.
  const chatWidth = await page.locator(".page-chat").evaluate((el) => el.getBoundingClientRect().width)
  expect(chatWidth).toBeGreaterThan(340)
})
