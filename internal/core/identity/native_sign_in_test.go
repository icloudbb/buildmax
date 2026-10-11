package identity

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestUserCodeFormat(t *testing.T) {
	if got := NormalizeUserCode(" abcd-efgh "); got != "ABCDEFGH" {
		t.Errorf("NormalizeUserCode = %q", got)
	}
	seen := map[string]bool{}
	for range 200 {
		code, err := NewUserCode()
		if err != nil {
			t.Fatal(err)
		}
		if len(code) != UserCodeLength || strings.ContainsAny(code, "01OIL") || strings.Trim(code, userCodeAlphabet) != "" {
			t.Fatalf("NewUserCode = %q", code)
		}
		seen[code] = true
		shown := DisplayUserCode(code)
		if len(shown) != 9 || shown[4] != '-' || NormalizeUserCode(strings.ToLower(shown)) != code {
			t.Fatalf("DisplayUserCode(%q) = %q does not round-trip", code, shown)
		}
	}
	if len(seen) < 199 {
		t.Errorf("200 draws gave %d distinct codes", len(seen))
	}
}

func TestNativeSignInPollSecret(t *testing.T) {
	a, err := NewNativeSignInPollSecret()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewNativeSignInPollSecret()
	if a == b {
		t.Fatal("two poll secrets are equal")
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(a, "bmxsignin_"))
	if !strings.HasPrefix(a, "bmxsignin_") || err != nil || len(raw) != 32 {
		t.Errorf("poll secret %q: want bmxsignin_ and 32 random bytes in base64url (%v)", a, err)
	}
}

// TestNativeSignInPollPacing is RFC 8628 §3.5: a poll earlier than the stored
// interval is slow_down and adds 5 seconds, up to 30.
func TestNativeSignInPollPacing(t *testing.T) {
	t0 := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	r := &NativeSignIn{CreatedAt: t0, Interval: NativeSignInInterval}

	if !r.Poll(t0.Add(2 * time.Second)) {
		t.Fatal("a first poll 2s after start was not slow_down")
	}
	if r.Interval != 10*time.Second {
		t.Fatalf("interval after slow_down = %v, want 10s", r.Interval)
	}
	at := t0.Add(2 * time.Second)
	if at = at.Add(10 * time.Second); r.Poll(at) {
		t.Fatal("a poll a full interval later was slow_down")
	}
	if r.Interval != 10*time.Second || !r.LastPolledAt.Equal(at) {
		t.Fatalf("an on-time poll changed the interval to %v, last %v", r.Interval, r.LastPolledAt)
	}
	for range 10 {
		at = at.Add(time.Second)
		r.Poll(at)
	}
	if r.Interval != NativeSignInMaxInterval {
		t.Fatalf("interval after repeated early polls = %v, want the 30s cap", r.Interval)
	}
}

func TestNativeSignInRedeemableBy(t *testing.T) {
	now := time.Date(2026, 10, 11, 9, 0, 0, 0, time.UTC)
	disabledAt := now.Add(-time.Minute)
	confirmed := func() *NativeSignIn {
		return &NativeSignIn{
			State: NativeSignInConfirmed, DecidedByUserID: "u1", DecidedSessionID: "s1",
			ExpiresAt: now.Add(time.Minute),
		}
	}
	user := &User{ID: "u1", Kind: KindHuman}
	session := &AuthSession{SID: "s1", UserID: "u1", Platform: PlatformPortal, AbsoluteExpiresAt: now.Add(time.Hour)}

	if !confirmed().RedeemableBy(user, session, now) {
		t.Fatal("a confirmed request with an active account and session is not redeemable")
	}
	cases := map[string]func() (*NativeSignIn, *User, *AuthSession){
		"pending": func() (*NativeSignIn, *User, *AuthSession) {
			r := confirmed()
			r.State = NativeSignInPending
			return r, user, session
		},
		"denied": func() (*NativeSignIn, *User, *AuthSession) {
			r := confirmed()
			r.State = NativeSignInDenied
			return r, user, session
		},
		"expired": func() (*NativeSignIn, *User, *AuthSession) {
			r := confirmed()
			r.ExpiresAt = now
			return r, user, session
		},
		"account gone": func() (*NativeSignIn, *User, *AuthSession) { return confirmed(), nil, session },
		"account disabled": func() (*NativeSignIn, *User, *AuthSession) {
			return confirmed(), &User{ID: "u1", DisabledAt: &disabledAt}, session
		},
		"service account": func() (*NativeSignIn, *User, *AuthSession) {
			return confirmed(), &User{ID: "u1", Kind: KindService}, session
		},
		"another account": func() (*NativeSignIn, *User, *AuthSession) { return confirmed(), &User{ID: "u2"}, session },
		"session revoked": func() (*NativeSignIn, *User, *AuthSession) { return confirmed(), user, nil },
		"session expired": func() (*NativeSignIn, *User, *AuthSession) {
			s := *session
			s.AbsoluteExpiresAt = now
			return confirmed(), user, &s
		},
		"another session": func() (*NativeSignIn, *User, *AuthSession) {
			s := *session
			s.SID = "s2"
			return confirmed(), user, &s
		},
		"session of another account": func() (*NativeSignIn, *User, *AuthSession) {
			s := *session
			s.UserID = "u2"
			return confirmed(), user, &s
		},
	}
	for name, build := range cases {
		r, u, s := build()
		if r.RedeemableBy(u, s, now) {
			t.Errorf("%s: redeemable", name)
		}
	}
}

// TestNativeSignInSessionInherits pins §14.5: the native session never
// outlives the sign-in that confirmed it.
func TestNativeSignInSessionInherits(t *testing.T) {
	expiry := time.Date(2026, 10, 11, 21, 0, 0, 123456000, time.UTC)
	r := &NativeSignIn{Platform: PlatformDesktop, ClientLabel: "alice-mbp · macOS"}
	got := r.SessionFor(&AuthSession{
		SID: "s1", UserID: "u1", Platform: PlatformPortal, AuthMethod: "oidc", AbsoluteExpiresAt: expiry,
	})
	want := NewAuthSession{
		UserID: "u1", Platform: PlatformDesktop, AuthMethod: "oidc",
		ClientLabel: "alice-mbp · macOS", AbsoluteExpiresAt: expiry,
	}
	if got != want {
		t.Errorf("SessionFor = %+v, want %+v", got, want)
	}
}

func TestNormalizeClientLabel(t *testing.T) {
	ok := map[string]string{
		"":                      "",
		"  alice-mbp · macOS  ": "alice-mbp · macOS",
		strings.Repeat("界", 64): strings.Repeat("界", 64),
	}
	for in, want := range ok {
		if got, err := NormalizeClientLabel(in); err != nil || got != want {
			t.Errorf("NormalizeClientLabel(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{
		strings.Repeat("a", 65),
		"alice\nmallory",
		"alice\x00",
		"alice‮mallory", // a bidi override reorders what the person reads
		"alice​mallory", // a zero-width space hides a difference
		"\xff\xfe",
	} {
		if _, err := NormalizeClientLabel(bad); !errors.Is(err, ErrClientLabelInvalid) {
			t.Errorf("NormalizeClientLabel(%q) = %v, want ErrClientLabelInvalid", bad, err)
		}
	}
}

func TestValidNativePlatform(t *testing.T) {
	for _, p := range []string{PlatformCLI, PlatformDesktop} {
		if !ValidNativePlatform(p) {
			t.Errorf("%q refused", p)
		}
	}
	for _, p := range []string{PlatformPortal, "", "CLI", "unknown"} {
		if ValidNativePlatform(p) {
			t.Errorf("%q accepted", p)
		}
	}
}
