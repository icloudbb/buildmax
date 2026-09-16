package schedule

import (
	"context"
	"time"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreschedule "github.com/icloudbb/buildmax/internal/core/schedule"
)

var (
	ErrNotConfigured    = apierr.New(apierr.KindNotConfigured, "schedules not configured")
	ErrInputRequired    = apierr.New(apierr.KindInvalid, "input required")
	ErrAgentRequired    = apierr.New(apierr.KindInvalid, "agent_id required")
	ErrCronRequired     = apierr.New(apierr.KindInvalid, "cron_expr required")
	ErrTimezoneRequired = apierr.New(apierr.KindInvalid, "timezone required")
	ErrAgentNotFound    = apierr.New(apierr.KindInvalid, "agent not found in this space")
	ErrScheduleNotFound = apierr.New(apierr.KindNotFound, "schedule not found")
)

// Service owns the application logic for recurring schedules: input and cron
// validation, computing each schedule's next fire, and confirming a schedule
// belongs to the space acting on it. Space membership is enforced above it by
// the HTTP guard; this layer enforces that the schedule and its agent are in the
// stated space, so an id from another space is reported as not found.
type Service struct {
	Schedules coreschedule.Store
	Agents    agentdef.Store
	// Now is the clock used to compute the first fire. Nil means the wall clock.
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

// CreateCmd creates a schedule. The schedule starts enabled with its first fire
// computed from the current time.
type CreateCmd struct {
	SpaceID  string
	UserID   string
	AgentID  string
	Name     string
	Input    string
	CronExpr string
	Timezone string
}

func (s *Service) Create(ctx context.Context, cmd CreateCmd) (*coreschedule.Schedule, error) {
	if s.Schedules == nil {
		return nil, ErrNotConfigured
	}
	if cmd.Input == "" {
		return nil, ErrInputRequired
	}
	if cmd.AgentID == "" {
		return nil, ErrAgentRequired
	}
	if cmd.CronExpr == "" {
		return nil, ErrCronRequired
	}
	if cmd.Timezone == "" {
		return nil, ErrTimezoneRequired
	}
	if err := Validate(cmd.CronExpr, cmd.Timezone); err != nil {
		return nil, invalid(err)
	}
	if err := s.requireAgentInSpace(ctx, cmd.AgentID, cmd.SpaceID); err != nil {
		return nil, err
	}
	next, err := Next(cmd.CronExpr, cmd.Timezone, s.now())
	if err != nil {
		return nil, invalid(err)
	}
	return s.Schedules.CreateSchedule(ctx, &coreschedule.CreateInput{
		SpaceID:    cmd.SpaceID,
		AgentID:    cmd.AgentID,
		CreatedBy:  cmd.UserID,
		Name:       cmd.Name,
		Input:      cmd.Input,
		CronExpr:   cmd.CronExpr,
		Timezone:   cmd.Timezone,
		Enabled:    true,
		NextFireAt: next,
	})
}

// Get returns a schedule only when it belongs to spaceID; otherwise it reports
// not found, so a schedule id from another space is not an existence oracle.
func (s *Service) Get(ctx context.Context, spaceID, scheduleID string) (*coreschedule.Schedule, error) {
	if s.Schedules == nil {
		return nil, ErrNotConfigured
	}
	sched, err := s.Schedules.GetSchedule(ctx, scheduleID)
	if err != nil {
		return nil, err
	}
	if sched == nil || sched.SpaceID != spaceID {
		return nil, ErrScheduleNotFound
	}
	return sched, nil
}

func (s *Service) List(ctx context.Context, spaceID string, limit, offset int) ([]coreschedule.Schedule, int, error) {
	if s.Schedules == nil {
		return nil, 0, ErrNotConfigured
	}
	return s.Schedules.ListSchedulesBySpace(ctx, spaceID, limit, offset)
}

// UpdateCmd changes a schedule's editable fields. Only non-nil fields change.
// When the cron expression or timezone changes, the next fire is recomputed from
// the current time so the stored due time stays consistent with the new rule.
type UpdateCmd struct {
	ScheduleID string
	SpaceID    string
	Name       *string
	Input      *string
	CronExpr   *string
	Timezone   *string
	Enabled    *bool
}

func (s *Service) Update(ctx context.Context, cmd UpdateCmd) (*coreschedule.Schedule, error) {
	existing, err := s.Get(ctx, cmd.SpaceID, cmd.ScheduleID)
	if err != nil {
		return nil, err
	}
	if cmd.Input != nil && *cmd.Input == "" {
		return nil, ErrInputRequired
	}
	in := coreschedule.UpdateInput{
		ScheduleID: cmd.ScheduleID,
		Name:       cmd.Name,
		Input:      cmd.Input,
		CronExpr:   cmd.CronExpr,
		Timezone:   cmd.Timezone,
		Enabled:    cmd.Enabled,
	}
	// A person disabling their own schedule is a manual pause. Enabling clears the
	// reason in the store, so it is only set here when disabling.
	if cmd.Enabled != nil && !*cmd.Enabled {
		manual := coreschedule.PauseReasonManual
		in.PauseReason = &manual
	}
	if cmd.CronExpr != nil || cmd.Timezone != nil {
		cronExpr := existing.CronExpr
		if cmd.CronExpr != nil {
			cronExpr = *cmd.CronExpr
		}
		timezone := existing.Timezone
		if cmd.Timezone != nil {
			timezone = *cmd.Timezone
		}
		if err := Validate(cronExpr, timezone); err != nil {
			return nil, invalid(err)
		}
		next, err := Next(cronExpr, timezone, s.now())
		if err != nil {
			return nil, invalid(err)
		}
		in.NextFireAt = &next
	}
	return s.Schedules.UpdateSchedule(ctx, in)
}

// Delete removes a schedule after confirming it belongs to spaceID. Tasks it
// already created are independent history and are untouched.
func (s *Service) Delete(ctx context.Context, spaceID, scheduleID string) error {
	if _, err := s.Get(ctx, spaceID, scheduleID); err != nil {
		return err
	}
	return s.Schedules.DeleteSchedule(ctx, scheduleID)
}

func (s *Service) requireAgentInSpace(ctx context.Context, agentID, spaceID string) error {
	if s.Agents == nil {
		return ErrNotConfigured
	}
	agent, err := s.Agents.GetAgent(ctx, agentID)
	if err != nil {
		return err
	}
	if agent == nil || agent.SpaceID != spaceID {
		return ErrAgentNotFound
	}
	return nil
}

// invalid wraps a validation error from the cron helpers as a KindInvalid apierr
// so the handler maps it to 400 rather than 500.
func invalid(err error) error {
	return apierr.New(apierr.KindInvalid, err.Error())
}
