import { expect, test } from "@playwright/test"

import { session } from "./fixtures"

// docs/design/ui-experience-program.md (D5): the Space governance sections
// (Secrets and the audit trail) read in Chinese. Both are owner-only, and the
// session's own Space is one the signed-in account owns.

test("a Space's secrets read in Chinese", async ({ page }) => {
  const current = await session(page)
  await page.evaluate(() => localStorage.setItem("buildmax_locale", "zh-CN"))
  await page.goto(`/#/spaces/${current.spaceId}/settings/secrets`)
  // The language is read when the app loads; a hash change alone does not reload it.
  await page.reload()

  await expect(page.getByRole("heading", { name: "密钥", exact: true, level: 2 })).toBeVisible()
  await expect(page.getByText("被授予密钥的 Agent 可以读取它的值。")).toBeVisible()

  await page.getByRole("button", { name: "新建密钥" }).click()
  await expect(page.getByRole("heading", { name: "新建密钥", level: 3 })).toBeVisible()
  await expect(page.getByRole("button", { name: "创建密钥" })).toBeVisible()
  await page.getByRole("button", { name: "取消" }).click()
  await expect(page.getByRole("button", { name: "创建密钥" })).toHaveCount(0)
})

test("a Space's audit trail reads in Chinese", async ({ page }) => {
  const current = await session(page)
  await page.evaluate(() => localStorage.setItem("buildmax_locale", "zh-CN"))
  await page.goto(`/#/spaces/${current.spaceId}/settings/audit`)
  await page.reload()

  await expect(page.getByRole("heading", { name: "审计记录", level: 2 })).toBeVisible()
  await expect(page.getByRole("button", { name: "导出 CSV" })).toBeVisible()
  await expect(page.getByText("导出完整记录，而不只是下方这一页。导出操作本身也会被记录。")).toBeVisible()
  // The trail itself loaded rather than falling into the error branch.
  await expect(page.locator(".audit-list, .page-activity__empty").first()).toBeVisible()
  await expect(page.locator(".settings-section__error")).toHaveCount(0)
})
