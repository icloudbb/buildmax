package db

import (
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/core/agentdef"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

// The personal usage route totals one user's own sessions: calls outside any
// task run, since a date, grouped by the rates they were priced at.
func TestSummarizeForegroundLLMCalls(t *testing.T) {
	s, ctx := newTestStore(t)
	user := newTestUser(t, s, "summary")
	other := newTestUser(t, s, "summary-other")
	spaceID := newTestSpace(t, s, user)
	agent, err := s.CreateAgentInSpace(ctx, agentdef.CreateInput{SpaceID: spaceID, UserID: user, Def: agentdef.Definition{Name: "summary-runner"}})
	if err != nil {
		t.Fatalf("CreateAgentInSpace: %v", err)
	}
	task, err := s.CreateTask(ctx, &coretask.CreateInput{SpaceID: spaceID, AgentID: &agent.ID, Input: "x", CreatedBy: user})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	model := "summary-" + testPublicID(t)
	t.Cleanup(func() {
		_ = s.db.WithContext(ctx).Delete(&llmCallRow{}, "model = ?", model)
		_ = s.db.WithContext(ctx).Delete(&taskRunRow{}, "task_id = (SELECT id FROM task WHERE public_id = ?)", task.ID)
		_ = s.db.WithContext(ctx).Delete(&taskRow{}, "public_id = ?", task.ID)
	})

	now := time.Now().UTC().Truncate(time.Second)
	rate := func(v int64) *int64 { return &v }
	seed := func(owner string, runID *string, accepted time.Time, priced bool, prompt, completion int) {
		t.Helper()
		call := sampleLLMCall()
		call.UserID = ptrString(owner)
		call.TaskRunID = runID
		call.Model = model
		call.AcceptedAt = accepted
		if priced {
			call.Currency = "USD"
			call.RateInputPerMTok = rate(200_000_000)
			call.RateCacheReadPerMTok = rate(0)
			call.RateCacheWritePerMTok = rate(0)
			call.RateOutputPerMTok = rate(1_200_000_000)
		}
		opened, err := s.OpenLLMCall(ctx, call)
		if err != nil {
			t.Fatalf("OpenLLMCall: %v", err)
		}
		if err := s.CompleteLLMCall(ctx, opened.ID, coregw.CallOutcome{
			Status: coregw.CallStatusSucceeded, Attempts: 1, CompletedAt: accepted,
			Usage: &coregw.CallUsage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: prompt + completion, Source: coregw.UsageSourceReported},
		}); err != nil {
			t.Fatalf("CompleteLLMCall: %v", err)
		}
	}

	seed(user, nil, now, true, 100, 10)                          // counted, priced
	seed(user, nil, now, true, 200, 20)                          // counted, same snapshot
	seed(user, nil, now, false, 5, 1)                            // counted, unpriced group
	seed(user, task.LastRunID, now, true, 9_000, 900)            // a task run's call: not foreground
	seed(user, nil, now.Add(-40*24*time.Hour), true, 7_000, 700) // outside the window
	seed(other, nil, now, true, 8_000, 800)                      // someone else's

	groups, err := s.SummarizeForegroundLLMCalls(ctx, user, now.Add(-30*24*time.Hour))
	if err != nil {
		t.Fatalf("SummarizeForegroundLLMCalls: %v", err)
	}
	var priced, unpriced *coregw.CallTotals
	for i := range groups {
		if groups[i].Currency == "USD" {
			priced = &groups[i]
		} else {
			unpriced = &groups[i]
		}
	}
	if len(groups) != 2 || priced == nil || unpriced == nil {
		t.Fatalf("groups = %+v, want one priced and one unpriced", groups)
	}
	if priced.Calls != 2 || priced.PromptTokens != 300 || priced.CompletionTokens != 30 || priced.RateOutputPerMTok != 1_200_000_000 {
		t.Errorf("priced group = %+v", *priced)
	}
	if unpriced.Calls != 1 || unpriced.TotalTokens != 6 {
		t.Errorf("unpriced group = %+v", *unpriced)
	}

	if got, err := s.SummarizeForegroundLLMCalls(ctx, "not a public id", now); err != nil || len(got) != 0 {
		t.Errorf("unparseable user = %v, %v; want nothing", got, err)
	}
}
