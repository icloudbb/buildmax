package mocktelegram

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	corechannel "github.com/icloudbb/buildmax/internal/core/channel"
	"github.com/icloudbb/buildmax/internal/infra/imchannel/telegram"
)

// The gateway drives the real adapter against this mock, so these tests do
// too: a mock checked only against hand-written requests would prove it agrees
// with itself, not with the connector a deployment runs.

func start(t *testing.T) (*Handler, *httptest.Server) {
	t.Helper()
	h := New()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return h, srv
}

func connector(srv *httptest.Server, token string) *telegram.Connector {
	return telegram.New(telegram.Config{Token: token, APIBaseURL: srv.URL, PollTimeout: 5 * time.Second})
}

// call posts one Bot API method and decodes the envelope.
func call(t *testing.T, srv *httptest.Server, token, method, body string) (int, map[string]json.RawMessage) {
	t.Helper()
	resp, err := http.Post(srv.URL+"/bot"+token+"/"+method, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("%s: decode: %v", method, err)
	}
	return resp.StatusCode, out
}

func updateIDs(t *testing.T, env map[string]json.RawMessage) []int64 {
	t.Helper()
	var updates []update
	if err := json.Unmarshal(env["result"], &updates); err != nil {
		t.Fatalf("decode updates: %v", err)
	}
	ids := make([]int64, len(updates))
	for i, u := range updates {
		ids[i] = u.UpdateID
	}
	return ids
}

func postControl(t *testing.T, srv *httptest.Server, path, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(srv.URL+"/smoke-telegram"+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestGetMeNamesTheBotAfterItsTokenDigits(t *testing.T) {
	_, srv := start(t)
	c := connector(srv, "2000:anything")
	id, err := c.BotID(context.Background())
	if err != nil || id != "2000" {
		t.Fatalf("BotID = %q, %v; want 2000", id, err)
	}
	if info := c.Info(context.Background()); info.BotHandle != "@smoke2000_bot" {
		t.Errorf("BotHandle = %q, want @smoke2000_bot", info.BotHandle)
	}
}

func TestAWrongTokenIsUnauthorizedOnEveryMethod(t *testing.T) {
	_, srv := start(t)
	for _, token := range []string{"invalid", "2000:invalid", "2000", "abc:secret", "2000:"} {
		if _, err := connector(srv, token).BotID(context.Background()); !errors.Is(err, telegram.ErrUnauthorized) {
			t.Errorf("token %q: BotID err = %v, want ErrUnauthorized", token, err)
		}
		for _, method := range []string{"getMe", "getUpdates", "sendMessage", "sendChatAction"} {
			if status, _ := call(t, srv, token, method, `{}`); status != http.StatusUnauthorized {
				t.Errorf("token %q %s: status %d, want 401", token, method, status)
			}
		}
	}
}

func TestReceiveDeliversAnEnqueuedMessageBeforeThePollTimesOut(t *testing.T) {
	_, srv := start(t)
	c := connector(srv, "2000:secret")
	ctx, cancel := context.WithCancel(context.Background())
	got := make(chan corechannel.Inbound, 1)
	done := make(chan error, 1)
	go func() { done <- c.Receive(ctx, func(in corechannel.Inbound) { got <- in }) }()

	// Let the poll start waiting first, so the update wakes it.
	time.Sleep(200 * time.Millisecond)
	sentAt := time.Now()
	resp := postControl(t, srv, ControlUpdatesPath, `{"bot_id":"2000","from_id":42,"text":"hello","username":"alice"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("enqueue: status %d", resp.StatusCode)
	}
	select {
	case in := <-got:
		if in.SenderID != "42" || in.ChatID != "42" || in.ChatType != corechannel.ChatPrivate || in.Text != "hello" || in.SenderHandle != "@alice" {
			t.Errorf("inbound = %+v", in)
		}
		if waited := time.Since(sentAt); waited > 2*time.Second {
			t.Errorf("delivery took %s; the long poll did not return early", waited)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("the enqueued message was not delivered")
	}
	cancel()
	if err := <-done; err != nil {
		t.Errorf("Receive = %v", err)
	}
}

func TestGetUpdatesDropsWhatAnOffsetConfirms(t *testing.T) {
	h, srv := start(t)
	first, _ := h.Enqueue(Inbound{BotID: "2000", FromID: 1, Text: "one"})
	second, _ := h.Enqueue(Inbound{BotID: "2000", FromID: 1, Text: "two"})
	if second <= first {
		t.Fatalf("update ids %d then %d do not increase", first, second)
	}
	if _, env := call(t, srv, "2000:s", "getUpdates", `{"offset":0,"timeout":0}`); len(updateIDs(t, env)) != 2 {
		t.Fatalf("unconfirmed poll = %v, want both", updateIDs(t, env))
	}
	if _, env := call(t, srv, "2000:s", "getUpdates", `{"offset":`+strconv.FormatInt(second, 10)+`,"timeout":0}`); !slices.Equal(updateIDs(t, env), []int64{second}) {
		t.Fatalf("poll past the first = %v, want [%d]", updateIDs(t, env), second)
	}
	// The first was confirmed by asking past it, so it is gone for good.
	if _, env := call(t, srv, "2000:s", "getUpdates", `{"timeout":0}`); !slices.Equal(updateIDs(t, env), []int64{second}) {
		t.Fatalf("poll after confirming = %v, want [%d]", updateIDs(t, env), second)
	}
	if _, env := call(t, srv, "2000:s", "getUpdates", `{"offset":`+strconv.FormatInt(second, 10)+`,"limit":1,"timeout":0}`); len(updateIDs(t, env)) != 1 {
		t.Fatalf("limit 1 = %v", updateIDs(t, env))
	}
}

func TestEachBotSeesOnlyItsOwnUpdates(t *testing.T) {
	h, srv := start(t)
	if _, err := h.Enqueue(Inbound{BotID: "2000", FromID: 1, Text: "for 2000"}); err != nil {
		t.Fatal(err)
	}
	if _, env := call(t, srv, "3000:s", "getUpdates", `{"timeout":0}`); len(updateIDs(t, env)) != 0 {
		t.Errorf("bot 3000 saw %v", updateIDs(t, env))
	}
	if _, env := call(t, srv, "2000:other-secret", "getUpdates", `{"timeout":0}`); len(updateIDs(t, env)) != 1 {
		t.Errorf("bot 2000 saw %v", updateIDs(t, env))
	}
}

func TestALongPollIsCappedAndEndsEmpty(t *testing.T) {
	h, srv := start(t)
	h.maxPoll = 200 * time.Millisecond
	began := time.Now()
	status, env := call(t, srv, "2000:s", "getUpdates", `{"timeout":30}`)
	if status != http.StatusOK || string(env["ok"]) != "true" || len(updateIDs(t, env)) != 0 {
		t.Fatalf("status %d, envelope %v", status, env)
	}
	if took := time.Since(began); took < 150*time.Millisecond || took > 5*time.Second {
		t.Errorf("poll took %s, want about the 200ms cap", took)
	}
}

func TestSentMessagesAreRecordedAndFiltered(t *testing.T) {
	_, srv := start(t)
	if err := connector(srv, "2000:s").Send(context.Background(), corechannel.Outbound{ChatID: "42", Text: "to 42"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := connector(srv, "3000:s").Send(context.Background(), corechannel.Outbound{ChatID: "7", Text: "to 7"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := connector(srv, "2000:s").Typing(context.Background(), "42"); err != nil {
		t.Fatalf("Typing: %v", err)
	}
	read := func(query string) []Sent {
		resp, err := http.Get(srv.URL + "/smoke-telegram" + ControlMessagesPath + query)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		var out []Sent
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if all := read(""); len(all) != 2 || all[0].Text != "to 42" || all[1].Text != "to 7" {
		t.Errorf("all = %+v, want both oldest first", all)
	}
	if one := read("?bot_id=2000&chat_id=42"); len(one) != 1 || one[0].BotID != "2000" || one[0].ChatID != "42" {
		t.Errorf("filtered = %+v", one)
	}
	if none := read("?chat_id=99"); none == nil || len(none) != 0 {
		t.Errorf("no match = %#v, want an empty array", none)
	}
}

func TestResetForgetsStateButKeepsIDsGrowing(t *testing.T) {
	h, srv := start(t)
	before, _ := h.Enqueue(Inbound{BotID: "2000", FromID: 1, Text: "x"})
	if err := connector(srv, "2000:s").Send(context.Background(), corechannel.Outbound{ChatID: "1", Text: "y"}); err != nil {
		t.Fatal(err)
	}
	if resp := postControl(t, srv, ControlResetPath, ``); resp.StatusCode != http.StatusOK {
		t.Fatalf("reset: status %d", resp.StatusCode)
	}
	if len(h.Messages("", "")) != 0 {
		t.Error("reset kept sent messages")
	}
	if _, env := call(t, srv, "2000:s", "getUpdates", `{"timeout":0}`); len(updateIDs(t, env)) != 0 {
		t.Error("reset kept queued updates")
	}
	// A poller's offset survives a reset, so a reused id would be skipped.
	if after, _ := h.Enqueue(Inbound{BotID: "2000", FromID: 1, Text: "z"}); after <= before {
		t.Errorf("update id %d after reset is not above %d", after, before)
	}
}

func TestControlRoutesRefuseBadRequests(t *testing.T) {
	_, srv := start(t)
	for _, body := range []string{`{"bot_id":"bot","from_id":1}`, `{"bot_id":"2000"}`, `not json`} {
		if resp := postControl(t, srv, ControlUpdatesPath, body); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", body, resp.StatusCode)
		}
	}
	// A numeric bot_id is accepted as readily as a string.
	if resp := postControl(t, srv, ControlUpdatesPath, `{"bot_id":2000,"from_id":1,"text":"n"}`); resp.StatusCode != http.StatusOK {
		t.Errorf("numeric bot_id: status %d", resp.StatusCode)
	}
	resp, err := http.Get(srv.URL + ControlUpdatesPath)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET updates: status %d, want 405", resp.StatusCode)
	}
	if status, _ := call(t, srv, "2000:s", "deleteWebhook", `{}`); status != http.StatusNotFound {
		t.Errorf("unknown method: status %d, want 404", status)
	}
}
