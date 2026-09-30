package db

import (
	"context"
	"database/sql"
	"time"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
)

// The administration runtime view reads Workflow rows for the waits and
// failures TaskRun rows cannot show: a run waiting on a person holds no
// TaskRun, and a run can fail while every node's TaskRun succeeded. Like the
// TaskRun reads, these select statuses, kinds, timestamps, classes, and ids —
// never a request's prompt, questions, or answer.

// pendingWorkflowRequests counts pending requests by kind with their oldest
// opening and earliest expiry. With no Space ids it answers for the whole
// deployment under key 0; otherwise it groups by Space row.
func (s *Store) pendingWorkflowRequests(ctx context.Context, spaceRowIDs []uint64) (map[uint64]coretask.WorkflowRuntime, error) {
	q := s.db.WithContext(ctx).Table("workflow_request r").
		Where("r.status = ?", coreworkflow.RequestStatusPending)
	if spaceRowIDs == nil {
		q = q.Select("0 AS space_row_id, r.kind, COUNT(*) AS n, MIN(r.created_at) AS oldest_at, MIN(r.expires_at) AS next_expiry_at").
			Group("r.kind")
	} else {
		if len(spaceRowIDs) == 0 {
			return map[uint64]coretask.WorkflowRuntime{}, nil
		}
		q = q.Joins("INNER JOIN workflow_run wr ON wr.id = r.workflow_run_id").
			Joins("INNER JOIN workflow w ON w.id = wr.workflow_id").
			Where("w.space_id IN ?", spaceRowIDs).
			Select("w.space_id AS space_row_id, r.kind, COUNT(*) AS n, MIN(r.created_at) AS oldest_at, MIN(r.expires_at) AS next_expiry_at").
			Group("w.space_id, r.kind")
	}
	var rows []struct {
		SpaceRowID   uint64       `gorm:"column:space_row_id"`
		Kind         string       `gorm:"column:kind"`
		N            int          `gorm:"column:n"`
		OldestAt     sql.NullTime `gorm:"column:oldest_at"`
		NextExpiryAt sql.NullTime `gorm:"column:next_expiry_at"`
	}
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[uint64]coretask.WorkflowRuntime{0: {WaitingRequests: map[string]int{}}}
	for _, row := range rows {
		w, ok := out[row.SpaceRowID]
		if !ok || w.WaitingRequests == nil {
			w.WaitingRequests = map[string]int{}
		}
		w.WaitingRequests[row.Kind] = row.N
		w.OldestWaitingRequestAt = earlierTime(w.OldestWaitingRequestAt, nullTimePtr(row.OldestAt))
		w.NextRequestExpiryAt = earlierTime(w.NextRequestExpiryAt, nullTimePtr(row.NextExpiryAt))
		out[row.SpaceRowID] = w
	}
	return out, nil
}

// workflowFailuresByClass counts Workflow runs that failed since the cutoff by
// class, deployment-wide under key 0 or grouped by Space row. A run records its
// class when it starts failing and its ended_at when the drain finishes, so a
// run still draining is counted once it has failed.
func (s *Store) workflowFailuresByClass(ctx context.Context, since time.Time, spaceRowIDs []uint64) (map[uint64]map[string]int, error) {
	q := s.db.WithContext(ctx).Table("workflow_run wr").
		Where("wr.failure_class <> '' AND wr.ended_at >= ?", since)
	if spaceRowIDs == nil {
		q = q.Select("0 AS space_row_id, wr.failure_class, COUNT(*) AS n").Group("wr.failure_class")
	} else {
		if len(spaceRowIDs) == 0 {
			return map[uint64]map[string]int{}, nil
		}
		q = q.Joins("INNER JOIN workflow w ON w.id = wr.workflow_id").
			Where("w.space_id IN ?", spaceRowIDs).
			Select("w.space_id AS space_row_id, wr.failure_class, COUNT(*) AS n").
			Group("w.space_id, wr.failure_class")
	}
	var rows []struct {
		SpaceRowID   uint64 `gorm:"column:space_row_id"`
		FailureClass string `gorm:"column:failure_class"`
		N            int    `gorm:"column:n"`
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

// oldestWaitingWorkflowRuns names, per Space, the Workflow run whose pending
// request has waited longest.
func (s *Store) oldestWaitingWorkflowRuns(ctx context.Context, spaceRowIDs []uint64) (map[uint64]string, error) {
	return s.firstWorkflowRunPerSpace(ctx,
		"SELECT w.space_id AS space_row_id, wr.public_id AS run_public_id, "+
			"ROW_NUMBER() OVER (PARTITION BY w.space_id ORDER BY r.created_at ASC, r.id ASC) AS rn "+
			"FROM workflow_request r INNER JOIN workflow_run wr ON wr.id = r.workflow_run_id "+
			"INNER JOIN workflow w ON w.id = wr.workflow_id "+
			"WHERE r.status = ? AND w.space_id IN ?",
		spaceRowIDs, coreworkflow.RequestStatusPending, spaceRowIDs)
}

// latestFailedWorkflowRuns names, per Space, the Workflow run that failed most
// recently since the cutoff.
func (s *Store) latestFailedWorkflowRuns(ctx context.Context, since time.Time, spaceRowIDs []uint64) (map[uint64]string, error) {
	return s.firstWorkflowRunPerSpace(ctx,
		"SELECT w.space_id AS space_row_id, wr.public_id AS run_public_id, "+
			"ROW_NUMBER() OVER (PARTITION BY w.space_id ORDER BY wr.ended_at DESC, wr.id DESC) AS rn "+
			"FROM workflow_run wr INNER JOIN workflow w ON w.id = wr.workflow_id "+
			"WHERE wr.failure_class <> '' AND wr.ended_at >= ? AND w.space_id IN ?",
		spaceRowIDs, since, spaceRowIDs)
}

// firstWorkflowRunPerSpace keeps the first-ranked run of each Space from a
// ranked query, so the result is one row per Space however many match.
func (s *Store) firstWorkflowRunPerSpace(ctx context.Context, ranked string, spaceRowIDs []uint64, args ...any) (map[uint64]string, error) {
	out := map[uint64]string{}
	if len(spaceRowIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		SpaceRowID  uint64 `gorm:"column:space_row_id"`
		RunPublicID string `gorm:"column:run_public_id"`
	}
	if err := s.db.WithContext(ctx).Table("(?) AS ranked", s.db.Raw(ranked, args...)).
		Select("ranked.space_row_id, ranked.run_public_id").
		Where("ranked.rn = 1").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.SpaceRowID] = row.RunPublicID
	}
	return out, nil
}
