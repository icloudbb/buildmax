package config

import "strings"

// This file is the single source of truth for environment variable names and the few
// functions that read them. Most configuration now lives in settings.yaml (local) or
// server.yaml (server/worker). Only bootstrap-level values that cannot be in a file
// remain as environment variables.

// Environment variable key names.
const (
	// BUILDMAX_HOME — path to the data directory; tells every binary where to find its
	// config files (settings.yaml, server.yaml). Must remain an env var because the
	// config files cannot be located until this path is known.
	EnvKeyBuildmaxHome = "BUILDMAX_HOME"

	// BUILDMAX_SERVER_URL — address this process uses to reach buildmax-server.
	// It overrides settings.yaml server_url for CLI/Desktop and server.yaml
	// worker.server_url for workers, so one deployed image can target a stage or
	// production control plane without rewriting its config file.
	EnvKeyBuildmaxServerURL = "BUILDMAX_SERVER_URL"

	// BUILDMAX_JWT_SECRET — optional override for jwt_secret in server.yaml.
	// In production, inject via env var (Kubernetes Secret, Docker secret) instead of
	// storing the value in the YAML file on disk.
	EnvKeyBuildmaxJWTSecret = "BUILDMAX_JWT_SECRET"

	// BUILDMAX_PUBLIC_BASE_URL — optional override for public_base_url in
	// server.yaml: the externally reachable origin at which people open BuildMax.
	// Artifact share links are rendered against it. It is distinct from
	// BUILDMAX_SERVER_URL, which is the address a process uses to reach the
	// server, not one the server advertises for itself.
	EnvKeyBuildmaxPublicBaseURL = "BUILDMAX_PUBLIC_BASE_URL"

	// BUILDMAX_RUN_TOKEN — the credential one task run presents to the managed LLM
	// gateway. The scheduler mints it per run and puts it in the worker process or
	// Job pod; nothing inherits it, which is why it is not marked WorkerNeeds
	// below. See docs/design/worker-run-token.md.
	EnvKeyBuildmaxRunToken = "BUILDMAX_RUN_TOKEN"

	// BUILDMAX_BRIDGE_SOCK — path to the run bridge's Unix socket, set inside a
	// worker run so a subprocess (the buildmax CLI launched through the Bash
	// tool) can reach the worker API without ever holding the run token. The
	// bridge injects the token and forwards to the worker listener. Generated per
	// run, so it is never a WorkerNeeds launch variable. See
	// docs/design/agent-bridge-cli.md.
	EnvKeyBuildmaxBridgeSock = "BUILDMAX_BRIDGE_SOCK"

	// BUILDMAX_TASK_RUN_ID — the id of the run a subprocess belongs to, set
	// alongside BUILDMAX_BRIDGE_SOCK so the CLI can address the run's own worker
	// routes. The id is not a secret; the run token, which is, stays in the
	// worker process behind the bridge.
	EnvKeyBuildmaxTaskRunID = "BUILDMAX_TASK_RUN_ID"

	// BUILDMAX_RUN_INTERRUPT_GRACE — how long a worker asked to stop may spend
	// uploading what its run produced and reporting the outcome. The scheduler
	// sets it from the server's own shutdown budget so the two windows nest;
	// unset, the worker uses its own default. See
	// docs/design/graceful-shutdown.md §6.3.
	EnvKeyBuildmaxRunInterruptGrace = "BUILDMAX_RUN_INTERRUPT_GRACE"

	// BUILDMAX_CREDENTIAL_STORE — set to "file" to keep a login's tokens in
	// auth.json instead of the operating system's credential store. It is for a
	// machine whose credential store is present but unusable, which the probe
	// in internal/interface/auth cannot tell from a working one.
	EnvKeyBuildmaxCredentialStore = "BUILDMAX_CREDENTIAL_STORE"

	// Test only.
	EnvKeyBuildmaxTestDSN = "BUILDMAX_TEST_DSN"

	// BUILDMAX_CACHE_QUALIFY_* name the provider the prompt-cache qualification
	// suite runs against. The suite calls a real, paid provider and is not part
	// of any check; unset, it skips. See docs/design/prompt-cache-control.md
	// section 9, phase 4.
	EnvKeyBuildmaxCacheQualifyProvider = "BUILDMAX_CACHE_QUALIFY_PROVIDER"
	EnvKeyBuildmaxCacheQualifyModel    = "BUILDMAX_CACHE_QUALIFY_MODEL"
	EnvKeyBuildmaxCacheQualifyAPIKey   = "BUILDMAX_CACHE_QUALIFY_API_KEY"
	EnvKeyBuildmaxCacheQualifyBaseURL  = "BUILDMAX_CACHE_QUALIFY_BASE_URL"
	// BUILDMAX_CACHE_QUALIFY_SLOW opts into the scenarios that must wait out a
	// retention window. They take minutes of wall clock, so they are off by
	// default rather than silently making a run look hung.
	EnvKeyBuildmaxCacheQualifySlow = "BUILDMAX_CACHE_QUALIFY_SLOW"
)

// Note: EnvKeyBuildmaxSandboxEnabled lives in sandbox.go alongside the
// resolver that reads it (per-subsystem env constant).

// EnvVar describes one environment variable used by BuildMax.
type EnvVar struct {
	Name        string
	Default     string
	Description string
	// WorkerNeeds marks a variable that a task-run worker reads.
	//
	// A worker executes model-chosen code, so a credential it does not read is
	// exposure with no purpose behind it. Only the marked variables are handed
	// to a worker process or Job pod. internal/bootstrap/worker.go is the
	// authority on what that set is: if a worker starts reading a new variable,
	// mark it here, and it will reach the worker. Nothing else will.
	WorkerNeeds bool
	// DirectLLMOnly narrows WorkerNeeds to deployments whose task runs call a
	// provider themselves. A managed run reaches models through the server and
	// holds no provider credential, so passing one would hand a model-driven
	// process a key it has no use for.
	DirectLLMOnly bool
}

var envVars = []EnvVar{
	{Name: EnvKeyBuildmaxHome, Default: "~/.buildmax", Description: "Application data directory; locates settings.yaml and server.yaml", WorkerNeeds: true},
	{Name: EnvKeyBuildmaxServerURL, Description: "Override for settings.yaml server_url and server.yaml worker.server_url", WorkerNeeds: true},
	{Name: EnvKeyBuildmaxJWTSecret, Description: "Override for jwt_secret in server.yaml; inject at deploy time in production"},
	{Name: EnvKeyBuildmaxPublicBaseURL, Description: "Override for public_base_url in server.yaml; the externally reachable origin artifact share links are built from"},
	{Name: EnvKeyBuildmaxDatabasePassword, Description: "Override for database.password in server.yaml"},
	{Name: EnvKeyBuildmaxMinIOAccessKey, Description: "Override for storage.minio.access_key in server.yaml", WorkerNeeds: true},
	{Name: EnvKeyBuildmaxMinIOSecretKey, Description: "Override for storage.minio.secret_key in server.yaml", WorkerNeeds: true},
	{Name: EnvKeyBuildmaxConversationAPIKey, Description: "Override for conversation.model.api_key in server.yaml", WorkerNeeds: true, DirectLLMOnly: true},
	{Name: EnvKeyBuildmaxCoordinationRedisPassword, Description: "Override for coordination.redis.password in server.yaml; the multi-replica coordination backend"},
	{Name: EnvKeyBuildmaxOIDCClientSecret, Description: "Override for oidc.client_secret in server.yaml; the confidential OIDC client's secret, injected at deploy time"},
	{Name: EnvKeyBuildmaxCORSOrigin, Description: "Override for cors_origin in server.yaml; set where the Portal's host port is chosen"},
	// The three model-selection overrides are read by the server, which decides
	// the transport and resolves the catalog, and delivered to each worker per
	// run through the task-run API — never inherited as environment. They stay
	// unmarked so a worker process is not handed a knob it does not read.
	{Name: EnvKeyBuildmaxWorkerLLMTransport, Description: "Override for worker.llm.transport (direct or buildmax); flips task runs between a direct provider call and the managed gateway"},
	{Name: EnvKeyBuildmaxLLMDefaultModel, Description: "Override for llm.default_model; the catalog model name managed runs and unqualified callers resolve to"},
	{Name: EnvKeyBuildmaxConversationModelTarget, Description: "Override for conversation.model_target; a catalog model name or ID for Tier 1 conversations"},
	// Deliberately not WorkerNeeds: a worker reads this, but it is injected per
	// run by the scheduler, never inherited from the server. Leaving it unmarked
	// is what strips a stale value the server happens to be holding, so the only
	// token a worker can find is the one minted for its own run.
	{Name: EnvKeyBuildmaxRunToken, Description: "Per-run credential for the managed LLM gateway; minted by the scheduler, not set by an operator"},
	// Deliberately not WorkerNeeds, for the same reason as the run token: the
	// scheduler sets it per dispatch from its own shutdown budget, and a stale
	// value inherited from the server would describe the wrong window.
	{Name: EnvKeyBuildmaxRunInterruptGrace, Description: "How long a worker asked to stop may spend reporting what its run produced; set by the scheduler, not by an operator"},
	// Deliberately not WorkerNeeds: a worker holds a run token, never a login,
	// so it has no credentials to store either way.
	{Name: EnvKeyBuildmaxCredentialStore, Description: "Set to \"file\" to keep CLI and Desktop login tokens in auth.json rather than the OS credential store"},
	{Name: EnvKeyBuildmaxTestDSN, Description: "MySQL DSN for store integration tests; unset skips those tests"},
	{Name: EnvKeyBuildmaxCacheQualifyProvider, Description: "Provider for the prompt-cache qualification suite; unset skips it"},
	{Name: EnvKeyBuildmaxCacheQualifyModel, Description: "Model identifier for the prompt-cache qualification suite"},
	{Name: EnvKeyBuildmaxCacheQualifyAPIKey, Description: "Credential for the prompt-cache qualification suite; a real, paid provider"},
	{Name: EnvKeyBuildmaxCacheQualifyBaseURL, Description: "Base URL override for the prompt-cache qualification suite"},
	{Name: EnvKeyBuildmaxCacheQualifySlow, Description: "Include the qualification scenarios that wait out a retention window (1/true/yes/on)"},
	{Name: EnvKeyBuildmaxSandboxEnabled, Description: "Override sandbox.enabled in settings; values: 1/true/yes/on or 0/false/no/off", WorkerNeeds: true},
	// WorkerNeeds: a k8s_job worker reads this straight off its image's own ENV
	// (Kubernetes never strips that), but a local_process worker is exec'd with
	// FilterWorkerEnv's filtered slice, not the server's raw environment -- the
	// same image ENV never reached it without this entry, silently resolving
	// WorkerSandboxSurface to the CLI baseline and leaving that worker's Bash
	// commands unsandboxed on every host FilterWorkerEnv runs on.
	{Name: EnvKeyBuildmaxSandboxBackendInstalled, Description: "Marks an image as installing the sandbox OS backend (bwrap+socat); set via Dockerfile ENV, not by an operator", WorkerNeeds: true},
	{Name: EnvKeyBuildmaxTraceDisabled, Description: "Disable durable run traces when truthy (1/true/yes/on); traces are on by default", WorkerNeeds: true},
}

// EnvVars returns every environment variable read by BuildMax binaries.
func EnvVars() []EnvVar { return append([]EnvVar(nil), envVars...) }

// WorkerNeedsEnv reports whether a task-run worker reads name.
//
// managedLLM says whether this deployment's task runs reach models through the
// server. When they do, provider credentials are withheld: the run has a
// gateway credential instead and never calls a provider itself.
//
// It answers false for an unrecognized BUILDMAX_ variable as well as for a
// known one a worker does not use, so a variable added to the server without a
// thought for workers stays on the server.
func WorkerNeedsEnv(name string, managedLLM bool) bool {
	for _, v := range envVars {
		if v.Name == name {
			return v.WorkerNeeds && !(managedLLM && v.DirectLLMOnly)
		}
	}
	return false
}

// WorkerEnvKeys returns the environment variable names a worker is given, in
// declaration order.
func WorkerEnvKeys(managedLLM bool) []string {
	var out []string
	for _, v := range envVars {
		if WorkerNeedsEnv(v.Name, managedLLM) {
			out = append(out, v.Name)
		}
	}
	return out
}

// FilterWorkerEnv returns environ with the BUILDMAX_ variables a worker does
// not read removed.
//
// Everything outside the BUILDMAX_ prefix passes through untouched: PATH, HOME
// and the rest are what let the worker binary run at all. Use this when handing
// a local worker process the server's environment.
func FilterWorkerEnv(environ []string, managedLLM bool) []string {
	out := make([]string, 0, len(environ))
	for _, e := range environ {
		name, _, found := strings.Cut(e, "=")
		if found && strings.HasPrefix(name, "BUILDMAX_") && !WorkerNeedsEnv(name, managedLLM) {
			continue
		}
		out = append(out, e)
	}
	return out
}
