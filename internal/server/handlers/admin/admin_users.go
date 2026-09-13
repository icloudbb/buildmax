package admin

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	"github.com/icloudbb/buildmax/internal/server/httputil"
)

// AdminUser is one account as an administrator sees it.
//
// It is a response struct rather than coreidentity.User on purpose. A row struct
// serialized straight out is how a password hash reaches a client, and this is
// the surface where that would matter most — see the secret assertion in
// system_authz_matrix_test.go.
type AdminUser struct {
	ID                string     `json:"id"`
	Email             string     `json:"email"`
	Name              string     `json:"name,omitempty"`
	QuotaTier         string     `json:"quota_tier,omitempty"`
	HasPassword       bool       `json:"has_password"`
	DisabledAt        *time.Time `json:"disabled_at,omitempty"`
	LastLoginAt       *time.Time `json:"last_login_at,omitempty"`
	LastLoginPlatform *string    `json:"last_login_platform,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
}

func toAdminUser(u coreidentity.User) AdminUser {
	return AdminUser{
		ID:                u.ID,
		Email:             u.Email,
		Name:              u.Name,
		QuotaTier:         u.QuotaTier,
		HasPassword:       u.HasPassword,
		DisabledAt:        u.DisabledAt,
		LastLoginAt:       u.LastLoginAt,
		LastLoginPlatform: u.LastLoginPlatform,
		CreatedAt:         u.CreatedAt,
	}
}

// AdminUsersResponse is a page of accounts.
type AdminUsersResponse struct {
	Users []AdminUser `json:"users"`
	Total int         `json:"total"`
}

// AdminUserDetail adds what an operator needs when acting on one account: which
// spaces it can reach, and how many live sessions it has.
type AdminUserDetail struct {
	AdminUser
	Spaces []AdminUserSpace `json:"spaces"`
	// SessionCount counts live login chains, not tokens. It is what "signed in
	// on two machines" means.
	SessionCount int `json:"session_count"`
	// SystemRoles are the deployment-scoped roles this account holds.
	SystemRoles []string `json:"system_roles"`
}

// AdminUserSpace names a space the account belongs to and its role there. It
// carries no space content — see docs/design/system-administration.md section 7.
type AdminUserSpace struct {
	SpaceID string `json:"space_id"`
	Name    string `json:"name"`
	Role    string `json:"role"`
}

// AdminCreateUserRequest is the body for POST /api/admin/users.
type AdminCreateUserRequest struct {
	Email string `json:"email"`
}

// AdminLoginCodeResponse carries a login code, which is shown once.
type AdminLoginCodeResponse struct {
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expires_at"`
}

// AdminSessionsRevokedResponse reports how many tokens a revocation retired.
type AdminSessionsRevokedResponse struct {
	Revoked int64 `json:"revoked"`
}

// AdminSession is one live login chain as an administrator sees it. It is a
// response struct, not the store's row: it carries safe metadata to recognise a
// device by, and never a token or its hash.
type AdminSession struct {
	SessionID  string    `json:"session_id"`
	Platform   string    `json:"platform,omitempty"`
	AuthMethod string    `json:"auth_method,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	LastSeenAt time.Time `json:"last_seen_at"`
	ExpiresAt  time.Time `json:"expires_at"`
}

// AdminSessionsResponse is the list of an account's live sessions.
type AdminSessionsResponse struct {
	Sessions []AdminSession `json:"sessions"`
}

// triState reads a query param that means true, false, or "either". It returns
// a pointer so the absent value (match either) stays distinct from an explicit
// false.
func triState(value, trueVal, falseVal string) *bool {
	switch value {
	case trueVal:
		t := true
		return &t
	case falseVal:
		f := false
		return &f
	default:
		return nil
	}
}

// parseTimeParam reads an optional RFC 3339 timestamp query param. An empty
// value is allowed and yields (nil, true); a malformed value writes a 400 and
// yields (nil, false) so the caller returns without querying.
func parseTimeParam(w http.ResponseWriter, value, name string) (*time.Time, bool) {
	if value == "" {
		return nil, true
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, name+" must be an RFC 3339 timestamp")
		return nil, false
	}
	return &t, true
}

// listAdminUsersHandler serves GET /api/admin/users.
func (h *Handler) listAdminUsersHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.guard().SystemAdmin(w, r); !ok {
		return
	}
	if !httputil.RequireStore(w, h.cfg.Users, "accounts not configured") {
		return
	}
	query := r.URL.Query()
	limit, offset := httputil.LimitOffset(query, "limit", "offset", httputil.BulkPageDefault, httputil.BulkPageMax)
	after, ok := parseTimeParam(w, query.Get("last_login_after"), "last_login_after")
	if !ok {
		return
	}
	before, ok := parseTimeParam(w, query.Get("last_login_before"), "last_login_before")
	if !ok {
		return
	}
	filter := coreidentity.UserFilter{
		Query:           strings.TrimSpace(query.Get("q")),
		SystemRole:      query.Get("system_role"),
		Platform:        query.Get("platform"),
		Disabled:        triState(query.Get("status"), "disabled", "enabled"),
		HasPassword:     triState(query.Get("has_password"), "true", "false"),
		LastLoginAfter:  after,
		LastLoginBefore: before,
	}
	users, total, err := h.cfg.Users.ListUsers(r.Context(), filter, limit, offset)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_list_users")
		return
	}
	out := make([]AdminUser, 0, len(users))
	for _, u := range users {
		out = append(out, toAdminUser(u))
	}
	httputil.WriteJSON(w, http.StatusOK, AdminUsersResponse{Users: out, Total: total})
}

// getAdminUserHandler serves GET /api/admin/users/{user_id}.
func (h *Handler) getAdminUserHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.guard().SystemAdmin(w, r); !ok {
		return
	}
	user, ok := h.adminTargetUser(w, r)
	if !ok {
		return
	}
	detail := AdminUserDetail{AdminUser: toAdminUser(*user), Spaces: []AdminUserSpace{}, SystemRoles: []string{}}

	// Space names and roles, not space contents. An administrator learns that the
	// account can reach a space, never what is in it.
	if h.cfg.Spaces != nil {
		spaces, err := h.cfg.Spaces.ListSpacesByUser(r.Context(), user.ID)
		if err != nil {
			httputil.WriteInternalError(w, err, "handler error", "handler", "admin_get_user", "user_id", user.ID)
			return
		}
		for _, space := range spaces {
			role := ""
			members, err := h.cfg.Spaces.ListSpaceMembers(r.Context(), space.ID)
			if err != nil {
				httputil.WriteInternalError(w, err, "handler error", "handler", "admin_get_user", "space_id", space.ID)
				return
			}
			for _, m := range members {
				if m.UserID == user.ID {
					role = m.Role
					break
				}
			}
			detail.Spaces = append(detail.Spaces, AdminUserSpace{SpaceID: space.ID, Name: space.Name, Role: role})
		}
	}
	if h.cfg.Sessions != nil {
		count, err := h.cfg.Sessions.CountUserSessions(r.Context(), user.ID, time.Now().UTC())
		if err != nil {
			httputil.WriteInternalError(w, err, "handler error", "handler", "admin_get_user", "sessions")
			return
		}
		detail.SessionCount = count
	}
	roles, err := h.cfg.Grants.ActiveSystemRoles(r.Context(), user.ID)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_get_user", "roles")
		return
	}
	detail.SystemRoles = append(detail.SystemRoles, roles...)

	httputil.WriteJSON(w, http.StatusOK, detail)
}

// createAdminUserHandler serves POST /api/admin/users.
//
// It returns the account and no credential. Issuing a way in is a second,
// separately audited call — the same split `buildmax-server user create` makes,
// for the same reason: creating an account and minting access to it are
// different decisions.
func (h *Handler) createAdminUserHandler(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.guard().SystemAdmin(w, r)
	if !ok {
		return
	}
	if !httputil.RequireStore(w, h.cfg.Users, "accounts not configured") {
		return
	}
	var req AdminCreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	email := strings.TrimSpace(req.Email)
	if email == "" || !strings.Contains(email, "@") {
		httputil.WriteJSONError(w, http.StatusBadRequest, "a valid email is required")
		return
	}
	user, err := h.cfg.Users.CreateUser(r.Context(), email, h.cfg.DefaultQuotaTier)
	if err != nil {
		if errors.Is(err, coreidentity.ErrEmailExists) {
			httputil.WriteJSONError(w, http.StatusConflict, "email already registered")
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_create_user")
		return
	}
	h.recordAdminUserAction(r, actorID, coreaudit.UserCreated, user.ID, "")
	httputil.WriteJSON(w, http.StatusCreated, toAdminUser(*user))
}

// issueAdminLoginCodeHandler serves POST /api/admin/users/{user_id}/login-code.
func (h *Handler) issueAdminLoginCodeHandler(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.guard().SystemAdmin(w, r)
	if !ok {
		return
	}
	if !httputil.RequireStore(w, h.cfg.LoginCodes, "login codes not configured") {
		return
	}
	user, ok := h.adminTargetUser(w, r)
	if !ok {
		return
	}
	// A code for an account that cannot use it would be a way in that opens
	// nothing, and an operator would reasonably read the success as "they can
	// sign in now".
	if user.Disabled() {
		httputil.WriteJSONError(w, http.StatusConflict, "the account is disabled; enable it before issuing a code")
		return
	}
	code, expiresAt, err := h.cfg.LoginCodes.CreateLoginCode(r.Context(), user.ID, coreidentity.LoginCodeTTLDefault)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_login_code", "user_id", user.ID)
		return
	}
	h.recordAdminUserAction(r, actorID, coreaudit.LoginCodeIssued, user.ID, "")
	// The code itself is never recorded, here or in the trail. The event says
	// one was issued; the plaintext exists in this response and nowhere else.
	httputil.WriteJSON(w, http.StatusOK, AdminLoginCodeResponse{Code: code, ExpiresAt: expiresAt})
}

// setAdminUserDisabledHandler serves the disable and enable routes.
// setUserStateRequest is the body of PUT /api/admin/users/{user_id}/state.
type setUserStateRequest struct {
	Disabled bool `json:"disabled"`
}

// setAdminUserStateHandler sets the account's stored `disabled` flag. See
// the route conventions in docs/contribute/architecture/server.md
func (h *Handler) setAdminUserStateHandler(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.guard().SystemAdmin(w, r)
	if !ok {
		return
	}
	user, ok := h.adminTargetUser(w, r)
	if !ok {
		return
	}
	var req setUserStateRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	disable := req.Disabled
	// An administrator who disables their own account locks themselves out
	// mid-request and cannot undo it, because the next call is refused
	// too. The recovery would be the operator command, for a mistake that
	// is easy to make and pointless to allow.
	if disable && user.ID == actorID {
		httputil.WriteJSONError(w, http.StatusConflict, "an administrator cannot disable their own account")
		return
	}

	var disabledAt *time.Time
	action := coreaudit.UserEnabled
	if disable {
		now := time.Now().UTC()
		disabledAt = &now
		action = coreaudit.UserDisabled
	}
	if err := h.cfg.Users.SetUserDisabled(r.Context(), user.ID, disabledAt); err != nil {
		if errors.Is(err, coreidentity.ErrUserNotFound) {
			httputil.WriteJSONError(w, http.StatusNotFound, "account not found")
			return
		}
		if errors.Is(err, coreidentity.ErrSystemGrantLastHolder) {
			httputil.WriteJSONError(w, http.StatusConflict,
				"this account is the deployment's last system administrator; grant another before disabling it")
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_set_user_disabled", "user_id", user.ID)
		return
	}

	revoked := int64(0)
	if disable && h.cfg.Sessions != nil {
		// Every session, and its refresh tokens, retired now rather than left to
		// expire. The access token cannot be revoked directly; what stops it is
		// the guard's session check refusing on the next request.
		n, err := h.cfg.Sessions.RevokeUserSessions(r.Context(), user.ID, time.Now().UTC())
		if err != nil {
			httputil.WriteInternalError(w, err, "handler error", "handler", "admin_set_user_disabled", "revoke_sessions")
			return
		}
		revoked = n
	}
	h.recordAdminUserAction(r, actorID, action, user.ID, "")

	updated, err := h.cfg.Users.GetUser(r.Context(), user.ID)
	if err != nil || updated == nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_set_user_disabled", "reload")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, struct {
		AdminUser
		SessionsRevoked int64 `json:"sessions_revoked"`
	}{AdminUser: toAdminUser(*updated), SessionsRevoked: revoked})
}

// revokeAdminUserSessionsHandler serves DELETE /api/admin/users/{user_id}/sessions.
func (h *Handler) revokeAdminUserSessionsHandler(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.guard().SystemAdmin(w, r)
	if !ok {
		return
	}
	if !httputil.RequireStore(w, h.cfg.Sessions, "sessions not configured") {
		return
	}
	user, ok := h.adminTargetUser(w, r)
	if !ok {
		return
	}
	n, err := h.cfg.Sessions.RevokeUserSessions(r.Context(), user.ID, time.Now().UTC())
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_revoke_sessions", "user_id", user.ID)
		return
	}
	h.recordAdminUserAction(r, actorID, coreaudit.SessionsRevoked, user.ID, "")
	httputil.WriteJSON(w, http.StatusOK, AdminSessionsRevokedResponse{Revoked: n})
}

// listAdminUserSessionsHandler serves GET /api/admin/users/{user_id}/sessions.
func (h *Handler) listAdminUserSessionsHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.guard().SystemAdmin(w, r); !ok {
		return
	}
	if !httputil.RequireStore(w, h.cfg.Sessions, "sessions not configured") {
		return
	}
	user, ok := h.adminTargetUser(w, r)
	if !ok {
		return
	}
	sessions, err := h.cfg.Sessions.ListUserSessions(r.Context(), user.ID, time.Now().UTC())
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_list_sessions", "user_id", user.ID)
		return
	}
	out := make([]AdminSession, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, AdminSession{
			SessionID:  s.SID,
			Platform:   s.Platform,
			AuthMethod: s.AuthMethod,
			CreatedAt:  s.CreatedAt,
			LastSeenAt: s.LastSeenAt,
			ExpiresAt:  s.AbsoluteExpiresAt,
		})
	}
	httputil.WriteJSON(w, http.StatusOK, AdminSessionsResponse{Sessions: out})
}

// revokeAdminUserSessionHandler serves
// DELETE /api/admin/users/{user_id}/sessions/{session_id}: signing one device
// out while the account's other sessions stay live.
func (h *Handler) revokeAdminUserSessionHandler(w http.ResponseWriter, r *http.Request) {
	actorID, ok := h.guard().SystemAdmin(w, r)
	if !ok {
		return
	}
	if !httputil.RequireStore(w, h.cfg.Sessions, "sessions not configured") {
		return
	}
	user, ok := h.adminTargetUser(w, r)
	if !ok {
		return
	}
	sessionID, ok := httputil.PathValue(w, r, "session_id")
	if !ok {
		return
	}
	// A session id names one chain for one account, so revoking by id alone
	// would work — but the URL claims this account, and the audit event will be
	// recorded against it, so confirm the session is really one of theirs and is
	// live. An id that is not returns 404 rather than a revoke recorded against
	// the wrong person.
	now := time.Now().UTC()
	sessions, err := h.cfg.Sessions.ListUserSessions(r.Context(), user.ID, now)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_revoke_session", "user_id", user.ID)
		return
	}
	found := false
	for _, s := range sessions {
		if s.SID == sessionID {
			found = true
			break
		}
	}
	if !found {
		httputil.WriteJSONError(w, http.StatusNotFound, "no live session with that id for this account")
		return
	}
	n, err := h.cfg.Sessions.RevokeSession(r.Context(), sessionID, now)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_revoke_session", "user_id", user.ID)
		return
	}
	h.recordAdminUserAction(r, actorID, coreaudit.SessionRevoked, user.ID, sessionID)
	httputil.WriteJSON(w, http.StatusOK, AdminSessionsRevokedResponse{Revoked: n})
}

// adminTargetUser resolves the {user_id} an admin route acts on.
func (h *Handler) adminTargetUser(w http.ResponseWriter, r *http.Request) (*coreidentity.User, bool) {
	if !httputil.RequireStore(w, h.cfg.Users, "accounts not configured") {
		return nil, false
	}
	userID, ok := httputil.PathValue(w, r, "user_id")
	if !ok {
		return nil, false
	}
	user, err := h.cfg.Users.GetUser(r.Context(), userID)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "admin_target_user", "user_id", userID)
		return nil, false
	}
	if user == nil {
		httputil.WriteJSONError(w, http.StatusNotFound, "account not found")
		return nil, false
	}
	return user, true
}

// recordAdminUserAction writes an account action taken through the admin API.
//
// Unlike the operator command's events, these name a person: the caller proved
// who they are, so the trail says so rather than naming the binary.
func (h *Handler) recordAdminUserAction(r *http.Request, actorID, action, targetUserID, detail string) {
	h.cfg.Audit.Record(r.Context(), coreaudit.Event{
		ActorType:  coreaudit.ActorUser,
		ActorID:    actorID,
		Action:     action,
		TargetType: "user",
		TargetID:   targetUserID,
		Detail:     detail,
	})
}
