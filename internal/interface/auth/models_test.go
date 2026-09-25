package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/infra/llmwire"
	"github.com/icloudbb/buildmax/internal/testsupport"
)

// deploymentOffering is a server that answers the model listing with what the
// test staged, recording the path it was asked for.
func deploymentOffering(t *testing.T, models []llmwire.Model) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != llmwire.ModelsPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(llmwire.ModelsResponse{Models: models})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestResolveModelSourceWithoutALoginIsLocal is the mode rule: no credentials
// means the caller uses settings.yaml, and nothing is fetched.
func TestResolveModelSourceWithoutALoginIsLocal(t *testing.T) {
	t.Setenv(config.EnvKeyBuildmaxHome, t.TempDir())

	source, err := ResolveModelSource(context.Background())
	if err != nil {
		t.Fatalf("ResolveModelSource: %v", err)
	}
	if source.Managed() {
		t.Errorf("source = %+v, want local mode", source)
	}
	if len(source.Entries) != 0 {
		t.Errorf("Entries = %+v, want none in local mode", source.Entries)
	}
}

// TestResolveModelSourceFetchesWhatTheDeploymentOffers is managed mode: the
// models are the server's, and each carries the window a session compacts
// against but no endpoint or credential of its own.
func TestResolveModelSourceFetchesWhatTheDeploymentOffers(t *testing.T) {
	srv := deploymentOffering(t, []llmwire.Model{
		{Name: "Fast", ContextWindow: 128_000},
		{Name: "Deep", ContextWindow: 200_000, Vision: true, Default: true},
	})
	t.Setenv(config.EnvKeyBuildmaxHome, t.TempDir())
	saveUsableLogin(t, srv.URL)

	source, err := ResolveModelSource(context.Background())
	if err != nil {
		t.Fatalf("ResolveModelSource: %v", err)
	}
	if !source.Managed() || source.ServerURL != srv.URL {
		t.Fatalf("source = %+v, want managed mode against %s", source, srv.URL)
	}
	if len(source.Entries) != 2 {
		t.Fatalf("Entries = %d, want 2", len(source.Entries))
	}
	if source.Default != "Deep" {
		t.Errorf("Default = %q, want the model the server marked", source.Default)
	}

	fast := source.Entries[0]
	if fast.Name != "Fast" || fast.Model != "Fast" {
		t.Errorf("entry = %+v, want the catalog name as both identity and model", fast)
	}
	if fast.ContextWindow != 128_000 {
		t.Errorf("ContextWindow = %d, want the server's", fast.ContextWindow)
	}
	if fast.APIKey != "" || fast.APIURL != "" {
		t.Errorf("entry = %+v, want no credential and no endpoint", fast)
	}
	if !source.Entries[1].Vision {
		t.Error("vision was dropped, so an image would never be sent to a model that reads one")
	}
}

// TestResolveModelSourceReportsAnExpiredLogin is the no-fallback rule at its
// sharpest.
//
// A spent login is not "signed out": auth.Info reports it that way, which is
// right for a command asking whether to offer an account, and using that here
// would silently put the session in local mode — sending the next prompt to a
// provider nobody chose for it. What decides the mode is the credentials file
// existing, so this must report the expiry and let the user answer it.
func TestResolveModelSourceReportsAnExpiredLogin(t *testing.T) {
	t.Setenv(config.EnvKeyBuildmaxHome, t.TempDir())
	saveExpiredLogin(t, "https://buildmax.example.com")

	// The premise: Info reports this login as signed out.
	info, err := Info()
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.LoggedIn {
		t.Fatal("the fixture is not an expired login")
	}

	source, err := ResolveModelSource(context.Background())
	if !errors.Is(err, ErrLoginExpired) {
		t.Fatalf("want ErrLoginExpired, got %v", err)
	}
	if source.Managed() || len(source.Entries) > 0 {
		t.Errorf("source = %+v, want nothing usable alongside the error", source)
	}
}

// TestResolveModelSourceReportsAServerRejectedLogin is the same no-fallback rule
// for the other shape of a dead login: the credential is on disk and not locally
// expired, so it is handed to the deployment, but the deployment answers 401
// because the session was revoked or the token is no longer trusted. That is not
// a bare failure to puzzle over — it is an expired login, and it must offer the
// same return to local mode. See docs/design/client-modes.md section 8.
func TestResolveModelSourceReportsAServerRejectedLogin(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
	}))
	t.Cleanup(srv.Close)
	t.Setenv(config.EnvKeyBuildmaxHome, t.TempDir())
	saveUsableLogin(t, srv.URL)

	source, err := ResolveModelSource(context.Background())
	if !errors.Is(err, ErrLoginExpired) {
		t.Fatalf("want ErrLoginExpired for a server-rejected credential, got %v", err)
	}
	if source.Managed() || len(source.Entries) > 0 {
		t.Errorf("source = %+v, want nothing usable alongside the error", source)
	}
}

// An outage is not an ended login. Reporting it as one would offer signing out
// as the way forward, discarding a credential that works again once the
// deployment is back.
func TestResolveModelSourceReportsAnOutageAsUnavailable(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(down.Close)
	gone := httptest.NewServer(http.NotFoundHandler())
	goneURL := gone.URL
	gone.Close()

	for name, serverURL := range map[string]string{"503": down.URL, "connection refused": goneURL} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(config.EnvKeyBuildmaxHome, t.TempDir())
			saveUsableLogin(t, serverURL)

			_, err := ResolveModelSource(context.Background())
			if !errors.Is(err, ErrServerUnavailable) {
				t.Fatalf("err = %v, want ErrServerUnavailable", err)
			}
			if errors.Is(err, ErrLoginExpired) {
				t.Errorf("an outage was reported as an expired login: %v", err)
			}
		})
	}
}

// A disabled account is its own answer: signing in again does not help until
// an administrator re-enables it.
func TestResolveModelSourceReportsADisabledAccount(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "account_disabled"})
	}))
	t.Cleanup(srv.Close)
	t.Setenv(config.EnvKeyBuildmaxHome, t.TempDir())
	saveUsableLogin(t, srv.URL)

	if _, err := ResolveModelSource(context.Background()); !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("err = %v, want ErrAccountDisabled", err)
	}
}

// The deployment's rates reach the entry, so a managed session prices itself
// instead of reporting its cost as unavailable.
func TestResolveModelSourceCarriesTheDeploymentsPricing(t *testing.T) {
	srv := deploymentOffering(t, []llmwire.Model{{
		Name: "Priced", Default: true,
		Pricing: &llmwire.ModelPricing{Currency: "USD", InputPerMTok: "0.2", OutputPerMTok: "1.2",
			CacheReadPerMTok: "0.02", CacheWritePerMTok: "0.25"},
	}, {Name: "Unpriced"}})
	t.Setenv(config.EnvKeyBuildmaxHome, t.TempDir())
	saveUsableLogin(t, srv.URL)

	source, err := ResolveModelSource(context.Background())
	if err != nil {
		t.Fatalf("ResolveModelSource: %v", err)
	}
	priced := source.Entries[0].Pricing
	if priced == nil || priced.Currency != "USD" || priced.InputPerMTok != "0.2" || priced.CacheWritePerMTok != "0.25" {
		t.Errorf("priced entry pricing = %+v", priced)
	}
	if source.Entries[1].Pricing != nil {
		t.Errorf("unpriced entry pricing = %+v, want nil", source.Entries[1].Pricing)
	}
}

// An empty catalog is reported rather than returned as a usable managed mode
// with nothing in it, which would fail later with no explanation.
func TestResolveModelSourceRejectsAnEmptyCatalog(t *testing.T) {
	srv := deploymentOffering(t, nil)
	t.Setenv(config.EnvKeyBuildmaxHome, t.TempDir())
	saveUsableLogin(t, srv.URL)

	if _, err := ResolveModelSource(context.Background()); err == nil {
		t.Fatal("an empty catalog produced a usable managed mode")
	}
}

func saveUsableLogin(t *testing.T, serverURL string) {
	t.Helper()
	creds := &Credentials{
		ServerURL: serverURL,
		Token:     testsupport.SignJWTWithExp("u_1", "secret", 24*time.Hour),
		UserID:    "u_1",
		Email:     "someone@example.com",
	}
	if err := Save(creds, config.AuthPath()); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

// An expired access token and no refresh token: the login is stored, and it can
// no longer authenticate anything.
func saveExpiredLogin(t *testing.T, serverURL string) {
	t.Helper()
	creds := &Credentials{
		ServerURL: serverURL,
		Token:     testsupport.SignJWTWithExp("u_1", "secret", -time.Hour),
		UserID:    "u_1",
	}
	if err := Save(creds, config.AuthPath()); err != nil {
		t.Fatalf("Save: %v", err)
	}
}
