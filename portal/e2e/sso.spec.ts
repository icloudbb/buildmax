import { expect, request, test } from "@playwright/test"

// Single sign-on next to password and login-code sign-in, in a real browser.
// The kind smoke already walks the same flow over HTTP; what only a browser
// shows is the sign-in page offering both, the cross-site round trip through
// the IdP setting the Portal's cookie, and the Portal hydrating a session from
// it on load.
//
// It drives the in-cluster mock provider kind deploys (deployment/smoke/
// mock-oidc). That provider's issuer is its cluster DNS name, which the browser
// is told resolves to this machine; its certificate is kind's own CA. A
// deployment without SSO, or one pointed at a real IdP, skips these tests.

const MOCK_IDP_HOST = "buildmax-smoke-oidc.buildmax.svc.cluster.local"

test.use({
  storageState: { cookies: [], origins: [] },
  ignoreHTTPSErrors: true,
  launchOptions: { args: [`--host-resolver-rules=MAP ${MOCK_IDP_HOST} 127.0.0.1`] },
})

test.beforeAll(async ({ baseURL }) => {
  const api = await request.newContext({ baseURL })
  try {
    const methods = await (await api.get("/api/auth/methods")).json()
    test.skip(methods.oidc?.enabled !== true, "this deployment does not offer SSO")
    const start = await api.get("/api/auth/oidc/start", { maxRedirects: 0 })
    const location = new URL(start.headers()["location"] ?? "", baseURL)
    test.skip(location.hostname !== MOCK_IDP_HOST, `SSO goes to ${location.hostname}, not the mock provider`)
  } finally {
    await api.dispose()
  }
})

function uniqueEmail(domain: string): string {
  return `sso-e2e-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}@${domain}`
}

test("the sign-in page offers SSO and native sign-in together", async ({ page }) => {
  await page.goto("/")
  await expect(page.getByRole("button", { name: "Sign in with Mock IdP" })).toBeVisible()
  await expect(page.getByLabel("Email")).toBeVisible()
  await expect(page.getByLabel("Password")).toBeVisible()
  await expect(page.getByRole("button", { name: /login code/i })).toBeVisible()
})

test("signing in through the IdP lands in the Portal and survives a reload", async ({ page, baseURL }) => {
  await page.goto("/")
  await page.getByRole("button", { name: "Sign in with Mock IdP" }).click()

  await expect(page.getByRole("heading", { name: "Mock IdP" })).toBeVisible()
  await page.getByLabel("Email", { exact: true }).fill(uniqueEmail("buildmax.local"))
  await page.getByLabel("Name").fill("SSO E2E")
  await page.getByRole("button", { name: "Sign in", exact: true }).click()

  await page.waitForURL((url) => url.origin === new URL(baseURL!).origin)
  // Signed in as the person the IdP named, not merely off the sign-in page.
  await expect(page.getByRole("button", { name: "User menu" })).toContainText("SSO E2E")
  await expect(page.locator(".login-page__card")).toHaveCount(0)

  // The session is the refresh cookie the callback set, not page state.
  await page.reload()
  await expect(page.getByRole("button", { name: "User menu" })).toContainText("SSO E2E")
  await expect(page.locator(".login-page__card")).toHaveCount(0)
})

test("an identity outside the allowed domains is refused with a reason", async ({ page }) => {
  await page.goto("/")
  await page.getByRole("button", { name: "Sign in with Mock IdP" }).click()
  await page.getByLabel("Email", { exact: true }).fill(uniqueEmail("outside.example"))
  await page.getByRole("button", { name: "Sign in", exact: true }).click()

  await expect(page.getByRole("alert")).toHaveText(/not authorized for this deployment/)
  await expect(page.getByRole("button", { name: "Sign in with Mock IdP" })).toBeVisible()
})

test("declining at the IdP returns to sign-in, which still works natively", async ({ page }) => {
  await page.goto("/")
  await page.getByRole("button", { name: "Sign in with Mock IdP" }).click()
  await page.getByRole("button", { name: "Deny" }).click()

  await expect(page.getByRole("alert")).toHaveText(/not authorized for this deployment/)
  await expect(page.getByLabel("Email")).toBeVisible()
  await expect(page.getByLabel("Password")).toBeVisible()
})
