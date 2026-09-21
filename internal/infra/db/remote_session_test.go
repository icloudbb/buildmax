package db

import (
	"testing"
	"time"

	coreremote "github.com/icloudbb/buildmax/internal/core/remotesession"
)

// The Remote Control session registry: register online, heartbeat, list, mark
// offline, and the stale-sweep backstop. Runs only against a real database (the
// ordinary suite skips it, like every other store integration test).
func TestRemoteSessionLifecycle(t *testing.T) {
	s, ctx := newTestStore(t)
	userID := newTestUser(t, s, "remote")

	id, err := s.RegisterRemoteSession(ctx, coreremote.NewRemoteSession{
		UserID: userID, DisplayName: "myhost — proj", Platform: "cli", Host: "myhost",
	})
	if err != nil {
		t.Fatalf("RegisterRemoteSession: %v", err)
	}
	if id == "" {
		t.Fatal("empty session id")
	}

	got, err := s.GetRemoteSession(ctx, id)
	if err != nil {
		t.Fatalf("GetRemoteSession: %v", err)
	}
	if got.UserID != userID {
		t.Errorf("owner = %q, want %q", got.UserID, userID)
	}
	if got.Status != coreremote.StatusOnline {
		t.Errorf("status = %q, want online", got.Status)
	}
	if got.Platform != "cli" || got.Host != "myhost" {
		t.Errorf("platform/host not persisted: %+v", got)
	}
	if got.LastSeenAt.IsZero() {
		t.Error("register should stamp last_seen_at")
	}

	// A heartbeat keeps it online and advances last-seen.
	later := time.Now().UTC().Add(time.Minute)
	if err := s.TouchRemoteSession(ctx, id, later); err != nil {
		t.Fatalf("TouchRemoteSession: %v", err)
	}

	list, err := s.ListRemoteSessionsByUser(ctx, userID)
	if err != nil {
		t.Fatalf("ListRemoteSessionsByUser: %v", err)
	}
	if len(list) != 1 || list[0].ID != id {
		t.Fatalf("list = %+v, want one session %q", list, id)
	}

	// A clean disconnect marks it offline.
	if err := s.MarkRemoteSessionOffline(ctx, id, time.Now().UTC()); err != nil {
		t.Fatalf("MarkRemoteSessionOffline: %v", err)
	}
	got, _ = s.GetRemoteSession(ctx, id)
	if got.Status != coreremote.StatusOffline {
		t.Errorf("after offline, status = %q, want offline", got.Status)
	}
}

// The reaper query retires only online sessions whose last-seen is at or before
// the cutoff, and never one that is still fresh.
func TestMarkStaleRemoteSessionsOffline(t *testing.T) {
	s, ctx := newTestStore(t)
	userID := newTestUser(t, s, "remotestale")

	stale, err := s.RegisterRemoteSession(ctx, coreremote.NewRemoteSession{UserID: userID, Platform: "cli"})
	if err != nil {
		t.Fatalf("register stale: %v", err)
	}
	fresh, err := s.RegisterRemoteSession(ctx, coreremote.NewRemoteSession{UserID: userID, Platform: "cli"})
	if err != nil {
		t.Fatalf("register fresh: %v", err)
	}
	// Age the stale one well past the cutoff; keep the fresh one recent.
	now := time.Now().UTC()
	if err := s.TouchRemoteSession(ctx, stale, now.Add(-10*time.Minute)); err != nil {
		t.Fatalf("age stale: %v", err)
	}
	if err := s.TouchRemoteSession(ctx, fresh, now); err != nil {
		t.Fatalf("touch fresh: %v", err)
	}

	n, err := s.MarkStaleRemoteSessionsOffline(ctx, now.Add(-5*time.Minute))
	if err != nil {
		t.Fatalf("MarkStaleRemoteSessionsOffline: %v", err)
	}
	if n != 1 {
		t.Fatalf("retired %d, want 1", n)
	}
	if g, _ := s.GetRemoteSession(ctx, stale); g.Status != coreremote.StatusOffline {
		t.Errorf("stale session still %q", g.Status)
	}
	if g, _ := s.GetRemoteSession(ctx, fresh); g.Status != coreremote.StatusOnline {
		t.Errorf("fresh session was retired: %q", g.Status)
	}
}
