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

// structuredCase describes how one protocol carries a structured answer, so the
// same assertions run against every provider's native mechanism.
type structuredCase struct {
	p             protocol
	wantMode      cllm.StructuredMode
	carriesAsText bool // the value is the model's text (not a tool input)
	// replyFor renders a model turn whose structured answer is value.
	replyFor func(value string) reply
	// wantInBody is what the outgoing request must contain to have asked for it.
	wantInBody []string
}

func structuredCases() []structuredCase {
	textReply := func(value string) reply { return reply{text: value} }
	return []structuredCase{
		{protocols[0], cllm.StructuredNative, true, textReply, []string{"json_schema", `"strict":true`, `"name":"person"`}},
		{protocols[1], cllm.StructuredNative, true, textReply, []string{"json_schema", `"strict":true`, `"name":"person"`}},
		{protocols[3], cllm.StructuredNative, true, textReply, []string{`"format"`, `"additionalProperties":false`}},
		{
			p:        protocols[2],
			wantMode: cllm.StructuredForcedTool,
			replyFor: func(value string) reply {
				return reply{toolCalls: []cllm.ToolCall{{ID: "toolu_1", Name: "person", Arguments: value}}}
			},
			wantInBody: []string{`"tool_choice"`, `"name":"person"`, `"additionalProperties":false`},
		},
	}
}

func TestStructuredOutputRequestCarriesMechanism(t *testing.T) {
	for _, tc := range structuredCases() {
		t.Run(tc.p.provider, func(t *testing.T) {
			up := newUpstream(t, tc.p, tc.replyFor(`{"name":"Ada"}`), 0)
			client := newTestClient(t, tc.p.provider, up.server.URL)

			_, err := client.ChatCompletionBlocking(context.Background(),
				cllm.Request{Messages: conformanceHistory(), Output: personOutput()})
			if err != nil {
				t.Fatalf("ChatCompletionBlocking: %v", err)
			}
			body := up.bodies[len(up.bodies)-1]
			for _, want := range tc.wantInBody {
				if !strings.Contains(body, want) {
					t.Errorf("request body missing %q\nbody: %s", want, body)
				}
			}
		})
	}
}

func TestStructuredOutputValidValue(t *testing.T) {
	const value = `{"name":"Ada","age":36}`
	for _, tc := range structuredCases() {
		modes := []string{"blocking", "streaming"}
		if !tc.carriesAsText {
			// The forced-tool value is assembled from the finished message either
			// way; one path is enough and the streaming fixture carries tool input
			// through the same accumulator the blocking one reads.
			modes = []string{"blocking"}
		}
		for _, mode := range modes {
			t.Run(tc.p.provider+"/"+mode, func(t *testing.T) {
				up := newUpstream(t, tc.p, tc.replyFor(value), 0)
				client := newTestClient(t, tc.p.provider, up.server.URL)
				completion := callStructured(t, client, mode, personOutput())

				s := completion.Structured
				if s == nil {
					t.Fatal("completion.Structured is nil")
				}
				if s.Err != nil {
					t.Fatalf("unexpected StructuredError: %v", s.Err)
				}
				if s.Mode != tc.wantMode {
					t.Errorf("Mode = %q, want %q", s.Mode, tc.wantMode)
				}
				if !s.Enforced {
					t.Error("Enforced = false, want true")
				}
				if string(s.Value) != value {
					t.Errorf("Value = %s, want %s", s.Value, value)
				}
				// The forced tool is the run reporting, not a tool for the caller.
				if len(completion.ToolCalls) != 0 {
					t.Errorf("ToolCalls = %v, want none", completion.ToolCalls)
				}
			})
		}
	}
}

func TestStructuredOutputOffSchemaValueIsTypedFailure(t *testing.T) {
	// age is a string, violating the schema; the model returned it anyway.
	const value = `{"name":"Ada","age":"old"}`
	for _, tc := range structuredCases() {
		t.Run(tc.p.provider, func(t *testing.T) {
			up := newUpstream(t, tc.p, tc.replyFor(value), 0)
			client := newTestClient(t, tc.p.provider, up.server.URL)
			completion := callStructured(t, client, "blocking", personOutput())

			s := completion.Structured
			if s == nil {
				t.Fatal("completion.Structured is nil")
			}
			if s.Err == nil {
				t.Fatal("expected a StructuredError for an off-schema value")
			}
			if s.Value != nil {
				t.Errorf("Value = %s, want nil on failure", s.Value)
			}
			if s.Enforced {
				t.Error("Enforced = true, want false on failure")
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

// TestStructuredForcedToolMissingIsTypedFailure pins the honest failure when a
// provider that maps to a forced tool did not make the call.
func TestStructuredForcedToolMissingIsTypedFailure(t *testing.T) {
	up := newUpstream(t, protocols[2], reply{text: "I would rather answer in prose."}, 0)
	client := newTestClient(t, protocols[2].provider, up.server.URL)
	completion := callStructured(t, client, "blocking", personOutput())

	if completion.Structured == nil || completion.Structured.Err == nil {
		t.Fatal("expected a StructuredError when the forced tool was not called")
	}
	if completion.Structured.Enforced {
		t.Error("Enforced = true, want false when the value never arrived")
	}
}

// TestNoOutputLeavesStructuredNil pins that a request without an Output schema is
// unchanged across every provider: no structured value on the completion.
func TestNoOutputLeavesStructuredNil(t *testing.T) {
	for _, p := range protocols {
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
