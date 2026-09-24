package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/icloudbb/buildmax/internal/infra/httpclient"
)

// AdminModel is one catalog entry as the admin API returns it. Only the fields
// the CLI shows are kept; the provider credential is never among them.
type AdminModel struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ProviderType string `json:"provider_type"`
	APIURL       string `json:"api_url"`
	Model        string `json:"model"`
	Enabled      bool   `json:"enabled"`
}

type adminModelsResponse struct {
	Models       []AdminModel `json:"models"`
	DefaultModel string       `json:"default_model,omitempty"`
}

// CreateModelInput mirrors the admin API's create body, which mirrors
// `buildmax-server model add`. Prices are strings in the model's currency and
// are resolved server-side. api_key is write-only: it is sent and never read
// back. Empty optional fields are omitted so the server applies its defaults.
type CreateModelInput struct {
	Name            string   `json:"name"`
	ProviderType    string   `json:"provider_type,omitempty"`
	APIURL          string   `json:"api_url"`
	APIKey          string   `json:"api_key,omitempty"`
	Model           string   `json:"model"`
	ContextWindow   int      `json:"context_window,omitempty"`
	CallTimeout     int      `json:"call_timeout,omitempty"`
	MaxTokens       int      `json:"max_tokens,omitempty"`
	Reasoning       string   `json:"reasoning,omitempty"`
	CacheMode       string   `json:"cache_mode,omitempty"`
	CacheTTL        string   `json:"cache_ttl,omitempty"`
	Currency        string   `json:"currency,omitempty"`
	InputPrice      string   `json:"input_price,omitempty"`
	CacheReadPrice  string   `json:"cache_read_price,omitempty"`
	CacheWritePrice string   `json:"cache_write_price,omitempty"`
	OutputPrice     string   `json:"output_price,omitempty"`
	Vision          bool     `json:"vision,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`
}

// ListModels returns the catalog, enabled or not, and the default model's name.
func (c *Client) ListModels(ctx context.Context, token string) ([]AdminModel, string, error) {
	var out adminModelsResponse
	if err := c.getJSON(ctx, token, "/api/admin/llm/models", &out); err != nil {
		return nil, "", err
	}
	return out.Models, out.DefaultModel, nil
}

// CreateModel adds a catalog model and returns it. The credential in the input
// travels in the request body alone and is returned by no read.
func (c *Client) CreateModel(ctx context.Context, token string, in CreateModelInput) (*AdminModel, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	resp, err := c.do(ctx, http.MethodPost, token, "/api/admin/llm/models", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return nil, httpclient.DecodeError(resp, "")
	}
	var out AdminModel
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}

// SetModelEnabled enables or disables a catalog model and returns its new state.
func (c *Client) SetModelEnabled(ctx context.Context, token, modelID string, enabled bool) (*AdminModel, error) {
	payload, err := json.Marshal(struct {
		Enabled bool `json:"enabled"`
	}{Enabled: enabled})
	if err != nil {
		return nil, err
	}
	path := "/api/admin/llm/models/" + url.PathEscape(modelID) + "/state"
	resp, err := c.do(ctx, http.MethodPut, token, path, "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, httpclient.DecodeError(resp, "")
	}
	var out AdminModel
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}

// ReplaceModelCredential rotates a catalog model's upstream key in place.
func (c *Client) ReplaceModelCredential(ctx context.Context, token, modelID, apiKey string) (*AdminModel, error) {
	payload, err := json.Marshal(struct {
		APIKey string `json:"api_key"`
	}{APIKey: apiKey})
	if err != nil {
		return nil, err
	}
	path := "/api/admin/llm/models/" + url.PathEscape(modelID) + "/credential"
	resp, err := c.do(ctx, http.MethodPut, token, path, "application/json", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, httpclient.DecodeError(resp, "")
	}
	var out AdminModel
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}
