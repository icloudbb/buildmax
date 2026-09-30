package client

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/icloudbb/buildmax/internal/infra/httpclient"
)

// QuotaTier is one seeded tier as the admin API returns it. Zero
// MaxStorageBytes means no storage limit.
type QuotaTier struct {
	TierName           string `json:"tier_name"`
	MaxRunsPerPeriod   int    `json:"max_runs_per_period"`
	MaxTokensPerPeriod int    `json:"max_tokens_per_period"`
	MaxStorageBytes    int64  `json:"max_storage_bytes"`
	PeriodDays         int    `json:"period_days"`
}

// ListQuotaTiers returns every tier a space can be assigned to.
func (c *Client) ListQuotaTiers(ctx context.Context, token string) ([]QuotaTier, error) {
	var out struct {
		Tiers []QuotaTier `json:"tiers"`
	}
	if err := c.getJSON(ctx, token, "/api/admin/quota-tiers", &out); err != nil {
		return nil, err
	}
	return out.Tiers, nil
}

// SetSpaceQuotaTier assigns a space to an existing tier. The server refuses an
// unknown tier with a message naming the valid ones.
func (c *Client) SetSpaceQuotaTier(ctx context.Context, token, spaceID, tier string) error {
	payload, err := json.Marshal(struct {
		Tier string `json:"tier"`
	}{Tier: tier})
	if err != nil {
		return err
	}
	path := "/api/admin/spaces/" + url.PathEscape(spaceID) + "/quota-tier"
	resp, err := c.do(ctx, http.MethodPut, token, path, "application/json", bytes.NewReader(payload))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return httpclient.DecodeError(resp, "")
	}
	return nil
}
