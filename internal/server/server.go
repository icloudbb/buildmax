package server

import (
	"context"
	"crypto/tls"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/icloudbb/buildmax/internal/server/handlers/admin"
	artifactsvc "github.com/icloudbb/buildmax/internal/service/artifact"
	"log/slog"
	"net/http"
	"sync"
	"time"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	coreartifact "github.com/icloudbb/buildmax/internal/core/artifact"
	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	"github.com/icloudbb/buildmax/internal/core/llm"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
	coreschedule "github.com/icloudbb/buildmax/internal/core/schedule"
	coreschema "github.com/icloudbb/buildmax/internal/core/schema"
	coresecret "github.com/icloudbb/buildmax/internal/core/secret"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	blob "github.com/icloudbb/buildmax/internal/infra/objectstore"
	"github.com/icloudbb/buildmax/internal/infra/workerclient"
	"github.com/icloudbb/buildmax/internal/server/handlers"
	workerroutes "github.com/icloudbb/buildmax/internal/server/handlers/worker"
	"github.com/icloudbb/buildmax/internal/server/httputil"
	"github.com/icloudbb/buildmax/internal/server/turnqueue"
	wsconn "github.com/icloudbb/buildmax/internal/server/websocket"
	"github.com/icloudbb/buildmax/internal/service/audit"
	"github.com/icloudbb/buildmax/internal/service/conversation"
	convchannel "github.com/icloudbb/buildmax/internal/service/conversation/channel"
	"github.com/icloudbb/buildmax/internal/service/issue"
	"github.com/icloudbb/buildmax/internal/service/llmgateway"
	pluginsvc "github.com/icloudbb/buildmax/internal/service/plugin"
	"github.com/icloudbb/buildmax/internal/service/quota"
	secretsvc "github.com/icloudbb/buildmax/internal/service/secret"
	"github.com/icloudbb/buildmax/internal/service/task"
	"github.com/icloudbb/buildmax/internal/service/workflow"
	workspacesvc "github.com/icloudbb/buildmax/internal/service/workspace"
)

//go:embed static/openapi.json static/swagger.html
var staticFS embed.FS

// readHeaderTimeout and idleTimeout bound connections that are doing nothing
// useful. Both matter to shutdown — see the http.Server literal in New.
const (
	readHeaderTimeout = 10 * time.Second
	idleTimeout       = 120 * time.Second
)

// AuthConfig holds auth and CORS settings plus optional quota for signup and create-chat/run.
type AuthConfig struct {
	JWTSecret   string // Required for login when UserStore is set
	AllowSignup bool   // Open POST /api/auth/otp to self-registration; closed by default
	CORSOrigin  string // If set, enable CORS with this origin (e.g. "http://localhost:5173")
	// PublicBaseURL is the externally reachable origin at which people open
	// BuildMax. Artifact share links are rendered against it; empty refuses
	// share creation rather than emitting an unreachable link.
	PublicBaseURL    string
	QuotaService     *quota.Service // Optional; when set, create chat/run enforce quota and return 429 when exceeded
	DefaultQuotaTier string         // Default quota tier for new users (e.g. signup); used when calling CreateUser
	// Token lifetimes; zero means the default in internal/core/identity.
	AccessTokenTTL       time.Duration
	RefreshTokenTTL      time.Duration
	RefreshRotationGrace time.Duration
}

// StoresConfig holds entity store interfaces used by handlers.
type StoresConfig struct {
	UserStore           coreidentity.UserStore
	LoginCodeStore      coreidentity.LoginCodeStore
	PasswordStore       coreidentity.PasswordStore
	RefreshTokenStore   coreidentity.RefreshTokenStore
	SpaceStore          corespace.Store
	WorkflowStore       coreworkflow.Store
	AgentStore          agentdef.Store
	IssueStore          coreissue.Store
	IssueCommentStore   coreissue.CommentStore
	TaskStore           coretask.Store
	TaskRunStore        coretask.RunStore
	ScheduleStore       coreschedule.Store
	LLMCallStore        coregw.CallStore
	UserWebhookKeyStore coreidentity.UserWebhookKeyStore
	AuditStore          coreaudit.Store
	SystemGrantStore    coreidentity.SystemGrantStore
	SchemaStore         coreschema.Store
	LLMModelStore       coregw.ModelStore
	// ArtifactStore records durable files. Nil leaves the artifact routes
	// answering 503, which is what a deployment with no database has.
	ArtifactStore coreartifact.Store
	// ArtifactShareStore persists public share links. Nil leaves sharing off
	// while artifacts otherwise work.
	ArtifactShareStore coreartifact.ShareStore
	// SecretStore is the Space Secret store. Nil disables the secret feature.
	SecretStore coresecret.Store
	// WorkspaceCheckpointStore reads a run's base checkpoint and records its
	// restore outcome for the worker API. Nil disables the base and restore
	// routes.
	WorkspaceCheckpointStore workerroutes.WorkspaceRunStore
}

// ServicesConfig holds application services the handlers reach through rather
// than a store directly.
type ServicesConfig struct {
	// Plugin publishes Marketplace releases and manages catalog entries. Nil
	// is a deployment with no Marketplace, which those routes report rather
	// than serving an empty catalog.
	Plugin *pluginsvc.Service
	// Secret backs the Space Secret management routes. Nil when no KEK file is
	// configured; those routes then report the feature off.
	Secret *secretsvc.Service
	// WorkspaceCheckpoints finalizes a seed a worker captured and uploaded. Nil
	// disables the worker checkpoint route, which is what a deployment with no
	// checkpoint storage has.
	WorkspaceCheckpoints *workspacesvc.Service
}

// StorageConfig holds blob storage and workspace paths.
type StorageConfig struct {
	PersistStorage blob.PersistStorage
	// ArtifactStorage holds artifact content.
	ArtifactStorage artifactsvc.ContentStore
	// MaxArtifactBytes caps one artifact. Zero uses the service default.
	MaxArtifactBytes int64
	// ArtifactShareTTL bounds a public share link's lifetime. Zero uses the
	// service default.
	ArtifactShareTTL time.Duration
	WorkspacesDir    string // Overrides config.WorkspacesDir() for workspace file operations
}

// WorkerConfig holds what a worker is told about models for its run. Worker
// authentication is not here: it is the run token the scheduler mints, verified
// with the deployment's JWT secret.
type WorkerConfig struct {
	// LLM tells a worker how to reach a model. Nil means direct.
	LLM *workerclient.TaskRunLLM
}

// ConversationConfig holds Tier 1 conversation stores and LLM wiring.
type ConversationConfig struct {
	TitleGenerator           llm.TitleGenerator
	ConversationStore        coreconv.Store
	ConversationMessageStore coreconv.MessageStore
	ConversationLLMClient    llm.LLMClient
	// LLMGateway serves managed inference to authenticated clients. Nil leaves
	// the /llm routes answering 503.
	LLMGateway *llmgateway.Service
}

// WebhookConfig holds webhook handler options (message path and optional user ID for created runs).
type WebhookConfig struct {
	MessagePath string // JSON path for message in body (default "message")
	UserID      string // CreatedBy for webhook runs (default "webhook")
}

// Config holds server configuration. Grouped fields document what is required for auth, storage, worker, and conversation.
type Config struct {
	Addr string // Public listen address (e.g. ":5678")
	// WorkerAddr is the internal worker-control listener address (e.g.
	// "127.0.0.1:5679"). Empty leaves the worker listener off, which is what a
	// test that only builds the public handler wants; the server binary always
	// sets it. The worker routes are registered on their own mux regardless, so
	// WorkerHandler is testable without opening a second socket. See
	// docs/design/worker-api-network-boundary.md.
	WorkerAddr string
	// WorkerTLS is the worker listener's TLS configuration. Nil serves plain
	// HTTP, which is a development-only mode; production sets the server
	// certificate and, for native mTLS, the client CA. The public listener has
	// no TLS field because TLS terminates at the Ingress in front of it.
	WorkerTLS *tls.Config
	Auth      AuthConfig
	Stores    StoresConfig
	Services  ServicesConfig
	Storage   StorageConfig
	Worker    WorkerConfig
	Conv      ConversationConfig
	Webhook   WebhookConfig
	// Audit records sensitive actions. Nil discards them.
	Audit *audit.Recorder
	// Deployment describes this deployment for the admin system status.
	Deployment admin.DeploymentInfo
	// Version is the application version stamped into the served OpenAPI
	// info.version. Empty leaves the document's placeholder in place. Bootstrap
	// sets it from the single build-version source. See
	// the route conventions in docs/contribute/architecture/server.md
	Version string
	// RedactedConfig is the operator-facing view of server.yaml. Nil means the
	// admin configuration route answers 503.
	RedactedConfig any
	// Readiness lists the dependency probes GET /readyz runs. Empty means the
	// endpoint reports ready without verifying anything, and says so by
	// returning an empty check list.
	Readiness []ReadinessCheck
	// Hub, EventBus, and TurnLocker are the cross-replica coordination backends.
	// All nil is the single-instance default: an in-memory stream hub, local
	// connection-event fan-out, and in-process turn serialization. They are set
	// together when coordination.mode is redis. See
	// docs/design/server-coordination.md.
	Hub        wsconn.StreamHub
	EventBus   handlers.EventBus
	TurnLocker turnqueue.Locker
}

// Server wraps the HTTP server and runs it.
type Server struct {
	srv *http.Server
	// workerSrv serves the worker control API on its own listener. Nil when
	// cfg.WorkerAddr is empty. workerHandler is kept separately so tests can
	// exercise the worker route set without a socket.
	workerSrv     *http.Server
	workerHandler http.Handler
	cfg           Config
	// handlers is kept so the server can run and stop the background work the
	// API surface owns — see StartBackground.
	handlers *handlers.Handler
	// drain is closed once this server is going away. It is a channel rather
	// than a flag because watcher streams block on it: a Portal tab following a
	// run has to be told, not polled. See docs/design/graceful-shutdown.md §5.
	drain     chan struct{}
	drainOnce sync.Once
	// openAPISpec is the served OpenAPI document, built once with info.version
	// stamped from cfg.Version.
	openAPISpec []byte
}

// New builds the server. The public listener serves healthz, readyz, openapi,
// swagger, and every non-worker API handler; a separate worker listener serves
// only /api/worker/*. The two never share a mux, so the public socket cannot
// dispatch a worker route even under a broad Ingress rule. See
// docs/design/worker-api-network-boundary.md.
func New(cfg Config) *Server {
	s := &Server{cfg: cfg, drain: make(chan struct{})}

	spec, err := buildOpenAPISpec(cfg.Version)
	if err != nil {
		// Fall back to the embedded document rather than fail to start: the spec
		// is documentation, not a request-serving dependency.
		slog.Error("openapi version stamp failed; serving unstamped spec", "err", err)
		spec, _ = staticFS.ReadFile("static/openapi.json")
	}
	s.openAPISpec = spec

	s.handlers = handlers.NewHandler(buildHandlersConfig(cfg, s.drain))

	publicMux := http.NewServeMux()
	publicMux.HandleFunc("GET /healthz", healthzHandler)
	publicMux.HandleFunc("GET /readyz", s.readyzHandler)
	publicMux.HandleFunc("GET /openapi.json", s.openAPIHandler)
	publicMux.HandleFunc("GET /swagger/", swaggerUIHandler)
	publicMux.HandleFunc("GET /swagger/index.html", swaggerUIHandler)
	publicMux.HandleFunc("GET /swagger", swaggerUIHandler)
	s.handlers.RegisterPublic(publicMux)

	// CORS is a browser control and belongs only to the public listener: the
	// worker is not a browser, and a worker route must not become reachable by a
	// cross-origin fallback path. Request logging wraps both.
	publicHandler := http.Handler(publicMux)
	if cfg.Auth.CORSOrigin != "" {
		publicHandler = corsMiddleware(publicHandler, cfg.Auth.CORSOrigin)
	}
	s.srv = newHTTPServer(cfg.Addr, requestLoggingMiddleware(publicHandler))

	workerMux := http.NewServeMux()
	s.handlers.RegisterWorker(workerMux)
	s.workerHandler = requestLoggingMiddleware(http.Handler(workerMux))
	if cfg.WorkerAddr != "" {
		s.workerSrv = newHTTPServer(cfg.WorkerAddr, s.workerHandler)
		// Nil in development (plain HTTP) and set in production. ListenAndServe
		// serves TLS whenever this is present.
		s.workerSrv.TLSConfig = cfg.WorkerTLS
	}
	return s
}

// newHTTPServer builds an http.Server with the shared timeout policy both
// listeners use.
func newHTTPServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:    addr,
		Handler: handler,
		// A connection that never completes its header is neither idle nor
		// active, so Shutdown neither closes it nor finishes waiting for it.
		// It is also the slowloris surface.
		ReadHeaderTimeout: readHeaderTimeout,
		// Bounds how many keep-alive connections are still open when draining
		// starts. Shutdown closes idle ones immediately, so fewer here is a
		// shorter drain.
		IdleTimeout: idleTimeout,
		// ReadTimeout and WriteTimeout stay unset on purpose. An artifact
		// upload is legitimately slow to read, and a write deadline would
		// truncate every SSE stream and every long managed model call.
	}
}

func buildHandlersConfig(cfg Config, drain <-chan struct{}) handlers.Config {
	msgPath := cfg.Webhook.MessagePath
	if msgPath == "" {
		msgPath = "message"
	}
	webhookUserID := cfg.Webhook.UserID
	if webhookUserID == "" {
		webhookUserID = convchannel.DefaultWebhookUserID
	}
	var webhookAdapter convchannel.Adapter
	var webhookEngine conversation.TurnEngine
	if cfg.Stores.UserWebhookKeyStore != nil {
		taskSvc := &task.Service{
			Agents:         cfg.Stores.AgentStore,
			Tasks:          cfg.Stores.TaskStore,
			TaskRuns:       cfg.Stores.TaskRunStore,
			QuotaChecker:   cfg.Auth.QuotaService,
			TitleGenerator: nil,
		}
		webhookAdapter = convchannel.NewWebhookAdapter(msgPath, webhookUserID)
		webhookEngine = &conversation.WebhookEngine{TaskService: taskSvc, Conversations: cfg.Conv.ConversationStore}
	}
	return handlers.Config{
		JWTSecret:                cfg.Auth.JWTSecret,
		AllowSignup:              cfg.Auth.AllowSignup,
		CORSOrigin:               cfg.Auth.CORSOrigin,
		AccessTokenTTL:           cfg.Auth.AccessTokenTTL,
		RefreshTokenTTL:          cfg.Auth.RefreshTokenTTL,
		RefreshRotationGrace:     cfg.Auth.RefreshRotationGrace,
		WorkerLLM:                cfg.Worker.LLM,
		UserStore:                cfg.Stores.UserStore,
		AuditStore:               cfg.Stores.AuditStore,
		SystemGrantStore:         cfg.Stores.SystemGrantStore,
		SchemaStore:              cfg.Stores.SchemaStore,
		LLMModelStore:            cfg.Stores.LLMModelStore,
		ArtifactStore:            cfg.Stores.ArtifactStore,
		SecretStore:              cfg.Stores.SecretStore,
		PluginService:            cfg.Services.Plugin,
		SecretService:            cfg.Services.Secret,
		WorkspaceCheckpoints:     cfg.Services.WorkspaceCheckpoints,
		WorkspaceCheckpointStore: cfg.Stores.WorkspaceCheckpointStore,
		Deployment:               cfg.Deployment,
		DependencyProbes:         dependencyProbes(cfg.Readiness),
		RedactedConfig:           cfg.RedactedConfig,
		Audit:                    cfg.Audit,
		LoginCodeStore:           cfg.Stores.LoginCodeStore,
		PasswordStore:            cfg.Stores.PasswordStore,
		RefreshTokenStore:        cfg.Stores.RefreshTokenStore,
		SpaceStore:               cfg.Stores.SpaceStore,
		WorkflowStore:            cfg.Stores.WorkflowStore,
		AgentStore:               cfg.Stores.AgentStore,
		IssueStore:               cfg.Stores.IssueStore,
		IssueCommentStore:        cfg.Stores.IssueCommentStore,
		TaskStore:                cfg.Stores.TaskStore,
		TaskRunStore:             cfg.Stores.TaskRunStore,
		ScheduleStore:            cfg.Stores.ScheduleStore,
		LLMCallStore:             cfg.Stores.LLMCallStore,
		UserWebhookKeyStore:      cfg.Stores.UserWebhookKeyStore,
		ConversationStore:        cfg.Conv.ConversationStore,
		ConversationMessageStore: cfg.Conv.ConversationMessageStore,
		PersistStorage:           cfg.Storage.PersistStorage,
		ArtifactStorage:          cfg.Storage.ArtifactStorage,
		ArtifactShareStore:       cfg.Stores.ArtifactShareStore,
		ArtifactPublicBaseURL:    cfg.Auth.PublicBaseURL,
		MaxArtifactBytes:         cfg.Storage.MaxArtifactBytes,
		ArtifactShareTTL:         cfg.Storage.ArtifactShareTTL,
		WorkspacesDir:            cfg.Storage.WorkspacesDir,
		DefaultQuotaTier:         cfg.Auth.DefaultQuotaTier,
		QuotaService:             cfg.Auth.QuotaService,
		TitleGenerator:           cfg.Conv.TitleGenerator,
		ConversationLLMClient:    cfg.Conv.ConversationLLMClient,
		LLMGateway:               cfg.Conv.LLMGateway,
		WebhookAdapter:           webhookAdapter,
		WebhookEngine:            webhookEngine,
		WebhookMessagePath:       msgPath,
		OnTaskRunTerminal:        buildOnTaskRunTerminal(cfg),
		Drain:                    drain,
		Hub:                      cfg.Hub,
		EventBus:                 cfg.EventBus,
		TurnLocker:               cfg.TurnLocker,
	}
}

// buildOnTaskRunTerminal composes what should happen when a worker run reaches
// a terminal status: advance the workflow it belongs to, and report it on the
// issue it belongs to.
//
// Each half is wired independently and each failure is logged rather than
// propagated. A deployment without workflows still reports runs on issues, and
// a comment store that is down does not stop a workflow from advancing.
func buildOnTaskRunTerminal(cfg Config) func(ctx context.Context, info coretask.RunTerminalInfo) {
	var workflowSvc *workflow.Service
	if cfg.Stores.WorkflowStore != nil {
		taskSvc := &task.Service{
			Agents:         cfg.Stores.AgentStore,
			Tasks:          cfg.Stores.TaskStore,
			TaskRuns:       cfg.Stores.TaskRunStore,
			QuotaChecker:   cfg.Auth.QuotaService,
			TitleGenerator: nil,
		}
		workflowSvc = &workflow.Service{
			Workflows:   cfg.Stores.WorkflowStore,
			Agents:      cfg.Stores.AgentStore,
			Issues:      cfg.Stores.IssueStore,
			TaskService: taskSvc,
			// Reconcile from the terminal callback reads the finished step's
			// TaskRun to fold it; without this reader it can never observe a
			// terminal step and every run strands in running.
			TaskRuns: cfg.Stores.TaskRunStore,
			Audit:    cfg.Audit,
		}
	}
	var runReporter *issue.RunReporter
	if cfg.Stores.IssueCommentStore != nil && cfg.Stores.TaskStore != nil {
		runReporter = &issue.RunReporter{
			Tasks:    cfg.Stores.TaskStore,
			Comments: cfg.Stores.IssueCommentStore,
		}
	}
	if workflowSvc == nil && runReporter == nil {
		return nil
	}
	return func(ctx context.Context, info coretask.RunTerminalInfo) {
		terminal := info
		if workflowSvc != nil {
			if err := workflowSvc.HandleTaskRunTerminal(ctx, terminal); err != nil && !errors.Is(err, workflow.ErrWorkflowRunNotFound) {
				slog.Warn("workflow terminal callback failed", "task_run_id", info.TaskRunID, "task_id", info.TaskID, "err", err)
			}
		}
		if runReporter != nil {
			if err := runReporter.ReportRunTerminal(ctx, terminal); err != nil {
				slog.Warn("issue run comment not written", "task_run_id", info.TaskRunID, "task_id", info.TaskID, "err", err)
			}
		}
	}
}

// Handler returns the public HTTP handler for use in tests.
func (s *Server) Handler() http.Handler {
	return s.srv.Handler
}

// WorkerHandler returns the worker-control HTTP handler for use in tests. It is
// built whether or not a worker socket is opened, so a test can prove the route
// boundary — a worker route answers here and 404s on Handler, and the reverse —
// without binding a second port.
func (s *Server) WorkerHandler() http.Handler {
	return s.workerHandler
}

// ListenAndServe serves until Shutdown is called, and reports nil for the
// orderly stop that causes.
//
// It does not install a signal handler. Stopping this process is a sequence
// that reaches further than the HTTP surface — see docs/design/graceful-shutdown.md
// — and the layer that assembles the scheduler and the background loops is the
// only one that can walk it in the right order.
func (s *Server) ListenAndServe() error {
	// Both listeners run in this one process. A bind failure on either is fatal
	// the same way a taken public port is: accepting user tasks while no worker
	// can report them is not a degraded mode, so the first listener to fail
	// returns and the caller walks the shutdown ladder. See
	// docs/design/worker-api-network-boundary.md §9.1.
	errs := make(chan error, 2)
	serve := func(name string, srv *http.Server) {
		// The certificate and key live in srv.TLSConfig, so ListenAndServeTLS is
		// called with empty paths. Only the worker listener ever carries TLS
		// here; the public listener terminates it at the Ingress.
		var err error
		if srv.TLSConfig != nil {
			err = srv.ListenAndServeTLS("", "")
		} else {
			err = srv.ListenAndServe()
		}
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			errs <- fmt.Errorf("%s listener: %w", name, err)
			return
		}
		errs <- nil
	}
	if s.workerSrv != nil {
		go serve("worker", s.workerSrv)
	}
	go serve("public", s.srv)
	return <-errs
}

// StartBackground launches the background work the API surface owns.
//
// Called by whoever runs the server rather than by New, so that building a
// handler — which a test does freely — never starts a goroutine.
func (s *Server) StartBackground() { s.handlers.StartBackground() }

// StopBackground stops that work and waits for it, bounded by ctx.
func (s *Server) StopBackground(ctx context.Context) { s.handlers.StopBackground(ctx) }

// Drain marks this server as going away: /readyz starts answering 503 so the
// load balancer stops sending it new work, and watcher streams return so they
// stop holding the drain open. It is idempotent and does not wait.
//
// Requests in flight are untouched. Ending them is Shutdown's job, one rung
// further down.
func (s *Server) Drain() {
	s.drainOnce.Do(func() {
		close(s.drain)
		s.handlers.BeginDrain()
		slog.Info("server draining")
	})
}

// Draining reports whether Drain has been called.
func (s *Server) Draining() bool {
	select {
	case <-s.drain:
		return true
	default:
		return false
	}
}

// Shutdown stops accepting connections and waits for the work in flight,
// bounded by ctx. It also drains, so a caller that stops the server without
// walking the whole ladder still takes it out of the load balancer first.
//
// Conversation turns are waited for first and explicitly: one reached over a
// WebSocket lives on a hijacked connection, which http.Server.Shutdown returns
// without waiting for.
func (s *Server) Shutdown(ctx context.Context) error {
	s.Drain()
	s.handlers.WaitTurns(ctx)
	// Public first, worker last. A worker reports its outcome over the worker
	// listener, so it outlives the public one; by the time the ladder reaches
	// here the scheduler has already stopped and its runs have reported, but
	// closing the worker listener last keeps the ordering the design states
	// rather than relying on that timing. See
	// docs/design/worker-api-network-boundary.md §9.2.
	err := s.srv.Shutdown(ctx)
	if s.workerSrv != nil {
		if werr := s.workerSrv.Shutdown(ctx); werr != nil && err == nil {
			err = werr
		}
	}
	return err
}

func serveStatic(w http.ResponseWriter, path, contentType string) {
	data, err := staticFS.ReadFile(path)
	if err != nil {
		slog.Error("static file read failed", "err", err, "path", path)
		httputil.WriteJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// buildOpenAPISpec returns the served OpenAPI document with info.version stamped
// from the build's application version, so the version has one source — the
// build — rather than a hand-maintained literal that drifts. The document's own
// info.version is a placeholder overridden here. An empty version leaves the
// placeholder in place. See the route conventions in docs/contribute/architecture/server.md
//
// It runs once in New and the bytes are served as-is, so parsing never happens
// on the request path.
func buildOpenAPISpec(version string) ([]byte, error) {
	raw, err := staticFS.ReadFile("static/openapi.json")
	if err != nil {
		return nil, err
	}
	if version == "" {
		return raw, nil
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	info := map[string]json.RawMessage{}
	if err := json.Unmarshal(doc["info"], &info); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(version)
	if err != nil {
		return nil, err
	}
	info["version"] = encoded
	if doc["info"], err = json.Marshal(info); err != nil {
		return nil, err
	}
	return json.Marshal(doc)
}

func (s *Server) openAPIHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(s.openAPISpec)
}

func swaggerUIHandler(w http.ResponseWriter, _ *http.Request) {
	serveStatic(w, "static/swagger.html", "text/html; charset=utf-8")
}

// dependencyProbes converts readiness checks into the shape the admin API
// reports. Both are a name and a probe; the conversion exists so that the
// handler package does not import this one, which imports it.
func dependencyProbes(checks []ReadinessCheck) []admin.DependencyProbe {
	out := make([]admin.DependencyProbe, 0, len(checks))
	for _, check := range checks {
		out = append(out, admin.DependencyProbe{Name: check.Name, Probe: check.Probe})
	}
	return out
}
