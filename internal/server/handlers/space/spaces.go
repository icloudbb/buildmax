package space

import (
	"errors"
	"net/http"
	"strings"
	"time"

	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/server/httputil"
	spacesvc "github.com/icloudbb/buildmax/internal/service/space"
	"github.com/icloudbb/buildmax/internal/util"
)

// maxSpaceNameRunes bounds a Space name to its varchar(255) column, so an
// over-long name is a 400 rather than a write error surfaced as a 500.
const maxSpaceNameRunes = 255

type spaceResponse struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	PersonalForUserID *string   `json:"personal_for_user_id,omitempty"`
	CreatedBy         string    `json:"created_by"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type spaceMemberResponse struct {
	SpaceID   string    `json:"space_id"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	UserName  *string   `json:"user_name,omitempty"`
	UserEmail *string   `json:"user_email,omitempty"`
}

type inviteMemberRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

type createSpaceRequest struct {
	Name string `json:"name"`
}

// invitationResponse never carries a code: §5.1 of
// docs/design/space-membership-lifecycle.md mints one only for an account
// created by the invitation itself, never for one that already existed, and
// space-scoped invitation only ever targets an account that already exists.
type invitationResponse struct {
	ID        string    `json:"id"`
	SpaceID   string    `json:"space_id"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"`
	InvitedBy string    `json:"invited_by"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

func invitationToResponse(inv corespace.Invitation) invitationResponse {
	return invitationResponse{
		ID:        inv.ID,
		SpaceID:   inv.SpaceID,
		UserID:    inv.UserID,
		Role:      inv.Role,
		InvitedBy: inv.InvitedBy,
		ExpiresAt: inv.ExpiresAt,
		CreatedAt: inv.CreatedAt,
	}
}

func invitationsToResponse(list []corespace.Invitation) []invitationResponse {
	out := make([]invitationResponse, len(list))
	for i := range list {
		out[i] = invitationToResponse(list[i])
	}
	return out
}

func spaceToResponse(space corespace.Space) spaceResponse {
	return spaceResponse{
		ID:                space.ID,
		Name:              space.Name,
		PersonalForUserID: space.PersonalForUserID,
		CreatedBy:         space.CreatedBy,
		CreatedAt:         space.CreatedAt,
		UpdatedAt:         space.UpdatedAt,
	}
}

func spaceMemberToResponse(member corespace.Member, user *coreidentity.User) spaceMemberResponse {
	resp := spaceMemberResponse{
		SpaceID:   member.SpaceID,
		UserID:    member.UserID,
		Role:      member.Role,
		CreatedAt: member.CreatedAt,
	}
	if user != nil {
		if user.Name != "" {
			resp.UserName = &user.Name
		}
		if user.Email != "" {
			resp.UserEmail = &user.Email
		}
	}
	return resp
}

func (h *Handler) listSpacesHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.guard().UserAndStore(w, r, h.cfg.Spaces, "spaces not configured")
	if !ok {
		return
	}
	list, err := h.cfg.Spaces.ListSpacesByUser(r.Context(), userID)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "list_spaces", "user_id", userID)
		return
	}
	out := make([]spaceResponse, len(list))
	for i := range list {
		out[i] = spaceToResponse(list[i])
	}
	httputil.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) createSpaceHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.guard().UserAndStore(w, r, h.cfg.Spaces, "spaces not configured")
	if !ok {
		return
	}
	var req createSpaceRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "name is required")
		return
	}
	if util.ExceedsRuneLimit(name, maxSpaceNameRunes) {
		httputil.WriteJSONError(w, http.StatusBadRequest, "name is too long")
		return
	}
	space, err := h.cfg.Spaces.CreateSpace(r.Context(), name, userID, h.cfg.DefaultQuotaTier)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "create_space", "user_id", userID)
		return
	}
	// The detail is the tier the space runs under. It is decided here and nowhere
	// else — there is no reassignment path — so this is the record of it.
	h.cfg.Audit.UserAction(r.Context(), userID, space.ID, coreaudit.SpaceCreated, "space", space.ID, space.QuotaTier)
	httputil.WriteJSON(w, http.StatusCreated, spaceToResponse(*space))
}

func (h *Handler) spaceService() *spacesvc.Service {
	return h.spaces
}

func newSpaceService(cfg Config) *spacesvc.Service {
	return &spacesvc.Service{Spaces: cfg.Spaces, Agents: cfg.Agents, Users: cfg.Users, LoginCodes: cfg.LoginCodes}
}

func (h *Handler) listSpaceMembersHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.guard().UserAndStore(w, r, h.cfg.Spaces, "spaces not configured")
	if !ok {
		return
	}
	spaceID, ok := httputil.PathValue(w, r, "space_id")
	if !ok {
		return
	}
	if _, resolvedSpaceID, ok := h.guard().ExplicitSpace(w, r, userID, spaceID); !ok || resolvedSpaceID == "" {
		return
	}
	members, err := h.spaceService().ListMembers(r.Context(), spaceID)
	if err != nil {
		if httputil.WriteServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "list_space_members", "user_id", userID, "space_id", spaceID)
		return
	}
	out := make([]spaceMemberResponse, len(members))
	for i := range members {
		out[i] = spaceMemberToResponse(members[i].Membership, members[i].User)
	}
	httputil.WriteJSON(w, http.StatusOK, out)
}

// inviteMemberHandler serves POST /api/spaces/{space_id}/invitations.
//
// It replaces the old instant-add route outright rather than living beside
// it: per docs/design/space-membership-lifecycle.md §5.1, a second route that
// could also add a member would be the same duplicated authority §1 of that
// document argues against, one layer down.
func (h *Handler) inviteMemberHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.guard().UserAndStore(w, r, h.cfg.Spaces, "spaces not configured")
	if !ok {
		return
	}
	spaceID, ok := httputil.PathValue(w, r, "space_id")
	if !ok {
		return
	}
	if _, resolvedSpaceID, ok := h.guard().ExplicitSpace(w, r, userID, spaceID); !ok || resolvedSpaceID == "" {
		return
	}
	var req inviteMemberRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	inv, user, err := h.spaceService().InviteMember(r.Context(), spacesvc.InviteMemberCmd{
		SpaceID: spaceID,
		ActorID: userID,
		Email:   req.Email,
		Role:    req.Role,
	})
	if err != nil {
		if httputil.WriteServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "invite_space_member", "user_id", userID, "space_id", spaceID)
		return
	}
	h.cfg.Audit.UserAction(r.Context(), userID, spaceID, coreaudit.SpaceMemberInvited, "user", user.ID, inv.Role)
	httputil.WriteJSON(w, http.StatusCreated, invitationToResponse(*inv))
}

// listSpaceInvitationsHandler serves GET /api/spaces/{space_id}/invitations.
// Reading who has been invited is the same authority as sending or revoking
// an invitation.
func (h *Handler) listSpaceInvitationsHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.guard().UserAndStore(w, r, h.cfg.Spaces, "spaces not configured")
	if !ok {
		return
	}
	spaceID, ok := httputil.PathValue(w, r, "space_id")
	if !ok {
		return
	}
	if _, resolvedSpaceID, ok := h.guard().ExplicitSpace(w, r, userID, spaceID); !ok || resolvedSpaceID == "" {
		return
	}
	invitations, err := h.spaceService().ListSpaceInvitations(r.Context(), spaceID, userID)
	if err != nil {
		if httputil.WriteServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "list_space_invitations", "user_id", userID, "space_id", spaceID)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, invitationsToResponse(invitations))
}

// revokeInvitationHandler serves DELETE
// /api/spaces/{space_id}/invitations/{invitation_id}.
func (h *Handler) revokeInvitationHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.guard().UserAndStore(w, r, h.cfg.Spaces, "spaces not configured")
	if !ok {
		return
	}
	spaceID, ok := httputil.PathValue(w, r, "space_id")
	if !ok {
		return
	}
	invitationID, ok := httputil.PathValue(w, r, "invitation_id")
	if !ok {
		return
	}
	if _, resolvedSpaceID, ok := h.guard().ExplicitSpace(w, r, userID, spaceID); !ok || resolvedSpaceID == "" {
		return
	}
	err := h.spaceService().RevokeInvitation(r.Context(), spacesvc.RevokeInvitationCmd{
		SpaceID:      spaceID,
		InvitationID: invitationID,
		ActorID:      userID,
	})
	if err != nil {
		if httputil.WriteServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "revoke_invitation", "user_id", userID, "space_id", spaceID)
		return
	}
	h.cfg.Audit.UserAction(r.Context(), userID, spaceID, coreaudit.InvitationRevoked, "space_invitation", invitationID, "")
	w.WriteHeader(http.StatusNoContent)
}

// listMyInvitationsHandler serves GET /api/invitations. It is not
// space-scoped -- it answers "what is pending for me", across every space --
// so unlike every other route in this file it takes no space_id and checks no
// space membership.
func (h *Handler) listMyInvitationsHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.guard().UserAndStore(w, r, h.cfg.Spaces, "spaces not configured")
	if !ok {
		return
	}
	invitations, err := h.spaceService().ListMyInvitations(r.Context(), userID)
	if err != nil {
		if httputil.WriteServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "list_my_invitations", "user_id", userID)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, invitationsToResponse(invitations))
}

// acceptInvitationHandler serves POST /api/invitations/{invitation_id}/accept.
//
// It takes no code: per docs/design/space-membership-lifecycle.md §5.1, the
// caller already reached this session on their own, so this is authorized by
// "this is my own pending row," not by proving anything a second time.
func (h *Handler) acceptInvitationHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.guard().UserAndStore(w, r, h.cfg.Spaces, "spaces not configured")
	if !ok {
		return
	}
	invitationID, ok := httputil.PathValue(w, r, "invitation_id")
	if !ok {
		return
	}
	accepted, err := h.spaceService().AcceptInvitation(r.Context(), spacesvc.AcceptInvitationCmd{
		InvitationID: invitationID,
		ActorID:      userID,
	})
	if err != nil {
		// Best-effort: an expired invitation still names a space worth
		// recording against, and a failed lookup here should not turn an
		// already-refused request into an internal error.
		if errors.Is(err, spacesvc.ErrInvitationExpired) {
			if inv, lookupErr := h.cfg.Spaces.GetInvitation(r.Context(), invitationID); lookupErr == nil && inv != nil {
				h.cfg.Audit.UserAction(r.Context(), userID, inv.SpaceID, coreaudit.InvitationExpired, "space_invitation", invitationID, "")
			}
		}
		if httputil.WriteServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "accept_invitation", "user_id", userID)
		return
	}
	h.cfg.Audit.UserAction(r.Context(), userID, accepted.SpaceID, coreaudit.InvitationAccepted, "space", accepted.SpaceID, accepted.Role)
	httputil.WriteJSON(w, http.StatusOK, invitationToResponse(*accepted))
}

func (h *Handler) removeSpaceMemberHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.guard().UserAndStore(w, r, h.cfg.Spaces, "spaces not configured")
	if !ok {
		return
	}
	spaceID, ok := httputil.PathValue(w, r, "space_id")
	if !ok {
		return
	}
	targetUserID, ok := httputil.PathValue(w, r, "user_id")
	if !ok {
		return
	}
	if _, resolvedSpaceID, ok := h.guard().ExplicitSpace(w, r, userID, spaceID); !ok || resolvedSpaceID == "" {
		return
	}
	err := h.spaceService().RemoveMember(r.Context(), spacesvc.RemoveMemberCmd{
		SpaceID:      spaceID,
		ActorID:      userID,
		TargetUserID: targetUserID,
	})
	if err != nil {
		if httputil.WriteServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "remove_space_member", "space_id", spaceID, "member_user_id", targetUserID)
		return
	}
	h.cfg.Audit.UserAction(r.Context(), userID, spaceID, coreaudit.SpaceMemberRemoved, "user", targetUserID, "")
	w.WriteHeader(http.StatusNoContent)
}

type setMemberRoleRequest struct {
	Role string `json:"role"`
}

// memberRoleResponse is deliberately smaller than spaceMemberResponse: a role
// change reports what changed, not the whole roster row, and building the
// latter would mean a second read this handler has no other reason to make.
type memberRoleResponse struct {
	SpaceID string `json:"space_id"`
	UserID  string `json:"user_id"`
	Role    string `json:"role"`
}

type memberLoginCodeResponse struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}

// setMemberRoleHandler serves PATCH /api/spaces/{space_id}/members/{user_id}.
//
// Setting role to owner is ownership transfer: the caller is demoted to
// admin in the same call, unilaterally and immediately -- see
// docs/design/space-membership-lifecycle.md §5.2-§5.3.
func (h *Handler) setMemberRoleHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.guard().UserAndStore(w, r, h.cfg.Spaces, "spaces not configured")
	if !ok {
		return
	}
	spaceID, ok := httputil.PathValue(w, r, "space_id")
	if !ok {
		return
	}
	targetUserID, ok := httputil.PathValue(w, r, "user_id")
	if !ok {
		return
	}
	if _, resolvedSpaceID, ok := h.guard().ExplicitSpace(w, r, userID, spaceID); !ok || resolvedSpaceID == "" {
		return
	}
	var req setMemberRoleRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	err := h.spaceService().SetMemberRole(r.Context(), spacesvc.SetMemberRoleCmd{
		SpaceID:      spaceID,
		ActorID:      userID,
		TargetUserID: targetUserID,
		Role:         req.Role,
	})
	if err != nil {
		if httputil.WriteServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "set_member_role", "user_id", userID, "space_id", spaceID)
		return
	}
	role := strings.TrimSpace(req.Role)
	// Transfer gets its own action distinct from a role change, even though
	// it is one call, so an investigation asking "did ownership ever move"
	// need not infer it from two member_role_changed rows.
	if role == corespace.RoleOwner {
		h.cfg.Audit.UserAction(r.Context(), userID, spaceID, coreaudit.OwnershipTransferred, "user", targetUserID, role)
	} else {
		h.cfg.Audit.UserAction(r.Context(), userID, spaceID, coreaudit.MemberRoleChanged, "user", targetUserID, role)
	}
	httputil.WriteJSON(w, http.StatusOK, memberRoleResponse{SpaceID: spaceID, UserID: targetUserID, Role: role})
}

// issueMemberLoginCodeHandler serves POST
// /api/spaces/{space_id}/members/{user_id}/login-code.
//
// This does not replace system-administration.md's system_admin route --
// that one still exists, still works deployment-wide, and is what recovers
// an owner who has no co-owner and no admin left in their own space. This
// route only removes the dependency on a system_admin existing at all for
// the common case of one member locked out of an otherwise healthy space. See
// docs/design/space-membership-lifecycle.md §5.4.
func (h *Handler) issueMemberLoginCodeHandler(w http.ResponseWriter, r *http.Request) {
	userID, ok := h.guard().UserAndStore(w, r, h.cfg.Spaces, "spaces not configured")
	if !ok {
		return
	}
	spaceID, ok := httputil.PathValue(w, r, "space_id")
	if !ok {
		return
	}
	targetUserID, ok := httputil.PathValue(w, r, "user_id")
	if !ok {
		return
	}
	if _, resolvedSpaceID, ok := h.guard().ExplicitSpace(w, r, userID, spaceID); !ok || resolvedSpaceID == "" {
		return
	}
	code, expiresAt, err := h.spaceService().IssueMemberLoginCode(r.Context(), spacesvc.IssueMemberLoginCodeCmd{
		SpaceID:      spaceID,
		ActorID:      userID,
		TargetUserID: targetUserID,
	})
	if err != nil {
		if httputil.WriteServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "issue_member_login_code", "user_id", userID, "space_id", spaceID)
		return
	}
	// The code itself is never recorded, here or in the trail -- it exists in
	// this response and nowhere else, matching the admin route's own rule.
	h.cfg.Audit.UserAction(r.Context(), userID, spaceID, coreaudit.SpaceMemberLoginCodeIssued, "user", targetUserID, "")
	httputil.WriteJSON(w, http.StatusOK, memberLoginCodeResponse{Code: code, ExpiresAt: expiresAt})
}
