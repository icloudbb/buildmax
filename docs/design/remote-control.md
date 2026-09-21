# Remote Control: Driving A Local Session From Another Device

> **简体中文：** [阅读中文镜像](../zh-CN/design/远程控制.md)

> **Audience:** contributors · **Status:** accepted — Phase 1 planned
>
> This record decides the mechanism for the accepted
> [Remote Control proposal](../proposals/remote-control.md); where the two
> disagree, this record is current. It is the rationale for a new **control
> plane** that lets a device-resident Agent session be observed and steered from
> another device through the server. It builds on [client modes](client-modes.md)
> (how a local client authenticates to the server), reuses the outbound-tunnel
> idea of [Agent Bridge CLI](agent-bridge-cli.md), and is distinct from the Task
> plane of [Agent execution and Task threads](agent-execution-and-task-threads.md).

## Contents

- [1. Decision](#1-decision)
- [2. Scope And Position](#2-scope-and-position)
- [3. Ownership And Authorization](#3-ownership-and-authorization)
- [4. The Agent WebSocket](#4-the-agent-websocket)
- [5. Live-Session Registry And Presence](#5-live-session-registry-and-presence)
- [6. Event Relay And Observation](#6-event-relay-and-observation)
- [7. Opt-In, Trust, And Kill Switch](#7-opt-in-trust-and-kill-switch)
- [8. Phasing](#8-phasing)
- [9. Open Questions](#9-open-questions)

## 1. Decision

Remote Control is a **control plane** over a live, device-resident Agent session:
the runtime, filesystem, tools, and MCP servers stay on the user's machine, and
the server is only a relay. The local process **dials out** to the server over a
WebSocket, registers the session against the user's account, heartbeats, and
relays its event stream; other devices observe (Phase 1) and later steer
(Phase 2+) that session through the server.

The plane is deliberately separate from Task/TaskRun. Task is a bounded-turn,
server-executed model whose run originates from a server dispatch; a Remote
Control session is persistent, device-resident, and its run originates on the
laptop. Modelling it as a TaskRun would invert ownership and distort both, so
Remote Control gets its own entity and its own relay path (§2 of the
[proposal](../proposals/remote-control.md) records the full host-by-surface
decomposition and why the control plane ships first).

## 2. Scope And Position

Phase 1 is **read-only remote observation**: opt in on the laptop, see the
session online from another device, and watch its stream. No inbound steering yet.
Phase 1 is the whole hard part, because the server cannot reach the laptop; once
the outbound channel, the registry, and presence exist, later phases add message
kinds over the same channel (§8).

Only one genuinely new network primitive is introduced: the **laptop→server agent
WebSocket** (§4). Everything a remote device needs to *observe* reuses existing
server machinery — the buffered [durable run stream](durable-run-trace.md)-style
`StreamHub` and the Server-Sent Events pattern already used for Task output — so
observation does not touch the space-scoped browser WebSocket at all (§6).

## 3. Ownership And Authorization

A live Remote Control session is **account-scoped, not Space-scoped.** It belongs
to a `UserID` and is visible and controllable only by that user. This is a
deliberate departure from the Portal norm that Space owns and authorizes
resources ([Space governance](space-governance.md)), and it is the answer to the
proposal's open question on scope.

Rationale: the thing being exposed is the user's own machine and their in-flight
local work, which has no Space until the user chooses to publish an artifact or
open an issue. Binding a laptop session to a Space would force an ownership
relationship that does not exist and would let Space membership imply reach into a
member's personal machine. The account is the true boundary; a future need to
share a live session into a Space is an additive grant, not the default.

The credential is the **existing user JWT** ([client modes](client-modes.md) §
credentials): the same access token the local client already holds for managed
inference authenticates the agent WebSocket. Phase 1 introduces no device token.
Per-device trust (a dedicated device credential, à la the
[worker run token](worker-run-token.md), plus a Trusted-Devices step-up) is
deferred to Phase 5; until then, account authentication plus opt-in and a kill
switch (§7) are the boundary.

## 4. The Agent WebSocket

The local process opens one **outbound** WebSocket to the server and keeps no
inbound port, so the machine is never externally reachable — the same posture as
[Agent Bridge CLI](agent-bridge-cli.md), reversed in direction (there a
subprocess reaches the server during a server-owned run; here a local session
reaches the server to expose itself). The connection carries session events out
and, in later phases, commands in; a single dialed connection is bidirectional
without any listening socket.

Authentication mirrors the browser upgrade: the token travels as a `?token=`
query parameter (WebSocket upgrades cannot set headers) and is validated as an
active user session before the socket is served. The client seeds its TLS trust
from the same HTTP client used for managed inference, so the WebSocket honors
exactly the trust the client already uses.

The message set is a small typed envelope (`{type, payload}`, the existing
protocol shape):

- `agent.register` — the client announces a session (display name, platform,
  host); the server creates or re-attaches the `RemoteSession` (§5) and replies
  with its id, which is the stream key and the URL the user opens on another
  device.
- `agent.heartbeat` — periodic liveness; the server records last-seen (§5).
- `agent.event` — one serialized run event (§6), appended to the session's
  stream.

Inbound message kinds (`prompt`, `approval`, `cancel`) are defined but not
handled in Phase 1; adding them is Phase 2–4 and requires an in-memory registry
to route a command to the replica holding the socket (§9).

## 5. Live-Session Registry And Presence

The registry is a persisted entity, `RemoteSession`, owned by `internal/core`
and stored by `internal/infra/db` following the established entity pattern
(public-ID string identity, singular snake_case table, additive `AutoMigrate`).
It records the owning user, a display name, platform and host, a status, and a
last-seen timestamp. Persisting it — rather than keeping only an in-memory socket
registry — is what makes presence and the session list correct across server
replicas: the database is the source of truth, and any replica answers "which of
my sessions are live" from it.

Presence follows the existing liveness precedent rather than inventing one.
`agent.heartbeat` performs a throttled last-seen write (the pattern of the auth
session touch), and a scheduler reaper — modeled on the stale-run reaper — marks a
session offline when its last-seen falls outside a grace window. A clean
disconnect marks the session offline immediately; the reaper is the backstop for
an unclean drop. The last-seen column is indexed because the reaper scans by it.

## 6. Event Relay And Observation

**Serialization.** Run events are serialized with the existing run-trace record
type ([durable run trace](durable-run-trace.md)): it already maps every persisted
event kind to a bounded, secret-redacted, snake_case JSON line, and it already
handles the awkward parts of a runtime event (the error value, oversized fields).
Reusing it means the relay is redacted and bounded by construction and produces
the same shape the trace already emits. Token-by-token deltas, which the trace
record deliberately omits, ride a debounced delta path reusing the worker stream
sender's batching, so live typing survives without a write per token.

**Tee point.** The relay attaches at the single runtime chokepoint where events
already fan to the trace recorder and the caller's sink. Instrumenting there means
every surface — CLI, TUI, Desktop, print — relays with no per-surface code; only
the opt-in trigger differs per surface (§7). The relay is **fail-open**: a relay
error never disturbs the local run, matching the trace and hook fail-open rules.

**Observation.** A remote device observes over Server-Sent Events, not the
space-scoped browser WebSocket, because the session is account-scoped (§3). The
server keys the existing buffered `StreamHub` by the session id: `agent.event`
appends, and an SSE endpoint subscribes with buffer replay then live deltas until
done — the same mechanism and multi-replica behavior as Task output streaming. A
plain HTTP list endpoint returns the caller's sessions with presence for the
session list. This reuses the Portal streaming-chat render path unchanged.

## 7. Opt-In, Trust, And Kill Switch

Remote Control is **off by default** and activates only by explicit opt-in, never
implicitly, because exposing a session is remote code execution on the user's
machine under their account. Phase 1 exposes the opt-in on the CLI/TUI first (a
flag and/or command); the relay itself is surface-agnostic (§6), so Desktop and
print-mode opt-ins are additive later.

Two controls bound the risk from the start: a **kill switch** setting that
disables Remote Control on a machine regardless of account state, and the ability
to end a session's reachability. Because the broker is the user's own
`buildmax-server`, the relayed transcript never leaves infrastructure the user
controls — a stronger data story than a third-party relay. The local sandbox
posture is unchanged: Remote Control does not alter where code runs, unlike the
network-exposed Environment plane, which argues separately for a fail-closed
default ([agent sandbox policy](agent-sandbox-policy.md)). Per-device trust is
Phase 5 (§8).

## 8. Phasing

1. **Read-only remote observation** — the entity, the agent WebSocket
   (register/heartbeat/event), presence, the relay tee, SSE observation, the
   session list, and a Portal read-only view.
2. **Remote follow-up prompt** — an inbound `prompt` delivered to the local
   session's existing mid-run input seam ([queued messages](queued-messages.md));
   introduces the in-memory registry for cross-replica command routing.
3. **Remote tool approval** — forward permission prompts and return the decision.
4. **Cancel and reconnect hardening** — inbound cancel; queue and replay across
   brief disconnects.
5. **Notification and per-device trust** — the first outbound push mechanism and a
   device credential plus Trusted-Devices step-up.

Extending Remote Control to a *cloud* host is a separate track built on the
environment substrate of the [proposal](../proposals/remote-control.md) §5, not on
these phases.

## 9. Open Questions

- **Cross-replica command routing (Phase 2).** Observation is replica-agnostic
  because it flows through the persisted entity and the coordination-bus-backed
  stream hub, but delivering a command to the one replica holding a session's
  socket needs an in-memory registry keyed by session id plus a bus route. The
  smallest correct design is deferred to Phase 2.
- **Sharing a live session into a Space.** Account scope (§3) is the default; if a
  need arises to let a Space see a member's live session, it should be an explicit
  additive grant, and its shape is unspecified here.
- **Transcript retention.** The server holds only what live observation and
  short-window reconnect require; the exact retention window for relayed stream
  buffers is unspecified and should not become durable storage of local history.
- **Desktop and print opt-in ergonomics.** The relay is surface-agnostic; the
  per-surface opt-in affordance (a Desktop toggle, a print-mode flag) is left to
  those surfaces' own follow-ups.
