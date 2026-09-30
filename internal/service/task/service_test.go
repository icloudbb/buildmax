package task

import (
	"context"
	"errors"
	"testing"
	"time"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	"github.com/icloudbb/buildmax/internal/core/apierr"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/mock"
)

func TestCreateRun_PersistsProvenance(t *testing.T) {
	taskStore := &mock.MockTaskStore{
		List: []coretask.Task{{
			ID:      "t_1",
			SpaceID: "tm_1",
			Status:  "SUCCEEDED",
		}},
	}
	runStore := &mock.MockTaskRunStore{}
	svc := &Service{
		Tasks:    taskStore,
		TaskRuns: runStore,
	}

	run, err := svc.CreateRun(context.Background(), CreateRunCmd{
		UserID:        "u1",
		TaskID:        "t_1",
		Input:         "try again",
		CreatedByType: coretask.RunCreatedByTypeUser,
		TriggerSource: coretask.RunTriggerSourcePortalConversation,
	})
	if err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if run == nil {
		t.Fatal("CreateRun returned nil run")
	}
	if len(runStore.Runs) != 1 {
		t.Fatalf("run count = %d, want 1", len(runStore.Runs))
	}
	got := runStore.Runs[0]
	if got.CreatedBy != "u1" {
		t.Fatalf("created_by = %q, want %q", got.CreatedBy, "u1")
	}
	if got.CreatedByType != coretask.RunCreatedByTypeUser {
		t.Fatalf("created_by_type = %q, want %q", got.CreatedByType, coretask.RunCreatedByTypeUser)
	}
	if got.TriggerSource != coretask.RunTriggerSourcePortalConversation {
		t.Fatalf("trigger_source = %q, want %q", got.TriggerSource, coretask.RunTriggerSourcePortalConversation)
	}
}

// normalizeCreateTaskProvenance / normalizeCreateRunProvenance are this
// value's one authoritative source (docs/design/portal-work-and-execution-experience.md
// slice 5): a caller that names no trigger gets the service's default, not an
// empty string a lower layer would have to guess how to fill in.
func TestCreateTask_DefaultsProvenanceWhenTheCallerNamesNone(t *testing.T) {
	taskStore := &mock.MockTaskStore{}
	svc := &Service{Tasks: taskStore}

	if _, err := svc.CreateTask(context.Background(), CreateTaskCmd{
		UserID:  "u1",
		SpaceID: "tm_1",
		Input:   "do the thing",
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if len(taskStore.Created) != 1 {
		t.Fatalf("CreateInput count = %d, want 1", len(taskStore.Created))
	}
	got := taskStore.Created[0]
	if got.InitialRunCreatedByType != coretask.RunCreatedByTypeUser {
		t.Errorf("initial_run_created_by_type = %q, want %q", got.InitialRunCreatedByType, coretask.RunCreatedByTypeUser)
	}
	if got.InitialRunTriggerSource != coretask.RunTriggerSourceTaskCreate {
		t.Errorf("initial_run_trigger_source = %q, want %q", got.InitialRunTriggerSource, coretask.RunTriggerSourceTaskCreate)
	}
}

func TestCreateRun_DefaultsProvenanceWhenTheCallerNamesNone(t *testing.T) {
	taskStore := &mock.MockTaskStore{List: []coretask.Task{{ID: "t_1", SpaceID: "tm_1", Status: "SUCCEEDED"}}}
	runStore := &mock.MockTaskRunStore{}
	svc := &Service{Tasks: taskStore, TaskRuns: runStore}

	if _, err := svc.CreateRun(context.Background(), CreateRunCmd{
		UserID: "u1",
		TaskID: "t_1",
		Input:  "try again",
	}); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	if len(runStore.Runs) != 1 {
		t.Fatalf("run count = %d, want 1", len(runStore.Runs))
	}
	got := runStore.Runs[0]
	if got.CreatedByType != coretask.RunCreatedByTypeUser {
		t.Errorf("created_by_type = %q, want %q", got.CreatedByType, coretask.RunCreatedByTypeUser)
	}
	if got.TriggerSource != coretask.RunTriggerSourceTaskRerun {
		t.Errorf("trigger_source = %q, want %q", got.TriggerSource, coretask.RunTriggerSourceTaskRerun)
	}
}

// Whitespace is no instruction. Admitting it spent a worker run and model
// tokens on nothing, where the empty string was already refused.
func TestBlankInputIsRefusedBeforeAnythingIsCreated(t *testing.T) {
	taskStore := &mock.MockTaskStore{List: []coretask.Task{{ID: "t_1", SpaceID: "tm_1", Status: "SUCCEEDED"}}}
	runStore := &mock.MockTaskRunStore{}
	svc := &Service{Tasks: taskStore, TaskRuns: runStore}

	for _, input := range []string{"", " ", "\n\t  \r\n"} {
		if _, err := svc.CreateTask(context.Background(), CreateTaskCmd{
			UserID: "u1", SpaceID: "tm_1", Input: input,
		}); !errors.Is(err, ErrInputRequired) {
			t.Errorf("CreateTask(%q) err = %v, want %v", input, err, ErrInputRequired)
		}
		if _, err := svc.CreateRun(context.Background(), CreateRunCmd{
			UserID: "u1", TaskID: "t_1", Input: input,
		}); !errors.Is(err, ErrInputRequired) {
			t.Errorf("CreateRun(%q) err = %v, want %v", input, err, ErrInputRequired)
		}
	}
	if len(taskStore.Created) != 0 || len(runStore.Runs) != 0 {
		t.Errorf("created %d tasks and %d runs from blank input, want none", len(taskStore.Created), len(runStore.Runs))
	}
}

// With an Agent selected, blank input means "no extra instruction" exactly as
// the empty string does: the run takes the Agent's own definition as its input.
func TestBlankInputWithAnAgentRunsTheAgentsDefinition(t *testing.T) {
	agentID := "a_1"
	taskStore := &mock.MockTaskStore{}
	svc := &Service{
		Tasks:  taskStore,
		Agents: &mock.MockAgentStore{Agents: []agentdef.Agent{{ID: agentID, SpaceID: "tm_1", Name: "digest", Instructions: "summarize"}}},
	}

	if _, err := svc.CreateTask(context.Background(), CreateTaskCmd{
		UserID: "u1", SpaceID: "tm_1", Input: "  \n", AgentID: &agentID,
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if len(taskStore.Created) != 1 || taskStore.Created[0].Input != buildTaskInputFromAgent(&agentdef.Agent{Name: "digest", Instructions: "summarize"}, "") {
		t.Fatalf("created = %+v, want one task whose input is the Agent's definition", taskStore.Created)
	}
}

// A deleted agent is invisible to every path that would start new work with
// it (agentdef.Agent.DeletedAt's own contract). Direct admission is one of
// those paths, and the only thing that keeps it that way is CreateTask
// resolving the agent through the same GetAgent a browsing caller uses, so
// this pins that resolution rather than a separate rule.
func TestCreateTaskRefusesADeletedAgent(t *testing.T) {
	deletedAt := time.Now().UTC()
	agentID := "a_deleted"
	agents := &mock.MockAgentStore{Agents: []agentdef.Agent{{
		ID: agentID, SpaceID: "tm_1", Name: "gone", DeletedAt: &deletedAt,
	}}}
	svc := &Service{
		Tasks:  &mock.MockTaskStore{},
		Agents: agents,
	}

	_, err := svc.CreateTask(context.Background(), CreateTaskCmd{
		UserID: "u1", SpaceID: "tm_1", Input: "do the thing", AgentID: &agentID,
	})
	if !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("CreateTask err = %v, want %v", err, ErrAgentNotFound)
	}
}

// Continue names no agent of its own -- it runs the task's. Deleting that
// agent between a task's first run and its Continue must refuse the same way
// admission would, not fall back to whatever the task remembers.
func TestCreateRunRefusesWhenTheTasksAgentWasDeleted(t *testing.T) {
	deletedAt := time.Now().UTC()
	agentID := "a_deleted"
	agents := &mock.MockAgentStore{Agents: []agentdef.Agent{{
		ID: agentID, SpaceID: "tm_1", Name: "gone", DeletedAt: &deletedAt,
	}}}
	taskStore := &mock.MockTaskStore{List: []coretask.Task{{
		ID: "t_1", SpaceID: "tm_1", Status: "SUCCEEDED", AgentID: &agentID,
	}}}
	svc := &Service{
		Tasks:    taskStore,
		TaskRuns: &mock.MockTaskRunStore{},
		Agents:   agents,
	}

	_, err := svc.CreateRun(context.Background(), CreateRunCmd{
		UserID: "u1", TaskID: "t_1", Input: "keep going",
		CreatedByType: coretask.RunCreatedByTypeUser,
		TriggerSource: coretask.RunTriggerSourcePortalTaskRerun,
	})
	if !errors.Is(err, ErrAgentNotFound) {
		t.Fatalf("CreateRun err = %v, want %v", err, ErrAgentNotFound)
	}
}

// quotaStub answers Check with whatever it was given.
type quotaStub struct {
	allowed bool
	reason  string
	err     error
}

func (q quotaStub) Check(context.Context, string, int, int) (bool, string, error) {
	return q.allowed, q.reason, q.err
}

// A limit that cannot be read is not a limit that passed. Admitting the run
// would spend a space's allowance without metering it, and the caller would see
// no difference from a space that had room.
func TestAdmitsRefusesWhenQuotaCannotBeRead(t *testing.T) {
	boom := errors.New("quota store unreachable")
	svc := &Service{QuotaChecker: quotaStub{err: boom}}

	err := svc.Admits(context.Background(), "tm_1")
	if !errors.Is(err, boom) {
		t.Fatalf("Admits err = %v, want %v", err, boom)
	}
	// A read failure is a 500, not the 429 an over-quota space gets: the space
	// is not over anything, the deployment cannot see.
	if kind, _ := apierr.KindOf(err); kind == apierr.KindQuotaExceeded {
		t.Error("an unreadable quota was reported to the caller as an exceeded one")
	}
}

func TestAdmitsRefusesAnOverQuotaSpaceWithTheQuotaKind(t *testing.T) {
	svc := &Service{QuotaChecker: quotaStub{allowed: false, reason: "quota exceeded: run limit"}}

	err := svc.Admits(context.Background(), "tm_1")
	if kind, _ := apierr.KindOf(err); kind != apierr.KindQuotaExceeded {
		t.Fatalf("Admits err kind = %q, want %q", kind, apierr.KindQuotaExceeded)
	}
}

// recordingQuota captures the last tokensToAdd it was asked about and refuses
// only when a token limit would be exceeded, so a test can tell a run-allowance
// question from a token one.
type recordingQuota struct {
	lastTokens  int
	refuseOnAny bool // refuse every Check, whatever the counts
}

func (q *recordingQuota) Check(_ context.Context, _ string, _ int, tokensToAdd int) (bool, string, error) {
	q.lastTokens = tokensToAdd
	if q.refuseOnAny {
		return false, "quota exceeded: run limit", nil
	}
	return true, "", nil
}

// bigTitleGenerator records whether it ran and returns a title whose token count
// would once have tipped a near-limit space over.
type bigTitleGenerator struct{ calls int }

func (g *bigTitleGenerator) GenerateTitle(context.Context, string) (string, int, int, error) {
	g.calls++
	return "a generated title", 4000, 4000, nil
}

// A space under its run limit creates the task even when the title it generates
// is expensive: the title's tokens are recorded as usage, not charged as a gate
// that would refuse the task after its conversation already existed. CreateTask
// asks the quota only about the run (zero tokens).
func TestCreateTaskDoesNotGateOnTitleTokens(t *testing.T) {
	taskStore := &mock.MockTaskStore{}
	quota := &recordingQuota{}
	gen := &bigTitleGenerator{}
	svc := &Service{Tasks: taskStore, QuotaChecker: quota, TitleGenerator: gen}

	if _, err := svc.CreateTask(context.Background(), CreateTaskCmd{
		UserID: "u1", SpaceID: "tm_1", Input: "do the thing",
	}); err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if quota.lastTokens != 0 {
		t.Errorf("quota was asked about %d tokens; the title must not be a gate", quota.lastTokens)
	}
	if len(taskStore.Created) != 1 {
		t.Fatalf("CreateInput count = %d, want 1", len(taskStore.Created))
	}
	got := taskStore.Created[0]
	if got.TitlePromptTokens != 4000 || got.TitleCompletionTokens != 4000 {
		t.Errorf("title tokens recorded = %d/%d, want 4000/4000",
			got.TitlePromptTokens, got.TitleCompletionTokens)
	}
}

// Out of runs, CreateTask refuses before it spends a title model call: the run
// allowance is asked first, so a refused task never pays for a title it will not
// keep.
func TestCreateTaskRefusesOutOfRunsBeforeGeneratingATitle(t *testing.T) {
	quota := &recordingQuota{refuseOnAny: true}
	gen := &bigTitleGenerator{}
	svc := &Service{Tasks: &mock.MockTaskStore{}, QuotaChecker: quota, TitleGenerator: gen}

	_, err := svc.CreateTask(context.Background(), CreateTaskCmd{
		UserID: "u1", SpaceID: "tm_1", Input: "do the thing",
	})
	if kind, _ := apierr.KindOf(err); kind != apierr.KindQuotaExceeded {
		t.Fatalf("CreateTask err kind = %q, want %q", kind, apierr.KindQuotaExceeded)
	}
	if gen.calls != 0 {
		t.Errorf("title generator ran %d times; a refused run must not spend a title call", gen.calls)
	}
}

// AdmitWorkflowTask without a key is a caller error: idempotent admission is
// meaningless without one, and falling through to a plain create would hide the
// mistake behind a task that a replay then duplicates.
func TestAdmitWorkflowTaskRequiresAKey(t *testing.T) {
	svc := &Service{Tasks: &mock.MockTaskStore{}}
	if _, err := svc.AdmitWorkflowTask(context.Background(), CreateTaskCmd{
		UserID: "u1", SpaceID: "tm_1", Input: "step work",
	}); !errors.Is(err, ErrAdmissionKeyRequired) {
		t.Fatalf("AdmitWorkflowTask with no key: err = %v, want ErrAdmissionKeyRequired", err)
	}
}

// AdmitWorkflowTask carries the key to the store and a replay under the same key
// returns the first task rather than creating a second -- the idempotency a
// Workflow node dispatch relies on when it is retried or recovered.
func TestAdmitWorkflowTaskIsIdempotentByKey(t *testing.T) {
	taskStore := &mock.MockTaskStore{}
	svc := &Service{Tasks: taskStore}
	cmd := CreateTaskCmd{
		UserID:        "u1",
		SpaceID:       "tm_1",
		Input:         "step work",
		CreatedByType: coretask.RunCreatedByTypeUser,
		TriggerSource: coretask.RunTriggerSourceWorkflowStep,
		AdmissionKey:  "workflow/wr_1/node/research",
	}
	first, err := svc.AdmitWorkflowTask(context.Background(), cmd)
	if err != nil {
		t.Fatalf("AdmitWorkflowTask (first): %v", err)
	}
	if len(taskStore.Created) != 1 {
		t.Fatalf("store recorded %d creates, want 1", len(taskStore.Created))
	}
	if taskStore.Created[0].AdmissionKey != cmd.AdmissionKey {
		t.Fatalf("store saw admission key %q, want %q", taskStore.Created[0].AdmissionKey, cmd.AdmissionKey)
	}
	if taskStore.Created[0].InitialRunTriggerSource != coretask.RunTriggerSourceWorkflowStep {
		t.Errorf("trigger source = %q, want %q", taskStore.Created[0].InitialRunTriggerSource, coretask.RunTriggerSourceWorkflowStep)
	}

	replay, err := svc.AdmitWorkflowTask(context.Background(), cmd)
	if err != nil {
		t.Fatalf("AdmitWorkflowTask (replay): %v", err)
	}
	if replay.ID != first.ID {
		t.Errorf("replay task = %q, want the original %q", replay.ID, first.ID)
	}
	if len(taskStore.Created) != 1 {
		t.Errorf("store created %d tasks, want 1: a replay must not create a second", len(taskStore.Created))
	}
}
