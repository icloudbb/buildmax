package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// set-key takes the key from standard input and sends it in the body of the
// credential route, so it never appears in an argument, a path, or a query.
func TestAdminModelSetKeyReadsTheKeyFromStandardInput(t *testing.T) {
	var gotPath, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.Method + " " + r.URL.Path
		var body struct {
			APIKey string `json:"api_key"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotKey = body.APIKey
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "m1", "name": "Fast", "enabled": true})
	}))
	t.Cleanup(srv.Close)
	signInHome(t, t.TempDir(), srv.URL)

	cmd := newAdminModelCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader("sk-rotated-key\n"))
	cmd.SetArgs([]string{"set-key", "m1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("set-key: %v\n%s", err, out.String())
	}
	if gotPath != "PUT /api/admin/llm/models/m1/credential" {
		t.Errorf("request = %q", gotPath)
	}
	if gotKey != "sk-rotated-key" {
		t.Errorf("sent key = %q, want the line from standard input", gotKey)
	}
	if strings.Contains(out.String(), "sk-rotated-key") {
		t.Errorf("the key was echoed: %s", out.String())
	}
}
