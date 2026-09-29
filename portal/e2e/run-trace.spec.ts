import { expect, test, type Locator, type Page } from "@playwright/test"

import { patchJSON, postJSON, reportLeftovers, session, tagged, type Session } from "./fixtures"

/**
 * A run has to explain itself, and it has to be honest about what confined it.
 *
 * The roadmap is unusually specific here: "A run that is not sandboxed must say
 * so — in its trace and in Portal. An unreported boundary is worse than a
 * missing one." `RunTraceModal` is the only surface that makes that claim, and
 * nothing below the UI can check it. The API-level smoke never reaches it
 * either: the modal opens from an issue's outputs, and the smoke creates a
 * conversation task, which has none.
 *
 * So this spec creates the issue the smoke does not, and then reads the answer
 * the way an operator would.
 */

// The worker has to start, run, and write its trace to storage. The API smoke
// allows two minutes for the same shape of work; this adds the polling that
// follows it.
const RUN_TIMEOUT_MS = 150_000

/** The value cell of one run-trace statistic, addressed by its exact term. */
function statValue(page: Page, term: string): Locator {
  const rows = page.locator(".run-trace__stats > div")
  return rows.filter({ has: page.locator("dt", { hasText: new RegExp(`^${term}$`) }) }).locator("dd")
}

/** Seed an issue whose agent has run to completion, and return its issue id. */
async function seedCompletedAgentRun(page: Page, session: Session): Promise<string> {
  const space = session.space

  const agent = await postJSON<{ id: string }>(page, `${space}/agents`, session, {
    name: tagged("Run trace probe"),
    description: "Created by the Portal browser tests.",
    instructions: "Reply with exactly: deployment smoke ok",
  })
  const issue = await postJSON<{ id: string; version: number }>(page, `${space}/issues`, session, {
    title: tagged("Run trace probe"),
    description: "Created by the Portal browser tests to exercise the run trace view.",
  })
  reportLeftovers(session.spaceId, [`agent ${agent.id}`, `issue ${issue.id}`])
  // An agent run is refused unless the issue is assigned to an agent, so this
  // is a precondition of the next call rather than a separate assertion.
  await patchJSON(page, `${space}/issues/${encodeURIComponent(issue.id)}`, session, {
    version: issue.version,
    executor_kind: "agent",
    executor_id: agent.id,
  })
  const task = await postJSON<{ id: string }>(
    page,
    `${space}/issues/${encodeURIComponent(issue.id)}/agent-runs`,
    session,
    { input: "Reply with exactly: deployment smoke ok" }
  )

  // Poll the task rather than the UI. A run that never finishes should fail
  // here, naming the status it got stuck in, instead of thirty screenshots
  // later as a missing button.
  await expect
    .poll(
      async () => {
        const res = await page.request.get(`${space}/tasks/${encodeURIComponent(task.id)}`, {
          headers: { Authorization: `Bearer ${session.token}` },
        })
        if (!res.ok()) return `HTTP ${res.status()}`
        const body = (await res.json()) as { status: string; error_message?: string | null }
        return body.status === "FAILED" ? `FAILED: ${body.error_message ?? "no message"}` : body.status
      },
      { timeout: RUN_TIMEOUT_MS, intervals: [1000] }
    )
    .toBe("SUCCEEDED")

  return issue.id
}

test("Portal states what confined a run, and what the run spent", async ({ page }) => {
  test.setTimeout(RUN_TIMEOUT_MS + 60_000)

  const current = await session(page)
  const issueId = await seedCompletedAgentRun(page, current)

  await page.goto(`/#/spaces/${current.spaceId}/issues/${issueId}`)
  // Discussion is its own tab, not part of the default Overview.
  await page.getByRole("navigation", { name: "Issue sections" }).getByRole("button", { name: "Discussion" }).click()

  // An agent's report is Markdown, and a reply to it belongs on the run's task,
  // so the comment renders Markdown and links to that task.
  await postJSON(page, `${current.space}/issues/${encodeURIComponent(issueId)}/comments`, current, {
    body: "Checked with **emphasis**.",
  })
  await page.reload()
  await page.getByRole("navigation", { name: "Issue sections" }).getByRole("button", { name: "Discussion" }).click()
  await expect(page.locator(".issue-discussion__body strong", { hasText: "emphasis" })).toBeVisible()
  await expect(
    page.locator(".issue-discussion__actions").getByRole("link", { name: "Open Task" }).first()
  ).toHaveAttribute("href", new RegExp(`#/spaces/${current.spaceId}/tasks/`))

  // The agent run's comment carries the way in. An issue's outputs are its
  // published artifacts now, and this run only replied, so it has no output
  // card — but every agent run posts a comment, and that comment carries a
  // "Run details" button (issue-discussion__actions). Scoping to the comment
  // reaches the run the same way an operator does.
  await page
    .locator(".issue-discussion__actions")
    .getByRole("button", { name: "Run details" })
    .first()
    .click()

  const dialog = page.getByRole("dialog", { name: "Run details" })
  await expect(dialog).toBeVisible()

  const boundary = dialog.locator(".run-trace__boundary")
  const failure = dialog.locator(".modal__error")

  // The modal fetches the trace when it opens, so wait for it to settle into
  // one of its two outcomes before judging which one it is. Asserting "no
  // error" while the request is still in flight would pass on an empty modal.
  await expect(boundary.or(failure).first()).toBeVisible()

  // A trace the server never recorded, or one storage lost, lands here. Either
  // is a real failure of the gate: a run nobody can read cannot explain itself.
  await expect(failure).toHaveCount(0)
  // The assertion the roadmap actually demands. `--unknown` is the class the
  // Portal uses for "this trace predates boundary recording", and a fresh
  // deployment producing it means the boundary stopped being recorded — the
  // unreported boundary the roadmap calls worse than a missing one.
  //
  // It deliberately does not assert `--open`. Workers are unsandboxed today,
  // but hardening them is planned P0.5 work, and this test should keep passing
  // the day it lands rather than failing as though something broke.
  await expect(boundary).not.toHaveClass(/run-trace__boundary--unknown/)

  // The MCP treatment sits beside the boundary. This is a worker run, so the
  // unattended-worker profile disables stdio — the line must say so, and must
  // not report the treatment as unknown, which would mean it stopped being
  // recorded.
  const mcp = dialog.locator(".run-trace__mcp")
  await expect(mcp).toBeVisible()
  await expect(mcp).not.toHaveClass(/run-trace__mcp--unknown/)
  await expect(mcp).toContainText(/stdio MCP disabled/)

  // What the run spent. The mock model in the smoke deployments reports both a
  // model name and token usage, so "—" here means the trace lost them rather
  // than that the deployment never had them.
  //
  // Rows are matched by their exact term because "Model" is a prefix of "Model
  // calls", and a prefix match would quietly assert against the wrong number.
  await expect(statValue(page, "Model")).not.toHaveText("—")
  await expect(statValue(page, "Duration")).not.toHaveText("—")
  await expect(statValue(page, "Tokens")).toHaveText(/\d+ in · \d+ out/)

  // The governance ledger, beside the trace. The smoke deployments run in
  // direct mode, so this section is expected to be empty here — what is under
  // test is that an empty one says which kind of empty it is. A blank section
  // would read as "this run spent nothing", which is the one thing an empty
  // ledger does not mean.
  const spend = dialog.locator(".run-trace__section").filter({ hasText: "Managed model calls" })
  await expect(spend).toBeVisible()
  await expect(spend.locator(".run-trace__spend-note, .run-trace__calls").first()).toBeVisible()

  // The workspace section states what became of the run's checkpoint. This is a
  // first, successful agent run, so it restored nothing and its result committed
  // — the section must say so rather than leave the reader guessing.
  const workspace = dialog.locator(".run-trace__section").filter({ hasText: "Workspace" })
  await expect(workspace).toBeVisible()
  await expect(statValue(page, "Checkpoint")).toHaveText("Committed")
  await expect(statValue(page, "Restore")).not.toHaveText("—")
})
