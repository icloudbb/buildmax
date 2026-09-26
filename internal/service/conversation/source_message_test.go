package conversation

import (
	"context"
	"encoding/json"
	"testing"

	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"
	"github.com/icloudbb/buildmax/internal/core/llm"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/mock"
	convchannel "github.com/icloudbb/buildmax/internal/service/conversation/channel"
	"github.com/icloudbb/buildmax/internal/service/task"
)

// toolThenReplyClient calls one tool on its first completion and answers with
// text on the second, which is the shape of a turn that starts a task.
type toolThenReplyClient struct {
	toolName string
	args     string
	calls    int
}

func (c *toolThenReplyClient) ChatCompletionBlocking(ctx context.Context, req llm.Request) (llm.Completion, error) {
	c.calls++
	if c.calls == 1 {
		return llm.Completion{ToolCalls: []llm.ToolCall{{ID: "call_1", Name: c.toolName, Arguments: c.args}}}, nil
	}
	return llm.Completion{Content: "started"}, nil
}

func (c *toolThenReplyClient) ChatCompletionStreaming(ctx context.Context, req llm.Request, onDelta func(string)) (llm.Completion, error) {
	return c.ChatCompletionBlocking(ctx, req)
}

func (c *toolThenReplyClient) ContextWindow() int { return 0 }

func startTaskArgs(t *testing.T, input string) string {
	t.Helper()
	b, err := json.Marshal(map[string]string{"input": input})
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// The run a turn creates records the message that asked for it, so the request
// sent to a worker can be compared with what the person actually said. The two
// texts differ here on purpose: that difference is the whole reason to store it.
func TestStartTaskRecordsTheMessageThatAskedForIt(t *testing.T) {
	const conversationID = "conv-1"
	const spaceID = "tm_1"
	tasks := &mock.MockTaskStore{}
	messages := &mock.MockConversationMessageStore{}
	svc := &Service{
		TaskService:       &task.Service{Tasks: tasks, TaskRuns: &mock.MockTaskRunStore{}},
		ConversationStore: &mock.MockConversationStore{Conversations: []coreconv.Conversation{{ID: conversationID, SpaceID: spaceID, Channel: convchannel.ChannelPortal}}},
		MessageStore:      messages,
		LLMClient:         &toolThenReplyClient{toolName: "StartTask", args: startTaskArgs(t, "investigate the flaky test")},
	}

	if _, err := svc.HandleTurn(context.Background(), HandleTurnCmd{
		UserID:         "u1",
		Channel:        convchannel.ChannelPortal,
		Message:        "look into the flaky test, but leave the CI config alone",
		ConversationID: conversationID,
	}); err != nil {
		t.Fatalf("HandleTurn: %v", err)
	}

	if len(tasks.Created) != 1 {
		t.Fatalf("tasks created = %d, want 1", len(tasks.Created))
	}
	created := tasks.Created[0]
	if created.InitialRunSourceMessageID == nil {
		t.Fatal("the run records no source message")
	}
	stored, err := messages.ListMessages(context.Background(), conversationID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) == 0 {
		t.Fatal("the turn stored no messages")
	}
	if *created.InitialRunSourceMessageID != stored[0].ID {
		t.Errorf("source message = %q, want the incoming message %q", *created.InitialRunSourceMessageID, stored[0].ID)
	}
	if stored[0].Content == created.Input {
		t.Error("this test is only meaningful when the run input differs from what was said")
	}
}

// A continuation is its own request. The run it creates points at the message
// that asked for it, not at the one that created the task.
func TestContinueTaskRecordsItsOwnMessage(t *testing.T) {
	const conversationID = "conv-1"
	const spaceID = "tm_1"
	tasks := &mock.MockTaskStore{List: []coretask.Task{{
		ID: "tk_1", ConversationID: conversationID, SpaceID: spaceID, Status: "SUCCEEDED", Input: "first",
	}}}
	runs := &mock.MockTaskRunStore{}
	messages := &mock.MockConversationMessageStore{}
	args, err := json.Marshal(map[string]string{"task_id": "tk_1", "input": "check the Windows runner"})
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{
		TaskService:       &task.Service{Tasks: tasks, TaskRuns: runs},
		ConversationStore: &mock.MockConversationStore{Conversations: []coreconv.Conversation{{ID: conversationID, SpaceID: spaceID, Channel: convchannel.ChannelPortal}}},
		MessageStore:      messages,
		LLMClient:         &toolThenReplyClient{toolName: "ContinueTask", args: string(args)},
	}

	if _, err := svc.HandleTurn(context.Background(), HandleTurnCmd{
		UserID:         "u1",
		Channel:        convchannel.ChannelPortal,
		Message:        "now check whether it fails on Windows too",
		ConversationID: conversationID,
	}); err != nil {
		t.Fatalf("HandleTurn: %v", err)
	}

	if len(runs.Runs) != 1 {
		t.Fatalf("runs created = %d, want 1", len(runs.Runs))
	}
	stored, err := messages.ListMessages(context.Background(), conversationID)
	if err != nil {
		t.Fatal(err)
	}
	if runs.Runs[0].SourceMessageID == nil || *runs.Runs[0].SourceMessageID != stored[0].ID {
		t.Errorf("source message = %v, want the incoming message %q", runs.Runs[0].SourceMessageID, stored[0].ID)
	}
}
