package browser

import (
	"context"
	"testing"
	"time"
)

// A navigation that begins shortly after an interaction is waited out, so the
// state read afterwards describes the page arrived at, not the one left.
func TestAwaitNavigationWaitsForANavigationTheInteractionStarted(t *testing.T) {
	p := &chromedpPage{}
	before, _ := p.nav.snapshot()
	go func() {
		time.Sleep(30 * time.Millisecond)
		p.nav.begin()
		time.Sleep(200 * time.Millisecond)
		p.nav.end()
	}()
	start := time.Now()
	p.awaitNavigation(context.Background(), before)
	if waited := time.Since(start); waited < 200*time.Millisecond {
		t.Fatalf("returned after %v, before the navigation finished loading", waited)
	}
}

// An interaction that navigates nothing waits only the start window.
func TestAwaitNavigationReturnsWhenNothingNavigates(t *testing.T) {
	p := &chromedpPage{}
	before, _ := p.nav.snapshot()
	start := time.Now()
	p.awaitNavigation(context.Background(), before)
	if waited := time.Since(start); waited > navigationStartWindow+200*time.Millisecond {
		t.Fatalf("waited %v with no navigation", waited)
	}
}

// A navigation that never finishes is bounded by the caller's context.
func TestAwaitNavigationIsBoundedByTheContext(t *testing.T) {
	p := &chromedpPage{}
	before, _ := p.nav.snapshot()
	p.nav.begin()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	p.awaitNavigation(ctx, before)
	if waited := time.Since(start); waited > time.Second {
		t.Fatalf("waited %v past the context deadline", waited)
	}
}
