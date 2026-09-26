package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
)

// TestServerDBConfig_DSNUsesTLS covers the connection a deployment makes to a
// database it already runs. Most managed MySQL either requires TLS or is
// reached across a network where the credentials must not travel in the clear,
// and until this existed the DSN had no way to ask for it.
func TestServerDBConfig_DSNUsesTLS(t *testing.T) {
	base := ServerDBConfig{Host: "db.example", Port: 3306, User: "u", Password: "p", Name: "buildmax"}

	t.Run("defaults to preferred", func(t *testing.T) {
		// preferred upgrades whenever the server offers TLS and behaves as
		// before against one that does not, so it introduces no failure mode a
		// plaintext connection did not already have.
		if got := base.DSN(); !strings.Contains(got, "tls="+DefaultDBTLSMode) {
			t.Errorf("DSN = %q, want tls=%s", got, DefaultDBTLSMode)
		}
	})

	t.Run("an explicit mode is passed through", func(t *testing.T) {
		for _, mode := range []string{"true", "skip-verify", "false"} {
			cfg := base
			cfg.TLS = mode
			if got := cfg.DSN(); !strings.Contains(got, "tls="+mode) {
				t.Errorf("DSN for %q = %q", mode, got)
			}
		}
	})

	t.Run("keeps the parameters the driver already needed", func(t *testing.T) {
		got := base.DSN()
		for _, want := range []string{"charset=utf8mb4", "parseTime=True"} {
			if !strings.Contains(got, want) {
				t.Errorf("DSN = %q, missing %q", got, want)
			}
		}
	})
}

// TestSessionLifetimesDefaultToTheIdentityDomain guards the access token's
// short default. A loader default of a week once shadowed the 15 minutes the
// identity domain and the documentation both promised, which is the replay
// window of a leaked token.
func TestSessionLifetimesDefaultToTheIdentityDomain(t *testing.T) {
	t.Setenv(EnvKeyBuildmaxHome, t.TempDir())

	cfg, err := LoadServerConfig()
	if err != nil {
		t.Fatalf("LoadServerConfig: %v", err)
	}
	if cfg.AccessTokenTTL != 15*time.Minute {
		t.Errorf("access_token_ttl defaults to %v, want 15m", cfg.AccessTokenTTL)
	}
	if cfg.RefreshTokenTTL != coreidentity.RefreshTokenTTLDefault {
		t.Errorf("refresh_token_ttl defaults to %v", cfg.RefreshTokenTTL)
	}
	if cfg.RefreshRotationGrace != coreidentity.RefreshRotationGraceDefault {
		t.Errorf("refresh_rotation_grace defaults to %v", cfg.RefreshRotationGrace)
	}
}

// TestStorageHasNoCredentialOrEndpointDefaults guards what makes the AWS path
// expressible at all.
//
// These four used to default to a local MinIO and its development credentials,
// so "unset" was unreachable: a deployment omitting them got localhost:9000 as
// user "minio" rather than the SDK's endpoint resolution and credential chain.
// A default here is not a convenience, it is a deployment silently pointed
// somewhere it did not ask for.
func TestStorageHasNoCredentialOrEndpointDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvKeyBuildmaxHome, dir)
	for _, name := range []string{
		EnvKeyBuildmaxMinIOAccessKey,
		EnvKeyBuildmaxMinIOSecretKey,
	} {
		t.Setenv(name, "")
	}
	if err := os.WriteFile(filepath.Join(dir, "server.yaml"), []byte("storage:\n  persist_backend: minio\n"), 0o600); err != nil {
		t.Fatalf("write server.yaml: %v", err)
	}

	cfg, err := LoadServerConfig()
	if err != nil {
		t.Fatalf("LoadServerConfig: %v", err)
	}
	if got := cfg.Storage.MinIO.AccessKey; got != "" {
		t.Errorf("access_key defaults to %q; a credential must never have a default", got)
	}
	if got := cfg.Storage.MinIO.SecretKey; got != "" {
		t.Errorf("secret_key defaults to %q; a credential must never have a default", got)
	}
	if got := cfg.Storage.MinIO.Endpoint; got != "" {
		t.Errorf("endpoint defaults to %q; empty is what selects AWS endpoint resolution", got)
	}
	if got := cfg.Storage.MinIO.Region; got != "" {
		t.Errorf("region defaults to %q; empty lets the SDK take it from the environment", got)
	}
}
