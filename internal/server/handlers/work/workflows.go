package work

import (
	"encoding/json"
	"net/http"
	"time"

	corespace "github.com/icloudbb/buildmax/internal/core/space"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/server/httputil"
	"github.com/icloudbb/buildmax/internal/service/task"
	"github.com/icloudbb/buildmax/internal/service/workflow"
)

type workflowResponse struct {
	ID          string    `json:"id"`
	SpaceID     string    `json:"space_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Definition  string    `json:"definition"`
	Status      string    `json:"status"`
	Revision    int       `json:"revision"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type workflowRevisionResponse struct {
	ID          string    `json:"id"`
	WorkflowID  string    `json:"workflow_id"`
	Revision    int       `json:"revision"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Definition  string    `json:"definition"`
	Status      string    `json:"status"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

type workflowRevisionListResponse struct {
	Revisions []workflowRevisionResponse `json:"revisions"`
	Total     int                        `json:"total"`
}

type workflowRunResponse struct {
	ID               string          `json:"id"`
	WorkflowID       string          `json:"workflow_id"`
	WorkflowRevision int             `json:"workflow_revision,omitempty"`
	IssueID          *string         `json:"issue_id,omitempty"`
	ScheduleID       *string         `json:"schedule_id,omitempty"`
	Status           string          `json:"status"`
	CreatedBy        string          `json:"created_by"`
	CreatedAt        time.Time       `json:"created_at"`
	StartedAt        *time.Time      `json:"started_at,omitempty"`
	EndedAt          *time.Time      `json:"ended_at,omitempty"`
	ErrorMessage     *string         `json:"error_message,omitempty"`
	Input            json.RawMessage `json:"input,omitempty"`
	Result           json.RawMessage `json:"result,omitempty"`
}

type workflowNodeRunResponse struct {
	ID                string     `json:"id"`
	WorkflowRunID     string     `json:"workflow_run_id"`
	NodeID            string     `json:"node_id"`
	NodeIndex         int        `json:"node_index"`
	NodeType          string     `json:"node_type"`
	Needs             []string   `json:"needs,omitempty"`
	IssueAccess       string     `json:"issue_access,omitempty"`
	TargetAgentID     *string    `json:"target_agent_id,omitempty"`
	AgentName         string     `json:"agent_name,omitempty"`
	AgentDescription  string     `json:"agent_description,omitempty"`
	AgentInstructions string     `json:"agent_instructions,omitempty"`
	AgentRevision     int        `json:"agent_revision,omitempty"`
	Prompt            string     `json:"prompt"`
	Status            string     `json:"status"`
	TaskID            *string    `json:"task_id,omitempty"`
	TaskRunID         *string    `json:"task_run_id,omitempty"`
	ResolvedInput     *string    `json:"resolved_input,omitempty"`
	Output            *string    `json:"output,omitempty"`
	ErrorMessage      *string    `json:"error_message,omitempty"`
	CreatedAt         time.Time  `json:"created_at"`
	StartedAt         *time.Time `json:"started_at,omitempty"`
	EndedAt           *time.Time `json:"ended_at,omitempty"`
}

type workflowListResponse struct {
	Workflows []workflowResponse `json:"workflows"`
}

type workflowRunListResponse struct {
	Runs  []workflowRunResponse `json:"runs"`
	Total int                   `json:"total"`
}

type workflowRunDetailResponse struct {
	Run   workflowRunResponse       `json:"run"`
	Steps []workflowNodeRunResponse `json:"steps"`
}

type createWorkflowRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Definition  string `json:"definition"`
}

type patchWorkflowRequest struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
	Definition  *string `json:"definition"`
	Status      *string `json:"status"`
}

type createWorkflowRunRequest struct {
	IssueID *string `json:"issue_id,omitempty"`
	// Input is the run's input, validated against the workflow's input_schema.
	Input json.RawMessage `json:"input,omitempty"`
}

func workflowToResponse(workflow coreworkflow.Workflow) workflowResponse {
	return workflowResponse{
		ID:          workflow.ID,
		SpaceID:     workflow.SpaceID,
		Name:        workflow.Name,
		Description: workflow.Description,
		Definition:  workflow.Definition,
		Status:      workflow.Status,
		Revision:    workflow.Revision,
		CreatedBy:   workflow.CreatedBy,
		CreatedAt:   workflow.CreatedAt,
		UpdatedAt:   workflow.UpdatedAt,
	}
}

func workflowRevisionToResponse(rev coreworkflow.Revision) workflowRevisionResponse {
	return workflowRevisionResponse{
		WorkflowID:  rev.WorkflowID,
		Revision:    rev.Revision,
		Name:        rev.Name,
		Description: rev.Description,
		Definition:  rev.Definition,
		Status:      rev.Status,
		CreatedBy:   rev.CreatedBy,
		CreatedAt:   rev.CreatedAt,
	}
}

func workflowRunToResponse(run coreworkflow.Run) workflowRunResponse {
	return workflowRunResponse{
		ID:               run.ID,
		WorkflowID:       run.WorkflowID,
		WorkflowRevision: run.WorkflowRevision,
		IssueID:          run.IssueID,
		ScheduleID:       run.ScheduleID,
		Status:           run.Status,
		CreatedBy:        run.CreatedBy,
		CreatedAt:        run.CreatedAt,
		StartedAt:        run.StartedAt,
		EndedAt:          run.EndedAt,
		ErrorMessage:     run.ErrorMessage,
		Input:            rawJSONOrNil(run.Input),
		Result:           rawJSONOrNil(run.Result),
	}
}

// rawJSONOrNil surfaces a run's stored input JSON as raw JSON, not a quoted
// string, and omits it entirely when the run carried no input.
func rawJSONOrNil(s *string) json.RawMessage {
	if s == nil {
		return nil
	}
	return json.RawMessage(*s)
}

func workflowNodeRunToResponse(step coreworkflow.NodeRun) workflowNodeRunResponse {
	return workflowNodeRunResponse{
		ID:                step.ID,
		WorkflowRunID:     step.WorkflowRunID,
		NodeID:            step.NodeID,
		NodeIndex:         step.NodeIndex,
		NodeType:          step.NodeType,
		Needs:             step.Needs,
		IssueAccess:       step.IssueAccess,
		TargetAgentID:     step.TargetAgentID,
		AgentName:         step.AgentName,
		AgentDescription:  step.AgentDescription,
		AgentInstructions: step.AgentInstructions,
		AgentRevision:     step.AgentRevision,
		Prompt:            step.Prompt,
		Status:            step.Status,
		TaskID:            step.TaskID,
		TaskRunID:         step.TaskRunID,
		ResolvedInput:     step.ResolvedInput,
		Output:            step.Output,
		ErrorMessage:      step.ErrorMessage,
		CreatedAt:         step.CreatedAt,
		StartedAt:         step.StartedAt,
		EndedAt:           step.EndedAt,
	}
}

func (h *Handler) workflowService() *workflow.Service {
	return h.workflows
}

func newWorkflowService(cfg Config, tasks *task.Service) *workflow.Service {
	return &workflow.Service{
		Workflows:   cfg.Workflows,
		Agents:      cfg.Agents,
		Issues:      cfg.Issues,
		TaskService: tasks,
		TaskRuns:    tasks.TaskRuns,
		// Artifacts is left nil here: this service dispatches only the run's first
		// step from the HTTP path, and a first step has no predecessor to bind a
		// node output or Artifact from. Reconciliation of later steps -- which can
		// bind Artifacts -- runs on the terminal-callback and recovery services,
		// which are wired with the artifact store.
		Audit: cfg.Audit,
	}
}

func (h *Handler) writeWorkflowSvcError(w http.ResponseWriter, err error) bool {
	return httputil.WriteServiceError(w, err)
}

func (h *Handler) listWorkflowsHandler(w http.ResponseWriter, r *http.Request) {
	_, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Workflows, "workflows not configured")
	if !ok {
		return
	}
	list, err := h.workflowService().ListWorkflows(r.Context(), spaceID)
	if err != nil {
		if h.writeWorkflowSvcError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "list_workflows", "space_id", spaceID)
		return
	}
	out := make([]workflowResponse, len(list))
	for i := range list {
		out[i] = workflowToResponse(list[i])
	}
	httputil.WriteJSON(w, http.StatusOK, workflowListResponse{Workflows: out})
}

func (h *Handler) createWorkflowHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Workflows, "workflows not configured")
	if !ok {
		return
	}
	if _, ok := h.guard().SpaceAction(w, r, userID, spaceID, corespace.ActionManageWorkflows); !ok {
		return
	}
	var req createWorkflowRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	createdWorkflow, err := h.workflowService().CreateWorkflow(r.Context(), workflow.CreateWorkflowCmd{
		SpaceID:     spaceID,
		UserID:      userID,
		Name:        req.Name,
		Description: req.Description,
		Definition:  req.Definition,
	})
	if err != nil {
		if h.writeWorkflowSvcError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "create_workflow", "space_id", spaceID)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, workflowToResponse(*createdWorkflow))
}

func (h *Handler) getWorkflowHandler(w http.ResponseWriter, r *http.Request) {
	_, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Workflows, "workflows not configured")
	if !ok {
		return
	}
	workflowID, ok := httputil.PathValue(w, r, "workflow_id")
	if !ok {
		return
	}
	workflow, err := h.workflowService().GetWorkflow(r.Context(), spaceID, workflowID)
	if err != nil {
		if h.writeWorkflowSvcError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "get_workflow", "space_id", spaceID, "workflow_id", workflowID)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, workflowToResponse(*workflow))
}

func (h *Handler) patchWorkflowHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Workflows, "workflows not configured")
	if !ok {
		return
	}
	if _, ok := h.guard().SpaceAction(w, r, userID, spaceID, corespace.ActionManageWorkflows); !ok {
		return
	}
	workflowID, ok := httputil.PathValue(w, r, "workflow_id")
	if !ok {
		return
	}
	var req patchWorkflowRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	updatedWorkflow, err := h.workflowService().UpdateWorkflow(r.Context(), workflow.UpdateWorkflowCmd{
		SpaceID:     spaceID,
		UserID:      userID,
		WorkflowID:  workflowID,
		Name:        req.Name,
		Description: req.Description,
		Definition:  req.Definition,
		Status:      req.Status,
	})
	if err != nil {
		if h.writeWorkflowSvcError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "patch_workflow", "space_id", spaceID, "workflow_id", workflowID)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, workflowToResponse(*updatedWorkflow))
}

// listWorkflowRevisionsHandler returns a workflow's recorded versions, newest
// first. Reading history needs no more than space membership.
func (h *Handler) listWorkflowRevisionsHandler(w http.ResponseWriter, r *http.Request) {
	_, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Workflows, "workflows not configured")
	if !ok {
		return
	}
	workflowID, ok := httputil.PathValue(w, r, "workflow_id")
	if !ok {
		return
	}
	limit, offset := httputil.LimitOffset(r.URL.Query(), "limit", "offset", httputil.BrowsePageDefault, httputil.BrowsePageMax)
	list, total, err := h.workflowService().ListWorkflowRevisions(r.Context(), spaceID, workflowID, limit, offset)
	if err != nil {
		if h.writeWorkflowSvcError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "list_workflow_revisions", "space_id", spaceID, "workflow_id", workflowID)
		return
	}
	out := make([]workflowRevisionResponse, len(list))
	for i := range list {
		out[i] = workflowRevisionToResponse(list[i])
	}
	httputil.WriteJSON(w, http.StatusOK, workflowRevisionListResponse{Revisions: out, Total: total})
}

// restoreWorkflowRevisionHandler writes an earlier revision's content back to
// the workflow. It is an ordinary edit and needs the permission an edit needs.
func (h *Handler) restoreWorkflowRevisionHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Workflows, "workflows not configured")
	if !ok {
		return
	}
	if _, ok := h.guard().SpaceAction(w, r, userID, spaceID, corespace.ActionManageWorkflows); !ok {
		return
	}
	workflowID, ok := httputil.PathValue(w, r, "workflow_id")
	if !ok {
		return
	}
	revision, ok := httputil.PathValueInt(w, r, "revision")
	if !ok {
		return
	}
	updated, err := h.workflowService().RestoreWorkflowRevision(r.Context(), workflow.RestoreWorkflowRevisionCmd{
		SpaceID:    spaceID,
		UserID:     userID,
		WorkflowID: workflowID,
		Revision:   revision,
	})
	if err != nil {
		if h.writeWorkflowSvcError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "restore_workflow_revision", "space_id", spaceID, "workflow_id", workflowID)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, workflowToResponse(*updated))
}

func (h *Handler) listWorkflowRunsHandler(w http.ResponseWriter, r *http.Request) {
	_, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Workflows, "workflows not configured")
	if !ok {
		return
	}
	workflowID, ok := httputil.PathValue(w, r, "workflow_id")
	if !ok {
		return
	}
	limit, offset := httputil.LimitOffset(r.URL.Query(), "limit", "offset", httputil.BrowsePageDefault, httputil.BrowsePageMax)
	runs, total, err := h.workflowService().ListWorkflowRuns(r.Context(), spaceID, workflowID, limit, offset)
	if err != nil {
		if h.writeWorkflowSvcError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "list_workflow_runs", "space_id", spaceID, "workflow_id", workflowID)
		return
	}
	out := make([]workflowRunResponse, len(runs))
	for i := range runs {
		out[i] = workflowRunToResponse(runs[i])
	}
	httputil.WriteJSON(w, http.StatusOK, workflowRunListResponse{Runs: out, Total: total})
}

// listScheduleRunsHandler returns the workflow runs a recurring schedule fired,
// newest first. It mirrors listScheduleTasksHandler for the Agent case: the
// schedule is the space-scoping anchor, so its runs are inherently in the space.
func (h *Handler) listScheduleRunsHandler(w http.ResponseWriter, r *http.Request) {
	_, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Workflows, "workflows not configured")
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
		httputil.WriteInternalError(w, err, "handler error", "handler", "list_schedule_runs", "schedule_id", scheduleID)
		return
	}
	if sched == nil || sched.SpaceID != spaceID {
		httputil.WriteJSONError(w, http.StatusNotFound, "schedule not found")
		return
	}
	limit, offset := httputil.LimitOffset(r.URL.Query(), "limit", "offset", httputil.BrowsePageDefault, httputil.BrowsePageMax)
	runs, total, err := h.cfg.Workflows.ListWorkflowRunsBySchedule(r.Context(), scheduleID, limit, offset)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "list_schedule_runs", "schedule_id", scheduleID)
		return
	}
	out := make([]workflowRunResponse, len(runs))
	for i := range runs {
		out[i] = workflowRunToResponse(runs[i])
	}
	httputil.WriteJSON(w, http.StatusOK, workflowRunListResponse{Runs: out, Total: total})
}

func (h *Handler) getWorkflowRunHandler(w http.ResponseWriter, r *http.Request) {
	_, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Workflows, "workflows not configured")
	if !ok {
		return
	}
	runID, ok := httputil.PathValue(w, r, "workflow_run_id")
	if !ok {
		return
	}
	run, steps, err := h.workflowService().GetWorkflowRunDetail(r.Context(), spaceID, runID)
	if err != nil {
		if h.writeWorkflowSvcError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "get_workflow_run", "space_id", spaceID, "workflow_run_id", runID)
		return
	}
	stepOut := make([]workflowNodeRunResponse, len(steps))
	for i := range steps {
		stepOut[i] = workflowNodeRunToResponse(steps[i])
	}
	httputil.WriteJSON(w, http.StatusOK, workflowRunDetailResponse{Run: workflowRunToResponse(*run), Steps: stepOut})
}

func (h *Handler) createWorkflowRunHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Workflows, "workflows not configured")
	if !ok {
		return
	}
	if _, ok := h.guard().SpaceAction(w, r, userID, spaceID, corespace.ActionRunWorkflow); !ok {
		return
	}
	workflowID, ok := httputil.PathValue(w, r, "workflow_id")
	if !ok {
		return
	}
	var req createWorkflowRunRequest
	if r.ContentLength != 0 {
		if !httputil.DecodeJSONBody(w, r, &req) {
			return
		}
	}
	run, steps, err := h.workflowService().StartWorkflowRun(r.Context(), workflow.StartWorkflowRunCmd{
		SpaceID:    spaceID,
		UserID:     userID,
		WorkflowID: workflowID,
		IssueID:    req.IssueID,
		Input:      string(req.Input),
	})
	if err != nil {
		if h.writeWorkflowSvcError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "create_workflow_run", "space_id", spaceID, "workflow_id", workflowID)
		return
	}
	stepOut := make([]workflowNodeRunResponse, len(steps))
	for i := range steps {
		stepOut[i] = workflowNodeRunToResponse(steps[i])
	}
	httputil.WriteJSON(w, http.StatusCreated, workflowRunDetailResponse{Run: workflowRunToResponse(*run), Steps: stepOut})
}

func (h *Handler) createIssueWorkflowRunHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Workflows, "workflows not configured")
	if !ok {
		return
	}
	if _, ok := h.guard().SpaceAction(w, r, userID, spaceID, corespace.ActionRunWorkflow); !ok {
		return
	}
	issueID, ok := httputil.PathValue(w, r, "issue_id")
	if !ok {
		return
	}
	if !httputil.RequireStore(w, h.cfg.Issues, "issues not configured") {
		return
	}
	issue, err := h.cfg.Issues.GetIssue(r.Context(), issueID)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "get_issue_for_workflow_run", "space_id", spaceID, "issue_id", issueID)
		return
	}
	if issue == nil || issue.SpaceID != spaceID {
		httputil.WriteJSONError(w, http.StatusNotFound, "issue not found")
		return
	}
	if issue.ExecutorKind == nil || issue.ExecutorID == nil || *issue.ExecutorKind != coreissue.ExecutorWorkflow {
		httputil.WriteJSONError(w, http.StatusBadRequest, "issue not assigned to workflow")
		return
	}
	run, steps, err := h.workflowService().StartWorkflowRun(r.Context(), workflow.StartWorkflowRunCmd{
		SpaceID:    spaceID,
		UserID:     userID,
		WorkflowID: *issue.ExecutorID,
		IssueID:    &issueID,
	})
	if err != nil {
		if h.writeWorkflowSvcError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "create_issue_workflow_run", "space_id", spaceID, "issue_id", issueID)
		return
	}
	stepOut := make([]workflowNodeRunResponse, len(steps))
	for i := range steps {
		stepOut[i] = workflowNodeRunToResponse(steps[i])
	}
	httputil.WriteJSON(w, http.StatusCreated, workflowRunDetailResponse{Run: workflowRunToResponse(*run), Steps: stepOut})
}
