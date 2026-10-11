package worker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	"github.com/icloudbb/buildmax/internal/core/apierr"
	coresecret "github.com/icloudbb/buildmax/internal/core/secret"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/mock"
	secretsvc "github.com/icloudbb/buildmax/internal/service/secret"
	"github.com/icloudbb/buildmax/internal/util"
)

// fakeMaterializer answers Materialize from a fixed set, or a typed error.
type fakeMaterializer struct {
	items    map[string]coresecret.Items // secretID -> items
	disabled map[string]bool
	broken   map[string]bool // the backend fails, which is not a refusal
}

func (f fakeMaterializer) Materialize(_ context.Context, _, id string) (coresecret.Items, error) {
	if f.disabled[id] {
		return nil, secretsvc.ErrDisabled
	}
	if f.broken[id] {
		return nil, errors.New("decrypt: key unavailable")
	}
	if it, ok := f.items[id]; ok {
		return it, nil
	}
	return nil, apierr.ErrNotFound
}

func secretsHandler(t *testing.T, cons agentdef.SecretConsumption, mat SecretMaterializer) (*http.ServeMux, string) {
	t.Helper()
	mux, id, _ := secretsHandlerWithRuns(t, cons, mat)
	return mux, id
}

func secretsHandlerWithRuns(t *testing.T, cons agentdef.SecretConsumption, mat SecretMaterializer) (*http.ServeMux, string, *mock.MockTaskRunStore) {
	t.Helper()
	const taskRunID, taskID, spaceID = "run-1", "task-1", "tm_1"
	agentID := "a_1"
	// The consumption is read from the pinned revision, not the live agent. The
	// run carries revision 1, and the agent's current definition consumes
	// nothing — so a test that got its grants must have read the pinned one.
	run := coretask.Run{ID: taskRunID, TaskID: taskID, Status: "RUNNING", AgentRevision: util.Ptr(1), CreatedAt: time.Unix(1, 0).UTC()}
	task := coretask.Task{ID: taskID, ConversationID: "conv-1", SpaceID: spaceID, CreatedBy: "u1", AgentID: &agentID}
	runs := &mock.MockTaskRunStore{Runs: []coretask.Run{run}, TaskList: []coretask.Task{task}}
	agents := &mock.MockAgentStore{
		Agents:    []agentdef.Agent{{ID: agentID, SpaceID: spaceID, Name: "deployer", Revision: 2}},
		Revisions: []agentdef.Revision{{AgentID: agentID, Revision: 1, SecretConsumption: cons}},
	}
	h := New(Config{JWTSecret: workerTestSecret, TaskRuns: runs, Agents: agents, Secrets: mat})
	mux := http.NewServeMux()
	h.Register(mux)
	return mux, taskRunID, runs
}

func getSecrets(t *testing.T, mux *http.ServeMux, taskRunID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/worker/task-runs/"+taskRunID+"/secrets", nil)
	req.Header.Set("Authorization", "Bearer "+runTokenFor(t, taskRunID, "task-1"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestGetTaskRunSecrets_ResolvesGrants(t *testing.T) {
	cons := agentdef.SecretConsumption{Env: []agentdef.SecretEnvGrant{
		{Secret: "sec_gh", Item: "token", EnvName: "GH_TOKEN"},
		{Secret: "sec_aws", Prefix: "AWS_"},
	}}
	mat := fakeMaterializer{items: map[string]coresecret.Items{
		"sec_gh":  {"token": "ghs_abc"},
		"sec_aws": {"access_key_id": "AKIA", "region": "us-east-1"},
	}}
	mux, id := secretsHandler(t, cons, mat)
	rec := getSecrets(t, mux, id)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if cc := rec.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}
	var got struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"GH_TOKEN": "ghs_abc", "AWS_access_key_id": "AKIA", "AWS_region": "us-east-1"}
	if len(got.Env) != len(want) {
		t.Fatalf("env = %v, want %v", got.Env, want)
	}
	for k, v := range want {
		if got.Env[k] != v {
			t.Fatalf("env[%q] = %q, want %q", k, got.Env[k], v)
		}
	}
}

// TestGetTaskRunSecrets_UsesPinnedRevisionNotLiveAgent is the guarantee that
// pinning buys: an agent whose consumption was edited after the run was
// dispatched cannot widen (or narrow) what the in-flight run receives. The run
// is pinned to revision 1, which consumes PINNED; the live agent is now on
// revision 5 consuming LIVE. Only the pinned grant may come back.
func TestGetTaskRunSecrets_UsesPinnedRevisionNotLiveAgent(t *testing.T) {
	const taskRunID, taskID, spaceID = "run-1", "task-1", "tm_1"
	agentID := "a_1"
	run := coretask.Run{ID: taskRunID, TaskID: taskID, Status: "RUNNING", AgentRevision: util.Ptr(1), CreatedAt: time.Unix(1, 0).UTC()}
	task := coretask.Task{ID: taskID, SpaceID: spaceID, CreatedBy: "u1", AgentID: &agentID}
	runs := &mock.MockTaskRunStore{Runs: []coretask.Run{run}, TaskList: []coretask.Task{task}}
	pinned := agentdef.SecretConsumption{Env: []agentdef.SecretEnvGrant{{Secret: "sec_pinned", Item: "k", EnvName: "PINNED"}}}
	live := agentdef.SecretConsumption{Env: []agentdef.SecretEnvGrant{{Secret: "sec_live", Item: "k", EnvName: "LIVE"}}}
	agents := &mock.MockAgentStore{
		Agents:    []agentdef.Agent{{ID: agentID, SpaceID: spaceID, Revision: 5, SecretConsumption: live}},
		Revisions: []agentdef.Revision{{AgentID: agentID, Revision: 1, SecretConsumption: pinned}},
	}
	mat := fakeMaterializer{items: map[string]coresecret.Items{
		"sec_pinned": {"k": "pinned-value"},
		"sec_live":   {"k": "live-value"},
	}}
	h := New(Config{JWTSecret: workerTestSecret, TaskRuns: runs, Agents: agents, Secrets: mat})
	mux := http.NewServeMux()
	h.Register(mux)

	rec := getSecrets(t, mux, taskRunID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Env["PINNED"] != "pinned-value" {
		t.Errorf("pinned grant missing: env = %v", got.Env)
	}
	if _, leaked := got.Env["LIVE"]; leaked {
		t.Errorf("the live revision's grant leaked into a pinned run: env = %v", got.Env)
	}
}

func TestGetTaskRunSecrets_NoConsumptionIsEmpty(t *testing.T) {
	mux, id := secretsHandler(t, agentdef.SecretConsumption{}, fakeMaterializer{})
	rec := getSecrets(t, mux, id)
	if rec.Code != http.StatusOK || rec.Body.String() == "" {
		t.Fatalf("status = %d body = %q", rec.Code, rec.Body.String())
	}
	var got struct {
		Env map[string]string `json:"env"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if len(got.Env) != 0 {
		t.Fatalf("env = %v, want empty", got.Env)
	}
}

func TestGetTaskRunSecrets_RequiredDisabledFails(t *testing.T) {
	cons := agentdef.SecretConsumption{Env: []agentdef.SecretEnvGrant{
		{Secret: "sec_gh", Item: "token", EnvName: "GH_TOKEN"},
	}}
	mat := fakeMaterializer{disabled: map[string]bool{"sec_gh": true}}
	mux, id := secretsHandler(t, cons, mat)
	rec := getSecrets(t, mux, id)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: %s", rec.Code, rec.Body.String())
	}
}

// A refused grant names its Secret on the run, so the person reading the
// failure is led to the grant to fix instead of a retry that fails the same way.
func TestGetTaskRunSecrets_RecordsTheRefusedGrantAsTheCause(t *testing.T) {
	cases := []struct {
		name  string
		grant agentdef.SecretEnvGrant
		mat   fakeMaterializer
		want  coretask.FailureCause
	}{
		{
			name:  "disabled",
			grant: agentdef.SecretEnvGrant{Secret: "sec_gh", Item: "token", EnvName: "GH_TOKEN"},
			mat:   fakeMaterializer{disabled: map[string]bool{"sec_gh": true}},
			want:  coretask.FailureCause{Kind: coretask.FailureCauseSecretGrant, SecretID: "sec_gh", SecretProblem: coretask.SecretDisabled},
		},
		{
			name:  "gone",
			grant: agentdef.SecretEnvGrant{Secret: "sec_gone", Prefix: "GONE_"},
			mat:   fakeMaterializer{},
			want:  coretask.FailureCause{Kind: coretask.FailureCauseSecretGrant, SecretID: "sec_gone", SecretProblem: coretask.SecretUnavailable},
		},
		{
			name:  "item missing",
			grant: agentdef.SecretEnvGrant{Secret: "sec_gh", Item: "token", EnvName: "GH_TOKEN"},
			mat:   fakeMaterializer{items: map[string]coresecret.Items{"sec_gh": {"other": "v"}}},
			want: coretask.FailureCause{
				Kind: coretask.FailureCauseSecretGrant, SecretID: "sec_gh",
				SecretProblem: coretask.SecretItemMissing, SecretItem: "token",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cons := agentdef.SecretConsumption{Env: []agentdef.SecretEnvGrant{tc.grant}}
			mux, id, runs := secretsHandlerWithRuns(t, cons, tc.mat)
			if rec := getSecrets(t, mux, id); rec.Code == http.StatusOK {
				t.Fatalf("status = 200, want a refusal")
			}
			got := runs.Runs[0].FailureCause
			if got == nil || *got != tc.want {
				t.Fatalf("failure_cause = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// A backend failure is not the Space's configuration: nothing names a Secret
// to fix, so the run's class alone explains it.
func TestGetTaskRunSecrets_BackendFailureRecordsNoCause(t *testing.T) {
	cons := agentdef.SecretConsumption{Env: []agentdef.SecretEnvGrant{{Secret: "sec_gh", Item: "token", EnvName: "GH_TOKEN"}}}
	mux, id, runs := secretsHandlerWithRuns(t, cons, fakeMaterializer{broken: map[string]bool{"sec_gh": true}})
	if rec := getSecrets(t, mux, id); rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if got := runs.Runs[0].FailureCause; got != nil {
		t.Fatalf("failure_cause = %+v, want none", got)
	}
}

func TestGetTaskRunSecrets_OptionalMissingSkipped(t *testing.T) {
	cons := agentdef.SecretConsumption{Env: []agentdef.SecretEnvGrant{
		{Secret: "sec_present", Item: "token", EnvName: "PRESENT"},
		{Secret: "sec_absent", Item: "token", EnvName: "ABSENT", Optional: true},
	}}
	mat := fakeMaterializer{items: map[string]coresecret.Items{"sec_present": {"token": "v"}}}
	mux, id := secretsHandler(t, cons, mat)
	rec := getSecrets(t, mux, id)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	var got struct {
		Env map[string]string `json:"env"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.Env["PRESENT"] != "v" || len(got.Env) != 1 {
		t.Fatalf("env = %v, want only PRESENT", got.Env)
	}
}

func TestGetTaskRunSecrets_FeatureOffIsEmpty(t *testing.T) {
	// No Secrets materializer: the route answers an empty grant set, not an error.
	cons := agentdef.SecretConsumption{Env: []agentdef.SecretEnvGrant{{Secret: "s", Item: "i", EnvName: "X"}}}
	const taskRunID, taskID, spaceID = "run-1", "task-1", "tm_1"
	agentID := "a_1"
	run := coretask.Run{ID: taskRunID, TaskID: taskID, Status: "RUNNING", AgentRevision: util.Ptr(1), CreatedAt: time.Unix(1, 0).UTC()}
	task := coretask.Task{ID: taskID, SpaceID: spaceID, CreatedBy: "u1", AgentID: &agentID}
	runs := &mock.MockTaskRunStore{Runs: []coretask.Run{run}, TaskList: []coretask.Task{task}}
	agents := &mock.MockAgentStore{
		Agents:    []agentdef.Agent{{ID: agentID, SpaceID: spaceID, Revision: 1}},
		Revisions: []agentdef.Revision{{AgentID: agentID, Revision: 1, SecretConsumption: cons}},
	}
	h := New(Config{JWTSecret: workerTestSecret, TaskRuns: runs, Agents: agents}) // Secrets nil
	mux := http.NewServeMux()
	h.Register(mux)
	rec := getSecrets(t, mux, taskRunID)
	if rec.Code != http.StatusOK {
		t.Fatalf("feature-off status = %d, want 200", rec.Code)
	}
}
