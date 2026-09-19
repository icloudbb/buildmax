import { expect, test } from "@playwright/test"

import { postJSON, reportLeftovers, session, tagged } from "./fixtures"

/**
 * A schedule is managed on its agent's detail page, so the Schedules tab is
 * where an operator meets one. This is browser-only: the API smoke never renders
 * a schedule, and the handler tests never route to the agent detail view. The
 * dispatcher that fires a due schedule is covered by Go tests; this asserts the
 * surface a person uses to create and read one.
 */
test("a schedule is listed on its agent's Schedules tab", async ({ page }) => {
  const current = await session(page)

  const agent = await postJSON<{ id: string }>(page, `${current.space}/agents`, current, {
    name: tagged("Schedule probe agent"),
    description: "Created by the Portal browser tests.",
    instructions: "Reply with exactly: deployment smoke ok",
  })
  const name = tagged("Nightly probe")
  const schedule = await postJSON<{ id: string }>(page, `${current.space}/schedules`, current, {
    agent_id: agent.id,
    name,
    input: "Summarize the new issues",
    cron_expr: "0 9 * * *",
    timezone: "UTC",
  })
  reportLeftovers(current.spaceId, [`agent ${agent.id}`, `schedule ${schedule.id}`])

  await page.goto(`/#/spaces/${current.spaceId}/agents/${agent.id}`)
  // The agent tablist is keyboard operated and scrolls within the page at
  // narrow widths. Scope it because the sidebar also has Schedules.
  const tabs = page.locator(".agent-detail__tabs")
  await tabs.getByRole("tab", { name: "Overview" }).focus()
  await page.keyboard.press("End")
  await expect(tabs.getByRole("tab", { name: /Revisions/ })).toBeFocused()
  await page.keyboard.press("ArrowLeft")
  await expect(tabs.getByRole("tab", { name: "Schedules", exact: true })).toHaveAttribute("aria-selected", "true")
  await expect(tabs.getByRole("tab", { name: "Schedules", exact: true })).toBeFocused()

  const list = page.locator(".agent-schedules__list")
  await expect(list.getByText(name, { exact: true })).toBeVisible()
  // The cron rule and its enabled state are what tell an operator when it runs.
  await expect(list.getByText("0 9 * * *", { exact: true })).toBeVisible()
  await expect(list.getByText("Enabled", { exact: true })).toBeVisible()
  await expect(page.getByRole("button", { name: "New schedule" })).toHaveClass(/bm-button--primary/)
  await expect(page.getByRole("button", { name: "Run agent" })).toHaveClass(/bm-button--secondary/)
  await expect(list.getByRole("button", { name: "Delete" })).toHaveClass(/bm-button--danger/)

  await tabs.getByRole("tab", { name: "Configuration" }).click()
  await expect(page.getByRole("button", { name: "Save changes" })).toHaveClass(/bm-button--primary/)
  await expect(page.getByRole("button", { name: "Delete agent" })).toHaveClass(/bm-button--danger/)
  await expect(page.getByRole("button", { name: "Run agent" })).toHaveCount(0)
})

test("a failed triggered-task load can be retried in its schedule card", async ({ page }) => {
  const current = await session(page)
  const agent = await postJSON<{ id: string }>(page, `${current.space}/agents`, current, {
    name: tagged("Schedule task error agent"),
    description: "Created by the Portal browser tests.",
    instructions: "Reply with exactly: deployment smoke ok",
  })
  const schedule = await postJSON<{ id: string }>(page, `${current.space}/schedules`, current, {
    agent_id: agent.id,
    name: tagged("Schedule task error"),
    input: "Summarize the new issues",
    cron_expr: "0 9 * * *",
    timezone: "UTC",
  })
  reportLeftovers(current.spaceId, [`agent ${agent.id}`, `schedule ${schedule.id}`])

  let recover = false
  await page.route(new RegExp(`/api/spaces/[^/]+/schedules/${schedule.id}/tasks$`), async (route) => {
    if (recover) return route.continue()
    await route.fulfill({ status: 500, contentType: "application/json", body: JSON.stringify({ error: "injected failure" }) })
  })

  await page.goto(`/#/spaces/${current.spaceId}/agents/${agent.id}`)
  await page.locator(".agent-detail__tabs").getByRole("tab", { name: "Schedules", exact: true }).click()
  const card = page.locator(".agent-schedules__card")
  await card.getByRole("button", { name: "Show triggered tasks" }).click()
  await expect(card.getByRole("alert")).toContainText("injected failure")
  await expect(card.getByText("Loading…")).toHaveCount(0)

  recover = true
  await card.getByRole("button", { name: "Retry triggered tasks" }).click()
  await expect(card.getByText("This schedule has not fired yet.")).toBeVisible()
  await expect(card.getByRole("alert")).toHaveCount(0)
})

// The space-wide overview is the other surface: every schedule across every
// agent, so an owner can see what unattended automation is running without
// opening each agent in turn. It names the owning agent, which the per-agent
// tab does not need to.
test("the space Schedules page lists a schedule and its agent", async ({ page }) => {
  const current = await session(page)

  const agentName = tagged("Space schedule agent")
  const agent = await postJSON<{ id: string }>(page, `${current.space}/agents`, current, {
    name: agentName,
    description: "Created by the Portal browser tests.",
    instructions: "Reply with exactly: deployment smoke ok",
  })
  const name = tagged("Space overview probe")
  const schedule = await postJSON<{ id: string }>(page, `${current.space}/schedules`, current, {
    agent_id: agent.id,
    name,
    input: "Summarize the new issues",
    cron_expr: "30 8 * * 1",
    timezone: "UTC",
  })
  reportLeftovers(current.spaceId, [`agent ${agent.id}`, `schedule ${schedule.id}`])

  await page.goto(`/#/spaces/${current.spaceId}/schedules`)
  await expect(page.getByRole("heading", { name: "Schedules", exact: true })).toBeVisible()
  const panel = page.getByRole("region", { name: "Schedule list" })
  await expect(panel.getByText(name, { exact: true })).toBeVisible()
  // The overview names the owning agent, which is its reason to exist over the
  // per-agent tab.
  await expect(panel.getByText(agentName, { exact: true })).toBeVisible()
})

// The overview is also a creation surface: a member can add a schedule here,
// choosing which agent runs it, without opening the agent first. This drives the
// form through the browser to prove the picker and submit path, not just the API.
test("a schedule can be created from the space Schedules page", async ({ page }) => {
  const current = await session(page)

  const agentName = tagged("Overview create agent")
  const agent = await postJSON<{ id: string }>(page, `${current.space}/agents`, current, {
    name: agentName,
    description: "Created by the Portal browser tests.",
    instructions: "Reply with exactly: deployment smoke ok",
  })
  reportLeftovers(current.spaceId, [`agent ${agent.id}`])

  await page.goto(`/#/spaces/${current.spaceId}/schedules`)
  await expect(page.getByRole("button", { name: "New schedule" })).toHaveCount(1)
  await page.getByRole("button", { name: "New schedule" }).click()

  const name = tagged("Created from overview")
  await page.getByLabel("Agent").selectOption({ label: agentName })
  await page.getByLabel("Name (optional)").fill(name)
  await page.getByLabel("Prompt").fill("Summarize the new issues")
  await page.getByLabel("Cron expression").fill("15 7 * * *")
  await page.getByLabel("Timezone").fill("UTC")
  await page.getByRole("button", { name: "Create schedule" }).click()

  const panel = page.getByRole("region", { name: "Schedule list" })
  await expect(panel.getByText(name, { exact: true })).toBeVisible()
  await expect(panel.getByText(agentName, { exact: true })).toBeVisible()
})
