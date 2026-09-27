package tool

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/agent"
	"github.com/icloudbb/buildmax/internal/core/llm"
)

type stubQuestioner struct {
	answer agent.Answer
	err    error
	got    []agent.Question
}

func (s *stubQuestioner) AskUser(_ context.Context, qs []agent.Question) (agent.Answer, error) {
	s.got = qs
	return s.answer, s.err
}

// The tool reaches the questioner only through the run loop, which is where a
// surface's questioner is installed; the test drives it the same way.
func askThroughLoop(t *testing.T, q agent.UserQuestioner, args string) string {
	t.Helper()
	var result string
	client := &scriptedClient{calls: []llm.ToolCall{{ID: "c1", Name: ToolNameAskUser, Arguments: args}}}
	history := &memHistory{}
	_ = history.Append(llm.Message{Role: "user", Content: "go"})
	registry := llm.NewToolRegistry()
	registry.AppendTools(NewAskUser())
	_, _, _, err := agent.RunLoop(context.Background(), agent.RunLoopOpts{
		LLMClient:    client,
		ToolRegistry: registry,
		MaxIter:      3,
		History:      history,
		Questioner:   q,
	})
	if err != nil {
		t.Fatalf("RunLoop: %v", err)
	}
	for _, m := range history.msgs {
		if m.Role == "tool" {
			result = m.Content
		}
	}
	return result
}

func TestAskUserReturnsEachAnswerUnderItsQuestion(t *testing.T) {
	q := &stubQuestioner{answer: agent.Answer{Values: []string{"Postgres", "Auth, Search", "orders-api"}}}
	got := askThroughLoop(t, q, `{"questions":[
		{"header":"Database","question":"Which database?","options":[{"label":"Postgres","description":"what prod runs"},{"label":"SQLite"}]},
		{"header":"Features","question":"Which features?","options":[{"label":"Auth"},{"label":"Billing"},{"label":"Search"}],"multi_select":true},
		{"question":"What is the service called?"}]}`)
	want := "The user answered:\nQ: Which database?\nA: Postgres\nQ: Which features?\nA: Auth, Search\nQ: What is the service called?\nA: orders-api"
	if got != want {
		t.Fatalf("result = %q, want %q", got, want)
	}
	if len(q.got) != 3 || q.got[0].Header != "Database" || q.got[0].Options[0] != (agent.QuestionOption{Label: "Postgres", Description: "what prod runs"}) ||
		!q.got[1].MultiSelect || len(q.got[1].Options) != 3 || len(q.got[2].Options) != 0 {
		t.Fatalf("asked %+v", q.got)
	}
}

func TestAskUserReportsADismissal(t *testing.T) {
	got := askThroughLoop(t, &stubQuestioner{answer: agent.Answer{Declined: true}}, `{"questions":[{"question":"Which?"}]}`)
	if !strings.Contains(got, "dismissed") || !strings.Contains(got, "Do not ask them again") {
		t.Fatalf("result = %q", got)
	}
}

// A run with nobody to ask is told so plainly, and to go on, rather than
// failing: the question was a request for judgment the Agent can still make.
func TestAskUserWithNobodyToAsk(t *testing.T) {
	got := askThroughLoop(t, nil, `{"questions":[{"question":"Which?"}]}`)
	if !strings.Contains(got, "Nobody is available") || !strings.Contains(got, "assumptions") {
		t.Fatalf("result = %q", got)
	}
}

func TestAskUserRejectsAnInvalidSetBeforeAsking(t *testing.T) {
	for _, args := range []string{
		`{"question":"Which?"}`,
		`{"questions":[]}`,
		`{"questions":[{"question":"Which?","options":[{"label":"a"},{"label":"b"},{"label":"c"},{"label":"d"},{"label":"e"}]}]}`,
		`{"questions":[{"question":"Which?","options":[{"label":"only"}],"multi_select":true}]}`,
	} {
		q := &stubQuestioner{answer: agent.Answer{Values: []string{"x"}}}
		got := askThroughLoop(t, q, args)
		if !strings.HasPrefix(got, "error") || q.got != nil {
			t.Errorf("%s: result = %q, asked %+v", args, got, q.got)
		}
	}
}

func TestAskUserRefusesAnAnswerThatMissesAQuestion(t *testing.T) {
	q := &stubQuestioner{answer: agent.Answer{Values: []string{"only one"}}}
	got := askThroughLoop(t, q, `{"questions":[{"question":"First?"},{"question":"Second?"}]}`)
	if !strings.Contains(got, "1 answers for 2 questions") {
		t.Fatalf("result = %q", got)
	}
}

func TestAskUserSurfacesAnUnansweredSet(t *testing.T) {
	got := askThroughLoop(t, &stubQuestioner{err: errors.New("context canceled")}, `{"questions":[{"question":"Which?"}]}`)
	if !strings.Contains(got, "not answered") {
		t.Fatalf("result = %q", got)
	}
}

// Asking must never itself need approval, and it must never share a parallel
// batch: it holds the run, and a surface shows one question at a time.
func TestAskUserIsAllowedButRunsAlone(t *testing.T) {
	tool := NewAskUser()
	if tool.DefaultAction() != llm.ToolActionAllow {
		t.Errorf("DefaultAction = %v, want Allow", tool.DefaultAction())
	}
	if tool.Access(nil) != llm.AccessWrite {
		t.Errorf("Access = %v, want AccessWrite", tool.Access(nil))
	}
}
