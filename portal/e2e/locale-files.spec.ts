import { expect, test } from "@playwright/test"

import { RUN_ID, reportLeftovers, session, tagged, uploadFile } from "./fixtures"

// docs/design/ui-experience-program.md (D5): the Files and Artifacts pages read
// in Chinese once the interface language is switched.
test("the Files page reads in Chinese", async ({ page }) => {
  const current = await session(page)
  const name = `locale-files-probe-${RUN_ID}.txt`
  await uploadFile(page, current, name, "locale files probe\n")
  reportLeftovers(current.spaceId, [`file ${name}`])
  await page.evaluate(() => localStorage.setItem("buildmax_locale", "zh-CN"))

  await page.goto(`/#/spaces/${current.spaceId}/files`)
  // The language is read when the app loads; a hash change alone does not reload it.
  await page.reload()
  await expect(page.getByRole("heading", { name: "文件", level: 1 })).toBeVisible()
  await expect(page.getByRole("button", { name: "上传文件", exact: true })).toBeVisible()
  await expect(page.getByRole("button", { name: "上传文件夹", exact: true })).toBeVisible()

  await page.getByRole("button", { name }).click()
  const viewer = page.getByLabel("文件内容")
  await expect(viewer.getByRole("heading", { name })).toBeVisible()
})

test("the Artifacts pages read in Chinese", async ({ page }) => {
  const current = await session(page)
  const filename = `${tagged("locale-artifact")}.txt`
  const res = await page.request.post(
    `${current.apiBase}/api/spaces/${encodeURIComponent(current.spaceId)}/artifacts`,
    {
      headers: { Authorization: `Bearer ${current.token}` },
      multipart: { file: { name: filename, mimeType: "text/plain", buffer: Buffer.from("locale artifact probe\n") } },
    },
  )
  expect(res.ok(), `upload ${filename} → ${res.status()} ${await res.text()}`).toBeTruthy()
  const artifact = (await res.json()) as { id: string }
  reportLeftovers(current.spaceId, [`artifact ${artifact.id}`])
  await page.evaluate(() => localStorage.setItem("buildmax_locale", "zh-CN"))

  await page.goto(`/#/spaces/${current.spaceId}/artifacts`)
  await page.reload()
  await expect(page.getByRole("heading", { name: "Artifact", level: 1, exact: true })).toBeVisible()
  await expect(page.getByRole("button", { name: "上传文件", exact: true })).toBeVisible()
  const row = page.locator(".artifact-row").filter({ has: page.getByRole("link", { name: filename }) })
  await expect(row.getByRole("button", { name: "下载" })).toBeVisible()
  await expect(row.getByText("手动上传")).toBeVisible()

  await row.getByRole("link", { name: filename }).click()
  await expect(page.getByRole("heading", { name: filename, level: 1 })).toBeVisible()
  await expect(page.getByText("详情", { exact: true })).toBeVisible()
  await page.getByRole("button", { name: "分享" }).click()
  await expect(page.getByRole("dialog", { name: "分享这个 Artifact" })).toBeVisible()
})
