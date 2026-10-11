import { expect, test, type Page } from "@playwright/test"

import { postJSON, reportLeftovers, session, tagged, type Session } from "./fixtures"

/**
 * A run refused for a configuration the person can fix has to say so in their
 * terms and lead with the fix (portal audit finding P2). The spec builds the
 * situation itself: an Agent granted a Secret that an owner then disabled,
 * assigned to an Issue. Only a real worker refusing the grant proves the cause
 * reaches Portal, and only a real render proves what the person reads and
 * which action leads.
 */

// The worker has to start and be refused; this allows for a cold worker.
const RUN_TIMEOUT_MS = 120_000

const TITLE = "This Agent's Secret grant is disabled"
const FIX = "Open the Agent to fix its Secret grant"

interface Blocked {
  issueId: string
  agentName: string
  secretName: string
}

async function seedBlockedAgentIssue(page: Page, current: Session): Promise<Blocked> {
  const space = current.space
  const secretName = tagged("blocked-grant-secret")
  const secret = await postJSON<{ id: string }>(page, `${space}/secrets`, current, {
    name: secretName,
    items: { TOKEN: "not-a-real-token" },
  })
  const agentName = tagged("Blocked grant agent")
  const agent = await postJSON<{ id: string }>(page, `${space}/agents`, current, {
    name: agentName,
    description: "Created by the Portal browser tests.",
    instructions: "Reply with exactly: deployment smoke ok",
    secret_consumption: { env: [{ secret: secret.id, item: "TOKEN", env_name: "BLOCKED_TOKEN" }] },
  })
  const disabled = await page.request.put(`${space}/secrets/${encodeURIComponent(secret.id)}/state`, {
    headers: { Authorization: `Bearer ${current.token}` },
    data: { state: "disabled" },
  })
  expect(disabled.ok(), `disable the Secret → ${disabled.status()} ${await disabled.text()}`).toBeTruthy()
  const issue = await postJSON<{ id: string }>(page, `${space}/issues`, current, {
    title: tagged("Blocked grant issue"),
    executor_kind: "agent",
    executor_id: agent.id,
  })
  reportLeftovers(current.spaceId, [`secret ${secret.id}`, `agent ${agent.id}`, `issue ${issue.id}`])
  return { issueId: issue.id, agentName, secretName }
}

test("a run refused for a disabled Secret grant names it, leads with the fix, and warned before it started", async ({ page }) => {
  test.setTimeout(RUN_TIMEOUT_MS + 60_000)
  const current = await session(page)
  const blocked = await seedBlockedAgentIssue(page, current)
  const issueUrl = `/#/spaces/${current.spaceId}/issues/${blocked.issueId}`
  await page.goto(issueUrl)

  // Before the run: the same warning the Agent page shows, the fix as the
  // primary action, and Run agent stepped back to secondary.
  const warning = page.getByTestId("issue-run-grant-warning")
  await expect(warning).toContainText("1 secret grant of this Agent no longer resolves, so a run fails until it is fixed.")
  await expect(warning.getByRole("link", { name: FIX })).toHaveClass(/bm-button--primary/)
  const runAgent = page.getByRole("button", { name: "Run agent" })
  await expect(runAgent).toHaveClass(/bm-button--secondary/)
  await expect(runAgent).toHaveAccessibleDescription(/no longer resolves/)

  await runAgent.click()

  // The failed run, in the person's terms, with the fix leading.
  const latest = page.getByRole("region", { name: "Latest run" })
  await expect(latest.getByText(TITLE)).toBeVisible({ timeout: RUN_TIMEOUT_MS })
  await expect(latest).toContainText(`${blocked.agentName} needs the Secret “${blocked.secretName}”, which is disabled.`)
  await expect(latest.getByRole("link", { name: FIX })).toHaveClass(/bm-button--primary/)
  await expect(latest.getByRole("button", { name: "Retry Run" })).toHaveClass(/bm-button--tertiary/)
  // The server's own text is kept, behind a disclosure.
  await latest.getByText("Server message").click()
  await expect(latest).toContainText("secret is disabled")
  // The fix leads to the Agent.
  await expect(latest.getByRole("link", { name: FIX })).toHaveAttribute("href", /#\/spaces\/[^/]+\/agents\/[^/]+$/)

  // The Agent's report in the Discussion is the same explanation.
  await page.getByRole("button", { name: /^Discussion/ }).click()
  await expect(page.locator(".issue-discussion").getByText(TITLE)).toBeVisible()

  // The Task page leads with the fix too, and names the cause in its details.
  await page.getByRole("button", { name: /^Overview/ }).click()
  await latest.getByRole("link", { name: "Open Task" }).click()
  await expect(page.getByRole("link", { name: FIX })).toHaveClass(/bm-button--primary/)
  await expect(page.getByRole("button", { name: "Retry last run" })).toHaveClass(/bm-button--tertiary/)
  await expect(page.locator(".bm-chat-thread").getByText(TITLE)).toBeVisible()
  await page.getByRole("button", { name: "Details", exact: true }).click()
  const details = page.getByRole("dialog", { name: "Task details" })
  await expect(details).toContainText(`Cause${TITLE}`)

  // Run details explains the run and asks for no trace that was never written.
  const notFound: string[] = []
  page.on("response", (response) => {
    if (response.status() === 404) notFound.push(response.url())
  })
  await details.getByRole("button", { name: "View trace" }).click()
  const runDetails = page.getByRole("dialog", { name: "Run details" })
  await expect(runDetails.getByRole("heading", { name: "Why it failed" })).toBeVisible()
  await expect(runDetails).toContainText(TITLE)
  await expect(runDetails).toContainText("This run has no trace.")
  await expect(runDetails.getByRole("link", { name: FIX })).toBeVisible()
  expect(notFound, "Run details made a request that 404s").toEqual([])

  // The same explanation in Chinese.
  await page.evaluate(() => localStorage.setItem("buildmax_locale", "zh-CN"))
  await page.goto(issueUrl)
  await page.reload()
  const latestZh = page.getByRole("region", { name: "最近一次运行" })
  await expect(latestZh.getByText("该 Agent 的密钥授权已被禁用")).toBeVisible()
  await expect(latestZh).toContainText(`${blocked.agentName} 需要密钥“${blocked.secretName}”，但该密钥已被禁用。`)
  await expect(latestZh.getByRole("link", { name: "打开 Agent 修复密钥授权" })).toHaveClass(/bm-button--primary/)
  await expect(page.getByTestId("issue-run-grant-warning")).toContainText("该 Agent 有 1 项密钥授权已无法解析")
})
