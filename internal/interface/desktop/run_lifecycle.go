package desktop

import (
	"context"

	"github.com/icloudbb/buildmax/internal/agentapp"
	"github.com/icloudbb/buildmax/internal/core/agent"
)

// desktopRun adapts one scheduled run's lifecycle to the desktop's Wails events.
// A run's turn-by-turn progress reaches the frontend through these methods; the
// RunScheduler owns the serialization, the queue, and the session, so nothing
// here repeats that. Foreground prompts and job deliveries share this adapter
// and differ only in touchLastUsed and onStart.
type desktopRun struct {
	app       *App
	ctx       context.Context
	projectID string
	// key is the scheduler key this run was reserved under (runKey of projectID
	// and the submit-time sessionID). It is fixed for the run's life so queue
	// lookups find the right queue even after a new chat earns a real id.
	key string
	// sessionID is the run's live session id, used to tag every emitted event so
	// the frontend routes it to the right chat tab. It starts as the submit-time
	// id ("" for a new chat) and is set to the real id in OnStart.
	sessionID string
	// wasNew records that this run began as a new chat (empty submit-time id), so
	// OnStart announces the real id it created for the frontend to adopt.
	wasNew  bool
	handler agent.ApprovalHandler
	// touchLastUsed advances the project's recency stamp after each good turn.
	// A user prompt does; a background delivery does not, because the user did
	// not reach for the project.
	touchLastUsed bool
	// onStart announces the run before its first output, or is nil for a
	// foreground run, which has nothing to announce.
	onStart func(sess *agentapp.SessionContext)
}

func (r *desktopRun) RunOpts() agentapp.RunPromptOpts {
	return agentapp.RunPromptOpts{
		Stream:    &desktopStreamSink{ctx: r.ctx, emit: r.app.emit, session: func() string { return r.sessionID }},
		Approval:  r.handler,
		EventSink: desktopEventSink(r.app.emit, r.ctx, func() string { return r.sessionID }, func() []string { return r.app.scheduler.Queued(r.key) }),
		Digest:    true,
	}
}

func (r *desktopRun) OnStart(sess *agentapp.SessionContext) {
	// A new chat submits with an empty id; adopt the real one so every event this
	// run emits is tagged with the session the frontend can route on.
	r.sessionID = sess.ID()
	// Announce the created id to the pending new-chat tab before any stream event,
	// so it adopts the right session even while others run concurrently.
	if r.wasNew {
		r.app.emit(r.ctx, eventSessionAdopted, &SessionAdoptedPayload{SessionID: sess.ID()})
	}
	if r.onStart != nil {
		r.onStart(sess)
	}
}

func (r *desktopRun) TurnDone(out agentapp.RunResult) {
	r.app.emitTurnDigest(r.ctx, r.sessionID, out)
	if r.touchLastUsed {
		touchProjectLastUsed(r.projectID)
	}
}

func (r *desktopRun) TurnError(err error) {
	r.app.emit(r.ctx, eventStreamError, &StreamErrorPayload{SessionID: r.sessionID, Message: err.Error()})
}

func (r *desktopRun) Dequeued(next string, snapshot []string) {
	r.app.emit(r.ctx, eventMessageDequeued, &MessageDequeuedPayload{SessionID: r.sessionID, Prompt: next, Queued: snapshot})
}

func (r *desktopRun) Done(out agentapp.RunResult, err error) {
	if err != nil {
		r.app.emit(r.ctx, eventStreamError, &StreamErrorPayload{SessionID: r.sessionID, Message: err.Error()})
		return
	}
	r.app.emit(r.ctx, eventStreamDone, replyPayload(out))
}
