package db

import (
	"testing"
	"time"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/util"
)

// SpaceUsageInWindow is the query behind every quota decision, and quota is a
// window-boundary arithmetic problem: a run at the edge of the window counts, a
// run a second outside it does not, and one space's spend never lands on
// another's bill. Only a real database exercises the join and the inclusive
// >= / <= comparison the mock reader cannot. See
// docs/design/verification-program.md §4.2.

// seedRun creates a task and its initial run in the space, then places the run
// at runAt with the given token counts. The task itself is pushed far into the
// past with no title tokens, so only the run's created_at and tokens matter.
func seedRun(t *testing.T, s *Store, spaceID, userID string, runAt time.Time, prompt, completion int) {
	t.Helper()
	ctx := t.Context()
	conv, err := s.CreateConversationInSpace(ctx, spaceID, userID, "portal", userID)
	if err != nil {
		t.Fatalf("CreateConversationInSpace: %v", err)
	}
	task, err := s.CreateTask(ctx, &coretask.CreateInput{
		SpaceID:        spaceID,
		ConversationID: conv.ID,
		Input:          "input",
		CreatedBy:      userID,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if task.LastRunID == nil {
		t.Fatal("CreateTask did not create its first run")
	}
	t.Cleanup(func() {
		_ = s.db.Delete(&taskRunRow{}, "public_id = ?", canonicalPublicID(*task.LastRunID)).Error
		_ = s.db.Delete(&taskRow{}, "public_id = ?", canonicalPublicID(task.ID)).Error
		_ = s.db.Delete(&conversationRow{}, "public_id = ?", canonicalPublicID(conv.ID)).Error
	})
	if err := s.db.Model(&taskRow{}).Where("public_id = ?", canonicalPublicID(task.ID)).
		Update("created_at", runAt.Add(-365*24*time.Hour)).Error; err != nil {
		t.Fatalf("push task out of window: %v", err)
	}
	if err := s.db.Model(&taskRunRow{}).Where("public_id = ?", canonicalPublicID(*task.LastRunID)).
		Updates(map[string]any{
			"created_at":        runAt,
			"prompt_tokens":     prompt,
			"completion_tokens": completion,
		}).Error; err != nil {
		t.Fatalf("place run: %v", err)
	}
}

func TestSpaceUsageInWindowCountsRunsOnTheInclusiveBoundary(t *testing.T) {
	s, ctx := newTestStore(t)
	userID := newTestUser(t, s, "quota-window")
	spaceID := newTestSpace(t, s, userID)

	since := time.Date(2026, 1, 15, 12, 0, 0, 0, time.UTC)
	until := since.Add(10 * time.Minute)

	// Two runs sit exactly on each edge (included), two a second beyond each
	// edge (excluded). Each in-window run bills 10+5 tokens.
	seedRun(t, s, spaceID, userID, since, 10, 5)
	seedRun(t, s, spaceID, userID, until, 10, 5)
	seedRun(t, s, spaceID, userID, since.Add(-time.Second), 999, 999)
	seedRun(t, s, spaceID, userID, until.Add(time.Second), 999, 999)

	runCount, tokens, err := s.SpaceUsageInWindow(ctx, spaceID, since, until)
	if err != nil {
		t.Fatalf("SpaceUsageInWindow: %v", err)
	}
	if runCount != 2 {
		t.Errorf("runCount = %d, want 2 (only the two boundary runs)", runCount)
	}
	if tokens != 30 {
		t.Errorf("tokens = %d, want 30 (two runs of 15)", tokens)
	}
}

func TestSpaceUsageInWindowBillsTitleTokensByTaskCreatedAt(t *testing.T) {
	s, ctx := newTestStore(t)
	userID := newTestUser(t, s, "quota-title")
	spaceID := newTestSpace(t, s, userID)

	since := time.Date(2026, 2, 1, 9, 0, 0, 0, time.UTC)
	until := since.Add(time.Hour)

	// A task whose title generation spent tokens inside the window, but whose
	// only run sits outside it: title tokens are billed even with no run in
	// range, and they follow the task's created_at, not the run's.
	conv, err := s.CreateConversationInSpace(ctx, spaceID, userID, "portal", userID)
	if err != nil {
		t.Fatalf("CreateConversationInSpace: %v", err)
	}
	task, err := s.CreateTask(ctx, &coretask.CreateInput{
		SpaceID:               spaceID,
		ConversationID:        conv.ID,
		Input:                 "input",
		CreatedBy:             userID,
		TitlePromptTokens:     7,
		TitleCompletionTokens: 3,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() {
		_ = s.db.Delete(&taskRunRow{}, "public_id = ?", canonicalPublicID(*task.LastRunID)).Error
		_ = s.db.Delete(&taskRow{}, "public_id = ?", canonicalPublicID(task.ID)).Error
		_ = s.db.Delete(&conversationRow{}, "public_id = ?", canonicalPublicID(conv.ID)).Error
	})
	if err := s.db.Model(&taskRow{}).Where("public_id = ?", canonicalPublicID(task.ID)).
		Update("created_at", since.Add(30*time.Minute)).Error; err != nil {
		t.Fatalf("place task in window: %v", err)
	}
	if err := s.db.Model(&taskRunRow{}).Where("public_id = ?", canonicalPublicID(*task.LastRunID)).
		Update("created_at", since.Add(-time.Hour)).Error; err != nil {
		t.Fatalf("push run out of window: %v", err)
	}

	runCount, tokens, err := s.SpaceUsageInWindow(ctx, spaceID, since, until)
	if err != nil {
		t.Fatalf("SpaceUsageInWindow: %v", err)
	}
	if runCount != 0 {
		t.Errorf("runCount = %d, want 0 (the run is outside the window)", runCount)
	}
	if tokens != 10 {
		t.Errorf("tokens = %d, want 10 (title tokens only)", tokens)
	}
}

func TestSpaceUsageInWindowIsScopedToOneSpace(t *testing.T) {
	s, ctx := newTestStore(t)
	since := time.Date(2026, 3, 10, 0, 0, 0, 0, time.UTC)
	until := since.Add(time.Hour)
	mid := since.Add(30 * time.Minute)

	owner := newTestUser(t, s, "quota-scope")
	spaceA := newTestSpace(t, s, owner)
	spaceB := newTestSpace(t, s, owner)
	seedRun(t, s, spaceA, owner, mid, 10, 5)
	seedRun(t, s, spaceB, owner, mid, 1000, 1000)

	runCount, tokens, err := s.SpaceUsageInWindow(ctx, spaceA, since, until)
	if err != nil {
		t.Fatalf("SpaceUsageInWindow: %v", err)
	}
	if runCount != 1 || tokens != 15 {
		t.Errorf("space A usage = (%d runs, %d tokens), want (1, 15); space B's run must not count", runCount, tokens)
	}
}

func TestSpaceUsageInWindowCountsNullRunTokensAsZero(t *testing.T) {
	s, ctx := newTestStore(t)
	userID := newTestUser(t, s, "quota-null")
	spaceID := newTestSpace(t, s, userID)

	since := time.Date(2026, 4, 5, 6, 0, 0, 0, time.UTC)
	until := since.Add(time.Hour)

	// A fresh run has never recorded usage, so its token columns are NULL. The
	// query must fold that to zero rather than skip the run or error.
	conv, err := s.CreateConversationInSpace(ctx, spaceID, userID, "portal", userID)
	if err != nil {
		t.Fatalf("CreateConversationInSpace: %v", err)
	}
	task, err := s.CreateTask(ctx, &coretask.CreateInput{
		SpaceID:        spaceID,
		ConversationID: conv.ID,
		Input:          "input",
		CreatedBy:      userID,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	t.Cleanup(func() {
		_ = s.db.Delete(&taskRunRow{}, "public_id = ?", canonicalPublicID(*task.LastRunID)).Error
		_ = s.db.Delete(&taskRow{}, "public_id = ?", canonicalPublicID(task.ID)).Error
		_ = s.db.Delete(&conversationRow{}, "public_id = ?", canonicalPublicID(conv.ID)).Error
	})
	if err := s.db.Model(&taskRunRow{}).Where("public_id = ?", canonicalPublicID(*task.LastRunID)).
		Update("created_at", since.Add(30*time.Minute)).Error; err != nil {
		t.Fatalf("place run: %v", err)
	}

	runCount, tokens, err := s.SpaceUsageInWindow(ctx, spaceID, since, until)
	if err != nil {
		t.Fatalf("SpaceUsageInWindow: %v", err)
	}
	if runCount != 1 {
		t.Errorf("runCount = %d, want 1", runCount)
	}
	if tokens != 0 {
		t.Errorf("tokens = %d, want 0 (NULL token columns fold to zero)", tokens)
	}
}

func TestSpaceUsageInWindowUnknownSpaceIsZero(t *testing.T) {
	s, ctx := newTestStore(t)
	since := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	until := since.Add(time.Hour)

	unknown, err := util.NewPublicID()
	if err != nil {
		t.Fatalf("NewPublicID: %v", err)
	}
	runCount, tokens, err := s.SpaceUsageInWindow(ctx, unknown, since, until)
	if err != nil {
		t.Fatalf("SpaceUsageInWindow on an unknown space should not error: %v", err)
	}
	if runCount != 0 || tokens != 0 {
		t.Errorf("unknown space usage = (%d, %d), want (0, 0)", runCount, tokens)
	}
}
