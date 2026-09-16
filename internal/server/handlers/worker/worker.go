package worker

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	"github.com/icloudbb/buildmax/internal/core/eligibility"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/infra/workerclient"
	"github.com/icloudbb/buildmax/internal/server/httputil"
	workspacesvc "github.com/icloudbb/buildmax/internal/service/workspace"
)

func (h *Handler) getTaskRun(w http.ResponseWriter, r *http.Request) {
	taskRunID := r.PathValue("task_run_id")
	if taskRunID == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "task_run_id required")
		return
	}
	if h.cfg.TaskRuns == nil {
		httputil.WriteJSONError(w, http.StatusServiceUnavailable, "task runs not configured")
		return
	}
	run, task, err := h.cfg.TaskRuns.GetTaskRunWithTask(r.Context(), taskRunID)
	if err != nil {
		httputil.WriteInternalError(w, err, "worker handler error", "handler", "get_worker_task_run", "task_run_id", taskRunID)
		return
	}
	if run == nil || task == nil {
		httputil.WriteJSONError(w, http.StatusNotFound, "run not found")
		return
	}
	// Re-check the initiator's authority at the moment the worker starts. It
	// closes the race where the account was disabled or removed from the Space
	// between the scheduler's dispatch check and here: record a cancel with the
	// reason and hand the worker a CancelRequested run, which it honors by
	// stopping and reporting CANCELED. A store outage leaves authority unknown;
	// let the run proceed rather than stop it on a guess — the reconciler will
	// revisit it.
	cancelRequested := run.CancelRequestedAt != nil
	if h.cfg.Eligible != nil && !coretask.RunStatusTerminal(run.Status) && run.CreatedBy != "" && !cancelRequested {
		switch err := h.cfg.Eligible.Check(r.Context(), run.CreatedBy, task.SpaceID); {
		case err == nil, errors.Is(err, eligibility.ErrUnavailable):
		default:
			reason := coretask.CancelReasonCreatorNotMember
			if errors.Is(err, eligibility.ErrAccountDisabled) {
				reason = coretask.CancelReasonCreatorDisabled
			}
			if _, cErr := h.cfg.TaskRuns.RequestTaskRunCancel(r.Context(), run.ID, "", reason, time.Now().UTC()); cErr != nil {
				componentLog().Warn("worker handler: could not record cancel for an ineligible run", "task_run_id", taskRunID, "err", cErr)
			}
			cancelRequested = true
		}
	}
	h.recordSeen(r, run)
	spaceInstructions := ""
	spaceInstructionsRevision := 0
	if h.cfg.Spaces != nil {
		space, terr := h.cfg.Spaces.GetSpace(r.Context(), task.SpaceID)
		if terr != nil {
			componentLog().Warn("worker handler: space agent instructions unavailable", "task_run_id", taskRunID, "space_id", task.SpaceID, "err", terr)
		} else if space != nil {
			spaceInstructions = space.AgentInstructions
			spaceInstructionsRevision = space.AgentInstructionsRevision
			h.recordSpaceAgentInstructionsRevision(r, run, spaceInstructionsRevision)
		}
	}
	// The agent's instructions are appended to the run's system prompt. Resolving them here
	// rather than at task creation means an edited definition takes effect on the next run,
	// which is what someone editing the field expects. A deleted agent still answers, because
	// a run that already names it has to finish under the identity it was started with.
	agentInstructions := ""
	var runAgent *agentdef.Agent
	if task.AgentID != nil && *task.AgentID != "" && h.cfg.Agents != nil {
		if a, aerr := h.cfg.Agents.GetAgentIncludingDeleted(r.Context(), *task.AgentID); aerr != nil {
			// A run missing its instructions is worse than a run that never had any, but
			// refusing to dispatch it is worse still. Say so and continue.
			componentLog().Warn("worker handler: agent instructions unavailable", "task_run_id", taskRunID, "agent_id", *task.AgentID, "err", aerr)
		} else if a != nil && a.SpaceID == task.SpaceID {
			agentInstructions = a.Instructions
			runAgent = a
			h.recordAgentRevision(r, run, a.Revision)
		}
	}

	// Resolved here, beside the agent revision, and against the run's space
	// rather than the agent's: an agent that failed the space check above is
	// treated as no agent, plugins included.
	pins, pluginRefusal := h.resolvePluginPins(r, run, task, runAgent)
	h.recordPluginPins(r, run, pins)

	networkTier, filesystemTier := h.resolveSandboxTiers(r, run, task, runAgent)
	h.recordSandboxTiers(r, run, runAgent, networkTier, filesystemTier)

	httputil.WriteJSON(w, http.StatusOK, workerclient.GetTaskRunResponse{
		Run: workerclient.TaskRunRun{
			ID:                run.ID,
			TaskID:            run.TaskID,
			PreviousTaskRunID: run.PreviousTaskRunID,
			Input:             run.Input,
			Status:            run.Status,
			// The worker polls this route while it executes, so this field is
			// how a cancel reaches a run that is already under way — including
			// one this fetch just canceled for an initiator that lost authority.
			CancelRequested: cancelRequested,
			CreatedAt:       run.CreatedAt,
		},
		Task: workerclient.TaskRunTask{
			ID:                             task.ID,
			ConversationID:                 task.ConversationID,
			SpaceID:                        task.SpaceID,
			UserID:                         task.CreatedBy,
			SessionID:                      task.SessionID,
			AgentInstructions:              agentInstructions,
			SpaceAgentInstructions:         spaceInstructions,
			SpaceAgentInstructionsRevision: spaceInstructionsRevision,
		},
		Plugins:     toWirePlugins(pins),
		PluginError: pluginRefusal,
		// Resolved alongside the agent revision and plugin pins, against the
		// same runAgent: an agent that failed the space check above declares
		// no tiers, the same way it names no plugins.
		Sandbox: &workerclient.TaskRunSandbox{
			NetworkTier:    networkTier,
			FilesystemTier: filesystemTier,
		},
		// The server decides how the run reaches a model. A worker executes
		// model-chosen code, so it is told the transport and alias rather than
		// choosing them — and it is told nothing else about the model, because
		// endpoint, upstream identifier, and credential stay on this side.
		LLM: h.resolveRunLLM(r.Context(), runAgent),
	})
}

// resolveRunLLM is the descriptor telling this run how to reach a model: the
// deployment default, with the alias and its context window overridden when the
// run's agent names a model.
//
// The override copies the shared descriptor rather than mutating it, and takes
// effect only on the managed transport: a direct-transport worker reads its
// model from its own server.yaml and ignores the alias here. The chosen model's
// own context window is looked up so the run compacts against the window of the
// model it actually calls, not the deployment default's; a lookup that comes up
// empty keeps the default window rather than disabling windowing.
func (h *Handler) resolveRunLLM(ctx context.Context, a *agentdef.Agent) *workerclient.TaskRunLLM {
	base := h.cfg.WorkerLLM
	if base == nil || a == nil || a.Model == "" {
		return base
	}
	over := *base
	over.Model = a.Model
	if h.cfg.Gateway != nil {
		if models, err := h.cfg.Gateway.Models(ctx); err == nil {
			for _, m := range models {
				if m.Name == a.Model {
					over.ContextWindow = m.ContextWindow
					break
				}
			}
		}
	}
	return &over
}

func (h *Handler) postStream(w http.ResponseWriter, r *http.Request) {
	taskRunID := r.PathValue("task_run_id")
	if taskRunID == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "task_run_id required")
		return
	}
	if h.cfg.TaskRuns == nil {
		httputil.WriteJSONError(w, http.StatusServiceUnavailable, "task runs not configured")
		return
	}
	run, _, err := h.cfg.TaskRuns.GetTaskRunWithTask(r.Context(), taskRunID)
	if err != nil || run == nil {
		if err != nil {
			httputil.WriteInternalError(w, err, "worker handler error", "handler", "post_worker_stream", "task_run_id", taskRunID)
		} else {
			httputil.WriteJSONError(w, http.StatusNotFound, "run not found")
		}
		return
	}
	if !requireRunning(w, run.Status) {
		return
	}
	var req workerclient.StreamDeltaRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	h.cfg.Hub.Append(run.ID, req.Delta)
	httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handlePatchRunning(w http.ResponseWriter, r *http.Request, taskRunID string, req *workerclient.PatchTaskRunRequest) bool {
	updated, err := h.cfg.TaskRuns.TransitionTaskRun(r.Context(), coretask.TransitionRunInput{
		TaskRunID:      taskRunID,
		ExpectedStatus: coretask.RunStatusScheduled,
		NewStatus:      coretask.RunStatusRunning,
		StartedAt:      req.StartedAt,
		SessionID:      req.SessionID,
	})
	if err != nil {
		httputil.WriteInternalError(w, err, "worker handler error", "handler", "patch_worker_task_run", "task_run_id", taskRunID)
		return false
	}
	if !updated {
		httputil.WriteJSONError(w, http.StatusConflict, "run not scheduled or already running")
		return false
	}
	return true
}

func (h *Handler) handlePatchTerminalStatus(w http.ResponseWriter, r *http.Request, taskRunID string, req *workerclient.PatchTaskRunRequest) bool {
	updated, err := h.cfg.TaskRuns.TransitionTaskRun(r.Context(), coretask.TransitionRunInput{
		TaskRunID:        taskRunID,
		ExpectedStatus:   coretask.RunStatusRunning,
		NewStatus:        coretask.RunStatus(req.Status),
		StartedAt:        req.StartedAt,
		EndedAt:          req.EndedAt,
		Output:           req.Output,
		Structured:       req.Structured,
		ErrorMessage:     req.ErrorMessage,
		SessionID:        req.SessionID,
		PromptTokens:     req.PromptTokens,
		CompletionTokens: req.CompletionTokens,
		TracePath:        req.TracePath,
	})
	if err != nil {
		httputil.WriteInternalError(w, err, "worker handler error", "handler", "patch_worker_task_run", "task_run_id", taskRunID)
		return false
	}
	// Commit the checkpoint the run captured, if any — a successful result or a
	// failed run's partial, decided by the terminal status. It runs whether or
	// not this transition won: the finalizer is idempotent, so a duplicate or
	// recovery report still lands the checkpoint a crashed worker uploaded but
	// never got to commit. Fail-open — the run's outcome is already accepted, and
	// a checkpoint that cannot be committed must not undo it (§8, §13).
	if req.WorkspaceCheckpoint != nil {
		h.finalizeRunCheckpoint(r.Context(), taskRunID, req.Status, req.WorkspaceCheckpoint)
	}
	if !updated {
		// A recovery loop or an earlier retry already committed the outcome. A
		// worker report is idempotent and must not rewrite that terminal state.
		return true
	}
	h.announcer().Announce(r.Context(), taskRunID, req.Status, req.Output, req.ErrorMessage)
	return true
}

// finalizeRunCheckpoint commits a terminal run's checkpoint over bytes the
// worker already uploaded: a successful result, which advances the Task head from
// the run's base, or a failed run's partial, which preserves the work and leaves
// the head where it is. The kind follows the terminal status. It is best-effort:
// every failure is logged and swallowed, because the terminal outcome is already
// accepted and a checkpoint that cannot be committed must not fail the run (§13).
// The finalizer is idempotent by (run, kind).
func (h *Handler) finalizeRunCheckpoint(ctx context.Context, taskRunID, status string, desc *workerclient.WorkspaceCheckpointDescriptor) {
	if h.cfg.Checkpoints == nil || h.cfg.TaskRuns == nil {
		return
	}
	run, task, err := h.cfg.TaskRuns.GetTaskRunWithTask(ctx, taskRunID)
	if err != nil || run == nil || task == nil {
		componentLog().Error("could not load run to commit its workspace checkpoint", "task_run_id", taskRunID, "err", err)
		return
	}
	if _, err := h.cfg.Checkpoints.FinalizeResult(ctx, workspacesvc.FinalizeResultInput{
		SpaceID:          task.SpaceID,
		TaskID:           task.ID,
		SourceTaskRunID:  taskRunID,
		BaseCheckpointID: run.WorkspaceBaseCheckpointID,
		Succeeded:        status == string(coretask.RunStatusSucceeded),
		Payload: workspacesvc.PayloadDescriptor{
			Format:            desc.PayloadFormat,
			SHA256:            desc.PayloadSHA256,
			SizeBytes:         desc.SizeBytes,
			UncompressedBytes: desc.UncompressedBytes,
			EntryCount:        desc.EntryCount,
		},
	}); err != nil {
		componentLog().Error("could not commit the workspace checkpoint; run outcome stands", "task_run_id", taskRunID, "err", err)
	}
}

func (h *Handler) patchTaskRun(w http.ResponseWriter, r *http.Request) {
	taskRunID := r.PathValue("task_run_id")
	if taskRunID == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "task_run_id required")
		return
	}
	if h.cfg.TaskRuns == nil {
		httputil.WriteJSONError(w, http.StatusServiceUnavailable, "task runs not configured")
		return
	}
	var req workerclient.PatchTaskRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if req.Status == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "status required")
		return
	}
	if req.Status == string(coretask.RunStatusRunning) {
		if h.handlePatchRunning(w, r, taskRunID, &req) {
			httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		}
		return
	}
	if !coretask.RunStatusTerminal(req.Status) {
		httputil.WriteJSONError(w, http.StatusBadRequest, "invalid run status")
		return
	}
	if h.handlePatchTerminalStatus(w, r, taskRunID, &req) {
		httputil.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// Identity belongs in an attr, not in every message string.
func componentLog() *slog.Logger { return slog.With("component", "worker_api") }

// recordSeen notes that this run's worker is still alive.
//
// This route is the heartbeat because the worker already calls it on a fixed
// interval for the whole time a run is RUNNING — WatchCancel polls it to learn
// about a cancel. Recording that existing call is what lets the stale-run
// reaper tell a SIGKILLed worker from a slow one in minutes instead of at the
// run timeout. No other worker route stamps: the streaming one fires many times
// a second, and the terminal PATCH would move the timestamp past the moment the
// work stopped.
//
// A failure is logged and dropped. A worker asking whether it was canceled must
// not be refused over bookkeeping, and the run timeout still closes the run.
func (h *Handler) recordSeen(r *http.Request, run *coretask.Run) {
	if h.cfg.TaskRuns == nil {
		return
	}
	if err := h.cfg.TaskRuns.MarkTaskRunSeen(r.Context(), run.ID, time.Now().UTC()); err != nil {
		componentLog().Warn("worker handler: liveness not recorded", "task_run_id", run.ID, "err", err)
	}
}

// resolveSandboxTiers returns this run's sandbox tiers: the pinned ones once
// a worker has already claimed this run, or the agent's current declaration
// resolved against the space's default on the first poll. Mirrors
// resolvePluginPins's first-write-wins read — see
// docs/design/agent-sandbox-policy.md §4.4 and §9 M3.
//
// A tier the agent leaves undeclared falls through to the space's default for
// that axis, and only then to the surface baseline an empty string resolves
// to elsewhere. Each axis falls through independently, the same way
// ResolveSandboxForRun's layers do.
func (h *Handler) resolveSandboxTiers(r *http.Request, run *coretask.Run, task *coretask.Task, a *agentdef.Agent) (networkTier, filesystemTier string) {
	if run.SandboxNetworkTier != nil {
		if run.SandboxFilesystemTier != nil {
			filesystemTier = *run.SandboxFilesystemTier
		}
		return *run.SandboxNetworkTier, filesystemTier
	}
	if a == nil {
		return "", ""
	}
	networkTier, filesystemTier = a.SandboxNetworkTier, a.SandboxFilesystemTier
	if (networkTier == "" || filesystemTier == "") && h.cfg.Spaces != nil {
		space, err := h.cfg.Spaces.GetSpace(r.Context(), task.SpaceID)
		if err != nil {
			componentLog().Warn("worker handler: space sandbox defaults unavailable", "task_run_id", run.ID, "space_id", task.SpaceID, "err", err)
		} else if space != nil {
			if networkTier == "" {
				networkTier = space.DefaultSandboxNetworkTier
			}
			if filesystemTier == "" {
				filesystemTier = space.DefaultSandboxFilesystemTier
			}
		}
	}
	return networkTier, filesystemTier
}

// recordSandboxTiers notes which sandbox tiers this run resolved to.
//
// Recorded even when both tiers are empty — that is itself the fact "this
// run resolved to the strictest tier on both axes" — which is why the guard
// below is run.SandboxNetworkTier == nil rather than "the tiers are
// non-empty" the way recordPluginPins guards on. The resolved tiers are
// recorded, not the agent's raw declaration, so a run the space's default
// tier upgraded shows that in the audit trail rather than the empty
// declaration that started it. A failure to record is logged and dropped: a
// worker waiting for its run must not be held up by bookkeeping.
func (h *Handler) recordSandboxTiers(r *http.Request, run *coretask.Run, a *agentdef.Agent, networkTier, filesystemTier string) {
	if a == nil || run.SandboxNetworkTier != nil || h.cfg.TaskRuns == nil {
		return
	}
	if err := h.cfg.TaskRuns.RecordTaskRunSandboxTiers(r.Context(), run.ID, networkTier, filesystemTier); err != nil {
		componentLog().Warn("worker handler: sandbox tiers not recorded", "task_run_id", run.ID, "err", err)
	}
}

// recordAgentRevision notes which definition this run was handed.
//
// Instructions are resolved on every poll so an edit takes effect on the next
// run; the record is written once so an edit during this one cannot rewrite what
// already ran. A failure to record is logged and dropped: a worker waiting for
// its instructions must not be held up by bookkeeping.
func (h *Handler) recordAgentRevision(r *http.Request, run *coretask.Run, revision int) {
	if run.AgentRevision != nil || revision <= 0 || h.cfg.TaskRuns == nil {
		return
	}
	if err := h.cfg.TaskRuns.RecordTaskRunAgentRevision(r.Context(), run.ID, revision); err != nil {
		componentLog().Warn("worker handler: agent revision not recorded", "task_run_id", run.ID, "revision", revision, "err", err)
	}
}

func (h *Handler) recordSpaceAgentInstructionsRevision(r *http.Request, run *coretask.Run, revision int) {
	if run.SpaceAgentInstructionsRevision != nil || h.cfg.TaskRuns == nil {
		return
	}
	if err := h.cfg.TaskRuns.RecordTaskRunSpaceAgentInstructionsRevision(r.Context(), run.ID, revision); err != nil {
		componentLog().Warn("worker handler: space agent instructions revision not recorded", "task_run_id", run.ID, "err", err)
	}
}
