package bootstrap

import (
	"context"
	"fmt"
	"github.com/icloudbb/buildmax/internal/server/handlers/admin"
	artifactsvc "github.com/icloudbb/buildmax/internal/service/artifact"
	workspacesvc "github.com/icloudbb/buildmax/internal/service/workspace"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/core/eligibility"
	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
	coresecret "github.com/icloudbb/buildmax/internal/core/secret"
	infracoord "github.com/icloudbb/buildmax/internal/infra/coordination"
	"github.com/icloudbb/buildmax/internal/infra/db"
	"github.com/icloudbb/buildmax/internal/infra/k8s"
	blob "github.com/icloudbb/buildmax/internal/infra/objectstore"
	infraoidc "github.com/icloudbb/buildmax/internal/infra/oidc"
	infrasecret "github.com/icloudbb/buildmax/internal/infra/secret"
	"github.com/icloudbb/buildmax/internal/infra/workerclient"
	httpserver "github.com/icloudbb/buildmax/internal/server"
	"github.com/icloudbb/buildmax/internal/server/authtoken"
	servercoord "github.com/icloudbb/buildmax/internal/server/coordination"
	"github.com/icloudbb/buildmax/internal/server/scheduler"
	"github.com/icloudbb/buildmax/internal/service/audit"
	"github.com/icloudbb/buildmax/internal/service/llmgateway"
	pluginsvc "github.com/icloudbb/buildmax/internal/service/plugin"
	"github.com/icloudbb/buildmax/internal/service/quota"
	secretsvc "github.com/icloudbb/buildmax/internal/service/secret"
	tasksvc "github.com/icloudbb/buildmax/internal/service/task"
	workflowsvc "github.com/icloudbb/buildmax/internal/service/workflow"
)

// RunServer loads server.yaml, resolves the listen port (flag overrides config),
// opens the DB, builds blob storage, starts the scheduler, and runs the HTTP server.
// portOverride > 0 takes priority over the port in server.yaml.
func RunServer(ctx context.Context, portOverride int) error {
	sc, err := config.LoadServerConfig()
	if err != nil {
		return fmt.Errorf("server config: %w", err)
	}

	// JWT secret: server.yaml jwt_secret field, overridable by BUILDMAX_JWT_SECRET env var.
	jwtSecret := sc.JWTSecret
	if jwtSecret == "" {
		return fmt.Errorf("jwt_secret is required (set in server.yaml or %s env var)", config.EnvKeyBuildmaxJWTSecret)
	}

	slog.Info("login accepts a password or a single-use code; a code lets the account set its own password",
		"issue_code_with", "buildmax-server user login-code <email>")
	// Self-registration and a reachable server together mean anyone can create
	// an account. Say so at startup rather than in a document nobody opened.
	if sc.AllowSignup {
		slog.Warn("open signup is enabled — anyone who can reach this server can create an account",
			"config", "allow_signup")
	}

	port := sc.Port
	if portOverride > 0 {
		port = portOverride
	}
	if port == 0 {
		port = 5678
	}

	// Fail closed before anything binds: a worker listener that shares the
	// public port, or half a TLS keypair, is a boundary that is not there. See
	// docs/design/worker-api-network-boundary.md §9.1 and §10.
	if err := sc.ValidateListeners(fmt.Sprintf(":%d", port)); err != nil {
		return fmt.Errorf("listener configuration: %w", err)
	}

	// Fail closed on an unusable coordination mode before anything binds: an
	// unnamed backend or a redis mode with no address is a topology the operator
	// did not choose.
	if err := sc.Coordination.Validate(); err != nil {
		return fmt.Errorf("coordination configuration: %w", err)
	}

	// Fail closed on a login configuration that would refuse everyone or run an
	// unbounded just-in-time provisioning, rather than discovering it at the
	// first sign-in.
	if err := sc.ValidateAuth(); err != nil {
		return fmt.Errorf("authentication configuration: %w", err)
	}
	if sc.LocalLoginMode() == config.LocalLoginOff && !sc.OIDC.Enabled {
		slog.Warn("no login method is enabled — local_login is off and oidc is disabled; no one can sign in",
			"local_login", sc.LocalLoginMode())
	}

	workspacesDir, err := resolveWorkspacesDir(sc.WorkspacesDir)
	if err != nil {
		return err
	}

	store, err := openStore(ctx, sc.Database)
	if err != nil {
		return err
	}

	storage, err := buildBlobStorage(ctx, sc.Storage, workspacesDir)
	if err != nil {
		return err
	}

	serverConfig, err := buildHTTPServerConfig(port, jwtSecret, sc, workspacesDir, store, storage)
	if err != nil {
		return err
	}

	// Cross-replica coordination. Nil backend is the single-instance default;
	// coordination.mode redis makes streaming, connection events, and turn
	// serialization consistent across replicas. Construction fails closed: a
	// configured-but-unreachable Redis stops startup rather than serving with
	// process-local coordination under a multi-replica manifest.
	coordCtx, coordCancel := context.WithCancel(context.Background())
	defer coordCancel()
	coordBackend, err := buildCoordination(coordCtx, sc.Coordination)
	if err != nil {
		return err
	}
	if coordBackend != nil {
		defer func() { _ = coordBackend.Close() }()
		serverConfig.Hub = servercoord.NewStreamHub(coordCtx, coordBackend)
		serverConfig.EventBus = servercoord.NewEventBus(coordCtx, coordBackend)
		serverConfig.TurnLocker = servercoord.NewTurnLocker(coordBackend)
		slog.Info("coordination backend enabled: multi-replica streaming, events, and turn serialization are shared through redis",
			"address", sc.Coordination.Redis.Address)
	}

	// The budget is resolved before anything starts, because the scheduler's
	// runner needs the worker's share of it at construction: how long a worker
	// gets to report is part of how it is spawned.
	budget := httpserver.NewShutdownBudget(sc.ShutdownGrace)

	runner, err := buildWorkerRunner(sc.Worker, budget.Workers)
	if err != nil {
		return err
	}

	// One eligibility check behind every durable dispatch path: an account that
	// is not disabled and still a member of the run's Space. The HTTP guard
	// enforces the same two facts on human requests; this reaches the Schedule,
	// Workflow, and worker-dispatch paths that never pass through a handler. See
	// docs/proposals/personnel-deactivation-lifecycle.md §7.
	elig := eligibility.New(store, store)

	sched, err := scheduler.NewScheduler(store, runner, runTokenMinter(sc, jwtSecret))
	if err != nil {
		return fmt.Errorf("scheduler: %w", err)
	}
	// So that work queued by an account an administrator has disabled, or a
	// member a Space owner has removed, does not start after the change.
	sched.WithEligibility(elig).Start()

	cleaner := scheduler.NewCredentialCleaner(store, 0)
	cleaner.Start()

	reaper := scheduler.NewStaleRunReaper(store, sc.Worker.RunTimeout, 0)
	reaper.Start()

	// Stops work already under way once its initiator loses authority: the
	// admission gates refuse new work, but a run already handed to a worker is
	// only reachable by this durable backstop, which re-checks eligibility and
	// requests cancellation.
	eligibilityReconciler := scheduler.NewEligibilityReconciler(store, elig, 0)
	eligibilityReconciler.Start()

	// Nil unless the operator set a retention window, so a deployment that
	// never chose one keeps every event.
	retainer := scheduler.NewAuditRetainer(store, store, sc.Audit.RetentionDays, 0)
	retainer.Start()

	// Nil unless the operator set a trace retention window. Records each prune
	// into the same audit trail the retainer above manages, so a missing trace
	// is explained where an operator already looks.
	traceRetainer := scheduler.NewTraceRetainer(store, storage.persist, store, sc.Trace.RetentionDays, 0)
	traceRetainer.Start()

	// Unconditional where artifact storage exists: an artifact whose bytes are
	// never reclaimed is a leak, not a retained record, so there is no window
	// that switches this off. The grace period only delays it.
	artifacts := scheduler.NewArtifactRetainer(store, storage.artifact, store, sc.Storage.ArtifactPurgeAfterDays, 0)
	artifacts.Start()

	// Reclaims checkpoint payloads a worker uploaded but never committed a
	// pointer for. Nil-safe, so a deployment without checkpoint storage skips it.
	checkpoints := scheduler.NewCheckpointOrphanSweeper(storage.checkpoint, store, sc.Storage.CheckpointOrphanGraceDays, 0)
	checkpoints.Start()

	// Fires recurring schedules: on each due time it admits one Task through the
	// same Task service the API uses, tagged with a schedule trigger source. It
	// reuses the quota service already built for the HTTP surface so a firing is
	// metered exactly as a user-started run is.
	scheduleAdmitter := &tasksvc.Service{
		Agents:       store,
		Tasks:        store,
		TaskRuns:     store,
		QuotaChecker: serverConfig.Auth.QuotaService,
	}
	// The workflow application service both the schedule dispatcher (to fire a
	// workflow schedule) and the recovery loop (to reconcile stranded runs) use. It
	// reuses the Task service above, so a step it dispatches is admitted and metered
	// exactly like any other run.
	workflowSvc := &workflowsvc.Service{
		Workflows:   store,
		Agents:      store,
		Issues:      store,
		TaskService: scheduleAdmitter,
		TaskRuns:    store,
		Artifacts:   store,
	}
	dispatcher, err := scheduler.NewScheduleDispatcher(store, scheduleAdmitter, 0)
	if err != nil {
		return fmt.Errorf("schedule dispatcher: %w", err)
	}
	// WithEligibility so a schedule whose creator an administrator disabled, or an
	// owner removed from the Space, pauses rather than starting work that would only
	// fail. WithWorkflows so a workflow schedule fires a run through the same service
	// the API and recovery loop use.
	dispatcher.WithEligibility(elig).WithWorkflows(workflowSvc).Start()

	// Recovers Workflow runs stranded by a lost terminal callback or a Server
	// restart: each sweep reconciles due runs from durable state. Every replica
	// runs it; the reconciliation lease, not process-local election, keeps two from
	// advancing one run at once.
	recovery, err := scheduler.NewWorkflowRecoveryLoop(workflowSvc, 0)
	if err != nil {
		return fmt.Errorf("workflow recovery loop: %w", err)
	}
	recovery.Start()

	s := httpserver.New(serverConfig)
	s.StartBackground()
	slog.Info("server starting",
		"addr", serverConfig.Addr,
		"worker_addr", serverConfig.WorkerAddr,
		"version", config.Version,
		"commit", config.Commit,
	)

	signalCtx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	serveErr := make(chan error, 1)
	go func() { serveErr <- s.ListenAndServe() }()

	select {
	case err := <-serveErr:
		// The listener failed before any signal — a taken port, a bad address.
		// Nothing has started serving, so there is nothing to drain.
		shutdownServer(context.Background(), targetsFor(s, sched, dispatcher, cleaner, reaper, eligibilityReconciler, retainer, traceRetainer, artifacts, checkpoints, recovery), budget)
		return err
	case <-signalCtx.Done():
	}

	// Stop listening for signals before the ladder starts: a second Ctrl-C
	// should abort a shutdown that is taking too long, not be swallowed by the
	// handler that is already running one.
	stopSignals()
	slog.Info("shutdown requested", "grace", sc.ShutdownGrace)
	shutdownServer(ctx, targetsFor(s, sched, dispatcher, cleaner, reaper, eligibilityReconciler, retainer, traceRetainer, artifacts, checkpoints, recovery), budget)

	slog.Info("server stopped")
	return <-serveErr
}

// serverLifecycle is the shutdown surface of the HTTP server. An interface
// because the order below is the whole point of this code, and a test that
// cannot observe the order cannot defend it.
type serverLifecycle interface {
	Drain()
	Shutdown(ctx context.Context) error
	StopBackground(ctx context.Context)
}

// namedStop is a component's stop and the name it is reported under when it
// overruns its share of the budget. The context is the budget: a stop that can
// observe it stops early rather than being abandoned.
type namedStop struct {
	name string
	stop func(context.Context)
}

// shutdownTargets is everything RunServer started, in one value so the ladder
// below reads as a sequence rather than as argument plumbing.
type shutdownTargets struct {
	server serverLifecycle
	// scheduler stops before the HTTP server, not after: what it dispatches
	// reports back over the API.
	scheduler namedStop
	// loops are the sweeps that need nothing from anyone, so they stop last.
	loops []namedStop
}

// targetsFor names what RunServer started in the order the ladder stops it.
func targetsFor(s *httpserver.Server, sched *scheduler.Scheduler, dispatcher *scheduler.ScheduleDispatcher, cleaner *scheduler.CredentialCleaner, reaper *scheduler.StaleRunReaper, eligibilityReconciler *scheduler.EligibilityReconciler, retainer *scheduler.AuditRetainer, traceRetainer *scheduler.TraceRetainer, artifacts *scheduler.ArtifactRetainer, checkpoints *scheduler.CheckpointOrphanSweeper, recovery *scheduler.WorkflowRecoveryLoop) shutdownTargets {
	return shutdownTargets{
		server:    s,
		scheduler: namedStop{name: "scheduler", stop: func(ctx context.Context) { sched.Stop(ctx) }},
		loops: []namedStop{
			// Stopped before the sweeps: it produces work, so quieting it first
			// keeps a shutdown from creating Tasks nothing will dispatch until the
			// next start.
			{name: "schedule dispatcher", stop: ignoringContext(dispatcher.Stop)},
			// Stopped early with the dispatcher: it too dispatches Workflow steps,
			// so it is quieted before the sweeps rather than left advancing runs
			// into a draining server.
			{name: "workflow recovery", stop: ignoringContext(recovery.Stop)},
			{name: "checkpoint orphan sweeper", stop: ignoringContext(checkpoints.Stop)},
			{name: "audit retainer", stop: ignoringContext(retainer.Stop)},
			{name: "trace retainer", stop: ignoringContext(traceRetainer.Stop)},
			{name: "artifact retainer", stop: ignoringContext(artifacts.Stop)},
			{name: "stale run reaper", stop: ignoringContext(reaper.Stop)},
			{name: "eligibility reconciler", stop: ignoringContext(eligibilityReconciler.Stop)},
			{name: "credential cleaner", stop: ignoringContext(cleaner.Stop)},
		},
	}
}

// ignoringContext adapts a stop that ends its own loop promptly and has nothing
// to shorten. stopWithin still bounds the wait.
func ignoringContext(stop func()) func(context.Context) {
	return func(context.Context) { stop() }
}

// shutdownServer walks the shutdown ladder from docs/design/graceful-shutdown.md §3.
//
// The order is not stylistic. A worker reports its outcome over this server's
// own HTTP API, so the listener has to outlive the runs — which is why the
// scheduler stops above the HTTP shutdown rather than in a defer below it.
// Every rung is bounded: a stop that hangs is worse than one that loses a
// little work.
func shutdownServer(ctx context.Context, t shutdownTargets, budget httpserver.ShutdownBudget) {
	// Rung 1: out of the load balancer, and watcher streams told to go
	// elsewhere. Immediate, and everything below is quieter for it.
	t.server.Drain()

	// Rungs 2-3: no new run is claimed, and the runs already dispatched are
	// asked to stop and given their window to report — while the API they
	// report to is still listening. That is what the whole order is for.
	stopWithin(ctx, budget.Workers, t.scheduler.name, t.scheduler.stop)

	// Rungs 4-6: streams have already been told (rung 1) and get their moment
	// to return before ordinary requests are drained and the listener closes.
	sleepWithin(ctx, budget.Streams)
	requestCtx, cancelRequests := context.WithTimeout(ctx, budget.Requests)
	defer cancelRequests()
	if err := t.server.Shutdown(requestCtx); err != nil {
		slog.Warn("some requests did not finish before shutdown", "err", err)
	}

	// Rung 7: last, because everything above could still have enqueued work
	// here — a run reported during rung 3 fires terminal callbacks.
	backgroundCtx, cancelBackground := context.WithTimeout(ctx, budget.Background)
	defer cancelBackground()
	t.server.StopBackground(backgroundCtx)
	for _, loop := range t.loops {
		stopWithin(ctx, budget.Background, loop.name, loop.stop)
	}
}

// stopWithin runs stop, which blocks until its loop has finished, and gives up
// waiting after limit. Giving up leaks the goroutine, which is acceptable
// exactly here: the process is about to exit anyway, and the alternative is a
// shutdown that never completes.
func stopWithin(ctx context.Context, limit time.Duration, name string, stop func(context.Context)) {
	stopCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	done := make(chan struct{})
	go func() {
		stop(stopCtx)
		close(done)
	}()
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		slog.Warn("component did not stop within its shutdown budget", "component", name, "budget", limit)
	case <-ctx.Done():
		slog.Warn("shutdown abandoned while stopping a component", "component", name)
	}
}

// sleepWithin waits out a phase that has nothing to wait on — the moment
// watcher streams need to notice the drain and return.
func sleepWithin(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
	}
}

func resolveWorkspacesDir(fromConfig string) (string, error) {
	if fromConfig == "" {
		return "", fmt.Errorf("workspaces_dir is required in server.yaml")
	}
	abs, err := filepath.Abs(fromConfig)
	if err != nil {
		return fromConfig, nil
	}
	return abs, nil
}

// buildCoordination constructs the Redis coordination backend when it is
// selected, and returns a nil backend for the single-instance default. A
// configured redis backend that cannot be reached returns an error so the caller
// fails closed. See docs/design/server-coordination.md §3.
func buildCoordination(ctx context.Context, cc config.ServerCoordinationConfig) (*infracoord.Backend, error) {
	if !cc.RedisEnabled() {
		return nil, nil
	}
	b, err := infracoord.New(ctx, infracoord.Options{
		Address:  cc.Redis.Address,
		Username: cc.Redis.Username,
		Password: cc.Redis.Password,
		DB:       cc.Redis.DB,
		TLS:      cc.Redis.TLS,
	})
	if err != nil {
		return nil, fmt.Errorf("coordination backend: %w", err)
	}
	return b, nil
}

func openStore(ctx context.Context, db_ config.ServerDBConfig) (*db.Store, error) {
	st, err := db.New(ctx, db_.DSN())
	if err != nil {
		return nil, fmt.Errorf("database: %w", err)
	}
	return st, nil
}

// blobStorage is what one deployment stores and where.
//
// The three are separate key spaces rather than one bucket with three names: a
// space's mutable home, the durable artifacts the space keeps, and plugin
// packages.
type blobStorage struct {
	persist  blob.PersistStorage
	artifact artifactsvc.ContentStore
	packages pluginsvc.PackageStore
	// checkpoint is the workspace checkpoint payload store: the finalizer reads it
	// to verify a worker's uploaded bytes and derive their key, and the orphan
	// sweep reads and prunes it. One store, two views.
	checkpoint blob.CheckpointStore
	// packageKeyPrefix scopes package keys inside whichever backend holds them.
	packageKeyPrefix string
}

func buildBlobStorage(ctx context.Context, sc config.ServerStorageConfig, workspacesDir string) (blobStorage, error) {
	wsCfg := toWorkspaceStorageConfig(sc)
	s3Client, err := buildOptionalS3Client(ctx, wsCfg)
	if err != nil {
		return blobStorage{}, err
	}
	persistRoot := func(spaceID string) string {
		return config.PersistentWorkspaceDir(workspacesDir, spaceID)
	}
	runGlobalDir := func(spaceID, taskID, taskRunID string) string {
		return config.RunGlobalDir(workspacesDir, spaceID, taskID, taskRunID)
	}
	persistStorage, err := BuildPersistStorage(wsCfg, persistRoot, runGlobalDir, s3Client)
	if err != nil {
		return blobStorage{}, fmt.Errorf("persist storage: %w", err)
	}
	// Under "spaces" so it cannot collide with a space's home directory.
	artifactRoot := func(spaceID, artifactID string) string {
		return filepath.Join(workspacesDir, "spaces", spaceID, "artifacts", artifactID)
	}
	artifactStorage, err := BuildArtifactStorage(wsCfg, artifactRoot, s3Client)
	if err != nil {
		return blobStorage{}, fmt.Errorf("artifact storage: %w", err)
	}
	// A single content-addressed tree for all spaces; the space id is inside the
	// key. Under "checkpoints" so it cannot collide with a space's home or the
	// artifacts tree.
	checkpointStore, err := BuildCheckpointStore(wsCfg, filepath.Join(workspacesDir, "checkpoints"), s3Client)
	if err != nil {
		return blobStorage{}, fmt.Errorf("checkpoint storage: %w", err)
	}
	packages, packagePrefix := BuildPluginPackageStorage(wsCfg, workspacesDir, s3Client)
	return blobStorage{
		persist:          persistStorage,
		artifact:         artifactStorage,
		checkpoint:       checkpointStore,
		packages:         packages,
		packageKeyPrefix: packagePrefix,
	}, nil
}

func buildOptionalS3Client(ctx context.Context, wsCfg config.WorkspaceStorageConfig) (blob.S3Client, error) {
	if wsCfg.PersistProvider != config.ProviderMinIO && wsCfg.ArtifactProvider != config.ProviderMinIO {
		return nil, nil
	}
	s3Client, err := BuildS3Client(ctx, wsCfg)
	if err != nil {
		return nil, fmt.Errorf("S3 client: %w", err)
	}
	return s3Client, nil
}

func buildHTTPServerConfig(port int, jwtSecret string, sc config.ServerConfig, workspacesDir string, st *db.Store, storage blobStorage) (httpserver.Config, error) {
	pluginService := &pluginsvc.Service{
		Catalog:     st,
		Activations: st,
		Spaces:      st,
		Packages:    storage.packages,
		KeyPrefix:   storage.packageKeyPrefix,
		Audit:       audit.NewRecorder(st),
	}
	quotaService := &quota.Service{
		SpaceStore:  st,
		UsageReader: st,
		TierStore:   st,
		// The stock dimension. Runs and tokens come from UsageReader over a
		// window; bytes held have no window and are read straight from what
		// the space's live artifacts add up to.
		StorageReader: st,
		DefaultTier:   sc.DefaultQuotaTier,
		// So a space admin can see that the space approached or hit its limits
		// without anyone having to notice a 429 in a log.
		Audit: st,
	}
	// The Space Secret feature is on only when a KEK file is configured. When it
	// is, a KEK that will not load fails startup rather than leaving the values
	// silently unreadable -- see docs/design/space-secrets.md §9.1.
	var secretStore coresecret.Store
	var secretService *secretsvc.Service
	if sc.Secret.KEKFile != "" {
		kek, err := infrasecret.LoadKEKFile(sc.Secret.KEKFile)
		if err != nil {
			return httpserver.Config{}, fmt.Errorf("secret store: %w", err)
		}
		cipher := infrasecret.NewCipher(kek)
		secretStore = st
		secretService = &secretsvc.Service{Store: st, Sealer: cipher}
		// The same deployment KEK protects managed-model credentials at rest, so
		// the catalog can accept a model with a key. Without a KEK the catalog
		// refuses a credentialed model rather than storing the key in the clear.
		st.SetCredentialCipher(cipher)
	}
	// Built before the scheduler starts (buildHTTPServerConfig runs before
	// NewScheduler): a certificate that will not load fails startup rather than
	// scheduling Jobs that then cannot complete a handshake with this listener.
	workerTLS, err := buildWorkerListenerTLS(sc.WorkerAPI.TLS)
	if err != nil {
		return httpserver.Config{}, err
	}
	// SSO provider: built only when configured, and warmed off the startup path so
	// a momentarily unreachable IdP surfaces as degraded rather than blocking the
	// server from starting. Its health is reported to the admin system view apart
	// from the readiness probes, because an IdP fetch is retryable.
	var oidcStatus admin.OIDCStatusFunc
	var oidcProvider *infraoidc.Provider
	if sc.OIDC.Enabled {
		oidcProvider = infraoidc.New(infraoidc.Config{
			Issuer:       sc.OIDC.Issuer,
			ClientID:     sc.OIDC.ClientID,
			ClientSecret: sc.OIDC.ClientSecret,
		})
		provider := oidcProvider
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := provider.Warm(ctx); err != nil {
				slog.Warn("oidc discovery is not yet reachable; sign-in retries it on demand", "err", err)
			}
		}()
		oidcStatus = func() (bool, time.Time, string) {
			st := provider.Status()
			return st.Available, st.LastRefresh, st.LastError
		}
	}
	oidcSessionMaxAge := sc.OIDC.SessionMaxAge
	if oidcSessionMaxAge <= 0 {
		oidcSessionMaxAge = config.OIDCSessionMaxAgeDefault
	}
	cfg := httpserver.Config{
		Addr: fmt.Sprintf(":%d", port),
		// The worker control API is served on its own listener, off the public
		// HTTP surface. See docs/design/worker-api-network-boundary.md.
		WorkerAddr: sc.WorkerAPI.Listen,
		WorkerTLS:  workerTLS,
		Auth: httpserver.AuthConfig{
			JWTSecret:               jwtSecret,
			AllowSignup:             sc.AllowSignup,
			LocalLogin:              sc.LocalLoginMode(),
			OIDCEnabled:             sc.OIDC.Enabled,
			OIDCDisplayName:         sc.OIDC.DisplayName,
			OIDCProvider:            oidcProvider,
			OIDCSessionMaxAge:       oidcSessionMaxAge,
			OIDCProvisioning:        sc.OIDC.Provisioning,
			OIDCAllowedEmailDomains: sc.OIDC.AllowedEmailDomains,
			CORSOrigin:              sc.CORSOrigin,
			PublicBaseURL:           sc.PublicBaseURL,
			QuotaService:            quotaService,
			DefaultQuotaTier:        sc.DefaultQuotaTier,
			AccessTokenTTL:          sc.AccessTokenTTL,
			RefreshTokenTTL:         sc.RefreshTokenTTL,
			RefreshRotationGrace:    sc.RefreshRotationGrace,
			SessionAbsoluteTTL:      sc.SessionAbsoluteTTL,
		},
		Stores: httpserver.StoresConfig{
			UserStore:                st,
			LoginCodeStore:           st,
			PasswordStore:            st,
			RefreshTokenStore:        st,
			AuthSessionStore:         st,
			ExternalIdentityStore:    st,
			SpaceStore:               st,
			WorkflowStore:            st,
			AgentStore:               st,
			IssueStore:               st,
			IssueCommentStore:        st,
			TaskStore:                st,
			TaskRunStore:             st,
			ScheduleStore:            st,
			LLMCallStore:             st,
			UserWebhookKeyStore:      st,
			AuditStore:               st,
			SystemGrantStore:         st,
			SchemaStore:              st,
			LLMModelStore:            st,
			ArtifactStore:            st,
			ArtifactShareStore:       st,
			SecretStore:              secretStore,
			WorkspaceCheckpointStore: st,
		},
		Services: httpserver.ServicesConfig{
			Plugin:               pluginService,
			Secret:               secretService,
			WorkspaceCheckpoints: workspacesvc.New(st, storage.checkpoint),
		},
		Storage: httpserver.StorageConfig{
			PersistStorage:   storage.persist,
			ArtifactStorage:  storage.artifact,
			MaxArtifactBytes: int64(sc.Storage.MaxArtifactMB) << 20,
			ArtifactShareTTL: time.Duration(sc.Storage.ArtifactShareTTLHours) * time.Hour,
			WorkspacesDir:    workspacesDir,
		},
		Worker: httpserver.WorkerConfig{
			LLM: workerLLMDescriptor(sc.Worker.LLM),
		},
		Conv: httpserver.ConversationConfig{
			ConversationStore:        st,
			ConversationMessageStore: st,
		},
		Webhook: httpserver.WebhookConfig{
			MessagePath: sc.Webhook.MessagePath,
			UserID:      sc.Webhook.UserID,
		},
		Audit:     audit.NewRecorder(st),
		Readiness: readinessChecks(st, storage.persist),
		// What the admin system status reports about this deployment, and the
		// operator-facing view of server.yaml. The redaction whitelist lives in
		// internal/config, next to the struct it describes.
		Deployment:     deploymentInfoFor(sc),
		RedactedConfig: sc.Redacted(),
		OIDCStatus:     oidcStatus,
		// The served OpenAPI info.version comes from the one build-version source.
		Version: config.Version,
	}
	if err := wireLLM(&cfg, sc, st, quotaService); err != nil {
		return httpserver.Config{}, err
	}
	return cfg, nil
}

// wireLLM builds the model router, routes Tier 1 conversation through it in
// process, and exposes the managed gateway to authenticated clients.
//
// The server resolves a catalog target it owns for its own inference: it does
// not call its own HTTP listener and is not subject to space model policy.
// readinessProbeSpace is a space id no space can have, so the storage probe reads
// nothing real. It exercises the configured backend — reachability,
// credentials, and bucket or directory access — without depending on any
// tenant's data existing.
const readinessProbeSpace = "_readiness_probe"

// readinessChecks are the dependencies the server cannot serve traffic without.
//
// Names are what an unauthenticated caller sees, so they say which dependency
// without saying where it lives.
// deploymentInfoFor describes a deployment from its configuration.
//
// It lives in bootstrap for the same reason readinessChecks does: the server
// layer does not know what a run mode or a model transport is, and keeping that
// mapping here is what stops configuration detail leaking into it.
//
// SandboxSurface is still left empty, but no longer because there is nothing
// to report: internal/agentapp/taskrun now passes config.WorkerSandboxSurface().
// What a worker resolves to depends on the environment that worker runs in,
// and this runs in the server's. The two agree in every deployment shipped
// today, since both come from the same image, but agreeing by construction is
// not the same as observing it, and a security surface that infers a boundary
// it cannot see is how an operator gets told the wrong one. Reporting what the
// worker actually resolved belongs with the run that resolved it.
func deploymentInfoFor(sc config.ServerConfig) admin.DeploymentInfo {
	transport := sc.Worker.LLM.Transport
	if transport == "" {
		transport = config.TransportDirect
	}
	runMode := sc.Worker.RunMode
	if runMode == "" {
		runMode = "local_process"
	}
	return admin.DeploymentInfo{
		Version:            config.VersionString(),
		DefaultModel:       sc.LLM.DefaultModel,
		WorkerRunMode:      runMode,
		WorkerLLMTransport: transport,
		AllowSignup:        sc.AllowSignup,
	}
}

func readinessChecks(st *db.Store, persist blob.PersistStorage) []httpserver.ReadinessCheck {
	return []httpserver.ReadinessCheck{
		{
			Name:  "database",
			Probe: st.Ping,
		},
		{
			Name: "object_storage",
			Probe: func(ctx context.Context) error {
				_, err := persist.ListFiles(ctx, readinessProbeSpace)
				return err
			},
		},
	}
}

func wireLLM(cfg *httpserver.Config, sc config.ServerConfig, st *db.Store, quotaService *quota.Service) error {
	// A nil *db.Store put straight into an interface parameter is a non-nil
	// interface holding a nil pointer, so the absence of a store has to be
	// stated rather than passed along.
	var models coregw.ModelStore
	if st != nil {
		models = st
	}

	routing, err := buildLLMRouting(sc, models)
	if err != nil {
		return err
	}
	if routing == nil {
		return nil
	}
	// Startup, not first call: a name in server.yaml that resolves to nothing is
	// a configuration mistake, and it should stop the server rather than surface
	// later as a model outage.
	if err := validateConfiguredModels(context.Background(), routing, sc); err != nil {
		return err
	}

	// The gateway needs a ledger. Without a store there is nowhere to account
	// managed calls, so it stays off rather than serving unmetered inference.
	if st != nil {
		cfg.Conv.LLMGateway = &llmgateway.Service{
			Router: routing.Router,
			Ledger: st,
			Quota:  quotaService,
		}
	}

	if routing.Tier1TargetID == "" {
		return nil
	}
	// conversation.model_target may be a catalog ID or an operator-facing model
	// name. Resolving it here, at the wiring step that already reads the catalog,
	// keeps buildLLMRouting free of catalog lookups and lets an operator point
	// Tier 1 at a seeded model without discovering its runtime ID first.
	targetID, err := resolveTier1TargetID(context.Background(), routing.Router.Resolver.Catalog, routing.Tier1TargetID)
	if err != nil {
		return err
	}
	routed, err := routing.Router.ClientForTarget(context.Background(), targetID, llmgateway.BaselineCapabilities())
	if err != nil {
		return fmt.Errorf("conversation model %q: %w", targetID, err)
	}
	cfg.Conv.TitleGenerator = cllm.NewTitleGenerator(routed.Client)
	cfg.Conv.ConversationLLMClient = routed.Client
	return nil
}

// resolveTier1TargetID maps a conversation.model_target value to a catalog
// target ID. The field accepts either a catalog ID or an operator-facing model
// name: the ID is generated at `model add`/`kind seed` time and is not known
// when the config is authored, so a name is what an operator can write down. An
// ID is tried first, so a deployment that named one keeps resolving to exactly
// that row.
func resolveTier1TargetID(ctx context.Context, catalog llmgateway.Catalog, target string) (string, error) {
	if _, err := catalog.Target(ctx, target); err == nil {
		return target, nil
	}
	resolved, err := catalog.TargetByName(ctx, target)
	if err != nil {
		return "", fmt.Errorf("conversation.model_target %q is not a catalog ID or model name: %w", target, err)
	}
	return resolved.ID, nil
}

// runTokenMinter returns the signer the scheduler gives each dispatched run.
//
// Every run gets one, not only a managed one. The token started as a gateway
// credential, but it is now what a worker uses to reach any of its own routes,
// so a direct-mode run needs it to report the work it did.
func runTokenMinter(sc config.ServerConfig, jwtSecret string) scheduler.MintRunToken {
	ttl := sc.Worker.RunTokenTTL
	if sc.Worker.LLM.Managed() {
		slog.Info("task runs use managed inference and hold no provider credential",
			"model", sc.Worker.LLM.Model, "run_token_ttl", ttl)
	}
	return func(claims authtoken.RunClaims) (string, error) {
		return authtoken.MintRun(jwtSecret, claims, ttl, time.Now())
	}
}

// workerLLMDescriptor is what a worker is told about models for its run. It
// carries a model name and nothing else — the endpoint, the upstream model, and
// the credential stay on the server.
//
// Nil means direct, so a deployment that has not enabled managed inference sends
// the field at all.
func workerLLMDescriptor(wc config.ServerWorkerLLMConfig) *workerclient.TaskRunLLM {
	if !wc.Managed() {
		return nil
	}
	return &workerclient.TaskRunLLM{
		Transport:     config.TransportBuildMax,
		Model:         wc.Model,
		ContextWindow: wc.ContextWindow,
		CallTimeout:   wc.CallTimeout,
	}
}

// validateConfiguredModels rejects a server.yaml that names a model the catalog
// does not have.
//
// It runs against the assembled catalog rather than against configuration alone
// because the catalog is a table: only a lookup can tell a real name from a
// typo. An empty catalog is not an error — an operator may add rows after
// starting the server — but a name that was written down and resolves to
// nothing is, since it parses cleanly and would then fail every call that
// relied on it.
func validateConfiguredModels(ctx context.Context, routing *llmRouting, sc config.ServerConfig) error {
	if routing == nil || routing.Router == nil || routing.Router.Resolver == nil {
		return nil
	}
	catalog := routing.Router.Resolver.Catalog
	check := func(field, name string) error {
		if name == "" {
			return nil
		}
		if _, err := catalog.TargetByName(ctx, name); err != nil {
			return fmt.Errorf("%s names %q, which is not in the model catalog: %w", field, name, err)
		}
		return nil
	}
	if err := check("llm.default_model", sc.LLM.DefaultModel); err != nil {
		return err
	}
	if sc.Worker.LLM.Managed() {
		return check("worker.llm.model", sc.Worker.LLM.Model)
	}
	return nil
}

func buildWorkerRunner(wc config.ServerWorkerConfig, stopGrace time.Duration) (scheduler.WorkerRunner, error) {
	switch wc.RunMode {
	case "k8s_job":
		jobClient, err := k8s.BuildK8sJobCreator()
		if err != nil {
			return nil, fmt.Errorf("k8s job creator: %w", err)
		}
		// Worker pods read the same server.yaml the server does: the ConfigMap
		// supplies the file, the inherited BUILDMAX_* environment supplies the
		// credentials that must not be written into it.
		runner, err := k8s.NewK8sJobRunner(
			wc.K8s.Namespace,
			wc.K8s.Image,
			k8s.WorkerEnvFromEnviron(wc.LLM.Managed()),
			k8s.PodConfig{
				ConfigMapName:   wc.K8s.ConfigMap,
				CAConfigMapName: wc.K8s.CAConfigMap,
				CAMountPath:     wc.ServerCAFile,
				HomeDir:         wc.K8s.HomeDir,
				Resources: k8s.PodResources{
					CPURequest:              wc.K8s.Resources.CPURequest,
					CPULimit:                wc.K8s.Resources.CPULimit,
					MemoryRequest:           wc.K8s.Resources.MemoryRequest,
					MemoryLimit:             wc.K8s.Resources.MemoryLimit,
					EphemeralStorageRequest: wc.K8s.Resources.EphemeralStorageRequest,
					EphemeralStorageLimit:   wc.K8s.Resources.EphemeralStorageLimit,
				},
			},
			jobClient,
		)
		if err != nil {
			return nil, fmt.Errorf("server.yaml: %w", err)
		}
		return runner, nil
	default:
		if wc.Binary == "" {
			return nil, fmt.Errorf("worker.binary is required in server.yaml for local_process mode")
		}
		// The two windows have to nest: a worker asked to stop reports over
		// this server's API, so it must finish inside the window the server
		// spends waiting for it. Derived from one budget rather than configured
		// separately, so an operator raising shutdown_grace moves both.
		env := config.FilterWorkerEnv(os.Environ(), wc.LLM.Managed())
		env = append(env, fmt.Sprintf("%s=%s", config.EnvKeyBuildmaxRunInterruptGrace, workerReportWindow(stopGrace)))
		return scheduler.NewLocalRunner(wc.Binary, env, config.EnvKeyBuildmaxRunToken, stopGrace), nil
	}
}

// workerReportWindow is how long a worker gets to report, given how long the
// server will wait for it. The margin covers the signal reaching the process and
// the process exiting after its last write.
func workerReportWindow(stopGrace time.Duration) time.Duration {
	return stopGrace * 80 / 100
}

// toWorkspaceStorageConfig converts ServerStorageConfig to the shared WorkspaceStorageConfig
// used by the objectstore builders.
func toWorkspaceStorageConfig(sc config.ServerStorageConfig) config.WorkspaceStorageConfig {
	return config.WorkspaceStorageConfig{
		PersistProvider:  sc.PersistBackend,
		ArtifactProvider: sc.ArtifactBackend,
		Endpoint:         sc.MinIO.Endpoint,
		Region:           sc.MinIO.Region,
		AccessKey:        sc.MinIO.AccessKey,
		SecretKey:        sc.MinIO.SecretKey,
		Bucket:           sc.MinIO.Bucket,
		Prefix:           sc.MinIO.Prefix,
		PathStyle:        sc.MinIO.PathStyle,
	}
}
