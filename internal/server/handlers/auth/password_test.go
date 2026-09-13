package auth

import (
	"bytes"
	"net/http"
	"sort"
	"testing"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	"github.com/icloudbb/buildmax/internal/mock"
)

const testPassword = "correct horse battery staple"

func hashFor(t *testing.T, plaintext string) string {
	t.Helper()
	hash, err := coreidentity.HashPassword(plaintext)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	return hash
}

// newPasswordMux wires a handler where u1 signs in with testPassword.
func newPasswordMux(t *testing.T, cfg Config) (*http.ServeMux, *mock.MockPasswordStore) {
	t.Helper()
	user := &coreidentity.User{ID: "u1", Email: "a@b.c", Name: "Alice"}
	passwords := &mock.MockPasswordStore{Hashes: map[string]string{"u1": hashFor(t, testPassword)}}
	if cfg.Users == nil {
		cfg.Users = &mock.MockUserStore{
			ByEmail: map[string]*coreidentity.User{"a@b.c": user},
			ByID:    map[string]*coreidentity.User{"u1": user},
		}
	}
	if cfg.Passwords == nil {
		cfg.Passwords = passwords
	} else if s, ok := cfg.Passwords.(*mock.MockPasswordStore); ok {
		passwords = s
	}
	if cfg.JWTSecret == "" {
		cfg.JWTSecret = "test-jwt-secret"
	}
	if cfg.RefreshTokens == nil {
		cfg.RefreshTokens = &mock.MockRefreshTokenStore{}
	}
	mux := http.NewServeMux()
	New(cfg).Register(mux)
	return mux, passwords
}

func TestPasswordLoginIssuesASession(t *testing.T) {
	mux, _ := newPasswordMux(t, Config{})
	rec := postJSON(t, mux, "/api/auth/login",
		`{"email":"a@b.c","password":"`+testPassword+`","platform":"portal"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	if body["access_token"] == "" {
		t.Error("no access token issued")
	}
	// A password login is a session like any other, so it gets both halves.
	if _, ok := body["refresh_token"].(string); !ok {
		t.Error("a password login returned no refresh token")
	}
}

// Every failure answers the same way. Telling an unknown address apart from a
// wrong password turns the login form into a way to ask who has an account.
func TestPasswordLoginFailuresAreIndistinguishable(t *testing.T) {
	noPassword := &coreidentity.User{ID: "u2", Email: "nopass@b.c"}
	mux, _ := newPasswordMux(t, Config{
		Users: &mock.MockUserStore{
			ByEmail: map[string]*coreidentity.User{
				"a@b.c":      {ID: "u1", Email: "a@b.c", Name: "Alice"},
				"nopass@b.c": noPassword,
			},
			ByID: map[string]*coreidentity.User{"u1": {ID: "u1", Email: "a@b.c"}, "u2": noPassword},
		},
	})

	tests := []struct {
		name string
		body string
	}{
		{"wrong password", `{"email":"a@b.c","password":"wrong but long enough"}`},
		{"unknown address", `{"email":"nobody@example.com","password":"` + testPassword + `"}`},
		{"account with no password set", `{"email":"nopass@b.c","password":"` + testPassword + `"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := postJSON(t, mux, "/api/auth/login", tt.body, nil)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			if got := decodeJSON(t, rec)["error"]; got != invalidPasswordMessage {
				t.Errorf("error = %q, want the same message every failure gives", got)
			}
		})
	}
}

// A password shorter than the minimum is refused at login too, not only when
// set — otherwise a hash written before the rule existed would still work.
func TestPasswordLoginRejectsAnEmptyPasswordWithoutFallingBackToOtp(t *testing.T) {
	mux, _ := newPasswordMux(t, Config{})
	rec := postJSON(t, mux, "/api/auth/login", `{"email":"a@b.c","password":"","otp":""}`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestSetPasswordRequiresTheCurrentOneWhenThereIsOne(t *testing.T) {
	mux, passwords := newPasswordMux(t, Config{})
	access, _ := loginWithPassword(t, mux)
	const next = "a much longer replacement passphrase"

	// Without the current password, the session alone is not enough. A stolen
	// access token cannot be revoked before it expires; letting one set a
	// password would make a temporary theft permanent.
	rec := postJSON(t, mux, "/api/auth/password", `{"new_password":"`+next+`"}`, map[string]string{
		"Authorization": "Bearer " + access,
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if coreidentity.VerifyPassword(passwords.Hashes["u1"], next) {
		t.Fatal("the password changed without the current one")
	}

	rec = postJSON(t, mux, "/api/auth/password",
		`{"current_password":"`+testPassword+`","new_password":"`+next+`"}`,
		map[string]string{"Authorization": "Bearer " + access})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if !coreidentity.VerifyPassword(passwords.Hashes["u1"], next) {
		t.Error("the new password was not stored")
	}
}

// Claiming an account: the session came from an operator-issued login code,
// which is the strongest proof this deployment has, and there is no current
// password to present.
func TestSetFirstPasswordNeedsOnlyTheSession(t *testing.T) {
	user := &coreidentity.User{ID: "u1", Email: "a@b.c", Name: "Alice"}
	passwords := &mock.MockPasswordStore{}
	mux := http.NewServeMux()
	New(Config{
		Users: &mock.MockUserStore{
			ByEmail: map[string]*coreidentity.User{"a@b.c": user},
			ByID:    map[string]*coreidentity.User{"u1": user},
		},
		LoginCodes: &mock.MockLoginCodeStore{Codes: map[string]*mock.MockLoginCode{
			"code-1": {UserID: "u1", ExpiresAt: time.Now().Add(time.Hour).UTC()},
		}},
		Passwords:     passwords,
		RefreshTokens: &mock.MockRefreshTokenStore{},
		JWTSecret:     "test-jwt-secret",
	}).Register(mux)

	rec := postJSON(t, mux, "/api/auth/login", `{"email":"a@b.c","otp":"code-1"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login with a code failed: %d %s", rec.Code, rec.Body.String())
	}
	access, _ := decodeJSON(t, rec)["access_token"].(string)

	const chosen = "the passphrase they chose themselves"
	rec = postJSON(t, mux, "/api/auth/password", `{"new_password":"`+chosen+`"}`, map[string]string{
		"Authorization": "Bearer " + access,
	})
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if !coreidentity.VerifyPassword(passwords.Hashes["u1"], chosen) {
		t.Fatal("the first password was not stored")
	}

	// And it works: recovery is complete, not half-done.
	rec = postJSON(t, mux, "/api/auth/login", `{"email":"a@b.c","password":"`+chosen+`"}`, nil)
	if rec.Code != http.StatusOK {
		t.Errorf("the password just set does not sign in: %d", rec.Code)
	}
}

func TestSetPasswordEnforcesTheLengthMinimum(t *testing.T) {
	mux, _ := newPasswordMux(t, Config{})
	access, _ := loginWithPassword(t, mux)

	rec := postJSON(t, mux, "/api/auth/password",
		`{"current_password":"`+testPassword+`","new_password":"short"}`,
		map[string]string{"Authorization": "Bearer " + access})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("at least")) {
		t.Errorf("body %s does not say what the rule is", rec.Body.String())
	}
}

func TestSetPasswordRequiresAuthentication(t *testing.T) {
	mux, _ := newPasswordMux(t, Config{})
	rec := postJSON(t, mux, "/api/auth/password", `{"new_password":"a long enough passphrase"}`, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func loginWithPassword(t *testing.T, mux *http.ServeMux) (accessToken, refreshToken string) {
	t.Helper()
	rec := postJSON(t, mux, "/api/auth/login", `{"email":"a@b.c","password":"`+testPassword+`"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d, body %s", rec.Code, rec.Body.String())
	}
	body := decodeJSON(t, rec)
	access, _ := body["access_token"].(string)
	refresh, _ := body["refresh_token"].(string)
	return access, refresh
}

// TestUnknownAddressCostsWhatAWrongPasswordCosts is the other half of
// TestPasswordLoginFailuresAreIndistinguishable.
//
// That test proves the three failures answer the same status and the same
// message. This one proves they take the same time, which is the channel the
// message cannot close: an unknown address that skips the hash comes back in
// microseconds while a known one spends milliseconds, and the difference
// answers "is this address registered" to anyone with a stopwatch.
//
// Nothing else asserts it. verifyPassword calls DummyVerifyPassword on the
// no-account path precisely for this, and removing that call leaves every other
// test in this package passing.
func TestUnknownAddressCostsWhatAWrongPasswordCosts(t *testing.T) {
	mux, _ := newPasswordMux(t, Config{
		Users: &mock.MockUserStore{
			ByEmail: map[string]*coreidentity.User{"a@b.c": {ID: "u1", Email: "a@b.c"}},
			ByID:    map[string]*coreidentity.User{"u1": {ID: "u1", Email: "a@b.c"}},
		},
	})

	known := medianDuration(5, func() {
		postJSON(t, mux, "/api/auth/login", `{"email":"a@b.c","password":"wrong but long enough"}`, nil)
	})
	unknown := medianDuration(5, func() {
		postJSON(t, mux, "/api/auth/login", `{"email":"nobody@example.com","password":"wrong but long enough"}`, nil)
	})

	if known <= 0 {
		t.Fatalf("a known-address failure measured as %v; the clock is not usable here", known)
	}
	if ratio := float64(unknown) / float64(known); ratio < 0.25 {
		t.Errorf("an unknown address failed in %v against a known address's %v (ratio %.3f).\n"+
			"Both are meant to cost a password verification; at this ratio the response\n"+
			"time says whether the address is registered.", unknown, known, ratio)
	}
}

// medianDuration runs fn n times and reports the middle result, so one
// descheduled iteration does not decide what the test saw.
func medianDuration(n int, fn func()) time.Duration {
	samples := make([]time.Duration, 0, n)
	for i := 0; i < n; i++ {
		start := time.Now()
		fn()
		samples = append(samples, time.Since(start))
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	return samples[len(samples)/2]
}
