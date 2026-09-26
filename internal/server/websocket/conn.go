package websocket

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"
	"github.com/icloudbb/buildmax/internal/server/turnqueue"
	"github.com/icloudbb/buildmax/internal/service/conversation"
	convchannel "github.com/icloudbb/buildmax/internal/service/conversation/channel"

	gws "github.com/gorilla/websocket"
)

// Identity belongs in an attr, not in every message string.
func componentLog() *slog.Logger { return slog.With("component", "websocket") }

const (
	wsWriteChSize  = 256
	wsPingInterval = 30 * time.Second
	wsReadDeadline = 60 * time.Second
	wsWriteWait    = 10 * time.Second
)

// Conn manages a single WebSocket connection for one authenticated user.
type Conn struct {
	conn    *gws.Conn
	deps    ConnDeps
	userID  string
	spaceID string

	writeCh chan []byte
	closed  chan struct{}

	// queuedMu guards queuedJobs, the turns this connection has waiting in the
	// server's turn registry. They are dropped when the connection goes away:
	// nothing is left to stream them to.
	queuedMu   sync.Mutex
	queuedJobs []*turnqueue.Job

	cancel context.CancelFunc
}

// trackQueuedJob remembers a turn this connection queued, pruning the ones that
// have since run.
func (wc *Conn) trackQueuedJob(job *turnqueue.Job) {
	wc.queuedMu.Lock()
	defer wc.queuedMu.Unlock()
	live := wc.queuedJobs[:0]
	for _, j := range wc.queuedJobs {
		select {
		case <-j.Done:
		default:
			live = append(live, j)
		}
	}
	wc.queuedJobs = append(live, job)
}

// dropQueuedJobs marks this connection's waiting turns as dropped. A turn already
// running is unaffected — it has a reply to finish writing to the store.
func (wc *Conn) dropQueuedJobs() {
	wc.queuedMu.Lock()
	jobs := wc.queuedJobs
	wc.queuedJobs = nil
	wc.queuedMu.Unlock()
	for _, j := range jobs {
		j.Dropped.Store(true)
	}
}

// wsSink implements model.StreamSink by sending conversation.message.delta events.
type wsSink struct {
	c              *Conn
	conversationID string
}

func (s *wsSink) OnDelta(delta string) {
	s.c.sendEvent(TypeMessageDelta, MessageDelta{
		ConversationID: s.conversationID,
		Delta:          delta,
	})
}

// ConnDeps is everything a live connection needs. It is a struct rather than a
// Handler because this package should not be able to reach a store it has no
// use for: a socket creates and reads conversations and runs turns, and that
// is the whole list.
type ConnDeps struct {
	Conversations coreconv.Store
	Turns         *turnqueue.Registry
	// Turner runs one conversation turn. An interface so this package does not
	// depend on the service that assembles agents, models, and tools.
	Turner   Turner
	Registry *ConnRegistry
	// CORSOrigin is checked on the upgrade. Empty or "*" accepts any origin,
	// which is what a deployment serving Portal from the same host has.
	CORSOrigin string
}

// Turner runs one conversation turn to completion, streaming as it goes.
type Turner interface {
	HandleTurn(ctx context.Context, cmd conversation.HandleTurnCmd) (conversation.ConversationResult, error)
}

// Serve upgrades an authenticated request and runs the connection until it
// closes. The caller has already decided who this is and which space they are
// in; this package does not repeat that.
func Serve(w http.ResponseWriter, r *http.Request, userID, spaceID string, deps ConnDeps) {
	upgrader := gws.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			if deps.CORSOrigin == "" || deps.CORSOrigin == "*" {
				return true
			}
			origin := r.Header.Get("Origin")
			return origin == "" || origin == deps.CORSOrigin
		},
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		componentLog().Warn("upgrade failed", "err", err, "user_id", userID)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	wc := &Conn{
		conn:    conn,
		deps:    deps,
		userID:  userID,
		spaceID: spaceID,
		writeCh: make(chan []byte, wsWriteChSize),
		closed:  make(chan struct{}),
		cancel:  cancel,
	}
	deps.Registry.Register(userID, wc)

	componentLog().Info("connected", "user_id", userID, "remote", r.RemoteAddr)
	go wc.writeLoop(ctx)
	wc.readLoop(ctx)
}

func (wc *Conn) readLoop(ctx context.Context) {
	defer func() {
		componentLog().Info("disconnected", "user_id", wc.userID)
		wc.cleanup()
		_ = wc.conn.Close()
	}()

	// Deadline and close-frame calls only fail on a connection that is already
	// broken, which the next read or write reports anyway.
	_ = wc.conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
	wc.conn.SetPongHandler(func(string) error {
		return wc.conn.SetReadDeadline(time.Now().Add(wsReadDeadline))
	})

	for {
		_, data, err := wc.conn.ReadMessage()
		if err != nil {
			if gws.IsUnexpectedCloseError(err, gws.CloseGoingAway, gws.CloseNormalClosure) {
				componentLog().Info("read error", "err", err, "user_id", wc.userID)
			}
			return
		}
		_ = wc.conn.SetReadDeadline(time.Now().Add(wsReadDeadline))

		env, err := Decode(data)
		if err != nil {
			componentLog().Warn("recv invalid message", "user_id", wc.userID, "err", err)
			wc.sendEvent(TypeSystemError, SystemError{Error: "invalid message format"})
			continue
		}
		componentLog().Debug("recv", "user_id", wc.userID, "type", env.Type)
		wc.handleClientEvent(ctx, env)
	}
}

func (wc *Conn) writeLoop(ctx context.Context) {
	ticker := time.NewTicker(wsPingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			_ = wc.conn.WriteMessage(gws.CloseMessage,
				gws.FormatCloseMessage(gws.CloseNormalClosure, ""))
			return
		case msg, ok := <-wc.writeCh:
			if !ok {
				_ = wc.conn.WriteMessage(gws.CloseMessage,
					gws.FormatCloseMessage(gws.CloseNormalClosure, ""))
				return
			}
			_ = wc.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := wc.conn.WriteMessage(gws.TextMessage, msg); err != nil {
				componentLog().Info("write error", "err", err, "user_id", wc.userID)
				return
			}
		case <-ticker.C:
			_ = wc.conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
			if err := wc.conn.WriteMessage(gws.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (wc *Conn) sendEvent(eventType string, payload any) {
	data, err := Encode(eventType, payload)
	if err != nil {
		componentLog().Warn("encode error", "type", eventType, "err", err)
		return
	}
	componentLog().Debug("send", "user_id", wc.userID, "type", eventType)
	wc.sendRaw(data)
}

// sendRaw writes an already-encoded frame to this connection. It is what a
// broadcast delivered from the coordination bus uses, where the event was encoded
// once on the replica that raised it.
func (wc *Conn) sendRaw(data []byte) {
	select {
	case <-wc.closed:
		return
	default:
	}
	select {
	case wc.writeCh <- data:
	case <-wc.closed:
	default:
		componentLog().Warn("write channel full, dropping event", "user_id", wc.userID)
	}
}

func (wc *Conn) handleClientEvent(ctx context.Context, env Envelope) {
	switch env.Type {
	case TypeConversationCreate:
		p, err := DecodePayload[ConversationCreate](env)
		if err != nil {
			wc.sendEvent(TypeConversationError, ConversationError{Error: "invalid payload"})
			return
		}
		wc.handleConversationCreate(ctx, p)
	case TypeConversationMessage:
		p, err := DecodePayload[ConversationMessage](env)
		if err != nil {
			wc.sendEvent(TypeConversationError, ConversationError{Error: "invalid payload"})
			return
		}
		wc.handleConversationMessage(ctx, p)
	default:
		wc.sendEvent(TypeSystemError, SystemError{Error: "unknown event type: " + env.Type})
	}
}

func (wc *Conn) handleConversationCreate(ctx context.Context, p ConversationCreate) {
	if p.Message == "" {
		wc.sendEvent(TypeConversationError, ConversationError{Error: "message required"})
		return
	}
	if wc.deps.Conversations == nil {
		wc.sendEvent(TypeConversationError, ConversationError{Error: "conversations not configured"})
		return
	}
	channel := p.Channel
	if channel == "" {
		channel = convchannel.ChannelPortal
	}
	// The same accepted set as the HTTP create route: a chat platform's channel
	// (telegram) is the channel gateway's to assign.
	if !convchannel.ValidChannel(channel) {
		wc.sendEvent(TypeConversationError, ConversationError{Error: "unknown channel " + channel})
		return
	}
	conv, err := wc.deps.Conversations.CreateConversationInSpace(ctx, wc.spaceID, wc.userID, channel, wc.userID)
	if err != nil {
		componentLog().Error("create conversation", "err", err, "user_id", wc.userID)
		wc.sendEvent(TypeConversationError, ConversationError{Error: "failed to create conversation"})
		return
	}
	componentLog().Info("conversation created", "user_id", wc.userID, "conversation_id", conv.ID)
	wc.sendEvent(TypeConversationCreated, ConversationCreated{ConversationID: conv.ID})
	wc.runConversationTurn(ctx, conv.ID, p.Message, channel)
}

func (wc *Conn) handleConversationMessage(ctx context.Context, p ConversationMessage) {
	if p.ConversationID == "" || p.Content == "" {
		wc.sendEvent(TypeConversationError, ConversationError{
			ConversationID: p.ConversationID,
			Error:          "conversation_id and content required",
		})
		return
	}
	if wc.deps.Conversations == nil {
		wc.sendEvent(TypeConversationError, ConversationError{
			ConversationID: p.ConversationID,
			Error:          "conversations not configured",
		})
		return
	}
	conv, err := wc.deps.Conversations.GetConversation(ctx, p.ConversationID)
	if err != nil {
		componentLog().Error("get conversation", "err", err, "conversation_id", p.ConversationID)
		wc.sendEvent(TypeConversationError, ConversationError{
			ConversationID: p.ConversationID,
			Error:          "failed to load conversation",
		})
		return
	}
	if conv == nil || conv.SpaceID != wc.spaceID {
		wc.sendEvent(TypeConversationError, ConversationError{
			ConversationID: p.ConversationID,
			Error:          "conversation not found",
		})
		return
	}
	componentLog().Info("conversation message", "user_id", wc.userID, "conversation_id", p.ConversationID)
	wc.runConversationTurn(ctx, p.ConversationID, p.Content, conv.Channel)
}

// runConversationTurn submits a turn for the conversation. A message that arrives
// while a turn is running is queued and runs as its own turn once that one
// finishes, rather than being rejected as it used to be.
func (wc *Conn) runConversationTurn(ctx context.Context, conversationID, message, channel string) {
	job := turnqueue.NewJob(func(fence int64) {
		wc.executeConversationTurn(ctx, conversationID, message, channel, fence)
	})
	job.OnDequeue = func() {
		wc.sendEvent(TypeMessageDequeued, MessageDequeued{
			ConversationID: conversationID,
			Content:        message,
		})
	}
	pos, err := wc.deps.Turns.Submit(conversationID, job)
	if err != nil {
		componentLog().Info("turn rejected: queue full", "user_id", wc.userID, "conversation_id", conversationID)
		wc.sendEvent(TypeConversationError, ConversationError{
			ConversationID: conversationID,
			Error:          err.Error(),
			Code:           ErrorCodeQueueFull,
		})
		return
	}
	if pos > 0 {
		wc.trackQueuedJob(job)
		componentLog().Info("turn queued", "user_id", wc.userID, "conversation_id", conversationID, "position", pos)
		wc.sendEvent(TypeMessageQueued, MessageQueued{
			ConversationID: conversationID,
			Content:        message,
			Position:       pos,
		})
	}
}

func (wc *Conn) executeConversationTurn(ctx context.Context, conversationID, message, channel string, fence int64) {
	componentLog().Info("turn start", "user_id", wc.userID, "conversation_id", conversationID, "channel", channel)
	sink := &wsSink{c: wc, conversationID: conversationID}
	svc := wc.deps.Turner
	_, err := svc.HandleTurn(ctx, conversation.HandleTurnCmd{
		UserID:         wc.userID,
		Channel:        channel,
		Message:        message,
		ConversationID: conversationID,
		StreamSink:     sink,
		Fence:          fence,
	})
	if err != nil {
		componentLog().Error("turn error", "user_id", wc.userID, "conversation_id", conversationID, "err", err)
		wc.sendEvent(TypeConversationError, ConversationError{
			ConversationID: conversationID,
			Error:          err.Error(),
		})
	}
	remaining := wc.deps.Turns.Waiting(conversationID)
	componentLog().Info("turn done", "user_id", wc.userID, "conversation_id", conversationID, "queued_remaining", remaining)
	wc.sendEvent(TypeMessageCompleted, MessageCompleted{
		ConversationID:  conversationID,
		QueuedRemaining: remaining,
	})
}

func (wc *Conn) cleanup() {
	wc.deps.Registry.Unregister(wc.userID, wc)
	wc.dropQueuedJobs()
	wc.cancel()
	select {
	case <-wc.closed:
	default:
		close(wc.closed)
	}
	close(wc.writeCh)
}
