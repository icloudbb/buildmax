package mock

import (
	"context"
	"fmt"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreschedule "github.com/icloudbb/buildmax/internal/core/schedule"
)

// MockScheduleStore is an in-memory coreschedule.Store for tests.
type MockScheduleStore struct {
	Schedules []coreschedule.Schedule
}

func (m *MockScheduleStore) CreateSchedule(_ context.Context, in *coreschedule.CreateInput) (*coreschedule.Schedule, error) {
	now := time.Now().UTC()
	s := coreschedule.Schedule{
		ID:           fmt.Sprintf("sch_%d", len(m.Schedules)+1),
		SpaceID:      in.SpaceID,
		ExecutorKind: in.ExecutorKind,
		ExecutorID:   in.ExecutorID,
		CreatedBy:    in.CreatedBy,
		Name:         in.Name,
		Input:        in.Input,
		CronExpr:     in.CronExpr,
		Timezone:     in.Timezone,
		Enabled:      in.Enabled,
		NextFireAt:   in.NextFireAt,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	m.Schedules = append(m.Schedules, s)
	return &m.Schedules[len(m.Schedules)-1], nil
}

func (m *MockScheduleStore) GetSchedule(_ context.Context, scheduleID string) (*coreschedule.Schedule, error) {
	for i := range m.Schedules {
		if m.Schedules[i].ID == scheduleID {
			return &m.Schedules[i], nil
		}
	}
	return nil, nil
}

func (m *MockScheduleStore) ListSchedulesBySpace(_ context.Context, spaceID string, limit, offset int) ([]coreschedule.Schedule, int, error) {
	var all []coreschedule.Schedule
	for i := len(m.Schedules) - 1; i >= 0; i-- {
		if m.Schedules[i].SpaceID == spaceID {
			all = append(all, m.Schedules[i])
		}
	}
	total := len(all)
	if offset >= total {
		return nil, total, nil
	}
	end := total
	if limit > 0 && offset+limit < end {
		end = offset + limit
	}
	return all[offset:end], total, nil
}

func (m *MockScheduleStore) UpdateSchedule(_ context.Context, in coreschedule.UpdateInput) (*coreschedule.Schedule, error) {
	for i := range m.Schedules {
		if m.Schedules[i].ID != in.ScheduleID {
			continue
		}
		if in.Name != nil {
			m.Schedules[i].Name = *in.Name
		}
		if in.Input != nil {
			m.Schedules[i].Input = *in.Input
		}
		if in.CronExpr != nil {
			m.Schedules[i].CronExpr = *in.CronExpr
		}
		if in.Timezone != nil {
			m.Schedules[i].Timezone = *in.Timezone
		}
		if in.Enabled != nil {
			m.Schedules[i].Enabled = *in.Enabled
			if *in.Enabled {
				m.Schedules[i].PauseReason = ""
			} else if in.PauseReason != nil {
				m.Schedules[i].PauseReason = *in.PauseReason
			}
		}
		if in.NextFireAt != nil {
			m.Schedules[i].NextFireAt = *in.NextFireAt
		}
		m.Schedules[i].UpdatedAt = time.Now().UTC()
		return &m.Schedules[i], nil
	}
	return nil, apierr.ErrNotFound
}

func (m *MockScheduleStore) DeleteSchedule(_ context.Context, scheduleID string) error {
	for i := range m.Schedules {
		if m.Schedules[i].ID == scheduleID {
			m.Schedules = append(m.Schedules[:i], m.Schedules[i+1:]...)
			return nil
		}
	}
	return apierr.ErrNotFound
}

func (m *MockScheduleStore) DueSchedules(_ context.Context, now time.Time, limit int) ([]coreschedule.Schedule, error) {
	var out []coreschedule.Schedule
	for i := range m.Schedules {
		if m.Schedules[i].Enabled && !m.Schedules[i].NextFireAt.After(now) {
			out = append(out, m.Schedules[i])
			if limit > 0 && len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (m *MockScheduleStore) ListEnabledSchedulesByCreator(_ context.Context, createdBy string) ([]coreschedule.Schedule, error) {
	var out []coreschedule.Schedule
	for i := range m.Schedules {
		if m.Schedules[i].Enabled && m.Schedules[i].CreatedBy == createdBy {
			out = append(out, m.Schedules[i])
		}
	}
	return out, nil
}

func (m *MockScheduleStore) ClaimSchedule(_ context.Context, in coreschedule.ClaimInput) (bool, error) {
	for i := range m.Schedules {
		if m.Schedules[i].ID != in.ScheduleID {
			continue
		}
		if !m.Schedules[i].Enabled || !m.Schedules[i].NextFireAt.Equal(in.ExpectedNextFireAt) {
			return false, nil
		}
		m.Schedules[i].NextFireAt = in.NewNextFireAt
		return true, nil
	}
	return false, nil
}

func (m *MockScheduleStore) RecordFire(_ context.Context, in coreschedule.RecordFireInput) error {
	for i := range m.Schedules {
		if m.Schedules[i].ID != in.ScheduleID {
			continue
		}
		firedAt := in.FiredAt
		m.Schedules[i].LastFireAt = &firedAt
		if in.FireRef != nil {
			m.Schedules[i].LastFireRef = in.FireRef
		}
		if in.Failed {
			m.Schedules[i].ConsecutiveFailures++
		} else {
			m.Schedules[i].ConsecutiveFailures = 0
		}
		return nil
	}
	return apierr.ErrNotFound
}
