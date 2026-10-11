package space

import (
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"net/http"
	"time"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/server/httputil"
	"github.com/icloudbb/buildmax/internal/service/agent"
	"github.com/icloudbb/buildmax/internal/service/workflow"
)

type AgentResponse struct {
	ID           string `json:"id"`
	UserID       string `json:"user_id"`
	SpaceID      string `json:"space_id,omitempty"`
	Name         string `json:"name"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
	// Model is the catalog model name this agent's runs call, or empty for the
	// deployment default. See agentdef.Agent.Model.
	Model   string   `json:"model,omitempty"`
	Plugins []string `json:"plugins,omitempty"`
	// SandboxNetworkTier and SandboxFilesystemTier declare this agent's
	// worker sandbox needs. Empty means the strictest tier on that axis. See
	// docs/design/agent-sandbox-policy.md.
	SandboxNetworkTier    string                     `json:"sandbox_network_tier,omitempty"`
	SandboxFilesystemTier string                     `json:"sandbox_filesystem_tier,omitempty"`
	SecretConsumption     agentdef.SecretConsumption `json:"secret_consumption,omitempty"`
	// SecretGrantProblems are the grants a run started now would not be
	// given, so whoever can run the Agent is warned before a run fails on one.
	SecretGrantProblems []secretGrantProblemResponse `json:"secret_grant_problems,omitempty"`
	Revision            int                          `json:"revision"`
	CreatedAt           time.Time                    `json:"created_at"`
}

// secretGrantProblemResponse names a grant by what secret_consumption already
// shows and says why it would not resolve. It carries nothing else about the
// Secret -- no name, state, or item list -- because members, who may not read
// Secret metadata, read it too. See docs/design/space-secrets.md §10.
type secretGrantProblemResponse struct {
	SecretID string                 `json:"secret_id"`
	Item     string                 `json:"item,omitempty"`
	Problem  coretask.SecretProblem `json:"problem"`
	Optional bool                   `json:"optional,omitempty"`
}

type agentRevisionResponse struct {
	AgentID               string                     `json:"agent_id"`
	Revision              int                        `json:"revision"`
	Name                  string                     `json:"name"`
	Description           string                     `json:"description"`
	Instructions          string                     `json:"instructions"`
	Model                 string                     `json:"model,omitempty"`
	Plugins               []string                   `json:"plugins,omitempty"`
	SandboxNetworkTier    string                     `json:"sandbox_network_tier,omitempty"`
	SandboxFilesystemTier string                     `json:"sandbox_filesystem_tier,omitempty"`
	SecretConsumption     agentdef.SecretConsumption `json:"secret_consumption,omitempty"`
	CreatedBy             string                     `json:"created_by"`
	CreatedAt             time.Time                  `json:"created_at"`
}

type agentRevisionListResponse struct {
	Revisions []agentRevisionResponse `json:"revisions"`
	Total     int                     `json:"total"`
}

type createAgentRequest struct {
	Name         string `json:"name"`
	Description  string `json:"description"`
	Instructions string `json:"instructions"`
	// Model is the catalog model name this agent's runs call. Empty is the
	// deployment default. Rejected on write if the deployment offers a catalog
	// and does not list it. See agentdef.Agent.Model.
	Model string `json:"model,omitempty"`
	// Plugins names catalog plugins, never releases: the version comes from
	// the space's activation.
	Plugins []string `json:"plugins,omitempty"`
	// SandboxNetworkTier and SandboxFilesystemTier declare this agent's
	// worker sandbox needs. Empty means the strictest tier on that axis.
	// Rejected on write if not one of config.ValidSandboxNetworkTier /
	// ValidSandboxFilesystemTier. See docs/design/agent-sandbox-policy.md.
	SandboxNetworkTier    string                     `json:"sandbox_network_tier,omitempty"`
	SandboxFilesystemTier string                     `json:"sandbox_filesystem_tier,omitempty"`
	SecretConsumption     agentdef.SecretConsumption `json:"secret_consumption,omitempty"`
}

// patchAgentRequest replaces the whole definition, plugins included. An absent
// list is an empty one, because a patch that could not clear the selection
// would leave no way to stop an agent loading a plugin. The same is true of
// the sandbox tiers: an absent field resets that axis to its strictest tier,
// not "leave it as it was."
type patchAgentRequest struct {
	Name                  string                     `json:"name"`
	Description           string                     `json:"description"`
	Instructions          string                     `json:"instructions"`
	Model                 string                     `json:"model,omitempty"`
	Plugins               []string                   `json:"plugins,omitempty"`
	SandboxNetworkTier    string                     `json:"sandbox_network_tier,omitempty"`
	SandboxFilesystemTier string                     `json:"sandbox_filesystem_tier,omitempty"`
	SecretConsumption     agentdef.SecretConsumption `json:"secret_consumption,omitempty"`
}

// agentResponses renders agents with the health of their Secret grants, read
// once for the whole set.
func (h *Handler) agentResponses(r *http.Request, agents ...agentdef.Agent) []AgentResponse {
	problems := h.agentService().SecretGrantProblems(r.Context(), agents)
	out := make([]AgentResponse, len(agents))
	for i, a := range agents {
		out[i] = agentToResponse(a)
		for _, p := range problems[a.ID] {
			out[i].SecretGrantProblems = append(out[i].SecretGrantProblems, secretGrantProblemResponse{
				SecretID: p.SecretID, Item: p.Item, Problem: p.Problem, Optional: p.Optional,
			})
		}
	}
	return out
}

func agentToResponse(a agentdef.Agent) AgentResponse {
	return AgentResponse{
		ID:                    a.ID,
		UserID:                a.UserID,
		SpaceID:               a.SpaceID,
		Name:                  a.Name,
		Description:           a.Description,
		Instructions:          a.Instructions,
		Model:                 a.Model,
		Plugins:               a.Plugins,
		SandboxNetworkTier:    a.SandboxNetworkTier,
		SandboxFilesystemTier: a.SandboxFilesystemTier,
		SecretConsumption:     a.SecretConsumption,
		Revision:              a.Revision,
		CreatedAt:             a.CreatedAt,
	}
}

func agentRevisionToResponse(rev agentdef.Revision) agentRevisionResponse {
	return agentRevisionResponse{
		AgentID:               rev.AgentID,
		Revision:              rev.Revision,
		Name:                  rev.Name,
		Description:           rev.Description,
		Instructions:          rev.Instructions,
		Model:                 rev.Model,
		Plugins:               rev.Plugins,
		SandboxNetworkTier:    rev.SandboxNetworkTier,
		SandboxFilesystemTier: rev.SandboxFilesystemTier,
		SecretConsumption:     rev.SecretConsumption,
		CreatedBy:             rev.CreatedBy,
		CreatedAt:             rev.CreatedAt,
	}
}

func (h *Handler) agentService() *agent.Service {
	return h.agents
}

func newSpaceAgentService(cfg Config, workflowUsage *workflow.Service) *agent.Service {
	svc := &agent.Service{Agents: cfg.Agents, Spaces: cfg.Spaces, Audit: cfg.Audit}
	if workflowUsage != nil {
		svc.Workflows = workflowUsage
	}
	// Nil when the deployment has no Marketplace, which is what makes naming a
	// plugin a refusal there rather than a stored selection nothing resolves.
	if cfg.Plugins != nil && cfg.Plugins.Activations != nil {
		svc.Plugins = cfg.Plugins
	}
	// Nil when the deployment has no secret store, which makes consuming a
	// Secret a refusal there rather than a stored config nothing resolves.
	if cfg.Secrets != nil {
		svc.Secrets = cfg.Secrets
	}
	// Nil in a direct-transport deployment with no catalog to check against;
	// a model name is then stored unchecked. See agent.Service.Models.
	if cfg.Models != nil {
		svc.Models = cfg.Models
	}
	return svc
}

// writeAgentServiceError maps what an agent write can refuse. It routes through
// the activation mapping because an edit that names a plugin can fail for that
// plugin's reasons — the release contributes a hook, the space has not activated
// it — and those answers belong to the caller, not in a 500.
func (h *Handler) writeAgentServiceError(w http.ResponseWriter, err error) bool {
	return writePluginActivationError(w, err)
}

func (h *Handler) listAgentsHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Agents, "agents not configured")
	if !ok {
		return
	}
	list, err := h.agentService().ListAgents(r.Context(), spaceID)
	if err != nil {
		if h.writeAgentServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "list_agents", "user_id", userID, "space_id", spaceID)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, h.agentResponses(r, list...))
}

func (h *Handler) createAgentHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Agents, "agents not configured")
	if !ok {
		return
	}
	if _, ok := h.guard().SpaceAction(w, r, userID, spaceID, corespace.ActionManageAgents); !ok {
		return
	}
	var req createAgentRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	created, err := h.agentService().CreateAgent(r.Context(), agent.CreateCmd{
		SpaceID:               spaceID,
		UserID:                userID,
		Name:                  req.Name,
		Description:           req.Description,
		Instructions:          req.Instructions,
		Model:                 req.Model,
		Plugins:               req.Plugins,
		SandboxNetworkTier:    req.SandboxNetworkTier,
		SandboxFilesystemTier: req.SandboxFilesystemTier,
		SecretConsumption:     req.SecretConsumption,
	})
	if err != nil {
		if h.writeAgentServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "create_agent", "user_id", userID, "space_id", spaceID)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, h.agentResponses(r, *created)[0])
}

func (h *Handler) getAgentHandler(w http.ResponseWriter, r *http.Request) {
	_, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Agents, "agents not configured")
	if !ok {
		return
	}
	agentID, ok := httputil.PathValue(w, r, "agent_id")
	if !ok {
		return
	}
	found, err := h.agentService().GetAgent(r.Context(), spaceID, agentID)
	if err != nil {
		if h.writeAgentServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "get_agent", "agent_id", agentID)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, h.agentResponses(r, *found)[0])
}

func (h *Handler) patchAgentHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Agents, "agents not configured")
	if !ok {
		return
	}
	if _, ok := h.guard().SpaceAction(w, r, userID, spaceID, corespace.ActionManageAgents); !ok {
		return
	}
	agentID, ok := httputil.PathValue(w, r, "agent_id")
	if !ok {
		return
	}
	var req patchAgentRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	updated, err := h.agentService().UpdateAgent(r.Context(), agent.UpdateCmd{
		SpaceID:               spaceID,
		UserID:                userID,
		AgentID:               agentID,
		Name:                  req.Name,
		Description:           req.Description,
		Instructions:          req.Instructions,
		Model:                 req.Model,
		Plugins:               req.Plugins,
		SandboxNetworkTier:    req.SandboxNetworkTier,
		SandboxFilesystemTier: req.SandboxFilesystemTier,
		SecretConsumption:     req.SecretConsumption,
	})
	if err != nil {
		if h.writeAgentServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "patch_agent", "agent_id", agentID)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, h.agentResponses(r, *updated)[0])
}

// listAgentRevisionsHandler returns an agent's recorded definitions, newest
// first. Reading history needs no more than space membership: it is the same
// content the agent itself exposes, at earlier points in time.
func (h *Handler) listAgentRevisionsHandler(w http.ResponseWriter, r *http.Request) {
	_, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Agents, "agents not configured")
	if !ok {
		return
	}
	agentID, ok := httputil.PathValue(w, r, "agent_id")
	if !ok {
		return
	}
	limit, offset := httputil.LimitOffset(r.URL.Query(), "limit", "offset", httputil.BrowsePageDefault, httputil.BrowsePageMax)
	list, total, err := h.agentService().ListRevisions(r.Context(), spaceID, agentID, limit, offset)
	if err != nil {
		if h.writeAgentServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "list_agent_revisions", "agent_id", agentID)
		return
	}
	out := make([]agentRevisionResponse, len(list))
	for i := range list {
		out[i] = agentRevisionToResponse(list[i])
	}
	httputil.WriteJSON(w, http.StatusOK, agentRevisionListResponse{Revisions: out, Total: total})
}

func (h *Handler) restoreAgentRevisionHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Agents, "agents not configured")
	if !ok {
		return
	}
	if _, ok := h.guard().SpaceAction(w, r, userID, spaceID, corespace.ActionManageAgents); !ok {
		return
	}
	agentID, ok := httputil.PathValue(w, r, "agent_id")
	if !ok {
		return
	}
	revision, ok := httputil.PathValueInt(w, r, "revision")
	if !ok {
		return
	}
	updated, err := h.agentService().RestoreRevision(r.Context(), agent.RestoreRevisionCmd{
		SpaceID:  spaceID,
		UserID:   userID,
		AgentID:  agentID,
		Revision: revision,
	})
	if err != nil {
		if h.writeAgentServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "restore_agent_revision", "agent_id", agentID)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, h.agentResponses(r, *updated)[0])
}

func (h *Handler) deleteAgentHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Agents, "agents not configured")
	if !ok {
		return
	}
	if _, ok := h.guard().SpaceAction(w, r, userID, spaceID, corespace.ActionManageAgents); !ok {
		return
	}
	agentID, ok := httputil.PathValue(w, r, "agent_id")
	if !ok {
		return
	}
	if err := h.agentService().DeleteAgent(r.Context(), spaceID, agentID, userID); err != nil {
		if h.writeAgentServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "delete_agent", "agent_id", agentID)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
