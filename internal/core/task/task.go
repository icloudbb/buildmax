package task

import (
	"context"
	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	"time"
)

// RunStatus is the canonical lifecycle status for task runs.
type RunStatus string

const (
	RunStatusPending   RunStatus = "PENDING"
	RunStatusScheduled RunStatus = "SCHEDULED"
	RunStatusRunning   RunStatus = "RUNNING"
	RunStatusSucceeded RunStatus = "SUCCEEDED"
	RunStatusFailed    RunStatus = "FAILED"
	// RunStatusCanceled is terminal and distinct from FAILED: nothing went
	// wrong, someone stopped the run. A canceled run keeps whatever output and
	// artifacts it had produced by then.
	RunStatusCanceled RunStatus = "CANCELED"
)

// RunStatusTerminal reports whether a run in this status has finished. A run
// leaves a non-terminal status only through its worker, the scheduler, or a
// cancel; a terminal one never changes again.
func RunStatusTerminal(status string) bool {
	switch RunStatus(status) {
	case RunStatusSucceeded, RunStatusFailed, RunStatusCanceled:
		return true
	default:
		return false
	}
}

// ValidRunStatusTransition reports whether a run may move directly from one
// status to another. Terminal statuses are immutable.
func ValidRunStatusTransition(from, to RunStatus) bool {
	switch from {
	case RunStatusPending:
		return to == RunStatusScheduled || to == RunStatusCanceled
	case RunStatusScheduled:
		return to == RunStatusRunning || to == RunStatusFailed || to == RunStatusCanceled
	case RunStatusRunning:
		return to == RunStatusSucceeded || to == RunStatusFailed || to == RunStatusCanceled
	default:
		return false
	}
}

// ActiveRunStatuses returns the statuses a run passes through before it
// finishes. It returns a fresh slice so callers cannot mutate the lifecycle
// definition for the rest of the process.
func ActiveRunStatuses() []string {
	return []string{
		string(RunStatusPending),
		string(RunStatusScheduled),
		string(RunStatusRunning),
	}
}

const (
	RunCreatedByTypeUser    = "user"
	RunCreatedByTypeWebhook = "webhook"
	RunCreatedByTypeSystem  = "system"
)

const (
	RunTriggerSourceTaskCreate = "task_create"
	RunTriggerSourceTaskRerun  = "task_rerun"
	// RunTriggerSourceTaskRetry marks a run that repeats an earlier one's
	// input rather than carrying new instructions. It is distinct from a rerun
	// because "this was run again unchanged" and "someone asked for something
	// else" are different answers to why a run exists.
	RunTriggerSourceTaskRetry          = "task_retry"
	RunTriggerSourcePortalConversation = "portal_conversation"
	RunTriggerSourcePortalTaskCreate   = "portal_task_create"
	RunTriggerSourcePortalTaskRerun    = "portal_task_rerun"
	RunTriggerSourceIssueAgentRun      = "issue_agent_run"
	RunTriggerSourceWorkflowStep       = "workflow_step"
	RunTriggerSourceWebhook            = "webhook"
	// RunTriggerSourceSchedule marks a run a recurring time trigger admitted.
	// The Task also carries the schedule's id as an origin relation, so "why did
	// this run" points at a schedule rather than a person. See
	// docs/design/scheduled-agent-execution.md.
	RunTriggerSourceSchedule = "schedule"
)

// Task holds the user-visible state for a background task.
type Task struct {
	ID string `json:"id"`
	// ConversationID is an optional projection target for a task started from
	// a foreground conversation. It is never the task's ownership boundary.
	ConversationID string  `json:"conversation_id,omitempty"`
	SpaceID        string  `json:"space_id"`
	IssueID        *string `json:"issue_id,omitempty"`
	// ScheduleID is an optional origin relation for a Task a recurring time
	// trigger created. Like ConversationID and IssueID it is an origin, never the
	// Task's ownership or authorization boundary.
	ScheduleID            *string `json:"schedule_id,omitempty"`
	Status                string  `json:"status"`
	Input                 string  `json:"input"`
	Title                 string  `json:"title,omitempty"`
	TitlePromptTokens     int     `json:"title_prompt_tokens,omitempty"`
	TitleCompletionTokens int     `json:"title_completion_tokens,omitempty"`
	Output                *string `json:"output,omitempty"`
	// OutputSchema is a JSON Schema (in the shared subset) that this Task's runs
	// must satisfy as their final answer. Set by a caller that needs a
	// machine-readable result — a Workflow node with an output_schema; nil for a
	// free-text Task. It rides the Task because Continue reuses it across runs.
	// See docs/design/structured-output.md.
	OutputSchema *string    `json:"output_schema,omitempty"`
	CreatedBy    string     `json:"created_by"`
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
	ErrorMessage *string    `json:"error_message,omitempty"`
	SessionID    *string    `json:"session_id,omitempty"`
	LastRunID    *string    `json:"last_run_id,omitempty"`
	AgentID      *string    `json:"agent_id,omitempty"`
	// WorkspaceHeadCheckpointID points at the latest checkpoint accepted as this
	// Task's recoverable workspace: its initial seed, then each successful
	// result. It is a database pointer among immutable checkpoints, never a
	// mutable object-store key, and nil until the first run commits a seed or
	// result. See docs/design/task-workspace-checkpoints.md §9.2.
	WorkspaceHeadCheckpointID *string `json:"workspace_head_checkpoint_id,omitempty"`
	// PluginEnvironmentHeadID points at the immutable Plugin environment
	// revision the next Continue uses. Nil for a Task that derives its
	// environment from the Agent revision and Space activation with no
	// autonomous install.
	PluginEnvironmentHeadID *string `json:"plugin_environment_head_id,omitempty"`
}

// Run is one execution (initial or follow-up) of a task.
type Run struct {
	ID     string `json:"id"`
	TaskID string `json:"task_id"`
	// PreviousTaskRunID names the immediately preceding run in this Task's
	// linear history. It is fixed when the run is created, so later updates to
	// Task.LastRunID cannot change where this run restores its session from.
	// Nil for the Task's first run.
	PreviousTaskRunID *string `json:"previous_task_run_id,omitempty"`
	Input             string  `json:"input"`
	CreatedBy         string  `json:"created_by,omitempty"`
	CreatedByType     string  `json:"created_by_type,omitempty"`
	TriggerSource     string  `json:"trigger_source,omitempty"`
	Status            string  `json:"status"`
	Output            *string `json:"output,omitempty"`
	// Structured is the validated machine-readable answer as JSON text, set only
	// when the run requested an output schema and the value validated. Nil for a
	// free-text run, or when the model's answer did not satisfy the schema. See
	// docs/design/structured-output.md.
	Structured       *string    `json:"structured,omitempty"`
	ErrorMessage     *string    `json:"error_message,omitempty"`
	StartedAt        *time.Time `json:"started_at,omitempty"`
	EndedAt          *time.Time `json:"ended_at,omitempty"`
	SessionID        *string    `json:"session_id,omitempty"`
	WorkerType       string     `json:"worker_type,omitempty"`
	K8sJobName       *string    `json:"k8s_job_name,omitempty"`
	K8sJobCreatedAt  *time.Time `json:"k8s_job_created_at,omitempty"`
	PromptTokens     *int       `json:"prompt_tokens,omitempty"`
	CompletionTokens *int       `json:"completion_tokens,omitempty"`
	// TracePath locates this run's durable trace inside run-global storage,
	// e.g. "traces/<session>/rt_….jsonl". Nil when no trace was written — the
	// run failed before an agent started, or tracing was disabled.
	TracePath *string `json:"trace_path,omitempty"`
	// CancelRequestedAt is when someone asked this run to stop. A cancel is
	// recorded rather than applied because the only thing that can stop a
	// started run is its own worker: the server states the intent, the worker
	// honors it and reports CANCELED. Nil means nobody has asked.
	CancelRequestedAt *time.Time `json:"cancel_requested_at,omitempty"`
	// CancelRequestedBy is the user who asked. A space's runs can be stopped by
	// anyone on the space, so "why did this stop" needs a name to answer.
	CancelRequestedBy *string `json:"cancel_requested_by,omitempty"`
	// RetryOfTaskRunID names the run this one repeats. Nil for every run that
	// carries its own instructions. The lineage is one level deep by record but
	// unbounded by use: retrying a retry points at the run it repeated, not at
	// the first of the chain.
	RetryOfTaskRunID *string `json:"retry_of_task_run_id,omitempty"`
	// AgentRevision numbers the agent definition this run was actually given.
	//
	// The definition is resolved when a worker asks for its run, not when the
	// task was created, so an edit takes effect on the next run. That is what
	// someone editing the field expects and it is also why this is recorded: a
	// run's instructions are otherwise whatever the agent says today, and no
	// record says which text produced this outcome. Nil for a run with no agent
	// and for runs that predate the column.
	AgentRevision *int `json:"agent_revision,omitempty"`
	// SpaceAgentInstructionsRevision numbers the Space-level instruction text
	// this run received. Revision 0 records that no Space instructions were
	// configured; nil means the run predates this provenance or the space could
	// not be resolved at dispatch.
	SpaceAgentInstructionsRevision *int `json:"space_agent_instructions_revision,omitempty"`
	// PluginPins are the releases this run was given, resolved when its worker
	// claimed it and fixed from that moment.
	//
	// Recorded for the reason AgentRevision is: afterwards nothing else can say
	// which versions this run actually had. The trace says so too, but a trace
	// is fail-open and lives in run-global storage, while this is the queryable
	// fact and what a retry reads. Nil for a run that resolved no plugins.
	PluginPins []coreplugin.Pin `json:"plugin_pins,omitempty"`
	// SandboxNetworkTier and SandboxFilesystemTier are this run's agent-
	// declared sandbox tiers, resolved and recorded at the same moment as
	// AgentRevision and PluginPins, for the same reason: afterwards nothing
	// else can say what boundary this run actually had, even if the agent's
	// declared tier changes later. Nil for a run with no agent, one that
	// predates this column, or one whose agent declared no tier on that
	// axis. See docs/design/agent-sandbox-policy.md §4.4.
	SandboxNetworkTier    *string `json:"sandbox_network_tier,omitempty"`
	SandboxFilesystemTier *string `json:"sandbox_filesystem_tier,omitempty"`
	// SourceMessageID names the conversation message this run was asked for in.
	//
	// Input is what Tier 1 decided to send a worker; this is what the person
	// actually said. They are not the same text and the difference is the point:
	// without it, nobody can tell a constraint the model dropped from one the
	// user never gave. Nil for a run with no message behind it — a workflow
	// step, an issue agent run, a retry, or a task created straight from the API.
	SourceMessageID *string `json:"source_message_id,omitempty"`
	// LastSeenAt is when this run's worker last called a route scoped to it.
	//
	// A worker polls its own run every few seconds for the whole time it is
	// RUNNING, so a run that stops reporting has lost its worker. Recording the
	// poll it already makes is what turns that into an observation: without it
	// a SIGKILLed worker is indistinguishable from a slow one until the run
	// timeout, hours later. Nil for a run no worker has claimed yet.
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	// IdempotencyKey is the caller's dedup key for this run, when it supplied
	// one. Unique per task: a Continue request that repeats a key gets back
	// this same run rather than a second one. Nil for a run created without a
	// key — a retry, a workflow step, an issue agent run, or an older client.
	IdempotencyKey *string `json:"idempotency_key,omitempty"`

	// Workspace checkpoint provenance. See
	// docs/design/task-workspace-checkpoints.md §9.3. The base is the immutable
	// workspace this run was authorized to read and modify, fixed before
	// execution; the result and partial are what it captured. The status fields
	// exist because a missing pointer alone cannot tell "not requested" from
	// "attempted and failed". Errors are bounded operator-facing text.
	WorkspaceBaseCheckpointID    *string `json:"workspace_base_checkpoint_id,omitempty"`
	WorkspaceResultCheckpointID  *string `json:"workspace_result_checkpoint_id,omitempty"`
	WorkspacePartialCheckpointID *string `json:"workspace_partial_checkpoint_id,omitempty"`
	WorkspaceRestoreStatus       string  `json:"workspace_restore_status,omitempty"`
	WorkspaceRestoreError        *string `json:"workspace_restore_error,omitempty"`
	WorkspaceCheckpointStatus    string  `json:"workspace_checkpoint_status,omitempty"`
	WorkspaceCheckpointError     *string `json:"workspace_checkpoint_error,omitempty"`
	// Plugin environment provenance. The base is the immutable Plugin set this
	// run materialized; the result is a new set a committed autonomous install
	// requested, which takes effect on the next TaskRun boundary.
	PluginEnvironmentBaseID   *string `json:"plugin_environment_base_id,omitempty"`
	PluginEnvironmentResultID *string `json:"plugin_environment_result_id,omitempty"`
	PluginEnvironmentStatus   string  `json:"plugin_environment_status,omitempty"`
	PluginEnvironmentError    *string `json:"plugin_environment_error,omitempty"`
}

// RunTerminalInfo describes a task run that reached a terminal state.
// Used by the workflow service to advance or finalize workflow step runs.
type RunTerminalInfo struct {
	TaskRunID      string
	TaskID         string
	ConversationID string
	// SpaceID is the space that owns the task.
	SpaceID      string
	UserID       string
	Status       string
	Output       *string
	ErrorMessage *string
}

// CreateInput is the input for CreateTask.
type CreateInput struct {
	ConversationID          string
	SpaceID                 string
	Input                   string
	Title                   string
	CreatedBy               string
	InitialRunCreatedBy     string
	InitialRunCreatedByType string
	InitialRunTriggerSource string
	// InitialRunSourceMessageID names the message that asked for this task.
	InitialRunSourceMessageID       *string
	TitlePromptTokens               int
	TitleCompletionTokens           int
	AgentID                         *string
	InitialRunAgentRevision         *int
	InitialRunSandboxNetworkTier    *string
	InitialRunSandboxFilesystemTier *string
	IssueID                         *string
	ScheduleID                      *string
	// AdmissionKey makes task creation idempotent for a caller that owns a
	// durable objective and may replay its dispatch — a Workflow node whose
	// coordinator can crash between admitting the Task and recording the link.
	// Empty (the common case) creates a task unconditionally. Non-empty binds
	// the task under a unique key scoped to its space, so a replay returns the
	// first call's task instead of a duplicate. See AdmitTask and
	// docs/design/workflow-runtime.md §11.
	AdmissionKey string
	// OutputSchema is a JSON Schema (shared subset) the task's runs must satisfy
	// as their final answer, or nil for free text. See
	// docs/design/structured-output.md.
	OutputSchema *string
}

// UpdateInput updates a task to the given status with optional fields.
type UpdateInput struct {
	TaskID       string
	Status       string
	StartedAt    *time.Time
	EndedAt      *time.Time
	Output       *string
	ErrorMessage *string
	SessionID    *string
}

// ClaimInput atomically transitions a task from ExpectedStatus to NewStatus.
type ClaimInput struct {
	TaskID         string
	ExpectedStatus string
	NewStatus      string
	StartedAt      *time.Time
	EndedAt        *time.Time
	Output         *string
	ErrorMessage   *string
	SessionID      *string
}

// TransitionRunInput atomically moves a run from ExpectedStatus to
// NewStatus and projects the accepted state onto its task.
type TransitionRunInput struct {
	TaskRunID        string
	ExpectedStatus   RunStatus
	NewStatus        RunStatus
	StartedAt        *time.Time
	EndedAt          *time.Time
	Output           *string
	Structured       *string
	ErrorMessage     *string
	SessionID        *string
	PromptTokens     *int
	CompletionTokens *int
	TracePath        *string
}

// Store provides task persistence. Tasks belong to a space and may optionally
// retain the conversation that requested them.
// CreateTask creates a task plus its first Run (both in one transaction).
type Store interface {
	// ListTasksByConversation returns tasks in the conversation. order is "asc" (oldest first) or "desc" (latest first); default "desc".
	ListTasksByConversation(ctx context.Context, conversationID string, order string) ([]Task, error)
	// ListTasksByConversationPaginated returns tasks with optional executed_only filter, ordered by created_at DESC. total is total matching count.
	ListTasksByConversationPaginated(ctx context.Context, conversationID string, executedOnly bool, limit, offset int) ([]Task, int, error)
	ListTasksByIssue(ctx context.Context, issueID string, limit, offset int) ([]Task, int, error)
	ListTasksByAgent(ctx context.Context, spaceID, agentID string, limit, offset int) ([]Task, int, error)
	// ListTasksBySchedule returns the tasks a recurring schedule created, newest
	// first, scoped to the space so a schedule id cannot read another space's
	// tasks. total is the count ignoring limit and offset.
	ListTasksBySchedule(ctx context.Context, spaceID, scheduleID string, limit, offset int) ([]Task, int, error)
	GetTask(ctx context.Context, taskID string) (*Task, error)
	GetTaskBySessionID(ctx context.Context, sessionID string) (*Task, error)
	// CreateTask creates a new task and its first Run (input, title, PENDING). Returns the task with last_run_id set.
	CreateTask(ctx context.Context, in *CreateInput) (*Task, error)
	// AdmitTask idempotently creates a task and its first Run for in.AdmissionKey,
	// which must be non-empty and is unique within the task's space. The first
	// call creates them; a replay with the same key and an identical admitted
	// payload returns that same task; a replay with a conflicting payload returns
	// ErrTaskAdmissionConflict. It is the durable-recovery admission a Workflow
	// node dispatch uses so a retried or concurrent dispatch cannot duplicate
	// execution. See docs/design/workflow-runtime.md §11.
	AdmitTask(ctx context.Context, in *CreateInput) (*Task, error)
	UpdateTask(ctx context.Context, in UpdateInput) error
	ClaimTask(ctx context.Context, in ClaimInput) (updated bool, err error)
}

// CreateRunInput describes a new run on an existing task.
type CreateRunInput struct {
	TaskID        string
	Input         string
	CreatedBy     string
	CreatedByType string
	TriggerSource string
	// RetryOfTaskRunID names the run this one repeats, when it repeats one.
	RetryOfTaskRunID *string
	// SourceMessageID names the conversation message that asked for this run.
	SourceMessageID       *string
	AgentRevision         *int
	SandboxNetworkTier    *string
	SandboxFilesystemTier *string
	// IdempotencyKey scopes this request against the task's other runs: a
	// second CreateTaskRun for the same task with the same key returns the run
	// the first call created instead of starting another one. Nil (the common
	// case) creates a new run unconditionally, the same as before this field
	// existed.
	IdempotencyKey *string
}

// RunStore provides task run persistence.
// RunTraceRef locates one run's durable trace: the run whose pointer to clear,
// and the space/task/run coordinates a storage backend needs to remove the
// object. It is what a retention sweep reads instead of a whole Run.
type RunTraceRef struct {
	SpaceID   string
	TaskID    string
	TaskRunID string
	TracePath string
}

type RunStore interface {
	// CreateTaskRun creates a new run (PENDING). Returns ErrRunInProgress if the task has any run in PENDING/SCHEDULED/RUNNING.
	CreateTaskRun(ctx context.Context, in CreateRunInput) (*Run, error)
	// CountTaskRunsByStatus returns how many runs are in each status. It is
	// the one number that answers "is work flowing through this deployment",
	// and it carries no space, input, or output — only counts.
	CountTaskRunsByStatus(ctx context.Context) (map[string]int, error)
	// GetNextPendingTaskRun returns the oldest run with status PENDING (by created_at), or (nil, nil) if none.
	GetNextPendingTaskRun(ctx context.Context) (*Run, error)
	GetTaskRun(ctx context.Context, taskRunID string) (*Run, error)
	// ListTaskRunsByTask returns every turn and attempt in chronological order.
	ListTaskRunsByTask(ctx context.Context, taskID string) ([]Run, error)
	// GetTaskRunWithTask returns the run and its task, or (nil, nil, nil) if run not found.
	GetTaskRunWithTask(ctx context.Context, taskRunID string) (*Run, *Task, error)
	// ListTaskRunIDsByTasks returns each task's run IDs, newest first, keyed by
	// task ID. Tasks with no runs are absent from the map.
	//
	// It exists because a task's last run is not its only run: a retried task
	// has earlier ones, and what those produced did not stop existing.
	ListTaskRunIDsByTasks(ctx context.Context, taskIDs []string) (map[string][]string, error)
	// GetActiveTaskRunByTask returns the task's run in PENDING, SCHEDULED, or
	// RUNNING, or (nil, nil) when the task has none. A task holds at most one.
	GetActiveTaskRunByTask(ctx context.Context, taskID string) (*Run, error)
	// RequestTaskRunCancel records who asked a run to stop, and when, on a run
	// that has not reached a terminal status. Returns false when the run is
	// already terminal or already carries a request, so a second cancel
	// neither resets the clock the backstop measures nor overwrites the name
	// of whoever asked first.
	RequestTaskRunCancel(ctx context.Context, taskRunID, requestedBy string, requestedAt time.Time) (bool, error)
	// TransitionTaskRun atomically updates a run only when its current status
	// matches ExpectedStatus, then updates the task projection in the same
	// transaction. A false result means another actor won the transition.
	TransitionTaskRun(ctx context.Context, in TransitionRunInput) (bool, error)
	UpdateTaskRunWorkerInfo(ctx context.Context, taskRunID, workerType string, k8sJobName *string, k8sJobCreatedAt *time.Time) error
	// MarkTaskRunSeen records that this run's worker is still reporting. It
	// writes only while the run is active, so a terminal run's last signal
	// stays the one it gave while it was working.
	MarkTaskRunSeen(ctx context.Context, taskRunID string, seenAt time.Time) error
	// RecordTaskRunAgentRevision stores which agent definition a run was given.
	// The first write wins: a run executes under the instructions it was handed
	// at dispatch, and a later edit does not retroactively change what ran.
	RecordTaskRunAgentRevision(ctx context.Context, taskRunID string, revision int) error
	// RecordTaskRunSpaceAgentInstructionsRevision stores which Space-level
	// instruction revision a run was given. The first write wins.
	RecordTaskRunSpaceAgentInstructionsRevision(ctx context.Context, taskRunID string, revision int) error
	// RecordTaskRunPluginPins stores the releases a run was given. Like the
	// agent revision, the first write wins: a worker polls its run, and a
	// space's activation edited mid-run must not rewrite what actually ran.
	RecordTaskRunPluginPins(ctx context.Context, taskRunID string, pins []coreplugin.Pin) error
	// RecordTaskRunSandboxTiers stores the agent-declared sandbox tiers a run
	// was given. Like the agent revision, the first write wins, and it is
	// written even when both tiers are empty -- see Run.SandboxNetworkTier.
	RecordTaskRunSandboxTiers(ctx context.Context, taskRunID string, networkTier, filesystemTier string) error
	// ListTaskRunsWithExpiredTrace returns runs that ended on or before cutoff
	// and still point at a trace, oldest first, up to limit. It drives trace
	// retention: a run with no EndedAt or no TracePath is never returned, so an
	// in-flight run's trace is never a candidate.
	ListTaskRunsWithExpiredTrace(ctx context.Context, cutoff time.Time, limit int) ([]RunTraceRef, error)
	// ClearTaskRunTracePath sets a run's trace pointer to NULL after its trace
	// has been removed, so the run reports that it has no trace rather than one
	// that fails to load. Clearing a run that already has none is not an error.
	ClearTaskRunTracePath(ctx context.Context, taskRunID string) error
}
