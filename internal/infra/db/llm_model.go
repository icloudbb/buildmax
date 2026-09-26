package db

import (
	"context"
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"

	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
)

// llmModelRow is the managed model catalog.
//
// APIKeySealed is the one column that must never be selected by a general read.
// It holds the provider credential encrypted at rest (envelope encryption under
// the deployment KEK); the store exposes it through LLMModelCredential alone,
// which decrypts it, so a query that forgets to exclude it cannot exist and a
// plaintext key is never written. The KEK rotation walk in sealed_key.go reads
// the sealed bytes too, but only re-wraps their DEK and never decrypts the key.
type llmModelRow struct {
	ID            uint64 `gorm:"primaryKey;autoIncrement"`
	PublicID      string `gorm:"column:public_id;type:char(20) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_llm_model_public_id;not null"`
	Name          string `gorm:"type:varchar(128);uniqueIndex;not null"`
	ProviderType  string `gorm:"type:varchar(32);not null"`
	APIURL        string `gorm:"type:varchar(512);not null"`
	APIKeySealed  []byte `gorm:"column:api_key_sealed;type:blob"`
	Model         string `gorm:"type:varchar(128);not null"`
	ContextWindow int    `gorm:"not null;default:0"`
	CallTimeout   int    `gorm:"not null;default:0"`
	MaxTokens     int    `gorm:"not null;default:0"`
	Reasoning     string `gorm:"type:varchar(16);not null;default:''"`
	// CacheMode and CacheTTL are the prompt-cache policy. Empty means unset,
	// which takes the default.
	CacheMode string `gorm:"size:16;not null;default:''"`
	CacheTTL  string `gorm:"size:16;not null;default:''"`
	// Rates are nano-currency-units per million tokens, held as integers
	// because a float would round a published price before anything read it.
	// An empty Currency means unpriced.
	// The column names are pinned because the naming strategy would render
	// MTok as "m_tok", which is nobody's idea of the name and would disagree
	// with every other place these rates are written.
	Currency          string `gorm:"size:8;not null;default:''"`
	InputPerMTok      int64  `gorm:"column:input_per_mtok;not null;default:0"`
	CacheReadPerMTok  int64  `gorm:"column:cache_read_per_mtok;not null;default:0"`
	CacheWritePerMTok int64  `gorm:"column:cache_write_per_mtok;not null;default:0"`
	OutputPerMTok     int64  `gorm:"column:output_per_mtok;not null;default:0"`
	Vision            bool   `gorm:"not null;default:false"`
	// Capabilities is a comma-separated list. The set is small, closed, and only
	// ever read whole, so a join table would buy nothing.
	Capabilities string    `gorm:"type:varchar(255)"`
	Enabled      bool      `gorm:"not null;default:true"`
	CreatedAt    time.Time `gorm:"autoCreateTime;index"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime"`
}

func (llmModelRow) TableName() string { return "llm_model" }

func toLLMModel(row *llmModelRow) *coregw.Model {
	if row == nil {
		return nil
	}
	return &coregw.Model{
		ID:                row.PublicID,
		Name:              row.Name,
		ProviderType:      row.ProviderType,
		APIURL:            row.APIURL,
		Model:             row.Model,
		ContextWindow:     row.ContextWindow,
		CallTimeout:       row.CallTimeout,
		MaxTokens:         row.MaxTokens,
		Reasoning:         row.Reasoning,
		CacheMode:         row.CacheMode,
		CacheTTL:          row.CacheTTL,
		Currency:          row.Currency,
		InputPerMTok:      row.InputPerMTok,
		CacheReadPerMTok:  row.CacheReadPerMTok,
		CacheWritePerMTok: row.CacheWritePerMTok,
		OutputPerMTok:     row.OutputPerMTok,
		Vision:            row.Vision,
		Capabilities:      splitCapabilities(row.Capabilities),
		Enabled:           row.Enabled,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}

func splitCapabilities(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func joinCapabilities(in []string) string {
	out := make([]string, 0, len(in))
	for _, c := range in {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return strings.Join(out, ",")
}

// llmModelColumns is every column except the credential. Reads name it
// explicitly so adding a column later cannot silently start returning the key.
var llmModelColumns = []string{
	"id", "public_id", "name", "provider_type", "api_url", "model",
	"context_window", "call_timeout", "max_tokens", "reasoning",
	"cache_mode", "cache_ttl",
	"currency", "input_per_mtok", "cache_read_per_mtok", "cache_write_per_mtok",
	"output_per_mtok",
	"vision", "capabilities", "enabled",
	"created_at", "updated_at",
}

// llmCredentialAAD domain-separates a model-credential blob from every other
// thing sealed under the same deployment KEK (Space Secrets bind their own AAD),
// so a blob authenticated as a credential cannot be opened as anything else. It
// binds no model id: like a Space Secret's AAD, the public id is minted after
// the value is sealed, and per-deployment isolation already comes from the KEK.
var llmCredentialAAD = []byte("bmax-llm-credential\x00")

// sealCredential encrypts a provider credential for storage. An empty
// credential (a provider that needs none) seals to nothing. A non-empty
// credential with no cipher configured is refused rather than stored plaintext.
func (s *Store) sealCredential(plaintext string) ([]byte, error) {
	if plaintext == "" {
		return nil, nil
	}
	if s.credentialCipher == nil {
		return nil, coregw.ErrCredentialEncryptionUnavailable
	}
	return s.credentialCipher.SealValue(plaintext, llmCredentialAAD)
}

// CreateLLMModel stores a new model. The name is unique so an operator cannot
// end up with two catalog entries that look identical in a listing.
func (s *Store) CreateLLMModel(ctx context.Context, in coregw.CreateModelInput) (*coregw.Model, error) {
	sealed, err := s.sealCredential(in.APIKey)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	row := &llmModelRow{
		Name:              in.Name,
		ProviderType:      in.ProviderType,
		APIURL:            in.APIURL,
		APIKeySealed:      sealed,
		Model:             in.Model,
		ContextWindow:     in.ContextWindow,
		CallTimeout:       in.CallTimeout,
		MaxTokens:         in.MaxTokens,
		Reasoning:         in.Reasoning,
		CacheMode:         in.CacheMode,
		CacheTTL:          in.CacheTTL,
		Currency:          in.Currency,
		InputPerMTok:      in.InputPerMTok,
		CacheReadPerMTok:  in.CacheReadPerMTok,
		CacheWritePerMTok: in.CacheWritePerMTok,
		OutputPerMTok:     in.OutputPerMTok,
		Vision:            in.Vision,
		Capabilities:      joinCapabilities(in.Capabilities),
		Enabled:           true,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	if err := createWithPublicID(ctx, s.db, "uq_llm_model_public_id",
		func(id string) { row.PublicID = id }, row); err != nil {
		if isDuplicateKey(err) {
			return nil, coregw.ErrModelNameTaken
		}
		return nil, err
	}
	return toLLMModel(row), nil
}

// GetLLMModel returns one model by ID, or (nil, nil) when not found.
func (s *Store) GetLLMModel(ctx context.Context, llmModelID string) (*coregw.Model, error) {
	if llmModelID == "" {
		return nil, nil
	}
	var row llmModelRow
	err := s.db.WithContext(ctx).Select(llmModelColumns).
		Where("public_id = ?", canonicalPublicID(llmModelID)).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toLLMModel(&row), nil
}

// GetLLMModelByName returns one model by name, or (nil, nil) when not found.
//
// The name column is uniquely indexed, so this is a single-row lookup rather
// than a scan — which is what lets the call path address a model by the name an
// operator gave it.
func (s *Store) GetLLMModelByName(ctx context.Context, name string) (*coregw.Model, error) {
	if name == "" {
		return nil, nil
	}
	var row llmModelRow
	err := s.db.WithContext(ctx).Select(llmModelColumns).
		Where("name = ?", name).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return toLLMModel(&row), nil
}

// ListLLMModels returns every model, enabled or not, oldest first.
func (s *Store) ListLLMModels(ctx context.Context) ([]coregw.Model, error) {
	var rows []llmModelRow
	if err := s.db.WithContext(ctx).Select(llmModelColumns).
		Order("created_at ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]coregw.Model, 0, len(rows))
	for i := range rows {
		out = append(out, *toLLMModel(&rows[i]))
	}
	return out, nil
}

// SetLLMModelEnabled retires or restores a model.
func (s *Store) SetLLMModelEnabled(ctx context.Context, llmModelID string, enabled bool) error {
	res := s.db.WithContext(ctx).Model(&llmModelRow{}).
		Where("public_id = ?", canonicalPublicID(llmModelID)).
		Updates(map[string]any{"enabled": enabled, "updated_at": time.Now().UTC()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("model not found")
	}
	return nil
}

// SetLLMModelCredential replaces a model's upstream key. updated_at moves
// with it, which is what makes the gateway rebuild a client it cached with the
// old key.
func (s *Store) SetLLMModelCredential(ctx context.Context, llmModelID, apiKey string) error {
	sealed, err := s.sealCredential(apiKey)
	if err != nil {
		return err
	}
	res := s.db.WithContext(ctx).Model(&llmModelRow{}).
		Where("public_id = ?", canonicalPublicID(llmModelID)).
		Updates(map[string]any{"api_key_sealed": sealed, "updated_at": time.Now().UTC()})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("model not found")
	}
	return nil
}

// LLMModelCredential returns the upstream key for a model.
//
// This is the only read that decrypts the credential column, which is what
// makes "the key reaches the provider client and nothing else" checkable rather
// than a matter of care.
func (s *Store) LLMModelCredential(ctx context.Context, llmModelID string) (string, error) {
	if llmModelID == "" {
		return "", errors.New("model id is required")
	}
	var row llmModelRow
	err := s.db.WithContext(ctx).Select("api_key_sealed").
		Where("public_id = ?", canonicalPublicID(llmModelID)).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", errors.New("model not found")
	}
	if err != nil {
		return "", err
	}
	if len(row.APIKeySealed) == 0 {
		// A provider that needs no credential, or a row from before encryption
		// was configured. Either way there is no key to hand back.
		return "", nil
	}
	if s.credentialCipher == nil {
		return "", coregw.ErrCredentialEncryptionUnavailable
	}
	return s.credentialCipher.OpenValue(row.APIKeySealed, llmCredentialAAD)
}
