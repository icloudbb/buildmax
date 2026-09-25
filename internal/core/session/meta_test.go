package session

import (
	"errors"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/core/llm"
)

func TestNewMetaHidesSubagentsByDefault(t *testing.T) {
	user := NewMeta("s1", KindUser, testTime)
	if user.Hidden {
		t.Error("a user session must not start hidden")
	}
	sub := NewMeta("s2", KindSubagent, testTime)
	if !sub.Hidden {
		t.Error("a subagent session must start hidden")
	}
	if user.CreatedAt != user.UpdatedAt {
		t.Error("created and updated must start equal")
	}
}

func TestMetaValidate(t *testing.T) {
	if err := NewMeta("s1", KindUser, testTime).Validate(); err != nil {
		t.Fatalf("valid meta rejected: %v", err)
	}
	if err := (Meta{Kind: KindUser}).Validate(); err == nil {
		t.Error("meta with no id accepted")
	}
	if err := (Meta{ID: "s1", Kind: "bogus"}).Validate(); err == nil {
		t.Error("meta with an unknown kind accepted")
	}
}

func TestApplyMetaUpdateChangesOnlyNamedFields(t *testing.T) {
	m := NewMeta("s1", KindUser, testTime)
	m.Title = "before"
	later := testTime.Add(time.Hour)

	title := "after"
	got := ApplyMetaUpdate(m, MetaUpdate{Title: &title}, later)

	if got.Title != "after" {
		t.Errorf("title = %q, want after", got.Title)
	}
	if got.Workspace != m.Workspace || got.SelectedModel != m.SelectedModel || got.Pinned != m.Pinned {
		t.Error("fields with a nil update pointer must not change")
	}
	if !got.UpdatedAt.Equal(later) {
		t.Errorf("updated_at = %v, want %v", got.UpdatedAt, later)
	}
	if m.Title != "before" {
		t.Error("ApplyMetaUpdate mutated its input")
	}
}

func TestApplyMetaUpdateAccumulatesUsage(t *testing.T) {
	m := NewMeta("s1", KindUser, testTime)
	m = ApplyMetaUpdate(m, MetaUpdate{AddPromptTokens: 100, AddCompletionTokens: 20}, testTime)
	m = ApplyMetaUpdate(m, MetaUpdate{AddPromptTokens: 50, AddCompletionTokens: 5}, testTime)
	if m.PromptTokens != 150 || m.CompletionTokens != 25 {
		t.Errorf("usage = %+v, want 150/25", m)
	}
}

func TestApplyMetaUpdateSumsCostAndFlagsMismatchedCurrency(t *testing.T) {
	m := NewMeta("s1", KindUser, testTime)
	m = ApplyMetaUpdate(m, MetaUpdate{AddCost: &llm.Cost{Currency: "USD", Total: 100}}, testTime)
	m = ApplyMetaUpdate(m, MetaUpdate{AddCost: &llm.Cost{Currency: "USD", Total: 50}}, testTime)
	if m.Cost == nil || m.Cost.Total != 150 {
		t.Fatalf("cost = %+v, want total 150", m.Cost)
	}
	if m.CostIncomplete {
		t.Fatal("matching-currency sum marked incomplete")
	}

	// A currency this build cannot convert must not silently vanish or produce
	// an invented total; the earlier total stands and is labelled incomplete.
	m = ApplyMetaUpdate(m, MetaUpdate{AddCost: &llm.Cost{Currency: "EUR", Total: 10}}, testTime)
	if m.Cost.Total != 150 {
		t.Errorf("total changed on currency mismatch: %+v", m.Cost)
	}
	if !m.CostIncomplete {
		t.Error("currency mismatch did not mark the total incomplete")
	}
}

func TestApplyMetaUpdateRecordsUsageBearingTurns(t *testing.T) {
	m := NewMeta("s1", KindUser, testTime)
	t1 := testTime.Add(time.Minute)
	t2 := testTime.Add(2 * time.Minute)

	m = ApplyMetaUpdate(m, MetaUpdate{
		AddPromptTokens: 100, AddCompletionTokens: 20,
		AddCost: &llm.Cost{Currency: "USD", Total: 30},
	}, t1)
	m = ApplyMetaUpdate(m, MetaUpdate{AddPromptTokens: 50, AddCompletionTokens: 5}, t2)

	if len(m.Turns) != 2 {
		t.Fatalf("turns = %d, want 2", len(m.Turns))
	}
	if !m.Turns[0].At.Equal(t1) || m.Turns[0].PromptTokens != 100 || m.Turns[0].CompletionTokens != 20 {
		t.Errorf("first turn = %+v", m.Turns[0])
	}
	if m.Turns[0].Cost == nil || m.Turns[0].Cost.Total != 30 {
		t.Errorf("first turn cost = %+v", m.Turns[0].Cost)
	}
	if m.Turns[1].Cost != nil {
		t.Errorf("second turn recorded a cost it did not carry: %+v", m.Turns[1].Cost)
	}

	// The invariant the breakdown exists to hold: summing the per-turn deltas
	// reproduces the running totals.
	var sumPrompt, sumCompletion int
	for _, tn := range m.Turns {
		sumPrompt += tn.PromptTokens
		sumCompletion += tn.CompletionTokens
	}
	if sumPrompt != m.PromptTokens || sumCompletion != m.CompletionTokens {
		t.Errorf("turn deltas %d/%d do not sum to totals %d/%d",
			sumPrompt, sumCompletion, m.PromptTokens, m.CompletionTokens)
	}
}

func TestApplyMetaUpdateRecordsNoTurnWithoutUsage(t *testing.T) {
	m := NewMeta("s1", KindUser, testTime)
	title, model := "renamed", "gpt"
	m = ApplyMetaUpdate(m, MetaUpdate{Title: &title, SelectedModel: &model}, testTime)
	if len(m.Turns) != 0 {
		t.Errorf("a metadata-only change recorded %d turns, want 0", len(m.Turns))
	}
}

func TestApplyMetaUpdateDoesNotMutateTurns(t *testing.T) {
	m := NewMeta("s1", KindUser, testTime)
	m = ApplyMetaUpdate(m, MetaUpdate{AddPromptTokens: 1}, testTime)
	before := m // shares the Turns backing array

	got := ApplyMetaUpdate(m, MetaUpdate{AddPromptTokens: 1}, testTime)
	if len(before.Turns) != 1 {
		t.Errorf("input's Turns grew to %d; ApplyMetaUpdate mutated its argument", len(before.Turns))
	}
	if len(got.Turns) != 2 {
		t.Errorf("result Turns = %d, want 2", len(got.Turns))
	}
}

func TestApplyMetaUpdatePreservesLineageAndForkedFrom(t *testing.T) {
	// MetaUpdate has no field for these, so this is really a compile-time
	// guarantee; the test documents the intent for a reader who might be
	// tempted to add one.
	m := NewMeta("s1", KindSubagent, testTime)
	m.ParentSessionID = "parent"
	m.AgentType = "explorer"
	got := ApplyMetaUpdate(m, MetaUpdate{}, testTime)
	if got.ParentSessionID != "parent" || got.AgentType != "explorer" || got.Kind != KindSubagent {
		t.Errorf("lineage changed: %+v", got)
	}
}

// A session is bound to where its first turn went; a later turn may go only
// there, whichever way the mode changed.
func TestCheckDestination(t *testing.T) {
	fresh := Meta{}
	if err := CheckDestination(fresh, "https://buildmax.example.com"); err != nil {
		t.Errorf("an unbound session refused a destination: %v", err)
	}
	local := Meta{PromptDestination: DestinationLocal}
	if err := CheckDestination(local, DestinationLocal); err != nil {
		t.Errorf("a local session refused local: %v", err)
	}
	if err := CheckDestination(local, "https://buildmax.example.com"); !errors.Is(err, ErrDestinationMismatch) {
		t.Errorf("a local session accepted a deployment: %v", err)
	}
	managed := Meta{PromptDestination: "https://buildmax.example.com"}
	if err := CheckDestination(managed, DestinationLocal); !errors.Is(err, ErrDestinationMismatch) {
		t.Errorf("a managed session accepted local mode: %v", err)
	}
	if err := CheckDestination(managed, "https://other.example.com"); !errors.Is(err, ErrDestinationMismatch) {
		t.Errorf("a managed session accepted another deployment: %v", err)
	}
}
