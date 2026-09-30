package llmremote_test

import (
	"context"
	"encoding/json"
	"testing"

	cllm "github.com/icloudbb/buildmax/internal/core/llm"
	"github.com/icloudbb/buildmax/internal/infra/llmremote"
)

var labelOutput = &cllm.OutputSchema{Name: "output", Schema: json.RawMessage(`{"type":"object"}`)}

// The output schema is sent on the request; without it the gateway calls the
// provider for free text and a managed run can never return a structured value.
func TestRequestCarriesTheOutputSchema(t *testing.T) {
	gateway := newFakeGateway(t)
	gateway.body = `{"llm_call_id":"lc_1","model":"fast","content":"{}"}`

	client := gateway.client(llmremote.Config{Token: "run-token", TaskRunID: "r_1"})
	if _, err := client.ChatCompletionBlocking(context.Background(), cllm.Request{
		Messages: []cllm.Message{{Role: "user", Content: "classify"}},
		Output:   labelOutput,
	}); err != nil {
		t.Fatalf("ChatCompletionBlocking: %v", err)
	}
	got := gateway.gotBody.Output
	if got == nil || got.Name != "output" || string(got.Schema) != `{"type":"object"}` {
		t.Errorf("request output = %+v, want the requested schema", got)
	}
}

// A request with no schema sends none: free text stays free text.
func TestRequestWithoutSchemaSendsNoOutput(t *testing.T) {
	gateway := newFakeGateway(t)
	gateway.body = `{"llm_call_id":"lc_1","model":"fast","content":"hi"}`

	client := gateway.client(llmremote.Config{Token: "tok"})
	if _, err := client.ChatCompletionBlocking(context.Background(),
		cllm.Request{Messages: []cllm.Message{{Role: "user", Content: "hi"}}}); err != nil {
		t.Fatalf("ChatCompletionBlocking: %v", err)
	}
	if gateway.gotBody.Output != nil {
		t.Errorf("request output = %+v, want none", gateway.gotBody.Output)
	}
}

// The gateway's verdict comes back as the same core value a direct provider
// client returns: a validated value, or a typed failure with no value.
func TestResponseStructuredBecomesTheCoreValue(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantValue string
		wantErr   bool
		enforced  bool
	}{
		{
			name:      "validated value",
			body:      `{"llm_call_id":"lc_1","model":"fast","content":"{\"label\":\"bug\"}","structured":{"value":{"label":"bug"},"mode":"native","enforced":true}}`,
			wantValue: `{"label":"bug"}`,
			enforced:  true,
		},
		{
			name:    "typed failure",
			body:    `{"llm_call_id":"lc_1","model":"fast","content":"nope","structured":{"mode":"native","error":"model output did not validate against the schema"}}`,
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gateway := newFakeGateway(t)
			gateway.body = tc.body
			client := gateway.client(llmremote.Config{Token: "tok"})
			completion, err := client.ChatCompletionBlocking(context.Background(), cllm.Request{
				Messages: []cllm.Message{{Role: "user", Content: "classify"}}, Output: labelOutput,
			})
			if err != nil {
				t.Fatalf("ChatCompletionBlocking: %v", err)
			}
			s := completion.Structured
			if s == nil {
				t.Fatal("completion carries no structured result")
			}
			if s.Mode != cllm.StructuredNative || s.Enforced != tc.enforced {
				t.Errorf("mode = %q enforced = %v", s.Mode, s.Enforced)
			}
			if tc.wantErr {
				if s.Err == nil || s.Value != nil {
					t.Errorf("structured = %+v, want a typed failure with no value", s)
				}
				return
			}
			if s.Err != nil || string(s.Value) != tc.wantValue {
				t.Errorf("structured = %+v, want value %s", s, tc.wantValue)
			}
		})
	}
}

// A streamed call's result event carries the structured value too.
func TestStreamResultCarriesStructured(t *testing.T) {
	gateway := newFakeGateway(t)
	gateway.body = "event: delta\ndata: {\"content\":\"{\\\"label\\\":\"}\n\n" +
		"event: result\ndata: {\"content\":\"{\\\"label\\\":\\\"bug\\\"}\",\"structured\":{\"value\":{\"label\":\"bug\"},\"mode\":\"forced_tool\",\"enforced\":true}}\n\n"

	client := gateway.client(llmremote.Config{Token: "tok"})
	completion, err := client.ChatCompletionStreaming(context.Background(), cllm.Request{
		Messages: []cllm.Message{{Role: "user", Content: "classify"}}, Output: labelOutput,
	}, nil)
	if err != nil {
		t.Fatalf("ChatCompletionStreaming: %v", err)
	}
	s := completion.Structured
	if s == nil || s.Err != nil || string(s.Value) != `{"label":"bug"}` || s.Mode != cllm.StructuredForcedTool {
		t.Errorf("structured = %+v, want the result event's value", s)
	}
	if gateway.gotBody.Output == nil {
		t.Error("streamed request did not carry the output schema")
	}
}
