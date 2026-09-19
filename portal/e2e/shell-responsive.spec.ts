import { expect, test } from "@playwright/test"

import { session } from "./fixtures"

// Narrow-width (320–767px) and keyboard coverage for the shell drawer and the
// shared dialog primitives, added alongside the slice that introduced them
// rather than deferred to a later regression matrix — a global stylesheet
// rule is not evidence of support on its own. See
// docs/design/portal-responsive-and-accessible-interaction.md.

test.use({ viewport: { width: 390, height: 844 } })

test("the narrow shell shows a compact header instead of the persistent sidebar", async ({ page }) => {
  await session(page)

  await expect(page.getByLabel("Sidebar", { exact: true })).toBeHidden()
  await expect(page.getByRole("button", { name: "Open navigation" })).toBeVisible()
  // No page ever needs two-dimensional scrolling to reach its own controls.
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(0)
})

test("the drawer traps Tab focus, closes on Escape, and returns focus to the menu button", async ({ page }) => {
  await session(page)

  const menuButton = page.getByRole("button", { name: "Open navigation" })
  await menuButton.focus()
  await menuButton.click()

  const dialog = page.getByRole("dialog", { name: "Navigation" })
  await expect(dialog).toBeVisible()

  // Shift+Tab from the first focusable element wraps to the last, proving the
  // trap rather than just that focus moved somewhere on open.
  await expect(dialog.getByRole("button", { name: "Close" })).toBeFocused()
  await page.keyboard.press("Shift+Tab")
  await expect(dialog.getByRole("button", { name: "User menu" })).toBeFocused()

  await page.keyboard.press("Escape")
  await expect(dialog).toBeHidden()
  await expect(menuButton).toBeFocused()
})

test("choosing a destination in the drawer navigates and closes it", async ({ page }) => {
  await session(page)

  await page.getByRole("button", { name: "Open navigation" }).click()
  await page.getByRole("dialog", { name: "Navigation" }).getByRole("button", { name: "Agents" }).click()

  await expect(page).toHaveURL(/#\/spaces\/[^/]+\/agents$/)
  await expect(page.getByRole("dialog")).toBeHidden()
})

test("a tabbed dialog becomes a full-height sheet with a horizontal, arrow-key tablist", async ({ page }) => {
  const current = await session(page)
  await page.goto(`/#/spaces/${current.spaceId}/agents`)
  // exact: agents named "…create agent…" render cards whose accessible name
  // matches this substring, so a seeded list makes the bare locator ambiguous.
  await page.getByRole("button", { name: "Create agent", exact: true }).click()

  const dialog = page.getByRole("dialog", { name: "New Agent" })
  await expect(dialog).toBeVisible()

  // The dialog element (role="dialog") is itself the ".modal" node.
  const box = await dialog.boundingBox()
  expect(box?.height).toBeGreaterThanOrEqual(800) // near-full 844px viewport height

  const basicsTab = page.getByRole("tab", { name: "Basics" })
  const sandboxTab = page.getByRole("tab", { name: "Sandbox access" })
  await expect(basicsTab).toHaveAttribute("aria-selected", "true")

  await basicsTab.focus()
  await page.keyboard.press("ArrowRight")
  await expect(sandboxTab).toHaveAttribute("aria-selected", "true")
  await expect(sandboxTab).toBeFocused()

  await page.keyboard.press("Escape")
  await expect(dialog).toBeHidden()
})

test("the compact header shows a state label, never a fabricated 'My Space', when the Space list fails", async ({ page }) => {
  // The compact header names the current Space. When the Space summary cannot
  // resolve it must fall back to the shared unresolved-state label, not a
  // fabricated "My Space" — see docs/design/portal-state-and-permission-feedback.md.
  const current = await session(page)
  await page.goto(`/#/spaces/${current.spaceId}/agents`)

  // Fail the Space list, then reload so the app re-fetches it: a hash change
  // alone would not re-run the fetch, and the already-loaded summary would
  // still resolve. On reload the id persists in storage but its summary cannot
  // resolve, so the header has no Space name to show.
  await page.route(/\/api\/spaces(\?|$)/, (route) =>
    route.fulfill({
      status: 500,
      contentType: "application/json",
      body: JSON.stringify({ error: "injected failure" }),
    }),
  )
  await page.reload()

  const compactSpace = page.locator(".shell__compact-space")
  await expect(compactSpace).toBeVisible()
  await expect(compactSpace).toHaveText("Space unavailable")
})
