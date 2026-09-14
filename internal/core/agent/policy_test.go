package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/llm"
)

// denyPolicy denies all tool calls unconditionally.
type denyPolicy struct{}

func (denyPolicy) Check(_, _ string, _ map[string]any) (llm.ToolAction, bool) {
	return llm.ToolActionDeny, true
}

// askPolicy returns Ask for all tool calls.
type askPolicy struct{}

func (askPolicy) Check(_, _ string, _ map[string]any) (llm.ToolAction, bool) {
	return llm.ToolActionAsk, true
}

// countingApproval counts how many times RequestApproval is called.
type countingApproval struct {
	calls   int
	approve bool
	session bool // when approving, keep the grant for the rest of the session
}

func (a *countingApproval) RequestApproval(_ context.Context, _ string, _ map[string]any) ApprovalDecision {
	a.calls++
	switch {
	case !a.approve:
		return ApprovalDeny
	case a.session:
		return ApprovalAllowSession
	default:
		return ApprovalAllowOnce
	}
}

// TestPolicyDeny verifies that ToolActionDeny prevents execution and returns an error result to the model.
func TestPolicyDeny(t *testing.T) {
	ctx := context.Background()
	tool := &mockTool{name: "bash", result: "should not run"}
	mock := &mockLLMClient{
		responses: []mockResponse{
			{toolCalls: []llm.ToolCall{{ID: "1", Name: "bash", Arguments: `{"command":"rm -rf /"}`}}},
			{content: "blocked"},
		},
	}
	sess := newTestBuffer()
	reply, stats, err := runLoopWithUserMsg(ctx, mock, newTestToolRegistry(tool), sess, "run something", func(o *RunLoopOpts) {
		o.Policy = denyPolicy{}
	})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if tool.executionCount() != 0 {
		t.Errorf("tool executed %d times; want 0 (denied by policy)", tool.executionCount())
	}
	if stats.ToolCalls != 1 {
		t.Errorf("stats.ToolCalls = %d, want 1", stats.ToolCalls)
	}
	// The tool result message sent to the model must carry the denial error.
	msgs := sess.messages
	var toolMsg *llm.Message
	for i := range msgs {
		if msgs[i].Role == "tool" {
			toolMsg = &msgs[i]
			break
		}
	}
	if toolMsg == nil {
		t.Fatal("no tool-role message found in history")
	}
	if !strings.Contains(toolMsg.Content, "denied by policy") {
		t.Errorf("tool message = %q; want it to contain 'denied by policy'", toolMsg.Content)
	}
	_ = reply
}

// TestPolicyAsk_Approved verifies that ToolActionAsk with an approving handler allows execution.
func TestPolicyAsk_Approved(t *testing.T) {
	ctx := context.Background()
	tool := &mockTool{name: "bash", result: "ran"}
	mock := &mockLLMClient{
		responses: []mockResponse{
			{toolCalls: []llm.ToolCall{{ID: "1", Name: "bash", Arguments: `{"command":"echo hi"}`}}},
			{content: "done"},
		},
	}
	approval := &countingApproval{approve: true}
	sess := newTestBuffer()
	_, _, err := runLoopWithUserMsg(ctx, mock, newTestToolRegistry(tool), sess, "hi", func(o *RunLoopOpts) {
		o.Policy = askPolicy{}
		o.Approval = approval
	})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if tool.executionCount() != 1 {
		t.Errorf("tool executed %d times; want 1 (approved)", tool.executionCount())
	}
	if approval.calls != 1 {
		t.Errorf("approval called %d times; want 1", approval.calls)
	}
}

// TestPolicyAsk_Denied verifies that ToolActionAsk with a denying handler blocks execution.
func TestPolicyAsk_Denied(t *testing.T) {
	ctx := context.Background()
	tool := &mockTool{name: "bash", result: "should not run"}
	mock := &mockLLMClient{
		responses: []mockResponse{
			{toolCalls: []llm.ToolCall{{ID: "1", Name: "bash", Arguments: `{"command":"echo hi"}`}}},
			{content: "blocked"},
		},
	}
	approval := &countingApproval{approve: false}
	sess := newTestBuffer()
	_, _, err := runLoopWithUserMsg(ctx, mock, newTestToolRegistry(tool), sess, "hi", func(o *RunLoopOpts) {
		o.Policy = askPolicy{}
		o.Approval = approval
	})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if tool.executionCount() != 0 {
		t.Errorf("tool executed %d times; want 0 (denied by user)", tool.executionCount())
	}
	// Verify denial message reached the model.
	msgs := sess.messages
	var toolMsg *llm.Message
	for i := range msgs {
		if msgs[i].Role == "tool" {
			toolMsg = &msgs[i]
			break
		}
	}
	if toolMsg == nil || !strings.Contains(toolMsg.Content, "denied by user") {
		t.Errorf("tool message = %q; want 'denied by user'", toolMsg.Content)
	}
}

// TestPolicyAsk_NoHandler verifies that Ask without an ApprovalHandler collapses to Deny.
func TestPolicyAsk_NoHandler(t *testing.T) {
	ctx := context.Background()
	tool := &mockTool{name: "bash", result: "ran"}
	mock := &mockLLMClient{
		responses: []mockResponse{
			{toolCalls: []llm.ToolCall{{ID: "1", Name: "bash", Arguments: `{"command":"echo hi"}`}}},
			{content: "blocked"},
		},
	}
	sess := newTestBuffer()
	_, _, err := runLoopWithUserMsg(ctx, mock, newTestToolRegistry(tool), sess, "hi", func(o *RunLoopOpts) {
		o.Policy = askPolicy{}
		// No Approval handler — Ask collapses to Deny.
	})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if tool.executionCount() != 0 {
		t.Errorf("tool executed %d times; want 0 (Ask with no handler → Deny)", tool.executionCount())
	}
}

// TestLoopGuard verifies that repeated identical tool calls are blocked after the threshold.
// After the guard fires, the model receives an error result and eventually gives a final reply.
func TestLoopGuard(t *testing.T) {
	ctx := context.Background()
	tool := &mockTool{name: "ping", result: "pong"}

	// Build responses: defaultMaxRepeatedCalls+2 tool calls (guard fires after max), then a final reply.
	// The +2 ensures at least one blocked call before the model produces a final response.
	var responses []mockResponse
	for range defaultMaxRepeatedCalls + 2 {
		responses = append(responses, mockResponse{
			toolCalls: []llm.ToolCall{{ID: "1", Name: "ping", Arguments: `{}`}},
		})
	}
	responses = append(responses, mockResponse{content: "stopped"})
	mock := &mockLLMClient{responses: responses}

	sess := newTestBuffer()
	if err := sess.Append(llm.Message{Role: "user", Content: "ping forever"}); err != nil {
		t.Fatal(err)
	}
	_, _, _, err := RunLoop(ctx, RunLoopOpts{
		LLMClient:    mock,
		SystemPrompt: testSystemPrompt,
		ToolRegistry: newTestToolRegistry(tool),
		MaxIter:      defaultMaxRepeatedCalls + 4,
		History:      sess,
	})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	// Tool executes at most defaultMaxRepeatedCalls times; the rest are blocked by the guard.
	if tool.executionCount() > defaultMaxRepeatedCalls {
		t.Errorf("tool executed %d times; loop guard should cap at %d", tool.executionCount(), defaultMaxRepeatedCalls)
	}
}
