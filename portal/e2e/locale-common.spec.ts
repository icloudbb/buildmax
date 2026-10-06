import { expect, test } from "@playwright/test"

import { session } from "./fixtures"

// docs/design/ui-experience-program.md (D5): the shared screens every reader
// meets — signing in and an address that matches nothing — read in Chinese.

test("the not-found page reads in Chinese", async ({ page }) => {
  await session(page)
  await page.evaluate(() => localStorage.setItem("buildmax_locale", "zh-CN"))

  await page.goto("/#/this/does/not/exist")
  // The language is read when the app loads; a hash change alone does not reload it.
  await page.reload()
  await expect(page.getByRole("heading", { name: "未找到页面", level: 1 })).toBeVisible()
  await expect(page.getByText("没有与此地址匹配的内容。地址可能有误，或是重命名之前的链接。")).toBeVisible()

  await page.getByRole("button", { name: "返回对话" }).click()
  await expect(page).toHaveURL(/#\/spaces\/[^/]+\/chat$/)
})

test("a Chinese browser signs in in Chinese", async ({ browser }) => {
  // No stored session: the sign-in page is what a fresh visitor sees.
  const context = await browser.newContext({ locale: "zh-CN", storageState: { cookies: [], origins: [] } })
  const page = await context.newPage()
  try {
    await page.goto("/")
    await expect(page.getByText("登录以继续")).toBeVisible()
    await expect(page.getByLabel("邮箱")).toBeVisible()
    await expect(page.getByLabel("密码")).toBeVisible()
    await expect(page.getByRole("button", { name: "登录", exact: true })).toBeVisible()

    await page.getByRole("button", { name: "忘记密码，或已有登录码？" }).click()
    await expect(page.getByLabel("登录码")).toBeVisible()
    await expect(page.getByRole("button", { name: "使用密码登录" })).toBeVisible()
  } finally {
    await context.close()
  }
})
