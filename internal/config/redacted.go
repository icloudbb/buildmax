package config

// The operator-facing view of server.yaml.
//
// It lives here, next to the struct it describes, rather than in the handler
// that serves it. When someone adds a configuration field, the decision about
// whether it may be shown is then in the file they are already editing, which
// is the best chance of that decision being made at all.
//
// The shape is a whitelist: every field below is named on purpose, and a field
// added to ServerConfig appears here only when someone puts it here. A
// blacklist — "hide anything called password" — fails open on every field added
// afterwards, which is the wrong direction for the one endpoint whose job is to
// be safe to look at.
//
// See docs/design/system-administration.md section 7.

// SecretStatus reports that a credential is configured, never anything about
// its value. Not a prefix, not a length, not a hash: each of those narrows a
// search for someone who has the response and wants the secret.
type SecretStatus struct {
	Set bool `json:"set"`
}

func secretStatus(v string) SecretStatus { return SecretStatus{Set: v != ""} }

// RedactedServerConfig is the effective server configuration, safe to show a
// System Administrator.
type RedactedServerConfig struct {
	LogLevel             string `json:"log_level,omitempty"`
	Port                 int    `json:"port"`
	AllowSignup          bool   `json:"allow_signup"`
	LocalLogin           string `json:"local_login,omitempty"`
	CORSOrigin           string `json:"cors_origin,omitempty"`
	WorkspacesDir        string `json:"workspaces_dir,omitempty"`
	DefaultQuotaTier     string `json:"default_quota_tier,omitempty"`
	AccessTokenTTL       string `json:"access_token_ttl,omitempty"`
	RefreshTokenTTL      string `json:"refresh_token_ttl,omitempty"`
	RefreshRotationGrace string `json:"refresh_rotation_grace,omitempty"`
	SessionAbsoluteTTL   string `json:"session_absolute_ttl,omitempty"`
	ShutdownGrace        string `json:"shutdown_grace,omitempty"`

	JWTSecret SecretStatus `json:"jwt_secret"`

	Database     RedactedDBConfig           `json:"database"`
	Storage      RedactedStorageConfig      `json:"storage"`
	Worker       RedactedWorkerConfig       `json:"worker"`
	LLM          RedactedLLMConfig          `json:"llm"`
	Coordination RedactedCoordinationConfig `json:"coordination"`
	OIDC         RedactedOIDCConfig         `json:"oidc"`
	Channels     RedactedChannelsConfig     `json:"channels"`

	// Warnings are configuration states worth an operator's attention. They are
	// not errors — the server is running — and they are computed rather than
	// stored, so the list reflects the process's own view of itself.
	Warnings []string `json:"warnings"`
}

// RedactedDBConfig shows where the database is, never how to open it.
type RedactedDBConfig struct {
	Host string `json:"host,omitempty"`
	Port int    `json:"port,omitempty"`
	User string `json:"user,omitempty"`
	Name string `json:"name,omitempty"`
	// TLS is the effective mode, not the raw setting, so an unset value reads
	// as what the connection actually does.
	TLS      string       `json:"tls"`
	Password SecretStatus `json:"password"`
}

// RedactedStorageConfig shows which backends are in use.
type RedactedStorageConfig struct {
	PersistBackend  string `json:"persist_backend,omitempty"`
	ArtifactBackend string `json:"artifact_backend,omitempty"`
	// MaxArtifactMB is shown because an operator diagnosing a refused upload
	// needs to see the limit that refused it. Zero means the built-in default.
	MaxArtifactMB int `json:"max_artifact_mb,omitempty"`
	// ArtifactPurgeAfterDays is shown for the same reason: an operator asking
	// why a deleted artifact's bytes are still in the bucket is reading for
	// this number. Zero means the next sweep reclaims them.
	ArtifactPurgeAfterDays int          `json:"artifact_purge_after_days,omitempty"`
	MinIOEndpoint          string       `json:"minio_endpoint,omitempty"`
	MinIOBucket            string       `json:"minio_bucket,omitempty"`
	MinIORegion            string       `json:"minio_region,omitempty"`
	MinIOAccessKey         SecretStatus `json:"minio_access_key"`
	MinIOSecretKey         SecretStatus `json:"minio_secret_key"`
}

// RedactedWorkerConfig shows how runs are launched.
type RedactedWorkerConfig struct {
	RunMode      string `json:"run_mode,omitempty"`
	ServerURL    string `json:"server_url,omitempty"`
	RunTokenTTL  string `json:"run_token_ttl,omitempty"`
	RunTimeout   string `json:"run_timeout,omitempty"`
	LLMTransport string `json:"llm_transport,omitempty"`
	LLMModel     string `json:"llm_model,omitempty"`
	K8sNamespace string `json:"k8s_namespace,omitempty"`
	K8sImage     string `json:"k8s_image,omitempty"`
}

// RedactedLLMConfig shows which model this deployment defaults to. The name is
// an identifier rather than a credential; the credentials are in the llm_model
// table and are never served anywhere.
type RedactedLLMConfig struct {
	DefaultModel  string        `json:"default_model,omitempty"`
	ConversationM RedactedModel `json:"conversation_model"`
}

// RedactedModel describes the Tier 1 model without its credential.
type RedactedModel struct {
	Name        string       `json:"name,omitempty"`
	Model       string       `json:"model,omitempty"`
	APIURL      string       `json:"api_url,omitempty"`
	ModelTarget string       `json:"model_target,omitempty"`
	APIKey      SecretStatus `json:"api_key"`
}

// RedactedCoordinationConfig shows how live state is shared across replicas.
// The Redis address is a location, not a credential; the password is reported
// only as configured or not.
type RedactedCoordinationConfig struct {
	Mode          string       `json:"mode,omitempty"`
	RedisAddress  string       `json:"redis_address,omitempty"`
	RedisTLS      bool         `json:"redis_tls,omitempty"`
	RedisPassword SecretStatus `json:"redis_password"`
}

// RedactedOIDCConfig shows how SSO is configured, never the client secret. The
// issuer and client_id are identifiers an operator needs to diagnose a login
// failure, not credentials; the secret is reported only as configured or not.
type RedactedOIDCConfig struct {
	Enabled             bool         `json:"enabled"`
	DisplayName         string       `json:"display_name,omitempty"`
	Issuer              string       `json:"issuer,omitempty"`
	ClientID            string       `json:"client_id,omitempty"`
	Provisioning        string       `json:"provisioning,omitempty"`
	AllowedEmailDomains []string     `json:"allowed_email_domains,omitempty"`
	SessionMaxAge       string       `json:"session_max_age,omitempty"`
	ClientSecret        SecretStatus `json:"client_secret"`
}

// RedactedChannelsConfig shows which chat platforms are connected. A bot
// token is the bot's whole identity, so it is reported only as set or not.
type RedactedChannelsConfig struct {
	TelegramAPIBaseURL string       `json:"telegram_api_base_url,omitempty"`
	TelegramBotToken   SecretStatus `json:"telegram_bot_token"`
}

// Redacted returns the operator-facing view of the configuration.
func (sc ServerConfig) Redacted() RedactedServerConfig {
	out := RedactedServerConfig{
		LogLevel:         sc.LogLevel,
		Port:             sc.Port,
		AllowSignup:      sc.AllowSignup,
		CORSOrigin:       sc.CORSOrigin,
		WorkspacesDir:    sc.WorkspacesDir,
		DefaultQuotaTier: sc.DefaultQuotaTier,
		JWTSecret:        secretStatus(sc.JWTSecret),
		Database: RedactedDBConfig{
			Host:     sc.Database.Host,
			Port:     sc.Database.Port,
			User:     sc.Database.User,
			Name:     sc.Database.Name,
			TLS:      effectiveDBTLS(sc.Database.TLS),
			Password: secretStatus(sc.Database.Password),
		},
		Storage: RedactedStorageConfig{
			PersistBackend:         sc.Storage.PersistBackend,
			ArtifactBackend:        sc.Storage.ArtifactBackend,
			MaxArtifactMB:          sc.Storage.MaxArtifactMB,
			ArtifactPurgeAfterDays: sc.Storage.ArtifactPurgeAfterDays,
			MinIOEndpoint:          sc.Storage.MinIO.Endpoint,
			MinIOBucket:            sc.Storage.MinIO.Bucket,
			MinIORegion:            sc.Storage.MinIO.Region,
			MinIOAccessKey:         secretStatus(sc.Storage.MinIO.AccessKey),
			MinIOSecretKey:         secretStatus(sc.Storage.MinIO.SecretKey),
		},
		Worker: RedactedWorkerConfig{
			RunMode:      sc.Worker.RunMode,
			ServerURL:    sc.Worker.ServerURL,
			LLMTransport: sc.Worker.LLM.Transport,
			LLMModel:     sc.Worker.LLM.Model,
			K8sNamespace: sc.Worker.K8s.Namespace,
			K8sImage:     sc.Worker.K8s.Image,
		},
		LLM: RedactedLLMConfig{
			DefaultModel: sc.LLM.DefaultModel,
			ConversationM: RedactedModel{
				Name:        sc.Conversation.Model.Name,
				Model:       sc.Conversation.Model.Model,
				APIURL:      sc.Conversation.Model.APIURL,
				ModelTarget: sc.Conversation.ModelTarget,
				APIKey:      secretStatus(sc.Conversation.Model.APIKey),
			},
		},
		Coordination: RedactedCoordinationConfig{
			Mode:          sc.Coordination.mode(),
			RedisAddress:  sc.Coordination.Redis.Address,
			RedisTLS:      sc.Coordination.Redis.TLS,
			RedisPassword: secretStatus(sc.Coordination.Redis.Password),
		},
		OIDC: RedactedOIDCConfig{
			Enabled:             sc.OIDC.Enabled,
			DisplayName:         sc.OIDC.DisplayName,
			Issuer:              sc.OIDC.Issuer,
			ClientID:            sc.OIDC.ClientID,
			Provisioning:        sc.OIDC.provisioning(),
			AllowedEmailDomains: sc.OIDC.AllowedEmailDomains,
			SessionMaxAge:       sc.OIDC.sessionMaxAge().String(),
			ClientSecret:        secretStatus(sc.OIDC.ClientSecret),
		},
		Channels: RedactedChannelsConfig{
			TelegramAPIBaseURL: sc.Channels.Telegram.APIBaseURL,
			TelegramBotToken:   secretStatus(sc.Channels.Telegram.BotToken),
		},
		LocalLogin: sc.localLogin(),
	}
	if sc.AccessTokenTTL > 0 {
		out.AccessTokenTTL = sc.AccessTokenTTL.String()
	}
	if sc.RefreshTokenTTL > 0 {
		out.RefreshTokenTTL = sc.RefreshTokenTTL.String()
	}
	if sc.RefreshRotationGrace > 0 {
		out.RefreshRotationGrace = sc.RefreshRotationGrace.String()
	}
	if sc.SessionAbsoluteTTL > 0 {
		out.SessionAbsoluteTTL = sc.SessionAbsoluteTTL.String()
	}
	if sc.ShutdownGrace > 0 {
		out.ShutdownGrace = sc.ShutdownGrace.String()
	}
	if sc.Worker.RunTokenTTL > 0 {
		out.Worker.RunTokenTTL = sc.Worker.RunTokenTTL.String()
	}
	if sc.Worker.RunTimeout > 0 {
		out.Worker.RunTimeout = sc.Worker.RunTimeout.String()
	}
	out.Warnings = sc.configWarnings()
	return out
}

func effectiveDBTLS(mode string) string {
	if mode == "" {
		return DefaultDBTLSMode
	}
	return mode
}

// configWarnings lists configuration states an operator should know about.
//
// Every entry is a documented trade-off somewhere else in the project; this is
// where a deployment finds out which of them apply to it, without reading the
// documentation for all of them.
func (sc ServerConfig) configWarnings() []string {
	warnings := []string{}
	if sc.AllowSignup {
		warnings = append(warnings, "allow_signup is on: anyone who can reach the server can create an account")
	}
	if sc.Worker.RunMode != "k8s_job" {
		warnings = append(warnings, "worker.run_mode is not k8s_job: runs execute as child processes of the server under its uid, so a task run shares the server's trust domain rather than being separated from it")
	}
	if sc.Worker.RunTokenTTL > 0 && sc.Worker.RunTimeout > sc.Worker.RunTokenTTL {
		warnings = append(warnings, "worker.run_timeout is longer than worker.run_token_ttl: a run can outlive its credential and then cannot report an outcome")
	}
	if sc.Storage.PersistBackend == "" || sc.Storage.PersistBackend == "local_fs" {
		warnings = append(warnings, "storage.persist_backend is local_fs: run output lives on the server's disk and is lost with the pod")
	}
	if sc.localLogin() == LocalLoginOff && !sc.OIDC.Enabled {
		warnings = append(warnings, "local_login is off and oidc is disabled: no one can sign in — configure oidc or widen local_login")
	}
	if sc.OIDC.Enabled && sc.localLogin() == LocalLoginOff {
		warnings = append(warnings, "local_login is off with oidc enabled: there is no break-glass login if the IdP is unreachable — local_login: system_admins keeps operators able to sign in")
	}
	if sc.Channels.Telegram.BotToken != "" && sc.PublicBaseURL == "" {
		warnings = append(warnings, "channels.telegram is on without public_base_url: the bot cannot send links, so people link their chat account by typing its code in the Portal")
	}
	return warnings
}
