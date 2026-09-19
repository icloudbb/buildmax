import { expect, test } from "@playwright/test"

import { reportLeftovers, session, tagged } from "./fixtures"

test("an uploaded artifact has a navigable row, action roles, and a recoverable preview", async ({ page }) => {
  const current = await session(page)
  const filename = `${tagged("artifact-preview")}.txt`
  await page.goto(`/#/spaces/${current.spaceId}/artifacts`)

  await expect(page.getByRole("heading", { name: "Artifacts", exact: true })).toBeVisible()
  await expect(page.getByRole("button", { name: "Upload a file" })).toHaveClass(/bm-button--primary/)
  await expect(page.getByRole("heading", { name: "All Artifacts" })).toHaveCount(0)

  const uploaded = page.waitForResponse((response) =>
    response.request().method() === "POST" && /\/api\/spaces\/[^/]+\/artifacts$/.test(response.url()))
  await page.locator(".artifact-list__file-input").setInputFiles({
    name: filename,
    mimeType: "text/plain",
    buffer: Buffer.from("artifact preview probe\n"),
  })
  const uploadResponse = await uploaded
  expect(uploadResponse.ok()).toBeTruthy()
  const artifact = (await uploadResponse.json()) as { id: string }
  reportLeftovers(current.spaceId, [`artifact ${artifact.id}`])

  const row = page.locator(".artifact-row").filter({ has: page.getByRole("link", { name: filename }) })
  await expect(row.getByRole("button", { name: "Download" })).toHaveClass(/bm-button--secondary/)
  await expect(row.getByRole("button", { name: "Delete" })).toHaveClass(/bm-button--danger/)

  let failPreview = true
  await page.route(new RegExp(`/api/artifacts/${artifact.id}/content$`), async (route) => {
    if (!failPreview) return route.continue()
    failPreview = false
    await route.fulfill({ status: 500, contentType: "application/json", body: JSON.stringify({ error: "injected preview failure" }) })
  })
  await row.getByRole("link", { name: filename }).click()
  await expect(page.getByRole("heading", { name: filename, level: 1 })).toBeVisible()
  await expect(page.getByRole("button", { name: "Download" })).toHaveClass(/bm-button--secondary/)
  await expect(page.getByRole("button", { name: "Share" })).toHaveClass(/bm-button--secondary/)
  await expect(page.getByRole("button", { name: "Delete" })).toHaveClass(/bm-button--danger/)

  const preview = page.locator(".artifact-preview")
  await expect(preview.getByRole("alert")).toContainText("injected preview failure")
  await preview.getByRole("button", { name: "Retry preview" }).click()
  await expect(preview.getByText("artifact preview probe")).toBeVisible()
  await expect(preview.getByRole("alert")).toHaveCount(0)

  await page.getByRole("button", { name: "Share" }).click()
  const share = page.getByRole("dialog", { name: "Share this artifact" })
  await expect(share.getByRole("button", { name: "Create public link" })).toHaveClass(/bm-button--primary/)
})
