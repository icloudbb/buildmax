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

// signIn switches the app to managed mode against a deployment offering the
// named models (the first is its default), the way a sign-in does: the next
// runtime is built from the deployment's catalog.
func signIn(app *App, models ...string) {
	entries := make([]config.ModelEntry, len(models))
	for i, m := range models {
		entries[i] = config.ModelEntry{Name: m, Model: m}
	}
	app.modelSource = func(context.Context) (auth.ModelSource, error) {
		return auth.ModelSource{ServerURL: "https://deploy.example.test", Entries: entries, Default: models[0]}, nil
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
// was saved, and the mode it was saved in — through a later default change and
// through a sign-in — and after sign-in it says, before firing, that it does not
// run here, then fails with that reason instead of running the deployment's.
func TestScheduleKeepsItsModelAcrossSignIn(t *testing.T) {
	app, server := scheduleApp(t)
	ctx := context.Background()

	created, err := app.CreateScheduledTask(t.TempDir(), "nightly", "summarize", "0 9 * * *", "UTC", "")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if created.Model != "Luna" || created.Destination != session.DestinationLocal {
		t.Fatalf("Default saved as %q on %q, want it pinned to the default of the moment, Luna, locally", created.Model, created.Destination)
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

	signIn(app, "BuildMax smoke")
	assertRefusedHere(t, app, server, created.ID, 2)

	// Saving "Default" while signed in pins the deployment's default and mode.
	edited, err := app.UpdateScheduledTask(created.ID, created.WorkingDir, "nightly", "summarize", "0 9 * * *", "UTC", "")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if edited.Model != "BuildMax smoke" || edited.Destination != "deploy.example.test" {
		t.Fatalf("Default re-saved signed in as %q on %q, want BuildMax smoke on deploy.example.test", edited.Model, edited.Destination)
	}
	if got := listedTask(t, app, created.ID); got.ModelProblem != "" {
		t.Fatalf("re-pinned card problem = %q, want none", got.ModelProblem)
	}
}

// A deployment offering a model of the same name is still another provider:
// a task saved locally on Luna does not run on the deployment's Luna.
func TestScheduleDoesNotRunASameNamedModelInAnotherMode(t *testing.T) {
	app, server := scheduleApp(t)
	created, err := app.CreateScheduledTask(t.TempDir(), "nightly", "summarize", "0 9 * * *", "UTC", "Luna")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	signIn(app, "Luna")
	assertRefusedHere(t, app, server, created.ID, 1)

	// Saving it signed in is the choice to run it there.
	edited, err := app.UpdateScheduledTask(created.ID, created.WorkingDir, "nightly", "summarize", "0 9 * * *", "UTC", "Luna")
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	if edited.Destination != "deploy.example.test" {
		t.Fatalf("re-saved destination = %q, want deploy.example.test", edited.Destination)
	}
	if got := listedTask(t, app, created.ID); got.ModelProblem != "" {
		t.Fatalf("re-pinned card problem = %q, want none", got.ModelProblem)
	}
}

// assertRefusedHere checks that a task saved locally on Luna, after sign-in,
// says on its card that it does not run in this mode, and that its fire fails
// with that reason without calling any model or changing the task.
func assertRefusedHere(t *testing.T, app *App, server *mockllm.Server, id string, runs int) {
	t.Helper()
	card := listedTask(t, app, id)
	if card.Model != "Luna" || card.Destination != session.DestinationLocal ||
		card.ModelProblem != modelProblemOtherMode || card.ModelServer != "deploy.example.test" {
		t.Fatalf("card after sign-in = %+v, want Luna saved locally, refused while signed in to deploy.example.test", card)
	}
	calls := len(server.Requests())
	fireErr := app.fireScheduled(context.Background(), storedTask(t, app, id), scheduleRunKey(id))
	if fireErr == nil || !strings.Contains(fireErr.Error(), `"Luna"`) || !strings.Contains(fireErr.Error(), "deploy.example.test") {
		t.Fatalf("signed-in fire error = %v, want it to say Luna was saved for another mode", fireErr)
	}
	refused := finishedRun(t, app, id, runs)
	if refused.Status != "failed" || refused.Model != "Luna" || refused.SessionID != "" || refused.Error != fireErr.Error() {
		t.Fatalf("refused run = %+v, want a failed Luna row with the reason and no session", refused)
	}
	if got := len(server.Requests()); got != calls {
		t.Fatalf("a refused fire called a model: %d calls, want %d", got, calls)
	}
	if rec := storedTask(t, app, id); rec.Model != "Luna" || rec.Destination != session.DestinationLocal {
		t.Fatalf("a refused fire changed the task to %q on %q", rec.Model, rec.Destination)
	}
}

// A task stored before schedules pinned their model and mode is never run on
// whichever model happens to be the default: the tick loop pauses it without
// recording a run, its card says why, and it cannot resume until it is saved.
func TestScheduleSavedBeforePinningIsPausedUntilSaved(t *testing.T) {
	app, server := scheduleApp(t)
	app.ctx = context.Background()
	now := time.Now().UTC()
	for _, r := range []schedstore.Record{
		{ID: "nomodel"},
		{ID: "nomode", Model: "Luna"},
	} {
		r.WorkingDir, r.Prompt, r.CronExpr, r.Timezone = t.TempDir(), "summarize", "0 9 * * *", "UTC"
		r.Enabled, r.NextFireAt, r.CreatedAt, r.UpdatedAt = true, now.Add(-time.Minute), now, now
		if err := app.ensureScheduleStore().Add(r); err != nil {
			t.Fatalf("add: %v", err)
		}
	}

	app.sweepSchedules()

	for _, id := range []string{"nomodel", "nomode"} {
		if got := listedTask(t, app, id); got.Enabled || got.ModelProblem != modelProblemUnpinned {
			t.Fatalf("%s after a sweep: enabled %v problem %q, want paused and unpinned", id, got.Enabled, got.ModelProblem)
		}
		if _, err := app.SetScheduledTaskEnabled(id, true); err == nil {
			t.Fatalf("%s resumed without being saved", id)
		}
	}
	if _, err := app.SetAllScheduledTasksEnabled(true); err != nil {
		t.Fatalf("enable all: %v", err)
	}
	for _, id := range []string{"nomodel", "nomode"} {
		if storedTask(t, app, id).Enabled {
			t.Fatalf("%s enabled by Enable all without being saved", id)
		}
	}
	if runs, err := app.ListScheduleRuns(); err != nil || len(runs) != 0 {
		t.Fatalf("runs = %v (%v), want none recorded for a task that never had a model", runs, err)
	}
	if n := len(server.Requests()); n != 0 {
		t.Fatalf("an unpinned task called a model %d times", n)
	}

	saved, err := app.UpdateScheduledTask("nomodel", storedTask(t, app, "nomodel").WorkingDir, "", "summarize", "0 9 * * *", "UTC", "")
	if err != nil {
		t.Fatalf("save: %v", err)
	}
	if saved.Model != "Luna" || saved.Destination != session.DestinationLocal {
		t.Fatalf("saved as %q on %q, want Luna locally", saved.Model, saved.Destination)
	}
	if resumed, err := app.SetScheduledTaskEnabled("nomodel", true); err != nil || !resumed.Enabled {
		t.Fatalf("resume after saving = (%v, %v), want enabled", resumed.Enabled, err)
	}
}

func TestCheckScheduleModel(t *testing.T) {
	const deploy = "https://deploy.example.test"
	local := fakeModels{offered: "Luna"}
	managed := fakeModels{offered: "Luna", server: deploy}
	other := fakeModels{offered: "Luna", server: "https://other.example.test"}
	cases := []struct {
		name        string
		model, dest string
		models      fakeModels
		want        string
	}{
		{"offered locally", "Luna", session.DestinationLocal, local, ""},
		{"offered by its deployment", "Luna", deploy, managed, ""},
		{"no model", "", session.DestinationLocal, local, modelProblemUnpinned},
		{"no mode", "Luna", "", local, modelProblemUnpinned},
		{"local task after sign-in", "Luna", session.DestinationLocal, managed, modelProblemOtherMode},
		{"deployment task after sign-out", "Luna", deploy, local, modelProblemOtherMode},
		{"another deployment", "Luna", deploy, other, modelProblemOtherMode},
		{"no longer configured locally", "Deep", session.DestinationLocal, local, modelProblemNotOffered},
		{"no longer offered by its deployment", "Deep", deploy, managed, modelProblemNotOffered},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := checkScheduleModel(schedstore.Record{Model: tc.model, Destination: tc.dest}, tc.models)
			if got != tc.want || (err == nil) != (tc.want == "") {
				t.Fatalf("checkScheduleModel(%q on %q) = (%q, %v), want %q", tc.model, tc.dest, got, err, tc.want)
			}
		})
	}
}

type fakeModels struct{ offered, server string }

func (f fakeModels) DefaultModelName() string     { return f.offered }
func (f fakeModels) OffersModel(name string) bool { return name != "" && name == f.offered }
func (f fakeModels) ManagedServerURL() string     { return f.server }
