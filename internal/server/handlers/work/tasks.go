package work

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/icloudbb/buildmax/internal/core/apierr"
	"log/slog"
	"net/http"
	"os"
	"time"

	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/server/httputil"
	"github.com/icloudbb/buildmax/internal/service/task"
)

type TaskResponse struct {
	ID             string     `json:"id"`
	SpaceID        string     `json:"space_id"`
	ConversationID string     `json:"conversation_id,omitempty"`
	SessionID      *string    `json:"session_id,omitempty"`
	Status         string     `json:"status"`
	Input          string     `json:"input"`
	Title          string     `json:"title,omitempty"`
	Output         *string    `json:"output,omitempty"`
	CreatedBy      string     `json:"created_by"`
	CreatedAt      time.Time  `json:"created_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	EndedAt        *time.Time `json:"ended_at,omitempty"`
	ErrorMessage   *string    `json:"error_message,omitempty"`
	AgentID        *string    `json:"agent_id,omitempty"`
	IssueID        *string    `json:"issue_id,omitempty"`
	// LastRunID names the run behind the task's current status. The run-scoped
	// routes -- trace, LLM calls -- are keyed by it, so a caller that can see a
	// task can reach what that task actually did.
	LastRunID *string `json:"last_run_id,omitempty"`
	// WorkflowRunID names the workflow run that dispatched this task, when the
	// task carries neither an IssueID nor a ConversationID of its own. A
	// workflow step task's only origin is its step run, so without this a
	// caller has no way to tell "started by a workflow" from "started with no
	// recorded origin at all." Resolved only for the single-task read: listing
	// endpoints would otherwise pay one lookup per row.
	WorkflowRunID *string `json:"workflow_run_id,omitempty"`
}

type createTaskRequest struct {
	Input   string  `json:"input"`
	AgentID *string `json:"agent_id,omitempty"`
}

func taskToResponse(task coretask.Task) TaskResponse {
	return TaskResponse{
		ID:             task.ID,
		SpaceID:        task.SpaceID,
		ConversationID: task.ConversationID,
		SessionID:      task.SessionID,
		Status:         task.Status,
		Input:          task.Input,
		Title:          task.Title,
		Output:         task.Output,
		CreatedBy:      task.CreatedBy,
		CreatedAt:      task.CreatedAt,
		StartedAt:      task.StartedAt,
		EndedAt:        task.EndedAt,
		ErrorMessage:   task.ErrorMessage,
		AgentID:        task.AgentID,
		IssueID:        task.IssueID,
		LastRunID:      task.LastRunID,
	}
}

func (h *Handler) createSpaceTaskHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Tasks, "tasks not configured")
	if !ok {
		return
	}
	var req createTaskRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	h.createDirectTask(w, r, userID, spaceID, req)
}

func (h *Handler) createAgentTaskHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Tasks, "tasks not configured")
	if !ok {
		return
	}
	agentID, ok := httputil.PathValue(w, r, "agent_id")
	if !ok {
		return
	}
	var req createTaskRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	req.AgentID = &agentID
	h.createDirectTask(w, r, userID, spaceID, req)
}

func (h *Handler) createDirectTask(w http.ResponseWriter, r *http.Request, userID, spaceID string, req createTaskRequest) {
	created, err := h.taskService().CreateTask(r.Context(), task.CreateTaskCmd{
		UserID: userID, SpaceID: spaceID, Input: req.Input, AgentID: req.AgentID,
		CreatedByType: coretask.RunCreatedByTypeUser,
		TriggerSource: coretask.RunTriggerSourcePortalTaskCreate,
	})
	if err != nil {
		if h.writeTaskServiceError(w, r, err, req.AgentID) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "create_direct_task", "space_id", spaceID)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, taskToResponse(*created))
}

func (h *Handler) listAgentTasksHandler(w http.ResponseWriter, r *http.Request) {
	_, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Tasks, "tasks not configured")
	if !ok {
		return
	}
	agentID, ok := httputil.PathValue(w, r, "agent_id")
	if !ok {
		return
	}
	if h.cfg.Agents == nil {
		httputil.WriteJSONError(w, http.StatusServiceUnavailable, "agents not configured")
		return
	}
	agent, err := h.cfg.Agents.GetAgent(r.Context(), agentID)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "list_agent_tasks", "agent_id", agentID)
		return
	}
	if agent == nil || agent.SpaceID != spaceID {
		httputil.WriteJSONError(w, http.StatusNotFound, "agent not found")
		return
	}
	limit, offset := httputil.LimitOffset(r.URL.Query(), "limit", "offset", httputil.BulkPageDefault, httputil.BulkPageMax)
	list, total, err := h.cfg.Tasks.ListTasksByAgent(r.Context(), spaceID, agentID, limit, offset)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "list_agent_tasks", "agent_id", agentID)
		return
	}
	out := make([]TaskResponse, len(list))
	for i := range list {
		out[i] = taskToResponse(list[i])
	}
	httputil.WriteJSON(w, http.StatusOK, tasksListResponse{Tasks: out, Total: total})
}

func (h *Handler) listScheduleTasksHandler(w http.ResponseWriter, r *http.Request) {
	_, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Tasks, "tasks not configured")
	if !ok {
		return
	}
	scheduleID, ok := httputil.PathValue(w, r, "schedule_id")
	if !ok {
		return
	}
	if h.cfg.Schedules == nil {
		httputil.WriteJSONError(w, http.StatusServiceUnavailable, "schedules not configured")
		return
	}
	sched, err := h.cfg.Schedules.GetSchedule(r.Context(), scheduleID)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "list_schedule_tasks", "schedule_id", scheduleID)
		return
	}
	if sched == nil || sched.SpaceID != spaceID {
		httputil.WriteJSONError(w, http.StatusNotFound, "schedule not found")
		return
	}
	limit, offset := httputil.LimitOffset(r.URL.Query(), "limit", "offset", httputil.BulkPageDefault, httputil.BulkPageMax)
	list, total, err := h.cfg.Tasks.ListTasksBySchedule(r.Context(), spaceID, scheduleID, limit, offset)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "list_schedule_tasks", "schedule_id", scheduleID)
		return
	}
	out := make([]TaskResponse, len(list))
	for i := range list {
		out[i] = taskToResponse(list[i])
	}
	httputil.WriteJSON(w, http.StatusOK, tasksListResponse{Tasks: out, Total: total})
}

func (h *Handler) taskService() *task.Service {
	return h.tasks
}

func newTaskService(cfg Config) *task.Service {
	var quotaChecker task.QuotaChecker
	if cfg.Quota != nil {
		quotaChecker = cfg.Quota
	}
	var workflowSteps task.WorkflowStepLookup
	if cfg.Workflows != nil {
		workflowSteps = cfg.Workflows
	}
	return &task.Service{
		Agents:         cfg.Agents,
		Tasks:          cfg.Tasks,
		TaskRuns:       cfg.TaskRuns,
		QuotaChecker:   quotaChecker,
		TitleGenerator: cfg.TitleGenerator,
		WorkflowSteps:  workflowSteps,
	}
}

// writeTaskServiceError answers a task-service refusal.
//
// agentID is still a parameter because one case is genuinely ambiguous: with no
// agent named in the request, "agent not found" means the task was not found.
func (h *Handler) writeTaskServiceError(w http.ResponseWriter, r *http.Request, err error, agentID *string) bool {
	if errors.Is(err, task.ErrAgentNotFound) && (agentID == nil || *agentID == "") {
		httputil.WriteJSONError(w, http.StatusNotFound, "task not found")
		return true
	}
	return httputil.WriteServiceError(w, err)
}

func (h *Handler) getTaskForSpace(w http.ResponseWriter, r *http.Request, spaceID, taskID string) (*coretask.Task, *coreconv.Conversation, bool) {
	task, err := h.cfg.Tasks.GetTask(r.Context(), taskID)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "get_task", "task_id", taskID)
		return nil, nil, false
	}
	if task == nil {
		httputil.WriteJSONError(w, http.StatusNotFound, "task not found")
		return nil, nil, false
	}
	if task.SpaceID != spaceID {
		httputil.WriteJSONError(w, http.StatusNotFound, "task not found")
		return nil, nil, false
	}
	return task, nil, true
}

type tasksListResponse struct {
	Tasks []TaskResponse `json:"tasks"`
	Total int            `json:"total"`
}

func (h *Handler) listConversationTasksHandler(w http.ResponseWriter, r *http.Request) {
	_, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Tasks, "tasks not configured")
	if !ok {
		return
	}
	conversationID, ok := httputil.PathValue(w, r, "conversation_id")
	if !ok {
		return
	}
	if _, ok = h.getConversationForSpace(w, r, spaceID, conversationID); !ok {
		return
	}
	q := r.URL.Query()
	usePaginated := q.Has("limit") || q.Has("offset") || q.Get("executed_only") == "true"
	if usePaginated {
		limit, offset := httputil.LimitOffset(q, "limit", "offset", httputil.BulkPageDefault, httputil.BulkPageMax)
		executedOnly := q.Get("executed_only") == "true"
		list, total, err := h.cfg.Tasks.ListTasksByConversationPaginated(r.Context(), conversationID, executedOnly, limit, offset)
		if err != nil {
			httputil.WriteInternalError(w, err, "handler error", "handler", "list_tasks", "conversation_id", conversationID)
			return
		}
		out := h.conversationTaskResponses(list)
		httputil.WriteJSON(w, http.StatusOK, tasksListResponse{Tasks: out, Total: total})
		return
	}
	order := q.Get("order")
	if order != "asc" && order != "desc" {
		order = "desc"
	}
	list, err := h.cfg.Tasks.ListTasksByConversation(r.Context(), conversationID, order)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "list_tasks", "conversation_id", conversationID)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, h.conversationTaskResponses(list))
}

// conversationTaskResponses answers a conversation's tasks with what a card
// needs to stand on its own: the run behind each status.
func (h *Handler) conversationTaskResponses(list []coretask.Task) []TaskResponse {
	out := make([]TaskResponse, len(list))
	for i := range list {
		out[i] = taskToResponse(list[i])
	}
	return out
}

func (h *Handler) createConversationTaskHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Tasks, "tasks not configured")
	if !ok {
		return
	}
	conversationID, ok := httputil.PathValue(w, r, "conversation_id")
	if !ok {
		return
	}
	if _, ok = h.getConversationForSpace(w, r, spaceID, conversationID); !ok {
		return
	}
	var req createTaskRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	createdTask, err := h.taskService().CreateTask(r.Context(), task.CreateTaskCmd{
		ConversationID: conversationID,
		UserID:         userID,
		SpaceID:        spaceID,
		Input:          req.Input,
		AgentID:        req.AgentID,
		CreatedByType:  coretask.RunCreatedByTypeUser,
		TriggerSource:  coretask.RunTriggerSourcePortalTaskCreate,
	})
	if err != nil {
		if h.writeTaskServiceError(w, r, err, req.AgentID) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "create_task", "conversation_id", conversationID)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, taskToResponse(*createdTask))
}

func (h *Handler) getTaskHandler(w http.ResponseWriter, r *http.Request) {
	_, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Tasks, "tasks not configured")
	if !ok {
		return
	}
	taskID, ok := httputil.PathValue(w, r, "task_id")
	if !ok {
		return
	}
	target, _, ok := h.getTaskForSpace(w, r, spaceID, taskID)
	if !ok {
		return
	}
	out := taskToResponse(*target)
	out.WorkflowRunID = h.resolveTaskWorkflowRunID(r.Context(), target)
	httputil.WriteJSON(w, http.StatusOK, out)
}

// resolveTaskWorkflowRunID finds the workflow run that dispatched a task with
// no Issue or Conversation of its own. A task with either already has a true
// origin to navigate through; only the workflow case needs this extra lookup.
func (h *Handler) resolveTaskWorkflowRunID(ctx context.Context, t *coretask.Task) *string {
	if h.cfg.Workflows == nil || (t.IssueID != nil && *t.IssueID != "") || t.ConversationID != "" {
		return nil
	}
	step, err := h.cfg.Workflows.GetWorkflowNodeRunByTaskID(ctx, t.ID)
	if err != nil || step == nil {
		return nil
	}
	return &step.WorkflowRunID
}

type createTaskRunRequest struct {
	Input string `json:"input"`
	// IdempotencyKey lets a client that cannot tell whether its Continue
	// request landed retry safely: a repeat with the same key on the same task
	// returns the run the first request created instead of starting a second
	// one. Optional; omitting it creates a new run unconditionally, as before.
	IdempotencyKey *string `json:"idempotency_key,omitempty"`
}

func (h *Handler) createTaskRunHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.TaskRuns, "task runs not configured")
	if !ok {
		return
	}
	taskID, ok := httputil.PathValue(w, r, "task_id")
	if !ok {
		return
	}
	target, _, ok := h.getTaskForSpace(w, r, spaceID, taskID)
	if !ok {
		return
	}
	var req createTaskRunRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	if req.Input == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "input required")
		return
	}
	run, err := h.taskService().CreateRun(r.Context(), task.CreateRunCmd{
		UserID: userID, TaskID: target.ID, Input: req.Input,
		CreatedByType:  coretask.RunCreatedByTypeUser,
		TriggerSource:  coretask.RunTriggerSourcePortalTaskRerun,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		if h.writeTaskServiceError(w, r, err, target.AgentID) {
			return
		}
		if errors.Is(err, coretask.ErrRunInProgress) {
			httputil.WriteJSONError(w, http.StatusConflict, "a run is already in progress for this task")
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "continue_task", "task_id", target.ID)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, run)
}

func (h *Handler) listTaskRunsHandler(w http.ResponseWriter, r *http.Request) {
	_, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.TaskRuns, "task runs not configured")
	if !ok {
		return
	}
	taskID, ok := httputil.PathValue(w, r, "task_id")
	if !ok {
		return
	}
	target, _, ok := h.getTaskForSpace(w, r, spaceID, taskID)
	if !ok {
		return
	}
	runs, err := h.cfg.TaskRuns.ListTaskRunsByTask(r.Context(), target.ID)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "list_task_runs", "task_id", taskID)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]interface{}{"runs": runs})
}

// retryTaskResponse names the new run and the one it repeats, so a caller that
// reloads a task can tell which of its runs is the retry.
type retryTaskResponse struct {
	TaskID           string `json:"task_id"`
	TaskRunID        string `json:"task_run_id"`
	RetryOfTaskRunID string `json:"retry_of_task_run_id"`
	Status           string `json:"status"`
}

// retryTaskHandler runs the task's most recent run again, with the same input.
//
// Retry exists because the common reason a run has to be repeated — a worker
// that died, an expired credential, a model that timed out — has nothing to do
// with what the run was asked to do, and making someone retype the instructions
// to recover from it invites them to retype them differently.
func (h *Handler) retryTaskHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.TaskRuns, "task runs not configured")
	if !ok {
		return
	}
	taskID, ok := httputil.PathValue(w, r, "task_id")
	if !ok {
		return
	}
	target, _, ok := h.getTaskForSpace(w, r, spaceID, taskID)
	if !ok {
		return
	}
	result, err := h.taskService().RetryRun(r.Context(), task.RetryRunCmd{UserID: userID, TaskID: target.ID})
	if err != nil {
		if h.writeTaskServiceError(w, r, err, nil) {
			return
		}
		// coretask.ErrRunInProgress comes from the store, below the service, so it
		// carries no Kind of its own.
		if errors.Is(err, coretask.ErrRunInProgress) {
			httputil.WriteJSONError(w, http.StatusConflict, "a run is already in progress for this task")
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "retry_task", "task_id", taskID)
		return
	}
	slog.Info("task run retried", "task_id", target.ID, "task_run_id", result.Run.ID, "retry_of_task_run_id", result.RetriedRun.ID, "user_id", userID)
	httputil.WriteJSON(w, http.StatusCreated, retryTaskResponse{
		TaskID:           target.ID,
		TaskRunID:        result.Run.ID,
		RetryOfTaskRunID: result.RetriedRun.ID,
		Status:           result.Run.Status,
	})
}

// cancelTaskResponse reports what the cancel did.
//
// The two outcomes are genuinely different and the caller has to tell them
// apart: a run that had not started is over when this returns, while a started
// one is only asked to stop and keeps running until its worker confirms.
type cancelTaskResponse struct {
	TaskID    string `json:"task_id"`
	TaskRunID string `json:"task_run_id"`
	// Status is the run's status now, not the one it is heading for.
	Status string `json:"status"`
	// CancelRequested is true while the run is still executing and the stop is
	// pending its worker.
	CancelRequested bool `json:"cancel_requested"`
}

// cancelTaskHandler stops the task's in-flight run.
//
// A run that has not been dispatched is canceled outright: no worker holds it,
// so nothing has to agree. A run already with a worker is asked instead, and the
// worker ends it — the server has no way to reach into another process's agent
// loop, and pretending otherwise would leave the run's own record lying about
// what it was doing. `StaleRunReaper` finishes the ones no worker answers for.
func (h *Handler) cancelTaskHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.TaskRuns, "task runs not configured")
	if !ok {
		return
	}
	taskID, ok := httputil.PathValue(w, r, "task_id")
	if !ok {
		return
	}
	target, _, ok := h.getTaskForSpace(w, r, spaceID, taskID)
	if !ok {
		return
	}
	run, err := h.cfg.TaskRuns.GetActiveTaskRunByTask(r.Context(), target.ID)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "cancel_task", "task_id", taskID)
		return
	}
	if run == nil {
		httputil.WriteJSONError(w, http.StatusConflict, "this task has no run in progress")
		return
	}

	// Record the request first. It names who asked and starts the clock the
	// backstop measures, and it stays true whichever of the two paths below the
	// run turns out to be on.
	now := time.Now().UTC()
	finished, err := h.taskService().RequestRunCancel(r.Context(), run.ID, userID, coretask.CancelReasonUserRequested, now)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "cancel_task", "task_run_id", run.ID)
		return
	}
	if finished {
		message := "this run was canceled before it started"
		h.runAnnouncer().Announce(r.Context(), run.ID, string(coretask.RunStatusCanceled), nil, &message)
		httputil.WriteJSON(w, http.StatusOK, cancelTaskResponse{
			TaskID:    target.ID,
			TaskRunID: run.ID,
			Status:    string(coretask.RunStatusCanceled),
		})
		return
	}

	// Not PENDING any more: either a worker has it, or it finished while this
	// request was in flight. Re-reading is what tells those apart.
	current, err := h.cfg.TaskRuns.GetTaskRun(r.Context(), run.ID)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "cancel_task", "task_run_id", run.ID)
		return
	}
	if current == nil || coretask.RunStatusTerminal(current.Status) {
		httputil.WriteJSONError(w, http.StatusConflict, "this run has already finished")
		return
	}
	if current.CancelRequestedAt == nil {
		httputil.WriteJSONError(w, http.StatusConflict, "this run could not be canceled")
		return
	}
	slog.Info("cancel requested for a running task", "task_id", target.ID, "task_run_id", current.ID, "status", current.Status, "user_id", userID)
	httputil.WriteJSON(w, http.StatusAccepted, cancelTaskResponse{
		TaskID:          target.ID,
		TaskRunID:       current.ID,
		Status:          current.Status,
		CancelRequested: true,
	})
}

type SessionMessage struct {
	Role       string            `json:"role"`
	Content    string            `json:"content,omitempty"`
	ToolCallID string            `json:"tool_call_id,omitempty"`
	ToolCalls  []SessionToolCall `json:"tool_calls,omitempty"`
}

type SessionToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
}

type ConversationResponse struct {
	ID        string           `json:"id"`
	Title     string           `json:"title,omitempty"`
	CreatedAt string           `json:"created_at"`
	Messages  []SessionMessage `json:"messages,omitempty"`
}

func (h *Handler) getTaskConversationHandler(w http.ResponseWriter, r *http.Request) {
	_, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Tasks, "tasks not configured")
	if !ok {
		return
	}
	taskID := r.PathValue("task_id")
	if taskID == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "task_id required")
		return
	}
	task, _, ok := h.getTaskForSpace(w, r, spaceID, taskID)
	if !ok {
		return
	}
	if task.SessionID == nil || *task.SessionID == "" {
		httputil.WriteJSONError(w, http.StatusNotFound, "conversation not found")
		return
	}
	if task.LastRunID == nil || *task.LastRunID == "" {
		httputil.WriteJSONError(w, http.StatusNotFound, "conversation not found")
		return
	}
	sessionID := *task.SessionID
	lastRunID := *task.LastRunID
	data, err := h.loadTaskConversationData(r.Context(), task, lastRunID, sessionID)
	if err != nil {
		if os.IsNotExist(err) || errors.Is(err, apierr.ErrNotFound) {
			httputil.WriteJSONError(w, http.StatusNotFound, "conversation file not found")
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "get_conversation", "task_id", task.ID)
		return
	}
	var out ConversationResponse
	if err := json.Unmarshal(data, &out); err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "get_conversation", "task_id", task.ID)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) loadTaskConversationData(ctx context.Context, task *coretask.Task, lastRunID, sessionID string) ([]byte, error) {
	return h.readRunGlobal(ctx, task, lastRunID, "sessions/"+sessionID+".json")
}
