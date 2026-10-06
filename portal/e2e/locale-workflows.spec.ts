import { expect, test } from "@playwright/test"

import { postJSON, reportLeftovers, session, tagged } from "./fixtures"

// docs/design/ui-experience-program.md (D5): the Workflows list reads in
// Chinese once the interface language is chosen.
test("the Workflows list reads in Chinese", async ({ page }) => {
  const current = await session(page)
  const agent = await postJSON<{ id: string }>(page, `${current.space}/agents`, current, {
    name: tagged("Chinese Workflow probe agent"),
    description: "Created by the Portal browser tests.",
    instructions: "Reply with exactly: deployment smoke ok",
  })
  const name = tagged("Chinese Workflow probe")
  const workflow = await postJSON<{ id: string }>(page, `${current.space}/workflows`, current, {
    name,
    description: "Created by the Portal browser tests to exercise the Chinese interface.",
    definition: JSON.stringify({
      schema_version: 1,
      nodes: [
        {
          id: "only",
          type: "agent_task",
          agent: { id: agent.id },
          input: { instruction: "Reply with exactly: deployment smoke ok" },
        },
      ],
    }),
  })
  reportLeftovers(current.spaceId, [`agent ${agent.id}`, `workflow ${workflow.id}`])
  await page.evaluate(() => localStorage.setItem("buildmax_locale", "zh-CN"))

  await page.goto(`/#/spaces/${current.spaceId}/workflows`)
  // The language is read when the app loads; a hash change alone does not reload it.
  await page.reload()
  await expect(page.getByRole("heading", { name: "Workflow", exact: true, level: 1 })).toBeVisible()
  await expect(page.getByText("定义可复用的分步执行计划，并手动运行。")).toBeVisible()
  await expect(page.getByRole("button", { name: "新建 Workflow" })).toHaveCount(1)

  const list = page.getByRole("region", { name: "Workflow 列表" })
  await expect(list.getByText(/^\d+ 个 Workflow$/)).toBeVisible()
  // A freshly created workflow is a draft, and its status reads in Chinese.
  await expect(list.locator("li", { hasText: name })).toContainText("草稿")
})
