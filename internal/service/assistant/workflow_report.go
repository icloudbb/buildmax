package assistant

import (
	"context"
	"fmt"
	"time"

	coreassistant "github.com/icloudbb/buildmax/internal/core/assistant"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
)

// WorkflowReports reads and settles the outcome reports Workflow runs an
// Assistant started owe the people who asked.
type WorkflowReports interface {
	GetWorkflow(ctx context.Context, workflowID string) (*coreworkflow.Workflow, error)
	ListPendingWorkflowReports(ctx context.Context, limit int) ([]coreworkflow.Run, error)
	SettleWorkflowReport(ctx context.Context, workflowRunID string, from, to coreworkflow.ReportStatus) (bool, error)
}

// sweepWorkflowReports tells each requester how a Workflow run their
// conversation started ended, as Outcome does for a Task. A run's end is
// observed by whichever replica reconciles it, so the report is settled from
// durable state instead, claimed before it is sent.
func (f *FrontDoor) sweepWorkflowReports(ctx context.Context) {
	if f.Reports == nil || f.Conversations == nil {
		return
	}
	runs, err := f.Reports.ListPendingWorkflowReports(ctx, deliveryBatch)
	if err != nil {
		f.log().Warn("list pending workflow reports failed", "err", err)
		return
	}
	for _, run := range runs {
		f.reportWorkflowRun(ctx, run)
	}
}

func (f *FrontDoor) reportWorkflowRun(ctx context.Context, run coreworkflow.Run) {
	log := f.log().With("workflow_run_id", run.ID)
	plan, err := f.planWorkflowReport(ctx, run)
	if err != nil {
		log.Warn("workflow report check failed; will retry", "err", err)
		return
	}
	to := coreworkflow.ReportSent
	if plan.skip != "" {
		to = coreworkflow.ReportSkipped
	}
	won, err := f.Reports.SettleWorkflowReport(ctx, run.ID, coreworkflow.ReportPending, to)
	if err != nil || !won {
		if err != nil {
			log.Warn("settle workflow report failed", "err", err)
		}
		return
	}
	if plan.skip != "" {
		log.Info("workflow report skipped", "reason", plan.skip)
		return
	}
	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := f.Service.Bots.Gateway.Send(sendCtx, plan.platform, plan.botKey, plan.chatID, plan.text); err != nil {
		log.Warn("workflow report not sent", "err", err)
		if _, err := f.Reports.SettleWorkflowReport(ctx, run.ID, coreworkflow.ReportSent, coreworkflow.ReportFailed); err != nil {
			log.Warn("record failed workflow report", "err", err)
		}
	}
}

// planWorkflowReport applies, when the run has ended, the checks a Task's
// outcome report makes: the Assistant would still answer this requester, in
// this chat, through its bot. An error is a lookup that failed, retried next
// sweep.
func (f *FrontDoor) planWorkflowReport(ctx context.Context, run coreworkflow.Run) (deliveryPlan, error) {
	skip := func(reason string) (deliveryPlan, error) { return deliveryPlan{skip: reason}, nil }
	if run.ConversationID == nil {
		return skip("no_conversation")
	}
	conv, err := f.Conversations.GetConversation(ctx, *run.ConversationID)
	if err != nil {
		return deliveryPlan{}, err
	}
	if conv == nil || conv.AssistantID == "" || conv.ChannelRef == "" {
		return skip("no_conversation")
	}
	svc := f.Service
	a, err := svc.Store.GetAssistant(ctx, conv.AssistantID)
	if err != nil {
		return deliveryPlan{}, err
	}
	if a == nil {
		return skip("assistant_unavailable")
	}
	avail, err := svc.Availability(ctx, a)
	if err != nil {
		return deliveryPlan{}, err
	}
	if avail != coreassistant.Available {
		return skip("assistant_unavailable")
	}
	if f.checkRequester(ctx, a, conv.UserID) != "" {
		return skip("requester_not_in_audience")
	}
	b, err := svc.Store.GetBindingByAssistant(ctx, a.ID)
	if err != nil {
		return deliveryPlan{}, err
	}
	if b == nil || b.Platform != conv.Channel {
		return skip("no_bot")
	}
	reachable, err := svc.Bots.Gateway.Reachable(ctx, conv.UserID, b.Platform)
	if err != nil {
		return deliveryPlan{}, err
	}
	if !reachable {
		return skip("link_inactive")
	}
	name := "The request you made"
	if wf, err := f.Reports.GetWorkflow(ctx, run.WorkflowID); err == nil && wf != nil && wf.Name != "" {
		name = fmt.Sprintf("“%s”", wf.Name)
	}
	return deliveryPlan{
		platform: b.Platform, botKey: b.ID, chatID: conv.ChannelRef,
		text: workflowOutcomeText(name, run, a.Def.Entry(coreassistant.KindWorkflow, run.WorkflowID)),
	}, nil
}

// workflowOutcomeText is a succeeded run's releasable fields under the
// Assistant's current roster entry, or a fixed sentence: never error text or
// a link.
func workflowOutcomeText(name string, run coreworkflow.Run, entry *coreassistant.RosterEntry) string {
	switch coreworkflow.RunStatus(run.Status) {
	case coreworkflow.RunStatusSucceeded:
		if fields := coreassistant.Release(run.Result, entry); len(fields) > 0 {
			return name + " is done.\n\n" + coreassistant.FormatReleased(fields)
		}
		return name + " is done. There is nothing from it this assistant may share."
	case coreworkflow.RunStatusCanceled:
		return name + " was stopped."
	default:
		return name + " could not be completed."
	}
}
