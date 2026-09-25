# Changelog

All notable changes to BuildMax are documented in this file.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).
BuildMax is currently in alpha, so incompatible changes may still occur between
pre-releases and must be called out in release notes.

## [Unreleased]

Unreleased entries live one per file under
[`docs/changelog/`](docs/changelog/README.md), so two branches adding one never
touch the same line. `./make changelog` prints what they currently say, and
release preparation folds them into a dated section here.

## [0.2.0-alpha.15] - 2026-09-25

### Added

- Local CLI runs can now drive a headless browser: when a system Chrome, Edge,
  or Chromium is installed, the agent can open an http(s) page, inspect its
  elements and console, click, type, and screenshot to verify a change against
  the real rendered page.

- Add a built-in WebSearch tool for public web results, with keyless access
  and an optional Firecrawl API key for higher limits.

- On Desktop you can now open a live view of the Agent's browser page inside a
  workspace tab — click the browser indicator in the status bar. It streams the
  same page the Agent drives (read-only).

- On Desktop the Agent's browser now opens as a visible window you can watch,
  with a status-bar indicator of the page each chat session is on.

- Desktop: a Launchpad in the bottom status bar lets you pin your own
  applications (for example VS Code) and websites and open them with one click.

- Desktop: double-click a chat or terminal tab to rename it in place, without
  opening the right-click menu.

- Desktop: a "+" button at the end of each workspace tab strip starts a new
  chat in that pane, so a new conversation no longer requires the project
  sidebar.

- Desktop: terminal tabs are restored after a restart — each reopens as a fresh
  shell in the project workspace with its last visible output replayed above the
  new prompt, instead of vanishing.

- Add experimental local app connectors with browser OAuth, fixed read and
  confirmed write CLI operations, and a Gmail sample plugin; also add remote
  MCP connection, tool discovery, and tool calling commands with optional
  Bearer token environment variables.

- Replace a catalog model's provider key in place with
  `buildmax admin model set-key <id>`, `buildmax-server model set-key --id`, or
  `PUT /api/admin/llm/models/{model_id}/credential`. The model keeps its name, so
  no client changes what it selects. `--api-key -` on either `model add` reads
  the key from standard input, and `./make kind seed` refreshes the keys of
  models it seeded before and skips example placeholder keys.

- Remote Control: start a local session with `buildmax --remote-control` to make
  it reachable through the server, then from another device under Remote Control
  in Portal watch it, send follow-up messages, approve or deny its tool calls,
  and stop a running turn. Execution and files stay on your machine.

- Schedules can now run a published workflow on a cron timetable, not just an
  agent: create one from the workflow's detail page or the space Schedules page,
  and each firing starts a workflow run.

- The space Schedules page has **Pause all** and **Resume all** buttons that
  enable or pause every schedule in the space at once, to halt or restart all
  unattended work without touching each row.

- Show the current session's complete fork tree in `buildmax info` and the
  TUI/Desktop `/info` panels, including surviving branches whose source session
  was deleted.

- Talk to your BuildMax assistant from Telegram: an operator connects a bot with
  `channels.telegram.bot_token`, people link their Telegram account under
  Account → Chat accounts, and the bot answers in their Space and reports when
  work it started finishes.

### Changed

- The deployment Administration pages now use the site's dark primary button
  for each search form and theme-colored list links, instead of white submit
  buttons and off-theme blue links in light mode.

- The deployment Administration area now lists its sections (Overview,
  Accounts, Spaces, and the rest) in the sidebar under the Deployment scope
  instead of as in-page tabs, matching how the rest of the app navigates.

- The deployment admin Spaces view now lists team spaces as a paginated table
  and omits every account's personal space, which is noise an operator does not
  govern.

- Desktop scheduled tasks now run in a working directory (your home directory by
  default) instead of requiring a project, let you pick the model each fire runs
  under, keep their own run history with a live cron preview when creating one,
  and open each run in the full chat view from the Schedules page — model picker,
  context gauge, and a reply that continues the session. The page also has a
  one-click toggle to pause or enable every task at once. Fires no longer appear
  as sessions in the project sidebar.

- Desktop: opening a scheduled run's conversation now uses a wide, tall modal so
  the embedded chat — thread, model picker, and context gauge — has room to
  work, rather than the narrow default dialog.

- Desktop: terminal tabs now use a full-contrast color palette and follow the
  app's light/dark theme, so program output (git, ls, build logs) reads clearly.

- The kind reference cluster now runs Cilium instead of kindnet, so
  NetworkPolicy is enforced in the kernel and a long-lived local cluster no
  longer drifts into slow DNS and unprotected new pods. Existing clusters keep
  kindnet until recreated with `./make kind down` and `./make kind up`.

- A local CLI or Desktop session stays in the mode of its first turn. Resuming it
  after `buildmax login` or `buildmax logout` is refused rather than replaying
  its history to a destination it never used; start a new session instead.

- The workflow detail page is now organized into tabs — Overview, Definition,
  Runs, Schedules, and Revisions — matching the agent detail page, so a
  workflow's schedules have a tab of their own instead of sharing the page.

### Fixed

- When the Agent's browser fails to start, the error now includes the browser's
  own last output, and names a host that forbids Chrome's sandbox, instead of
  only "websocket url timeout reached".

- Desktop: destructive confirmations — deleting a project with sessions,
  clearing a project's sessions, and removing a Git-checkout plugin — now use an
  in-app dialog. They previously relied on the native `window.confirm`, which the
  webview can silently drop, leaving the action unconfirmable.

- Desktop: deleting a scheduled task now works. The confirmation moved from the
  native `window.confirm`, which the webview could silently drop, to an in-app
  dialog that spells out the deletion is irreversible, and the Delete button
  reads as a danger action.

- The managed gateway logs a provider's failure reason on the server, with the
  call's ledger ID, so an operator can tell a bad key from a provider outage.
  Callers still see only the stable error class. A client whose server is
  unreachable now says so instead of reporting the gateway "refused the call".

- `./make kind up` works again after MinIO withdrew its images: the kind
  deployment now pulls the MinIO server and `mc` from SILO, the community MinIO
  fork (`pgsty/minio`, `pgsty/mc`), pinned by tag and digest.

- A signed-in session whose BuildMax server fails a model call with HTTP 500
  now says the server failed the call, instead of that it may be down or
  restarting.

- `buildmax doctor` checks the mode first and no longer fails a signed-in setup
  for lacking `settings.yaml`, probes unused local models, or suggests
  `buildmax login` for an outage. `buildmax me` asks the deployment, so a
  revoked login is no longer shown as signed in. Signed-out hints now mention
  `buildmax login`, and an unknown model name says which deployment was asked.

- Managed mode: a deployment that is down or restarting is no longer reported as
  an ended login. The CLI says the login still works and offers retry or
  `buildmax logout`; Desktop keeps the workbench open under a banner that
  retries on its own, instead of a sign-in screen whose only way out discarded a
  working login. A login the server rejects stays in place until you sign in
  again or out, rather than being cleared so the next command silently ran in
  local mode, and a disabled account gets its own message.

- Managed mode: signed-in sessions now show what they cost. The deployment's
  model list carries its prices, so the session footer, `buildmax info`,
  `buildmax usage`, and Desktop `/info` no longer say "not priced", and the
  Portal **Usage** page totals your own CLI and Desktop calls on the deployment.

- Remote Control: a session watched from another device no longer goes dead
  after a minute of quiet. The server keeps the stream alive through silence and
  the Portal view reconnects on its own after a dropped connection, so an idle
  session, a tool-only turn, or a brief network blip no longer strands the
  watcher on a stale page.

- `buildmax tools status` now lists the Browser tools when a system browser is
  installed, matching what an interactive run actually gets, instead of omitting
  them.

- The workflow graph's running and succeeded nodes now use the shared status
  palette instead of an undefined accent variable that fell back to blue in
  light mode.

- Workflow runs no longer stall with every node stuck pending when the create
  request that started them is canceled or times out; the first dispatch runs on
  a context detached from the request and the reconcile lease is always released,
  so recovery picks the run up promptly instead of waiting out the lease TTL.

## [0.2.0-alpha.14] - 2026-09-20

### Added

- The deployment administration area has an LLM calls tab that searches the
  managed inference ledger across every space — model, tokens, cost, and status
  per call, filterable by user, model, status, surface, and time. It carries no
  prompts or generated content.

- `buildmax agent trigger <agent>` starts an agent run on the server from the
  command line, and `buildmax task status <id>` follows the task it created, so
  an agent working locally can trigger and observe another agent without a
  browser.

- `buildmax artifact publish <file>` uploads a local file to the server as an
  artifact and prints its id, with `--space`, `--title`, and `--share`, so an
  agent working locally can give a result a durable handle to reference in an
  issue comment.

- Portal chat can now list a space's published workflows, start a run of one,
  and check a run's status and result, alongside the existing task tools. It
  cannot create or edit workflows; authoring stays a reviewed publish action.

- The Portal Schedules page now creates a schedule directly, picking which agent
  runs it, so you no longer have to open an agent first.

- Desktop can schedule a prompt to run in a project on a cron expression from a
  new Schedules view; tasks fire in-process as new sessions while the app is open.

- Desktop workspace tabs can be dragged to reorder within a pane, and a
  right-click menu closes other tabs or tabs to the right and, on file and diff
  tabs, copies the file's relative or absolute path.

- Desktop chat and terminal tabs can be renamed from their context menu (a chat
  rename renames the session), file and diff tabs show the full path on hover,
  and a pane in a grid can be maximized — floated forward as an overlay over the
  dimmed grid — and restored.

- The Desktop centre is now a tab surface: chat, terminal, file, and diff tabs
  live side by side, tiled into a pane grid you can split and drag between, with
  the layout remembered per project. A left Explorer sidebar browses the
  workspace files and changes, file tabs can be edited and saved, and several
  chat sessions can run at once — each in its own tab.

- `buildmax issue comment <id>` posts a report on an issue from the command
  line — the body from `-m` or stdin — so an agent working locally can report
  through a command, and `buildmax --help` now groups commands into Server and
  Local sections.

- The Portal workflow editor gains a visual graph canvas — steps are nodes and
  dependencies are edges drawn between them — beside the raw JSON view, so a
  branching workflow no longer has to be written by hand.

- `buildmax workflow run <workflow>` starts a run of a published workflow from
  the command line, with `buildmax workflow list` to find one and
  `buildmax workflow status <run-id>` to follow it, so an agent working locally
  can trigger and observe a workflow without a browser.

### Changed

- An Agent's Issue comments are now bounded by the server, not just the runtime
  tool: a report over the per-Agent length limit is rejected, and a run may add
  only a fixed number of comments to its Issue before further ones are refused.

- Creating an Issue with a status, owner, or executor is now one request:
  `POST /api/spaces/{space_id}/issues` accepts `status`, `owner_id`,
  `executor_kind`, and `executor_id`, and a refused value creates nothing, so
  Portal no longer leaves a half-configured Issue behind a failed create.

- Portal now uses consistent primary actions on work collections, opens Issue
  details with the result and next step before editing, and shows Workflow run
  results before diagnostics. Task actions and conversation task cards use the
  shared button roles and readable run status; Chat tabs support arrow keys,
  and conversation Task failures provide local feedback and retry. Workflow
  editing and running use the same action roles and readable status labels.
  Agent detail and schedules now use those roles too, with keyboard tabs and
  local retry when triggered tasks fail to load.
  Artifacts now use the shared action roles, navigable names, truthful counts,
  and local preview retry.
  Administration, Space settings, Files, Marketplace, sign-in, and the rest of
  Issue detail follow the same roles, and buttons keep their label and width
  while an action is in flight.

- The Portal workflow editor is now flow-first: the graph canvas fills the page,
  the name and description collapse into a Settings drawer, the duplicate
  read-only topology diagram is gone, and a step's id can be renamed in the
  inspector to something meaningful — the rename updates every reference to it.

- The Portal workflow detail page replaces the status dropdown with explicit
  Save as draft, Publish, and Archive actions, moves version history to a header
  button, and no longer shows the workflow's opaque id.

- An agent working a space issue now reads and reports on it by running
  `buildmax issue show` and `buildmax issue comment` rather than through the
  built-in GetIssue and ReportToIssue tools, which have been removed; issue
  length and per-run comment limits are now enforced on the server for every
  client alike.

- Moved the Harbor evaluation target from Terminal-Bench 2.1 to Terminal-Bench
  4.0 (66 tasks) and repointed the pinned `--canary` subset at five cheap,
  Linux-only 4.0 tasks meant for a quick local regression check with a real
  model rather than a leaderboard score.

- The end-of-turn recap now costs far fewer tokens: it spends a model call only
  when the turn actually changed something and did not already describe it —
  read-only and self-explaining turns are skipped — and when it does run, the
  file bodies a write or edit carried are no longer sent to be summarised.

- The Portal Workflow detail page now follows the workflow's lifecycle: a
  draft opens in an editing layout whose primary action is Publish, while a
  published workflow leads with its read-only topology and recent runs, keeps
  Run as the primary action, and moves editing behind an Edit button. Run
  input is collected in a dialog, and version history is reached on demand
  from the header.

### Fixed

- Desktop: a terminal tab now keeps its full scrollback when you switch away
  and back, instead of collapsing to only the most recent command's output.

- Desktop terminal tabs keep their output and scrollback when switching to
  another tab and back, and when dragged between panes or re-tiled; the emulator
  is no longer torn down and recreated on the move.

- Desktop terminal tabs are no longer killed when switching projects; each
  project's tabs, including its running shells and their scrollback, are
  restored intact on returning to it.

- The Portal workflow and agent version history lists each revision's author by
  name rather than an opaque user id, and the workflow history dialog drops a
  redundant heading; the header button now reads simply "History".

## [0.2.0-alpha.13] - 2026-09-18

### Added

- A System Administrator can preview what disabling an account would stop with
  `GET /api/admin/users/{user_id}/deactivation-impact` — live sessions, webhook
  keys, memberships and roles, sole-owned Spaces, enabled schedules, active runs
  by status, and the cancellation bound, as counts and ids only, never Space
  content. Disabling an account (`PUT .../state`) now commits the account gate
  and then, in one orchestrated step, revokes sessions, pauses the account's
  schedules, cancels its in-flight runs, and — when `retire_webhook_keys` is set
  for a leaver rather than a suspension — permanently retires its webhook keys;
  the response reports the gate result alongside those cleanup counts.

- SSO account linking: a new `external_identity` table binds a BuildMax account
  to a verified `(issuer, subject)` at the IdP, and an association step resolves
  a verified sign-in to its account — reusing an existing link, linking an
  operator-created account by verified email, refusing a takeover, or creating an
  account just in time within `allowed_email_domains`. A System Administrator can
  list an account's identity links and, while the account is disabled, unlink one
  (`GET`/`DELETE /api/admin/users/{user_id}/identities`); linking and unlinking
  are recorded in the same transaction as the change. The browser sign-in flow
  that drives this lands in a following change.

- Corporate sign-in over OpenID Connect is now usable end to end (Okta the first
  supported provider): a "Sign in with <provider>" button on the Portal takes the
  browser through `GET /api/auth/oidc/start` and `…/callback`, which verifies the
  ID token, links or provisions the account, and opens the same session a
  password login would. Native password and login-code sign-in are gated
  independently by `local_login` (`all`, `system_admins`, `off`), so a deployment
  can run SSO only, both, or keep a break-glass path for operators. See
  [deploy/authentication.md](https://github.com/icloudbb/buildmax/blob/main/docs/deploy/authentication.md).
  The pinned real-Okta qualification and secret/key-rotation drills are still to
  come.

- Groundwork for corporate sign-in over OpenID Connect: a server `oidc` block
  (issuer, client, `provisioning`, `allowed_email_domains`, `session_max_age`),
  a `local_login` knob (`all`, `system_admins`, `off`) that gates native
  password and login-code sign-in independently of SSO, and a new unauthenticated
  `GET /api/auth/methods` that reports the enabled sign-in methods. The client
  secret is injected with `BUILDMAX_OIDC_CLIENT_SECRET` and never served; the
  admin system view reports the provider's live health. The browser login flow
  itself lands in a following change.

- Disabling an account in the Portal admin area now opens a guided impact
  preview first: it shows what the disable would stop — live sessions, webhook
  keys, memberships, enabled schedules, and active runs — warns when a shared
  space would be left with no enabled owner, states how long already-running
  work may take to stop, and lets the operator choose a temporary suspension or a
  leaver whose webhook keys are retired. Confirming reports what the cleanup did.
  The admin Spaces view gains a "Make owner" action that recovers a shared space
  whose owners are all disabled by promoting an enabled member.

- A System Administrator can recover a shared Space whose recorded owners are
  all disabled by promoting an enabled member to owner, with
  `PUT /api/admin/spaces/{space_id}/owner` or, for break glass when the Server
  or IdP is unavailable, `buildmax-server space recover-owner <space_id>
  <successor_email>`. It refuses a personal Space, a Space whose owner can still
  sign in, and a successor who is not already an enabled member; it creates no
  membership and grants the operator no access to the Space's contents; and it
  records a `space.ownership_recovered` audit event naming the disabled owner,
  the successor, and the actor.

- A workflow run now shows a read-only graph of its nodes laid out left to
  right by dependency, with each node colored by status and linking to its Task.
  It reads the run's node records, so the picture matches exactly what ran.

- A Workflow run now takes an immutable input validated against the workflow's
  `input_schema` at admission and frozen onto the run; the Portal generates a run
  input form from that schema, and a workflow without one runs with no input as
  before.

- A workflow definition may declare a `result` selector (a `source` and RFC 6901
  `pointer` into a step's output, like an input binding); a succeeding run resolves
  it once and stores it as the run's authoritative result, surfaced on the run
  detail and on the issue the run belongs to.

- A Workflow `agent_task` step can declare an `output_schema` (a JSON Schema in
  the supported subset): the step's run is constrained to return a machine-readable
  answer matching it, the validated value is persisted on the run and step, and
  the step succeeds only when the answer validates — otherwise it fails.

### Changed

- The Compose bundle now serves the Portal and the API through one gateway on a
  single origin (as the Kubernetes deployment already does), so the session
  cookie's same-origin check is satisfied and sign-in works out of the box; open
  the same published port as before.

- Server sessions are now durable and checked on every request: logout,
  administrator revocation, and account disablement stop an already-issued
  access token within the access-token window instead of at its expiry. Access
  tokens default to 15 minutes and sessions have a 90-day absolute lifetime
  (`access_token_ttl`, `session_absolute_ttl`). Existing sessions must sign in
  again after the upgrade.

- Unattended Agent work now stops when its initiator loses authority — the
  account is disabled or removed from the run's Space. A run that has not started
  no longer starts a worker and instead reaches `CANCELED` (not `FAILED`) with a
  `cancel_reason` of `creator_disabled` or `creator_not_member`; a run already
  under way is asked to stop by a background reconciler and by the worker's own
  re-check when it fetches its run. A schedule whose creator lost authority
  pauses with a matching `pause_reason`. A run's managed-inference token is now
  attributed to the run's own initiator rather than the original Task creator, so
  a Continue by a colleague runs under that colleague.

- The Portal now keeps its refresh credential in a Secure, HttpOnly,
  SameSite=Strict cookie the browser manages instead of in `localStorage`, and
  holds only a short-lived access token in memory. New `/api/auth/portal/{login,
  session,logout}` routes deliver it; native CLI and Desktop clients keep using
  the JSON `/api/auth/*` routes. Serve the Portal and API from one origin (a
  reverse proxy in production; the dev server proxies `/api` automatically).

- Publishing a workflow now pins each node's Agent to a specific revision:
  a node that names no `agent.revision` is pinned to the Agent's current
  revision, and the pinned number is stored in the published definition. A run
  started later snapshots that revision's content, so editing an Agent no longer
  changes what an already-published plan runs. Publication rejects a node that
  pins a revision the Agent never had.

- A workflow run now dispatches every ready node at once instead of one at a
  time, so independent branches of the graph execute in parallel. A definition
  may cap the parallelism with `policy.max_parallel_nodes` (1 to the deployment
  maximum); absent, a run uses the deployment ceiling. Failure stays fail-fast:
  one node's failure now also cancels the siblings that were running alongside
  it and ends the run.

- A Workflow definition must now declare `"schema_version": 1`, and may declare
  an `input_schema` and a `result` selector; publication rejects an unknown
  version, an input schema outside the supported subset, or a result naming a
  step that does not exist.

- A workflow node now names its Agent under `agent` (`{"id": …}`) and its task
  under `input` (`{"instruction": …, "bindings": […]}`) instead of the flat
  `target_agent_id`, `prompt`, and `bindings` fields, and it declares an
  `issue_access` mode. `none` (the default) gives the node's Task no Issue
  relation; `if_bound` attaches the run's Issue when it has one; `required`
  additionally refuses to start a run that has no Issue. This makes each node's
  Issue capability an explicit choice and replaces the earlier flat node shape;
  existing definitions must be re-authored.

- A workflow run's per-step records are now node runs: the API and Portal
  expose `node_id`, `node_index`, and `node_type`, each run records the full
  resolved input its node received and its complete output (replacing the
  truncated output summary), and the run detail shows both.

- Workflow step input bindings now select a value from a source (`workflow.input`
  or an earlier step's `node.<id>.output` envelope of text, structured output, and
  Artifact references) at an RFC 6901 JSON Pointer, instead of injecting an
  earlier step's whole output; the step editor authors a source and pointer per
  input.

- A workflow definition now describes a graph of `nodes` joined by `needs`
  edges instead of an ordered `steps` array: a node becomes ready when every
  node it needs has succeeded, so dependencies — not list position — decide the
  order. Publication rejects a graph that is not acyclic, a `needs` edge to a
  missing node, or an input binding that reads a node which is not one of its
  predecessors. Execution stays fail-fast (one node's failure blocks the rest)
  and dispatches one ready node at a time for now. This replaces the earlier
  `steps`/`step_id` shape; existing definitions must be re-authored as
  `nodes`/`id`.

### Fixed

- Fixed cross-origin sign-in: the server's CORS responses now set
  `Access-Control-Allow-Credentials: true`, so a deployment that serves the
  Portal and the API on different origins (such as the Compose bundle) can send
  the session cookie and complete login, which the browser had been blocking.

- Desktop tool-call cards no longer break the tool name mid-word at narrow
  window widths, keeping short transcripts scannable.

- A large dialog whose content is taller than the window — such as the New
  Workflow form with a step and its input bindings — now caps its height and
  scrolls its body, so the footer buttons (for example Create workflow) stay
  reachable instead of overflowing off the bottom of the screen.

- A system administrator's grant can no longer be revoked at the same moment
  another administrator's account is disabled in a way that left the deployment
  with no effective administrator; the last-holder guard now serializes the two
  correctly.

- Fixed the Portal showing a whole-page error, instead of the app shell with a
  "Space unavailable" label, when the Space list failed to load on a reload — the
  session restore's brief unauthenticated window had cleared the remembered Space
  before the list was even fetched.

## [0.2.0-alpha.12] - 2026-09-13

### Added

- Creating, disabling, or destroying a space Secret now writes a
  `secret.created`, `secret.disabled`, or `secret.destroyed` event to the space
  audit trail, with the Secret's id as the target and no item name, value, or
  ciphertext in the event.

### Changed

- Admin stored-flag transitions are now a single idempotent `PUT .../state`
  instead of paired POST actions: `PUT /api/admin/users/{user_id}/state`
  (`disabled`), `PUT /api/admin/llm/models/{model_id}/state` (`enabled`),
  `PUT /api/admin/plugins/{plugin_name}/state` (`archived`), and
  `PUT /api/admin/plugins/{plugin_name}/releases/{version}/state` (`yanked`).
  The old `/disable`, `/enable`, `/archive`, `/unarchive`, and `/yank` routes no
  longer exist; clients that ship with the server were updated in step.

- Authentication routes moved under a common `/api/auth/` prefix:
  `/api/auth/otp`, `/api/auth/login`, `/api/auth/logout`, `/api/auth/password`,
  and `/api/auth/token/refresh`. The old top-level paths (`/api/login`,
  `/api/otp/request`, and the rest) no longer exist; clients that ship with the
  server were updated in step.

- The OpenAPI specification is split along the listener boundary: the public
  `/openapi.json` no longer documents the worker control plane, which now lives
  in its own `openapi-worker.json`. This matches the two-listener network
  boundary, where the public socket cannot dispatch a worker route.

### Fixed

- `buildmax -r <value>` with a malformed (non-UUID) session id now reports an
  "invalid resume id" usage error instead of the "session not found" a
  well-formed but unknown id gets, matching the `--session-id` validation.

- `buildmax --session-id <uuid>` now creates the session when it does not exist,
  matching the flag's documented "load if exists, else create" contract, so a
  caller can start a run under a deterministic id. `-r/--resume` still errors on
  an unknown id.

- The OpenAPI documents now define the `Artifact` schema every artifact route's
  response referenced, so `/openapi.json` (and the worker document) no longer
  carry a dangling `$ref` for the file a create or read returns.

- The served OpenAPI document's `info.version` now reflects the running build
  instead of a stale hand-maintained literal, so `/openapi.json` and Swagger UI
  report the deployment's actual version.

- Portal now shows "1 member" (not "1 members") for a one-member space, exposes
  the agent-detail section tabs with the standard `tab` role, and gives the
  new-secret item name and value inputs real accessible labels.

- The Portal dashboard no longer issues `GET /api/spaces/null/conversations` on
  first load, removing the 403 console error emitted on every sign-in before the
  current space resolves.

## [0.2.0-alpha.11] - 2026-09-13

### Added

- Each release now attaches downloadable desktop builds — a macOS `.dmg` and a
  Windows `.exe`, each with a `.sha256`. The bundles are unsigned during alpha,
  so the install guide covers the one-time Gatekeeper and SmartScreen step.

- The space audit trail now records space creation (with its quota tier),
  webhook key creation and revocation, agent definition create/update/delete,
  and workflow create/publish/archive/unpublish; and every event carries the
  task run it was recorded on behalf of, so an investigation can pivot between
  an audit event and the run that caused it.

- A server can now expire old run traces: `server.yaml` `trace.retention_days`
  (0, the default, keeps every trace forever) runs an hourly sweep that removes
  the trace of a run that ended longer ago than the window and records a
  `traces.pruned` audit event, so a trace missing by policy is distinguishable
  from one that was lost.

- Portal Run Details now shows how a worker run's MCP transports were treated —
  that stdio is disabled by the unattended-worker profile and which remote
  transports the run resolved — beside the sandbox boundary, recorded in the
  run's trace. An older trace reads as unknown rather than as stdio-allowed.

- The Workflow step form can now add, name, and remove a step's input bindings
  to an earlier step's output, so passing one Agent's result to the next no
  longer requires hand-editing the definition JSON.

- A Workflow step can now take an earlier step's output as input. A step
  declares `bindings` naming a prior step, and the run feeds that step's full
  output to the downstream Agent as labelled, untrusted context — so a
  multi-step Workflow can pass one Agent's result to the next.

### Changed

- The Portal's mutable Space file surface is now named **Files** everywhere
  (sidebar, breadcrumb, page title, and contextual links), renamed from
  "Workspace Files"; its route is unchanged.

### Fixed

- On narrow screens, the Portal's compact header no longer shows a fabricated
  "My Space" when the current Space cannot be resolved; it now shows the same
  loading/unavailable state label the sidebar uses.

- A task that fits within a Space's run limit is no longer refused for the
  tokens its auto-generated title spent, so starting one no longer leaves an
  empty conversation behind on a token-quota refusal.

- Fixed the Portal task view briefly showing a previous turn's reply before the
  new run's output arrived: the live output stream is now scoped per run, so a
  finished run's buffered text is never replayed to the next turn's watchers.

- Editing, publishing, or restoring a Workflow while someone else edits it now
  returns a conflict to the later save instead of silently overwriting the newer
  definition or failing with an internal error.

- A Workflow run no longer stalls when a step finishes but its completion
  signal is lost or the server restarts: a background recovery loop reconciles
  due runs from stored state, advancing them without the callback.

- A Workflow run now completes when its step finishes: the server's terminal
  callback reads the finished step's result and advances the run, instead of
  leaving every run stranded in "running".

### Security

- Unattended worker runs now reject stdio MCP servers before any child process
  or model call, since a worker would launch them outside its sandbox; configure
  a remote (`http` or `sse`) transport instead. CLI, Desktop, and evaluation runs
  keep stdio.

## [0.2.0-alpha.10] - 2026-09-12

### Added

- A Space can run an Agent on a recurring cron schedule: create, list, edit,
  enable/disable, and delete schedules under
  `/api/spaces/{space_id}/schedules`, and each due time starts one Task
  automatically.

- `buildmax info --json` now reports a per-turn usage and cost breakdown under
  `stats.turns`, recorded in the session file so a session describes what each
  turn cost from its own record.

- `buildmax usage` sums token and cost totals across your local sessions,
  grouped by day, workspace, or model and narrowed with `--since`, so a week of
  use answers as one figure instead of one session at a time.

### Changed

- The project moved to the `icloudbb` GitHub organization. The Go module path
  is now `github.com/icloudbb/buildmax`, container images publish to
  `ghcr.io/icloudbb/buildmax` and `ghcr.io/icloudbb/buildmax-portal`, and the
  old `gougoujiang` GitHub and GHCR locations remain read-only for one Alpha
  cycle. Update imports, `go install` targets, and image references.

### Fixed

- The `Read` tool now reports `(file is empty)` after successfully reading a
  zero-byte file instead of returning indistinguishable empty output.

- Schedules now accept any IANA timezone (for example `Asia/Shanghai`), not only
  `UTC`: the server binary embeds the timezone database, so a named zone resolves
  in container deployments that ship no system zoneinfo.

- On a multi-replica Server, a conversation turn whose coordination lease
  expired and was taken over can no longer write behind the new holder: message
  writes now carry the lease's fencing token and a stale one is rejected.

- Write now reports the resolved file path and number of UTF-8 bytes written on
  success, making its result directly verifiable by the agent.

## [0.2.0-alpha.9] - 2026-09-10

### Added

- The admin account list can be filtered by whether an account is enabled,
  whether it has set a password, whether it holds a system role, and the
  platform it last signed in from — in Portal and on `GET /api/admin/users`
  (`status`, `has_password`, `system_role`, `platform`) — so an operator can
  work a specific set without paging through everyone.

- `buildmax admin` gained `model list`, `model add`, `model enable`, and `model
  disable`: managing the deployment's model catalog over the authenticated Admin
  API, the automation peer of the Portal Models area. `model add` sends the
  provider key in the request body only; it is stored encrypted and never read
  back, and a deployment with no encryption key configured refuses a model that
  carries one.

- `buildmax admin` gained `user list`, `user create`, `user login-code`, `user
  disable`, and `user enable`: managing deployment accounts over the
  authenticated Admin API, the automation peer of the Portal Accounts area.
  Creating an account and issuing a login code stay separate steps, and there is
  no `set-password` — a login code lets the person choose their own password.

- Added `POST /api/admin/llm/models`, so a System Administrator can add a
  managed model through the admin API rather than only `buildmax-server model
  add`. It takes the same fields, and `api_key` is write-only: accepted in the
  request body, stored encrypted at rest, and returned by no read. Adding a
  model that carries a credential requires a configured encryption key.

- A System Administrator can list an account's live login sessions and revoke
  one of them — signing a single device out while the account's other sessions
  keep working — through `GET` and `DELETE /api/admin/users/{user_id}/sessions/
  {session_id}`. The listing carries only safe metadata (session id, platform,
  and timestamps), never a token.

- `buildmax admin list`, `buildmax admin grant <email>`, and `buildmax admin
  revoke <email>` manage deployment administrators from the signed-in CLI over
  the same API the Portal uses, so routine administration no longer needs shell
  access to the server's database (which `buildmax-server admin` still holds for
  first-time and lockout recovery).

- A background sweep reclaims checkpoint payloads that no checkpoint references
  and that are older than a grace period — the bytes a worker uploaded when a
  finalize failed or a worker died before committing the pointer — so orphaned
  workspace-checkpoint objects no longer accumulate. The grace is set with
  `storage.checkpoint_orphan_grace_days` (0, the default, reclaims on the next
  hourly sweep).

- The in-Portal help center now offers a Simplified Chinese translation of the
  whole manual, with an EN / 中文 switch in the Help sidebar; the choice is
  remembered per browser and defaults to the browser's language.

- A `coordination` server setting shares live streaming, connection events, and
  conversation turn serialization across replicas through Redis, so a deployment
  can run more than one server replica correctly; `mode: local` (a single
  replica) stays the default, and `mode: redis` fails closed when Redis is
  unreachable.

- The Portal Accounts list now pages through every account with Previous and
  Next controls and a "1–50 of N" position, so a deployment with more than one
  page of accounts is fully reachable rather than stopping at the first fifty.

- Portal gains an Administrators section for managing who can operate the
  deployment — list active grants (and revoked history), grant an account by
  email, and revoke — and, for a confirmed administrator, Administration moves to
  a first-level sidebar destination. The Overview now shows the caller's own
  grant and a read-only view of the effective configuration.

- The Portal has a built-in help center: a **Help** icon in the top bar (and the
  user menu) opens an end-user manual covering getting started, the CLI and TUI,
  models and tools, extending the agent, safety, and the Portal itself. The pages
  are plain Markdown under the repository-root `help/` directory and are baked
  into the portal image at build time, so the manual ships with the app and needs
  no separate site.

- The Portal admin Models area now has an "Add a model" form, so a System
  Administrator can add a managed model without the server command line. The API
  key is a password field, sent only in the request body, stored encrypted, and
  never shown again; it is cleared as soon as the model is added. A deployment
  with no encryption key configured reports that it cannot accept a credential.

- Portal now shows a real not-found page for an unrecognized address instead
  of silently opening Chat, and Agent, Workflow, Workflow Run, Task, and
  Issue detail pages distinguish a deleted resource from one you don't have
  access to.

- A run's detail now shows the plugin releases it actually resolved and the
  artifacts it published, both read from the same authoritative records a
  retry and an issue's output list already use.

- The Portal "Run details" view now shows what became of a run's workspace: a
  Workspace section reports whether the run restored its base checkpoint and
  whether its result checkpoint committed, with the bounded reason on a failure,
  so an operator can see a run's continuity state without reading the database.

- The Portal account detail now lists an account's live login sessions —
  platform and timestamps — and an operator can revoke one of them, signing a
  single device out while the account's other sessions keep working, alongside
  the existing revoke-everything action.

- Added a complete Simplified Chinese mirror of the design records, with
  per-page language navigation and automatic synchronization checks.

- A TaskRun's durable trace now records who or what started it and why
  (created_by, created_by_type, trigger_source, retry_of_task_run_id) on its
  run_start line, so a downloaded trace explains its own origin.

### Changed

- Portal task runs now execute in a single `workspace/` directory — the agent's
  working directory, holding the space's files — instead of splitting them into
  separate read and output directories. Files an agent means to keep are
  published with `UploadArtifact`; the run's reply is still recorded as its
  result.

- The end-user manual now lives only in `help/`, the same pages the Portal
  serves under **Help**; the duplicate copies under `docs/` (the old `guide/` and
  `start/` directories and the CLI reference) are gone, so `docs/` is now
  contributor, operator, and design material.

- Issue owner and executor are now independent fields instead of one combined
  assignee, so an issue can have an accountable person and a selected Agent or
  Workflow at the same time. `owner_id` replaces the person case of the old
  `assignee_kind`/`assignee_id` pair, and `executor_kind`/`executor_id`
  replace the agent and workflow cases.

- Working a space issue locally moved from the `buildmax --issue <id>` flag to
  the `buildmax issue start <id>` subcommand, alongside `issue list`, `show`, and
  `status`. It takes the same run flags as `buildmax` itself (`-p`, `--model`,
  `--workspace`, and so on).

- Expand `./make kind fixtures` with shared-space roles, invitations, assigned and nested issues, workflow states, files, artifacts, synthetic secrets, and pagination data. Add `--runs` for free-mock conversation and Task/Workflow history, and repair incomplete comment seeding without duplicating existing fixtures.

- Managed-model provider credentials are now encrypted at rest, under the same
  deployment key-encryption boundary that protects Space Secrets. Adding a model
  that carries a credential (via `buildmax-server model add`) now requires a
  configured encryption key (`secret.kek_file`); without one the credential is
  refused rather than stored in the clear. Credential-free models (for example
  an Ollama target) are unaffected. Existing plaintext credentials are not
  migrated — re-add those models once an encryption key is configured.

- Opening an account in the Portal admin area now puts it in the URL
  (`#/admin/accounts/<id>`), so the detail panel survives a reload and can be
  linked or shared rather than vanishing when the page is refreshed.

- The Portal admin Accounts area can now filter accounts by last-login date
  range ("signed in after" / "signed in before"), alongside the existing
  status, password-state, role, and platform filters. Accounts that never
  signed in are excluded by either bound.

- Chat's Files tab, an Issue's Discussion panel, an Artifact's origin, and the
  Marketplace plugin detail now link to Workspace Files, Space Plugins, or the
  producing task instead of duplicating those surfaces or leaving a dead end;
  Issue results and a run's plugin picker link out the same way.

- Workspace Files now explains it holds mutable working state, distinct from
  Artifacts' immutable published output, and the empty root folder points to
  uploading or having an agent write there instead of just saying "(empty)".

- Issue Detail is now organized into Overview, Discussion, Results, and Runs
  tabs instead of ten stacked sections, and the assignee line reads "Owner" or
  "Executor" depending on who or what it points to.

- Creating an account in the Portal admin area now lands the operator on the new
  account's detail, where the login code is issued, with a note that the account
  cannot sign in until a code is issued. Create and issue-a-code remain separate
  actions with separate audit events; the Portal only guides the operator from
  one to the next.

- Portal's browser tab title now names the current page and, for Space-scoped
  pages, the Space, and the narrow compact header shows the same Space and
  page cues as the desktop sidebar. Old flat addresses like `#/agents` or
  `#/issue/<id>` are no longer recognized; every Space-owned link is now
  `#/spaces/<space_id>/...`.

- Removed the duplicate plugin catalog from Account settings. Browse published
  plugins in Marketplace instead; the old `#/account/plugins` link redirects
  there.

- Portal's narrow-width shell (roughly a phone-sized window) now shows a
  compact header with the current Space, page title, and a menu button that
  opens the full navigation in an accessible overlay drawer, instead of
  squeezing the sidebar into the page. Dialogs across Portal — including
  side-tab forms, which now use a horizontal, arrow-key-navigable tab strip at
  narrow widths — trap keyboard focus, restore it to the control that opened
  them, and become full-height sheets on narrow screens. Chat, Issues, Issue
  Detail, and Task Detail also reflow at narrow widths: the thread and
  composer no longer lose most of their width to fixed side margins, a task's
  header actions wrap under its title instead of clipping it, and primary
  action buttons meet a 44px minimum touch target. Workspace Files now shows
  either the current folder's contents or the selected file at narrow widths,
  with a Back action, instead of squeezing a fixed-width folder tree beside
  unreadably narrow content; Artifacts, admin lists (Administrators, Accounts,
  Spaces, Models, Plugins), and Space membership rows reflow to a stacked
  layout instead of wrapping into an ambiguous multi-item row; and a run's
  tool paths and token counts scroll horizontally in their own row instead of
  being cut off with an ellipsis.

- Each session in the Portal account detail now shows its session id and when it
  was last active alongside its platform and sign-in and expiry times, so an
  operator can tell two sessions on the same platform apart before revoking one.

- Portal's sidebar now groups Space navigation into Work, Reuse, Data, and
  Manage, renames Home to Chat, adds a Workspace Files entry, and separates
  deployment Administration from the Space-scoped groups.

- Portal's Space-owned pages (Chat, Issues, Agents, Workflows, Workflow Runs,
  Tasks, Workspace Files, Artifacts, Space settings) now use canonical
  `#/spaces/{space_id}/...` URLs, so a copied or reloaded link always reopens
  the same Space. Old links keep working during the migration.

- The Workflow editor now presents each step as an Agent step instead of a
  free-form type and editable id, and raw definition JSON moved behind an
  explicit "Advanced" toggle instead of always showing beside the form.

- Removed the per-run "output files" list. A task run's reply is still recorded
  and shown as its result; files a run means to keep are published with
  `UploadArtifact` and appear as the space's artifacts, and a Task's working
  files are recovered through its workspace checkpoint rather than downloaded
  file by file.

- The end-user manual now lives under `manual/`, while Portal continues to
  serve it from its existing **Help** route.

- `buildmax whoami` is now `buildmax me`.

- `buildmax-server` sheds two routine operations now that `buildmax admin`
  covers them: `user set-password` is removed (issue a login code and let the
  person choose their own password — the safer equivalent), and `admin list` is
  removed (use `buildmax admin list`, or the Portal). Creating accounts and
  issuing login codes, and granting or revoking administrators, stay on
  `buildmax-server` as the break-glass path.

- Kubernetes worker pods now carry an ephemeral-storage request and limit, and
  each of their scratch volumes is capped at that limit, so a runaway workspace
  is evicted cleanly instead of filling the node. A `k8s_job` deployment must add
  `ephemeral_storage_request` and `ephemeral_storage_limit` under
  `worker.k8s.resources`, which are now required alongside the CPU and memory
  bounds.

### Fixed

- `buildmax init` now rejects a negative `--context-window` before writing
  `settings.yaml`; zero continues to select the provider-appropriate default.

- A signed-in CLI whose deployment rejects the stored credential with 401 (the
  session was revoked, or the server no longer trusts the token) now reports the
  login as expired and names `buildmax logout` to return to local mode, instead
  of failing with a bare `list the models ... : server 401: unauthorized`.

- Portal's Issue, Workflow, and Workflow Run breadcrumbs now show the loaded
  title or name instead of the raw public ID.

- Every Portal collection — Issues, Workflows, Agents, Conversations, Files,
  the Space members, invitations, secrets, and audit trail, and the plugin
  catalog on Marketplace, the Space Plugins tab, and the admin model catalog —
  now distinguishes a request that failed from a genuinely empty list, offering
  Retry instead of a calm "nothing here" screen; the Agents list in particular
  no longer renders a load failure as "No agents yet". A failed refresh of an
  already-loaded list keeps its data on screen with a warning rather than
  wiping it.

- Issue Detail now gives Save and Run their own error and success feedback,
  explains why a Run button is disabled, and Run Agent opens the task it
  started instead of leaving you on the form.

- Failed row-level actions in Portal now show their error next to the specific
  control that failed instead of a page-level banner that did not say which one
  it was about: Space plugin activate/update/suspend, Space membership actions
  (invite, remove, role change, ownership transfer, login code, revoke),
  revision Restore on Workflow and Agent detail, Artifact Download and Delete,
  and admin model retire/enable. Creating an Issue or Workflow now opens the
  created object rather than returning to the list.

- Portal's owner/admin-only controls (on Issues, Workflows, Agents, Space
  secrets, and the Space audit trail) no longer treat a role lookup that is
  still loading, or one that failed outright, the same as a confirmed denial:
  each now says which of the three it is, and a failed lookup can be retried by
  refreshing instead of silently staying read-only.

- Portal no longer labels the current Space "My Space" while Space resolution
  is still loading or has failed — that name is shown only for an actually
  resolved personal Space — and the shell's pre-Space gate now distinguishes a
  failed Space lookup (an error with Retry) from an account that genuinely
  belongs to no Space yet (a create-Space prompt), instead of one "No space
  available" catch-all.

- Switching Space in Portal's sidebar now redirects away from every
  Space-owned page, including Agent and Task detail, which previously kept
  showing the Space you switched away from.

- Task breadcrumbs now navigate to the Issue, Conversation, or Workflow run
  that started the task, and each run's input is credited to who or what
  actually triggered it instead of always showing "You".

- Fixed the Space switcher's label losing its association with the dropdown
  for an account in 2 or more Spaces at narrow widths: the persistent sidebar
  and the narrow navigation drawer could both render the same hardcoded id at
  once, and only one `<label>` can own it.

- Deployment administrator authority is now safe under concurrency: grants can
  no longer be duplicated by a race; two administrators can no longer revoke or
  disable at the same time and leave the deployment with no one able to reach its
  admin area; a disabled account no longer counts as a holder; and granting a
  role to a disabled account is refused instead of stored as unusable authority.

### Security

- Request logs now redact credential-bearing query parameters, so a WebSocket
  upgrade's `?token=` JWT and similar secrets no longer appear verbatim in logs
  or in artifacts that capture them.

## [0.2.0-alpha.8] - 2026-09-06

### Added

- The Portal agent dialog now has a Plugins group for choosing which catalog
  plugins an agent loads on its background runs, offering only the plugins the
  space can name and keeping any already-named plugin visible even if it is no
  longer available.

- The Portal artifact page now renders Markdown artifacts as formatted text and
  shows HTML artifacts as a live page in a sandboxed frame, instead of offering
  only a download for them.

- Artifacts can now be given a revocable public link that opens without a
  BuildMax login and renders in the Portal (Markdown formatted, HTML in a
  sandbox); `UploadArtifact(share=true)` returns one, and the server builds it
  from the new `public_base_url` / `BUILDMAX_PUBLIC_BASE_URL` setting.

- Desktop's file browser and changes panel now syntax-highlight file and diff
  content, the file browser adds a Source/Preview toggle for Markdown files,
  and the changes panel adds a List/Tree toggle that groups changed files by
  directory.

- Desktop now has a docked inspector column on the right for browsing workspace
  files, reviewing changes, and reading session info, replacing the slide-over
  drawers: switch views from the header, drag to resize it, expand it to fill
  the chat area for review, and collapse the left sidebar to give the
  conversation the full width.

- Add `./make e2e desktop-ui`, which drives desktop/frontend's React app and
  its bound Go methods through `wails dev`'s browser dev server against a
  fresh, discarded `BUILDMAX_HOME`; `./make run desktop-dev` starts that same
  dev server for ad hoc use, and `.buildmax/skills/drive-desktop/` is a
  Playwright REPL for poking at it by hand.

- `./make kind fixtures` seeds a running local Kubernetes deployment with an
  idempotent set of business data — two accounts with personal spaces, an agent,
  a workflow, and issues across every status — so automated Portal testing can
  start from populated views instead of an empty deployment.

- A running deployment can now flip its own conversations and task runs
  between a seeded catalog model and the free mock by environment alone —
  `BUILDMAX_WORKER_LLM_TRANSPORT`, `BUILDMAX_LLM_DEFAULT_MODEL`, and
  `BUILDMAX_CONVERSATION_MODEL_TARGET` override the matching `server.yaml`
  fields, `conversation.model_target` accepts a model name as well as an ID, and
  `./make kind use-model <name>` / `./make kind mock` switch a kind cluster.

- Portal agents can now choose which model their background runs call, so
  different agents can run on different models. The agent editor offers the
  deployment's catalog models plus a "Deployment default"; an unknown model is
  refused on save, and the choice takes effect on the managed worker transport.

- Portal now has a global Marketplace page, reached from a storefront button in
  the header beside the theme toggle, that lists the plugins this deployment
  publishes and how to install them.

- Space owners and admins can set shared Agent instructions that every
  background run inherits before its selected Agent's own instructions.

- The local kind deployment now runs the worker control channel over HTTPS with
  a generated certificate, and `./make kind` verifies the worker API boundary in
  the same smoke: a labelled worker pod reaches the internal listener, an
  unlabelled pod is denied it by the NetworkPolicy, and `/api/worker` answers
  `404` on the public Service.

- The reference Kubernetes manifests now separate the worker control API from
  the public API: a `buildmax-api` Service behind the Ingress, an internal
  `buildmax-worker-api` ClusterIP on port 5679, a `NetworkPolicy` that admits
  only labelled worker pods to that port, and the worker-api CA mounted
  read-only into each worker Job.

- The `/worktree` panel now marks each tree as clean or with its uncommitted and
  unmerged counts, and can remove a stale one in place with `d` — a confirm that
  names what would be discarded before a tree holding work is deleted.

### Changed

- The agent Configuration tab now uses a left sidebar of sections (Basics,
  Sandbox access, Plugins, Secrets) showing one section at a time, instead of a
  tall stack of cards, so every configuration group is visible at a glance.

- The Portal create agent dialog now organises configuration into tabs down a
  left sidebar (Basics, Sandbox access, Plugins, Secrets), so the dialog's
  height stays bounded instead of growing into one long scroll.

- The Portal Agents section now opens each agent on its own page — Overview,
  Configuration, Runs (execution history), and Revisions — instead of an edit
  dialog, and the Agents home adds an overview with space-wide run counts, a
  success rate, and a recent-activity feed across agents.

- The chat composer's Send and Stop buttons are now compact icons, in both
  Desktop and Portal; the action's word stays as the button's accessible label.

- Desktop shows context-window usage as a donut gauge after the git branch in
  the status bar, filling amber then red as the window fills; clicking it opens
  the exact used, free, and window token counts.

- Desktop now triggers panels through TUI-style slash commands typed in the
  chat input — `/info`, `/diff`, `/mcp`, `/tools`, `/worktree`, `/compact`, and
  the rest — replacing the row of buttons below the composer. `/agents` and
  `/plugins` are now slash commands in the terminal UI too.

- Portal no longer shows internal entity IDs on the artifacts list, artifact
  detail, and agent detail pages, leaving only the information a user acts on.

- `./make kind reload` replaces `./make kind images`: it still builds and loads
  the local images, and now also restarts the `buildmax-server` and
  `buildmax-portal` deployments so a code change takes effect without a full
  `./make kind up`.

- The local `kind` stack now generates an ephemeral Space Secret key-encryption
  key and mounts it, so the Secrets feature can be exercised end to end there
  instead of answering "secrets not configured"; the deployment baseline mounts
  the key from an optional Secret so other deployments are unaffected.

- The ownership and authorization boundary is renamed from **Team** to
  **Space** across the product: API routes move from `/api/teams/{team_id}`
  to `/api/spaces/{space_id}`, the `team_id` JSON field becomes `space_id`,
  and Portal, the CLI, and stored data use Space throughout. This is a
  breaking API change with no compatibility shim, as the Alpha allows.

- A Space's Secrets page was redesigned into per-secret cards with an
  at-a-glance state, item-name chips, a clearer create form, and a security
  caution restyled from an alarm into a readable note.

- A Space's sandbox defaults moved out of the Plugins tab into their own
  Security tab, and the Plugins tab was redesigned into scannable per-plugin
  cards with an at-a-glance activation status.

- The Task page now streams the in-flight run's output live over server-sent
  events instead of only polling: tokens appear as the agent produces them,
  and the poll continues to own run lifecycle and status so a dropped or
  draining stream falls back cleanly.

- The Portal task page leads with the conversation; a Details button in the
  header (beside Open agent) opens a dialog with the task's agent, timing,
  origin, and trace/files entry points, instead of foregrounding backend run
  numbers, repeating controls under every message, or crowding the transcript.

- While an agent run is in flight, the Portal task conversation now shows an
  animated working indicator instead of the raw "Run pending / scheduled /
  running" status text, which the reader does not need to see.

- Trim the status footer to just the context share on both the CLI TUI and the
  Desktop status bar; the CLI's per-run token and cache breakdowns stay
  available under `/info`.

- The worker control API (`/api/worker/*`) is now served on a separate internal
  listener, off the public HTTP surface. It binds `127.0.0.1:5679` by default,
  so set `worker.server_url` to that listener (via `worker_api.listen`) rather
  than the public port; the public listener answers `404` for worker routes.

### Fixed

- Fixed artifact uploads failing with an internal error on deployments whose
  artifact storage is an S3-compatible store reached over plain HTTP, such as
  the bundled MinIO: the streamed upload is now sent through the S3 transfer
  manager instead of a single request the SDK cannot sign.

- Desktop: the model picker now marks the model you switched to as active when
  reopened, instead of always checking the first entry.

- Desktop now sends prompts with the model you switch to: switching a
  conversation's model records it on that conversation, so its next turn uses
  it instead of falling back to the default.

- Fixed a regression on deployments using the local-filesystem artifact backend
  (including the default Docker Compose stack) where a finished task run's stored
  result was truncated to empty and its artifact content came back blank.

- Portal buttons written against the shared `btn` class (Secrets, webhook keys,
  several modals) rendered as unstyled browser defaults because the class had no
  CSS; they now have a proper button style.

- The Portal task page's "Retry last run" button is back in the header. A
  recent redesign dropped it by mistake, leaving Retry reachable only through
  the API.

- A run's output now keeps everything the agent said during the turn, not only
  its closing message: text the model wrote before a tool call — its narration
  of what it is about to do — is joined with the text it wrote after, so an
  Agent's TaskRun shows the whole turn rather than dropping the earlier part.
  This also removes the flicker where streamed narration appeared and then
  vanished when the run finished.

- Continue a Task with the prior Agent session instead of starting the next
  TaskRun with empty conversation history.

- The Portal task page now shows a breadcrumb back to where the task belongs —
  its agent, issue, or conversation — instead of only "Home".

- Continuing or retrying a Task no longer leaves its status, output, and
  timing showing the previous run's outcome until the scheduler next polls;
  the new run is reflected immediately.

### Security

- The Portal container now uses the slim Nginx Alpine image, removing unused
  util-linux libraries with known high-severity vulnerabilities from the
  runtime image.

- The worker control listener now supports TLS, and a worker reaches the server
  through one HTTP client that verifies the server certificate against a
  configured CA (`worker.server_ca_file`) with no insecure fallback. A
  `k8s_job` whose `worker.server_url` is `http://` is refused at startup unless
  `worker.allow_insecure_http` is set.

- Worker API routes now enforce the run's lifecycle: a run must be claimed
  (RUNNING) before it can stream, publish an artifact, read a Space Secret, add
  an Issue comment, download a plugin, or make a managed model call, and a
  terminal run is refused — so a leaked but unexpired run token cannot act once
  the run is over. Secret materialization also reads its consumption from the
  Agent revision pinned onto the run, not the agent's current revision.

## [0.2.0-alpha.7] - 2026-09-02

### Added

- Portal's agent editor can now set an agent's network and filesystem
  sandbox tier directly, and a team can set the default tier an agent
  inherits when it declares neither, from a new "Sandbox defaults" section
  on the team's Plugins settings tab.

- A background agent definition can now declare a network sandbox tier
  (none/registries/open) and a filesystem tier (workspace/shared-read/
  external-write) that its worker runs apply, without an operator
  hand-editing `policy.yaml` per agent.

- Deleted and expired artifacts now have their stored objects reclaimed by an
  hourly retention sweep, so deleting an artifact eventually frees the bytes
  instead of only hiding it; `storage.artifact_purge_after_days` delays that,
  and the sweep records what it expired and reclaimed in the audit trail.

- `./make test mysql` runs the store tests against a real MySQL on a database
  it creates and drops, and every pull request now runs it against a pinned
  server. The tests existed but skipped themselves without a DSN, so schema,
  query, and transaction behavior was outside ordinary change review.

- Artifacts are now a top-level area in Portal instead of a space-settings
  tab, and each one has its own page at `#/artifact/<id>` showing its
  provenance, size, digest, and preview — so an `ar_` reference an agent
  returns is something a teammate can open, not just look up in a list.

- A run whose sandbox resolved weaker than its surface's own baseline — or
  fell back to unconfined because the OS backend was unavailable — now logs a
  warning at startup and marks the run's trace and `SessionStart` hook
  payload as downgraded, instead of proceeding silently.

- The sandbox can now bound a Bash command's own CPU time, memory, process
  count, and open file descriptors (`sandbox.process.*` in settings.yaml /
  policy.yaml). Memory limits have no effect on macOS, which does not
  support them at the OS level.

- The CLI's `/skills` panel is now selectable: arrow keys move, typing filters
  by name or description, and Enter fills the input with `/<skill-name>` for
  you to finish and send, matching Desktop's skill picker.

- The store's four conditional-update claims — task claiming, run transition,
  result-delivery claiming, and cancellation beside a worker's report — are now
  tested against a real MySQL under contention, so a run cannot quietly be
  claimed twice or a task summary delivered twice.

- A quota tier can now cap what a space's artifacts hold in total with
  `max_storage_bytes`, refusing an upload that would cross it; space settings
  report storage alongside runs and tokens. The seeded tiers set no limit, so
  an existing deployment is unaffected until an operator chooses one.

- The deployment smoke now dispatches a real task that calls the `Bash` tool
  through the worker and checks the sandbox actually confined it, so a
  regression in worker sandboxing is caught automatically instead of only by
  manual reproduction.

### Changed

- `./make e2e local` now picks a fresh Compose project name and ports for
  every run instead of a fixed one, so it never collides with a contributor's
  persistent stack or another concurrent run. `./make compose up`/`kind up`
  can also be pointed at a second, differently named and ported stack with
  `BUILDMAX_COMPOSE_PROJECT`/`BUILDMAX_KIND_CLUSTER` and matching port
  variables, so multiple deployments can run side by side.

- `./make e2e` now requires a suite instead of defaulting to `kind`. The default
  was the suite with the heaviest prerequisite, so a bare invocation reported a
  missing cluster rather than a missing argument; it now prints the six suites
  and what each one needs.

- Local kind manifests now live under `deployment/kind/`, dropping the old
  `dev-` directory prefix; the `./make kind` command surface is unchanged.

- Contributor-local configuration now lives in one gitignored `.local/`
  directory, created by `./make setup local` from the committed templates. The
  repository-root `.env` became `.local/env`, `settings.local.yaml` became
  `.local/settings.yaml`, and the local copy of
  `deployment/buildmax-secret.example.yaml` became `.local/buildmax-secret.yaml`.
  `./make doctor` reports whether the directory is there, and the command moves
  files left at the old paths rather than duplicating them.
  `deployment/compose/.env` is unchanged: Compose reads it from the compose
  file's own directory.

- `./make help` now groups commands by what running one does — the everyday
  local ones, the model runs that need an API key, the deployments that start
  containers or bill a provider, and the release chores — replacing an
  "Advanced" section that held `fmt` next to the DigitalOcean infrastructure.

- Trimmed the long-winded comments in `config-examples/*.example.yaml` down
  to the facts a reader needs, keeping every documented key and default.

- Adding a team member is now an invitation: `POST /api/teams/{team_id}/members`
  is replaced by `POST /api/teams/{team_id}/invitations`, which creates a
  pending offer instead of adding the account immediately. The invited
  account sees it at `GET /api/invitations` and confirms it at
  `POST /api/invitations/{invitation_id}/accept`. Inviting an email with no
  BuildMax account is refused, naming the `system_admin` path to create one
  first — team-scoped invitation never creates an account. Admin may now
  invite at the member role; owner may invite at member or admin. A team
  owner can also `PATCH /api/teams/{team_id}/members/{user_id}` to promote or
  demote a member without a remove/re-add round trip, transfer ownership by
  setting a target's role to owner (unilateral and immediate), and
  `POST /api/teams/{team_id}/members/{user_id}/login-code` to recover a
  locked-out member of their own team without needing a `system_admin`.
  Portal's Space → Members page has an Invite dialog, a pending-invitations
  list with revoke, a role selector, a distinct ownership-transfer
  confirmation, and a login-code action; Account → Invitations lists and
  accepts what has been sent to the signed-in user.

### Fixed

- Creating a conversation now rejects a channel the caller may not claim.
  `workflow`, `issue_agent`, and `system` mark a conversation the server made
  and nobody holds; naming one produced a conversation the Portal rendered as
  agent-owned and the list hid. Only `portal`, `telegram`, `cron`, and
  `webhook` are accepted, and an unknown channel is a 400 rather than a stored
  string nothing understands.

- `./make kind seed` reads a model's `cache_control` and `pricing` blocks and
  passes them to the catalog, so a seeded deployment answers with the same
  cache policy and rates as the local settings it was seeded from. It no longer
  reads the removed `prompt_cache` key, whose flag the catalog command dropped —
  a settings file that still carried it failed the whole seed.

- A quota limit that cannot be read now refuses the work instead of admitting
  it. A failed team, tier, or usage lookup was reported as "allowed", so a
  deployment whose database was unreachable served unmetered runs and managed
  inference and recorded nothing about having done so. Such a failure is a 500
  naming the read that failed, distinct from the 429 an over-quota team gets;
  a team with no record, no tier, or a tier that names nothing is still
  admitted, because absence of a limit is not the same as not knowing. Team
  usage reports the same way rather than showing a zeroed snapshot.

- Creating a team now reports its plugin curation mode as `open` rather than
  leaving the field empty, so the team returned by a create and the same team
  returned by a later read no longer disagree about who fills its plugin
  activation list.

- Worker pods now use a custom seccomp profile instead of Kubernetes'
  `RuntimeDefault`, and the sandbox re-binds `/proc` instead of mounting a
  fresh one — both were silently preventing the worker's Bash sandbox from
  running at all once deployed to a real cluster.

### Security

- `command` and `http` hooks now run through the same sandbox confinement
  that already applies to `Bash` and `WebFetch`, instead of reaching a shell
  or the network unconstrained regardless of the sandbox being enabled.

- The sandbox now probes its backend with a real confined command before
  trusting it, instead of only checking that `bwrap`/`sandbox-exec` is on
  `PATH`. A backend that cannot actually confine a command now reports
  unavailable, so `fail_if_unavailable` refuses to start the run instead of
  silently executing commands unsandboxed.

- Fixed the worker Bash sandbox being silently unconfined under
  `worker.run_mode: local_process` (Compose): the marker that gates the
  strict worker baseline never reached that worker's filtered environment,
  so model-chosen commands ran with no filesystem confinement at all. A
  Compose deployment also needs the Job pod's seccomp override for `bwrap`
  to build its sandbox at all; `deployment/compose/compose.yaml` now sets
  it.

- Worker task runs built from the official container images now select the
  stricter worker sandbox baseline instead of resolving to the permissive
  CLI default; the worker container images now install `bubblewrap` and
  `socat`, the Linux sandbox backend's dependencies. A worker running
  outside those images (a bare host or native Windows) keeps the CLI
  default, since it cannot guarantee the backend is present.

## [0.2.0-alpha.6] - 2026-08-29

`0.2.0-alpha.5` carries the same changes but published nothing: it was tagged,
and its release build then failed before uploading any archive or image. Only
its Portal image exists. This version replaces it.

### Highlights

- The agent now remembers a local project between sessions. Each project keeps
  small Markdown memories under a generated index; only the index is carried
  into a turn, and the agent opens a memory when the line suggests it is worth
  reading. Every surface can see what a project remembers: `buildmax info`, the
  TUI `/info` panel, and a read-only Memory view in Desktop.
- Team issues reach the terminal. `buildmax issue list`, `issue show`, and
  `issue status` work the issues a team assigned you, and `buildmax --issue
  <id>` scopes a local session to one: the agent reads the issue and reports
  back, while status, assignee, and sub-issues stay a person's decision.
- Desktop and the CLI share one local project catalog, so both list the same
  sessions for the same repository, worktrees included.
- `/compact` compacts a TUI session on demand instead of waiting for the
  context window to fill.
- Alpha releases prepare themselves: a daily check opens a reviewable pull
  request, and merging it is what creates the tag.

### Upgrade notes

- **A deployment running workers as Kubernetes Jobs must set all four
  `worker.k8s.resources` bounds.** The server now refuses to start when one is
  missing or invalid, naming the key to edit, instead of logging the problem
  and running worker pods unbounded.
- **`buildmax stats` is now `buildmax info`**, and the TUI `/stats` panel is
  `/info`. Scripts reading `--json` get the statistics under `stats` and the
  new memory listing under `project_memory`.
- **A run trace's `prompt_layers` record is now `context_sources`.** Anything
  reading traces has to follow the rename; the record now names every source a
  run started with, not only the system-prompt layers.
- **`--continue` is now scoped to the directory you are in**, not the newest
  session anywhere on the machine. `--continue --project` widens it to every
  directory of the project and prints where it will run.

### Added

- The agent can now remember things about a local project between sessions. A
  project keeps one bounded memory store at
  `<BUILDMAX_HOME>/projects/<project_id>/memory/`: small Markdown files, one
  per memory, under a generated `MEMORY.md` index. Only the index is shown to
  the model on every turn, as fallible recall rather than as instruction --
  `AGENTS.md` stays the place for rules -- and the agent opens a memory's body
  with `MemoryRead` when the line suggests it is worth reading. `MemoryWrite`
  creates, replaces, or deletes one memory at a time, and changing a memory
  requires having read it, so two sessions recording different facts never
  collide and a stale write risks one memory instead of the store. Subagents
  receive neither the index nor the tools. The store is shared by every session
  of that project, including those in other worktrees of the same repository,
  it is yours to read, edit, or empty at any time, and `--no-project-memory`
  runs without it in either direction.

- `buildmax --issue <id>` scopes a local session to a team issue: the agent can
  read the issue, its sub-issues, and recent discussion, and post a short report
  back. It cannot change the issue's status, assignee, or sub-issues. A report
  from your machine is recorded as a local agent report attributed to you, and
  Portal shows it as reported rather than said — it is not a run the deployment
  scheduled, counted, or traced.

- `buildmax issue list` shows the issues a team assigned you, across every team
  you belong to, so team work can be picked up from the terminal instead of a
  board in a browser. Listing issues by assignee and by status now works over
  the API too; both filters were described but not implemented.

- `buildmax issue show <id>` prints one issue with its sub-issues and recent
  discussion, and `buildmax issue status <id> <status>` moves it when you are
  done. Moving status stays a person's action: an agent working the issue can
  say it believes the work is finished, and you decide. A session started with
  `--issue` now also prints which server, team, and issue it is working and
  where prompts go, before the first model call.

- An agent working a team issue can now read it and report back. `GetIssue`
  returns the issue, its sub-issues, and recent discussion; `ReportToIssue`
  posts one bounded comment on the thread. Both are scoped to the issue the run
  was started for, and neither can change its status, assignee, or sub-issues.

- `/compact` in the TUI summarizes the conversation so far and continues from
  the summary, instead of waiting for the context window to fill up. It keeps a
  much shorter tail verbatim than the automatic pass, reports what it replaced
  and what the context costs now, and honors the same `pre_compact` and
  `post_compact` hooks.

- Desktop has a **Memory** button beside the message box: it lists what the
  project remembers and shows the body of whichever memory you select, over the
  same store the CLI and TUI read. It is read-only for now — memories are
  Markdown files and the drawer prints the directory so you can edit them
  directly — and it names any file that could not be parsed, since such a memory
  is silently absent from every run until it is repaired.

- `buildmax project list` shows the local projects and marks the ones whose
  locator no longer resolves; `buildmax project relink <project-id>` points one
  at the current directory after a repository or folder has moved, keeping the
  memories and sessions attached to it. A run that registers a new project while
  others are unresolved now says so and names the command, since otherwise the
  duplicate looks like the feature working.

- A daily release check now prepares a reviewable pull request once the latest
  alpha is at least 72 hours old and user-visible changes are waiting. Merging
  that pull request creates the version tag and starts the existing publication
  workflows; an empty or unreviewed release is never published on the timer.

- Add `./make ocean` to provision disposable DigitalOcean infrastructure, deploy a pinned private application trial, inspect it, and tear it down for beta qualification.

- Add idempotent OpenRouter model initialization for the DigitalOcean trial,
  including automatic selection and rollout of the Tier 1 conversation model.

- Add `./make ocean show all` to inspect the standard Kubernetes workload
  resources in the BuildMax namespace with the qualification cluster's isolated
  kubeconfig.

- Add an owner-only Kubernetes tunnel for inspecting the qualification MySQL
  database locally without opening its firewall to the public internet.

### Changed

- A deployment running workers as Kubernetes Jobs must now set all four
  `worker.k8s.resources` bounds, and the server refuses to start when one is
  missing, is not a Kubernetes quantity, is zero or negative, or names a limit
  below its own request. The error names the key to edit. Previously a typo such
  as `memory_limit: 4 gigabytes` was logged and dropped, which left worker pods
  running model-chosen commands with no memory limit while the configuration
  looked correct.

- `--continue` now resumes the newest session recorded in the directory you are
  in, rather than the newest session anywhere on the machine. In a repository
  with worktrees, a project-wide search could pick a session from a sibling
  worktree and run there, moving your working root out from under the workflow
  whose whole purpose is branch isolation. When this directory has no sessions
  but the project does, `--continue` says how many and names `--continue
  --project`, which widens the search and prints the directory it will run in.
  The TUI `/sessions` picker spans the project -- one Git repository including
  all its worktrees, or one plain folder -- and marks sessions recorded in
  another tree; press `a` to see every project. `--resume <id>` still finds a
  session by id, but returns to the directory it ran in and refuses to continue
  one that belongs to a different project. `buildmax info` with no argument
  follows the same scope.

- `buildmax stats` is now `buildmax info`, and the TUI `/stats` panel is
  `/info`, because both now answer a second question: what the session's project
  remembers. In the TUI the two halves are tabs — `tab` and the arrow keys
  switch, and on the memory tab `enter` opens a memory to read the reason behind
  it, which until now meant finding the file by hand. On the command line the
  memory listing follows the statistics, and `--json` carries both under `stats`
  and `project_memory`.

- Desktop and the CLI now share one local project catalog, so both opened on the
  same repository list the same sessions. A folder Desktop already knows --
  including a worktree of a repository in the list -- opens that project instead
  of adding a duplicate, session grouping follows the project a session belongs
  to rather than matching folder paths, and deleting a project no longer takes
  its sessions with it unless you confirm that as well.

- The run trace's `prompt_layers` record is now `context_sources`, which names
  every source a run started with rather than only the system-prompt layers: the
  instruction layers and their sizes, the project and the project memory it
  loaded with that document's revision and digest, the session notes and todos
  it inherited, and whether a compaction summary stood in for messages. It
  carries sizes and revisions, never content. `buildmax doctor` now also reports
  which project the current directory belongs to, where its memory file is and
  whether it fits its budget, and any sessions naming a project this machine no
  longer has.

- Let operators explicitly print qualification database credentials with
  `./make ocean info --show-secrets` while keeping ordinary output redacted.

### Fixed

- The release build no longer fails after the tag exists. The image scan that
  now runs before publication kept its database inside the checkout, and
  GoReleaser refuses to publish from a worktree git reports as dirty, so
  `v0.2.0-alpha.5` was tagged and then published nothing. The scanner now caches
  outside the checkout.

- Editing an issue no longer silently overwrites someone else's change. An
  update now carries the version it was built from, and the server refuses a
  write built on a stale copy; Portal reloads the issue and asks you to reapply.

## [0.2.0-alpha.4] - 2026-08-29

### Highlights

- A container-image-only security fix. Both published images apply their base
  image's pending security updates at build time, so they no longer ship an
  openssl the alpine branch has already patched. Nothing else changed since
  0.2.0-alpha.3.

### Upgrade notes

- **Operators running the 0.2.0-alpha.3 images should pull this version.**
  `ghcr.io/gougoujiang/buildmax:0.2.0-alpha.3` and
  `ghcr.io/gougoujiang/buildmax-portal:0.2.0-alpha.3` carry CVE-2026-14456 in
  openssl `3.5.7-r0`; their base tags lagged the fix alpine had published as
  `3.5.8-r0`. The archives are unaffected — the binaries are built with
  `CGO_ENABLED=0` and do not link the system openssl — so an installation from
  a 0.2.0-alpha.3 archive needs nothing.

### Security

- The published container images now apply their base image's pending security
  updates at build time. A base tag lags its branch's updates, so
  `ghcr.io/gougoujiang/buildmax:0.2.0-alpha.3` and the matching
  `buildmax-portal` image shipped openssl 3.5.7-r0 while alpine had already
  published 3.5.8-r0, and the release scan failed on CVE-2026-14456 after both
  images were pushed. The binaries in the archives were never affected: they
  are built with `CGO_ENABLED=0` and do not link the system openssl.

## [0.2.0-alpha.3] - 2026-08-29

### Highlights

- Ask the TUI agent for a worktree and it makes one, moves the session into it,
  and works there — every tool follows, along with the tree's own hooks,
  skills, and MCP servers. `/worktree` shows what exists and who is in it, and
  a delegated subagent can be given a worktree of its own.
- BuildMax can be measured on Terminal-Bench. `evaluation/harbor/` pins the
  harness, the dataset, and the adapter that runs the built CLI inside a task
  container, and `./make eval harbor run` starts a run and files it as trial
  bundles in the same contract as the local suite. Harbor stays the harness and
  its verifier stays authoritative.
- A Bash command that leaves a background process behind no longer hangs the
  agent. The tool waited on a pipe the process inherited, so a run was observed
  sitting on one call for two hours under a documented 120-second budget.
- A task run whose worker is killed without warning now fails within minutes.
  The server records the poll a worker already makes, and the reaper closes a
  run that has gone quiet — instead of leaving the Portal showing work in
  progress until the six-hour timeout.
- The agent loop's iteration cap is configurable with `agent.max_iterations`
  and `--max-iterations`, so a long unattended task is not cut off at the fixed
  200 that suited interactive work.
- A run that fails part way through reports what it did before it failed. The
  workspace, model, elapsed time, tool calls, and tokens already spent were all
  dropped on the failure path, in text and in `--output json` alike.

### Upgrade notes

- **A run that reaches the iteration cap now exits `7`**, with error kind
  `iteration_cap`, rather than sharing `4` with a failed model call. A script or
  harness that read `4` as a spent budget should follow the new code; `4` now
  means a fault worth retrying.
- `/rewind` takes a prompt back rather than moving to a message: it removes the
  prompt you pick along with everything after it and returns its text to the
  input box to edit and send again. Neither `/rewind` nor `/fork` offers an
  assistant message that asked for a tool any more, because choosing one left
  the conversation holding a tool call with no result.
- `./make eval` now measures CLI tasks only. Pass `--surface worker` for the
  worker tasks, or `--surface all` for both, which is what it used to run.
- A worker that finds its run already claimed exits `0` instead of `2`. Nothing
  read the code, and under Kubernetes the non-zero exit restarted a pod that
  could only refuse the run again.

### Added

- Ask the TUI agent for a worktree and it makes one, moves the session into it,
  and works there — every tool follows, along with the tree's own hooks,
  skills, and MCP servers. `/worktree` shows what exists and who is in it;
  removal asks first and refuses to discard uncommitted work. A delegated
  subagent can be given a worktree of its own with `Task`'s `worktree`
  argument.

- `./make build cli <os/arch>` cross-builds a static CLI for another platform
  and names the artifact `buildmax-<os>-<arch>`, leaving the host binary in
  place. It is how the CLI gets into a container image the project does not
  own, such as an external benchmark's.

- `./make build desktop` packages the Wails desktop app on its own, without
  spending the server, worker, and Portal builds to get at it. CI now runs it
  on macOS and Windows after a merge that touches the app, weekly, and on
  demand: nothing built the packaged app before, so a break in the asset
  embedding, the Wails configuration, or the native link waited for whoever ran
  `./make build` next.

- Add `./make eval harbor run`, which starts a Terminal-Bench run rather than
  only importing one. It assembles the Harbor command from
  `evaluation/harbor/pins.json` — dataset ref, adapter import path, and the
  `PYTHONPATH` that lets Harbor import the adapter — checks the toolchain the
  way `./make doctor harbor` does, cross-builds the `linux/amd64` CLI if it is
  missing, and imports the finished job. Tasks are selected with `--task`,
  `--canary`, `--limit`, or `--all`, and there is no default: the default would
  be all 89. `--oracle` runs each task's own reference solution to prove the
  environment, and `--dry-run` prints the command without running it. Harbor
  still owns the tasks, the containers, and the verdict.

- Add `evaluation/harbor/`: pinned Harbor, dataset, and adapter versions, the
  custom-Agent adapter that runs the built CLI against Terminal-Bench 2.1 inside
  a task container, and `./make eval harbor --job <dir>`, which files a finished
  job as BuildMax trial bundles and reports it in the same contract as the local
  suite — same subject tuple, same failure taxonomy, same pass rate with its
  uncertainty. Harbor stays the harness and its verifier stays authoritative:
  BuildMax neither re-runs the benchmark nor re-grades it, and an agent timeout,
  a verifier timeout, and a container that never started stay three different
  facts. `./make doctor harbor` reports what a run needs, reading the pinned
  versions rather than restating them, and prints the fix for each missing piece
  instead of installing anything.

- Make the agent loop's iteration cap configurable with `agent.max_iterations`
  in `settings.yaml` and `--max-iterations` for one run, so a long unattended
  task is not cut off at the fixed 200 that suited interactive work. A run that
  reaches the cap now exits `7` with error kind `iteration_cap` rather than
  sharing `4` with a failed model call, so a script or harness can tell a spent
  budget from a fault worth retrying.

- Add fail-closed `--sandbox` and `--sandbox-mode` controls for requiring Bash
  confinement on one CLI run without changing settings or weakening policy.

- `BUILDMAX_SERVER_URL` can now select the BuildMax server offered at CLI and
  Desktop sign-in and used by workers without rewriting configuration files.

- Add `./make setup harbor`, the write half of `./make doctor harbor`: it
  installs uv when it is missing, installs the Harbor version pinned by
  `evaluation/harbor/pins.json`, cross-builds the `linux/amd64` CLI a trial
  uploads, and finishes by re-running doctor's own probes, so what setup
  installs and what a benchmark run requires cannot drift apart. Steps already
  done are skipped. Installing uv runs Astral's installer, and the exact command
  is printed before it runs. A trial sandbox stays yours to choose: setup reports
  that Docker or a `DAYTONA_API_KEY` is missing rather than picking one. Doctor
  still installs nothing. Both commands now name `linux/amd64` rather than the
  host's architecture, because the architecture that matters is the task image's:
  an arm64 binary uploaded into an emulated image fails with an exec format error
  once the trial is already running.

- Hooks can subscribe to `worktree_create`, `worktree_remove`, and
  `cwd_changed`, so an audit or notification hook can follow which tree a
  session is working in. All three are advisory.

### Changed

- `./make eval` now runs only CLI evaluation tasks by default; select worker
  tasks with `--surface worker` or both surfaces with `--surface all`.

- `/rewind` now takes a prompt back rather than moving to a message: it lists
  the prompts you typed, removes the one you pick along with everything after
  it, and returns its text to the input box to edit and send again. The Desktop
  History panel does the same, and lists rewind and fork points separately.

### Fixed

- Stop a Bash command that leaves a background process behind from hanging the
  agent forever. The tool reads output through a pipe, and a server or daemon
  the command started inherits the write end and holds it open, so the wait
  outlived both the command and its timeout — a run was observed sitting on one
  call for two hours under a documented 120-second budget. The tool now stops
  waiting shortly after the command ends or its deadline passes, and says so
  when output was cut short.

- Report what an evaluation run spent. The trial home a trial runs under carried
  a model entry with no prices, so `./make eval` had always reported cost as
  unavailable however the model was configured; it now carries the price list
  from the same `settings.yaml` entry it takes the endpoint from. A
  Terminal-Bench run takes one through the new `pricing` agent kwarg, passed
  explicitly rather than read from the machine, so the figure is reproducible.

- A run that fails part way through now reports what it did before it failed.
  The workspace, the model, the elapsed time, the tool calls, and the tokens
  already spent were all dropped on the failure path, so `buildmax -p` closed
  with `Tool calls: 0`, `Duration: 0ms`, an empty `Workspace:`, and no token
  line even when the run had edited files and been charged for the calls that
  got it there. `--output json` reported the same blanks. The session's own
  totals missed them too, so a conversation resumed after a failed turn counted
  from zero for good.

- Fix the documented Terminal-Bench run command, which named the dataset without
  its pinned ref. Harbor resolves a bare name to `latest` while the importer
  stamps the pinned digest on every bundle it writes, so a run started from the
  README filed its evidence under a dataset version it had not measured. The run
  command and the reproduction command recorded on each bundle are now built by
  one function, so neither can drift from the pins again.

- The `/rewind` and `/fork` pickers no longer offer an assistant message that
  asked for a tool. Choosing one left the conversation holding a tool call with
  no result, which OpenAI and Ollama refuse; the picker now offers the reply
  that ended each turn.

- A worker that finds its run already belongs to someone else now exits cleanly
  instead of reporting a failed dispatch. It had exited `2` so the scheduler
  could tell that case from a run that failed to start, but nothing read the
  code: under Kubernetes `2` is non-zero like any other failure, so the Job
  restarted a pod that could only refuse the run again, and under the local
  runner it made the scheduler mark a run `FAILED` while another worker was
  still executing it. The Job's retry budget stays at three, which is what
  recovers a worker that died before claiming its run — while reading its
  configuration, fetching the run, or resolving its model — since the run is
  still `SCHEDULED` for a fresh pod to take.

- A task run whose worker was killed without warning is now failed within
  minutes instead of hours. A worker already polls its own run route every few
  seconds so it can hear about a cancel; the server now records that poll, and
  the stale-run reaper fails a `RUNNING` run that has gone quiet for two
  minutes. Before this, only `worker.run_timeout` — six hours by default — ever
  closed such a run, so a SIGKILL, an OOM kill, or a lost node left the Portal
  showing work in progress for the rest of the day. The timeout stays as the
  backstop for a run that never reached `RUNNING` or never reported at all.
  Nothing is re-run: a worker that died may already have caused side effects,
  and whether the task is safe to repeat is not the server's call.

## [0.2.0-alpha.2] - 2026-08-26

### Highlights

- A conversation can go back: `/rewind` in the TUI moves it to an earlier
  message, `/fork` branches a new session off one, and Desktop offers both
  behind a **History** button. Each names the tools that ran in the span you
  are choosing across, because rewinding moves the conversation and does not
  undo the files those tools wrote.
- A session is now a folder — metadata, an append-only conversation journal,
  and its own run traces — written as the turn happens rather than rewritten
  after it. An interrupted run keeps everything up to the moment it stopped,
  and a session can only be open in one place at a time.
- Logins are kept in the operating system's credential store (Keychain,
  Credential Manager, Secret Service) instead of as plaintext in `auth.json`.
- A turn ends with a dim recap of what it did, and the answer you are likely
  about to type is offered as ghost text when the agent asks you something.
- `openapi.json` now describes every route the server registers — 117
  operations instead of 40 — held to an exact match by a test in both
  directions.

### Upgrade notes

- **Sessions from earlier versions are ignored.** The move to `sessions/<id>/`
  ships with no conversion, and the `traces/` root is gone with it. Existing
  conversations and their traces stay on disk untouched; BuildMax will not
  read them. Delete them, or keep them for reference.
- A plaintext `auth.json` is moved into the credential store the first time it
  is read, so a login survives the upgrade. A machine with no usable store
  falls back to the file as before, and `BUILDMAX_CREDENTIAL_STORE=file` keeps
  the previous behavior deliberately. `buildmax login`, `whoami`, and `doctor`
  say which one a login is actually using.
- API clients generated from `openapi.json` will see far more than the routes
  change: `created_at`, `started_at`, and `ended_at` are typed as RFC 3339
  strings rather than integers, which is what the API has always sent, and
  managed inference is documented at `/api/llm/models` and
  `/api/llm/completions` rather than the team-scoped paths that never existed.
- A Compose stack that moves the Portal off `8080` should set
  `BUILDMAX_CORS_ORIGIN`, which now follows `BUILDMAX_PORTAL_PORT`, instead of
  hand-editing `server.yaml`.

### Added

- Desktop offers rewind and fork through a **History** button in the chat status
  bar: one list of messages, with a tab for whether choosing one moves this
  conversation back or starts a new session from it. Each names the tools that
  ran in the span you are choosing across, because their effects stay on disk
  either way. Both are refused while a run is in flight, and say so.

- `/fork` in the TUI branches a new session off an earlier message and switches
  to it, leaving the original untouched — for trying a second approach without
  losing the first. The two are independent from that point on, so deleting one
  never affects the other. It shares the picker `/rewind` uses, and names the
  tools that ran after the fork point, because their effects are on disk and the
  new session's history will not mention them.

- CLI and Desktop now keep a login's access and refresh tokens in the
  operating system's credential store (Keychain, Credential Manager, Secret
  Service) instead of as plaintext in `auth.json`. A file written before this
  change is moved on first read, and a machine with no usable credential store
  falls back to the file as before; `buildmax login`, `buildmax whoami`, and
  `buildmax doctor` say which one a login is actually using. Set
  `BUILDMAX_CREDENTIAL_STORE=file` to keep the previous behavior.

- `/rewind` in the TUI moves the conversation back to an earlier message. It
  says which tools ran in the part you are about to drop before you choose, and
  again afterwards, because rewinding moves the conversation and does not undo
  the files it wrote or the commands it ran. Nothing is deleted: the messages
  you rewind past stay on disk, and the next reply starts a new branch.

- The CLI TUI and Desktop now end a turn with a dim recap of what it did, and
  offer the answer you are likely about to type as ghost text in the input box
  when the agent asks you something — `tab` accepts it. Neither enters the
  conversation. Configure with `agent.turn_digest` in `settings.yaml`.

### Changed

- `./make help` now lists every command, grouped by what it is for, with the
  contributor path under it. It used to open on six commands and keep the rest
  — including `eval`, `models`, and the deployment tasks — behind
  `./make help all`, which is now an alias for the same list.

- Enabling or disabling a catalog model with `buildmax-server model enable` or
  `model disable` now records the model's name in the audit trail, which the
  equivalent `/api/admin` route already did. The trail distinguishes a catalog
  change by who made it, not by where it was made.

- Each session is now a folder under `sessions/<id>/` holding its metadata, an
  append-only conversation journal, and its own run traces, replacing the single
  JSON file per session and the `traces/` root. The conversation is written as
  it happens rather than rewritten after each reply, so an interrupted run keeps
  everything up to the moment it stopped, and BuildMax can tell a tool call that
  never started from one that may already have changed something. A session can
  be open in one place at a time; opening one already in use says so instead of
  letting two runs overwrite each other. Sessions from earlier versions are not
  migrated and are ignored.

### Fixed

- Session files and the session index are now replaced atomically, so a crash,
  a full disk, or a machine failure part-way through a save leaves the previous
  conversation intact instead of an unreadable file.

- Prevented stale-run recovery and late worker reports from overwriting a task
  run's committed outcome or leaving its task and artifact list out of sync.

- The Compose stack derives `cors_origin` from `BUILDMAX_PORTAL_PORT` through
  the new `BUILDMAX_CORS_ORIGIN` override, so moving the Portal off `8080` — to
  run it beside a kind cluster, which cannot move — no longer needs a hand edit
  of `server.yaml` to keep the browser from blocking every request.

- A team membership record with no role is now read as a member everywhere.
  Team-scoped routes previously refused such a record entirely while resource
  routes admitted it, so the same account could be a member for one request and
  a stranger for the next. No release could create one, so this affects only a
  database written before the role was defaulted.

- A background run on a deployment that leaves `worker.llm.model` unset reaches
  the deployment's default model again. The run was assembled with no model at
  all and failed with `model not found: ""`; naming a model in `worker.llm` was
  the only way around it.

- Starting an Issue's assigned Agent no longer leaves an empty conversation
  behind when the run is refused. A team at its quota limit collected one on
  every attempt: the conversation was created before the task, the task was
  what checked the allowance, and nothing deletes a conversation.

- `openapi.json` now describes every route the server registers, 117 operations
  instead of 40, and corrects the schemas that called a timestamp an integer:
  `created_at`, `started_at`, and `ended_at` are RFC 3339 strings, which is what
  the API has always sent. Tests now hold the document to an exact match with
  the registered routes.

- The served OpenAPI document no longer describes routes that do not exist.
  Managed inference is `GET /api/llm/models` and `POST /api/llm/completions`,
  not the team-scoped paths it listed, and listing or creating a conversation is
  team-scoped rather than `/api/conversations`.

- Correct model administration and kind-seeding guidance to use the
  deployment-wide catalog instead of removed aliases and managed settings.

- Fixed TUI shutdown so Ctrl+C cancels and joins the active agent run instead
  of leaving stream senders or background goroutines behind.

## [0.2.0-alpha.1] - 2026-08-24

### Highlights

- The local agent can work in the background: `Bash`, `Task`, and the new
  `Monitor` tool detach into jobs, `JobList`, `JobOutput`, and `JobStop`
  inspect them, and a completion or a monitor line can wake the conversation
  in the TUI and in Desktop. Every job writes a durable, redacted event log.
- The CLI and Desktop now have exactly two modes, decided by whether you are
  signed in: local models from `settings.yaml` straight to their provider, or
  the models a deployment offers through its gateway. Neither mode covers for
  the other.
- Local inference through Ollama, in a local session and through a deployment
  alike, with no provider key on either path.
- Runs report what they cost: per-model `pricing`, per-call cost in traces,
  cache tokens on every surface that shows spend, `buildmax stats` for a
  session, and prompt caching as a policy rather than a boolean.
- Teams activate published plugin releases, an agent names the plugins its
  background runs load, and a worker materializes exactly those. Portal gains
  a Plugins section under Space.
- Identity and storage were rebuilt: opaque unprefixed entity identifiers,
  numeric relational keys underneath them, `DATETIME(6)` timestamps, and an
  API resource that names its own identifier `id`.

### Upgrade notes

- **An existing Alpha database and object store must be recreated.** Opaque
  entity identifiers, numeric relational keys, text `public_id` columns, and
  `DATETIME(6)` timestamps all land in this release with no conversion
  migration. Every existing identifier, access token, refresh session, worker
  token, Portal link, bookmark, and stored object key is invalid.
- API clients that read a resource's own identifier from a type-named field
  (`task_id`, `user_id`, `team_id` on the resource itself) must read `id`.
  Relationships keep their semantic names.
- The deployment-wide `worker.token` is gone. Remove the secret from your
  manifests and upgrade the server before the worker image: a run dispatched
  without a run token now fails rather than falling back.
- Managed models are named by catalog name. Replace `llm.aliases` and
  `llm.default_alias` with `llm.default_model`, `worker.llm.alias` with
  `worker.llm.model`, and `/api/teams/{team_id}/llm/...` with `/api/llm/...`;
  a `transport: buildmax` entry drops `team_id` and names the catalog model.
- `models[].transport` and `models[].server_url` are removed from
  `settings.yaml`; a session's mode follows `buildmax login`/`logout`, and
  `default_model` names the entry a signed-out session starts with.
- The `prompt_cache` boolean is removed. Write `cache_control: {mode: off}`
  where it said `false`; Anthropic agent turns now cache by default.
- The server stops in order within `shutdown_grace` (default 25s). Deployments
  should set a matching `terminationGracePeriodSeconds` and `preStop` pause, as
  the reference manifests do.
- Native Desktop bundles are still not published by the release workflow. The
  tagged source contains Desktop; GitHub Release artifacts contain the CLI and
  server binaries.

### Added

- A Portal agent names the plugins it loads for a background run, and loads only
  those: nothing is inherited from what its team activated. The selection
  versions with the rest of the definition, so an earlier revision still says
  what that agent named, and restoring one brings its selection back.

- Background events can wake the conversation in the TUI: `run_in_background`
  calls accept `deliver_result` to have the completion delivered as its own
  turn, and a `Monitor` started with `react` sends each delivered line back
  for analysis. Delivered payloads are marked as untrusted observations,
  recorded with non-user provenance, and never run user-prompt hooks.

- Background subagents in the TUI and Desktop: `Task` accepts
  `run_in_background` to delegate investigation without blocking the
  conversation. The final reply is read with `JobOutput`, the job stops with
  `JobStop`, and traces link the subagent run to the tool call that launched
  it.

- `./make cache-qualify` checks prompt caching against a real provider. Every
  other cache test runs against a fake upstream, which proves what BuildMax
  sends and nothing about what a provider does with it — a request can be
  perfectly shaped while the provider declines to cache it, for a minimum prefix
  length, an unsupported model, or an expired retention window. The suite runs
  first write, sequential read, changed prefix, long-history lookback,
  streaming, concurrent cold starts, and retention, and prints what the provider
  reported for each. Name the target with `BUILDMAX_CACHE_QUALIFY_PROVIDER`,
  `_MODEL`, `_API_KEY`, and optionally `_BASE_URL`; it calls a paid provider, no
  check runs it, and it skips when none is named. A model entry can also name an
  `integration` for an OpenAI-compatible gateway whose cache behaviour has been
  qualified — none has, so every value is currently refused.

- Desktop delivers background events into the conversation: a completion
  requested with `deliver_result` or a `react` monitor line runs as its own
  turn when the owning session is on screen and idle, and is parked — not
  lost — while that session is busy or another one is open. The transcript
  labels delivered events as background observations, collapsed by default.

- Background jobs write a durable event log under `<traces>/jobs/`: launch
  provenance (owning session, parent run and tool call, sandbox fact),
  monitor lines with drop accounting, and the terminal state. Logs are
  redacted, bounded, and always end with how the job ended.

- `./make kind seed` fills the local kind cluster's model catalog from the
  repository-root `settings.local.yaml` and grants the deployment's teams an
  alias for each model, so the CLI and Desktop can drive the managed transport
  against real inference. The cluster's own Portal conversations and task runs
  keep answering from the deterministic mock.

- Background commands in the TUI and Desktop: `Bash` accepts
  `run_in_background` to detach long builds, tests, or servers as local jobs,
  and the new `JobList`, `JobOutput`, and `JobStop` tools inspect and stop
  them. Jobs pass the normal permission and sandbox checks before detaching
  and end with the application.

- Local models through Ollama: `provider: ollama` on a model entry calls a
  local daemon's own API with no `api_key` at all. It sends the context window
  on every call, so the daemon no longer applies its own default and quietly
  truncates the system prompt and tool definitions out of a longer request —
  the failure that made small local models look like they could not call tools.
  `buildmax init --ollama` writes the entry, `buildmax models --local` lists
  what is installed and which models can call tools, and `buildmax doctor`
  reports a daemon that is not running or a model that is not pulled with the
  command that fixes it.

- A deployment can serve a local Ollama model: `--provider ollama` on
  `buildmax-server model add`, or `provider: ollama` under
  `conversation.model`, with no credential in either place. Real inference and
  real tool calls reach the gateway, the `llm_call` ledger, and quota without a
  provider key or a bill. The daemon stays on the host — a pod cannot use the
  host's GPU — and the deployment names an address that reaches it, which under
  Docker Desktop is `host.docker.internal`.

- New `Monitor` tool in the TUI and Desktop: watch logs, files, or CI by
  running a command whose stdout lines become bounded events. Lines are
  rate-limited and truncated, dropped lines are counted, and the watcher
  passes the same permission and sandbox checks as `Bash`.

- OpenAI Responses calls now carry a scoped `prompt_cache_key`, and can ask for
  24-hour retention with `cache_control: {ttl: 24h}`. The API caches on its own
  either way, so the key does not turn caching on — it decides which prefixes
  are looked up together, which matters because callers sharing a credential
  otherwise share one bucket. The key is derived from the credential, the model,
  the team on a managed call, and fingerprints of the system prompt and tool
  definitions; it carries none of them in readable form and is never written to
  a ledger, trace, or log. Retention vocabulary is per provider: `5m` and `1h`
  are Anthropic's and `24h` is OpenAI's, and asking for one where it is not
  documented is refused at startup rather than sent and ignored.

- Add `./make models list`, `./make models info <model>`, and
  `./make models check` to look up configured and OpenRouter-catalog model
  details, and catch context_window drift, from the terminal instead of the
  openrouter.ai models page.

- `buildmax -p --output json` and `--output jsonl` now report `trace_id` and
  `trace_path`, so a script can open the trace that run wrote instead of
  guessing at the newest file in the session's trace directory.

- A run can now report what it cost. A model entry takes a `pricing` block —
  currency plus four decimal rates per million tokens, for fresh input, cache
  reads, cache writes, and output — and the CLI prints a `Cost(session)` line,
  `--format json` carries the same figures, and the session file keeps a running
  total. A managed deployment sets the same rates per catalog model with
  `--currency`, `--input-price`, `--cache-read-price`, `--cache-write-price`,
  and `--output-price` on `buildmax-server model add`; the rates in force are
  copied onto each call's ledger row when it is accepted, so repricing a model
  does not restate what a team already spent. Portal's run view shows the run's
  cost and what caching saved against an uncached baseline. Cost is shown only
  where every rate needed for it was recorded — anything else reads
  `unavailable` rather than zero — and a saving is reported only when caching
  actually saved: a run that wrote cache entries nothing read back paid more
  than it would have uncached, and is shown as the cost it was.

- Run details now name which revision of an agent definition a run executed
  under, and say when the definition has been edited since. Editing an agent
  changes what its next run does, which is intended; until now nothing recorded
  which text produced a given result.

- Run details now open with where the run came from. A background run records
  the conversation message it was asked for in, and the Portal shows that
  message next to the instruction the worker was actually given — so a
  constraint missing from the instruction can be told apart from one that was
  never asked for. It is shown even for a run that wrote no trace.

- `buildmax stats [session-id]` reports one session's spend with its cache
  breakdown, how close it came to the context window, how many bytes each tool
  put back into that window, the split between model time and tool time, and
  what its delegated runs cost; `--json` emits the whole record. `/stats` in
  the TUI shows the same figures for the session on screen.

- A team can activate published plugin releases for its background runs, pinned
  to an exact version and digest. A team either curates that list or leaves it
  open, in which case naming a plugin in an agent activates it. Releases
  contributing hooks or MCP servers cannot be activated yet.
  `buildmax plugin activations --team <id>` reads what a team activated.

- Portal gains a Plugins section under Space: what this team has activated and
  at which version, what each release contributes, which agents name it, whether
  a newer release is available, and whether the team curates its list or opens
  the whole catalog. Owners and admins can activate, update, and suspend from
  there; any member can read it.

- A run trace now records what each model call cost, not just what the run did.
  `llm_end` carries that call's own token counts and its estimated cost;
  `run_end` carries the run's. The per-call figures matter because the running
  totals cannot answer which turn was expensive, and subtracting consecutive
  records to find out goes wrong the moment a call in between failed. It also
  makes the shape of caching visible: the turn that writes a cache entry costs
  more than it would have uncached, and only a later turn reading it back puts
  the run ahead. Costs are absent when the model was unpriced, which is not the
  same fact as a call that cost nothing, and `cost_incomplete` on `run_end`
  says a call did work that could not be priced.

- A background run now loads the plugins its agent names. The server resolves
  the team's activations when the worker claims the run, and the worker fetches
  exactly those releases, verifies each against its pinned digest before
  extraction, and refuses to start rather than run without one it was told to
  have.

### Changed

- An API resource now names its own identifier `id` rather than repeating its
  type — a task returns `{"id": ..., "team_id": ...}` where it used to return
  `{"task_id": ..., "team_id": ...}`. Relationships are unchanged and keep their
  semantic names, so only the field naming the resource itself moved. Users,
  teams, artifacts, audit events, managed model catalog entries, system grants,
  managed LLM call records, and webhook keys are affected; most other resources
  already used `id`. Agent and workflow revisions no longer return an identifier
  at all: a revision is addressed by its parent plus its revision number, which
  is what the restore route already used. Catalog plugins and plugin releases
  likewise drop theirs, being addressed by name and by name plus version.

- The CLI and Desktop now run in one of two modes, decided by whether you are
  signed in. Signed out, the models are the ones in `settings.yaml` and each
  call goes straight to its provider; signed in, they are the ones that
  deployment offers and every prompt goes there. `buildmax login` and
  `buildmax logout` switch, and nothing else configures it — `models[].transport`
  and `models[].server_url` are gone, because a session is in one mode or the
  other rather than holding both kinds of entry. A new `default_model` key names
  which entry a session starts with while signed out; a deployment names its
  own. `buildmax models`, the `/model` pickers, and the TUI footer all say which
  mode you are in, and `buildmax doctor` reports it as a check of its own.
  Desktop no longer opens on a sign-in form: local mode is a working state, so
  the workbench opens directly and signing in is an action in the account menu.
  Neither mode covers for the other — a deployment that is down, or a login that
  has expired, stops the session and says so rather than quietly sending the
  next prompt to a provider you did not choose for it.

- `./make doctor` now warns that Portal test dependencies and Playwright
  browsers are unavailable when npm is missing, instead of reporting cached
  browser state as ready for `./make e2e`.

- The server now stops in order on SIGINT or SIGTERM: it reports itself
  unready so a load balancer stops sending it work, ends the streams watching a
  run so the Portal reopens them elsewhere, refuses new conversation turns and
  waits for the ones running, drains in-flight requests, and only then stops its
  background loops. The whole budget is `shutdown_grace` in `server.yaml`,
  default 25s; the reference manifests set a matching
  `terminationGracePeriodSeconds` and a `preStop` pause.

- `buildmax --help` no longer lists cobra's auto-generated `completion`
  command. `buildmax completion <shell>` still prints the shell script for
  anyone who wants it.

- BuildMax now identifies its outbound LLM requests with a versioned
  `User-Agent` header.

- `worker.run_mode: local_process` is now documented as what it is — a
  single-machine topology where a task run is a child process of the server
  under the same uid, sharing its trust domain — instead of being called a
  development path the Compose deployment contradicts. The startup warning says
  the same. Nothing about how a worker is launched changed; a deployment that
  needs its server separated from model-chosen code still runs
  `worker.run_mode: k8s_job`.

- A managed model is now named by its catalog name rather than by a team alias.
  `server.yaml` loses `llm.aliases` and `llm.default_alias` and gains
  `llm.default_model`, which names one of the models `buildmax-server model add`
  created, or is left empty to use the first enabled one. `worker.llm.alias`
  becomes `worker.llm.model`. A name that matches no catalog row now stops the
  server at startup, while an empty catalog does not — rows are added while the
  server runs. In `settings.yaml`, a `transport: buildmax` entry drops `team_id`
  and puts the catalog name in `model`; every model a deployment offers is
  available to every user of it, so `buildmax models --team <id>` becomes
  `buildmax models --server`. The gateway routes move from
  `/api/teams/{team_id}/llm/...` to `/api/llm/...`, and a managed call is
  recorded against the person who made it rather than against a team.

- Entity relationships are stored as numeric keys rather than as repeated
  identifier strings. Every reference that names exactly one kind of row is now
  a `bigint`, the public handle each row shows the outside world is a separate
  `binary(12)` column, and translating between the two happens inside the store
  and nowhere else. Nothing about the API changed — a handle is still what
  every request, response, token, log line, and object key carries. What
  changed is underneath: a team's task list is answered by reading an index
  backwards instead of sorting rows, usage aggregation is answered from indexes
  without reading rows at all, and identity no longer depends on the database's
  text collation. References that cannot be one number — a polymorphic actor, a
  provider's tool-call ID, an agent session naming a file — stay text
  deliberately, and a test refuses a new one added without that reason.

- Login-chain, trace-file, and Desktop-project identifiers lost their `as_`,
  `rt_`, and `p_` prefixes and are now ordinary opaque IDs, leaving one
  identifier format in the codebase. None of the three is read by a person or
  dispatched on; they had kept a prefix only because they are not database
  rows, which was where the previous change stopped rather than a reason to
  keep one. Background jobs are the exception and keep `jb_`: a job ID reaches
  the model as a bare string inside tool output, and free prose is the one
  place a type prefix says something the surrounding context does not.

- Entity identifiers are now opaque and unprefixed: `ivyoh5qcfu6ypfkhyedq`
  rather than `t_9f3k2m8x1qwe7rt4zy0p`. Each is 96 bits of crypto-random data
  written as 20 lowercase base32 characters, case-insensitive on input and safe
  unchanged in a URL, a filename, a Kubernetes name, and a shell. The type
  prefix is gone because nothing dispatched on it — a route, a JSON field, and a
  column already name the type — and because it was the only thing standing
  between a presentation choice and the database schema. Agent and workflow
  revisions, catalog plugins, and plugin releases carry no identifier at all
  now; they are addressed by their parent plus a revision number, by name, and
  by name plus version. Login-chain, trace-file, and Desktop-project identifiers
  keep their prefixes: none of them names a database row. **This is a breaking
  change with no compatibility path.** Every existing identifier, access token,
  refresh session, worker token, Portal link, bookmark, and stored object key is
  invalid. Recreate the database and object store rather than upgrading.

- Sub-agent delegations to a read-only agent type such as `explore` now run in
  parallel with each other and no longer prompt for approval, and a sub-agent's
  own tool calls honour `agent.max_parallel_tools` instead of always running one
  at a time.

- Prompt caching is now a policy rather than a boolean, and Anthropic agent
  turns cache by default. A model entry takes `cache_control: {mode, ttl}` —
  `mode: auto` (the new default) asks on an agent turn, whose prefix goes out
  again on the next iteration, and never on a one-shot call such as title
  generation or compaction, where a cache write costs more than it can ever
  save; `off` never asks and `force` always does. `ttl` selects retention where
  the provider documents it — `5m` or `1h` on Anthropic, `24h` on OpenAI — and
  is refused at startup anywhere else rather than sent and ignored, as is
  `force` on a provider that takes no cache instructions at all. The
  `prompt_cache` boolean it replaces is removed rather than kept as a
  shorthand; write `cache_control: {mode: off}` where it said `false`. Managed
  deployments get the same policy per catalog model through `--cache-mode` and
  `--cache-ttl` on `buildmax-server model add`.

- Prompt-cache token counts now reach every surface that shows what a run
  spent. Providers already reported them and the managed ledger already stored
  them, but they stopped there: run statistics, run traces, session totals, the
  CLI's summary and `--format json` output, Desktop's run status, the team
  run-ledger route, and Portal's run-spend view all dropped them, so a cached
  run was indistinguishable from an uncached one. Cached counts remain a
  breakdown of the prompt rather than an addition to it, and each surface shows
  them only where a provider actually reported some — a provider that reports
  nothing is not a provider that missed.

- Server `public_id` columns now store the 20-character canonical text form
  instead of raw bytes, so direct database queries show the same IDs the API
  does. Breaking for existing server databases: there is no migration —
  recreate the database (`./make kind down && ./make kind up`, or drop the
  Compose MySQL volume).

- The deployment-wide `worker.token` is gone. Every `/api/worker/*` route now
  takes only the run token the server mints at dispatch, so a worker can read
  and write its own run and nothing else. `worker.token` and
  `BUILDMAX_WORKER_TOKEN` are no longer read, and the secret has been dropped
  from the Compose and Kubernetes manifests — remove it from yours. Upgrade the
  server before the worker image: a run dispatched without a run token now
  fails immediately instead of falling back.

- Stopping a server in `local_process` worker mode no longer waits for the agent
  run it dispatched. The scheduler stops claiming immediately, asks the worker it
  spawned to stop — which reports what the run produced — and gives up on a
  worker that will not go, within the same `shutdown_grace` budget. In
  `k8s_job` mode the Jobs already outlive the server, and still do.

- Every stored moment in time is now a `DATETIME(6)` column and an RFC 3339
  string in the API, replacing Unix seconds in `bigint` columns and JSON
  numbers. Audit `since` and `until` query parameters take RFC 3339 too. An
  existing Alpha database is recreated rather than converted; no conversion
  migration ships.

- A task run whose worker is shut down — a drained node, an evicted pod, a
  restarted deployment — now stops, uploads what it produced, and reports
  `FAILED` with a message naming the shutdown, instead of sitting in `RUNNING`
  until the stale-run reaper closes it hours later. Its output and artifacts are
  kept and shown the way a cancelled run's are.

### Fixed

- Editing an agent no longer fails with a duplicate-key error. The update wrote
  a row rebuilt from the domain model, which no longer carries the database's
  own key, so it was saved as a new agent rather than as a change to the
  existing one; the unique index caught it and refused the edit. The update now
  addresses the agent by its identifier.

- The Portal conversation list no longer fills up with machinery. A workflow
  step and an issue agent run each create a conversation because a task requires
  one, and those were listed alongside conversations people actually hold — on a
  team that runs either, they pushed real conversations off the page. They are
  still kept, and a link straight to one still opens it.

- A finished background task now always gets reported back to its conversation.
  The reply used to be a one-shot attempt: a model call that failed, a busy
  conversation, or a server restart between the task finishing and the reply
  being written meant the conversation was simply never told. The report is now
  recorded as owed and retried until it succeeds, or until it has failed enough
  times to be given up on with the reason kept. The result itself was never at
  risk — the task's card reads it directly.

- Closing the runtime now waits for the background job trace writer to drain,
  so a job's final record always lands in `traces/jobs/` before exit.

- Refresh the built-in context_window fallback table: several ids had drifted
  from what providers now report (some by a lot, e.g. deepseek-r1), one
  Anthropic id had a hyphen where the real id uses a dot and never matched,
  and OpenAI/Anthropic now cover their fast/mid/premium tiers instead of one
  model each.

- The TUI `/model` and `/tasks` panels scroll. With more entries than fit the
  terminal each listed the first few and a `… N more` row, while the arrow keys
  kept moving a selection through the ones it was not showing — so a model past
  the fold could be switched to, and a job past it stopped with `s`, without
  ever being seen. Both lists are now a window that follows the selection, and
  `/model` opens on the model in use rather than at the top.

- A Portal conversation now shows the background tasks it started. Each task
  gets a card in the thread carrying its status, what the run produced, its
  files, its run details, and stop or run-again, placed among the messages by
  when it was created. The cards are read from the server, so they survive a
  refresh and a dropped connection, and a task result is no longer displayed as
  if the user had typed it.

- The Portal image now ships third-party license attributions: the licenses of
  every npm production dependency are served at `/third-party-notices.txt`, and
  the image carries the Apache-2.0 text under `/usr/share/licenses`.

- Pressing esc on the `/` command popup now closes it for good. The popup was
  rebuilt from the input on every message, so the next cursor blink brought it
  straight back; it now stays closed until the typed command changes.

- Session token counts and cost now include the work a run delegated to a
  subagent and what each context compaction cost; both were previously spent
  and never counted, so long sessions and ones that used `Task` under-reported
  themselves. A subagent's trace is also filed under the session that started
  it rather than under a discarded id, and `tool_end` records how a call failed.

- A finished background task now reports back whether or not anyone is watching.
  The reply used to be sent through the creator's first open browser connection,
  which meant it was skipped entirely when they had none and reached only one
  tab when they had several. Every connection on the team is now told the task
  changed, and the reply itself is written to the conversation independently of
  any connection.

- A run trace now reaches disk one record at a time, so a run that is
  interrupted — killed, crashed, or given up on mid-turn — keeps its final
  records instead of losing up to 4KB of them. Previously the tail stayed
  buffered until the run closed cleanly, which made the last recorded event
  appear seconds before the last one that actually happened. Shutting the
  runtime down also closes the trace of a run still in flight, marking it
  abandoned rather than leaving it open with no `run_end`.

- A TUI panel such as `/model` or `/tools` now trims its list to what the
  terminal can show. It listed a fixed number of rows whatever the height was,
  so on a short terminal the panel pushed the input box and the footer off the
  top of the screen, and nothing could scroll them back. Panel lines also no
  longer wrap inside the panel border, which was quietly doubling the height of
  the `/tools`, `/skills`, and `/diff` panels.

- Closing a TUI panel such as `/model` no longer leaves its box drawn above the
  input. The terminal renderer the CLI depends on stopped erasing the lines a
  shrinking frame vacates, so every dismissed panel stayed on screen; the
  dependency is pinned back to a working revision.

- The inbound webhook's `202` response no longer labels a run identifier
  `task_id`. It was returning the task run's ID under the task's name and no
  task identifier at all, so a caller that wanted to follow the work had to
  guess which of the two it had been handed. The body now carries both, matching
  what creating a run through the API already returned:
  `{"task_id": "...", "task_run_id": "..."}`.

## [0.1.0-alpha.2] - 2026-08-22

### Highlights

- Desktop can now run locally against models from `settings.yaml`, while a
  signed-in session still provides managed models and team work from a BuildMax
  deployment.
- Private deployments gain a System Administrator surface, account controls,
  deployment health and configuration views, model administration, audit
  search and export, retention controls, and quota warnings.
- Task runs can use operator-approved models through the managed LLM gateway,
  publish durable artifacts, expose model-call and trace details, and be
  stopped or retried from Portal.
- Issues now support comments and a two-level sub-issue hierarchy; agents and
  workflows keep append-only revision histories and pin the definitions used
  by a run.
- Plugins can contribute skills, agents, MCP servers, and hooks from local
  directories or an immutable private marketplace, with source policy and
  management surfaces in Portal and Desktop.
- The local agent loop gains queued mid-run input, parallel read-only tool
  calls, durable notes and tasks, provider-native model protocols, reasoning,
  prompt caching, and image results from MCP tools.

### Upgrade notes

- Every worker API route now prefers a short-lived credential scoped to one
  task run. The deprecated deployment-wide `worker.token` remains accepted for
  this release with a warning so a rolling upgrade can finish; it is scheduled
  for removal in the next release.
- MCP tools without a true `readOnlyHint` are now treated as writes. Interactive
  CLI/TUI and Desktop sessions ask before write tools run; autonomous surfaces
  refuse calls that require a person to approve them. Review MCP annotations
  and `tools.permissions` before upgrading unattended workloads.
- Database migrations remain forward-only and are applied at server startup.
  Back up the database before upgrading; rolling back the database schema is
  not supported.
- Native Desktop bundles are still not published by the release workflow. The
  tagged source contains Desktop local mode, while GitHub Release artifacts
  contain the CLI and server binaries.

### Added

- System Administrators get an account API under `/api/admin`: list and search
  accounts, inspect one with its teams, roles, and live session count, create
  one, issue a login code, revoke every session, and grant or revoke the
  administrator role itself. Every one of those is recorded in the audit trail
  against the administrator who did it. The API refuses to revoke the
  deployment's last grant — that is what the operator command is for — and an
  administrator cannot disable their own account.

- `GET /api/admin/audit-events` searches the audit trail across every team,
  filtered by team, actor, action, and time window. It is the only way to read
  the events that have no team at all — logins, administrator grants, account
  actions — which the team-scoped trail could never return; ask for those with
  `team_id=none`. `GET /api/admin/teams` and `/api/admin/teams/{team_id}` list
  teams with their size, quota tier, and usage, and name their members and
  roles. Both are metadata: an administrator learns that a team exists and how
  large it is, never what is in it, and reaching a team's own resources still
  requires membership.

- The administration area has a Models page, and `GET /api/admin/llm/models`
  behind it, showing which upstreams the deployment will call and which aliases
  point at each one — a model that is enabled with no alias is unreachable by
  every team, which is the most common reason an operator's model appears not
  to work. Models can be retired and restored from there. Adding one stays
  `buildmax-server model add`: it carries a provider credential, and doing that
  over HTTP puts the key in a request body, a proxy log, and whatever the
  browser did with the form.

- `GET /api/admin/system` and `GET /api/admin/config` report what a deployment
  is doing: version, applied schema migrations, dependency health as `/readyz`
  sees it, worker run mode and model transport, task runs by status, and the
  effective `server.yaml` with every credential reduced to whether it is set.
  Not its length, not a prefix — presence only. The configuration view also
  computes warnings for states that are trade-offs elsewhere in the project:
  self-registration left open, the deprecated shared worker token still set,
  managed worker inference with no aliases, local-process run mode, a run
  timeout that outlives its run token, and run output on local disk.

- Agents and workflows keep a numbered, append-only history. Every edit records
  the definition it produced and who wrote it, `GET
  /api/teams/{team_id}/agents/{agent_id}/revisions` and the matching workflow
  route read it back, and `POST .../revisions/{revision}/restore` writes an
  earlier version's content back — which appends a new revision rather than
  erasing the ones after it. Saving without changing anything records nothing.
  Restoring a workflow leaves its `draft`/`published`/`archived` state alone, so
  bringing back an old definition cannot unpublish a workflow a team is running,
  and the definition is revalidated so a version whose agents were since deleted
  is refused rather than restored into a plan that cannot run. A workflow run
  records the workflow revision it expanded and each step records the agent
  revision it ran under. Existing agents and workflows are given a revision 1
  holding their current content on upgrade. Portal shows the history on the
  workflow page and on each agent, with restore in place.

- The audit trail can be kept for a fixed window. `audit.retention_days` in
  `server.yaml` expires events older than it; the default is 0, which keeps
  everything, because a deployment that never chose a retention policy has not
  decided to discard evidence. Every sweep that removed anything records an
  `audit.pruned` event naming the range and the count — a trail that begins
  partway through says that policy shortened it, rather than leaving a reader
  to wonder whether somebody truncated it. Nothing else deletes an audit event,
  and there is no way to delete a particular one.

- The audit trail can be downloaded. A space owner exports their own from space
  settings, a System Administrator exports the deployment's — under the same
  filters as the search, including the events that belong to no space — from
  `#/admin`. Both come as CSV or JSONL. An export is itself recorded as
  `audit.exported` with the number of events that actually left, because
  reading the whole record is an action on it; an administrator's export
  narrowed to one space is recorded in that space's trail too, so its owner can
  see that the deployment read it.

- The desktop app can be used without a BuildMax server. Its sign-in screen now
  offers local mode, which runs the agent on this machine against the models in
  `settings.yaml` — the same thing the CLI does without `buildmax login`.
  Signing in is still there, and still what adds managed models and a team's
  work; the choice is remembered, and either one can be left from the sidebar.

- Issues have a comment thread. Team members can comment, edit their own
  comments, and delete their own; team owners can delete any. Deletion is
  permanent. When an agent run finishes on an issue it posts one comment with
  what it reported and a link to the run.

- Read-only tool calls from the same model message now run at the same time.
  When the model asks for several files or searches at once, they no longer
  queue behind each other -- three 100ms reads take about 100ms rather than
  300ms, and `WebFetch` batches gain the most. Only calls a tool declares
  read-only overlap: writes, shell commands, `Task`, and MCP calls still run
  alone and in order, calls are never reordered, and the message history a run
  produces is identical whatever the limit. Tune with `agent.max_parallel_tools`
  in `settings.yaml` (default 4, range 1-16; 1 restores one-at-a-time).

- Plugins: a directory under `~/.buildmax/plugins` holding `skills/`,
  `agents/`, `mcp.json`, and `hooks.yaml` now loads on the next run, so a
  team can share a workflow by cloning one repository. `buildmax plugin
  list`, `status`, `validate`, `enable`, and `disable` show what each one
  contributes, which checkout it came from, and what overrode it.

- Private plugin Marketplace: a System Administrator publishes a plugin
  directory with `buildmax plugin publish`, and anyone signed in to that
  deployment installs it by name with `buildmax plugin install`. Releases are
  immutable and identified by version and SHA-256 digest, which the client
  verifies against the catalog before anything is unpacked.

- Operators can restrict where plugins may come from with a
  `plugins.allowed_sources` block in `policy.yaml`. A plugin an operator
  excluded shows as `refused` with the reason rather than disappearing, and a
  directory whose provenance cannot be established does not pass a policy that
  names one.

- Portal and Desktop manage plugins: an administration section for the
  deployment's catalog and its releases, a browsable list of what is published
  with the command that installs it, and a Desktop panel showing what a
  project's runtime loaded, with install, update, disable, and remove.

- Portal has an administration area at `#/admin`, visible only to a System
  Administrator: deployment health and version, accounts with disable, enable,
  login-code and session controls, spaces with their size and usage, and the
  deployment-wide audit trail. It is a separate area from space settings rather
  than another tab in it, because authority over the deployment is not authority
  inside a space — every page says what it does not show, and there is no link
  from a space into its contents.

- A message typed while the agent is working is now queued instead of refused,
  on the CLI/TUI, Desktop, and Portal alike. Up to ten can wait per
  conversation; past that the message is refused rather than the oldest being
  dropped, so nothing the user believes is scheduled disappears. In the TUI the
  input stays visible and editable during a run, `Enter` queues, and `Esc` takes
  the last queued message back; Portal and Desktop show what is waiting at the
  end of the thread. Stopping a run discards everything queued behind it — those
  messages were written for work that has just been called off. A run that
  *fails* keeps the queue, and each message still gets its turn.

- On the CLI/TUI and Desktop a queued message no longer waits for the whole run.
  The agent picks it up at its next step — as soon as the tool batch it is
  running finishes — so a correction reaches the model while the work it
  corrects is still in progress, instead of after it. Such a message passes the
  same `UserPromptSubmit` hook as any other prompt, and appears in the run trace
  as `user_input`: a message that entered a run after it started is part of what
  that run was told to do. Portal keeps delivering a queued message as its own
  turn — its foreground turns are short, and its queue is shared by every client
  watching the conversation.

- Quota now says something before it starts refusing work. Passing 80% of a
  space's run or token limit records `quota.threshold_reached`, and work
  refused at the limit records `quota.exceeded` — at most once per limit per
  period, so a space that keeps submitting does not fill its own trail with
  retries. The actor is the deployment, not whoever submitted the work that
  tipped the total over. Portal states the same thing in space settings, and a
  space with no limits reports nothing rather than reporting comfort.

- A finished task run can be run again. Portal's Issue Detail shows **Retry Run**
  once a run is over, backed by
  `POST /api/teams/{team_id}/tasks/{task_id}/retry`. The retry carries the same
  input the previous run had, so recovering from a worker that died or a model
  that timed out no longer means retyping the instructions — and no longer
  invites retyping them slightly differently. The new run records which run it
  repeats, and the run it repeats is left exactly as it was, so what went wrong
  stays readable next to the attempt that followed it. A run still in flight is
  not retried but stopped first; a task that has never finished a run has
  nothing to repeat; and a workflow step is refused, because its workflow
  advances from that step's outcome and a run started outside it would report a
  second one.

- Portal's **Run details** now shows the managed model calls a run made, beside
  the trace the run wrote itself. The ledger has had a route since it became
  readable, and nothing displayed it: the two records answer different
  questions, and the difference is the point. The trace is what the agent did;
  this is what the deployment was asked to serve, on which operator-approved
  alias, and it is what a team's quota is computed from. An empty list is not
  reported as "spent nothing" — a run whose trace shows model calls that the
  ledger never saw is a run that reached a provider directly, and it says so.
  A call whose provider reported no usage is counted as unreported rather than
  as zero.

- Issues can be broken down into sub-issues. An issue may have one parent in the
  same team, the hierarchy is two levels deep, and a parent shows how many of its
  sub-issues are done. Sub-issue status is never rolled up into the parent —
  closing a parent with open sub-issues is allowed and warned about, not blocked.

- Durable traces now record each subagent run and link it to the immediate
  parent run, so a delegated run can be traced in either direction.

- A deployment can now have System Administrators: an authority over the
  deployment itself, held by an account and separate from every Team role.
  `buildmax-server admin grant | revoke | list` manages it on the machine that
  already holds the database credentials, which is also what recovers a
  deployment that has lost every administrator — there is no configuration
  value and no break-glass credential. A grant carries no access to any team's
  issues, conversations, artifacts, files, or run traces; those stay behind
  team membership. Granting, revoking, and — new — account creation, password
  setting, and login-code issuance from `buildmax-server user` are all recorded
  in the audit trail, which those three commands previously wrote nothing to.

- A team can keep durable files as artifacts. Upload one to
  `/api/teams/{team_id}/artifacts` and it gets a stable `ar_` reference that any
  member can open, preview, or download at `/api/artifacts/{artifact_id}`,
  wherever they saw the reference — the ID is the address, and no team appears
  in it. Content is immutable, deletion takes effect immediately at the
  authorization boundary, and a caller who is not in the owning team cannot tell
  a real artifact from one that never existed. `storage.max_artifact_mb` caps a
  single upload; it defaults to 100 MB.

- Agents can publish a finished file with the `UploadArtifact` tool and cite the
  `ar_` reference in their answer. It appears only where there is a server to
  publish to — a logged-in CLI or Desktop session, or a worker running a team's
  task — so a session running straight against a model provider is not offered
  a tool that could only fail. Nothing is uploaded automatically: the agent
  names the one file, which is what keeps `.env` files, caches, and intermediate
  output out of a team's artifacts. What a run publishes shows up on its issue
  alongside the run's own output.

- Task runs can reach models through the managed LLM gateway instead of calling
  a provider themselves. Set `worker.llm.transport: buildmax` in `server.yaml`,
  optionally with `worker.llm.alias`, and a worker no longer receives
  `BUILDMAX_CONVERSATION_MODEL_API_KEY` at all — it reaches operator-approved
  models through the server. The server states the transport and alias in the
  run's worker-API response, so a worker never chooses its own model, and it is
  told nothing else about it: the endpoint, the upstream model identifier, and
  the credential stay server-side. Direct mode is unchanged and remains the
  default; there is no automatic fallback between the two, because a server
  outage must not silently redirect governed traffic to a personal provider key.
  A deployment that asks for managed runs it cannot serve — `transport:
  buildmax` with no `llm.aliases`, or an alias no team may call — now fails at
  startup rather than at every run's first model call.

### Changed

- Deleting an agent no longer removes the record. Tasks, workflow step runs, and
  revisions all name an agent by ID, so dropping the row left dangling
  references and broke any workflow run still in flight at its next step. The
  agent is now marked deleted: it disappears from listings and from everything
  that starts new work — a run cannot be started with it, an issue cannot be
  assigned to it, and a workflow definition naming it is refused — while records
  that already refer to it still resolve and an in-flight run finishes on the
  snapshot it started with. Deleting an agent a *published* workflow still names
  is refused with `409` naming those workflows, so the breakage surfaces at the
  delete rather than at the next run; draft and archived workflows do not block
  it. There is no undelete: the row exists so references resolve, not as a
  recycle bin.

- The TUI footer says how the current model is reached — `direct` for a provider
  called from this machine, `buildmax <host>` for one reached through a BuildMax
  deployment. Transport is a property of the model entry and `/model` switches
  entries mid-session, so the answer now sits next to the model name instead of
  only in `buildmax models`.

- Log records name their subsystem in a `component` attribute instead of a
  prefix on the message. Four background loops in the scheduler alone had spelled
  it four different ways, one of them not at all, so no filter selected a
  subsystem. Levels follow one rule now — Error means a unit of work failed or
  was lost, Warn means degraded but finished — which makes a threshold select
  something. A worker stamps its run id onto every record it writes, including
  those from the agent loop and the tools.

- MCP calls are gated by what the server says about the tool. `CallMcpTool` now
  reads the `readOnlyHint` annotation from `tools/list`, which BuildMax fetched
  and discarded until now: a tool the server advertises as read-only runs
  unprompted, and anything else asks. **This tightens autonomous surfaces**, the
  one behavior change there — a task run, print-mode run, or Portal conversation
  calling an MCP tool that is not advertised as read-only is now refused rather
  than run silently. A server that omits the annotation is treated the same as
  one that says `false`, because the protocol cannot distinguish them. The hint
  decides whether BuildMax asks; it never grants trust.

- The server answers every request with an `X-Request-Id` header, and its logs
  now record each request when it finishes rather than when it starts — with the
  status, the duration, and that same id. A bug report can name the request, and
  every log line the request produced can be found by that name. The log level
  follows the status, so filtering at Error selects server faults without
  sweeping up refused requests.

- Tool permissions are configurable. A `tools.permissions` block in
  `settings.yaml` sets any tool to `allow`, `ask`, or `deny`, keyed by tool name
  or by the target it dispatches to (`CallMcpTool:github/*`), most specific rule
  winning. `allow` turns off the category prompt but not the safety checks — a
  sensitive path and a risky shell command still prompt, and only `deny`
  outranks them. `ask` means a person must look, so on a surface with no person
  the call is refused rather than run. `buildmax tools status` prints every
  tool's classification, its resolved action, the layer that decided it, and any
  rule ignored for an unrecognised action; `/tools` in the TUI marks tools that
  do not simply run.

- A worker reports what the server actually said when a call to the worker API
  fails, instead of only the HTTP status. A run that could not be claimed or
  patched used to log "500 Internal Server Error" and discard the sentence
  explaining why.

- Tools now classify what a call does, and the ones that write ask before they
  run. On the CLI TUI and Desktop, `Write`, `Edit`, `Task`, and `CallMcpTool`
  request approval where they previously ran unannounced; read-only tools are
  unchanged, and `Bash` keeps its own risk classifier as the authority. Surfaces
  with nobody to ask — print mode, workers, eval, and Portal conversations —
  behave exactly as before: the category prompt is only raised where a person
  can answer it, so a task run's file writes and shell commands are unaffected.
  The prompt itself now offers three answers rather than two — allow once (`y`),
  allow for the rest of the session (`a`), or deny (`n`) — and a session grant
  covers the tool by name, or the specific `server/tool` for an MCP call.
  Grants are held in memory and are gone when the process exits.

### Fixed

- Adding a member to a team on a deployment with no user store answers 503
  rather than 500. Every other "not configured" answer in the API is 503; this
  one was the outlier.

- A run's system prompt can be added to: free text appended as its last layer,
  after the runtime prompt and both `AGENTS.md` files. It is additive rather
  than a replacement, and because it lives in the system prompt it is sent with
  every model call instead of fading as the conversation is compacted. On the
  command line it comes from `--append-system-prompt`,
  `--append-system-prompt-file` (preferred for anything long or private, since
  an argument is readable by every process on the machine), or `--agent NAME`
  for the body of a definition under `.buildmax/agents/` — which supplies prompt
  text only, and does not switch the model or restrict tools. The text is capped
  at 8192 characters and an over-limit value is rejected rather than truncated.
  An `## Invariants` section within it is also restated at the end of every
  request. Resuming a session without one of these flags keeps the text it
  already ran under.

- A long session no longer loses its earliest context outright. Each compaction
  summarized only the messages it was discarding and then replaced the stored
  summary, so after the second compaction everything the first one covered was
  gone rather than condensed — which is why a long-running agent could forget
  what it had been asked to do. A compaction now summarizes the previous summary
  together with the newly discarded messages, so each summary subsumes the one
  before it. The stored summary is also bounded relative to the context window,
  since it lives in the system prompt, which is re-sent in full on every call and
  is never trimmed.

- Compaction summaries are better targeted. The summarizer is now shown the
  session's live notes and open tasks and asked to spend its detail on what
  bears on them, compressing what is already settled. It previously had no
  signal about what was still open.

- The desktop app no longer opens on a blank window. Its frontend bundle shipped
  two copies of React — one reached through the symlinked `@buildmax/gui`
  package — so the first hook of the first render threw and nothing was drawn.

- Cancelling a Desktop run while a tool approval prompt was showing left the
  project stuck. The run goroutine waited forever for an answer nobody would
  give, so its cleanup never ran and every later message was refused with "a run
  is already in progress". Approval prompts now return when the run is
  cancelled.

- The Desktop sign-in form shows the Server URL field instead of hiding it
  behind a collapsed "Server" toggle, and fills it from `settings.yaml`'s
  `server_url` the way `buildmax login` already did. A deployment that does not
  answer on `http://localhost:5678` — the kind cluster serves Portal and API
  from `http://localhost:8080` — is now reachable without hunting for the field.

- The system prompt no longer carries the compaction summary twice. Both the
  caller and the agent loop appended it, so every run after its first in-run
  compaction sent two copies.

- A failed run keeps the reason its worker reported. The worker records the
  failure and then exits non-zero, and the scheduler used to overwrite the
  record with the process error — so "context deadline exceeded calling the
  model" became "exit status 1". The scheduler now leaves a run alone once it
  has reached a terminal status.

- Signing in with a single-use login code no longer spends the code when the
  email address does not match it. The code was redeemed before the address was
  checked, so one mistyped or autofilled address burned it: the retry with the
  right address failed too, and an operator had to issue another code. The
  address is resolved first and the code is only redeemed for that account, and
  the server now logs why a code was rejected — the response still says nothing
  more than `invalid otp`. Whitespace around a pasted address or code is also
  ignored now, rather than failing the same opaque way.

- An MCP server that returns an image — a screenshot, a rendered chart — now
  sends the model the image instead of a base64 blob pasted into the tool
  result. Set `vision: true` on a model that can read images, or `--vision` on a
  catalog model. Left off, which is the default, the tool result says what came
  back (`(image: image/png, 43.2 KB)`) and the image is not sent, because a
  model without image support rejects such a request rather than ignoring it.

- A model entry can now name the wire protocol its endpoint speaks, so BuildMax
  reaches a provider's own API instead of only OpenAI-compatible gateways. Set
  `provider` on a `settings.yaml` model — `openai_compatible` (OpenAI Chat
  Completions, the default), `openai` (OpenAI's Responses API), or `anthropic`
  (the Anthropic Messages API) — and optionally `max_tokens` to cap one
  response. The value names a protocol, not a vendor: Claude through OpenRouter
  is `openai_compatible`, and Claude from `api.anthropic.com` is `anthropic`. An
  operator can serve the same three from the managed gateway with
  `buildmax-server model add --provider --max-tokens`, and `model list` now
  shows each row's provider. Existing configuration is unchanged: an entry that
  names no provider keeps calling what it always called.

- A model can now reason before it answers, and keep that reasoning across tool
  calls. Set `reasoning: low`, `medium`, or `high` on a `settings.yaml` model,
  or pass `--reasoning` to `buildmax-server model add`, and an `anthropic` model
  uses extended thinking at that effort while an `openai` model keeps its
  reasoning between turns. `openai_compatible` has no such state and ignores the
  setting. It is off by default, because it changes what a call costs and older
  models reject it. The reasoning itself never appears in the transcript:
  BuildMax stores it beside the assistant message and sends it back unread, and
  a session continued against a different provider drops what that provider
  cannot use rather than failing. CLI sessions, Portal conversations, and
  managed gateway calls all carry it, so a run keeps its continuity across a
  restart.

- Notes are now saved at the moment they would otherwise be lost. Before a
  compaction discards messages, the agent gets a bounded turn — with only the
  note and task-list tools in reach — to move anything it still needs out of
  the material about to go. Until now a note existed only if the agent
  remembered to write one, which it is least likely to do exactly when the
  context is filling up. A write rejected for exceeding the note limit earns
  one correction, since this is the last moment the material exists. A failed
  checkpoint is logged and the compaction proceeds.

- A Portal agent's instructions now reach the agent through its system prompt.
  They were
  rendered into the task input, which is a message like any other, so a long
  run compacted them away — and a task created with its own input dropped them
  entirely. The server resolves them per run, so editing an agent takes effect
  on its next run, and they travel in the worker API response rather than on
  the worker's command line, where every process on the machine could read
  them.

- Prompt caching is now available with `prompt_cache: true` on a model, or
  `--prompt-cache` on a catalog model. For `anthropic` it places cache
  breakpoints around the tool definitions and system prompt, which do not change
  between calls in a run; the OpenAI protocols already cache on their own. It is
  off by default because a cache write costs more than not caching and only pays
  back over several calls. Every provider now reports `cache_read_tokens` and
  `cache_write_tokens`, and a managed deployment records both on the call
  ledger. They break the prompt count down rather than adding to it, so a spend
  report must not sum them alongside it.

- The server creates the database named by `database.name` when the MySQL it
  connects to does not have it, instead of refusing to start. It is attempted
  only after the connection failed for that reason; an account without `CREATE`
  rights gets an error naming the statement to run by hand.

- The agent keeps durable session notes. A new `NoteWrite` tool records short
  entries — decisions and why they were made, approaches already ruled out,
  constraints stated once — and `TodoWrite` now stores its list instead of only
  formatting it. Both are kept on the session rather than in the conversation,
  so they survive the compaction that eventually discards the messages that
  produced them, and both are shown to the agent on every turn. Each call
  carries the complete list and replaces what was stored, which is what forces
  the agent to drop entries it no longer needs; notes are capped at 15 entries
  of 200 characters and an over-limit call is rejected with an explanation. A
  session that writes neither carries nothing extra. A subagent writes to its
  own list and cannot overwrite the one belonging to the run that delegated to
  it.

- A task run whose worker never reported an outcome no longer stays `SCHEDULED`
  or `RUNNING` forever. Only the worker moves a run out of those states, so an
  evicted pod, a killed process, or a run outliving its credential left work
  that Portal showed as in progress and nothing would ever close. The server now
  records such a run as failed after `worker.run_timeout` (6h by default), with
  an error that names the timeout rather than guessing which of those happened.

- A run trace now records which system-prompt layers the run loaded and how
  large each was, so a finished run can say what it was told before the
  conversation started.

- Portal can show a run's trace on deployments that keep run state on local
  disk. `local_fs` is the default persist backend, and it deliberately stores
  no copy of a run's global files — the worker has already written them under
  `workspaces_dir`. The trace endpoint asked only the backend, so it answered
  "this run's trace is no longer in storage" for every run on every default
  deployment, including the Compose quickstart, while the file sat on disk the
  whole time. It now falls back to disk, as the task conversation endpoint
  already did. Deployments backed by S3 were unaffected.

- Refreshing credentials now retries a short-lived Windows file-sharing
  conflict while atomically replacing `auth.json`. Without that retry, a
  concurrent caller could read the old refresh token after a successful
  exchange and spend it a second time.

- On Windows, a command that read the saved login while another one was renewing
  it could fail with "the process cannot access the file because it is being
  used by another process". Replacing `auth.json` is a rename, which a
  concurrent reader survives on macOS and Linux but not on Windows; reads and
  the replacement are now serialized within a process, so a run that renews its
  token no longer trips another caller in the same binary.

- A workflow run now uses the agent definitions it started with. Steps are
  dispatched one at a time as the previous step's task run finishes, and each
  dispatch re-read the agent, so editing an agent while a run was in flight
  changed what its later steps sent to the model — a run could execute two
  different versions of the same agent, with nothing recording that it happened.
  Each step run now stores the agent name, description, and instructions as they
  were when the run started, and dispatches from that copy. Runs already in
  flight when this ships keep the old behavior, since their steps carry no
  snapshot. The captured definition is returned with the workflow run detail and
  shown per step in Portal.

- `--workspace` now applies to the TUI. The flag reached the `--agent`
  definition lookup but never the agent itself, so file tools, `AGENTS.md`, and
  the footer's git branch all ran against the current directory while `--agent`
  resolved somewhere else — one run with two ideas of where it was. Print mode
  was never affected.

### Security

- An account can be disabled, and disabling it stops access immediately rather
  than when a token expires. Every credential the account holds is refused:
  password, login code, refresh token, the access token it is already carrying,
  and its webhook keys. Live sessions are revoked at the same time, and work the
  account queued but that has not started fails at dispatch instead of running.
  Previously there was no way to stop an account short of editing the database,
  and an issued access token stayed usable for its full lifetime — seven days by
  default. Disabling is not deletion: nothing is removed, and enabling reverses
  the state and nothing else.

- Each task run is now dispatched with its own gateway credential rather than
  sharing the deployment-wide worker token. The scheduler mints a short-lived
  run token naming the run's user, team, and task, delivers it in
  `BUILDMAX_RUN_TOKEN`, and the managed inference route accepts nothing else —
  a run token presented against another run's URL is refused, and so is the
  shared worker token. Every managed call a worker makes is therefore recorded
  in the `llm_call` ledger against a user as well as a team, which a shared
  secret could never support. Lifetime is `worker.run_token_ttl`, 24h by
  default; it is not renewable, so it must outlast your longest run. A run token
  cannot be used as a user login, and a user access token cannot be used as a
  run token. The token is cleared from the worker's environment once read, so
  model-chosen shell commands cannot print it — the sandbox that would otherwise
  strip it is off by default.

- `GET /api/teams/{team_id}/task-runs/{task_run_id}/llm-calls` lists what a run
  spent and on which approved alias. The ledger recorded this from the day it
  existed and had no route, so reading it meant querying the database —
  diagnosing a run should not require the database password. It carries no
  prompts or generated content, and omits the catalog entry an alias resolved
  to, which is the operator's routing rather than the team's.

- The run token now authenticates **every** `/api/worker/*` route, not only
  managed inference, so a run can read and write its own record and nothing
  else. The deployment-wide `worker.token` could name any run, which meant any
  worker could read the prompt text of every team's tasks, `PATCH` another
  team's run to SUCCEEDED with output it invented, or push deltas into another
  run's live stream. It is still accepted for one release, with a deprecation
  warning, because a server that has not restarted yet dispatches workers
  without minting a token; the next release removes it. Since every route needs
  one, every dispatched run is now given a token, direct-mode runs included.
  `buildmax-server run-token <task_run_id>` mints one for driving a worker route
  by hand, which is what copying the shared secret used to be for.

- A task run in flight can be stopped. Portal's Issue Detail shows **Stop Run**
  while an agent task is pending or running, backed by
  `POST /api/teams/{team_id}/tasks/{task_id}/cancel`. A run nobody has picked up
  yet ends immediately as `CANCELED`. A run a worker is already executing is
  asked to stop: the worker notices within seconds, ends its agent loop, uploads
  what the run produced, and reports `CANCELED` — so a canceled run keeps its
  output and artifacts instead of throwing away the work already done. The run
  records who asked and when. Nothing is left hanging if the worker is gone:
  the server finishes a run whose cancel goes unconfirmed for two minutes, which
  is the same sweep that closes abandoned runs. Cancelling twice while a run is
  stopping is not an error, and a canceled task can be run again.

## [0.1.0-alpha.1] - 2026-08-17

### Security

- The server pod is now created with the same containment as worker pods:
  non-root with an explicit uid, no added capabilities, no privilege
  escalation, `RuntimeDefault` seccomp, and a read-only root filesystem with a
  writable `/tmp`. It is applied in the local kind manifest as well as the
  production reference, so `./make kind up` exercises it rather than leaving it
  a setting that only appears in a file nobody applies.

- Task-run workers are no longer handed the JWT signing secret or the database
  password. Both reached workers by inheritance — a local worker got the
  server's whole environment, and a Kubernetes worker Job got every `BUILDMAX_*`
  variable the server held — even though a worker reads neither: it talks to the
  server over HTTP with its own worker token and never opens the database. Since
  a worker executes model-chosen shell commands, holding the signing secret
  meant a prompt could mint a token for any user, and holding the database
  password meant it could read every team's data. A worker now receives only the
  variables marked `WorkerNeeds` in `internal/config/env_spec.go`, and an
  unrecognized `BUILDMAX_` variable is withheld by default. Object-storage keys
  are still passed, because workers read and write run state directly; narrowing
  that needs a server-issued, run-scoped credential and is separate work.

- Worker Job pods are now created confined: non-root with an explicit uid, no
  automounted service-account token, all capabilities dropped, `RuntimeDefault`
  seccomp, and a read-only root filesystem with a writable `/tmp`. None of it is
  configurable, because a worker runs model-chosen shell commands. `run_as_user`
  under `worker.k8s` covers clusters that assign their own uid ranges.
  `worker.k8s.resources` adds optional CPU and memory bounds; leaving them unset
  keeps existing deployments unbounded rather than handing them a limit nobody
  chose. `local_process` mode is unchanged and remains a development path.

- The managed inference routes now authenticate before checking whether a
  gateway is configured. They were the only team-scoped routes that answered
  `503 managed inference not configured` to an anonymous caller, which told
  anyone who asked whether a deployment offers managed models.

- Signing in now returns two credentials instead of one. The access token is
  still a signed JWT the server keeps no record of; alongside it comes a refresh
  token, stored as a hash in the new `user_refresh_token` table and exchanged at
  `POST /api/token/refresh` for the next pair. A single 24-hour token meant two
  things at once: nothing could retire it early, and everyone had to sign in
  again every day — which, with no mail channel, meant an operator issuing a
  login code by hand each time. Splitting them separates those questions.
  `access_token_ttl` (7 days) is now the only thing that governs how long a
  leaked token works, and `refresh_token_ttl` (30 days) governs how long a
  session can be renewed without a new login code.

  Each exchange spends the token presented and issues its replacement in the
  same session, so a token appearing twice means two holders. Past
  `refresh_rotation_grace` that revokes the whole session and records an
  `auth.refresh_reuse` audit event — the legitimate holder is signed out too,
  because there is no way to tell the copies apart. The grace window exists
  because the CLI and Desktop share one credentials file across processes, where
  two simultaneous refreshes are ordinary rather than suspicious.

  Every login opens its own session, and `POST /api/logout` revokes one of them
  without touching the others. Expired login codes and refresh tokens are now
  swept hourly; the sweep for login codes had been written but never wired up.
  Existing tokens keep working until they expire, so upgrading does not sign
  anyone out.

  Every client renews rather than expiring. The Portal refreshes when a call
  comes back `401` and replays it, sharing one exchange between requests that
  fail together — several refreshes of the same token would read as a replay
  and revoke the session. Its WebSocket asks for a fresh token before each
  connect, because a rejected upgrade arrives as a close event with nothing to
  read. The CLI and Desktop renew inside `TokenForServer`, so `~/.buildmax/
  auth.json` now holds both credentials and is written atomically; `buildmax
  logout` revokes the session on the server rather than only forgetting it
  locally. Being offline never discards a session — only the server rejecting
  the refresh token does.

  Managed model clients now read the credential per request instead of at
  construction. A client is built once and cached for the life of the process,
  so a token captured there was fine until it expired and useless afterwards,
  with no way back short of a restart.

- People sign in with an email address and a password, hashed with argon2id and
  a per-account salt. Until now the only credential was an operator-issued
  login code, which meant every sign-in on every device went through a person —
  workable for a demo, not for a small team that will not stand up an identity
  provider. Passwords are the one credential that needs no delivery channel,
  which is why they come before SSO rather than after it.

  Login codes keep their job and lose the wrong one: they are now the recovery
  path, not the everyday way in. A code claims a new account or replaces a
  forgotten password, and `POST /api/password` sets one from a signed-in
  session. Changing an existing password requires the current one — a session by
  itself must not be enough, because an access token cannot be revoked before it
  expires and allowing it would turn a stolen token into a permanent takeover.
  Setting the *first* password needs only the session, which came from a code an
  operator issued by hand.

  The minimum is twelve characters and there is no composition rule, because
  "one digit and one symbol" pushes people toward short predictable passwords
  that satisfy it. Every failed password login answers with the same sentence
  and does the same hashing work whether or not the address exists, so the form
  cannot be used to ask who has an account. `user.password_set` and the
  credential each login used now appear in the audit trail.

  **Login is not rate limited.** A reachable server can be brute-forced online;
  the length minimum and a memory-hard hash raise the cost per guess but are not
  a substitute for throttling. Unified rate limiting is separate, planned work.

- `dev_login_otp` is removed, along with `BUILDMAX_DEV_LOGIN_OTP`. It was a
  fixed code that authenticated every registered account — a standing
  authentication bypass kept for the convenience of clicking through the Portal
  locally. A password does that job with no bypass:
  `buildmax-server user set-password dev@local` reads one from stdin, so it
  lands in neither shell history nor the process list. A `dev_login_otp:` left
  in `server.yaml` is ignored, as any unknown key is — the bypass is gone
  either way, but remove the line so nobody reads it as still doing something.

- The Portal's sign-up page is gone. It collected an email address, told the
  person a code had been sent, and sent nothing — BuildMax has no mail channel.
  `allow_signup` still works for the API, and still only creates an account that
  needs an operator-issued code before anyone can use it, which is why no form
  offers it.

### Added

- `deployment/production/`, a private deployment reference for a cluster that
  already runs its own MySQL, object storage, ingress, and certificates. One
  plain-YAML manifest and a README stating the contract each dependency has to
  meet — DDL privileges, `utf8mb4`, a dedicated bucket, one origin for Portal
  and API. It is written to be read and adapted rather than applied: every
  dependency address is a placeholder, so an unedited `kubectl apply` fails
  instead of coming up against the wrong database. Deliberately not a chart or
  a kustomize base, so it converts to whatever a cluster is already managed
  with. Nothing applies it, so `internal/architecture` parses its ConfigMap the
  way the server parses its own config and asserts the settings that make it a
  production reference rather than a copy of the development stack.

- A deployment can now point BuildMax at a database and an object store it
  already runs, which the connection layer previously could not express.
  `database.tls` carries a TLS mode into the DSN, defaulting to `preferred` —
  TLS whenever the server offers it, unverified — so an existing plaintext
  connection keeps working while every server that supports TLS gets it; set
  `true` for a managed database that should be verified. An empty
  `storage.minio.endpoint` now means AWS S3 and lets the SDK resolve the
  regional endpoint, instead of forcing a base endpoint at every store, and
  bucket addressing follows from that rather than being pinned to path style,
  which AWS S3 has not supported for buckets created since 2020;
  `storage.minio.path_style` overrides it. Leaving both storage keys empty
  falls through to the AWS SDK's default credential chain, so a pod can reach a
  bucket through IRSA, workload identity, or an instance profile rather than a
  long-lived key the deployment has to store and rotate.

- Versioned schema migrations. Changes that `AutoMigrate` cannot express — a
  backfill, a drop, a rename — are now an ordered list recorded in a new
  `schema_migration` table, so each runs at most once per database instead of
  probing `information_schema` on every server start forever. The two existing
  one-time migrations became the first two entries and are recorded on upgrade.
  The schema moves **forward only**: `Migration` has no `Down` field, and the
  compatibility promise is that schema version N keeps serving code from
  release N-1 — so a removal takes two releases, one that stops using the
  column and one that drops it. A binary that meets migrations from a later
  release warns and continues, because that is the promise working rather than
  a fault. Rolling a database back is not supported; recovery from a bad
  upgrade is a restore from backup.

- `POST /api/worker/task-runs/{task_run_id}/llm/completions`, the managed
  inference entry point for workers — the last route needed before a task run
  can use operator-approved models without holding an upstream provider key.
  The team, task, and run are derived from server state, so the only thing
  taken from a worker is the prompt it wants answered. A call is accepted only
  while the run is executing: the worker token identifies a worker, not the
  owner of a particular run, so without that any token holder could spend a
  team's quota against a run that finished weeks ago. Nothing calls it yet —
  workers still receive a direct model entry, and switching them over needs a
  decision about which alias a worker resolves.

- `GET /readyz`, which reports whether the server can actually serve traffic by
  probing MySQL and object storage. `/healthz` keeps its old meaning and still
  checks nothing: the two exist because Kubernetes acts on them very
  differently — a failed readiness check stops traffic, a failed liveness check
  restarts the container — and pointing both at a dependency-aware endpoint
  would turn every database blip into a restart of a working server. The
  reference manifest now points readiness at `/readyz` and leaves liveness on
  `/healthz`. The response names the failing dependency but never the reason:
  the endpoint is unauthenticated and connection errors carry DSNs, endpoints,
  and bucket names, so the reason goes to the server log. The storage probe
  only reads, so a backend that accepts reads and refuses writes still reports
  ready.

- A `sandbox_boundary` record in every run trace, written immediately after
  `run_start`, carrying whether the run was sandboxed and — when it was — the
  mode, backend, and the settings/policy/env source chain that decided it. It is
  written for unsandboxed runs too, with an explicit `"sandboxed": false`:
  traces are about to become the basis for answering what confined a run, and a
  missing field would read as "nobody checked" rather than "nothing confined
  it". The Bash sandbox still defaults off on every surface, so most runs record
  `false` today.

- A **Run details** view in Portal, on any issue output produced by a task run.
  It reports the model, duration, model and tool calls, tokens, the files the
  run changed, each tool call with its duration or the reason it was denied,
  and the failure cause. Three things it refuses to leave implicit: a run
  nothing confined says so in words rather than by omission, a run that wrote
  no terminal record is marked as such instead of reading like a success, and a
  bounded tool list says how many calls it is hiding. A run whose trace was
  never recorded and one whose trace has left storage both explain which.

- `GET /api/teams/{team_id}/task-runs/{task_run_id}/trace`, which answers what
  a run used, touched, spent, why it ended, and what confined it — the model,
  the tool calls with their durations, the files it wrote, tokens, the terminal
  error, and the resolved execution boundary. It returns a summary rather than
  the raw trace: model output, tool arguments, and tool results stay in the
  file. A run whose trace was never recorded and one whose trace has gone
  missing from storage are both 404 but say which.

- Worker task runs now upload their run trace and record where it landed, in a
  new `task_run.trace_path` column. The trace was previously written inside the
  run-scoped `BUILDMAX_HOME` and discarded with the run directory:
  `uploadTaskGlobal` uploads a named allowlist, not the whole tree, so nothing
  carried it out. The path is recorded on failure as well as success, since
  diagnosing a failed run is what a trace is for.

- `docs/contribute/architecture/data-model.md`, a full reference for the server
  database: every one of the 18 tables with its columns, types, nullability,
  indexes, and enumerated values, two entity-relationship diagrams, and the
  procedure for adding, renaming, or dropping schema. The schema had only ever
  existed as GORM tags on unexported structs, so a newcomer had to read
  `internal/infra/db` file by file to learn the data model.

- `./make check ci`, which runs everything a pull request runs except the
  Windows job: `check all` plus workflow linting, a Git history secret scan, Go
  and npm production dependency license checks, GoReleaser configuration
  validation, and a Windows cross-build. The tool versions come from
  `.github/workflows/ci.yml`, and a test now fails when the task runner's pins
  and the workflow's disagree. For contributors who would rather spend a
  laptop's time than the repository's Actions minutes. `./make doctor` now also
  reports shellcheck, because actionlint drops its shell script pass without it
  and does not say so, and GoReleaser, whose version it compares against the
  one the workflows run since nothing in `go.mod` pins it.
- `./make kind status` and `./make compose status`, read-only summaries of the
  two local deployment paths. `kind status` prints the selected cluster and
  context, probes the Portal ingress, and lists nodes plus the Deployments,
  Jobs, and Pods in every namespace `kind up` installs. `compose status` lists
  every service, including exited ones, and probes the server and Portal ports
  on the host. Neither creates, builds, or generates anything, so a stack that
  was never started is distinguishable from an unhealthy one without reading
  full container logs.
- A managed model catalog in the new `llm_model` table, edited with
  `buildmax-server model add|list|enable|disable` on the machine that already
  holds the database credentials. Credentials are read by exactly one query, the
  one that builds a provider client, and never appear in a listing, an API
  response, or an error — but note that database backups now carry provider
  keys. An optional `llm` block in `server.yaml` maps team aliases to catalog
  models, and `conversation.model_target` runs Tier 1 on one. An alias naming a
  model that does not exist fails its own calls rather than stopping the server,
  because the catalog is edited independently of the policy. See
  `docs/design/llm-gateway.md`.
- A managed inference gateway: `GET /api/teams/{team_id}/llm/models` lists the
  aliases a team may use, and `POST /api/teams/{team_id}/llm/completions` runs
  one blocking call against an operator-approved model. Clients name an alias,
  never an endpoint, a credential, or a provider model identifier. Every call is
  recorded in a new `llm_call` ledger — identity, model, timing, outcome, and
  token usage, with no prompts or generated content. No BuildMax client uses the
  gateway yet, and a team already over quota is refused, which is accounting
  rather than a spending ceiling.
- CLI and Desktop can use a deployment's managed models. A `settings.yaml` entry
  with `transport: buildmax`, `server_url`, and `team_id` calls a BuildMax
  server instead of a provider, so no provider key sits on the machine; its
  `model` field is a team alias. The credential comes from `buildmax login` and
  is only sent to the server the login belongs to. `buildmax models` lists every
  configured model and where it sends prompts, and `--team` lists a team's
  aliases. The model picker in the TUI and Desktop names the destination for a
  managed entry, and `buildmax doctor` reports one that cannot authenticate.
  There is no automatic fallback between the two modes, and the login expires
  after 24 hours with no refresh yet. Workers and the evaluation harness stay
  direct.
- Managed calls can stream. `stream: true` answers with typed `delta`, `result`,
  and `error` events over SSE; abandoning the call cancels the provider request
  so it stops costing tokens. A call refused before any output is still a plain
  HTTP error, and one that fails after its first delta reports the failure as an
  error event. Repeating a `call_id` answers `409` naming the original call
  instead of running it twice. A reverse proxy in front of the server needs
  response buffering off for this route.
- A Compose stack under `deployment/compose/`: MySQL, the server, and the
  Portal, with a script that generates the secrets. `docs/deploy/compose.md`
  walks from nothing to a signed-in Portal.
- Single-use login codes. `buildmax-server user create` and
  `buildmax-server user login-code` let an operator create an account and issue
  a per-account, expiring code, which is how a deployment signs people in
  without a mail channel. Codes are stored hashed and redeemed atomically.

- The Portal is published as a container image,
  `ghcr.io/gougoujiang/buildmax-portal`, tagged with the release it was built
  from. A separate workflow builds it, so a frontend failure cannot hold up the
  binaries.
- `BUILDMAX_API_BASE` configures the Portal image's API URL at container start,
  which is what lets one published image serve any deployment.

- `buildmax init` writes a starter `settings.yaml`, optionally with the API key
  supplied on the command line.
- golangci-lint, govulncheck, and `-race` tests in CI, plus CodeQL analysis for
  Go and TypeScript that starts once the repository is public.
- ESLint for the Portal and desktop frontends, with `npm run lint` in each.
- Open source governance, support, maintainer, and conduct policies.
- Markdown, npm production license, configuration example, Git history secret
  scanning, and release snapshot checks in CI.
- SPDX SBOM generation, GitHub artifact attestations, and release image
  vulnerability scanning.
- Native Linux, macOS, and Windows release archive smoke checks, including
  checksum and required-content validation.
- A public launch checklist and documented issue triage policy.

- Contributor on-ramp documents: `docs/contribute/first-pr.md` walks clone to
  pull request, and `docs/contribute/conventions.md` collects the naming, entity
  ID, tool-output, commit, and changelog rules that review applies. Neither the
  build, the tests, the lint, nor the deployment smokes need a model API key —
  `CONTRIBUTING.md` now says so instead of listing one as a prerequisite.
- `.gitattributes` and `.editorconfig`, so line endings and editor defaults match
  what the toolchains already enforce, and the language statistics describe the
  project rather than its fixtures.
- `.buildmax/README.md` explains why this repository checks in its own workspace
  agent configuration when `.claude/` and `.vibe/` stay ignored.
- A contributor doctor, scoped `check` tasks, Node/npm/Wails version pins,
  fresh-clone CI, Desktop frontend tests, and an implementation-task issue
  template make both human and agent-assisted contributions reproducible.

- An audit trail for sensitive actions, in a new `audit_event` table with an
  owner-only `GET /api/teams/{team_id}/audit-events`. It records logins, team
  membership changes, model catalog changes, and refused team-scoped
  requests — who did what to which object, and nothing else: no prompts, no
  generated content, no tool output, no credentials. Run diagnostics stay in
  the durable run trace and per-call accounting in `llm_call`, because the same
  fact recorded twice gets two retention policies and two chances to disagree.
  A failed audit write is logged and dropped rather than failing the action
  that caused it, so a logging outage does not become an authentication outage;
  the cost is that this records what happened while the database was reachable,
  not a guarantee that every action was recorded.

- An **Audit** tab in space settings, listing the trail for owners: sign-ins,
  membership changes, model changes, and refused requests, newest first with
  paging. Refusals are set apart from the successful actions around them, since
  a refusal is the entry an owner is usually looking for. An action this Portal
  does not recognise is shown verbatim rather than hidden, so a newer server's
  events never silently disappear from the list. Members see the tab and an
  explanation of why the contents are owner-only, rather than a tab that exists
  for some people and not others.

- Portal browser tests, run by `./make e2e` against a deployment that is
  already up. They deliberately do not repeat the API-level deployment smoke,
  which already drives login, team, task, worker, and artifact — what only a
  browser can show is whether the published bundle works against a real server:
  the runtime API base, hash routing, session restoration, and the views that
  exist only in the UI. `deployment-smoke` runs them after the kind stack comes
  up, and uploads a trace on failure. They found the blank-page defect fixed
  below on their first run.

### Changed

- The support matrix gains a compatibility section, and its stale rows are
  corrected. It now states what an upgrade may do to a deployment: the schema
  moves forward only with one release of binary rollback, the HTTP API carries
  no version and may change with a changelog note, configuration is additive
  with removals announced but no deprecation period, and stored data is never
  rewritten or deleted by an upgrade. Where a promise does not exist it says so
  rather than implying one. The audit log, the private Kubernetes reference,
  and worker pod containment are no longer described as missing.

- The frontend toolchain moves to Node 24 and npm 11, from Node 22 and npm 10.
  Node 22 entered maintenance; 24 is the active LTS. This affects `gui/`,
  `portal/`, and `desktop/frontend/` only — normal CLI work still needs no Node
  at all. The three lockfiles were regenerated with npm 11, which changed
  nothing but deduplicating a nested `@eslint/js` copy. `./make doctor` reports
  the versions it wants, and `TestFrontendToolchainPinsAgree` fails if
  `.node-version`, the `packageManager` fields, and the `engines` ranges drift
  apart again.

- `storage.minio` no longer defaults its endpoint, region, and credentials to a
  local MinIO and that server's development user. Those defaults made "unset"
  unreachable, so a deployment that omitted them was silently pointed at
  `localhost:9000` as user `minio` instead of falling through to AWS endpoint
  resolution and the SDK credential chain. A credential should never have a
  default. Nothing in the repository relied on them — Compose uses the local
  filesystem backend and the kind manifest sets all of them explicitly — but a
  deployment that did will now need to state them.

- Full `./make build` is now strict and includes the Portal; frontend or Wails
  failures no longer leave a successful partial build. Portal and Desktop lint
  are zero-warning gates.
- `AGENTS.md` is now a compact, stable navigation and constraint guide backed
  by integrity tests for the repository's `.buildmax` agent configuration.
- Release chores now live under `./make release`, and local image loading is
  `./make kind images`.
- `ROADMAP.md` moved to `docs/ROADMAP.md`, so the repository root keeps only the
  files GitHub and packaging tools expect there.

- **Self-registration is closed by default.** `POST /api/otp/request` refuses
  `intent: signup` with 403 unless `server.yaml` sets `allow_signup: true`.
  Together with `dev_login_otp`, open signup meant anyone who could reach a
  server could create an account and then sign in as it.
- The Portal no longer hard-codes a developer's local kind hostname to pick its
  API URL; the kind manifest sets `BUILDMAX_API_BASE` like any other deployment.
- `buildmax version` falls back to the Go build info, so a binary installed with
  `go install` reports its module version instead of `dev`.
- Startup tells a first-time user what to do: a missing configuration file, a
  file without models, and an unedited placeholder key are now three distinct
  messages instead of one.
- Go 1.26.6, `golang.org/x/net` 0.55.0, `golang.org/x/text` 0.39.0, and
  `goldmark` 1.7.17 — closing 23 vulnerabilities reachable from this code.
- Release tools are pinned to exact versions for reproducible builds.
- Release setup instructions now use the current YAML configuration flow.
- The local kind manifests moved from `setup/` into `deployment/`, next to the
  deployment files they resemble. Their current path is `deployment/kind/`;
  `./make kind up` is unchanged.
- Community health files — Code of Conduct, Support, Governance, Maintainers,
  Trademarks — moved to `.github/`, where GitHub surfaces them exactly as it does
  from the repository root. The root now carries only README, CONTRIBUTING,
  SECURITY, CHANGELOG, ROADMAP, LICENSE, and the agent instructions.
- `AGENTS.md` no longer duplicates the repository tree or detailed project
  conventions. It keeps only a compact command/constraint guide and routes to
  `docs/contribute/repo-layout.md`, `CONTRIBUTING.md`, and the new
  `docs/contribute/conventions.md` for the full sources of truth.

### Removed

- Dead code across the agent runtime, storage, handlers, and CLI that the new
  linter surfaced.

### Fixed

- `LICENSE` is the verbatim Apache License 2.0 again. The copy shipped in
  `0.1.0-alpha` was missing the closing paragraph of section 5, the one that
  keeps a separately executed license agreement in force over the default
  contribution grant — the same grant `CONTRIBUTING.md` relies on to state that
  BuildMax needs no CLA. Only the appendix copyright line is filled in, which
  the license permits, so automated license detection now identifies the
  repository and every release archive as Apache-2.0 rather than as an unknown
  license.

- Portal rendered a blank page. `@buildmax/gui` is a symlinked workspace package
  that externalises React, so its bare `import "react"` resolved from its own
  real path — and `gui` has React installed as a peer. The bundle therefore
  shipped two React instances and every hook threw
  `Cannot read properties of null (reading 'useState')`. Deduplicating React in
  the Portal's Vite config fixes it, and takes 8 kB off the bundle. This was
  invisible to the build, to TypeScript, and to the API-level deployment smoke;
  the browser tests added alongside it are what found it.

## [0.1.0-alpha] - 2026-08-09

### Added

- Initial alpha release of the BuildMax CLI/TUI, server, and worker binaries.
- Linux, macOS, and Windows archives with checksums and third-party notices.
- Multi-architecture Linux container image published to GHCR.

[Unreleased]: https://github.com/icloudbb/buildmax/compare/v0.2.0-alpha.15...HEAD
[0.2.0-alpha.15]: https://github.com/icloudbb/buildmax/compare/v0.2.0-alpha.14...v0.2.0-alpha.15
[0.2.0-alpha.14]: https://github.com/icloudbb/buildmax/compare/v0.2.0-alpha.13...v0.2.0-alpha.14
[0.2.0-alpha.13]: https://github.com/icloudbb/buildmax/compare/v0.2.0-alpha.12...v0.2.0-alpha.13
[0.2.0-alpha.12]: https://github.com/icloudbb/buildmax/compare/v0.2.0-alpha.11...v0.2.0-alpha.12
[0.2.0-alpha.11]: https://github.com/icloudbb/buildmax/compare/v0.2.0-alpha.10...v0.2.0-alpha.11
[0.2.0-alpha.10]: https://github.com/icloudbb/buildmax/compare/v0.2.0-alpha.9...v0.2.0-alpha.10
[0.2.0-alpha.9]: https://github.com/icloudbb/buildmax/compare/v0.2.0-alpha.8...v0.2.0-alpha.9
[0.2.0-alpha.8]: https://github.com/icloudbb/buildmax/compare/v0.2.0-alpha.7...v0.2.0-alpha.8
[0.2.0-alpha.7]: https://github.com/icloudbb/buildmax/compare/v0.2.0-alpha.6...v0.2.0-alpha.7
[0.2.0-alpha.6]: https://github.com/icloudbb/buildmax/compare/v0.2.0-alpha.4...v0.2.0-alpha.6
[0.2.0-alpha.4]: https://github.com/icloudbb/buildmax/compare/v0.2.0-alpha.3...v0.2.0-alpha.4
[0.2.0-alpha.3]: https://github.com/icloudbb/buildmax/compare/v0.2.0-alpha.2...v0.2.0-alpha.3
[0.2.0-alpha.2]: https://github.com/icloudbb/buildmax/compare/v0.2.0-alpha.1...v0.2.0-alpha.2
[0.2.0-alpha.1]: https://github.com/icloudbb/buildmax/compare/v0.1.0-alpha.2...v0.2.0-alpha.1
[0.1.0-alpha.2]: https://github.com/icloudbb/buildmax/compare/v0.1.0-alpha.1...v0.1.0-alpha.2
[0.1.0-alpha.1]: https://github.com/icloudbb/buildmax/compare/v0.1.0-alpha...v0.1.0-alpha.1
[0.1.0-alpha]: https://github.com/icloudbb/buildmax/releases/tag/v0.1.0-alpha
