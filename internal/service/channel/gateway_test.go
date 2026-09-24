package channel

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	corechannel "github.com/icloudbb/buildmax/internal/core/channel"
	"github.com/icloudbb/buildmax/internal/core/eligibility"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/util"
)

const (
	adaID      = "user_ada"
	adaChat    = "4242"
	personalID = "space_personal"
	teamID     = "space_team"
	portalURL  = "https://buildmax.example"
)

type harness struct {
	g     *Gateway
	conn  *fakeConnector
	ids   *fakeIdentities
	convs *fakeConversations
	elig  *fakeEligibility
	turns *fakeTurns
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		conn:  newFakeConnector(),
		ids:   newFakeIdentities(),
		convs: &fakeConversations{},
		elig:  &fakeEligibility{},
		turns: &fakeTurns{},
	}
	h.g = New(Config{
		Connectors:    []corechannel.Connector{h.conn},
		Identities:    h.ids,
		Conversations: h.convs,
		Spaces: fakeSpaces{byUser: map[string][]corespace.Space{adaID: {
			{ID: personalID, Name: "Ada's Space", PersonalForUserID: util.Ptr(adaID)},
			{ID: teamID, Name: "Team"},
		}}},
		Tasks:       fakeTasks{"task1": {ID: "task1", Title: "Summarize the logs"}},
		Eligibility: h.elig,
		PortalURL:   portalURL,
	})
	h.g.SetTurns(h.turns)
	return h
}

func dm(text string) corechannel.Inbound {
	return corechannel.Inbound{ChatID: adaChat, ChatType: corechannel.ChatPrivate, SenderID: adaChat, SenderHandle: "@ada", Text: text}
}

func (h *harness) send(in corechannel.Inbound) {
	h.g.handle(context.Background(), h.conn, in)
}

func TestNewReturnsNilWithoutConnectors(t *testing.T) {
	if New(Config{}) != nil {
		t.Fatal("a gateway with no connector should be nil so the server skips it")
	}
}

// An unlinked sender gets a link code and nothing else: no model runs.
func TestUnlinkedSenderIsOfferedAPairingCode(t *testing.T) {
	h := newHarness(t)
	h.send(dm("hello"))

	code := h.ids.onlyCode()
	if code == "" {
		t.Fatal("no pairing was stored")
	}
	reply := h.conn.last()
	if !strings.Contains(reply, portalURL+"/#/account/chat/"+displayCode(code)) {
		t.Errorf("reply lacks the confirm link: %q", reply)
	}
	if len(h.turns.seen()) != 0 {
		t.Error("an unlinked sender reached the model")
	}

	// A second message inside the throttle window writes no new code.
	h.send(dm("hello again"))
	if len(h.conn.messages()) != 1 || h.ids.onlyCode() != code {
		t.Error("a repeat message from an unlinked sender was answered with a new code")
	}
}

func TestConfirmPairingLinksAndTellsTheChat(t *testing.T) {
	h := newHarness(t)
	h.send(dm("hello"))
	code := h.ids.onlyCode()

	preview, err := h.g.PreviewPairing(context.Background(), strings.ToLower(displayCode(code)))
	if err != nil || preview.Handle != "@ada" {
		t.Fatalf("PreviewPairing = %+v, %v; want the chat account's handle", preview, err)
	}
	link, err := h.g.ConfirmPairing(context.Background(), adaID, displayCode(code))
	if err != nil {
		t.Fatalf("ConfirmPairing: %v", err)
	}
	if link.UserID != adaID || link.ExternalUserID != adaChat {
		t.Errorf("link = %+v", link)
	}
	if !strings.Contains(h.conn.last(), "Linked") {
		t.Errorf("the chat was not told: %q", h.conn.last())
	}
	if _, err := h.g.ConfirmPairing(context.Background(), adaID, code); !errors.Is(err, corechannel.ErrPairingNotFound) {
		t.Errorf("a spent code confirmed again: %v", err)
	}
}

func TestLinkedMessageRunsATurnInThePersonalSpace(t *testing.T) {
	h := newHarness(t)
	h.ids.link(adaID, adaChat)

	h.send(dm("what is running?"))
	h.send(dm("and now?"))

	convs := h.convs.all()
	if len(convs) != 1 {
		t.Fatalf("conversations = %d, want the second message to continue the first", len(convs))
	}
	c := convs[0]
	if c.SpaceID != personalID || c.UserID != adaID || c.Channel != corechannel.PlatformTelegram || c.ChannelRef != adaChat {
		t.Errorf("conversation = %+v", c)
	}
	calls := h.turns.seen()
	if len(calls) != 2 || calls[0] != c.ID+"|"+adaID+"|telegram|what is running?" {
		t.Errorf("turns = %q", calls)
	}
	if h.conn.last() != "echo: and now?" {
		t.Errorf("reply = %q", h.conn.last())
	}
}

// Group chats are refused before any lookup: replies there reach people who
// may not belong to the Space.
func TestGroupMessagesAreIgnored(t *testing.T) {
	h := newHarness(t)
	h.ids.link(adaID, adaChat)
	in := dm("hi")
	in.ChatType = corechannel.ChatGroup
	h.send(in)
	if len(h.conn.messages()) != 0 || len(h.turns.seen()) != 0 {
		t.Error("a group message was answered")
	}
}

func TestIneligibleSenderIsRefusedBeforeTheModel(t *testing.T) {
	h := newHarness(t)
	h.ids.link(adaID, adaChat)
	h.elig.refuse(adaID, personalID, eligibility.ErrAccountDisabled)
	h.send(dm("hi"))
	if len(h.turns.seen()) != 0 {
		t.Fatal("a disabled account reached the model")
	}
	if h.conn.last() != "Your BuildMax account is disabled." {
		t.Errorf("reply = %q", h.conn.last())
	}
}

func TestSpaceCommandListsAndSwitches(t *testing.T) {
	h := newHarness(t)
	h.ids.link(adaID, adaChat)

	h.send(dm("/space"))
	if list := h.conn.last(); !strings.Contains(list, "1. Ada's Space  ← current") || !strings.Contains(list, "2. Team") {
		t.Errorf("space list = %q", list)
	}
	h.send(dm("/space@buildmax_bot 2"))
	if !strings.Contains(h.conn.last(), "Switched to Team") {
		t.Errorf("switch reply = %q", h.conn.last())
	}
	h.send(dm("hi team"))
	convs := h.convs.all()
	if len(convs) != 1 || convs[0].SpaceID != teamID {
		t.Fatalf("conversations = %+v, want one in the team Space", convs)
	}

	h.elig.refuse(adaID, teamID, eligibility.ErrNotSpaceMember)
	h.send(dm("still there?"))
	if !strings.Contains(h.conn.last(), "no longer a member") {
		t.Errorf("reply after removal = %q", h.conn.last())
	}
}

func TestNewCommandStartsAFreshConversationInTheSameSpace(t *testing.T) {
	h := newHarness(t)
	h.ids.link(adaID, adaChat)
	h.send(dm("first"))
	h.send(dm("/new"))
	h.send(dm("second"))
	convs := h.convs.all()
	if len(convs) != 2 || convs[1].SpaceID != personalID {
		t.Fatalf("conversations = %+v", convs)
	}
	if !strings.HasPrefix(h.turns.seen()[1], convs[1].ID+"|") {
		t.Error("the message after /new did not go to the new conversation")
	}
}

func TestTurnFailuresAreExplainedWithoutInternals(t *testing.T) {
	h := newHarness(t)
	h.ids.link(adaID, adaChat)

	h.turns.err = ErrBusy
	h.send(dm("hi"))
	if !strings.Contains(h.conn.last(), "earlier messages") {
		t.Errorf("busy reply = %q", h.conn.last())
	}
	h.turns.err = apierr.New(apierr.KindQuotaExceeded, "this space is over its monthly allowance")
	h.send(dm("hi"))
	if h.conn.last() != "this space is over its monthly allowance" {
		t.Errorf("quota reply = %q", h.conn.last())
	}
	h.turns.err = errors.New("dial tcp 10.0.0.7:3306: connection refused")
	h.send(dm("hi"))
	if strings.Contains(h.conn.last(), "10.0.0.7") || !strings.Contains(h.conn.last(), "/#/spaces/"+personalID+"/chat/") {
		t.Errorf("internal failure reply = %q", h.conn.last())
	}
}

// A chat's messages are answered in order, one at a time, and a redelivered
// event is dropped.
func TestAcceptSerializesAChatAndDropsRedeliveries(t *testing.T) {
	h := newHarness(t)
	h.ids.link(adaID, adaChat)
	h.turns.block = make(chan struct{})
	for i, text := range []string{"one", "two", "three"} {
		in := dm(text)
		in.EventID = string(rune('a' + i))
		h.g.accept(h.conn, in)
	}
	dup := dm("two")
	dup.EventID = "b"
	h.g.accept(h.conn, dup)
	close(h.turns.block)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h.g.Wait(ctx)

	var got []string
	for _, call := range h.turns.seen() {
		got = append(got, call[strings.LastIndex(call, "|")+1:])
	}
	if strings.Join(got, ",") != "one,two,three" {
		t.Errorf("turn order = %v, want one,two,three with the redelivery dropped", got)
	}
}

func TestReportRunTerminalTellsTheOriginatingChat(t *testing.T) {
	h := newHarness(t)
	h.ids.link(adaID, adaChat)
	h.send(dm("start the job"))
	conv := h.convs.all()[0]
	output := "All 3 services are healthy."
	info := coretask.RunTerminalInfo{TaskRunID: "run1", TaskID: "task1", ConversationID: conv.ID, SpaceID: personalID, UserID: adaID, Status: string(coretask.RunStatusSucceeded), Output: &output}

	before := len(h.conn.messages())
	h.g.ReportRunTerminal(context.Background(), info)
	msgs := h.conn.messages()
	if len(msgs) != before+1 {
		t.Fatalf("messages = %d, want one report", len(msgs))
	}
	report := msgs[len(msgs)-1]
	if report.ChatID != adaChat || !strings.Contains(report.Text, "Task “Summarize the logs” finished.") ||
		!strings.Contains(report.Text, output) || !strings.Contains(report.Text, portalURL+"/#/spaces/"+personalID+"/tasks/task1") {
		t.Errorf("report = %+v", report)
	}

	// No report once the owner can no longer see the Space, or has unlinked.
	h.elig.refuse(adaID, personalID, eligibility.ErrNotSpaceMember)
	h.g.ReportRunTerminal(context.Background(), info)
	h.elig.refuse(adaID, personalID, nil)
	links, _ := h.ids.ListIdentitiesByUser(context.Background(), adaID)
	_ = h.ids.DeleteIdentity(context.Background(), adaID, links[0].ID)
	h.g.ReportRunTerminal(context.Background(), info)
	// Nor for a run from a conversation no chat carries.
	info.ConversationID = "portal-conversation"
	h.g.ReportRunTerminal(context.Background(), info)
	if len(h.conn.messages()) != before+1 {
		t.Errorf("reports sent where none should be: %+v", h.conn.messages()[before+1:])
	}
}

func TestFormatReportForFailures(t *testing.T) {
	h := newHarness(t)
	msg := "worker exited"
	got := h.g.formatReport(coretask.RunTerminalInfo{TaskID: "t", SpaceID: "s", Status: string(coretask.RunStatusFailed), ErrorMessage: &msg}, "")
	if !strings.HasPrefix(got, "A task failed.\n\nworker exited") {
		t.Errorf("failure report = %q", got)
	}
	got = h.g.formatReport(coretask.RunTerminalInfo{TaskID: "t", SpaceID: "s", Status: string(coretask.RunStatusCanceled)}, "Deploy")
	if !strings.HasPrefix(got, "Task “Deploy” was canceled.") {
		t.Errorf("cancel report = %q", got)
	}
}

// Only the lease holder receives, and it stops when the lease is lost.
func TestConnectorReceivesOnlyWhileHoldingTheLease(t *testing.T) {
	h := newHarness(t)
	locker := &fakeLocker{grant: true}
	h.g.locker = locker
	h.g.Start()
	defer h.g.Stop()

	select {
	case <-h.conn.received:
	case <-time.After(5 * time.Second):
		t.Fatal("the lease holder never started receiving")
	}
	locker.mu.Lock()
	lease := locker.lease
	locker.mu.Unlock()
	close(lease.lost)
	select {
	case <-lease.released:
	case <-time.After(5 * time.Second):
		t.Fatal("receiving did not stop after the lease was lost")
	}
}

func TestNormalizePairingCode(t *testing.T) {
	if got := NormalizePairingCode(" abcd-efgh "); got != "ABCDEFGH" {
		t.Errorf("NormalizePairingCode = %q", got)
	}
	code, err := newPairingCode()
	if err != nil || len(code) != pairingLength || strings.ContainsAny(code, "01OIL") {
		t.Errorf("newPairingCode = %q, %v", code, err)
	}
}
