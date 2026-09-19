package work

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/icloudbb/buildmax/internal/server/turnqueue"

	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"
	"github.com/icloudbb/buildmax/internal/server/httputil"
	"github.com/icloudbb/buildmax/internal/service/conversation"
	convchannel "github.com/icloudbb/buildmax/internal/service/conversation/channel"
	"github.com/icloudbb/buildmax/internal/service/task"
	"github.com/icloudbb/buildmax/internal/service/workflow"
)

type conversationListResponse struct {
	Conversations []conversationResponse `json:"conversations"`
	Total         int                    `json:"total"`
}

type conversationResponse struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	SpaceID   string    `json:"space_id,omitempty"`
	Channel   string    `json:"channel"`
	Title     string    `json:"title,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	CreatedBy string    `json:"created_by"`
}

type createConversationRequest struct {
	Channel string `json:"channel"`
	Message string `json:"message"`
}

type createConversationResponse struct {
	ConversationID string `json:"conversation_id"`
	Reply          string `json:"reply,omitempty"`
}

type conversationMessageResponse struct {
	ID        string    `json:"id"`
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Channel   *string   `json:"channel,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type messagesResponse struct {
	Messages []conversationMessageResponse `json:"messages"`
}

type addMessageRequest struct {
	Content string `json:"content"`
}

type addMessageResponse struct {
	Reply string `json:"reply"`
}

// isVisibleConversationMessage reports whether a stored message belongs in the
// Portal transcript.
//
// Tool traffic is excluded by role. System-channel messages are internal input,
// not something a person said in the transcript.
func isVisibleConversationMessage(m coreconv.Message) bool {
	if m.Channel != nil && *m.Channel == convchannel.ChannelSystem {
		return false
	}
	return m.Role == "user" || m.Role == "assistant"
}

type sseSink struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func (s *sseSink) OnDelta(delta string) {
	writeSSE(s.w, delta)
	if s.flusher != nil {
		s.flusher.Flush()
	}
}

type runConversationTurnInput struct {
	conversationID       string
	message              string
	channel              string
	userID               string
	stream               bool
	streamStatus         int
	streamInitialPayload string
}

func (h *Handler) conversationService() *conversation.Service {
	return h.conversations
}

func newConversationService(cfg Config, tasks *task.Service, workflows *workflow.Service) *conversation.Service {
	return &conversation.Service{
		TaskService:       tasks,
		WorkflowService:   workflows,
		ConversationStore: cfg.Conversations,
		MessageStore:      cfg.Messages,
		LLMClient:         cfg.ConversationLLM,
		TitleGenerator:    cfg.TitleGenerator,
		AgentStore:        cfg.Agents,
	}
}

func (h *Handler) writeConversationServiceError(w http.ResponseWriter, r *http.Request, err error, agentID *string) bool {
	if h.writeTaskServiceError(w, r, err, agentID) {
		return true
	}
	// turnqueue.ErrQueueFull is this package's own, and its text names the queue depth,
	// so it is answered here rather than given a Kind.
	if errors.Is(err, turnqueue.ErrQueueFull) {
		httputil.WriteJSONError(w, http.StatusTooManyRequests, err.Error())
		return true
	}
	// A turn refused because this instance is stopping is retryable somewhere
	// else, which 503 is the way to say.
	if errors.Is(err, turnqueue.ErrDraining) {
		httputil.WriteJSONError(w, http.StatusServiceUnavailable, err.Error())
		return true
	}
	return false
}

// runConversationTurn runs one turn for the conversation, streaming it when asked.
//
// The turn goes through the server's turn registry, so a request that arrives while
// the conversation is busy waits for its turn instead of racing the running one
// through the same message history. A streaming request writes its headers first,
// so the client sees the stream open while it waits.
func (h *Handler) runConversationTurn(w http.ResponseWriter, r *http.Request, in runConversationTurnInput) (reply string, err error) {
	cmd := conversation.HandleTurnCmd{
		UserID:         in.userID,
		Channel:        in.channel,
		Message:        in.message,
		ConversationID: in.conversationID,
	}
	if in.stream {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Connection", "keep-alive")
		w.WriteHeader(in.streamStatus)
		flusher, _ := w.(http.Flusher)
		if in.streamInitialPayload != "" {
			writeSSE(w, in.streamInitialPayload)
			if flusher != nil {
				flusher.Flush()
			}
		}
		cmd.StreamSink = &sseSink{w: w, flusher: flusher}
		var turnErr error
		waitErr := h.cfg.Turns.RunSync(r.Context(), in.conversationID, func(fence int64) {
			cmd.Fence = fence
			_, turnErr = h.conversationService().HandleTurn(r.Context(), cmd)
		})
		if turnErr == nil {
			turnErr = waitErr
		}
		if turnErr != nil {
			errJSON, _ := json.Marshal(turnErr.Error())
			writeSSE(w, `{"error":`+string(errJSON)+`}`)
		}
		writeSSE(w, "done")
		if flusher != nil {
			flusher.Flush()
		}
		return "", nil
	}
	var result conversation.ConversationResult
	var turnErr error
	if waitErr := h.cfg.Turns.RunSync(r.Context(), in.conversationID, func(fence int64) {
		cmd.Fence = fence
		result, turnErr = h.conversationService().HandleTurn(r.Context(), cmd)
	}); waitErr != nil {
		return "", waitErr
	}
	if turnErr != nil {
		return "", turnErr
	}
	return result.Reply, nil
}

func (h *Handler) listConversationsHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Conversations, "conversations not configured")
	if !ok {
		return
	}
	limit, offset := httputil.LimitOffset(r.URL.Query(), "limit", "offset", httputil.ListPageDefault, httputil.ListPageMax)
	list, total, err := h.cfg.Conversations.ListConversationsBySpace(r.Context(), spaceID, limit, offset)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "list_conversations", "user_id", userID, "space_id", spaceID)
		return
	}
	out := make([]conversationResponse, len(list))
	for i := range list {
		out[i] = conversationResponse{
			ID:        list[i].ID,
			UserID:    list[i].UserID,
			SpaceID:   list[i].SpaceID,
			Channel:   list[i].Channel,
			Title:     list[i].Title,
			CreatedAt: list[i].CreatedAt,
			CreatedBy: list[i].CreatedBy,
		}
	}
	httputil.WriteJSON(w, http.StatusOK, conversationListResponse{Conversations: out, Total: total})
}

func (h *Handler) createConversationHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Conversations, "conversations not configured")
	if !ok {
		return
	}
	if !httputil.RequireStore(w, h.cfg.Messages, "conversation messages not configured") {
		return
	}
	var req createConversationRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	if req.Channel == "" {
		req.Channel = convchannel.ChannelPortal
	}
	// The synthetic channels — workflow, issue_agent, system — mark a
	// conversation the server made that nobody holds. A caller that could name
	// one would get a conversation the Portal renders as agent-owned and the
	// transcript hides, so the accepted set is the transport list alone.
	if !convchannel.ValidChannel(req.Channel) {
		httputil.WriteJSONError(w, http.StatusBadRequest,
			"unknown channel "+req.Channel+": use one of "+strings.Join(convchannel.ValidChannels(), ", "))
		return
	}
	conv, err := h.cfg.Conversations.CreateConversationInSpace(r.Context(), spaceID, userID, req.Channel, userID)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "create_conversation", "user_id", userID, "space_id", spaceID)
		return
	}
	if req.Message == "" || h.cfg.ConversationLLM == nil {
		httputil.WriteJSON(w, http.StatusCreated, createConversationResponse{ConversationID: conv.ID, Reply: ""})
		return
	}
	streamRequested := r.URL.Query().Get("stream") == "1"
	initialPayload := ""
	if streamRequested {
		initialPayload = `{"conversation_id":"` + conv.ID + `"}`
	}
	reply, err := h.runConversationTurn(w, r, runConversationTurnInput{
		conversationID:       conv.ID,
		message:              req.Message,
		channel:              req.Channel,
		userID:               userID,
		stream:               streamRequested,
		streamStatus:         http.StatusCreated,
		streamInitialPayload: initialPayload,
	})
	if err != nil {
		if h.writeConversationServiceError(w, r, err, nil) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "conversation_loop", "conversation_id", conv.ID)
		return
	}
	if !streamRequested {
		httputil.WriteJSON(w, http.StatusCreated, createConversationResponse{ConversationID: conv.ID, Reply: reply})
	}
}

func (h *Handler) getConversationMessagesHandler(w http.ResponseWriter, r *http.Request) {
	_, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Conversations, "conversations not configured")
	if !ok {
		return
	}
	conversationID, ok := httputil.PathValue(w, r, "conversation_id")
	if !ok {
		return
	}
	if !httputil.RequireStore(w, h.cfg.Conversations, "conversations not configured") {
		return
	}
	if !httputil.RequireStore(w, h.cfg.Messages, "conversation messages not configured") {
		return
	}
	conv, err := h.cfg.Conversations.GetConversation(r.Context(), conversationID)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "get_conversation", "conversation_id", conversationID)
		return
	}
	if conv == nil || conv.SpaceID != spaceID {
		httputil.WriteJSONError(w, http.StatusNotFound, "conversation not found")
		return
	}
	msgs, err := h.cfg.Messages.ListMessages(r.Context(), conversationID)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "list_messages", "conversation_id", conversationID)
		return
	}
	out := make([]conversationMessageResponse, 0, len(msgs))
	for i := range msgs {
		if !isVisibleConversationMessage(msgs[i]) {
			continue
		}
		out = append(out, conversationMessageResponse{
			ID:        msgs[i].ID,
			Role:      msgs[i].Role,
			Content:   msgs[i].Content,
			Channel:   msgs[i].Channel,
			CreatedAt: msgs[i].CreatedAt,
		})
	}
	httputil.WriteJSON(w, http.StatusOK, messagesResponse{Messages: out})
}

func (h *Handler) addConversationMessageHandler(w http.ResponseWriter, r *http.Request) {
	userID, spaceID, ok := h.guard().UserAndPathSpace(w, r, h.cfg.Conversations, "conversations not configured")
	if !ok {
		return
	}
	conversationID, ok := httputil.PathValue(w, r, "conversation_id")
	if !ok {
		return
	}
	conv, ok := h.getConversationForSpace(w, r, spaceID, conversationID)
	if !ok {
		return
	}
	if !httputil.RequireStore(w, h.cfg.Messages, "conversation messages not configured") {
		return
	}
	if h.cfg.ConversationLLM == nil {
		httputil.WriteJSONError(w, http.StatusServiceUnavailable, "conversation LLM not configured")
		return
	}
	var req addMessageRequest
	if !httputil.DecodeJSONBody(w, r, &req) {
		return
	}
	if req.Content == "" {
		httputil.WriteJSONError(w, http.StatusBadRequest, "content required")
		return
	}
	streamRequested := r.URL.Query().Get("stream") == "1"
	reply, err := h.runConversationTurn(w, r, runConversationTurnInput{
		conversationID: conversationID,
		message:        req.Content,
		channel:        conv.Channel,
		userID:         userID,
		stream:         streamRequested,
		streamStatus:   http.StatusOK,
	})
	if err != nil {
		if h.writeConversationServiceError(w, r, err, nil) {
			return
		}
		httputil.WriteInternalError(w, err, "handler error", "handler", "conversation_loop", "conversation_id", conversationID)
		return
	}
	if !streamRequested {
		httputil.WriteJSON(w, http.StatusOK, addMessageResponse{Reply: reply})
	}
}

func (h *Handler) getConversationForSpace(w http.ResponseWriter, r *http.Request, spaceID, conversationID string) (*coreconv.Conversation, bool) {
	if !httputil.RequireStore(w, h.cfg.Conversations, "conversations not configured") {
		return nil, false
	}
	conv, err := h.cfg.Conversations.GetConversation(r.Context(), conversationID)
	if err != nil {
		httputil.WriteInternalError(w, err, "handler error", "handler", "get_conversation", "conversation_id", conversationID)
		return nil, false
	}
	if conv == nil || conv.SpaceID != spaceID {
		httputil.WriteJSONError(w, http.StatusNotFound, "conversation not found")
		return nil, false
	}
	return conv, true
}
