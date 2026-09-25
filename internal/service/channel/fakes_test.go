package channel

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	corechannel "github.com/icloudbb/buildmax/internal/core/channel"
	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"
	"github.com/icloudbb/buildmax/internal/core/eligibility"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

// fakeConnector records what the gateway sends and lets a test push messages.
type fakeConnector struct {
	mu       sync.Mutex
	sent     []corechannel.Outbound
	inbox    chan corechannel.Inbound
	received chan struct{}
}

func newFakeConnector() *fakeConnector {
	return &fakeConnector{inbox: make(chan corechannel.Inbound, 16), received: make(chan struct{}, 16)}
}

func (f *fakeConnector) Platform() string { return corechannel.PlatformTelegram }

func (f *fakeConnector) Receive(ctx context.Context, deliver func(corechannel.Inbound)) error {
	f.received <- struct{}{}
	for {
		select {
		case <-ctx.Done():
			return nil
		case in := <-f.inbox:
			deliver(in)
		}
	}
}

func (f *fakeConnector) Send(_ context.Context, msg corechannel.Outbound) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, msg)
	return nil
}

func (f *fakeConnector) Typing(context.Context, string) error { return nil }

func (f *fakeConnector) Info(context.Context) corechannel.Info {
	return corechannel.Info{Platform: corechannel.PlatformTelegram, Name: "Telegram", BotHandle: "@bot"}
}

func (f *fakeConnector) messages() []corechannel.Outbound {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]corechannel.Outbound(nil), f.sent...)
}

func (f *fakeConnector) last() string {
	m := f.messages()
	if len(m) == 0 {
		return ""
	}
	return m[len(m)-1].Text
}

// fakeIdentities is an in-memory corechannel.IdentityStore.
type fakeIdentities struct {
	mu       sync.Mutex
	links    []corechannel.Identity
	pairings map[string]corechannel.Pairing
	nextID   int
}

func newFakeIdentities() *fakeIdentities {
	return &fakeIdentities{pairings: map[string]corechannel.Pairing{}}
}

func (f *fakeIdentities) link(userID, externalID string) corechannel.Identity {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	id := corechannel.Identity{ID: fmt.Sprintf("link%d", f.nextID), UserID: userID, Platform: corechannel.PlatformTelegram, ExternalUserID: externalID}
	f.links = append(f.links, id)
	return id
}

func (f *fakeIdentities) IdentityByExternal(_ context.Context, platform, tenant, externalUserID string) (*corechannel.Identity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, l := range f.links {
		if l.Platform == platform && l.Tenant == tenant && l.ExternalUserID == externalUserID {
			return &l, nil
		}
	}
	return nil, nil
}

func (f *fakeIdentities) ListIdentitiesByUser(_ context.Context, userID string) ([]corechannel.Identity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []corechannel.Identity
	for _, l := range f.links {
		if l.UserID == userID {
			out = append(out, l)
		}
	}
	return out, nil
}

func (f *fakeIdentities) DeleteIdentity(_ context.Context, userID, identityID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, l := range f.links {
		if l.ID == identityID && l.UserID == userID {
			f.links = append(f.links[:i], f.links[i+1:]...)
			return nil
		}
	}
	return apierr.ErrNotFound
}

func (f *fakeIdentities) CreatePairing(_ context.Context, p corechannel.Pairing, code string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k, old := range f.pairings {
		if old.Platform == p.Platform && old.ExternalUserID == p.ExternalUserID {
			delete(f.pairings, k)
		}
	}
	f.pairings[code] = p
	return nil
}

func (f *fakeIdentities) PairingByCode(_ context.Context, code string, now time.Time) (*corechannel.Pairing, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.pairings[code]
	if !ok || !now.Before(p.ExpiresAt) {
		return nil, nil
	}
	return &p, nil
}

func (f *fakeIdentities) ConsumePairing(ctx context.Context, code, userID string, now time.Time) (*corechannel.Identity, *corechannel.Pairing, error) {
	p, _ := f.PairingByCode(ctx, code, now)
	if p == nil {
		return nil, nil, corechannel.ErrPairingNotFound
	}
	if existing, _ := f.IdentityByExternal(ctx, p.Platform, p.Tenant, p.ExternalUserID); existing != nil {
		if existing.UserID != userID {
			return nil, nil, corechannel.ErrAlreadyLinked
		}
		return existing, p, nil
	}
	f.mu.Lock()
	delete(f.pairings, code)
	f.mu.Unlock()
	id := f.link(userID, p.ExternalUserID)
	return &id, p, nil
}

func (f *fakeIdentities) onlyCode() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for code := range f.pairings {
		return code
	}
	return ""
}

// fakeConversations stores chat conversations in memory.
type fakeConversations struct {
	mu    sync.Mutex
	convs []coreconv.Conversation
}

func (f *fakeConversations) GetConversation(_ context.Context, id string) (*coreconv.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.convs {
		if c.ID == id {
			return &c, nil
		}
	}
	return nil, nil
}

func (f *fakeConversations) LatestChatConversation(_ context.Context, userID, channel, ref string) (*coreconv.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.convs) - 1; i >= 0; i-- {
		c := f.convs[i]
		if c.UserID == userID && c.Channel == channel && c.ChannelRef == ref {
			return &c, nil
		}
	}
	return nil, nil
}

func (f *fakeConversations) CreateChatConversation(_ context.Context, spaceID, userID, channel, ref string) (*coreconv.Conversation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := coreconv.Conversation{ID: fmt.Sprintf("conv%d", len(f.convs)+1), SpaceID: spaceID, UserID: userID, Channel: channel, ChannelRef: ref, CreatedBy: userID}
	f.convs = append(f.convs, c)
	return &c, nil
}

func (f *fakeConversations) all() []coreconv.Conversation {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]coreconv.Conversation(nil), f.convs...)
}

type fakeSpaces struct{ byUser map[string][]corespace.Space }

func (f fakeSpaces) ListSpacesByUser(_ context.Context, userID string) ([]corespace.Space, error) {
	return f.byUser[userID], nil
}

type fakeTasks map[string]coretask.Task

func (f fakeTasks) GetTask(_ context.Context, id string) (*coretask.Task, error) {
	if t, ok := f[id]; ok {
		return &t, nil
	}
	return nil, nil
}

// fakeEligibility refuses the (user, space) pairs a test lists.
type fakeEligibility struct {
	mu      sync.Mutex
	refused map[string]error
}

func (f *fakeEligibility) Check(_ context.Context, userID, spaceID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refused[userID+"/"+spaceID]
}

func (f *fakeEligibility) refuse(userID, spaceID string, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refused == nil {
		f.refused = map[string]error{}
	}
	f.refused[userID+"/"+spaceID] = err
}

var _ eligibility.Checker = (*fakeEligibility)(nil)

// fakeTurns answers every turn by echoing it and records the calls.
type fakeTurns struct {
	mu    sync.Mutex
	calls []string
	err   error
	block chan struct{}
}

func (f *fakeTurns) RunChannelTurn(_ context.Context, conversationID, userID, channel, message string) (string, error) {
	if f.block != nil {
		<-f.block
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, strings.Join([]string{conversationID, userID, channel, message}, "|"))
	if f.err != nil {
		return "", f.err
	}
	return "echo: " + message, nil
}

func (f *fakeTurns) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// fakeLocker grants or withholds the connector lease.
type fakeLocker struct {
	mu    sync.Mutex
	grant bool
	lease *fakeLease
}

type fakeLease struct {
	lost     chan struct{}
	released chan struct{}
	once     sync.Once
}

func (l *fakeLease) Lost() <-chan struct{} { return l.lost }
func (l *fakeLease) Release()              { l.once.Do(func() { close(l.released) }) }

func (f *fakeLocker) TryAcquire(context.Context, string) (Lease, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.grant {
		return nil, false, nil
	}
	f.grant = false
	f.lease = &fakeLease{lost: make(chan struct{}), released: make(chan struct{})}
	return f.lease, true, nil
}
