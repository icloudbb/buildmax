import { expect, test } from "@playwright/test"

import { RUN_ID, postJSON, reportLeftovers, session } from "./fixtures"
import { buildPluginPackage } from "./pluginPackage"

/**
 * Portal presents the plugin lifecycle as a scoped sequence: Marketplace
 * release -> Space activation -> Agent selection -> run resolution. Each
 * scope has its own handler tests, but nothing below the UI proves the
 * sequence actually connects one page to the next -- that only exists once
 * Marketplace, Space Plugins, and an agent's picker are assembled together.
 *
 * `./make e2e` grants the test account system_admin, which is what makes
 * publishing possible at all.
 */

test("a plugin release moves from Marketplace through Space Plugins to an agent's selection", async ({ page }) => {
  const current = await session(page)
  const name = `e2e-plugin-probe-${RUN_ID}`.toLowerCase()
  const displayName = `E2E Plugin Probe ${RUN_ID}`

  // Publishing is deliberately CLI/admin-only, not a Portal form -- see
  // AdminPlugins.tsx -- so this seeds through the admin API the same bytes
  // `buildmax plugin publish` would send, rather than a form this app has no
  // "publish" button for.
  await postJSON(page, `${current.apiBase}/api/admin/plugins`, current, {
    name,
    display_name: displayName,
  })
  const publishRes = await page.request.post(`${current.apiBase}/api/admin/plugins/${name}/releases`, {
    headers: { Authorization: `Bearer ${current.token}` },
    data: buildPluginPackage(`name: ${name}\nversion: 1.0.0\ndescription: e2e probe.\n`),
  })
  expect(publishRes.ok(), `publish → ${publishRes.status()} ${await publishRes.text()}`).toBeTruthy()

  const agent = await postJSON<{ id: string }>(page, `${current.space}/agents`, current, {
    name: `E2E plugin probe agent ${RUN_ID}`,
  })
  reportLeftovers(current.spaceId, [`plugin ${name}`, `agent ${agent.id}`])

  // Marketplace: the release is listed, and its detail links to Space
  // Plugins for an owner of the current space -- browsing here never
  // activates anything.
  await page.goto("/#/marketplace")
  await expect(page.getByRole("heading", { name: "Marketplace" })).toBeVisible()
  await page.locator(".mkt-card").filter({ hasText: displayName }).click()
  await expect(page.getByRole("heading", { name: "Space activation" })).toBeVisible()
  await page.getByRole("button", { name: "Open Space Plugins" }).click()

  // Space Plugins: an owner activates it for this space.
  await expect(page.getByRole("heading", { name: "Plugins", exact: true })).toBeVisible()
  const row = page.locator(".tp-card").filter({ hasText: displayName })
  await expect(row).toBeVisible()
  await row.getByRole("button", { name: "Activate" }).click()
  await expect(row.locator(".tp-status")).toHaveText(/Active/)

  // Agent Plugins: the agent's picker offers what was just activated.
  await page.goto(`/#/spaces/${current.spaceId}/agents/${agent.id}`)
  await page.getByRole("tab", { name: "Configuration" }).click()
  await page.getByRole("button", { name: "Plugins", exact: true }).click()
  await expect(page.getByText(name)).toBeVisible()

  await expect(page.locator(".settings-section__error")).toHaveCount(0)
  await expect(page.locator(".modal__error")).toHaveCount(0)
})
