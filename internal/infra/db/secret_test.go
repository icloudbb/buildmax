package db

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/core/apierr"
	coresecret "github.com/icloudbb/buildmax/internal/core/secret"
)

// The db Store must satisfy the domain contract.
var _ coresecret.Store = (*Store)(nil)

// secretTestSpace creates a throwaway user and returns its id and personal space
// id, registering cleanup of the user (and its space) plus any secret rows the
// test leaves behind.
func secretTestSpace(t *testing.T, s *Store, email string) (userID, spaceID string) {
	t.Helper()
	ctx := context.Background()
	if existing, _ := s.UserByEmail(ctx, email); existing != nil {
		deleteTestUser(t, s, existing.ID)
	}
	u, err := s.CreateUser(ctx, email, "free_trial")
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	space, err := s.GetPersonalSpaceByUser(ctx, u.ID)
	if err != nil {
		t.Fatalf("GetPersonalSpaceByUser: %v", err)
	}
	t.Cleanup(func() {
		s.db.Where("space_id IN (SELECT id FROM space WHERE public_id = ?)", space.ID).Delete(&secretRow{})
		deleteTestUser(t, s, u.ID)
	})
	return u.ID, space.ID
}

func sealedFixture(cipher, nonce, wrapped []byte, keyID string) coresecret.Sealed {
	return coresecret.Sealed{Ciphertext: cipher, Nonce: nonce, WrappedDEK: wrapped, KeyID: keyID}
}

func TestSecretStore_CreateGetSealedRoundTrip(t *testing.T) {
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn, Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	userID, spaceID := secretTestSpace(t, s, "secret-roundtrip@example.com")

	sealed := sealedFixture([]byte("cipher-bytes"), []byte("nonce12bytes"), []byte("wrapped-dek"), "file:root:1")
	created, err := s.CreateSecret(ctx, coresecret.CreateInput{
		SpaceID:     spaceID,
		Name:        "aws-prod",
		Description: "prod read-only",
		CreatedBy:   userID,
		ItemNames:   []string{"access_key_id", "secret_access_key", "region"},
		Sealed:      sealed,
	})
	if err != nil {
		t.Fatalf("CreateSecret: %v", err)
	}
	if created.ID == "" || created.SpaceID != spaceID || created.CreatedBy != userID {
		t.Fatalf("created secret = %+v", created)
	}
	if created.Provider != coresecret.ProviderEmbedded || created.State != coresecret.StateActive {
		t.Fatalf("provider/state = %q/%q", created.Provider, created.State)
	}
	if len(created.ItemNames) != 3 {
		t.Fatalf("item names = %v", created.ItemNames)
	}

	// GetSecret carries metadata only -- the type has no sealed field, so this
	// asserts the read path returns item names without touching ciphertext.
	got, err := s.GetSecret(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetSecret: %v", err)
	}
	if got.Name != "aws-prod" || len(got.ItemNames) != 3 {
		t.Fatalf("GetSecret = %+v", got)
	}

	// GetSealed returns the exact sealed bytes.
	_, gotSealed, err := s.GetSealed(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetSealed: %v", err)
	}
	if string(gotSealed.Ciphertext) != "cipher-bytes" || gotSealed.KeyID != "file:root:1" {
		t.Fatalf("GetSealed = %+v", gotSealed)
	}
}

func TestSecretStore_UpdateItemsRewritesWhole(t *testing.T) {
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn, Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	userID, spaceID := secretTestSpace(t, s, "secret-update@example.com")

	created, err := s.CreateSecret(ctx, coresecret.CreateInput{
		SpaceID: spaceID, Name: "gh", CreatedBy: userID,
		ItemNames: []string{"token"},
		Sealed:    sealedFixture([]byte("v1"), []byte("n1"), []byte("w1"), "file:root:1"),
	})
	if err != nil {
		t.Fatalf("CreateSecret: %v", err)
	}

	if _, err := s.UpdateItems(ctx, coresecret.UpdateItemsInput{
		ID:        created.ID,
		ItemNames: []string{"token", "host"},
		Sealed:    sealedFixture([]byte("v2"), []byte("n2"), []byte("w2"), "file:root:2"),
	}); err != nil {
		t.Fatalf("UpdateItems: %v", err)
	}

	got, sealed, err := s.GetSealed(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetSealed: %v", err)
	}
	if len(got.ItemNames) != 2 || string(sealed.Ciphertext) != "v2" || sealed.KeyID != "file:root:2" {
		t.Fatalf("after update: names=%v sealed=%+v", got.ItemNames, sealed)
	}
}

// A miss is nil, not an error: GetSecret and GetSealed follow the getter
// convention (a method that returns a value signals absence with a nil value,
// not ErrNotFound). The destroyed-Secret refusal is exercised separately.
func TestSecretStore_MissIsNil(t *testing.T) {
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn, Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if sec, err := s.GetSecret(ctx, "sec_does_not_exist"); err != nil || sec != nil {
		t.Fatalf("GetSecret miss = %+v, %v; want nil, nil", sec, err)
	}
	sec, sealed, err := s.GetSealed(ctx, "sec_does_not_exist")
	if err != nil || sec != nil || sealed != nil {
		t.Fatalf("GetSealed miss = %+v, %+v, %v; want nil, nil, nil", sec, sealed, err)
	}
}

func TestSecretStore_DestroyClearsMaterial(t *testing.T) {
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn, Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	userID, spaceID := secretTestSpace(t, s, "secret-destroy@example.com")

	created, err := s.CreateSecret(ctx, coresecret.CreateInput{
		SpaceID: spaceID, Name: "temp", CreatedBy: userID,
		ItemNames: []string{"token"},
		Sealed:    sealedFixture([]byte("v1"), []byte("n1"), []byte("w1"), "file:root:1"),
	})
	if err != nil {
		t.Fatalf("CreateSecret: %v", err)
	}

	// Disable first: still readable as sealed, just not grantable (state check
	// lives above the store).
	if _, err := s.SetState(ctx, created.ID, coresecret.StateDisabled); err != nil {
		t.Fatalf("SetState disabled: %v", err)
	}
	got, err := s.GetSecret(ctx, created.ID)
	if err != nil || got.State != coresecret.StateDisabled {
		t.Fatalf("after disable: %+v err=%v", got, err)
	}

	// Destroy: the row stays for audit, but GetSealed refuses -- the material
	// is gone.
	if _, err := s.SetState(ctx, created.ID, coresecret.StateDestroyed); err != nil {
		t.Fatalf("SetState destroyed: %v", err)
	}
	if _, _, err := s.GetSealed(ctx, created.ID); !errors.Is(err, apierr.ErrNotFound) {
		t.Fatalf("GetSealed after destroy = %v, want ErrNotFound", err)
	}
	// Metadata still resolves.
	if meta, err := s.GetSecret(ctx, created.ID); err != nil || meta.State != coresecret.StateDestroyed {
		t.Fatalf("metadata after destroy: %+v err=%v", meta, err)
	}
}

func TestSecretStore_SpaceScopeAndUniqueness(t *testing.T) {
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn, Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	userA, spaceA := secretTestSpace(t, s, "secret-scope-a@example.com")
	_, spaceB := secretTestSpace(t, s, "secret-scope-b@example.com")

	mk := func(space, name string) error {
		_, err := s.CreateSecret(ctx, coresecret.CreateInput{
			SpaceID: space, Name: name, CreatedBy: userA,
			ItemNames: []string{"k"},
			Sealed:    sealedFixture([]byte("c"), []byte("n"), []byte("w"), "file:root:1"),
		})
		return err
	}
	if err := mk(spaceA, "dup"); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if err := mk(spaceA, "dup"); err == nil {
		t.Fatal("second create with same (space,name) should fail")
	}
	// Same name in another space is fine.
	if err := mk(spaceB, "dup"); err != nil {
		t.Fatalf("same name in another space: %v", err)
	}

	// A listing is scoped to its space.
	listA, err := s.ListSecretsBySpace(ctx, spaceA)
	if err != nil {
		t.Fatalf("ListSecretsBySpace A: %v", err)
	}
	for _, sec := range listA {
		if sec.SpaceID != spaceA {
			t.Fatalf("space A listing leaked %s from %s", sec.ID, sec.SpaceID)
		}
	}
}
