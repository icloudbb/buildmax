package task

import (
	"testing"
	"time"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/mock"
)

func TestRequestRunCancelPreservesWorkerAuthorityAndFirstIntent(t *testing.T) {
	for _, status := range []coretask.RunStatus{coretask.RunStatusPending, coretask.RunStatusScheduled, coretask.RunStatusRunning, coretask.RunStatusSucceeded} {
		t.Run(string(status), func(t *testing.T) {
			store := &mock.MockTaskRunStore{Runs: []coretask.Run{{ID: "run", Status: string(status)}}}
			svc := &Service{TaskRuns: store}
			now := time.Now().UTC()
			finished, err := svc.RequestRunCancel(t.Context(), "run", "", coretask.CancelReasonWorkflowStopped, now)
			if err != nil || finished != (status == coretask.RunStatusPending) {
				t.Fatalf("cancel: %v %v", finished, err)
			}
			_, err = svc.RequestRunCancel(t.Context(), "run", "another-person", coretask.CancelReasonUserRequested, now.Add(time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			got, _ := store.GetTaskRun(t.Context(), "run")
			want := status
			if status == coretask.RunStatusPending {
				want = coretask.RunStatusCanceled
			}
			if got.Status != string(want) {
				t.Fatalf("status %s, want %s", got.Status, want)
			}
			if status == coretask.RunStatusSucceeded {
				if got.CancelRequestedAt != nil {
					t.Fatal("changed completed run")
				}
			} else if got.CancelRequestedAt == nil || !got.CancelRequestedAt.Equal(now) || got.CancelReason != coretask.CancelReasonWorkflowStopped {
				t.Fatalf("lost original intent: %+v", got)
			}
		})
	}
}
