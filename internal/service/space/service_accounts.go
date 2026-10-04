package space

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/service/accountlifecycle"
	"github.com/icloudbb/buildmax/internal/util"
)

// maxServiceAccountNameRunes bounds the name to its varchar(255) column.
const maxServiceAccountNameRunes = 255

// Refusals for Space-owned service accounts. See
// docs/design/space-assistants.md §6.
var (
	ErrServiceAccountsNotConfigured             = apierr.New(apierr.KindNotConfigured, "service accounts not configured")
	ErrOnlyOwnerOrAdminCanManageServiceAccounts = apierr.New(apierr.KindForbidden,
		"only a space owner or admin may manage service accounts")
	ErrServiceAccountPersonalSpace = apierr.New(apierr.KindInvalid, "a personal space cannot have service accounts")
	ErrServiceAccountNameRequired  = apierr.New(apierr.KindInvalid, "name is required")
	ErrServiceAccountNameTooLong   = apierr.New(apierr.KindInvalid, "name is too long")
	// ErrInvalidSponsor covers a sponsor who is not an enabled person holding
	// owner or admin in this Space: someone accountable must be able to act on
	// the account.
	ErrInvalidSponsor = apierr.New(apierr.KindInvalid,
		"the sponsor must be an enabled owner or admin of this space")
	// ErrServiceAccountNotFound also covers a service account of another Space,
	// so an id is not an existence oracle across Spaces.
	ErrServiceAccountNotFound = apierr.New(apierr.KindNotFound, "service account not found")
	// ErrServiceAccountMembershipFixed refuses a role change or removal: a
	// service account is a member of exactly one Space, and disabling is how it
	// stops.
	ErrServiceAccountMembershipFixed = apierr.New(apierr.KindConflict,
		"a service account stays a member of its own space; disable it instead")
	// ErrServiceAccountNoSignIn refuses issuing a credential to one.
	ErrServiceAccountNoSignIn = apierr.New(apierr.KindInvalid, "a service account cannot sign in")
	// ErrServiceAccountCannotJoin refuses inviting one into a Space.
	ErrServiceAccountCannotJoin = apierr.New(apierr.KindInvalid, "a service account cannot join another space")
)

// ServiceAccount is a service account as its Space sees it.
type ServiceAccount struct {
	User    coreidentity.User
	SpaceID string
	// NeedsSponsor is derived on read: the sponsor is gone, disabled, or no
	// longer an owner or admin of the Space. Assistants consult it; nothing
	// else stops.
	NeedsSponsor bool
}

type CreateServiceAccountCmd struct {
	SpaceID string
	ActorID string
	Name    string
	// SponsorUserID defaults to the caller.
	SponsorUserID string
}

type UpdateServiceAccountCmd struct {
	SpaceID       string
	ActorID       string
	UserID        string
	Name          *string
	SponsorUserID *string
}

type SetServiceAccountStateCmd struct {
	SpaceID  string
	ActorID  string
	UserID   string
	Disabled bool
}

// ListServiceAccounts returns the Space's service accounts. Reading is any
// member's; the caller's membership is checked by the transport.
func (s *Service) ListServiceAccounts(ctx context.Context, spaceID string) ([]ServiceAccount, error) {
	if s.Spaces == nil || s.Users == nil || s.ServiceAccounts == nil {
		return nil, ErrServiceAccountsNotConfigured
	}
	members, err := s.Spaces.ListSpaceMembers(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	accounts, err := s.ServiceAccounts.ListServiceAccountsBySpace(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	out := make([]ServiceAccount, 0, len(accounts))
	sponsors := map[string]*coreidentity.User{}
	for i := range accounts {
		needs, err := s.needsSponsor(ctx, members, accounts[i].SponsorUserID, sponsors)
		if err != nil {
			return nil, err
		}
		out = append(out, ServiceAccount{User: accounts[i], SpaceID: spaceID, NeedsSponsor: needs})
	}
	return out, nil
}

// CreateServiceAccount creates a service account as a member of a team Space.
func (s *Service) CreateServiceAccount(ctx context.Context, cmd CreateServiceAccountCmd) (*ServiceAccount, error) {
	members, err := s.authorizeServiceAccounts(ctx, cmd.SpaceID, cmd.ActorID)
	if err != nil {
		return nil, err
	}
	space, err := s.Spaces.GetSpace(ctx, cmd.SpaceID)
	if err != nil {
		return nil, err
	}
	if space == nil {
		return nil, apierr.ErrNotFound
	}
	if space.PersonalForUserID != nil {
		return nil, ErrServiceAccountPersonalSpace
	}
	name, err := serviceAccountName(cmd.Name)
	if err != nil {
		return nil, err
	}
	sponsorID := strings.TrimSpace(cmd.SponsorUserID)
	if sponsorID == "" {
		sponsorID = cmd.ActorID
	}
	if err := s.checkSponsor(ctx, members, sponsorID); err != nil {
		return nil, err
	}
	user, err := s.ServiceAccounts.CreateServiceAccount(ctx, coreidentity.NewServiceAccount{
		SpaceID: cmd.SpaceID, Name: name, SponsorUserID: sponsorID,
	})
	if err != nil {
		return nil, fmt.Errorf("create service account: %w", err)
	}
	return &ServiceAccount{User: *user, SpaceID: cmd.SpaceID}, nil
}

// UpdateServiceAccount renames a service account, re-sponsors it, or both.
// Re-sponsoring to a valid owner or admin is what clears NeedsSponsor.
func (s *Service) UpdateServiceAccount(ctx context.Context, cmd UpdateServiceAccountCmd) (*ServiceAccount, error) {
	members, err := s.authorizeServiceAccounts(ctx, cmd.SpaceID, cmd.ActorID)
	if err != nil {
		return nil, err
	}
	if _, err := s.serviceAccountIn(ctx, members, cmd.UserID); err != nil {
		return nil, err
	}
	update := coreidentity.ServiceAccountUpdate{UserID: cmd.UserID}
	if cmd.Name != nil {
		name, err := serviceAccountName(*cmd.Name)
		if err != nil {
			return nil, err
		}
		update.Name = &name
	}
	if cmd.SponsorUserID != nil {
		sponsorID := strings.TrimSpace(*cmd.SponsorUserID)
		if err := s.checkSponsor(ctx, members, sponsorID); err != nil {
			return nil, err
		}
		update.SponsorUserID = &sponsorID
	}
	if err := s.ServiceAccounts.UpdateServiceAccount(ctx, update); err != nil {
		if errors.Is(err, coreidentity.ErrUserNotFound) {
			return nil, ErrServiceAccountNotFound
		}
		return nil, fmt.Errorf("update service account: %w", err)
	}
	return s.reloadServiceAccount(ctx, cmd.SpaceID, members, cmd.UserID)
}

// SetServiceAccountState disables or re-enables a service account through the
// account gate, so a disable also stops what is already running as it.
func (s *Service) SetServiceAccountState(ctx context.Context, cmd SetServiceAccountStateCmd) (*ServiceAccount, error) {
	members, err := s.authorizeServiceAccounts(ctx, cmd.SpaceID, cmd.ActorID)
	if err != nil {
		return nil, err
	}
	if s.Lifecycle == nil {
		return nil, ErrServiceAccountsNotConfigured
	}
	if _, err := s.serviceAccountIn(ctx, members, cmd.UserID); err != nil {
		return nil, err
	}
	if cmd.Disabled {
		_, err = s.Lifecycle.Disable(ctx, cmd.UserID, accountlifecycle.DisableOptions{})
		// The gate committed; a cleanup step that failed is converged on by the
		// eligibility reconciler, so it is not a failed request.
		if errors.Is(err, accountlifecycle.ErrCleanupIncomplete) {
			slog.Warn("service account disabled with incomplete cleanup", "err", err, "user_id", cmd.UserID)
			err = nil
		}
	} else {
		err = s.Lifecycle.Enable(ctx, cmd.UserID)
	}
	if err != nil {
		return nil, fmt.Errorf("set service account state: %w", err)
	}
	return s.reloadServiceAccount(ctx, cmd.SpaceID, members, cmd.UserID)
}

func (s *Service) authorizeServiceAccounts(ctx context.Context, spaceID, actorID string) ([]corespace.Member, error) {
	if s.Spaces == nil || s.Users == nil || s.ServiceAccounts == nil {
		return nil, ErrServiceAccountsNotConfigured
	}
	members, err := s.Spaces.ListSpaceMembers(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	if !allows(members, actorID, corespace.ActionManageServiceAccounts) {
		return nil, ErrOnlyOwnerOrAdminCanManageServiceAccounts
	}
	return members, nil
}

// serviceAccountIn resolves userID as a service account that is a member of
// the roster's Space.
func (s *Service) serviceAccountIn(ctx context.Context, members []corespace.Member, userID string) (*coreidentity.User, error) {
	if !isMember(members, userID) {
		return nil, ErrServiceAccountNotFound
	}
	user, err := s.Users.GetUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	if user == nil || !user.IsService() {
		return nil, ErrServiceAccountNotFound
	}
	return user, nil
}

func (s *Service) reloadServiceAccount(ctx context.Context, spaceID string, members []corespace.Member, userID string) (*ServiceAccount, error) {
	user, err := s.serviceAccountIn(ctx, members, userID)
	if err != nil {
		return nil, err
	}
	// Re-read the roster: a re-sponsor is judged against who holds owner or
	// admin now, not before the call.
	current, err := s.Spaces.ListSpaceMembers(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	needs, err := s.needsSponsor(ctx, current, user.SponsorUserID, map[string]*coreidentity.User{})
	if err != nil {
		return nil, err
	}
	return &ServiceAccount{User: *user, SpaceID: spaceID, NeedsSponsor: needs}, nil
}

// ValidSponsor reports whether sponsorID may be accountable for something the
// Space publishes: an enabled person who is an owner or admin of it. An
// Assistant's own sponsor is held to the same rule as a service account's.
func (s *Service) ValidSponsor(ctx context.Context, spaceID, sponsorID string) (bool, error) {
	if s.Spaces == nil || s.Users == nil {
		return false, ErrServiceAccountsNotConfigured
	}
	members, err := s.Spaces.ListSpaceMembers(ctx, spaceID)
	if err != nil {
		return false, err
	}
	needs, err := s.needsSponsor(ctx, members, &sponsorID, map[string]*coreidentity.User{})
	return !needs, err
}

// checkSponsor accepts an enabled person who is an owner or admin of the
// Space.
func (s *Service) checkSponsor(ctx context.Context, members []corespace.Member, sponsorID string) error {
	needs, err := s.needsSponsor(ctx, members, &sponsorID, map[string]*coreidentity.User{})
	if err != nil {
		return err
	}
	if needs {
		return ErrInvalidSponsor
	}
	return nil
}

// needsSponsor is the one sponsor-validity rule: creation and re-sponsoring
// refuse what it reports, and listing reports it as the sponsor-needed state.
// cache spares a listing one account read per service account.
func (s *Service) needsSponsor(ctx context.Context, members []corespace.Member, sponsorID *string, cache map[string]*coreidentity.User) (bool, error) {
	if sponsorID == nil || *sponsorID == "" {
		return true, nil
	}
	switch corespace.EffectiveRoleOf(members, *sponsorID) {
	case corespace.RoleOwner, corespace.RoleAdmin:
	default:
		return true, nil
	}
	sponsor, ok := cache[*sponsorID]
	if !ok {
		var err error
		sponsor, err = s.Users.GetUser(ctx, *sponsorID)
		if err != nil {
			return false, err
		}
		cache[*sponsorID] = sponsor
	}
	return sponsor == nil || sponsor.Disabled() || sponsor.IsService(), nil
}

func serviceAccountName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", ErrServiceAccountNameRequired
	}
	if util.ExceedsRuneLimit(name, maxServiceAccountNameRunes) {
		return "", ErrServiceAccountNameTooLong
	}
	return name, nil
}

// refuseServiceAccountMember answers whether a membership change targets a
// service account, which keeps its one member-role membership for life.
func (s *Service) refuseServiceAccountMember(ctx context.Context, userID string) error {
	if s.Users == nil {
		return nil
	}
	user, err := s.Users.GetUser(ctx, userID)
	if err != nil {
		return err
	}
	if user != nil && user.IsService() {
		return ErrServiceAccountMembershipFixed
	}
	return nil
}
