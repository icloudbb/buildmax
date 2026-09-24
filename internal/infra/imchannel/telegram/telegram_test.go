package telegram

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"

	corechannel "github.com/icloudbb/buildmax/internal/core/channel"
)

const testToken = "123456:secret-token-value"

// fakeBotAPI is a Bot API double: it serves queued updates to getUpdates,
// honoring offset the way Telegram does, and records every other call.
type fakeBotAPI struct {
	t       *testing.T
	mu      sync.Mutex
	updates []map[string]any
	calls   []recordedCall
	status  map[string]string // method → canned error JSON
}

type recordedCall struct {
	Method string
	Params map[string]any
}

func newFakeBotAPI(t *testing.T) (*fakeBotAPI, *httptest.Server) {
	f := &fakeBotAPI{t: t, status: map[string]string{}}
	srv := httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(srv.Close)
	return f, srv
}

func (f *fakeBotAPI) serve(w http.ResponseWriter, r *http.Request) {
	prefix := "/bot" + testToken + "/"
	if !strings.HasPrefix(r.URL.Path, prefix) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":404,"description":"Not Found"}`))
		return
	}
	method := strings.TrimPrefix(r.URL.Path, prefix)
	var params map[string]any
	_ = json.NewDecoder(r.Body).Decode(&params)
	f.mu.Lock()
	f.calls = append(f.calls, recordedCall{Method: method, Params: params})
	canned := f.status[method]
	f.mu.Unlock()
	if canned != "" {
		_, _ = w.Write([]byte(canned))
		return
	}
	switch method {
	case "getUpdates":
		offset := int64(0)
		if v, ok := params["offset"].(float64); ok {
			offset = int64(v)
		}
		f.mu.Lock()
		var out []map[string]any
		for _, u := range f.updates {
			if int64(u["update_id"].(int)) >= offset {
				out = append(out, u)
			}
		}
		f.mu.Unlock()
		if len(out) == 0 {
			// Stand in for the long poll without holding the test.
			time.Sleep(20 * time.Millisecond)
		}
		writeResult(w, out)
	case "getMe":
		writeResult(w, map[string]any{"id": 1, "is_bot": true, "username": "buildmax_test_bot"})
	default:
		writeResult(w, true)
	}
}

func writeResult(w http.ResponseWriter, result any) {
	if result == nil {
		result = []any{}
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
}

func (f *fakeBotAPI) push(u map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updates = append(f.updates, u)
}

func (f *fakeBotAPI) callsTo(method string) []recordedCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []recordedCall
	for _, c := range f.calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

func textUpdate(id int, chatType string, from map[string]any, text string) map[string]any {
	return map[string]any{
		"update_id": id,
		"message": map[string]any{
			"message_id": id,
			"from":       from,
			"chat":       map[string]any{"id": 42, "type": chatType},
			"text":       text,
		},
	}
}

func TestReceiveNormalizesMessagesAndSkipsBots(t *testing.T) {
	api, srv := newFakeBotAPI(t)
	person := map[string]any{"id": 42, "is_bot": false, "username": "ada"}
	api.push(textUpdate(10, "private", person, "hello"))
	api.push(textUpdate(11, "private", map[string]any{"id": 7, "is_bot": true, "username": "other_bot"}, "beep"))
	api.push(textUpdate(12, "supergroup", map[string]any{"id": 43, "first_name": "Grace", "last_name": "Hopper"}, "hi all"))
	api.push(map[string]any{"update_id": 13, "message": map[string]any{"from": person, "chat": map[string]any{"id": 42, "type": "private"}}})

	c := New(Config{Token: testToken, APIBaseURL: srv.URL, PollTimeout: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	var got []corechannel.Inbound
	done := make(chan error, 1)
	go func() {
		done <- c.Receive(ctx, func(in corechannel.Inbound) {
			got = append(got, in)
			if len(got) == 3 {
				cancel()
			}
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Receive: %v", err)
		}
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("Receive did not deliver three messages")
	}

	want := []corechannel.Inbound{
		{EventID: "10", ChatID: "42", ChatType: corechannel.ChatPrivate, SenderID: "42", SenderHandle: "@ada", Text: "hello"},
		{EventID: "12", ChatID: "42", ChatType: corechannel.ChatGroup, SenderID: "43", SenderHandle: "Grace Hopper", Text: "hi all"},
		{EventID: "13", ChatID: "42", ChatType: corechannel.ChatPrivate, SenderID: "42", SenderHandle: "@ada", Unsupported: true},
	}
	if len(got) != len(want) {
		t.Fatalf("delivered %d messages, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("message %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// Stopping must confirm what was taken, or the next holder of the connector
// answers the same messages again.
func TestReceiveConfirmsTakenUpdatesWhenItStops(t *testing.T) {
	api, srv := newFakeBotAPI(t)
	api.push(textUpdate(20, "private", map[string]any{"id": 42, "username": "ada"}, "one"))
	c := New(Config{Token: testToken, APIBaseURL: srv.URL, PollTimeout: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	if err := c.Receive(ctx, func(corechannel.Inbound) { cancel() }); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	polls := api.callsTo("getUpdates")
	last := polls[len(polls)-1]
	if off, _ := last.Params["offset"].(float64); off != 21 {
		t.Errorf("last getUpdates offset = %v, want 21 so update 20 is confirmed", last.Params["offset"])
	}
}

func TestReceiveStopsOnARejectedToken(t *testing.T) {
	api, srv := newFakeBotAPI(t)
	api.status["getUpdates"] = `{"ok":false,"error_code":401,"description":"Unauthorized"}`
	c := New(Config{Token: testToken, APIBaseURL: srv.URL, PollTimeout: time.Second})
	err := c.Receive(context.Background(), func(corechannel.Inbound) {})
	if !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("Receive = %v, want ErrUnauthorized", err)
	}
}

func TestSendDisablesPreviewsAndSplitsLongText(t *testing.T) {
	api, srv := newFakeBotAPI(t)
	c := New(Config{Token: testToken, APIBaseURL: srv.URL})
	long := strings.Repeat("a", maxMessageUnits) + "tail"
	if err := c.Send(context.Background(), corechannel.Outbound{ChatID: "42", Text: long}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	sends := api.callsTo("sendMessage")
	if len(sends) != 2 {
		t.Fatalf("sendMessage calls = %d, want 2", len(sends))
	}
	if chatID, _ := sends[0].Params["chat_id"].(float64); chatID != 42 {
		t.Errorf("chat_id = %v, want the number 42", sends[0].Params["chat_id"])
	}
	opts, _ := sends[0].Params["link_preview_options"].(map[string]any)
	if opts["is_disabled"] != true {
		t.Errorf("link_preview_options = %v, want previews disabled", sends[0].Params["link_preview_options"])
	}
	if sends[1].Params["text"] != "tail" {
		t.Errorf("second part = %q, want the remainder", sends[1].Params["text"])
	}
}

func TestSendRetriesOnceAfterARateLimit(t *testing.T) {
	api, srv := newFakeBotAPI(t)
	api.status["sendMessage"] = `{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":1}}`
	c := New(Config{Token: testToken, APIBaseURL: srv.URL})
	go func() {
		time.Sleep(200 * time.Millisecond)
		api.mu.Lock()
		delete(api.status, "sendMessage")
		api.mu.Unlock()
	}()
	if err := c.Send(context.Background(), corechannel.Outbound{ChatID: "42", Text: "hi"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if n := len(api.callsTo("sendMessage")); n != 2 {
		t.Errorf("sendMessage calls = %d, want a retry after the limit", n)
	}
}

// The token is in every request URL, so a transport error must not carry it
// into a log line.
func TestErrorsNeverCarryTheToken(t *testing.T) {
	c := New(Config{Token: testToken, APIBaseURL: "http://127.0.0.1:1"})
	err := c.Send(context.Background(), corechannel.Outbound{ChatID: "42", Text: "hi"})
	if err == nil {
		t.Fatal("Send to a closed port succeeded")
	}
	if strings.Contains(err.Error(), "secret-token-value") {
		t.Errorf("error leaks the token: %v", err)
	}
}

func TestInfoNamesTheBot(t *testing.T) {
	_, srv := newFakeBotAPI(t)
	info := New(Config{Token: testToken, APIBaseURL: srv.URL}).Info(context.Background())
	if info.BotHandle != "@buildmax_test_bot" || info.BotURL != "https://t.me/buildmax_test_bot" {
		t.Errorf("Info = %+v", info)
	}
}

func TestSplitMessageCountsUTF16AndPrefersNewlines(t *testing.T) {
	// An emoji is two UTF-16 units: three of them do not fit in five units.
	parts := splitMessage("😀😀😀", 5)
	if len(parts) != 2 || parts[0] != "😀😀" || parts[1] != "😀" {
		t.Errorf("emoji split = %q", parts)
	}
	parts = splitMessage("first line\nsecond line", 15)
	if len(parts) != 2 || parts[0] != "first line\n" {
		t.Errorf("newline split = %q, want a break after the newline", parts)
	}
	for _, p := range splitMessage(strings.Repeat("ab\n", 3000), maxMessageUnits) {
		if n := len(utf16.Encode([]rune(p))); n > maxMessageUnits {
			t.Errorf("part of %d units exceeds the limit", n)
		}
	}
	if splitMessage("", 10) != nil {
		t.Error("empty text should produce no parts")
	}
}
