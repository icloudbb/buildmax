package handlers

import (
	"context"
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
	accountroutes "github.com/icloudbb/buildmax/internal/server/handlers/account"
	"github.com/icloudbb/buildmax/internal/server/handlers/admin"
	artifactroutes "github.com/icloudbb/buildmax/internal/server/handlers/artifact"
	authroutes "github.com/icloudbb/buildmax/internal/server/handlers/auth"
	"github.com/icloudbb/buildmax/internal/server/handlers/runterminal"
	spaceroutes "github.com/icloudbb/buildmax/internal/server/handlers/space"
	"github.com/icloudbb/buildmax/internal/server/handlers/work"
	"github.com/icloudbb/buildmax/internal/server/handlers/worker"
	"github.com/icloudbb/buildmax/internal/server/turnqueue"
	wsconn "github.com/icloudbb/buildmax/internal/server/websocket"
	artifactsvc "github.com/icloudbb/buildmax/internal/service/artifact"
	"github.com/icloudbb/buildmax/internal/service/audit"
	"github.com/icloudbb/buildmax/internal/service/conversation"
	convchannel "github.com/icloudbb/buildmax/internal/service/conversation/channel"
	"github.com/icloudbb/buildmax/internal/service/llmgateway"
	pluginsvc "github.com/icloudbb/buildmax/internal/service/plugin"
	"github.com/icloudbb/buildmax/internal/service/quota"
	secretsvc "github.com/icloudbb/buildmax/internal/service/secret"
	workspacesvc "github.com/icloudbb/buildmax/internal/service/workspace"
)

// Config holds all dependencies for the unified handler (auth, user API, worker API, inbound webhook).
type Config struct {
	JWTSecret  string
	CORSOrigin string
	// WorkerLLM tells a worker how to reach a model for its run. Nil means
	// direct, which is what a deployment that has not enabled managed worker
	// inference reports.
	WorkerLLM *workerclient.TaskRunLLM
	// AllowSignup opens POST /api/auth/otp to self-registration. False — the
	// zero value — means accounts are created by an operator.
	AllowSignup bool
	// LocalLogin gates native password/login-code sign-in: "all" (default),
	// "system_admins", or "off". Advertised at GET /api/auth/methods.
	LocalLogin string
	// OIDCEnabled and OIDCDisplayName advertise SSO at GET /api/auth/methods.
	// They carry no secret: the issuer, client, and policy stay server-side.
	OIDCEnabled     bool
	OIDCDisplayName string

	// Token lifetimes. Zero means the model package's default. The access
	// token is signed and unstored, so its lifetime is the window in which a
	// stolen one still works; the refresh token is a row and can be revoked
	// before it expires.
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	// RefreshRotationGrace is how long a just-rotated refresh token may be
	// exchanged again before that counts as reuse. It exists because the CLI
	// and Desktop share one credentials file between processes.
	RefreshRotationGrace time.Duration
	// SessionAbsoluteTTL caps a session's life from its creation, regardless of
	// refresh activity. Zero means the core package default.
	SessionAbsoluteTTL time.Duration

	// Stores
	UserStore             coreidentity.UserStore
	LoginCodeStore        coreidentity.LoginCodeStore
	PasswordStore         coreidentity.PasswordStore
	RefreshTokenStore     coreidentity.RefreshTokenStore
	AuthSessionStore      coreidentity.AuthSessionStore
	ExternalIdentityStore coreidentity.ExternalIdentityStore
	SpaceStore            corespace.Store
	WorkflowStore         coreworkflow.Store
	AgentStore            agentdef.Store
	IssueStore            coreissue.Store
	IssueCommentStore     coreissue.CommentStore
	TaskStore             coretask.Store
	TaskRunStore          coretask.RunStore
	ScheduleStore         coreschedule.Store
	// LLMCallStore reads the managed call ledger. Nil leaves the ledger
	// unreadable over HTTP, which is what a deployment with no database has.
	LLMCallStore             coregw.CallStore
	UserWebhookKeyStore      coreidentity.UserWebhookKeyStore
	ConversationStore        coreconv.Store
	ConversationMessageStore coreconv.MessageStore
	AuditStore               coreaudit.Store
	// LLMModelStore reads the managed model catalog. Nil leaves the admin
	// catalog routes answering 503. It is deliberately read plus enable/disable
	// here: the credential half of the catalog is edited by
	// `buildmax-server model`, on the machine that holds the database
	// credentials.
	LLMModelStore coregw.ModelStore

	// PluginService publishes Marketplace releases and manages catalog
	// entries. Nil leaves the catalog routes reporting that this deployment
	// has no Marketplace.
	PluginService *pluginsvc.Service
	// SecretStore is the Space Secret store, used by the agent consumption
	// validator and the secret service. Nil disables the secret feature.
	SecretStore coresecret.Store
	// SecretService backs the Secret management routes. Nil when no KEK file is
	// configured; then the routes report the feature off.
	SecretService *secretsvc.Service
	// SchemaStore reports which migrations a database has had applied. Nil
	// leaves that field of the system status empty.
	SchemaStore coreschema.Store
	// ArtifactStore records durable files. Nil, or no ArtifactStorage, leaves
	// the artifact routes answering 503: metadata without content is not an
	// artifact capability.
	ArtifactStore coreartifact.Store
	// ArtifactShareStore persists public share links. Nil leaves sharing off.
	ArtifactShareStore coreartifact.ShareStore
	// SystemGrantStore reads deployment-scoped role grants. Nil leaves every
	// /api/admin route answering 503 to an authenticated caller, which is what
	// a deployment with no database has: no way to know whether anyone is an
	// administrator, and therefore no basis for letting one in.
	SystemGrantStore coreidentity.SystemGrantStore

	// WorkspaceCheckpoints finalizes a seed a worker captured; nil disables the
	// worker checkpoint route. WorkspaceCheckpointStore reads a run's base and
	// records its restore outcome; nil disables the base and restore routes.
	WorkspaceCheckpoints     *workspacesvc.Service
	WorkspaceCheckpointStore worker.WorkspaceRunStore

	// Storage
	PersistStorage   blob.PersistStorage
	ArtifactStorage  artifactsvc.ContentStore
	MaxArtifactBytes int64
	// ArtifactPublicBaseURL is the externally reachable origin share links are
	// rendered against. Empty refuses share creation.
	ArtifactPublicBaseURL string
	// ArtifactShareTTL bounds a share link's lifetime. Zero uses the default.
	ArtifactShareTTL time.Duration
	WorkspacesDir    string

	// Auth / quota
	DefaultQuotaTier string
	QuotaService     *quota.Service

	// Conversation / LLM
	TitleGenerator        llm.TitleGenerator
	ConversationLLMClient llm.LLMClient

	// LLMGateway serves managed inference. Nil means the deployment offers no
	// managed models and the /llm routes answer 503.
	LLMGateway *llmgateway.Service

	// Audit records sensitive actions. Nil discards them, so a deployment
	// without a database still serves.
	Audit *audit.Recorder

	// Deployment describes facts about this deployment that do not change
	// while it runs, for the admin system status.
	Deployment admin.DeploymentInfo
	// DependencyProbes are the same checks the readiness endpoint runs. The
	// admin status reports them so an operator sees what /readyz sees without
	// needing to reach it.
	DependencyProbes []admin.DependencyProbe
	// OIDCStatus reports the live SSO provider health for the admin system view.
	// Nil means SSO is not configured. Bootstrap adapts the provider into it.
	OIDCStatus admin.OIDCStatusFunc
	// RedactedConfig is the operator-facing view of server.yaml, built by
	// internal/config so that the decision about which fields may be shown
	// lives next to the struct. Nil means the deployment reports none.
	RedactedConfig any

	// Inbound webhook
	WebhookAdapter     convchannel.Adapter
	WebhookEngine      conversation.TurnEngine
	WebhookMessagePath string

	// Hub is optional; if nil NewHandler creates an in-memory one. A Redis-backed
	// hub is injected here when coordination.mode is redis, so the stream a worker
	// pushes on one replica is readable on another. See
	// docs/design/server-coordination.md.
	Hub wsconn.StreamHub

	// EventBus is optional. When set (coordination.mode redis), connection-registry
	// broadcasts are published to it and this replica delivers what it receives to
	// its own connections, so an event raised on one replica reaches sockets on
	// another. Nil keeps broadcasts process-local.
	EventBus EventBus

	// TurnLocker is optional. When set (coordination.mode redis), the turn queue
	// serializes a conversation's turns across replicas. Nil serializes within this
	// process only, which is correct for a single-replica deployment.
	TurnLocker turnqueue.Locker

	// OnTaskRunTerminal is an optional external callback fired when a worker run reaches
	// terminal status (after the internal hub/registry callbacks run).
	OnTaskRunTerminal func(ctx context.Context, info coretask.RunTerminalInfo)

	// Drain is closed when the server is going away. Watcher streams — the ones
	// that only observe state living in the database — end on it so they stop
	// holding the shutdown open, and so the browser knows to resubscribe
	// somewhere else. Streams that carry work in progress deliberately ignore
	// it. Nil means nothing ever drains, which is what a test has.
	// See docs/design/graceful-shutdown.md §5.
	Drain <-chan struct{}
}

// EventBus is the cross-replica connection-event fan-out the handler wires the
// connection registry to. It is satisfied by the Redis-backed bus in
// internal/server/coordination; kept an interface so this package does not depend
// on it.
type EventBus interface {
	// PublishEvent sends one already-encoded broadcast to every replica.
	PublishEvent(payload []byte)
	// Incoming carries the broadcasts this replica must deliver to its own
	// connections.
	Incoming() <-chan []byte
}

// Handler serves all HTTP routes: auth, user API, worker API, inbound webhook.
type Handler struct {
	cfg          Config
	hub          wsconn.StreamHub
	connRegistry *wsconn.ConnRegistry
	// turns serializes the turns of one conversation and queues the rest. It is
	// server-scoped, not connection-scoped — see turnqueue.Registry.
	turns *turnqueue.Registry
	// terminal owns the callbacks a finished run fires. Server-scoped for the
	// same reason: a shutdown has to wait for all of them, not for the ones one
	// request happened to start.
	terminal *runterminal.Group

	// Surfaces and shared services are assembled once. A request executes a
	// capability; it never rebuilds the application's dependency graph.
	admin         *admin.Handler
	auth          *authroutes.Handler
	account       *accountroutes.Handler
	space         *spaceroutes.Handler
	work          *work.Handler
	worker        *worker.Handler
	artifact      *artifactroutes.Handler
	artifacts     *artifactsvc.Service
	conversations *conversation.Service
}

// NewHandler returns a configured Handler. If cfg.Hub is nil a new in-memory
// StreamHub is created internally. When cfg.EventBus and cfg.TurnLocker are set,
// the handler coordinates streaming, connection events, and turn serialization
// across replicas; nil keeps each process-local. See
// docs/design/server-coordination.md.
func NewHandler(cfg Config) *Handler {
	hub := cfg.Hub
	if hub == nil {
		hub = wsconn.NewStreamHub()
	}
	connRegistry := wsconn.NewConnRegistry()
	if cfg.EventBus != nil {
		connRegistry.SetPublisher(cfg.EventBus.PublishEvent)
		go connRegistry.Consume(cfg.EventBus.Incoming())
	}
	h := &Handler{
		cfg:          cfg,
		hub:          hub,
		connRegistry: connRegistry,
		turns:        turnqueue.NewRegistry(cfg.TurnLocker),
		terminal:     runterminal.NewGroup(),
	}
	h.artifacts = h.buildArtifactService()
	h.conversations = h.buildConversationService()
	h.admin = h.buildAdminHandler()
	h.auth = h.buildAuthHandler()
	h.account = h.buildAccountHandler()
	h.space = h.buildSpaceHandler()
	h.work = h.buildWorkHandler()
	h.worker = h.buildWorkerHandler()
	h.artifact = h.buildArtifactHandler()
	return h
}
