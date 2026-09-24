package db

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	corechannel "github.com/icloudbb/buildmax/internal/core/channel"
)

// newChatAccount returns a platform account id unique to this run, and removes
// any pairing left for it.
func newChatAccount(t *testing.T, s *Store) string {
	t.Helper()
	id := "tg-" + testPublicID(t)
	t.Cleanup(func() {
		_ = s.db.Delete(&channelPairingRow{}, "external_user_id = ?", id).Error
	})
	return id
}

func pairingFor(externalID string, expires time.Time) corechannel.Pairing {
	return corechannel.Pairing{
		Platform: corechannel.PlatformTelegram, ExternalUserID: externalID,
		ChatID: externalID, Handle: "@ada", ExpiresAt: expires,
	}
}

func TestHashPairingCodeDoesNotStorePlaintext(t *testing.T) {
	if h := hashPairingCode("ABCDEFGH"); strings.Contains(h, "ABCDEFGH") || len(h) != 64 {
		t.Errorf("hash %q", h)
	}
}

func TestChannelPairingLinksOnce(t *testing.T) {
	s, ctx := newTestStore(t)
	userID := newTestUser(t, s, "chatlink")
	ext := newChatAccount(t, s)
	now := time.Now().UTC()
	code := "C" + testPublicID(t)[:7]

	if err := s.CreatePairing(ctx, pairingFor(ext, now.Add(time.Minute)), code); err != nil {
		t.Fatalf("CreatePairing: %v", err)
	}
	var row channelPairingRow
	if err := s.db.Where("external_user_id = ?", ext).Take(&row).Error; err != nil || row.CodeHash == code {
		t.Fatalf("pairing row = %+v, %v; want only the hash stored", row, err)
	}
	p, err := s.PairingByCode(ctx, code, now)
	if err != nil || p == nil || p.Handle != "@ada" || p.ChatID != ext {
		t.Fatalf("PairingByCode = %+v, %v", p, err)
	}

	link, pairing, err := s.ConsumePairing(ctx, code, userID, now)
	if err != nil {
		t.Fatalf("ConsumePairing: %v", err)
	}
	if link.ID == "" || link.UserID != userID || link.ExternalUserID != ext || pairing.ChatID != ext {
		t.Errorf("link = %+v, pairing = %+v", link, pairing)
	}
	if _, _, err := s.ConsumePairing(ctx, code, userID, now); !errors.Is(err, corechannel.ErrPairingNotFound) {
		t.Errorf("spent code = %v, want ErrPairingNotFound", err)
	}
	got, err := s.IdentityByExternal(ctx, corechannel.PlatformTelegram, "", ext)
	if err != nil || got == nil || got.UserID != userID {
		t.Fatalf("IdentityByExternal = %+v, %v", got, err)
	}
	links, err := s.ListIdentitiesByUser(ctx, userID)
	if err != nil || len(links) != 1 || links[0].ID != link.ID {
		t.Errorf("ListIdentitiesByUser = %+v, %v", links, err)
	}

	// Someone else cannot remove the link by naming it.
	if err := s.DeleteIdentity(ctx, newTestUser(t, s, "chatother"), link.ID); !errors.Is(err, apierr.ErrNotFound) {
		t.Errorf("DeleteIdentity by another user = %v, want ErrNotFound", err)
	}
	if err := s.DeleteIdentity(ctx, userID, link.ID); err != nil {
		t.Fatalf("DeleteIdentity: %v", err)
	}
	if got, _ := s.IdentityByExternal(ctx, corechannel.PlatformTelegram, "", ext); got != nil {
		t.Error("the link survived its deletion")
	}
}

// A chat account speaks for one user: linking it to a second needs the first
// link removed, while confirming again for the same user is harmless.
func TestChannelPairingRefusesASecondUser(t *testing.T) {
	s, ctx := newTestStore(t)
	first := newTestUser(t, s, "chatfirst")
	second := newTestUser(t, s, "chatsecond")
	ext := newChatAccount(t, s)
	now := time.Now().UTC()

	code1 := "D" + testPublicID(t)[:7]
	if err := s.CreatePairing(ctx, pairingFor(ext, now.Add(time.Minute)), code1); err != nil {
		t.Fatalf("CreatePairing: %v", err)
	}
	link, _, err := s.ConsumePairing(ctx, code1, first, now)
	if err != nil {
		t.Fatalf("ConsumePairing: %v", err)
	}

	code2 := "E" + testPublicID(t)[:7]
	if err := s.CreatePairing(ctx, pairingFor(ext, now.Add(time.Minute)), code2); err != nil {
		t.Fatalf("CreatePairing: %v", err)
	}
	if _, _, err := s.ConsumePairing(ctx, code2, second, now); !errors.Is(err, corechannel.ErrAlreadyLinked) {
		t.Fatalf("ConsumePairing by a second user = %v, want ErrAlreadyLinked", err)
	}
	again, _, err := s.ConsumePairing(ctx, code2, first, now)
	if err != nil || again.ID != link.ID {
		t.Errorf("reconfirming for the same user = %+v, %v; want the existing link", again, err)
	}
}

func TestChannelPairingExpiresAndIsReplaced(t *testing.T) {
	s, ctx := newTestStore(t)
	userID := newTestUser(t, s, "chatexpire")
	ext := newChatAccount(t, s)
	now := time.Now().UTC()

	old := "F" + testPublicID(t)[:7]
	if err := s.CreatePairing(ctx, pairingFor(ext, now.Add(time.Minute)), old); err != nil {
		t.Fatalf("CreatePairing: %v", err)
	}
	fresh := "G" + testPublicID(t)[:7]
	if err := s.CreatePairing(ctx, pairingFor(ext, now.Add(time.Minute)), fresh); err != nil {
		t.Fatalf("CreatePairing: %v", err)
	}
	if p, _ := s.PairingByCode(ctx, old, now); p != nil {
		t.Error("a replaced code is still redeemable")
	}
	if _, _, err := s.ConsumePairing(ctx, fresh, userID, now.Add(2*time.Minute)); !errors.Is(err, corechannel.ErrPairingNotFound) {
		t.Errorf("expired code = %v, want ErrPairingNotFound", err)
	}
}

// Two confirmations racing on one code make one link.
func TestChannelPairingConsumeIsAtomic(t *testing.T) {
	s, ctx := newTestStore(t)
	userID := newTestUser(t, s, "chatrace")
	ext := newChatAccount(t, s)
	now := time.Now().UTC()
	code := "H" + testPublicID(t)[:7]
	if err := s.CreatePairing(ctx, pairingFor(ext, now.Add(time.Minute)), code); err != nil {
		t.Fatalf("CreatePairing: %v", err)
	}
	var wg sync.WaitGroup
	errs := make([]error, 4)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, errs[i] = s.ConsumePairing(ctx, code, userID, now)
		}()
	}
	wg.Wait()
	won := 0
	for _, err := range errs {
		switch {
		case err == nil:
			won++
		case !errors.Is(err, corechannel.ErrPairingNotFound):
			t.Errorf("racing confirm = %v", err)
		}
	}
	if won != 1 {
		t.Errorf("%d confirmations won, want exactly one", won)
	}
	if links, _ := s.ListIdentitiesByUser(ctx, userID); len(links) != 1 {
		t.Errorf("links = %d, want 1", len(links))
	}
}

// A chat's newest conversation is its current one, and a chat account relinked
// to another user never continues the previous user's conversation.
func TestLatestChatConversation(t *testing.T) {
	s, ctx := newTestStore(t)
	userID := newTestUser(t, s, "chatconv")
	other := newTestUser(t, s, "chatconvother")
	space := newTestSpace(t, s, userID)
	chat := "chat-" + testPublicID(t)

	if got, err := s.LatestChatConversation(ctx, userID, corechannel.PlatformTelegram, chat); err != nil || got != nil {
		t.Fatalf("before any conversation = %+v, %v", got, err)
	}
	first, err := s.CreateChatConversation(ctx, space, userID, corechannel.PlatformTelegram, chat)
	if err != nil {
		t.Fatalf("CreateChatConversation: %v", err)
	}
	second, err := s.CreateChatConversation(ctx, space, userID, corechannel.PlatformTelegram, chat)
	if err != nil {
		t.Fatalf("CreateChatConversation: %v", err)
	}
	got, err := s.LatestChatConversation(ctx, userID, corechannel.PlatformTelegram, chat)
	if err != nil || got == nil || got.ID != second.ID || got.ChannelRef != chat || got.SpaceID != space {
		t.Fatalf("LatestChatConversation = %+v, %v; want the second (%s), not %s", got, err, second.ID, first.ID)
	}
	if read, _ := s.GetConversation(ctx, first.ID); read == nil || read.ChannelRef != chat || read.Channel != corechannel.PlatformTelegram {
		t.Errorf("GetConversation = %+v", read)
	}
	if got, _ := s.LatestChatConversation(ctx, other, corechannel.PlatformTelegram, chat); got != nil {
		t.Error("another user continued this user's chat conversation")
	}
}
