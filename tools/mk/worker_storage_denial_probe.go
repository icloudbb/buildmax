package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// The worker-storage-denial probe proves, against the running kind deployment,
// the beta-readiness case the server's /readyz cannot see: a worker whose own
// object-storage writes are refused. Workers write to the bucket with their own
// client — a Task's seed and result checkpoints and the run's state (trace,
// session bundle, logs) — while published artifacts go through the server, so a
// worker-only denial leaves the server healthy and must surface on the run
// instead. The probe asserts the run ends FAILED with a cause an operator can
// read, keeps the evidence it can safely keep (status, reply, the artifact the
// server stored), and records nothing that points at an object that is not
// there: no trace pointer and no artifact that fails to download.
//
// The denial is a Cilium L7 policy on the MinIO pod: worker pods may GET and
// HEAD, the Envoy proxy answers every other method with 403, and every other pod
// keeps full access. Denying writes rather than all traffic is what lets a
// Continue restore its base and reach the agent, so the refusal lands on the
// run's own writes at the end rather than on its first read. The policy is in
// place before any worker it governs starts, so no connection predates it.
//
// Two write points are exercised. A new Task's first run captures its seed
// before the agent starts and fails closed there. A Continue of a Task that
// already has a head restores it, publishes an artifact through the server,
// answers, and then cannot store its run state: that one used to be reported
// SUCCEEDED with a trace pointer storage did not hold.

const workerStorageProbeEmail = "worker-storage-probe@buildmax.local"

const workerStorageDenyPolicyName = "buildmax-drill-deny-worker-minio-write"

// workerStorageDenyPolicy lets worker pods read the bucket and nothing more,
// and leaves every other pod — the server in particular — unrestricted. A
// namespaced Cilium selector without a namespace label matches only its own
// namespace, hence the Exists on the namespace key.
const workerStorageDenyPolicy = `apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: ` + workerStorageDenyPolicyName + `
  namespace: storage
spec:
  endpointSelector:
    matchLabels:
      app: minio
  ingress:
  - fromEndpoints:
    - matchLabels:
        k8s:io.kubernetes.pod.namespace: buildmax
        app.kubernetes.io/name: buildmax-worker
    toPorts:
    - ports:
      - port: "9000"
        protocol: TCP
      rules:
        http:
        - method: GET
        - method: HEAD
  - fromEndpoints:
    - matchExpressions:
      - key: k8s:io.kubernetes.pod.namespace
        operator: Exists
      - key: app.kubernetes.io/name
        operator: NotIn
        values:
        - buildmax-worker
  - fromEntities:
    - host
    - remote-node
`

// workerStorageRunDeadline bounds each run the probe drives. A refused write is
// answered at once, so a run that takes longer is stranded, not slow.
const workerStorageRunDeadline = 2 * time.Minute

// kindWorkerStorageDenialProbe drives the worker write-denial drill.
func kindWorkerStorageDenialProbe() error {
	fmt.Println("Probing a worker's object-storage write denial...")
	target := kindSmokeTarget()
	ctx := context.Background()
	client := &http.Client{Timeout: 30 * time.Second}

	// Idempotent cleanup: a previous run interrupted mid-drill left its policy.
	_ = kindKubectl("delete", "ciliumnetworkpolicy", workerStorageDenyPolicyName, "-n", "storage", "--ignore-not-found")

	pods, err := serverPodNames()
	if err != nil {
		return err
	}
	if len(pods) == 0 {
		return fmt.Errorf("no running buildmax-server pod to read /readyz from")
	}
	stop, base, err := startPodForward(ctx, pods[0], "18097")
	if err != nil {
		return err
	}
	defer stop()
	readyzURL := base + "/readyz"
	if err := waitReadyzCheck(ctx, client, readyzURL, "object_storage", "ok", true, 30*time.Second); err != nil {
		return fmt.Errorf("the server was not ready before the drill: %w", err)
	}
	restarts, err := serverPodRestarts()
	if err != nil {
		return err
	}

	token, spaceID, err := smokeSignIn(ctx, client, target, workerStorageProbeEmail)
	if err != nil {
		return err
	}
	// The file the Continue run publishes; materialized into every workspace.
	if err := uploadSmokeFile(ctx, client, target.apiBase, spaceID, token); err != nil {
		return fmt.Errorf("upload the file the run will publish: %w", err)
	}
	convID, err := coordCreateConversation(ctx, client, target.apiBase, spaceID, token)
	if err != nil {
		return err
	}
	spaceBase := target.apiBase + "/api/spaces/" + url.PathEscape(spaceID)

	// A Task with a committed head, created while storage is healthy, so the
	// Continue under denial restores rather than seeds.
	headTaskID, err := coordCreateTask(ctx, client, target.apiBase, spaceID, convID, token, "Reply with exactly deployment smoke ok.")
	if err != nil {
		return err
	}
	headTaskURL := spaceBase + "/tasks/" + url.PathEscape(headTaskID)
	if _, err := waitForTaskSuccess(ctx, client, headTaskURL, token); err != nil {
		return fmt.Errorf("the baseline run before the denial: %w", err)
	}

	policyPath, cleanup, err := writeTempManifest("buildmax-worker-storage-denial-*.yaml", workerStorageDenyPolicy)
	if err != nil {
		return err
	}
	defer cleanup()
	fmt.Println("  denying worker writes to object storage...")
	if err := kindKubectl("apply", "-f", policyPath); err != nil {
		return fmt.Errorf("apply the worker write-deny policy: %w", err)
	}
	policyDeleted := false
	defer func() {
		if !policyDeleted {
			_ = kindKubectl("delete", "-f", policyPath, "--ignore-not-found")
		}
	}()

	// The observable marker that the fault is the one armed: a worker-labelled
	// pod's write is refused by the proxy while its read reaches MinIO, and an
	// unlabelled pod's write still reaches MinIO itself.
	if err := assertWorkerWriteDenied(); err != nil {
		return err
	}

	// The server's path stays healthy for the whole denial.
	watchCtx, stopWatch := context.WithCancel(ctx)
	defer stopWatch()
	readyzFault := watchReadyz(watchCtx, client, readyzURL)

	seedMessage, err := workerStorageSeedCase(ctx, client, target, spaceID, convID, token)
	if err != nil {
		return fmt.Errorf("seed write under denial: %w", err)
	}
	stateMessage, artifactID, err := workerStorageContinueCase(ctx, client, target, spaceID, headTaskID, token)
	if err != nil {
		return fmt.Errorf("run-state write under denial: %w", err)
	}

	stopWatch()
	if fault := readyzFault(); fault != nil {
		return fmt.Errorf("the server's /readyz degraded while only the worker was denied: %w", fault)
	}
	if err := assertServerPodsUnrestarted(restarts); err != nil {
		return fmt.Errorf("the worker denial disturbed the server pods: %w", err)
	}

	fmt.Println("  restoring worker writes...")
	if err := kindKubectl("delete", "-f", policyPath, "--ignore-not-found"); err != nil {
		return fmt.Errorf("remove the worker write-deny policy: %w", err)
	}
	policyDeleted = true

	if err := workerStorageRecoveryCase(ctx, client, target, spaceID, convID, token); err != nil {
		return fmt.Errorf("recovery after the denial: %w", err)
	}

	fmt.Printf("Worker storage denial verified: with worker writes refused and /readyz healthy, a first run failed at its seed (%q) and a Continue failed at its run state (%q) keeping its reply and a downloadable artifact %s and no trace pointer; a fresh run succeeded once writes were restored.\n",
		seedMessage, stateMessage, artifactID)
	return nil
}

// workerStorageSeedCase starts a new Task under the denial. Its first run must
// capture a seed before the agent starts, and must fail closed there.
func workerStorageSeedCase(ctx context.Context, client *http.Client, target smokeTarget, spaceID, convID, token string) (string, error) {
	taskID, err := coordCreateTask(ctx, client, target.apiBase, spaceID, convID, token, "Reply with exactly deployment smoke ok.")
	if err != nil {
		return "", err
	}
	taskURL := target.apiBase + "/api/spaces/" + url.PathEscape(spaceID) + "/tasks/" + url.PathEscape(taskID)
	message, err := waitForTaskFailure(ctx, client, taskURL, token, workerStorageRunDeadline)
	if err != nil {
		return "", err
	}
	if !strings.Contains(message, "checkpoint payload to object storage") || !strings.Contains(message, "403") {
		return "", fmt.Errorf("the run failed, but its cause does not name the refused checkpoint upload: %q", message)
	}
	runID, err := lastRunID(ctx, client, taskURL, token)
	if err != nil {
		return "", err
	}
	if err := assertRunProvenanceTerminal(ctx, client, target, spaceID, runID, token); err != nil {
		return "", err
	}
	return message, nil
}

// workerStorageContinueCase continues the Task that has a head. The run restores
// it, publishes an artifact through the server, answers, and cannot store its
// run state. It returns the failure message and the published artifact.
func workerStorageContinueCase(ctx context.Context, client *http.Client, target smokeTarget, spaceID, taskID, token string) (string, string, error) {
	spaceBase := target.apiBase + "/api/spaces/" + url.PathEscape(spaceID)
	taskURL := spaceBase + "/tasks/" + url.PathEscape(taskID)

	// The run's first model call publishes the materialized file; its second is
	// the scripted reply. Armed before the Continue exists so the worker cannot
	// reach its model first, and cleared whatever happens.
	armed := map[string]any{
		"name": "UploadArtifact", "args": map[string]any{"path": "deployment-smoke.txt", "title": "worker storage drill"},
		"times": 1,
	}
	if err := requestJSON(ctx, client, http.MethodPost, target.llmControlToolCallURL, "", armed, nil, http.StatusOK); err != nil {
		return "", "", fmt.Errorf("arm the artifact publish: %w", err)
	}
	defer func() {
		_ = requestJSON(ctx, client, http.MethodPost, target.llmControlToolCallURL, "", map[string]any{"clear": true}, nil, http.StatusOK)
	}()

	var run struct {
		ID string `json:"id"`
	}
	if err := requestJSON(ctx, client, http.MethodPost, taskURL+"/runs", token, map[string]string{"input": "Publish the file, then reply."}, &run, http.StatusCreated); err != nil {
		return "", "", fmt.Errorf("continue the task: %w", err)
	}
	if run.ID == "" {
		return "", "", fmt.Errorf("the Continue returned no run id")
	}

	message, err := waitForTaskFailure(ctx, client, taskURL, token, workerStorageRunDeadline)
	if err != nil {
		return "", "", err
	}
	if !strings.Contains(message, "object storage") || !strings.Contains(message, "403") {
		return "", "", fmt.Errorf("the run failed, but its cause does not name the refused run-state write: %q", message)
	}

	// Retained: the run record, its reply, and no trace pointer to a trace that
	// never reached storage.
	var runs struct {
		Runs []struct {
			ID        string  `json:"id"`
			Status    string  `json:"status"`
			Output    *string `json:"output"`
			TracePath *string `json:"trace_path"`
		} `json:"runs"`
	}
	if err := requestJSON(ctx, client, http.MethodGet, taskURL+"/runs", token, nil, &runs, http.StatusOK); err != nil {
		return "", "", fmt.Errorf("list the task's runs: %w", err)
	}
	found := false
	for _, r := range runs.Runs {
		if r.ID != run.ID {
			continue
		}
		found = true
		if r.Status != "FAILED" {
			return "", "", fmt.Errorf("run %s status = %q, want FAILED", r.ID, r.Status)
		}
		if strings.TrimSpace(stringValue(r.Output)) != smokeReply {
			return "", "", fmt.Errorf("run %s kept output %q, want the reply it produced (%q)", r.ID, stringValue(r.Output), smokeReply)
		}
		if r.TracePath != nil {
			return "", "", fmt.Errorf("run %s records trace %q, which never reached storage", r.ID, *r.TracePath)
		}
	}
	if !found {
		return "", "", fmt.Errorf("run %s is missing from its task's runs", run.ID)
	}
	traceURL := spaceBase + "/task-runs/" + url.PathEscape(run.ID) + "/trace"
	if _, err := requestText(ctx, client, http.MethodGet, traceURL, token, nil, http.StatusNotFound); err != nil {
		return "", "", fmt.Errorf("the run's trace should read as never recorded: %w", err)
	}
	if err := assertRunProvenanceTerminal(ctx, client, target, spaceID, run.ID, token); err != nil {
		return "", "", err
	}

	// Every artifact the run lists downloads with the bytes it published, and it
	// published one: a listed artifact with no object behind it is the defect
	// this drill exists to catch.
	var prov struct {
		Artifacts []struct {
			ID       string `json:"id"`
			Filename string `json:"filename"`
		} `json:"artifacts"`
	}
	if err := requestJSON(ctx, client, http.MethodGet, spaceBase+"/task-runs/"+url.PathEscape(run.ID), token, nil, &prov, http.StatusOK); err != nil {
		return "", "", fmt.Errorf("read the run's provenance: %w", err)
	}
	if len(prov.Artifacts) == 0 {
		return "", "", fmt.Errorf("run %s lists no artifact; the armed UploadArtifact call never reached its worker", run.ID)
	}
	for _, a := range prov.Artifacts {
		content, err := requestText(ctx, client, http.MethodGet, target.apiBase+"/api/artifacts/"+url.PathEscape(a.ID)+"/content", token, nil, http.StatusOK)
		if err != nil {
			return "", "", fmt.Errorf("artifact %s (%s) is listed but does not download: %w", a.ID, a.Filename, err)
		}
		if content != smokeReply {
			return "", "", fmt.Errorf("artifact %s downloaded %q, want %q", a.ID, content, smokeReply)
		}
	}
	return message, prov.Artifacts[0].ID, nil
}

// workerStorageRecoveryCase proves writes are back: a new Task seeds, answers,
// stores its run state, and its trace reads back.
func workerStorageRecoveryCase(ctx context.Context, client *http.Client, target smokeTarget, spaceID, convID, token string) error {
	spaceBase := target.apiBase + "/api/spaces/" + url.PathEscape(spaceID)
	// Retried as a whole: the proxy learns the policy removal asynchronously, and
	// a worker scheduled in that window is refused exactly as under the denial.
	return retryFor(ctx, 30*time.Second, func() error {
		taskID, err := coordCreateTask(ctx, client, target.apiBase, spaceID, convID, token, "Reply with exactly deployment smoke ok.")
		if err != nil {
			return err
		}
		taskURL := spaceBase + "/tasks/" + url.PathEscape(taskID)
		if _, err := waitForTaskSuccess(ctx, client, taskURL, token); err != nil {
			return err
		}
		runID, err := lastRunID(ctx, client, taskURL, token)
		if err != nil {
			return err
		}
		traceURL := spaceBase + "/task-runs/" + url.PathEscape(runID) + "/trace"
		return retryFor(ctx, 30*time.Second, func() error {
			_, err := requestText(ctx, client, http.MethodGet, traceURL, token, nil, http.StatusOK)
			return err
		})
	})
}

// assertWorkerWriteDenied checks the armed fault from both sides before any run
// depends on it.
func assertWorkerWriteDenied() error {
	worker, err := kindMinIOAccess("storage-deny-worker", workerProbeLabels)
	if err != nil {
		return err
	}
	if worker.write != "PROXY_DENIED" || !strings.HasPrefix(worker.read, "HTTP/1.1 200") {
		return fmt.Errorf("the deny policy is not in force for workers: write=%s read=%q\n%s", worker.write, worker.read, worker.raw)
	}
	other, err := kindMinIOAccess("storage-deny-other", "role=probe")
	if err != nil {
		return err
	}
	if other.write != "MINIO" {
		return fmt.Errorf("the deny policy reaches beyond workers: an unlabelled pod's write was %s\n%s", other.write, other.raw)
	}
	return nil
}

type minioAccess struct {
	// write is PROXY_DENIED when the policy's proxy answered, MINIO when MinIO
	// itself did (its answers carry x-amz-request-id), NONE otherwise.
	write string
	// read is the status line of a GET of MinIO's liveness route.
	read string
	// raw is the pod's whole output, for a failure to quote.
	raw string
}

// kindMinIOAccess runs a throwaway pod with labels and reports how MinIO's
// port answers it a write and a read. An anonymous write is refused by MinIO
// too, so what distinguishes the denial is who answered, not the status code.
// It waits kindPolicySettle first, for the reason kindTCPReachable does.
func kindMinIOAccess(name, labels string) (minioAccess, error) {
	const host = "minio.storage.svc.cluster.local"
	// stdin is held open after the request: nc stops reading the socket once its
	// input ends, which would drop a reply still on its way through the proxy.
	script := fmt.Sprintf(`sleep %d
w=$({ printf 'PUT /bmstore/buildmax-drill-probe HTTP/1.1\r\nHost: %[2]s:9000\r\nContent-Length: 0\r\nConnection: close\r\n\r\n'; sleep 3; } | nc -w 5 %[2]s 9000 2>&1)
r=$({ printf 'GET /minio/health/live HTTP/1.1\r\nHost: %[2]s:9000\r\nConnection: close\r\n\r\n'; sleep 3; } | nc -w 5 %[2]s 9000 2>&1)
if echo "$w" | grep -qi 'x-amz-request-id'; then c=MINIO; elif echo "$w" | grep -q ' 403 '; then c=PROXY_DENIED; else c=NONE; fi
echo "BM_WRITE=$c"
echo "BM_READ=$(echo "$r" | head -n 1 | tr -d '\r')"
echo "$w" | head -n 12 | sed 's/^/BM_RAW_WRITE: /'`, int(kindPolicySettle.Seconds()), host)
	out, err := captureCombined("kubectl", "--context", kindContext(),
		"run", name, "-n", "buildmax", "--rm", "-i", "--restart=Never", "--quiet",
		"--image=buildmax:local", "--image-pull-policy=Never", "--labels="+labels,
		"--command", "--", "sh", "-c", script)
	if err != nil {
		return minioAccess{}, fmt.Errorf("storage access probe %q: %w\n%s", name, err, out)
	}
	access := minioAccess{raw: out}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(line, "BM_WRITE="); ok {
			access.write = v
		}
		if v, ok := strings.CutPrefix(line, "BM_READ="); ok {
			access.read = v
		}
	}
	if access.write == "" {
		return minioAccess{}, fmt.Errorf("storage access probe %q was inconclusive:\n%s", name, out)
	}
	return access, nil
}

// watchReadyz samples /readyz every two seconds until ctx ends and returns a
// function reporting the first sample that was not ready with object storage
// ok, or nil.
func watchReadyz(ctx context.Context, client *http.Client, readyzURL string) func() error {
	var (
		mu    sync.Mutex
		fault error
		done  = make(chan struct{})
	)
	go func() {
		defer close(done)
		for {
			code, status, err := readyzCheckStatus(ctx, client, readyzURL, "object_storage")
			if ctx.Err() != nil {
				return
			}
			mu.Lock()
			if fault == nil {
				switch {
				case err != nil:
					fault = err
				case code != http.StatusOK || status != "ok":
					fault = fmt.Errorf("/readyz = %d with object_storage %q", code, status)
				}
			}
			mu.Unlock()
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
		}
	}()
	return func() error {
		<-done
		mu.Lock()
		defer mu.Unlock()
		return fault
	}
}
