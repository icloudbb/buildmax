// Package workerclient defines the worker API client and HTTP contract types.
package workerclient

import (
	"time"
)

// GetTaskRunResponse is the JSON response for GET /api/worker/task-runs/{task_run_id} (snake_case).
type GetTaskRunResponse struct {
	Run  TaskRunRun  `json:"run"`
	Task TaskRunTask `json:"task"`
	LLM  *TaskRunLLM `json:"llm,omitempty"`
	// Plugins are the releases this run materializes, resolved by the server
	// when the worker claimed the run. A worker does not resolve its own: it
	// receives a finished list. Empty when the run's agent names none, or when
	// there is no agent.
	Plugins []TaskRunPlugin `json:"plugins,omitempty"`
	// PluginError is why this run cannot proceed — a named plugin its space has
	// not activated, or whose activation is suspended. A worker that receives
	// it must fail the run rather than start it: an agent that names a plugin
	// has declared it needs one, and a background run doing quietly less than
	// its definition says is read by somebody who was not watching it.
	PluginError string `json:"plugin_error,omitempty"`
	// Sandbox declares this run's agent-declared network/filesystem sandbox
	// tiers, resolved by the server when the worker claimed the run. Absent
	// means both tiers are the strictest, so a worker built before this
	// field existed applies the SandboxSurfaceWorker baseline it always did.
	// See docs/design/agent-sandbox-policy.md.
	Sandbox *TaskRunSandbox `json:"sandbox,omitempty"`
}

// TaskRunSandbox is the sandbox portion of the GET response.
type TaskRunSandbox struct {
	NetworkTier    string `json:"network_tier,omitempty"`
	FilesystemTier string `json:"filesystem_tier,omitempty"`
}

// TaskRunSecretsResponse carries a run's resolved Secret env grants: the
// variable names its agent declared, mapped to the values the server decrypted.
// It is fetched on its own route, not folded into GetTaskRunResponse, so the
// values ride a response that is Cache-Control: no-store and never logged. See
// docs/design/space-secrets.md §7.
type TaskRunSecretsResponse struct {
	Env map[string]string `json:"env,omitempty"`
}

// TaskRunPlugin is one release a run will fetch and verify. The digest is what
// the worker checks the bytes against before it extracts them.
type TaskRunPlugin struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

// TaskRunLLM tells a worker how to reach a model for this run.
//
// It is deliberately thin. A managed run learns a model name and nothing else —
// no endpoint, no upstream model identifier, no credential — because those stay
// inside the server's authorization boundary. A direct run learns nothing here
// and reads its model from the server.yaml it already mounts.
//
// Absent means direct, so a worker built before this field behaves as it always
// did.
type TaskRunLLM struct {
	// Transport is "direct" or "buildmax".
	Transport string `json:"transport"`
	// Model is the catalog model to call. Empty uses the deployment default.
	Model string `json:"model,omitempty"`
	// ContextWindow is the usable context size for the model; 0 disables
	// windowing.
	ContextWindow int `json:"context_window,omitempty"`
	// CallTimeout bounds one call, in seconds; 0 uses the client default.
	CallTimeout int `json:"call_timeout,omitempty"`
}

// TaskRunRun is the run portion of the GET response.
type TaskRunRun struct {
	ID                string  `json:"id"`
	TaskID            string  `json:"task_id"`
	PreviousTaskRunID *string `json:"previous_task_run_id,omitempty"`
	Input             string  `json:"input"`
	Status            string  `json:"status"`
	// CancelRequested is true once someone has asked this run to stop. The
	// worker polls for it and is what actually stops: the server records the
	// intent, the run's own process ends it. Absent means no request, so a
	// worker built before cancellation existed reads what it always did.
	CancelRequested bool      `json:"cancel_requested,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

// TaskRunTask is the task portion of the GET response.
type TaskRunTask struct {
	ID             string  `json:"id"`
	ConversationID string  `json:"conversation_id"`
	SpaceID        string  `json:"space_id"`
	UserID         string  `json:"user_id"`
	SessionID      *string `json:"session_id,omitempty"`
	// AgentInstructions is the instruction text of the agent this task names, resolved by
	// the server. The worker appends it to the run's system prompt, which is re-sent whole on
	// every call, rather than leaving it in the task input, which the conversation eventually
	// compacts away.
	//
	// It travels here rather than on the worker's command line for the same reason the run
	// token does: argv is readable by every process on the machine, and this is text a user
	// wrote, which may carry something they would not publish.
	//
	// Absent means the task names no agent, or a server built before this field existed, so
	// a worker reads it as it always did.
	AgentInstructions string `json:"agent_instructions,omitempty"`
	// SpaceAgentInstructions is the Space-level guidance inherited by every
	// background agent run. Revision identifies the version recorded on this
	// TaskRun. Both are absent when the space has never configured the layer.
	SpaceAgentInstructions         string `json:"space_agent_instructions,omitempty"`
	SpaceAgentInstructionsRevision int    `json:"space_agent_instructions_revision,omitempty"`
}

// PatchTaskRunRequest is the JSON body for PATCH /api/worker/task-runs/{task_run_id} (snake_case).
type PatchTaskRunRequest struct {
	Status    string     `json:"status"`
	SessionID *string    `json:"session_id,omitempty"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Output    *string    `json:"output,omitempty"`
	// Structured is the validated structured-output value as JSON text, sent on a
	// terminal report when the run requested an output schema and it validated.
	// See docs/design/structured-output.md.
	Structured       *string `json:"structured,omitempty"`
	ErrorMessage     *string `json:"error_message,omitempty"`
	PromptTokens     *int    `json:"prompt_tokens,omitempty"`
	CompletionTokens *int    `json:"completion_tokens,omitempty"`
	// TracePath locates the run's durable trace inside run-global storage, e.g.
	// "traces/<session>/rt_….jsonl". Sent on both success and failure; omitted
	// when no trace was written.
	TracePath *string `json:"trace_path,omitempty"`
	// WorkspaceCheckpoint is the result checkpoint the worker captured from
	// workspace/ after execution and uploaded to the object store, carried on the
	// terminal report so the server commits it as it accepts the outcome. Nil
	// when the run captured none — it produced no checkpoint, or capture failed
	// (which does not fail the run). See
	// docs/design/task-workspace-checkpoints.md §8.
	WorkspaceCheckpoint *WorkspaceCheckpointDescriptor `json:"workspace_checkpoint,omitempty"`
}

// WorkspaceCheckpointDescriptor is the format, digest, and counters of a
// checkpoint payload the worker has already uploaded. The server derives space,
// task, and run from the run token and the payload key from the digest; the
// worker names no key. It is the terminal-report counterpart of the seed's
// SeedCheckpointRequest.
type WorkspaceCheckpointDescriptor struct {
	PayloadFormat     string `json:"payload_format"`
	PayloadSHA256     string `json:"payload_sha256"`
	SizeBytes         int64  `json:"size_bytes"`
	UncompressedBytes int64  `json:"uncompressed_bytes"`
	EntryCount        int64  `json:"entry_count"`
}

// StreamDeltaRequest is the JSON body for POST /api/worker/task-runs/{task_run_id}/stream (snake_case).
type StreamDeltaRequest struct {
	Delta string `json:"delta"`
}

// Run status values match coretask.RunStatus (PENDING, SCHEDULED, RUNNING, SUCCEEDED, FAILED, CANCELED).
// Use coretask.RunStatusPending, coretask.RunStatusScheduled, etc. when building or comparing status.
