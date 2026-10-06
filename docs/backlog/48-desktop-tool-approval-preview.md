---
id: desktop-tool-approval-preview
title: Show the exact change a Desktop tool approval grants, and default to the narrowest grant
roadmap: R6
source: docs/design/ui-experience-program.md#phases
depends_on: []
verification: ["./make test", "./make check desktop", "./make e2e desktop", "./make e2e desktop-ui"]
claim:
pr:
---

## Outcome

Before approving an Edit or a Write, the person reads the change as it will
be applied, whitespace included, and is told when a file will be emptied or
overwritten. Pressing Enter never grants more than one call. Tool cards show
enough of their arguments to identify the target. This task carries Desktop
audit findings D2 (Major), D5, and D7.

## Findings

**D2 (Major, error prevention and state legibility). The tool approval card
misrepresents the change being approved.**

- Screen: the inline **Tool approval** card in a chat.
- Reproduction: in a project, ask the Agent to add a line to `src/main.go`
  and empty `NOTES.txt`, as with the audit's prompt below. Wait for the
  approvals.
- Actual:
  - An Edit is shown as raw key/value rows: `file_path`, `new_string`,
    `old_string`, and `replace_all`.
  - Newlines and indentation are collapsed. `new_string` reads
    `fmt.Println("hello") fmt.Println("goodbye")` on one line, although the
    real argument was two indented lines.
  - The Write that emptied `NOTES.txt` shows `content` with an empty value,
    and does not say that the file will be emptied.
  - There is no diff preview.
- The person approves a change they cannot read accurately.

**D5 (Minor). The default choice in an approval can become the broader
grant.**

- Reproduction: answer the first approval with **Allow session**, then look
  at the second approval.
- Actual: the highlighted default became **Allow session(a)**. The footer
  says "Enter confirm", so one keypress grants the wider permission.

**D7 (Minor). Tool cards hide their arguments.**

- Actual:
  - Arguments are cut at about 15 characters ("Read (src/mai…") in narrow
    cards, even when there is room to spare.
  - The Write card shows raw JSON (`{"content":"","file_path":"NOT…`).

## Scope

- In `desktop/frontend/src/components/ApprovalPanel.jsx`, render Edit and
  Write as a diff against the file's current content. Preserve whitespace.
  State plainly when a Write creates, overwrites, or empties a file. If the
  frontend lacks the current content, add a bound method that reads it, kept
  inside the project's tool root (`internal/interface/desktop/approval.go`).
- Render other tools' arguments with whitespace preserved, not as collapsed
  rows.
- Make the narrowest option (allow once) the default for every approval,
  whatever earlier approvals answered.
- In tool cards, show the target path, truncated from the start so the file
  name stays, using the available width. Never show raw JSON as the card
  summary.

## Out Of Scope

- Changing the permission model or the grant scopes themselves.
- The Explorer diff tab. [Task 50](50-desktop-diff-and-terminal-tabs.md) also
  needs a readable diff. Whichever task lands first owns the diff renderer and
  the other reuses it. It moves to gui only if Portal needs it too, under the
  program's D4 rule.

## Acceptance Criteria

- For the audit's Edit, the approval shows the two indented lines as an added
  hunk against the current file.
- For the Write that empties `NOTES.txt`, the approval says that the file will
  be emptied and shows the removed lines.
- The default focused choice is "allow once" on every approval, covered by a
  vitest test that first answers one approval with "allow session".
- Tool cards show `src/main.go` in full in a standard-width chat.

## Verification

1. `./make test`, if a bound Go method is added.
2. `./make check desktop`, including `ApprovalPanel.test.jsx`.
3. `./make e2e desktop`, because approvals cross the bridge.
4. `./make e2e desktop-ui`.

## Notes

Audit context: the phase 0 Desktop audit ran on 2026-10-06 against `main` at
`e98efc7a`. It drove `./make run desktop-dev` through the browser bridge on
macOS, with a throwaway Git repository `greeter`. GPT-5.6 Luna ran the prompt
"Edit src/main.go so main also prints "goodbye" on a second line, and delete
the TODO line from NOTES.txt. Keep the change minimal." The trace recorded 4
tool calls (Read, Read, Edit, Write), and the `tool_start` arguments confirm
the two-line `new_string`. The operator was an Agent with repository
knowledge. The full report is kept in history at
[`718a6ab3`](https://github.com/icloudbb/buildmax/blob/718a6ab3969d35bdba7f66013d8ccfefe7549af8/docs/contribute/exploratory-runs/2026-10-06-desktop-ui-journey-audit.md).
