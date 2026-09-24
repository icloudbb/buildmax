package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
	"github.com/icloudbb/buildmax/internal/mock"
)

// fakeCallLedger records the filter a request resolved to and returns whatever
// the test staged. The mapping and query parsing are what these tests pin; the
// store's own filtering is the db package's to prove.
type fakeCallLedger struct {
	calls      []coregw.Call
	lastFilter coregw.CallFilter
	lastLimit  int
	lastOffset int
}

func (f *fakeCallLedger) OpenLLMCall(_ context.Context, call *coregw.Call) (*coregw.Call, error) {
	return call, nil
}
func (f *fakeCallLedger) CompleteLLMCall(context.Context, string, coregw.CallOutcome) error {
	return nil
}
func (f *fakeCallLedger) GetLLMCall(context.Context, string) (*coregw.Call, error) { return nil, nil }
func (f *fakeCallLedger) GetLLMCallByClientID(context.Context, string, string) (*coregw.Call, error) {
	return nil, nil
}
func (f *fakeCallLedger) ListLLMCallsByTaskRun(context.Context, string) ([]coregw.Call, error) {
	return nil, nil
}
func (f *fakeCallLedger) SearchLLMCalls(_ context.Context, filter coregw.CallFilter, limit, offset int) ([]coregw.Call, int, error) {
	f.lastFilter = filter
	f.lastLimit = limit
	f.lastOffset = offset
	return f.calls, len(f.calls), nil
}

func llmCallsMux(t *testing.T, ledger coregw.CallStore) *http.ServeMux {
	t.Helper()
	users := &mock.MockUserStore{}
	seedUser(t, users, adminUser, "admin@example.com")
	grants := &mock.MockSystemGrantStore{}
	grants.GrantForTest(adminUser, coreidentity.SystemRoleAdmin)

	h := New(Config{
		JWTSecret: testSecret,
		Grants:    grants,
		Users:     users,
		Spaces:    &mock.MockSpaceStore{},
		Audits:    &mock.MockAuditStore{},
		LLMCalls:  ledger,
	})
	mux := http.NewServeMux()
	h.Register(mux)
	return mux
}

func getLLMCalls(t *testing.T, mux *http.ServeMux, query string) AdminLLMCallsResponse {
	t.Helper()
	rec := adminRequestAs(t, mux, adminCase{"GET", "/api/admin/llm/calls" + query}, adminUser)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s got %d: %s", query, rec.Code, rec.Body.String())
	}
	var out AdminLLMCallsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func ptr[T any](v T) *T { return &v }

// pricedCall is a succeeded worker call with the rate snapshot a cost is figured
// from: 10k fresh prompt tokens and 1k output at the rates below.
func pricedCall() coregw.Call {
	return coregw.Call{
		ID:                "lc_1",
		UserID:            ptr("u_alice"),
		TaskRunID:         ptr("r_1"),
		Surface:           coregw.CallSurfaceWorker,
		Model:             "gpt-luna",
		TargetID:          "tgt_7",
		ProviderType:      "openai_compatible",
		UpstreamModel:     "openai/gpt-luna",
		Status:            coregw.CallStatusSucceeded,
		AcceptedAt:        time.Unix(1000, 0).UTC(),
		PromptTokens:      ptr(10_000),
		CompletionTokens:  ptr(1_000),
		TotalTokens:       ptr(11_000),
		UsageSource:       coregw.UsageSourceReported,
		Currency:          "USD",
		RateInputPerMTok:  ptr(int64(3_000_000_000)),
		RateOutputPerMTok: ptr(int64(15_000_000_000)),
	}
}

// TestAdminLLMCallsShowsRoutingTheSpaceViewHides is the reason this route maps
// its own row: the operator reading it is who decided the routing, so target_id,
// provider_type, and upstream_model appear here where the space-scoped view drops
// them.
func TestAdminLLMCallsShowsRoutingTheSpaceViewHides(t *testing.T) {
	mux := llmCallsMux(t, &fakeCallLedger{calls: []coregw.Call{pricedCall()}})
	out := getLLMCalls(t, mux, "")
	if out.Total != 1 || len(out.Calls) != 1 {
		t.Fatalf("total=%d calls=%d, want 1/1", out.Total, len(out.Calls))
	}
	got := out.Calls[0]
	if got.TargetID != "tgt_7" || got.ProviderType != "openai_compatible" || got.UpstreamModel != "openai/gpt-luna" {
		t.Errorf("routing fields missing: %+v", got)
	}
	if got.Cost == nil {
		t.Fatal("a priced row produced no cost")
	}
	// 10k fresh at $3/M + 1k out at $15/M.
	if got.Cost.Uncached != 30_000_000 || got.Cost.Output != 15_000_000 {
		t.Errorf("cost = %+v", *got.Cost)
	}
}

func TestAdminLLMCallsPassesEveryFilterToTheStore(t *testing.T) {
	ledger := &fakeCallLedger{}
	mux := llmCallsMux(t, ledger)
	query := "?user_id=u_alice&model=gpt-luna&status=SUCCEEDED&surface=worker" +
		"&since=" + rfc3339(200) + "&until=" + rfc3339(400) + "&limit=10&offset=20"
	getLLMCalls(t, mux, query)

	f := ledger.lastFilter
	if f.UserID != "u_alice" || f.Model != "gpt-luna" || f.Status != "SUCCEEDED" || f.Surface != "worker" {
		t.Errorf("filter = %+v", f)
	}
	if !f.Since.Equal(time.Unix(200, 0).UTC()) || !f.Until.Equal(time.Unix(400, 0).UTC()) {
		t.Errorf("bounds = %v..%v", f.Since, f.Until)
	}
	if ledger.lastLimit != 10 || ledger.lastOffset != 20 {
		t.Errorf("window = limit %d offset %d, want 10/20", ledger.lastLimit, ledger.lastOffset)
	}
}

// TestAdminLLMCallsRejectsAMalformedTimestamp: unlike the audit search, this
// route validates its bounds, matching the account list it sits beside.
func TestAdminLLMCallsRejectsAMalformedTimestamp(t *testing.T) {
	mux := llmCallsMux(t, &fakeCallLedger{})
	rec := adminRequestAs(t, mux, adminCase{"GET", "/api/admin/llm/calls?since=yesterday"}, adminUser)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("got %d, want 400", rec.Code)
	}
}

// TestAdminLLMCallsWithoutAStoreReports503 keeps the ledger's absence a distinct
// answer from an empty one.
func TestAdminLLMCallsWithoutAStoreReports503(t *testing.T) {
	mux := llmCallsMux(t, nil)
	rec := adminRequestAs(t, mux, adminCase{"GET", "/api/admin/llm/calls"}, adminUser)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("got %d, want 503", rec.Code)
	}
}

// TestAdminLLMCallsCarriesNoContent: the ledger is an accounting record, and
// reading it across every space must not become a way to read across them.
func TestAdminLLMCallsCarriesNoContent(t *testing.T) {
	mux := llmCallsMux(t, &fakeCallLedger{calls: []coregw.Call{pricedCall()}})
	rec := adminRequestAs(t, mux, adminCase{"GET", "/api/admin/llm/calls"}, adminUser)
	body := strings.ToLower(rec.Body.String())
	for _, forbidden := range []string{"prompt\"", "message", "output_text", "content", "input\""} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the response carries a %q field: %s", forbidden, rec.Body.String())
		}
	}
}

func (f *fakeCallLedger) SummarizeForegroundLLMCalls(context.Context, string, time.Time) ([]coregw.CallTotals, error) {
	return nil, nil
}
