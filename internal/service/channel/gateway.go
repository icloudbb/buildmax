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
	"fmt"
	"log/slog"
	"sync"
	"time"

	corechannel "github.com/icloudbb/buildmax/internal/core/channel"
	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"
	"github.com/icloudbb/buildmax/internal/core/eligibility"
	coreidentity "github.com/icloudbb/buildmax/internal/core/identity"
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

// ErrBusy, ErrRestarting, and ErrUnavailable are what a TurnRunner returns when
// a turn cannot start now; they become a retry hint in the chat rather than a
// failure.
var (
	ErrBusy        = errors.New("too many messages are waiting in this conversation")
	ErrRestarting  = errors.New("the server is restarting")
	ErrUnavailable = errors.New("the server cannot take a turn right now")
)

// TurnRunner runs one Tier 1 Conversation turn and returns the reply. The
// server implements it with the turn queue and Conversation service Portal chat
// uses, so a chat turn is serialized with, and identical to, a Portal one.
type TurnRunner interface {
	RunChannelTurn(ctx context.Context, conversationID, userID, channel, message string) (string, error)
}

// FrontDoor answers linked people who message a bot other than the system
// bot, such as a Space Assistant's. The Gateway has already identified the
// sender and checked their sign-in; who may ask, and what is answered, is the
// front door's. It returns the reply to send, or "" for none.
type FrontDoor interface {
	Answer(ctx context.Context, connectorKey string, in corechannel.Inbound, userID string) string
}

// Conversations is the slice of Conversation storage the gateway needs.
type Conversations interface {
	GetConversation(ctx context.Context, conversationID string) (*coreconv.Conversation, error)
	LatestChatConversation(ctx context.Context, userID, channel, connector, channelRef string) (*coreconv.Conversation, error)
	CreateChatConversation(ctx context.Context, spaceID, userID, channel, connector, channelRef string) (*coreconv.Conversation, error)
}

// Users reads an account's last sign-in, which bounds how long its chat links act.
type Users interface {
	GetUser(ctx context.Context, userID string) (*coreidentity.User, error)
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
	// Connectors are the system bots from server configuration, registered
	// under corechannel.ConnectorSystem. Others are added with Register.
	Connectors    []corechannel.Connector
	Identities    corechannel.IdentityStore
	Conversations Conversations
	Spaces        Spaces
	Tasks         Tasks
	Eligibility   eligibility.Checker
	Users         Users
	// SignInWindow is how long after a user's last sign-in their chat links
	// keep acting (corechannel.ActiveUntil). Zero means the default session
	// lifetime, so a link never outlives what a login could.
	SignInWindow time.Duration
	// PortalURL is the public origin links in chat messages point at. Empty
	// leaves links out and tells people where to go in words instead.
	PortalURL string
	Locker    Locker
	Logger    *slog.Logger
}

// Gateway connects chat platforms to Conversations.
type Gateway struct {
	identities    corechannel.IdentityStore
	conversations Conversations
	spaces        Spaces
	tasks         Tasks
	eligible      eligibility.Checker
	users         Users
	signInWindow  time.Duration
	portalURL     string
	locker        Locker
	log           *slog.Logger
	now           func() time.Time

	turnsMu   sync.RWMutex
	turns     TurnRunner
	frontDoor FrontDoor

	mu       sync.Mutex
	bots     map[string]*registration
	order    []string
	chats    map[string]*chatQueue
	seen     map[string]time.Time
	offered  map[string]time.Time
	ctx      context.Context
	cancel   context.CancelFunc
	receiver sync.WaitGroup
	work     sync.WaitGroup
}

// bot is a connector together with the key it is registered under. Every
// message, conversation, and pairing carries the key, because one platform can
// have several bots and a person's chat id can be the same with each of them.
type bot struct {
	key string
	corechannel.Connector
}

// registration is one registered bot and, while the Gateway runs, the receive
// loop serving it.
type registration struct {
	bot
	stop context.CancelFunc
	done chan struct{}
}

type chatQueue struct {
	running bool
	pending []corechannel.Inbound
}

// New returns a Gateway serving the system connectors in cfg. It exists even
// without one, so bots registered later, such as a Space Assistant's, can be
// served by a deployment that configures no system bot.
func New(cfg Config) *Gateway {
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	window := cfg.SignInWindow
	if window <= 0 {
		window = coreidentity.SessionAbsoluteTTLDefault
	}
	g := &Gateway{
		identities:    cfg.Identities,
		conversations: cfg.Conversations,
		spaces:        cfg.Spaces,
		tasks:         cfg.Tasks,
		eligible:      cfg.Eligibility,
		users:         cfg.Users,
		signInWindow:  window,
		portalURL:     cfg.PortalURL,
		locker:        cfg.Locker,
		log:           log.With("component", "channel_gateway"),
		now:           time.Now,
		bots:          map[string]*registration{},
		chats:         map[string]*chatQueue{},
		seen:          map[string]time.Time{},
		offered:       map[string]time.Time{},
	}
	// Server configuration holds at most one bot per platform, so the system
	// key alone cannot collide: its key is the platform's system bot.
	for _, c := range cfg.Connectors {
		key := systemKey(c.Platform())
		g.bots[key] = &registration{bot: bot{key: corechannel.ConnectorSystem, Connector: c}}
		g.order = append(g.order, key)
	}
	return g
}

// systemKey is the registry key of a platform's system bot. Other bots are
// registered under their own globally unique key.
func systemKey(platform string) string {
	return corechannel.ConnectorSystem + ":" + platform
}

func registryKey(key, platform string) string {
	if key == "" || key == corechannel.ConnectorSystem {
		return systemKey(platform)
	}
	return key
}

// connector returns the bot registered under key on platform, or nil. An
// empty key is the system bot, which is what rows written before keys existed
// meant.
func (g *Gateway) connector(platform, key string) *bot {
	g.mu.Lock()
	defer g.mu.Unlock()
	r := g.bots[registryKey(key, platform)]
	if r == nil || r.Platform() != platform {
		return nil
	}
	b := r.bot
	return &b
}

// Register adds a bot under key and, if the Gateway is running, starts
// receiving for it. It refuses a key already in use and a bot BuildMax already
// receives for under another key.
func (g *Gateway) Register(ctx context.Context, key string, c corechannel.Connector) error {
	if key == "" || key == corechannel.ConnectorSystem {
		return fmt.Errorf("register chat connector: key %q is reserved", key)
	}
	id, err := c.BotID(ctx)
	if err != nil {
		return fmt.Errorf("register chat connector: identify bot: %w", err)
	}
	g.mu.Lock()
	others := make([]bot, 0, len(g.bots))
	_, taken := g.bots[key]
	for _, r := range g.bots {
		if r.Platform() == c.Platform() {
			others = append(others, r.bot)
		}
	}
	g.mu.Unlock()
	if taken {
		return fmt.Errorf("register chat connector: key %q is already registered", key)
	}
	// Asked outside the lock: a connector may call its platform to answer.
	for _, o := range others {
		otherID, err := o.BotID(ctx)
		if err != nil {
			return fmt.Errorf("register chat connector: identify bot %q: %w", o.key, err)
		}
		if otherID == id {
			return corechannel.ErrBotInUse
		}
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, taken := g.bots[key]; taken {
		return fmt.Errorf("register chat connector: key %q is already registered", key)
	}
	r := &registration{bot: bot{key: key, Connector: c}}
	g.bots[key] = r
	g.order = append(g.order, key)
	if g.ctx != nil {
		g.startLocked(r)
	}
	return nil
}

// BotInUse reports whether a bot with this platform id is registered under a
// key other than except, so a new binding can be refused before it is stored.
func (g *Gateway) BotInUse(ctx context.Context, platform, botID, except string) (bool, error) {
	g.mu.Lock()
	var others []bot
	for key, r := range g.bots {
		if key != except && r.Platform() == platform {
			others = append(others, r.bot)
		}
	}
	g.mu.Unlock()
	for _, o := range others {
		id, err := o.BotID(ctx)
		if err != nil {
			return false, fmt.Errorf("identify bot %q: %w", o.key, err)
		}
		if id == botID {
			return true, nil
		}
	}
	return false, nil
}

// Registered reports the keys of the bots registered besides the system bots.
func (g *Gateway) Registered() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []string
	for key, r := range g.bots {
		if r.key != corechannel.ConnectorSystem {
			out = append(out, key)
		}
	}
	return out
}

// Unregister stops receiving for the bot under key and removes it, waiting for
// its receive loop to return. Messages it already accepted are still answered.
// Unknown keys are ignored.
func (g *Gateway) Unregister(key string) {
	g.mu.Lock()
	r := g.bots[key]
	if r == nil || r.key == corechannel.ConnectorSystem {
		g.mu.Unlock()
		return
	}
	delete(g.bots, key)
	for i, k := range g.order {
		if k == key {
			g.order = append(g.order[:i], g.order[i+1:]...)
			break
		}
	}
	stop, done := r.stop, r.done
	g.mu.Unlock()
	if stop != nil {
		stop()
		<-done
	}
}

// SetTurns wires the turn runner. It is set after construction because the
// runner is the server's handler, which is itself built with this gateway.
func (g *Gateway) SetTurns(t TurnRunner) {
	g.turnsMu.Lock()
	defer g.turnsMu.Unlock()
	g.turns = t
}

// SetFrontDoor wires what answers bots other than the system bot. Without one,
// such a bot answers linked people that it is not available.
func (g *Gateway) SetFrontDoor(f FrontDoor) {
	g.turnsMu.Lock()
	defer g.turnsMu.Unlock()
	g.frontDoor = f
}

func (g *Gateway) currentFrontDoor() FrontDoor {
	g.turnsMu.RLock()
	defer g.turnsMu.RUnlock()
	return g.frontDoor
}

func (g *Gateway) turnRunner() TurnRunner {
	g.turnsMu.RLock()
	defer g.turnsMu.RUnlock()
	return g.turns
}

// Start begins receiving on every registered bot, and on every bot registered
// later. Receiving stops at Stop; sending (replies, outcome reports, link
// confirmations) works whether or not this replica is the one receiving.
func (g *Gateway) Start() {
	if g == nil {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.ctx != nil {
		return
	}
	g.ctx, g.cancel = context.WithCancel(context.Background())
	for _, key := range g.order {
		g.startLocked(g.bots[key])
	}
}

func (g *Gateway) startLocked(r *registration) {
	ctx, stop := context.WithCancel(g.ctx)
	r.stop, r.done = stop, make(chan struct{})
	g.receiver.Add(1)
	go func() {
		defer g.receiver.Done()
		defer close(r.done)
		g.runConnector(ctx, r.bot)
	}()
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

func (g *Gateway) runConnector(ctx context.Context, c bot) {
	log := g.log.With("platform", c.Platform(), "connector", c.key)
	for ctx.Err() == nil {
		runCtx, release, ok := g.hold(ctx, c)
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

// hold takes the bot's receive lease, returning a context that ends when the
// lease is lost. Without a locker this replica is the only one and always holds.
func (g *Gateway) hold(ctx context.Context, c bot) (context.Context, func(), bool) {
	if g.locker == nil {
		return ctx, func() {}, true
	}
	lease, ok, err := g.locker.TryAcquire(ctx, "channel-connector:"+c.Platform()+":"+c.key)
	if err != nil {
		g.log.Warn("could not ask for the chat connector lease", "platform", c.Platform(), "connector", c.key, "err", err)
		return nil, nil, false
	}
	if !ok {
		return nil, nil, false
	}
	runCtx, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-lease.Lost():
			g.log.Warn("chat connector lease lost; stopping receive", "platform", c.Platform(), "connector", c.key)
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

// accept queues a delivered message behind any other from the same chat with
// the same bot, so a chat's messages are answered in order while different
// chats proceed at once.
func (g *Gateway) accept(c bot, in corechannel.Inbound) {
	key := c.Platform() + "\x00" + c.key + "\x00" + in.Tenant + "\x00" + in.ChatID
	g.mu.Lock()
	if g.duplicateLocked(c, in.EventID) {
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
			g.log.Warn("chat queue full; dropping message", "platform", c.Platform(), "connector", c.key)
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

func (g *Gateway) drainChat(c bot, key string, q *chatQueue, in corechannel.Inbound) {
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
// Event ids are per bot. Only one replica receives for a bot, so memory is
// enough; a handoff can still redeliver, which the connector narrows by
// confirming what it took.
func (g *Gateway) duplicateLocked(c bot, eventID string) bool {
	if eventID == "" {
		return false
	}
	now := g.now()
	id := c.Platform() + "\x00" + c.key + "\x00" + eventID
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
