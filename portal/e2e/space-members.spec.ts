import { expect, test, type Browser, type Page } from "@playwright/test"

import { MEMBER_STATE } from "./global-setup"
import { createSpace, getJSON, postJSON, reportLeftovers, session, tagged, type Session } from "./fixtures"

/**
 * Managing who is in a Space, as its owner meets it: whom a pending invitation
 * is for, what an invite to an unknown address says, what a role change asks
 * and reports, and that every settings tab fits a phone. Then the other side:
 * someone who joins a Space works in it, with its role, without a reload.
 *
 * Only a browser proves these. The handler tests prove the invitee's identity
 * and the refusal's code cross the wire; what a person reads, and which role
 * gates the controls they are offered, exists only in the rendered app.
 */

interface Invitation {
  id: string
  user_name?: string
  user_email?: string
}

interface Member {
  user_id: string
  role: string
  user_name?: string
  user_email?: string
}

function memberEmail(): string {
  const email = process.env.BUILDMAX_E2E_MEMBER_EMAIL
  expect(email, "BUILDMAX_E2E_MEMBER_EMAIL is set by ./make e2e").toBeTruthy()
  return email!
}

/** The second account `./make e2e` signs in, in its own browser context, with the app loaded. */
async function openMember(page: Page, browser: Browser): Promise<{ page: Page; session: Session }> {
  const context = await browser.newContext({
    storageState: MEMBER_STATE,
    baseURL: new URL(page.url()).origin,
    locale: "en-US",
  })
  const memberPage = await context.newPage()
  return { page: memberPage, session: await session(memberPage) }
}

const UNKNOWN_EMAIL_COPY = "No account uses this email. Ask a system administrator to create one, then invite it."

test("a pending invitation names its invitee, Revoke says whose it is, and an unknown email is refused in plain words", async ({
  page,
}) => {
  const owner = await session(page)
  const teamName = tagged("Invitee names")
  const team = await createSpace(page, owner, teamName)
  reportLeftovers(team.id, [`space ${team.id}`])
  const email = memberEmail()

  // Straight to the new Space by URL: the app listed its Spaces before this
  // one existed, and reaching it must not need a reload.
  await page.goto(`/#/spaces/${team.id}/settings/members`)
  const members = page.locator(".settings-page__section").filter({ has: page.getByRole("heading", { name: "Members" }) })
  await members.getByRole("button", { name: "Invite", exact: true }).click()
  const dialog = page.getByRole("dialog", { name: "Invite" })
  await dialog.getByLabel("Spacemate email").fill(`no-account-${Date.now()}@buildmax.local`)
  await dialog.getByRole("button", { name: "Send Invite" }).click()
  const refusal = dialog.getByRole("alert")
  await expect(refusal).toHaveText(UNKNOWN_EMAIL_COPY)
  await expect(refusal).not.toContainText("/api/")
  await expect(refusal).not.toContainText("buildmax-server")

  await dialog.getByLabel("Spacemate email").fill(email)
  await dialog.getByRole("button", { name: "Send Invite" }).click()
  await expect(dialog).toHaveCount(0)

  const [pending] = await getJSON<Invitation[]>(page, `${owner.apiBase}/api/spaces/${team.id}/invitations`, owner)
  expect(pending.user_email).toBe(email)
  const label = pending.user_name || email
  const row = page.locator(".space-settings-page__invitations .space-settings-page__member").filter({ hasText: email })
  await expect(row).toBeVisible()
  await expect(row.locator(".space-settings-page__member-name")).toHaveText(label)
  // The ID stays, as secondary metadata rather than the row's only name.
  await expect(row).toContainText(`Invitation ${pending.id}`)
  await expect(row.getByRole("button", { name: `Revoke the invitation for ${label}` })).toBeVisible()

  // The same, in Chinese. The language is read when the app loads.
  await page.evaluate(() => localStorage.setItem("buildmax_locale", "zh-CN"))
  await page.reload()
  try {
    const zhRow = page.locator(".space-settings-page__invitations .space-settings-page__member").filter({ hasText: email })
    await expect(page.getByRole("heading", { name: "待接受的邀请" })).toBeVisible()
    await expect(zhRow).toContainText(`邀请 ${pending.id}`)
    await expect(zhRow.getByRole("button", { name: `撤销对 ${label} 的邀请` })).toBeVisible()
    await page.getByRole("button", { name: "邀请", exact: true }).click()
    const zhDialog = page.getByRole("dialog", { name: "邀请" })
    await zhDialog.getByLabel("成员邮箱").fill(`no-account-${Date.now()}@buildmax.local`)
    await zhDialog.getByRole("button", { name: "发送邀请" }).click()
    await expect(zhDialog.getByRole("alert")).toHaveText("没有账户使用此邮箱。请让系统管理员先创建账户，然后再邀请。")
    await zhDialog.getByRole("button", { name: "取消" }).click()

    await zhRow.getByRole("button", { name: `撤销对 ${label} 的邀请` }).click()
    await expect(zhRow).toHaveCount(0)
  } finally {
    await page.evaluate(() => localStorage.removeItem("buildmax_locale"))
  }
})

test("raising a member to Admin asks first, and every role change says what it saved", async ({ page, browser }) => {
  const owner = await session(page)
  const teamName = tagged("Role changes")
  const team = await createSpace(page, owner, teamName)
  reportLeftovers(team.id, [`space ${team.id}`])
  const email = memberEmail()
  const invitation = await postJSON<{ id: string }>(page, `${owner.apiBase}/api/spaces/${team.id}/invitations`, owner, {
    email,
    role: "member",
  })
  const member = await openMember(page, browser)
  try {
    await postJSON(member.page, `${member.session.apiBase}/api/invitations/${invitation.id}/accept`, member.session, {})
  } finally {
    await member.page.context().close()
  }
  const roster = () => getJSON<Member[]>(page, `${owner.apiBase}/api/spaces/${team.id}/members`, owner)
  const joined = (await roster()).find((m) => m.user_email === email)
  expect(joined, "the member joined the Space").toBeTruthy()
  const name = joined!.user_name || email

  await page.goto(`/#/spaces/${team.id}/settings/members`)
  const row = page.locator(".space-settings-page__member-list .space-settings-page__member").filter({ hasText: email })
  const select = row.getByRole("combobox", { name: `Role for ${name}` })
  await expect(select).toHaveValue("member")

  // Declining leaves the role as it was, on screen and on the server.
  const asked: string[] = []
  page.once("dialog", (dialog) => {
    asked.push(dialog.message())
    void dialog.dismiss()
  })
  await select.selectOption("admin")
  await expect.poll(() => asked.length).toBe(1)
  expect(asked[0]).toBe(`Change ${name}'s role in ${teamName} to admin?\n\nThis gives them more authority, immediately.`)
  await expect(select).toHaveValue("member")
  await expect(row.getByRole("status")).toHaveCount(0)
  expect((await roster()).find((m) => m.user_email === email)?.role).toBe("member")

  page.once("dialog", (dialog) => void dialog.accept())
  await select.selectOption("admin")
  await expect(row.getByRole("status")).toHaveText(`Saved. ${name} is now admin.`)
  await expect(select).toHaveValue("admin")
  expect((await roster()).find((m) => m.user_email === email)?.role).toBe("admin")

  // Lowering a role takes nothing the owner cannot give back, so it asks
  // nothing -- but still says what it saved.
  const unexpected: string[] = []
  const onDialog = (dialog: { message(): string; dismiss(): Promise<void> }) => {
    unexpected.push(dialog.message())
    void dialog.dismiss()
  }
  page.on("dialog", onDialog)
  try {
    await select.selectOption("member")
    await expect(row.getByRole("status")).toHaveText(`Saved. ${name} is now member.`)
    expect(unexpected).toEqual([])
    expect((await roster()).find((m) => m.user_email === email)?.role).toBe("member")
  } finally {
    page.off("dialog", onDialog)
  }
})

test("every Space settings tab is reachable at 390px without hidden scrolling", async ({ page }) => {
  const owner = await session(page)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto(`/#/spaces/${owner.spaceId}/settings`)

  const tablist = page.getByRole("tablist", { name: "Space sections" })
  const tabs = tablist.getByRole("tab")
  await expect(tabs).toHaveCount(8)
  for (const tab of await tabs.all()) {
    await expect(tab).toBeInViewport({ ratio: 1 })
  }
  // Nothing is tucked away behind a horizontal scroll, in the list or the page.
  expect(await tablist.evaluate((el) => el.scrollWidth - el.clientWidth)).toBeLessThanOrEqual(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth - document.documentElement.clientWidth)).toBeLessThanOrEqual(0)

  // The last tab works from there, not only shows.
  await tablist.getByRole("tab", { name: "Audit" }).click()
  await expect(page).toHaveURL(new RegExp(`#/spaces/${owner.spaceId}/settings/audit$`))
})

test("accepting an invitation makes the joined Space current, with its own role, without a reload", async ({
  page,
  browser,
}) => {
  const owner = await session(page)
  const teamName = tagged("Joined in place")
  const team = await createSpace(page, owner, teamName)
  const otherName = tagged("Still pending")
  const other = await createSpace(page, owner, otherName)
  reportLeftovers(team.id, [`space ${team.id}`, `space ${other.id}`])

  // The member's app lists its Spaces before either invitation exists.
  const member = await openMember(page, browser)
  const otherInvitation = await postJSON<{ id: string }>(page, `${owner.apiBase}/api/spaces/${other.id}/invitations`, owner, {
    email: memberEmail(),
    role: "member",
  })
  try {
    await postJSON(page, `${owner.apiBase}/api/spaces/${team.id}/invitations`, owner, { email: memberEmail(), role: "member" })
    const received = await getJSON<{ space_id: string; invited_by_name?: string }[]>(
      member.page,
      `${member.session.apiBase}/api/invitations`,
      member.session,
    )
    const inviter = received.find((inv) => inv.space_id === team.id)?.invited_by_name
    expect(inviter, "the invitee reads who invited them").toBeTruthy()

    // Two pending invitations, told apart by the Space each is for.
    await member.page.goto("/#/account/invitations")
    const rows = member.page.locator(".space-settings-page__member")
    const teamRow = rows.filter({ hasText: teamName })
    const otherRow = rows.filter({ hasText: otherName })
    await expect(teamRow).toContainText(`invited by ${inviter} · member · expires `)
    await expect(otherRow).toContainText(`invited by ${inviter} · member · expires `)
    await teamRow.getByRole("button", { name: `Accept the invitation to ${teamName}` }).click()

    await expect(member.page.getByText(`You joined ${teamName} as member. It is now your current Space.`)).toBeVisible()
    const switcher = member.page.getByLabel("Sidebar", { exact: true }).getByLabel("Space", { exact: true })
    await expect(switcher).toHaveValue(team.id)
    // Only the chosen one was accepted.
    await expect(teamRow).toHaveCount(0)
    await expect(otherRow).toBeVisible()

    await member.page.getByRole("button", { name: `Open ${teamName}` }).click()
    await expect(member.page).toHaveURL(new RegExp(`#/spaces/${team.id}/issues$`))

    // The role is the one granted here, not the owner role the member holds in
    // their own Space: no Agent management is offered.
    await member.page.goto(`/#/spaces/${team.id}/agents`)
    await expect(member.page.getByText("only space owners and admins can create or edit them")).toBeVisible()
    await expect(member.page.getByRole("button", { name: "Create agent" })).toHaveCount(0)

    // The remaining row in Chinese. The language is read when the app loads.
    await member.page.evaluate(() => localStorage.setItem("buildmax_locale", "zh-CN"))
    await member.page.goto("/#/account/invitations")
    await member.page.reload()
    const zhRow = member.page.locator(".space-settings-page__member").filter({ hasText: otherName })
    await expect(zhRow).toContainText(`由 ${inviter} 邀请 · 成员 · `)
    await expect(zhRow).toContainText(" 过期")
    await expect(zhRow.getByRole("button", { name: `接受加入 ${otherName} 的邀请` })).toBeVisible()
  } finally {
    await member.page.context().close()
    // Leave no pending invitation behind for a later run to trip over.
    await page.request.delete(`${owner.apiBase}/api/spaces/${other.id}/invitations/${otherInvitation.id}`, {
      headers: { Authorization: `Bearer ${owner.token}` },
    })
  }
})

test("a link into a Space joined elsewhere opens it with its own role, without a reload", async ({ page, browser }) => {
  const owner = await session(page)
  const teamName = tagged("Joined elsewhere")
  const team = await createSpace(page, owner, teamName)
  reportLeftovers(team.id, [`space ${team.id}`])
  const invitation = await postJSON<{ id: string }>(page, `${owner.apiBase}/api/spaces/${team.id}/invitations`, owner, {
    email: memberEmail(),
    role: "member",
  })

  const member = await openMember(page, browser)
  try {
    // Accepted outside this app's session -- another tab, another device.
    await postJSON(member.page, `${member.session.apiBase}/api/invitations/${invitation.id}/accept`, member.session, {})
    await member.page.goto(`/#/spaces/${team.id}/agents`)

    const switcher = member.page.getByLabel("Sidebar", { exact: true }).getByLabel("Space", { exact: true })
    await expect(switcher).toHaveValue(team.id)
    await expect(switcher.locator("option:checked")).toHaveText(teamName)
    await expect(member.page.getByText("only space owners and admins can create or edit them")).toBeVisible()
    await expect(member.page.getByRole("button", { name: "Create agent" })).toHaveCount(0)
  } finally {
    await member.page.context().close()
  }
})
