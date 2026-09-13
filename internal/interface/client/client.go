// Package client provides an HTTP client for the BuildMax server API.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/icloudbb/buildmax/internal/infra/httpclient"
	"github.com/icloudbb/buildmax/internal/infra/llmwire"
)

// DefaultServerURL is where a client looks when nobody has named a server: the
// address buildmax-server listens on when it runs on this machine. It is the
// last fallback, not an assumption — settings.yaml's server_url wins, and a
// deployment behind an ingress publishes one origin for Portal and API that is
// not this one.
const DefaultServerURL = "http://localhost:5678"

// LoginUser is the user subset returned in a login response.
type LoginUser struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

// LoginResponse is the successful result of POST /api/auth/login.
type LoginResponse struct {
	// Token is AccessToken under the name it had before a login returned two
	// credentials. A server older than that split sends only this one.
	Token       string `json:"token"`
	AccessToken string `json:"access_token"`
	// RefreshToken is empty when the server keeps no store for it, which means
	// the login ends when the access token does.
	RefreshToken string    `json:"refresh_token"`
	ExpiresIn    int64     `json:"expires_in"`
	User         LoginUser `json:"user"`
}

// Access returns the access token under whichever name the server used.
func (r *LoginResponse) Access() string {
	if r.AccessToken != "" {
		return r.AccessToken
	}
	return r.Token
}

// RefreshResponse is the successful result of POST /api/auth/token/refresh.
type RefreshResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
}

// ErrRefreshRejected means the server refused the refresh token: it is spent,
// revoked, expired, or was replayed. The session is over and only a new login
// will produce another.
var ErrRefreshRejected = errors.New("refresh token rejected")

// Client is a stateless HTTP client for the BuildMax server API.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// NewClient creates a Client for the given server base URL.
func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL:    strings.TrimRight(baseURL, "/"),
		HTTPClient: http.DefaultClient,
	}
}

// RequestOTP calls POST /api/auth/otp. intent is "login" or "signup".
func (c *Client) RequestOTP(ctx context.Context, email, intent string) error {
	body, _ := json.Marshal(map[string]string{
		"email":  email,
		"intent": intent,
	})
	url := c.BaseURL + "/api/auth/otp"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return httpclient.DecodeError(resp, "")
}

// Login calls POST /api/auth/login with a single-use login code — the recovery
// path, used to claim a new account or replace a forgotten password.
// platform identifies the calling client ("cli", "desktop", "portal").
func (c *Client) Login(ctx context.Context, email, otp, platform string) (*LoginResponse, error) {
	return c.login(ctx, map[string]string{
		"email":    email,
		"otp":      otp,
		"platform": platform,
	})
}

// LoginWithPassword calls POST /api/auth/login with a password, the everyday way in.
func (c *Client) LoginWithPassword(ctx context.Context, email, password, platform string) (*LoginResponse, error) {
	return c.login(ctx, map[string]string{
		"email":    email,
		"password": password,
		"platform": platform,
	})
}

func (c *Client) login(ctx context.Context, payload map[string]string) (*LoginResponse, error) {
	body, _ := json.Marshal(payload)
	url := c.BaseURL + "/api/auth/login"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, httpclient.DecodeError(resp, "")
	}
	var lr LoginResponse
	if err := json.NewDecoder(resp.Body).Decode(&lr); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &lr, nil
}

// Refresh calls POST /api/auth/token/refresh, exchanging a refresh token for a new
// pair.
//
// A rejected token returns ErrRefreshRejected, which the caller must be able to
// tell apart from the server being unreachable: one means sign in again, the
// other means try later.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (*RefreshResponse, error) {
	body, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})
	url := c.BaseURL + "/api/auth/token/refresh"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return nil, ErrRefreshRejected
	}
	if resp.StatusCode != http.StatusOK {
		return nil, httpclient.DecodeError(resp, "")
	}
	var rr RefreshResponse
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if rr.AccessToken == "" {
		return nil, fmt.Errorf("refresh returned no access token")
	}
	return &rr, nil
}

// Logout calls POST /api/auth/logout to revoke the session behind refreshToken.
func (c *Client) Logout(ctx context.Context, refreshToken, accessToken string) error {
	body, _ := json.Marshal(map[string]string{"refresh_token": refreshToken})
	url := c.BaseURL + "/api/auth/logout"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+accessToken)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	return httpclient.DecodeError(resp, "")
}

// ListServerModels calls GET /api/llm/models and returns the models this
// deployment offers through the managed gateway. Being signed in is the whole
// authorization: every catalog model is available to every user.
//
// The reply names models only. Which provider serves one, and with whose
// credential, stays on the server.
func (c *Client) ListServerModels(ctx context.Context, token string) ([]llmwire.Model, error) {
	url := c.BaseURL + llmwire.ModelsPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, httpclient.DecodeError(resp, "")
	}
	var out llmwire.ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return out.Models, nil
}
