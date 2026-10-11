import { expect, test, type Browser, type Page } from "@playwright/test"

import { MEMBER_STATE } from "./global-setup"
import { createSpace, patchJSON, postJSON, reportLeftovers, RUN_ID, session, tagged, type Session } from "./fixtures"
import { buildPluginPackage } from "./pluginPackage"

/**
 * A run blocked by the Space's setup has to lead each reader to a fix they can
 * make: an owner to the setting, a member to the people who can change it.
 * Each case builds its own Space so a membership or a suspended plugin cannot
 * leak into another spec, and only a real worker refusing the run proves the
 * cause reaches Portal.
 */

// The worker has to start and be refused; this allows for a cold worker.
const RUN_TIMEOUT_MS = 120_000

/** The member account `./make e2e` signs in, joined to `spaceId` as a member. */
async function joinAsMember(page: Page, browser: Browser, owner: Session, spaceId: string): Promise<Page> {
  const portalBase = new URL(page.url()).origin
  const context = await browser.newContext({ storageState: MEMBER_STATE, baseURL: portalBase })
  const memberPage = await context.newPage()
  const member = await session(memberPage)
  // global-setup signed this account in with the same address.
  const email = process.env.BUILDMAX_E2E_MEMBER_EMAIL
  expect(email, "BUILDMAX_E2E_MEMBER_EMAIL is set by ./make e2e").toBeTruthy()
  const invitation = await postJSON<{ id: string }>(page, `${owner.apiBase}/api/spaces/${spaceId}/invitations`, owner, {
    email,
    role: "member",
  })
  await postJSON(memberPage, `${member.apiBase}/api/invitations/${invitation.id}/accept`, member, {})
  return memberPage
}

/**
 * Open a page of the Space the member just joined. The app listed its Spaces
 * before the invitation was accepted, and a hash change does not list them
 * again, so the page is reloaded to read the new Space and the member's role.
 */
async function openAsMember(memberPage: Page, path: string): Promise<void> {
  await memberPage.goto(path)
  await memberPage.reload()
}

test("a member is warned before Run agent about a broken Secret grant, never shown the Secret, and told who can fix it", async ({
  page,
  browser,
}) => {
  test.setTimeout(RUN_TIMEOUT_MS + 90_000)
  const owner = await session(page)
  const space = await createSpace(page, owner, tagged("Member grant warning"))
  const base = `${owner.apiBase}/api/spaces/${space.id}`
  const secretName = tagged("member-hidden-secret")
  const secret = await postJSON<{ id: string }>(page, `${base}/secrets`, owner, {
    name: secretName,
    description: "withheld from members",
    items: { TOKEN: "not-a-real-token" },
  })
  const agentName = tagged("Member grant agent")
  const agent = await postJSON<{ id: string }>(page, `${base}/agents`, owner, {
    name: agentName,
    instructions: "Reply with exactly: deployment smoke ok",
    secret_consumption: { env: [{ secret: secret.id, item: "TOKEN", env_name: "BLOCKED_TOKEN" }] },
  })
  const disabled = await page.request.put(`${base}/secrets/${encodeURIComponent(secret.id)}/state`, {
    headers: { Authorization: `Bearer ${owner.token}` },
    data: { state: "disabled" },
  })
  expect(disabled.ok(), `disable the Secret → ${disabled.status()}`).toBeTruthy()
  const issue = await postJSON<{ id: string }>(page, `${base}/issues`, owner, {
    title: tagged("Member grant issue"),
    executor_kind: "agent",
    executor_id: agent.id,
  })
  reportLeftovers(space.id, [`space ${space.id}`, `secret ${secret.id}`, `agent ${agent.id}`, `issue ${issue.id}`])

  const member = await joinAsMember(page, browser, owner, space.id)
  try {
    // Nothing the design withholds from members reaches the browser at all,
    // not only the screen: the Agent read carries the grant's health alone.
    const withheld = [secretName, "withheld from members"]
    const leaks: string[] = []
    const reads: Promise<void>[] = []
    // JSON bodies only: a Task's event stream stays open while the page does
    // and carries run output, not Secret metadata. Each read is bounded: a body
    // the browser dropped across a reload can leave text() pending forever, and
    // the direct reads below cover the routes that matter either way.
    member.on("response", (response) => {
      if (!response.url().includes("/api/")) return
      if (!(response.headers()["content-type"] ?? "").includes("application/json")) return
      const body = response.text().catch(() => "")
      const bounded = new Promise<string>((resolve) => setTimeout(() => resolve(""), 10_000))
      reads.push(
        Promise.race([body, bounded]).then((text) => {
          for (const value of withheld) if (text.includes(value)) leaks.push(`${response.url()} carried ${value}`)
        }),
      )
    })

    await openAsMember(member, `/#/spaces/${space.id}/issues/${issue.id}`)
    const warning = member.getByTestId("issue-run-grant-warning")
    await expect(warning).toHaveText(
      "⚠ 1 secret grant of this Agent no longer resolves, so a run fails until it is fixed. Ask a Space owner or admin to fix it.",
    )
    // A member cannot fix the grant, so there is no link to a page that cannot.
    await expect(warning.getByRole("link")).toHaveCount(0)
    const runAgent = member.getByRole("button", { name: "Run agent" })
    await expect(runAgent).toHaveClass(/bm-button--secondary/)
    await expect(runAgent).toHaveAccessibleDescription(/no longer resolves/)

    await runAgent.click()
    const latest = member.getByRole("region", { name: "Latest run" })
    await expect(latest.getByText("This Agent's Secret grant is disabled")).toBeVisible({ timeout: RUN_TIMEOUT_MS })
    await expect(latest).toContainText(
      `${agentName} needs a Secret granted to it, which is disabled. Only a Space owner or admin can fix this; ask one to, then run again.`,
    )
    await expect(latest.getByRole("link", { name: "Open the Agent to fix its Secret grant" })).toHaveCount(0)
    await expect(latest.getByRole("button", { name: "Retry Run" })).toHaveClass(/bm-button--tertiary/)
    await expect(member.locator("main")).not.toContainText(secret.id)

    // The Agent page warns the member the same way before its own Run agent.
    await openAsMember(member, `/#/spaces/${space.id}/agents/${agent.id}`)
    await expect(member.locator(".agent-detail__banner")).toHaveText(
      "⚠ 1 secret grant no longer resolves. Ask a Space owner or admin to fix it before you run this Agent.",
    )
    await expect(member.getByRole("button", { name: "Fix in config" })).toHaveCount(0)

    // The reads the member's pages make for this Agent and Issue, made
    // directly, so the assertion does not depend on what the browser kept.
    const memberSession = await session(member)
    const memberBase = `${memberSession.apiBase}/api/spaces/${space.id}`
    for (const path of [`/agents`, `/agents/${agent.id}`, `/issues/${issue.id}`, `/issues/${issue.id}/flow`]) {
      const response = await member.request.get(`${memberBase}${path}`, {
        headers: { Authorization: `Bearer ${memberSession.token}` },
      })
      expect(response.ok(), `GET ${path} as the member → ${response.status()}`).toBeTruthy()
      const text = await response.text()
      for (const value of withheld) if (text.includes(value)) leaks.push(`GET ${path} carried ${value}`)
    }

    await Promise.all(reads)
    expect(leaks).toEqual([])
  } finally {
    await member.context().close()
  }
})

test("a run refused for a suspended plugin names it and leads an owner to the Space's plugin settings", async ({ page, browser }) => {
  test.setTimeout(RUN_TIMEOUT_MS + 90_000)
  const owner = await session(page)
  const space = await createSpace(page, owner, tagged("Suspended plugin"))
  const base = `${owner.apiBase}/api/spaces/${space.id}`
  const name = `e2e-suspended-plugin-${RUN_ID}`.toLowerCase()
  await postJSON(page, `${owner.apiBase}/api/admin/plugins`, owner, { name, display_name: `E2E suspended plugin ${RUN_ID}` })
  const publish = await page.request.post(`${owner.apiBase}/api/admin/plugins/${name}/releases`, {
    headers: { Authorization: `Bearer ${owner.token}` },
    data: buildPluginPackage(`name: ${name}\nversion: 1.0.0\ndescription: e2e suspended plugin.\n`),
  })
  expect(publish.ok(), `publish → ${publish.status()} ${await publish.text()}`).toBeTruthy()
  await postJSON(page, `${base}/plugin-activations`, owner, { plugin_name: name })
  const agentName = tagged("Suspended plugin agent")
  const agent = await postJSON<{ id: string }>(page, `${base}/agents`, owner, {
    name: agentName,
    instructions: "Reply with exactly: deployment smoke ok",
    plugins: [name],
  })
  await patchJSON(page, `${base}/plugin-activations/${name}`, owner, { enabled: false })
  const issue = await postJSON<{ id: string }>(page, `${base}/issues`, owner, {
    title: tagged("Suspended plugin issue"),
    executor_kind: "agent",
    executor_id: agent.id,
  })
  reportLeftovers(space.id, [`space ${space.id}`, `plugin ${name}`, `agent ${agent.id}`, `issue ${issue.id}`])

  const issueUrl = `/#/spaces/${space.id}/issues/${issue.id}`
  await page.goto(issueUrl)
  await page.getByRole("button", { name: "Run agent" }).click()
  const latest = page.getByRole("region", { name: "Latest run" })
  await expect(latest.getByText("This Agent's plugin is suspended in this Space")).toBeVisible({ timeout: RUN_TIMEOUT_MS })
  await expect(latest).toContainText(
    `${agentName} names the plugin “${name}”, whose activation in this Space is suspended. Resume it in the Space's plugin settings, or remove it from the Agent, then run again.`,
  )
  const fix = latest.getByRole("link", { name: "Open plugin settings" })
  await expect(fix).toHaveClass(/bm-button--primary/)
  await expect(fix).toHaveAttribute("href", `#/spaces/${space.id}/settings/plugins`)
  await expect(latest.getByRole("button", { name: "Retry Run" })).toHaveClass(/bm-button--tertiary/)

  // A member reads the same plugin, and who can resume it.
  const member = await joinAsMember(page, browser, owner, space.id)
  try {
    await openAsMember(member, issueUrl)
    const memberLatest = member.getByRole("region", { name: "Latest run" })
    await expect(memberLatest).toContainText(
      `${agentName} names the plugin “${name}”, whose activation in this Space is suspended. Only a Space owner or admin can fix this; ask one to, then run again.`,
    )
    await expect(memberLatest.getByRole("link", { name: "Open plugin settings" })).toHaveCount(0)
  } finally {
    await member.context().close()
  }

  // In Chinese, for the owner.
  await page.evaluate(() => localStorage.setItem("buildmax_locale", "zh-CN"))
  await page.goto(issueUrl)
  await page.reload()
  const latestZh = page.getByRole("region", { name: "最近一次运行" })
  await expect(latestZh.getByText("该 Agent 的插件在此 Space 中已暂停")).toBeVisible()
  await expect(latestZh.getByRole("link", { name: "打开插件设置" })).toBeVisible()
  await page.evaluate(() => localStorage.removeItem("buildmax_locale"))
})
