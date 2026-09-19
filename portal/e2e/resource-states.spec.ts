import { expect, test, type Route } from "@playwright/test"

import { postJSON, reportLeftovers, session } from "./fixtures"

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
 * The audit trail is one subject because it renders the whole vocabulary --
 * Alert error/stale plus the permission `failed` hint (features/audit/
 * SpaceAuditSection.tsx) -- from one owner-only section, and it is where the
 * e2e assessment saw an unexplained load hang. Conversation cases create a
 * disposable conversation and intercept its task responses to exercise failures.
 */

const AUDIT_EVENTS = /\/api\/spaces\/[^/]+\/audit-events(\?|$)/
const SPACE_MEMBERS = /\/api\/spaces\/[^/]+\/members(\?|$)/
const ISSUE_CREATE = /\/api\/spaces\/[^/]+\/issues$/

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

test("a new Issue's details travel with the create, so a refusal leaves nothing to finish", async ({ page }) => {
  const current = await session(page)
  const bodies: Array<Record<string, unknown>> = []
  await page.route(ISSUE_CREATE, async (route) => {
    if (route.request().method() !== "POST") return route.continue()
    bodies.push(route.request().postDataJSON())
    await fail(route)
  })

  await page.goto(`/#/spaces/${current.spaceId}/issues`)
  await page.getByRole("button", { name: "New Issue" }).click()
  const dialog = page.getByRole("dialog", { name: "New Issue" })
  await dialog.getByLabel("Title").fill("One request probe")
  await dialog.getByLabel("Status").selectOption("in_progress")
  await dialog.getByRole("button", { name: "Create issue" }).click()

  // Nothing was created, so the dialog keeps Create rather than pointing at a
  // half-configured Issue.
  await expect(dialog.getByRole("alert")).toBeVisible()
  await expect(dialog.getByRole("button", { name: "Create issue" })).toBeEnabled()
  expect(bodies).toHaveLength(1)
  expect(bodies[0]).toMatchObject({ title: "One request probe", status: "in_progress" })
})

test("a failed conversation Task retry remains visible after the button stops being busy", async ({ page }) => {
  const current = await session(page)
  const conversation = await postJSON<{ conversation_id: string }>(
    page, `${current.space}/conversations`, current, { channel: "portal" }
  )
  reportLeftovers(current.spaceId, [`conversation ${conversation.conversation_id}`])
  const taskId = "e2e-card-task"
  await page.route(new RegExp(`/conversations/${conversation.conversation_id}/tasks$`), (route) => route.fulfill({
    status: 200,
    contentType: "application/json",
    body: JSON.stringify([{
      id: taskId,
      title: "Retry feedback probe",
      input: "Probe",
      output: "Finished output",
      status: "SUCCEEDED",
      created_at: new Date().toISOString(),
    }]),
  }))
  let attempts = 0
  await page.route(new RegExp(`/tasks/${taskId}/retry$`), async (route) => {
    attempts += 1
    await fail(route)
  })

  await page.goto(`/#/spaces/${current.spaceId}/chat/${conversation.conversation_id}`)
  const card = page.locator(".task-card").filter({ hasText: "Retry feedback probe" })
  await expect(card).toBeVisible()
  const retry = card.getByRole("button", { name: "Run again" })
  await retry.click()
  await expect(retry).toBeEnabled()
  await expect(card.getByText("injected failure")).toBeVisible()
  expect(attempts).toBe(1)
})

test("a failed conversation Task list offers a retry without hiding the conversation", async ({ page }) => {
  const current = await session(page)
  const conversation = await postJSON<{ conversation_id: string }>(
    page, `${current.space}/conversations`, current, { channel: "portal" }
  )
  reportLeftovers(current.spaceId, [`conversation ${conversation.conversation_id}`])
  let calls = 0
  let recover = false
  await page.route(new RegExp(`/conversations/${conversation.conversation_id}/tasks$`), async (route) => {
    calls += 1
    if (!recover) return fail(route)
    await route.fulfill({ status: 200, contentType: "application/json", body: "[]" })
  })

  await page.goto(`/#/spaces/${current.spaceId}/chat/${conversation.conversation_id}`)
  const alert = page.locator(".state-alert--error")
  await expect(alert).toContainText("Background tasks: injected failure")
  await expect(page.getByRole("textbox", { name: "Message" })).toBeVisible()
  const callsBeforeRetry = calls
  recover = true
  await alert.getByRole("button", { name: "Retry tasks" }).click()
  await expect(alert).toHaveCount(0)
  expect(calls).toBeGreaterThan(callsBeforeRetry)
})
