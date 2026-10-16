import { expect, test } from "@playwright/test"

import { createSpace, postJSON, reportLeftovers, session, tagged } from "./fixtures"
import { MEMBER_STATE } from "./global-setup"

/**
 * docs/design/portal-navigation-and-space-context.md requires unknown,
 * forbidden, and missing routes to be distinguishable and actionable --
 * never a silent fallback to Chat. Confirmed against the server
 * (internal/service/{agent,issue}/service.go and friends) that a
 * resource-by-id lookup returns a genuine 404 once the caller has already
 * passed the Space-level guard, so Issue detail's not-found state below is a
 * real 404, not a guess.
 */

// A syntactically plausible but never-issued public id: no account is ever a
// member of it, so every Space-scoped fetch against it hits the same 403 the
// server returns uniformly for "doesn't exist" and "not yours" -- see
// internal/server/access/space.go's SpaceAction.
const NO_SUCH_SPACE = "aaaaaaaaaaaaaaaaaaaa"
const NO_SUCH_ISSUE = "bbbbbbbbbbbbbbbbbbbb"

test("an unrecognized hash renders not-found, never Chat", async ({ page }) => {
  // Ensures the account's Spaces are resolved before navigating to the bad
  // hash, so "Back to Issues" below has a real Space id to land on rather than
  // racing SpaceContext's own first load.
  await session(page)
  await page.goto("/#/this/does/not/exist")
  await expect(page.getByRole("heading", { name: "Page not found" })).toBeVisible()
  await expect(page.getByRole("textbox", { name: "What would you like to do?" })).toHaveCount(0)

  // The one safe action works and lands on a real Space's default destination.
  await page.getByRole("button", { name: "Back to Issues" }).click()
  await expect(page).toHaveURL(/#\/spaces\/[^/]+\/issues$/)
})

test("an incomplete Space-scoped path renders not-found", async ({ page }) => {
  const current = await session(page)
  await page.goto(`/#/spaces/${current.spaceId}/workflow-runs`)
  await expect(page.getByRole("heading", { name: "Page not found" })).toBeVisible()
})

test("a Issue id that never existed renders not-found, not Chat", async ({ page }) => {
  const current = await session(page)
  await page.goto(`/#/spaces/${current.spaceId}/issues/${NO_SUCH_ISSUE}`)
  await expect(page.getByRole("heading", { name: "Issue", exact: true })).toBeVisible()
  await expect(page.getByText(/No issue with this reference exists/)).toBeVisible()

  await page.getByRole("button", { name: "Back to Issues" }).click()
  await expect(page).toHaveURL(new RegExp(`#/spaces/${current.spaceId}/issues$`))
})

test("a Space no account is a member of renders forbidden, not a blank page", async ({ page }) => {
  await session(page)
  await page.goto(`/#/spaces/${NO_SUCH_SPACE}/issues/${NO_SUCH_ISSUE}`)
  await expect(page.getByRole("heading", { name: "Issue", exact: true })).toBeVisible()
  await expect(page.getByText(/you don't have access to/i)).toBeVisible()
})

test("a real Space owned by another account renders forbidden, proving the membership boundary", async ({ page, browser }) => {
  // The synthetic-id test above cannot tell a working guard from an id that
  // exists nowhere: both resolve against an empty member list. This drives the
  // other branch of internal/server/access/space.go's SpaceAction -- a Space
  // whose member list holds *other* accounts but not the caller -- which only a
  // second real account against a real, populated foreign Space can reach. It
  // is the case where kind's real multi-user authorization earns its keep over
  // a synthetic 403 any backend would return.
  const admin = await session(page)
  const foreign = await createSpace(page, admin, tagged("Foreign space probe"))
  const issue = await postJSON<{ id: string }>(page, `${admin.apiBase}/api/spaces/${foreign.id}/issues`, admin, {
    title: tagged("Foreign issue"),
  })
  reportLeftovers(foreign.id, [`space ${foreign.id}`, `issue ${issue.id}`])

  // A second context signed in as the member account: the default `page` holds
  // the admin session, and only a browser that is genuinely not in the Space
  // can show what its owner's neighbour meets. `browser.newContext` does not
  // inherit the project's baseURL, so take the Portal origin from the admin
  // page rather than restating the deployment's URL here.
  const portalBase = new URL(page.url()).origin
  const memberContext = await browser.newContext({ storageState: MEMBER_STATE, baseURL: portalBase })
  try {
    const memberPage = await memberContext.newPage()
    await memberPage.goto(`/#/spaces/${foreign.id}/issues/${issue.id}`)
    await expect(memberPage.getByRole("heading", { name: "Issue", exact: true })).toBeVisible()
    await expect(memberPage.getByText(/you don't have access to/i)).toBeVisible()
  } finally {
    await memberContext.close()
  }
})
