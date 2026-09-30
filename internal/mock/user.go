package mock

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
)

// MockUserStore is an in-memory UserStore for tests.
// Use ByEmail and ByID to pre-seed users; CreateErr and NextUserID for behavior.
type MockUserStore struct {
	ByEmail   map[string]*coreidentity.User
	ByID      map[string]*coreidentity.User
	CreateErr error
	// DisableErr, when set, is returned by SetUserDisabled instead of applying
	// the change. It lets a handler test drive the last-holder refusal, which
	// the real store decides atomically against the grant table and this
	// in-memory double cannot reproduce on its own.
	DisableErr error
	NextUserID int
	// PersonalSpaceTier records, by user id, the quota tier CreateUser gave the
	// account's personal Space; the real store writes it on that Space's row.
	PersonalSpaceTier map[string]string
}

// UserByEmail matches without regard to case, the way the real store's
// utf8mb4_0900_ai_ci email column does. Login resolves the account by address
// and nothing compares the two strings afterwards, so a case-sensitive mock
// would let a regression in that lookup pass here.
func (m *MockUserStore) UserByEmail(_ context.Context, email string) (*coreidentity.User, error) {
	if m.ByEmail == nil {
		return nil, nil
	}
	if u, ok := m.ByEmail[email]; ok {
		return u, nil
	}
	for stored, u := range m.ByEmail {
		if strings.EqualFold(stored, email) {
			return u, nil
		}
	}
	return nil, nil
}

func (m *MockUserStore) GetUser(_ context.Context, userID string) (*coreidentity.User, error) {
	if m.ByID != nil {
		return m.ByID[userID], nil
	}
	return nil, nil
}

func (m *MockUserStore) CreateUser(_ context.Context, email string, defaultQuotaTier string) (*coreidentity.User, error) {
	if m.CreateErr != nil {
		return nil, m.CreateErr
	}
	if m.ByEmail == nil {
		m.ByEmail = make(map[string]*coreidentity.User)
	}
	if m.ByID == nil {
		m.ByID = make(map[string]*coreidentity.User)
	}
	if existing := m.ByEmail[email]; existing != nil {
		return nil, coreidentity.ErrEmailExists
	}
	m.NextUserID++
	u := &coreidentity.User{
		ID:        fmt.Sprintf("mock-u-%d", m.NextUserID),
		Email:     email,
		Name:      "",
		CreatedAt: time.Now().UTC(),
	}
	m.ByEmail[email] = u
	m.ByID[u.ID] = u
	if m.PersonalSpaceTier == nil {
		m.PersonalSpaceTier = make(map[string]string)
	}
	m.PersonalSpaceTier[u.ID] = defaultQuotaTier
	return u, nil
}

func (m *MockUserStore) UpdateLoginMeta(_ context.Context, userID string, loginAt time.Time, platform string) error {
	if u, ok := m.ByID[userID]; ok {
		u.LastLoginAt = &loginAt
		u.LastLoginPlatform = &platform
	}
	return nil
}

func (m *MockUserStore) ListUsers(_ context.Context, filter coreidentity.UserFilter, limit, offset int) ([]coreidentity.User, int, error) {
	var all []coreidentity.User
	for _, u := range m.ByID {
		if filter.Query != "" && !strings.Contains(u.Email, filter.Query) {
			continue
		}
		if filter.Disabled != nil && (u.DisabledAt != nil) != *filter.Disabled {
			continue
		}
		if filter.HasPassword != nil && u.HasPassword != *filter.HasPassword {
			continue
		}
		if filter.Platform != "" && (u.LastLoginPlatform == nil || *u.LastLoginPlatform != filter.Platform) {
			continue
		}
		// SystemRole is a grant-store concern this double does not model; a test
		// that filters by it uses the real store.
		all = append(all, *u)
	}
	// Map iteration is random, and a list endpoint's paging assertions are not
	// worth making flaky. Newest first matches the store, with the id as the
	// tiebreaker seeded ids do not otherwise have.
	sort.Slice(all, func(i, j int) bool {
		if all[i].CreatedAt != all[j].CreatedAt {
			return all[i].CreatedAt.After(all[j].CreatedAt)
		}
		return all[i].ID > all[j].ID
	})
	page, total := paginate(all, limit, offset)
	return page, total, nil
}

func (m *MockUserStore) SetUserDisabled(_ context.Context, userID string, disabledAt *time.Time) error {
	if m.DisableErr != nil {
		return m.DisableErr
	}
	u, ok := m.ByID[userID]
	if !ok || u == nil {
		return coreidentity.ErrUserNotFound
	}
	u.DisabledAt = disabledAt
	return nil
}

// DisableForTest disables a seeded account. Test setup, not a store method.
func (m *MockUserStore) DisableForTest(userID string, at time.Time) {
	if u, ok := m.ByID[userID]; ok {
		u.DisabledAt = &at
	}
}

// MockPasswordStore is an in-memory PasswordStore for tests. Seed Hashes with
// the encoded hash a test expects to verify against.
type MockPasswordStore struct {
	Hashes  map[string]string
	ReadErr error
	SetErr  error
}

func (m *MockPasswordStore) PasswordHash(_ context.Context, userID string) (string, error) {
	if m.ReadErr != nil {
		return "", m.ReadErr
	}
	return m.Hashes[userID], nil
}

func (m *MockPasswordStore) SetPassword(_ context.Context, userID, encodedHash string, _ time.Time) error {
	if m.SetErr != nil {
		return m.SetErr
	}
	if m.Hashes == nil {
		m.Hashes = make(map[string]string)
	}
	m.Hashes[userID] = encodedHash
	return nil
}
