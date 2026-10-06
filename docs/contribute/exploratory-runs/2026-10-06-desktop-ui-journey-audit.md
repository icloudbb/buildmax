# Desktop UI Journey Audit (Phase 0)

> **简体中文：** [阅读中文镜像](../../zh-CN/contribute/exploratory-runs/2026-10-06-desktop-ui-journey-audit.md)
> **Audience:** Desktop and `@buildmax/gui` contributors, the maintainer · **Status:** pending triage

This is the Desktop half of the phase 0 audit in the
[UI experience program](../../design/ui-experience-program.md). The Portal half is
[2026-10-06-portal-ui-journey-audit.md](2026-10-06-portal-ui-journey-audit.md).

**The operator was an Agent with repository knowledge, not a new person.** It
knew the bindings, the sandbox layout, and the code. It used that knowledge for
setup and to explain results, and it judged each step by what is on screen.
This audit is evidence about the product. It is not the non-author operator
journey (Q7).

## Charter

| Field | Value |
|---|---|
| Journeys | 1. First launch with no project. 2. Open a project and run a chat that edits a file. 3. Review the diff. 4. Use the terminal tab. 5. Create a local Schedule. 6. Sign in and reach the Issues view. |
| Why | Desktop journeys have been checked only by their implementers. The findings feed phase 4 rework and the visual-language decision. |
| Starting data | A sandbox home with no project, no session, and nobody signed in. Its `settings.yaml` was seeded by `./make run desktop-dev` from the contributor's `~/.buildmax`, so real models were configured. A throwaway Git repository `greeter` had three files and one commit. |
| Success | Each journey can be finished through visible controls, and the state is legible at each step. |
| Capture | 390, 768, and 1280 px, in light and dark themes, plus 简体中文 at 1280 px (light). |
| Model | A real model for the editing chat. The scheduled run used whatever model the app resolved at fire time (see D4). |
| Budget | About 25 minutes of Desktop operation, inside a shared lock on the bridge port. |

## Environment

- **Source:** `main` at `e98efc7a`, with a clean tree.
- **Desktop:** `./make run desktop-dev` (`wails dev`) on macOS (Darwin
  25.6). Its `BUILDMAX_HOME` was the worktree's `testing-sandbox`.
- **Browser:** headless Chromium driving the browser bridge at
  `http://localhost:34115` over CDP, one script per step. The theme was
  switched with the status-bar toggle, and the language through the user
  menu. The window was resized with the viewport.
- **Server for sign-in:** the Portal audit's ephemeral kind cluster at
  `http://localhost:54421`, signed in as `alice@buildmax.local` with a login
  code minted by `./make kind login`. That minting is preparation.
- **Date:** 2026-10-06, 14:32–14:53 (UTC+8).

## Model

The editing chat used **GPT-5.6 Luna** (`openai/gpt-5.6-luna`, the sandbox
default) with this prompt:

> Edit src/main.go so main also prints "goodbye" on a second line, and delete
> the TODO line from NOTES.txt. Keep the change minimal.

- Trace: 3 LLM calls (about 4.1 s, 2.5 s, and 2.4 s), 4 tool calls (Read,
  Read, Edit, Write), 15,724 prompt tokens, and 210 completion tokens.
- Recorded cost: 1,812,170 nano-USD, about **$0.0018**.
- Wall time was 06:34:57–06:38:20 UTC. Most of it was the operator's script
  waiting before it answered the approval.
- The edit was correct: `main.go` gained `fmt.Println("goodbye")`, and
  `NOTES.txt` was emptied.
- The final assistant message was empty.

## Explored

1. **First launch.** Home reads "Continue your work", with empty "Recent chats"
   and "Recent projects" and a **New Project** button. The sidebar shows Home,
   Schedules, and Projects. The model the chat will use is not mentioned
   (`d01-first-launch-*`).
2. **Opening a project (assisted).** **New Project** opens a dialog with Name
   and **Choose folder…**. The bridge cannot drive the native folder chooser,
   so `window.go.desktop.App.OpenFolderDialog` was replaced in the page to
   return the prepared folder. Every later step went through the real
   `OpenProject` binding and the real UI. With the name left empty, the
   project was named after the folder, although the placeholder said
   "My Project" (`d02`–`d04`).
3. **Chat that edits files.** After sending, the transcript kept its
   empty-state line and did not show the sent message (D1). An inline **Tool
   approval** card asked about the Edit and then the Write (D2, D5). After
   both approvals the turn finished, the message appeared, the tab took the
   prompt as its title, and the turn ended with a blank bubble (D6)
   (`d05`–`d09`).
4. **Diff review.** In the Explorer, **Changes** listed both files with +/−
   counts. Opening `main.go` showed a unified diff in a new tab (D8)
   (`d10`, `d11-diff-*`).
5. **Terminal.** **New terminal** opened `Terminal 1` in the project folder.
   `ls && go run ./src` printed the files, then `hello` and `goodbye`
   (`d13-terminal-*`). After visiting Schedules and Issues and returning, the
   earlier output was gone (D16).
6. **Local Schedule.** **New Schedule** opened a long dialog (D10–D12). An
   empty submit, then an invalid cron, produced raw errors. A valid
   `*/5 * * * *` Asia/Shanghai task was created. It fired at 14:45:55 and
   appeared under Recent runs as `DONE`. Opening the run showed a reply from
   the server's mock model, not from the model the dialog named (D4)
   (`d15`–`d20`, `d26`, `d28`). The schedule was then paused and deleted
   through the UI. Deletion asked for confirmation and named what would be
   lost.
7. **Sign-in and Issues.** User menu → **Sign in to a server** opens a page
   that leads with email and password (D14). The login-code path worked. The
   **Issues** view appeared in the sidebar. It was empty until an Issue was
   assigned to Alice in Portal; after **Refresh** it listed that Issue. Detail
   offered **Move to**, a Project picker with **Start chat**, and comments
   (`d25`, `d29`, `d30-issue-detail-*`). **Start chat** did not start a chat
   (D3).
8. **Chinese pass.** First launch, chat, diff, terminal, Schedules, the
   schedule dialog, and Issues were captured in 简体中文. No overflow
   appeared. See [Chinese Interface](#chinese-interface).

## Findings

| Severity | Count |
|---|---|
| Blocker | 0 |
| Major | 4 |
| Minor | 11 |
| Cosmetic | 5 |

Bridge-only noise is not counted. Wails `ipc.js` threw `Cannot read
properties of null (reading 'nodes' | 'send')` on every reload.

### Major

**D1. A new chat does not show the sent message while its first turn runs.**
*State legibility.*

- Screen: workbench chat tab.
- Reproduction:
  1. Open a project.
  2. Type a prompt in **New Chat** and press Enter.
- Actual:
  - For the whole turn (tool calls and approval included) the transcript kept
    "Type a message below to start a new chat." and showed no user bubble
    (`d05-chat-running-1280-light`, `d06`).
  - The message appeared only after the turn ended (`d08`).
- Code reading: `ChatSession.handleSend` appends the message optimistically.
  The hypothesis, unverified, is that adopting the new session ID discards
  that local state. Seen once; later turns in an existing session were not
  tested.

**D2. The tool approval card misrepresents the change being approved.**
*Error prevention and state legibility.*

- Screen: inline Tool approval.
- Actual:
  - An Edit is shown as raw key/value rows (`file_path`, `new_string`,
    `old_string`, `replace_all`).
  - Newlines and indentation are collapsed, so `new_string` reads
    `fmt.Println("hello") fmt.Println("goodbye")` on one line. The real
    argument was two indented lines.
  - The Write that emptied `NOTES.txt` shows `content` with an empty value and
    no statement that the file will be emptied.
  - There is no diff preview.
- The person approves a change they cannot read accurately.
- Evidence: `d06-chat-done-1280-light`, `d07-approval-2`, and the
  `tool_start` arguments in the trace.

**D3. Issues → Start chat does not open a chat with the Issue.**
*The journey step fails silently.*

- Screen: Issues view → workbench.
- Reproduction:
  1. Sign in.
  2. Open **Issues** from Home and select an Issue.
  3. Leave Project = greeter and choose **Start chat**.
- Actual: the workbench reopens the project's previous tabs with
  **Terminal 1** active. No new chat tab appears, and no composer contains the
  Issue text (`d31-issue-start-chat-1280-light`).
- The hint promised "Starts a new chat with this issue in the message box".
- Code reading: `handleNewChatInProject` adds a new chat tab only when the
  project is already current. Here it was not, because the view came from
  Home.

**D4. A local schedule silently runs on a different model after sign-in.**
*The person is misled.*

- Screen: Schedules and the run view.
- Reproduction:
  1. In local mode, create a schedule with Model "Default (GPT-5.6 Luna)".
  2. Sign in to a server before it fires.
- Actual:
  - The run's composer shows the model **BuildMax smoke**, and the reply is
    "deployment smoke ok".
  - Nothing on the schedule card says which model will run, or that signing
    in changed it.
- Unattended work moved to another provider's model without notice.
- Evidence: `d26-schedule-after-fire`, `d28-schedule-run-open-1280-light`.

### Minor

**D5. The default choice in an approval can become the broader grant.**

- On the second approval, after the first was answered with **Allow
  session**, the highlighted default was **Allow session(a)**.
- The footer says "Enter confirm", so one keypress grants the wider
  permission.

**D6. A turn that ends without text leaves a blank bubble.**

- The model's final message was empty (4 completion tokens).
- The transcript ends in an empty white bubble with no "done" or summary.

**D7. Tool cards hide their arguments.**

- Arguments are cut at about 15 characters ("Read (src/mai…") in narrow cards,
  even with room to spare.
- The Write card shows raw JSON (`{"content":"","file_path":"NOT…`).

**D8. The diff view is hard to read and offers no actions.**

- The raw Git headers are shown (`diff --git …`, `index …`).
- An added line is marked only by a faint tint with no "+", and in dark theme
  the tint is nearly invisible.
- There is no revert or accept action next to the Agent's change.
- Evidence: `d11-diff-1280-light`, `d11-diff-768-dark`.

**D9. Narrow windows break the layout.**

- The sidebar keeps 288 px at any width. At 390 px the content column is about
  100 px, and headings break letter by letter (`d01-first-launch-390-light`).
- At 768 px the Issue detail column is about 100 px: its title breaks
  mid-word ("Unassigne d"), its ID is clipped, and the tab bar clips the "+"
  button (`d30-issue-detail-768-dark`).
- `run.go` sets no minimum window size.
- **Hide sidebar** recovers (`d14-390-sidebar-hidden-light`).

**D10. The New Schedule dialog hides its own buttons.**

- At 1280×860, the footer (**Create schedule**, Cancel) and the validation
  message sit below the dialog's scroll fold.
- The native default window is shorter still (800 px).
- Evidence: `d17-new-schedule-empty-submit`.

**D11. Schedule times are hard to read.**

- Timezone defaults to `UTC`, while "Next runs" lists local times with no zone
  label: `0 9 * * *` UTC appears as "17:00".
- The working directory defaults to the home folder even with a project open.
- A paused task still shows "Next: 14:50".

**D12. Schedule errors are raw and stale.**

- The cron error is parser text: `cron "every 5 minutes": expected exactly 5
  fields, found 3: [every 5 minutes]`.
- "a prompt is required" stays on screen after the prompt is filled in.

**D13. The Schedules list cuts off what matters.**

- The working-directory path is cut at its end, which hides the folder name.
- Cron is shown raw.
- The run status is the raw uppercase `DONE`.

**D14. The sign-in page does not match the account flow and speaks
developer.**

- It leads with Email and Password, while server accounts here sign in with
  login codes.
- Its copy cites "settings.yaml" and "Behind an ingress it is the origin the
  Portal is on".
- The default server is `http://localhost:5678`.
- It has no theme or language control.

**D16. Terminal scrollback is lost after leaving the workbench.**

- After visiting Schedules or Issues and returning, Terminal 1 is blank. The
  shell session survives, and new output appears.

### Cosmetic

- **D15.** The Issues view header says "Open Space work you own", but its
  empty state says "No open issues are assigned to you". The detail shows the
  raw Issue ID.
- **D17.** On first launch, "Continue your work" heads a page with nothing to
  continue. The New Project name placeholder says "My Project", but the
  folder name is used.
- **D18.** Icons mix: an emoji 💬 tab icon sits among line icons. The composer
  says "/ for commands" twice.
- **D19.** In Chinese, the diff tab title "main.go (diff)" stays English.
- **D20.** The theme ignores the OS preference and always starts light, the
  same as Portal P27.

### Chinese Interface

- **Layout:** every captured view rendered without overflow or truncation at
  1280 px. The Chinese copy fits the existing layout.
- **Untranslated text:**
  - "(diff)" in tab titles (D19);
  - raw schedule and run values (`DONE`, cron text, Go parser errors);
  - server-originated messages, which D5 accepts.
- **Not covered:** the sign-in page could not be switched to Chinese without
  a reload that leaves it, so its Chinese rendering was not captured.

## Visual-Language Recommendation

**Refine; do not replace.** The Desktop Majors concern behavior and
information:

- a lost message echo (D1);
- an approval card that misstates the edit (D2);
- a broken Start chat (D3);
- a silent model switch (D4).

The visual problems that remain match the program's convergence work:

- hand-rolled dialogs that clip their footer (D10);
- an emoji tab icon among line icons (D18);
- raw status text (D13);
- a diff with color-only meaning (D8);
- no responsive rules (D9).

These are what `@buildmax/gui` primitives, one icon set, `StatusLabel`, and
contrast checks fix. The neutral style reads well in both themes. Converging
Desktop onto the shared primitives is the right next step; a new style is not.

## Default Landing Page Evidence

Desktop has no Space landing page. Its Home lists recent chats and projects.
After sign-in, owned Issues sit in a separate **Issues** view, which stays
empty until Portal assigns work, and nothing on Home signals that an Issue is
waiting. This supports the Portal finding: assigned work and pending requests
need to reach the first screen a person sees, whichever page that is.

## Not Exercised Or Not Observable

- **Native window focus,** the native folder chooser (stubbed; see Explored
  2), the packaged app's layout and fonts, native menus, and OS
  notifications. All observation went through the `wails dev` browser bridge.
- **First launch without models.** The sandbox's `settings.yaml` was seeded
  from the contributor's home by `./make run desktop-dev`, so the "no model
  configured" state was never seen.
- **A second turn in an existing session,** cancelling a turn, forking or
  rewinding history, Launchpad, and memory.
- **Writes from the Issues view** (**Move to**, posting a comment) and
  sign-out.
- **Accessibility tooling:** keyboard-only operation of the workbench, screen
  readers, and zoom.

## Cleanup

- The local schedule was deleted through the UI.
- `wails dev` and the driver browser were stopped, and the shared bridge lock
  was released at 14:53.
- The sandbox (`testing-sandbox/`) and the sample repository under
  `.artifacts/ui-audit/greeter/` are gitignored and local to this worktree.
- The kind cluster used for sign-in was removed with `./make kind down` at the
  end of the Portal audit.

## Follow-Up

Draft phase 4 tasks from D1–D4 first:

1. Session adoption keeps the optimistic message.
2. The approval card renders a diff for Edit and Write.
3. Start chat opens a draft chat in any project state.
4. Schedules show and pin their model across sign-in.

D10 and D12 are trivial fix candidates: keep the dialog footer outside the
scroll area, and clear stale errors on change.
