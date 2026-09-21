package scheduler

import (
	"context"
	"time"
)

// defaultRemoteSessionGrace is how long a Remote Control session may go without a
// heartbeat before the server treats its control channel as gone and marks it
// offline. The agent socket heartbeats well inside this window, so the backstop
// only fires on an unclean drop that never reached the socket's own cleanup.
const defaultRemoteSessionGrace = 90 * time.Second

// defaultRemoteSessionSweepInterval is how often presence is swept.
const defaultRemoteSessionSweepInterval = 30 * time.Second

// RemoteSessionStore is the narrow surface the reaper needs.
type RemoteSessionStore interface {
	MarkStaleRemoteSessionsOffline(ctx context.Context, cutoff time.Time) (int64, error)
}

// RemoteSessionReaper marks a live session offline when its heartbeats lapse. A
// clean disconnect marks the session offline immediately on the socket; this is
// the backstop for a laptop that slept, lost the network, or was killed without
// the socket ever closing cleanly. See docs/design/remote-control.md.
type RemoteSessionReaper struct {
	sessions RemoteSessionStore
	grace    time.Duration
	interval time.Duration
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// NewRemoteSessionReaper returns a reaper, or nil when there is no store to
// sweep — so a caller need not check before starting it. Zero values use the
// defaults.
func NewRemoteSessionReaper(sessions RemoteSessionStore, grace, interval time.Duration) *RemoteSessionReaper {
	if sessions == nil {
		return nil
	}
	if grace <= 0 {
		grace = defaultRemoteSessionGrace
	}
	if interval <= 0 {
		interval = defaultRemoteSessionSweepInterval
	}
	return &RemoteSessionReaper{
		sessions: sessions,
		grace:    grace,
		interval: interval,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// Start launches the sweep loop. Calling it on a nil reaper is a no-op.
func (c *RemoteSessionReaper) Start() {
	if c == nil {
		return
	}
	go c.loop()
	c.log().Info("started", "grace", c.grace, "interval", c.interval)
}

// Stop signals the loop to exit and blocks until it has finished.
func (c *RemoteSessionReaper) Stop() {
	if c == nil {
		return
	}
	close(c.stopCh)
	<-c.doneCh
	c.log().Info("stopped")
}

func (c *RemoteSessionReaper) loop() {
	defer close(c.doneCh)
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.Sweep(context.Background(), time.Now())
		}
	}
}

// Sweep marks every online session whose last heartbeat is older than the grace
// offline. Exported so a test can drive one pass without a clock.
func (c *RemoteSessionReaper) Sweep(ctx context.Context, now time.Time) {
	if c == nil {
		return
	}
	n, err := c.sessions.MarkStaleRemoteSessionsOffline(ctx, now.Add(-c.grace))
	if err != nil {
		c.log().WarnContext(ctx, "remote session sweep failed", "err", err)
		return
	}
	if n > 0 {
		c.log().InfoContext(ctx, "marked stale remote sessions offline", "count", n)
	}
}
