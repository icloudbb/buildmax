package cli

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/interface/appconnect"
)

func TestConnectOAuthLoopbackPKCE(t *testing.T) {
	t.Setenv(config.EnvKeyBuildmaxHome, t.TempDir())
	t.Setenv(config.EnvKeyBuildmaxCredentialStore, "file")
	t.Setenv("TEST_OAUTH_CLIENT_ID", "public-client")
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		if r.Form.Get("code_verifier") == "" || r.Form.Get("code") != "test-code" {
			t.Errorf("missing PKCE verifier or code")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"access_token":"connected","refresh_token":"refresh","token_type":"Bearer","expires_in":3600}`)
	}))
	defer provider.Close()
	dir := t.TempDir()
	manifest := fmt.Sprintf(`auth:
  authorization_url: %s/authorize
  token_url: %s/token
  client_id_env: TEST_OAUTH_CLIENT_ID
  scopes: [read]
api_origin: %s
operations:
  profile: {method: GET, path: /profile, effect: read}
`, provider.URL, provider.URL, provider.URL)
	if err := os.WriteFile(filepath.Join(dir, "connector.yaml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := appconnect.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	old := openExternalURL
	openExternalURL = func(raw string) error {
		u, err := url.Parse(raw)
		if err != nil {
			return err
		}
		q := u.Query()
		if q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("state") == "" {
			t.Error("authorization URL lacks state or PKCE")
		}
		callback, err := url.Parse(q.Get("redirect_uri"))
		if err != nil {
			return err
		}
		callback.RawQuery = url.Values{"state": {q.Get("state")}, "code": {"test-code"}}.Encode()
		resp, err := http.Get(callback.String())
		if err != nil {
			return err
		}
		resp.Body.Close()
		return nil
	}
	defer func() { openExternalURL = old }()
	var output bytes.Buffer
	if err := connectOAuth(context.Background(), &output, "fixture", m); err != nil {
		t.Fatal(err)
	}
	token, err := appconnect.LoadToken("fixture")
	if err != nil || token.AccessToken != "connected" {
		t.Fatalf("connection not saved: %v", err)
	}
}

func TestWriteConfirmationRequiresTerminal(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()
	if _, err := confirmAppWrite(&bytes.Buffer{}, "fixture", "create_item", `{}`); err == nil {
		t.Fatal("noninteractive write accepted")
	}
}

func TestConnectOAuthProviderDenialReturnsImmediately(t *testing.T) {
	t.Setenv("TEST_OAUTH_CLIENT_ID", "public-client")
	dir := t.TempDir()
	manifest := `auth:
  authorization_url: http://127.0.0.1:1/authorize
  token_url: http://127.0.0.1:1/token
  client_id_env: TEST_OAUTH_CLIENT_ID
  scopes: [read]
api_origin: http://127.0.0.1:1
operations:
  profile: {method: GET, path: /profile, effect: read}
`
	if err := os.WriteFile(filepath.Join(dir, "connector.yaml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := appconnect.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	old := openExternalURL
	openExternalURL = func(raw string) error {
		u, err := url.Parse(raw)
		if err != nil {
			return err
		}
		callback, err := url.Parse(u.Query().Get("redirect_uri"))
		if err != nil {
			return err
		}
		callback.RawQuery = url.Values{"state": {u.Query().Get("state")}, "error": {"access_denied"}}.Encode()
		resp, err := http.Get(callback.String())
		if err != nil {
			return err
		}
		resp.Body.Close()
		return nil
	}
	defer func() { openExternalURL = old }()
	if err := connectOAuth(context.Background(), &bytes.Buffer{}, "fixture", m); err == nil {
		t.Fatal("provider denial accepted")
	}
}
