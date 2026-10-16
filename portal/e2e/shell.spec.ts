import { expect, test } from "@playwright/test"

// What only a browser proves: the published bundle works against a real
// server. The Portal image ships one bundle for every deployment and reads its
// API base at container start, so a wrong or missing runtime config produces a
// page that renders and then fails every request — invisible to a build, and
// invisible to the API-level smoke.

test("the signed-in shell renders against a real deployment", async ({ page }) => {
  await page.goto("/")

  // The login card is what an unauthenticated or misconfigured app falls back
  // to, so its absence is half the assertion.
  await expect(page.locator(".login-page__card")).toHaveCount(0)
  // The other half: the shell actually rendered. Navigation is asserted rather
  // than the wordmark, which is ASCII art and carries no text to match.
  for (const item of ["Issues", "Workflows", "Agents", "Artifacts"]) {
    await expect(page.getByRole("link", { name: item }).first()).toBeVisible()
  }
})

// The sidebar behaves like web navigation: each destination is an address that
// opens in a new tab or can be copied, and the current one is announced, not
// only drawn.
test("the sidebar is links to the hash routes, with exactly one current", async ({ page }) => {
  await page.goto("/")
  const nav = page.getByLabel("Sidebar", { exact: true }).getByRole("navigation", { name: "Primary" })
  const issues = nav.getByRole("link", { name: "Issues" })
  await expect(issues).toHaveAttribute("aria-current", "page")
  await expect(nav.locator('[aria-current="page"]')).toHaveCount(1)

  const href = await nav.getByRole("link", { name: "Workflows" }).getAttribute("href")
  expect(href).toMatch(/^#\/spaces\/[^/]+\/workflows$/)
  await nav.getByRole("link", { name: "Workflows" }).click()
  await expect(page).toHaveURL(new RegExp(`${href}$`))
  await expect(nav.getByRole("link", { name: "Workflows" })).toHaveAttribute("aria-current", "page")
  await expect(issues).not.toHaveAttribute("aria-current", "page")
  await expect(nav.locator('[aria-current="page"]')).toHaveCount(1)
})

test("the runtime API base is the one this deployment serves", async ({ page }) => {
  const config = await page.request.get("/config.js")
  expect(config.ok()).toBeTruthy()
  // The published image ships one bundle for every deployment and learns its
  // API base at container start, so a wrong value here produces a page that
  // renders and then fails every request.
  //
  // What "right" means is the deployment's to say, not this spec's. The kind
  // reference puts one ingress in front of Portal and server, so the base is
  // same-origin; the Compose quickstart publishes them on separate ports, so it
  // is absolute. `./make e2e` passes in whichever it just pointed the browser
  // at, and the default keeps a hand-run suite honest about the reference.
  const expected = process.env.BUILDMAX_E2E_API_BASE ?? "/"
  expect(await config.text()).toContain(`apiBase: "${expected}"`)
})

test("a reload keeps the session", async ({ page }) => {
  await page.goto("/")
  await page.reload()
  // Session restoration lives entirely in the browser, so nothing below the UI
  // can test it.
  await expect(page.locator(".login-page__card")).toHaveCount(0)
})
