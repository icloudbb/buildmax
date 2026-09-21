package cli

import (
	"context"
	"sync"

	tea "charm.land/bubbletea/v2"
)

// tuiRunOwner owns every foreground agent goroutine started by one TUI. The
// program may stop receiving stream messages before a run returns, so sends
// and the run itself share this cancellation boundary.
type tuiRunOwner struct {
	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
}

func newTUIRunOwner(parent context.Context) *tuiRunOwner {
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	return &tuiRunOwner{ctx: ctx, cancel: cancel}
}

func (o *tuiRunOwner) Go(run func(context.Context)) bool {
	if o == nil || run == nil {
		return false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return false
	}
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		run(o.ctx)
	}()
	return true
}

// GoCancelable runs like Go but on a child context whose returned cancel stops
// this one run without ending the owner, so a single turn can be interrupted
// (locally or from a remote device) while the TUI keeps going. The cancel is nil
// when the run could not start.
func (o *tuiRunOwner) GoCancelable(run func(context.Context)) (context.CancelFunc, bool) {
	if o == nil || run == nil {
		return nil, false
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return nil, false
	}
	ctx, cancel := context.WithCancel(o.ctx)
	o.wg.Add(1)
	go func() {
		defer o.wg.Done()
		defer cancel()
		run(ctx)
	}()
	return cancel, true
}

func (o *tuiRunOwner) Cancel() {
	if o != nil {
		o.cancel()
	}
}

func (o *tuiRunOwner) Close() {
	if o == nil {
		return
	}
	o.mu.Lock()
	if o.closed {
		o.mu.Unlock()
		return
	}
	o.closed = true
	o.cancel()
	o.mu.Unlock()
	o.wg.Wait()
}

func sendTUIMessage(ctx context.Context, channel chan<- tea.Msg, msg tea.Msg) bool {
	select {
	case channel <- msg:
		return true
	case <-ctx.Done():
		return false
	}
}
