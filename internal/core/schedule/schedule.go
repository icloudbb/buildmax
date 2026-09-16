// Package schedule holds the domain for a time trigger that runs an Agent on a
// recurring wall-clock schedule. A Schedule is Space-owned; each firing admits
// one ordinary Task through the existing Task application service, tagged with a
// schedule trigger source. See docs/design/scheduled-agent-execution.md.
//
// This package is pure domain: it does not parse cron expressions or load
// timezones. The caller that owns those (the dispatcher/service) computes each
// NextFireAt and hands it in, so no scheduling library reaches core.
package schedule

import (
	"context"
	"time"
)

// Pause reasons record why a schedule is disabled, so an operator sees whether
// they paused it or the system did. Empty on an enabled schedule; enabling
// clears it.
const (
	// PauseReasonManual is a person disabling the schedule.
	PauseReasonManual = "manual"
	// PauseReasonCreatorDisabled is the schedule's creator account being disabled.
	PauseReasonCreatorDisabled = "creator_disabled"
	// PauseReasonCreatorNotMember is the creator being removed from the Space.
	PauseReasonCreatorNotMember = "creator_not_member"
	// PauseReasonConsecutiveFailures is the dispatcher pausing after a run of
	// firings that could not admit a Task.
	PauseReasonConsecutiveFailures = "consecutive_failures"
	// PauseReasonInvalidCron is a stored cron expression that no longer parses,
	// so the schedule can never compute a next fire time.
	PauseReasonInvalidCron = "invalid_cron"
)

// Schedule is a Space-owned recurring time trigger for one Agent.
type Schedule struct {
	ID        string `json:"id"`
	SpaceID   string `json:"space_id"`
	AgentID   string `json:"agent_id"`
	CreatedBy string `json:"created_by"`
	Name      string `json:"name,omitempty"`
	// Input is the fixed prompt each firing runs. The first slice does not
	// template it; see the design record's open questions.
	Input    string `json:"input"`
	CronExpr string `json:"cron_expr"`
	// Timezone is an IANA name, e.g. "Asia/Shanghai". Cron is evaluated in it so
	// a wall-clock time survives DST; storage and comparison stay UTC.
	Timezone string `json:"timezone"`
	// Enabled false is a paused schedule: it keeps its row and its NextFireAt but
	// the dispatcher does not claim it.
	Enabled bool `json:"enabled"`
	// PauseReason records why a paused schedule is paused; empty when enabled. It
	// is diagnostic, not authority: the dispatcher gates on Enabled, not on this.
	PauseReason string `json:"pause_reason,omitempty"`
	// NextFireAt is the UTC instant the dispatcher claims on and the
	// compare-and-swap target that makes a firing exactly-once (§7).
	NextFireAt time.Time  `json:"next_fire_at"`
	LastFireAt *time.Time `json:"last_fire_at,omitempty"`
	// LastTaskID is the Task the most recent firing created, or nil when no
	// firing has produced a Task yet.
	LastTaskID *string `json:"last_task_id,omitempty"`
	// ConsecutiveFailures counts firings that failed to admit a Task since the
	// last success. It bounds runaway cost: the dispatcher pauses a schedule that
	// fails this many times in a row (the threshold lives with the dispatcher).
	ConsecutiveFailures int       `json:"consecutive_failures"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

// CreateInput describes a new Schedule. NextFireAt is supplied by the caller,
// which computes it from CronExpr and Timezone; core does not parse cron.
type CreateInput struct {
	SpaceID    string
	AgentID    string
	CreatedBy  string
	Name       string
	Input      string
	CronExpr   string
	Timezone   string
	Enabled    bool
	NextFireAt time.Time
}

// UpdateInput changes a Schedule's editable fields. Only non-nil fields are
// written. When CronExpr or Timezone changes, the caller recomputes and passes
// NextFireAt in the same call so the stored due time stays consistent with the
// rule.
type UpdateInput struct {
	ScheduleID string
	Name       *string
	Input      *string
	CronExpr   *string
	Timezone   *string
	Enabled    *bool
	NextFireAt *time.Time
	// PauseReason is written only alongside disabling. Enabling the schedule
	// clears it regardless, so a re-enabled schedule never carries a stale
	// reason; the store enforces that so no caller has to remember it.
	PauseReason *string
}

// ClaimInput advances a due Schedule's NextFireAt only when it still equals
// ExpectedNextFireAt and the Schedule is enabled. The conditional update is what
// makes one due time fire exactly once across server replicas.
type ClaimInput struct {
	ScheduleID         string
	ExpectedNextFireAt time.Time
	NewNextFireAt      time.Time
}

// RecordFireInput records the outcome of one firing. Failed increments the
// consecutive-failure counter; a success resets it to zero. TaskID is nil when
// the firing produced no Task (admission refused or errored).
type RecordFireInput struct {
	ScheduleID string
	FiredAt    time.Time
	TaskID     *string
	Failed     bool
}

// Store provides Schedule persistence. Schedules belong to a Space.
type Store interface {
	CreateSchedule(ctx context.Context, in *CreateInput) (*Schedule, error)
	GetSchedule(ctx context.Context, scheduleID string) (*Schedule, error)
	// ListSchedulesBySpace returns a Space's schedules newest first. total is the
	// total matching count, ignoring limit and offset.
	ListSchedulesBySpace(ctx context.Context, spaceID string, limit, offset int) ([]Schedule, int, error)
	UpdateSchedule(ctx context.Context, in UpdateInput) (*Schedule, error)
	DeleteSchedule(ctx context.Context, scheduleID string) error
	// DueSchedules returns enabled schedules whose NextFireAt is at or before
	// now, oldest due time first, capped at limit.
	DueSchedules(ctx context.Context, now time.Time, limit int) ([]Schedule, error)
	// ListEnabledSchedulesByCreator returns every enabled schedule a given
	// account created, across Spaces. A deactivation pauses these at once rather
	// than waiting for each to reach its next fire time.
	ListEnabledSchedulesByCreator(ctx context.Context, createdBy string) ([]Schedule, error)
	// ClaimSchedule atomically advances NextFireAt. A false result means the
	// schedule changed under the caller — another replica claimed it, or it was
	// disabled or edited — and this caller must not fire it.
	ClaimSchedule(ctx context.Context, in ClaimInput) (claimed bool, err error)
	// RecordFire stores a firing's outcome: last fire time, the Task it created,
	// and the consecutive-failure counter.
	RecordFire(ctx context.Context, in RecordFireInput) error
}
