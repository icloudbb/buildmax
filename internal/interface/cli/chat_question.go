package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/icloudbb/buildmax/internal/core/agent"
	"github.com/icloudbb/buildmax/internal/util"
)

// questionRequestMsg puts an AskUser question set up in the TUI. The run blocks
// on response until the user answers or the run is cancelled; id correlates a
// cancel with the panel it must dismiss.
type questionRequestMsg struct {
	id        string
	Questions []agent.Question
	response  chan agent.Answer
}

// questionWithdrawnMsg dismisses the panel of a question set whose run stopped
// waiting for it.
type questionWithdrawnMsg struct{ id string }

// TUIQuestionHandler implements agent.UserQuestioner for the Bubble Tea TUI.
// Create it before the program and wire the program in after tea.NewProgram.
type TUIQuestionHandler struct {
	program *tea.Program

	mu      sync.Mutex
	pending map[string]chan agent.Answer
}

func NewTUIQuestionHandler() *TUIQuestionHandler {
	return &TUIQuestionHandler{pending: make(map[string]chan agent.Answer)}
}

func (h *TUIQuestionHandler) SetProgram(p *tea.Program) { h.program = p }

// AskUser shows the questions and blocks until they are answered or the run ends.
func (h *TUIQuestionHandler) AskUser(ctx context.Context, qs []agent.Question) (agent.Answer, error) {
	if h.program == nil {
		return agent.Answer{}, errors.New("no terminal to ask in")
	}
	id, _ := util.NewPublicID()
	ch := make(chan agent.Answer, 1)
	h.mu.Lock()
	h.pending[id] = ch
	h.mu.Unlock()

	h.program.Send(questionRequestMsg{id: id, Questions: qs, response: ch})

	select {
	case a := <-ch:
		return a, nil
	case <-ctx.Done():
		h.withdraw(id)
		h.program.Send(questionWithdrawnMsg{id: id})
		return agent.Answer{}, ctx.Err()
	}
}

// deliver hands the answer to the waiting run. It is a no-op for a question set
// already answered or withdrawn, so a late key press reaches no run.
func (h *TUIQuestionHandler) deliver(id string, a agent.Answer) bool {
	h.mu.Lock()
	ch, ok := h.pending[id]
	delete(h.pending, id)
	h.mu.Unlock()
	if ok {
		ch <- a // buffered, and only one deliver can find it
	}
	return ok
}

func (h *TUIQuestionHandler) withdraw(id string) {
	h.mu.Lock()
	delete(h.pending, id)
	h.mu.Unlock()
}

// questionForm is the panel's state for one question set. Each question has one
// row per option plus a last row holding its own answer field, so an answer of
// the user's own is typed under the question it answers, not in the chat input.
type questionForm struct {
	req     *questionRequestMsg
	current int
	cursor  int
	checked [][]bool
	answers []string // "" until answered
	inputs  []textinput.Model
}

func newQuestionForm(req *questionRequestMsg, width int) *questionForm {
	f := &questionForm{
		req:     req,
		checked: make([][]bool, len(req.Questions)),
		answers: make([]string, len(req.Questions)),
		inputs:  make([]textinput.Model, len(req.Questions)),
	}
	for i, q := range req.Questions {
		f.checked[i] = make([]bool, len(q.Options))
		in := textinput.New()
		in.Prompt = ""
		in.Placeholder = "Type something else…"
		if len(q.Options) == 0 {
			in.Placeholder = "Type your answer…"
		}
		in.SetWidth(max(width-12, 20))
		f.inputs[i] = in
	}
	f.focus()
	return f
}

func (f *questionForm) question() agent.Question { return f.req.Questions[f.current] }

// onInput reports whether the cursor is on the current question's answer field.
func (f *questionForm) onInput() bool { return f.cursor == len(f.question().Options) }

// focus gives the answer field the keyboard exactly when the cursor is on it.
func (f *questionForm) focus() {
	for i := range f.inputs {
		if i == f.current && f.onInput() {
			f.inputs[i].Focus()
		} else {
			f.inputs[i].Blur()
		}
	}
}

func (f *questionForm) moveTo(question int) {
	f.current = (question + len(f.req.Questions)) % len(f.req.Questions)
	f.cursor = 0
	f.focus()
}

// answer records the current question's answer and moves to the next
// unanswered one. It reports true once every question has an answer.
func (f *questionForm) answer(value string) bool {
	f.answers[f.current] = value
	for step := 1; step <= len(f.answers); step++ {
		next := (f.current + step) % len(f.answers)
		if f.answers[next] == "" {
			f.moveTo(next)
			return false
		}
	}
	return true
}

// confirm answers the current question from its row state: the option under the
// cursor, the checked options plus any typed text for a multi-select question,
// or the typed text. It is a no-op when there is nothing to send yet.
func (f *questionForm) confirm() bool {
	q := f.question()
	typed := strings.TrimSpace(f.inputs[f.current].Value())
	if q.MultiSelect {
		var picked []string
		for i, on := range f.checked[f.current] {
			if on {
				picked = append(picked, q.Options[i].Label)
			}
		}
		if typed != "" {
			picked = append(picked, typed)
		}
		if len(picked) == 0 {
			return false
		}
		return f.answer(strings.Join(picked, ", "))
	}
	if f.onInput() {
		if typed == "" {
			return false
		}
		return f.answer(typed)
	}
	return f.answer(q.Options[f.cursor].Label)
}

// handleQuestionKey drives the panel. While it is up it owns the keyboard, so
// nothing typed for it lands in the chat input.
//
//   - ↑/↓ move between a question's options and its answer field.
//   - Enter picks the option under the cursor, sends the typed answer, or, on a
//     multi-select question, sends what is checked (plus anything typed).
//   - Space checks an option of a multi-select question; a digit picks (or
//     checks) that option outright. On the answer field both are just text.
//   - Tab / Shift+Tab switch between questions; Esc dismisses the whole set.
func handleQuestionKey(m *Model, msg tea.KeyPressMsg) tea.Cmd {
	f := m.question
	q := f.question()
	switch msg.Code {
	case tea.KeyEscape:
		return m.finishQuestion(agent.Answer{Declined: true})
	case tea.KeyTab:
		if msg.Mod&tea.ModShift != 0 {
			f.moveTo(f.current - 1)
		} else {
			f.moveTo(f.current + 1)
		}
		return nil
	case tea.KeyUp:
		if f.cursor > 0 {
			f.cursor--
			f.focus()
		}
		return nil
	case tea.KeyDown:
		if f.cursor < len(q.Options) {
			f.cursor++
			f.focus()
		}
		return nil
	case tea.KeyEnter:
		if f.confirm() {
			return m.finishQuestion(agent.Answer{Values: f.answers})
		}
		return nil
	}
	if f.onInput() {
		var cmd tea.Cmd
		f.inputs[f.current], cmd = f.inputs[f.current].Update(msg)
		return cmd
	}
	if msg.Code == tea.KeySpace && q.MultiSelect {
		f.checked[f.current][f.cursor] = !f.checked[f.current][f.cursor]
		return nil
	}
	if s := msg.String(); len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
		if n := int(s[0] - '1'); n < len(q.Options) {
			if q.MultiSelect {
				f.checked[f.current][n] = !f.checked[f.current][n]
				return nil
			}
			if f.answer(q.Options[n].Label) {
				return m.finishQuestion(agent.Answer{Values: f.answers})
			}
		}
	}
	return nil
}

// handleQuestionPaste puts pasted text into the answer field when it has the
// keyboard; anywhere else in the panel a paste has nowhere to go.
func handleQuestionPaste(m *Model, msg tea.PasteMsg) tea.Cmd {
	f := m.question
	if !f.onInput() {
		return nil
	}
	var cmd tea.Cmd
	f.inputs[f.current], cmd = f.inputs[f.current].Update(msg)
	return cmd
}

// finishQuestion delivers the answer, clears the panel, and leaves the exchange
// in scrollback, where the rest of the conversation is.
func (m *Model) finishQuestion(a agent.Answer) tea.Cmd {
	if m.question == nil {
		return nil
	}
	req := m.question.req
	m.question = nil
	if h, ok := m.opts.Questioner.(*TUIQuestionHandler); ok {
		if !h.deliver(req.id, a) {
			return nil
		}
	} else {
		select {
		case req.response <- a:
		default:
		}
	}
	return tea.Println(formatQuestionsForScrollback(req.Questions, a) + "\n")
}

func handleQuestionWithdrawn(m *Model, msg questionWithdrawnMsg) (tea.Model, tea.Cmd) {
	if m.question != nil && m.question.req.id == msg.id {
		m.question = nil
	}
	return m, nil
}

func formatQuestionsForScrollback(qs []agent.Question, a agent.Answer) string {
	lines := make([]string, 0, 2*len(qs))
	for i, q := range qs {
		answer := "(dismissed)"
		if !a.Declined && i < len(a.Values) {
			answer = a.Values[i]
		}
		lines = append(lines, assistantGlyphStyle.Render("? ")+q.Text, "  "+toolGlyphPendingStyle.Render("→ ")+answer)
	}
	return strings.Join(lines, "\n")
}

// renderQuestionPanel renders the pending AskUser question set above the input.
func (m *Model) renderQuestionPanel() string {
	f := m.question
	if f == nil {
		return ""
	}
	q := f.question()
	var lines []string
	if len(f.req.Questions) > 1 {
		tabs := make([]string, len(f.req.Questions))
		for i, qq := range f.req.Questions {
			label := qq.Header
			if label == "" {
				label = fmt.Sprintf("Q%d", i+1)
			}
			if f.answers[i] != "" {
				label += " ✓"
			}
			if i == f.current {
				tabs[i] = approvalSelectedStyle.Render(label)
			} else {
				tabs[i] = approvalUnselectedStyle.Render(label)
			}
		}
		lines = append(lines, strings.Join(tabs, " "), "")
	} else if q.Header != "" {
		lines = append(lines, toolGlyphPendingStyle.Render(q.Header))
	}
	lines = append(lines, q.Text, "")
	for i, o := range q.Options {
		marker := "  "
		if i == f.cursor {
			marker = "› "
		}
		label := fmt.Sprintf("%d. %s", i+1, o.Label)
		if q.MultiSelect {
			box := "[ ] "
			if f.checked[f.current][i] {
				box = "[x] "
			}
			label = box + label
		}
		if i == f.cursor {
			label = approvalSelectedStyle.Render(label)
		}
		if o.Description != "" {
			label += "  " + toolGlyphPendingStyle.Render(o.Description)
		}
		lines = append(lines, marker+label)
	}
	marker := "  "
	if f.onInput() {
		marker = "› "
	}
	lines = append(lines, marker+"✎ "+f.inputs[f.current].View())

	var hint string
	switch {
	case q.MultiSelect:
		hint = "space/1-" + fmt.Sprint(len(q.Options)) + ": check · enter: send checked"
	case len(q.Options) > 0:
		hint = "↑↓ / 1-" + fmt.Sprint(len(q.Options)) + ": choose · enter: send"
	default:
		hint = "enter: send"
	}
	if len(f.req.Questions) > 1 {
		hint += " · tab: next question"
	}
	hint += " · esc: dismiss"
	lines = append(lines, "", toolGlyphPendingStyle.Render(hint))
	return approvalPanelStyle.Width(m.width - 4).Render("Question\n" + strings.Join(lines, "\n"))
}
