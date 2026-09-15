package work

import (
	"context"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	"net/http"
	"strings"
	"time"

	agentsvc "github.com/icloudbb/buildmax/internal/service/agent"

	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/server/httputil"
	"github.com/icloudbb/buildmax/internal/service/issue"
	"github.com/icloudbb/buildmax/internal/service/task"
)

type IssueResponse struct {
	ID            string    `json:"id"`
	UserID        string    `json:"user_id"`
	SpaceID       string    `json:"space_id,omitempty"`
	ParentIssueID *string   `json:"parent_issue_id,omitempty"`
	Title         string    `json:"title"`
	Description   string    `json:"description"`
	Status        string    `json:"status"`
	OwnerID       *string   `json:"owner_id,omitempty"`
	ExecutorKind  *string   `json:"executor_kind,omitempty"`
	ExecutorID    *string   `json:"executor_id,omitempty"`
	CreatedBy     string    `json:"created_by"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	// Version is what a later update must send back. Every response that
	// carries an issue carries it, because every one of them is a potential
	// read half of a read-modify-write.
	Version uint64 `json:"version"`
	// ChildCount, DoneChildCount, and CommentCount are derived per response,
	// never stored. They are zero on responses that do not compute them.
	ChildCount     int `json:"child_count"`
	DoneChildCount int `json:"done_child_count"`
	CommentCount   int `json:"comment_count"`
}

type issueListResponse struct {
	Issues []IssueResponse `json:"issues"`
	Total  int             `json:"total"`
}

type issueFlowRunResponse struct {
	Run   workflowRunResponse       `json:"run"`
	Steps []workflowNodeRunResponse `json:"steps"`
}

type issueFlowResponse struct {
	Issue IssueResponse `json:"issue"`
	// Parent is set on a sub-issue; Children on a parent. Runs, agent tasks,
	// and outputs stay scoped to Issue — a parent's Results panel must keep
	// meaning "what this issue produced".
	Parent       *IssueResponse         `json:"parent,omitempty"`
	Children     []IssueResponse        `json:"children"`
	Workflow     *workflowResponse      `json:"workflow,omitempty"`
	Runs         []issueFlowRunResponse `json:"runs"`
	AgentTasks   []TaskResponse         `json:"agent_tasks"`
	LatestResult *issueOutputResponse   `json:"latest_result,omitempty"`
	Outputs      []issueOutputResponse  `json:"outputs"`
	Total        int                    `json:"total"`
}

type createIssueAgentRunRequest struct {
	Input string `json:"input"`
}

type createIssueRequest struct {
	Title         string  `json:"title"`
	Description   string  `json:"description"`
	ParentIssueID *string `json:"parent_issue_id"`
}

type patchIssueRequest struct {
	// Version is required: it is the version the client read. Absent or stale
	// is refused rather than applied.
	Version       uint64  `json:"version"`
	Title         *string `json:"title"`
	Description   *string `json:"description"`
	Status        *string `json:"status"`
	OwnerID       *string `json:"owner_id"`
	ExecutorKind  *string `json:"executor_kind"`
	ExecutorID    *string `json:"executor_id"`
	ParentIssueID *string `json:"parent_issue_id"`
}

func issueToResponse(issue coreissue.Issue) IssueResponse {
	return IssueResponse{
		ID:            issue.ID,
		UserID:        issue.UserID,
		SpaceID:       issue.SpaceID,
		ParentIssueID: issue.ParentIssueID,
		Title:         issue.Title,
		Description:   issue.Description,
		Status:        issue.Status,
		OwnerID:       issue.OwnerID,
		ExecutorKind:  issue.ExecutorKind,
		ExecutorID:    issue.ExecutorID,
		CreatedBy:     issue.CreatedBy,
		CreatedAt:     issue.CreatedAt,
		UpdatedAt:     issue.UpdatedAt,
		Version:       issue.Version,
	}
}

// decorateIssueResponses fills the derived counts for a page of issues. The
// rules for loading them, including that a failure degrades to zero, live in
// the service; this only places them on the response.
func (h *Handler) decorateIssueResponses(ctx context.Context, out []IssueResponse) {
	if len(out) == 0 {
		return
	}
	ids := make([]string, len(out))
	for i := range out {
		ids[i] = out[i].ID
	}
	counts := h.issueService().CountsFor(ctx, ids)
	for i := range out {
		c := counts[out[i].ID]
		out[i].ChildCount = c.Children
		out[i].DoneChildCount = c.DoneChildren
		out[i].CommentCount = c.Comments
	}
}

func buildIssueAgentRunInput(issue coreissue.Issue) string {
	var b strings.Builder
	b.WriteString("Work on this issue.\n\n")
	b.WriteString("Title: ")
	b.WriteString(issue.Title)
	if strings.TrimSpace(issue.Description) != "" {
		b.WriteString("\n\nDescription:\n")
		b.WriteString(issue.Description)
	}
	return b.String()
}

func (h *Handler) issueService() *issue.Service {
	return h.issues
}

func newIssueService(cfg Config) *issue.Service {
	return &issue.Service{
		Issues:    cfg.Issues,
		Comments:  cfg.IssueComments,
		Agents:    cfg.Agents,
		Spaces:    cfg.Spaces,
		Workflows: cfg.Workflows,
	}
}

func (h *Handler) writeIssueServiceError(w http.ResponseWriter, err error) bool {
	return httputil.WriteServiceError(w, err)
}

func (h *Handler) listIssuesHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Issues, "issues not configured")
	if !ok {
		return
	}
	limit, offset := httputil.LimitOffset(r.URL.Query(), "limit", "offset", httputil.ListPageDefault, httputil.ListPageMax)
	// No parent_id lists every issue in the space, sub-issues included. That is
	// what callers predating the hierarchy expect, so the board opts into the
	// filtered view rather than the endpoint changing under anyone.
	var filter coreissue.ListFilter
	switch parentID := r.URL.Query().Get("parent_id"); parentID {
	case "":
	case "none":
		filter.TopLevelOnly = true
	default:
		filter.ParentIssueID = parentID
	}
	// owner=me is the inbox: the caller is the one identity this route can
	// resolve without being told, and spelling out one's own user id to ask
	// what one is accountable for is a worse question than the one being asked.
	if owner := r.URL.Query().Get("owner"); owner == "me" {
		filter.OwnerID = userID
	} else {
		filter.OwnerID = r.URL.Query().Get("owner_id")
	}
	filter.ExecutorKind = r.URL.Query().Get("executor_kind")
	filter.ExecutorID = r.URL.Query().Get("executor_id")
	filter.Status = r.URL.Query().Get("status")
	list, total, err := h.issueService().ListIssues(r.Context(), spaceID, filter, limit, offset)
	if err != nil {
		if h.writeIssueServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "list_issues", "user_id", userID, "space_id", spaceID)
		return
	}
	out := make([]IssueResponse, len(list))
	for i := range list {
		out[i] = issueToResponse(list[i])
	}
	h.decorateIssueResponses(r.Context(), out)
	httputil.WriteJSON(w, http.StatusOK, issueListResponse{Issues: out, Total: total})
}

func (h *Handler) createIssueHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Issues, "issues not configured")
	if !ok {
		return
	}
	var req createIssueRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	createdIssue, err := h.issueService().CreateIssue(r.Context(), issue.CreateIssueCmd{
		UserID:        userID,
		SpaceID:       spaceID,
		Title:         req.Title,
		Description:   req.Description,
		ParentIssueID: req.ParentIssueID,
	})
	if err != nil {
		if h.writeIssueServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "create_issue", "user_id", userID, "space_id", spaceID)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, issueToResponse(*createdIssue))
}

func (h *Handler) getIssueHandler(w http.ResponseWriter, r *http.Request) {
	_, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Issues, "issues not configured")
	if !ok {
		return
	}
	issueID, ok := httputil.PathValue(w, r, "issue_id")
	if !ok {
		return
	}
	issue, err := h.issueService().GetIssue(r.Context(), spaceID, issueID)
	if err != nil {
		if h.writeIssueServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "get_issue", "issue_id", issueID)
		return
	}
	out := []IssueResponse{issueToResponse(*issue)}
	h.decorateIssueResponses(r.Context(), out)
	httputil.WriteJSON(w, http.StatusOK, out[0])
}

// issueRelatives resolves the issue's place in the hierarchy: its parent if it
// is a sub-issue, its children if it is a parent. The hierarchy is two levels
// deep, so an issue is never both.
//

func (h *Handler) getIssueFlowHandler(w http.ResponseWriter, r *http.Request) {
	_, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Issues, "issues not configured")
	if !ok {
		return
	}
	if !httputil.RequireStore(w, h.cfg.Workflows, "workflows not configured") {
		return
	}
	issueID, ok := httputil.PathValue(w, r, "issue_id")
	if !ok {
		return
	}
	limit, offset := httputil.LimitOffset(r.URL.Query(), "limit", "offset", httputil.BrowsePageDefault, httputil.BrowsePageMax)
	flow, err := h.loadIssueFlow(r.Context(), spaceID, issueID, limit, offset)
	if err != nil {
		if h.writeIssueServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "get_issue_flow", "issue_id", issueID)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, h.issueFlowToResponse(r.Context(), flow))
}

// issueFlowToResponse turns the gathered view into the shape the Portal reads.
func (h *Handler) issueFlowToResponse(ctx context.Context, flow *issueFlow) issueFlowResponse {
	self := []IssueResponse{issueToResponse(flow.Issue)}
	h.decorateIssueResponses(ctx, self)

	var parentOut *IssueResponse
	if flow.Parent != nil {
		out := issueToResponse(*flow.Parent)
		parentOut = &out
	}
	childrenOut := make([]IssueResponse, len(flow.Children))
	for i := range flow.Children {
		childrenOut[i] = issueToResponse(flow.Children[i])
	}
	h.decorateIssueResponses(ctx, childrenOut)

	var workflowOut *workflowResponse
	if flow.Workflow != nil {
		out := workflowToResponse(*flow.Workflow)
		workflowOut = &out
	}

	runOut := make([]issueFlowRunResponse, len(flow.Runs))
	for i := range flow.Runs {
		steps := make([]workflowNodeRunResponse, len(flow.Runs[i].Steps))
		for j := range flow.Runs[i].Steps {
			steps[j] = workflowNodeRunToResponse(flow.Runs[i].Steps[j])
		}
		runOut[i] = issueFlowRunResponse{Run: workflowRunToResponse(flow.Runs[i].Run), Steps: steps}
	}

	agentTasks := make([]TaskResponse, len(flow.AgentTasks))
	for i := range flow.AgentTasks {
		agentTasks[i] = taskToResponse(flow.AgentTasks[i])
	}

	outputs, latest := h.aggregateIssueOutputs(ctx, flow.AgentTasks, flow.StepsByTaskID)
	return issueFlowResponse{
		Issue:        self[0],
		Parent:       parentOut,
		Children:     childrenOut,
		Workflow:     workflowOut,
		Runs:         runOut,
		AgentTasks:   agentTasks,
		LatestResult: latest,
		Outputs:      outputs,
		Total:        flow.TotalRuns,
	}
}

func (h *Handler) createIssueAgentRunHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Issues, "issues not configured")
	if !ok {
		return
	}
	if !httputil.RequireStore(w, h.cfg.Agents, "agents not configured") {
		return
	}
	if !httputil.RequireStore(w, h.cfg.Tasks, "tasks not configured") {
		return
	}
	issueID, ok := httputil.PathValue(w, r, "issue_id")
	if !ok {
		return
	}
	var req createIssueAgentRunRequest
	if r.ContentLength != 0 {
		if !httputil.DecodeJSONBody(w, r, &req) {
			return
		}
	}
	plan, err := h.issueService().PlanAssignedAgentRun(r.Context(),
		issue.StartAssignedAgentCmd{SpaceID: spaceID, IssueID: issueID, UserID: userID, Input: req.Input},
		h.taskService(),
	)
	if err != nil {
		if httputil.WriteServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "create_issue_agent_run", "issue_id", issueID)
		return
	}
	input := req.Input
	if input == "" {
		input = buildIssueAgentRunInput(plan.Issue)
	}
	createdTask, err := h.taskService().CreateTask(r.Context(), task.CreateTaskCmd{
		UserID:        userID,
		SpaceID:       spaceID,
		Input:         input,
		AgentID:       &plan.AgentID,
		IssueID:       &issueID,
		CreatedByType: coretask.RunCreatedByTypeUser,
		TriggerSource: coretask.RunTriggerSourceIssueAgentRun,
	})
	if err != nil {
		if h.writeTaskServiceError(w, r, err, &plan.AgentID) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "create_issue_agent_task", "issue_id", issueID)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, taskToResponse(*createdTask))
}

func (h *Handler) patchIssueHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Issues, "issues not configured")
	if !ok {
		return
	}
	issueID, ok := httputil.PathValue(w, r, "issue_id")
	if !ok {
		return
	}
	var req patchIssueRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	if req.ExecutorKind != nil && *req.ExecutorKind == coreissue.ExecutorWorkflow {
		if _, ok := h.guard().SpaceAction(w, r, userID, spaceID, corespace.ActionAssignIssueWorkflow); !ok {
			return
		}
	}
	updatedIssue, err := h.issueService().UpdateIssue(r.Context(), issue.UpdateIssueCmd{
		UserID:        userID,
		SpaceID:       spaceID,
		IssueID:       issueID,
		IfVersion:     req.Version,
		Title:         req.Title,
		Description:   req.Description,
		Status:        req.Status,
		OwnerID:       req.OwnerID,
		ExecutorKind:  req.ExecutorKind,
		ExecutorID:    req.ExecutorID,
		ParentIssueID: req.ParentIssueID,
	})
	if err != nil {
		if h.writeIssueServiceError(w, err) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "patch_issue", "issue_id", issueID)
		return
	}
	out := []IssueResponse{issueToResponse(*updatedIssue)}
	h.decorateIssueResponses(r.Context(), out)
	httputil.WriteJSON(w, http.StatusOK, out[0])
}

func newWorkAgentService(cfg Config) *agentsvc.Service { return &agentsvc.Service{Agents: cfg.Agents} }
