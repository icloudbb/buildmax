package spacerecovery_test

import (
	"context"
	"errors"
	"testing"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	"github.com/icloudbb/buildmax/internal/service/spacerecovery"
)

// TestRecoverRefusesAServiceAccountSuccessor: a service account is a member
// for life and never owns a Space.
func TestRecoverRefusesAServiceAccountSuccessor(t *testing.T) {
	users, spaces := scenario()
	users.ByID["successor"].Kind = coreidentity.KindService
	svc := &spacerecovery.Service{Spaces: spaces, Users: users}

	_, err := svc.RecoverOwnership(context.Background(), spacerecovery.RecoverCmd{SpaceID: space, SuccessorID: "successor"})
	if !errors.Is(err, spacerecovery.ErrSuccessorIsService) {
		t.Fatalf("err = %v, want ErrSuccessorIsService", err)
	}
}
