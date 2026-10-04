// Package schedule holds the domain for a time trigger that runs an executor on
// a recurring wall-clock schedule. A Schedule is Space-owned; each firing starts
// its executor -- an Agent firing admits one ordinary Task, a Workflow firing
// starts one workflow run -- tagged with a schedule trigger source. See
// docs/design/scheduled-agent-execution.md.
//
// This package is pure domain: it does not parse cron expressions or load
// timezones. The caller that owns those (the dispatcher/service) computes each
// NextFireAt and hands it in, so no scheduling library reaches core.
package schedule

import (
	"context"
	"time"
)

// Executor kinds name what a schedule fires. They match issue.ExecutorAgent and
// issue.ExecutorWorkflow so "what performs the work" is one vocabulary across the
// product; ExecutorID stays an opaque handle whose meaning ExecutorKind fixes (an
// Agent id or a Workflow id).
const (
	ExecutorAgent    = "agent"
	ExecutorWorkflow = "workflow"
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
	// failed firings: ones that could not start their executor, or whose
	// execution ended failed.
	PauseReasonConsecutiveFailures = "consecutive_failures"
	// PauseReasonInvalidCron is a stored cron expression that no longer parses,
	// so the schedule can never compute a next fire time.
	PauseReasonInvalidCron = "invalid_cron"
)

// Schedule is a Space-owned recurring time trigger for one executor.
type Schedule struct {
	ID      string `json:"id"`
	SpaceID string `json:"space_id"`
	// ExecutorKind and ExecutorID name what the schedule fires: an Agent or a
	// Workflow in the same Space. ExecutorID is an opaque handle -- ExecutorKind
	// fixes which table it names -- so this package needs no reference to either.
	ExecutorKind string `json:"executor_kind"`
	ExecutorID   string `json:"executor_id"`
	CreatedBy    string `json:"created_by"`
	Name         string `json:"name,omitempty"`
	// Input is the fixed input each firing runs: a prompt for an Agent, or run
	// input JSON (validated against the workflow's input_schema) for a Workflow.
	// The first slice does not template it; see the design record's open questions.
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
	// LastFireRef is what the most recent firing produced: the Task id for an
	// Agent schedule, the workflow-run id for a Workflow schedule. Nil when no
	// firing has produced anything yet. It is an opaque handle read alongside
	// ExecutorKind, not a joined reference.
	LastFireRef *string `json:"last_fire_ref,omitempty"`
	// ConsecutiveFailures counts failed firings since the last one that succeeded:
	// a firing that could not start its executor, or whose Task run or workflow
	// run ended failed. A canceled execution is neither. It bounds runaway cost:
	// the dispatcher pauses a schedule that fails this many times in a row (the
	// threshold lives with the dispatcher). Enabling a schedule clears it.
	ConsecutiveFailures int `json:"consecutive_failures"`
	// Delivery, when set, sends each firing's releasable result to one person
	// through a Space Assistant's bot.
	Delivery  *Delivery `json:"delivery,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Delivery names who hears a schedule's result and through which Assistant.
// The result is released under the Assistant's roster entry for the schedule's
// executor at the time it is sent. See docs/design/space-assistants.md §11.
type Delivery struct {
	AssistantID string `json:"assistant_id"`
	// RequesterID is the BuildMax user the result goes to. A bot can message
	// only people who started a chat with it, so they must already have a
	// conversation with the Assistant.
	RequesterID string `json:"requester_id"`
}

// Delivery statuses. A firing that started its executor on a schedule with a
// Delivery records one pending FireDelivery; it settles once when the run ends.
const (
	DeliveryPending   = "pending"
	DeliveryDelivered = "delivered"
	// DeliverySkipped is a delivery that was not attempted; Reason says why.
	DeliverySkipped = "skipped"
	// DeliveryFailed is a send the chat platform refused or could not take.
	DeliveryFailed = "failed"
)

// Skip reasons, recorded on a skipped delivery and shown in the schedule's run
// history so the Space can tell what to fix.
const (
	SkipNoTarget          = "no_target"
	SkipRunNotSucceeded   = "run_not_succeeded"
	SkipAssistantGone     = "assistant_unavailable"
	SkipNoBot             = "no_bot"
	SkipNotInAudience     = "requester_not_in_audience"
	SkipLinkInactive      = "link_inactive"
	SkipNoConversation    = "no_conversation"
	SkipNotOnRoster       = "not_on_roster"
	SkipNothingReleasable = "nothing_releasable"
)

// FireDelivery is one firing's delivery: what the firing started (FireRef, read
// alongside the schedule's ExecutorKind) and how sending its result went.
type FireDelivery struct {
	ID         string     `json:"id"`
	ScheduleID string     `json:"schedule_id"`
	FireRef    string     `json:"fire_ref"`
	Status     string     `json:"status"`
	Reason     string     `json:"reason,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	SettledAt  *time.Time `json:"settled_at,omitempty"`
}

// SettleDeliveryInput settles a delivery still in From, pending when empty.
// Settling is the claim: of two replicas that reach one delivery, one settles
// it and sends, and the other does neither. A send that then fails moves the
// delivery from delivered to failed.
type SettleDeliveryInput struct {
	DeliveryID string
	From       string
	Status     string
	Reason     string
	SettledAt  time.Time
}

// CreateInput describes a new Schedule. NextFireAt is supplied by the caller,
// which computes it from CronExpr and Timezone; core does not parse cron.
type CreateInput struct {
	SpaceID      string
	ExecutorKind string
	ExecutorID   string
	CreatedBy    string
	Name         string
	Input        string
	CronExpr     string
	Timezone     string
	Enabled      bool
	NextFireAt   time.Time
	Delivery     *Delivery
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
	// clears it and the consecutive-failure counter regardless, so a re-enabled
	// schedule never carries a stale reason or a count that would pause it again
	// before it fired; the store enforces that so no caller has to remember it.
	PauseReason *string
	// Delivery sets the delivery target; a Delivery with an empty AssistantID
	// removes it.
	Delivery *Delivery
}

// ClaimInput advances a due Schedule's NextFireAt only when it still equals
// ExpectedNextFireAt and the Schedule is enabled. The conditional update is what
// makes one due time fire exactly once across server replicas.
type ClaimInput struct {
	ScheduleID         string
	ExpectedNextFireAt time.Time
	NewNextFireAt      time.Time
}

// RecordFireInput records one firing. Failed means the executor could not be
// started, which counts toward the consecutive-failure counter at once. A
// firing that started leaves the counter alone: what it started has not ended
// yet, and the store folds that outcome in when it does. A firing that started
// on a schedule with a Delivery also records a pending FireDelivery. FireRef is nil when the
// firing produced nothing. It is the Task id for an Agent firing, the
// workflow-run id for a Workflow firing.
type RecordFireInput struct {
	ScheduleID string
	FiredAt    time.Time
	FireRef    *string
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
	// RecordFire stores a firing: last fire time, what it started, and one more
	// consecutive failure when it could not start anything. The terminal outcome
	// of what it started is folded into the counter by the run transition that
	// records it, in the same transaction, so no caller reports it here.
	RecordFire(ctx context.Context, in RecordFireInput) error
	// ListPendingDeliveries returns pending deliveries across Spaces, oldest
	// first, capped at limit.
	ListPendingDeliveries(ctx context.Context, limit int) ([]FireDelivery, error)
	// SettleDelivery settles a pending delivery; false means it was no longer
	// pending.
	SettleDelivery(ctx context.Context, in SettleDeliveryInput) (bool, error)
	// ListDeliveriesBySchedule returns a schedule's deliveries newest first.
	ListDeliveriesBySchedule(ctx context.Context, scheduleID string, limit, offset int) ([]FireDelivery, int, error)
}
