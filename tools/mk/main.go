// Command mk is BuildMax's task runner: build, test, run, and release chores
// for local development.
//
// It is the implementation behind ./make and make.bat, which are one-line shims
// that forward here. The tasks live in Go rather than in shell so that macOS,
// Linux, and Windows contributors run the same code path — the previous
// bash + batch pair drifted apart because every task had to be written twice,
// and the batch half quietly stopped building the server, worker, gui, and
// desktop targets.
//
// mk depends only on the standard library, so `./make test` still works when
// the rest of the module does not compile.
package main

import (
	"fmt"
	"os"
	"runtime"
)

const (
	cliBinary     = "buildmax"
	serverBinary  = "buildmax-server"
	workerBinary  = "buildmax-worker"
	desktopBinary = "buildmax-desktop"
	evalBinary    = "eval"

	binDir     = "bin"
	desktopDir = "cmd/buildmax-desktop"
	sandboxDir = "testing-sandbox"
)

func main() {
	if err := dispatch(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func dispatch(args []string) error {
	root, err := moduleRoot()
	if err != nil {
		return err
	}
	// Every task below assumes the repo root is the working directory, the same
	// way the old scripts began with a cd to their own directory.
	if err := os.Chdir(root); err != nil {
		return err
	}
	if err := loadDotEnv(root); err != nil {
		return err
	}

	if len(args) == 0 {
		usage()
		return nil
	}
	rest := args[1:]
	// `<command> --help` is the form most people try before `help <command>`,
	// and every one of these commands used to answer it by running, or by
	// reporting the flag as a bad argument. eval is the exception: its
	// arguments belong to the benchmark binary, which has help of its own.
	if len(rest) > 0 && isHelpFlag(rest[0]) && args[0] != "eval" {
		return cmdHelp(args[:1])
	}
	switch args[0] {
	case "build":
		return cmdBuild(rest)
	case "clean":
		return cmdClean()
	case "test":
		return cmdTest(rest)
	case "check":
		return cmdCheck(rest)
	case "doctor":
		return cmdDoctor(rest)
	case "setup":
		return cmdSetup(rest)
	case "fmt":
		return cmdFmt()
	case "lint":
		return cmdLint()
	case "agent-smoke":
		return cmdAgentSmoke()
	case "cache-qualify":
		return cmdCacheQualify(rest)
	case "e2e":
		return cmdE2E(rest)
	case "eval":
		return cmdEval(rest)
	case "run":
		return cmdRun(rest)
	case "board":
		return cmdBoard(rest)
	case "changelog":
		return cmdChangelog(rest)
	case "models":
		return cmdModels(rest)
	case "release":
		return cmdRelease(rest)
	case "install":
		return cmdInstall()
	case "compose":
		return cmdCompose(rest)
	case "kind":
		return cmdKind(rest)
	case "ocean":
		return cmdOcean(rest)
	case "help", "-h", "--help":
		return cmdHelp(rest)
	default:
		return unknownCommand(args[0])
	}
}

// isHelpFlag reports whether an argument asks for the command's own page. The
// bare word is accepted alongside the flags because a task runner reads like a
// tool with subcommands, where `<command> help` is the usual spelling.
func isHelpFlag(arg string) bool {
	switch arg {
	case "help", "-h", "--help":
		return true
	}
	return false
}

// unknownCommand answers a typo with the one line that says what went wrong.
// Printing the whole usage block first pushed that line off the top of a short
// terminal, and every subcommand here already reports a bad argument on its
// own — `check` names its scopes, `changelog` names its categories. The command
// list is too long to inline, so this points at help instead.
func unknownCommand(name string) error {
	m := mk()
	if closest, found := nearestCommand(name); found {
		return fmt.Errorf("unknown command: %s; did you mean `%s %s`? Run `%s help` for the command list", name, m, closest, m)
	}
	return fmt.Errorf("unknown command: %s; run `%s help` for the command list", name, m)
}

// nearestCommand finds the documented command a mistyped word probably meant.
// The candidates come from the help tables rather than from a list of their
// own: a command missing from help is undiscoverable anyway, so there is no
// second place for this to drift from.
func nearestCommand(name string) (string, bool) {
	budget := 1
	if len(name) >= 4 {
		// Two, so a transposition — `buidl` for `build` — still resolves.
		budget = 2
	}
	best, bestDistance := "", budget+1
	for _, candidate := range helpCommandNames() {
		if distance := editDistance(name, candidate); distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}
	return best, best != ""
}

// editDistance is the usual Levenshtein distance, over bytes: every command
// name here is ASCII.
func editDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min(previous[j]+1, current[j-1]+1, previous[j-1]+cost)
		}
		previous, current = current, previous
	}
	return previous[len(b)]
}

// mk names the entry point in help and error text, so Windows users are not
// told to run a shell script they cannot invoke.
func mk() string {
	if runtime.GOOS == "windows" {
		return "make.bat"
	}
	return "./make"
}

// exe appends the executable suffix Windows requires, leaving other platforms
// untouched.
func exe(name string) string {
	if runtime.GOOS == "windows" {
		return name + ".exe"
	}
	return name
}

func cmdRelease(args []string) error {
	if len(args) == 0 {
		return usageErrorf("release", "release needs an action")
	}
	subcommand, rest := args[0], args[1:]
	switch subcommand {
	case "bump":
		return cmdBump(rest)
	case "next":
		return cmdNextVersion(rest)
	case "desktop":
		return cmdReleaseDesktop(rest)
	case "verify":
		return cmdVerifyArchive(rest)
	case "notes":
		return cmdReleaseNotes(rest)
	case "notices":
		return cmdNotices(rest)
	case "licenses":
		return cmdNPMLicenses(rest)
	case "upgrade-fixture":
		return cmdReleaseUpgradeFixture(rest)
	default:
		return usageErrorf("release", "unknown release action: %s", subcommand)
	}
}
