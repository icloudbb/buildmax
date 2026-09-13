# Server Coordination

> **简体中文：** [阅读中文镜像](../zh-CN/design/服务器协调.md)

> **Audience:** contributors and operators · **Status:** implemented and
> qualified. Both `local` and `redis` backends ship, message-write fencing is
> enforced, and a deployed kind probe proves cross-replica delivery, turn
> serialization, and recovery after a Redis restart against the candidate
> topology.

Related: [Graceful shutdown](graceful-shutdown.md), [Worker API network
boundary](worker-api-network-boundary.md), [Enterprise
deployment](enterprise-deployment.md), and [Agent execution and Task
threads](agent-execution-and-task-threads.md).

## Contents

- [1. Status](#1-status)
- [2. Problem](#2-problem)
- [3. Decision](#3-decision)
- [4. The Coordinator Seam](#4-the-coordinator-seam)
- [5. Stream Delivery](#5-stream-delivery)
- [6. Event Fan-Out](#6-event-fan-out)
- [7. Conversation Turn Serialization](#7-conversation-turn-serialization)
- [8. Configuration And Failure Policy](#8-configuration-and-failure-policy)
- [9. Deployment Topology](#9-deployment-topology)
- [10. Testing](#10-testing)
- [11. Out Of Scope And Open Questions](#11-out-of-scope-and-open-questions)

## 1. Status

The shared-coordination mechanism described here is implemented. The basic/kind
and production manifests run two `buildmax-server` replicas with
`coordination.mode: redis`; Compose remains the single-server `local` path.
Redis-backed Task streams, connection-event fan-out, renewable Conversation
turn leases, startup failure when configured Redis is unavailable, and manifest
topology checks all ship.

Each lease's fencing token is enforced in the Conversation message-history write
path: a write carrying a token below the highest the conversation has accepted is
rejected, so a holder that resumed after its lease expired cannot append behind
the replica that superseded it. The deployed qualification gap is now closed:
[`kindCoordinationProbe`](../../tools/mk/coordination_probe.go), run by
`./make kind smoke`, drives the two-replica + Redis kind stack directly and
proves cross-replica stream delivery, cross-replica turn-lease serialization,
and recovery after a Redis restart — the delivery, serialization, and recovery
evidence R1 asks for (§10). In-process and miniredis tests prove the mechanism;
this probe proves the candidate topology. Propagating a lost lease to the running
turn (§11) and a server rolling-update drill remain open beyond that bar, and are
R3 deployment-qualification concerns rather than coordination-correctness gaps.

## 2. Problem

Three server structures keep state that only one process can see:

- **The stream hub** (`internal/server/websocket/hub.go`) buffers a task run's
  streamed deltas and fans them to that task's SSE subscribers. A worker pushes
  deltas to whichever replica its HTTP call lands on; a browser subscribes on
  whichever replica its SSE request lands on. When those differ, the browser sees
  nothing until the durable result arrives.
- **The connection registry** (`internal/server/websocket/registry.go`) holds the
  live WebSocket connections and fans an event to every connection watching a
  space. A `task.status.changed` invalidation raised on one replica reaches only
  the sockets that replica happens to hold; a browser on another replica keeps
  showing stale state.
- **The turn queue** (`internal/server/turnqueue/turnqueue.go`) serializes a
  conversation's turns so two of them never interleave their reads and writes of
  one message history. Serialization anchored to a process cannot see a turn
  running on another replica. This is the one that corrupts data rather than
  degrading a view: two turns for one conversation, admitted on two replicas,
  both believe they are the sole writer.

The scheduler and the stale-run reaper are already safe at N replicas: they claim
work by an atomic conditional `UPDATE`, so more replicas are more pollers, not
duplicated work. The three structures above have no such anchor.

## 3. Decision

Introduce one shared coordination backend, selected by configuration:

- `coordination.mode: local` (default) keeps today's in-process implementations
  unchanged. This is what CLI-adjacent single-instance and every development
  deployment use, and it is the supported topology whenever the mode is `local`:
  exactly one server replica.
- `coordination.mode: redis` routes all three structures through Redis so N
  replicas share the live state. A deployment that runs more than one replica
  must set this.

Redis carries all three because their needs — a replayable per-run stream, a
low-latency per-space broadcast, and a per-conversation mutual-exclusion lease —
are all first-class Redis primitives, so one dependency and one connection pool
cover the whole surface. A hybrid split across MySQL advisory locks and a
separate broker was rejected under Occam: it multiplies backends without removing
the one that low-latency fan-out actually needs.

**Fail closed.** When `mode: redis` is configured and Redis is unreachable at
startup, the server refuses to start. Silently falling back to `local` under two
replicas would reinstate the exact turn-serialization corruption this record
exists to prevent — the same reasoning as the worker sandbox, which never runs
unconfined merely because its backend is unavailable.

## 4. The Coordinator Seam

A `coordination.Backend` lives in `internal/infra/coordination`. It owns the
shared Redis client, health-checks it once at construction, and vends the three
backed implementations the server needs:

- a `websocket.StreamHub` (the interface already anticipates a Redis impl),
- an event publisher/subscriber the connection registry broadcasts through,
- a `turnqueue.Locker` the turn queue acquires before running a turn.

Each consumer depends on a small interface it defines, and the infra package
implements it — the dependency runs `server → infra → core`, never the reverse.
Passing a `nil` backend selects the in-process behavior, so the `local` mode is
not a second code path but the absence of the shared one. `bootstrap` constructs
the backend from `ServerConfig` and hands it to `NewHandler`, which today builds
the three in-memory structures directly.

## 5. Stream Delivery

The stream hub is keyed by `task_run_id`, not `task_id`: a task's successive
turns each own a distinct buffer. A run's buffer lingers briefly past its own end
(a short done-TTL, so a straggler still catches the tail), but the next turn is a
new run under a new key, so a finished run's output is never replayed to the next
run's watchers. The SSE handler resolves the task's active run
(`GetActiveTaskRunByTask`) and subscribes to that run's key; the front end still
addresses the endpoint by `task_id` and never learns the run key.

The Redis stream hub maps `StreamHub` onto a Redis Stream keyed `stream:{task_run_id}`.

- `Append(runID, delta)` is `XADD` with `MAXLEN ~` approximate trimming, which
  caps memory the way the 2 MiB in-memory buffer does, and refreshes a TTL so an
  abandoned run's stream expires instead of leaking.
- `Subscribe(runID)` tails entries appended after the subscription point
  (`XREAD BLOCK` from the current last id), delivering live deltas — the same
  semantics the in-memory hub has, so both backends share one code path.
- `Buffer(runID)` returns the current snapshot (`XRANGE - +`, minus the terminal
  marker) so a late subscriber catches up, exactly as the in-memory hub does. The
  small overlap window between `Subscribe` and `Buffer` can duplicate a fragment;
  that matches the in-memory hub's accepted behavior and is cosmetic on additive
  text.
- `Done(runID)` appends a terminal marker entry and sets a short TTL; a
  subscriber that reads the marker emits the `[[DONE]]` sentinel and stops.

A worker `XADD` on one replica and a browser `XREAD` on another now see the same
stream.

## 6. Event Fan-Out

Each replica keeps its own connection registry exactly as today: a socket is
physically owned by the replica it connected to, and only that replica can write
to it. Cross-replica reach is added by publishing, not by sharing the map.

- `Broadcast(space, user, type, payload)` publishes the event envelope to a
  single Redis Pub/Sub channel instead of iterating local sockets.
- Every replica runs one subscriber on that channel. On each envelope it performs
  the local fan-out — `audience(space, user)` over its own sockets — that
  `Broadcast` used to do inline.
- The publishing replica receives its own message back over Pub/Sub and fans out
  there too, so the publish path never fans out locally. One code path delivers
  to every connection on every replica, with no double-send.

A single channel is chosen over per-space channels for simplicity; the envelope
carries the space id and each replica filters. Per-space channels are a later
optimization if cross-space traffic ever justifies dynamic (un)subscription.
`BroadcastSink`, which streams a server-initiated turn's deltas, rides the same
`Broadcast` and so becomes multi-replica correct with no change of its own.

## 7. Conversation Turn Serialization

The turn queue keeps its in-process mechanics — the per-conversation goroutine,
the pending list, position reporting, `OnDequeue`, and draining — because two
turns on one replica still serialize cheaply there. A distributed lease guards the
one boundary that must hold across replicas: starting a turn.

- Before it calls a job's `run`, the queue's goroutine acquires
  `lock:conv:{conversation_id}` (`SET NX PX`, blocking with a bounded wait). A
  watchdog renews the lease on an interval while the turn runs; release drops the
  lock.
- A replica that cannot acquire the lock waits, which is exactly the cross-replica
  serialization required. A replica that dies mid-turn has its lease expire, so
  another replica takes over rather than deadlocking.
- The lease carries a monotonic **fencing token**, exposed on the lease and
  threaded into every message-history write the turn makes. The conversation
  stores the highest token it has accepted and rejects a lower one with
  `ErrStaleTurnWrite`, so a stalled holder that resumes after its lease expired
  cannot interleave a write behind the replica that took the lock. This is the
  safety net for a >TTL pause, on top of the mutual exclusion the held lease and
  its renewal deliver. The token is zero on the single-instance (`local`) path,
  which enforces no fence because the in-process queue is the only writer.
- Queue **position** becomes per-replica and therefore approximate across
  replicas. That is acceptable: position is a UX hint the surface shows, not a
  guarantee, and the serialization it hints at is now enforced by the lease.

Because the lease holds even during a rolling update's brief two-replica overlap,
the server keeps `RollingUpdate` with `maxUnavailable: 0` — B-i removes the need
to fall back to a `Recreate` strategy or a single-replica cap.

## 8. Configuration And Failure Policy

`server.yaml` gains a `coordination` section:

```yaml
coordination:
  mode: local            # or: redis
  redis:
    address: redis:6379   # host:port
    username: ""          # optional (Redis 6+ ACL)
    password: ""          # optional; overridable by BUILDMAX_COORDINATION_REDIS_PASSWORD
    db: 0                 # logical database
    tls: false            # dial over TLS
```

- `mode` defaults to `local`. Any value other than `local` or `redis` is a
  configuration error the server refuses to start on.
- The Redis password follows the same environment-override pattern as the
  database password: `BUILDMAX_COORDINATION_REDIS_PASSWORD` overrides the file so
  the credential need not be on disk.
- With `mode: redis`, construction dials Redis and issues one `PING`. Failure is
  fatal — the server exits rather than serving with process-local coordination
  under a multi-replica manifest.

## 9. Deployment Topology

- `deployment/buildmax-deploy.yaml` and
  `deployment/production/buildmax.yaml` include a Redis `Deployment` and
  `Service`, set `coordination.mode: redis` and the address in the Server
  `ConfigMap`, and run `buildmax-server` at two replicas. The basic manifest is
  what kind exercises; production carries the same coordination shape.
- Redis itself is a single instance in the first supported topology. Its state is
  reconstructible — streams and locks are live, not durable, and a Redis restart
  costs at most in-flight stream deltas and a brief re-acquire of conversation
  leases — so it needs no persistence volume. This limit is recorded in the
  deployment docs rather than hidden.
- Compose runs one Server with `mode: local`. Kind uses the basic manifest's two
  Servers and Redis, so the normal cluster smoke exercises the supported
  multi-replica topology rather than reserving it for production.

## 10. Testing

- Unit tests cover each backed implementation against an in-process
  [miniredis](https://github.com/alicebob/miniredis), so the coordination behavior
  runs on every build without a Redis service. This proves the mechanism, not the
  deployed candidate topology (§1); a real-Redis scope is left to the candidate
  exercise rather than asserted here.
- A cross-instance test builds two `Handler`s sharing one Redis and asserts the
  three guarantees: an `Append` on one is read by a `Subscribe` on the other; a
  `Broadcast` on one reaches a socket registered on the other; a turn lease held
  by one blocks a turn for the same conversation on the other.
- `stream_multireplica_test.go` specifically drives a Task delta through one
  handler and reads it from another, and its local-backend counterpart proves
  the same delivery does not cross process-local hubs accidentally.
- A fail-closed test asserts that `mode: redis` with an unreachable address makes
  server construction return an error rather than a working handler.
- The store scope (`./make test mysql`) asserts the write-path fencing: once a
  conversation has accepted a message under a token, `AppendMessage` refuses a
  lower token with `ErrStaleTurnWrite`, an unfenced write does not lower the bar,
  and the rejected writes leave no row.
- An architecture test asserts the production manifest's `buildmax-server` replica
  count is consistent with a configured coordination backend, so a manifest that
  scales the server without `coordination.mode: redis` fails the build.
- A deployed probe closes the gap the miniredis tests leave open.
  [`kindCoordinationProbe`](../../tools/mk/coordination_probe.go), run at the end
  of `./make kind smoke` beside the sandbox and worker-API boundary probes,
  forwards each `buildmax-server` pod on its own port and drives the two replicas
  by hand against real Redis. It proves three things a unit test cannot, on the
  candidate topology: a task's worker output reaches a stream opened on either
  replica (delivery); a turn stalled at the model on one replica holds the
  conversation lease so a turn raised on the other replica cannot reach the model
  until it releases (serialization); and both recover after the Redis pod is
  restarted (recovery). A stall armed on the shared mock is what opens the window
  to observe each before any reply is written, and each turn carries a per-run
  nonce because the mock's request log is cumulative across runs.

## 11. Out Of Scope And Open Questions

- Redis high availability (Sentinel or Cluster) is not required for the first
  supported multi-replica topology and is left to the operator's own Redis.
- Sharing durable Session state across devices is a separate direction
  ([durable Agent sessions](../proposals/durable-agent-sessions.md)); this record
  covers only live server coordination.
- Per-space Pub/Sub channels, backpressure metrics on a slow subscriber, and a
  Redis-outage readiness signal that degrades rather than exits are deferred until
  a running multi-replica deployment shows they are needed.
- Lease renewal discards Redis errors and does not cancel or notify the running
  turn when ownership is lost (`internal/infra/coordination/lock.go`). The
  write-path fence makes such a turn a wasted one rather than a corrupted
  conversation: its later appends fail with `ErrStaleTurnWrite`. Propagating
  lease loss to the turn is a candidate-evidence question, not a data-safety
  gap.
