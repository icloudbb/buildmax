package cli

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/icloudbb/buildmax/internal/core/agent"
	"github.com/icloudbb/buildmax/internal/util"
)

// questionModel puts a question set up the way AskUser does, without a program:
// the handler holds the pending answer channel the run would block on.
func questionModel(t *testing.T, qs ...agent.Question) (*Model, chan agent.Answer) {
	t.Helper()
	h := NewTUIQuestionHandler()
	m := NewModel(TUIOpts{Session: testSessionContext(), Workspace: util.FixedRoot(t.TempDir()), Questioner: h})
	m.width = 100
	m.busy = true
	ch := make(chan agent.Answer, 1)
	h.pending["q1"] = ch
	next, _ := m.Update(questionRequestMsg{id: "q1", Questions: qs, response: ch})
	return next.(*Model), ch
}

func press(m *Model, msgs ...tea.KeyPressMsg) *Model {
	for _, msg := range msgs {
		next, _ := m.Update(msg)
		m = next.(*Model)
	}
	return m
}

func typeText(m *Model, s string) *Model {
	for _, r := range s {
		m = press(m, tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// panelText is the panel as the user reads it, without styling: the field's
// placeholder styles its first letter apart, which would split a plain match.
func panelText(m *Model) string { return ansiEscape.ReplaceAllString(m.renderQuestionPanel(), "") }

func key(code rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: code} }

func digit(d rune) tea.KeyPressMsg { return tea.KeyPressMsg{Code: d, Text: string(d)} }

func answered(t *testing.T, ch chan agent.Answer) agent.Answer {
	t.Helper()
	select {
	case a := <-ch:
		return a
	default:
		t.Fatal("the run got no answer")
		return agent.Answer{}
	}
}

func notAnswered(t *testing.T, ch chan agent.Answer) {
	t.Helper()
	select {
	case a := <-ch:
		t.Fatalf("the run was answered early with %+v", a)
	default:
	}
}

var (
	dbQuestion = agent.Question{Header: "Database", Text: "Which database?", Options: []agent.QuestionOption{
		{Label: "Postgres", Description: "what prod runs"}, {Label: "SQLite"},
	}}
	featureQuestion = agent.Question{Header: "Features", Text: "Which features?", MultiSelect: true, Options: []agent.QuestionOption{
		{Label: "Auth"}, {Label: "Billing"}, {Label: "Search"},
	}}
	nameQuestion = agent.Question{Header: "Name", Text: "What is the service called?"}
)

func TestQuestionPanelShowsTheQuestionOptionsAndAnswerField(t *testing.T) {
	m, _ := questionModel(t, dbQuestion)
	out := panelText(m)
	for _, want := range []string{"Which database?", "1. Postgres", "what prod runs", "2. SQLite", "Type something else", "esc: dismiss"} {
		if !strings.Contains(out, want) {
			t.Errorf("panel is missing %q:\n%s", want, out)
		}
	}
}

// The panel is the one place to type while it is up: the chat input and its
// "queue message" hint come back only once the questions are answered.
func TestQuestionPanelReplacesTheChatInput(t *testing.T) {
	m, _ := questionModel(t, nameQuestion)
	if v := ansiEscape.ReplaceAllString(m.View().Content, ""); strings.Contains(v, "Type a message") || strings.Contains(v, "queue message") {
		t.Fatalf("the chat input is still offered under the question:\n%s", v)
	}
	m = typeText(m, "orders-api")
	m = press(m, key(tea.KeyEnter))
	if v := ansiEscape.ReplaceAllString(m.View().Content, ""); !strings.Contains(v, "Type a message") && !strings.Contains(v, "queue") {
		t.Fatalf("the chat input did not come back after answering:\n%s", v)
	}
}

func TestQuestionDigitPicksAnOption(t *testing.T) {
	m, ch := questionModel(t, dbQuestion)
	m = press(m, digit('2'))
	if a := answered(t, ch); !reflect.DeepEqual(a.Values, []string{"SQLite"}) {
		t.Fatalf("answer = %+v, want SQLite", a)
	}
	if m.question != nil {
		t.Error("the panel is still up after the answer")
	}
}

func TestQuestionEnterSendsTheHighlightedOption(t *testing.T) {
	m, ch := questionModel(t, dbQuestion)
	press(m, key(tea.KeyDown), key(tea.KeyEnter))
	if a := answered(t, ch); !reflect.DeepEqual(a.Values, []string{"SQLite"}) {
		t.Fatalf("answer = %+v, want SQLite", a)
	}
}

// An answer of the user's own is typed in the panel, under its question: the
// chat input stays untouched, and a digit typed there is text, not a choice.
func TestQuestionOwnAnswerIsTypedInThePanel(t *testing.T) {
	m, ch := questionModel(t, dbQuestion)
	m = press(m, key(tea.KeyDown), key(tea.KeyDown))
	m = typeText(m, "MySQL 8")
	notAnswered(t, ch)
	if v := m.inputBlock.Value(); v != "" {
		t.Fatalf("the answer went to the chat input: %q", v)
	}
	if out := panelText(m); !strings.Contains(out, "MySQL 8") {
		t.Fatalf("the panel does not show what was typed:\n%s", out)
	}
	press(m, key(tea.KeyEnter))
	if a := answered(t, ch); !reflect.DeepEqual(a.Values, []string{"MySQL 8"}) {
		t.Fatalf("answer = %+v, want the typed text", a)
	}
}

// A question with no options starts on its answer field.
func TestOpenQuestionTakesTypingStraightAway(t *testing.T) {
	m, ch := questionModel(t, nameQuestion)
	m = typeText(m, "orders-api")
	press(m, key(tea.KeyEnter))
	if a := answered(t, ch); !reflect.DeepEqual(a.Values, []string{"orders-api"}) {
		t.Fatalf("answer = %+v", a)
	}
}

func TestMultiSelectSendsEveryCheckedOption(t *testing.T) {
	m, ch := questionModel(t, featureQuestion)
	m = press(m, key(tea.KeyEnter)) // nothing checked yet: nothing to send
	notAnswered(t, ch)
	m = press(m, key(tea.KeySpace), digit('3'))
	press(m, key(tea.KeyEnter))
	if a := answered(t, ch); !reflect.DeepEqual(a.Values, []string{"Auth, Search"}) {
		t.Fatalf("answer = %+v, want Auth and Search", a)
	}
}

// Several questions are answered one after another and sent together, each in
// its own form.
func TestQuestionSetIsAnsweredAsAWhole(t *testing.T) {
	m, ch := questionModel(t, dbQuestion, featureQuestion, nameQuestion)
	if out := panelText(m); !strings.Contains(out, "Database") || !strings.Contains(out, "Features") || !strings.Contains(out, "tab: next question") {
		t.Fatalf("panel does not show the set's tabs:\n%s", out)
	}
	m = press(m, digit('1'))
	notAnswered(t, ch)
	if m.question.current != 1 {
		t.Fatalf("answering the first question moved to %d, want 1", m.question.current)
	}
	m = press(m, digit('2'), key(tea.KeyEnter))
	notAnswered(t, ch)
	m = typeText(m, "orders-api")
	press(m, key(tea.KeyEnter))
	want := []string{"Postgres", "Billing", "orders-api"}
	if a := answered(t, ch); !reflect.DeepEqual(a.Values, want) || a.Declined {
		t.Fatalf("answer = %+v, want %v", a, want)
	}
}

func TestTabMovesBetweenQuestions(t *testing.T) {
	m, _ := questionModel(t, dbQuestion, nameQuestion)
	m = press(m, key(tea.KeyTab))
	if m.question.current != 1 {
		t.Fatalf("tab moved to %d, want 1", m.question.current)
	}
	m = press(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	if m.question.current != 0 {
		t.Fatalf("shift+tab moved to %d, want 0", m.question.current)
	}
}

func TestQuestionEscDismissesTheSet(t *testing.T) {
	m, ch := questionModel(t, dbQuestion, nameQuestion)
	press(m, key(tea.KeyEscape))
	if a := answered(t, ch); !a.Declined {
		t.Fatalf("answer = %+v, want a dismissal", a)
	}
}

// A run that stopped waiting withdraws its questions; the panel goes with them,
// and a late key press reaches nobody.
func TestWithdrawnQuestionClearsThePanel(t *testing.T) {
	m, ch := questionModel(t, dbQuestion)
	h := m.opts.Questioner.(*TUIQuestionHandler)
	h.withdraw("q1")
	next, _ := m.Update(questionWithdrawnMsg{id: "q1"})
	m = next.(*Model)
	if m.question != nil {
		t.Fatal("the panel outlived its run")
	}
	if h.deliver("q1", agent.Answer{Values: []string{"late"}}) {
		t.Fatal("a withdrawn question accepted an answer")
	}
	notAnswered(t, ch)
}
