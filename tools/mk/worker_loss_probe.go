package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// The worker-loss probe proves, against the running kind deployment, the one
// failure-recovery fact no handler or store test can reach: when a worker is
// lost mid-run, the run reaches a terminal, diagnosable state rather than
// staying RUNNING forever, and its record stays retrievable. It holds a run
// mid-execution, deletes its worker, and asserts the run settles to FAILED with
// a message naming the worker loss. This is end-to-end-testing.md §6.1's
// "Failure recovery" path, beside the sandbox, worker-API, and coordination
// probes.
//
// The kill is a Job deletion, which terminates the worker pod through the
// kubelet with SIGTERM. In this deployment that is the common worker-loss shape
// — a rollout, an eviction, a drained node — and the worker catches it and
// reports the run as interrupted. A worker that instead vanishes without
// reporting (a node crash, a kernel OOM-kill) is left to the server's liveness
// sweep; that path is covered at the store level by
// TestReaperClosesRunsWhoseWorkerWentSilent in stale_runs_test.go and cannot be
// reproduced from here, because the kernel will not deliver an in-container
// SIGKILL to the worker's PID 1 and any orderly deletion the kubelet performs
// starts with the SIGTERM the worker reports on. Either way the run lands in the
// same terminal, diagnosable state this probe asserts.

const workerLossProbeEmail = "worker-loss-probe@buildmax.local"

// workerLossStall holds the worker's run at its model call long enough to reach
// RUNNING, find the worker, and delete it mid-run before the run could finish.
const workerLossStall = 30 * time.Second

// workerLossSettleDeadline bounds the wait for the run to reach FAILED after the
// worker is deleted. A worker reports its interruption within a graceful-stop
// window; this is generous so a slow report is not read as a stranded run.
const workerLossSettleDeadline = 90 * time.Second

// kindWorkerLossProbe drives one run, deletes its worker, and asserts the run
// settles to a diagnosable terminal state.
func kindWorkerLossProbe() error {
	fmt.Println("Probing worker-loss recovery...")
	target := kindSmokeTarget()
	ctx := context.Background()
	client := &http.Client{Timeout: 30 * time.Second}

	token, spaceID, err := smokeSignIn(ctx, client, target, workerLossProbeEmail)
	if err != nil {
		return err
	}

	convID, err := coordCreateConversation(ctx, client, target.apiBase, spaceID, token)
	if err != nil {
		return err
	}
	taskID, err := coordCreateTask(ctx, client, target.apiBase, spaceID, convID, token, "Run until the probe kills the worker.")
	if err != nil {
		return err
	}

	// Stall after the task exists: task creation generates a title with its own
	// synchronous model call, and stalling before it would just hang the create.
	// The worker is dispatched afterward, so arming the stall now still catches
	// its run's model call and holds the run RUNNING long enough to delete the
	// worker mid-run.
	if err := armLLMStall(ctx, client, target, workerLossStall); err != nil {
		return err
	}
	defer func() { _ = armLLMStall(ctx, client, target, 0) }()

	taskURL := target.apiBase + "/api/spaces/" + url.PathEscape(spaceID) + "/tasks/" + url.PathEscape(taskID)
	if err := waitForTaskStatus(ctx, client, taskURL, token, "RUNNING", 90*time.Second); err != nil {
		return fmt.Errorf("task did not reach RUNNING before the kill: %w", err)
	}

	runID, err := lastRunID(ctx, client, taskURL, token)
	if err != nil {
		return err
	}

	job, err := workerJobForRun(ctx, runID)
	if err != nil {
		return err
	}
	fmt.Printf("  deleting worker job %s mid-run...\n", job)
	// Delete the Job, not just its pod: the Job restarts a failed pod
	// (RestartPolicy OnFailure with a backoff limit), so deleting the pod alone
	// would let a replacement keep the run alive. Deleting the Job removes the
	// worker and the controller that would replace it.
	if err := kindKubectl("delete", "job", job, "-n", "buildmax", "--wait=false"); err != nil {
		return fmt.Errorf("delete the worker job: %w", err)
	}

	// With its worker gone, the run must reach a terminal FAILED state naming the
	// loss rather than staying RUNNING.
	message, err := waitForTaskFailure(ctx, client, taskURL, token, workerLossSettleDeadline)
	if err != nil {
		return fmt.Errorf("the run was not settled after its worker was killed: %w", err)
	}
	if !strings.Contains(message, "worker") {
		return fmt.Errorf("the run failed but the reason does not name the worker; message = %q", message)
	}

	// The run must remain diagnosable: its provenance answers after the worker is
	// gone, carrying the terminal status rather than a stranded RUNNING.
	if err := assertRunProvenanceTerminal(ctx, client, target, spaceID, runID, token); err != nil {
		return err
	}

	fmt.Printf("Worker-loss recovery verified: a killed worker's run settled to FAILED (%q) and stayed retrievable.\n", message)
	return nil
}

// lastRunID reads a task's current run id.
func lastRunID(ctx context.Context, client *http.Client, taskURL, token string) (string, error) {
	var detail struct {
		LastRunID string `json:"last_run_id"`
	}
	if err := requestJSON(ctx, client, http.MethodGet, taskURL, token, nil, &detail, http.StatusOK); err != nil {
		return "", fmt.Errorf("read task detail: %w", err)
	}
	if detail.LastRunID == "" {
		return "", fmt.Errorf("task has no run to kill")
	}
	return detail.LastRunID, nil
}

var workerJobNonDNS = regexp.MustCompile(`[^a-z0-9-]+`)

// workerJobForRun finds the worker Job that runs runID. A worker Job is named
// buildmax-worker-<sanitized run id>-<timestamp>; the timestamp is not knowable
// here, so the run-id prefix is matched instead. It retries because the Job is
// created around the moment the run starts, which is when the caller looks.
func workerJobForRun(ctx context.Context, runID string) (string, error) {
	sanitized := workerJobNonDNS.ReplaceAllString(strings.ToLower(runID), "-")
	sanitized = strings.Trim(sanitized, "-")
	if len(sanitized) > 30 {
		sanitized = sanitized[:30]
	}
	needle := "buildmax-worker-" + sanitized
	deadline := time.Now().Add(30 * time.Second)
	for {
		out, err := captureKindKubectl("get", "jobs", "-n", "buildmax",
			"-l", "app.kubernetes.io/name=buildmax-worker",
			"-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\n\"}{end}")
		if err != nil {
			return "", fmt.Errorf("list worker jobs: %w\n%s", err, out)
		}
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			if name := strings.TrimSpace(line); strings.HasPrefix(name, needle) {
				return name, nil
			}
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("no worker job for run %s appeared (looked for a name starting %q)", runID, needle)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// waitForTaskFailure polls a task until it ends FAILED and returns its error
// message, or errors if it succeeds, is canceled, or never settles in time.
func waitForTaskFailure(ctx context.Context, client *http.Client, taskURL, token string, timeout time.Duration) (string, error) {
	var detail struct {
		Status       string  `json:"status"`
		ErrorMessage *string `json:"error_message"`
	}
	deadline := time.Now().Add(timeout)
	for {
		if err := requestJSON(ctx, client, http.MethodGet, taskURL, token, nil, &detail, http.StatusOK); err != nil {
			return "", err
		}
		switch detail.Status {
		case "FAILED":
			if detail.ErrorMessage == nil {
				return "", nil
			}
			return *detail.ErrorMessage, nil
		case "SUCCEEDED", "CANCELED":
			return "", fmt.Errorf("run ended %s, not FAILED — the kill did not strand it", detail.Status)
		}
		if time.Now().After(deadline) {
			return "", fmt.Errorf("run stayed %s past %s; it was never settled", detail.Status, timeout)
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

// assertRunProvenanceTerminal confirms the killed run's record is still
// retrievable and reports the terminal status.
func assertRunProvenanceTerminal(ctx context.Context, client *http.Client, target smokeTarget, spaceID, runID, token string) error {
	var prov struct {
		Status string `json:"status"`
	}
	provURL := target.apiBase + "/api/spaces/" + url.PathEscape(spaceID) + "/task-runs/" + url.PathEscape(runID)
	if err := requestJSON(ctx, client, http.MethodGet, provURL, token, nil, &prov, http.StatusOK); err != nil {
		return fmt.Errorf("the killed run's provenance was not retrievable: %w", err)
	}
	if prov.Status != "FAILED" {
		return fmt.Errorf("run provenance status = %q, want FAILED", prov.Status)
	}
	return nil
}
