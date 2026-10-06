package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/icloudbb/buildmax/internal/config"
)

// scopeOrder is the order `all` and `ci` run their scopes in: the Go gate
// first, because it fails fastest on the code most changes touch.
// gui runs before its two consumers: a component change that breaks its own
// tests should say so once, not twice through Portal and Desktop.
var scopeOrder = []string{"go", "gui", "portal", "desktop", "docs"}

func cmdCheck(args []string) error {
	scope := "all"
	if len(args) > 0 {
		scope = args[0]
	}
	if len(args) > 1 {
		return usageErrorf("check", "check takes at most one scope")
	}

	checks := map[string]func() error{
		"go":      checkGo,
		"gui":     checkGUI,
		"portal":  checkPortal,
		"desktop": checkDesktop,
		"docs":    checkDocs,
	}
	switch scope {
	case "all":
		if err := runScopes(checks); err != nil {
			return err
		}
		fmt.Println("[check] all scopes passed")
		return nil
	case "ci":
		return checkCI(checks)
	}
	check, ok := checks[scope]
	if !ok {
		return usageErrorf("check", "unknown check scope: %s", scope)
	}
	return runCheck(scope, check)
}

func runScopes(checks map[string]func() error) error {
	for _, name := range scopeOrder {
		if err := runCheck(name, checks[name]); err != nil {
			return err
		}
	}
	return nil
}

func runCheck(name string, check func() error) error {
	fmt.Printf("[check] %s\n", name)
	if err := check(); err != nil {
		return fmt.Errorf("check %s: %w", name, err)
	}
	fmt.Printf("[check] %s passed\n", name)
	return nil
}

func checkGo() error {
	files, err := trackedGoFiles()
	if err != nil {
		return err
	}
	for _, batch := range batchArgs(files) {
		unformatted, err := capture("gofmt", append([]string{"-l"}, batch...)...)
		if err != nil {
			return fmt.Errorf("gofmt: %w", err)
		}
		if unformatted != "" {
			return fmt.Errorf("unformatted Go files:\n%s\nrun: %s fmt", unformatted, mk())
		}
	}
	if err := runCmd("go", "mod", "tidy", "-diff"); err != nil {
		return fmt.Errorf("go.mod is not tidy: %w", err)
	}
	if err := runCmd("go", "build", "./..."); err != nil {
		return err
	}
	if err := runCmd("go", "vet", "./..."); err != nil {
		return err
	}
	if err := cmdTest([]string{"race"}); err != nil {
		return err
	}
	return cmdLint()
}

// cmdFmt formats every tracked Go file.
//
// checkGo has always reported unformatted files, but the fix it handed back was
// `git ls-files '*.go' | xargs gofmt -w` — a shell pipeline cmd.exe cannot run,
// from the task runner that exists precisely so all three platforms run the
// same code. The most frequent remedy in the repository was the one command
// Windows contributors could not follow.
func cmdFmt() error {
	files, err := trackedGoFiles()
	if err != nil {
		return err
	}
	if len(files) == 0 {
		fmt.Println("No tracked Go files.")
		return nil
	}
	// -l with -w so the report names what actually changed. Saying "formatted
	// 634 files" after rewriting none is the kind of output that stops being read.
	var rewritten []string
	for _, batch := range batchArgs(files) {
		changed, err := captureErr("gofmt", append([]string{"-l", "-w"}, batch...)...)
		if err != nil {
			return fmt.Errorf("gofmt -w: %w", err)
		}
		if changed != "" {
			rewritten = append(rewritten, strings.Split(changed, "\n")...)
		}
	}
	if len(rewritten) == 0 {
		fmt.Printf("All %d tracked Go files were already formatted.\n", len(files))
		return nil
	}
	for _, file := range rewritten {
		fmt.Println(file)
	}
	fmt.Printf("Reformatted %d of %d tracked Go files.\n", len(rewritten), len(files))
	return nil
}

// trackedGoFiles lists the Go files git knows about. gofmt runs over that list
// rather than over `.` so ignored and generated trees stay untouched.
func trackedGoFiles() ([]string, error) {
	files, err := capture("git", "ls-files", "*.go")
	if err != nil {
		return nil, fmt.Errorf("list tracked Go files: %w", err)
	}
	if files == "" {
		return nil, nil
	}
	return strings.Split(files, "\n"), nil
}

// batchArgs splits a file list into command lines cmd.exe can carry. Its limit
// is about 32 KB and the tracked Go files already spend two thirds of that, so
// the list would have outgrown a single invocation on Windows first — the
// platform least likely to be the one that noticed.
func batchArgs(files []string) [][]string {
	const limit = 6000
	var batches [][]string
	var batch []string
	size := 0
	for _, file := range files {
		if len(batch) > 0 && size+len(file)+1 > limit {
			batches = append(batches, batch)
			batch, size = nil, 0
		}
		batch = append(batch, file)
		size += len(file) + 1
	}
	if len(batch) > 0 {
		batches = append(batches, batch)
	}
	return batches
}

// checkGUI builds the shared component package, lints it (Stylelint and the
// jsx-a11y ESLint pass), and runs its own tests. The build already runs as
// part of the Portal and Desktop scopes, because both consume dist/; what is
// here and nowhere else is gui's own lint and test run.
func checkGUI() error {
	if err := buildGUI(); err != nil {
		return err
	}
	if err := runIn("gui", "npm", "run", "lint"); err != nil {
		return err
	}
	return runIn("gui", "npm", "test")
}

func checkPortal() error {
	if err := buildGUI(); err != nil {
		return err
	}
	if err := ensureNPMDeps("portal"); err != nil {
		return err
	}
	for _, args := range [][]string{{"run", "lint"}, {"run", "build"}, {"test"}} {
		if err := runIn("portal", "npm", args...); err != nil {
			return err
		}
	}
	return nil
}

func checkDesktop() error {
	if err := buildGUI(); err != nil {
		return err
	}
	if err := ensureNPMDeps("desktop/frontend"); err != nil {
		return err
	}
	for _, args := range [][]string{{"run", "lint"}, {"run", "build"}, {"test"}} {
		if err := runIn("desktop/frontend", "npm", args...); err != nil {
			return err
		}
	}
	return nil
}

func checkDocs() error {
	if err := runCmd("go", "test", "./internal/architecture"); err != nil {
		return err
	}
	// markdownlint has no Go equivalent worth swapping the rule set for, so the
	// documentation gate is the one contributor path that needs Node — and a
	// documentation fix is what first-pr.md recommends starting with. Saying so
	// beats `exec: "npm": executable file not found in $PATH`.
	if !have("npm") {
		return fmt.Errorf("the Markdown lint needs Node and npm, which are not installed\n  Install the version in .node-version, or open the pull request and let CI run this check")
	}
	return runCmd("npm", "exec", "--yes", "--package=markdownlint-cli2@0.23.2", "--", "markdownlint-cli2")
}

// Pinned to the versions .github/workflows/ci.yml runs, so a local pass means
// the same thing CI does. TestCIToolPinsMatchWorkflow fails when they drift,
// rather than leaving the local check quietly on an older tool.
const (
	actionlintPkg = "github.com/rhysd/actionlint/cmd/actionlint@v1.7.12"
	gitleaksPkg   = "github.com/zricethezav/gitleaks/v8@v8.30.1"
	goLicensesPkg = "github.com/google/go-licenses@v1.6.0"

	// GoReleaser reaches CI through goreleaser-action, so nothing here can read
	// its version from a module path the way the pins above do.
	// TestGoReleaserPinMatchesWorkflows compares this against the `version:`
	// those steps pass.
	goreleaserPkg = "github.com/goreleaser/goreleaser/v2@v2.17.1"
)

// checkCI runs the required pull-request suite plus the conditional release
// check, for contributors who would rather spend a laptop's time than the
// repository's Actions minutes. The native Windows test, which runs after
// merge, has no local equivalent; everything else does.
func checkCI(checks map[string]func() error) error {
	before, err := worktreeState()
	if err != nil {
		return err
	}

	// Seconds-long steps first, so an obvious failure does not wait behind the
	// race suite and three frontend builds.
	steps := []struct {
		name string
		run  func() error
	}{
		{"workflows", checkWorkflows},
		{"secrets", checkSecrets},
		{"npm licenses", func() error { return cmdNPMLicenses(nil) }},
		{"release config", checkReleaseConfig},
		{"go licenses", checkGoLicenses},
		{"windows cross build", checkWindowsCrossBuild},
	}
	for _, step := range steps {
		fmt.Printf("[check] ci: %s\n", step.name)
		if err := step.run(); err != nil {
			return fmt.Errorf("check ci %s: %w", step.name, err)
		}
	}
	if err := runScopes(checks); err != nil {
		return err
	}
	// The persistence job is part of the pull-request suite, but it needs a
	// MySQL this command must not start for you. Running it when the DSN is
	// there and naming it when it is not beats a green "ci passed" that quietly
	// covered one job fewer than CI will.
	mysqlRan, err := checkMySQLIfConfigured()
	if err != nil {
		return fmt.Errorf("check ci mysql: %w", err)
	}
	// The screenshot job renders in Docker, for the same reason MySQL is
	// conditional: this command does not start a daemon for you.
	visualRan := have("docker") && succeeds("docker", "info")
	if visualRan {
		fmt.Println("[check] ci: visual")
		if err := e2eVisual(nil); err != nil {
			return fmt.Errorf("check ci visual: %w", err)
		}
	}
	if err := reportWorktreeDrift(before); err != nil {
		return err
	}
	fmt.Printf("[check] ci passed; the Windows job still needs a Windows machine\n")
	if !mysqlRan {
		fmt.Printf("[check] ci did not run the MySQL job: set %s and run `./make test mysql`\n",
			config.EnvKeyBuildmaxTestDSN)
	}
	if !visualRan {
		fmt.Printf("[check] ci did not run the visual job: start Docker and run `%s e2e visual`\n", mk())
	}
	return nil
}

// checkMySQLIfConfigured runs the persistence scope when a DSN says where, and
// reports whether it did.
func checkMySQLIfConfigured() (bool, error) {
	if os.Getenv(config.EnvKeyBuildmaxTestDSN) == "" {
		return false, nil
	}
	fmt.Printf("[check] ci: mysql\n")
	return true, cmdTestMySQL(nil)
}

func checkWorkflows() error {
	// actionlint skips its shellcheck pass without saying so, so a `run:` block
	// can pass here and fail on the runner.
	if !have("shellcheck") {
		fmt.Println("shellcheck is not installed; actionlint will skip its shell script pass")
	}
	return runCmd("go", "run", actionlintPkg)
}

// checkSecrets scans the history of the checked-out commit, as CI does; other
// local or fetched branches are scanned when they are checked out themselves.
func checkSecrets() error {
	return runCmd("go", "run", gitleaksPkg, "git", "--redact", "--no-banner", "--exit-code", "1", "--log-opts=HEAD")
}

func checkGoLicenses() error {
	return runCmd("go", "run", goLicensesPkg, "check", "./cmd/...", "--disallowed_types=forbidden,restricted")
}

// checkReleaseConfig mirrors the pull-request half of the conditional
// release-snapshot workflow.
//
// It runs the pinned version through `go run` like every other CI tool here.
// That replaced a check that used whatever `goreleaser` the contributor had
// installed and skipped itself entirely when they had none — so the one step
// meant to catch a broken .goreleaser.yaml before it reached a release was the
// step most likely never to run. The first build takes about half a minute;
// after that it comes from the Go build cache.
func checkReleaseConfig() error {
	return runCmd("go", "run", goreleaserPkg, "check")
}

// checkWindowsCrossBuild mirrors the Go job's Windows cross-compile step. It
// proves the code still compiles for Windows; it cannot run the tests, which is
// why checkCI says so on the way out.
func checkWindowsCrossBuild() error {
	env := []string{"GOOS=windows", "GOARCH=amd64"}
	for _, args := range [][]string{{"build", "./..."}, {"vet", "./..."}} {
		if err := runWith("", env, "go", args...); err != nil {
			return err
		}
	}
	return nil
}

// worktreeState records what git already considers dirty. CI runs on a clean
// checkout and ends with `git diff --exit-code`; locally the same assertion
// would fail on unrelated work in progress, so compare against this baseline
// instead of demanding a clean tree.
func worktreeState() (map[string]string, error) {
	output, err := capture("git", "status", "--porcelain")
	if err != nil {
		return nil, fmt.Errorf("read worktree state: %w", err)
	}
	state := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		if len(line) > 3 {
			state[line[3:]] = line[:2]
		}
	}
	return state, nil
}

func reportWorktreeDrift(before map[string]string) error {
	after, err := worktreeState()
	if err != nil {
		return err
	}
	changed := driftedPaths(before, after)
	if len(changed) == 0 {
		return nil
	}
	return fmt.Errorf("the checks changed files that CI would see as a dirty tree:\n  %s", strings.Join(changed, "\n  "))
}

// driftedPaths lists what the checks themselves dirtied. A path that was
// already modified beforehand is the contributor's own work and stays out of
// the report unless a check changed it further.
func driftedPaths(before, after map[string]string) []string {
	var changed []string
	for path, status := range after {
		if before[path] != status {
			changed = append(changed, path)
		}
	}
	sort.Strings(changed)
	return changed
}

func ensureNPMDeps(dir string) error {
	if isDir(dir + "/node_modules") {
		return nil
	}
	if err := runIn(dir, "npm", "ci"); err != nil {
		return fmt.Errorf("%s npm ci: %w", dir, err)
	}
	return nil
}
