import { expect, test } from "@playwright/test"

// The administration area is deployment-scoped and reachable only through the
// UI, so this is the first place its wiring runs end to end: hash route,
// /api/admin authorization, and rendering. `./make e2e` grants the test account
// system_admin, which is what makes these reachable at all.

test("an administrator can open the deployment overview by URL", async ({ page }) => {
  await page.goto("/#/admin")

  await expect(page.getByRole("heading", { name: "Administration" })).toBeVisible()
  await expect(page.getByLabel("Sidebar").getByLabel("Current scope")).toHaveText("Deployment")
  await expect(page.getByLabel("Sidebar").getByRole("combobox")).toHaveCount(0)
  await expect(page.getByLabel("Sidebar").getByRole("button", { name: "Back to space" })).toBeVisible()
  await expect(page.getByRole("heading", { name: "Health" })).toBeVisible()

  // The status is answered rather than falling into the error branch. Whether
  // the deployment is ready is not this test's business — that it could say is.
  await expect(page.locator(".admin-pill").first()).toBeVisible()
  await expect(page.locator(".settings-section__error")).toHaveCount(0)
})

test("each administration section is linkable and survives a reload", async ({ page }) => {
  for (const [path, heading] of [
    ["/#/admin/administrators", "Administrators"],
    ["/#/admin/accounts", "Accounts"],
    ["/#/admin/spaces", "Spaces"],
    ["/#/admin/models", "Models"],
    ["/#/admin/audit", "Audit trail"],
  ] as const) {
    await page.goto(path)
    await expect(page.getByRole("heading", { name: heading })).toBeVisible()
    await page.reload()
    await expect(page.getByRole("heading", { name: heading })).toBeVisible()
    await expect(page.locator(".settings-section__error")).toHaveCount(0)
  }
})

test("an administrator reaches administration from the first-level sidebar", async ({ page }) => {
  await page.goto("/")
  await page.getByRole("button", { name: "Administration" }).click()
  await expect(page.getByRole("heading", { name: "Administration" })).toBeVisible()

  // On the Deployment scope the sidebar lists the Administration sections
  // themselves, and choosing one moves between them without leaving the scope
  // or reintroducing a Space switcher.
  const sidebar = page.getByLabel("Sidebar")
  await sidebar.getByRole("button", { name: "Accounts" }).click()
  await expect(page).toHaveURL(/#\/admin\/accounts$/)
  await expect(page.getByRole("heading", { name: "Accounts" })).toBeVisible()
  await expect(sidebar.getByLabel("Current scope")).toHaveText("Deployment")
})

test("the Administrators section lists who can operate the deployment", async ({ page }) => {
  await page.goto("/#/admin/administrators")
  await expect(page.getByRole("heading", { name: "Administrators" })).toBeVisible()

  // `./make e2e` granted the test account, so the list is never empty and the
  // account carries an active grant.
  await expect(page.locator(".admin-list__row").first()).toBeVisible()
  await expect(page.locator(".admin-pill--ok").first()).toBeVisible()
  await expect(page.locator(".settings-section__error")).toHaveCount(0)
})

test("an account's detail lists its live sessions", async ({ page }) => {
  const email = process.env.BUILDMAX_E2E_EMAIL
  test.skip(!email, "BUILDMAX_E2E_EMAIL not set")

  await page.goto("/#/admin/accounts")
  await expect(page.getByRole("heading", { name: "Accounts" })).toBeVisible()

  // Open the signed-in operator's own account — it is guaranteed to have a live
  // session, the one this browser is signed in with. Revoking it is deliberately
  // not exercised here: it would sign the test out.
  await page.getByRole("button", { name: email! }).first().click()
  await expect(page.getByRole("heading", { name: "Sessions" })).toBeVisible()
  await expect(page.getByText("No live sessions")).toHaveCount(0)
  await expect(page.getByRole("button", { name: "Revoke" }).first()).toBeVisible()
  await expect(page.locator(".settings-section__error")).toHaveCount(0)
})

test("an open account detail is a linkable address that survives a reload", async ({ page }) => {
  const email = process.env.BUILDMAX_E2E_EMAIL
  test.skip(!email, "BUILDMAX_E2E_EMAIL not set")

  await page.goto("/#/admin/accounts")
  await page.getByRole("button", { name: email! }).first().click()
  await expect(page.getByRole("heading", { name: "Sessions" })).toBeVisible()
  // Opening the detail put the account in the URL, not just component state.
  await expect(page).toHaveURL(/#\/admin\/accounts\/.+/)

  await page.reload()
  await expect(page.getByRole("heading", { name: "Sessions" })).toBeVisible()
})

test("the last-login range filter narrows the account list", async ({ page }) => {
  const email = process.env.BUILDMAX_E2E_EMAIL
  test.skip(!email, "BUILDMAX_E2E_EMAIL not set")

  await page.goto("/#/admin/accounts")
  await expect(page.getByRole("button", { name: email! }).first()).toBeVisible()

  // Use a UTC date safely beyond the local/UTC day boundary. Adding one local
  // calendar day before converting to ISO can still produce today's UTC date
  // just after midnight in Singapore.
  const futureDay = new Date(Date.now() + 48 * 60 * 60 * 1000).toISOString().slice(0, 10)

  // Nobody can have signed in on this future date, so the whole list drops out. This is
  // what proves the date input is converted to a real instant and sent.
  await page.getByLabel("Signed in after").fill(futureDay)
  await expect(page.getByText("No accounts match")).toBeVisible()
  await expect(page.getByRole("button", { name: email! })).toHaveCount(0)

  // A bound well in the past keeps the operator, who just signed in.
  await page.getByLabel("Signed in after").fill("2000-01-01")
  await expect(page.getByRole("button", { name: email! }).first()).toBeVisible()
  await expect(page.locator(".settings-section__error")).toHaveCount(0)
})

test("creating an account walks the operator into issuing its login code", async ({ page }) => {
  await page.goto("/#/admin/accounts")
  await expect(page.getByRole("heading", { name: "Accounts" })).toBeVisible()

  const email = `joiner-${Date.now()}@example.com`
  await page.getByLabel("Email for the new account").fill(email)
  await page.getByRole("button", { name: "Create", exact: true }).click()

  // Creating lands on the new account's detail — where the credential is issued
  // — rather than leaving the operator to find it again. The account exists but
  // still cannot sign in, and the page says so.
  await expect(page).toHaveURL(/#\/admin\/accounts\/.+/)
  await expect(page.getByRole("heading", { name: email })).toBeVisible()
  await expect(page.getByText("cannot sign in yet")).toBeVisible()
  await expect(page.getByRole("button", { name: "Issue a login code" })).toBeVisible()
  await expect(page.locator(".settings-section__error")).toHaveCount(0)
})

test("disabling an account previews its impact before committing", async ({ page }) => {
  // A throwaway account so the disable does not touch the operator or the shared
  // fixtures. Created through the same UI the joiner test exercises.
  await page.goto("/#/admin/accounts")
  const email = `leaver-${Date.now()}@example.com`
  await page.getByLabel("Email for the new account").fill(email)
  await page.getByRole("button", { name: "Create", exact: true }).click()
  await expect(page.getByRole("heading", { name: email })).toBeVisible()

  // Disable opens the guided impact preview rather than committing immediately.
  await page.getByRole("button", { name: "Disable", exact: true }).click()
  const dialog = page.getByRole("dialog")
  await expect(dialog.getByText(`Disable ${email}?`)).toBeVisible()
  // The impact loads as counts, never content.
  await expect(dialog.locator(".admin-impact")).toBeVisible()

  await dialog.getByRole("button", { name: "Disable account" }).click()

  // The account is disabled and the outcome is reported; enabling is now offered.
  await expect(page.getByText(`${email} is disabled.`)).toBeVisible()
  await expect(page.getByRole("button", { name: "Enable" })).toBeVisible()
  await expect(page.locator(".settings-section__error")).toHaveCount(0)
})

test("an administrator can add a model through the Portal", async ({ page }) => {
  await page.goto("/#/admin/models")
  await expect(page.getByRole("heading", { name: "Add a model" })).toBeVisible()

  // A credential-free target (ollama needs no key), so this works whether or not
  // the deployment has an encryption key configured — the catalog row is what is
  // under test, not a live upstream.
  const name = `Portal Model ${Date.now()}`
  await page.getByLabel("Name", { exact: true }).fill(name)
  await page.getByLabel("Provider", { exact: true }).fill("ollama")
  await page.getByLabel("API URL").fill("http://ollama.test:11434/v1")
  await page.getByLabel("Provider model ID").fill("llama3")
  await page.getByRole("button", { name: "Add model" }).click()

  await expect(page.getByText(`Added ${name}`)).toBeVisible()
  await expect(page.locator(".settings-section__error")).toHaveCount(0)

  // It is really in the catalog, not just an optimistic message.
  await page.reload()
  await expect(page.locator(".admin-list__main").filter({ hasText: name })).toBeVisible()
})

test("a model's API key never appears on the page", async ({ page }) => {
  await page.goto("/#/admin/models")

  const secret = `sk-portal-leak-${Date.now()}`
  await page.getByLabel("Name", { exact: true }).fill(`Keyed Model ${Date.now()}`)
  await page.getByLabel("Provider", { exact: true }).fill("openai_compatible")
  await page.getByLabel("API URL").fill("https://api.example.test/v1")
  await page.getByLabel("Provider model ID").fill("vendor/model")
  await page.getByLabel("API key").fill(secret)
  await page.getByRole("button", { name: "Add model" }).click()

  // Whether the deployment accepted the model (it has an encryption key) or
  // refused it (it does not), the outcome is shown and the key is rendered
  // nowhere: on success the form is cleared, and a refusal names the missing key
  // config, never the credential.
  await expect(page.locator(".admin-notice, .settings-section__error")).toBeVisible()
  await expect(page.locator("body")).not.toContainText(secret)
})

test("the audit search reaches the events that have no space", async ({ page }) => {
  await page.goto("/#/admin/audit")
  await expect(page.getByRole("heading", { name: "Audit trail" })).toBeVisible()

  // Logins and grants are recorded with no space, so the space-scoped trail can
  // never return them. This deployment has at least the test account's own
  // login and grant, so the filter must find something.
  await page.getByRole("button", { name: "Deployment only" }).click()
  await expect(page.locator(".audit-row").first()).toBeVisible()
  await expect(page.locator(".settings-section__error")).toHaveCount(0)
})
