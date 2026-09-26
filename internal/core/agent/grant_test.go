package agent

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/core/llm"
)

// scopedTool reports a grant scope taken from its "target" argument, standing in
// for a dispatching tool such as CallMcpTool.
type scopedTool struct{ *mockTool }

func (s scopedTool) GrantScope(args map[string]any) string {
	target, _ := args["target"].(string)
	return target
}

func askTwice(name, args string) []mockResponse {
	return []mockResponse{
		{toolCalls: []llm.ToolCall{{ID: "1", Name: name, Arguments: args}}},
		{toolCalls: []llm.ToolCall{{ID: "2", Name: name, Arguments: args}}},
		{content: "done"},
	}
}

// TestSessionGrant_StopsAsking is the point of the feature: a prompt the user
// cannot answer once is a prompt they turn off.
func TestSessionGrant_StopsAsking(t *testing.T) {
	tool := &mockTool{name: "bash", result: "ran"}
	approval := &countingApproval{approve: true, session: true}
	sess := newTestBuffer()

	_, _, err := runLoopWithUserMsg(context.Background(), &mockLLMClient{responses: askTwice("bash", `{"command":"echo hi"}`)},
		newTestToolRegistry(tool), sess, "hi", func(o *RunLoopOpts) {
			o.Policy = askPolicy{}
			o.Approval = approval
			o.Grants = NewSessionGrants()
		})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if approval.calls != 1 {
		t.Errorf("approval called %d times; want 1 (second call covered by the grant)", approval.calls)
	}
	if tool.executionCount() != 2 {
		t.Errorf("tool executed %d times; want 2", tool.executionCount())
	}
}

// TestSessionGrant_AllowOnceKeepsAsking pins the other half: allow-once must not
// leak into a grant.
func TestSessionGrant_AllowOnceKeepsAsking(t *testing.T) {
	tool := &mockTool{name: "bash", result: "ran"}
	approval := &countingApproval{approve: true}
	sess := newTestBuffer()

	_, _, err := runLoopWithUserMsg(context.Background(), &mockLLMClient{responses: askTwice("bash", `{"command":"echo hi"}`)},
		newTestToolRegistry(tool), sess, "hi", func(o *RunLoopOpts) {
			o.Policy = askPolicy{}
			o.Approval = approval
			o.Grants = NewSessionGrants()
		})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if approval.calls != 2 {
		t.Errorf("approval called %d times; want 2 (allow-once grants nothing)", approval.calls)
	}
}

// TestSessionGrant_CannotSoftenDeny is why the grant is consulted after
// resolution rather than ahead of it: it answers an Ask, and a Deny was never a
// question.
func TestSessionGrant_CannotSoftenDeny(t *testing.T) {
	tool := &mockTool{name: "bash", result: "should not run"}
	grants := NewSessionGrants()
	grants.grant("bash")
	sess := newTestBuffer()

	_, _, err := runLoopWithUserMsg(context.Background(), &mockLLMClient{responses: askTwice("bash", `{"command":"rm -rf /"}`)},
		newTestToolRegistry(tool), sess, "hi", func(o *RunLoopOpts) {
			o.Policy = denyPolicy{}
			o.Grants = grants
		})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if tool.executionCount() != 0 {
		t.Errorf("tool executed %d times; want 0 (a grant must not soften a policy denial)", tool.executionCount())
	}
	if !strings.Contains(lastToolMessage(t, sess), "denied by policy") {
		t.Error("want the policy denial to reach the model")
	}
}

// TestSessionGrant_ScopeIsolatesTargets: one approval for a dispatching tool
// must not cover every target it can reach.
func TestSessionGrant_ScopeIsolatesTargets(t *testing.T) {
	tool := scopedTool{&mockTool{name: "call", result: "ran"}}
	approval := &countingApproval{approve: true, session: true}
	sess := newTestBuffer()

	responses := []mockResponse{
		{toolCalls: []llm.ToolCall{{ID: "1", Name: "call", Arguments: `{"target":"github/create_issue"}`}}},
		{toolCalls: []llm.ToolCall{{ID: "2", Name: "call", Arguments: `{"target":"github/create_issue"}`}}},
		{toolCalls: []llm.ToolCall{{ID: "3", Name: "call", Arguments: `{"target":"jira/delete_issue"}`}}},
		{content: "done"},
	}
	_, _, err := runLoopWithUserMsg(context.Background(), &mockLLMClient{responses: responses},
		newTestToolRegistry(tool), sess, "hi", func(o *RunLoopOpts) {
			o.Policy = askPolicy{}
			o.Approval = approval
			o.Grants = NewSessionGrants()
		})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if approval.calls != 2 {
		t.Errorf("approval called %d times; want 2 (github granted, jira still asks)", approval.calls)
	}
	// The prompt names what the session grant would cover.
	if got := strings.Join(approval.targets, ","); got != "github/create_issue,jira/delete_issue" {
		t.Errorf("prompt targets = %q; want each prompt to carry its grant target", got)
	}
}

// TestSessionGrant_NilStoreGrantsNothing keeps the zero value usable on surfaces
// that do not offer the session option.
func TestSessionGrant_NilStoreGrantsNothing(t *testing.T) {
	var grants *SessionGrants
	grants.grant("bash")
	if grants.granted("bash") {
		t.Error("a nil store must grant nothing")
	}
	if len(grants.Scopes()) != 0 {
		t.Error("a nil store must report no scopes")
	}
}

func lastToolMessage(t *testing.T, sess *testBuffer) string {
	t.Helper()
	for i := len(sess.messages) - 1; i >= 0; i-- {
		if sess.messages[i].Role == "tool" {
			return sess.messages[i].Content
		}
	}
	t.Fatal("no tool-role message in history")
	return ""
}

// blockingApproval never answers, standing in for a prompt whose owner has
// walked away.
type blockingApproval struct {
	once    *sync.Once
	entered chan struct{}
}

func (b blockingApproval) RequestApproval(ctx context.Context, _ string, _ map[string]any, _ string) ApprovalDecision {
	b.once.Do(func() { close(b.entered) })
	<-ctx.Done()
	return ApprovalDeny
}

// TestApproval_ReturnsOnCancellation covers the deadlock this handler contract
// exists to prevent: a cancelled run whose prompt is still up must not leave
// the loop goroutine waiting for an answer nobody will give.
func TestApproval_ReturnsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	approval := blockingApproval{once: &sync.Once{}, entered: make(chan struct{})}
	tool := &mockTool{name: "bash", result: "should not run"}
	sess := newTestBuffer()

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = runLoopWithUserMsg(ctx, &mockLLMClient{responses: askTwice("bash", `{"command":"echo hi"}`)},
			newTestToolRegistry(tool), sess, "hi", func(o *RunLoopOpts) {
				o.Policy = askPolicy{}
				o.Approval = approval
			})
	}()

	<-approval.entered
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunLoop did not return after cancelling a run waiting on approval")
	}
	if tool.executionCount() != 0 {
		t.Errorf("tool executed %d times; want 0", tool.executionCount())
	}
}
