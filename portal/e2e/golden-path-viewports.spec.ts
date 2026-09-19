import { expect, test, type Page } from "@playwright/test"

import { patchJSON, postJSON, reportLeftovers, session, tagged, type Session } from "./fixtures"

/**
 * The design's own acceptance criteria, run literally: "At 390, 768, and 1280
 * CSS pixels, a user can switch Space, start Chat, open an Issue and its
 * latest run, browse Files, and reach Space settings." One test,
 * one seed, three widths — reseeding per width would only spend the run's
 * budget on repeating the same worker turn.
 *
 * See docs/design/portal-responsive-and-accessible-interaction.md.
 */

const RUN_TIMEOUT_MS = 150_000
const WIDTHS = [390, 768, 1280] as const

async function expectNoHorizontalOverflow(page: Page, label: string): Promise<void> {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow, `${label} caused horizontal overflow`).toBeLessThanOrEqual(0)
}

/** Seed an issue whose agent has run to completion, and return its issue id. */
async function seedCompletedAgentRun(page: Page, current: Session): Promise<string> {
  const space = current.space
  const agent = await postJSON<{ id: string }>(page, `${space}/agents`, current, {
    name: tagged("Viewport golden path"),
    description: "Created by the Portal browser tests.",
    instructions: "Reply with exactly: deployment smoke ok",
  })
  const issue = await postJSON<{ id: string; version: number }>(page, `${space}/issues`, current, {
    title: tagged("Viewport golden path"),
    description: "Created by the Portal browser tests to exercise the golden path at every width.",
  })
  reportLeftovers(current.spaceId, [`agent ${agent.id}`, `issue ${issue.id}`])
  await patchJSON(page, `${space}/issues/${encodeURIComponent(issue.id)}`, current, {
    version: issue.version,
    executor_kind: "agent",
    executor_id: agent.id,
  })
  const task = await postJSON<{ id: string }>(
    page,
    `${space}/issues/${encodeURIComponent(issue.id)}/agent-runs`,
    current,
    { input: "Reply with exactly: deployment smoke ok" }
  )
  await expect
    .poll(
      async () => {
        const res = await page.request.get(`${space}/tasks/${encodeURIComponent(task.id)}`, {
          headers: { Authorization: `Bearer ${current.token}` },
        })
        if (!res.ok()) return `HTTP ${res.status()}`
        const body = (await res.json()) as { status: string; error_message?: string | null }
        return body.status === "FAILED" ? `FAILED: ${body.error_message ?? "no message"}` : body.status
      },
      { timeout: RUN_TIMEOUT_MS, intervals: [1000] }
    )
    .toBe("SUCCEEDED")
  return issue.id
}

/**
 * Open the Space switcher, which lives in the narrow drawer below 768px and
 * in the persistent sidebar at or above it. The persistent `<aside>` stays in
 * the DOM (just CSS-hidden) even while the drawer is open, and both mount the
 * same labeled `<select>`, so the switcher is scoped to whichever container
 * is the visible one rather than matched by label alone.
 */
async function withSpaceSwitcher(page: Page, width: number, fn: (switcher: ReturnType<Page["getByLabel"]>) => Promise<void>) {
  if (width < 768) {
    await page.getByRole("button", { name: "Open navigation" }).click()
    const drawer = page.getByRole("dialog", { name: "Navigation" })
    await expect(drawer).toBeVisible()
    await fn(drawer.getByLabel("Space", { exact: true }))
    // Escape closes the drawer without navigating, leaving whatever page the
    // switcher's change already routed to (or didn't) untouched.
    await page.keyboard.press("Escape")
    return
  }
  const sidebar = page.getByLabel("Sidebar", { exact: true })
  await fn(sidebar.getByLabel("Space", { exact: true }))
}

test("switch Space, start Chat, open an Issue's latest run, browse Files, and reach Space settings — at 390, 768, and 1280px", async ({
  page,
}) => {
  test.setTimeout(RUN_TIMEOUT_MS + 60_000)

  const current = await session(page)
  const spaceB = await postJSON<{ id: string; name: string }>(page, `${current.apiBase}/api/spaces`, current, {
    name: tagged("Viewport probe space"),
  })
  reportLeftovers(current.spaceId, [`(and space ${spaceB.id}, no delete route)`])
  const issueId = await seedCompletedAgentRun(page, current)

  // The Space list loads once when the app mounts; created via the API
  // directly (seeding goes through the API rather than the UI, per
  // fixtures.ts), the running session does not know about it until the app
  // remounts.
  await page.reload()

  for (const width of WIDTHS) {
    await page.setViewportSize({ width, height: 900 })

    // --- Switch Space ---
    // Asserted against the select's own value, not visible text: a closed
    // <select>'s text content is every <option>'s text concatenated, not
    // just the selected one, so it would read as "visible" either way.
    await page.goto(`/#/spaces/${current.spaceId}/chat`)
    await withSpaceSwitcher(page, width, async (switcher) => {
      await switcher.selectOption({ label: spaceB.name })
      await expect(switcher).toHaveValue(spaceB.id)
    })
    await withSpaceSwitcher(page, width, async (switcher) => {
      await switcher.selectOption({ label: "My Space" })
      await expect(switcher).toHaveValue(current.spaceId)
    })
    await expectNoHorizontalOverflow(page, `${width}px switch Space`)

    // --- Start Chat ---
    await page.goto(`/#/spaces/${current.spaceId}/chat`)
    await expect(page.getByRole("textbox", { name: "What would you like to do?" })).toBeVisible()
    await expectNoHorizontalOverflow(page, `${width}px start Chat`)

    // --- Open an Issue and its latest run ---
    await page.goto(`/#/spaces/${current.spaceId}/issues/${issueId}`)
    await expect(page.getByRole("heading", { level: 1 })).toContainText("Viewport golden path")
    // Discussion is its own tab, not part of the default Overview.
    await page.getByRole("navigation", { name: "Issue sections" }).getByRole("button", { name: "Discussion" }).click()
    await page.locator(".issue-discussion__actions").getByRole("button", { name: "Run details" }).first().click()
    const runDialog = page.getByRole("dialog", { name: "Run details" })
    await expect(runDialog).toBeVisible()
    await expect(runDialog.locator(".modal__error")).toHaveCount(0)
    await page.keyboard.press("Escape")
    await expect(runDialog).toBeHidden()
    await expectNoHorizontalOverflow(page, `${width}px open Issue and its latest run`)

    // --- Browse Files ---
    await page.goto(`/#/spaces/${current.spaceId}/files`)
    await expect(page.getByRole("heading", { name: "Files" })).toBeVisible()
    await expectNoHorizontalOverflow(page, `${width}px browse Files`)

    // --- Reach Space settings ---
    await page.goto(`/#/spaces/${current.spaceId}/settings`)
    await expect(page.getByRole("heading", { name: "Space settings", exact: true }).first()).toBeVisible()
    await expectNoHorizontalOverflow(page, `${width}px reach Space settings`)
  }
})
