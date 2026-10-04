package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

type countingSettler struct{ sweeps atomic.Int32 }

func (c *countingSettler) SweepDeliveries(context.Context) { c.sweeps.Add(1) }

// The sweeper settles on every tick until stopped, and is absent without a
// settler so a deployment without Assistants starts nothing.
func TestDeliverySweeperSweepsUntilStopped(t *testing.T) {
	if NewDeliverySweeper(nil, 0) != nil {
		t.Error("a sweeper with nothing to settle was built")
	}
	settler := &countingSettler{}
	d := NewDeliverySweeper(settler, 5*time.Millisecond)
	d.Start()
	deadline := time.Now().Add(2 * time.Second)
	for settler.sweeps.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	d.Stop()
	after := settler.sweeps.Load()
	if after < 2 {
		t.Fatalf("swept %d times, want at least 2", after)
	}
	time.Sleep(20 * time.Millisecond)
	if settler.sweeps.Load() != after {
		t.Error("the sweeper kept sweeping after Stop")
	}
}
