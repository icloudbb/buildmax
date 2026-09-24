package space

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
	corequota "github.com/icloudbb/buildmax/internal/core/quota"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/quota"
	"github.com/icloudbb/buildmax/internal/testsupport"
	"github.com/icloudbb/buildmax/internal/util"
)

// foregroundLedger answers only the summary; the usage routes read nothing else.
type foregroundLedger struct {
	coregw.CallStore
	groups   []coregw.CallTotals
	gotUser  string
	gotSince time.Time
}

func (l *foregroundLedger) SummarizeForegroundLLMCalls(_ context.Context, userID string, since time.Time) ([]coregw.CallTotals, error) {
	l.gotUser, l.gotSince = userID, since
	return l.groups, nil
}

// A signed-in CLI or Desktop session's calls belong to no space, so the
// personal usage route reports them beside the space figures; otherwise the
// user has nowhere to see what those sessions cost.
func TestPersonalUsageIncludesTheCallersOwnManagedCalls(t *testing.T) {
	const secret, userID, spaceID = "test-usage-secret", "u1", "tm_personal_u1"
	spaceStore := &mock.MockSpaceStore{
		Spaces:  []corespace.Space{{ID: spaceID, Name: "Mine", PersonalForUserID: util.Ptr(userID), QuotaTier: "free_trial", CreatedBy: userID, CreatedAt: time.Now().UTC()}},
		Members: []corespace.Member{{SpaceID: spaceID, UserID: userID, Role: corespace.RoleOwner, CreatedAt: time.Now().UTC()}},
	}
	checker := &quota.Service{
		SpaceStore:  spaceStore,
		UsageReader: &mock.MockUsageReader{RunCount: 4, TotalTokens: 30},
		TierStore:   &mock.MockTierStore{Tier: &corequota.Tier{TierName: "free_trial", PeriodDays: 30}},
		DefaultTier: "free_trial",
	}
	ledger := &foregroundLedger{groups: []coregw.CallTotals{
		// Two priced calls: 1M fresh input at 0.2 and 1M output at 1.2 per MTok.
		{Calls: 2, PromptTokens: 1_000_000, CompletionTokens: 1_000_000, TotalTokens: 2_000_000,
			Currency: "USD", RateInputPerMTok: 200_000_000, RateOutputPerMTok: 1_200_000_000},
		{Calls: 1, PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}}
	h := New(Config{JWTSecret: secret, Spaces: spaceStore, Quota: checker, LLMCalls: ledger})
	mux := http.NewServeMux()
	h.Register(mux)

	get := func(path string) map[string]json.RawMessage {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT(userID, secret))
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", path, rec.Code, rec.Body.String())
		}
		var body map[string]json.RawMessage
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return body
	}

	body := get("/api/usage")
	var managed struct {
		Calls         int `json:"call_count"`
		TotalTokens   int `json:"total_tokens"`
		UnpricedCalls int `json:"unpriced_calls"`
		Costs         []struct {
			Currency string `json:"currency"`
			Total    int64  `json:"total"`
		} `json:"costs"`
	}
	if err := json.Unmarshal(body["managed_calls"], &managed); err != nil {
		t.Fatalf("managed_calls: %v (%s)", err, body["managed_calls"])
	}
	if managed.Calls != 3 || managed.TotalTokens != 2_000_015 || managed.UnpricedCalls != 1 {
		t.Errorf("managed_calls = %+v", managed)
	}
	if len(managed.Costs) != 1 || managed.Costs[0].Currency != "USD" || managed.Costs[0].Total != 1_400_000_000 {
		t.Errorf("costs = %+v, want 1.4 USD", managed.Costs)
	}
	if ledger.gotUser != userID || time.Since(ledger.gotSince) < 29*24*time.Hour {
		t.Errorf("summary asked for user %q since %v, want the caller over the 30-day period", ledger.gotUser, ledger.gotSince)
	}

	// A space's usage is the space's; one member's own sessions are not in it.
	if _, ok := get("/api/spaces/" + spaceID + "/usage")["managed_calls"]; ok {
		t.Error("the space usage route reported one member's own calls")
	}
}
