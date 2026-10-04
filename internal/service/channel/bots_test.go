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
	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

const assistantKey = "asst1"

// withAssistantBot registers a second Telegram bot beside the system one.
func withAssistantBot(t *testing.T, h *harness) (*fakeConnector, bot) {
	t.Helper()
	c := newFakeConnector("200")
	if err := h.g.Register(context.Background(), assistantKey, c); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return c, bot{key: assistantKey, Connector: c}
}

// A person can link from whichever bot they message first, and that bot is the
// one that confirms it. The link is theirs, so every bot then recognizes them.
func TestAnyBotCanStartAPairingAndConfirmsIt(t *testing.T) {
	h := newHarness(t)
	asst, b := withAssistantBot(t, h)

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

// A person's chat id is the same with every bot, so each bot keeps its own
// conversation and reports through itself.
func TestEachBotKeepsItsOwnConversationAndReports(t *testing.T) {
	h := newHarness(t)
	h.ids.link(adaID, adaChat)
	asst, b := withAssistantBot(t, h)

	h.send(dm("to the system bot"))
	h.g.handle(context.Background(), b, dm("to the assistant bot"))
	h.g.handle(context.Background(), b, dm("again to the assistant bot"))

	convs := h.convs.all()
	if len(convs) != 2 {
		t.Fatalf("conversations = %+v, want one per bot", convs)
	}
	byKey := map[string]string{}
	for _, c := range convs {
		byKey[c.ChannelConnector] = c.ID
	}
	if byKey[corechannel.ConnectorSystem] == "" || byKey[assistantKey] == "" {
		t.Fatalf("conversations by bot = %v", byKey)
	}
	if asst.last() != "echo: again to the assistant bot" || h.conn.last() != "echo: to the system bot" {
		t.Errorf("replies went to the wrong bot: system %q, assistant %q", h.conn.last(), asst.last())
	}

	before := len(h.conn.messages())
	info := coretask.RunTerminalInfo{TaskID: "task1", ConversationID: byKey[assistantKey], SpaceID: personalID, Status: string(coretask.RunStatusCanceled)}
	h.g.ReportRunTerminal(context.Background(), info)
	if !strings.Contains(asst.last(), "was canceled") || len(h.conn.messages()) != before {
		t.Errorf("the report did not go through the conversation's bot: assistant %q", asst.last())
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
	_, b := withAssistantBot(t, h)
	in := dm("same id")
	in.EventID = "7"
	h.g.accept(h.system(), in)
	h.g.accept(b, in)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h.g.Wait(ctx)
	if n := len(h.turns.seen()); n != 2 {
		t.Errorf("turns = %d, want one per bot", n)
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

	asst, _ := withAssistantBot(t, h)
	waitReceiving(t, asst)
	want := []string{"channel-connector:telegram:" + assistantKey, "channel-connector:telegram:system"}
	if got := locker.keys(); !slices.Equal(got, want) {
		t.Errorf("leases = %v, want %v", got, want)
	}

	asst.inbox <- dm("through the running assistant bot")
	waitFor(t, func() bool { return asst.last() == "echo: through the running assistant bot" })

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
