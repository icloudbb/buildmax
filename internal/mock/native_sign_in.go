package mock

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
)

// MockNativeSignIn is one stored request, keyed by hashes the way the real
// store keeps it.
type MockNativeSignIn struct {
	UserCodeHash   string
	PollSecretHash string
	Request        coreidentity.NativeSignIn
}

// MockNativeSignInStore is an in-memory NativeSignInStore for tests.
//
// It reproduces the rules the service and handlers branch on: first decision
// wins, pacing under one lock, and redemption that re-checks the account and
// the confirming session, creates the session through Sessions and its refresh
// token through Refresh, and removes the request — all under one mutex, so
// concurrent redemptions produce exactly one session, as the real store's row
// lock does.
type MockNativeSignInStore struct {
	mu       sync.Mutex
	Requests []*MockNativeSignIn
	Users    *MockUserStore
	Sessions *MockAuthSessionStore
	Refresh  *MockRefreshTokenStore
}

func mockHash(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])
}

// requires m.mu
func (m *MockNativeSignInStore) find(match func(*MockNativeSignIn) bool) (int, *MockNativeSignIn) {
	for i, r := range m.Requests {
		if match(r) {
			return i, r
		}
	}
	return -1, nil
}

func (m *MockNativeSignInStore) CountPendingNativeSignIns(_ context.Context, now time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range m.Requests {
		if r.Request.State == coreidentity.NativeSignInPending && now.Before(r.Request.ExpiresAt) {
			n++
		}
	}
	return n, nil
}

func (m *MockNativeSignInStore) CreateNativeSignIn(_ context.Context, in coreidentity.NewNativeSignIn) error {
	if in.UserCode == "" || in.PollSecret == "" {
		return errors.New("native sign-in: user code and poll secret required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	codeHash := mockHash(in.UserCode)
	if _, r := m.find(func(r *MockNativeSignIn) bool { return r.UserCodeHash == codeHash }); r != nil {
		return coreidentity.ErrUserCodeTaken
	}
	m.Requests = append(m.Requests, &MockNativeSignIn{
		UserCodeHash:   codeHash,
		PollSecretHash: mockHash(in.PollSecret),
		Request: coreidentity.NativeSignIn{
			Platform:    in.Platform,
			ClientLabel: in.ClientLabel,
			State:       coreidentity.NativeSignInPending,
			Interval:    coreidentity.NativeSignInInterval,
			CreatedAt:   in.CreatedAt,
			ExpiresAt:   in.ExpiresAt,
		},
	})
	return nil
}

func (m *MockNativeSignInStore) PendingNativeSignIn(_ context.Context, userCode string, now time.Time) (coreidentity.NativeSignIn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	codeHash := mockHash(userCode)
	_, r := m.find(func(r *MockNativeSignIn) bool {
		return r.UserCodeHash == codeHash && r.Request.State == coreidentity.NativeSignInPending && now.Before(r.Request.ExpiresAt)
	})
	if userCode == "" || r == nil {
		return coreidentity.NativeSignIn{}, coreidentity.ErrNativeSignInNotFound
	}
	return r.Request, nil
}

func (m *MockNativeSignInStore) DecideNativeSignIn(_ context.Context, userCode string, d coreidentity.NativeSignInDecision) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	codeHash := mockHash(userCode)
	_, r := m.find(func(r *MockNativeSignIn) bool {
		return r.UserCodeHash == codeHash && r.Request.State == coreidentity.NativeSignInPending && d.At.Before(r.Request.ExpiresAt)
	})
	if userCode == "" || r == nil {
		return coreidentity.ErrNativeSignInNotFound
	}
	r.Request.State = d.State
	r.Request.DecidedByUserID = d.UserID
	r.Request.DecidedSessionID = d.SessionID
	r.Request.DecidedAt = d.At
	return nil
}

// requires m.mu
func (m *MockNativeSignInStore) live(pollSecret string, now time.Time) (int, *MockNativeSignIn) {
	if pollSecret == "" {
		return -1, nil
	}
	secretHash := mockHash(pollSecret)
	return m.find(func(r *MockNativeSignIn) bool {
		return r.PollSecretHash == secretHash && now.Before(r.Request.ExpiresAt)
	})
}

func (m *MockNativeSignInStore) PollNativeSignIn(_ context.Context, pollSecret string, now time.Time) (coreidentity.NativeSignIn, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, r := m.live(pollSecret, now)
	if r == nil {
		return coreidentity.NativeSignIn{}, false, coreidentity.ErrNativeSignInNotFound
	}
	slow := r.Request.Poll(now)
	return r.Request, slow, nil
}

func (m *MockNativeSignInStore) RedeemNativeSignIn(ctx context.Context, pollSecret string, now time.Time, refreshTTL time.Duration) (coreidentity.NativeSignInRedemption, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	i, r := m.live(pollSecret, now)
	if r == nil || r.Request.State != coreidentity.NativeSignInConfirmed {
		return coreidentity.NativeSignInRedemption{}, coreidentity.ErrNativeSignInNotFound
	}
	user, err := m.Users.GetUser(ctx, r.Request.DecidedByUserID)
	if err != nil {
		return coreidentity.NativeSignInRedemption{}, err
	}
	var confirming *coreidentity.AuthSession
	if s, err := m.Sessions.ActiveSession(ctx, r.Request.DecidedSessionID, now); err == nil {
		confirming = &s
	}
	if !r.Request.RedeemableBy(user, confirming, now) {
		r.Request.State = coreidentity.NativeSignInDenied
		return coreidentity.NativeSignInRedemption{}, coreidentity.ErrNativeSignInRefused
	}
	next := r.Request.SessionFor(confirming)
	sid, err := m.Sessions.CreateSession(ctx, next)
	if err != nil {
		return coreidentity.NativeSignInRedemption{}, err
	}
	refresh, _, err := m.Refresh.CreateRefreshToken(ctx, coreidentity.NewRefreshToken{
		UserID: next.UserID, SessionID: sid, Platform: next.Platform, TTL: refreshTTL,
	})
	if err != nil {
		return coreidentity.NativeSignInRedemption{}, err
	}
	m.Requests = append(m.Requests[:i], m.Requests[i+1:]...)
	return coreidentity.NativeSignInRedemption{
		UserID: next.UserID, SessionID: sid, Platform: next.Platform,
		AuthMethod: next.AuthMethod, RefreshToken: refresh,
	}, nil
}

func (m *MockNativeSignInStore) DeleteExpiredNativeSignIns(_ context.Context, before time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	kept := m.Requests[:0]
	var n int64
	for _, r := range m.Requests {
		if !r.Request.ExpiresAt.After(before) {
			n++
			continue
		}
		kept = append(kept, r)
	}
	m.Requests = kept
	return n, nil
}
