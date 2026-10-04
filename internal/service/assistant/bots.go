package assistant

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	coreassistant "github.com/icloudbb/buildmax/internal/core/assistant"
	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	corechannel "github.com/icloudbb/buildmax/internal/core/channel"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
)

// ErrNoEncryptionKey refuses storing a bot token on a deployment that has no
// key to seal it with; it is never stored in the clear.
var ErrNoEncryptionKey = apierr.New(apierr.KindNotConfigured,
	"binding a bot needs a deployment encryption key (secret.kek_file)")

// Gateway is the chat Gateway's surface for bots registered at runtime.
type Gateway interface {
	Register(ctx context.Context, key string, c corechannel.Connector) error
	Unregister(key string)
	Registered() []string
	BotInUse(ctx context.Context, platform, botID, except string) (bool, error)
	Send(ctx context.Context, platform, connectorKey, chatID, text string) error
}

// ConnectorFactory builds the connector that speaks as a bot from its token.
// Injected, because the platform adapters are infrastructure.
type ConnectorFactory func(platform, token string) (corechannel.Connector, error)

// BindCmd gives an Assistant its bot.
type BindCmd struct {
	SpaceID     string
	ActorID     string
	AssistantID string
	Platform    string
	Token       string
}

// Bind checks the token with the platform, refuses a bot BuildMax already
// serves, stores the binding with its token sealed, and connects the bot.
func (s *Service) Bind(ctx context.Context, cmd BindCmd) (*View, error) {
	a, err := s.load(ctx, cmd.SpaceID, cmd.AssistantID)
	if err != nil {
		return nil, err
	}
	if s.Bots == nil {
		return nil, ErrNotConfigured
	}
	if cmd.Platform != corechannel.PlatformTelegram {
		return nil, coreassistant.ErrUnsupportedPlatform
	}
	token := strings.TrimSpace(cmd.Token)
	if token == "" {
		return nil, coreassistant.ErrInvalidBotToken
	}
	existing, err := s.Store.GetBindingByAssistant(ctx, a.ID)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, coreassistant.ErrAlreadyBound
	}
	c, err := s.Bots.connect(cmd.Platform, token)
	if err != nil {
		return nil, err
	}
	botID, err := c.BotID(ctx)
	if err != nil {
		// The platform's error can quote the request; the token stays out of
		// what the caller and the log see.
		slog.Warn("assistant bot token not accepted", "assistant_id", a.ID, "platform", cmd.Platform)
		return nil, coreassistant.ErrInvalidBotToken
	}
	if inUse, err := s.Bots.Gateway.BotInUse(ctx, cmd.Platform, botID, ""); err != nil {
		return nil, err
	} else if inUse {
		return nil, corechannel.ErrBotInUse
	}
	handle := c.Info(ctx).BotHandle
	b, err := s.Store.CreateBinding(ctx, coreassistant.NewBinding{
		AssistantID: a.ID, Platform: cmd.Platform, BotID: botID, BotHandle: handle,
		Token: token, CreatedBy: cmd.ActorID,
	})
	if errors.Is(err, coregw.ErrCredentialEncryptionUnavailable) {
		return nil, ErrNoEncryptionKey
	}
	if err != nil {
		return nil, err
	}
	s.Audit.UserAction(ctx, cmd.ActorID, cmd.SpaceID, coreaudit.AssistantBound, "assistant", a.ID, b.BotHandle)
	s.Bots.SyncNow(ctx)
	return s.Get(ctx, cmd.SpaceID, a.ID)
}

// Unbind disconnects and forgets an Assistant's bot.
func (s *Service) Unbind(ctx context.Context, spaceID, actorID, assistantID string) (*View, error) {
	a, err := s.load(ctx, spaceID, assistantID)
	if err != nil {
		return nil, err
	}
	if err := s.Store.DeleteBindingByAssistant(ctx, a.ID); err != nil {
		return nil, err
	}
	s.Audit.UserAction(ctx, actorID, spaceID, coreaudit.AssistantUnbound, "assistant", a.ID, a.Def.Name)
	s.Bots.SyncNow(ctx)
	return s.Get(ctx, spaceID, a.ID)
}

// defaultSyncInterval is how soon a binding made on another replica is served
// here, and how often a bot that failed to connect is retried.
const defaultSyncInterval = 15 * time.Second

// Reconciler keeps the Gateway's registered bots in step with the stored
// bindings. Every replica runs one; the Gateway's receive lease lets exactly
// one replica receive for each bot, and every replica can send as it.
type Reconciler struct {
	Store    coreassistant.Store
	Gateway  Gateway
	Connect  ConnectorFactory
	Interval time.Duration
	Log      *slog.Logger

	// syncMu serializes passes; mu guards the loop's channels, so a Trigger
	// never waits behind a pass that is calling a platform.
	syncMu  sync.Mutex
	mu      sync.Mutex
	trigger chan struct{}
	stop    chan struct{}
	done    chan struct{}
}

func (r *Reconciler) connect(platform, token string) (corechannel.Connector, error) {
	if r.Connect == nil {
		return nil, ErrNotConfigured
	}
	c, err := r.Connect(platform, token)
	if err != nil {
		return nil, coreassistant.ErrUnsupportedPlatform
	}
	return c, nil
}

func (r *Reconciler) log() *slog.Logger {
	if r.Log != nil {
		return r.Log
	}
	return slog.Default()
}

// Start runs Sync now and then on every interval or Trigger until Stop.
func (r *Reconciler) Start() {
	if r == nil {
		return
	}
	r.mu.Lock()
	if r.stop != nil {
		r.mu.Unlock()
		return
	}
	r.trigger, r.stop, r.done = make(chan struct{}, 1), make(chan struct{}), make(chan struct{})
	r.mu.Unlock()
	interval := r.Interval
	if interval <= 0 {
		interval = defaultSyncInterval
	}
	go func() {
		defer close(r.done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			r.Sync(ctx)
			cancel()
			select {
			case <-r.stop:
				return
			case <-ticker.C:
			case <-r.trigger:
			}
		}
	}()
}

// Stop ends the loop and waits for it. Registered bots stay registered: the
// Gateway's own Stop ends their receive loops.
func (r *Reconciler) Stop() {
	if r == nil {
		return
	}
	r.mu.Lock()
	stop, done := r.stop, r.done
	r.mu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	<-done
}

// Trigger asks the loop to sync soon, without waiting.
func (r *Reconciler) Trigger() {
	if r == nil {
		return
	}
	r.mu.Lock()
	t := r.trigger
	r.mu.Unlock()
	if t == nil {
		return
	}
	select {
	case t <- struct{}{}:
	default:
	}
}

// SyncNow syncs on the caller's goroutine, so the replica that changed a
// binding serves it before answering the request.
func (r *Reconciler) SyncNow(ctx context.Context) {
	if r == nil {
		return
	}
	r.Sync(ctx)
}

// Sync registers the bot of every stored binding the Gateway lacks and removes
// every registered bot whose binding is gone. A bot that cannot connect, such
// as one whose token was revoked, is logged and retried on the next pass.
func (r *Reconciler) Sync(ctx context.Context) {
	if r == nil || r.Store == nil || r.Gateway == nil {
		return
	}
	r.syncMu.Lock()
	defer r.syncMu.Unlock()
	bindings, err := r.Store.ListBindings(ctx)
	if err != nil {
		r.log().Warn("assistant bots not synced", "err", err)
		return
	}
	want := make(map[string]coreassistant.Binding, len(bindings))
	for _, b := range bindings {
		want[b.ID] = b
	}
	for _, key := range r.Gateway.Registered() {
		if _, ok := want[key]; !ok {
			r.Gateway.Unregister(key)
		}
	}
	have := map[string]bool{}
	for _, key := range r.Gateway.Registered() {
		have[key] = true
	}
	for id, b := range want {
		if have[id] {
			continue
		}
		token, err := r.Store.BindingToken(ctx, id)
		if err != nil {
			r.log().Warn("assistant bot token not readable", "binding_id", id, "err", err)
			continue
		}
		c, err := r.connect(b.Platform, token)
		if err != nil {
			r.log().Warn("assistant bot not connected", "binding_id", id, "err", err)
			continue
		}
		if err := r.Gateway.Register(ctx, id, c); err != nil {
			level := slog.LevelWarn
			if errors.Is(err, corechannel.ErrBotInUse) {
				level = slog.LevelError
			}
			r.log().Log(ctx, level, "assistant bot not connected", "binding_id", id, "assistant_id", b.AssistantID, "err", err)
		}
	}
}
