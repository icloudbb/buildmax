import { expect, request, test, type Page } from "@playwright/test"

import { createSpace, getJSON, reportLeftovers, session, tagged, type Session } from "./fixtures"

/**
 * A Space Assistant answering a person in a chat app, end to end: the chat
 * link made in the Portal, the bot bound and published in the Portal, the
 * reply that comes back through the bot, and a request it escalates to an Issue
 * that a member answers from the Portal.
 *
 * kind runs a Telegram Bot API double (deployment/smoke/mock-telegram) and
 * points the server's system bot at it. The spec plays the person on the other
 * side through the double's control routes, which the ingress publishes under
 * /smoke-telegram/control/:
 *
 *   POST updates   {bot_id, from_id, text, username} sends a bot a private message
 *   GET  messages  ?bot_id=&chat_id= lists what bots sent, oldest first
 *   POST reset     forgets every queued update and sent message
 *
 * A bot is any token <digits>:<secret>; its id is the digits. The model is the
 * kind mock, which answers "deployment smoke ok". To make a turn call a tool,
 * arm one on the mock model with POST /smoke-llm/control/toolcall
 * {name, args, match}; the match reserves it for the request carrying that
 * text (internal/testsupport/mockllm).
 *
 * A deployment without the double, such as the Compose stack, skips this spec.
 */

const CONTROL = "/smoke-telegram/control"
/** The system bot server.kind.yaml configures. */
const SYSTEM_BOT = "1000"
/** A first assistant reply, from the turn and the mock model. */
const SMOKE_REPLY = "deployment smoke ok"

interface SentMessage {
  bot_id: string
  chat_id: string
  text: string
}

interface ChannelLinks {
  platforms: { platform: string; bot_handle?: string }[]
  links: { id: string; platform: string; handle: string }[]
}

interface StoredAssistant {
  id: string
  name: string
  availability: string
  binding?: { bot_handle: string }
}

interface IssueList {
  issues: { id: string; title: string; assistant_id?: string }[]
}

interface ConversationList {
  conversations: { id: string; channel: string; assistant_id?: string }[]
}

test.beforeAll(async ({ baseURL }) => {
  const api = await request.newContext({ baseURL })
  try {
    // An SPA fallback answers any path with HTML, so only a JSON array is the
    // double answering.
    const res = await api.get(`${CONTROL}/messages?bot_id=0`)
    const body = res.ok() && (res.headers()["content-type"] ?? "").includes("json") ? await res.json() : null
    test.skip(!Array.isArray(body), "this deployment does not run the mock Telegram Bot API")
  } finally {
    await api.dispose()
  }
})

/** A numeric id no earlier run used, so reruns on one cluster never collide. */
function freshID(prefix: string): string {
  return prefix + `${Date.now()}`.slice(-7) + `${Math.floor(Math.random() * 100)}`.padStart(2, "0")
}

async function tell(page: Page, botId: string, fromId: string, text: string): Promise<void> {
  const res = await page.request.post(`${CONTROL}/updates`, {
    data: { bot_id: botId, from_id: Number(fromId), text, username: `e2e_${fromId}` },
  })
  expect(res.ok(), `POST ${CONTROL}/updates → ${res.status()} ${await res.text()}`).toBeTruthy()
}

async function sent(page: Page, botId: string, chatId: string): Promise<SentMessage[]> {
  const res = await page.request.get(`${CONTROL}/messages?bot_id=${botId}&chat_id=${chatId}`)
  expect(res.ok(), `GET ${CONTROL}/messages → ${res.status()}`).toBeTruthy()
  return res.json() as Promise<SentMessage[]>
}

/** The first message botId sent chatId that matches, waiting for it. */
async function awaitReply(
  page: Page,
  botId: string,
  chatId: string,
  match: (text: string) => boolean,
  what: string
): Promise<string> {
  let found = ""
  await expect
    .poll(
      async () => {
        found = (await sent(page, botId, chatId)).find((m) => match(m.text))?.text ?? ""
        return found !== ""
      },
      { message: `bot ${botId} never sent ${what}`, timeout: 60_000, intervals: [500, 1_000, 2_000] }
    )
    .toBe(true)
  return found
}

async function deleteQuietly(page: Page, path: string, current: Session): Promise<void> {
  const res = await page.request.delete(path, { headers: { Authorization: `Bearer ${current.token}` } })
  if (!res.ok() && res.status() !== 404) console.log(`[e2e] cleanup DELETE ${path} → ${res.status()}`)
}

test("a linked member talks to a published Space Assistant through its own bot and is answered from an escalated Issue", async ({ page }) => {
  // A real turn runs between the message and the reply, on top of linking,
  // creating, binding, and publishing in the browser.
  test.setTimeout(180_000)
  const current = await session(page)
  const links = await getJSON<ChannelLinks>(page, `${current.apiBase}/api/channel-links`, current)
  test.skip(
    !links.platforms.some((p) => p.bot_handle === `@smoke${SYSTEM_BOT}_bot`),
    "the system bot is not the mock's, so its messages cannot be read back"
  )

  const fromId = freshID("7")
  const assistantBot = freshID("2")
  const name = tagged("Front desk")
  let linkId = ""
  let teamId = ""
  let assistantId = ""
  try {
    // Linking starts in the chat: the unlinked sender gets a code, which the
    // signed-in person confirms in the Portal after seeing whose account it is.
    await tell(page, SYSTEM_BOT, fromId, "hello")
    const offer = await awaitReply(page, SYSTEM_BOT, fromId, (t) => /code [A-Z0-9]{4}-[A-Z0-9]{4}/.test(t), "a link code")
    const code = /code ([A-Z0-9]{4}-[A-Z0-9]{4})/.exec(offer)![1]
    await page.goto(`/#/account/chat/${code}`)
    const confirm = page.getByRole("dialog", { name: "Confirm chat link" })
    await expect(confirm).toContainText(`@e2e_${fromId}`)
    await confirm.getByRole("button", { name: "Link account" }).click()
    await expect(page.getByRole("status").filter({ hasText: "Linked Telegram account" })).toBeVisible()
    await awaitReply(page, SYSTEM_BOT, fromId, (t) => t.startsWith("Linked to your BuildMax account"), "the link confirmation")
    const linked = await getJSON<ChannelLinks>(page, `${current.apiBase}/api/channel-links`, current)
    linkId = linked.links.find((l) => l.handle === `@e2e_${fromId}`)?.id ?? ""
    expect(linkId, "the server did not record the link").toBeTruthy()

    // Assistants live only in team Spaces. The reload lets the switcher know it.
    const team = await createSpace(page, current, tagged("Assistant chat"))
    teamId = team.id
    reportLeftovers(team.id, [`space ${team.id}`])
    await page.reload()
    await page.goto(`/#/spaces/${team.id}/settings/assistants`)
    await page.getByRole("region", { name: "Assistants" }).getByRole("button", { name: "New assistant" }).click()
    const form = page.getByRole("form", { name: "New assistant" })
    await form.getByLabel("Name", { exact: true }).fill(name)
    await form.getByRole("button", { name: "Create assistant" }).click()
    const detail = page.getByRole("region", { name })
    await expect(detail.getByRole("heading", { name, level: 2 })).toBeVisible()

    // Binding checks the token with the Bot API, which names the bot.
    await detail.getByLabel("Telegram bot token").fill(`${assistantBot}:${freshID("s")}`)
    await detail.getByRole("button", { name: "Bind bot" }).click()
    // The handle arrives with its "@", so the page must not add another.
    await expect(detail).toContainText(`@smoke${assistantBot}_bot on Telegram`)
    await expect(detail).not.toContainText("@@")

    await detail.getByRole("group", { name: "Assistant actions" }).getByRole("button", { name: "Publish" }).click()
    const dialog = page.getByRole("dialog", { name: "Confirm what this assistant discloses" })
    await dialog.getByRole("button", { name: "Publish" }).click()
    await expect(dialog).toBeHidden()
    await expect(detail.getByTestId("assistant-availability").first()).toHaveText("Available")

    const assistants = await getJSON<StoredAssistant[]>(page, `${current.apiBase}/api/spaces/${team.id}/assistants`, current)
    const stored = assistants.find((a) => a.name === name)
    expect(stored?.binding?.bot_handle, "the server did not record the binding").toBe(`@smoke${assistantBot}_bot`)
    assistantId = stored!.id

    // The person messages the Assistant's own bot. Its first answer says who
    // operates it and who can read along, then gives the model's reply.
    await tell(page, assistantBot, fromId, "What are your opening hours?")
    const reply = await awaitReply(page, assistantBot, fromId, (t) => t.includes(SMOKE_REPLY), "the assistant's answer")
    expect(reply).toContain(`${name} is operated by`)
    expect(reply).toContain("can review this conversation")

    // The Space can review it: the conversation is the Assistant's, in the Space.
    const list = await getJSON<ConversationList>(page, `${current.apiBase}/api/spaces/${team.id}/conversations`, current)
    const conv = list.conversations.find((c) => c.assistant_id === assistantId)
    expect(conv, "the Space does not list the assistant's conversation").toBeTruthy()
    expect(conv!.channel).toBe("telegram")

    // A request the Assistant cannot answer becomes an Issue that a member
    // answers. The mock model calls Escalate only for the message carrying the
    // marker, so no other spec's turn can take the armed call.
    const marker = `refund${freshID("9")}`
    const arm = await page.request.post("/smoke-llm/control/toolcall", {
      data: { name: "Escalate", args: { summary: `Asks for a refund on order 42 (${marker})` }, match: marker },
    })
    expect(arm.ok(), `arm Escalate → ${arm.status()} ${await arm.text()}`).toBeTruthy()
    await tell(page, assistantBot, fromId, `I want a refund for order 42 ${marker}`)
    let issueId = ""
    await expect
      .poll(
        async () => {
          const res = await getJSON<IssueList>(page, `${current.apiBase}/api/spaces/${team.id}/issues`, current)
          issueId = res.issues.find((i) => i.assistant_id === assistantId)?.id ?? ""
          return issueId
        },
        { message: "the escalation opened no Issue", timeout: 60_000, intervals: [500, 1_000, 2_000] }
      )
      .not.toBe("")

    // The Issue says where it came from, and its Discussion answers the
    // requester in the chat through the Assistant's bot.
    await page.goto(`/#/spaces/${team.id}/issues/${issueId}`)
    await expect(page.getByTestId("issue-escalation")).toContainText(`Escalated by ${name} for you`)
    await page.getByRole("navigation", { name: "Issue sections" }).getByRole("button", { name: "Discussion" }).click()
    const answer = `Refund for order 42 approved (${marker})`
    await page.getByPlaceholder("Write a comment").fill(answer)
    await page.getByRole("button", { name: "Reply to requester" }).click()
    await expect(page.getByText(`Replied to the requester through ${name}:`)).toBeVisible()
    await awaitReply(page, assistantBot, fromId, (t) => t === answer, "the member's reply")
  } finally {
    // An attached cluster keeps running: a bound bot would be polled for good,
    // and the link would keep acting as the test account.
    if (assistantId) {
      await deleteQuietly(page, `${current.apiBase}/api/spaces/${teamId}/assistants/${assistantId}/binding`, current)
    }
    if (linkId) await deleteQuietly(page, `${current.apiBase}/api/channel-links/${linkId}`, current)
  }
})
