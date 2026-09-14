package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// baseOIDC is a valid enabled block; each test perturbs one field so a failure
// names the rule that broke rather than a whole invalid config.
func baseOIDC() ServerConfig {
	return ServerConfig{
		PublicBaseURL: "https://buildmax.example.com",
		LocalLogin:    LocalLoginSystemAdmins,
		OIDC: ServerOIDCConfig{
			Enabled:             true,
			Issuer:              "https://example.okta.com",
			ClientID:            "0oaClient",
			ClientSecret:        "shh",
			Provisioning:        OIDCProvisioningJIT,
			AllowedEmailDomains: []string{"example.com"},
		},
	}
}

func TestValidateAuth(t *testing.T) {
	t.Run("a valid enabled block passes", func(t *testing.T) {
		if err := baseOIDC().ValidateAuth(); err != nil {
			t.Fatalf("ValidateAuth: %v", err)
		}
	})

	t.Run("disabled OIDC ignores its own fields", func(t *testing.T) {
		// An operator drafting a block, not yet turned on, must not be blocked
		// from starting — even with an http issuer and no domains.
		sc := ServerConfig{OIDC: ServerOIDCConfig{Issuer: "http://nope", Provisioning: OIDCProvisioningJIT}}
		if err := sc.ValidateAuth(); err != nil {
			t.Fatalf("disabled OIDC should not validate its fields: %v", err)
		}
	})

	t.Run("an http issuer is refused", func(t *testing.T) {
		sc := baseOIDC()
		sc.OIDC.Issuer = "http://example.okta.com"
		if err := sc.ValidateAuth(); err == nil || !strings.Contains(err.Error(), "issuer must be https") {
			t.Fatalf("err = %v, want an https-issuer refusal", err)
		}
	})

	t.Run("a missing client_id is refused", func(t *testing.T) {
		sc := baseOIDC()
		sc.OIDC.ClientID = ""
		if err := sc.ValidateAuth(); err == nil || !strings.Contains(err.Error(), "client_id") {
			t.Fatalf("err = %v, want a client_id refusal", err)
		}
	})

	t.Run("jit with no allowed domains is refused", func(t *testing.T) {
		sc := baseOIDC()
		sc.OIDC.AllowedEmailDomains = nil
		if err := sc.ValidateAuth(); err == nil || !strings.Contains(err.Error(), "allowed_email_domains") {
			t.Fatalf("err = %v, want an unbounded-JIT refusal", err)
		}
	})

	t.Run("existing_only needs no domains", func(t *testing.T) {
		sc := baseOIDC()
		sc.OIDC.Provisioning = OIDCProvisioningExistingOnly
		sc.OIDC.AllowedEmailDomains = nil
		if err := sc.ValidateAuth(); err != nil {
			t.Fatalf("existing_only with no domains should pass: %v", err)
		}
	})

	t.Run("an unknown provisioning mode is refused", func(t *testing.T) {
		sc := baseOIDC()
		sc.OIDC.Provisioning = "auto"
		if err := sc.ValidateAuth(); err == nil || !strings.Contains(err.Error(), "provisioning") {
			t.Fatalf("err = %v, want a provisioning refusal", err)
		}
	})

	t.Run("enabled OIDC requires a public base URL", func(t *testing.T) {
		sc := baseOIDC()
		sc.PublicBaseURL = ""
		if err := sc.ValidateAuth(); err == nil || !strings.Contains(err.Error(), "public_base_url") {
			t.Fatalf("err = %v, want a public_base_url refusal", err)
		}
	})

	t.Run("a non-loopback http base URL is refused", func(t *testing.T) {
		sc := baseOIDC()
		sc.PublicBaseURL = "http://buildmax.example.com"
		if err := sc.ValidateAuth(); err == nil || !strings.Contains(err.Error(), "https") {
			t.Fatalf("err = %v, want an https base-URL refusal", err)
		}
	})

	t.Run("a loopback http base URL is allowed for development", func(t *testing.T) {
		sc := baseOIDC()
		sc.PublicBaseURL = "http://127.0.0.1:5678"
		if err := sc.ValidateAuth(); err != nil {
			t.Fatalf("loopback http should be allowed: %v", err)
		}
	})

	t.Run("an unknown local_login mode is refused", func(t *testing.T) {
		sc := baseOIDC()
		sc.LocalLogin = "admins"
		if err := sc.ValidateAuth(); err == nil || !strings.Contains(err.Error(), "local_login") {
			t.Fatalf("err = %v, want a local_login refusal", err)
		}
	})

	t.Run("allow_signup contradicts a narrowed local_login", func(t *testing.T) {
		sc := ServerConfig{AllowSignup: true, LocalLogin: LocalLoginSystemAdmins}
		if err := sc.ValidateAuth(); err == nil || !strings.Contains(err.Error(), "allow_signup") {
			t.Fatalf("err = %v, want an allow_signup contradiction", err)
		}
	})
}

// TestServerConfigOIDCDefaults pins the defaults a bare server.yaml gets, so the
// safe values (native login for everyone, JIT bounded by an explicit list, a
// 12h SSO ceiling) do not depend on an operator writing them out.
func TestServerConfigOIDCDefaults(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvKeyBuildmaxHome, dir)
	t.Setenv(EnvKeyBuildmaxOIDCClientSecret, "")
	if err := os.WriteFile(filepath.Join(dir, "server.yaml"), []byte("port: 5678\n"), 0o600); err != nil {
		t.Fatalf("write server.yaml: %v", err)
	}
	cfg, err := LoadServerConfig()
	if err != nil {
		t.Fatalf("LoadServerConfig: %v", err)
	}
	if cfg.LocalLoginMode() != LocalLoginAll {
		t.Errorf("local_login default = %q, want %q", cfg.LocalLoginMode(), LocalLoginAll)
	}
	if cfg.OIDC.Enabled {
		t.Error("oidc.enabled defaults on; a deployment that names no IdP must get native login only")
	}
	if got := cfg.OIDC.provisioning(); got != OIDCProvisioningJIT {
		t.Errorf("provisioning default = %q, want %q", got, OIDCProvisioningJIT)
	}
	if got := cfg.OIDC.sessionMaxAge(); got != OIDCSessionMaxAgeDefault {
		t.Errorf("session_max_age default = %v, want %v", got, OIDCSessionMaxAgeDefault)
	}
}

// TestOIDCClientSecretFromEnv covers injecting the secret at deploy time rather
// than writing it to server.yaml, the same pattern as jwt_secret.
func TestOIDCClientSecretFromEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(EnvKeyBuildmaxHome, dir)
	t.Setenv(EnvKeyBuildmaxOIDCClientSecret, "from-the-environment")
	if err := os.WriteFile(filepath.Join(dir, "server.yaml"), []byte("oidc:\n  enabled: true\n"), 0o600); err != nil {
		t.Fatalf("write server.yaml: %v", err)
	}
	cfg, err := LoadServerConfig()
	if err != nil {
		t.Fatalf("LoadServerConfig: %v", err)
	}
	if cfg.OIDC.ClientSecret != "from-the-environment" {
		t.Errorf("client_secret = %q, want the env value", cfg.OIDC.ClientSecret)
	}
}

// TestRedactedOIDCHidesTheSecret is the one that matters for the admin view: the
// issuer and client id are diagnostic identifiers and are shown, but the secret
// is reported only as configured.
func TestRedactedOIDCHidesTheSecret(t *testing.T) {
	sc := baseOIDC()
	sc.OIDC.DisplayName = "Okta"
	sc.OIDC.SessionMaxAge = 8 * time.Hour
	r := sc.Redacted()

	if r.OIDC.Issuer != sc.OIDC.Issuer || r.OIDC.ClientID != sc.OIDC.ClientID {
		t.Errorf("issuer/client_id not surfaced: %+v", r.OIDC)
	}
	if !r.OIDC.ClientSecret.Set {
		t.Error("a configured client secret should report Set")
	}
	if r.OIDC.SessionMaxAge != (8 * time.Hour).String() {
		t.Errorf("session_max_age = %q, want %q", r.OIDC.SessionMaxAge, (8 * time.Hour).String())
	}
	// The one assertion the whole redaction file exists for: no path in the
	// operator-facing view carries the secret's value.
	encoded, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal redacted config: %v", err)
	}
	blob := strings.ToLower(string(encoded))
	if strings.Contains(blob, "shh") {
		t.Errorf("the client secret value leaked into the redacted view: %s", blob)
	}
	if r.LocalLogin != LocalLoginSystemAdmins {
		t.Errorf("local_login = %q, want %q", r.LocalLogin, LocalLoginSystemAdmins)
	}
}

// TestConfigWarnsWhenNobodyCanSignIn covers the lockout an operator most wants
// to be told about before it strands them.
func TestConfigWarnsWhenNobodyCanSignIn(t *testing.T) {
	sc := ServerConfig{LocalLogin: LocalLoginOff}
	if !hasWarning(sc.Redacted().Warnings, "no one can sign in") {
		t.Errorf("no lockout warning in %v", sc.Redacted().Warnings)
	}

	sso := baseOIDC()
	sso.LocalLogin = LocalLoginOff
	if !hasWarning(sso.Redacted().Warnings, "break-glass") {
		t.Errorf("no break-glass warning in %v", sso.Redacted().Warnings)
	}
}

func hasWarning(warnings []string, substr string) bool {
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}
