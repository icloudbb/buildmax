package task

import (
	"context"
	"time"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

// RequestRunCancel records durable stop intent for an exact run. Only a run
// still pending can be ended here; a dispatched run belongs to its worker.
// Repeated calls preserve the first request and are safe after a restart.
// finished reports a pending run this call ended, for terminal notification.
func (s *Service) RequestRunCancel(ctx context.Context, runID, userID, reason string, now time.Time) (finished bool, err error) {
	if s.TaskRuns == nil {
		return false, ErrTaskRunsNotConfigured
	}
	if _, err := s.TaskRuns.RequestTaskRunCancel(ctx, runID, userID, reason, now); err != nil {
		return false, err
	}
	message := "this run was canceled before it started"
	return s.TaskRuns.TransitionTaskRun(ctx, coretask.TransitionRunInput{
		TaskRunID: runID, ExpectedStatus: coretask.RunStatusPending,
		NewStatus: coretask.RunStatusCanceled, EndedAt: &now, ErrorMessage: &message,
	})
}
