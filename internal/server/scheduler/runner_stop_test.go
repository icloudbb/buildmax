//go:build !windows

package scheduler

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

// buildHelper compiles a stand-in worker. A real program rather than a shell
// script because a script's `sleep` outlives the shell that is killed, and the
// orphan keeps the test's pipes open.
func buildHelper(t *testing.T, name, body string) string {
	t.Helper()
	dir := t.TempDir()
	src := filepath.Join(dir, "main.go")
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, name)
	build := exec.Command("go", "build", "-o", bin, src)
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", name, err, out)
	}
	return bin
}

// writeSignalCatcher builds a worker that reports when its handler is installed
// and records the signal it received before exiting cleanly — the shape of a
// worker that reports its outcome before it goes.
func writeSignalCatcher(t *testing.T, ready, evidence string) string {
	t.Helper()
	return buildHelper(t, "catcher", `package main

import (
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGTERM)
	_ = os.WriteFile(`+"`"+ready+"`"+`, []byte("ready"), 0o644)
	<-ch
	_ = os.WriteFile(`+"`"+evidence+"`"+`, []byte("stopped politely"), 0o644)
}
`)
}

// writeStubbornWorker builds a worker that ignores the request to stop.
func writeStubbornWorker(t *testing.T, ready string) string {
	t.Helper()
	return buildHelper(t, "stubborn", `package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	signal.Ignore(syscall.SIGTERM)
	_ = os.WriteFile(`+"`"+ready+"`"+`, []byte("ready"), 0o644)
	// Sleep, not select{}: with no other goroutine or timer, select{} is a
	// deadlock the runtime kills at once, so the "stubborn" worker would exit
	// on its own instead of making the runner kill it.
	time.Sleep(time.Hour)
}
`)
}

// waitForFile blocks until path exists.
func waitForFile(t *testing.T, path string, limit time.Duration) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s never appeared", path)
}

// Cancelling a dispatch must ask the worker to stop rather than kill it: the
// kill is what would take the run's output, artifacts, and outcome with it.
func TestLocalRunnerSignalsTheWorkerInsteadOfKillingIt(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a helper binary")
	}
	dir := t.TempDir()
	ready := filepath.Join(dir, "ready.txt")
	evidence := filepath.Join(dir, "stopped.txt")
	runner := NewLocalRunner(writeSignalCatcher(t, ready, evidence), os.Environ(), "", 5*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, _, err := runner.Run(ctx, coretask.Run{ID: "r_local_signal_000000000"}, "")
		done <- err
	}()

	// Give the child time to start and install its handler before asking it to
	// stop. A worker that has not reached its handler yet dies on the default
	// disposition, which is the process starting up, not the mechanism failing.
	waitForFile(t, ready, 10*time.Second)
	cancel()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the worker never exited after its dispatch was cancelled")
	}

	body, err := os.ReadFile(evidence)
	if err != nil {
		t.Fatalf("the worker was killed rather than asked to stop: %v", err)
	}
	if string(body) != "stopped politely" {
		t.Errorf("evidence = %q", body)
	}
}

// A worker that ignores the request is killed when its window expires, so a
// stuck run cannot outlive the process that spawned it.
func TestLocalRunnerKillsAWorkerThatWillNotStop(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns a sleeping process")
	}
	ready := filepath.Join(t.TempDir(), "ready.txt")
	runner := NewLocalRunner(writeStubbornWorker(t, ready), os.Environ(), "", 300*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_, _, _, _ = runner.Run(ctx, coretask.Run{ID: "r_local_stubborn_000000"}, "")
		close(done)
	}()

	waitForFile(t, ready, 10*time.Second)
	cancel()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("a worker that ignored the request was never killed")
	}
}
