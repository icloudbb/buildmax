package quota

import (
	"context"
	"strings"
	"testing"

	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	corequota "github.com/icloudbb/buildmax/internal/core/quota"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/mock"
)

func alertingService(runs, tokens int, maxRuns, maxTokens int) (*Service, *mock.MockAuditStore) {
	audits := &mock.MockAuditStore{}
	return &Service{
		SpaceStore:  &mockSpaceStore{space: &corespace.Space{ID: "tm_1", QuotaTier: "free_trial"}},
		UsageReader: &mockUsageReader{runCount: runs, totalTokens: tokens},
		TierStore: &mockTierStore{tier: &corequota.Tier{
			TierName:           "free_trial",
			MaxRunsPerPeriod:   maxRuns,
			MaxTokensPerPeriod: maxTokens,
			PeriodDays:         30,
		}},
		DefaultTier: "free_trial",
		Audit:       audits,
	}, audits
}

func actions(audits *mock.MockAuditStore) []string {
	out := make([]string, 0, len(audits.Events))
	for _, e := range audits.Events {
		out = append(out, e.Action)
	}
	return out
}

// A space that is about to start having work refused should hear about it
// before the refusals, not after. The crossing is only visible on the
// admission path: usage is a rolling window, so there is no period boundary a
// sweep could notice it at.
func TestCheckRecordsCrossingTheThreshold(t *testing.T) {
	c, audits := alertingService(7, 0, 10, 100_000)

	if refusal, _ := c.Check(context.Background(), "tm_1", 1, 0); refusal != nil {
		t.Fatal("Check refused a run inside the limit")
	}
	if len(audits.Events) != 1 {
		t.Fatalf("recorded %v, want one threshold event", actions(audits))
	}
	event := audits.Events[0]
	if event.Action != coreaudit.QuotaThresholdReached {
		t.Errorf("action = %q, want %q", event.Action, coreaudit.QuotaThresholdReached)
	}
	if !strings.HasPrefix(event.Detail, "runs") {
		t.Errorf("detail = %q, want it to name the limit", event.Detail)
	}
	// The actor is the deployment, not whoever submitted the work that tipped
	// the total over. A quota is the space's, and naming the last member to
	// submit would read as blame for a shared budget.
	if event.ActorType != coreaudit.ActorSystem {
		t.Errorf("actor type = %q, want %q", event.ActorType, coreaudit.ActorSystem)
	}
}

// Below the threshold there is nothing to say, and saying it anyway would make
// the trail a log of ordinary work.
func TestCheckRecordsNothingWellInsideTheLimit(t *testing.T) {
	c, audits := alertingService(1, 0, 10, 100_000)

	if refusal, _ := c.Check(context.Background(), "tm_1", 1, 0); refusal != nil {
		t.Fatal("Check refused a run inside the limit")
	}
	if len(audits.Events) != 0 {
		t.Fatalf("recorded %v, want nothing", actions(audits))
	}
}

// A space that keeps submitting at 90% must not turn its own trail into a log
// of retries. One warning per limit per period is the whole point.
func TestCheckRecordsOneWarningPerLimitPerPeriod(t *testing.T) {
	c, audits := alertingService(9, 0, 10, 100_000)

	for range 5 {
		if _, err := c.Check(context.Background(), "tm_1", 0, 0); err != nil {
			t.Fatalf("Check: %v", err)
		}
	}
	if len(audits.Events) != 1 {
		t.Fatalf("recorded %v, want exactly one", actions(audits))
	}
}

// The two limits are separate facts, and a space can be near one and nowhere
// near the other.
func TestCheckRecordsEachLimitSeparately(t *testing.T) {
	c, audits := alertingService(9, 95_000, 10, 100_000)

	if _, err := c.Check(context.Background(), "tm_1", 0, 0); err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(audits.Events) != 2 {
		t.Fatalf("recorded %v, want one per limit", actions(audits))
	}
	details := audits.Events[0].Detail + "|" + audits.Events[1].Detail
	if !strings.Contains(details, "runs") || !strings.Contains(details, "tokens") {
		t.Errorf("details = %q, want both limits named", details)
	}
}

// A refusal is a different event from a warning, because it calls for a
// different response: one is a heads-up, the other is work not happening.
func TestCheckRecordsTheRefusal(t *testing.T) {
	c, audits := alertingService(10, 0, 10, 100_000)

	refusal, _ := c.Check(context.Background(), "tm_1", 1, 0)
	if refusal == nil {
		t.Fatal("Check allowed a run past the limit")
	}
	if refusal.Reason() != "quota exceeded: run limit" {
		t.Errorf("reason = %q", refusal.Reason())
	}
	if len(audits.Events) != 1 || audits.Events[0].Action != coreaudit.QuotaExceeded {
		t.Fatalf("recorded %v, want one %q", actions(audits), coreaudit.QuotaExceeded)
	}
}

// Enforcement is the point; the record of it is what a space admin reads
// afterwards. A deployment with no audit store must still enforce quota rather
// than fail an admission because there was nowhere to write.
func TestCheckEnforcesWithoutAnAuditStore(t *testing.T) {
	c, _ := alertingService(10, 0, 10, 100_000)
	c.Audit = nil

	if refusal, _ := c.Check(context.Background(), "tm_1", 1, 0); refusal == nil {
		t.Error("Check allowed a run past the limit when auditing was off")
	}
}
