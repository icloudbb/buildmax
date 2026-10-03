package main

import (
	"fmt"
	"os"
	"strings"
)

// kind signs in through SSO next to password and login-code sign-in. By default
// the IdP is deployment/smoke/mock-oidc, so the whole flow runs offline; the
// BUILDMAX_KIND_OIDC_* variables (set them in .local/env) point the same
// deployment at a real IdP such as an Okta developer tenant instead, which is
// how a provider is qualified by hand.

// kindOIDCServiceDNS is the mock's Service name, and the issuer's host. Server
// pods resolve it natively; a browser maps it to 127.0.0.1.
const kindOIDCServiceDNS = "buildmax-smoke-oidc.buildmax.svc.cluster.local"

const (
	kindOIDCManifest = "deployment/smoke/mock-oidc.kind.yaml"
	// The committed mock issuer, on the default TLS port. Both the server config
	// and the mock's manifest spell it this way.
	kindOIDCDefaultIssuer = "https://" + kindOIDCServiceDNS + ":" + defaultKindTLSPort
	kindOIDCDefaultClient = "buildmax-kind"
	kindOIDCDefaultName   = "Mock IdP"
	kindOIDCDefaultDomain = "buildmax.local"

	envKindOIDCIssuer   = "BUILDMAX_KIND_OIDC_ISSUER"
	envKindOIDCClientID = "BUILDMAX_KIND_OIDC_CLIENT_ID"
	envKindOIDCName     = "BUILDMAX_KIND_OIDC_DISPLAY_NAME"
	envKindOIDCDomains  = "BUILDMAX_KIND_OIDC_ALLOWED_DOMAINS"
	envOIDCClientSecret = "BUILDMAX_OIDC_CLIENT_SECRET"
)

// kindOIDCIssuer is the mock's issuer on this cluster's TLS port.
func kindOIDCIssuer() string {
	return "https://" + kindOIDCServiceDNS + ":" + kindTLSPort()
}

// kindExternalOIDC reports whether a real IdP replaces the mock.
func kindExternalOIDC() bool {
	return strings.TrimSpace(os.Getenv(envKindOIDCIssuer)) != ""
}

// kindOIDCClientSecret is the secret both sides share. A real IdP issued it, so
// it must come from the environment; the mock accepts whatever it is given.
func kindOIDCClientSecret() (string, error) {
	if kindExternalOIDC() {
		secret := os.Getenv(envOIDCClientSecret)
		if secret == "" {
			return "", fmt.Errorf("%s is set but %s is not; put the client secret your IdP issued in .local/env", envKindOIDCIssuer, envOIDCClientSecret)
		}
		return secret, nil
	}
	return randomHex(32)
}

// renderKindOIDCConfig rewrites the committed mock oidc block in a server.yaml
// for this cluster: the mock's issuer on this cluster's TLS port, or the real
// IdP named by the environment. A config without the block is left alone.
func renderKindOIDCConfig(content string) (string, error) {
	issuerLine := "issuer: " + kindOIDCDefaultIssuer
	if !strings.Contains(content, issuerLine) {
		return content, nil
	}
	if !kindExternalOIDC() {
		return strings.Replace(content, issuerLine, "issuer: "+kindOIDCIssuer(), 1), nil
	}
	clientID := strings.TrimSpace(os.Getenv(envKindOIDCClientID))
	if clientID == "" {
		return "", fmt.Errorf("%s is set but %s is not", envKindOIDCIssuer, envKindOIDCClientID)
	}
	name := strings.TrimSpace(os.Getenv(envKindOIDCName))
	if name == "" {
		name = "single sign-on"
	}
	var domains []string
	for d := range strings.SplitSeq(os.Getenv(envKindOIDCDomains), ",") {
		if d = strings.TrimSpace(d); d != "" {
			domains = append(domains, d)
		}
	}
	if len(domains) == 0 {
		return "", fmt.Errorf("%s is set but %s is not; name the email domains that may be provisioned, comma-separated", envKindOIDCIssuer, envKindOIDCDomains)
	}
	replacements := []struct{ from, to string }{
		{issuerLine, "issuer: " + strings.TrimSpace(os.Getenv(envKindOIDCIssuer))},
		{"client_id: " + kindOIDCDefaultClient, "client_id: " + clientID},
		{"display_name: " + kindOIDCDefaultName, "display_name: " + yamlQuote(name)},
		{"    - " + kindOIDCDefaultDomain + "\n", yamlList(domains, "    ")},
	}
	for _, r := range replacements {
		if strings.Count(content, r.from) != 1 {
			return "", fmt.Errorf("server config does not contain exactly one %q to rewrite for %s", strings.TrimSpace(r.from), envKindOIDCIssuer)
		}
		content = strings.Replace(content, r.from, r.to, 1)
	}
	return content, nil
}

func yamlQuote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

func yamlList(items []string, indent string) string {
	var b strings.Builder
	for _, item := range items {
		b.WriteString(indent + "- " + yamlQuote(item) + "\n")
	}
	return b.String()
}

// applyKindOIDC deploys the mock provider: its certificate, the CA the server
// trusts through buildmax-trust, and its manifest on this cluster's TLS port.
// The mock is deployed even when a real IdP is configured, so switching back is
// only a matter of unsetting the environment.
func applyKindOIDC() error {
	certPEM, keyPEM, err := generateSelfSignedCert(kindOIDCServiceDNS)
	if err != nil {
		return err
	}
	tlsManifest, err := captureKindKubectl(
		"create", "secret", "generic", "buildmax-smoke-oidc-tls", "-n", "buildmax",
		"--type=kubernetes.io/tls",
		"--from-literal=tls.crt="+string(certPEM),
		"--from-literal=tls.key="+string(keyPEM),
		"--dry-run=client", "-o", "yaml",
	)
	if err != nil {
		return fmt.Errorf("render mock OIDC TLS secret: %w", err)
	}
	if err := runStdin(tlsManifest, "kubectl", "--context", kindContext(), "apply", "-f", "-"); err != nil {
		return err
	}
	trustManifest, err := captureKindKubectl(
		"create", "configmap", "buildmax-trust", "-n", "buildmax",
		"--from-literal=smoke-oidc-ca.crt="+string(certPEM),
		"--dry-run=client", "-o", "yaml",
	)
	if err != nil {
		return fmt.Errorf("render buildmax-trust configmap: %w", err)
	}
	if err := runStdin(trustManifest, "kubectl", "--context", kindContext(), "apply", "-f", "-"); err != nil {
		return err
	}
	manifest, err := os.ReadFile(kindOIDCManifest)
	if err != nil {
		return fmt.Errorf("read %s: %w", kindOIDCManifest, err)
	}
	return runStdin(renderKindOIDCManifest(string(manifest)), "kubectl", "--context", kindContext(), "apply", "-f", "-")
}

// renderKindOIDCManifest moves the mock's issuer and Service onto this
// cluster's TLS port. The container listens on its own port, so every 8443 in
// the manifest is the external one.
func renderKindOIDCManifest(content string) string {
	return strings.ReplaceAll(content, defaultKindTLSPort, kindTLSPort())
}
