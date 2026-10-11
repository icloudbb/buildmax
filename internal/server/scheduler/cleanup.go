package scheduler

import (
	"context"
	"time"
)

// defaultCleanupInterval is how often expired credentials are swept. Nothing
// depends on the timing: an expired row is already refused by the code that
// reads it, and deleting it only keeps the tables from growing without end.
const defaultCleanupInterval = time.Hour

// ExpiredCredentialStore deletes credential rows that can no longer be
// redeemed. Each method returns how many rows it removed.
type ExpiredCredentialStore interface {
	DeleteExpiredLoginCodes(ctx context.Context, before time.Time) (int64, error)
	DeleteExpiredRefreshTokens(ctx context.Context, before time.Time) (int64, error)
	DeleteExpiredNativeSignIns(ctx context.Context, before time.Time) (int64, error)
}

// CredentialCleaner periodically removes expired login codes, refresh tokens,
// and native sign-in requests.
//
// No table is correctness-critical to sweep — expiry is enforced on read,
// not by absence — so a failed sweep is logged and retried on the next tick
// rather than stopping the server.
type CredentialCleaner struct {
	store    ExpiredCredentialStore
	interval time.Duration
	stopCh   chan struct{}
	doneCh   chan struct{}
}

// NewCredentialCleaner returns a cleaner for store. A nil store returns nil,
// so a deployment without a database simply has nothing to sweep. Use 0 for
// the default interval.
func NewCredentialCleaner(store ExpiredCredentialStore, interval time.Duration) *CredentialCleaner {
	if store == nil {
		return nil
	}
	if interval <= 0 {
		interval = defaultCleanupInterval
	}
	return &CredentialCleaner{
		store:    store,
		interval: interval,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// Start launches the sweep loop in a background goroutine. Calling it on a nil
// cleaner is a no-op, so the caller does not need to check.
func (c *CredentialCleaner) Start() {
	if c == nil {
		return
	}
	go c.loop()
	c.log().Info("started", "interval", c.interval)
}

// Stop signals the loop to exit and blocks until it has finished.
func (c *CredentialCleaner) Stop() {
	if c == nil {
		return
	}
	close(c.stopCh)
	<-c.doneCh
	c.log().Info("stopped")
}

func (c *CredentialCleaner) loop() {
	defer close(c.doneCh)
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	// Sweep once at startup as well, so a server that is restarted more often
	// than the interval still clears anything left behind.
	c.sweep()
	for {
		select {
		case <-c.stopCh:
			return
		case <-ticker.C:
			c.sweep()
		}
	}
}

func (c *CredentialCleaner) sweep() {
	ctx := context.Background()
	now := time.Now().UTC()
	codes, err := c.store.DeleteExpiredLoginCodes(ctx, now)
	if err != nil {
		c.log().Warn("delete expired login codes failed", "err", err)
	}
	tokens, err := c.store.DeleteExpiredRefreshTokens(ctx, now)
	if err != nil {
		c.log().Warn("delete expired refresh tokens failed", "err", err)
	}
	signIns, err := c.store.DeleteExpiredNativeSignIns(ctx, now)
	if err != nil {
		c.log().Warn("delete expired native sign-in requests failed", "err", err)
	}
	if codes > 0 || tokens > 0 || signIns > 0 {
		c.log().Info("expired credentials removed",
			"login_codes", codes, "refresh_tokens", tokens, "native_sign_ins", signIns)
	}
}
