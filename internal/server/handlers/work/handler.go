// Package work serves the surface a space does its work on: issues and their
// comments, workflows, tasks and the runs that execute them, the conversations
// that start them, and the files and traces they leave behind.
//
// This is one package rather than four because the entities are one story. An
// issue is assigned to a workflow, a workflow step dispatches a task, a task
// run reports back into the conversation that asked for it, and the run's
// artifacts hang off the issue. Splitting them would not remove that coupling,
// it would turn it into four packages passing each other interfaces.
package work

import (
	"context"
	"net/http"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
	coreissue "github.com/icloudbb/buildmax/internal/core/issue"
	"github.com/icloudbb/buildmax/internal/core/llm"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
	coreschedule "github.com/icloudbb/buildmax/internal/core/schedule"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	blob "github.com/icloudbb/buildmax/internal/infra/objectstore"
	"github.com/icloudbb/buildmax/internal/server/access"
	"github.com/icloudbb/buildmax/internal/server/handlers/runterminal"
	"github.com/icloudbb/buildmax/internal/server/turnqueue"
	wsconn "github.com/icloudbb/buildmax/internal/server/websocket"
	agentsvc "github.com/icloudbb/buildmax/internal/service/agent"
	artifactsvc "github.com/icloudbb/buildmax/internal/service/artifact"
	"github.com/icloudbb/buildmax/internal/service/audit"
	"github.com/icloudbb/buildmax/internal/service/conversation"
	"github.com/icloudbb/buildmax/internal/service/issue"
	"github.com/icloudbb/buildmax/internal/service/quota"
	"github.com/icloudbb/buildmax/internal/service/task"
	"github.com/icloudbb/buildmax/internal/service/workflow"
)

type Config struct {
	JWTSecret string

	// Users is part of authentication, not work ownership: every user-facing
	// route must reject a disabled account before it reads the space's work.
	Users         coreidentity.UserStore
	Sessions      coreidentity.AuthSessionStore
	Issues        coreissue.Store
	IssueComments coreissue.CommentStore
	Workflows     coreworkflow.Store
	Tasks         coretask.Store
	TaskRuns      coretask.RunStore
	Agents        agentdef.Store
	// Schedules confirms a schedule belongs to the space before its tasks are
	// listed. Nil leaves the schedule-tasks route reporting the feature is off.
	Schedules     coreschedule.Store
	Spaces        corespace.Store
	Conversations coreconv.Store
	Messages      coreconv.MessageStore
	// LLMCalls reads the managed call ledger for one run. Nil leaves that
	// route answering 503, which is what a deployment with no database has.
	LLMCalls coregw.CallStore

	PersistStorage blob.PersistStorage
	// Artifacts lets an issue show what its runs published. Nil means this
	// deployment has no artifact store, and an issue reports no published files.
	Artifacts     *artifactsvc.Service
	WorkspacesDir string

	Quota          *quota.Service
	TitleGenerator llm.TitleGenerator
	// ConversationLLM answers a Tier 1 turn. Nil leaves conversations unable to
	// run, which the routes report rather than assume.
	ConversationLLM llm.LLMClient
	Audit           *audit.Recorder

	// Hub streams a running task's output; Turns keeps one conversation to one
	// turn at a time. Both are server-scoped and shared with the socket, so
	// they arrive rather than being created here.
	Hub   wsconn.StreamHub
	Turns *turnqueue.Registry
	// OnTerminal closes out a run cancelled here, reaching the listeners a
	// worker's own report reaches.
	OnTerminal func(ctx context.Context, info coretask.RunTerminalInfo)

	// TerminalGroup owns the callbacks a cancel here fires, so a shutdown waits
	// for them instead of dropping them.
	TerminalGroup *runterminal.Group

	// Drain is closed when the server is going away, and ends the task stream —
	// a watcher of state that lives in the database, so resubscribing elsewhere
	// loses nothing. The conversation streams in this package ignore it: they
	// carry a turn being produced, which no other instance can take over.
	// See docs/design/graceful-shutdown.md §5.
	Drain <-chan struct{}
}

type Handler struct {
	cfg Config

	tasks         *task.Service
	conversations *conversation.Service
	issues        *issue.Service
	agents        *agentsvc.Service
	workflows     *workflow.Service
}

// New builds the work surface. A nil Hub or Turns gets one of its own, which
// is what the unified handler did: a deployment with nobody watching still has
// runs to stream and turns to serialize.
func New(cfg Config) *Handler {
	if cfg.Hub == nil {
		cfg.Hub = wsconn.NewStreamHub()
	}
	if cfg.Turns == nil {
		cfg.Turns = turnqueue.NewRegistry(nil)
	}
	h := &Handler{cfg: cfg}
	h.tasks = newTaskService(cfg)
	h.workflows = newWorkflowService(cfg, h.tasks)
	h.conversations = newConversationService(cfg, h.tasks, h.workflows)
	h.issues = newIssueService(cfg)
	h.agents = newWorkAgentService(cfg)
	return h
}

func (h *Handler) guard() *access.Guard {
	return &access.Guard{
		JWTSecret: h.cfg.JWTSecret,
		Users:     h.cfg.Users,
		Spaces:    h.cfg.Spaces,
		Sessions:  h.cfg.Sessions,
		Audit:     h.cfg.Audit,
	}
}

func (h *Handler) Register(mux *http.ServeMux) {
	// Issues
	mux.HandleFunc("GET /api/spaces/{space_id}/issues", h.listIssuesHandler)
	mux.HandleFunc("POST /api/spaces/{space_id}/issues", h.createIssueHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/issues/{issue_id}", h.getIssueHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/issues/{issue_id}/flow", h.getIssueFlowHandler)
	mux.HandleFunc("PATCH /api/spaces/{space_id}/issues/{issue_id}", h.patchIssueHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/issues/{issue_id}/comments", h.listIssueCommentsHandler)
	mux.HandleFunc("POST /api/spaces/{space_id}/issues/{issue_id}/comments", h.createIssueCommentHandler)
	mux.HandleFunc("PATCH /api/spaces/{space_id}/issues/{issue_id}/comments/{comment_id}", h.patchIssueCommentHandler)
	mux.HandleFunc("DELETE /api/spaces/{space_id}/issues/{issue_id}/comments/{comment_id}", h.deleteIssueCommentHandler)
	mux.HandleFunc("POST /api/spaces/{space_id}/issues/{issue_id}/agent-runs", h.createIssueAgentRunHandler)
	mux.HandleFunc("POST /api/spaces/{space_id}/issues/{issue_id}/workflow-runs", h.createIssueWorkflowRunHandler)

	// Workflows
	mux.HandleFunc("GET /api/spaces/{space_id}/workflows", h.listWorkflowsHandler)
	mux.HandleFunc("POST /api/spaces/{space_id}/workflows", h.createWorkflowHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/workflows/{workflow_id}", h.getWorkflowHandler)
	mux.HandleFunc("PATCH /api/spaces/{space_id}/workflows/{workflow_id}", h.patchWorkflowHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/workflows/{workflow_id}/revisions", h.listWorkflowRevisionsHandler)
	mux.HandleFunc("POST /api/spaces/{space_id}/workflows/{workflow_id}/revisions/{revision}/restore", h.restoreWorkflowRevisionHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/workflows/{workflow_id}/runs", h.listWorkflowRunsHandler)
	mux.HandleFunc("POST /api/spaces/{space_id}/workflows/{workflow_id}/runs", h.createWorkflowRunHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/workflow-runs/{workflow_run_id}", h.getWorkflowRunHandler)

	// Files
	mux.HandleFunc("POST /api/spaces/{space_id}/upload", h.uploadHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/files", h.filesTreeHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/files/{path...}", h.fileContentHandler)

	// Conversations
	mux.HandleFunc("GET /api/spaces/{space_id}/conversations", h.listConversationsHandler)
	mux.HandleFunc("POST /api/spaces/{space_id}/conversations", h.createConversationHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/conversations/{conversation_id}/messages", h.getConversationMessagesHandler)
	mux.HandleFunc("POST /api/spaces/{space_id}/conversations/{conversation_id}/messages", h.addConversationMessageHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/conversations/{conversation_id}/tasks", h.listConversationTasksHandler)
	mux.HandleFunc("POST /api/spaces/{space_id}/conversations/{conversation_id}/tasks", h.createConversationTaskHandler)

	// Tasks and task runs
	mux.HandleFunc("POST /api/spaces/{space_id}/tasks", h.createSpaceTaskHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/agents/{agent_id}/tasks", h.listAgentTasksHandler)
	mux.HandleFunc("POST /api/spaces/{space_id}/agents/{agent_id}/tasks", h.createAgentTaskHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/schedules/{schedule_id}/tasks", h.listScheduleTasksHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/tasks/{task_id}", h.getTaskHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/tasks/{task_id}/runs", h.listTaskRunsHandler)
	mux.HandleFunc("POST /api/spaces/{space_id}/tasks/{task_id}/runs", h.createTaskRunHandler)
	mux.HandleFunc("POST /api/spaces/{space_id}/tasks/{task_id}/cancel", h.cancelTaskHandler)
	mux.HandleFunc("POST /api/spaces/{space_id}/tasks/{task_id}/retry", h.retryTaskHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/tasks/{task_id}/conversation", h.getTaskConversationHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/tasks/{task_id}/stream", h.getChatStreamHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/task-runs/{task_run_id}", h.getTaskRunProvenanceHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/task-runs/{task_run_id}/trace", h.getTaskRunTraceHandler)
	mux.HandleFunc("GET /api/spaces/{space_id}/task-runs/{task_run_id}/llm-calls", h.listTaskRunLLMCallsHandler)
}

// runAnnouncer closes out a run cancelled here, reaching the same listeners a
// worker's own report does.
func (h *Handler) runAnnouncer() *runterminal.Announcer {
	return &runterminal.Announcer{Runs: h.cfg.TaskRuns, Hub: h.cfg.Hub, On: h.cfg.OnTerminal, Group: h.cfg.TerminalGroup}
}
