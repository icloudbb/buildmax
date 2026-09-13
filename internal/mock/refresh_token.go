package mock

import (
	"context"
	"fmt"
	"sync"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
)

// MockRefreshToken is one issued token in MockRefreshTokenStore.
type MockRefreshToken struct {
	UserID    string
	SessionID string
	Platform  string
	CreatedAt time.Time
	ExpiresAt time.Time
	UsedAt    *time.Time
	RevokedAt *time.Time
}

// MockRefreshTokenStore is an in-memory RefreshTokenStore for tests.
//
// It reproduces the rotation rules the real store enforces — spend on
// exchange, a grace window for concurrent callers, revoke the session on reuse
// — because those rules are what the handlers branch on. A mock that always
// succeeded would let a handler test pass while the endpoint handed a replayed
// credential a fresh session.
type MockRefreshTokenStore struct {
	mu     sync.Mutex
	Tokens map[string]*MockRefreshToken
	// CreateErr and RotateErr force a store failure.
	CreateErr error
	RotateErr error
	// issued counts tokens minted, so each plaintext is distinct.
	issued int
}

func (m *MockRefreshTokenStore) CreateRefreshToken(_ context.Context, in coreidentity.NewRefreshToken) (string, time.Time, error) {
	if m.CreateErr != nil {
		return "", time.Time{}, m.CreateErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mint(in.UserID, in.SessionID, in.Platform, time.Now().UTC(), in.TTL)
}

// mint requires m.mu.
func (m *MockRefreshTokenStore) mint(userID, sessionID, platform string, now time.Time, ttl time.Duration) (string, time.Time, error) {
	if m.Tokens == nil {
		m.Tokens = make(map[string]*MockRefreshToken)
	}
	if ttl <= 0 {
		ttl = coreidentity.RefreshTokenTTLDefault
	}
	m.issued++
	plaintext := fmt.Sprintf("mock-refresh-%s-%d", userID, m.issued)
	expiresAt := now.Add(ttl)
	m.Tokens[plaintext] = &MockRefreshToken{
		UserID:    userID,
		SessionID: sessionID,
		Platform:  platform,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}
	return plaintext, expiresAt, nil
}

func (m *MockRefreshTokenStore) RotateRefreshToken(_ context.Context, plaintext string, now time.Time, ttl, grace time.Duration) (coreidentity.RotatedRefreshToken, error) {
	if m.RotateErr != nil {
		return coreidentity.RotatedRefreshToken{}, m.RotateErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.Tokens[plaintext]
	if !ok || row.RevokedAt != nil || !row.ExpiresAt.After(now) {
		return coreidentity.RotatedRefreshToken{}, coreidentity.ErrRefreshTokenInvalid
	}
	if row.UsedAt != nil && now.Sub(*row.UsedAt) > grace {
		m.revokeSession(row.SessionID, now)
		// The caller needs to know whose session was just revoked in order to
		// record it, so the identifiers come back alongside the error.
		return coreidentity.RotatedRefreshToken{UserID: row.UserID, SessionID: row.SessionID}, coreidentity.ErrRefreshTokenReused
	}
	if row.UsedAt == nil {
		spent := now
		row.UsedAt = &spent
	}
	next, expiresAt, err := m.mint(row.UserID, row.SessionID, row.Platform, now, ttl)
	if err != nil {
		return coreidentity.RotatedRefreshToken{}, err
	}
	return coreidentity.RotatedRefreshToken{
		UserID:    row.UserID,
		SessionID: row.SessionID,
		Plaintext: next,
		ExpiresAt: expiresAt,
	}, nil
}

// Backdate moves a spent token's used_at further into the past, so a test can
// put a token outside the rotation grace window without waiting for the clock.
// It reports whether the token was there to move.
func (m *MockRefreshTokenStore) Backdate(plaintext string, by time.Duration) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.Tokens[plaintext]
	if !ok || row.UsedAt == nil {
		return false
	}
	moved := row.UsedAt.Add(-by)
	row.UsedAt = &moved
	return true
}

func (m *MockRefreshTokenStore) RevokeRefreshTokenSession(_ context.Context, plaintext string, now time.Time) (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.Tokens[plaintext]
	if !ok {
		return "", "", nil
	}
	m.revokeSession(row.SessionID, now)
	return row.UserID, row.SessionID, nil
}

// revokeSession requires m.mu. Session-level revocation is owned by
// MockAuthSessionStore now; this stays because the rotation reuse path and
// RevokeRefreshTokenSession retire a chain's tokens through it.
func (m *MockRefreshTokenStore) revokeSession(sessionID string, now time.Time) int64 {
	var n int64
	for _, row := range m.Tokens {
		if row.SessionID == sessionID && row.RevokedAt == nil {
			revoked := now
			row.RevokedAt = &revoked
			n++
		}
	}
	return n
}

func (m *MockRefreshTokenStore) DeleteExpiredRefreshTokens(_ context.Context, before time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for plaintext, row := range m.Tokens {
		if !row.ExpiresAt.After(before) {
			delete(m.Tokens, plaintext)
			n++
		}
	}
	return n, nil
}
