// Package mockoidc is an OpenID Connect provider for end-to-end tests: enough
// of the authorization-code flow for BuildMax's real relying party to complete a
// sign-in against it, with no tenant, account, or network beyond the test.
//
// It is deliberately strict where a real IdP is strict, because a lenient mock
// would let a broken client pass: the client authenticates with HTTP Basic, a
// code is single-use and bound to its redirect URI and PKCE challenge, and the
// ID Token is RS256-signed with the nonce the client sent. What a real IdP
// decides about the person — who they are, whether the email is verified — is
// instead chosen on the sign-in form, so a suite can drive the first-login,
// existing-account, and refused-domain paths with one deployment.
//
// It is stdlib-only so deployment/smoke/mock-oidc can build it without
// downloading modules.
package mockoidc

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html/template"
	"math/big"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

// Config names the one client this provider serves.
type Config struct {
	// Issuer is the exact issuer URL clients are configured with. Endpoints are
	// served beneath its path, so it may carry one.
	Issuer       string
	ClientID     string
	ClientSecret string
	// EmailVerifiedOnlyAtUserInfo leaves email_verified out of the ID Token and
	// answers it only at UserInfo, as Okta's org authorization server does. A
	// client that reads an absent claim as false refuses every such sign-in.
	EmailVerifiedOnlyAtUserInfo bool
}

// Server is the provider. It is safe for concurrent use.
type Server struct {
	cfg    Config
	prefix string
	key    *rsa.PrivateKey
	kid    string

	mu     sync.Mutex
	codes  map[string]grant
	tokens map[string]Identity
}

// Identity is what the sign-in form asserts about the person.
type Identity struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
}

// grant is an issued authorization code and everything the token request must
// match.
type grant struct {
	identity      Identity
	clientID      string
	redirectURI   string
	nonce         string
	codeChallenge string
	expires       time.Time
}

const (
	codeTTL  = time.Minute
	tokenTTL = 10 * time.Minute
)

// New builds a provider with a fresh signing key.
func New(cfg Config) (*Server, error) {
	if cfg.Issuer == "" || cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, errors.New("mockoidc: issuer, client ID, and client secret are required")
	}
	u, err := url.Parse(cfg.Issuer)
	if err != nil || u.Host == "" {
		return nil, errors.New("mockoidc: issuer must be an absolute URL")
	}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	return &Server{
		cfg:    cfg,
		prefix: strings.TrimRight(u.Path, "/"),
		key:    key,
		kid:    randomToken()[:16],
		codes:  map[string]grant{},
		tokens: map[string]Identity{},
	}, nil
}

// Handler serves discovery, JWKS, authorize, token, and UserInfo beneath the
// issuer's path.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+s.prefix+"/.well-known/openid-configuration", s.discovery)
	mux.HandleFunc("GET "+s.prefix+"/jwks", s.jwks)
	mux.HandleFunc("GET "+s.prefix+"/authorize", s.authorizeForm)
	mux.HandleFunc("POST "+s.prefix+"/authorize", s.authorizeSubmit)
	mux.HandleFunc("POST "+s.prefix+"/token", s.token)
	mux.HandleFunc("GET "+s.prefix+"/userinfo", s.userinfo)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	return mux
}

// SubjectFor is the subject the form assigns an email when none is entered:
// stable per address, so a person signing in twice is the same identity, and
// opaque, as a real IdP's subject is.
func SubjectFor(email string) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(email))))
	return "mock-" + hex.EncodeToString(sum[:8])
}

func (s *Server) endpoint(path string) string {
	return strings.TrimRight(s.cfg.Issuer, "/") + path
}

func (s *Server) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                s.cfg.Issuer,
		"authorization_endpoint":                s.endpoint("/authorize"),
		"token_endpoint":                        s.endpoint("/token"),
		"userinfo_endpoint":                     s.endpoint("/userinfo"),
		"jwks_uri":                              s.endpoint("/jwks"),
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"token_endpoint_auth_methods_supported": []string{"client_secret_basic"},
		"code_challenge_methods_supported":      []string{"S256"},
		"scopes_supported":                      []string{"openid", "email", "profile"},
	})
}

func (s *Server) jwks(w http.ResponseWriter, _ *http.Request) {
	pub := s.key.PublicKey
	writeJSON(w, http.StatusOK, map[string]any{"keys": []map[string]any{{
		"kty": "RSA", "use": "sig", "alg": "RS256", "kid": s.kid,
		"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}}})
}

// authorizeParams are the request parameters the form carries through to its
// submit unchanged.
type authorizeParams struct {
	ClientID, RedirectURI, State, Nonce, Scope, CodeChallenge, CodeChallengeMethod string
}

func readAuthorizeParams(v url.Values) authorizeParams {
	return authorizeParams{
		ClientID: v.Get("client_id"), RedirectURI: v.Get("redirect_uri"), State: v.Get("state"),
		Nonce: v.Get("nonce"), Scope: v.Get("scope"),
		CodeChallenge: v.Get("code_challenge"), CodeChallengeMethod: v.Get("code_challenge_method"),
	}
}

// validate refuses what a real IdP refuses before showing a login page. Errors
// here are shown to the person rather than redirected, because a request that
// names the wrong client or redirect cannot be trusted with a redirect.
func (s *Server) validate(p authorizeParams, responseType string) error {
	switch {
	case p.ClientID != s.cfg.ClientID:
		return errors.New("unknown client_id")
	case p.RedirectURI == "":
		return errors.New("redirect_uri is required")
	case responseType != "code":
		return errors.New("only response_type=code is supported")
	case !hasScope(p.Scope, "openid"):
		return errors.New("scope must include openid")
	case p.CodeChallenge == "" || p.CodeChallengeMethod != "S256":
		return errors.New("PKCE with S256 is required")
	}
	return nil
}

var formPage = template.Must(template.New("form").Parse(`<!doctype html>
<html><head><meta charset="utf-8"><title>Mock IdP sign-in</title>
<style>body{font-family:system-ui,sans-serif;max-width:24rem;margin:4rem auto;padding:0 1rem}
label{display:block;margin-top:1rem}input[type=email],input[type=text]{width:100%;padding:.4rem;box-sizing:border-box}
button{margin-top:1.25rem;margin-right:.5rem;padding:.5rem 1rem}</style></head>
<body><h1>Mock IdP</h1><p>A test identity provider. Nothing here is checked: who you say you are is who you are.</p>
<form method="post">
<input type="hidden" name="client_id" value="{{.ClientID}}">
<input type="hidden" name="redirect_uri" value="{{.RedirectURI}}">
<input type="hidden" name="response_type" value="code">
<input type="hidden" name="state" value="{{.State}}">
<input type="hidden" name="nonce" value="{{.Nonce}}">
<input type="hidden" name="scope" value="{{.Scope}}">
<input type="hidden" name="code_challenge" value="{{.CodeChallenge}}">
<input type="hidden" name="code_challenge_method" value="{{.CodeChallengeMethod}}">
<label for="email">Email</label><input id="email" name="email" type="email" required>
<label for="name">Name</label><input id="name" name="name" type="text">
<label for="subject">Subject (optional)</label><input id="subject" name="subject" type="text">
<label><input name="email_verified" type="checkbox" value="true" checked> Email verified</label>
<button type="submit" name="decision" value="allow">Sign in</button>
<button type="submit" name="decision" value="deny" formnovalidate>Deny</button>
</form></body></html>`))

func (s *Server) authorizeForm(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p := readAuthorizeParams(q)
	if err := s.validate(p, q.Get("response_type")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = formPage.Execute(w, p)
}

func (s *Server) authorizeSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "malformed form", http.StatusBadRequest)
		return
	}
	p := readAuthorizeParams(r.PostForm)
	if err := s.validate(p, r.PostForm.Get("response_type")); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	back, err := url.Parse(p.RedirectURI)
	if err != nil {
		http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
		return
	}
	q := back.Query()
	q.Set("state", p.State)
	if r.PostForm.Get("decision") == "deny" {
		q.Set("error", "access_denied")
		back.RawQuery = q.Encode()
		http.Redirect(w, r, back.String(), http.StatusFound)
		return
	}
	email := strings.TrimSpace(r.PostForm.Get("email"))
	if email == "" {
		http.Error(w, "email is required", http.StatusBadRequest)
		return
	}
	id := Identity{
		Subject:       strings.TrimSpace(r.PostForm.Get("subject")),
		Email:         email,
		EmailVerified: r.PostForm.Get("email_verified") == "true",
		Name:          strings.TrimSpace(r.PostForm.Get("name")),
	}
	if id.Subject == "" {
		id.Subject = SubjectFor(email)
	}
	code := randomToken()
	s.mu.Lock()
	s.codes[code] = grant{
		identity: id, clientID: p.ClientID, redirectURI: p.RedirectURI, nonce: p.Nonce,
		codeChallenge: p.CodeChallenge, expires: time.Now().Add(codeTTL),
	}
	s.mu.Unlock()
	q.Set("code", code)
	back.RawQuery = q.Encode()
	http.Redirect(w, r, back.String(), http.StatusFound)
}

func (s *Server) token(w http.ResponseWriter, r *http.Request) {
	id, secret, ok := r.BasicAuth()
	if !ok || id != s.cfg.ClientID || secret != s.cfg.ClientSecret {
		w.Header().Set("WWW-Authenticate", `Basic realm="mockoidc"`)
		tokenError(w, http.StatusUnauthorized, "invalid_client")
		return
	}
	if err := r.ParseForm(); err != nil || r.PostForm.Get("grant_type") != "authorization_code" {
		tokenError(w, http.StatusBadRequest, "unsupported_grant_type")
		return
	}
	code := r.PostForm.Get("code")
	s.mu.Lock()
	g, found := s.codes[code]
	// Spent on first presentation, valid or not, as RFC 6749 §4.1.2 asks.
	delete(s.codes, code)
	s.mu.Unlock()
	if !found || time.Now().After(g.expires) || g.clientID != id || g.redirectURI != r.PostForm.Get("redirect_uri") {
		tokenError(w, http.StatusBadRequest, "invalid_grant")
		return
	}
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if base64.RawURLEncoding.EncodeToString(sum[:]) != g.codeChallenge {
		tokenError(w, http.StatusBadRequest, "invalid_grant")
		return
	}
	now := time.Now()
	claims := map[string]any{
		"iss": s.cfg.Issuer, "sub": g.identity.Subject, "aud": s.cfg.ClientID, "azp": s.cfg.ClientID,
		"iat": now.Unix(), "exp": now.Add(tokenTTL).Unix(), "nonce": g.nonce,
		"email": g.identity.Email,
	}
	if !s.cfg.EmailVerifiedOnlyAtUserInfo {
		claims["email_verified"] = g.identity.EmailVerified
	}
	if g.identity.Name != "" {
		claims["name"] = g.identity.Name
	}
	idToken, err := s.sign(claims)
	if err != nil {
		tokenError(w, http.StatusInternalServerError, "server_error")
		return
	}
	access := randomToken()
	s.mu.Lock()
	s.tokens[access] = g.identity
	s.mu.Unlock()
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token": access, "token_type": "Bearer", "expires_in": int(tokenTTL / time.Second), "id_token": idToken,
	})
}

func (s *Server) userinfo(w http.ResponseWriter, r *http.Request) {
	access, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	s.mu.Lock()
	id, found := s.tokens[access]
	s.mu.Unlock()
	if !ok || !found {
		w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	body := map[string]any{"sub": id.Subject, "email": id.Email, "email_verified": id.EmailVerified}
	if id.Name != "" {
		body["name"] = id.Name
	}
	writeJSON(w, http.StatusOK, body)
}

// sign produces a compact RS256 JWS.
func (s *Server) sign(claims map[string]any) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT", "kid": s.kid})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	signing := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	digest := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, s.key, crypto.SHA256, digest[:])
	if err != nil {
		return "", err
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func hasScope(scope, want string) bool {
	return slices.Contains(strings.Fields(scope), want)
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func tokenError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
