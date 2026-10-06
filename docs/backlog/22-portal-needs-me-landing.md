---
id: portal-needs-me-landing
title: Show the work that needs the signed-in person on the Portal page they land on
roadmap: R6
source: docs/design/ui-experience-program.md#d6-the-landing-page-shows-the-work-that-needs-the-person
depends_on: []
verification: ["./make check docs", "./make test", "./make check portal", "./make e2e visual", "./make e2e kind"]
claim:
pr:
---

## Outcome

A Space member who opens Portal sees the work waiting on them without going
looking for it:

- Issues they own that need attention;
- failed runs;
- runs waiting on their answer;
- pending Workflow input requests.

Each item links to the place where it is resolved. On 2026-10-07 the maintainer
decided that whatever page a signed-in person lands on must show this. Today
no page does.

## Evidence

From the phase 0 Portal audit (Journey 1, first sign-in, and the landing-page
evidence):

- **Team member with work.** `alice@buildmax.local` owns an Issue in the
  fixture Space "BuildMax QA". That Space has a failed run, 2 Task runs at
  "Needs your answer", and 1 pending Workflow input request. Her landing
  page, **Chat**, showed one old conversation and none of that. The **Issues**
  page did not show the waiting requests either. They were reachable only
  through **Agents → QA Writer → Runs**, or through Administration's counts.
- **New person.** `nora@buildmax.local` landed on **Chat** in her empty
  personal Space. Chat was the only page that offered an immediate action.
- **Space switching** keeps the current section, so the landing page matters
  on first entry and on a bare `#/`.

## This Task Needs A Design First

The decision fixes the outcome. It does not choose the query, the meaning of
"mine", or the host page. These facts were verified on `main` at `e36fa722`:

- `GET /api/spaces/{space_id}/issues?owner=me` lists the caller's owned Issues.
  An Issue response carries status, owner, executor, and child and comment
  counts, but **no run state**, so "needs attention" cannot be computed from it.
- A Task (`internal/core/task/task.go`) carries `CreatedBy`, `RequestedBy`
  (the person behind a Space Assistant dispatch), `IssueID`, `Status`, and
  `AwaitingAnswer`. But Tasks are listed only per Agent, Conversation, or
  Schedule. **No route lists a Space's Tasks** by creator, state, or
  awaiting answer.
- `GET /api/spaces/{space_id}/workflow-requests` lists the Space's pending
  Workflow requests. Any member who may run the Space's Workflows may answer
  one, and the first answer wins. Portal fetches this list only on the
  Workflows page.
- Desktop's Issues inbox (`internal/interface/desktop/issues.go`) already fans
  out per Space with `owner=me`, across every Space the person is in.
- The [navigation record](../design/portal-navigation-and-space-context.md#alternatives-rejected)
  rejected a dashboard built "without an authoritative aggregate query and
  validated operator questions". The audit supplies the questions; the query
  is still missing.

So this task needs **server/API work and a design section before any code**.
Write that section in the navigation record, because it owns the default
destination and the rejected dashboard. Update its zh-CN mirror. Take the
decision to the maintainer in the shape that
[backlog/README.md](README.md#from-roadmap-to-ready-tasks) step 3 describes,
and build only after approval. The design answers:

1. **Attention.** When does an owned Issue need attention? *Recommended:* when
   its latest run failed, is awaiting an answer, or has a pending Workflow
   request. Do not add staleness or dismissal state.
2. **Mine.** When is a run "mine"? *Recommended:* when the person created it,
   requested it through an Assistant, or owns its origin Issue.
3. **Failed runs.** Which failed runs show? *Recommended:* a Task whose latest
   run failed. Continuing or retrying it clears the entry, so no new
   "acknowledged" state is needed.
4. **Workflow requests.** Which requests show? *Recommended:* every pending
   request in the Space, because no request names a responder today.
5. **Scope.** *Recommended:* Space-scoped, which keeps the authorization
   boundary. Desktop fans out per Space, the same way it does for Issues.
6. **Query shape.** *Recommended:* one Space-scoped read in the service layer
   that returns the four bounded groups. That gives "needs me" a single
   authoritative definition, which Portal and Desktop
   ([task 58](58-desktop-needs-me-home.md)) both reuse. *Alternative:* filters
   on the existing lists (a Space Task list by creator and state, plus
   latest-run state on Issues). This reuses more, but every client has to
   repeat the definition.
7. **Host page.** *Recommended:* the Issues page gains the section above the
   collection and becomes the default Space destination. Issue is the primary
   work object, and the audit concluded that Issues would be the better
   default once it shows this. *Alternative:* keep Chat as the landing page
   and add the section there. An empty Space still needs an obvious first
   action in either case.

## Scope

After the design is approved:

- Add the service query and route, with its `openapi.json` entry, Space
  authorization, and a row in the authorization matrix test.
- Build the Portal section on the chosen host page. Each item links to its
  resolution: the Task page to answer or diagnose, the Workflow request's
  respond action, or the Issue.
- Make the chosen page the default destination for `#/` and first Space entry.
- Show empty, loading, and error states. Loading never claims zero.
- Provide English and Simplified Chinese text.

## Out Of Scope

- Desktop's Home, which is [task 58](58-desktop-needs-me-home.md).
- A cross-Space inbox, notifications, push, snooze or dismiss state, and
  live updates beyond the page's existing refresh.
- Changing who may answer a Workflow request.

## Acceptance Criteria

- The design section is merged in English and zh-CN, with the maintainer's
  approval recorded.
- One service method defines "needs me". The route is in `openapi.json`, and a
  non-member is refused like every other Space route.
- On `./make kind fixtures --runs` data, Alice's landing page in "BuildMax QA"
  lists the failed run, both "Needs your answer" runs, the pending Workflow
  request, and her owned Issue that needs attention. Each one opens where it is
  resolved. Nora's empty personal Space shows nothing waiting and still offers
  a first action.
- The page renders in both locales at 390, 768, and 1280 px without horizontal
  overflow.
- The user manual's Portal pages, the Portal architecture document,
  `current-state.md`, and a changelog entry describe the result.

## Verification

1. `./make check docs` for the design section.
2. `./make test` for the service, handler, and authorization matrix. Run
   `./make test mysql` if the change touches `internal/infra/db`.
3. `./make check portal`, then `./make e2e visual`. Update the baselines if the
   landing page's look changes on purpose.
4. Use the kind loop from `docs/contribute/testing.md` with
   `./make kind fixtures --runs`, then run `./make e2e kind`.

## Notes

Audit context: the phase 0 Portal audit ran on 2026-10-06 against `main` at
`e98efc7a`. It used an ephemeral kind cluster, the in-cluster mock model, and
`./make kind fixtures --runs` data. The operator was an Agent with repository
knowledge. The full report is kept in history at
[`718a6ab3`](https://github.com/icloudbb/buildmax/blob/718a6ab3969d35bdba7f66013d8ccfefe7549af8/docs/contribute/exploratory-runs/2026-10-06-portal-ui-journey-audit.md).
