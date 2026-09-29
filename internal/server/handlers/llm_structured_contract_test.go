package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	cllm "github.com/icloudbb/buildmax/internal/core/llm"
)

// recordingUpstream is an OpenAI-compatible provider serving one canned
// response and recording every request body, so a test can see what reached
// the provider through each path.
type recordingUpstream struct {
	mu     sync.Mutex
	bodies []string
}

func newRecordingUpstream(t *testing.T, body string) (*recordingUpstream, *httptest.Server) {
	t.Helper()
	rec := &recordingUpstream{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		rec.bodies = append(rec.bodies, string(raw))
		rec.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return rec, server
}

func (r *recordingUpstream) last() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.bodies) == 0 {
		return ""
	}
	return r.bodies[len(r.bodies)-1]
}

// A managed call with an output schema must reach the provider constrained and
// return the verdict a direct call to the same provider returns: validated once,
// by the server's provider client, for a conforming and an off-schema answer
// alike. This is the hop a managed worker's extraction call crosses.
func TestManagedStructuredOutputMatchesADirectCall(t *testing.T) {
	output := &cllm.OutputSchema{Name: "output", Schema: json.RawMessage(
		`{"type":"object","properties":{"label":{"type":"string"}},"required":["label"],"additionalProperties":false}`)}
	tests := []struct {
		name      string
		content   string
		wantValue string
	}{
		{name: "conforming answer", content: `{\"label\":\"bug\"}`, wantValue: `{"label":"bug"}`},
		{name: "off-schema answer", content: `{\"severity\":3}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec, upstream := newRecordingUpstream(t, `{
				"id":"chatcmpl-s","object":"chat.completion","created":1,"model":"vendor/x",
				"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"`+tc.content+`"}}],
				"usage":{"prompt_tokens":2,"completion_tokens":2,"total_tokens":4}
			}`)
			req := cllm.Request{Messages: []cllm.Message{{Role: "user", Content: "classify"}}, Output: output}

			want, err := directClient(t, upstream.URL).ChatCompletionBlocking(context.Background(), req)
			if err != nil {
				t.Fatalf("direct: %v", err)
			}
			got, err := managedGateway(t, upstream.URL).ChatCompletionBlocking(context.Background(), req)
			if err != nil {
				t.Fatalf("managed: %v", err)
			}
			if !strings.Contains(rec.last(), `"json_schema"`) {
				t.Errorf("managed upstream request was not schema-constrained:\n%s", rec.last())
			}

			if got.Structured == nil || want.Structured == nil {
				t.Fatalf("structured: managed %+v, direct %+v", got.Structured, want.Structured)
			}
			if got.Structured.Mode != want.Structured.Mode || got.Structured.Enforced != want.Structured.Enforced {
				t.Errorf("mode/enforced: managed %q/%v, direct %q/%v", got.Structured.Mode, got.Structured.Enforced,
					want.Structured.Mode, want.Structured.Enforced)
			}
			if tc.wantValue == "" {
				if got.Structured.Err == nil || got.Structured.Value != nil {
					t.Errorf("managed structured = %+v, want the typed failure a direct call gets (%+v)", got.Structured, want.Structured)
				}
				return
			}
			if got.Structured.Err != nil || string(got.Structured.Value) != tc.wantValue {
				t.Errorf("managed structured = %+v, want value %s", got.Structured, tc.wantValue)
			}
		})
	}
}
