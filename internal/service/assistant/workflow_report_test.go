package assistant

import (
	"context"
	"fmt"
	"testing"

	coreassistant "github.com/icloudbb/buildmax/internal/core/assistant"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/mock"
)

type reportFixture struct {
	*frontDoorFixture
	runs *mock.MockWorkflowStore
}

// newReportFixture has member chatting with the published HR assistant in
// chat-9, and a run of its roster Workflow wf_leave that their conversation
// started, still running and owing them a report.
func newReportFixture(t *testing.T) *reportFixture {
	t.Helper()
	f := newFrontDoorFixture(t, coreassistant.AudienceSpaceMembers)
	f.ask(t, member, "chat-9", "hello")
	conv := f.convs.list[len(f.convs.list)-1].ID
	r := &reportFixture{frontDoorFixture: f, runs: &mock.MockWorkflowStore{
		Workflows: []coreworkflow.Workflow{{ID: "wf_leave", SpaceID: team, Name: "Leave request"}},
		Runs: []coreworkflow.Run{{
			ID: "wr1", WorkflowID: "wf_leave", ConversationID: &conv, Status: string(coreworkflow.RunStatusRunning),
			RequestedBy: member, AssistantID: f.view.Assistant.ID, ReportStatus: coreworkflow.ReportPending,
		}},
	}}
	f.door.Reports = r.runs
	return r
}

func (r *reportFixture) end(status, result string) {
	r.runs.Runs[0].Status = status
	if result != "" {
		r.runs.Runs[0].Result = &result
	}
}

// A run still going reports nothing; once it ends the requester gets its
// releasable fields under the Workflow's roster entry, exactly once.
func TestWorkflowReportSendsReleasableFieldsOnce(t *testing.T) {
	r := newReportFixture(t)
	ctx := context.Background()

	r.door.SweepDeliveries(ctx)
	if len(r.gateway.sent) != 0 || r.runs.Runs[0].ReportStatus != coreworkflow.ReportPending {
		t.Fatalf("a running workflow was reported: %q", r.gateway.sent)
	}

	r.end(string(coreworkflow.RunStatusSucceeded), `{"answer":"approved for 21-24 Dec","raw":"manager queue 77"}`)
	r.door.SweepDeliveries(ctx)
	r.door.SweepDeliveries(ctx)
	want := "telegram|" + r.view.Binding.ID + "|chat-9|“Leave request” is done.\n\nanswer: approved for 21-24 Dec"
	if len(r.gateway.sent) != 1 || r.gateway.sent[0] != want {
		t.Errorf("sent = %q, want once %q", r.gateway.sent, want)
	}
	if got := r.runs.Runs[0].ReportStatus; got != coreworkflow.ReportSent {
		t.Errorf("report status = %q, want sent", got)
	}
}

// A run that failed or was stopped gets a fixed sentence, never its error.
func TestWorkflowReportSaysHowAnUnsuccessfulRunEnded(t *testing.T) {
	for status, want := range map[coreworkflow.RunStatus]string{
		coreworkflow.RunStatusFailed:   "“Leave request” could not be completed.",
		coreworkflow.RunStatusCanceled: "“Leave request” was stopped.",
	} {
		r := newReportFixture(t)
		msg := "node review: HRIS token rejected"
		r.runs.Runs[0].ErrorMessage = &msg
		r.end(string(status), "")
		r.door.SweepDeliveries(context.Background())
		if len(r.gateway.sent) != 1 || r.gateway.sent[0] != "telegram|"+r.view.Binding.ID+"|chat-9|"+want {
			t.Errorf("%s: sent = %q", status, r.gateway.sent)
		}
	}
}

// Nothing is sent while the Assistant would not answer the requester: the
// report is settled as skipped, and a run whose Workflow left the roster
// releases nothing of its result.
func TestWorkflowReportRespectsTheAssistantsChecks(t *testing.T) {
	r := newReportFixture(t)
	r.end(string(coreworkflow.RunStatusSucceeded), `{"answer":"approved"}`)
	if _, err := r.svc.SetState(context.Background(), SetStateCmd{SpaceID: team, ActorID: owner, AssistantID: r.view.Assistant.ID, State: coreassistant.StatePaused}); err != nil {
		t.Fatal(err)
	}
	r.door.SweepDeliveries(context.Background())
	if len(r.gateway.sent) != 0 || r.runs.Runs[0].ReportStatus != coreworkflow.ReportSkipped {
		t.Errorf("paused assistant: sent %q, status %q", r.gateway.sent, r.runs.Runs[0].ReportStatus)
	}

	r = newReportFixture(t)
	r.end(string(coreworkflow.RunStatusSucceeded), `{"answer":"approved"}`)
	r.gateway.unreachable = map[string]bool{member: true}
	r.door.SweepDeliveries(context.Background())
	if len(r.gateway.sent) != 0 || r.runs.Runs[0].ReportStatus != coreworkflow.ReportSkipped {
		t.Errorf("inactive link: sent %q, status %q", r.gateway.sent, r.runs.Runs[0].ReportStatus)
	}

	r = newReportFixture(t)
	r.runs.Runs[0].WorkflowID = "wf_gone"
	r.end(string(coreworkflow.RunStatusSucceeded), `{"answer":"approved"}`)
	r.door.SweepDeliveries(context.Background())
	if len(r.gateway.sent) != 1 || r.gateway.sent[0] != "telegram|"+r.view.Binding.ID+"|chat-9|The request you made is done. There is nothing from it this assistant may share." {
		t.Errorf("off-roster run: sent %q", r.gateway.sent)
	}
}

// A send the platform refuses is recorded as failed, not retried.
func TestWorkflowReportRecordsAFailedSend(t *testing.T) {
	r := newReportFixture(t)
	r.end(string(coreworkflow.RunStatusSucceeded), `{"answer":"approved"}`)
	r.gateway.sendErr = fmt.Errorf("telegram down")
	r.door.SweepDeliveries(context.Background())
	r.gateway.sendErr = nil
	r.door.SweepDeliveries(context.Background())
	if r.runs.Runs[0].ReportStatus != coreworkflow.ReportFailed || len(r.gateway.sent) != 0 {
		t.Errorf("status %q, sent %q", r.runs.Runs[0].ReportStatus, r.gateway.sent)
	}
}
