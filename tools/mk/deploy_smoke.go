package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	composeFile             = "deployment/compose/compose.yaml"
	composeSmokeFile        = "deployment/compose/compose.smoke.yaml"
	composeSmokeManagedFile = "deployment/compose/compose.smoke.managed.yaml"
	smokeEmail              = "deployment-smoke@buildmax.local"
	// smokeOutsiderEmail owns a space of its own and belongs to none of the
	// smoke account's, which is what makes it able to prove a denial.
	smokeOutsiderEmail = "deployment-smoke-outsider@buildmax.local"
	smokeReply         = "deployment smoke ok"
	// smokeManagedModel is llm.default_model in the managed smoke configuration,
	// and what the call ledger must record for the run. The stack names no worker
	// model on purpose, so matching this proves two things at once: the run
	// reached a model the operator approved rather than one it picked, and an
	// empty worker model resolved to the deployment's default instead of to
	// nothing.
	smokeManagedModel = "BuildMax smoke"
)

var loginCodePattern = regexp.MustCompile(`bmxlogin_[a-f0-9]+`)

type smokeTarget struct {
	apiBase              string
	portalURL            string
	portalRuntimeAPIBase string
	admin                func(args ...string) (string, error)
	// managedLLM says this stack runs task-run inference through the gateway, so
	// the smoke additionally proves the worker held no provider credential.
	managedLLM bool
	// llmControlURL arms a stall on this stack's mock model. It is the smoke's
	// one way to change what the deployment does from outside, and it exists
	// because a run that answers instantly is over before it can be canceled.
	llmControlURL string
	// llmControlToolCallURL arms a one-shot tool call on this stack's mock
	// model, and llmControlRequestsURL reads back what it actually received —
	// together the only way to prove the worker's Bash sandbox confined a
	// real command, since the task's own final output is always the
	// scenario's scripted text regardless of what the tool did.
	llmControlToolCallURL string
	llmControlRequestsURL string
}

// cancelStall is how long the mock holds every model call while the
// cancellation case runs. It has to outlast noticing the run is RUNNING and
// asking it to stop, and it is paid twice — once by creating the task, once by
// the run — so it stays as short as that allows.
const cancelStall = 20 * time.Second

func cmdCompose(args []string) error {
	if len(args) > 0 && args[0] == "upgrade-drill" {
		return cmdComposeUpgradeDrill(args[1:])
	}
	if len(args) == 0 || len(args) > 2 {
		return usageErrorf("compose", "compose needs an action")
	}
	switch args[0] {
	case "up":
		if err := ensureComposeEnv(); err != nil {
			return err
		}
		return runCmd("docker", "compose", "-p", composeProjectName(), "-f", composeFile, "up", "-d", "--build", "--wait")
	case "smoke":
		managed, err := composeSmokeMode(args[1:])
		if err != nil {
			return err
		}
		if err := composeUpSmokeStack(managed); err != nil {
			return err
		}
		target := composeSmokeTarget(managed)
		if err := runDeploymentSmoke(context.Background(), target); err != nil {
			return err
		}
		printSmokeLogin(target)
		return nil
	case "status":
		return composeStatus()
	// logs and down name the same project either way, and neither reads the
	// mount the managed overlay changes, so both overlays are always passed
	// rather than asking which mode started the stack.
	case "logs":
		return runCmd("docker", append(composeSmokeArgs(true), "logs", "--tail", "200")...)
	case "down":
		return runCmd("docker", append(composeSmokeArgs(true), "down")...)
	default:
		return usageErrorf("compose", "unknown compose action: %s", args[0])
	}
}

// composeUpSmokeStack brings up the stack with the smoke overlay, which is what
// puts the deterministic model in front of the server. Plain `compose up` is a
// real deployment and expects a real provider, so anything that has to make the
// agent run — the API smoke and the browser tests alike — needs this one.
func composeUpSmokeStack(managed bool) error {
	if err := ensureComposeEnv(); err != nil {
		return err
	}
	return runCmd("docker", append(composeSmokeArgs(managed), "up", "-d", "--build", "--wait")...)
}

// composeSmokeMode reads the optional mode argument. Direct is the default
// because it is the transport most deployments use and the one that must keep
// working with no server-side model policy at all.
func composeSmokeMode(args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	switch args[0] {
	case "direct":
		return false, nil
	case "managed":
		return true, nil
	default:
		return false, fmt.Errorf("unknown smoke mode %q (want direct or managed)", args[0])
	}
}

func composeSmokeArgs(managed bool) []string {
	files := []string{"compose", "-p", composeProjectName(), "-f", composeFile, "-f", composeSmokeFile}
	if managed {
		files = append(files, "-f", composeSmokeManagedFile)
	}
	return files
}

// composeProjectName names the Compose project docker groups these
// containers, networks, and volumes under. Two stacks with different project
// names never collide, whatever ports they publish — which is what lets
// e2eOwningCompose hand each run of its own a name nothing else is using
// rather than reusing the fixed one a contributor's persistent `compose up`
// stack answers to.
func composeProjectName() string {
	return envOr("BUILDMAX_COMPOSE_PROJECT", "buildmax")
}

// composeSmokeTarget describes the quickstart stack. A gateway fronts the Portal
// and the server on one published port, so the browser is same-origin and the
// Portal's runtime API base is "/", like the kind reference. The server also
// publishes its own port, which the non-browser smoke calls here use directly.
func composeSmokeTarget(managed bool) smokeTarget {
	return smokeTarget{
		apiBase:               composeServerURL(),
		portalURL:             composePortalURL(),
		portalRuntimeAPIBase:  "/",
		managedLLM:            managed,
		llmControlURL:         composeSmokeLLMControlURL(),
		llmControlToolCallURL: composeSmokeLLMControlBase() + llmControlToolCallPath,
		llmControlRequestsURL: composeSmokeLLMControlBase() + llmControlRequestsPath,
		admin: func(args ...string) (string, error) {
			cmdArgs := append(composeSmokeArgs(managed), "exec", "-T", "server", "buildmax-server")
			return captureCombined("docker", append(cmdArgs, args...)...)
		},
	}
}

// llmControlStallPath, llmControlToolCallPath, and llmControlRequestsPath must
// match mockllm's ControlStallPath, ControlToolCallPath, and
// ControlRequestsPath, which this file cannot import: the task runner ships,
// and mockllm is test-only. A mismatch fails the smoke at the first arming
// rather than quietly skipping the case.
const (
	llmControlStallPath    = "/control/stall"
	llmControlToolCallPath = "/control/toolcall"
	llmControlRequestsPath = "/control/requests"
)

// composeSmokeLLMControlBase is the mock model's control route prefix as
// published by the smoke overlay. Compose maps the whole mock container port
// to the host, so every control route is reachable through it — unlike kind,
// where an ingress allowlists paths one at a time.
func composeSmokeLLMControlBase() string {
	return "http://127.0.0.1:" + envOr("BUILDMAX_SMOKE_LLM_PORT", "8091")
}

// composeSmokeLLMControlURL is the mock model's stall control route.
func composeSmokeLLMControlURL() string {
	return composeSmokeLLMControlBase() + llmControlStallPath
}

func composeEnvPath() string {
	return filepath.Join("deployment", "compose", ".env")
}

func composeServerURL() string {
	return "http://localhost:" + envOr("BUILDMAX_SERVER_PORT", "5678")
}

func composePortalURL() string {
	return "http://localhost:" + envOr("BUILDMAX_PORTAL_PORT", "8080")
}

// composeStatus reports what the quickstart stack is running without starting,
// building, or generating anything, so it stays safe to run against a stack
// this invocation did not create.
func composeStatus() error {
	if err := requireCommands("docker"); err != nil {
		return err
	}
	if !exists(composeEnvPath()) {
		fmt.Printf("%s does not exist, so the stack has never been started. Run %s compose up.\n", composeEnvPath(), mk())
		return nil
	}
	if !succeeds("docker", "info") {
		return errors.New("docker is installed but the engine is not ready")
	}

	fmt.Println("Services")
	// --all keeps exited containers visible: a server that crashed on startup is
	// exactly what this command exists to show.
	if err := runCmd("docker", append(composeSmokeArgs(true), "ps", "--all")...); err != nil {
		return err
	}
	fmt.Printf("\nServer: %s (%s)\n", composeServerURL(), httpHealth(composeServerURL()+"/healthz"))
	fmt.Printf("Portal: %s (%s)\n", composePortalURL(), httpHealth(composePortalURL()))
	fmt.Printf("\nRun %s compose logs for container logs.\n", mk())
	return nil
}

func ensureComposeEnv() error {
	if exists(composeEnvPath()) {
		return nil
	}
	return runCmd(filepath.Join("deployment", "compose", "generate-env.sh"))
}

// httpHealth describes one endpoint in a single status line, because a running
// container does not prove its published port still answers on the host.
func httpHealth(endpoint string) string {
	client := &http.Client{Timeout: 5 * time.Second}
	if err := expectHTTPStatus(context.Background(), client, endpoint, http.StatusOK); err != nil {
		return fmt.Sprintf("unreachable: %v", err)
	}
	return "healthy"
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func captureCombined(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))
	if err != nil {
		return text, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, text)
	}
	return text, nil
}

func runDeploymentSmoke(ctx context.Context, target smokeTarget) error {
	client := &http.Client{Timeout: 10 * time.Second}
	if err := waitForHTTP(ctx, client, target.apiBase+"/healthz", 90*time.Second); err != nil {
		return err
	}
	// Wait rather than assert once: the gateway that fronts the Portal may still
	// be warming up (or re-resolving a just-recreated upstream) right after the
	// stack comes up, which is a transient 502, not a failure.
	if err := waitForHTTP(ctx, client, target.portalURL, 60*time.Second); err != nil {
		return fmt.Errorf("portal: %w", err)
	}
	portalConfig, err := requestText(ctx, client, http.MethodGet, strings.TrimRight(target.portalURL, "/")+"/config.js", "", nil, http.StatusOK)
	if err != nil {
		return fmt.Errorf("portal runtime config: %w", err)
	}
	wantAPIBase := fmt.Sprintf("apiBase: %q", target.portalRuntimeAPIBase)
	if !strings.Contains(portalConfig, wantAPIBase) {
		return fmt.Errorf("portal runtime config does not contain %q: %s", wantAPIBase, strings.TrimSpace(portalConfig))
	}

	token, spaceID, err := smokeSignIn(ctx, client, target, smokeEmail)
	if err != nil {
		return err
	}

	if err := uploadSmokeFile(ctx, client, target.apiBase, spaceID, token); err != nil {
		return err
	}
	fileURL := target.apiBase + "/api/spaces/" + url.PathEscape(spaceID) + "/files/deployment-smoke.txt"
	content, err := requestText(ctx, client, http.MethodGet, fileURL, token, nil, http.StatusOK)
	if err != nil {
		return err
	}
	if content != smokeReply {
		return fmt.Errorf("uploaded file content = %q, want %q", content, smokeReply)
	}

	var conversation struct {
		ID string `json:"conversation_id"`
	}
	conversationsURL := target.apiBase + "/api/spaces/" + url.PathEscape(spaceID) + "/conversations"
	if err := requestJSON(ctx, client, http.MethodPost, conversationsURL, token, map[string]string{"channel": "portal"}, &conversation, http.StatusCreated); err != nil {
		return err
	}
	var task struct {
		ID string `json:"id"`
	}
	tasksURL := conversationsURL + "/" + url.PathEscape(conversation.ID) + "/tasks"
	if err := requestJSON(ctx, client, http.MethodPost, tasksURL, token, map[string]string{"input": "Reply with exactly deployment smoke ok."}, &task, http.StatusCreated); err != nil {
		return err
	}

	taskURL := target.apiBase + "/api/spaces/" + url.PathEscape(spaceID) + "/tasks/" + url.PathEscape(task.ID)
	output, err := waitForTaskSuccess(ctx, client, taskURL, token)
	if err != nil {
		return err
	}
	if strings.TrimSpace(output) != smokeReply {
		return fmt.Errorf("task output = %q, want %q", output, smokeReply)
	}

	// The reply, checked above, is the run's output. Downstream assertions need
	// the run that produced it, which the task names as its last run.
	var taskDetail struct {
		LastRunID string `json:"last_run_id"`
	}
	if err := requestJSON(ctx, client, http.MethodGet, taskURL, token, nil, &taskDetail, http.StatusOK); err != nil {
		return err
	}
	if taskDetail.LastRunID == "" {
		return errors.New("successful task has no last run")
	}
	runID := taskDetail.LastRunID

	if err := assertManagedRun(ctx, client, target, spaceID, runID, token); err != nil {
		return err
	}
	if err := assertWorkerSandboxConfines(ctx, client, target, spaceID, conversation.ID, token); err != nil {
		return err
	}
	if err := assertRetryRunsAgain(ctx, client, target, spaceID, task.ID, runID, token); err != nil {
		return err
	}
	if err := assertSpaceBoundaryHolds(ctx, client, target, spaceID); err != nil {
		return err
	}
	// Last, because it arms a stall on the shared mock: anything after it would
	// be waiting on that stall rather than on the deployment.
	if err := assertCancellationSettles(ctx, client, target, spaceID, conversation.ID, token); err != nil {
		return err
	}
	if err := assertWorkflowCancellationSettles(ctx, client, target, spaceID, token); err != nil {
		return fmt.Errorf("workflow cancellation: %w", err)
	}

	covered := "portal, auth, space boundary, storage, scheduler, worker, artifact, retry, task cancellation, and Workflow drain"
	if target.managedLLM {
		covered += ", with the run reaching its model through the gateway rather than a provider key"
	}
	fmt.Printf("Deployment smoke passed: %s (%s)\n", covered, target.portalURL)
	return nil
}

// assertCancellationSettles proves a run stopped on request reaches CANCELED
// and stays there.
//
// Below a deployment this is a state machine and a fake clock. Here it is a
// worker process that has to notice, an agent unwinding a model call it is
// blocked on, and a row that has to end up saying what happened. A run that
// keeps going after the API accepted the cancellation, or that settles as
// FAILED, or that never settles at all, is only visible from outside.
//
// The window comes from the mock: a run whose model call answers immediately is
// finished before anything can cancel it, so the smoke arms a stall and takes it
// away again. See docs/design/end-to-end-testing.md §6.2.
func assertCancellationSettles(ctx context.Context, client *http.Client, target smokeTarget, spaceID, conversationID, token string) error {
	if err := armLLMStall(ctx, client, target, cancelStall); err != nil {
		return err
	}
	// Cleared whatever happens below: every later wait is on the deployment, and
	// a mock still stalling would be what those waits measured.
	defer func() { _ = armLLMStall(ctx, client, target, 0) }()

	// Creating the task waits out the stall too: the create path titles the task
	// with a model call of its own, and a stall armed over HTTP cannot be aimed
	// at one caller. So this case gets a client that outlasts it, rather than
	// arming the stall later — the run is over 30ms after its worker spawns, and
	// a case that has to slip between those two events is a coin toss, not a
	// test.
	patient := &http.Client{Timeout: cancelStall + 30*time.Second}

	var task struct {
		ID string `json:"id"`
	}
	tasksURL := target.apiBase + "/api/spaces/" + url.PathEscape(spaceID) + "/conversations/" + url.PathEscape(conversationID) + "/tasks"
	if err := requestJSON(ctx, patient, http.MethodPost, tasksURL, token, map[string]string{"input": "Stall until this run is canceled."}, &task, http.StatusCreated); err != nil {
		return fmt.Errorf("cancellation: create task: %w", err)
	}
	taskURL := target.apiBase + "/api/spaces/" + url.PathEscape(spaceID) + "/tasks/" + url.PathEscape(task.ID)

	if err := waitForTaskStatus(ctx, patient, taskURL, token, "RUNNING", 2*time.Minute); err != nil {
		return fmt.Errorf("cancellation: %w", err)
	}

	// 202, not 200: 200 is the answer for a run no worker had yet, and this one
	// is executing. Accepting both would let the case pass without ever proving
	// a worker gave a run up.
	cancelBody, err := request(ctx, patient, http.MethodPost, taskURL+"/cancel", token, "", nil, http.StatusAccepted)
	if err != nil {
		return fmt.Errorf("cancellation: ask the running run to stop: %w", err)
	}
	if err := cancelBody.Close(); err != nil {
		return err
	}

	if err := waitForTaskStatus(ctx, patient, taskURL, token, "CANCELED", 2*time.Minute); err != nil {
		return fmt.Errorf("cancellation: %w", err)
	}

	// Terminal means terminal. A run resurrected by a retry nobody asked for, or
	// a worker reporting a result after the fact, would show up here.
	time.Sleep(5 * time.Second)
	var settled struct {
		Status string `json:"status"`
	}
	if err := requestJSON(ctx, patient, http.MethodGet, taskURL, token, nil, &settled, http.StatusOK); err != nil {
		return fmt.Errorf("cancellation: re-read the canceled task: %w", err)
	}
	if settled.Status != "CANCELED" {
		return fmt.Errorf("cancellation: the task left CANCELED for %s five seconds later", settled.Status)
	}

	return nil
}

// armLLMStall tells this stack's mock model to hold every later reply for d.
// Zero clears it.
func armLLMStall(ctx context.Context, client *http.Client, target smokeTarget, d time.Duration) error {
	if target.llmControlURL == "" {
		return errors.New("this stack does not publish its mock model's control route")
	}
	body := map[string]int{"ms": int(d.Milliseconds())}
	if err := requestJSON(ctx, client, http.MethodPost, target.llmControlURL, "", body, nil, http.StatusOK); err != nil {
		return fmt.Errorf("arm a %s model stall: %w", d, err)
	}
	return nil
}

// smokeSandboxProbeMarker and smokeSandboxDeniedMarker are what the probe
// command prints, checked in the mock's own recorded tool result rather than
// in the task's final output: the mock always answers with its scripted text
// regardless of what a tool actually returned, so that alone proves nothing.
const (
	smokeSandboxProbeMarker   = "BUILDMAX_SANDBOX_PROBE_OK"
	smokeSandboxDeniedMarker  = "BUILDMAX_SANDBOX_PROBE_WRITE_DENIED"
	smokeSandboxEscapedMarker = "BUILDMAX_SANDBOX_PROBE_WRITE_ESCAPED"
)

// smokeSandboxProbeCommand runs inside the worker's Bash sandbox. It proves
// two things a passing task never does on its own: that the sandbox actually
// ran the command (smokeSandboxProbeMarker), and that it denied a write
// outside the workspace (smokeSandboxDeniedMarker) rather than letting it
// through (smokeSandboxEscapedMarker).
const smokeSandboxProbeCommand = "echo " + smokeSandboxProbeMarker +
	" && (echo x > /etc/buildmax-smoke-should-fail 2>&1 && echo " + smokeSandboxEscapedMarker +
	" || echo " + smokeSandboxDeniedMarker + ")"

// smokeSandboxProbeArmCount covers the routing call the conversation makes to
// decide a message should become a task, ahead of the worker's own agent
// turn — confirmed by inspecting a real deployment's mock-recorded requests
// while building this probe: arming exactly once landed the tool call on
// that routing call instead, and the worker's own turn went on to answer
// with its ordinary scripted text, having never touched Bash. Arming a few
// extra means an earlier call also gets a Bash tool call it does not expect;
// empirically it tolerates that and still dispatches the task normally, and
// the worker's own turn is what echoes the marker back. Generous on purpose:
// a slower or more loaded deployment can insert more calls ahead of the
// worker's turn than the two seen locally -- a plausible explanation for a
// kind CI run that reported no Bash tool result at all despite the same
// probe passing repeatedly on a local kind cluster, though not confirmed
// against that run's own request log. Any arm the run does not use is
// dropped by the deferred clear below rather than leaking into whatever runs
// next.
const smokeSandboxProbeArmCount = 6

// assertWorkerSandboxConfines proves the worker's Bash sandbox confines a
// real command on this deployment, not merely that the sandbox surface was
// selected. Selecting it once already was not enough on its own — see
// docs/design/agent-sandbox-policy.md and deployment/seccomp/README.md for
// why the worker pod's own production hardening kept the sandbox from
// running at all until this was verified against a real pod and fixed.
//
// It arms the shared mock to answer the probe task's next few calls with a
// Bash call instead of scripted text, dispatches the task, waits for it to
// succeed, and scans the mock's own request log for a follow-up call
// carrying the tool result — the task's own final output is always the
// scenario's scripted text regardless of what the tool did, so checking that
// would prove nothing.
//
// Skips silently on a target with no control routes to arm: a target that
// cannot script its mock cannot run this probe, the same way armLLMStall's
// callers already gate on llmControlURL.
func assertWorkerSandboxConfines(ctx context.Context, client *http.Client, target smokeTarget, spaceID, conversationID, token string) error {
	if target.llmControlToolCallURL == "" || target.llmControlRequestsURL == "" {
		return nil
	}

	var before []struct{ Body []byte }
	if err := requestJSON(ctx, client, http.MethodGet, target.llmControlRequestsURL, "", nil, &before, http.StatusOK); err != nil {
		return fmt.Errorf("read sandbox probe baseline requests: %w", err)
	}

	armed := map[string]any{
		"name": "Bash", "args": map[string]any{"command": smokeSandboxProbeCommand},
		"times": smokeSandboxProbeArmCount,
	}
	if err := requestJSON(ctx, client, http.MethodPost, target.llmControlToolCallURL, "", armed, nil, http.StatusOK); err != nil {
		return fmt.Errorf("arm sandbox probe tool call: %w", err)
	}
	// However much of the arming this run actually used, none of it may
	// leak into whatever the smoke runs next.
	defer func() {
		_ = requestJSON(ctx, client, http.MethodPost, target.llmControlToolCallURL, "", map[string]any{"clear": true}, nil, http.StatusOK)
	}()

	tasksURL := target.apiBase + "/api/spaces/" + url.PathEscape(spaceID) + "/conversations/" + url.PathEscape(conversationID) + "/tasks"
	var task struct {
		ID string `json:"id"`
	}
	if err := requestJSON(ctx, client, http.MethodPost, tasksURL, token, map[string]string{"input": "Run the sandbox probe."}, &task, http.StatusCreated); err != nil {
		return fmt.Errorf("create sandbox probe task: %w", err)
	}
	taskURL := target.apiBase + "/api/spaces/" + url.PathEscape(spaceID) + "/tasks/" + url.PathEscape(task.ID)
	if _, err := waitForTaskSuccess(ctx, client, taskURL, token); err != nil {
		return fmt.Errorf("sandbox probe task: %w", err)
	}

	var after []struct{ Body []byte }
	if err := requestJSON(ctx, client, http.MethodGet, target.llmControlRequestsURL, "", nil, &after, http.StatusOK); err != nil {
		return fmt.Errorf("read sandbox probe requests: %w", err)
	}
	if len(after) <= len(before) {
		return fmt.Errorf("sandbox probe recorded no new requests")
	}
	// The command's own source carries every marker literally (it is an
	// if/else that prints one or the other), and the conversation echoes
	// that source back as the assistant's tool_calls arguments on every
	// request from here on — so matching against a whole request body would
	// find a marker whether or not the sandbox actually produced it. Only
	// the tool role's own content is the command's real output.
	var toolResult string
	for _, req := range after[len(before):] {
		var parsed struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(req.Body, &parsed); err != nil {
			continue
		}
		for _, m := range parsed.Messages {
			if m.Role == "tool" && strings.Contains(m.Content, smokeSandboxProbeMarker) {
				toolResult = m.Content
			}
		}
		if toolResult != "" {
			break
		}
	}
	if toolResult == "" {
		return fmt.Errorf("no tool result the sandbox probe made echoed back %q — the worker's Bash sandbox may not have run the probe at all", smokeSandboxProbeMarker)
	}
	if strings.Contains(toolResult, smokeSandboxEscapedMarker) {
		return fmt.Errorf("sandbox probe wrote outside its workspace — the worker's Bash sandbox is not confining commands: %s", toolResult)
	}
	if !strings.Contains(toolResult, smokeSandboxDeniedMarker) {
		return fmt.Errorf("sandbox probe's tool result missing %q: %s", smokeSandboxDeniedMarker, toolResult)
	}
	return nil
}

// waitForTaskStatus polls until the task reads want, or reports what it settled
// on instead. A terminal status that is not the one wanted ends the wait: no
// amount of further polling moves a run that is already over.
func waitForTaskStatus(ctx context.Context, client *http.Client, taskURL, token, want string, timeout time.Duration) error {
	var current struct {
		Status       string  `json:"status"`
		ErrorMessage *string `json:"error_message"`
	}
	deadline := time.Now().Add(timeout)
	for {
		if err := requestJSON(ctx, client, http.MethodGet, taskURL, token, nil, &current, http.StatusOK); err != nil {
			return err
		}
		switch {
		case current.Status == want:
			return nil
		case isTerminalSmokeStatus(current.Status):
			return fmt.Errorf("the run settled as %s rather than reaching %s: %s",
				current.Status, want, stringValue(current.ErrorMessage))
		case time.Now().After(deadline):
			return fmt.Errorf("the run did not reach %s within %s (status %s)", want, timeout, current.Status)
		}
		time.Sleep(time.Second)
	}
}

func isTerminalSmokeStatus(status string) bool {
	switch status {
	case "SUCCEEDED", "FAILED", "CANCELED":
		return true
	}
	return false
}

// waitForTaskSuccess polls a task until it succeeds, and returns its output. A
// failure names the status it ended in, because "did not succeed" sends the
// reader looking in the wrong place.
func waitForTaskSuccess(ctx context.Context, client *http.Client, taskURL, token string) (string, error) {
	var completed struct {
		Status       string  `json:"status"`
		Output       *string `json:"output"`
		ErrorMessage *string `json:"error_message"`
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		if err := requestJSON(ctx, client, http.MethodGet, taskURL, token, nil, &completed, http.StatusOK); err != nil {
			return "", err
		}
		switch {
		case completed.Status == "SUCCEEDED":
			return stringValue(completed.Output), nil
		case completed.Status == "FAILED":
			return "", fmt.Errorf("task run failed: %s", stringValue(completed.ErrorMessage))
		case time.Now().After(deadline):
			return "", fmt.Errorf("task did not finish within two minutes (status %s)", completed.Status)
		}
		time.Sleep(time.Second)
	}
}

// assertRetryRunsAgain proves a retry executes rather than records.
//
// The handler test already covers the rule that a finished run may be retried.
// What no test below a deployment can show is that the retry reaches a worker:
// a second run id is cheap to write down, and a second trace is not — it exists
// only because a process started and ran. See
// docs/design/end-to-end-testing.md §6.1.
func assertRetryRunsAgain(ctx context.Context, client *http.Client, target smokeTarget, spaceID, taskID, firstRunID, token string) error {
	var retried struct {
		TaskRunID        string `json:"task_run_id"`
		RetryOfTaskRunID string `json:"retry_of_task_run_id"`
	}
	taskURL := target.apiBase + "/api/spaces/" + url.PathEscape(spaceID) + "/tasks/" + url.PathEscape(taskID)
	if err := requestJSON(ctx, client, http.MethodPost, taskURL+"/retry", token, nil, &retried, http.StatusCreated); err != nil {
		return fmt.Errorf("retry the finished run: %w", err)
	}
	switch {
	case retried.RetryOfTaskRunID != firstRunID:
		return fmt.Errorf("retry says it retried %q, want the run that just finished, %q", retried.RetryOfTaskRunID, firstRunID)
	case retried.TaskRunID == "" || retried.TaskRunID == firstRunID:
		return fmt.Errorf("retry produced run id %q, want a new one", retried.TaskRunID)
	}

	if _, err := waitForTaskSuccess(ctx, client, taskURL, token); err != nil {
		return fmt.Errorf("the retried run: %w", err)
	}
	// Polled, not read once: a task reports SUCCEEDED before its run's trace is
	// queryable, so a single read here fails on a run that did everything right.
	traceURL := target.apiBase + "/api/spaces/" + url.PathEscape(spaceID) + "/task-runs/" + url.PathEscape(retried.TaskRunID) + "/trace"
	deadline := time.Now().Add(30 * time.Second)
	for {
		trace, err := requestText(ctx, client, http.MethodGet, traceURL, token, nil, http.StatusOK)
		if err == nil && strings.TrimSpace(trace) != "" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the retried run %s produced no trace within 30s of succeeding, so nothing executed it", retried.TaskRunID)
		}
		time.Sleep(time.Second)
	}
}

// assertSpaceBoundaryHolds proves the space boundary is enforced at the
// deployment edge.
//
// The authorization matrix is covered by handler tests against a real router,
// but those call the handler. This calls the published API through whatever
// sits in front of it — an ingress in kind, a published port in Compose — which
// is the only way to catch a deployment that authenticates somewhere else, or
// not at all.
func assertSpaceBoundaryHolds(ctx context.Context, client *http.Client, target smokeTarget, spaceID string) error {
	usageURL := target.apiBase + "/api/spaces/" + url.PathEscape(spaceID) + "/usage"
	if err := expectStatusWithToken(ctx, client, usageURL, "", http.StatusUnauthorized); err != nil {
		return fmt.Errorf("an unauthenticated read of a space: %w", err)
	}

	outsider, outsiderSpaceID, err := smokeOutsider(ctx, client, target)
	if err != nil {
		return err
	}
	if err := expectStatusWithToken(ctx, client, usageURL, outsider, http.StatusForbidden); err != nil {
		return fmt.Errorf("a signed-in stranger reading another space: %w", err)
	}
	// The same token against its own space, so the refusal above is a boundary
	// holding rather than a token that never worked.
	ownURL := target.apiBase + "/api/spaces/" + url.PathEscape(outsiderSpaceID) + "/usage"
	if err := expectStatusWithToken(ctx, client, ownURL, outsider, http.StatusOK); err != nil {
		return fmt.Errorf("the stranger reading its own space: %w", err)
	}
	return nil
}

// smokeOutsider signs in an account that belongs to no space of the smoke
// account's, returning its token and its own space id.
func smokeOutsider(ctx context.Context, client *http.Client, target smokeTarget) (string, string, error) {
	return smokeSignIn(ctx, client, target, smokeOutsiderEmail)
}

// smokeSignIn creates the account if it is new, spends a login code, and
// returns the token together with the personal space every account is given.
//
// The account is addressed by email throughout, so the failures name which one
// could not sign in: a smoke run drives two, and they prove different things.
func smokeSignIn(ctx context.Context, client *http.Client, target smokeTarget, email string) (string, string, error) {
	if output, err := target.admin("user", "create", email); err != nil && !strings.Contains(output, "already has an account") {
		return "", "", fmt.Errorf("create the account for %s: %w", email, err)
	}
	codeOutput, err := target.admin("user", "login-code", email)
	if err != nil {
		return "", "", fmt.Errorf("issue a login code for %s: %w", email, err)
	}
	code := loginCodePattern.FindString(codeOutput)
	if code == "" {
		return "", "", fmt.Errorf("the login-code command for %s returned no bmxlogin_ code", email)
	}
	var login struct {
		Token string `json:"token"`
	}
	if err := requestJSON(ctx, client, http.MethodPost, target.apiBase+"/api/auth/login", "", map[string]string{
		"email": email, "otp": code, "platform": "deployment-smoke",
	}, &login, http.StatusOK); err != nil {
		return "", "", fmt.Errorf("sign %s in: %w", email, err)
	}
	if login.Token == "" {
		return "", "", fmt.Errorf("the login response for %s contained no token", email)
	}
	var spaces []struct {
		ID string `json:"id"`
	}
	if err := requestJSON(ctx, client, http.MethodGet, target.apiBase+"/api/spaces", login.Token, nil, &spaces, http.StatusOK); err != nil {
		return "", "", err
	}
	if len(spaces) == 0 || spaces[0].ID == "" {
		return "", "", fmt.Errorf("%s has no personal space", email)
	}
	return login.Token, spaces[0].ID, nil
}

// expectStatusWithToken reads endpoint as whoever token names — nobody, when it
// is empty — and reports whether the status was the one required.
func expectStatusWithToken(ctx context.Context, client *http.Client, endpoint, token string, want int) error {
	body, err := request(ctx, client, http.MethodGet, endpoint, token, "", nil, want)
	if err != nil {
		return err
	}
	return body.Close()
}

// assertManagedRun proves the run reached its model through the gateway.
//
// The call ledger is the evidence because only a gateway call produces a row: a
// worker that had quietly used a provider key would finish the same task, return
// the same output, and leave nothing here. The row also has to name a user and a
// space, which is what the run token carries and a shared worker credential could
// never supply.
//
// It reads the ledger through the same space-authorized route an operator would,
// which makes this assertion cover the route as well as the transport.
func assertManagedRun(ctx context.Context, client *http.Client, target smokeTarget, spaceID, taskRunID, token string) error {
	if !target.managedLLM {
		return nil
	}
	var calls []struct {
		UserID  *string `json:"user_id"`
		Model   string  `json:"model"`
		Status  string  `json:"status"`
		Surface string  `json:"surface"`
	}
	callsURL := target.apiBase + "/api/spaces/" + url.PathEscape(spaceID) + "/task-runs/" + url.PathEscape(taskRunID) + "/llm-calls"
	if err := requestJSON(ctx, client, http.MethodGet, callsURL, token, nil, &calls, http.StatusOK); err != nil {
		return fmt.Errorf("run llm calls: %w", err)
	}
	if len(calls) == 0 {
		return fmt.Errorf("the managed run left no call-ledger row for %s, so it did not reach the gateway", taskRunID)
	}
	call := calls[0]
	switch {
	case call.Surface != "worker":
		return fmt.Errorf("call ledger surface = %q, want worker", call.Surface)
	case call.UserID == nil || *call.UserID == "":
		return errors.New("the managed call is attributed to no user; a run belongs to whoever created it")
	case call.Model != smokeManagedModel:
		return fmt.Errorf("call ledger model = %q, want %q", call.Model, smokeManagedModel)
	case !strings.EqualFold(call.Status, "succeeded"):
		return fmt.Errorf("call ledger status = %q, want succeeded", call.Status)
	}
	return nil
}

func printSmokeLogin(target smokeTarget) {
	output, err := target.admin("user", "login-code", smokeEmail)
	if err != nil {
		fmt.Printf("Smoke passed, but issuing an interactive login code failed: %v\n", err)
		return
	}
	fmt.Printf("Open %s and sign in as %s with this single-use code:\n%s\n", target.portalURL, smokeEmail, output)
	// The Desktop app and `buildmax login` want the API, which is the Portal's
	// own origin behind an ingress and a separate port under Compose. Saying so
	// here is cheaper than letting someone accept a default that cannot connect.
	fmt.Printf("Signing in from the Desktop app or `buildmax login`? Use Server URL %s\n", target.apiBase)
}

func waitForHTTP(ctx context.Context, client *http.Client, endpoint string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if err := expectHTTPStatus(ctx, client, endpoint, http.StatusOK); err == nil {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s did not become ready within %s", endpoint, timeout)
		}
		time.Sleep(time.Second)
	}
}

func expectHTTPStatus(ctx context.Context, client *http.Client, endpoint string, want int) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != want {
		return fmt.Errorf("GET %s returned %s", endpoint, resp.Status)
	}
	return nil
}

func requestJSON(ctx context.Context, client *http.Client, method, endpoint, token string, body, out any, want int) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	response, err := request(ctx, client, method, endpoint, token, "application/json", reader, want)
	if err != nil {
		return err
	}
	defer response.Close()
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(response).Decode(out); err != nil {
		return fmt.Errorf("decode %s %s: %w", method, endpoint, err)
	}
	return nil
}

func requestText(ctx context.Context, client *http.Client, method, endpoint, token string, body io.Reader, want int) (string, error) {
	response, err := request(ctx, client, method, endpoint, token, "", body, want)
	if err != nil {
		return "", err
	}
	defer response.Close()
	data, err := io.ReadAll(response)
	return string(data), err
}

func request(ctx context.Context, client *http.Client, method, endpoint, token, contentType string, body io.Reader, want int) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, method, endpoint, body)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" && body != nil {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != want {
		defer resp.Body.Close()
		data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("%s %s returned %s: %s", method, endpoint, resp.Status, strings.TrimSpace(string(data)))
	}
	return resp.Body, nil
}

func uploadSmokeFile(ctx context.Context, client *http.Client, apiBase, spaceID, token string) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("files", "deployment-smoke.txt")
	if err != nil {
		return err
	}
	if _, err := io.WriteString(part, smokeReply); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}
	endpoint := apiBase + "/api/spaces/" + url.PathEscape(spaceID) + "/upload"
	response, err := request(ctx, client, http.MethodPost, endpoint, token, writer.FormDataContentType(), &body, http.StatusOK)
	if err != nil {
		return err
	}
	return response.Close()
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
