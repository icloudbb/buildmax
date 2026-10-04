package workflow

import (
	"context"
	"encoding/json"
	"strconv"
	"time"
)

const (
	StatusDraft     = "draft"
	StatusPublished = "published"
	StatusArchived  = "archived"

	NodeTypeAgentTask = "agent_task"
	// NodeTypeHumanInput is a node a person completes: it opens a durable
	// request and its output is the validated answer. It runs no Agent and
	// holds no worker while it waits.
	NodeTypeHumanInput = "human_input"

	// DefinitionSchemaVersion is the only workflow definition contract version the
	// runtime accepts. A definition must declare it explicitly; publication rejects
	// any other value so a stored plan always names the contract it was written for.
	DefinitionSchemaVersion = 1

	// MaxParallelNodesCeiling is the deployment maximum for how many nodes of one
	// run may execute at once. A definition's policy may set a lower bound but not
	// exceed this; it is the Space/deployment limit §6.2 requires ready nodes to
	// stay within, and it caps a run's concurrent worker Tasks regardless of graph
	// width. A definition that names no limit runs up to this ceiling.
	MaxParallelNodesCeiling = 8

	// MaxNodeAttemptsCeiling bounds a node's max_attempts, first attempt
	// included. Every attempt is a full Agent run that may have side effects, so
	// retry is bounded tightly rather than left to the author.
	MaxNodeAttemptsCeiling = 5

	// MinTimeoutSeconds and MaxTimeoutSeconds bound every timeout_seconds a
	// definition declares. The floor stays above the reconciler's observation
	// interval so a timeout is enforced near when it is due; the ceiling keeps a
	// forgotten run from holding a deadline nobody will ever reach.
	MinTimeoutSeconds = 60
	MaxTimeoutSeconds = 30 * 24 * 60 * 60
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
	RunStatusFailing   RunStatus = "failing"
	RunStatusCanceling RunStatus = "canceling"
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
	// NodeRunStatusRetryWait is an attempt that failed while the node's
	// max_attempts still allows another; the next attempt is admitted at
	// NextAttemptAt.
	NodeRunStatusRetryWait NodeRunStatus = "retry_wait"
	// NodeRunStatusWaiting is a node with an outstanding durable request: a
	// human_input node awaiting its answer, or an agent_task node whose attempt
	// ended on questions. It consumes no worker.
	NodeRunStatusWaiting NodeRunStatus = "waiting"
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
		return to == RunStatusSucceeded || to == RunStatusFailed || to == RunStatusCanceled || to == RunStatusFailing || to == RunStatusCanceling
	case RunStatusFailing:
		return to == RunStatusFailed
	case RunStatusCanceling:
		return to == RunStatusCanceled
	default:
		return false
	}
}

// FailureClass records why a Workflow run failed, as a closed enum. The
// service sets it where it decides the failure — when the run starts failing —
// and nothing derives it from error text, which may carry Space content. It is
// the run's own cause: a node whose Agent run failed is FailureNode here, and
// that TaskRun's own failure class says why the Agent run failed.
type FailureClass string

const (
	// FailureNode is a node attempt that failed with no attempt left.
	FailureNode FailureClass = "node_failed"
	// FailureOutputSchema is a node whose run finished without a value
	// satisfying its output_schema.
	FailureOutputSchema FailureClass = "output_schema"
	// FailureNodeTimeout is a node whose last attempt exceeded its timeout.
	FailureNodeTimeout FailureClass = "node_timeout"
	// FailureAdmission is a node whose Task, retry, or resumed attempt could not
	// be admitted, or whose bound inputs could not be resolved.
	FailureAdmission FailureClass = "admission"
	// FailureRequestDeclined is a request a Space member declined.
	FailureRequestDeclined FailureClass = "request_declined"
	// FailureRequestExpired is a request nobody answered before it expired.
	FailureRequestExpired FailureClass = "request_expired"
	// FailureRunDeadline is a run that passed its policy.timeout_seconds.
	FailureRunDeadline FailureClass = "run_deadline"
	// FailureUnclassified is the fallback when no rule applied.
	FailureUnclassified FailureClass = "unclassified"
)

// FailureClasses lists every class, in a stable order for reports.
func FailureClasses() []FailureClass {
	return []FailureClass{
		FailureNode, FailureOutputSchema, FailureNodeTimeout, FailureAdmission,
		FailureRequestDeclined, FailureRequestExpired, FailureRunDeadline, FailureUnclassified,
	}
}

// NormalizeFailureClass returns the class named by s, or FailureUnclassified
// for an empty or unknown value, so a stored class is always one of the enum.
func NormalizeFailureClass(s string) FailureClass {
	for _, c := range FailureClasses() {
		if string(c) == s {
			return c
		}
	}
	return FailureUnclassified
}

// Request kinds and statuses. A request is the durable record of a Workflow
// waiting on a person: RequestKindInput for a human_input node, and
// RequestKindQuestion for an agent_task attempt that ended on AskUser
// questions. Only a pending request can be resolved, once.
const (
	RequestKindInput    = "input"
	RequestKindQuestion = "question"

	RequestStatusPending  = "pending"
	RequestStatusAnswered = "answered"
	RequestStatusDeclined = "declined"
	RequestStatusExpired  = "expired"
	RequestStatusCanceled = "canceled"
)

// Request is one durable request a Workflow run waits on. It belongs to one
// node run; Key makes opening it idempotent within the run.
type Request struct {
	ID            string `json:"id"`
	WorkflowRunID string `json:"workflow_run_id"`
	NodeRunID     string `json:"node_run_id"`
	NodeID        string `json:"node_id"`
	Kind          string `json:"kind"`
	// Prompt is what the responder is asked: a human_input node's instruction
	// with its bound values. Empty for a question, which carries Questions.
	Prompt string `json:"prompt,omitempty"`
	// Questions is the AskUser question set as JSON, for a question request.
	Questions *string `json:"questions,omitempty"`
	// ResponseSchema is the JSON Schema the answer must satisfy; nil means the
	// answer is free text (a JSON string).
	ResponseSchema *string `json:"response_schema,omitempty"`
	// TaskRunID is the attempt that asked, for a question request.
	TaskRunID *string    `json:"task_run_id,omitempty"`
	Status    string     `json:"status"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	// Response is the answer as JSON, or a decline's reason as a JSON string.
	Response    *string    `json:"response,omitempty"`
	RespondedBy *string    `json:"responded_by,omitempty"`
	RespondedAt *time.Time `json:"responded_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
}

// OpenRequestInput opens a request and moves its node to waiting in one
// transaction under the run lock, so a request is never opened on a run that
// is already stopping. NodeExpected is pending for a human_input node and
// running for a question. A repeated open with the same Key returns the
// existing request.
type OpenRequestInput struct {
	WorkflowRunID  string
	NodeRunID      string
	NodeExpected   NodeRunStatus
	Key            string
	Kind           string
	Prompt         string
	Questions      *string
	ResponseSchema *string
	TaskRunID      *string
	ExpiresAt      *time.Time
	// ResolvedInput lands on the node with the move to waiting (a human_input
	// node's rendered prompt), like an agent node's Task input.
	ResolvedInput *string
	Now           time.Time
}

// ResolveRequestInput moves a pending request to Status (answered, declined,
// or expired). An answer or decline is refused once ExpiresAt has passed at
// Now, so an expired request cannot be answered in a race with expiry.
type ResolveRequestInput struct {
	RequestID   string
	Status      string
	Response    *string
	RespondedBy *string
	Now         time.Time
}

// ValidNodeRunTransition reports whether a step run may move directly from one
// status to another. A pending step may start (running), be blocked by an
// earlier failure, or fail outright when its task cannot be created; a running
// step ends succeeded, failed, or canceled, or waits to retry. A retrying step
// starts its next attempt, fails when that attempt cannot be admitted, or is
// canceled when the run stops. Terminal statuses are immutable.
func ValidNodeRunTransition(from, to NodeRunStatus) bool {
	switch from {
	case NodeRunStatusPending:
		return to == NodeRunStatusRunning || to == NodeRunStatusBlocked ||
			to == NodeRunStatusFailed || to == NodeRunStatusCanceled || to == NodeRunStatusWaiting
	case NodeRunStatusRunning:
		return to == NodeRunStatusSucceeded || to == NodeRunStatusFailed || to == NodeRunStatusCanceled ||
			to == NodeRunStatusRetryWait || to == NodeRunStatusWaiting
	case NodeRunStatusRetryWait:
		return to == NodeRunStatusRunning || to == NodeRunStatusFailed || to == NodeRunStatusCanceled
	case NodeRunStatusWaiting:
		// An answered human_input node succeeds; an answered question resumes
		// its Agent; a declined or expired request fails the node.
		return to == NodeRunStatusSucceeded || to == NodeRunStatusRunning ||
			to == NodeRunStatusFailed || to == NodeRunStatusCanceled
	default:
		return false
	}
}

// RetryBackoff is how long a node waits after its failed attempt'th attempt
// before the next one: 30s doubling per attempt, capped at 10 minutes. The
// common failures an automatic retry can outlive -- a provider outage, a lost
// worker -- take minutes, not milliseconds, to clear.
func RetryBackoff(attempt int) time.Duration {
	const base, ceiling = 30 * time.Second, 10 * time.Minute
	d := base
	for i := 1; i < attempt && d < ceiling; i++ {
		d *= 2
	}
	return min(d, ceiling)
}

// Deadline is start plus timeoutSeconds, or nil when no timeout is set.
func Deadline(start time.Time, timeoutSeconds int) *time.Time {
	if timeoutSeconds <= 0 {
		return nil
	}
	d := start.Add(time.Duration(timeoutSeconds) * time.Second)
	return &d
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
	// ScheduleID is the recurring schedule that started this run, or nil when a
	// person, agent, issue, or recovery started it. It records the trigger so a
	// schedule can list the runs it fired, mirroring a Task's schedule linkage.
	ScheduleID *string `json:"schedule_id,omitempty"`
	// ConversationID is the conversation whose turn started this run, or nil.
	// A Space Assistant's turn reads only the runs its own conversation started.
	ConversationID *string `json:"conversation_id,omitempty"`
	// Input is the run's immutable input JSON, validated against the definition's
	// input_schema at admission. Nil when the definition declares no input_schema.
	Input     *string `json:"input,omitempty"`
	Status    string  `json:"status"`
	CreatedBy string  `json:"created_by"`
	// Result is the run's declared result JSON: the value the definition's result
	// selector resolved to when the run succeeded, stored independently of the
	// node runs so a reader has one authoritative answer. Nil when the definition
	// declares no result or the run did not succeed. Immutable once set.
	Result       *string    `json:"result,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	EndedAt      *time.Time `json:"ended_at,omitempty"`
	ErrorMessage *string    `json:"error_message,omitempty"`
	// FailureClass is why the run failed, set when it starts failing; empty
	// for a run that has not failed.
	FailureClass string `json:"failure_class,omitempty"`
	// DeadlineAt is when the run fails if it has not finished, from the
	// definition's policy.timeout_seconds at admission. Nil when none is set.
	DeadlineAt *time.Time `json:"deadline_at,omitempty"`
	// Reconciliation scheduling and ownership. ReconcileOwner and LeaseExpiresAt
	// are a bounded lease that reduces duplicate reconciliation work; they are not
	// the correctness mechanism. NextReconcileAt is when this run next wants a
	// reconciliation pass. All three are cleared when the run becomes terminal.
	ReconcileOwner  *string    `json:"reconcile_owner,omitempty"`
	LeaseExpiresAt  *time.Time `json:"lease_expires_at,omitempty"`
	NextReconcileAt *time.Time `json:"next_reconcile_at,omitempty"`
}

// NodeRun is one durable node execution record under a workflow run. NodeID is
// the authoring node's id and NodeIndex is its position in the definition's
// deterministic topological order, so listing node runs by index shows them
// consistent with their dependencies. Needs is the run's snapshot of the node's
// `needs` edges: readiness is decided from these persisted edges, not from
// array position.
type NodeRun struct {
	ID            string `json:"id"`
	WorkflowRunID string `json:"workflow_run_id"`
	NodeID        string `json:"node_id"`
	NodeIndex     int    `json:"node_index"`
	NodeType      string `json:"node_type"`
	// Needs is the run's snapshot of this node's dependency edges (the ids of the
	// nodes that must succeed before it becomes ready), taken at start so a later
	// definition edit cannot change what an in-flight run waits on.
	Needs []string `json:"needs,omitempty"`
	// IssueAccess is the run's snapshot of this node's Issue access mode, so a
	// later definition edit cannot change what an in-flight node's Task can reach.
	IssueAccess   string  `json:"issue_access,omitempty"`
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
	// MaxAttempts and TimeoutSeconds are the run's snapshot of the node's retry
	// and per-attempt timeout policy. MaxAttempts is at least 1; TimeoutSeconds
	// is 0 when the node has no timeout.
	MaxAttempts    int `json:"max_attempts"`
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
	// Attempt counts the attempts admitted so far: 0 before dispatch, 1 for the
	// first. TaskRunID is always the latest attempt's run; earlier attempts are
	// the TaskRuns under TaskID, linked by retry_of_task_run_id.
	Attempt int `json:"attempt"`
	// DeadlineAt is when the current attempt times out; NextAttemptAt is when a
	// retry_wait node admits its next attempt. Nil when not applicable.
	DeadlineAt    *time.Time `json:"deadline_at,omitempty"`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"`
	Status        string     `json:"status"`
	TaskID        *string    `json:"task_id,omitempty"`
	TaskRunID     *string    `json:"task_run_id,omitempty"`
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
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
	// Policy, when set, carries run-wide execution policy. Absent leaves every
	// field at its default.
	Policy *DefinitionPolicy `json:"policy,omitempty"`
	// Nodes is the definition's unordered set of nodes. JSON represents it as an
	// array, but array position is not control flow: a node's dependencies come
	// from its `needs` edges, and the execution order is the topological order of
	// the resulting DAG.
	Nodes []DefinitionNode `json:"nodes"`
	// Result, when set, selects the WorkflowRun result from one node's output
	// envelope. The selected node must exist. Absent leaves the run without a
	// declared result.
	Result *ResultSelector `json:"result,omitempty"`
}

// DefinitionPolicy is the run-wide execution policy of a workflow definition.
type DefinitionPolicy struct {
	// MaxParallelNodes bounds how many of a run's nodes may execute at once. Zero
	// (absent) means no definition-set limit, so the run uses the deployment
	// ceiling. Publication rejects a value above MaxParallelNodesCeiling.
	MaxParallelNodes int `json:"max_parallel_nodes,omitempty"`
	// TimeoutSeconds bounds the whole run from admission. When it passes, no new
	// node starts and the run fails after its active work drains. Zero means no
	// run deadline.
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
}

// NodePolicy is one node's execution policy.
type NodePolicy struct {
	// TimeoutSeconds bounds each attempt from its dispatch, queue time included.
	// A timed-out attempt is canceled and counts as a failed attempt. Zero means
	// no per-attempt timeout.
	TimeoutSeconds int `json:"timeout_seconds,omitempty"`
	// MaxAttempts is how many attempts the node may make, first included. Zero
	// means one: retry is opt-in because a failed Agent run may already have
	// acted on an external system.
	MaxAttempts int `json:"max_attempts,omitempty"`
}

// MaxParallelNodes is the effective concurrency limit for a run of this
// definition: the policy's value when it set one, otherwise the deployment
// ceiling. It is always in [1, MaxParallelNodesCeiling].
func (d *Definition) MaxParallelNodes() int {
	if d.Policy != nil && d.Policy.MaxParallelNodes > 0 {
		return d.Policy.MaxParallelNodes
	}
	return MaxParallelNodesCeiling
}

// ResultSelector selects the WorkflowRun result from one step's output envelope,
// with the same source/pointer grammar as a StepBinding. Source must name a
// step's output ("node.<node_id>.output"); Pointer is an RFC 6901 pointer into
// that envelope, empty for the whole value.
type ResultSelector struct {
	Source  string `json:"source"`
	Pointer string `json:"pointer"`
}

// DefinitionNode describes one node in a workflow definition's DAG.
type DefinitionNode struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	// Needs lists the ids of the nodes that must succeed before this one becomes
	// ready. It forms a directed acyclic graph; array position is not control
	// flow. An empty list is a root that is ready at run start.
	Needs []string `json:"needs,omitempty"`
	// Agent names the Agent an agent_task node runs as. A human_input node has
	// none.
	Agent NodeAgent `json:"agent,omitzero"`
	// Input is the node's task instruction and the values bound into it.
	Input NodeInput `json:"input"`
	// IssueAccess controls whether this node's Task receives the run's Issue.
	// One of "none" (default), "if_bound", or "required". Empty means "none".
	IssueAccess string `json:"issue_access,omitempty"`
	// OutputSchema, when set, is a JSON Schema (in the shared subset) the node's
	// agent run must satisfy as its final answer. Publication rejects a schema
	// outside the subset. The node succeeds only when the run returns a value
	// that validates against it. Empty leaves the node free text. See
	// docs/design/structured-output.md.
	OutputSchema json.RawMessage `json:"output_schema,omitempty"`
	// Policy, when set, carries the node's retry and timeout policy.
	Policy *NodePolicy `json:"policy,omitempty"`
}

// MaxAttempts is the node's effective attempt budget: its policy's value, or 1.
func (n *DefinitionNode) MaxAttempts() int {
	if n.Policy != nil && n.Policy.MaxAttempts > 0 {
		return n.Policy.MaxAttempts
	}
	return 1
}

// TimeoutSeconds is the node's per-attempt timeout, or 0 for none.
func (n *DefinitionNode) TimeoutSeconds() int {
	if n.Policy != nil {
		return n.Policy.TimeoutSeconds
	}
	return 0
}

// NodeAgent names the Agent a node runs as. Revision, when set, pins a specific
// Agent revision; zero leaves the node on the Agent's current definition, which
// the run still snapshots at start. Revision-pinning at publication is a later
// slice, so a zero revision is accepted today.
type NodeAgent struct {
	ID       string `json:"id"`
	Revision int    `json:"revision,omitempty"`
}

// NodeInput is a node's task instruction and the values bound into it.
type NodeInput struct {
	Instruction string `json:"instruction"`
	// Bindings feed selected values into this node's input. Each names a value
	// (Name) taken from a source (the run's input, or a transitive predecessor
	// node's output envelope) at an RFC 6901 pointer. The bound value reaches the
	// Task as labelled untrusted context, never the agent's instructions.
	Bindings []StepBinding `json:"bindings,omitempty"`
}

// IssueAccess is how a node relates to the run's Issue. IssueAccessNone gives the
// node's Task no Issue relation; IssueAccessIfBound gives it the run's Issue when
// the run has one; IssueAccessRequired additionally makes run admission fail when
// the run has no Issue. This makes Issue capability an explicit per-node choice
// instead of silently granting or withholding it everywhere.
const (
	IssueAccessNone     = "none"
	IssueAccessIfBound  = "if_bound"
	IssueAccessRequired = "required"
)

// ValidIssueAccess reports whether s is a supported issue_access value. The empty
// string is not valid here; the parser defaults it to IssueAccessNone first.
func ValidIssueAccess(s string) bool {
	switch s {
	case IssueAccessNone, IssueAccessIfBound, IssueAccessRequired:
		return true
	default:
		return false
	}
}

// BindingSourceWorkflowInput is the binding source that selects into the run's
// immutable input JSON. The only other source is an earlier node's output,
// named by NodeOutputSource / parsed by ParseNodeOutputSource.
const BindingSourceWorkflowInput = "workflow.input"

// StepBinding binds a value selected from a source at an RFC 6901 pointer into a
// downstream step's input under a name.
//
// Source is either "workflow.input" (the run's frozen input) or
// "node.<node_id>.output" (an earlier node's output envelope: text, structured,
// artifacts, task_id, task_run_id). Pointer is an RFC 6901 JSON Pointer into
// that value; the empty string selects the whole value. There is no JSONPath,
// filter, function, or template evaluation in this contract.
type StepBinding struct {
	Name    string `json:"name"`
	Source  string `json:"source"`
	Pointer string `json:"pointer"`
}

// NodeOutputSource is the binding source string that selects an earlier node's
// output envelope by its node id.
func NodeOutputSource(nodeID string) string {
	return "node." + nodeID + ".output"
}

// TaskAdmissionKey identifies the single Task owned by a logical graph node.
func TaskAdmissionKey(workflowRunID, nodeID string) string {
	return "workflow/" + workflowRunID + "/node/" + nodeID
}

// TaskRunAdmissionKey identifies one retry attempt's TaskRun under the node's
// Task, so a repeated admission of the same attempt resolves to one run.
func TaskRunAdmissionKey(workflowRunID, nodeID string, attempt int) string {
	return TaskAdmissionKey(workflowRunID, nodeID) + "/attempt/" + strconv.Itoa(attempt)
}

// ParseNodeOutputSource returns the node id a "node.<node_id>.output" source
// names, or ("", false) when source is not that shape. The node id may itself
// contain dots, so only the fixed "node." prefix and ".output" suffix are
// stripped.
func ParseNodeOutputSource(source string) (string, bool) {
	const prefix, suffix = "node.", ".output"
	if len(source) <= len(prefix)+len(suffix) {
		return "", false
	}
	if source[:len(prefix)] != prefix || source[len(source)-len(suffix):] != suffix {
		return "", false
	}
	return source[len(prefix) : len(source)-len(suffix)], true
}

// NodeOutputEnvelope is the addressable value of a node's output that a
// downstream binding selects into with an RFC 6901 pointer. It is built from an
// accepted node run: the full output text, the validated structured value (or
// null), the Artifact references the accepted TaskRun produced, and the Task and
// TaskRun handles.
type NodeOutputEnvelope struct {
	Text       string               `json:"text"`
	Structured json.RawMessage      `json:"structured"`
	Artifacts  []NodeOutputArtifact `json:"artifacts"`
	TaskID     string               `json:"task_id,omitempty"`
	TaskRunID  string               `json:"task_run_id,omitempty"`
}

// NodeOutputArtifact is one stable Artifact reference in a node output envelope.
// It carries only the reference a downstream Agent needs to open the Artifact
// through its normal authorized capability, never the bytes.
type NodeOutputArtifact struct {
	ID        string `json:"id"`
	Path      string `json:"path,omitempty"`
	MediaType string `json:"media_type,omitempty"`
}

type CreateRunInput struct {
	WorkflowID       string
	WorkflowRevision int
	IssueID          *string
	// ScheduleID is the schedule that started the run, or nil for any other
	// trigger. It is recorded so a schedule can list the runs it fired.
	ScheduleID *string
	// ConversationID is the conversation whose turn started the run, or nil.
	ConversationID *string
	// Input is the run's immutable input JSON, already validated against the
	// definition's input_schema. Nil when the definition declares no input_schema.
	Input      *string
	Status     string
	CreatedBy  string
	StartedAt  *time.Time
	DeadlineAt *time.Time
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
	Needs             []string
	IssueAccess       string
	TargetAgentID     *string
	AgentName         string
	AgentDescription  string
	AgentInstructions string
	AgentRevision     int
	Prompt            string
	Bindings          []StepBinding
	OutputSchema      *string
	MaxAttempts       int
	TimeoutSeconds    int
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
	// Result is the run's declared result JSON, written when the run moves to
	// succeeded. Nil leaves the column untouched, so a definition with no result
	// selector or a non-success transition stores nothing.
	Result *string
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
	// Attempt and DeadlineAt land with a move to running (the attempt's number
	// and its timeout); NextAttemptAt lands with a move to retry_wait. A move
	// out of retry_wait clears NextAttemptAt; a move out of running clears
	// DeadlineAt.
	Attempt       *int
	DeadlineAt    *time.Time
	NextAttemptAt *time.Time
}

// StopRunInput records run-level stop intent that no single node caused: the
// run deadline passing, or a person canceling the run. RunStatus is failing or
// canceling. In one transaction the run moves from RunExpected, pending nodes
// become blocked, and retry_wait and waiting nodes become canceled with their
// pending requests; running nodes keep their state until their TaskRuns end.
type StopRunInput struct {
	WorkflowRunID string
	RunExpected   RunStatus
	RunStatus     RunStatus
	ErrorMessage  *string
	// FailureClass is recorded with a move to failing; the store normalizes it.
	FailureClass FailureClass
}

// BeginRunDrainInput commits the first failed/canceled node and stops further
// admission atomically. RunStatus is failing or canceling; running siblings
// remain observable until their TaskRuns end. EndedAt applies only to the node.
// Both expected states and the node's membership must match or nothing changes.
type BeginRunDrainInput struct {
	WorkflowRunID string
	NodeRunID     string
	NodeExpected  NodeRunStatus
	NodeStatus    NodeRunStatus
	RunExpected   RunStatus
	RunStatus     RunStatus
	TaskRunID     *string
	Output        *string
	Structured    *string
	ErrorMessage  *string
	// FailureClass is recorded with a move to failing; the store normalizes it.
	FailureClass FailureClass
	StartedAt    *time.Time
	EndedAt      *time.Time
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
	// ListWorkflowRunsBySchedule returns the runs a recurring schedule started,
	// newest first, so a schedule can show its firing history.
	ListWorkflowRunsBySchedule(ctx context.Context, scheduleID string, limit, offset int) ([]Run, int, error)
	GetWorkflowRun(ctx context.Context, workflowRunID string) (*Run, error)
	ListWorkflowNodeRuns(ctx context.Context, workflowRunID string) ([]NodeRun, error)
	CreateWorkflowNodeRuns(ctx context.Context, workflowRunID string, steps []CreateNodeRunInput) ([]NodeRun, error)
	// TransitionWorkflowRun and TransitionWorkflowNodeRun apply one guarded
	// status change each; a false result means the row was not at the expected
	// status, so another actor won the transition. BeginWorkflowRunDrain
	// records stop intent and blocks pending nodes in one transaction.
	TransitionWorkflowRun(ctx context.Context, in TransitionRunInput) (bool, error)
	TransitionWorkflowNodeRun(ctx context.Context, in TransitionNodeRunInput) (bool, error)
	BeginWorkflowRunDrain(ctx context.Context, in BeginRunDrainInput) (bool, error)
	StopWorkflowRun(ctx context.Context, in StopRunInput) (bool, error)
	// OpenWorkflowRequest opens a request and moves its node to waiting. A false
	// result means the run or node was no longer at the expected status.
	OpenWorkflowRequest(ctx context.Context, in OpenRequestInput) (*Request, bool, error)
	// ResolveWorkflowRequest moves a pending, unexpired request to its outcome;
	// false means another actor resolved it first or it had expired.
	ResolveWorkflowRequest(ctx context.Context, in ResolveRequestInput) (bool, error)
	GetWorkflowRequest(ctx context.Context, requestID string) (*Request, error)
	ListWorkflowRequestsByRun(ctx context.Context, workflowRunID string) ([]Request, error)
	// ListPendingWorkflowRequestsBySpace returns the space's pending requests,
	// oldest first, with the total count.
	ListPendingWorkflowRequestsBySpace(ctx context.Context, spaceID string, limit, offset int) ([]Request, int, error)
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
