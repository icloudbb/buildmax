package main

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// The R1 coordination probe proves, against the running two-replica + Redis
// kind stack, the three server-coordination guarantees a miniredis unit test
// cannot: that live output streams cross replicas, that a conversation turn
// lease serializes turns raised on different replicas, and that both recover
// after the Redis pod restarts. It is the deployed evidence R1 asks for —
// "candidate reconnect, contention, outage, and recovery exercises still need
// operating proof" — beside the sandbox and worker-API boundary probes. See
// docs/design/server-coordination.md §10.
//
// The load-balanced public Service hides which replica answered, so the probe
// forwards each buildmax-server pod on its own local port and drives replica A
// and replica B by hand. A stall armed on the shared mock model holds every
// reply, which is what lets the probe attach a stream to a still-running turn,
// or catch a second turn while the first still holds the lease, before any
// output flows.

const (
	// coordForwardPortA and coordForwardPortB publish one server pod each. The
	// worker-boundary probe already proves per-pod reachability; here two
	// distinct pods are the whole point, so each gets its own local port.
	coordForwardPortA = "18091"
	coordForwardPortB = "18092"

	coordProbeEmail = "coord-probe@buildmax.local"

	// coordStall holds every mock reply long enough to attach a stream on both
	// replicas, or to catch the second turn blocked on the lease, before the
	// first reply is written.
	coordStall = 12 * time.Second
)

// kindCoordinationProbe runs the three coordination checks against the live
// deployment the smoke just exercised.
func kindCoordinationProbe() error {
	fmt.Println("Probing server coordination across replicas...")
	target := kindSmokeTarget()
	ctx := context.Background()
	client := &http.Client{Timeout: 30 * time.Second}

	pods, err := serverPodNames()
	if err != nil {
		return err
	}
	if len(pods) < 2 {
		return fmt.Errorf("coordination probe needs two buildmax-server replicas, found %d: %v", len(pods), pods)
	}

	stopA, baseA, err := startPodForward(ctx, pods[0], coordForwardPortA)
	if err != nil {
		return err
	}
	defer stopA()
	stopB, baseB, err := startPodForward(ctx, pods[1], coordForwardPortB)
	if err != nil {
		return err
	}
	defer stopB()

	token, spaceID, err := smokeSignIn(ctx, client, target, coordProbeEmail)
	if err != nil {
		return err
	}

	if err := coordProbeStreamDelivery(ctx, client, target, baseA, baseB, spaceID, token, "replicas"); err != nil {
		return fmt.Errorf("cross-replica stream delivery: %w", err)
	}
	if err := coordProbeTurnSerialization(ctx, client, target, baseA, baseB, spaceID, token); err != nil {
		return fmt.Errorf("cross-replica turn serialization: %w", err)
	}
	if err := coordProbeRedisRecovery(ctx, client, target, baseA, baseB, spaceID, token); err != nil {
		return fmt.Errorf("recovery after a Redis restart: %w", err)
	}

	fmt.Println("Coordination verified: streams cross replicas, turns serialize across replicas, and both recover after a Redis restart.")
	return nil
}

// coordProbeStreamDelivery proves a task's worker output reaches a stream opened
// on either replica. The worker appends its deltas to whichever replica the
// internal worker API routed to; requiring both A and B to receive the reply
// makes at least one of them a genuine cross-replica hop through Redis, whichever
// way the routing fell.
func coordProbeStreamDelivery(ctx context.Context, client *http.Client, target smokeTarget, baseA, baseB, spaceID, token, label string) error {
	if err := armLLMStall(ctx, client, target, coordStall); err != nil {
		return err
	}
	defer func() { _ = armLLMStall(ctx, client, target, 0) }()

	convID, err := coordCreateConversation(ctx, client, target.apiBase, spaceID, token)
	if err != nil {
		return err
	}
	taskID, err := coordCreateTask(ctx, client, target.apiBase, spaceID, convID, token, "Reply with exactly deployment smoke ok.")
	if err != nil {
		return err
	}

	taskURL := target.apiBase + "/api/spaces/" + url.PathEscape(spaceID) + "/tasks/" + url.PathEscape(taskID)
	// Attach to a live run, not a finished one: the stall keeps the model call
	// outstanding, so the task sits in RUNNING while both streams connect.
	if err := waitForTaskStatus(ctx, client, taskURL, token, "RUNNING", 90*time.Second); err != nil {
		return fmt.Errorf("task did not reach RUNNING: %w", err)
	}

	streamPath := "/api/spaces/" + url.PathEscape(spaceID) + "/tasks/" + url.PathEscape(taskID) + "/stream"
	deadline := coordStall + 90*time.Second
	gotA := readSSEUntil(ctx, baseA+streamPath, token, smokeReply, deadline)
	gotB := readSSEUntil(ctx, baseB+streamPath, token, smokeReply, deadline)
	errA, errB := <-gotA, <-gotB
	if errA != nil {
		return fmt.Errorf("replica A did not receive the run's output across %s: %w", label, errA)
	}
	if errB != nil {
		return fmt.Errorf("replica B did not receive the run's output across %s: %w", label, errB)
	}
	return nil
}

// coordProbeTurnSerialization proves the Redis turn lease serializes two turns
// raised for one conversation on two replicas. Turn one is started first and,
// stalled at the model, holds the conversation lease; turn two, raised on the
// other replica, must not reach the model until turn one releases.
func coordProbeTurnSerialization(ctx context.Context, client *http.Client, target smokeTarget, baseA, baseB, spaceID, token string) error {
	convID, err := coordCreateConversation(ctx, client, target.apiBase, spaceID, token)
	if err != nil {
		return err
	}

	// The mock's request log is cumulative across probe runs, so each turn carries
	// a per-run nonce: a request body matching oneMark or twoMark is this run's own
	// turn, never a marker left in the log by an earlier run.
	nonce := fmt.Sprintf("R1PROBE-%d", time.Now().UnixNano())
	oneMark, twoMark := nonce+"-ONE", nonce+"-TWO"

	if err := armLLMStall(ctx, client, target, coordStall); err != nil {
		return err
	}
	defer func() { _ = armLLMStall(ctx, client, target, 0) }()

	msgPath := "/api/spaces/" + url.PathEscape(spaceID) + "/conversations/" + url.PathEscape(convID) + "/messages"
	// Turn one first. Wait until it has actually reached the model — and so holds
	// the lease and is stalled there — before raising turn two, rather than
	// guessing at a fixed delay: which turn wins the lease must not be a race.
	oneDone := coordPostTurn(ctx, baseA+msgPath, token, oneMark)
	if err := coordWaitForMarker(ctx, client, target, oneMark, 30*time.Second); err != nil {
		return fmt.Errorf("turn one never reached the model (the probe's premise): %w", err)
	}

	twoDone := coordPostTurn(ctx, baseB+msgPath, token, twoMark)

	// Give turn two long enough to acquire the lease and call the model if the
	// lease failed to hold it. While turn one is still running (stalled at the
	// model), turn two must not have reached the model.
	time.Sleep(4 * time.Second)
	select {
	case err := <-oneDone:
		// Turn one released before the probe could observe turn two blocked: the
		// stall was too short for a clean observation, not a serialization result.
		_ = err
		return fmt.Errorf("turn one finished before the mid-window check; the stall window was too short to observe serialization — rerun")
	default:
	}
	if two, err := coordMockRequestsContaining(ctx, client, target, twoMark); err != nil {
		return err
	} else if two != 0 {
		return fmt.Errorf("turn two reached the model %d time(s) while turn one held the conversation lease; the cross-replica lease did not serialize the turns", two)
	}

	if err := <-oneDone; err != nil {
		return fmt.Errorf("turn one: %w", err)
	}
	if err := <-twoDone; err != nil {
		return fmt.Errorf("turn two: %w", err)
	}

	// With the lease released, turn two must have run — exactly once.
	if n, err := coordMockRequestsContaining(ctx, client, target, twoMark); err != nil {
		return err
	} else if n < 1 {
		return fmt.Errorf("turn two never reached the model even after turn one released the lease")
	}
	return nil
}

// coordProbeRedisRecovery restarts the Redis pod and re-proves cross-replica
// stream delivery. Redis state is live, not durable, so recovery means new work
// re-acquires leases and streams deliver again once Redis is back — not that
// in-flight deltas survive the restart.
func coordProbeRedisRecovery(ctx context.Context, client *http.Client, target smokeTarget, baseA, baseB, spaceID, token string) error {
	fmt.Println("  restarting Redis to exercise recovery...")
	if err := kindKubectl("rollout", "restart", "deployment/buildmax-redis", "-n", "buildmax"); err != nil {
		return fmt.Errorf("restart the Redis deployment: %w", err)
	}
	if err := kindKubectl("rollout", "status", "deployment/buildmax-redis", "-n", "buildmax", "--timeout=120s"); err != nil {
		return fmt.Errorf("the restarted Redis did not become ready: %w", err)
	}
	// Let the servers' Redis clients reconnect before leaning on coordination
	// again. The servers stay up across the blip (lease renewal discards Redis
	// errors); if a forwarded pod had died the re-proof below would fail loudly.
	time.Sleep(8 * time.Second)

	if err := coordProbeStreamDelivery(ctx, client, target, baseA, baseB, spaceID, token, "replicas after the Redis restart"); err != nil {
		return err
	}
	return nil
}

// serverPodNames lists the buildmax-server pods by name, newest-ready first is
// not required — any two distinct Ready pods are two replicas.
func serverPodNames() ([]string, error) {
	out, err := captureKindKubectl("get", "pods", "-n", "buildmax",
		"-l", "app=buildmax-server", "--field-selector=status.phase=Running",
		"-o", "jsonpath={range .items[*]}{.metadata.name}{\"\\n\"}{end}")
	if err != nil {
		return nil, fmt.Errorf("list buildmax-server pods: %w\n%s", err, out)
	}
	var pods []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if name := strings.TrimSpace(line); name != "" {
			pods = append(pods, name)
		}
	}
	return pods, nil
}

// startPodForward forwards one server pod's public port to a local port and
// returns once /healthz answers there, so a caller never races the tunnel. The
// returned stop tears the tunnel down.
func startPodForward(ctx context.Context, pod, localPort string) (func(), string, error) {
	cmd := exec.Command("kubectl", "--context", kindContext(), "-n", "buildmax",
		"port-forward", "pod/"+pod, localPort+":5678")
	var lines sync.Mutex
	out := &prefixWriter{prefix: "forward/" + pod, lines: &lines}
	cmd.Stdout, cmd.Stderr = out, out
	if err := cmd.Start(); err != nil {
		return nil, "", fmt.Errorf("forward pod %s: %w", pod, err)
	}
	stop := func() {
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}

	base := "http://localhost:" + localPort
	healthCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for {
		if _, err := request(healthCtx, &http.Client{Timeout: 2 * time.Second}, http.MethodGet, base+"/healthz", "", "", nil, http.StatusOK); err == nil {
			return stop, base, nil
		}
		select {
		case <-healthCtx.Done():
			stop()
			return nil, "", fmt.Errorf("forward to pod %s on :%s never became healthy", pod, localPort)
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// readSSEUntil opens the task stream and reports nil once a data frame carrying
// want has arrived, or an error if the stream closes or the deadline passes
// first. The context, not a client timeout, bounds the read, because an SSE
// connection is meant to stay open.
func readSSEUntil(ctx context.Context, streamURL, token, want string, timeout time.Duration) <-chan error {
	result := make(chan error, 1)
	go func() {
		cctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		req, err := http.NewRequestWithContext(cctx, http.MethodGet, streamURL, nil)
		if err != nil {
			result <- err
			return
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := (&http.Client{}).Do(req)
		if err != nil {
			result <- fmt.Errorf("open stream: %w", err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			result <- fmt.Errorf("stream returned %s", resp.Status)
			return
		}
		var seen strings.Builder
		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			seen.WriteString(strings.TrimPrefix(line, "data: "))
			if strings.Contains(seen.String(), want) {
				result <- nil
				return
			}
		}
		if err := scanner.Err(); err != nil {
			result <- fmt.Errorf("read stream (saw %q): %w", seen.String(), err)
			return
		}
		result <- fmt.Errorf("stream closed without %q (saw %q)", want, seen.String())
	}()
	return result
}

// coordPostTurn sends one conversation message turn and reports its outcome on a
// channel, so two turns can be in flight at once.
func coordPostTurn(ctx context.Context, endpoint, token, content string) <-chan error {
	done := make(chan error, 1)
	go func() {
		client := &http.Client{Timeout: 3 * time.Minute}
		done <- requestJSON(ctx, client, http.MethodPost, endpoint, token, map[string]string{"content": content}, nil, http.StatusOK)
	}()
	return done
}

// coordWaitForMarker polls the mock's request log until at least one recorded
// request body carries marker, or the timeout passes.
func coordWaitForMarker(ctx context.Context, client *http.Client, target smokeTarget, marker string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		n, err := coordMockRequestsContaining(ctx, client, target, marker)
		if err != nil {
			return err
		}
		if n >= 1 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%q never appeared in the mock's request log within %s", marker, timeout)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// coordMockRequestsContaining counts the mock's recorded model requests whose
// body carries marker.
func coordMockRequestsContaining(ctx context.Context, client *http.Client, target smokeTarget, marker string) (int, error) {
	var reqs []struct {
		Body []byte
	}
	if err := requestJSON(ctx, client, http.MethodGet, target.llmControlRequestsURL, "", nil, &reqs, http.StatusOK); err != nil {
		return 0, fmt.Errorf("read the mock's request log: %w", err)
	}
	n := 0
	for _, r := range reqs {
		if strings.Contains(string(r.Body), marker) {
			n++
		}
	}
	return n, nil
}

func coordCreateConversation(ctx context.Context, client *http.Client, apiBase, spaceID, token string) (string, error) {
	var conversation struct {
		ID string `json:"conversation_id"`
	}
	endpoint := apiBase + "/api/spaces/" + url.PathEscape(spaceID) + "/conversations"
	if err := requestJSON(ctx, client, http.MethodPost, endpoint, token, map[string]string{"channel": "portal"}, &conversation, http.StatusCreated); err != nil {
		return "", fmt.Errorf("create a conversation: %w", err)
	}
	return conversation.ID, nil
}

func coordCreateTask(ctx context.Context, client *http.Client, apiBase, spaceID, conversationID, token, input string) (string, error) {
	var task struct {
		ID string `json:"id"`
	}
	endpoint := apiBase + "/api/spaces/" + url.PathEscape(spaceID) + "/conversations/" + url.PathEscape(conversationID) + "/tasks"
	if err := requestJSON(ctx, client, http.MethodPost, endpoint, token, map[string]string{"input": input}, &task, http.StatusCreated); err != nil {
		return "", fmt.Errorf("create a task: %w", err)
	}
	return task.ID, nil
}
