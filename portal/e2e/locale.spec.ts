import { expect, test } from "@playwright/test"

import { session } from "./fixtures"

// docs/design/ui-experience-program.md (D5): the interface language is chosen
// in the user menu, applies in place, and survives a reload.
test("switching to Chinese translates the shell in place and persists", async ({ page }) => {
  const current = await session(page)
  await page.goto(`/#/spaces/${current.spaceId}/issues`)
  await expect(page.getByRole("button", { name: "Schedules" }).first()).toBeVisible()

  await page.getByRole("button", { name: "User menu" }).first().click()
  await page.getByRole("menuitemradio", { name: "简体中文" }).click()

  await expect(page.getByRole("button", { name: "定时任务" }).first()).toBeVisible()
  await expect(page.locator("html")).toHaveAttribute("lang", "zh-CN")
  await expect(page).toHaveTitle(/^Issue · .+ · BuildMax$/)

  await page.reload()
  await expect(page.getByRole("button", { name: "定时任务" }).first()).toBeVisible()

  await page.getByRole("button", { name: "用户菜单" }).first().click()
  await page.getByRole("menuitemradio", { name: "English" }).click()
  await expect(page.getByRole("button", { name: "Schedules" }).first()).toBeVisible()
  await expect(page.locator("html")).toHaveAttribute("lang", "en")
})

test("a Chinese browser starts in Chinese", async ({ browser }) => {
  const context = await browser.newContext({ locale: "zh-CN", storageState: "./e2e/.auth/state.json" })
  const page = await context.newPage()
  try {
    const current = await session(page)
    await page.goto(`/#/spaces/${current.spaceId}/issues`)
    await expect(page.getByRole("button", { name: "定时任务" }).first()).toBeVisible()
  } finally {
    await context.close()
  }
})
