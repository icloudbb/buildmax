import { expect, test } from "@playwright/test"

import { postJSON, reportLeftovers, session, tagged } from "./fixtures"

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

test("the Issue journey reads in Chinese", async ({ page }) => {
  const current = await session(page)
  const title = tagged("Chinese Issue journey probe")
  const issue = await postJSON<{ id: string }>(page, `${current.space}/issues`, current, {
    title,
    description: "Created by the Portal browser tests to exercise the Chinese interface.",
  })
  reportLeftovers(current.spaceId, [`issue ${issue.id}`])
  await page.evaluate(() => localStorage.setItem("buildmax_locale", "zh-CN"))

  await page.goto(`/#/spaces/${current.spaceId}/issues`)
  // The language is read when the app loads; a hash change alone does not reload it.
  await page.reload()
  await expect(page.getByRole("heading", { name: "Issue", level: 1 })).toBeVisible()
  await expect(page.getByRole("button", { name: "新建 Issue" })).toBeVisible()

  await page.goto(`/#/spaces/${current.spaceId}/issues/${issue.id}`)
  await expect(page.getByRole("heading", { name: title, level: 1 })).toBeVisible()
  const tabs = page.getByRole("navigation", { name: "Issue 分区" })
  await expect(tabs.getByRole("button", { name: "概览" })).toHaveAttribute("aria-current", "true")
  await expect(page.getByRole("heading", { name: "最新结果", exact: true })).toBeVisible()
  await tabs.getByRole("button", { name: "讨论" }).click()
  await expect(page.getByRole("heading", { name: "讨论", exact: true })).toBeVisible()
})
