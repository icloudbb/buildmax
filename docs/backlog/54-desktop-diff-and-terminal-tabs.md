---
id: desktop-diff-and-terminal-tabs
title: Make Desktop's diff tab readable and keep terminal output when the person returns
roadmap: R6
source: docs/design/ui-experience-program.md#phases
depends_on: []
verification: ["./make check desktop", "./make e2e desktop-ui", "./make e2e visual"]
claim:
pr:
---

## Outcome

Reviewing an Agent's change in the diff tab shows clearly what was added and
removed, in both themes and both languages. A terminal still shows its earlier
output after the person visits another view. This task carries Desktop audit
findings D8, D16, and D19.

## Findings

**D8 (Minor). The diff view is hard to read.**

- Screen: Explorer → **Changes** → a changed file, which opens a diff tab.
- Actual:
  - The raw Git headers are shown (`diff --git …`, `index …`).
  - An added line is marked only by a faint tint, with no "+". In the dark
    theme the tint is nearly invisible, so the meaning is carried by color
    alone.
  - There is no revert or accept action next to the Agent's change.

**D16 (Minor). Terminal scrollback is lost after leaving the workbench.**

- Reproduction:
  1. Open **New terminal** in a project and run `ls && go run ./src`.
  2. Visit Schedules or Issues, then return to the workbench.
- Actual: Terminal 1 is blank. The shell session survives, and new output
  appears.

**D19 (Cosmetic).** In Chinese, the diff tab title "main.go (diff)" stays in
English.

## Scope

- Hide the raw Git headers. Show a file header and hunks, with "+" and "−"
  markers and a background tint that meets contrast in both themes.
- Find out why returning to the workbench clears the terminal, then keep its
  scrollback across view switches. The Go side already has a terminal
  snapshot store (`internal/interface/desktop/terminalsnapshot.go`); check
  whether the frontend restores from it, or whether it remounts the terminal
  without its buffer.
- Take the diff tab's title suffix from the catalog.

## Out Of Scope

- Revert or accept actions on an Agent's change. Desktop has no such operation
  today, so adding one is new capability, which the
  [program's non-goals](../design/ui-experience-program.md#non-goals) exclude.
  Raise it separately if it is wanted.
- The approval card's diff, which is
  [task 48](26-desktop-tool-approval-preview.md). Share the renderer with it.

## Acceptance Criteria

- The diff tab shows no `diff --git` or `index` line. Every added and removed
  line carries a "+" or "−" marker. Axe contrast passes in light and dark.
- After running a command, visiting Schedules, and returning, the terminal
  shows the earlier output.
- The diff tab title is translated in zh-CN.

## Verification

1. `./make check desktop`.
2. `./make e2e desktop-ui`.
3. `./make e2e visual`. Add a diff view to the Desktop baselines if none
   exists.

## Notes

Audit context: the phase 0 Desktop audit ran on 2026-10-06 against `main` at
`e98efc7a`. It drove `./make run desktop-dev` through the browser bridge on
macOS, with a throwaway Git repository `greeter` whose `src/main.go` an Agent
edited. The operator was an Agent with repository knowledge. The full report
is kept in history at
[`718a6ab3`](https://github.com/icloudbb/buildmax/blob/718a6ab3969d35bdba7f66013d8ccfefe7549af8/docs/contribute/exploratory-runs/2026-10-06-desktop-ui-journey-audit.md).
