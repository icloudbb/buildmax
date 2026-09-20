import { expect, test } from "@playwright/test"

import { postJSON, reportLeftovers, session, tagged } from "./fixtures"

/**
 * `agent_task` is the only step type the runtime executes, so the visual editor
 * presents an Agent step in its node inspector rather than a free-form Type
 * field, and a step's id is generated rather than typed. The raw JSON view
 * exists for exact inspection and for fields the visual editor does not author,
 * not as a second, unchecked way to build the same workflow -- both paths run
 * the same validation before Save is enabled. None of that is provable without
 * actually rendering the editor: a handler test can assert the API rejects a bad
 * definition, not that the Portal never lets someone author one.
 */

test("the node inspector has an editable step id and no free-form Type field, and creates a workflow", async ({
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

  // The canvas opens with one step node; the inspector edits the selected step,
  // shows its generated id in an editable "Step id" field the author can rename,
  // and has no free-form Type field.
  await expect(dialog.locator(".wf-node")).toHaveCount(1)
  await expect(dialog.getByLabel("Step id")).toHaveValue(/^step_/)
  await expect(dialog.getByLabel("Type")).toHaveCount(0)

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

test("raw JSON mode is checked against the same validation as the visual editor", async ({ page }) => {
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

  await dialog.getByRole("button", { name: "Edit raw JSON" }).click()
  const definitionField = dialog.getByLabel(/^Definition \(JSON\)/)
  await expect(definitionField).toBeVisible()

  // A step type this Portal build does not know how to run -- exactly what the
  // visual editor makes impossible to author in the first place.
  await definitionField.fill(
    JSON.stringify({
      schema_version: 1,
      nodes: [{ id: "s1", type: "shell_command", agent: { id: agent.id }, input: { instruction: "rm -rf /" } }],
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
      nodes: [{ id: "s1", type: "agent_task", agent: { id: agent.id }, input: { instruction: "Reply with exactly: deployment smoke ok" } }],
    })
  )
  await expect(submit).toBeEnabled()
})

test("the visual editor renders a branching graph, adds a step, and round-trips a binding", async ({ page }) => {
  const current = await session(page)
  const agentName = tagged("Workflow branch agent")
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
  await dialog.getByLabel("Name").fill(tagged("Workflow branch"))

  // Author a fan-out DAG (two steps both depending on the first) with a binding,
  // through raw JSON -- a branch the retired linear form could not express.
  await dialog.getByRole("button", { name: "Edit raw JSON" }).click()
  await dialog.getByLabel(/^Definition \(JSON\)/).fill(
    JSON.stringify({
      schema_version: 1,
      nodes: [
        { id: "collect", type: "agent_task", agent: { id: agent.id }, input: { instruction: "Collect the sources." } },
        {
          id: "analyze",
          type: "agent_task",
          needs: ["collect"],
          agent: { id: agent.id },
          input: {
            instruction: "Analyze them.",
            bindings: [{ name: "research", source: "node.collect.output", pointer: "/text" }],
          },
        },
        { id: "draft", type: "agent_task", needs: ["collect"], agent: { id: agent.id }, input: { instruction: "Draft a summary." } },
      ],
    })
  )
  await dialog.getByRole("button", { name: "Create workflow" }).click()
  await expect(dialog).toBeHidden()
  await expect(page).toHaveURL(new RegExp(`#/spaces/${current.spaceId}/workflows/[^/]+$`))

  // A draft opens on its Definition tab, which is the visual editor: the graph
  // renders every node and both fan-out edges.
  await expect(page.locator(".wf-node")).toHaveCount(3)
  await expect(page.locator(".react-flow__edge")).toHaveCount(2)

  // Adding a step through the canvas toolbar grows the graph.
  await page.getByRole("button", { name: "Add step" }).click()
  await expect(page.locator(".wf-node")).toHaveCount(4)

  // Renaming a node's id in the inspector rewrites every reference to it: the
  // downstream `needs` edges and the binding that reads its output.
  await page.locator(".wf-node", { hasText: "collect" }).click()
  const idField = page.getByLabel("Step id")
  await expect(idField).toHaveValue("collect")
  await idField.fill("gather")
  await idField.press("Enter")
  await expect(page.locator(".wf-node", { hasText: "gather" })).toBeVisible()

  // Switching to raw JSON carries the renamed wire shape the runtime reads: the
  // node id, the edges that pointed at it, and the binding source it feeds.
  await page.getByRole("button", { name: "Edit raw JSON" }).click()
  const raw = page.getByLabel(/^Definition \(JSON\)/)
  await expect(raw).toHaveValue(/"id":\s*"gather"/)
  await expect(raw).toHaveValue(/"needs":\s*\[\s*"gather"\s*\]/)
  await expect(raw).toHaveValue(/"source":\s*"node\.gather\.output"/)
})
