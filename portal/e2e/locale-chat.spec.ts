import { expect, test } from "@playwright/test"

import { postJSON, reportLeftovers, session } from "./fixtures"

// docs/design/ui-experience-program.md (D5): starting a chat and reading a
// conversation work in Chinese.
test("the chat journey reads in Chinese", async ({ page }) => {
  const current = await session(page)
  await page.evaluate(() => localStorage.setItem("buildmax_locale", "zh-CN"))

  await page.goto(`/#/spaces/${current.spaceId}/chat`)
  // The language is read when the app loads; a hash change alone does not reload it.
  await page.reload()
  await expect(page.getByRole("heading", { name: "对话", level: 1 })).toBeVisible()
  await expect(page.getByRole("textbox", { name: "你想做什么？" })).toBeVisible()
  await expect(page.getByRole("tab", { name: "最近的对话" })).toHaveAttribute("aria-selected", "true")
  await page.getByRole("tab", { name: "文件" }).click()
  await expect(page.getByRole("link", { name: "打开文件" })).toBeVisible()

  const conversation = await postJSON<{ conversation_id: string }>(
    page,
    `${current.space}/conversations`,
    current,
    { channel: "portal" }
  )
  reportLeftovers(current.spaceId, [`conversation ${conversation.conversation_id}`])
  await page.goto(`/#/spaces/${current.spaceId}/chat/${conversation.conversation_id}`)
  await expect(page.getByRole("textbox", { name: "消息", exact: true })).toBeVisible()
  await expect(page.getByLabel("对话记录")).toBeVisible()
})
