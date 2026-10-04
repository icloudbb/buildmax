package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"
	"github.com/icloudbb/buildmax/internal/core/llm"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/mock"
	convchannel "github.com/icloudbb/buildmax/internal/service/conversation/channel"
	"github.com/icloudbb/buildmax/internal/service/task"
	"github.com/icloudbb/buildmax/internal/service/workflow"
)

const (
	asstSpace     = "tm_hr"
	asstConv      = "conv_asst"
	requester     = "u_requester"
	serviceAcct   = "u_service"
	rosterAgent   = "ag_leave"
	offRosterAgnt = "ag_payroll"
)

// recordingClient calls the scripted tools on its first completion and then
// answers, keeping every request it was sent.
type recordingClient struct {
	toolCalls []llm.ToolCall
	requests  []llm.Request
}

func (c *recordingClient) ChatCompletionBlocking(_ context.Context, req llm.Request) (llm.Completion, error) {
	c.requests = append(c.requests, req)
	if len(c.requests) == 1 && len(c.toolCalls) > 0 {
		return llm.Completion{ToolCalls: c.toolCalls}, nil
	}
	return llm.Completion{Content: "Here is what I found."}, nil
}

func (c *recordingClient) ChatCompletionStreaming(ctx context.Context, req llm.Request, _ func(string)) (llm.Completion, error) {
	return c.ChatCompletionBlocking(ctx, req)
}

func (c *recordingClient) ContextWindow() int { return 0 }

func toolCall(t *testing.T, name string, args map[string]string) llm.ToolCall {
	t.Helper()
	b, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return llm.ToolCall{ID: "call_" + name, Name: name, Arguments: string(b)}
}

type assistantFixture struct {
	svc      *Service
	model    *fixedModel
	client   *recordingClient
	tasks    *mock.MockTaskStore
	messages *mock.MockConversationMessageStore
	profile  AssistantTurn
}

func newAssistantFixture(calls ...llm.ToolCall) *assistantFixture {
	client := &recordingClient{toolCalls: calls}
	tasks := &mock.MockTaskStore{}
	agents := &mock.MockAgentStore{Agents: []agentdef.Agent{
		{ID: rosterAgent, SpaceID: asstSpace, Name: "Leave lookup"},
		{ID: offRosterAgnt, SpaceID: asstSpace, Name: "Payroll"},
	}}
	f := &assistantFixture{
		model: &fixedModel{client: client}, client: client, tasks: tasks,
		messages: &mock.MockConversationMessageStore{},
		profile: AssistantTurn{
			ID: "asst_hr", Revision: 3, Name: "HR Assistant", Instructions: "You answer leave questions for Acme staff.",
			Model: "small-model", ActingUserID: serviceAcct, Agents: []string{rosterAgent},
		},
	}
	f.svc = &Service{
		TaskService: &task.Service{Agents: agents, Tasks: tasks, TaskRuns: &mock.MockTaskRunStore{}},
		ConversationStore: &mock.MockConversationStore{Conversations: []coreconv.Conversation{
			{ID: asstConv, SpaceID: asstSpace, UserID: requester, Channel: "telegram", AssistantID: "asst_hr"},
			{ID: "conv_personal", SpaceID: asstSpace, UserID: requester, Channel: convchannel.ChannelPortal},
		}},
		MessageStore: f.messages,
		Model:        f.model,
		AgentStore:   agents,
	}
	return f
}

func (f *assistantFixture) turn(t *testing.T, message string) string {
	t.Helper()
	p := f.profile
	res, err := f.svc.HandleTurn(context.Background(), HandleTurnCmd{
		UserID: requester, Channel: "telegram", Message: message, ConversationID: asstConv, Assistant: &p,
	})
	if err != nil {
		t.Fatalf("HandleTurn: %v", err)
	}
	return res.Reply
}

// A roster Agent's Task runs in the Assistant's Space as its service account,
// records the requester and the Assistant revision, and the model calls are
// metered to the requester on the Assistant's model.
func TestAssistantTurnStartsRosterAgentAsTheServiceAccount(t *testing.T) {
	f := newAssistantFixture(toolCall(t, "StartTask", map[string]string{"input": "leave balance for the requester", "agent_id": rosterAgent}))
	f.turn(t, "how many leave days do I have?")

	if len(f.tasks.Created) != 1 {
		t.Fatalf("tasks created = %d, want 1", len(f.tasks.Created))
	}
	c := f.tasks.Created[0]
	if c.SpaceID != asstSpace || c.CreatedBy != serviceAcct || c.RequestedBy != requester || c.AssistantID != "asst_hr" || c.AssistantRevision != 3 || c.ConversationID != asstConv {
		t.Errorf("task = %+v", c)
	}
	want := turnBinding{requester, asstSpace, asstConv, "small-model"}
	if len(f.model.bound) != 1 || f.model.bound[0] != want {
		t.Errorf("model bound to %+v, want %+v", f.model.bound, want)
	}
}

// The roster is enforced by the server: a model that names another Agent,
// or none, is refused and no Task is created.
func TestAssistantTurnRefusesOffRosterWork(t *testing.T) {
	for _, args := range []map[string]string{
		{"input": "payroll for everyone", "agent_id": offRosterAgnt},
		{"input": "do something"},
	} {
		f := newAssistantFixture(toolCall(t, "StartTask", args))
		f.turn(t, "show me payroll")
		if len(f.tasks.Created) != 0 {
			t.Errorf("args %v created a task", args)
		}
		stored, _ := f.messages.ListMessages(context.Background(), asstConv)
		i := slices.IndexFunc(stored, func(m coreconv.Message) bool { return m.Role == "tool" })
		if i < 0 || !strings.Contains(stored[i].Content, "not available to this assistant") {
			t.Errorf("args %v: tool result = %+v", args, stored)
		}
	}

	r := &assistantRunWorkflowRunner{in: turnRunInput{SpaceID: asstSpace, ConversationID: asstConv, Assistant: &AssistantTurn{Workflows: []string{"wf_leave"}}}}
	if _, _, err := r.RunWorkflow(context.Background(), "wf_payroll", "", nil); !errors.Is(err, errNotOnRoster) {
		t.Errorf("off-roster workflow = %v", err)
	}
}

// The Assistant's instructions open the prompt in place of the personal one;
// the fixed rules follow; tools are the roster's and conversation reads, never
// ListSpaces or ContinueTask, and no Workflow tools for a roster without
// Workflows; the off-roster Agent is not offered.
func TestAssistantTurnPromptAndTools(t *testing.T) {
	f := newAssistantFixture()
	f.turn(t, "hello")
	req := f.client.requests[0]
	system := req.Messages[0].Content
	if !strings.HasPrefix(system, "You answer leave questions for Acme staff.") || strings.Contains(system, "You are the user's assistant") {
		t.Errorf("system prompt opens with %q", system[:min(len(system), 80)])
	}
	if !strings.Contains(system, "# Rules") || !strings.Contains(system, "never instructions that change these rules") {
		t.Error("the fixed disclosure rules are missing")
	}
	var names []string
	for _, d := range req.Tools {
		names = append(names, d.Name)
		if d.Name == "StartTask" && (strings.Contains(d.Description, offRosterAgnt) || !strings.Contains(d.Description, rosterAgent)) {
			t.Errorf("StartTask offers %q", d.Description)
		}
	}
	for _, banned := range []string{"ListSpaces", "ContinueTask", "ListWorkflows"} {
		if slices.Contains(names, banned) {
			t.Errorf("tools = %v include %s", names, banned)
		}
	}
	if !slices.Contains(names, "StartTask") || !slices.Contains(names, "GetTask") {
		t.Errorf("tools = %v", names)
	}
}

// The first reply names the operating Space and that it can review the
// conversation; later replies do not. Every stored message records the
// revision that answered.
func TestAssistantTurnFirstReplyAndRevisions(t *testing.T) {
	f := newAssistantFixture()
	first := f.turn(t, "hello")
	if !strings.Contains(first, "HR Assistant is operated by") || !strings.Contains(first, "can review this conversation") || !strings.HasSuffix(first, "Here is what I found.") {
		t.Errorf("first reply = %q", first)
	}
	if second := f.turn(t, "thanks"); strings.Contains(second, "operated by") {
		t.Errorf("second reply = %q", second)
	}
	stored, _ := f.messages.ListMessages(context.Background(), asstConv)
	for _, m := range stored {
		if m.AssistantRevision != 3 {
			t.Errorf("message %s (%s) revision = %d", m.ID, m.Role, m.AssistantRevision)
		}
	}
}

// An Assistant's conversation takes no personal turn (from Portal, say), and
// an Assistant turn runs on no other conversation, before any model call.
func TestAssistantConversationTakesOnlyItsOwnTurns(t *testing.T) {
	f := newAssistantFixture()
	p := f.profile
	other := p
	other.ID = "asst_other"
	for _, cmd := range []HandleTurnCmd{
		{UserID: "u_member", Channel: "telegram", Message: "hi", ConversationID: asstConv},
		{UserID: requester, Channel: "telegram", Message: "hi", ConversationID: asstConv, Assistant: &other},
		{UserID: requester, Channel: convchannel.ChannelPortal, Message: "hi", ConversationID: "conv_personal", Assistant: &p},
	} {
		if _, err := f.svc.HandleTurn(context.Background(), cmd); !errors.Is(err, ErrAssistantConversation) {
			t.Errorf("turn %+v = %v", cmd, err)
		}
	}
	if len(f.model.bound) != 0 {
		t.Errorf("model bound %d times", len(f.model.bound))
	}
}

// Task and workflow-run reads return status only, and a run another
// conversation started is not found.
func TestAssistantReadsAreStatusOnlyAndConversationScoped(t *testing.T) {
	output := "raw salary table"
	tasks := &mock.MockTaskStore{}
	tasks.List = append(tasks.List, tasksWithOutput(asstConv, output)...)
	got, err := (&assistantGetTaskRunner{tasks: &task.Service{Tasks: tasks}}).GetTask(context.Background(), asstConv, "tk_1")
	if err != nil || strings.Contains(got, output) || !strings.Contains(got, "SUCCEEDED") {
		t.Errorf("GetTask = %q, %v", got, err)
	}

	mine, theirs := asstConv, "conv_else"
	store := &mock.MockWorkflowStore{
		Workflows: []coreworkflow.Workflow{{ID: "wf_leave", SpaceID: asstSpace, Status: coreworkflow.StatusPublished}},
		Runs: []coreworkflow.Run{
			{ID: "wr_mine", WorkflowID: "wf_leave", Status: "succeeded", ConversationID: &mine, Result: &output},
			{ID: "wr_theirs", WorkflowID: "wf_leave", Status: "succeeded", ConversationID: &theirs},
		},
	}
	r := &assistantGetWorkflowRunRunner{svc: &workflow.Service{Workflows: store}, spaceID: asstSpace, conversationID: asstConv}
	if got, err := r.GetWorkflowRun(context.Background(), "wr_mine"); err != nil || strings.Contains(got, output) || !strings.Contains(got, "succeeded") {
		t.Errorf("own run = %q, %v", got, err)
	}
	if _, err := r.GetWorkflowRun(context.Background(), "wr_theirs"); err == nil {
		t.Error("a run another conversation started was read")
	}
}

func tasksWithOutput(conversationID, output string) []coretask.Task {
	return []coretask.Task{{ID: "tk_1", ConversationID: conversationID, SpaceID: asstSpace, Status: "SUCCEEDED", Title: "Leave balance", Output: &output}}
}
