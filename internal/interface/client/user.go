package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/icloudbb/buildmax/internal/infra/httpclient"
)

// CreateAccount creates an account by email and returns it. Creating an account
// grants no way in: a separate login code does that, the same split the server
// command makes.
func (c *Client) CreateAccount(ctx context.Context, token, email string) (*AdminAccount, error) {
	body, _ := json.Marshal(map[string]string{"email": email})
	resp, err := c.do(ctx, http.MethodPost, token, "/api/admin/users", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return nil, httpclient.DecodeError(resp, "")
	}
	var out AdminAccount
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}

// IssueLoginCode mints a single-use login code for the account and returns it
// with its expiry. The code is shown once and recoverable nowhere.
func (c *Client) IssueLoginCode(ctx context.Context, token, userID string) (string, time.Time, error) {
	path := "/api/admin/users/" + url.PathEscape(userID) + "/login-code"
	resp, err := c.do(ctx, http.MethodPost, token, path, "", nil)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, httpclient.DecodeError(resp, "")
	}
	var out struct {
		Code      string    `json:"code"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", time.Time{}, fmt.Errorf("decode response: %w", err)
	}
	return out.Code, out.ExpiresAt, nil
}

// AccountStateChange is the account after a disable or enable, with what a
// disable's cleanup did (zero on enable).
type AccountStateChange struct {
	AdminAccount
	SessionsRevoked int64 `json:"sessions_revoked"`
	// CleanupFailed names the cleanup steps that failed after the account was
	// already disabled. Disabling again retries them.
	CleanupFailed []string `json:"cleanup_failed,omitempty"`
}

// SetAccountDisabled disables or enables the account and returns the result.
func (c *Client) SetAccountDisabled(ctx context.Context, token, userID string, disabled bool) (*AccountStateChange, error) {
	payload, err := json.Marshal(struct {
		Disabled bool `json:"disabled"`
	}{Disabled: disabled})
	if err != nil {
		return nil, err
	}
	path := "/api/admin/users/" + url.PathEscape(userID) + "/state"
	resp, err := c.do(ctx, http.MethodPut, token, path, "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, httpclient.DecodeError(resp, "")
	}
	var out AccountStateChange
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}

// ListAccounts returns accounts newest first with the total matched. query is an
// optional email substring; an empty query lists everyone.
func (c *Client) ListAccounts(ctx context.Context, token, query string) ([]AdminAccount, int, error) {
	path := "/api/admin/users"
	if query != "" {
		path += "?q=" + url.QueryEscape(query)
	}
	var out adminUsersResponse
	if err := c.getJSON(ctx, token, path, &out); err != nil {
		return nil, 0, err
	}
	return out.Users, out.Total, nil
}
