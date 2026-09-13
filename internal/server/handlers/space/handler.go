// Package space serves what a space owns: its membership, its agents, its
// consumption, and its audit trail.
//
// The boundary is the one the product already has. Space is the ownership and
// authorization unit for Portal resources, so a package holding exactly the
// stores a space's own routes read makes that unit something the compiler knows
// about rather than something every reviewer has to remember.
package space

import (
	"net/http"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	coreschedule "github.com/icloudbb/buildmax/internal/core/schedule"
	coresecret "github.com/icloudbb/buildmax/internal/core/secret"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/server/access"
	agentsvc "github.com/icloudbb/buildmax/internal/service/agent"
	"github.com/icloudbb/buildmax/internal/service/audit"
	"github.com/icloudbb/buildmax/internal/service/plugin"
	"github.com/icloudbb/buildmax/internal/service/quota"
	schedulesvc "github.com/icloudbb/buildmax/internal/service/schedule"
	secretsvc "github.com/icloudbb/buildmax/internal/service/secret"
	spacesvc "github.com/icloudbb/buildmax/internal/service/space"
	"github.com/icloudbb/buildmax/internal/service/workflow"
)

type Config struct {
	JWTSecret        string
	DefaultQuotaTier string

	Spaces corespace.Store
	Users  coreidentity.UserStore
	Agents agentdef.Store
	Audits coreaudit.Store
	// LoginCodes backs the space-scoped access-recovery route only -- issuing a
	// code for a locked-out member of the caller's own space. Nil leaves that
	// route unavailable, which is what a deployment with no login-code store
	// has. See docs/design/space-membership-lifecycle.md §5.4.
	LoginCodes coreidentity.LoginCodeStore
	// Workflows answers one question here -- which published workflows still
	// name an agent -- so that deleting one cannot silently break them. Nil
	// leaves that check unmade, which is what a deployment without workflows
	// has.
	Workflows coreworkflow.Store
	// Schedules backs the recurring-schedule routes. Nil leaves them reporting
	// the feature is off. See docs/design/scheduled-agent-execution.md.
	Schedules coreschedule.Store

	Quota *quota.Service
	Audit *audit.Recorder
	// Plugins carries the space half of plugin distribution: which releases a
	// space's background runs may use. Nil in a deployment without a
	// Marketplace, which is why every route here checks before using it.
	Plugins *plugin.Service
	// Secrets is the low-level Secret read the agent consumption validator
	// uses. Nil when the deployment has no secret store; then consuming a
	// Secret is refused. See docs/design/space-secrets.md.
	Secrets coresecret.Store
	// SecretService backs the Secret management routes. Nil when the secret
	// feature is off (no KEK file configured), and then those routes report it.
	SecretService *secretsvc.Service
	// Models enumerates the deployment's model catalog so an agent naming a
	// model it does not offer is refused on write. Nil in a direct-transport
	// deployment, which has no catalog; a model name is then stored unchecked.
	// See agentsvc.ModelCatalog.
	Models agentsvc.ModelCatalog
}

type Handler struct {
	cfg Config

	spaces    *spacesvc.Service
	agents    *agentsvc.Service
	schedules *schedulesvc.Service
}

func New(cfg Config) *Handler {
	h := &Handler{cfg: cfg}
	h.spaces = newSpaceService(cfg)
	h.agents = newSpaceAgentService(cfg, newWorkflowUsage(cfg))
	h.schedules = newScheduleService(cfg)
	return h
}

func (h *Handler) guard() *access.Guard {
	return &access.Guard{
		JWTSecret: h.cfg.JWTSecret,
		Users:     h.cfg.Users,
		Spaces:    h.cfg.Spaces,
		Audit:     h.cfg.Audit,
	}
}

// workflowUsage answers only "which published workflows name this agent".
//
// Built from the workflow store alone rather than from a full workflow service:
// the delete guard needs one query, and a service wired for orchestration would
// tie an agent edit to task dispatch.
func newWorkflowUsage(cfg Config) *workflow.Service {
	if cfg.Workflows == nil {
		return nil
	}
	return &workflow.Service{Workflows: cfg.Workflows}
}

func (h *Handler) Register(mux *http.ServeMux) {
	// Spaces and members
	mux.HandleFunc("GET /api/spaces", h.listSpacesHandler)
	mux.HandleFunc("POST /api/spaces", h.createSpaceHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/members", h.listSpaceMembersHandler)
	mux.HandleFunc("DELETE /api/spaces/{space_id}/members/{user_id}", h.removeSpaceMemberHandler)
	mux.HandleFunc("PATCH /api/spaces/{space_id}/members/{user_id}", h.setMemberRoleHandler)
	mux.HandleFunc("POST /api/spaces/{space_id}/members/{user_id}/login-code", h.issueMemberLoginCodeHandler)

	// Invitations. See docs/design/space-membership-lifecycle.md.
	mux.HandleFunc("POST /api/spaces/{space_id}/invitations", h.inviteMemberHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/invitations", h.listSpaceInvitationsHandler)
	mux.HandleFunc("DELETE /api/spaces/{space_id}/invitations/{invitation_id}", h.revokeInvitationHandler)
	mux.HandleFunc("GET /api/invitations", h.listMyInvitationsHandler)
	mux.HandleFunc("POST /api/invitations/{invitation_id}/accept", h.acceptInvitationHandler)

	// Agents
	mux.HandleFunc("GET /api/spaces/{space_id}/agents", h.listAgentsHandler)
	mux.HandleFunc("POST /api/spaces/{space_id}/agents", h.createAgentHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/agents/{agent_id}", h.getAgentHandler)
	mux.HandleFunc("PATCH /api/spaces/{space_id}/agents/{agent_id}", h.patchAgentHandler)
	mux.HandleFunc("DELETE /api/spaces/{space_id}/agents/{agent_id}", h.deleteAgentHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/agents/{agent_id}/revisions", h.listAgentRevisionsHandler)
	mux.HandleFunc("POST /api/spaces/{space_id}/agents/{agent_id}/revisions/{revision}/restore", h.restoreAgentRevisionHandler)

	// Schedules -- recurring time triggers that admit Agent Tasks.
	mux.HandleFunc("GET /api/spaces/{space_id}/schedules", h.listSchedulesHandler)
	mux.HandleFunc("POST /api/spaces/{space_id}/schedules", h.createScheduleHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/schedules/{schedule_id}", h.getScheduleHandler)
	mux.HandleFunc("PATCH /api/spaces/{space_id}/schedules/{schedule_id}", h.patchScheduleHandler)
	mux.HandleFunc("DELETE /api/spaces/{space_id}/schedules/{schedule_id}", h.deleteScheduleHandler)

	// Plugin activation
	mux.HandleFunc("GET /api/spaces/{space_id}/plugin-activations", h.listPluginActivationsHandler)
	mux.HandleFunc("POST /api/spaces/{space_id}/plugin-activations", h.activatePluginHandler)
	mux.HandleFunc("PATCH /api/spaces/{space_id}/plugin-activations/{plugin_name}", h.patchPluginActivationHandler)
	mux.HandleFunc("PUT /api/spaces/{space_id}/plugin-curation", h.setPluginCurationHandler)

	// Sandbox defaults -- the tiers an agent that declares neither inherits.
	// See docs/design/agent-sandbox-policy.md §9 M3.
	mux.HandleFunc("GET /api/spaces/{space_id}/sandbox-defaults", h.getSandboxDefaultsHandler)
	mux.HandleFunc("PUT /api/spaces/{space_id}/sandbox-defaults", h.setSandboxDefaultsHandler)

	// Space-wide guidance inherited by background Agent runs.
	mux.HandleFunc("GET /api/spaces/{space_id}/agent-instructions", h.getAgentInstructionsHandler)
	mux.HandleFunc("PUT /api/spaces/{space_id}/agent-instructions", h.setAgentInstructionsHandler)

	// Space Secrets. Owner-only; values are write-only, with no reveal route.
	// See docs/design/space-secrets.md.
	mux.HandleFunc("GET /api/spaces/{space_id}/secrets", h.listSecretsHandler)
	mux.HandleFunc("POST /api/spaces/{space_id}/secrets", h.createSecretHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/secrets/{secret_id}", h.getSecretHandler)
	mux.HandleFunc("PATCH /api/spaces/{space_id}/secrets/{secret_id}", h.editSecretHandler)
	mux.HandleFunc("PUT /api/spaces/{space_id}/secrets/{secret_id}/state", h.setSecretStateHandler)

	// Usage and the audit trail
	mux.HandleFunc("GET /api/usage", h.usageHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/usage", h.spaceUsageHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/audit-events", h.listAuditEventsHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/audit-events/export", h.exportAuditEventsHandler)
}
