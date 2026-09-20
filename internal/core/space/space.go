package space

import (
	"context"
	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	"time"
)

const (
	// RoleOwner is the initial role for the user who creates a space.
	RoleOwner = "owner"
	// RoleAdmin can manage shared automation assets but not membership ownership.
	RoleAdmin = "admin"
	// RoleMember is the basic collaboration role for invited members.
	RoleMember = "member"
	// DefaultPersonalName is the initial UX-facing name for a user's own space.
	DefaultPersonalName = "My Space"
)

// Space is the ownership and collaboration boundary for working resources.
// A user's default personal space is represented by personal_for_user_id.
type Space struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	PersonalForUserID *string `json:"personal_for_user_id,omitempty"`
	QuotaTier         string  `json:"quota_tier,omitempty"`
	// PluginCuration is who fills this space's plugin activation list; empty
	// reads as plugin.CurationOpen. See core/plugin/activation.go.
	PluginCuration coreplugin.Curation `json:"plugin_curation,omitempty"`
	// AgentInstructions are shared guidance appended to every background agent
	// run in this space. Revision changes whenever the text changes so a TaskRun
	// can record exactly which space-level instructions it received.
	AgentInstructions         string `json:"agent_instructions,omitempty"`
	AgentInstructionsRevision int    `json:"agent_instructions_revision,omitempty"`
	// DefaultSandboxNetworkTier and DefaultSandboxFilesystemTier are the
	// config.SandboxNetworkTier / config.SandboxFilesystemTier values an agent
	// that declares neither tier inherits. Empty means the surface baseline
	// applies instead -- see docs/design/agent-sandbox-policy.md §9 M3.
	DefaultSandboxNetworkTier    string    `json:"default_sandbox_network_tier,omitempty"`
	DefaultSandboxFilesystemTier string    `json:"default_sandbox_filesystem_tier,omitempty"`
	CreatedBy                    string    `json:"created_by"`
	CreatedAt                    time.Time `json:"created_at"`
	UpdatedAt                    time.Time `json:"updated_at"`
}

// Member is one user's membership in a space.
type Member struct {
	SpaceID   string    `json:"space_id"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// InvitationTTLDefault is how long a pending invitation stays acceptable when
// nobody accepts it. Three days rather than a login code's shorter window: an
// invitation is meant to be acted on whenever the recipient next opens
// Portal, not inside the exchange that sent it. See
// docs/design/space-membership-lifecycle.md.
const InvitationTTLDefault = 72 * time.Hour

// Invitation is a pending offer of space membership against an account that
// already exists — space-scoped invitation never creates one. See
// docs/design/space-membership-lifecycle.md §1 for why account creation and
// space membership are kept as two different authorities.
type Invitation struct {
	ID      string `json:"id"`
	SpaceID string `json:"space_id"`
	UserID  string `json:"user_id"`
	Role    string `json:"role"`
	// InvitedBy is the user who sent the invitation.
	InvitedBy string    `json:"invited_by"`
	ExpiresAt time.Time `json:"expires_at"`
	// AcceptedAt and RevokedAt are mutually exclusive; both nil means still
	// pending. There is no separate status column -- these two timestamps plus
	// ExpiresAt are the whole state, the same shape user.disabled_at and
	// system_grant.revoked_at already use for "off until proven otherwise".
	AcceptedAt *time.Time `json:"accepted_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// Pending reports whether the invitation may still be accepted: neither
// answered nor withdrawn, and not past its offer window.
func (i Invitation) Pending(now time.Time) bool {
	return i.AcceptedAt == nil && i.RevokedAt == nil && now.Before(i.ExpiresAt)
}

// Store provides space persistence and membership lookup.
type Store interface {
	// GetSpace returns the space by space_id, or (nil, nil) when not found.
	GetSpace(ctx context.Context, spaceID string) (*Space, error)
	// GetPersonalSpaceByUser returns the default personal space for the user, or (nil, nil) when not found.
	GetPersonalSpaceByUser(ctx context.Context, userID string) (*Space, error)
	// ListSpacesByUser returns all spaces the user belongs to, ordered by created_at ASC.
	ListSpacesByUser(ctx context.Context, userID string) ([]Space, error)
	// CreateSpace creates a new space and owner membership.
	CreateSpace(ctx context.Context, name, createdBy, quotaTier string) (*Space, error)
	// AddSpaceMember adds or updates a space membership.
	AddSpaceMember(ctx context.Context, spaceID, userID, role string) (*Member, error)
	// RemoveSpaceMember removes one membership from a space.
	RemoveSpaceMember(ctx context.Context, spaceID, userID string) error
	// ListSpaceMembers returns members of the space ordered by created_at ASC.
	ListSpaceMembers(ctx context.Context, spaceID string) ([]Member, error)
	// ListTeamSpaces returns every collaborative space newest first, with the
	// total count. A non-empty query filters on name as a substring. Each
	// account's personal space is excluded: it exists for every account and is
	// not a thing an administrator governs, so it is noise on this surface.
	//
	// It is the one method here that ignores membership, so only
	// deployment-scoped callers may reach it. It returns spaces, never their
	// contents: an administrator learns that a space exists and how large it is,
	// not what is in it.
	ListTeamSpaces(ctx context.Context, query string, limit, offset int) ([]Space, int, error)
	// CountSpaceMembers returns member counts for the given spaces, keyed by
	// space id. It exists so listing spaces is two queries rather than one per
	// row.
	CountSpaceMembers(ctx context.Context, spaceIDs []string) (map[string]int, error)
	// SetSpacePluginCuration records who fills the space's plugin activation
	// list, or returns ErrNotFound. The value is validated above this layer.
	SetSpacePluginCuration(ctx context.Context, spaceID string, mode coreplugin.Curation) error
	// SetSpaceSandboxDefaults records the tiers an agent that declares neither
	// inherits, or returns ErrNotFound. The values are validated above this
	// layer, the same way SetSpacePluginCuration's mode is.
	SetSpaceSandboxDefaults(ctx context.Context, spaceID, networkTier, filesystemTier string) error
	// SetSpaceAgentInstructions replaces the shared instructions for future
	// background agent runs. An identical value is a no-op; a change advances
	// AgentInstructionsRevision atomically.
	SetSpaceAgentInstructions(ctx context.Context, spaceID, instructions string) error

	// CreateInvitation creates a pending invitation for userID to join spaceID
	// at role, sent by invitedBy, acceptable until expiresAt.
	CreateInvitation(ctx context.Context, spaceID, userID, role, invitedBy string, expiresAt time.Time) (*Invitation, error)
	// GetInvitation returns one invitation by its handle, or (nil, nil) when
	// not found.
	GetInvitation(ctx context.Context, invitationID string) (*Invitation, error)
	// ListPendingInvitationsBySpace returns a space's still-pending invitations,
	// newest first.
	ListPendingInvitationsBySpace(ctx context.Context, spaceID string, now time.Time) ([]Invitation, error)
	// ListPendingInvitationsByUser returns one account's still-pending
	// invitations across every space, newest first -- what GET /api/invitations
	// answers.
	ListPendingInvitationsByUser(ctx context.Context, userID string, now time.Time) ([]Invitation, error)
	// AcceptInvitation marks a pending invitation accepted and creates the
	// resulting space membership in the same transaction, or returns (nil, nil)
	// when the row does not exist or is no longer pending -- an invitation
	// marked accepted with no membership to show for it would be evidence of a
	// bug no caller could act on.
	AcceptInvitation(ctx context.Context, invitationID string, now time.Time) (*Invitation, error)
	// RevokeInvitation marks a pending invitation revoked. A row that does not
	// exist or is no longer pending is not an error -- withdrawing an offer
	// that already resolved itself asks for nothing this store has to refuse.
	RevokeInvitation(ctx context.Context, invitationID string, now time.Time) error

	// TransferOwnership makes toUserID the space's owner and demotes fromUserID
	// to admin, atomically -- a space must never be read with two owners or
	// none because a caller observed the change half-applied. Both must
	// already be members; the service enforces that, and the last-owner
	// invariant, before calling this. See
	// docs/design/space-membership-lifecycle.md §5.2-§5.3.
	TransferOwnership(ctx context.Context, spaceID, fromUserID, toUserID string) error
}
