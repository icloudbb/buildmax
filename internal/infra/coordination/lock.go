package coordination

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// lockRetry is how long AcquireLock waits between attempts while another holder
// has the lock.
const lockRetry = 50 * time.Millisecond

// acquireScript grants the lock only when it is free, issuing a monotonic fence
// token from a per-key counter. It returns the fence on success or -1 when held,
// so a caller never overwrites a live holder's lock.
//
// Redis losing its data, as a restart without persistence does, loses the
// counter too. A missing counter therefore starts from the clock in
// microseconds rather than from zero, so a fence issued after the loss still
// exceeds the ones writes were fenced with before it. And the key holds the
// fence with a nonce unique to this grant, so even an equal fence never lets an
// old holder renew a lock granted since.
var acquireScript = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 1 then return -1 end
if redis.call('EXISTS', KEYS[2]) == 0 then redis.call('SET', KEYS[2], ARGV[3]) end
local fence = redis.call('INCR', KEYS[2])
redis.call('SET', KEYS[1], fence .. ':' .. ARGV[2], 'PX', ARGV[1])
return fence
`)

// renewScript extends the lease only while this holder still owns it, so a lease
// that already expired and was re-granted is not silently resurrected.
var renewScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('PEXPIRE', KEYS[1], ARGV[2])
end
return 0
`)

// releaseScript deletes the lock only while this holder still owns it.
var releaseScript = redis.NewScript(`
if redis.call('GET', KEYS[1]) == ARGV[1] then
  return redis.call('DEL', KEYS[1])
end
return 0
`)

// Lease is a held distributed lock. Release stops the renewer and drops the lock
// if this holder still owns it.
type Lease struct {
	backend *Backend
	lockKey string
	fence   int64
	// holder is the value this grant stored under lockKey; renew and release
	// act only while the key still holds it.
	holder   string
	cancel   context.CancelFunc
	stopOnce sync.Once
	done     chan struct{}
	// lost closes when renewal finds the lock gone or cannot confirm it for a
	// whole TTL, so a holder doing long work learns it no longer has exclusivity
	// instead of carrying on beside the next holder.
	lost     chan struct{}
	lostOnce sync.Once
}

// Fence returns the monotonic token issued when the lock was granted. A later
// grant of the same lock always carries a higher token.
func (l *Lease) Fence() int64 { return l.fence }

// Lost is closed once this holder can no longer be sure it holds the lock.
// A holder that only needs the lock for one short critical section can ignore
// it; one that holds it indefinitely must stop when it closes.
func (l *Lease) Lost() <-chan struct{} { return l.lost }

func (l *Lease) markLost() { l.lostOnce.Do(func() { close(l.lost) }) }

// Release stops renewal and drops the lock. It is safe to call more than once.
func (l *Lease) Release() {
	l.stopOnce.Do(func() {
		l.cancel()
		<-l.done // wait for the renewer to stop before deleting the key
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = releaseScript.Run(ctx, l.backend.rdb, []string{l.lockKey}, l.holder).Err()
	})
}

// AcquireLock blocks until it holds the lock for key or ctx is cancelled. The
// lease renews itself on an interval derived from ttl until Release; if the
// holder dies without releasing, the lease expires after ttl and another caller
// can acquire it.
func (b *Backend) AcquireLock(ctx context.Context, key string, ttl time.Duration) (*Lease, error) {
	lockKey := "lock:" + key
	fenceKey := "fence:" + key
	ttlMillis := strconv.FormatInt(ttl.Milliseconds(), 10)

	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		nonce, err := newNonce()
		if err != nil {
			return nil, err
		}
		res, err := acquireScript.Run(ctx, b.rdb, []string{lockKey, fenceKey}, ttlMillis, nonce, fenceFloor()).Int64()
		if err != nil {
			return nil, err
		}
		if res > 0 {
			return b.startLease(lockKey, res, nonce, ttl), nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(lockRetry):
		}
	}
}

// TryAcquireLock takes the lock for key if it is free and reports false
// without waiting if another holder has it. It suits a standby that retries on
// its own slow schedule, where AcquireLock's tight retry would poll Redis many
// times a second for as long as the holder lives.
func (b *Backend) TryAcquireLock(ctx context.Context, key string, ttl time.Duration) (*Lease, bool, error) {
	lockKey := "lock:" + key
	fenceKey := "fence:" + key
	nonce, err := newNonce()
	if err != nil {
		return nil, false, err
	}
	res, err := acquireScript.Run(ctx, b.rdb, []string{lockKey, fenceKey}, strconv.FormatInt(ttl.Milliseconds(), 10), nonce, fenceFloor()).Int64()
	if err != nil {
		return nil, false, err
	}
	if res <= 0 {
		return nil, false, nil
	}
	return b.startLease(lockKey, res, nonce, ttl), true, nil
}

// fenceFloor is where a missing fence counter starts: the clock, which has
// moved past every fence issued before Redis lost the counter unless one key
// was granted more than once a microsecond.
func fenceFloor() string { return strconv.FormatInt(time.Now().UnixMicro(), 10) }

func newNonce() (string, error) {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf[:]), nil
}

// startLease begins renewing a granted lock on its own goroutine.
func (b *Backend) startLease(lockKey string, fence int64, nonce string, ttl time.Duration) *Lease {
	renewCtx, cancel := context.WithCancel(context.Background())
	lease := &Lease{
		backend: b,
		lockKey: lockKey,
		fence:   fence,
		holder:  strconv.FormatInt(fence, 10) + ":" + nonce,
		cancel:  cancel,
		done:    make(chan struct{}),
		lost:    make(chan struct{}),
	}
	interval := ttl / 3
	if interval <= 0 {
		interval = ttl
	}
	go func() {
		defer close(lease.done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		confirmed := time.Now()
		for {
			select {
			case <-renewCtx.Done():
				return
			case <-ticker.C:
				ctx, c := context.WithTimeout(renewCtx, ttl)
				renewed, err := renewScript.Run(ctx, b.rdb, []string{lockKey}, lease.holder, strconv.FormatInt(ttl.Milliseconds(), 10)).Int64()
				c()
				switch {
				case err == nil && renewed == 1:
					confirmed = time.Now()
				case err == nil:
					// The key expired or was re-granted: someone else may hold it.
					lease.markLost()
					return
				case time.Since(confirmed) >= ttl:
					// Unreachable for a whole TTL: the key has expired by now.
					lease.markLost()
					return
				}
			}
		}
	}()
	return lease
}
