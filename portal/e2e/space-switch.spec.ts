import { expect, test, type Page } from "@playwright/test"

import { createSpace, postJSON, reportLeftovers, session, tagged } from "./fixtures"

/**
 * docs/design/portal-navigation-and-space-context.md requires every
 * Space-owned route to redirect to a valid destination in the target Space on
 * switch, never leaving stale data on screen. Before this, the redirect list
 * in App.tsx was a hand-maintained if-chain that had already dropped Agent and
 * Task -- switching Space while viewing either did nothing. `spaceSwitchTarget`
 * (portal/src/lib/spaceSwitch.ts) replaces it with an exhaustive, type-checked
 * table; this spec is the browser-level proof that switching actually drives
 * the UI, not just that the pure function returns the right Route.
 */

/**
 * The persistent sidebar's own Space switcher, at the default desktop
 * viewport this spec runs at. The narrow drawer mounts a second, identically
 * labeled `<select>` in the same DOM (see golden-path-viewports.spec.ts), so
 * scope to the sidebar landmark rather than matching the label alone.
 */
function spaceSwitcher(page: Page) {
  return page.getByLabel("Sidebar", { exact: true }).getByLabel("Space", { exact: true })
}

/** Select `spaceId` in the sidebar's Space switcher and wait for the hash to move. */
async function switchSpace(page: Page, spaceId: string): Promise<void> {
  await spaceSwitcher(page).selectOption(spaceId)
  await page.waitForFunction(
    (id) => window.location.hash.includes(`/spaces/${id}/`),
    spaceId,
  )
}

test("switching Space from Issue, Agent, Workflow, Workflow Run, and Task detail lands on a valid destination with no stale data", async ({
  page,
}) => {
  const current = await session(page)
  const second = await createSpace(page, current, tagged("Space switch probe"))
  console.log(`[e2e] run left a space: ${second.id} (${tagged("Space switch probe")})`)
  // Created through the API, not the app: SpaceContext only loaded the
  // account's Spaces once, at the initial page load `session()` already did,
  // and a hash-only `page.goto()` never remounts it. Reload once so the
  // switcher actually offers the new Space.
  await page.reload()

  const agent = await postJSON<{ id: string }>(page, `${current.space}/agents`, current, {
    name: tagged("Space switch agent"),
    description: "Created by the Portal browser tests.",
    instructions: "Reply with exactly: deployment smoke ok",
  })
  const issue = await postJSON<{ id: string }>(page, `${current.space}/issues`, current, {
    title: tagged("Space switch issue"),
  })
  const workflow = await postJSON<{ id: string }>(page, `${current.space}/workflows`, current, {
    name: tagged("Space switch workflow"),
    description: "Created by the Portal browser tests.",
    definition: JSON.stringify({
      schema_version: 1,
      nodes: [{ id: "only", type: "agent_task", agent: { id: agent.id }, input: { instruction: "Reply with exactly: ok" } }],
    }),
  })
  const task = await postJSON<{ id: string }>(page, `${current.space}/agents/${agent.id}/tasks`, current, {
    input: "Space switch probe",
  })
  reportLeftovers(current.spaceId, [
    `agent ${agent.id}`,
    `issue ${issue.id}`,
    `workflow ${workflow.id}`,
    `task ${task.id}`,
  ])

  // Issue detail -> the target Space's Issues collection.
  await page.goto(`/#/spaces/${current.spaceId}/issues/${issue.id}`)
  await switchSpace(page, second.id)
  await expect(page).toHaveURL(new RegExp(`#/spaces/${second.id}/issues$`))
  await expect(page.getByRole("heading", { name: "Issues", exact: true })).toBeVisible()
  await expect(page.getByText(tagged("Space switch issue"))).toHaveCount(0)

  // Agent detail -> the target Space's Agents collection.
  await page.goto(`/#/spaces/${current.spaceId}/agents/${agent.id}`)
  await switchSpace(page, second.id)
  await expect(page).toHaveURL(new RegExp(`#/spaces/${second.id}/agents$`))
  await expect(page.getByRole("heading", { name: "Agents", exact: true })).toBeVisible()

  // Workflow detail -> the target Space's Workflows collection.
  await page.goto(`/#/spaces/${current.spaceId}/workflows/${workflow.id}`)
  await switchSpace(page, second.id)
  await expect(page).toHaveURL(new RegExp(`#/spaces/${second.id}/workflows$`))
  await expect(page.getByRole("heading", { name: "Workflows", exact: true })).toBeVisible()

  // Task detail has no collection page of its own -- it lands on Chat. The
  // composer has no page heading, so the breadcrumb is the marker.
  await page.goto(`/#/spaces/${current.spaceId}/tasks/${task.id}`)
  await switchSpace(page, second.id)
  await expect(page).toHaveURL(new RegExp(`#/spaces/${second.id}/chat$`))
  await expect(page.getByLabel("Breadcrumb").getByText("Chat", { exact: true })).toBeVisible()
})

test("switching Space from a global route changes nothing", async ({ page }) => {
  const current = await session(page)
  const second = await createSpace(page, current, tagged("Space switch probe 2"))
  console.log(`[e2e] run left a space: ${second.id} (${tagged("Space switch probe 2")})`)

  await page.goto("/#/account")
  // Reload once so SpaceContext (loaded before this Space existed) refetches
  // and the switcher offers it -- see the comment in the other test.
  await page.reload()
  await expect(page.getByRole("heading", { name: "General", exact: true })).toBeVisible()

  // Global routes carry no Space prefix at all, so there is no hash change to
  // wait for here -- confirm the switcher itself moved, then that the route
  // (and everything on the page) did not.
  await spaceSwitcher(page).selectOption(second.id)
  await expect(spaceSwitcher(page)).toHaveValue(second.id)
  await expect(page).toHaveURL(/#\/account$/)
  await expect(page.getByRole("heading", { name: "General", exact: true })).toBeVisible()
})
