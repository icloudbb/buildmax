package scheduler

import (
	"context"
	"testing"
	"time"
)

type fakeRemoteSessionStore struct {
	cutoff time.Time
	called int
	n      int64
}

func (f *fakeRemoteSessionStore) MarkStaleRemoteSessionsOffline(_ context.Context, cutoff time.Time) (int64, error) {
	f.cutoff = cutoff
	f.called++
	return f.n, nil
}

// Sweep asks the store to retire sessions whose last heartbeat is older than the
// grace, passing now-grace as the cutoff.
func TestRemoteSessionReaperSweep(t *testing.T) {
	store := &fakeRemoteSessionStore{n: 3}
	r := NewRemoteSessionReaper(store, 90*time.Second, time.Minute)
	now := time.Now()

	r.Sweep(context.Background(), now)

	if store.called != 1 {
		t.Fatalf("store called %d times, want 1", store.called)
	}
	want := now.Add(-90 * time.Second)
	if !store.cutoff.Equal(want) {
		t.Errorf("cutoff = %v, want %v", store.cutoff, want)
	}
}

// A nil store yields a nil reaper whose lifecycle methods are safe no-ops, so a
// deployment without a database need not guard the calls.
func TestNewRemoteSessionReaperNilStore(t *testing.T) {
	r := NewRemoteSessionReaper(nil, 0, 0)
	if r != nil {
		t.Fatal("expected nil reaper for nil store")
	}
	r.Start()
	r.Stop()
	r.Sweep(context.Background(), time.Now())
}
