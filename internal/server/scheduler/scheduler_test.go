package scheduler

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

// spyTaskRunStore records task-run transitions for tests.
type spyTaskRunStore struct {
	mu sync.Mutex

	// GetNextPendingTaskRun: return one run on first call, nil thereafter
	pendingRun   *coretask.Run
	pendingCalls int

	// GetTaskRunWithTask: the task behind pendingRun, or nil to report one missing
	task *coretask.Task

	// GetTaskRun: what the run looks like when the scheduler re-reads it after
	// the worker exits. Nil means the store has nothing to say about it.
	storedRun *coretask.Run

	// Recorded calls
	lastUpdateStatus *struct {
		taskRunID    string
		status       string
		endedAt      *time.Time
		errorMessage *string
		cancelReason string
	}
}

func newSpyTaskRunStore(taskRunID string) *spyTaskRunStore {
	return &spyTaskRunStore{
		pendingRun: &coretask.Run{
			ID:        taskRunID,
			TaskID:    "t_test",
			Input:     "input",
			Status:    "PENDING",
			CreatedBy: "u_test",
			CreatedAt: time.Now().UTC(),
		},
		task: &coretask.Task{
			ID:        "t_test",
			SpaceID:   "tm_test",
			CreatedBy: "u_test",
		},
	}
}

func (s *spyTaskRunStore) CreateTaskRun(_ context.Context, _ coretask.CreateRunInput) (*coretask.Run, error) {
	return nil, nil
}

func (s *spyTaskRunStore) ListTaskRunIDsByTasks(_ context.Context, _ []string) (map[string][]string, error) {
	return map[string][]string{}, nil
}

func (s *spyTaskRunStore) CountTaskRunsByStatus(_ context.Context) (map[string]int, error) {
	return nil, nil
}

func (s *spyTaskRunStore) GetNextPendingTaskRun(_ context.Context) (*coretask.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingCalls++
	if s.pendingCalls == 1 && s.pendingRun != nil {
		return s.pendingRun, nil
	}
	return nil, nil
}

func (s *spyTaskRunStore) GetTaskRun(_ context.Context, _ string) (*coretask.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.storedRun == nil {
		return s.pendingRun, nil
	}
	return s.storedRun, nil
}

func (s *spyTaskRunStore) ListTaskRunsByTask(_ context.Context, _ string) ([]coretask.Run, error) {
	return nil, nil
}

func (s *spyTaskRunStore) GetTaskRunWithTask(_ context.Context, _ string) (*coretask.Run, *coretask.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.task == nil {
		return nil, nil, nil
	}
	return s.pendingRun, s.task, nil
}

func (s *spyTaskRunStore) GetActiveTaskRunByTask(_ context.Context, _ string) (*coretask.Run, error) {
	return nil, nil
}

func (s *spyTaskRunStore) RequestTaskRunCancel(_ context.Context, _, _, _ string, _ time.Time) (bool, error) {
	return false, nil
}

func (s *spyTaskRunStore) ListActiveTaskRunsByCreator(_ context.Context, _ string) ([]coretask.ActiveRunRef, error) {
	return nil, nil
}

func (s *spyTaskRunStore) TransitionTaskRun(_ context.Context, in coretask.TransitionRunInput) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !coretask.ValidRunStatusTransition(in.ExpectedStatus, in.NewStatus) {
		return false, coretask.ErrInvalidRunTransition
	}
	var run *coretask.Run
	if s.storedRun != nil {
		run = s.storedRun
	} else {
		run = s.pendingRun
	}
	if run == nil || run.Status != string(in.ExpectedStatus) {
		return false, nil
	}
	run.Status = string(in.NewStatus)
	if in.NewStatus == coretask.RunStatusFailed || in.NewStatus == coretask.RunStatusCanceled {
		cancelReason := ""
		if in.CancelReason != nil {
			cancelReason = *in.CancelReason
		}
		s.lastUpdateStatus = &struct {
			taskRunID    string
			status       string
			endedAt      *time.Time
			errorMessage *string
			cancelReason string
		}{in.TaskRunID, string(in.NewStatus), in.EndedAt, in.ErrorMessage, cancelReason}
	}
	return true, nil
}

func (s *spyTaskRunStore) MarkTaskRunSeen(_ context.Context, _ string, _ time.Time) error {
	return nil
}

func (s *spyTaskRunStore) UpdateTaskRunWorkerInfo(_ context.Context, _ string, _ string, _ *string, _ *time.Time) error {
	return nil
}

func (s *spyTaskRunStore) RecordTaskRunAgentRevision(_ context.Context, _ string, _ int) error {
	return nil
}

func (s *spyTaskRunStore) RecordTaskRunSpaceAgentInstructionsRevision(_ context.Context, _ string, _ int) error {
	return nil
}

func (s *spyTaskRunStore) RecordTaskRunPluginPins(_ context.Context, _ string, _ []coreplugin.Pin) error {
	return nil
}

func (s *spyTaskRunStore) RecordTaskRunSandboxTiers(_ context.Context, _ string, _, _ string) error {
	return nil
}

func (s *spyTaskRunStore) ListTaskRunsWithExpiredTrace(_ context.Context, _ time.Time, _ int) ([]coretask.RunTraceRef, error) {
	return nil, nil
}

func (s *spyTaskRunStore) ClearTaskRunTracePath(_ context.Context, _ string) error {
	return nil
}

// failingRunner implements WorkerRunner and always returns an error.
type failingRunner struct{ err error }

func (f *failingRunner) Run(_ context.Context, _ coretask.Run, _ string) (string, *string, *time.Time, error) {
	if f.err != nil {
		return "", nil, nil, f.err
	}
	return "", nil, nil, errSpawnFailed
}

var errSpawnFailed = errors.New("spawn failed for test")

// A canceled worker exits non-zero after it has already reported CANCELED.
// Overwriting that with the process error would turn an honored instruction
// into a failure, and replace the reason the run stopped with "exit status 1".
func TestScheduler_Loop_KeepsAnOutcomeTheWorkerAlreadyReported(t *testing.T) {
	taskRunID := "r_spy_canceled_00000000"
	spy := newSpyTaskRunStore(taskRunID)
	spy.storedRun = &coretask.Run{ID: taskRunID, Status: string(coretask.RunStatusCanceled)}

	s, err := NewSchedulerWithPollInterval(spy, &failingRunner{err: errSpawnFailed}, nil, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	s.Start()
	time.Sleep(25 * time.Millisecond)
	s.Stop(context.Background())

	spy.mu.Lock()
	defer spy.mu.Unlock()
	if spy.lastUpdateStatus != nil {
		t.Errorf("the run was rewritten to %q after its worker reported CANCELED", spy.lastUpdateStatus.status)
	}
}

func TestScheduler_Loop_SpawnFailure_MarksRunFailed(t *testing.T) {
	taskRunID := "r_spy123456789012345678"
	spy := newSpyTaskRunStore(taskRunID)
	runner := &failingRunner{err: errSpawnFailed}

	s, err := NewSchedulerWithPollInterval(spy, runner, nil, 10*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	s.Start()
	// Wait for the loop to pick the run and record the failure, not for a fixed
	// time: a 25ms sleep missed the first 10ms tick on Windows, whose timers are
	// about 15ms coarse, and the test failed there with nothing called.
	deadline := time.Now().Add(5 * time.Second)
	for {
		spy.mu.Lock()
		called := spy.lastUpdateStatus != nil
		spy.mu.Unlock()
		if called || time.Now().After(deadline) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	s.Stop(context.Background())

	spy.mu.Lock()
	defer spy.mu.Unlock()

	// UpdateTaskRunStatus must have been called with FAILED, not PENDING
	if spy.lastUpdateStatus == nil {
		t.Fatal("UpdateTaskRunStatus was not called")
	}
	if spy.lastUpdateStatus.status != "FAILED" {
		t.Errorf("UpdateRun status = %q, want FAILED", spy.lastUpdateStatus.status)
	}
	if spy.lastUpdateStatus.taskRunID != taskRunID {
		t.Errorf("UpdateRun taskRunID = %q, want %q", spy.lastUpdateStatus.taskRunID, taskRunID)
	}
	if spy.lastUpdateStatus.endedAt == nil {
		t.Error("UpdateRun endedAt is nil, want set")
	}
	if spy.lastUpdateStatus.errorMessage == nil || *spy.lastUpdateStatus.errorMessage == "" {
		t.Error("UpdateRun errorMessage is nil or empty, want non-empty")
	}

}
