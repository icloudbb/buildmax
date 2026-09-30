package db

import (
	"context"
	"errors"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
)

// SpaceUsageInWindow returns run count and total tokens for the space in [since, until].
// Runs: task_runs where the task's space = spaceID and run created_at in window.
// Tokens: sum of run prompt+completion tokens for those runs, plus task title
// tokens for tasks created in the space in window, plus the prompt+completion
// tokens of the space's conversation-turn calls accepted in window.
//
// The space's handle is resolved once, at the top. Everything after it is a
// numeric comparison: this is the hottest aggregation in the deployment and it
// no longer touches a string.
func (s *Store) SpaceUsageInWindow(ctx context.Context, spaceID string, since, until time.Time) (runCount, totalTokens int, err error) {
	spaceKey, err := lookupKey(ctx, s.db, "space", spaceID)
	if errors.Is(err, apierr.ErrNotFound) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}

	var runCnt int64
	err = s.db.WithContext(ctx).Model(&taskRunRow{}).
		Joins("INNER JOIN task ON task.id = task_run.task_id AND task.space_id = ?", spaceKey).
		Where("task_run.created_at >= ? AND task_run.created_at <= ?", since, until).
		Count(&runCnt).Error
	if err != nil {
		return 0, 0, err
	}
	runCount = int(runCnt)

	var runTokens int
	err = s.db.WithContext(ctx).Model(&taskRunRow{}).
		Select("COALESCE(SUM(COALESCE(prompt_tokens, 0) + COALESCE(completion_tokens, 0)), 0)").
		Joins("INNER JOIN task ON task.id = task_run.task_id AND task.space_id = ?", spaceKey).
		Where("task_run.created_at >= ? AND task_run.created_at <= ?", since, until).
		Scan(&runTokens).Error
	if err != nil {
		return runCount, 0, err
	}

	// Title generation is billed to the space too: it is a model call the space's
	// work caused, even though no run records it.
	var titleTokens int
	err = s.db.WithContext(ctx).Model(&taskRow{}).
		Select("COALESCE(SUM(title_prompt_tokens + title_completion_tokens), 0)").
		Where("space_id = ? AND created_at >= ? AND created_at <= ?", spaceKey, since, until).
		Scan(&titleTokens).Error
	if err != nil {
		return runCount, runTokens, err
	}

	// A conversation turn is the space's work too, and no run records it: the
	// ledger row is the only record of what the turn spent. A run's own calls
	// are left to the run's totals above, which already count them, so the
	// task_run_id bound is what keeps one worker call from counting twice.
	var chatTokens int
	err = s.db.WithContext(ctx).Model(&llmCallRow{}).
		Select("COALESCE(SUM(COALESCE(llm_call.prompt_tokens, 0) + COALESCE(llm_call.completion_tokens, 0)), 0)").
		Joins("INNER JOIN conversation ON conversation.id = llm_call.conversation_id AND conversation.space_id = ?", spaceKey).
		Where("llm_call.task_run_id IS NULL AND llm_call.accepted_at >= ? AND llm_call.accepted_at <= ?", since, until).
		Scan(&chatTokens).Error
	if err != nil {
		return runCount, runTokens + titleTokens, err
	}

	return runCount, runTokens + titleTokens + chatTokens, nil
}
