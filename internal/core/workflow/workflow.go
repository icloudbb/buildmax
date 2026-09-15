package workflow

import (
	"context"
	"encoding/json"
	"time"
)

const (
	StatusDraft     = "draft"
	StatusPublished = "published"
	StatusArchived  = "archived"

	NodeTypeAgentTask = "agent_task"

	// DefinitionSchemaVersion is the only workflow definition contract version the
	// runtime accepts. A definition must declare it explicitly; publication rejects
	// any other value so a stored plan always names the contract it was written for.
	DefinitionSchemaVersion = 1
)

// RunStatus is the lifecycle status of one workflow run. NodeRunStatus is one
// step's status within that run. Both are the canonical execution-plane state
// machine for workflows, the analog of coretask.RunStatus, and every move
// between their values goes through the transition helpers below so an illegal
// or concurrent change is refused at the store rather than silently written.
type RunStatus string

type NodeRunStatus string

const (
	RunStatusPending   RunStatus = "pending"
	RunStatusRunning   RunStatus = "running"
	RunStatusSucceeded RunStatus = "succeeded"
	RunStatusFailed    RunStatus = "failed"
	RunStatusCanceled  RunStatus = "canceled"
)

const (
	NodeRunStatusPending   NodeRunStatus = "pending"
	NodeRunStatusRunning   NodeRunStatus = "running"
	NodeRunStatusSucceeded NodeRunStatus = "succeeded"
	NodeRunStatusFailed    NodeRunStatus = "failed"
	NodeRunStatusCanceled  NodeRunStatus = "canceled"
	// NodeRunStatusBlocked is terminal: an earlier step ended badly, so this
	// still-pending step will never run.
	NodeRunStatusBlocked NodeRunStatus = "blocked"
)

// RunStatusTerminal reports whether a run in this status has finished; a
// terminal run never changes again.
func RunStatusTerminal(s RunStatus) bool {
	switch s {
	case RunStatusSucceeded, RunStatusFailed, RunStatusCanceled:
		return true
	default:
		return false
	}
}

// TerminalRunStatuses lists the statuses RunStatusTerminal reports true for, so
// a store can build the "non-terminal" filter the due-run query and the lease
// guards rest on without duplicating the set.
func TerminalRunStatuses() []RunStatus {
	return []RunStatus{RunStatusSucceeded, RunStatusFailed, RunStatusCanceled}
}

// NodeRunStatusTerminal reports whether a step run has finished. Blocked is
// terminal alongside the three natural ends: a blocked step is never revisited.
func NodeRunStatusTerminal(s NodeRunStatus) bool {
	switch s {
	case NodeRunStatusSucceeded, NodeRunStatusFailed, NodeRunStatusCanceled, NodeRunStatusBlocked:
		return true
	default:
		return false
	}
}

// ValidRunStatusTransition reports whether a run may move directly from one
// status to another. Terminal statuses are immutable. Runs are created running;
// pending is reserved for a run that has not yet been dispatched.
func ValidRunStatusTransition(from, to RunStatus) bool {
	switch from {
	case RunStatusPending:
		return to == RunStatusRunning || to == RunStatusFailed || to == RunStatusCanceled
	case RunStatusRunning:
		return to == RunStatusSucceeded || to == RunStatusFailed || to == RunStatusCanceled
	default:
		return false
	}
}

// ValidNodeRunTransition reports whether a step run may move directly from one
// status to another. A pending step may start (running), be blocked by an
// earlier failure, or fail outright when its task cannot be created; a running
// step ends succeeded, failed, or canceled. Terminal statuses are immutable.
func ValidNodeRunTransition(from, to NodeRunStatus) bool {
	switch from {
	case NodeRunStatusPending:
		return to == NodeRunStatusRunning || to == NodeRunStatusBlocked ||
			to == NodeRunStatusFailed || to == NodeRunStatusCanceled
	case NodeRunStatusRunning:
		return to == NodeRunStatusSucceeded || to == NodeRunStatusFailed || to == NodeRunStatusCanceled
	default:
		return false
	}
}

// Workflow is a reusable space-scoped execution plan.
type Workflow struct {
	ID          string `json:"id"`
	SpaceID     string `json:"space_id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Definition  string `json:"definition"`
	Status      string `json:"status"`
	// Revision numbers the workflow_revision row holding this content. It
	// starts at 1 and advances every time the name, description, definition,
	// or status changes.
	Revision  int       `json:"revision"`
	CreatedBy string    `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Revision is one recorded version of a workflow.
//
// Revisions are append-only: an edit adds one, nothing rewrites or deletes one,
// and restoring an older revision is itself an edit that appends a new one.
type Revision struct {
	WorkflowID  string    `json:"workflow_id"`
	Revision    int       `json:"revision"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Definition  string    `json:"definition"`
	Status      string    `json:"status"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

// Run is one execution attempt of a workflow.
type Run struct {
	ID         string `json:"id"`
	WorkflowID string `json:"workflow_id"`
	// WorkflowRevision is the workflow revision this run expanded. It is 0 for
	// runs started before workflows recorded revisions.
	WorkflowRevision int     `json:"workflow_revision,omitempty"`
	IssueID          *string `json:"issue_id,omitempty"`
	// Input is the run's immutable input JSON, validated against the definition's
	// input_schema at admission. Nil when the definition declares no input_schema.
	Input        *string    `json:"input,omitempty"`
	Status       string     `json:"status"`
	CreatedBy    string     `json:"created_by"`
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
	ErrorMessage *string    `json:"error_message,omitempty"`
	// Reconciliation scheduling and ownership. ReconcileOwner and LeaseExpiresAt
	// are a bounded lease that reduces duplicate reconciliation work; they are not
	// the correctness mechanism. NextReconcileAt is when this run next wants a
	// reconciliation pass. All three are cleared when the run becomes terminal.
	ReconcileOwner  *string    `json:"reconcile_owner,omitempty"`
	LeaseExpiresAt  *time.Time `json:"lease_expires_at,omitempty"`
	NextReconcileAt *time.Time `json:"next_reconcile_at,omitempty"`
}

// NodeRun is one durable node execution record under a workflow run. The linear
// precursor authors nodes as ordered `steps`; NodeID carries the authoring
// step's id, and NodeIndex is its position, so the graph term (node) names the
// runtime record while the definition keeps the `steps` shape until Phase 3.
type NodeRun struct {
	ID            string  `json:"id"`
	WorkflowRunID string  `json:"workflow_run_id"`
	NodeID        string  `json:"node_id"`
	NodeIndex     int     `json:"node_index"`
	NodeType      string  `json:"node_type"`
	TargetAgentID *string `json:"target_agent_id,omitempty"`
	// AgentName, AgentDescription, and AgentInstructions capture the target agent
	// definition as it was when the run started, so later edits to the agent cannot
	// change what a step in flight sends to the model.
	AgentName         string `json:"agent_name,omitempty"`
	AgentDescription  string `json:"agent_description,omitempty"`
	AgentInstructions string `json:"agent_instructions,omitempty"`
	AgentRevision     int    `json:"agent_revision,omitempty"`
	Prompt            string `json:"prompt"`
	// Bindings is the run's snapshot of this node's input bindings, taken at start
	// so a later definition edit cannot change what an in-flight node receives.
	Bindings []StepBinding `json:"bindings,omitempty"`
	// OutputSchema is the run's snapshot of this node's output schema, taken at
	// start so a later definition edit cannot change what an in-flight node must
	// satisfy. Nil for a free-text node.
	OutputSchema *string `json:"output_schema,omitempty"`
	Status       string  `json:"status"`
	TaskID       *string `json:"task_id,omitempty"`
	TaskRunID    *string `json:"task_run_id,omitempty"`
	// ResolvedInput is the full Task input this node received -- its prompt with
	// every binding materialized -- captured when the node started so the run
	// record shows exactly what the agent was given. Nil until the node is
	// dispatched.
	ResolvedInput *string `json:"resolved_input,omitempty"`
	// Output is the node's full output: the whole text its accepted TaskRun
	// produced, not a truncated summary, so downstream bindings and the run
	// result read it without re-reading the Task plane. Nil until the node
	// succeeds, or when it produced no text.
	Output *string `json:"output,omitempty"`
	// Structured is the validated structured-output value the accepted run
	// produced, as JSON text. Nil for a free-text step, or when the value did not
	// validate (which fails the step).
	Structured   *string    `json:"structured,omitempty"`
	ErrorMessage *string    `json:"error_message,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
}

// Definition is the parsed structure of a workflow definition JSON.
type Definition struct {
	// SchemaVersion names the definition contract this plan is written for. It must
	// equal DefinitionSchemaVersion; publication refuses anything else rather than
	// guessing an unversioned shape.
	SchemaVersion int `json:"schema_version"`
	// InputSchema, when set, is a JSON Schema (in the shared subset) that a run's
	// immutable input must satisfy at admission and that drives the Portal input
	// form. Absent means the run takes no declared input. Publication rejects a
	// schema outside the subset.
	InputSchema json.RawMessage  `json:"input_schema,omitempty"`
	Steps       []DefinitionStep `json:"steps"`
	// Result, when set, selects the WorkflowRun result from one step's output. The
	// selected step must exist. Absent leaves the run without a declared result.
	Result *ResultSelector `json:"result,omitempty"`
}

// ResultSelector names the step whose output becomes the WorkflowRun result.
type ResultSelector struct {
	FromStep string `json:"from_step"`
}

// DefinitionStep describes one step in a workflow definition.
type DefinitionStep struct {
	StepID        string `json:"step_id"`
	Type          string `json:"type"`
	TargetAgentID string `json:"target_agent_id"`
	Prompt        string `json:"prompt"`
	// Bindings feed an earlier step's output into this step's input. Each names a
	// value (Name) taken from the whole output of a prior step (FromStep). The
	// bound output reaches the Task as labelled untrusted context, never the
	// agent's instructions.
	Bindings []StepBinding `json:"bindings,omitempty"`
	// OutputSchema, when set, is a JSON Schema (in the shared subset) the step's
	// agent run must satisfy as its final answer. Publication rejects a schema
	// outside the subset. The step succeeds only when the run returns a value
	// that validates against it. Empty leaves the step free text. See
	// docs/design/structured-output.md.
	OutputSchema json.RawMessage `json:"output_schema,omitempty"`
}

// StepBinding binds one earlier step's output into a downstream step's input
// under a name. The whole upstream output is bound; there is no selection or
// templating in this contract.
type StepBinding struct {
	Name     string `json:"name"`
	FromStep string `json:"from_step"`
}

type CreateRunInput struct {
	WorkflowID       string
	WorkflowRevision int
	IssueID          *string
	// Input is the run's immutable input JSON, already validated against the
	// definition's input_schema. Nil when the definition declares no input_schema.
	Input     *string
	Status    string
	CreatedBy string
	StartedAt *time.Time
}

type UpdateInput struct {
	Name        *string
	Description *string
	Definition  *string
	Status      *string
	// ExpectedRevision is the revision the service observed before this edit. The
	// store guards the workflow row update on it and appends the next revision in
	// the same transaction, so a second writer that started from the same revision
	// loses the compare-and-set and gets ErrRevisionConflict rather than
	// overwriting the winner or leaking the duplicate-key error.
	ExpectedRevision int
	// UpdatedBy is recorded as the author of the revision this update appends.
	UpdatedBy string
}

type CreateNodeRunInput struct {
	NodeID            string
	NodeIndex         int
	NodeType          string
	TargetAgentID     *string
	AgentName         string
	AgentDescription  string
	AgentInstructions string
	AgentRevision     int
	Prompt            string
	Bindings          []StepBinding
	OutputSchema      *string
	Status            string
}

// TransitionRunInput atomically moves a run from ExpectedStatus to NewStatus.
// The store writes nothing unless the run's current status is ExpectedStatus
// and the move is a ValidRunStatusTransition.
type TransitionRunInput struct {
	WorkflowRunID  string
	ExpectedStatus RunStatus
	NewStatus      RunStatus
	StartedAt      *time.Time
	EndedAt        *time.Time
	ErrorMessage   *string
}

// TransitionNodeRunInput atomically moves a node run from ExpectedStatus to
// NewStatus, carrying the fields that land with a status change. The store
// writes nothing unless the node's current status is ExpectedStatus and the
// move is a ValidNodeRunTransition. ResolvedInput lands with the move to
// running; Output and Structured land with the move to succeeded.
type TransitionNodeRunInput struct {
	NodeRunID      string
	ExpectedStatus NodeRunStatus
	NewStatus      NodeRunStatus
	TaskID         *string
	TaskRunID      *string
	ResolvedInput  *string
	Output         *string
	Structured     *string
	ErrorMessage   *string
	StartedAt      *time.Time
	EndedAt        *time.Time
}

// FinalizeFailedRunInput ends a run because one step ended badly. In one
// transaction the store moves the step to NodeStatus (failed or canceled),
// blocks every later step still pending, and moves the run to RunStatus. Both
// moves are guarded: nothing is written unless the step is at NodeExpected and
// both transitions are valid.
type FinalizeFailedRunInput struct {
	WorkflowRunID string
	NodeRunID     string
	NodeIndex     int
	NodeExpected  NodeRunStatus
	NodeStatus    NodeRunStatus
	RunExpected   RunStatus
	RunStatus     RunStatus
	TaskRunID     *string
	ErrorMessage  *string
	StartedAt     *time.Time
	EndedAt       *time.Time
}

// ClaimLeaseInput acquires a reconciliation lease on a non-terminal run for
// Owner. The store writes nothing unless the run has no owner or its prior lease
// has expired at Now.
type ClaimLeaseInput struct {
	WorkflowRunID  string
	Owner          string
	Now            time.Time
	LeaseExpiresAt time.Time
}

// RenewLeaseInput extends the lease Owner already holds. A stale owner -- one a
// takeover has replaced -- matches nothing and gets a false result.
type RenewLeaseInput struct {
	WorkflowRunID  string
	Owner          string
	LeaseExpiresAt time.Time
}

// ReleaseLeaseInput clears the lease Owner holds and sets when the run next
// wants a pass. NextReconcileAt is nil to leave the run without a scheduled
// pass, waking only from a due sweep. A stale owner matches nothing.
type ReleaseLeaseInput struct {
	WorkflowRunID   string
	Owner           string
	NextReconcileAt *time.Time
}

// Store provides workflow and workflow execution persistence.
type Store interface {
	ListWorkflowsBySpace(ctx context.Context, spaceID string) ([]Workflow, error)
	CreateWorkflow(ctx context.Context, spaceID, createdBy, name, description, definition string) (*Workflow, error)
	GetWorkflow(ctx context.Context, workflowID string) (*Workflow, error)
	UpdateWorkflow(ctx context.Context, workflowID, spaceID string, in UpdateInput) (*Workflow, error)
	CreateWorkflowRun(ctx context.Context, in CreateRunInput) (*Run, error)
	ListWorkflowRunsByWorkflow(ctx context.Context, workflowID string, limit, offset int) ([]Run, int, error)
	ListWorkflowRunsByIssue(ctx context.Context, issueID string, limit, offset int) ([]Run, int, error)
	GetWorkflowRun(ctx context.Context, workflowRunID string) (*Run, error)
	ListWorkflowNodeRuns(ctx context.Context, workflowRunID string) ([]NodeRun, error)
	CreateWorkflowNodeRuns(ctx context.Context, workflowRunID string, steps []CreateNodeRunInput) ([]NodeRun, error)
	// TransitionWorkflowRun and TransitionWorkflowNodeRun apply one guarded
	// status change each; a false result means the row was not at the expected
	// status, so another actor won the transition. FinalizeFailedWorkflowRun
	// ends a run and blocks its remaining steps in one transaction.
	TransitionWorkflowRun(ctx context.Context, in TransitionRunInput) (bool, error)
	TransitionWorkflowNodeRun(ctx context.Context, in TransitionNodeRunInput) (bool, error)
	FinalizeFailedWorkflowRun(ctx context.Context, in FinalizeFailedRunInput) (bool, error)
	// ListDueWorkflowRuns returns non-terminal runs that need a reconciliation
	// pass at now -- their scheduled time has arrived or their lease expired --
	// in stable oldest-due order, bounded by limit (a documented default when
	// limit <= 0). ClaimWorkflowRunLease, RenewWorkflowRunLease, and
	// ReleaseWorkflowRunLease are guarded writes: a false result means the run was
	// terminal, already leased to another unexpired owner, or held by someone
	// else, so this caller did not win the lease.
	ListDueWorkflowRuns(ctx context.Context, now time.Time, limit int) ([]Run, error)
	ClaimWorkflowRunLease(ctx context.Context, in ClaimLeaseInput) (bool, error)
	RenewWorkflowRunLease(ctx context.Context, in RenewLeaseInput) (bool, error)
	ReleaseWorkflowRunLease(ctx context.Context, in ReleaseLeaseInput) (bool, error)
	GetWorkflowNodeRunByTaskID(ctx context.Context, taskID string) (*NodeRun, error)
	GetWorkflowNodeRunByTaskRunID(ctx context.Context, taskRunID string) (*NodeRun, error)
	// ListWorkflowRevisions returns a workflow's revisions, newest first, with
	// the total count.
	ListWorkflowRevisions(ctx context.Context, workflowID string, limit, offset int) ([]Revision, int, error)
	// GetWorkflowRevision returns one revision, or nil when the workflow has no
	// such revision number.
	GetWorkflowRevision(ctx context.Context, workflowID string, revision int) (*Revision, error)
}
