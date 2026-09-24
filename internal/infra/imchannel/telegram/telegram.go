// Package telegram is the Telegram Bot API connector. It receives by long
// polling getUpdates, which needs no public URL, and speaks the few methods a
// text chat uses over plain HTTPS rather than through an SDK: the surface is
// small and stable, and a dependency would be most of the code.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf16"

	corechannel "github.com/icloudbb/buildmax/internal/core/channel"
)

// DefaultAPIBaseURL is the public Bot API. A self-hosted Bot API server or a
// test double replaces it.
const DefaultAPIBaseURL = "https://api.telegram.org"

const (
	// pollTimeout is how long one getUpdates call waits for a message. The HTTP
	// client allows a margin on top so a slow answer is not a failure.
	pollTimeout = 30 * time.Second
	httpMargin  = 15 * time.Second
	// maxMessageUnits is Telegram's text limit, counted in UTF-16 code units.
	maxMessageUnits = 4096
	// maxBackoff caps the wait between failed polls.
	maxBackoff = 30 * time.Second
)

// ErrUnauthorized means Telegram rejected the bot token. Polling again cannot
// fix it; an operator has to.
var ErrUnauthorized = errors.New("telegram rejected the bot token")

// Config configures one bot.
type Config struct {
	Token string
	// APIBaseURL defaults to DefaultAPIBaseURL.
	APIBaseURL string
	// HTTPClient defaults to a client whose timeout suits long polling.
	HTTPClient *http.Client
	// PollTimeout overrides pollTimeout, for tests.
	PollTimeout time.Duration
	Logger      *slog.Logger
}

// Connector is one Telegram bot.
type Connector struct {
	token   string
	base    string
	client  *http.Client
	poll    time.Duration
	log     *slog.Logger
	infoMu  sync.Mutex
	botName string
}

var _ corechannel.Connector = (*Connector)(nil)

// New returns a connector for cfg.
func New(cfg Config) *Connector {
	base := strings.TrimRight(cfg.APIBaseURL, "/")
	if base == "" {
		base = DefaultAPIBaseURL
	}
	poll := cfg.PollTimeout
	if poll <= 0 {
		poll = pollTimeout
	}
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: poll + httpMargin}
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	return &Connector{token: cfg.Token, base: base, client: client, poll: poll, log: log.With("component", "telegram")}
}

func (c *Connector) Platform() string { return corechannel.PlatformTelegram }

// Info names the bot, asking Telegram once and remembering the answer.
func (c *Connector) Info(ctx context.Context) corechannel.Info {
	info := corechannel.Info{Platform: corechannel.PlatformTelegram, Name: "Telegram"}
	c.infoMu.Lock()
	defer c.infoMu.Unlock()
	if c.botName == "" {
		ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		var me struct {
			Username string `json:"username"`
		}
		if err := c.call(ctx, "getMe", nil, &me); err == nil {
			c.botName = me.Username
		}
	}
	if c.botName != "" {
		info.BotHandle = "@" + c.botName
		info.BotURL = "https://t.me/" + c.botName
	}
	return info
}

type update struct {
	UpdateID int64    `json:"update_id"`
	Message  *message `json:"message"`
}

type message struct {
	From *user  `json:"from"`
	Chat chat   `json:"chat"`
	Text string `json:"text"`
}

type user struct {
	ID        int64  `json:"id"`
	IsBot     bool   `json:"is_bot"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

type chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

// Receive long-polls getUpdates until ctx ends. The offset lives in memory:
// Telegram keeps every update this poller has not confirmed by asking past it,
// so a replica that takes over the connector starts from what the last holder
// left unconfirmed.
func (c *Connector) Receive(ctx context.Context, deliver func(corechannel.Inbound)) error {
	var offset int64
	defer func() { c.confirm(offset) }()
	backoff := time.Second
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		var updates []update
		err := c.call(ctx, "getUpdates", map[string]any{
			"offset":          offset,
			"timeout":         int(c.poll / time.Second),
			"allowed_updates": []string{"message"},
		}, &updates)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if errors.Is(err, ErrUnauthorized) {
				return err
			}
			// A conflict here means a webhook is set for this bot or another
			// process is polling it; both make this poller lose messages, so the
			// operator needs to hear it, but retrying is still right.
			c.log.Warn("telegram poll failed; retrying", "err", err, "backoff", backoff)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, maxBackoff)
			continue
		}
		backoff = time.Second
		for _, u := range updates {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			if in, ok := normalize(u); ok {
				deliver(in)
			}
		}
	}
}

// confirm tells Telegram every update below offset was taken. Telegram
// otherwise confirms a batch only on the next poll, so a poller that stops
// would hand its last batch, already accepted, to the next holder again.
func (c *Connector) confirm(offset int64) {
	if offset == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var ignored []update
	if err := c.call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 0, "limit": 1}, &ignored); err != nil {
		c.log.Warn("telegram could not confirm the last updates; they may be delivered again", "err", err)
	}
}

// normalize turns an update into an Inbound, or reports false for one the
// gateway never needs to see: an edit, a service message, another bot.
func normalize(u update) (corechannel.Inbound, bool) {
	m := u.Message
	if m == nil || m.From == nil || m.From.IsBot {
		return corechannel.Inbound{}, false
	}
	chatType := corechannel.ChatGroup
	if m.Chat.Type == "private" {
		chatType = corechannel.ChatPrivate
	}
	return corechannel.Inbound{
		EventID:      strconv.FormatInt(u.UpdateID, 10),
		ChatID:       strconv.FormatInt(m.Chat.ID, 10),
		ChatType:     chatType,
		SenderID:     strconv.FormatInt(m.From.ID, 10),
		SenderHandle: handle(m.From),
		Text:         m.Text,
		Unsupported:  m.Text == "",
	}, true
}

func handle(u *user) string {
	if u.Username != "" {
		return "@" + u.Username
	}
	return strings.TrimSpace(u.FirstName + " " + u.LastName)
}

// Send posts text as plain text with link previews off: a preview fetches a
// URL the model wrote, which is an exfiltration path, and Markdown parse modes
// reject the unescaped text a model produces.
func (c *Connector) Send(ctx context.Context, msg corechannel.Outbound) error {
	for _, part := range splitMessage(msg.Text, maxMessageUnits) {
		if err := c.send(ctx, msg.ChatID, part); err != nil {
			return err
		}
	}
	return nil
}

func (c *Connector) send(ctx context.Context, chatID, text string) error {
	params := map[string]any{
		"chat_id":              chatParam(chatID),
		"text":                 text,
		"link_preview_options": map[string]bool{"is_disabled": true},
	}
	err := c.call(ctx, "sendMessage", params, nil)
	var limited *rateLimitedError
	if errors.As(err, &limited) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(limited.retryAfter):
		}
		err = c.call(ctx, "sendMessage", params, nil)
	}
	return err
}

// Typing shows "typing…" for about five seconds.
func (c *Connector) Typing(ctx context.Context, chatID string) error {
	return c.call(ctx, "sendChatAction", map[string]any{"chat_id": chatParam(chatID), "action": "typing"}, nil)
}

// chatParam sends a numeric chat id as a number, which every Bot API method
// accepts, and anything else as the string Telegram takes for a public
// username.
func chatParam(chatID string) any {
	if n, err := strconv.ParseInt(chatID, 10, 64); err == nil {
		return n
	}
	return chatID
}

type rateLimitedError struct{ retryAfter time.Duration }

func (e *rateLimitedError) Error() string {
	return fmt.Sprintf("telegram rate limit: retry after %s", e.retryAfter)
}

type apiResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
	ErrorCode   int             `json:"error_code"`
	Parameters  struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

// call invokes one Bot API method. The token is part of the URL, so no error
// that could carry the URL leaves here unredacted.
func (c *Connector) call(ctx context.Context, method string, params any, out any) error {
	var body io.Reader = http.NoBody
	if params != nil {
		b, err := json.Marshal(params)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/bot"+c.token+"/"+method, body)
	if err != nil {
		return c.redact(method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return c.redact(method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var parsed apiResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&parsed); err != nil {
		return fmt.Errorf("telegram %s: HTTP %d with an unreadable body", method, resp.StatusCode)
	}
	if !parsed.OK {
		switch {
		// Telegram answers a wrong token with 404 on every method, not only 401.
		case parsed.ErrorCode == http.StatusUnauthorized || parsed.ErrorCode == http.StatusNotFound:
			return ErrUnauthorized
		case parsed.ErrorCode == http.StatusTooManyRequests && parsed.Parameters.RetryAfter > 0:
			return &rateLimitedError{retryAfter: time.Duration(parsed.Parameters.RetryAfter) * time.Second}
		}
		return fmt.Errorf("telegram %s: %d %s", method, parsed.ErrorCode, parsed.Description)
	}
	if out != nil {
		if err := json.Unmarshal(parsed.Result, out); err != nil {
			return fmt.Errorf("telegram %s: decode result: %w", method, err)
		}
	}
	return nil
}

func (c *Connector) redact(method string, err error) error {
	msg := err.Error()
	if c.token != "" {
		msg = strings.ReplaceAll(msg, c.token, "<redacted>")
	}
	return fmt.Errorf("telegram %s: %s", method, msg)
}

// splitMessage cuts text into parts of at most limit UTF-16 code units,
// preferring to break after a newline in the second half of a part so a split
// rarely lands mid-paragraph.
func splitMessage(text string, limit int) []string {
	if text == "" {
		return nil
	}
	var parts []string
	runes := []rune(text)
	for len(runes) > 0 {
		units, cut, lastNewline := 0, len(runes), -1
		for i, r := range runes {
			n := len(utf16.Encode([]rune{r}))
			if units+n > limit {
				cut = i
				break
			}
			units += n
			if r == '\n' {
				lastNewline = i
			}
		}
		if cut < len(runes) && lastNewline >= cut/2 {
			cut = lastNewline + 1
		}
		parts = append(parts, string(runes[:cut]))
		runes = runes[cut:]
	}
	return parts
}
