package auth

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/interface/client"
	"github.com/icloudbb/buildmax/internal/testsupport"
)

// storeSession writes a full login — both credentials — into an isolated
// BUILDMAX_HOME.
func storeSession(t *testing.T, serverURL, token, refreshToken string) {
	t.Helper()
	t.Setenv(config.EnvKeyBuildmaxHome, t.TempDir())
	creds := &Credentials{
		ServerURL:    serverURL,
		Token:        token,
		RefreshToken: refreshToken,
		UserID:       "u_1",
		Email:        "a@b.c",
	}
	if err := Save(creds, config.AuthPath()); err != nil {
		t.Fatalf("Save: %v", err)
	}
}

// fakeRefresh stands in for the server, counting exchanges.
type fakeRefresh struct {
	mu        sync.Mutex
	calls     int
	presented []string
	resp      *client.RefreshResponse
	err       error
}

func (f *fakeRefresh) Refresh(_ context.Context, refreshToken string) (*client.RefreshResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.presented = append(f.presented, refreshToken)
	if f.err != nil {
		return nil, f.err
	}
	return f.resp, nil
}

func (f *fakeRefresh) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// useFakeRefresh points the credential layer at f for the duration of a test.
func useFakeRefresh(t *testing.T, f *fakeRefresh) {
	t.Helper()
	original := newClient
	newClient = func(string) refreshClient { return f }
	t.Cleanup(func() { newClient = original })
}

func TestTokenForServerRenewsAnExpiredAccessToken(t *testing.T) {
	storeSession(t, "https://buildmax.example.com",
		testsupport.SignJWTWithExp("u_1", "secret", -time.Hour), "bmxrefresh_1")
	fresh := testsupport.SignJWTWithExp("u_1", "secret", 7*24*time.Hour)
	f := &fakeRefresh{resp: &client.RefreshResponse{
		AccessToken:  fresh,
		RefreshToken: "bmxrefresh_2",
		ExpiresIn:    604800,
	}}
	useFakeRefresh(t, f)

	got, err := TokenForServer("https://buildmax.example.com")
	if err != nil {
		t.Fatalf("TokenForServer: %v", err)
	}
	if got != fresh {
		t.Error("TokenForServer returned the expired token rather than the renewed one")
	}
	if f.presented[0] != "bmxrefresh_1" {
		t.Errorf("presented %q, want the stored refresh token", f.presented[0])
	}

	// Both halves of the rotated pair are persisted, or the next process would
	// present a token this one has already spent.
	creds, err := Load(config.AuthPath())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if creds.Token != fresh {
		t.Error("the renewed access token was not saved")
	}
	if creds.RefreshToken != "bmxrefresh_2" {
		t.Errorf("stored refresh token = %q, want the rotated one", creds.RefreshToken)
	}
}

// Renewing early matters because a managed call is authorized when it starts
// and may run for minutes afterwards.
func TestTokenForServerRenewsBeforeExpiryNotAtIt(t *testing.T) {
	storeSession(t, "https://buildmax.example.com",
		testsupport.SignJWTWithExp("u_1", "secret", refreshSkew/2), "bmxrefresh_1")
	fresh := testsupport.SignJWTWithExp("u_1", "secret", 7*24*time.Hour)
	f := &fakeRefresh{resp: &client.RefreshResponse{AccessToken: fresh, RefreshToken: "bmxrefresh_2"}}
	useFakeRefresh(t, f)

	got, err := TokenForServer("https://buildmax.example.com")
	if err != nil {
		t.Fatalf("TokenForServer: %v", err)
	}
	if got != fresh {
		t.Error("a token inside the skew window was used instead of renewed")
	}
}

func TestTokenForServerLeavesAGoodTokenAlone(t *testing.T) {
	token := testsupport.SignJWTWithExp("u_1", "secret", 7*24*time.Hour)
	storeSession(t, "https://buildmax.example.com", token, "bmxrefresh_1")
	f := &fakeRefresh{}
	useFakeRefresh(t, f)

	got, err := TokenForServer("https://buildmax.example.com")
	if err != nil {
		t.Fatalf("TokenForServer: %v", err)
	}
	if got != token {
		t.Error("the stored token was replaced despite being valid")
	}
	if f.count() != 0 {
		t.Errorf("%d refreshes for a token that had not expired", f.count())
	}
}

// Goroutines in one process share a mutex, so they must not each spend an
// exchange. The server's grace window is for separate processes; burning it
// here would leave nothing for the case it was built for.
func TestConcurrentCallersRefreshOnce(t *testing.T) {
	storeSession(t, "https://buildmax.example.com",
		testsupport.SignJWTWithExp("u_1", "secret", -time.Hour), "bmxrefresh_1")
	fresh := testsupport.SignJWTWithExp("u_1", "secret", 7*24*time.Hour)
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
			tokens[i], errs[i] = TokenForServer("https://buildmax.example.com")
		}()
	}
	close(start)
	wg.Wait()

	for i := range callers {
		if errs[i] != nil {
			t.Errorf("caller %d: %v", i, errs[i])
			continue
		}
		if tokens[i] != fresh {
			t.Errorf("caller %d got a token other than the renewed one", i)
		}
	}
	if f.count() != 1 {
		t.Errorf("%d exchanges for %d concurrent callers, want 1", f.count(), callers)
	}
}

// A rejected refresh token means the session is over — spent, revoked, or
// reported as reused. The login still stays on disk: its presence is the mode,
// and clearing it here would turn the next command into a local-mode run the
// user never chose (docs/design/client-modes.md section 8).
func TestRejectedRefreshReportsAnExpiredLoginAndKeepsTheMode(t *testing.T) {
	storeSession(t, "https://buildmax.example.com",
		testsupport.SignJWTWithExp("u_1", "secret", -time.Hour), "bmxrefresh_1")
	useFakeRefresh(t, &fakeRefresh{err: client.ErrRefreshRejected})

	_, err := TokenForServer("https://buildmax.example.com")
	if !errors.Is(err, ErrLoginExpired) {
		t.Fatalf("err = %v, want ErrLoginExpired", err)
	}
	creds, loadErr := Load(config.AuthPath())
	if loadErr != nil {
		t.Fatalf("Load: %v", loadErr)
	}
	if creds == nil {
		t.Error("a rejected session cleared the login, which silently returns the client to local mode")
	}
}

// An unreachable server is not an ended session. Discarding the login here
// would cost the user a login code they did not need to spend.
func TestUnreachableServerKeepsTheStoredLogin(t *testing.T) {
	storeSession(t, "https://buildmax.example.com",
		testsupport.SignJWTWithExp("u_1", "secret", -time.Hour), "bmxrefresh_1")
	useFakeRefresh(t, &fakeRefresh{err: errors.New("dial tcp: connection refused")})

	if _, err := TokenForServer("https://buildmax.example.com"); err == nil {
		t.Fatal("an unreachable server produced no error")
	}
	creds, err := Load(config.AuthPath())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if creds == nil || creds.RefreshToken != "bmxrefresh_1" {
		t.Error("a network failure discarded a session that was still good")
	}
}

// An expired access token with a refresh token behind it is still a login.
// Reporting it as signed out would send someone to ask for a code they do not
// need.
func TestInfoTreatsARenewableSessionAsSignedIn(t *testing.T) {
	storeSession(t, "https://buildmax.example.com",
		testsupport.SignJWTWithExp("u_1", "secret", -time.Hour), "bmxrefresh_1")

	info, err := Info()
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if !info.LoggedIn {
		t.Error("a session that can be renewed was reported as signed out")
	}
	if info.Email != "a@b.c" {
		t.Errorf("email = %q, want the stored one", info.Email)
	}
}

func TestInfoReportsAnUnrenewableExpiredSessionAsSignedOut(t *testing.T) {
	storeSession(t, "https://buildmax.example.com",
		testsupport.SignJWTWithExp("u_1", "secret", -time.Hour), "")

	info, err := Info()
	if err != nil {
		t.Fatalf("Info: %v", err)
	}
	if info.LoggedIn {
		t.Error("an expired session with nothing to renew it was reported as signed in")
	}
}

// CanAuthenticate is what `buildmax doctor` calls. A diagnostic that rotated
// the user's refresh token would be changing what it claims to be inspecting.
func TestCanAuthenticateMakesNoNetworkCall(t *testing.T) {
	storeSession(t, "https://buildmax.example.com",
		testsupport.SignJWTWithExp("u_1", "secret", -time.Hour), "bmxrefresh_1")
	f := &fakeRefresh{}
	useFakeRefresh(t, f)

	if err := CanAuthenticate("https://buildmax.example.com"); err != nil {
		t.Errorf("CanAuthenticate: %v", err)
	}
	if f.count() != 0 {
		t.Errorf("a read-only check made %d refresh calls", f.count())
	}

	if err := CanAuthenticate("https://attacker.example.net"); err == nil {
		t.Error("CanAuthenticate accepted a server the login does not belong to")
	}
}
