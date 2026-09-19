package desktop

import (
	"context"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/agentapp"
	"github.com/icloudbb/buildmax/internal/agentapp/job"
	"github.com/icloudbb/buildmax/internal/config"
)

// recordingEmitter captures frontend events for assertions.
type recordingEmitter struct {
	mu     sync.Mutex
	events []struct {
		name string
		data any
	}
}

func (r *recordingEmitter) emit(_ context.Context, name string, data any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, struct {
		name string
		data any
	}{name, data})
}

func (r *recordingEmitter) jobUpdates() []JobPayload {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []JobPayload
	for _, e := range r.events {
		if e.name == eventJobUpdate {
			if p, ok := e.data.(JobPayload); ok {
				out = append(out, p)
			}
		}
	}
	return out
}

func (r *recordingEmitter) adoptedSessionIDs() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, e := range r.events {
		if e.name == eventSessionAdopted {
			if p, ok := e.data.(*SessionAdoptedPayload); ok {
				out = append(out, p.SessionID)
			}
		}
	}
	return out
}

func shellJobSpecForTest(command string) job.CommandSpec {
	if runtime.GOOS == "windows" {
		return job.CommandSpec{Command: command, Name: "cmd", Args: []string{"/c", command}}
	}
	return job.CommandSpec{Command: command, Name: "sh", Args: []string{"-c", command}}
}

// newJobsTestApp builds an App with one saved project and a recording emitter.
func newJobsTestApp(t *testing.T) (*App, *recordingEmitter, string) {
	t.Helper()
	t.Setenv("BUILDMAX_HOME", t.TempDir())
	rec := &recordingEmitter{}
	app := NewApp()
	app.emit = rec.emit
	app.Startup(context.Background())
	t.Cleanup(func() { app.Shutdown(context.Background()) })

	project, err := app.OpenProject(t.TempDir(), "jobs")
	if err != nil {
		t.Fatalf("open project: %v", err)
	}
	return app, rec, project.ID
}

func TestDesktopJobBridge(t *testing.T) {
	app, rec, projectID := newJobsTestApp(t)

	ag, err := app.agentAppForProject(projectID)
	if err != nil {
		t.Fatal(err)
	}
	if ag.Jobs() == nil {
		t.Fatal("desktop AgentApp has no job manager")
	}
	j, err := ag.Jobs().StartCommand(shellJobSpecForTest("echo desktop"), job.Provenance{SessionID: "s1"})
	if err != nil {
		t.Fatal(err)
	}

	// The lifecycle pump forwards the terminal transition to the frontend.
	deadline := time.Now().Add(15 * time.Second)
	for {
		updates := rec.jobUpdates()
		done := false
		for _, u := range updates {
			if u.ID == j.ID && !u.Running && u.State == string(job.StateSucceeded) && u.ProjectID == projectID {
				done = true
			}
		}
		if done {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no terminal job-update event; got %+v", rec.jobUpdates())
		}
		time.Sleep(10 * time.Millisecond)
	}

	list, err := app.ListJobs(projectID)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].ID != j.ID {
		t.Fatalf("ListJobs = %+v", list)
	}

	out, err := app.GetJobOutput(projectID, j.ID, "stdout", 0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Data, "desktop") || out.Running {
		t.Fatalf("GetJobOutput = %+v", out)
	}
}

func (r *recordingEmitter) eventNames() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	names := make([]string, len(r.events))
	for i, e := range r.events {
		names[i] = e.name
	}
	return names
}

// writeSessionFile puts a minimal owning session bundle on disk, as the
// launching turn would have.
func writeSessionFile(t *testing.T, id string) {
	t.Helper()
	sess, err := agentapp.NewSessionManager(config.SessionsDir()).CreateWithID(id, "test-model")
	if err != nil {
		t.Fatal(err)
	}
	// Closed straight away: the bundle is what this fixture is for, and holding
	// its writer lock would block the code under test from opening it.
	if err := sess.Close(); err != nil {
		t.Fatal(err)
	}
}

// A finished deliverable job is parked for its owning session — not pushed —
// and the frontend is nudged to pull it when that session is on screen.
func TestDesktopParksRequestedDeliveries(t *testing.T) {
	app, rec, projectID := newJobsTestApp(t)
	writeSessionFile(t, "sess-a")
	ag, err := app.agentAppForProject(projectID)
	if err != nil {
		t.Fatal(err)
	}
	spec := shellJobSpecForTest("echo result")
	spec.Deliver = true
	if _, err := ag.Jobs().StartCommand(spec, job.Provenance{SessionID: "sess-a"}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(15 * time.Second)
	for app.PendingJobDeliveries(projectID, "sess-a") == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("delivery never parked; events: %v", rec.eventNames())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if n := app.PendingJobDeliveries(projectID, "other-session"); n != 0 {
		t.Fatalf("other session sees %d deliveries", n)
	}
	found := false
	for _, name := range rec.eventNames() {
		if name == eventJobDeliveryPending {
			found = true
		}
	}
	if !found {
		t.Fatal("no job-delivery-pending nudge emitted")
	}

	// While that session's own run is in flight the parked event stays parked.
	// Runs are keyed per session now, so it is sess-a's slot that must be busy.
	_, release := occupySession(t, app, projectID, "sess-a")
	started, err := app.DeliverNextJobEvent(projectID, "sess-a")
	if err != nil || started {
		t.Fatalf("busy delivery = %v, %v; want false, nil", started, err)
	}
	if app.PendingJobDeliveries(projectID, "sess-a") != 1 {
		t.Fatal("busy delivery consumed the parked event")
	}
	release()
	waitSessionNotBusy(t, app, projectID, "sess-a")

	// Idle delivery starts a turn. This test app has no model configured, so
	// the turn fails — through the normal stream-error path — but the parked
	// event was consumed and the run slot released.
	started, err = app.DeliverNextJobEvent(projectID, "sess-a")
	if err != nil {
		t.Fatal(err)
	}
	if !started {
		t.Fatal("idle delivery did not start")
	}
	if app.PendingJobDeliveries(projectID, "sess-a") != 0 {
		t.Fatal("delivery not consumed")
	}
	waitSessionNotBusy(t, app, projectID, "sess-a")
	sawDelivery := false
	for _, name := range rec.eventNames() {
		if name == eventJobDelivery {
			sawDelivery = true
		}
	}
	if !sawDelivery {
		t.Fatal("no job-delivery event emitted")
	}
	// Nothing left: the next pull is a clean no-op.
	started, err = app.DeliverNextJobEvent(projectID, "sess-a")
	if err != nil || started {
		t.Fatalf("empty delivery = %v, %v; want false, nil", started, err)
	}
}

func TestDesktopStopJob(t *testing.T) {
	app, _, projectID := newJobsTestApp(t)
	ag, err := app.agentAppForProject(projectID)
	if err != nil {
		t.Fatal(err)
	}
	command := "sleep 30"
	if runtime.GOOS == "windows" {
		command = "ping -n 31 127.0.0.1 > NUL"
	}
	j, err := ag.Jobs().StartCommand(shellJobSpecForTest(command), job.Provenance{})
	if err != nil {
		t.Fatal(err)
	}
	if err := app.StopJob(projectID, j.ID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for {
		if snap, ok := ag.Jobs().Get(j.ID); ok && !snap.Running() {
			if snap.State != job.StateCanceled {
				t.Fatalf("state = %s", snap.State)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("job never stopped")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
