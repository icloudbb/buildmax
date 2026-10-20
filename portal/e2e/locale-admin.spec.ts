import { expect, test, type Page } from "@playwright/test"
import { session } from "./fixtures"

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

// A server identifier: object_storage, system_admin, k8s_job, SUCCEEDED.
const RAW_IDENTIFIER = /^(?:[a-z0-9]+(?:_[a-z0-9]+)+|[A-Z]{2,}(?:_[A-Z]+)*)$/

// What a person reads first on these pages. Raw values may stay as secondary
// technical detail — beside a label (.admin-fact__detail) or in the effective
// configuration disclosure, which is server.yaml by design — so those are not
// here. textContent, not innerText: the fact labels are uppercased by CSS.
async function primaryLabels(page: Page): Promise<string[]> {
  const texts = await page
    .locator(
      ".admin-fact__label, .admin-fact__value, .admin-pill, .admin-sections .bm-status, .admin-table__nowrap, .admin-table__muted",
    )
    .evaluateAll((elements) =>
      elements.filter((element) => !element.closest(".admin-config")).map((element) => element.textContent ?? ""),
    )
  return texts.map((text) => text.trim()).filter(Boolean)
}

async function expectNoRawLabels(page: Page, where: string): Promise<void> {
  const raw = (await primaryLabels(page)).filter((text) => RAW_IDENTIFIER.test(text))
  expect(raw, `${where} shows server identifiers as labels`).toEqual([])
}

// Every failure class shown says who acts and what they check.
async function expectNextSteps(page: Page): Promise<void> {
  for (const step of await page.locator(".admin-failures__next").allTextContents()) {
    expect(step.trim()).not.toBe("")
  }
  const rows = page.locator(".admin-table").filter({ has: page.locator("th", { hasText: /^(Next step|下一步)$/ }) }).locator("tbody tr")
  for (const step of await rows.locator("td:nth-child(3)").allTextContents()) {
    expect(step.trim()).toMatch(/^[^:：]+[:：]\s*\S/)
  }
}

for (const locale of ["en", "zh-CN"] as const) {
  test(`Administration shows labels, not server identifiers (${locale})`, async ({ page }) => {
    const current = await session(page)
    await page.addInitScript((value) => localStorage.setItem("buildmax_locale", value), locale)

    await page.goto("/#/admin")
    // A hash change keeps the loaded app; the language is read on load.
    await page.reload()
    await expect(page.locator(".admin-fact").first()).toBeVisible()
    await expect(page.locator(".settings-section__error")).toHaveCount(0)
    await expect(page.getByText(locale === "en" ? "Object storage" : "对象存储", { exact: true })).toBeVisible()
    await expectNoRawLabels(page, "Overview")
    await expectNextSteps(page)
    // A Space link reads as Portal's links do: no browser-default underline.
    for (const link of await page.locator("a.admin-link").all()) {
      await expect(link).toHaveCSS("text-decoration-line", "none")
    }

    await page.goto("/#/admin/spaces")
    await expect(page.locator(".admin-table, .admin-empty").first()).toBeVisible()
    await expectNoRawLabels(page, "Spaces")

    await page.goto(`/#/admin/spaces/${current.spaceId}`)
    await expect(page.getByLabel(locale === "en" ? "Quota tier" : "配额等级")).toBeVisible()
    await expectNoRawLabels(page, "Space detail")
  })
}
