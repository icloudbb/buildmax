package handlers

import (
	"context"
	accountroutes "github.com/icloudbb/buildmax/internal/server/handlers/account"
	"github.com/icloudbb/buildmax/internal/server/handlers/admin"
	artifactroutes "github.com/icloudbb/buildmax/internal/server/handlers/artifact"
	authroutes "github.com/icloudbb/buildmax/internal/server/handlers/auth"
	spaceroutes "github.com/icloudbb/buildmax/internal/server/handlers/space"
	"github.com/icloudbb/buildmax/internal/server/handlers/work"
	"github.com/icloudbb/buildmax/internal/server/handlers/worker"
	agentsvc "github.com/icloudbb/buildmax/internal/service/agent"
	artifactsvc "github.com/icloudbb/buildmax/internal/service/artifact"
	"github.com/icloudbb/buildmax/internal/service/conversation"
	issuesvc "github.com/icloudbb/buildmax/internal/service/issue"
	"github.com/icloudbb/buildmax/internal/service/llmgateway"
	"github.com/icloudbb/buildmax/internal/service/task"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/server/access"
)

// guard answers who is calling and whether they may proceed.
//
// Built per call from the stores this handler holds, so it stays correct if a
// store is swapped in a test after construction.
func (h *Handler) guard() *access.Guard {
	return &access.Guard{
		JWTSecret: h.cfg.JWTSecret,
		Users:     h.cfg.UserStore,
		Spaces:    h.cfg.SpaceStore,
		Grants:    h.cfg.SystemGrantStore,
		Sessions:  h.cfg.AuthSessionStore,
		Audit:     h.cfg.Audit,
	}
}

// refuseDisabled answers a disabled account and reports whether the caller may
// continue.
//

// adminHandler builds the deployment-scoped surface from the fields it needs.
//
// Constructed here rather than injected so internal/server keeps assembling one
// Config; what changed is that administration can only see this slice of it.
func (h *Handler) buildAdminHandler() *admin.Handler {
	return admin.New(admin.Config{
		JWTSecret:        h.cfg.JWTSecret,
		DefaultQuotaTier: h.cfg.DefaultQuotaTier,
		Users:            h.cfg.UserStore,
		LoginCodes:       h.cfg.LoginCodeStore,
		RefreshTokens:    h.cfg.RefreshTokenStore,
		Sessions:         h.cfg.AuthSessionStore,
		Spaces:           h.cfg.SpaceStore,
		Grants:           h.cfg.SystemGrantStore,
		Audits:           h.cfg.AuditStore,
		Models:           h.cfg.LLMModelStore,
		Plugins:          h.cfg.PluginService,
		Schema:           h.cfg.SchemaStore,
		TaskRuns:         h.cfg.TaskRunStore,
		Quota:            h.cfg.QuotaService,
		Audit:            h.cfg.Audit,
		Deployment:       h.cfg.Deployment,
		DependencyProbes: h.cfg.DependencyProbes,
		RedactedConfig:   h.cfg.RedactedConfig,
		OIDCStatus:       h.cfg.OIDCStatus,
	})
}

// workerHandler builds the worker API from the fields it needs.
//
// OnTerminal fans a finished run out to whoever is watching: the connected
// clients this package tracks, and then the server's own callback. The worker
// package is told what to call, not who is listening.
func (h *Handler) buildWorkerHandler() *worker.Handler {
	return worker.New(worker.Config{
		JWTSecret:     h.cfg.JWTSecret,
		WorkerLLM:     h.cfg.WorkerLLM,
		TaskRuns:      h.cfg.TaskRunStore,
		Agents:        h.cfg.AgentStore,
		Spaces:        h.cfg.SpaceStore,
		Gateway:       h.cfg.LLMGateway,
		Artifacts:     h.artifacts,
		Issues:        h.workerIssueAccess(),
		Hub:           h.hub,
		OnTerminal:    h.terminalListeners,
		TerminalGroup: h.terminal,
		// The activation store rather than the plugin service: this route
		// resolves what a space already activated and must not be able to
		// activate anything on a run token's behalf.
		Activations: h.activationStore(),
		Plugins:     h.cfg.PluginService,
		// The secret service materializes a run's declared grants. Nil when the
		// feature is off, which the route reads as an empty grant set.
		Secrets: h.secretMaterializer(),
		// The store records the audit of what a run was granted, independent of
		// the feature flag: recording is fail-open. Nil when no secret store is
		// configured, which records nothing.
		SecretAudit: h.secretGrantRecorder(),
		// Workspace checkpoints: seed finalization and base/restore bookkeeping.
		// Nil disables the routes, which is what a deployment with no checkpoint
		// storage has.
		Checkpoints:   h.cfg.WorkspaceCheckpoints,
		WorkspaceRuns: h.cfg.WorkspaceCheckpointStore,
	})
}

// secretMaterializer adapts the secret service to the worker route's narrow
// materializer, or nil when the feature is off.
func (h *Handler) secretMaterializer() worker.SecretMaterializer {
	if h.cfg.SecretService == nil {
		return nil
	}
	return h.cfg.SecretService
}

// secretGrantRecorder adapts the secret store to the worker route's audit
// recorder, or nil when no secret store is configured.
func (h *Handler) secretGrantRecorder() worker.SecretGrantRecorder {
	if h.cfg.SecretStore == nil {
		return nil
	}
	return h.cfg.SecretStore
}

// workerIssueAccess is the Issue capability a run token gets, or nil when this
// deployment stores no issues or no comments. Both stores are required: an
// agent that can read an issue but not report on it is worse than one that
// knows it has neither.
func (h *Handler) workerIssueAccess() worker.IssueAccess {
	if h.cfg.IssueStore == nil || h.cfg.IssueCommentStore == nil {
		return nil
	}
	return &issuesvc.Service{Issues: h.cfg.IssueStore, Comments: h.cfg.IssueCommentStore}
}

// activationStore is the plugin service's activation store, or nil when this
// deployment has no Marketplace.
func (h *Handler) activationStore() worker.ActivationReader {
	if h.cfg.PluginService == nil {
		return nil
	}
	return h.cfg.PluginService.Activations
}

func (h *Handler) terminalListeners(ctx context.Context, info coretask.RunTerminalInfo) {
	h.reportTaskRunTerminal(ctx, info)
	if h.cfg.OnTaskRunTerminal != nil {
		h.cfg.OnTaskRunTerminal(ctx, info)
	}
}

// authHandler builds the session routes from the fields they need.
func (h *Handler) buildAuthHandler() *authroutes.Handler {
	return authroutes.New(authroutes.Config{
		JWTSecret:            h.cfg.JWTSecret,
		AllowSignup:          h.cfg.AllowSignup,
		LocalLogin:           h.cfg.LocalLogin,
		OIDCEnabled:          h.cfg.OIDCEnabled,
		OIDCDisplayName:      h.cfg.OIDCDisplayName,
		DefaultQuotaTier:     h.cfg.DefaultQuotaTier,
		AccessTokenTTL:       h.cfg.AccessTokenTTL,
		RefreshTokenTTL:      h.cfg.RefreshTokenTTL,
		RefreshRotationGrace: h.cfg.RefreshRotationGrace,
		SessionAbsoluteTTL:   h.cfg.SessionAbsoluteTTL,
		Users:                h.cfg.UserStore,
		LoginCodes:           h.cfg.LoginCodeStore,
		Passwords:            h.cfg.PasswordStore,
		RefreshTokens:        h.cfg.RefreshTokenStore,
		Sessions:             h.cfg.AuthSessionStore,
		Audit:                h.cfg.Audit,
	})
}

// buildAccountHandler builds the account surface: the routes the acting subject
// owns across spaces, holding only the stores those routes read.
func (h *Handler) buildAccountHandler() *accountroutes.Handler {
	return accountroutes.New(accountroutes.Config{
		JWTSecret:   h.cfg.JWTSecret,
		Users:       h.cfg.UserStore,
		Sessions:    h.cfg.AuthSessionStore,
		WebhookKeys: h.cfg.UserWebhookKeyStore,
		Audit:       h.cfg.Audit,
	})
}

// spaceHandler builds the space surface from the stores a space's own routes read.
func (h *Handler) buildSpaceHandler() *spaceroutes.Handler {
	return spaceroutes.New(spaceroutes.Config{
		JWTSecret:        h.cfg.JWTSecret,
		DefaultQuotaTier: h.cfg.DefaultQuotaTier,
		Spaces:           h.cfg.SpaceStore,
		Users:            h.cfg.UserStore,
		Sessions:         h.cfg.AuthSessionStore,
		Agents:           h.cfg.AgentStore,
		Audits:           h.cfg.AuditStore,
		Workflows:        h.cfg.WorkflowStore,
		Schedules:        h.cfg.ScheduleStore,
		Quota:            h.cfg.QuotaService,
		Audit:            h.cfg.Audit,
		Plugins:          h.cfg.PluginService,
		LoginCodes:       h.cfg.LoginCodeStore,
		Secrets:          h.cfg.SecretStore,
		SecretService:    h.cfg.SecretService,
		Models:           h.modelCatalog(),
	})
}

// modelCatalog adapts the LLM gateway to the narrow name list the agent service
// checks a chosen model against. Nil when this deployment has no gateway (a
// direct-transport deployment), which leaves an agent's model name stored
// unchecked because there is no catalog to check it against.
func (h *Handler) modelCatalog() agentsvc.ModelCatalog {
	if h.cfg.LLMGateway == nil {
		return nil
	}
	return gatewayModelCatalog{gw: h.cfg.LLMGateway}
}

type gatewayModelCatalog struct{ gw *llmgateway.Service }

func (g gatewayModelCatalog) ModelNames(ctx context.Context) ([]string, error) {
	models, err := g.gw.Models(ctx)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(models))
	for _, m := range models {
		names = append(names, m.Name)
	}
	return names, nil
}

// artifactHandler builds the artifact surface.
//
// It is handed the capability rather than the pieces, and holds neither issue,
// task, nor conversation store: an artifact service that could reach them is
// one edit away from an artifact that belongs to a run again.
func (h *Handler) buildArtifactHandler() *artifactroutes.Handler {
	return artifactroutes.New(artifactroutes.Config{
		JWTSecret: h.cfg.JWTSecret,
		Users:     h.cfg.UserStore,
		Spaces:    h.cfg.SpaceStore,
		Sessions:  h.cfg.AuthSessionStore,
		Artifacts: h.artifacts,
		Audit:     h.cfg.Audit,
	})
}

// artifactService returns nil when either half of the capability is missing, so
// "not configured" is decided once here rather than at each route.
func (h *Handler) buildArtifactService() *artifactsvc.Service {
	if h.cfg.ArtifactStore == nil || h.cfg.ArtifactStorage == nil {
		return nil
	}
	svc := &artifactsvc.Service{
		Artifacts:     h.cfg.ArtifactStore,
		Storage:       h.cfg.ArtifactStorage,
		Audit:         h.cfg.Audit,
		MaxFileBytes:  h.cfg.MaxArtifactBytes,
		Shares:        h.cfg.ArtifactShareStore,
		PublicBaseURL: h.cfg.ArtifactPublicBaseURL,
		ShareTTL:      h.cfg.ArtifactShareTTL,
	}
	// Assigned through the nil check rather than directly: a typed nil in the
	// interface field would satisfy "not nil" and then panic on the first call.
	if h.cfg.QuotaService != nil {
		svc.Quota = h.cfg.QuotaService
	}
	return svc
}

// workHandler builds the work surface from the stores those routes read.
func (h *Handler) buildWorkHandler() *work.Handler {
	return work.New(work.Config{
		JWTSecret:       h.cfg.JWTSecret,
		Users:           h.cfg.UserStore,
		Sessions:        h.cfg.AuthSessionStore,
		Issues:          h.cfg.IssueStore,
		IssueComments:   h.cfg.IssueCommentStore,
		Workflows:       h.cfg.WorkflowStore,
		Tasks:           h.cfg.TaskStore,
		TaskRuns:        h.cfg.TaskRunStore,
		Agents:          h.cfg.AgentStore,
		Schedules:       h.cfg.ScheduleStore,
		Spaces:          h.cfg.SpaceStore,
		Conversations:   h.cfg.ConversationStore,
		Messages:        h.cfg.ConversationMessageStore,
		LLMCalls:        h.cfg.LLMCallStore,
		PersistStorage:  h.cfg.PersistStorage,
		Artifacts:       h.artifacts,
		WorkspacesDir:   h.cfg.WorkspacesDir,
		Quota:           h.cfg.QuotaService,
		TitleGenerator:  h.cfg.TitleGenerator,
		ConversationLLM: h.cfg.ConversationLLMClient,
		Audit:           h.cfg.Audit,
		Hub:             h.hub,
		Turns:           h.turns,
		OnTerminal:      h.terminalListeners,
		TerminalGroup:   h.terminal,
		Drain:           h.cfg.Drain,
	})
}

// conversationService answers a Tier 1 turn for the socket, which is the only
// thing the root package still runs itself.
func (h *Handler) buildConversationService() *conversation.Service {
	var quotaChecker task.QuotaChecker
	if h.cfg.QuotaService != nil {
		quotaChecker = h.cfg.QuotaService
	}
	var workflowSteps task.WorkflowStepLookup
	if h.cfg.WorkflowStore != nil {
		workflowSteps = h.cfg.WorkflowStore
	}
	return &conversation.Service{
		TaskService: &task.Service{
			Agents:         h.cfg.AgentStore,
			Tasks:          h.cfg.TaskStore,
			TaskRuns:       h.cfg.TaskRunStore,
			QuotaChecker:   quotaChecker,
			WorkflowSteps:  workflowSteps,
			TitleGenerator: h.cfg.TitleGenerator,
		},
		ConversationStore: h.cfg.ConversationStore,
		MessageStore:      h.cfg.ConversationMessageStore,
		LLMClient:         h.cfg.ConversationLLMClient,
		TitleGenerator:    h.cfg.TitleGenerator,
		AgentStore:        h.cfg.AgentStore,
	}
}
