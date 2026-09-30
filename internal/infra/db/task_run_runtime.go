package db

import (
	"context"
	"database/sql"
	"time"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"gorm.io/gorm"
)

// RuntimeSummary answers "is work moving" for the whole deployment from run rows
// alone, so two replicas cannot disagree. It reads statuses, timestamps, and
// failure classes — never input, output, or error text.
func (s *Store) RuntimeSummary(ctx context.Context, staleBefore, failedSince time.Time) (coretask.RuntimeSummary, error) {
	runs := func() *gorm.DB { return s.db.WithContext(ctx).Model(&taskRunRow{}) }
	var out coretask.RuntimeSummary

	var oldest sql.NullTime
	if err := runs().
		Select("MIN(created_at)").
		Where("status = ?", string(coretask.RunStatusPending)).
		Row().Scan(&oldest); err != nil {
		return out, err
	}
	out.OldestPendingAt = nullTimePtr(oldest)

	oldest = sql.NullTime{}
	if err := runs().
		Select("MIN(created_at)").
		Where("status = ? AND started_at IS NULL", string(coretask.RunStatusScheduled)).
		Row().Scan(&oldest); err != nil {
		return out, err
	}
	out.OldestUnstartedAt = nullTimePtr(oldest)

	// The reaper's own selection: a NULL last_seen_at is no signal, not silence.
	var stale int64
	if err := runs().
		Where("status = ? AND last_seen_at IS NOT NULL AND last_seen_at <= ?", string(coretask.RunStatusRunning), staleBefore).
		Count(&stale).Error; err != nil {
		return out, err
	}
	out.StaleRunning = int(stale)

	failures, err := s.failuresByClass(ctx, failedSince, nil)
	if err != nil {
		return out, err
	}
	out.FailuresByClass = failures[0]

	waiting, err := s.pendingWorkflowRequests(ctx, nil)
	if err != nil {
		return out, err
	}
	workflowFailures, err := s.workflowFailuresByClass(ctx, failedSince, nil)
	if err != nil {
		return out, err
	}
	out.WorkflowRuntime = waiting[0]
	out.WorkflowFailuresByClass = workflowFailures[0]
	return out, nil
}

// ListSpaceRunActivity pages through the Spaces with an active run, a pending
// Workflow request, or a TaskRun or Workflow run failure since failedSince —
// team and personal alike. The Space that has waited longest leads: its key is
// the older of its oldest active run and its oldest pending request.
func (s *Store) ListSpaceRunActivity(ctx context.Context, failedSince time.Time, limit, offset int) ([]coretask.SpaceRunActivity, int, error) {
	limit, offset = clampPage(limit, offset)
	active := coretask.ActiveRunStatuses()
	// One row per fact that puts a Space on the list, with the time it has
	// waited since; a failure has none.
	facts := s.db.Raw("SELECT t.space_id AS space_row_id, "+
		"CASE WHEN task_run.status IN ? THEN task_run.created_at END AS waiting_since "+
		"FROM task_run INNER JOIN task t ON t.id = task_run.task_id "+
		"WHERE task_run.status IN ? OR (task_run.failure_class <> '' AND task_run.ended_at >= ?) "+
		"UNION ALL "+
		"SELECT w.space_id, r.created_at FROM workflow_request r "+
		"INNER JOIN workflow_run wr ON wr.id = r.workflow_run_id INNER JOIN workflow w ON w.id = wr.workflow_id "+
		"WHERE r.status = ? "+
		"UNION ALL "+
		"SELECT w.space_id, NULL FROM workflow_run wr INNER JOIN workflow w ON w.id = wr.workflow_id "+
		"WHERE wr.failure_class <> '' AND wr.ended_at >= ?",
		active, active, failedSince, coreworkflow.RequestStatusPending, failedSince)

	var total int64
	if err := s.db.WithContext(ctx).Table("(?) AS facts", facts).
		Distinct("facts.space_row_id").Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []coretask.SpaceRunActivity{}, 0, nil
	}

	var page []struct {
		SpaceRowID    uint64 `gorm:"column:space_row_id"`
		SpacePublicID string `gorm:"column:space_public_id"`
	}
	err := s.db.WithContext(ctx).Table("(?) AS facts", facts).
		Joins("INNER JOIN space sp ON sp.id = facts.space_row_id").
		Select("facts.space_row_id, sp.public_id AS space_public_id, MIN(facts.waiting_since) AS space_waiting_since").
		Group("facts.space_row_id, sp.public_id").
		Order("space_waiting_since IS NULL, space_waiting_since ASC, facts.space_row_id ASC").
		Limit(limit).Offset(offset).
		Scan(&page).Error
	if err != nil {
		return nil, 0, err
	}

	spaceIDs := make([]uint64, 0, len(page))
	for _, row := range page {
		spaceIDs = append(spaceIDs, row.SpaceRowID)
	}
	activeBySpace, err := s.activeRunsBySpace(ctx, spaceIDs)
	if err != nil {
		return nil, 0, err
	}
	failures, err := s.failuresByClass(ctx, failedSince, spaceIDs)
	if err != nil {
		return nil, 0, err
	}
	waiting, err := s.pendingWorkflowRequests(ctx, spaceIDs)
	if err != nil {
		return nil, 0, err
	}
	workflowFailures, err := s.workflowFailuresByClass(ctx, failedSince, spaceIDs)
	if err != nil {
		return nil, 0, err
	}
	oldestWaiting, err := s.oldestWaitingWorkflowRuns(ctx, spaceIDs)
	if err != nil {
		return nil, 0, err
	}
	latestFailed, err := s.latestFailedWorkflowRuns(ctx, failedSince, spaceIDs)
	if err != nil {
		return nil, 0, err
	}

	out := make([]coretask.SpaceRunActivity, 0, len(page))
	for _, row := range page {
		activity := coretask.SpaceRunActivity{
			SpaceID:                    row.SpacePublicID,
			Active:                     map[string]int{},
			FailuresByClass:            failures[row.SpaceRowID],
			WorkflowRuntime:            waiting[row.SpaceRowID],
			OldestWaitingWorkflowRunID: oldestWaiting[row.SpaceRowID],
			LatestFailedWorkflowRunID:  latestFailed[row.SpaceRowID],
		}
		if a, ok := activeBySpace[row.SpaceRowID]; ok {
			activity.Active = a.counts
			activity.OldestActiveAt = a.oldest
		}
		if activity.FailuresByClass == nil {
			activity.FailuresByClass = map[string]int{}
		}
		if activity.WaitingRequests == nil {
			activity.WaitingRequests = map[string]int{}
		}
		activity.WorkflowFailuresByClass = workflowFailures[row.SpaceRowID]
		if activity.WorkflowFailuresByClass == nil {
			activity.WorkflowFailuresByClass = map[string]int{}
		}
		out = append(out, activity)
	}
	return out, int(total), nil
}

type spaceActiveRuns struct {
	counts map[string]int
	oldest *time.Time
}

// activeRunsBySpace counts each Space's active runs by status and finds when
// its oldest one was created.
func (s *Store) activeRunsBySpace(ctx context.Context, spaceRowIDs []uint64) (map[uint64]spaceActiveRuns, error) {
	out := map[uint64]spaceActiveRuns{}
	if len(spaceRowIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		SpaceRowID     uint64       `gorm:"column:space_row_id"`
		Status         string       `gorm:"column:status"`
		N              int          `gorm:"column:n"`
		OldestActiveAt sql.NullTime `gorm:"column:oldest_active_at"`
	}
	err := s.db.WithContext(ctx).Table("task_run").
		Joins("INNER JOIN task t ON t.id = task_run.task_id").
		Where("task_run.status IN ? AND t.space_id IN ?", coretask.ActiveRunStatuses(), spaceRowIDs).
		Select("t.space_id AS space_row_id, task_run.status, COUNT(*) AS n, MIN(task_run.created_at) AS oldest_active_at").
		Group("t.space_id, task_run.status").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		a, ok := out[row.SpaceRowID]
		if !ok {
			a = spaceActiveRuns{counts: map[string]int{}}
		}
		a.counts[row.Status] = row.N
		a.oldest = earlierTime(a.oldest, nullTimePtr(row.OldestActiveAt))
		out[row.SpaceRowID] = a
	}
	return out, nil
}

// failuresByClass counts failures since the cutoff by class. With no Space ids
// it counts the whole deployment under key 0; otherwise it groups by Space row.
func (s *Store) failuresByClass(ctx context.Context, since time.Time, spaceRowIDs []uint64) (map[uint64]map[string]int, error) {
	q := s.db.WithContext(ctx).Table("task_run").
		Where("task_run.failure_class <> '' AND task_run.ended_at >= ?", since)
	var rows []struct {
		SpaceRowID   uint64 `gorm:"column:space_row_id"`
		FailureClass string `gorm:"column:failure_class"`
		N            int    `gorm:"column:n"`
	}
	if spaceRowIDs == nil {
		q = q.Select("0 AS space_row_id, task_run.failure_class, COUNT(*) AS n").Group("task_run.failure_class")
	} else {
		if len(spaceRowIDs) == 0 {
			return map[uint64]map[string]int{}, nil
		}
		q = q.Joins("INNER JOIN task t ON t.id = task_run.task_id").
			Where("t.space_id IN ?", spaceRowIDs).
			Select("t.space_id AS space_row_id, task_run.failure_class, COUNT(*) AS n").
			Group("t.space_id, task_run.failure_class")
	}
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[uint64]map[string]int{0: {}}
	for _, row := range rows {
		if out[row.SpaceRowID] == nil {
			out[row.SpaceRowID] = map[string]int{}
		}
		out[row.SpaceRowID][row.FailureClass] = row.N
	}
	return out, nil
}

// earlierTime returns the earlier of two optional times.
func earlierTime(a, b *time.Time) *time.Time {
	if a == nil {
		return b
	}
	if b == nil || !b.Before(*a) {
		return a
	}
	return b
}

func nullTimePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time.UTC()
	return &v
}
