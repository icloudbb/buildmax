import { expect, test, type Page } from "@playwright/test"

import { createSpace, postJSON, reportLeftovers, session, tagged, type Session } from "./fixtures"

/**
 * A new person's first Issue, in a Space with nothing that can run it. The
 * Issue itself has to say that an Agent or a published Workflow runs it, lead
 * to creating one, and keep the person on the Issue when the run starts --
 * a path only a real render can prove, because each step is what the page
 * says and where it leaves the person, not what the API accepts.
 */

/** A fresh Space, so the account's seeded Agents and Workflows are not offered. */
async function openNewSpace(page: Page, name: string): Promise<Session> {
  const current = await session(page)
  const created = await createSpace(page, current, tagged(name))
  reportLeftovers(current.spaceId, [`space ${created.id} (no delete route)`])
  // The Space list loads once when the app mounts; one created through the API
  // is unknown to the running session until it remounts.
  await page.reload()
  return { ...current, spaceId: created.id, space: `${current.apiBase}/api/spaces/${encodeURIComponent(created.id)}` }
}

test("a new person goes from New Issue to a started run without leaving the Issue", async ({ page }) => {
  const { spaceId } = await openNewSpace(page, "Executor path space")
  await page.goto(`/#/spaces/${spaceId}/issues`)

  await page.getByRole("button", { name: "New Issue" }).click()
  const issueDialog = page.getByRole("dialog", { name: "New Issue" })
  const executor = issueDialog.getByLabel("Executor")
  await expect(executor.locator("option")).toHaveText(["None"])
  await expect(
    issueDialog.getByText(
      "Nothing in this Space can run it yet: it needs an Agent or a published Workflow. You can create an Agent now.",
    ),
  ).toBeVisible()

  const title = tagged("Executor path probe")
  await issueDialog.getByLabel("Title").fill(title)

  // Creating the Agent swaps dialogs and comes back to the same draft.
  await issueDialog.getByRole("button", { name: "Create an Agent" }).click()
  const agentDialog = page.getByRole("dialog", { name: "New Agent" })
  await expect(agentDialog).toBeVisible()
  await expect(agentDialog.getByText("belong to this Space", { exact: false })).toBeVisible()
  const agentName = tagged("Executor path agent")
  await agentDialog.getByLabel("Name", { exact: true }).fill(agentName)
  await agentDialog.getByRole("button", { name: "Create agent" }).click()
  await expect(agentDialog).toBeHidden()

  await expect(issueDialog).toBeVisible()
  await expect(issueDialog.getByLabel("Title")).toHaveValue(title)
  await expect(executor.locator("option:checked")).toHaveText(agentName)
  await expect(issueDialog.getByText("An Agent or a published Workflow runs the work.")).toBeVisible()
  await issueDialog.getByRole("button", { name: "Create issue" }).click()

  await expect(page.getByRole("heading", { name: title, level: 1 })).toBeVisible()
  const issueUrl = page.url()
  expect(issueUrl).toMatch(new RegExp(`#/spaces/${spaceId}/issues/[^/]+$`))

  await page.getByRole("button", { name: "Run agent" }).click()

  // Run keeps the person on the Issue, says the run started, and links to it.
  const started = page.getByRole("status").filter({ hasText: "Run started." })
  await expect(started).toBeVisible()
  await expect(page).toHaveURL(issueUrl)
  const startedLink = started.getByRole("link", { name: "Open Task" })
  await expect(startedLink).toHaveAttribute("href", new RegExp(`^#/spaces/${spaceId}/tasks/[^/]+$`))
  const taskHref = await startedLink.getAttribute("href")
  const taskId = taskHref?.split("/tasks/")[1] ?? ""

  // The Overview's latest run is that run, with the same link.
  const outcome = page.getByRole("region", { name: "Latest run" })
  await expect(outcome).toContainText(`Agent run · ${agentName}`)
  await expect(outcome.getByRole("link", { name: "Open Task" })).toHaveAttribute("href", taskHref ?? "")

  await startedLink.click()
  await expect(page).toHaveURL(new RegExp(`#/spaces/${spaceId}/tasks/${taskId}$`))
})

test("an Issue with no executor says why it cannot run and creates its Agent in place", async ({ page }) => {
  const current = await openNewSpace(page, "Executor reason space")
  const { spaceId } = current
  const title = tagged("Executor reason probe")
  const issue = await postJSON<{ id: string }>(page, `${current.space}/issues`, current, { title })
  reportLeftovers(spaceId, [`issue ${issue.id}`])

  const issueUrl = new RegExp(`#/spaces/${spaceId}/issues/${issue.id}$`)
  await page.goto(`/#/spaces/${spaceId}/issues/${issue.id}`)
  await expect(page.getByRole("heading", { name: title, level: 1 })).toBeVisible()

  const run = page.getByRole("button", { name: "Run", exact: true })
  await expect(run).toBeDisabled()
  await expect(run).toHaveAccessibleDescription(
    "This Issue has no executor, and nothing in this Space can run it yet. Create an Agent to run it.",
  )

  await page.getByRole("button", { name: "Create an Agent" }).click()
  const agentDialog = page.getByRole("dialog", { name: "New Agent" })
  const agentName = tagged("Executor reason agent")
  await agentDialog.getByLabel("Name", { exact: true }).fill(agentName)
  await agentDialog.getByRole("button", { name: "Create agent" }).click()
  await expect(agentDialog).toBeHidden()
  await expect(page).toHaveURL(issueUrl)

  // The new Agent is chosen but not saved: assigning it stays the person's Save.
  const executor = page.getByLabel("Executor")
  await expect(executor).toBeFocused()
  await expect(executor.locator("option:checked")).toHaveText(agentName)
  await expect(
    page.getByText(`Created ${agentName}. Save to make it this Issue's executor, then run it.`, { exact: true }),
  ).toHaveRole("status")

  await page.getByRole("button", { name: "Save changes" }).click()
  await expect(
    page.getByText("Saved. Saving does not start a run — choose Run agent to start one.", { exact: true }),
  ).toHaveRole("status")
  await expect(page.getByRole("button", { name: "Run agent" })).toBeEnabled()
  await expect(page).toHaveURL(issueUrl)
})
