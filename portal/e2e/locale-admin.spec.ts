import { expect, test } from "@playwright/test"

// docs/design/ui-experience-program.md (D5): the Administration overview reads
// in Chinese. `./make e2e` grants the test account system_admin, as admin.spec.ts
// relies on, which is what makes the area reachable at all.
test("the Administration overview reads in Chinese", async ({ page }) => {
  await page.goto("/#/admin")
  await expect(page.getByRole("heading", { name: "Administration", level: 1 })).toBeVisible()

  await page.evaluate(() => localStorage.setItem("buildmax_locale", "zh-CN"))
  // The language is read when the app loads; a hash change alone does not reload it.
  await page.reload()

  await expect(page.getByRole("heading", { name: "系统管理", level: 1 })).toBeVisible()
  await expect(page.getByRole("heading", { name: "健康状况", exact: true })).toBeVisible()
  await expect(page.getByRole("heading", { name: "工作进度", exact: true })).toBeVisible()
  await expect(page.getByRole("heading", { name: "需要关注的 Space", exact: true })).toBeVisible()
  await expect(page.locator(".settings-section__error")).toHaveCount(0)
})
