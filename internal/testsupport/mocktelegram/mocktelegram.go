// Package mocktelegram serves the subset of the Telegram Bot API that
// internal/infra/imchannel/telegram speaks, for any number of bots at once, so
// an end-to-end suite can drive the chat gateway and Space Assistant bots
// without Telegram, a bot account, or outbound network.
//
// A bot exists as soon as a token names it: the token is <digits>:<secret>, the
// digits are the bot's id, and its username is smoke<digits>_bot. The secret is
// not checked, except that the text "invalid" (or a token with no colon) is
// refused the way Telegram refuses a wrong token. A suite plays the people on
// the other side over the control routes: it enqueues a private-chat message
// for a bot and reads back what the bots sent.
//
// See "Chat Apps" in docs/deploy/local-kind.md.
package mocktelegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Control routes, matched by suffix so an ingress may serve them under a
// prefix, as mockllm's are.
const (
	// ControlUpdatesPath enqueues a private-chat message for a bot. POST.
	ControlUpdatesPath = "/control/updates"
	// ControlMessagesPath lists what bots sent, oldest first. GET.
	ControlMessagesPath = "/control/messages"
	// ControlResetPath forgets every queued update and sent message. POST.
	ControlResetPath = "/control/reset"
)

// maxPoll caps a getUpdates long poll, so a poller's request never outlives an
// ingress or client timeout and a test's wait stays short.
const maxPoll = 10 * time.Second

// maxSent bounds the sent-message record of a mock left running for days.
const maxSent = 10000

// Inbound is one message a person sends a bot.
type Inbound struct {
	BotID string `json:"bot_id"`
	// FromID is the sender's Telegram user id.
	FromID int64 `json:"from_id"`
	// ChatID defaults to FromID, which is a private chat's id.
	ChatID   int64  `json:"chat_id,omitempty"`
	Text     string `json:"text"`
	Username string `json:"username,omitempty"`
}

// Sent is one message a bot sent.
type Sent struct {
	BotID     string `json:"bot_id"`
	ChatID    string `json:"chat_id"`
	Text      string `json:"text"`
	MessageID int64  `json:"message_id"`
}

type update struct {
	UpdateID int64   `json:"update_id"`
	Message  message `json:"message"`
}

type message struct {
	MessageID int64  `json:"message_id"`
	Date      int64  `json:"date"`
	From      user   `json:"from"`
	Chat      chat   `json:"chat"`
	Text      string `json:"text"`
}

type user struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	Username  string `json:"username,omitempty"`
	FirstName string `json:"first_name"`
}

type chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

// Handler is the mock Bot API and its control routes.
type Handler struct {
	maxPoll time.Duration

	mu sync.Mutex
	// pending is each bot's updates not yet confirmed by a poll past them.
	pending map[string][]update
	sent    []Sent
	// lastUpdate and lastMessage only grow, across Reset too: a poller keeps
	// its offset in memory, and an id below it would never be delivered.
	lastUpdate  int64
	lastMessage int64
	// arrived is closed and replaced whenever an update is queued, waking
	// every long poll at once.
	arrived chan struct{}
}

// New returns an empty mock. Update ids start from the clock rather than
// from one, so a poller that outlives a restart of the mock still sees the
// ids issued after it as newer than its offset.
func New() *Handler {
	return &Handler{
		maxPoll:    maxPoll,
		pending:    map[string][]update{},
		lastUpdate: time.Now().UnixMilli(),
		arrived:    make(chan struct{}),
	}
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasSuffix(r.URL.Path, ControlUpdatesPath):
		h.serveControlUpdates(w, r)
	case strings.HasSuffix(r.URL.Path, ControlMessagesPath):
		h.serveControlMessages(w, r)
	case strings.HasSuffix(r.URL.Path, ControlResetPath):
		if r.Method != http.MethodPost {
			http.Error(w, "mocktelegram: only POST", http.StatusMethodNotAllowed)
			return
		}
		h.Reset()
		writeJSON(w, http.StatusOK, map[string]bool{"reset": true})
	default:
		h.serveBotAPI(w, r)
	}
}

// Enqueue queues in for its bot and returns the update id it was given.
func (h *Handler) Enqueue(in Inbound) (int64, error) {
	if !validBotID(in.BotID) {
		return 0, fmt.Errorf("bot_id %q is not a bot id (digits)", in.BotID)
	}
	if in.FromID <= 0 {
		return 0, fmt.Errorf("from_id must be a positive user id")
	}
	chatID := in.ChatID
	if chatID == 0 {
		chatID = in.FromID
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastUpdate++
	h.lastMessage++
	h.pending[in.BotID] = append(h.pending[in.BotID], update{
		UpdateID: h.lastUpdate,
		Message: message{
			MessageID: h.lastMessage,
			Date:      time.Now().Unix(),
			From:      user{ID: in.FromID, Username: in.Username, FirstName: firstName(in)},
			Chat:      chat{ID: chatID, Type: "private"},
			Text:      in.Text,
		},
	})
	close(h.arrived)
	h.arrived = make(chan struct{})
	return h.lastUpdate, nil
}

func firstName(in Inbound) string {
	if in.Username != "" {
		return in.Username
	}
	return "User " + strconv.FormatInt(in.FromID, 10)
}

// Messages returns what bots sent, oldest first, filtered by bot and chat when
// those are not empty.
func (h *Handler) Messages(botID, chatID string) []Sent {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := []Sent{}
	for _, m := range h.sent {
		if (botID == "" || m.BotID == botID) && (chatID == "" || m.ChatID == chatID) {
			out = append(out, m)
		}
	}
	return out
}

// Reset forgets every queued update and sent message.
func (h *Handler) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pending = map[string][]update{}
	h.sent = nil
}

func (h *Handler) serveControlUpdates(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "mocktelegram: only POST", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		BotID    json.RawMessage `json:"bot_id"`
		FromID   int64           `json:"from_id"`
		ChatID   int64           `json:"chat_id"`
		Text     string          `json:"text"`
		Username string          `json:"username"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "mocktelegram: parse update: "+err.Error(), http.StatusBadRequest)
		return
	}
	id, err := h.Enqueue(Inbound{
		BotID: unquote(body.BotID), FromID: body.FromID, ChatID: body.ChatID,
		Text: body.Text, Username: body.Username,
	})
	if err != nil {
		http.Error(w, "mocktelegram: "+err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"update_id": id})
}

func (h *Handler) serveControlMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "mocktelegram: only GET", http.StatusMethodNotAllowed)
		return
	}
	q := r.URL.Query()
	writeJSON(w, http.StatusOK, h.Messages(q.Get("bot_id"), q.Get("chat_id")))
}

// serveBotAPI answers POST <base>/bot<token>/<method>.
func (h *Handler) serveBotAPI(w http.ResponseWriter, r *http.Request) {
	rest, method := lastSegment(r.URL.Path)
	_, segment := lastSegment(rest)
	token, ok := strings.CutPrefix(segment, "bot")
	if !ok || method == "" {
		apiError(w, http.StatusNotFound, "Not Found")
		return
	}
	botID, ok := botIDOf(token)
	if !ok {
		apiError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}
	params, err := readParams(r)
	if err != nil {
		apiError(w, http.StatusBadRequest, "Bad Request: "+err.Error())
		return
	}
	switch method {
	case "getMe":
		id, _ := strconv.ParseInt(botID, 10, 64)
		apiResult(w, user{ID: id, IsBot: true, Username: username(botID), FirstName: "Smoke " + botID})
	case "getUpdates":
		h.getUpdates(w, r, botID, params)
	case "sendMessage":
		h.sendMessage(w, botID, params)
	case "sendChatAction":
		apiResult(w, true)
	default:
		apiError(w, http.StatusNotFound, "Not Found: method not found")
	}
}

// getUpdates drops every update below offset, which is how a poller confirms
// it took them, then answers what remains or waits up to timeout seconds for
// something to arrive.
func (h *Handler) getUpdates(w http.ResponseWriter, r *http.Request, botID string, params map[string]json.RawMessage) {
	offset := intParam(params, "offset")
	limit := intParam(params, "limit")
	if limit <= 0 || limit > 100 {
		limit = 100
	}
	wait := min(time.Duration(intParam(params, "timeout"))*time.Second, h.maxPoll)
	deadline := time.NewTimer(max(wait, 0))
	defer deadline.Stop()
	for {
		updates, arrived := h.take(botID, offset, int(limit))
		if len(updates) > 0 || wait <= 0 {
			apiResult(w, updates)
			return
		}
		select {
		case <-arrived:
		case <-deadline.C:
			apiResult(w, []update{})
			return
		case <-r.Context().Done():
			return
		}
	}
}

func (h *Handler) take(botID string, offset int64, limit int) ([]update, <-chan struct{}) {
	h.mu.Lock()
	defer h.mu.Unlock()
	queue := h.pending[botID]
	if offset > 0 {
		i := 0
		for i < len(queue) && queue[i].UpdateID < offset {
			i++
		}
		queue = queue[i:]
		h.pending[botID] = queue
	}
	out := make([]update, 0, min(len(queue), limit))
	out = append(out, queue[:min(len(queue), limit)]...)
	return out, h.arrived
}

func (h *Handler) sendMessage(w http.ResponseWriter, botID string, params map[string]json.RawMessage) {
	chatID := unquote(params["chat_id"])
	var text string
	if raw, ok := params["text"]; ok {
		if err := json.Unmarshal(raw, &text); err != nil {
			apiError(w, http.StatusBadRequest, "Bad Request: text is not a string")
			return
		}
	}
	if chatID == "" || text == "" {
		apiError(w, http.StatusBadRequest, "Bad Request: chat_id and text are required")
		return
	}
	h.mu.Lock()
	h.lastMessage++
	sent := Sent{BotID: botID, ChatID: chatID, Text: text, MessageID: h.lastMessage}
	h.sent = append(h.sent, sent)
	if len(h.sent) > maxSent {
		h.sent = append([]Sent(nil), h.sent[len(h.sent)-maxSent:]...)
	}
	h.mu.Unlock()
	id, _ := strconv.ParseInt(botID, 10, 64)
	chatNum, _ := strconv.ParseInt(chatID, 10, 64)
	apiResult(w, message{
		MessageID: sent.MessageID,
		Date:      time.Now().Unix(),
		From:      user{ID: id, IsBot: true, Username: username(botID), FirstName: "Smoke " + botID},
		Chat:      chat{ID: chatNum, Type: "private"},
		Text:      text,
	})
}

// botIDOf reads the bot id out of a token, or reports false for one Telegram
// would refuse.
func botIDOf(token string) (string, bool) {
	id, secret, ok := strings.Cut(token, ":")
	if !ok || secret == "" || secret == "invalid" || !validBotID(id) {
		return "", false
	}
	return id, true
}

func validBotID(id string) bool {
	if id == "" || len(id) > 18 {
		return false
	}
	for _, c := range id {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func username(botID string) string { return "smoke" + botID + "_bot" }

// lastSegment splits path at its last slash.
func lastSegment(path string) (rest, last string) {
	i := strings.LastIndex(path, "/")
	if i < 0 {
		return "", path
	}
	return path[:i], path[i+1:]
}

// readParams reads a method's JSON body. An empty body is no parameters, as
// the adapter sends for getMe.
func readParams(r *http.Request) (map[string]json.RawMessage, error) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	params := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(body)) == 0 {
		return params, nil
	}
	if err := json.Unmarshal(body, &params); err != nil {
		return nil, err
	}
	return params, nil
}

func intParam(params map[string]json.RawMessage, name string) int64 {
	n, _ := strconv.ParseInt(unquote(params[name]), 10, 64)
	return n
}

// unquote reads a JSON number or string as its text, since the Bot API takes
// either for an id.
func unquote(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return strings.TrimSpace(string(raw))
}

func apiResult(w http.ResponseWriter, result any) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
}

func apiError(w http.ResponseWriter, status int, description string) {
	writeJSON(w, status, map[string]any{"ok": false, "error_code": status, "description": description})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
