package channel

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	corechannel "github.com/icloudbb/buildmax/internal/core/channel"
	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

const assistantKey = "asst1"

// fakeFrontDoor answers for bots other than the system bot.
type fakeFrontDoor struct {
	mu    sync.Mutex
	calls []string
	// outcomeKey is the bot Outcome reports through.
	outcomeKey string
}

func (f *fakeFrontDoor) Answer(_ context.Context, key string, in corechannel.Inbound, userID string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, key+"|"+userID+"|"+in.Text)
	return "front: " + in.Text
}

func (f *fakeFrontDoor) Outcome(_ context.Context, conv *coreconv.Conversation, info coretask.RunTerminalInfo) (string, string, bool) {
	return f.outcomeKey, "outcome of " + info.TaskID, true
}

func (f *fakeFrontDoor) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// withAssistantBot registers a second Telegram bot beside the system one, with
// a front door answering it.
func withAssistantBot(t *testing.T, h *harness) (*fakeConnector, bot, *fakeFrontDoor) {
	t.Helper()
	c := newFakeConnector("200")
	if err := h.g.Register(context.Background(), assistantKey, c); err != nil {
		t.Fatalf("Register: %v", err)
	}
	fd := &fakeFrontDoor{}
	h.g.SetFrontDoor(fd)
	return c, bot{key: assistantKey, Connector: c}, fd
}

// A person can link from whichever bot they message first, and that bot is the
// one that confirms it. The link is theirs, so every bot then recognizes them.
func TestAnyBotCanStartAPairingAndConfirmsIt(t *testing.T) {
	h := newHarness(t)
	asst, b, _ := withAssistantBot(t, h)

	h.g.handle(context.Background(), b, dm("hello"))
	code := h.ids.onlyCode()
	if code == "" || !strings.Contains(asst.last(), displayCode(code)) {
		t.Fatalf("the assistant bot offered no code: %q", asst.last())
	}
	if p, _ := h.ids.PairingByCode(context.Background(), code, time.Now()); p == nil || p.Connector != assistantKey {
		t.Fatalf("pairing = %+v, want it to record the issuing bot", p)
	}
	if _, err := h.g.ConfirmPairing(context.Background(), adaID, code); err != nil {
		t.Fatalf("ConfirmPairing: %v", err)
	}
	if !strings.Contains(asst.last(), "Linked") {
		t.Errorf("the issuing bot did not confirm: %q", asst.last())
	}
	if len(h.conn.messages()) != 0 {
		t.Errorf("the system bot spoke in a pairing it did not start: %+v", h.conn.messages())
	}

	h.send(dm("hi from the system bot"))
	if h.conn.last() != "echo: hi from the system bot" {
		t.Errorf("the system bot did not recognize the link: %q", h.conn.last())
	}
}

// A linked person messaging another bot reaches its front door, never the
// personal assistant, and a run from that bot's conversation reports through it.
func TestOtherBotsGoToTheFrontDoorAndReportThroughThemselves(t *testing.T) {
	h := newHarness(t)
	h.ids.link(adaID, adaChat)
	asst, b, fd := withAssistantBot(t, h)

	h.send(dm("to the system bot"))
	h.g.handle(context.Background(), b, dm("to the assistant bot"))
	h.g.handle(context.Background(), b, dm("/space"))

	if got := fd.seen(); !slices.Equal(got, []string{assistantKey + "|" + adaID + "|to the assistant bot", assistantKey + "|" + adaID + "|/space"}) {
		t.Errorf("front door calls = %v", got)
	}
	if n := len(h.turns.seen()); n != 1 {
		t.Errorf("personal turns = %d, want only the system bot's", n)
	}
	if asst.last() != "front: /space" || h.conn.last() != "echo: to the system bot" {
		t.Errorf("replies went to the wrong bot: system %q, assistant %q", h.conn.last(), asst.last())
	}

	// A person's chat id is the same with every bot, so the assistant's
	// conversation is told apart by its connector key.
	conv, _ := h.convs.CreateChatConversation(context.Background(), teamID, adaID, corechannel.PlatformTelegram, assistantKey, adaChat)
	h.elig.refuse(adaID, teamID, nil)
	before := len(h.conn.messages())
	info := coretask.RunTerminalInfo{TaskID: "task1", ConversationID: conv.ID, SpaceID: teamID, Status: string(coretask.RunStatusCanceled)}
	h.g.ReportRunTerminal(context.Background(), info)
	if !strings.Contains(asst.last(), "was canceled") || len(h.conn.messages()) != before {
		t.Errorf("the report did not go through the conversation's bot: assistant %q", asst.last())
	}

	// An Assistant's conversation is reported by its front door: its text,
	// through the bot it names, and never the personal report.
	h.convs.mu.Lock()
	h.convs.convs = append(h.convs.convs, coreconv.Conversation{
		ID: "asst-conv", SpaceID: teamID, UserID: adaID, Channel: corechannel.PlatformTelegram,
		ChannelConnector: "an-old-binding", ChannelRef: adaChat, AssistantID: "asst_hr",
	})
	h.convs.mu.Unlock()
	fd.outcomeKey = assistantKey
	info = coretask.RunTerminalInfo{TaskID: "task2", ConversationID: "asst-conv", SpaceID: teamID, Status: string(coretask.RunStatusSucceeded)}
	h.g.ReportRunTerminal(context.Background(), info)
	if asst.last() != "outcome of task2" || len(h.conn.messages()) != before {
		t.Errorf("assistant report = %q", asst.last())
	}
}

// Without a front door, another bot tells a linked person it is not available.
func TestOtherBotWithoutAFrontDoorIsNotAvailable(t *testing.T) {
	h := newHarness(t)
	h.ids.link(adaID, adaChat)
	c := newFakeConnector("200")
	if err := h.g.Register(context.Background(), assistantKey, c); err != nil {
		t.Fatal(err)
	}
	h.g.handle(context.Background(), bot{key: assistantKey, Connector: c}, dm("hi"))
	if c.last() != "This assistant is not available." || len(h.turns.seen()) != 0 {
		t.Errorf("reply = %q, turns = %v", c.last(), h.turns.seen())
	}
}

// A conversation or pairing written before connector keys existed belongs to
// the system bot.
func TestAnEmptyConnectorKeyMeansTheSystemBot(t *testing.T) {
	h := newHarness(t)
	if c := h.g.connector(corechannel.PlatformTelegram, ""); c == nil || c.key != corechannel.ConnectorSystem {
		t.Fatalf("connector(\"\") = %+v, want the system bot", c)
	}
	if c := h.g.connector(corechannel.PlatformTelegram, "missing"); c != nil {
		t.Errorf("connector(missing) = %+v, want nil", c)
	}
}

func TestRegisterRefusesABotAlreadyServed(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if err := h.g.Register(ctx, "dup", newFakeConnector("100")); !errors.Is(err, corechannel.ErrBotInUse) {
		t.Errorf("registering the system bot's token again: %v, want ErrBotInUse", err)
	}
	withAssistantBot(t, h)
	if inUse, err := h.g.BotInUse(ctx, corechannel.PlatformTelegram, "200", ""); err != nil || !inUse {
		t.Errorf("BotInUse(200) = %v, %v; want the registered assistant bot", inUse, err)
	}
	if inUse, _ := h.g.BotInUse(ctx, corechannel.PlatformTelegram, "200", assistantKey); inUse {
		t.Error("BotInUse counted the bot it was told to except")
	}
	if err := h.g.Register(ctx, assistantKey, newFakeConnector("300")); err == nil {
		t.Error("a key in use was registered again")
	}
	if err := h.g.Register(ctx, corechannel.ConnectorSystem, newFakeConnector("300")); err == nil {
		t.Error("the system key was registered")
	}
	failing := newFakeConnector("")
	failing.botErr = errors.New("unauthorized")
	if err := h.g.Register(ctx, "bad", failing); err == nil {
		t.Error("a bot that could not be identified was registered")
	}
}

// Redelivery ids are per bot: the same id from two bots is two messages.
func TestDeduplicationIsPerBot(t *testing.T) {
	h := newHarness(t)
	h.ids.link(adaID, adaChat)
	_, b, fd := withAssistantBot(t, h)
	in := dm("same id")
	in.EventID = "7"
	h.g.accept(h.system(), in)
	h.g.accept(b, in)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h.g.Wait(ctx)
	if len(h.turns.seen()) != 1 || len(fd.seen()) != 1 {
		t.Errorf("turns = %v, front door = %v; want one message per bot", h.turns.seen(), fd.seen())
	}
}

// grantAllLocker grants every lease it is asked for once per key.
type grantAllLocker struct {
	mu   sync.Mutex
	held map[string]bool
}

func (l *grantAllLocker) TryAcquire(_ context.Context, key string) (Lease, bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held[key] {
		return nil, false, nil
	}
	l.held[key] = true
	return &fakeLease{lost: make(chan struct{}), released: make(chan struct{})}, true, nil
}

func (l *grantAllLocker) keys() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for k := range l.held {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// A bot registered on a running Gateway starts receiving under its own lease,
// and stops when unregistered while the system bot keeps going.
func TestRegisteredBotReceivesUntilUnregistered(t *testing.T) {
	h := newHarness(t)
	h.ids.link(adaID, adaChat)
	locker := &grantAllLocker{held: map[string]bool{}}
	h.g.locker = locker
	h.g.Start()
	defer h.g.Stop()
	waitReceiving(t, h.conn)

	asst, _, _ := withAssistantBot(t, h)
	waitReceiving(t, asst)
	want := []string{"channel-connector:telegram:" + assistantKey, "channel-connector:telegram:system"}
	if got := locker.keys(); !slices.Equal(got, want) {
		t.Errorf("leases = %v, want %v", got, want)
	}

	asst.inbox <- dm("through the running assistant bot")
	waitFor(t, func() bool { return asst.last() == "front: through the running assistant bot" })

	done := make(chan struct{})
	go func() { h.g.Unregister(assistantKey); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Unregister did not return")
	}
	if c := h.g.connector(corechannel.PlatformTelegram, assistantKey); c != nil {
		t.Error("the unregistered bot can still be sent through")
	}
	h.g.Unregister(systemKey(corechannel.PlatformTelegram))
	h.conn.inbox <- dm("system still here")
	waitFor(t, func() bool { return h.conn.last() == "echo: system still here" })
}

func waitReceiving(t *testing.T, c *fakeConnector) {
	t.Helper()
	select {
	case <-c.received:
	case <-time.After(5 * time.Second):
		t.Fatal("the bot never started receiving")
	}
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
