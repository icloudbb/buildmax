// Package admin serves the deployment-scoped routes.
//
// It is a package rather than a file group because its boundary is real: every
// route here requires a system_admin grant and none is space-scoped. Keeping it
// beside the space-scoped routes meant one Handler could reach every store, so
// nothing but review stopped an admin route from growing a space's data or a
// space route from consulting a grant. This Config names what administration
// needs and nothing else.
package admin

import (
	"net/http"
	"time"

	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
	coreschema "github.com/icloudbb/buildmax/internal/core/schema"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/server/access"
	"github.com/icloudbb/buildmax/internal/service/audit"
	pluginsvc "github.com/icloudbb/buildmax/internal/service/plugin"
	"github.com/icloudbb/buildmax/internal/service/quota"
)

type Config struct {
	JWTSecret        string
	DefaultQuotaTier string

	Users              coreidentity.UserStore
	LoginCodes         coreidentity.LoginCodeStore
	RefreshTokens      coreidentity.RefreshTokenStore
	Sessions           coreidentity.AuthSessionStore
	ExternalIdentities coreidentity.ExternalIdentityStore
	Spaces             corespace.Store
	Grants             coreidentity.SystemGrantStore
	Audits             coreaudit.Store
	Models             coregw.ModelStore
	Schema             coreschema.Store
	TaskRuns           coretask.RunStore

	Quota *quota.Service
	// Plugins publishes releases and manages catalog entries. Nil is a
	// deployment with no Marketplace, which every route here reports rather
	// than pretending an empty catalog.
	Plugins *pluginsvc.Service
	// Audit records who did what. Nil discards it, which is what a deployment
	// without a database has.
	Audit *audit.Recorder

	Deployment       DeploymentInfo
	DependencyProbes []DependencyProbe
	// RedactedConfig is the operator-facing view of server.yaml, built by
	// internal/config so the decision about which fields may be shown lives
	// next to the struct.
	RedactedConfig any
	// OIDCStatus reports the live SSO provider health for the system view. Nil
	// means SSO is not configured. It is a closure so this package needs no
	// import of the OIDC provider; bootstrap adapts the provider into it. Unlike
	// a dependency probe, a degraded provider does not make the deployment
	// not-ready: an IdP fetch is retryable and must not fail /readyz.
	OIDCStatus OIDCStatusFunc
}

// OIDCStatusFunc reports the SSO provider's current health: whether discovery
// has succeeded, when it last did, and the last error's message (already safe
// to show).
type OIDCStatusFunc func() (available bool, lastRefresh time.Time, lastError string)

type Handler struct{ cfg Config }

func New(cfg Config) *Handler { return &Handler{cfg: cfg} }

func (h *Handler) guard() *access.Guard {
	return &access.Guard{
		JWTSecret: h.cfg.JWTSecret,
		Users:     h.cfg.Users,
		Spaces:    h.cfg.Spaces,
		Grants:    h.cfg.Grants,
		Sessions:  h.cfg.Sessions,
		Audit:     h.cfg.Audit,
	}
}

// Register adds the deployment-scoped routes.
//
// None takes a {space_id}: an admin route that looked space-scoped would invite
// exactly the confusion the boundary exists to prevent. See
// docs/design/system-administration.md.
func (h *Handler) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/admin/me", h.adminMeHandler)
	mux.HandleFunc("GET /api/admin/grants", h.listAdminGrantsHandler)
	mux.HandleFunc("POST /api/admin/grants", h.createAdminGrantHandler)
	mux.HandleFunc("DELETE /api/admin/grants/{user_id}", h.deleteAdminGrantHandler)
	mux.HandleFunc("GET /api/admin/users", h.listAdminUsersHandler)
	mux.HandleFunc("POST /api/admin/users", h.createAdminUserHandler)
	mux.HandleFunc("GET /api/admin/users/{user_id}", h.getAdminUserHandler)
	mux.HandleFunc("POST /api/admin/users/{user_id}/login-code", h.issueAdminLoginCodeHandler)
	// Stored-flag transitions are a state sub-resource, not RPC actions. See
	// the route conventions in docs/contribute/architecture/server.md
	mux.HandleFunc("PUT /api/admin/users/{user_id}/state", h.setAdminUserStateHandler)
	mux.HandleFunc("GET /api/admin/users/{user_id}/identities", h.listAdminUserIdentitiesHandler)
	mux.HandleFunc("DELETE /api/admin/users/{user_id}/identities/{identity_id}", h.unlinkAdminUserIdentityHandler)
	mux.HandleFunc("GET /api/admin/users/{user_id}/sessions", h.listAdminUserSessionsHandler)
	mux.HandleFunc("DELETE /api/admin/users/{user_id}/sessions", h.revokeAdminUserSessionsHandler)
	mux.HandleFunc("DELETE /api/admin/users/{user_id}/sessions/{session_id}", h.revokeAdminUserSessionHandler)
	mux.HandleFunc("GET /api/admin/system", h.adminSystemHandler)
	mux.HandleFunc("GET /api/admin/config", h.adminConfigHandler)
	mux.HandleFunc("GET /api/admin/audit-events", h.listAdminAuditEventsHandler)
	mux.HandleFunc("GET /api/admin/audit-events/export", h.exportAdminAuditEventsHandler)
	mux.HandleFunc("GET /api/admin/spaces", h.listAdminSpacesHandler)
	mux.HandleFunc("GET /api/admin/spaces/{space_id}", h.getAdminSpaceHandler)
	mux.HandleFunc("GET /api/admin/llm/models", h.listAdminModelsHandler)
	mux.HandleFunc("POST /api/admin/llm/models", h.createAdminModelHandler)
	mux.HandleFunc("PUT /api/admin/llm/models/{model_id}/state", h.setAdminModelStateHandler)
	mux.HandleFunc("GET /api/admin/plugins", h.listAdminPluginsHandler)
	mux.HandleFunc("POST /api/admin/plugins", h.createAdminPluginHandler)
	mux.HandleFunc("GET /api/admin/plugins/{plugin_name}/releases", h.listAdminPluginReleasesHandler)
	mux.HandleFunc("POST /api/admin/plugins/{plugin_name}/releases", h.publishAdminPluginReleaseHandler)
	mux.HandleFunc("PUT /api/admin/plugins/{plugin_name}/releases/{version}/state", h.setAdminReleaseStateHandler)
	mux.HandleFunc("PUT /api/admin/plugins/{plugin_name}/state", h.setAdminPluginStateHandler)
}

// systemRoleAdmin keeps admin_system.go from importing the model package for
// one constant.
func systemRoleAdmin() string { return coreidentity.SystemRoleAdmin }
