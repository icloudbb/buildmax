// Package main is the entry point for the BuildMax worker (runs a single task run via API + direct storage).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/icloudbb/buildmax/internal/bootstrap"
	"github.com/icloudbb/buildmax/internal/config"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
	log "github.com/icloudbb/buildmax/internal/infra/log"
)

func main() {
	sc, _ := config.LoadServerConfig()
	// Default worker to debug log level when not explicitly set, so ephemeral worker logs are detailed enough to triage.
	level := config.LogLevel(sc.LogLevel)
	if level == "info" && sc.LogLevel == "" {
		level = "debug"
	}
	log.Init(log.LogConfig{LogsDir: config.LogsDir(), Level: level, Filename: "buildmax-worker.log", AlsoStdout: true})

	taskRunID := flag.String("task-run-id", "", "task run ID to run (required)")
	flag.Parse()
	if *taskRunID == "" {
		slog.Error("worker: --task-run-id is required")
		fmt.Fprintf(os.Stderr, "error: --task-run-id is required\n")
		os.Exit(1)
	}
	// A worker is stopped from outside more often than it finishes early: a node
	// drains, a pod is evicted, an operator restarts the deployment. Catching
	// the signal is what lets the run report what it produced instead of going
	// silent and being declared abandoned hours later by the stale-run reaper.
	// See docs/design/graceful-shutdown.md §6.2.
	ctx, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	if err := bootstrap.RunWorker(ctx, *taskRunID); err != nil {
		if code := workerExitCode(*taskRunID, err); code != 0 {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(code)
		}
	}
}

// workerExitCode decides what a finished worker tells its scheduler, and logs
// what happened.
//
// Exit status is a dispatch-level signal here, not a summary of the run: a run
// that reported its own outcome exits zero however it ended. Under Kubernetes
// `RestartPolicy: OnFailure` a non-zero exit starts a new pod, whose worker
// immediately refuses the run for no longer being SCHEDULED — so reporting a
// stopped run as a failed dispatch spends the Job's backoff on nothing.
//
// The Job's backoff is worth keeping for what it is actually for. A worker can
// die before it claims its run — while reading its configuration, fetching the
// run, or resolving its model — and the run is still SCHEDULED, so a fresh pod
// picks it up and succeeds. Every case below that exits zero is one where a
// restart cannot help.
func workerExitCode(taskRunID string, err error) int {
	switch {
	case err == nil:
		return 0
	case errors.Is(err, bootstrap.ErrAlreadyClaimed):
		// Not this worker's run: somebody else is executing it, or already did.
		// Zero, because a restart would refuse it again for the same reason and
		// nothing reads a distinct code — and because under the local runner a
		// non-zero exit makes the scheduler fail a run another worker is in the
		// middle of.
		slog.Info("run already claimed; this worker has nothing to do", "task_run_id", taskRunID)
		return 0
	case errors.Is(err, coretask.ErrRunCanceled):
		// The run did what it was told and has already reported CANCELED.
		slog.Info("worker run canceled", "task_run_id", taskRunID)
		return 0
	case errors.Is(err, coretask.ErrRunInterrupted):
		// The process was asked to stop; the run has already reported what it
		// produced and why it stopped.
		slog.Info("worker run interrupted by shutdown", "task_run_id", taskRunID)
		return 0
	case errors.Is(err, coretask.ErrRunFailed):
		// The run failed at its work and has already reported FAILED with the
		// cause; a restart would only find it terminal and refuse it.
		slog.Error("worker run failed and reported it", "task_run_id", taskRunID, "err", err)
		return 0
	default:
		// Failed before the claim, or could not report its outcome.
		slog.Error("worker run failed", "task_run_id", taskRunID, "err", err)
		return 1
	}
}
