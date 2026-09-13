package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// The database-outage probe proves, against the running kind deployment, the
// dependency-degradation contract the beta-readiness record names: when the
// server loses its database at runtime it reports the failure through /readyz
// (which System Status mirrors, reading the same probes) and is taken out of the
// Service rather than restarted, and it recovers on its own once the database
// returns -- no rebuild, no lost pod. It runs in kindSmoke beside the sandbox,
// worker-API boundary, coordination, and worker-loss probes.
//
// It severs access with a deny-all NetworkPolicy on the MySQL pod rather than
// scaling MySQL away: the kind MySQL keeps its data on an emptyDir
// (deployment/kind/mysql.yaml), so removing the pod would erase the schema, and
// a server that survives the outage -- which migrates only at startup -- could
// not recover against an empty database. Blocking the network leaves the data
// intact, which is what "normal service returns after database access is
// restored" requires. The policy alone is not enough: the server's pooled
// connections are already established and the pool sets no lifetime, so they are
// reused and never notice a block on new connections. The probe therefore also
// kills the server's sessions inside MySQL, forcing the pool to reopen against
// the block; MySQL itself stays up, so recovery is a fast reconnection when the
// policy is removed rather than a wait on a database restart.
//
// /readyz is read through a direct port-forward to a server pod, not through the
// ingress: the readiness probe pulls a pod out of the Service during the outage,
// so the ingress would answer its own endpoint-less 503 rather than the app's
// 503 that names which dependency failed. The pod keeps answering /readyz
// throughout. The end-to-end recovery check afterward does go through the
// ingress, because "normal service returns" is a claim about the whole path.
//
// This is the runtime-loss path. Losing the database at process start is a
// separate, deliberate fail-fast: bootstrap connects and migrates with no
// retry, so the server exits and CrashLoopBackOffs until the database is
// reachable. That asymmetry is intended and is not what this probe exercises.

const dbOutageProbeEmail = "db-outage-probe@buildmax.local"

// dbOutageDenyPolicy denies all ingress to the MySQL pod, cutting the server's
// path to it without touching the pod or its data.
const dbOutageDenyPolicy = `apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: buildmax-drill-deny-mysql
  namespace: db
spec:
  podSelector:
    matchLabels:
      app: mysql
  policyTypes:
  - Ingress
  ingress: []
`

const (
	// dbOutageDegradeDeadline bounds the wait for /readyz to report the outage.
	// A readiness ping times out at readinessTimeout (3s) and the probe interval
	// is a few seconds; this is generous so a slow flip is not read as a stuck
	// server.
	dbOutageDegradeDeadline = 30 * time.Second
	// dbOutageRecoverDeadline bounds recovery after access is restored: the pool
	// must reconnect and a probe must pass before this elapses.
	dbOutageRecoverDeadline = 60 * time.Second
)

// kindDBOutageProbe drives the database-outage degradation and recovery drill.
func kindDBOutageProbe() error {
	fmt.Println("Probing database-outage degradation and recovery...")
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
	stop, base, err := startPodForward(ctx, pods[0], "18095")
	if err != nil {
		return err
	}
	defer stop()
	readyzURL := base + "/readyz"

	// Baseline: the pod is ready and names the database dependency healthy.
	if err := waitReadyzDatabase(ctx, client, readyzURL, "ok", true, 30*time.Second); err != nil {
		return fmt.Errorf("the server was not ready before the outage: %w", err)
	}

	// Record the server pods' restart counts so "not rebuilt" is an exact claim.
	before, err := serverPodRestarts()
	if err != nil {
		return err
	}

	policyPath, cleanup, err := writeTempManifest("buildmax-db-outage-*.yaml", dbOutageDenyPolicy)
	if err != nil {
		return err
	}
	defer cleanup()

	fmt.Println("  denying the server's access to MySQL...")
	if err := kindKubectl("apply", "-f", policyPath); err != nil {
		return fmt.Errorf("apply the deny policy: %w", err)
	}
	// Give the CNI a moment to program the policy so a reopened connection is
	// actually blocked, not racing enforcement.
	time.Sleep(2 * time.Second)
	policyDeleted := false
	defer func() {
		if !policyDeleted {
			_ = kindKubectl("delete", "-f", policyPath, "--ignore-not-found")
		}
	}()

	// The deny policy blocks new connections, but the server's pooled connections
	// are already established and the pool sets no lifetime, so they would be
	// reused indefinitely and never notice the block. Kill the server's
	// connections inside MySQL so the pool must reopen -- and, blocked by the
	// policy, fail. MySQL itself stays up, so recovery is a fast reconnection when
	// the policy is removed rather than a wait on a database restart, and its data
	// is untouched. The kill runs through the API server, which the ingress policy
	// does not gate; it kills only the application user's sessions, not root's own
	// (this one included), so it cannot cut itself off before finishing.
	fmt.Println("  killing the server's database connections...")
	killSessions := `mysql -uroot -pbuildmax -N -e "SELECT CONCAT('KILL ', id, ';') FROM information_schema.processlist WHERE user = 'buildmax'" | mysql -uroot -pbuildmax`
	if out, err := captureKindKubectl("exec", "deploy/mysql", "-n", "db", "--", "sh", "-c", killSessions); err != nil {
		return fmt.Errorf("kill the server's database connections: %w\n%s", err, out)
	}

	// The failure must surface: /readyz answers 503 and names the database check
	// failed, not merely a blanket unavailable.
	if err := waitReadyzDatabase(ctx, client, readyzURL, "failed", false, dbOutageDegradeDeadline); err != nil {
		return fmt.Errorf("the server did not report the database outage: %w", err)
	}

	// Degraded, not rebuilt: the same pods with the same restart counts.
	if err := assertServerPodsUnrestarted(before); err != nil {
		return fmt.Errorf("the outage disturbed the server pods: %w", err)
	}

	fmt.Println("  restoring MySQL access...")
	if err := kindKubectl("delete", "-f", policyPath, "--ignore-not-found"); err != nil {
		return fmt.Errorf("remove the deny policy: %w", err)
	}
	policyDeleted = true

	// The pod recovers on its own: /readyz reports the database healthy again.
	if err := waitReadyzDatabase(ctx, client, readyzURL, "ok", true, dbOutageRecoverDeadline); err != nil {
		return fmt.Errorf("the server did not recover after access was restored: %w", err)
	}

	// Normal service returns through the whole path, against the same data: a
	// sign-in reads and writes the recovered database (its user and space rows
	// exist only if the schema and data survived the outage). It is retried for a
	// short while because the pod rejoins the Service a few readiness cycles after
	// its own /readyz recovers, and a request through the ingress before then has
	// no ready endpoint to reach.
	if err := retryFor(ctx, dbOutageRecoverDeadline, func() error {
		_, _, err := smokeSignIn(ctx, client, target, dbOutageProbeEmail)
		return err
	}); err != nil {
		return fmt.Errorf("normal service did not return after recovery: %w", err)
	}

	// And no restart across the whole exercise.
	if err := assertServerPodsUnrestarted(before); err != nil {
		return fmt.Errorf("the server pods were restarted during the outage exercise: %w", err)
	}

	fmt.Println("Database-outage recovery verified: /readyz reported the database failed while the pods stayed up, then recovered and served normally through the ingress once access was restored.")
	return nil
}

// waitReadyzDatabase polls a /readyz URL until the database check reports
// wantDBStatus and the HTTP code matches wantReady (200 ready / 503 not), or
// errors if it never does within timeout.
func waitReadyzDatabase(ctx context.Context, client *http.Client, url, wantDBStatus string, wantReady bool, timeout time.Duration) error {
	wantCode := http.StatusOK
	if !wantReady {
		wantCode = http.StatusServiceUnavailable
	}
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		code, dbStatus, err := readyzDatabaseStatus(ctx, client, url)
		switch {
		case err != nil:
			lastErr = err
		case code == wantCode && dbStatus == wantDBStatus:
			return nil
		default:
			lastErr = fmt.Errorf("/readyz = %d with database %q, want %d with %q", code, dbStatus, wantCode, wantDBStatus)
		}
		if time.Now().After(deadline) {
			return lastErr
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// readyzDatabaseStatus reads one /readyz response and returns its HTTP code and
// the status of the "database" check. A response that omits the check reads as
// an empty status, which no caller is waiting for.
func readyzDatabaseStatus(ctx context.Context, client *http.Client, url string) (int, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	var body struct {
		Status string `json:"status"`
		Checks []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"checks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		// A 503 from the ingress with no ready endpoint is not the app's JSON;
		// report the code so the caller keeps polling rather than failing hard.
		return resp.StatusCode, "", nil
	}
	for _, c := range body.Checks {
		if c.Name == "database" {
			return resp.StatusCode, c.Status, nil
		}
	}
	return resp.StatusCode, "", nil
}

// serverPodRestarts maps each running buildmax-server pod to its container
// restart count.
func serverPodRestarts() (map[string]int, error) {
	out, err := captureKindKubectl("get", "pods", "-n", "buildmax",
		"-l", "app=buildmax-server", "--field-selector=status.phase=Running",
		"-o", "jsonpath={range .items[*]}{.metadata.name}{\"=\"}{.status.containerStatuses[0].restartCount}{\"\\n\"}{end}")
	if err != nil {
		return nil, fmt.Errorf("read server pod restart counts: %w\n%s", err, out)
	}
	restarts := map[string]int{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		name, count, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		n := 0
		if _, err := fmt.Sscanf(count, "%d", &n); err != nil {
			return nil, fmt.Errorf("parse restart count %q for %s: %w", count, name, err)
		}
		restarts[name] = n
	}
	if len(restarts) == 0 {
		return nil, fmt.Errorf("no running buildmax-server pods found")
	}
	return restarts, nil
}

// assertServerPodsUnrestarted fails if any recorded pod is gone or has a higher
// restart count than it did before -- either would mean the outage restarted a
// server rather than merely degrading it.
func assertServerPodsUnrestarted(before map[string]int) error {
	now, err := serverPodRestarts()
	if err != nil {
		return err
	}
	for name, was := range before {
		is, ok := now[name]
		if !ok {
			return fmt.Errorf("server pod %s is gone; it was restarted or replaced", name)
		}
		if is > was {
			return fmt.Errorf("server pod %s restarted %d -> %d", name, was, is)
		}
	}
	return nil
}

// retryFor calls fn until it returns nil or timeout elapses, waiting a second
// between attempts. It returns the last error on timeout.
func retryFor(ctx context.Context, timeout time.Duration, fn func() error) error {
	deadline := time.Now().Add(timeout)
	for {
		err := fn()
		if err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

// writeTempManifest writes a manifest to a temp file and returns its path and a
// cleanup that removes it.
func writeTempManifest(pattern, content string) (string, func(), error) {
	f, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", nil, fmt.Errorf("create temp manifest: %w", err)
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", nil, fmt.Errorf("write temp manifest: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(f.Name())
		return "", nil, fmt.Errorf("close temp manifest: %w", err)
	}
	path := f.Name()
	return path, func() { _ = os.Remove(path) }, nil
}
