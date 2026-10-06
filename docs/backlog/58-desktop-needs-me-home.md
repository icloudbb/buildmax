---
id: desktop-needs-me-home
title: Show the work that needs the signed-in person on Desktop's Home
roadmap: R6
source: docs/design/ui-experience-program.md#d6-the-landing-page-shows-the-work-that-needs-the-person
depends_on: [38-portal-needs-me-landing.md]
verification: ["./make test", "./make e2e desktop", "./make check desktop", "./make e2e desktop-ui"]
claim:
pr:
---

## Outcome

When Desktop is signed in to a server, its Home shows what is waiting on the
person: the same four groups that task 22 defines, across their Spaces. Each
item opens where it is resolved. This applies the program's D6 decision to
the first screen Desktop shows.

## Evidence

From the phase 0 Desktop audit's landing-page evidence: Desktop has no Space
landing page. Its Home lists recent chats and projects. After sign-in, owned
Issues sit in a separate **Issues** view, which stays empty until Portal
assigns work, and nothing on Home signals that an Issue is waiting.

These were checked on `main` at `e36fa722`. Desktop's Issues inbox
(`ListMyIssues` in `internal/interface/desktop/issues.go`) already fans out
over every Space the person belongs to, using `owner=me` and the
`internal/interface/client` package.

## Scope

- Bind a method that calls task 22's query for each of the person's Spaces,
  the same way `ListMyIssues` fans out. Report a Space that fails as a
  warning, not as an empty result.
- On Home, while signed in, show the groups with counts. Each item opens where
  it is resolved: the Issue in the Issues view, or the run or request in
  Portal when Desktop has no view for it.
- Show nothing extra while signed out, and keep the local Home unchanged.
- Provide English and zh-CN text.

## Out Of Scope

- Defining "needs me", which task 22's design owns. This task reuses it
  unchanged.
- Notifications or background polling beyond Home's existing refresh.

## Acceptance Criteria

- Signed in as `alice@buildmax.local` against `./make kind fixtures --runs`
  data, Home shows her failed run, the "Needs your answer" runs, the pending
  Workflow request, and her owned Issue that needs attention, each with a
  working link.
- A Space whose request fails shows a warning, and the other Spaces still
  list their items.
- Signed out, Home is unchanged.

## Verification

1. `./make test` for the bound method.
2. `./make e2e desktop`, for the bridge call.
3. `./make check desktop`, then `./make e2e desktop-ui`.

## Notes

This task depends on task 22, which designs and ships the server query.

Audit context: the phase 0 Desktop audit ran on 2026-10-06 against `main` at
`e98efc7a`. It drove `./make run desktop-dev` through the browser bridge on
macOS, signed in to the Portal audit's kind cluster. The operator was an Agent
with repository knowledge. The full report is kept in history at
[`718a6ab3`](https://github.com/icloudbb/buildmax/blob/718a6ab3969d35bdba7f66013d8ccfefe7549af8/docs/contribute/exploratory-runs/2026-10-06-desktop-ui-journey-audit.md).
