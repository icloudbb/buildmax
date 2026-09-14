import { expect, test } from "@playwright/test"

import { postJSON, reportLeftovers, session, tagged } from "./fixtures"

/**
 * `agent_task` is the only step type the runtime executes, so the normal
 * Workflow editor presents an Agent step rather than a free-form Type field,
 * and a step's id is generated rather than typed. The advanced JSON view
 * exists for exact inspection, not as a second, unchecked way to build the
 * same workflow -- both paths run the same validation before Save is
 * enabled. None of that is provable without actually rendering the form:
 * a handler test can assert the API rejects a bad definition, not that the
 * Portal form never lets someone type one in the first place.
 */

test("the Agent-step form has no free-form Type field or editable step id, and creates a workflow", async ({
  page,
}) => {
  const current = await session(page)
  const agentName = tagged("Workflow authoring probe agent")
  const agent = await postJSON<{ id: string }>(page, `${current.space}/agents`, current, {
    name: agentName,
    description: "Created by the Portal browser tests.",
    instructions: "Reply with exactly: deployment smoke ok",
  })
  reportLeftovers(current.spaceId, [`agent ${agent.id}`])

  await page.goto(`/#/spaces/${current.spaceId}/workflows`)
  await page.getByRole("button", { name: "New Workflow" }).click()

  const dialog = page.getByRole("dialog", { name: "New Workflow" })
  await expect(dialog).toBeVisible()

  // The step card names what it is -- an Agent step -- and shows its
  // generated id as read-only text, not as an input a person could edit.
  await expect(dialog.getByText("Agent Step 1", { exact: true })).toBeVisible()
  await expect(dialog.getByText(/^id: step_/)).toBeVisible()
  await expect(dialog.getByLabel("Type")).toHaveCount(0)
  await expect(dialog.getByLabel("Step ID")).toHaveCount(0)

  await dialog.getByLabel("Name").fill(tagged("Workflow authoring probe"))
  await dialog.getByLabel("Agent").selectOption({ label: `${agentName} (${agent.id})` })
  await dialog.getByLabel("Prompt").fill("Reply with exactly: deployment smoke ok")

  const submit = dialog.getByRole("button", { name: "Create workflow" })
  await expect(submit).toBeEnabled()
  await submit.click()

  await expect(dialog).toBeHidden()
  // Creating a workflow navigates to its detail page (Workflows.tsx handleCreate),
  // where the name appears in both the breadcrumb and the narrow compact header;
  // assert the landing route rather than a name that now resolves to two elements.
  await expect(page).toHaveURL(new RegExp(`#/spaces/${current.spaceId}/workflows/[^/]+$`))
})

test("advanced JSON mode is checked against the same validation as the step form", async ({ page }) => {
  const current = await session(page)
  const agent = await postJSON<{ id: string }>(page, `${current.space}/agents`, current, {
    name: tagged("Workflow authoring validation agent"),
    description: "Created by the Portal browser tests.",
    instructions: "Reply with exactly: deployment smoke ok",
  })
  reportLeftovers(current.spaceId, [`agent ${agent.id}`])

  await page.goto(`/#/spaces/${current.spaceId}/workflows`)
  await page.getByRole("button", { name: "New Workflow" }).click()

  const dialog = page.getByRole("dialog", { name: "New Workflow" })
  await dialog.getByLabel("Name").fill(tagged("Workflow authoring validation"))

  await dialog.getByRole("button", { name: "Advanced: edit raw JSON" }).click()
  const definitionField = dialog.getByLabel(/^Definition \(JSON\)/)
  await expect(definitionField).toBeVisible()

  // A step type this Portal build does not know how to run -- exactly what
  // the normal form makes impossible to type in the first place.
  await definitionField.fill(
    JSON.stringify({
      schema_version: 1,
      steps: [{ step_id: "s1", type: "shell_command", target_agent_id: agent.id, prompt: "rm -rf /" }],
    })
  )

  const submit = dialog.getByRole("button", { name: "Create workflow" })
  await expect(submit).toBeDisabled()
  await expect(dialog.getByText(/shell_command.*not.*support/i)).toBeVisible()

  // The same JSON, with the one field the runtime actually supports, is
  // accepted -- proving the block above was the type, not the JSON mode.
  await definitionField.fill(
    JSON.stringify({
      schema_version: 1,
      steps: [{ step_id: "s1", type: "agent_task", target_agent_id: agent.id, prompt: "Reply with exactly: deployment smoke ok" }],
    })
  )
  await expect(submit).toBeEnabled()
})

test("the step form authors an input binding to an earlier step, and it persists", async ({ page }) => {
  const current = await session(page)
  const agentName = tagged("Workflow binding form agent")
  const agent = await postJSON<{ id: string }>(page, `${current.space}/agents`, current, {
    name: agentName,
    description: "Created by the Portal browser tests.",
    instructions: "Reply with exactly: deployment smoke ok",
  })
  reportLeftovers(current.spaceId, [`agent ${agent.id}`])

  // The New Workflow modal grows with each step and does not scroll on the
  // desktop layout, so a two-step form needs a viewport tall enough to keep
  // the second step's binding control and the submit button on screen.
  await page.setViewportSize({ width: 1280, height: 1800 })

  await page.goto(`/#/spaces/${current.spaceId}/workflows`)
  await page.getByRole("button", { name: "New Workflow" }).click()
  const dialog = page.getByRole("dialog", { name: "New Workflow" })
  await expect(dialog).toBeVisible()

  await dialog.getByLabel("Name").fill(tagged("Workflow binding form"))

  const stepCards = dialog.locator(".workflow-page__step")
  // Step 1 is the source the second step reads from. A binding control cannot
  // exist here: there is no earlier step to bind.
  await stepCards.nth(0).getByLabel("Agent").selectOption({ label: `${agentName} (${agent.id})` })
  await stepCards.nth(0).getByLabel("Prompt").fill("Reply with exactly: deployment smoke ok")
  await expect(stepCards.nth(0).getByRole("button", { name: "Add input" })).toHaveCount(0)

  await dialog.getByRole("button", { name: "Add Agent Step" }).click()
  await stepCards.nth(1).getByLabel("Agent").selectOption({ label: `${agentName} (${agent.id})` })
  await stepCards.nth(1).getByLabel("Prompt").fill("Summarize the research below.")

  // The generated id is the value the source-step select carries, so read it
  // off step 1's card rather than assuming it.
  const step1IdText = await stepCards.nth(0).getByText(/^id: step_/).textContent()
  const step1Id = step1IdText!.replace(/^id:\s*/, "").trim()

  await stepCards.nth(1).getByRole("button", { name: "Add input" }).click()
  await stepCards.nth(1).getByLabel("Input 1 name").fill("research")
  await stepCards.nth(1).getByLabel("Input 1 source step").selectOption(step1Id)

  const submit = dialog.getByRole("button", { name: "Create workflow" })
  await expect(submit).toBeEnabled()
  await submit.click()
  await expect(dialog).toBeHidden()
  await expect(page).toHaveURL(new RegExp(`#/spaces/${current.spaceId}/workflows/[^/]+$`))

  // The binding the form authored survives the round-trip: the saved definition,
  // reopened in advanced JSON, carries the wire shape the runtime reads.
  await page.getByRole("button", { name: "Advanced: edit raw JSON" }).click()
  await expect(page.getByLabel(/^Definition \(JSON\)/)).toHaveValue(
    new RegExp(`"from_step":\\s*"${step1Id}"`)
  )
})
