// Package remotesession is the account-scoped registry of live, device-resident
// Agent sessions exposed through Remote Control. A local CLI/TUI or Desktop
// session dials out to the server, registers itself here against its owning
// user, and heartbeats; other devices discover and observe it by this record.
//
// It is deliberately account-scoped, not Space-scoped: the thing exposed is the
// user's own machine and its in-flight local work, which has no Space until the
// user publishes something. See docs/design/remote-control.md.
package remotesession

import (
	"context"
	"errors"
	"time"
)

// Status is the presence of a registered session.
type Status string

const (
	// StatusOnline means the session's control channel is connected and
	// heartbeating.
	StatusOnline Status = "online"
	// StatusOffline means the channel disconnected cleanly, or its heartbeats
	// lapsed past the liveness grace and a reaper retired it.
	StatusOffline Status = "offline"
)

// ErrNotFound means no such session, used when resolving a session by its public
// id (for an ownership-checked stream, say).
var ErrNotFound = errors.New("remote session not found")

// RemoteSession is one live local session made reachable through the server.
type RemoteSession struct {
	// ID is the public session identifier: the stream key and the handle another
	// device opens.
	ID string
	// UserID is the owning account. Only this user may see or observe the session.
	UserID string
	// DisplayName is a human label for the session list ("myhost — my project").
	DisplayName string
	// Platform is the surface that registered it ("cli", "desktop"), a label for
	// the operator rather than something the server enforces.
	Platform string
	// Host is the machine name the session runs on, for the session list.
	Host string
	// Status is the current presence.
	Status Status
	// CreatedAt is when the session registered.
	CreatedAt time.Time
	// LastSeenAt is the most recent heartbeat. Zero until the first touch.
	LastSeenAt time.Time
}

// NewRemoteSession describes a session to register.
type NewRemoteSession struct {
	UserID      string
	DisplayName string
	Platform    string
	Host        string
}

// Store owns the lifecycle of the live-session registry. Presence is a persisted
// fact so any server replica answers "which of my sessions are live" from the
// database rather than from a per-replica socket table.
type Store interface {
	// RegisterRemoteSession records a newly connected session as online and
	// returns its public id, which becomes the stream key.
	RegisterRemoteSession(ctx context.Context, in NewRemoteSession) (id string, err error)

	// TouchRemoteSession records a heartbeat: it stamps last-seen and keeps the
	// session online (flipping it back if a reaper had retired it while the
	// channel stayed up). A missing session is not an error.
	TouchRemoteSession(ctx context.Context, id string, now time.Time) error

	// MarkRemoteSessionOffline retires one session on a clean disconnect. A
	// missing or already-offline session is not an error.
	MarkRemoteSessionOffline(ctx context.Context, id string, now time.Time) error

	// MarkStaleRemoteSessionsOffline retires every online session whose last-seen
	// is at or before the cutoff, returning how many it retired. This is the
	// backstop for an unclean drop, run by the reaper.
	MarkStaleRemoteSessionsOffline(ctx context.Context, cutoff time.Time) (int64, error)

	// GetRemoteSession returns the session by public id, or ErrNotFound. The
	// caller checks ownership before observing it.
	GetRemoteSession(ctx context.Context, id string) (RemoteSession, error)

	// ListRemoteSessionsByUser returns the user's sessions, newest first, for the
	// session list.
	ListRemoteSessionsByUser(ctx context.Context, userID string) ([]RemoteSession, error)
}
