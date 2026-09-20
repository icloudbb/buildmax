package scheduler

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/icloudbb/buildmax/internal/core/eligibility"
	coreschedule "github.com/icloudbb/buildmax/internal/core/schedule"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	schedulesvc "github.com/icloudbb/buildmax/internal/service/schedule"
	tasksvc "github.com/icloudbb/buildmax/internal/service/task"
	workflowsvc "github.com/icloudbb/buildmax/internal/service/workflow"
)

func (d *ScheduleDispatcher) log() *slog.Logger { return componentLog("schedule_dispatcher") }

const (
	// defaultSchedulePollInterval is coarse on purpose: the smallest schedule
	// granularity is one minute, so checking once a minute never misses a due
	// time by more than the tick.
	defaultSchedulePollInterval = time.Minute
	// scheduleDueBatch bounds how many due schedules one tick claims, so a large
	// backlog after an outage is worked through over a few ticks rather than in
	// one unbounded pass.
	scheduleDueBatch = 100
	// maxConsecutiveScheduleFailures pauses a schedule that fails to admit a Task
	// this many times in a row, so an unattended trigger that fails forever stops
	// spending quota. A small single-digit value; see the proposal's open
	// questions for tuning it.
	maxConsecutiveScheduleFailures = 5
)

// ScheduleAdmitter creates the Task an Agent firing produces. It is the Task
// application service narrowed to the one call the dispatcher makes, so the
// dispatcher cannot reach past admission into task internals.
type ScheduleAdmitter interface {
	CreateTask(ctx context.Context, cmd tasksvc.CreateTaskCmd) (*coretask.Task, error)
}

// WorkflowStarter starts the run a Workflow firing produces. It is the workflow
// application service narrowed to the one call the dispatcher makes. Nil on a
// dispatcher wired without workflows, in which case a workflow schedule's firing
// is recorded as a failure and the schedule pauses after a run of them.
type WorkflowStarter interface {
	StartWorkflowRun(ctx context.Context, cmd workflowsvc.StartWorkflowRunCmd) (*coreworkflow.Run, []coreworkflow.NodeRun, error)
}

// ScheduleDispatcher polls for due schedules and admits one Task per firing
// through the Task service. It creates no execution state of its own: a firing
// is an ordinary Task tagged with a schedule trigger source.
//
// A failed sweep is logged and retried on the next tick rather than stopping the
// server, the same fail-open stance the other scheduler loops take.
type ScheduleDispatcher struct {
	schedules coreschedule.Store
	admitter  ScheduleAdmitter
	// workflows starts a workflow schedule's run. Nil skips workflow firing, which
	// a deployment that wires no workflow service has.
	workflows WorkflowStarter
	// eligible answers whether a schedule's creator may still run work in its
	// Space. Nil skips the check, which is what a deployment that wires no
	// authority stores has.
	eligible    eligibility.Checker
	interval    time.Duration
	maxFailures int
	// now is the clock, injectable so a test can drive due times and catch-up
	// without waiting on the wall clock.
	now    func() time.Time
	stopCh chan struct{}
	doneCh chan struct{}
}

// NewScheduleDispatcher returns a dispatcher for the given schedule store and
// Task admitter. Use 0 for the default poll interval.
func NewScheduleDispatcher(schedules coreschedule.Store, admitter ScheduleAdmitter, interval time.Duration) (*ScheduleDispatcher, error) {
	if schedules == nil {
		return nil, errors.New("schedule dispatcher: schedules must not be nil")
	}
	if admitter == nil {
		return nil, errors.New("schedule dispatcher: admitter must not be nil")
	}
	if interval <= 0 {
		interval = defaultSchedulePollInterval
	}
	return &ScheduleDispatcher{
		schedules:   schedules,
		admitter:    admitter,
		interval:    interval,
		maxFailures: maxConsecutiveScheduleFailures,
		now:         func() time.Time { return time.Now().UTC() },
		stopCh:      make(chan struct{}),
		doneCh:      make(chan struct{}),
	}, nil
}

// WithEligibility lets the dispatcher pause a schedule whose creator was
// disabled or removed from its Space, rather than minting Tasks that would fail
// at dispatch. Optional, like the run scheduler's own check.
func (d *ScheduleDispatcher) WithEligibility(c eligibility.Checker) *ScheduleDispatcher {
	d.eligible = c
	return d
}

// WithWorkflows lets the dispatcher fire workflow schedules by starting a
// workflow run. Without it, a workflow schedule's firing is recorded as a
// failure and the schedule pauses after a run of them.
func (d *ScheduleDispatcher) WithWorkflows(w WorkflowStarter) *ScheduleDispatcher {
	d.workflows = w
	return d
}

// Start launches the poll loop. A nil dispatcher is a no-op so the caller does
// not have to check.
func (d *ScheduleDispatcher) Start() {
	if d == nil {
		return
	}
	go d.loop()
	d.log().Info("started", "interval", d.interval)
}

// Stop signals the loop to exit and blocks until it has finished.
func (d *ScheduleDispatcher) Stop() {
	if d == nil {
		return
	}
	close(d.stopCh)
	<-d.doneCh
	d.log().Info("stopped")
}

func (d *ScheduleDispatcher) loop() {
	defer close(d.doneCh)
	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()
	for {
		select {
		case <-d.stopCh:
			return
		case <-ticker.C:
			d.sweep(context.Background())
		}
	}
}

// sweep claims and fires every schedule due at the current time.
func (d *ScheduleDispatcher) sweep(ctx context.Context) {
	now := d.now()
	due, err := d.schedules.DueSchedules(ctx, now, scheduleDueBatch)
	if err != nil {
		d.log().WarnContext(ctx, "list due schedules failed", "err", err)
		return
	}
	for i := range due {
		d.fireOne(ctx, due[i], now)
	}
}

// fireOne claims one due schedule and admits its Task.
//
// The claim advances next_fire_at before admission, so a schedule whose Task
// cannot be created does not wedge on the same due time: it records a failed
// fire and moves to its next time, and a run of failures pauses it. The next
// time is computed from now rather than the stale due time, so a schedule missed
// during an outage fires once and resumes instead of replaying every slot.
func (d *ScheduleDispatcher) fireOne(ctx context.Context, s coreschedule.Schedule, now time.Time) {
	log := d.log().With("schedule_id", s.ID, "space_id", s.SpaceID)

	next, err := schedulesvc.Next(s.CronExpr, s.Timezone, now)
	if err != nil {
		// A stored expression that no longer parses can never fire; pause it so the
		// dispatcher stops reaching it every tick.
		log.WarnContext(ctx, "invalid cron on a stored schedule; pausing", "err", err)
		d.pause(ctx, s.ID, coreschedule.PauseReasonInvalidCron, log)
		return
	}

	claimed, err := d.schedules.ClaimSchedule(ctx, coreschedule.ClaimInput{
		ScheduleID:         s.ID,
		ExpectedNextFireAt: s.NextFireAt,
		NewNextFireAt:      next,
	})
	if err != nil {
		log.WarnContext(ctx, "claim schedule failed", "err", err)
		return
	}
	if !claimed {
		// Another replica advanced it, or it was disabled or edited between the due
		// query and here. Either way this caller must not fire it.
		return
	}

	// Checked after the claim so exactly one caller reaches it: a schedule whose
	// creator can no longer run work in this Space pauses rather than minting
	// Tasks that would only fail at dispatch. A store outage leaves eligibility
	// unknown; fire anyway rather than pause a space's schedule over a blip, the
	// same trade-off the run scheduler makes.
	if d.eligible != nil && s.CreatedBy != "" {
		switch err := d.eligible.Check(ctx, s.CreatedBy, s.SpaceID); {
		case err == nil:
		case errors.Is(err, eligibility.ErrUnavailable):
			log.WarnContext(ctx, "could not verify schedule creator eligibility; firing anyway", "err", err)
		default:
			reason := coreschedule.PauseReasonCreatorNotMember
			if errors.Is(err, eligibility.ErrAccountDisabled) {
				reason = coreschedule.PauseReasonCreatorDisabled
			}
			log.InfoContext(ctx, "schedule creator is no longer eligible; pausing", "user_id", s.CreatedBy, "reason", reason, "err", err)
			d.pause(ctx, s.ID, reason, log)
			return
		}
	}

	fireRef, err := d.startExecutor(ctx, s)
	if err != nil {
		log.WarnContext(ctx, "schedule firing could not start its executor",
			"executor_kind", s.ExecutorKind, "err", err)
		if rErr := d.schedules.RecordFire(ctx, coreschedule.RecordFireInput{
			ScheduleID: s.ID, FiredAt: now, Failed: true,
		}); rErr != nil {
			log.WarnContext(ctx, "record failed fire", "err", rErr)
		}
		// s.ConsecutiveFailures is the count before this fire; +1 is where it now
		// stands. Pause once a run of failures reaches the bound.
		if s.ConsecutiveFailures+1 >= d.maxFailures {
			log.WarnContext(ctx, "schedule paused after consecutive failures",
				"consecutive_failures", s.ConsecutiveFailures+1)
			d.pause(ctx, s.ID, coreschedule.PauseReasonConsecutiveFailures, log)
		}
		return
	}

	if err := d.schedules.RecordFire(ctx, coreschedule.RecordFireInput{
		ScheduleID: s.ID, FiredAt: now, FireRef: fireRef, Failed: false,
	}); err != nil {
		log.WarnContext(ctx, "record successful fire", "err", err)
	}
	log.InfoContext(ctx, "schedule fired",
		"executor_kind", s.ExecutorKind, "fire_ref", derefString(fireRef), "next_fire_at", next)
}

// startExecutor starts a firing's executor and returns the opaque public id it
// produced: a Task for an Agent schedule, a workflow run for a Workflow schedule.
// A firing is tagged with the schedule trigger source and attributed to the
// schedule's creator, so a run started here is metered and authorized as that
// person's work.
func (d *ScheduleDispatcher) startExecutor(ctx context.Context, s coreschedule.Schedule) (*string, error) {
	switch s.ExecutorKind {
	case coreschedule.ExecutorWorkflow:
		if d.workflows == nil {
			return nil, errors.New("workflow schedule fired but no workflow service is wired")
		}
		run, _, err := d.workflows.StartWorkflowRun(ctx, workflowsvc.StartWorkflowRunCmd{
			SpaceID:    s.SpaceID,
			UserID:     s.CreatedBy,
			WorkflowID: s.ExecutorID,
			ScheduleID: &s.ID,
			Input:      s.Input,
		})
		if err != nil {
			return nil, err
		}
		return &run.ID, nil
	default:
		agentID := s.ExecutorID
		task, err := d.admitter.CreateTask(ctx, tasksvc.CreateTaskCmd{
			SpaceID:       s.SpaceID,
			UserID:        s.CreatedBy,
			AgentID:       &agentID,
			Input:         s.Input,
			ScheduleID:    &s.ID,
			CreatedByType: coretask.RunCreatedByTypeSystem,
			TriggerSource: coretask.RunTriggerSourceSchedule,
		})
		if err != nil {
			return nil, err
		}
		return &task.ID, nil
	}
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// pause disables a schedule and records why, so an operator can tell an
// automatic pause from one they applied. The pause itself is the authority; the
// reason is diagnostic.
func (d *ScheduleDispatcher) pause(ctx context.Context, scheduleID, reason string, log *slog.Logger) {
	disabled := false
	if _, err := d.schedules.UpdateSchedule(ctx, coreschedule.UpdateInput{
		ScheduleID: scheduleID, Enabled: &disabled, PauseReason: &reason,
	}); err != nil {
		log.WarnContext(ctx, "pause schedule failed", "err", err)
	}
}
