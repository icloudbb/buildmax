package identity_test

import (
	"context"
	"errors"
	"testing"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/identity"
)

type fixedIssuer struct{}

func (fixedIssuer) Mint(string, string, time.Time) (string, time.Duration, error) {
	return "access-token", time.Hour, nil
}

const goodPassword = "correct horse battery staple"

func newLoginService(t *testing.T) *identity.Service {
	t.Helper()
	hash, err := coreidentity.HashPassword(goodPassword)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	known := &coreidentity.User{ID: "u_known", Email: "known@example.test"}
	noPassword := &coreidentity.User{ID: "u_nopass", Email: "nopass@example.test"}
	return &identity.Service{
		Users: &mock.MockUserStore{
			ByEmail: map[string]*coreidentity.User{
				known.Email:      known,
				noPassword.Email: noPassword,
			},
			ByID: map[string]*coreidentity.User{known.ID: known, noPassword.ID: noPassword},
		},
		Passwords: &mock.MockPasswordStore{Hashes: map[string]string{known.ID: hash}},
		Tokens:    fixedIssuer{},
		Sessions:  &mock.MockAuthSessionStore{},
	}
}

// TestEveryPasswordFailureArrivesAsOneThing is the assertion the transport
// cannot make.
//
// The handler answers one status and one message for all three, and its tests
// check that. They cannot check that the three are the same *value* by the time
// they get there -- a service that returned a distinguishable error for each
// would still let the handler flatten them, and the next caller of this service
// would get three answers to "is this address registered".
func TestEveryPasswordFailureArrivesAsOneThing(t *testing.T) {
	svc := newLoginService(t)
	cases := []struct {
		name  string
		email string
	}{
		{"wrong password", "known@example.test"},
		{"no account", "nobody@example.test"},
		{"account with no password", "nopass@example.test"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Login(context.Background(), identity.LoginCmd{
				Email: tc.email, Password: "wrong but long enough",
			})
			var invalid *identity.InvalidCredential
			if !errors.As(err, &invalid) {
				t.Fatalf("err = %v (%T), want *InvalidCredential", err, err)
			}
			if invalid.Method != identity.MethodPassword {
				t.Errorf("method = %q, want %q", invalid.Method, identity.MethodPassword)
			}
			if invalid.Error() != "invalid password" {
				t.Errorf("Error() = %q; the three failures must not read differently", invalid.Error())
			}
		})
	}
}

// TestAFailureCarriesItsReasonWithoutPublishingIt keeps the operator's half.
// The reason is on the error for a log line; nothing about it reaches Error(),
// which is what a careless transport would put in a response body.
func TestAFailureCarriesItsReasonWithoutPublishingIt(t *testing.T) {
	svc := newLoginService(t)
	_, err := svc.Login(context.Background(), identity.LoginCmd{
		Email: "known@example.test", Password: "wrong but long enough",
	})
	var invalid *identity.InvalidCredential
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want *InvalidCredential", err)
	}
	if invalid.Reason == "" {
		t.Error("no reason for the log")
	}
	if invalid.Error() == invalid.Reason {
		t.Error("the reason reached Error(), which is what a response body prints")
	}
}

// TestADisabledAccountIsRefusedAfterTheCredential pins the order. Refusing
// earlier would answer an unauthenticated caller that the address is
// registered; refusing later, with the credential proven, tells the person
// whose account it is why they cannot get in.
func TestADisabledAccountIsRefusedAfterTheCredential(t *testing.T) {
	svc := newLoginService(t)
	users := svc.Users.(*mock.MockUserStore)
	disabled := time.Now().UTC()
	users.ByEmail["known@example.test"].DisabledAt = &disabled

	// Right password: the account is disabled and hears so.
	if _, err := svc.Login(context.Background(), identity.LoginCmd{
		Email: "known@example.test", Password: goodPassword,
	}); !errors.Is(err, identity.ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
	// Wrong password on the same disabled account: still just a bad credential,
	// so the refusal says nothing about the account existing.
	_, err := svc.Login(context.Background(), identity.LoginCmd{
		Email: "known@example.test", Password: "wrong but long enough",
	})
	var invalid *identity.InvalidCredential
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want *InvalidCredential rather than a disabled answer", err)
	}
}
