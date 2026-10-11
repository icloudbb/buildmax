package work

import (
	"net/http"
	"time"

	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/server/httputil"
	"github.com/icloudbb/buildmax/internal/util"
)

// sourceMessageMaxLen bounds the quoted message. It is a comparison, not a
// transcript: the conversation route serves the full text.
const sourceMessageMaxLen = 2000

// RunProvenanceResponse answers where one run came from, and why it failed.
//
// It is deliberately not the run record. What a reader needs here is the chain
// of responsibility — who or what asked, through which path, repeating which
// earlier attempt, and in which message — next to the instruction that reached
// the worker, and for a failed run, what stopped it. Output, tokens, and worker
// placement answer different questions and have their own routes.
type RunProvenanceResponse struct {
	TaskRunID string `json:"task_run_id"`
	TaskID    string `json:"task_id"`
	Status    string `json:"status"`
	// Input is what the worker was given. Compare it with SourceMessage.
	Input             string                        `json:"input"`
	CreatedBy         string                        `json:"created_by,omitempty"`
	CreatedByType     string                        `json:"created_by_type,omitempty"`
	TriggerSource     string                        `json:"trigger_source,omitempty"`
	RetryOfTaskRunID  *string                       `json:"retry_of_task_run_id,omitempty"`
	CreatedAt         time.Time                     `json:"created_at"`
	SourceMessage     *SourceMessageResponse        `json:"source_message,omitempty"`
	Agent             *RunAgentResponse             `json:"agent,omitempty"`
	SpaceInstructions *RunSpaceInstructionsResponse `json:"space_instructions,omitempty"`
	// PluginPins are the releases this run actually resolved, not what the
	// agent currently names — see docs/design/portal-data-and-plugin-surfaces.md
	// and coretask.Run.PluginPins. Empty for a run that resolved none, which
	// includes every run that predates this column.
	PluginPins []coreplugin.Pin `json:"plugin_pins,omitempty"`
	// Artifacts are what this run published, looked up the same way an issue's
	// output list is: by source ID, through the artifact service, rather than
	// a Portal-only record of what a run produced. See
	// docs/design/unified-artifacts.md section 5.2.
	Artifacts []RunArtifactResponse `json:"artifacts,omitempty"`
	// ErrorMessage, FailureClass, and FailureCause say why a run ended in
	// failure: the server's text, whose problem it was, and the configuration
	// to fix when the server knew. A reader opens a failed run asking why.
	ErrorMessage *string                `json:"error_message,omitempty"`
	FailureClass string                 `json:"failure_class,omitempty"`
	FailureCause *coretask.FailureCause `json:"failure_cause,omitempty"`
	// TraceRecorded says whether the trace route has a trace to serve, so a
	// reader of a run that ended before its Agent started, or whose trace
	// expired, is told so without asking for one that is not there.
	TraceRecorded bool `json:"trace_recorded"`
}

// RunArtifactResponse is the summary a reader needs to recognise and open an
// artifact this run published, without repeating the whole Artifact record.
type RunArtifactResponse struct {
	ID        string    `json:"id"`
	Title     string    `json:"title,omitempty"`
	Filename  string    `json:"filename"`
	MediaType string    `json:"media_type,omitempty"`
	SizeBytes int64     `json:"size_bytes,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type RunSpaceInstructionsResponse struct {
	Revision        int `json:"revision"`
	CurrentRevision int `json:"current_revision,omitempty"`
}

// RunAgentResponse names the agent definition a run executed under.
//
// Revision is the point. An agent's instructions are resolved when its worker
// asks for the run, so the definition can change between two runs of one task —
// and without the number, nothing says which text produced which outcome.
type RunAgentResponse struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	// Revision is 0 when the run predates the record or never reached a worker.
	Revision int `json:"revision,omitempty"`
	// CurrentRevision is what the definition says now. A number ahead of
	// Revision means the agent was edited after this run.
	CurrentRevision int  `json:"current_revision,omitempty"`
	Deleted         bool `json:"deleted,omitempty"`
}

// SourceMessageResponse is what the person actually said.
type SourceMessageResponse struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Truncated bool      `json:"truncated"`
	CreatedAt time.Time `json:"created_at"`
}

// getTaskRunProvenanceHandler serves GET
// /api/spaces/{space_id}/task-runs/{task_run_id}.
func (h *Handler) getTaskRunProvenanceHandler(w http.ResponseWriter, r *http.Request) {
	_, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.TaskRuns, "task runs not configured")
	if !ok {
		return
	}
	taskRunID, ok := httputil.PathValue(w, r, "task_run_id")
	if !ok {
		return
	}
	run, task, ok := h.runAndTaskForSpace(w, r, spaceID, taskRunID)
	if !ok {
		return
	}
	out := RunProvenanceResponse{
		TaskRunID:         run.ID,
		TaskID:            task.ID,
		Status:            run.Status,
		Input:             run.Input,
		CreatedBy:         run.CreatedBy,
		CreatedByType:     run.CreatedByType,
		TriggerSource:     run.TriggerSource,
		RetryOfTaskRunID:  run.RetryOfTaskRunID,
		CreatedAt:         run.CreatedAt,
		SourceMessage:     h.resolveSourceMessage(r, task, run.SourceMessageID),
		Agent:             h.resolveRunAgent(r, task, run),
		SpaceInstructions: h.resolveRunSpaceInstructions(r, task, run),
		PluginPins:        run.PluginPins,
		Artifacts:         h.resolveRunArtifacts(r, run),
		ErrorMessage:      run.ErrorMessage,
		FailureClass:      run.FailureClass,
		FailureCause:      run.FailureCause,
		TraceRecorded:     run.TracePath != nil && *run.TracePath != "",
	}
	httputil.WriteJSON(w, http.StatusOK, out)
}

// resolveRunArtifacts lists what this run published.
//
// A store that cannot answer, or a run that published nothing, both return no
// artifacts — provenance must not fail because this one part of it could not
// be read.
func (h *Handler) resolveRunArtifacts(r *http.Request, run *coretask.Run) []RunArtifactResponse {
	if h.cfg.Artifacts == nil || !h.cfg.Artifacts.Available() {
		return nil
	}
	bySource, err := h.cfg.Artifacts.ListBySource(r.Context(), []string{run.ID})
	if err != nil {
		return nil
	}
	artifacts := bySource[run.ID]
	if len(artifacts) == 0 {
		return nil
	}
	out := make([]RunArtifactResponse, 0, len(artifacts))
	for _, a := range artifacts {
		out = append(out, RunArtifactResponse{
			ID:        a.ID,
			Title:     a.Title,
			Filename:  a.Filename,
			MediaType: a.MediaType,
			SizeBytes: a.SizeBytes,
			CreatedAt: a.CreatedAt,
		})
	}
	return out
}

func (h *Handler) resolveRunSpaceInstructions(r *http.Request, task *coretask.Task, run *coretask.Run) *RunSpaceInstructionsResponse {
	if run.SpaceAgentInstructionsRevision == nil {
		return nil
	}
	out := &RunSpaceInstructionsResponse{Revision: *run.SpaceAgentInstructionsRevision}
	if h.cfg.Spaces == nil {
		return out
	}
	space, err := h.cfg.Spaces.GetSpace(r.Context(), task.SpaceID)
	if err == nil && space != nil {
		out.CurrentRevision = space.AgentInstructionsRevision
	}
	return out
}

// resolveSourceMessage reads the message a run was asked for in.
//
// A message that cannot be read leaves the field absent rather than failing the
// request: the rest of the provenance is still true, and a run with no message
// behind it is the normal case anyway. The message is confirmed to belong to
// the run's own conversation before it is returned, so a stale handle cannot
// quote text from somewhere else.
func (h *Handler) resolveSourceMessage(r *http.Request, task *coretask.Task, messageID *string) *SourceMessageResponse {
	if messageID == nil || *messageID == "" || h.cfg.Messages == nil {
		return nil
	}
	msg, err := h.cfg.Messages.GetMessage(r.Context(), *messageID)
	if err != nil || msg == nil || msg.ConversationID != task.ConversationID {
		return nil
	}
	content := util.TruncateRunes(msg.Content, sourceMessageMaxLen)
	return &SourceMessageResponse{
		ID:        msg.ID,
		Content:   content,
		Truncated: content != msg.Content,
		CreatedAt: msg.CreatedAt,
	}
}

// resolveRunAgent names the agent this run executed under.
//
// A deleted agent is still resolved and still named: a run that already ran
// under it does not stop having done so, and refusing to say which definition
// produced an outcome is the opposite of provenance.
func (h *Handler) resolveRunAgent(r *http.Request, task *coretask.Task, run *coretask.Run) *RunAgentResponse {
	if task.AgentID == nil || *task.AgentID == "" {
		return nil
	}
	out := &RunAgentResponse{ID: *task.AgentID}
	if run.AgentRevision != nil {
		out.Revision = *run.AgentRevision
	}
	if h.cfg.Agents == nil {
		return out
	}
	agent, err := h.cfg.Agents.GetAgentIncludingDeleted(r.Context(), *task.AgentID)
	if err != nil || agent == nil || agent.SpaceID != task.SpaceID {
		return out
	}
	out.Name = agent.Name
	out.CurrentRevision = agent.Revision
	out.Deleted = agent.DeletedAt != nil
	return out
}
