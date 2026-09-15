package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	coreartifact "github.com/icloudbb/buildmax/internal/core/artifact"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/task"
)

func TestParseDefinition_ValidatesBindings(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{
			name: "binds an earlier node output at a pointer",
			raw:  `{"schema_version":1,"steps":[{"step_id":"a","type":"agent_task","target_agent_id":"x","prompt":"p"},{"step_id":"b","type":"agent_task","target_agent_id":"x","prompt":"p","bindings":[{"name":"r","source":"node.a.output","pointer":"/text"}]}]}`,
		},
		{
			name: "binds the workflow input with the whole-value pointer",
			raw:  `{"schema_version":1,"input_schema":{"type":"object","additionalProperties":false,"properties":{"topic":{"type":"string"}}},"steps":[{"step_id":"a","type":"agent_task","target_agent_id":"x","prompt":"p","bindings":[{"name":"t","source":"workflow.input","pointer":"/topic"}]}]}`,
		},
		{
			name:    "source names a missing step",
			raw:     `{"schema_version":1,"steps":[{"step_id":"b","type":"agent_task","target_agent_id":"x","prompt":"p","bindings":[{"name":"r","source":"node.a.output","pointer":""}]}]}`,
			wantErr: true,
		},
		{
			name:    "source names a later step",
			raw:     `{"schema_version":1,"steps":[{"step_id":"a","type":"agent_task","target_agent_id":"x","prompt":"p","bindings":[{"name":"r","source":"node.b.output","pointer":""}]},{"step_id":"b","type":"agent_task","target_agent_id":"x","prompt":"p"}]}`,
			wantErr: true,
		},
		{
			name:    "source names itself",
			raw:     `{"schema_version":1,"steps":[{"step_id":"a","type":"agent_task","target_agent_id":"x","prompt":"p","bindings":[{"name":"r","source":"node.a.output","pointer":""}]}]}`,
			wantErr: true,
		},
		{
			name:    "unknown source",
			raw:     `{"schema_version":1,"steps":[{"step_id":"a","type":"agent_task","target_agent_id":"x","prompt":"p"},{"step_id":"b","type":"agent_task","target_agent_id":"x","prompt":"p","bindings":[{"name":"r","source":"node.a.text","pointer":""}]}]}`,
			wantErr: true,
		},
		{
			name:    "invalid pointer",
			raw:     `{"schema_version":1,"steps":[{"step_id":"a","type":"agent_task","target_agent_id":"x","prompt":"p"},{"step_id":"b","type":"agent_task","target_agent_id":"x","prompt":"p","bindings":[{"name":"r","source":"node.a.output","pointer":"text"}]}]}`,
			wantErr: true,
		},
		{
			name:    "duplicate binding name",
			raw:     `{"schema_version":1,"steps":[{"step_id":"a","type":"agent_task","target_agent_id":"x","prompt":"p"},{"step_id":"b","type":"agent_task","target_agent_id":"x","prompt":"p","bindings":[{"name":"r","source":"node.a.output","pointer":"/text"},{"name":"r","source":"node.a.output","pointer":"/text"}]}]}`,
			wantErr: true,
		},
		{
			name:    "empty binding name",
			raw:     `{"schema_version":1,"steps":[{"step_id":"a","type":"agent_task","target_agent_id":"x","prompt":"p"},{"step_id":"b","type":"agent_task","target_agent_id":"x","prompt":"p","bindings":[{"name":"","source":"node.a.output","pointer":""}]}]}`,
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseDefinition(tc.raw)
			if tc.wantErr && !errors.Is(err, ErrInvalidBinding) {
				t.Fatalf("err = %v, want ErrInvalidBinding", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
		})
	}
}

func TestParseDefinition_ValidatesOutputSchema(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{
			name: "absent output_schema is free text",
			raw:  `{"schema_version":1,"steps":[{"step_id":"a","type":"agent_task","target_agent_id":"x","prompt":"p"}]}`,
		},
		{
			name: "schema in the supported subset",
			raw:  `{"schema_version":1,"steps":[{"step_id":"a","type":"agent_task","target_agent_id":"x","prompt":"p","output_schema":{"type":"object","additionalProperties":false,"properties":{"n":{"type":"integer"}},"required":["n"]}}]}`,
		},
		{
			name:    "schema outside the subset is rejected",
			raw:     `{"schema_version":1,"steps":[{"step_id":"a","type":"agent_task","target_agent_id":"x","prompt":"p","output_schema":{"type":"string","pattern":"x"}}]}`,
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseDefinition(tc.raw)
			if tc.wantErr && !errors.Is(err, ErrInvalidOutputSchema) {
				t.Fatalf("err = %v, want ErrInvalidOutputSchema", err)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
		})
	}
}

func TestParseDefinition_ValidatesContract(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr error
	}{
		{
			name: "declares schema_version, input_schema, and result",
			raw:  `{"schema_version":1,"input_schema":{"type":"object","additionalProperties":false,"properties":{"topic":{"type":"string"}},"required":["topic"]},"steps":[{"step_id":"a","type":"agent_task","target_agent_id":"x","prompt":"p"}],"result":{"source":"node.a.output","pointer":"/text"}}`,
		},
		{
			name:    "missing schema_version is rejected",
			raw:     `{"steps":[{"step_id":"a","type":"agent_task","target_agent_id":"x","prompt":"p"}]}`,
			wantErr: ErrUnsupportedSchemaVersion,
		},
		{
			name:    "unknown schema_version is rejected",
			raw:     `{"schema_version":2,"steps":[{"step_id":"a","type":"agent_task","target_agent_id":"x","prompt":"p"}]}`,
			wantErr: ErrUnsupportedSchemaVersion,
		},
		{
			name:    "input_schema outside the subset is rejected",
			raw:     `{"schema_version":1,"input_schema":{"type":"string","pattern":"x"},"steps":[{"step_id":"a","type":"agent_task","target_agent_id":"x","prompt":"p"}]}`,
			wantErr: ErrInvalidInputSchema,
		},
		{
			name:    "result selecting a missing step is rejected",
			raw:     `{"schema_version":1,"steps":[{"step_id":"a","type":"agent_task","target_agent_id":"x","prompt":"p"}],"result":{"source":"node.b.output","pointer":""}}`,
			wantErr: ErrInvalidResult,
		},
		{
			name:    "result with a non-node source is rejected",
			raw:     `{"schema_version":1,"steps":[{"step_id":"a","type":"agent_task","target_agent_id":"x","prompt":"p"}],"result":{"source":"workflow.input","pointer":""}}`,
			wantErr: ErrInvalidResult,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseDefinition(tc.raw)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestResolveRunInput(t *testing.T) {
	const schema = `{"type":"object","additionalProperties":false,"properties":{"topic":{"type":"string"}},"required":["topic"]}`
	cases := []struct {
		name    string
		schema  string
		raw     string
		want    string // expected stored input; "" means nil
		wantErr error
	}{
		{name: "no schema and no input", schema: "", raw: "", want: ""},
		{name: "no schema rejects supplied input", schema: "", raw: `{"topic":"x"}`, wantErr: ErrInvalidRunInput},
		{name: "schema requires input", schema: schema, raw: "", wantErr: ErrInvalidRunInput},
		{name: "schema accepts valid input", schema: schema, raw: `{"topic":"markets"}`, want: `{"topic":"markets"}`},
		{name: "schema rejects invalid input", schema: schema, raw: `{"topic":1}`, wantErr: ErrInvalidRunInput},
		{name: "schema rejects unknown field", schema: schema, raw: `{"topic":"x","extra":1}`, wantErr: ErrInvalidRunInput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			def := &coreworkflow.Definition{}
			if tc.schema != "" {
				def.InputSchema = json.RawMessage(tc.schema)
			}
			got, err := resolveRunInput(def, tc.raw)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if tc.want == "" {
				if got != nil {
					t.Fatalf("input = %q, want nil", *got)
				}
				return
			}
			if got == nil || *got != tc.want {
				t.Fatalf("input = %v, want %q", got, tc.want)
			}
		})
	}
}

// stubArtifactReader returns a fixed source->artifacts map so a binding into a
// node's /artifacts resolves without a real store.
type stubArtifactReader struct {
	bySource map[string][]coreartifact.Artifact
}

func (s stubArtifactReader) ListArtifactsBySource(_ context.Context, sourceIDs []string) (map[string][]coreartifact.Artifact, error) {
	out := make(map[string][]coreartifact.Artifact)
	for _, id := range sourceIDs {
		if arts, ok := s.bySource[id]; ok {
			out[id] = arts
		}
	}
	return out, nil
}

func TestResolveStepBindings_TextInputAndArtifact(t *testing.T) {
	output := "the full research text"
	structured := `{"score":7}`
	svc := &Service{Artifacts: stubArtifactReader{bySource: map[string][]coreartifact.Artifact{
		"trn_c": {{ID: "art_1", Filename: "report.md", MediaType: "text/markdown"}},
	}}}
	input := `{"topic":"markets"}`
	run := &coreworkflow.Run{ID: "wr_1", Input: &input}
	collect := coreworkflow.NodeRun{
		NodeID: "collect", Output: &output, Structured: &structured,
		TaskID: strPtr("tsk_c"), TaskRunID: strPtr("trn_c"),
	}
	summarize := coreworkflow.NodeRun{
		NodeID: "summarize",
		Bindings: []coreworkflow.StepBinding{
			{Name: "research", Source: "node.collect.output", Pointer: "/text"},
			{Name: "score", Source: "node.collect.output", Pointer: "/structured/score"},
			{Name: "file", Source: "node.collect.output", Pointer: "/artifacts/0"},
			{Name: "topic", Source: "workflow.input", Pointer: "/topic"},
		},
	}
	bound, err := svc.resolveStepBindings(context.Background(), run, summarize, []coreworkflow.NodeRun{collect, summarize})
	if err != nil {
		t.Fatalf("resolveStepBindings: %v", err)
	}
	got := map[string]string{}
	for _, b := range bound {
		got[b.Name] = b.Value
	}
	if got["research"] != output {
		t.Errorf("research = %q, want %q", got["research"], output)
	}
	if got["score"] != "7" {
		t.Errorf("score = %q, want 7", got["score"])
	}
	if got["topic"] != "markets" {
		t.Errorf("topic = %q, want markets", got["topic"])
	}
	if !strings.Contains(got["file"], `"art_1"`) || !strings.Contains(got["file"], "report.md") {
		t.Errorf("file artifact reference = %q, want the artifact id and path", got["file"])
	}
}

func TestResolveStepBindings_PointerMissFailsBinding(t *testing.T) {
	output := "text only"
	svc := &Service{}
	collect := coreworkflow.NodeRun{NodeID: "collect", Output: &output, TaskRunID: strPtr("trn_c")}
	summarize := coreworkflow.NodeRun{
		NodeID:   "summarize",
		Bindings: []coreworkflow.StepBinding{{Name: "missing", Source: "node.collect.output", Pointer: "/artifacts/0"}},
	}
	_, err := svc.resolveStepBindings(context.Background(), &coreworkflow.Run{ID: "wr_1"}, summarize, []coreworkflow.NodeRun{collect, summarize})
	if !errors.Is(err, ErrInvalidBinding) {
		t.Fatalf("err = %v, want ErrInvalidBinding for an unresolved pointer", err)
	}
}

func strPtr(s string) *string { return &s }

func TestBuildWorkflowTaskInput_LabelsBoundOutputAsUntrusted(t *testing.T) {
	agent := &agentdef.Agent{Name: "Writer", Description: "writes", Instructions: "Follow the plan."}
	input := buildWorkflowTaskInput(agent, "Write the report.", []boundValue{
		{Name: "research", Source: "node.collect.output", Pointer: "/text", Value: "the full research text"},
	})

	instructions := input[:strings.Index(input, "Write the report.")]
	if strings.Contains(instructions, "the full research text") {
		t.Fatal("bound value leaked into the agent instructions; it must be input data only")
	}
	if !strings.Contains(input, `<workflow-input name="research" source="node.collect.output" pointer="/text">`) {
		t.Fatalf("input missing the labelled binding block:\n%s", input)
	}
	if !strings.Contains(input, "the full research text") {
		t.Fatalf("input missing the bound output:\n%s", input)
	}
	if !strings.Contains(input, "untrusted data") {
		t.Fatalf("input missing the untrusted-data framing:\n%s", input)
	}
}

// TestStartWorkflowRun_BindsUpstreamOutputIntoDownstreamInput proves the whole
// slice end to end over in-memory doubles: a second step that binds the first
// step's output runs with that step's full output in its Task input, not the
// truncated summary and not its instructions.
func TestStartWorkflowRun_BindsUpstreamOutputIntoDownstreamInput(t *testing.T) {
	workflowStore := &mock.MockWorkflowStore{
		Workflows: []coreworkflow.Workflow{{
			ID:      "w_1",
			SpaceID: "tm_1",
			Name:    "WF",
			Definition: `{"schema_version":1,"steps":[` +
				`{"step_id":"collect","type":"agent_task","target_agent_id":"a_1","prompt":"collect"},` +
				`{"step_id":"summarize","type":"agent_task","target_agent_id":"a_2","prompt":"write-the-summary","bindings":[{"name":"research","source":"node.collect.output","pointer":"/text"}]}` +
				`]}`,
			Status: coreworkflow.StatusPublished,
		}},
	}
	taskStore := &mock.MockTaskStore{}
	taskRuns := &mock.MockTaskRunStore{}
	agentStore := &mock.MockAgentStore{
		Agents: []agentdef.Agent{
			{ID: "a_1", SpaceID: "tm_1", Name: "Collector", Instructions: "collect"},
			{ID: "a_2", SpaceID: "tm_1", Name: "Summarizer", Instructions: "summarize"},
		},
	}
	svc := &Service{
		Workflows: workflowStore,
		Agents:    agentStore,
		TaskRuns:  taskRuns,
		TaskService: &task.Service{
			Agents:   agentStore,
			Tasks:    taskStore,
			TaskRuns: taskRuns,
		},
	}
	run, steps, err := svc.StartWorkflowRun(context.Background(), StartWorkflowRunCmd{
		SpaceID: "tm_1", UserID: "u1", WorkflowID: "w_1",
	})
	if err != nil {
		t.Fatalf("StartWorkflowRun: %v", err)
	}

	// The full output is longer than the 500-rune display summary, so a marker at
	// the end proves the whole output was bound, not the summary.
	fullOutput := "COLLECTED:" + strings.Repeat("x", 600) + ":END-MARKER"
	taskRuns.Runs = append(taskRuns.Runs, coretask.Run{
		ID:     *steps[0].TaskRunID,
		TaskID: *steps[0].TaskID,
		Status: string(coretask.RunStatusSucceeded),
		Output: &fullOutput,
	})
	if err := svc.Reconcile(context.Background(), run.ID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var summarizeInput string
	for _, c := range taskStore.Created {
		if strings.HasSuffix(c.AdmissionKey, "node/summarize") {
			summarizeInput = c.Input
		}
	}
	if summarizeInput == "" {
		t.Fatal("summarize step was never dispatched with an input")
	}
	if !strings.Contains(summarizeInput, `<workflow-input name="research" source="node.collect.output" pointer="/text">`) {
		t.Fatalf("summarize input missing the labelled binding block:\n%s", summarizeInput)
	}
	if !strings.Contains(summarizeInput, "END-MARKER") {
		t.Fatalf("summarize input carried the truncated summary, not the full output:\n%s", summarizeInput)
	}
	instructions := summarizeInput[:strings.Index(summarizeInput, "write-the-summary")]
	if strings.Contains(instructions, "END-MARKER") {
		t.Fatal("bound output leaked into the agent identity/instructions region")
	}
}

// TestStartWorkflowRun_StoresDeclaredResult proves a succeeding run resolves its
// declared result selector against the finished node output and stores it on the
// run as one authoritative answer.
func TestStartWorkflowRun_StoresDeclaredResult(t *testing.T) {
	workflowStore := &mock.MockWorkflowStore{
		Workflows: []coreworkflow.Workflow{{
			ID:      "w_1",
			SpaceID: "tm_1",
			Name:    "WF",
			Definition: `{"schema_version":1,"steps":[` +
				`{"step_id":"only","type":"agent_task","target_agent_id":"a_1","prompt":"do the work"}` +
				`],"result":{"source":"node.only.output","pointer":"/text"}}`,
			Status: coreworkflow.StatusPublished,
		}},
	}
	taskStore := &mock.MockTaskStore{}
	taskRuns := &mock.MockTaskRunStore{}
	agentStore := &mock.MockAgentStore{Agents: []agentdef.Agent{{ID: "a_1", SpaceID: "tm_1", Name: "Worker", Instructions: "work"}}}
	svc := &Service{
		Workflows:   workflowStore,
		Agents:      agentStore,
		TaskRuns:    taskRuns,
		TaskService: &task.Service{Agents: agentStore, Tasks: taskStore, TaskRuns: taskRuns},
	}
	run, steps, err := svc.StartWorkflowRun(context.Background(), StartWorkflowRunCmd{SpaceID: "tm_1", UserID: "u1", WorkflowID: "w_1"})
	if err != nil {
		t.Fatalf("StartWorkflowRun: %v", err)
	}
	output := "the final answer"
	taskRuns.Runs = append(taskRuns.Runs, coretask.Run{
		ID:     *steps[0].TaskRunID,
		TaskID: *steps[0].TaskID,
		Status: string(coretask.RunStatusSucceeded),
		Output: &output,
	})
	if err := svc.Reconcile(context.Background(), run.ID); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	got, _, err := svc.GetWorkflowRunDetail(context.Background(), "tm_1", run.ID)
	if err != nil {
		t.Fatalf("GetWorkflowRunDetail: %v", err)
	}
	if got.Status != string(coreworkflow.RunStatusSucceeded) {
		t.Fatalf("status = %q, want succeeded", got.Status)
	}
	// The /text pointer selects the output text, stored as the JSON string it is.
	if got.Result == nil || *got.Result != `"the final answer"` {
		t.Fatalf("result = %v, want %q", got.Result, `"the final answer"`)
	}
}
