package tool

import (
	"context"
	"github.com/icloudbb/buildmax/internal/util"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/icloudbb/buildmax/internal/agentapp/job"
	"github.com/icloudbb/buildmax/internal/core/agent"
	"github.com/icloudbb/buildmax/internal/core/session"
)

func sleepCommandForTest() string {
	if runtime.GOOS == "windows" {
		return "ping -n 31 127.0.0.1 > NUL"
	}
	return "sleep 30"
}

func newJobManager(t *testing.T) *job.Manager {
	t.Helper()
	m := job.NewManager()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = m.Close(ctx)
	})
	return m
}

// parseJobID extracts the "jb_..." job ID from Bash's background-start output.
// Bash reports a failed start as a soft "Cannot start background job: ..."
// message with a nil error, so a missing ID means the start actually failed
// (seen intermittently on Windows CI). Fail with the output rather than slicing
// out[-1:], which panics and hides the real message.
func parseJobID(t *testing.T, out string) string {
	t.Helper()
	idx := strings.Index(out, "jb_")
	if idx < 0 {
		t.Fatalf("no job ID in background output (start may have failed): %q", out)
	}
	id := out[idx:]
	if end := strings.IndexAny(id, " \n"); end > 0 {
		id = id[:end]
	}
	return id
}

// startBackground runs command through Bash's background path and returns the
// job ID parsed from the tool output.
func startBackground(t *testing.T, b *Bash, m *job.Manager, command string) string {
	t.Helper()
	ctx := session.CtxWithSessionID(context.Background(), "sess-1")
	out, err := b.Execute(ctx, map[string]any{"command": command, "run_in_background": true})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	id := parseJobID(t, out)
	if _, ok := m.Get(id); !ok {
		t.Fatalf("job %q not in manager", id)
	}
	return id
}

func waitJobDone(t *testing.T, m *job.Manager, id string) job.Job {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if j, ok := m.Get(id); ok && !j.Running() {
			return j
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %s never finished", id)
	return job.Job{}
}

func TestBashRunInBackground(t *testing.T) {
	m := newJobManager(t)
	b := NewBash(util.FixedRoot(t.TempDir())).WithJobs(m)

	id := startBackground(t, b, m, "echo detached")
	j := waitJobDone(t, m, id)
	if j.State != job.StateSucceeded {
		t.Fatalf("job = %+v", j)
	}
	if j.Provenance.SessionID != "sess-1" {
		t.Fatalf("provenance = %+v", j.Provenance)
	}

	// A launch from inside a run records which run and tool call detached it.
	ctx := session.CtxWithSessionID(context.Background(), "sess-1")
	ctx = agent.CtxWithRunID(ctx, "rt_run")
	ctx = agent.CtxWithToolCall(ctx, "call_bg")
	bgOut, bgErr := b.Execute(ctx, map[string]any{"command": "echo linked", "run_in_background": true})
	if bgErr != nil {
		t.Fatal(bgErr)
	}
	linkedID := parseJobID(t, bgOut)
	linked, _ := m.Get(linkedID)
	if linked.Provenance.ParentTraceID != "rt_run" || linked.Provenance.ParentToolCallID != "call_bg" {
		t.Fatalf("provenance = %+v", linked.Provenance)
	}

	out, err := NewJobOutput(m).Execute(context.Background(), map[string]any{"job_id": id})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "detached") || !strings.Contains(out, "succeeded") || !strings.Contains(out, "next_cursor:") {
		t.Fatalf("JobOutput = %q", out)
	}

	list, err := NewJobList(m).Execute(context.Background(), map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(list, id) {
		t.Fatalf("JobList = %q", list)
	}
}

func TestBashBackgroundRefusedInSubagent(t *testing.T) {
	m := newJobManager(t)
	b := NewBash(util.FixedRoot(t.TempDir())).WithJobs(m)
	ctx := agent.CtxMarkSubagent(session.CtxWithSessionID(context.Background(), "sub-sess"))
	out, err := b.Execute(ctx, map[string]any{"command": "echo hi", "run_in_background": true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "not available inside a subagent") {
		t.Fatalf("output = %q", out)
	}
	if jobs := m.List(); len(jobs) != 0 {
		t.Fatalf("job started despite refusal: %+v", jobs)
	}
}

func TestBashBackgroundUnavailableWithoutManager(t *testing.T) {
	b := NewBash(util.FixedRoot(t.TempDir()))
	out, err := b.Execute(context.Background(), map[string]any{"command": "echo hi", "run_in_background": true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "not available on this surface") {
		t.Fatalf("output = %q", out)
	}
	// The parameter is only advertised when jobs are available.
	schema := b.Parameters().(map[string]any)["properties"].(map[string]any)
	if _, ok := schema["run_in_background"]; ok {
		t.Fatal("run_in_background advertised without a job manager")
	}
	withJobs := NewBash(util.FixedRoot(t.TempDir())).WithJobs(job.NewManager())
	schema = withJobs.Parameters().(map[string]any)["properties"].(map[string]any)
	if _, ok := schema["run_in_background"]; !ok {
		t.Fatal("run_in_background missing with a job manager")
	}
}

func TestJobStopTool(t *testing.T) {
	m := newJobManager(t)
	b := NewBash(util.FixedRoot(t.TempDir())).WithJobs(m)
	id := startBackground(t, b, m, sleepCommandForTest())

	out, err := NewJobStop(m).Execute(context.Background(), map[string]any{"job_id": id})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Stop requested") {
		t.Fatalf("JobStop = %q", out)
	}
	j := waitJobDone(t, m, id)
	if j.State != job.StateCanceled || j.StopReason != job.StopUser {
		t.Fatalf("job = %+v", j)
	}
	// Stopping again reports the terminal state instead of erroring.
	out, err = NewJobStop(m).Execute(context.Background(), map[string]any{"job_id": id})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "already finished") {
		t.Fatalf("JobStop = %q", out)
	}
}

func TestJobToolsUnknownID(t *testing.T) {
	m := newJobManager(t)
	out, err := NewJobOutput(m).Execute(context.Background(), map[string]any{"job_id": "jb_nope"})
	if err != nil || !strings.Contains(out, "No such job") {
		t.Fatalf("JobOutput = %q, %v", out, err)
	}
	out, err = NewJobStop(m).Execute(context.Background(), map[string]any{"job_id": "jb_nope"})
	if err != nil || !strings.Contains(out, "No such job") {
		t.Fatalf("JobStop = %q, %v", out, err)
	}
	out, err = NewJobList(m).Execute(context.Background(), map[string]any{})
	if err != nil || out != "No background jobs." {
		t.Fatalf("JobList = %q, %v", out, err)
	}
}
