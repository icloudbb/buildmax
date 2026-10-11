package desktop

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/config"
	"github.com/icloudbb/buildmax/internal/core/session"
	schedstore "github.com/icloudbb/buildmax/internal/infra/localschedulestore"
	"github.com/icloudbb/buildmax/internal/interface/auth"
	"github.com/icloudbb/buildmax/internal/testsupport/mockllm"
)

// scheduleApp starts an app in local mode on a temporary home whose
// settings.yaml offers two scripted models, Luna (the default) and Other.
func scheduleApp(t *testing.T) (*App, *mockllm.Server) {
	t.Helper()
	server, err := mockllm.Start(mockllm.Scenario{Steps: []mockllm.Step{{Text: "done"}}, Repeat: true})
	if err != nil {
		t.Fatalf("start mockllm: %v", err)
	}
	t.Cleanup(server.Close)
	url := server.BaseURL(mockllm.ProtocolOpenAIChat)
	var settings strings.Builder
	settings.WriteString("log_level: error\ndefault_model: Luna\nmodels:\n")
	for _, m := range []struct{ name, model string }{{"Luna", "mock-luna"}, {"Other", "mock-other"}} {
		fmt.Fprintf(&settings, "  - model: %s\n    name: %s\n    api_url: %q\n    api_key: mock-key\n    context_window: 128000\n", m.model, m.name, url)
	}
	home := t.TempDir()
	if err := os.WriteFile(filepath.Join(home, "settings.yaml"), []byte(settings.String()), 0o600); err != nil {
		t.Fatalf("write settings.yaml: %v", err)
	}
	t.Setenv("BUILDMAX_HOME", home)
	t.Setenv("HOME", t.TempDir())

	app := NewApp()
	app.emit = (&uiEvents{}).emit
	// Local mode, without reading this machine's real login.
	app.modelSource = func(context.Context) (auth.ModelSource, error) { return auth.ModelSource{}, nil }
	t.Cleanup(func() { app.Shutdown(context.Background()) })
	return app, server
}

// signIn switches the app to managed mode against a deployment that offers only
// its own model, the way a sign-in does: the next runtime is built from the
// deployment's catalog.
func signIn(app *App) {
	app.modelSource = func(context.Context) (auth.ModelSource, error) {
		return auth.ModelSource{
			ServerURL: "https://deploy.example.test",
			Entries:   []config.ModelEntry{{Name: "BuildMax smoke", Model: "BuildMax smoke"}},
			Default:   "BuildMax smoke",
		}, nil
	}
	app.rebuildRuntimes()
}

func storedTask(t *testing.T, app *App, id string) schedstore.Record {
	t.Helper()
	rec, err := app.ensureScheduleStore().Get(id)
	if err != nil {
		t.Fatalf("get task %s: %v", id, err)
	}
	return rec
}

func listedTask(t *testing.T, app *App, id string) ScheduledTaskPayload {
	t.Helper()
	tasks, err := app.ListScheduledTasks()
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	for _, p := range tasks {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("task %s not listed", id)
	return ScheduledTaskPayload{}
}

// finishedRun waits for the task's newest run to leave "running".
func finishedRun(t *testing.T, app *App, id string, want int) ScheduleRunPayload {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		runs, err := app.ListScheduleRuns()
		if err != nil {
			t.Fatalf("list runs: %v", err)
		}
		var mine []ScheduleRunPayload
		for _, r := range runs {
			if r.ScheduleID == id {
				mine = append(mine, r)
			}
		}
		if len(mine) == want && mine[0].Status != "running" {
			return mine[0]
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("task %s did not record %d finished runs within 20s", id, want)
	return ScheduleRunPayload{}
}

func modelCalls(server *mockllm.Server, providerModel string) int {
	n := 0
	for _, r := range server.Requests() {
		if bytes.Contains(r.Body, []byte(fmt.Sprintf("%q", providerModel))) {
			n++
		}
	}
	return n
}

// D4: a task saved with "Default" keeps the model that was the default when it
// was saved — through a later default change and through a sign-in — and after
// sign-in it says, before firing, that the deployment does not offer that
// model, then fails with that reason instead of running the deployment's.
func TestScheduleKeepsItsModelAcrossSignIn(t *testing.T) {
	app, server := scheduleApp(t)
	ctx := context.Background()

	created, err := app.CreateScheduledTask(t.TempDir(), "nightly", "summarize", "0 9 * * *", "UTC", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Model != "Luna" {
		t.Fatalf("Default saved as %q, want it pinned to the default of the moment, Luna", created.Model)
	}
	if got := listedTask(t, app, created.ID); got.Model != "Luna" || got.ModelProblem != "" {
		t.Fatalf("card before sign-in: model %q problem %q, want Luna and none", got.Model, got.ModelProblem)
	}

	// The default moves; the task does not.
	host, err := app.scheduleModelHost()
	if err != nil {
		t.Fatalf("model host: %v", err)
	}
	host.SetDefaultModel("Other")

	if err := app.fireScheduled(ctx, storedTask(t, app, created.ID), scheduleRunKey(created.ID)); err != nil {
		t.Fatalf("local fire: %v", err)
	}
	run := finishedRun(t, app, created.ID, 1)
	if run.Status != "ok" || run.Model != "Luna" {
		t.Fatalf("local run: status %q model %q (%s), want ok on Luna", run.Status, run.Model, run.Error)
	}
	loaded, err := sessionManager().Load(run.SessionID, session.LoadMetaOnly)
	if err != nil {
		t.Fatalf("load run session: %v", err)
	}
	if loaded.Meta.SelectedModel != "Luna" {
		t.Fatalf("run session model = %q, want Luna so the run view agrees with the card", loaded.Meta.SelectedModel)
	}
	if modelCalls(server, "mock-luna") == 0 || modelCalls(server, "mock-other") != 0 {
		t.Fatalf("the local fire did not call Luna alone: %d Luna calls, %d Other calls",
			modelCalls(server, "mock-luna"), modelCalls(server, "mock-other"))
	}

	signIn(app)

	card := listedTask(t, app, created.ID)
	if card.Model != "Luna" || card.ModelProblem != modelProblemNotOffered || card.ModelServer != "deploy.example.test" {
		t.Fatalf("card after sign-in: model %q problem %q server %q, want Luna not offered by deploy.example.test",
			card.Model, card.ModelProblem, card.ModelServer)
	}

	calls := len(server.Requests())
	fireErr := app.fireScheduled(ctx, storedTask(t, app, created.ID), scheduleRunKey(created.ID))
	if fireErr == nil || !strings.Contains(fireErr.Error(), `"Luna"`) || !strings.Contains(fireErr.Error(), "deploy.example.test") {
		t.Fatalf("signed-in fire error = %v, want it to say Luna is not offered by the deployment", fireErr)
	}
	refused := finishedRun(t, app, created.ID, 2)
	if refused.Status != "failed" || refused.Model != "Luna" || refused.SessionID != "" || refused.Error != fireErr.Error() {
		t.Fatalf("refused run = %+v, want a failed Luna row with the reason and no session", refused)
	}
	if got := len(server.Requests()); got != calls {
		t.Fatalf("a refused fire called a model: %d calls, want %d", got, calls)
	}
	if storedTask(t, app, created.ID).Model != "Luna" {
		t.Fatal("a refused fire changed the task's model")
	}

	// Saving "Default" while signed in pins the deployment's default instead.
	edited, err := app.UpdateScheduledTask(created.ID, created.WorkingDir, "nightly", "summarize", "0 9 * * *", "UTC", "")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if edited.Model != "BuildMax smoke" {
		t.Fatalf("Default re-saved signed in as %q, want BuildMax smoke", edited.Model)
	}
	if got := listedTask(t, app, created.ID); got.ModelProblem != "" {
		t.Fatalf("re-pinned card problem = %q, want none", got.ModelProblem)
	}
}

// A task stored before schedules pinned their model has none. It is never run
// on whichever model happens to be the default: its card says so, its fire
// fails saying so, and pausing or resuming it does not choose one for it.
func TestScheduleWithoutAModelDoesNotRun(t *testing.T) {
	app, server := scheduleApp(t)
	now := time.Now().UTC()
	legacy := schedstore.Record{
		ID: "legacy", WorkingDir: t.TempDir(), Prompt: "summarize", CronExpr: "0 9 * * *",
		Timezone: "UTC", Enabled: true, NextFireAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := app.ensureScheduleStore().Add(legacy); err != nil {
		t.Fatalf("add: %v", err)
	}
	if got := listedTask(t, app, legacy.ID); got.ModelProblem != modelProblemMissing {
		t.Fatalf("card problem = %q, want %q", got.ModelProblem, modelProblemMissing)
	}

	err := app.fireScheduled(context.Background(), legacy, scheduleRunKey(legacy.ID))
	if err == nil || !strings.Contains(err.Error(), "no model") {
		t.Fatalf("fire error = %v, want it to say the schedule has no model", err)
	}
	if run := finishedRun(t, app, legacy.ID, 1); run.Status != "failed" || run.SessionID != "" {
		t.Fatalf("run = %+v, want a failed row with no session", run)
	}
	if n := len(server.Requests()); n != 0 {
		t.Fatalf("a task without a model called a model %d times", n)
	}

	for _, enabled := range []bool{false, true} {
		if _, err := app.SetScheduledTaskEnabled(legacy.ID, enabled); err != nil {
			t.Fatalf("set enabled %v: %v", enabled, err)
		}
		if rec := storedTask(t, app, legacy.ID); rec.Model != "" || rec.Enabled != enabled {
			t.Fatalf("after set enabled %v: model %q enabled %v, want no model chosen", enabled, rec.Model, rec.Enabled)
		}
	}
}

func TestCheckScheduleModel(t *testing.T) {
	local := fakeModels{offered: "Luna"}
	managed := fakeModels{offered: "Deep", server: "https://deploy.example.test"}
	cases := []struct {
		name   string
		model  string
		models fakeModels
		want   string
	}{
		{"offered locally", "Luna", local, ""},
		{"offered by the deployment", "Deep", managed, ""},
		{"no model", "", local, modelProblemMissing},
		{"local model after sign-in", "Luna", managed, modelProblemNotOffered},
		{"deployment model after sign-out", "Deep", local, modelProblemNotOffered},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := checkScheduleModel(tc.model, tc.models)
			if got != tc.want || (err == nil) != (tc.want == "") {
				t.Fatalf("checkScheduleModel(%q) = (%q, %v), want %q", tc.model, got, err, tc.want)
			}
		})
	}
}

type fakeModels struct{ offered, server string }

func (f fakeModels) DefaultModelName() string     { return f.offered }
func (f fakeModels) OffersModel(name string) bool { return name != "" && name == f.offered }
func (f fakeModels) ManagedServerURL() string     { return f.server }
