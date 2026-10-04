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

// A service account has no email, password, code, link, or session in the real
// store, so no sign-in path can reach it there. These tests seed one with every
// credential a person could hold, so each path is shown refusing the kind
// itself, with the refusal it gives anyone else.
func serviceAccount() *coreidentity.User {
	return &coreidentity.User{ID: "u_svc", Email: "svc@example.test", Kind: coreidentity.KindService}
}

func TestPasswordLoginRefusesAServiceAccount(t *testing.T) {
	svc := newLoginService(t)
	u := serviceAccount()
	users := svc.Users.(*mock.MockUserStore)
	users.ByEmail[u.Email], users.ByID[u.ID] = u, u
	hash, err := coreidentity.HashPassword(goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	svc.Passwords.(*mock.MockPasswordStore).Hashes[u.ID] = hash

	_, err = svc.Login(context.Background(), identity.LoginCmd{Email: u.Email, Password: goodPassword})
	var invalid *identity.InvalidCredential
	if !errors.As(err, &invalid) || invalid.Error() != "invalid password" {
		t.Fatalf("err = %v, want the ordinary invalid password", err)
	}
}

func TestLoginCodeRefusesAServiceAccount(t *testing.T) {
	u := serviceAccount()
	codes := &mock.MockLoginCodeStore{}
	code, _, err := codes.CreateLoginCode(context.Background(), u.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	svc := &identity.Service{
		Users: &mock.MockUserStore{
			ByEmail: map[string]*coreidentity.User{u.Email: u},
			ByID:    map[string]*coreidentity.User{u.ID: u},
		},
		LoginCodes: codes,
		Tokens:     fixedIssuer{},
	}
	_, err = svc.Login(context.Background(), identity.LoginCmd{Email: u.Email, Otp: code})
	var invalid *identity.InvalidCredential
	if !errors.As(err, &invalid) || invalid.Method != identity.MethodLoginCode {
		t.Fatalf("err = %v, want the ordinary invalid login code", err)
	}
	if codes.Codes[code].Used {
		t.Error("the code was spent on an account that can never use it")
	}
}

func TestRefreshRefusesAServiceAccount(t *testing.T) {
	u := serviceAccount()
	svc, sessions, _, plaintext := newRefreshService(t, u)
	_, err := svc.Refresh(context.Background(), plaintext)
	var invalid *identity.InvalidRefresh
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want *InvalidRefresh", err)
	}
	if n, _ := sessions.CountUserSessions(context.Background(), u.ID, time.Now()); n != 0 {
		t.Errorf("%d session(s) survived; a refused refresh ends its session", n)
	}
}

func TestSSORefusesAServiceAccount(t *testing.T) {
	t.Run("an existing link", func(t *testing.T) {
		u := serviceAccount()
		users := &mock.MockUserStore{ByID: map[string]*coreidentity.User{u.ID: u}}
		svc, eids := assocService(t, users, identity.ProvisioningJIT, []string{"example.test"})
		if _, err := eids.LinkExisting(context.Background(), coreidentity.LinkIdentity{
			UserID: u.ID, Issuer: testIssuer, Subject: testSubject,
		}); err != nil {
			t.Fatal(err)
		}
		_, err := svc.Associate(context.Background(), identity.AssociationInput{
			Issuer: testIssuer, Subject: testSubject, VerifiedEmail: u.Email,
		})
		if !errors.Is(err, identity.ErrNotAuthorizedForDeployment) {
			t.Fatalf("err = %v, want ErrNotAuthorizedForDeployment", err)
		}
	})
	t.Run("a first sign-in matching by email", func(t *testing.T) {
		u := serviceAccount()
		users := &mock.MockUserStore{
			ByID:    map[string]*coreidentity.User{u.ID: u},
			ByEmail: map[string]*coreidentity.User{u.Email: u},
		}
		svc, eids := assocService(t, users, identity.ProvisioningJIT, []string{"example.test"})
		_, err := svc.Associate(context.Background(), identity.AssociationInput{
			Issuer: testIssuer, Subject: testSubject, VerifiedEmail: u.Email,
		})
		if !errors.Is(err, identity.ErrNotAuthorizedForDeployment) {
			t.Fatalf("err = %v, want ErrNotAuthorizedForDeployment", err)
		}
		if link, _ := eids.IdentityBySubject(context.Background(), testIssuer, testSubject); link != nil {
			t.Fatalf("a link to a service account was written: %+v", link)
		}
	})
}
