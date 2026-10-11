package identity_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/audit"
	"github.com/icloudbb/buildmax/internal/service/identity"
)

type nativeFixture struct {
	t        *testing.T
	svc      *identity.Service
	now      time.Time
	mu       sync.Mutex
	users    *mock.MockUserStore
	sessions *mock.MockAuthSessionStore
	signIns  *mock.MockNativeSignInStore
	audit    *mock.MockAuditStore
	grants   *mock.MockSystemGrantStore
}

const (
	aliceID  = "u_alice"
	malletID = "u_mallet"
)

func newNativeFixture(t *testing.T) *nativeFixture {
	t.Helper()
	alice := &coreidentity.User{ID: aliceID, Email: "alice@example.test", Kind: coreidentity.KindHuman}
	mallet := &coreidentity.User{ID: malletID, Email: "mallet@example.test", Kind: coreidentity.KindHuman}
	users := &mock.MockUserStore{
		ByEmail: map[string]*coreidentity.User{alice.Email: alice, mallet.Email: mallet},
		ByID:    map[string]*coreidentity.User{alice.ID: alice, mallet.ID: mallet},
	}
	refresh := &mock.MockRefreshTokenStore{}
	sessions := &mock.MockAuthSessionStore{Refresh: refresh}
	f := &nativeFixture{
		t:        t,
		now:      time.Now().UTC().Truncate(time.Second),
		users:    users,
		sessions: sessions,
		signIns:  &mock.MockNativeSignInStore{Users: users, Sessions: sessions, Refresh: refresh},
		audit:    &mock.MockAuditStore{},
		grants:   &mock.MockSystemGrantStore{},
	}
	f.svc = &identity.Service{
		Users:         users,
		RefreshTokens: refresh,
		Sessions:      sessions,
		NativeSignIns: f.signIns,
		Grants:        f.grants,
		LocalLogin:    identity.LocalLoginAll,
		OIDCEnabled:   true,
		Tokens:        fixedIssuer{},
		Audit:         audit.NewRecorder(f.audit),
		Now:           f.clock,
	}
	return f
}

func (f *nativeFixture) clock() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func (f *nativeFixture) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

// portalSession opens a session as the Portal would have, with the given proof
// and absolute expiry.
func (f *nativeFixture) session(userID, platform, method string, expiresAt time.Time) string {
	f.t.Helper()
	sid, err := f.sessions.CreateSession(context.Background(), coreidentity.NewAuthSession{
		UserID: userID, Platform: platform, AuthMethod: method, AbsoluteExpiresAt: expiresAt,
	})
	if err != nil {
		f.t.Fatal(err)
	}
	return sid
}

func (f *nativeFixture) start(platform, label string) *identity.NativeSignInStarted {
	f.t.Helper()
	started, err := f.svc.StartNativeSignIn(context.Background(), identity.StartNativeSignInCmd{Platform: platform, ClientLabel: label})
	if err != nil {
		f.t.Fatalf("StartNativeSignIn: %v", err)
	}
	return started
}

// poll waits one interval first, as a well-behaved client does.
func (f *nativeFixture) poll(secret string) (*identity.LoginResult, error) {
	f.advance(coreidentity.NativeSignInMaxInterval)
	return f.svc.PollNativeSignIn(context.Background(), secret)
}

func (f *nativeFixture) liveSessions(userID string) []coreidentity.AuthSession {
	f.t.Helper()
	out, err := f.sessions.ListUserSessions(context.Background(), userID, f.clock())
	if err != nil {
		f.t.Fatal(err)
	}
	return out
}

func (f *nativeFixture) events(action string) []coreaudit.Event {
	var out []coreaudit.Event
	for _, e := range f.audit.Events {
		if e.Action == action {
			out = append(out, e)
		}
	}
	return out
}

// TestNativeSignInRedeemsAnInheritingSession walks the whole flow and pins
// what the redeemed session is: the request's platform and label, the
// confirming session's method, and exactly its absolute expiry.
func TestNativeSignInRedeemsAnInheritingSession(t *testing.T) {
	f := newNativeFixture(t)
	ctx := context.Background()
	portalExpiry := f.now.Add(11*time.Hour + 123*time.Millisecond)
	portal := f.session(aliceID, coreidentity.PlatformPortal, identity.MethodOIDC, portalExpiry)

	started := f.start(coreidentity.PlatformCLI, "  alice-mbp · macOS ")
	if started.ExpiresIn != 10*time.Minute || started.Interval != 5*time.Second {
		t.Errorf("expires_in %v, interval %v; want 10m and 5s", started.ExpiresIn, started.Interval)
	}
	if len(started.UserCode) != 9 || started.UserCode[4] != '-' || !strings.HasPrefix(started.PollSecret, "bmxsignin_") {
		t.Fatalf("started = %+v", started)
	}
	for _, r := range f.signIns.Requests {
		if strings.Contains(r.UserCodeHash, coreidentity.NormalizeUserCode(started.UserCode)) || r.PollSecretHash == started.PollSecret {
			t.Fatal("a plaintext code or secret reached the store")
		}
	}

	// Polling at once is too early; a full interval later it is just pending.
	if _, err := f.svc.PollNativeSignIn(ctx, started.PollSecret); !errors.Is(err, identity.ErrSlowDown) {
		t.Fatalf("immediate poll = %v, want slow_down", err)
	}
	if _, err := f.poll(started.PollSecret); !errors.Is(err, identity.ErrAuthorizationPending) {
		t.Fatalf("poll before a decision = %v, want authorization_pending", err)
	}

	view, err := f.svc.LookUpNativeSignIn(ctx, aliceID, portal, strings.ToLower(started.UserCode))
	if err != nil {
		t.Fatalf("LookUpNativeSignIn: %v", err)
	}
	if view.Request.Platform != coreidentity.PlatformCLI || view.Request.ClientLabel != "alice-mbp · macOS" || !view.SessionExpiresAt.Equal(portalExpiry) {
		t.Errorf("view = %+v", view)
	}

	if err := f.svc.ConfirmNativeSignIn(ctx, aliceID, portal, started.UserCode); err != nil {
		t.Fatalf("ConfirmNativeSignIn: %v", err)
	}
	result, err := f.poll(started.PollSecret)
	if err != nil {
		t.Fatalf("poll after confirmation: %v", err)
	}
	if result.User.ID != aliceID || result.AccessToken == "" || result.RefreshToken == "" {
		t.Fatalf("result = %+v", result)
	}
	var native *coreidentity.AuthSession
	for _, s := range f.liveSessions(aliceID) {
		if s.SID == result.SessionID {
			native = &s
		}
	}
	if native == nil {
		t.Fatal("the redeemed session is not live")
	}
	if native.Platform != coreidentity.PlatformCLI || native.ClientLabel != "alice-mbp · macOS" ||
		native.AuthMethod != identity.MethodOIDC || !native.AbsoluteExpiresAt.Equal(portalExpiry) {
		t.Errorf("native session = %+v; want cli, the label, oidc, and expiry %v exactly", native, portalExpiry)
	}

	confirmed := f.events(coreaudit.NativeSignInConfirmed)
	if len(confirmed) != 1 || confirmed[0].ActorID != aliceID || confirmed[0].TargetType != "auth_session" ||
		confirmed[0].TargetID != portal || confirmed[0].Detail != `cli; claimed label "alice-mbp · macOS"` {
		t.Errorf("confirmed events = %+v", confirmed)
	}
	logins := f.events(coreaudit.UserLogin)
	if len(logins) != 1 || logins[0].TargetID != coreidentity.PlatformCLI || logins[0].Detail != identity.LoginDetailPortalConfirmation {
		t.Errorf("login events = %+v", logins)
	}
	for _, e := range f.audit.Events {
		if strings.Contains(e.Detail, coreidentity.NormalizeUserCode(started.UserCode)) || strings.Contains(e.Detail, started.PollSecret) {
			t.Errorf("audit event %s carries the code or secret: %q", e.Action, e.Detail)
		}
	}

	// Redeemed once: the same secret is now indistinguishable from an unknown one.
	if _, err := f.poll(started.PollSecret); !errors.Is(err, identity.ErrExpiredToken) {
		t.Errorf("poll after redemption = %v, want expired_token", err)
	}
}

func TestNativeSignInStartValidates(t *testing.T) {
	f := newNativeFixture(t)
	ctx := context.Background()
	for _, platform := range []string{"", coreidentity.PlatformPortal, "unknown"} {
		if _, err := f.svc.StartNativeSignIn(ctx, identity.StartNativeSignInCmd{Platform: platform}); !errors.Is(err, identity.ErrNativeSignInPlatform) {
			t.Errorf("platform %q: %v, want ErrNativeSignInPlatform", platform, err)
		}
	}
	if _, err := f.svc.StartNativeSignIn(ctx, identity.StartNativeSignInCmd{
		Platform: coreidentity.PlatformDesktop, ClientLabel: "evil‮label",
	}); !errors.Is(err, identity.ErrClientLabelInvalid) {
		t.Errorf("bidi label: %v, want ErrClientLabelInvalid", err)
	}
	if len(f.signIns.Requests) != 0 {
		t.Errorf("a refused start stored %d requests", len(f.signIns.Requests))
	}
}

// TestNativeSignInStartCap: the 501st live pending request is refused; expired
// and decided ones do not count.
func TestNativeSignInStartCap(t *testing.T) {
	f := newNativeFixture(t)
	ctx := context.Background()
	for i := range coreidentity.NativeSignInPendingCap {
		state := coreidentity.NativeSignInPending
		expires := f.now.Add(time.Minute)
		f.signIns.Requests = append(f.signIns.Requests, &mock.MockNativeSignIn{
			UserCodeHash: "code" + string(rune(i)), PollSecretHash: "secret" + string(rune(i)),
			Request: coreidentity.NativeSignIn{State: state, ExpiresAt: expires},
		})
	}
	if _, err := f.svc.StartNativeSignIn(ctx, identity.StartNativeSignInCmd{Platform: coreidentity.PlatformCLI}); !errors.Is(err, identity.ErrTooManyPendingSignIns) {
		t.Fatalf("501st start = %v, want ErrTooManyPendingSignIns", err)
	}
	f.signIns.Requests[0].Request.State = coreidentity.NativeSignInConfirmed
	f.start(coreidentity.PlatformCLI, "")
	if _, err := f.svc.StartNativeSignIn(ctx, identity.StartNativeSignInCmd{Platform: coreidentity.PlatformCLI}); !errors.Is(err, identity.ErrTooManyPendingSignIns) {
		t.Fatalf("start at the cap again = %v", err)
	}
	f.advance(2 * time.Minute)
	f.start(coreidentity.PlatformCLI, "")
}

func TestNativeSignInPollAnswersExpiredToken(t *testing.T) {
	f := newNativeFixture(t)
	ctx := context.Background()
	if _, err := f.svc.PollNativeSignIn(ctx, ""); !errors.Is(err, identity.ErrPollSecretRequired) {
		t.Errorf("empty secret = %v", err)
	}
	if _, err := f.svc.PollNativeSignIn(ctx, "bmxsignin_unknown"); !errors.Is(err, identity.ErrExpiredToken) {
		t.Errorf("unknown secret = %v, want expired_token", err)
	}
	started := f.start(coreidentity.PlatformDesktop, "")
	f.advance(coreidentity.NativeSignInTTL)
	if _, err := f.svc.PollNativeSignIn(ctx, started.PollSecret); !errors.Is(err, identity.ErrExpiredToken) {
		t.Errorf("expired request = %v, want expired_token", err)
	}
}

// TestNativeSignInConfirmOnlyFromPortal: a CLI or Desktop session cannot mint
// another native session, so a stolen native token cannot multiply itself.
func TestNativeSignInConfirmOnlyFromPortal(t *testing.T) {
	f := newNativeFixture(t)
	ctx := context.Background()
	started := f.start(coreidentity.PlatformCLI, "")
	for _, platform := range []string{coreidentity.PlatformCLI, coreidentity.PlatformDesktop, "unknown"} {
		sid := f.session(aliceID, platform, identity.MethodPassword, f.now.Add(time.Hour))
		if err := f.svc.ConfirmNativeSignIn(ctx, aliceID, sid, started.UserCode); !errors.Is(err, identity.ErrNativeSignInPortalOnly) {
			t.Errorf("confirm from %s = %v, want ErrNativeSignInPortalOnly", platform, err)
		}
		if _, err := f.svc.LookUpNativeSignIn(ctx, aliceID, sid, started.UserCode); !errors.Is(err, identity.ErrNativeSignInPortalOnly) {
			t.Errorf("look-up from %s = %v, want ErrNativeSignInPortalOnly", platform, err)
		}
	}
	// Another account's Portal session is not the caller's.
	other := f.session(malletID, coreidentity.PlatformPortal, identity.MethodPassword, f.now.Add(time.Hour))
	if err := f.svc.ConfirmNativeSignIn(ctx, aliceID, other, started.UserCode); !errors.Is(err, identity.ErrNativeSignInPortalOnly) {
		t.Errorf("confirm with another account's session = %v", err)
	}
	if _, err := f.poll(started.PollSecret); !errors.Is(err, identity.ErrAuthorizationPending) {
		t.Errorf("a refused confirmation decided the request: %v", err)
	}
}

// TestNativeSignInConfirmReappliesAdmission: a session opened before an
// operator tightened sign-in cannot launder itself into a fresh native one.
func TestNativeSignInConfirmReappliesAdmission(t *testing.T) {
	cases := []struct {
		name       string
		method     string
		localLogin string
		oidc       bool
		admin      bool
		want       error
	}{
		{"password under all", identity.MethodPassword, identity.LocalLoginAll, false, false, nil},
		{"password under system_admins, not an admin", identity.MethodPassword, identity.LocalLoginSystemAdmins, true, false, identity.ErrNativeSignInReauthenticate},
		{"password under system_admins, an admin", identity.MethodPassword, identity.LocalLoginSystemAdmins, true, true, nil},
		{"login code under off", identity.MethodLoginCode, identity.LocalLoginOff, true, false, identity.ErrNativeSignInReauthenticate},
		{"oidc under off", identity.MethodOIDC, identity.LocalLoginOff, true, false, nil},
		{"oidc after SSO was disabled", identity.MethodOIDC, identity.LocalLoginAll, false, false, identity.ErrNativeSignInReauthenticate},
		{"an unknown method", "magic", identity.LocalLoginAll, true, false, identity.ErrNativeSignInReauthenticate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newNativeFixture(t)
			f.svc.LocalLogin, f.svc.OIDCEnabled = tc.localLogin, tc.oidc
			if tc.admin {
				f.grants.GrantForTest(aliceID, coreidentity.SystemRoleAdmin)
			}
			started := f.start(coreidentity.PlatformCLI, "")
			portal := f.session(aliceID, coreidentity.PlatformPortal, tc.method, f.now.Add(time.Hour))
			err := f.svc.ConfirmNativeSignIn(context.Background(), aliceID, portal, started.UserCode)
			if !errors.Is(err, tc.want) {
				t.Fatalf("confirm = %v, want %v", err, tc.want)
			}
			if tc.want != nil && len(f.events(coreaudit.NativeSignInConfirmed)) != 0 {
				t.Error("a refused confirmation was audited as confirmed")
			}
		})
	}
}

// TestNativeSignInFirstDecisionWins: once decided, a code is not valid for a
// second decision either way, and a denial ends the request for the client.
func TestNativeSignInFirstDecisionWins(t *testing.T) {
	f := newNativeFixture(t)
	ctx := context.Background()
	portal := f.session(aliceID, coreidentity.PlatformPortal, identity.MethodPassword, f.now.Add(time.Hour))

	denied := f.start(coreidentity.PlatformCLI, "")
	if err := f.svc.DenyNativeSignIn(ctx, aliceID, portal, denied.UserCode); err != nil {
		t.Fatalf("deny: %v", err)
	}
	if err := f.svc.ConfirmNativeSignIn(ctx, aliceID, portal, denied.UserCode); !errors.Is(err, identity.ErrNativeSignInCodeInvalid) {
		t.Errorf("confirm after deny = %v, want ErrNativeSignInCodeInvalid", err)
	}
	if _, err := f.svc.LookUpNativeSignIn(ctx, aliceID, portal, denied.UserCode); !errors.Is(err, identity.ErrNativeSignInCodeInvalid) {
		t.Errorf("look-up after deny = %v", err)
	}
	if _, err := f.poll(denied.PollSecret); !errors.Is(err, identity.ErrAccessDenied) {
		t.Errorf("poll after deny = %v, want access_denied", err)
	}
	if got := f.events(coreaudit.NativeSignInDenied); len(got) != 1 || got[0].Detail != coreidentity.PlatformCLI {
		t.Errorf("denied events = %+v", got)
	}

	confirmed := f.start(coreidentity.PlatformDesktop, "")
	if err := f.svc.ConfirmNativeSignIn(ctx, aliceID, portal, confirmed.UserCode); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	for _, decide := range []func(context.Context, string, string, string) error{f.svc.ConfirmNativeSignIn, f.svc.DenyNativeSignIn} {
		if err := decide(ctx, aliceID, portal, confirmed.UserCode); !errors.Is(err, identity.ErrNativeSignInCodeInvalid) {
			t.Errorf("second decision = %v, want ErrNativeSignInCodeInvalid", err)
		}
	}
	for _, code := range []string{"", "ABCD", "WRONG-CODE", "ZZZZ-ZZZZ"} {
		if err := f.svc.ConfirmNativeSignIn(ctx, aliceID, portal, code); !errors.Is(err, identity.ErrNativeSignInCodeInvalid) {
			t.Errorf("confirm %q = %v, want ErrNativeSignInCodeInvalid", code, err)
		}
	}
}

// TestNativeSignInRedemptionRefusals: a confirmation is a decision about a
// moment. An account disabled, or a confirming session revoked or expired,
// before the client redeems it yields access_denied and no session.
func TestNativeSignInRedemptionRefusals(t *testing.T) {
	cases := map[string]func(f *nativeFixture, portal string){
		"account disabled": func(f *nativeFixture, _ string) {
			at := f.now
			f.users.ByID[aliceID].DisabledAt = &at
		},
		"confirming session revoked": func(f *nativeFixture, portal string) {
			if _, err := f.sessions.RevokeSession(context.Background(), portal, f.now); err != nil {
				f.t.Fatal(err)
			}
		},
		// The next poll lands 30s later, past the session's 45s.
		"confirming session expired": func(f *nativeFixture, _ string) {
			f.advance(20 * time.Second)
		},
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			f := newNativeFixture(t)
			ctx := context.Background()
			portal := f.session(aliceID, coreidentity.PlatformPortal, identity.MethodPassword, f.now.Add(45*time.Second))
			started := f.start(coreidentity.PlatformCLI, "")
			if err := f.svc.ConfirmNativeSignIn(ctx, aliceID, portal, started.UserCode); err != nil {
				t.Fatalf("confirm: %v", err)
			}
			before := len(f.sessions.Sessions)
			change(f, portal)
			for range 2 {
				if _, err := f.poll(started.PollSecret); !errors.Is(err, identity.ErrAccessDenied) {
					t.Fatalf("poll = %v, want access_denied", err)
				}
			}
			if len(f.sessions.Sessions) != before {
				t.Errorf("a refused redemption created a session")
			}
			if len(f.events(coreaudit.UserLogin)) != 0 {
				t.Error("a refused redemption was audited as a login")
			}
		})
	}
}

// TestNativeSignInConcurrentPollsRedeemOnce: many polls racing on one
// confirmed request produce exactly one session.
func TestNativeSignInConcurrentPollsRedeemOnce(t *testing.T) {
	f := newNativeFixture(t)
	ctx := context.Background()
	portal := f.session(aliceID, coreidentity.PlatformPortal, identity.MethodPassword, f.now.Add(time.Hour))
	started := f.start(coreidentity.PlatformCLI, "")
	if err := f.svc.ConfirmNativeSignIn(ctx, aliceID, portal, started.UserCode); err != nil {
		t.Fatal(err)
	}
	f.advance(coreidentity.NativeSignInMaxInterval)
	before := len(f.sessions.Sessions)

	var wg sync.WaitGroup
	results := make(chan error, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := f.svc.PollNativeSignIn(ctx, started.PollSecret)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	wins := 0
	for err := range results {
		switch {
		case err == nil:
			wins++
		case errors.Is(err, identity.ErrSlowDown), errors.Is(err, identity.ErrExpiredToken):
		default:
			t.Errorf("poll = %v", err)
		}
	}
	if wins != 1 || len(f.sessions.Sessions) != before+1 {
		t.Errorf("%d polls redeemed, %d sessions created; want exactly one", wins, len(f.sessions.Sessions)-before)
	}
}

// TestLoginRecordsClientLabel: a password-signed native session is as
// recognizable in the session list as a confirmed one.
func TestLoginRecordsClientLabel(t *testing.T) {
	svc := newLoginService(t)
	sessions := svc.Sessions.(*mock.MockAuthSessionStore)
	result, err := svc.Login(context.Background(), identity.LoginCmd{
		Email: "known@example.test", Password: goodPassword, Platform: "desktop", ClientLabel: " alice-mbp · macOS ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := sessions.Sessions[result.SessionID].ClientLabel; got != "alice-mbp · macOS" {
		t.Errorf("session label = %q", got)
	}
	if _, err := svc.Login(context.Background(), identity.LoginCmd{
		Email: "known@example.test", Password: goodPassword, ClientLabel: strings.Repeat("x", 65),
	}); !errors.Is(err, identity.ErrClientLabelInvalid) {
		t.Errorf("long label = %v, want ErrClientLabelInvalid", err)
	}
}
