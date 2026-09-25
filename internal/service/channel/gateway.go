// Package channel carries instant-messaging chats into BuildMax. A platform
// Connector delivers messages; the Gateway decides, before any model runs, who
// the sender is and which Space they may use, then hands the message to the
// same Tier 1 Conversation turn Portal chat uses and sends the reply back. It
// also reports a Task's outcome to the chat that started it.
//
// Everything here is platform-neutral. Adding a platform is adding a
// Connector. See docs/design/instant-messaging-channels.md.
package channel

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	corechannel "github.com/icloudbb/buildmax/internal/core/channel"
	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"
	"github.com/icloudbb/buildmax/internal/core/eligibility"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

const (
	// turnTimeout bounds one message's handling, model turn included, so a hung
	// provider cannot hold a chat's queue forever.
	turnTimeout = 10 * time.Minute
	// maxPendingPerChat caps messages waiting behind the one being answered.
	maxPendingPerChat = 10
	// standbyRetry is how often a replica not holding a connector's lease asks
	// for it again.
	standbyRetry = 10 * time.Second
	// failureRetry is the pause after a connector gave up, such as on a rejected
	// token, so a misconfiguration logs once a minute rather than spinning.
	failureRetry = time.Minute
	// typingInterval refreshes the typing indicator, which platforms show for a
	// few seconds per request.
	typingInterval = 4 * time.Second
	// pairingThrottle limits how often one unlinked chat account is sent a new
	// link code, so strangers messaging the bot cannot turn it into a writer of
	// database rows.
	pairingThrottle = 30 * time.Second
	// seenWindow is how long an event id is remembered to drop a redelivery.
	seenWindow = 10 * time.Minute
)

// ErrBusy and ErrRestarting are what a TurnRunner returns when a turn cannot
// start now; they become a retry hint in the chat rather than a failure.
var (
	ErrBusy       = errors.New("too many messages are waiting in this conversation")
	ErrRestarting = errors.New("the server is restarting")
)

// TurnRunner runs one Tier 1 Conversation turn and returns the reply. The
// server implements it with the turn queue and Conversation service Portal chat
// uses, so a chat turn is serialized with, and identical to, a Portal one.
type TurnRunner interface {
	RunChannelTurn(ctx context.Context, conversationID, userID, channel, message string) (string, error)
}

// Conversations is the slice of Conversation storage the gateway needs.
type Conversations interface {
	GetConversation(ctx context.Context, conversationID string) (*coreconv.Conversation, error)
	LatestChatConversation(ctx context.Context, userID, channel, channelRef string) (*coreconv.Conversation, error)
	CreateChatConversation(ctx context.Context, spaceID, userID, channel, channelRef string) (*coreconv.Conversation, error)
}

// Spaces lists the Spaces a user belongs to.
type Spaces interface {
	ListSpacesByUser(ctx context.Context, userID string) ([]corespace.Space, error)
}

// Tasks reads a Task to name it in an outcome report.
type Tasks interface {
	GetTask(ctx context.Context, taskID string) (*coretask.Task, error)
}

// Locker grants the exclusive right to receive for one connector across
// replicas. Nil means a single replica, which needs no lease.
type Locker interface {
	TryAcquire(ctx context.Context, key string) (Lease, bool, error)
}

// Lease is a held connector lease.
type Lease interface {
	Lost() <-chan struct{}
	Release()
}

// Config assembles a Gateway.
type Config struct {
	Connectors    []corechannel.Connector
	Identities    corechannel.IdentityStore
	Conversations Conversations
	Spaces        Spaces
	Tasks         Tasks
	Eligibility   eligibility.Checker
	// PortalURL is the public origin links in chat messages point at. Empty
	// leaves links out and tells people where to go in words instead.
	PortalURL string
	Locker    Locker
	Logger    *slog.Logger
}

// Gateway connects chat platforms to Conversations.
type Gateway struct {
	connectors    map[string]corechannel.Connector
	order         []string
	identities    corechannel.IdentityStore
	conversations Conversations
	spaces        Spaces
	tasks         Tasks
	eligible      eligibility.Checker
	portalURL     string
	locker        Locker
	log           *slog.Logger
	now           func() time.Time

	turnsMu sync.RWMutex
	turns   TurnRunner

	mu       sync.Mutex
	chats    map[string]*chatQueue
	seen     map[string]time.Time
	offered  map[string]time.Time
	cancel   context.CancelFunc
	started  bool
	receiver sync.WaitGroup
	work     sync.WaitGroup
}

type chatQueue struct {
	running bool
	pending []corechannel.Inbound
}

// New returns a Gateway, or nil when no connector is configured.
func New(cfg Config) *Gateway {
	if len(cfg.Connectors) == 0 {
		return nil
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	g := &Gateway{
		connectors:    make(map[string]corechannel.Connector, len(cfg.Connectors)),
		identities:    cfg.Identities,
		conversations: cfg.Conversations,
		spaces:        cfg.Spaces,
		tasks:         cfg.Tasks,
		eligible:      cfg.Eligibility,
		portalURL:     cfg.PortalURL,
		locker:        cfg.Locker,
		log:           log.With("component", "channel_gateway"),
		now:           time.Now,
		chats:         map[string]*chatQueue{},
		seen:          map[string]time.Time{},
		offered:       map[string]time.Time{},
	}
	for _, c := range cfg.Connectors {
		g.connectors[c.Platform()] = c
		g.order = append(g.order, c.Platform())
	}
	return g
}

// SetTurns wires the turn runner. It is set after construction because the
// runner is the server's handler, which is itself built with this gateway.
func (g *Gateway) SetTurns(t TurnRunner) {
	g.turnsMu.Lock()
	defer g.turnsMu.Unlock()
	g.turns = t
}

func (g *Gateway) turnRunner() TurnRunner {
	g.turnsMu.RLock()
	defer g.turnsMu.RUnlock()
	return g.turns
}

// Start begins receiving on every connector. Receiving stops at Stop; sending
// (replies, outcome reports, link confirmations) works whether or not this
// replica is the one receiving.
func (g *Gateway) Start() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.started {
		return
	}
	g.started = true
	ctx, cancel := context.WithCancel(context.Background())
	g.cancel = cancel
	for _, platform := range g.order {
		c := g.connectors[platform]
		g.receiver.Add(1)
		go g.runConnector(ctx, c)
	}
}

// Stop ends receiving and waits for the receivers to return. Messages already
// accepted are still answered; Wait waits for them.
func (g *Gateway) Stop() {
	if g == nil {
		return
	}
	g.mu.Lock()
	cancel := g.cancel
	g.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	g.receiver.Wait()
}

// Wait blocks until accepted messages have been answered or ctx ends.
func (g *Gateway) Wait(ctx context.Context) {
	if g == nil {
		return
	}
	done := make(chan struct{})
	go func() {
		g.work.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		g.log.Warn("chat messages were still being answered at shutdown")
	}
}

func (g *Gateway) runConnector(ctx context.Context, c corechannel.Connector) {
	defer g.receiver.Done()
	log := g.log.With("platform", c.Platform())
	for ctx.Err() == nil {
		runCtx, release, ok := g.hold(ctx, c.Platform())
		if !ok {
			sleep(ctx, standbyRetry)
			continue
		}
		log.Info("receiving chat messages")
		err := c.Receive(runCtx, func(in corechannel.Inbound) { g.accept(c, in) })
		release()
		if err != nil && ctx.Err() == nil {
			log.Error("chat connector stopped; retrying later", "err", err, "retry_in", failureRetry)
			sleep(ctx, failureRetry)
		}
	}
}

// hold takes the connector's lease, returning a context that ends when the
// lease is lost. Without a locker this replica is the only one and always holds.
func (g *Gateway) hold(ctx context.Context, platform string) (context.Context, func(), bool) {
	if g.locker == nil {
		return ctx, func() {}, true
	}
	lease, ok, err := g.locker.TryAcquire(ctx, "channel-connector:"+platform)
	if err != nil {
		g.log.Warn("could not ask for the chat connector lease", "platform", platform, "err", err)
		return nil, nil, false
	}
	if !ok {
		return nil, nil, false
	}
	runCtx, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-lease.Lost():
			g.log.Warn("chat connector lease lost; stopping receive", "platform", platform)
			cancel()
		case <-runCtx.Done():
		}
	}()
	return runCtx, func() { cancel(); lease.Release() }, true
}

func sleep(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

// accept queues a delivered message behind any other from the same chat, so a
// chat's messages are answered in order while different chats proceed at once.
func (g *Gateway) accept(c corechannel.Connector, in corechannel.Inbound) {
	key := c.Platform() + "\x00" + in.Tenant + "\x00" + in.ChatID
	g.mu.Lock()
	if g.duplicateLocked(c.Platform(), in.EventID) {
		g.mu.Unlock()
		return
	}
	q := g.chats[key]
	if q == nil {
		q = &chatQueue{}
		g.chats[key] = q
	}
	if q.running {
		if len(q.pending) >= maxPendingPerChat {
			g.mu.Unlock()
			g.log.Warn("chat queue full; dropping message", "platform", c.Platform())
			return
		}
		q.pending = append(q.pending, in)
		g.mu.Unlock()
		return
	}
	q.running = true
	g.work.Add(1)
	g.mu.Unlock()
	go g.drainChat(c, key, q, in)
}

func (g *Gateway) drainChat(c corechannel.Connector, key string, q *chatQueue, in corechannel.Inbound) {
	defer g.work.Done()
	for {
		ctx, cancel := context.WithTimeout(context.Background(), turnTimeout)
		g.handle(ctx, c, in)
		cancel()
		g.mu.Lock()
		if len(q.pending) == 0 {
			q.running = false
			delete(g.chats, key)
			g.mu.Unlock()
			return
		}
		in, q.pending = q.pending[0], q.pending[1:]
		g.mu.Unlock()
	}
}

// duplicateLocked records an event id and reports whether it was already seen.
// Only one replica receives for a connector, so memory is enough; a handoff can
// still redeliver, which the connector narrows by confirming what it took.
func (g *Gateway) duplicateLocked(platform, eventID string) bool {
	if eventID == "" {
		return false
	}
	now := g.now()
	id := platform + "\x00" + eventID
	if at, ok := g.seen[id]; ok && now.Sub(at) < seenWindow {
		return true
	}
	if len(g.seen) > 10000 {
		for k, at := range g.seen {
			if now.Sub(at) >= seenWindow {
				delete(g.seen, k)
			}
		}
	}
	g.seen[id] = now
	return false
}

// reply sends text to the message's chat, logging rather than returning a
// failure: there is nobody else to tell.
func (g *Gateway) reply(ctx context.Context, c corechannel.Connector, chatID, text string) {
	if text == "" {
		return
	}
	if err := c.Send(ctx, corechannel.Outbound{ChatID: chatID, Text: text}); err != nil {
		g.log.Warn("chat reply not delivered", "platform", c.Platform(), "err", err)
	}
}

// typing keeps a typing indicator up until the returned stop is called.
func (g *Gateway) typing(ctx context.Context, c corechannel.Connector, chatID string) func() {
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_ = c.Typing(ctx, chatID)
			select {
			case <-ctx.Done():
				return
			case <-time.After(typingInterval):
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}
