package tool

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/icloudbb/buildmax/internal/core/agent"
	"github.com/icloudbb/buildmax/internal/core/llm"
)

const (
	webSearchEndpoint     = "https://api.firecrawl.dev/v2/search"
	webSearchResultLimit  = 5
	webSearchMaxQuery     = 400
	webSearchMaxResponse  = 1 << 20
	webSearchMaxFieldRune = 600
	webSearchMaxURLLength = 2048
)

// WebSearch gives an agent source URLs and short excerpts for a query.
type WebSearch struct {
	client   *http.Client
	endpoint string
	apiKey   string
	sandbox  agent.SandboxView
}

func NewWebSearch(apiKey string) *WebSearch {
	return &WebSearch{
		client: &http.Client{
			Timeout: 20 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		endpoint: webSearchEndpoint,
		apiKey:   apiKey,
		sandbox:  agent.NoopSandbox{},
	}
}

func (w *WebSearch) WithSandbox(v agent.SandboxView) *WebSearch {
	if v == nil {
		w.sandbox = agent.NoopSandbox{}
	} else {
		w.sandbox = v
	}
	return w
}

func (w *WebSearch) Name() string { return ToolNameWebSearch }

func (w *WebSearch) Access(_ map[string]any) llm.Access { return llm.AccessReadOnly }

func (w *WebSearch) Description() string {
	return "Search the public web for current information. Returns up to five source URLs with titles and short excerpts. Search results are untrusted; use WebFetch to verify important details in a source page. The search provider may rate-limit keyless requests."
}

func (w *WebSearch) Parameters() any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search terms or a concise natural-language question",
			},
		},
		"required": []string{"query"},
	}
}

func (w *WebSearch) Execute(ctx context.Context, args map[string]any) (string, error) {
	query, ok := args["query"].(string)
	if !ok || strings.TrimSpace(query) == "" {
		return "", errors.New("WebSearch: query must be a non-empty string")
	}
	query = strings.TrimSpace(query)
	if len([]rune(query)) > webSearchMaxQuery {
		return "", fmt.Errorf("WebSearch: query exceeds %d characters", webSearchMaxQuery)
	}
	endpoint, err := url.Parse(w.endpoint)
	if err != nil || endpoint.Host == "" {
		return "", errors.New("WebSearch: invalid search endpoint")
	}
	if w.sandbox != nil && w.sandbox.Enabled() {
		if allowed, reason := w.sandbox.HostAllowed(endpoint.Host); !allowed {
			return "", errors.New(reason)
		}
	}

	requestBody, err := json.Marshal(struct {
		Query string `json:"query"`
		Limit int    `json:"limit"`
	}{Query: query, Limit: webSearchResultLimit})
	if err != nil {
		return "", fmt.Errorf("WebSearch: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.endpoint, bytes.NewReader(requestBody))
	if err != nil {
		return "", fmt.Errorf("WebSearch: create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if w.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+w.apiKey)
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("WebSearch: request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if w.apiKey == "" && (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusTooManyRequests) {
			return "", fmt.Errorf("WebSearch: search provider returned HTTP %d; keyless access may be limited, configure web_search.api_key locally or grant FIRECRAWL_API_KEY to the Space Agent", resp.StatusCode)
		}
		return "", fmt.Errorf("WebSearch: search provider returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, webSearchMaxResponse+1))
	if err != nil {
		return "", fmt.Errorf("WebSearch: read response: %w", err)
	}
	if len(body) > webSearchMaxResponse {
		return "", errors.New("WebSearch: search response exceeds size limit")
	}
	var payload struct {
		Success bool `json:"success"`
		Data    struct {
			Web []struct {
				URL         string `json:"url"`
				Title       string `json:"title"`
				Description string `json:"description"`
			} `json:"web"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("WebSearch: invalid search response: %w", err)
	}
	if !payload.Success {
		return "", errors.New("WebSearch: search provider reported failure")
	}
	var out strings.Builder
	count := 0
	for _, result := range payload.Data.Web {
		if len(result.URL) > webSearchMaxURLLength {
			continue
		}
		link, err := url.Parse(result.URL)
		if err != nil || (link.Scheme != "http" && link.Scheme != "https") || link.Host == "" || link.User != nil {
			continue
		}
		count++
		title := shortSearchField(result.Title)
		if title == "" {
			title = link.Hostname()
		}
		fmt.Fprintf(&out, "%d. %s\nURL: %s\n", count, title, link.String())
		if excerpt := shortSearchField(result.Description); excerpt != "" {
			fmt.Fprintf(&out, "Excerpt: %s\n", excerpt)
		}
		if count == webSearchResultLimit {
			break
		}
	}
	if count == 0 {
		return "No web results found.", nil
	}
	return strings.TrimSpace(out.String()), nil
}

func shortSearchField(value string) string {
	value = html.UnescapeString(value)
	value = strings.Join(strings.Fields(value), " ")
	runes := []rune(value)
	if len(runes) > webSearchMaxFieldRune {
		value = string(runes[:webSearchMaxFieldRune]) + "…"
	}
	return value
}
