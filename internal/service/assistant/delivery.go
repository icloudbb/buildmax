package assistant

import (
	"context"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreassistant "github.com/icloudbb/buildmax/internal/core/assistant"
	coreschedule "github.com/icloudbb/buildmax/internal/core/schedule"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
)

// deliveryBatch bounds one sweep; the rest wait for the next.
const deliveryBatch = 50

var (
	ErrDeliveryAssistant   = apierr.New(apierr.KindInvalid, "delivery assistant not found in this space")
	ErrDeliveryNotOnRoster = apierr.New(apierr.KindInvalid,
		"the assistant's roster does not include what this schedule runs, so nothing of its result could be sent")
	ErrDeliveryNoConversation = apierr.New(apierr.KindInvalid,
		"this person has no conversation with the assistant; a bot can message only people who wrote to it first")
)

// DeliverySchedules is the schedule store narrowed to what delivery reads and
// settles.
type DeliverySchedules interface {
	GetSchedule(ctx context.Context, scheduleID string) (*coreschedule.Schedule, error)
	ListPendingDeliveries(ctx context.Context, limit int) ([]coreschedule.FireDelivery, error)
	SettleDelivery(ctx context.Context, in coreschedule.SettleDeliveryInput) (bool, error)
}

// DeliveryRuns reads how the work a firing started ended.
type DeliveryRuns interface {
	GetTask(ctx context.Context, taskID string) (*coretask.Task, error)
	GetTaskRun(ctx context.Context, taskRunID string) (*coretask.Run, error)
	GetWorkflowRun(ctx context.Context, workflowRunID string) (*coreworkflow.Run, error)
}

// CheckDeliveryTarget is the save-time check on a Schedule's delivery target:
// the Assistant is in the Space, its roster has the schedule's executor (or
// nothing of the result could be released), and the requester has written to
// it. Whether the Assistant is published, and the requester still in its
// audience, are checked when each result is sent.
func (f *FrontDoor) CheckDeliveryTarget(ctx context.Context, spaceID, executorKind, executorID string, d coreschedule.Delivery) error {
	if f == nil || f.Service.ready() != nil || f.Conversations == nil {
		return ErrNotConfigured
	}
	a, err := f.Service.Store.GetAssistant(ctx, d.AssistantID)
	if err != nil {
		return err
	}
	if a == nil || a.SpaceID != spaceID {
		return ErrDeliveryAssistant
	}
	if a.Def.Entry(executorKind, executorID) == nil {
		return ErrDeliveryNotOnRoster
	}
	conv, err := f.Conversations.LatestAssistantConversation(ctx, a.ID, d.RequesterID, "", "")
	if err != nil {
		return err
	}
	if conv == nil {
		return ErrDeliveryNoConversation
	}
	return nil
}

// DeliveryOutputSchema is the output schema an Agent firing of a delivering
// schedule must answer in: its roster entry's, read when the firing starts.
// Nil when there is none to apply; the delivery then records why it sent
// nothing.
func (f *FrontDoor) DeliveryOutputSchema(ctx context.Context, s coreschedule.Schedule) *string {
	if f == nil || f.Service.ready() != nil || s.Delivery == nil || s.ExecutorKind != coreschedule.ExecutorAgent {
		return nil
	}
	a, err := f.Service.Store.GetAssistant(ctx, s.Delivery.AssistantID)
	if err != nil || a == nil || a.SpaceID != s.SpaceID {
		return nil
	}
	entry := a.Def.Entry(coreassistant.KindAgent, s.ExecutorID)
	if entry == nil || len(entry.OutputSchema) == 0 {
		return nil
	}
	schema := string(entry.OutputSchema)
	return &schema
}

// Requesters lists who an Assistant's bot may message, for choosing a
// delivery target.
func (f *FrontDoor) Requesters(ctx context.Context, spaceID, assistantID string) ([]coreassistant.Requester, error) {
	if f == nil || f.Conversations == nil {
		return nil, ErrNotConfigured
	}
	if _, err := f.Service.load(ctx, spaceID, assistantID); err != nil {
		return nil, err
	}
	return f.Conversations.ListAssistantRequesters(ctx, assistantID, 200)
}

// SweepDeliveries settles every pending Schedule delivery and Workflow outcome
// report whose run has ended: it sends what the requester may learn through
// the Assistant's bot, or records why it did not. It is safe on every replica
// at once; each is claimed before it is sent, so it is sent at most once.
func (f *FrontDoor) SweepDeliveries(ctx context.Context) {
	if f == nil || f.Service.ready() != nil {
		return
	}
	f.sweepScheduleDeliveries(ctx)
	f.sweepWorkflowReports(ctx)
}

func (f *FrontDoor) sweepScheduleDeliveries(ctx context.Context) {
	if f.Schedules == nil || f.Runs == nil {
		return
	}
	pending, err := f.Schedules.ListPendingDeliveries(ctx, deliveryBatch)
	if err != nil {
		f.log().Warn("list pending deliveries failed", "err", err)
		return
	}
	for _, d := range pending {
		f.settleDelivery(ctx, d)
	}
}

func (f *FrontDoor) settleDelivery(ctx context.Context, d coreschedule.FireDelivery) {
	log := f.log().With("delivery_id", d.ID, "schedule_id", d.ScheduleID, "fire_ref", d.FireRef)
	s, err := f.Schedules.GetSchedule(ctx, d.ScheduleID)
	if err != nil || s == nil {
		// A deleted schedule takes its deliveries with it.
		if err != nil {
			log.Warn("delivery schedule lookup failed", "err", err)
		}
		return
	}
	ended, succeeded, result, err := f.fireOutcome(ctx, *s, d.FireRef)
	if err != nil {
		log.Warn("delivery run lookup failed", "err", err)
		return
	}
	if !ended {
		return
	}
	plan, err := f.planDelivery(ctx, *s, succeeded, result)
	if err != nil {
		log.Warn("delivery check failed; will retry", "err", err)
		return
	}
	status := coreschedule.DeliveryDelivered
	if plan.skip != "" {
		status = coreschedule.DeliverySkipped
	}
	won, err := f.Schedules.SettleDelivery(ctx, coreschedule.SettleDeliveryInput{
		DeliveryID: d.ID, Status: status, Reason: plan.skip, SettledAt: time.Now().UTC(),
	})
	if err != nil || !won {
		if err != nil {
			log.Warn("settle delivery failed", "err", err)
		}
		return
	}
	if plan.skip != "" {
		log.Info("schedule delivery skipped", "reason", plan.skip)
		return
	}
	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := f.Service.Bots.Gateway.Send(sendCtx, plan.platform, plan.botKey, plan.chatID, plan.text); err != nil {
		log.Warn("schedule delivery not sent", "err", err)
		if _, err := f.Schedules.SettleDelivery(ctx, coreschedule.SettleDeliveryInput{
			DeliveryID: d.ID, From: coreschedule.DeliveryDelivered, Status: coreschedule.DeliveryFailed,
			SettledAt: time.Now().UTC(),
		}); err != nil {
			log.Warn("record failed delivery", "err", err)
		}
	}
}

// fireOutcome reads whether what a firing started has ended, whether it
// succeeded, and its structured result: the Task's last run, or the workflow
// run. Work that no longer exists has ended without success.
func (f *FrontDoor) fireOutcome(ctx context.Context, s coreschedule.Schedule, ref string) (ended, succeeded bool, result *string, err error) {
	if s.ExecutorKind == coreschedule.ExecutorWorkflow {
		run, err := f.Runs.GetWorkflowRun(ctx, ref)
		if err != nil || run == nil {
			return err == nil, false, nil, err
		}
		if !coreworkflow.RunStatusTerminal(coreworkflow.RunStatus(run.Status)) {
			return false, false, nil, nil
		}
		return true, run.Status == string(coreworkflow.RunStatusSucceeded), run.Result, nil
	}
	t, err := f.Runs.GetTask(ctx, ref)
	if err != nil || t == nil {
		return err == nil, false, nil, err
	}
	if t.LastRunID == nil {
		return false, false, nil, nil
	}
	run, err := f.Runs.GetTaskRun(ctx, *t.LastRunID)
	if err != nil || run == nil {
		return err == nil, false, nil, err
	}
	if !coretask.RunStatusTerminal(run.Status) {
		return false, false, nil, nil
	}
	return true, run.Status == string(coretask.RunStatusSucceeded), run.Structured, nil
}

// deliveryPlan is either a reason to skip or what to send where.
type deliveryPlan struct {
	skip                           string
	platform, botKey, chatID, text string
}

// planDelivery applies, at send time, every rule that would stop the
// Assistant answering this requester itself, then the release contract of its
// current roster entry. An error is a lookup that failed, retried next sweep.
func (f *FrontDoor) planDelivery(ctx context.Context, s coreschedule.Schedule, succeeded bool, result *string) (deliveryPlan, error) {
	skip := func(reason string) (deliveryPlan, error) { return deliveryPlan{skip: reason}, nil }
	if s.Delivery == nil {
		return skip(coreschedule.SkipNoTarget)
	}
	if !succeeded {
		return skip(coreschedule.SkipRunNotSucceeded)
	}
	svc := f.Service
	a, err := svc.Store.GetAssistant(ctx, s.Delivery.AssistantID)
	if err != nil {
		return deliveryPlan{}, err
	}
	if a == nil || a.SpaceID != s.SpaceID {
		return skip(coreschedule.SkipAssistantGone)
	}
	avail, err := svc.Availability(ctx, a)
	if err != nil {
		return deliveryPlan{}, err
	}
	if avail != coreassistant.Available {
		return skip(coreschedule.SkipAssistantGone)
	}
	entry := a.Def.Entry(s.ExecutorKind, s.ExecutorID)
	if entry == nil {
		return skip(coreschedule.SkipNotOnRoster)
	}
	fields := coreassistant.Release(result, entry)
	if len(fields) == 0 {
		return skip(coreschedule.SkipNothingReleasable)
	}
	if f.checkRequester(ctx, a, s.Delivery.RequesterID) != "" {
		return skip(coreschedule.SkipNotInAudience)
	}
	b, err := svc.Store.GetBindingByAssistant(ctx, a.ID)
	if err != nil {
		return deliveryPlan{}, err
	}
	if b == nil {
		return skip(coreschedule.SkipNoBot)
	}
	reachable, err := svc.Bots.Gateway.Reachable(ctx, s.Delivery.RequesterID, b.Platform)
	if err != nil {
		return deliveryPlan{}, err
	}
	if !reachable {
		return skip(coreschedule.SkipLinkInactive)
	}
	conv, err := f.Conversations.LatestAssistantConversation(ctx, a.ID, s.Delivery.RequesterID, b.Platform, "")
	if err != nil {
		return deliveryPlan{}, err
	}
	if conv == nil {
		return skip(coreschedule.SkipNoConversation)
	}
	title := s.Name
	if title == "" {
		title = "Scheduled update"
	}
	return deliveryPlan{
		platform: b.Platform, botKey: b.ID, chatID: conv.ChannelRef,
		text: title + "\n\n" + coreassistant.FormatReleased(fields),
	}, nil
}
