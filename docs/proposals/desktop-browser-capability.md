# Desktop Browser Capability: Technical Investigation

> **简体中文：** [阅读中文镜像](../zh-CN/proposals/desktop-browser-capability.md)
>
> **Audience:** contributors and product designers · **Status:** proposal — under discussion
>
> **Opened:** 2026-09-21 · **Evidence reviewed:** 2026-09-21

Related: [roadmap](../ROADMAP.md), [current state](../current-state.md),
[Desktop architecture](../contribute/architecture/desktop.md),
[tool architecture](../contribute/architecture/tools.md),
[tool permissions](../design/tool-permissions.md),
[sandbox boundaries](../design/sandbox-boundaries.md), and
[Desktop workspace tabs](desktop-workspace-tabs.md).

## Contents

- [1. Finding and Decision Boundary](#1-finding-and-decision-boundary)
- [2. User Outcome and Scope](#2-user-outcome-and-scope)
- [3. How Comparable Products Work](#3-how-comparable-products-work)
- [4. BuildMax Baseline](#4-buildmax-baseline)
- [5. Technical Options](#5-technical-options)
- [6. Candidate Architecture](#6-candidate-architecture)
- [7. Trust and Failure Boundaries](#7-trust-and-failure-boundaries)
- [8. Proposed Experiment and Evidence](#8-proposed-experiment-and-evidence)
- [9. Open Questions and Likely Destination](#9-open-questions-and-likely-destination)
- [10. External Sources](#10-external-sources)

## 1. Finding and Decision Boundary

BuildMax already fetches and searches web content. The missing capability is a
**shared, rendered page** that a Desktop user can watch and an Agent can inspect
and operate during the same run. The first valuable outcome is a local web-app
verification loop: start the app, open its route, perform a short user journey,
read the result and console errors, fix a defect, and repeat the journey.

The candidate first implementation is a Desktop-owned, visible Chromium process
with an isolated profile, driven from Go over the Chrome DevTools Protocol
(CDP). It can initially appear as a separate browser window. That proves the
Agent/browser contract without changing the Desktop shell. Rendering the *same
controllable page* inside a Desktop workspace tab is a separate product and
native-view decision. This is an investigation and candidate direction, not an
accepted architecture or a roadmap commitment.

The decision to make now is whether the local verification journey warrants a
Desktop-owned browser at all. The decision to make **after a working prototype**
is whether a browser inside the workspace is valuable enough to justify a
second native view, browser-engine packaging, focus/layout work, and its larger
security surface. No peer implementation proves that Wails v2 supplies that
second step for BuildMax out of the box.

## 2. User Outcome and Scope

### Essential outcome

A user asks the Desktop Agent to verify a change in a running local web app.
They can see the page the Agent uses, take over to inspect it, and receive a
report of the exact route, interactions, observed result, and unresolved limits.
The Agent must act on page state it actually observed rather than infer success
from source code or an HTTP response.

The comparable workflows are documented by [VS Code browser tools][vscode-tools]
and [Claude Code with Claude in Chrome][claude-chrome]. This is evidence of a
useful workflow, not evidence that BuildMax users require the same full browser
product. A bounded local prototype should test that assumption.

### Goals for a first slice

- Navigate to `http://localhost` or another explicitly approved HTTP(S) origin.
- Return a bounded, current page snapshot with identifiable interactive
  elements; click and type against those elements, rejecting stale references.
- Capture a screenshot and recent console errors from the same page.
- Let the user watch and intervene in the visible browser window.
- Keep browser state separate from the user's normal browser profile and from
  other BuildMax sessions.
- Stop operations promptly on run cancellation and close owned resources on
  Desktop shutdown.

### Non-goals for that slice

- Automating the user's ordinary Chrome profile or silently inheriting its
  cookies, passwords, history, and tabs.
- Arbitrary JavaScript evaluation, raw CDP access, file upload/download, or
  unrestricted authenticated transactions as Agent tools.
- A general web-research replacement for `WebSearch` and `WebFetch`.
- A browser for CLI, workers, Portal, or scheduled unattended runs by default.
- Migrating Desktop to Electron or Wails v3 solely for a browser tab.

## 3. How Comparable Products Work

The products have similar **Agent loops** but different browser hosts. Public
documentation supports the distinctions below; it does not reveal every
private implementation detail.

| Product | Browser host and Agent control | Relevant boundary |
|---|---|---|
| Codex in the ChatGPT desktop app | A built-in browser with a profile separate from the user's regular browser; Computer Use can open, click, type, inspect, and screenshot. Developer mode offers approved full CDP access. An extension connects supported external browsers. | The built-in browser is unavailable in Codex CLI and its IDE extension. Website access and sensitive actions have separate approvals. [Official OpenAI documentation][openai-browser] |
| Claude Code | Claude in Chrome is a Chrome extension usable from Claude Code. It reads/operates pages and exposes DOM, network, and console information for development. The extension declares `debugger`, `scripting`, and tab permissions; Chrome documents `debugger` as a CDP transport. | This is control of Chrome through an extension, not evidence that the Claude Code CLI embeds its own browser engine. Claude Cowork also has a separately documented built-in browser. [Anthropic][claude-chrome], [Chrome][chrome-debugger] |
| VS Code | The open-source integrated browser is an Electron `WebContentsView`. The main process owns pages and sessions; a shared process runs Playwright, which reaches the page through a CDP proxy; the workbench holds a state mirror. | Agent-created pages are isolated. A user-opened tab is private until explicitly shared with an Agent. [Architecture source][vscode-architecture], [user documentation][vscode-tools] |

The common implementation pattern is: a real browser engine owns rendered
state; a controller exposes bounded navigation, observation, and interaction;
the Agent loop receives tool results; and the UI presents the same page and
permission state to the user. A web fetcher has no durable rendered page, and
an `iframe` alone has no Agent controller.

## 4. BuildMax Baseline

- Desktop is a Wails v2/React shell whose local chat runs through the shared Go
  `agentapp` runtime. Its Go bridge forwards tool and approval events to the
  frontend. See [Desktop architecture](../contribute/architecture/desktop.md),
  [`internal/interface/desktop/app.go`](../../internal/interface/desktop/app.go),
  and [`internal/interface/desktop/approval.go`](../../internal/interface/desktop/approval.go).
- Base tools include `WebFetch` and `WebSearch`, and Desktop enables MCP. None
  owns a browser page or offers DOM, screenshot, console, or interaction tools.
  See [`internal/agentapp/assembly.go`](../../internal/agentapp/assembly.go) and
  [`internal/agentapp/app.go`](../../internal/agentapp/app.go).
- `llm.MultimodalTool` and image `ContentPart`s can already carry screenshots
  through the Agent loop and model adapters. The browser work needs bounded
  image production and presentation, not a new generic message shape. See
  [`internal/core/llm/tool.go`](../../internal/core/llm/tool.go) and
  [`internal/core/llm/llm.go`](../../internal/core/llm/llm.go).
- The Desktop workspace already has a tab/pane state model for chat, terminal,
  file, and diff. It could display browser metadata or a future native browser
  view, but adding a `browser` tab kind does not itself create a controllable
  browser. See [`desktop/frontend/src/lib/tabs.js`](../../desktop/frontend/src/lib/tabs.js).
- The tool registry is cached per model and reused across sessions. The run
  carries its Session ID in `context.Context`; browser page ownership must be
  checked against that identity at every operation, not against the frontend's
  active tab. See [`internal/agentapp/app.go`](../../internal/agentapp/app.go)
  and [`internal/core/session`](../../internal/core/session).
- Existing permission resolution can ask before a write and scope a session
  grant through `llm.GrantScoper`. Desktop currently shows a generic tool
  approval with allow-once, allow-session, and deny choices. Browser origin
  admission and page sharing still need explicit semantics; a grant for one
  browser action must not silently authorize every site or page. See
  [`internal/core/agent/agent.go`](../../internal/core/agent/agent.go),
  [`internal/core/llm/tool.go`](../../internal/core/llm/tool.go), and
  [`desktop/frontend/src/components/ApprovalPanel.jsx`](../../desktop/frontend/src/components/ApprovalPanel.jsx).

Wails reuses each platform's native WebView rather than bundling a browser
engine. Its `WindowExecJS` targets the application window, not a separate
cross-platform CDP browser target. A React `iframe` is an unsuitable shortcut:
sites can prohibit embedding with CSP `frame-ancestors`, and it would leave
page control and the Wails Go-binding boundary unresolved. These conclusions
follow from [Wails' architecture][wails-intro], [its window runtime][wails-window],
and the [CSP specification summary][mdn-frame-ancestors]; they are not a claim
that an in-app view is impossible.

## 5. Technical Options

| Option | What it proves or delivers | Main cost or gap | Assessment |
|---|---|---|---|
| Existing browser MCP server | Fast experiment with the current Desktop MCP gateway and no product code | Requires user setup and an external server/runtime; BuildMax does not own page visibility, profiles, or consistent approval UX | Use only as a workflow probe |
| Desktop-owned Chromium over CDP | One real visible page shared by user and Agent; Go can own session mapping, tools, and lifecycle without a Node runtime | Needs executable discovery or browser distribution, CDP security, profile isolation, and cross-platform packaging checks | Preferred first product prototype |
| Wails WebView/React `iframe` | Can show embeddable pages inside current UI | Not a general browser; sites may reject frames; different native engines need different automation paths; loading untrusted content near app bindings raises a trust question | Do not use as the Agent browser backend |
| Native embedded Chromium view or Electron/CEF shell | Closest to VS Code's in-app shared page | Shell integration, process model, focus and overlays, binary size, platform packaging, and migration cost | Reconsider only after the external-window prototype demonstrates user demand |
| Chrome extension for the user's browser | Access to existing authenticated tabs and familiar Chrome UI | Extension permissions, distribution, native communication, and a much broader personal-data grant | Separate later capability; not required for local-app verification |

A Go CDP client such as [chromedp][chromedp] is a plausible implementation
dependency, not yet a selected one. CDP exposes navigation, DOM/accessibility
state, input, screenshots, and console events ([protocol reference][cdp]).
The controller may use installed Chrome/Edge where supported, or a pinned
Chrome for Testing binary. [Chrome for Testing][chrome-for-testing] is intended
for automation; [Playwright's browser documentation][playwright-browsers]
illustrates the packaging/version issue any browser-backed solution must solve.

## 6. Candidate Architecture

```text
Desktop React UI <--- Wails events/bindings ---> Desktop App
                                                  |
                                          browser manager (Go)
                                                  |
                                     isolated Chromium + CDP
                                                  ^
Agent loop -> Desktop-only Browser tool(s) -> session-scoped controller
```

1. **Ownership.** The Desktop process owns browser startup, process shutdown,
   profile location, and the mapping from BuildMax Session ID to browser
   context/pages. Agent tool calls carry the Session ID from the run context.
   The Go manager is authoritative; React mirrors page URL, title, loading,
   ownership, and approval state through Wails events.
2. **Tool assembly.** `agentapp` receives an optional browser capability from
   Desktop. It registers browser tools only when that capability is usable,
   following the existing surface-scoped tool rule. CLI and workers neither
   gain a dead tool nor a browser process. Tool names belong in
   [`internal/tool/names.go`](../../internal/tool/names.go). Decide whether the
   first contract is one action-based tool or a few focused tools during the
   prototype; do not create one tool per CDP command.
3. **Observation.** Prefer a bounded accessibility/DOM snapshot with stable
   references local to one page revision, plus a screenshot when visual
   judgment matters. Report URL, title, viewport, and snapshot revision with
   every observation. Interaction rejects a reference after navigation or DOM
   replacement rather than acting on a guessed target. Keep page text and
   console excerpts bounded before they enter model context and traces.
4. **Interaction.** A small, explicit operation set suffices for the first
   journey: navigate, inspect, click, type, screenshot, and recent console
   errors. Each call checks page ownership, current origin, policy, timeout,
   and cancellation. A user action in the visible browser can change the page;
   the next Agent action must reobserve it.
5. **Presentation.** Initially show the browser's own visible window and a
   compact Desktop activity indicator linking the current session to its page.
   If an in-workspace browser is later chosen, the native page must remain the
   same page that CDP controls. A static screenshot tab would be a preview,
   not an interactive built-in browser.

This arrangement follows the repository dependency direction: browser process
control is an infrastructure concern; `agentapp` assembles the optional tools;
the Desktop interface owns user-visible lifecycle and authorization; core only
retains the existing tool and message contracts. The prototype should establish
the smallest interface that supports that flow before a new abstraction is
made durable.

## 7. Trust and Failure Boundaries

| Boundary | Candidate rule and reason |
|---|---|
| Profile and identity | Always launch with a separate user-data directory. Do not attach to the default Chrome profile. Chrome 136+ intentionally disallows remote debugging of its default directory; Chrome recommends a non-default one. [Chrome security note][chrome-remote-debugging] |
| Page visibility | Agent-created pages belong only to their BuildMax session. A page opened by the user is not exposed to an Agent without an explicit share action; sharing is revocable. VS Code demonstrates this boundary, but BuildMax must implement its own. [VS Code documentation][vscode-tools] |
| Site access | Check the canonical origin before navigation and after redirects. Do not equate a tool session grant with all-origin consent. The first slice can limit interactions to local development origins while evaluating public-site behavior. |
| Agent input | Page text, ARIA names, console output, and screenshots are untrusted data. The Agent must not treat instructions in a page as user instructions. Authentication and consequential actions need a separate user decision; a generic click cannot reliably be classified as harmless by its selector alone. |
| App bridge | Untrusted pages never receive Wails Go bindings, the BuildMax auth token, or direct access to the React app's state. Keep browser automation in the Go controller and return bounded results to the Agent. |
| CDP transport | Prefer a private process transport if the selected driver supports it. If using a debugging port, bind only to loopback, choose a fresh port, do not expose its address to pages or traces, and close it with the browser process. CDP is powerful enough to inspect cookies and page internals. |
| Network reach | Browser navigation permission is not a full egress sandbox: loaded pages can request subresources, and the browser runs with the local machine's network access. Do not describe the first prototype as worker-grade isolation. |
| Downloads and files | Disable Agent-driven uploads/downloads and `file:`, `javascript:`, `data:`, and browser-internal navigation in the initial tool contract. If later enabled, give each a separate file and approval boundary. |
| Failure and recovery | A browser crash, disconnected CDP session, blocked site, timeout, or stale element is a tool error the Agent can diagnose. Shutdown and cancellation must release processes and temporary profiles. Never silently reconnect to a different browser/profile. |

The existing Desktop approval panel is a starting UI, but an approval that says
only `Browser` plus raw arguments is insufficient for a site grant. It must
show the exact origin, requested action, current page, and whether the grant is
once or for this session. Any richer, persistent allowlist would be a separate
decision, not an accidental side effect of `ApprovalAllowSession`.

## 8. Proposed Experiment and Evidence

### Bounded prototype

Use an isolated test profile and a small local web app served on a random
loopback port. The app should have a stateful form, a route change, and a
deliberate console error. The user journey is:

1. Desktop Agent starts or finds the local server and opens the route in its
   visible browser.
2. It observes a bounded page snapshot, types and clicks through the form,
   and reports the resulting state, URL, and console error.
3. The user interrupts once; the next Agent action reobserves rather than
   relying on a stale element reference.
4. Cancel the run, close the session, and close Desktop; confirm browser
   process and temporary profile cleanup at each boundary.

### Evidence required before accepting the direction

- **Utility:** one reproducible fix-and-verify task succeeds using observed
  browser state, with tool calls and screenshots reviewable in the Desktop
  transcript. Compare the same task with `WebFetch`/`WebSearch` alone.
- **Correctness:** navigation, redirects, cross-origin changes, stale elements,
  dialogs, new tabs, console errors, crashes, and cancellation have explicit
  outcomes; no call can use another session's page.
- **Trust:** a page cannot call Wails bindings, learn CDP access details, read
  another browser profile, or gain an all-site grant from a one-site approval.
  Test a deliberately malicious page instruction as data, not as authority.
- **Operations:** run the prototype on supported macOS, Windows, and Linux
  targets, recording installed-browser discovery, startup time, memory use,
  packaging size, version compatibility, and failure messages. A macOS-only
  prototype is useful evidence but not proof of portable delivery.
- **Verification:** Go unit tests for session/origin/action gates and cleanup;
  a real-browser local integration journey for CDP behavior; Desktop bridge and
  UI checks for binding/event/approval behavior; packaged-app launch evidence
  if the shipped Wails bundle changes. Select actual commands from
  [testing guidance](../contribute/testing.md) and `./make help` when work begins.

This report made no code change and ran no browser prototype. The claims above
are source review and repository inspection, not measured BuildMax performance
or a qualified security boundary.

## 9. Open Questions and Likely Destination

1. Is the primary task **local web-app verification**, or must the first
   release also operate authenticated third-party sites? The latter changes
   profile, login, permission, and consequential-action requirements.
2. Is a separate visible browser window acceptable for the first useful
   version? If not, what user observation justifies an embedded native view
   before the Agent/browser contract is validated?
3. Which browser executable is part of the supported Desktop contract:
   discovered system Chrome/Edge, a managed Chrome for Testing download, or a
   bundled binary? Installation, updates, license checks, and offline behavior
   depend on this answer.
4. Should browser state be ephemeral per Session, persist across that Session's
   turns, or persist beyond Desktop restarts? Start with the least retained
   state the observed journey needs; do not silently create a second durable
   session store.
5. Which permissions belong in the existing tool policy, and which are
   browser-specific site/page-sharing decisions? The prototype should exercise
   both without treating a tool grant as an origin grant.

If accepted, record priority in the [roadmap](../ROADMAP.md), move the settled
architecture and trust rationale into a `docs/design/` record with its required
Chinese mirror, and create a ready backlog item only when the implementation
is decomposed and its dependencies are complete. User-facing browser behavior
and settings then belong in the manual/reference documentation. Retire this
proposal when that decision is made.

## 10. External Sources

All product and technical references below were checked on 2026-09-21. Public
product documentation establishes exposed behavior; only the VS Code source
describes its internal architecture in detail.

- [Official OpenAI documentation: Browser][openai-browser]
- [Anthropic: Get started with Claude in Chrome][claude-chrome]
- [Chrome Extensions: debugger API][chrome-debugger]
- [VS Code: integrated browser architecture source][vscode-architecture]
- [VS Code: browser tools for agents][vscode-tools]
- [Wails v2: introduction][wails-intro] and [window runtime][wails-window]
- [Chrome DevTools Protocol reference][cdp]
- [chromedp repository][chromedp]
- [Chrome: remote debugging profile change][chrome-remote-debugging]
- [Chrome for Testing][chrome-for-testing]
- [Playwright: browser binaries and installation][playwright-browsers]
- [MDN: CSP `frame-ancestors`][mdn-frame-ancestors]

[openai-browser]: https://learn.chatgpt.com/docs/browser
[claude-chrome]: https://support.claude.com/en/articles/12012173-get-started-with-claude-in-chrome
[chrome-debugger]: https://developer.chrome.com/docs/extensions/reference/api/debugger
[vscode-architecture]: https://github.com/microsoft/vscode/blob/main/.github/skills/integrated-browser/SKILL.md
[vscode-tools]: https://code.visualstudio.com/docs/agents/run/browser-tools
[wails-intro]: https://v2.wails.io/docs/introduction/
[wails-window]: https://v2.wails.io/docs/reference/runtime/window/
[mdn-frame-ancestors]: https://developer.mozilla.org/en-US/docs/Web/HTTP/Reference/Headers/Content-Security-Policy/frame-ancestors
[cdp]: https://chromedevtools.github.io/devtools-protocol/
[chromedp]: https://github.com/chromedp/chromedp
[chrome-remote-debugging]: https://developer.chrome.com/blog/remote-debugging-port
[chrome-for-testing]: https://developer.chrome.com/docs/automation-and-testing/chrome-for-testing
[playwright-browsers]: https://playwright.dev/docs/browsers
