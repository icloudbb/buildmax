package oidc

import (
	"context"
	"errors"
	"fmt"

	coreoidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

// defaultScopes are requested when a caller names none. openid is required;
// email and profile carry the verified email and display name a first
// association needs. Offline access is deliberately not requested: BuildMax
// keeps its own session and discards the provider's tokens, so a refresh token
// from the IdP would be a credential held for no purpose.
var defaultScopes = []string{coreoidc.ScopeOpenID, "email", "profile"}

// Transaction is the per-login secret set that binds a callback to the browser
// that began it. The handler stores it integrity-protected in a short-lived
// cookie and hands it back to Complete; none of it is a bearer credential, but
// all of it must survive the round trip unaltered for the callback to be
// accepted.
type Transaction struct {
	State    string `json:"state"`
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
}

// Claims are the verified facts a completed sign-in yields. Email is carried
// with its verification flag rather than pre-filtered so the caller decides what
// an unverified email may do — a first association requires a verified one.
type Claims struct {
	Issuer        string
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
}

// BeginAuth mints a transaction and the authorization URL to redirect the
// browser to. redirectURL is the deployment's fixed callback URL, built from
// public_base_url — never from a request Host. It triggers discovery if needed,
// so a momentarily unreachable IdP surfaces here rather than at a dead redirect.
func (p *Provider) BeginAuth(ctx context.Context, redirectURL string, scopes []string) (string, Transaction, error) {
	conf, err := p.oauth2Config(ctx, redirectURL, scopes)
	if err != nil {
		return "", Transaction{}, err
	}
	// GenerateVerifier is a 32-byte URL-safe random string; it is unguessable, so
	// it serves for the state and nonce as well as the PKCE verifier.
	txn := Transaction{
		State:    oauth2.GenerateVerifier(),
		Nonce:    oauth2.GenerateVerifier(),
		Verifier: oauth2.GenerateVerifier(),
	}
	url := conf.AuthCodeURL(txn.State,
		coreoidc.Nonce(txn.Nonce),
		oauth2.S256ChallengeOption(txn.Verifier),
	)
	return url, txn, nil
}

// Complete exchanges the authorization code and validates the ID Token,
// returning the verified claims. It performs the server-side confidential-client
// exchange (client_secret_basic), validates the ID Token signature against the
// IdP's JWKS and its issuer/audience/expiry via the provider, then checks the
// nonce and, when present, the authorized-party claim. If the ID Token carries
// no email it falls back to UserInfo, requiring the subject to match.
func (p *Provider) Complete(ctx context.Context, code string, txn Transaction, redirectURL string, scopes []string) (*Claims, error) {
	conf, err := p.oauth2Config(ctx, redirectURL, scopes)
	if err != nil {
		return nil, err
	}
	token, err := conf.Exchange(ctx, code, oauth2.VerifierOption(txn.Verifier))
	if err != nil {
		return nil, fmt.Errorf("token exchange: %w", err)
	}
	rawID, ok := token.Extra("id_token").(string)
	if !ok || rawID == "" {
		return nil, errors.New("token response carried no id_token")
	}
	idToken, err := p.Verify(ctx, rawID)
	if err != nil {
		return nil, fmt.Errorf("verify id token: %w", err)
	}
	if idToken.Nonce != txn.Nonce {
		return nil, errors.New("id token nonce does not match the login transaction")
	}
	var c struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
		AuthorizedTo  string `json:"azp"`
	}
	if err := idToken.Claims(&c); err != nil {
		return nil, fmt.Errorf("read id token claims: %w", err)
	}
	// When the IdP issues the token to a party, that party must be this client.
	// It is only meaningful when present; go-oidc has already checked the audience
	// contains this client.
	if c.AuthorizedTo != "" && c.AuthorizedTo != p.cfg.ClientID {
		return nil, errors.New("id token azp is not this client")
	}
	claims := &Claims{
		Issuer: idToken.Issuer, Subject: idToken.Subject,
		Email: c.Email, EmailVerified: c.EmailVerified, Name: c.Name,
	}
	if claims.Email == "" {
		if err := p.fillFromUserInfo(ctx, token, idToken.Subject, claims); err != nil {
			return nil, err
		}
	}
	return claims, nil
}

// fillFromUserInfo fetches the UserInfo endpoint when the ID Token lacked an
// email, and refuses a response whose subject does not match the ID Token's — a
// mismatched sub is a different account, never a source of this login's email.
func (p *Provider) fillFromUserInfo(ctx context.Context, token *oauth2.Token, subject string, claims *Claims) error {
	if _, err := p.ensure(ctx); err != nil {
		return err
	}
	p.mu.Lock()
	core := p.provider
	p.mu.Unlock()
	if core == nil {
		return errors.New("oidc provider is not initialized")
	}
	info, err := core.UserInfo(withDeadline(p.ctx, ctx), oauth2.StaticTokenSource(token))
	if err != nil {
		return fmt.Errorf("fetch userinfo: %w", err)
	}
	if info.Subject != subject {
		return errors.New("userinfo subject does not match the id token")
	}
	var uc struct {
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
		Name          string `json:"name"`
	}
	if err := info.Claims(&uc); err != nil {
		return fmt.Errorf("read userinfo claims: %w", err)
	}
	claims.Email = uc.Email
	claims.EmailVerified = uc.EmailVerified
	if claims.Name == "" {
		claims.Name = uc.Name
	}
	return nil
}

// oauth2Config builds the exchange configuration for one redirect URL. The auth
// style is forced to the HTTP Basic header (client_secret_basic), the
// confidential-client method BuildMax registers with the IdP.
func (p *Provider) oauth2Config(ctx context.Context, redirectURL string, scopes []string) (*oauth2.Config, error) {
	endpoint, err := p.Endpoint(ctx)
	if err != nil {
		return nil, err
	}
	endpoint.AuthStyle = oauth2.AuthStyleInHeader
	if len(scopes) == 0 {
		scopes = defaultScopes
	}
	return &oauth2.Config{
		ClientID:     p.cfg.ClientID,
		ClientSecret: p.cfg.ClientSecret,
		Endpoint:     endpoint,
		RedirectURL:  redirectURL,
		Scopes:       scopes,
	}, nil
}
