package db

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
	"github.com/icloudbb/buildmax/internal/util"
	"gorm.io/gorm/schema"
)

const catalogSecret = "sk-CATALOG-SECRET-VALUE"

func sampleModelInput(name string) coregw.CreateModelInput {
	return coregw.CreateModelInput{
		Name:          name,
		ProviderType:  "openai_compatible",
		APIURL:        "https://upstream.example.com/v1",
		APIKey:        catalogSecret,
		Model:         "vendor/fast-1",
		ContextWindow: 128000,
		CallTimeout:   300,
		MaxTokens:     4096,
		Capabilities:  []string{"text_chat", "tool_calls"},
	}
}

func TestCapabilityListRoundTrip(t *testing.T) {
	tests := []struct {
		in   []string
		want []string
	}{
		{in: nil, want: nil},
		{in: []string{"text_chat"}, want: []string{"text_chat"}},
		{in: []string{"text_chat", "tool_calls"}, want: []string{"text_chat", "tool_calls"}},
		{in: []string{"  text_chat  ", "", "tool_calls"}, want: []string{"text_chat", "tool_calls"}},
	}
	for _, tc := range tests {
		got := splitCapabilities(joinCapabilities(tc.in))
		if strings.Join(got, ",") != strings.Join(tc.want, ",") {
			t.Errorf("round trip of %v = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// TestLLMModelReadsExcludeTheCredential is the structural guard for the rule
// that a stored key reaches the provider client and nothing else: every
// general-purpose read names its columns, and the credential is not among them.
func TestLLMModelReadsExcludeTheCredential(t *testing.T) {
	for _, column := range llmModelColumns {
		if column == "api_key" || column == "api_key_sealed" {
			t.Fatalf("%s is in the general read column list", column)
		}
	}
	// The entity itself has no field to put it in either.
	if strings.Contains(fmt.Sprintf("%+v", coregw.Model{}), "APIKey") {
		t.Error("coregw.Model has an APIKey field")
	}
}

// fakeCredentialCipher is a reversible stand-in for the deployment cipher so the
// store tests exercise encrypt-before-store and decrypt-on-read without a KEK.
// The real envelope encryption is tested in internal/infra/secret; here the only
// property that matters is that the transform is reversible and hides the
// plaintext, so "the column is not the key" is a real assertion. XOR with a
// non-zero byte changes every byte, so no plaintext substring survives.
type fakeCredentialCipher struct{}

func (fakeCredentialCipher) SealValue(plaintext string, _ []byte) ([]byte, error) {
	out := make([]byte, len(plaintext))
	for i := range plaintext {
		out[i] = plaintext[i] ^ 0x5a
	}
	return out, nil
}

func (fakeCredentialCipher) OpenValue(blob []byte, _ []byte) (string, error) {
	out := make([]byte, len(blob))
	for i := range blob {
		out[i] = blob[i] ^ 0x5a
	}
	return string(out), nil
}

func newCatalogStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn, Options{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	s.SetCredentialCipher(fakeCredentialCipher{})
	return s, ctx
}

func TestCreateAndReadLLMModel(t *testing.T) {
	s, ctx := newCatalogStore(t)

	name := "Catalog " + testPublicID(t)
	created, err := s.CreateLLMModel(ctx, sampleModelInput(name))
	if err != nil {
		t.Fatalf("CreateLLMModel: %v", err)
	}
	defer func() {
		_ = s.db.WithContext(ctx).Delete(&llmModelRow{}, "public_id = ?", canonicalPublicID(created.ID))
	}()

	if _, ok := util.CanonicalPublicID(created.ID); !ok {
		t.Errorf("LLMModelID = %q, want a canonical public ID", created.ID)
	}
	if !created.Enabled {
		t.Error("a new model is not enabled")
	}
	if strings.Contains(fmt.Sprintf("%+v", *created), catalogSecret) {
		t.Error("CreateLLMModel returned the credential")
	}

	got, err := s.GetLLMModel(ctx, created.ID)
	if err != nil || got == nil {
		t.Fatalf("GetLLMModel: %v, %v", got, err)
	}
	if got.Name != name || got.Model != "vendor/fast-1" || got.ContextWindow != 128000 {
		t.Errorf("stored model = %+v", got)
	}
	if got.MaxTokens != 4096 {
		t.Errorf("MaxTokens = %d, want 4096", got.MaxTokens)
	}
	if strings.Join(got.Capabilities, ",") != "text_chat,tool_calls" {
		t.Errorf("Capabilities = %v", got.Capabilities)
	}
	if strings.Contains(fmt.Sprintf("%+v", *got), catalogSecret) {
		t.Error("GetLLMModel returned the credential")
	}

	listed, err := s.ListLLMModels(ctx)
	if err != nil {
		t.Fatalf("ListLLMModels: %v", err)
	}
	if strings.Contains(fmt.Sprintf("%+v", listed), catalogSecret) {
		t.Error("ListLLMModels returned the credential")
	}

	// The one read that is allowed to see it.
	key, err := s.LLMModelCredential(ctx, created.ID)
	if err != nil {
		t.Fatalf("LLMModelCredential: %v", err)
	}
	if key != catalogSecret {
		t.Errorf("LLMModelCredential = %q", key)
	}
}

func TestLLMModelNameIsUnique(t *testing.T) {
	s, ctx := newCatalogStore(t)

	name := "Catalog " + testPublicID(t)
	created, err := s.CreateLLMModel(ctx, sampleModelInput(name))
	if err != nil {
		t.Fatalf("CreateLLMModel: %v", err)
	}
	defer func() {
		_ = s.db.WithContext(ctx).Delete(&llmModelRow{}, "public_id = ?", canonicalPublicID(created.ID))
	}()

	if _, err := s.CreateLLMModel(ctx, sampleModelInput(name)); !errors.Is(err, coregw.ErrModelNameTaken) {
		t.Fatalf("want ErrLLMModelNameTaken, got %v", err)
	}
}

func TestSetLLMModelEnabled(t *testing.T) {
	s, ctx := newCatalogStore(t)

	created, err := s.CreateLLMModel(ctx, sampleModelInput("Catalog "+testPublicID(t)))
	if err != nil {
		t.Fatalf("CreateLLMModel: %v", err)
	}
	defer func() {
		_ = s.db.WithContext(ctx).Delete(&llmModelRow{}, "public_id = ?", canonicalPublicID(created.ID))
	}()

	if err := s.SetLLMModelEnabled(ctx, created.ID, false); err != nil {
		t.Fatalf("SetLLMModelEnabled: %v", err)
	}
	got, err := s.GetLLMModel(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetLLMModel: %v", err)
	}
	if got.Enabled {
		t.Error("the model is still enabled after being retired")
	}

	if err := s.SetLLMModelEnabled(ctx, "lm_does_not_exist", false); err == nil {
		t.Error("retiring a model that does not exist reported success")
	}
}

func TestGetLLMModelMissing(t *testing.T) {
	s, ctx := newCatalogStore(t)

	got, err := s.GetLLMModel(ctx, "lm_does_not_exist")
	if err != nil {
		t.Fatalf("GetLLMModel: %v", err)
	}
	if got != nil {
		t.Errorf("GetLLMModel = %v, want nil", got)
	}
	if got, err := s.GetLLMModel(ctx, ""); err != nil || got != nil {
		t.Errorf("GetLLMModel(\"\") = %v, %v", got, err)
	}
	if _, err := s.LLMModelCredential(ctx, "lm_does_not_exist"); err == nil {
		t.Error("a credential was returned for a model that does not exist")
	}
}

// TestLLMModelCredentialIsEncryptedAtRest is the whole point of the change: the
// stored column is ciphertext, not the key, and only LLMModelCredential turns it
// back. Reading the column directly is how "at rest" is checked rather than
// trusted.
func TestLLMModelCredentialIsEncryptedAtRest(t *testing.T) {
	s, ctx := newCatalogStore(t)

	created, err := s.CreateLLMModel(ctx, sampleModelInput("Catalog "+testPublicID(t)))
	if err != nil {
		t.Fatalf("CreateLLMModel: %v", err)
	}
	defer func() {
		_ = s.db.WithContext(ctx).Delete(&llmModelRow{}, "public_id = ?", canonicalPublicID(created.ID))
	}()

	var raw llmModelRow
	if err := s.db.WithContext(ctx).Select("api_key_sealed").
		Where("public_id = ?", canonicalPublicID(created.ID)).First(&raw).Error; err != nil {
		t.Fatalf("read sealed column: %v", err)
	}
	if len(raw.APIKeySealed) == 0 {
		t.Fatal("no credential was stored")
	}
	if strings.Contains(string(raw.APIKeySealed), catalogSecret) {
		t.Error("the stored column contains the plaintext key")
	}

	key, err := s.LLMModelCredential(ctx, created.ID)
	if err != nil || key != catalogSecret {
		t.Fatalf("LLMModelCredential round trip = %q, %v; want the original key", key, err)
	}
}

// TestLLMModelCredentialWithoutCipher is the fail-closed rule: with no
// deployment encryption key, a credentialed model is refused rather than stored
// in the clear, while a credential-free model is unaffected.
func TestLLMModelCredentialWithoutCipher(t *testing.T) {
	s, ctx := newCatalogStore(t)
	s.SetCredentialCipher(nil) // a deployment with no KEK configured

	if _, err := s.CreateLLMModel(ctx, sampleModelInput("Catalog "+testPublicID(t))); !errors.Is(err, coregw.ErrCredentialEncryptionUnavailable) {
		t.Fatalf("credentialed create without a cipher = %v, want ErrCredentialEncryptionUnavailable", err)
	}

	free := sampleModelInput("Catalog " + testPublicID(t))
	free.APIKey = ""
	created, err := s.CreateLLMModel(ctx, free)
	if err != nil {
		t.Fatalf("credential-free create without a cipher: %v", err)
	}
	defer func() {
		_ = s.db.WithContext(ctx).Delete(&llmModelRow{}, "public_id = ?", canonicalPublicID(created.ID))
	}()
	if key, err := s.LLMModelCredential(ctx, created.ID); err != nil || key != "" {
		t.Fatalf("credential-free model's credential = %q, %v; want empty", key, err)
	}
}

// TestLLMModelColumnsCoversTheRow is the guard the comment on llmModelColumns
// asked for and could not enforce.
//
// Reads name their columns explicitly so a column added later cannot silently
// start returning the credential. The cost is the mirror failure, and it
// happened: the prompt-cache policy and pricing columns were added to the row
// and not to the list, so every catalog read returned them as zero — a managed
// deployment would have found its models unpriced and its cache policy unset,
// with nothing failing to say so. The store tests skip without a DSN, so
// nothing caught it.
//
// api_key is the one column deliberately absent. Everything else must be
// listed, and this fails when it is not.
func TestLLMModelColumnsCoversTheRow(t *testing.T) {
	listed := make(map[string]bool, len(llmModelColumns))
	for _, c := range llmModelColumns {
		listed[c] = true
	}
	// The credential is excluded on purpose; naming it here is the whole point
	// of the list.
	skip := map[string]bool{"api_key_sealed": true}

	rowType := reflect.TypeOf(llmModelRow{})
	for i := range rowType.NumField() {
		column := gormColumnName(rowType.Field(i))
		if column == "" || skip[column] {
			continue
		}
		if !listed[column] {
			t.Errorf("llmModelRow.%s maps to column %q, which llmModelColumns does not select; "+
				"reads would return it as zero", rowType.Field(i).Name, column)
		}
	}
	for _, c := range llmModelColumns {
		if !rowHasColumn(rowType, c) {
			t.Errorf("llmModelColumns selects %q, which llmModelRow has no field for", c)
		}
	}
}

// gormColumnName is the column a struct field maps to.
//
// GORM's own naming strategy answers it rather than a hand-rolled snake_case:
// approximating it is how a guard ends up disagreeing with the schema it
// guards, and initialisms like APIURL are exactly where the two diverge.
func gormColumnName(f reflect.StructField) string {
	tag := f.Tag.Get("gorm")
	for _, part := range strings.Split(tag, ";") {
		if name, ok := strings.CutPrefix(strings.TrimSpace(part), "column:"); ok {
			return name
		}
	}
	if tag == "-" {
		return ""
	}
	return (schema.NamingStrategy{}).ColumnName("", f.Name)
}

func rowHasColumn(rowType reflect.Type, column string) bool {
	for i := range rowType.NumField() {
		if gormColumnName(rowType.Field(i)) == column {
			return true
		}
	}
	return false
}
