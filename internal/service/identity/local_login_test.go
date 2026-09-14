package identity_test

import (
	"context"
	"errors"
	"testing"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/identity"
)

// localLoginService is newLoginService with a chosen local_login mode and an
// optional admin grant for the known account.
func localLoginService(t *testing.T, mode string, knownIsAdmin bool) *identity.Service {
	t.Helper()
	svc := newLoginService(t)
	svc.LocalLogin = mode
	grants := &mock.MockSystemGrantStore{}
	if knownIsAdmin {
		grants.GrantForTest("u_known", coreidentity.SystemRoleAdmin)
	}
	svc.Grants = grants
	return svc
}

// TestLocalLoginOffRefusesEvenAValidCredential pins that turning native login
// off refuses an account that would otherwise succeed.
func TestLocalLoginOffRefusesEvenAValidCredential(t *testing.T) {
	svc := localLoginService(t, identity.LocalLoginOff, false)
	_, err := svc.Login(context.Background(), identity.LoginCmd{Email: "known@example.test", Password: goodPassword})
	if !errors.Is(err, identity.ErrLocalLoginNotAllowed) {
		t.Fatalf("err = %v, want ErrLocalLoginNotAllowed", err)
	}
}

// TestLocalLoginSystemAdminsAdmitsOnlyAdmins is the break-glass rule: an admin
// gets in with a password, a non-admin with the same valid credential does not.
func TestLocalLoginSystemAdminsAdmitsOnlyAdmins(t *testing.T) {
	t.Run("an admin is admitted", func(t *testing.T) {
		svc := localLoginService(t, identity.LocalLoginSystemAdmins, true)
		if _, err := svc.Login(context.Background(), identity.LoginCmd{
			Email: "known@example.test", Password: goodPassword,
		}); err != nil {
			t.Fatalf("an admin should sign in: %v", err)
		}
	})
	t.Run("a non-admin with a valid credential is refused", func(t *testing.T) {
		svc := localLoginService(t, identity.LocalLoginSystemAdmins, false)
		_, err := svc.Login(context.Background(), identity.LoginCmd{
			Email: "known@example.test", Password: goodPassword,
		})
		if !errors.Is(err, identity.ErrLocalLoginNotAllowed) {
			t.Fatalf("err = %v, want ErrLocalLoginNotAllowed", err)
		}
	})
}

// TestLocalLoginSystemAdminsRefusesAWrongCredentialAsUsual keeps the mode from
// turning into an existence oracle: a wrong password still reads as a bad
// credential, not "not allowed", so it says nothing about the account.
func TestLocalLoginSystemAdminsRefusesAWrongCredentialAsUsual(t *testing.T) {
	svc := localLoginService(t, identity.LocalLoginSystemAdmins, true)
	_, err := svc.Login(context.Background(), identity.LoginCmd{
		Email: "known@example.test", Password: "wrong but long enough",
	})
	var invalid *identity.InvalidCredential
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want *InvalidCredential before the local-login check", err)
	}
}

// TestLocalLoginAllIsUnchanged confirms the default admits everyone.
func TestLocalLoginAllIsUnchanged(t *testing.T) {
	svc := localLoginService(t, identity.LocalLoginAll, false)
	if _, err := svc.Login(context.Background(), identity.LoginCmd{
		Email: "known@example.test", Password: goodPassword,
	}); err != nil {
		t.Fatalf("all should admit any valid credential: %v", err)
	}
}
