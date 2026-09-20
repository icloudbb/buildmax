package tool

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/llm"
)

func TestWebSearchReturnsBoundedSources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v2/search" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("Authorization = %q", got)
		}
		var request struct {
			Query string `json:"query"`
			Limit int    `json:"limit"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if request.Query != "BuildMax agent" || request.Limit != 5 {
			t.Errorf("request = %+v", request)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"success":true,"data":{"web":[{"url":"https://example.com/a","title":" First  result ","description":"Useful  excerpt"},{"url":"javascript:alert(1)","title":"bad"},{"url":"https://example.com/b","title":"Second","description":"More"}]}}`))
	}))
	defer server.Close()

	search := NewWebSearch("test-key")
	search.endpoint = server.URL + "/v2/search"
	got, err := search.Execute(context.Background(), map[string]any{"query": " BuildMax agent "})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "1. First result\nURL: https://example.com/a\nExcerpt: Useful excerpt") ||
		!strings.Contains(got, "2. Second\nURL: https://example.com/b") ||
		strings.Contains(got, "javascript:") {
		t.Errorf("unexpected result: %q", got)
	}
}

func TestWebSearchKeylessLimitExplained(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("keyless request sent Authorization")
		}
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	search := NewWebSearch("")
	search.endpoint = server.URL
	_, err := search.Execute(context.Background(), map[string]any{"query": "example"})
	if err == nil || !strings.Contains(err.Error(), "web_search.api_key") {
		t.Fatalf("error = %v", err)
	}
}

func TestWebSearchHonorsSandboxBeforeRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("blocked host was contacted")
	}))
	defer server.Close()
	search := NewWebSearch("").WithSandbox(denyAllSandbox{})
	search.endpoint = server.URL
	_, err := search.Execute(context.Background(), map[string]any{"query": "example"})
	if err == nil || !strings.Contains(err.Error(), "sandbox: blocked") {
		t.Fatalf("error = %v", err)
	}
}

func TestWebSearchRejectsRedirect(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("redirect target was contacted")
	}))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	search := NewWebSearch("")
	search.endpoint = source.URL
	_, err := search.Execute(context.Background(), map[string]any{"query": "example"})
	if err == nil || !strings.Contains(err.Error(), "HTTP 307") {
		t.Fatalf("error = %v", err)
	}
}

func TestWebSearchQueryValidation(t *testing.T) {
	search := NewWebSearch("")
	for _, query := range []any{"", 123, strings.Repeat("a", webSearchMaxQuery+1)} {
		_, err := search.Execute(context.Background(), map[string]any{"query": query})
		if err == nil {
			t.Errorf("query %v was accepted", query)
		}
	}
	if search.Name() != ToolNameWebSearch || search.Access(nil) != llm.AccessReadOnly {
		t.Error("WebSearch tool contract changed")
	}
}
