package systemadmin

import (
	"context"
	"errors"
	"testing"

	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	"github.com/icloudbb/buildmax/internal/mock"
)

// TestGrantRefusesAServiceAccount: a system role is reached through a
// signed-in session, which a service account never has.
func TestGrantRefusesAServiceAccount(t *testing.T) {
	svc, _ := newService(t)
	users := svc.Users.(*mock.MockUserStore)
	users.ByID["u_svc"] = &coreidentity.User{ID: "u_svc", Kind: coreidentity.KindService}

	_, err := svc.Grant(context.Background(), "u_svc", coreidentity.SystemRoleAdmin, coreaudit.UserActor("u_admin"))
	if !errors.Is(err, ErrServiceAccount) {
		t.Fatalf("err = %v, want ErrServiceAccount", err)
	}
}
