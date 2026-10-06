# Portal UI Journey Audit (Phase 0)

> **简体中文：** [阅读中文镜像](../../zh-CN/contribute/exploratory-runs/2026-10-06-portal-ui-journey-audit.md)
> **Audience:** Portal and `@buildmax/gui` contributors, the maintainer · **Status:** pending triage

This is the Portal half of the phase 0 audit in the
[UI experience program](../../design/ui-experience-program.md)
([backlog 10](../../backlog/10-ui-journey-audit.md)). The Desktop half is
[2026-10-06-desktop-ui-journey-audit.md](2026-10-06-desktop-ui-journey-audit.md).

**The operator was an Agent with repository knowledge, not a new person.** It
knew the route shapes, the fixture data, and where the code lives. It used that
knowledge to set up the environment and to explain results, and it tried to
judge every step only by what is on screen. This audit is evidence about the
product. It is not the non-author operator journey (Q7).

## Charter

| Field | Value |
|---|---|
| Journeys | 1. First sign-in and Space orientation. 2. Create an Issue and run it to a result. 3. Diagnose a failed run from the Issue and from Administration. 4. Author and run a Workflow. 5. Set up a Schedule. 6. Find a shared Artifact or File. 7. Invite a member and change their role. |
| Why | Every Portal journey has so far been checked only by the person who built it. The findings decide which screens are reworked (phase 3) and whether the visual language changes (phase 2). |
| Roles and starting data | `nora@buildmax.local`: a new account with only its personal Space. `alice@buildmax.local`: Space owner and System Administrator, with the `./make kind fixtures --runs` data. |
| Success | Each journey can be finished through visible controls. At each step the person can tell what state the work is in and what to do next. |
| Capture | Each key screen at 390, 768, and 1280 px, in light and dark themes, plus one pass in 简体中文 at 1280 px (light). |
| Scope and mutations | Only an owned ephemeral cluster. Test resources were created there (an Agent, an Issue, a Workflow, a Schedule, an invitation, a role change that was then reverted). |
| Model | The in-cluster mock model only (free, deterministic). |
| Budget | About 75 minutes of operation. The session was interrupted overnight by an API rate limit and resumed against the same cluster. |

## Environment

- **Source:** `main` at `e98efc7a`, after the UI program, the token fix, and the
  complete Chinese interface. The tree was clean apart from the backlog claim
  commit.
- **Deployment:** an ephemeral kind cluster `buildmax-eph-1981bb29`, created by
  `BUILDMAX_KIND_EPHEMERAL=1 ./make kind up`. Its server and Portal images were
  built from that commit. Portal was served at `http://localhost:54421`. Seed
  data came from `./make kind fixtures --runs`.
- **Browser:** headless Chromium 1243 (the repository's Playwright install),
  driven over CDP. One script ran per step. Screenshots show the unmodified
  viewport, not an expanded page. The theme and language were set through the
  same `localStorage` keys that the interface writes (`buildmax_theme`,
  `buildmax_locale`).
- **Preparation, not journey:** login codes were minted with
  `./make kind login <email>` and typed into the login-code form. This is the
  supported operator path; a new person would get the code from an
  administrator.
- **Dates:** 2026-10-06 14:45–15:00 and 2026-10-07 05:59–06:12 (UTC+8).

## Explored

1. **First sign-in.** The sign-in page leads with Okta and email/password. The
   "Forgot your password, or have a login code?" link opens the code form.
   After sign-in, Nora lands on **Chat** in "My Space". The page has a prompt
   box, a "Recent Conversations / Files" tab pair, and a sidebar with Work,
   Resources, and Manage groups. Nothing explains what a Space, an Issue, or an
   Agent is. No horizontal overflow appeared at any width
   (`p03-first-landing-*`).
2. **Issue to result.** Creating the Issue was straightforward (`p05`, `p06`).
   Running it was not. The Executor list held only "None", and its hint
   pointed at published workflows. The run only became possible after Nora
   found **Agents**, created one, and came back to edit the Issue. **Run
   agent** then navigated to the Task page, and the mock answered in a few
   seconds (`p11`–`p13`). Back on the Issue, the result is described in five
   places that disagree (finding P1).
3. **Failed run.** As Alice, the executor of "Unassigned backlog item" was set
   to "QA Blocked Agent". That agent's Secret grant is disabled, and its Agent
   page already showed a warning. The run failed in about 0 s. The cause
   appeared only as a raw worker error (P2). From **Administration**, the
   failure is counted as "Space configuration → Space owner", with no route to
   the run, which matches the metadata-only design (P5).
4. **Workflow.** A Workflow was authored in the New Workflow modal with a
   React Flow canvas (`p21`, `p22`). It was saved as Draft. **Run Workflow**
   was disabled without saying why. After **Publish**, the run succeeded and
   the run page said "No result was produced" (`p26`, P1/P10/P11).
5. **Schedule.** A Monday 09:00 schedule was created for the Workflow (`p29`,
   `p31`). Invalid cron text and timezone were each rejected with raw server
   text, one error at a time (P12).
6. **Shared Artifact or File.** Files opens at a root called "home" (P7).
   Artifacts lists three files with no sign of which ones are shared. Sharing
   is found only inside each Artifact's **Share** dialog (P19).
7. **Members.** **Invite** worked for an existing account. The new pending row
   showed only an opaque ID (P4). An unknown email returned an operator-facing
   API hint (P13). Changing Carol to Admin from the role select took effect
   immediately and was still set after a reload (P14). The role was restored
   afterwards.
8. **Chinese pass.** Every captured page was rendered in 简体中文 at 1280 px.
   No page overflowed. Translation findings are folded into the entries below
   and summarized under [Chinese Interface](#chinese-interface).

## Findings

Severity follows the program's rubric: Blocker, Major, Minor, Cosmetic. IDs
appear in nora's personal Space (`e5ffwpdprxbc22m3ix7a`) and the fixture Space
"BuildMax QA" (`vnriocemqxlnxwptmwoq`). Both lived on the deleted cluster; they
identify screenshots, not live resources.

| Severity | Count |
|---|---|
| Blocker | 0 |
| Major | 4 |
| Minor | 16 |
| Cosmetic | 6 |

### Major

**P1. An Issue's result is described in five places that contradict each
other.** *State legibility.*

- Screen: Issue Detail, Results tab, Runs tab, and Workflow run page.
- Reproduction:
  1. Create an Issue.
  2. Set an Agent executor and choose **Run agent**.
  3. When the Task shows Done, return to the Issue.
- Actual:
  - The overview card says **Latest result: No result yet.**
  - The lower section **Latest Outcome** says **Succeeded** and shows the Task
    ID.
  - The **Results** tab says "No results produced yet".
  - The **Runs** tab says "Run History 0 total … No runs yet." and then
    "Agent Run Sequence 1 tasks … Succeeded".
  - The Agent's actual output ("deployment smoke ok") appears only as a
    **Discussion** comment.
- The Workflow run page repeats the pattern: "Result: Succeeded — No result
  was produced", while the step's output sits further down (`p26`).
- In Chinese, both labels read **最新结果**, one with "暂无结果" and one with
  "成功" (`p14-issue-with-result-zh-1280`).
- Expected: a single answer to "did it work and what did it produce?" on the
  Issue.
- Seed observation "Latest Outcome shown twice" is **confirmed**. It is worse
  than a duplicate: the two sections disagree.
- Evidence: `p14-issue-with-result-{390,768,1280}-{light,dark}`,
  `p15-issue-tab-results`, `p15-issue-tab-runs`.

**P2. A failed run shows only a raw internal error and offers a retry that
cannot succeed.** *Error recovery.*

- Screen: Task page, Issue Discussion, Task details, and Run details.
- Reproduction (Alice in BuildMax QA):
  1. Set "Unassigned backlog item" Executor to **QA Blocked Agent**.
  2. Choose **Run agent**.
- Actual:
  - The transcript shows an italic message in the Agent's bubble:
    `secret grant unavailable: worker API GET /api/worker/task-runs/<id>/secrets: secret is disabled (409)`.
  - The Issue's Latest Outcome says only "Failed".
  - **Details** (Task details) lists the agent, status, and 0 s duration, but
    no cause.
  - **Run details** says "no trace was recorded for this run", and the browser
    logs a 404.
  - The most prominent actions are **Retry last run** and **Retry Run**, which
    fail the same way.
  - Nothing links to the Agent, whose page already says "⚠ 1 secret grant no
    longer resolve. Fix in config".
  - **Run agent** started the run without warning about that known
    configuration problem.
- Expected: the cause in the person's terms (which Secret, which Agent), and
  the fix as the primary action.
- Evidence: `a12-failed-task-1280-light`, `a13-failed-task-details`,
  `a14-issue-after-failure-*`, `a15-run-details-from-issue`.

**P3. A new person has no path from an Issue to something that can run it.**
*Discoverability.*

- Screen: New Issue dialog and Issue Detail.
- Reproduction:
  1. Sign in as a new account and open **Issues**.
  2. Choose **New Issue**.
- Actual:
  - The Executor select has only "None".
  - Its hint reads "What runs the work. Only published workflows are
    available.", which does not mention Agents. The hint stays the same after
    an Agent exists and is listed.
  - The Issue page with Executor None shows no run action and no hint.
  - Nora ran the Issue only by leaving it, finding Agents, creating one, and
    returning to **Edit issue**.
- Expected: say that an Agent or a published Workflow is needed, and link to
  creating one.
- Evidence: `p05-new-issue-*`, `p06-issue-detail-new-*`, `p07-agents-empty`.

**P4. Pending invitations are identified only by an opaque ID.** *State
legibility.*

- Screen: Space settings → Members.
- Reproduction:
  1. **Invite** `nora@buildmax.local` as Member.
- Actual: the new row reads `3n4sj7muxl32i3ejxscq — Invited as member, expires
  10/10/2026, 06:09:40` beside an older `q6p6c6vl2454y2xv547q`. Neither row
  names the invitee, so the owner cannot tell which one to **Revoke**.
- Expected: the invitee's email or name.
- Evidence: `a28-invite-sent-1280-light`, `a25-members-*`.

### Minor

**P5. Administration shows raw identifiers and gives no next step for a
failure.**

- Screen: Administration → Overview and Spaces.
- Raw values on the page:
  - `DATABASE`, `OBJECT_STORAGE`, and `ok` as health labels.
  - `CANCELED`, `FAILED`, `RUNNING`, and `SUCCEEDED` as Task-run counts.
  - Role `system_admin`, worker mode `k8s_job`, transport `direct`, and tiers
    `pro` and `free_trial`.
  - Run IDs in the "Spaces needing attention" table.
- These stay untranslated in Chinese. The quota "258 / 10000000" is
  unformatted, and "1 running" wraps as "1 runnin g".
- The FAILED cell ("3 space configuration") names who acts but not what to
  tell them. Metadata-only is the design, so the gap is wording, not access.
- Seed observation "Administration shows raw identifiers" is **confirmed**.
- Evidence: `a06-admin-overview-*`, `a07`, `a08`, `a17-admin-space-detail-*`.

**P6. The Agent success rate counts canceled runs as failures.**

- Screen: Agents list KPI and Agent Detail.
- `AgentList.tsx` and `AgentDetail.tsx` use `taskRunFailed`, which treats
  `CANCELED` as failed.
- Observed: QA Writer has 18 runs (14 Done, 2 Stopped, 2 "Needs your answer")
  and shows **88%**, which is 14 ÷ 16. The two stopped runs count against it.
- An Agent whose only finished runs were canceled therefore shows 0%. The
  operator could not observe this directly: the mock finished each run in
  seconds, before **Stop** could be reached.
- Seed observation "0% when the only runs were canceled" is **confirmed by
  code and by the 88% arithmetic**, but not observed on screen.

**P7. The Files root is called "home", and the folder is not in the URL.**

- In English, both the tree root and the panel title read "home".
- In Chinese, the panel title reads "根目录" while the tree root still reads
  "home".
- Opening a folder does not change `#/…/files`, so a reload returns to the
  root.
- Seed observation is **confirmed**.
- Evidence: `a18-files-*`, `a18-files-zh-1280`.

**P8. Task naming is unstable.**

- The Task heading is a generated title. With the mock it is
  "deployment smoke ok" for every run, including runs that failed before
  starting.
- The breadcrumb shows the Issue title when reached from the Issue, but a
  generic "Issue" after a reload.
- Agent run lists mix generated titles with raw prompts such as
  "Agent: QA Reviewer Description: Reviews acceptance…".
- The mock exaggerates this, but runs are never named after the Issue or
  Workflow they serve.

**P9. Running an Issue leaves the Issue.**

- **Run agent** navigates to the Task page.
- The Issue status stays "To do" after a successful run.
- The save message says "use Run to schedule one", while the button is
  labeled **Run agent** and "schedule" collides with the Schedules feature.

**P10. Workflow authoring is dense and uses developer terms.**

- The graph editor lives in a 600 px modal.
- Step IDs are machine-made (`step_a5fd8c28`).
- Agent options carry raw IDs: "Onboarding summarizer (vgivleuxrkt2av5233xa)".
- "Issue access" offers the raw values `none`, `if_bound`, and `required`.
- **Run Workflow** is disabled on a Draft with no explanation (no title, no
  description).
- At 390 px the canvas is barely usable (`p23-workflow-detail-390-light`).

**P11. A Workflow step's output is unlabeled.**

- The output appears as a bare block directly under the collapsed "Resolved
  input this node received" summary, so it reads as the input (`p27`).

**P12. The Schedule form is weak at validation and preview.**

- Timezone defaults to `UTC`, not the browser's zone.
- There is no next-run preview. Desktop has one.
- Errors are raw server text (`timezone "Shanghai": unknown time zone
  Shanghai`) and arrive one at a time: the invalid cron "every monday" was not
  reported until the timezone was fixed.
- "Pause all" and "Resume all" are shown together.
- A schedule row offers only **Pause**, with no visible edit or delete.
- Evidence: `p29`, `p30`, `p31-*`.

**P13. The unknown-email invite error is written for operators.**

- It reads "…ask a system administrator to create one (POST /api/admin/users
  or buildmax-server user create), then invite it".

**P14. A role change applies from a select with no confirmation or saved
feedback.**

- The change persisted, as checked after a reload. A mistaken selection
  silently grants Admin.

**P15. The Agent dialog's copy contradicts Agent scope.**

- The dialog says "Agents are personas or task templates you can use across
  your account".
- The page says "space agents" and the route is Space-scoped.

**P16. Sidebar navigation is not exposed as navigation.**

- Items are `<button>` elements with no `aria-current`, so the current page is
  visual only.
- They are not links, so open-in-new-tab and copy-link do not work.

**P17. Raw IDs appear in work views.**

- "Latest agent task: f4qupcltm5roh4o5snrq".
- Task details: "Task czdpj4…".
- Workflow step: "Task: 7yzq… / Run: ruw5…".

**P19. Shared Artifacts cannot be found.**

- The Artifacts list shows no share state and has no search or filter.
- Finding one means opening each Artifact's **Share** dialog (`a21`, `a22`,
  `a23`).

**P20. Space settings tabs are hidden at 390 px.**

- A horizontally scrolling pill shows 3 of the 8 tabs with no scroll
  affordance (`a25-members-390-light`).

**P22. Portal ships one 1.3 MB script, uncompressed.**

- `index-*.js` is 1,300,313 bytes (plus 151 KB of CSS).
- Through the kind ingress, a `gzip` request received the same byte count, so
  the file was not compressed.
- No journey was blocked, but every first load pays this cost.
- Seed observation "one ~1 MB chunk" is **confirmed**: the chunk is 1.3 MB,
  and on kind it is not compressed. Production ingress compression was not
  checked.

### Cosmetic

- **P18.** Issue list snippets show raw Markdown ("## Acceptance criteria -
  [ ] …").
- **P21.** Titles repeat. The breadcrumb and h1 repeat "Chat", and at 390 px
  the mobile header repeats it a third time. **Back to Issues** duplicates the
  breadcrumb. The Chat placeholder is clipped mid-line at 390 px.
- **P23.** Every non-admin page load logs `403 /api/admin/me`, and the signed-out
  page logs a burst of 401s.
- **P24.** Admin Space links use the browser's default blue underline, unlike
  every other link.
- **P25.** Grammar: "1 tasks"; "1 secret grant no longer resolve".
- **P27.** The theme ignores the OS preference and always starts light.
  `ThemeContext` reads only `localStorage`.

### Chinese Interface

- **Layout:** no page overflowed at 1280 px, and no truncation was found.
- **Terminology:** Space, Issue, Agent, Workflow, and Artifact are kept in
  English, as the program's D5 rule requires.
- **Untranslated text** is limited to:
  - server-originated text (Task prompts such as "Work on this issue.", and
    error messages), which D5 accepts;
  - the Administration identifiers in P5, which D5 does not accept;
  - the uppercase "TOKEN" label.
- **Misleading wording:** the two **最新结果** sections in P1, and the "根目录"
  versus "home" mismatch in P7.

## Seed Observations

| Observation | Verdict | Evidence |
|---|---|---|
| Issue Detail shows "Latest Outcome / 最新结果" twice | Confirmed. The two sections also disagree (P1). | `p14-*` |
| Agent success rate is 0% when the only runs were canceled | Confirmed by code and arithmetic. Not reproduced on screen because the mock finishes too fast (P6). | `AgentList.tsx`, QA Writer 88% |
| Files tree root reads "home" while the panel title says 根目录/root | Confirmed in Chinese. English says "home" in both places (P7). | `a18-files-zh-1280` |
| Administration overview shows raw identifiers | Confirmed (P5). | `a06-*` |
| Portal ships one ~1 MB JS chunk | Confirmed: 1.3 MB, uncompressed on kind (P22). | network measurement |

## Visual-Language Recommendation

**Refine the current neutral style; do not adopt a new one.** None of the four
Major findings comes from color, type, or layout style. They come from
information architecture and copy: where a result lives (P1), how a failure
explains itself (P2), how a person finds an executor (P3), and how a record is
named (P4). In both themes the pages are legible, and no width overflows.

The remaining visual problems are consistency gaps that phase 1 primitives
already target:

- raw uppercase status words beside styled labels (P5) — one `StatusLabel`
  over one vocabulary;
- default-blue admin links (P24), emoji folder icons in Files, and duplicate
  headers (P21) — page-anatomy components and one icon set;
- a modal editor for Issue creation beside an inline editor for Issue editing,
  and a dense graph modal for Workflows (P10) — form-field and layout
  primitives.

A new visual language would leave every Major finding in place.

## Default Landing Page Evidence

- **New person.** Nora's empty personal Space landed on **Chat**. Chat was the
  only page that offered an immediate action. Issues and Agents were empty,
  and running an Issue required first creating an Agent (P3).
- **Team member with work.** Alice owns an Issue and belongs to a team Space
  with a failed run, 2 Task runs at "Needs your answer", and 1 Workflow input
  request. Her Chat landing showed one old conversation and none of that.
  The Issues page does not show the waiting requests either. They were
  reachable only through Agents → QA Writer → Runs, or through
  Administration's counts.
- **Space switching** keeps the current section (Issues stays Issues), so the
  landing page matters only on first entry and on bare `#/`.
- **Conclusion:** neither Chat nor today's Issues page shows "what needs me"
  to a team member. Chat suits an empty Space. A team Space needs a landing
  that surfaces the person's owned Issues and pending requests, which today's
  Issues view would need to gain before it is the better default.

## Not Exercised Or Not Observable

- Sending a Portal Chat message, and Chat with a real model. Every Portal run
  used the mock, which also produced the generic Task titles in P8.
- Okta/OIDC sign-in, password sign-in, and password setup.
- File and Artifact upload, which needs a native file chooser. A headless
  driver does not show one.
- Screen-reader output, keyboard-only operation, and 200% zoom. Accessibility
  findings come from DOM inspection.
- Canceling a run before it finished (P6).
- Answering a Workflow input request or an AskUser question.
- Whether production ingress compresses the script bundle.

## Cleanup

- The ephemeral cluster was removed with `./make kind down`. It reported
  "Deleted nodes" and removed `.local/kind-ephemeral.env`.
- The Chromium profiles were deleted. No background process remains.
- Screenshots stay only under the gitignored `.artifacts/ui-audit/`.

## Follow-Up

Draft phase 3 tasks from P1–P4 first, in journey order:

1. One result model on Issue Detail, carried over to the Workflow run page.
2. Failure explanation and fix-first recovery.
3. An executor path from the Issue form.
4. Invitation identity.

P5, P6, P12, and P22 are small, separable fixes. P6 is a trivial fix
candidate: exclude `CANCELED` from the success-rate denominator.
