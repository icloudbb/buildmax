package agent

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/llm"
)

// recordingHookRunner captures every hook invocation; an optional block decision
// is applied to the configured event.
type recordingHookRunner struct {
	mu        sync.Mutex
	calls     []HookInput
	blockOn   HookEvent
	blockOnce bool
	reason    string
	blocked   bool
}

func (r *recordingHookRunner) Run(_ context.Context, in HookInput) HookOutput {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, in)
	if r.blockOn != "" && in.Event == r.blockOn && (!r.blockOnce || !r.blocked) {
		r.blocked = true
		return HookOutput{Decision: HookDecisionBlock, Reason: r.reason}
	}
	return HookOutput{}
}

func (r *recordingHookRunner) snapshot() []HookInput {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]HookInput, len(r.calls))
	copy(out, r.calls)
	return out
}

func (r *recordingHookRunner) countOf(ev HookEvent) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, c := range r.calls {
		if c.Event == ev {
			n++
		}
	}
	return n
}

// TestHook_PreToolUseBlock asserts that a PreToolUse hook returning Block prevents the tool from
// executing, surfaces a deny event, and feeds an error string back to the model.
func TestHook_PreToolUseBlock(t *testing.T) {
	ctx := context.Background()
	mock := &mockLLMClient{
		responses: []mockResponse{
			{toolCalls: []llm.ToolCall{{ID: "c1", Name: "writefile", Arguments: `{"path":"x"}`}}},
			{content: "stopped by policy"},
		},
	}
	tool := &mockTool{name: "writefile", result: "ok"}
	sess := newTestBuffer()
	if err := sess.Append(llm.Message{Role: "user", Content: "please write"}); err != nil {
		t.Fatal(err)
	}
	hooks := &recordingHookRunner{blockOn: HookPreToolUse, reason: "forbidden path"}
	var deniedReasons []string
	_, _, _, err := RunLoop(ctx, RunLoopOpts{
		LLMClient:    mock,
		SystemPrompt: testSystemPrompt,
		ToolRegistry: newTestToolRegistry(tool),
		MaxIter:      5,
		History:      sess,
		Hooks:        hooks,
		EventSink: func(e Event) {
			if e.Kind == EventToolDenied {
				deniedReasons = append(deniedReasons, e.DenyReason)
			}
		},
	})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if tool.executionCount() != 0 {
		t.Errorf("tool executed %d times, want 0 (blocked)", tool.executionCount())
	}
	if got := hooks.countOf(HookPreToolUse); got != 1 {
		t.Errorf("PreToolUse fired %d times, want 1", got)
	}
	if got := hooks.countOf(HookPostToolUse); got != 0 {
		t.Errorf("PostToolUse fired %d times, want 0 when blocked", got)
	}
	if len(deniedReasons) != 1 || deniedReasons[0] != DenyReasonHook {
		t.Errorf("deny reasons = %v, want [%q]", deniedReasons, DenyReasonHook)
	}
	// The tool-role message appended to history should carry the hook reason for the LLM.
	var toolMsg *llm.Message
	for i := range sess.messages {
		if sess.messages[i].Role == "tool" {
			toolMsg = &sess.messages[i]
			break
		}
	}
	if toolMsg == nil {
		t.Fatal("expected a tool-role message in history")
	}
	if !strings.Contains(toolMsg.Content, "forbidden path") {
		t.Errorf("tool message %q does not contain hook reason", toolMsg.Content)
	}
}

// TestHook_PreToolUseAllowFiresPostHook asserts that when PreToolUse allows, the tool runs and
// PostToolUse fires with the result.
func TestHook_PreToolUseAllowFiresPostHook(t *testing.T) {
	ctx := context.Background()
	mock := &mockLLMClient{
		responses: []mockResponse{
			{toolCalls: []llm.ToolCall{{ID: "c1", Name: "echo", Arguments: `{"a":1}`}}},
			{content: "done"},
		},
	}
	tool := &mockTool{name: "echo", result: "echoed"}
	sess := newTestBuffer()
	if err := sess.Append(llm.Message{Role: "user", Content: "echo me"}); err != nil {
		t.Fatal(err)
	}
	hooks := &recordingHookRunner{}
	_, _, _, err := RunLoop(ctx, RunLoopOpts{
		LLMClient:    mock,
		SystemPrompt: testSystemPrompt,
		ToolRegistry: newTestToolRegistry(tool),
		MaxIter:      5,
		History:      sess,
		Hooks:        hooks,
		SessionID:    "sess-1",
		Workspace:    "/tmp/ws",
	})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if tool.executionCount() != 1 {
		t.Errorf("tool executed %d times, want 1", tool.executionCount())
	}
	if got := hooks.countOf(HookPreToolUse); got != 1 {
		t.Errorf("PreToolUse fired %d times, want 1", got)
	}
	if got := hooks.countOf(HookPostToolUse); got != 1 {
		t.Errorf("PostToolUse fired %d times, want 1", got)
	}
	// Verify the PostToolUse payload carries the tool result and identifiers.
	calls := hooks.snapshot()
	var post HookInput
	for _, c := range calls {
		if c.Event == HookPostToolUse {
			post = c
			break
		}
	}
	if post.ToolName != "echo" {
		t.Errorf("post.ToolName = %q, want %q", post.ToolName, "echo")
	}
	if post.ToolResult != "echoed" {
		t.Errorf("post.ToolResult = %q, want %q", post.ToolResult, "echoed")
	}
	if post.SessionID != "sess-1" || post.Workspace != "/tmp/ws" {
		t.Errorf("post identifiers = %+v, want session/workspace propagation", post)
	}
}

// TestHook_StopFiresOnSuccess asserts that the main-agent happy path emits
// Stop (not SubagentStop / StopFailure).
func TestHook_StopFiresOnSuccess(t *testing.T) {
	ctx := context.Background()
	mock := &mockLLMClient{responses: []mockResponse{{content: "hi"}}}
	hooks := &recordingHookRunner{}
	sess := newTestBuffer()
	if err := sess.Append(llm.Message{Role: "user", Content: "hello"}); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := RunLoop(ctx, RunLoopOpts{
		LLMClient:    mock,
		SystemPrompt: testSystemPrompt,
		ToolRegistry: newTestToolRegistry(),
		MaxIter:      5,
		History:      sess,
		Hooks:        hooks,
	})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if got := hooks.countOf(HookStop); got != 1 {
		t.Errorf("Stop fired %d times, want 1", got)
	}
	if got := hooks.countOf(HookSubagentStop); got != 0 {
		t.Errorf("SubagentStop fired %d times, want 0 on main agent run", got)
	}
	if got := hooks.countOf(HookStopFailure); got != 0 {
		t.Errorf("StopFailure fired %d times, want 0 on success", got)
	}
}

// TestHook_StopFailureFiresOnMaxIter asserts that the error path emits
// StopFailure with the failure message.
func TestHook_StopFailureFiresOnMaxIter(t *testing.T) {
	ctx := context.Background()
	var responses []mockResponse
	for range 5 {
		responses = append(responses, mockResponse{toolCalls: []llm.ToolCall{{ID: "c1", Name: "ping", Arguments: "{}"}}})
	}
	mock := &mockLLMClient{responses: responses}
	tool := &mockTool{name: "ping", result: "pong"}
	hooks := &recordingHookRunner{}
	sess := newTestBuffer()
	if err := sess.Append(llm.Message{Role: "user", Content: "ping"}); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := RunLoop(ctx, RunLoopOpts{
		LLMClient:    mock,
		SystemPrompt: testSystemPrompt,
		ToolRegistry: newTestToolRegistry(tool),
		MaxIter:      2,
		History:      sess,
		Hooks:        hooks,
	})
	if err == nil {
		t.Fatal("RunLoop: expected max-iter error")
	}
	if got := hooks.countOf(HookStopFailure); got != 1 {
		t.Errorf("StopFailure fired %d times, want 1", got)
	}
	if got := hooks.countOf(HookStop); got != 0 {
		t.Errorf("Stop fired %d times, want 0 on error path", got)
	}
	calls := hooks.snapshot()
	last := calls[len(calls)-1]
	if last.Event != HookStopFailure || last.Error == "" {
		t.Errorf("last hook = %+v; want HookStopFailure with Error set", last)
	}
}

// TestHook_SubagentStopFiresWhenIsSubagent asserts that a subagent run emits
// SubagentStop (not Stop) on success, with IsSubagent and AgentType stamped.
func TestHook_SubagentStopFiresWhenIsSubagent(t *testing.T) {
	ctx := context.Background()
	mock := &mockLLMClient{responses: []mockResponse{{content: "done"}}}
	hooks := &recordingHookRunner{}
	sess := newTestBuffer()
	if err := sess.Append(llm.Message{Role: "user", Content: "do it"}); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := RunLoop(ctx, RunLoopOpts{
		LLMClient:    mock,
		SystemPrompt: testSystemPrompt,
		ToolRegistry: newTestToolRegistry(),
		MaxIter:      5,
		History:      sess,
		Hooks:        hooks,
		IsSubagent:   true,
		AgentType:    "explorer",
	})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if got := hooks.countOf(HookSubagentStop); got != 1 {
		t.Errorf("SubagentStop fired %d times, want 1", got)
	}
	if got := hooks.countOf(HookStop); got != 0 {
		t.Errorf("Stop fired %d times, want 0 inside subagent", got)
	}
	calls := hooks.snapshot()
	last := calls[len(calls)-1]
	if !last.IsSubagent || last.AgentType != "explorer" {
		t.Errorf("last subagent hook = %+v; want IsSubagent + AgentType=\"explorer\"", last)
	}
}

// TestHook_PostToolUseFailureFiresOnToolError asserts that PostToolUseFailure
// fires (not PostToolUse) when a tool returns an error.
func TestHook_PostToolUseFailureFiresOnToolError(t *testing.T) {
	ctx := context.Background()
	mock := &mockLLMClient{
		responses: []mockResponse{
			{toolCalls: []llm.ToolCall{{ID: "c1", Name: "explode", Arguments: "{}"}}},
			{content: "ok recovered"},
		},
	}
	tool := &failingTool{mockTool: mockTool{name: "explode"}}
	hooks := &recordingHookRunner{}
	sess := newTestBuffer()
	if err := sess.Append(llm.Message{Role: "user", Content: "try"}); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := RunLoop(ctx, RunLoopOpts{
		LLMClient:    mock,
		SystemPrompt: testSystemPrompt,
		ToolRegistry: newTestToolRegistry(tool),
		MaxIter:      5,
		History:      sess,
		Hooks:        hooks,
	})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if got := hooks.countOf(HookPostToolUseFailure); got != 1 {
		t.Errorf("PostToolUseFailure fired %d times, want 1", got)
	}
	if got := hooks.countOf(HookPostToolUse); got != 0 {
		t.Errorf("PostToolUse fired %d times, want 0 on failure path", got)
	}
	// Verify ToolError carries the error message.
	for _, c := range hooks.snapshot() {
		if c.Event == HookPostToolUseFailure {
			if c.ToolError == "" {
				t.Errorf("ToolError empty on PostToolUseFailure: %+v", c)
			}
			return
		}
	}
}

// TestHook_NotificationApprovalLifecycle asserts that an Ask resolution fires
// Notification(approval_required) before approval and
// Notification(permission_denied) on user deny.
func TestHook_NotificationApprovalLifecycle(t *testing.T) {
	ctx := context.Background()
	mock := &mockLLMClient{
		responses: []mockResponse{
			{toolCalls: []llm.ToolCall{{ID: "c1", Name: "ask-tool", Arguments: "{}"}}},
			{content: "stopped"},
		},
	}
	tool := &mockTool{name: "ask-tool", result: "ok"}
	hooks := &recordingHookRunner{}
	sess := newTestBuffer()
	if err := sess.Append(llm.Message{Role: "user", Content: "go"}); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := RunLoop(ctx, RunLoopOpts{
		LLMClient:    mock,
		SystemPrompt: testSystemPrompt,
		ToolRegistry: newTestToolRegistry(tool),
		MaxIter:      5,
		History:      sess,
		Hooks:        hooks,
		Policy:       askAlwaysPolicy{},
		Approval:     denyAlwaysApproval{},
	})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	kinds := make([]string, 0)
	for _, c := range hooks.snapshot() {
		if c.Event == HookNotification {
			kinds = append(kinds, c.NotificationKind)
		}
	}
	if len(kinds) != 2 || kinds[0] != NotificationApprovalRequired || kinds[1] != NotificationPermissionDenied {
		t.Errorf("notification kinds = %v, want [approval_required, permission_denied]", kinds)
	}
	if tool.executionCount() != 0 {
		t.Errorf("tool ran %d times, want 0 after deny", tool.executionCount())
	}
}

// failingTool is a tool whose Execute always returns an error.
type failingTool struct {
	mockTool
}

func (t *failingTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	t.mockTool.mu.Lock()
	t.mockTool.executed++
	t.mockTool.mu.Unlock()
	return "", errFailingTool
}

var errFailingTool = stringError("boom")

type stringError string

func (e stringError) Error() string { return string(e) }

// askAlwaysPolicy resolves every tool to Ask so the approval branch runs.
type askAlwaysPolicy struct{}

func (askAlwaysPolicy) Check(_, _ string, _ map[string]any) (llm.ToolAction, bool) {
	return llm.ToolActionAsk, true
}

// denyAlwaysApproval denies every approval request.
type denyAlwaysApproval struct{}

func (denyAlwaysApproval) RequestApproval(_ context.Context, _ string, _ map[string]any, _ string) ApprovalDecision {
	return ApprovalDeny
}

// TestHook_NilRunnerIsAllowed verifies nil Hooks behaves the same as no hooks.
func TestHook_NilRunnerIsAllowed(t *testing.T) {
	ctx := context.Background()
	mock := &mockLLMClient{responses: []mockResponse{{content: "ok"}}}
	sess := newTestBuffer()
	if err := sess.Append(llm.Message{Role: "user", Content: "hi"}); err != nil {
		t.Fatal(err)
	}
	reply, _, _, err := RunLoop(ctx, RunLoopOpts{
		LLMClient:    mock,
		SystemPrompt: testSystemPrompt,
		ToolRegistry: newTestToolRegistry(),
		MaxIter:      3,
		History:      sess,
		Hooks:        nil,
	})
	if err != nil || reply != "ok" {
		t.Fatalf("RunLoop with nil hooks: reply=%q err=%v", reply, err)
	}
}
