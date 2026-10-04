package channel

import (
	"context"
	"errors"
	"testing"
	"time"

	corechannel "github.com/icloudbb/buildmax/internal/core/channel"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
)

// serviceUsers reports every account as a service account that signed in just
// now, so only the kind can be what refuses it.
type serviceUsers struct{}

func (serviceUsers) GetUser(_ context.Context, userID string) (*coreidentity.User, error) {
	now := time.Now()
	return &coreidentity.User{ID: userID, Kind: coreidentity.KindService, LastLoginAt: &now}, nil
}

// TestConfirmPairingRefusesAServiceAccount: a service account is never a
// requester, so confirming a code for one is refused the way a stale sign-in
// is, and no link is written.
func TestConfirmPairingRefusesAServiceAccount(t *testing.T) {
	h := newHarness(t)
	h.send(dm("hello"))
	code := h.ids.onlyCode()
	h.g.users = serviceUsers{}

	if _, err := h.g.ConfirmPairing(context.Background(), adaID, displayCode(code)); !errors.Is(err, corechannel.ErrSignInRequired) {
		t.Fatalf("ConfirmPairing = %v, want ErrSignInRequired", err)
	}
	if links, _ := h.ids.ListIdentitiesByUser(context.Background(), adaID); len(links) != 0 {
		t.Errorf("a service account was linked: %+v", links)
	}
}
