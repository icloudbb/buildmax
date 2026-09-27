package agent

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/llm"
)

type fixedQuestioner struct {
	answer Answer
	asked  [][]Question
}

func (q *fixedQuestioner) AskUser(_ context.Context, qs []Question) (Answer, error) {
	q.asked = append(q.asked, qs)
	return q.answer, nil
}

// askingTool stands in for AskUser: it asks through whatever questioner the
// run put on its context, and reports whether there was one.
type askingTool struct {
	found  bool
	answer Answer
}

func (t *askingTool) Name() string        { return ToolNameAskUser }
func (t *askingTool) Description() string { return "asks" }
func (t *askingTool) Parameters() any     { return map[string]any{} }

func (t *askingTool) Execute(ctx context.Context, _ map[string]any) (string, error) {
	q, ok := QuestionerFromCtx(ctx)
	t.found = ok
	if !ok {
		return "nobody to ask", nil
	}
	a, err := q.AskUser(ctx, []Question{{Text: "Which database?", Options: []QuestionOption{{Label: "Postgres"}}}})
	t.answer = a
	return strings.Join(a.Values, ","), err
}

func runAskingLoop(t *testing.T, ctx context.Context, tool *askingTool, opts RunLoopOpts) {
	t.Helper()
	history := newTestBuffer()
	_ = history.Append(llm.Message{Role: "user", Content: "go"})
	opts.LLMClient = &mockLLMClient{responses: []mockResponse{
		{toolCalls: []llm.ToolCall{{ID: "call_1", Name: ToolNameAskUser, Arguments: "{}"}}},
		{content: "done"},
	}}
	opts.ToolRegistry = newTestToolRegistry(tool)
	opts.MaxIter = 5
	opts.History = history
	if _, _, _, err := RunLoop(ctx, opts); err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
}

// The run's questioner reaches the tool, and the Notification hook announces
// the question before it goes up, as it does for an approval prompt.
func TestRunLoopHandsTheQuestionerToToolsAndNotifies(t *testing.T) {
	questioner := &fixedQuestioner{answer: Answer{Values: []string{"Postgres"}}}
	hooks := &recordingHookRunner{}
	tool := &askingTool{}
	runAskingLoop(t, context.Background(), tool, RunLoopOpts{Questioner: questioner, Hooks: hooks})

	if !tool.found || len(tool.answer.Values) != 1 || tool.answer.Values[0] != "Postgres" {
		t.Fatalf("tool found questioner = %v, answer = %+v", tool.found, tool.answer)
	}
	if len(questioner.asked) != 1 || questioner.asked[0][0].Text != "Which database?" {
		t.Fatalf("asked = %+v", questioner.asked)
	}
	var notified []HookInput
	for _, c := range hooks.snapshot() {
		if c.Event == HookNotification {
			notified = append(notified, c)
		}
	}
	if len(notified) != 1 || notified[0].NotificationKind != NotificationUserQuestion ||
		notified[0].ToolName != ToolNameAskUser || notified[0].ToolCallID != "call_1" ||
		!reflect.DeepEqual(notified[0].ToolArgs["questions"], []any{"Which database?"}) {
		t.Fatalf("notifications = %+v, want one user_question for call_1", notified)
	}
}

// A subagent runs on its parent's tool-call context. A loop given no
// questioner must clear the inherited one, or the delegate would reach the
// parent's user.
func TestRunLoopWithoutQuestionerClearsAnInheritedOne(t *testing.T) {
	parent := &fixedQuestioner{answer: Answer{Values: []string{"leaked"}}}
	ctx := ctxWithQuestioner(context.Background(), parent)
	tool := &askingTool{}
	runAskingLoop(t, ctx, tool, RunLoopOpts{})

	if tool.found || len(parent.asked) != 0 {
		t.Fatalf("a run without a questioner reached the inherited one (asked %d)", len(parent.asked))
	}
}

func TestValidateQuestions(t *testing.T) {
	opts := func(labels ...string) []QuestionOption {
		out := make([]QuestionOption, len(labels))
		for i, l := range labels {
			out[i] = QuestionOption{Label: l}
		}
		return out
	}
	one := func(q Question) []Question { return []Question{q} }
	cases := []struct {
		name    string
		qs      []Question
		wantErr string
	}{
		{"open question", one(Question{Text: "What should the service be called?"}), ""},
		{"with options", one(Question{Text: "Which DB?", Options: opts("Postgres", "SQLite")}), ""},
		{"several questions of different forms", []Question{
			{Header: "Database", Text: "Which DB?", Options: opts("Postgres", "SQLite")},
			{Header: "Features", Text: "Which features?", Options: opts("Auth", "Billing", "Search"), MultiSelect: true},
			{Header: "Name", Text: "What is it called?"},
		}, ""},
		{"none", nil, "at least one"},
		{"too many questions", []Question{{Text: "a"}, {Text: "b"}, {Text: "c"}, {Text: "d"}, {Text: "e"}}, "limit"},
		{"repeated question", []Question{{Text: "Which?"}, {Text: "which?"}}, "repeats"},
		{"empty", one(Question{Text: "  "}), "required"},
		{"too long", one(Question{Text: strings.Repeat("x", MaxQuestionChars+1)}), "limit"},
		{"long header", one(Question{Header: strings.Repeat("h", MaxQuestionHeaderChars+1), Text: "Which?"}), "header"},
		{"too many options", one(Question{Text: "Which?", Options: opts("a", "b", "c", "d", "e")}), "limit"},
		{"multi-select with one option", one(Question{Text: "Which?", Options: opts("a"), MultiSelect: true}), "multi_select"},
		{"blank label", one(Question{Text: "Which?", Options: opts("a", " ")}), "label is required"},
		{"duplicate label", one(Question{Text: "Which?", Options: opts("Yes", "yes")}), "repeats"},
		{"long label", one(Question{Text: "Which?", Options: opts(strings.Repeat("x", MaxOptionLabelChars+1))}), "limit"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := ValidateQuestions(c.qs)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateQuestions: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("err = %v, want one containing %q", err, c.wantErr)
			}
		})
	}
}

// An unattended run hands its questions on and must not keep going: the turn
// ends after the tool batch with no further model call, so the model neither
// guesses the answer nor holds the worker waiting for one.
func TestDeferredAnswerEndsTheTurnAfterTheBatch(t *testing.T) {
	history := newTestBuffer()
	_ = history.Append(llm.Message{Role: "user", Content: "go"})
	client := &mockLLMClient{responses: []mockResponse{
		{content: "I have looked at the repo.", toolCalls: []llm.ToolCall{{ID: "call_1", Name: ToolNameAskUser, Arguments: "{}"}}},
		{content: "this call must never happen"},
	}}
	tool := &askingTool{}
	reply, _, structured, err := RunLoop(context.Background(), RunLoopOpts{
		LLMClient:    client,
		ToolRegistry: newTestToolRegistry(tool),
		MaxIter:      5,
		History:      history,
		Questioner:   &fixedQuestioner{answer: Answer{Deferred: true}},
	})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	if client.calls != 1 {
		t.Fatalf("model called %d times, want 1: the turn must end after the question", client.calls)
	}
	if reply != "I have looked at the repo." || structured != nil {
		t.Fatalf("reply = %q, structured = %v", reply, structured)
	}
	last := history.messages[len(history.messages)-1]
	if last.Role != "tool" {
		t.Fatalf("history ends with %q, want the tool result the next user message follows", last.Role)
	}
}
