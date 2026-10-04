package assistant

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	coreassistant "github.com/icloudbb/buildmax/internal/core/assistant"
	coreschedule "github.com/icloudbb/buildmax/internal/core/schedule"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	coreworkflow "github.com/icloudbb/buildmax/internal/core/workflow"
	"github.com/icloudbb/buildmax/internal/mock"
)

type fakeRuns struct {
	tasks     map[string]*coretask.Task
	taskRuns  map[string]*coretask.Run
	workflows map[string]*coreworkflow.Run
}

func (r *fakeRuns) GetTask(_ context.Context, id string) (*coretask.Task, error) {
	return r.tasks[id], nil
}

func (r *fakeRuns) GetTaskRun(_ context.Context, id string) (*coretask.Run, error) {
	return r.taskRuns[id], nil
}

func (r *fakeRuns) GetWorkflowRun(_ context.Context, id string) (*coreworkflow.Run, error) {
	return r.workflows[id], nil
}

// setRun records task's run in status, with structured as its answer.
func (r *fakeRuns) setRun(task, status, structured string) {
	runID := task + "_run"
	r.tasks[task] = &coretask.Task{ID: task, LastRunID: &runID}
	run := &coretask.Run{ID: runID, Status: status}
	if structured != "" {
		run.Structured = &structured
	}
	r.taskRuns[runID] = run
}

type deliveryFixture struct {
	*frontDoorFixture
	schedules *mock.MockScheduleStore
	runs      *fakeRuns
}

// newDeliveryFixture has member chatting with the published HR assistant in
// chat-9 and a schedule of agent_hr delivering to them through it, with one
// firing pending on task t1.
func newDeliveryFixture(t *testing.T) *deliveryFixture {
	t.Helper()
	f := newFrontDoorFixture(t, coreassistant.AudienceSpaceMembers)
	f.ask(t, member, "chat-9", "hello")
	d := &deliveryFixture{
		frontDoorFixture: f,
		schedules: &mock.MockScheduleStore{
			Schedules: []coreschedule.Schedule{{
				ID: "sch1", SpaceID: team, ExecutorKind: coreschedule.ExecutorAgent, ExecutorID: "agent_hr",
				Name:     "Leave balance",
				Delivery: &coreschedule.Delivery{AssistantID: f.view.Assistant.ID, RequesterID: member},
			}},
			Deliveries: []coreschedule.FireDelivery{{ID: "d1", ScheduleID: "sch1", FireRef: "t1", Status: coreschedule.DeliveryPending}},
		},
		runs: &fakeRuns{tasks: map[string]*coretask.Task{}, taskRuns: map[string]*coretask.Run{}, workflows: map[string]*coreworkflow.Run{}},
	}
	f.door.Schedules, f.door.Runs = d.schedules, d.runs
	return d
}

func (d *deliveryFixture) delivery(t *testing.T) coreschedule.FireDelivery {
	t.Helper()
	return d.schedules.Deliveries[0]
}

// A finished firing sends only the releasable fields, once, through the
// Assistant's bot into the requester's chat; one still running waits.
func TestScheduleDeliverySendsReleasableFieldsOnce(t *testing.T) {
	d := newDeliveryFixture(t)
	ctx := context.Background()

	d.runs.setRun("t1", string(coretask.RunStatusRunning), "")
	d.door.SweepDeliveries(ctx)
	if len(d.gateway.sent) != 0 || d.delivery(t).Status != coreschedule.DeliveryPending {
		t.Fatalf("a running firing was settled: sent %v, delivery %+v", d.gateway.sent, d.delivery(t))
	}

	d.runs.setRun("t1", string(coretask.RunStatusSucceeded), `{"answer":"12 days left","raw":"payroll row 4411"}`)
	d.door.SweepDeliveries(ctx)
	d.door.SweepDeliveries(ctx)
	want := "telegram|" + d.view.Binding.ID + "|chat-9|Leave balance\n\nanswer: 12 days left"
	if len(d.gateway.sent) != 1 || d.gateway.sent[0] != want {
		t.Errorf("sent = %q, want once %q", d.gateway.sent, want)
	}
	if got := d.delivery(t); got.Status != coreschedule.DeliveryDelivered || got.Reason != "" || got.SettledAt == nil {
		t.Errorf("delivery = %+v, want delivered", got)
	}
}

// A Workflow firing is released under the Workflow's roster entry.
func TestScheduleDeliveryCoversWorkflowRuns(t *testing.T) {
	d := newDeliveryFixture(t)
	d.schedules.Schedules[0].ExecutorKind, d.schedules.Schedules[0].ExecutorID = coreschedule.ExecutorWorkflow, "wf_leave"
	result := `{"answer":"approved","raw":"x"}`
	d.runs.workflows["t1"] = &coreworkflow.Run{ID: "t1", WorkflowID: "wf_leave", Status: string(coreworkflow.RunStatusSucceeded), Result: &result}

	d.door.SweepDeliveries(context.Background())

	if len(d.gateway.sent) != 1 || d.gateway.sent[0] != "telegram|"+d.view.Binding.ID+"|chat-9|Leave balance\n\nanswer: approved" {
		t.Errorf("sent = %q", d.gateway.sent)
	}
}

// Each rule that would stop the Assistant answering the requester itself, and
// each result with nothing to release, skips the delivery with its reason and
// sends nothing.
func TestScheduleDeliverySkipsWithAReason(t *testing.T) {
	succeeded := func(d *deliveryFixture) {
		d.runs.setRun("t1", string(coretask.RunStatusSucceeded), `{"answer":"12 days left","raw":"r"}`)
	}
	tests := []struct {
		name  string
		setup func(*deliveryFixture)
		want  string
	}{
		{name: "no target", want: coreschedule.SkipNoTarget, setup: func(d *deliveryFixture) {
			succeeded(d)
			d.schedules.Schedules[0].Delivery = nil
		}},
		{name: "run failed", want: coreschedule.SkipRunNotSucceeded, setup: func(d *deliveryFixture) {
			d.runs.setRun("t1", string(coretask.RunStatusFailed), "")
		}},
		{name: "task gone", want: coreschedule.SkipRunNotSucceeded},
		{name: "assistant paused", want: coreschedule.SkipAssistantGone, setup: func(d *deliveryFixture) {
			succeeded(d)
			if _, err := d.svc.SetState(context.Background(), SetStateCmd{SpaceID: team, ActorID: owner, AssistantID: d.view.Assistant.ID, State: coreassistant.StatePaused}); err != nil {
				panic(err)
			}
		}},
		{name: "executor left the roster", want: coreschedule.SkipNotOnRoster, setup: func(d *deliveryFixture) {
			succeeded(d)
			d.schedules.Schedules[0].ExecutorID = "agent_other"
		}},
		{name: "nothing releasable", want: coreschedule.SkipNothingReleasable, setup: func(d *deliveryFixture) {
			d.runs.setRun("t1", string(coretask.RunStatusSucceeded), `{"raw":"only the private part"}`)
		}},
		{name: "requester left the audience", want: coreschedule.SkipNotInAudience, setup: func(d *deliveryFixture) {
			succeeded(d)
			d.spaces.Members = slices.DeleteFunc(d.spaces.Members, func(m corespace.Member) bool { return m.UserID == member })
		}},
		{name: "bot unbound", want: coreschedule.SkipNoBot, setup: func(d *deliveryFixture) {
			succeeded(d)
			if _, err := d.svc.Unbind(context.Background(), team, owner, d.view.Assistant.ID); err != nil {
				panic(err)
			}
		}},
		{name: "chat link inactive", want: coreschedule.SkipLinkInactive, setup: func(d *deliveryFixture) {
			succeeded(d)
			d.gateway.unreachable = map[string]bool{member: true}
		}},
		{name: "requester never wrote", want: coreschedule.SkipNoConversation, setup: func(d *deliveryFixture) {
			succeeded(d)
			d.schedules.Schedules[0].Delivery.RequesterID = admin
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := newDeliveryFixture(t)
			if tc.setup != nil {
				tc.setup(d)
			}
			d.door.SweepDeliveries(context.Background())
			if got := d.delivery(t); got.Status != coreschedule.DeliverySkipped || got.Reason != tc.want {
				t.Errorf("delivery = %+v, want skipped for %s", got, tc.want)
			}
			if len(d.gateway.sent) != 0 {
				t.Errorf("sent %q for a skipped delivery", d.gateway.sent)
			}
		})
	}
}

// A send the platform refuses is recorded as failed, not retried.
func TestScheduleDeliveryRecordsAFailedSend(t *testing.T) {
	d := newDeliveryFixture(t)
	d.runs.setRun("t1", string(coretask.RunStatusSucceeded), `{"answer":"12 days left","raw":"r"}`)
	d.gateway.sendErr = fmt.Errorf("telegram down")

	d.door.SweepDeliveries(context.Background())
	d.gateway.sendErr = nil
	d.door.SweepDeliveries(context.Background())

	if got := d.delivery(t); got.Status != coreschedule.DeliveryFailed {
		t.Errorf("delivery = %+v, want failed", got)
	}
	if len(d.gateway.sent) != 0 {
		t.Errorf("a failed delivery was sent again: %q", d.gateway.sent)
	}
}

// A target is accepted only for an Assistant of the Space whose roster has the
// schedule's executor, and a requester who has written to it.
func TestCheckDeliveryTarget(t *testing.T) {
	d := newDeliveryFixture(t)
	ctx := context.Background()
	id := d.view.Assistant.ID
	check := func(kind, executor, assistant, requester string) error {
		return d.door.CheckDeliveryTarget(ctx, team, kind, executor, coreschedule.Delivery{AssistantID: assistant, RequesterID: requester})
	}
	if err := check(coreschedule.ExecutorAgent, "agent_hr", id, member); err != nil {
		t.Errorf("valid target: %v", err)
	}
	for _, tc := range []struct {
		name                                 string
		kind, executor, assistant, requester string
		want                                 error
	}{
		{"unknown assistant", coreschedule.ExecutorAgent, "agent_hr", "as_missing", member, ErrDeliveryAssistant},
		{"not on roster", coreschedule.ExecutorAgent, "agent_other", id, member, ErrDeliveryNotOnRoster},
		{"workflow kind mismatch", coreschedule.ExecutorWorkflow, "agent_hr", id, member, ErrDeliveryNotOnRoster},
		{"never wrote", coreschedule.ExecutorAgent, "agent_hr", id, admin, ErrDeliveryNoConversation},
	} {
		if err := check(tc.kind, tc.executor, tc.assistant, tc.requester); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
	if err := d.door.CheckDeliveryTarget(ctx, "space_other", coreschedule.ExecutorAgent, "agent_hr", coreschedule.Delivery{AssistantID: id, RequesterID: member}); !errors.Is(err, ErrDeliveryAssistant) {
		t.Errorf("another Space's assistant: err = %v", err)
	}

	got, err := d.door.Requesters(ctx, team, id)
	if err != nil || len(got) != 1 || got[0].UserID != member {
		t.Errorf("requesters = %+v, %v; want the one who wrote", got, err)
	}
}

// An Agent firing gets its roster entry's output schema; a Workflow or a
// schedule without a target gets none.
func TestDeliveryOutputSchema(t *testing.T) {
	d := newDeliveryFixture(t)
	ctx := context.Background()
	s := d.schedules.Schedules[0]
	if got := d.door.DeliveryOutputSchema(ctx, s); got == nil || *got != string(resultSchema) {
		t.Errorf("agent schema = %v, want the roster entry's", got)
	}
	wf := s
	wf.ExecutorKind, wf.ExecutorID = coreschedule.ExecutorWorkflow, "wf_leave"
	plain := s
	plain.Delivery = nil
	for _, other := range []coreschedule.Schedule{wf, plain} {
		if got := d.door.DeliveryOutputSchema(ctx, other); got != nil {
			t.Errorf("schema for %+v = %q, want none", other, *got)
		}
	}
}
