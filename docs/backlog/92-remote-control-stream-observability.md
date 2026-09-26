---
id: remote-control-stream-observability
title: Carry turn boundaries and steer-context into the Remote Control stream
roadmap: none
source: docs/design/remote-control.md
depends_on: []
verification: [go, portal]
claim:
pr:
---

## Outcome

A device watching a Remote Control session can tell what the session is doing:
whose message started a turn, which tools it is running, and when a turn ends.
Today the relay carries only assistant text deltas and approval frames, so the
device sees a wall of concatenated replies, cannot see a tool-only turn at all,
and shows a live "Stop" affordance after the turn has already finished.

## Scope

Decide and implement the run events the Remote Control relay carries beyond raw
assistant deltas, at least: the user prompt that opened a turn (local or
device-sent), a tool-call start/end summary, and a turn-end marker. Extend the
relay protocol (`internal/infra/runrelay`), the agent WebSocket frames
(`internal/server/websocket`), and the Portal session view
(`portal/src/pages/remoteControl`) to render them, so the stream reads as a
sequence of turns rather than one growing string, and "Stop" and the status
label reflect whether a turn is actually running. Mark device-originated input
in the TUI so a local operator can tell a remote follow-up or Stop from their
own typing.

## Out Of Scope

- The idle-stream keep-alive and client reconnect: shipped separately as the F1
  fix from the 2026-09-23 exploratory run (SSE heartbeat plus Portal backoff
  reconnect).
- Desktop and print-mode Remote Control opt-in, which the manual already lists
  as not built.
- Persisting or replaying full turn history for a session opened long after the
  fact; this task is about the live stream's legibility.

## Acceptance Criteria

- A tool-only turn (no assistant text) is visible on the device as it runs.
- The Portal view separates turns and attributes each to its prompt; a reload
  replays them without concatenating adjacent replies into one block.
- The "Stop" control and status label show a running turn only while one is
  actually running, driven by a turn-end signal rather than by "any delta seen".
- The TUI distinguishes a device-sent prompt or Stop from local input.
- The relayed frames stay redacted and bounded the way the content stream is.

## Verification

- `go`: unit tests for the new relay frames and their server handling, including
  a turn-end frame ending the "running" state.
- `portal`: component tests for the turn-separated render and the Stop/label
  state; a `drive-portal` pass against kind for the live behaviour.

## Notes

Found as F2 in the 2026-09-23 exploratory run (browser tools + Remote Control).
The `StreamSink` interface in `internal/core/llm` carries only `OnDelta`, so a
turn-end signal belongs at the run-loop turn boundary in `internal/agentapp`,
not on `StreamSink`. This is a design decision about relay richness, which is
why it is a task rather than folded into the F1 fix. See
manual/remote-control.md "Not yet available" and the phasing in docs/design/remote-control.md.
