package mock

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
)

// MockSpaceStore is an in-memory SpaceStore for tests.
type MockSpaceStore struct {
	Spaces      []corespace.Space
	Members     []corespace.Member
	Invitations []corespace.Invitation

	invitationSeq int
}

func (m *MockSpaceStore) GetSpace(_ context.Context, spaceID string) (*corespace.Space, error) {
	for i := range m.Spaces {
		if m.Spaces[i].ID == spaceID {
			return &m.Spaces[i], nil
		}
	}
	return nil, nil
}

func (m *MockSpaceStore) GetPersonalSpaceByUser(_ context.Context, userID string) (*corespace.Space, error) {
	for i := range m.Spaces {
		if m.Spaces[i].PersonalForUserID != nil && *m.Spaces[i].PersonalForUserID == userID {
			return &m.Spaces[i], nil
		}
	}
	return nil, nil
}

func (m *MockSpaceStore) ListSpacesByUser(_ context.Context, userID string) ([]corespace.Space, error) {
	var out []corespace.Space
	for _, member := range m.Members {
		if member.UserID != userID {
			continue
		}
		for _, space := range m.Spaces {
			if space.ID == member.SpaceID {
				out = append(out, space)
			}
		}
	}
	return out, nil
}

func (m *MockSpaceStore) CreateSpace(_ context.Context, name, createdBy, quotaTier string) (*corespace.Space, error) {
	id := fmt.Sprintf("tm_%d", len(m.Spaces)+1)
	space := corespace.Space{
		ID:        id,
		Name:      name,
		QuotaTier: quotaTier,
		CreatedBy: createdBy,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	m.Spaces = append(m.Spaces, space)
	m.Members = append(m.Members, corespace.Member{
		SpaceID:   id,
		UserID:    createdBy,
		Role:      corespace.RoleOwner,
		CreatedAt: time.Now().UTC(),
	})
	return &m.Spaces[len(m.Spaces)-1], nil
}

func (m *MockSpaceStore) AddSpaceMember(_ context.Context, spaceID, userID, role string) (*corespace.Member, error) {
	for i := range m.Members {
		if m.Members[i].SpaceID == spaceID && m.Members[i].UserID == userID {
			m.Members[i].Role = role
			return &m.Members[i], nil
		}
	}
	member := corespace.Member{
		SpaceID:   spaceID,
		UserID:    userID,
		Role:      role,
		CreatedAt: time.Now().UTC(),
	}
	m.Members = append(m.Members, member)
	return &m.Members[len(m.Members)-1], nil
}

func (m *MockSpaceStore) TransferOwnership(_ context.Context, spaceID, fromUserID, toUserID string) error {
	for i := range m.Members {
		if m.Members[i].SpaceID != spaceID {
			continue
		}
		if m.Members[i].UserID == toUserID {
			m.Members[i].Role = corespace.RoleOwner
		}
		if m.Members[i].UserID == fromUserID {
			m.Members[i].Role = corespace.RoleAdmin
		}
	}
	return nil
}

func (m *MockSpaceStore) RemoveSpaceMember(_ context.Context, spaceID, userID string) error {
	out := m.Members[:0]
	for _, member := range m.Members {
		if member.SpaceID == spaceID && member.UserID == userID {
			continue
		}
		out = append(out, member)
	}
	m.Members = out
	return nil
}

func (m *MockSpaceStore) ListSpaceMembers(_ context.Context, spaceID string) ([]corespace.Member, error) {
	var out []corespace.Member
	for _, member := range m.Members {
		if member.SpaceID == spaceID {
			out = append(out, member)
		}
	}
	return out, nil
}

func (m *MockSpaceStore) ListTeamSpaces(_ context.Context, query string, limit, offset int) ([]corespace.Space, int, error) {
	var all []corespace.Space
	for i := range m.Spaces {
		if m.Spaces[i].PersonalForUserID != nil {
			continue
		}
		if query == "" || strings.Contains(m.Spaces[i].Name, query) {
			all = append(all, m.Spaces[i])
		}
	}
	page, total := paginate(all, limit, offset)
	return page, total, nil
}

func (m *MockSpaceStore) CountSpaceMembers(_ context.Context, spaceIDs []string) (map[string]int, error) {
	wanted := make(map[string]bool, len(spaceIDs))
	for _, id := range spaceIDs {
		wanted[id] = true
	}
	out := make(map[string]int, len(spaceIDs))
	for _, member := range m.Members {
		if wanted[member.SpaceID] {
			out[member.SpaceID]++
		}
	}
	return out, nil
}

func (m *MockSpaceStore) SetSpacePluginCuration(_ context.Context, spaceID string, mode coreplugin.Curation) error {
	for i := range m.Spaces {
		if m.Spaces[i].ID == spaceID {
			m.Spaces[i].PluginCuration = mode
			return nil
		}
	}
	return apierr.ErrNotFound
}

func (m *MockSpaceStore) SetSpaceSandboxDefaults(_ context.Context, spaceID, networkTier, filesystemTier string) error {
	for i := range m.Spaces {
		if m.Spaces[i].ID == spaceID {
			m.Spaces[i].DefaultSandboxNetworkTier = networkTier
			m.Spaces[i].DefaultSandboxFilesystemTier = filesystemTier
			return nil
		}
	}
	return apierr.ErrNotFound
}

func (m *MockSpaceStore) SetSpaceAgentInstructions(_ context.Context, spaceID, instructions string) error {
	for i := range m.Spaces {
		if m.Spaces[i].ID == spaceID {
			if m.Spaces[i].AgentInstructions != instructions {
				m.Spaces[i].AgentInstructions = instructions
				m.Spaces[i].AgentInstructionsRevision++
			}
			return nil
		}
	}
	return apierr.ErrNotFound
}

func (m *MockSpaceStore) CreateInvitation(_ context.Context, spaceID, userID, role, invitedBy string, expiresAt time.Time) (*corespace.Invitation, error) {
	m.invitationSeq++
	inv := corespace.Invitation{
		ID:        fmt.Sprintf("inv_%d", m.invitationSeq),
		SpaceID:   spaceID,
		UserID:    userID,
		Role:      role,
		InvitedBy: invitedBy,
		ExpiresAt: expiresAt,
		CreatedAt: time.Now().UTC(),
	}
	m.Invitations = append(m.Invitations, inv)
	return &m.Invitations[len(m.Invitations)-1], nil
}

func (m *MockSpaceStore) GetInvitation(_ context.Context, invitationID string) (*corespace.Invitation, error) {
	for i := range m.Invitations {
		if m.Invitations[i].ID == invitationID {
			return &m.Invitations[i], nil
		}
	}
	return nil, nil
}

func (m *MockSpaceStore) ListPendingInvitationsBySpace(_ context.Context, spaceID string, now time.Time) ([]corespace.Invitation, error) {
	var out []corespace.Invitation
	for _, inv := range m.Invitations {
		if inv.SpaceID == spaceID && inv.Pending(now) {
			out = append(out, inv)
		}
	}
	return out, nil
}

func (m *MockSpaceStore) ListPendingInvitationsByUser(_ context.Context, userID string, now time.Time) ([]corespace.Invitation, error) {
	var out []corespace.Invitation
	for _, inv := range m.Invitations {
		if inv.UserID == userID && inv.Pending(now) {
			out = append(out, inv)
		}
	}
	return out, nil
}

func (m *MockSpaceStore) AcceptInvitation(ctx context.Context, invitationID string, now time.Time) (*corespace.Invitation, error) {
	for i := range m.Invitations {
		if m.Invitations[i].ID != invitationID {
			continue
		}
		if !m.Invitations[i].Pending(now) {
			return nil, nil
		}
		m.Invitations[i].AcceptedAt = &now
		if _, err := m.AddSpaceMember(ctx, m.Invitations[i].SpaceID, m.Invitations[i].UserID, m.Invitations[i].Role); err != nil {
			return nil, err
		}
		return &m.Invitations[i], nil
	}
	return nil, nil
}

func (m *MockSpaceStore) RevokeInvitation(_ context.Context, invitationID string, now time.Time) error {
	for i := range m.Invitations {
		if m.Invitations[i].ID == invitationID && m.Invitations[i].Pending(now) {
			m.Invitations[i].RevokedAt = &now
			return nil
		}
	}
	return nil
}
