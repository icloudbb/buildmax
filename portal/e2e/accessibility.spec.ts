import AxeBuilder from "@axe-core/playwright"
import { expect, test, type Page } from "@playwright/test"

import { animationsSettled, session } from "./fixtures"

// A focused accessibility scan for the shell, one dialog, and one tabbed
// surface, per the acceptance criteria in
// docs/design/portal-responsive-and-accessible-interaction.md. This is not a
// site-wide audit — each scan is scoped to the region Slices 1-2 built (via
// `include`), not the whole page, so an unrelated pre-existing issue
// elsewhere in the page behind a dialog does not fail a test about the
// dialog's own semantics.
//
// Scoped to WCAG 2.x A/AA rules, which is what the design's Accessibility
// Requirements section actually asks for (names/states/relationships, focus,
// target size, color-as-the-only-signal, motion) — axe's "best-practice"
// heuristics are a different, opinionated bar this design doc does not
// commit to.
//
// `color-contrast` is on: the theme tokens meet AA in both themes, and the
// screenshot suite (portal/visual) holds the specimen and one page per
// template to it on every pull request. A violation here is fixed in the
// token or the component, not by disabling the rule.
const TAGS = ["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"]

async function scan(page: Page, include: string) {
  await animationsSettled(page)
  return new AxeBuilder({ page }).include(include).withTags(TAGS).analyze()
}

function describeViolations(violations: { id: string; help: string; nodes: { target: unknown[] }[] }[]): string {
  return violations
    .map((v) => `${v.id} (${v.help}): ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`)
    .join("\n")
}

test.use({ viewport: { width: 390, height: 844 } })

test("the narrow shell and its open navigation drawer have no WCAG A/AA violations", async ({ page }) => {
  await session(page)
  await page.getByRole("button", { name: "Open navigation" }).click()
  await expect(page.getByRole("dialog", { name: "Navigation" })).toBeVisible()

  const results = await scan(page, ".shell__compact-header, .drawer-overlay")
  expect(results.violations, describeViolations(results.violations)).toEqual([])
})

test("a plain dialog (Create Space) has no WCAG A/AA violations", async ({ page }) => {
  await session(page)
  // The persistent sidebar is CSS-hidden at this file's narrow viewport; its
  // "+" control is reachable through the drawer instead.
  await page.getByRole("button", { name: "Open navigation" }).click()
  await page
    .getByRole("dialog", { name: "Navigation" })
    .getByRole("button", { name: "Create a new space" })
    .click()
  await expect(page.getByRole("dialog", { name: "Create Space" })).toBeVisible()

  const results = await scan(page, ".modal-overlay")
  expect(results.violations, describeViolations(results.violations)).toEqual([])
})

test("a tabbed surface (the Create Agent dialog's tabs) has no WCAG A/AA violations", async ({ page }) => {
  const current = await session(page)
  await page.goto(`/#/spaces/${current.spaceId}/agents`)
  await expect(page.getByRole("button", { name: "Create agent", exact: true })).toHaveCount(1)
  await page.getByRole("button", { name: "Create agent", exact: true }).click()
  await expect(page.getByRole("dialog", { name: "New Agent" })).toBeVisible()
  await expect(page.getByRole("tablist")).toBeVisible()

  const results = await scan(page, ".modal-overlay")
  expect(results.violations, describeViolations(results.violations)).toEqual([])
})

test("the narrow Issue Board and its filters have no WCAG A/AA violations", async ({ page }) => {
  const current = await session(page)
  await page.goto(`/#/spaces/${current.spaceId}/issues?view=board`)
  await expect(page.getByRole("region", { name: /^Done\b/ })).toBeVisible()
  await expect(page.locator(".issue-board .page-activity__empty", { hasText: "Loading…" })).toHaveCount(0)

  const results = await scan(page, ".issues-page__controls, .issue-board")
  expect(results.violations, describeViolations(results.violations)).toEqual([])
})
