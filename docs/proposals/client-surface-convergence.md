# Client Surface Convergence Across Desktop, Web, and Mobile

> **简体中文：** [阅读中文镜像](../zh-CN/proposals/client-surface-convergence.md)
>
> **Audience:** contributors, product designers, and early adopters · **Status:** proposal — under discussion
>
> **Opened:** 2026-09-20

Related: [roadmap](../ROADMAP.md), [surface positioning](../design/surface-positioning.md),
[client modes](../design/client-modes.md),
[Desktop architecture](../contribute/architecture/desktop.md),
[Server architecture](../contribute/architecture/server.md),
[Remote Control](../design/remote-control.md), and
[client sessions and API credentials](client-sessions-and-api-credentials.md).

## Contents

- [1. Summary](#1-summary)
- [2. The Prompting Question](#2-the-prompting-question)
- [3. Problem and Current Context](#3-problem-and-current-context)
- [4. Why a Framework Swap Does Not Reach the Goal](#4-why-a-framework-swap-does-not-reach-the-goal)
- [5. What Cloud, Web, and Mobile Actually Require](#5-what-cloud-web-and-mobile-actually-require)
- [6. Goals](#6-goals)
- [7. Non-Goals](#7-non-goals)
- [8. The Convergence Model](#8-the-convergence-model)
- [9. The Switchable Data Layer](#9-the-switchable-data-layer)
- [10. The Environment Plane and the Desktop-in-Cloud Shape](#10-the-environment-plane-and-the-desktop-in-cloud-shape)
- [11. Mobile as a Thin Client](#11-mobile-as-a-thin-client)
- [12. Options and Trade-Offs](#12-options-and-trade-offs)
- [13. Open Questions and Evidence Needed](#13-open-questions-and-evidence-needed)
- [14. Likely Destination if Accepted](#14-likely-destination-if-accepted)

## 1. Summary

BuildMax ships two React clients today: Desktop, a Wails application whose
frontend talks to a local Go backend over in-process IPC, and Portal, a browser
application that talks to `buildmax-server` over HTTP and WebSocket. They already
share presentation through `@buildmax/gui` but nothing else.

Two adjacent asks have surfaced: run the Desktop experience in the cloud and let
users reach it from a browser, and reach BuildMax from a phone. A natural first
instinct is to reconsider the desktop shell — for example, migrating from Wails
to Tauri, which is understood to support mobile. This paper argues that the
desktop-shell choice is the wrong lever for both asks. Wails and Tauri are the
same architectural category — a local backend behind a native WebView bound by
in-process IPC — and neither exposes its backend to a remote browser. The lever
that reaches cloud, web, and mobile is a network API plus a web frontend, which
BuildMax already has in Portal and `buildmax-server`. The proposal is to make
the client's *data layer* the seam: one shared UI over a data interface that can
speak either local IPC or the remote HTTP/WebSocket API, so the same surface
serves local-native, cloud-web, and thin-mobile modes without a second UI and
without changing the desktop framework. The clearest instance of the cloud-web
mode is the **Desktop-in-Cloud** shape (§10): a long-running, Space-scoped
environment that serves the Desktop experience over the web, managed within
Portal but running on a separate execution plane from its task-centric
Task/TaskRun model.

## 2. The Prompting Question

The question was concrete: Tauri's architecture differs from the Wails setup
BuildMax uses; if we want Desktop to run independently in the cloud and be
reached over the web, does Tauri have an advantage? A follow-up added that Tauri
is understood to have stronger mobile support.

Both observations are partly true as framework facts. Tauri and Wails are not
identical, and Tauri 2.0 does ship first-class iOS and Android targets that Wails
does not. The paper's claim is narrower and, it argues, decisive: neither fact
changes what is needed to run in the cloud, serve a browser, or serve a phone for
*this* codebase. Recording the reasoning here prevents the framework question
from being reopened without new evidence.

## 3. Problem and Current Context

The two clients are structurally different below the shared UI:

- **Desktop is Wails with in-process IPC, not HTTP.** The binary entry is
  `cmd/buildmax-desktop/main.go`; `wails.Run` binds the whole `App` struct
  (`internal/interface/desktop/run.go`), so the frontend calls exported Go
  methods such as `SendMessageStream` and `ListProjects`
  (`internal/interface/desktop/app.go`) through the injected `window.go` object,
  and Go pushes streams back with `runtime.EventsEmit`. The UI assets are
  `//go:embed`-compiled and served by the WebView's in-process asset server; the
  Desktop process opens no locally reachable HTTP port. Its only outbound
  networking is as a client of a managed server (login, managed-server URL).
- **Portal is a browser client over HTTP and WebSocket.** `buildmax-server`
  (`cmd/buildmax-server/main.go`) runs a standard `http.Server`
  (`internal/server/server.go`) with routes in
  `internal/server/handlers/routes.go`, including the per-space WebSocket
  upgrade. Portal reaches it through `portal/src/lib/api/*`.
- **Only presentation is shared.** `@buildmax/gui` (`gui/`) is a presentational
  React component and theme package consumed by both apps via a `file:`
  dependency. It contains no data, network, or auth logic; each app owns its own
  data layer — Desktop over `window.go`/`window.runtime`, Portal over
  `lib/api/*`.

So the capability that "run in the cloud, reach it from a browser" needs — a
frontend served to an arbitrary remote browser talking to a backend over the
network — exists in exactly one place today (Portal + server) and is absent from
Desktop by construction.

## 4. Why a Framework Swap Does Not Reach the Goal

Wails and Tauri share one architecture: a local backend, a native OS WebView,
and an IPC bridge injected into that WebView. In Wails the bridge is `window.go`
plus `runtime` events; in Tauri it is `invoke` plus events. The bridge is
injected into the *local* WebView only and does not traverse the network. Pointing
either shell at a remote URL merely loads a remote page into the local WebView;
it does not make the backend's commands reachable by a separate remote browser.
Therefore Tauri, like Wails, has no native path to "backend in the cloud, UI in a
user's browser." A migration would spend a large, cross-cutting effort and leave
the actual goal exactly where it started.

For BuildMax specifically the swap is worse than neutral. The backend is Go
end-to-end, and AGENTS.md fixes Go as the primary implementation language with a
single-binary CLI/TUI and no Node. Wails keeps Desktop-to-Server in one Go stack.
Tauri's core is Rust; adopting it forks the backend language and splits logic
away from `internal/*`, which is a cost the cloud/web/mobile goals never repay
because none of them run on the desktop shell at all.

## 5. What Cloud, Web, and Mobile Actually Require

All three asks reduce to the same requirement: a network API (HTTP/WebSocket)
plus a frontend that speaks it.

- **Cloud + web** is the Portal shape by definition: the backend runs on a
  server, the browser is the client.
- **Mobile**, for a product whose heavy work is a Go agent runtime — tool loop,
  sandbox, subprocesses, local Git operations — is almost certainly a *thin
  client*. A phone is not where that runtime runs; the realistic phone use is
  observe-and-lightly-act (read tasks and issues, approve, message an agent,
  watch a run stream), with the runtime executing server-side. A thin client
  again needs only the network API.

The desktop shell is orthogonal to all three. What varies across surfaces is the
data layer's transport, not the UI and not the framework.

## 6. Goals

- One React UI, built on `@buildmax/gui`, that renders the same core surfaces
  across local-native (Desktop), cloud-web (Portal), and thin-mobile modes.
- A single client-side data interface with two interchangeable implementations —
  local IPC and remote HTTP/WebSocket — chosen at runtime by mode.
- Preserve Wails for what only a local shell can do: local project and
  filesystem access, the local terminal, OS integration.
- Make "run in the cloud, reach from a browser" a natural extension of the
  existing Portal + `buildmax-server` path rather than new infrastructure.
- Name and enable the **Desktop-in-Cloud** shape (§10) as a separate
  **Environment plane**: a long-running, Space-scoped machine that serves the
  Desktop experience over the web on its own workspace, managed within Portal —
  reusing its authentication, Space authorization, and plugin distribution — but
  distinct from the Task/TaskRun execution model, with its own provisioning,
  lease, resource management, and reclamation.
- Give mobile a clear, low-cost path (responsive Portal, then PWA, then an
  optional thin native wrapper) that reuses the same UI and data interface.

## 7. Non-Goals

- Migrating the desktop shell to Tauri or any other framework.
- Running the full agent runtime — sandbox, tool loop, subprocess execution — on
  a phone or inside a remote browser tab.
- Exposing the Desktop process itself to remote browsers, or turning Wails into a
  network server.
- Merging Portal and Desktop into one deployable; they stay distinct clients that
  converge on shared UI and a shared data contract, per
  [surface positioning](../design/surface-positioning.md).
- Changing the authentication or authorization model; this paper assumes the
  existing session and credential work (see
  [client sessions and API credentials](client-sessions-and-api-credentials.md)).

## 8. The Convergence Model

Three layers, with the seam drawn deliberately:

1. **Presentation** — `@buildmax/gui` and app-level composed views. Shared across
   every surface. Already presentational-only, so it is the natural common layer.
2. **Data layer** — a client-side interface (list projects, send a message and
   receive a stream, respond to an approval, cancel a run, and so on) with two
   implementations: an IPC adapter over `window.go`/`window.runtime` for the
   local Wails shell, and an HTTP/WebSocket adapter over the server API for
   browser and mobile. This is the new seam.
3. **Shell** — Wails for local-native; the browser for web; an optional thin
   native wrapper (Capacitor or React Native) for mobile. The shell chooses which
   data-layer implementation is active and supplies only shell-specific
   capabilities.

The same UI then runs in three modes without a second codebase: local Wails
WebView over IPC; the same UI over HTTP/WebSocket to a managed server (which is
what "Desktop in the cloud, reached from a browser" means concretely — it is
Portal); and a thin mobile client over the same API.

## 9. The Switchable Data Layer

Today the coupling to Wails lives entirely in Desktop's data layer: the frontend
reads `window.go`/`window.runtime` directly. The work is to define one data
interface the UI depends on, then provide the IPC and HTTP/WebSocket adapters
behind it. Portal's `lib/api/*` is already the second adapter in all but name;
the task is to reconcile the two behind a shared contract rather than to invent a
transport.

Two shapes need care because IPC and HTTP differ in nature:

- **Streaming.** Desktop uses Wails events (`runtime.EventsEmit`); Portal uses a
  WebSocket. The interface should express "subscribe to a run's stream" so each
  adapter satisfies it with its native mechanism.
- **Request/response and errors.** Method-call semantics over IPC and
  request/response over HTTP must present one error and result shape to the UI, so
  the UI does not branch on transport.

This is an incremental refactor of an existing seam, not a rewrite, and it does
not require touching the Go backend beyond ensuring the server API covers the
surfaces the shared UI needs.

## 10. The Environment Plane and the Desktop-in-Cloud Shape

The convergence model makes one flagship shape concrete and worth naming as a
target: a **personal cloud machine**. A user reaches a long-running remote
environment from a browser and gets a Desktop-equivalent experience running on
it: projects, chat sessions, the terminal, file and diff views, approvals, and
live run streams. It is the Codespaces/Gitpod pattern applied to BuildMax — the
agent runtime and the workspace live on the machine, and the browser is a thin
client over the HTTP/WebSocket data adapter of §9.

What makes it a composition of existing parts rather than a new product:

- The runtime is already shared — `internal/agentapp` assembles the same models,
  tools, MCP, hooks, sandbox, traces, and sessions for CLI, Desktop, evaluation,
  and workers, so running it on a long-running environment reuses it unchanged.
- The UI is already shared through `@buildmax/gui`; the Desktop React surface runs
  in the browser over the HTTP/WebSocket adapter.
- The transport already exists in Portal + `buildmax-server`; the terminal maps
  naturally onto a WebSocket, and the shipped Desktop terminal PTY manager
  (`internal/interface/desktop/terminal.go`) is its seed.

**This is a second execution plane, not the Task plane.** Task plus TaskRun is a
*bounded-turn* model: a run materializes the Space's files into a run-scoped
`workspace/`, executes one turn or attempt, records an authoritative result, and
is torn down, with state crossing runs through checkpoints. A long-running
environment is the opposite in kind — a persistent, interactively used machine
with a live terminal, in-flight files, and background processes that outlive any
single turn. Forcing it into TaskRun would distort both. It is a distinct
**Environment plane** with its own resource, lifecycle, and management surface,
and it must not inherit the task-centric execution model.

**Portal unifies management, not execution.** The Environment plane reuses
Portal's existing control surfaces unchanged:

- **Authentication and authorization** — the same JWT and single-use login codes,
  and **Space as the authorization boundary**: an environment is a Space-scoped
  resource, so who may provision, reach, or destroy it follows Space membership
  and roles, exactly like Tasks, Artifacts, and Workflows.
- **Plugin distribution** — the same server-resolved, Space-explicit activation
  workers already receive, into a run-scoped `BUILDMAX_HOME`.

It then adds what the task-centric model has no concept of, and this is where the
new design work lives:

- **Provisioning / request** — a member requests an environment; the server
  allocates the container, its workspace, and a Space-scoped `BUILDMAX_HOME`.
- **Lease** — the environment is held under a lease with an idle timeout, renewed
  by use, so "long-running" does not mean "forever-running."
- **Resource management** — CPU, memory, and storage bounds plus per-Space quotas,
  because an idle environment still costs, unlike an ephemeral Job.
- **Reclamation** — hibernate on idle, stop on lease expiry, and a defined answer
  for what persists across hibernate and restart versus what is lost on
  reclamation.

Two properties define the shape and must be decided deliberately:

- **The workspace is the machine's, not the user's laptop.** Projects live on that
  machine, cloned or mounted there; this is Codespaces semantics, and local-only
  OS integration does not carry over. It is a defining feature of the shape, not a
  gap, but it must be stated so "same as Desktop" is not read as "your local
  files."
- **Network exposure changes the trust posture.** A network-reachable agent has
  shell and filesystem access, so the Bash sandbox should default on with
  fail-closed enforcement, matching the worker posture rather than the local-CLI
  default. See [agent sandbox policy](../design/agent-sandbox-policy.md).

**It is distinct from Portal's task-centric model, not from Portal.** The
Environment plane and the Task plane share authentication, Space authorization,
plugin distribution, the runtime, the UI, and the transport; they do not share the
execution model or its resource, and neither subsumes the other. A standalone
single-tenant server built on the same `agentapp` runtime remains a possible
packaging of the same shape, but hosting the Environment plane inside Portal is
preferred: it reuses the multi-tenant control surfaces above instead of
reinventing them.

Since this proposal opened, [Remote Control](../design/remote-control.md) has
been accepted and its first phases shipped. It places itself and this
Environment plane on one grid of runtime host by interaction surface, and builds
the narrow-surface, local-host quadrant -- steering a session on the user's
machine through the server -- without any of the environment substrate above.

## 11. Mobile as a Thin Client

Given a network API, mobile has a graduated path, cheapest first, each step
reusing the shared UI and data layer:

1. **Responsive Portal.** Portal is already a browser app; making its core
   surfaces mobile-responsive makes them usable in a phone browser with no new
   framework.
2. **PWA.** Add a manifest and service worker for installability, an offline
   shell, and push — a near-native feel without an app store.
3. **Thin native wrapper.** For store distribution or deeper native capabilities
   (push, biometrics, deep links), wrap the web UI with Capacitor or React
   Native, reusing `@buildmax/gui` and the HTTP/WebSocket adapter.

Tauri's genuine mobile strength does not help here: it serves teams running a
Rust application on the device, whereas BuildMax's phone surface is a thin client
over a Go server. The mobile advantage lands outside this codebase's shape.

## 12. Options and Trade-Offs

- **A — Converge on a switchable data layer (this proposal).** One UI, two data
  adapters, Wails retained. Highest reuse; the cloud/web path already exists;
  mobile is incremental. Cost: designing one data interface that fits both IPC and
  HTTP cleanly, and keeping the server API complete enough for the shared UI.
- **B — Migrate Desktop to Tauri.** Large cross-cutting effort; forks the backend
  into Rust against the Go single-stack principle; still yields no remote-browser
  or thin-mobile capability. Rejected as not addressing the goal.
- **C — Keep Desktop and Portal fully separate, build a third mobile client.**
  Three UIs and three data layers to maintain; presentation and behavior drift.
  Highest long-run cost, weakest coherence.
- **D — Do nothing.** Portal already serves the web today; the cloud/web ask is
  partly met and mobile is a phone-browser experience only. Lowest cost now, but
  Desktop's Wails coupling stays load-bearing and every cross-surface change pays
  the split twice.

## 13. Open Questions and Evidence Needed

- Does the current server API already cover every surface the shared UI needs, or
  are there Desktop-only capabilities (local project, filesystem, terminal) that
  must stay IPC-only by design? Enumerate the gap.
- What is the right shape for the shared streaming abstraction so one interface
  maps cleanly onto both Wails events and a WebSocket without leaking either?
- Which mobile step (responsive Portal, PWA, native wrapper) does the earliest
  real user need justify, and what evidence would move the decision past
  responsive Portal?
- §10 establishes the Environment plane as separate from the Task plane and
  managed within Portal; what stays open is the concrete policy — default lease
  and idle-hibernation windows, per-Space quotas, exactly what persists across
  hibernate and reclamation, how the workspace is provisioned (clone on start,
  mount, or attach an existing repository), and the sandbox defaults for an
  interactively reachable environment.
- What is the smallest slice that proves the switchable data layer — for
  instance, one surface rendered from `@buildmax/gui` running unchanged over both
  the IPC adapter and the HTTP adapter?

## 14. Likely Destination if Accepted

If accepted, the data-layer seam and the surface convergence become a
[design record](../design/README.md) — most naturally an extension of
[surface positioning](../design/surface-positioning.md) and
[client modes](../design/client-modes.md) — and the incremental refactor plus the
mobile path enter [ROADMAP.md](../ROADMAP.md) as decomposed
[backlog](../backlog/README.md) items. The decision that a desktop-framework
migration is not the lever is recorded there so it is not relitigated without new
evidence. This proposal is then deleted.
