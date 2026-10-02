package tool

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/icloudbb/buildmax/internal/core/agent"
	"github.com/icloudbb/buildmax/internal/core/llm"
)

// AskUser puts up to four questions to the user. Interactively (TUI and
// Desktop project chats) it waits for the answers, which come back as the tool
// result. Deferred (worker TaskRuns) nobody is waiting: the questions are handed
// on, the turn ends, and the user answers later in their own words. The
// questioner reaches it through the context because the registry is shared
// across runs. See docs/design/agent-user-questions.md.
type AskUser struct {
	deferred bool
}

// NewAskUser creates the interactive AskUser tool.
func NewAskUser() *AskUser { return &AskUser{} }

// NewDeferredAskUser creates the AskUser tool for unattended runs, whose
// description tells the model the turn ends when it asks.
func NewDeferredAskUser() *AskUser { return &AskUser{deferred: true} }

func (t *AskUser) Name() string { return ToolNameAskUser }

// Access implements llm.AccessDeclarer. A question changes nothing, but it
// holds the run until a person answers, and a surface shows one question at a
// time, so it must not share a parallel batch.
func (t *AskUser) Access(_ map[string]any) llm.Access { return llm.AccessWrite }

// DefaultAction implements llm.PolicyProvider: asking permission to ask a
// question would be a prompt about a prompt.
func (t *AskUser) DefaultAction() llm.ToolAction { return llm.ToolActionAllow }

func (t *AskUser) Description() string {
	if t.deferred {
		return "Ask the user questions you are blocked on. It is the only way to put a question to the " +
			"user in this run: a question written in your reply is not delivered as one, and the run just " +
			"ends. Nobody is watching this run: calling it ends " +
			"your turn, the questions go to the user, and they answer later in their own words as the next " +
			"message. Use it only when you cannot make reasonable progress without a decision only the user " +
			"can make or a fact you cannot find; otherwise decide, state your assumption, and keep working. " +
			"Before asking, finish what you can and say what you have done. Ask everything you need in one " +
			"call (up to four questions), each with options when the likely answers are known, recommended " +
			"option first. Never add an \"Other\" option, and never use it to ask permission to run a tool."
	}
	return "Ask the user questions and wait for the answers. Use it when you need a decision only " +
		"the user can make — an ambiguous requirement, a choice between approaches with different " +
		"trade-offs, a preference — or a fact you cannot find yourself. When you need several decisions, " +
		"ask them together in one call (up to four questions) rather than one after another. Give each " +
		"question options when the likely answers are known, recommended option first; set multi_select " +
		"when more than one may apply; omit options for an open question. The user can always answer in " +
		"their own words, so never add an \"Other\" option. Do not use it to ask permission to run a tool (tool approval already " +
		"does that) or to confirm something you can check yourself."
}

func (t *AskUser) Parameters() any {
	option := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"label": map[string]any{
				"type":        "string",
				"description": fmt.Sprintf("The answer as the user would say it, in a few words. At most %d characters.", agent.MaxOptionLabelChars),
			},
			"description": map[string]any{
				"type":        "string",
				"description": fmt.Sprintf("Optional one-line consequence of choosing it. At most %d characters.", agent.MaxOptionDetailChars),
			},
		},
		"required": []string{"label"},
	}
	question := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"question": map[string]any{
				"type":        "string",
				"description": fmt.Sprintf("The question, complete and specific enough to answer without scrolling back. At most %d characters.", agent.MaxQuestionChars),
			},
			"header": map[string]any{
				"type":        "string",
				"description": fmt.Sprintf("A short label for the question, such as \"Database\" or \"Auth\", shown as its tab. At most %d characters.", agent.MaxQuestionHeaderChars),
			},
			"options": map[string]any{
				"type":        "array",
				"description": fmt.Sprintf("Up to %d answers the user can pick instead of typing. Omit for an open-ended question.", agent.MaxQuestionOptions),
				"maxItems":    agent.MaxQuestionOptions,
				"items":       option,
			},
			"multi_select": map[string]any{
				"type":        "boolean",
				"description": "Let the user pick more than one option. Needs at least two options.",
			},
		},
		"required": []string{"question"},
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"questions": map[string]any{
				"type":        "array",
				"description": fmt.Sprintf("The questions to ask together, 1 to %d. They are shown one at a time and answered as a set.", agent.MaxQuestions),
				"minItems":    1,
				"maxItems":    agent.MaxQuestions,
				"items":       question,
			},
		},
		"required": []string{"questions"},
	}
}

func (t *AskUser) Execute(ctx context.Context, args map[string]any) (string, error) {
	qs, err := parseQuestions(args)
	if err != nil {
		return "", err
	}
	if err := agent.ValidateQuestions(qs); err != nil {
		return "", err
	}
	questioner, ok := agent.QuestionerFromCtx(ctx)
	if !ok {
		return "Nobody is available to answer questions in this run, so nothing was asked. " +
			"Decide on your best judgment, say in your reply which assumptions you made, and continue.", nil
	}
	answer, err := questioner.AskUser(ctx, qs)
	if err != nil {
		return "", fmt.Errorf("questions were not answered: %w", err)
	}
	if answer.Deferred {
		return "The questions were sent to the user, who will answer in their own words in the next " +
			"message. Your turn ends now: do not guess the answers or continue the work that depends on them.", nil
	}
	if answer.Declined {
		return "The user dismissed the questions without answering. Do not ask them again. Proceed on " +
			"your best judgment and state the assumptions, or stop and explain what you need to continue.", nil
	}
	if len(answer.Values) != len(qs) {
		return "", fmt.Errorf("got %d answers for %d questions", len(answer.Values), len(qs))
	}
	var b strings.Builder
	b.WriteString("The user answered:")
	for i, q := range qs {
		fmt.Fprintf(&b, "\nQ: %s\nA: %s", q.Text, answer.Values[i])
	}
	return b.String(), nil
}

func parseQuestions(args map[string]any) ([]agent.Question, error) {
	raw, ok := args["questions"].([]any)
	if !ok {
		return nil, errors.New("questions must be an array of {question, header, options, multi_select} objects")
	}
	qs := make([]agent.Question, 0, len(raw))
	for i, item := range raw {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("questions[%d] must be an object with a question", i)
		}
		text, _ := obj["question"].(string)
		header, _ := obj["header"].(string)
		multi, _ := obj["multi_select"].(bool)
		q := agent.Question{Header: strings.TrimSpace(header), Text: strings.TrimSpace(text), MultiSelect: multi}
		if rawOpts, present := obj["options"]; present && rawOpts != nil {
			opts, ok := rawOpts.([]any)
			if !ok {
				return nil, fmt.Errorf("questions[%d].options must be an array of {label, description} objects", i)
			}
			for j, o := range opts {
				opt, ok := o.(map[string]any)
				if !ok {
					return nil, fmt.Errorf("questions[%d].options[%d] must be an object with a label", i, j)
				}
				label, _ := opt["label"].(string)
				detail, _ := opt["description"].(string)
				q.Options = append(q.Options, agent.QuestionOption{
					Label:       strings.TrimSpace(label),
					Description: strings.TrimSpace(detail),
				})
			}
		}
		qs = append(qs, q)
	}
	return qs, nil
}
