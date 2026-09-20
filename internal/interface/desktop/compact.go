package desktop

import (
	"context"
	"errors"
	"fmt"

	"github.com/icloudbb/buildmax/internal/core/session"
)

// CompactResultPayload is what one compaction did, for the notice the frontend
// shows afterwards. Summarized == 0 with no error means the pass found nothing
// worth replacing, and Reason says why.
type CompactResultPayload struct {
	Summarized    int    `json:"summarized"`
	Kept          int    `json:"kept"`
	Reason        string `json:"reason,omitempty"`
	BeforeTokens  int    `json:"before_tokens"`
	AfterTokens   int    `json:"after_tokens"`
	ContextWindow int    `json:"context_window"`
}

// CompactProjectSession summarizes a session's history and continues from the
// summary — the /compact command. It is a model call that rewrites the
// model-visible history, so it takes the session's writer lock the way a run
// does; a session a run holds is reported busy rather than compacted under it.
//
// Desktop holds no session between calls, so this opens the session, compacts,
// and closes it. The frontend reloads the thread and run status afterwards.
func (a *App) CompactProjectSession(projectID, sessionID string) (CompactResultPayload, error) {
	if sessionID == "" {
		return CompactResultPayload{}, fmt.Errorf("no session is open")
	}
	ag, err := a.resolveSessionApp(projectID, sessionID)
	if err != nil {
		return CompactResultPayload{}, err
	}
	sess, err := sessionManager().Open(sessionID, ag.DefaultModelName())
	if err != nil {
		switch {
		case errors.Is(err, session.ErrLocked):
			return CompactResultPayload{}, fmt.Errorf("this session is busy; stop the run before you compact it")
		case errors.Is(err, session.ErrSessionNotFound):
			return CompactResultPayload{}, fmt.Errorf("session not found: %s", sessionID)
		}
		return CompactResultPayload{}, fmt.Errorf("open session: %w", err)
	}
	defer closeQuietly(sess)

	res, err := ag.CompactSession(context.Background(), sess)
	if err != nil {
		return CompactResultPayload{}, err
	}
	return CompactResultPayload{
		Summarized:    res.Summarized,
		Kept:          res.Kept,
		Reason:        res.Reason,
		BeforeTokens:  res.BeforeTokens,
		AfterTokens:   res.Status.ContextTokens,
		ContextWindow: res.Status.ContextWindow,
	}, nil
}
