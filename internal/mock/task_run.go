package mock

import (
	"context"
	"fmt"
	"time"

	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

// MockTaskRunStore is an in-memory TaskRunStore for tests.
type MockTaskRunStore struct {
	Runs     []coretask.Run
	TaskList []coretask.Task
}

func (m *MockTaskRunStore) CreateTaskRun(_ context.Context, in coretask.CreateRunInput) (*coretask.Run, error) {
	// An idempotency key wins over the active-run check below, the same order
	// the real store applies inside its task-row lock: see internal/infra/db.
	if in.IdempotencyKey != nil && *in.IdempotencyKey != "" {
		for i := range m.Runs {
			if m.Runs[i].TaskID == in.TaskID && m.Runs[i].IdempotencyKey != nil && *m.Runs[i].IdempotencyKey == *in.IdempotencyKey {
				return &m.Runs[i], nil
			}
		}
	}
	// One task holds at most one active run. The real store enforces this, and
	// callers handle the refusal, so a double that quietly allowed a second one
	// would let a test pass on behavior the deployment does not have.
	for i := range m.Runs {
		if m.Runs[i].TaskID == in.TaskID && !coretask.RunStatusTerminal(m.Runs[i].Status) {
			return nil, coretask.ErrRunInProgress
		}
	}
	run := coretask.Run{
		ID:                    fmt.Sprintf("r_mock_%d", len(m.Runs)+1),
		TaskID:                in.TaskID,
		Input:                 in.Input,
		CreatedBy:             in.CreatedBy,
		CreatedByType:         in.CreatedByType,
		TriggerSource:         in.TriggerSource,
		Status:                string(coretask.RunStatusPending),
		RetryOfTaskRunID:      in.RetryOfTaskRunID,
		SourceMessageID:       in.SourceMessageID,
		AgentRevision:         in.AgentRevision,
		SandboxNetworkTier:    in.SandboxNetworkTier,
		SandboxFilesystemTier: in.SandboxFilesystemTier,
		IdempotencyKey:        in.IdempotencyKey,
		CreatedAt:             time.Now().UTC(),
	}
	m.Runs = append(m.Runs, run)
	return &m.Runs[len(m.Runs)-1], nil
}
func (m *MockTaskRunStore) CountTaskRunsByStatus(_ context.Context) (map[string]int, error) {
	out := make(map[string]int)
	for _, run := range m.Runs {
		out[run.Status]++
	}
	return out, nil
}

func (m *MockTaskRunStore) GetNextPendingTaskRun(_ context.Context) (*coretask.Run, error) {
	return nil, nil
}
func (m *MockTaskRunStore) GetTaskRun(_ context.Context, taskRunID string) (*coretask.Run, error) {
	for i := range m.Runs {
		if m.Runs[i].ID == taskRunID {
			return &m.Runs[i], nil
		}
	}
	return nil, nil
}

func (m *MockTaskRunStore) ListTaskRunsByTask(_ context.Context, taskID string) ([]coretask.Run, error) {
	var out []coretask.Run
	for _, run := range m.Runs {
		if run.TaskID == taskID {
			out = append(out, run)
		}
	}
	return out, nil
}
func (m *MockTaskRunStore) GetTaskRunWithTask(_ context.Context, taskRunID string) (*coretask.Run, *coretask.Task, error) {
	var run *coretask.Run
	for i := range m.Runs {
		if m.Runs[i].ID == taskRunID {
			run = &m.Runs[i]
			break
		}
	}
	if run == nil {
		return nil, nil, nil
	}
	var task *coretask.Task
	for i := range m.TaskList {
		if m.TaskList[i].ID == run.TaskID {
			task = &m.TaskList[i]
			break
		}
	}
	return run, task, nil
}
func (m *MockTaskRunStore) ListTaskRunIDsByTasks(_ context.Context, taskIDs []string) (map[string][]string, error) {
	want := make(map[string]bool, len(taskIDs))
	for _, id := range taskIDs {
		want[id] = true
	}
	out := make(map[string][]string)
	for i := len(m.Runs) - 1; i >= 0; i-- {
		if want[m.Runs[i].TaskID] {
			out[m.Runs[i].TaskID] = append(out[m.Runs[i].TaskID], m.Runs[i].ID)
		}
	}
	return out, nil
}

func (m *MockTaskRunStore) GetActiveTaskRunByTask(_ context.Context, taskID string) (*coretask.Run, error) {
	for i := range m.Runs {
		if m.Runs[i].TaskID != taskID {
			continue
		}
		if !coretask.RunStatusTerminal(m.Runs[i].Status) {
			return &m.Runs[i], nil
		}
	}
	return nil, nil
}

func (m *MockTaskRunStore) RequestTaskRunCancel(_ context.Context, taskRunID, requestedBy, reason string, requestedAt time.Time) (bool, error) {
	for i := range m.Runs {
		if m.Runs[i].ID != taskRunID {
			continue
		}
		if coretask.RunStatusTerminal(m.Runs[i].Status) || m.Runs[i].CancelRequestedAt != nil {
			return false, nil
		}
		m.Runs[i].CancelRequestedAt = &requestedAt
		m.Runs[i].CancelReason = reason
		if requestedBy != "" {
			m.Runs[i].CancelRequestedBy = &requestedBy
		}
		return true, nil
	}
	return false, nil
}

func (m *MockTaskRunStore) ListActiveTaskRunsForEligibility(_ context.Context, afterID string, limit int) ([]coretask.ActiveRunRef, error) {
	var out []coretask.ActiveRunRef
	seen := afterID == ""
	for i := range m.Runs {
		r := m.Runs[i]
		if !seen {
			if r.ID == afterID {
				seen = true
			}
			continue
		}
		if coretask.RunStatusTerminal(r.Status) || r.CancelRequestedAt != nil {
			continue
		}
		spaceID := ""
		for j := range m.TaskList {
			if m.TaskList[j].ID == r.TaskID {
				spaceID = m.TaskList[j].SpaceID
				break
			}
		}
		out = append(out, coretask.ActiveRunRef{TaskRunID: r.ID, SpaceID: spaceID, CreatedBy: r.CreatedBy})
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (m *MockTaskRunStore) TransitionTaskRun(ctx context.Context, in coretask.TransitionRunInput) (bool, error) {
	if !coretask.ValidRunStatusTransition(in.ExpectedStatus, in.NewStatus) {
		return false, coretask.ErrInvalidRunTransition
	}
	for i := range m.Runs {
		if m.Runs[i].ID != in.TaskRunID || m.Runs[i].Status != string(in.ExpectedStatus) {
			continue
		}
		m.Runs[i].Status = string(in.NewStatus)
		if in.StartedAt != nil {
			m.Runs[i].StartedAt = in.StartedAt
		}
		if in.EndedAt != nil {
			m.Runs[i].EndedAt = in.EndedAt
		}
		if in.Output != nil {
			m.Runs[i].Output = in.Output
		}
		if in.ErrorMessage != nil {
			m.Runs[i].ErrorMessage = in.ErrorMessage
		}
		if in.SessionID != nil {
			m.Runs[i].SessionID = in.SessionID
		}
		if in.PromptTokens != nil {
			m.Runs[i].PromptTokens = in.PromptTokens
		}
		if in.CompletionTokens != nil {
			m.Runs[i].CompletionTokens = in.CompletionTokens
		}
		if in.TracePath != nil {
			m.Runs[i].TracePath = in.TracePath
		}
		if in.CancelReason != nil {
			m.Runs[i].CancelReason = *in.CancelReason
		}
		return true, m.syncTaskFromRun(ctx, in.TaskRunID)
	}
	return false, nil
}
func (m *MockTaskRunStore) UpdateTaskRunWorkerInfo(_ context.Context, taskRunID, workerType string, k8sJobName *string, k8sJobCreatedAt *time.Time) error {
	return nil
}

// MarkTaskRunSeen stamps an active run, matching the store's status guard so a
// test cannot observe a terminal run's timestamp moving.
func (m *MockTaskRunStore) MarkTaskRunSeen(_ context.Context, taskRunID string, seenAt time.Time) error {
	for i := range m.Runs {
		if m.Runs[i].ID != taskRunID || coretask.RunStatusTerminal(m.Runs[i].Status) {
			continue
		}
		m.Runs[i].LastSeenAt = &seenAt
		return nil
	}
	return nil
}

func (m *MockTaskRunStore) syncTaskFromRun(_ context.Context, taskRunID string) error {
	for i := range m.Runs {
		if m.Runs[i].ID != taskRunID {
			continue
		}
		run := m.Runs[i]
		for j := range m.TaskList {
			if m.TaskList[j].ID != run.TaskID {
				continue
			}
			m.TaskList[j].Status = run.Status
			m.TaskList[j].LastRunID = &run.ID
			m.TaskList[j].Output = run.Output
			m.TaskList[j].StartedAt = run.StartedAt
			m.TaskList[j].EndedAt = run.EndedAt
			m.TaskList[j].ErrorMessage = run.ErrorMessage
		}
		return nil
	}
	return nil
}

func (m *MockTaskRunStore) RecordTaskRunPluginPins(_ context.Context, taskRunID string, pins []coreplugin.Pin) error {
	for i := range m.Runs {
		if m.Runs[i].ID != taskRunID {
			continue
		}
		// First write wins, as in the store.
		if len(m.Runs[i].PluginPins) == 0 {
			m.Runs[i].PluginPins = pins
		}
		return nil
	}
	return nil
}

func (m *MockTaskRunStore) RecordTaskRunSandboxTiers(_ context.Context, taskRunID string, networkTier, filesystemTier string) error {
	for i := range m.Runs {
		if m.Runs[i].ID != taskRunID {
			continue
		}
		// First write wins, as in the store. Written even when both tiers are
		// empty, so the guard is nil-ness, not emptiness.
		if m.Runs[i].SandboxNetworkTier == nil {
			m.Runs[i].SandboxNetworkTier = &networkTier
			m.Runs[i].SandboxFilesystemTier = &filesystemTier
		}
		return nil
	}
	return nil
}

func (m *MockTaskRunStore) RecordTaskRunAgentRevision(_ context.Context, taskRunID string, revision int) error {
	for i := range m.Runs {
		if m.Runs[i].ID != taskRunID {
			continue
		}
		// First write wins, as in the store: a worker polls its run more than once.
		if m.Runs[i].AgentRevision == nil {
			m.Runs[i].AgentRevision = &revision
		}
		return nil
	}
	return nil
}

func (m *MockTaskRunStore) RecordTaskRunSpaceAgentInstructionsRevision(_ context.Context, taskRunID string, revision int) error {
	for i := range m.Runs {
		if m.Runs[i].ID != taskRunID {
			continue
		}
		if m.Runs[i].SpaceAgentInstructionsRevision == nil {
			m.Runs[i].SpaceAgentInstructionsRevision = &revision
		}
		return nil
	}
	return nil
}

// ListTaskRunsWithExpiredTrace is inert: the mock carries no space coordinate, so
// trace retention is exercised against a purpose-built fake or the real store,
// not this double.
func (m *MockTaskRunStore) ListTaskRunsWithExpiredTrace(_ context.Context, _ time.Time, _ int) ([]coretask.RunTraceRef, error) {
	return nil, nil
}

func (m *MockTaskRunStore) ClearTaskRunTracePath(_ context.Context, taskRunID string) error {
	for i := range m.Runs {
		if m.Runs[i].ID == taskRunID {
			m.Runs[i].TracePath = nil
		}
	}
	return nil
}
