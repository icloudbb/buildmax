package llmcatalog

import (
	"errors"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/core/apierr"
	"github.com/icloudbb/buildmax/internal/core/llm"
	coregw "github.com/icloudbb/buildmax/internal/core/llmgateway"
)

// TestValidateCredentialRule pins which catalog targets may be added
// without a credential. The catalog decides where prompts go and what pays for
// them, so the exemption has to be exactly one provider wide: the one with no
// secret to hold.
func TestValidateCredentialRule(t *testing.T) {
	base := coregw.CreateModelInput{
		Name:         "Target",
		APIURL:       "https://api.example.test/v1",
		Model:        "some-model",
		ProviderType: llm.ProviderOpenAICompatible,
	}
	cases := []struct {
		name     string
		provider string
		apiKey   string
		wantErr  string
	}{
		{name: "hosted with a key", provider: llm.ProviderOpenAICompatible, apiKey: "sk-test"},
		{name: "hosted without a key", provider: llm.ProviderOpenAICompatible, wantErr: "api_key is required"},
		{name: "anthropic without a key", provider: llm.ProviderAnthropic, wantErr: "api_key is required"},
		{name: "local without a key", provider: llm.ProviderOllama},
		{name: "local with a key", provider: llm.ProviderOllama, apiKey: "not-needed"},
		{name: "unimplemented provider", provider: "bedrock", apiKey: "sk-test", wantErr: "not implemented"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			in.ProviderType = tc.provider
			in.APIKey = tc.apiKey

			err := Validate(in)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("validateModelInput: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want it to mention %q", err, tc.wantErr)
			}
		})
	}
}

// TestValidateRejectsEveryWayARowCannotServe walks the branches the
// credential test does not. A row that reaches the catalog is a row a call will
// be routed to, so each of these is the last place the operator hears about it
// instead of somebody's first prompt.
//
// It is written as a characterization: the messages are what the command says
// today, and a change to any of them is a change to what an operator reads.
func TestValidateRejectsEveryWayARowCannotServe(t *testing.T) {
	valid := coregw.CreateModelInput{
		Name:         "Target",
		APIURL:       "https://api.example.test/v1",
		Model:        "some-model",
		APIKey:       "sk-test",
		ProviderType: llm.ProviderOpenAICompatible,
	}
	if err := Validate(valid); err != nil {
		t.Fatalf("the valid input was refused: %v", err)
	}

	cases := []struct {
		name    string
		mutate  func(*coregw.CreateModelInput)
		wantErr string
	}{
		{"no name", func(in *coregw.CreateModelInput) { in.Name = "" }, "name is required"},
		{"no api url", func(in *coregw.CreateModelInput) { in.APIURL = "" }, "api_url is required"},
		{"no model", func(in *coregw.CreateModelInput) { in.Model = "" }, "model is required"},
		{"negative context window", func(in *coregw.CreateModelInput) { in.ContextWindow = -1 }, "context_window must be between"},
		{"negative call timeout", func(in *coregw.CreateModelInput) { in.CallTimeout = -1 }, "call_timeout must be between"},
		{"negative max tokens", func(in *coregw.CreateModelInput) { in.MaxTokens = -1 }, "max_tokens must be between"},
		{"absurd context window", func(in *coregw.CreateModelInput) { in.ContextWindow = 1 << 40 }, "context_window must be between"},
		{"absurd call timeout", func(in *coregw.CreateModelInput) { in.CallTimeout = 1 << 40 }, "call_timeout must be between"},
		{"absurd max tokens", func(in *coregw.CreateModelInput) { in.MaxTokens = 1 << 40 }, "max_tokens must be between"},
		{"name too long", func(in *coregw.CreateModelInput) { in.Name = strings.Repeat("x", 200) }, "name is too long"},
		{"unknown reasoning", func(in *coregw.CreateModelInput) { in.Reasoning = "ultra" }, "is not a level"},
		{"unknown cache mode", func(in *coregw.CreateModelInput) { in.CacheMode = "sometimes" }, "is not a mode"},
		{"unknown cache ttl", func(in *coregw.CreateModelInput) { in.CacheTTL = "forever" }, "is not a retention"},
		{"unknown capability", func(in *coregw.CreateModelInput) { in.Capabilities = []string{"telepathy"} }, "unknown capability"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := valid
			tc.mutate(&in)
			err := Validate(in)
			if err == nil {
				t.Fatalf("%s was accepted", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

// TestValidateAcceptsZeroForTheOptionalNumbers pins that unset is not
// the same as invalid: a catalog row may leave the window, the timeout, and the
// token cap to the runtime's defaults.
func TestValidateAcceptsZeroForTheOptionalNumbers(t *testing.T) {
	in := coregw.CreateModelInput{
		Name:         "Target",
		APIURL:       "https://api.example.test/v1",
		Model:        "some-model",
		APIKey:       "sk-test",
		ProviderType: llm.ProviderOpenAICompatible,
	}
	if err := Validate(in); err != nil {
		t.Fatalf("zero values were refused: %v", err)
	}
}

// TestValidateRefusalsCarryTheirFieldAndKind is what lets an edge render a
// refusal in its own words. The field is the catalog's name for the input, not
// any caller's: the shell turns api_key into --api-key and an HTTP caller would
// turn it into a JSON key, and neither belongs in the service.
func TestValidateRefusalsCarryTheirFieldAndKind(t *testing.T) {
	base := coregw.CreateModelInput{
		Name:         "Target",
		APIURL:       "https://api.example.test/v1",
		Model:        "some-model",
		APIKey:       "sk-test",
		ProviderType: llm.ProviderOpenAICompatible,
	}
	cases := []struct {
		name  string
		mut   func(*coregw.CreateModelInput)
		field string
	}{
		{"name", func(in *coregw.CreateModelInput) { in.Name = "" }, "name"},
		{"api url", func(in *coregw.CreateModelInput) { in.APIURL = "" }, "api_url"},
		{"api key", func(in *coregw.CreateModelInput) { in.APIKey = "" }, "api_key"},
		{"provider", func(in *coregw.CreateModelInput) { in.ProviderType = "bedrock" }, "provider_type"},
		// A capability is not one input field, so this one names none and the
		// edge prints the message on its own.
		{"capability", func(in *coregw.CreateModelInput) { in.Capabilities = []string{"telepathy"} }, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := base
			tc.mut(&in)
			err := Validate(in)
			if err == nil {
				t.Fatal("accepted")
			}
			var invalid *InvalidField
			if !errors.As(err, &invalid) {
				t.Fatalf("error %v does not carry a field", err)
			}
			if invalid.Field != tc.field {
				t.Errorf("field = %q, want %q", invalid.Field, tc.field)
			}
			if !errors.Is(err, ErrInvalid) {
				t.Error("refusal does not match ErrInvalid")
			}
			if kind, _ := apierr.KindOf(err); kind != apierr.KindInvalid {
				t.Errorf("kind = %q, want invalid", kind)
			}
		})
	}
}
