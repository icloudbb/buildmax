package auth

import (
	"time"

	"github.com/icloudbb/buildmax/internal/server/access"
	identitysvc "github.com/icloudbb/buildmax/internal/service/identity"
)

// jwtIssuer mints access tokens for the identity service.
//
// The signing secret stays here. A service that reached for it would be a
// service that depends on a transport, and this is the whole of what it needs
// instead: a token and how long it lasts.
type jwtIssuer struct {
	secret string
	ttl    time.Duration
}

func (i jwtIssuer) Mint(userID, sessionID string, now time.Time) (string, time.Duration, error) {
	token, err := access.Mint(i.secret, userID, sessionID, now, i.ttl)
	if err != nil {
		return "", 0, err
	}
	return token, i.ttl, nil
}

// identityService builds the authentication workflows from this handler's
// configuration. It is built per call rather than held, the same way the other
// handlers in this package build theirs.
func (h *Handler) identityService() *identitysvc.Service {
	return &identitysvc.Service{
		Users:              h.cfg.Users,
		Passwords:          h.cfg.Passwords,
		LoginCodes:         h.cfg.LoginCodes,
		RefreshTokens:      h.cfg.RefreshTokens,
		Sessions:           h.cfg.Sessions,
		Tokens:             jwtIssuer{secret: h.cfg.JWTSecret, ttl: h.accessTokenTTL()},
		RefreshTTL:         h.refreshTokenTTL(),
		RotationGrace:      h.refreshRotationGrace(),
		SessionAbsoluteTTL: h.sessionAbsoluteTTL(),
		Audit:              h.cfg.Audit,
	}
}
