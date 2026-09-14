package main

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// The object-storage-denial probe proves, against the running kind deployment,
// the storage half of the dependency-degradation contract the beta-readiness
// record names: when the server loses object storage at runtime it reports the
// failure through /readyz -- which System Status mirrors, reading the same
// probes -- and is taken out of the Service rather than restarted, and it
// recovers on its own once storage returns, its bucket intact. It runs in
// kindSmoke after the database-outage probe, and shares its shape: the
// object_storage readiness check probes real bucket reachability
// (persist.ListFiles against a sentinel prefix), so a lost bucket is a failed
// check, not a crash.
//
// Interrupting storage takes the same two steps the database probe needs, for
// the same reason. A deny-all NetworkPolicy on the MinIO pod blocks new
// connections, but the S3 client's established keep-alive connection is
// grandfathered through and reused, so the readiness probe never notices a block
// on new connections alone. MinIO has no session-kill, so the probe drops the
// connection by bouncing MinIO in place: a SIGTERM to its PID 1 exits the
// container and the kubelet restarts it in the same pod, so the emptyDir bucket
// survives (deployment/kind/minio.yaml), while the server's reconnection now
// hits the policy and the outage holds until it is removed. MinIO restarts
// quickly and has no readiness gate, so by the time the policy is removed it is
// serving again and recovery is a fast reconnection. Blocking access rather than
// deleting the pod is what lets recovery find the same bucket: a lost emptyDir
// would leave a fresh MinIO with no bucket, which the running server -- it
// creates the bucket at startup -- would not restore.
//
// As in the database probe, /readyz is read straight from a server pod: a
// not-ready pod leaves the Service, so the ingress would answer its own
// endpoint-less 503 rather than the app's 503 that names object_storage. The
// recovery sign-in goes through the ingress, for the whole-path claim.

const storageDenialProbeEmail = "storage-denial-probe@buildmax.local"

// storageDenyPolicy denies all ingress to the MinIO pod, cutting the server's
// path to object storage without deleting the pod or its bucket.
const storageDenyPolicy = `apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: buildmax-drill-deny-minio
  namespace: storage
spec:
  podSelector:
    matchLabels:
      app: minio
  policyTypes:
  - Ingress
  ingress: []
`

const (
	// storageDegradeDeadline bounds the wait for /readyz to report the outage.
	storageDegradeDeadline = 30 * time.Second
	// storageRecoverDeadline bounds recovery after access is restored.
	storageRecoverDeadline = 60 * time.Second
)

// kindStorageDenialProbe drives the object-storage degradation and recovery
// drill.
func kindStorageDenialProbe() error {
	fmt.Println("Probing object-storage degradation and recovery...")
	target := kindSmokeTarget()
	ctx := context.Background()
	client := &http.Client{Timeout: 10 * time.Second}

	pods, err := serverPodNames()
	if err != nil {
		return err
	}
	if len(pods) == 0 {
		return fmt.Errorf("no running buildmax-server pod to read /readyz from")
	}
	stop, base, err := startPodForward(ctx, pods[0], "18096")
	if err != nil {
		return err
	}
	defer stop()
	readyzURL := base + "/readyz"

	// Baseline: the pod is ready and names object storage healthy.
	if err := waitReadyzCheck(ctx, client, readyzURL, "object_storage", "ok", true, 30*time.Second); err != nil {
		return fmt.Errorf("the server was not ready before the outage: %w", err)
	}

	before, err := serverPodRestarts()
	if err != nil {
		return err
	}

	policyPath, cleanup, err := writeTempManifest("buildmax-storage-denial-*.yaml", storageDenyPolicy)
	if err != nil {
		return err
	}
	defer cleanup()

	fmt.Println("  denying the server's access to object storage...")
	if err := kindKubectl("apply", "-f", policyPath); err != nil {
		return fmt.Errorf("apply the deny policy: %w", err)
	}
	time.Sleep(2 * time.Second)
	policyDeleted := false
	defer func() {
		if !policyDeleted {
			_ = kindKubectl("delete", "-f", policyPath, "--ignore-not-found")
		}
	}()

	// Drop the established S3 connection by bouncing MinIO. The container exits
	// and the kubelet restarts it in place, so the bucket on the emptyDir
	// survives; the server's reconnection then hits the deny policy. The exec goes
	// through the API server, which the ingress policy does not gate; SIGTERM to
	// PID 1 is a graceful MinIO shutdown, and the exec can exit non-zero as its
	// own connection drops, so /readyz below is the real signal.
	fmt.Println("  bouncing MinIO to drop the established connection...")
	if out, err := captureKindKubectl("exec", "deploy/minio", "-n", "storage", "--", "sh", "-c", "kill 1"); err != nil {
		fmt.Printf("  (minio bounce reported %v; the /readyz check below is the real signal)\n%s", err, out)
	}

	// The failure must surface: /readyz answers 503 and names object storage
	// failed.
	if err := waitReadyzCheck(ctx, client, readyzURL, "object_storage", "failed", false, storageDegradeDeadline); err != nil {
		return fmt.Errorf("the server did not report the object-storage outage: %w", err)
	}

	// Degraded, not rebuilt: the same server pods with the same restart counts.
	if err := assertServerPodsUnrestarted(before); err != nil {
		return fmt.Errorf("the outage disturbed the server pods: %w", err)
	}

	fmt.Println("  restoring object-storage access...")
	if err := kindKubectl("delete", "-f", policyPath, "--ignore-not-found"); err != nil {
		return fmt.Errorf("remove the deny policy: %w", err)
	}
	policyDeleted = true

	// Wait for MinIO to be serving again before expecting the server to recover.
	// The bounce restarts the container, and the kubelet's restart backoff grows
	// with a pod's restart history: a fresh cluster restarts it at once, but one
	// that has run this drill several times can sit in a multi-minute backoff. That
	// backoff is a test artifact, not the recovery behavior under test, so it is
	// waited out here rather than charged against the readiness recovery deadline.
	// The generous ceiling covers repeated local runs; `kind reload` resets it.
	if err := waitForMinioUp(ctx, 6*time.Minute); err != nil {
		return fmt.Errorf("object storage did not come back after the bounce: %w", err)
	}

	// The server recovers against the same bucket: object storage reporting
	// healthy again means the restarted MinIO came back with its contents, since
	// the readiness probe lists the bucket rather than assuming it.
	if err := waitReadyzCheck(ctx, client, readyzURL, "object_storage", "ok", true, storageRecoverDeadline); err != nil {
		return fmt.Errorf("the server did not recover after access was restored: %w", err)
	}

	// Normal service returns through the whole path: the pod rejoins the Service
	// and a sign-in succeeds. Retried while the endpoint rejoins.
	if err := retryFor(ctx, storageRecoverDeadline, func() error {
		_, _, err := smokeSignIn(ctx, client, target, storageDenialProbeEmail)
		return err
	}); err != nil {
		return fmt.Errorf("normal service did not return after recovery: %w", err)
	}

	if err := assertServerPodsUnrestarted(before); err != nil {
		return fmt.Errorf("the server pods were restarted during the outage exercise: %w", err)
	}

	fmt.Println("Object-storage recovery verified: /readyz reported object storage failed while the pods stayed up, then recovered with the bucket intact once access was restored.")
	return nil
}

// waitForMinioUp waits until the MinIO pod's container reports ready again after
// the in-place bounce, so a kubelet restart backoff is waited out rather than
// mistaken for a server that will not recover.
func waitForMinioUp(ctx context.Context, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var last string
	for {
		out, err := captureKindKubectl("get", "pods", "-n", "storage", "-l", "app=minio",
			"-o", "jsonpath={.items[0].status.containerStatuses[0].ready}")
		last = strings.TrimSpace(out)
		if err == nil && last == "true" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("MinIO not ready within %s (last ready=%q)", timeout, last)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}
