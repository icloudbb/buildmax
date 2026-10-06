---
id: desktop-chat-turn-echo
title: Show a sent Desktop message at once and end every turn legibly
roadmap: R6
source: docs/design/ui-experience-program.md#phases
depends_on: []
verification: ["./make check desktop", "./make e2e desktop", "./make e2e desktop-ui"]
claim: gougoujiang 2026-10-07
pr:
---

## Outcome

A message appears in the transcript the moment it is sent, including the
first message of a new chat, and stays there while the turn runs. A turn that
ends without text ends cleanly. The composer gives its hint once. This task
carries Desktop audit findings D1 (Major) and D6, and the duplicated hint from
D18.

## Findings

**D1 (Major, state legibility). A new chat does not show the sent message
while its first turn runs.**

- Screen: a workbench chat tab.
- Reproduction:
  1. Open a project.
  2. Type a prompt in **New Chat** and press Enter.
- Actual:
  - For the whole turn, tool calls and approvals included, the transcript
    kept "Type a message below to start a new chat." and showed no user
    bubble.
  - The message appeared only after the turn ended.
- From reading the code: `ChatSession.handleSend`
  (`desktop/frontend/src/components/ChatSession.jsx`) appends the message
  optimistically. The unverified hypothesis is that adopting the new session's
  ID discards that local state.
- The audit saw this once, on a new chat. It did not test later turns in an
  existing session.

**D6 (Minor). A turn that ends without text leaves a blank bubble.**

- Reproduction: a turn whose final assistant message is empty. In the audit,
  GPT-5.6 Luna made an Edit and a Write, then ended with 4 completion tokens
  and no text.
- Actual: the transcript ends in an empty white bubble, with no "done" and no
  summary.

**D18, composer part (Cosmetic).** The composer says "/ for commands" twice:
once in the placeholder (`chat.composer.placeholder`, "Type a message… (/ for
commands, Enter to send)") and once in `chat.slashHint`, both in
`desktop/frontend/src/i18n/chat.js`.

## Scope

- Find the root cause of D1 before changing code. Keep the optimistic message
  through session adoption on a new chat. Cover a second turn in an existing
  session as well.
- Render no empty assistant bubble. The end of the turn stays visible through
  the existing tool cards and the turn's completed state.
- Keep one slash-command hint, in both locales.

## Out Of Scope

- The emoji 💬 tab icon from D18. It is part of the single icon set in
  [task 20](20-desktop-gui-convergence.md).
- Tool card and approval rendering, which is
  [task 26](26-desktop-tool-approval-preview.md).

## Acceptance Criteria

- A vitest test sends the first message of a new chat, simulates session
  adoption, and asserts that the user bubble stays visible throughout. A
  second test covers an existing session.
- A turn whose final message is empty renders no empty bubble.
- The composer shows the slash hint once.

## Verification

1. `./make check desktop`.
2. `./make e2e desktop`, if the bridge's session events change.
3. `./make e2e desktop-ui`.

## Notes

Audit context: the phase 0 Desktop audit ran on 2026-10-06 against `main` at
`e98efc7a`. It drove `./make run desktop-dev` through the browser bridge on
macOS, with a throwaway Git repository `greeter`. The editing chat used
GPT-5.6 Luna and the prompt "Edit src/main.go so main also prints "goodbye" on
a second line, and delete the TODO line from NOTES.txt. Keep the change
minimal." That run cost about $0.0018. The operator was an Agent with
repository knowledge. The full report is kept in history at
[`718a6ab3`](https://github.com/icloudbb/buildmax/blob/718a6ab3969d35bdba7f66013d8ccfefe7549af8/docs/contribute/exploratory-runs/2026-10-06-desktop-ui-journey-audit.md).
