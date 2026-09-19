package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/icloudbb/buildmax/evaluation/contract"
	"github.com/icloudbb/buildmax/evaluation/harbor"
	"github.com/icloudbb/buildmax/evaluation/runner"
)

const harborRunUsage = `usage: eval harbor run --model <provider/model> [flags] [harbor flags]

Run the pinned Terminal-Bench target through Harbor, then import the job.

Harbor owns the tasks, the containers, and the verdict. What this owns is the
command: the dataset with its immutable ref, the adapter's import path, and the
PYTHONPATH that lets Harbor import it all come from evaluation/harbor/pins.json,
so a run cannot quietly measure a floating dataset.

It needs Docker (or a cloud sandbox) and a model API key, and it spends money.
Pick tasks with --task or --canary; --all is the whole dataset and is the
expensive one. Arguments after the flags are passed to Harbor verbatim.
`

// harborRunOptions is what a run needs beyond the pins. Anything a result
// depends on that is not here is pinned, which is the point of the split.
type harborRunOptions struct {
	model     string
	binary    string
	reasoning string
	tasks     stringList
	canary    bool
	all       bool
	oracle    bool

	attempts      int
	limit         int
	maxIterations int

	jobsDir string
	jobName string

	dryRun   bool
	noImport bool
}

// stringList is a flag that may be repeated, and that also splits a
// comma-separated value: both spellings of "these tasks" are ones a caller
// reasonably tries.
type stringList []string

func (l *stringList) String() string { return strings.Join(*l, ",") }

func (l *stringList) Set(value string) error {
	for _, part := range strings.Split(value, ",") {
		if part = strings.TrimSpace(part); part != "" {
			*l = append(*l, part)
		}
	}
	return nil
}

func runHarborBenchmark(args []string) error {
	var opt harborRunOptions
	// The import flags are the same ones the import-only form takes: a run ends
	// in an import, and it would be its own surprise if the bundles landed
	// somewhere else depending on which command wrote them.
	var imp harborOptions

	fs := flag.NewFlagSet("harbor run", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(os.Stderr, harborRunUsage, "\nflags:\n"); fs.PrintDefaults() }
	fs.StringVar(&opt.model, "model", "", "Harbor model, as <provider>/<model> (required unless --oracle)")
	fs.StringVar(&opt.binary, "binary", filepath.Join("bin", "buildmax-linux-amd64"),
		"the Linux CLI a trial uploads and measures")
	fs.StringVar(&opt.reasoning, "reasoning", "", "reasoning effort: off, low, medium, or high")
	fs.Var(&opt.tasks, "task", "qualified task name, <org>/<name>; repeatable or comma-separated")
	fs.BoolVar(&opt.canary, "canary", false, "run the canary subset named in pins.json")
	fs.BoolVar(&opt.all, "all", false, "run the whole dataset")
	fs.BoolVar(&opt.oracle, "oracle", false,
		"run each task's own reference solution instead of BuildMax, to prove the environment")
	fs.IntVar(&opt.attempts, "attempts", 1, "attempts per task")
	fs.IntVar(&opt.limit, "limit", 0, "cap how many tasks run, applied after the filters")
	fs.IntVar(&opt.maxIterations, "max-iterations", 0, "agent iteration cap; unset takes the CLI's own default")
	fs.StringVar(&opt.jobsDir, "jobs-dir", filepath.Join(".artifacts", "harbor", "jobs"),
		"where Harbor writes the job")
	fs.StringVar(&opt.jobName, "job-name", "", "name for the job directory (default: buildmax-<timestamp>)")
	fs.BoolVar(&opt.dryRun, "dry-run", false, "print the Harbor command and exit without running it")
	fs.BoolVar(&opt.noImport, "no-import", false, "leave the finished job unimported")
	fs.StringVar(&imp.pins, "pins", filepath.Join("evaluation", "harbor", harbor.PinsFile),
		"pinned harness, dataset, and adapter versions")
	fs.StringVar(&imp.out, "out", filepath.Join(".artifacts", "evaluation"),
		"directory to write trial bundles into")
	fs.StringVar(&imp.name, "name", "candidate", "name for the imported subject")
	fs.StringVar(&imp.retention, "retention", string(contract.RetentionBounded),
		"how much free text bundles keep: full, bounded, or metadata")
	if err := fs.Parse(args); err != nil {
		return err
	}

	pins, err := harbor.LoadPins(imp.pins)
	if err != nil {
		return err
	}
	spec, err := opt.spec(pins, fs.Args())
	if err != nil {
		return err
	}
	if opt.dryRun {
		fmt.Println(strings.Join(harbor.RunCommand(pins, spec), " "))
		return nil
	}

	fmt.Printf("%s, %d attempt(s) each, into %s\n", describeSelection(opt, pins), spec.Attempts, spec.JobsDir)
	_, runErr := harbor.Run(pins, spec, os.Stdout, os.Stderr)
	if runErr != nil {
		// Reported, not returned yet: a run that died partway still holds the
		// trials that finished, and importing them is how it gets diagnosed.
		fmt.Fprintf(os.Stderr, "\n%v\n", runErr)
	}
	resolved, resolveErr := harbor.ResolveJob(spec.JobsDir, spec.JobName)
	if resolveErr != nil {
		// Nothing was written, so there is nothing to import and nothing to
		// diagnose from. The run's own error is the better one to report.
		if runErr != nil {
			return runErr
		}
		return fmt.Errorf("harbor wrote no job under %s", spec.JobsDir)
	}
	jobDir := resolved
	fmt.Printf("\njob: %s\n", jobDir)

	if opt.oracle {
		// An oracle job measured the environment with the tasks' own solutions.
		// There is no BuildMax artifact in it, so there is nothing to file as a
		// subject's evidence.
		fmt.Println("oracle run: nothing to import, the subject was the task's own solution")
		return runErr
	}
	if opt.noImport {
		fmt.Printf("import it with: eval harbor --job %s\n", jobDir)
		return runErr
	}
	if err := importFinishedJob(jobDir, imp, pins); err != nil {
		return err
	}
	return runErr
}

// spec turns the flags into the one shape RunCommand takes, refusing the
// combinations that would spend money on the wrong thing.
func (o harborRunOptions) spec(pins harbor.Pins, extra []string) (harbor.RunSpec, error) {
	spec := harbor.RunSpec{
		Model:      o.model,
		Attempts:   o.attempts,
		Limit:      o.limit,
		JobsDir:    o.jobsDir,
		JobName:    o.jobName,
		AdapterSrc: filepath.Join("evaluation", "harbor", "src"),
		Extra:      extra,
	}
	if spec.JobName == "" {
		spec.JobName = harbor.DefaultJobName(time.Now())
	}

	switch {
	case o.canary && o.all:
		return spec, fmt.Errorf("--canary and --all select different task sets; pass one")
	case o.canary:
		spec.Tasks = append(spec.Tasks, pins.Canary.Tasks...)
	case len(o.tasks) > 0:
		spec.Tasks = append(spec.Tasks, o.tasks...)
	case o.all, o.limit > 0:
		// The whole dataset, or as much of it as --limit allows.
	default:
		// No default selection. The dataset is 66 tasks and every attempt costs
		// money, so the expensive run is the one a caller has to ask for.
		return spec, fmt.Errorf("select tasks with --task, --canary, --limit, or --all")
	}

	if o.oracle {
		// The oracle takes no model and no binary: it runs the task's own
		// reference solution, and passing either would describe a subject that
		// is not the one being measured.
		spec.Agent = harbor.OracleAgent
		spec.Model = ""
		return spec, nil
	}

	if o.model == "" {
		return spec, fmt.Errorf("--model is required: BuildMax has no house model to fall back to")
	}
	info, err := os.Stat(o.binary)
	if err != nil || info.IsDir() {
		return spec, fmt.Errorf("no Linux CLI at %s: build one with `make build cli linux/amd64`, or name another with --binary", o.binary)
	}
	spec.Kwargs = map[string]any{"binary": o.binary}
	if o.reasoning != "" {
		spec.Kwargs["reasoning_effort"] = o.reasoning
	}
	if o.maxIterations > 0 {
		spec.Kwargs["max_iterations"] = o.maxIterations
	}
	return spec, nil
}

func describeSelection(o harborRunOptions, pins harbor.Pins) string {
	agent := "BuildMax"
	if o.oracle {
		agent = "the oracle"
	}
	switch {
	case o.canary:
		return fmt.Sprintf("%s over the %d canary task(s)", agent, len(pins.Canary.Tasks))
	case len(o.tasks) > 0:
		return fmt.Sprintf("%s over %s", agent, strings.Join(o.tasks, ", "))
	case o.limit > 0:
		return fmt.Sprintf("%s over %d task(s) of %s", agent, o.limit, pins.Dataset.Name)
	default:
		return fmt.Sprintf("%s over all %d task(s) of %s", agent, pins.Dataset.Tasks, pins.Dataset.Name)
	}
}

// importFinishedJob files a job the run just produced, reporting it exactly as
// the import-only form does.
func importFinishedJob(jobDir string, imp harborOptions, pins harbor.Pins) error {
	experimentID := "ex_" + time.Now().UTC().Format("20060102T150405")
	result, err := importJob(jobDir, imp, pins, imp.name, experimentID, time.Now().UTC())
	if err != nil {
		return err
	}
	fmt.Println()
	runner.WriteReport(os.Stdout, result)
	return harborExit(result)
}
