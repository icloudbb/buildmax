// Package testsupport holds helpers that exist only for tests.
//
// Separate from internal/util because of what it contains: nearly every
// production package imports util, which put a JWT minter in every shipped
// binary. internal/architecture keeps this package out of production code.
package testsupport

import (
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// TestJWTClaims is used by SignJWT for test tokens that match the server's sub
// and sid claims.
type TestJWTClaims struct {
	jwt.RegisteredClaims
	Sub string `json:"sub"`
	Sid string `json:"sid,omitempty"`
}

// SignJWT builds a JWT with sub claim and 24h expiry for tests. It carries no
// sid, so it authenticates against a guard with no session store; a guard that
// has one refuses it, which is what forces a real session. Use SignJWTWithSID
// when the guard checks sessions.
func SignJWT(sub, secret string) string {
	return SignJWTWithExp(sub, secret, 24*time.Hour)
}

// SignJWTWithSID builds a JWT carrying both sub and the session id, for tests
// whose guard checks the session store.
func SignJWTWithSID(sub, sid, secret string) string {
	now := time.Now()
	claims := TestJWTClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
		Sub: sub,
		Sid: sid,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, _ := token.SignedString([]byte(secret))
	return s
}

// SignJWTWithExp builds a JWT with sub claim and the given expiry offset from now.
// Use a negative duration to create an already-expired token.
func SignJWTWithExp(sub, secret string, expiresIn time.Duration) string {
	now := time.Now()
	claims := TestJWTClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(expiresIn)),
			IssuedAt:  jwt.NewNumericDate(now),
		},
		Sub: sub,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, _ := token.SignedString([]byte(secret))
	return s
}
