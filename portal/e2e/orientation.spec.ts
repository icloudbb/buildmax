import { expect, test, type Page } from "@playwright/test"

import { getJSON, postJSON, reportLeftovers, session, tagged, type Session } from "./fixtures"

/**
 * docs/design/portal-navigation-and-space-context.md's Slice 5 (orientation
 * polish): the browser tab title and the narrow compact header are the two
 * surfaces in-page chrome cannot cover -- a person with several Spaces open
 * across tabs, or on a phone-width viewport, tells them apart by these alone.
 */

/** There is no GET /api/spaces/{id} -- only the caller's own membership list. */
async function currentSpace(page: Page, current: Session): Promise<{ name: string }> {
  const spaces = await getJSON<{ id: string; name: string }[]>(page, `${current.apiBase}/api/spaces`, current)
  const space = spaces.find((s) => s.id === current.spaceId)
  if (!space) throw new Error(`session Space ${current.spaceId} missing from GET /api/spaces`)
  return space
}

test("the document title names the page and the Space for a Space-scoped route", async ({ page }) => {
  const current = await session(page)
  const space = await currentSpace(page, current)

  await page.goto(`/#/spaces/${current.spaceId}/issues`)
  await expect(page).toHaveTitle(new RegExp(`^Issues · ${space.name} · BuildMax$`))

  await page.goto(`/#/spaces/${current.spaceId}/agents`)
  await expect(page).toHaveTitle(new RegExp(`^Agents · ${space.name} · BuildMax$`))
})

test("the document title omits the Space for a global route", async ({ page }) => {
  await session(page)
  await page.goto("/#/account")
  await expect(page).toHaveTitle(/^General · BuildMax$/)
})

test("the document title names the not-found page", async ({ page }) => {
  await session(page)
  await page.goto("/#/this/does/not/exist")
  await expect(page).toHaveTitle(/^Page not found · BuildMax$/)
})

test("browser back and forward restore the route and the title", async ({ page }) => {
  const current = await session(page)
  await page.goto(`/#/spaces/${current.spaceId}/issues`)
  await page.goto(`/#/spaces/${current.spaceId}/agents`)

  await page.goBack()
  await expect(page).toHaveURL(new RegExp(`#/spaces/${current.spaceId}/issues$`))
  await expect(page.getByRole("heading", { name: "Issues", exact: true })).toBeVisible()

  await page.goForward()
  await expect(page).toHaveURL(new RegExp(`#/spaces/${current.spaceId}/agents$`))
  await expect(page.getByRole("heading", { name: "Agents", exact: true })).toBeVisible()
})

test("the narrow compact header names the Space and leaves the page's name to its heading", async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  const current = await session(page)
  const space = await currentSpace(page, current)

  await page.goto(`/#/spaces/${current.spaceId}/issues`)
  const header = page.locator(".shell__compact-header")
  await expect(header.locator(".shell__compact-space")).toHaveText(space.name)
  await expect(page.getByRole("heading", { name: "Issues", level: 1 })).toBeVisible()
  await expect(page.getByText("Issues", { exact: true }).filter({ visible: true })).toHaveCount(1)
})

// docs/design/portal-frontend-page-system.md: the shell states scope once and
// the page owns one real H1. The breadcrumb is the way back to the parent, so a
// detail page carries no second back link beside it.
test("Chat and Issue Detail state their title once at every width", async ({ page }) => {
  const current = await session(page)
  const title = tagged("Title stated once probe")
  const issue = await postJSON<{ id: string }>(page, `${current.space}/issues`, current, {
    title,
    description: "Created by the Portal browser tests to check page headings.",
  })
  reportLeftovers(current.spaceId, [`issue ${issue.id}`])
  // The sidebar names destinations, not the page; everything else that could
  // repeat the title is the compact header or the main column.
  const orientation = page.locator(".shell__compact-header, .shell__main")

  for (const width of [390, 768, 1280]) {
    await page.setViewportSize({ width, height: 900 })

    await page.goto(`/#/spaces/${current.spaceId}/chat`)
    await expect(page.getByRole("heading", { name: "Chat", level: 1 })).toBeVisible()
    await expect(orientation.getByText("Chat", { exact: true }).filter({ visible: true }), `Chat at ${width}px`).toHaveCount(1)

    await page.goto(`/#/spaces/${current.spaceId}/issues/${issue.id}`)
    await expect(page.getByRole("heading", { name: title, level: 1 })).toBeVisible()
    await expect(orientation.getByText(title, { exact: true }).filter({ visible: true }), `Issue at ${width}px`).toHaveCount(1)
    await expect(page.getByLabel("Breadcrumb").getByRole("link", { name: "Issues" })).toBeVisible()
    await expect(page.getByRole("link", { name: "Back to Issues" })).toHaveCount(0)
  }
})
