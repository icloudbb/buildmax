package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A disable whose cleanup partly failed still disabled the account. The command
// must say so, name what failed and how to retry, and exit nonzero so a script
// does not read the cleanup as done.
func TestAdminUserDisableReportsIncompleteCleanup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"users": []map[string]any{{"id": "u_9", "email": "gone@corp.com"}}, "total": 1,
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "u_9", "email": "gone@corp.com", "disabled_at": "2026-09-06T00:00:00Z",
			"sessions_revoked": 2, "cleanup_failed": []string{"schedules"},
		})
	}))
	t.Cleanup(srv.Close)
	signInHome(t, t.TempDir(), srv.URL)

	cmd := newAdminUserCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"disable", "gone@corp.com"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("an incomplete cleanup exited zero: %s", out.String())
	}
	if !strings.Contains(out.String(), "disabled gone@corp.com; 2 session token(s) revoked") {
		t.Errorf("the disable itself was not reported: %s", out.String())
	}
	for _, want := range []string{"is disabled", "schedules", "again to retry"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}
