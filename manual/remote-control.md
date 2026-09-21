# Remote Control

Remote Control lets you start an agent session at your desk and keep working with
it from another device — a laptop or a phone browser — through your BuildMax
server. The session keeps running on your machine: your files, tools, and MCP
servers stay local, and the other device is just a window onto it.

## Start a reachable session

Sign in first, then start an interactive session with Remote Control on:

```bash
buildmax --remote-control
buildmax --remote-control --remote-control-name "My project"
```

The session connects to the managed server it is signed in to and registers
itself under your account. When it is reachable, the log prints the session it
was assigned. `--remote-control-name` sets the label another device shows; it
defaults to your machine's host name.

Remote Control needs a login (`buildmax login`); it has nothing to reach without
a managed server. It is off unless you pass the flag.

## Watch and steer from another device

Open **Remote Control** in Portal on the other device. You will see your live
sessions, each with an online dot. Open one to:

- **Watch its output** stream as it happens.
- **Send a follow-up message** — it is delivered into the running turn, or starts
  a new one if the session is idle, exactly as if you had typed it at the
  terminal.
- **Approve or deny a tool call** when the session asks for permission. Whoever
  answers first — the terminal or the device — wins, and the other prompt clears.
- **Stop a running turn** with the Stop button. It interrupts that turn; the
  session stays usable.

## What stays on your machine

Execution, the filesystem, and tools never leave your computer. The server
relays messages and the session's output; it is the bridge, not the runtime.
Because the server is your own BuildMax deployment, that relayed traffic stays on
infrastructure you control.

## Interruptions

If your network drops briefly, the session reconnects on its own and reattaches
to the same session, so the URL another device is watching keeps working and its
stream resumes. While the session is unreachable it shows as offline; it comes
back online when it reconnects. If you close the terminal or quit the process,
the session goes offline until you start it again.

## Not yet available

Opt-in is on the CLI/TUI today. Desktop and print-mode opt-in, mobile push
notifications, and per-device trust are not built.
