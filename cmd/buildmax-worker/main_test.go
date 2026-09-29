package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/icloudbb/buildmax/internal/bootstrap"
	coretask "github.com/icloudbb/buildmax/internal/core/task"
)

// Exit status is what the scheduler reads, and under Kubernetes it decides
// whether the Job starts another pod. A run that already reported its own
// outcome must not be reported again as a failed dispatch, and neither must a
// run this worker never owned.
//
// Non-zero is reserved for the case a restart can fix: a worker that died
// before claiming its run, which leaves the run SCHEDULED for a fresh pod — and
// for a run whose outcome never reached the server, which must not look clean.
func TestWorkerExitCode(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{name: "a run that finished", err: nil, want: 0},
		{name: "a run that was canceled", err: coretask.ErrRunCanceled, want: 0},
		{name: "a run interrupted by shutdown", err: coretask.ErrRunInterrupted, want: 0},
		{name: "a run wrapped in context", err: errors.Join(errors.New("worker run"), coretask.ErrRunInterrupted), want: 0},
		{name: "a run another worker had claimed", err: bootstrap.ErrAlreadyClaimed, want: 0},
		// The status guard reaches this wrapped; a restarted pod takes that
		// path, not the transition's.
		{name: "a run no longer SCHEDULED", err: fmt.Errorf("%w (status=RUNNING)", bootstrap.ErrAlreadyClaimed), want: 0},
		// RunTask and the refusal paths mark a failure only after the server
		// holds the FAILED outcome; a pod restarted after it met exactly the
		// "run already claimed" refusal this exit code exists to avoid.
		{name: "a run that failed at its work and reported it", err: fmt.Errorf("%w: %w", coretask.ErrRunFailed, errors.New("the model refused")), want: 0},
		{name: "a run that failed and could not report it", err: errors.New("report FAILED: connection refused"), want: 1},
		{name: "a worker that failed before claiming its run", err: errors.New("get run: connection refused"), want: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := workerExitCode("rt_1", tc.err); got != tc.want {
				t.Errorf("workerExitCode(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}
