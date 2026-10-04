package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/icloudbb/buildmax/internal/infra/httpclient"
)

// SystemGrant is one deployment-scoped authority as the admin API returns it,
// with the account it names already resolved to an email.
type SystemGrant struct {
	ID        string     `json:"id"`
	UserID    string     `json:"user_id"`
	Role      string     `json:"role"`
	GrantedBy string     `json:"granted_by"`
	GrantedAt time.Time  `json:"granted_at"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
	Email     string     `json:"email,omitempty"`
}

// Active reports whether the grant is still in force.
func (g SystemGrant) Active() bool { return g.RevokedAt == nil }

type adminGrantsResponse struct {
	Grants []SystemGrant `json:"grants"`
}

// AdminAccount is one account as the admin user list returns it. Only the fields
// the CLI needs — to resolve an email to an id and show whether the account is
// disabled — are kept.
type AdminAccount struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
	// Kind is "human" or "service". A service account has no email and never
	// signs in.
	Kind        string     `json:"kind,omitempty"`
	DisabledAt  *time.Time `json:"disabled_at,omitempty"`
	HasPassword bool       `json:"has_password"`
}

// Disabled reports whether the account is currently refused.
func (a AdminAccount) Disabled() bool { return a.DisabledAt != nil }

type adminUsersResponse struct {
	Users []AdminAccount `json:"users"`
	Total int            `json:"total"`
}

// ListSystemGrants returns the deployment's system grants, newest first.
// includeRevoked adds the retired ones, which is how the trail of who held
// authority is read.
func (c *Client) ListSystemGrants(ctx context.Context, token string, includeRevoked bool) ([]SystemGrant, error) {
	path := "/api/admin/grants"
	if includeRevoked {
		path += "?include_revoked=true"
	}
	var out adminGrantsResponse
	if err := c.getJSON(ctx, token, path, &out); err != nil {
		return nil, err
	}
	return out.Grants, nil
}

// GrantSystemRole grants role to the account with userID and returns the grant.
// An empty role lets the server apply its default (system_admin).
func (c *Client) GrantSystemRole(ctx context.Context, token, userID, role string) (*SystemGrant, error) {
	body, _ := json.Marshal(map[string]string{"user_id": userID, "role": role})
	resp, err := c.do(ctx, http.MethodPost, token, "/api/admin/grants", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return nil, httpclient.DecodeError(resp, "")
	}
	var out SystemGrant
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}

// RevokeSystemRole revokes role from the account with userID. An empty role
// lets the server apply its default (system_admin). Revoking the last holder is
// refused by the server; the error carries the recovery command.
func (c *Client) RevokeSystemRole(ctx context.Context, token, userID, role string) error {
	path := "/api/admin/grants/" + url.PathEscape(userID)
	if role != "" {
		path += "?role=" + url.QueryEscape(role)
	}
	resp, err := c.do(ctx, http.MethodDelete, token, path, "", nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return httpclient.DecodeError(resp, "")
	}
	return nil
}

// FindAccountByEmail resolves an email to exactly one account through the admin
// user search. It refuses when the search matches no account or more than one,
// so a caller never hands authority to a guess. The search is a substring match
// server-side, so this narrows it to an exact, case-insensitive address.
func (c *Client) FindAccountByEmail(ctx context.Context, token, email string) (*AdminAccount, error) {
	var out adminUsersResponse
	if err := c.getJSON(ctx, token, "/api/admin/users?q="+url.QueryEscape(email), &out); err != nil {
		return nil, err
	}
	var matches []AdminAccount
	for _, u := range out.Users {
		if strings.EqualFold(u.Email, email) {
			matches = append(matches, u)
		}
	}
	switch len(matches) {
	case 1:
		return &matches[0], nil
	case 0:
		return nil, fmt.Errorf("no account with email %q", email)
	default:
		return nil, fmt.Errorf("more than one account matches %q", email)
	}
}
