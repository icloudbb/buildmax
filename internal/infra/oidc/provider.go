// Package oidc wraps a maintained OpenID Connect library so the rest of the
// server never touches raw JOSE. It owns the two network facts a login depends
// on — the IdP's discovery document and its signing keys — and the ID-token
// verification that turns a returned token into a trustworthy subject.
//
// The configured issuer is the only URL trust root: discovery, JWKS, and the
// authorize/token endpoints are all taken from it, never from a request. A
// network fetch to the IdP can fail without the server being broken, so the
// provider initializes lazily and reports its health rather than refusing to
// start — a momentarily unreachable IdP must not fail the readiness probe.
package oidc

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"

	"golang.org/x/oauth2"
)

// asymmetricAlgs is the closed set of signing algorithms an ID token may use.
// It is set on the verifier explicitly rather than left to the provider's
// advertised list so that "none" and HMAC (HS*) — which would let anyone
// holding the client secret forge a token — are refused whatever the IdP's
// discovery document claims to support.
var asymmetricAlgs = []string{
	coreoidc.RS256, coreoidc.RS384, coreoidc.RS512,
	coreoidc.ES256, coreoidc.ES384, coreoidc.ES512,
	coreoidc.PS256, coreoidc.PS384, coreoidc.PS512,
	coreoidc.EdDSA,
}

// Config is what the provider needs to reach one IdP and verify its tokens. It
// is derived from server.yaml's oidc block; the client secret is held only for
// the token exchange and never leaves this process.
type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	// HTTPClient overrides the client used for every IdP fetch (discovery, JWKS,
	// token, UserInfo). Nil uses http.DefaultClient. Tests point it at a fake
	// issuer; a deployment may set a client with a corporate trust store.
	HTTPClient *http.Client
}

// Status is the operator-facing health of the provider: whether discovery has
// succeeded, when it last did, and the last error's message (already safe to
// show — go-oidc errors carry no secret). It backs the admin diagnostics view.
type Status struct {
	Available   bool
	LastRefresh time.Time
	LastError   string
}

// Provider verifies ID tokens for one IdP. It initializes lazily and caches the
// discovery result; JWKS caching and refresh-on-unknown-kid are handled by the
// underlying remote key set. It is safe for concurrent use.
type Provider struct {
	cfg Config
	// ctx carries the HTTP client for every later IdP fetch. The remote key set
	// keeps it for background JWKS refreshes and ignores its cancellation, so it
	// is deliberately not request-scoped.
	ctx context.Context

	mu          sync.Mutex
	provider    *coreoidc.Provider
	verifier    *coreoidc.IDTokenVerifier
	lastErr     error
	lastRefresh time.Time
}

// New builds a provider from cfg without contacting the IdP. Discovery happens
// on first use, so a server whose IdP is briefly unreachable at boot still
// starts and recovers on its own.
func New(cfg Config) *Provider {
	client := cfg.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	return &Provider{
		cfg: cfg,
		ctx: coreoidc.ClientContext(context.Background(), client),
	}
}

// ensure performs discovery once and caches the provider and verifier. A failed
// attempt is retried on the next call rather than cached, so an IdP that was
// down at first use is picked up when it returns. It records the outcome for
// Status either way.
func (p *Provider) ensure(ctx context.Context) (*coreoidc.IDTokenVerifier, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.verifier != nil {
		return p.verifier, nil
	}
	// Discovery uses the stored client (so tests reach the fake issuer) but honors
	// the caller's deadline for this attempt.
	provider, err := coreoidc.NewProvider(withDeadline(p.ctx, ctx), p.cfg.Issuer)
	if err != nil {
		p.lastErr = err
		return nil, err
	}
	verifier := provider.VerifierContext(p.ctx, &coreoidc.Config{
		ClientID:             p.cfg.ClientID,
		SupportedSigningAlgs: asymmetricAlgs,
	})
	p.provider = provider
	p.verifier = verifier
	p.lastErr = nil
	p.lastRefresh = time.Now()
	return verifier, nil
}

// Verify checks an ID token's signature against the IdP's JWKS and its issuer,
// audience, and expiry, returning the verified token. The caller still has to
// check the nonce and, when present, the azp claim — go-oidc leaves those to
// the caller because only the caller knows the nonce it issued.
func (p *Provider) Verify(ctx context.Context, rawIDToken string) (*coreoidc.IDToken, error) {
	verifier, err := p.ensure(ctx)
	if err != nil {
		return nil, err
	}
	return verifier.Verify(ctx, rawIDToken)
}

// Endpoint returns the IdP's authorize and token endpoints from discovery. It
// triggers discovery if it has not happened yet.
func (p *Provider) Endpoint(ctx context.Context) (oauth2.Endpoint, error) {
	if _, err := p.ensure(ctx); err != nil {
		return oauth2.Endpoint{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.provider.Endpoint(), nil
}

// UserInfoEndpoint returns the discovered UserInfo endpoint, or empty when the
// IdP advertises none.
func (p *Provider) UserInfoEndpoint(ctx context.Context) (string, error) {
	if _, err := p.ensure(ctx); err != nil {
		return "", err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.provider.UserInfoEndpoint(), nil
}

// Warm triggers discovery so a deployment can surface a degraded IdP at startup
// without waiting for the first login. Its error is advisory: the provider
// still works once the IdP returns.
func (p *Provider) Warm(ctx context.Context) error {
	_, err := p.ensure(ctx)
	return err
}

// Status reports the provider's health for the admin diagnostics view.
func (p *Provider) Status() Status {
	p.mu.Lock()
	defer p.mu.Unlock()
	st := Status{Available: p.verifier != nil, LastRefresh: p.lastRefresh}
	if p.lastErr != nil {
		st.LastError = p.lastErr.Error()
	}
	return st
}

// withDeadline carries base's HTTP client but adopts req's deadline, if any, so
// one discovery attempt cannot hang past the caller's timeout while the client
// (used later for background JWKS refresh) still outlives the request.
func withDeadline(base, req context.Context) context.Context {
	if _, ok := req.Deadline(); !ok {
		return base
	}
	return &deadlineContext{Context: base, deadline: req}
}

type deadlineContext struct {
	context.Context
	deadline context.Context
}

func (c *deadlineContext) Deadline() (time.Time, bool) { return c.deadline.Deadline() }
func (c *deadlineContext) Done() <-chan struct{}       { return c.deadline.Done() }
func (c *deadlineContext) Err() error {
	if err := c.deadline.Err(); err != nil {
		return err
	}
	return c.Context.Err()
}

// ErrNotConfigured is returned by callers that ask for a provider a deployment
// did not configure. It lives here so the handler and the wiring agree on one
// value.
var ErrNotConfigured = errors.New("oidc is not configured")
