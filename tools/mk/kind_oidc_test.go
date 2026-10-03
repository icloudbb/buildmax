package main

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// clearKindOIDCEnv pins the variables .local/env may set, so a contributor's
// own IdP does not change what these tests see.
func clearKindOIDCEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{envKindOIDCIssuer, envKindOIDCClientID, envKindOIDCName, envKindOIDCDomains, envOIDCClientSecret} {
		t.Setenv(key, "")
	}
}

func renderedOIDC(t *testing.T) map[string]any {
	t.Helper()
	path, cleanup, err := renderKindSmokeConfig("deployment/smoke/server.kind.yaml")
	if err != nil {
		t.Fatalf("renderKindSmokeConfig: %v", err)
	}
	t.Cleanup(cleanup)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		OIDC map[string]any `yaml:"oidc"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("rendered config is not YAML: %v", err)
	}
	return cfg.OIDC
}

// The mock's issuer names kind's TLS port, which an ephemeral cluster moves; a
// stale port would make every SSO sign-in fail discovery.
func TestRenderKindConfigPutsTheMockIssuerOnThisClustersTLSPort(t *testing.T) {
	t.Chdir("../..")
	clearKindOIDCEnv(t)
	t.Setenv("BUILDMAX_KIND_TLS_PORT", "18543")

	oidc := renderedOIDC(t)
	if oidc["issuer"] != "https://"+kindOIDCServiceDNS+":18543" || oidc["client_id"] != kindOIDCDefaultClient {
		t.Errorf("oidc = %v; want the mock on port 18543", oidc)
	}

	manifest, err := os.ReadFile(kindOIDCManifest)
	if err != nil {
		t.Fatal(err)
	}
	rendered := renderKindOIDCManifest(string(manifest))
	if !strings.Contains(rendered, "value: https://"+kindOIDCServiceDNS+":18543") || !strings.Contains(rendered, "port: 18543") {
		t.Errorf("the mock's manifest does not agree with the server on port 18543:\n%s", rendered)
	}
	if strings.Contains(rendered, "8443") {
		t.Errorf("the mock's manifest still names the default port:\n%s", rendered)
	}
}

func TestRenderKindConfigPointsAtARealIdPFromTheEnvironment(t *testing.T) {
	t.Chdir("../..")
	clearKindOIDCEnv(t)
	t.Setenv(envKindOIDCIssuer, "https://example.okta.com")
	t.Setenv(envKindOIDCClientID, "0oaExample")
	t.Setenv(envKindOIDCName, `Okta "dev"`)
	t.Setenv(envKindOIDCDomains, "example.com, example.org")

	oidc := renderedOIDC(t)
	if oidc["issuer"] != "https://example.okta.com" || oidc["client_id"] != "0oaExample" || oidc["display_name"] != `Okta "dev"` {
		t.Errorf("oidc = %v; want the real IdP", oidc)
	}
	domains, _ := oidc["allowed_email_domains"].([]any)
	if len(domains) != 2 || domains[0] != "example.com" || domains[1] != "example.org" {
		t.Errorf("allowed_email_domains = %v", oidc["allowed_email_domains"])
	}
	if oidc["enabled"] != true {
		t.Errorf("SSO was not left enabled: %v", oidc)
	}
}

// A real IdP issued its client secret; generating one would only fail the token
// exchange later, with a much less useful error.
func TestARealIdPRequiresItsSecretAndDomains(t *testing.T) {
	t.Chdir("../..")
	clearKindOIDCEnv(t)
	t.Setenv(envKindOIDCIssuer, "https://example.okta.com")
	t.Setenv(envKindOIDCClientID, "0oaExample")

	if _, err := kindOIDCClientSecret(); err == nil || !strings.Contains(err.Error(), envOIDCClientSecret) {
		t.Errorf("secret error = %v; want it to name %s", err, envOIDCClientSecret)
	}
	if _, _, err := renderKindSmokeConfig("deployment/smoke/server.kind.yaml"); err == nil || !strings.Contains(err.Error(), envKindOIDCDomains) {
		t.Errorf("render error = %v; want it to name %s", err, envKindOIDCDomains)
	}
}
