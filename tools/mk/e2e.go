package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// artifactDir is where a failed suite leaves its evidence. One predictable
// place, cleared at the start of a run, so "read the artifact" is an
// instruction rather than a hunt.
const artifactDir = ".artifacts/e2e"

// cmdE2E runs one named end-to-end suite.
//
// The suites differ in what they own. The Portal ones attach to a deployment
// someone else brought up — kind or Compose locally, the smoke jobs in CI —
// because what they prove is that the published bundle works against a real
// server, and a stack this command started privately would be a different
// claim. `local` is the exception: it owns a throwaway Compose stack for one
// run, which is the shape a contributor wants when there is nothing running
// yet. `cli` owns nothing but a temporary directory.
//
// Whichever it is, the command says so before it starts. A suite that silently
// chose a mode leaves the reader guessing what its result covered.
//
// For the same reason there is no default suite. It used to be kind, the one
// with the heaviest prerequisite: a bare `./make e2e` reached for a cluster the
// reader had not started, and reported that as the failure. Refusing prints the
// six suites and what each one needs, which is the answer that invocation was
// asking for.
//
// The two deployments differ in a way the tests can see. kind puts one ingress
// in front of Portal and server, so the bundle's API base is same-origin;
// Compose publishes them on separate ports, so it is absolute. Neither is wrong
// and the browser cannot guess which it is looking at, so the target decides and
// passes the answer in.
func cmdE2E(args []string) error {
	if len(args) == 0 {
		return usageErrorf("e2e", "e2e needs a suite")
	}
	suite := args[0]
	// desktop-ui optionally takes a scope; every other suite takes none.
	if suite == "desktop-ui" {
		return e2eDesktopUI(args[1:])
	}
	if len(args) > 1 {
		return usageErrorf("e2e", "e2e runs one suite at a time")
	}
	switch suite {
	case "cli":
		return e2eCLI()
	case "desktop":
		return e2eDesktopBridge()
	case "desktop-launch":
		return e2eDesktopLaunch()
	case "all":
		return e2eFullMatrix()
	case "local":
		return e2eOwningCompose()
	case "kind", "compose":
		target, err := e2eTarget(suite)
		if err != nil {
			return err
		}
		// kind attaches to a shared, persistent cluster whose single-pod MySQL
		// can be mid-restart; refuse before making test data rather than fail
		// the suite in ways that read as product bugs. Compose owns a fresh
		// database it just started, so it has nothing to preflight.
		if suite == "kind" {
			if err := kindDatabasePreflight(); err != nil {
				return err
			}
			// Before any test data: a green result against a stale :local image is
			// worse than a red one, because it is read as passing on this checkout.
			if err := kindSourceIdentityPreflight(); err != nil {
				return err
			}
		}
		fmt.Printf("[e2e] attaching to the %s deployment at %s (this command did not start it)\n", suite, target.portalURL)
		return e2ePortal(target, suite)
	default:
		return usageErrorf("e2e", "unknown e2e suite: %s", suite)
	}
}

// e2eCLI runs the suite that needs no deployment at all. It is listed here so
// an agent choosing a suite finds every suite in one place, rather than
// learning that this one is spelled as a Go test.
func e2eCLI() error {
	fmt.Println("[e2e] CLI and TUI suite: the built binary, a temporary home, and a scripted model")
	// -count=1 because a cached pass is not evidence that the binary built from
	// the current tree still works.
	return runCmd("go", "test", "-count=1", "./internal/e2e/cli/...")
}

// e2eDesktopBridge runs the Wails bridge suite. It stops at the bridge, in Go:
// no browser, no window. `desktop-ui` drives the React app and the same bound
// methods through `wails dev`'s browser dev server; the native window and the
// packaged, signed build are what still has no suite — see e2eDesktopUI.
//
// The name is the suite: a bridge test called anything but TestBridge* is not
// selected here, so it runs only in `./make test` and this command reports a
// pass without having executed it.
func e2eDesktopBridge() error {
	fmt.Println("[e2e] Desktop bridge suite: bound methods, frontend events, approvals, and history — no window")
	return runCmd("go", "test", "-count=1", "-run", "^TestBridge", "./internal/interface/desktop/")
}

// e2eFullMatrix runs every suite that this machine can run on its own, cheapest
// first, and stops at the first failure.
//
// kind is not in it. That suite needs a cluster this command would have to
// create, and a release check that quietly builds a Kubernetes cluster is a
// surprise; run `./make e2e kind` against one deliberately.
func e2eFullMatrix() error {
	for _, suite := range []struct {
		name string
		run  func() error
	}{
		{"cli", e2eCLI},
		{"desktop", e2eDesktopBridge},
		{"local", e2eOwningCompose},
	} {
		fmt.Printf("[e2e] matrix: %s\n", suite.name)
		if err := suite.run(); err != nil {
			return fmt.Errorf("the %s suite failed, so the rest were not run: %w", suite.name, err)
		}
	}
	fmt.Println("[e2e] matrix passed: cli, desktop, and local")
	return nil
}

// e2eOwningCompose brings up a Compose stack under a project name and ports
// this run picked for itself, runs the browser tests, and takes the stack
// down again, volumes included. Failure still tears down: a stack left
// running after a failed run is a trap for the next one, which would attach
// to it and report on the wrong deployment.
//
// The project name and ports are chosen fresh every run rather than reused
// from a fixed default, so this never has to guess whether something already
// answering on the usual port is a contributor's persistent stack or a
// leftover of its own — it is always neither, and several runs (different
// worktrees, different agents, a human's `compose up` alongside them) can own
// a stack of their own at the same time.
func e2eOwningCompose() (err error) {
	if err = ephemeralComposeEnv("buildmax-e2e"); err != nil {
		return err
	}
	fmt.Printf("[e2e] owning a Compose stack (project %s, port %s) for this run: starting it, testing it, and taking it down\n",
		composeProjectName(), envOr("BUILDMAX_PORTAL_PORT", "8080"))
	// Registered before `up`, so teardown runs even when startup fails partway:
	// `compose up` that errors after creating some containers would otherwise
	// leave a partial stack for the next run to attach to and report on. -v,
	// unlike a plain `compose down`: a project this run invented has no reason to
	// keep its volumes for a next run that will invent its own.
	defer func() {
		downErr := runCmd("docker", append(composeSmokeArgs(true), "down", "-v")...)
		if downErr != nil && err == nil {
			err = fmt.Errorf("take the Compose stack down: %w", downErr)
		}
	}()
	// The smoke overlay, not a plain `up`: the browser tests drive a run to
	// completion, which needs the deterministic model in front of the server
	// rather than a provider key this machine may not have.
	if upErr := composeUpSmokeStack(false); upErr != nil {
		return fmt.Errorf("start the Compose stack: %w", upErr)
	}
	return e2ePortal(composeSmokeTarget(false), "local")
}

// ephemeralComposeEnv picks a Compose project name (prefix plus a random
// suffix) and three host ports nothing else is using, and sets them as this
// process's environment, which is what composeProjectName, composePortalURL,
// and the rest of this file read.
// Every docker/npm child this run starts inherits that environment, so one
// assignment here is what the whole stack, and the tests against it, agree on.
func ephemeralComposeEnv(prefix string) error {
	suffix, err := randomHex(4)
	if err != nil {
		return fmt.Errorf("choose an ephemeral Compose project: %w", err)
	}
	ports := map[string]string{}
	for _, key := range []string{"BUILDMAX_SERVER_PORT", "BUILDMAX_PORTAL_PORT", "BUILDMAX_SMOKE_LLM_PORT"} {
		port, err := freeTCPPort()
		if err != nil {
			return fmt.Errorf("choose a free port for %s: %w", key, err)
		}
		ports[key] = strconv.Itoa(port)
	}
	if err := os.Setenv("BUILDMAX_COMPOSE_PROJECT", prefix+"-"+suffix); err != nil {
		return err
	}
	for key, value := range ports {
		if err := os.Setenv(key, value); err != nil {
			return err
		}
	}
	return nil
}

// freeTCPPort asks the OS for a port nothing is listening on right now, by
// binding to one and giving it back. Another process could still claim it
// before docker does, but that race is the same one any dev tool that picks
// its own port runs, and it is not worth a retry loop here.
func freeTCPPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

// e2ePortal runs the browser tests against an already-running deployment.
// invocation is the suite name this run was asked for, which is what the run
// note has to repeat: the artifact directory is named for the surface, and
// telling a reader to run `e2e portal` would name no command at all.
func e2ePortal(target smokeTarget, invocation string) error {
	if err := e2ePreflight(); err != nil {
		return err
	}
	baseURL := envOr("BUILDMAX_E2E_BASE_URL", target.portalURL)

	ctx := context.Background()
	client := &http.Client{Timeout: 5 * time.Second}
	if err := waitForHTTP(ctx, client, baseURL, 15*time.Second); err != nil {
		return fmt.Errorf("no deployment is answering at %s: %w\nStart one with `%s kind up` or `%s compose smoke`, run `%s e2e local` to have one started for you, or set BUILDMAX_E2E_BASE_URL", baseURL, err, mk(), mk(), mk())
	}
	if err := confirmDeployment(ctx, client, baseURL, target); err != nil {
		return err
	}

	// A login code arrives out of band by design, so the browser cannot fetch
	// one. Issuing it here is what lets the tests sign in.
	if output, err := target.admin("user", "create", smokeEmail); err != nil && !strings.Contains(output, "already has an account") {
		return fmt.Errorf("create the end-to-end account: %w", err)
	}
	// The same account holds the deployment-scoped grant, so the browser tests
	// can reach /admin. Granting an existing grant is refused rather than being
	// an error worth stopping for — this command runs against a deployment that
	// may already have been set up.
	if output, err := target.admin("admin", "grant", smokeEmail); err != nil && !strings.Contains(output, "already holds") {
		return fmt.Errorf("grant system_admin to the end-to-end account: %w", err)
	}
	code, err := issueLoginCode(target, smokeEmail)
	if err != nil {
		return err
	}
	// A second account with no grant of any kind. A role-specific view can only
	// be proved by someone who does not hold the role, and the account above
	// holds every one of them.
	if output, err := target.admin("user", "create", smokeOutsiderEmail); err != nil && !strings.Contains(output, "already has an account") {
		return fmt.Errorf("create the ungranted end-to-end account: %w", err)
	}
	memberCode, err := issueLoginCode(target, smokeOutsiderEmail)
	if err != nil {
		return err
	}

	artifacts, runID, err := prepareArtifacts("portal", invocation, baseURL)
	if err != nil {
		return err
	}

	fmt.Printf("[e2e] running Portal browser tests against %s\n", baseURL)
	fmt.Printf("[e2e] artifacts from this run: %s\n", artifacts)
	// npm run rather than npx: npx will happily resolve Playwright from a
	// global cache, where it cannot see the config's own @playwright/test
	// import. The local bin is the only one that works.
	return runWith("portal", []string{
		"BUILDMAX_E2E_BASE_URL=" + baseURL,
		"BUILDMAX_E2E_API_BASE=" + target.portalRuntimeAPIBase,
		"BUILDMAX_E2E_EMAIL=" + smokeEmail,
		"BUILDMAX_E2E_LOGIN_CODE=" + code,
		"BUILDMAX_E2E_MEMBER_EMAIL=" + smokeOutsiderEmail,
		"BUILDMAX_E2E_MEMBER_LOGIN_CODE=" + memberCode,
		// Resources the specs create carry this, so a deployment several runs
		// old can still say which run left what behind.
		"BUILDMAX_E2E_RUN_ID=" + runID,
		// A subdirectory, because Playwright empties its output directory when a
		// run starts: the note beside it would not survive being told to write
		// there.
		"BUILDMAX_E2E_ARTIFACTS=" + filepath.Join(artifacts, "results"),
	}, "npm", "run", "e2e")
}

// issueLoginCode mints a single-use code for one account. The browser cannot
// fetch one, which is the point of an out-of-band credential.
func issueLoginCode(target smokeTarget, email string) (string, error) {
	output, err := target.admin("user", "login-code", email)
	if err != nil {
		return "", fmt.Errorf("issue a login code for %s: %w", email, err)
	}
	code := loginCodePattern.FindString(output)
	if code == "" {
		return "", fmt.Errorf("the login-code command for %s returned no bmxlogin_ code", email)
	}
	return code, nil
}

// e2ePreflight names what is missing before a browser starts. Playwright's own
// failure for an absent browser arrives minutes later and reads like a test
// failure, which sends the reader to the wrong problem.
func e2ePreflight() error {
	if err := requireCommands("node", "npm"); err != nil {
		return err
	}
	// `check portal` installs the Portal dependencies when they are missing
	// rather than reporting them, and a browser run has no reason to behave
	// differently: same tree, same command, same lockfile.
	if _, err := os.Stat(filepath.Join("portal", "node_modules", "@playwright", "test")); err != nil {
		fmt.Println("[e2e] installing the Portal test dependencies...")
		if err := runIn("portal", "npm", "ci"); err != nil {
			return fmt.Errorf("install the Portal test dependencies: %w", err)
		}
	}
	// The browsers stay a decision. They are a few hundred megabytes into a
	// cache shared with every other project on the machine, not a directory this
	// repository owns, so downloading them silently is a different kind of act.
	if dir := playwrightBrowserDir(); dir != "" {
		if _, err := os.Stat(dir); err != nil {
			return fmt.Errorf("no Playwright browsers in %s: run `npm --prefix portal exec -- playwright install chromium`", dir)
		}
	}
	return nil
}

// playwrightBrowserDir is where Playwright keeps its browsers by default. An
// empty string means this platform's location is not known here, in which case
// the check is skipped rather than guessed wrong.
func playwrightBrowserDir() string {
	if custom := os.Getenv("PLAYWRIGHT_BROWSERS_PATH"); custom != "" {
		return custom
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	switch runtime.GOOS {
	case "darwin":
		return filepath.Join(home, "Library", "Caches", "ms-playwright")
	case "linux":
		return filepath.Join(home, ".cache", "ms-playwright")
	default:
		return ""
	}
}

// prepareArtifacts clears and recreates this suite's artifact directory and
// leaves a note in it saying what ran and how to run it again. It returns the
// absolute directory and the run id the suite tags its resources with.
//
// Clearing matters: evidence from an earlier run sitting beside this one's is
// worse than none, because it is read as this one's.
func prepareArtifacts(surface, invocation, baseURL string) (string, string, error) {
	dir, err := filepath.Abs(filepath.Join(artifactDir, surface))
	if err != nil {
		return "", "", err
	}
	if err := os.RemoveAll(dir); err != nil {
		return "", "", fmt.Errorf("clear %s: %w", dir, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("create %s: %w", dir, err)
	}
	runID := time.Now().UTC().Format("20060102-150405")
	// source is the working tree this run tested from. For kind it is also the
	// deployed identity: kindSourceIdentityPreflight has already refused the run
	// unless the deployment was built from this same commit, so the two are
	// proven equal by the time this is written.
	note := fmt.Sprintf("surface: %s\ndeployment: %s\nsource: %s\nrun id: %s\nreproduce: %s e2e %s\n",
		surface, baseURL, resolveCommitSHA(), runID, mk(), invocation)
	if err := os.WriteFile(filepath.Join(dir, "run.txt"), []byte(note), 0o644); err != nil {
		return "", "", fmt.Errorf("write %s: %w", filepath.Join(dir, "run.txt"), err)
	}
	return dir, runID, nil
}

// confirmDeployment checks that the stack answering is the one that was asked
// for.
//
// Both deployments publish the Portal on port 8080 by default, so something
// answering there proves nothing about which it is. Running the kind target
// against a Compose stack otherwise fails several steps later, inside a
// `kubectl exec`, with an error about the wrong thing entirely.
//
// The runtime config is the cheapest distinguishing fact: it is written at
// container start from the deployment's own API base, which is same-origin
// behind an ingress and absolute behind published ports.
func confirmDeployment(ctx context.Context, client *http.Client, portalURL string, target smokeTarget) error {
	config, err := requestText(ctx, client, http.MethodGet, strings.TrimRight(portalURL, "/")+"/config.js", "", nil, http.StatusOK)
	if err != nil {
		return fmt.Errorf("read the Portal runtime config at %s: %w", portalURL, err)
	}
	if want := fmt.Sprintf("apiBase: %q", target.portalRuntimeAPIBase); !strings.Contains(config, want) {
		return fmt.Errorf("the deployment at %s does not look like the one requested: its runtime config is %s, which does not contain %s\nCheck which stack is running, or pass the other target to `%s e2e`",
			portalURL, strings.TrimSpace(config), want, mk())
	}
	return nil
}

// e2eTarget resolves which running deployment to test. kind stays the default
// because it is the reference deployment and what the browser job in CI brings
// up.
func e2eTarget(name string) (smokeTarget, error) {
	switch name {
	case "kind":
		return kindSmokeTarget(), nil
	case "compose":
		// The browser tests read a deployment; they do not care which transport
		// its task runs use, and the file list only affects which server.yaml a
		// fresh stack would mount.
		return composeSmokeTarget(false), nil
	}
	return smokeTarget{}, fmt.Errorf("unknown e2e target %q (want kind or compose)", name)
}
