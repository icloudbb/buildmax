import { expect, test, type Locator, type Page } from "@playwright/test"

import { session } from "./fixtures"

// docs/design/ui-experience-program.md (D5): Space settings and Account
// settings read in Chinese. The role is a stored value; only its label changes.

/** The value cell of one overview row, addressed by its exact term. */
function summaryValue(page: Page, term: string): Locator {
  const rows = page.locator(".space-settings-page__summary > div")
  return rows.filter({ has: page.locator("dt", { hasText: new RegExp(`^${term}$`) }) }).locator("dd")
}

test("Space settings overview and members read in Chinese", async ({ page }) => {
  const current = await session(page)
  await page.evaluate(() => localStorage.setItem("buildmax_locale", "zh-CN"))

  await page.goto(`/#/spaces/${current.spaceId}/settings`)
  // The language is read when the app loads; a hash change alone does not reload it.
  await page.reload()
  await expect(page.getByRole("heading", { name: "Space 设置", level: 1 }).first()).toBeVisible()
  await expect(summaryValue(page, "配额等级")).toBeVisible()
  await expect(summaryValue(page, "你的角色")).toHaveText("所有者")
  await expect(page.locator(".settings-section__error")).toHaveCount(0)

  const tabs = page.getByRole("tablist", { name: "Space 分区" })
  await tabs.getByRole("tab", { name: "成员" }).click()
  const members = page.locator(".settings-page__section").filter({
    has: page.getByRole("heading", { name: "成员", level: 2 }),
  })
  await expect(members.getByText("我", { exact: true })).toBeVisible()
  await expect(page.locator(".settings-section__error")).toHaveCount(0)
})

test("Account general reads in Chinese", async ({ page }) => {
  await session(page)
  await page.evaluate(() => localStorage.setItem("buildmax_locale", "zh-CN"))

  await page.goto("/#/account")
  await page.reload()
  await expect(page.getByRole("heading", { name: "账户", level: 1 }).first()).toBeVisible()
  await expect(page.getByRole("heading", { name: "通用", level: 2 })).toBeVisible()
  await expect(page.getByRole("heading", { name: "密码", level: 2 })).toBeVisible()
  await expect(page.getByLabel("当前密码", { exact: true })).toBeVisible()
})
