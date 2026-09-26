package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/viper"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
)

// ---------------------------------------------------------------------------
// ServerConfig and nested types
// ---------------------------------------------------------------------------

// ServerConfig is the root of BUILDMAX_HOME/server.yaml.
type ServerConfig struct {
	LogLevel  string `mapstructure:"log_level"`
	Port      int    `mapstructure:"port"`
	JWTSecret string `mapstructure:"jwt_secret"`
	// AccessTokenTTL is how long a signed access token stays valid. It is not
	// stored anywhere, so this is also the window in which a leaked one still
	// works — shortening it costs nothing but refresh traffic.
	AccessTokenTTL time.Duration `mapstructure:"access_token_ttl"`
	// RefreshTokenTTL is how long a login can be renewed without a new login
	// code. Every rotation restarts it, so an active session lives on and an
	// abandoned one expires.
	RefreshTokenTTL time.Duration `mapstructure:"refresh_token_ttl"`
	// RefreshRotationGrace is how long a just-rotated refresh token may be
	// exchanged again before the server treats it as reuse and revokes the
	// session. It absorbs concurrent refreshes from processes sharing one
	// credentials file; it is not a security setting to raise casually.
	RefreshRotationGrace time.Duration `mapstructure:"refresh_rotation_grace"`
	// SessionAbsoluteTTL caps how long one login may live from its creation,
	// regardless of how often its refresh token rotates. It is the ceiling that
	// makes "a session cannot renew forever" true. Zero means the default in
	// internal/core/identity.
	SessionAbsoluteTTL time.Duration `mapstructure:"session_absolute_ttl"`
	// AllowSignup opens POST /api/auth/otp to self-registration. It defaults
	// to false, and the zero value is the safe one on purpose: a server that
	// forgets to configure this is closed, not open.
	AllowSignup bool `mapstructure:"allow_signup"`
	// LocalLogin gates the native password and login-code paths independently of
	// whether SSO is configured. It is one of LocalLoginAll (default),
	// LocalLoginSystemAdmins (a break-glass path for operators while everyone
	// else signs in through the IdP), or LocalLoginOff. It is orthogonal to
	// oidc.enabled: a deployment can run both, only one, or — with off and no
	// OIDC — lock itself out, which startup warns about. See
	// docs/design/enterprise-identity-and-access.md §11.
	LocalLogin string `mapstructure:"local_login"`
	// ShutdownGrace is the whole budget for stopping: draining connections,
	// letting interrupted runs report, and stopping the background loops. The
	// phases are derived from it rather than configured separately, because two
	// knobs that must agree are a way to get them to disagree.
	//
	// It must stay below the orchestrator's own kill deadline —
	// terminationGracePeriodSeconds on Kubernetes, TimeoutStopSec under systemd
	// — or the process is killed partway through an orderly stop. See
	// docs/design/graceful-shutdown.md.
	ShutdownGrace time.Duration `mapstructure:"shutdown_grace"`
	CORSOrigin    string        `mapstructure:"cors_origin"`
	// PublicBaseURL is the externally reachable origin at which people open
	// BuildMax (the Portal, or the shared origin behind a reverse proxy).
	// Artifact share links are rendered against it. Empty refuses share
	// creation rather than emitting an internal URL nobody can open. It is not
	// derived from worker.server_url (a cluster-internal listener) or
	// BUILDMAX_SERVER_URL (the address a process uses to reach the server).
	PublicBaseURL    string              `mapstructure:"public_base_url"`
	WorkspacesDir    string              `mapstructure:"workspaces_dir"`
	DefaultQuotaTier string              `mapstructure:"default_quota_tier"`
	Conversation     ServerConvConfig    `mapstructure:"conversation"`
	LLM              ServerLLMConfig     `mapstructure:"llm"`
	Database         ServerDBConfig      `mapstructure:"database"`
	Webhook          ServerWebhookConfig `mapstructure:"webhook"`
	Worker           ServerWorkerConfig  `mapstructure:"worker"`
	// WorkerAPI is the internal listener that serves the worker control API,
	// kept off the public HTTP surface.
	WorkerAPI ServerWorkerAPIConfig `mapstructure:"worker_api"`
	Storage   ServerStorageConfig   `mapstructure:"storage"`
	Audit     ServerAuditConfig     `mapstructure:"audit"`
	Trace     ServerTraceConfig     `mapstructure:"trace"`
	Secret    ServerSecretConfig    `mapstructure:"secret"`
	// Coordination selects how live server state — streamed deltas, connection
	// events, and conversation turn serialization — is shared across replicas.
	// Its zero value is the single-instance in-process backend. See
	// docs/design/server-coordination.md.
	Coordination ServerCoordinationConfig `mapstructure:"coordination"`
	// OIDC configures corporate sign-in over OpenID Connect. Its zero value is
	// disabled: a deployment that names no IdP gets native login only. See
	// docs/design/enterprise-identity-and-access.md.
	OIDC ServerOIDCConfig `mapstructure:"oidc"`
	// Channels connects instant-messaging platforms, so a linked user can talk
	// to their assistant from a chat app. Its zero value connects none. See
	// docs/design/instant-messaging-channels.md.
	Channels ServerChannelsConfig `mapstructure:"channels"`
}

// ServerChannelsConfig holds one block per supported chat platform.
type ServerChannelsConfig struct {
	Telegram ServerTelegramConfig `mapstructure:"telegram"`
}

// ServerTelegramConfig configures the Telegram bot. The bot receives by long
// polling, so it needs outbound HTTPS to the Bot API and no public URL.
type ServerTelegramConfig struct {
	// BotToken is the token BotFather issued. Empty leaves Telegram off. Inject
	// it with BUILDMAX_TELEGRAM_BOT_TOKEN rather than writing it to disk; it is
	// never served, logged, or handed to a worker.
	BotToken string `mapstructure:"bot_token"`
	// APIBaseURL overrides https://api.telegram.org, for a self-hosted Bot API
	// server.
	APIBaseURL string `mapstructure:"api_base_url"`
}

// Local-login modes for local_login. They gate the native password and
// login-code paths independently of whether OIDC is configured.
const (
	// LocalLoginAll accepts native login for every account. It is the default,
	// so a deployment that configures no SSO behaves exactly as before.
	LocalLoginAll = "all"
	// LocalLoginSystemAdmins accepts native login only for System Administrators:
	// the break-glass path that keeps operators able to sign in when the IdP is
	// unreachable, while everyone else must use SSO.
	LocalLoginSystemAdmins = "system_admins"
	// LocalLoginOff refuses every native login. Only valid with SSO configured;
	// with neither, nobody can sign in, which startup warns about.
	LocalLoginOff = "off"
)

// OIDC provisioning modes for oidc.provisioning.
const (
	// OIDCProvisioningJIT creates a BuildMax account on a first successful
	// sign-in whose verified email is in allowed_email_domains. It is the SSO
	// default; the domain allow-list is what bounds who it will create.
	OIDCProvisioningJIT = "jit"
	// OIDCProvisioningExistingOnly refuses a subject with no linked account: an
	// operator provisions accounts, and SSO only authenticates them.
	OIDCProvisioningExistingOnly = "existing_only"
)

// ServerOIDCConfig configures corporate sign-in over OpenID Connect. OIDC proves
// authentication only; BuildMax still owns accounts, sessions, and every
// authorization decision. See docs/design/enterprise-identity-and-access.md.
type ServerOIDCConfig struct {
	// Enabled turns on the SSO login flow and advertises it at
	// GET /api/auth/methods. It is orthogonal to local_login.
	Enabled bool `mapstructure:"enabled"`
	// DisplayName labels the Portal's sign-in button ("Sign in with <name>").
	// Empty falls back to a generic label; it is never a trust input.
	DisplayName string `mapstructure:"display_name"`
	// Issuer is the IdP's issuer URL and the only URL trust root: Discovery,
	// JWKS, and the token/authorize endpoints are all taken from it, never from a
	// request. It must be HTTPS.
	Issuer string `mapstructure:"issuer"`
	// ClientID is this deployment's registered OIDC client.
	ClientID string `mapstructure:"client_id"`
	// ClientSecret authenticates the confidential client at the token endpoint.
	// Inject it through BUILDMAX_OIDC_CLIENT_SECRET rather than writing it to
	// disk; it is never served, logged, or handed to a worker.
	ClientSecret string `mapstructure:"client_secret"`
	// Provisioning is OIDCProvisioningJIT (default) or OIDCProvisioningExistingOnly.
	Provisioning string `mapstructure:"provisioning"`
	// AllowedEmailDomains bounds JIT provisioning: a first sign-in creates an
	// account only when its verified email's domain is in this list. It is
	// required, and must be non-empty, whenever provisioning is jit — an
	// unbounded JIT would let anyone the IdP authenticates create an account.
	AllowedEmailDomains []string `mapstructure:"allowed_email_domains"`
	// SessionMaxAge caps a session opened through SSO from its creation. Zero
	// uses OIDCSessionMaxAgeDefault. It is the SSO analogue of session_absolute_ttl
	// and is deliberately shorter, because a corporate login is expected to be
	// re-proven against the IdP more often than a native one.
	SessionMaxAge time.Duration `mapstructure:"session_max_age"`
}

// OIDCSessionMaxAgeDefault is the session ceiling for an SSO login when
// oidc.session_max_age is unset.
const OIDCSessionMaxAgeDefault = 12 * time.Hour

// localLogin returns the effective local-login mode, defaulting to all so a
// deployment that configures nothing keeps native login for everyone.
func (sc ServerConfig) localLogin() string {
	if sc.LocalLogin == "" {
		return LocalLoginAll
	}
	return sc.LocalLogin
}

// LocalLogin returns the effective local-login mode.
func (sc ServerConfig) LocalLoginMode() string { return sc.localLogin() }

// ValidateAuth reports the first problem in the login configuration, so a
// deployment that would refuse every login or run an unbounded JIT refuses to
// start rather than discovering it at the first sign-in.
func (sc ServerConfig) ValidateAuth() error {
	switch sc.localLogin() {
	case LocalLoginAll, LocalLoginSystemAdmins, LocalLoginOff:
	default:
		return fmt.Errorf("local_login %q is not one of %q, %q, or %q", sc.LocalLogin, LocalLoginAll, LocalLoginSystemAdmins, LocalLoginOff)
	}
	// Self-registration is a native-login concept. It cannot mean anything when
	// native login is not open to everyone, so pairing it with a narrower mode is
	// a contradiction the operator should see, not a silently ignored setting.
	if sc.AllowSignup && sc.localLogin() != LocalLoginAll {
		return fmt.Errorf("allow_signup requires local_login %q, not %q", LocalLoginAll, sc.localLogin())
	}
	return sc.OIDC.validate(sc.PublicBaseURL)
}

// SessionMaxAge returns the effective SSO session ceiling.
func (o ServerOIDCConfig) sessionMaxAge() time.Duration {
	if o.SessionMaxAge <= 0 {
		return OIDCSessionMaxAgeDefault
	}
	return o.SessionMaxAge
}

// provisioning returns the effective provisioning mode, defaulting to jit.
func (o ServerOIDCConfig) provisioning() string {
	if o.Provisioning == "" {
		return OIDCProvisioningJIT
	}
	return o.Provisioning
}

// validate reports the first problem that would make SSO unusable or unsafe.
// It is a no-op when OIDC is disabled: an operator drafting a block should not
// be blocked from starting until they turn it on.
func (o ServerOIDCConfig) validate(publicBaseURL string) error {
	if !o.Enabled {
		return nil
	}
	iss, err := url.Parse(o.Issuer)
	if err != nil || o.Issuer == "" || iss.Host == "" {
		return fmt.Errorf("oidc.issuer must be a valid URL when oidc.enabled")
	}
	if iss.Scheme != "https" {
		return errors.New("oidc.issuer must be https: the issuer is the only URL trust root and its metadata cannot travel in the clear")
	}
	if o.ClientID == "" {
		return errors.New("oidc.client_id is required when oidc.enabled")
	}
	switch o.provisioning() {
	case OIDCProvisioningJIT:
		if len(o.AllowedEmailDomains) == 0 {
			return fmt.Errorf("oidc.allowed_email_domains must be non-empty when oidc.provisioning is %q: unbounded just-in-time provisioning would let anyone the IdP authenticates create an account", OIDCProvisioningJIT)
		}
	case OIDCProvisioningExistingOnly:
	default:
		return fmt.Errorf("oidc.provisioning %q is not one of %q or %q", o.Provisioning, OIDCProvisioningJIT, OIDCProvisioningExistingOnly)
	}
	// The browser is redirected back to public_base_url + a fixed callback path,
	// and an OAuth redirect URI carrying a code must not travel in the clear. A
	// loopback origin is the documented development exception.
	if publicBaseURL == "" {
		return errors.New("public_base_url is required when oidc.enabled: the OIDC redirect URI is built from it")
	}
	pub, err := url.Parse(publicBaseURL)
	if err != nil || pub.Host == "" {
		return fmt.Errorf("public_base_url must be a valid URL when oidc.enabled")
	}
	if pub.Scheme != "https" && !isLoopbackHost(pub.Hostname()) {
		return errors.New("public_base_url must be https when oidc.enabled, except for a loopback development origin")
	}
	return nil
}

// Coordination backend modes for coordination.mode.
const (
	// CoordinationModeLocal keeps live state in one process's memory. It is the
	// default and the only correct mode for a single server replica.
	CoordinationModeLocal = "local"
	// CoordinationModeRedis shares live state through Redis so more than one
	// server replica stays consistent.
	CoordinationModeRedis = "redis"
)

// ServerCoordinationConfig selects and configures the cross-replica coordination
// backend. A deployment that runs more than one server replica must set
// mode: redis; the default single-replica topology needs nothing here. See
// docs/design/server-coordination.md.
type ServerCoordinationConfig struct {
	// Mode is CoordinationModeLocal (default) or CoordinationModeRedis. Any other
	// value is a configuration error the server refuses to start on, rather than
	// silently choosing a backend the operator did not name.
	Mode  string                  `mapstructure:"mode"`
	Redis ServerCoordinationRedis `mapstructure:"redis"`
}

// ServerCoordinationRedis holds the connection settings for the Redis
// coordination backend. They are read only when mode is redis.
type ServerCoordinationRedis struct {
	// Address is the Redis endpoint as host:port.
	Address string `mapstructure:"address"`
	// Username is optional and used with Redis 6+ ACLs.
	Username string `mapstructure:"username"`
	// Password is optional. It follows the same pattern as database.password:
	// BUILDMAX_COORDINATION_REDIS_PASSWORD overrides the file so the credential
	// need not sit on disk.
	Password string `mapstructure:"password"`
	// DB is the logical Redis database number.
	DB int `mapstructure:"db"`
	// TLS dials Redis over TLS.
	TLS bool `mapstructure:"tls"`
}

// Mode returns the coordination mode, defaulting to local when unset so the
// zero-value config is the single-instance one.
func (c ServerCoordinationConfig) mode() string {
	if c.Mode == "" {
		return CoordinationModeLocal
	}
	return c.Mode
}

// Validate reports the first problem that would make the coordination backend
// unusable, so a misconfigured deployment refuses to start rather than serving
// with process-local coordination under a multi-replica manifest.
func (c ServerCoordinationConfig) Validate() error {
	switch c.mode() {
	case CoordinationModeLocal:
		return nil
	case CoordinationModeRedis:
		if c.Redis.Address == "" {
			return errors.New("coordination.redis.address is required when coordination.mode is redis")
		}
		return nil
	default:
		return fmt.Errorf("coordination.mode %q is not one of %q or %q", c.Mode, CoordinationModeLocal, CoordinationModeRedis)
	}
}

// RedisEnabled reports whether the Redis coordination backend is selected.
func (c ServerCoordinationConfig) RedisEnabled() bool {
	return c.mode() == CoordinationModeRedis
}

// ServerWorkerAPIConfig configures the second HTTP listener that serves only
// the worker control API (/api/worker/*). Splitting it off the public listener
// is what lets a Kubernetes NetworkPolicy admit a worker to those routes while
// denying it the rest of the API, and keeps /api/worker off the public Ingress.
// See docs/design/worker-api-network-boundary.md.
type ServerWorkerAPIConfig struct {
	// Listen is the worker listener's bind address. It defaults to loopback so
	// an accidental deployment opens no new unauthenticated cluster port; a
	// Kubernetes deployment binds it to :5679 deliberately and fronts it with
	// its own internal Service.
	Listen string `mapstructure:"listen"`
	// TLS is the worker listener's server certificate and optional native mTLS
	// client CA. Empty cert and key serve plain HTTP, which is a development-only
	// mode. Enforcing HTTPS in production is the worker-client trust work in the
	// design record's M2, not this listener's parsing.
	TLS ServerWorkerAPITLSConfig `mapstructure:"tls"`
}

// ServerWorkerAPITLSConfig holds the worker listener's TLS material. The
// certificate and key are the server identity a worker verifies; client_ca_file
// is optional and, when set, turns on native mTLS so the listener also requires
// a client certificate the CA issued.
type ServerWorkerAPITLSConfig struct {
	CertFile     string `mapstructure:"cert_file"`
	KeyFile      string `mapstructure:"key_file"`
	ClientCAFile string `mapstructure:"client_ca_file"` // optional native mTLS
}

// ServerAuditConfig decides how long the governance trail is kept.
type ServerAuditConfig struct {
	// RetentionDays expires audit events older than the window. Zero, the
	// default, keeps them forever.
	//
	// Keeping is the default because the trail is evidence, and a deployment
	// that never chose a retention policy has not decided to discard anything.
	// Setting this is a deliberate act with a cost, which is why the sweep
	// records what it removed: a trail that begins partway through then says
	// that policy shortened it, rather than leaving a reader to guess between
	// policy and loss.
	RetentionDays int `mapstructure:"retention_days"`
}

// ServerTraceConfig decides how long a run's durable trace is kept.
type ServerTraceConfig struct {
	// RetentionDays expires the trace of a run that ended longer ago than the
	// window. Zero, the default, keeps every trace forever.
	//
	// Traces are diagnostic evidence, so keeping is the default for the same
	// reason it is for the audit trail: a deployment that never chose a policy
	// has not decided to discard anything. When a sweep does remove a trace it
	// clears the run's pointer to it and records the prune, so a later reader
	// finds a run that explicitly has no trace rather than one whose trace
	// silently 404s. See docs/design/durable-run-trace.md.
	RetentionDays int `mapstructure:"retention_days"`
}

// ServerConvConfig holds Tier 1 conversation LLM settings.
type ServerConvConfig struct {
	Model ServerModelEntry `mapstructure:"model"`
	// ModelTarget names a catalog model (an llm_model row) to use for Tier 1
	// inference instead of the model above. It is the server picking its own
	// model rather than being granted one, and accepts either the catalog ID or
	// the operator-facing model name — the name is what an operator can write
	// down before the row's runtime ID exists (after `model add`/`kind seed`).
	ModelTarget string `mapstructure:"model_target"`
}

// ServerModelEntry is the LLM model used for Tier 1 conversation.
type ServerModelEntry struct {
	Model         string `mapstructure:"model"`
	Name          string `mapstructure:"name"`
	APIURL        string `mapstructure:"api_url"`
	APIKey        string `mapstructure:"api_key"`
	ContextWindow int    `mapstructure:"context_window"`
	CallTimeout   int    `mapstructure:"call_timeout"` // seconds; 0 = uses DefaultCallTimeoutSecs
	MaxTokens     int    `mapstructure:"max_tokens"`   // 0 = the adapter's own default
	// Provider is the wire protocol this model speaks. Empty means
	// LLMProviderOpenAICompatible.
	Provider string `mapstructure:"provider"`
	// Reasoning is the effort level: off (the default), low, medium, or high.
	Reasoning string `mapstructure:"reasoning"`
	// CacheControl is this model's prompt-cache policy.
	CacheControl *CacheControl `mapstructure:"cache_control"`
	// Pricing is what this model charges; without it cost is unavailable.
	Pricing *ModelPricing `mapstructure:"pricing"`
	// Vision says this model accepts image input.
	Vision bool `mapstructure:"vision"`
}

// RuntimeModelEntry converts the server's resolved model configuration into
// the model shape used by the shared agent runtime. Environment overrides have
// already been applied before this conversion, so credentials stay in memory.
//
// This is the direct path: the entry carries the server's own credential and
// the run calls the provider itself. A managed run is assembled elsewhere, from
// what the server told it at dispatch — see internal/bootstrap.resolveRunModel.
func (m ServerModelEntry) RuntimeModelEntry() ModelEntry {
	return ModelEntry{
		Model:         m.Model,
		Name:          m.Name,
		APIURL:        m.APIURL,
		APIKey:        m.APIKey,
		ContextWindow: m.ContextWindow,
		CallTimeout:   m.CallTimeout,
		MaxTokens:     m.MaxTokens,
		Provider:      m.Provider,
		Reasoning:     m.Reasoning,
		CacheControl:  m.CacheControl,
		Pricing:       m.Pricing,
		Vision:        m.Vision,
	}
}

// ServerLLMConfig is the deployment's managed-model configuration. The catalog
// itself lives in the llm_model table, edited with `buildmax-server model`,
// because it holds provider credentials and changes while the server runs.
//
// Every catalog model is available to every user of the deployment: a space is a
// collaboration boundary, not a model authorization boundary. See
// docs/design/client-modes.md section 5.
//
// It is optional. A server with no llm section serves Tier 1 conversations from
// conversation.model and lets callers name any catalog model.
type ServerLLMConfig struct {
	// DefaultModel names the catalog model a caller gets when it names none.
	// Empty falls back to the first model in the catalog, so a single-model
	// deployment needs no configuration at all.
	DefaultModel string `mapstructure:"default_model"`
}

// ServerDBConfig holds MySQL connection settings.
type ServerDBConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	User     string `mapstructure:"user"`
	Password string `mapstructure:"password"`
	Name     string `mapstructure:"name"`
	// TLS is the go-sql-driver tls parameter. Empty means DefaultDBTLSMode.
	//
	// A deployment pointing at a database it already runs — RDS, Aurora, Cloud
	// SQL — is the case this exists for: most managed MySQL either requires TLS
	// or is reached over a network where the credentials should not travel in
	// the clear.
	//
	// Values: "preferred" (TLS when the server offers it, certificate not
	// verified), "true" (require TLS and verify the certificate against the
	// system roots), "skip-verify" (require TLS, accept any certificate), or
	// "false" (never).
	TLS string `mapstructure:"tls"`
}

// DefaultDBTLSMode is used when database.tls is unset.
//
// "preferred" upgrades a connection whenever the server advertises TLS and
// behaves exactly as before against one that does not, so it has no failure
// mode a plaintext connection did not already have. It does not verify the
// certificate; a deployment that needs that sets "true".
const DefaultDBTLSMode = "preferred"

// DSN builds a MySQL DSN from the database config.
func (d ServerDBConfig) DSN() string {
	host := d.Host
	if host == "" {
		host = "localhost"
	}
	port := d.Port
	if port == 0 {
		port = 3306
	}
	user := d.User
	if user == "" {
		user = "buildmax"
	}
	password := d.Password
	name := d.Name
	if name == "" {
		name = "buildmax"
	}
	tlsMode := d.TLS
	if tlsMode == "" {
		tlsMode = DefaultDBTLSMode
	}
	return (&serverDBFormatter{host: host, port: port, user: user, password: password, name: name, tls: tlsMode}).dsn()
}

type serverDBFormatter struct {
	host, user, password, name, tls string
	port                            int
}

func (f *serverDBFormatter) dsn() string {
	dsn := f.user + ":" + f.password + "@tcp(" + f.host + ":" + itoa(f.port) + ")/" + f.name + "?charset=utf8mb4&parseTime=True"
	if f.tls != "" {
		dsn += "&tls=" + f.tls
	}
	return dsn
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n >= 10 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	pos--
	buf[pos] = byte('0' + n)
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// ServerWebhookConfig holds webhook handler options.
type ServerWebhookConfig struct {
	MessagePath string `mapstructure:"message_path"`
	UserID      string `mapstructure:"user_id"`
}

// ServerWorkerConfig holds worker launch and connection options.
type ServerWorkerConfig struct {
	Binary    string                `mapstructure:"binary"`
	RunMode   string                `mapstructure:"run_mode"`
	ServerURL string                `mapstructure:"server_url"`
	LLM       ServerWorkerLLMConfig `mapstructure:"llm"`
	K8s       ServerK8sConfig       `mapstructure:"k8s"`
	// RunTokenTTL bounds a run token. Zero uses authtoken's default. It has to
	// outlast the longest run: the token is not renewable, so a run that outlives
	// it can no longer report anything, including its own result.
	RunTokenTTL time.Duration `mapstructure:"run_token_ttl"`
	// RunTimeout is how long a run may stay SCHEDULED or RUNNING before the
	// server records it as abandoned. Zero uses the scheduler's default.
	//
	// Only the worker moves a run out of those states, so without this a run
	// whose worker died stays there forever. Keep it at or below RunTokenTTL: a
	// run that outlived its credential cannot report an outcome, so nothing else
	// will ever close it.
	RunTimeout time.Duration `mapstructure:"run_timeout"`
	// AllowInsecureHTTP permits a worker to reach the server over plain HTTP.
	// Off by default and required for a k8s_job whose server_url is http://,
	// because .cluster.local, loopback, and private addresses are routing facts,
	// not evidence that a network is confidential. local_process, Compose, and
	// kind development set it. Enforcing this is the design record's M2.
	AllowInsecureHTTP bool `mapstructure:"allow_insecure_http"`
	// ServerCAFile is the CA the worker verifies the worker listener's
	// certificate against. Empty uses the system trust roots.
	ServerCAFile string `mapstructure:"server_ca_file"`
	// ClientCertFile and ClientKeyFile are the optional native mTLS client
	// identity a worker presents in addition to its run token. Both or neither.
	ClientCertFile string `mapstructure:"client_cert_file"`
	ClientKeyFile  string `mapstructure:"client_key_file"`
}

// ServerWorkerLLMConfig decides how a task run reaches a model.
//
// The choice is the operator's and lives on the server, not in the worker's
// hands: a worker executes model-chosen code, so it is told which transport and
// alias to use rather than selecting them.
type ServerWorkerLLMConfig struct {
	// Transport is TransportDirect or TransportBuildMax. Empty means direct,
	// which is what every existing deployment gets.
	Transport string `mapstructure:"transport"`
	// Model is the catalog model a managed run calls. Empty uses the
	// deployment's llm.default_model.
	Model string `mapstructure:"model"`
	// ContextWindow and CallTimeout describe the alias to the run. The protocol
	// does not report them per call, so they come from configuration or stay
	// unset.
	ContextWindow int `mapstructure:"context_window"`
	CallTimeout   int `mapstructure:"call_timeout"`
}

// Managed reports whether task runs call the gateway instead of a provider.
func (c ServerWorkerLLMConfig) Managed() bool { return c.Transport == TransportBuildMax }

// ServerK8sConfig holds Kubernetes worker job settings.
type ServerK8sConfig struct {
	Namespace string `mapstructure:"namespace"`
	Image     string `mapstructure:"image"`
	// ConfigMap names a ConfigMap holding a server.yaml key. It is mounted into
	// every worker pod so the worker reads the same configuration the server does.
	// Empty means no config file is mounted and the worker relies on inherited
	// environment variables alone, which is rarely enough.
	ConfigMap string `mapstructure:"config_map"`
	// CAConfigMap names a ConfigMap holding the worker-api CA certificate,
	// mounted read-only into every worker pod at worker.server_ca_file so the
	// worker verifies the server listener. Empty mounts none. See
	// docs/design/worker-api-network-boundary.md §6.
	CAConfigMap string `mapstructure:"ca_config_map"`
	// HomeDir is BUILDMAX_HOME inside a worker pod; server.yaml is mounted there.
	HomeDir string `mapstructure:"home_dir"`
	// Resources bounds a worker pod. Every bound is required in this run mode:
	// the server refuses to start rather than schedule a worker that model-
	// chosen commands could run unbounded.
	Resources ServerK8sResources `mapstructure:"resources"`
}

// ServerK8sResources holds Kubernetes quantity strings for a worker pod.
type ServerK8sResources struct {
	CPURequest    string `mapstructure:"cpu_request"`
	CPULimit      string `mapstructure:"cpu_limit"`
	MemoryRequest string `mapstructure:"memory_request"`
	MemoryLimit   string `mapstructure:"memory_limit"`
	// EphemeralStorageRequest and EphemeralStorageLimit bound the pod's local
	// scratch disk — the writable layer plus every emptyDir, which is where the
	// materialized workspace, the checkpoint payload staged for upload, and
	// ordinary tool output all land. The limit is also applied as the sizeLimit
	// of each emptyDir, so a runaway workspace is evicted cleanly instead of
	// filling the node. Required in this run mode, like the CPU and memory
	// bounds. See docs/design/task-workspace-checkpoints.md §12.3.
	EphemeralStorageRequest string `mapstructure:"ephemeral_storage_request"`
	EphemeralStorageLimit   string `mapstructure:"ephemeral_storage_limit"`
}

// ServerStorageConfig holds blob storage backend selection and MinIO settings.
type ServerStorageConfig struct {
	PersistBackend  string            `mapstructure:"persist_backend"`
	ArtifactBackend string            `mapstructure:"artifact_backend"`
	MinIO           ServerMinIOConfig `mapstructure:"minio"`
	// MaxArtifactMB caps one artifact upload. Zero uses the built-in default.
	//
	// It is a per-file limit, not a space storage allowance. The allowance is a
	// stock rather than a rate and lives in the quota tier as
	// max_storage_bytes; this stays the cap on any one request.
	MaxArtifactMB int `mapstructure:"max_artifact_mb"`
	// ArtifactPurgeAfterDays delays reclaiming a deleted artifact's object.
	//
	// Zero, the default, reclaims it on the next retention sweep: deletion has
	// already taken effect at the authorization boundary, so holding the bytes
	// afterwards is cost and exposure rather than safety. A deployment that
	// wants a window in which an operator could still recover the object from
	// the bucket sets a number of days here — BuildMax itself offers no
	// undelete, so the window is for the bucket's own tooling.
	ArtifactPurgeAfterDays int `mapstructure:"artifact_purge_after_days"`
	// ArtifactShareTTLHours bounds a public share link's lifetime, as both the
	// default and the maximum a create request may ask for. Zero uses the
	// built-in default (30 days).
	ArtifactShareTTLHours int `mapstructure:"artifact_share_ttl_hours"`
	// CheckpointOrphanGraceDays holds an uncommitted checkpoint payload this long
	// before the orphan sweep reclaims it — bytes a worker uploaded whose pointer
	// never committed. Zero, the default, reclaims on the next sweep once the
	// payload is older than the sweep can observe an in-flight upload, which is
	// safe because the commit protocol writes bytes before the pointer. A larger
	// window lets an operator recover the payload from the bucket first; BuildMax
	// offers no undelete of its own.
	CheckpointOrphanGraceDays int `mapstructure:"checkpoint_orphan_grace_days"`
}

// ServerSecretConfig configures the Space Secret store. KEKFile is the path to
// the mounted key file that wraps every secret's data key; the key material
// itself is never in server.yaml or the environment, only the path is. Empty
// disables the Space Secret feature: an agent that consumes a Secret is refused,
// the same way naming a plugin is refused with no Marketplace. See
// docs/design/space-secrets.md §9.1.
type ServerSecretConfig struct {
	KEKFile string `mapstructure:"kek_file"`
}

// ServerMinIOConfig holds MinIO/S3 connection settings.
type ServerMinIOConfig struct {
	Endpoint  string `mapstructure:"endpoint"`
	Region    string `mapstructure:"region"`
	AccessKey string `mapstructure:"access_key"`
	SecretKey string `mapstructure:"secret_key"`
	Bucket    string `mapstructure:"bucket"`
	Prefix    string `mapstructure:"prefix"`
	// PathStyle forces bucket-in-path addressing. Unset derives it from
	// endpoint: set means an S3-compatible store such as MinIO, which needs
	// path style; empty means real AWS S3, which does not. Set it explicitly
	// for a compatible store that uses virtual-host addressing.
	PathStyle *bool `mapstructure:"path_style"`
}

// ---------------------------------------------------------------------------
// Loader
// ---------------------------------------------------------------------------

// ServerConfigPath returns the path to the server config file (BUILDMAX_HOME/server.yaml).
func ServerConfigPath() string {
	return filepath.Join(DataDir(), "server.yaml")
}

// Environment overrides for secret-bearing server.yaml fields. The file stays the
// source of truth for shape and non-secret values; these exist so a deployment can
// inject credentials from a Kubernetes Secret, a Docker secret, or a CI variable
// without writing them to disk. Same pattern as jwt_secret.
const (
	// BUILDMAX_DATABASE_PASSWORD overrides database.password.
	EnvKeyBuildmaxDatabasePassword = "BUILDMAX_DATABASE_PASSWORD"
	// BUILDMAX_STORAGE_MINIO_ACCESS_KEY overrides storage.minio.access_key.
	EnvKeyBuildmaxMinIOAccessKey = "BUILDMAX_STORAGE_MINIO_ACCESS_KEY"
	// BUILDMAX_STORAGE_MINIO_SECRET_KEY overrides storage.minio.secret_key.
	EnvKeyBuildmaxMinIOSecretKey = "BUILDMAX_STORAGE_MINIO_SECRET_KEY"
	// BUILDMAX_CONVERSATION_MODEL_API_KEY overrides conversation.model.api_key.
	EnvKeyBuildmaxConversationAPIKey = "BUILDMAX_CONVERSATION_MODEL_API_KEY"
	// BUILDMAX_COORDINATION_REDIS_PASSWORD overrides coordination.redis.password.
	EnvKeyBuildmaxCoordinationRedisPassword = "BUILDMAX_COORDINATION_REDIS_PASSWORD"
	// BUILDMAX_OIDC_CLIENT_SECRET overrides oidc.client_secret. Injecting the
	// confidential client's secret at deploy time keeps it off disk, like
	// jwt_secret; it is never served, logged, or handed to a worker.
	EnvKeyBuildmaxOIDCClientSecret = "BUILDMAX_OIDC_CLIENT_SECRET"
	// BUILDMAX_TELEGRAM_BOT_TOKEN overrides channels.telegram.bot_token. The
	// token is the bot's whole identity, so it is injected like a secret.
	EnvKeyBuildmaxTelegramBotToken = "BUILDMAX_TELEGRAM_BOT_TOKEN"
)

// BUILDMAX_CORS_ORIGIN overrides cors_origin.
//
// The one override here that carries no secret. cors_origin has to name the
// origin the Portal is actually served from, which is a host port the
// deployment chooses and the file cannot know: the Compose stack publishes the
// Portal on BUILDMAX_PORTAL_PORT, so a committed server.yaml that spells the
// default out is wrong for every other value. Deriving it where the port is
// chosen is what keeps moving that port a one-variable change.
const EnvKeyBuildmaxCORSOrigin = "BUILDMAX_CORS_ORIGIN"

// Model-selection overrides. None carries a secret: the credential still comes
// from the catalog row (managed) or conversation.model.api_key (direct). They
// exist so one built image can flip between a deterministic mock and a seeded
// catalog model by environment alone, without rewriting the mounted server.yaml
// — which is what lets `kind use-model`/`kind mock` switch a running cluster.
const (
	// BUILDMAX_WORKER_LLM_TRANSPORT overrides worker.llm.transport ("direct" or
	// "buildmax"). The server reads it and tells each worker which transport to
	// use, so the choice still lives on the server, not in the worker's hands.
	EnvKeyBuildmaxWorkerLLMTransport = "BUILDMAX_WORKER_LLM_TRANSPORT"
	// BUILDMAX_LLM_DEFAULT_MODEL overrides llm.default_model: the catalog model
	// name a managed run and any caller that names none resolves to.
	EnvKeyBuildmaxLLMDefaultModel = "BUILDMAX_LLM_DEFAULT_MODEL"
	// BUILDMAX_CONVERSATION_MODEL_TARGET overrides conversation.model_target: a
	// catalog model name or ID for Tier 1 conversations.
	EnvKeyBuildmaxConversationModelTarget = "BUILDMAX_CONVERSATION_MODEL_TARGET"
)

// LoadServerConfig reads BUILDMAX_HOME/server.yaml via Viper and applies defaults.
// Secret-bearing fields, cors_origin, and worker.server_url can be overridden
// by the environment variables above and in env_spec.go.
// A missing file is not an error — returns a config with all defaults applied.
func LoadServerConfig() (ServerConfig, error) {
	v := viper.New()
	v.SetConfigFile(ServerConfigPath())

	// Defaults
	v.SetDefault("log_level", "info")
	v.SetDefault("port", 5678)
	v.SetDefault("cors_origin", "http://localhost:5173")
	// The session lifetimes come from the identity domain rather than being
	// restated here: a copy here once shadowed the 15-minute access-token
	// default with a week.
	v.SetDefault("access_token_ttl", coreidentity.AccessTokenTTLDefault)
	v.SetDefault("refresh_token_ttl", coreidentity.RefreshTokenTTLDefault)
	v.SetDefault("refresh_rotation_grace", coreidentity.RefreshRotationGraceDefault)
	v.SetDefault("shutdown_grace", "25s")
	v.SetDefault("default_quota_tier", "free_trial")
	v.SetDefault("webhook.message_path", "message")
	v.SetDefault("webhook.user_id", "webhook")
	v.SetDefault("worker.binary", "buildmax-worker")
	v.SetDefault("worker.run_mode", "local_process")
	// Loopback by default: the secure default opens no new cluster port. A
	// Kubernetes deployment overrides it to :5679 and fronts it with an internal
	// Service. See docs/design/worker-api-network-boundary.md §3.
	v.SetDefault("worker_api.listen", "127.0.0.1:5679")
	v.SetDefault("worker.k8s.namespace", "buildmax")
	v.SetDefault("worker.k8s.image", "buildmax:local")
	v.SetDefault("worker.k8s.config_map", "buildmax-config")
	v.SetDefault("worker.k8s.home_dir", "/buildmax")
	v.SetDefault("storage.persist_backend", ProviderLocalFS)
	v.SetDefault("storage.artifact_backend", ProviderLocalFS)
	// endpoint, region, and the two keys have no defaults on purpose.
	//
	// A default endpoint sends a deployment to localhost; default credentials
	// send it as a MinIO development user. Both used to be set, which made
	// "leave it empty and let the SDK resolve it" impossible to express — the
	// value was never empty. Unset now means unset, which is what selects AWS
	// endpoint resolution and the default credential chain.
	v.SetDefault("storage.minio.bucket", "bmstore")
	v.SetDefault("storage.minio.prefix", "workspaces")
	v.SetDefault("database.host", "localhost")
	v.SetDefault("database.port", 3306)
	v.SetDefault("database.user", "buildmax")
	v.SetDefault("database.password", "buildmax")
	v.SetDefault("database.name", "buildmax")
	v.SetDefault("conversation.model.context_window", 0)
	v.SetDefault("coordination.mode", CoordinationModeLocal)
	v.SetDefault("coordination.redis.db", 0)
	v.SetDefault("local_login", LocalLoginAll)
	v.SetDefault("oidc.enabled", false)
	v.SetDefault("oidc.provisioning", OIDCProvisioningJIT)
	v.SetDefault("oidc.session_max_age", OIDCSessionMaxAgeDefault.String())

	// Environment overrides for values the file cannot hold: credentials that
	// should not be on disk, and cors_origin, which only the deployment knows.
	// An explicit env name is passed, so SetEnvPrefix does not apply to these.
	v.SetEnvPrefix("BUILDMAX")
	_ = v.BindEnv("jwt_secret", EnvKeyBuildmaxJWTSecret)
	_ = v.BindEnv("cors_origin", EnvKeyBuildmaxCORSOrigin)
	_ = v.BindEnv("public_base_url", EnvKeyBuildmaxPublicBaseURL)
	_ = v.BindEnv("worker.server_url", EnvKeyBuildmaxServerURL)
	_ = v.BindEnv("database.password", EnvKeyBuildmaxDatabasePassword)
	_ = v.BindEnv("storage.minio.access_key", EnvKeyBuildmaxMinIOAccessKey)
	_ = v.BindEnv("storage.minio.secret_key", EnvKeyBuildmaxMinIOSecretKey)
	_ = v.BindEnv("conversation.model.api_key", EnvKeyBuildmaxConversationAPIKey)
	_ = v.BindEnv("coordination.redis.password", EnvKeyBuildmaxCoordinationRedisPassword)
	_ = v.BindEnv("oidc.client_secret", EnvKeyBuildmaxOIDCClientSecret)
	_ = v.BindEnv("channels.telegram.bot_token", EnvKeyBuildmaxTelegramBotToken)
	_ = v.BindEnv("worker.llm.transport", EnvKeyBuildmaxWorkerLLMTransport)
	_ = v.BindEnv("llm.default_model", EnvKeyBuildmaxLLMDefaultModel)
	_ = v.BindEnv("conversation.model_target", EnvKeyBuildmaxConversationModelTarget)

	if err := v.ReadInConfig(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return ServerConfig{}, err
	}

	var cfg ServerConfig
	if err := v.Unmarshal(&cfg); err != nil {
		return ServerConfig{}, err
	}
	return cfg, nil
}

// ValidateListeners enforces the fail-closed two-listener rules from
// docs/design/worker-api-network-boundary.md §10 that can be decided from
// configuration alone. publicAddr is the resolved public listen address (e.g.
// ":5678"). It reports the first problem it finds so a misconfigured deployment
// refuses to start rather than opening a socket that undoes the boundary.
//
// The rules that need TLS trust-root probing or the resolved worker URL — an
// http:// k8s_job without allow_insecure_http, an https:// URL with no usable
// roots, a worker URL that points back at the public listener — are the design
// record's M2 and are not enforced here yet.
func (sc ServerConfig) ValidateListeners(publicAddr string) error {
	workerListen := sc.WorkerAPI.Listen
	if workerListen == "" {
		return errors.New("worker_api.listen is required")
	}

	// A shared port is the failure this whole design exists to prevent: two
	// names for one socket. The public listener binds every interface on its
	// port, so a worker listener on the same port collides whatever its host.
	publicPort, err := listenPort(publicAddr)
	if err != nil {
		return fmt.Errorf("public listen address %q: %w", publicAddr, err)
	}
	workerPort, err := listenPort(workerListen)
	if err != nil {
		return fmt.Errorf("worker_api.listen %q: %w", workerListen, err)
	}
	if publicPort == workerPort {
		return fmt.Errorf("worker_api.listen port %s collides with the public listener; the two listeners must use different ports", workerPort)
	}

	// Half a keypair serves no TLS and hides the intent to. Both or neither.
	cert, key := sc.WorkerAPI.TLS.CertFile, sc.WorkerAPI.TLS.KeyFile
	if (cert == "") != (key == "") {
		return errors.New("worker_api.tls needs both cert_file and key_file, or neither")
	}

	// Native mTLS is the worker presenting a client certificate. Half of one is
	// a deployment that meant to and cannot.
	clientCert, clientKey := sc.Worker.ClientCertFile, sc.Worker.ClientKeyFile
	if (clientCert == "") != (clientKey == "") {
		return errors.New("worker.client_cert_file and worker.client_key_file must be set together for native mTLS, or neither")
	}

	if err := sc.validateWorkerURL(publicPort); err != nil {
		return err
	}

	return nil
}

// validateWorkerURL checks the address workers are handed. An empty URL is left
// to the worker to reject at run time; a set one must not be plaintext for a
// k8s_job without an explicit opt-in, and must not point back at the public
// listener. publicPort is the resolved public listener port.
func (sc ServerConfig) validateWorkerURL(publicPort string) error {
	raw := sc.Worker.ServerURL
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("worker.server_url %q: %w", raw, err)
	}
	if u.Host == "" {
		return fmt.Errorf("worker.server_url %q has no host", raw)
	}

	// Plaintext is a routing fact, not a confidential channel. A k8s_job over
	// http must say so deliberately; .cluster.local is not evidence of privacy.
	if u.Scheme == "http" && sc.Worker.RunMode == "k8s_job" && !sc.Worker.AllowInsecureHTTP {
		return errors.New("worker.server_url uses http with run_mode k8s_job; set worker.allow_insecure_http to accept a plaintext control channel, or use https")
	}

	// A worker URL on the public listener would route worker calls to a mux that
	// does not serve them. Only decidable for a loopback host, where the port is
	// this same process; a DNS name may resolve anywhere.
	if isLoopbackHost(u.Hostname()) && u.Port() == publicPort {
		return fmt.Errorf("worker.server_url %q points at the public listener port %s; workers must reach the worker listener", raw, publicPort)
	}
	return nil
}

// isLoopbackHost reports whether host is a loopback name or address.
func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// listenPort extracts the port from a listen address such as ":5678",
// "127.0.0.1:5679", or "0.0.0.0:5678".
func listenPort(addr string) (string, error) {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", err
	}
	if port == "" {
		return "", errors.New("no port")
	}
	return port, nil
}
