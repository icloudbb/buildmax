package bootstrap

import (
	"context"
	"errors"
	"testing"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/infra/workerclient"
)

type recordingUpdater struct {
	req *workerclient.PatchTaskRunRequest
	err error
}

func (u *recordingUpdater) UpdateRunStatus(_ context.Context, _ string, req *workerclient.PatchTaskRunRequest) error {
	if u.err != nil {
		return u.err
	}
	u.req = req
	return nil
}

// A refusal the server recorded is the run's outcome, so the worker must say
// so: the exit code treats ErrRunFailed as terminal and does not restart a pod
// that would only find the run FAILED. A refusal it could not report is not
// marked, and still reaches the scheduler as a failed dispatch.
func TestReportPluginRefusalMarksOnlyAReportedFailure(t *testing.T) {
	reported := &recordingUpdater{}
	err := reportPluginRefusal(context.Background(), reported, "rt_1", "plugin p is not activated")
	if !errors.Is(err, coretask.ErrRunFailed) {
		t.Fatalf("err = %v, want ErrRunFailed for a reported refusal", err)
	}
	if reported.req == nil || reported.req.Status != string(coretask.RunStatusFailed) {
		t.Fatalf("report = %+v, want FAILED", reported.req)
	}

	unreachable := &recordingUpdater{err: errors.New("connection refused")}
	err = reportPluginRefusal(context.Background(), unreachable, "rt_1", "plugin p is not activated")
	if err == nil || errors.Is(err, coretask.ErrRunFailed) {
		t.Fatalf("err = %v, want the report error without ErrRunFailed", err)
	}
}
