package appconnect

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/config"
	"golang.org/x/oauth2"
)

func TestConnectorRefreshAndBoundedOperations(t *testing.T) {
	t.Setenv(config.EnvKeyBuildmaxHome, t.TempDir())
	t.Setenv(config.EnvKeyBuildmaxCredentialStore, "file")
	t.Setenv("TEST_APP_CLIENT_ID", "public-client")
	var refreshed, reads, writes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			refreshed.Add(1)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"access_token":"new-access","refresh_token":"new-refresh","token_type":"Bearer","expires_in":3600}`)
		case "/profile", "/items":
			if r.Header.Get("Authorization") != "Bearer new-access" {
				t.Errorf("missing refreshed token")
			}
			if r.Method == "GET" {
				reads.Add(1)
			} else {
				writes.Add(1)
			}
			fmt.Fprint(w, `{"ok":true}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	pluginDir := t.TempDir()
	manifest := fmt.Sprintf(`auth:
  authorization_url: %s/authorize
  token_url: %s/token
  client_id_env: TEST_APP_CLIENT_ID
  scopes: [items.read, items.write]
api_origin: %s
operations:
  profile: {method: GET, path: /profile, effect: read}
  list_items: {method: GET, path: /items, effect: read}
  create_item: {method: POST, path: /items, effect: write}
`, server.URL, server.URL, server.URL)
	if err := os.WriteFile(filepath.Join(pluginDir, "connector.yaml"), []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(pluginDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveToken("fixture", &oauth2.Token{AccessToken: "old", RefreshToken: "old-refresh", Expiry: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(tokenFile("fixture"))
	if err != nil {
		t.Fatalf("stat token file: %v", err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode = %04o, want 0600", info.Mode().Perm())
	}
	for _, name := range []string{"profile", "list_items"} {
		out, err := Call(context.Background(), "fixture", m, name, nil, false)
		if err != nil || string(out) != `{"ok":true}` {
			t.Fatalf("%s: %s, %v", name, out, err)
		}
	}
	if _, err := Call(context.Background(), "fixture", m, "create_item", []byte(`{"title":"x"}`), false); err == nil {
		t.Fatal("write without approval succeeded")
	}
	if _, err := Call(context.Background(), "fixture", m, "hidden", nil, true); err == nil {
		t.Fatal("undeclared operation succeeded")
	}
	if _, err := Call(context.Background(), "fixture", m, "create_item", []byte(`{"title":"x"}`), true); err != nil {
		t.Fatal(err)
	}
	if refreshed.Load() != 1 || reads.Load() != 2 || writes.Load() != 1 {
		t.Fatalf("refresh=%d reads=%d writes=%d", refreshed.Load(), reads.Load(), writes.Load())
	}
	token, err := LoadToken("fixture")
	if err != nil || token.AccessToken != "new-access" || token.RefreshToken != "new-refresh" {
		t.Fatalf("refresh not persisted: %v", err)
	}
}

func TestConnectorRejectsUnsafeEndpoints(t *testing.T) {
	base := `auth:
  authorization_url: https://example.com/auth
  token_url: https://example.com/token
  client_id_env: TEST_APP_CLIENT_ID
  scopes: [read]
api_origin: https://api.example.com
operations:
  profile: {method: GET, path: /profile, effect: read}
`
	cases := []string{
		strings.Replace(base, "https://api.example.com", "http://evil.example.com", 1),
		strings.Replace(base, "path: /profile", "path: /../admin", 1),
		strings.Replace(base, "path: /profile", "path: /%2e%2e/admin", 1),
		strings.Replace(base, "effect: read", "effect: write", 1),
		strings.Replace(base, "api_origin: https://api.example.com", "api_origin: https://api.example.com/other", 1),
		base + "extra: ignored\n",
		base + "---\napi_origin: https://other.example.com\n",
	}
	for i, value := range cases {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "connector.yaml"), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(dir); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
}
