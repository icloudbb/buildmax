package llm

import (
	"context"
	"strings"
	"testing"

	cllm "github.com/icloudbb/buildmax/internal/core/llm"
)

// personSchema is a small object schema in the shared subset.
const personSchema = `{"type":"object","additionalProperties":false,"properties":{"name":{"type":"string"},"age":{"type":"integer"}},"required":["name"]}`

func personOutput() *cllm.OutputSchema {
	return &cllm.OutputSchema{Name: "person", Schema: []byte(personSchema)}
}

// openAIStructuredProtocols is the subset of protocols that map structured
// output natively in Phase 1.
var openAIStructuredProtocols = []protocol{protocols[0], protocols[1]}

func TestStructuredOutputRequestCarriesSchema(t *testing.T) {
	for _, p := range openAIStructuredProtocols {
		t.Run(p.provider, func(t *testing.T) {
			up := newUpstream(t, p, reply{text: `{"name":"Ada"}`}, 0)
			client := newTestClient(t, p.provider, up.server.URL)

			_, err := client.ChatCompletionBlocking(context.Background(),
				cllm.Request{Messages: conformanceHistory(), Output: personOutput()})
			if err != nil {
				t.Fatalf("ChatCompletionBlocking: %v", err)
			}
			body := up.bodies[len(up.bodies)-1]
			for _, want := range []string{"json_schema", `"strict":true`, `"name":"person"`, `"additionalProperties":false`} {
				if !strings.Contains(body, want) {
					t.Errorf("request body missing %q\nbody: %s", want, body)
				}
			}
		})
	}
}

func TestStructuredOutputValidValue(t *testing.T) {
	const value = `{"name":"Ada","age":36}`
	for _, p := range openAIStructuredProtocols {
		for _, mode := range []string{"blocking", "streaming"} {
			t.Run(p.provider+"/"+mode, func(t *testing.T) {
				up := newUpstream(t, p, reply{text: value}, 0)
				client := newTestClient(t, p.provider, up.server.URL)
				completion := callStructured(t, client, mode, personOutput())

				if completion.Structured == nil {
					t.Fatal("completion.Structured is nil")
				}
				s := completion.Structured
				if s.Err != nil {
					t.Fatalf("unexpected StructuredError: %v", s.Err)
				}
				if s.Mode != cllm.StructuredNative {
					t.Errorf("Mode = %q, want native", s.Mode)
				}
				if !s.Enforced {
					t.Error("Enforced = false, want true for native")
				}
				if string(s.Value) != value {
					t.Errorf("Value = %s, want %s", s.Value, value)
				}
			})
		}
	}
}

func TestStructuredOutputOffSchemaValueIsTypedFailure(t *testing.T) {
	// age is a string, violating the schema; the model returned it anyway.
	const value = `{"name":"Ada","age":"old"}`
	for _, p := range openAIStructuredProtocols {
		t.Run(p.provider, func(t *testing.T) {
			up := newUpstream(t, p, reply{text: value}, 0)
			client := newTestClient(t, p.provider, up.server.URL)
			completion := callStructured(t, client, "blocking", personOutput())

			if completion.Structured == nil {
				t.Fatal("completion.Structured is nil")
			}
			s := completion.Structured
			if s.Err == nil {
				t.Fatal("expected a StructuredError for an off-schema value")
			}
			if s.Value != nil {
				t.Errorf("Value = %s, want nil on failure", s.Value)
			}
			if s.Enforced {
				t.Error("Enforced = true, want false on failure")
			}
			// The text output is still the whole turn, even when it did not validate.
			if completion.Content != value {
				t.Errorf("Content = %q, want the whole turn %q", completion.Content, value)
			}
		})
	}
}

func TestStructuredOutputUnsupportedSchemaIsTypedFailure(t *testing.T) {
	unsupported := &cllm.OutputSchema{Name: "bad", Schema: []byte(`{"type":"string","pattern":"x"}`)}
	up := newUpstream(t, protocols[0], reply{text: `"ok"`}, 0)
	client := newTestClient(t, protocols[0].provider, up.server.URL)
	completion := callStructured(t, client, "blocking", unsupported)

	if completion.Structured == nil || completion.Structured.Err == nil {
		t.Fatal("expected a StructuredError for an out-of-subset schema")
	}
	if !strings.Contains(completion.Structured.Err.Error(), "supported subset") {
		t.Errorf("error %q does not explain the subset violation", completion.Structured.Err.Error())
	}
}

// TestNoOutputLeavesStructuredNil pins that a request without an Output schema is
// unchanged: no structured value and no response_format on the wire.
func TestNoOutputLeavesStructuredNil(t *testing.T) {
	for _, p := range openAIStructuredProtocols {
		t.Run(p.provider, func(t *testing.T) {
			up := newUpstream(t, p, reply{text: "plain text"}, 0)
			client := newTestClient(t, p.provider, up.server.URL)
			completion, err := client.ChatCompletionBlocking(context.Background(),
				cllm.Request{Messages: conformanceHistory()})
			if err != nil {
				t.Fatalf("ChatCompletionBlocking: %v", err)
			}
			if completion.Structured != nil {
				t.Errorf("Structured = %+v, want nil without an Output schema", completion.Structured)
			}
			if strings.Contains(up.bodies[len(up.bodies)-1], "json_schema") {
				t.Error("request carried a json_schema format without an Output schema")
			}
		})
	}
}

func callStructured(t *testing.T, client *Client, mode string, output *cllm.OutputSchema) cllm.Completion {
	t.Helper()
	req := cllm.Request{Messages: conformanceHistory(), Output: output}
	var (
		completion cllm.Completion
		err        error
	)
	switch mode {
	case "streaming":
		completion, err = client.ChatCompletionStreaming(context.Background(), req, nil)
	default:
		completion, err = client.ChatCompletionBlocking(context.Background(), req)
	}
	if err != nil {
		t.Fatalf("ChatCompletion(%s): %v", mode, err)
	}
	return completion
}
