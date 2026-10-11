package work

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	coreartifact "github.com/icloudbb/buildmax/internal/core/artifact"
	coreconv "github.com/icloudbb/buildmax/internal/core/conversation"
	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	corespace "github.com/icloudbb/buildmax/internal/core/space"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/mock"
	artifactsvc "github.com/icloudbb/buildmax/internal/service/artifact"
	"github.com/icloudbb/buildmax/internal/testsupport"
	"github.com/icloudbb/buildmax/internal/util"
)

const provenanceSecret = "test-run-provenance-secret"

type provenanceFixture struct {
	handler   *Handler
	mux       *http.ServeMux
	messages  *mock.MockConversationMessageStore
	artifacts *mock.MockArtifactStore
}

func newProvenanceFixture(t *testing.T, run coretask.Run, task coretask.Task) provenanceFixture {
	t.Helper()
	messages := &mock.MockConversationMessageStore{}
	artifacts := &mock.MockArtifactStore{}
	h := New(Config{
		JWTSecret: provenanceSecret,
		Spaces: &mock.MockSpaceStore{
			Spaces:  []corespace.Space{{ID: "tm_1", Name: "My Space", PersonalForUserID: util.Ptr("u1"), CreatedBy: "u1"}},
			Members: []corespace.Member{{SpaceID: "tm_1", UserID: "u1", Role: corespace.RoleOwner}},
		},
		Conversations: &mock.MockConversationStore{
			Conversations: []coreconv.Conversation{{ID: "conv1", UserID: "u1", SpaceID: "tm_1", Channel: "portal", CreatedBy: "u1"}},
		},
		Tasks:     &mock.MockTaskStore{List: []coretask.Task{task}},
		TaskRuns:  &mock.MockTaskRunStore{Runs: []coretask.Run{run}, TaskList: []coretask.Task{task}},
		Messages:  messages,
		Artifacts: &artifactsvc.Service{Artifacts: artifacts, Storage: mock.NewMockArtifactStorage()},
	})
	mux := http.NewServeMux()
	h.Register(mux)
	return provenanceFixture{handler: h, mux: mux, messages: messages, artifacts: artifacts}
}

func (f provenanceFixture) get(t *testing.T, taskRunID string) (int, RunProvenanceResponse) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/spaces/tm_1/task-runs/"+taskRunID, nil)
	req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", provenanceSecret))
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	var out RunProvenanceResponse
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode body: %v", err)
		}
	}
	return rec.Code, out
}

func provenanceTask() coretask.Task {
	return coretask.Task{ID: "tk_1", ConversationID: "conv1", SpaceID: "tm_1", Status: "SUCCEEDED", Input: "x", CreatedBy: "u1"}
}

// The route exists so the instruction a worker was given can be read next to
// what the person actually asked for. Returning one without the other would
// make it pointless.
func TestRunProvenanceQuotesTheMessageBehindTheRun(t *testing.T) {
	run := coretask.Run{
		ID: "tr_1", TaskID: "tk_1", Input: "investigate the flaky test",
		Status: "SUCCEEDED", CreatedBy: "u1", CreatedByType: coretask.RunCreatedByTypeUser,
		TriggerSource: coretask.RunTriggerSourcePortalConversation, CreatedAt: time.Unix(1000, 0).UTC(),
	}
	f := newProvenanceFixture(t, run, provenanceTask())
	asked, err := f.messages.AppendMessage(t.Context(), coreconv.AppendInput{
		ConversationID: "conv1",
		Role:           "user",
		Content:        "look into the flaky test, but leave the CI config alone",
	})
	if err != nil {
		t.Fatal(err)
	}
	f.handler.cfg.TaskRuns.(*mock.MockTaskRunStore).Runs[0].SourceMessageID = &asked.ID

	code, out := f.get(t, "tr_1")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if out.SourceMessage == nil {
		t.Fatal("no source message returned")
	}
	if out.SourceMessage.ID != asked.ID || out.SourceMessage.Content != asked.Content {
		t.Errorf("source message = %+v, want %s / %q", out.SourceMessage, asked.ID, asked.Content)
	}
	if out.SourceMessage.Truncated {
		t.Error("a short message should not be marked truncated")
	}
	if out.Input != run.Input {
		t.Errorf("input = %q, want the run's own instruction", out.Input)
	}
	if out.TriggerSource != coretask.RunTriggerSourcePortalConversation {
		t.Errorf("trigger_source = %q", out.TriggerSource)
	}
}

// A long message is quoted for comparison, not served whole: the conversation
// route is where the transcript lives.
func TestRunProvenanceTruncatesALongMessage(t *testing.T) {
	run := coretask.Run{ID: "tr_1", TaskID: "tk_1", Input: "do it", Status: "SUCCEEDED", CreatedAt: time.Unix(1000, 0).UTC()}
	f := newProvenanceFixture(t, run, provenanceTask())
	asked, err := f.messages.AppendMessage(t.Context(), coreconv.AppendInput{
		ConversationID: "conv1",
		Role:           "user",
		Content:        strings.Repeat("a", sourceMessageMaxLen+50),
	})
	if err != nil {
		t.Fatal(err)
	}
	f.handler.cfg.TaskRuns.(*mock.MockTaskRunStore).Runs[0].SourceMessageID = &asked.ID

	_, out := f.get(t, "tr_1")
	if out.SourceMessage == nil || !out.SourceMessage.Truncated {
		t.Fatalf("source message = %+v, want a truncated quote", out.SourceMessage)
	}
	if len([]rune(out.SourceMessage.Content)) > sourceMessageMaxLen+1 {
		t.Errorf("quote is %d runes, want at most %d plus the ellipsis", len([]rune(out.SourceMessage.Content)), sourceMessageMaxLen)
	}
}

// A run with no message behind it is normal — a workflow step, an issue agent
// run, a retry. The rest of the provenance is still true and still answered.
func TestRunProvenanceWithoutASourceMessage(t *testing.T) {
	run := coretask.Run{
		ID: "tr_1", TaskID: "tk_1", Input: "step 2", Status: "RUNNING",
		CreatedByType: coretask.RunCreatedByTypeSystem, TriggerSource: coretask.RunTriggerSourceWorkflowStep, CreatedAt: time.Unix(1000, 0).UTC(),
	}
	f := newProvenanceFixture(t, run, provenanceTask())

	code, out := f.get(t, "tr_1")
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if out.SourceMessage != nil {
		t.Errorf("source message = %+v, want none", out.SourceMessage)
	}
	if out.TriggerSource != coretask.RunTriggerSourceWorkflowStep {
		t.Errorf("trigger_source = %q, want the workflow step", out.TriggerSource)
	}
}

// A handle pointing outside the run's own conversation quotes nothing. It is
// how a stale or wrong reference stops being a way to read someone else's text.
func TestRunProvenanceIgnoresAMessageFromAnotherConversation(t *testing.T) {
	run := coretask.Run{ID: "tr_1", TaskID: "tk_1", Input: "do it", Status: "SUCCEEDED", CreatedAt: time.Unix(1000, 0).UTC()}
	f := newProvenanceFixture(t, run, provenanceTask())
	elsewhere, err := f.messages.AppendMessage(t.Context(), coreconv.AppendInput{
		ConversationID: "conv-other",
		Role:           "user",
		Content:        "something said in another conversation",
	})
	if err != nil {
		t.Fatal(err)
	}
	f.handler.cfg.TaskRuns.(*mock.MockTaskRunStore).Runs[0].SourceMessageID = &elsewhere.ID

	_, out := f.get(t, "tr_1")
	if out.SourceMessage != nil {
		t.Errorf("source message = %+v, want none", out.SourceMessage)
	}
}

// The run belongs to a space, and a stranger to that space cannot read where it
// came from any more than what it produced.
func TestRunProvenanceRefusesAnotherSpace(t *testing.T) {
	run := coretask.Run{ID: "tr_1", TaskID: "tk_1", Input: "do it", Status: "SUCCEEDED", CreatedAt: time.Unix(1000, 0).UTC()}
	f := newProvenanceFixture(t, run, provenanceTask())

	req := httptest.NewRequest(http.MethodGet, "/api/spaces/tm_other/task-runs/tr_1", nil)
	req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", provenanceSecret))
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("status = 200, want a refusal; body = %s", rec.Body.String())
	}
}

// An agent's instructions are resolved when its worker asks for the run, so two
// runs of one task can execute different text. The run names the revision it
// was handed, and the response says when the definition has moved on since.
func TestRunProvenanceNamesTheAgentRevisionThatRan(t *testing.T) {
	revision := 2
	run := coretask.Run{
		ID: "tr_1", TaskID: "tk_1", Input: "do it", Status: "SUCCEEDED",
		AgentRevision: &revision, CreatedAt: time.Unix(1000, 0).UTC(),
	}
	task := provenanceTask()
	task.AgentID = util.Ptr("ag_1")
	f := newProvenanceFixture(t, run, task)
	f.handler.cfg.Agents = &mock.MockAgentStore{Agents: []agentdef.Agent{
		{ID: "ag_1", SpaceID: "tm_1", Name: "Reviewer", Revision: 5},
	}}

	_, out := f.get(t, "tr_1")
	if out.Agent == nil {
		t.Fatal("no agent returned")
	}
	if out.Agent.Revision != 2 {
		t.Errorf("revision = %d, want the one the run was handed", out.Agent.Revision)
	}
	if out.Agent.CurrentRevision != 5 {
		t.Errorf("current_revision = %d, want what the definition says now", out.Agent.CurrentRevision)
	}
	if out.Agent.Name != "Reviewer" {
		t.Errorf("name = %q", out.Agent.Name)
	}
}

// Space instructions are resolved independently from the agent. Their revision
// makes the global context behind an old run visible even after the Space is
// edited.
func TestRunProvenanceNamesTheSpaceInstructionsRevisionThatRan(t *testing.T) {
	revision := 2
	run := coretask.Run{
		ID: "tr_1", TaskID: "tk_1", Input: "do it", Status: "SUCCEEDED",
		SpaceAgentInstructionsRevision: &revision, CreatedAt: time.Unix(1000, 0).UTC(),
	}
	f := newProvenanceFixture(t, run, provenanceTask())
	f.handler.cfg.Spaces.(*mock.MockSpaceStore).Spaces[0].AgentInstructionsRevision = 5

	_, out := f.get(t, "tr_1")
	if out.SpaceInstructions == nil {
		t.Fatal("no Space instructions provenance returned")
	}
	if out.SpaceInstructions.Revision != 2 {
		t.Errorf("revision = %d, want the one the run was handed", out.SpaceInstructions.Revision)
	}
	if out.SpaceInstructions.CurrentRevision != 5 {
		t.Errorf("current_revision = %d, want what the Space says now", out.SpaceInstructions.CurrentRevision)
	}
}

// A deleted agent is still named. A run that already executed under it does not
// stop having done so, and hiding the definition is the opposite of provenance.
func TestRunProvenanceNamesADeletedAgent(t *testing.T) {
	revision := 1
	run := coretask.Run{ID: "tr_1", TaskID: "tk_1", Input: "do it", Status: "SUCCEEDED", AgentRevision: &revision, CreatedAt: time.Unix(1000, 0).UTC()}
	task := provenanceTask()
	task.AgentID = util.Ptr("ag_1")
	f := newProvenanceFixture(t, run, task)
	f.handler.cfg.Agents = &mock.MockAgentStore{Agents: []agentdef.Agent{
		{ID: "ag_1", SpaceID: "tm_1", Name: "Retired", Revision: 1, DeletedAt: util.Ptr(time.Unix(9, 0).UTC())},
	}}

	_, out := f.get(t, "tr_1")
	if out.Agent == nil || !out.Agent.Deleted || out.Agent.Name != "Retired" {
		t.Fatalf("agent = %+v, want the deleted definition named", out.Agent)
	}
}

// A task with no agent has no agent block at all, rather than an empty one that
// reads as an agent nobody can identify.
func TestRunProvenanceOmitsTheAgentWhenThereIsNone(t *testing.T) {
	run := coretask.Run{ID: "tr_1", TaskID: "tk_1", Input: "do it", Status: "SUCCEEDED", CreatedAt: time.Unix(1000, 0).UTC()}
	f := newProvenanceFixture(t, run, provenanceTask())

	_, out := f.get(t, "tr_1")
	if out.Agent != nil {
		t.Errorf("agent = %+v, want none", out.Agent)
	}
}

// The releases a run actually resolved are what a Portal reader needs to
// answer "why did this run have this capability" -- not what the agent
// currently names, which can have moved on since. See
// docs/design/portal-data-and-plugin-surfaces.md.
func TestRunProvenanceNamesTheResolvedPlugins(t *testing.T) {
	run := coretask.Run{
		ID: "tr_1", TaskID: "tk_1", Input: "do it", Status: "SUCCEEDED", CreatedAt: time.Unix(1000, 0).UTC(),
		PluginPins: []coreplugin.Pin{{PluginName: "code-review", Version: "1.2.0", Digest: "sha256:abc"}},
	}
	f := newProvenanceFixture(t, run, provenanceTask())

	_, out := f.get(t, "tr_1")
	if len(out.PluginPins) != 1 || out.PluginPins[0].PluginName != "code-review" || out.PluginPins[0].Version != "1.2.0" {
		t.Errorf("plugin_pins = %+v, want the run's resolved pin", out.PluginPins)
	}
}

// A run that resolved no plugins reports none, not an empty list standing in
// for "we don't know".
func TestRunProvenanceOmitsPluginPinsWhenNoneResolved(t *testing.T) {
	run := coretask.Run{ID: "tr_1", TaskID: "tk_1", Input: "do it", Status: "SUCCEEDED", CreatedAt: time.Unix(1000, 0).UTC()}
	f := newProvenanceFixture(t, run, provenanceTask())

	_, out := f.get(t, "tr_1")
	if len(out.PluginPins) != 0 {
		t.Errorf("plugin_pins = %+v, want none", out.PluginPins)
	}
}

// What this run published is looked up by its own id through the artifact
// service, the same way an issue's output list is, rather than a Portal-only
// record of what a run produced.
func TestRunProvenanceListsWhatTheRunPublished(t *testing.T) {
	run := coretask.Run{ID: "tr_1", TaskID: "tk_1", Input: "do it", Status: "SUCCEEDED", CreatedAt: time.Unix(1000, 0).UTC()}
	f := newProvenanceFixture(t, run, provenanceTask())
	if _, err := f.artifacts.CreateArtifact(t.Context(), coreartifact.CreateInput{
		SpaceID: "tm_1", ArtifactID: "tsyt7at6cjfr33d73mta", Filename: "report.pdf",
		MediaType: "application/pdf", SizeBytes: 2048,
		SourceType: coreartifact.SourceTaskRun, SourceID: "tr_1",
		CreatedByType: coreartifact.CreatorAgent, Title: "Quarterly report",
	}); err != nil {
		t.Fatal(err)
	}

	_, out := f.get(t, "tr_1")
	if len(out.Artifacts) != 1 {
		t.Fatalf("artifacts = %+v, want the one published", out.Artifacts)
	}
	got := out.Artifacts[0]
	if got.ID != "tsyt7at6cjfr33d73mta" || got.Title != "Quarterly report" || got.Filename != "report.pdf" {
		t.Errorf("artifact = %+v", got)
	}
}

// A run that published nothing reports no artifacts, not an empty list
// standing in for "we don't know".
func TestRunProvenanceOmitsArtifactsWhenNonePublished(t *testing.T) {
	run := coretask.Run{ID: "tr_1", TaskID: "tk_1", Input: "do it", Status: "SUCCEEDED", CreatedAt: time.Unix(1000, 0).UTC()}
	f := newProvenanceFixture(t, run, provenanceTask())

	_, out := f.get(t, "tr_1")
	if len(out.Artifacts) != 0 {
		t.Errorf("artifacts = %+v, want none", out.Artifacts)
	}
}

// A reader opens a failed run asking why it ended. The answer is the class and
// the cause the server recorded, with the server's own text beside them, and
// whether a trace exists so the reader is not sent to fetch one that does not.
func TestRunProvenanceExplainsAFailedRun(t *testing.T) {
	run := coretask.Run{
		ID: "tr_1", TaskID: "tk_1", Input: "deploy", Status: "FAILED", CreatedAt: time.Unix(1000, 0).UTC(),
		ErrorMessage: util.Ptr("secret grant unavailable: secret is disabled (409)"),
		FailureClass: string(coretask.FailureSpaceConfiguration),
		FailureCause: &coretask.FailureCause{Kind: coretask.FailureCauseSecretGrant, SecretID: "sec_gh", SecretProblem: coretask.SecretDisabled},
	}
	f := newProvenanceFixture(t, run, provenanceTask())

	req := httptest.NewRequest(http.MethodGet, "/api/spaces/tm_1/task-runs/tr_1", nil)
	req.Header.Set("Authorization", "Bearer "+testsupport.SignJWT("u1", provenanceSecret))
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if got := string(raw["failure_class"]); got != `"space_configuration"` {
		t.Errorf("failure_class = %s", got)
	}
	if got := string(raw["failure_cause"]); got != `{"kind":"secret_grant","secret_id":"sec_gh","secret_problem":"disabled"}` {
		t.Errorf("failure_cause = %s", got)
	}
	if got := string(raw["error_message"]); !strings.Contains(got, "secret is disabled") {
		t.Errorf("error_message = %s", got)
	}
	if got := string(raw["trace_recorded"]); got != "false" {
		t.Errorf("trace_recorded = %s, want false for a run with no trace", got)
	}
}

func TestRunProvenanceSaysATraceWasRecorded(t *testing.T) {
	run := coretask.Run{
		ID: "tr_1", TaskID: "tk_1", Input: "do it", Status: "SUCCEEDED", CreatedAt: time.Unix(1000, 0).UTC(),
		TracePath: util.Ptr("traces/s/rt_1.jsonl"),
	}
	f := newProvenanceFixture(t, run, provenanceTask())
	_, out := f.get(t, "tr_1")
	if !out.TraceRecorded {
		t.Error("trace_recorded = false, want true")
	}
	if out.FailureClass != "" || out.FailureCause != nil || out.ErrorMessage != nil {
		t.Errorf("a succeeded run carries a failure: %+v", out)
	}
}
