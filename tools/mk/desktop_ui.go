package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// desktopDevServerURL is where `wails dev` serves desktop/frontend with the
// real Go bindings injected, once its own build step (compile Go, package,
// self-sign) finishes. cmd/buildmax-desktop/wails.json sets
// frontend:dev:serverUrl to "auto", and this is Wails' fixed default for that
// setting — the project does not override it, so there is nothing to read out
// of the config here.
const desktopDevServerURL = "http://localhost:34115"

// e2eDesktopUI drives desktop/frontend's real UI through the browser dev
// server `wails dev` exposes, bindings and all.
//
// This is narrower than the packaged-app smoke
// docs/design/end-to-end-testing.md §9 still lists as open work: it proves the
// React app and its bound Go methods work together, not that the native
// window renders the same page, and not the signed, packaged binary.
// `wails dev` launches a native window too, as a side effect of starting —
// this suite makes no assertion about it and does not need one to be present.
func e2eDesktopUI(args []string) error {
	// core runs only the @smoke-tagged cases -- the subset CI gates every desktop
	// change on. The full run is the default and is what the release matrix and
	// an on-demand local check use.
	core := false
	switch {
	case len(args) == 0:
	case len(args) == 1 && args[0] == "core":
		core = true
	default:
		return usageErrorf("e2e", "desktop-ui takes at most `core`")
	}

	if core {
		fmt.Println("[e2e] Desktop UI suite (core): the @smoke cases only, through `wails dev`'s browser bridge")
	} else {
		fmt.Println("[e2e] Desktop UI suite: desktop/frontend driven through `wails dev`'s browser bridge (a native window may briefly appear as a side effect of `wails dev` itself)")
	}
	if err := e2eDesktopUIPreflight(); err != nil {
		return err
	}

	// A fresh, unseeded BUILDMAX_HOME per run: never the contributor's real
	// ~/.buildmax, and never even the sandbox `run desktop` seeds from it. No
	// settings.yaml and nobody signed in is also the one state this suite needs
	// to stay deterministic without a model or an account.
	home, err := os.MkdirTemp("", "buildmax-e2e-desktop-ui-")
	if err != nil {
		return fmt.Errorf("create an isolated BUILDMAX_HOME: %w", err)
	}
	defer os.RemoveAll(home)

	artifacts, runID, err := prepareArtifacts("desktop-ui", "desktop-ui", desktopDevServerURL)
	if err != nil {
		return err
	}
	fmt.Printf("[e2e] artifacts from this run: %s\n", artifacts)

	dev, err := startWailsDev(home, filepath.Join(artifacts, "wails-dev.log"))
	if err != nil {
		return err
	}
	defer dev.stop()

	// The wait covers a full compile of the Desktop binary, not only a server
	// starting: `wails dev` generates bindings and builds before it listens.
	// A cold build on a CI macOS runner took about a minute and has crossed 90
	// seconds, at which point the build was killed mid-compile and read as a
	// hang. The budget stays finite so a real hang still fails.
	ctx, cancel := context.WithTimeout(context.Background(), wailsDevReadyTimeout)
	defer cancel()
	if err := waitForHTTP(ctx, &http.Client{Timeout: 5 * time.Second}, desktopDevServerURL, wailsDevReadyTimeout); err != nil {
		return fmt.Errorf("`wails dev` never answered at %s: %w\nSee %s", desktopDevServerURL, err, dev.logPath)
	}

	fmt.Printf("[e2e] running desktop UI browser tests against %s\n", desktopDevServerURL)
	npmArgs := []string{"run", "e2e"}
	if core {
		// Everything after `--` reaches playwright; --grep selects by tag.
		npmArgs = append(npmArgs, "--", "--grep", "@smoke")
	}
	testErr := runWith(filepath.Join("desktop", "frontend"), []string{
		"BUILDMAX_E2E_BASE_URL=" + desktopDevServerURL,
		"BUILDMAX_E2E_RUN_ID=" + runID,
		"BUILDMAX_E2E_ARTIFACTS=" + filepath.Join(artifacts, "results"),
	}, "npm", npmArgs...)
	if testErr != nil {
		return fmt.Errorf("%w\nSee %s for what `wails dev` printed", testErr, dev.logPath)
	}
	return nil
}

// wailsDevReadyTimeout is how long `wails dev` gets to compile and answer.
const wailsDevReadyTimeout = 5 * time.Minute

// e2eDesktopUIPreflight names what is missing before `wails dev` starts. Its
// own failure two build steps in reads like a Wails problem, which sends the
// reader to the wrong place.
func e2eDesktopUIPreflight() error {
	if err := requireCommands("go", "node", "npm"); err != nil {
		return err
	}
	// `wails dev` compiles the frontend once before it starts watching, and
	// that build fails on a missing gui/dist the same way `build desktop` does
	// — but from inside the Wails CLI's own output, where it is hard to place.
	// The suite provisions the gui build itself, the same as it does the
	// desktop frontend test deps below; the early refusal stays only as a
	// fallback when that build cannot produce gui/dist.
	if isDir("gui") && !exists(filepath.Join("gui", "dist", "index.js")) {
		fmt.Println("[e2e] building the gui package (required by `wails dev`)...")
		if err := runIn("gui", "npm", "ci"); err != nil {
			return fmt.Errorf("build the gui package (npm ci): %w", err)
		}
		if err := runIn("gui", "npm", "run", "build"); err != nil {
			return fmt.Errorf("build the gui package (npm run build): %w", err)
		}
		if !exists(filepath.Join("gui", "dist", "index.js")) {
			return fmt.Errorf("gui not built (missing gui/dist/index.js); run `cd gui && npm ci && npm run build`")
		}
	}
	frontend := filepath.Join("desktop", "frontend")
	if _, err := os.Stat(filepath.Join(frontend, "node_modules", "@playwright", "test")); err != nil {
		fmt.Println("[e2e] installing the desktop frontend test dependencies...")
		if err := runIn(frontend, "npm", "ci"); err != nil {
			return fmt.Errorf("install the desktop frontend test dependencies: %w", err)
		}
	}
	// The browsers stay a decision, same as the Portal suite: a few hundred
	// megabytes into a cache shared with every other project on the machine,
	// not a directory this repository owns.
	if dir := playwrightBrowserDir(); dir != "" {
		if _, err := os.Stat(dir); err != nil {
			return fmt.Errorf("no Playwright browsers in %s: run `npm --prefix desktop/frontend exec -- playwright install chromium`", dir)
		}
	}
	return nil
}

// wailsDevProcess is the running `wails dev` this suite owns: its own
// process, not one a contributor started, so this suite is what stops it.
type wailsDevProcess struct {
	cmd     *exec.Cmd
	logPath string
	logFile *os.File
}

// startWailsDev launches `wails dev` in its own process group so stop() can
// tear down the whole tree it spawns — the Wails CLI itself, the compiled
// wails binary, the native app, and the Vite dev server underneath it — not
// just the `go run` wrapper this starts.
func startWailsDev(home, logPath string) (*wailsDevProcess, error) {
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return nil, err
	}
	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("go", "run", wailsCLIPkg, "dev", "-tags", "desktop")
	cmd.Dir = desktopDir
	cmd.Env = append(os.Environ(), "BUILDMAX_HOME="+home)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return nil, fmt.Errorf("start `wails dev`: %w", err)
	}
	fmt.Printf("[e2e] wails dev starting (pid %d, BUILDMAX_HOME=%s, log %s)\n", cmd.Process.Pid, home, logPath)
	return &wailsDevProcess{cmd: cmd, logPath: logPath, logFile: logFile}, nil
}

func (p *wailsDevProcess) stop() {
	if p == nil || p.cmd.Process == nil {
		return
	}
	killProcessGroup(p.cmd)
	_ = p.cmd.Wait()
	_ = p.logFile.Close()
}
