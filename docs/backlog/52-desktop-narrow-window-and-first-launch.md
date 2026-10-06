---
id: desktop-narrow-window-and-first-launch
title: Keep Desktop usable in a narrow window and honest on first launch
roadmap: R6
source: docs/design/ui-experience-program.md#phases
depends_on: []
verification: ["./make check desktop", "./make e2e desktop-ui", "./make e2e visual", "./make build desktop", "./make e2e desktop-launch"]
claim:
pr:
---

## Outcome

Desktop's content stays readable at any window size it allows, and a first
launch invites the person to start rather than to "continue". This task
carries Desktop audit findings D9 and D17.

## Findings

**D9 (Minor). Narrow windows break the layout.**

- Screen: Home and the Issues detail.
- Actual:
  - The sidebar keeps 288 px at any width (`SIDEBAR_DEFAULT_WIDTH` in
    `desktop/frontend/src/App.jsx`). At 390 px the content column is about
    100 px, and headings break letter by letter.
  - At 768 px the Issue detail column is about 100 px. Its title breaks
    mid-word ("Unassigne d"), its ID is clipped, and the tab bar clips the
    "+" button.
  - `internal/interface/desktop/run.go` sets only `Width: 1280` and
    `Height: 800`, with no minimum window size.
  - **Hide sidebar** recovers the layout.

**D17 (Cosmetic). First launch says "continue" with nothing to continue.**

- Screen: Home on first launch, and the New Project dialog.
- Actual:
  - "Continue your work" heads a page whose "Recent chats" and "Recent
    projects" are empty.
  - The New Project name placeholder says "My Project". With the name left
    empty, the project is named after its folder.

## Scope

- Set a minimum window size in the Wails options.
- Below a width breakpoint, collapse the sidebar into the existing hidden
  state, or narrow it, so the content column keeps a readable minimum. Make
  the Issue detail and the tab bar reflow rather than clip.
- On a first launch with no recent work, give Home a heading that invites
  starting, such as "Open a project to start". Make the New Project
  placeholder say that the folder name is used when the name is left empty.
- Provide English and zh-CN text.

## Out Of Scope

- Decomposing `App.jsx` beyond the sidebar-width logic this task touches.
- Desktop's Issues behavior, which is
  [task 56](30-desktop-issue-start-chat-and-sign-in.md).

## Acceptance Criteria

- The packaged app cannot be resized below the minimum.
- In the browser bridge at 390 and 768 px, no heading breaks inside a word,
  and the Issue detail title and the tab bar's "+" are fully visible.
- A fresh home shows the start-oriented heading. The placeholder matches the
  naming behavior.

## Verification

1. `./make check desktop`.
2. `./make e2e desktop-ui`.
3. `./make e2e visual`. The fresh-home and new-project baselines change on
   purpose.
4. `./make build desktop` and `./make e2e desktop-launch`, because `run.go`
   builds only under the desktop tag.

## Notes

Audit context: the phase 0 Desktop audit ran on 2026-10-06 against `main` at
`e98efc7a`. It drove `./make run desktop-dev` (`wails dev`) through the browser
bridge on macOS, resizing the viewport for widths. The operator was an Agent
with repository knowledge. The browser bridge cannot show the packaged
window's real minimum, which is why the packaged launch is verified
separately. The full report is kept in history at
[`718a6ab3`](https://github.com/icloudbb/buildmax/blob/718a6ab3969d35bdba7f66013d8ccfefe7549af8/docs/contribute/exploratory-runs/2026-10-06-desktop-ui-journey-audit.md).
