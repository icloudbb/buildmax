package worker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	agentdef "github.com/icloudbb/buildmax/internal/core/agentdef"
	coreplugin "github.com/icloudbb/buildmax/internal/core/plugin"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	"github.com/icloudbb/buildmax/internal/infra/pluginwire"
	"github.com/icloudbb/buildmax/internal/infra/workerclient"
	"github.com/icloudbb/buildmax/internal/mock"
	"github.com/icloudbb/buildmax/internal/service/audit"
	pluginsvc "github.com/icloudbb/buildmax/internal/service/plugin"
)

const (
	pluginTestSpace = "tm_1"
	pluginTestRun   = "tr_1"
)

type pinFixture struct {
	mux         *http.ServeMux
	runs        *mock.MockTaskRunStore
	activations *mock.MockPluginActivationStore
	agents      *mock.MockAgentStore
	catalog     *mock.MockPluginStore
	packages    *mock.MockPluginPackageStorage
}

// newPinFixture wires the real worker routes over in-memory stores, with one
// task whose agent names the plugins the caller passes.
func newPinFixture(t *testing.T, agentPlugins []string) *pinFixture {
	t.Helper()
	agents := &mock.MockAgentStore{}
	created, err := agents.CreateAgentInSpace(context.Background(), agentdef.CreateInput{
		SpaceID: pluginTestSpace, UserID: "u_1",
		Def: agentdef.Definition{Name: "Reviewer", Plugins: agentPlugins},
	})
	if err != nil {
		t.Fatalf("CreateAgentInSpace: %v", err)
	}
	agentID := created.ID
	runs := &mock.MockTaskRunStore{
		Runs:     []coretask.Run{{ID: pluginTestRun, TaskID: "tk_1", Status: string(coretask.RunStatusRunning)}},
		TaskList: []coretask.Task{{ID: "tk_1", SpaceID: pluginTestSpace, CreatedBy: "u_1", AgentID: &agentID}},
	}
	catalog := mock.NewMockPluginStore()
	packages := mock.NewMockPluginPackageStorage()
	activations := mock.NewMockPluginActivationStore()
	h := New(Config{
		JWTSecret:   workerTestSecret,
		TaskRuns:    runs,
		Agents:      agents,
		Activations: activations,
		Plugins: &pluginsvc.Service{
			Catalog: catalog, Activations: activations, Packages: packages,
			KeyPrefix: "bm", Audit: audit.NewRecorder(&mock.MockAuditStore{}),
		},
	})
	mux := http.NewServeMux()
	h.Register(mux)
	return &pinFixture{mux: mux, runs: runs, activations: activations, agents: agents, catalog: catalog, packages: packages}
}

func (f *pinFixture) activate(t *testing.T, name, version, digest string, enabled bool) {
	t.Helper()
	ctx := context.Background()
	if _, err := f.activations.ActivatePlugin(ctx, coreplugin.ActivateInput{
		SpaceID: pluginTestSpace, PluginName: name, Version: version, Digest: digest,
		Origin: coreplugin.ActivationCurated, ActorID: "u_1",
	}); err != nil {
		t.Fatalf("ActivatePlugin: %v", err)
	}
	if !enabled {
		if _, err := f.activations.SetPluginActivationEnabled(ctx, pluginTestSpace, name, false, "u_1"); err != nil {
			t.Fatalf("SetPluginActivationEnabled: %v", err)
		}
	}
}

// claim drives the route a worker calls to fetch its run.
func (f *pinFixture) claim(t *testing.T) workerclient.GetTaskRunResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/worker/task-runs/"+pluginTestRun, nil)
	req.Header.Set("Authorization", "Bearer "+runTokenFor(t, pluginTestRun, "tk_1"))
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("claim status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var out workerclient.GetTaskRunResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func TestClaimResolvesAndRecordsPins(t *testing.T) {
	f := newPinFixture(t, []string{"code-review"})
	f.activate(t, "code-review", "1.2.0", "sha256:abc", true)

	got := f.claim(t)
	if got.PluginError != "" {
		t.Fatalf("unexpected refusal: %s", got.PluginError)
	}
	if len(got.Plugins) != 1 {
		t.Fatalf("plugins = %+v, want one", got.Plugins)
	}
	if got.Plugins[0].Version != "1.2.0" || got.Plugins[0].Digest != "sha256:abc" {
		t.Errorf("pin = %+v, want 1.2.0 with its digest", got.Plugins[0])
	}
	if len(f.runs.Runs[0].PluginPins) != 1 {
		t.Error("the run did not record what it was given; nothing else can say afterwards")
	}
}

// A worker polls its run while it executes. A space moving a pin in that window
// must not change what the run was given.
func TestASecondClaimKeepsTheRecordedPins(t *testing.T) {
	f := newPinFixture(t, []string{"code-review"})
	f.activate(t, "code-review", "1.0.0", "sha256:one", true)
	if got := f.claim(t); got.Plugins[0].Version != "1.0.0" {
		t.Fatalf("first claim = %+v", got.Plugins)
	}

	if _, err := f.activations.MovePluginActivationPin(context.Background(), coreplugin.MovePinInput{
		SpaceID: pluginTestSpace, PluginName: "code-review", Version: "2.0.0", Digest: "sha256:two", ActorID: "u_1",
	}); err != nil {
		t.Fatalf("MovePluginActivationPin: %v", err)
	}

	got := f.claim(t)
	if got.Plugins[0].Version != "1.0.0" {
		t.Errorf("second claim = %q; a pin moved mid-run must not change what this run has", got.Plugins[0].Version)
	}
}

func TestClaimRefusesAnUnactivatedPlugin(t *testing.T) {
	f := newPinFixture(t, []string{"code-review"})

	got := f.claim(t)
	if got.PluginError == "" {
		t.Fatal("want a refusal naming the plugin")
	}
	if len(got.Plugins) != 0 {
		t.Errorf("a refused run was still given plugins: %+v", got.Plugins)
	}
	if len(f.runs.Runs[0].PluginPins) != 0 {
		t.Error("a refused run recorded pins")
	}
}

// Suspending stops the agents that name it, visibly.
func TestClaimRefusesASuspendedActivation(t *testing.T) {
	f := newPinFixture(t, []string{"code-review"})
	f.activate(t, "code-review", "1.0.0", "sha256:abc", false)

	got := f.claim(t)
	if got.PluginError == "" {
		t.Fatal("a suspended activation must fail the run rather than drop the plugin")
	}
}

// The worker fails a run it is refused, and the failure names the plugin and
// why, so the person reading it is led to the Space's plugin settings.
func TestClaimRecordsTheRefusedPluginAsTheCause(t *testing.T) {
	cases := []struct {
		name     string
		activate func(f *pinFixture)
		want     coretask.PluginProblem
	}{
		{"not activated", func(*pinFixture) {}, coretask.PluginNotActivated},
		{"suspended", func(f *pinFixture) { f.activate(t, "code-review", "1.0.0", "sha256:abc", false) }, coretask.PluginSuspended},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newPinFixture(t, []string{"code-review"})
			f.runs.Runs[0].Status = string(coretask.RunStatusScheduled)
			c.activate(f)

			if got := f.claim(t); got.PluginError == "" {
				t.Fatal("want a refusal")
			}
			want := coretask.FailureCause{Kind: coretask.FailureCausePlugin, PluginName: "code-review", PluginProblem: c.want}
			if got := f.runs.Runs[0].FailureCause; got == nil || *got != want {
				t.Errorf("cause = %+v, want %+v", got, want)
			}
		})
	}
}

// Only a worker starting the run acts on a refusal. A run already under way
// polls this route against its Agent's current plugins, and naming a plugin
// there must not become the cause of whatever ends the run later.
func TestClaimRecordsNoPluginCauseForARunUnderWay(t *testing.T) {
	f := newPinFixture(t, []string{"code-review"})

	if got := f.claim(t); got.PluginError == "" {
		t.Fatal("want a refusal")
	}
	if got := f.runs.Runs[0].FailureCause; got != nil {
		t.Errorf("a RUNNING run recorded cause %+v", got)
	}
}

// An agent that names nothing loads nothing, and needs no activation.
func TestClaimWithNoNamedPluginsResolvesNothing(t *testing.T) {
	f := newPinFixture(t, nil)

	got := f.claim(t)
	if got.PluginError != "" || len(got.Plugins) != 0 {
		t.Errorf("got %+v / %q, want no plugins and no refusal", got.Plugins, got.PluginError)
	}
}

// A run with no agent takes the same path as one whose agent names nothing.
func TestClaimWithNoAgentResolvesNothing(t *testing.T) {
	f := newPinFixture(t, []string{"code-review"})
	f.runs.TaskList[0].AgentID = nil

	got := f.claim(t)
	if got.PluginError != "" || len(got.Plugins) != 0 {
		t.Errorf("got %+v / %q, want no plugins and no refusal", got.Plugins, got.PluginError)
	}
}

// An agent whose space is not the run's space is treated as no agent, plugins
// included — the rule the route already applies to its instructions.
func TestClaimIgnoresAnAgentFromAnotherSpace(t *testing.T) {
	f := newPinFixture(t, []string{"code-review"})
	f.agents.Agents[0].SpaceID = "tm_other"

	got := f.claim(t)
	if got.PluginError != "" || len(got.Plugins) != 0 {
		t.Errorf("got %+v / %q, want the agentless path", got.Plugins, got.PluginError)
	}
}

func TestDownloadServesOnlyThePinnedRelease(t *testing.T) {
	f := newPinFixture(t, []string{"code-review"})
	ctx := context.Background()
	if _, err := f.catalog.CreatePlugin(ctx, coreplugin.CreateInput{Name: "code-review", CreatedBy: "u_1"}); err != nil {
		t.Fatalf("CreatePlugin: %v", err)
	}
	if _, err := f.catalog.CreatePluginRelease(ctx, coreplugin.CreateReleaseInput{
		PluginName: "code-review", Version: "1.0.0", Digest: "sha256:abc",
		ObjectKey: "bm/code-review/1.0.0", PublishedBy: "u_1",
	}); err != nil {
		t.Fatalf("CreatePluginRelease: %v", err)
	}
	if err := f.packages.Put(ctx, "bm/code-review/1.0.0", strings.NewReader("PACKAGE")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	f.activate(t, "code-review", "1.0.0", "sha256:abc", true)
	f.claim(t) // records the pins that authorize the download

	rec := f.download(t, "code-review", "1.0.0")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "PACKAGE" {
		t.Errorf("body = %q", rec.Body.String())
	}
	if rec.Header().Get(pluginwire.DigestHeader) != "sha256:abc" {
		t.Errorf("digest header = %q; the worker verifies against it", rec.Header().Get(pluginwire.DigestHeader))
	}

	// A release this run was never pinned to is not this run's to fetch.
	if rec := f.download(t, "code-review", "9.9.9"); rec.Code != http.StatusNotFound {
		t.Errorf("unpinned version status = %d, want 404", rec.Code)
	}
	if rec := f.download(t, "other", "1.0.0"); rec.Code != http.StatusNotFound {
		t.Errorf("unpinned plugin status = %d, want 404", rec.Code)
	}
}

// A run token is not a user: it must not reach the catalog routes.
func TestTheWorkerRoutesDoNotServeTheCatalog(t *testing.T) {
	f := newPinFixture(t, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/worker/task-runs/"+pluginTestRun+"/plugins", nil)
	req.Header.Set("Authorization", "Bearer "+runTokenFor(t, pluginTestRun, "tk_1"))
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusOK {
		t.Errorf("a listing route exists on the worker API (status %d)", rec.Code)
	}
}

func (f *pinFixture) download(t *testing.T, name, version string) *httptest.ResponseRecorder {
	t.Helper()
	path := "/api/worker/task-runs/" + pluginTestRun + "/plugins/" + name + "/" + version + "/download"
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+runTokenFor(t, pluginTestRun, "tk_1"))
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	return rec
}
