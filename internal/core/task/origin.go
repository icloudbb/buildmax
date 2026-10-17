package task

// OriginKind names what a Task serves.
type OriginKind string

const (
	// OriginWorkflow is a Task a Workflow step dispatched.
	OriginWorkflow OriginKind = "workflow"
	// OriginIssue is a Task run for an Issue.
	OriginIssue OriginKind = "issue"
	// OriginConversation is a Task a Conversation turn started.
	OriginConversation OriginKind = "conversation"
	// OriginSchedule is a Task a Schedule fired.
	OriginSchedule OriginKind = "schedule"
)

// Origin is what a Task serves, named as a person knows it. It is read with
// the Task, so a list labels every row without a lookup per row. See
// docs/design/portal-work-and-execution-experience.md#execution-provenance.
type Origin struct {
	Kind OriginKind `json:"kind"`
	// ID is the origin's public ID. For a Workflow step it is the Workflow
	// run, the page a person navigates through to reach the step.
	ID string `json:"id"`
	// Name is the Issue title, the Workflow name, the Conversation title, or
	// the Schedule name as it reads now. Empty when the origin has none.
	Name string `json:"name,omitempty"`
	// Step is the 1-based position of the Workflow step the Task runs, in its
	// run's definition order. Set only for OriginWorkflow.
	Step int `json:"step,omitempty"`
}

// OriginRelations are the origin relations a Task was created with, each with
// the name it has now. An empty ID means the Task has no such relation.
type OriginRelations struct {
	WorkflowRunID     string
	WorkflowName      string
	WorkflowStep      int
	IssueID           string
	IssueTitle        string
	ConversationID    string
	ConversationTitle string
	ScheduleID        string
	ScheduleName      string
}

// Origin picks the one origin a Task presents. A Workflow step outranks the
// Issue its Task may carry for Issue access: the step's Task belongs to the
// Workflow run, and that run is what the Issue started. Nil for a Task with no
// recorded origin, such as one started directly on an Agent.
func (r OriginRelations) Origin() *Origin {
	switch {
	case r.WorkflowRunID != "":
		return &Origin{Kind: OriginWorkflow, ID: r.WorkflowRunID, Name: r.WorkflowName, Step: r.WorkflowStep}
	case r.IssueID != "":
		return &Origin{Kind: OriginIssue, ID: r.IssueID, Name: r.IssueTitle}
	case r.ConversationID != "":
		return &Origin{Kind: OriginConversation, ID: r.ConversationID, Name: r.ConversationTitle}
	case r.ScheduleID != "":
		return &Origin{Kind: OriginSchedule, ID: r.ScheduleID, Name: r.ScheduleName}
	}
	return nil
}
