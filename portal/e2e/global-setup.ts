import { chromium, type Browser } from "@playwright/test"
import { mkdir } from "node:fs/promises"
import { dirname } from "node:path"

export const ADMIN_STATE = "./e2e/.auth/state.json"
export const MEMBER_STATE = "./e2e/.auth/member.json"

/**
 * Sign in once per role and save the sessions for every spec.
 *
 * Two accounts, because a role-specific view can only be proved by someone who
 * does not have the role: the deployment administrator sees the admin area, and
 * an ordinary account must not. One session could show the first half and never
 * the second.
 *
 * The login code is single-use, so this is also why the login path is not a
 * separate spec: it runs here, and a break in it fails the whole suite before
 * the first test — loudly, and with a screenshot.
 *
 * `./make e2e` issues both codes and passes them in; a login code cannot be
 * fetched from the browser, since the whole point is that it arrives out of
 * band.
 */
async function globalSetup() {
  const baseURL = process.env.BUILDMAX_E2E_BASE_URL ?? "http://localhost:8080"
  const admin = credentials("BUILDMAX_E2E_EMAIL", "BUILDMAX_E2E_LOGIN_CODE")
  const member = credentials("BUILDMAX_E2E_MEMBER_EMAIL", "BUILDMAX_E2E_MEMBER_LOGIN_CODE")

  await mkdir(dirname(ADMIN_STATE), { recursive: true })
  const browser = await chromium.launch()
  try {
    await signIn(browser, baseURL, admin.email, admin.code, ADMIN_STATE)
    await signIn(browser, baseURL, member.email, member.code, MEMBER_STATE)
  } finally {
    await browser.close()
  }
}

function credentials(emailVar: string, codeVar: string): { email: string; code: string } {
  const email = process.env[emailVar]
  const code = process.env[codeVar]
  if (!email || !code) {
    throw new Error(
      `${emailVar} and ${codeVar} are required. Run these through \`./make e2e\`, which issues a code against the running deployment.`
    )
  }
  return { email, code }
}

async function signIn(browser: Browser, baseURL: string, email: string, code: string, statePath: string): Promise<void> {
  const context = await browser.newContext({ baseURL })
  const page = await context.newPage()
  try {
    await page.goto("/")
    // The form opens on password. A login code is the recovery path, which is
    // what an out-of-band code is for and the only credential a test can be
    // handed without also owning an account's password.
    await page.getByRole("button", { name: /login code/i }).click()
    await page.getByLabel("Email").fill(email)
    await page.getByLabel("Login code").fill(code)
    // Tie the wait to the login request, not a downstream UI side effect. A
    // rejected code or an errored call otherwise surfaced only as a blind wait
    // for the card to disappear, timing out with no hint of why — which is what
    // made a login hiccup an undiagnosable flake before the whole suite. Waiting
    // for the response fails fast and loud with the status and body, and confirms
    // success before asserting on the card. exact: the form also carries a
    // "Sign in with a password" link a substring match would also find.
    const [response] = await Promise.all([
      page.waitForResponse(
        (r) => r.url().includes("/api/auth/portal/login") && r.request().method() === "POST",
        { timeout: 30_000 }
      ),
      page.getByRole("button", { name: "Sign in", exact: true }).click(),
    ])
    if (!response.ok()) {
      throw new Error(
        `sign-in for ${email} failed: POST /api/auth/portal/login returned ${response.status()} ${await response.text()}`
      )
    }
    // Signed in: the token lands in context and the login card unmounts.
    // Asserting on its absence rather than on a URL keeps this working if the
    // post-login landing route moves.
    await page.locator(".login-page__card").waitFor({ state: "detached", timeout: 30_000 })
    await context.storageState({ path: statePath })
  } finally {
    await context.close()
  }
}

export default globalSetup
