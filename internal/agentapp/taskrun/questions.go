package taskrun

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/icloudbb/buildmax/internal/core/agent"
)

// deferredQuestioner answers AskUser in an unattended run. Nobody is waiting,
// so it records the questions and defers: the loop ends the turn, the run
// reports the questions, and the user answers later in their own words by
// continuing the Task. See docs/design/agent-user-questions.md.
type deferredQuestioner struct {
	mu    sync.Mutex
	asked []agent.Question
}

func (q *deferredQuestioner) AskUser(_ context.Context, qs []agent.Question) (agent.Answer, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.asked = append(q.asked, qs...)
	return agent.Answer{Deferred: true}, nil
}

func (q *deferredQuestioner) questions() []agent.Question {
	if q == nil {
		return nil
	}
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]agent.Question(nil), q.asked...)
}

// questionsJSON is the question set as the JSON text the run reports, or nil
// when the run asked nothing.
func questionsJSON(qs []agent.Question) *string {
	if len(qs) == 0 {
		return nil
	}
	b, err := json.Marshal(qs)
	if err != nil {
		return nil
	}
	s := string(b)
	return &s
}

// withQuestions appends the questions to the run's reply. The output is what
// every reader of a run already shows — the Task thread, the Issue report, a
// chat channel's outcome message — so the questions reach the user wherever
// they look, with nothing new to render.
func withQuestions(reply string, qs []agent.Question) string {
	if len(qs) == 0 {
		return reply
	}
	var b strings.Builder
	if reply = strings.TrimSpace(reply); reply != "" {
		b.WriteString(reply)
		b.WriteString("\n\n")
	}
	b.WriteString("**Waiting for your answer**\n")
	for i, q := range qs {
		fmt.Fprintf(&b, "\n%d. %s", i+1, q.Text)
		if len(q.Options) > 0 {
			labels := make([]string, len(q.Options))
			for j, o := range q.Options {
				labels[j] = o.Label
			}
			sep := " or "
			if q.MultiSelect {
				sep = ", "
			}
			fmt.Fprintf(&b, " (%s)", strings.Join(labels, sep))
		}
	}
	b.WriteString("\n\nReply to this task in your own words to continue.")
	return b.String()
}
