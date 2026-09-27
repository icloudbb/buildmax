package desktop

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/agentapp"
	"github.com/icloudbb/buildmax/internal/core/agent"
)

type questionResult struct {
	answer agent.Answer
	err    error
}

// questioningHost is a RunHost whose turn asks one question and reports what
// came back, so a test can hold a run on a question without a model.
type questioningHost struct{ got chan questionResult }

func (h *questioningHost) OpenSession(string) (*agentapp.SessionContext, error) {
	return agentapp.NewSessionContext(""), nil
}
func (h *questioningHost) CloseSession(*agentapp.SessionContext) {}

func (h *questioningHost) RunPrompt(ctx context.Context, _ *agentapp.SessionContext, _ string, o agentapp.RunPromptOpts) (agentapp.RunResult, error) {
	a, err := o.Questioner.AskUser(ctx, []agent.Question{
		{Header: "Database", Text: "Which database?", Options: []agent.QuestionOption{{Label: "Postgres"}, {Label: "SQLite"}}},
		{Text: "What is it called?"},
	})
	h.got <- questionResult{a, err}
	return agentapp.RunResult{}, nil
}

func (h *questioningHost) RunBackgroundEvent(ctx context.Context, s *agentapp.SessionContext, _ agentapp.BackgroundEvent, o agentapp.RunPromptOpts) (agentapp.RunResult, error) {
	return h.RunPrompt(ctx, s, "", o)
}

func (h *questioningHost) result(t *testing.T) questionResult {
	t.Helper()
	select {
	case r := <-h.got:
		return r
	case <-time.After(10 * time.Second):
		t.Fatal("the run never got an answer")
		return questionResult{}
	}
}

// startQuestioningRun binds a questioner the way hostForProject does.
func startQuestioningRun(t *testing.T, app *App, sessionID string) *questioningHost {
	t.Helper()
	host := &questioningHost{got: make(chan questionResult, 1)}
	lc := &desktopRun{app: app, ctx: context.Background(), projectID: "p", sessionID: sessionID, key: runKey("p", sessionID)}
	lc.questioner = &runQuestioner{app: app, run: lc}
	hostFn := func() (agentapp.RunHost, error) { return host, nil }
	if _, err := app.scheduler.Submit(context.Background(), lc.key, sessionID, "go", hostFn, lc); err != nil {
		t.Fatalf("submit %s: %v", sessionID, err)
	}
	return host
}

func TestQuestionReachesItsChatAndTakesTheAnswer(t *testing.T) {
	app, events := approvalApp()
	host := startQuestioningRun(t, app, "sA")
	req, ok := events.waitFor(t, eventQuestionRequest).(*QuestionRequestPayload)
	if !ok {
		t.Fatal("no question request payload")
	}
	// The run adopts the session it opened, and tags the question with it.
	if req.SessionID == "" || req.ProjectID != "p" || len(req.Questions) != 2 ||
		req.Questions[0].Header != "Database" || req.Questions[0].Options[1].Label != "SQLite" {
		t.Fatalf("request = %+v", req)
	}

	// An empty answer is not an answer; it must not release the run.
	if err := app.RespondQuestion(req.QuestionID, []string{"Postgres", "  "}, false); err == nil {
		t.Fatal("an empty answer was accepted")
	}
	if err := app.RespondQuestion(req.QuestionID, nil, false); err == nil {
		t.Fatal("no answers were accepted")
	}
	if err := app.RespondQuestion(req.QuestionID, []string{"Postgres", " orders-api "}, false); err != nil {
		t.Fatalf("respond: %v", err)
	}
	if r := host.result(t); r.err != nil || r.answer.Declined || !reflect.DeepEqual(r.answer.Values, []string{"Postgres", "orders-api"}) {
		t.Fatalf("run got %+v", r)
	}
	if err := app.RespondQuestion(req.QuestionID, []string{"again", "again"}, false); err == nil {
		t.Fatal("a question was answered twice")
	}
}

func TestDismissedQuestionReachesTheRunAsDeclined(t *testing.T) {
	app, events := approvalApp()
	host := startQuestioningRun(t, app, "sA")
	req := events.waitFor(t, eventQuestionRequest).(*QuestionRequestPayload)
	if err := app.RespondQuestion(req.QuestionID, []string{"ignored"}, true); err != nil {
		t.Fatalf("respond: %v", err)
	}
	if r := host.result(t); !r.answer.Declined || r.answer.Values != nil {
		t.Fatalf("run got %+v, want a bare dismissal", r)
	}
}

// Cancelling a run withdraws its question: the run returns, and a late answer
// reaches nobody.
func TestCancelledRunWithdrawsItsQuestion(t *testing.T) {
	app, events := approvalApp()
	host := startQuestioningRun(t, app, "sA")
	req := events.waitFor(t, eventQuestionRequest).(*QuestionRequestPayload)
	if err := app.CancelRun("p", "sA"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if r := host.result(t); !errors.Is(r.err, context.Canceled) {
		t.Fatalf("run got %+v, want a cancellation", r)
	}
	if err := app.RespondQuestion(req.QuestionID, []string{"late", "late"}, false); err == nil {
		t.Fatal("a withdrawn question accepted an answer")
	}
}
