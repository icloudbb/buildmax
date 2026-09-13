---
name: drive-portal
description: "Drive Portal's real browser UI ad hoc against a running kind or Compose deployment: sign in with a freshly minted login code, click, type, screenshot, read console errors. Use when asked to poke at, click through, or screenshot Portal while working on a feature — not for a pass/fail check, use `./make e2e {local,kind,compose}` for that."
---

# Drive Portal

Ad hoc, agent-driven exploration of Portal's real UI: the same login-code
flow and browser `./make e2e {local,kind,compose}` scripts as a fixed
Playwright suite (see docs/contribute/testing.md), but here as a REPL you can
send one command at a time and read the result of each before deciding the
next — sign in, open the view a change touched, click into it, screenshot it,
read whatever the console logged.

This is not a pass/fail suite. For "does Portal still work," run
`./make e2e kind` (or `local`/`compose`) instead — same login flow, but
scripted, asserted, and torn down automatically. Reach for this skill instead
when the question is "what does this actually look like" or "does this flow
work," and you do not yet know what to assert — typically right after
`./make kind reload` has picked up a code change and before writing the
Playwright spec for it.

## Setup — one process, against an already-running deployment

Unlike Desktop, Portal has no separate dev server this skill starts for you.
Bring up a deployment first, then point the driver at it:

```bash
./make kind up            # or: ./make compose smoke
./make kind reload server # after a code change, before driving it
```

Both serve Portal at `http://localhost:8080` by default — override with
`BUILDMAX_PORTAL_URL` if the cluster or Compose project used a different
port.

Start the driver:

```bash
node .buildmax/skills/drive-portal/driver.mjs
```

It resolves Playwright from `portal/node_modules`, so that has to be
installed first — `npm --prefix portal ci` if `./make e2e kind` or `./make
check portal` has not already put it there. If no Chromium is cached, `npm
--prefix portal exec -- playwright install chromium` once.

Type `launch`, then a command per line. Wrap the driver in tmux for an agent
to drive: send a line with `tmux send-keys`, wait for its output with
`tmux capture-pane`, then send the next — the driver serializes commands
internally, but you still want to see each result before deciding the next
one. If tmux (or `screen`) isn't available in the sandbox, a growing-file
substitute works just as well, since the driver only ever reads stdin line by
line:

```bash
touch /tmp/portal-cmds.txt
tail -f -n +1 /tmp/portal-cmds.txt | node .buildmax/skills/drive-portal/driver.mjs > /tmp/portal-driver.log 2>&1 &
echo launch >> /tmp/portal-cmds.txt   # then tail the log to read each result
```

`tail -f` never sends EOF, so the driver's stdin stays open across as many
appended commands as you send — unlike a one-shot pipe, which closes stdin
(and the browser with it) as soon as the writer finishes.

## Commands

| command | what it does |
|---|---|
| `launch` | open a browser page at `BUILDMAX_PORTAL_URL`, print its title |
| `login [email]` | mint a fresh code with `./make kind login`, sign in through the login-code form |
| `ss [name]` | screenshot → `/tmp/buildmax-portal-shots/<name>.png` (override: `SCREENSHOT_DIR`); temporarily expands Portal's scrolling shell so the full page is captured, not just what fit in the viewport |
| `click <css-sel>` | click an element (a real Playwright locator — waits up to 8s to be actionable) |
| `click-text <text>` | click the first element whose visible text contains `<text>` |
| `role <role> <name>` | click the first element with that accessible role and name, e.g. `role button Switch to dark mode` — the only way to reach an icon-only control, whose name lives in `aria-label` and has no visible text for `click-text` to match |
| `roles` | dump the accessible role/name tree of the current page, to find a selector without guessing or opening an e2e spec |
| `label <text>` | focus the input behind an accessible label (then `type` into it) |
| `type <text>` | keyboard-type into whatever has focus |
| `press <key>` | press one key (`Enter`, `Escape`, ...) |
| `confirm [accept\|off]` | arm the next browser dialog: `confirm` (or `confirm accept`) accepts the next `window.confirm()` once so a confirm-gated delete/destroy/disable actually runs; `confirm off` (or `confirm reject`) restores the default of dismissing it. One-shot — re-arm per gated action; the outcome is logged and visible in `console` |
| `probe <api-path>` | GET an API path with the same bearer token Portal itself sends (read from `localStorage`, against the app's API base) and print the status plus a body snippet — the authenticated status the app's own request would get, e.g. `probe /api/spaces/<id>/members`. Login first |
| `wait <css-sel>` | wait up to 10s for a selector to appear |
| `wait-text <text>` | wait up to 10s for text to appear anywhere on the page |
| `url` | print the current page URL |
| `eval <js>` | evaluate an expression in the page, print it as JSON |
| `text [css-sel]` | print `innerText` of a selector, or the whole body |
| `console [errors]` | print captured console messages; `console errors` filters to `console.error`/uncaught exceptions |
| `reload` | reload the page, clearing captured console messages |
| `quit` | close the browser, exit |

## A representative walkthrough

```
launch
login
wait-text Dashboard
ss after-login
click-text Issues
wait-text New issue
ss issues-list
console errors
```

## Gotchas

- **A login code is single-use and out of band on purpose.** `login` calls
  `./make kind login [email]` itself — you never need to run that separately
  or paste a code by hand. Default email is the deployment-smoke account;
  the account is created if it does not exist yet, so `login` works right
  after `kind up` with no prior `kind smoke` run.
- **React controlled inputs.** A raw `eval el.value = '...'` does not fire
  React's `onChange`. Use `label` to focus the field by its accessible label,
  then `type` — same reason `login` uses `getByLabel(...).fill(...)`-style
  interaction rather than `eval`.
- **Portal's own specs (`portal/e2e/*.spec.ts`) are the reference for
  selectors** — most views are driven by `getByRole`/`getByLabel`/`getByText`
  there, not CSS classes; skim the closest spec before guessing a selector, or
  run `roles` to see what's actually on the page.
- **A dialog's own close control may itself be icon-only.** If `click-text
  Close` doesn't dismiss it, `press Escape` reliably does.
- **A `window.confirm()`-gated action does nothing until you arm acceptance.**
  Distinct from the icon-only-close case above: many management actions
  (schedule delete, secret disable/destroy, comment or artifact delete, and
  Administration operations) are gated on a browser `window.confirm()`, which
  the driver's dialog handler dismisses by default — the click lands, `console
  errors` stays clean, and nothing changes. Run `confirm` first to accept the
  next dialog, then click; re-arm before each gated action.
- **`probe` reuses the page's own token — a plain `eval fetch` does not.**
  Portal authenticates with a bearer token it holds in memory, not a cookie, so
  `eval fetch(url, {credentials:"include"})` sends no `Authorization` header and
  returns 401 where the app's request would get 200 or 403. `probe` reads the
  same token the app stores and reports the real authenticated status, so you
  can tell an authorization failure (403) from an unauthenticated probe (401)
  without reading server logs. Run `login` first, or it probes as anonymous.
- **Websockets / long-poll.** `wait` and `wait-text` target the element you
  actually need; there is no generic "network idle" wait, because a live
  conversation or task view never goes idle.
- **Check `console errors` before declaring success.** A page can render its
  shell while every data fetch fails.
