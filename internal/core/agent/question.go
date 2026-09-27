package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"unicode/utf8"
)

// ToolNameAskUser is here rather than in internal/tool because the loop names
// the tool in the Notification hook it fires, and core must not depend on tool.
const ToolNameAskUser = "AskUser"

// Bounds on a question set. It is read by a person between two keystrokes, so
// it is short: at most four questions, and a list longer than four options is a
// menu the Agent should have narrowed itself. The user can always answer in
// their own words instead.
const (
	MaxQuestions           = 4
	MaxQuestionChars       = 1000
	MaxQuestionHeaderChars = 24
	MaxQuestionOptions     = 4
	MaxOptionLabelChars    = 80
	MaxOptionDetailChars   = 200
)

// Question is one decision an Agent puts to the person driving the run. Header
// is a short label that tells several questions apart. With MultiSelect the
// user may pick more than one option.
type Question struct {
	Header      string           `json:"header,omitempty"`
	Text        string           `json:"question"`
	Options     []QuestionOption `json:"options,omitempty"`
	MultiSelect bool             `json:"multi_select,omitempty"`
}

// QuestionOption is one answer the Agent expects, offered so the user can pick
// it instead of typing it.
type QuestionOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

// Answer is the user's reply to one question set. Values[i] answers
// question i: an option label, the labels picked from a multi-select question
// joined with ", ", or the user's own words. Declined means they dismissed the
// whole set without answering, which is an answer too: the Agent must not ask
// it again.
//
// Deferred means nobody is waiting at the session: the set was handed on for
// the user to answer later, in their own words, as the next message of the
// conversation. RunLoop ends the turn once the current tool batch finishes,
// because an unattended run must not hold a worker for an answer.
type Answer struct {
	Values   []string
	Declined bool
	Deferred bool
}

// UserQuestioner puts a question set to the person driving the run and blocks
// until they answer. Like ApprovalHandler it must return on ctx.Done(): a
// cancelled run may never get an answer. It never times out otherwise, because
// a person who stepped away is still the one who has to decide.
type UserQuestioner interface {
	AskUser(ctx context.Context, questions []Question) (Answer, error)
}

// ValidateQuestions enforces the bounds above and rejects questions or options
// a user could not tell apart.
func ValidateQuestions(qs []Question) error {
	if len(qs) == 0 {
		return errors.New("at least one question is required")
	}
	if len(qs) > MaxQuestions {
		return fmt.Errorf("%d questions given; the limit is %d", len(qs), MaxQuestions)
	}
	seen := make(map[string]bool, len(qs))
	for i, q := range qs {
		if err := validateQuestion(q); err != nil {
			return fmt.Errorf("questions[%d]: %w", i, err)
		}
		key := strings.ToLower(strings.TrimSpace(q.Text))
		if seen[key] {
			return fmt.Errorf("questions[%d] repeats an earlier question", i)
		}
		seen[key] = true
	}
	return nil
}

func validateQuestion(q Question) error {
	if strings.TrimSpace(q.Text) == "" {
		return errors.New("question is required")
	}
	if n := utf8.RuneCountInString(q.Text); n > MaxQuestionChars {
		return fmt.Errorf("question is %d characters; the limit is %d", n, MaxQuestionChars)
	}
	if n := utf8.RuneCountInString(q.Header); n > MaxQuestionHeaderChars {
		return fmt.Errorf("header is %d characters; the limit is %d", n, MaxQuestionHeaderChars)
	}
	if len(q.Options) > MaxQuestionOptions {
		return fmt.Errorf("%d options given; the limit is %d", len(q.Options), MaxQuestionOptions)
	}
	if q.MultiSelect && len(q.Options) < 2 {
		return errors.New("multi_select needs at least two options to choose among")
	}
	seen := make(map[string]bool, len(q.Options))
	for i, o := range q.Options {
		label := strings.TrimSpace(o.Label)
		if label == "" {
			return fmt.Errorf("options[%d].label is required", i)
		}
		if n := utf8.RuneCountInString(label); n > MaxOptionLabelChars {
			return fmt.Errorf("options[%d].label is %d characters; the limit is %d", i, n, MaxOptionLabelChars)
		}
		if n := utf8.RuneCountInString(o.Description); n > MaxOptionDetailChars {
			return fmt.Errorf("options[%d].description is %d characters; the limit is %d", i, n, MaxOptionDetailChars)
		}
		key := strings.ToLower(label)
		if seen[key] {
			return fmt.Errorf("options[%d].label %q repeats an earlier option", i, label)
		}
		seen[key] = true
	}
	return nil
}

type questionerKey struct{}

// ctxWithQuestioner installs the run's questioner, or clears an inherited one
// when q is nil: a subagent runs on its parent's tool-call context, and must not
// reach the parent's user through it.
func ctxWithQuestioner(ctx context.Context, q UserQuestioner) context.Context {
	return context.WithValue(ctx, questionerKey{}, q)
}

// QuestionerFromCtx returns the run's questioner, or (nil, false) when nobody
// can answer — an unattended run, or a subagent.
func QuestionerFromCtx(ctx context.Context) (UserQuestioner, bool) {
	q, ok := ctx.Value(questionerKey{}).(UserQuestioner)
	return q, ok && q != nil
}

// notifyingQuestioner fires the Notification hook before the question goes up,
// as the approval gate does, so an operator's notifier can tell "needs a
// decision" apart from "done".
//
// It also records a deferred answer in yield, which is how the loop learns to
// end the turn: the tool's result alone would only ask the model to stop.
type notifyingQuestioner struct {
	opts  RunLoopOpts
	inner UserQuestioner
	yield *atomic.Bool
}

func (n notifyingQuestioner) AskUser(ctx context.Context, qs []Question) (Answer, error) {
	fireNotification(ctx, n.opts, NotificationUserQuestion, ToolNameAskUser, ToolCallFromCtx(ctx), questionArgs(qs), "")
	a, err := n.inner.AskUser(ctx, qs)
	if err == nil && a.Deferred {
		n.yield.Store(true)
	}
	return a, err
}

// questionArgs is what the hook sees: the question texts, not the options,
// which a notifier has no use for.
func questionArgs(qs []Question) map[string]any {
	texts := make([]any, len(qs))
	for i, q := range qs {
		texts[i] = q.Text
	}
	return map[string]any{"questions": texts}
}
