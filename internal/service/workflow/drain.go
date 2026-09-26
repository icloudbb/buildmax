package workflow

import (
	"context"
	"fmt"
	"time"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/util"
)

// drainRun never dispatches. Stop intent lives on the Workflow before any Task
// request, so an interrupted pass can repeat every request and observation.
func (s *Service) drainRun(ctx context.Context, run *coreworkflow.Run, nodes []coreworkflow.NodeRun, now time.Time, next **time.Time) error {
	*next = util.Ptr(now.Add(reconcileObserveInterval))
	active := false
	for _, node := range nodes {
		if coreworkflow.NodeRunStatusTerminal(coreworkflow.NodeRunStatus(node.Status)) {
			continue
		}
		if node.TaskRunID == nil || *node.TaskRunID == "" {
			return fmt.Errorf("draining node %s has no TaskRun", node.ID)
		}
		if s.TaskService == nil {
			return ErrTasksNotConfigured
		}
		if _, err := s.TaskService.RequestRunCancel(ctx, *node.TaskRunID, "", coretask.CancelReasonWorkflowStopped, now); err != nil {
			return err
		}
		taskRun, err := s.stepTaskRun(ctx, &node)
		if err != nil {
			return err
		}
		if taskRun == nil || !coretask.RunStatusTerminal(taskRun.Status) {
			active = true
			continue
		}
		status := coreworkflow.NodeRunStatusCanceled
		switch taskRun.Status {
		case string(coretask.RunStatusSucceeded):
			status = coreworkflow.NodeRunStatusSucceeded
		case string(coretask.RunStatusFailed):
			status = coreworkflow.NodeRunStatusFailed
		}
		if _, err := s.Workflows.TransitionWorkflowNodeRun(ctx, coreworkflow.TransitionNodeRunInput{
			NodeRunID: node.ID, ExpectedStatus: coreworkflow.NodeRunStatusRunning, NewStatus: status,
			Output: taskRun.Output, Structured: taskRun.Structured, ErrorMessage: taskRun.ErrorMessage, EndedAt: &now,
		}); err != nil {
			return err
		}
	}
	if active {
		return nil
	}
	status := coreworkflow.RunStatusFailed
	if run.Status == string(coreworkflow.RunStatusCanceling) {
		status = coreworkflow.RunStatusCanceled
	}
	_, err := s.Workflows.TransitionWorkflowRun(ctx, coreworkflow.TransitionRunInput{
		WorkflowRunID: run.ID, ExpectedStatus: coreworkflow.RunStatus(run.Status), NewStatus: status, EndedAt: &now,
	})
	if err == nil {
		*next = nil
	}
	return err
}
