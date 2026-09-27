package agent

import "context"

// HookEvent names the lifecycle point at which a hook fires.
//
// String form is what hook implementations and persisted settings use to address an event.
// The values intentionally match the spelling used by Claude Code's hook design so external
// scripts can be reused with minimal change.
type HookEvent string

const (
	// HookSessionStart fires when a session is opened or resumed. Advisory.
	HookSessionStart HookEvent = "SessionStart"
	// HookSessionEnd fires when a session is finalized/closed. Advisory.
	HookSessionEnd HookEvent = "SessionEnd"

	// HookUserPromptSubmit fires when a user prompt enters the agent, before
	// it is appended to history or sent to the LLM. Hooks may block to
	// abort the turn with a reason surfaced back to the user.
	HookUserPromptSubmit HookEvent = "UserPromptSubmit"

	// HookPreToolUse fires just before a tool would execute. Hooks may
	// block to deny the call.
	HookPreToolUse HookEvent = "PreToolUse"
	// HookPostToolUse fires after a tool finishes successfully. Advisory.
	HookPostToolUse HookEvent = "PostToolUse"
	// HookPostToolUseFailure fires after a tool finishes with an error.
	// Advisory. Separated from HookPostToolUse so audit/notification hooks
	// can subscribe to one channel without filtering payloads.
	HookPostToolUseFailure HookEvent = "PostToolUseFailure"

	// HookNotification fires when the agent needs the user's attention
	// (e.g. an approval prompt, or after a permission denial). Advisory.
	HookNotification HookEvent = "Notification"

	// HookPreCompact fires before context compaction summarizes old
	// messages. Hooks may block to skip compaction.
	HookPreCompact HookEvent = "PreCompact"
	// HookPostCompact fires after context compaction successfully replaces
	// old messages. Advisory.
	HookPostCompact HookEvent = "PostCompact"

	// HookSubagentStart fires when a subagent starts executing. Advisory.
	HookSubagentStart HookEvent = "SubagentStart"
	// HookSubagentStop fires when a subagent finishes successfully.
	// Advisory.
	HookSubagentStop HookEvent = "SubagentStop"
	// HookStop fires when the main agent loop finishes successfully.
	// Advisory. For subagents, HookSubagentStop is used instead.
	HookStop HookEvent = "Stop"
	// HookStopFailure fires when the agent loop exits with an error
	// (whether main agent or subagent). Advisory.
	HookStopFailure HookEvent = "StopFailure"

	// HookWorktreeCreate fires after a worktree has been created and entered.
	// Advisory: the tool call that asked for it already passed PreToolUse, and
	// a second gate over the same decision would only be a way to half-create
	// one.
	HookWorktreeCreate HookEvent = "WorktreeCreate"
	// HookWorktreeRemove fires after a worktree and its branch are gone.
	// Advisory, and for the same reason.
	HookWorktreeRemove HookEvent = "WorktreeRemove"
	// HookCwdChanged fires whenever the session's workspace root moves,
	// including the move a create performs. Advisory. It is the event to
	// subscribe to for "where is this session working now"; the two worktree
	// events say what happened to the tree itself.
	HookCwdChanged HookEvent = "CwdChanged"
)

// NotificationKind enumerates the reasons a HookNotification event fires.
const (
	NotificationApprovalRequired = "approval_required"
	NotificationPermissionDenied = "permission_denied"
	// NotificationUserQuestion fires when the Agent puts a question to the
	// user through AskUser and is waiting for the answer.
	NotificationUserQuestion = "user_question"
)

// HookInput carries the payload sent to hooks for one event.
//
// All fields use snake_case JSON tags so the on-stdin JSON matches the project's
// persistence convention (CLAUDE.md §6.1). A hook implementation only reads the
// fields relevant to its event; the rest are zero values.
type HookInput struct {
	Event     HookEvent `json:"event"`
	SessionID string    `json:"session_id,omitempty"`
	Workspace string    `json:"workspace,omitempty"`

	// IsSubagent is true when the event fires inside a subagent run.
	// AgentType identifies which subagent (its def name). Both fields are
	// stamped on every event from a subagent so audit hooks can attribute.
	IsSubagent bool   `json:"is_subagent,omitempty"`
	AgentType  string `json:"agent_type,omitempty"`

	// Populated for HookUserPromptSubmit.
	Prompt string `json:"prompt,omitempty"`

	// Populated for HookPreToolUse, HookPostToolUse, HookPostToolUseFailure,
	// and HookNotification (when tied to an approval flow).
	ToolName   string         `json:"tool_name,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	ToolArgs   map[string]any `json:"tool_args,omitempty"`

	// Populated for HookPostToolUse (result string) and
	// HookPostToolUseFailure (error string).
	ToolResult string `json:"tool_result,omitempty"`
	ToolError  string `json:"tool_error,omitempty"`

	// Populated for HookNotification: see NotificationApprovalRequired /
	// NotificationPermissionDenied. NotificationReason carries any extra
	// human-readable context.
	NotificationKind   string `json:"notification_kind,omitempty"`
	NotificationReason string `json:"notification_reason,omitempty"`

	// Populated for HookPreCompact (about-to-summarize counts) and
	// HookPostCompact (final counts plus summary).
	Summarized int    `json:"summarized,omitempty"`
	Kept       int    `json:"kept,omitempty"`
	Summary    string `json:"summary,omitempty"`

	// Populated for HookStop, HookSubagentStop, HookStopFailure, and
	// HookSessionEnd.
	Stats *RunStats `json:"stats,omitempty"`
	// Populated for HookStopFailure (the failure message).
	Error string `json:"error,omitempty"`

	// Populated for HookWorktreeCreate, HookWorktreeRemove, and
	// HookCwdChanged. Workspace carries where the session is now; these say
	// which tree it is and, for a move, where it came from.
	WorktreePath      string `json:"worktree_path,omitempty"`
	WorktreeBranch    string `json:"worktree_branch,omitempty"`
	PreviousWorkspace string `json:"previous_workspace,omitempty"`

	// Sandbox is the runtime sandbox snapshot for the current run.
	// Populated on HookSessionStart (always) and on every gating event
	// thereafter so hooks can enforce policy like "fail if the sandbox
	// is off on the worker" without having to read settings themselves.
	Sandbox *SandboxInfo `json:"sandbox,omitempty"`
}

// HookDecision is the gate signal a hook may return for PreToolUse / PreCompact.
type HookDecision string

const (
	// HookDecisionAllow is the zero value and means "do not interfere with the action".
	HookDecisionAllow HookDecision = ""
	// HookDecisionBlock halts the upcoming action; Reason is surfaced to the agent and user.
	HookDecisionBlock HookDecision = "block"
)

// HookOutput is the aggregated result of running all hooks for one event.
//
// For advisory events (PostToolUse, PostCompact, RunEnd), Decision is ignored.
// For gating events (PreToolUse, PreCompact), Decision == HookDecisionBlock halts the action.
type HookOutput struct {
	Decision HookDecision `json:"decision,omitempty"`
	Reason   string       `json:"reason,omitempty"`
}

// Blocked reports whether the output asks the caller to stop the upcoming action.
func (o HookOutput) Blocked() bool { return o.Decision == HookDecisionBlock }

// HookRunner runs configured hooks for one event and returns the aggregated decision.
//
// Implementations must be safe to call from the RunLoop goroutine and should not block
// indefinitely; the shell runner is expected to enforce per-hook timeouts internally.
// Failures inside an implementation should fail open (return HookOutput{}) and log,
// so a broken hook never silently breaks the agent loop.
type HookRunner interface {
	Run(ctx context.Context, in HookInput) HookOutput
}

// runHook is a small helper that respects a nil runner (treating it as Noop) so call
// sites in agent.go stay compact.
func runHook(ctx context.Context, runner HookRunner, in HookInput) HookOutput {
	if runner == nil {
		return HookOutput{}
	}
	return runner.Run(ctx, in)
}

// baseHookInput builds a HookInput pre-populated with the per-run attribution
// fields (SessionID, Workspace, IsSubagent, AgentType) taken from opts.
// Callers fill in event-specific fields.
func baseHookInput(opts RunLoopOpts, event HookEvent) HookInput {
	return HookInput{
		Event:      event,
		SessionID:  opts.SessionID,
		Workspace:  opts.Workspace,
		IsSubagent: opts.IsSubagent,
		AgentType:  opts.AgentType,
	}
}
