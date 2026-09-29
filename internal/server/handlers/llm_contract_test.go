package handlers

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
	llm "github.com/icloudbb/buildmax/internal/infra/llm"
	"github.com/icloudbb/buildmax/internal/infra/llmremote"
	"github.com/icloudbb/buildmax/internal/service/llmgateway"
	"github.com/icloudbb/buildmax/internal/testsupport"
)

// The managed contract: a call through the gateway must produce the same core
// content, tool calls, and usage as a direct call to the same provider. Both
// paths here end at one fake upstream, so a difference can only come from the
// protocol, the handler, or the remote client.
//
// This test lives in the server package because it drives the real handler with
// the real client. internal/infra may not import internal/server, and the
// architecture boundary test reads test files too.

// fakeUpstream is an OpenAI-compatible provider serving one canned response.
func fakeUpstream(t *testing.T, body string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server
}

// managedGateway starts the real handler over a catalog whose single target is
// the given upstream, and returns a managed client pointed at it.
func managedGateway(t *testing.T, upstreamURL string) *llmremote.Client {
	t.Helper()
	return managedGatewayWith(t, upstreamURL, 10*time.Second, &llmStubLedger{})
}

// managedGatewayWith is managedGateway with the target's call timeout and the
// ledger chosen by the test.
func managedGatewayWith(t *testing.T, upstreamURL string, callTimeout time.Duration, ledger *llmStubLedger) *llmremote.Client {
	t.Helper()

	target := llmgateway.Target{
		ID:            "mt_fast",
		Name:          "Fast",
		ProviderType:  cllm.ProviderOpenAICompatible,
		Endpoint:      upstreamURL,
		CredentialRef: "ref",
		UpstreamModel: "vendor/x",
		Capabilities:  llmgateway.NewCapabilitySet(llmgateway.BaselineCapabilities()...),
		Enabled:       true,
	}
	catalog, err := llmgateway.NewStaticCatalog([]llmgateway.Target{target})
	if err != nil {
		t.Fatalf("NewStaticCatalog: %v", err)
	}
	svc := &llmgateway.Service{
		Router: &llmgateway.Router{
			Resolver: &llmgateway.Resolver{Catalog: catalog, DefaultModel: "Fast"},
			Factory: func(_ context.Context, target llmgateway.Target) (cllm.LLMClient, error) {
				return directClientWith(t, target.Endpoint, callTimeout), nil
			},
		},
		Ledger: ledger,
	}

	h := NewHandler(Config{
		JWTSecret:  llmTestSecret,
		SpaceStore: llmTestSpaceStore(),
		LLMGateway: svc,
	})
	mux := http.NewServeMux()
	h.Register(mux)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return llmremote.NewClient(llmremote.Config{
		ServerURL:   server.URL,
		Token:       testsupport.SignJWT(llmTestUser, llmTestSecret),
		CallTimeout: 10 * time.Second,
	})
}

func directClient(t *testing.T, upstreamURL string) cllm.LLMClient {
	t.Helper()
	return directClientWith(t, upstreamURL, 10*time.Second)
}

func directClientWith(t *testing.T, upstreamURL string, callTimeout time.Duration) cllm.LLMClient {
	t.Helper()
	client, err := llm.NewClient(llm.Config{
		APIKey:      "upstream-key",
		BaseURL:     upstreamURL,
		Model:       "vendor/x",
		CallTimeout: callTimeout,
	})
	if err != nil {
		t.Fatalf("build direct client: %v", err)
	}
	return client
}

func TestManagedCallMatchesADirectCall(t *testing.T) {
	tests := []struct {
		name     string
		upstream string
	}{
		{
			name: "content, tool calls, and usage",
			upstream: `{
				"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"vendor/x",
				"choices":[{"index":0,"finish_reason":"tool_calls","message":{
					"role":"assistant","content":"hi there",
					"tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"a\"}"}}]
				}}],
				"usage":{"prompt_tokens":10,"completion_tokens":4,"total_tokens":14}
			}`,
		},
		{
			name: "content only",
			upstream: `{
				"id":"chatcmpl-2","object":"chat.completion","created":1,"model":"vendor/x",
				"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"plain answer"}}],
				"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}
			}`,
		},
		{
			name: "provider reports no usage",
			upstream: `{
				"id":"chatcmpl-3","object":"chat.completion","created":1,"model":"vendor/x",
				"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"no usage here"}}]
			}`,
		},
		{
			name: "empty content with a tool call only",
			upstream: `{
				"id":"chatcmpl-4","object":"chat.completion","created":1,"model":"vendor/x",
				"choices":[{"index":0,"finish_reason":"tool_calls","message":{
					"role":"assistant","content":"",
					"tool_calls":[{"id":"call_9","type":"function","function":{"name":"bash","arguments":"{}"}}]
				}}],
				"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
			}`,
		},
	}

	messages := []cllm.Message{
		{Role: "system", Content: "be brief"},
		{Role: "user", Content: "hello"},
	}
	tools := []cllm.ToolDef{{
		Name:        "read_file",
		Description: "reads a file",
		Parameters:  map[string]any{"type": "object"},
	}}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			upstream := fakeUpstream(t, tc.upstream)

			want, err := directClient(t, upstream.URL).
				ChatCompletionBlocking(context.Background(), cllm.Request{Messages: messages, Tools: tools})
			if err != nil {
				t.Fatalf("direct call: %v", err)
			}

			got, err := managedGateway(t, upstream.URL).
				ChatCompletionBlocking(context.Background(), cllm.Request{Messages: messages, Tools: tools})
			if err != nil {
				t.Fatalf("managed call: %v", err)
			}

			if got.Content != want.Content {
				t.Errorf("content: managed %q, direct %q", got.Content, want.Content)
			}
			if got.Usage != want.Usage {
				t.Errorf("usage: managed %+v, direct %+v", got.Usage, want.Usage)
			}
			if len(got.ToolCalls) != len(want.ToolCalls) {
				t.Fatalf("tool calls: managed %d, direct %d", len(got.ToolCalls), len(want.ToolCalls))
			}
			for i := range want.ToolCalls {
				if got.ToolCalls[i] != want.ToolCalls[i] {
					t.Errorf("tool call %d: managed %+v, direct %+v", i, got.ToolCalls[i], want.ToolCalls[i])
				}
			}
		})
	}
}

// fakeStreamingUpstream is an OpenAI-compatible provider that streams.
func fakeStreamingUpstream(t *testing.T, chunks []string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("the test server cannot flush")
			return
		}
		for _, chunk := range chunks {
			_, _ = w.Write([]byte("data: " + chunk + "\n\n"))
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	t.Cleanup(server.Close)
	return server
}

func TestManagedStreamMatchesADirectStream(t *testing.T) {
	chunks := []string{
		`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"Hel"}}]}`,
		`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"lo th"}}]}`,
		`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"ere"}}]}`,
		`{"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`,
	}
	upstream := fakeStreamingUpstream(t, chunks)
	messages := []cllm.Message{{Role: "user", Content: "hi"}}

	var directDeltas []string
	want, err := directClient(t, upstream.URL).ChatCompletionStreaming(
		context.Background(), cllm.Request{Messages: messages},
		func(d string) { directDeltas = append(directDeltas, d) })
	if err != nil {
		t.Fatalf("direct stream: %v", err)
	}

	var managedDeltas []string
	got, err := managedGateway(t, upstream.URL).ChatCompletionStreaming(
		context.Background(), cllm.Request{Messages: messages},
		func(d string) { managedDeltas = append(managedDeltas, d) })
	if err != nil {
		t.Fatalf("managed stream: %v", err)
	}

	if got.Content != want.Content {
		t.Errorf("content: managed %q, direct %q", got.Content, want.Content)
	}
	if got.Usage != want.Usage {
		t.Errorf("usage: managed %+v, direct %+v", got.Usage, want.Usage)
	}
	// Deltas must survive the extra hop intact, not merely add up to the same
	// text: a managed stream that batched everything would still pass a
	// content-only check while feeling nothing like a direct one.
	if len(managedDeltas) != len(directDeltas) {
		t.Fatalf("delta count: managed %d %v, direct %d %v",
			len(managedDeltas), managedDeltas, len(directDeltas), directDeltas)
	}
	for i := range directDeltas {
		if managedDeltas[i] != directDeltas[i] {
			t.Errorf("delta %d: managed %q, direct %q", i, managedDeltas[i], directDeltas[i])
		}
	}
}

// TestClientDisconnectCancelsTheUpstream is the whole point of propagating a
// context through the gateway: an abandoned call must stop costing tokens, not
// run to completion with nobody listening.
func TestClientDisconnectCancelsTheUpstream(t *testing.T) {
	upstreamCanceled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(`data: {"id":"1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"first"}}]}` + "\n\n"))
		flusher.Flush()

		// Hold the response open until the caller goes away.
		<-r.Context().Done()
		close(upstreamCanceled)
	}))
	t.Cleanup(upstream.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	client := managedGateway(t, upstream.URL)
	done := make(chan error, 1)
	go func() {
		_, err := client.ChatCompletionStreaming(ctx,
			cllm.Request{Messages: []cllm.Message{{Role: "user", Content: "hi"}}},
			func(string) { cancel() })
		done <- err
	}()

	select {
	case <-upstreamCanceled:
	case <-time.After(5 * time.Second):
		t.Fatal("the upstream request was never canceled")
	}

	select {
	case err := <-done:
		if err == nil {
			t.Error("a canceled stream returned success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the managed call did not return after cancellation")
	}
}

// TestManagedClientSatisfiesTheAgentContract proves the two implementations are
// interchangeable where the agent loop expects a client, not merely similar.
func TestManagedClientSatisfiesTheAgentContract(t *testing.T) {
	upstream := fakeUpstream(t, `{
		"id":"chatcmpl-5","object":"chat.completion","created":1,"model":"vendor/x",
		"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],
		"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
	}`)

	clients := map[string]cllm.LLMClient{
		"direct":  directClient(t, upstream.URL),
		"managed": managedGateway(t, upstream.URL),
	}
	for name, client := range clients {
		t.Run(name, func(t *testing.T) {
			completion, err := client.ChatCompletionBlocking(context.Background(),
				cllm.Request{Messages: []cllm.Message{{Role: "user", Content: "hi"}}})
			if err != nil {
				t.Fatalf("ChatCompletionBlocking: %v", err)
			}
			if completion.Content != "ok" || completion.Usage.TotalTokens != 2 {
				t.Errorf("content=%q usage=%+v", completion.Content, completion.Usage)
			}
		})
	}
}

// TestManagedCallHidesTheUpstream checks that nothing about how the deployment
// reaches the provider survives the round trip to the client.
func TestManagedCallHidesTheUpstream(t *testing.T) {
	upstream := fakeUpstream(t, `{
		"id":"chatcmpl-6","object":"chat.completion","created":1,"model":"vendor/x",
		"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"ok"}}],
		"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
	}`)

	client := managedGateway(t, upstream.URL)
	models, err := client.Models(context.Background())
	if err != nil {
		t.Fatalf("Models: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("got %d models, want 1", len(models))
	}
	// The catalog name is what a client learns. The catalog ID and the
	// provider's own model identifier are not.
	if models[0].Name != "Fast" {
		t.Errorf("name = %q", models[0].Name)
	}
	if models[0].Name == "vendor/x" || models[0].Name == "mt_fast" {
		t.Errorf("listing exposed an internal identifier: %q", models[0].Name)
	}
}

// The Beta failure drills, end to end: the real provider client, the real
// handler, and the real remote client must agree on what happened, and the
// ledger must agree with them. A timeout of the model's own call_timeout is a
// provider failure the operator acts on, never a cancellation.
func TestManagedProviderFailuresReachTheClientClassified(t *testing.T) {
	tests := []struct {
		name     string
		handler  http.HandlerFunc
		wantCode string
	}{
		{
			name: "provider timeout",
			handler: func(_ http.ResponseWriter, r *http.Request) {
				// The server notices a departed client only once the body
				// is read; the bound keeps a regression from hanging Close.
				_, _ = io.Copy(io.Discard, r.Body)
				select {
				case <-r.Context().Done():
				case <-time.After(5 * time.Second):
				}
			},
			wantCode: llmgateway.ErrorClassUpstreamTimeout,
		},
		{
			name: "credential refused",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"message":"Incorrect API key provided: upstream-key","type":"invalid_request_error"}}`))
			},
			wantCode: llmgateway.ErrorClassUpstreamAuth,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(tc.handler)
			t.Cleanup(upstream.Close)

			ledger := &llmStubLedger{}
			client := managedGatewayWith(t, upstream.URL, 200*time.Millisecond, ledger)
			_, err := client.ChatCompletionBlocking(context.Background(),
				cllm.Request{Messages: []cllm.Message{{Role: "user", Content: "hi"}}})

			var gwErr *llmremote.GatewayError
			if !errors.As(err, &gwErr) {
				t.Fatalf("want *GatewayError, got %T: %v", err, err)
			}
			if gwErr.Code != tc.wantCode {
				t.Errorf("code = %q, want %q (%v)", gwErr.Code, tc.wantCode, err)
			}
			if strings.Contains(err.Error(), "upstream-key") || strings.Contains(err.Error(), "Incorrect API key") {
				t.Errorf("the provider's text reached the client: %v", err)
			}
			if ledger.outcome.Status != coregw.CallStatusFailed {
				t.Errorf("ledger status = %q, want %q", ledger.outcome.Status, coregw.CallStatusFailed)
			}
			if ledger.outcome.ErrorClass == nil || *ledger.outcome.ErrorClass != tc.wantCode {
				t.Errorf("ledger class = %v, want %q", ledger.outcome.ErrorClass, tc.wantCode)
			}
		})
	}
}

// The remote client mirrors the server's codes because it cannot import them.
// This is the one place both are in reach, so it holds them equal.
func TestRemoteClientCodesMatchTheGatewayClasses(t *testing.T) {
	pairs := [][2]string{
		{llmremote.CodeTargetNotFound, llmgateway.ErrorClassTargetNotFound},
		{llmremote.CodeTargetDisabled, llmgateway.ErrorClassTargetDisabled},
		{llmremote.CodeCapability, llmgateway.ErrorClassCapability},
		{llmremote.CodeQuotaExceeded, llmgateway.ErrorClassQuotaExceeded},
		{llmremote.CodeDuplicateCall, llmgateway.ErrorClassDuplicateCall},
		{llmremote.CodeInvalidRequest, llmgateway.ErrorClassInvalidRequest},
		{llmremote.CodeNotConfigured, llmgateway.ErrorClassNotConfigured},
		{llmremote.CodeCanceled, llmgateway.ErrorClassCanceled},
		{llmremote.CodeUpstream, llmgateway.ErrorClassUpstream},
		{llmremote.CodeUpstreamTimeout, llmgateway.ErrorClassUpstreamTimeout},
		{llmremote.CodeUpstreamAuth, llmgateway.ErrorClassUpstreamAuth},
		{llmremote.CodeUpstreamRateLimited, llmgateway.ErrorClassUpstreamRateLimited},
		{llmremote.CodeInternal, llmgateway.ErrorClassInternal},
	}
	for _, p := range pairs {
		if p[0] != p[1] {
			t.Errorf("remote code %q != gateway class %q", p[0], p[1])
		}
	}
}
