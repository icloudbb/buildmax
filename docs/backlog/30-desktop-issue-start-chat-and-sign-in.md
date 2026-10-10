---
id: desktop-issue-start-chat-and-sign-in
title: Make Desktop's Issue Start chat open a draft chat, and match sign-in to how server accounts sign in
roadmap: R6
source: docs/design/ui-experience-program.md#phases
depends_on: []
verification: ["./make check desktop", "./make e2e desktop-ui", "./make test"]
claim: gougoujiang 2026-10-10
pr:
---

## Outcome

Choosing **Start chat** on an Issue always opens a new chat in the chosen
project, with the Issue in the message box. The sign-in page leads with the
way server accounts actually sign in, in a person's words. The Issues view
speaks consistently and does not lead with IDs. This task carries Desktop
audit findings D3 (Major), D14, and D15. It also decomposes the part of
`App.jsx` that D3 touches.

## Findings

**D3 (Major, the journey step fails silently). Issues → Start chat does not
open a chat with the Issue.**

- Screen: the Issues view, then the workbench.
- Reproduction:
  1. Sign in to a server.
  2. Open **Issues** from Home and select an Issue assigned to you.
  3. Leave Project = greeter and choose **Start chat**.
- Actual: the workbench reopens the project's previous tabs with **Terminal
  1** active. No new chat tab appears, and no composer contains the Issue
  text. The hint had promised "Starts a new chat with this issue in the
  message box".
- From reading the code: `handleNewChatInProject` in
  `desktop/frontend/src/App.jsx` adds a new chat tab only when the project is
  already current. Here it was not, because the view was reached from Home.

**D14 (Minor). The sign-in page does not match the account flow, and speaks
developer.**

- Screen: user menu → **Sign in to a server**.
- Actual:
  - It leads with Email and Password, while server accounts here sign in with
    login codes. The login-code path worked.
  - Its copy cites "settings.yaml" and "Behind an ingress it is the origin the
    Portal is on".
  - The default server is `http://localhost:5678`.
  - It has no theme or language control. The audit could not switch this page
    to Chinese without a reload that leaves it, so its Chinese rendering was
    not captured.

**D15 (Cosmetic). The Issues view's wording disagrees with itself.**

- Actual: the header says "Open Space work you own", while the empty state
  says "No open issues are assigned to you". The detail shows the raw Issue
  ID.

## Scope

- Make **Start chat** work whether or not the chosen project is current:
  switch to the project, open a new chat tab, and prefill the composer. Move
  the "open project, then start a draft chat" orchestration out of `App.jsx`
  into a module or hook with its own vitest test. This is phase 4's "decompose
  App.jsx where the findings touch it".
- Make the sign-in page lead with the login-code path, keeping password
  sign-in available for accounts that have one. Rewrite the server-address
  help in person-facing terms, such as "the address you open Portal at". Do
  not present a developer address as the answer. Make the theme and language
  controls reachable on the page.
- Use one term, "owned", throughout the Issues view, and move the Issue ID to
  secondary metadata.
- Provide English and zh-CN text.

## Out Of Scope

- Showing waiting work on Home, which is
  [task 58](58-desktop-needs-me-home.md).
- Server-side sign-in changes.

## Acceptance Criteria

- From Home → Issues → an Issue → **Start chat**, with a project that is not
  current, a new chat tab opens in that project with the Issue text in the
  composer. A vitest test covers both the current-project and
  other-project cases.
- The sign-in page offers the login-code path first, contains no
  `settings.yaml` or ingress wording, and can be switched to Chinese in place.
- The Issues view's header and empty state use the same term, and no raw ID is
  a primary label.

## Verification

1. `./make check desktop`.
2. `./make e2e desktop-ui`. The sign-in baseline changes on purpose, so also
   run `./make e2e visual`.
3. `./make test`, if a bound Go method changes.

## Notes

Audit context: the phase 0 Desktop audit ran on 2026-10-06 against `main` at
`e98efc7a`. It drove `./make run desktop-dev` through the browser bridge on
macOS. Sign-in used the Portal audit's ephemeral kind cluster as
`alice@buildmax.local`, with a login code minted by `./make kind login`. The
Issues view stayed empty until an Issue was assigned to Alice in Portal; after
**Refresh** it listed that Issue. **Move to** and posting comments were not
exercised. The operator was an Agent with repository knowledge. The full
report is kept in history at
[`718a6ab3`](https://github.com/icloudbb/buildmax/blob/718a6ab3969d35bdba7f66013d8ccfefe7549af8/docs/contribute/exploratory-runs/2026-10-06-desktop-ui-journey-audit.md).
