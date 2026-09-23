package main

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

func cmdRun(args []string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "server":
		return runServer()
	case "cli":
		return runLocalBinary(cliBinary, "Starting CLI...", stripArgSeparator(args[1:]))
	case "desktop":
		return runLocalBinary(desktopBinary, "Starting desktop app (Ctrl+C to stop)...", nil)
	case "desktop-dev":
		return runDesktopDev()
	case "portal":
		return runPortal()
	default:
		if sub == "" {
			return usageErrorf("run", "run needs a target")
		}
		return usageErrorf("run", "unknown run target: %s", sub)
	}
}

// stripArgSeparator drops a single leading "--" from arguments forwarded to the
// CLI. The help documents `mk run cli -- <args>`, using "--" as the boundary
// between mk's own arguments and the CLI's. mk is not cobra and gives "--" no
// meaning, so without this the literal token reaches the binary, where cobra
// reads the following word as a positional argument and starts the TUI instead
// of running the named subcommand.
func stripArgSeparator(args []string) []string {
	if len(args) > 0 && args[0] == "--" {
		return args[1:]
	}
	return args
}

// runServer preflights the database before starting the server.
//
// The server needs MySQL, and the usual local one is the cluster's, forwarded
// by `mk kind forward`. Without the preflight the failure is a dial error from deep
// inside the server, which says what did not connect but not which of the two
// commands is missing.
func runServer() error {
	sandbox, err := seedSandboxConfig()
	if err != nil {
		return err
	}
	if err := checkServerDatabase(sandbox); err != nil {
		return err
	}
	return runLocalBinary(serverBinary, "Starting server (Ctrl+C to stop)...", nil)
}

// checkServerDatabase fails when the sandbox server.yaml points at a database
// on this machine that nothing is answering for.
//
// A remote database is left alone: this can only tell a contributor how to
// start the local one, and a dial from here proves nothing about a host the
// server may reach differently.
func checkServerDatabase(sandbox string) error {
	body, err := os.ReadFile(filepath.Join(sandbox, "server.yaml"))
	if err != nil {
		// No server.yaml is not a database problem; the server reports its own
		// missing configuration better than a guess here would.
		return nil
	}
	host, port := serverDatabaseAddress(string(body))
	if !isLocalHost(host) {
		return nil
	}
	conn, dialErr := net.DialTimeout("tcp", net.JoinHostPort(host, port), 500*time.Millisecond)
	if dialErr == nil {
		_ = conn.Close()
		return nil
	}
	return fmt.Errorf("no database is answering at %s, and %s needs one\n  Start the development MySQL with %s kind up, then forward it with %s kind forward in another terminal\n  Or point database.host in %s at a database you already run",
		net.JoinHostPort(host, port), mk()+" run server", mk(), mk(), filepath.Join(sandbox, "server.yaml"))
}

// serverDatabaseAddress reads database.host and database.port out of
// server.yaml. mk parses configuration textually everywhere rather than take a
// YAML dependency for the few keys it reads.
func serverDatabaseAddress(body string) (string, string) {
	host, port := "localhost", "3306"
	block := regexp.MustCompile(`(?ms)^database:\s*$(.*?)(?:^\S|\z)`).FindStringSubmatch(body)
	if block == nil {
		return host, port
	}
	if match := regexp.MustCompile(`(?m)^\s+host:\s*"?([^"\s#]+)"?`).FindStringSubmatch(block[1]); match != nil {
		host = match[1]
	}
	if match := regexp.MustCompile(`(?m)^\s+port:\s*"?(\d+)"?`).FindStringSubmatch(block[1]); match != nil {
		port = match[1]
	}
	return host, port
}

func isLocalHost(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "::1", "0.0.0.0":
		return true
	}
	return false
}

// runLocalBinary starts one of the built binaries against testing-sandbox, so a
// local run never touches the developer's real ~/.buildmax data.
func runLocalBinary(binary, banner string, extra []string) error {
	path := filepath.Join(binDir, exe(binary))
	if !exists(path) {
		return fmt.Errorf("%s not found. Run %s build first", path, mk())
	}
	sandbox, err := seedSandboxConfig()
	if err != nil {
		return err
	}
	fmt.Printf("%s (BUILDMAX_HOME=./%s)\n", banner, sandboxDir)
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	return runWith("", []string{"BUILDMAX_HOME=" + sandbox}, abs, extra...)
}

// seedSandboxConfig copies the developer's model and server config into the
// sandbox on first use, so `run server` works without re-entering settings.
// Existing sandbox files are left alone.
func seedSandboxConfig() (string, error) {
	sandbox, err := filepath.Abs(sandboxDir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(sandbox, 0o755); err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return sandbox, nil
	}
	src := filepath.Join(home, ".buildmax")
	for _, name := range []string{"settings.yaml", "server.yaml"} {
		from, to := filepath.Join(src, name), filepath.Join(sandbox, name)
		if !exists(from) || exists(to) {
			continue
		}
		if err := copyFile(from, to, 0o644); err != nil {
			return "", err
		}
		logf("sandbox", "Copied %s -> %s", from, to)
	}
	return sandbox, nil
}

// runDesktopDev starts `wails dev` in the foreground against the testing
// sandbox, the same browser bridge `./make e2e desktop-ui` drives and the one
// a `.claude/skills` driver script attaches to for ad hoc, agent-driven UI
// exploration. `./make run desktop` starts the built, packaged app instead —
// this one live-reloads and answers over HTTP at desktopDevServerURL, with no
// native window required to reach it.
func runDesktopDev() error {
	if !isDir(desktopDir) {
		return fmt.Errorf("%s not found", desktopDir)
	}
	// wails dev builds the frontend once before it starts watching, and that
	// build fails on a missing gui/dist the same way `build desktop` does — but
	// from inside the Wails CLI's own output, where it is hard to place.
	if isDir("gui") && !exists(filepath.Join("gui", "dist", "index.js")) {
		return fmt.Errorf("gui not built (missing gui/dist/index.js); run `cd gui && npm ci && npm run build`")
	}
	sandbox, err := seedSandboxConfig()
	if err != nil {
		return err
	}
	fmt.Printf("Starting desktop dev server (Ctrl+C to stop). Browser bridge at %s (BUILDMAX_HOME=./%s)\n", desktopDevServerURL, sandboxDir)
	return runWith(desktopDir, []string{"BUILDMAX_HOME=" + sandbox}, "go", "run", wailsCLIPkg, "dev", "-tags", "desktop")
}

func runPortal() error {
	if !isDir("portal") {
		return fmt.Errorf("portal/ directory not found")
	}
	// The portal imports @buildmax/gui from gui/dist, so the shared package has
	// to be built before Vite can resolve it.
	if isDir("gui") && !exists(filepath.Join("gui", "dist", "index.js")) {
		fmt.Println("Building gui package (required by portal)...")
		if err := runIn("gui", "npm", "ci"); err != nil {
			return fmt.Errorf("gui npm ci failed: %w", err)
		}
		if err := runIn("gui", "npm", "run", "build"); err != nil {
			return fmt.Errorf("gui build failed: %w", err)
		}
	}
	if !isDir(filepath.Join("portal", "node_modules")) {
		fmt.Println("Installing portal dependencies...")
		if err := runIn("portal", "npm", "ci"); err != nil {
			return fmt.Errorf("portal npm ci failed; try running 'cd portal && npm ci' manually: %w", err)
		}
	}
	fmt.Println("Starting Portal dev server (Ctrl+C to stop)...")
	return runIn("portal", "npm", "run", "dev")
}
