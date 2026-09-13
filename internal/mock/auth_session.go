package mock

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
)

// MockAuthSession is one session in MockAuthSessionStore.
type MockAuthSession struct {
	SID               string
	UserID            string
	Platform          string
	AuthMethod        string
	CreatedAt         time.Time
	LastSeenAt        time.Time
	AbsoluteExpiresAt time.Time
	RevokedAt         *time.Time
}

// MockAuthSessionStore is an in-memory AuthSessionStore for tests. It reproduces
// the authority rules the real store enforces — a session is inactive once
// revoked or past its absolute expiry — because those are what the guard and the
// refresh path branch on.
type MockAuthSessionStore struct {
	mu sync.Mutex
	// Sessions is keyed by sid.
	Sessions map[string]*MockAuthSession
	// CreateErr forces a store failure.
	CreateErr error
	// Refresh, when set, is revoked alongside a session so the mock cascade
	// mirrors the real store. Optional.
	Refresh *MockRefreshTokenStore
	issued  int
}

func (m *MockAuthSessionStore) CreateSession(_ context.Context, in coreidentity.NewAuthSession) (string, error) {
	if m.CreateErr != nil {
		return "", m.CreateErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Sessions == nil {
		m.Sessions = make(map[string]*MockAuthSession)
	}
	m.issued++
	sid := fmt.Sprintf("mock-session-%s-%d", in.UserID, m.issued)
	m.Sessions[sid] = &MockAuthSession{
		SID:               sid,
		UserID:            in.UserID,
		Platform:          in.Platform,
		AuthMethod:        in.AuthMethod,
		CreatedAt:         time.Now().UTC(),
		AbsoluteExpiresAt: in.AbsoluteExpiresAt,
	}
	return sid, nil
}

func (m *MockAuthSessionStore) ActiveSession(_ context.Context, sid string, now time.Time) (coreidentity.AuthSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.Sessions[sid]
	if !ok || s.RevokedAt != nil || !s.AbsoluteExpiresAt.After(now) {
		return coreidentity.AuthSession{}, coreidentity.ErrSessionInactive
	}
	return s.toCore(), nil
}

func (m *MockAuthSessionStore) TouchSession(_ context.Context, sid string, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.Sessions[sid]
	if !ok || s.RevokedAt != nil || !s.AbsoluteExpiresAt.After(now) {
		return nil
	}
	s.LastSeenAt = now
	return nil
}

func (m *MockAuthSessionStore) RevokeSession(_ context.Context, sid string, now time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	if s, ok := m.Sessions[sid]; ok && s.RevokedAt == nil {
		revoked := now
		s.RevokedAt = &revoked
		n = 1
	}
	if m.Refresh != nil {
		m.Refresh.mu.Lock()
		m.Refresh.revokeSession(sid, now)
		m.Refresh.mu.Unlock()
	}
	return n, nil
}

func (m *MockAuthSessionStore) RevokeUserSessions(_ context.Context, userID string, now time.Time) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int64
	for _, s := range m.Sessions {
		if s.UserID == userID && s.RevokedAt == nil {
			revoked := now
			s.RevokedAt = &revoked
			n++
			if m.Refresh != nil {
				m.Refresh.mu.Lock()
				m.Refresh.revokeSession(s.SID, now)
				m.Refresh.mu.Unlock()
			}
		}
	}
	return n, nil
}

func (m *MockAuthSessionStore) CountUserSessions(_ context.Context, userID string, now time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int
	for _, s := range m.Sessions {
		if s.UserID == userID && s.RevokedAt == nil && s.AbsoluteExpiresAt.After(now) {
			n++
		}
	}
	return n, nil
}

func (m *MockAuthSessionStore) ListUserSessions(_ context.Context, userID string, now time.Time) ([]coreidentity.AuthSession, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []coreidentity.AuthSession
	for _, s := range m.Sessions {
		if s.UserID == userID && s.RevokedAt == nil && s.AbsoluteExpiresAt.After(now) {
			out = append(out, s.toCore())
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (s *MockAuthSession) toCore() coreidentity.AuthSession {
	return coreidentity.AuthSession{
		SID:               s.SID,
		UserID:            s.UserID,
		Platform:          s.Platform,
		AuthMethod:        s.AuthMethod,
		CreatedAt:         s.CreatedAt,
		LastSeenAt:        s.LastSeenAt,
		AbsoluteExpiresAt: s.AbsoluteExpiresAt,
	}
}
