package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/infra/llmwire"
	"github.com/icloudbb/buildmax/internal/interface/auth"
	"github.com/icloudbb/buildmax/internal/testsupport"
)

// signInHome stores a usable login to serverURL in home, the state
// `buildmax login` leaves behind.
func signInHome(t *testing.T, home, serverURL string) {
	t.Helper()
	t.Setenv(config.EnvKeyBuildmaxHome, home)
	creds := &auth.Credentials{
		ServerURL: serverURL,
		Token:     testsupport.SignJWTWithExp("u_1", "secret", 24*time.Hour),
		UserID:    "u_1",
		Email:     "someone@example.com",
	}
	if err := auth.Save(creds, config.AuthPath()); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

func deploymentServing(t *testing.T, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusOK {
			w.WriteHeader(status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(llmwire.ModelsResponse{Models: []llmwire.Model{{Name: "Deep", Default: true}}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// Signed in, settings.yaml is not what serves prompts, so its absence is not a
// setup problem and its local models are not probed.
func TestDoctorSignedInNeedsNoSettings(t *testing.T) {
	home := t.TempDir()
	srv := deploymentServing(t, http.StatusOK)
	signInHome(t, home, srv.URL)

	out, err := runDoctorInHome(t, home, "--workspace", t.TempDir())
	if err != nil {
		t.Fatalf("doctor failed for a working signed-in setup: %v\n%s", err, out)
	}
	for _, want := range []string{"[OK]   mode: signed in to " + srv.URL, "not needed while signed in"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "buildmax init") {
		t.Errorf("doctor sent a signed-in user to buildmax init:\n%s", out)
	}
}

// Signed in with a settings.yaml, its models serve nothing, so none is checked
// or marked as the default.
func TestDoctorSignedInSkipsLocalModels(t *testing.T) {
	home := t.TempDir()
	srv := deploymentServing(t, http.StatusOK)
	signInHome(t, home, srv.URL)
	writeSettings(t, home, "models:\n  - model: gpt-test\n    name: Local\n    api_url: https://example.invalid/v1\n    api_key: sk-test\n")

	out, err := runDoctorInHome(t, home, "--workspace", t.TempDir())
	if err != nil {
		t.Fatalf("doctor: %v\n%s", err, out)
	}
	if strings.Contains(out, "model[0]") || strings.Contains(out, "(default)") {
		t.Errorf("doctor checked unused local models while signed in:\n%s", out)
	}
	if !strings.Contains(out, "unused while signed in") {
		t.Errorf("doctor did not say settings.yaml is unused:\n%s", out)
	}
}

// An outage fails the mode check, and the advice is to wait or work locally —
// not to sign in again, which a working login does not need.
func TestDoctorReportsAnOutageWithoutSendingTheUserToLogin(t *testing.T) {
	home := t.TempDir()
	srv := deploymentServing(t, http.StatusServiceUnavailable)
	signInHome(t, home, srv.URL)

	out, err := runDoctorInHome(t, home, "--workspace", t.TempDir())
	if err == nil {
		t.Fatalf("doctor passed with the deployment down:\n%s", out)
	}
	if !strings.Contains(out, "[FAIL] mode") || !strings.Contains(out, "Your login still works") {
		t.Errorf("doctor output does not explain the outage:\n%s", out)
	}
	if strings.Contains(out, "buildmax login") {
		t.Errorf("doctor sent the user to buildmax login for an outage:\n%s", out)
	}
}

// `me` asks the deployment, so a login it has revoked is not reported as
// logged in.
func TestMeReportsALoginTheServerRejected(t *testing.T) {
	home := t.TempDir()
	srv := deploymentServing(t, http.StatusUnauthorized)
	signInHome(t, home, srv.URL)

	root := NewRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"me"})
	err := root.Execute()
	out := buf.String()
	if err == nil {
		t.Fatalf("me succeeded for a rejected login:\n%s", out)
	}
	if strings.Contains(out, "Logged in as") {
		t.Errorf("me reported a rejected login as logged in:\n%s", out)
	}
	if !strings.Contains(err.Error(), "buildmax login") {
		t.Errorf("me error does not name the way forward: %v", err)
	}
}
