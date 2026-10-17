import { expect, test, type Page } from "@playwright/test"

import { MEMBER_STATE } from "./global-setup"

/**
 * A page load that goes as expected logs no error. The browser console prints
 * every 401 and 403 as a failed resource, so a refusal Portal asks for on
 * purpose — whether a person is an administrator, whether a signed-out browser
 * has a session — reads as a defect to anyone with the console open.
 */

/** Collect each 401 and 403 the page receives, as "status path". */
function watchRefusals(page: Page): string[] {
  const refused: string[] = []
  page.on("response", (res) => {
    if (res.status() === 401 || res.status() === 403) {
      refused.push(`${res.status()} ${new URL(res.url()).pathname}`)
    }
  })
  return refused
}

test.describe("an account without a deployment grant", () => {
  test.use({ storageState: MEMBER_STATE })

  test("loads its pages without a refused request", async ({ page }) => {
    const refused = watchRefusals(page)
    await page.goto("/")
    await expect(page.getByRole("heading", { name: "Issues", level: 1 })).toBeVisible()
    await page.getByRole("link", { name: "Agents" }).first().click()
    await expect(page.getByRole("heading", { name: "Agents", level: 1 })).toBeVisible()
    await page.reload()
    await expect(page.getByRole("heading", { name: "Agents", level: 1 })).toBeVisible()
    await page.waitForLoadState("networkidle")
    expect(refused).toEqual([])
  })
})

test.describe("a signed-out browser", () => {
  test.use({ storageState: { cookies: [], origins: [] } })

  test("loads the sign-in page without a refused request", async ({ page }) => {
    const refused = watchRefusals(page)
    await page.goto("/")
    await expect(page.locator(".login-page__card")).toBeVisible()
    // A deep link, too: the address a signed-out person most often follows.
    await page.goto("/#/spaces/somewhere/issues")
    await page.reload()
    await expect(page.locator(".login-page__card")).toBeVisible()
    await page.waitForLoadState("networkidle")
    expect(refused).toEqual([])
  })
})
