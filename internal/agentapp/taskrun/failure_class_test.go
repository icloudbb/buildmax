package taskrun

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/infra/llmremote"
	"github.com/icloudbb/buildmax/internal/testsupport/mockllm"
)

// A direct-transport worker calls the server's model with the server's key, so
// a refused key must send the operator to where the dispatcher says that key
// lives, not to a settings.yaml the worker never read -- and must not quote it.
func TestRunTaskNamesTheDispatcherKeySourceWhenTheProviderRefusesTheKey(t *testing.T) {
	const key = "direct-worker-key"
	const hint = "check the server's conversation key"
	server, err := mockllm.Start(mockllm.Scenario{Steps: []mockllm.Step{
		{Status: http.StatusUnauthorized, Error: "invalid api key"},
	}, Repeat: true})
	if err != nil {
		t.Fatalf("start mock model: %v", err)
	}
	t.Cleanup(server.Close)

	updater := &fakeUpdater{}
	sessionID := "sid-key"
	err = RunTask(context.Background(), RunTaskInput{
		Task:      &coretask.Task{ID: "task1", SpaceID: "space1", SessionID: &sessionID},
		Run:       &coretask.Run{ID: "run1", Input: "hello"},
		SessionID: sessionID,
		Paths:     NewRuntimePathsFromRoot(t.TempDir()),
		Persist:   newFakePersistStorage(),
		Updater:   updater,
		Model: config.ModelEntry{
			Model: "mock-model", Name: "mock", APIURL: server.BaseURL(mockllm.ProtocolOpenAIChat),
			APIKey: key, ContextWindow: 128000,
		},
		ModelCredentialHint: hint,
	})
	if !errors.Is(err, coretask.ErrRunFailed) {
		t.Fatalf("RunTask err = %v, want a reported failure", err)
	}
	req := updater.req
	if req == nil || req.ErrorMessage == nil {
		t.Fatalf("report = %+v, want an error message", req)
	}
	msg := *req.ErrorMessage
	if !strings.Contains(msg, "authentication failed (HTTP 401): "+hint) {
		t.Errorf("error message = %q, want the dispatcher's key source", msg)
	}
	if strings.Contains(msg, "settings.yaml") || strings.Contains(msg, key) {
		t.Errorf("error message = %q points at settings.yaml or quotes the key", msg)
	}
	if c := req.FailureClass; c == nil || *c != string(coretask.FailureModel) {
		t.Errorf("failure_class = %v, want model", c)
	}
}

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
