package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAdminQuotaTierSetSendsTheTier(t *testing.T) {
	var gotPath, gotTier string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.Method + " " + r.URL.Path
		var body struct {
			Tier string `json:"tier"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotTier = body.Tier
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	signInHome(t, t.TempDir(), srv.URL)

	cmd := newAdminQuotaTierCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"set", "sp_9", "pro"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("set: %v\n%s", err, out.String())
	}
	if gotPath != "PUT /api/admin/spaces/sp_9/quota-tier" || gotTier != "pro" {
		t.Errorf("request = %q with tier %q", gotPath, gotTier)
	}
	if !strings.Contains(out.String(), "sp_9 is now on the pro tier") {
		t.Errorf("output = %q", out.String())
	}
}

// The server's refusal already names the valid tiers; the command must pass it
// through and exit nonzero rather than claim the change.
func TestAdminQuotaTierSetReportsARefusal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": `unknown quota tier: "gold"; valid tiers: free_trial, pro`})
	}))
	t.Cleanup(srv.Close)
	signInHome(t, t.TempDir(), srv.URL)

	cmd := newAdminQuotaTierCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"set", "sp_9", "gold"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "free_trial, pro") {
		t.Fatalf("err = %v, want the server's refusal naming the valid tiers", err)
	}
	if strings.Contains(out.String(), "is now on") {
		t.Errorf("a refusal was reported as a change: %s", out.String())
	}
}

func TestAdminQuotaTierList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/admin/quota-tiers" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"tiers": []map[string]any{
			{"tier_name": "free_trial", "max_runs_per_period": 10, "max_tokens_per_period": 100000, "max_storage_bytes": 0, "period_days": 30},
			{"tier_name": "pro", "max_runs_per_period": 1000, "max_tokens_per_period": 10000000, "max_storage_bytes": 0, "period_days": 30},
		}})
	}))
	t.Cleanup(srv.Close)
	signInHome(t, t.TempDir(), srv.URL)

	cmd := newAdminQuotaTierCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("list: %v\n%s", err, out.String())
	}
	for _, want := range []string{"free_trial", "pro", "1000", "30 days", "unlimited"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
}
