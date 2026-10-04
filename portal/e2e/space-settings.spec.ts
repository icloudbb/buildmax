import { expect, test, type Locator, type Page } from "@playwright/test"

import { createSpace, getJSON, reportLeftovers, session, tagged } from "./fixtures"

/**
 * The space overview is where an operator reads the deployment's own settings
 * back: which quota tier this server was configured with, and how much of it
 * the space has spent.
 *
 * None of it is a Portal decision. `default_quota_tier` is a line in the
 * deployment's `server.yaml`, and the counts are what the server totalled over
 * its own usage window — so a handler test cannot know what the deployment was
 * configured to say, and the API smoke never renders it. What this spec adds is
 * the last link: the number an operator acts on is the number the deployment
 * reported, not a placeholder the UI fell back to.
 *
 * The audit section is covered separately, in space-audit.spec.ts.
 */

interface Usage {
  run_count: number
  total_tokens: number
  tier: string
  period_days: number
  max_runs_per_period?: number
  max_tokens_per_period?: number
}

/** The value cell of one overview row, addressed by its exact term. */
function summaryValue(page: Page, term: string): Locator {
  const rows = page.locator(".space-settings-page__summary > div")
  return rows.filter({ has: page.locator("dt", { hasText: new RegExp(`^${term}$`) }) }).locator("dd")
}

test("the space overview reports the deployment's quota tier and what it counted", async ({ page }) => {
  const current = await session(page)
  const usage = await getJSON<Usage>(page, `${current.space}/usage`, current)

  // Reachable by URL, not only by clicking through the tabs: a section an
  // operator cannot link to is one they cannot send to a colleague.
  await page.goto(`/#/spaces/${current.spaceId}/settings`)
  await expect(page.getByRole("heading", { name: "Space settings", exact: true }).first()).toBeVisible()

  // Compared against what the deployment answered rather than a hard-coded
  // tier. The claim is that Portal reports this server, and pinning the value
  // here would instead assert what the smoke's server.yaml happens to say.
  await expect(summaryValue(page, "Quota tier")).toHaveText(usage.tier)
  await expect(summaryValue(page, "Your role")).toHaveText("owner")

  // The limit half is the part that has to have crossed the wire. Rendered
  // without one, this cell silently drops to a bare count, which reads as a
  // space with no quota rather than as a deployment that failed to report it.
  expect(usage.max_runs_per_period, "the deployment reported no run limit").toBeGreaterThan(0)
  await expect(summaryValue(page, "Runs this period")).toHaveText(
    `${usage.run_count} / ${usage.max_runs_per_period}`
  )
  await expect(page.getByText(`Current usage window: last ${usage.period_days} days.`)).toBeVisible()

  await expect(page.locator(".settings-section__error")).toHaveCount(0)
})

test("the space members section names the signed-in account", async ({ page }) => {
  const current = await session(page)
  await page.goto(`/#/spaces/${current.spaceId}/settings/members`)

  // Owner-only controls are the point: whether this account may remove members
  // is decided from the membership the deployment returned, and the section is
  // the only place that decision is visible.
  const members = page.locator(".settings-page__section").filter({
    has: page.getByRole("heading", { name: "Members" }),
  })
  await expect(members.getByText("Me", { exact: true })).toBeVisible()
  await expect(page.locator(".settings-section__error")).toHaveCount(0)
})

test("a space owner creates and disables a service account", async ({ page }) => {
  // A service account belongs to a team space, never a personal one, so the
  // spec makes its own. The page is reloaded so the switcher knows the Space.
  const current = await session(page)
  const team = await createSpace(page, current, tagged("Service account probe"))
  reportLeftovers(team.id, [`space ${team.id}`])
  await page.reload()
  await page.goto(`/#/spaces/${team.id}/settings/service-accounts`)

  const section = page.getByRole("region", { name: "Service accounts" })
  await section.getByRole("button", { name: "New service account" }).click()
  const name = tagged("Probe bot")
  await section.getByLabel("Name").fill(name)
  await section.getByRole("button", { name: "Create service account" }).click()

  const card = section.getByTestId("service-account").filter({ hasText: name })
  await expect(card).toContainText("Sponsored by you")
  await expect(card).toContainText("active")

  await card.getByRole("button", { name: "Disable" }).click()
  await expect(card.getByRole("button", { name: "Enable" })).toBeVisible()
  await expect(card).toContainText("disabled")

  // What the page shows is what the server holds, not local state.
  const accounts = await getJSON<{ id: string; name: string; disabled_at?: string }[]>(
    page,
    `${current.apiBase}/api/spaces/${team.id}/service-accounts`,
    current
  )
  const stored = accounts.find((a) => a.name === name)
  expect(stored?.disabled_at, "the server did not record the disable").toBeTruthy()

  // The roster names it as a service account and offers no member controls.
  await page.goto(`/#/spaces/${team.id}/settings/members`)
  const row = page.locator(".space-settings-page__member").filter({ hasText: name })
  await expect(row).toContainText("Service account")
  await expect(row.getByRole("button")).toHaveCount(0)
})
