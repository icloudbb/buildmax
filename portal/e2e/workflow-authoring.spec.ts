import { expect, test, type Page } from "@playwright/test"

import { postJSON, reportLeftovers, session, tagged, type Session } from "./fixtures"

/**
 * `agent_task` is the only step type the runtime executes, so the visual editor
 * presents an Agent step in its node inspector rather than a free-form Type
 * field, and a step's id is generated rather than typed. The raw JSON view
 * exists for exact inspection and for fields the visual editor does not author,
 * not as a second, unchecked way to build the same workflow -- both paths run
 * the same validation before Save is enabled. None of that is provable without
 * actually rendering the editor: a handler test can assert the API rejects a bad
 * definition, not that the Portal never lets someone author one.
 *
 * A new Workflow is authored on a page of its own, sized for its graph
 * (docs/design/portal-workflow-visual-editor.md), and every step reads as
 * "Step N" and its Agent's name rather than as a generated id.
 */

async function createAgent(page: Page, current: Session, name: string): Promise<{ id: string }> {
  const agent = await postJSON<{ id: string }>(page, `${current.space}/agents`, current, {
    name,
    description: "Created by the Portal browser tests.",
    instructions: "Reply with exactly: deployment smoke ok",
  })
  reportLeftovers(current.spaceId, [`agent ${agent.id}`])
  return agent
}

async function openNewWorkflow(page: Page, current: Session): Promise<void> {
  await page.goto(`/#/spaces/${current.spaceId}/workflows`)
  await page.getByRole("button", { name: "New Workflow" }).click()
  await expect(page).toHaveURL(new RegExp(`#/spaces/${current.spaceId}/workflows/new$`))
  await expect(page.getByRole("heading", { level: 1, name: "New Workflow" })).toBeVisible()
}

const GENERATED_STEP_ID = /step_[0-9a-f]{8}/

test("a new Workflow is authored on its own page, with the step id kept out of the form", async ({ page }) => {
  const current = await session(page)
  const agentName = tagged("Workflow authoring probe agent")
  await createAgent(page, current, agentName)

  await openNewWorkflow(page, current)
  // The editor is a page, not a dialog, and the canvas opens with one step.
  await expect(page.getByRole("dialog")).toHaveCount(0)
  await expect(page.locator(".wf-node")).toHaveCount(1)
  await expect(page.locator(".wf-node")).toContainText("Step 1")
  await expect(page.getByLabel("Type")).toHaveCount(0)

  // The generated id is behind the inspector's Advanced disclosure, renamable
  // there, and named nowhere else on the page.
  const stepId = page.getByLabel("Step ID")
  await expect(stepId).toBeHidden()
  await expect(page.locator(".wf-visual")).not.toContainText(GENERATED_STEP_ID)
  await page.locator(".wf-inspector__advanced summary").click()
  await expect(stepId).toHaveValue(/^step_/)

  await page.getByLabel("Name", { exact: true }).fill(tagged("Workflow authoring probe"))
  await page.getByLabel("Agent", { exact: true }).selectOption({ label: agentName })
  await page.getByLabel("Prompt").fill("Reply with exactly: deployment smoke ok")

  const submit = page.getByRole("button", { name: "Create workflow" })
  await expect(submit).toBeEnabled()
  await submit.click()
  // Creating a workflow navigates to its detail page.
  await expect(page).toHaveURL(new RegExp(`#/spaces/${current.spaceId}/workflows/(?!new$)[^/]+$`))
})

test("authoring a two-step Workflow names steps, Agents, and Issue access in a person's terms", async ({ page }) => {
  const current = await session(page)
  const agentName = tagged("Workflow two-step agent")
  const agent = await createAgent(page, current, agentName)

  await openNewWorkflow(page, current)
  // Create stays disabled until the Workflow has a name, and says so.
  const submit = page.getByRole("button", { name: "Create workflow" })
  await expect(submit).toBeDisabled()
  await expect(submit).toHaveAccessibleDescription("Name the Workflow to create it.")
  await page.getByLabel("Name", { exact: true }).fill(tagged("Workflow two-step"))

  await page.getByLabel("Agent", { exact: true }).selectOption({ label: agentName })
  await page.getByRole("button", { name: "Add step" }).click()
  await expect(page.locator(".wf-node")).toHaveCount(2)
  // The added step is selected; it is "Step 2", and its Agent picker lists
  // Agents by name alone.
  await expect(page.locator(".wf-inspector__title")).toHaveText("Step 2")
  const agentPicker = page.getByLabel("Agent", { exact: true })
  await expect(agentPicker.locator("option")).toContainText([agentName])
  await expect(agentPicker).not.toContainText(agent.id)
  await agentPicker.selectOption({ label: agentName })

  // Issue access offers person-facing modes, each with a one-line hint.
  const issueAccess = page.getByLabel("Issue access")
  await expect(issueAccess.locator("option")).toHaveText([
    "No Issue access",
    "Use the run's Issue, if any",
    "Require an Issue",
  ])
  await expect(issueAccess).toHaveAccessibleDescription(/never sees an Issue/)
  await issueAccess.selectOption({ label: "Use the run's Issue, if any" })
  await expect(issueAccess).toHaveAccessibleDescription(/When the run was started from an Issue/)
  await expect(page.locator(".wf-node", { hasText: "Step 2" })).toContainText("Issue if any")

  // Neither node names a generated step id or an Agent id; both read as a
  // numbered step and the Agent's name.
  const nodes = page.locator(".wf-node")
  for (const [i, heading] of ["Step 1", "Step 2"].entries()) {
    await expect(nodes.nth(i).locator(".wf-node__heading")).toHaveText(heading)
    await expect(nodes.nth(i).locator(".wf-node__agent")).toHaveText(agentName)
  }
  const editorText = await page.locator(".wf-visual").innerText()
  expect(editorText).not.toMatch(GENERATED_STEP_ID)
  expect(editorText).not.toContain(agent.id)
  expect(editorText).not.toMatch(/\bif_bound\b/)

  // The wire value is still what is saved.
  await page.getByRole("button", { name: "Edit raw JSON" }).click()
  await expect(page.getByLabel(/^Definition \(JSON\)/)).toHaveValue(/"issue_access":\s*"if_bound"/)
  await page.getByRole("button", { name: "Visual editor" }).click()

  await expect(submit).toBeEnabled()
  await submit.click()
  await expect(page).toHaveURL(new RegExp(`#/spaces/${current.spaceId}/workflows/(?!new$)[^/]+$`))

  // The plan on Overview reads the same way.
  await page.locator(".detail-tabs").getByRole("tab", { name: "Overview" }).click()
  const plan = page.getByRole("tabpanel")
  await expect(plan.locator(".wf-graph__node")).toHaveCount(2)
  await expect(plan.locator(".wf-graph__node").first()).toContainText("Step 1")
  await expect(plan).not.toContainText(GENERATED_STEP_ID)
  await expect(plan).not.toContainText(agent.id)
})

test("raw JSON mode is checked against the same validation as the visual editor", async ({ page }) => {
  const current = await session(page)
  const agent = await createAgent(page, current, tagged("Workflow authoring validation agent"))

  await openNewWorkflow(page, current)
  await page.getByLabel("Name", { exact: true }).fill(tagged("Workflow authoring validation"))

  await page.getByRole("button", { name: "Edit raw JSON" }).click()
  const definitionField = page.getByLabel(/^Definition \(JSON\)/)
  await expect(definitionField).toBeVisible()

  // A step type this Portal build does not know how to run -- exactly what the
  // visual editor makes impossible to author in the first place.
  await definitionField.fill(
    JSON.stringify({
      schema_version: 1,
      nodes: [{ id: "s1", type: "shell_command", agent: { id: agent.id }, input: { instruction: "rm -rf /" } }],
    })
  )

  const submit = page.getByRole("button", { name: "Create workflow" })
  await expect(submit).toBeDisabled()
  await expect(submit).toHaveAccessibleDescription("Fix the problems marked on the steps to create it.")
  await expect(page.getByText(/shell_command.*not.*support/i)).toBeVisible()

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
  const agent = await createAgent(page, current, tagged("Workflow branch agent"))

  await openNewWorkflow(page, current)
  await page.getByLabel("Name", { exact: true }).fill(tagged("Workflow branch"))

  // Author a fan-out DAG (two steps both depending on the first) with a binding,
  // through raw JSON -- a branch the retired linear form could not express.
  await page.getByRole("button", { name: "Edit raw JSON" }).click()
  await page.getByLabel(/^Definition \(JSON\)/).fill(
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
  await page.getByRole("button", { name: "Create workflow" }).click()
  await expect(page).toHaveURL(new RegExp(`#/spaces/${current.spaceId}/workflows/(?!new$)[^/]+$`))

  // A draft opens on its Definition tab, which is the visual editor: the graph
  // renders every node and both fan-out edges.
  await expect(page.locator(".wf-node")).toHaveCount(3)
  await expect(page.locator(".react-flow__edge")).toHaveCount(2)

  // A binding reads an earlier step's output, named by that step's number.
  await page.locator(".wf-node", { hasText: "Step 2" }).click()
  await expect(page.getByLabel("Input 1 source")).toHaveValue("node.collect.output")
  await expect(page.getByLabel("Input 1 source").locator("option:checked")).toHaveText("Step 1 output")

  // Adding a step through the canvas toolbar grows the graph.
  await page.getByRole("button", { name: "Add step" }).click()
  await expect(page.locator(".wf-node")).toHaveCount(4)

  // Renaming a node's id under Advanced rewrites every reference to it: the
  // downstream `needs` edges and the binding that reads its output.
  await page.locator(".wf-node", { hasText: "Step 1" }).click()
  await page.locator(".wf-inspector__advanced summary").click()
  const idField = page.getByLabel("Step ID")
  await expect(idField).toHaveValue("collect")
  await idField.fill("gather")
  await idField.press("Enter")
  await expect(page.locator(".react-flow__edge")).toHaveCount(2)

  // Switching to raw JSON carries the renamed wire shape the runtime reads: the
  // node id, the edges that pointed at it, and the binding source it feeds.
  await page.getByRole("button", { name: "Edit raw JSON" }).click()
  const raw = page.getByLabel(/^Definition \(JSON\)/)
  await expect(raw).toHaveValue(/"id":\s*"gather"/)
  await expect(raw).toHaveValue(/"needs":\s*\[\s*"gather"\s*\]/)
  await expect(raw).toHaveValue(/"source":\s*"node\.gather\.output"/)
})

test("a step's attempts and timeout, and the run timeout, are authored in the editor and saved as policy", async ({
  page,
}) => {
  const current = await session(page)
  const agentName = tagged("Workflow retry policy agent")
  await createAgent(page, current, agentName)

  await openNewWorkflow(page, current)
  await page.getByLabel("Name", { exact: true }).fill(tagged("Workflow retry policy"))
  await page.getByLabel("Agent", { exact: true }).selectOption({ label: agentName })
  await page.getByLabel("Prompt").fill("Reply with exactly: deployment smoke ok")

  // Six attempts is past the runtime's ceiling, so Save stays disabled until
  // the author picks a value publication would accept.
  const submit = page.getByRole("button", { name: "Create workflow" })
  await page.getByLabel("Attempts").fill("6")
  await expect(submit).toBeDisabled()
  await page.getByLabel("Attempts").fill("3")
  await page.getByLabel("Timeout per attempt (min)").fill("10")
  await page.getByLabel("Run timeout (min)").fill("60")
  await expect(page.locator(".wf-node", { hasText: "3 attempts" })).toBeVisible()
  await expect(page.locator(".wf-node", { hasText: "timeout 10m" })).toBeVisible()

  await page.getByRole("button", { name: "Edit raw JSON" }).click()
  const raw = page.getByLabel(/^Definition \(JSON\)/)
  await expect(raw).toHaveValue(/"max_attempts":\s*3/)
  await expect(raw).toHaveValue(/"timeout_seconds":\s*600/)
  await expect(raw).toHaveValue(/"timeout_seconds":\s*3600/)

  await expect(submit).toBeEnabled()
  await submit.click()
  await expect(page).toHaveURL(new RegExp(`#/spaces/${current.spaceId}/workflows/(?!new$)[^/]+$`))
})

test.describe("at 390px", () => {
  test.use({ viewport: { width: 390, height: 844 } })

  async function expectNoHorizontalOverflow(page: Page): Promise<void> {
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
    expect(overflow).toBeLessThanOrEqual(0)
  }

  test("a Workflow's steps and their order read as a list, Run is reachable, and the page says graph editing needs a wider window", async ({
    page,
  }) => {
    const current = await session(page)
    const agentName = tagged("Workflow narrow agent")
    const agent = await createAgent(page, current, agentName)
    const workflow = await postJSON<{ id: string }>(page, `${current.space}/workflows`, current, {
      name: tagged("Workflow narrow"),
      description: "Created by the Portal browser tests to exercise a narrow window.",
      definition: JSON.stringify({
        schema_version: 1,
        nodes: [
          { id: "collect", type: "agent_task", agent: { id: agent.id }, input: { instruction: "Collect the sources." } },
          { id: "summarize", type: "agent_task", needs: ["collect"], agent: { id: agent.id }, input: { instruction: "Summarize them." } },
        ],
      }),
    })
    reportLeftovers(current.spaceId, [`workflow ${workflow.id}`])

    await page.goto(`/#/spaces/${current.spaceId}/workflows/${workflow.id}`)
    // A draft opens on Definition. The canvas gives way to the ordered steps,
    // with a note that connecting steps needs a wider window.
    await expect(page.getByText("Connecting steps on the graph needs a wider window.", { exact: false })).toBeVisible()
    await expect(page.locator(".react-flow")).toHaveCount(0)
    const steps = page.getByRole("list", { name: "Steps" }).getByRole("listitem")
    await expect(steps).toHaveCount(2)
    await expect(steps.nth(0)).toContainText("Step 1")
    await expect(steps.nth(0)).toContainText(agentName)
    await expect(steps.nth(1)).toContainText("Step 2")
    await expect(steps.nth(1)).toContainText("After Step 1")
    // Choosing a step in the list opens it in the inspector.
    await steps.nth(1).getByRole("button").click()
    await expect(page.locator(".wf-inspector__title")).toHaveText("Step 2")
    await expectNoHorizontalOverflow(page)

    // Run is reachable and says why it is disabled.
    const run = page.getByRole("button", { name: "Run Workflow" })
    await run.scrollIntoViewIfNeeded()
    await expect(run).toBeVisible()
    await expect(run).toBeDisabled()
    await expect(run).toHaveAccessibleDescription("Publish this Workflow to run it.")

    // Overview reads the plan the same way.
    await page.locator(".detail-tabs").getByRole("tab", { name: "Overview" }).click()
    const plan = page.getByRole("list", { name: "Plan" }).getByRole("listitem")
    await expect(plan).toHaveCount(2)
    await expect(plan.nth(1)).toContainText("After Step 1")
    await expectNoHorizontalOverflow(page)

    // The new-Workflow page fits too.
    await page.goto(`/#/spaces/${current.spaceId}/workflows/new`)
    await expect(page.getByRole("heading", { level: 1, name: "New Workflow" })).toBeVisible()
    await expect(page.getByRole("list", { name: "Steps" }).getByRole("listitem")).toHaveCount(1)
    await expectNoHorizontalOverflow(page)
  })
})
