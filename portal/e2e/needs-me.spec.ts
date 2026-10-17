import { expect, test, type Page } from "@playwright/test"

import { createSpace, getJSON, patchJSON, postJSON, reportLeftovers, session, tagged, type Session } from "./fixtures"

/**
 * The page a person lands on leads with the work waiting on them
 * (docs/design/portal-navigation-and-space-context.md, "Landing on the work
 * that needs you"). Each case seeds a fresh Space, so what other runs left in
 * the smoke account's Spaces cannot appear in, or hide, what is asserted.
 *
 * Only a real worker failing a run, a real Workflow run opening a request, and
 * the mock model asking a question prove the four groups end to end. The
 * question needs the kind mock's control route; elsewhere that group is
 * skipped and the other three are still checked.
 */

// The workers have to start; this allows for cold ones.
const RUN_TIMEOUT_MS = 150_000

interface Seeded {
  space: Session
  issueId: string
  issueTitle: string
  failedTaskId: string
  blockedAgent: string
  workflowName: string
  workflowRunId: string
  askingTaskId: string | null
  askingAgent: string
}

function inSpace(current: Session, spaceId: string): Session {
  return { ...current, spaceId, space: `${current.apiBase}/api/spaces/${encodeURIComponent(spaceId)}` }
}

async function pollTask(page: Page, current: Session, taskId: string, done: (task: { status: string; awaiting_answer?: boolean }) => boolean) {
  await expect
    .poll(
      async () => {
        const task = await getJSON<{ status: string; awaiting_answer?: boolean }>(page, `${current.space}/tasks/${encodeURIComponent(taskId)}`, current)
        return done(task) ? "settled" : `${task.status}${task.awaiting_answer ? " (asking)" : ""}`
      },
      { message: `task ${taskId} did not settle`, timeout: RUN_TIMEOUT_MS, intervals: [2_000] },
    )
    .toBe("settled")
}

async function seed(page: Page, current: Session): Promise<Seeded> {
  const created = await createSpace(page, current, tagged("Needs me"))
  const space = inSpace(current, created.id)
  const members = await getJSON<{ user_id: string }[]>(page, `${space.space}/members`, space)
  const me = members[0].user_id

  // A failed run: an Agent whose required Secret grant an owner disabled is
  // refused when its run starts. Run on an Issue the caller owns, it also puts
  // that Issue in attention.
  const secret = await postJSON<{ id: string }>(page, `${space.space}/secrets`, space, {
    name: tagged("needs-me-secret"),
    items: { TOKEN: "not-a-real-token" },
  })
  const blockedAgent = tagged("Needs me blocked agent")
  const blocked = await postJSON<{ id: string }>(page, `${space.space}/agents`, space, {
    name: blockedAgent,
    description: "Created by the Portal browser tests.",
    instructions: "Reply with exactly: deployment smoke ok",
    secret_consumption: { env: [{ secret: secret.id, item: "TOKEN", env_name: "BLOCKED_TOKEN" }] },
  })
  const disabled = await page.request.put(`${space.space}/secrets/${encodeURIComponent(secret.id)}/state`, {
    headers: { Authorization: `Bearer ${space.token}` },
    data: { state: "disabled" },
  })
  expect(disabled.ok(), `disable the Secret → ${disabled.status()} ${await disabled.text()}`).toBeTruthy()
  const issueTitle = tagged("Needs me failing issue")
  const issue = await postJSON<{ id: string }>(page, `${space.space}/issues`, space, {
    title: issueTitle,
    owner_id: me,
    executor_kind: "agent",
    executor_id: blocked.id,
  })
  const failed = await postJSON<{ id: string }>(page, `${space.space}/issues/${encodeURIComponent(issue.id)}/agent-runs`, space, {})

  // A pending Workflow request: a published Workflow whose first step waits on a person.
  const workflowName = tagged("Needs me sign-off")
  const workflow = await postJSON<{ id: string }>(page, `${space.space}/workflows`, space, {
    name: workflowName,
    definition: JSON.stringify({
      schema_version: 1,
      nodes: [{ id: "approve", type: "human_input", input: { instruction: "Approve the needs-me release notes?" } }],
    }),
  })
  await patchJSON(page, `${space.space}/workflows/${encodeURIComponent(workflow.id)}`, space, { status: "published" })
  const started = await postJSON<{ run: { id: string } }>(page, `${space.space}/workflows/${encodeURIComponent(workflow.id)}/runs`, space, {})

  // A run waiting for an answer: the mock model asks, reserved for this run's
  // own input so no other caller takes the armed call.
  const askingAgent = tagged("Needs me asking agent")
  const asker = await postJSON<{ id: string }>(page, `${space.space}/agents`, space, {
    name: askingAgent,
    description: "Created by the Portal browser tests.",
    instructions: "Ask which checklist to use, then summarize.",
  })
  const marker = `needs-me-${Date.now().toString(36)}${Math.random().toString(36).slice(2, 6)}`
  // Armed after the create, whose title is its one synchronous model call; the
  // next call carrying the marker is the worker's first turn. A deployment
  // without the mock's control route refuses the arm, and the case skips the
  // group rather than wait for a question that never comes.
  const asking = await postJSON<{ id: string }>(page, `${space.space}/agents/${encodeURIComponent(asker.id)}/tasks`, space, {
    input: `Ask which checklist to use before summarizing (${marker}).`,
  })
  const arm = await page.request.post("/smoke-llm/control/toolcall", {
    data: { name: "AskUser", args: { questions: [{ question: "Which checklist should the summary follow?" }] }, times: 1, match: marker },
  })
  const askingTaskId = arm.ok() ? asking.id : null

  reportLeftovers(space.spaceId, [
    `space ${space.spaceId}`,
    `secret ${secret.id}`,
    `agent ${blocked.id}`,
    `agent ${asker.id}`,
    `issue ${issue.id}`,
    `workflow ${workflow.id}`,
    `workflow run ${started.run.id}`,
  ])

  await pollTask(page, space, failed.id, (task) => task.status === "FAILED")
  if (askingTaskId) await pollTask(page, space, askingTaskId, (task) => task.status === "SUCCEEDED" && task.awaiting_answer === true)

  return {
    space,
    issueId: issue.id,
    issueTitle,
    failedTaskId: failed.id,
    blockedAgent,
    workflowName,
    workflowRunId: started.run.id,
    askingTaskId,
    askingAgent,
  }
}

async function expectNoHorizontalOverflow(page: Page): Promise<void> {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth)
  expect(overflow).toBeLessThanOrEqual(0)
}

test("the bare address lands on Issues, which leads with what is waiting", async ({ page }) => {
  await session(page)
  await expect(page.getByRole("heading", { level: 1, name: "Issues" })).toBeVisible()
  await expect(page.getByRole("heading", { level: 2, name: "Waiting on you" })).toBeVisible()
})

test("an empty Space says nothing is waiting and offers a first Issue", async ({ page }) => {
  const current = await session(page)
  const created = await createSpace(page, current, tagged("Needs me empty"))
  reportLeftovers(created.id, [`space ${created.id}`])
  await page.goto(`/#/spaces/${created.id}/issues`)

  const section = page.getByRole("region", { name: "Waiting on you" })
  await expect(section.getByText("Nothing in this Space is waiting on you.", { exact: false })).toBeVisible()
  await section.getByRole("button", { name: "Create an Issue" }).click()
  await expect(page.getByRole("dialog")).toBeVisible()
})

test("the work waiting on a person is listed, and each item opens where it is resolved", async ({ page }) => {
  test.setTimeout(RUN_TIMEOUT_MS * 2 + 60_000)
  const current = await session(page)
  const seeded = await seed(page, current)
  const issues = `/#/spaces/${seeded.space.spaceId}/issues`
  await page.goto(issues)

  const section = page.getByRole("region", { name: "Waiting on you" })
  const attention = section.getByRole("group", { name: /Your Issues that need attention/ })
  const failedRuns = section.getByRole("group", { name: /Failed runs/ })
  const requests = section.getByRole("group", { name: /Workflow requests to answer/ })
  const awaiting = section.getByRole("group", { name: /Runs waiting for your answer/ })

  const issueLink = attention.getByRole("link", { name: new RegExp(seeded.issueTitle) })
  await expect(issueLink).toContainText("Its latest run failed")
  const failedLink = failedRuns.getByRole("link", { name: new RegExp(seeded.blockedAgent) })
  await expect(failedLink).toContainText("This Agent's Secret grant is disabled")
  // A run that failed before it started is named by the Issue it serves.
  await expect(failedLink).toContainText(seeded.issueTitle)
  const requestLink = requests.getByRole("link", { name: new RegExp(seeded.workflowName) })
  await expect(requestLink).toContainText("Approve the needs-me release notes?")
  if (seeded.askingTaskId) {
    await expect(awaiting.getByRole("link", { name: new RegExp(seeded.askingAgent) })).toContainText("Needs your answer")
  }

  // Every width and both languages, without page-level horizontal scroll.
  for (const locale of ["en", "zh-CN"]) {
    await page.evaluate((value) => localStorage.setItem("buildmax_locale", value), locale)
    for (const width of [390, 768, 1280]) {
      await page.setViewportSize({ width, height: 900 })
      await page.reload()
      const region = page.getByRole("region", { name: locale === "en" ? "Waiting on you" : "等待你处理" })
      // The Issue, and its failed run named after it.
      await expect(region.getByText(seeded.issueTitle)).toHaveCount(2)
      await expect(region.getByText(seeded.issueTitle).first()).toBeVisible()
      await expectNoHorizontalOverflow(page)
    }
  }
  await page.evaluate(() => localStorage.setItem("buildmax_locale", "en"))
  await page.setViewportSize({ width: 1280, height: 900 })
  // The language is read when the app loads; a hash change alone keeps Chinese.
  await page.reload()

  // Each item opens where it is resolved.
  await issueLink.click()
  await expect(page).toHaveURL(new RegExp(`/issues/${seeded.issueId}$`))
  await page.goto(issues)
  await failedLink.click()
  await expect(page).toHaveURL(new RegExp(`/tasks/${seeded.failedTaskId}$`))
  await page.goto(issues)
  await requestLink.click()
  await expect(page).toHaveURL(new RegExp(`/workflow-runs/${seeded.workflowRunId}$`))
  await expect(page.getByRole("region", { name: "Request from step approve" })).toBeVisible()
  if (seeded.askingTaskId) {
    await page.goto(issues)
    await awaiting.getByRole("link", { name: new RegExp(seeded.askingAgent) }).click()
    await expect(page).toHaveURL(new RegExp(`/tasks/${seeded.askingTaskId}$`))
  }
})
