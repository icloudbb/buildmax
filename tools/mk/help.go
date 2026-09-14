package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/icloudbb/buildmax/internal/config"
)

// Help lives in two layers: `help` is every command, grouped by what running one
// does to you, and `help <command>` is one command's own page.
//
// It used to open on a six-command subset, with everything else behind
// `help all`. That hid commands from the one reader who needs a list — the
// person who does not yet know what exists, and who has no reason to guess that
// `eval` or `models` is a word this runner knows. It also gave every common
// command two descriptions, which drifted. The full list is thirty lines; a
// first contribution is served by the path printed under it, not by truncating
// what is above it.
//
// The per-command pages are also the single source of the usage lines the
// commands print when an argument is wrong. Before this table those lines were
// written twice — once in help, once at the failure — and drifted apart, which
// is exactly the moment a reader is least able to tell which one is right.

type helpRow struct {
	name        string
	description string
}

type helpSection struct {
	name string
	rows []helpRow
}

// helpTopic is one command's page: the argument shape, what the command does,
// what each argument means, and what to type.
type helpTopic struct {
	name     string
	usage    string   // the argument shape, without the ./make prefix
	summary  string   // one line, printed under the usage
	details  []string // paragraphs, wrapped where they are written
	args     []helpRow
	examples []string // each is an argument list, printed with the ./make prefix
	see      string   // a document that carries the long version
}

// allHelpSections groups by effect rather than by how often a command is run.
// The grouping before this one had an "Advanced" section holding gofmt next to
// a command that bills a cloud provider, and filed e2e under Deployment, where
// the two suites needing no deployment at all — cli and desktop, both plain Go
// tests `test` already runs — were invisible to anyone looking for them. A
// section header is the only place this list can answer "is it safe to run this
// right now", so that is what the headers say.
func allHelpSections() []helpSection {
	return []helpSection{
		{"Development", []helpRow{
			{"doctor [all|harbor]", "Inspect core tools; 'all' adds frontend, 'harbor' the benchmark toolchain"},
			{"setup <local|harbor>", "Create " + localDir + "/ from the templates, or install the Terminal-Bench toolchain"},
			{"build [cli [os/arch]|desktop]", "Build all targets, only " + exe(cliBinary) + ", or the Desktop app"},
			{"test [race|mysql] [pkg]", "Run isolated Go tests; add packages or `go test` flags to narrow"},
			{"check [scope]", "Run checks for go, gui, portal, desktop, docs, all, or ci"},
			{"fmt", "Format every tracked Go file with gofmt"},
			{"lint", "Run pinned golangci-lint and govulncheck"},
			{"e2e <suite>", "Run one end-to-end suite: cli, desktop, desktop-ui, desktop-launch, local, compose, kind, or all"},
			{"run <target>", "Run a binary or frontend development server locally"},
			{"board [--md]", "Show project status derived from the backlog, Roadmap, and git"},
			{"clean", "Remove binaries, native app builds, node_modules, and dist"},
			{"help [command]", "Show this list, or one command's arguments and examples"},
		}},
		{"Models and evaluation", []helpRow{
			{"models <list|info|check>", "List, look up on OpenRouter, or check " + localSettingsPath + " models"},
			{"eval [harbor] [flags]", "Measure CLI or worker behavior, or run/import Terminal-Bench"},
			{"agent-smoke", "Drive the agent's tools with a real model (needs an API key; not a deterministic test)"},
			{"cache-qualify", "Qualify prompt caching against a real provider (needs an API key; not a test)"},
		}},
		{"Deployment (starts containers or bills a provider)", []helpRow{
			{"compose <action>", "Manage the Compose quickstart (up|smoke [managed]|status|logs|down)"},
			{"kind <action>", "Manage the local Kubernetes reference deployment; run `help kind` for actions"},
			{"ocean <action>", "Manage the disposable DigitalOcean qualification infrastructure"},
		}},
		{"Release", []helpRow{
			{"changelog [new|release]", "Add, preview, or fold in unreleased entries"},
			{"release <action>", "Run bump, notes, verify, notices, or licenses"},
			{"install", "Install binaries to ~/.local/bin"},
		}},
	}
}

// helpTopics is the per-command page for every command dispatch accepts, in the
// order help lists them. TestEveryCommandHasAHelpTopic keeps the two sets
// equal, so a new command cannot ship without a page.
func helpTopics() []helpTopic {
	return []helpTopic{
		{
			name:    "doctor",
			usage:   "doctor [all|harbor]",
			summary: "Report the contributor environment without changing anything.",
			details: []string{
				"Doctor only reads. It never installs a tool, edits a file, or touches the\n" +
					"workspace, so it is safe as a first command in an unfamiliar checkout.\n" +
					"A failing check prints the command that fixes it rather than running it;\n" +
					"`" + mk() + " setup harbor` is the command that runs those for the benchmark scope.",
				"Go and git are required; everything else is reported as a warning, because\n" +
					"which of them you need depends on what you are changing. A Go-only change\n" +
					"can ignore the Node, npm, Wails, Docker, and kubectl lines.",
				"`doctor harbor` is its own section rather than a tier of the default one:\n" +
					"Harbor is needed only to run the external Terminal-Bench target, so its\n" +
					"lines would be noise for every other contributor. It reads the pinned\n" +
					"versions from evaluation/harbor/pins.json, and blocks only on the pinned\n" +
					"Harbor itself. Hub sign-in is reported and blocks nothing: a local run\n" +
					"reads the public dataset anonymously, and only publishing needs an account.",
			},
			args: []helpRow{
				{"(none)", "Core tools; frontend tools are warnings"},
				{"all", "Require the pinned Node and npm as well"},
				{"harbor", "What a Terminal-Bench run needs: the pinned Harbor, a trial sandbox, a Linux CLI"},
			},
			examples: []string{"doctor", "doctor all", "doctor harbor"},
			see:      "docs/contribute/first-pr.md",
		},
		{
			name:    "setup",
			usage:   "setup <local|harbor>",
			summary: "Create or install what a scope needs, then report what is left.",
			details: []string{
				"Setup is doctor's write half, and stays a separate command so that doctor\n" +
					"can keep changing nothing.",
				"`setup local` is the first command to run in a fresh clone. It creates\n" +
					localDir + "/, the one directory holding your own configuration, and fills it\n" +
					"from the committed templates: " + localEnvPath + " for the credentials `" + mk() + "`\n" +
					"tasks use, " + localSettingsPath + " for the models `models` and `kind seed`\n" +
					"read, and " + localSecretPath + " for a Kubernetes deployment you apply\n" +
					"by hand. It then prints what is left to fill in.",
				"The directory is gitignored in full, and the command never overwrites a file\n" +
					"you have already edited -- delete one to start it over. A file left at its\n" +
					"pre-" + localDir + "/ path is moved rather than duplicated, so credentials that\n" +
					"were working keep working.",
				"`setup harbor` installs uv if it is missing, installs the Harbor version\n" +
					"pinned by evaluation/harbor/pins.json, and cross-builds the linux/amd64 CLI\n" +
					"a trial uploads -- the task images are amd64 whatever this machine is. Every\n" +
					"step is skipped when it is already done.",
				"Installing uv runs Astral's own installer, which downloads and executes a\n" +
					"script from outside this repository. The exact command is printed before it\n" +
					"runs. Nothing else here reaches beyond uv, Go, and this checkout.",
				"It finishes by running `doctor harbor`, so what setup installs and what a\n" +
					"benchmark run requires cannot drift apart. A trial sandbox is the piece it\n" +
					"will not install for you: choose Docker or a DAYTONA_API_KEY yourself.",
				"uv installs into ~/.local/bin. When that is not on your PATH, setup uses it\n" +
					"for the rest of the run and prints how to make it permanent.",
			},
			args: []helpRow{
				{"local", localDir + "/ and the config files a fresh clone needs"},
				{"harbor", "uv, the pinned Harbor, and a Linux CLI for Terminal-Bench"},
			},
			examples: []string{"setup local", "setup harbor"},
			see:      "docs/reference/configuration.md",
		},
		{
			name:    "build",
			usage:   "build [cli [os/arch]|desktop]",
			summary: "Build every local target, or one of them.",
			details: []string{
				"The full build is strict and covers the Go binaries, the shared gui package,\n" +
					"Portal, and the Wails desktop app, so it needs the pinned Node and npm.\n" +
					"`build cli` needs nothing but Go and is the fast inner loop.",
				"`build desktop` packages the Wails app alone -- the frontend, the gui package\n" +
					"it consumes, and the native bundle -- without spending the server, worker,\n" +
					"and Portal builds to get at it.",
				"Binaries land in " + binDir + "/ with the same version and commit ldflags a\n" +
					"released build carries.",
				"`build cli linux/amd64` cross-builds a static CLI for another platform and\n" +
					"names the artifact after it, leaving the host binary alone. That is what\n" +
					"puts the CLI inside a container image someone else owns.",
			},
			args: []helpRow{
				{"(none)", "Go binaries, gui, Portal, and the desktop app"},
				{"cli", "Only " + exe(cliBinary)},
				{"cli <os/arch>", "A static " + cliBinary + "-<os>-<arch> for another platform"},
				{"desktop", "Only the packaged desktop app, and the gui build it needs"},
			},
			examples: []string{"build cli", "build cli linux/amd64", "build desktop", "build"},
		},
		{
			name:    "test",
			usage:   "test [race|mysql] [packages] [go test flags]",
			summary: "Run Go tests with BUILDMAX_HOME pointed at the testing sandbox.",
			details: []string{
				"Narrowing a run belongs here rather than in a bare `go test`: only this\n" +
					"command sets BUILDMAX_HOME to ./" + sandboxDir + ", and config.DataDir panics\n" +
					"under test rather than fall back to your real ~/.buildmax.",
				"Packages have to come before flags. A pattern written after one cannot be\n" +
					"told from a flag value, so the run would silently widen to ./... — this\n" +
					"refuses instead of passing for the wrong reason.",
				"`test mysql` is the persistence gate. The store tests skip themselves\n" +
					"without " + config.EnvKeyBuildmaxTestDSN + ", so a plain run proves nothing about\n" +
					"schema, query, or transaction behavior; this scope requires the DSN, runs\n" +
					"on a database it creates and drops, and fails if a test skips anyway.\n" +
					"It never starts Docker — point it at a MySQL you already run.",
			},
			args: []helpRow{
				{"race", "Add the race detector"},
				{"mysql", "Run the store scope against the " + config.EnvKeyBuildmaxTestDSN + " server"},
				{"packages", "Package patterns, default ./..."},
				{"go test flags", "Passed to `go test` verbatim: -run, -count, -v"},
			},
			examples: []string{"test", "test race", "test ./internal/tool -run TestNames", "test mysql", "test mysql -run TestCreateSpace"},
			see:      "docs/contribute/testing.md",
		},
		{
			name:    "check",
			usage:   "check [go|gui|portal|desktop|docs|all|ci]",
			summary: "Run the pre-pull-request checks for one scope, or all of them.",
			details: []string{
				"`check ci` is what a pull request runs, minus the Windows job, and is the\n" +
					"command every pre-PR instruction in this repository points at. No scope\n" +
					"needs a model API key.",
				"Prefer a narrow scope while iterating and the wide one before handing work\n" +
					"over. `check ci` also reports any file the checks themselves dirtied, which\n" +
					"is what CI sees as a failing tree.",
			},
			args: []helpRow{
				{"go", "gofmt, go mod tidy, build, vet, race tests, lint"},
				{"gui", "gui build, then the shared component tests"},
				{"portal", "gui build, then Portal lint, build, and tests"},
				{"desktop", "gui build, then Desktop frontend lint, build, and tests"},
				{"docs", "Architecture boundary tests and the Markdown lint (needs npm)"},
				{"all", "Every scope above, Go first (the default)"},
				{"ci", "The scopes plus workflow lint, secret scan, licenses,"},
				{"", "the release config, and a Windows cross build"},
			},
			examples: []string{"check go", "check ci"},
			see:      "docs/contribute/testing.md",
		},
		{
			name:    "fmt",
			usage:   "fmt",
			summary: "Format every tracked Go file with gofmt.",
			details: []string{
				"This is the fix `" + mk() + " check go` points at when it reports unformatted\n" +
					"files. It runs over the files git tracks, not the whole tree, so ignored\n" +
					"and generated directories stay untouched, and it names what it rewrote.",
			},
		},
		{
			name:    "lint",
			usage:   "lint",
			summary: "Run the pinned golangci-lint and govulncheck.",
			details: []string{
				"Both run through `go run` at the version pinned in tools/mk, which is the\n" +
					"version CI runs, so a locally installed linter cannot drift from the gate.\n" +
					"The rule set is .golangci.yml. `" + mk() + " check go` ends with this command.",
			},
		},
		{
			name:    "e2e",
			usage:   "e2e <cli|desktop|desktop-ui [core]|desktop-launch|local|compose|kind|all>",
			summary: "Run one end-to-end suite.",
			details: []string{
				"The suites are a local feedback loop, not a pull-request gate, and none of\n" +
					"them needs a provider API key: every one answers the model from a committed\n" +
					"scenario. They differ in what they own — the Portal suites attach to a\n" +
					"deployment someone else started, `local` and `desktop-ui` each own a stack\n" +
					"for one run (a Compose deployment, a `wails dev` process), and `cli` owns\n" +
					"nothing but a temporary directory. Each says which it is before it starts.",
				"There is no default suite. Naming one is how you know what a green run\n" +
					"covered, and the cheapest suites are the ones with no prerequisite at\n" +
					"all: `cli` and `desktop` are Go tests that ./make test already runs.",
				"`desktop-ui` needs Node, npm, and a Playwright Chromium install, same as\n" +
					"`local`. It drives desktop/frontend through `wails dev`'s own browser dev\n" +
					"server, bindings and all, against a fresh isolated BUILDMAX_HOME it creates\n" +
					"and discards — never a contributor's real ~/.buildmax. `wails dev` opens a\n" +
					"native window too, as a side effect of starting; this suite makes no\n" +
					"assertion about it and does not need one to be present. It stops what it\n" +
					"started when the run ends either way.",
				"Whatever the outcome, the suite leaves its evidence under " + artifactDir + "/.",
			},
			args: []helpRow{
				{"cli", "The CLI and TUI suite: built binary, temporary home"},
				{"desktop", "The Desktop bridge suite: bound methods, events, approvals — no window"},
				{"desktop-ui", "desktop/frontend driven through `wails dev`; add `core` for the @smoke subset CI gates on"},
				{"desktop-launch", "Launch the packaged app (after `build desktop`) and require it to stay up"},
				{"local", "Portal browser tests against a Compose stack this command owns"},
				{"compose", "The same tests against a running Compose stack"},
				{"kind", "The same tests against a running kind deployment"},
				{"all", "Every suite that needs no cluster: cli, desktop, then local"},
			},
			examples: []string{"e2e cli", "e2e local"},
			see:      "docs/contribute/testing.md",
		},
		{
			name:    "run",
			usage:   "run <cli|server|desktop|desktop-dev|portal> [arguments]",
			summary: "Run a built binary, or a dev server, against the testing sandbox.",
			details: []string{
				"The binaries run with BUILDMAX_HOME set to ./" + sandboxDir + ", so a local\n" +
					"run never reads or writes your real ~/.buildmax. Build them first; the\n" +
					"command says so when one is missing. On first use your model and server\n" +
					"settings are copied into the sandbox, so the CLI can answer straight away.",
				"Arguments after `cli` are passed to the CLI itself.",
			},
			args: []helpRow{
				{"cli", "Run " + exe(cliBinary)},
				{"server", "Run " + exe(serverBinary)},
				{"desktop", "Run " + exe(desktopBinary)},
				{"desktop-dev", "Run `wails dev`: the browser bridge at " + desktopDevServerURL + ", live-reloading"},
				{"portal", "Start the Portal dev server (Vite)"},
			},
			examples: []string{"run cli", "run cli -- init", "run server", "run desktop-dev"},
		},
		{
			name:    "clean",
			usage:   "clean",
			summary: "Remove build outputs and installed frontend dependencies.",
			details: []string{
				"Removes " + binDir + "/, the desktop app build, and node_modules plus dist for\n" +
					"gui, Portal, and the Desktop frontend. The next full build reinstalls them,\n" +
					"which takes minutes rather than seconds.",
				"It leaves ./" + sandboxDir + " and your real ~/.buildmax alone: neither is a\n" +
					"build output.",
			},
		},
		{
			name:    "help",
			usage:   "help [command]",
			summary: "Show every command, or one command's own page.",
			details: []string{
				"With no argument it prints every command, grouped by what it is for, and\n" +
					"the four-command path a first contribution takes. `help all` is the old\n" +
					"spelling of that and prints the same list.",
				"Every command also answers its own help flag: `" + mk() + " check --help` prints\n" +
					"the same page as `" + mk() + " help check`. The exception is eval, whose\n" +
					"arguments belong to the benchmark binary.",
			},
			args: []helpRow{
				{"(none)", "Every command, grouped, and the contribution path"},
				{"<command>", "That command's arguments, examples, and caveats"},
			},
			examples: []string{"help", "help test", "help eval"},
		},
		{
			name:    "board",
			usage:   "board [--md]",
			summary: "Show the project status view derived from the backlog, Roadmap, designs, proposals, and git.",
			details: []string{
				"The board holds no state of its own. It reads the backlog task frontmatter,\n" +
					"the Roadmap `Status:` lines, the design index progress column, the open\n" +
					"proposal files, the unreleased changelog, and recent git history, so it\n" +
					"cannot drift from them the way a hand-maintained status file would. There\n" +
					"is nothing to commit.",
				"A backlog task is in progress when it is claimed, in review once its `pr`\n" +
					"field names an open pull request, ready when it is unclaimed and every\n" +
					"dependency has merged, and blocked while a dependency file still exists.\n" +
					"The format contract those columns depend on is enforced by the architecture\n" +
					"tests `" + mk() + " check docs` runs, not by this command.",
				"A design record is listed as unfinished unless its `docs/design/README.md`\n" +
					"progress is Complete or Superseded in part. Every proposal file is listed,\n" +
					"since a proposal exists only while its direction is still open.",
				"`--md` prints the same view as Markdown on stdout, for pasting into a note or\n" +
					"an issue. It is not written to a file.",
			},
			args: []helpRow{
				{"(none)", "Print the status view for the terminal"},
				{"--md", "Print the same view as Markdown on stdout"},
			},
			examples: []string{"board", "board --md"},
			see:      "docs/backlog/README.md",
		},
		{
			name:    "models",
			usage:   "models <list|info [model or search term]|check>",
			summary: "List locally configured models, look up one on OpenRouter, or check for drift.",
			details: []string{
				"Reads " + localSettingsPath + ": a gitignored file in the same shape as\n" +
					"BUILDMAX_HOME/settings.yaml, kept separate so this never touches your real\n" +
					"runtime configuration. `" + mk() + " setup local` creates it from\n" +
					"" + localSettingsExample + ".",
				"`list` prints the models configured there — no network. `info` with no\n" +
					"argument prints the full info block for every model configured in\n" +
					"" + localSettingsPath + ", in file order. With a model id or search term,\n" +
					"it fetches the live catalog from " + openRouterModelsURL + " and prints\n" +
					"context window, modality, supported parameters, and full pricing. An exact\n" +
					"model id (as it appears in " + localSettingsPath + ", e.g. openai/gpt-4o-mini)\n" +
					"matches that one model; anything else is matched as a case-insensitive\n" +
					"substring against every id and display name, and every match is printed.\n" +
					"When a configured model has an api_key, `info` sends it, since OpenRouter\n" +
					"can return an account-specific rate; the public catalog still answers with\n" +
					"no key.",
				"`check` compares every configured context_window against OpenRouter's\n" +
					"current value and exits non-zero if any model has drifted or has\n" +
					"disappeared from the catalog — provider catalogs change without notice.",
			},
			args: []helpRow{
				{"list", "List the models in " + localSettingsPath},
				{"info", "OpenRouter details for every model in " + localSettingsPath},
				{"info <model or search term>", "OpenRouter details for a model"},
				{"check", "Diff configured context_window against OpenRouter"},
			},
			examples: []string{"models list", "models info", "models info openai/gpt-4o-mini", "models check"},
		},
		{
			name:    "eval",
			usage:   "eval [harbor] [flags]",
			summary: "Evaluate the CLI against evaluation/suite/, or import an external benchmark.",
			details: []string{
				"Builds " + exe(cliBinary) + " and the runner, then measures CLI tasks as a black box.\n" +
					"Pass --surface worker to build " + exe(workerBinary) + " and run worker tasks,\n" +
					"or --surface all to run both surfaces. A worker task is dispatched the way a\n" +
					"scheduler dispatches one. Every trial runs the artifact a user would run, in a\n" +
					"temporary home built from the subject alone, so your own settings, plugins,\n" +
					"and hooks cannot change what is measured.",
				"Arguments pass through, so `" + mk() + " eval --help` prints the runner's own flags\n" +
					"rather than this page. --binary defaults to the CLI just built; pass it\n" +
					"explicitly to measure a different artifact, and --baseline to compare two.",
				"Your model credential is read from settings.yaml, so this needs a model API key\n" +
					"and spends tokens. Trial bundles are written under .artifacts/evaluation/ and\n" +
					"stay on this machine.",
				"`" + mk() + " eval harbor run` starts the external benchmark instead: it checks\n" +
					"the toolchain, cross-builds the linux/amd64 CLI, assembles the Harbor command\n" +
					"from evaluation/harbor/pins.json -- dataset ref included -- launches it, and\n" +
					"imports the finished job. Select tasks with --task, --canary, --limit, or\n" +
					"--all; there is no default, because the default would be the whole dataset.\n" +
					"--oracle runs each task's own solution to prove the environment, and\n" +
					"--dry-run prints the command without running it. It needs Docker and a model\n" +
					"API key, and it spends money.",
				"`" + mk() + " eval harbor --job <dir>` is the import alone: it files a\n" +
					"Terminal-Bench job Harbor already ran and reports it in the same contract.\n" +
					"It builds no CLI and calls no model — the artifact that produced the job is\n" +
					"named by the evidence, not by whatever this tree compiles to now — and it\n" +
					"measures rather than gates, so a task the subject did not solve is a score\n" +
					"and not a failure. See evaluation/harbor/README.md and `" + mk() + " doctor harbor`.",
				"See docs/design/evaluation-system.md for what the suites measure and what a\n" +
					"bundle contains.",
			},
			args: []helpRow{
				{"(none)", "Measure the built CLI against the local suite"},
				{"harbor", "Import a finished Terminal-Bench job instead; takes --job"},
				{"harbor run", "Run Terminal-Bench through Harbor, then import the job"},
			},
			examples: []string{
				"eval --help",
				"eval --task local-summarize-data",
				"eval --surface worker",
				"eval --surface all",
				"eval harbor run --oracle --limit 5",
				"eval harbor run --canary --model anthropic/claude-opus-4-7",
				"eval --trials 5",
				"eval --baseline bin/buildmax-previous",
				"eval harbor --job .artifacts/harbor/jobs/<job>",
				"eval harbor --job runs/new --baseline-job runs/old",
			},
		},
		{
			name:    "agent-smoke",
			usage:   "agent-smoke",
			summary: "Drive the agent's tools with a real model. Not a test.",
			details: []string{
				"It builds the CLI and asks a real model to exercise the tools, then the model\n" +
					"writes its own PASS/FAIL table. Read that table: the exit code only says the\n" +
					"process finished. Nothing here is deterministic, which is why no check runs it.",
				"It needs a usable api_key in " + sandboxDir + "/settings.yaml and it calls a paid\n" +
					"provider. Missing configuration is reported before anything starts.",
			},
		},
		{
			name:    "cache-qualify",
			usage:   "cache-qualify [go test flags]",
			summary: "Qualify prompt caching against a real provider. Not a test.",
			details: []string{
				"Every other cache test in the tree proves what BuildMax sends and nothing\n" +
					"about what a provider does with it, and a cache is where those two come\n" +
					"apart: a request can be perfectly shaped and the provider can still decline\n" +
					"to cache it, for a minimum prefix length, an unsupported model, or a\n" +
					"retention window that expired.",
				"It runs the scenarios docs/design/prompt-cache-control.md gates on — first\n" +
					"write, sequential read, changed prefix, long-history lookback, streaming,\n" +
					"concurrent cold starts, and retention — and prints what the provider\n" +
					"reported for each. A provider is not described as cache-capable until it\n" +
					"passes.",
				"Name the target with BUILDMAX_CACHE_QUALIFY_PROVIDER, _MODEL, _API_KEY, and\n" +
					"optionally _BASE_URL. It calls a paid provider. Set\n" +
					"BUILDMAX_CACHE_QUALIFY_SLOW to include the scenarios that wait out a\n" +
					"retention window, which take minutes of wall clock.",
			},
			examples: []string{"cache-qualify"},
		},
		{
			name:    "compose",
			usage:   "compose <up|smoke [managed]|status|logs|down>",
			summary: "Manage the Docker Compose quickstart deployment.",
			details: []string{
				"This one changes your machine: it builds images and starts containers, and\n" +
					"needs Docker. `up` is a real deployment and expects a real model provider;\n" +
					"`smoke` adds the overlay that puts a deterministic model in front of the\n" +
					"server, which is what makes an agent run reproducible.",
				"`smoke managed` routes task-run inference through the gateway, so the run also\n" +
					"proves the worker held no provider credential.",
			},
			args: []helpRow{
				{"up", "Start the quickstart stack"},
				{"smoke [managed]", "Start the stack with the deterministic model and smoke it"},
				{"status", "Report container and endpoint state"},
				{"logs", "Tail the last 200 lines from every service"},
				{"down", "Stop the stack"},
			},
			examples: []string{"compose smoke", "compose logs", "compose down"},
			see:      "docs/deploy/compose.md",
		},
		{
			name:    "kind",
			usage:   "kind <up|reload [service]|seed|fixtures [--runs]|use-model <name>|mock|smoke [managed]|info [email]|login [email]|forward|status|logs [service]|down>",
			summary: "Manage the local Kubernetes reference deployment.",
			details: []string{
				"Needs Docker and kubectl, and creates a kind cluster — set BUILDMAX_KIND_CLUSTER\n" +
					"to use a name other than the default. The cluster has no registry, so `reload`\n" +
					"builds the server and Portal images locally, loads them into it, and restarts the\n" +
					"deployments so a code change takes effect; name `server` or `portal` to reload\n" +
					"just one. It is the local development loop after `up`; a deployment that does\n" +
					"not exist yet is skipped rather than failed.",
				"The kind reference serves Portal and the server from one ingress, which is the\n" +
					"difference the browser tests can see: here the bundle's API base is\n" +
					"same-origin, under Compose it is absolute.",
				"MySQL and MinIO are reachable only inside the cluster. `forward` publishes both\n" +
					"to this machine for as long as it runs, which is how you read what a run wrote.\n" +
					"A target whose host port is already taken is skipped, not fatal.",
				"A login code is single-use and printed once, so `info` issues a fresh one\n" +
					"rather than trying to show a code that is already spent. `login` is the same\n" +
					"code path as JSON on stdout instead of a banner, for a script — such as the\n" +
					"`drive-portal` skill — to sign in without a human copying anything.",
				"`seed` puts the models in " + localSettingsPath + " into the cluster's catalog, so\n" +
					"the CLI and Desktop can drive it over the managed transport with real inference.\n" +
					"A seeded row is callable at once and needs no restart. The cluster's own Portal\n" +
					"conversations and task runs keep answering from the mock, so `smoke` stays\n" +
					"deterministic and free.",
				"`use-model` then points the cluster's own conversations and task runs at a\n" +
					"seeded model through the managed gateway — it spends real provider quota, so\n" +
					"`mock` switches back to the free in-cluster mock. Both take effect by setting\n" +
					"environment on the server and restarting it; the committed config is untouched.",
				"`fixtures` seeds four accounts, personal and shared QA spaces, membership\n" +
					"roles and an invitation, assigned and nested issues, comments, workflows,\n" +
					"files, artifacts, and synthetic secrets. It grants Alice System Administrator\n" +
					"authority and publishes the sample plugins to the Marketplace, with one\n" +
					"activated in the QA space. A separate space exercises pagination.\n" +
					"Reruns reuse named resources and fill missing data. `fixtures --runs` also\n" +
					"creates a conversation, Task Continue/Retry history, and Issue Agent/Workflow\n" +
					"results. Execution requires the reference free mock configuration; it refuses\n" +
					"model overrides. Sign in with `login alice@buildmax.local`. See local-kind.md\n" +
					"for the coverage matrix and fields reconciled on reruns.",
			},
			args: []helpRow{
				{"up", "Create the cluster and apply the reference deployment"},
				{"reload [service]", "Build and load the images, then restart the deployments; server or portal for just one"},
				{"seed", "Put the models in " + localSettingsPath + " into the cluster's catalog"},
				{"fixtures [--runs]", "Seed QA data; --runs adds execution history using the free mock"},
				{"use-model <name>", "Point conversations and task runs at a seeded catalog model"},
				{"mock", "Switch conversations and task runs back to the free in-cluster mock"},
				{"smoke [managed]", "Run the deployment smoke against the cluster"},
				{"info [email]", "Print the endpoints and issue a fresh login code"},
				{"login [email]", "Issue a fresh login code as {email,code,portal_url} JSON, for a script"},
				{"forward", "Forward MySQL (3306) and MinIO (9000, 9001) to 127.0.0.1"},
				{"status", "Report pod, service, and ingress state"},
				{"logs [service]", "Tail every namespace's logs, or one of ingress, mysql, minio, server, portal, worker"},
				{"down", "Delete the cluster"},
			},
			examples: []string{"kind up", "kind fixtures", "kind seed", "kind use-model \"Claude Sonnet 5\"", "kind mock", "kind smoke"},
			see:      "docs/deploy/local-kind.md",
		},
		{
			name:    "ocean",
			usage:   "ocean <doctor|plan|up|deploy|info|app-status|show|model|database|status|down>",
			summary: "Manage the disposable DigitalOcean beta-qualification infrastructure.",
			details: []string{
				"This command uses OpenTofu to create one non-HA DOKS cluster and one single-node\n" +
					"managed MySQL cluster in the existing buildmax-beta Project and VPC. The\n" +
					"buildmax-beta Spaces bucket is also read as an existing resource. None of those\n" +
					"three persistent resources is owned or deleted by this command.",
				"DOKS and MySQL are billable until `" + mk() + " ocean down` succeeds. `up` and\n" +
					"`down` show a saved plan and require the project name as confirmation before\n" +
					"applying it. DOKS high availability is explicitly disabled for this temporary\n" +
					"qualification environment.",
				"OpenTofu state contains the database password and kubeconfig. It defaults to\n" +
					"~/.buildmax/qualification/ocean, outside the checkout, with owner-only\n" +
					"permissions. Back it up while resources exist and never publish it.",
				"`deploy` requires BUILDMAX_OCEAN_HOSTNAME and BUILDMAX_OCEAN_ALLOWED_CIDRS.\n" +
					"It deploys immutable image digests behind a Caddy HTTPS edge and prints the\n" +
					"Load Balancer IP for the Route 53 record you manage manually.",
				"`model init` reads OPENROUTER_API_KEY from .env and initializes the managed\n" +
					"model catalog without printing the key. `database forward` keeps MySQL private\n" +
					"and forwards it through the Kubernetes API to local port 13306.",
			},
			args: []helpRow{
				{"doctor", "Check tools, credentials, names, and the state location without changing anything"},
				{"plan", "Initialize OpenTofu, validate the configuration, and save a create/update plan"},
				{"up", "Plan, confirm, apply, and write an owner-only kubeconfig"},
				{"deploy", "Deploy the pinned BuildMax trial behind a restricted HTTPS edge"},
				{"info [--show-secrets]", "Print resource outputs; opt in to database credentials"},
				{"app-status", "Show application pods, services, and the Load Balancer address"},
				{"show all", "Run kubectl get all for the BuildMax namespace"},
				{"model init", "Initialize the OpenRouter model and select it for conversations"},
				{"model list", "List the managed model catalog without provider credentials"},
				{"database forward", "Forward private MySQL to 127.0.0.1:13306"},
				{"status", "List OpenTofu state entries, including persistent read-only data sources"},
				{"down", "Plan, confirm, and destroy only the disposable resources"},
			},
			examples: []string{"ocean doctor", "ocean plan", "ocean up", "ocean deploy", "ocean model init", "ocean show all", "ocean database forward", "ocean down"},
			see:      "docs/deploy/digitalocean.md",
		},
		{
			name:    "changelog",
			usage:   "changelog [new <category> <slug> | release <version>]",
			summary: "Add, preview, or fold in the unreleased changelog entries.",
			details: []string{
				"Every user-visible change needs an entry. `new` writes the file for you under\n" +
					changelogDir + "/<category>/, holding the one list item it will become — one\n" +
					"file per entry, so parallel branches never conflict in the same list.",
				"With no arguments it prints the unreleased section as it stands. `release`\n" +
					"folds those files into CHANGELOG.md under the version and removes them.",
			},
			args: []helpRow{
				{"(none)", "Print the unreleased section"},
				{"new <category> <slug>", "Add an entry: " + strings.Join(changelogCategories, ", ")},
				{"release <version>", "Fold the entries into CHANGELOG.md"},
			},
			examples: []string{"changelog", "changelog new fixed windows-path-quoting"},
			see:      changelogDir + "/README.md",
		},
		{
			name:    "release",
			usage:   "release <bump|next|notes|verify|notices|licenses|desktop>",
			summary: "Run one release chore.",
			details: []string{
				"`bump` tags the next version locally and stops there, because pushing the tag\n" +
					"is what starts the release build. `notes` prints what that build will publish\n" +
					"as the release body, or writes it with `-o`. The rest are checks and\n" +
					"generated files that CI also runs.",
				"`desktop` packages the Wails app for the host OS into " + desktopReleaseDir + "/ -- a\n" +
					"`.dmg` on macOS, the self-contained `.exe` on Windows, each with a `.sha256`.\n" +
					"GoReleaser cannot: it runs on one Linux runner, so the desktop release is a\n" +
					"per-OS job that calls this. The bundles are unsigned during alpha.",
				"Each action takes its own flags: `" + mk() + " release verify --help` prints them.",
			},
			args: []helpRow{
				{"bump [patch|minor|major]", "Tag the next version locally (default patch)"},
				{"next", "Print the next numbered alpha tag without changing git"},
				{"notes <version>", "Compose the release body from that version's CHANGELOG.md section"},
				{"verify", "Validate the built GoReleaser archives"},
				{"notices", "Regenerate NOTICE-THIRD-PARTY"},
				{"licenses", "Check npm production dependencies against the allowed set"},
				{"desktop", "Package the Wails app for the host OS into " + desktopReleaseDir + "/"},
			},
			examples: []string{"release notices", "release bump minor", "release notes v0.2.0-alpha.1"},
			see:      "docs/contribute/releasing.md",
		},
		{
			name:    "install",
			usage:   "install",
			summary: "Copy the built binaries into ~/.local/bin.",
			details: []string{
				"The CLI is required and the rest are copied when present, so `build cli`\n" +
					"followed by `install` is a valid shortcut. This writes outside the\n" +
					"repository; it tells you how to put the directory on your PATH when it is\n" +
					"not there already.",
			},
		},
	}
}

func lookupHelpTopic(name string) (helpTopic, bool) {
	for _, topic := range helpTopics() {
		if topic.name == name {
			return topic, true
		}
	}
	return helpTopic{}, false
}

// helpCommandNames lists the bare command word of every help row, dropping the
// argument placeholders the tables carry for display.
func helpCommandNames() []string {
	var names []string
	add := func(rows []helpRow) {
		for _, row := range rows {
			names = append(names, strings.Fields(row.name)[0])
		}
	}
	for _, section := range allHelpSections() {
		add(section.rows)
	}
	return names
}

// formatHelpRow lays out one name/description pair. The width lives here rather
// than at each call site so a new row lands aligned without anyone counting
// spaces, and so a usage error and the help page align the same way.
func formatHelpRow(row helpRow) string {
	const width = 24
	if row.description == "" {
		return "  " + row.name
	}
	// A name that fills the column still needs a gap after it, which %-24s does
	// not give: `bump [patch|minor|major]` is exactly 24 characters and ran into
	// its own description.
	if len(row.name) >= width {
		return "  " + row.name + "  " + row.description
	}
	return fmt.Sprintf("  %-*s%s", width, row.name, row.description)
}

func printHelpRows(rows []helpRow) {
	for _, row := range rows {
		fmt.Println(formatHelpRow(row))
	}
}

// usageErrorf answers a bad invocation with the leading line that says what was
// wrong, then the command's own argument list. The list comes from the help
// topic rather than from a string next to the check, so the two cannot disagree
// about what the command accepts.
func usageErrorf(name, format string, args ...any) error {
	var b strings.Builder
	if format != "" {
		fmt.Fprintf(&b, format+"\n", args...)
	}
	topic, ok := lookupHelpTopic(name)
	if !ok {
		return errors.New(strings.TrimSuffix(b.String(), "\n"))
	}
	fmt.Fprintf(&b, "usage: %s %s", mk(), topic.usage)
	for _, row := range topic.args {
		b.WriteString("\n")
		b.WriteString(formatHelpRow(row))
	}
	fmt.Fprintf(&b, "\nRun `%s help %s` for the full page", mk(), name)
	return errors.New(b.String())
}

func cmdHelp(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	if len(args) > 1 {
		return usageErrorf("help", "help takes at most one command")
	}
	// `all` was the spelling for the full list back when `help` printed a subset
	// of it. It stays as an alias rather than becoming an unknown topic, because
	// it is still in shell history and in older checkouts of the documentation.
	if args[0] == "all" {
		usage()
		return nil
	}
	if topic, ok := lookupHelpTopic(args[0]); ok {
		printHelpTopic(topic)
		return nil
	}
	if closest, found := nearestCommand(args[0]); found {
		return fmt.Errorf("no help topic %q; did you mean `%s help %s`?", args[0], mk(), closest)
	}
	return fmt.Errorf("no help topic %q; run `%s help` for the command list", args[0], mk())
}

func printHelpTopic(topic helpTopic) {
	m := mk()
	fmt.Printf("Usage: %s %s\n", m, topic.usage)
	fmt.Println()
	fmt.Println(topic.summary)
	for _, paragraph := range topic.details {
		fmt.Println()
		fmt.Println(paragraph)
	}
	if len(topic.args) > 0 {
		fmt.Println()
		fmt.Println("Arguments:")
		printHelpRows(topic.args)
	}
	if len(topic.examples) > 0 {
		fmt.Println()
		fmt.Println("Examples:")
		for _, example := range topic.examples {
			fmt.Printf("  %s %s\n", m, example)
		}
	}
	if topic.see != "" {
		fmt.Println()
		fmt.Printf("See %s\n", topic.see)
	}
}

func usage() {
	m := mk()
	fmt.Printf("Usage: %s <command>\n", m)
	for _, section := range allHelpSections() {
		fmt.Println()
		fmt.Printf("%s:\n", section.name)
		printHelpRows(section.rows)
	}
	// The path is printed under the list rather than in place of it: a first
	// contribution still gets its four commands, and everyone else has already
	// read that the runner does more than build and test.
	fmt.Println()
	fmt.Println("Typical contribution path:")
	fmt.Printf("  %s doctor\n", m)
	fmt.Printf("  %s build cli\n", m)
	fmt.Printf("  %s test\n", m)
	// `check ci` rather than `check all`: it is what gates the pull request, and
	// naming a weaker command here than first-pr.md does sent contributors to
	// whichever document they happened to read.
	fmt.Printf("  %s check ci\n", m)
	fmt.Println()
	fmt.Printf("Run %s help <command> for one command's arguments and examples.\n", m)
}
