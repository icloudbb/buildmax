# Agent Browser Capability

> **简体中文：** [阅读中文镜像](../zh-CN/design/Agent 浏览器能力.md)
>
> **Audience:** contributors · **Status:** active plan — partially shipped ·
> **Reviewed:** 2026-09-26
>
> **Shipped:** CLI headless tools (#714, #715), Desktop visible window (#717),
> untrusted-page test (#718), the read-only Desktop live-view tab (#719), and
> per-origin admission with origin-scoped session grants (§3).
> **Remaining:** Linux and Windows portability evidence, workers/Portal/scheduled
> runs (off pending an egress sandbox), interactive takeover and page sharing,
> and a managed browser download (§9).

This record fixes the architecture and trust rules for giving an Agent a real,
controllable browser; §8 records the alternatives weighed and the peer survey.
Related:
[tool architecture](../contribute/architecture/tools.md),
[tool permissions](tool-permissions.md),
[sandbox boundaries](sandbox-boundaries.md),
[agent bridge CLI](agent-bridge-cli.md), and
[Desktop architecture](../contribute/architecture/desktop.md).

## Contents

- [1. Decision and Scope](#1-decision-and-scope)
- [2. Architecture](#2-architecture)
- [3. Tool Contract](#3-tool-contract)
- [4. Browser Discovery and Lifecycle](#4-browser-discovery-and-lifecycle)
- [5. Trust and Failure Boundaries](#5-trust-and-failure-boundaries)
- [6. Verification](#6-verification)
- [7. Desktop Presentation](#7-desktop-presentation)
- [8. Alternatives Considered](#8-alternatives-considered)
- [9. Deferred](#9-deferred)

## 1. Decision and Scope

The essential outcome is that an Agent can **verify a change against the real,
rendered page**: navigate to a route, observe the page and its console, click
and type, and act on what it observed rather than infer success from source or
an HTTP response. `WebFetch` returns static HTML and cannot serve this.

This is a capability of the shared Agent runtime, not of one surface. It first
shipped **headless on the CLI**, driven by a Go-owned Chromium process over the
Chrome DevTools Protocol (CDP). Desktop then adds a **visible window**: it
launches the browser headful so a user can watch the page the Agent drives, and
shows a compact activity indicator linking each session to its current page,
and can render a read-only live view of that page in a workspace tab (§7). User
takeover and page sharing remain deferred. The peer survey (§8) establishes the
workflow's value, so this work proves architecture, portability, and trust — not
demand.

Locked decisions:

- **Browser source:** discover an installed system Chrome/Edge; fail with a
  clear, actionable error when none is found. Managed Chrome for Testing
  downloads and bundling are deferred.
- **Surfaces:** CLI (headless) and Desktop (headful, its own visible window,
  an activity indicator, and a read-only screencast tab). Interactive takeover
  of the page is deferred.
- **Tool contract:** a few focused tools, one verb each — not a single
  action-multiplexing tool, and never one tool per CDP command.
- **CDP client:** [chromedp](https://github.com/chromedp/chromedp) is the
  selected dependency.

## 2. Architecture

The dependency direction is preserved: browser process control is an
infrastructure concern; `agentapp` assembles the optional tools; `core` keeps
its existing tool and message contracts and imports no browser code.

- **Controller (`internal/infra/browser`).** Owns executable discovery, process
  start/stop, the isolated user-data directory, the CDP transport, and the map
  from BuildMax Session ID to a browser context and its pages. It is the
  authoritative state regardless of which surface drove the run. Browser launch
  is lazy: no process starts until the first browser tool call.
- **Port (`internal/tool`).** The tool package defines a `BrowserController`
  interface it depends on, the way it already defines `ArtifactPublisher`.
  `internal/infra/browser` implements it. This keeps `core`/`tool` free of a
  concrete CDP dependency and lets tests substitute a fake.
- **Assembly.** The controller flows through the existing optional-capability
  funnel: a new field on `AppConfig` (`internal/agentapp/app.go`), copied in
  `buildAgentApp` (`internal/agentapp/app_builder.go`), passed to
  `buildToolRegistry` and into `buildBaseTools`
  (`internal/agentapp/assembly.go`). A nil controller means the browser tools
  are **omitted entirely**, following the `publisher != nil` precedent — a tool
  that only answers "unavailable" costs a round trip and teaches the model
  nothing.
- **Surface enablement is policy, separate from the capability.** The CLI
  interactive and print configs pass a real controller. The unattended worker
  config (`UnattendedWorker: true`) and delegate/subagent workspaces pass nil:
  workers stay off pending the egress-sandbox story (section 5), and subagents
  follow the same rule as background jobs — the primary run holds the
  capability.
- **Lifecycle.** `AgentApp.Close` closes the controller alongside jobs and
  worktrees, releasing every browser process and temporary profile when the
  runtime closes.

## 3. Tool Contract

A small, explicit set covers the first verification journey. Each is a normal
`llm.Tool`; the screenshot tool additionally implements `llm.MultimodalTool`.
Names are constants in `internal/tool/names.go` and must appear in
[tool architecture](../contribute/architecture/tools.md); user-facing entries
also go in `manual/tools.md`.

| Tool | Purpose | Result |
|---|---|---|
| `BrowserNavigate` | Go to an approved HTTP(S) origin | Final URL, title, HTTP status, and load outcome |
| `BrowserSnapshot` | Bounded accessibility/DOM snapshot with stable element references local to one page revision | Text snapshot plus URL, title, viewport, and revision |
| `BrowserClick` | Click a referenced element | New observation, or a stale-reference error after navigation/DOM replacement |
| `BrowserType` | Type into a referenced element | As above |
| `BrowserScreenshot` | Capture the current page | `ToolResult` with a text line and an image `ContentPart` (base64), following the MCP gateway's `formatCallToolContent` template |
| `BrowserConsole` | Recent console errors | Bounded, redacted excerpt |

Every call:

- reads `session.SessionIDFromContext` and operates only on that session's
  page; a call carrying no matching page is an error, never another session's
  page;
- rejects a stale element reference rather than guessing a target;
- bounds page text and console excerpts before they enter model context and
  traces;
- honors context cancellation and a per-call timeout.

Interaction tools declare `AccessWrite`; observation tools declare
`AccessReadOnly`, so interaction goes through the ordinary approval path: an
interactive session asks before `BrowserNavigate`, `BrowserClick`, or
`BrowserType`, and the `BrowserNavigate` prompt shows the requested URL.

**Origin admission.** `BrowserNavigate` is the one place an origin is admitted,
and it names the exact origin:

- `tool.BrowserOrigin` is the single definition of an origin: an absolute
  `http`/`https` URL with a host, serialized as a browser does — lowercase
  scheme and host, default port omitted, credentials, path, query, and fragment
  dropped. Every other scheme is rejected before anything loads.
- `BrowserNavigate` implements `llm.GrantScoper` with that origin, so "allow for
  session" covers exactly one origin: `http://localhost:3000/a` and
  `HTTP://LocalHost:3000/b` share a grant; `http://localhost:3001`,
  `https://localhost:3000`, and `http://127.0.0.1:3000` each ask again. The
  approval prompt in the TUI and Desktop names the origin the grant would cover,
  and a `settings.yaml` rule can name one too (`BrowserNavigate:<origin>`).
- The controller records the requested origin as the session page's admitted
  origin. A server redirect elsewhere is not admitted: the prompt never showed
  it.

**Interaction is confined to the admitted origin.** A click or form submission
on an approved page can navigate anywhere. The controller refuses `BrowserClick`
and `BrowserType` whenever the page is not on the admitted origin, and the
refusal tells the model to call `BrowserNavigate` with the page's URL — which
puts the new origin through its own per-origin approval. Every result that
reports page state (navigate, snapshot, click, type, screenshot) says when the
page has left the admitted origin. The check runs twice: against the URL the
controller last observed, and inside the click/type script against the live
`location.origin`, so a page that navigated itself after the snapshot — possibly
to a document planting a matching `data-bm-ref` — is never acted on.

This confines interaction rather than scoping `BrowserClick`/`BrowserType`
grants by origin. A scoped click grant would need the session's current page
inside `GrantScope`, which sees only arguments, and its prompt would show a
bare element reference; routing every new origin back through
`BrowserNavigate` reuses the one prompt that already shows a URL, and adds no
second grant key. Two limits are deliberate: the navigation that leaves the
origin has already happened when it is reported — blocking it would need
request interception — and observation of the new page is still allowed, since
page content is data (§5).

## 4. Browser Discovery and Lifecycle

- **Discovery.** Look for a system Chrome or Chromium, then Edge, at the
  conventional per-OS locations (macOS app bundles, Windows registry/Program
  Files paths, Linux `PATH` and known package paths). Resolve once per
  controller. When none is found, return one clear error naming what was
  searched and how to install or point at a browser; never silently degrade to
  a different automation path.
- **Profile.** Always launch with a fresh, isolated user-data directory under a
  temporary location, never the user's default Chrome profile (Chrome 136+
  blocks remote debugging of the default directory). Remove it on close.
- **CDP transport.** Prefer chromedp's pipe transport; if a port is used, bind
  loopback only, choose a fresh port, and keep the address out of pages and
  traces. Close it with the process.
- **State lifetime.** Browser state is ephemeral per Session in the first slice:
  no persistence across turns or restarts, and no second durable session store.

## 5. Trust and Failure Boundaries

- **Page and session ownership** is keyed by the run's Session ID at every
  operation, not by any frontend's active state.
- **Agent input is untrusted data.** Page text, ARIA names, console output, and
  screenshots may contain instructions; the Agent must not treat them as user
  instructions. Consequential actions need a separate user decision.
- **No app-internal reach.** The controller returns only bounded results; a page
  never receives process bindings, credentials, or CDP transport details. (On a
  future Desktop presentation layer this extends to Wails bindings and the auth
  token.)
- **Network reach is not a sandbox.** A loaded page issues subresource requests
  with the local machine's network access. On local runs this egress is accepted
  under the user's own authority, consistent with the local CLI trust model in
  [agent bridge CLI](agent-bridge-cli.md). It is **not** worker-grade isolation,
  which is exactly why workers, Portal, and scheduled runs stay off in this
  slice.
- **Disabled in the first tool contract:** arbitrary JavaScript evaluation, raw
  CDP access, file upload/download, and `file:`, `javascript:`, `data:`, and
  browser-internal navigation. Each would need its own file and approval
  boundary if ever enabled.
- **Failure is a diagnosable tool error.** A crash, disconnected CDP session,
  blocked site, timeout, or stale element returns a meaningful error. Shutdown
  and cancellation release processes and temporary profiles; the controller
  never silently reconnects to a different browser or profile.

## 6. Verification

- Go unit tests for executable discovery, session-scoped ownership (a
  cross-session call is rejected), URL-scheme admission, per-origin session
  grants through the real agent loop, origin-confined interaction (including a
  real-browser case for a page that redirects itself after the snapshot),
  stale-reference rejection, and cleanup on cancel/close, using a fake
  `BrowserController` where a real browser is not needed.
- A real-browser local integration journey behind a skip when no Chrome/Edge is
  present: serve a small local app with a stateful form, a route change, and a
  deliberate console error; the Agent opens the route, observes, types and
  clicks, reports state/URL/console error, then the run is cancelled and cleanup
  is confirmed.
- A malicious-page-instruction test proving page content is handled as data,
  not authority.
- Operations evidence across macOS, Windows, and Linux: discovery, startup
  time, memory, and failure messages. A single-platform result is evidence, not
  proof of portable delivery.
- Select actual commands from [testing guidance](../contribute/testing.md) and
  `./make help`. Adding chromedp requires a clean `go mod tidy`, a passing
  `go-licenses check`, and a regenerated `NOTICE-THIRD-PARTY`.

## 7. Desktop Presentation

Desktop enables the capability headful, so the browser is its own visible OS
window the user can watch. The controller takes an optional `Observer` (set via
`AppConfig.BrowserObserver`, wired only by Desktop) and reports a page `Event`
on navigation and on close. Desktop forwards each as a `desktop/browser/state`
Wails event, and the frontend shows a compact status-bar indicator of the page
each session is on, cleared when the page is released. The event carries only
URL, title, session, and a closed flag — no page content, and untrusted pages
never reach the Go↔frontend bridge. The CLI sets no observer and stays headless.

Desktop can also embed a **live view** of the page inside a workspace tab. Wails
v2 cannot host the real Chromium page natively, so rather than a second native
engine, the controller drives a CDP screencast: when a frame observer is present
(`AppConfig.BrowserFrameObserver`, Desktop only), navigation starts
`Page.startScreencast`, and each JPEG frame flows through the observer to a
`desktop/browser/frame` Wails event that a `browser` tab renders. This shows the
*same* page CDP controls — the invariant a static screenshot could not meet —
without a native embed. The view is read-only in this slice (frames out, no
input in); frames carry only the image and size, never bindings or page scripts.
The CLI sets no frame observer, so no screencast runs there.

## 8. Alternatives Considered

| Option | Why it was not chosen |
|---|---|
| Go-owned Chromium over CDP | **Chosen.** One real page any local surface can drive; Go owns session mapping, tools, and lifecycle with no Node runtime; Desktop can show the same page. |
| Wails WebView or a React `iframe` | Not a general browser: sites refuse framing with CSP [`frame-ancestors`][mdn-frame-ancestors], an `iframe` has no Agent controller, each platform's native WebView needs its own automation path ([Wails][wails-intro] reuses the OS WebView; its [window runtime][wails-window] targets the app window, not a CDP browser target), and untrusted content would sit next to the Wails Go bindings. |
| An existing browser MCP server | Could feel out tool ergonomics through the MCP gateway, but BuildMax would not own profile isolation, lifecycle, or a consistent approval UX, and peers already prove the workflow's value. |
| Native embedded Chromium or an Electron/CEF shell | Closest to a VS Code-style shared in-app page, at the cost of shell integration, process model, binary size, and packaging. Reconsidered only if the read-only screencast (§7) proves insufficient. |
| Chrome extension in the user's browser | Reaches existing authenticated tabs, but needs extension permissions, distribution, native messaging, and a much broader personal-data grant. A separate later capability, not required to verify a local app. |

**Peer survey (checked 2026-09-21).** Comparable products establish that
"the Agent acts on the observed rendered page" is valuable and shipped; they
differ in browser host. Codex in the ChatGPT desktop app has a built-in browser
with a profile separate from the user's, unavailable in Codex CLI
([OpenAI][openai-browser]). Claude Code operates Chrome through the Claude in
Chrome extension, which declares the `debugger` permission — Chrome's CDP
transport ([Anthropic][claude-chrome], [Chrome][chrome-debugger]). VS Code's
integrated browser is an Electron `WebContentsView` whose main process owns
pages while a shared process runs Playwright through a CDP proxy
([architecture source][vscode-architecture], [user documentation][vscode-tools]);
its user/Agent shared-tab model exists for human co-viewing, which BuildMax did
not make a first-slice requirement, so it is prior art rather than the template.
The common pattern — a real engine owns rendered state, a controller exposes
bounded navigation, observation, and interaction, and any UI presents the same
page — is what the chosen design follows.

[mdn-frame-ancestors]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Content-Security-Policy/frame-ancestors
[wails-intro]: https://v2.wails.io/docs/introduction/
[wails-window]: https://v2.wails.io/docs/reference/runtime/window/
[openai-browser]: https://learn.chatgpt.com/docs/browser
[claude-chrome]: https://support.claude.com/en/articles/12012173-get-started-with-claude-in-chrome
[chrome-debugger]: https://developer.chrome.com/docs/extensions/reference/api/debugger
[vscode-architecture]: https://github.com/microsoft/vscode/blob/main/.github/skills/integrated-browser/SKILL.md
[vscode-tools]: https://code.visualstudio.com/docs/agents/run/browser-tools

## 9. Deferred

Explicitly out of scope, each its own later decision: **interactive** takeover of
the embedded view (forwarding input over CDP) and page sharing, blocking a
cross-origin navigation before it loads (request interception; §3 confines only
interaction), Linux and Windows operations evidence (§6), a native
embedded engine (CEF/Electron) should the read-only screencast prove
insufficient, managed Chrome for Testing or a bundled browser, enabling the
capability for workers/Portal/scheduled runs, subagent browser access,
uploads/downloads, arbitrary JavaScript, and authenticated third-party-site
operation. User-facing behavior and settings move to the manual/reference
documentation as each ships.
