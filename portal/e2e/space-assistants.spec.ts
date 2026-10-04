import AxeBuilder from "@axe-core/playwright"
import { expect, test, type Page } from "@playwright/test"

import { createSpace, getJSON, reportLeftovers, session, tagged } from "./fixtures"

/**
 * A Space Assistant is published only after its owner confirms the statement
 * of what it discloses (docs/design/space-assistants.md §8). Handler tests
 * prove the server refuses an unconfirmed publish; what this spec adds is that
 * the Portal reads that refusal, shows the statement, and sends back exactly
 * the digest it showed, so the confirmation a person makes is the one the
 * server records.
 *
 * Binding a bot needs a reachable Telegram Bot API. kind runs a double of one,
 * and assistant-chat.spec.ts binds a bot and talks to it there.
 */

/** WCAG A/AA over one region, on the same terms as accessibility.spec.ts. */
async function expectAccessible(page: Page, include: string) {
  const results = await new AxeBuilder({ page })
    .include(include)
    .withTags(["wcag2a", "wcag2aa", "wcag21a", "wcag21aa"])
    .disableRules(["color-contrast"])
    .analyze()
  const found = results.violations.map((v) => `${v.id}: ${v.nodes.map((n) => n.target.join(" ")).join(", ")}`)
  expect(found, found.join("\n")).toEqual([])
}

interface StoredAssistant {
  id: string
  name: string
  state: string
  availability: string
  service_account_id: string
}

test("a space owner creates an assistant and publishes it through the disclosure statement", async ({ page }) => {
  // Assistants live only in team Spaces, so the spec makes its own. The page
  // is reloaded so the switcher knows the Space.
  const current = await session(page)
  const team = await createSpace(page, current, tagged("Assistant probe"))
  reportLeftovers(team.id, [`space ${team.id}`])
  await page.reload()
  await page.goto(`/#/spaces/${team.id}/settings/assistants`)

  const list = page.getByRole("region", { name: "Assistants" })
  await list.getByRole("button", { name: "New assistant" }).click()
  const form = page.getByRole("form", { name: "New assistant" })
  const name = tagged("Probe desk")
  await form.getByLabel("Name", { exact: true }).fill(name)
  await expectAccessible(page, ".asst")
  await form.getByRole("button", { name: "Create assistant" }).click()

  // Created paused: publishing is a separate step.
  const detail = page.getByRole("region", { name })
  await expect(detail.getByRole("heading", { name, level: 2 })).toBeVisible()
  await expect(detail.getByTestId("assistant-availability").first()).toHaveText("Paused")

  await detail.getByRole("group", { name: "Assistant actions" }).getByRole("button", { name: "Publish" }).click()
  const dialog = page.getByRole("dialog", { name: "Confirm what this assistant discloses" })
  await expect(dialog).toContainText("Publishing is a disclosure decision")
  // The server's own sentence, not a Portal paraphrase.
  await expect(dialog).toContainText("Any member of this Space can ask this Assistant.")
  await expect(dialog).toContainText("It can run no Agents or Workflows.")
  await expectAccessible(page, ".modal-overlay")
  await dialog.getByRole("button", { name: "Publish" }).click()
  await expect(dialog).toBeHidden()
  await expect(detail.getByTestId("assistant-availability").first()).toHaveText("Available")
  await expectAccessible(page, ".asst")

  // What the page shows is what the server holds, not local state.
  const path = `${current.apiBase}/api/spaces/${team.id}/assistants`
  let stored = (await getJSON<StoredAssistant[]>(page, path, current)).find((a) => a.name === name)
  expect(stored?.state, "the server did not record the publish").toBe("active")
  expect(stored?.availability).toBe("available")
  // No service account was named, so one was made for it.
  expect(stored?.service_account_id).toBeTruthy()

  await detail.getByRole("group", { name: "Assistant actions" }).getByRole("button", { name: "Pause" }).click()
  await expect(detail.getByTestId("assistant-availability").first()).toHaveText("Paused")
  stored = (await getJSON<StoredAssistant[]>(page, path, current)).find((a) => a.name === name)
  expect(stored?.state, "the server did not record the pause").toBe("paused")
  expect(stored?.availability).toBe("paused")

  // The list reads it back from the server too.
  await page.goto(`/#/spaces/${team.id}/settings/assistants`)
  const card = page.getByTestId("assistant").filter({ hasText: name })
  await expect(card.getByTestId("assistant-availability")).toHaveText("Paused")
  await expect(card).toContainText("no bot bound")
})
