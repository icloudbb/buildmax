package mock

import (
	"context"
	"errors"
	"fmt"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

// MockTaskStore is an in-memory TaskStore for tests.
type MockTaskStore struct {
	List      []coretask.Task
	ListErr   error
	Create    *coretask.Task
	CreateErr error
	// Created records what each CreateTask was asked for. The returned Task
	// drops most of it, so provenance a caller set can only be asserted here.
	Created []coretask.CreateInput
	// admissions records the task and payload fingerprint each admission key
	// resolved to, so AdmitTask replays the same idempotency the store does.
	admissions map[string]mockAdmission
	// workflowSteps holds the tasks a Workflow step admitted, which the store
	// leaves out of an Issue's own Agent runs.
	workflowSteps map[string]bool
}

type mockAdmission struct {
	taskID      string
	fingerprint string
}

func (m *MockTaskStore) ListTasksByConversation(_ context.Context, conversationID string, order string) ([]coretask.Task, error) {
	list, _, err := m.ListTasksByConversationPaginated(context.Background(), conversationID, false, 0, 0)
	if err != nil {
		return nil, err
	}
	if order == "asc" {
		for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
			list[i], list[j] = list[j], list[i]
		}
	}
	return list, nil
}

func (m *MockTaskStore) ListTasksByConversationPaginated(_ context.Context, conversationID string, executedOnly bool, limit, offset int) ([]coretask.Task, int, error) {
	if m.ListErr != nil {
		return nil, 0, m.ListErr
	}
	var filtered []coretask.Task
	for _, c := range m.List {
		if c.ConversationID != conversationID {
			continue
		}
		if executedOnly && (c.LastRunID == nil || *c.LastRunID == "") {
			continue
		}
		filtered = append(filtered, c)
	}
	for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
		filtered[i], filtered[j] = filtered[j], filtered[i]
	}
	total := len(filtered)
	if offset > len(filtered) {
		return []coretask.Task{}, total, nil
	}
	end := offset + limit
	if limit <= 0 {
		end = len(filtered)
	}
	if end > len(filtered) {
		end = len(filtered)
	}
	return filtered[offset:end], total, nil
}

func (m *MockTaskStore) ListIssueAgentTasks(_ context.Context, issueID string, limit, offset int) ([]coretask.Task, int, error) {
	if m.ListErr != nil {
		return nil, 0, m.ListErr
	}
	var filtered []coretask.Task
	for _, task := range m.List {
		if task.IssueID != nil && *task.IssueID == issueID && !m.workflowSteps[task.ID] {
			filtered = append(filtered, task)
		}
	}
	for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
		filtered[i], filtered[j] = filtered[j], filtered[i]
	}
	total := len(filtered)
	if offset > len(filtered) {
		return []coretask.Task{}, total, nil
	}
	end := offset + limit
	if limit <= 0 || end > len(filtered) {
		end = len(filtered)
	}
	return filtered[offset:end], total, nil
}

func (m *MockTaskStore) ListTasksByAgent(_ context.Context, spaceID, agentID string, limit, offset int) ([]coretask.Task, int, error) {
	if m.ListErr != nil {
		return nil, 0, m.ListErr
	}
	var filtered []coretask.Task
	for _, task := range m.List {
		if task.SpaceID == spaceID && task.AgentID != nil && *task.AgentID == agentID {
			filtered = append(filtered, task)
		}
	}
	return pageTasksNewestFirst(filtered, limit, offset)
}

func (m *MockTaskStore) ListTasksBySchedule(_ context.Context, spaceID, scheduleID string, limit, offset int) ([]coretask.Task, int, error) {
	if m.ListErr != nil {
		return nil, 0, m.ListErr
	}
	var filtered []coretask.Task
	for _, task := range m.List {
		if task.SpaceID == spaceID && task.ScheduleID != nil && *task.ScheduleID == scheduleID {
			filtered = append(filtered, task)
		}
	}
	return pageTasksNewestFirst(filtered, limit, offset)
}

// pageTasksNewestFirst reverses insertion order (newest first) and applies the
// limit/offset window, matching the store's DESC-by-created_at listings.
func pageTasksNewestFirst(filtered []coretask.Task, limit, offset int) ([]coretask.Task, int, error) {
	for i, j := 0, len(filtered)-1; i < j; i, j = i+1, j-1 {
		filtered[i], filtered[j] = filtered[j], filtered[i]
	}
	total := len(filtered)
	if offset >= total {
		return []coretask.Task{}, total, nil
	}
	end := offset + limit
	if limit <= 0 || end > total {
		end = total
	}
	return filtered[offset:end], total, nil
}

func (m *MockTaskStore) CreateTask(_ context.Context, in *coretask.CreateInput) (*coretask.Task, error) {
	if m.CreateErr != nil {
		return nil, m.CreateErr
	}
	if m.Create != nil {
		return m.Create, nil
	}
	if in == nil {
		return nil, nil
	}
	m.Created = append(m.Created, *in)
	id := len(m.List) + 1
	taskID := fmt.Sprintf("t_mock_%d", id)
	lastRunID := fmt.Sprintf("r_mock_%d", id)
	task := &coretask.Task{
		ID:                taskID,
		ConversationID:    in.ConversationID,
		SpaceID:           in.SpaceID,
		Status:            "PENDING",
		Input:             in.Input,
		Title:             in.Title,
		CreatedBy:         in.CreatedBy,
		RequestedBy:       in.RequestedBy,
		AssistantID:       in.AssistantID,
		AssistantRevision: in.AssistantRevision,
		CreatedAt:         seqTime(12345),
		AgentID:           in.AgentID,
		IssueID:           in.IssueID,
	}
	task.LastRunID = &lastRunID
	m.List = append(m.List, *task)
	return task, nil
}

func (m *MockTaskStore) AdmitTask(ctx context.Context, in *coretask.CreateInput) (*coretask.Task, error) {
	if m.CreateErr != nil {
		return nil, m.CreateErr
	}
	if in == nil {
		return nil, nil
	}
	if in.AdmissionKey == "" {
		return nil, errors.New("AdmitTask requires an admission key")
	}
	fingerprint := coretask.AdmissionFingerprint(in)
	key := in.SpaceID + "\x00" + in.AdmissionKey
	if prev, ok := m.admissions[key]; ok {
		if prev.fingerprint != fingerprint {
			return nil, coretask.ErrTaskAdmissionConflict
		}
		return m.GetTask(ctx, prev.taskID)
	}
	task, err := m.CreateTask(ctx, in)
	if err != nil {
		return nil, err
	}
	if m.admissions == nil {
		m.admissions = map[string]mockAdmission{}
	}
	m.admissions[key] = mockAdmission{taskID: task.ID, fingerprint: fingerprint}
	if in.WorkflowNodeRunID != "" {
		if m.workflowSteps == nil {
			m.workflowSteps = map[string]bool{}
		}
		m.workflowSteps[task.ID] = true
	}
	return task, nil
}

func (m *MockTaskStore) GetTaskBySessionID(_ context.Context, sessionID string) (*coretask.Task, error) {
	for i := range m.List {
		if m.List[i].SessionID != nil && *m.List[i].SessionID == sessionID {
			return &m.List[i], nil
		}
	}
	return nil, nil
}

func (m *MockTaskStore) UpdateTask(_ context.Context, in coretask.UpdateInput) error {
	for i := range m.List {
		if m.List[i].ID == in.TaskID {
			m.List[i].Status = in.Status
			if in.StartedAt != nil {
				m.List[i].StartedAt = in.StartedAt
			}
			if in.EndedAt != nil {
				m.List[i].EndedAt = in.EndedAt
			}
			if in.Output != nil {
				m.List[i].Output = in.Output
			}
			if in.ErrorMessage != nil {
				m.List[i].ErrorMessage = in.ErrorMessage
			}
			if in.SessionID != nil {
				m.List[i].SessionID = in.SessionID
			}
			return nil
		}
	}
	return nil
}

func (m *MockTaskStore) ClaimTask(_ context.Context, in coretask.ClaimInput) (bool, error) {
	for i := range m.List {
		if m.List[i].ID == in.TaskID && m.List[i].Status == in.ExpectedStatus {
			m.List[i].Status = in.NewStatus
			if in.StartedAt != nil {
				m.List[i].StartedAt = in.StartedAt
			}
			if in.EndedAt != nil {
				m.List[i].EndedAt = in.EndedAt
			}
			if in.Output != nil {
				m.List[i].Output = in.Output
			}
			if in.ErrorMessage != nil {
				m.List[i].ErrorMessage = in.ErrorMessage
			}
			if in.SessionID != nil {
				m.List[i].SessionID = in.SessionID
			}
			return true, nil
		}
	}
	return false, nil
}

func (m *MockTaskStore) GetTask(_ context.Context, taskID string) (*coretask.Task, error) {
	for i := range m.List {
		if m.List[i].ID == taskID {
			return &m.List[i], nil
		}
	}
	return nil, nil
}
