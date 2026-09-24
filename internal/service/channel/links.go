package channel

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"time"

	corechannel "github.com/icloudbb/buildmax/internal/core/channel"
)

// pairingAlphabet leaves out characters people confuse when retyping a code
// (0/O, 1/I/L), so a code read off a phone screen survives being typed.
const pairingAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// pairingLength gives about 40 bits: far beyond guessing within the code's
// ten-minute life, and still short enough to type.
const pairingLength = 8

func newPairingCode() (string, error) {
	size := big.NewInt(int64(len(pairingAlphabet)))
	var b strings.Builder
	for range pairingLength {
		n, err := rand.Int(rand.Reader, size)
		if err != nil {
			return "", err
		}
		b.WriteByte(pairingAlphabet[n.Int64()])
	}
	return b.String(), nil
}

// NormalizePairingCode accepts a code the way people retype one: any case, with
// or without the separator the bot prints.
func NormalizePairingCode(code string) string {
	code = strings.ToUpper(code)
	return strings.NewReplacer("-", "", " ", "").Replace(code)
}

func displayCode(code string) string {
	if len(code) != pairingLength {
		return code
	}
	return code[:4] + "-" + code[4:]
}

// offerPairing answers an unlinked sender with a link code. The link is
// confirmed in the Portal by a signed-in person who sees which chat account
// they are linking, so the chat itself never carries a BuildMax secret.
func (g *Gateway) offerPairing(ctx context.Context, c corechannel.Connector, in corechannel.Inbound) {
	if !g.mayOffer(c.Platform(), in.Tenant, in.SenderID) {
		return
	}
	code, err := newPairingCode()
	if err != nil {
		g.log.Error("pairing code not generated", "err", err)
		return
	}
	err = g.identities.CreatePairing(ctx, corechannel.Pairing{
		Platform:       c.Platform(),
		Tenant:         in.Tenant,
		ExternalUserID: in.SenderID,
		ChatID:         in.ChatID,
		Handle:         in.SenderHandle,
		ExpiresAt:      g.now().Add(corechannel.PairingTTL),
	}, code)
	if err != nil {
		g.log.Error("pairing not stored", "platform", c.Platform(), "err", err)
		g.reply(ctx, c, in.ChatID, genericFailure)
		return
	}
	shown := displayCode(code)
	var b strings.Builder
	b.WriteString("This chat account is not linked to BuildMax yet.\n\n")
	if link := g.link("/#/account/chat/" + shown); link != "" {
		fmt.Fprintf(&b, "Open this link, sign in, and confirm:\n%s\n\n", link)
		fmt.Fprintf(&b, "Or enter the code %s under Account → Chat accounts in BuildMax.", shown)
	} else {
		fmt.Fprintf(&b, "Sign in to BuildMax, open Account → Chat accounts, and enter the code %s.", shown)
	}
	fmt.Fprintf(&b, " It expires in %d minutes. Only confirm a code you asked for yourself.", int(corechannel.PairingTTL/time.Minute))
	g.reply(ctx, c, in.ChatID, b.String())
}

func (g *Gateway) mayOffer(platform, tenant, sender string) bool {
	key := platform + "\x00" + tenant + "\x00" + sender
	now := g.now()
	g.mu.Lock()
	defer g.mu.Unlock()
	if at, ok := g.offered[key]; ok && now.Sub(at) < pairingThrottle {
		return false
	}
	if len(g.offered) > 10000 {
		for k, at := range g.offered {
			if now.Sub(at) >= pairingThrottle {
				delete(g.offered, k)
			}
		}
	}
	g.offered[key] = now
	return true
}

// Platforms describes the configured connectors, for the Portal to say where
// the bot is.
func (g *Gateway) Platforms(ctx context.Context) []corechannel.Info {
	if g == nil {
		return nil
	}
	out := make([]corechannel.Info, 0, len(g.order))
	for _, p := range g.order {
		out = append(out, g.connectors[p].Info(ctx))
	}
	return out
}

// PreviewPairing shows what a code would link, so the person confirming sees
// the chat account's name before its messages start acting as them.
func (g *Gateway) PreviewPairing(ctx context.Context, code string) (*corechannel.Pairing, error) {
	p, err := g.identities.PairingByCode(ctx, NormalizePairingCode(code), g.now())
	if err != nil {
		return nil, err
	}
	if p == nil {
		return nil, corechannel.ErrPairingNotFound
	}
	return p, nil
}

// ConfirmPairing links the chat account a code names to userID and tells the
// chat it worked.
func (g *Gateway) ConfirmPairing(ctx context.Context, userID, code string) (*corechannel.Identity, error) {
	ident, pairing, err := g.identities.ConsumePairing(ctx, NormalizePairingCode(code), userID, g.now())
	if err != nil {
		return nil, err
	}
	if c := g.connectors[pairing.Platform]; c != nil && pairing.ChatID != "" {
		sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		g.reply(sendCtx, c, pairing.ChatID, "Linked to your BuildMax account. Send me a message to start, or /help to see what I can do.")
	}
	return ident, nil
}

// ListLinks returns userID's chat-account links.
func (g *Gateway) ListLinks(ctx context.Context, userID string) ([]corechannel.Identity, error) {
	return g.identities.ListIdentitiesByUser(ctx, userID)
}

// Unlink removes one of userID's links. The chat account's next message is
// answered with a new link code.
func (g *Gateway) Unlink(ctx context.Context, userID, identityID string) error {
	return g.identities.DeleteIdentity(ctx, userID, identityID)
}
