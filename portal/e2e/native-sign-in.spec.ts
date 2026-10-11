import { expect, request, test, type APIRequestContext, type Browser, type Page } from "@playwright/test"
import { session } from "./fixtures"
import { ADMIN_STATE } from "./global-setup"

// A CLI or Desktop sign-in confirmed in the Portal (design
// enterprise-identity-and-access §14), in a real browser: the client's half is
// driven through the API exactly as a client would, the person's half through
// the page.
//
// Opening the link signed out goes through the Portal's sign-in first and must
// land back on the same request: by password, which keeps the hash, and by
// SSO, which carries it through the signed OIDC transaction. The SSO case
// needs the mock provider kind deploys (deployment/smoke/mock-oidc) and skips
// elsewhere, as sso.spec.ts does.
//
// On kind, `./make e2e kind` forwards each server replica and passes them in
// BUILDMAX_E2E_SERVER_REPLICAS. Start, confirmation, and redemption are then
// each placed on a named replica, both ways round, which is the deployed
// evidence that the request's state is shared rather than held by the process
// that started it. Elsewhere every step goes to the deployment's one API.

const MOCK_IDP_HOST = "buildmax-smoke-oidc.buildmax.svc.cluster.local"
const MOCK_IDP_DOMAIN = "buildmax.local"

test.use({
  storageState: { cookies: [], origins: [] },
  ignoreHTTPSErrors: true,
  launchOptions: { args: [`--host-resolver-rules=MAP ${MOCK_IDP_HOST} 127.0.0.1`] },
})

interface Replica {
  name: string
  base: string
}

/** The kind replicas, or none: then every step uses the deployment's own API. */
const replicas: Replica[] = (process.env.BUILDMAX_E2E_SERVER_REPLICAS ?? "")
  .split(",")
  .filter(Boolean)
  .map((entry) => {
    const [name, base] = entry.split("=", 2)
    return { name, base }
  })

function apiOf(baseURL: string, replica: Replica | undefined): string {
  return replica?.base ?? new URL(baseURL).origin
}

interface Started {
  user_code: string
  poll_secret: string
  interval: number
  verification_uri_complete: string
  /** Client-side pacing: when this client last spoke to the server about it. */
  lastPollAt: number
}

async function start(api: APIRequestContext, base: string, platform: string, label: string): Promise<Started> {
  const res = await api.post(`${base}/api/auth/native-sign-ins`, { data: { platform, client_label: label } })
  expect(res.status(), `start on ${base}: ${await res.text()}`).toBe(200)
  return { ...((await res.json()) as Omit<Started, "lastPollAt">), lastPollAt: Date.now() }
}

/**
 * One poll, paced as a client paces it: no sooner than the interval after the
 * previous one, and five seconds slower after each slow_down.
 */
async function poll(api: APIRequestContext, base: string, started: Started): Promise<{ status: number; body: Record<string, unknown> }> {
  const due = started.lastPollAt + started.interval * 1000 + 250
  if (Date.now() < due) await new Promise((resolve) => setTimeout(resolve, due - Date.now()))
  const res = await api.post(`${base}/api/auth/native-sign-ins/poll`, { data: { poll_secret: started.poll_secret } })
  started.lastPollAt = Date.now()
  const answer = { status: res.status(), body: (await res.json()) as Record<string, unknown> }
  if (answer.body.error === "slow_down") started.interval += 5
  return answer
}

/** Poll until the request is no longer pending, and return that answer. */
async function pollUntilDecided(api: APIRequestContext, base: string, started: Started) {
  const deadline = Date.now() + 60_000
  for (;;) {
    const answer = await poll(api, base, started)
    const waiting = answer.body.error === "authorization_pending" || answer.body.error === "slow_down"
    if (!waiting || Date.now() > deadline) return answer
  }
}

/** Nothing is decided by opening the page: the client still hears "pending". */
async function expectStillPending(api: APIRequestContext, base: string, started: Started) {
  const answer = await poll(api, base, started)
  expect(answer.status).toBe(400)
  expect(answer.body.error).toBe("authorization_pending")
}

/**
 * Send the page's native sign-in calls (look-up, confirm, deny) to one replica
 * instead of through the load-balancing ingress. Returns what was sent there.
 */
async function pinPageTo(page: Page, replica: Replica | undefined): Promise<string[]> {
  const sent: string[] = []
  if (!replica) return sent
  await page.route(
    (url) => url.pathname.startsWith("/api/auth/native-sign-ins"),
    async (route) => {
      const url = new URL(route.request().url())
      sent.push(`${route.request().method()} ${url.pathname}`)
      const response = await route.fetch({ url: replica.base + url.pathname + url.search })
      await route.fulfill({ response })
    }
  )
  return sent
}

/** Open the link the client was given, on the Portal under test. */
async function openLink(page: Page, baseURL: string, started: Started) {
  const link = new URL(started.verification_uri_complete)
  expect(link.hash).toBe(`#/sign-in/${started.user_code}`)
  if (replicas.length > 0) {
    // kind sets public_base_url, so the link is the Portal's own address.
    expect(link.origin).toBe(new URL(baseURL).origin)
  }
  await page.goto(`/${link.hash}`)
}

async function expectRequestCard(page: Page, started: Started, client: string, label: string, email: string) {
  await expect(page.getByText(`${client} is asking to sign in to your account`)).toBeVisible()
  await expect(page.getByText(label, { exact: true })).toBeVisible()
  await expect(page.getByText("Reported by the client, not verified")).toBeVisible()
  await expect(page.getByText(started.user_code, { exact: true })).toBeVisible()
  await expect(page.getByText(email, { exact: true })).toBeVisible()
  await expect(page.getByText("If someone sent you this link or code, deny it.", { exact: false })).toBeVisible()
}

/** After a decision the code is gone from the address, and a reload does not bring it back. */
async function expectCodeCleared(page: Page, started: Started) {
  await expect(page).toHaveURL(/#\/sign-in$/)
  await page.reload()
  await expect(page.getByLabel("Code")).toBeVisible()
  await expect(page.getByText(started.user_code)).toHaveCount(0)
}

function uniqueEmail(prefix: string, domain: string): string {
  return `${prefix}-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}@${domain}`
}

function describeReplicas(...steps: Array<[string, Replica | undefined]>): string {
  return steps.map(([step, replica]) => `${step} on ${replica?.name ?? "the deployment API"}`).join(", ")
}

test("a CLI sign-in confirmed through SSO returns to the request, and each step works on either replica", async ({
  page,
  baseURL,
}) => {
  test.setTimeout(180_000)
  const api = await request.newContext({ ignoreHTTPSErrors: true })
  try {
    const methods = await (await api.get(`${new URL(baseURL!).origin}/api/auth/methods`)).json()
    test.skip(methods.oidc?.enabled !== true, "this deployment does not offer SSO")
    const ssoStart = await api.get(`${new URL(baseURL!).origin}/api/auth/oidc/start`, { maxRedirects: 0 })
    const idp = new URL(ssoStart.headers()["location"] ?? "", baseURL)
    test.skip(idp.hostname !== MOCK_IDP_HOST, `SSO goes to ${idp.hostname}, not the mock provider`)

    const [a, b] = replicas
    const email = uniqueEmail("native-sso", MOCK_IDP_DOMAIN)

    // Round one: started on A, confirmed on B, redeemed on A.
    const first = await start(api, apiOf(baseURL!, a), "cli", "e2e-runner · Linux")
    const sentToB = await pinPageTo(page, b)
    await openLink(page, baseURL!, first)

    // Signed out, the Portal asks for a sign-in first; SSO carries the request's
    // address through the IdP and back.
    await page.getByRole("button", { name: "Sign in with Mock IdP" }).click()
    await expect(page.getByRole("heading", { name: "Mock IdP" })).toBeVisible()
    await page.getByLabel("Email", { exact: true }).fill(email)
    await page.getByLabel("Name").fill("Native SSO E2E")
    await page.getByRole("button", { name: "Sign in", exact: true }).click()
    await page.waitForURL((url) => url.origin === new URL(baseURL!).origin && url.hash === `#/sign-in/${first.user_code}`)

    await expectRequestCard(page, first, "BuildMax CLI", "e2e-runner · Linux", email)
    await expectStillPending(api, apiOf(baseURL!, a), first)

    await page.getByRole("button", { name: "Confirm sign-in" }).click()
    await expect(page.getByText("Sign-in confirmed. Return to BuildMax CLI")).toBeVisible()
    if (b) expect(sentToB).toContain("POST /api/auth/native-sign-ins/confirm")

    const redeemed = await pollUntilDecided(api, apiOf(baseURL!, a), first)
    expect(redeemed.status, JSON.stringify(redeemed.body)).toBe(200)
    expect((redeemed.body.user as { email: string }).email).toBe(email)
    // The native session is real: it reaches the API as that account.
    const spaces = await api.get(`${apiOf(baseURL!, b)}/api/spaces`, {
      headers: { Authorization: `Bearer ${redeemed.body.access_token as string}` },
    })
    expect(spaces.status()).toBe(200)
    // Exactly one poll redeems a request.
    expect((await poll(api, apiOf(baseURL!, a), first)).body.error).toBe("expired_token")
    await expectCodeCleared(page, first)
    console.log(`[e2e] native sign-in round one: ${describeReplicas(["start", a], ["confirm", b], ["redeem", a])}`)

    // Round two, the other way round, and already signed in: started on B,
    // confirmed on A, redeemed on B.
    await page.unrouteAll({ behavior: "wait" })
    const second = await start(api, apiOf(baseURL!, b), "desktop", "e2e-runner · Windows")
    const sentToA = await pinPageTo(page, a)
    await openLink(page, baseURL!, second)
    await expectRequestCard(page, second, "BuildMax Desktop", "e2e-runner · Windows", email)
    await expectStillPending(api, apiOf(baseURL!, b), second)

    await page.getByRole("button", { name: "Confirm sign-in" }).click()
    await expect(page.getByText("Sign-in confirmed. Return to BuildMax Desktop")).toBeVisible()
    if (a) expect(sentToA).toContain("POST /api/auth/native-sign-ins/confirm")

    const redeemedTwo = await pollUntilDecided(api, apiOf(baseURL!, b), second)
    expect(redeemedTwo.status, JSON.stringify(redeemedTwo.body)).toBe(200)
    expect((redeemedTwo.body.user as { email: string }).email).toBe(email)
    await expectCodeCleared(page, second)
    console.log(`[e2e] native sign-in round two: ${describeReplicas(["start", b], ["confirm", a], ["redeem", b])}`)
  } finally {
    await api.dispose()
  }
})

/** A fresh account with a password, made through the admin API as an operator would. */
async function passwordAccount(browser: Browser, baseURL: string): Promise<{ email: string; password: string }> {
  const context = await browser.newContext({ baseURL, storageState: ADMIN_STATE })
  const page = await context.newPage()
  try {
    const admin = await session(page)
    const email = uniqueEmail("native-password", "example.com")
    const auth = { Authorization: `Bearer ${admin.token}` }
    const created = await page.request.post(`${admin.apiBase}/api/admin/users`, { headers: auth, data: { email } })
    expect(created.status(), await created.text()).toBe(201)
    const { id } = (await created.json()) as { id: string }
    const issued = await page.request.post(`${admin.apiBase}/api/admin/users/${id}/login-code`, { headers: auth })
    expect(issued.ok(), await issued.text()).toBeTruthy()
    const { code } = (await issued.json()) as { code: string }

    // Claim the account with its code, then set the password the person will
    // type, as someone given a code does.
    const login = await page.request.post(`${admin.apiBase}/api/auth/login`, {
      data: { email, otp: code, platform: "cli" },
    })
    expect(login.ok(), await login.text()).toBeTruthy()
    const { access_token: token } = (await login.json()) as { access_token: string }
    const password = `pw-${Math.random().toString(36).slice(2)}-${Date.now().toString(36)}`
    const set = await page.request.post(`${admin.apiBase}/api/auth/password`, {
      headers: { Authorization: `Bearer ${token}` },
      data: { new_password: password, current_password: "" },
    })
    expect(set.ok(), await set.text()).toBeTruthy()
    return { email, password }
  } finally {
    await context.close()
  }
}

test("a sign-in opened signed out returns to the request after a password sign-in, and can be denied", async ({
  page,
  browser,
  baseURL,
}) => {
  test.setTimeout(180_000)
  const { email, password } = await passwordAccount(browser, baseURL!)
  const api = await request.newContext({ ignoreHTTPSErrors: true })
  try {
    const [a, b] = replicas

    // Confirmed: started on A, confirmed on B, redeemed on B.
    const first = await start(api, apiOf(baseURL!, a), "cli", "e2e-runner · macOS")
    const sentToB = await pinPageTo(page, b)
    await openLink(page, baseURL!, first)
    await page.getByLabel("Email").fill(email)
    await page.getByLabel("Password").fill(password)
    await page.getByRole("button", { name: "Sign in", exact: true }).click()

    await expect(page).toHaveURL(new RegExp(`#/sign-in/${first.user_code}$`))
    await expectRequestCard(page, first, "BuildMax CLI", "e2e-runner · macOS", email)
    await expectStillPending(api, apiOf(baseURL!, a), first)

    await page.getByRole("button", { name: "Confirm sign-in" }).click()
    await expect(page.getByText("Sign-in confirmed. Return to BuildMax CLI")).toBeVisible()
    if (b) expect(sentToB).toContain("POST /api/auth/native-sign-ins/confirm")
    const redeemed = await pollUntilDecided(api, apiOf(baseURL!, b), first)
    expect(redeemed.status, JSON.stringify(redeemed.body)).toBe(200)
    expect((redeemed.body.user as { email: string }).email).toBe(email)
    await expectCodeCleared(page, first)

    // Denied: started on B, denied on A, and the client polling A is told.
    await page.unrouteAll({ behavior: "wait" })
    const second = await start(api, apiOf(baseURL!, b), "desktop", "someone-else · Linux")
    const sentToA = await pinPageTo(page, a)
    await openLink(page, baseURL!, second)
    await expectRequestCard(page, second, "BuildMax Desktop", "someone-else · Linux", email)
    await page.getByRole("button", { name: "Deny" }).click()
    await expect(page.getByText("Sign-in denied. BuildMax Desktop will report that the sign-in was not allowed.")).toBeVisible()
    if (a) expect(sentToA).toContain("POST /api/auth/native-sign-ins/deny")
    const denied = await pollUntilDecided(api, apiOf(baseURL!, a), second)
    expect(denied.status).toBe(400)
    expect(denied.body.error).toBe("access_denied")
    await expectCodeCleared(page, second)

    // A decided code is just not valid any more, whichever decision it got.
    await page.goto(`/#/sign-in/${second.user_code}`)
    await expect(page.getByText("This code is not valid any more.")).toBeVisible()
    await expect(page.getByRole("button", { name: "Confirm sign-in" })).toHaveCount(0)
    console.log(
      `[e2e] native sign-in, password: ${describeReplicas(["start", a], ["confirm", b], ["redeem", b])}; ` +
        `deny: ${describeReplicas(["start", b], ["deny", a], ["poll", a])}`
    )
  } finally {
    await api.dispose()
  }
})

test("a native session is listed in Account → Sessions and revoking it there stops its token", async ({
  page,
  browser,
  baseURL,
}) => {
  test.setTimeout(180_000)
  const { email, password } = await passwordAccount(browser, baseURL!)
  const api = await request.newContext({ ignoreHTTPSErrors: true })
  try {
    const [a, b] = replicas
    const label = `e2e-sessions-${Date.now().toString(36)} · Linux`
    const started = await start(api, apiOf(baseURL!, a), "cli", label)
    await openLink(page, baseURL!, started)
    await page.getByLabel("Email").fill(email)
    await page.getByLabel("Password").fill(password)
    await page.getByRole("button", { name: "Sign in", exact: true }).click()
    await expectRequestCard(page, started, "BuildMax CLI", label, email)
    await page.getByRole("button", { name: "Confirm sign-in" }).click()
    await expect(page.getByText("Sign-in confirmed. Return to BuildMax CLI")).toBeVisible()
    const redeemed = await pollUntilDecided(api, apiOf(baseURL!, a), started)
    expect(redeemed.status, JSON.stringify(redeemed.body)).toBe(200)
    const cliToken = redeemed.body.access_token as string
    const cliRefresh = redeemed.body.refresh_token as string
    const asCLI = { Authorization: `Bearer ${cliToken}` }

    // The CLI sees itself in the same list, as platform cli with its label, and
    // nothing in it is a token.
    const listed = await api.get(`${apiOf(baseURL!, b)}/api/auth/sessions`, { headers: asCLI })
    const listedText = await listed.text()
    expect(listed.status(), listedText).toBe(200)
    expect(listedText).not.toContain(cliToken)
    expect(listedText).not.toContain(cliRefresh)
    const own = (JSON.parse(listedText) as { sessions: Array<Record<string, unknown>> }).sessions.find((s) => s.current)
    expect(own).toMatchObject({ platform: "cli", client_label: label, auth_method: "password" })

    // The person sees it in the Portal, marked as the client's own claim, beside
    // this browser.
    await page.goto("/#/account/sessions")
    const list = page.getByRole("list", { name: "Signed-in sessions" })
    const row = list.getByRole("listitem").filter({ hasText: label })
    await expect(row).toContainText("BuildMax CLI")
    await expect(row).toContainText("Reported by the client, not verified")
    await expect(list.getByRole("listitem").filter({ hasText: "This browser" })).toHaveCount(1)

    page.once("dialog", (dialog) => void dialog.accept())
    await row.getByRole("button", { name: `Sign out ${label}` }).click()
    await expect(page.getByText(`Signed out ${label}.`)).toBeVisible()
    await expect(row).toHaveCount(0)

    // The CLI's next request is refused on every replica, and its refresh token
    // is retired with it: the CLI can only report that its login expired.
    for (const replica of replicas.length > 0 ? replicas : [undefined]) {
      const refused = await api.get(`${apiOf(baseURL!, replica)}/api/spaces`, { headers: asCLI })
      expect(refused.status(), `revoked token on ${replica?.name ?? "the deployment API"}`).toBe(401)
    }
    const refresh = await api.post(`${apiOf(baseURL!, a)}/api/auth/token/refresh`, {
      data: { refresh_token: cliRefresh },
    })
    expect(refresh.status(), await refresh.text()).toBe(401)

    // Signing this browser out from the same list returns it to sign-in, and a
    // reload does not bring the session back.
    page.once("dialog", (dialog) => void dialog.accept())
    await page.getByRole("button", { name: "Sign out of this browser" }).click()
    await expect(page.getByLabel("Password")).toBeVisible()
    await page.reload()
    await expect(page.getByLabel("Password")).toBeVisible()
    console.log(
      `[e2e] self-service sessions: ${describeReplicas(["start", a], ["list", b], ["refresh", a])}, revoked token refused on every replica`
    )
  } finally {
    await api.dispose()
  }
})
