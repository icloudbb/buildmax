import { expect, test, type Route } from "@playwright/test"

import { session } from "./fixtures"

/**
 * The failure half of the resource-state model in
 * docs/design/portal-state-and-permission-feedback.md.
 *
 * Every other spec attaches to a real deployment and proves it reports itself
 * truthfully. That contract cannot reach `error` (5xx / network), `stale` (a
 * refresh that fails after a prior success), or permission `failed` (the role
 * lookup itself errored): a healthy backend does not produce them on demand,
 * and the deterministic mock only scripts inference, not the Portal's resource
 * fetches. So this is the one spec that intercepts responses with `page.route`.
 * It tests how Portal *presents* a failing dependency, not the deployment --
 * the states its unit tests derive (state/resourceState.test.ts) and no
 * integration test until now exercised through a real page.
 *
 * The audit trail is the subject because it renders the whole vocabulary --
 * Alert error/stale plus the permission `failed` hint (features/audit/
 * SpaceAuditSection.tsx) -- from one owner-only section, and it is where the
 * e2e assessment saw an unexplained load hang. These tests create no server
 * data: the responses are faked, so nothing is left behind.
 */

const AUDIT_EVENTS = /\/api\/spaces\/[^/]+\/audit-events(\?|$)/
const SPACE_MEMBERS = /\/api\/spaces\/[^/]+\/members(\?|$)/
const ISSUE_CREATE = /\/api\/spaces\/[^/]+\/issues$/
const ISSUE_PATCH = /\/api\/spaces\/[^/]+\/issues\/e2e-created-issue$/

function fail(route: Route): Promise<void> {
  return route.fulfill({
    status: 500,
    contentType: "application/json",
    body: JSON.stringify({ error: "injected failure" }),
  })
}

test("a 5xx on the audit fetch renders the error state, not a silent blank", async ({ page }) => {
  await page.route(AUDIT_EVENTS, fail)

  const current = await session(page)
  await page.goto(`/#/spaces/${current.spaceId}/settings/audit`)

  await expect(page.getByRole("heading", { name: "Audit trail" })).toBeVisible()
  const alert = page.locator(".state-alert--error")
  await expect(alert.getByText("Something went wrong")).toBeVisible()
  // Recoverable failures offer a retry; the list is absent rather than empty.
  await expect(alert.getByRole("button", { name: "Retry" })).toBeVisible()
  await expect(page.locator(".audit-list")).toHaveCount(0)
})

test("a failed refresh after a good load reads as stale, keeping the prior rows", async ({ page }) => {
  // The subtle branch of deriveResourceState: prior data plus a non-forbidden,
  // non-notFound error is Stale, never Error -- old rows stay, with a warning
  // over them. The first page carries total > returned so "Show older" is
  // present to trigger the second, failing fetch within the same mount.
  let calls = 0
  await page.route(AUDIT_EVENTS, async (route) => {
    calls += 1
    if (calls === 1) {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          events: [
            {
              id: "aud_e2e_stale",
              actor_type: "system",
              actor_id: "buildmax",
              action: "user.login",
              target_id: "web-e2e",
              created_at: "2026-09-09T00:00:00Z",
            },
          ],
          total: 2,
        }),
      })
      return
    }
    await fail(route)
  })

  const current = await session(page)
  await page.goto(`/#/spaces/${current.spaceId}/settings/audit`)

  const row = page.locator(".audit-row").filter({ hasText: "Signed in from web-e2e" })
  await expect(row).toBeVisible()
  const showOlder = page.getByRole("button", { name: /Show older/ })
  await expect(showOlder).toBeVisible()

  await showOlder.click()

  // Stale is a persistent warning over current content, so it is role="status".
  const stale = page.locator(".state-alert--stale")
  await expect(stale.getByText("Showing previous data")).toBeVisible()
  // The failed refresh must not wipe what was already on screen.
  await expect(row).toBeVisible()
})

test("a failed role lookup renders permission 'failed', never a silent denial", async ({ page }) => {
  // failed must never read as denied: the reader might be an owner the server
  // could not confirm. The audit section says so and offers a refresh, rather
  // than the flat "only an owner can read this" it shows a confirmed non-owner.
  await page.route(SPACE_MEMBERS, fail)

  const current = await session(page)
  await page.goto(`/#/spaces/${current.spaceId}/settings/audit`)

  await expect(page.getByRole("heading", { name: "Audit trail" })).toBeVisible()
  await expect(page.getByText(/Couldn't verify your role in this space/)).toBeVisible()
})

test("a failed Issue setup after creation names the created object and prevents a duplicate", async ({ page }) => {
  const current = await session(page)
  let creates = 0
  await page.route(ISSUE_CREATE, async (route) => {
    if (route.request().method() !== "POST") return route.continue()
    creates += 1
    await route.fulfill({
      status: 201,
      contentType: "application/json",
      body: JSON.stringify({ id: "e2e-created-issue", version: 1 }),
    })
  })
  await page.route(ISSUE_PATCH, async (route) => {
    if (route.request().method() !== "PATCH") return route.continue()
    await fail(route)
  })

  await page.goto(`/#/spaces/${current.spaceId}/issues`)
  await page.getByRole("button", { name: "New Issue" }).click()
  const dialog = page.getByRole("dialog", { name: "New Issue" })
  await dialog.getByLabel("Title").fill("Partial setup probe")
  await dialog.getByLabel("Status").selectOption("in_progress")
  await dialog.getByRole("button", { name: "Create issue" }).click()

  await expect(dialog.getByRole("alert")).toContainText("Issue was created, but its details were not saved")
  await expect(dialog.getByRole("button", { name: "Create issue" })).toHaveCount(0)
  await expect(dialog.getByRole("button", { name: "Open created issue" })).toBeVisible()
  expect(creates).toBe(1)
})
