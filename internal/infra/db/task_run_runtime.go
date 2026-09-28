package db

import (
	"context"
	"database/sql"
	"time"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
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
	return out, nil
}

// ListSpaceRunActivity pages through the Spaces with an active run or a failure
// since failedSince — team and personal alike — oldest active run first, so the
// Space that has waited longest leads.
func (s *Store) ListSpaceRunActivity(ctx context.Context, failedSince time.Time, limit, offset int) ([]coretask.SpaceRunActivity, int, error) {
	limit, offset = clampPage(limit, offset)
	active := coretask.ActiveRunStatuses()
	scope := func() *gorm.DB {
		return s.db.WithContext(ctx).Table("task_run").
			Joins("INNER JOIN task t ON t.id = task_run.task_id").
			Where("task_run.status IN ? OR (task_run.failure_class <> '' AND task_run.ended_at >= ?)", active, failedSince)
	}

	var total int64
	if err := scope().Distinct("t.space_id").Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []coretask.SpaceRunActivity{}, 0, nil
	}

	var rows []struct {
		SpaceRowID     uint64       `gorm:"column:space_row_id"`
		SpacePublicID  string       `gorm:"column:space_public_id"`
		Pending        int          `gorm:"column:pending"`
		Scheduled      int          `gorm:"column:scheduled"`
		Running        int          `gorm:"column:running"`
		OldestActiveAt sql.NullTime `gorm:"column:oldest_active_at"`
	}
	err := scope().
		Joins("INNER JOIN space sp ON sp.id = t.space_id").
		Select("t.space_id AS space_row_id, sp.public_id AS space_public_id, "+
			"SUM(CASE WHEN task_run.status = ? THEN 1 ELSE 0 END) AS pending, "+
			"SUM(CASE WHEN task_run.status = ? THEN 1 ELSE 0 END) AS scheduled, "+
			"SUM(CASE WHEN task_run.status = ? THEN 1 ELSE 0 END) AS running, "+
			"MIN(CASE WHEN task_run.status IN ? THEN task_run.created_at END) AS oldest_active_at",
			string(coretask.RunStatusPending), string(coretask.RunStatusScheduled), string(coretask.RunStatusRunning), active).
		Group("t.space_id, sp.public_id").
		Order("oldest_active_at IS NULL, oldest_active_at ASC, t.space_id ASC").
		Limit(limit).Offset(offset).
		Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}

	spaceIDs := make([]uint64, 0, len(rows))
	for _, row := range rows {
		spaceIDs = append(spaceIDs, row.SpaceRowID)
	}
	failures, err := s.failuresByClass(ctx, failedSince, spaceIDs)
	if err != nil {
		return nil, 0, err
	}

	out := make([]coretask.SpaceRunActivity, 0, len(rows))
	for _, row := range rows {
		activity := coretask.SpaceRunActivity{
			SpaceID:         row.SpacePublicID,
			Active:          map[string]int{},
			OldestActiveAt:  nullTimePtr(row.OldestActiveAt),
			FailuresByClass: failures[row.SpaceRowID],
		}
		if activity.FailuresByClass == nil {
			activity.FailuresByClass = map[string]int{}
		}
		for status, n := range map[coretask.RunStatus]int{
			coretask.RunStatusPending: row.Pending, coretask.RunStatusScheduled: row.Scheduled, coretask.RunStatusRunning: row.Running,
		} {
			if n > 0 {
				activity.Active[string(status)] = n
			}
		}
		out = append(out, activity)
	}
	return out, int(total), nil
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

func nullTimePtr(t sql.NullTime) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time.UTC()
	return &v
}
