package desktop

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/icloudbb/buildmax/internal/core/agent"
)

const eventQuestionRequest = "desktop/question-request"

// QuestionRequestPayload is emitted to the frontend when the Agent asks the
// user a question set. It is routed like an approval: SessionID picks the chat
// tab, and QuestionID is what that tab answers with.
type QuestionRequestPayload struct {
	QuestionID string           `json:"question_id"`
	ProjectID  string           `json:"project_id"`
	SessionID  string           `json:"session_id"`
	Questions  []agent.Question `json:"questions"`
}

// runQuestioner is one run's agent.UserQuestioner, bound to the run for the
// same reason runApprover is: its question carries the run's own session id.
type runQuestioner struct {
	app *App
	run *desktopRun
}

// AskUser emits a question-request event and blocks until RespondQuestion
// answers it or the run is cancelled.
func (h *runQuestioner) AskUser(ctx context.Context, qs []agent.Question) (agent.Answer, error) {
	h.app.mu.Lock()
	uiCtx := h.app.ctx
	h.app.mu.Unlock()
	if uiCtx == nil {
		return agent.Answer{}, errors.New("the desktop window is not ready to ask")
	}

	id, answer := h.app.questions.open()
	h.app.emit(uiCtx, eventQuestionRequest, &QuestionRequestPayload{
		QuestionID: id,
		ProjectID:  h.run.projectID,
		SessionID:  h.run.sessionID,
		Questions:  qs,
	})

	select {
	case a := <-answer:
		return a, nil
	case <-ctx.Done():
		// Withdrawn so the run's cleanup runs; the frontend drops the panel when
		// the run's stream ends.
		h.app.questions.withdraw(id)
		return agent.Answer{}, ctx.Err()
	}
}

// RespondQuestion is called by the frontend when the user answers an AskUser
// question set. questionID is the question_id of the desktop/question-request
// being answered; an id that is unknown, already answered, or withdrawn is an
// error and reaches no run. answers holds one answer per question, in order;
// declined means the user dismissed the set, and then answers is ignored. An
// empty answer that is not a dismissal is refused rather than sent: the Agent
// would read it as the user saying nothing on purpose.
func (a *App) RespondQuestion(questionID string, answers []string, declined bool) error {
	if declined {
		return a.questions.resolve(questionID, agent.Answer{Declined: true})
	}
	if len(answers) == 0 {
		return errors.New("no answers; answer every question or dismiss them")
	}
	values := make([]string, len(answers))
	for i, v := range answers {
		values[i] = strings.TrimSpace(v)
		if values[i] == "" {
			return fmt.Errorf("answer %d is empty; answer every question or dismiss them", i+1)
		}
	}
	return a.questions.resolve(questionID, agent.Answer{Values: values})
}
