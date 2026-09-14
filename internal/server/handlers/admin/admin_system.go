package admin

import (
	"context"
	"net/http"
	"time"

	"github.com/icloudbb/buildmax/internal/server/httputil"
)

// systemProbeTimeout bounds the whole status response. It mirrors the readiness
// endpoint's own bound for the same reason: a status page that can hang is
// worse than one that reports a dependency as failed.
const systemProbeTimeout = 3 * time.Second

// DependencyProbe is one dependency the deployment needs, as the admin API
// reports it. It is the same shape the readiness endpoint uses; the server
// converts its checks into these so that handlers do not import the package
// that imports them.
type DependencyProbe struct {
	// Name must be safe to show: "database", not a DSN.
	Name  string
	Probe func(ctx context.Context) error
}

// DeploymentInfo are the facts about a deployment that do not change while it
// runs. Bootstrap knows them; the handler only reports them.
type DeploymentInfo struct {
	// Version is the running binary's version string. It arrives from
	// bootstrap rather than being read here: internal/server must not import
	// internal/config, and a version the handler resolved itself would be a
	// second answer to a question the process already has one for.
	Version string
	// WorkerRunMode is "k8s_job" or the local-process path.
	WorkerRunMode string
	// WorkerLLMTransport is "direct" or "buildmax".
	WorkerLLMTransport string
	AllowSignup        bool
	// SandboxSurface is the execution boundary worker runs resolve to. Empty
	// means this deployment did not report one, which is every deployment
	// today — not that runs are unconfined. See deploymentInfoFor in
	// internal/bootstrap for why the server does not answer for the worker.
	SandboxSurface string
	// DefaultModel is the catalog model a caller gets when it names none.
	// Empty means the first enabled model in the catalog, so the admin view
	// reports what was configured rather than what it resolves to.
	DefaultModel string
}

// AdminSystemResponse is what an operator opens first: is this deployment all
// right, and is it the version they think it is.
type AdminSystemResponse struct {
	Version string `json:"version"`
	// SchemaMigrations are the steps applied beyond the row structs'
	// additive DDL. It is not a schema version — see the store method.
	SchemaMigrations []adminSchemaMigration `json:"schema_migrations"`
	Dependencies     []adminDependency      `json:"dependencies"`
	// Ready mirrors what /readyz would answer right now.
	Ready              bool           `json:"ready"`
	WorkerRunMode      string         `json:"worker_run_mode,omitempty"`
	WorkerLLMTransport string         `json:"worker_llm_transport,omitempty"`
	SandboxSurface     string         `json:"sandbox_surface,omitempty"`
	AllowSignup        bool           `json:"allow_signup"`
	TaskRuns           map[string]int `json:"task_runs"`
	SystemAdmins       int            `json:"system_admins"`
	ServerTime         time.Time      `json:"server_time"`
	// OIDC is the live SSO provider health. Nil when SSO is not configured. It
	// is separate from Dependencies because a degraded IdP does not make the
	// deployment not-ready — the fetch is retryable.
	OIDC *adminOIDCStatus `json:"oidc,omitempty"`
}

// adminOIDCStatus is what an operator reads to tell "SSO is off" from "SSO is on
// but the IdP is unreachable right now". It carries no secret: the last error is
// a go-oidc message, and issuer/client live in the redacted config view.
type adminOIDCStatus struct {
	Configured  bool       `json:"configured"`
	Available   bool       `json:"available"`
	LastRefresh *time.Time `json:"last_refresh,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
}

type adminSchemaMigration struct {
	ID        string    `json:"id"`
	AppliedAt time.Time `json:"applied_at"`
}

type adminDependency struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

// adminSystemHandler serves GET /api/admin/system.
//
// Everything here is either a count, a name, or a status. A failed dependency
// is named and not explained, exactly as /readyz does it: connection errors
// carry DSNs, endpoints, and bucket names, and the reason belongs in the server
// log where an operator already has to be.
func (h *Handler) adminSystemHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.guard().SystemAdmin(w, r); !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), systemProbeTimeout)
	defer cancel()

	out := AdminSystemResponse{
		Version:            h.cfg.Deployment.Version,
		SchemaMigrations:   []adminSchemaMigration{},
		Dependencies:       []adminDependency{},
		Ready:              true,
		WorkerRunMode:      h.cfg.Deployment.WorkerRunMode,
		WorkerLLMTransport: h.cfg.Deployment.WorkerLLMTransport,
		SandboxSurface:     h.cfg.Deployment.SandboxSurface,
		AllowSignup:        h.cfg.Deployment.AllowSignup,
		TaskRuns:           map[string]int{},
		ServerTime:         time.Now().UTC(),
	}

	for _, dep := range h.cfg.DependencyProbes {
		if dep.Probe == nil {
			continue
		}
		status := "ok"
		if err := dep.Probe(ctx); err != nil {
			status = "failed"
			out.Ready = false
		}
		out.Dependencies = append(out.Dependencies, adminDependency{Name: dep.Name, Status: status})
	}

	// SSO health is reported apart from the readiness dependencies: a momentarily
	// unreachable IdP is retryable and must not make the deployment not-ready.
	if h.cfg.OIDCStatus != nil {
		available, lastRefresh, lastErr := h.cfg.OIDCStatus()
		st := &adminOIDCStatus{Configured: true, Available: available, LastError: lastErr}
		if !lastRefresh.IsZero() {
			st.LastRefresh = &lastRefresh
		}
		out.OIDC = st
	}

	// Each remaining read is best-effort. A status page that returns 500
	// because one of its five questions could not be answered tells an operator
	// nothing during exactly the outage they opened it for.
	if h.cfg.Schema != nil {
		if migrations, err := h.cfg.Schema.AppliedMigrations(ctx); err == nil {
			for _, m := range migrations {
				out.SchemaMigrations = append(out.SchemaMigrations, adminSchemaMigration{ID: m.ID, AppliedAt: m.AppliedAt})
			}
		}
	}
	if h.cfg.TaskRuns != nil {
		if counts, err := h.cfg.TaskRuns.CountTaskRunsByStatus(ctx); err == nil {
			out.TaskRuns = counts
		}
	}
	if h.cfg.Grants != nil {
		if n, err := h.cfg.Grants.CountActiveSystemGrants(ctx, systemRoleAdmin()); err == nil {
			out.SystemAdmins = n
		}
	}
	httputil.WriteJSON(w, http.StatusOK, out)
}

// adminConfigHandler serves GET /api/admin/config.
//
// Read-only, and it will stay read-only until there is somewhere to write to.
// server.yaml is read at process start, so a write through this API would
// change one replica's view of the world, or none. Configuration stays
// source-controlled — see docs/design/system-administration.md section 7.2.
func (h *Handler) adminConfigHandler(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.guard().SystemAdmin(w, r); !ok {
		return
	}
	if h.cfg.RedactedConfig == nil {
		httputil.WriteJSONError(w, http.StatusServiceUnavailable, "configuration reporting not configured")
		return
	}
	httputil.WriteJSON(w, http.StatusOK, h.cfg.RedactedConfig)
}
