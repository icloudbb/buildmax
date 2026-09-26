package db

import (
	"context"
	"fmt"
)

// The rows sealed under the deployment KEK, seen as a whole: which keys they
// depend on, and moving them to the current key. Two tables hold sealed data.
// A Space Secret row names its KEK in the plaintext key_id column. A managed
// model credential keeps its key id inside the sealed blob, the one place it is
// written; the catalog is operator-curated and small, so reading its blobs costs
// less than a second copy of the key id that could disagree with the first.
// See docs/design/space-secrets.md §9.1.

// rewrapBatch bounds one read of the walk, so a large table is moved without
// holding a long transaction or a large result set.
const rewrapBatch = 200

// KeyRewrapper moves a sealed DEK to the current KEK. It is an interface so this
// package does not import the crypto implementation; internal/infra/secret's
// Cipher satisfies it.
type KeyRewrapper interface {
	CurrentKeyID() string
	// RewrapDEK re-wraps a Space Secret's DEK sealed under keyID.
	RewrapDEK(wrapped []byte, keyID string) ([]byte, string, error)
	// RewrapValue re-wraps a sealed credential blob, returning nil when it is
	// already under the current KEK, and the key id it was under.
	RewrapValue(blob []byte) ([]byte, string, error)
}

// RewrapResult reports one rewrap pass.
type RewrapResult struct {
	// Rewrapped counts the rows moved to the current KEK, by the key id they
	// were under.
	Rewrapped map[string]int64
	// Skipped counts rows rewritten by a concurrent edit between the read and
	// the write. The edit sealed them under the current KEK already, so they
	// need nothing more.
	Skipped int64
}

// SealedKeyReferences counts the sealed rows that depend on each KEK. valueKeyID
// reads the key id a sealed credential blob names.
func (s *Store) SealedKeyReferences(ctx context.Context, valueKeyID func(blob []byte) (string, error)) (map[string]int64, error) {
	var groups []struct {
		KeyID string
		N     int64
	}
	if err := s.db.WithContext(ctx).Model(&secretRow{}).
		Select("key_id, COUNT(*) AS n").Where("key_id <> ''").
		Group("key_id").Scan(&groups).Error; err != nil {
		return nil, err
	}
	refs := make(map[string]int64, len(groups))
	for _, g := range groups {
		refs[g.KeyID] += g.N
	}
	err := s.eachSealedCredential(ctx, func(row *llmModelRow) error {
		keyID, err := valueKeyID(row.APIKeySealed)
		if err != nil {
			return fmt.Errorf("llm_model %s: %w", row.PublicID, err)
		}
		refs[keyID]++
		return nil
	})
	if err != nil {
		return nil, err
	}
	return refs, nil
}

// RewrapSealedKeys moves every sealed row not under the current KEK to it,
// rewriting only the wrapped DEK and its key id. Each row is its own write,
// conditional on the row still holding what was read, so a concurrent edit is
// never overwritten and nothing locks the features that use these rows. It is
// idempotent and resumable: a row already under the current KEK is not
// selected. It stops at the first row that fails, naming it, and returns what
// it moved before that.
func (s *Store) RewrapSealedKeys(ctx context.Context, r KeyRewrapper) (RewrapResult, error) {
	res := RewrapResult{Rewrapped: map[string]int64{}}
	current := r.CurrentKeyID()

	var cursor uint64
	for {
		var rows []secretRow
		if err := s.db.WithContext(ctx).Select("id", "public_id", "key_id", "wrapped_dek").
			Where("id > ? AND key_id <> '' AND key_id <> ?", cursor, current).
			Order("id").Limit(rewrapBatch).Find(&rows).Error; err != nil {
			return res, err
		}
		for i := range rows {
			row := &rows[i]
			wrapped, keyID, err := r.RewrapDEK(row.WrappedDEK, row.KeyID)
			if err != nil {
				return res, fmt.Errorf("secret %s: %w", row.PublicID, err)
			}
			// UpdateColumns leaves updated_at alone: no value changed.
			upd := s.db.WithContext(ctx).Model(&secretRow{}).
				Where("id = ? AND key_id = ? AND wrapped_dek = ?", row.ID, row.KeyID, row.WrappedDEK).
				UpdateColumns(map[string]any{"wrapped_dek": wrapped, "key_id": keyID})
			if upd.Error != nil {
				return res, fmt.Errorf("secret %s: %w", row.PublicID, upd.Error)
			}
			res.tally(row.KeyID, upd.RowsAffected)
		}
		if len(rows) < rewrapBatch {
			break
		}
		cursor = rows[len(rows)-1].ID
	}

	err := s.eachSealedCredential(ctx, func(row *llmModelRow) error {
		blob, from, err := r.RewrapValue(row.APIKeySealed)
		if err != nil {
			return fmt.Errorf("llm_model %s: %w", row.PublicID, err)
		}
		if blob == nil {
			return nil
		}
		// updated_at stays: the gateway rebuilds a cached client when it moves,
		// and the credential itself is unchanged.
		upd := s.db.WithContext(ctx).Model(&llmModelRow{}).
			Where("id = ? AND api_key_sealed = ?", row.ID, row.APIKeySealed).
			UpdateColumn("api_key_sealed", blob)
		if upd.Error != nil {
			return fmt.Errorf("llm_model %s: %w", row.PublicID, upd.Error)
		}
		res.tally(from, upd.RowsAffected)
		return nil
	})
	return res, err
}

func (r *RewrapResult) tally(fromKeyID string, affected int64) {
	if affected == 0 {
		r.Skipped++
		return
	}
	r.Rewrapped[fromKeyID]++
}

// eachSealedCredential visits every model row holding a sealed credential, in
// bounded batches by row key.
func (s *Store) eachSealedCredential(ctx context.Context, fn func(row *llmModelRow) error) error {
	var cursor uint64
	for {
		var rows []llmModelRow
		if err := s.db.WithContext(ctx).Select("id", "public_id", "api_key_sealed").
			Where("id > ? AND api_key_sealed IS NOT NULL AND LENGTH(api_key_sealed) > 0", cursor).
			Order("id").Limit(rewrapBatch).Find(&rows).Error; err != nil {
			return err
		}
		for i := range rows {
			if err := fn(&rows[i]); err != nil {
				return err
			}
		}
		if len(rows) < rewrapBatch {
			return nil
		}
		cursor = rows[len(rows)-1].ID
	}
}
