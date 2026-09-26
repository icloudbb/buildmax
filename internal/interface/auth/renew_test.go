package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/infra/llmwire"
	"github.com/icloudbb/buildmax/internal/interface/client"
	"github.com/icloudbb/buildmax/internal/testsupport"
)

// rotatedDeployment is a server whose JWT secret has changed: it lists models
// only for accept and refuses every other token, including ones that have not
// expired, with the 401 the auth guard writes.
func rotatedDeployment(t *testing.T, accept string, requests *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Header.Get("Authorization") != "Bearer "+accept {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(llmwire.ModelsResponse{Models: []llmwire.Model{{Name: "Fast", Default: true}}})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// After a JWT secret rotation the stored token still looks valid here, so
// nothing renews it on expiry. The server's 401 is the signal: the client
// renews once and the call goes through, with no new login.
func TestServerCallRecoversFromARefusedTokenWithoutReLogin(t *testing.T) {
	stale := testsupport.SignJWTWithExp("u_1", "old-secret", 24*time.Hour)
	fresh := testsupport.SignJWTWithExp("u_1", "new-secret", 15*time.Minute)
	var requests atomic.Int32
	srv := rotatedDeployment(t, fresh, &requests)
	storeSession(t, srv.URL, stale, "bmxrefresh_1")
	f := &fakeRefresh{resp: &client.RefreshResponse{AccessToken: fresh, RefreshToken: "bmxrefresh_2"}}
	useFakeRefresh(t, f)

	source, err := ResolveModelSource(context.Background())
	if err != nil {
		t.Fatalf("ResolveModelSource after rotation: %v", err)
	}
	if source.Default != "Fast" {
		t.Errorf("source = %+v, want the deployment's catalog", source)
	}
	if f.count() != 1 || requests.Load() != 2 {
		t.Errorf("%d exchanges and %d requests, want one renewal and one retry", f.count(), requests.Load())
	}
	// The renewed pair is stored, so the next call — here or in another
	// process — starts from it instead of being refused again.
	creds, err := Load(config.AuthPath())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if creds.Token != fresh || creds.RefreshToken != "bmxrefresh_2" {
		t.Error("the renewed credentials were not saved")
	}
}

// When the renewal is refused too, the session really is over and the caller
// reports an ended login rather than an outage.
func TestServerCallStillReportsAnEndedLoginWhenRenewalFails(t *testing.T) {
	stale := testsupport.SignJWTWithExp("u_1", "old-secret", 24*time.Hour)
	var requests atomic.Int32
	srv := rotatedDeployment(t, "never-issued", &requests)
	storeSession(t, srv.URL, stale, "bmxrefresh_1")
	f := &fakeRefresh{err: client.ErrRefreshRejected}
	useFakeRefresh(t, f)

	_, err := ResolveModelSource(context.Background())
	if !errors.Is(err, ErrLoginExpired) {
		t.Fatalf("err = %v, want ErrLoginExpired", err)
	}
	if f.count() != 1 || requests.Load() != 1 {
		t.Errorf("%d exchanges and %d requests, want one attempt and no retry", f.count(), requests.Load())
	}
}

// Parallel calls refused with the same token must spend the rotating refresh
// token once: the server reads a second exchange of it as reuse.
func TestConcurrentRefusalsRenewOnce(t *testing.T) {
	stale := testsupport.SignJWTWithExp("u_1", "old-secret", 24*time.Hour)
	fresh := testsupport.SignJWTWithExp("u_1", "new-secret", 15*time.Minute)
	storeSession(t, "https://buildmax.example.com", stale, "bmxrefresh_1")
	f := &fakeRefresh{resp: &client.RefreshResponse{AccessToken: fresh, RefreshToken: "bmxrefresh_2"}}
	useFakeRefresh(t, f)

	const callers = 8
	tokens := make([]string, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			tokens[i], errs[i] = RenewRejected("https://buildmax.example.com", stale)
		}()
	}
	close(start)
	wg.Wait()

	for i := range callers {
		if errs[i] != nil || tokens[i] != fresh {
			t.Errorf("caller %d: token matches=%v err=%v", i, tokens[i] == fresh, errs[i])
		}
	}
	if f.count() != 1 {
		t.Errorf("%d exchanges for %d concurrent refusals, want 1", f.count(), callers)
	}
}

// A refusal of a token that is no longer the stored one means someone already
// renewed — another goroutine or another BuildMax process — so the stored
// token is the answer and no exchange is spent.
func TestRenewRejectedUsesATokenAlreadyRenewedElsewhere(t *testing.T) {
	current := testsupport.SignJWTWithExp("u_1", "new-secret", 15*time.Minute)
	storeSession(t, "https://buildmax.example.com", current, "bmxrefresh_2")
	f := &fakeRefresh{}
	useFakeRefresh(t, f)

	got, err := RenewRejected("https://buildmax.example.com", "an-older-token")
	if err != nil {
		t.Fatalf("RenewRejected: %v", err)
	}
	if got != current || f.count() != 0 {
		t.Errorf("got the stored token=%v after %d exchanges, want it with none", got == current, f.count())
	}
}
