package desktop

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/interface/auth"
	"github.com/icloudbb/buildmax/internal/testsupport"
)

func signInTo(t *testing.T, serverURL string) {
	t.Helper()
	t.Setenv(config.EnvKeyBuildmaxHome, t.TempDir())
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

// A deployment outage is not an ended login. The ended-login screen offers
// signing out as the way on, which would discard a credential that works
// again the moment the deployment is back.
func TestAuthStatusReportsAnOutageAsUnavailableNotExpired(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(down.Close)
	signInTo(t, down.URL)

	status, err := NewApp().GetAuthStatus()
	if err != nil {
		t.Fatalf("GetAuthStatus: %v", err)
	}
	if status.Expired {
		t.Errorf("an outage was reported as an expired login: %+v", status)
	}
	if !status.Unavailable || status.UnavailableDetail == "" || !status.LoggedIn {
		t.Errorf("status = %+v, want a signed-in, unavailable deployment with its detail", status)
	}
}

// A login the deployment rejects is the ended session §8 describes.
func TestAuthStatusReportsARejectedLoginAsExpired(t *testing.T) {
	rejecting := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(rejecting.Close)
	signInTo(t, rejecting.URL)

	status, err := NewApp().GetAuthStatus()
	if err != nil {
		t.Fatalf("GetAuthStatus: %v", err)
	}
	if !status.Expired || status.Unavailable {
		t.Errorf("status = %+v, want expired and not unavailable", status)
	}
}
