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

const (
	testIssuer  = "https://example.okta.com"
	testSubject = "okta|00u123"
)

// assocService wires the association Service over in-memory doubles. Seed the
// user store first; the identity store shares it so provisioning and disabled
// checks behave like the real store's one transaction.
func assocService(t *testing.T, users *mock.MockUserStore, provisioning string, domains []string) (*identity.Service, *mock.MockExternalIdentityStore) {
	t.Helper()
	eids := &mock.MockExternalIdentityStore{Users: users}
	return &identity.Service{
		Users:               users,
		ExternalIdentities:  eids,
		Provisioning:        provisioning,
		AllowedEmailDomains: domains,
		DefaultQuotaTier:    "free_trial",
	}, eids
}

func seedUser(id, email string) *mock.MockUserStore {
	u := &coreidentity.User{ID: id, Email: email}
	return &mock.MockUserStore{
		ByID:    map[string]*coreidentity.User{id: u},
		ByEmail: map[string]*coreidentity.User{email: u},
	}
}

// TestAssociateRule1ExistingLinkWins pins that a subject already linked resolves
// to its account regardless of the email the IdP now sends — a changed email
// cannot move an identity.
func TestAssociateRule1ExistingLinkWins(t *testing.T) {
	users := seedUser("u_known", "known@example.com")
	svc, eids := assocService(t, users, identity.ProvisioningJIT, []string{"example.com"})
	if _, err := eids.LinkExisting(context.Background(), coreidentity.LinkIdentity{
		UserID: "u_known", Issuer: testIssuer, Subject: testSubject,
	}); err != nil {
		t.Fatalf("seed link: %v", err)
	}

	out, err := svc.Associate(context.Background(), identity.AssociationInput{
		Issuer: testIssuer, Subject: testSubject, VerifiedEmail: "moved@elsewhere.com",
	})
	if err != nil {
		t.Fatalf("Associate: %v", err)
	}
	if out.User.ID != "u_known" || out.Created {
		t.Errorf("resolved to %+v, want the linked account and not created", out)
	}
}

func TestAssociateRule2DisabledLinkedAccountRefused(t *testing.T) {
	users := seedUser("u_off", "off@example.com")
	users.DisableForTest("u_off", time.Now())
	svc, eids := assocService(t, users, identity.ProvisioningJIT, []string{"example.com"})
	if _, err := eids.LinkExisting(context.Background(), coreidentity.LinkIdentity{
		UserID: "u_off", Issuer: testIssuer, Subject: testSubject,
	}); err != nil {
		t.Fatalf("seed link: %v", err)
	}
	_, err := svc.Associate(context.Background(), identity.AssociationInput{
		Issuer: testIssuer, Subject: testSubject, VerifiedEmail: "off@example.com",
	})
	if !errors.Is(err, identity.ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
}

// TestAssociateRule3LinksExistingByEmail is the migration path for
// operator-created accounts.
func TestAssociateRule3LinksExistingByEmail(t *testing.T) {
	users := seedUser("u_made", "made@example.com")
	svc, eids := assocService(t, users, identity.ProvisioningExistingOnly, nil)

	out, err := svc.Associate(context.Background(), identity.AssociationInput{
		Issuer: testIssuer, Subject: testSubject, VerifiedEmail: "made@example.com", Name: "Made",
	})
	if err != nil {
		t.Fatalf("Associate: %v", err)
	}
	if out.User.ID != "u_made" || out.Created {
		t.Errorf("resolved to %+v, want the existing account linked, not created", out)
	}
	link, _ := eids.IdentityBySubject(context.Background(), testIssuer, testSubject)
	if link == nil || link.UserID != "u_made" {
		t.Errorf("no link created for the existing account: %+v", link)
	}
}

// TestAssociateRule4EmailAlreadyLinkedElsewhere refuses rather than moving or
// replacing a link.
func TestAssociateRule4EmailAlreadyLinkedElsewhere(t *testing.T) {
	users := seedUser("u_a", "shared@example.com")
	svc, eids := assocService(t, users, identity.ProvisioningJIT, []string{"example.com"})
	// The account already has a different subject at this issuer.
	if _, err := eids.LinkExisting(context.Background(), coreidentity.LinkIdentity{
		UserID: "u_a", Issuer: testIssuer, Subject: "okta|OLD",
	}); err != nil {
		t.Fatalf("seed link: %v", err)
	}
	_, err := svc.Associate(context.Background(), identity.AssociationInput{
		Issuer: testIssuer, Subject: testSubject, VerifiedEmail: "shared@example.com",
	})
	if !errors.Is(err, identity.ErrIdentityNeedsOperator) {
		t.Fatalf("err = %v, want ErrIdentityNeedsOperator", err)
	}
}

func TestAssociateRule2DisabledByEmailRefused(t *testing.T) {
	users := seedUser("u_off", "off@example.com")
	users.DisableForTest("u_off", time.Now())
	svc, _ := assocService(t, users, identity.ProvisioningJIT, []string{"example.com"})
	_, err := svc.Associate(context.Background(), identity.AssociationInput{
		Issuer: testIssuer, Subject: testSubject, VerifiedEmail: "off@example.com",
	})
	if !errors.Is(err, identity.ErrDisabled) {
		t.Fatalf("err = %v, want ErrDisabled", err)
	}
}

func TestAssociateExistingOnlyRefusesUnknown(t *testing.T) {
	users := &mock.MockUserStore{}
	svc, _ := assocService(t, users, identity.ProvisioningExistingOnly, nil)
	_, err := svc.Associate(context.Background(), identity.AssociationInput{
		Issuer: testIssuer, Subject: testSubject, VerifiedEmail: "nobody@example.com",
	})
	if !errors.Is(err, identity.ErrNotAuthorizedForDeployment) {
		t.Fatalf("err = %v, want ErrNotAuthorizedForDeployment", err)
	}
}

func TestAssociateJITProvisionsAllowedDomain(t *testing.T) {
	users := &mock.MockUserStore{}
	svc, eids := assocService(t, users, identity.ProvisioningJIT, []string{"example.com"})
	out, err := svc.Associate(context.Background(), identity.AssociationInput{
		Issuer: testIssuer, Subject: testSubject, VerifiedEmail: "new@example.com", Name: "New",
	})
	if err != nil {
		t.Fatalf("Associate: %v", err)
	}
	if !out.Created || out.User.Email != "new@example.com" {
		t.Errorf("resolved to %+v, want a created account", out)
	}
	if out.User.QuotaTier != "free_trial" {
		t.Errorf("quota tier = %q, want the deployment default", out.User.QuotaTier)
	}
	if link, _ := eids.IdentityBySubject(context.Background(), testIssuer, testSubject); link == nil {
		t.Error("JIT did not create the identity link")
	}
}

func TestAssociateJITRefusesDisallowedDomain(t *testing.T) {
	users := &mock.MockUserStore{}
	svc, _ := assocService(t, users, identity.ProvisioningJIT, []string{"example.com"})
	_, err := svc.Associate(context.Background(), identity.AssociationInput{
		Issuer: testIssuer, Subject: testSubject, VerifiedEmail: "new@evil.com",
	})
	if !errors.Is(err, identity.ErrNotAuthorizedForDeployment) {
		t.Fatalf("err = %v, want ErrNotAuthorizedForDeployment", err)
	}
}

// TestAssociateJITEmptyDomainsRefuses is the safety the design rests JIT on: an
// empty allow-list means "nobody", not "everybody".
func TestAssociateJITEmptyDomainsRefuses(t *testing.T) {
	users := &mock.MockUserStore{}
	svc, _ := assocService(t, users, identity.ProvisioningJIT, nil)
	_, err := svc.Associate(context.Background(), identity.AssociationInput{
		Issuer: testIssuer, Subject: testSubject, VerifiedEmail: "new@example.com",
	})
	if !errors.Is(err, identity.ErrNotAuthorizedForDeployment) {
		t.Fatalf("err = %v, want a refusal for an unbounded JIT", err)
	}
}

// TestAssociateDomainMatchIsCanonicalAndExact covers the two ways a loose match
// would be wrong: case, and a suffix that is not the domain.
func TestAssociateDomainMatchIsCanonicalAndExact(t *testing.T) {
	t.Run("case-insensitive domain, exact", func(t *testing.T) {
		users := &mock.MockUserStore{}
		svc, _ := assocService(t, users, identity.ProvisioningJIT, []string{"Example.COM"})
		out, err := svc.Associate(context.Background(), identity.AssociationInput{
			Issuer: testIssuer, Subject: testSubject, VerifiedEmail: "Person@Example.com",
		})
		if err != nil || !out.Created {
			t.Fatalf("err = %v, out = %+v; want a created account", err, out)
		}
	})
	t.Run("a lookalike suffix is not the domain", func(t *testing.T) {
		users := &mock.MockUserStore{}
		svc, _ := assocService(t, users, identity.ProvisioningJIT, []string{"example.com"})
		_, err := svc.Associate(context.Background(), identity.AssociationInput{
			Issuer: testIssuer, Subject: testSubject, VerifiedEmail: "u@notexample.com",
		})
		if !errors.Is(err, identity.ErrNotAuthorizedForDeployment) {
			t.Fatalf("err = %v, want refusal; suffix match must not pass", err)
		}
	})
}

func TestAssociateRefusesWhenSSONotConfigured(t *testing.T) {
	svc := &identity.Service{Users: &mock.MockUserStore{}}
	_, err := svc.Associate(context.Background(), identity.AssociationInput{
		Issuer: testIssuer, Subject: testSubject, VerifiedEmail: "a@example.com",
	})
	if !errors.Is(err, identity.ErrSSONotConfigured) {
		t.Fatalf("err = %v, want ErrSSONotConfigured", err)
	}
}
