package taskrun

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/infra/llmremote"
)

// A provider failure is covered by infra/llm's IsProviderError test; here an
// ordinary error inside the run must not be filed as the model's.
func TestClassifyRunErrorFilesNonProviderFailuresAsTheRun(t *testing.T) {
	if got := classifyRunError(errors.New("max iterations exceeded")); got != coretask.FailureRun {
		t.Errorf("class = %q, want run", got)
	}
}

// A managed run's provider failures arrive as gateway codes, and must land in
// the same class a direct run's would, so the administrator's "who acts next"
// does not depend on the transport.
func TestClassifyRunErrorFilesGatewayRefusalsByOwner(t *testing.T) {
	tests := []struct {
		code string
		want coretask.FailureClass
	}{
		{code: llmremote.CodeUpstream, want: coretask.FailureModel},
		{code: llmremote.CodeUpstreamTimeout, want: coretask.FailureModel},
		{code: llmremote.CodeUpstreamAuth, want: coretask.FailureModel},
		{code: llmremote.CodeUpstreamRateLimited, want: coretask.FailureModel},
		{code: llmremote.CodeTargetNotFound, want: coretask.FailureModel},
		{code: llmremote.CodeTargetDisabled, want: coretask.FailureModel},
		{code: llmremote.CodeCapability, want: coretask.FailureModel},
		{code: llmremote.CodeQuotaExceeded, want: coretask.FailureSpaceConfiguration},
		{code: llmremote.CodeCanceled, want: coretask.FailureInfrastructure},
		{code: llmremote.CodeNotConfigured, want: coretask.FailureInfrastructure},
		{code: llmremote.CodeInternal, want: coretask.FailureInfrastructure},
		{code: "", want: coretask.FailureInfrastructure},
		{code: llmremote.CodeInvalidRequest, want: coretask.FailureRun},
		{code: llmremote.CodeDuplicateCall, want: coretask.FailureRun},
		{code: "something_newer", want: coretask.FailureUnclassified},
	}
	for _, tc := range tests {
		// Wrapped as the agent loop wraps it, so the test exercises errors.As.
		err := fmt.Errorf("agent: llm call: %w", &llmremote.GatewayError{StatusCode: http.StatusBadGateway, Code: tc.code})
		if got := classifyRunError(err); got != tc.want {
			t.Errorf("code %q: class = %q, want %q", tc.code, got, tc.want)
		}
	}
}
