package coordination

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

// newTestBackend starts an in-process Redis and returns a backend pointing at it.
func newTestBackend(t *testing.T) *Backend {
	t.Helper()
	mr := miniredis.RunT(t)
	b, err := New(context.Background(), Options{Address: mr.Addr()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = b.Close() })
	return b
}

func TestNewFailsClosedOnUnreachableRedis(t *testing.T) {
	// A port nothing listens on: New must return an error rather than a backend,
	// so the caller can refuse to start.
	_, err := New(context.Background(), Options{Address: "127.0.0.1:6390"})
	if err == nil {
		t.Fatal("New with an unreachable address returned no error; the server would serve with no coordination")
	}
}

func TestPublishReachesASubscriberOnAnotherBackend(t *testing.T) {
	mr := miniredis.RunT(t)
	pub, err := New(context.Background(), Options{Address: mr.Addr()})
	if err != nil {
		t.Fatalf("New pub: %v", err)
	}
	defer func() { _ = pub.Close() }()
	sub, err := New(context.Background(), Options{Address: mr.Addr()})
	if err != nil {
		t.Fatalf("New sub: %v", err)
	}
	defer func() { _ = sub.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	msgs := sub.Subscribe(ctx, "events")
	// Give the subscription a moment to register before publishing.
	time.Sleep(50 * time.Millisecond)

	if err := pub.Publish(context.Background(), "events", []byte("hello")); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	select {
	case got := <-msgs:
		if string(got) != "hello" {
			t.Fatalf("got %q, want hello", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber on a second backend never received the published event")
	}
}

func TestStreamAppendSnapshotAndTail(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	key := "stream:task1"

	tailCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	tail := b.StreamTail(tailCtx, key)
	time.Sleep(50 * time.Millisecond) // let the tail read the current end

	if err := b.StreamAppend(ctx, key, "alpha", 100, time.Minute); err != nil {
		t.Fatalf("append: %v", err)
	}
	if err := b.StreamAppend(ctx, key, "beta", 100, time.Minute); err != nil {
		t.Fatalf("append: %v", err)
	}

	if got := collect(t, tail, 2); got[0] != "alpha" || got[1] != "beta" {
		t.Fatalf("tail delivered %v, want [alpha beta]", got)
	}

	snap, err := b.StreamSnapshot(ctx, key)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap != "alphabeta" {
		t.Fatalf("snapshot = %q, want alphabeta", snap)
	}

	if err := b.StreamDone(ctx, key, time.Minute); err != nil {
		t.Fatalf("done: %v", err)
	}
	select {
	case got, ok := <-tail:
		if !ok {
			t.Fatal("tail closed before delivering the done marker")
		}
		if got != DoneMarker {
			t.Fatalf("expected DoneMarker, got %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("tail never delivered the done marker")
	}
}

// collect reads n values from a tail channel or fails.
func collect(t *testing.T, ch <-chan string, n int) []string {
	t.Helper()
	out := make([]string, 0, n)
	for len(out) < n {
		select {
		case v := <-ch:
			out = append(out, v)
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out after %d of %d values: %v", len(out), n, out)
		}
	}
	return out
}

func TestLockSerializesAcrossBackends(t *testing.T) {
	mr := miniredis.RunT(t)
	a, err := New(context.Background(), Options{Address: mr.Addr()})
	if err != nil {
		t.Fatalf("New a: %v", err)
	}
	defer func() { _ = a.Close() }()
	b, err := New(context.Background(), Options{Address: mr.Addr()})
	if err != nil {
		t.Fatalf("New b: %v", err)
	}
	defer func() { _ = b.Close() }()

	ctx := context.Background()
	lease1, err := a.AcquireLock(ctx, "conv:1", time.Minute)
	if err != nil {
		t.Fatalf("acquire a: %v", err)
	}

	// A second backend must not acquire the same lock while a holds it.
	acquired := make(chan *Lease, 1)
	go func() {
		l, err := b.AcquireLock(ctx, "conv:1", time.Minute)
		if err != nil {
			return
		}
		acquired <- l
	}()
	select {
	case <-acquired:
		t.Fatal("a second backend acquired a lock the first still held")
	case <-time.After(300 * time.Millisecond):
		// expected: still blocked
	}

	lease1.Release()

	select {
	case lease2 := <-acquired:
		if lease2.Fence() <= lease1.Fence() {
			t.Fatalf("second grant fence %d not greater than first %d", lease2.Fence(), lease1.Fence())
		}
		lease2.Release()
	case <-time.After(2 * time.Second):
		t.Fatal("second backend never acquired the lock after release")
	}
}

func TestAcquireLockRespectsContextCancel(t *testing.T) {
	b := newTestBackend(t)
	lease, err := b.AcquireLock(context.Background(), "conv:x", time.Minute)
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	defer lease.Release()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if _, err := b.AcquireLock(ctx, "conv:x", time.Minute); err == nil {
		t.Fatal("a blocked acquire returned no error when its context expired")
	}
}

func TestTryAcquireLockRefusesAHeldLockWithoutWaiting(t *testing.T) {
	b := newTestBackend(t)
	ctx := context.Background()
	first, ok, err := b.TryAcquireLock(ctx, "connector", time.Second)
	if err != nil || !ok {
		t.Fatalf("first TryAcquireLock = %v, %v; want the free lock", ok, err)
	}
	defer first.Release()
	if _, ok, err := b.TryAcquireLock(ctx, "connector", time.Second); err != nil || ok {
		t.Fatalf("second TryAcquireLock = %v, %v; want refused while held", ok, err)
	}
	first.Release()
	second, ok, err := b.TryAcquireLock(ctx, "connector", time.Second)
	if err != nil || !ok {
		t.Fatalf("TryAcquireLock after release = %v, %v; want acquired", ok, err)
	}
	second.Release()
}

// A holder whose key vanished must be told, or it keeps consuming beside the
// replica that takes the lock next.
func TestLeaseReportsLossWhenItsKeyIsGone(t *testing.T) {
	mr := miniredis.RunT(t)
	b, err := New(context.Background(), Options{Address: mr.Addr()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = b.Close() }()
	lease, ok, err := b.TryAcquireLock(context.Background(), "connector", 300*time.Millisecond)
	if err != nil || !ok {
		t.Fatalf("TryAcquireLock = %v, %v", ok, err)
	}
	defer lease.Release()
	select {
	case <-lease.Lost():
		t.Fatal("lease reported lost while its key was still held")
	case <-time.After(250 * time.Millisecond):
	}
	mr.Del("lock:connector")
	select {
	case <-lease.Lost():
	case <-time.After(2 * time.Second):
		t.Fatal("lease never reported the lost lock")
	}
}
