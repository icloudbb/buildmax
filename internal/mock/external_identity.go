package mock

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
)

// MockExternalIdentityStore is an in-memory ExternalIdentityStore for tests. It
// reproduces the two uniqueness invariants the real store enforces — one subject
// per (issuer, subject), one identity per (issuer, user) — and the
// unlink-only-while-disabled precondition, because those are what the
// association service and the admin routes branch on.
//
// Users, when set, is the account backing store: CreateUserWithIdentity adds to
// it and UnlinkIdentity reads disabled state from it, so the double behaves like
// the real store's shared transaction.
type MockExternalIdentityStore struct {
	mu    sync.Mutex
	links []*coreidentity.ExternalIdentity
	Users *MockUserStore
	// LinkErr and CreateErr force a store failure for a specific branch.
	LinkErr   error
	CreateErr error
	issued    int
}

func (m *MockExternalIdentityStore) IdentityBySubject(_ context.Context, issuer, subject string) (*coreidentity.ExternalIdentity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l := m.findBySubject(issuer, subject); l != nil {
		cp := *l
		return &cp, nil
	}
	return nil, nil
}

func (m *MockExternalIdentityStore) IdentityByUserAndIssuer(_ context.Context, userID, issuer string) (*coreidentity.ExternalIdentity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, l := range m.links {
		if l.UserID == userID && l.Issuer == issuer {
			cp := *l
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *MockExternalIdentityStore) ListUserIdentities(_ context.Context, userID string) ([]coreidentity.ExternalIdentity, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []coreidentity.ExternalIdentity
	for _, l := range m.links {
		if l.UserID == userID {
			out = append(out, *l)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

func (m *MockExternalIdentityStore) LinkExisting(_ context.Context, in coreidentity.LinkIdentity) (*coreidentity.ExternalIdentity, error) {
	if m.LinkErr != nil {
		return nil, m.LinkErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.conflict(in.Issuer, in.Subject, in.UserID) {
		return nil, coreidentity.ErrIdentityConflict
	}
	link := m.appendLink(in.UserID, in.Issuer, in.Subject, in.Seen)
	cp := *link
	return &cp, nil
}

func (m *MockExternalIdentityStore) CreateUserWithIdentity(_ context.Context, in coreidentity.ProvisionUser) (*coreidentity.User, *coreidentity.ExternalIdentity, error) {
	if m.CreateErr != nil {
		return nil, nil, m.CreateErr
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Users == nil {
		return nil, nil, fmt.Errorf("mock external identity: Users backref not set")
	}
	if u, _ := m.Users.UserByEmail(context.Background(), in.Email); u != nil {
		return nil, nil, coreidentity.ErrEmailExists
	}
	if m.conflict(in.Issuer, in.Subject, "") {
		return nil, nil, coreidentity.ErrIdentityConflict
	}
	user, err := m.Users.CreateUser(context.Background(), in.Email, in.QuotaTier)
	if err != nil {
		return nil, nil, err
	}
	if in.Name != "" {
		user.Name = in.Name
	}
	link := m.appendLink(user.ID, in.Issuer, in.Subject, coreidentity.SeenClaims{Email: in.Email, Name: in.Name})
	userCopy, linkCopy := *user, *link
	return &userCopy, &linkCopy, nil
}

func (m *MockExternalIdentityStore) UnlinkIdentity(_ context.Context, in coreidentity.UnlinkIdentity) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Users != nil {
		u, _ := m.Users.GetUser(context.Background(), in.UserID)
		if u == nil {
			return coreidentity.ErrIdentityNotFound
		}
		if u.DisabledAt == nil {
			return coreidentity.ErrUnlinkRequiresDisabled
		}
	}
	for i, l := range m.links {
		if l.ID == in.IdentityID && l.UserID == in.UserID {
			m.links = append(m.links[:i], m.links[i+1:]...)
			return nil
		}
	}
	return coreidentity.ErrIdentityNotFound
}

func (m *MockExternalIdentityStore) UpdateLastSeen(_ context.Context, issuer, subject string, seen coreidentity.SeenClaims, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l := m.findBySubject(issuer, subject); l != nil {
		l.LastSeenEmail = seen.Email
		l.LastSeenName = seen.Name
		l.LastLoginAt = now
	}
	return nil
}

func (m *MockExternalIdentityStore) findBySubject(issuer, subject string) *coreidentity.ExternalIdentity {
	for _, l := range m.links {
		if l.Issuer == issuer && l.Subject == subject {
			return l
		}
	}
	return nil
}

// conflict reports whether inserting (issuer, subject) for userID would violate
// either uniqueness index. userID "" skips the (issuer, user) check, for the
// provisioning path where the user does not exist yet.
func (m *MockExternalIdentityStore) conflict(issuer, subject, userID string) bool {
	for _, l := range m.links {
		if l.Issuer == issuer && l.Subject == subject {
			return true
		}
		if userID != "" && l.Issuer == issuer && l.UserID == userID {
			return true
		}
	}
	return false
}

func (m *MockExternalIdentityStore) appendLink(userID, issuer, subject string, seen coreidentity.SeenClaims) *coreidentity.ExternalIdentity {
	m.issued++
	now := time.Now().UTC()
	link := &coreidentity.ExternalIdentity{
		ID:            fmt.Sprintf("mock-eid-%d", m.issued),
		UserID:        userID,
		Issuer:        issuer,
		Subject:       subject,
		LastSeenEmail: seen.Email,
		LastSeenName:  seen.Name,
		LastLoginAt:   now,
		CreatedAt:     now,
	}
	m.links = append(m.links, link)
	return link
}
