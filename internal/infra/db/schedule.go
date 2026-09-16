package db

import (
	"context"
	"errors"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreschedule "github.com/icloudbb/buildmax/internal/core/schedule"
	"github.com/icloudbb/buildmax/internal/util"

	"gorm.io/gorm"
)

type scheduleRow struct {
	ID       uint64 `gorm:"primaryKey;autoIncrement"`
	PublicID string `gorm:"column:public_id;type:char(20) CHARACTER SET ascii COLLATE ascii_bin;uniqueIndex:uq_schedule_public_id;not null"`
	// The space index carries created_at: a space's schedule list is ordered by
	// it, so the sort rides the same index the filter uses.
	SpaceID   uint64 `gorm:"column:space_id;not null;index:idx_schedule_space_created,priority:1"`
	AgentID   uint64 `gorm:"column:agent_id;not null;index"`
	CreatedBy uint64 `gorm:"column:created_by;not null"`
	Name      string `gorm:"type:varchar(256)"`
	Input     string `gorm:"type:text;not null"`
	CronExpr  string `gorm:"column:cron_expr;type:varchar(256);not null"`
	Timezone  string `gorm:"type:varchar(64);not null"`
	// The due index is (enabled, next_fire_at): the dispatcher's one query is
	// "enabled rows whose next_fire_at has arrived, oldest first", and the
	// composite index serves both the filter and the order.
	Enabled             bool       `gorm:"not null;index:idx_schedule_due,priority:1"`
	PauseReason         string     `gorm:"column:pause_reason;type:varchar(32);not null;default:''"`
	NextFireAt          time.Time  `gorm:"column:next_fire_at;not null;index:idx_schedule_due,priority:2"`
	LastFireAt          *time.Time `gorm:"column:last_fire_at"`
	LastTaskID          *uint64    `gorm:"column:last_task_id;index"`
	ConsecutiveFailures int        `gorm:"column:consecutive_failures;not null"`
	CreatedAt           time.Time  `gorm:"autoCreateTime;index:idx_schedule_space_created,priority:2"`
	UpdatedAt           time.Time  `gorm:"autoUpdateTime"`
}

func (scheduleRow) TableName() string { return "schedule" }

// scheduleReadRow is the row plus the handles its converted references resolve
// to. A pointer field is one a LEFT JOIN may leave NULL.
type scheduleReadRow struct {
	Row               scheduleRow `gorm:"embedded"`
	SpacePublicID     string      `gorm:"column:space_public_id"`
	AgentPublicID     string      `gorm:"column:agent_public_id"`
	CreatedByPublicID string      `gorm:"column:created_by_public_id"`
	LastTaskPublicID  *string     `gorm:"column:last_task_public_id"`
}

// scheduleSelect is the one place a schedule read's join set is written down, so
// the detail read, the listing, and the due query cannot drift apart. Every join
// is a primary-key lookup, which keeps a listing one query.
func (s *Store) scheduleSelect(ctx context.Context) *gorm.DB {
	return s.db.WithContext(ctx).Model(&scheduleRow{}).
		Select("schedule.*, sp.public_id AS space_public_id, ag.public_id AS agent_public_id, " +
			"cb.public_id AS created_by_public_id, lt.public_id AS last_task_public_id").
		Joins("INNER JOIN space sp ON sp.id = schedule.space_id").
		Joins("INNER JOIN agent ag ON ag.id = schedule.agent_id").
		Joins("INNER JOIN `user` cb ON cb.id = schedule.created_by").
		Joins("LEFT JOIN task lt ON lt.id = schedule.last_task_id")
}

func toSchedule(row *scheduleReadRow) *coreschedule.Schedule {
	if row == nil {
		return nil
	}
	out := &coreschedule.Schedule{
		ID:                  row.Row.PublicID,
		SpaceID:             row.SpacePublicID,
		AgentID:             row.AgentPublicID,
		CreatedBy:           row.CreatedByPublicID,
		Name:                row.Row.Name,
		Input:               row.Row.Input,
		CronExpr:            row.Row.CronExpr,
		Timezone:            row.Row.Timezone,
		Enabled:             row.Row.Enabled,
		PauseReason:         row.Row.PauseReason,
		NextFireAt:          row.Row.NextFireAt,
		LastFireAt:          row.Row.LastFireAt,
		ConsecutiveFailures: row.Row.ConsecutiveFailures,
		CreatedAt:           row.Row.CreatedAt,
		UpdatedAt:           row.Row.UpdatedAt,
	}
	if row.Row.LastTaskID != nil {
		lastTask := derefPublicID(row.LastTaskPublicID)
		out.LastTaskID = &lastTask
	}
	return out
}

func toSchedules(rows []scheduleReadRow) []coreschedule.Schedule {
	out := make([]coreschedule.Schedule, len(rows))
	for i := range rows {
		out[i] = *toSchedule(&rows[i])
	}
	return out
}

// CreateSchedule inserts a new schedule. NextFireAt is supplied by the caller,
// which owns the cron parsing this package deliberately does not.
func (s *Store) CreateSchedule(ctx context.Context, in *coreschedule.CreateInput) (*coreschedule.Schedule, error) {
	if in == nil {
		return nil, errors.New("coreschedule.CreateInput is required")
	}
	row := &scheduleRow{
		Name:       in.Name,
		Input:      in.Input,
		CronExpr:   in.CronExpr,
		Timezone:   in.Timezone,
		Enabled:    in.Enabled,
		NextFireAt: in.NextFireAt.UTC(),
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		spaceKey, err := lookupKey(ctx, tx, "space", in.SpaceID)
		if err != nil {
			return err
		}
		row.SpaceID = spaceKey
		agentKey, err := lookupKey(ctx, tx, "agent", in.AgentID)
		if err != nil {
			return err
		}
		row.AgentID = agentKey
		creator, err := lookupKey(ctx, tx, "user", in.CreatedBy)
		if err != nil {
			return err
		}
		row.CreatedBy = creator
		return createWithPublicID(ctx, tx, "uq_schedule_public_id",
			func(id string) { row.PublicID = id }, row)
	})
	if err != nil {
		return nil, err
	}
	return s.GetSchedule(ctx, row.PublicID)
}

// GetSchedule returns the schedule by id, or (nil, nil) if not found.
func (s *Store) GetSchedule(ctx context.Context, scheduleID string) (*coreschedule.Schedule, error) {
	id, ok := util.CanonicalPublicID(scheduleID)
	if !ok {
		return nil, nil
	}
	var row scheduleReadRow
	err := s.scheduleSelect(ctx).Where("schedule.public_id = ?", id).Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return toSchedule(&row), nil
}

// ListSchedulesBySpace returns a space's schedules newest first. total is the
// count ignoring limit and offset.
func (s *Store) ListSchedulesBySpace(ctx context.Context, spaceID string, limit, offset int) ([]coreschedule.Schedule, int, error) {
	limit, offset = capPage(limit, offset)
	spaceKey, err := lookupKey(ctx, s.db, "space", spaceID)
	if errors.Is(err, apierr.ErrNotFound) {
		return nil, 0, nil
	}
	if err != nil {
		return nil, 0, err
	}
	var total int64
	if err := s.db.WithContext(ctx).Model(&scheduleRow{}).Where("space_id = ?", spaceKey).Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []scheduleReadRow
	err = s.scheduleSelect(ctx).Where("schedule.space_id = ?", spaceKey).
		Order("schedule.created_at DESC").Limit(limit).Offset(offset).Find(&list).Error
	return toSchedules(list), int(total), err
}

// UpdateSchedule writes a schedule's non-nil editable fields and returns the
// updated schedule. It reports ErrNotFound when the id names no row.
func (s *Store) UpdateSchedule(ctx context.Context, in coreschedule.UpdateInput) (*coreschedule.Schedule, error) {
	id, ok := util.CanonicalPublicID(in.ScheduleID)
	if !ok {
		return nil, apierr.ErrNotFound
	}
	updates := map[string]any{}
	if in.Name != nil {
		updates["name"] = *in.Name
	}
	if in.Input != nil {
		updates["input"] = *in.Input
	}
	if in.CronExpr != nil {
		updates["cron_expr"] = *in.CronExpr
	}
	if in.Timezone != nil {
		updates["timezone"] = *in.Timezone
	}
	if in.Enabled != nil {
		updates["enabled"] = *in.Enabled
		// Enabling clears the reason so a re-enabled schedule never reads as paused
		// for a cause that no longer holds. Disabling records the reason the caller
		// gave, or none for a plain disable.
		if *in.Enabled {
			updates["pause_reason"] = ""
		} else if in.PauseReason != nil {
			updates["pause_reason"] = *in.PauseReason
		}
	}
	if in.NextFireAt != nil {
		updates["next_fire_at"] = in.NextFireAt.UTC()
	}
	if len(updates) > 0 {
		res := s.db.WithContext(ctx).Model(&scheduleRow{}).Where("public_id = ?", id).Updates(updates)
		if res.Error != nil {
			return nil, res.Error
		}
	}
	schedule, err := s.GetSchedule(ctx, id)
	if err != nil {
		return nil, err
	}
	if schedule == nil {
		return nil, apierr.ErrNotFound
	}
	return schedule, nil
}

// DeleteSchedule removes a schedule. Tasks it already created are independent
// history and are not touched. It reports ErrNotFound when the id names no row.
func (s *Store) DeleteSchedule(ctx context.Context, scheduleID string) error {
	id, ok := util.CanonicalPublicID(scheduleID)
	if !ok {
		return apierr.ErrNotFound
	}
	res := s.db.WithContext(ctx).Where("public_id = ?", id).Delete(&scheduleRow{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return apierr.ErrNotFound
	}
	return nil
}

// DueSchedules returns enabled schedules whose next_fire_at is at or before now,
// oldest due time first, capped at limit.
func (s *Store) DueSchedules(ctx context.Context, now time.Time, limit int) ([]coreschedule.Schedule, error) {
	if limit <= 0 {
		limit = 100
	}
	var list []scheduleReadRow
	err := s.scheduleSelect(ctx).
		Where("schedule.enabled = ? AND schedule.next_fire_at <= ?", true, now.UTC()).
		Order("schedule.next_fire_at ASC").Limit(limit).Find(&list).Error
	return toSchedules(list), err
}

// ClaimSchedule advances next_fire_at only when the schedule is still enabled and
// its next_fire_at still equals ExpectedNextFireAt. The conditional update rests
// on the server serializing two writes to one row, so exactly one of several
// replicas racing to fire the same due time wins.
func (s *Store) ClaimSchedule(ctx context.Context, in coreschedule.ClaimInput) (bool, error) {
	id, ok := util.CanonicalPublicID(in.ScheduleID)
	if !ok {
		return false, nil
	}
	res := s.db.WithContext(ctx).Model(&scheduleRow{}).
		Where("public_id = ? AND enabled = ? AND next_fire_at = ?", id, true, in.ExpectedNextFireAt.UTC()).
		Update("next_fire_at", in.NewNextFireAt.UTC())
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// RecordFire stores a firing's outcome. A success resets the consecutive-failure
// counter; a failure increments it. last_task_id is set only when the firing
// produced a Task.
func (s *Store) RecordFire(ctx context.Context, in coreschedule.RecordFireInput) error {
	id, ok := util.CanonicalPublicID(in.ScheduleID)
	if !ok {
		return apierr.ErrNotFound
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{"last_fire_at": in.FiredAt.UTC()}
		if in.Failed {
			updates["consecutive_failures"] = gorm.Expr("consecutive_failures + 1")
		} else {
			updates["consecutive_failures"] = 0
		}
		if in.TaskID != nil {
			taskKey, err := lookupKey(ctx, tx, "task", *in.TaskID)
			if err != nil {
				return err
			}
			updates["last_task_id"] = taskKey
		}
		res := tx.WithContext(ctx).Model(&scheduleRow{}).Where("public_id = ?", id).Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return apierr.ErrNotFound
		}
		return nil
	})
}
