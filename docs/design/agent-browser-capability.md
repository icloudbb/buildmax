# Agent Browser Capability

> **简体中文：** [阅读中文镜像](../zh-CN/design/Agent 浏览器能力.md)
>
> **Audience:** contributors · **Status:** active plan · **Reviewed:** 2026-09-22

This record fixes the architecture and trust rules for giving an Agent a real,
controllable browser. It is the decision that follows the investigation in
[browser capability proposal](../proposals/browser-capability.md); read that for
the alternatives weighed and the peer survey. Related:
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
- [7. Deferred](#7-deferred)

## 1. Decision and Scope

The essential outcome is that an Agent can **verify a change against the real,
rendered page**: navigate to a route, observe the page and its console, click
and type, and act on what it observed rather than infer success from source or
an HTTP response. `WebFetch` returns static HTML and cannot serve this.

This is a capability of the shared Agent runtime, not of one surface. The first
slice delivers it **headless on the CLI**, driven by a Go-owned Chromium process
over the Chrome DevTools Protocol (CDP). No visible window, no frontend work,
and no user takeover are part of this slice; those are a later, optional Desktop
enhancement. The peer survey in the proposal establishes the workflow's value,
so this work proves architecture, portability, and trust — not demand.

Locked decisions:

- **Browser source:** discover an installed system Chrome/Edge; fail with a
  clear, actionable error when none is found. Managed Chrome for Testing
  downloads and bundling are deferred.
- **First surface:** CLI, headless. Desktop presentation is deferred.
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
- checks the current origin against policy before acting, and rejects a stale
  element reference rather than guessing a target;
- bounds page text and console excerpts before they enter model context and
  traces;
- honors context cancellation and a per-call timeout.

Interaction tools declare `AccessWrite`; observation tools declare
`AccessReadOnly`. Origin admission uses the existing approval path and must
name the exact origin, action, and current page — a session grant for one
origin never authorizes another.

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
  cross-session call is rejected), origin admission, stale-reference rejection,
  and cleanup on cancel/close, using a fake `BrowserController` where a real
  browser is not needed.
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

## 7. Deferred

Explicitly out of the first slice, each its own later decision: a visible
Desktop window and the Go↔frontend presentation layer (the `terminalManager`
pattern), user takeover and page sharing, managed Chrome for Testing or a
bundled browser, enabling the capability for workers/Portal/scheduled runs,
subagent browser access, uploads/downloads, arbitrary JavaScript, and
authenticated third-party-site operation. User-facing behavior and settings
move to the manual/reference documentation as each ships.
