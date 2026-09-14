package llm

import (
	"encoding/json"

	"github.com/icloudbb/buildmax/internal/core/jsonschema"
	cllm "github.com/icloudbb/buildmax/internal/core/llm"
)

// nativeCandidate captures an OpenAI-family model's terminating text as the
// candidate structured value. Both OpenAI protocols return the value as that
// text, and the Client validates it against the schema (finalizeStructured).
// Returns nil when the request asked for no structured output.
func nativeCandidate(req cllm.Request, content string) *cllm.Structured {
	if req.Output == nil {
		return nil
	}
	return &cllm.Structured{
		Value:    json.RawMessage(content),
		Mode:     cllm.StructuredNative,
		Enforced: true,
	}
}

// finalizeStructured validates the candidate value an adapter placed on the
// completion against the requested schema. Validation lives here, once, so no
// consumer re-implements it — and it runs even for a native provider, so a
// provider bug cannot pass an off-schema value (structured-output.md §9).
//
// A schema outside the supported subset, or a value that does not conform, is a
// typed StructuredError on the completion rather than a transport error: the
// call reached the model and it answered; the answer just did not validate.
func finalizeStructured(req cllm.Request, completion *cllm.Completion) {
	if req.Output == nil || completion.Structured == nil {
		return
	}
	structured := completion.Structured
	// An adapter that already reported a failure — a forced tool the model never
	// called — keeps its own, more specific message.
	if structured.Err != nil {
		return
	}
	candidate := structured.Value
	fail := func(msg string) {
		structured.Value = nil
		structured.Enforced = false
		structured.Err = &cllm.StructuredError{Message: msg}
	}

	schema, err := jsonschema.Compile(req.Output.Schema)
	if err != nil {
		fail("output schema is not in the supported subset: " + err.Error())
		return
	}
	if err := schema.Validate(candidate); err != nil {
		fail("model output did not validate against the schema: " + err.Error())
		return
	}
	structured.Value = candidate
}
