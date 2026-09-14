package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestMethodsHandler covers what an unauthenticated client learns before it can
// sign in — and, just as much, what it must not learn.
func TestMethodsHandler(t *testing.T) {
	t.Run("advertises SSO without leaking its configuration", func(t *testing.T) {
		h := New(Config{
			LocalLogin:      "system_admins",
			OIDCEnabled:     true,
			OIDCDisplayName: "Okta",
		})
		resp := serveMethods(t, h)

		if resp.LocalLogin != "system_admins" {
			t.Errorf("local_login = %q, want system_admins", resp.LocalLogin)
		}
		if !resp.OIDC.Enabled || resp.OIDC.DisplayName != "Okta" {
			t.Errorf("oidc = %+v, want enabled Okta", resp.OIDC)
		}
	})

	t.Run("an empty local_login reads as all", func(t *testing.T) {
		// The zero value is native login for everyone, so a deployment that set
		// nothing does not advertise a locked door.
		resp := serveMethods(t, New(Config{}))
		if resp.LocalLogin != "all" {
			t.Errorf("local_login = %q, want all", resp.LocalLogin)
		}
		if resp.OIDC.Enabled {
			t.Error("oidc advertised enabled with no configuration")
		}
	})

	t.Run("a disabled provider carries no display name", func(t *testing.T) {
		// display_name is set even when disabled would leak that an IdP is named
		// but held back; keep it empty so "off" says nothing more than off.
		h := New(Config{OIDCEnabled: false, OIDCDisplayName: "Okta"})
		if dn := serveMethods(t, h).OIDC.DisplayName; dn != "" {
			t.Errorf("display_name = %q for a disabled provider, want empty", dn)
		}
	})

	t.Run("the response never carries issuer, client, or policy", func(t *testing.T) {
		h := New(Config{OIDCEnabled: true, OIDCDisplayName: "Okta"})
		rr := httptest.NewRecorder()
		h.methodsHandler(rr, httptest.NewRequest(http.MethodGet, "/api/auth/methods", nil))
		body := strings.ToLower(rr.Body.String())
		for _, forbidden := range []string{"issuer", "client_id", "client_secret", "allowed_email_domains", "provisioning"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("methods response leaked %q: %s", forbidden, rr.Body.String())
			}
		}
	})
}

func serveMethods(t *testing.T, h *Handler) MethodsResponse {
	t.Helper()
	rr := httptest.NewRecorder()
	h.methodsHandler(rr, httptest.NewRequest(http.MethodGet, "/api/auth/methods", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rr.Code, rr.Body.String())
	}
	var resp MethodsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v; body %s", err, rr.Body.String())
	}
	return resp
}
