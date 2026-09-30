package quota

import (
	"context"
	"errors"
	"strings"
	"testing"

	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	corequota "github.com/icloudbb/buildmax/internal/core/quota"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"github.com/icloudbb/buildmax/internal/mock"
)

func tierService(runs int) (*Service, *mock.MockSpaceStore, *mock.MockAuditStore) {
	spaces := &mock.MockSpaceStore{Spaces: []corespace.Space{{ID: "tm_1", QuotaTier: "free_trial"}}}
	audits := &mock.MockAuditStore{}
	return &Service{
		SpaceStore:  spaces,
		UsageReader: &mockUsageReader{runCount: runs},
		TierStore: &mock.MockTierCatalog{Tiers: []corequota.Tier{
			{TierName: "free_trial", MaxRunsPerPeriod: 10, MaxTokensPerPeriod: 100_000, PeriodDays: 30},
			{TierName: "pro", MaxRunsPerPeriod: 1000, MaxTokensPerPeriod: 10_000_000, PeriodDays: 30},
		}},
		DefaultTier: "free_trial",
		Audit:       audits,
	}, spaces, audits
}

// The operator's lever for a space that hit its limit: the next admission
// reads the new tier, with nothing restarted in between.
func TestAssignTierTakesEffectAtTheNextCheck(t *testing.T) {
	svc, _, _ := tierService(10)
	ctx := context.Background()

	if allowed, _, err := svc.Check(ctx, "tm_1", 1, 0); err != nil || allowed {
		t.Fatalf("setup: Check on free_trial = %v, %v; want a refusal at the run limit", allowed, err)
	}
	previous, err := svc.AssignTier(ctx, "u_admin", "tm_1", "pro")
	if err != nil {
		t.Fatalf("AssignTier: %v", err)
	}
	if previous != "free_trial" {
		t.Errorf("previous = %q, want free_trial", previous)
	}
	if allowed, reason, err := svc.Check(ctx, "tm_1", 1, 0); err != nil || !allowed {
		t.Errorf("Check on pro = %v (%q), %v; want admitted", allowed, reason, err)
	}
}

func TestAssignTierRecordsTheChange(t *testing.T) {
	svc, _, audits := tierService(0)
	if _, err := svc.AssignTier(context.Background(), "u_admin", "tm_1", "pro"); err != nil {
		t.Fatalf("AssignTier: %v", err)
	}
	if len(audits.Events) != 1 {
		t.Fatalf("recorded %d events, want 1: %+v", len(audits.Events), audits.Events)
	}
	e := audits.Events[0]
	if e.Action != coreaudit.SpaceQuotaTierChanged || e.ActorType != coreaudit.ActorUser || e.ActorID != "u_admin" ||
		e.SpaceID != "tm_1" || e.TargetType != "space" || e.TargetID != "tm_1" || e.Detail != "free_trial -> pro" {
		t.Errorf("event = %+v", e)
	}
}

// An unknown tier must not reach the column: Check would read it as "no
// limit", so a typo would silently unmeter the space.
func TestAssignTierRefusesAnUnknownTier(t *testing.T) {
	svc, spaces, audits := tierService(0)
	_, err := svc.AssignTier(context.Background(), "u_admin", "tm_1", "enterprise")
	if !errors.Is(err, ErrUnknownTier) {
		t.Fatalf("err = %v, want ErrUnknownTier", err)
	}
	if msg := err.Error(); !strings.Contains(msg, `"enterprise"`) || !strings.Contains(msg, "free_trial, pro") {
		t.Errorf("message %q should name the tier asked for and the valid ones", msg)
	}
	if spaces.Spaces[0].QuotaTier != "free_trial" {
		t.Errorf("tier changed to %q on a refusal", spaces.Spaces[0].QuotaTier)
	}
	if len(audits.Events) != 0 {
		t.Errorf("a refusal was recorded: %+v", audits.Events)
	}
}

func TestAssignTierOnAnUnknownSpace(t *testing.T) {
	svc, _, _ := tierService(0)
	if _, err := svc.AssignTier(context.Background(), "u_admin", "tm_nobody", "pro"); !errors.Is(err, ErrSpaceNotFound) {
		t.Errorf("err = %v, want ErrSpaceNotFound", err)
	}
}

func TestAssignTierToTheCurrentTierRecordsNothing(t *testing.T) {
	svc, _, audits := tierService(0)
	previous, err := svc.AssignTier(context.Background(), "u_admin", "tm_1", "free_trial")
	if err != nil || previous != "free_trial" {
		t.Fatalf("AssignTier = %q, %v", previous, err)
	}
	if len(audits.Events) != 0 {
		t.Errorf("a no-op was recorded: %+v", audits.Events)
	}
}

// A space created with no tier runs under the deployment default, so that is
// the tier the trail says it left.
func TestAssignTierNamesTheDefaultAsTheOldTier(t *testing.T) {
	svc, spaces, audits := tierService(0)
	spaces.Spaces[0].QuotaTier = ""
	previous, err := svc.AssignTier(context.Background(), "u_admin", "tm_1", "pro")
	if err != nil || previous != "free_trial" {
		t.Fatalf("AssignTier = %q, %v; want free_trial", previous, err)
	}
	if len(audits.Events) != 1 || audits.Events[0].Detail != "free_trial -> pro" {
		t.Errorf("events = %+v", audits.Events)
	}
}
