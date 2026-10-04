package scheduler

import (
	"context"
	"time"
)

// defaultDeliverySweepInterval is how long a finished run can wait before its
// result is sent. Short, because a person is waiting for it in a chat.
const defaultDeliverySweepInterval = 15 * time.Second

// DeliverySettler sends, or records why it did not send, what finished runs
// owe a person through an Assistant: the results of schedule firings that
// deliver through it, and the outcome of Workflow runs it started.
type DeliverySettler interface {
	SweepDeliveries(ctx context.Context)
}

// DeliverySweeper settles those from durable state, so a result
// is delivered whichever replica saw its run end, and after a restart. Each
// delivery is claimed before it is sent, so every replica runs one.
type DeliverySweeper struct {
	settler  DeliverySettler
	interval time.Duration
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// NewDeliverySweeper returns a sweeper, or nil when there is nothing to settle
// with, so a caller need not check before starting it. Zero uses the default
// interval.
func NewDeliverySweeper(settler DeliverySettler, interval time.Duration) *DeliverySweeper {
	if settler == nil {
		return nil
	}
	if interval <= 0 {
		interval = defaultDeliverySweepInterval
	}
	return &DeliverySweeper{settler: settler, interval: interval, stopCh: make(chan struct{}), doneCh: make(chan struct{})}
}

// Start launches the sweep loop. Calling it on a nil sweeper is a no-op.
func (d *DeliverySweeper) Start() {
	if d == nil {
		return
	}
	go d.loop()
	componentLog("schedule_delivery").Info("started", "interval", d.interval)
}

// Stop signals the loop to exit and blocks until it has finished.
func (d *DeliverySweeper) Stop() {
	if d == nil {
		return
	}
	close(d.stopCh)
	<-d.doneCh
	componentLog("schedule_delivery").Info("stopped")
}

func (d *DeliverySweeper) loop() {
	defer close(d.doneCh)
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for {
		select {
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.settler.SweepDeliveries(context.Background())
		}
	}
}
