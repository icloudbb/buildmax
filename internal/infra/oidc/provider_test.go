package oidc_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	bmoidc "github.com/icloudbb/buildmax/internal/infra/oidc"
)

// fakeIDP is a minimal, in-process OpenID Connect issuer: a discovery document
// and a JWKS whose keys it also signs tokens with. It is the adversary these
// tests use to prove BuildMax's own verification, since the real-Okta login is
// Phase 3. Its JWKS can rotate so the unknown-kid refresh path is exercised.
type fakeIDP struct {
	t      *testing.T
	server *httptest.Server
	keys   map[string]*rsa.PrivateKey // kid -> key currently published
}

func newFakeIDP(t *testing.T) *fakeIDP {
	t.Helper()
	idp := &fakeIDP{t: t, keys: map[string]*rsa.PrivateKey{}}
	idp.addKey("key-1")
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, map[string]any{
			"issuer":                 idp.server.URL,
			"authorization_endpoint": idp.server.URL + "/authorize",
			"token_endpoint":         idp.server.URL + "/token",
			"userinfo_endpoint":      idp.server.URL + "/userinfo",
			"jwks_uri":               idp.server.URL + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, idp.jwks())
	})
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

func (idp *fakeIDP) addKey(kid string) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		idp.t.Fatalf("generate key: %v", err)
	}
	idp.keys[kid] = key
}

// rotate replaces the published key set with a single new key, as an IdP does
// when it rolls its signing key. A verifier holding the old JWKS must refresh
// on the unknown kid to accept a token signed by the new one.
func (idp *fakeIDP) rotate(kid string) {
	idp.keys = map[string]*rsa.PrivateKey{}
	idp.addKey(kid)
}

func (idp *fakeIDP) jwks() map[string]any {
	var keys []map[string]any
	for kid, key := range idp.keys {
		pub := key.Public().(*rsa.PublicKey)
		keys = append(keys, map[string]any{
			"kty": "RSA",
			"kid": kid,
			"alg": "RS256",
			"use": "sig",
			"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(exponentBytes(pub.E)),
		})
	}
	return map[string]any{"keys": keys}
}

// mint signs an RS256 ID token with the named key. Passing an unpublished kid is
// how a token from a rotated-away key is simulated.
func (idp *fakeIDP) mint(kid string, claims jwt.MapClaims) string {
	key, ok := idp.keys[kid]
	if !ok {
		idp.t.Fatalf("mint with unknown kid %q", kid)
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = kid
	s, err := tok.SignedString(key)
	if err != nil {
		idp.t.Fatalf("sign: %v", err)
	}
	return s
}

// mintHS256 forges a token with an HMAC signature over the client secret — the
// attack the asymmetric-only restriction exists to refuse.
func (idp *fakeIDP) mintHS256(secret string, claims jwt.MapClaims) string {
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tok.Header["kid"] = "key-1"
	s, err := tok.SignedString([]byte(secret))
	if err != nil {
		idp.t.Fatalf("sign hs256: %v", err)
	}
	return s
}

func (idp *fakeIDP) claims(sub, clientID string) jwt.MapClaims {
	now := time.Now()
	return jwt.MapClaims{
		"iss":            idp.server.URL,
		"sub":            sub,
		"aud":            clientID,
		"exp":            now.Add(time.Hour).Unix(),
		"iat":            now.Unix(),
		"nonce":          "n-abc",
		"email":          "user@example.com",
		"email_verified": true,
	}
}

const testClientID = "0oaClient"

func newProvider(idp *fakeIDP) *bmoidc.Provider {
	return bmoidc.New(bmoidc.Config{
		Issuer:       idp.server.URL,
		ClientID:     testClientID,
		ClientSecret: "shh",
		HTTPClient:   idp.server.Client(),
	})
}

func TestProviderVerifiesAGoodToken(t *testing.T) {
	idp := newFakeIDP(t)
	p := newProvider(idp)

	raw := idp.mint("key-1", idp.claims("okta|123", testClientID))
	tok, err := p.Verify(context.Background(), raw)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if tok.Subject != "okta|123" {
		t.Errorf("subject = %q, want okta|123", tok.Subject)
	}
	var extra struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := tok.Claims(&extra); err != nil {
		t.Fatalf("claims: %v", err)
	}
	if extra.Email != "user@example.com" || !extra.EmailVerified {
		t.Errorf("claims = %+v, want the verified email", extra)
	}
	if tok.Nonce != "n-abc" {
		t.Errorf("nonce = %q, want n-abc (the caller checks its value)", tok.Nonce)
	}
	if !p.Status().Available {
		t.Error("status should report available after a successful verify")
	}
}

func TestProviderRefusesAWrongAudience(t *testing.T) {
	idp := newFakeIDP(t)
	p := newProvider(idp)

	raw := idp.mint("key-1", idp.claims("okta|123", "some-other-client"))
	if _, err := p.Verify(context.Background(), raw); err == nil {
		t.Fatal("a token minted for another client was accepted")
	}
}

func TestProviderRefusesAnExpiredToken(t *testing.T) {
	idp := newFakeIDP(t)
	p := newProvider(idp)

	claims := idp.claims("okta|123", testClientID)
	claims["exp"] = time.Now().Add(-time.Minute).Unix()
	if _, err := p.Verify(context.Background(), idp.mint("key-1", claims)); err == nil {
		t.Fatal("an expired token was accepted")
	}
}

// TestProviderRefusesHS256 is the important negative: an attacker who knows the
// client secret (a confidential value, but not the signing key) must not be
// able to forge a token by switching the algorithm to HMAC.
func TestProviderRefusesHS256(t *testing.T) {
	idp := newFakeIDP(t)
	p := newProvider(idp)

	raw := idp.mintHS256("shh", idp.claims("okta|123", testClientID))
	if _, err := p.Verify(context.Background(), raw); err == nil {
		t.Fatal("an HS256 token signed over the client secret was accepted")
	}
}

// TestProviderRefreshesOnUnknownKid covers a signing-key rollover: the verifier
// caches the JWKS, the IdP rotates, and a token from the new key must still be
// accepted after one refresh — without restarting the server.
func TestProviderRefreshesOnUnknownKid(t *testing.T) {
	idp := newFakeIDP(t)
	p := newProvider(idp)

	// Prime the cache with the first key.
	if _, err := p.Verify(context.Background(), idp.mint("key-1", idp.claims("okta|1", testClientID))); err != nil {
		t.Fatalf("first verify: %v", err)
	}
	// The IdP rolls its key; the cached JWKS no longer has the new kid.
	idp.rotate("key-2")
	raw := idp.mint("key-2", idp.claims("okta|1", testClientID))
	if _, err := p.Verify(context.Background(), raw); err != nil {
		t.Fatalf("verify after rotation: %v", err)
	}
}

func TestProviderReportsAnUnreachableIssuer(t *testing.T) {
	p := bmoidc.New(bmoidc.Config{Issuer: "https://127.0.0.1:1", ClientID: testClientID})
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if _, err := p.Verify(ctx, "whatever"); err == nil {
		t.Fatal("verify against an unreachable issuer should fail")
	}
	st := p.Status()
	if st.Available {
		t.Error("an unreachable issuer must not report available")
	}
	if st.LastError == "" {
		t.Error("a failed discovery should record its error for the admin view")
	}
}

// big returns the big-endian minimal bytes of a public exponent, for the JWK "e"
// field. RSA exponents are small (65537), so two or three bytes.
func exponentBytes(e int) []byte {
	var b []byte
	for e > 0 {
		b = append([]byte{byte(e & 0xff)}, b...)
		e >>= 8
	}
	return b
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
