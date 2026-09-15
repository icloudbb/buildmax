package workflow

import (
	"context"
	"testing"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	coreaudit "github.com/icloudbb/buildmax/internal/core/audit"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/audit"
)

type fakeAudit struct{ events []coreaudit.Event }

func (f *fakeAudit) RecordAuditEvent(_ context.Context, e coreaudit.Event) error {
	f.events = append(f.events, e)
	return nil
}

func (f *fakeAudit) find(action string) (coreaudit.Event, bool) {
	for _, e := range f.events {
		if e.Action == action {
			return e, true
		}
	}
	return coreaudit.Event{}, false
}

// A lifecycle move is recorded as the state it reached, so "was this ever
// published" is a filter over the trail rather than a scan. This is the mapping
// that has to hold for that to be true.
func TestWorkflowUpdateAction(t *testing.T) {
	published := coreworkflow.StatusPublished
	archived := coreworkflow.StatusArchived
	draft := coreworkflow.StatusDraft
	cases := []struct {
		name   string
		status *string
		want   string
	}{
		{"content edit", nil, coreaudit.WorkflowUpdated},
		{"publish", &published, coreaudit.WorkflowPublished},
		{"archive", &archived, coreaudit.WorkflowArchived},
		{"back to draft", &draft, coreaudit.WorkflowUnpublished},
	}
	for _, tc := range cases {
		if got := workflowUpdateAction(tc.status); got != tc.want {
			t.Errorf("%s: action = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestWorkflowCreateAndPublishAreAudited(t *testing.T) {
	events := &fakeAudit{}
	svc := &Service{
		Workflows: &mock.MockWorkflowStore{},
		Agents: &mock.MockAgentStore{
			Agents: []agentdef.Agent{{ID: "a_1", SpaceID: "tm_1", Name: "Agent 1"}},
		},
		Audit: audit.NewRecorder(events),
	}
	def := `{"schema_version":1,"steps":[{"step_id":"collect","type":"agent_task","target_agent_id":"a_1","prompt":"collect"}]}`
	created, err := svc.CreateWorkflow(context.Background(), CreateWorkflowCmd{
		SpaceID: "tm_1", UserID: "u_1", Name: "WF", Definition: def,
	})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if ev, ok := events.find(coreaudit.WorkflowCreated); !ok || ev.TargetID != created.ID || ev.SpaceID != "tm_1" || ev.Detail != "WF" {
		t.Errorf("create event = %+v ok=%v", ev, ok)
	}

	published := coreworkflow.StatusPublished
	if _, err := svc.UpdateWorkflow(context.Background(), UpdateWorkflowCmd{
		SpaceID: "tm_1", UserID: "u_2", WorkflowID: created.ID, Status: &published,
	}); err != nil {
		t.Fatalf("UpdateWorkflow: %v", err)
	}
	if ev, ok := events.find(coreaudit.WorkflowPublished); !ok || ev.ActorID != "u_2" || ev.TargetID != created.ID {
		t.Errorf("publish event = %+v ok=%v", ev, ok)
	}
	// The lifecycle move is recorded as the state it reached, not as a plain edit.
	if _, ok := events.find(coreaudit.WorkflowUpdated); ok {
		t.Error("a status change was also recorded as a plain update")
	}
}
