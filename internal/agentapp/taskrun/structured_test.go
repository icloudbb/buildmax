package taskrun

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/icloudbb/buildmax/internal/config"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/infra/llmwire"
	"github.com/icloudbb/buildmax/internal/testsupport/mockllm"
)

const labelSchema = `{"type":"object","properties":{"label":{"type":"string"}},"required":["label"],"additionalProperties":false}`

// runWithSchema executes a worker run of a Task that declares labelSchema and
// returns the terminal report it would send.
func runWithSchema(t *testing.T, model config.ModelEntry, managed ManagedInference) (runResult, *fakeUpdater) {
	t.Helper()
	schema := labelSchema
	task := &coretask.Task{ID: "task1", SpaceID: "tm1", OutputSchema: &schema}
	run := &coretask.Run{ID: "run1", TaskID: task.ID, Input: "classify: the app crashes on start"}
	dirs := testRunDirs(t)
	result, err := executeRunTask(context.Background(), RunTaskInput{Model: model, Managed: managed}, task, run, dirs)
	if err != nil {
		t.Fatalf("executeRunTask: %v", err)
	}
	updater := &fakeUpdater{}
	if err := reportRunOutcome(context.Background(), RunScope{SpaceID: task.SpaceID, TaskID: task.ID, TaskRunID: run.ID},
		result, coretask.RunStatusSucceeded, "", "", nil, updater); err != nil {
		t.Fatalf("reportRunOutcome: %v", err)
	}
	return result, updater
}

func assertReportedLabel(t *testing.T, updater *fakeUpdater) {
	t.Helper()
	if updater.req == nil || updater.req.Structured == nil {
		t.Fatal("the terminal report carries no structured value")
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(*updater.req.Structured), &got); err != nil || got["label"] != "bug" {
		t.Errorf("reported structured = %s, want {\"label\":\"bug\"}", *updater.req.Structured)
	}
}

// A direct-transport worker run of a Task with an output schema asks the
// provider for the schema and reports the validated value on its terminal
// report — the fact a Workflow node with an output_schema folds.
func TestWorkerRunWithOutputSchemaReportsStructured_Direct(t *testing.T) {
	server, err := mockllm.Start(mockllm.Scenario{Steps: []mockllm.Step{
		{Text: "It is a bug."},
		{Text: `{"label":"bug"}`},
	}})
	if err != nil {
		t.Fatalf("start mock model: %v", err)
	}
	t.Cleanup(server.Close)
	model := config.ModelEntry{
		Model: "mock-model", Name: "mock", APIURL: server.BaseURL(mockllm.ProtocolOpenAIChat),
		APIKey: "mock-key", ContextWindow: 128000,
	}

	_, updater := runWithSchema(t, model, ManagedInference{})

	assertReportedLabel(t, updater)
	calls := server.Requests()
	if len(calls) != 2 {
		t.Fatalf("model calls = %d, want the turn and one extraction call", len(calls))
	}
	if !strings.Contains(string(calls[1].Body), `"label"`) {
		t.Errorf("extraction call did not carry the output schema:\n%s", calls[1].Body)
	}
}

// fakeManagedGateway answers the worker completions route the way the server's
// gateway does: free text for an ordinary turn, and for a call carrying an
// output schema, the provider client's validated value.
type fakeManagedGateway struct {
	mu        sync.Mutex
	gotOutput []*llmwire.OutputSchema
}

func (g *fakeManagedGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var req llmwire.CompletionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	g.mu.Lock()
	g.gotOutput = append(g.gotOutput, req.Output)
	g.mu.Unlock()
	resp := llmwire.CompletionResponse{LLMCallID: "lc_1", Model: "Fast", Content: "It is a bug."}
	if req.Output != nil {
		resp.Content = `{"label":"bug"}`
		resp.Structured = &llmwire.Structured{Value: json.RawMessage(`{"label":"bug"}`), Mode: "native", Enforced: true}
	}
	if !req.Stream {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
		return
	}
	body, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", llmwire.EventResult, body)
}

// A managed-transport worker run sends its output schema through the gateway
// hop and reports the value the gateway returns. Before the wire contract
// carried both, the schema never left the worker and every such run reported
// no structured value.
func TestWorkerRunWithOutputSchemaReportsStructured_Managed(t *testing.T) {
	gateway := &fakeManagedGateway{}
	server := httptest.NewServer(gateway)
	t.Cleanup(server.Close)
	model := config.ModelEntry{Model: "Fast", Name: "Fast", ContextWindow: 128000}

	_, updater := runWithSchema(t, model, ManagedInference{ServerURL: server.URL, RunToken: "run-token"})

	assertReportedLabel(t, updater)
	gateway.mu.Lock()
	defer gateway.mu.Unlock()
	var withSchema int
	for _, out := range gateway.gotOutput {
		if out != nil {
			withSchema++
			if !strings.Contains(string(out.Schema), `"label"`) {
				t.Errorf("gateway received schema %s, want the task's", out.Schema)
			}
		}
	}
	if withSchema != 1 {
		t.Errorf("calls carrying the output schema = %d, want exactly the extraction call", withSchema)
	}
}
