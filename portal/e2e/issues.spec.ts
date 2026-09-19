import { expect, test } from "@playwright/test"

import { postJSON, reportLeftovers, session, tagged } from "./fixtures"

/**
 * Issue Detail is organized into four areas -- Overview, Discussion, Results,
 * Runs -- so the page answers "what is being done" without also rendering
 * every parallel execution summary at once. That only shows up in the
 * browser: a handler test can prove the API never starts a run on save, but
 * only a real render proves the tabs actually swap content instead of
 * stacking it.
 */

test("Issue Detail organizes into Overview, Discussion, Results, and Runs tabs", async ({ page }) => {
  const current = await session(page)
  const title = tagged("Issue detail tabs probe")
  const issue = await postJSON<{ id: string }>(page, `${current.space}/issues`, current, {
    title,
    description: "Created by the Portal browser tests to exercise Issue Detail's tabs.",
  })
  reportLeftovers(current.spaceId, [`issue ${issue.id}`])

  await page.goto(`/#/spaces/${current.spaceId}/issues/${issue.id}`)
  await expect(page.getByRole("heading", { name: title, level: 1 })).toBeVisible()

  // Overview opens in read mode. Editing is a deliberate action; the latest
  // outcome is available before any form fields.
  const tabs = page.getByRole("navigation", { name: "Issue sections" })
  await expect(tabs.getByRole("button", { name: "Overview" })).toHaveAttribute("aria-current", "true")
  const titleField = page.getByLabel("Title")
  await expect(titleField).toHaveCount(0)
  await expect(page.getByText("No result yet.")).toBeVisible()
  await expect(page.getByRole("heading", { name: "Latest Outcome", exact: true })).toBeVisible()
  await expect(page.getByText("No execution runs recorded for this issue yet.")).toBeVisible()
  await expect(page.getByRole("heading", { name: "Discussion", exact: true })).toHaveCount(0)
  await expect(page.getByRole("heading", { name: "Results", exact: true })).toHaveCount(0)
  await expect(page.getByRole("heading", { name: "Run History", exact: true })).toHaveCount(0)

  // Each tab replaces the page's content rather than adding to it -- the
  // previous tab's section headings are gone, not just scrolled past.
  await tabs.getByRole("button", { name: "Discussion" }).click()
  await expect(page.getByRole("heading", { name: "Discussion", exact: true })).toBeVisible()
  await expect(page.getByLabel("Title")).toHaveCount(0)
  await expect(page.getByRole("heading", { name: "Latest Outcome", exact: true })).toHaveCount(0)

  await tabs.getByRole("button", { name: "Results" }).click()
  await expect(page.getByRole("heading", { name: "Results", exact: true })).toBeVisible()
  await expect(page.getByRole("heading", { name: "Discussion", exact: true })).toHaveCount(0)

  await tabs.getByRole("button", { name: "Runs" }).click()
  await expect(page.getByRole("heading", { name: "Run History", exact: true })).toBeVisible()
  await expect(page.getByRole("heading", { name: "Agent Run Sequence", exact: true })).toBeVisible()
  await expect(page.getByRole("heading", { name: "Results", exact: true })).toHaveCount(0)

  await tabs.getByRole("button", { name: "Overview" }).click()
  await page.getByRole("button", { name: "Edit issue" }).click()
  await expect(titleField).toHaveValue(title)
})

test("saving an Issue never starts a run", async ({ page }) => {
  const current = await session(page)
  const title = tagged("Issue save no-run probe")
  const issue = await postJSON<{ id: string }>(page, `${current.space}/issues`, current, {
    title,
    description: "Created by the Portal browser tests to prove Save never schedules work.",
  })
  reportLeftovers(current.spaceId, [`issue ${issue.id}`])

  await page.goto(`/#/spaces/${current.spaceId}/issues/${issue.id}`)
  await page.getByRole("button", { name: "Edit issue" }).click()
  const titleField = page.getByLabel("Title")
  await expect(titleField).toHaveValue(title)

  const description = page.getByLabel("Description")
  await description.fill("Edited by the Portal browser tests.")
  await page.getByRole("button", { name: "Save changes" }).click()
  await expect(page.getByText("Saved.", { exact: true })).toBeVisible()

  // Reloading re-fetches from the API rather than trusting the form's own
  // state, so this proves the edit actually persisted server-side.
  await page.reload()
  await page.getByRole("button", { name: "Edit issue" }).click()
  await expect(description).toHaveValue("Edited by the Portal browser tests.")
  await page.getByRole("button", { name: "Cancel" }).click()

  // And it never scheduled a run: the Runs tab is still empty.
  const tabs = page.getByRole("navigation", { name: "Issue sections" })
  await tabs.getByRole("button", { name: "Runs" }).click()
  await expect(page.getByText("No runs yet.")).toBeVisible()
  await expect(page.getByText("No agent runs recorded for this issue yet.")).toBeVisible()
})
