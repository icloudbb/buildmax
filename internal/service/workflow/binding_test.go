package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
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
			name: "binds an earlier step",
			raw:  `{"schema_version":1,"steps":[{"step_id":"a","type":"agent_task","target_agent_id":"x","prompt":"p"},{"step_id":"b","type":"agent_task","target_agent_id":"x","prompt":"p","bindings":[{"name":"r","from_step":"a"}]}]}`,
		},
		{
			name:    "from_step missing",
			raw:     `{"schema_version":1,"steps":[{"step_id":"b","type":"agent_task","target_agent_id":"x","prompt":"p","bindings":[{"name":"r","from_step":"a"}]}]}`,
			wantErr: true,
		},
		{
			name:    "from_step is a later step",
			raw:     `{"schema_version":1,"steps":[{"step_id":"a","type":"agent_task","target_agent_id":"x","prompt":"p","bindings":[{"name":"r","from_step":"b"}]},{"step_id":"b","type":"agent_task","target_agent_id":"x","prompt":"p"}]}`,
			wantErr: true,
		},
		{
			name:    "from_step is itself",
			raw:     `{"schema_version":1,"steps":[{"step_id":"a","type":"agent_task","target_agent_id":"x","prompt":"p","bindings":[{"name":"r","from_step":"a"}]}]}`,
			wantErr: true,
		},
		{
			name:    "duplicate binding name",
			raw:     `{"schema_version":1,"steps":[{"step_id":"a","type":"agent_task","target_agent_id":"x","prompt":"p"},{"step_id":"b","type":"agent_task","target_agent_id":"x","prompt":"p","bindings":[{"name":"r","from_step":"a"},{"name":"r","from_step":"a"}]}]}`,
			wantErr: true,
		},
		{
			name:    "empty binding name",
			raw:     `{"schema_version":1,"steps":[{"step_id":"a","type":"agent_task","target_agent_id":"x","prompt":"p"},{"step_id":"b","type":"agent_task","target_agent_id":"x","prompt":"p","bindings":[{"name":"","from_step":"a"}]}]}`,
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
			raw:  `{"schema_version":1,"input_schema":{"type":"object","additionalProperties":false,"properties":{"topic":{"type":"string"}},"required":["topic"]},"steps":[{"step_id":"a","type":"agent_task","target_agent_id":"x","prompt":"p"}],"result":{"from_step":"a"}}`,
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
			raw:     `{"schema_version":1,"steps":[{"step_id":"a","type":"agent_task","target_agent_id":"x","prompt":"p"}],"result":{"from_step":"b"}}`,
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

func TestBuildWorkflowTaskInput_LabelsBoundOutputAsUntrusted(t *testing.T) {
	agent := &agentdef.Agent{Name: "Writer", Description: "writes", Instructions: "Follow the plan."}
	input := buildWorkflowTaskInput(agent, "Write the report.", []boundOutput{
		{Name: "research", FromStep: "collect", Output: "the full research text"},
	})

	instructions := input[:strings.Index(input, "Write the report.")]
	if strings.Contains(instructions, "the full research text") {
		t.Fatal("bound output leaked into the agent instructions; it must be input data only")
	}
	if !strings.Contains(input, `<workflow-input name="research" from-step="collect">`) {
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
				`{"step_id":"summarize","type":"agent_task","target_agent_id":"a_2","prompt":"write-the-summary","bindings":[{"name":"research","from_step":"collect"}]}` +
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
	if !strings.Contains(summarizeInput, `<workflow-input name="research" from-step="collect">`) {
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
