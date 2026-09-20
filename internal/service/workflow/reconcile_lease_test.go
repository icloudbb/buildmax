package workflow

import (
	"context"
	"errors"
	"testing"

	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/mock"
)

// canceledPassStore models a real database that rejects work on a canceled
// context: the pass's first read fails once the caller's context is canceled,
// and the lease release refuses to write on a canceled context. It records the
// context the release actually ran on, so the test can prove the release
// survived the caller's cancellation instead of stranding the lease.
type canceledPassStore struct {
	*mock.MockWorkflowStore
	releaseCalled bool
	releaseCtxErr error
}

func (s *canceledPassStore) GetWorkflowRun(ctx context.Context, id string) (*coreworkflow.Run, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return s.MockWorkflowStore.GetWorkflowRun(ctx, id)
}

func (s *canceledPassStore) ReleaseWorkflowRunLease(ctx context.Context, in coreworkflow.ReleaseLeaseInput) (bool, error) {
	s.releaseCalled = true
	s.releaseCtxErr = ctx.Err()
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return s.MockWorkflowStore.ReleaseWorkflowRunLease(ctx, in)
}

// TestReconcileReleasesLeaseWhenPassContextCanceled proves the failure that left
// a run's nodes stuck pending: a first-dispatch pass whose context was canceled
// (a disconnected create request) claimed the lease, then failed to release it
// on that same canceled context, so recovery could not retake the run until the
// lease's full TTL expired. Reconcile must release on a context detached from the
// caller's, handing the lease back so recovery retakes the run within a due-run
// interval.
func TestReconcileReleasesLeaseWhenPassContextCanceled(t *testing.T) {
	base := &mock.MockWorkflowStore{
		Runs: []coreworkflow.Run{{
			ID:     "wr_test",
			Status: string(coreworkflow.RunStatusRunning),
		}},
	}
	store := &canceledPassStore{MockWorkflowStore: base}
	svc := &Service{Workflows: store}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := svc.Reconcile(ctx, "wr_test")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Reconcile error = %v, want context.Canceled from the aborted pass", err)
	}
	if !store.releaseCalled {
		t.Fatal("lease was never released: a canceled pass stranded it for the full lease TTL")
	}
	if store.releaseCtxErr != nil {
		t.Fatalf("lease release ran on a canceled context (%v); it must detach from the caller's", store.releaseCtxErr)
	}

	run, err := base.GetWorkflowRun(context.Background(), "wr_test")
	if err != nil {
		t.Fatalf("GetWorkflowRun: %v", err)
	}
	if run.ReconcileOwner != nil {
		t.Fatal("lease still held after a canceled pass; recovery cannot retake the run until the TTL expires")
	}
	if run.NextReconcileAt == nil {
		t.Fatal("canceled pass left no next_reconcile_at; recovery would never find the run due")
	}
}
