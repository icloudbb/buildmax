// Package channel is the platform-neutral contract between BuildMax and an
// instant-messaging platform: who a chat account is, how a platform delivers a
// message, and how a reply goes back. A platform adapter implements Connector;
// everything above it — pairing, authorization, turn delivery, outcome reports —
// is written once against these types. See
// docs/design/instant-messaging-channels.md.
package channel

import (
	"context"
	"time"

	"github.com/icloudbb/buildmax/internal/core/apierr"
)

// PlatformTelegram names the Telegram Bot API connector. A platform name is also
// the Conversation channel of every conversation that platform carries.
const PlatformTelegram = "telegram"

// PairingTTL bounds how long a link code the bot hands out stays redeemable.
// Short, because the code travels through a chat client the server does not
// control, and a person linking is at the keyboard anyway.
const PairingTTL = 10 * time.Minute

// ErrAlreadyLinked refuses linking a chat account that already speaks for a
// different BuildMax account. The existing link has to be removed first, so a
// chat account never silently changes whose authority it carries.
var ErrAlreadyLinked = apierr.New(apierr.KindConflict, "this chat account is already linked to another BuildMax account; unlink it there first")

// ErrPairingNotFound covers a wrong, spent, and expired code alike: the three
// are indistinguishable to someone guessing, which is the point.
var ErrPairingNotFound = apierr.New(apierr.KindNotFound, "link code not found or expired; send the bot a new message for a fresh one")

// Identity links one account on a chat platform to one BuildMax user. It is not
// a sign-in credential: nothing authenticates to the Portal with it. It only
// says whose authority a message from that chat account carries.
type Identity struct {
	ID       string
	UserID   string
	Platform string
	// Tenant scopes ExternalUserID on platforms whose user ids are per
	// organization or per app. Empty on platforms with global ids (Telegram).
	Tenant string
	// ExternalUserID is the platform's immutable id for the account, never a
	// display name, which the account holder can change to anything.
	ExternalUserID string
	// Handle is the display name at link time, shown so a person can recognize
	// which chat account a link belongs to. Never used for lookup.
	Handle    string
	CreatedAt time.Time
}

// Pairing is a link request a chat account started and a signed-in BuildMax
// user has not confirmed yet.
type Pairing struct {
	Platform       string
	Tenant         string
	ExternalUserID string
	// ChatID is where the bot tells the person the link succeeded.
	ChatID    string
	Handle    string
	ExpiresAt time.Time
}

// IdentityStore persists chat-account links and pending pairings.
type IdentityStore interface {
	// IdentityByExternal returns the link for a chat account, or (nil, nil).
	IdentityByExternal(ctx context.Context, platform, tenant, externalUserID string) (*Identity, error)
	ListIdentitiesByUser(ctx context.Context, userID string) ([]Identity, error)
	// DeleteIdentity removes one of userID's links. apierr.ErrNotFound when the
	// link does not exist or belongs to someone else.
	DeleteIdentity(ctx context.Context, userID, identityID string) error
	// CreatePairing stores a pending link under the hash of code, replacing any
	// earlier pending link from the same chat account.
	CreatePairing(ctx context.Context, p Pairing, code string) error
	// PairingByCode returns the pending link a code names, or (nil, nil) when it
	// is unknown or expired at now.
	PairingByCode(ctx context.Context, code string, now time.Time) (*Pairing, error)
	// ConsumePairing spends code and links its chat account to userID in one
	// transaction. ErrPairingNotFound for a wrong or expired code,
	// ErrAlreadyLinked when the chat account is linked to someone else. Linking
	// an account already linked to userID succeeds and returns that link.
	ConsumePairing(ctx context.Context, code, userID string, now time.Time) (*Identity, *Pairing, error)
}

// Chat types an adapter reports. Only private chats are served; a group is
// named so an adapter can say what it saw and the gateway can decline it.
const (
	ChatPrivate = "private"
	ChatGroup   = "group"
)

// Inbound is one platform message, normalized.
type Inbound struct {
	// EventID is the platform's id for this delivery, used to drop a
	// redelivered message. Platforms deliver at least once.
	EventID  string
	Tenant   string
	ChatID   string
	ChatType string
	SenderID string
	// SenderHandle is the sender's display name, for pairing only.
	SenderHandle string
	Text         string
	// Unsupported is set for a message with no text (a photo, a sticker), which
	// the gateway answers rather than ignores.
	Unsupported bool
}

// Outbound is one message to a chat.
type Outbound struct {
	ChatID string
	Text   string
}

// Info describes a configured connector for the people linking to it.
type Info struct {
	Platform string `json:"platform"`
	// Name is the platform's display name.
	Name string `json:"name"`
	// BotHandle and BotURL say where to find the bot. Empty when the platform
	// has not answered yet.
	BotHandle string `json:"bot_handle,omitempty"`
	BotURL    string `json:"bot_url,omitempty"`
}

// Connector is one bot credential on one platform. Receive runs only on the
// replica holding the connector's lease, because platforms that work without a
// public URL deliver each message to one consumer. Send, Typing, and Info run
// on any replica.
type Connector interface {
	Platform() string
	// Receive delivers messages until ctx ends. It handles transient platform
	// errors itself and returns early only when retrying cannot help, such as a
	// rejected credential. deliver must return promptly; the connector does not
	// ask for the next message until it does.
	Receive(ctx context.Context, deliver func(Inbound)) error
	// Send posts text to a chat, split to the platform's size limit.
	Send(ctx context.Context, msg Outbound) error
	// Typing shows a chat that a reply is being written. Best effort.
	Typing(ctx context.Context, chatID string) error
	Info(ctx context.Context) Info
}
