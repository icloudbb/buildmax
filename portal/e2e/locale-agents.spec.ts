import { expect, test } from "@playwright/test"

import { postJSON, reportLeftovers, session, tagged } from "./fixtures"

// docs/design/ui-experience-program.md (D5): the Agents list and the space
// Schedules overview read in Chinese once that language is chosen.
test("the Agents list and the Schedules page read in Chinese", async ({ page }) => {
  const current = await session(page)
  const agentName = tagged("Chinese agents probe")
  const agent = await postJSON<{ id: string }>(page, `${current.space}/agents`, current, {
    name: agentName,
    description: "Created by the Portal browser tests to exercise the Chinese interface.",
    instructions: "Reply with exactly: deployment smoke ok",
  })
  const scheduleName = tagged("Chinese schedule probe")
  const schedule = await postJSON<{ id: string }>(page, `${current.space}/schedules`, current, {
    executor_kind: "agent",
    executor_id: agent.id,
    name: scheduleName,
    input: "Summarize the new issues",
    cron_expr: "0 9 * * *",
    timezone: "UTC",
  })
  reportLeftovers(current.spaceId, [`agent ${agent.id}`, `schedule ${schedule.id}`])
  await page.evaluate(() => localStorage.setItem("buildmax_locale", "zh-CN"))

  await page.goto(`/#/spaces/${current.spaceId}/agents`)
  // The language is read when the app loads; a hash change alone does not reload it.
  await page.reload()
  await expect(page.getByRole("heading", { name: "Agent", exact: true, level: 1 })).toBeVisible()
  await expect(page.getByRole("button", { name: "创建 Agent", exact: true })).toBeVisible()
  await expect(page.getByText("运行总数", { exact: true })).toBeVisible()
  await expect(page.getByRole("button", { name: `打开 Agent ${agentName}` })).toBeVisible()

  await page.goto(`/#/spaces/${current.spaceId}/schedules`)
  await expect(page.getByRole("heading", { name: "定时任务", exact: true, level: 1 })).toBeVisible()
  await expect(page.getByRole("button", { name: "新建定时任务" })).toBeVisible()
  await expect(page.getByRole("button", { name: "全部暂停" })).toBeVisible()
  const panel = page.getByRole("region", { name: "定时任务列表" })
  await expect(panel.getByText(scheduleName, { exact: true })).toBeVisible()
  await expect(panel.getByText("已启用", { exact: true }).first()).toBeVisible()
})
