package mock

import (
	"context"
	"fmt"
	"sort"
	"time"

	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
)

// MockWorkflowStore is an in-memory WorkflowStore for tests. It records
// revisions the way the database store does, so a test can assert on history.
type MockWorkflowStore struct {
	Workflows []coreworkflow.Workflow
	Revisions []coreworkflow.Revision
	Runs      []coreworkflow.Run
	NodeRuns  []coreworkflow.NodeRun
}

func (m *MockWorkflowStore) appendRevision(w *coreworkflow.Workflow, createdBy string) {
	m.Revisions = append(m.Revisions, coreworkflow.Revision{
		WorkflowID:  w.ID,
		Revision:    w.Revision,
		Name:        w.Name,
		Description: w.Description,
		Definition:  w.Definition,
		Status:      w.Status,
		CreatedBy:   createdBy,
		CreatedAt:   time.Now().UTC(),
	})
}

func (m *MockWorkflowStore) ListWorkflowsBySpace(_ context.Context, spaceID string) ([]coreworkflow.Workflow, error) {
	var out []coreworkflow.Workflow
	for _, workflow := range m.Workflows {
		if workflow.SpaceID == spaceID {
			out = append(out, workflow)
		}
	}
	return out, nil
}

func (m *MockWorkflowStore) CreateWorkflow(_ context.Context, spaceID, createdBy, name, description, definition string) (*coreworkflow.Workflow, error) {
	workflow := coreworkflow.Workflow{
		ID:          fmt.Sprintf("w_mock_%d", len(m.Workflows)+1),
		SpaceID:     spaceID,
		Name:        name,
		Description: description,
		Definition:  definition,
		Status:      coreworkflow.StatusDraft,
		Revision:    1,
		CreatedBy:   createdBy,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	m.Workflows = append(m.Workflows, workflow)
	created := &m.Workflows[len(m.Workflows)-1]
	m.appendRevision(created, createdBy)
	return created, nil
}

func (m *MockWorkflowStore) GetWorkflow(_ context.Context, workflowID string) (*coreworkflow.Workflow, error) {
	for i := range m.Workflows {
		if m.Workflows[i].ID == workflowID {
			return &m.Workflows[i], nil
		}
	}
	return nil, nil
}

func (m *MockWorkflowStore) UpdateWorkflow(_ context.Context, workflowID, spaceID string, in coreworkflow.UpdateInput) (*coreworkflow.Workflow, error) {
	for i := range m.Workflows {
		if m.Workflows[i].ID != workflowID || m.Workflows[i].SpaceID != spaceID {
			continue
		}
		updated := m.Workflows[i]
		if in.Name != nil {
			updated.Name = *in.Name
		}
		if in.Description != nil {
			updated.Description = *in.Description
		}
		if in.Definition != nil {
			updated.Definition = *in.Definition
		}
		if in.Status != nil {
			updated.Status = *in.Status
		}
		if updated.Name == m.Workflows[i].Name && updated.Description == m.Workflows[i].Description &&
			updated.Definition == m.Workflows[i].Definition && updated.Status == m.Workflows[i].Status {
			return &m.Workflows[i], nil
		}
		// Guard the write on the revision the caller observed, as the real store
		// does, so a stale edit is refused rather than overwriting a newer one.
		if m.Workflows[i].Revision != in.ExpectedRevision {
			return nil, coreworkflow.ErrRevisionConflict
		}
		if updated.Revision < 1 {
			updated.Revision = 1
		}
		updated.Revision++
		updated.UpdatedAt = time.Now().UTC()
		m.Workflows[i] = updated
		m.appendRevision(&m.Workflows[i], in.UpdatedBy)
		return &m.Workflows[i], nil
	}
	return nil, nil
}

func (m *MockWorkflowStore) ListWorkflowRevisions(_ context.Context, workflowID string, limit, offset int) ([]coreworkflow.Revision, int, error) {
	var all []coreworkflow.Revision
	for i := len(m.Revisions) - 1; i >= 0; i-- {
		if m.Revisions[i].WorkflowID == workflowID {
			all = append(all, m.Revisions[i])
		}
	}
	return pageRevisions(all, limit, offset), len(all), nil
}

func (m *MockWorkflowStore) GetWorkflowRevision(_ context.Context, workflowID string, revision int) (*coreworkflow.Revision, error) {
	for i := range m.Revisions {
		if m.Revisions[i].WorkflowID == workflowID && m.Revisions[i].Revision == revision {
			return &m.Revisions[i], nil
		}
	}
	return nil, nil
}

func (m *MockWorkflowStore) CreateWorkflowRun(_ context.Context, in coreworkflow.CreateRunInput) (*coreworkflow.Run, error) {
	run := coreworkflow.Run{
		ID:               fmt.Sprintf("wr_mock_%d", len(m.Runs)+1),
		WorkflowID:       in.WorkflowID,
		WorkflowRevision: in.WorkflowRevision,
		IssueID:          in.IssueID,
		ScheduleID:       in.ScheduleID,
		Status:           in.Status,
		CreatedBy:        in.CreatedBy,
		CreatedAt:        time.Now().UTC(),
		StartedAt:        in.StartedAt,
	}
	m.Runs = append(m.Runs, run)
	return &m.Runs[len(m.Runs)-1], nil
}

func (m *MockWorkflowStore) ListWorkflowRunsByWorkflow(_ context.Context, workflowID string, limit, offset int) ([]coreworkflow.Run, int, error) {
	var out []coreworkflow.Run
	for _, run := range m.Runs {
		if run.WorkflowID == workflowID {
			out = append(out, run)
		}
	}
	total := len(out)
	if offset > total {
		return []coreworkflow.Run{}, total, nil
	}
	if limit <= 0 || offset+limit > total {
		limit = total - offset
	}
	return out[offset : offset+limit], total, nil
}

func (m *MockWorkflowStore) ListWorkflowRunsByIssue(_ context.Context, issueID string, limit, offset int) ([]coreworkflow.Run, int, error) {
	var out []coreworkflow.Run
	for _, run := range m.Runs {
		if run.IssueID != nil && *run.IssueID == issueID {
			out = append(out, run)
		}
	}
	total := len(out)
	if offset > total {
		return []coreworkflow.Run{}, total, nil
	}
	if limit <= 0 || offset+limit > total {
		limit = total - offset
	}
	return out[offset : offset+limit], total, nil
}

func (m *MockWorkflowStore) ListWorkflowRunsBySchedule(_ context.Context, scheduleID string, limit, offset int) ([]coreworkflow.Run, int, error) {
	var out []coreworkflow.Run
	for _, run := range m.Runs {
		if run.ScheduleID != nil && *run.ScheduleID == scheduleID {
			out = append(out, run)
		}
	}
	total := len(out)
	if offset > total {
		return []coreworkflow.Run{}, total, nil
	}
	if limit <= 0 || offset+limit > total {
		limit = total - offset
	}
	return out[offset : offset+limit], total, nil
}

func (m *MockWorkflowStore) GetWorkflowRun(_ context.Context, workflowRunID string) (*coreworkflow.Run, error) {
	for i := range m.Runs {
		if m.Runs[i].ID == workflowRunID {
			return &m.Runs[i], nil
		}
	}
	return nil, nil
}

func (m *MockWorkflowStore) ListWorkflowNodeRuns(_ context.Context, workflowRunID string) ([]coreworkflow.NodeRun, error) {
	var out []coreworkflow.NodeRun
	for _, step := range m.NodeRuns {
		if step.WorkflowRunID == workflowRunID {
			out = append(out, step)
		}
	}
	return out, nil
}

func (m *MockWorkflowStore) CreateWorkflowNodeRuns(_ context.Context, workflowRunID string, steps []coreworkflow.CreateNodeRunInput) ([]coreworkflow.NodeRun, error) {
	out := make([]coreworkflow.NodeRun, len(steps))
	for i := range steps {
		out[i] = coreworkflow.NodeRun{
			ID:                fmt.Sprintf("wsr_mock_%d", len(m.NodeRuns)+1),
			WorkflowRunID:     workflowRunID,
			NodeID:            steps[i].NodeID,
			NodeIndex:         steps[i].NodeIndex,
			NodeType:          steps[i].NodeType,
			Needs:             steps[i].Needs,
			IssueAccess:       steps[i].IssueAccess,
			TargetAgentID:     steps[i].TargetAgentID,
			AgentName:         steps[i].AgentName,
			AgentDescription:  steps[i].AgentDescription,
			AgentInstructions: steps[i].AgentInstructions,
			AgentRevision:     steps[i].AgentRevision,
			Prompt:            steps[i].Prompt,
			Bindings:          steps[i].Bindings,
			Status:            steps[i].Status,
			CreatedAt:         time.Now().UTC(),
		}
		m.NodeRuns = append(m.NodeRuns, out[i])
	}
	return out, nil
}

func (m *MockWorkflowStore) TransitionWorkflowRun(_ context.Context, in coreworkflow.TransitionRunInput) (bool, error) {
	if !coreworkflow.ValidRunStatusTransition(in.ExpectedStatus, in.NewStatus) {
		return false, fmt.Errorf("%w: %s -> %s", coreworkflow.ErrInvalidRunTransition, in.ExpectedStatus, in.NewStatus)
	}
	for i := range m.Runs {
		if m.Runs[i].ID != in.WorkflowRunID {
			continue
		}
		if m.Runs[i].Status != string(in.ExpectedStatus) {
			return false, nil
		}
		m.Runs[i].Status = string(in.NewStatus)
		if in.StartedAt != nil {
			m.Runs[i].StartedAt = in.StartedAt
		}
		if in.EndedAt != nil {
			m.Runs[i].EndedAt = in.EndedAt
		}
		if in.ErrorMessage != nil {
			m.Runs[i].ErrorMessage = in.ErrorMessage
		}
		if in.Result != nil {
			m.Runs[i].Result = in.Result
		}
		if coreworkflow.RunStatusTerminal(in.NewStatus) {
			clearRunLease(&m.Runs[i])
		}
		return true, nil
	}
	return false, nil
}

func (m *MockWorkflowStore) TransitionWorkflowNodeRun(_ context.Context, in coreworkflow.TransitionNodeRunInput) (bool, error) {
	if !coreworkflow.ValidNodeRunTransition(in.ExpectedStatus, in.NewStatus) {
		return false, fmt.Errorf("%w: %s -> %s", coreworkflow.ErrInvalidNodeRunTransition, in.ExpectedStatus, in.NewStatus)
	}
	for i := range m.NodeRuns {
		if m.NodeRuns[i].ID != in.NodeRunID {
			continue
		}
		if m.NodeRuns[i].Status != string(in.ExpectedStatus) {
			return false, nil
		}
		m.NodeRuns[i].Status = string(in.NewStatus)
		if in.TaskID != nil {
			if *in.TaskID == "" {
				m.NodeRuns[i].TaskID = nil
			} else {
				m.NodeRuns[i].TaskID = in.TaskID
			}
		}
		if in.TaskRunID != nil {
			if *in.TaskRunID == "" {
				m.NodeRuns[i].TaskRunID = nil
			} else {
				m.NodeRuns[i].TaskRunID = in.TaskRunID
			}
		}
		if in.ResolvedInput != nil {
			if *in.ResolvedInput == "" {
				m.NodeRuns[i].ResolvedInput = nil
			} else {
				m.NodeRuns[i].ResolvedInput = in.ResolvedInput
			}
		}
		if in.Output != nil {
			if *in.Output == "" {
				m.NodeRuns[i].Output = nil
			} else {
				m.NodeRuns[i].Output = in.Output
			}
		}
		if in.ErrorMessage != nil {
			if *in.ErrorMessage == "" {
				m.NodeRuns[i].ErrorMessage = nil
			} else {
				m.NodeRuns[i].ErrorMessage = in.ErrorMessage
			}
		}
		if in.StartedAt != nil {
			m.NodeRuns[i].StartedAt = in.StartedAt
		}
		if in.EndedAt != nil {
			m.NodeRuns[i].EndedAt = in.EndedAt
		}
		return true, nil
	}
	return false, nil
}

func (m *MockWorkflowStore) FinalizeFailedWorkflowRun(_ context.Context, in coreworkflow.FinalizeFailedRunInput) (bool, error) {
	if !coreworkflow.ValidNodeRunTransition(in.NodeExpected, in.NodeStatus) {
		return false, fmt.Errorf("%w: %s -> %s", coreworkflow.ErrInvalidNodeRunTransition, in.NodeExpected, in.NodeStatus)
	}
	if !coreworkflow.ValidRunStatusTransition(in.RunExpected, in.RunStatus) {
		return false, fmt.Errorf("%w: %s -> %s", coreworkflow.ErrInvalidRunTransition, in.RunExpected, in.RunStatus)
	}
	stepApplied := false
	for i := range m.NodeRuns {
		if m.NodeRuns[i].ID != in.NodeRunID {
			continue
		}
		if m.NodeRuns[i].Status != string(in.NodeExpected) {
			return false, nil
		}
		m.NodeRuns[i].Status = string(in.NodeStatus)
		if in.TaskRunID != nil && *in.TaskRunID != "" {
			m.NodeRuns[i].TaskRunID = in.TaskRunID
		}
		if in.ErrorMessage != nil {
			m.NodeRuns[i].ErrorMessage = in.ErrorMessage
		}
		if in.StartedAt != nil {
			m.NodeRuns[i].StartedAt = in.StartedAt
		}
		if in.EndedAt != nil {
			m.NodeRuns[i].EndedAt = in.EndedAt
		}
		stepApplied = true
		break
	}
	if !stepApplied {
		return false, nil
	}
	// Fail-fast: block every node still pending in this run, regardless of graph
	// position, and cancel every sibling still running, since the run is
	// terminating.
	for i := range m.NodeRuns {
		if m.NodeRuns[i].WorkflowRunID != in.WorkflowRunID {
			continue
		}
		if m.NodeRuns[i].Status == string(coreworkflow.NodeRunStatusPending) {
			m.NodeRuns[i].Status = string(coreworkflow.NodeRunStatusBlocked)
		} else if m.NodeRuns[i].Status == string(coreworkflow.NodeRunStatusRunning) && m.NodeRuns[i].ID != in.NodeRunID {
			m.NodeRuns[i].Status = string(coreworkflow.NodeRunStatusCanceled)
			m.NodeRuns[i].EndedAt = in.EndedAt
		}
	}
	for i := range m.Runs {
		if m.Runs[i].ID != in.WorkflowRunID {
			continue
		}
		if m.Runs[i].Status == string(in.RunExpected) {
			m.Runs[i].Status = string(in.RunStatus)
			if in.EndedAt != nil {
				m.Runs[i].EndedAt = in.EndedAt
			}
			if in.ErrorMessage != nil {
				m.Runs[i].ErrorMessage = in.ErrorMessage
			}
			clearRunLease(&m.Runs[i]) // in.RunStatus is always terminal here
		}
		break
	}
	return true, nil
}

// clearRunLease drops the reconciliation lease and schedule, as the store does
// when a run reaches a terminal status.
func clearRunLease(run *coreworkflow.Run) {
	run.ReconcileOwner = nil
	run.LeaseExpiresAt = nil
	run.NextReconcileAt = nil
}

func (m *MockWorkflowStore) ListDueWorkflowRuns(_ context.Context, now time.Time, limit int) ([]coreworkflow.Run, error) {
	if limit <= 0 {
		limit = 100
	}
	var out []coreworkflow.Run
	for _, run := range m.Runs {
		if coreworkflow.RunStatusTerminal(coreworkflow.RunStatus(run.Status)) {
			continue
		}
		leaseExpired := run.LeaseExpiresAt != nil && !run.LeaseExpiresAt.After(now)
		if run.NextReconcileAt == nil || !run.NextReconcileAt.After(now) || leaseExpired {
			out = append(out, run)
		}
	}
	// Oldest-due first with never-scheduled runs leading, matching the store.
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].NextReconcileAt, out[j].NextReconcileAt
		switch {
		case a == nil && b == nil:
			return out[i].ID < out[j].ID
		case a == nil:
			return true
		case b == nil:
			return false
		case a.Equal(*b):
			return out[i].ID < out[j].ID
		default:
			return a.Before(*b)
		}
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *MockWorkflowStore) ClaimWorkflowRunLease(_ context.Context, in coreworkflow.ClaimLeaseInput) (bool, error) {
	for i := range m.Runs {
		if m.Runs[i].ID != in.WorkflowRunID {
			continue
		}
		if coreworkflow.RunStatusTerminal(coreworkflow.RunStatus(m.Runs[i].Status)) {
			return false, nil
		}
		held := m.Runs[i].ReconcileOwner != nil &&
			(m.Runs[i].LeaseExpiresAt == nil || m.Runs[i].LeaseExpiresAt.After(in.Now))
		if held {
			return false, nil
		}
		owner := in.Owner
		expiry := in.LeaseExpiresAt
		m.Runs[i].ReconcileOwner = &owner
		m.Runs[i].LeaseExpiresAt = &expiry
		return true, nil
	}
	return false, nil
}

func (m *MockWorkflowStore) RenewWorkflowRunLease(_ context.Context, in coreworkflow.RenewLeaseInput) (bool, error) {
	for i := range m.Runs {
		if m.Runs[i].ID != in.WorkflowRunID {
			continue
		}
		if m.Runs[i].ReconcileOwner == nil || *m.Runs[i].ReconcileOwner != in.Owner {
			return false, nil
		}
		expiry := in.LeaseExpiresAt
		m.Runs[i].LeaseExpiresAt = &expiry
		return true, nil
	}
	return false, nil
}

func (m *MockWorkflowStore) ReleaseWorkflowRunLease(_ context.Context, in coreworkflow.ReleaseLeaseInput) (bool, error) {
	for i := range m.Runs {
		if m.Runs[i].ID != in.WorkflowRunID {
			continue
		}
		if m.Runs[i].ReconcileOwner == nil || *m.Runs[i].ReconcileOwner != in.Owner {
			return false, nil
		}
		m.Runs[i].ReconcileOwner = nil
		m.Runs[i].LeaseExpiresAt = nil
		m.Runs[i].NextReconcileAt = in.NextReconcileAt
		return true, nil
	}
	return false, nil
}

func (m *MockWorkflowStore) GetWorkflowNodeRunByTaskID(_ context.Context, taskID string) (*coreworkflow.NodeRun, error) {
	for i := range m.NodeRuns {
		if m.NodeRuns[i].TaskID != nil && *m.NodeRuns[i].TaskID == taskID {
			return &m.NodeRuns[i], nil
		}
	}
	return nil, nil
}

func (m *MockWorkflowStore) GetWorkflowNodeRunByTaskRunID(_ context.Context, taskRunID string) (*coreworkflow.NodeRun, error) {
	for i := range m.NodeRuns {
		if m.NodeRuns[i].TaskRunID != nil && *m.NodeRuns[i].TaskRunID == taskRunID {
			return &m.NodeRuns[i], nil
		}
	}
	return nil, nil
}
