# Portal

> **简体中文：** [阅读中文镜像](../../zh-CN/contribute/architecture/portal.md)
> **Audience:** contributors · **Status:** current

## Purpose

The Portal is the space collaboration surface under `portal/`. It is a React 19,
Vite, and TypeScript app that talks to the Go server over HTTP and WebSocket.

Portal owns the cloud/space lane:

- login/signup
- space/space switching and settings
- conversations
- issues
- workflows and workflow runs
- run diagnostics: what a task run used, touched, spent, why it ended, and what
  confined it
- stopping a run in flight: Issue Detail offers Stop Run for a task that is still
  pending or running, and the button stays until the server's answer, because a
  started run only reaches `canceled` when its worker confirms
- repeating a finished run: the same rows offer Retry Run once the run is over,
  whatever it ended as, and show the server's own reason when it refuses
- the space audit trail, for owners
- agents
- Space-level Agent instructions, inherited by every background Agent run and
  revisioned at worker claim time; they do not alter the Tier 1 coordinator
- artifacts: the space's durable files, listed and opened at their own opaque
  address rather than through the run that produced them
- space files
- usage and webhook keys

## Current Shape

- Routes are in `portal/src/router.ts`.
- Pages live under `portal/src/pages/*`.
- API calls live under `portal/src/features/*/api.ts` and `portal/src/lib/api`.
- Shared presentation components come from `@buildmax/gui`.
- The shared `Button` and `IconButton` own action appearance, target size, focus,
  and busy state. Portal owns placement and permission decisions. Collection
  creation stays in the header, while an empty state explains what is missing.
  Issue Detail opens in read mode with its result and next action before editing.
  Task Detail and conversation task cards use the same action roles; the Chat
  start page owns its heading and keyboard-operated tabs.
  Workflow Detail assigns one primary action per lifecycle view: Publish for
  drafts, Run for published read mode, and Save while editing a published flow.
  Agent Detail does the same for Run, configuration Save, and schedule creation;
  its tabs are keyboard operated and schedule cards own their retry state.
  Artifacts own upload as the collection action; detail and sharing controls
  use the shared action roles, and preview retry stays inside the preview.
  Administration, Space settings, Files, Marketplace, and sign-in use the same
  roles. Portal CSS defines no button geometry, color, or focus rule of its own:
  a page passes a class to `Button` only for placement. An in-flight action sets
  `busy` rather than swapping its label.
- Cross-cutting state lives in `portal/src/contexts/` — `AppContext`,
  `AuthContext`, `SpaceContext`, and `WebSocketContext`, which carries
  conversation streaming.
- The HTTP layer is `portal/src/lib/api/` (`client`, `mappers`, `types`, plus
  `sse` and `ws` for streaming transports).
- `portal/src/features/conversations/` draws the transcript and, in the same
  thread, one card per background task the conversation started. The cards are
  read from the tasks route and reloaded on every invalidation the socket
  reports, so what a run produced does not depend on the summary Tier 1 writes
  about it. `thread.ts` decides the order.
- `portal/src/features/runs/` reads a task run's trace summary, where the run
  came from, and the managed
  model calls the deployment served for it, and `portal/src/features/audit/`
  reads the space audit trail. Both keep their
  display decisions in a pure module — `summary.ts`, `spend.ts`, and
  `describe.ts` — rather
  than inside the component, because Portal has no DOM test environment and the
  judgements worth pinning are exactly the ones that would otherwise go
  untested: an unsandboxed run must say so, an unrecorded boundary is not the
  same as an unconfined one, an empty model-call ledger is not the same as a
  run that spent nothing, and an audit action this Portal does not recognise is
  shown verbatim rather than hidden.

## Testing

Unit tests are Vitest over pure modules; `vite.config.ts` excludes `e2e/` from
them. Portal has no DOM test environment, so display decisions live in pure
modules — `features/runs/summary.ts`, `features/runs/spend.ts`,
`features/audit/describe.ts`, `features/usage/pressure.ts`,
`features/conversations/thread.ts`, `features/runs/origin.ts`,
`features/artifacts/display.ts` — where they can be asserted without one. The
artifact one mirrors a server authorization rule to decide whether to offer a
delete button, so it is pinned in both directions: a mirror that drifts either
shows a button that is refused or hides one that would have worked.

`portal/e2e/` holds Playwright specs, run by `./make e2e` against a deployment.
They cover only what a browser can show: that the published bundle works
against a real server. The API-level flow belongs to `./make kind smoke`, and
repeating it here would be slower and no more informative.

`./make e2e` issues the login codes, because a code arrives out of band by
design and the browser cannot fetch one. Playwright's global setup signs in and
saves a session per role — the deployment administrator and an account holding
no grant — which is also why signing in is not a separate spec: a break in it
fails the whole suite before the first test. Two accounts exist because a
role-specific view can only be proved by someone who does not have the role.

`./make e2e` defaults to the kind deployment; `./make e2e compose` runs the same
specs against the quickstart stack; `./make e2e local` starts a Compose stack,
runs them, and takes it down again, which is the shape to use when nothing is
running yet. Both attached targets are in `deployment-smoke.yml`, because the
two differ in ways a browser can see. kind serves Portal and server from one
ingress, so the bundle's API base is same-origin; Compose publishes them on
separate ports, so it is absolute. A spec that needs to know is told through
`BUILDMAX_E2E_API_BASE` rather than assuming either shape — the task runner
passes whichever it just pointed the browser at. Every run writes its evidence
to `.artifacts/e2e/portal/`, cleared beforehand so a failure's traces are never
mixed with an older run's, alongside a note naming the deployment and the
command that reproduces it.

`run-trace.spec.ts` is the exception to "seed nothing". The run trace view
opens only from an issue's outputs, and the API-level smoke creates a
conversation task, which has none — so the spec creates the issue and agent run
through the API before reading the result through the UI. That keeps the pure
module's claim about boundaries honest in the one place it is actually shown:
`summary.ts` proves the wording, this proves a real run reaches it.

## Product Boundary

Portal is the cloud space workspace. CLI and Desktop are the local execution
lane. Desktop may bridge to Portal later, but Portal remains the place for
space administration, issue/workflow management, space files, governance, and
cloud results.
