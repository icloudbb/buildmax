package db

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/config"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
	"github.com/icloudbb/buildmax/internal/util"
)

func ptrString(s string) *string { return &s }

// ledgerUser is the person a ledger fixture attributes to. A call references a
// user row, so the test creates one rather than naming a handle.
func ledgerUser(t *testing.T, s *Store) string {
	t.Helper()
	return newTestUser(t, s, "ledger")
}

func sampleLLMCall() *coregw.Call {
	return &coregw.Call{
		UserID:        nil,
		Surface:       coregw.CallSurfaceCLI,
		SessionID:     ptrString("session-1"),
		Model:         "fast",
		TargetID:      "mt_fast",
		ProviderType:  "openai_compatible",
		UpstreamModel: "vendor/fast-1",
		Streaming:     true,
		AcceptedAt:    time.Unix(1_700_000_000, 0).UTC(),
	}
}

// TestLLMCallRowRoundTrip runs without a database: it is the mapping that
// silently drops a column when the model grows.
func TestLLMCallRowRoundTrip(t *testing.T) {
	// The references are left out: they are row keys now, and resolving them
	// needs a database. The store integration tests cover that direction.
	call := sampleLLMCall()
	call.ID = ""
	call.UserID = nil
	call.ClientCallID = ptrString("client-key-1")
	upstreamStarted := time.Unix(1_700_000_001, 0).UTC()
	firstDelta := time.Unix(1_700_000_002, 0).UTC()
	completed := time.Unix(1_700_000_003, 0).UTC()
	call.UpstreamStartedAt = &upstreamStarted
	call.FirstDeltaAt = &firstDelta
	call.CompletedAt = &completed
	call.Status = coregw.CallStatusSucceeded
	call.ErrorClass = ptrString("upstream_unavailable")
	call.Attempts = 2
	prompt, completion, total := 100, 20, 120
	call.PromptTokens = &prompt
	call.CompletionTokens = &completion
	call.TotalTokens = &total
	call.UsageSource = coregw.UsageSourceReported

	got := toLLMCall(&llmCallReadRow{Row: *llmCallValues(call)})
	if got == nil {
		t.Fatal("round trip produced nil")
	}
	if *got != *call {
		t.Errorf("round trip changed the record:\n got %+v\nwant %+v", *got, *call)
	}

	if llmCallValues(nil) != nil {
		t.Error("llmCallValues(nil) must be nil")
	}
	if toLLMCall(nil) != nil {
		t.Error("toLLMCall(nil) must be nil")
	}
}

// TestLLMCallCarriesNoContent is a structural guard for the redaction rule in
// docs/design/llm-gateway.md: the ledger records metadata, never prompts, tool
// payloads, or generated content.
func TestLLMCallCarriesNoContent(t *testing.T) {
	forbidden := []string{"prompt", "message", "content", "tool", "input", "output", "text", "body"}
	// Counts and prices, not payloads. The rate fields name the token class
	// they price — input, output — which is what trips the word list; they hold
	// nano-currency-units per million tokens and no call content.
	allowed := map[string]bool{
		"PromptTokens":          true,
		"RateInputPerMTok":      true,
		"RateOutputPerMTok":     true,
		"RateCacheReadPerMTok":  true,
		"RateCacheWritePerMTok": true,
	}

	rowType := reflect.TypeOf(llmCallRow{})
	for i := range rowType.NumField() {
		field := rowType.Field(i)
		if allowed[field.Name] {
			continue
		}
		name := strings.ToLower(field.Name)
		for _, word := range forbidden {
			if strings.Contains(name, word) {
				t.Errorf("llm_call field %q looks like it carries call content", field.Name)
			}
		}
	}
}

func TestOpenAndCompleteLLMCall(t *testing.T) {
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	call := sampleLLMCall()
	call.UserID = ptrString(ledgerUser(t, s))
	call.ClientCallID = ptrString(testPublicID(t))
	opened, err := s.OpenLLMCall(ctx, call)
	if err != nil {
		t.Fatalf("OpenLLMCall: %v", err)
	}
	defer func() {
		_ = s.db.WithContext(ctx).Delete(&llmCallRow{}, "public_id = ?", canonicalPublicID(opened.ID))
	}()

	if _, ok := util.CanonicalPublicID(opened.ID); !ok {
		t.Errorf("LLMCallID = %q, want a canonical public ID", opened.ID)
	}
	if opened.Status != coregw.CallStatusAccepted {
		t.Errorf("Status = %q, want %q", opened.Status, coregw.CallStatusAccepted)
	}
	if opened.UsageSource != coregw.UsageSourceUnavailable {
		t.Errorf("UsageSource = %q, want %q", opened.UsageSource, coregw.UsageSourceUnavailable)
	}
	if opened.PromptTokens != nil {
		t.Error("an accepted call must not carry token counts yet")
	}

	completedAt := opened.AcceptedAt.Add(3 * time.Second)
	upstreamStarted := opened.AcceptedAt.Add(1 * time.Second)
	err = s.CompleteLLMCall(ctx, opened.ID, coregw.CallOutcome{
		Status:            coregw.CallStatusSucceeded,
		Attempts:          1,
		UpstreamStartedAt: &upstreamStarted,
		CompletedAt:       completedAt,
		Usage: &coregw.CallUsage{
			PromptTokens:     100,
			CompletionTokens: 20,
			TotalTokens:      120,
			Source:           coregw.UsageSourceReported,
		},
	})
	if err != nil {
		t.Fatalf("CompleteLLMCall: %v", err)
	}

	got, err := s.GetLLMCall(ctx, opened.ID)
	if err != nil {
		t.Fatalf("GetLLMCall: %v", err)
	}
	if got == nil {
		t.Fatal("GetLLMCall returned nothing for a stored call")
	}
	if got.Status != coregw.CallStatusSucceeded {
		t.Errorf("Status = %q, want %q", got.Status, coregw.CallStatusSucceeded)
	}
	if got.TotalTokens == nil || *got.TotalTokens != 120 {
		t.Errorf("TotalTokens = %v, want 120", got.TotalTokens)
	}
	if got.CompletedAt == nil || !got.CompletedAt.Equal(completedAt) {
		t.Errorf("CompletedAt = %v, want %v", got.CompletedAt, completedAt)
	}

	byClient, err := s.GetLLMCallByClientID(ctx, *call.UserID, *call.ClientCallID)
	if err != nil {
		t.Fatalf("GetLLMCallByClientID: %v", err)
	}
	if byClient == nil || byClient.ID != opened.ID {
		t.Errorf("GetLLMCallByClientID returned %v, want %q", byClient, opened.ID)
	}
	// Another person's identical key must not resolve this call.
	other, err := s.GetLLMCallByClientID(ctx, ledgerUser(t, s), *call.ClientCallID)
	if err != nil {
		t.Fatalf("GetLLMCallByClientID for another user: %v", err)
	}
	if other != nil {
		t.Error("a client call ID resolved across users")
	}
}

func TestCompleteLLMCallKeepsUnavailableUsage(t *testing.T) {
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	call := sampleLLMCall()
	call.UserID = ptrString(ledgerUser(t, s))
	opened, err := s.OpenLLMCall(ctx, call)
	if err != nil {
		t.Fatalf("OpenLLMCall: %v", err)
	}
	defer func() {
		_ = s.db.WithContext(ctx).Delete(&llmCallRow{}, "public_id = ?", canonicalPublicID(opened.ID))
	}()

	errorClass := "upstream_unavailable"
	err = s.CompleteLLMCall(ctx, opened.ID, coregw.CallOutcome{
		Status:      coregw.CallStatusFailed,
		ErrorClass:  &errorClass,
		Attempts:    3,
		CompletedAt: opened.AcceptedAt.Add(5 * time.Second),
	})
	if err != nil {
		t.Fatalf("CompleteLLMCall: %v", err)
	}

	got, err := s.GetLLMCall(ctx, opened.ID)
	if err != nil {
		t.Fatalf("GetLLMCall: %v", err)
	}
	// A failed call reports no tokens rather than zero tokens: they are
	// different facts and only one of them may be billed.
	if got.TotalTokens != nil {
		t.Errorf("TotalTokens = %v, want nil", got.TotalTokens)
	}
	if got.UsageSource != coregw.UsageSourceUnavailable {
		t.Errorf("UsageSource = %q, want %q", got.UsageSource, coregw.UsageSourceUnavailable)
	}
	if got.Attempts != 3 {
		t.Errorf("Attempts = %d, want 3", got.Attempts)
	}
}

// TestOpenLLMCallRejectsADuplicateClientID proves the unique index, not a
// look-before-insert, is what stops one client call ID running twice.
func TestOpenLLMCallRejectsADuplicateClientID(t *testing.T) {
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	key := testPublicID(t)
	user := ledgerUser(t, s)
	first := sampleLLMCall()
	first.UserID = &user
	first.ClientCallID = &key
	opened, err := s.OpenLLMCall(ctx, first)
	if err != nil {
		t.Fatalf("OpenLLMCall: %v", err)
	}
	defer func() {
		_ = s.db.WithContext(ctx).Delete(&llmCallRow{}, "client_call_id = ?", key)
	}()

	second := sampleLLMCall()
	second.UserID = &user
	second.ClientCallID = &key
	if _, err := s.OpenLLMCall(ctx, second); !errors.Is(err, coregw.ErrDuplicateCall) {
		t.Fatalf("want ErrDuplicateLLMCall, got %v", err)
	}

	// Another person may use the same key: the constraint is user-scoped.
	otherUser := ledgerUser(t, s)
	other := sampleLLMCall()
	other.UserID = &otherUser
	other.ClientCallID = &key
	otherOpened, err := s.OpenLLMCall(ctx, other)
	if err != nil {
		t.Fatalf("another user was blocked by the key: %v", err)
	}
	if otherOpened.ID == opened.ID {
		t.Error("two calls share one id")
	}

	// A call with no client key is never a duplicate.
	for range 2 {
		anonymous := sampleLLMCall()
		anonymous.UserID = &user
		opened, err := s.OpenLLMCall(ctx, anonymous)
		if err != nil {
			t.Fatalf("a call with no client key was rejected: %v", err)
		}
		defer func() {
			_ = s.db.WithContext(ctx).Delete(&llmCallRow{}, "public_id = ?", canonicalPublicID(opened.ID))
		}()
	}
}

// TestSearchLLMCalls covers the deployment-wide read: it spans users, orders
// newest first, and each filter narrows independently.
//
// Every seeded row shares one model that no other test uses, so the assertions
// stay deterministic against a store other tests also write to.
func TestSearchLLMCalls(t *testing.T) {
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	model := "search-" + testPublicID(t)
	t.Cleanup(func() { _ = s.db.WithContext(ctx).Delete(&llmCallRow{}, "model = ?", model) })

	userA := newTestUser(t, s, "search-a")
	userB := newTestUser(t, s, "search-b")

	seed := func(user, status, surface string, acceptedUnix int64) string {
		t.Helper()
		call := sampleLLMCall()
		call.UserID = ptrString(user)
		call.ClientCallID = nil
		call.Model = model
		call.Status = status
		call.Surface = surface
		call.AcceptedAt = time.Unix(acceptedUnix, 0).UTC()
		opened, err := s.OpenLLMCall(ctx, call)
		if err != nil {
			t.Fatalf("OpenLLMCall: %v", err)
		}
		return opened.ID
	}

	c1 := seed(userA, coregw.CallStatusSucceeded, coregw.CallSurfaceWorker, 1_000)
	c2 := seed(userA, coregw.CallStatusFailed, coregw.CallSurfaceCLI, 2_000)
	c3 := seed(userB, coregw.CallStatusSucceeded, coregw.CallSurfaceWorker, 3_000)

	ids := func(calls []coregw.Call) []string {
		out := make([]string, len(calls))
		for i, c := range calls {
			out[i] = c.ID
		}
		return out
	}
	assert := func(name string, filter coregw.CallFilter, limit, offset, wantTotal int, wantOrder []string) {
		t.Helper()
		filter.Model = model
		got, total, err := s.SearchLLMCalls(ctx, filter, limit, offset)
		if err != nil {
			t.Fatalf("%s: SearchLLMCalls: %v", name, err)
		}
		if total != wantTotal {
			t.Errorf("%s: total = %d, want %d", name, total, wantTotal)
		}
		if !reflect.DeepEqual(ids(got), wantOrder) {
			t.Errorf("%s: order = %v, want %v", name, ids(got), wantOrder)
		}
	}

	// Newest first, and spanning both users — the read the per-run route cannot do.
	assert("all", coregw.CallFilter{}, 50, 0, 3, []string{c3, c2, c1})
	assert("by user", coregw.CallFilter{UserID: userA}, 50, 0, 2, []string{c2, c1})
	assert("by status", coregw.CallFilter{Status: coregw.CallStatusSucceeded}, 50, 0, 2, []string{c3, c1})
	assert("by surface", coregw.CallFilter{Surface: coregw.CallSurfaceCLI}, 50, 0, 1, []string{c2})
	assert("by window", coregw.CallFilter{Since: time.Unix(1_500, 0).UTC(), Until: time.Unix(2_500, 0).UTC()}, 50, 0, 1, []string{c2})

	// The page window rides the count: total is every match, not the page size.
	assert("first page", coregw.CallFilter{}, 2, 0, 3, []string{c3, c2})
	assert("second page", coregw.CallFilter{}, 2, 2, 3, []string{c1})

	// An unparseable user id matches nothing rather than erroring.
	assert("bad user id", coregw.CallFilter{UserID: "not a public id"}, 50, 0, 0, []string{})
}

func TestGetLLMCallMissing(t *testing.T) {
	dsn := os.Getenv(config.EnvKeyBuildmaxTestDSN)
	if dsn == "" {
		t.Skip(config.EnvKeyBuildmaxTestDSN + " not set, skipping store integration test")
	}
	ctx := context.Background()
	s, err := New(ctx, dsn)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	got, err := s.GetLLMCall(ctx, "lc_does_not_exist")
	if err != nil {
		t.Fatalf("GetLLMCall: %v", err)
	}
	if got != nil {
		t.Errorf("GetLLMCall = %v, want nil for a missing call", got)
	}

	// An empty key is a lookup with nothing to find, not an error.
	if got, err := s.GetLLMCallByClientID(ctx, "tm_ledger", ""); err != nil || got != nil {
		t.Errorf("GetLLMCallByClientID with an empty key = %v, %v", got, err)
	}
}
