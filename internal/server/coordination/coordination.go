// Package coordination adapts the shared Redis primitives in
// internal/infra/coordination to the server's own interfaces: the stream hub the
// SSE handler reads, the event bus the connection registry broadcasts through,
// and the turn locker the turn queue holds. It is the server-side half of
// docs/design/server-coordination.md; the infra half stays free of server types.
package coordination

import (
	"context"
	"time"

	infra "github.com/icloudbb/buildmax/internal/infra/coordination"
	"github.com/icloudbb/buildmax/internal/server/turnqueue"
	wsconn "github.com/icloudbb/buildmax/internal/server/websocket"
)

const (
	// streamKeyPrefix namespaces a run's stream key.
	streamKeyPrefix = "stream:"
	// streamMaxLen caps a stream at roughly this many entries, the Redis analogue
	// of the in-memory hub's byte cap.
	streamMaxLen = 4096
	// streamTTL expires an abandoned stream; every append refreshes it.
	streamTTL = 30 * time.Minute
	// streamDoneTTL lets a finished stream linger for stragglers, then expire.
	streamDoneTTL = time.Minute
	// eventsChannel carries every connection-registry broadcast.
	eventsChannel = "buildmax:events"
	// remoteControlChannel carries Remote Control commands (a remote prompt) to
	// whichever replica holds the target session's agent socket.
	remoteControlChannel = "buildmax:remote-control"
	// convLockPrefix namespaces a conversation's turn lease.
	convLockPrefix = "conv:"
	// turnLeaseTTL bounds a conversation turn lease. It renews at a third of this
	// while the turn runs, and expires this long after a holder dies.
	turnLeaseTTL = 30 * time.Second
)

// The Redis-backed adapters must satisfy the interfaces their consumers hold.
var (
	_ wsconn.StreamHub = (*StreamHub)(nil)
	_ turnqueue.Locker = (*TurnLocker)(nil)
)

// StreamHub is the Redis-backed websocket.StreamHub.
type StreamHub struct {
	backend *infra.Backend
	ctx     context.Context
}

// NewStreamHub returns a stream hub bound to the server's lifetime ctx.
func NewStreamHub(ctx context.Context, backend *infra.Backend) *StreamHub {
	return &StreamHub{backend: backend, ctx: ctx}
}

func (h *StreamHub) Append(runID, delta string) {
	if runID == "" || delta == "" {
		return
	}
	_ = h.backend.StreamAppend(h.ctx, streamKeyPrefix+runID, delta, streamMaxLen, streamTTL)
}

func (h *StreamHub) Buffer(runID string) string {
	if runID == "" {
		return ""
	}
	s, _ := h.backend.StreamSnapshot(h.ctx, streamKeyPrefix+runID)
	return s
}

func (h *StreamHub) Done(runID string) {
	if runID == "" {
		return
	}
	_ = h.backend.StreamDone(h.ctx, streamKeyPrefix+runID, streamDoneTTL)
}

func (h *StreamHub) Subscribe(runID string) (<-chan string, func()) {
	if runID == "" {
		ch := make(chan string, 1)
		close(ch)
		return ch, func() {}
	}
	subCtx, cancel := context.WithCancel(h.ctx)
	raw := h.backend.StreamTail(subCtx, streamKeyPrefix+runID)
	out := make(chan string, 256)
	go func() {
		defer close(out)
		for d := range raw {
			msg := d
			if d == infra.DoneMarker {
				msg = wsconn.StreamEventDone
			}
			select {
			case out <- msg:
			case <-subCtx.Done():
				return
			}
		}
	}()
	return out, cancel
}

// EventBus is the Redis-backed connection-event fan-out. PublishEvent sends a
// broadcast to every replica; Incoming carries the broadcasts this replica must
// deliver to its own connections.
type EventBus struct {
	backend  *infra.Backend
	ctx      context.Context
	incoming <-chan []byte
}

// NewEventBus subscribes to the shared events channel for the server's lifetime.
func NewEventBus(ctx context.Context, backend *infra.Backend) *EventBus {
	return &EventBus{
		backend:  backend,
		ctx:      ctx,
		incoming: backend.Subscribe(ctx, eventsChannel),
	}
}

func (e *EventBus) PublishEvent(payload []byte) {
	_ = e.backend.Publish(e.ctx, eventsChannel, payload)
}

func (e *EventBus) Incoming() <-chan []byte { return e.incoming }

// CommandBus is the Redis-backed Remote Control command fan-out. A command is
// broadcast to every replica; the one holding the target session's socket
// delivers it and the rest ignore it, the same shape as EventBus but on its own
// channel so the two never cross.
type CommandBus struct {
	backend  *infra.Backend
	ctx      context.Context
	incoming <-chan []byte
}

// NewCommandBus subscribes to the shared Remote Control channel for the server's
// lifetime.
func NewCommandBus(ctx context.Context, backend *infra.Backend) *CommandBus {
	return &CommandBus{
		backend:  backend,
		ctx:      ctx,
		incoming: backend.Subscribe(ctx, remoteControlChannel),
	}
}

func (c *CommandBus) PublishCommand(payload []byte) {
	_ = c.backend.Publish(c.ctx, remoteControlChannel, payload)
}

func (c *CommandBus) Incoming() <-chan []byte { return c.incoming }

// TurnLocker is the Redis-backed turnqueue.Locker.
type TurnLocker struct {
	backend *infra.Backend
}

// NewTurnLocker returns a locker over the coordination backend.
func NewTurnLocker(backend *infra.Backend) *TurnLocker {
	return &TurnLocker{backend: backend}
}

func (l *TurnLocker) Acquire(ctx context.Context, conversationID string) (turnqueue.Lease, error) {
	lease, err := l.backend.AcquireLock(ctx, convLockPrefix+conversationID, turnLeaseTTL)
	if err != nil {
		return nil, err
	}
	return lease, nil
}
